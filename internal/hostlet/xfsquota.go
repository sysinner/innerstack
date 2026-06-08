// Copyright 2015 Eryx <evorui at gmail dot com>, All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package hostlet

import (
	"crypto/sha1"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/hooto/hlog4g/hlog"
	ps_disk "github.com/shirou/gopsutil/v4/disk"

	"github.com/sysinner/incore/v2/internal/config"
	"github.com/sysinner/incore/v2/internal/hostlet/hostapi"
	"github.com/sysinner/incore/v2/internal/hostlet/hoststatus"
	"github.com/sysinner/incore/v2/pkg/inapi"
)

var (
	// quotaCtrNameV2Re matches v2 container names: app-{appId:8-16hex}-{repId:1-3digits}
	quotaCtrNameV2Re = regexp.MustCompile(`^app-[a-f0-9]{8,16}-[0-9]{1,3}$`)

	// quotaCtrNameV1Re matches v1 app instance replica names: {appId:12-20hex}.{repId:4hex}
	quotaCtrNameV1Re = regexp.MustCompile(`^[a-f0-9]{12,20}\.[a-f0-9]{4}$`)

	// quotaMultiSpace matches two or more consecutive spaces for normalizing
	// xfs_quota command output.
	quotaMultiSpace = regexp.MustCompile(`\ {2,}`)
)

// QuotaConfig manages XFS project quota entries for app instance volume isolation.
// It persists quota state to a JSON file and synchronizes the kernel quota
// projects via the xfs_quota command-line tool.
type QuotaConfig struct {
	mu          sync.Mutex      `json:"-"`
	path        string          `json:"-"`
	syncMapsSum string          `json:"-"`
	Items       []*QuotaProject `json:"items,omitempty"`
	Updated     int64           `json:"updated"`
	IdOffset    int             `json:"id_offset"`
	MountPoints []string        `json:"mount_points"`
}

// QuotaProject represents a single XFS project quota entry.
// Each active container/app-instance-replica gets a unique project ID with configured
// soft/hard block limits enforced by the XFS filesystem.
type QuotaProject struct {
	Id   int    `json:"id"`
	Mnt  string `json:"mnt"`
	Name string `json:"name"`
	Soft int64  `json:"soft"`
	Hard int64  `json:"hard"`
	Used int64  `json:"used"`
}

var (
	quotaInited    bool
	quotaRefreshed int64
	quotaCmd       = "xfs_quota"
	quotaConfig    QuotaConfig
)

// quotaCtrNameMatch extracts a container name from a directory path.
// It supports both v2 (app-{hex}-{decimal}) and v1 ({hex}.{hex}) naming
// conventions. Returns the name and true on match.
func quotaCtrNameMatch(dir string) (string, bool) {
	if n := strings.LastIndex(dir, "/"); n > 0 && (n+1) < len(dir) {
		name := dir[n+1:]
		if quotaCtrNameV2Re.MatchString(name) || quotaCtrNameV1Re.MatchString(name) {
			return name, true
		}
	}
	return "", false
}

// quotaIsV2Name returns true if the name matches the v2 container naming format.
func quotaIsV2Name(name string) bool {
	return quotaCtrNameV2Re.MatchString(name)
}

// Fetch returns the quota project with the given name, or nil.
func (it *QuotaConfig) Fetch(name string) *QuotaProject {
	it.mu.Lock()
	defer it.mu.Unlock()

	for _, v := range it.Items {
		if name == v.Name {
			return v
		}
	}
	return nil
}

// FetchById returns the quota project with the given project ID, or nil.
func (it *QuotaConfig) FetchById(id int) *QuotaProject {
	it.mu.Lock()
	defer it.mu.Unlock()

	for _, v := range it.Items {
		if id == v.Id {
			return v
		}
	}
	return nil
}

// FetchOrCreate returns an existing project by name, or allocates a new one.
// The project ID is assigned from a sequentially increasing offset (starting
// at 100) with gap detection to reuse IDs freed by removed projects.
func (it *QuotaConfig) FetchOrCreate(mnt, name string) *QuotaProject {
	it.mu.Lock()
	defer it.mu.Unlock()

	if mnt == "" || mnt == "/" {
		mnt = "/opt"
	}

	for _, v := range it.Items {
		if name == v.Name {
			return v
		}
	}

	if it.IdOffset < 100 || it.IdOffset >= 100000 {
		it.IdOffset = 100
	}

	bindID := 0
	for i := it.IdOffset; i <= 110000; i++ {
		hit := false
		for _, v := range it.Items {
			if v.Id == i {
				hit = true
				break
			}
		}
		if !hit {
			bindID = i
			break
		}
	}

	if bindID == 0 {
		return nil
	}

	p := &QuotaProject{
		Id:   bindID,
		Name: name,
		Mnt:  mnt,
	}

	it.Items = append(it.Items, p)
	it.IdOffset = bindID + 1

	return p
}

// Remove deletes the project with the given name from the in-memory list.
func (it *QuotaConfig) Remove(name string) {
	it.mu.Lock()
	defer it.mu.Unlock()

	for i, v := range it.Items {
		if name == v.Name {
			it.Items = append(it.Items[:i], it.Items[i+1:]...)
			return
		}
	}
}

// Has checks whether a project with the given ID exists.
func (it *QuotaConfig) Has(id uint32) bool {
	it.mu.Lock()
	defer it.mu.Unlock()

	for _, v := range it.Items {
		if v.Id == int(id) {
			return true
		}
	}
	return false
}

// Sync persists the quota configuration to the JSON state file.
func (it *QuotaConfig) Sync() error {
	it.Updated = time.Now().Unix()
	data, err := json.MarshalIndent(it, "", "  ")
	if err != nil {
		return fmt.Errorf("[xfsquota] marshal config: %w", err)
	}
	return os.WriteFile(it.path, data, 0644)
}

// SyncVendor writes the /etc/projects file that maps project IDs to their
// directory paths. The file is only rewritten when content changes (detected
// via SHA1 checksum) to minimize unnecessary disk I/O.
//
// It preserves any existing v1 entries that are not managed by v2,
// enabling coexistence during migration from v1 to v2.
func (it *QuotaConfig) SyncVendor() error {
	// Build the v2 entries
	v2maps := ""
	for _, v := range it.Items {
		if v.Id < 1 {
			continue
		}
		// Map project ID to the container's base directory on the host.
		// Using the base directory (not /home subdirectory) ensures that
		// xfs_quota path output contains the container name as the last
		// path component, enabling correct name matching in refresh.
		v2maps += fmt.Sprintf("%d:%s\n", v.Id,
			filepath.Clean(config.Config.Hostlet.AppPath+"/"+v.Name))
	}

	// Collect existing v1 entries from /etc/projects to preserve them
	var v1maps string
	if data, err := os.ReadFile("/etc/projects"); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			// Parse "id:path" format
			parts := strings.SplitN(line, ":", 2)
			if len(parts) != 2 {
				v1maps += line + "\n"
				continue
			}
			// Extract name from path and check if it's a v1 entry
			if name, ok := quotaCtrNameMatch(parts[1]); ok && !quotaIsV2Name(name) {
				// This is a v1 entry, preserve it
				v1maps += line + "\n"
			}
		}
	}

	maps := v1maps + v2maps

	mapsSum := fmt.Sprintf("%x", sha1.Sum([]byte(maps)))
	if mapsSum == it.syncMapsSum {
		return nil
	}

	if err := writeFile("/etc/projects", maps); err != nil {
		return err
	}
	it.syncMapsSum = mapsSum
	return nil
}

// writeFile atomically writes data to a file by truncating and rewriting.
func writeFile(path, data string) error {
	fp, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0644)
	if err != nil {
		return err
	}
	defer fp.Close()

	fp.Seek(0, 0)
	fp.Truncate(0)

	_, err = fp.WriteString(data)
	return err
}

// quotaCleanupContainer immediately removes the XFS quota project for a
// destroyed container. This provides faster cleanup than waiting for the
// next periodic quota refresh cycle.
func quotaCleanupContainer(containerName string) {
	if !quotaInited {
		return
	}

	proj := quotaConfig.Fetch(containerName)
	if proj == nil {
		return
	}

	// Zero out quota limits
	args := []string{
		"-x",
		"-c",
		fmt.Sprintf("limit -p bsoft=0 bhard=0 %d", proj.Id),
		proj.Mnt,
	}
	if _, err := exec.Command(quotaCmd, args...).CombinedOutput(); err != nil {
		slog.Warn("xfsquota: cleanup limit failed",
			"container", containerName, "error", err)
	}

	// Clear project directory mapping
	args = []string{
		"-xc",
		fmt.Sprintf("project -C %d", proj.Id),
		proj.Mnt,
	}
	if _, err := exec.Command(quotaCmd, args...).CombinedOutput(); err != nil {
		slog.Warn("xfsquota: cleanup project failed",
			"container", containerName, "error", err)
	}

	// Remove from in-memory config
	quotaConfig.Remove(containerName)

	if err := quotaConfig.SyncVendor(); err != nil {
		slog.Warn("xfsquota: SyncVendor after cleanup failed", "error", err)
	}

	if err := quotaConfig.Sync(); err != nil {
		slog.Warn("xfsquota: Sync after cleanup failed", "error", err)
	}

	slog.Info("xfsquota: container quota cleaned up",
		"container", containerName, "project_id", proj.Id)
}

// quotaKeeperInit performs one-time initialization of the XFS quota subsystem.
// It verifies xfs_quota availability, detects XFS mount points eligible for
// project quotas, and loads previously persisted quota state.
func quotaKeeperInit() error {
	if quotaInited {
		return nil
	}

	defer func() {
		quotaInited = true
	}()

	if runtime.GOOS != "linux" {
		return nil
	}

	// Verify xfs_quota command is available
	if _, err := exec.Command(quotaCmd, "-V").CombinedOutput(); err != nil {
		return errors.New("command " + quotaCmd + " not found")
	}

	devs, _ := ps_disk.Partitions(false)

	// Sort by mountpoint descending so longer paths match first
	sort.Slice(devs, func(i, j int) bool {
		return strings.Compare(devs[i].Mountpoint, devs[j].Mountpoint) > 0
	})

	appPath := config.Config.Hostlet.AppPath
	var mountPoints []string

	for _, d := range devs {
		// Only consider mount points under /data/, /opt, or the app instance base path
		if !strings.HasPrefix(d.Mountpoint, "/data/") &&
			!strings.HasPrefix(d.Mountpoint, "/opt") &&
			!strings.HasPrefix(appPath, d.Mountpoint) {
			continue
		}

		// Skip container storage mount points
		if strings.Contains(d.Mountpoint, "/opt/docker/") ||
			strings.Contains(d.Mountpoint, "devicemapper") {
			continue
		}

		if d.Fstype != "xfs" {
			slog.Warn("xfsquota: skipping non-xfs mount point",
				"mount", d.Mountpoint, "fstype", d.Fstype)
			continue
		}

		// Verify the mount point supports project quota
		if _, err := exec.Command(quotaCmd, "-x", "-c", "report", d.Mountpoint).CombinedOutput(); err != nil {
			slog.Warn("xfsquota: prjquota not available on mount point",
				"mount", d.Mountpoint)
			continue
		}

		mountPoints = append(mountPoints, d.Mountpoint)
	}

	if len(mountPoints) == 0 {
		return errors.New("no XFS quota-capable mount point found")
	}

	// Load persisted quota state
	cfgPath := config.Prefix + "/etc/hostlet_vol_quota.json"
	if data, err := os.ReadFile(cfgPath); err == nil {
		if err := json.Unmarshal(data, &quotaConfig); err != nil {
			slog.Warn("xfsquota: failed to parse saved config", "error", err)
		}
	}
	quotaConfig.path = cfgPath
	quotaConfig.MountPoints = mountPoints

	quotaRefreshed = time.Now().Unix()

	slog.Info("xfsquota: initialized",
		"mount_points", strings.Join(mountPoints, ", "))

	return nil
}

// xfsQuotaRefresh performs a full quota synchronization cycle:
//  1. Initialize the quota subsystem (first call only).
//  2. Read current quota state from XFS via xfs_quota path/df commands.
//  3. Remove stale project entries for containers that no longer exist.
//  4. Create/update project quotas for active containers based on their
//     configured volume limits.
//  5. Clean up quota limits for inactive containers (zero out limits).
func xfsQuotaRefresh() error {

	if runtime.GOOS != "linux" {
		return nil
	}

	if err := quotaKeeperInit(); err != nil {
		slog.Warn("xfsquota: init failed", "error", err)
		return nil
	}

	tn := time.Now().Unix()
	if quotaRefreshed > 0 && (tn-quotaRefreshed) < 10 {
		return nil
	}

	// Phase 1: Read current quota state from the kernel
	var (
		pathGots  = map[string]int{} // containerName -> projectId
		quotaGots = map[int]string{} // set of project IDs found active
		deviceOk  int
	)

	for _, quotaMountpoint := range quotaConfig.MountPoints {
		// Query xfs_quota path to map project IDs to directory paths
		out, err := exec.Command(quotaCmd, "-xc", "path", quotaMountpoint).CombinedOutput()
		if err != nil {
			return fmt.Errorf("[xfsquota] path query failed: %w", err)
		}

		lines := strings.Split(quotaMultiSpace.ReplaceAllString(string(out), " "), "\n")

		for _, v := range lines {
			vs := strings.Split(strings.TrimSpace(v), " ")
			if len(vs) < 4 || len(vs[0]) < 2 {
				continue
			}

			if vs[0] == "[000]" {
				deviceOk++
				continue
			}

			if len(vs) != 5 || vs[3] != "(project" || len(vs[4]) < 2 {
				continue
			}

			id, err := strconv.ParseInt(vs[4][:len(vs[4])-1], 10, 32)
			if err != nil || id < 100 {
				continue
			}

			if name, ok := quotaCtrNameMatch(vs[1]); ok {
				pathGots[name] = int(id)
			}
		}

		// Query xfs_quota df to get current usage and limits
		out, _ = exec.Command(quotaCmd, "-xc", "df", quotaMountpoint).CombinedOutput()
		lines = strings.Split(quotaMultiSpace.ReplaceAllString(string(out), " "), "\n")

		for _, v := range lines {
			v = strings.TrimSpace(v)
			vs := strings.Split(v, " ")
			if len(vs) != 6 {
				// Handle "project quota flag not set" entries
				if len(vs) > 2 && strings.Contains(v, "project quota flag not set") {
					if name, ok := quotaCtrNameMatch(vs[len(vs)-1]); ok {
						if id, ok := pathGots[name]; ok && id >= 100 {
							if proj := quotaConfig.FetchById(id); proj != nil {
								proj.Used = 0
								proj.Soft = 0
								proj.Hard = 0
								proj.Mnt = quotaMountpoint
								quotaGots[id] = quotaMountpoint
							}
						}
					}
				}
				continue
			}

			name, ok := quotaCtrNameMatch(vs[5])
			if !ok {
				continue
			}

			id, ok := pathGots[name]
			if !ok || id < 100 {
				continue
			}

			proj := quotaConfig.FetchById(id)
			if proj == nil {
				continue
			}

			if i64, err := strconv.ParseInt(vs[2], 10, 64); err == nil {
				proj.Used = i64 * 1024
			}

			if i64, err := strconv.ParseInt(vs[1], 10, 64); err == nil {
				proj.Soft = i64 * 1024
			}

			proj.Hard = proj.Soft
			proj.Mnt = quotaMountpoint

			quotaGots[id] = quotaMountpoint
		}
	}

	for p, id := range pathGots {
		if quotaGots[id] != "" {
			args := []string{
				"-xc",
				fmt.Sprintf("project -s %d", id),
				quotaGots[id],
			}
			exec.Command(quotaCmd, args...).CombinedOutput()
			hlog.Printf("info", "project init %d:%s", id, p)
		}
	}

	// Phase 2: Remove stale v2 entries whose container directories no longer
	// exist on disk. Also clean up their kernel quota projects to prevent
	// orphaned entries. v1 entries are never removed by v2 code.
	{
		var dels []*QuotaProject
		for _, v := range quotaConfig.Items {
			// Only clean up v2-managed entries
			if !quotaIsV2Name(v.Name) {
				continue
			}
			ctrDir := filepath.Clean(config.Config.Hostlet.AppPath + "/" + v.Name)
			if _, err := os.Stat(ctrDir); os.IsNotExist(err) {
				dels = append(dels, v)
			}
		}
		for _, v := range dels {
			if v.Mnt != "" && v.Id >= 100 {
				exec.Command(quotaCmd, "-x", "-c",
					fmt.Sprintf("limit -p bsoft=0 bhard=0 %d", v.Id),
					v.Mnt).CombinedOutput()
				exec.Command(quotaCmd, "-xc",
					fmt.Sprintf("project -C %d", v.Id),
					v.Mnt).CombinedOutput()
			}
			slog.Info("xfsquota: removing stale entry",
				"container", v.Name, "project_id", v.Id)
			quotaConfig.Remove(v.Name)
		}
		if len(dels) > 0 {
			if err := quotaConfig.SyncVendor(); err != nil {
				return err
			}
		}
	}

	if err := quotaConfig.Sync(); err != nil {
		return err
	}

	// Phase 3: Create/update quotas for active app instances
	hoststatus.ActiveAppList.Range(func(key, value any) bool {
		app, ok := value.(*inapi.AppInstance)
		if !ok || app.Spec == nil || app.Deploy == nil || len(app.Deploy.Replicas) == 0 {
			return true
		}

		if app.Deploy.Action != inapi.OpActionStart {
			return true
		}

		for _, rep := range app.Deploy.Replicas {
			if rep.HostId == "" || rep.HostId != config.Config.Hostlet.HostId {
				continue
			}

			repInstance := &inapi.AppReplicaInstance{
				App:     app,
				Replica: rep,
			}

			ctrName := repInstance.ContainerName()

			// Verify the container base directory exists
			appPaths := hostapi.NewContainerPath(config.Config.Hostlet.AppPath, ctrName)
			ctrDir := appPaths.ContainerBaseDir()
			if _, err := os.Stat(ctrDir); err != nil {
				continue
			}

			// Get or create a quota project for this container
			proj := quotaConfig.FetchOrCreate("", ctrName)
			if proj == nil {
				slog.Error("xfsquota: failed to allocate project", "container", ctrName)
				continue
			}

			volLimit := max(app.Deploy.VolumeLimit, 100<<20)
			// Skip if the project already has limits within 1% of the target.
			// This avoids re-initializing every refresh cycle.
			softDiff := float64(proj.Soft-volLimit) / float64(volLimit)
			hardDiff := float64(proj.Hard-volLimit) / float64(volLimit)
			if softDiff > -0.01 && softDiff < 0.01 &&
				hardDiff > -0.01 && hardDiff < 0.01 {
				continue
			}

			// Sync /etc/projects mapping file
			if err := quotaConfig.SyncVendor(); err != nil {
				slog.Warn("xfsquota: SyncVendor failed", "error", err)
				continue
			}

			// Initialize the project: assign container base directory to project ID
			args := []string{
				quotaCmd,
				"-x",
				"-c",
				fmt.Sprintf("project -s -p %s %d", ctrDir, proj.Id),
				proj.Mnt,
			}
			if out, err := exec.Command("sh", "-c", strings.Join(args, " ")+"\nexit 0\n").CombinedOutput(); err != nil {
				slog.Warn("xfsquota: project init failed",
					"container", ctrName, "error", err, "output", string(out))
				continue
			} else {
				slog.Info("xfsquota: project initialized",
					"container", ctrName, "project_id", proj.Id, "path", ctrDir)
			}

			// Set the quota limits (soft == hard)
			args = []string{
				quotaCmd,
				"-x",
				"-c",
				fmt.Sprintf("\"limit -p bsoft=%d bhard=%d %d\"", volLimit, volLimit, proj.Id),
				proj.Mnt,
			}
			if out, err := exec.Command("sh", "-c", strings.Join(args, " ")+"\nexit 0\n").CombinedOutput(); err != nil {
				slog.Warn("xfsquota: limit set failed",
					"container", ctrName, "error", err, "output", string(out))
				continue
			}
			slog.Info("xfsquota: limit set",
				"container", ctrName, "project_id", proj.Id,
				"soft", volLimit, "hard", volLimit,
				"args", strings.Join(args, " "))

			// Update in-memory state so next refresh cycle skips
			proj.Soft = volLimit
			proj.Hard = volLimit
		}

		return true
	})

	if err := quotaConfig.Sync(); err != nil {
		return err
	}

	// Phase 4: Clean up quotas for inactive v2 containers (zero out limits).
	// v1 entries are never modified by v2 code.
	for _, v := range quotaConfig.Items {
		if v.Soft < 1 || !quotaIsV2Name(v.Name) {
			continue
		}

		// Check if this container is still active
		ctrName := v.Name
		active := false

		hoststatus.ActiveAppList.Range(func(key, value any) bool {
			app, ok := value.(*inapi.AppInstance)
			if !ok || app.Deploy == nil {
				return true
			}

			if app.Deploy.Action != inapi.OpActionStart {
				return true
			}

			for _, rep := range app.Deploy.Replicas {
				if rep.HostId != config.Config.Hostlet.HostId {
					continue
				}
				repInstance := &inapi.AppReplicaInstance{
					App:     app,
					Replica: rep,
				}
				if repInstance.ContainerName() == ctrName {
					active = true
					return false
				}
			}
			return true
		})

		if active {
			continue
		}

		// Zero out quota limits for inactive container
		args := []string{
			"-x",
			"-c",
			fmt.Sprintf("limit -p bsoft=0 bhard=0 %d", v.Id),
			v.Mnt,
		}
		if _, err := exec.Command(quotaCmd, args...).CombinedOutput(); err != nil {
			slog.Warn("xfsquota: clean quota limit failed",
				"container", v.Name, "error", err)
			return err
		}

		// Clear project directory mapping
		args = []string{
			"-xc",
			fmt.Sprintf("project -C %d", v.Id),
			v.Mnt,
		}
		if _, err := exec.Command(quotaCmd, args...).CombinedOutput(); err != nil {
			slog.Warn("xfsquota: clean project failed",
				"container", v.Name, "error", err)
			return err
		}

		slog.Info("xfsquota: cleaned up project",
			"container", v.Name, "project_id", v.Id)
	}

	quotaRefreshed = tn
	return nil
}

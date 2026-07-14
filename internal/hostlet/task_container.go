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
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/sysinner/innerstack/v2/internal/config"
	"github.com/sysinner/innerstack/v2/internal/hostlet/docker"
	"github.com/sysinner/innerstack/v2/internal/hostlet/hostapi"
	"github.com/sysinner/innerstack/v2/internal/hostlet/hoststatus"
	"github.com/sysinner/innerstack/v2/internal/inutil"
	"github.com/sysinner/innerstack/v2/pkg/inapi"
)

// hostSrcPaths returns source file paths based on the current config prefix.
// Must be called after config.Setup() to get the correct installation prefix.
func hostSrcPaths() *hostapi.HostSourcePaths {
	return hostapi.NewHostSourcePaths(config.Prefix)
}

// lxcfsBinPaths defines lxcfs binary path and its corresponding proc directory.
var lxcfsBinPaths = [][]string{
	{"/usr/bin/lxcfs", "/var/lib/lxcfs/proc/"},
}

// lxcfsProcEntries lists /proc files that lxcfs overrides for container isolation.
var lxcfsProcEntries = []string{
	"cpuinfo", "diskstats", "meminfo", "stat", "swaps", "uptime", "slabinfo",
}

// lxcfsSysEntries lists /sys paths that lxcfs overrides for container isolation.
var lxcfsSysEntries = [][]string{
	{"/var/lib/lxcfs/sys/devices/system/cpu", "/sys/devices/system/cpu"},
}

const (
	defaultContainerTimeout = 30 * time.Second
	imagePullTimeout        = 5 * time.Minute
)

var (
	ctrDriver    hostapi.Driver
	ctrDrivers   []hostapi.Driver
	lxcfsEnabled atomic.Bool
)

// Workflow State Machine:
//
//	                   +----------+
//	                   |  empty   |
//	                   +----+-----+
//	                        | start
//	                        v
//	                   +----------+
//	         +---------| starting |<-------+
//	         |         +----+-----+         |
//	         |              |               |
//	         | start        | success       | stop
//	         |              v               |
//	         |         +----+----+          |
//	 +-------+---------| running |----------+
//	 |       |         +----+----+          |
//	 |       |              |               |
//	 |       |              | stop          |
//	 |       |              v               |
//	 |       |         +----+----+          |
//	 |       +-------->| stopping|----------+
//	 |                 +----+----+          |
//	 |                      |               |
//	 | destroy              | success       | destroy
//	 |                      v               |
//	 |                 +----+----+          |
//	 |        +--------| stopped |----------+
//	 |        |        +----+----+          |
//	 |        |             |               |
//	 |        |             | destroy       |
//	 |        |             v               |
//	 |        |        +----------+         |
//	 |        +------->|destroying|<--------+
//	 |                 +----+-----+
//	 |                      |
//	 |                      | success
//	 |                      v
//	 |                 +----------+
//	 +---------------->|destroyed |
//	                   +----------+
//
//	Event transitions (on success/fail):
//	- starting  + success -> running
//	- starting  + fail    -> failed
//	- stopping  + success -> stopped
//	- stopping  + fail    -> failed
//	- destroying+ success -> destroyed
//	- destroying+ fail    -> failed
func taskContainerInit() {
	// User action: start
	hoststatus.AppWorkflow.Register(
		inapi.OpStateEmpty, inapi.OpActionStart, inapi.OpStateStarting,
		operateContainerStarting)
	hoststatus.AppWorkflow.Register(
		inapi.OpStateStopped, inapi.OpActionStart, inapi.OpStateStarting,
		operateContainerStarting)
	hoststatus.AppWorkflow.Register(
		inapi.OpStateFailed, inapi.OpActionStart, inapi.OpStateStarting,
		operateContainerStarting)
	// Re-attempt starting if previous start was interrupted
	hoststatus.AppWorkflow.Register(
		inapi.OpStateStarting, inapi.OpActionStart, inapi.OpStateStarting,
		operateContainerStarting)
	// Already in target state - no action needed
	hoststatus.AppWorkflow.Register(
		inapi.OpStateRunning, inapi.OpActionStart, inapi.OpStateRunning, nil)

	// User action: stop
	hoststatus.AppWorkflow.Register(
		inapi.OpStateRunning, inapi.OpActionStop, inapi.OpStateStopping,
		operateContainerStopping)
	hoststatus.AppWorkflow.Register(
		inapi.OpStateStarting, inapi.OpActionStop, inapi.OpStateStopping,
		operateContainerStopping)
	// Already in target state - no action needed
	hoststatus.AppWorkflow.Register(
		inapi.OpStateStopped, inapi.OpActionStop, inapi.OpStateStopped, nil)
	hoststatus.AppWorkflow.Register(
		inapi.OpStateEmpty, inapi.OpActionStop, inapi.OpStateStopped, nil)

	// User action: destroy
	hoststatus.AppWorkflow.Register(
		inapi.OpStateRunning, inapi.OpActionDestroy, inapi.OpStateDestroying,
		operateContainerDestroying)
	hoststatus.AppWorkflow.Register(
		inapi.OpStateStopped, inapi.OpActionDestroy, inapi.OpStateDestroying,
		operateContainerDestroying)
	hoststatus.AppWorkflow.Register(
		inapi.OpStateStopping, inapi.OpActionDestroy, inapi.OpStateDestroying,
		operateContainerDestroying)
	hoststatus.AppWorkflow.Register(
		inapi.OpStateFailed, inapi.OpActionDestroy, inapi.OpStateDestroying,
		operateContainerDestroying)
	// Already in target state - no action needed
	hoststatus.AppWorkflow.Register(
		inapi.OpStateDestroyed, inapi.OpActionDestroy, inapi.OpStateDestroyed, nil)

	// Event: success
	hoststatus.AppWorkflow.Register(
		inapi.OpStateStarting, inapi.OpEventSuccess, inapi.OpStateRunning, nil)
	hoststatus.AppWorkflow.Register(
		inapi.OpStateStopping, inapi.OpEventSuccess, inapi.OpStateStopped, nil)
	hoststatus.AppWorkflow.Register(
		inapi.OpStateDestroying, inapi.OpEventSuccess, inapi.OpStateDestroyed, nil)

	// Event: fail
	hoststatus.AppWorkflow.Register(
		inapi.OpStateStarting, inapi.OpEventFail, inapi.OpStateFailed, nil)
	hoststatus.AppWorkflow.Register(
		inapi.OpStateStopping, inapi.OpEventFail, inapi.OpStateFailed, nil)
	hoststatus.AppWorkflow.Register(
		inapi.OpStateDestroying, inapi.OpEventFail, inapi.OpStateFailed, nil)
}

// containerRefresh is the main refresh loop for container management.
//
// # Architecture Overview
//
// The container-server (docker, containerd, etc.) runs as an independent system.
// Hostlet communicates with it via driver API to manage container lifecycle.
//
// Refresh Steps
//
//  1. Status Refresh (containerStatusRefresh)
//     - Periodically fetches container-server status
//     - Updates local cache of images and containers
//
//  2. Control Refresh (containerControlRefresh)
//     - Checks desired vs actual state for each app replica
//     - Computes state diff and executes workflow commands
//
// # Distributed System Considerations
//
// In distributed environments, batch-fetched container states may experience:
//   - Transient state fluctuations
//   - Temporary inconsistencies
//   - Network-induced anomalies
//
// Therefore, containerControlRefresh must handle:
//   - Idempotent operations (safe to retry)
//   - Robust error handling
//   - Fault tolerance for edge cases
func containerRefresh() error {
	if err := containerStatusRefresh(); err != nil {
		slog.Error("hostlet", "err", err.Error())
		return err
	} else if err = containerControlRefresh(); err != nil {
		slog.Error("hostlet", "err", err.Error())
		return err
	}
	containerOrphanSweep()
	return nil
}

// lxcfsDetect checks if an lxcfs binary is running on the host.
// It sets lxcfsEnabled to true if config enables it and the process is detected.
func lxcfsDetect() {
	if !config.Config.Hostlet.LxcFsEnable || lxcfsEnabled.Load() {
		return
	}
	for _, vp := range lxcfsBinPaths {
		out, err := exec.Command("pidof", vp[0]).Output()
		if err != nil {
			continue
		}
		if pid, _ := strconv.Atoi(strings.TrimSpace(string(out))); pid > 0 {
			lxcfsEnabled.Store(true)
			slog.Info("lxcfs detected", "binary", vp[0])
			return
		}
	}
}

// lxcfsMounts returns volume mounts for lxcfs proc entries if lxcfs is enabled.
// It detects the running lxcfs binary and generates read-only mounts for each
// /proc entry (cpuinfo, meminfo, etc.) to provide container-level resource isolation.
func lxcfsMounts() []hostapi.MountBind {
	if !lxcfsEnabled.Load() {
		return nil
	}
	for _, vp := range lxcfsBinPaths {
		out, err := exec.Command("pidof", vp[0]).Output()
		if err != nil {
			continue
		}
		if pid, _ := strconv.Atoi(strings.TrimSpace(string(out))); pid > 0 {
			mounts := make([]hostapi.MountBind, 0, len(lxcfsProcEntries)+len(lxcfsSysEntries))
			for _, entry := range lxcfsProcEntries {
				mounts = append(mounts, hostapi.MountBind{
					HostPath:      vp[1] + entry,
					ContainerPath: "/proc/" + entry,
					ReadOnly:      false,
				})
			}
			for _, entry := range lxcfsSysEntries {
				mounts = append(mounts, hostapi.MountBind{
					HostPath:      entry[0],
					ContainerPath: entry[1],
					ReadOnly:      false,
				})
			}
			return mounts
		}
	}
	// Process was previously detected but is no longer running
	lxcfsEnabled.Store(false)
	return nil
}

// containerStatusRefresh initializes driver and refreshes container/image cache.
func containerStatusRefresh() error {
	// Detect lxcfs availability on each refresh cycle
	lxcfsDetect()

	ctx, cancel := context.WithTimeout(context.Background(), defaultContainerTimeout)
	defer cancel()

	// Initialize driver
	if ctrDriver == nil {
		driver, err := docker.NewDriver()
		if err != nil {
			slog.Warn("container driver init failed", "error", err)
			return err
		}
		ctrDriver = driver
		ctrDrivers = append(ctrDrivers, driver)
	}

	// Check service availability
	if err := ctrDriver.Ping(ctx); err != nil {
		slog.Warn("container service unavailable", "error", err)
		hoststatus.StatusSet.Delete(ctrDriver.Name())
		hoststatus.ImageList.Clear()
		hoststatus.ContainerList.Clear()
		return err
	}

	// Get driver info
	info, err := ctrDriver.Info(ctx)
	if err != nil {
		slog.Warn("container info fetch failed", "error", err)
		return err
	}
	hoststatus.StatusSet.Store(ctrDriver.Name(), info)

	// Refresh image cache
	images, err := ctrDriver.ImageList(ctx)
	if err != nil {
		slog.Warn("image list fetch failed", "error", err)
		return err
	}
	hoststatus.ImageList.Clear()
	for _, img := range images {
		hoststatus.ImageList.Store(img.Name, img)
	}

	// Refresh container cache
	containers, err := ctrDriver.ContainerList(ctx)
	if err != nil {
		slog.Warn("container list fetch failed", "error", err)
		return err
	}
	for _, ctr := range containers {
		if !hostapi.ContainerNameValid.MatchString(ctr.Name) {
			continue
		}
		if prev, ok := hoststatus.ContainerList.Load(ctr.Name); !ok {
			hoststatus.ContainerList.Store(ctr.Name, ctr)
		} else if pctr, ok := prev.(*hostapi.ContainerInfo); ok {
			pctr.Started = ctr.Started
			pctr.State = ctr.State
			pctr.Image = ctr.Image
			pctr.IP = ctr.IP
		}
	}
	hoststatus.ContainerReady.Store(true)

	return nil
}

var (
	repInstances hostapi.AppReplicaInstanceList
)

// containerControlRefresh processes app replica state transitions via workflow.
func containerControlRefresh() error {
	if !hoststatus.HostReady.Load() ||
		!hoststatus.ContainerReady.Load() ||
		ctrDriver == nil {
		return nil
	}

	hoststatus.ActiveAppList.Range(func(key, value any) bool {
		app, ok := value.(*inapi.AppInstance)
		if !ok || app.Deploy == nil {
			return true
		}

		// Soft delete: tear the container(s) down, archive the data dir, and
		// record the instance so it is skipped on subsequent syncs. This is a
		// terminal action handled outside the start/stop/destroy workflow.
		if app.Deploy.Action == inapi.OpActionDelete {
			containerDeleteRefresh(app)
			return true
		}

		if app.Spec == nil ||
			app.Spec.Resources == nil ||
			len(app.Deploy.Replicas) == 0 {
			return true
		}

		if app.Deploy.Action == "" {
			app.Deploy.Action = inapi.OpActionStart
		}

		for _, rep := range app.Deploy.Replicas {
			//
			if rep.HostId == "" || rep.HostId != config.Config.Hostlet.HostId {
				continue
			}

			//
			repInstance := &inapi.AppReplicaInstance{
				App:            app,
				Replica:        rep,
				ZoneBaseDomain: zoneNetworkMap.VpcNetworkDomain,
			}

			// Mark host-receive on first sight of an assigned replica.
			re := hoststatus.ReplicaStage(app.InstanceName(), rep.Id)
			// Align with the current deploy revision; a new revision clears
			// stale stage progress from a prior deploy.
			re.SyncRevision(app.Deploy.Revision)
			if re.Stage.Find(inapi.AppDeployStageNameHostRecv) == nil {
				re.SetInstant(inapi.AppDeployStageNameHostRecv, "")
			}

			if runtime.GOOS != "linux" {
				repInstance.Replica.VpcIpv4 = ""

				for _, dep := range repInstance.App.Deploy.Depends {
					for _, rep := range dep.Replicas {
						rep.VpcIpv4 = ""
					}
				}
			}

			// Sync actual container state to replica
			currentState := containerStateSync(repInstance)
			if currentState == "" {
				currentState = inapi.OpStateEmpty
				repInstance.Replica.State = currentState
			}

			//
			if containerSpecReset(repInstance) {
				slog.Info(fmt.Sprintf("container spec reset, app %s",
					repInstance.ContainerName()))
				if _, err := operateContainerDestroying(repInstance); err != nil {
					slog.Warn("container remove failed for resource update",
						"container", repInstance.ContainerName(), "error", err)
					continue
				}
				// The container has been removed; the actual state is now
				// empty. Update currentState so the workflow proceeds to
				// create+start in this same tick. Without this the stale
				// pre-destroy state (e.g. "running") maps to a no-op
				// transition and the recreate is delayed to the next tick.
				currentState = inapi.OpStateEmpty
				repInstance.Replica.State = currentState
			}

			// Get next command from workflow state machine
			cmd, ok := hoststatus.AppWorkflow.NextCommand(currentState, app.Deploy.Action)
			if !ok || cmd == nil {
				continue
			}

			// If command is nil, state is already at target (no action needed)
			// Just update the state and continue
			if cmd.Command == nil {
				// Sync app_replica.json to running container when AppSpec changes
				// but no container spec reset is required
				containerAppInstanceSync(repInstance)
				continue
			}

			repInstance.Replica.State = cmd.State // pendding

			// Execute state transition command
			nextState, err := cmd.Command(repInstance)
			if err != nil {
				slog.Warn("app deploy command failed",
					"app", app.InstanceName(),
					"replica", rep.Id,
					"state", currentState,
					"action", app.Deploy.Action,
					"next_state", nextState,
					"error", err)
			} else {
				slog.Info("app deploy command succeeded",
					"app", app.InstanceName(),
					"replica", rep.Id,
					"state", currentState,
					"action", app.Deploy.Action,
					"next_state", nextState)
			}
			if nextState != "" {
				repInstance.Replica.State = nextState
			}

			// Record terminal host-side stages from the resolved next state.
			switch nextState {
			case inapi.OpStateRunning:
				re.SetInstant(inapi.AppDeployStageNameContainerRunning, "")
			case inapi.OpStateStopped:
				re.SetInstant(inapi.AppDeployStageNameContainerStop, "")
			case inapi.OpStateDestroyed:
				re.SetInstant(inapi.AppDeployStageNameContainerDestroy, "")
			}
		}
		return true
	})

	return nil
}

// containerStateSync syncs container actual state to replica state.
// State is already mapped to inapi.OpState* by docker driver.
func containerStateSync(rep *inapi.AppReplicaInstance) string {
	ctrInfo, exists := hoststatus.ContainerList.Load(rep.ContainerName())
	if !exists || ctrInfo == nil {
		return inapi.OpStateEmpty
	}

	info, ok := ctrInfo.(*hostapi.ContainerInfo)
	if !ok {
		return inapi.OpStateEmpty
	}

	if info.State != "" {
		rep.Replica.State = info.State
		return info.State
	}
	return inapi.OpStateEmpty
}

// containerHasLxcfsMounts checks whether the container's current binds include
// lxcfs proc entries by looking for "/proc/<entry>" container paths.
func containerHasLxcfsMounts(binds []string) bool {
	for _, bind := range binds {
		for _, entry := range lxcfsProcEntries {
			target := "/proc/" + entry
			// Bind format: host_path:container_path[:ro]
			parts := strings.SplitN(bind, ":", 3)
			if len(parts) >= 2 && parts[1] == target {
				return true
			}
		}
	}
	return false
}

// containerSpecReset checks if container spec differs from desired config.
// Returns true if container needs to be recreated.
//
// Checked specs:
//   - Image: must match exactly
//   - CpuLimit: tolerance 1% (millicores)
//   - MemoryLimit: tolerance 1% (bytes)
//   - ServicePorts: port mappings must match
//   - LxcfsMounts: must match current lxcfs enabled state
func containerSpecReset(rep *inapi.AppReplicaInstance) bool {

	ctrInfo, exists := hoststatus.ContainerList.Load(rep.ContainerName())
	if !exists || ctrInfo == nil {
		return false
	}

	info, ok := ctrInfo.(*hostapi.ContainerInfo)
	if !ok {
		return false
	}

	// A Deploy.Revision increment (issued by every successful app-deploy)
	// must force a full container recreate so the new spec, config and
	// dependencies take effect.
	//
	// The last-applied revision is taken from the on-disk app_replica.json
	// (written on every create/provision, always present for a running
	// container, survives hostlet restarts), which is the ground truth.
	// hoststatus.Active.AppliedRevisions is used only as an in-memory cache
	// for the steady-state fast path: when the cache is missing (hostlet
	// restart without persistence, or a container created before revision
	// tracking) or behind the desired revision, we consult the disk. This
	// both recovers the applied revision for bootstrap containers so a bump
	// is not silently ignored, and avoids a spurious recreate when the cache
	// is stale (persistence fell behind the on-disk file).
	if rep.App.Deploy != nil {
		want := rep.App.Deploy.Revision
		applied, hasApplied := hoststatus.Active.AppliedRevision(rep.ContainerName())
		if !hasApplied || want > applied {
			if diskRev, hasDisk := readAppliedRevisionFromDisk(rep); hasDisk {
				applied = diskRev
				hoststatus.Active.SetAppliedRevision(rep.ContainerName(), diskRev)
			}
			if want > applied {
				slog.Info("container spec reset: deploy revision changed",
					"container", rep.ContainerName(),
					"current", applied,
					"desired", want)
				return true
			}
		}
	}

	tn := time.Now().Unix()

	// Fetch detailed container info if resource limits are missing from cache
	if info.LastInspectTime+1800 < tn {
		ctx, cancel := context.WithTimeout(context.Background(), defaultContainerTimeout)
		if ifo, err := ctrDriver.ContainerInspect(ctx, rep.ContainerName()); err == nil {
			info.CpuLimit = ifo.CpuLimit
			info.MemoryLimit = ifo.MemoryLimit
			info.Ports = ifo.Ports
			info.IP = ifo.IP
			info.Binds = ifo.Binds
			info.LastInspectTime = tn
		}
		cancel()
	}

	// Helper: calculate relative difference (returns 0 if target is 0)
	relDiff := func(target, current int64) float64 {
		if target <= 0 {
			return 0
		}
		return math.Abs(float64(target-current)) / float64(target)
	}

	if rep.App.Spec.Resources != nil &&
		rep.App.Spec.Image != "" &&
		rep.App.Spec.Image != info.Image {
		slog.Info("container spec reset: image mismatch",
			"desired", rep.App.Spec.Image,
			"current", info.Image)
		return true
	}

	// Check 2: CpuLimit with 1% tolerance
	if relDiff(rep.App.Deploy.CpuLimit, info.CpuLimit) > 0.01 {
		slog.Info("container spec reset: cpu limit mismatch",
			"desired", rep.App.Deploy.CpuLimit,
			"current", info.CpuLimit)
		return true
	}

	// Check 3: MemoryLimit with 1% tolerance
	if relDiff(rep.App.Deploy.MemoryLimit, info.MemoryLimit) > 0.01 {
		slog.Info("container spec reset: memory limit mismatch",
			"desired", rep.App.Deploy.MemoryLimit,
			"current", info.MemoryLimit)
		return true
	}

	// Check 4: ServicePorts must match (using allocated host ports)
	if len(rep.Replica.ServicePorts) > 0 {
		for _, sp := range rep.Replica.ServicePorts {
			if sp == nil || sp.Port < 1 || sp.HostPort < 1 {
				continue
			}
			if binding, bound := info.Ports[int32(sp.Port)]; !bound {
				slog.Info("container spec reset: service port not bound",
					"desired_port", sp.Port,
					"bound_ports", info.Ports)
				return true
			} else if binding.HostPort != int32(sp.HostPort) {
				slog.Info("container spec reset: host port mismatch",
					"desired_host_port", sp.HostPort,
					"current_host_port", binding.HostPort)
				return true
			}
		}
	}

	if rep.Replica.VpcIpv4 != "" && rep.Replica.VpcIpv4 != info.IP {
		slog.Info("container spec reset: vpc_ipv4",
			"vpc_ipv4", rep.Replica.VpcIpv4,
			"container_ip", info.IP)
		return true
	}

	// Check lxcfs mount state: container must have lxcfs mounts when enabled,
	// and must not have them when disabled.
	hasLxcfsBinds := containerHasLxcfsMounts(info.Binds)
	wantLxcfsBinds := lxcfsEnabled.Load()
	if hasLxcfsBinds != wantLxcfsBinds {
		slog.Info("container spec reset: lxcfs mounts mismatch",
			"has_lxcfs_binds", hasLxcfsBinds,
			"want_lxcfs_binds", wantLxcfsBinds)
		return true
	}

	return false
}

// operateContainerStarting handles the start action for a replica.
// Returns the next state on success or failure.
func operateContainerStarting(rep *inapi.AppReplicaInstance) (string, error) {
	containerName := rep.ContainerName()

	// Check existing container state
	if ctrInfo, exists := hoststatus.ContainerList.Load(containerName); exists {
		if info, ok := ctrInfo.(*hostapi.ContainerInfo); ok {
			switch info.State {
			case inapi.OpStateRunning:
				// Check if resource limits mismatch desired config
				if containerSpecReset(rep) {
					slog.Info("container resource limits changed, recreating container",
						"container", containerName)
					// Remove and recreate with new resource limits
					ctx, cancel := context.WithTimeout(context.Background(), defaultContainerTimeout)
					if err := ctrDriver.ContainerRemove(ctx, containerName); err != nil {
						cancel()
						slog.Warn("container remove failed for resource update",
							"container", containerName, "error", err)
						return inapi.OpStateFailed, fmt.Errorf("container remove failed: %w", err)
					}
					cancel()
					hoststatus.ContainerList.Delete(containerName)
					// Fall through to create new container
				} else {
					return inapi.OpStateRunning, nil
				}
			case inapi.OpStateStopped:
				// Check if resource limits mismatch before starting
				if containerSpecReset(rep) {
					slog.Info("stopped container resource limits changed, recreating container",
						"container", containerName)
					ctx, cancel := context.WithTimeout(context.Background(), defaultContainerTimeout)
					if err := ctrDriver.ContainerRemove(ctx, containerName); err != nil {
						cancel()
						slog.Warn("container remove failed for resource update",
							"container", containerName, "error", err)
						return inapi.OpStateFailed, fmt.Errorf("container remove failed: %w", err)
					}
					cancel()
					hoststatus.ContainerList.Delete(containerName)
					// Fall through to create new container
				} else {
					// Refresh the bind-mounted .innerstack files so that
					// starting an already-initialized container picks up the
					// latest inagent/ininit/app_replica.json (e.g. after a
					// host-side inagent upgrade). Best-effort: the container
					// already holds valid files from a prior create, so on
					// failure we log and start with the existing files.
					if err := provisionInnerStack(rep); err != nil {
						slog.Warn("provision innerstack files failed on start, using existing files",
							"container", containerName, "error", err)
					}
					re := hoststatus.ReplicaStage(rep.App.InstanceName(), rep.Replica.Id)
					re.SetRunning(inapi.AppDeployStageNameContainerStart, "")
					ctx, cancel := context.WithTimeout(context.Background(), defaultContainerTimeout)
					if err := ctrDriver.ContainerStart(ctx, containerName); err != nil {
						cancel()
						// Remove and recreate
						ctx2, cancel2 := context.WithTimeout(context.Background(), defaultContainerTimeout)
						if err := ctrDriver.ContainerRemove(ctx2, containerName); err != nil {
							cancel2()
							slog.Warn("container remove failed", "container", containerName, "error", err)
							re.SetFailed(inapi.AppDeployStageNameContainerStart, err.Error())
							return inapi.OpStateFailed, fmt.Errorf("container remove failed: %w", err)
						}
						cancel2()
						hoststatus.ContainerList.Delete(containerName)
					} else {
						cancel()
						re.SetSuccess(inapi.AppDeployStageNameContainerStart, "")
						slog.Info("container started", "container", containerName)
						return inapi.OpStateRunning, nil
					}
				}
			}
		}
	}

	// Create container
	if err := containerCreate(rep); err != nil {
		return inapi.OpStateFailed, err
	}

	// Start container
	re := hoststatus.ReplicaStage(rep.App.InstanceName(), rep.Replica.Id)
	re.SetRunning(inapi.AppDeployStageNameContainerStart, "")
	ctx, cancel := context.WithTimeout(context.Background(), defaultContainerTimeout)
	if err := ctrDriver.ContainerStart(ctx, containerName); err != nil {
		cancel()
		slog.Warn("container start failed", "container", containerName, "error", err)
		re.SetFailed(inapi.AppDeployStageNameContainerStart, err.Error())
		return inapi.OpStateFailed, fmt.Errorf("container start failed: %w", err)
	}
	cancel()
	re.SetSuccess(inapi.AppDeployStageNameContainerStart, "")

	slog.Info("container started", "container", containerName)
	return inapi.OpStateRunning, nil
}

// operateContainerStopping handles the stop action for a replica.
// Returns the next state on success or failure.
func operateContainerStopping(rep *inapi.AppReplicaInstance) (string, error) {
	containerName := rep.ContainerName()

	ctrInfo, exists := hoststatus.ContainerList.Load(containerName)
	if !exists {
		return inapi.OpStateStopped, nil
	}

	info, ok := ctrInfo.(*hostapi.ContainerInfo)
	if !ok {
		return inapi.OpStateStopped, nil
	}

	if info.State == inapi.OpStateStopped {
		return inapi.OpStateStopped, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), defaultContainerTimeout)
	defer cancel()

	if err := ctrDriver.ContainerStop(ctx, containerName); err != nil {
		slog.Warn("container stop failed", "container", containerName, "error", err)
		return inapi.OpStateFailed, fmt.Errorf("container stop failed: %w", err)
	}

	slog.Info("container stopped", "container", containerName)
	return inapi.OpStateStopped, nil
}

// operateContainerDestroying handles the destroy action for a replica.
// Returns the next state on success or failure.
func operateContainerDestroying(rep *inapi.AppReplicaInstance) (string, error) {
	if err := destroyContainerByName(rep.ContainerName()); err != nil {
		return inapi.OpStateFailed, err
	}
	return inapi.OpStateDestroyed, nil
}

// destroyContainerByName stops and force-removes the named container, drops it
// from the local cache, and clears its XFS quota project. It is the shared
// teardown primitive used by both the destroy/soft-delete workflow (via
// operateContainerDestroying) and the orphan sweep. Idempotent: a container
// absent from the local cache is a no-op.
func destroyContainerByName(containerName string) error {
	ctrInfo, exists := hoststatus.ContainerList.Load(containerName)
	if !exists {
		return nil
	}

	info, ok := ctrInfo.(*hostapi.ContainerInfo)
	if !ok {
		hoststatus.ContainerList.Delete(containerName)
		return nil
	}

	// Stop container first if running
	if info.State == inapi.OpStateRunning {
		ctx, cancel := context.WithTimeout(context.Background(), defaultContainerTimeout)
		if err := ctrDriver.ContainerStop(ctx, containerName); err != nil {
			cancel()
			slog.Warn("container stop failed during destroy", "container", containerName, "error", err)
			// Continue to remove anyway
		}
		cancel()
	}

	// Remove container
	ctx, cancel := context.WithTimeout(context.Background(), defaultContainerTimeout)
	if err := ctrDriver.ContainerRemove(ctx, containerName); err != nil {
		cancel()
		slog.Warn("container remove failed", "container", containerName, "error", err)
		return fmt.Errorf("container remove failed: %w", err)
	}
	cancel()

	hoststatus.ContainerList.Delete(containerName)

	// Clean up XFS quota project for this container
	quotaCleanupContainer(containerName)

	slog.Info("container destroyed", "container", containerName)
	return nil
}

// containerDeleteRefresh tears down all local replicas of a soft-deleted
// instance (Deploy.Action == delete): stops and removes each container,
// archives its data directory, then records the instance as deleted so it is
// skipped on subsequent syncs. If any container removal fails the instance is
// left unrecorded so it is retried on the next tick.
func containerDeleteRefresh(app *inapi.AppInstance) {
	if _, done := hoststatus.Active.DeletedAt(app.InstanceId()); done {
		return // already torn down on this host
	}

	failed := false
	for _, rep := range app.Deploy.Replicas {
		if rep == nil || rep.HostId == "" ||
			rep.HostId != config.Config.Hostlet.HostId {
			continue
		}

		repInstance := &inapi.AppReplicaInstance{
			App:            app,
			Replica:        rep,
			ZoneBaseDomain: zoneNetworkMap.VpcNetworkDomain,
		}

		if _, err := operateContainerDestroying(repInstance); err != nil {
			slog.Warn("soft-delete container destroy failed",
				"app", app.InstanceName(),
				"replica", rep.Id,
				"error", err)
			failed = true
			continue
		}

		archiveContainerDir(repInstance)
	}

	if !failed {
		hoststatus.Active.MarkDeleted(app.InstanceId(), time.Now().Unix())
		saveHostActiveConfig()
		slog.Info("soft-delete instance torn down", "app", app.InstanceName())
	}
}

// archiveContainerDir moves a replica's data directory under the "deleted"
// subtree so a torn-down instance's data is retained rather than clobbered.
func archiveContainerDir(rep *inapi.AppReplicaInstance) {
	if rep == nil {
		return
	}
	archiveContainerDirByName(rep.ContainerName())
}

// archiveContainerDirByName moves a container's data directory under the
// "deleted" subtree with a datetime suffix
// (AppPath/<container> -> AppPath/deleted/<container>_<datetime>) so a
// torn-down instance's data is retained and repeated teardowns across
// re-deploy/delete cycles never collide. Used by both the leader-driven
// soft-delete flow (containerDeleteRefresh) and the hostlet orphan sweep.
// Best-effort: a missing source (already archived) is a no-op; a rename failure
// is logged but does not block the teardown since the container has already
// been removed.
func archiveContainerDirByName(containerName string) {
	appBasePath := config.Config.Hostlet.AppPath
	if appBasePath == "" || containerName == "" {
		return
	}

	src := filepath.Join(appBasePath, containerName)
	if _, err := os.Stat(src); err != nil {
		return // nothing to archive (already gone)
	}

	dst := filepath.Join(appBasePath, "deleted",
		fmt.Sprintf("%s_%s", containerName, time.Now().Format("20060102-150405")))
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		slog.Warn("archive mkdir failed",
			"dir", filepath.Dir(dst), "error", err)
		return
	}
	// The datetime suffix normally makes the destination unique; RemoveAll keeps
	// the rename idempotent if the same second is ever reused.
	_ = os.RemoveAll(dst)
	if err := os.Rename(src, dst); err != nil {
		slog.Warn("archive rename failed",
			"src", src, "dst", dst, "error", err)
		return
	}
	slog.Info("archived container dir", "src", src, "dst", dst)
}

// orphanQuarantine stops a freshly-detected orphan container so it no longer
// consumes resources, flipping its restart policy to "no" first so the stop is
// not undone by a Docker daemon restart during the grace window. Both steps are
// best-effort: the container is recorded as an orphan regardless, and a failure
// here is retried on the next tick (the entry stays stopped-or-tracked).
func orphanQuarantine(containerName string) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultContainerTimeout)
	defer cancel()
	if err := ctrDriver.ContainerUpdateRestartPolicy(ctx, containerName, inapi.OpRestartPolicyNo); err != nil {
		slog.Warn("orphan restart-policy flip failed",
			"container", containerName, "error", err)
	}
	if err := ctrDriver.ContainerStop(ctx, containerName); err != nil {
		slog.Warn("orphan container stop failed",
			"container", containerName, "error", err)
	}
}

// orphanRestoreRestartPolicy restores an orphaned container's restart policy to
// "always" when it returns to the desired set, so normal auto-restart behavior
// resumes after the reconcile loop restarts it. Best-effort; a container that
// vanished (removed externally) is a no-op via the driver's "no such container"
// handling.
func orphanRestoreRestartPolicy(containerName string) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultContainerTimeout)
	defer cancel()
	if err := ctrDriver.ContainerUpdateRestartPolicy(ctx, containerName, inapi.OpRestartPolicyAlways); err != nil {
		slog.Warn("orphan restart-policy restore failed",
			"container", containerName, "error", err)
	}
}

// orphanStop idempotently stops a tracked orphan that is somehow running again
// (e.g. started manually), keeping it quarantined during the grace window.
func orphanStop(containerName string) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultContainerTimeout)
	defer cancel()
	if err := ctrDriver.ContainerStop(ctx, containerName); err != nil {
		slog.Warn("orphan container re-stop failed",
			"container", containerName, "error", err)
	}
}

// containerOrphanSweep reconciles the reverse direction the main loop does not:
// it finds local containers that no longer appear in the zonelet's fresh desired
// app list and quarantines them. This is the hostlet-local, leader-absence-driven
// cleanup flow and is deliberately separate from the leader-driven soft-delete
// flow (containerDeleteRefresh), which runs while the instance is still
// delivered with Action==delete.
//
// Safety is layered (do not accidentally stop or remove a legitimate container):
//
//  1. Freshness gate: the last HostStatusUpdate must have succeeded recently
//     (OrphanSyncStaleLimit). A failed or stale zonelet response suspends all
//     orphan action.
//  2. Empty-Desired brake: an empty fresh desired set is indistinguishable from
//     a transiently-stale empty response and gives no basis to declare any
//     container an orphan, so the sweep refuses to act.
//  3. Only i8k_{name}_{rep} names are considered.
//  4. On detection the orphan is quarantined (restart policy -> "no", then
//     stopped). This is reversible: if the leader resumes delivering the
//     instance, the reconcile loop restarts it and the sweep restores the
//     policy; no data is lost.
//  5. The container is removed and its data archived only after
//     OrphanContainerGracePeriod of continuous absence.
//
// Orphan detection keys off the fresh Desired snapshot rather than ActiveAppList
// (which is append-only and would never report an absence). The sweep runs after
// containerControlRefresh each tick, so anything the reconcile loop manages is
// in Desired and is skipped here.
func containerOrphanSweep() {
	if ctrDriver == nil {
		return
	}

	now := time.Now().Unix()

	// Suspend on a failed or stale zonelet response.
	if !hoststatus.Desired.IsFresh(now, inapi.OrphanSyncStaleLimit) {
		return
	}

	// Panic-brake: refuse to make orphan decisions from an empty desired set.
	if hoststatus.Desired.Empty() {
		if hoststatus.ContainerList.Len() > 0 {
			slog.Warn("orphan sweep suspended: desired app list empty but local containers exist",
				"local_containers", hoststatus.ContainerList.Len())
		}
		return
	}

	dirty := false

	// Recovery pass: orphans that returned to the desired set restore their
	// restart policy and clear the tracking record. The reconcile loop (run
	// earlier this tick) restarts the container.
	for name := range hoststatus.Active.Orphans() {
		if hoststatus.Desired.Contains(name) {
			orphanRestoreRestartPolicy(name)
			hoststatus.Active.ClearOrphan(name)
			dirty = true
			slog.Info("orphan container returned to desired set", "container", name)
		}
	}

	// Detect/reap pass over the local containers.
	hoststatus.ContainerList.Range(func(key, value any) bool {
		name, ok := key.(string)
		if !ok || !hostapi.ContainerNameValid.MatchString(name) {
			return true
		}
		if hoststatus.Desired.Contains(name) {
			return true // managed by the reconcile loop
		}

		firstSeen, existed := hoststatus.Active.OrphanFirstSeen(name)

		if !existed {
			// First detection: record first-seen and quarantine.
			hoststatus.Active.MarkOrphan(name, now)
			firstSeen = now
			dirty = true
			orphanQuarantine(name)
			slog.Warn("orphan container detected: quarantined (stopped)",
				"container", name,
				"grace_seconds", inapi.OrphanContainerGracePeriod)
		} else if info, ok := value.(*hostapi.ContainerInfo); ok &&
			info.State == inapi.OpStateRunning {
			// Already tracked but running again: keep it quarantined.
			orphanStop(name)
		}

		// Grace elapsed: remove the container and archive its data directory.
		if now-firstSeen >= inapi.OrphanContainerGracePeriod {
			if err := destroyContainerByName(name); err != nil {
				slog.Warn("orphan container remove failed",
					"container", name, "error", err)
			} else {
				archiveContainerDirByName(name)
				hoststatus.Active.ClearOrphan(name)
				dirty = true
				slog.Warn("orphan container removed and archived",
					"container", name)
			}
		}
		return true
	})

	if dirty {
		saveHostActiveConfig()
	}
}

// containerAppInstanceSync checks if the on-disk app_replica.json differs from
// the current in-memory AppReplicaInstance and updates it if needed.
// This ensures that changes to AppSpec are propagated into running containers
// without requiring a container restart.
//
// The file is bind-mounted from host to container, so writing to the host path
// is immediately visible inside the container.
func containerAppInstanceSync(rep *inapi.AppReplicaInstance) {

	if !repInstances.TryStore(rep) {
		return
	}

	appBasePath := config.Config.Hostlet.AppPath
	if appBasePath == "" {
		return
	}

	appPaths := hostapi.NewContainerPath(appBasePath, rep.ContainerName())
	appInstancePath := appPaths.AppReplicaFile()

	ensureHostletEndpoint(rep)

	// Read existing file content
	existingData, err := os.ReadFile(appInstancePath)
	if err != nil {
		// File does not exist yet, write it
		if os.IsNotExist(err) {
			if writeErr := inutil.JsonEncodeToFileIndent(appInstancePath, rep, 0644); writeErr != nil {
				slog.Warn("app_replica.json create failed",
					"path", appInstancePath, "err", writeErr.Error())
			}
		}
		return
	}

	// Encode current in-memory state
	newData, err := json.Marshal(rep)
	if err != nil {
		slog.Warn("app_replica.json marshal failed", "err", err.Error())
		return
	}

	// Compare: only write if content changed to minimize unnecessary disk I/O
	if string(existingData) == string(newData) {
		return
	}

	if err := inutil.JsonEncodeToFileIndent(appInstancePath, rep, 0644); err != nil {
		slog.Warn("app_replica.json update failed",
			"path", appInstancePath, "err", err.Error())
		return
	}

	slog.Info("app_replica.json updated for AppSpec sync",
		"app", rep.App.InstanceName(),
		"container", rep.ContainerName(),
		"path", appInstancePath)
}

// provisionInnerStack writes the bind-mounted .innerstack files for a
// replica: app_replica.json, the ininit entrypoint script and the inagent
// binary. It is invoked on every start so that an already-initialized
// .innerstack mount is refreshed with the latest inagent/ininit/config
// (including after a host-side inagent upgrade). Idempotent: existing
// files are overwritten in place.
//
// ininit and inagent are required by the container entrypoint; failure to
// write them is fatal. app_replica.json failure is non-fatal (warned).
func provisionInnerStack(rep *inapi.AppReplicaInstance) error {
	appBasePath := config.Config.Hostlet.AppPath
	if appBasePath == "" {
		return nil
	}

	appPaths := hostapi.NewContainerPath(appBasePath, rep.ContainerName())

	innerStackPath := appPaths.InnerStackDir()
	if err := os.MkdirAll(innerStackPath, 0755); err != nil {
		return fmt.Errorf("[provisionInnerStack] mkdir innerstack failed: %w", err)
	}

	// app_replica.json: non-fatal on failure.
	appInstancePath := appPaths.AppReplicaFile()
	ensureHostletEndpoint(rep)
	if err := inutil.JsonEncodeToFileIndent(appInstancePath, rep, 0644); err != nil {
		slog.Warn("app_replica.json create failed",
			"path", appInstancePath, "err", err.Error())
	} else {
		slog.Debug("app_replica.json created", "path", appInstancePath)
	}

	// ininit script (embedded). Required by the container entrypoint.
	ininitPath := appPaths.IninitFile()
	if err := os.WriteFile(ininitPath, ininitScript, 0755); err != nil {
		return fmt.Errorf("[provisionInnerStack] write ininit script failed: %w", err)
	}
	slog.Debug("ininit script written", "path", ininitPath)

	// inagent binary: default to the Go build; use the C++ slim build when
	// enabled, falling back to the Go build if the slim binary is absent.
	arch := "amd64"
	if info, ok := hoststatus.StatusSet.Load("docker"); ok {
		if driverInfo, ok := info.(*hostapi.DriverInfo); ok && driverInfo.Arch != "" {
			arch = driverInfo.Arch
		}
	}
	srcPaths := hostSrcPaths()
	inagentPath := appPaths.InagentFile()

	var srcInagentPath string
	if config.Config.Hostlet.InagentSlimEnable {
		srcInagentPath = srcPaths.InagentSlimSrc(arch)
		if _, err := os.Stat(srcInagentPath); err != nil {
			slog.Warn("inagent-slim binary not found, falling back to Go inagent",
				"path", srcInagentPath, "err", err.Error())
			srcInagentPath = srcPaths.InagentSrc(arch)
		}
	} else {
		srcInagentPath = srcPaths.InagentSrc(arch)
	}
	if _, err := os.Stat(srcInagentPath); err != nil {
		return fmt.Errorf("[provisionInnerStack] inagent source binary not found at %s: %w", srcInagentPath, err)
	}
	if _, err := exec.Command("install", srcInagentPath, inagentPath).Output(); err != nil {
		return fmt.Errorf("[provisionInnerStack] copy inagent binary failed: %w", err)
	}
	slog.Debug("inagent binary copied",
		"src", srcInagentPath, "path", inagentPath, "arch", arch)

	return nil
}

// readAppliedRevisionFromDisk reads the Deploy.Revision recorded in the
// on-disk app_replica.json for the replica. That file is written on every
// create/provision, so its revision is the ground truth of the last-applied
// deploy. Returns (0, false) if the file is absent or unreadable.
func readAppliedRevisionFromDisk(rep *inapi.AppReplicaInstance) (uint64, bool) {
	appBasePath := config.Config.Hostlet.AppPath
	if appBasePath == "" {
		return 0, false
	}
	appPaths := hostapi.NewContainerPath(appBasePath, rep.ContainerName())
	data, err := os.ReadFile(appPaths.AppReplicaFile())
	if err != nil {
		return 0, false
	}
	var stored inapi.AppReplicaInstance
	if err := json.Unmarshal(data, &stored); err != nil {
		return 0, false
	}
	if stored.App == nil || stored.App.Deploy == nil {
		return 0, false
	}
	return stored.App.Deploy.Revision, true
}

// containerCreate creates a new container for the given replica.
func containerCreate(rep *inapi.AppReplicaInstance) error {
	containerName := rep.ContainerName()

	if rep.App.Spec.Resources == nil {
		return fmt.Errorf("app spec resources is nil")
	}

	re := hoststatus.ReplicaStage(rep.App.InstanceName(), rep.Replica.Id)

	image := rep.App.Spec.Image

	// Pull image if not exists
	if _, exists := hoststatus.ImageList.Load(image); !exists {
		slog.Info("pulling container image", "image", image)
		re.SetRunning(inapi.AppDeployStageNameImagePull, image)
		ctx, cancel := context.WithTimeout(context.Background(), imagePullTimeout)
		if err := ctrDriver.ImagePull(ctx, image); err != nil {
			cancel()
			slog.Warn("image pull failed", "image", image, "error", err)
			re.SetFailed(inapi.AppDeployStageNameImagePull, err.Error())
			return fmt.Errorf("image pull failed: %w", err)
		}
		cancel()
		re.SetSuccess(inapi.AppDeployStageNameImagePull, "")

		// Refresh image list
		ctx2, cancel2 := context.WithTimeout(context.Background(), defaultContainerTimeout)
		if images, err := ctrDriver.ImageList(ctx2); err == nil {
			hoststatus.ImageList.Clear()
			for _, img := range images {
				hoststatus.ImageList.Store(img.Name, img)
			}
		}
		cancel2()
	}

	// Download and prepare packages
	re.SetRunning(inapi.AppDeployStageNamePkgDownload, "")
	pkgMounts, err := EnsurePackages(rep.App)
	if err != nil {
		slog.Warn("package preparation failed",
			"app", rep.App.InstanceName(),
			"error", err)
		re.SetFailed(inapi.AppDeployStageNamePkgDownload, err.Error())
		return fmt.Errorf("package preparation failed: %w", err)
	}
	re.SetSuccess(inapi.AppDeployStageNamePkgDownload, "")

	opts := &hostapi.ContainerCreateOptions{
		Name:          containerName,
		Image:         image,
		RestartPolicy: "always",
		Labels: map[string]string{
			"app_name":   rep.App.InstanceName(),
			"app_rep_id": fmt.Sprintf("%d", rep.Replica.Id),
		},
		Env: []string{
			fmt.Sprintf("APP_NAME=%s", rep.App.InstanceName()),
			fmt.Sprintf("APP_REP_ID=%d", rep.Replica.Id),
			fmt.Sprintf("APP_HOST_ID=%s", config.Config.Hostlet.HostId),
		},
	}

	if rep.App.Deploy != nil {
		opts.CpuLimit = rep.App.Deploy.CpuLimit
		opts.MemoryLimit = rep.App.Deploy.MemoryLimit
	}

	// DNS servers from host config
	opts.DnsServers = config.Config.Hostlet.DnsServers

	// VPC networking
	opts.VpcIPv4 = rep.Replica.VpcIpv4
	opts.VpcSubnet = config.Config.Hostlet.VpcInstanceCIDR
	if !strings.Contains(opts.VpcSubnet, "/") {
		opts.VpcSubnet += "/24"
	}

	// Setup port bindings using allocated host ports from scheduler
	for _, sp := range rep.Replica.ServicePorts {
		if sp != nil && sp.Port > 0 && sp.HostPort > 0 {
			opts.Ports = append(opts.Ports, hostapi.PortBinding{
				ContainerPort: int32(sp.Port),
				HostPort:      int32(sp.HostPort),
				Protocol:      "tcp",
			})
		}
	}

	// Fallback: bind service ports without host port allocation (1:1 mapping)
	if len(opts.Ports) == 0 {
		for _, sp := range rep.App.Spec.ServicePorts {
			if sp != nil && sp.Port > 0 && sp.Port < 65536 {
				opts.Ports = append(opts.Ports, hostapi.PortBinding{
					ContainerPort: int32(sp.Port),
					HostPort:      int32(sp.Port),
					Protocol:      "tcp",
				})
			}
		}
	}

	// Setup volume mounts using hostapi path utilities
	appBasePath := config.Config.Hostlet.AppPath
	if appBasePath != "" {
		appPaths := hostapi.NewContainerPath(appBasePath, containerName)

		// Mount /opt
		optPath := appPaths.OptDir()
		if err := os.MkdirAll(optPath, 0755); err == nil {
			opts.Mounts = append(opts.Mounts, hostapi.MountBind{
				HostPath: optPath, ContainerPath: "/opt", ReadOnly: false,
			})
		}

		// Mount /home
		homePath := appPaths.HomeDir()
		if err := os.MkdirAll(homePath, 0755); err == nil {
			opts.Mounts = append(opts.Mounts, hostapi.MountBind{
				HostPath: homePath, ContainerPath: "/home", ReadOnly: false,
			})
		}

		// Mount packages as read-only
		for pkgName, installDir := range pkgMounts {
			opts.Mounts = append(opts.Mounts, hostapi.MountBind{
				HostPath:      installDir,
				ContainerPath: fmt.Sprintf("/usr/innerstack/%s", pkgName),
				ReadOnly:      true,
			})
		}

		// Mount lxcfs volumes for container resource isolation
		if lxcfsVols := lxcfsMounts(); len(lxcfsVols) > 0 {
			opts.Mounts = append(opts.Mounts, lxcfsVols...)
		}

		// Set container command to run ininit
		opts.Cmd = hostapi.ContainerCmd()
	}

	// Provision the bind-mounted .innerstack files (app_replica.json,
	// ininit, inagent) before the container is created so the entrypoint
	// finds them on first start. Fatal on ininit/inagent failure.
	re.SetRunning(inapi.AppDeployStageNameProvision, "")
	if err := provisionInnerStack(rep); err != nil {
		re.SetFailed(inapi.AppDeployStageNameProvision, err.Error())
		return err
	}
	re.SetSuccess(inapi.AppDeployStageNameProvision, "")

	ctx, cancel := context.WithTimeout(context.Background(), defaultContainerTimeout)
	re.SetRunning(inapi.AppDeployStageNameContainerCreate, "")
	_, err = ctrDriver.ContainerCreate(ctx, opts)
	cancel()
	if err != nil {
		slog.Warn("container create failed", "container", containerName, "error", err)
		re.SetFailed(inapi.AppDeployStageNameContainerCreate, err.Error())
		return fmt.Errorf("container create failed: %w", err)
	}
	re.SetSuccess(inapi.AppDeployStageNameContainerCreate, "")

	hoststatus.ContainerList.Store(containerName, &hostapi.ContainerInfo{
		Name: containerName, Image: image, State: inapi.OpStateStarting,
	})
	// Record the applied Deploy.Revision so that containerSpecReset detects
	// the next revision increment and triggers a recreate, rather than
	// re-destroying this freshly created container on the following tick.
	// Persist it so an increment issued while the hostlet is down is still
	// detected after a restart.
	repInstances.TryStore(rep)
	if rep.App.Deploy != nil {
		hoststatus.Active.SetAppliedRevision(containerName, rep.App.Deploy.Revision)
	}
	saveHostActiveConfig()
	slog.Info("container created", "container", containerName)
	return nil
}

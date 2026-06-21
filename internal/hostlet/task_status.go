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
	"fmt"
	"log/slog"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	ps_cpu "github.com/shirou/gopsutil/v4/cpu"
	ps_disk "github.com/shirou/gopsutil/v4/disk"
	ps_mem "github.com/shirou/gopsutil/v4/mem"
	ps_net "github.com/shirou/gopsutil/v4/net"
	"google.golang.org/protobuf/proto"

	"github.com/sysinner/incore/v2/internal/client"
	"github.com/sysinner/incore/v2/internal/config"
	"github.com/sysinner/incore/v2/internal/hostlet/hostapi"
	"github.com/sysinner/incore/v2/internal/hostlet/hoststatus"
	"github.com/sysinner/incore/v2/internal/inutil"
	"github.com/sysinner/incore/v2/pkg/inapi"
)

var (
	hostStatus         = hostStatusMut{HostStatus: &inapi.HostStatus{}}
	hostStatusCounter  = inutil.NewGroupSlidingCounter(15 * 60) // 15 minutes window
	lastAppDiskFreshed int64
)

func init() {
	hostStatus.Uptime = time.Now().Unix()
}

type hostStatusMut struct {
	mu sync.RWMutex
	*inapi.HostStatus
}

func (it *hostStatusMut) clone() *inapi.HostStatus {
	it.mu.RLock()
	defer it.mu.RUnlock()
	return proto.Clone(it.HostStatus).(*inapi.HostStatus)
}

func (it *hostStatusMut) lock(fn func()) {
	it.mu.Lock()
	defer it.mu.Unlock()
	fn()
}

// statusRefresh collects host metrics and reports to zone leader.
func statusRefresh() error {
	const rtWindowSize int64 = 60
	tn := time.Now().Unix()

	if hostStatus.Updated == 0 {
		hostStatus.lock(func() {
			hostStatus.CpuCores = int32(runtime.NumCPU())
		})
	}

	if hostStatus.CpuModel == "" {
		if info, err := ps_cpu.Info(); err == nil && len(info) > 0 {
			hostStatus.lock(func() {
				hostStatus.CpuModel = info[0].ModelName
			})
		}
	}

	// Memory metrics
	if vm, _ := ps_mem.VirtualMemory(); vm != nil {
		hostStatus.lock(func() {
			hostStatus.MemTotal = int64(vm.Total)
			hostStatus.MemUsed = int64(vm.Used)
			hostStatus.MemAvailable = int64(vm.Available)
		})
	}

	// Network I/O
	if nio, _ := ps_net.IOCounters(false); len(nio) > 0 {
		rs := hostStatusCounter.Counter("net/rb").Record(int64(nio[0].BytesRecv))
		rn := hostStatusCounter.Counter("net/rc").Record(int64(nio[0].PacketsRecv))
		ss := hostStatusCounter.Counter("net/sb").Record(int64(nio[0].BytesSent))
		sn := hostStatusCounter.Counter("net/sc").Record(int64(nio[0].PacketsSent))

		hostStatus.lock(func() {
			hostStatus.NetRecvBytes = rs.Delta(rtWindowSize)
			hostStatus.NetRecvCount = rn.Delta(rtWindowSize)
			hostStatus.NetSentBytes = ss.Delta(rtWindowSize)
			hostStatus.NetSentCount = sn.Delta(rtWindowSize)
		})
	}

	// CPU metrics
	if cio, _ := ps_cpu.Times(false); len(cio) > 0 {
		cs := hostStatusCounter.Counter("cpu/sys").Record(int64(cio[0].System * 1e3))
		cu := hostStatusCounter.Counter("cpu/user").Record(int64(cio[0].User * 1e3))

		hostStatus.lock(func() {
			hostStatus.CpuSys = cs.Delta(rtWindowSize)
			hostStatus.CpuUser = cu.Delta(rtWindowSize)
		})
	}

	// Disk I/O
	devs, _ := ps_disk.Partitions(true)
	if devName, mntPoint := diskDevName(devs, config.Config.Hostlet.AppPath); devName != "" {
		if diom, err := ps_disk.IOCounters(devName); err == nil {
			if dio, ok := diom[devName]; ok {
				rn := hostStatusCounter.Counter("fs/sp/rc").Record(int64(dio.ReadCount))
				rs := hostStatusCounter.Counter("fs/sp/rb").Record(int64(dio.ReadBytes))
				wn := hostStatusCounter.Counter("fs/sp/wc").Record(int64(dio.WriteCount))
				ws := hostStatusCounter.Counter("fs/sp/wb").Record(int64(dio.WriteBytes))

				hostStatus.lock(func() {
					hostStatus.DiskReadBytes = rs.Delta(rtWindowSize)
					hostStatus.DiskReadCount = rn.Delta(rtWindowSize)
					hostStatus.DiskWriteBytes = ws.Delta(rtWindowSize)
					hostStatus.DiskWriteCount = wn.Delta(rtWindowSize)
				})
			}
		}

		if hostStatus.DiskTotalBytes == 0 || lastAppDiskFreshed+1800 < tn {
			if st, err := ps_disk.Usage(mntPoint); err == nil {
				hostStatus.lock(func() {
					hostStatus.DiskTotalBytes = int64(st.Total)
					hostStatus.DiskFreeBytes = int64(st.Free)
				})
			}
			lastAppDiskFreshed = tn
		}
	}

	// Container driver info
	for _, drv := range ctrDrivers {
		val, ok := hoststatus.StatusSet.Load(drv.Name())
		if !ok {
			continue
		}
		driverInfo, ok := val.(*hostapi.DriverInfo)
		if !ok {
			continue
		}
		hostStatus.lock(func() {
			if hostStatus.Containers == nil {
				hostStatus.Containers = make(map[string]*inapi.HostStatus_Container)
			}
			hostStatus.Containers[driverInfo.Name] = &inapi.HostStatus_Container{
				Driver:       driverInfo.Name,
				Version:      driverInfo.Version,
				ApiVersion:   driverInfo.APIVersion,
				ContainerNum: int32(driverInfo.ContainerNum),
				ImageNum:     int32(driverInfo.ImageNum),
			}
		})
	}

	hs := hostStatus.clone()

	zoneLeaderAddr := ""
	if len(config.Config.Server.ZoneHosts) > 0 {
		zoneLeaderAddr = config.Config.Server.ZoneHosts[0]
	}

	if zoneLeaderAddr == "" {
		return nil
	}

	conn, err := client.Connect(zoneLeaderAddr, config.Config.Hostlet.AuthKey(), false)
	if err != nil {
		slog.Warn("zone leader connection failed", "err", err)
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	zc := inapi.NewZoneInternalServiceClient(conn)
	resp, err := zc.HostStatusUpdate(ctx, &inapi.HostStatusUpdateRequest{
		Host: &inapi.Host{
			Id:       config.Config.Hostlet.HostId,
			PeerAddr: fmt.Sprintf("%s:%d", config.Config.Hostlet.LanAddr, config.Config.Server.PeerPort),
		},
		Status: hs,
	})
	if err != nil {
		slog.Warn("status update failed", "err", err)
		return err
	}

	for _, app := range resp.AppInstances {
		hoststatus.ActiveAppList.Store(app.InstanceName(), app)
	}

	cfgFlush := false

	// Extract VPC configuration from zone leader response
	if resp.VpcBridgeIp != "" && resp.VpcBridgeIp != config.Config.Hostlet.VpcBridgeIP {
		config.Config.Hostlet.VpcBridgeIP = resp.VpcBridgeIp
		cfgFlush = true
	}
	if resp.VpcInstanceCidr != "" && resp.VpcInstanceCidr != config.Config.Hostlet.VpcInstanceCIDR {
		config.Config.Hostlet.VpcInstanceCIDR = resp.VpcInstanceCidr
		cfgFlush = true
	}

	if resp.ZoneNetworkMap != nil &&
		resp.ZoneNetworkMap.Revision > zoneNetworkMap.Revision {
		if resp.ZoneNetworkMap.VpcNetworkDomain != "" &&
			resp.ZoneNetworkMap.VpcNetworkDomain != config.Config.Hostlet.VpcNetworkDomain {
			config.Config.Hostlet.VpcNetworkDomain = resp.ZoneNetworkMap.VpcNetworkDomain
			cfgFlush = true
		}
		zoneNetworkMap = *resp.ZoneNetworkMap
	}

	if cfgFlush {
		config.Flush()
	}

	hoststatus.HostReady.Store(true)

	return nil
}

// diskDevName finds the device name and mount point for a given path.
func diskDevName(pls []ps_disk.PartitionStat, path string) (string, string) {
	if runtime.GOOS == "darwin" {
		if !strings.HasPrefix(path, "/Volumes/") {
			path = "/Volumes" + path
		}
	}

	path = filepath.Clean(path)
	sort.Slice(pls, func(i, j int) bool {
		return strings.Compare(pls[i].Mountpoint, pls[j].Mountpoint) > 0
	})

	for _, v := range pls {
		if strings.HasPrefix(v.Device, "/dev/") && strings.HasPrefix(path, v.Mountpoint) {
			if runtime.GOOS == "darwin" {
				return "disk0", v.Mountpoint
			}
			return v.Device[5:], v.Mountpoint
		}
	}
	return "", ""
}

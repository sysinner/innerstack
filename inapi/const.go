// Copyright 2015 Eryx <evorui аt gmаil dοt cοm>, All rights reserved.
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

package inapi

const (
	// CPU resource limits in millicores
	CPUMin = 10
	CPUMax = 64000

	// Memory resource limits in bytes
	MemoryMin = 64 * 1024 * 1024         // 64 MiB
	MemoryMax = 512 * 1024 * 1024 * 1024 // 512 GiB

	// Volume size limits in bytes
	VolumeMin = 1 * 1024 * 1024 * 1024         // 1 GiB
	VolumeMax = 10 * 1024 * 1024 * 1024 * 1024 // 10 TiB
)

const (
	HealthStatusActionActive uint32 = 1 << 1
	HealthStatusActionSetup  uint32 = 1 << 2

	HealthFailoverActiveTimeDef   int32 = 7200
	HealthFailoverActiveTimeMin   int32 = 300
	HealthFailoverScheduleTimeMin int32 = 36000

	HealthFailoverMsgSent uint32 = 1 << 16
)

const (
	OpActionStart     string = "start"
	OpActionRunning   string = "running"
	OpActionStop      string = "stop"
	OpActionStopped   string = "stopped"
	OpActionDestroy   string = "destroy"
	OpActionDestroyed string = "destroyed"
	OpActionMigrate   string = "migrate"
	OpActionMigrated  string = "migrated"
	OpActionFailover  string = "failover"
	OpActionPending   string = "pending"
	OpActionWarning   string = "warning"
	OpActionRestart   string = "restart"
	OpActionResFree   string = "res_free"
	OpActionHang      string = "hang"
	OpActionUnbound   string = "unbound"
	OpActionForce     string = "force"
)

const (
	OpLogOK    = "ok"
	OpLogInfo  = "info"
	OpLogWarn  = "warn"
	OpLogError = "error"

	NsOpLogZoneRepMigrateAlloc       = "zm/rep-migrate/alloc"
	NsOpLogZoneRepMigratePrevStop    = "zm/rep-migrate/stop"
	NsOpLogZoneRepMigratePrevDestory = "zm/rep-migrate/destroy"
	NsOpLogZoneRepMigrateNextData    = "zm/rep-migrate/data"
	NsOpLogZoneRepMigrateDone        = "zm/rep-migrate/done"
)

const (
	OpLogNsZoneMasterPodScheduleCharge  = "zm/ps/charge"
	OpLogNsZoneMasterPodScheduleAlloc   = "zm/ps/alloc"
	OpLogNsZoneMasterPodScheduleResFree = "zm/ps/resfree"
)

// var (
// 	OpLogNsZoneMasterPodScheduleRep = func(repId uint32) string {
// 		if repId > 65535 {
// 			repId = 65535
// 		}
// 		return fmt.Sprintf("zm/ps/rep/%d", repId)
// 	}
// )

// type OpLogList []*OpLogSets

// func (ls *OpLogList) Get(sets_name string) *OpLogSets {
// 	oplogListMu.RLock()
// 	defer oplogListMu.RUnlock()
// 	return OpLogSetsSliceGet(*ls, sets_name)
// }

// func (ls *OpLogList) LogSet(sets_name string, version uint32, name, status, msg string) {

// 	oplogListMu.Lock()
// 	defer oplogListMu.Unlock()

// 	sets := OpLogSetsSliceGet(*ls, sets_name)
// 	if sets == nil {
// 		sets = &OpLogSets{
// 			Name:    sets_name,
// 			Version: version,
// 		}
// 		*ls, _ = OpLogSetsSliceSync(*ls, sets)
// 	}

// 	if version < sets.Version {
// 		return
// 	}

// 	sets.LogSet(version, name, status, msg)
// }

// func NewOpLogSets(sets_name string, version uint32) *OpLogSets {

// 	return &OpLogSets{
// 		Name:    sets_name,
// 		Version: version,
// 	}
// }

// func (rs *OpLogSets) LogSet(version uint32, name, status, message string) {

// 	oplogSetsMu.Lock()
// 	defer oplogSetsMu.Unlock()

// 	if version > 0 && version > rs.Version {
// 		rs.Version = version
// 		rs.Items = []*OpLogEntry{}
// 	}

// 	tn := uint64(time.Now().UnixNano() / 1e6)

// 	rs.Items, _ = OpLogEntrySliceSync(rs.Items, &OpLogEntry{
// 		Name:    name,
// 		Status:  status,
// 		Message: message,
// 		Updated: tn,
// 	})
// }

// func (rs *OpLogSets) LogSetEntry(entry *OpLogEntry) {
// 	rs.Items, _ = OpLogEntrySliceSync(rs.Items, entry)
// }

// func NewOpLogEntry(name, status, message string) *OpLogEntry {
// 	return &OpLogEntry{
// 		Name:    name,
// 		Status:  status,
// 		Message: message,
// 		Updated: uint64(time.Now().UnixNano() / 1e6),
// 	}
// }

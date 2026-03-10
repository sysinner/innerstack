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

// AppSpecConfigField type constants
const (
	SpecFieldTypeUnspec = ""       // unspecified type
	SpecFieldTypeString = "string" // string type
	SpecFieldTypeSelect = "select" // select type

	SpecFieldTypeText         = "text"          // text type
	SpecFieldTypeTextJSON     = "text_json"     // json text
	SpecFieldTypeTextTOML     = "text_toml"     // toml text
	SpecFieldTypeTextYAML     = "text_yaml"     // yaml text
	SpecFieldTypeTextINI      = "text_ini"      // ini text
	SpecFieldTypeTextJavaProp = "text_javaprop" // java properties
	SpecFieldTypeTextMarkdown = "text_markdown" // markdown text

	SpecFieldTypeAuthCert = "auth_cert" // auth certificate
)

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

// Health status action flags
const (
	HealthStatusActionActive uint32 = 1 << 1 // health status: active
	HealthStatusActionSetup  uint32 = 1 << 2 // health status: setup

	// Health failover timing configuration (in seconds)
	HealthFailoverActiveTimeDef   int32 = 7200  // default active time: 2 hours
	HealthFailoverActiveTimeMin   int32 = 300   // minimum active time: 5 minutes
	HealthFailoverScheduleTimeMin int32 = 36000 // minimum schedule time: 10 hours

	HealthFailoverMsgSent uint32 = 1 << 16 // failover message sent flag
)

const (
	// User action command
	OpActionStart   = "start"   // user action: start
	OpActionStop    = "stop"    // user action: stop
	OpActionDestroy = "destroy" // user action: destroy
)

// AppReplicaState represents the actual runtime state of a replica
// These are stable states that a replica can be in
const (
	// Empty state
	OpStateEmpty = "" // empty state

	// Lifecycle states
	OpStateStarting   = "starting"   // replica state: starting
	OpStateRunning    = "running"    // replica state: running
	OpStateStopping   = "stopping"   // replica state: stopping
	OpStateStopped    = "stopped"    // replica state: stopped
	OpStateDestroying = "destroying" // replica state: destroying
	OpStateDestroyed  = "destroyed"  // replica state: destroyed

	// Error state
	OpStateFailed = "failed" // replica state: failed
)

// AppReplicaEvent represents user actions or transition results
// These trigger state transitions in the state machine
const (
	// Transition result events
	OpEventSuccess = "success" // transition result: success
	OpEventFail    = "fail"    // transition result: fail
)

// Operation log status levels
const (
	OpLogOK    = "ok"    // operation completed successfully
	OpLogInfo  = "info"  // informational message
	OpLogWarn  = "warn"  // warning message
	OpLogError = "error" // error message
)

// Zone replica migration operation log namespaces
// Used for tracking migration progress across zone replicas
const (
	NsOpLogZoneRepMigrateAlloc       = "zm/rep-migrate/alloc"   // allocate resources for migration
	NsOpLogZoneRepMigratePrevStop    = "zm/rep-migrate/stop"    // stop previous replica
	NsOpLogZoneRepMigratePrevDestory = "zm/rep-migrate/destroy" // destroy previous replica
	NsOpLogZoneRepMigrateNextData    = "zm/rep-migrate/data"    // migrate data to new replica
	NsOpLogZoneRepMigrateDone        = "zm/rep-migrate/done"    // migration completed
)

// Zone master pod scheduling operation log namespaces
// Used for tracking pod scheduling operations
const (
	OpLogNsZoneMasterPodScheduleCharge  = "zm/ps/charge"  // charge resources for scheduling
	OpLogNsZoneMasterPodScheduleAlloc   = "zm/ps/alloc"   // allocate pod to host
	OpLogNsZoneMasterPodScheduleResFree = "zm/ps/resfree" // free allocated resources
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

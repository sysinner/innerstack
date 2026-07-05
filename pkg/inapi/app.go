package inapi

import (
	"fmt"
	"strconv"
	"time"
)

// NormalizeLegacy migrates the deprecated single-value resource fields
// (cpu_limit/memory_limit/volume_limit) into the min/max range fields and
// clears the legacy fields. It is idempotent and operates on string values
// only; numeric range and system-bounds validation happens in the zonelet
// deploy path (which has access to the parsers).
//
// Mapping rules, applied per resource (cpu, memory, volume):
//   - both min and max empty, legacy non-empty -> min = max = legacy
//   - min empty, max set                      -> min = max
//   - max empty, min set                      -> max = min
//   - both min and max set                    -> unchanged (range wins over
//     a concurrently set legacy field)
//
// The legacy field is always cleared after mapping, giving a canonical form
// for persistence and TOML export. Mapping cpu_limit to a fixed point
// (min = max = legacy) preserves the historical deploy behavior, since the
// deploy runtime value defaults to the min.
func (x *AppSpecResources) NormalizeLegacy() {
	if x == nil {
		return
	}
	x.CpuLimit = normLegacyPair(&x.CpuMin, &x.CpuMax, x.CpuLimit)
	x.MemoryLimit = normLegacyPair(&x.MemoryMin, &x.MemoryMax, x.MemoryLimit)
	x.VolumeLimit = normLegacyPair(&x.VolumeMin, &x.VolumeMax, x.VolumeLimit)
}

// normLegacyPair applies the per-resource NormalizeLegacy mapping to a single
// (min, max, legacy) triple. min/max are mutated in place via pointers; the
// residual legacy value (always "" after mapping) is returned.
func normLegacyPair(min, max *string, legacy string) string {
	switch {
	case *min == "" && *max == "" && legacy != "":
		*min, *max = legacy, legacy
	case *min == "" && *max != "":
		*min = *max
	case *max == "" && *min != "":
		*max = *min
	}
	return ""
}

// Field returns the AppDeployConfigItem with the given name, or nil if not found
func (x *AppDeployConfigItem) Item(name string) *AppDeployConfigItem {
	if x != nil {
		for _, item := range x.Items {
			if item != nil && item.Name == name {
				return item
			}
		}
	}
	return nil
}

// // Value returns the value of the field with the given name
// func (x *AppDeployConfigItem) Value(name string) string {
// 	if field := x.Field(name); field != nil {
// 		return field.Value
// 	}
// 	return ""
// }

// // ValueOK returns the value of the field with the given name and a boolean indicating if it was found
// func (x *AppDeployConfigItem) ValueOK(name string) (string, bool) {
// 	if field := x.Field(name); field != nil {
// 		return field.Value, true
// 	}
// 	return "", false
// }

// InstanceId returns the unique identifier of the application instance.
func (x *AppInstance) InstanceId() string {
	if x != nil && x.Meta != nil {
		return x.Meta.Id
	}
	return ""
}

// InstanceName returns the human-readable name of the application instance.
func (x *AppInstance) InstanceName() string {
	if x != nil && x.Meta != nil {
		return x.Meta.Name
	}
	return ""
}

// ContainerName returns the container name for an app replica.
// Format: i8k_{instanceName}_{replicaId}
func ContainerName(instanceName string, repId uint32) string {
	return fmt.Sprintf("i8k_%s_%d", instanceName, repId)
}

// ContainerName returns the container name for the app replica instance.
// The instance name (Meta.Name) is the logical key after the id/name refactor.
func (it *AppReplicaInstance) ContainerName() string {
	if it == nil || it.App == nil || it.Replica == nil {
		return ""
	}
	return ContainerName(it.App.InstanceName(), it.Replica.Id)
}

// stageNowMs returns the current time as a unix timestamp in milliseconds.
func stageNowMs() int64 {
	return time.Now().UnixMilli()
}

// StagesRoot returns the root AppDeployStage of the deploy lifecycle,
// creating it if absent.
func (x *AppDeploy) StagesRoot() *AppDeployStage {
	if x == nil {
		return nil
	}
	if x.Stages == nil {
		x.Stages = &AppDeployStage{
			Name:  AppDeployStageNameDeploy,
			Owner: AppStageOwnerZonelet,
		}
	}
	return x.Stages
}

// Find returns the immediate child stage with the given name, or nil if not
// found.
func (x *AppDeployStage) Find(name string) *AppDeployStage {
	if x == nil {
		return nil
	}
	for _, s := range x.Stages {
		if s != nil && s.Name == name {
			return s
		}
	}
	return nil
}

// Child returns the immediate child stage with the given name, creating it
// (with the given owner, pending) if absent. owner must be one of the
// AppStageOwner* constants so every stage has a determined owner.
func (x *AppDeployStage) Child(name, owner string) *AppDeployStage {
	if x == nil {
		return nil
	}
	if s := x.Find(name); s != nil {
		return s
	}
	s := &AppDeployStage{Name: name, Owner: owner, State: AppStageStatePending}
	x.Stages = append(x.Stages, s)
	return s
}

// SetRunning marks the stage as running. created is set to now if this is
// the first run; attempt is incremented when transitioning out of a failed
// state (a retry).
func (x *AppDeployStage) SetRunning(msg string) {
	if x == nil {
		return
	}
	if x.State == AppStageStateFailed {
		x.Attempt++
	}
	if x.Created == 0 {
		x.Created = stageNowMs()
	}
	x.Finished = 0
	x.State = AppStageStateRunning
	if msg != "" {
		x.Message = msg
	}
}

// SetSuccess marks the stage as successfully completed at the current time.
func (x *AppDeployStage) SetSuccess(msg string) {
	if x == nil {
		return
	}
	if x.Created == 0 {
		x.Created = stageNowMs()
	}
	x.Finished = stageNowMs()
	x.State = AppStageStateSuccess
	if msg != "" {
		x.Message = msg
	}
}

// SetFailed marks the stage as failed at the current time with a message.
func (x *AppDeployStage) SetFailed(msg string) {
	if x == nil {
		return
	}
	if x.Created == 0 {
		x.Created = stageNowMs()
	}
	x.Finished = stageNowMs()
	x.State = AppStageStateFailed
	if msg != "" {
		x.Message = msg
	}
}

// SetInstant records an instantaneous stage event: created and finished are
// both set to now and the state to success.
func (x *AppDeployStage) SetInstant(msg string) {
	if x == nil {
		return
	}
	now := stageNowMs()
	x.Created = now
	x.Finished = now
	x.State = AppStageStateSuccess
	if msg != "" {
		x.Message = msg
	}
}

// SetRevisionDeep stamps the AppDeploy.revision this stage (and its nested
// children) was recorded against, for consistency checking across the
// distributed deploy chain.
func (x *AppDeployStage) SetRevisionDeep(rev uint64) {
	if x == nil {
		return
	}
	x.Revision = rev
	for _, s := range x.Stages {
		s.SetRevisionDeep(rev)
	}
}

// ReplicaStage returns the per-replica child node (name =
// AppDeployStageNameReplica) whose attrs "rep_id" matches repId, creating
// it if absent.
func (x *AppDeployStage) ReplicaStage(repId uint32) *AppDeployStage {
	if x == nil {
		return nil
	}
	want := strconv.FormatUint(uint64(repId), 10)
	for _, s := range x.Stages {
		if s == nil || s.Name != AppDeployStageNameReplica {
			continue
		}
		if s.Attrs == nil {
			continue
		}
		if s.Attrs[AppDeployStageReplicaAttrRepId] == want {
			return s
		}
	}
	s := &AppDeployStage{
		Name:  AppDeployStageNameReplica,
		Owner: AppStageOwnerZonelet,
		State: AppStageStatePending,
		Attrs: map[string]string{
			AppDeployStageReplicaAttrRepId: want,
		},
	}
	x.Stages = append(x.Stages, s)
	return s
}

// PruneChildren removes immediate children whose name does not satisfy keep.
func (x *AppDeployStage) PruneChildren(keep func(name string) bool) {
	if x == nil || keep == nil {
		return
	}
	kept := x.Stages[:0]
	for _, s := range x.Stages {
		if s != nil && !keep(s.Name) {
			continue
		}
		kept = append(kept, s)
	}
	x.Stages = kept
}

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

package hoststatus

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"google.golang.org/protobuf/proto"

	"github.com/sysinner/innerstack/v2/internal/inutil"
	"github.com/sysinner/innerstack/v2/internal/inutil/syncx"
	"github.com/sysinner/innerstack/v2/internal/stateflow"
	"github.com/sysinner/innerstack/v2/pkg/inapi"
)

var (
	StatusSet sync.Map

	ActiveAppList syncx.Map

	ContainerList syncx.Map

	ImageList syncx.Map

	Active HostActiveConfig

	// Desired is the freshest view of which container names the zonelet leader
	// most recently reported as desired on this host (across all deploy
	// actions). It is rebuilt wholesale on every successful HostStatusUpdate
	// and is the authoritative comparison set for orphan-container detection:
	// unlike ActiveAppList (append-only), it can express "the leader no longer
	// delivers this instance".
	Desired desiredSnapshot

	AppWorkflow stateflow.AppStateWorkflow

	// ReplicaStages holds the host-side deploy stage progress per replica,
	// keyed by ReplicaStageKey(instanceName, repId). It is hostlet-local and
	// persisted across ticks; the status loop reports dirty entries upward
	// via HostStatusUpdate.
	ReplicaStages syncx.Map
)

var (
	HostReady      atomic.Bool
	ContainerReady atomic.Bool
)

type HostActiveConfig struct {
	mu sync.RWMutex

	AppInstances []*inapi.AppInstance `json:"app_instances,omitempty"`

	// AppliedRevisions records the last-applied Deploy.Revision per
	// container name. It is persisted to hostlet_active.json so that a
	// Deploy.Revision increment issued while the hostlet was down is still
	// detected after restart and triggers a container recreate.
	AppliedRevisions map[string]uint64 `json:"applied_revisions,omitempty"`

	// SecretKeys holds the per-replica inagent status API secret keyed by
	// container name. Persisted to hostlet_active.json so it survives hostlet
	// restart and stays consistent with the secret written into app_replica.json.
	SecretKeys map[string]string `json:"secret_keys,omitempty"`

	// DeletedInstances records app instances this hostlet has torn down for a
	// soft delete (Deploy.Action == delete), keyed by instance id, value is the
	// unix time of the teardown. Persisted so the hostlet keeps skipping them
	// across restarts until the zone TTL physically removes the instance.
	DeletedInstances map[string]int64 `json:"deleted_instances,omitempty"`

	// OrphanContainers records local containers that exist without a matching
	// desired app replica in the zonelet's fresh app list, keyed by container
	// name, value is the unix time the orphan was first observed. Persisted so
	// the removal grace window survives a hostlet restart. This is the
	// hostlet-local, leader-absence-driven cleanup state and is deliberately
	// separate from DeletedInstances (the leader-driven soft-delete flow).
	OrphanContainers map[string]int64 `json:"orphan_containers,omitempty"`

	index map[string]*inapi.AppInstance
}

// SetAppliedRevision records the last-applied Deploy.Revision for a
// container. Safe for concurrent use.
func (it *HostActiveConfig) SetAppliedRevision(containerName string, rev uint64) {
	it.mu.Lock()
	defer it.mu.Unlock()
	if it.AppliedRevisions == nil {
		it.AppliedRevisions = make(map[string]uint64)
	}
	it.AppliedRevisions[containerName] = rev
}

// AppliedRevision returns the last-applied Deploy.Revision for a container,
// or (0, false) if none is recorded.
func (it *HostActiveConfig) AppliedRevision(containerName string) (uint64, bool) {
	it.mu.RLock()
	defer it.mu.RUnlock()
	if it.AppliedRevisions == nil {
		return 0, false
	}
	rev, ok := it.AppliedRevisions[containerName]
	return rev, ok
}

// SecretKey returns the inagent API secret for a container, or "" if none.
func (it *HostActiveConfig) SecretKey(containerName string) string {
	it.mu.RLock()
	defer it.mu.RUnlock()
	if it.SecretKeys == nil {
		return ""
	}
	return it.SecretKeys[containerName]
}

// SetSecretKey sets the inagent API secret for a container.
func (it *HostActiveConfig) SetSecretKey(containerName, secret string) {
	it.mu.Lock()
	defer it.mu.Unlock()
	if it.SecretKeys == nil {
		it.SecretKeys = make(map[string]string)
	}
	it.SecretKeys[containerName] = secret
}

// EnsureSecretKey returns the existing inagent API secret for a container,
// generating a new random one if absent. changed is true if a new secret was
// created (the caller should persist the active config to disk).
func (it *HostActiveConfig) EnsureSecretKey(containerName string) (string, bool) {
	it.mu.Lock()
	defer it.mu.Unlock()
	if it.SecretKeys == nil {
		it.SecretKeys = make(map[string]string)
	}
	if s := it.SecretKeys[containerName]; s != "" {
		return s, false
	}
	s := inutil.SeqRandHexString(2, 32)
	it.SecretKeys[containerName] = s
	return s, true
}

// DeleteSecretKey removes the inagent API secret for a container.
func (it *HostActiveConfig) DeleteSecretKey(containerName string) {
	it.mu.Lock()
	defer it.mu.Unlock()
	if it.SecretKeys == nil {
		return
	}
	delete(it.SecretKeys, containerName)
}

// MarkDeleted records that this hostlet has torn down the container(s) for an
// instance at unix time ts, so the instance is skipped on subsequent syncs.
// It also prunes entries older than the zone soft-delete TTL window (plus a
// one-day slack) so the map cannot grow without bound across many deletions.
func (it *HostActiveConfig) MarkDeleted(instanceId string, ts int64) {
	if instanceId == "" {
		return
	}
	it.mu.Lock()
	defer it.mu.Unlock()
	if it.DeletedInstances == nil {
		it.DeletedInstances = make(map[string]int64)
	}
	it.DeletedInstances[instanceId] = ts

	// Bound growth: after the zone TTL elapses the leader stops delivering the
	// instance, so entries older than the TTL window are no longer needed.
	cutoff := ts - (inapi.AppInstanceSoftDeleteTTL / 1000) - 24*3600
	if cutoff > 0 {
		for id, t := range it.DeletedInstances {
			if t < cutoff {
				delete(it.DeletedInstances, id)
			}
		}
	}
}

// DeletedAt returns the unix time at which an instance was torn down on this
// host, and whether such a record exists.
func (it *HostActiveConfig) DeletedAt(instanceId string) (int64, bool) {
	it.mu.RLock()
	defer it.mu.RUnlock()
	if it.DeletedInstances == nil {
		return 0, false
	}
	ts, ok := it.DeletedInstances[instanceId]
	return ts, ok
}

// MarkOrphan records the first-seen time for a locally-orphaned container. It
// is a no-op if the container is already tracked, so the first-seen time is
// preserved across ticks until the orphan is cleared or removed. Safe for
// concurrent use.
func (it *HostActiveConfig) MarkOrphan(containerName string, ts int64) {
	if containerName == "" {
		return
	}
	it.mu.Lock()
	defer it.mu.Unlock()
	if it.OrphanContainers == nil {
		it.OrphanContainers = make(map[string]int64)
	}
	if _, ok := it.OrphanContainers[containerName]; ok {
		return
	}
	it.OrphanContainers[containerName] = ts
}

// OrphanFirstSeen returns the unix time at which a container was first observed
// as orphaned, and whether such a record exists.
func (it *HostActiveConfig) OrphanFirstSeen(containerName string) (int64, bool) {
	it.mu.RLock()
	defer it.mu.RUnlock()
	if it.OrphanContainers == nil {
		return 0, false
	}
	ts, ok := it.OrphanContainers[containerName]
	return ts, ok
}

// ClearOrphan removes the orphan tracking record for a container (used when the
// container returns to the desired set, or after it has been removed).
func (it *HostActiveConfig) ClearOrphan(containerName string) {
	it.mu.Lock()
	defer it.mu.Unlock()
	if it.OrphanContainers == nil {
		return
	}
	delete(it.OrphanContainers, containerName)
}

// Orphans returns a snapshot copy of the orphan container map. The copy lets
// callers iterate and mutate (ClearOrphan/MarkOrphan) without nesting the read
// lock behind a write lock.
func (it *HostActiveConfig) Orphans() map[string]int64 {
	it.mu.RLock()
	defer it.mu.RUnlock()
	if len(it.OrphanContainers) == 0 {
		return nil
	}
	out := make(map[string]int64, len(it.OrphanContainers))
	for k, v := range it.OrphanContainers {
		out[k] = v
	}
	return out
}

// MarshalJSON serializes the persisted fields under the read lock. The alias
// avoids recursing back into MarshalJSON.
func (it *HostActiveConfig) MarshalJSON() ([]byte, error) {
	it.mu.RLock()
	defer it.mu.RUnlock()
	type Alias HostActiveConfig
	return json.Marshal((*Alias)(it))
}

func (h *HostActiveConfig) UnmarshalJSON(data []byte) error {
	// 定义一个别名，避免递归调用 UnmarshalJSON 导致死循环
	type Alias HostActiveConfig
	aux := &struct {
		*Alias
	}{
		Alias: (*Alias)(h),
	}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}

	h.index = make(map[string]*inapi.AppInstance)

	for _, app := range h.AppInstances {
		if app != nil {
			h.index[app.InstanceName()] = app
		}
	}

	return nil
}

// desiredSnapshot holds the set of container names the zonelet leader most
// recently reported as desired on this host, plus the time and success of that
// report. It is rebuilt wholesale on every successful HostStatusUpdate; on an
// RPC failure SetFailed marks it unusable so orphan detection is suspended.
type desiredSnapshot struct {
	mu       sync.RWMutex
	names    map[string]struct{}
	syncedAt int64 // unix seconds of the last successful HostStatusUpdate
	ok       bool  // whether the last HostStatusUpdate succeeded
}

// Replace records the container names the leader just reported as desired for
// this host and marks the snapshot fresh at unix time ts.
func (d *desiredSnapshot) Replace(names map[string]struct{}, ts int64) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.names = names
	d.syncedAt = ts
	d.ok = true
}

// SetFailed marks the last HostStatusUpdate as failed so orphan detection is
// suspended until a fresh response arrives.
func (d *desiredSnapshot) SetFailed() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.ok = false
}

// Contains reports whether name is in the freshest desired set.
func (d *desiredSnapshot) Contains(name string) bool {
	d.mu.RLock()
	defer d.mu.RUnlock()
	if len(d.names) == 0 {
		return false
	}
	_, ok := d.names[name]
	return ok
}

// Empty reports whether the freshest desired set contains no names. The orphan
// sweep treats an empty fresh set as "no basis for orphan decisions" and
// refuses to act, since it is indistinguishable from a transiently-stale empty
// response.
func (d *desiredSnapshot) Empty() bool {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return len(d.names) == 0
}

// IsFresh reports whether the snapshot is usable for orphan decisions: the last
// sync must have succeeded and be within staleLimit seconds of now.
func (d *desiredSnapshot) IsFresh(now, staleLimit int64) bool {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.ok && d.syncedAt > 0 && (now-d.syncedAt) <= staleLimit
}

// ReplicaStageEntry holds the host-side stage node for a replica and a dirty
// flag indicating whether it needs to be reported upward. The mutex guards
// Stage/Dirty against concurrent access from the hostlet main loop and the
// inagent status HTTP handler.
type ReplicaStageEntry struct {
	mu       sync.Mutex
	Stage    *inapi.AppDeployStage
	Dirty    bool
	Revision uint64 // AppDeploy.Revision this entry's stages are based on
}

// ReplicaStageKey returns the map key for a (instanceName, repId) pair.
func ReplicaStageKey(instanceName string, repId uint32) string {
	return fmt.Sprintf("%s/%d", instanceName, repId)
}

// ParseReplicaStageKey splits a ReplicaStageKey back into its instance name
// and replica id. ok is false if the key is malformed.
func ParseReplicaStageKey(key string) (string, uint32, bool) {
	idx := strings.LastIndex(key, "/")
	if idx < 0 {
		return "", 0, false
	}
	repId, err := strconv.ParseUint(key[idx+1:], 10, 32)
	if err != nil {
		return "", 0, false
	}
	return key[:idx], uint32(repId), true
}

// ReplicaStage returns the host-side stage entry for the given replica,
// creating it if absent. The entry's Stage is a holder node whose children
// are the host-side deploy stages (host_recv, image_pull, ...).
func ReplicaStage(instanceName string, repId uint32) *ReplicaStageEntry {
	key := ReplicaStageKey(instanceName, repId)
	if v, ok := ReplicaStages.Load(key); ok {
		return v.(*ReplicaStageEntry)
	}
	entry := &ReplicaStageEntry{
		Stage: &inapi.AppDeployStage{
			Name:  inapi.AppDeployStageNameReplica,
			Owner: inapi.AppStageOwnerHostlet,
			Attrs: map[string]string{
				inapi.AppDeployStageReplicaAttrRepId: strconv.FormatUint(uint64(repId), 10),
			},
		},
	}
	ReplicaStages.Store(key, entry)
	return entry
}

// ReplicaStageDelete removes the host-side stage entry for a replica.
func ReplicaStageDelete(instanceName string, repId uint32) {
	ReplicaStages.Delete(ReplicaStageKey(instanceName, repId))
}

// SyncRevision aligns the entry with the current AppDeploy.Revision. When
// the revision changed, all stage children are cleared (the prior revision's
// progress is stale) and the dirty flag is set so the reset propagates
// upward. Returns true if a reset occurred.
func (e *ReplicaStageEntry) SyncRevision(rev uint64) bool {
	if e == nil {
		return false
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.Revision == rev {
		return false
	}
	e.Revision = rev
	e.Stage.Stages = nil
	e.Dirty = true
	return true
}

// stageName applies a stage transition by name, stamps it with the entry's
// revision, and marks the entry dirty.
func (e *ReplicaStageEntry) stageName(name, msg string, fn func(*inapi.AppDeployStage, string)) {
	if e == nil {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	s := e.Stage.Child(name, inapi.AppStageOwnerHostlet)
	fn(s, msg)
	s.Revision = e.Revision
	e.Dirty = true
}

func (e *ReplicaStageEntry) SetRunning(name, msg string) {
	e.stageName(name, msg, (*inapi.AppDeployStage).SetRunning)
}

func (e *ReplicaStageEntry) SetSuccess(name, msg string) {
	e.stageName(name, msg, (*inapi.AppDeployStage).SetSuccess)
}

func (e *ReplicaStageEntry) SetFailed(name, msg string) {
	e.stageName(name, msg, (*inapi.AppDeployStage).SetFailed)
}

func (e *ReplicaStageEntry) SetInstant(name, msg string) {
	e.stageName(name, msg, (*inapi.AppDeployStage).SetInstant)
}

// MergeInagent replaces the inagent-owned children of this entry's stage
// with the reported stages, and marks the entry dirty. Host-side children
// are preserved. Safe for concurrent use.
func (e *ReplicaStageEntry) MergeInagent(stages []*inapi.AppDeployStage) {
	if e == nil {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.Stage.PruneChildren(func(name string) bool {
		_, isInagent := inapi.AppDeployStageInagentNames[name]
		return !isInagent // keep non-inagent (host-side) children
	})
	for _, s := range stages {
		if s == nil || s.Name == "" {
			continue
		}
		// Discard stages based on a stale deploy revision.
		if s.Revision > 0 && e.Revision > 0 && s.Revision < e.Revision {
			continue
		}
		dst := e.Stage.Child(s.Name, inapi.AppStageOwnerInagent)
		dst.State = s.State
		dst.Attempt = s.Attempt
		dst.Created = s.Created
		dst.Finished = s.Finished
		dst.Message = s.Message
		dst.Revision = s.Revision
	}
	e.Dirty = true
}

// SnapshotIfDirty returns a clone of the stage children if the entry is dirty,
// or nil if nothing changed. It does not clear the dirty flag; the caller
// clears it via ClearDirty after a successful upward push so a failed push is
// retried on the next tick.
func (e *ReplicaStageEntry) SnapshotIfDirty() []*inapi.AppDeployStage {
	if e == nil {
		return nil
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if !e.Dirty {
		return nil
	}
	stages := make([]*inapi.AppDeployStage, 0, len(e.Stage.Stages))
	for _, s := range e.Stage.Stages {
		if s == nil {
			continue
		}
		stages = append(stages, cloneStage(s))
	}
	return stages
}

// ClearDirty clears the dirty flag. Called after a successful upward push.
func (e *ReplicaStageEntry) ClearDirty() {
	if e == nil {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.Dirty = false
}

// cloneStage returns a deep copy of an AppDeployStage.
func cloneStage(s *inapi.AppDeployStage) *inapi.AppDeployStage {
	if s == nil {
		return nil
	}
	return proto.Clone(s).(*inapi.AppDeployStage)
}

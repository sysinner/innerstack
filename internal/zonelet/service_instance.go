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

package zonelet

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/lynkdb/kvgo/v2/pkg/kvapi"
	"github.com/sysinner/innerstack/v2/internal/config"
	"github.com/sysinner/innerstack/v2/internal/data"
	"github.com/sysinner/innerstack/v2/internal/inutil"
	"github.com/sysinner/innerstack/v2/internal/inutil/autofill"
	"github.com/sysinner/innerstack/v2/internal/status"
	"github.com/sysinner/innerstack/v2/pkg/inapi"
	"github.com/sysinner/innerstack/v2/pkg/inauth"
)

func (s *zoneServer) AppInstanceDeploy(
	ctx context.Context, req *inapi.AppInstanceDeployRequest,
) (*inapi.AppInstanceDeployResponse, error) {

	if !inauth.AppContext(ctx).Allow(inapi.AuthScope_App_Write) {
		return nil, errors.New("auth fail: missing app:rw scope")
	}

	if !status.IsZoneletLeader() {
		return nil, errors.New("zonelet leader")
	}

	// rpcStartMs anchors the deploy lifecycle timing for this request.
	rpcStartMs := time.Now().UnixMilli()

	// name is the primary logical key for both create and update.
	if req.Name == "" {
		return nil, errors.New("name is required")
	}
	if err := inapi.DNSLabelValid(req.Name); err != nil {
		return nil, fmt.Errorf("name: %w", err)
	}

	// Determine whether this is a create or update by looking up the instance
	// by name (the logical key).
	existingByName, existingKvMeta, err := loadInstanceByName(req.Name)
	if err != nil {
		return nil, err
	}
	isUpdate := existingByName != nil

	// For new instances, spec is required
	if !isUpdate && req.Spec == nil {
		return nil, errors.New("spec is required for new instance")
	}

	var (
		// parsed holds the validated min/max resource ranges derived from
		// req.Spec.Resources (after legacy migration). It is the basis for
		// both the CREATE defaults (min) and the UPDATE range checks.
		parsed parsedResources

		// appSpecPrev* describe the currently persisted AppSpec for the
		// requested spec name. They are loaded once during validation so the
		// version can be resolved against the existing one, and reused by
		// refreshAppSpec when persisting the resolved version.
		appSpecPrevVersion uint64
		appSpecPrevExists  bool
	)

	refreshAppSpec := func(spec *inapi.AppSpec) error {

		key := inapi.NsAppSpec(spec.Name)

		if !appSpecPrevExists {
			if rs := data.Zonelet.NewWriter(key, spec).SetCreateOnly(true).Exec(); !rs.OK() {
				return rs.Error()
			}
			return nil
		}

		if rs := data.Zonelet.NewWriter(key, spec).SetPrevVersion(
			appSpecPrevVersion).Exec(); !rs.OK() {
			return rs.Error()
		}

		return nil
	}

	if req.Spec != nil {
		if err := inapi.DNSLabelValid(req.Spec.Name); err != nil {
			return nil, fmt.Errorf("spec.name: %w", err)
		}

		// Resolve the spec version against any previously persisted spec.
		// An empty version defaults to "0.0.1"; a provided version is validated.
		// When a previous spec exists, the request main version (MAJOR.MINOR.PATCH)
		// must not be lower than the existing one; an equal main version bumps the
		// release number on top of the previous version (0.0.1 -> 0.0.1-1,
		// 0.0.1-1 -> 0.0.1-2).
		{
			var prev inapi.AppSpec
			if rs := data.Zonelet.NewReader(
				inapi.NsAppSpec(req.Spec.Name)).Exec(); rs.NotFound() {
				// no previous spec persisted yet
			} else if !rs.OK() {
				return nil, rs.Error()
			} else if err := rs.Item().JsonDecode(&prev); err != nil {
				return nil, err
			} else {
				appSpecPrevExists = true
				appSpecPrevVersion = rs.Item().Meta.Version
			}

			resolved, err := appSpecResolveVersion(
				req.Spec.Version, prev.Version, appSpecPrevExists)
			if err != nil {
				return nil, err
			}
			req.Spec.Version = resolved
		}

		if req.Spec.Resources == nil {
			return nil, errors.New("spec.resources is required")
		}

		// Migrate legacy single-value fields (cpu_limit/memory_limit/
		// volume_limit) into the min/max range, then parse and validate.
		req.Spec.Resources.NormalizeLegacy()

		parsed, err = parseSpecResources(req.Spec.Resources)
		if err != nil {
			return nil, err
		}

		// Validate task trigger fields uniqueness (mutually exclusive)
		for _, task := range req.Spec.Tasks {
			if task == nil {
				continue
			}
			if err := inapi.ValidateTaskTrigger(task); err != nil {
				return nil, fmt.Errorf("task %q: %w", task.Name, err)
			}
		}

		// Validate config field schemas (names, group/array_group structure)
		for _, cfg := range req.Spec.Configs {
			if cfg == nil {
				continue
			}
			if err := inapi.ValidateSpecConfigField(cfg); err != nil {
				return nil, fmt.Errorf("config %q: %w", cfg.Name, err)
			}
		}

		// Validate app-level dependencies (spec.depends) exist as deployed instances
		if err := validateAppDependencies(req.Spec.Depends); err != nil {
			return nil, err
		}

		// Validate deploy-time dependency bindings (deploy.depends)
		if req.Deploy != nil && len(req.Deploy.Depends) > 0 {
			if err := validateDeployDependencies(req.Spec.Name, req.Deploy.Depends); err != nil {
				return nil, err
			}
		}

		// Validate deploy configs against the spec (e.g. array_group key
		// presence and uniqueness within each array_group).
		if req.Deploy != nil && len(req.Deploy.Configs) > 0 {
			if err := inapi.ValidateDeployConfigItems(req.Spec.Configs, req.Deploy.Configs); err != nil {
				return nil, err
			}
		}

		// Validate package dependencies exist and are fully uploaded
		if err := s.validatePackageDependencies(req.Spec.Packages); err != nil {
			return nil, err
		}

		//
		if err := refreshAppSpec(req.Spec); err != nil {
			slog.Info("app-spec update fail", "err", err.Error())
		} else {
			slog.Info(fmt.Sprintf("app-spec updated, name %s, version %s",
				req.Spec.Name, req.Spec.Version))
		}
	}

	var instance *inapi.AppInstance

	if isUpdate {
		// Update existing instance
		instance = existingByName

		// A soft-deleted instance is read-only until the TTL physically removes
		// it; reject any modification.
		if instance.Deploy != nil && instance.Deploy.Action == inapi.OpActionDelete {
			return nil, fmt.Errorf(
				"instance %q is deleted (read-only), it will be removed after TTL",
				req.Name)
		}

		// Build KV key from the instance name (logical key)
		var (
			key         = inapi.NsAppInstance(config.Config.Zonelet.ZoneName, instance.InstanceName())
			prevVersion = existingKvMeta.Version
		)

		// Update spec only if provided
		if req.Spec != nil {
			instance.Spec = req.Spec
		}

		if instance.Deploy == nil {
			instance.Deploy = &inapi.AppDeploy{}
		}

		// Resolve deploy resources against the spec range.
		//
		// `parsed` was derived from req.Spec during validation. When the
		// request carries an explicit override (e.g. --cpu 2000m) but no new
		// spec, the range is taken from the persisted instance.Spec instead,
		// so a flag-only scaling update still validates against the spec.
		//
		// Per resource:
		//   - explicit override in req.Deploy -> strict requireInRange; an
		//     out-of-range value is an error so operator typos surface.
		//   - otherwise (only when req.Spec is provided) -> keep the existing
		//     deploy value, clamped into the (possibly narrowed) range so a
		//     spec-only update always lands.
		// When neither a new spec nor an override is present, resources are
		// left untouched (requirement: updates that don't set resources keep
		// the prior value).
		var (
			hasOverride = req.Deploy != nil && (req.Deploy.CpuLimit > 0 ||
				req.Deploy.MemoryLimit > 0 || req.Deploy.VolumeLimit > 0)
			updParsed = parsed
		)
		if req.Spec == nil && hasOverride {
			if instance.Spec == nil || instance.Spec.Resources == nil {
				return nil, errors.New("cannot override resources: instance has no spec resources")
			}
			instance.Spec.Resources.NormalizeLegacy()
			p, err := parseSpecResources(instance.Spec.Resources)
			if err != nil {
				return nil, err
			}
			updParsed = p
		}

		if req.Spec != nil || hasOverride {
			if req.Deploy != nil && req.Deploy.CpuLimit > 0 {
				v, err := requireInRange("deploy.cpu_limit",
					req.Deploy.CpuLimit, updParsed.cpuMin, updParsed.cpuMax)
				if err != nil {
					return nil, err
				}
				instance.Deploy.CpuLimit = v
			} else if req.Spec != nil {
				instance.Deploy.CpuLimit = clampDeployRes(
					instance.InstanceName(), "cpu_limit",
					instance.Deploy.CpuLimit, updParsed.cpuMin, updParsed.cpuMax)
			}
			if req.Deploy != nil && req.Deploy.MemoryLimit > 0 {
				v, err := requireInRange("deploy.memory_limit",
					req.Deploy.MemoryLimit, updParsed.memMin, updParsed.memMax)
				if err != nil {
					return nil, err
				}
				instance.Deploy.MemoryLimit = v
			} else if req.Spec != nil {
				instance.Deploy.MemoryLimit = clampDeployRes(
					instance.InstanceName(), "memory_limit",
					instance.Deploy.MemoryLimit, updParsed.memMin, updParsed.memMax)
			}
			if req.Deploy != nil && req.Deploy.VolumeLimit > 0 {
				v, err := requireInRange("deploy.volume_limit",
					req.Deploy.VolumeLimit, updParsed.volMin, updParsed.volMax)
				if err != nil {
					return nil, err
				}
				instance.Deploy.VolumeLimit = v
			} else if req.Spec != nil {
				instance.Deploy.VolumeLimit = clampDeployRes(
					instance.InstanceName(), "volume_limit",
					instance.Deploy.VolumeLimit, updParsed.volMin, updParsed.volMax)
			}
		}

		if req.ReplicaCap > 0 {
			instance.Deploy.ReplicaCap = min(inapi.AppReplicaCapMax, req.ReplicaCap)
		}

		// Update deploy configs if provided
		if req.Deploy != nil && len(req.Deploy.Configs) > 0 {
			instance.Deploy.Configs = req.Deploy.Configs
		}

		// Update deploy action if provided. restart is a transient signal
		// rather than a recorded state: the hostlet rebuilds a container
		// whenever Deploy.Revision changes, and the unconditional revision
		// bump below therefore performs the stop/start cycle. Normalize
		// restart to start so the hostlet workflow (which only registers
		// start/stop/destroy) brings the container back up after recreate.
		if req.Deploy != nil && req.Deploy.Action != "" {
			if req.Deploy.Action == inapi.OpActionRestart {
				instance.Deploy.Action = inapi.OpActionStart
			} else {
				instance.Deploy.Action = req.Deploy.Action
			}
		}

		// Update deploy dependency bindings if provided
		if req.Deploy != nil && len(req.Deploy.Depends) > 0 {
			instance.Deploy.Depends = req.Deploy.Depends
		}

		// Resolve config field auto-fill values
		resolveDeployConfigFields(instance)

		instance.Deploy.Revision += 1

		// Detect dependency changes for reverse-reference updates
		prevDepNames := deployDependInstanceNames(existingByName.Deploy)
		nextDepNames := deployDependInstanceNames(instance.Deploy)

		appDeployStagesMarkPreHost(instance.Deploy, rpcStartMs)

		if rs := data.Zonelet.NewWriter(key, instance).SetPrevVersion(
			prevVersion).Exec(); !rs.OK() {
			return nil, rs.Error()
		}

		// Update reverse references (ref_by_instances) on dependent instances
		if err := s.updateDependencyReverseRefs(instance.InstanceName(), prevDepNames, nextDepNames); err != nil {
			slog.Error("zonelet app-instance-update: failed to update reverse refs",
				"instance_name", instance.InstanceName(),
				"err", err.Error())
		}

		slog.Warn("zonelet app-instance-update",
			"instance_name", instance.InstanceName(),
			"replica_cap", instance.Deploy.ReplicaCap,
			"action", instance.Deploy.Action,
		)
	} else {
		// Create a new instance

		// Check name uniqueness within the zone
		if err := validateInstanceNameUnique(req.Name); err != nil {
			return nil, err
		}

		// Default the runtime allocation to the spec min for each resource.
		// An explicit override in req.Deploy (e.g. --cpu 2000m) replaces the
		// min, validated strictly against the range.
		cpu := parsed.cpuMin
		mem := parsed.memMin
		vol := parsed.volMin
		if req.Deploy != nil {
			if v, err := requireInRange("deploy.cpu_limit",
				req.Deploy.CpuLimit, parsed.cpuMin, parsed.cpuMax); err != nil {
				return nil, err
			} else {
				cpu = v
			}
			if v, err := requireInRange("deploy.memory_limit",
				req.Deploy.MemoryLimit, parsed.memMin, parsed.memMax); err != nil {
				return nil, err
			} else {
				mem = v
			}
			if v, err := requireInRange("deploy.volume_limit",
				req.Deploy.VolumeLimit, parsed.volMin, parsed.volMax); err != nil {
				return nil, err
			} else {
				vol = v
			}
		}

		deploy := &inapi.AppDeploy{
			Action:      inapi.OpActionStart,
			CpuLimit:    cpu,
			MemoryLimit: mem,
			VolumeLimit: vol,
			ReplicaCap:  max(1, min(inapi.AppReplicaCapMax, req.ReplicaCap)),
			Revision:    1,
		}

		if req.Deploy != nil {
			// Set deploy configs if provided
			if len(req.Deploy.Configs) > 0 {
				deploy.Configs = req.Deploy.Configs
			}

			// Set deploy dependency bindings if provided
			if len(req.Deploy.Depends) > 0 {
				deploy.Depends = req.Deploy.Depends
			}
		}

		instance = &inapi.AppInstance{
			Meta: &inapi.Metadata{
				Id:      inutil.SeqRandHexString(4, 8),
				Name:    req.Name,
				Created: rpcStartMs / 1000,
				Updated: rpcStartMs / 1000,
			},
			Deploy: deploy,
			Spec:   req.Spec,
		}

		// Resolve config field auto-fill values
		resolveDeployConfigFields(instance)

		appDeployStagesMarkPreHost(instance.Deploy, rpcStartMs)

		key := inapi.NsAppInstance(config.Config.Zonelet.ZoneName, instance.InstanceName())

		if rs := data.Zonelet.NewWriter(key, instance).
			SetCreateOnly(true).Exec(); !rs.OK() {
			return nil, rs.Error()
		}

		// Update reverse references (ref_by_instances) on dependent instances
		depNames := deployDependInstanceNames(deploy)
		if err := s.updateDependencyReverseRefs(instance.InstanceName(), nil, depNames); err != nil {
			slog.Error("zonelet app-instance-deploy: failed to update reverse refs",
				"instance_name", instance.InstanceName(),
				"err", err.Error())
		}

		slog.Warn("zonelet app-instance-deploy",
			"instance_name", instance.InstanceName(),
			"replica_cap", instance.Deploy.ReplicaCap,
		)
	}

	status.Zonelet_ForceRefresh.Store(true)

	return &inapi.AppInstanceDeployResponse{
		Id:   instance.InstanceId(),
		Name: instance.InstanceName(),
	}, nil
}

func (s *zoneServer) AppInstanceInfo(
	ctx context.Context, req *inapi.AppInstanceInfoRequest,
) (*inapi.AppInstanceInfoResponse, error) {

	if !inauth.AppContext(ctx).Allow(inapi.AuthScope_App_Read) {
		return nil, errors.New("auth fail: missing app:ro scope")
	}

	if !status.IsZoneletLeader() {
		return nil, errors.New("zonelet leader")
	}

	if req.Name == "" {
		return nil, errors.New("name is required")
	}
	if err := inapi.DNSLabelValid(req.Name); err != nil {
		return nil, fmt.Errorf("name: %w", err)
	}

	var instance inapi.AppInstance

	key := inapi.NsAppInstance(config.Config.Zonelet.ZoneName, req.Name)
	if rs := data.Zonelet.NewReader(key).Exec(); !rs.OK() {
		if rs.NotFound() {
			return nil, errors.New("instance not found")
		}
		return nil, rs.Error()
	} else if err := rs.Item().JsonDecode(&instance); err != nil {
		return nil, err
	}

	return &inapi.AppInstanceInfoResponse{
		Instance: &instance,
	}, nil
}

func (s *zoneServer) AppInstanceList(
	ctx context.Context, req *inapi.AppInstanceListRequest,
) (*inapi.AppInstanceListResponse, error) {

	if !inauth.AppContext(ctx).Allow(inapi.AuthScope_App_Read) {
		return nil, errors.New("auth fail: missing app:ro scope")
	}

	if !status.IsZoneletLeader() {
		return nil, errors.New("zonelet leader")
	}

	resp := &inapi.AppInstanceListResponse{}

	offset := inapi.NsAppInstance(config.Config.Zonelet.ZoneName, "")

	rs := data.Zonelet.NewRanger(offset, append(offset, 0xff)).Exec()
	for _, item := range rs.Items {
		var instance inapi.AppInstance
		if err := item.JsonDecode(&instance); err == nil {
			resp.Items = append(resp.Items, &instance)
		}
	}

	return resp, nil
}

func (s *zoneServer) AppInstanceDelete(
	ctx context.Context, req *inapi.AppInstanceDeleteRequest,
) (*inapi.AppInstanceDeleteResponse, error) {

	if !inauth.AppContext(ctx).Allow(inapi.AuthScope_App_Write) {
		return nil, errors.New("auth fail: missing app:rw scope")
	}

	if !status.IsZoneletLeader() {
		return nil, errors.New("zonelet leader")
	}

	if req.Name == "" {
		return nil, errors.New("name is required")
	}
	if err := inapi.DNSLabelValid(req.Name); err != nil {
		return nil, fmt.Errorf("name: %w", err)
	}

	// Soft delete: rather than removing the key, mark Deploy.Action=delete and
	// apply a TTL. The hostlet tears the container down and archives its data,
	// while the instance stays readable (but read-only) until the store
	// physically removes the key after the TTL. This keeps the pull-based
	// hostlet model in sync: the instance is still delivered with action=delete
	// so the host actually reaps the container (a hard delete made the key
	// vanish and left the hostlet pinned to the stale action=start).
	instance, kvMeta, err := loadInstanceByName(req.Name)
	if err != nil {
		return nil, err
	}
	if instance == nil {
		return nil, errors.New("instance not found")
	}

	// Already soft-deleted: the instance is read-only until the TTL removes it.
	if instance.Deploy != nil && instance.Deploy.Action == inapi.OpActionDelete {
		return nil, fmt.Errorf(
			"instance %q is already marked deleted (read-only), it will be removed after TTL",
			req.Name)
	}

	if instance.Deploy == nil {
		instance.Deploy = &inapi.AppDeploy{}
	}
	instance.Deploy.Action = inapi.OpActionDelete

	key := inapi.NsAppInstance(config.Config.Zonelet.ZoneName, req.Name)

	rs := data.Zonelet.NewWriter(key, instance).
		SetPrevVersion(kvMeta.Version).
		SetTTL(inapi.AppInstanceSoftDeleteTTL).
		Exec()
	if !rs.OK() {
		return nil, rs.Error()
	}

	// Update the in-memory set immediately so the next status poll delivers
	// action=delete instead of the stale prior action.
	gAppInstanceSet.Store(instance, rs.Item().Meta)

	slog.Warn("zonelet app-instance-delete (soft)",
		"instance_name", req.Name,
		"ttl_ms", inapi.AppInstanceSoftDeleteTTL,
	)

	status.Zonelet_ForceRefresh.Store(true)

	return &inapi.AppInstanceDeleteResponse{}, nil
}

// parsedResources holds the int64 min/max ranges for the three deploy
// resources, derived from a normalized AppSpecResources.
type parsedResources struct {
	cpuMin, cpuMax int64
	memMin, memMax int64
	volMin, volMax int64
}

// parseSpecResources parses a normalized AppSpecResources into int64 ranges
// and validates system bounds and min<=max. The caller must run
// NormalizeLegacy first so the legacy single-value fields have been migrated
// into the range fields.
func parseSpecResources(res *inapi.AppSpecResources) (parsedResources, error) {
	var p parsedResources
	if res == nil {
		return p, errors.New("spec.resources is required")
	}
	var err error
	if p.cpuMin, err = inutil.ParseCPUs(res.CpuMin); err != nil {
		return p, fmt.Errorf("invalid cpu_min: %w", err)
	}
	if p.cpuMax, err = inutil.ParseCPUs(res.CpuMax); err != nil {
		return p, fmt.Errorf("invalid cpu_max: %w", err)
	}
	if p.memMin, err = inutil.ParseBytes(res.MemoryMin); err != nil {
		return p, fmt.Errorf("invalid memory_min: %w", err)
	}
	if p.memMax, err = inutil.ParseBytes(res.MemoryMax); err != nil {
		return p, fmt.Errorf("invalid memory_max: %w", err)
	}
	if p.volMin, err = inutil.ParseBytes(res.VolumeMin); err != nil {
		return p, fmt.Errorf("invalid volume_min: %w", err)
	}
	if p.volMax, err = inutil.ParseBytes(res.VolumeMax); err != nil {
		return p, fmt.Errorf("invalid volume_max: %w", err)
	}

	if p.cpuMin < inapi.CPUMin || p.cpuMin > inapi.CPUMax ||
		p.cpuMax < inapi.CPUMin || p.cpuMax > inapi.CPUMax {
		return p, fmt.Errorf("spec.cpu_min/cpu_max must be between %d and %d",
			inapi.CPUMin, inapi.CPUMax)
	}
	if p.memMin < inapi.MemoryMin || p.memMin > inapi.MemoryMax ||
		p.memMax < inapi.MemoryMin || p.memMax > inapi.MemoryMax {
		return p, fmt.Errorf("spec.memory_min/memory_max must be between %d and %d",
			inapi.MemoryMin, inapi.MemoryMax)
	}
	if p.volMin < inapi.VolumeMin || p.volMin > inapi.VolumeMax ||
		p.volMax < inapi.VolumeMin || p.volMax > inapi.VolumeMax {
		return p, fmt.Errorf("spec.volume_min/volume_max must be between %d and %d",
			inapi.VolumeMin, inapi.VolumeMax)
	}

	if p.cpuMin > p.cpuMax {
		return p, errors.New("spec.cpu_min must be <= cpu_max")
	}
	if p.memMin > p.memMax {
		return p, errors.New("spec.memory_min must be <= memory_max")
	}
	if p.volMin > p.volMax {
		return p, errors.New("spec.volume_min must be <= volume_max")
	}
	return p, nil
}

// resolveInRange returns v clamped into [lo, hi]. A non-positive v yields lo
// (the deploy default). Used to keep an existing deploy value when a spec is
// updated without an explicit resource override, nudging it into a narrowed
// range if needed.
func resolveInRange(v, lo, hi int64) int64 {
	if v <= 0 {
		return lo
	}
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// requireInRange returns v unchanged if it is within [lo, hi]; a zero v yields
// lo (the default). An out-of-range non-zero v yields an error. Used for
// explicit operator overrides where silent clamping would hide a typo.
func requireInRange(name string, v, lo, hi int64) (int64, error) {
	if v == 0 {
		return lo, nil
	}
	if v < lo || v > hi {
		return 0, fmt.Errorf("%s %d is out of range [%d, %d]", name, v, lo, hi)
	}
	return v, nil
}

// clampDeployRes returns the existing deploy resource value clamped into
// [lo, hi] via resolveInRange, logging a warn when the spec range forced a
// change. Used on the no-override update path.
func clampDeployRes(instanceName, name string, v, lo, hi int64) int64 {
	next := resolveInRange(v, lo, hi)
	if next != v {
		slog.Warn("zonelet app-instance-update: clamped deploy resource",
			"instance_name", instanceName,
			"resource", name,
			"prev", v, "next", next)
	}
	return next
}

// loadInstanceByName loads an app instance by its name (the logical key).
// Returns (nil, nil, nil) when not found.
func loadInstanceByName(name string) (*inapi.AppInstance, *kvapi.Meta, error) {
	if name == "" {
		return nil, nil, nil
	}
	key := inapi.NsAppInstance(config.Config.Zonelet.ZoneName, name)
	rs := data.Zonelet.NewReader(key).Exec()
	if !rs.OK() {
		if rs.NotFound() {
			return nil, nil, nil
		}
		return nil, nil, rs.Error()
	}
	var instance inapi.AppInstance
	if err := rs.Item().JsonDecode(&instance); err != nil {
		return nil, nil, err
	}
	return &instance, rs.Item().Meta, nil
}

// appDeployStagesMarkPreHost records the zone-side, pre-host stages of a
// deploy lifecycle on the AppDeploy root stage: the root is marked running,
// req_validate is marked successful (spanning from rpcStartMs to now), and
// instance_persist is recorded as an instantaneous event. Existing per-
// replica stage subtrees are preserved.
//
// The root's Revision is intentionally left at its previous value: it is a
// sync barrier meaning "the revision the zone leader has reconciled into the
// stage tree". The scheduler advances it to the current Deploy.Revision via
// schedulerReconcileDeployRevision once stale replica stages have been
// cleared. This lets clients (the CLI watch) wait for a stage tree that
// actually reflects the new revision rather than reading back stale state.
func appDeployStagesMarkPreHost(deploy *inapi.AppDeploy, rpcStartMs int64) {
	if deploy == nil {
		return
	}
	rev := deploy.Revision
	root := deploy.StagesRoot()
	root.SetRunning("")

	rv := root.Child(inapi.AppDeployStageNameReqValidate, inapi.AppStageOwnerZonelet)
	rv.Created = rpcStartMs // reset to this deploy's start on every submit
	rv.SetSuccess("")
	rv.Revision = rev

	ip := root.Child(inapi.AppDeployStageNameInstancePersist, inapi.AppStageOwnerZonelet)
	ip.SetInstant("")
	ip.Revision = rev
}

// appDeployStagesReconcile advances the deploy stage root to the current
// Deploy.Revision, clearing stale relayed (host-side/inagent) replica stages
// left from a prior revision. It is the scheduler-side complement to
// appDeployStagesMarkPreHost: the deploy RPC bumps Deploy.Revision but leaves
// root.Revision behind as a sync barrier; this function lifts the barrier once
// the stage tree has been reconciled to the new revision.
//
// It mutates only the in-memory stage tree and returns whether a reconcile was
// performed (false when the root is already current, i.e. a no-op). The caller
// persists the result.
func appDeployStagesReconcile(deploy *inapi.AppDeploy) bool {
	if deploy == nil {
		return false
	}
	curRev := deploy.Revision
	root := deploy.StagesRoot()
	if root.Revision >= curRev {
		return false
	}
	root.SetRunning("")
	// Drop stale relayed children from each replica node; the hostlet
	// re-reports them at the current revision. Zone-side children
	// (schedule/ipam_alloc/port_alloc/deliver) stay valid for placed replicas.
	for _, repNode := range root.Stages {
		if repNode == nil || repNode.Name != inapi.AppDeployStageNameReplica {
			continue
		}
		repNode.PruneChildren(func(name string) bool {
			_, isRelayed := inapi.AppDeployStageRelayedNames[name]
			return !isRelayed
		})
	}
	// Stamp the root and its remaining descendants with the current revision.
	root.SetRevisionDeep(curRev)
	return true
}

// validateInstanceNameUnique checks that no other app instance in the zone
// has the given name.
func validateInstanceNameUnique(name string) error {
	// Fast path: direct key read (the name is the logical key).
	if found, _, err := loadInstanceByName(name); err != nil {
		return err
	} else if found != nil {
		return fmt.Errorf("app name %q already in use", name)
	}

	// Defensive scan to guard against legacy data keyed by id.
	offset := inapi.NsAppInstance(config.Config.Zonelet.ZoneName, "")
	rs := data.Zonelet.NewRanger(offset, append(offset, 0xff)).Exec()

	for _, item := range rs.Items {
		var inst inapi.AppInstance
		if err := item.JsonDecode(&inst); err != nil {
			continue
		}
		if inst.InstanceName() == name {
			return fmt.Errorf("app name %q already in use", name)
		}
	}
	return nil
}

// validateAppDependencies checks that all app-level dependencies
// (spec.depends) reference existing deployed app instances.
// Each dependency's name must match an existing instance's spec.name.
func validateAppDependencies(depends []*inapi.AppSpecDepend) error {
	if len(depends) == 0 {
		return nil
	}

	// Load all deployed app instances, indexed by spec.name for O(1) lookup.
	offset := inapi.NsAppInstance(config.Config.Zonelet.ZoneName, "")
	rs := data.Zonelet.NewRanger(offset, append(offset, 0xff)).Exec()

	available := make(map[string]struct{}, len(rs.Items))

	for _, item := range rs.Items {
		var inst inapi.AppInstance
		if err := item.JsonDecode(&inst); err != nil {
			continue
		}
		if inst.Spec == nil || inst.Spec.Name == "" {
			continue
		}
		available[inst.Spec.Name] = struct{}{}
	}

	for _, dep := range depends {
		if dep == nil {
			continue
		}
		if dep.Name == "" {
			return errors.New("app dependency name is required")
		}

		if _, exists := available[dep.Name]; !exists {
			return fmt.Errorf("app dependency %q has no deployed instance", dep.Name)
		}
	}

	return nil
}

// resolveDeployConfigFields resolves config field auto-fill values and defaults
// for an app instance. For each spec config field it ensures a deploy value
// exists, applying auto-fill expressions or defaults as needed. Flat fields are
// resolved directly; "group" fields resolve their single child set; "array_group"
// fields resolve each replicated instance's child set (so e.g. each database
// gets its own auto-generated password). Existing non-empty values are always
// preserved (idempotent on update).
func resolveDeployConfigFields(instance *inapi.AppInstance) {
	if instance == nil || instance.Spec == nil ||
		len(instance.Spec.Configs) == 0 || instance.Deploy == nil {
		return
	}

	for _, field := range instance.Spec.Configs {
		if field == nil || field.Name == "" {
			continue
		}

		switch field.Type {
		case inapi.SpecFieldTypeGroup, inapi.SpecFieldTypeArrayGroup:
			resolveGroupedConfigField(field, instance.Deploy)
		default:
			resolveFlatConfigField(field, instance.Deploy)
		}
	}
}

// configFieldAutoValue derives a value for a spec config field from its
// auto-fill expression or default. Returns "" when no value can be derived.
func configFieldAutoValue(field *inapi.AppSpecConfigItem) string {
	if field == nil {
		return ""
	}
	if field.AutoFill != "" {
		val, err := autofill.GenerateIfEmpty(field.AutoFill, "")
		if err != nil {
			slog.Warn("config auto-fill failed",
				"field", field.Name,
				"autofill", field.AutoFill,
				"err", err.Error())
			return ""
		}
		if val != "" {
			return val
		}
		if field.Default != "" {
			return field.Default
		}
		return ""
	}
	return field.Default
}

// resolveFlatConfigField applies auto-fill/default to a flat deploy config
// field, preserving any existing non-empty value.
func resolveFlatConfigField(field *inapi.AppSpecConfigItem, deploy *inapi.AppDeploy) {
	idx := slices.IndexFunc(deploy.Configs, func(v *inapi.AppDeployConfigItem) bool {
		return v != nil && v.Name == field.Name
	})
	if idx >= 0 && deploy.Configs[idx].Value != "" {
		return
	}
	val := configFieldAutoValue(field)
	if val == "" {
		return
	}
	if idx >= 0 {
		deploy.Configs[idx].Value = val
		return
	}
	deploy.Configs = append(deploy.Configs, &inapi.AppDeployConfigItem{
		Name:  field.Name,
		Value: val,
	})
}

// resolveGroupedConfigField resolves child-field values within a "group" or
// "array_group" deploy item. For a group the deploy item's items hold the field
// values directly; for an array_group each element of the deploy item's items is
// a replicated instance whose own items hold the field values. Empty child
// values are filled from each child spec field's auto-fill/default; existing
// values are preserved.
func resolveGroupedConfigField(field *inapi.AppSpecConfigItem, deploy *inapi.AppDeploy) {
	if len(field.Items) == 0 {
		return
	}
	idx := slices.IndexFunc(deploy.Configs, func(v *inapi.AppDeployConfigItem) bool {
		return v != nil && v.Name == field.Name
	})
	if idx < 0 {
		return
	}
	item := deploy.Configs[idx]

	if field.Type == inapi.SpecFieldTypeArrayGroup {
		for _, inst := range item.Items {
			if inst == nil {
				continue
			}
			resolveChildConfigFields(field.Items, &inst.Items)
		}
		return
	}
	// group: the deploy item's items are the field values.
	resolveChildConfigFields(field.Items, &item.Items)
}

// resolveChildConfigFields fills empty child deploy values referenced by dst
// using the child spec field definitions (specItems), applying auto-fill/
// default. Missing children with a derivable value are appended.
func resolveChildConfigFields(
	specItems []*inapi.AppSpecConfigItem,
	dst *[]*inapi.AppDeployConfigItem,
) {
	for _, sf := range specItems {
		if sf == nil || sf.Name == "" {
			continue
		}
		var cur *inapi.AppDeployConfigItem
		for _, d := range *dst {
			if d != nil && d.Name == sf.Name {
				cur = d
				break
			}
		}
		if cur != nil && cur.Value != "" {
			continue
		}
		val := configFieldAutoValue(sf)
		if val == "" {
			continue
		}
		if cur != nil {
			cur.Value = val
		} else {
			*dst = append(*dst, &inapi.AppDeployConfigItem{Name: sf.Name, Value: val})
		}
	}
}

// validateDeployDependencies validates that each deploy-time dependency binding
// references an existing, valid app instance. It checks:
//   - instance_name is not empty
//   - the target instance exists in the zone
//   - the target instance's spec.name matches the declared spec_name
//   - self-reference is not allowed
func validateDeployDependencies(
	currentSpecName string, depends []*inapi.AppDeployDepend,
) error {
	if len(depends) == 0 {
		return nil
	}

	for _, dep := range depends {
		if dep == nil {
			continue
		}
		if dep.SpecName == "" {
			return errors.New("deploy dependency spec_name is required")
		}
		if dep.InstanceName == "" {
			return fmt.Errorf("deploy dependency %q: instance_name is required", dep.SpecName)
		}

		// Load target instance by name (the logical key)
		target, _, err := loadInstanceByName(dep.InstanceName)
		if err != nil {
			return fmt.Errorf("deploy dependency %q: failed to query instance: %w",
				dep.SpecName, err)
		}
		if target == nil {
			return fmt.Errorf("deploy dependency %q: instance %q not found",
				dep.SpecName, dep.InstanceName)
		}

		// Verify spec.name match
		if target.Spec == nil || target.Spec.Name != dep.SpecName {
			return fmt.Errorf(
				"deploy dependency %q: instance %q has spec.name %q, expected %q",
				dep.SpecName, dep.InstanceName, target.Spec.GetName(), dep.SpecName)
		}

		// Prevent self-reference
		if target.Spec.Name == currentSpecName {
			return fmt.Errorf("deploy dependency %q: self-reference is not allowed",
				dep.SpecName)
		}
	}

	return nil
}

// deployDependInstanceNames extracts the set of dependency instance names from
// an AppDeploy's depends list. Returns nil if deploy is nil or has no depends.
func deployDependInstanceNames(deploy *inapi.AppDeploy) map[string]struct{} {
	if deploy == nil {
		return nil
	}
	names := make(map[string]struct{}, len(deploy.Depends))
	for _, dep := range deploy.Depends {
		if dep != nil && dep.InstanceName != "" {
			names[dep.InstanceName] = struct{}{}
		}
	}
	return names
}

// updateDependencyReverseRefs maintains the reverse dependency links
// (ref_by_instances) on all affected target instances. When an app instance's
// dependency bindings change, this function:
//   - removes the instance name from ref_by_instances of previously depended instances
//   - adds the instance name to ref_by_instances of newly depended instances
func (s *zoneServer) updateDependencyReverseRefs(
	instanceName string,
	prevDepNames, nextDepNames map[string]struct{},
) error {

	if len(prevDepNames) == 0 && len(nextDepNames) == 0 {
		return nil
	}

	dels := map[string]bool{}
	for name := range nextDepNames {
		delete(prevDepNames, name)
		if _, ok := prevDepNames[name]; ok {
			continue
		}
		dels[name] = true
	}
	for name := range prevDepNames {
		dels[name] = false
	}

	for name, add := range dels {

		var (
			key         = inapi.NsAppInstance(config.Config.Zonelet.ZoneName, name)
			depInst     inapi.AppInstance
			prevVersion uint64
		)

		if rs := data.Zonelet.NewReader(key).Exec(); !rs.OK() {
			if rs.NotFound() {
				slog.Warn("updateDependencyReverseRefs: dependent instance not found",
					"dep_instance_name", name)
				continue
			}
			return fmt.Errorf("[zonelet.updateDependencyReverseRefs] read instance %s: %w",
				name, rs.Error())
		} else if err := rs.Item().JsonDecode(&depInst); err != nil {
			return fmt.Errorf("[zonelet.updateDependencyReverseRefs] decode instance %s: %w",
				name, err)
		} else {
			prevVersion = rs.Item().Meta.Version
		}

		if add {
			if slices.Contains(depInst.RefByInstances, instanceName) {
				continue
			}
			depInst.RefByInstances = append(depInst.RefByInstances, instanceName)
		} else {
			if !slices.Contains(depInst.RefByInstances, instanceName) {
				continue
			}
			depInst.RefByInstances = slices.DeleteFunc(depInst.RefByInstances, func(v string) bool {
				return v == instanceName
			})
		}

		if rs := data.Zonelet.NewWriter(key, &depInst).
			SetPrevVersion(prevVersion).Exec(); !rs.OK() {
			return fmt.Errorf("[zonelet.updateDependencyReverseRefs] write instance %s: %w",
				name, rs.Error())
		}
	}

	return nil
}

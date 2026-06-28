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
		cpuLimit, memoryLimit, volumeLimit int64

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

		if req.Spec.Resources.CpuLimit != "" {
			if v, err := inutil.ParseCPUs(req.Spec.Resources.CpuLimit); err != nil {
				return nil, fmt.Errorf("invalid cpu_limit: %w", err)
			} else {
				cpuLimit = v
			}
		}

		if req.Spec.Resources.MemoryLimit != "" {
			if v, err := inutil.ParseBytes(req.Spec.Resources.MemoryLimit); err != nil {
				return nil, fmt.Errorf("invalid memory_limit: %w", err)
			} else {
				memoryLimit = v
			}
		}

		if req.Spec.Resources.VolumeLimit != "" {
			if v, err := inutil.ParseBytes(req.Spec.Resources.VolumeLimit); err != nil {
				return nil, fmt.Errorf("invalid volume_limit: %w", err)
			} else {
				volumeLimit = v
			}
		}

		if cpuLimit < inapi.CPUMin || cpuLimit > inapi.CPUMax {
			return nil, fmt.Errorf("spec.cpu_limit must be between %d and %d",
				inapi.CPUMin, inapi.CPUMax)
		}

		if memoryLimit < inapi.MemoryMin || memoryLimit > inapi.MemoryMax {
			return nil, fmt.Errorf("spec.memory_limit must be between %d and %d",
				inapi.MemoryMin, inapi.MemoryMax)
		}

		if volumeLimit < inapi.VolumeMin || volumeLimit > inapi.VolumeMax {
			return nil, fmt.Errorf("spec.volume_limit must be between %d and %d",
				inapi.VolumeMin, inapi.VolumeMax)
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

		// Update resource limits only if spec is provided
		if req.Spec != nil {
			instance.Deploy.CpuLimit = cpuLimit
			instance.Deploy.MemoryLimit = memoryLimit
			instance.Deploy.VolumeLimit = volumeLimit
		}

		if req.ReplicaCap > 0 {
			instance.Deploy.ReplicaCap = min(inapi.AppReplicaCapMax, req.ReplicaCap)
		}

		// Update deploy configs if provided
		if req.Deploy != nil && len(req.Deploy.Configs) > 0 {
			instance.Deploy.Configs = req.Deploy.Configs
		}

		// Update deploy action if provided
		if req.Deploy != nil && req.Deploy.Action != "" {
			instance.Deploy.Action = req.Deploy.Action
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

		deploy := &inapi.AppDeploy{
			Action:      inapi.OpActionStart,
			CpuLimit:    cpuLimit,
			MemoryLimit: memoryLimit,
			VolumeLimit: volumeLimit,
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
				Id:   inutil.SeqRandHexString(4, 8),
				Name: req.Name,
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

	key := inapi.NsAppInstance(config.Config.Zonelet.ZoneName, req.Name)

	if rs := data.Zonelet.NewDeleter(key).Exec(); !rs.OK() {
		if rs.NotFound() {
			return nil, errors.New("instance not found")
		}
		return nil, rs.Error()
	}

	slog.Warn("zonelet app-instance-delete",
		"instance_name", req.Name,
	)

	status.Zonelet_ForceRefresh.Store(true)

	return &inapi.AppInstanceDeleteResponse{}, nil
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
func appDeployStagesMarkPreHost(deploy *inapi.AppDeploy, rpcStartMs int64) {
	if deploy == nil {
		return
	}
	rev := deploy.Revision
	root := deploy.StagesRoot()
	root.SetRunning("")
	root.Revision = rev

	rv := root.Child(inapi.AppDeployStageNameReqValidate, inapi.AppStageOwnerZonelet)
	rv.Created = rpcStartMs // reset to this deploy's start on every submit
	rv.SetSuccess("")
	rv.Revision = rev

	ip := root.Child(inapi.AppDeployStageNameInstancePersist, inapi.AppStageOwnerZonelet)
	ip.SetInstant("")
	ip.Revision = rev
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
// for an app instance. If Spec.Config is set, it ensures all config fields have
// values in Deploy.Configs, applying auto-fill expressions or defaults as needed.
func resolveDeployConfigFields(instance *inapi.AppInstance) {
	if instance == nil || instance.Spec == nil ||
		len(instance.Spec.Configs) == 0 || instance.Deploy == nil {
		return
	}

	// Resolve each config field
	for _, field := range instance.Spec.Configs {
		if field == nil || field.Name == "" {
			continue
		}

		var currentValue string

		idx := slices.IndexFunc(instance.Deploy.Configs, func(v *inapi.AppDeployConfigItem) bool {
			return v.Name == field.Name
		})
		if idx >= 0 {
			if instance.Deploy.Configs[idx].Value != "" {
				continue
			}
			currentValue = instance.Deploy.Configs[idx].Value
		}

		// Apply auto-fill or default
		if field.AutoFill != "" {
			val, err := autofill.GenerateIfEmpty(field.AutoFill, "")
			if err != nil {
				slog.Warn("config auto-fill failed",
					"field", field.Name,
					"autofill", field.AutoFill,
					"err", err.Error())
				continue
			}
			if val != "" {
				currentValue = val
			} else if field.Default != "" {
				currentValue = field.Default
			}
		} else if field.Default != "" {
			currentValue = field.Default
		}

		if currentValue == "" {
			continue
		}

		if idx >= 0 {
			instance.Deploy.Configs[idx].Value = currentValue
		} else {
			instance.Deploy.Configs = append(instance.Deploy.Configs, &inapi.AppDeployConfigItem{
				Name:  field.Name,
				Value: currentValue,
			})
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

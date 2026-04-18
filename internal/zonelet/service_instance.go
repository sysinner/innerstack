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

package zonelet

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"

	"github.com/sysinner/incore/v2/inapi"
	"github.com/sysinner/incore/v2/internal/config"
	"github.com/sysinner/incore/v2/internal/data"
	"github.com/sysinner/incore/v2/internal/inutil"
	"github.com/sysinner/incore/v2/internal/inutil/autofill"
	"github.com/sysinner/incore/v2/internal/status"
	"github.com/sysinner/incore/v2/pkg/inauth"
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

	// For new instances, spec is required
	if req.Id == "" && req.Spec == nil {
		return nil, errors.New("spec is required for new instance")
	}

	var cpuLimit, memoryLimit, volumeLimit int64

	if req.Spec != nil {
		if req.Spec.Name == "" {
			return nil, errors.New("spec.name is required")
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
	}

	var instance *inapi.AppInstance

	if req.Id != "" {
		// Update existing instance
		var (
			key                     = inapi.NsAppInstance(config.Config.Zonelet.ZoneName, req.Id)
			prevVersion      uint64 = 0
			existingInstance inapi.AppInstance
		)

		if rs := data.Zonelet.NewReader(key).Exec(); !rs.OK() {
			if rs.NotFound() {
				return nil, errors.New("instance not found")
			}
			return nil, rs.Error()
		} else if err := rs.Item().JsonDecode(&existingInstance); err != nil {
			return nil, err
		} else {
			prevVersion = rs.Item().Meta.Version
		}

		instance = &existingInstance

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
			instance.Deploy.ReplicaCap = min(128, req.ReplicaCap)
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

		instance.Deploy.Version += 1

		// Detect dependency changes for reverse-reference updates
		prevDepIDs := deployDependInstanceIDs(existingInstance.Deploy)
		nextDepIDs := deployDependInstanceIDs(instance.Deploy)

		if rs := data.Zonelet.NewWriter(key, instance).SetPrevVersion(
			prevVersion).Exec(); !rs.OK() {
			return nil, rs.Error()
		}

		// Update reverse references (ref_by_instance_ids) on dependent instances
		if err := s.updateDependencyReverseRefs(req.Id, prevDepIDs, nextDepIDs); err != nil {
			slog.Error("zonelet app-instance-update: failed to update reverse refs",
				"instance_id", req.Id,
				"err", err.Error())
		}

		slog.Warn("zonelet app-instance-update",
			"instance_id", req.Id,
			"instance_name", instance.Name,
			"replica_cap", instance.Deploy.ReplicaCap,
			"action", instance.Deploy.Action,
		)
	} else {
		// 创建新实例

		deploy := &inapi.AppDeploy{
			Action:      inapi.OpActionStart,
			CpuLimit:    cpuLimit,
			MemoryLimit: memoryLimit,
			VolumeLimit: volumeLimit,
			ReplicaCap:  max(1, min(128, req.ReplicaCap)),
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
			Id:     inutil.SeqRandHexString(4, 8),
			Name:   req.Spec.Name,
			Deploy: deploy,
			Spec:   req.Spec,
		}

		// Resolve config field auto-fill values
		resolveDeployConfigFields(instance)

		key := inapi.NsAppInstance(config.Config.Zonelet.ZoneName, instance.Id)

		if rs := data.Zonelet.NewWriter(key, instance).
			SetCreateOnly(true).Exec(); !rs.OK() {
			return nil, rs.Error()
		}

		// Update reverse references (ref_by_instance_ids) on dependent instances
		depIDs := deployDependInstanceIDs(deploy)
		if err := s.updateDependencyReverseRefs(instance.Id, nil, depIDs); err != nil {
			slog.Error("zonelet app-instance-deploy: failed to update reverse refs",
				"instance_id", instance.Id,
				"err", err.Error())
		}

		slog.Warn("zonelet app-instance-deploy",
			"instance_id", instance.Id,
			"instance_name", instance.Name,
			"replica_cap", instance.Deploy.ReplicaCap,
		)
	}

	status.Zonelet_ForceRefresh.Store(true)

	return &inapi.AppInstanceDeployResponse{
		Id: instance.Id,
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

	if req.Id == "" {
		return nil, errors.New("id is required")
	}

	var instance inapi.AppInstance

	if rs := data.Zonelet.NewReader(
		inapi.NsAppInstance(config.Config.Zonelet.ZoneName, req.Id)).Exec(); !rs.OK() {
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

	if req.Id == "" {
		return nil, errors.New("id is required")
	}

	key := inapi.NsAppInstance(config.Config.Zonelet.ZoneName, req.Id)

	if rs := data.Zonelet.NewDeleter(key).Exec(); !rs.OK() {
		if rs.NotFound() {
			return nil, errors.New("instance not found")
		}
		return nil, rs.Error()
	}

	slog.Warn("zonelet app-instance-delete",
		"instance_id", req.Id,
	)

	status.Zonelet_ForceRefresh.Store(true)

	return &inapi.AppInstanceDeleteResponse{}, nil
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
//   - instance_id is not empty
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
		if dep.InstanceId == "" {
			return fmt.Errorf("deploy dependency %q: instance_id is required", dep.SpecName)
		}

		// Load target instance
		key := inapi.NsAppInstance(config.Config.Zonelet.ZoneName, dep.InstanceId)
		var target inapi.AppInstance
		if rs := data.Zonelet.NewReader(key).Exec(); !rs.OK() {
			if rs.NotFound() {
				return fmt.Errorf("deploy dependency %q: instance %q not found",
					dep.SpecName, dep.InstanceId)
			}
			return fmt.Errorf("deploy dependency %q: failed to query instance: %w",
				dep.SpecName, rs.Error())
		} else if err := rs.Item().JsonDecode(&target); err != nil {
			return fmt.Errorf("deploy dependency %q: failed to decode instance: %w",
				dep.SpecName, err)
		}

		// Verify spec.name match
		if target.Spec == nil || target.Spec.Name != dep.SpecName {
			return fmt.Errorf(
				"deploy dependency %q: instance %q has spec.name %q, expected %q",
				dep.SpecName, dep.InstanceId, target.Spec.GetName(), dep.SpecName)
		}

		// Prevent self-reference
		if target.Spec.Name == currentSpecName {
			return fmt.Errorf("deploy dependency %q: self-reference is not allowed",
				dep.SpecName)
		}
	}

	return nil
}

// deployDependInstanceIDs extracts the set of dependency instance IDs from
// an AppDeploy's depends list. Returns nil if deploy is nil or has no depends.
func deployDependInstanceIDs(deploy *inapi.AppDeploy) map[string]struct{} {
	if deploy == nil {
		return nil
	}
	ids := make(map[string]struct{}, len(deploy.Depends))
	for _, dep := range deploy.Depends {
		if dep != nil && dep.InstanceId != "" {
			ids[dep.InstanceId] = struct{}{}
		}
	}
	return ids
}

// updateDependencyReverseRefs maintains the reverse dependency links
// (ref_by_instance_ids) on all affected target instances. When an app instance's
// dependency bindings change, this function:
//   - removes the instance ID from ref_by_instance_ids of previously depended instances
//   - adds the instance ID to ref_by_instance_ids of newly depended instances
func (s *zoneServer) updateDependencyReverseRefs(
	instanceID string,
	prevDepIDs, nextDepIDs map[string]struct{},
) error {

	if len(prevDepIDs) == 0 && len(nextDepIDs) == 0 {
		return nil
	}

	dels := map[string]bool{}
	for id := range nextDepIDs {
		delete(prevDepIDs, id)
		if _, ok := prevDepIDs[id]; ok {
			continue
		}
		dels[id] = true
	}
	for id := range prevDepIDs {
		dels[id] = false
	}

	for id, add := range dels {

		var (
			key         = inapi.NsAppInstance(config.Config.Zonelet.ZoneName, id)
			depInst     inapi.AppInstance
			prevVersion uint64
		)

		if rs := data.Zonelet.NewReader(key).Exec(); !rs.OK() {
			if rs.NotFound() {
				slog.Warn("updateDependencyReverseRefs: dependent instance not found",
					"dep_instance_id", id)
				continue
			}
			return fmt.Errorf("[zonelet.updateDependencyReverseRefs] read instance %s: %w",
				id, rs.Error())
		} else if err := rs.Item().JsonDecode(&depInst); err != nil {
			return fmt.Errorf("[zonelet.updateDependencyReverseRefs] decode instance %s: %w",
				id, err)
		} else {
			prevVersion = rs.Item().Meta.Version
		}

		if add {
			if slices.Contains(depInst.RefByInstances, instanceID) {
				continue
			}
			depInst.RefByInstances = append(depInst.RefByInstances, instanceID)
		} else {
			if !slices.Contains(depInst.RefByInstances, instanceID) {
				continue
			}
			depInst.RefByInstances = slices.DeleteFunc(depInst.RefByInstances, func(v string) bool {
				return v == instanceID
			})
		}

		if rs := data.Zonelet.NewWriter(key, &depInst).
			SetPrevVersion(prevVersion).Exec(); !rs.OK() {
			return fmt.Errorf("[zonelet.updateDependencyReverseRefs] write instance %s: %w",
				id, rs.Error())
		}
	}

	return nil
}

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

package hostlet

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"os"
	"time"

	"github.com/sysinner/incore/v2/inapi"
	"github.com/sysinner/incore/v2/internal/config"
	"github.com/sysinner/incore/v2/internal/hostlet/docker"
	"github.com/sysinner/incore/v2/internal/hostlet/hostapi"
	"github.com/sysinner/incore/v2/internal/hostlet/hoststatus"
	"github.com/sysinner/incore/v2/internal/inutil"
)

var (
	hostSrcPaths = hostapi.NewHostSourcePaths(config.Prefix)
)

const (
	defaultContainerTimeout = 30 * time.Second
	imagePullTimeout        = 5 * time.Minute
)

var (
	ctrDriver  hostapi.Driver
	ctrDrivers []hostapi.Driver
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
//	- starting  + success → running
//	- starting  + fail    → failed
//	- stopping  + success → stopped
//	- stopping  + fail    → failed
//	- destroying+ success → destroyed
//	- destroying+ fail    → failed
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
	return nil
}

// containerStatusRefresh initializes driver and refreshes container/image cache.
func containerStatusRefresh() error {
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
		}
	}
	hoststatus.ContainerReady.Store(true)

	return nil
}

// containerControlRefresh processes app replica state transitions via workflow.
func containerControlRefresh() error {
	if !hoststatus.HostReady.Load() ||
		!hoststatus.ContainerReady.Load() ||
		ctrDriver == nil {
		return nil
	}

	hoststatus.ActiveAppList.Range(func(key, value any) bool {
		app, ok := value.(*inapi.AppInstance)
		if !ok || app.Spec == nil ||
			app.Spec.Resources == nil ||
			app.Deploy == nil || len(app.Deploy.Replicas) == 0 {
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
			repInstance := &hostapi.AppReplicaInstance{
				App:     app,
				Replica: rep,
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
			}

			// Get next command from workflow state machine
			cmd, ok := hoststatus.AppWorkflow.NextCommand(currentState, app.Deploy.Action)
			if !ok || cmd == nil {
				continue
			}

			// If command is nil, state is already at target (no action needed)
			// Just update the state and continue
			if cmd.Command == nil {
				continue
			}

			repInstance.Replica.State = cmd.State // pendding

			// Execute state transition command
			nextState, err := cmd.Command(repInstance)
			if err != nil {
				slog.Warn("app operate command failed",
					"app", app.Id,
					"replica", rep.Id,
					"state", currentState,
					"action", app.Deploy.Action,
					"next_state", nextState,
					"error", err)
			} else {
				slog.Info("app operate command succeeded",
					"app", app.Id,
					"replica", rep.Id,
					"state", currentState,
					"action", app.Deploy.Action,
					"next_state", nextState)
			}
			if nextState != "" {
				repInstance.Replica.State = nextState
			}
		}
		return true
	})

	return nil
}

// containerStateSync syncs container actual state to replica state.
// State is already mapped to inapi.OpState* by docker driver.
func containerStateSync(rep *hostapi.AppReplicaInstance) string {
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

// containerSpecReset checks if container spec differs from desired config.
// Returns true if container needs to be recreated.
//
// Checked specs:
//   - Image: must match exactly
//   - CpuLimit: tolerance 1% (millicores)
//   - MemoryLimit: tolerance 1% (bytes)
//   - ServicePorts: port mappings must match
func containerSpecReset(rep *hostapi.AppReplicaInstance) bool {

	ctrInfo, exists := hoststatus.ContainerList.Load(rep.ContainerName())
	if !exists || ctrInfo == nil {
		return false
	}

	info, ok := ctrInfo.(*hostapi.ContainerInfo)
	if !ok {
		return false
	}

	tn := time.Now().Unix()

	// Fetch detailed container info if resource limits are missing from cache
	if info.LastInspectTime+1800 < tn {
		ctx, cancel := context.WithTimeout(context.Background(), defaultContainerTimeout)
		if ifo, err := ctrDriver.ContainerInspect(ctx, rep.ContainerName()); err == nil {
			info.CpuLimit = ifo.CpuLimit
			info.MemoryLimit = ifo.MemoryLimit
			info.Ports = ifo.Ports
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
		slog.Debug("container spec reset: image mismatch",
			"desired", rep.App.Spec.Image,
			"current", info.Image)
		return true
	}

	// Check 2: CpuLimit with 1% tolerance
	if relDiff(rep.App.Deploy.CpuLimit, info.CpuLimit) > 0.01 {
		slog.Debug("container spec reset: cpu limit mismatch",
			"desired", rep.App.Deploy.CpuLimit,
			"current", info.CpuLimit)
		return true
	}

	// Check 3: MemoryLimit with 1% tolerance
	if relDiff(rep.App.Deploy.MemoryLimit, info.MemoryLimit) > 0.01 {
		slog.Debug("container spec reset: memory limit mismatch",
			"desired", rep.App.Deploy.MemoryLimit,
			"current", info.MemoryLimit)
		return true
	}

	// Check 4: ServicePorts must match (all desired ports should be bound)
	if len(rep.App.Spec.ServicePorts) != len(info.Ports) {
		return true
	}
	for _, sp := range rep.App.Spec.ServicePorts {
		if sp == nil || sp.Port < 1 || sp.Port >= 65536 {
			continue
		}
		if _, bound := info.Ports[int32(sp.Port)]; !bound {
			slog.Debug("container spec reset: service port not bound",
				"desired_port", sp.Port,
				"bound_ports", info.Ports)
			return true
		}
	}

	return false
}

// operateContainerStarting handles the start action for a replica.
// Returns the next state on success or failure.
func operateContainerStarting(rep *hostapi.AppReplicaInstance) (string, error) {
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
					ctx, cancel := context.WithTimeout(context.Background(), defaultContainerTimeout)
					if err := ctrDriver.ContainerStart(ctx, containerName); err != nil {
						cancel()
						// Remove and recreate
						ctx2, cancel2 := context.WithTimeout(context.Background(), defaultContainerTimeout)
						if err := ctrDriver.ContainerRemove(ctx2, containerName); err != nil {
							cancel2()
							slog.Warn("container remove failed", "container", containerName, "error", err)
							return inapi.OpStateFailed, fmt.Errorf("container remove failed: %w", err)
						}
						cancel2()
						hoststatus.ContainerList.Delete(containerName)
					} else {
						cancel()
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
	ctx, cancel := context.WithTimeout(context.Background(), defaultContainerTimeout)
	if err := ctrDriver.ContainerStart(ctx, containerName); err != nil {
		cancel()
		slog.Warn("container start failed", "container", containerName, "error", err)
		return inapi.OpStateFailed, fmt.Errorf("container start failed: %w", err)
	}
	cancel()

	slog.Info("container started", "container", containerName)
	return inapi.OpStateRunning, nil
}

// operateContainerStopping handles the stop action for a replica.
// Returns the next state on success or failure.
func operateContainerStopping(rep *hostapi.AppReplicaInstance) (string, error) {
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
func operateContainerDestroying(rep *hostapi.AppReplicaInstance) (string, error) {
	containerName := rep.ContainerName()

	ctrInfo, exists := hoststatus.ContainerList.Load(containerName)
	if !exists {
		return inapi.OpStateDestroyed, nil
	}

	info, ok := ctrInfo.(*hostapi.ContainerInfo)
	if !ok {
		hoststatus.ContainerList.Delete(containerName)
		return inapi.OpStateDestroyed, nil
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
		return inapi.OpStateFailed, fmt.Errorf("container remove failed: %w", err)
	}
	cancel()

	hoststatus.ContainerList.Delete(containerName)
	slog.Info("container destroyed", "container", containerName)
	return inapi.OpStateDestroyed, nil
}

// containerCreate creates a new container for the given replica.
func containerCreate(rep *hostapi.AppReplicaInstance) error {
	containerName := rep.ContainerName()

	if rep.App.Spec.Resources == nil {
		return fmt.Errorf("app spec resources is nil")
	}

	image := rep.App.Spec.Image

	// Pull image if not exists
	if _, exists := hoststatus.ImageList.Load(image); !exists {
		slog.Info("pulling container image", "image", image)
		ctx, cancel := context.WithTimeout(context.Background(), imagePullTimeout)
		if err := ctrDriver.ImagePull(ctx, image); err != nil {
			cancel()
			slog.Warn("image pull failed", "image", image, "error", err)
			return fmt.Errorf("image pull failed: %w", err)
		}
		cancel()

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
	pkgMounts, err := EnsurePackages(rep.App)
	if err != nil {
		slog.Warn("package preparation failed",
			"app", rep.App.Id,
			"error", err)
		return fmt.Errorf("package preparation failed: %w", err)
	}

	opts := &hostapi.ContainerCreateOptions{
		Name:          containerName,
		Image:         image,
		RestartPolicy: "always",
		Labels: map[string]string{
			"app_id":     rep.App.Id,
			"app_rep_id": fmt.Sprintf("%d", rep.Replica.Id),
		},
		Env: []string{
			fmt.Sprintf("APP_ID=%s", rep.App.Id),
			fmt.Sprintf("APP_REP_ID=%d", rep.Replica.Id),
			fmt.Sprintf("APP_HOST_ID=%s", config.Config.Hostlet.HostId),
		},
	}

	if rep.App.Deploy != nil {
		opts.CpuLimit = rep.App.Deploy.CpuLimit
		opts.MemoryLimit = rep.App.Deploy.MemoryLimit
	}

	// Setup port bindings for all service ports
	for _, sp := range rep.App.Spec.ServicePorts {
		if sp != nil && sp.Port > 0 && sp.Port < 65536 {
			opts.Ports = append(opts.Ports, hostapi.PortBinding{
				ContainerPort: int32(sp.Port),
				HostPort:      int32(sp.Port),
				Protocol:      "tcp",
			})
		}
	}

	// Setup volume mounts using hostapi path utilities
	podBasePath := config.Config.Hostlet.PodPath
	if podBasePath != "" {
		podPaths := hostapi.NewPodPaths(podBasePath, containerName)

		// Mount /opt
		optPath := podPaths.OptDir()
		if err := os.MkdirAll(optPath, 0755); err == nil {
			opts.Mounts = append(opts.Mounts, hostapi.MountBind{
				HostPath: optPath, ContainerPath: "/opt", ReadOnly: false,
			})
		}

		// Mount /home
		homePath := podPaths.HomeDir()
		if err := os.MkdirAll(homePath, 0755); err == nil {
			opts.Mounts = append(opts.Mounts, hostapi.MountBind{
				HostPath: homePath, ContainerPath: "/home", ReadOnly: false,
			})
		}

		// Mount packages as read-only
		for pkgName, installDir := range pkgMounts {
			opts.Mounts = append(opts.Mounts, hostapi.MountBind{
				HostPath:      installDir,
				ContainerPath: fmt.Sprintf("/usr/instack/%s", pkgName),
				ReadOnly:      true,
			})
		}

		// Create sysinner directory and files
		sysinnerPath := podPaths.SysinnerDir()
		if err := os.MkdirAll(sysinnerPath, 0755); err == nil {
			// Write app_instance.json
			appInstancePath := podPaths.AppInstanceFile()
			if err := inutil.JsonEncodeToFileIndent(appInstancePath, rep, 0644); err == nil {
				slog.Debug("app_instance.json created", "path", appInstancePath)
			} else {
				slog.Warn("app_instance.json create faild",
					"path", appInstancePath, "err", err.Error())
			}

			// Copy ininit script
			srcIninitPath := hostSrcPaths.IninitSrc()
			ininitPath := podPaths.IninitFile()
			if ininitData, err := os.ReadFile(srcIninitPath); err == nil {
				if err := os.WriteFile(ininitPath, ininitData, 0755); err == nil {
					slog.Debug("ininit script copied", "path", ininitPath)
				} else {
					slog.Warn("failed to write ininit script", "error", err)
				}
			} else {
				slog.Warn("failed to read ininit script", "path", srcIninitPath, "error", err)
			}

			// Copy inagent binary based on architecture (amd64/arm64)
			arch := "amd64"
			if info, ok := hoststatus.StatusSet.Load("docker"); ok {
				if driverInfo, ok := info.(*hostapi.DriverInfo); ok && driverInfo.Arch != "" {
					arch = driverInfo.Arch
				}
			}
			srcInagentPath := hostSrcPaths.InagentSrc(arch)
			inagentPath := podPaths.InagentFile()
			if inagentData, err := os.ReadFile(srcInagentPath); err == nil {
				if err := os.WriteFile(inagentPath, inagentData, 0755); err == nil {
					slog.Debug("inagent binary copied", "path", inagentPath, "arch", arch)
				} else {
					slog.Warn("failed to write inagent binary", "error", err)
				}
			} else {
				slog.Warn("failed to read inagent binary", "path", srcInagentPath, "error", err)
			}

			// Set container command to run ininit
			opts.Cmd = hostapi.ContainerCmd()
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), defaultContainerTimeout)
	_, err = ctrDriver.ContainerCreate(ctx, opts)
	cancel()
	if err != nil {
		slog.Warn("container create failed", "container", containerName, "error", err)
		return fmt.Errorf("container create failed: %w", err)
	}

	hoststatus.ContainerList.Store(containerName, &hostapi.ContainerInfo{
		Name: containerName, Image: image, State: inapi.OpStateStarting,
	})
	slog.Info("container created", "container", containerName)
	return nil
}

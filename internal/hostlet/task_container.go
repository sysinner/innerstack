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
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/sysinner/incore/v2/inapi"
	"github.com/sysinner/incore/v2/internal/config"
	"github.com/sysinner/incore/v2/internal/hostlet/docker"
	"github.com/sysinner/incore/v2/internal/hostlet/hostapi"
	"github.com/sysinner/incore/v2/internal/hostlet/hoststatus"
)

const (
	defaultContainerTimeout = 30 * time.Second
	imagePullTimeout        = 5 * time.Minute
)

var (
	ctrDriver  hostapi.Driver
	ctrDrivers []hostapi.Driver
)

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
		hoststatus.StatusSet.Delete("container")
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
	hoststatus.ContainerList.Clear()
	for _, ctr := range containers {
		if ctr.Name != "" {
			hoststatus.ContainerList.Store(ctr.Name, ctr)
		}
	}
	hoststatus.ContainerReady.Store(true)

	return nil
}

// containerControlRefresh ensures app containers are running.
func containerControlRefresh() error {
	if !hoststatus.HostReady.Load() || !hoststatus.ContainerReady.Load() || ctrDriver == nil {
		return nil
	}

	hoststatus.ActiveAppList.Range(func(key, value any) bool {
		app, ok := value.(*inapi.AppInstance)
		if !ok || app.Spec == nil || app.Operate == nil || len(app.Operate.Replicas) == 0 {
			return true
		}

		for _, rep := range app.Operate.Replicas {
			if rep.HostId == "" || rep.HostId != config.Config.Hostlet.HostId {
				continue
			}

			containerName := fmt.Sprintf("app-%s-%04d", app.Id, rep.Id)
			ctrInfo, exists := hoststatus.ContainerList.Load(containerName)

			if !exists {
				containerEnsureRunning(app, rep, containerName)
			} else if info, ok := ctrInfo.(*hostapi.ContainerInfo); ok && info.State != "running" {
				containerEnsureRunning(app, rep, containerName)
			}
		}
		return true
	})

	return nil
}

// containerEnsureRunning creates or starts a container for the given app replica.
func containerEnsureRunning(app *inapi.AppInstance, replica *inapi.AppOperateReplica, containerName string) {
	image := app.Spec.RuntimeImage
	if image == "" {
		slog.Warn("container image not specified", "app", app.Id)
		return
	}

	// Pull image if not exists
	if _, exists := hoststatus.ImageList.Load(image); !exists {
		slog.Info("pulling container image", "image", image)
		ctx, cancel := context.WithTimeout(context.Background(), imagePullTimeout)
		if err := ctrDriver.ImagePull(ctx, image); err != nil {
			cancel()
			slog.Warn("image pull failed", "image", image, "error", err)
			return
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

	// Check existing container state
	if ctrInfo, exists := hoststatus.ContainerList.Load(containerName); exists {
		if info, ok := ctrInfo.(*hostapi.ContainerInfo); ok {
			switch info.State {
			case "running":
				return
			case "exited", "dead":
				ctx, cancel := context.WithTimeout(context.Background(), defaultContainerTimeout)
				if err := ctrDriver.ContainerStart(ctx, containerName); err != nil {
					cancel()
					// Remove and recreate
					ctx2, cancel2 := context.WithTimeout(context.Background(), defaultContainerTimeout)
					if err := ctrDriver.ContainerRemove(ctx2, containerName); err != nil {
						cancel2()
						slog.Warn("container remove failed", "container", containerName, "error", err)
						return
					}
					cancel2()
				} else {
					cancel()
					slog.Info("container started", "container", containerName)
					return
				}
			case "paused":
				ctx, cancel := context.WithTimeout(context.Background(), defaultContainerTimeout)
				ctrDriver.ContainerStart(ctx, containerName)
				cancel()
				return
			}
		}
	}

	// Create container
	opts := &hostapi.ContainerCreateOptions{
		Name:          containerName,
		Image:         image,
		RestartPolicy: "always",
		Labels: map[string]string{
			"app_id":     app.Id,
			"app_name":   app.Name,
			"app_rep_id": fmt.Sprintf("%d", replica.Id),
			"host_id":    config.Config.Hostlet.HostId,
		},
		Env: []string{
			fmt.Sprintf("APP_ID=%s", app.Id),
			fmt.Sprintf("APP_NAME=%s", app.Name),
			fmt.Sprintf("APP_REP_ID=%d", replica.Id),
			fmt.Sprintf("HOST_ID=%s", config.Config.Hostlet.HostId),
		},
	}

	if app.Operate != nil {
		opts.CpuLimit = app.Operate.CpuLimit
		opts.MemoryLimit = app.Operate.MemoryLimit
	}

	if app.Spec.ServicePorts != nil && app.Spec.ServicePorts.Port > 0 {
		opts.Ports = []hostapi.PortBinding{{
			ContainerPort: int32(app.Spec.ServicePorts.Port),
			HostPort:      int32(app.Spec.ServicePorts.Port),
			Protocol:      "tcp",
		}}
	}

	// Setup volume mounts
	podBasePath := config.Config.Hostlet.PodPath
	if podBasePath != "" {
		containerPodPath := fmt.Sprintf("%s/%s", podBasePath, containerName)

		// Mount /opt
		optPath := containerPodPath + "/opt"
		if err := os.MkdirAll(optPath, 0755); err == nil {
			opts.Mounts = append(opts.Mounts, hostapi.MountBind{
				HostPath: optPath, ContainerPath: "/opt", ReadOnly: false,
			})
		}

		// Mount /home
		homePath := containerPodPath + "/home"
		if err := os.MkdirAll(homePath, 0755); err == nil {
			opts.Mounts = append(opts.Mounts, hostapi.MountBind{
				HostPath: homePath, ContainerPath: "/home", ReadOnly: false,
			})
		}

		// Create sysinner directory and files
		sysinnerPath := homePath + "/action/.sysinner"
		if err := os.MkdirAll(sysinnerPath, 0755); err == nil {
			// Write app_instance.json
			appInstancePath := sysinnerPath + "/app_instance.json"
			if data, err := json.MarshalIndent(app, "", "  "); err == nil {
				if err := os.WriteFile(appInstancePath, data, 0644); err == nil {
					slog.Info("app_instance.json created", "path", appInstancePath)
				}
			}

			// Copy ininit script from cmd/inagent/ininit
			srcIninitPath := config.Prefix + "/cmd/inagent/ininit"
			ininitPath := sysinnerPath + "/ininit"
			if ininitData, err := os.ReadFile(srcIninitPath); err == nil {
				if err := os.WriteFile(ininitPath, ininitData, 0755); err == nil {
					slog.Info("ininit script copied", "path", ininitPath)
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
			srcInagentPath := config.Prefix + "/bin/inagent-linux-" + arch
			inagentPath := sysinnerPath + "/inagent"
			if inagentData, err := os.ReadFile(srcInagentPath); err == nil {
				if err := os.WriteFile(inagentPath, inagentData, 0755); err == nil {
					slog.Info("inagent binary copied", "path", inagentPath, "arch", arch)
				} else {
					slog.Warn("failed to write inagent binary", "error", err)
				}
			} else {
				slog.Warn("failed to read inagent binary", "path", srcInagentPath, "error", err)
			}

			// Set container command to run ininit
			opts.Cmd = []string{"/bin/sh", "/home/action/.sysinner/ininit"}
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), defaultContainerTimeout)
	_, err := ctrDriver.ContainerCreate(ctx, opts)
	cancel()
	if err != nil {
		slog.Warn("container create failed", "container", containerName, "error", err)
		return
	}

	hoststatus.ContainerList.Store(containerName, &hostapi.ContainerInfo{
		Name: containerName, Image: image, State: "created",
	})
	slog.Info("container created", "container", containerName)

	// Start container
	ctx, cancel = context.WithTimeout(context.Background(), defaultContainerTimeout)
	if err := ctrDriver.ContainerStart(ctx, containerName); err != nil {
		cancel()
		slog.Warn("container start failed", "container", containerName, "error", err)
		return
	}
	cancel()
	slog.Info("container started", "container", containerName)
}

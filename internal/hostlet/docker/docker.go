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

// Package docker provides a Docker container driver implementation.
package docker

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"strings"
	"sync"

	drvClient "github.com/fsouza/go-dockerclient"

	"github.com/sysinner/incore/v2/inapi"
	"github.com/sysinner/incore/v2/internal/hostlet/hostapi"
)

// dockerStateMap maps Docker container states to inapi replica states.
var dockerStateMap = map[string]string{
	"running": inapi.OpStateRunning,
	"exited":  inapi.OpStateStopped,
	"dead":    inapi.OpStateStopped,
	"paused":  inapi.OpStateRunning,
	"created": inapi.OpStateStarting,
}

var (
	driver             hostapi.Driver = &dockerDriver{}
	clientUnixSockAddr                = "unix:///var/run/docker.sock"
)

func init() {
	if runtime.GOOS == "darwin" {
		if up, err := os.UserHomeDir(); err == nil {
			clientUnixSockAddr = "unix://" + up + "/.docker/run/docker.sock"
		}
	}
}

// NewDriver creates a new Docker driver instance.
func NewDriver() (hostapi.Driver, error) {
	return driver, nil
}

type dockerDriver struct {
	mu     sync.RWMutex
	client *drvClient.Client
}

func (it *dockerDriver) Name() string {
	return "docker"
}

func (it *dockerDriver) init() error {
	it.mu.Lock()
	defer it.mu.Unlock()

	if it.client != nil {
		return nil
	}

	client, err := drvClient.NewClient(clientUnixSockAddr)
	if err != nil {
		return err
	}
	if err := client.Ping(); err != nil {
		return err
	}
	it.client = client
	return nil
}

func (it *dockerDriver) Info(ctx context.Context) (*hostapi.DriverInfo, error) {
	if err := it.init(); err != nil {
		return nil, err
	}

	info := &hostapi.DriverInfo{Name: it.Name()}

	if version, err := it.client.Version(); err == nil {
		info.Version = version.Get("Version")
		info.APIVersion = version.Get("ApiVersion")
		info.OS = version.Get("Os")
		info.Arch = version.Get("Arch")
		info.Kernel = version.Get("KernelVersion")
	}

	if env, err := it.client.Info(); err == nil {
		info.ContainerNum = env.Containers
		info.ImageNum = env.Images
	}

	return info, nil
}

func (it *dockerDriver) Ping(ctx context.Context) error {
	if err := it.init(); err != nil {
		return err
	}
	return it.client.Ping()
}

func (it *dockerDriver) ImageList(ctx context.Context) ([]*hostapi.ImageInfo, error) {
	if err := it.init(); err != nil {
		return nil, err
	}

	images, err := it.client.ListImages(drvClient.ListImagesOptions{
		All:     false,
		Context: ctx,
	})
	if err != nil {
		return nil, err
	}

	result := make([]*hostapi.ImageInfo, 0, len(images))
	for _, img := range images {
		if len(img.RepoTags) == 0 {
			continue
		}
		result = append(result, &hostapi.ImageInfo{
			Name:        strings.Trim(strings.Join(img.RepoTags, "/"), "/"),
			ID:          img.ID,
			RepoTags:    img.RepoTags,
			RepoDigests: img.RepoDigests,
			Size:        img.Size,
			Created:     img.Created,
			Labels:      img.Labels,
		})
	}
	return result, nil
}

func (it *dockerDriver) ContainerList(ctx context.Context) ([]*hostapi.ContainerInfo, error) {
	if err := it.init(); err != nil {
		return nil, err
	}

	containers, err := it.client.ListContainers(drvClient.ListContainersOptions{
		All:     true,
		Context: ctx,
	})
	if err != nil {
		return nil, err
	}

	result := make([]*hostapi.ContainerInfo, 0, len(containers))
	for _, ctr := range containers {
		// Map Docker state to inapi state
		state := ctr.State
		if s, ok := dockerStateMap[ctr.State]; ok {
			state = s
		}

		info := &hostapi.ContainerInfo{
			ID:     ctr.ID,
			Name:   strings.Trim(strings.Join(ctr.Names, "/"), "/"),
			Image:  ctr.Image,
			State:  state,
			Status: ctr.Status,
			Labels: ctr.Labels,
		}

		if ctr.Created > 0 {
			info.Created = ctr.Created
		}

		for _, port := range ctr.Ports {
			pb := hostapi.PortBinding{
				ContainerPort: int32(port.PrivatePort),
				HostPort:      int32(port.PublicPort),
				Protocol:      port.Type,
			}
			if port.IP != "" {
				pb.HostIP = port.IP
			}
			if info.Ports == nil {
				info.Ports = make(map[int32]hostapi.PortBinding)
			}
			info.Ports[pb.ContainerPort] = pb
		}

		for _, net := range ctr.Networks.Networks {
			if net.IPAddress != "" {
				info.IP = net.IPAddress
				break
			}
		}

		result = append(result, info)
	}

	return result, nil
}

// ContainerInspect returns detailed container info including resource limits.
func (it *dockerDriver) ContainerInspect(
	ctx context.Context, nameOrId string,
) (*hostapi.ContainerInfo, error) {
	if err := it.init(); err != nil {
		return nil, err
	}

	container, err := it.client.InspectContainerWithOptions(drvClient.InspectContainerOptions{
		ID:      nameOrId,
		Context: ctx,
	})
	if err != nil {
		return nil, err
	}

	// Map Docker state to inapi state
	state := container.State.Status
	if s, ok := dockerStateMap[container.State.Status]; ok {
		state = s
	}

	info := &hostapi.ContainerInfo{
		ID:      container.ID,
		Name:    strings.TrimPrefix(container.Name, "/"),
		Image:   container.Config.Image,
		ImageID: container.Image,
		State:   state,
		Status:  container.State.String(),
		Labels:  container.Config.Labels,
	}

	// CPU limit: NanoCPUs is in units of 1e-9 CPUs.
	// Convert to millicores (1000 = 1 core).
	if container.HostConfig.NanoCPUs > 0 {
		info.CpuLimit = container.HostConfig.NanoCPUs / 1e6
	}

	// Memory limit (bytes)
	if container.HostConfig.Memory > 0 {
		info.MemoryLimit = container.HostConfig.Memory
	}

	if container.Created.Unix() > 0 {
		info.Created = container.Created.Unix()
	}

	if container.State.Pid != 0 {
		info.Pid = container.State.Pid
	}

	if !container.State.StartedAt.IsZero() {
		info.Started = container.State.StartedAt.Unix()
	}

	// Extract port bindings
	for port, bindings := range container.HostConfig.PortBindings {
		for _, binding := range bindings {
			proto := "tcp"
			parts := strings.Split(string(port), "/")
			if len(parts) == 2 {
				proto = parts[1]
			}
			var containerPort int32
			fmt.Sscanf(parts[0], "%d", &containerPort)
			var hostPort int32
			fmt.Sscanf(binding.HostPort, "%d", &hostPort)
			if info.Ports == nil {
				info.Ports = make(map[int32]hostapi.PortBinding)
			}
			info.Ports[containerPort] = hostapi.PortBinding{
				HostIP:        binding.HostIP,
				HostPort:      hostPort,
				ContainerPort: containerPort,
				Protocol:      proto,
			}
		}
	}

	// Extract IP from network settings
	if container.NetworkSettings != nil {
		for _, net := range container.NetworkSettings.Networks {
			if net.IPAddress != "" {
				info.IP = net.IPAddress
				break
			}
		}
	}

	return info, nil
}

func (it *dockerDriver) ContainerCreate(
	ctx context.Context, opts *hostapi.ContainerCreateOptions,
) (*hostapi.ContainerInfo, error) {
	if err := it.init(); err != nil {
		return nil, err
	}

	config := &drvClient.Config{
		Image:  opts.Image,
		Cmd:    opts.Cmd,
		Env:    opts.Env,
		Labels: opts.Labels,
	}

	hostConfig := &drvClient.HostConfig{}

	// CPU limit: NanoCPUs (1e9 = 1 CPU)
	if opts.CpuLimit > 0 {
		hostConfig.NanoCPUs = opts.CpuLimit * 1e6
	}

	// Memory limit (bytes)
	if opts.MemoryLimit > 0 {
		hostConfig.Memory = opts.MemoryLimit
	}

	// Port bindings
	if len(opts.Ports) > 0 {
		config.ExposedPorts = make(map[drvClient.Port]struct{})
		hostConfig.PortBindings = make(map[drvClient.Port][]drvClient.PortBinding)

		for _, port := range opts.Ports {
			containerPort := drvClient.Port(fmt.Sprintf("%d/%s", port.ContainerPort, port.Protocol))
			config.ExposedPorts[containerPort] = struct{}{}

			binding := drvClient.PortBinding{HostPort: fmt.Sprintf("%d", port.HostPort)}
			if port.HostIP != "" {
				binding.HostIP = port.HostIP
			}
			hostConfig.PortBindings[containerPort] = []drvClient.PortBinding{binding}
		}
	}

	// Restart policy
	if opts.RestartPolicy != "" {
		hostConfig.RestartPolicy = drvClient.RestartPolicy{Name: opts.RestartPolicy}
	}

	// Volume mounts
	if len(opts.Mounts) > 0 {
		hostConfig.Binds = make([]string, 0, len(opts.Mounts))
		for _, mount := range opts.Mounts {
			bind := mount.HostPath + ":" + mount.ContainerPath
			if mount.ReadOnly {
				bind += ":ro"
			}
			hostConfig.Binds = append(hostConfig.Binds, bind)
		}
	}

	container, err := it.client.CreateContainer(drvClient.CreateContainerOptions{
		Name:       opts.Name,
		Config:     config,
		HostConfig: hostConfig,
		Context:    ctx,
	})
	if err != nil {
		return nil, err
	}

	return &hostapi.ContainerInfo{
		ID:      container.ID,
		Name:    opts.Name,
		Image:   opts.Image,
		State:   inapi.OpStateStarting,
		Created: container.Created.Unix(),
		Labels:  opts.Labels,
	}, nil
}

func (it *dockerDriver) ContainerStart(ctx context.Context, nameOrId string) error {
	if err := it.init(); err != nil {
		return err
	}
	return it.client.StartContainerWithContext(nameOrId, nil, ctx)
}

func (it *dockerDriver) ContainerStop(ctx context.Context, nameOrId string) error {
	if err := it.init(); err != nil {
		return err
	}
	return it.client.StopContainerWithContext(nameOrId, 10, ctx)
}

func (it *dockerDriver) ContainerRemove(ctx context.Context, nameOrId string) error {
	if err := it.init(); err != nil {
		return err
	}
	return it.client.RemoveContainer(drvClient.RemoveContainerOptions{
		ID:      nameOrId,
		Force:   true,
		Context: ctx,
	})
}

func (it *dockerDriver) ImagePull(ctx context.Context, image string) error {
	if err := it.init(); err != nil {
		return err
	}
	return it.client.PullImage(drvClient.PullImageOptions{
		Repository: image,
		Context:    ctx,
	}, drvClient.AuthConfiguration{})
}

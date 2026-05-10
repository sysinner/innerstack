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

// Package docker provides a Docker container driver implementation.
package docker

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"

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

// cfgStorageOptSizeEnable controls whether StorageOpt size is set on create.
// Disabled automatically when the backing filesystem does not support it.
var cfgStorageOptSizeEnable = true

func init() {
	if runtime.GOOS == "darwin" {
		cfgStorageOptSizeEnable = false
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
	mu           sync.RWMutex
	client       *drvClient.Client
	vpcSubnet    string
	vpcNetworkID string
}

func (it *dockerDriver) Name() string {
	return "docker"
}

// resetClient clears the cached Docker client so the next operation
// will re-establish the connection.
func (it *dockerDriver) resetClient() {
	it.mu.Lock()
	it.client = nil
	it.mu.Unlock()
}

// initClientWithRetry tries to create and validate a Docker client with retries.
// It uses exponential-like backoff (1s, 2s, 3s) across 3 attempts.
func (it *dockerDriver) initClientWithRetry() error {
	it.mu.Lock()
	defer it.mu.Unlock()

	if it.client != nil {
		return nil
	}

	const maxRetries = 3
	for i := 0; i < maxRetries; i++ {
		client, err := drvClient.NewClient(clientUnixSockAddr)
		if err != nil {
			slog.Warn("docker client connect error",
				"attempt", i+1, "error", err)
			if i < maxRetries-1 {
				time.Sleep(time.Duration(i+1) * time.Second)
			}
			continue
		}
		if err := client.Ping(); err != nil {
			slog.Warn("docker client ping error",
				"attempt", i+1, "error", err)
			if i < maxRetries-1 {
				time.Sleep(time.Duration(i+1) * time.Second)
			}
			continue
		}
		it.client = client
		return nil
	}

	return errors.New("docker: failed to connect after retries")
}

func (it *dockerDriver) init() error {
	return it.initClientWithRetry()
}

const vpcNetworkName = "invpc2_docker"

// ensureVpcNetwork ensures the VPC Docker network exists with the correct subnet.
func (it *dockerDriver) ensureVpcNetwork(ctx context.Context, subnet string) error {
	if err := it.init(); err != nil {
		return err
	}

	it.mu.RLock()
	if it.vpcSubnet == subnet && it.vpcNetworkID != "" {
		it.mu.RUnlock()
		return nil
	}
	it.mu.RUnlock()

	it.mu.Lock()
	defer it.mu.Unlock()

	// Double-check after acquiring write lock
	if it.vpcSubnet == subnet && it.vpcNetworkID != "" {
		return nil
	}

	networks, err := it.client.ListNetworks()
	if err != nil {
		return fmt.Errorf("[docker.ensureVpcNetwork] list networks failed: %w", err)
	}

	for _, net := range networks {
		if net.Name == vpcNetworkName {
			if len(net.IPAM.Config) > 0 && net.IPAM.Config[0].Subnet == subnet {
				it.vpcSubnet = subnet
				it.vpcNetworkID = net.ID
				return nil
			}
			// Network exists with wrong subnet, remove it
			if err := it.client.RemoveNetwork(net.ID); err != nil {
				return fmt.Errorf("[docker.ensureVpcNetwork] remove old network failed: %w", err)
			}
			break
		}
	}

	// Create the network
	netOpts := drvClient.CreateNetworkOptions{
		Name:   vpcNetworkName,
		Driver: "bridge",
		IPAM: &drvClient.IPAMOptions{
			Config: []drvClient.IPAMConfig{
				{Subnet: subnet},
			},
		},
		Context: ctx,
	}

	netInfo, err := it.client.CreateNetwork(netOpts)
	if err != nil {
		return fmt.Errorf("[docker.ensureVpcNetwork] create network failed: %w", err)
	}

	it.vpcSubnet = subnet
	it.vpcNetworkID = netInfo.ID
	return nil
}

func (it *dockerDriver) Info(ctx context.Context) (*hostapi.DriverInfo, error) {
	defer recoverPanic()

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

	if env, err := it.client.Info(); err != nil {
		// Connection lost, reset client for next retry cycle
		slog.Warn("docker Info failed, resetting client", "error", err)
		it.resetClient()
		return nil, fmt.Errorf("[docker.Info] docker info failed: %w", err)
	} else {
		info.ContainerNum = env.Containers
		info.ImageNum = env.Images
	}

	return info, nil
}

func (it *dockerDriver) Ping(ctx context.Context) error {
	defer recoverPanic()

	if err := it.init(); err != nil {
		return err
	}
	if err := it.client.Ping(); err != nil {
		// Connection lost, reset client for next retry cycle
		it.resetClient()
		return err
	}
	return nil
}

func (it *dockerDriver) ImageList(ctx context.Context) ([]*hostapi.ImageInfo, error) {
	defer recoverPanic()

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
	defer recoverPanic()

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

		// Prefer IP from VPC network, fall back to any network
		for netName, net := range ctr.Networks.Networks {
			if net.IPAddress != "" {
				if netName == vpcNetworkName {
					info.IP = net.IPAddress
					break
				}
				if info.IP == "" {
					info.IP = net.IPAddress
				}
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
	defer recoverPanic()

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

	// Extract volume binds from HostConfig
	if len(container.HostConfig.Binds) > 0 {
		info.Binds = container.HostConfig.Binds
	}

	// Extract IP from network settings, prefer VPC network
	if container.NetworkSettings != nil {
		for netName, net := range container.NetworkSettings.Networks {
			if net.IPAddress != "" {
				if netName == vpcNetworkName {
					info.IP = net.IPAddress
					break
				}
				if info.IP == "" {
					info.IP = net.IPAddress
				}
			}
		}
	}

	return info, nil
}

// isDockerError checks if a Docker API error message contains any of the
// given substrings, enabling graceful handling of common container states.
func isDockerError(err error, substrings ...string) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	for _, sub := range substrings {
		if strings.Contains(msg, sub) {
			return true
		}
	}
	return false
}

func (it *dockerDriver) ContainerCreate(
	ctx context.Context, opts *hostapi.ContainerCreateOptions,
) (*hostapi.ContainerInfo, error) {
	defer recoverPanic()

	if err := it.init(); err != nil {
		return nil, err
	}

	config := &drvClient.Config{
		Image:  opts.Image,
		Cmd:    opts.Cmd,
		Env:    opts.Env,
		Labels: opts.Labels,
	}

	// Set hostname if provided
	if opts.Hostname != "" {
		config.Hostname = opts.Hostname
	}

	hostConfig := &drvClient.HostConfig{}

	// CPU limit: NanoCPUs (1e9 = 1 CPU)
	if opts.CpuLimit > 0 {
		hostConfig.NanoCPUs = opts.CpuLimit * 1e6
	}

	// Memory limit (bytes), disable swap by setting MemorySwap == Memory
	if opts.MemoryLimit > 0 {
		hostConfig.Memory = opts.MemoryLimit
		hostConfig.MemorySwap = opts.MemoryLimit
		hostConfig.ShmSize = opts.MemoryLimit / 2
	}

	// PidsLimit restricts the number of processes inside the container
	pidsLimit := int64(100)
	hostConfig.PidsLimit = &pidsLimit

	// Ulimits: raise nofile for container processes
	hostConfig.Ulimits = []drvClient.ULimit{
		{Name: "nofile", Soft: 10000, Hard: 10000},
	}

	// StorageOpt: set rootfs size when backing filesystem supports it
	if cfgStorageOptSizeEnable {
		hostConfig.StorageOpt = map[string]string{
			"size": "10G",
		}
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

	// DNS servers
	if len(opts.DnsServers) > 0 {
		hostConfig.DNS = opts.DnsServers
		config.DNS = opts.DnsServers
	}

	// Extra hosts (added to /etc/hosts)
	// TODO: add extra hosts support when needed

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

	// VPC endpoint config
	var networkingConfig *drvClient.NetworkingConfig
	if opts.VpcIPv4 != "" && opts.VpcSubnet != "" {
		if err := it.ensureVpcNetwork(ctx, opts.VpcSubnet); err != nil {
			return nil, fmt.Errorf("vpc network setup failed: %w", err)
		}
		networkingConfig = &drvClient.NetworkingConfig{
			EndpointsConfig: map[string]*drvClient.EndpointConfig{
				vpcNetworkName: {
					IPAMConfig: &drvClient.EndpointIPAMConfig{
						IPv4Address: opts.VpcIPv4,
					},
				},
			},
		}
	}

	// Non-Linux: clear CPU set, force root user
	if runtime.GOOS != "linux" {
		hostConfig.CPUSetCPUs = ""
		config.User = "root"
	}

	container, err := it.client.CreateContainer(drvClient.CreateContainerOptions{
		Name:             opts.Name,
		Config:           config,
		HostConfig:       hostConfig,
		NetworkingConfig: networkingConfig,
		Context:          ctx,
	})
	if err != nil {
		// Graceful handling: "container already exists" is not a fatal error
		if isDockerError(err, "container already exists") {
			slog.Warn("container already exists, fetching existing",
				"container", opts.Name)
			if existing, inspectErr := it.client.InspectContainerWithOptions(
				drvClient.InspectContainerOptions{ID: opts.Name}); inspectErr == nil {
				return &hostapi.ContainerInfo{
					ID:      existing.ID,
					Name:    opts.Name,
					Image:   opts.Image,
					State:   inapi.OpStateStarting,
					Created: existing.Created.Unix(),
					Labels:  opts.Labels,
				}, nil
			}
		}

		// Auto-disable StorageOpt when backing filesystem does not support it
		if isDockerError(err, "storage-opt") {
			cfgStorageOptSizeEnable = false
			slog.Warn("disabled StorageOpt (backing filesystem unsupported)",
				"error", err)
		}

		return nil, fmt.Errorf("[docker.ContainerCreate] create container %s failed: %w", opts.Name, err)
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
	defer recoverPanic()

	if err := it.init(); err != nil {
		return err
	}

	err := it.client.StartContainerWithContext(nameOrId, nil, ctx)
	if err != nil {
		// OCI runtime failures often indicate a corrupted container config.
		// Remove the container so the next refresh cycle recreates it.
		if isDockerError(err,
			"OCI runtime create failed",
			"storage-opt",
		) {
			slog.Warn("container start OCI/storage error, removing for re-creation",
				"container", nameOrId, "error", err)
			if rmErr := it.client.RemoveContainer(drvClient.RemoveContainerOptions{
				ID: nameOrId, Force: true, Context: ctx,
			}); rmErr != nil {
				slog.Warn("container remove after start failure also failed",
					"container", nameOrId, "remove_error", rmErr)
			}
			time.Sleep(1 * time.Second)
		}

		// "No such container" means the container is gone externally
		if isDockerError(err,
			"No such container",
			"no such file or directory",
		) {
			slog.Warn("container disappeared before start",
				"container", nameOrId)
		}

		return fmt.Errorf("[docker.ContainerStart] start %s failed: %w", nameOrId, err)
	}

	return nil
}

func (it *dockerDriver) ContainerStop(ctx context.Context, nameOrId string) error {
	defer recoverPanic()

	if err := it.init(); err != nil {
		return err
	}

	err := it.client.StopContainerWithContext(nameOrId, 10, ctx)
	if err != nil {
		// Graceful handling: container already gone or not running
		if isDockerError(err,
			"No such container",
			"Container not running",
		) {
			slog.Debug("container stop skipped (already gone or not running)",
				"container", nameOrId, "error", err)
			return nil
		}
		return fmt.Errorf("[docker.ContainerStop] stop %s failed: %w", nameOrId, err)
	}

	return nil
}

func (it *dockerDriver) ContainerRemove(ctx context.Context, nameOrId string) error {
	defer recoverPanic()

	if err := it.init(); err != nil {
		return err
	}

	err := it.client.RemoveContainer(drvClient.RemoveContainerOptions{
		ID:      nameOrId,
		Force:   true,
		Context: ctx,
	})
	if err != nil {
		// Graceful handling: container already gone
		if isDockerError(err, "No such container") {
			slog.Debug("container remove skipped (already gone)",
				"container", nameOrId)
			return nil
		}
		return fmt.Errorf("[docker.ContainerRemove] remove %s failed: %w", nameOrId, err)
	}

	return nil
}

func (it *dockerDriver) ImagePull(ctx context.Context, image string) error {
	defer recoverPanic()

	if err := it.init(); err != nil {
		return err
	}
	if err := it.client.PullImage(drvClient.PullImageOptions{
		Repository: image,
		Context:    ctx,
	}, drvClient.AuthConfiguration{}); err != nil {
		return fmt.Errorf("[docker.ImagePull] pull %s failed: %w", image, err)
	}
	return nil
}

// ContainerStats fetches resource usage statistics for a running container.
// Returns a snapshot of CPU, memory, network and block I/O metrics.
func (it *dockerDriver) ContainerStats(
	ctx context.Context, nameOrId string,
) (*hostapi.ContainerStats, error) {
	defer recoverPanic()

	if err := it.init(); err != nil {
		return nil, err
	}

	statsTimeout := 3 * time.Second
	statsBuf := make(chan *drvClient.Stats, 2)

	if err := it.client.Stats(drvClient.StatsOptions{
		ID:                nameOrId,
		Stats:             statsBuf,
		Stream:            false,
		Timeout:           statsTimeout,
		InactivityTimeout: statsTimeout,
		Context:           ctx,
	}); err != nil {
		return nil, fmt.Errorf("[docker.ContainerStats] stats %s failed: %w", nameOrId, err)
	}

	stats, ok := <-statsBuf
	if !ok || stats == nil {
		return nil, errors.New("docker stats: timeout, no data received")
	}

	result := &hostapi.ContainerStats{
		Time: time.Now().Unix(),
	}

	// Memory
	result.MemoryUsage = int64(stats.MemoryStats.Usage)
	result.MemoryCache = int64(stats.MemoryStats.Stats.Cache)

	// CPU
	result.CpuTotalUsage = int64(stats.CPUStats.CPUUsage.TotalUsage)

	// Network I/O
	for _, v := range stats.Networks {
		result.NetRxBytes += int64(v.RxBytes)
		result.NetTxBytes += int64(v.TxBytes)
	}

	// Block I/O
	for _, v := range stats.BlkioStats.IOServiceBytesRecursive {
		switch v.Op {
		case "Read":
			result.BlkReadBytes += int64(v.Value)
		case "Write":
			result.BlkWriteBytes += int64(v.Value)
		}
	}
	for _, v := range stats.BlkioStats.IOServicedRecursive {
		switch v.Op {
		case "Read":
			result.BlkReadOps += int64(v.Value)
		case "Write":
			result.BlkWriteOps += int64(v.Value)
		}
	}

	return result, nil
}

// recoverPanic is a defensive measure against unexpected panics in the Docker
// client library. It logs the panic and prevents the hostlet from crashing.
func recoverPanic() {
	if r := recover(); r != nil {
		slog.Error("docker driver panic recovered",
			"error", r, "stack", string(debugStack()))
	}
}

// debugStack returns a goroutine stack trace (lightweight wrapper).
func debugStack() []byte {
	buf := make([]byte, 4096)
	n := runtime.Stack(buf, false)
	return buf[:n]
}

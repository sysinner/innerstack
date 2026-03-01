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

package hostapi

import (
	"context"
)

// DriverInfo represents container driver metadata.
type DriverInfo struct {
	Name         string `json:"name"`
	Version      string `json:"version"`
	APIVersion   string `json:"api_version,omitempty"`
	OS           string `json:"os,omitempty"`
	Arch         string `json:"arch,omitempty"`
	Kernel       string `json:"kernel,omitempty"`
	ContainerNum int    `json:"container_num,omitempty"`
	ImageNum     int    `json:"image_num,omitempty"`
}

// ContainerInfo represents container instance details.
type ContainerInfo struct {
	ID      string            `json:"id"`
	Name    string            `json:"name"`
	Image   string            `json:"image"`
	ImageID string            `json:"image_id"`
	State   string            `json:"state"`             // running, exited, paused, etc.
	Status  string            `json:"status"`            // detailed status
	Pid     int               `json:"pid"`               // process ID
	IP      string            `json:"ip"`                // container IP
	Ports   []PortBinding     `json:"ports"`             // port mappings
	Created int64             `json:"created"`           // unix timestamp (seconds)
	Started int64             `json:"started,omitempty"` // unix timestamp (seconds)
	Labels  map[string]string `json:"labels,omitempty"`
}

// PortBinding represents a port mapping configuration.
type PortBinding struct {
	HostIP        string `json:"host_ip"`
	HostPort      int32  `json:"host_port"`
	ContainerPort int32  `json:"container_port"`
	Protocol      string `json:"protocol"` // tcp, udp
}

// MountBind represents a directory mount configuration.
type MountBind struct {
	HostPath      string `json:"host_path"`      // host path
	ContainerPath string `json:"container_path"` // container path
	ReadOnly      bool   `json:"read_only"`      // read-only flag
}

// ImageInfo represents container image metadata.
type ImageInfo struct {
	Name        string            `json:"name"`
	ID          string            `json:"id"`
	RepoTags    []string          `json:"repo_tags"`
	RepoDigests []string          `json:"repo_digests"`
	Size        int64             `json:"size"`    // bytes
	Created     int64             `json:"created"` // unix timestamp (seconds)
	Labels      map[string]string `json:"labels,omitempty"`
}

// ContainerCreateOptions defines container creation parameters.
type ContainerCreateOptions struct {
	Name          string            // container name
	Image         string            // image name
	Cmd           []string          // entrypoint command
	Env           []string          // environment variables
	Labels        map[string]string // metadata labels
	CpuLimit      int64             // CPU limit (millicores, 1000 = 1 core)
	MemoryLimit   int64             // memory limit (bytes)
	Ports         []PortBinding     // port mappings
	Mounts        []MountBind       // volume mounts
	RestartPolicy string            // no, always, on-failure, unless-stopped
}

// Driver defines the container runtime driver interface.
type Driver interface {
	Name() string
	Info(ctx context.Context) (*DriverInfo, error)
	Ping(ctx context.Context) error
	ImageList(ctx context.Context) ([]*ImageInfo, error)
	ContainerList(ctx context.Context) ([]*ContainerInfo, error)
	ContainerCreate(ctx context.Context, opts *ContainerCreateOptions) (*ContainerInfo, error)
	ContainerStart(ctx context.Context, nameOrId string) error
	ContainerStop(ctx context.Context, nameOrId string) error
	ContainerRemove(ctx context.Context, nameOrId string) error
	ImagePull(ctx context.Context, image string) error
}

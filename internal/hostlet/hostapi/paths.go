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

package hostapi

import (
	"fmt"
	"path"
)

// Container internal paths (paths inside the container)
const (
	// HomeDir is the home directory inside container
	HomeDir = "/home/action"

	// InnerStackDir is the innerstack config directory inside container
	InnerStackDir = "/home/action/.innerstack"

	// AppReplicaFile is the app replica config file path inside container
	AppReplicaFile = "/home/action/.innerstack/app_replica.json"

	// IninitFile is the ininit script path inside container
	IninitFile = "/home/action/.innerstack/ininit"

	// InagentFile is the inagent binary path inside container
	InagentFile = "/home/action/.innerstack/inagent"

	// InagentSock is the inagent unix socket path inside container
	InagentSock = "unix:/home/action/.innerstack/inagent.sock"

	// ContainerEntrypoint is the default container entrypoint command
	ContainerEntrypoint = "/bin/sh"
)

// InitDirs are directories to be created on container initialization
var InitDirs = []string{
	"/home/action/local/bin",
	"/home/action/local/share",
	"/home/action/local/profile.d",
	"/home/action/var/tmp",
	"/home/action/var/log",
	"/home/action/.ssh",
}

// ContainerPath contains paths for a specific app instance/container
type ContainerPath struct {
	// BasePath is the base path for app instance data on host
	BasePath string
	// ContainerName is the container name
	ContainerName string
}

// NewContainerPath creates a ContainerPath instance
func NewContainerPath(basePath, containerName string) *ContainerPath {
	return &ContainerPath{
		BasePath:      basePath,
		ContainerName: containerName,
	}
}

// ContainerBaseDir returns the container-specific directory on host
func (p *ContainerPath) ContainerBaseDir() string {
	return path.Join(p.BasePath, p.ContainerName)
}

// OptDir returns the /opt mount directory on host
func (p *ContainerPath) OptDir() string {
	return path.Join(p.ContainerBaseDir(), "opt")
}

// HomeDir returns the /home mount directory on host
func (p *ContainerPath) HomeDir() string {
	return path.Join(p.ContainerBaseDir(), "home")
}

// InnerStackDir returns the innerstack directory on host
func (p *ContainerPath) InnerStackDir() string {
	return path.Join(p.HomeDir(), "action", ".innerstack")
}

// AppReplicaFile returns the app replica json file path on host
func (p *ContainerPath) AppReplicaFile() string {
	return path.Join(p.InnerStackDir(), "app_replica.json")
}

// IninitFile returns the ininit script path on host
func (p *ContainerPath) IninitFile() string {
	return path.Join(p.InnerStackDir(), "ininit")
}

// InagentFile returns the inagent binary path on host
func (p *ContainerPath) InagentFile() string {
	return path.Join(p.InnerStackDir(), "inagent")
}

// HostSourcePaths contains paths for source files on host
type HostSourcePaths struct {
	// Prefix is the installation prefix directory
	Prefix string
}

// NewHostSourcePaths creates a HostSourcePaths instance
func NewHostSourcePaths(prefix string) *HostSourcePaths {
	return &HostSourcePaths{Prefix: prefix}
}

// IninitSrc returns the source path of ininit script
func (h *HostSourcePaths) IninitSrc() string {
	return path.Join(h.Prefix, "cmd", "inagent", "ininit")
}

// InagentSrc returns the source path of inagent binary for given architecture
func (h *HostSourcePaths) InagentSrc(arch string) string {
	return path.Join(h.Prefix, "bin", fmt.Sprintf("inagent-linux-%s", arch))
}

// InagentSlimSrc returns the source path of inagent slim (C++) binary for given architecture
func (h *HostSourcePaths) InagentSlimSrc(arch string) string {
	return path.Join(h.Prefix, "bin", fmt.Sprintf("inagent-slim-linux-%s", arch))
}

// ContainerCmd returns the container entrypoint command
func ContainerCmd() []string {
	return []string{ContainerEntrypoint, IninitFile}
}

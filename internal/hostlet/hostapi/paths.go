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

	// SysinnerDir is the sysinner config directory inside container
	SysinnerDir = "/home/action/.sysinner"

	// AppInstanceFile is the app instance config file path inside container
	AppInstanceFile = "/home/action/.sysinner/app_instance.json"

	// IninitFile is the ininit script path inside container
	IninitFile = "/home/action/.sysinner/ininit"

	// InagentFile is the inagent binary path inside container
	InagentFile = "/home/action/.sysinner/inagent"

	// InagentSock is the inagent unix socket path inside container
	InagentSock = "unix:/home/action/.sysinner/inagent.sock"

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

// PodPaths contains paths for a specific pod/container
type PodPaths struct {
	// PodBase is the base path for pod data on host
	PodBase string
	// ContainerName is the container name
	ContainerName string
}

// NewPodPaths creates a PodPaths instance
func NewPodPaths(podBase, containerName string) *PodPaths {
	return &PodPaths{
		PodBase:       podBase,
		ContainerName: containerName,
	}
}

// ContainerPodDir returns the container-specific directory on host
func (p *PodPaths) ContainerPodDir() string {
	return path.Join(p.PodBase, p.ContainerName)
}

// OptDir returns the /opt mount directory on host
func (p *PodPaths) OptDir() string {
	return path.Join(p.ContainerPodDir(), "opt")
}

// HomeDir returns the /home mount directory on host
func (p *PodPaths) HomeDir() string {
	return path.Join(p.ContainerPodDir(), "home")
}

// SysinnerDir returns the sysinner directory on host
func (p *PodPaths) SysinnerDir() string {
	return path.Join(p.HomeDir(), "action", ".sysinner")
}

// AppInstanceFile returns the app instance json file path on host
func (p *PodPaths) AppInstanceFile() string {
	return path.Join(p.SysinnerDir(), "app_instance.json")
}

// IninitFile returns the ininit script path on host
func (p *PodPaths) IninitFile() string {
	return path.Join(p.SysinnerDir(), "ininit")
}

// InagentFile returns the inagent binary path on host
func (p *PodPaths) InagentFile() string {
	return path.Join(p.SysinnerDir(), "inagent")
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

// ContainerCmd returns the container entrypoint command
func ContainerCmd() []string {
	return []string{ContainerEntrypoint, IninitFile}
}

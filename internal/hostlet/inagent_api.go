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

package hostlet

import (
	"fmt"
	"log/slog"
	"net/http"

	"github.com/hooto/httpsrv"

	"github.com/sysinner/innerstack/v2/internal/config"
	"github.com/sysinner/innerstack/v2/internal/hostlet/hoststatus"
	"github.com/sysinner/innerstack/v2/pkg/inapi"
)

// InagentStatusPath is the full HTTP path the inagent POSTs stage reports to.
// It is served on the shared HTTP server (ServerConfig.HttpPort) under the
// hostlet module.
const InagentStatusPath = "/in/api/v2/hostlet/inagent/status"

// inagentStatusAction is the action name (relative to the hostlet module).
const inagentStatusAction = "/inagent/status"

// NewHostletModule builds the hostlet HTTP module, mounted under
// "/in/api/v2/hostlet" by the server. It currently exposes the inagent
// stage-progress reporting endpoint.
func NewHostletModule() *httpsrv.Module {
	mod := httpsrv.NewModule()
	mod.RegisterAction(inagentStatusAction, httpsrvInagentStatus)
	return mod
}

// inagentEndpointURL builds the hostlet status API URL the inagent should
// POST to, using the host peer (LAN) address and the shared HTTP port.
// Returns "" when the address or port is unavailable (inagent then skips
// reporting).
func inagentEndpointURL() string {
	addr := config.Config.Hostlet.LanAddr
	port := config.Config.Server.HttpPort
	if addr == "" || port == 0 {
		return ""
	}
	return fmt.Sprintf("http://%s:%d%s", addr, port, InagentStatusPath)
}

// httpsrvInagentStatus is the Ctx adapter for the inagent status endpoint.
func httpsrvInagentStatus(ctx httpsrv.Ctx) error {
	if ctx.Request().Method != http.MethodPost {
		ctx.Status(http.StatusMethodNotAllowed)
		return ctx.JSON(map[string]string{"error": "method not allowed"})
	}

	var report inapi.InagentStatusReport
	if err := ctx.Request().JsonDecode(&report); err != nil {
		slog.Warn("inagent status: bad request", "err", err)
		ctx.Status(http.StatusBadRequest)
		return ctx.JSON(map[string]string{"error": "bad request"})
	}

	code, msg := mergeInagentStatus(&report, ctx.Header("X-Secret-Key"))
	if code == http.StatusOK {
		slog.Debug("inagent status report merged",
			"instance", report.InstanceName, "replica", report.ReplicaId,
			"stages", len(report.Stages))
	} else {
		slog.Warn("inagent status report rejected",
			"instance", report.InstanceName, "replica", report.ReplicaId,
			"code", code, "msg", msg)
	}
	ctx.Status(code)
	if code == http.StatusOK {
		return ctx.JSON(map[string]string{"ok": "true"})
	}
	return ctx.JSON(map[string]string{"error": msg})
}

// mergeInagentStatus verifies the per-replica secret and merges the reported
// stages into the matching ReplicaStageEntry. It returns the HTTP status code
// to respond with and a message.
func mergeInagentStatus(report *inapi.InagentStatusReport, secret string) (int, string) {
	if report == nil || report.InstanceName == "" {
		return http.StatusBadRequest, "instance_name required"
	}

	// The container name is derived from the reported identity so a container
	// cannot report for another replica.
	want := hoststatus.Active.SecretKey(
		inapi.ContainerName(report.InstanceName, report.ReplicaId))
	if want == "" || secret == "" || secret != want {
		return http.StatusUnauthorized, "unauthorized"
	}

	hoststatus.ReplicaStage(report.InstanceName, report.ReplicaId).
		MergeInagent(report.Stages)

	return http.StatusOK, "OK"
}

// ensureHostletEndpoint injects the hostlet status API URL and a per-replica
// secret into the AppReplicaInstance before it is written to app_replica.json.
// The secret is generated on first use and persisted to hostlet_active.json.
// When the endpoint URL cannot be determined (e.g. peer address unknown) the
// inagent simply does not report.
func ensureHostletEndpoint(rep *inapi.AppReplicaInstance) {
	if rep == nil {
		return
	}
	url := inagentEndpointURL()
	if url == "" {
		slog.Warn("inagent status endpoint not provisioned: peer addr or http port unset",
			"lan_addr", config.Config.Hostlet.LanAddr,
			"http_port", config.Config.Server.HttpPort)
		return
	}
	secret, changed := hoststatus.Active.EnsureSecretKey(rep.ContainerName())
	rep.HostletEndpoint = &inapi.HostletStatusEndpoint{
		Url:       url,
		SecretKey: secret,
	}
	if changed {
		saveHostActiveConfig()
		slog.Info("inagent status endpoint provisioned",
			"container", rep.ContainerName(), "url", url)
	}
}

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

package status

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"google.golang.org/protobuf/proto"

	"github.com/sysinner/innerstack/v2/pkg/inapi"
)

const (
	heartbeatInterval = 30 * time.Second
	httpTimeout       = 3 * time.Second
)

var (
	mu        sync.Mutex
	stages    []*inapi.AppDeployStage
	dirty     bool
	lastFlush time.Time
	revision  uint64 // AppDeploy.Revision stages are stamped with
)

// child returns the inagent stage node by name, creating it if absent.
func child(name string) *inapi.AppDeployStage {
	for _, s := range stages {
		if s != nil && s.Name == name {
			return s
		}
	}
	s := &inapi.AppDeployStage{
		Name:  name,
		Owner: inapi.AppStageOwnerInagent,
		State: inapi.AppStageStatePending,
	}
	stages = append(stages, s)
	return s
}

// SetRevision sets the AppDeploy.Revision stages are stamped with. When the
// revision changes, prior stages are cleared (they were based on stale meta).
func SetRevision(rev uint64) {
	mu.Lock()
	defer mu.Unlock()
	if revision == rev {
		return
	}
	revision = rev
	stages = nil
	dirty = true
}

// apply transitions a named stage to the given state and marks the reporter
// dirty. It is a no-op when the state and message are unchanged.
func apply(name, state, msg string) {
	mu.Lock()
	defer mu.Unlock()
	s := child(name)
	if s.State == state && s.Message == msg && s.Revision == revision {
		return
	}
	s.Owner = inapi.AppStageOwnerInagent
	switch state {
	case inapi.AppStageStateRunning:
		s.SetRunning(msg)
	case inapi.AppStageStateSuccess:
		s.SetSuccess(msg)
	case inapi.AppStageStateFailed:
		s.SetFailed(msg)
	}
	s.Revision = revision
	dirty = true
}

// SetBoot records inagent daemon startup as an instantaneous stage.
func SetBoot() {
	mu.Lock()
	defer mu.Unlock()
	s := child(inapi.AppDeployStageNameInagentBoot)
	if s.State == inapi.AppStageStateSuccess && s.Revision == revision {
		return
	}
	s.Owner = inapi.AppStageOwnerInagent
	s.SetInstant("")
	s.Revision = revision
	dirty = true
}

// SetSpecLoad records the app_replica.json load result.
func SetSpecLoad(state, msg string) {
	apply(inapi.AppDeployStageNameSpecLoad, state, msg)
}

// SetTaskRun records the aggregate OnStartup task execution state.
func SetTaskRun(state, msg string) {
	apply(inapi.AppDeployStageNameTaskRun, state, msg)
}

// Flush POSTs the current stage tree to the hostlet status API when there are
// unsent changes or the heartbeat interval has elapsed. On success it clears
// the dirty flag. A missing endpoint (old hostlet / not yet provisioned) is a
// no-op.
func Flush(endpoint *inapi.HostletStatusEndpoint, instanceName string, repId uint32) {
	if endpoint == nil || endpoint.Url == "" {
		return
	}

	mu.Lock()
	if !dirty && time.Since(lastFlush) < heartbeatInterval {
		mu.Unlock()
		return
	}
	snap := cloneStages(stages)
	mu.Unlock()

	body, err := json.Marshal(&inapi.InagentStatusReport{
		InstanceName: instanceName,
		ReplicaId:    repId,
		Updated:      time.Now().UnixMilli(),
		Stages:       snap,
	})
	if err != nil {
		return
	}

	req, err := http.NewRequest(http.MethodPost, endpoint.Url, bytes.NewReader(body))
	if err != nil {
		slog.Warn("inagent status post: build request failed", "err", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Secret-Key", endpoint.SecretKey)

	client := &http.Client{Timeout: httpTimeout}
	resp, err := client.Do(req)
	if err != nil {
		slog.Warn("inagent status post failed", "url", endpoint.Url, "err", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		mu.Lock()
		dirty = false
		lastFlush = time.Now()
		mu.Unlock()
		slog.Debug("inagent status posted", "url", endpoint.Url, "stages", len(snap))
	} else {
		slog.Warn("inagent status post rejected", "url", endpoint.Url, "status", resp.StatusCode)
	}
}

// cloneStages returns a deep copy of the stage list so the POST body is a
// stable snapshot decoupled from later mutations.
func cloneStages(src []*inapi.AppDeployStage) []*inapi.AppDeployStage {
	if len(src) == 0 {
		return nil
	}
	dst := make([]*inapi.AppDeployStage, 0, len(src))
	for _, s := range src {
		if s == nil {
			continue
		}
		dst = append(dst, proto.Clone(s).(*inapi.AppDeployStage))
	}
	return dst
}

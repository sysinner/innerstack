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

package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/sysinner/innerstack/v2/pkg/inapi"
)

// watchDeployStages polls AppInstanceInfo every second and renders the
// deploy stage tree in the terminal until the deploy reaches a terminal
// state (all replicas reach the action's target stage, or any stage fails),
// the timeout elapses, or the user interrupts. It returns nil on success
// or interruption, and an error only on unrecoverable polling failure.
func watchDeployStages(
	zc inapi.ZoneServiceClient, name, action string, timeout time.Duration,
) error {

	effAction := action
	if effAction == "" {
		effAction = inapi.OpActionStart
	}
	terminalStage := deployTerminalStage(effAction)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	start := time.Now()

	// Grace period after all replicas reach container_running, to let the
	// inagent report task_run before giving up (e.g. stale/slim inagent
	// without the reporter). Bounds the wait so the watch does not hang for
	// the full timeout.
	const inagentGrace = 30 * time.Second

	var fallbackSince time.Time

	// Render immediately, then on each tick.
	render := func() (terminal bool, failed bool) {
		info, err := fetchInstanceInfo(zc, name)
		now := time.Now().UnixMilli()
		clearScreen()
		terminal, failed, fallback := renderDeployStages(info, err, name, effAction, terminalStage, start.UnixMilli(), now)
		if fallback {
			if fallbackSince.IsZero() {
				fallbackSince = time.Now()
			}
		} else {
			fallbackSince = time.Time{}
		}
		// Fallback: containers are running but the inagent never reported
		// any stage within the grace window; treat the deploy as done.
		if !terminal && fallback && !fallbackSince.IsZero() &&
			time.Since(fallbackSince) >= inagentGrace {
			terminal = true
		}
		return terminal, failed
	}

	terminal, failed := render()
	for !terminal {
		select {
		case <-ctx.Done():
			fmt.Printf("\nwatch stopped: %v\n", ctx.Err())
			return nil
		case <-ticker.C:
		}
		terminal, failed = render()
	}

	// Final static summary (no more clear-screen).
	fmt.Println()
	if failed {
		fmt.Printf("deploy stages for '%s' ended with failure\n", name)
	} else {
		fmt.Printf("deploy stages for '%s' completed successfully\n", name)
	}
	return nil
}

// deployTerminalStage returns the stage name whose success marks the deploy
// as complete for the given action. For start, the deploy is not complete
// until the inagent reports task_run success (the app is ready), which also
// ensures inagent stages are observed before the watch exits.
func deployTerminalStage(action string) string {
	switch action {
	case inapi.OpActionStop:
		return inapi.AppDeployStageNameContainerStop
	case inapi.OpActionDestroy:
		return inapi.AppDeployStageNameContainerDestroy
	default:
		return inapi.AppDeployStageNameTaskRun
	}
}

// fetchInstanceInfo loads the current app instance state (including
// Deploy.Stages) from the zone leader.
func fetchInstanceInfo(zc inapi.ZoneServiceClient, name string) (*inapi.AppInstance, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	resp, err := zc.AppInstanceInfo(ctx, &inapi.AppInstanceInfoRequest{Name: name})
	if err != nil {
		return nil, err
	}
	if resp.Instance == nil {
		return nil, fmt.Errorf("instance not found")
	}
	return resp.Instance, nil
}

// renderDeployStages prints the stage tree and reports whether the deploy
// reached a terminal state. Returns (terminal, failed, fallbackReady) where
// fallbackReady indicates all replicas reached container_running success
// while no inagent stage has been observed yet (used to bound the wait for an
// inagent that never reports).
func renderDeployStages(
	instance *inapi.AppInstance, fetchErr error,
	name, action, terminalStage string,
	startMs, nowMs int64,
) (terminal, failed, fallbackReady bool) {

	elapsed := time.Duration(nowMs-startMs) * time.Millisecond

	fmt.Printf("Watching deploy stages for '%s' (action=%s)  elapsed=%s  Ctrl-C to stop\n",
		name, action, fmtDur(int64(elapsed/time.Millisecond)))
	fmt.Println(strings.Repeat("-", 70))

	if fetchErr != nil {
		fmt.Printf("waiting for stage data: %v\n", fetchErr)
		return false, false, false
	}

	var (
		root       *inapi.AppDeployStage
		replicaCap uint32
	)
	if instance.Deploy != nil {
		root = instance.Deploy.Stages
		replicaCap = instance.Deploy.ReplicaCap
	}
	if replicaCap == 0 {
		replicaCap = 1
	}

	if root == nil {
		fmt.Println("waiting for stage data...")
		return false, false, false
	}

	// Sync barrier: the stage tree is only valid when its root revision has
	// caught up to the desired Deploy.Revision. A lagging root means the
	// deploy RPC bumped Deploy.Revision but the scheduler has not yet
	// reconciled the stage tree (e.g. stale replica stages from the previous
	// revision are still present). Wait for the new state to sync rather than
	// risking a premature terminal evaluation.
	if instance.Deploy != nil && root.Revision < instance.Deploy.Revision {
		fmt.Printf("waiting for stage data to sync (stages rev %d -> deploy rev %d)...\n",
			root.Revision, instance.Deploy.Revision)
		return false, false, false
	}

	// Instance-level stages and replica nodes.
	renderStageChildren(root.Stages, 0, nowMs)

	// Terminal evaluation across all expected replicas.
	done, failed := 0, false
	readyCount := 0
	inagentSeen := false
	for repId := uint32(0); repId < replicaCap; repId++ {
		repNode := findReplicaNode(root, repId)
		if repNode == nil {
			continue
		}
		switch replicaNodeStatus(repNode, terminalStage) {
		case "done":
			done++
		case "failed":
			failed = true
		}
		if s := repNode.Find(inapi.AppDeployStageNameContainerRunning); s != nil &&
			s.State == inapi.AppStageStateSuccess {
			readyCount++
		}
		for n := range inapi.AppDeployStageInagentNames {
			if repNode.Find(n) != nil {
				inagentSeen = true
			}
		}
	}

	fmt.Println(strings.Repeat("-", 70))
	agg := "RUN"
	switch {
	case failed:
		agg = "FAIL"
	case done >= int(replicaCap):
		agg = "OK"
	}
	fmt.Printf("State: %s   replicas: %d/%d", agg, done, replicaCap)
	if failed {
		fmt.Print("   (one or more stages failed)")
	}
	fmt.Println()

	return (done >= int(replicaCap)) || failed, failed,
		readyCount >= int(replicaCap) && !inagentSeen
}

// renderStageChildren recursively prints stage nodes. Replica nodes are
// rendered as a "Replica <id>" header; schedule nodes print their children
// indented beneath them.
func renderStageChildren(stages []*inapi.AppDeployStage, indent int, nowMs int64) {
	// Stable, semantic ordering by a predefined name priority.
	ordered := append([]*inapi.AppDeployStage(nil), stages...)
	sort.SliceStable(ordered, func(i, j int) bool {
		return stageOrder(ordered[i].Name) < stageOrder(ordered[j].Name)
	})

	pad := strings.Repeat(" ", indent)
	for _, s := range ordered {
		if s == nil {
			continue
		}
		if s.Name == inapi.AppDeployStageNameReplica {
			repId := s.Attrs[inapi.AppDeployStageReplicaAttrRepId]
			fmt.Printf("%sReplica %s\n", pad, repId)
			renderStageChildren(s.Stages, indent+2, nowMs)
			continue
		}
		printStageLine(s, pad, nowMs)
		renderStageChildren(s.Stages, indent+2, nowMs)
	}
}

// printStageLine prints a single stage row: marker, owner, name, duration,
// message.
func printStageLine(s *inapi.AppDeployStage, pad string, nowMs int64) {
	msg := s.Message
	fmt.Printf("%s%s %-8s %-22s %8s", pad, stateMark(s.State), s.Owner, s.Name, stageDuration(s, nowMs))
	if msg != "" {
		fmt.Printf("  %s", msg)
	}
	fmt.Println()
}

// stateMark returns a fixed-width ASCII status marker for a stage state.
func stateMark(state string) string {
	switch state {
	case inapi.AppStageStateSuccess:
		return "OK  "
	case inapi.AppStageStateRunning:
		return "RUN "
	case inapi.AppStageStateFailed:
		return "FAIL"
	default:
		return "WAIT"
	}
}

// stageDuration formats a stage's duration: elapsed-while-running, or the
// completed span; empty when the stage has not started yet.
func stageDuration(s *inapi.AppDeployStage, nowMs int64) string {
	if s.Created == 0 {
		return ""
	}
	if s.Finished >= s.Created {
		return fmtDur(s.Finished - s.Created)
	}
	return fmtDur(nowMs - s.Created)
}

// fmtDur formats a millisecond duration compactly.
func fmtDur(ms int64) string {
	if ms < 0 {
		ms = 0
	}
	if ms < 1000 {
		return strconv.FormatInt(ms, 10) + "ms"
	}
	if ms < 60_000 {
		return fmt.Sprintf("%.1fs", float64(ms)/1000.0)
	}
	return fmt.Sprintf("%dm%ds", ms/60_000, (ms%60_000)/1000)
}

// stageOrder imposes a stable, semantic ordering on stage names for display.
// Unknown names sort last by their literal string.
func stageOrder(name string) int {
	order := []string{
		inapi.AppDeployStageNameReqValidate,
		inapi.AppDeployStageNameInstancePersist,
		inapi.AppDeployStageNameReplica,
		inapi.AppDeployStageNameSchedule,
		inapi.AppDeployStageNameHostFit,
		inapi.AppDeployStageNameHostPrioritize,
		inapi.AppDeployStageNameIpamAlloc,
		inapi.AppDeployStageNamePortAlloc,
		inapi.AppDeployStageNameDeliver,
		inapi.AppDeployStageNameHostRecv,
		inapi.AppDeployStageNameImagePull,
		inapi.AppDeployStageNamePkgDownload,
		inapi.AppDeployStageNameProvision,
		inapi.AppDeployStageNameContainerCreate,
		inapi.AppDeployStageNameContainerStart,
		inapi.AppDeployStageNameContainerRunning,
		inapi.AppDeployStageNameContainerStop,
		inapi.AppDeployStageNameContainerDestroy,
		inapi.AppDeployStageNameInagentBoot,
		inapi.AppDeployStageNameSpecLoad,
		inapi.AppDeployStageNameTaskRun,
	}
	for i, n := range order {
		if n == name {
			return i
		}
	}
	return len(order)
}

// findReplicaNode returns the per-replica child node whose attrs "rep_id"
// matches repId, or nil if absent (replica not yet placed).
func findReplicaNode(root *inapi.AppDeployStage, repId uint32) *inapi.AppDeployStage {
	if root == nil {
		return nil
	}
	want := strconv.FormatUint(uint64(repId), 10)
	for _, s := range root.Stages {
		if s == nil || s.Name != inapi.AppDeployStageNameReplica {
			continue
		}
		if s.Attrs != nil && s.Attrs[inapi.AppDeployStageReplicaAttrRepId] == want {
			return s
		}
	}
	return nil
}

// replicaNodeStatus returns "done" if the replica reached the terminal stage
// successfully, "failed" if any of its stages failed, or "" while in
// progress.
func replicaNodeStatus(repNode *inapi.AppDeployStage, terminalStage string) string {
	if repNode == nil {
		return ""
	}
	if stageTreeHasFailed(repNode) {
		return "failed"
	}
	if t := repNode.Find(terminalStage); t != nil && t.State == inapi.AppStageStateSuccess {
		return "done"
	}
	return ""
}

// stageTreeHasFailed reports whether any stage in the subtree (including the
// node itself) is in the failed state.
func stageTreeHasFailed(s *inapi.AppDeployStage) bool {
	if s == nil {
		return false
	}
	if s.State == inapi.AppStageStateFailed {
		return true
	}
	for _, c := range s.Stages {
		if stageTreeHasFailed(c) {
			return true
		}
	}
	return false
}

// clearScreen clears the terminal and moves the cursor home.
func clearScreen() {
	fmt.Print("\033[2J\033[H")
}

// Copyright 2026 Eryx <evorui at gmail dot com>, All rights reserved.
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

package zonelet

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync/atomic"
	"time"

	"google.golang.org/protobuf/proto"

	"github.com/sysinner/innerstack/v2/internal/config"
	"github.com/sysinner/innerstack/v2/internal/data"
	"github.com/sysinner/innerstack/v2/internal/inutil"
	"github.com/sysinner/innerstack/v2/internal/status"
	"github.com/sysinner/innerstack/v2/pkg/inapi"
)

// Built-in alert evaluation. The leader
// evaluates a small data-driven rule table every alertEvalTicks loop passes
// (1s loop -> 5s cadence) over data already in memory: the registered host
// set and the host status report cache. Alert state transitions are
// write-through persisted to kvgo so a new leader resumes from the same
// records; the AlertList RPC reads the store, not this state.

const (
	// alertEvalTicks is the evaluation interval in zonelet loop ticks
	// (1 tick = 1s).
	alertEvalTicks uint64 = 5

	// alertResolvedTTLMs lets resolved alert records expire from the store
	// after 30 days; firing records carry no TTL.
	alertResolvedTTLMs int64 = 30 * 86400 * 1000

	// alertFiringRefreshSec bounds how often a steady firing record is
	// rewritten (last_ts/count refresh) to roughly one write per minute.
	alertFiringRefreshSec int64 = 60

	// alertNotifyMaxInFlight bounds concurrent webhook deliveries so a mass
	// transition cannot spawn an unbounded goroutine burst.
	alertNotifyMaxInFlight int32 = 16

	// alertNotifyTimeout bounds one webhook POST.
	alertNotifyTimeout = 3 * time.Second
)

// alertRuleSpec drives the shared streak state machine per rule. The
// fire/resolve streaks debounce flapping evaluations; the freshness
// threshold itself is the host_down debounce, so it fires on the first
// breach.
type alertRuleSpec struct {
	id            string
	fireStreak    int // consecutive positive evaluations before firing
	resolveStreak int // consecutive negative evaluations before resolving
}

var alertRuleSpecs = []alertRuleSpec{
	{inapi.AlertRuleHostDown, 1, 2},
	{inapi.AlertRuleDiskFull, 2, 1},
	{inapi.AlertRuleReplicaCrashloop, 1, 1},
}

func alertRuleSpecOf(rule string) (alertRuleSpec, bool) {
	for _, spec := range alertRuleSpecs {
		if spec.id == rule {
			return spec, true
		}
	}
	return alertRuleSpec{}, false
}

// alertEntry is the in-memory evaluation state of one (rule, target) pair.
type alertEntry struct {
	alert        *inapi.Alert // persisted shape
	posRun       int          // consecutive positive evaluations
	negRun       int          // consecutive negative evaluations
	lastWriteSec int64        // last store write (transition or refresh)
}

// restartTracker derives crashloop events from a replica's cumulative
// restart counter. Events are evaluation-pass timestamps: a delta of N
// counts as N restart events at the first evaluation that observes it. A
// counter decrease (container recreation) resets the window.
type restartTracker struct {
	instance  string  // owning instance, for the liveness sweep
	seen      bool    // first sighting only seeds the baseline
	lastCount int32   // last observed cumulative counter
	events    []int64 // unix seconds of restart events, ascending
}

// alertsRuntime is the leader's alert evaluation state. It is only touched
// from the zonelet loop goroutine (the RPC path reads the store instead), so
// it needs no locking.
type alertsRuntime struct {
	loaded bool
	// entries holds firing or streak-building alerts, keyed "{rule}/{target}".
	// Resolved alerts are dropped from memory; their store records expire by
	// TTL.
	entries map[string]*alertEntry
	// kvStatus caches the last persisted status of hosts that have not
	// reported since this leader started, so freshness is still judgeable
	// right after a leader switch.
	kvStatus map[string]*inapi.HostStatus
	// restarts holds the crashloop event windows, keyed by container name
	// (i8k_{instance}_{replica}), diffed from the replica metrics cache
	// each evaluation pass.
	restarts map[string]*restartTracker
}

var alertsRT alertsRuntime

func (r *alertsRuntime) reset() {
	*r = alertsRuntime{}
}

// alertsRefresh is the evaluation entry point, called from the zonelet loop
// every alertEvalTicks. now is unix seconds.
func alertsRefresh(now int64) {

	if config.Config.Zonelet.ZoneName == "" {
		return
	}

	if !status.IsZoneletLeader() {
		if alertsRT.loaded {
			// Dropping the state forces a reload from the store on the next
			// leadership term; another leader may have changed it meanwhile.
			alertsRT.reset()
		}
		return
	}

	if !alertsRT.loaded {
		if err := alertsLoadFromStore(); err != nil {
			slog.Error("alerts state load failed", "err", err.Error())
			return
		}
		alertsRT.loaded = true
		alertsRT.restarts = map[string]*restartTracker{}
	}

	// The host registry may not be populated yet on a fresh leader; with an
	// empty universe the removal sweep below would wrongly resolve every
	// firing alert.
	if gHostSet.Len() == 0 {
		return
	}

	alertsEvalHosts(now)
	alertsEvalCrashloop(now)
}

// alertsLoadFromStore seeds the in-memory state from persisted firing
// records.
func alertsLoadFromStore() error {

	alertsRT.entries = map[string]*alertEntry{}
	alertsRT.kvStatus = map[string]*inapi.HostStatus{}
	alertsRT.restarts = map[string]*restartTracker{}

	prefix := inapi.NsZoneletAlertPrefix(config.Config.Zonelet.ZoneName)
	rs := data.Zonelet.NewRanger(prefix, append(prefix, 0xff)).
		SetLimit(inapi.AlertScanLimit).
		Exec()
	if !rs.OK() {
		if rs.NotFound() {
			return nil
		}
		return rs.Error()
	}

	for _, item := range rs.Items {
		a := &inapi.Alert{}
		if err := item.JsonDecode(a); err != nil {
			continue
		}
		if a.State != inapi.AlertStateFiring || a.Rule == "" || a.Target == "" {
			continue
		}
		alertsRT.entries[a.Rule+"/"+a.Target] = &alertEntry{
			alert:        a,
			posRun:       int(a.BreachCount),
			lastWriteSec: a.UpdateTime,
		}
	}

	return nil
}

// alertsEvalHosts runs the host-scoped rules over the registered host set
// and sweeps firing alerts whose target left the universe.
func alertsEvalHosts(now int64) {

	evaluated := make(map[string]struct{}, gHostSet.Len())

	downLimit := config.Config.Alerts.HostDownSec()
	diskLimit := config.Config.Alerts.DiskFreePercentV()

	gHostSet.Iter(func(kvHost *inapi.KvEntry) bool {
		host, ok := kvHost.Value.(*inapi.Host)
		if !ok || host.Id == "" {
			return true
		}
		evaluated[host.Id] = struct{}{}

		hs := alertHostStatus(host.Id)

		// host_down: freshness of the last status report.
		downBreach, downMsg := false, ""
		if hs != nil {
			if age := now - hs.Updated; age > downLimit {
				downBreach = true
				downMsg = fmt.Sprintf("report age %ds > %ds", age, downLimit)
			}
		}
		alertEvalCondition(inapi.AlertRuleHostDown, host.Id, downBreach, now, downMsg)

		// disk_full: sustained low free-disk ratio.
		diskBreach, diskMsg := false, ""
		if hs != nil && hs.DiskTotalBytes > 0 &&
			hs.DiskFreeBytes*100 < diskLimit*hs.DiskTotalBytes {
			diskBreach = true
			diskMsg = fmt.Sprintf("disk free %s / %s < %d%%",
				inutil.PrettyBytes(hs.DiskFreeBytes, 1024),
				inutil.PrettyBytes(hs.DiskTotalBytes, 1024),
				diskLimit)
		}
		alertEvalCondition(inapi.AlertRuleDiskFull, host.Id, diskBreach, now, diskMsg)

		return true
	})

	// Targets of host-scoped rules that left the registered host set (host
	// removed/retired) resolve at once: a removed host never reports again
	// and would otherwise fire host_down forever. Replica-scoped alerts are
	// resolved by the crashloop evaluation instead.
	for id, e := range alertsRT.entries {
		if e.alert.Rule == inapi.AlertRuleReplicaCrashloop {
			continue
		}
		if _, ok := evaluated[e.alert.Target]; ok {
			continue
		}
		alertResolve(id, e, now, "target removed from zone")
	}
}

// alertsEvalCrashloop evaluates the replica_crashloop rule: it diffs the
// freshly reported cumulative restart counters (the replica metrics cache,
// refreshed by every 3s status report) into per-replica event windows, then
// applies the window threshold and sweeps trackers whose instance is gone.
// The counters ride the existing metrics channel, so no separate report
// path exists; a first sighting only seeds the baseline (restarts before
// this leader observed the container have no timestamps).
func alertsEvalCrashloop(now int64) {

	gReplicaMetrics.mu.RLock()
	for name, m := range gReplicaMetrics.items {
		instance, _, ok := inapi.ParseContainerName(name)
		if !ok || m == nil {
			continue
		}
		tr := alertsRT.restarts[name]
		if tr == nil {
			tr = &restartTracker{instance: instance}
			alertsRT.restarts[name] = tr
		}
		switch {
		case !tr.seen:
			tr.seen = true
			tr.lastCount = m.RestartCount
		case m.RestartCount > tr.lastCount:
			for i := tr.lastCount; i < m.RestartCount; i++ {
				tr.events = append(tr.events, now)
			}
			tr.lastCount = m.RestartCount
		case m.RestartCount < tr.lastCount:
			// Counter reset: the container was recreated; the window
			// restarts.
			tr.events = nil
			tr.lastCount = m.RestartCount
		}
	}
	gReplicaMetrics.mu.RUnlock()

	window := config.Config.Alerts.CrashloopWindowSec()
	threshold := int(config.Config.Alerts.CrashloopRestartsV())

	for name, tr := range alertsRT.restarts {
		if gAppInstanceSet.Load(tr.instance) != nil {
			// Drop events that aged out of the window.
			fresh := tr.events[:0]
			for _, ts := range tr.events {
				if now-ts <= window {
					fresh = append(fresh, ts)
				}
			}
			tr.events = fresh

			breach := len(fresh) >= threshold
			msg := ""
			if breach {
				msg = fmt.Sprintf("%d restarts within %ds", len(fresh), window)
			}
			alertEvalCondition(inapi.AlertRuleReplicaCrashloop, name, breach, now, msg)
			continue
		}

		// Instance gone: drop the tracker and resolve a firing alert.
		delete(alertsRT.restarts, name)
		if e, ok := alertsRT.entries[inapi.AlertRuleReplicaCrashloop+"/"+name]; ok {
			alertResolve(inapi.AlertRuleReplicaCrashloop+"/"+name, e, now,
				"replica removed")
		}
	}
}

// alertHostStatus returns the freshest known status of a host: the live
// report cache first, then a one-time store fallback (misses included, so a
// host that never persisted a report costs one store read per leader term,
// not per pass). The fallback matters right after a leader switch, before
// any host has reported to this leader: the persisted record still carries
// the previous leader's last report time.
func alertHostStatus(hostId string) *inapi.HostStatus {

	if v, ok := status.Zonelet_HostStatusSet.Load(hostId); ok {
		if hs, ok := v.(*inapi.HostStatus); ok {
			return hs
		}
	}
	if hs, ok := alertsRT.kvStatus[hostId]; ok {
		return hs
	}

	var hs inapi.HostStatus
	rs := data.Zonelet.NewReader(
		inapi.NsHostStatus(config.Config.Zonelet.ZoneName, hostId)).Exec()
	if rs.OK() {
		if err := rs.Item().JsonDecode(&hs); err == nil && hs.Updated > 0 {
			alertsRT.kvStatus[hostId] = &hs
			return &hs
		}
	}

	// Cache the miss (a nil value under a present key).
	alertsRT.kvStatus[hostId] = nil
	return nil
}

// alertEvalCondition applies one rule/target evaluation to the streak state
// machine and persists transitions. now is unix seconds.
func alertEvalCondition(rule, target string, positive bool, now int64, message string) {

	spec, ok := alertRuleSpecOf(rule)
	if !ok {
		return
	}

	id := rule + "/" + target
	e := alertsRT.entries[id]
	if e == nil {
		if !positive {
			// Healthy and never alerted: nothing to record.
			return
		}
		e = &alertEntry{
			alert: &inapi.Alert{
				Zone:   config.Config.Zonelet.ZoneName,
				Rule:   rule,
				Target: target,
			},
		}
		alertsRT.entries[id] = e
	}

	if positive {
		e.negRun = 0
		e.posRun++
		e.alert.UpdateTime = now
		if message != "" {
			e.alert.Message = message
		}

		switch {
		case e.alert.State == inapi.AlertStateFiring:
			e.alert.BreachCount++
			if now-e.lastWriteSec >= alertFiringRefreshSec {
				alertsWrite(e.alert, 0)
				e.lastWriteSec = now
			}

		case e.posRun >= spec.fireStreak:
			e.alert.State = inapi.AlertStateFiring
			e.alert.StartTime = now
			e.alert.BreachCount = 1
			alertsWrite(e.alert, 0)
			e.lastWriteSec = now
			alertNotify(e.alert)
			slog.Warn("alert firing",
				"rule", e.alert.Rule, "target", e.alert.Target,
				"message", e.alert.Message)
		}
		return
	}

	// Negative evaluation.
	e.posRun = 0
	if e.alert.State != inapi.AlertStateFiring {
		// The positive streak broke before reaching the fire threshold:
		// drop the candidate.
		delete(alertsRT.entries, id)
		return
	}

	e.negRun++
	if e.negRun >= spec.resolveStreak {
		alertResolve(id, e, now, "condition cleared")
	}
}

// alertResolve transitions a firing alert to resolved, persists it with a
// TTL and notifies; the in-memory entry is dropped so a recurrence starts a
// fresh incident.
func alertResolve(id string, e *alertEntry, now int64, message string) {

	e.alert.State = inapi.AlertStateResolved
	e.alert.UpdateTime = now
	if message != "" {
		e.alert.Message = message
	}

	alertsWrite(e.alert, alertResolvedTTLMs)
	alertNotify(e.alert)
	delete(alertsRT.entries, id)

	slog.Info("alert resolved",
		"rule", e.alert.Rule, "target", e.alert.Target, "message", message)
}

// alertsWrite persists one alert state record; ttlMs > 0 applies an expiry
// (resolved records), otherwise the record stays until overwritten.
func alertsWrite(a *inapi.Alert, ttlMs int64) {
	w := data.Zonelet.NewWriter(
		inapi.NsZoneletAlert(a.Zone, a.Rule+"/"+a.Target), a)
	if ttlMs > 0 {
		w.SetTTL(ttlMs)
	}
	if rs := w.Exec(); !rs.OK() {
		slog.Error("alert state write failed",
			"rule", a.Rule, "target", a.Target, "err", rs.ErrorMessage())
	}
}

// alertNotify delivers a state transition to the configured webhook, off
// the evaluation loop. Failures are logged and never retried: notification
// loss does not affect the state machine.
var alertNotifyInFlight atomic.Int32

func alertNotify(a *inapi.Alert) {

	url := config.Config.Alerts.Webhook()
	if url == "" {
		return
	}

	if alertNotifyInFlight.Load() >= alertNotifyMaxInFlight {
		slog.Warn("alert webhook skipped, too many in flight",
			"rule", a.Rule, "target", a.Target)
		return
	}

	ev := proto.Clone(a).(*inapi.Alert)
	alertNotifyInFlight.Add(1)
	go func() {
		defer alertNotifyInFlight.Add(-1)
		alertWebhookPost(url, ev)
	}()
}

func alertWebhookPost(url string, a *inapi.Alert) {

	body, err := json.Marshal(map[string]any{
		"zone":    a.Zone,
		"rule":    a.Rule,
		"target":  a.Target,
		"state":   a.State,
		"ts":      a.UpdateTime,
		"message": a.Message,
	})
	if err != nil {
		slog.Error("alert webhook marshal failed", "err", err.Error())
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), alertNotifyTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		slog.Error("alert webhook request build failed", "err", err.Error())
		return
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		slog.Warn("alert webhook post failed", "err", err.Error())
		return
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, io.LimitReader(resp.Body, 4<<10))

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		slog.Warn("alert webhook post rejected",
			"status", resp.StatusCode, "rule", a.Rule, "target", a.Target)
	}
}

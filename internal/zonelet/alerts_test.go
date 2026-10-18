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
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/lynkdb/kvgo/v2/pkg/kvapi"

	"github.com/sysinner/innerstack/v2/internal/config"
	"github.com/sysinner/innerstack/v2/internal/data"
	zstatus "github.com/sysinner/innerstack/v2/internal/status"
	"github.com/sysinner/innerstack/v2/pkg/inapi"
)

const alertsTestZone = "alertstest"

// setupAlertsTest wires the alert suite's zone, store, leader state and the
// evaluation universes, returning an admin context (wildcard scope).
func setupAlertsTest(t *testing.T) context.Context {
	t.Helper()

	prevHostId := config.Config.Hostlet.HostId
	if config.Config.Hostlet.HostId == "" {
		config.Config.Hostlet.HostId = "h-alert-test"
	}

	// Drop host status entries leaked by earlier tests (sync.Map has no
	// Clear); each test seeds its own hosts.
	zstatus.Zonelet_HostStatusSet.Range(func(k, v any) bool {
		zstatus.Zonelet_HostStatusSet.Delete(k)
		return true
	})

	ctx := setupZoneletStoreTest(t, alertsTestZone)

	t.Cleanup(func() {
		config.Config.Hostlet.HostId = prevHostId
		alertsRT.reset()
		gHostSet.Clear()
		gAppInstanceSet.Clear()
		gReplicaMetrics.mu.Lock()
		gReplicaMetrics.items = map[string]*inapi.NodeMetrics{}
		gReplicaMetrics.mu.Unlock()
	})

	return ctx
}

// seedHost registers a host in the evaluation universe with a status report
// aged ageSec seconds and the given disk values.
func seedHost(hostId string, ageSec, diskFree, diskTotal int64, now int64) {
	gHostSet.Store(hostId, nil, &inapi.Host{Id: hostId})
	zstatus.Zonelet_HostStatusSet.Store(hostId, &inapi.HostStatus{
		HostId:         hostId,
		Updated:        now - ageSec,
		DiskFreeBytes:  diskFree,
		DiskTotalBytes: diskTotal,
	})
}

func alertEntryOf(t *testing.T, rule, target string) *alertEntry {
	t.Helper()
	e, ok := alertsRT.entries[rule+"/"+target]
	if !ok {
		return nil
	}
	return e
}

func TestAlertHostDown(t *testing.T) {
	ctx := setupAlertsTest(t)
	const host = "h-down-1"
	t0 := int64(1_700_000_000)

	// Fresh report: no alert.
	seedHost(host, 10, 500, 1000, t0)
	alertsRefresh(t0)
	if e := alertEntryOf(t, inapi.AlertRuleHostDown, host); e != nil {
		t.Fatalf("fresh host created an entry: %+v", e.alert)
	}

	// At the threshold exactly (age == 30): still nothing.
	seedHost(host, 30, 500, 1000, t0)
	alertsRefresh(t0)
	if e := alertEntryOf(t, inapi.AlertRuleHostDown, host); e != nil {
		t.Fatalf("age == threshold fired early: %+v", e.alert)
	}

	// Age 45 > 30: fires on the first breach (streak 1).
	seedHost(host, 45, 500, 1000, t0)
	alertsRefresh(t0)
	e := alertEntryOf(t, inapi.AlertRuleHostDown, host)
	if e == nil || e.alert.State != inapi.AlertStateFiring {
		t.Fatalf("age 45s should fire, got %+v", e)
	}
	if e.alert.StartTime != t0 {
		t.Fatalf("start_time = %d, want %d", e.alert.StartTime, t0)
	}

	// Persisted and readable through the RPC.
	svr := &zoneServer{}
	resp, err := svr.AlertList(ctx, &inapi.AlertListRequest{
		State: inapi.AlertStateFiring,
		Rule:  inapi.AlertRuleHostDown,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Items) != 1 || resp.Items[0].Target != host {
		t.Fatalf("AlertList = %+v", resp.Items)
	}

	// One fresh evaluation is not enough to resolve (streak 2).
	t1 := t0 + 60
	seedHost(host, 1, 500, 1000, t1)
	alertsRefresh(t1)
	if e := alertEntryOf(t, inapi.AlertRuleHostDown, host); e == nil ||
		e.alert.State != inapi.AlertStateFiring {
		t.Fatalf("single fresh eval resolved the alert: %+v", e)
	}

	// Second consecutive fresh evaluation resolves.
	alertsRefresh(t1 + 5)
	if e := alertEntryOf(t, inapi.AlertRuleHostDown, host); e != nil {
		t.Fatalf("alert should be resolved and dropped, got %+v", e.alert)
	}

	// The resolved record is persisted with the cleared message.
	resp, err = svr.AlertList(ctx, &inapi.AlertListRequest{
		State: inapi.AlertStateResolved,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Items) != 1 || resp.Items[0].Target != host {
		t.Fatalf("resolved record missing: %+v", resp.Items)
	}
}

func TestAlertDiskFullStreak(t *testing.T) {
	setupAlertsTest(t)
	const host = "h-disk-1"
	now := int64(1_700_000_000)

	// Single breach then recovery: no record at all (fire streak 2).
	seedHost(host, 1, 50, 1000, now) // 5% free
	alertsRefresh(now)
	if e := alertEntryOf(t, inapi.AlertRuleDiskFull, host); e == nil {
		// streak candidate exists below the fire threshold
		t.Fatalf("breach should start a streak candidate")
	}
	seedHost(host, 1, 500, 1000, now) // 50% free
	alertsRefresh(now + 5)
	if e := alertEntryOf(t, inapi.AlertRuleDiskFull, host); e != nil {
		t.Fatalf("single breach should not persist an alert: %+v", e.alert)
	}

	// Two consecutive breaches fire.
	seedHost(host, 1, 50, 1000, now)
	alertsRefresh(now + 10)
	alertsRefresh(now + 15)
	e := alertEntryOf(t, inapi.AlertRuleDiskFull, host)
	if e == nil || e.alert.State != inapi.AlertStateFiring {
		t.Fatalf("two breaches should fire, got %+v", e)
	}

	// Exactly at the threshold (free*100 == pct*total) is not a breach.
	seedHost(host, 1, 100, 1000, now)
	alertsRefresh(now + 20)
	if e := alertEntryOf(t, inapi.AlertRuleDiskFull, host); e != nil {
		t.Fatalf("threshold value should resolve immediately: %+v", e.alert)
	}
}

func TestAlertHostRemoved(t *testing.T) {
	setupAlertsTest(t)
	const host = "h-removed-1"
	now := int64(1_700_000_000)

	seedHost(host, 60, 500, 1000, now)
	alertsRefresh(now)
	if e := alertEntryOf(t, inapi.AlertRuleHostDown, host); e == nil ||
		e.alert.State != inapi.AlertStateFiring {
		t.Fatalf("stale host should fire, got %+v", e)
	}

	// The host leaves the registered set (removed from the zone).
	gHostSet.Clear()
	seedHost("h-other-1", 1, 500, 1000, now)
	alertsRefresh(now + 5)

	if e := alertEntryOf(t, inapi.AlertRuleHostDown, host); e != nil {
		t.Fatalf("removed host should resolve, got %+v", e.alert)
	}
}

func TestAlertStandbyNoEval(t *testing.T) {
	setupAlertsTest(t)
	const host = "h-standby-1"
	now := int64(1_700_000_000)

	seedHost(host, 60, 500, 1000, now)
	alertsRefresh(now) // fires on the leader

	// Node loses leadership: state resets, no further evaluation.
	zstatus.ZoneletLeader = "h-someone-else"
	alertsRefresh(now + 5)
	if alertsRT.loaded {
		t.Fatalf("standby should reset the loaded state")
	}
	if e := alertEntryOf(t, inapi.AlertRuleHostDown, host); e != nil {
		t.Fatalf("standby reset should clear entries, got %+v", e.alert)
	}
}

func TestAlertKvStatusFallback(t *testing.T) {
	setupAlertsTest(t)
	const host = "h-fallback-1"
	now := int64(1_700_000_000)

	// The host reported to a previous leader: only the store has the record.
	gHostSet.Store(host, nil, &inapi.Host{Id: host})
	prev := &inapi.HostStatus{HostId: host, Updated: now - 60}
	if rs := data.Zonelet.NewWriter(
		inapi.NsHostStatus(alertsTestZone, host), prev).Exec(); !rs.OK() {
		t.Fatalf("seed host status: %s", rs.ErrorMessage())
	}

	alertsRefresh(now)

	e := alertEntryOf(t, inapi.AlertRuleHostDown, host)
	if e == nil || e.alert.State != inapi.AlertStateFiring {
		t.Fatalf("kvgo fallback should make the stale host fire, got %+v", e)
	}
}

func TestAlertListFiltersAndPaging(t *testing.T) {
	ctx := setupAlertsTest(t)
	now := int64(1_700_000_000)

	// Two firing host_down + one firing disk_full (needs its 2-eval streak).
	seedHost("h-page-1", 60, 500, 1000, now)
	seedHost("h-page-2", 60, 500, 1000, now)
	seedHost("h-page-3", 1, 50, 1000, now)
	alertsRefresh(now)
	alertsRefresh(now + 5)

	svr := &zoneServer{}

	resp, err := svr.AlertList(ctx, &inapi.AlertListRequest{
		State: inapi.AlertStateFiring,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Items) != 3 {
		t.Fatalf("firing items = %d, want 3", len(resp.Items))
	}

	// Rule filter.
	resp, err = svr.AlertList(ctx, &inapi.AlertListRequest{
		Rule: inapi.AlertRuleDiskFull,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Items) != 1 || resp.Items[0].Target != "h-page-3" {
		t.Fatalf("rule filter items = %+v", resp.Items)
	}

	// Limit + has_more + offset cursor.
	resp, err = svr.AlertList(ctx, &inapi.AlertListRequest{PageSize: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Items) != 2 || resp.NextPageToken == "" {
		t.Fatalf("page size 2 should have more, items %d next=%q",
			len(resp.Items), resp.NextPageToken)
	}
	cursor := resp.NextPageToken

	resp2, err := svr.AlertList(ctx, &inapi.AlertListRequest{PageToken: cursor})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp2.Items) != 1 || resp2.NextPageToken != "" {
		t.Fatalf("page token should return the tail, items %d next=%q",
			len(resp2.Items), resp2.NextPageToken)
	}

	// Invalid state filter.
	if _, err := svr.AlertList(ctx, &inapi.AlertListRequest{
		State: "bogus",
	}); err == nil {
		t.Fatalf("invalid state filter should error")
	}
}

func TestAlertWebhook(t *testing.T) {
	setupAlertsTest(t)

	// Capture webhook deliveries; the notify path is async.
	events := make(chan map[string]any, 4)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var ev map[string]any
		if err := json.NewDecoder(r.Body).Decode(&ev); err != nil {
			t.Errorf("webhook body: %v", err)
		}
		events <- ev
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	prevURL := config.Config.Alerts.WebhookURL
	config.Config.Alerts.WebhookURL = srv.URL
	t.Cleanup(func() { config.Config.Alerts.WebhookURL = prevURL })

	const host = "h-hook-1"
	now := int64(1_700_000_000)
	seedHost(host, 60, 500, 1000, now)
	alertsRefresh(now) // fires

	waitEvent := func(wantState string) {
		select {
		case ev := <-events:
			if ev["state"] != wantState || ev["target"] != host {
				t.Fatalf("webhook event = %v, want state %s target %s",
					ev, wantState, host)
			}
		case <-time.After(3 * time.Second):
			t.Fatalf("webhook %s event not delivered", wantState)
		}
	}

	waitEvent(inapi.AlertStateFiring)

	seedHost(host, 1, 500, 1000, now)
	alertsRefresh(now + 5)
	alertsRefresh(now + 10) // resolves (streak 2)

	waitEvent(inapi.AlertStateResolved)
}

// seedInstance registers an app instance in the evaluation universe.
func seedInstance(name string) {
	gAppInstanceSet.Store(&inapi.AppInstance{
		Meta:   &inapi.Metadata{Name: name},
		Spec:   &inapi.AppSpec{Resources: &inapi.AppSpecResources{}},
		Deploy: &inapi.AppDeploy{},
	}, &kvapi.Meta{Version: 1})
}

// reportRestart seeds the replica metrics cache with a cumulative restart
// counter, mimicking the hostlet report path.
func reportRestart(instance string, repId uint32, count int32) {
	gReplicaMetrics.store(inapi.ContainerName(instance, repId),
		&inapi.NodeMetrics{Updated: time.Now().Unix(), RestartCount: count})
}

func TestAlertCrashloop(t *testing.T) {
	setupAlertsTest(t)
	const instance = "myapp"
	const container = "i8k_myapp_0"
	t0 := int64(1_700_000_000)

	seedInstance(instance)

	// First leader pass loads the state and enables the trackers.
	seedHost("h-crash-1", 1, 500, 1000, t0)
	alertsRefresh(t0)

	// First sighting seeds the baseline; no events yet.
	reportRestart(instance, 0, 0)
	alertsRefresh(t0 + 5)
	if e := alertEntryOf(t, inapi.AlertRuleReplicaCrashloop, container); e != nil {
		t.Fatalf("baseline should not create an entry: %+v", e.alert)
	}

	// Two restarts: below the threshold of 3.
	reportRestart(instance, 0, 1)
	reportRestart(instance, 0, 2)
	alertsRefresh(t0 + 25)
	if e := alertEntryOf(t, inapi.AlertRuleReplicaCrashloop, container); e != nil {
		t.Fatalf("2 restarts should not fire: %+v", e.alert)
	}

	// Third restart within the window fires.
	reportRestart(instance, 0, 3)
	alertsRefresh(t0 + 35)
	e := alertEntryOf(t, inapi.AlertRuleReplicaCrashloop, container)
	if e == nil || e.alert.State != inapi.AlertStateFiring {
		t.Fatalf("3 restarts in window should fire, got %+v", e)
	}
	if got := e.alert.Message; got != "3 restarts within 600s" {
		t.Fatalf("message = %q", got)
	}

	// Events age out of the window: the alert resolves.
	alertsRefresh(t0 + 35 + 601)
	if e := alertEntryOf(t, inapi.AlertRuleReplicaCrashloop, container); e != nil {
		t.Fatalf("aged-out events should resolve, got %+v", e.alert)
	}
}

func TestAlertCrashloopCounterReset(t *testing.T) {
	setupAlertsTest(t)
	const instance = "myapp2"
	const container = "i8k_myapp2_0"
	t0 := int64(1_700_000_000)

	seedInstance(instance)
	seedHost("h-crash-2", 1, 500, 1000, t0)
	alertsRefresh(t0)

	reportRestart(instance, 0, 0)
	alertsRefresh(t0 + 5)
	reportRestart(instance, 0, 3)
	alertsRefresh(t0 + 15)
	if e := alertEntryOf(t, inapi.AlertRuleReplicaCrashloop, container); e == nil ||
		e.alert.State != inapi.AlertStateFiring {
		t.Fatalf("a delta of 3 across evals should fire, got %+v", e)
	}

	// Counter reset (container recreated): the window restarts and the alert
	// resolves on the next evaluation.
	reportRestart(instance, 0, 0)
	alertsRefresh(t0 + 25)
	if e := alertEntryOf(t, inapi.AlertRuleReplicaCrashloop, container); e != nil {
		t.Fatalf("counter reset should resolve, got %+v", e.alert)
	}
}

func TestAlertCrashloopInstanceRemoved(t *testing.T) {
	setupAlertsTest(t)
	const instance = "myapp3"
	const container = "i8k_myapp3_0"
	t0 := int64(1_700_000_000)

	seedInstance(instance)
	seedHost("h-crash-3", 1, 500, 1000, t0)
	alertsRefresh(t0)

	reportRestart(instance, 0, 0)
	alertsRefresh(t0 + 5)
	reportRestart(instance, 0, 3)
	alertsRefresh(t0 + 15)
	if e := alertEntryOf(t, inapi.AlertRuleReplicaCrashloop, container); e == nil {
		t.Fatalf("crashloop should fire")
	}

	// The instance is deleted; the tracker sweep resolves the alert.
	gAppInstanceSet.Clear()
	alertsRefresh(t0 + 20)

	if e := alertEntryOf(t, inapi.AlertRuleReplicaCrashloop, container); e != nil {
		t.Fatalf("removed instance should resolve, got %+v", e.alert)
	}
	if _, tracked := alertsRT.restarts[container]; tracked {
		t.Fatalf("tracker should be swept")
	}
}

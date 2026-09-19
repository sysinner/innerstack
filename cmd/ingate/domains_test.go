package main

import (
	"testing"

	"github.com/sysinner/innerstack/v2/pkg/inapi"
)

// TestDomainFreshRefreshesDomainProto pins the fix for the stale-Domain bug:
// domainFresh must swap the retained *GatewayIngressDeploy, not just rebuild
// the routes. Without the swap, flipping LetsencryptEnable rebuilds routing
// and the TLS whitelist but leaves the port-80 ACME split decision on the
// old proto until process restart.
func TestDomainFreshRefreshesDomainProto(t *testing.T) {
	routes := func() []*inapi.GatewayIngressDeploy_HttpRoute {
		return []*inapi.GatewayIngressDeploy_HttpRoute{{
			Type: inapi.GatewayIngressType_Instance,
			Path: "/",
			Targets: []*inapi.GatewayIngressDeploy_HttpRoute_Target{
				{Backend: "10.0.0.1:8080"},
			},
		}}
	}

	entry := &DomainEntry{
		Domain: &inapi.GatewayIngressDeploy{
			Domain:   "example.com",
			Revision: 1,
			Routes:   routes(),
		},
		indexRoutes: map[string]*DomainEntryRoute{},
	}
	domainFresh(entry, entry.Domain)

	if entry.LetsencryptEnabled() {
		t.Fatal("LetsencryptEnabled = true, want false on the initial proto")
	}
	if entry.setupRevision != 1 {
		t.Fatalf("setupRevision = %d, want 1", entry.setupRevision)
	}
	if entry.lookup("/") == nil {
		t.Fatal("route / missing after initial fresh")
	}

	updated := &inapi.GatewayIngressDeploy{
		Domain:            "example.com",
		Revision:          2,
		LetsencryptEnable: true,
		Routes:            routes(),
	}
	domainFresh(entry, updated)

	if entry.Domain != updated {
		t.Fatal("domainFresh retained the stale domain proto")
	}
	if !entry.LetsencryptEnabled() {
		t.Fatal("LetsencryptEnabled = false after refresh, want true")
	}
	if entry.setupRevision != 2 {
		t.Fatalf("setupRevision = %d, want 2", entry.setupRevision)
	}
	if entry.lookup("/") == nil {
		t.Fatal("route / missing after refresh")
	}
}

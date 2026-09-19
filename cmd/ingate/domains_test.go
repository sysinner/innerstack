package main

import (
	"sync"
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

// TestConfigDomainReadsSynchronizedWithIndexWrites pins Domain()'s locking
// contract: indexDomains is swapped by configRefresh under cfg.mu, so a
// concurrent Domain() must hold at least the read lock. Under -race this
// catches a future regression that drops the lock (concurrent map access).
func TestConfigDomainReadsSynchronizedWithIndexWrites(t *testing.T) {
	entry := &DomainEntry{
		Domain:      &inapi.GatewayIngressDeploy{Domain: "example.com"},
		indexRoutes: map[string]*DomainEntryRoute{},
	}

	oldIndex := cfg.indexDomains
	cfg.indexDomains = map[string]*DomainEntry{"example.com": entry}
	t.Cleanup(func() { cfg.indexDomains = oldIndex })

	var wg sync.WaitGroup
	stop := make(chan struct{})

	wg.Add(1)
	go func() { // writer mimics configRefresh's index swap under cfg.mu
		defer wg.Done()
		defer close(stop)
		for i := 0; i < 500; i++ {
			cfg.mu.Lock()
			cfg.indexDomains["example.com"] = entry
			cfg.mu.Unlock()
		}
	}()

	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					if cfg.Domain("example.com") == nil {
						t.Error("Domain(example.com) = nil, want the entry")
						return
					}
				}
			}
		}()
	}

	wg.Wait()
}

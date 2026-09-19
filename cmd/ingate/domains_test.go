package main

import (
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/hooto/htoml4g/htoml"
	"github.com/sysinner/innerstack/v2/pkg/inapi"
)

// TestDomainFreshRefreshesDomainProto pins the fix for the stale-Domain bug:
// domainFresh must swap the retained *GatewayIngressDeploy, not just rebuild
// the routes. Without the swap, flipping LetsencryptEnable rebuilds routing
// and the TLS whitelist but leaves the port-80 ACME split decision on the
// old proto until process restart.
func TestDomainFreshRefreshesDomainProto(t *testing.T) {
	routes := func() []*inapi.GatewayIngressDeploy_HttpRoute {
		return []*inapi.GatewayIngressDeploy_HttpRoute{instanceRoute("/")}
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

// setAndRestore swaps *p to v for the duration of the test and restores the
// old value afterwards.
func setAndRestore[T any](t *testing.T, p *T, v T) {
	t.Helper()
	old := *p
	*p = v
	t.Cleanup(func() { *p = old })
}

// backupConfig swaps the package-level state configRefresh mutates (and the
// prefix it persists under) for the duration of a test.
func backupConfig(t *testing.T) {
	t.Helper()

	setAndRestore(t, &prefix, t.TempDir())
	setAndRestore(t, &cfg.Domains, nil)
	setAndRestore(t, &cfg.indexDomains, map[string]*DomainEntry{})
	setAndRestore(t, &cfg.lastRevision, uint64(0))
	setAndRestore(t, &cfg.lastFullUpdated, int64(0))

	if err := os.MkdirAll(filepath.Join(prefix, "etc"), 0o755); err != nil {
		t.Fatal(err)
	}
}

func instanceRoute(path string) *inapi.GatewayIngressDeploy_HttpRoute {
	return &inapi.GatewayIngressDeploy_HttpRoute{
		Type: inapi.GatewayIngressType_Instance,
		Path: path,
		Targets: []*inapi.GatewayIngressDeploy_HttpRoute_Target{
			{Backend: "10.0.0.1:8080"},
		},
	}
}

func localfsRoute(path, backend string) *inapi.GatewayIngressDeploy_HttpRoute {
	return &inapi.GatewayIngressDeploy_HttpRoute{
		Type: gatewayIngressTypeLocalfs,
		Path: path,
		Targets: []*inapi.GatewayIngressDeploy_HttpRoute_Target{
			{Backend: backend},
		},
	}
}

func localfsRouteCount(routes []*inapi.GatewayIngressDeploy_HttpRoute) int {
	n := 0
	for _, route := range routes {
		if route.Type == gatewayIngressTypeLocalfs {
			n++
		}
	}
	return n
}

// TestConfigRefreshPersistsMergedLocalfsRoutes pins the disappearing-localfs
// fix: a zonelet push never carries localfs routes, and domainFresh retains
// them only in the runtime index. Before the fix, cfg.Domains held the raw
// pushed protos, so the next flush wrote etc/ingate.toml without the
// operator's localfs stanza -- silently while serving continued from the
// retained route, fatally after the next restart.
func TestConfigRefreshPersistsMergedLocalfsRoutes(t *testing.T) {
	backupConfig(t)

	root := t.TempDir()

	// Operator TOML view: localfs /res + instance /.
	tomlDomain := &inapi.GatewayIngressDeploy{
		Domain:   "example.com",
		Revision: 1,
		Routes: []*inapi.GatewayIngressDeploy_HttpRoute{
			localfsRoute("/res", root),
			instanceRoute("/"),
		},
	}
	if err := configRefresh([]*inapi.GatewayIngressDeploy{tomlDomain}); err != nil {
		t.Fatal(err)
	}

	// Zonelet push at a higher revision: same domain, no localfs route.
	pushed := &inapi.GatewayIngressDeploy{
		Domain:   "example.com",
		Revision: 2,
		Routes: []*inapi.GatewayIngressDeploy_HttpRoute{
			instanceRoute("/"),
		},
	}
	if err := configRefresh([]*inapi.GatewayIngressDeploy{pushed}); err != nil {
		t.Fatal(err)
	}

	// The persisted view must still carry the localfs route.
	if len(cfg.Domains) != 1 {
		t.Fatalf("cfg.Domains = %d domains, want 1", len(cfg.Domains))
	}
	var localfs *inapi.GatewayIngressDeploy_HttpRoute
	for _, route := range cfg.Domains[0].Routes {
		if route.Type == gatewayIngressTypeLocalfs {
			localfs = route
		}
	}
	if localfs == nil {
		t.Fatal("localfs route missing from cfg.Domains after push refresh")
	}
	if len(localfs.Targets) != 1 || localfs.Targets[0].Backend != root {
		t.Fatalf("localfs targets = %v, want [%s]", localfs.Targets, root)
	}

	// And it must survive the TOML round-trip on disk.
	var onDisk Config
	if err := htoml.DecodeFromFile(
		filepath.Join(prefix, "etc", appName+".toml"),
		&onDisk,
	); err != nil {
		t.Fatal(err)
	}
	if localfsRouteCount(onDisk.Domains[0].Routes) != 1 {
		t.Fatal("localfs route missing from persisted ingate.toml")
	}

	// The runtime index still serves /res from the retained route.
	entry := cfg.Domain("example.com")
	if entry == nil {
		t.Fatal("domain entry missing after push refresh")
	}
	if route := entry.lookup("/res/deb/dists/trixie/Release"); route == nil ||
		route.Type != gatewayIngressTypeLocalfs {
		t.Fatalf("lookup(/res/...) = %+v, want the retained localfs route", route)
	}

	// Idempotence: a second push at the same revision must not duplicate it.
	if err := configRefresh([]*inapi.GatewayIngressDeploy{pushed}); err != nil {
		t.Fatal(err)
	}
	if n := localfsRouteCount(cfg.Domains[0].Routes); n != 1 {
		t.Fatalf("localfs route count = %d after repeat push, want 1", n)
	}
}

// TestConfigRefreshKeepsTomlOwnedDomain covers a domain defined only in the
// operator's TOML (localfs-bearing, never pushed by the zonelet): a full
// refresh must not evict it from the index or the persisted config, while a
// formerly pushed domain absent from the list is still deleted (CLI removal).
func TestConfigRefreshKeepsTomlOwnedDomain(t *testing.T) {
	backupConfig(t)

	statics := &inapi.GatewayIngressDeploy{
		Domain:   "statics.example.com",
		Revision: 1,
		Routes: []*inapi.GatewayIngressDeploy_HttpRoute{
			localfsRoute("/", t.TempDir()),
		},
	}
	if err := configRefresh([]*inapi.GatewayIngressDeploy{statics}); err != nil {
		t.Fatal(err)
	}

	// Full refresh carrying an unrelated pushed domain.
	pushed := &inapi.GatewayIngressDeploy{
		Domain:   "app.example.com",
		Revision: 5,
		Routes: []*inapi.GatewayIngressDeploy_HttpRoute{
			instanceRoute("/"),
		},
	}
	if err := configRefresh([]*inapi.GatewayIngressDeploy{pushed}); err != nil {
		t.Fatal(err)
	}

	if cfg.Domain("statics.example.com") == nil {
		t.Fatal("TOML-owned domain evicted from index by full refresh")
	}
	if cfg.Domain("app.example.com") == nil {
		t.Fatal("pushed domain missing from index")
	}

	// The pushed domain disappears from the list -> deleted (CLI removal);
	// the TOML-owned one survives and stays persisted.
	if err := configRefresh(nil); err != nil {
		t.Fatal(err)
	}
	if cfg.Domain("statics.example.com") == nil {
		t.Fatal("TOML-owned domain evicted when push list became empty")
	}
	if cfg.Domain("app.example.com") != nil {
		t.Fatal("formerly pushed domain survived deletion")
	}
	if len(cfg.Domains) != 1 || cfg.Domains[0].Domain != "statics.example.com" {
		t.Fatalf("cfg.Domains = %v, want [statics.example.com]", cfg.Domains)
	}
}

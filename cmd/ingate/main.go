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

package main

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/tls"
	_ "embed"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"net/http/pprof"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/hooto/hmetrics"
	"github.com/hooto/htoml4g/htoml"
	"github.com/lynkdb/lynkapi/go/lynkapi"
	"golang.org/x/crypto/acme/autocert"
	"golang.org/x/time/rate"

	"github.com/sysinner/innerstack/v2/internal/client"
	"github.com/sysinner/innerstack/v2/internal/inutil/tplrender"
	"github.com/sysinner/innerstack/v2/pkg/inapi"
	"github.com/sysinner/innerstack/v2/pkg/inauth"
	"github.com/sysinner/innerstack/v2/pkg/inlog"
	"github.com/sysinner/innerstack/v2/pkg/signals"
)

//go:embed builtin/403.html
var builtin_403_HTML []byte

//go:embed builtin/404.html
var builtin_404_HTML []byte

//go:embed module/domain-sale.html
var module_DomainSale_HTML string

func init() {
	inlog.Setup()
}

func main() {

	mux := http.NewServeMux()
	mux.HandleFunc("/", rootHandler)
	mux.HandleFunc("/+/metrics", hmetrics.HttpHandler)

	os.MkdirAll(tlsCacheDir, 0750)

	certManager = autocert.Manager{
		Prompt:     autocert.AcceptTOS,
		Cache:      autocert.DirCache(tlsCacheDir),
		HostPolicy: autocert.HostWhitelist([]string{}...),
	}

	{
		for {
			if err := initSetup(); err != nil {
				slog.Error("init config fail : " + err.Error())
				time.Sleep(1e9)
			} else {
				break
			}
		}

		if err := configRefresh(cfg.Domains); err != nil {
			slog.Error("domains init fail : " + err.Error())
		} else {
			slog.Info("domains init done", "num", len(cfg.Domains))
		}
	}
	if cfg.Server.DebugPprofEnable {
		mux.HandleFunc("/debug/pprof/", pprof.Index)
		mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
		mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
		mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
		mux.HandleFunc("/debug/pprof/trace", pprof.Trace)

		slog.Info("pprof enabled")
	}
	{
		httpServer = &http.Server{
			Addr:              fmt.Sprintf(":%d", cfg.Server.HttpPort),
			Handler:           httpRootHandler{},
			ReadTimeout:       time.Duration(cfg.Server.ReadTimeout) * time.Second,
			ReadHeaderTimeout: time.Duration(cfg.Server.ReadHeaderTimeout) * time.Second,
			WriteTimeout:      time.Duration(cfg.Server.WriteTimeout) * time.Second,
			IdleTimeout:       time.Duration(cfg.Server.IdleTimeout) * time.Second,
			MaxHeaderBytes:    1 << 20,
		}
		signals.Go(func() {
			slog.Info("http server start " + httpServer.Addr)
			if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				slog.Info("http server quit : " + err.Error())
			}
		}, func() {
			serverShutdown(httpServer, "http")
		})
	}
	if cfg.Server.HttpsPort > 0 {
		httpsServer = &http.Server{
			Addr:    fmt.Sprintf(":%d", cfg.Server.HttpsPort),
			Handler: mux,
			TLSConfig: &tls.Config{
				GetCertificate: certManager.GetCertificate,
			},
			ReadTimeout:       time.Duration(cfg.Server.ReadTimeout) * time.Second,
			ReadHeaderTimeout: time.Duration(cfg.Server.ReadHeaderTimeout) * time.Second,
			WriteTimeout:      time.Duration(cfg.Server.WriteTimeout) * time.Second,
			IdleTimeout:       time.Duration(cfg.Server.IdleTimeout) * time.Second,
			MaxHeaderBytes:    1 << 20,
		}
		signals.Go(func() {
			slog.Info("https server start " + httpsServer.Addr)
			if err := httpsServer.ListenAndServeTLS(
				"",
				"",
			); err != nil &&
				err != http.ErrServerClosed {
				slog.Error("https server quit : " + err.Error())
			}
			tlsDomainCache = nil
		}, func() {
			serverShutdown(httpsServer, "https")
		})
	}

	if cfg.Zone != nil {
		signals.Go(func() {

			ticker := time.NewTicker(time.Second * 10)
			defer ticker.Stop()

			for {
				select {
				case <-signals.Done():
					return

				case <-ticker.C:
					if err := configRefresh(nil); err != nil {
						slog.Error("domains refresh fail : " + err.Error())
					}
				}
			}
		}, nil)
	}

	// IP 限流清理
	signals.Go(ipLimiterCleaner, nil)

	signals.Wait()
}

// serverShutdown drains a server with a bounded wait. Shutdown(context.Background())
// would block forever on an active connection: streaming responses clear the
// per-response write deadline and are drained through the rate limiter, so a
// large throttled transfer can hold one for hours. Past the deadline the
// caller (signals.Wait) proceeds and the process exits, dropping whatever is
// left -- graceful first, forceful second.
func serverShutdown(srv *http.Server, name string) {
	ctx, cancel := context.WithTimeout(context.Background(),
		time.Duration(cfg.Server.ShutdownTimeout)*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		slog.Warn(name + " server shutdown: " + err.Error())
	}
}

type Config struct {
	mu sync.RWMutex

	Server ConfigServer `toml:"server"`

	Limit ConfigLimit `toml:"limit"`

	Zone *ConfigZone `toml:"zone,omitempty"`

	Modules      []*ConfigModule          `toml:"modules"`
	indexModules map[string]*ConfigModule `toml:"-"`

	Domains      []*inapi.GatewayIngressDeploy `toml:"domains"`
	indexDomains map[string]*DomainEntry       `toml:"-"`

	lastRevision    uint64
	lastFullUpdated int64
}

type ConfigServer struct {
	HttpPort  int `toml:"http_port"`
	HttpsPort int `toml:"https_port"`

	// ReadTimeout bounds reading the request (headers + body).
	ReadTimeout int64 `toml:"read_timeout"`
	// ReadHeaderTimeout bounds reading the request headers only; the dedicated
	// slowloris guard. Applied in addition to ReadTimeout.
	ReadHeaderTimeout int64 `toml:"read_header_timeout"`
	// WriteTimeout is the fixed upper bound on writing a response. Streaming
	// and rate-limited responses (localfs downloads, proxied bodies)
	// legitimately outlast it, so rootHandler clears the per-response write
	// deadline for those; WriteByteTimeout is the real stall guard.
	WriteTimeout int64 `toml:"write_timeout"`
	// WriteByteTimeout fires when a write makes no progress for this long --
	// kills stalled/dead clients without punishing slow-but-progressing
	// transfers. The primary write-side timeout for a streaming gateway.
	WriteByteTimeout int64 `toml:"write_byte_timeout"`
	// IdleTimeout is the keep-alive idle timeout between requests.
	IdleTimeout int64 `toml:"idle_timeout"`
	// MaxBodySize bounds the request body (http.MaxBytesReader).
	MaxBodySize int64 `toml:"max_body_size"`
	// MaxRespSize is a hard cap on a single proxied response body: exceeding
	// it aborts the response (connection closed) instead of streaming it
	// indefinitely. Streaming keeps memory bounded regardless; this cap bounds
	// the transfer itself (e.g. a dripping upstream pinning a goroutine).
	// Scope: proxied routes only (localfs serves operator-owned static
	// content and is uncapped); event streams are exempt because a healthy
	// stream legitimately accumulates bytes over its lifetime.
	MaxRespSize int64 `toml:"max_resp_size"`
	// ShutdownTimeout bounds the graceful-drain wait on SIGTERM. Streaming
	// responses legitimately outlast WriteTimeout (rootHandler clears the
	// per-response write deadline), so an unbounded Shutdown would hang the
	// process on a throttled transfer until the supervisor SIGKILLs it;
	// past the deadline the process exits and the remaining connections
	// are dropped.
	ShutdownTimeout int64 `toml:"shutdown_timeout"`

	DebugPprofEnable bool `toml:"debug_pprof_enable,omitempty"`
}

type ConfigZone struct {
	Name  string   `toml:"name"`
	Hosts []string `toml:"hosts"      json:"hosts"`
	AK    string   `toml:"access_key"`
}

type ConfigModule struct {
	Module  string            `toml:"module"`
	Domains []string          `toml:"domains"`
	Options map[string]string `toml:"options,omitempty"`

	handler ModuleHandler
}

type ConfigLimit struct {
	Rate           int64 `toml:"rate"`            // 每秒限制字节数 (如 102400 表示 100KB/s)
	Burst          int64 `toml:"burst"`           // 允许的突发字节数
	IpExpireAfter  int64 `toml:"ip_expire_after"` // IP 钝化过期时间 (秒)
	CleanupSeconds int64 `toml:"cleanup_seconds"` // 清理检查频率 (秒)
}

// AccessKey parses the AK string (ak_{id}_{secret}) into an AccessKey
func (c *ConfigZone) AccessKey() (*inauth.AccessKey, error) {
	if c.AK == "" {
		return nil, errors.New("access_key not set")
	}
	return inauth.ParseAccessKey(c.AK)
}

func (it *Config) Domain(name string) *DomainEntry {
	it.mu.RLock()
	defer it.mu.RUnlock()
	domain, ok := it.indexDomains[name]
	if ok {
		return domain
	}
	return nil
}

func (it *Config) Module(domain string) *ConfigModule {
	it.mu.RLock()
	defer it.mu.RUnlock()
	m, ok := it.indexModules[domain]
	if ok {
		return m
	}
	return nil
}

type DomainEntry struct {
	Domain *inapi.GatewayIngressDeploy `json:"domain"`
	Routes []*DomainEntryRoute         `json:"routes"`

	mu          sync.RWMutex
	indexRoutes map[string]*DomainEntryRoute

	setupRevision uint64
}

// LetsencryptEnabled reports whether the domain's current config requests a
// Let's Encrypt certificate. The Domain proto is swapped by domainFresh, so
// the read must hold the entry lock.
func (it *DomainEntry) LetsencryptEnabled() bool {
	it.mu.RLock()
	defer it.mu.RUnlock()
	return it.Domain != nil && it.Domain.LetsencryptEnable
}

type DomainEntryRoute struct {
	Type string     `json:"type"`
	Path string     `json:"path"`
	Urls []*url.URL `json:"urls"`

	callCount    uint64
	reverseProxy []*httputil.ReverseProxy
}

func (it *DomainEntry) lookup(urlPath string) *DomainEntryRoute {
	it.mu.RLock()
	defer it.mu.RUnlock()

	tempPath := urlPath
	for {
		if route, ok := it.indexRoutes[tempPath]; ok {
			return route
		}
		if tempPath == "/" || tempPath == "." || tempPath == "" {
			break
		}
		parent := path.Dir(tempPath)
		if parent == tempPath {
			break
		}
		tempPath = parent
	}

	if len(it.Routes) > 0 {
		return it.Routes[len(it.Routes)-1]
	}

	return nil
}

var (
	appName = "ingate"

	prefix = "/opt/innerstack"

	tlsCacheDir = prefix + "/var/" + appName + "_tls_cache"

	tlsDomainCache = []string{}

	httpServer  *http.Server
	httpsServer *http.Server

	certManager autocert.Manager

	version = "v2.0.0-alpha.5.2"

	cfg Config

	zoneConn *client.ClientConn
)

var (
	metricCounter = hmetrics.RegisterCounterMap(
		"counter",
		"The General Counter Metric",
	)

	metricGauge = hmetrics.RegisterGaugeMap(
		"gauge",
		"The General Gauge Metric",
	)

	metricLatency = hmetrics.RegisterHistogramMap(
		"latency",
		"The General Latency Metric",
		hmetrics.NewBuckets(0.0001, 1.5, 36),
	)

	metricHistogram = hmetrics.RegisterHistogramMap(
		"histogram",
		"The General Histogram Metric",
		hmetrics.NewBuckets(0.0001, 1.5, 36),
	)

	metricComplex = hmetrics.RegisterComplexMap(
		"complex",
		"The General Complex Metric",
		hmetrics.NewBuckets(0.0001, 1.5, 36),
	)
)

// confClampInt64 applies the ingate config value policy: a non-positive
// value means "use the default", otherwise the value is clamped into [lo, hi].
func confClampInt64(v, def, lo, hi int64) int64 {
	if v <= 0 {
		return def
	}
	return min(max(v, lo), hi)
}

func initSetup() error {

	prefixes := []string{prefix}
	if v, err := filepath.Abs(filepath.Dir(os.Args[0])); err == nil && v != prefix {
		v = strings.TrimSuffix(v, "/bin")
		prefixes = append(prefixes, v)
	}

	var err error

	for _, p := range prefixes {
		if err = htoml.DecodeFromFile(p+"/etc/"+appName+".toml", &cfg); err == nil {
			prefix = p
			tlsCacheDir = prefix + "/var/" + appName + "_tls_cache"
			break
		}
	}
	if err != nil {
		return err
	}

	if cfg.Server.HttpPort == 0 {
		// required
		cfg.Server.HttpPort = 80

		if cfg.Server.HttpsPort == 0 {
			// optional
			cfg.Server.HttpsPort = 443
		}
	}

	// server.size/time policy: non-positive means "use the default",
	// otherwise clamp into [lo, hi]
	cfg.Server.MaxBodySize = confClampInt64(cfg.Server.MaxBodySize, 16<<20, 8<<20, 64<<20)
	cfg.Server.MaxRespSize = confClampInt64(cfg.Server.MaxRespSize, 1<<30, 16<<20, 8<<30)
	cfg.Server.ReadTimeout = confClampInt64(cfg.Server.ReadTimeout, 61, 3, 300)
	cfg.Server.ReadHeaderTimeout = confClampInt64(cfg.Server.ReadHeaderTimeout, 10, 3, 60)
	cfg.Server.WriteTimeout = confClampInt64(cfg.Server.WriteTimeout, 61, 3, 300)
	cfg.Server.WriteByteTimeout = confClampInt64(cfg.Server.WriteByteTimeout, 60, 5, 600)
	cfg.Server.IdleTimeout = confClampInt64(cfg.Server.IdleTimeout, 120, 10, 600)
	// Upper bound overlaps systemd's default TimeoutStopSec (90s): two
	// servers draining sequentially at the cap must still fit within it.
	cfg.Server.ShutdownTimeout = confClampInt64(cfg.Server.ShutdownTimeout, 30, 1, 300)

	{
		if cfg.Limit.Rate <= 0 {
			cfg.Limit.Rate = 100 * 1024 // 默认 100KB/s
		}
		if cfg.Limit.Burst <= 0 {
			cfg.Limit.Burst = 100 * 1024
		}
		if cfg.Limit.IpExpireAfter <= 0 {
			cfg.Limit.IpExpireAfter = 60 // 默认 1 分钟
		}
		if cfg.Limit.CleanupSeconds <= 0 {
			cfg.Limit.CleanupSeconds = 30 // 默认 30 秒检查一次
		}
	}

	cfg.indexDomains = map[string]*DomainEntry{}

	cfg.indexModules = map[string]*ConfigModule{}
	for _, module := range cfg.Modules {
		switch module.Module {
		case "DomainSale":
			if len(module.Options) > 0 && module.Options["contact_email"] != "" {
				module.handler = module_DomainSale_Handler
				for _, d := range module.Domains {
					cfg.indexModules[strings.ToLower(d)] = module
					slog.Info(fmt.Sprintf("module %s domain %s", module.Module, d))
				}
			}
		}
	}

	if zoneConn == nil && cfg.Zone != nil &&
		len(cfg.Zone.Hosts) > 0 && cfg.Zone.AK != "" {
		ak, err := cfg.Zone.AccessKey()
		if err != nil {
			return fmt.Errorf("invalid access key: %w", err)
		}

		conn, err := client.Connect(cfg.Zone.Hosts[0], ak, false)
		if err != nil {
			return fmt.Errorf("failed to connect to zone leader %s: %w",
				cfg.Zone.Hosts[0], err)
		}
		// defer conn.Close()
		zoneConn = conn
		slog.Warn("zone connect init ak-id " + ak.Id)
	}

	return nil
}

// domainFresh rebuilds a DomainEntry's routing state from the incoming
// domain config: routes, index, setup revision, and the retained domain
// proto itself. Swapping the proto keeps per-domain flags (LetsencryptEnable)
// current for readers like the port-80 ACME split; rebuilding routes alone
// would leave the old decision in place until process restart. Called from
// configRefresh under cfg.mu; the entry lock guards the fields against
// request-time readers.
func domainFresh(domainEntry *DomainEntry, domain *inapi.GatewayIngressDeploy) {
	domainEntry.mu.Lock()
	defer domainEntry.mu.Unlock()

	// Expired routes need no explicit transport teardown: every proxy shares
	// the package-level reverseProxyTransport, so there is nothing per-route
	// to close, and closing the shared transport would drop healthy idle
	// connections too. Connections pooled for vanished backends expire via
	// the transport's IdleConnTimeout.
	prevRoutes := domainEntry.Routes

	domainEntry.Routes = nil
	domainEntry.indexRoutes = map[string]*DomainEntryRoute{}
	domainEntry.setupRevision = domain.Revision
	domainEntry.Domain = domain

	for _, route := range prevRoutes {
		switch route.Type {
		case "localfs":

			if p := lynkapi.SlicesSearchFunc(domain.Routes,
				func(a *inapi.GatewayIngressDeploy_HttpRoute) bool {
					return a.Path == route.Path
				}); p == nil {
				domainEntry.Routes = append(domainEntry.Routes, route)
				domainEntry.indexRoutes[route.Path] = route
			}
		}
	}

	for _, location := range domain.Routes {

		if len(location.Targets) == 0 {
			continue
		}

		switch location.Type {
		case inapi.GatewayIngressType_Instance,
			inapi.GatewayIngressType_Upstream:
			var (
				urls []*url.URL
				rps  []*httputil.ReverseProxy
			)
			for _, tg := range location.Targets {
				u := &url.URL{
					Scheme: "http",
					Host:   tg.Backend,
				}
				urls = append(urls, u)
				rps = append(rps, newReverseProxy(u))
			}
			if len(urls) > 0 {
				route := &DomainEntryRoute{
					Path:         location.Path,
					Type:         location.Type,
					Urls:         urls,
					reverseProxy: rps,
				}
				domainEntry.Routes = append(domainEntry.Routes, route)
				domainEntry.indexRoutes[route.Path] = route
			}

		case inapi.GatewayIngressType_Redirect:

			if u, err := url.Parse(location.Targets[0].Backend); err == nil {

				route := &DomainEntryRoute{
					Path: location.Path,
					Type: location.Type,
					Urls: []*url.URL{u},
				}
				domainEntry.Routes = append(domainEntry.Routes, route)
				domainEntry.indexRoutes[route.Path] = route
			} else {
				slog.Warn("parse backend fail", "err", err.Error())
			}

		case "localfs":

			route := &DomainEntryRoute{
				Path: location.Path,
				Type: location.Type,
			}

			if len(location.Targets) == 1 && len(location.Targets[0].Backend) > 1 {
				localPath := filepath.Clean(location.Targets[0].Backend)
				if st, err := os.Stat(localPath); err == nil && st.IsDir() {
					route.Urls = []*url.URL{{Path: localPath}}
				}
				slog.Info(fmt.Sprintf("domain %s, route %s, localfs %s",
					domain.Domain, route.Path, localPath))
			}

			domainEntry.Routes = append(domainEntry.Routes, route)
			domainEntry.indexRoutes[route.Path] = route
		}
	}

	slog.Info(fmt.Sprintf("updated domain %s, routes %d",
		domain.Domain, len(domain.Routes)))
}

func configRefresh(domains []*inapi.GatewayIngressDeploy) error {

	tn := time.Now().Unix()
	req := &inapi.GatewayIngressDeployListRequest{}

	if len(domains) == 0 && zoneConn != nil {

		if cfg.lastFullUpdated+600 < tn {
			req.Revision = 0
			cfg.lastFullUpdated = tn
		} else {
			req.Revision = cfg.lastRevision
		}

		zc := inapi.NewZoneInternalServiceClient(zoneConn)

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		rspList, err := zc.GatewayIngressDeployList(ctx, req)
		if err != nil {
			return err
		}

		if len(rspList.Items) == 0 {
			return nil
		}

		if req.Revision == 0 && len(cfg.Domains) > 0 {
			r := float64(len(rspList.Items)) / float64(len(cfg.Domains))
			if r < 0.5 {
				slog.Info(
					fmt.Sprintf("fetch domains %d/%d, skip", len(rspList.Items), len(cfg.Domains)),
				)
				return nil
			}
		}

		domains = rspList.Items

		slog.Info(fmt.Sprintf("req revision %d, fetch domains %d",
			req.Revision, len(rspList.Items)))
	}

	var (
		newDomains   = []*inapi.GatewayIngressDeploy{}
		tlsDomainSet = []string{}
		flush        = false
	)

	// The write lock guards only the in-memory index swap below (no early
	// return until the matching Unlock); the TLS refresh and the config
	// file write at the end of this function run outside it.
	cfg.mu.Lock()

	for _, domain := range domains {
		//
		domainEntry, added := cfg.indexDomains[domain.Domain]
		if !added {
			domainEntry = &DomainEntry{
				Domain:      domain,
				indexRoutes: map[string]*DomainEntryRoute{},
			}
			slog.Info(fmt.Sprintf("add domain %s, routes %d",
				domain.Domain, len(domain.Routes)))
		}

		cfg.lastRevision = max(cfg.lastRevision, domain.Revision)

		if !added || domain.Revision > domainEntry.setupRevision {
			domainFresh(domainEntry, domain)
			flush = true
		}

		//
		if len(domainEntry.Routes) == 0 {
			continue
		}

		// locations
		sort.Slice(domainEntry.Routes, func(i, j int) bool {
			return strings.Compare(domainEntry.Routes[i].Path, domainEntry.Routes[j].Path) > 0
		})

		if domain.LetsencryptEnable && !slices.Contains(tlsDomainSet, domain.Domain) {
			tlsDomainSet = append(tlsDomainSet, domain.Domain)
		}

		if !added {
			cfg.indexDomains[domain.Domain] = domainEntry
		}

		newDomains = append(newDomains, domain)
	}

	if req.Revision == 0 &&
		(len(newDomains) != len(cfg.indexDomains) ||
			len(newDomains) != len(cfg.Domains)) {

		slog.Info(fmt.Sprintf("cfg domains %d, new domains %d, setup %d",
			len(cfg.Domains), len(newDomains), len(cfg.indexDomains)))

		for _, domain := range cfg.Domains {
			if p := lynkapi.SlicesSearchFunc(newDomains, func(a *inapi.GatewayIngressDeploy) bool {
				return a.Domain == domain.Domain
			}); p == nil {
				delete(cfg.indexDomains, domain.Domain)
				slog.Info("delete domain " + domain.Domain)
			}
		}
		flush = true
		slog.Info(fmt.Sprintf("setup domains %d to %d", len(cfg.Domains), len(newDomains)))
		cfg.Domains = newDomains
	}

	cfg.mu.Unlock()

	if cfg.Server.HttpsPort > 0 &&
		!slices.Equal(tlsDomainCache, tlsDomainSet) {
		//
		certManager.HostPolicy = autocert.HostWhitelist(tlsDomainSet...)
		tlsDomainCache = tlsDomainSet
		slog.Info(fmt.Sprintf("tls refresh %d, domains %s",
			len(tlsDomainSet), strings.Join(tlsDomainSet, ",")))
	}

	// Persist outside the write lock: every proxied request takes cfg.mu
	// through cfg.Domain(), so holding it across the disk write (fsync)
	// would stall all in-flight requests on each full refresh. The RLock
	// only keeps the encoder's snapshot consistent against the next
	// refresh; readers never block each other.
	if flush {
		cfg.mu.RLock()
		err := htoml.EncodeToFile(&cfg, prefix+"/etc/"+appName+".toml")
		cfg.mu.RUnlock()
		if err != nil {
			return err
		}
	}

	return nil
}

type ipLimiterEntry struct {
	limiter    *rate.Limiter
	lastActive atomic.Int64
}

var ipLimiters sync.Map

func ipAddress(r *http.Request) string {
	// 客户端 IP
	ip, _, _ := net.SplitHostPort(r.RemoteAddr)

	// 如果有前端代理，取 X-Forwarded-For 或 X-Real-IP
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		ip = strings.Split(xff, ",")[0]
	} else if xri := r.Header.Get("X-Real-IP"); xri != "" {
		ip = strings.Split(xri, ",")[0]
	}

	// 防止伪造
	if pip := net.ParseIP(ip); pip == nil {
		ip = "127.0.0.0"
	}

	return ip
}

// newReverseProxy creates a reverse proxy to the target backend, injecting the
// standard forwarding headers so that upstream services can reconstruct the
// original client request, matching the nginx equivalents:
//
//	Host              : original Host header ($http_host)
//	X-Real-IP         : client remote address ($remote_addr)
//	X-Forwarded-For   : client ip appended to any existing value
//	                    ($proxy_add_x_forwarded_for)
//	X-Forwarded-Proto : request scheme ($scheme)
//
// reverseProxyTransport bounds the upstream side of proxied requests. The
// default http.DefaultTransport has no ResponseHeaderTimeout, so a backend that
// accepts the connection but never responds would hang the proxy goroutine
// indefinitely (rootHandler clears the write deadline, so nothing else would
// interrupt it). ResponseHeaderTimeout caps the wait for upstream response
// headers; the body read is bounded by the request context (client disconnect).
var reverseProxyTransport = &http.Transport{
	Proxy: http.ProxyFromEnvironment,
	DialContext: (&net.Dialer{
		Timeout:   10 * time.Second,
		KeepAlive: 30 * time.Second,
	}).DialContext,
	MaxIdleConns:          100,
	IdleConnTimeout:       90 * time.Second,
	TLSHandshakeTimeout:   10 * time.Second,
	ExpectContinueTimeout: 1 * time.Second,
	ResponseHeaderTimeout: 30 * time.Second,
}

func newReverseProxy(target *url.URL) *httputil.ReverseProxy {
	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.Transport = reverseProxyTransport
	baseDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		baseDirector(req)

		// Host ($http_host): NewSingleHostReverseProxy leaves req.Host intact,
		// which carries the original client Host header, so no override is needed.

		clientIP := req.RemoteAddr
		if host, _, err := net.SplitHostPort(req.RemoteAddr); err == nil {
			clientIP = host
		}

		// X-Real-IP ($remote_addr)
		req.Header.Set("X-Real-IP", clientIP)

		// X-Forwarded-For ($proxy_add_x_forwarded_for)
		if prior := req.Header.Get("X-Forwarded-For"); prior != "" {
			req.Header.Set("X-Forwarded-For", prior+", "+clientIP)
		} else {
			req.Header.Set("X-Forwarded-For", clientIP)
		}

		// X-Forwarded-Proto ($scheme)
		if req.TLS != nil {
			req.Header.Set("X-Forwarded-Proto", "https")
		} else {
			req.Header.Set("X-Forwarded-Proto", "http")
		}
	}
	return proxy
}

func ipLimiter(ip string) *rate.Limiter {
	now := time.Now().Unix()

	if val, ok := ipLimiters.Load(ip); ok {
		entry := val.(*ipLimiterEntry)
		entry.lastActive.Store(now)
		return entry.limiter
	}

	entry := &ipLimiterEntry{
		limiter: rate.NewLimiter(rate.Limit(cfg.Limit.Rate), int(cfg.Limit.Burst)),
	}
	entry.lastActive.Store(now)
	ipLimiters.Store(ip, entry)
	return entry.limiter
}

func ipLimiterCleaner() {

	ticker := time.NewTicker(time.Duration(cfg.Limit.CleanupSeconds) * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-signals.Done():
			return
		case <-ticker.C:
			var (
				ttl   = time.Now().Unix() - cfg.Limit.IpExpireAfter
				count = 0
			)
			ipLimiters.Range(func(key, value any) bool {
				entry := value.(*ipLimiterEntry)
				if entry.lastActive.Load() < ttl {
					ipLimiters.Delete(key)
					count++
				}
				return true
			})
			if count > 0 {
				slog.Info(fmt.Sprintf("ip limiter cleaned: %d entries removed", count))
			}
		}
	}
}

type throttledResponseWriter struct {
	http.ResponseWriter
	limiter *rate.Limiter
	ctx     context.Context
	// byteTimeout is re-armed at the start of every chunk, before its
	// limiter wait, implementing the WriteByteTimeout semantic (http.Server
	// has no such field): a write that makes no progress for this long fails
	// and ends the response, while a slow-but-progressing transfer keeps
	// extending it.
	byteTimeout time.Duration
	rc          *http.ResponseController
}

func (trw *throttledResponseWriter) Write(p []byte) (n int, err error) {
	chunkSize := 4 << 10

	for i := 0; i < len(p); i += chunkSize {
		end := i + chunkSize
		if end > len(p) {
			end = len(p)
		}

		n_chunk := end - i

		// Re-arm the per-chunk write deadline BEFORE entering the limiter
		// queue: a stalled/dead client (no write progress) is dropped after
		// byteTimeout, but a healthy transfer queueing behind the shared
		// per-IP reservation must not burn its budget while waiting for
		// tokens. Arming at queue entry gives each chunk a full byteTimeout
		// for the wait plus the write; arming after WaitN instead would let
		// the previous chunk's timer expire mid-queue -- on HTTP/2 that
		// expiry fires an async RST_STREAM that a later write cannot
		// revoke, killing healthy rate-limited streams.
		if trw.byteTimeout > 0 && trw.rc != nil {
			_ = trw.rc.SetWriteDeadline(time.Now().Add(trw.byteTimeout))
		}

		if err := trw.limiter.WaitN(trw.ctx, n_chunk); err != nil {
			return n, err
		}

		m, err := trw.ResponseWriter.Write(p[i:end])
		n += m
		if err != nil {
			return n, err
		}
	}
	return n, nil
}

// Flush forwards to the underlying connection writer; reached through
// respWriter.Flush when ReverseProxy flushes a streaming response per chunk.
// It reuses the cached rc (populated by the constructors) instead of
// allocating a controller per call on the streaming hot path.
func (trw *throttledResponseWriter) Flush() {
	rc := trw.rc
	if rc == nil {
		rc = http.NewResponseController(trw.ResponseWriter)
	}
	_ = rc.Flush()
}

var skipGzipExts = map[string]bool{
	".jpg": true, ".jpeg": true, ".png": true, ".gif": true,
	".webp": true, ".mp4": true, ".mp3": true, ".zip": true,
	".gz": true, ".rar": true, ".pdf": true,
}

var gzipMinSize = 1024

// respWriter streams a proxied response to the client with bounded memory:
// body bytes accumulate only until gzipMinSize is crossed or the first Flush
// arrives, then response headers are committed and every further byte is
// written through (gzip-compressed when applicable) instead of buffered.
// respMaxSize, when > 0, is a hard cap on the total response body size.
type respWriter struct {
	http.ResponseWriter

	requestPath string

	statusCode int

	// writeSize counts raw upstream body bytes; compSize counts bytes handed
	// to the client (compressed or not).
	writeSize   int64
	compSize    int64
	respMaxSize int64

	gzipAccept bool
	gzipWriter *gzip.Writer

	headerDone bool

	// rc wraps the underlying (throttled) writer for Flush; built lazily on
	// first use so the per-chunk flush path does not allocate.
	rc *http.ResponseController

	// capExempt is resolved once at the first body byte: event streams are
	// exempt from respMaxSize (a healthy stream accumulates bytes over its
	// lifetime; dead ones are dropped by WriteByteTimeout instead).
	capChecked bool
	capExempt  bool

	// buf holds the not-yet-committed prefix of the body; it never grows
	// beyond gzipMinSize plus one write chunk (~32KB from ReverseProxy).
	buf bytes.Buffer
}

// respCountWriter is the gzip.Writer destination; it keeps compSize in sync
// for the compressed path, whose bytes bypass respWriter.writeBody.
type respCountWriter struct {
	rw *respWriter
}

func (c *respCountWriter) Write(p []byte) (int, error) {
	n, err := c.rw.ResponseWriter.Write(p)
	c.rw.compSize += int64(n)
	return n, err
}

// Write streams one upstream body chunk. Below gzipMinSize the chunk stays
// buffered (so small responses finish with an exact Content-Length); past
// the threshold the response commits and streams. Exceeding respMaxSize
// hard-aborts the response.
func (w *respWriter) Write(b []byte) (int, error) {

	if !w.capChecked {
		w.capChecked = true
		w.capExempt = strings.Contains(
			w.Header().Get("Content-Type"), "text/event-stream")
	}
	if w.respMaxSize > 0 && !w.capExempt &&
		w.writeSize+int64(len(b)) > w.respMaxSize {
		slog.Warn("response exceeds size limit, abort",
			"path", w.requestPath, "size_limit", w.respMaxSize)
		// ErrAbortHandler is the net/http mechanism to hard-abort a response
		// mid-transfer: the connection is closed, so the client sees a broken
		// frame instead of a silently truncated body. Returning an error here
		// would let net/http finish the chunked framing cleanly and deliver a
		// truncated response as if it were complete.
		panic(http.ErrAbortHandler)
	}
	w.writeSize += int64(len(b))

	if w.headerDone {
		return w.writeBody(b)
	}

	w.buf.Write(b)
	if w.buf.Len() < gzipMinSize {
		return len(b), nil
	}

	return len(b), w.commit(w.gzipEligible())
}

func (w *respWriter) WriteHeader(statusCode int) {
	if w.statusCode == 0 {
		w.statusCode = statusCode
	}
}

// gzipEligible reports whether the response should be compressed, judged
// from the finalized response headers. Range/partial responses and
// server-sent events are excluded: re-framing the former breaks the
// Content-Range contract, and buffering the latter for the size threshold
// would delay (or swallow) small interactive events.
func (w *respWriter) gzipEligible() bool {
	if !w.gzipAccept {
		return false
	}
	h := w.Header()
	if h.Get("Content-Encoding") != "" || h.Get("Content-Range") != "" {
		return false
	}
	contentType := h.Get("Content-Type")
	if strings.Contains(contentType, "text/event-stream") ||
		strings.Contains(contentType, "image/") ||
		strings.Contains(contentType, "video/") {
		return false
	}
	if skipGzipExts[strings.ToLower(filepath.Ext(w.requestPath))] {
		return false
	}
	return true
}

// commit finalizes headers and switches to streaming: the buffered prefix is
// written through, optionally wrapping the client writer in a gzip stream
// (Content-Length is dropped in that case; framing becomes chunked).
func (w *respWriter) commit(gzipOn bool) error {
	setProxyBadge(w.Header())

	if gzipOn {
		w.Header().Del("Content-Length")
		w.Header().Set("Content-Encoding", "gzip")
		w.gzipWriter = gzip.NewWriter(&respCountWriter{rw: w})
	}
	w.headerDone = true

	if w.statusCode > 0 {
		w.ResponseWriter.WriteHeader(w.statusCode)
	} else {
		w.ResponseWriter.WriteHeader(http.StatusOK)
	}

	if w.buf.Len() == 0 {
		return nil
	}
	_, err := w.writeBody(w.buf.Bytes())
	w.buf.Reset()
	return err
}

// writeBody forwards body bytes to the client, through the gzip stream when
// active.
func (w *respWriter) writeBody(b []byte) (int, error) {
	if w.gzipWriter != nil {
		return w.gzipWriter.Write(b)
	}
	n, err := w.ResponseWriter.Write(b)
	w.compSize += int64(n)
	return n, err
}

// Flush pushes already-written bytes to the client; ReverseProxy calls it
// per chunk for streaming responses, and immediately (before the first body
// byte) for responses of unknown length. The first Flush commits the response
// under the normal gzipEligible rules: event streams and range responses
// stay pass-through, everything else starts streaming gzip right away so
// unknown-length responses keep the compression the buffered implementation
// applied. gzip.Writer.Flush emits a sync flush, preserving delivery latency.
func (w *respWriter) Flush() {
	if !w.headerDone {
		_ = w.commit(w.gzipEligible())
	}
	if w.gzipWriter != nil {
		_ = w.gzipWriter.Flush()
	}
	if w.rc == nil {
		w.rc = http.NewResponseController(w.ResponseWriter)
	}
	_ = w.rc.Flush()
}

// finish completes the response: it closes an active gzip stream (flushing
// its footer) and, for a response that never committed, commits the buffered
// bytes with an exact Content-Length. Status codes are forwarded verbatim;
// redirect coercion is not done here -- the GatewayIngressType_Redirect
// branch sets its own 302, and a proxied Location response (e.g. 201
// Created) must not be rewritten.
func (w *respWriter) finish() {
	if w.gzipWriter != nil {
		_ = w.gzipWriter.Close()
		w.gzipWriter = nil
	}
	if w.headerDone {
		return
	}
	// exact Content-Length for the buffered (never-streamed) response
	if w.buf.Len() > 0 {
		w.Header().Set("Content-Length", strconv.Itoa(w.buf.Len()))
	}
	_ = w.commit(false)
}

// setProxyBadge stamps the gateway identity over any upstream-supplied
// X-Proxy value; called at header-commit time, after ReverseProxy has copied
// the upstream headers, and by localfsServe.
func setProxyBadge(h http.Header) {
	h.Set("X-Proxy", "InnerStack/"+version)
}

type httpRootHandler struct{}

func (it httpRootHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {

	if n := strings.IndexByte(r.Host, ':'); n > 0 {
		r.Host = r.Host[:n]
	}

	if domain := cfg.Domain(r.Host); domain == nil {

		if module := cfg.Module(r.Host); module != nil {
			module.handler(&ServiceContext{Options: module.Options}, w, r)
		} else {
			handleWriteHtml(w, 404, builtin_404_HTML)
		}
	} else if cfg.Server.HttpsPort > 0 && domain.LetsencryptEnabled() {
		certManager.HTTPHandler(nil).ServeHTTP(w, r)
	} else {
		rootHandler(w, r)
	}
}

func rootHandler(w http.ResponseWriter, r *http.Request) {

	r.Body = http.MaxBytesReader(w, r.Body, cfg.Server.MaxBodySize)

	urlPath := path.Clean(r.URL.Path)

	// ingate streams and rate-limits its responses (localfs downloads, proxied
	// upstream bodies), which legitimately outlast http.Server's fixed
	// WriteTimeout -- for HTTP/2 that timer fires onWriteTimeout and resets the
	// stream with INTERNAL_ERROR mid-transfer. Clear the per-response write
	// deadline here so neither the streaming transfer (proxy reading the
	// upstream) nor the throttled writes get cut off. Stall/dead-client
	// protection comes from the per-chunk deadline re-armed inside
	// throttledResponseWriter (WriteByteTimeout semantic) and from the request
	// context (client disconnect).
	_ = http.NewResponseController(w).SetWriteDeadline(time.Time{})

	// localfs is served straight from disk by localfsServe, streaming the file
	// under the rate limit with accurate Content-Length and Range support.
	if localfsServe(w, r, urlPath) {
		return
	}

	w = &throttledResponseWriter{
		ResponseWriter: w,
		limiter:        ipLimiter(ipAddress(r)),
		ctx:            r.Context(),
		byteTimeout:    time.Duration(cfg.Server.WriteByteTimeout) * time.Second,
		rc:             http.NewResponseController(w),
	}

	var (
		tn = time.Now()

		hitRoute *DomainEntryRoute
		hw       = &respWriter{
			requestPath:    urlPath,
			respMaxSize:    cfg.Server.MaxRespSize,
			ResponseWriter: w,
		}
	)

	defer func() {
		lat := time.Since(tn)
		metricComplex.Add("Service", "RootHandler", 1, 0, lat)
		if hitRoute != nil {
			metricComplex.Add("Service", "RouteType:"+hitRoute.Type, 1, 0, lat)
			// The metric registry never evicts a series, so the label must be
			// bounded by config, not by client input. hitRoute != nil implies
			// cfg.Domain(r.Host) matched (r.Host is a configured domain), and
			// hitRoute.Path comes from the ingress config: cardinality is
			// #domains x #routes. Using the raw URL path here would grow one
			// series per distinct request path forever (scanner / ID-bearing
			// paths) and eventually exhaust memory.
			metricComplex.Add("HostService", r.Host+":"+hitRoute.Path, 1, 0, lat)
		}
		metricGauge.Add("Service", "RawSize", float64(hw.writeSize))
		metricGauge.Add("Service", "CompSize", float64(hw.compSize))
	}()

	if strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
		hw.gzipAccept = true
	}

	handler := func(w2 http.ResponseWriter, r *http.Request) *DomainEntryRoute {

		domain := cfg.Domain(r.Host)
		if domain == nil {
			handleWriteHtml(w2, 403, builtin_403_HTML)
			return nil
		}

		route := domain.lookup(urlPath)
		if route == nil {
			handleWriteHtml(w2, 404, builtin_404_HTML)
			return nil
		}

		switch route.Type {

		case inapi.GatewayIngressType_Instance,
			inapi.GatewayIngressType_Upstream:

			if len(route.reverseProxy) > 0 {
				idx := int(atomic.AddUint64(&route.callCount, 1) % uint64(len(route.reverseProxy)))
				route.reverseProxy[idx].ServeHTTP(w2, r)
			}
			return route

		case inapi.GatewayIngressType_Redirect:
			w2.Header().Set("Location", route.Urls[0].String())
			w2.WriteHeader(http.StatusFound)
			return route
		}

		handleWriteHtml(w2, 404, builtin_404_HTML)
		return nil
	}

	hitRoute = handler(hw, r)

	// Finalize the response: close the gzip stream of a streamed body, or
	// flush a still-buffered small response with an exact Content-Length (or
	// its redirect status). finish() runs even when hitRoute is nil, so
	// buffered error pages (403/404) are delivered instead of net/http
	// silently answering 200 with an empty body.
	hw.finish()
}

// localfsServe streams a static file for a request whose route resolves to a
// localfs entry, writing the response (file contents, or a 403/404 page) to w.
// It returns true when the matched route is localfs (handled), false otherwise
// so rootHandler can fall through to the proxy/redirect path.
//
// The file is streamed under the per-IP rate limit via http.ServeContent,
// which sets an accurate Content-Length, honors Range requests, and is fully
// HTTP/2 compliant. rootHandler clears the write deadline
// up front (streaming/throttled transfers legitimately outlast WriteTimeout);
// the throttle re-arms it per chunk (WriteByteTimeout) to drop stalled clients,
// and rate.Limiter.WaitN cancels on r.Context() when the client disconnects.
//
// Access is confined to the route's root directory via os.Root: path
// resolution cannot escape the root even by following a symlink planted
// inside it (rootDir comes from the zonelet-pushed ingress config and may be
// writable by other processes, so plain os.Open would leak arbitrary host
// files).
func localfsServe(w http.ResponseWriter, r *http.Request, urlPath string) bool {

	domain := cfg.Domain(r.Host)
	if domain == nil {
		return false
	}

	route := domain.lookup(urlPath)
	if route == nil || route.Type != "localfs" {
		return false
	}

	// Metrics are recorded only once this function actually handles the
	// request: rootHandler's own defer covers the non-localfs paths, and an
	// unconditional defer here would double-count every proxied request.
	// The HostService label uses the configured route path (bounded by the
	// ingress config), never the raw request path -- the metric registry
	// never evicts a series, so per-URL labels would grow without bound.
	tn := time.Now()
	metricComplex.Add("Service", "RootHandler", 1, 0, 0)
	metricComplex.Add("Service", "RouteType:localfs", 1, 0, 0)
	defer func() {
		metricComplex.Add("HostService", r.Host+":"+route.Path, 1, 0, time.Since(tn))
	}()

	setProxyBadge(w.Header())

	// route.Urls is populated only when the configured target exists and is a
	// directory (see configRefresh). An empty set means the path is not
	// resolvable from this process -- surface a real 404 rather than a 200/empty.
	if len(route.Urls) == 0 {
		handleWriteHtml(w, http.StatusNotFound, builtin_404_HTML)
		return true
	}

	rootDir := route.Urls[0].Path
	// Strip the route prefix and its separator, then clean; an empty
	// remainder (urlPath == route.Path) cleans to ".", which the dir check
	// below rejects with 403.
	relPath := filepath.Clean(strings.TrimPrefix(strings.TrimPrefix(urlPath, route.Path), "/"))

	// Explicit lexical escapes get 403; os.Root refusals, including the
	// symlink escapes it detects during resolution, surface as the 404
	// below. os.Root is the confinement -- this guard only preserves the
	// status-code distinction.
	if filepath.IsAbs(relPath) || strings.HasPrefix(relPath, "..") {
		handleWriteHtml(w, http.StatusForbidden, builtin_403_HTML)
		return true
	}

	root, err := os.OpenRoot(rootDir)
	if err != nil {
		handleWriteHtml(w, http.StatusNotFound, builtin_404_HTML)
		return true
	}
	defer root.Close()

	f, err := root.Open(relPath)
	if err != nil {
		handleWriteHtml(w, http.StatusNotFound, builtin_404_HTML)
		return true
	}
	defer f.Close()

	// Stat on the open fd: no path re-resolution, no stat/open race.
	st, err := f.Stat()
	if err != nil {
		handleWriteHtml(w, http.StatusNotFound, builtin_404_HTML)
		return true
	}
	if st.IsDir() {
		handleWriteHtml(w, http.StatusForbidden, builtin_403_HTML)
		return true
	}

	// The write deadline is already cleared by rootHandler; the throttle
	// re-arms it per chunk (WriteByteTimeout) so a stalled client is still dropped.
	tw := &throttledResponseWriter{
		ResponseWriter: w,
		limiter:        ipLimiter(ipAddress(r)),
		ctx:            r.Context(),
		byteTimeout:    time.Duration(cfg.Server.WriteByteTimeout) * time.Second,
		rc:             http.NewResponseController(w),
	}

	// ServeContent (not ServeFile): ServeFile 301-redirects to "./" when
	// r.URL.Path ends in "/index.html". It streams the file in chunks
	// (io.CopyN) through tw, so only a
	// small buffer is in flight at a time rather than the whole file. It sets
	// Content-Length = sendSize itself, and WaitN blocks (never drops bytes),
	// so the byte count always matches -- HTTP/2 is happy as long as the
	// transfer is not interrupted, which the deadline clear above guarantees.
	http.ServeContent(tw, r, st.Name(), st.ModTime(), f)
	return true
}

// modules

type ServiceContext struct {
	Options map[string]string
}

func (it *ServiceContext) Option(name string) string {
	if it.Options != nil {
		return it.Options[name]
	}
	return ""
}

type ModuleHandler func(ctx *ServiceContext, w http.ResponseWriter, r *http.Request)

// module:DomainSale

func module_DomainSale_Handler(ctx *ServiceContext, w http.ResponseWriter, r *http.Request) {

	params := map[string]string{
		"domain_name":   r.Host,
		"contact_email": ctx.Option("contact_email"),
	}

	data, _ := tplrender.Render(module_DomainSale_HTML, params)

	w.Header().Set("Content-Type", "text/html")
	w.Write(data)
}

func handleWriteHtml(w http.ResponseWriter, code int, body []byte) {
	w.Header().Set("Content-Type", "text/html")
	w.WriteHeader(code)
	w.Write(body)
}

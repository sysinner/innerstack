// Copyright 2015 Eryx <evorui аt gmаil dοt cοm>, All rights reserved.
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
	"io"
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
	"google.golang.org/grpc"

	"github.com/sysinner/incore/v2/inapi"
	"github.com/sysinner/incore/v2/internal/client"
	"github.com/sysinner/incore/v2/internal/inutil/tplrender"
	"github.com/sysinner/incore/v2/pkg/inauth"
	"github.com/sysinner/incore/v2/pkg/inlog"
	"github.com/sysinner/incore/v2/pkg/signals"
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
			Addr:           fmt.Sprintf(":%d", cfg.Server.HttpPort),
			Handler:        httpRootHandler{},
			ReadTimeout:    time.Duration(cfg.Server.ReadTimeout) * time.Second,
			WriteTimeout:   time.Duration(cfg.Server.WriteTimeout) * time.Second,
			MaxHeaderBytes: 1 << 20,
		}
		signals.Go(func() {
			slog.Info("http server start " + httpServer.Addr)
			if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				slog.Info("http server quit : " + err.Error())
			}
		}, func() {
			httpServer.Shutdown(context.Background())
		})
	}
	if cfg.Server.HttpsPort > 0 {
		httpsServer = &http.Server{
			Addr:    fmt.Sprintf(":%d", cfg.Server.HttpsPort),
			Handler: mux,
			TLSConfig: &tls.Config{
				GetCertificate: certManager.GetCertificate,
			},
			ReadTimeout:    time.Duration(cfg.Server.ReadTimeout) * time.Second,
			WriteTimeout:   time.Duration(cfg.Server.WriteTimeout) * time.Second,
			MaxHeaderBytes: 1 << 20,
		}
		signals.Go(func() {
			slog.Info("https server start " + httpsServer.Addr)
			if err := httpsServer.ListenAndServeTLS("", ""); err != nil && err != http.ErrServerClosed {
				slog.Error("https server quit : " + err.Error())
			}
			tlsDomainCache = nil
		}, func() {
			httpsServer.Shutdown(context.Background())
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

type Config struct {
	mu sync.RWMutex

	Server ConfigServer `toml:"server"`

	Limit ConfigLimit `toml:"limit"`

	Zone *ConfigZone `toml:"zone,omitempty"`

	Modules      []*ConfigModule          `toml:"modules"`
	indexModules map[string]*ConfigModule `toml:"-"`

	Domains      []*inapi.GatewayIngressDeploy `toml:"domains"`
	indexDomains map[string]*DomainEntry       `toml:"-"`

	lastVersion     uint64
	lastFullUpdated int64
}

type ConfigServer struct {
	HttpPort  int `toml:"http_port"`
	HttpsPort int `toml:"https_port"`

	ReadTimeout  int64 `toml:"read_timeout"`
	WriteTimeout int64 `toml:"write_timeout"`
	MaxBodySize  int64 `toml:"max_body_size"`

	DebugPprofEnable bool `toml:"debug_pprof_enable,omitempty"`
}

type ConfigZone struct {
	Name  string   `toml:"name"`
	Hosts []string `json:"hosts" toml:"hosts"`
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
	it.mu.Lock()
	defer it.mu.Unlock()
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

	setupVersion uint64
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
	appName = "inservice"

	prefix = "/opt/instack"

	tlsCacheDir = prefix + "/var/" + appName + "_tls_cache"

	tlsDomainCache = []string{}

	httpServer  *http.Server
	httpsServer *http.Server

	certManager autocert.Manager

	version = "0.11"

	cfg Config

	zoneConn *grpc.ClientConn
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

	if cfg.Server.MaxBodySize <= 0 {
		cfg.Server.MaxBodySize = 16 << 20 // 默认 16MB
	} else {
		// min 8 MB, max 64 MB
		cfg.Server.MaxBodySize = max(cfg.Server.MaxBodySize, 8<<20)
		// max 64 MB
		cfg.Server.MaxBodySize = min(cfg.Server.MaxBodySize, 64<<20)
	}

	if cfg.Server.ReadTimeout <= 0 {
		cfg.Server.ReadTimeout = 61
	} else {
		cfg.Server.ReadTimeout = max(cfg.Server.ReadTimeout, 3)   // 最小 3 秒
		cfg.Server.ReadTimeout = min(cfg.Server.ReadTimeout, 300) // 最大 300 秒
	}

	if cfg.Server.WriteTimeout <= 0 {
		cfg.Server.WriteTimeout = 61
	} else {
		cfg.Server.WriteTimeout = max(cfg.Server.WriteTimeout, 3)   // 最小 3 秒
		cfg.Server.WriteTimeout = min(cfg.Server.WriteTimeout, 300) // 最大 300 秒
	}

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

func configRefresh(domains []*inapi.GatewayIngressDeploy) error {

	tn := time.Now().Unix()
	req := &inapi.GatewayIngressDeployListRequest{}

	if len(domains) == 0 && zoneConn != nil {

		if cfg.lastFullUpdated+600 < tn {
			req.Version = 0
			cfg.lastFullUpdated = tn
		} else {
			req.Version = cfg.lastVersion
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

		if req.Version == 0 && len(cfg.Domains) > 0 {
			r := float64(len(rspList.Items)) / float64(len(cfg.Domains))
			if r < 0.5 {
				slog.Info(fmt.Sprintf("fetch domains %d/%d, skip", len(rspList.Items), len(cfg.Domains)))
				return nil
			}
		}

		domains = rspList.Items

		slog.Info(fmt.Sprintf("req version %d, fetch domains %d",
			req.Version, len(rspList.Items)))
	}

	var (
		newDomains   = []*inapi.GatewayIngressDeploy{}
		tlsDomainSet = []string{}
		flush        = false
	)

	domainFresh := func(domainEntry *DomainEntry, domain *inapi.GatewayIngressDeploy) {
		domainEntry.mu.Lock()
		defer domainEntry.mu.Unlock()

		prevRoutes := domainEntry.Routes

		for _, route := range prevRoutes {
			if route.reverseProxy != nil {
				for _, rp := range route.reverseProxy {
					if rp.Transport != nil {
						if closer, ok := rp.Transport.(io.Closer); ok {
							closer.Close()
						}
					}
				}
			}
		}

		flush = true
		domainEntry.Routes = nil
		domainEntry.indexRoutes = map[string]*DomainEntryRoute{}
		domainEntry.setupVersion = domain.Version

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
					rps = append(rps, httputil.NewSingleHostReverseProxy(u))
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

	cfg.mu.Lock()
	defer cfg.mu.Unlock()

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

		cfg.lastVersion = max(cfg.lastVersion, domain.Version)

		if !added || domain.Version > domainEntry.setupVersion {
			domainFresh(domainEntry, domain)
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

	if req.Version == 0 &&
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

	if cfg.Server.HttpsPort > 0 &&
		!slices.Equal(tlsDomainCache, tlsDomainSet) {
		//
		certManager.HostPolicy = autocert.HostWhitelist(tlsDomainSet...)
		tlsDomainCache = tlsDomainSet
		slog.Info(fmt.Sprintf("tls refresh %d, domains %s",
			len(tlsDomainSet), strings.Join(tlsDomainSet, ",")))
	}

	if flush {
		if err := htoml.EncodeToFile(&cfg, prefix+"/etc/"+appName+".toml"); err != nil {
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
}

func (trw *throttledResponseWriter) Write(p []byte) (n int, err error) {
	chunkSize := 4 << 10

	for i := 0; i < len(p); i += chunkSize {
		end := i + chunkSize
		if end > len(p) {
			end = len(p)
		}

		n_chunk := end - i

		// 阻塞等待令牌发放
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

var skipGzipExts = map[string]bool{
	".jpg": true, ".jpeg": true, ".png": true, ".gif": true,
	".webp": true, ".mp4": true, ".mp3": true, ".zip": true,
	".gz": true, ".rar": true, ".pdf": true,
}

var gzipMinSize = 1024

type respWriter struct {
	http.ResponseWriter

	requestPath string

	statusCode int

	writeSize int
	writeBuff *bytes.Buffer

	gzipAccept bool
	gzipWriter *gzip.Writer
}

func (w *respWriter) Write(b []byte) (int, error) {

	if w.writeBuff == nil {
		w.writeBuff = &bytes.Buffer{}
	}

	// 如果满足 gzip 条件且尚未初始化，直接包装原始 ResponseWriter
	if w.gzipWriter == nil && w.gzipAccept &&
		w.Header().Get("Content-Encoding") == "" {

		ext := filepath.Ext(w.requestPath)
		isCompressed := skipGzipExts[strings.ToLower(ext)]

		contentType := w.Header().Get("Content-Type")
		if contentType != "" &&
			(strings.Contains(contentType, "image/") || strings.Contains(contentType, "video/")) {
			isCompressed = true
		}

		if !isCompressed && (len(b) >= gzipMinSize || w.writeBuff.Len() >= gzipMinSize) {
			if w.writeBuff.Len() > 0 {
				writeBuff := &bytes.Buffer{}
				w.gzipWriter = gzip.NewWriter(writeBuff)
				if n, err := w.gzipWriter.Write(w.writeBuff.Bytes()); err != nil {
					return n, err
				}
				w.writeBuff = writeBuff
			} else {
				w.gzipWriter = gzip.NewWriter(w.writeBuff)
			}
		}
	}

	if w.gzipWriter != nil {
		return w.gzipWriter.Write(b)
	}

	w.writeSize += len(b)
	return w.writeBuff.Write(b)
}

func (w *respWriter) WriteHeader(statusCode int) {
	if w.statusCode == 0 {
		w.statusCode = statusCode
	}
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
	} else if cfg.Server.HttpsPort > 0 && domain.Domain.LetsencryptEnable {
		certManager.HTTPHandler(nil).ServeHTTP(w, r)
	} else {
		rootHandler(w, r)
	}
}

func rootHandler(w http.ResponseWriter, r *http.Request) {

	r.Body = http.MaxBytesReader(w, r.Body, cfg.Server.MaxBodySize)

	w = &throttledResponseWriter{
		ResponseWriter: w,
		limiter:        ipLimiter(ipAddress(r)),
		ctx:            r.Context(),
	}

	var (
		tn      = time.Now()
		urlPath = path.Clean(r.URL.Path)

		hitRoute *DomainEntryRoute
		hw       = &respWriter{
			requestPath:    urlPath,
			ResponseWriter: w,
		}
	)

	defer func() {
		lat := time.Since(tn)
		metricComplex.Add("Service", "RootHandler", 1, 0, lat)
		if hitRoute != nil {
			metricComplex.Add("Service", "RouteType:"+hitRoute.Type, 1, 0, lat)
		}
		if urlPath != "" {
			metricComplex.Add("HostService", r.Host+":"+urlPath, 1, 0, lat)
		}
		metricGauge.Add("Service", "RawSize", float64(hw.writeSize))
		if hw.writeBuff != nil {
			metricGauge.Add("Service", "CompSize", float64(hw.writeBuff.Len()))
		}
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

		case "localfs":
			if len(route.Urls) > 0 {

				relPath := strings.TrimPrefix(urlPath, route.Path)

				finalPath := filepath.Join(route.Urls[0].Path, relPath)

				rel, err := filepath.Rel(route.Urls[0].Path, finalPath)
				if err != nil || strings.HasPrefix(rel, "..") {
					handleWriteHtml(w2, 403, builtin_403_HTML)
				} else if st, err := os.Stat(finalPath); err != nil {
					handleWriteHtml(w2, 404, builtin_404_HTML)
				} else if st.IsDir() {
					handleWriteHtml(w2, 403, builtin_403_HTML)
				} else {
					http.ServeFile(w2, r, finalPath)
				}
				return route
			}
		}

		handleWriteHtml(w2, 404, builtin_404_HTML)
		return nil
	}

	hitRoute = handler(hw, r)

	w.Header().Del("X-Proxy")
	w.Header().Set("X-Proxy", "InnerStack/"+version)

	if hitRoute == nil {
		return
	}

	if hw.gzipWriter != nil {
		hw.gzipWriter.Close()
		w.Header().Del("Content-Encoding")
		w.Header().Set("Content-Encoding", "gzip")
	}

	if hw.writeBuff != nil && hw.writeBuff.Len() > 0 {
		w.Header().Del("Content-Length")
		w.Header().Set("Content-Length", strconv.Itoa(hw.writeBuff.Len()))
	}

	if uri := w.Header().Get("Location"); uri != "" &&
		w.Header().Get("Content-Type") == "" {
		if hw.statusCode >= 300 && hw.statusCode < 310 {
			w.WriteHeader(hw.statusCode)
		} else {
			w.WriteHeader(http.StatusFound)
		}
		return
	} else if hw.statusCode > 0 {
		w.WriteHeader(hw.statusCode)
	}

	if hw.writeBuff != nil && hw.writeBuff.Len() > 0 {
		w.Write(hw.writeBuff.Bytes())
	}
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

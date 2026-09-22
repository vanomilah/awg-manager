package cdndispatcher

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Dispatcher coordinates HTTP traffic from the router's origin proxy port (default :9009)
// to either Xray (for VPN path e.g. /cdn-bridge) or Telegram WEB Proxy (for /?bridge= and /api/v1).
type Dispatcher struct {
	mu       sync.Mutex
	dataDir  string
	cfg      Config
	snapshot atomic.Pointer[routeSnapshot]
	server   *http.Server
	listener net.Listener
	exited   chan struct{}
	running  bool
}

type Config struct {
	ListenAddr     string `json:"listen_addr"`                // e.g. ":9009"
	XrayTarget     string `json:"xray_target"`                // e.g. "http://127.0.0.1:9008"
	TgTarget       string `json:"tg_target"`                  // e.g. "http://127.0.0.1:8085"
	PublicHostname string `json:"public_hostname"`            // Shared Public domain for CDN ingress (default empty)
	XrayPublicHost string `json:"xray_public_host,omitempty"` // Dedicated Xray CDN host if distinct
	TgPublicHost   string `json:"tg_public_host,omitempty"`   // Dedicated Telegram CDN host if distinct
	XrayPathPrefix string `json:"xray_path_prefix"`           // e.g. "/cdn-bridge"
}

type routeSnapshot struct {
	xrayProxy      http.Handler
	tgProxy        http.Handler
	xrayPathPrefix string
	publicHostname string
	xrayPublicHost string
	tgPublicHost   string
}

func normalizeHost(rawHost string) string {
	rawHost = strings.TrimSpace(rawHost)
	if rawHost == "" {
		return ""
	}
	if h, _, err := net.SplitHostPort(rawHost); err == nil {
		rawHost = h
	}
	rawHost = strings.TrimPrefix(rawHost, "[")
	rawHost = strings.TrimSuffix(rawHost, "]")
	return strings.ToLower(strings.TrimSpace(rawHost))
}

func isTelegramEndpoint(path string) bool {
	if path == "/" || path == "" {
		return true
	}
	if path == "/api" || strings.HasPrefix(path, "/api/") {
		return true
	}
	if path == "/tg" || strings.HasPrefix(path, "/tg/") {
		return true
	}
	if path == "/metrics" || path == "/health" {
		return true
	}
	return false
}

func New(cfg Config) *Dispatcher {
	return NewWithDataDir("/opt/etc/awg-manager", cfg)
}

func NewWithDataDir(dataDir string, cfg Config) *Dispatcher {
	if cfg.ListenAddr == "" {
		cfg.ListenAddr = ":9009"
	}
	if cfg.XrayTarget == "" {
		cfg.XrayTarget = "http://127.0.0.1:9008"
	}
	if cfg.TgTarget == "" {
		cfg.TgTarget = "http://127.0.0.1:8085"
	}
	if cfg.XrayPathPrefix == "" {
		cfg.XrayPathPrefix = "/cdn-bridge"
	}

	d := &Dispatcher{
		dataDir: dataDir,
		cfg:     cfg,
	}

	snap, _ := d.buildSnapshot(cfg)
	if snap != nil {
		d.snapshot.Store(snap)
	}

	return d
}

func (d *Dispatcher) SetDataDir(dir string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.dataDir = dir
}

// NormalizePathPrefix returns a clean, canonical path prefix with leading slash and no trailing slash (e.g. "/cdn-bridge").
func NormalizePathPrefix(prefix string) string {
	norm := "/" + strings.Trim(strings.TrimSpace(prefix), "/")
	if norm == "/" {
		return "/cdn-bridge"
	}
	return norm
}

func (d *Dispatcher) buildSnapshot(cfg Config) (*routeSnapshot, error) {
	normPrefix := NormalizePathPrefix(cfg.XrayPathPrefix)

	normPublic := normalizeHost(cfg.PublicHostname)
	normXray := normalizeHost(cfg.XrayPublicHost)
	normTg := normalizeHost(cfg.TgPublicHost)

	if normXray == "" && normPublic != "" {
		normXray = normPublic
	}
	if normTg == "" && normPublic != "" {
		normTg = normPublic
	}
	if normTg == "" && d.dataDir != "" {
		tproxyCfgPath := filepath.Join(d.dataDir, "tproxy", "config.json")
		if data, err := os.ReadFile(tproxyCfgPath); err == nil {
			var tp struct {
				PublicHostname string `json:"public_hostname"`
			}
			if json.Unmarshal(data, &tp) == nil && tp.PublicHostname != "" {
				normTg = normalizeHost(tp.PublicHostname)
			}
		}
	}

	snap := &routeSnapshot{
		xrayPathPrefix: normPrefix,
		publicHostname: normPublic,
		xrayPublicHost: normXray,
		tgPublicHost:   normTg,
	}

	if cfg.XrayTarget != "" {
		xrayURL, err := url.Parse(cfg.XrayTarget)
		if err != nil {
			return nil, fmt.Errorf("invalid xray target: %w", err)
		}
		snap.xrayProxy = &httputil.ReverseProxy{
			FlushInterval: -1,
			Rewrite: func(pr *httputil.ProxyRequest) {
				pr.SetURL(xrayURL)
				pr.Out.Host = pr.In.Host

				// 1. Ensure trailing slash if request path is exactly prefix (/cdn-bridge -> /cdn-bridge/)
				// Xray's splithttp handler strictly expects path prefix with trailing slash.
				if pr.Out.URL.Path == normPrefix {
					pr.Out.URL.Path = normPrefix + "/"
				}

				// 2. Fix: When requests arrive via CDN or KeenDNS relay, the Referer header
				// (which Xray SplitHTTP uses to carry x_padding) is often stripped by edge servers
				// or local reverse proxies. If neither Referer nor x_padding query parameter is present,
				// inject a valid padding Referer header so Xray server's padding validation succeeds.
				if pr.Out.Header.Get("Referer") == "" && pr.Out.URL.Query().Get("x_padding") == "" {
					dummyPadding := strings.Repeat("X", 300)
					targetHost := normXray
					if targetHost == "" {
						targetHost = pr.Out.Host
					}
					pr.Out.Header.Set("Referer", "https://"+targetHost+normPrefix+"/?x_padding="+dummyPadding)
				}
			},
			ModifyResponse: func(resp *http.Response) error {
				resp.Header.Set("X-CDN-Route", "xray")
				resp.Header.Set("X-Accel-Buffering", "no")
				resp.Header.Set("Cache-Control", "no-store, no-cache, must-revalidate, max-age=0, s-maxage=0, no-transform")
				resp.Header.Set("Pragma", "no-cache")
				resp.Header.Set("Expires", "0")
				return nil
			},
			ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
				w.Header().Set("X-CDN-Route", "xray")
				http.Error(w, "Xray service unavailable", http.StatusServiceUnavailable)
			},
		}
	}

	if cfg.TgTarget != "" {
		tgURL, err := url.Parse(cfg.TgTarget)
		if err != nil {
			return nil, fmt.Errorf("invalid tg target: %w", err)
		}
		snap.tgProxy = &httputil.ReverseProxy{
			FlushInterval: -1,
			Rewrite: func(pr *httputil.ProxyRequest) {
				pr.SetURL(tgURL)
				if normTg != "" {
					pr.Out.Host = normTg
				} else if cfg.PublicHostname != "" {
					pr.Out.Host = cfg.PublicHostname
				} else {
					pr.Out.Host = pr.In.Host
				}
				forwarded := pr.In.Header.Get("X-Forwarded-For")
				if forwarded != "" {
					parts := strings.Split(forwarded, ",")
					pr.Out.Header.Set("X-Forwarded-For", strings.TrimSpace(parts[0]))
				} else if pr.In.RemoteAddr != "" {
					host, _, _ := net.SplitHostPort(pr.In.RemoteAddr)
					if host != "" {
						pr.Out.Header.Set("X-Forwarded-For", host)
					}
				}
			},
			ModifyResponse: func(resp *http.Response) error {
				resp.Header.Set("X-CDN-Route", "tgwebproxy")
				return nil
			},
			ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
				w.Header().Set("X-CDN-Route", "tgwebproxy")
				http.Error(w, "Telegram service unavailable", http.StatusServiceUnavailable)
			},
		}
	}

	return snap, nil
}

func (d *Dispatcher) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// 1. Loopback-only health check endpoint
	if r.URL.Path == "/.cdndisp/health" {
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			host = r.RemoteAddr
		}
		ip := net.ParseIP(host)
		if ip == nil || !ip.IsLoopback() {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("{\"status\":\"ok\"}\n"))
		return
	}

	snap := d.snapshot.Load()
	if snap == nil {
		http.Error(w, "Dispatcher not configured", http.StatusServiceUnavailable)
		return
	}

	path := r.URL.Path
	prefix := snap.xrayPathPrefix
	matchesXray := path == prefix || strings.HasPrefix(path, prefix+"/")
	isTelegram := isTelegramEndpoint(path)

	reqHost := normalizeHost(r.Host)
	if fwdHost := normalizeHost(r.Header.Get("X-Forwarded-Host")); fwdHost != "" {
		reqHost = fwdHost
	}
	xrayHost := snap.xrayPublicHost
	tgHost := snap.tgPublicHost

	isLoopback := reqHost == "127.0.0.1" || reqHost == "localhost" || strings.HasPrefix(reqHost, "127.")

	// If no hostnames are configured, or request arrives via local reverse proxy (e.g. Keenetic KeenDNS on 127.0.0.1),
	// route based on path prefix:
	if (xrayHost == "" && tgHost == "") || isLoopback {
		if matchesXray {
			if snap.xrayProxy != nil {
				w.Header().Set("X-CDN-Route", "xray")
				snap.xrayProxy.ServeHTTP(w, r)
				return
			}
			w.Header().Set("X-CDN-Route", "xray")
			http.Error(w, "Xray target unavailable", http.StatusServiceUnavailable)
			return
		}
		if isTelegram {
			if snap.tgProxy != nil {
				w.Header().Set("X-CDN-Route", "tgwebproxy")
				snap.tgProxy.ServeHTTP(w, r)
				return
			}
			w.Header().Set("X-CDN-Route", "tgwebproxy")
			http.Error(w, "Telegram target unavailable", http.StatusServiceUnavailable)
			return
		}
		http.NotFound(w, r)
		return
	}

	isXrayMatch := (xrayHost != "" && reqHost == xrayHost)
	isTgMatch := (tgHost != "" && reqHost == tgHost)

	// Distinct host routing:
	// 1. Host == XrayPublicHost && !isTgMatch (Xray-only host)
	if isXrayMatch && !isTgMatch {
		if matchesXray {
			if snap.xrayProxy != nil {
				w.Header().Set("X-CDN-Route", "xray")
				snap.xrayProxy.ServeHTTP(w, r)
				return
			}
			w.Header().Set("X-CDN-Route", "xray")
			http.Error(w, "Xray target unavailable", http.StatusServiceUnavailable)
			return
		}
		// Xray-only host requested with non-Xray path (or Telegram endpoint): reject with 404
		http.NotFound(w, r)
		return
	}

	// 2. Host == TgPublicHost && !isXrayMatch (Telegram-only host)
	if isTgMatch && !isXrayMatch {
		if isTelegram {
			if snap.tgProxy != nil {
				w.Header().Set("X-CDN-Route", "tgwebproxy")
				snap.tgProxy.ServeHTTP(w, r)
				return
			}
			w.Header().Set("X-CDN-Route", "tgwebproxy")
			http.Error(w, "Telegram target unavailable", http.StatusServiceUnavailable)
			return
		}
		// Telegram-only host requested with non-Telegram path (or Xray path): reject with 404
		http.NotFound(w, r)
		return
	}

	// 3. Shared host (Host == XrayPublicHost && Host == TgPublicHost)
	if isXrayMatch && isTgMatch {
		if matchesXray {
			if snap.xrayProxy != nil {
				w.Header().Set("X-CDN-Route", "xray")
				snap.xrayProxy.ServeHTTP(w, r)
				return
			}
			w.Header().Set("X-CDN-Route", "xray")
			http.Error(w, "Xray target unavailable", http.StatusServiceUnavailable)
			return
		}
		if isTelegram {
			if snap.tgProxy != nil {
				w.Header().Set("X-CDN-Route", "tgwebproxy")
				snap.tgProxy.ServeHTTP(w, r)
				return
			}
			w.Header().Set("X-CDN-Route", "tgwebproxy")
			http.Error(w, "Telegram target unavailable", http.StatusServiceUnavailable)
			return
		}
		http.NotFound(w, r)
		return
	}

	// 4. Unknown host
	http.NotFound(w, r)
}

func (d *Dispatcher) Start() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.startLocked()
}

func (d *Dispatcher) startLocked() error {
	if d.running {
		return nil
	}

	snap, err := d.buildSnapshot(d.cfg)
	if err != nil {
		return err
	}
	d.snapshot.Store(snap)

	ln, err := net.Listen("tcp", d.cfg.ListenAddr)
	if err != nil {
		return fmt.Errorf("cdn dispatcher bind %s: %w", d.cfg.ListenAddr, err)
	}

	server := &http.Server{
		Handler: d,
	}

	exited := make(chan struct{})
	d.server = server
	d.listener = ln
	d.exited = exited
	d.running = true

	go func() {
		defer close(exited)
		if err := server.Serve(ln); err != nil && err != http.ErrServerClosed {
			log.Printf("[CDN-Dispatcher] Serve error: %v", err)
		}
		d.mu.Lock()
		if d.server == server {
			d.running = false
		}
		d.mu.Unlock()
	}()

	return nil
}

func (d *Dispatcher) Stop() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.stopLocked()
}

func (d *Dispatcher) stopLocked() error {
	if !d.running || d.server == nil {
		return nil
	}

	server := d.server
	exited := d.exited
	d.running = false
	d.server = nil
	d.listener = nil

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	err := server.Shutdown(ctx)
	select {
	case <-exited:
	case <-ctx.Done():
		_ = server.Close()
	}
	return err
}

type applyMode int

const (
	modePatch applyMode = iota
	modeReplace
)

func (d *Dispatcher) applyConfigLocked(cfg Config, mode applyMode) error {
	var newCfg Config
	if mode == modePatch {
		newCfg = d.cfg
		if cfg.ListenAddr != "" {
			newCfg.ListenAddr = cfg.ListenAddr
		}
		if cfg.XrayTarget != "" {
			newCfg.XrayTarget = cfg.XrayTarget
		}
		if cfg.TgTarget != "" {
			newCfg.TgTarget = cfg.TgTarget
		}
		if cfg.PublicHostname != "" {
			newCfg.PublicHostname = cfg.PublicHostname
		}
		if cfg.XrayPublicHost != "" {
			newCfg.XrayPublicHost = cfg.XrayPublicHost
		}
		if cfg.TgPublicHost != "" {
			newCfg.TgPublicHost = cfg.TgPublicHost
		}
		if cfg.XrayPathPrefix != "" {
			newCfg.XrayPathPrefix = cfg.XrayPathPrefix
		}
	} else {
		newCfg = cfg
	}

	newSnap, err := d.buildSnapshot(newCfg)
	if err != nil {
		return err
	}

	if !d.running {
		d.cfg = newCfg
		d.snapshot.Store(newSnap)
		return nil
	}

	// Two-phase listener switch if address changed
	if newCfg.ListenAddr != d.cfg.ListenAddr {
		newLn, err := net.Listen("tcp", newCfg.ListenAddr)
		if err != nil {
			return fmt.Errorf("cdn dispatcher reconfigure bind %s: %w", newCfg.ListenAddr, err)
		}

		oldServer := d.server
		oldExited := d.exited

		newServer := &http.Server{
			Handler: d,
		}
		newExited := make(chan struct{})

		d.snapshot.Store(newSnap)
		d.cfg = newCfg
		d.server = newServer
		d.listener = newLn
		d.exited = newExited

		go func() {
			defer close(newExited)
			if err := newServer.Serve(newLn); err != nil && err != http.ErrServerClosed {
				log.Printf("[CDN-Dispatcher] Serve error: %v", err)
			}
			d.mu.Lock()
			if d.server == newServer {
				d.running = false
			}
			d.mu.Unlock()
		}()

		// Drain old server asynchronously
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			_ = oldServer.Shutdown(ctx)
			select {
			case <-oldExited:
			case <-ctx.Done():
				_ = oldServer.Close()
			}
		}()

		return nil
	}

	// Address didn't change: atomic snapshot swap
	d.cfg = newCfg
	d.snapshot.Store(newSnap)
	return nil
}

func (d *Dispatcher) Reconfigure(cfg Config) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.applyConfigLocked(cfg, modePatch)
}

// ApplyConfig performs full-state replacement of the dispatcher configuration, allowing fields like PublicHostname to be cleared.
func (d *Dispatcher) ApplyConfig(cfg Config) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.applyConfigLocked(cfg, modeReplace)
}

func (d *Dispatcher) UpdateConfig(cfg Config) {
	_ = d.Reconfigure(cfg)
}

// StartConfigured starts the dispatcher runtime (satisfies BootLifecycle).
func (d *Dispatcher) StartConfigured() error {
	return d.Start()
}

// ShutdownRuntime stops the dispatcher runtime (satisfies BootLifecycle).
func (d *Dispatcher) ShutdownRuntime(ctx context.Context) error {
	return d.Stop()
}

func (d *Dispatcher) IsRunning() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.running
}

func (d *Dispatcher) GetConfig() Config {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.cfg
}

package serverwizard

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"testing"
	"time"

	"github.com/hoaxisr/awg-manager/internal/sys/procnet"
	"github.com/hoaxisr/awg-manager/internal/tgwebproxy"
	"github.com/hoaxisr/awg-manager/internal/vlessserver"
)

type mockFileInfo struct {
	name  string
	size  int64
	isDir bool
}

func (m mockFileInfo) Name() string       { return m.name }
func (m mockFileInfo) Size() int64        { return m.size }
func (m mockFileInfo) Mode() fs.FileMode  { return 0755 }
func (m mockFileInfo) ModTime() time.Time { return time.Now() }
func (m mockFileInfo) IsDir() bool        { return m.isDir }
func (m mockFileInfo) Sys() interface{}   { return nil }

func TestPreflightEngine_TelegramChecks(t *testing.T) {
	xray := &mockXrayReader{}
	disp := &mockDispReader{}
	tg := &mockTgReader{
		st: tgwebproxy.Status{PID: 1234},
	}
	rec := &mockRecReader{required: false}

	engine := NewPreflightEngine(xray, disp, tg, rec, nil)

	// Mock file stats: all binaries exist
	engine.statFn = func(path string) (os.FileInfo, error) {
		return mockFileInfo{name: path, size: 1000}, nil
	}

	// Mock sockets: 8443 is free, 8085 is owned by tg (PID 1234)
	engine.findProcFn = func(procDir, addr string, port int) (procnet.ListenerLookup, error) {
		if port == 8443 {
			return procnet.ListenerLookup{SocketFound: false}, nil
		}
		if port == 8085 {
			return procnet.ListenerLookup{SocketFound: true, SocketInode: "999", PID: 1234}, nil
		}
		return procnet.ListenerLookup{SocketFound: false}, nil
	}

	ctx := context.Background()
	resp := engine.Run(ctx, WizardPlanRequest{
		Kind:         "tgwebproxy",
		DirectPort:   8443,
		ListenPort:   8085,
		CdnProfileID: "cdn_get",
	})

	if !resp.CanProceed {
		t.Fatalf("expected CanProceed=true, checks: %+v", resp.Checks)
	}

	// Now introduce port conflict on 8443 (PID 9999)
	engine.findProcFn = func(procDir, addr string, port int) (procnet.ListenerLookup, error) {
		if port == 8443 {
			return procnet.ListenerLookup{SocketFound: true, SocketInode: "100", PID: 9999}, nil
		}
		return procnet.ListenerLookup{SocketFound: false}, nil
	}

	resp = engine.Run(ctx, WizardPlanRequest{
		Kind:       "tgwebproxy",
		DirectPort: 8443,
	})

	if resp.CanProceed {
		t.Fatalf("expected CanProceed=false on port conflict")
	}

	// Missing binary telemt
	engine.statFn = func(path string) (os.FileInfo, error) {
		if path == "/opt/bin/telemt" {
			return nil, errors.New("file not found")
		}
		return mockFileInfo{name: path, size: 1000}, nil
	}

	resp = engine.Run(ctx, WizardPlanRequest{Kind: "tgwebproxy"})
	if resp.CanProceed {
		t.Fatalf("expected CanProceed=false on missing telemt binary")
	}

	// But if telemt exists at managed path, and scenario is direct_fake_tls without tproxy-server:
	engine.findProcFn = func(procDir, addr string, port int) (procnet.ListenerLookup, error) {
		return procnet.ListenerLookup{SocketFound: false}, nil
	}
	engine.statFn = func(path string) (os.FileInfo, error) {
		if path == "/opt/bin/telemt" || path == "/opt/bin/tproxy-server" {
			return nil, errors.New("file not found")
		}
		if path == "/opt/etc/awg-manager/telemt/telemt" {
			return mockFileInfo{name: path, size: 1000}, nil
		}
		return mockFileInfo{name: path, size: 1000}, nil
	}
	resp = engine.Run(ctx, WizardPlanRequest{
		Kind:       "tgwebproxy",
		Scenario:   "direct_fake_tls",
		DirectPort: 8443,
	})
	if !resp.CanProceed {
		t.Fatalf("expected CanProceed=true for direct_fake_tls with managed telemt binary, got %+v", resp.Checks)
	}
}

func TestPreflightEngine_XrayChecks(t *testing.T) {
	xray := &mockXrayReader{
		st: vlessserver.Status{Installed: true, PID: 5678, Version: "awgm-xhttpd 1.0.0-c"},
	}
	disp := &mockDispReader{}
	tg := &mockTgReader{}
	rec := &mockRecReader{required: false}

	engine := NewPreflightEngine(xray, disp, tg, rec, nil)

	// Stat returns xray exists
	engine.statFn = func(path string) (os.FileInfo, error) {
		return mockFileInfo{name: path, size: 5000000}, nil
	}

	// 1099 is listening (Mihomo active), 9008 is free, 9009 is free
	engine.findProcFn = func(procDir, addr string, port int) (procnet.ListenerLookup, error) {
		if port == 1099 {
			return procnet.ListenerLookup{SocketFound: true, PID: 111}, nil
		}
		return procnet.ListenerLookup{SocketFound: false}, nil
	}

	ctx := context.Background()
	resp := engine.Run(ctx, WizardPlanRequest{
		Kind:         "xray",
		PublicDomain: "cdn.myvpn.net",
		Path:         "/cdn-bridge/",
	})

	if !resp.CanProceed {
		t.Fatalf("expected CanProceed=true, got %+v", resp.Checks)
	}

	// Invalid domain
	resp = engine.Run(ctx, WizardPlanRequest{
		Kind:         "xray",
		PublicDomain: "bad domain name",
		Path:         "/cdn-bridge/",
	})
	if resp.CanProceed {
		t.Fatalf("expected CanProceed=false on invalid domain name")
	}

	// Legacy mihomo:1099 blocked in preflight (A2)
	resp = engine.Run(ctx, WizardPlanRequest{
		Kind:           "xray",
		PublicDomain:   "cdn.myvpn.net",
		Path:           "/cdn-bridge/",
		UpstreamDevice: "mihomo:1099",
	})
	if resp.CanProceed {
		t.Fatalf("expected CanProceed=false when legacy mihomo:1099 is requested")
	}

	// Recovery required
	rec.required = true
	rec.reason = "corrupt journal"
	resp = engine.Run(ctx, WizardPlanRequest{
		Kind:         "xray",
		PublicDomain: "cdn.myvpn.net",
		Path:         "/cdn-bridge/",
	})
	if resp.CanProceed {
		t.Fatalf("expected CanProceed=false when recovery required")
	}
}

func TestPreflightEngine_XHTTPDMandatoryAndNoLegacyXrayBinary(t *testing.T) {
	xray := &mockXrayReader{
		st: vlessserver.Status{Installed: true, PID: 5678, Version: "awgm-xhttpd 1.0.0-c"},
	}
	disp := &mockDispReader{}
	tg := &mockTgReader{}
	rec := &mockRecReader{required: false}

	engine := NewPreflightEngine(xray, disp, tg, rec, nil)
	// /opt/sbin/xray MUST NEVER be probed or required
	engine.statFn = func(path string) (os.FileInfo, error) {
		if path == "/opt/sbin/xray" {
			t.Fatalf("PreflightEngine must never stat legacy /opt/sbin/xray")
		}
		return mockFileInfo{name: path, size: 1000}, nil
	}
	engine.findProcFn = func(procDir, addr string, port int) (procnet.ListenerLookup, error) {
		return procnet.ListenerLookup{SocketFound: false}, nil
	}

	resp := engine.Run(context.Background(), WizardPlanRequest{
		Kind:         "xray",
		PublicDomain: "cdn.example.test",
		Path:         "/cdn-bridge/",
	})
	if !resp.CanProceed {
		t.Fatalf("expected CanProceed=true when awgm-xhttpd is installed without /opt/sbin/xray, got: %+v", resp.Checks)
	}

	// Missing / broken awgm-xhttpd must block preflight
	xray.st = vlessserver.Status{Installed: false, UnavailableReason: "awgm-xhttpd binary missing at /opt/sbin/awgm-xhttpd"}
	respMissing := engine.Run(context.Background(), WizardPlanRequest{
		Kind:         "xray",
		PublicDomain: "cdn.example.test",
		Path:         "/cdn-bridge/",
	})
	if respMissing.CanProceed {
		t.Fatalf("expected CanProceed=false when awgm-xhttpd is unavailable")
	}

	// WebSocket transport must block preflight
	xray.st = vlessserver.Status{Installed: true, Version: "awgm-xhttpd 1.0.0-c"}
	respWS := engine.Run(context.Background(), WizardPlanRequest{
		Kind:         "xray",
		PublicDomain: "cdn.example.test",
		Path:         "/cdn-bridge/",
		CdnProfileID: "cdn_ws",
		Mode:         "ws",
	})
	if respWS.CanProceed {
		t.Fatalf("expected CanProceed=false when WebSocket transport is requested for VLESS")
	}
}

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
	"github.com/hoaxisr/awg-manager/internal/xrayserver"
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
}

func TestPreflightEngine_XrayChecks(t *testing.T) {
	xray := &mockXrayReader{
		st: xrayserver.Status{PID: 5678},
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

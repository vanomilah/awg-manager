package openflux

import (
	"bytes"
	"compress/flate"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/hoaxisr/awg-manager/internal/storage"
)

func TestConfigValidation(t *testing.T) {
	cfg := DefaultConfig()
	if err := cfg.Validate(); err != nil {
		t.Fatalf("DefaultConfig should be valid: %v", err)
	}

	// Short secret
	cfg.Secret = "short"
	if err := cfg.Validate(); err == nil {
		t.Fatalf("expected error for short secret, got nil")
	}

	// Valid secret
	cfg.Secret = "1234567890123456"
	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected valid secret, got %v", err)
	}

	// Invalid direct port when direct is enabled
	cfg.Transports = append(cfg.Transports, "direct")
	cfg.DirectListen = "0.0.0.0:22"
	if err := cfg.Validate(); err == nil {
		t.Fatalf("expected error for reserved port 22, got nil")
	}

	cfg.DirectListen = "0.0.0.0:8445"
	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected valid direct listen, got %v", err)
	}

	// NAT mode tests
	cfg.NatMode = "none"
	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected valid natMode none, got %v", err)
	}
	cfg.NatMode = "internet-only"
	if err := cfg.Validate(); err == nil {
		t.Fatalf("expected error for internet-only without WAN, got nil")
	}
	cfg.NatStaticWANs = []string{"ISP"}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected valid internet-only with WAN, got %v", err)
	}
}

func TestBuildArgs(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Secret = "supersecretkey12345"
	cfg.Transports = []string{"mailru", "yandex", "direct"}
	cfg.MailruURL = "https://docs.mail.ru/sample"
	cfg.YandexURL = "https://docs.yandex.ru/sample"

	args := cfg.BuildArgs("/tmp/secret.key")
	argsStr := strings.Join(args, " ")

	if !strings.Contains(argsStr, "--role=exit") {
		t.Errorf("args missing --role=exit: %s", argsStr)
	}
	if !strings.Contains(argsStr, "--mode=l4") {
		t.Errorf("args missing --mode=l4: %s", argsStr)
	}
	if !strings.Contains(argsStr, "--negotiate") {
		t.Errorf("args missing --negotiate: %s", argsStr)
	}
	if !strings.Contains(argsStr, "--encryption-key-file=/tmp/secret.key") {
		t.Errorf("args missing --encryption-key-file: %s", argsStr)
	}
	if !strings.Contains(argsStr, "--direct-listen=0.0.0.0:8445") {
		t.Errorf("args missing --direct-listen: %s", argsStr)
	}
	if !strings.Contains(argsStr, "--mailru-url=https://docs.mail.ru/sample") {
		t.Errorf("args missing --mailru-url: %s", argsStr)
	}
	if !strings.Contains(argsStr, "--yandex-url=https://docs.yandex.ru/sample") {
		t.Errorf("args missing --yandex-url: %s", argsStr)
	}
}

func TestServerRoutingAndEgressArgs(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Secret = "supersecretkey12345"
	cfg.EgressInterface = "192.168.1.50"

	args := cfg.BuildArgs("/tmp/secret.key")
	argsStr := strings.Join(args, " ")
	if !strings.Contains(argsStr, "--local-ip=192.168.1.50") {
		t.Errorf("expected --local-ip=192.168.1.50, got %s", argsStr)
	}

	// In internet-only mode without explicit EgressInterface, fallback to static WAN IP if valid
	cfg2 := DefaultConfig()
	cfg2.Secret = "supersecretkey12345"
	cfg2.NatMode = "internet-only"
	cfg2.NatStaticWANs = []string{"192.168.1.100"}
	args2 := cfg2.BuildArgs("/tmp/secret.key")
	args2Str := strings.Join(args2, " ")
	if !strings.Contains(args2Str, "--local-ip=192.168.1.100") {
		t.Errorf("expected --local-ip=192.168.1.100 from NatStaticWANs, got %s", args2Str)
	}
}

func TestEncodeAndDecodeLink(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Name = "HomeTest"
	cfg.Secret = "supersecretkey12345"
	cfg.MailruURL = "https://docs.mail.ru/sample"

	link, err := GenerateLink(cfg, "192.168.90.1")
	if err != nil {
		t.Fatalf("GenerateLink failed: %v", err)
	}

	if !strings.HasPrefix(link, Prefix) {
		t.Fatalf("link does not start with %s: %s", Prefix, link)
	}

	// Decompress and verify JSON payload
	body := strings.TrimPrefix(link, Prefix)
	compressed, err := base64.RawURLEncoding.DecodeString(body)
	if err != nil {
		t.Fatalf("base64 decode failed: %v", err)
	}

	r := flate.NewReader(bytes.NewReader(compressed))
	decompressed, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("flate read failed: %v", err)
	}

	var parsed ShareConfig
	if err := json.Unmarshal(decompressed, &parsed); err != nil {
		t.Fatalf("json unmarshal failed: %v", err)
	}

	if parsed.Name != "HomeTest" {
		t.Errorf("expected name HomeTest, got %s", parsed.Name)
	}
	if parsed.Secret != "supersecretkey12345" {
		t.Errorf("expected secret match, got %s", parsed.Secret)
	}
	if !parsed.Negotiate {
		t.Errorf("expected Negotiate=true")
	}
	if len(parsed.Transports) == 0 {
		t.Errorf("expected at least one transport")
	}
	if parsed.Codec != "" {
		t.Errorf("expected empty codec for batched default, got %s", parsed.Codec)
	}

	// Legacy codec test
	cfgLegacy := cfg
	cfgLegacy.Codec = "legacy"
	linkLegacy, err := GenerateLink(cfgLegacy, "192.168.90.1")
	if err != nil {
		t.Fatalf("GenerateLink with legacy codec failed: %v", err)
	}
	bodyLegacy := strings.TrimPrefix(linkLegacy, Prefix)
	compressedLegacy, _ := base64.RawURLEncoding.DecodeString(bodyLegacy)
	rLegacy := flate.NewReader(bytes.NewReader(compressedLegacy))
	decompLegacy, _ := io.ReadAll(rLegacy)
	var parsedLegacy ShareConfig
	_ = json.Unmarshal(decompLegacy, &parsedLegacy)
	if parsedLegacy.Codec != "legacy" {
		t.Errorf("expected codec legacy, got %s", parsedLegacy.Codec)
	}

	// Single transport mode
	cfgSingle := DefaultConfig()
	cfgSingle.TransportMode = "single"
	cfgSingle.Transports = []string{"mailru"}
	cfgSingle.MailruURL = "https://docs.mail.ru/single"
	linkSingle, err := GenerateLink(cfgSingle, "192.168.90.1")
	if err != nil {
		t.Fatalf("GenerateLink single mode failed: %v", err)
	}
	bodySingle := strings.TrimPrefix(linkSingle, Prefix)
	compressedSingle, _ := base64.RawURLEncoding.DecodeString(bodySingle)
	rSingle := flate.NewReader(bytes.NewReader(compressedSingle))
	decompSingle, _ := io.ReadAll(rSingle)
	var parsedSingle ShareConfig
	_ = json.Unmarshal(decompSingle, &parsedSingle)
	if parsedSingle.Negotiate {
		t.Errorf("expected Negotiate=false for single transport")
	}
	if len(parsedSingle.Transports) != 1 || parsedSingle.Transports[0].Type != "mailru" {
		t.Errorf("expected 1 mailru transport in single mode, got %+v", parsedSingle.Transports)
	}

	// No transports error
	cfgEmpty := DefaultConfig()
	cfgEmpty.Transports = nil
	if _, err := GenerateLink(cfgEmpty, ""); err == nil {
		t.Errorf("expected error for empty transports, got nil")
	}
}

func TestNormalizeArch(t *testing.T) {
	if got := NormalizeArch("aarch64"); got != "aarch64-3.10" {
		t.Errorf("NormalizeArch(aarch64) = %s, want aarch64-3.10", got)
	}
	if got := NormalizeArch("arm64"); got != "aarch64-3.10" {
		t.Errorf("NormalizeArch(arm64) = %s, want aarch64-3.10", got)
	}
	if !IsArchSupported("aarch64") {
		t.Errorf("IsArchSupported(aarch64) = false, want true")
	}
}

func TestDecodeLinkAndClientConfigFromShare(t *testing.T) {
	srvCfg := DefaultConfig()
	srvCfg.Name = "KeeneticDacha"
	srvCfg.Secret = "supersecretkey12345"
	srvCfg.Transports = []string{"mailru", "yandex", "direct"}
	srvCfg.MailruURL = "https://docs.mail.ru/sample"
	srvCfg.YandexURL = "https://docs.yandex.ru/sample"

	link, err := GenerateLink(srvCfg, "203.0.113.10")
	if err != nil {
		t.Fatalf("GenerateLink failed: %v", err)
	}

	// Test Decode with extra whitespace/newlines
	sc, err := Decode("  \n" + link + "  \t")
	if err != nil {
		t.Fatalf("Decode failed: %v", err)
	}
	if sc.Name != "KeeneticDacha" {
		t.Errorf("expected Name KeeneticDacha, got %q", sc.Name)
	}
	if !sc.Negotiate {
		t.Errorf("expected Negotiate=true")
	}
	if sc.Secret != "supersecretkey12345" {
		t.Errorf("expected Secret match, got %q", sc.Secret)
	}

	base := DefaultClientConfig()
	base.InboundMode = "socks5"
	base.Socks5Listen = "127.0.0.1:1088"
	cliCfg := ClientConfigFromShare(sc, base)

	if cliCfg.Name != "KeeneticDacha" {
		t.Errorf("expected cliCfg.Name KeeneticDacha, got %q", cliCfg.Name)
	}
	if cliCfg.Socks5Listen != "127.0.0.1:1088" {
		t.Errorf("expected preserved Socks5Listen 127.0.0.1:1088, got %q", cliCfg.Socks5Listen)
	}
	if cliCfg.DirectDial != "203.0.113.10:8445" {
		t.Errorf("expected DirectDial 203.0.113.10:8445, got %q", cliCfg.DirectDial)
	}
	if cliCfg.MailruURL != "https://docs.mail.ru/sample" {
		t.Errorf("expected MailruURL, got %q", cliCfg.MailruURL)
	}
	if cliCfg.YandexURL != "https://docs.yandex.ru/sample" {
		t.Errorf("expected YandexURL, got %q", cliCfg.YandexURL)
	}
	if err := cliCfg.ValidateForStart(); err != nil {
		t.Errorf("expected converted ClientConfig to be valid for start: %v", err)
	}

	args := cliCfg.BuildArgs("/tmp/client.key")
	argsStr := strings.Join(args, " ")
	for _, expected := range []string{
		"--role=client",
		"--inbound=socks5",
		"--socks5=127.0.0.1:1088",
		"--negotiate",
		"--encryption-key-file=/tmp/client.key",
		"--transports=mailru:100,yandex:90,direct:50",
		"--direct-dial=203.0.113.10:8445",
		"--mailru-url=https://docs.mail.ru/sample",
		"--yandex-url=https://docs.yandex.ru/sample",
	} {
		if !strings.Contains(argsStr, expected) {
			t.Errorf("client BuildArgs missing %q in %s", expected, argsStr)
		}
	}

	// Test TUN mode build args (must NOT be downgraded to socks5, must pass real TUN flags)
	tunBase := DefaultClientConfig()
	tunBase.InboundMode = "tun"
	tunBase.TunIface = "opkgtun2"
	tunBase.TunAddr = "10.10.10.2/24"
	tunBase.TunMTU = 1280
	tunCfg := ClientConfigFromShare(sc, tunBase)
	tunArgs := tunCfg.BuildArgs("/tmp/client.key")
	tunArgsStr := strings.Join(tunArgs, " ")
	for _, expected := range []string{
		"--inbound=tun",
		"--tun-name=opkgtun2",
		"--tun-addr=10.10.10.2/24",
		"--tun-mtu=1280",
		"--transports=mailru:100,yandex:90,direct:50",
	} {
		if !strings.Contains(tunArgsStr, expected) {
			t.Errorf("expected TUN client args to have %q, got: %s", expected, tunArgsStr)
		}
	}
}

func TestClientConfigValidationEdgeCases(t *testing.T) {
	cfg := DefaultClientConfig()
	// Stopped default config without endpoints should pass structural Validate()
	if err := cfg.Validate(); err != nil {
		t.Fatalf("stopped DefaultClientConfig should pass Validate(): %v", err)
	}
	// But ValidateForStart() should fail when no transport endpoints or secret are set
	if err := cfg.ValidateForStart(); err == nil {
		t.Fatalf("expected error from ValidateForStart() on empty default config")
	}

	// Reserved port check (1099, 8443, 9090)
	for _, reservedPort := range []string{"127.0.0.1:1099", "127.0.0.1:8443", "127.0.0.1:9090"} {
		cfg.Socks5Listen = reservedPort
		if err := cfg.Validate(); err == nil {
			t.Fatalf("expected error for reserved port %s", reservedPort)
		}
	}
	cfg.Socks5Listen = "127.0.0.1:1085"

	// Same port for SOCKS5 and HTTP proxy
	cfg.HttpProxyListen = "127.0.0.1:1085"
	if err := cfg.Validate(); err == nil {
		t.Fatalf("expected error when Socks5Listen and HttpProxyListen use same port")
	}
	cfg.HttpProxyListen = "127.0.0.1:1086"

	// Telemost-only in session mode falls back to --transport=telemost --url=...
	telemostCfg := DefaultClientConfig()
	telemostCfg.TransportMode = "session"
	telemostCfg.Transports = []string{"telemost"}
	telemostCfg.TelemostURL = "https://telemost.yandex.ru/j/1234567890"
	telemostCfg.Secret = "1234567890123456"
	if err := telemostCfg.ValidateForStart(); err != nil {
		t.Fatalf("expected telemostCfg to pass ValidateForStart: %v", err)
	}
	telemostArgs := strings.Join(telemostCfg.BuildArgs("/tmp/client.key"), " ")
	if !strings.Contains(telemostArgs, "--transport=telemost") || !strings.Contains(telemostArgs, "--url=https://telemost.yandex.ru/j/1234567890") {
		t.Fatalf("expected telemost single-transport fallback args, got: %s", telemostArgs)
	}

	// Invalid link decode cases
	for _, bad := range []string{
		"",
		"http://example.com",
		"OPENFLUX://v1/abc",
		"openflux://v2/abc",
		"openflux://v1/!!!not_base64!!!",
	} {
		if _, err := Decode(bad); err == nil {
			t.Errorf("expected Decode(%q) to fail, got nil", bad)
		}
	}
}

func TestClientServiceConfigPersistenceAndStatus(t *testing.T) {
	ctx := t.Context()
	tmpDir := t.TempDir()
	svc := New(tmpDir, "aarch64", func(_ context.Context) string {
		return "198.51.100.7"
	})

	// Initial status should include Client with default port 1085
	st := svc.GetStatus(ctx)
	if st.Client == nil {
		t.Fatalf("expected st.Client to be non-nil")
	}
	if st.Client.Socks5Port != DefaultClientSocks5Port {
		t.Fatalf("expected default Socks5Port %d, got %d", DefaultClientSocks5Port, st.Client.Socks5Port)
	}

	// Generate a server link and parse it via ParseClientLink
	srvCfg := DefaultConfig()
	srvCfg.Name = "RouterExit"
	srvCfg.Secret = "0123456789abcdef0123456789abcdef"
	srvCfg.Transports = []string{"mailru", "yandex", "direct"}
	srvCfg.MailruURL = "https://docs.mail.ru/d/testdoc"
	if err := svc.SaveConfig(ctx, srvCfg); err != nil {
		t.Fatalf("SaveConfig failed: %v", err)
	}

	st = svc.GetStatus(ctx)
	if st.Link == "" {
		t.Fatalf("expected non-empty server link")
	}

	share, parsedClientCfg, err := svc.ParseClientLink(st.Link)
	if err != nil {
		t.Fatalf("ParseClientLink failed: %v", err)
	}
	if share.Name != "RouterExit" {
		t.Fatalf("expected share.Name RouterExit, got %q", share.Name)
	}
	if parsedClientCfg.DirectDial != "198.51.100.7:8445" {
		t.Fatalf("expected DirectDial 198.51.100.7:8445, got %q", parsedClientCfg.DirectDial)
	}

	parsedClientCfg.Socks5Listen = "127.0.0.1:1089"
	parsedClientCfg.HttpProxyListen = "127.0.0.1:1090"
	if err := svc.SaveClientConfig(ctx, parsedClientCfg); err != nil {
		t.Fatalf("SaveClientConfig failed: %v", err)
	}

	// Reload service from disk and verify client_config.json persistence
	svc2 := New(tmpDir, "aarch64", nil)
	cliSt := svc2.GetClientStatus()
	if cliSt.Socks5Port != 1089 {
		t.Fatalf("expected reloaded Socks5Port 1089, got %d", cliSt.Socks5Port)
	}
	if cliSt.Config == nil || cliSt.Config.Name != "RouterExit" {
		t.Fatalf("expected reloaded client config Name RouterExit, got %+v", cliSt.Config)
	}
	if cliSt.HttpURI != "http://127.0.0.1:1090" {
		t.Fatalf("expected HttpURI http://127.0.0.1:1090, got %q", cliSt.HttpURI)
	}

	// Now delete client and verify it resets and removes client_config.json
	if err := svc2.DeleteClient(ctx); err != nil {
		t.Fatalf("DeleteClient failed: %v", err)
	}
	delSt := svc2.GetClientStatus()
	if delSt.Config != nil {
		t.Fatalf("expected nil Config after DeleteClient, got %+v", delSt.Config)
	}
	if _, err := os.Stat(filepath.Join(tmpDir, "openflux", "client_config.json")); !os.IsNotExist(err) {
		t.Fatalf("expected client_config.json to be deleted, got err: %v", err)
	}
}

type mockRouterCmds struct {
	allocatedNdms string
	allocatedTun  string
	createdNdms   string
	createdDesc   string
	createdMtu    int
	createdAddr   string
	downNdms      string
	delNdms       string
	permittedPol  string
	deniedPol     string
	createErr     error
	permitErr     error
}

func (m *mockRouterCmds) AllocateTunInterface(ctx context.Context, preferred string) (string, string, error) {
	if m.allocatedNdms != "" {
		return m.allocatedNdms, m.allocatedTun, nil
	}
	ndms, tun := AutoDiscoverFreeTun(preferred)
	return ndms, tun, nil
}

func (m *mockRouterCmds) CreateInterface(ctx context.Context, ndmsName, desc string, mtu int, ipWithMask string, global bool, defaultRoute bool) error {
	m.createdNdms = ndmsName
	m.createdDesc = desc
	m.createdMtu = mtu
	m.createdAddr = ipWithMask
	if m.createErr != nil {
		return m.createErr
	}
	return nil
}

func (m *mockRouterCmds) DownInterface(ctx context.Context, ndmsName string) error {
	m.downNdms = ndmsName
	return nil
}

func (m *mockRouterCmds) DeleteInterface(ctx context.Context, ndmsName string) error {
	m.delNdms = ndmsName
	return nil
}

func (m *mockRouterCmds) PermitPolicy(ctx context.Context, policy, iface string) error {
	m.permittedPol = policy
	if m.permitErr != nil {
		return m.permitErr
	}
	return nil
}

func (m *mockRouterCmds) DenyPolicy(ctx context.Context, policy, iface string) error {
	m.deniedPol = policy
	return nil
}

func TestAutoDiscoverFreeTunAndTunAllocation(t *testing.T) {
	ctx := context.Background()

	// 1. AutoDiscoverFreeTun test - must never allocate OpkgTun0 or OpkgTun1 (reserved for router mode)
	ndms, tun := AutoDiscoverFreeTun("")
	if !strings.HasPrefix(ndms, "OpkgTun") || !strings.HasPrefix(tun, "opkgtun") {
		t.Fatalf("unexpected AutoDiscoverFreeTun output: %s, %s", ndms, tun)
	}
	if ndms == "OpkgTun0" || ndms == "OpkgTun1" {
		t.Fatalf("AutoDiscoverFreeTun allocated forbidden router mode index: %s", ndms)
	}

	// Preferred 0 or 1 must be rejected and fall back to safe non-router index >= 2
	ndms0, tun0 := AutoDiscoverFreeTun("OpkgTun0")
	if ndms0 == "OpkgTun0" || tun0 == "opkgtun0" {
		t.Fatalf("AutoDiscoverFreeTun must reject preferred OpkgTun0, got %s, %s", ndms0, tun0)
	}
	ndms1, tun1 := AutoDiscoverFreeTun("OpkgTun1")
	if ndms1 == "OpkgTun1" || tun1 == "opkgtun1" {
		t.Fatalf("AutoDiscoverFreeTun must reject preferred OpkgTun1, got %s, %s", ndms1, tun1)
	}

	ndms22, tun22 := AutoDiscoverFreeTun("OpkgTun22")
	if ndms22 != "OpkgTun22" || tun22 != "opkgtun22" {
		t.Fatalf("expected OpkgTun22/opkgtun22 for free preferred, got %s, %s", ndms22, tun22)
	}

	// 2. Service TUN allocation without routerCmds (fallback)
	tmpDir := t.TempDir()
	svc := New(tmpDir, "aarch64", nil)

	cfg := DefaultClientConfig()
	cfg.InboundMode = "tun"
	cfg.NdmsIface = ""
	cfg.TunIface = ""
	cfg.Policy = "Policy0"

	if err := svc.SaveClientConfig(ctx, cfg); err != nil {
		t.Fatalf("SaveClientConfig failed: %v", err)
	}

	st := svc.GetClientStatus()
	if st.NdmsIface == "" || !strings.HasPrefix(st.NdmsIface, "OpkgTun") || st.NdmsIface == "OpkgTun0" || st.NdmsIface == "OpkgTun1" {
		t.Fatalf("expected valid non-router auto-allocated NdmsIface, got %q", st.NdmsIface)
	}
	if st.TunIface == "" || !strings.HasPrefix(st.TunIface, "opkgtun") || st.TunIface == "opkgtun0" || st.TunIface == "opkgtun1" {
		t.Fatalf("expected valid non-router auto-allocated TunIface, got %q", st.TunIface)
	}
	if st.Policy != "Policy0" {
		t.Fatalf("expected Policy0, got %q", st.Policy)
	}

	// 3. Service TUN allocation with RouterCommands and policy switching
	mock := &mockRouterCmds{
		allocatedNdms: "OpkgTun7",
		allocatedTun:  "opkgtun7",
	}
	svc.SetRouterCommands(mock)

	cfg2 := DefaultClientConfig()
	cfg2.InboundMode = "tun"
	cfg2.NdmsIface = ""
	cfg2.TunIface = ""
	cfg2.Policy = "Policy1"

	if err := svc.SaveClientConfig(ctx, cfg2); err != nil {
		t.Fatalf("SaveClientConfig with mockRouterCmds failed: %v", err)
	}

	// When policy was Policy0 in cfg, and changed to Policy1 in cfg2, old Policy0 must be denied
	if mock.deniedPol != "Policy0" {
		t.Fatalf("expected old policy Policy0 to be denied on change, got %q", mock.deniedPol)
	}

	st2 := svc.GetClientStatus()
	if st2.NdmsIface != "OpkgTun7" {
		t.Fatalf("expected NdmsIface OpkgTun7 from routerCmds, got %q", st2.NdmsIface)
	}
	if st2.TunIface != "opkgtun7" {
		t.Fatalf("expected TunIface opkgtun7 from routerCmds, got %q", st2.TunIface)
	}
	if st2.Policy != "Policy1" {
		t.Fatalf("expected Policy1, got %q", st2.Policy)
	}

	// 4. Switching to socks5 mode cleans up tun interface and old policy
	cfg3 := cfg2
	cfg3.InboundMode = "socks5"
	cfg3.Policy = ""
	if err := svc.SaveClientConfig(ctx, cfg3); err != nil {
		t.Fatalf("SaveClientConfig with socks5 failed: %v", err)
	}
	if mock.deniedPol != "Policy1" {
		t.Fatalf("expected Policy1 to be denied on switch to socks5, got %q", mock.deniedPol)
	}
	if mock.downNdms != "OpkgTun7" {
		t.Fatalf("expected OpkgTun7 to be brought down on switch to socks5, got %q", mock.downNdms)
	}

	// 5. When interface changes from OpkgTun7 to OpkgTun14 in TUN mode, old OpkgTun7 must be cleaned up
	mock.downNdms = ""
	mock.delNdms = ""
	mock.allocatedNdms = "OpkgTun7"
	mock.allocatedTun = "opkgtun7"

	// First restore to OpkgTun7 in TUN mode
	cfg4 := cfg2
	cfg4.InboundMode = "tun"
	cfg4.NdmsIface = "OpkgTun7"
	cfg4.TunIface = "opkgtun7"
	if err := svc.SaveClientConfig(ctx, cfg4); err != nil {
		t.Fatalf("SaveClientConfig restore failed: %v", err)
	}

	// Now reallocate to OpkgTun14
	mock.downNdms = ""
	mock.delNdms = ""
	mock.allocatedNdms = "OpkgTun14"
	mock.allocatedTun = "opkgtun14"
	cfg5 := cfg4
	cfg5.NdmsIface = ""
	cfg5.TunIface = ""
	if err := svc.SaveClientConfig(ctx, cfg5); err != nil {
		t.Fatalf("SaveClientConfig with new iface failed: %v", err)
	}
	if mock.downNdms != "OpkgTun7" {
		t.Fatalf("expected old OpkgTun7 to be brought down on iface change, got %q", mock.downNdms)
	}
	if mock.delNdms != "OpkgTun7" {
		t.Fatalf("expected old OpkgTun7 to be deleted on iface change, got %q", mock.delNdms)
	}
}

func TestExtractLastErrorFromLog(t *testing.T) {
	// Case 1: Openflux help banner output after flag failure
	helpLog := `written by p1neappleXpress
flag provided but not defined: -tun-name
OpenFlux — Network stack research tool. TCP tunnel with pluggable transports.

USAGE
  openflux --role=<role> --transport=<type> [OPTIONS]

LOGGING
  -d, --debug=1                Packet movement
  -dd, --debug=2               Plus operational logs: sessions, carriers, handshakes, crypto, control, errors.
  -ddd, --debug=3              Plus hexdumps of packets and ciphertext.`

	got := extractLastErrorFromLog(helpLog)
	want := "flag provided but not defined: -tun-name"
	if got != want {
		t.Errorf("extractLastErrorFromLog(helpLog) = %q, want %q", got, want)
	}

	// Case 2: Platform unsupported error
	tunLog := `written by p1neappleXpress
utun: utun client is only supported on macOS
exit status 1`
	gotTun := extractLastErrorFromLog(tunLog)
	if !strings.Contains(gotTun, "utun client is only supported on macOS") {
		t.Errorf("extractLastErrorFromLog(tunLog) = %q, want utun error", gotTun)
	}

	// Case 3: Address bind error
	bindLog := `2026/10/14 07:39:22 [FATAL] listen tcp 127.0.0.1:1085: bind: address already in use`
	gotBind := extractLastErrorFromLog(bindLog)
	if !strings.Contains(gotBind, "bind: address already in use") {
		t.Errorf("extractLastErrorFromLog(bindLog) = %q, want bind error", gotBind)
	}

	// Case 4: Pure usage without error returns empty
	pureUsage := `USAGE
  openflux --role=<role> [OPTIONS]
  -dd, --debug=2 Plus operational logs: sessions, carriers, handshakes, crypto, control, errors.`
	gotPure := extractLastErrorFromLog(pureUsage)
	if gotPure != "" {
		t.Errorf("extractLastErrorFromLog(pureUsage) = %q, want empty string", gotPure)
	}

	// Case 5: DNS resolution failure in session mode
	dnsLog := `2026/10/14 18:30:00 [ERROR] transport dial: dial tcp: lookup docs.mail.ru: no such host`
	gotDNS := extractLastErrorFromLog(dnsLog)
	if !strings.Contains(gotDNS, "no such host") {
		t.Errorf("extractLastErrorFromLog(dnsLog) = %q, want DNS failure", gotDNS)
	}

	// Case 6: I/O timeout failure
	timeoutLog := `2026/10/14 18:31:00 [ERROR] transport dial: dial tcp 1.2.3.4:443: i/o timeout`
	gotTimeout := extractLastErrorFromLog(timeoutLog)
	if !strings.Contains(gotTimeout, "i/o timeout") {
		t.Errorf("extractLastErrorFromLog(timeoutLog) = %q, want timeout failure", gotTimeout)
	}

	// Case 7: TLS handshake failure
	tlsLog := `2026/10/14 18:32:00 [ERROR] tls: handshake failure`
	gotTLS := extractLastErrorFromLog(tlsLog)
	if !strings.Contains(gotTLS, "tls: handshake failure") {
		t.Errorf("extractLastErrorFromLog(tlsLog) = %q, want TLS failure", gotTLS)
	}
}

func TestParseStaleNatDeleteArgs(t *testing.T) {
	sampleOutput := `-P PREROUTING ACCEPT
-P POSTROUTING ACCEPT
-A POSTROUTING -o nwg0 -m comment --comment "AWGM_OPENFLUX" -j MASQUERADE
-A POSTROUTING -o ppp0 -m comment --comment "AWGM_OPENFLUX" -j MASQUERADE
-A POSTROUTING -o usb0 -m comment --comment "AWGM_OPENFLUX" -j MASQUERADE
-A POSTROUTING -o eth2.2 -m comment --comment "OTHER_RULE" -j MASQUERADE
`
	// Keep nwg0 and usb0. ppp0 rule must be deleted. OTHER_RULE must be ignored.
	keepWANs := []string{"nwg0", "usb0"}
	delArgs := ParseStaleNatDeleteArgs(sampleOutput, keepWANs)

	if len(delArgs) != 1 {
		t.Fatalf("expected 1 delete command for ppp0, got %d: %v", len(delArgs), delArgs)
	}

	cmd := delArgs[0]
	expected := []string{"-t", "nat", "-D", "POSTROUTING", "-o", "ppp0", "-m", "comment", "--comment", "AWGM_OPENFLUX", "-j", "MASQUERADE"}
	if len(cmd) != len(expected) {
		t.Fatalf("expected command length %d, got %d: %v", len(expected), len(cmd), cmd)
	}
	for i, arg := range expected {
		if cmd[i] != arg {
			t.Errorf("at index %d: expected %q, got %q", i, arg, cmd[i])
		}
	}
}

func TestClientTunNDMSRollback(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()

	binPath := filepath.Join(tmpDir, "openflux")
	if runtime.GOOS == "windows" {
		binPath = filepath.Join(tmpDir, "openflux.exe")
	}
	if err := os.WriteFile(binPath, []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
		t.Fatalf("failed to write dummy binary: %v", err)
	}

	svc := &Service{
		openfluxDir:   tmpDir,
		clientCfgPath: filepath.Join(tmpDir, "client_config.json"),
		clientKeyPath: filepath.Join(tmpDir, "client_secret.key"),
		clientPidPath: filepath.Join(tmpDir, "openflux_client.pid"),
		clientLogPath: filepath.Join(tmpDir, "openflux_client.log"),
		binPath:       binPath,
		clientConfig: ClientConfig{
			Enabled:       true,
			InboundMode:   "tun",
			NdmsIface:     "OpkgTun7",
			TunAddr:       "10.10.10.2/24",
			Policy:        "Policy0",
			TransportMode: "session",
			Transports:    []string{"direct"},
			DirectDial:    "1.2.3.4:8445",
			Secret:        "1234567890123456",
		},
	}

	mock := &mockRouterCmds{
		permitErr: os.ErrPermission,
	}
	svc.routerCmds = mock

	// Attempt starting client - PermitPolicy must fail and trigger DownInterface rollback
	err := svc.startClientProcess(ctx, true)
	if err == nil {
		t.Fatalf("expected startClientProcess to fail due to permitErr, got nil")
	}

	if mock.downNdms != "OpkgTun7" {
		t.Fatalf("expected rollback DownInterface to be called for OpkgTun7, got %q", mock.downNdms)
	}
}

func TestSecureZeroAndRemoveFile(t *testing.T) {
	tmpDir := t.TempDir()
	keyFile := filepath.Join(tmpDir, "secret.key")
	if err := os.WriteFile(keyFile, []byte("super-secret-key-123456\n"), 0600); err != nil {
		t.Fatal(err)
	}

	secureZeroAndRemoveFile(keyFile)

	if _, err := os.Stat(keyFile); !os.IsNotExist(err) {
		t.Fatalf("expected file to be removed, got err: %v", err)
	}
}

func TestEarlyCrashCancelsAutoReconnectAndWipesKey(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()

	binPath := filepath.Join(tmpDir, "openflux_failing")
	if runtime.GOOS == "windows" {
		binPath = filepath.Join(tmpDir, "openflux_failing.bat")
		_ = os.WriteFile(binPath, []byte("@echo off\r\necho fatal crash\r\nexit /b 1\r\n"), 0755)
	} else {
		_ = os.WriteFile(binPath, []byte("#!/bin/sh\necho fatal crash\nexit 1\n"), 0755)
	}

	keyPath := filepath.Join(tmpDir, "client_secret.key")
	svc := &Service{
		openfluxDir:   tmpDir,
		clientCfgPath: filepath.Join(tmpDir, "client_config.json"),
		clientKeyPath: keyPath,
		clientPidPath: filepath.Join(tmpDir, "openflux_client.pid"),
		clientLogPath: filepath.Join(tmpDir, "openflux_client.log"),
		binPath:       binPath,
		clientConfig: ClientConfig{
			Enabled:       true,
			AutoReconnect: true,
			InboundMode:   "socks5",
			TransportMode: "session",
			Secret:        "1234567890123456",
		},
	}

	err := svc.startClientProcess(ctx, true)
	if err == nil {
		t.Fatalf("expected startClientProcess to fail for failing binary, got nil")
	}

	// Verify clientStopCh is closed and set to nil to prevent AutoReconnect
	svc.mu.Lock()
	stopCh := svc.clientStopCh
	svc.mu.Unlock()
	if stopCh != nil {
		t.Fatalf("expected clientStopCh to be nil after early crash, got %v", stopCh)
	}

	// Verify key file was securely wiped and removed
	if _, statErr := os.Stat(keyPath); !os.IsNotExist(statErr) {
		t.Fatalf("expected key file %s to be deleted after start failure, got err: %v", keyPath, statErr)
	}
}

func TestStopProcessesWipesKeyFiles(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()

	serverKey := filepath.Join(tmpDir, "secret.key")
	clientKey := filepath.Join(tmpDir, "client_secret.key")

	_ = os.WriteFile(serverKey, []byte("server-secret-key-123456\n"), 0600)
	_ = os.WriteFile(clientKey, []byte("client-secret-key-123456\n"), 0600)

	svc := &Service{
		openfluxDir:   tmpDir,
		keyPath:       serverKey,
		pidPath:       filepath.Join(tmpDir, "openflux.pid"),
		clientKeyPath: clientKey,
		clientPidPath: filepath.Join(tmpDir, "openflux_client.pid"),
	}

	_ = svc.stopProcess(ctx)
	if _, err := os.Stat(serverKey); !os.IsNotExist(err) {
		t.Fatalf("expected server key file to be wiped on stopProcess, got err: %v", err)
	}

	_ = svc.stopClientProcess(ctx)
	if _, err := os.Stat(clientKey); !os.IsNotExist(err) {
		t.Fatalf("expected client key file to be wiped on stopClientProcess, got err: %v", err)
	}
}

type mockTunnelStore struct {
	tunnels map[string]*storage.AWGTunnel
}

func newMockTunnelStore() *mockTunnelStore {
	return &mockTunnelStore{tunnels: make(map[string]*storage.AWGTunnel)}
}

func (m *mockTunnelStore) Get(id string) (*storage.AWGTunnel, error) {
	if t, ok := m.tunnels[id]; ok {
		cp := *t
		return &cp, nil
	}
	return nil, os.ErrNotExist
}

func (m *mockTunnelStore) Exists(id string) bool {
	_, ok := m.tunnels[id]
	return ok
}

func (m *mockTunnelStore) Create(t *storage.AWGTunnel) error {
	cp := *t
	m.tunnels[t.ID] = &cp
	return nil
}

func (m *mockTunnelStore) Update(id string, mut func(*storage.AWGTunnel) error) error {
	t, ok := m.tunnels[id]
	if !ok {
		return os.ErrNotExist
	}
	return mut(t)
}

func (m *mockTunnelStore) Delete(id string) error {
	delete(m.tunnels, id)
	return nil
}

func TestTunnelStoreMirroring(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()

	svc := New(tmpDir, "aarch64", nil)
	store := newMockTunnelStore()
	svc.SetTunnelStore(store)

	// Initially, store is empty
	if store.Exists("openflux-client") {
		t.Fatalf("expected store to not have openflux-client initially")
	}

	// 1. Save client config with TUN mode
	cfg := DefaultClientConfig()
	cfg.Name = "MyOpenFlux"
	cfg.InboundMode = "tun"
	cfg.NdmsIface = "OpkgTun5"
	cfg.TunIface = "opkgtun5"
	cfg.TunAddr = "10.10.10.2/24"
	cfg.TunMTU = 1300
	cfg.Transports = []string{"mailru", "yandex"}
	cfg.MailruURL = "https://docs.mail.ru/sample"
	cfg.Secret = "1234567890123456"
	cfg.Enabled = true

	if err := svc.SaveClientConfig(ctx, cfg); err != nil {
		t.Fatalf("SaveClientConfig failed: %v", err)
	}

	// Store must now contain mirrored tunnel "openflux-client"
	if !store.Exists("openflux-client") {
		t.Fatalf("expected openflux-client mirror in store after SaveClientConfig")
	}

	tun, err := store.Get("openflux-client")
	if err != nil {
		t.Fatalf("failed to get openflux-client from store: %v", err)
	}
	if tun.Backend != "wdtt-raw" {
		t.Errorf("expected Backend=wdtt-raw, got %q", tun.Backend)
	}
	if tun.OpenFluxClientID != "default" {
		t.Errorf("expected OpenFluxClientID=default, got %q", tun.OpenFluxClientID)
	}
	if tun.RawNdmsIface != "OpkgTun5" || tun.RawKernelIface != "opkgtun5" {
		t.Errorf("expected OpkgTun5/opkgtun5, got %q/%q", tun.RawNdmsIface, tun.RawKernelIface)
	}
	if tun.Interface.Address != "10.10.10.2/24" || tun.Interface.MTU != 1300 {
		t.Errorf("expected 10.10.10.2/24 MTU 1300, got %q MTU %d", tun.Interface.Address, tun.Interface.MTU)
	}
	if tun.Enabled != true {
		t.Errorf("expected Enabled=true, got false")
	}

	// 2. Stop client
	if err := svc.StopClient(ctx); err != nil {
		t.Fatalf("StopClient failed: %v", err)
	}
	tun, _ = store.Get("openflux-client")
	if tun.Enabled != false {
		t.Errorf("expected Enabled=false after StopClient, got true")
	}

	// 3. Switch to socks5 mode -> mirror should be removed from AWGTunnelStore
	cfgSocks := cfg
	cfgSocks.InboundMode = "socks5"
	cfgSocks.Enabled = false
	if err := svc.SaveClientConfig(ctx, cfgSocks); err != nil {
		t.Fatalf("SaveClientConfig with socks5 failed: %v", err)
	}
	if store.Exists("openflux-client") {
		t.Errorf("expected openflux-client mirror to be removed when switched to socks5 mode")
	}

	// 4. Switch back to tun mode and delete
	cfg.Enabled = false
	if err := svc.SaveClientConfig(ctx, cfg); err != nil {
		t.Fatalf("SaveClientConfig failed: %v", err)
	}
	if !store.Exists("openflux-client") {
		t.Fatalf("expected openflux-client mirror recreated")
	}

	if err := svc.DeleteClient(ctx); err != nil {
		t.Fatalf("DeleteClient failed: %v", err)
	}
	if store.Exists("openflux-client") {
		t.Errorf("expected openflux-client mirror deleted after DeleteClient")
	}
}

func TestCloudFirstTransportPriorities(t *testing.T) {
	// Verify priority ordering: Mail.ru > Vyandex > Yandex > CupsOnline > Telemost > Direct
	if DefaultTransportPriority("mailru") != 100 {
		t.Errorf("expected mailru=100, got %d", DefaultTransportPriority("mailru"))
	}
	if DefaultTransportPriority("vyandex") != 95 {
		t.Errorf("expected vyandex=95, got %d", DefaultTransportPriority("vyandex"))
	}
	if DefaultTransportPriority("yandex") != 90 {
		t.Errorf("expected yandex=90, got %d", DefaultTransportPriority("yandex"))
	}
	if DefaultTransportPriority("cupsonline") != 80 {
		t.Errorf("expected cupsonline=80, got %d", DefaultTransportPriority("cupsonline"))
	}
	if DefaultTransportPriority("telemost") != 75 {
		t.Errorf("expected telemost=75, got %d", DefaultTransportPriority("telemost"))
	}
	if DefaultTransportPriority("direct") != 50 {
		t.Errorf("expected direct=50, got %d", DefaultTransportPriority("direct"))
	}

	// Verify DefaultConfig and DefaultClientConfig defaults
	srvDef := DefaultConfig()
	for _, tr := range srvDef.Transports {
		if tr == "direct" {
			t.Errorf("DefaultConfig must NOT include direct by default (cloud whitelist bypass first)")
		}
	}
	if srvDef.Transports[0] != "mailru" {
		t.Errorf("DefaultConfig expected mailru first, got %s", srvDef.Transports[0])
	}

	cliDef := DefaultClientConfig()
	for _, tr := range cliDef.Transports {
		if tr == "direct" {
			t.Errorf("DefaultClientConfig must NOT include direct by default")
		}
	}
	if cliDef.InboundMode != "tun" {
		t.Errorf("DefaultClientConfig InboundMode must default to 'tun', got %q", cliDef.InboundMode)
	}

	// Verify BuildArgs does NOT include direct if not configured
	srvCfg := DefaultConfig()
	srvCfg.Secret = "1234567890123456"
	srvCfg.MailruURL = "https://docs.mail.ru/sample"
	srvArgs := strings.Join(srvCfg.BuildArgs("/tmp/key"), " ")
	if strings.Contains(srvArgs, "direct") {
		t.Errorf("srvArgs must NOT contain direct when omitted from Transports: %s", srvArgs)
	}
	if strings.Contains(srvArgs, "--direct-listen") {
		t.Errorf("srvArgs must NOT contain --direct-listen when direct omitted: %s", srvArgs)
	}

	cliCfg := DefaultClientConfig()
	cliCfg.Secret = "1234567890123456"
	cliCfg.MailruURL = "https://docs.mail.ru/sample"
	cliArgs := strings.Join(cliCfg.BuildArgs("/tmp/key"), " ")
	if strings.Contains(cliArgs, "direct") {
		t.Errorf("cliArgs must NOT contain direct when omitted from Transports: %s", cliArgs)
	}
}

func TestDeterministicPrioritySorting(t *testing.T) {
	// 1. Config.BuildArgs sorting
	srvCfg := DefaultConfig()
	srvCfg.Secret = "1234567890123456"
	srvCfg.Transports = []string{"direct", "yandex", "mailru"}
	srvCfg.MailruURL = "https://docs.mail.ru/sample"
	srvCfg.YandexURL = "https://docs.yandex.ru/sample"
	srvArgs := srvCfg.BuildArgs("/tmp/key")

	foundSrvTransports := ""
	for _, a := range srvArgs {
		if strings.HasPrefix(a, "--transports=") {
			foundSrvTransports = strings.TrimPrefix(a, "--transports=")
			break
		}
	}
	expectedOrder := "mailru:100,yandex:90,direct:50"
	if foundSrvTransports != expectedOrder {
		t.Fatalf("expected srv --transports=%q, got %q", expectedOrder, foundSrvTransports)
	}

	// 2. ClientConfig.BuildArgs sorting
	cliCfg := DefaultClientConfig()
	cliCfg.Secret = "1234567890123456"
	cliCfg.Transports = []string{"direct", "yandex", "mailru"}
	cliCfg.DirectDial = "203.0.113.10:8445"
	cliCfg.MailruURL = "https://docs.mail.ru/sample"
	cliCfg.YandexURL = "https://docs.yandex.ru/sample"
	cliArgs := cliCfg.BuildArgs("/tmp/key")

	foundCliTransports := ""
	for _, a := range cliArgs {
		if strings.HasPrefix(a, "--transports=") {
			foundCliTransports = strings.TrimPrefix(a, "--transports=")
			break
		}
	}
	if foundCliTransports != expectedOrder {
		t.Fatalf("expected cli --transports=%q, got %q", expectedOrder, foundCliTransports)
	}

	// 3. GenerateLink sorting
	link, err := GenerateLink(srvCfg, "203.0.113.10:8445")
	if err != nil {
		t.Fatalf("GenerateLink failed: %v", err)
	}
	sc, err := Decode(link)
	if err != nil {
		t.Fatalf("Decode failed: %v", err)
	}
	if len(sc.Transports) != 3 {
		t.Fatalf("expected 3 transports in link, got %d", len(sc.Transports))
	}
	if sc.Transports[0].Type != "mailru" || sc.Transports[0].Priority != 100 {
		t.Errorf("expected 1st transport mailru:100, got %s:%d", sc.Transports[0].Type, sc.Transports[0].Priority)
	}
	if sc.Transports[1].Type != "yandex" || sc.Transports[1].Priority != 90 {
		t.Errorf("expected 2nd transport yandex:90, got %s:%d", sc.Transports[1].Type, sc.Transports[1].Priority)
	}
	if sc.Transports[2].Type != "direct" || sc.Transports[2].Priority != 50 {
		t.Errorf("expected 3rd transport direct:50, got %s:%d", sc.Transports[2].Type, sc.Transports[2].Priority)
	}
}

func TestWebDAVTransportIntegration(t *testing.T) {
	// 1. Priority check: webdav must be 110 (top cloud transport)
	if prio := DefaultTransportPriority("webdav"); prio != 110 {
		t.Fatalf("expected DefaultTransportPriority('webdav') == 110, got %d", prio)
	}
	if prio := DefaultTransportPriority("mailru"); prio != 100 {
		t.Fatalf("expected DefaultTransportPriority('mailru') == 100, got %d", prio)
	}

	// 2. Server Config.BuildArgs with WebDAV
	srvCfg := DefaultConfig()
	srvCfg.Secret = "1234567890123456"
	srvCfg.Transports = []string{"direct", "mailru", "webdav", "yandex"}
	srvCfg.WebdavURL = "https://webdav.cloud.mail.ru/openflux/"
	srvCfg.WebdavUser = "testuser@mail.ru"
	srvCfg.WebdavPassword = "secretWebdavPassword123"
	srvCfg.MailruURL = "https://docs.mail.ru/doc123"
	srvCfg.YandexURL = "https://docs.yandex.ru/doc456"
	srvCfg.DirectHost = "203.0.113.10"

	srvArgs := srvCfg.BuildArgs("/tmp/key")
	srvArgsStr := strings.Join(srvArgs, " ")

	expectedTransports := "--transports=webdav:110,mailru:100,yandex:90,direct:50"
	if !strings.Contains(srvArgsStr, expectedTransports) {
		t.Errorf("srvArgs missing %s: got %s", expectedTransports, srvArgsStr)
	}
	if !strings.Contains(srvArgsStr, "--webdav-url=https://webdav.cloud.mail.ru/openflux/") {
		t.Errorf("srvArgs missing --webdav-url: got %s", srvArgsStr)
	}
	if !strings.Contains(srvArgsStr, "--webdav-user=testuser@mail.ru") {
		t.Errorf("srvArgs missing --webdav-user: got %s", srvArgsStr)
	}
	if !strings.Contains(srvArgsStr, "--webdav-pass=secretWebdavPassword123") {
		t.Errorf("srvArgs missing --webdav-pass: got %s", srvArgsStr)
	}
	if !strings.Contains(srvArgsStr, "--session-context=https://webdav.cloud.mail.ru/openflux/") {
		t.Errorf("srvArgs missing webdav session context: got %s", srvArgsStr)
	}

	// 3. Client Config.BuildArgs with WebDAV
	cliCfg := DefaultClientConfig()
	cliCfg.Secret = "1234567890123456"
	cliCfg.Transports = []string{"direct", "webdav"}
	cliCfg.WebdavURL = "https://webdav.yandex.ru/openflux/"
	cliCfg.WebdavUser = "yandex_user"
	cliCfg.WebdavPassword = "yandex_password"
	cliCfg.DirectDial = "203.0.113.10:8445"

	cliArgs := cliCfg.BuildArgs("/tmp/cli_key")
	cliArgsStr := strings.Join(cliArgs, " ")

	expectedCliTransports := "--transports=webdav:110,direct:50"
	if !strings.Contains(cliArgsStr, expectedCliTransports) {
		t.Errorf("cliArgs missing %s: got %s", expectedCliTransports, cliArgsStr)
	}
	if !strings.Contains(cliArgsStr, "--webdav-url=https://webdav.yandex.ru/openflux/") {
		t.Errorf("cliArgs missing --webdav-url: got %s", cliArgsStr)
	}
	if !strings.Contains(cliArgsStr, "--webdav-user=yandex_user") {
		t.Errorf("cliArgs missing --webdav-user: got %s", cliArgsStr)
	}
	if !strings.Contains(cliArgsStr, "--webdav-pass=yandex_password") {
		t.Errorf("cliArgs missing --webdav-pass: got %s", cliArgsStr)
	}

	// 4. Client single transport mode
	cliSingle := DefaultClientConfig()
	cliSingle.TransportMode = "single"
	cliSingle.Transports = []string{"webdav"}
	cliSingle.WebdavURL = "https://webdav.cloud.mail.ru/openflux/"
	cliSingle.WebdavUser = "user@mail.ru"
	cliSingle.WebdavPassword = "app_pass_987"
	singleArgs := cliSingle.BuildArgs("")
	singleArgsStr := strings.Join(singleArgs, " ")
	if !strings.Contains(singleArgsStr, "--transport=webdav") {
		t.Errorf("expected --transport=webdav in single mode: got %s", singleArgsStr)
	}
	if !strings.Contains(singleArgsStr, "--url=https://webdav.cloud.mail.ru/openflux/") {
		t.Errorf("expected --url in single mode: got %s", singleArgsStr)
	}
	if !strings.Contains(singleArgsStr, "--webdav-user=user@mail.ru") {
		t.Errorf("expected --webdav-user in single mode: got %s", singleArgsStr)
	}
	if !strings.Contains(singleArgsStr, "--webdav-pass=app_pass_987") {
		t.Errorf("expected --webdav-pass in single mode: got %s", singleArgsStr)
	}

	// 5. GenerateLink and Decode with WebDAV
	link, err := GenerateLink(srvCfg, "203.0.113.10:8445")
	if err != nil {
		t.Fatalf("GenerateLink failed: %v", err)
	}
	sc, err := Decode(link)
	if err != nil {
		t.Fatalf("Decode failed: %v", err)
	}
	if len(sc.Transports) != 4 {
		t.Fatalf("expected 4 transports in link, got %d", len(sc.Transports))
	}
	if sc.Transports[0].Type != "webdav" || sc.Transports[0].Priority != 110 {
		t.Fatalf("expected 1st transport webdav:110, got %s:%d", sc.Transports[0].Type, sc.Transports[0].Priority)
	}
	if sc.Transports[0].URL != "https://webdav.cloud.mail.ru/openflux/" {
		t.Errorf("expected WebDAV URL in share transport, got %q", sc.Transports[0].URL)
	}
	if sc.Transports[0].User != "testuser@mail.ru" {
		t.Errorf("expected WebDAV User in share transport, got %q", sc.Transports[0].User)
	}
	if sc.Transports[0].Password != "secretWebdavPassword123" {
		t.Errorf("expected WebDAV Password in share transport, got %q", sc.Transports[0].Password)
	}

	// 6. ClientConfigFromShare converts link correctly into ClientConfig
	imported := ClientConfigFromShare(sc, DefaultClientConfig())
	if imported.WebdavURL != "https://webdav.cloud.mail.ru/openflux/" {
		t.Errorf("imported WebdavURL mismatch: %q", imported.WebdavURL)
	}
	if imported.WebdavUser != "testuser@mail.ru" {
		t.Errorf("imported WebdavUser mismatch: %q", imported.WebdavUser)
	}
	if imported.WebdavPassword != "secretWebdavPassword123" {
		t.Errorf("imported WebdavPassword mismatch: %q", imported.WebdavPassword)
	}

	// 7. ValidateForStart with only WebDAV configured
	webdavOnly := DefaultClientConfig()
	webdavOnly.Secret = "1234567890123456"
	webdavOnly.Transports = []string{"webdav"}
	webdavOnly.WebdavURL = "https://webdav.cloud.mail.ru/openflux/"
	if err := webdavOnly.ValidateForStart(); err != nil {
		t.Errorf("ValidateForStart failed with WebDAV configured: %v", err)
	}

	// 8. Sanitization of embedded credentials in WebdavURL
	secPass := "superSecretAppPassword999"
	secUser := "testowner@mail.ru"
	embeddedURL := fmt.Sprintf("https://%s:%s@webdav.cloud.mail.ru/openflux/", secUser, secPass)

	sanitizedCfg := DefaultConfig()
	sanitizedCfg.Secret = "1234567890123456"
	sanitizedCfg.Transports = []string{"webdav"}
	sanitizedCfg.WebdavURL = embeddedURL

	sanitizedArgs := sanitizedCfg.BuildArgs("")
	sanitizedArgsStr := strings.Join(sanitizedArgs, " ")
	for _, arg := range sanitizedArgs {
		if strings.HasPrefix(arg, "--session-context=") {
			if strings.Contains(arg, secPass) || strings.Contains(arg, "@") {
				t.Errorf("BuildArgs leaked credentials in session-context: %s", arg)
			}
		}
	}
	if !strings.Contains(sanitizedArgsStr, "--session-context=https://webdav.cloud.mail.ru/openflux/") {
		t.Errorf("expected clean session context without credentials: got %s", sanitizedArgsStr)
	}
	if !strings.Contains(sanitizedArgsStr, "--webdav-user="+secUser) {
		t.Errorf("expected extracted webdav-user: got %s", sanitizedArgsStr)
	}
	if !strings.Contains(sanitizedArgsStr, "--webdav-pass="+secPass) {
		t.Errorf("expected extracted webdav-pass: got %s", sanitizedArgsStr)
	}

	sanitizedLink, err := GenerateLink(sanitizedCfg, "")
	if err != nil {
		t.Fatalf("GenerateLink failed for embedded URL: %v", err)
	}
	decodedShare, err := Decode(sanitizedLink)
	if err != nil {
		t.Fatalf("Decode failed: %v", err)
	}
	if strings.Contains(decodedShare.Context, secPass) {
		t.Errorf("ShareConfig.Context leaked password: %q", decodedShare.Context)
	}
	if decodedShare.Context != "https://webdav.cloud.mail.ru/openflux/" {
		t.Errorf("expected clean ShareConfig.Context: got %q", decodedShare.Context)
	}
	if len(decodedShare.Transports) == 0 {
		t.Fatalf("expected at least 1 transport in decoded share")
	}
	if strings.Contains(decodedShare.Transports[0].URL, secPass) {
		t.Errorf("ShareTransport.URL leaked password: %q", decodedShare.Transports[0].URL)
	}
	if decodedShare.Transports[0].User != secUser || decodedShare.Transports[0].Password != secPass {
		t.Errorf("expected extracted user/pass in ShareTransport: got user=%q pass=%q", decodedShare.Transports[0].User, decodedShare.Transports[0].Password)
	}
}




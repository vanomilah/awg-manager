package openflux

import (
	"bytes"
	"compress/flate"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"strings"
	"testing"
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

	// Invalid direct port
	cfg.DirectListen = "0.0.0.0:22"
	if err := cfg.Validate(); err == nil {
		t.Fatalf("expected error for reserved port 22, got nil")
	}

	cfg.DirectListen = "0.0.0.0:8445"
	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected valid direct listen, got %v", err)
	}
}

func TestBuildArgs(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Secret = "supersecretkey12345"
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
		"--direct-dial=203.0.113.10:8445",
		"--mailru-url=https://docs.mail.ru/sample",
		"--yandex-url=https://docs.yandex.ru/sample",
	} {
		if !strings.Contains(argsStr, expected) {
			t.Errorf("client BuildArgs missing %q in %s", expected, argsStr)
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
}





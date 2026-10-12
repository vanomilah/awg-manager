package openflux

import (
	"bytes"
	"compress/flate"
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


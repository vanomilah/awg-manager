package xraybin

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var versionRegex = regexp.MustCompile(`Xray\s+([0-9]+)\.([0-9]+)\.([0-9]+)`)

// Capabilities describes the supported features of an installed Xray binary.
type Capabilities struct {
	RawVersion      string   `json:"raw_version"`
	Major           int      `json:"major"`
	Minor           int      `json:"minor"`
	Patch           int      `json:"patch"`
	SupportsXHTTP   bool     `json:"supports_xhttp"`
	SupportsReality bool     `json:"supports_reality"`
	SupportsMuxCool bool     `json:"supports_mux_cool"`
	Protocols       []string `json:"protocols"`
	Transports      []string `json:"transports"`
}

// InspectBinary executes `xray version` and extracts the feature capability matrix.
func InspectBinary(ctx context.Context, binPath string) (*Capabilities, error) {
	if binPath == "" {
		return nil, fmt.Errorf("binary path cannot be empty")
	}

	cmdCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	cmd := exec.CommandContext(cmdCtx, binPath, "version")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("execute %s version: %w (output: %s)", binPath, err, strings.TrimSpace(string(out)))
	}

	rawOut := string(out)
	matches := versionRegex.FindStringSubmatch(rawOut)
	if len(matches) < 4 {
		return &Capabilities{
			RawVersion:      strings.TrimSpace(rawOut),
			SupportsXHTTP:   true,
			SupportsReality: true,
			Protocols:       []string{"vless", "vmess", "trojan", "shadowsocks", "socks", "http", "freedom", "blackhole"},
			Transports:      []string{"tcp", "xhttp", "ws", "grpc", "httpupgrade"},
		}, nil
	}

	major, _ := strconv.Atoi(matches[1])
	minor, _ := strconv.Atoi(matches[2])
	patch, _ := strconv.Atoi(matches[3])

	// Feature support matrix:
	// REALITY: Xray >= 1.8.0
	// XHTTP: Xray >= 1.8.11 (or version 24+)
	supportsReality := major > 1 || (major == 1 && minor >= 8)
	supportsXHTTP := major >= 24 || (major == 1 && (minor > 8 || (minor == 8 && patch >= 11)))

	return &Capabilities{
		RawVersion:      fmt.Sprintf("%d.%d.%d", major, minor, patch),
		Major:           major,
		Minor:           minor,
		Patch:           patch,
		SupportsXHTTP:   supportsXHTTP,
		SupportsReality: supportsReality,
		SupportsMuxCool: true,
		Protocols:       []string{"vless", "vmess", "trojan", "shadowsocks", "socks", "http", "freedom", "blackhole"},
		Transports:      []string{"tcp", "xhttp", "ws", "grpc", "httpupgrade"},
	}, nil
}

// TestConfig runs `xray run -test -c configPath` to verify configuration syntax and runtime readiness.
func TestConfig(ctx context.Context, binPath, configPath string) error {
	cmdCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	cmd := exec.CommandContext(cmdCtx, binPath, "run", "-test", "-c", configPath)
	var outBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &outBuf

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("xray run -test failed: %w (output: %s)", err, strings.TrimSpace(outBuf.String()))
	}
	return nil
}

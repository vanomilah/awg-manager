package xraybin

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

type BinarySource string

const (
	SourcePackage  BinarySource = "opkg"     // Owned by opkg package xray-core
	SourceManaged  BinarySource = "managed"  // Managed by AWG Manager via manifest
	SourceExternal BinarySource = "external" // External / custom binary
)

type ManagedManifest struct {
	CanonicalPath   string `json:"canonical_path"`
	SHA256          string `json:"sha256"`
	Architecture    string `json:"architecture"`
	Version         string `json:"version"`
	InstallerSource string `json:"installer_source"`
	InstalledAt     string `json:"installed_at"`
}

type ResolvedBinary struct {
	Path         string       `json:"path"`
	Version      string       `json:"version"`
	Fingerprint  string       `json:"fingerprint"`
	Source       BinarySource `json:"source"`
	Installed    bool         `json:"installed"`
	CanUninstall bool         `json:"can_uninstall"`
	Blockers     []string     `json:"blockers,omitempty"`
}

type Resolver struct {
	dataDir        string
	opkgListFile   string
	customBinPaths []string
}

func NewResolver(dataDir string) *Resolver {
	return NewResolverWithPaths(dataDir, "/opt/lib/opkg/info/xray-core.list", []string{
		"/opt/bin/xray",
		"/opt/sbin/xray",
	})
}

func NewResolverWithPaths(dataDir, opkgListFile string, customBinPaths []string) *Resolver {
	return &Resolver{
		dataDir:        dataDir,
		opkgListFile:   opkgListFile,
		customBinPaths: customBinPaths,
	}
}

func (r *Resolver) manifestPath() string {
	return filepath.Join(r.dataDir, "xray", "managed-manifest.json")
}

// Resolve locates the active xray-core binary, identifies its origin, version, and fingerprint
func (r *Resolver) Resolve(ctx context.Context) ResolvedBinary {
	binPath := r.findBinaryPath()
	if binPath == "" {
		return ResolvedBinary{
			Installed: false,
			Source:    SourceExternal,
		}
	}

	fingerprint, _ := computeFileSHA256(binPath)
	version := r.detectVersion(ctx, binPath)
	source := r.classifySource(binPath, fingerprint)

	canUninstall, reason := r.CanUninstall(source)
	var blockers []string
	if !canUninstall && reason != "" {
		blockers = append(blockers, reason)
	}

	return ResolvedBinary{
		Path:         binPath,
		Version:      version,
		Fingerprint:  fingerprint,
		Source:       source,
		Installed:    true,
		CanUninstall: canUninstall,
		Blockers:     blockers,
	}
}

func isExecutable(fi os.FileInfo) bool {
	if runtime.GOOS == "windows" {
		return true
	}
	return fi.Mode()&0111 != 0
}

// IsXrayExecutableName reports whether base is an exact supported Xray binary filename ("xray" or "xray.exe").
func IsXrayExecutableName(base string) bool {
	lower := strings.ToLower(strings.TrimSpace(base))
	return lower == "xray" || lower == "xray.exe"
}

func (r *Resolver) findBinaryPath() string {
	// 1. Check if opkg package file list specifies a binary that exists
	if opkgBin := r.findOpkgBinary(); opkgBin != "" {
		return opkgBin
	}

	// 2. Check standard search paths
	for _, p := range r.customBinPaths {
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() && isExecutable(fi) {
			return p
		}
	}

	// 3. Fallback to system PATH
	if p, err := exec.LookPath("xray"); err == nil {
		return p
	}

	return ""
}

func (r *Resolver) findOpkgBinary() string {
	data, err := os.ReadFile(r.opkgListFile)
	if err != nil {
		return ""
	}
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		p := strings.TrimSpace(line)
		if IsXrayExecutableName(filepath.Base(p)) {
			if fi, err := os.Stat(p); err == nil && !fi.IsDir() && isExecutable(fi) {
				return p
			}
		}
	}
	return ""
}

func (r *Resolver) classifySource(binPath, fingerprint string) BinarySource {
	// Check if listed in opkg list
	if opkgBin := r.findOpkgBinary(); opkgBin != "" && filepath.Clean(opkgBin) == filepath.Clean(binPath) {
		return SourcePackage
	}

	// Check if valid manifest exists
	if manifestData, err := os.ReadFile(r.manifestPath()); err == nil {
		var m ManagedManifest
		if err := json.Unmarshal(manifestData, &m); err == nil {
			if filepath.Clean(m.CanonicalPath) == filepath.Clean(binPath) && m.SHA256 == fingerprint && fingerprint != "" {
				return SourceManaged
			}
		}
	}

	// Any other binary is external
	return SourceExternal
}

func (r *Resolver) detectVersion(ctx context.Context, binPath string) string {
	ctxTimeout, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctxTimeout, binPath, "version")
	out, err := cmd.Output()
	if err != nil {
		return "" // strictly no fictitious fallback version
	}

	lines := strings.Split(string(out), "\n")
	if len(lines) > 0 {
		fields := strings.Fields(lines[0])
		if len(fields) >= 2 && strings.EqualFold(fields[0], "xray") {
			return fields[1]
		}
	}
	return ""
}

func (r *Resolver) PackageInstalled() bool {
	cmd := exec.Command("/opt/bin/opkg", "status", "xray-core")
	out, err := cmd.Output()
	if err != nil {
		// Fallback check: does the list file exist?
		_, statErr := os.Stat(r.opkgListFile)
		return statErr == nil
	}
	return strings.Contains(string(out), "Status: install user installed") ||
		strings.Contains(string(out), "Status: install ok installed")
}

func (r *Resolver) CanUninstall(source BinarySource) (bool, string) {
	switch source {
	case SourcePackage:
		return true, "opkg"
	case SourceManaged:
		return true, "manifest"
	case SourceExternal:
		return false, "Внешний бинарник управляется вне AWG Manager и не может быть удалён"
	default:
		return false, "Неизвестный источник бинарника"
	}
}

func (r *Resolver) Uninstall(ctx context.Context, source BinarySource) error {
	switch source {
	case SourcePackage:
		cmd := exec.CommandContext(ctx, "/opt/bin/opkg", "remove", "xray-core")
		out, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("opkg remove xray-core: %s (%w)", string(out), err)
		}
		return nil
	case SourceManaged:
		binPath := r.findBinaryPath()
		if binPath != "" {
			_ = os.Remove(binPath)
		}
		_ = os.Remove(r.manifestPath())
		return nil
	default:
		return fmt.Errorf("cannot uninstall binary with source %s", source)
	}
}

var defaultResolver = NewResolver("/opt/etc/awg-manager")

func Resolve() ResolvedBinary {
	return defaultResolver.Resolve(context.Background())
}

func Uninstall(res ResolvedBinary) error {
	return defaultResolver.Uninstall(context.Background(), res.Source)
}

func computeFileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

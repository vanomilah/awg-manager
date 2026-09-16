package xraybin

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestResolver_PackageSource(t *testing.T) {
	tmpDir := t.TempDir()
	binPath := filepath.Join(tmpDir, "xray")
	_ = os.WriteFile(binPath, []byte("#!/bin/sh\necho 'Xray 26.7.28 (test)'\n"), 0755)

	opkgList := filepath.Join(tmpDir, "xray-core.list")
	_ = os.WriteFile(opkgList, []byte(binPath+"\n"), 0644)

	res := NewResolver(tmpDir)
	res.opkgListFile = opkgList
	res.customBinPaths = []string{binPath}

	rb := res.Resolve(context.Background())
	if !rb.Installed {
		t.Fatalf("expected binary to be detected as installed")
	}
	if rb.Source != SourcePackage {
		t.Errorf("expected SourcePackage, got %s", rb.Source)
	}
	canUn, method := res.CanUninstall(rb.Source)
	if !canUn || method != "opkg" {
		t.Errorf("expected CanUninstall=true with method opkg, got %v (%s)", canUn, method)
	}
}

func TestResolver_ManagedSource(t *testing.T) {
	tmpDir := t.TempDir()
	binDir := filepath.Join(tmpDir, "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatalf("mkdir bin: %v", err)
	}
	binPath := filepath.Join(binDir, "xray")
	binContent := []byte("#!/bin/sh\necho 'Xray 26.7.28 (managed)'\n")
	if err := os.WriteFile(binPath, binContent, 0755); err != nil {
		t.Fatalf("write bin: %v", err)
	}

	fingerprint, err := computeFileSHA256(binPath)
	if err != nil {
		t.Fatalf("compute sha: %v", err)
	}

	xrayDir := filepath.Join(tmpDir, "xray")
	if err := os.MkdirAll(xrayDir, 0755); err != nil {
		t.Fatalf("mkdir xray: %v", err)
	}
	manifest := ManagedManifest{
		CanonicalPath: binPath,
		SHA256:        fingerprint,
		Architecture:  "arm64",
		Version:       "26.7.28",
	}
	mData, _ := json.Marshal(manifest)
	if err := os.WriteFile(filepath.Join(xrayDir, "managed-manifest.json"), mData, 0644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	res := NewResolver(tmpDir)
	res.opkgListFile = filepath.Join(tmpDir, "nonexistent.list")
	res.customBinPaths = []string{binPath}

	rb := res.Resolve(context.Background())
	if rb.Source != SourceManaged {
		t.Errorf("expected SourceManaged, got %s", rb.Source)
	}
	canUn, method := res.CanUninstall(rb.Source)
	if !canUn || method != "manifest" {
		t.Errorf("expected CanUninstall=true with method manifest, got %v (%s)", canUn, method)
	}
}

func TestResolver_ExternalSource(t *testing.T) {
	tmpDir := t.TempDir()
	binPath := filepath.Join(tmpDir, "xray")
	_ = os.WriteFile(binPath, []byte("#!/bin/sh\necho 'Xray 26.7.28 (external)'\n"), 0755)

	res := NewResolver(tmpDir)
	res.opkgListFile = filepath.Join(tmpDir, "nonexistent.list")
	res.customBinPaths = []string{binPath}

	rb := res.Resolve(context.Background())
	if rb.Source != SourceExternal {
		t.Errorf("expected SourceExternal, got %s", rb.Source)
	}
	canUn, _ := res.CanUninstall(rb.Source)
	if canUn {
		t.Errorf("expected CanUninstall=false for external binary")
	}
}

func TestResolver_NoFictitiousVersion(t *testing.T) {
	tmpDir := t.TempDir()
	binPath := filepath.Join(tmpDir, "xray")
	// Broken script that fails execution
	_ = os.WriteFile(binPath, []byte("#!/bin/sh\nexit 1\n"), 0755)

	res := NewResolver(tmpDir)
	res.opkgListFile = filepath.Join(tmpDir, "nonexistent.list")
	res.customBinPaths = []string{binPath}

	rb := res.Resolve(context.Background())
	if rb.Version != "" {
		t.Errorf("expected empty version string on version detect failure, got %q", rb.Version)
	}
}

func TestIsXrayExecutableName(t *testing.T) {
	valid := []string{"xray", "xray.exe", "XRAY", "Xray.Exe", "  xray  "}
	for _, v := range valid {
		if !IsXrayExecutableName(v) {
			t.Errorf("expected %q to be valid Xray executable name", v)
		}
	}

	invalid := []string{"notxray", "xray-wrapper", "fake-xray-daemon", "other", "xray_bin", "xray1", "", "   "}
	for _, inv := range invalid {
		if IsXrayExecutableName(inv) {
			t.Errorf("expected %q to NOT be valid Xray executable name", inv)
		}
	}
}


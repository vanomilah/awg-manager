package updater

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hoaxisr/awg-manager/internal/downloader"
)

type fakeDownloader struct {
	readAllFn      func(ctx context.Context, req downloader.Request) ([]byte, downloader.ResponseMeta, error)
	downloadFileFn func(ctx context.Context, req downloader.FileRequest) (downloader.FileResult, error)
}

func TestIPKFilenameFromURL_RejectsUnsafeShellCharacters(t *testing.T) {
	cases := []string{
		"http://repo.local/a/awg-manager_2.12.0_aarch64-3.10-kn.ipk%3Btouch",
		"http://repo.local/a/awg-manager_2.12.0_aarch64-3.10-kn.ipk%26x",
		"http://repo.local/a/awg-manager_2.12.0_aarch64-3.10-kn.ipk%7Cx",
		"http://repo.local/a/awg-manager_2.12.0_aarch64-3.10-kn.ipk%24%28x%29",
		"http://repo.local/a/awg-manager_2.12.0_aarch64-3.10-kn.ipk%60x%60",
		"http://repo.local/a/awg-manager%202.12.0.ipk",
		"http://repo.local/a/../awg-manager_2.12.0.ipk",
		"http://repo.local/a/other-package_1.0.0.ipk",
		"http://repo.local/a/awg-manager_2.12.0_aarch64-3.10-kn.txt",
	}
	for _, raw := range cases {
		name, err := ipkFilenameFromURL(raw)
		if err == nil {
			t.Fatalf("expected reject for %q, got name=%q", raw, name)
		}
	}
}

func (f *fakeDownloader) ReadAll(ctx context.Context, req downloader.Request) ([]byte, downloader.ResponseMeta, error) {
	if f.readAllFn != nil {
		return f.readAllFn(ctx, req)
	}
	return nil, downloader.ResponseMeta{}, errors.New("readall not implemented")
}

func (f *fakeDownloader) DownloadFile(ctx context.Context, req downloader.FileRequest) (downloader.FileResult, error) {
	if f.downloadFileFn != nil {
		return f.downloadFileFn(ctx, req)
	}
	return downloader.FileResult{}, errors.New("downloadfile not implemented")
}

func TestCheckWithDownloader_UsesDownloaderRequest(t *testing.T) {
	arch := archSuffix()
	ipkName := "awg-manager_9.9.9_" + arch + "-kn.ipk"
	packages := "Package: awg-manager\nVersion: 9.9.9\nFilename: " + ipkName + "\n"
	var seen downloader.Request
	dl := &fakeDownloader{
		readAllFn: func(_ context.Context, req downloader.Request) ([]byte, downloader.ResponseMeta, error) {
			seen = req
			return gzipBytes(t, packages), downloader.ResponseMeta{StatusCode: http.StatusOK}, nil
		},
	}

	info := checkWithDownloader(context.Background(), "2.0.0", channelStable, dl)
	if !info.Available {
		t.Fatalf("expected update available, got %+v", info)
	}
	if seen.Purpose != "awgm-update-check" {
		t.Fatalf("purpose = %q, want awgm-update-check", seen.Purpose)
	}
	if seen.Timeout != repoTimeout {
		t.Fatalf("timeout = %s, want %s", seen.Timeout, repoTimeout)
	}
	if seen.MaxBodyBytes != packagesMaxBytes {
		t.Fatalf("max body bytes = %d, want %d", seen.MaxBodyBytes, packagesMaxBytes)
	}
}

func TestChangelogFetcher_UsesDownloaderRequest(t *testing.T) {
	const md = "## [1.0.0] - 2026-01-01\n\n### Added\n- item\n"
	var seen downloader.Request
	dl := &fakeDownloader{
		readAllFn: func(_ context.Context, req downloader.Request) ([]byte, downloader.ResponseMeta, error) {
			seen = req
			return []byte(md), downloader.ResponseMeta{StatusCode: http.StatusOK}, nil
		},
	}
	f := newChangelogFetcher("http://repo.local/CHANGELOG.md", repoTimeout, dl)
	entries, err := f.Fetch(context.Background())
	if err != nil {
		t.Fatalf("fetch changelog: %v", err)
	}
	if entries["1.0.0"].Version != "1.0.0" {
		t.Fatalf("unexpected entries: %+v", entries)
	}
	if seen.Purpose != "awgm-changelog" {
		t.Fatalf("purpose = %q, want awgm-changelog", seen.Purpose)
	}
	if seen.MaxBodyBytes != changelogMaxBytes {
		t.Fatalf("max body bytes = %d, want %d", seen.MaxBodyBytes, changelogMaxBytes)
	}
}

func TestUpgradeWithDownloader_UsesFileRequest(t *testing.T) {
	// downloadDir is constant; verify destination suffix and purpose.
	var seen downloader.FileRequest
	dl := &fakeDownloader{
		downloadFileFn: func(_ context.Context, req downloader.FileRequest) (downloader.FileResult, error) {
			seen = req
			return downloader.FileResult{Path: req.DestPath, Size: 123}, nil
		},
	}

	oldStart := startDetachedUpgrade
	startDetachedUpgrade = func(_ string) error { return nil }
	t.Cleanup(func() { startDetachedUpgrade = oldStart })

	url := "http://repo.local/aarch64-k3.10/awg-manager_2.12.0_aarch64-3.10-kn.ipk?token=x"
	if err := upgradeWithDownloader(context.Background(), url, dl); err != nil {
		t.Fatalf("upgradeWithDownloader: %v", err)
	}
	if seen.Request.Purpose != "awgm-update-ipk" {
		t.Fatalf("purpose = %q, want awgm-update-ipk", seen.Request.Purpose)
	}
	if seen.Request.Timeout != downloadTimeout {
		t.Fatalf("timeout = %s, want %s", seen.Request.Timeout, downloadTimeout)
	}
	if seen.MaxFileBytes != ipkMaxBytes {
		t.Fatalf("max file bytes = %d, want %d", seen.MaxFileBytes, ipkMaxBytes)
	}
	if !strings.HasPrefix(filepath.Clean(seen.DestPath), filepath.Clean(downloadDir)+string(os.PathSeparator)) {
		t.Fatalf("dest path = %q, want inside %q", seen.DestPath, downloadDir)
	}
	if filepath.Base(seen.DestPath) != "awg-manager_2.12.0_aarch64-3.10-kn.ipk" {
		t.Fatalf("dest filename = %q, want awg-manager_2.12.0_aarch64-3.10-kn.ipk", filepath.Base(seen.DestPath))
	}
}

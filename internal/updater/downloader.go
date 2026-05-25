package updater

import (
	"bytes"
	"context"

	"github.com/hoaxisr/awg-manager/internal/downloader"
)

const (
	packagesMaxBytes  int64 = 2 << 20
	changelogMaxBytes int64 = 512 << 10
	ipkMaxBytes       int64 = 64 << 20
)

type Downloader interface {
	ReadAll(ctx context.Context, req downloader.Request) ([]byte, downloader.ResponseMeta, error)
	DownloadFile(ctx context.Context, req downloader.FileRequest) (downloader.FileResult, error)
}

func newDefaultDownloader() Downloader {
	return downloader.NewService(downloader.Deps{})
}

func fetchLatestPackageWithDownloader(ctx context.Context, dl Downloader, pkgsURL, packageName string, cmp func(a, b string) int) (PackageEntry, error) {
	if dl == nil {
		dl = newDefaultDownloader()
	}
	body, _, err := dl.ReadAll(ctx, downloader.Request{
		Purpose:      "awgm-update-check",
		URL:          pkgsURL,
		MaxBodyBytes: packagesMaxBytes,
		Timeout:      repoTimeout,
	})
	if err != nil {
		return PackageEntry{}, err
	}
	return parsePackagesGz(bytes.NewReader(body), packageName, cmp)
}

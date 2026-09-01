package installer

import (
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"

	"github.com/hoaxisr/awg-manager/internal/logging"
)

const DefaultBinaryPath = "/opt/etc/awg-manager/mihomo/mihomo"

type Installer struct {
	binaryPath string
	arch       string
	spec       BinarySpec
	appLog     *logging.ScopedLogger
	shaMu      sync.Mutex
}

func New(binaryPath, arch string, spec BinarySpec, appLog logging.AppLogger) *Installer {
	return &Installer{
		binaryPath: binaryPath,
		arch:       arch,
		spec:       spec,
		appLog:     logging.NewScopedLogger(appLog, logging.GroupSystem, "mihomo-installer"),
	}
}

func (i *Installer) IsInstalled() bool {
	info, err := os.Stat(i.binaryPath)
	return err == nil && info.Mode().IsRegular() && (info.Mode().Perm()&0111 != 0)
}

func (i *Installer) Install(ctx context.Context) error {
	i.appLog.Info("install", i.arch, fmt.Sprintf("Downloading Mihomo v%s from %s", i.spec.Version, i.spec.URL))

	req, err := http.NewRequestWithContext(ctx, "GET", i.spec.URL, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Errorf("bad status: %d", resp.StatusCode)
	}

	var reader io.Reader = resp.Body
	if filepath.Ext(i.spec.URL) == ".gz" {
		gz, err := gzip.NewReader(resp.Body)
		if err != nil {
			return fmt.Errorf("gzip reader: %w", err)
		}
		defer gz.Close()
		reader = gz
	}

	tmpPath := i.binaryPath + ".tmp"
	os.MkdirAll(filepath.Dir(i.binaryPath), 0755)

	out, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, reader); err != nil {
		os.Remove(tmpPath)
		return err
	}
	out.Close()

	if err := os.Rename(tmpPath, i.binaryPath); err != nil {
		os.Remove(tmpPath)
		return err
	}

	i.appLog.Info("install", i.arch, "Mihomo installed successfully")
	return nil
}

func (i *Installer) Remove(ctx context.Context) error {
	err := os.Remove(i.binaryPath)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

package managed

import (
	"context"
	"fmt"
	"strings"

	"github.com/hoaxisr/awg-manager/internal/sys/exec"
)

const wgBin = "/opt/sbin/wg"

// wgRunner is the indirection seam for tests. Production wires
// realWgRunner (which calls internal/sys/exec.Run on wgBin); tests pass
// stubs without forking real binaries.
type wgRunner func(ctx context.Context, name string, args ...string) (string, error)

func realWgRunner(ctx context.Context, name string, args ...string) (string, error) {
	result, err := exec.Run(ctx, name, args...)
	if err != nil {
		return "", fmt.Errorf("%s: %w", name, exec.FormatError(result, err))
	}
	return result.Stdout, nil
}

func readKernelPrivateKeyWith(ctx context.Context, kernelName string, run wgRunner) (string, error) {
	if kernelName == "" {
		return "", fmt.Errorf("readKernelPrivateKey: empty kernel name")
	}
	out, err := run(ctx, wgBin, "show", kernelName, "private-key")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

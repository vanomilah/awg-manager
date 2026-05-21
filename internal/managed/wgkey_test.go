package managed

import (
	"context"
	"errors"
	"testing"
)

func TestReadKernelPrivateKey_ParsesBase64(t *testing.T) {
	stub := func(ctx context.Context, name string, args ...string) (string, error) {
		if name != wgBin || len(args) != 3 || args[0] != "show" || args[1] != "nwg63" || args[2] != "private-key" {
			t.Fatalf("unexpected argv: %s %v", name, args)
		}
		return "yKOZJtI2nQbSWzo8zvmBSjjnSkn89AkLXbekWTgKQ08=\n", nil
	}
	key, err := readKernelPrivateKeyWith(context.Background(), "nwg63", stub)
	if err != nil {
		t.Fatalf("readKernelPrivateKey: %v", err)
	}
	if key != "yKOZJtI2nQbSWzo8zvmBSjjnSkn89AkLXbekWTgKQ08=" {
		t.Errorf("key: got %q", key)
	}
}

func TestReadKernelPrivateKey_PropagatesError(t *testing.T) {
	stub := func(ctx context.Context, name string, args ...string) (string, error) {
		return "", errors.New("Unable to access interface: No such device")
	}
	_, err := readKernelPrivateKeyWith(context.Background(), "nwg99", stub)
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
}

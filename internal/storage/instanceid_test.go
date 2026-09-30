package storage

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadOrCreateInstanceID(t *testing.T) {
	dir := t.TempDir()
	id, err := LoadOrCreateInstanceID(dir)
	if err != nil || len(id) != 32 {
		t.Fatalf("id=%q err=%v", id, err)
	}
	again, err := LoadOrCreateInstanceID(dir)
	if err != nil || again != id {
		t.Fatalf("ID не устойчив: %q → %q", id, again)
	}
	if err := os.WriteFile(filepath.Join(dir, InstanceIDFile), []byte("garbage"), 0o644); err != nil {
		t.Fatal(err)
	}
	fresh, err := LoadOrCreateInstanceID(dir)
	if err != nil || len(fresh) != 32 || fresh == id {
		t.Fatalf("негодный ID не заменён: %q", fresh)
	}
}

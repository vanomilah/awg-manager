package procnet

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestIPToProcHex(t *testing.T) {
	if hex := IPToProcHex("127.0.0.1"); hex != "0100007F" {
		t.Errorf("expected 0100007F for 127.0.0.1, got %s", hex)
	}
	if hex := IPToProcHex("0.0.0.0"); hex != "00000000" {
		t.Errorf("expected 00000000 for 0.0.0.0, got %s", hex)
	}
	if hex := IPToProcHex("::1"); hex != "00000000000000000000000001000000" {
		t.Errorf("expected 00000000000000000000000001000000 for ::1, got %s", hex)
	}
	if hex := IPToProcHex("invalid"); hex != "" {
		t.Errorf("expected empty string for invalid IP, got %s", hex)
	}
}

func TestIsIPv6Target(t *testing.T) {
	if IsIPv6Target("127.0.0.1") {
		t.Errorf("expected 127.0.0.1 to NOT be IPv6")
	}
	if IsIPv6Target("0.0.0.0") {
		t.Errorf("expected 0.0.0.0 to NOT be IPv6")
	}
	if IsIPv6Target("") {
		t.Errorf("expected empty to NOT be IPv6")
	}
	if !IsIPv6Target("::1") {
		t.Errorf("expected ::1 to be IPv6")
	}
	if !IsIPv6Target("::") {
		t.Errorf("expected :: to be IPv6")
	}
	if !IsIPv6Target("2001:db8::1") {
		t.Errorf("expected 2001:db8::1 to be IPv6")
	}
}

func TestFindListeningProcess_FailClosed(t *testing.T) {
	t.Run("MissingProcTableReturnsError", func(t *testing.T) {
		emptyProc := t.TempDir()
		_, err := FindListeningProcess(emptyProc, "127.0.0.1", 443)
		if err == nil || !strings.Contains(err.Error(), "ipv4 socket table") {
			t.Errorf("expected error for missing proc table, got: %v", err)
		}
	})

	t.Run("DirectoryAsSocketTableReturnsError", func(t *testing.T) {
		procDir := t.TempDir()
		if err := os.MkdirAll(filepath.Join(procDir, "net", "tcp"), 0755); err != nil {
			t.Fatal(err)
		}
		_, err := FindListeningProcess(procDir, "127.0.0.1", 443)
		if err == nil || !strings.Contains(err.Error(), "is a directory") {
			t.Errorf("expected is a directory error, got: %v", err)
		}
	})

	t.Run("IPv6MissingTCP6ReturnsError", func(t *testing.T) {
		procDir := t.TempDir()
		if err := os.MkdirAll(filepath.Join(procDir, "net"), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(procDir, "net", "tcp"), []byte(""), 0644); err != nil {
			t.Fatal(err)
		}
		_, err := FindListeningProcess(procDir, "::1", 443)
		if err == nil || !strings.Contains(err.Error(), "ipv6 socket table") {
			t.Errorf("expected ipv6 socket table unavailable error, got: %v", err)
		}
	})

	t.Run("SocketPresentWithoutPID", func(t *testing.T) {
		procDir := t.TempDir()
		if err := os.MkdirAll(filepath.Join(procDir, "net"), 0755); err != nil {
			t.Fatal(err)
		}
		tcpContent := "  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode\n" +
			"   0: 0100007F:01BB 00000000:0000 0A 00000000:00000000 00:00000000 00000000     0        0 88888 1 0000000000000000 100 0 0 10 0\n"
		if err := os.WriteFile(filepath.Join(procDir, "net", "tcp"), []byte(tcpContent), 0644); err != nil {
			t.Fatal(err)
		}
		lookup, err := FindListeningProcess(procDir, "127.0.0.1", 443)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !lookup.SocketFound {
			t.Errorf("expected SocketFound to be true")
		}
		if lookup.SocketInode != "88888" {
			t.Errorf("expected inode 88888, got %s", lookup.SocketInode)
		}
		if lookup.PID != 0 {
			t.Errorf("expected PID 0 when unmapped, got %d", lookup.PID)
		}
	})

	t.Run("CleanAbsence", func(t *testing.T) {
		procDir := t.TempDir()
		if err := os.MkdirAll(filepath.Join(procDir, "net"), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(procDir, "net", "tcp"), []byte(""), 0644); err != nil {
			t.Fatal(err)
		}
		lookup, err := FindListeningProcess(procDir, "127.0.0.1", 443)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if lookup.SocketFound {
			t.Errorf("expected SocketFound=false on empty table")
		}
		if lookup.PID != 0 {
			t.Errorf("expected PID 0")
		}
	})
}

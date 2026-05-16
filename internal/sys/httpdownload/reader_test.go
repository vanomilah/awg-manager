package httpdownload

import (
	"bytes"
	"errors"
	"io"
	"sync/atomic"
	"testing"
)

func TestReader_PassthroughBytes(t *testing.T) {
	src := bytes.Repeat([]byte("x"), 1024)
	pr := NewReader(bytes.NewReader(src), int64(len(src)), nil)
	got, err := io.ReadAll(pr)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if !bytes.Equal(got, src) {
		t.Errorf("payload mismatch: got %d bytes, want %d", len(got), len(src))
	}
	if pr.BytesRead() != int64(len(src)) {
		t.Errorf("BytesRead = %d, want %d", pr.BytesRead(), len(src))
	}
}

func TestReader_EmitsAtLeastOnceOnEOF(t *testing.T) {
	// 1 KB of data — well under emitBytes threshold, so we should still
	// see a final emit triggered by io.EOF.
	src := bytes.Repeat([]byte("x"), 1024)
	var calls atomic.Int32
	var lastDownloaded atomic.Int64
	pr := NewReader(bytes.NewReader(src), int64(len(src)), func(downloaded, total int64) {
		calls.Add(1)
		lastDownloaded.Store(downloaded)
	})
	if _, err := io.ReadAll(pr); err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if calls.Load() == 0 {
		t.Error("expected at least one progress emit (EOF flush)")
	}
	if got := lastDownloaded.Load(); got != int64(len(src)) {
		t.Errorf("last downloaded = %d, want %d (final frame)", got, len(src))
	}
}

func TestReader_EmitsAfterByteThreshold(t *testing.T) {
	// Use fixed-size reads so threshold-based emits are deterministic.
	// With 8KB chunks and 256KB payload, Reader crosses 64KB threshold
	// repeatedly and must emit multiple frames.
	src := bytes.Repeat([]byte("x"), 256*1024)
	var calls atomic.Int32
	var lastDownloaded atomic.Int64
	pr := NewReader(bytes.NewReader(src), int64(len(src)), func(downloaded, total int64) {
		calls.Add(1)
		lastDownloaded.Store(downloaded)
	})

	buf := make([]byte, 8*1024)
	for {
		_, err := pr.Read(buf)
		if err == nil {
			continue
		}
		if errors.Is(err, io.EOF) {
			break
		}
		t.Fatalf("Read: %v", err)
	}

	if got := calls.Load(); got < 4 {
		t.Errorf("expected ≥4 emits across 256KB stream, got %d", got)
	}
	if got := lastDownloaded.Load(); got != int64(len(src)) {
		t.Errorf("last downloaded = %d, want %d (final frame)", got, len(src))
	}
}

func TestReader_NilProgressIsSafe(t *testing.T) {
	src := bytes.Repeat([]byte("x"), 1024)
	pr := NewReader(bytes.NewReader(src), 0, nil)
	if _, err := io.ReadAll(pr); err != nil {
		t.Fatalf("ReadAll with nil progress: %v", err)
	}
}

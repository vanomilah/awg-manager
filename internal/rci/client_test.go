package rci

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/hoaxisr/awg-manager/internal/logging"
)

// recordedEntry captures a single log call for assertions.
type recordedEntry struct {
	level    logging.Level
	group    string
	subgroup string
	action   string
	target   string
	message  string
}

// recordingLogger implements logging.AppLogger by storing every call.
// Goroutine-safe is unnecessary — tests are sequential.
type recordingLogger struct {
	entries []recordedEntry
}

func (r *recordingLogger) AppLog(level logging.Level, group, subgroup, action, target, message string) {
	r.entries = append(r.entries, recordedEntry{level, group, subgroup, action, target, message})
}

// newTestClientWithLogger is like newTestClient but wires a recorder so log
// emissions are observable.
func newTestClientWithLogger(handler http.Handler) (*Client, *httptest.Server, *recordingLogger) {
	c, srv := newTestClient(handler)
	rec := &recordingLogger{}
	c.SetAppLogger(rec)
	return c, srv, rec
}

func newTestClient(handler http.Handler) (*Client, *httptest.Server) {
	srv := httptest.NewServer(handler)
	c := &Client{
		http:    srv.Client(),
		baseURL: srv.URL,
	}
	return c, srv
}

func TestGet_DecodesJSON(t *testing.T) {
	c, srv := newTestClient(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/show/version" {
			t.Errorf("path = %q", r.URL.Path)
		}
		w.Write([]byte(`{"release":"4.03"}`))
	}))
	defer srv.Close()

	var dst struct{ Release string }
	if err := c.Get(context.Background(), "/show/version", &dst); err != nil {
		t.Fatal(err)
	}
	if dst.Release != "4.03" {
		t.Errorf("release = %q", dst.Release)
	}
}

func TestGetRaw_ReturnsBytes(t *testing.T) {
	c, srv := newTestClient(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"raw": true}`))
	}))
	defer srv.Close()

	data, err := c.GetRaw(context.Background(), "/show/interface/Wireguard0")
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `{"raw": true}` {
		t.Errorf("got %q", string(data))
	}
}

func TestPost_SendsJSON(t *testing.T) {
	var received []byte
	c, srv := newTestClient(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("content-type = %q", ct)
		}
		received, _ = io.ReadAll(r.Body)
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	payload := map[string]any{"ip": map[string]any{"route": map[string]any{"default": true}}}
	_, err := c.Post(context.Background(), payload)
	if err != nil {
		t.Fatal(err)
	}
	var parsed map[string]any
	json.Unmarshal(received, &parsed)
	if parsed["ip"] == nil {
		t.Error("expected ip key in payload")
	}
}

func TestPostBatch_SendsArray(t *testing.T) {
	var received []byte
	c, srv := newTestClient(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received, _ = io.ReadAll(r.Body)
		w.Write([]byte(`[{}, {}]`))
	}))
	defer srv.Close()

	cmds := []any{
		map[string]any{"interface": map[string]any{"name": "Wireguard0", "up": true}},
		map[string]any{"system": map[string]any{"configuration": map[string]any{"save": map[string]any{}}}},
	}
	results, err := c.PostBatch(context.Background(), cmds)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Errorf("expected 2 results, got %d", len(results))
	}
	var parsed []any
	json.Unmarshal(received, &parsed)
	if len(parsed) != 2 {
		t.Errorf("expected 2-element array, got %d", len(parsed))
	}
}

func TestGet_Non200_ReturnsError(t *testing.T) {
	c, srv := newTestClient(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	}))
	defer srv.Close()

	var dst any
	err := c.Get(context.Background(), "/show/version", &dst)
	if err == nil {
		t.Fatal("expected error for 500 status")
	}
}

func TestPost_RCIError_ReturnsError(t *testing.T) {
	c, srv := newTestClient(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"status":"error","message":"address conflict"}`))
	}))
	defer srv.Close()

	_, err := c.Post(context.Background(), map[string]any{})
	if err == nil {
		t.Fatal("expected error for RCI error response")
	}
}

func TestGet_Success_EmitsNoLog(t *testing.T) {
	c, srv, rec := newTestClientWithLogger(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	var dst struct{ Ok bool }
	if err := c.Get(context.Background(), "/show/version", &dst); err != nil {
		t.Fatal(err)
	}
	if len(rec.entries) != 0 {
		t.Errorf("expected no log entries on success, got %d: %+v", len(rec.entries), rec.entries)
	}
}

func TestGet_Non200_LogsError(t *testing.T) {
	c, srv, rec := newTestClientWithLogger(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	}))
	defer srv.Close()

	var dst any
	_ = c.Get(context.Background(), "/show/version", &dst)
	if len(rec.entries) != 1 {
		t.Fatalf("expected 1 log entry, got %d", len(rec.entries))
	}
	e := rec.entries[0]
	if e.level != "error" {
		t.Errorf("level = %q, want error", e.level)
	}
	if e.action != "GET" || e.target != "/show/version" {
		t.Errorf("action/target = %q/%q", e.action, e.target)
	}
	if !strings.Contains(e.message, "status 500") {
		t.Errorf("message = %q, want contains 'status 500'", e.message)
	}
}

func TestGet_TransportError_LogsError(t *testing.T) {
	// Build a client whose URL is unreachable.
	rec := &recordingLogger{}
	c := &Client{
		http:    &http.Client{Timeout: 100 * time.Millisecond},
		baseURL: "http://127.0.0.1:1", // refused
	}
	c.SetAppLogger(rec)

	var dst any
	_ = c.Get(context.Background(), "/show/version", &dst)
	if len(rec.entries) != 1 {
		t.Fatalf("expected 1 log entry, got %d: %+v", len(rec.entries), rec.entries)
	}
	e := rec.entries[0]
	if e.level != "error" {
		t.Errorf("level = %q, want error", e.level)
	}
	if !strings.Contains(e.message, "transport:") {
		t.Errorf("message = %q, want contains 'transport:'", e.message)
	}
}

func TestGet_DecodeError_LogsError(t *testing.T) {
	c, srv, rec := newTestClientWithLogger(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`not json`))
	}))
	defer srv.Close()

	var dst struct{ X int }
	_ = c.Get(context.Background(), "/show/version", &dst)
	if len(rec.entries) != 1 {
		t.Fatalf("expected 1 log entry, got %d", len(rec.entries))
	}
	e := rec.entries[0]
	if e.level != "error" {
		t.Errorf("level = %q, want error", e.level)
	}
	if !strings.Contains(e.message, "decode:") {
		t.Errorf("message = %q, want contains 'decode:'", e.message)
	}
}

func TestPost_Success_EmitsNoLog(t *testing.T) {
	c, srv, rec := newTestClientWithLogger(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	if _, err := c.Post(context.Background(), map[string]any{"x": 1}); err != nil {
		t.Fatal(err)
	}
	if len(rec.entries) != 0 {
		t.Errorf("expected no log entries on success, got %d: %+v", len(rec.entries), rec.entries)
	}
}

func TestPost_TransportError_LogsError(t *testing.T) {
	rec := &recordingLogger{}
	c := &Client{
		http:    &http.Client{Timeout: 100 * time.Millisecond},
		baseURL: "http://127.0.0.1:1",
	}
	c.SetAppLogger(rec)

	_, _ = c.Post(context.Background(), map[string]any{})
	if len(rec.entries) != 1 {
		t.Fatalf("expected 1 log entry, got %d", len(rec.entries))
	}
	e := rec.entries[0]
	if e.level != "error" {
		t.Errorf("level = %q, want error", e.level)
	}
	if !strings.Contains(e.message, "transport:") {
		t.Errorf("message = %q, want contains 'transport:'", e.message)
	}
}

func TestPost_Non200_LogsError(t *testing.T) {
	c, srv, rec := newTestClientWithLogger(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		w.Write([]byte(`internal err`))
	}))
	defer srv.Close()

	_, _ = c.Post(context.Background(), map[string]any{})
	if len(rec.entries) != 1 {
		t.Fatalf("expected 1 log entry, got %d", len(rec.entries))
	}
	e := rec.entries[0]
	if e.level != "error" {
		t.Errorf("level = %q, want error", e.level)
	}
	if !strings.Contains(e.message, "status 500") {
		t.Errorf("message = %q, want contains 'status 500'", e.message)
	}
}

func TestPost_NDMSError_LogsWarn(t *testing.T) {
	c, srv, rec := newTestClientWithLogger(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// HTTP 200 but body carries NDMS error envelope. ExtractError parses this.
		w.Write([]byte(`{"status":"error","message":"address conflict"}`))
	}))
	defer srv.Close()

	_, _ = c.Post(context.Background(), map[string]any{})
	if len(rec.entries) != 1 {
		t.Fatalf("expected 1 log entry, got %d", len(rec.entries))
	}
	e := rec.entries[0]
	if e.level != "warn" {
		t.Errorf("level = %q, want warn", e.level)
	}
	if !strings.HasPrefix(e.message, "ndms-error:") {
		t.Errorf("message = %q, want starts with 'ndms-error:'", e.message)
	}
}

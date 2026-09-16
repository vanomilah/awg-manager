package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/hoaxisr/awg-manager/internal/mihomonative"
)

type fakeMutationApplier struct {
	degraded       bool
	applyCount     int
	draftCount     int
	pendingCount   int
	reconcileCalls []string
}

func (f *fakeMutationApplier) ApplyNativeMutation(ctx context.Context, mutateFn func() error) error {
	f.applyCount++
	return mutateFn()
}

func (f *fakeMutationApplier) ApplyDraftOnly(ctx context.Context, mutateFn func() error) error {
	f.draftCount++
	return mutateFn()
}

func (f *fakeMutationApplier) ApplyPendingDraft(ctx context.Context) error {
	f.pendingCount++
	return nil
}

func (f *fakeMutationApplier) IsDegraded() bool {
	return f.degraded
}

func (f *fakeMutationApplier) Reconcile(ctx context.Context, action string, force bool) error {
	f.reconcileCalls = append(f.reconcileCalls, action)
	return nil
}

func TestMihomoHandler_DegradedReturns503(t *testing.T) {
	native, err := mihomonative.NewStore(filepath.Join(t.TempDir(), "native.json"))
	if err != nil {
		t.Fatal(err)
	}
	h := NewMihomoHandler(nil)
	h.SetNativeStore(native)

	applier := &fakeMutationApplier{degraded: true}
	h.SetMutationApplier(applier)

	mux := http.NewServeMux()
	h.RegisterRoutes(mux, nil)

	// Attempt a mutation while degraded
	body := []byte(`{"type":"select","name":"TestGroup","proxies":["DIRECT"]}`)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/mihomo/native/groups", bytes.NewReader(body))
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected HTTP 503, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp["code"] != "RECOVERY_REQUIRED" {
		t.Fatalf("expected code RECOVERY_REQUIRED, got: %#v", resp)
	}
}

func TestMihomoHandler_ApplyVsDraftRouting(t *testing.T) {
	native, err := mihomonative.NewStore(filepath.Join(t.TempDir(), "native.json"))
	if err != nil {
		t.Fatal(err)
	}
	h := NewMihomoHandler(nil)
	h.SetNativeStore(native)

	applier := &fakeMutationApplier{degraded: false}
	h.SetMutationApplier(applier)

	mux := http.NewServeMux()
	h.RegisterRoutes(mux, nil)

	// 1. apply=false -> ApplyDraftOnly
	body := []byte(`{"type":"select","name":"DraftGroup","proxies":["DIRECT"]}`)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/mihomo/native/groups?apply=false", bytes.NewReader(body))
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if applier.draftCount != 1 || applier.applyCount != 0 {
		t.Fatalf("expected draftCount=1, applyCount=0; got draft=%d apply=%d", applier.draftCount, applier.applyCount)
	}

	// 2. apply=true (default) -> ApplyNativeMutation
	body2 := []byte(`{"type":"select","name":"AppliedGroup","proxies":["DIRECT"]}`)
	rec2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodPost, "/api/mihomo/native/groups", bytes.NewReader(body2))
	mux.ServeHTTP(rec2, req2)

	if rec2.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec2.Code, rec2.Body.String())
	}
	if applier.applyCount != 1 {
		t.Fatalf("expected applyCount=1, got: %d", applier.applyCount)
	}
}

func TestMihomoHandler_RecoveryReconcileEndpoint(t *testing.T) {
	h := NewMihomoHandler(nil)
	applier := &fakeMutationApplier{degraded: true}
	h.SetMutationApplier(applier)

	mux := http.NewServeMux()
	h.RegisterRoutes(mux, nil)

	body := []byte(`{"action":"rollback_to_lkg","force":false}`)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/mihomo/recovery/reconcile", bytes.NewReader(body))
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	if len(applier.reconcileCalls) != 1 || applier.reconcileCalls[0] != "rollback_to_lkg" {
		t.Fatalf("expected reconcile call rollback_to_lkg, got: %#v", applier.reconcileCalls)
	}
}

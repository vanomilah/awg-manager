package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hoaxisr/awg-manager/internal/downloader"
)

type fakeAmneziaCPDownloader struct {
	readAll func(ctx context.Context, req downloader.Request) ([]byte, downloader.ResponseMeta, error)
}

func (f fakeAmneziaCPDownloader) ReadAll(ctx context.Context, req downloader.Request) ([]byte, downloader.ResponseMeta, error) {
	return f.readAll(ctx, req)
}

func TestAmneziaCPLoginUsesDownloader(t *testing.T) {
	var seen downloader.Request
	h := NewAmneziaCPHandler(nil)
	h.SetDownloader(fakeAmneziaCPDownloader{
		readAll: func(_ context.Context, req downloader.Request) ([]byte, downloader.ResponseMeta, error) {
			seen = req
			headers := http.Header{}
			headers.Add("Set-Cookie", "v_sid=session-123; Path=/; HttpOnly")
			return []byte(`{"ok":true}`), downloader.ResponseMeta{
				StatusCode: http.StatusOK,
				Headers:    headers,
			}, nil
		},
	})

	r := httptest.NewRequest(http.MethodPost, "/api/amnezia-premium/login", strings.NewReader(`{"vpnKey":"vpn://premium","remember":true}`))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h.Login(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "session-123") {
		t.Fatalf("response does not contain sid: %s", w.Body.String())
	}
	if seen.Purpose != "amnezia-premium-login" {
		t.Fatalf("purpose = %q, want amnezia-premium-login", seen.Purpose)
	}
	if seen.Method != http.MethodPost {
		t.Fatalf("method = %q, want POST", seen.Method)
	}
	if seen.URL != amneziaCPOrigin+"/api/login" {
		t.Fatalf("url = %q", seen.URL)
	}
	if !strings.Contains(string(seen.Body), `"vpnKey":"vpn://premium"`) {
		t.Fatalf("body = %s", string(seen.Body))
	}
	if seen.Headers.Get("Origin") != amneziaCPOrigin {
		t.Fatalf("Origin header = %q", seen.Headers.Get("Origin"))
	}
	if seen.Headers.Get("Connection") != "close" {
		t.Fatalf("Connection header = %q, want close", seen.Headers.Get("Connection"))
	}
}

func TestAmneziaCPLoginPreservesCPErrorStatus(t *testing.T) {
	h := NewAmneziaCPHandler(nil)
	h.SetDownloader(fakeAmneziaCPDownloader{
		readAll: func(_ context.Context, req downloader.Request) ([]byte, downloader.ResponseMeta, error) {
			if req.Purpose != "amnezia-premium-login" {
				t.Fatalf("purpose = %q", req.Purpose)
			}
			return []byte(`{"message":"bad key"}`), downloader.ResponseMeta{StatusCode: http.StatusUnprocessableEntity}, nil
		},
	})

	r := httptest.NewRequest(http.MethodPost, "/api/amnezia-premium/login", strings.NewReader(`{"vpnKey":"vpn://bad"}`))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h.Login(w, r)

	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422, body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "bad key") {
		t.Fatalf("response does not contain CP error: %s", w.Body.String())
	}
}

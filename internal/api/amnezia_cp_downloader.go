package api

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/hoaxisr/awg-manager/internal/downloader"
)

// Amnezia CP error responses contain user-facing messages; allow 4xx/5xx
// through the downloader so handlers can parse and return the original body.
var amneziaCPAllowedStatuses = makeHTTPStatusRange(200, 599)

type amneziaCPDownloader interface {
	ReadAll(ctx context.Context, req downloader.Request) ([]byte, downloader.ResponseMeta, error)
}

// SetDownloader routes Amnezia Premium CP requests through the shared service downloader.
// With nil downloader the handler keeps its dedicated direct HTTP client.
func (h *AmneziaCPHandler) SetDownloader(dl amneziaCPDownloader) {
	if h == nil || dl == nil {
		return
	}
	client := newAmneziaCPHTTPClient()
	client.Transport = amneziaCPDownloadTransport{dl: dl}
	h.client = client
}

type amneziaCPDownloadTransport struct {
	dl amneziaCPDownloader
}

func (t amneziaCPDownloadTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	var reqBody []byte
	if req.Body != nil {
		body, err := io.ReadAll(req.Body)
		closeErr := req.Body.Close()
		if err != nil {
			return nil, err
		}
		if closeErr != nil {
			return nil, closeErr
		}
		reqBody = body
	}

	headers := req.Header.Clone()
	headers.Set("Connection", "close")

	body, meta, err := t.dl.ReadAll(req.Context(), downloader.Request{
		Purpose:       amneziaCPPurpose(req.URL.Path),
		URL:           req.URL.String(),
		Method:        req.Method,
		Headers:       headers,
		Body:          reqBody,
		Timeout:       45 * time.Second,
		MaxBodyBytes:  maxBodySize,
		AllowedStatus: amneziaCPAllowedStatuses,
	})
	if err != nil {
		return nil, err
	}

	statusText := http.StatusText(meta.StatusCode)
	if statusText == "" {
		statusText = "status"
	}
	return &http.Response{
		StatusCode:    meta.StatusCode,
		Status:        fmt.Sprintf("%d %s", meta.StatusCode, statusText),
		Proto:         "HTTP/1.1",
		ProtoMajor:    1,
		ProtoMinor:    1,
		Header:        meta.Headers.Clone(),
		Body:          io.NopCloser(bytes.NewReader(body)),
		ContentLength: int64(len(body)),
		Request:       req,
	}, nil
}

func amneziaCPPurpose(path string) string {
	switch path {
	case "/api/login":
		return "amnezia-premium-login"
	case "/api/account-info":
		return "amnezia-premium-account-info"
	case "/api/download-config":
		return "amnezia-premium-download-config"
	default:
		return "amnezia-premium"
	}
}

func makeHTTPStatusRange(from, to int) []int {
	if to < from {
		return nil
	}
	out := make([]int, 0, to-from+1)
	for status := from; status <= to; status++ {
		out = append(out, status)
	}
	return out
}

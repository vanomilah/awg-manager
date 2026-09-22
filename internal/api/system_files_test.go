package api

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestFilesUpload_LargeIPK(t *testing.T) {
	h, root := newSystemToolsForTest(t, "expert")

	// 15 MB payload (exceeds previous 10MB limit)
	const fileSize = 15 * 1024 * 1024
	largePayload := bytes.Repeat([]byte("A"), fileSize)

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	if err := mw.WriteField("path", root); err != nil {
		t.Fatal(err)
	}
	part, err := mw.CreateFormFile("file", "awg-manager_2.17.60_aarch64-3.10-kn.ipk")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(part, bytes.NewReader(largePayload)); err != nil {
		t.Fatal(err)
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/system/files/upload", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rec := httptest.NewRecorder()

	h.FilesUpload(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d: %s", rec.Code, rec.Body.String())
	}

	var res struct {
		Success bool `json:"success"`
		Data    struct {
			Path string `json:"path"`
		} `json:"data"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&res); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !res.Success {
		t.Fatalf("expected success: true, got %v", res)
	}

	savedPath := filepath.Join(root, "awg-manager_2.17.60_aarch64-3.10-kn.ipk")
	fi, err := os.Stat(savedPath)
	if err != nil {
		t.Fatalf("uploaded file not found on disk: %v", err)
	}
	if fi.Size() != fileSize {
		t.Fatalf("uploaded file size = %d, want %d", fi.Size(), fileSize)
	}
}

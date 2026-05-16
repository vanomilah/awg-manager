package api

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"

	"github.com/hoaxisr/awg-manager/internal/response"
)

var utf8BOM = []byte{0xEF, 0xBB, 0xBF}

// parseJSON guards method + reads the request body into T. On any
// error — wrong method, body-read failure, decode failure — it writes
// the canonical error response and returns (zero, false). Callers bail
// out immediately on false.
//
// Body size is capped to maxBodySize via http.MaxBytesReader so an
// oversized payload gets a clean 413 from the decoder rather than
// draining the whole body into memory.
func parseJSON[T any](w http.ResponseWriter, r *http.Request, method string) (T, bool) {
	var dst T
	if r.Method != method {
		response.MethodNotAllowed(w)
		return dst, false
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxBodySize)
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		response.ErrorWithStatus(w, http.StatusBadRequest, "invalid body", "INVALID_BODY")
		return dst, false
	}
	raw = bytes.TrimSpace(raw)
	raw = bytes.TrimPrefix(raw, utf8BOM)
	if len(raw) == 0 {
		response.ErrorWithStatus(w, http.StatusBadRequest, "invalid JSON", "INVALID_JSON")
		return dst, false
	}
	if err := json.Unmarshal(raw, &dst); err != nil {
		response.ErrorWithStatus(w, http.StatusBadRequest, "invalid JSON", "INVALID_JSON")
		return dst, false
	}
	return dst, true
}

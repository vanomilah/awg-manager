package xrayserver

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// XHTTPTuning encapsulates ultra-low latency tuning parameters for Xray XHTTP transport.
type XHTTPTuning struct {
	Preset             string `json:"preset"`                // "live_low_latency" | "balanced" | "custom"
	UplinkMethod       string `json:"uplink_method"`         // "GET" | "POST" | "DELETE"
	MinPostsIntervalMs string `json:"min_posts_interval_ms"` // "5-10"
	MaxBufferedPosts   int    `json:"max_buffered_posts"`    // 1..1000, default 30
	StreamUpServerSecs string `json:"stream_up_server_secs"` // "20-80"
	StreamPathSuffix   string `json:"stream_path_suffix"`    // "stream.ts"
	EnableXmux         bool   `json:"enable_xmux"`           // false for low latency
	XPaddingBytes      string `json:"x_padding_bytes"`       // "100-1000"
	XPaddingHeader     string `json:"x_padding_header"`      // "Referer"
	XPaddingPlacement  string `json:"x_padding_placement"`   // "queryInHeader"
}

// DefaultLiveLowLatencyTuning returns the benchmark ultra-low latency preset for Happ and speedtest.
func DefaultLiveLowLatencyTuning() XHTTPTuning {
	return XHTTPTuning{
		Preset:             "live_low_latency",
		UplinkMethod:       "GET",
		MinPostsIntervalMs: "5-10",
		MaxBufferedPosts:   30,
		StreamUpServerSecs: "20-80",
		StreamPathSuffix:   "stream.ts",
		EnableXmux:         false,
		XPaddingBytes:      "100-1000",
		XPaddingHeader:     "Referer",
		XPaddingPlacement:  "queryInHeader",
	}
}

// DefaultBalancedTuning returns the balanced streaming preset with connection multiplexing enabled.
func DefaultBalancedTuning() XHTTPTuning {
	return XHTTPTuning{
		Preset:             "balanced",
		UplinkMethod:       "POST",
		MinPostsIntervalMs: "30-100",
		MaxBufferedPosts:   50,
		StreamUpServerSecs: "20-80",
		StreamPathSuffix:   "",
		EnableXmux:         true,
		XPaddingBytes:      "100-1000",
		XPaddingHeader:     "Referer",
		XPaddingPlacement:  "queryInHeader",
	}
}

var rangeRegex = regexp.MustCompile(`^\d+(-\d+)?$`)

func validateRangeString(val, fieldName string) error {
	val = strings.TrimSpace(val)
	if val == "" {
		return nil
	}
	if !rangeRegex.MatchString(val) {
		return fmt.Errorf("%s must match format 'N' or 'min-max', got %q", fieldName, val)
	}
	if strings.Contains(val, "-") {
		parts := strings.Split(val, "-")
		minVal, _ := strconv.Atoi(parts[0])
		maxVal, _ := strconv.Atoi(parts[1])
		if minVal > maxVal {
			return fmt.Errorf("%s: min (%d) cannot be greater than max (%d)", fieldName, minVal, maxVal)
		}
	}
	return nil
}

func sanitizeStreamPathSuffix(raw string) string {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimLeft(raw, "/")
	raw = strings.ReplaceAll(raw, "..", "")
	raw = strings.ReplaceAll(raw, "?", "")
	raw = strings.ReplaceAll(raw, "#", "")
	raw = strings.TrimSpace(raw)
	return raw
}

// ValidateAndNormalizeXHTTPTuning validates tuning parameters according to Argon architectural specs.
func ValidateAndNormalizeXHTTPTuning(t *XHTTPTuning) error {
	if t == nil {
		return nil
	}
	preset := strings.ToLower(strings.TrimSpace(t.Preset))
	switch preset {
	case "live_low_latency":
		*t = DefaultLiveLowLatencyTuning()
		return nil
	case "balanced":
		*t = DefaultBalancedTuning()
		return nil
	case "custom":
		t.Preset = "custom"
		method := strings.ToUpper(strings.TrimSpace(t.UplinkMethod))
		if method != "GET" && method != "POST" && method != "DELETE" {
			if method == "" {
				method = "GET"
			} else {
				return fmt.Errorf("invalid uplink_method %q: must be GET, POST, or DELETE", t.UplinkMethod)
			}
		}
		t.UplinkMethod = method

		if t.MinPostsIntervalMs == "" {
			t.MinPostsIntervalMs = "5-10"
		}
		if err := validateRangeString(t.MinPostsIntervalMs, "min_posts_interval_ms"); err != nil {
			return err
		}

		if t.MaxBufferedPosts <= 0 {
			t.MaxBufferedPosts = 30
		} else if t.MaxBufferedPosts < 1 || t.MaxBufferedPosts > 1000 {
			return fmt.Errorf("max_buffered_posts must be between 1 and 1000, got %d", t.MaxBufferedPosts)
		}

		if t.StreamUpServerSecs == "" {
			t.StreamUpServerSecs = "20-80"
		}
		if err := validateRangeString(t.StreamUpServerSecs, "stream_up_server_secs"); err != nil {
			return err
		}

		t.StreamPathSuffix = sanitizeStreamPathSuffix(t.StreamPathSuffix)

		if t.XPaddingBytes == "" {
			t.XPaddingBytes = "100-1000"
		}
		if err := validateRangeString(t.XPaddingBytes, "x_padding_bytes"); err != nil {
			return err
		}

		if t.XPaddingHeader == "" {
			t.XPaddingHeader = "Referer"
		}
		if t.XPaddingPlacement == "" {
			t.XPaddingPlacement = "queryInHeader"
		}
		return nil
	default:
		*t = DefaultLiveLowLatencyTuning()
		return nil
	}
}

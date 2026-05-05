// Package vlink: Clash / mihomo YAML subscription support.
//
// Entry points: IsClashYAML detects the format; ParseClashBody parses the
// body and returns a BatchResult identical in shape to ParseBatch. Per-
// protocol mappers live in clash_<protocol>.go (mirrors existing per-
// protocol layout for share-link parsers).
package vlink

import (
	"bytes"
	"regexp"
)

// scanLimit is how many bytes IsClashYAML inspects. A real Clash subscription
// has "proxies:" within the first few hundred bytes; 4 KB is a forgiving cap.
const scanLimit = 4 * 1024

// proxiesHeaderRe matches a top-level "proxies:" key (no leading indent).
var proxiesHeaderRe = regexp.MustCompile(`(?m)^proxies:\s*$`)

// IsClashYAML reports whether body looks like a Clash/mihomo subscription
// (top-level "proxies:" key in valid YAML). Cheap: scans the first 4 KB only.
// False positives on bodies that happen to contain "proxies:" mid-document
// are tolerable because ParseClashBody will then parse and find no entries.
func IsClashYAML(body []byte) bool {
	if len(body) == 0 {
		return false
	}
	head := body
	if len(head) > scanLimit {
		head = head[:scanLimit]
	}
	// Reject obvious non-YAML preludes.
	trimmed := bytes.TrimSpace(head)
	if len(trimmed) == 0 {
		return false
	}
	if trimmed[0] == '<' {
		return false // HTML
	}
	return proxiesHeaderRe.Match(head)
}

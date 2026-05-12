package subscription

import (
	"regexp"
	"strings"

	"github.com/hoaxisr/awg-manager/internal/singbox/vlink"
)

// schemeLineRegex matches share-link URLs we're interested in extracting from
// HTML / mixed text. Order: well-known schemes only.
var schemeLineRegex = regexp.MustCompile(
	`(vless|trojan|ss|hysteria2|hy2|naive\+\w+|vpn)://[^\s"<>']+`,
)

// NormalizeBody decodes a subscription body via the standard cascade:
//  1. Try base64 (urlsafe-aware) once. If the decoded text looks like
//     another base64 string, try a second pass.
//  2. If body looks like HTML, extract scheme:// URLs from anchor hrefs
//     or plain text fragments.
//  3. Fall back: split as plain text, drop empty / "#"-prefixed lines.
//
// The returned slice is the list of share links the caller should then feed
// to vlink.ParseBatch.
func NormalizeBody(body []byte, contentType string) []string {
	if decoded, ok := vlink.DoubleDecode(body); ok {
		return splitLines(decoded)
	}
	if isHTML(body, contentType) {
		extracted := extractFromHTML(body)
		if len(extracted) > 0 {
			return extracted
		}
	}
	return splitLines(body)
}

func splitLines(b []byte) []string {
	s := strings.NewReplacer("\r\n", "\n", "\r", "\n").Replace(string(b))
	parts := strings.Split(s, "\n")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" || strings.HasPrefix(p, "#") {
			continue
		}
		out = append(out, p)
	}
	return out
}

func isHTML(body []byte, contentType string) bool {
	if strings.Contains(strings.ToLower(contentType), "text/html") {
		return true
	}
	trimmed := strings.TrimSpace(string(body))
	return strings.HasPrefix(trimmed, "<!DOCTYPE") || strings.HasPrefix(trimmed, "<html") || strings.HasPrefix(trimmed, "<")
}

// extractFromHTML pulls all scheme:// URLs out of an HTML body — anchor
// hrefs first, then any plain-text occurrence not already captured.
//
// Each match is run through jsonUnescapeURL to recover share-links
// embedded as JSON-escaped strings inside React/Vue/etc. payloads. The
// motivating case is GitHub's blob view: it serialises the file body
// as a JSON string inside a script tag, so a real `&` separator in a
// share-link surfaces in the HTML as the six-character literal `&`.
// The schemeLineRegex character class accepts that sequence as part of
// the URL, and downstream `url.Parse` then treats `\` and `u0026` as
// part of a single query parameter value — yielding the classic
// "everything after the first `=` is one giant `flow=` value" failure.
//
// Other JSON escapes worth unwrapping (`<` / `>` / `'` /
// `\/` / `\"` / `\\`) appear in the same envelope for similar reasons.
// The order of replacements is significant — backslash must be last so
// we don't mangle the front of the other escapes mid-pass.
func extractFromHTML(body []byte) []string {
	matches := schemeLineRegex.FindAll(body, -1)
	out := make([]string, 0, len(matches))
	seen := make(map[string]bool)
	for _, m := range matches {
		s := jsonUnescapeURL(string(m))
		if seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}

// jsonUnescapeURL decodes the common JSON string-escape sequences that
// leak into share-link substrings when they're scraped from HTML pages
// whose content was serialised as JSON for a client-side framework.
// Returns the input unchanged when no escape markers are present, so
// the fast path is one substring check.
func jsonUnescapeURL(s string) string {
	if !strings.Contains(s, `\u`) && !strings.Contains(s, `\/`) &&
		!strings.Contains(s, `\"`) && !strings.Contains(s, `\\`) {
		return s
	}
	r := strings.NewReplacer(
		`&`, "&",
		`<`, "<",
		`>`, ">",
		`'`, "'",
		`\/`, "/",
		`\"`, `"`,
		`\\`, `\`, // last — must not chew the leading backslash of the others
	)
	return r.Replace(s)
}

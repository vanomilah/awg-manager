package subscription

import (
	"net/url"
	"strings"
)

// RewriteForRaw maps a small set of well-known git-hosting web-view
// ("blob") URLs to the equivalent raw-content URL. Returns the rewritten
// URL plus a boolean indicating whether a rewrite happened. URLs not
// matching a known pattern are returned unchanged with rewrote=false.
//
// Rationale: when a user pastes a github.com/.../blob/... URL, our
// fetcher otherwise downloads HTML and the share-link extractor scrapes
// URLs out of GitHub's React payload — where `&` is JSON-escaped to
// `&`. The literal `&` substring is not a real URL separator,
// so url.Parse treats the whole query as a single param. Rewriting to
// raw side-steps the HTML detour entirely.
//
// Supported patterns (path-prefix → path-prefix substitution, host swap
// where the hoster requires one):
//
//   - github.com/{O}/{R}/blob/{REF}/{PATH}
//     → raw.githubusercontent.com/{O}/{R}/{REF}/{PATH}
//
//   - gitlab.com/{O}/{R}/-/blob/{REF}/{PATH}
//     → gitlab.com/{O}/{R}/-/raw/{REF}/{PATH}
//     (works for self-hosted GitLab too — only path is rewritten)
//
//   - Gitea / Forgejo: {host}/{O}/{R}/src/branch/{REF}/{PATH}
//     → {host}/{O}/{R}/raw/branch/{REF}/{PATH}
//
// The function is deliberately conservative: query string and fragment
// are preserved as-is, unknown hosts are not rewritten, and the result
// always passes through net/url for syntactic validation.
func RewriteForRaw(rawURL string) (string, bool) {
	u, err := url.Parse(rawURL)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return rawURL, false
	}
	host := strings.ToLower(u.Host)

	// github.com/{owner}/{repo}/blob/{ref}/{path...}
	if host == "github.com" {
		parts := strings.SplitN(strings.TrimPrefix(u.Path, "/"), "/", 5)
		if len(parts) >= 5 && parts[2] == "blob" {
			u.Host = "raw.githubusercontent.com"
			u.Path = "/" + parts[0] + "/" + parts[1] + "/" + parts[3] + "/" + parts[4]
			return u.String(), true
		}
	}

	// GitLab — same host, /-/blob/ → /-/raw/. Covers gitlab.com and any
	// self-hosted instance using the standard layout.
	if i := strings.Index(u.Path, "/-/blob/"); i != -1 {
		u.Path = u.Path[:i] + "/-/raw/" + u.Path[i+len("/-/blob/"):]
		return u.String(), true
	}

	// Gitea / Forgejo — same host, /src/branch/ → /raw/branch/ (and
	// /src/commit/ → /raw/commit/ , /src/tag/ → /raw/tag/).
	for _, ref := range []string{"branch", "commit", "tag"} {
		from := "/src/" + ref + "/"
		to := "/raw/" + ref + "/"
		if i := strings.Index(u.Path, from); i != -1 {
			u.Path = u.Path[:i] + to + u.Path[i+len(from):]
			return u.String(), true
		}
	}

	return rawURL, false
}

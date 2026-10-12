package openflux

import "strings"

// PinnedVersion is the current pinned release tag / core version of OpenFlux.
const PinnedVersion = "0.5.1-node-v1.3.1"

// BinarySpec contains download details and integrity hash for an OpenFlux binary.
type BinarySpec struct {
	Version string
	URL     string
	SHA256  string
	Size    int64
}

// EmbeddedBinaries holds pinned binary specs keyed by router architecture.
var EmbeddedBinaries = map[string]BinarySpec{
	"aarch64-3.10": {
		Version: PinnedVersion,
		URL:     "https://github.com/p1neappleXpress/OpenFlux/releases/download/node-v1.3.1/openflux-linux-arm64",
		SHA256:  "dcf1fbf57bf535483c15b0aa0bd8bf6a3bc34248c1992db79b7ceeb818172f69",
		Size:    24772734,
	},
	"aarch64": {
		Version: PinnedVersion,
		URL:     "https://github.com/p1neappleXpress/OpenFlux/releases/download/node-v1.3.1/openflux-linux-arm64",
		SHA256:  "dcf1fbf57bf535483c15b0aa0bd8bf6a3bc34248c1992db79b7ceeb818172f69",
		Size:    24772734,
	},
	"arm64": {
		Version: PinnedVersion,
		URL:     "https://github.com/p1neappleXpress/OpenFlux/releases/download/node-v1.3.1/openflux-linux-arm64",
		SHA256:  "dcf1fbf57bf535483c15b0aa0bd8bf6a3bc34248c1992db79b7ceeb818172f69",
		Size:    24772734,
	},
	"x86_64": {
		Version: PinnedVersion,
		URL:     "https://github.com/p1neappleXpress/OpenFlux/releases/download/node-v1.3.1/openflux-linux-amd64",
		SHA256:  "94f0fbd2f7f60763bf6c4be0623e928ce878f846e33308dd41bc8d735e7b69b0",
		Size:    26390654,
	},
	"amd64": {
		Version: PinnedVersion,
		URL:     "https://github.com/p1neappleXpress/OpenFlux/releases/download/node-v1.3.1/openflux-linux-amd64",
		SHA256:  "94f0fbd2f7f60763bf6c4be0623e928ce878f846e33308dd41bc8d735e7b69b0",
		Size:    26390654,
	},
	"arm": {
		Version: PinnedVersion,
		URL:     "https://github.com/p1neappleXpress/OpenFlux/releases/download/node-v1.3.1/openflux-linux-arm",
		SHA256:  "c19b474aa03e5a77bd8a1d31672bc324a9678a87de333a71f384ae2d1c6c888f",
		Size:    24903806,
	},
}

// NormalizeArch standardizes router architecture strings to match EmbeddedBinaries keys.
func NormalizeArch(arch string) string {
	a := strings.ToLower(strings.TrimSpace(arch))
	switch {
	case strings.Contains(a, "aarch64") || strings.Contains(a, "arm64"):
		return "aarch64-3.10"
	case strings.Contains(a, "x86_64") || strings.Contains(a, "amd64"):
		return "x86_64"
	case strings.Contains(a, "arm"):
		return "arm"
	default:
		return a
	}
}

// IsArchSupported checks whether a prebuilt OpenFlux binary is available for arch.
func IsArchSupported(arch string) bool {
	norm := NormalizeArch(arch)
	_, ok := EmbeddedBinaries[norm]
	return ok
}

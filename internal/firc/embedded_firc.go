package firc

import (
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"net/url"
)

const FircRequiredVersion = "unreleased"

// IsAvailable reports whether the spec has valid download URLs and checksums.
func (s FircBinarySpec) IsAvailable() bool {
	valid := func(rawURL, sum string) bool {
		u, err := url.Parse(rawURL)
		digest, hexErr := hex.DecodeString(sum)
		return err == nil && u.Scheme == "https" && u.Host != "" && u.User == nil && hexErr == nil && len(digest) == 32
	}
	return validRuntimeVersion(s.Version) && s.PackageSize > 0 && s.PackageSize <= maxRuntimeIPKBytes && valid(s.PackageURL, s.PackageSHA256)
}

// EmbeddedFircBinaries maps architecture to FIRC binary download specs.
// URLs and SHA256 digests will be populated when FIRC releases are published.
//
//go:embed firc_releases.json
var runtimeReleasesJSON []byte

var EmbeddedFircBinaries = func() map[string]FircBinarySpec {
	var specs map[string]FircBinarySpec
	if err := json.Unmarshal(runtimeReleasesJSON, &specs); err != nil {
		panic("invalid embedded FIRC release manifest: " + err.Error())
	}
	return specs
}()

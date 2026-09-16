package installer

const RequiredVersion = "1.19.29"

type BinarySpec struct {
	Version string
	URL     string
	SHA256  string
	Size    int64 // Uncompressed size (if known) or 0
}

var EmbeddedBinaries = map[string]BinarySpec{
	// The legacy MIPS builds stay pinned until the newer assets are validated
	// on real 3.4 kernels. ARM64 is the tested target for the Mihomo routing
	// integration and uses the current stable core required by sniffing.
	"mipsel-3.4": {Version: "1.18.7", URL: "https://github.com/MetaCubeX/mihomo/releases/download/v1.18.7/mihomo-linux-mipsle-softfloat-v1.18.7.gz"},
	"mips-3.4":   {Version: "1.18.7", URL: "https://github.com/MetaCubeX/mihomo/releases/download/v1.18.7/mihomo-linux-mips-softfloat-v1.18.7.gz"},
	"aarch64-3.10": {
		Version: RequiredVersion,
		URL:     "https://github.com/MetaCubeX/mihomo/releases/download/v1.19.29/mihomo-linux-arm64-v1.19.29.gz",
		SHA256:  "9a868b5e4e0ad91d9d71e1b41b0cfce78aaba44360c30df74a723f8e3926a86c",
		Size:    44236926,
	},
}

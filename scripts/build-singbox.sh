#!/bin/bash
set -euo pipefail

usage() {
    echo "Usage: $0 <mipsel-3.4|mips-3.4|aarch64-3.10>" >&2
}

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"

ENTWARE_ARCH="${1:-}"
if [[ -z "$ENTWARE_ARCH" || $# -ne 1 ]]; then
    usage
    exit 1
fi

REQUIRED_VERSION_FILE="$PROJECT_ROOT/internal/singbox/installer/embedded.go"
DEFAULT_SINGBOX_VERSION="$(sed -n 's/^const RequiredVersion = "\(.*\)"/\1/p' "$REQUIRED_VERSION_FILE")"
SINGBOX_VERSION="${SINGBOX_VERSION:-$DEFAULT_SINGBOX_VERSION}"
SINGBOX_REF="${SINGBOX_REF:-v$SINGBOX_VERSION}"
SINGBOX_GO="${SINGBOX_GO:-go}"
CRONET_GO_DIR="${CRONET_GO_DIR:-$HOME/cronet-go}"
RELEASE_REPO="${RELEASE_REPO:-miuirussia/awg-manager}"
RELEASE_TAG="${RELEASE_TAG:-latest}"
RELEASE_BASE_URL="${RELEASE_BASE_URL:-https://github.com/$RELEASE_REPO/releases/download/$RELEASE_TAG}"
export GOTOOLCHAIN="${GOTOOLCHAIN:-local}"

case "$ENTWARE_ARCH" in
    mipsel-3.4)
        GOARCH="mipsle"
        GOMIPS_VALUE="softfloat"
        NAIVE=true
        ;;
    mips-3.4)
        GOARCH="mips"
        GOMIPS_VALUE="softfloat"
        NAIVE=false
        ;;
    aarch64-3.10)
        GOARCH="arm64"
        GOMIPS_VALUE=""
        NAIVE=true
        ;;
    *)
        echo "Unknown architecture: $ENTWARE_ARCH" >&2
        usage
        exit 1
        ;;
esac

if [[ -z "$SINGBOX_VERSION" ]]; then
    echo "ERROR: unable to determine sing-box version from $REQUIRED_VERSION_FILE" >&2
    exit 1
fi

require_command() {
    local name="$1"
    if ! command -v "$name" >/dev/null 2>&1; then
        echo "ERROR: missing required command: $name" >&2
        exit 1
    fi
}

require_command git
require_command file
require_command python3
require_command gofmt
require_command "$SINGBOX_GO"

sha256_file() {
    local path="$1"
    if command -v sha256sum >/dev/null 2>&1; then
        sha256sum "$path" | awk '{print $1}'
    else
        shasum -a 256 "$path" | awk '{print $1}'
    fi
}

cd "$PROJECT_ROOT"
mkdir -p dist build

echo "Building sing-box $SINGBOX_VERSION for $ENTWARE_ARCH using $($SINGBOX_GO version)"

if [[ -n "${SINGBOX_SRC:-}" ]]; then
    SINGBOX_DIR="$SINGBOX_SRC"
    if [[ ! -f "$SINGBOX_DIR/go.mod" ]]; then
        echo "ERROR: SINGBOX_SRC does not look like a sing-box checkout: $SINGBOX_DIR" >&2
        exit 1
    fi
else
    SINGBOX_DIR="${RUNNER_TEMP:-$PROJECT_ROOT/build}/sing-box-src"
    if [[ ! -d "$SINGBOX_DIR/.git" ]]; then
        rm -rf "$SINGBOX_DIR"
        git clone --depth=1 --branch "$SINGBOX_REF" https://github.com/sagernet/sing-box.git "$SINGBOX_DIR"
    else
        git -C "$SINGBOX_DIR" fetch --depth=1 origin "$SINGBOX_REF"
        git -C "$SINGBOX_DIR" checkout --force FETCH_HEAD
    fi
fi

cd "$SINGBOX_DIR"

if [[ "$NAIVE" == true ]]; then
    CRONET_REF="$(cat .github/CRONET_GO_VERSION)"
    if [[ ! -d "$CRONET_GO_DIR/.git" ]]; then
        mkdir -p "$CRONET_GO_DIR"
        git init "$CRONET_GO_DIR"
    fi
    if git -C "$CRONET_GO_DIR" remote get-url origin >/dev/null 2>&1; then
        git -C "$CRONET_GO_DIR" remote set-url origin https://github.com/sagernet/cronet-go.git
    else
        git -C "$CRONET_GO_DIR" remote add origin https://github.com/sagernet/cronet-go.git
    fi
    git -C "$CRONET_GO_DIR" fetch --depth=1 origin "$CRONET_REF"
    git -C "$CRONET_GO_DIR" checkout --force FETCH_HEAD
    git -C "$CRONET_GO_DIR" submodule update --init --recursive --depth=1

    rm -f "$CRONET_GO_DIR/naiveproxy/src/build/linux/sysroot_scripts/keyring.gpg"
    (
        cd "$CRONET_GO_DIR"
        GPG_TTY=/dev/null ./naiveproxy/src/build/linux/sysroot_scripts/generate_keyring.sh
    )

    (
        cd "$CRONET_GO_DIR"
        "$SINGBOX_GO" run ./cmd/build-naive --target="linux/$GOARCH" --libc=musl download-toolchain
    )
    eval "$(
        cd "$CRONET_GO_DIR"
        "$SINGBOX_GO" run ./cmd/build-naive --target="linux/$GOARCH" --libc=musl env --export
    )"
    TAGS="$(cat release/DEFAULT_BUILD_TAGS),with_musl"
    CGO_ENABLED_VALUE="1"
else
    TAGS="$(cat release/DEFAULT_BUILD_TAGS_OTHERS)"
    CGO_ENABLED_VALUE="0"
fi

LDFLAGS_SHARED="$(cat release/LDFLAGS)"
OUTPUT_TMP="$PROJECT_ROOT/build/sing-box-$SINGBOX_VERSION-$ENTWARE_ARCH"
OUTPUT="$PROJECT_ROOT/dist/sing-box-$SINGBOX_VERSION-$ENTWARE_ARCH"

ENV_VARS=(
    "CGO_ENABLED=$CGO_ENABLED_VALUE"
    "GOOS=linux"
    "GOARCH=$GOARCH"
)
if [[ -n "$GOMIPS_VALUE" ]]; then
    ENV_VARS+=("GOMIPS=$GOMIPS_VALUE")
fi

env "${ENV_VARS[@]}" "$SINGBOX_GO" build -v -trimpath \
    -o "$OUTPUT_TMP" \
    -tags "$TAGS" \
    -ldflags "-X 'github.com/sagernet/sing-box/constant.Version=$SINGBOX_VERSION' $LDFLAGS_SHARED -s -w -buildid=" \
    ./cmd/sing-box

mv "$OUTPUT_TMP" "$OUTPUT"
chmod +x "$OUTPUT"
file "$OUTPUT"
ls -lh "$OUTPUT"

OUTPUT_SHA256="$(sha256_file "$OUTPUT")"
OUTPUT_URL="$RELEASE_BASE_URL/$(basename "$OUTPUT")"

EMBEDDED_GO="$REQUIRED_VERSION_FILE" \
SINGBOX_VERSION="$SINGBOX_VERSION" \
ENTWARE_ARCH="$ENTWARE_ARCH" \
OUTPUT_URL="$OUTPUT_URL" \
OUTPUT_SHA256="$OUTPUT_SHA256" \
python3 <<'PY'
import os
import pathlib
import re
import sys

path = pathlib.Path(os.environ["EMBEDDED_GO"])
version = os.environ["SINGBOX_VERSION"]
arch = os.environ["ENTWARE_ARCH"]
url = os.environ["OUTPUT_URL"]
sha256 = os.environ["OUTPUT_SHA256"]

text = path.read_text()
text = re.sub(
    r'const RequiredVersion = "([^"]*)"',
    f'const RequiredVersion = "{version}"',
    text,
    count=1,
)

entry_pattern = re.compile(
    rf'(\t"{re.escape(arch)}":\s*)'
    r'\{Version: RequiredVersion, URL: "[^"]*", SHA256: "[^"]*"\},'
)
replacement = (
    rf'\1{{Version: RequiredVersion, URL: "{url}", SHA256: "{sha256}"}},'
)
text, count = entry_pattern.subn(replacement, text, count=1)
if count != 1:
    sys.stderr.write(f"ERROR: unable to update EmbeddedBinaries entry for {arch}\n")
    sys.exit(1)

path.write_text(text)
PY

gofmt -w "$REQUIRED_VERSION_FILE"
echo "Updated $REQUIRED_VERSION_FILE for $ENTWARE_ARCH"

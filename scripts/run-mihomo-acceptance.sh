#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$REPO_ROOT"

MANIFEST="$REPO_ROOT/scripts/mihomo-acceptance-manifest.json"

if [ ! -f "$MANIFEST" ]; then
    echo "ERROR: Acceptance manifest not found at $MANIFEST" >&2
    exit 1
fi

# 1. Bootstrap cache
bash "$REPO_ROOT/scripts/bootstrap-mihomo-acceptance.sh" "$MANIFEST"

# 2. Derive cache directory
MANIFEST_SHA="$(sha256sum "$MANIFEST" | awk '{print $1}')"
VERSION="$(python3 -c "import json; print(json.load(open('$MANIFEST'))['version'])")"
CACHE_DIR="$REPO_ROOT/.cache/mihomo/$VERSION/$MANIFEST_SHA"

if [ ! -f "$CACHE_DIR/.bootstrap_complete" ]; then
    echo "ERROR: Bootstrap marker not found at $CACHE_DIR/.bootstrap_complete" >&2
    exit 1
fi

echo "Running machine-verified Mihomo acceptance suite..."
JSON_OUTPUT=$(mktemp)
trap 'rm -f "$JSON_OUTPUT"' EXIT

set +e
MIHOMO_FIXTURE_DIR="$CACHE_DIR" \
MIHOMO_ACCEPTANCE_MANIFEST="$MANIFEST" \
MIHOMO_ACCEPTANCE=1 \
go test -json -run 'TestGenerateMihomoConfig_RepresentativeBinaryValidation' ./internal/singbox/router > "$JSON_OUTPUT" 2>&1
TEST_EXIT=$?
set -e

python3 "$REPO_ROOT/scripts/verify-mihomo-acceptance-json.py" \
    --json "$JSON_OUTPUT" \
    --test "TestGenerateMihomoConfig_RepresentativeBinaryValidation"

if [ $TEST_EXIT -ne 0 ]; then
    echo "ERROR: go test exited with non-zero code $TEST_EXIT" >&2
    exit $TEST_EXIT
fi

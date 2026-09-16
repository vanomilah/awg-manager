#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
BOOTSTRAP_SCRIPT="$REPO_ROOT/scripts/bootstrap-mihomo-acceptance.sh"

if [ ! -f "$BOOTSTRAP_SCRIPT" ]; then
    echo "ERROR: bootstrap script not found at $BOOTSTRAP_SCRIPT" >&2
    exit 1
fi

TMP_DIR="$(mktemp -d /tmp/mihomo-bootstrap-test.XXXXXX)"
PROD_CACHE="$REPO_ROOT/.cache/mihomo"

SERVER_PID=""
cleanup() {
    local exit_code=$?
    if [ -n "$SERVER_PID" ] && kill -0 "$SERVER_PID" 2>/dev/null; then
        kill "$SERVER_PID" 2>/dev/null || true
        wait "$SERVER_PID" 2>/dev/null || true
    fi
    rm -rf "$TMP_DIR"
    exit "$exit_code"
}
trap cleanup EXIT INT TERM

# Production cache isolation verification setup
PROD_BEFORE="$TMP_DIR/prod_cache_before.sha"
PROD_AFTER="$TMP_DIR/prod_cache_after.sha"
if [ -d "$PROD_CACHE" ]; then
    find "$PROD_CACHE" -type f -exec sha256sum {} + | sort > "$PROD_BEFORE"
else
    touch "$PROD_BEFORE"
fi

mkdir -p "$TMP_DIR/www"

# Prepare mock files
MOCK_BIN="$TMP_DIR/www/mihomo"
echo '#!/bin/sh' > "$MOCK_BIN"
echo 'echo "Mihomo v1.19.29 linux amd64"' >> "$MOCK_BIN"
chmod +x "$MOCK_BIN"
BIN_UNCOMPRESSED_SHA="$(sha256sum "$MOCK_BIN" | awk '{print $1}')"

MOCK_BIN_GZ="$TMP_DIR/www/mihomo-linux-amd64.gz"
gzip -c "$MOCK_BIN" > "$MOCK_BIN_GZ"
BIN_COMPRESSED_SHA="$(sha256sum "$MOCK_BIN_GZ" | awk '{print $1}')"

MOCK_GEOIP="$TMP_DIR/www/geoip.dat"
echo "mock geoip content for testing" > "$MOCK_GEOIP"
GEOIP_SHA="$(sha256sum "$MOCK_GEOIP" | awk '{print $1}')"

MOCK_GEOSITE="$TMP_DIR/www/geosite.dat"
echo "mock geosite content for testing" > "$MOCK_GEOSITE"
GEOSITE_SHA="$(sha256sum "$MOCK_GEOSITE" | awk '{print $1}')"

MOCK_ASN="$TMP_DIR/www/ASN.mmdb"
echo "mock ASN database content for testing" > "$MOCK_ASN"
ASN_SHA="$(sha256sum "$MOCK_ASN" | awk '{print $1}')"

# Dynamic HTTP server
PORT_FILE="$TMP_DIR/port"
REQUESTS_LOG="$TMP_DIR/requests.log"
touch "$REQUESTS_LOG"

cat << 'EOF' > "$TMP_DIR/server.py"
import http.server
import socketserver
import sys

web_dir = sys.argv[1]
port_file = sys.argv[2]
log_file = sys.argv[3]

class Handler(http.server.SimpleHTTPRequestHandler):
    def __init__(self, *args, **kwargs):
        super().__init__(*args, directory=web_dir, **kwargs)
    def do_GET(self):
        if "/slow/" in self.path:
            import time
            time.sleep(5)
            self.path = self.path.replace("/slow/", "/")
        return super().do_GET()
    def log_message(self, format, *args):
        try:
            with open(log_file, "a") as f:
                f.write(f"{self.command} {self.path}\n")
        except Exception:
            pass

with socketserver.TCPServer(("127.0.0.1", 0), Handler) as httpd:
    port = httpd.server_address[1]
    with open(port_file, "w") as f:
        f.write(str(port))
    httpd.serve_forever()
EOF

python3 "$TMP_DIR/server.py" "$TMP_DIR/www" "$PORT_FILE" "$REQUESTS_LOG" &
SERVER_PID=$!

for _ in $(seq 1 50); do
    if [ -f "$PORT_FILE" ] && [ -s "$PORT_FILE" ]; then
        break
    fi
    sleep 0.1
done

if [ ! -f "$PORT_FILE" ] || [ ! -s "$PORT_FILE" ]; then
    echo "ERROR: HTTP server failed to start and write port" >&2
    exit 1
fi
PORT="$(cat "$PORT_FILE")"
BASE_URL="http://127.0.0.1:$PORT"

create_manifest() {
    local target_file="$1"
    local version="$2"
    local bin_c_sha="$3"
    local bin_u_sha="$4"
    local geoip_sha="$5"
    local geosite_sha="$6"
    local asn_sha="$7"
    local url_prefix="$8"

    cat << EOF > "$target_file"
{
    "version": "$version",
    "binary": {
        "assetName": "mihomo-linux-amd64.gz",
        "url": "$url_prefix/mihomo-linux-amd64.gz",
        "compressedSha256": "$bin_c_sha",
        "uncompressedSha256": "$bin_u_sha"
    },
    "geodata": {
        "geoip.dat": {
            "url": "$url_prefix/geoip.dat",
            "sha256": "$geoip_sha"
        },
        "geosite.dat": {
            "url": "$url_prefix/geosite.dat",
            "sha256": "$geosite_sha"
        },
        "ASN.mmdb": {
            "url": "$url_prefix/ASN.mmdb",
            "sha256": "$asn_sha"
        }
    }
}
EOF
}

MANIFEST_STANDARD="$TMP_DIR/manifest_standard.json"
create_manifest "$MANIFEST_STANDARD" "v1.19.29" \
    "$BIN_COMPRESSED_SHA" "$BIN_UNCOMPRESSED_SHA" \
    "$GEOIP_SHA" "$GEOSITE_SHA" "$ASN_SHA" "$BASE_URL"

MANIFEST_SHA="$(sha256sum "$MANIFEST_STANDARD" | awk '{print $1}')"

# Tree snapshot helper: outputs relative path, mode, and sha256
snapshot_tree() {
    local root="$1"
    if [ ! -d "$root" ]; then
        return 0
    fi
    (
        cd "$root" && \
        find . -mindepth 1 | LC_ALL=C sort | while IFS= read -r rel; do
            local mode
            mode="$(stat -c "%a %F" "$rel" 2>/dev/null || stat -f "%p %HT" "$rel")"
            if [ -f "$rel" ] && [ ! -L "$rel" ]; then
                local sha
                sha="$(sha256sum "$rel" | awk '{print $1}')"
                echo "FILE $rel $mode $sha"
            elif [ -L "$rel" ]; then
                local target
                target="$(readlink "$rel")"
                echo "LINK $rel $mode -> $target"
            elif [ -d "$rel" ]; then
                echo "DIR $rel $mode"
            else
                echo "OTHER $rel $mode"
            fi
        done
    )
}

# Allow mock HTTP server for bootstrap
export ACCEPTANCE_ALLOW_HTTP="1"

echo "=== Scenario 1: Fresh Download ==="
export MIHOMO_ACCEPTANCE_CACHE_ROOT="$TMP_DIR/cache_s1"
mkdir -p "$MIHOMO_ACCEPTANCE_CACHE_ROOT"
EXPECTED_CACHE_DIR="$MIHOMO_ACCEPTANCE_CACHE_ROOT/v1.19.29/$MANIFEST_SHA"
> "$REQUESTS_LOG"
bash "$BOOTSTRAP_SCRIPT" "$MANIFEST_STANDARD"

if [ ! -f "$EXPECTED_CACHE_DIR/.bootstrap_complete" ]; then
    echo "FAIL: .bootstrap_complete missing in $EXPECTED_CACHE_DIR" >&2
    exit 1
fi
for f in "mihomo:$BIN_UNCOMPRESSED_SHA" "geoip.dat:$GEOIP_SHA" "geosite.dat:$GEOSITE_SHA" "ASN.mmdb:$ASN_SHA"; do
    fname="${f%%:*}"
    fsha="${f#*:}"
    actual="$(sha256sum "$EXPECTED_CACHE_DIR/$fname" | awk '{print $1}')"
    if [ "$actual" != "$fsha" ]; then
        echo "FAIL: $fname SHA mismatch: expected $fsha, got $actual" >&2
        exit 1
    fi
done
REQ_COUNT="$(grep -c "GET " "$REQUESTS_LOG" || true)"
if [ "$REQ_COUNT" -ne 4 ]; then
    echo "FAIL: Expected 4 HTTP requests on fresh download, got $REQ_COUNT" >&2
    exit 1
fi
echo "PASS: Scenario 1 (Fresh Download)"

echo "=== Scenario 2: Cache Hit ==="
export MIHOMO_ACCEPTANCE_CACHE_ROOT="$TMP_DIR/cache_s1"
> "$REQUESTS_LOG"
bash "$BOOTSTRAP_SCRIPT" "$MANIFEST_STANDARD"
REQ_COUNT="$(grep -c "GET " "$REQUESTS_LOG" || true)"
if [ "$REQ_COUNT" -ne 0 ]; then
    echo "FAIL: Expected 0 HTTP requests on cache hit, got $REQ_COUNT" >&2
    exit 1
fi
echo "PASS: Scenario 2 (Cache Hit)"

echo "=== Scenario 3: Corrupted Binary Recovery & Quarantine Retention ==="
export MIHOMO_ACCEPTANCE_CACHE_ROOT="$TMP_DIR/cache_s3"
mkdir -p "$MIHOMO_ACCEPTANCE_CACHE_ROOT"
S3_CACHE_DIR="$MIHOMO_ACCEPTANCE_CACHE_ROOT/v1.19.29/$MANIFEST_SHA"
# 1. Fresh download to establish known initial cache in isolated cache_s3
bash "$BOOTSTRAP_SCRIPT" "$MANIFEST_STANDARD" >/dev/null

# 2. Introduce corruption in mihomo binary
echo "corrupt binary data" > "$S3_CACHE_DIR/mihomo"
S3_CORRUPTED_PRE="$TMP_DIR/s3_corrupted_pre.tree"
snapshot_tree "$S3_CACHE_DIR" > "$S3_CORRUPTED_PRE"

> "$REQUESTS_LOG"
bash "$BOOTSTRAP_SCRIPT" "$MANIFEST_STANDARD"

ACTUAL_SHA="$(sha256sum "$S3_CACHE_DIR/mihomo" | awk '{print $1}')"
if [ "$ACTUAL_SHA" != "$BIN_UNCOMPRESSED_SHA" ]; then
    echo "FAIL: mihomo was not repaired after corruption" >&2
    exit 1
fi
REQ_COUNT="$(grep -c "GET " "$REQUESTS_LOG" || true)"
if [ "$REQ_COUNT" -ne 4 ]; then
    echo "FAIL: Expected 4 HTTP requests to repair corrupted cache, got $REQ_COUNT" >&2
    exit 1
fi

# 3. Verify quarantine directory exists and matches pre-repair corrupted snapshot
mapfile -d '' S3_Q_DIRS < <(find "$MIHOMO_ACCEPTANCE_CACHE_ROOT" -maxdepth 1 -name "quarantine.*" -print0)
if [ "${#S3_Q_DIRS[@]}" -ne 1 ]; then
    echo "FAIL: Expected exactly 1 quarantine directory in Scenario 3, got ${#S3_Q_DIRS[@]}" >&2
    exit 1
fi
S3_Q_TREE="$TMP_DIR/s3_quarantine.tree"
snapshot_tree "${S3_Q_DIRS[0]}" > "$S3_Q_TREE"
if ! cmp -s "$S3_CORRUPTED_PRE" "$S3_Q_TREE"; then
    echo "FAIL: Quarantine contents do not match pre-repair corrupted snapshot!" >&2
    diff -u "$S3_CORRUPTED_PRE" "$S3_Q_TREE" >&2
    exit 1
fi

# 4. Retention verification: subsequent bootstrap runs must NOT delete quarantine
bash "$BOOTSTRAP_SCRIPT" "$MANIFEST_STANDARD" >/dev/null
mapfile -d '' S3_Q_DIRS_AFTER < <(find "$MIHOMO_ACCEPTANCE_CACHE_ROOT" -maxdepth 1 -name "quarantine.*" -print0)
if [ "${#S3_Q_DIRS_AFTER[@]}" -ne 1 ]; then
    echo "FAIL: Quarantine was deleted by subsequent bootstrap run! Retention policy violated." >&2
    exit 1
fi
echo "PASS: Scenario 3 (Corrupted Binary Recovery & Quarantine Retention)"

echo "=== Scenario 4: Checksum Mismatch Failure ==="
export MIHOMO_ACCEPTANCE_CACHE_ROOT="$TMP_DIR/cache_s4"
mkdir -p "$MIHOMO_ACCEPTANCE_CACHE_ROOT"
MANIFEST_BAD="$TMP_DIR/manifest_bad.json"
create_manifest "$MANIFEST_BAD" "v1.19.29" \
    "0000000000000000000000000000000000000000000000000000000000000000" "$BIN_UNCOMPRESSED_SHA" \
    "$GEOIP_SHA" "$GEOSITE_SHA" "$ASN_SHA" "$BASE_URL"
BAD_SHA="$(sha256sum "$MANIFEST_BAD" | awk '{print $1}')"
BAD_CACHE_DIR="$MIHOMO_ACCEPTANCE_CACHE_ROOT/v1.19.29/$BAD_SHA"

if bash "$BOOTSTRAP_SCRIPT" "$MANIFEST_BAD" 2>/dev/null; then
    echo "FAIL: Expected bootstrap to fail on checksum mismatch" >&2
    exit 1
fi
if [ -d "$BAD_CACHE_DIR" ]; then
    echo "FAIL: Cache directory should not exist after checksum failure" >&2
    exit 1
fi
STAGING_COUNT="$(find "$MIHOMO_ACCEPTANCE_CACHE_ROOT" -maxdepth 1 -name "staging.*" | wc -l)"
if [ "$STAGING_COUNT" -ne 0 ]; then
    echo "FAIL: Lingering staging directories found after failure: $STAGING_COUNT" >&2
    exit 1
fi
echo "PASS: Scenario 4 (Checksum Mismatch Failure)"

echo "=== Scenario 5: Network Failure ==="
export MIHOMO_ACCEPTANCE_CACHE_ROOT="$TMP_DIR/cache_s5"
mkdir -p "$MIHOMO_ACCEPTANCE_CACHE_ROOT"
MANIFEST_UNREACHABLE="$TMP_DIR/manifest_unreachable.json"
create_manifest "$MANIFEST_UNREACHABLE" "v1.19.29" \
    "$BIN_COMPRESSED_SHA" "$BIN_UNCOMPRESSED_SHA" \
    "$GEOIP_SHA" "$GEOSITE_SHA" "$ASN_SHA" "http://127.0.0.1:1"
UNREACHABLE_SHA="$(sha256sum "$MANIFEST_UNREACHABLE" | awk '{print $1}')"
UNREACHABLE_CACHE_DIR="$MIHOMO_ACCEPTANCE_CACHE_ROOT/v1.19.29/$UNREACHABLE_SHA"

if bash "$BOOTSTRAP_SCRIPT" "$MANIFEST_UNREACHABLE" 2>/dev/null; then
    echo "FAIL: Expected bootstrap to fail on unreachable server" >&2
    exit 1
fi
if [ -d "$UNREACHABLE_CACHE_DIR" ]; then
    echo "FAIL: Cache directory should not exist after network failure" >&2
    exit 1
fi
STAGING_COUNT="$(find "$MIHOMO_ACCEPTANCE_CACHE_ROOT" -maxdepth 1 -name "staging.*" | wc -l)"
if [ "$STAGING_COUNT" -ne 0 ]; then
    echo "FAIL: Lingering staging directories found after network failure: $STAGING_COUNT" >&2
    exit 1
fi
echo "PASS: Scenario 5 (Network Failure)"

echo "=== Scenario 6: Manifest Mismatch / Multiple Versions ==="
export MIHOMO_ACCEPTANCE_CACHE_ROOT="$TMP_DIR/cache_s6"
mkdir -p "$MIHOMO_ACCEPTANCE_CACHE_ROOT"
bash "$BOOTSTRAP_SCRIPT" "$MANIFEST_STANDARD"

MANIFEST_V2="$TMP_DIR/manifest_v2.json"
create_manifest "$MANIFEST_V2" "v1.20.0" \
    "$BIN_COMPRESSED_SHA" "$BIN_UNCOMPRESSED_SHA" \
    "$GEOIP_SHA" "$GEOSITE_SHA" "$ASN_SHA" "$BASE_URL"
V2_SHA="$(sha256sum "$MANIFEST_V2" | awk '{print $1}')"
V2_CACHE_DIR="$MIHOMO_ACCEPTANCE_CACHE_ROOT/v1.20.0/$V2_SHA"
ORIG_CACHE_DIR="$MIHOMO_ACCEPTANCE_CACHE_ROOT/v1.19.29/$MANIFEST_SHA"

bash "$BOOTSTRAP_SCRIPT" "$MANIFEST_V2"
if [ ! -f "$V2_CACHE_DIR/.bootstrap_complete" ]; then
    echo "FAIL: v2 cache directory missing .bootstrap_complete" >&2
    exit 1
fi
if [ ! -f "$ORIG_CACHE_DIR/.bootstrap_complete" ]; then
    echo "FAIL: original cache directory was altered by v2 bootstrap" >&2
    exit 1
fi
echo "PASS: Scenario 6 (Manifest Mismatch / Multiple Versions)"

echo "=== Scenario 7: Concurrent Runs (flock safety) ==="
export MIHOMO_ACCEPTANCE_CACHE_ROOT="$TMP_DIR/cache_s7"
mkdir -p "$MIHOMO_ACCEPTANCE_CACHE_ROOT"
MANIFEST_CONCURRENT="$TMP_DIR/manifest_concurrent.json"
create_manifest "$MANIFEST_CONCURRENT" "v1.21.0" \
    "$BIN_COMPRESSED_SHA" "$BIN_UNCOMPRESSED_SHA" \
    "$GEOIP_SHA" "$GEOSITE_SHA" "$ASN_SHA" "$BASE_URL"
CONCURRENT_SHA="$(sha256sum "$MANIFEST_CONCURRENT" | awk '{print $1}')"
CONCURRENT_CACHE_DIR="$MIHOMO_ACCEPTANCE_CACHE_ROOT/v1.21.0/$CONCURRENT_SHA"

bash "$BOOTSTRAP_SCRIPT" "$MANIFEST_CONCURRENT" & pid1=$!
bash "$BOOTSTRAP_SCRIPT" "$MANIFEST_CONCURRENT" & pid2=$!
bash "$BOOTSTRAP_SCRIPT" "$MANIFEST_CONCURRENT" & pid3=$!

wait $pid1
wait $pid2
wait $pid3

if [ ! -f "$CONCURRENT_CACHE_DIR/.bootstrap_complete" ]; then
    echo "FAIL: Concurrent cache directory missing .bootstrap_complete" >&2
    exit 1
fi
STAGING_COUNT="$(find "$MIHOMO_ACCEPTANCE_CACHE_ROOT" -maxdepth 1 -name "staging.*" | wc -l)"
if [ "$STAGING_COUNT" -ne 0 ]; then
    echo "FAIL: Lingering staging directories found after concurrent runs: $STAGING_COUNT" >&2
    exit 1
fi
echo "PASS: Scenario 7 (Concurrent Runs)"

echo "=== Scenario 8: Path Traversal Rejection ==="
export MIHOMO_ACCEPTANCE_CACHE_ROOT="$TMP_DIR/cache_s8"
mkdir -p "$MIHOMO_ACCEPTANCE_CACHE_ROOT"
MANIFEST_EVIL="$TMP_DIR/manifest_evil.json"
create_manifest "$MANIFEST_EVIL" "../../../../tmp/evil" \
    "$BIN_COMPRESSED_SHA" "$BIN_UNCOMPRESSED_SHA" \
    "$GEOIP_SHA" "$GEOSITE_SHA" "$ASN_SHA" "$BASE_URL"

SNAPSHOT_BEFORE="$TMP_DIR/s8_outside_before.tree"
snapshot_tree "$TMP_DIR" > "$SNAPSHOT_BEFORE"

if bash "$BOOTSTRAP_SCRIPT" "$MANIFEST_EVIL" 2>/dev/null; then
    echo "FAIL: Expected bootstrap to reject path traversal in version" >&2
    exit 1
fi

SNAPSHOT_AFTER="$TMP_DIR/s8_outside_after.tree"
snapshot_tree "$TMP_DIR" > "$SNAPSHOT_AFTER"
# Filter out expected test log files/manifests created during test
diff -u <(grep -v "cache_s8\|s8_" "$SNAPSHOT_BEFORE") <(grep -v "cache_s8\|s8_" "$SNAPSHOT_AFTER") || {
    echo "FAIL: Files outside cache root were modified or created!" >&2
    exit 1
}
echo "PASS: Scenario 8 (Path Traversal Rejection)"

echo "=== Scenario 9: Malformed JSON Manifest ==="
export MIHOMO_ACCEPTANCE_CACHE_ROOT="$TMP_DIR/cache_s9"
mkdir -p "$MIHOMO_ACCEPTANCE_CACHE_ROOT"
MANIFEST_MALFORMED="$TMP_DIR/manifest_malformed.json"
echo "{not valid json" > "$MANIFEST_MALFORMED"

if bash "$BOOTSTRAP_SCRIPT" "$MANIFEST_MALFORMED" 2>/dev/null; then
    echo "FAIL: Expected bootstrap to fail on malformed JSON" >&2
    exit 1
fi
STAGING_COUNT="$(find "$MIHOMO_ACCEPTANCE_CACHE_ROOT" -maxdepth 1 -name "staging.*" | wc -l)"
if [ "$STAGING_COUNT" -ne 0 ]; then
    echo "FAIL: Staging directories found after malformed JSON failure: $STAGING_COUNT" >&2
    exit 1
fi
echo "PASS: Scenario 9 (Malformed JSON Manifest)"

echo "=== Scenario 10: Missing Manifest Field ==="
export MIHOMO_ACCEPTANCE_CACHE_ROOT="$TMP_DIR/cache_s10"
mkdir -p "$MIHOMO_ACCEPTANCE_CACHE_ROOT"
MANIFEST_INCOMPLETE="$TMP_DIR/manifest_incomplete.json"
echo '{"version":"v1.19.29"}' > "$MANIFEST_INCOMPLETE"

if bash "$BOOTSTRAP_SCRIPT" "$MANIFEST_INCOMPLETE" 2>/dev/null; then
    echo "FAIL: Expected bootstrap to fail on incomplete manifest" >&2
    exit 1
fi
echo "PASS: Scenario 10 (Missing Manifest Field)"

echo "=== Scenario 11: HTTP URL Rejected Without Flag ==="
export MIHOMO_ACCEPTANCE_CACHE_ROOT="$TMP_DIR/cache_s11"
mkdir -p "$MIHOMO_ACCEPTANCE_CACHE_ROOT"
unset ACCEPTANCE_ALLOW_HTTP || true

if bash "$BOOTSTRAP_SCRIPT" "$MANIFEST_STANDARD" 2>/dev/null; then
    echo "FAIL: Expected bootstrap to reject HTTP without ACCEPTANCE_ALLOW_HTTP=1" >&2
    exit 1
fi
echo "PASS: Scenario 11 (HTTP URL Rejected Without Flag)"

echo "=== Scenario 12: HTTP URL Accepted With Flag ==="
export MIHOMO_ACCEPTANCE_CACHE_ROOT="$TMP_DIR/cache_s12"
mkdir -p "$MIHOMO_ACCEPTANCE_CACHE_ROOT"
export ACCEPTANCE_ALLOW_HTTP="1"

if ! bash "$BOOTSTRAP_SCRIPT" "$MANIFEST_STANDARD" >/dev/null 2>&1; then
    echo "FAIL: Expected bootstrap to accept HTTP when ACCEPTANCE_ALLOW_HTTP=1" >&2
    exit 1
fi
echo "PASS: Scenario 12 (HTTP URL Accepted With Flag)"

echo "=== Scenario 13: Publication Failure Rollback (Byte-For-Byte Restore) ==="
export MIHOMO_ACCEPTANCE_CACHE_ROOT="$TMP_DIR/cache_s13"
mkdir -p "$MIHOMO_ACCEPTANCE_CACHE_ROOT"
S13_CACHE_DIR="$MIHOMO_ACCEPTANCE_CACHE_ROOT/v1.19.29/$MANIFEST_SHA"

# 1. Bootstrap valid cache first
bash "$BOOTSTRAP_SCRIPT" "$MANIFEST_STANDARD" >/dev/null

# 2. Add marker and remove .bootstrap_complete to trigger transaction without modifying file content
touch "$S13_CACHE_DIR/pre_trans_marker"
rm -f "$S13_CACHE_DIR/.bootstrap_complete"

# 3. Take pre-transaction snapshot NOW (reflects exact state before transaction)
S13_PRE="$TMP_DIR/s13_pre.tree"
snapshot_tree "$S13_CACHE_DIR" > "$S13_PRE"

# 4. Run bootstrap with failpoint to break publication AFTER quarantine
set +e
_BOOTSTRAP_FAIL_PUBLISH=1 bash "$BOOTSTRAP_SCRIPT" "$MANIFEST_STANDARD" 2>"$TMP_DIR/s13_err"
S13_RC=$?
set -e

if [ "$S13_RC" -ne 1 ]; then
    echo "FAIL: Expected exit code 1 on publication failure, got $S13_RC" >&2
    exit 1
fi

# 5. Verify staging count is 0
STAGING_COUNT="$(find "$MIHOMO_ACCEPTANCE_CACHE_ROOT" -maxdepth 1 -name "staging.*" | wc -l)"
if [ "$STAGING_COUNT" -ne 0 ]; then
    echo "FAIL: Staging directory lingering after publication failure: $STAGING_COUNT" >&2
    exit 1
fi

# 6. Verify quarantine count is 0 (clean restore)
mapfile -d '' S13_Q_DIRS < <(find "$MIHOMO_ACCEPTANCE_CACHE_ROOT" -maxdepth 1 -name "quarantine.*" -print0)
if [ "${#S13_Q_DIRS[@]}" -ne 0 ]; then
    echo "FAIL: Quarantine directory should have been restored, but found ${#S13_Q_DIRS[@]}" >&2
    exit 1
fi

# 7. Verify byte-for-byte, mode, and structure restore against pre-transaction snapshot
S13_POST="$TMP_DIR/s13_post.tree"
snapshot_tree "$S13_CACHE_DIR" > "$S13_POST"
if ! cmp -s "$S13_PRE" "$S13_POST"; then
    echo "FAIL: Post-rollback cache does not match pre-transaction snapshot!" >&2
    diff -u "$S13_PRE" "$S13_POST" >&2
    exit 1
fi
echo "PASS: Scenario 13 (Publication Failure Rollback)"

echo "=== Scenario 14: Recovery Failure (Exit 2 & Quarantine Preservation) ==="
export MIHOMO_ACCEPTANCE_CACHE_ROOT="$TMP_DIR/cache_s14"
mkdir -p "$MIHOMO_ACCEPTANCE_CACHE_ROOT"
S14_CACHE_DIR="$MIHOMO_ACCEPTANCE_CACHE_ROOT/v1.19.29/$MANIFEST_SHA"

# 1. Bootstrap valid cache
bash "$BOOTSTRAP_SCRIPT" "$MANIFEST_STANDARD" >/dev/null

# 2. Add marker and remove .bootstrap_complete to trigger transaction
touch "$S14_CACHE_DIR/pre_trans_marker"
rm -f "$S14_CACHE_DIR/.bootstrap_complete"

# 3. Take pre-transaction snapshot NOW
S14_PRE="$TMP_DIR/s14_pre.tree"
snapshot_tree "$S14_CACHE_DIR" > "$S14_PRE"

# 4. Run with both failpoints active
set +e
_BOOTSTRAP_FAIL_PUBLISH=1 _BOOTSTRAP_FAIL_RESTORE=1 bash "$BOOTSTRAP_SCRIPT" "$MANIFEST_STANDARD" 2>"$TMP_DIR/s14_err"
S14_RC=$?
set -e

if [ "$S14_RC" -ne 2 ]; then
    echo "FAIL: Expected exit code 2 on recovery failure, got $S14_RC" >&2
    exit 1
fi

if ! grep -q "RECOVERY-REQUIRED: Failed to restore quarantine" "$TMP_DIR/s14_err"; then
    echo "FAIL: Expected RECOVERY-REQUIRED message in stderr" >&2
    cat "$TMP_DIR/s14_err" >&2
    exit 1
fi

# 5. Staging must be cleaned up
STAGING_COUNT="$(find "$MIHOMO_ACCEPTANCE_CACHE_ROOT" -maxdepth 1 -name "staging.*" | wc -l)"
if [ "$STAGING_COUNT" -ne 0 ]; then
    echo "FAIL: Staging directory lingering after recovery failure: $STAGING_COUNT" >&2
    exit 1
fi

# 6. Target cache directory must NOT exist
if [ -d "$S14_CACHE_DIR" ]; then
    echo "FAIL: Destination cache dir exists despite restore failure" >&2
    exit 1
fi

# 7. Exactly one quarantine directory must exist and match pre-transaction snapshot
mapfile -d '' S14_Q_DIRS < <(find "$MIHOMO_ACCEPTANCE_CACHE_ROOT" -maxdepth 1 -name "quarantine.*" -print0)
if [ "${#S14_Q_DIRS[@]}" -ne 1 ]; then
    echo "FAIL: Expected exactly 1 quarantine directory, got ${#S14_Q_DIRS[@]}" >&2
    exit 1
fi

S14_Q_TREE="$TMP_DIR/s14_quarantine.tree"
snapshot_tree "${S14_Q_DIRS[0]}" > "$S14_Q_TREE"
if ! cmp -s "$S14_PRE" "$S14_Q_TREE"; then
    echo "FAIL: Quarantine contents do not match pre-transaction snapshot!" >&2
    diff -u "$S14_PRE" "$S14_Q_TREE" >&2
    exit 1
fi
echo "PASS: Scenario 14 (Recovery Failure)"

echo "=== Scenario 15: Missing Tool Preflight (Single Source & Isolated PATH) ==="
mapfile -t REQ_CMDS < <(bash "$BOOTSTRAP_SCRIPT" --list-required-cmds | tr ' ' '\n')
BASH_BIN="$(command -v bash)"

# 1. Automated static analysis: verify exact parity between invoked commands and REQUIRED_CMDS
python3 "$REPO_ROOT/scripts/tests/verify_bootstrap_commands.py"
python3 -m unittest "$REPO_ROOT/scripts/tests/test_verify_bootstrap_commands.py"

CRITICAL_TOOLS=(curl python3 sha256sum gzip mktemp flock mv dirname rm sleep)
for tool in "${CRITICAL_TOOLS[@]}"; do
    ISOLATED_DIR="$(mktemp -d "$TMP_DIR/isolated_bin_${tool}.XXXXXX")"
    for cmd in "${REQ_CMDS[@]}"; do
        if [ "$cmd" != "$tool" ]; then
            CMD_BIN="$(command -v "$cmd")"
            if [ -n "$CMD_BIN" ]; then
                ln -sf "$CMD_BIN" "$ISOLATED_DIR/$cmd"
            fi
        fi
    done
    set +e
    PATH="$ISOLATED_DIR" "$BASH_BIN" "$BOOTSTRAP_SCRIPT" "$MANIFEST_STANDARD" 2>"$TMP_DIR/s15_${tool}_err"
    TOOL_RC=$?
    set -e

    if [ "$TOOL_RC" -ne 1 ]; then
        echo "FAIL: Expected preflight to exit 1 for missing $tool, got $TOOL_RC" >&2
        exit 1
    fi
    if ! grep -q "Required command not found: $tool" "$TMP_DIR/s15_${tool}_err"; then
        echo "FAIL: Expected preflight error message for $tool" >&2
        cat "$TMP_DIR/s15_${tool}_err" >&2
        exit 1
    fi
    rm -rf "$ISOLATED_DIR"
done
echo "PASS: Scenario 15 (Missing Tool Preflight)"

echo "=== Scenario 16: Signal Interruption (SIGTERM / SIGINT exit code & staging cleanup) ==="
export MIHOMO_ACCEPTANCE_CACHE_ROOT="$TMP_DIR/cache_s16"
mkdir -p "$MIHOMO_ACCEPTANCE_CACHE_ROOT"
S16_CACHE_DIR="$MIHOMO_ACCEPTANCE_CACHE_ROOT/v1.19.29/$MANIFEST_SHA"

# 1. Establish valid prior cache to verify retention across interrupted executions
bash "$BOOTSTRAP_SCRIPT" "$MANIFEST_STANDARD" >/dev/null
S16_PRE="$TMP_DIR/s16_pre.tree"
snapshot_tree "$S16_CACHE_DIR" > "$S16_PRE"

# Create slow manifest for a different version so bootstrap is forced to download
MANIFEST_SLOW="$TMP_DIR/manifest_slow.json"
SLOW_URL="$BASE_URL/slow"
create_manifest "$MANIFEST_SLOW" "v1.22.0" \
    "$BIN_COMPRESSED_SHA" "$BIN_UNCOMPRESSED_SHA" \
    "$GEOIP_SHA" "$GEOSITE_SHA" "$ASN_SHA" "$SLOW_URL"
SLOW_SHA="$(sha256sum "$MANIFEST_SLOW" | awk '{print $1}')"
S16_SLOW_CACHE_DIR="$MIHOMO_ACCEPTANCE_CACHE_ROOT/v1.22.0/$SLOW_SHA"

# 2. Test SIGTERM (exit code 143)
set +e
bash "$BOOTSTRAP_SCRIPT" "$MANIFEST_SLOW" &
BOOTSTRAP_PID=$!

for _ in $(seq 1 50); do
    mapfile -d '' S16_STAGING_DIRS < <(find "$MIHOMO_ACCEPTANCE_CACHE_ROOT" -maxdepth 1 -name "staging.*" -print0)
    if [ "${#S16_STAGING_DIRS[@]}" -gt 0 ]; then
        break
    fi
    sleep 0.05
done

kill -TERM "$BOOTSTRAP_PID" 2>/dev/null || true
wait "$BOOTSTRAP_PID"
SIG_RC=$?
set -e

if [ "$SIG_RC" -ne 143 ]; then
    echo "FAIL: Expected bootstrap interrupted by SIGTERM to exit 143, got $SIG_RC" >&2
    exit 1
fi

STAGING_COUNT="$(find "$MIHOMO_ACCEPTANCE_CACHE_ROOT" -maxdepth 1 -name "staging.*" | wc -l)"
if [ "$STAGING_COUNT" -ne 0 ]; then
    echo "FAIL: Staging directory not cleaned up after SIGTERM: $STAGING_COUNT" >&2
    exit 1
fi

if [ -d "$S16_SLOW_CACHE_DIR" ]; then
    echo "FAIL: Target cache directory created despite SIGTERM interruption" >&2
    exit 1
fi

# Verify prior valid cache was preserved intact
S16_POST_TERM="$TMP_DIR/s16_post_term.tree"
snapshot_tree "$S16_CACHE_DIR" > "$S16_POST_TERM"
if ! cmp -s "$S16_PRE" "$S16_POST_TERM"; then
    echo "FAIL: Prior cache was modified after SIGTERM interruption!" >&2
    diff -u "$S16_PRE" "$S16_POST_TERM" >&2
    exit 1
fi

# 3. Test SIGINT (exit code 130) using Python subprocess to bypass non-interactive subshell SIG_IGN
python3 -c '
import subprocess, time, signal, sys, os
script = sys.argv[1]
manifest = sys.argv[2]
cache_root = sys.argv[3]
proc = subprocess.Popen(["bash", script, manifest])
found = False
for _ in range(100):
    time.sleep(0.05)
    if any(d.startswith("staging.") for d in os.listdir(cache_root)):
        found = True
        break
if not found:
    sys.exit("Staging dir did not appear in time")
proc.send_signal(signal.SIGINT)
rc = proc.wait()
if rc != 130:
    sys.exit(f"Expected rc 130 on SIGINT, got {rc}")
' "$BOOTSTRAP_SCRIPT" "$MANIFEST_SLOW" "$MIHOMO_ACCEPTANCE_CACHE_ROOT"

STAGING_COUNT="$(find "$MIHOMO_ACCEPTANCE_CACHE_ROOT" -maxdepth 1 -name "staging.*" | wc -l)"
if [ "$STAGING_COUNT" -ne 0 ]; then
    echo "FAIL: Staging directory not cleaned up after SIGINT: $STAGING_COUNT" >&2
    exit 1
fi

if [ -d "$S16_SLOW_CACHE_DIR" ]; then
    echo "FAIL: Target cache directory created despite SIGINT interruption" >&2
    exit 1
fi

# Verify prior valid cache was preserved intact
S16_POST_INT="$TMP_DIR/s16_post_int.tree"
snapshot_tree "$S16_CACHE_DIR" > "$S16_POST_INT"
if ! cmp -s "$S16_PRE" "$S16_POST_INT"; then
    echo "FAIL: Prior cache was modified after SIGINT interruption!" >&2
    diff -u "$S16_PRE" "$S16_POST_INT" >&2
    exit 1
fi

echo "PASS: Scenario 16 (Signal Interruption)"

echo "=== Scenario 17: Signal During Publication Window (SIGTERM & SIGINT state-aware rollback) ==="
export MIHOMO_ACCEPTANCE_CACHE_ROOT="$TMP_DIR/cache_s17"
mkdir -p "$MIHOMO_ACCEPTANCE_CACHE_ROOT"
S17_CACHE_DIR="$MIHOMO_ACCEPTANCE_CACHE_ROOT/v1.19.29/$MANIFEST_SHA"

# 1. Establish initial valid cache
bash "$BOOTSTRAP_SCRIPT" "$MANIFEST_STANDARD" >/dev/null

# 2. Test SIGTERM during old_quarantined window with explicit ready marker synchronization
echo "corrupted-for-sigterm" > "$S17_CACHE_DIR/mihomo"
S17_PRE_TERM="$TMP_DIR/s17_pre_term.tree"
snapshot_tree "$S17_CACHE_DIR" > "$S17_PRE_TERM"
S17_MARKER_TERM="$TMP_DIR/s17_marker_term"
rm -f "$S17_MARKER_TERM"

set +e
_BOOTSTRAP_PAUSE_OLD_QUARANTINED=5 _BOOTSTRAP_OLD_QUARANTINED_READY_MARKER="$S17_MARKER_TERM" bash "$BOOTSTRAP_SCRIPT" "$MANIFEST_STANDARD" &
S17_PID=$!

for _ in $(seq 1 100); do
    if [ -f "$S17_MARKER_TERM" ]; then
        break
    fi
    sleep 0.05
done

if [ ! -f "$S17_MARKER_TERM" ]; then
    echo "FAIL: Ready marker was not created within timeout for SIGTERM" >&2
    kill -9 "$S17_PID" 2>/dev/null || true
    exit 1
fi

kill -TERM "$S17_PID" 2>/dev/null || true
wait "$S17_PID"
S17_TERM_RC=$?
set -e

if [ "$S17_TERM_RC" -ne 143 ]; then
    echo "FAIL: Expected bootstrap interrupted by SIGTERM during publication to exit 143, got $S17_TERM_RC" >&2
    exit 1
fi

STAGING_COUNT="$(find "$MIHOMO_ACCEPTANCE_CACHE_ROOT" -maxdepth 1 -name "staging.*" | wc -l)"
if [ "$STAGING_COUNT" -ne 0 ]; then
    echo "FAIL: Staging directory not cleaned up after SIGTERM in publication: $STAGING_COUNT" >&2
    exit 1
fi

mapfile -d '' S17_Q_LEFTOVER < <(find "$MIHOMO_ACCEPTANCE_CACHE_ROOT" -maxdepth 1 -name "quarantine.*" -print0)
if [ "${#S17_Q_LEFTOVER[@]}" -ne 0 ]; then
    echo "FAIL: Quarantine directory was left behind instead of restored: ${#S17_Q_LEFTOVER[@]}" >&2
    exit 1
fi

S17_POST_TERM="$TMP_DIR/s17_post_term.tree"
snapshot_tree "$S17_CACHE_DIR" > "$S17_POST_TERM"
if ! cmp -s "$S17_PRE_TERM" "$S17_POST_TERM"; then
    echo "FAIL: Cache was not restored bit-for-bit after SIGTERM in publication!" >&2
    diff -u "$S17_PRE_TERM" "$S17_POST_TERM" >&2
    exit 1
fi

# 3. Test SIGINT during old_quarantined window with explicit ready marker synchronization
echo "corrupted-for-sigint" > "$S17_CACHE_DIR/mihomo"
S17_PRE_INT="$TMP_DIR/s17_pre_int.tree"
snapshot_tree "$S17_CACHE_DIR" > "$S17_PRE_INT"
S17_MARKER_INT="$TMP_DIR/s17_marker_int"
rm -f "$S17_MARKER_INT"

python3 -c '
import subprocess, time, signal, sys, os
script = sys.argv[1]
manifest = sys.argv[2]
cache_root = sys.argv[3]
marker = sys.argv[4]
env = os.environ.copy()
env["_BOOTSTRAP_PAUSE_OLD_QUARANTINED"] = "5"
env["_BOOTSTRAP_OLD_QUARANTINED_READY_MARKER"] = marker
proc = subprocess.Popen(["bash", script, manifest], env=env)
found = False
for _ in range(100):
    time.sleep(0.05)
    if os.path.exists(marker):
        found = True
        break
if not found:
    proc.kill()
    sys.exit("Quarantine ready marker did not appear in time")
proc.send_signal(signal.SIGINT)
rc = proc.wait()
if rc != 130:
    sys.exit(f"Expected rc 130 on SIGINT, got {rc}")
' "$BOOTSTRAP_SCRIPT" "$MANIFEST_STANDARD" "$MIHOMO_ACCEPTANCE_CACHE_ROOT" "$S17_MARKER_INT"

STAGING_COUNT="$(find "$MIHOMO_ACCEPTANCE_CACHE_ROOT" -maxdepth 1 -name "staging.*" | wc -l)"
if [ "$STAGING_COUNT" -ne 0 ]; then
    echo "FAIL: Staging directory not cleaned up after SIGINT in publication: $STAGING_COUNT" >&2
    exit 1
fi

mapfile -d '' S17_Q_LEFTOVER < <(find "$MIHOMO_ACCEPTANCE_CACHE_ROOT" -maxdepth 1 -name "quarantine.*" -print0)
if [ "${#S17_Q_LEFTOVER[@]}" -ne 0 ]; then
    echo "FAIL: Quarantine directory was left behind instead of restored after SIGINT: ${#S17_Q_LEFTOVER[@]}" >&2
    exit 1
fi

S17_POST_INT="$TMP_DIR/s17_post_int.tree"
snapshot_tree "$S17_CACHE_DIR" > "$S17_POST_INT"
if ! cmp -s "$S17_PRE_INT" "$S17_POST_INT"; then
    echo "FAIL: Cache was not restored bit-for-bit after SIGINT in publication!" >&2
    diff -u "$S17_PRE_INT" "$S17_POST_INT" >&2
    exit 1
fi

# 4. Test SIGTERM during old_quarantine_pending window BEFORE mv
echo "corrupted-for-pending-pre-mv" > "$S17_CACHE_DIR/mihomo"
S17_PRE_PENDING="$TMP_DIR/s17_pre_pending.tree"
snapshot_tree "$S17_CACHE_DIR" > "$S17_PRE_PENDING"
S17_MARKER_PENDING="$TMP_DIR/s17_marker_pending"
rm -f "$S17_MARKER_PENDING"

set +e
_BOOTSTRAP_PAUSE_OLD_QUARANTINE_PENDING=5 _BOOTSTRAP_OLD_QUARANTINE_PENDING_READY_MARKER="$S17_MARKER_PENDING" bash "$BOOTSTRAP_SCRIPT" "$MANIFEST_STANDARD" &
S17_PENDING_PID=$!

for _ in $(seq 1 100); do
    if [ -f "$S17_MARKER_PENDING" ]; then
        break
    fi
    sleep 0.05
done

if [ ! -f "$S17_MARKER_PENDING" ]; then
    echo "FAIL: Pending ready marker was not created within timeout" >&2
    kill -9 "$S17_PENDING_PID" 2>/dev/null || true
    exit 1
fi

kill -TERM "$S17_PENDING_PID" 2>/dev/null || true
wait "$S17_PENDING_PID"
S17_PENDING_RC=$?
set -e

if [ "$S17_PENDING_RC" -ne 143 ]; then
    echo "FAIL: Expected bootstrap interrupted during pending window to exit 143, got $S17_PENDING_RC" >&2
    exit 1
fi

STAGING_COUNT="$(find "$MIHOMO_ACCEPTANCE_CACHE_ROOT" -maxdepth 1 -name "staging.*" | wc -l)"
if [ "$STAGING_COUNT" -ne 0 ]; then
    echo "FAIL: Staging directory not cleaned up after SIGTERM in pending window: $STAGING_COUNT" >&2
    exit 1
fi

mapfile -d '' S17_Q_LEFTOVER < <(find "$MIHOMO_ACCEPTANCE_CACHE_ROOT" -maxdepth 1 -name "quarantine.*" -print0)
if [ "${#S17_Q_LEFTOVER[@]}" -ne 0 ]; then
    echo "FAIL: Lingering quarantine directory found after interruption in pending window: ${#S17_Q_LEFTOVER[@]}" >&2
    exit 1
fi

S17_POST_PENDING="$TMP_DIR/s17_post_pending.tree"
snapshot_tree "$S17_CACHE_DIR" > "$S17_POST_PENDING"
if ! cmp -s "$S17_PRE_PENDING" "$S17_POST_PENDING"; then
    echo "FAIL: Cache was modified after SIGTERM in pending window before mv!" >&2
    diff -u "$S17_PRE_PENDING" "$S17_POST_PENDING" >&2
    exit 1
fi

# 5. Test trapped signal during old_quarantine_pending immediately post-mv (atomic recovery window)
echo "corrupted-for-pending-post-mv" > "$S17_CACHE_DIR/mihomo"
S17_PRE_POST_MV="$TMP_DIR/s17_pre_post_mv.tree"
snapshot_tree "$S17_CACHE_DIR" > "$S17_PRE_POST_MV"

set +e
_BOOTSTRAP_TEST_SIGNAL_QUARANTINE_PENDING_POST_MV=1 bash "$BOOTSTRAP_SCRIPT" "$MANIFEST_STANDARD" 2>"$TMP_DIR/s17_post_mv.err"
S17_POST_MV_RC=$?
set -e

if [ "$S17_POST_MV_RC" -ne 143 ]; then
    echo "FAIL: Expected bootstrap interrupted post-mv in old_quarantine_pending to exit 143, got $S17_POST_MV_RC" >&2
    exit 1
fi

if ! grep -q "State-aware recovery: restored old cache from quarantine on exit" "$TMP_DIR/s17_post_mv.err"; then
    echo "FAIL: State-aware recovery was not triggered for signal post-mv in old_quarantine_pending!" >&2
    cat "$TMP_DIR/s17_post_mv.err" >&2
    exit 1
fi

STAGING_COUNT="$(find "$MIHOMO_ACCEPTANCE_CACHE_ROOT" -maxdepth 1 -name "staging.*" | wc -l)"
if [ "$STAGING_COUNT" -ne 0 ]; then
    echo "FAIL: Staging directory not cleaned up after post-mv signal in old_quarantine_pending: $STAGING_COUNT" >&2
    exit 1
fi

mapfile -d '' S17_Q_LEFTOVER < <(find "$MIHOMO_ACCEPTANCE_CACHE_ROOT" -maxdepth 1 -name "quarantine.*" -print0)
if [ "${#S17_Q_LEFTOVER[@]}" -ne 0 ]; then
    echo "FAIL: Quarantine directory was left behind instead of restored: ${#S17_Q_LEFTOVER[@]}" >&2
    exit 1
fi

S17_POST_POST_MV="$TMP_DIR/s17_post_post_mv.tree"
snapshot_tree "$S17_CACHE_DIR" > "$S17_POST_POST_MV"
if ! cmp -s "$S17_PRE_POST_MV" "$S17_POST_POST_MV"; then
    echo "FAIL: Cache was not restored bit-for-bit after post-mv signal in old_quarantine_pending!" >&2
    diff -u "$S17_PRE_POST_MV" "$S17_POST_POST_MV" >&2
    exit 1
fi

# 6. Test signal during old_quarantined with simultaneous staging cleanup failure (Priority 1 Cache Recovery)
echo "corrupted-for-cleanup-failure" > "$S17_CACHE_DIR/mihomo"
S17_PRE_CLEANUP_FAIL="$TMP_DIR/s17_pre_cleanup_fail.tree"
snapshot_tree "$S17_CACHE_DIR" > "$S17_PRE_CLEANUP_FAIL"
S17_MARKER_CLEANUP="$TMP_DIR/s17_marker_cleanup"
rm -f "$S17_MARKER_CLEANUP"

set +e
_BOOTSTRAP_FAIL_CLEANUP_STAGING=1 _BOOTSTRAP_PAUSE_OLD_QUARANTINED=5 _BOOTSTRAP_OLD_QUARANTINED_READY_MARKER="$S17_MARKER_CLEANUP" bash "$BOOTSTRAP_SCRIPT" "$MANIFEST_STANDARD" 2>"$TMP_DIR/s17_cleanup_fail.err" &
S17_CLEANUP_PID=$!

for _ in $(seq 1 100); do
    if [ -f "$S17_MARKER_CLEANUP" ]; then
        break
    fi
    sleep 0.05
done

if [ ! -f "$S17_MARKER_CLEANUP" ]; then
    echo "FAIL: Ready marker was not created within timeout for cleanup failure test" >&2
    kill -9 "$S17_CLEANUP_PID" 2>/dev/null || true
    exit 1
fi

kill -TERM "$S17_CLEANUP_PID" 2>/dev/null || true
wait "$S17_CLEANUP_PID"
S17_CLEANUP_RC=$?
set -e

if [ "$S17_CLEANUP_RC" -ne 143 ]; then
    echo "FAIL: Expected exit code 143 (unmasked by staging cleanup failure), got $S17_CLEANUP_RC" >&2
    exit 1
fi

if ! grep -q "WARN: Simulated failure removing staging directory" "$TMP_DIR/s17_cleanup_fail.err"; then
    echo "FAIL: Expected warning about simulated staging cleanup failure!" >&2
    cat "$TMP_DIR/s17_cleanup_fail.err" >&2
    exit 1
fi

if ! grep -q "State-aware recovery: restored old cache from quarantine on exit" "$TMP_DIR/s17_cleanup_fail.err"; then
    echo "FAIL: Cache recovery was not triggered when staging cleanup failed!" >&2
    cat "$TMP_DIR/s17_cleanup_fail.err" >&2
    exit 1
fi

S17_POST_CLEANUP_FAIL="$TMP_DIR/s17_post_cleanup_fail.tree"
snapshot_tree "$S17_CACHE_DIR" > "$S17_POST_CLEANUP_FAIL"
if ! cmp -s "$S17_PRE_CLEANUP_FAIL" "$S17_POST_CLEANUP_FAIL"; then
    echo "FAIL: Cache was not restored bit-for-bit despite staging cleanup failure!" >&2
    diff -u "$S17_PRE_CLEANUP_FAIL" "$S17_POST_CLEANUP_FAIL" >&2
    exit 1
fi

# Clean up simulated staging directory left behind by the test
find "$MIHOMO_ACCEPTANCE_CACHE_ROOT" -maxdepth 1 -name "staging.*" -exec rm -rf {} + 2>/dev/null || true

# 7. Test simultaneous staging cleanup failure AND recovery failure (Exit 2 Priority & Quarantine Preservation)
echo "corrupted-for-double-failure" > "$S17_CACHE_DIR/mihomo"
S17_PRE_DOUBLE="$TMP_DIR/s17_pre_double.tree"
snapshot_tree "$S17_CACHE_DIR" > "$S17_PRE_DOUBLE"
S17_MARKER_DOUBLE="$TMP_DIR/s17_marker_double"
rm -f "$S17_MARKER_DOUBLE"

set +e
_BOOTSTRAP_FAIL_CLEANUP_STAGING=1 _BOOTSTRAP_FAIL_RESTORE=1 _BOOTSTRAP_PAUSE_OLD_QUARANTINED=5 _BOOTSTRAP_OLD_QUARANTINED_READY_MARKER="$S17_MARKER_DOUBLE" bash "$BOOTSTRAP_SCRIPT" "$MANIFEST_STANDARD" 2>"$TMP_DIR/s17_double_fail.err" &
S17_DOUBLE_PID=$!

for _ in $(seq 1 100); do
    if [ -f "$S17_MARKER_DOUBLE" ]; then
        break
    fi
    sleep 0.05
done

if [ ! -f "$S17_MARKER_DOUBLE" ]; then
    echo "FAIL: Ready marker was not created within timeout for double failure test" >&2
    kill -9 "$S17_DOUBLE_PID" 2>/dev/null || true
    exit 1
fi

kill -TERM "$S17_DOUBLE_PID" 2>/dev/null || true
wait "$S17_DOUBLE_PID"
S17_DOUBLE_RC=$?
set -e

if [ "$S17_DOUBLE_RC" -ne 2 ]; then
    echo "FAIL: Expected recovery-required exit code 2 to take precedence over signal 143 and cleanup error, got $S17_DOUBLE_RC" >&2
    exit 1
fi

if ! grep -q "RECOVERY-REQUIRED: Quarantine preserved at" "$TMP_DIR/s17_double_fail.err"; then
    echo "FAIL: Expected RECOVERY-REQUIRED notice for failed restore!" >&2
    cat "$TMP_DIR/s17_double_fail.err" >&2
    exit 1
fi

mapfile -d '' S17_DOUBLE_Q < <(find "$MIHOMO_ACCEPTANCE_CACHE_ROOT" -maxdepth 1 -name "quarantine.*" -print0)
if [ "${#S17_DOUBLE_Q[@]}" -ne 1 ]; then
    echo "FAIL: Expected exactly 1 preserved quarantine directory after recovery failure, got ${#S17_DOUBLE_Q[@]}" >&2
    exit 1
fi

if [ -d "$S17_CACHE_DIR" ]; then
    echo "FAIL: Cache directory should not exist after recovery failure (quarantined and unrecovered)!" >&2
    exit 1
fi

S17_DOUBLE_Q_TREE="$TMP_DIR/s17_double_q.tree"
snapshot_tree "${S17_DOUBLE_Q[0]}" > "$S17_DOUBLE_Q_TREE"
if ! cmp -s "$S17_PRE_DOUBLE" "$S17_DOUBLE_Q_TREE"; then
    echo "FAIL: Preserved quarantine contents do not match pre-failure snapshot bit-for-bit!" >&2
    diff -u "$S17_PRE_DOUBLE" "$S17_DOUBLE_Q_TREE" >&2
    exit 1
fi

# Clean up preserved quarantine and staging for next tests
rm -rf "${S17_DOUBLE_Q[0]}"
find "$MIHOMO_ACCEPTANCE_CACHE_ROOT" -maxdepth 1 -name "staging.*" -exec rm -rf {} + 2>/dev/null || true

echo "PASS: Scenario 17 (Signal During Publication Window)"

# Verify production cache was never modified
if [ -d "$PROD_CACHE" ]; then
    find "$PROD_CACHE" -type f -exec sha256sum {} + | sort > "$PROD_AFTER"
else
    touch "$PROD_AFTER"
fi
if ! cmp -s "$PROD_BEFORE" "$PROD_AFTER"; then
    echo "FAIL: Production cache was modified during tests!" >&2
    exit 1
fi
echo "PASS: Production cache isolation confirmed"

echo "ALL 17 BOOTSTRAP ACCEPTANCE SCENARIOS PASSED SUCCESSFULLY!"

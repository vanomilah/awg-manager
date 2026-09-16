#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
VERIFIER="$REPO_ROOT/scripts/verify-mihomo-acceptance-json.py"

if [ ! -f "$VERIFIER" ]; then
    echo "ERROR: Verifier script not found at $VERIFIER" >&2
    exit 1
fi

TMP_DIR="$(mktemp -d /tmp/verifier-test.XXXXXX)"
trap 'rm -rf "$TMP_DIR"' EXIT

TARGET="TestSampleAcceptance"
PKG="github.com/hoaxisr/awg-manager/internal/sample"

echo "=== Scenario 1: Valid Pass ==="
cat << EOF > "$TMP_DIR/valid_pass.json"
{"Action":"start","Package":"$PKG"}
{"Action":"run","Package":"$PKG","Test":"$TARGET"}
{"Action":"output","Package":"$PKG","Test":"$TARGET","Output":"=== RUN   $TARGET\n"}
{"Action":"output","Package":"$PKG","Test":"$TARGET","Output":"--- PASS: $TARGET (0.01s)\n"}
{"Action":"pass","Package":"$PKG","Test":"$TARGET","Elapsed":0.01}
{"Action":"output","Package":"$PKG","Output":"PASS\n"}
{"Action":"pass","Package":"$PKG","Elapsed":0.02}
EOF

if ! python3 "$VERIFIER" --json "$TMP_DIR/valid_pass.json" --test "$TARGET" >/dev/null; then
    echo "FAIL: Valid pass scenario failed" >&2
    exit 1
fi
echo "PASS: Valid Pass"

echo "=== Scenario 2: Skipped Test ==="
cat << EOF > "$TMP_DIR/skipped.json"
{"Action":"start","Package":"$PKG"}
{"Action":"run","Package":"$PKG","Test":"$TARGET"}
{"Action":"output","Package":"$PKG","Test":"$TARGET","Output":"=== RUN   $TARGET\n"}
{"Action":"output","Package":"$PKG","Test":"$TARGET","Output":"    sample_test.go:10: skipping\n"}
{"Action":"skip","Package":"$PKG","Test":"$TARGET","Elapsed":0.00}
{"Action":"pass","Package":"$PKG","Elapsed":0.01}
EOF

if python3 "$VERIFIER" --json "$TMP_DIR/skipped.json" --test "$TARGET" 2>/dev/null; then
    echo "FAIL: Expected skipped test to be rejected" >&2
    exit 1
fi
echo "PASS: Skipped Test Rejected"

echo "=== Scenario 3: Failed Test ==="
cat << EOF > "$TMP_DIR/failed.json"
{"Action":"start","Package":"$PKG"}
{"Action":"run","Package":"$PKG","Test":"$TARGET"}
{"Action":"output","Package":"$PKG","Test":"$TARGET","Output":"=== RUN   $TARGET\n"}
{"Action":"output","Package":"$PKG","Test":"$TARGET","Output":"    sample_test.go:20: assertion failed\n"}
{"Action":"fail","Package":"$PKG","Test":"$TARGET","Elapsed":0.01}
{"Action":"fail","Package":"$PKG","Elapsed":0.02}
EOF

if python3 "$VERIFIER" --json "$TMP_DIR/failed.json" --test "$TARGET" 2>/dev/null; then
    echo "FAIL: Expected failed test to be rejected" >&2
    exit 1
fi
echo "PASS: Failed Test Rejected"

echo "=== Scenario 4: Missing Test ==="
cat << EOF > "$TMP_DIR/missing.json"
{"Action":"start","Package":"$PKG"}
{"Action":"run","Package":"$PKG","Test":"TestOther"}
{"Action":"pass","Package":"$PKG","Test":"TestOther","Elapsed":0.01}
{"Action":"pass","Package":"$PKG","Elapsed":0.02}
EOF

if python3 "$VERIFIER" --json "$TMP_DIR/missing.json" --test "$TARGET" 2>/dev/null; then
    echo "FAIL: Expected missing test to be rejected" >&2
    exit 1
fi
echo "PASS: Missing Test Rejected"

echo "=== Scenario 5: Package Failure ==="
cat << EOF > "$TMP_DIR/package_fail.json"
{"Action":"start","Package":"$PKG"}
{"Action":"run","Package":"$PKG","Test":"$TARGET"}
{"Action":"pass","Package":"$PKG","Test":"$TARGET","Elapsed":0.01}
{"Action":"fail","Package":"$PKG","Elapsed":0.02}
EOF

if python3 "$VERIFIER" --json "$TMP_DIR/package_fail.json" --test "$TARGET" 2>/dev/null; then
    echo "FAIL: Expected package failure to be rejected" >&2
    exit 1
fi
echo "PASS: Package Failure Rejected"

echo "=== Scenario 6: Empty Stream ==="
touch "$TMP_DIR/empty.json"
if python3 "$VERIFIER" --json "$TMP_DIR/empty.json" --test "$TARGET" 2>/dev/null; then
    echo "FAIL: Expected empty stream to be rejected" >&2
    exit 1
fi
echo "PASS: Empty Stream Rejected"

echo "=== Scenario 7: Malformed Non-JSON Only ==="
echo "not valid json lines" > "$TMP_DIR/malformed.json"
if python3 "$VERIFIER" --json "$TMP_DIR/malformed.json" --test "$TARGET" 2>/dev/null; then
    echo "FAIL: Expected malformed json to be rejected" >&2
    exit 1
fi
echo "PASS: Malformed Non-JSON Rejected"

echo "=== Scenario 8: Pass Without Prior Run ==="
cat << EOF > "$TMP_DIR/pass_without_run.json"
{"Action":"start","Package":"$PKG"}
{"Action":"pass","Package":"$PKG","Test":"$TARGET","Elapsed":0.01}
{"Action":"pass","Package":"$PKG","Elapsed":0.02}
EOF

if python3 "$VERIFIER" --json "$TMP_DIR/pass_without_run.json" --test "$TARGET" 2>/dev/null; then
    echo "FAIL: Expected pass without run to be rejected" >&2
    exit 1
fi
echo "PASS: Pass Without Prior Run Rejected"

echo "=== Scenario 9: Duplicate Test Run ==="
cat << EOF > "$TMP_DIR/dup_run.json"
{"Action":"start","Package":"$PKG"}
{"Action":"run","Package":"$PKG","Test":"$TARGET"}
{"Action":"run","Package":"$PKG","Test":"$TARGET"}
{"Action":"pass","Package":"$PKG","Test":"$TARGET","Elapsed":0.01}
{"Action":"pass","Package":"$PKG","Elapsed":0.02}
EOF

if python3 "$VERIFIER" --json "$TMP_DIR/dup_run.json" --test "$TARGET" 2>/dev/null; then
    echo "FAIL: Expected duplicate run to be rejected" >&2
    exit 1
fi
echo "PASS: Duplicate Test Run Rejected"

echo "=== Scenario 10: Duplicate Test Terminal ==="
cat << EOF > "$TMP_DIR/dup_pass.json"
{"Action":"start","Package":"$PKG"}
{"Action":"run","Package":"$PKG","Test":"$TARGET"}
{"Action":"pass","Package":"$PKG","Test":"$TARGET","Elapsed":0.01}
{"Action":"pass","Package":"$PKG","Test":"$TARGET","Elapsed":0.01}
{"Action":"pass","Package":"$PKG","Elapsed":0.02}
EOF

if python3 "$VERIFIER" --json "$TMP_DIR/dup_pass.json" --test "$TARGET" 2>/dev/null; then
    echo "FAIL: Expected duplicate pass to be rejected" >&2
    exit 1
fi
echo "PASS: Duplicate Test Terminal Rejected"

echo "=== Scenario 11: Output After Test Terminal ==="
cat << EOF > "$TMP_DIR/output_after_pass.json"
{"Action":"start","Package":"$PKG"}
{"Action":"run","Package":"$PKG","Test":"$TARGET"}
{"Action":"pass","Package":"$PKG","Test":"$TARGET","Elapsed":0.01}
{"Action":"output","Package":"$PKG","Test":"$TARGET","Output":"late output\n"}
{"Action":"pass","Package":"$PKG","Elapsed":0.02}
EOF

if python3 "$VERIFIER" --json "$TMP_DIR/output_after_pass.json" --test "$TARGET" 2>/dev/null; then
    echo "FAIL: Expected output after test terminal to be rejected" >&2
    exit 1
fi
echo "PASS: Output After Test Terminal Rejected"

echo "=== Scenario 12: Package Terminal Absent ==="
cat << EOF > "$TMP_DIR/no_pkg_terminal.json"
{"Action":"start","Package":"$PKG"}
{"Action":"run","Package":"$PKG","Test":"$TARGET"}
{"Action":"pass","Package":"$PKG","Test":"$TARGET","Elapsed":0.01}
EOF

if python3 "$VERIFIER" --json "$TMP_DIR/no_pkg_terminal.json" --test "$TARGET" 2>/dev/null; then
    echo "FAIL: Expected missing package terminal to be rejected" >&2
    exit 1
fi
echo "PASS: Package Terminal Absent Rejected"

echo "=== Scenario 13: Duplicate Package Terminal ==="
cat << EOF > "$TMP_DIR/dup_pkg_pass.json"
{"Action":"start","Package":"$PKG"}
{"Action":"run","Package":"$PKG","Test":"$TARGET"}
{"Action":"pass","Package":"$PKG","Test":"$TARGET","Elapsed":0.01}
{"Action":"pass","Package":"$PKG","Elapsed":0.02}
{"Action":"pass","Package":"$PKG","Elapsed":0.03}
EOF

if python3 "$VERIFIER" --json "$TMP_DIR/dup_pkg_pass.json" --test "$TARGET" 2>/dev/null; then
    echo "FAIL: Expected duplicate package pass to be rejected" >&2
    exit 1
fi
echo "PASS: Duplicate Package Terminal Rejected"

echo "=== Scenario 14: Package Terminal Arriving Before Test Terminal ==="
cat << EOF > "$TMP_DIR/pkg_pass_before_test.json"
{"Action":"start","Package":"$PKG"}
{"Action":"run","Package":"$PKG","Test":"$TARGET"}
{"Action":"pass","Package":"$PKG","Elapsed":0.02}
{"Action":"pass","Package":"$PKG","Test":"$TARGET","Elapsed":0.01}
EOF

if python3 "$VERIFIER" --json "$TMP_DIR/pkg_pass_before_test.json" --test "$TARGET" 2>/dev/null; then
    echo "FAIL: Expected package pass before test terminal to be rejected immediately" >&2
    exit 1
fi
echo "PASS: Package Terminal Before Test Terminal Rejected"

echo "=== Scenario 15: Name Collision Across Packages ==="
cat << EOF > "$TMP_DIR/collision.json"
{"Action":"start","Package":"pkg1"}
{"Action":"run","Package":"pkg1","Test":"$TARGET"}
{"Action":"pass","Package":"pkg1","Test":"$TARGET"}
{"Action":"pass","Package":"pkg1"}
{"Action":"start","Package":"pkg2"}
{"Action":"run","Package":"pkg2","Test":"$TARGET"}
{"Action":"pass","Package":"pkg2","Test":"$TARGET"}
{"Action":"pass","Package":"pkg2"}
EOF

if python3 "$VERIFIER" --json "$TMP_DIR/collision.json" --test "$TARGET" 2>/dev/null; then
    echo "FAIL: Expected package collision to be rejected" >&2
    exit 1
fi
echo "PASS: Name Collision Across Packages Rejected"

echo "=== Scenario 16: Unknown Action Encountered ==="
cat << EOF > "$TMP_DIR/unknown_action.json"
{"Action":"benchmark","Package":"$PKG"}
EOF

if python3 "$VERIFIER" --json "$TMP_DIR/unknown_action.json" --test "$TARGET" 2>/dev/null; then
    echo "FAIL: Expected unknown action to be rejected" >&2
    exit 1
fi
echo "PASS: Unknown Action Encountered Rejected"

echo "=== Scenario 17: Real NDJSON Fixture Verification ==="
REAL_FIXTURE="$REPO_ROOT/scripts/tests/fixtures/acceptance-go-test-pass.ndjson"
if [ ! -f "$REAL_FIXTURE" ]; then
    echo "FAIL: Real NDJSON fixture not found at $REAL_FIXTURE" >&2
    exit 1
fi

if ! python3 "$VERIFIER" --json "$REAL_FIXTURE" --test "TestValidateMihomoVersion_ExecutionScript" >/dev/null; then
    echo "FAIL: Real NDJSON fixture verification failed" >&2
    exit 1
fi
echo "PASS: Real NDJSON Fixture Verification"

echo "ALL 17 VERIFIER SCENARIOS PASSED SUCCESSFULLY!"

#!/usr/bin/env bash
set -euo pipefail

# 1. Definitive list of all external commands used by this script
REQUIRED_CMDS=(
    python3 curl gzip sha256sum awk chmod mv mkdir rmdir
    touch mktemp flock realpath dirname pwd rm sleep
)

# Sourced / helper inspection hook for test harness single source of truth
if [ "${1:-}" = "--list-required-cmds" ]; then
    echo "${REQUIRED_CMDS[*]}"
    exit 0
fi

# 2. Preflight: execute before ANY external command (uses bash builtins only)
for cmd in "${REQUIRED_CMDS[@]}"; do
    if ! command -v "$cmd" >/dev/null 2>&1; then
        echo "ERROR: Required command not found: $cmd" >&2
        exit 1
    fi
done

FIELDS_TMP=""
CLEANUP_STAGING_DIR=""
QUARANTINE_DIR=""
PUBLICATION_STATE="uninitialized"

cleanup() {
    local original_rc=$?
    local cleanup_rc=0
    trap - EXIT
    set +e

    # 1. PRIORITY 1: State-aware recovery if publication was interrupted after old cache was quarantined or during rename window
    if { [ "${PUBLICATION_STATE:-}" = "old_quarantine_pending" ] || [ "${PUBLICATION_STATE:-}" = "old_quarantined" ]; } && [ -n "${CACHE_DIR:-}" ]; then
        if [ ! -d "$CACHE_DIR" ]; then
            if [ "${_BOOTSTRAP_FAIL_RESTORE:-}" = "1" ]; then
                echo "FAILPOINT: Simulating restore failure on exit" >&2
                echo "RECOVERY-REQUIRED: Failed to restore quarantine $QUARANTINE_DIR -> $CACHE_DIR on exit" >&2
                echo "RECOVERY-REQUIRED: Quarantine preserved at $QUARANTINE_DIR" >&2
                PUBLICATION_STATE="restore_failed"
                exit 2
            fi
            if [ -n "$QUARANTINE_DIR" ] && [ -d "$QUARANTINE_DIR" ]; then
                if ! mv "$QUARANTINE_DIR" "$CACHE_DIR" 2>/dev/null; then
                    echo "RECOVERY-REQUIRED: Failed to restore quarantine $QUARANTINE_DIR -> $CACHE_DIR on exit" >&2
                    echo "RECOVERY-REQUIRED: Quarantine preserved at $QUARANTINE_DIR" >&2
                    PUBLICATION_STATE="restore_failed"
                    exit 2
                fi
                echo "State-aware recovery: restored old cache from quarantine on exit" >&2
                PUBLICATION_STATE="restored"
            else
                echo "RECOVERY-REQUIRED: Cache directory $CACHE_DIR is missing and quarantine directory $QUARANTINE_DIR is unavailable on exit" >&2
                PUBLICATION_STATE="restore_failed"
                exit 2
            fi
        fi
    fi

    # 2. PRIORITY 2: Best-effort temporary file/directory cleanup (never blocks or preempts recovery)
    if [ -n "$FIELDS_TMP" ] && [ -f "$FIELDS_TMP" ]; then
        if [ "${_BOOTSTRAP_FAIL_CLEANUP_FIELDS:-}" = "1" ]; then
            echo "WARN: Simulated failure removing temporary fields file $FIELDS_TMP" >&2
            cleanup_rc=1
        elif ! rm -f "$FIELDS_TMP" 2>/dev/null; then
            echo "WARN: Failed to remove temporary fields file $FIELDS_TMP" >&2
            cleanup_rc=1
        fi
    fi

    if [ -n "$CLEANUP_STAGING_DIR" ] && [ -d "$CLEANUP_STAGING_DIR" ]; then
        if [ "${_BOOTSTRAP_FAIL_CLEANUP_STAGING:-}" = "1" ]; then
            echo "WARN: Simulated failure removing staging directory $CLEANUP_STAGING_DIR" >&2
            cleanup_rc=1
        elif ! rm -rf "$CLEANUP_STAGING_DIR" 2>/dev/null; then
            echo "WARN: Failed to remove staging directory $CLEANUP_STAGING_DIR" >&2
            cleanup_rc=1
        fi
    fi

    # 3. Exit code policy: preserve non-zero original exit/signal code; otherwise use cleanup_rc
    if [ "$original_rc" -ne 0 ]; then
        exit "$original_rc"
    fi
    exit "$cleanup_rc"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

# 3. Directory resolution (safe now that dirname and pwd are confirmed present)
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
MANIFEST="${1:-$REPO_ROOT/scripts/mihomo-acceptance-manifest.json}"

if [ ! -f "$MANIFEST" ]; then
    echo "ERROR: Acceptance manifest not found at $MANIFEST" >&2
    exit 1
fi

MANIFEST_SHA="$(sha256sum "$MANIFEST" | awk '{print $1}')"

FIELDS_TMP="$(mktemp)"

if ! python3 "$REPO_ROOT/scripts/validate_manifest.py" "$MANIFEST" > "$FIELDS_TMP"; then
    echo "ERROR: Manifest validation failed" >&2
    exit 1
fi

# Verify stream ends with a NUL byte (trust boundary guard)
if ! python3 -c 'import sys; f=open(sys.argv[1], "rb").read(); sys.exit(0 if f and f.endswith(b"\x00") else 1)' "$FIELDS_TMP"; then
    echo "ERROR: Manifest validator output does not end with NUL delimiter" >&2
    exit 1
fi

# Read all fields with mapfile
mapfile -d '' -t fields < "$FIELDS_TMP"
rm -f "$FIELDS_TMP"
FIELDS_TMP=""

if [ "${#fields[@]}" -ne 11 ]; then
    echo "ERROR: Manifest validator produced ${#fields[@]} fields, expected exactly 11" >&2
    exit 1
fi

VERSION="${fields[0]}"
BIN_ASSET_NAME="${fields[1]}"
BIN_URL="${fields[2]}"
BIN_COMPRESSED_SHA="${fields[3]}"
BIN_UNCOMPRESSED_SHA="${fields[4]}"
GEOIP_URL="${fields[5]}"
GEOIP_SHA="${fields[6]}"
GEOSITE_URL="${fields[7]}"
GEOSITE_SHA="${fields[8]}"
ASN_URL="${fields[9]}"
ASN_SHA="${fields[10]}"

# Paths are computed strictly AFTER successful validation
CACHE_ROOT="${MIHOMO_ACCEPTANCE_CACHE_ROOT:-$REPO_ROOT/.cache/mihomo}"
if ! mkdir -p "$CACHE_ROOT"; then
    echo "ERROR: Failed to create cache root directory $CACHE_ROOT" >&2
    exit 1
fi

CANON_CACHE_ROOT="$(cd "$CACHE_ROOT" && pwd -P)"
CACHE_DIR="$CACHE_ROOT/$VERSION/$MANIFEST_SHA"
CANON_CACHE_DIR="$(realpath -m "$CACHE_DIR")"

# Path traversal guard
if [[ "$CANON_CACHE_DIR" != "$CANON_CACHE_ROOT"/* ]]; then
    echo "ERROR: Computed cache directory escapes cache root: $CACHE_DIR" >&2
    exit 1
fi

LOCK_FILE="$CACHE_ROOT/.lock.${MANIFEST_SHA}"
exec 200>"$LOCK_FILE"
flock -x 200

verify_file() {
    local file="$1"
    local expected="$2"
    if [ ! -f "$file" ] || [ ! -s "$file" ]; then
        return 1
    fi
    local actual
    actual="$(sha256sum "$file" | awk '{print $1}')"
    [ "$actual" = "$expected" ]
}

if [ -f "$CACHE_DIR/.bootstrap_complete" ] && \
   verify_file "$CACHE_DIR/mihomo" "$BIN_UNCOMPRESSED_SHA" && \
   verify_file "$CACHE_DIR/geoip.dat" "$GEOIP_SHA" && \
   verify_file "$CACHE_DIR/geosite.dat" "$GEOSITE_SHA" && \
   verify_file "$CACHE_DIR/ASN.mmdb" "$ASN_SHA"; then
    echo "Mihomo acceptance cache is up to date: $CACHE_DIR"
    exit 0
fi

STAGING_DIR="$(mktemp -d "$CACHE_ROOT/staging.XXXXXXXXXX")"
CLEANUP_STAGING_DIR="$STAGING_DIR"

echo "Bootstrapping Mihomo acceptance fixtures into cache ($VERSION / $MANIFEST_SHA)..."

echo "Downloading $BIN_ASSET_NAME..."
curl -fsSL --retry 3 --connect-timeout 10 --max-time 120 "$BIN_URL" -o "$STAGING_DIR/$BIN_ASSET_NAME"
if ! verify_file "$STAGING_DIR/$BIN_ASSET_NAME" "$BIN_COMPRESSED_SHA"; then
    echo "ERROR: Checksum mismatch for $BIN_ASSET_NAME" >&2
    exit 1
fi

echo "Decompressing $BIN_ASSET_NAME -> mihomo..."
gzip -dc "$STAGING_DIR/$BIN_ASSET_NAME" > "$STAGING_DIR/mihomo"
chmod +x "$STAGING_DIR/mihomo"
rm -f "$STAGING_DIR/$BIN_ASSET_NAME"

if ! verify_file "$STAGING_DIR/mihomo" "$BIN_UNCOMPRESSED_SHA"; then
    echo "ERROR: Checksum mismatch for uncompressed mihomo binary" >&2
    exit 1
fi

echo "Downloading geoip.dat..."
curl -fsSL --retry 3 --connect-timeout 10 --max-time 120 "$GEOIP_URL" -o "$STAGING_DIR/geoip.dat"
if ! verify_file "$STAGING_DIR/geoip.dat" "$GEOIP_SHA"; then
    echo "ERROR: Checksum mismatch for geoip.dat" >&2
    exit 1
fi

echo "Downloading geosite.dat..."
curl -fsSL --retry 3 --connect-timeout 10 --max-time 120 "$GEOSITE_URL" -o "$STAGING_DIR/geosite.dat"
if ! verify_file "$STAGING_DIR/geosite.dat" "$GEOSITE_SHA"; then
    echo "ERROR: Checksum mismatch for geosite.dat" >&2
    exit 1
fi

echo "Downloading ASN.mmdb..."
curl -fsSL --retry 3 --connect-timeout 10 --max-time 120 "$ASN_URL" -o "$STAGING_DIR/ASN.mmdb"
if ! verify_file "$STAGING_DIR/ASN.mmdb" "$ASN_SHA"; then
    echo "ERROR: Checksum mismatch for ASN.mmdb" >&2
    exit 1
fi

if ! touch "$STAGING_DIR/.bootstrap_complete"; then
    echo "ERROR: Failed to write completion marker" >&2
    exit 1
fi

QUARANTINE_DIR=""
PUBLICATION_STATE="prepared"

publish_cache() {
    if ! mkdir -p "$(dirname "$CACHE_DIR")"; then
        echo "ERROR: Failed to create parent directory for cache" >&2
        return 1
    fi

    # Transition 1: PREPARED -> OLD_QUARANTINE_PENDING -> OLD_QUARANTINED
    if [ -d "$CACHE_DIR" ]; then
        if ! QUARANTINE_DIR="$(mktemp -d "$CACHE_ROOT/quarantine.XXXXXXXXXX")"; then
            echo "ERROR: Failed to allocate quarantine directory" >&2
            return 1
        fi
        if ! rmdir "$QUARANTINE_DIR"; then
            echo "ERROR: Failed to clear quarantine reservation name" >&2
            return 1
        fi

        # Pre-arm recovery state BEFORE mv
        PUBLICATION_STATE="old_quarantine_pending"

        if [ -n "${_BOOTSTRAP_PAUSE_OLD_QUARANTINE_PENDING:-}" ]; then
            if [ -n "${_BOOTSTRAP_OLD_QUARANTINE_PENDING_READY_MARKER:-}" ]; then
                touch "${_BOOTSTRAP_OLD_QUARANTINE_PENDING_READY_MARKER}"
            fi
            echo "PAUSE_OLD_QUARANTINE_PENDING: Sleeping ${_BOOTSTRAP_PAUSE_OLD_QUARANTINE_PENDING}s in old_quarantine_pending state" >&2
            sleep "${_BOOTSTRAP_PAUSE_OLD_QUARANTINE_PENDING}"
        fi

        if ! mv "$CACHE_DIR" "$QUARANTINE_DIR"; then
            PUBLICATION_STATE="prepared"
            echo "ERROR: Failed to quarantine old cache directory" >&2
            return 1
        fi

        # Test hook to simulate immediate trapped signal post-mv while state is old_quarantine_pending
        if [ "${_BOOTSTRAP_TEST_SIGNAL_QUARANTINE_PENDING_POST_MV:-}" = "1" ]; then
            echo "FAILPOINT: Simulating trapped signal immediately post-mv while state is old_quarantine_pending" >&2
            kill -TERM $$
        fi

        PUBLICATION_STATE="old_quarantined"

        if [ -n "${_BOOTSTRAP_PAUSE_OLD_QUARANTINED:-}" ]; then
            if [ -n "${_BOOTSTRAP_OLD_QUARANTINED_READY_MARKER:-}" ]; then
                touch "${_BOOTSTRAP_OLD_QUARANTINED_READY_MARKER}"
            fi
            echo "PAUSE_OLD_QUARANTINED: Sleeping ${_BOOTSTRAP_PAUSE_OLD_QUARANTINED}s in old_quarantined state" >&2
            sleep "${_BOOTSTRAP_PAUSE_OLD_QUARANTINED}"
        fi
    fi

    # Transition 2: OLD_QUARANTINED -> COMMITTED (or failpoint)
    local publish_failed=0
    if [ "${_BOOTSTRAP_FAIL_PUBLISH:-}" = "1" ]; then
        echo "FAILPOINT: Simulating publication failure after quarantine" >&2
        publish_failed=1
    elif ! mv "$STAGING_DIR" "$CACHE_DIR"; then
        echo "ERROR: Failed to move staging to destination cache" >&2
        publish_failed=1
    fi

    if [ "$publish_failed" -eq 1 ]; then
        # Recovery attempt: restore quarantine back to cache dir
        if [ -n "$QUARANTINE_DIR" ] && [ -d "$QUARANTINE_DIR" ]; then
            if [ "${_BOOTSTRAP_FAIL_RESTORE:-}" = "1" ]; then
                echo "FAILPOINT: Simulating restore failure" >&2
                echo "RECOVERY-REQUIRED: Failed to restore quarantine $QUARANTINE_DIR -> $CACHE_DIR" >&2
                echo "RECOVERY-REQUIRED: Quarantine preserved at $QUARANTINE_DIR" >&2
                PUBLICATION_STATE="restore_failed"
                return 2
            fi
            if ! mv "$QUARANTINE_DIR" "$CACHE_DIR"; then
                echo "RECOVERY-REQUIRED: Failed to restore quarantine $QUARANTINE_DIR -> $CACHE_DIR" >&2
                echo "RECOVERY-REQUIRED: Quarantine preserved at $QUARANTINE_DIR" >&2
                PUBLICATION_STATE="restore_failed"
                return 2
            fi
            echo "Restored old cache from quarantine after publication failure" >&2
            PUBLICATION_STATE="restored"
        fi
        return 1
    fi

    PUBLICATION_STATE="committed"
    CLEANUP_STAGING_DIR="" # Staging successfully moved; disarm cleanup dispatcher
    return 0
}

rc=0
publish_cache || rc=$?
if [ "$rc" -ne 0 ]; then
    if [ "$rc" -eq 2 ]; then
        echo "CRITICAL: Recovery failed. Manual intervention required." >&2
    fi
    exit "$rc"
fi

echo "Successfully bootstrapped Mihomo acceptance assets into $CACHE_DIR"

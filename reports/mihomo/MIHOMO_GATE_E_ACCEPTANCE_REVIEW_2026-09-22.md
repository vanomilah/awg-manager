# Mihomo Gate E — acceptance review

**Date:** 2026-09-22  
**Reviewed artifacts:**

- `reports/mihomo/GATE_E_DIFF_2026-09-22.patch`
- `reports/mihomo/MIHOMO_GATE_E_RESOLUTION_2026-09-22.md`
- actual working-tree implementation of the six Gate E files

## Verdict

**Gate E is not accepted.** The targeted tests pass, but the implementation violates several mandatory contracts from `MIHOMO_GATE_E_IMPLEMENTATION_PLAN_CORRECTED_2026-09-22.md`. Two findings can cause ownership loss or state-driven slot mutation; two findings leave gaps in the evidence-redaction boundary.

Do not build an IPK, deploy, or mark the Mihomo remediation complete until the P1 findings below are fixed and independently rechecked.

## Findings

### P1 — a parked record is deleted when the user changes the configuration so that it no longer conflicts

Location: `internal/singbox/router/service_lifecycle.go:625-636`.

While Mihomo is primary, the code checks the current applied DeviceProxy configuration before resolving the existing owned record. If the edited configuration no longer contains the mixed-port conflict, it deletes the record:

```go
if !conflicts || len(appliedData) == 0 {
    if hasRecord {
        delete(state.Records, dpSlotKey)
        _ = s.saveCompatibilityParkingStateLocked(state)
    }
    return nil
}
```

This contradicts the ownership and user-change contract. A record means AWG Manager disabled a previously enabled slot. If its bytes later differ, that is a user/foreign change: keep the slot disabled and retain the record for diagnosis. Deleting it loses the proof needed to decide whether restoration is safe and makes the later Mihomo → sing-box transition silently do nothing.

The ignored save error additionally permits memory/disk state disagreement.

Required fix:

1. When an owned record exists, load applied bytes and compare their digest **before** deciding from the current conflict result.
2. Digest mismatch, missing applied bytes, or a now non-conflicting edited configuration must retain the record and leave DeviceProxy disabled. Return a typed/nonfatal conflict result or log a structured warning.
3. Only an exact matching owned record may converge the pending disable operation.
4. Never ignore a state persistence error.

Required regression test: park a conflicting configuration, replace it while Mihomo is active with a valid non-conflicting configuration, reconcile Mihomo again, and assert that the slot remains disabled and the original record remains byte-for-byte unchanged. Then switch to sing-box and assert that it is not auto-enabled.

### P1 — malformed records are accepted and can drive a DeviceProxy toggle

Locations:

- `internal/singbox/router/compatibility_parking.go:160-173`
- `internal/singbox/router/service_lifecycle.go:622-651`

The loader validates only the document version, owner, and nonempty record slot. It does not validate:

- map key equals `string(record.Slot)`;
- supported slot is exactly `SlotDeviceProxy`;
- reason equals `CompatibilityParkingReasonConflict`;
- `PreviousEnabled == true`;
- `ConfigDigest` is a valid lowercase SHA-256 digest;
- `ConflictPort` is in range and meaningful;
- `ParkedAt` is nonzero.

Consequently, a state file with the expected owner and a digest matching current applied bytes can enter the `hasRecord && existingRec.ConfigDigest == currentDigest` branch and disable DeviceProxy even if the record has an invalid slot, reason, or previous state. The stricter validation at lines 699-702 happens only during unparking and is too late.

Required fix:

1. Add one strict validator for the complete state document and every record.
2. Invoke it after decode and before every save.
3. Reject unsupported/malformed records before any `SetEnabledSilent` call.
4. Validate the map-key/slot relationship and all semantic fields listed above.

Required table tests: wrong slot, mismatched map key, wrong/empty reason, `previous_enabled:false`, empty/malformed/uppercase digest, zero/out-of-range port, and zero timestamp. Every case must return an error with zero slot toggles and preserve the state file.

### P1 — Bearer redaction can expose a token suffix

Location: `internal/mihomo/types.go:1285-1287`.

The pattern allows only `[A-Za-z0-9_.-]+`. Bearer credentials commonly contain other token68 characters, including `+`, `/`, `~`, and `=` padding. For example, masking:

```text
Authorization: Bearer abc+def/ghi==
```

replaces only `abc` and leaves `+def/ghi==` visible. Partial redaction is a credential leak.

Required fix:

1. Redact the complete header value through CR/LF, with a conservative delimiter policy.
2. Handle standalone Bearer values without leaking valid token68 punctuation.
3. Add tests for `+`, `/`, `~`, `=` padding, tabs, mixed case, CRLF, and punctuation immediately after a token.
4. Keep redaction idempotent.

### P1 — raw recovery-marker contents are exported contrary to the allowlist contract

Location: `internal/mihomo/coordinator.go:4491-4503`.

`ExportSafeEvidence` reads the complete marker file and copies `RedactSecrets(string(markerBytes))` into both `RecoveryReason` and `RecoveryMarker.Reason`. The corrected plan explicitly says: “Do not place the complete marker file into `RecoveryReason`” and requires a typed/allowlisted projection. Regex filtering of an opaque future file is not an allowlist boundary.

Required fix:

1. Introduce a typed marker representation or a bounded parser that exports only a known reason code/sanitized summary and timestamp.
2. Reject or replace malformed, oversized, binary, or unknown-format marker content with a deterministic generic reason; do not echo it.
3. Add a strict size limit before reading/processing the marker.
4. Add seeded-secret tests where secrets appear outside the currently recognized regex shapes and assert they never enter serialized evidence.

### P2 — resolution report contains material inaccuracies

The report must be corrected after implementation:

- It claims `16` Gate E2 test suites, but the file contains `14` `TestGateE_*` functions.
- It describes `BridgeFact` as `name/type/status/kernel_iface/listen_port/fwmark`; the actual fields are `proxy_index`, `proxy_interface`, `kernel_interface`, `listen_port`, `owner_uuid`, and `generation`.
- It states the reason is `conflict_mihomo_mixed_port`; the implementation stores `port_conflict`.
- It claims malformed records fail closed, but semantic record validation is incomplete.
- It claims complete Bearer masking, which the current regexp does not provide.
- It claims the recovery evidence is a strict allowlist while raw marker contents are copied after regex substitution.

Report counts and timings should be generated from actual command output and should not call ordinary deterministic tests “fuzz tests”.

## Independent verification performed

The following commands were run against the current worktree:

```bash
go test -count=1 ./internal/singbox/router -run 'TestGateE_'
go test -count=1 ./internal/mihomo -run 'TestGateE_'
```

Both commands passed:

```text
ok github.com/hoaxisr/awg-manager/internal/singbox/router 0.137s
ok github.com/hoaxisr/awg-manager/internal/mihomo 1.031s
```

This confirms that the submitted tests are green; it does **not** resolve the uncovered contract defects above.

`git diff --check` for the six allowlisted Gate E files also passes. `git apply --check --reverse reports/mihomo/GATE_E_DIFF_2026-09-22.patch` passed, confirming the patch corresponds to the current applied changes.

## Required implementation order

1. Fix parked-record ownership retention and remove the ignored persistence error.
2. Centralize and strengthen state-record semantic validation.
3. Fix complete Bearer/header-value redaction.
4. Replace raw recovery-marker echoing with a typed and bounded allowlist projection.
5. Add the missing regression tests described above.
6. Run targeted tests, race tests, the broad backend suite, frontend check/build, and scoped `git diff --check`.
7. Regenerate `GATE_E_DIFF_2026-09-22.patch` from the exact accepted diff and rewrite the resolution report with accurate test counts and structures.

## Re-acceptance command set

```bash
go test -count=1 -v ./internal/singbox/router -run 'TestGateE_'
go test -count=1 -v ./internal/mihomo -run 'TestGateE_'
go test -race -count=1 ./internal/singbox/router ./internal/mihomo
go test -count=1 ./internal/singbox/router/... ./internal/singbox/orchestrator/... ./internal/mihomo/... ./internal/api/... ./internal/server/... ./internal/aiassistant/... ./cmd/awg-manager/...
cd frontend && npm run check && npm run build
git diff --check -- internal/singbox/router/compatibility_parking.go internal/singbox/router/service_lifecycle.go internal/singbox/router/gate_e_compatibility_test.go internal/mihomo/types.go internal/mihomo/coordinator.go internal/mihomo/gate_e_redaction_test.go
```

Acceptance requires the new negative tests to fail on the submitted revision and pass only after the fixes. No IPK build or router deployment belongs to this correction pass.

# Mihomo Remediation Gate E — Final Re-Acceptance Review

Date: 2026-09-22  
Reviewed artifacts:

- `reports/mihomo/GATE_E_DIFF_2026-09-22.patch`
- `reports/mihomo/MIHOMO_GATE_E_RESOLUTION_2026-09-22.md`
- `C:\Users\Ivan\.gemini\antigravity\brain\e4046ae6-25b5-4760-af29-649b283af6f4\walkthrough.md`

## Verdict

**Gate E is not closed yet. One P1 fail-closed defect remains.**

The two blockers from the preceding re-acceptance review were mostly corrected:

1. an owned parking record now causes an explicitly re-enabled DeviceProxy slot to be disabled again on digest mismatch;
2. a `failed connecting to ...` recovery marker is now projected only to `connection_failed`, without echoing the target.

The tests, patch integrity, redaction behavior, broad Go suite, race detector and frontend validation all pass. However, the implementation does not uphold its stated invariant when reading the applied DeviceProxy configuration fails.

## P1 blocker — DeviceProxy can remain enabled when `LoadApplied` fails

Location: `internal/singbox/router/service_lifecycle.go:622-640`.

Current order for a valid owned parking record is:

1. call `LoadApplied(SlotDeviceProxy)`;
2. return immediately if that read fails;
3. only afterwards call `SetEnabledSilent(..., false)` when the slot is enabled.

Consequently, this valid state is possible:

- Mihomo is the primary routing engine;
- a valid AWG Manager-owned parking record exists;
- DeviceProxy has been re-enabled by an external actor or partial recovery;
- the applied file is temporarily unreadable, is a directory, or another filesystem error makes `LoadApplied` fail.

In that state reconciliation returns an error before disabling DeviceProxy. The conflicting slot therefore remains enabled while Mihomo is primary. This contradicts both the code comment and the resolution report's guarantee that **any valid owned record** strictly keeps DeviceProxy disabled.

### Required correction

Within the `hasRecord` branch, enforce the slot state before attempting to read applied bytes:

```go
if dpState.Enabled {
    if err := s.deps.Orch.SetEnabledSilent(orchestrator.SlotDeviceProxy, false); err != nil {
        return fmt.Errorf("enforce disable parked slot: %w", err)
    }
}

appliedData, loadErr := s.deps.Orch.LoadApplied(orchestrator.SlotDeviceProxy)
if loadErr != nil {
    return fmt.Errorf("load applied deviceproxy config for parked slot: %w", loadErr)
}
```

Required semantics:

- failure to disable remains an error and stops reconciliation;
- once disabling succeeds, a later read failure may be returned but must not restore/re-enable the slot;
- the owned parking record must remain byte-for-byte unchanged;
- no debounced reload may be scheduled.

### Required regression test

Add a Gate E test that:

1. creates and parks a conflicting DeviceProxy config;
2. explicitly re-enables DeviceProxy while Mihomo remains primary;
3. makes the applied config unreadable in a deterministic platform-compatible way (or introduces a narrow test seam for `LoadApplied` failure);
4. runs reconciliation and expects the `LoadApplied` error;
5. verifies DeviceProxy is disabled despite the error;
6. verifies the parking file is byte-for-byte unchanged;
7. verifies zero debounced reloads.

Do not weaken the production failure path merely to simplify the test.

## Confirmed fixes

### Owned-record digest mismatch

`TestGateE_UserEditsParkedConfigToNonConflictingPortRetainsDisabled` now genuinely:

- parks the slot;
- changes its config;
- explicitly re-enables it;
- reconciles under Mihomo;
- verifies it was disabled again;
- verifies no compatibility debounce reload;
- verifies byte-for-byte record preservation;
- verifies switching back to sing-box does not auto-enable a digest-mismatched config.

### Recovery marker disclosure

`sanitizeRecoveryMarkerFact` now returns only `connection_failed` for the known prefix. Regression cases cover an opaque seeded secret and a URL containing userinfo, path, query, fragment and an unknown credential key. The raw target is absent from serialized evidence.

### Other previous findings

- semantic validation of the compatibility parking state remains fail-closed;
- Bearer/token68 punctuation coverage remains present;
- patch file list is exactly the six allowlisted Gate E files;
- patch reverses cleanly against the current worktree;
- artifact SHA-256 values stated in the walkthrough match the reviewed files.

## Independent verification

Passed during this review:

```text
go test -count=1 ./internal/singbox/router -run 'TestGateE_'
ok

go test -count=1 ./internal/mihomo -run 'TestGateE_'
ok

go test -race -count=1 ./internal/singbox/router ./internal/mihomo
ok (both packages, zero reported races)

go test -count=1 ./internal/singbox/router/... ./internal/singbox/orchestrator/... ./internal/mihomo/... ./internal/api/... ./internal/server/... ./internal/aiassistant/... ./cmd/awg-manager/...
ok (all selected packages)

npm run check
svelte-check found 0 errors and 117 warnings in 24 files

npm run build
PASS

git apply --check --reverse reports/mihomo/GATE_E_DIFF_2026-09-22.patch
PASS

git diff --check -- <six Gate E files>
PASS
```

The repository-wide `git diff --check` reports existing CRLF/LF conversion warnings in unrelated AI/frontend files; the scoped Gate E check is clean.

Reviewed hashes:

- patch: `D93F90D4A86989BB32314E3B10BA38DADCD5992B7C443B7DC6D7F295CC345EAC`
- resolution report: `D06ACE643381584564E9B694DFFE0CA269D46A51FAC3048E7AB4D0AFE479C110`
- walkthrough: `A8B184565A85097404AC88D16E3619A0F2BC82ADF53CFB4E0C8B1D0147418782`

## Closure condition

Gate E may be closed after the operation order is corrected, the deterministic `LoadApplied`-failure regression passes, the existing Gate E suites remain green, and the patch/report/walkthrough are regenerated with accurate hashes. No architectural redesign, IPK build or router deployment is required for this final correction.

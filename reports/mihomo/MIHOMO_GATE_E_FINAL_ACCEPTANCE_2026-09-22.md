# Mihomo Remediation Gate E — Final Acceptance

Date: 2026-09-22

Reviewed artifacts:

- `reports/mihomo/GATE_E_DIFF_2026-09-22.patch`
- `reports/mihomo/MIHOMO_GATE_E_RESOLUTION_2026-09-22.md`
- `C:\Users\Ivan\.gemini\antigravity\brain\e4046ae6-25b5-4760-af29-649b283af6f4\walkthrough.md`

## Verdict

**ACCEPTED. Gate E is closed.**

The final P1 fail-closed defect has been corrected. When a valid owned parking record exists and Mihomo is primary, an enabled DeviceProxy slot is now disabled before the implementation attempts to read its applied configuration. A subsequent `LoadApplied` failure is propagated without re-enabling the slot, modifying the parking record, or scheduling a debounced reload.

No remaining P0/P1 blocker was found in the reviewed Gate E scope.

## Confirmed final correction

In `internal/singbox/router/service_lifecycle.go`, the owned-record branch now performs operations in the safe order:

1. enforce `SlotDeviceProxy` disabled with `SetEnabledSilent`;
2. propagate any failure to disable;
3. only then read applied bytes;
4. propagate a read failure while preserving the already-disabled state;
5. retain the original owned parking record.

`TestGateE_LoadAppliedFailureEnforcesDisabledSlot` reproduces the relevant state and verifies:

- an owned record has already been created;
- DeviceProxy is explicitly re-enabled while Mihomo remains primary;
- `LoadApplied` fails deterministically;
- reconciliation returns the expected error;
- DeviceProxy is nevertheless disabled;
- the parking file remains byte-for-byte unchanged;
- zero debounced reloads are scheduled.

The preceding fixes also remain present:

- digest mismatch retains the owned record and enforces disabled state;
- semantic parking-state validation fails closed;
- Bearer/token68 redaction handles punctuation and boundaries;
- recovery markers expose only bounded allowlisted categories;
- `connection_failed` no longer echoes its raw target.

## Artifact integrity

The patch reverses cleanly against the current worktree and contains exactly the seven declared Gate E files:

1. `internal/mihomo/coordinator.go`
2. `internal/mihomo/gate_e_redaction_test.go`
3. `internal/mihomo/types.go`
4. `internal/singbox/router/compatibility_parking.go`
5. `internal/singbox/router/gate_e_compatibility_test.go`
6. `internal/singbox/router/service_lifecycle.go`
7. `internal/singbox/router/service.go`

Reviewed SHA-256 values agree with the walkthrough:

- patch: `9BE02AFCDA96CA58A419AE94C99901A57EE7DE8A9E520AA9A7920F45C041E171`
- resolution report: `AF48D23FD5493F2B04C00ED37D6427F28EEC390790435899A8F033D5BC802B1D`
- walkthrough: `186E2E05213B91DBE01F598D42539941B69FC145B1D526C08DDE6DB2371AE15F`

## Independent verification

Executed against the current worktree:

```text
wsl.exe -d Ubuntu -- bash -lc "cd /mnt/e/AWGM/awg-manager && go test -count=1 ./internal/singbox/router -run 'TestGateE_' && go test -count=1 ./internal/mihomo -run 'TestGateE_'"
PASS

wsl.exe -d Ubuntu -- bash -lc "cd /mnt/e/AWGM/awg-manager && go test -race -count=1 ./internal/singbox/router ./internal/mihomo"
PASS; no races reported

wsl.exe -d Ubuntu -- bash -lc "cd /mnt/e/AWGM/awg-manager && go test -count=1 ./internal/singbox/router/... ./internal/singbox/orchestrator/... ./internal/mihomo/... ./internal/api/... ./internal/server/... ./internal/aiassistant/... ./cmd/awg-manager/..."
PASS; all selected packages green

git apply --check --reverse reports/mihomo/GATE_E_DIFF_2026-09-22.patch
PASS

git diff --check -- <seven Gate E files>
PASS
```

The first native Windows router test attempt failed before compilation because the local Go build-cache returned `Access is denied`. The same current source passed in WSL, including under the race detector; this is an environment/cache failure, not a Gate E test failure.

Frontend check and production build had already passed independently immediately before this Go-only final correction. The final patch introduces no frontend changes.

## Test seam follow-up

The previously recorded non-blocking technical debt has been resolved. The package-level mutable hook was removed and replaced with the instance-scoped `Deps.LoadAppliedDeviceProxy` override. The regression test configures it through `SetLoadAppliedDeviceProxyForTest`, so separate service instances no longer share mutable hook state.

## Program status

Gate E satisfies its acceptance contract. Subject to the already recorded acceptance decisions for Gates A through D, the Mihomo remediation sequence A–E can now be treated as closed at source/test level. Router deployment and live traffic validation remain separate release activities and were not performed during this review.

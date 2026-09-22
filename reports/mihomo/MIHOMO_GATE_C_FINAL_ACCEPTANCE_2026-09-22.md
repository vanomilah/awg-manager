# Mihomo Gate C — final acceptance

Date: 2026-09-22  
Branch: `feature/mihomo-ai-proxyrt`

Reviewed artifacts:

- `reports/mihomo/GATE_C_DIFF_2026-09-22.patch`
- `reports/mihomo/MIHOMO_REMEDIATION_GATE_C_RESOLUTION_REPORT_2026-09-22.md`
- implementation in the current working tree

## Verdict

**ACCEPTED at source/unit/race-test level. Gate C is closed.**

All four findings from `MIHOMO_GATE_C_ACCEPTANCE_REVIEW_REV2_2026-09-22.md` are resolved in code and covered by focused regression tests.

This acceptance does **not** claim router/device acceptance. No IPK was built and no deployment or live NDMS test was performed during this review.

## Confirmed corrections

1. **Missing process proof fails closed.**
   `VerifyActiveProcessProof` now accepts an absent receipt only when there is no applied record or the runtime is `RuntimeOff`. An enforced runtime without a receipt returns `ErrProcessProofFailed`.

2. **Bridge rollback uses an observed port.**
   `ProxyObservation` now carries the live listen port. `inspectBridgeLocked` builds its observed reference from live NDMS fields instead of the desired reference. A modified bridge with an unobservable port is rejected before mutation. The regression test independently seeds live port A, requests port B, fails a later publication, and proves restoration to A.

3. **Legacy generation manifests remain readable without trusting stale runtime evidence.**
   The strict custom decoder accepts only the deprecated `process_receipt` compatibility field, discards it, continues rejecting other unknown fields, and new manifests do not serialize it.

4. **Transactional restart failures preserve recovery identity.**
   `restartControlledLocked` receives the active `TxID`; stop/reap failures include it in the recovery marker. Marker-write errors are returned via `errors.Join` instead of being silently discarded.

## Independent verification

Executed against the current working tree:

```text
go test -count=1 ./internal/sys/procnet ./internal/mihomo ./internal/singbox ./internal/mihomonative ./cmd/awg-manager
PASS

go test -race -count=1 ./internal/sys/procnet ./internal/mihomo ./internal/singbox ./cmd/awg-manager
PASS

git apply --check --reverse -- reports/mihomo/GATE_C_DIFF_2026-09-22.patch
PASS

git diff --check -- cmd/awg-manager internal/mihomo internal/singbox internal/sys/procnet
PASS
```

Artifact hashes reviewed:

```text
GATE_C_DIFF_2026-09-22.patch
32270CA87A3541FD10CF2FEC8612F9F03E5CF42D495916359AC67100CCBFBE85

MIHOMO_REMEDIATION_GATE_C_RESOLUTION_REPORT_2026-09-22.md
1B38947EB6998CD4F20708D5BBE57B9D4F58371EE85ED48A02B5F617745B4218
```

## Remaining work outside Gate C

- Build/package acceptance when explicitly requested.
- Live-router validation of the NDMS running-config format and live proxy-port observation.
- Reboot/recovery and actual traffic validation on the target Keenetic device.
- Preserve the reviewed artifact hashes; any subsequent change requires a new focused review.


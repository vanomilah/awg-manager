# Mihomo Gate C — acceptance review rev.2

Date: 2026-09-22  
Reviewed artifacts:

- `reports/mihomo/MIHOMO_REMEDIATION_GATE_C_RESOLUTION_REPORT_2026-09-22.md`
- `reports/mihomo/GATE_C_DIFF_2026-09-22.patch`

## Verdict

**REWORK REQUIRED — Gate C is not accepted yet.**

The revision fixes a substantial part of the previous review: process identity is now revalidated, listener ownership is fail-closed, direct stop paths use the common helper, the immutable generation manifest no longer writes a live process receipt, bridge publication checks the live kernel interface, compensation tests were added, and the submitted patch reverses cleanly against the current working tree.

Four remaining contract defects must be fixed. The first two are release blockers because they can falsely report a valid runtime or incorrectly restore a bridge after a failed batch.

## Required fixes

### P0-1. `VerifyActiveProcessProof` still accepts a missing proof

Location: `internal/mihomo/coordinator.go:1099-1105`.

Current behavior:

```go
if c.appliedRecord == nil || c.appliedRecord.ProcessReceipt == nil {
    return nil
}
```

For `RuntimeEnforced`, an applied record without `ProcessReceipt` is not a verified active process. Returning success is fail-open and contradicts the Gate C invariant that an enforced runtime must have an authoritative live receipt.

Required behavior:

- no applied record: return the explicitly documented result for “nothing active”;
- `RuntimeOff`: a nil receipt is valid;
- `RuntimeEnforced`: a nil receipt must return an error wrapping `ErrProcessProofFailed`;
- keep the existing strict PID/start-ticks/executable/cmdline/listener verification when a receipt exists.

Required regression test: an enforced `AppliedGenerationRecord` with `ProcessReceipt == nil` must fail with `ErrProcessProofFailed`; the same shape with `RuntimeOff` must succeed.

### P0-2. Bridge compensation uses a fabricated, not observed, before-image

Locations:

- `cmd/awg-manager/mihomo_bridge_runtime.go:652-707`
- `cmd/awg-manager/mihomo_bridge_runtime.go:910-931`
- `internal/singbox/proxy.go:242-252`

`inspectBridgeLocked` initializes `obsRef := ref` and replaces only `KernelInterface` from live NDMS. Consequently `ProxyInterface` and, critically, `ListenPort` in the alleged before-image are copied from the desired bridge. `ProxyObservation` does not expose a live listen port at all.

This makes both statements false:

- `isIdentical` does not prove that the existing Proxy interface points at the desired port;
- `beforeRef := obs.BridgeRef` is not a complete restorable before-image.

On partial failure, `PublishBridge(beforeRef)` can therefore reapply the new desired port instead of restoring the actual previous mapping. The current pre-existing-bridge test cannot catch this because it seeds and inspects through the same desired data path.

Required resolution (choose one and encode it explicitly):

1. extend the NDMS inspection adapter so the actual live proxy target/listen port and interface identity are observed, then construct the before-image only from those live values; or
2. if NDMS cannot expose the old target, fail closed before modifying an existing non-identical bridge. Do not promise compensation for state that cannot be observed and restored. Existing exact-owned bridges may remain idempotent only when all required observable fields are proven.

Required regression test: seed an owned live `ProxyN` with old port A, apply desired port B, fail publication of a later bridge, and assert the live proxy is restored to A. The fake must return its own live port rather than deriving it from the requested `BridgeRef`. If live port inspection is impossible, assert that the attempted A→B mutation is rejected before any mutation.

### P1-1. Removing `process_receipt` broke strict decoding of existing generation bundles

Locations:

- `internal/mihomo/types.go:376-390`
- `internal/mihomo/generation_store.go:349-358`
- `internal/mihomo/types.go:1323-1328` (`DecodeJSONStrict`)

New manifests correctly omit the runtime receipt, but old Gate C/intermediate bundles containing `process_receipt` are now rejected by `DisallowUnknownFields`. The schema version remains `1`, so this is an unversioned incompatible change and can break rollback/read-after-upgrade.

Required resolution:

- add an explicit compatibility path for legacy `process_receipt` while never treating it as authoritative runtime evidence and never writing it into new immutable manifests; or
- bump the manifest schema version and implement a tested migration.

A minimal safe option is a dedicated legacy decode DTO/custom decoder which accepts and discards only this known deprecated field, while continuing to reject all other unknown fields.

Required regression test: load a version-1 generation manifest containing a legacy `process_receipt`, verify the bundle remains readable, verify the field is ignored for runtime proof, and verify a newly archived manifest omits it.

### P1-2. Transaction identity is dropped during the normal restart stop phase

Locations:

- `internal/mihomo/coordinator.go:2750`
- `internal/mihomo/coordinator.go:2863-2869`
- other `restartControlledLocked` callers at approximately lines 3691, 4214 and 4781.

`restartControlledLocked` always calls:

```go
c.stopControlledLocked(ctx, "", "restart")
```

Thus a stop/reap failure during a normal apply writes an unscoped recovery marker even though an active transaction and `TxID` exist. This weakens crash/recovery attribution and violates the requirement that recovery markers identify the active transaction.

Required resolution:

- pass `txID` into `restartControlledLocked` (or an equivalent operation context);
- transactional apply/rollback/regenerate callers must pass their manifest `TxID`;
- non-transactional recovery callers may pass an empty value only when no transaction exists;
- add a test that injects `StopAndWait` failure during a normal apply and asserts `StateRecoveryRequired` plus a marker containing the transaction ID.

Also stop silently discarding `writeRecoveryMarkerLocked` errors at `coordinator.go:2847` and `2857`: return/join them with the stop failure so durable recovery failure is visible.

## Independently verified

The following commands were run against the current tree, not inferred from the supplied report:

```text
go test -count=1 ./internal/sys/procnet ./internal/mihomo ./internal/singbox ./internal/mihomonative ./cmd/awg-manager
PASS

go test -race -count=1 ./internal/sys/procnet ./internal/mihomo ./cmd/awg-manager
PASS

git apply --check --reverse -- reports/mihomo/GATE_C_DIFF_2026-09-22.patch
PASS

git diff --check
PASS (only unrelated CRLF conversion warnings were printed)
```

Patch SHA-256 at review time begins with `792FBA62D5B436DA9BAE3EB`; resolution report SHA-256 begins with `F6A153CF0BBD4162114D574`.

## Re-acceptance checklist

Do not create another broad implementation plan. Implement the four fixes above and provide one focused resolution report plus a refreshed isolated diff.

- [ ] Missing receipt fails closed for `RuntimeEnforced` and remains valid for `RuntimeOff`.
- [ ] Bridge rollback uses a genuinely observed complete before-image, or mutation is refused when that image cannot be obtained.
- [ ] Legacy generation manifests with `process_receipt` remain readable without trusting that receipt.
- [ ] Restart stop failures preserve `TxID`; recovery-marker write failures are surfaced.
- [ ] New negative regression tests pass.
- [ ] Targeted normal and race suites pass.
- [ ] `git diff --check` passes.
- [ ] No IPK build, router deployment, `--force-reinstall`, cleanup, or unrelated-tree edits.


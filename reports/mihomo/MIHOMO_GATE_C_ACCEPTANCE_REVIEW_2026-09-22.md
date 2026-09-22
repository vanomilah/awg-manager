# Mihomo Gate C — acceptance review

**Date:** 2026-09-22  
**Reviewed artifacts:**

- `reports/mihomo/GATE_C_DIFF_2026-09-22.patch`
- `reports/mihomo/MIHOMO_REMEDIATION_GATE_C_RESOLUTION_REPORT_2026-09-22.md`
- Antigravity `walkthrough.md`
- current working tree on `feature/mihomo-ai-proxyrt`

## Verdict

**GATE C IS NOT CLOSED.**

The targeted package suite and race suite pass, but the implementation and the generated artifacts do not satisfy several mandatory Gate C acceptance criteria. The resolution report overstates completion.

This document is an implementation order. Fix the findings below directly; do not return another planning document.

## Independently reproduced checks

```text
go test -count=1 ./internal/sys/procnet ./internal/mihomo ./internal/singbox ./internal/mihomonative ./cmd/awg-manager

ok  internal/sys/procnet
ok  internal/mihomo
ok  internal/singbox
ok  internal/mihomonative
ok  cmd/awg-manager
```

```text
go test -race -count=1 ./internal/sys/procnet ./internal/mihomo ./internal/singbox ./cmd/awg-manager

ok  internal/sys/procnet
ok  internal/mihomo
ok  internal/singbox
ok  cmd/awg-manager
```

Passing tests are acknowledged. The defects below are missing assertions or untested production paths.

## P0-1 — bridge compensation deletes state that pre-existed the batch

File: `cmd/awg-manager/mihomo_bridge_runtime.go`, `ApplyBridges`.

Current code appends every successfully published ref to `published`. `PublishBridge` is idempotent and also succeeds when the exact owned `ProxyN` already existed before this call. If a later bridge fails, compensation withdraws all entries in `published`, including those that existed before the batch.

This violates the required rule: compensate only mutations introduced by the current operation. It can remove a previously working bridge during a failed apply.

### Required fix

Before each publish, inspect the exact bridge and classify it as:

- already present and exact-owned with correct observable identity: do not add it to compensation;
- absent: publish and add it to the `createdByThisBatch` compensation set;
- foreign, ambiguous or mismatched: fail closed without mutation.

If an existing owned bridge is modified rather than created, capture a complete restorable before-image and restore that exact before-image during compensation. Do not simply withdraw it.

Compensation errors must be joined with the original error and returned. Current `_ = r.WithdrawBridge(...)` silently hides failed rollback.

### Mandatory regression

Seed bridge A as already active before `ApplyBridges([A, B])`. Make publication of B fail. Prove A still exists unchanged after compensation. Also inject a compensation failure and prove both the original and compensation errors are observable.

## P0-2 — ExactBridge observation fabricates `KernelInterface`

File: `cmd/awg-manager/mihomo_bridge_runtime.go`, `inspectBridgeLocked`.

Current code starts with desired `ref.KernelInterface` and replaces it only when live `obsProxy.SystemName` is non-empty:

```go
kernelIface := ref.KernelInterface
if obsProxy.SystemName != "" {
    kernelIface = obsProxy.SystemName
}
```

Therefore an empty/unavailable live `SystemName` is reported as if NDMS had observed the desired kernel interface. This is precisely the desired-state-as-runtime-proof error prohibited by Binding Amendment 1.

### Required fix

- Populate observed `KernelInterface` only from live `ProxyObservation.SystemName`.
- Never fall back to `ref.KernelInterface` in an observed fact.
- For operations whose acceptance requires kernel identity, empty `SystemName` must fail closed.
- If a particular Keenetic firmware legitimately cannot expose `SystemName`, introduce an explicit capability/result state and use the independently specified alternative proof; do not synthesize the desired value.
- Publish postcondition must verify `Exists`, canonical owner, `Up` according to the real NDMS contract, and exact `SystemName` when required.

### Mandatory regression

Live observation with empty `SystemName` and desired `t2s10` must not return observed `t2s10` and must not pass exact publication verification.

## P0-3 — generation bundle is published before process proof and permanently stores a nil/stale receipt

Files:

- `internal/mihomo/coordinator.go`, normal apply path
- `internal/mihomo/generation_store.go`, `PublishStagedBundle`

The candidate generation bundle is published using `newRec` before Mihomo is restarted and before `c.processReceipt` is captured. `GenerationManifest.ProcessReceipt` is therefore nil (or potentially unrelated if later code changes reuse state), while final `verified-active.json` is written with a newly captured receipt.

`verifyAuthoritativeGenerationLocked` checks bundle digests and mode but does not compare `gm.ProcessReceipt` with the authoritative applied record. Thus Gate C creates two durable descriptions of the same generation that disagree about process proof while still reporting success.

This violates the plan requirement not to mutate a committed bundle after publication and to make durable generation evidence internally consistent.

### Required fix

Choose and document one valid model:

1. **Preferred:** make process receipt an authoritative runtime record only, remove it from immutable `GenerationManifest`, migrate/accept existing bundles explicitly, and ensure rollback always captures a new receipt after starting the target generation; or
2. stage the bundle before runtime activation but atomically finalize an immutable bundle containing the verified receipt before pointer/verified-active commit, without in-place mutation of a published/committed bundle.

Do not leave a `ProcessReceipt` field in generation manifest that is silently inconsistent and ignored by verification.

### Mandatory regression

After apply, read both `verified-active.json` and `generation.manifest.json`. Their documented receipt contract must hold. Crash tests are required around runtime proof, bundle finalization and pointer promotion.

## P0-4 — not all coordinator stop paths use the fail-closed lifecycle helper

File: `internal/mihomo/coordinator.go`.

Direct `Operator.StopAndWait` calls remain at least in:

- first-generation startup rollback;
- legacy LKG fallback in `Reconcile`.

The legacy fallback writes the replacement active config before it proves the old process stopped. If stop/reap then fails, the running process and the active config can describe different runtime state. This contradicts the report's claim that all paths use `stopControlledLocked` and abort before config mutation/spawn.

### Required fix

- Route every coordinator stop through one fail-closed helper that preserves `errors.Is(ErrProcessNotReaped)` and writes a transaction-scoped marker where a transaction exists.
- In legacy fallback, prove stop/reap before overwriting active config.
- After stop, prove `(running == false && pid == 0)` before config replacement or new spawn.
- Audit every `StopAndWait(` occurrence, not only normal apply/regenerate.

### Mandatory regression

Inject `ErrProcessNotReaped` into both remaining paths. Prove active config is not replaced/unlinked, Start is never called, state is recovery-required and the marker identifies the operation/transaction.

## P1-1 — Stop/Kill/Reap report claims tests that do not exist

File: `internal/mihomo/gate_c_test.go`.

`TestGateC3_StopAndWait_ReapContracts` contains only `StopAlreadyStopped`. It does not test:

- graceful exit;
- graceful timeout followed by kill and reap;
- canceled caller context followed by independent reap wait;
- `os.ErrProcessDone`/ESRCH;
- reap timeout returning `ErrProcessNotReaped`;
- concurrent/repeated stop without double `Wait` or deadlock.

The resolution report nevertheless states those contracts are verified.

### Required fix

Add a deterministic injected process-control seam (signal/kill/wait or equivalent). Do not use five-second sleeps or flaky zombie-process tricks. Exercise every contract above and update the report to contain only actually executed tests.

Also handle `done == nil` for a supposedly running managed process as an invalid lifecycle state; returning only `sigErr` (often nil) does not prove reap.

## P1-2 — procfs lookup is still partly fail-open and cross-family wildcard matching is over-broad

File: `internal/sys/procnet/listener.go`.

Problems:

1. A matching socket with no resolved PID returns `SocketFound=true, PID=0, err=nil`. The verifier later rejects PID mismatch, but the typed procfs API itself does not distinguish observation failure from absence as required.
2. `/proc/<pid>/fd` directory/readlink errors are silently skipped. If access was denied, ownership was not disproved; it was unobservable.
3. Malformed rows are silently skipped even when they may correspond to the target port.
4. For a specific IPv4 target, an IPv6 wildcard row is accepted merely because its address is all zeroes. A `tcp6` socket may be IPv6-only; procfs matching cannot assume it serves `127.0.0.1`.

### Required fix

- Socket found but owner cannot be proven: return a typed/error observation failure.
- Track relevant permission/read errors and fail closed when they prevent ownership proof.
- Distinguish absent socket from malformed/unobservable target evidence.
- Do not match an IPv6 wildcard to a specific IPv4 target unless dual-stack capability is independently proven. General `tcp`/`udp` can inspect both families, but family-specific ownership remains explicit.

### Mandatory regressions

- socket row exists, no matching fd -> error;
- matching fd directory unreadable -> error;
- specific IPv4 target plus only IPv6 wildcard -> not accepted as IPv4 owner;
- malformed target row -> observation error;
- IPv4-only system without optional IPv6 table remains supported.

## P1-3 — same-daemon process proof trusts cached PID without OS identity/socket revalidation

File: `internal/mihomo/coordinator.go`, `VerifyActiveProcessProof`.

Within the same daemon epoch, matching `IsRunning()` PID returns success without checking start ticks, executable, cmdline or socket ownership. PID equality is not proof against external replacement, stale operator state, or listener takeover.

At minimum, any call advertised as `VerifyActiveProcessProof` should perform the OS proof. If a cheaper cached check is needed for UI polling, expose it under a different name and never use it for mutation/recovery authorization.

### Mandatory regression

Same epoch and same PID, but changed start ticks/cmdline or stolen listener, must fail strict active proof.

## P1-4 — `GATE_C_DIFF` is not a clean Gate C patch

The patch is about 446 KB and was produced against a working tree that already contains many unrelated modifications. Its accompanying status section lists frontend, AI assistant, CDN, Xray, `wdtt`, `qwdtt` and other unrelated files. Calling it a clean Gate C diff is inaccurate and makes safe transfer/review impossible.

### Required fix

Regenerate the artifact from an explicit Gate C path/file allow-list or from a known Gate B baseline commit/tree. The patch must contain only Gate C production/test changes. Do not include status output inside a patch intended for `git apply`.

If isolation cannot be reconstructed safely because there is no baseline commit, state that honestly and provide:

- a file allow-list;
- per-file Gate C hunks;
- base commit/hash;
- warning that the artifact is review-only and not directly applicable.

## P2 — report corrections

Until the blockers above are fixed:

- replace `GATE C CLOSED` with `GATE C IMPLEMENTED, ACCEPTANCE BLOCKED`;
- remove claims that every stop path uses the helper;
- remove claims that all reap scenarios are tested;
- state that tests pass but do not cover the discovered paths;
- do not claim the current patch is clean or isolated.

## Required implementation order

1. P0-1 bridge before-image/compensation.
2. P0-2 honest live bridge observation and postconditions.
3. P0-3 durable receipt model and crash boundaries.
4. P0-4 all stop paths and safe legacy fallback ordering.
5. P1-1 deterministic lifecycle test seam and full tests.
6. P1-2 procfs fail-closed ownership/family semantics.
7. P1-3 strict same-epoch proof.
8. Regenerate isolated patch and truthful report.

After each item, run its narrow tests. Then run:

```bash
go test -count=1 ./internal/sys/procnet ./internal/mihomo ./internal/singbox ./internal/mihomonative ./cmd/awg-manager
go test -race -count=1 ./internal/sys/procnet ./internal/mihomo ./internal/singbox ./cmd/awg-manager
git diff --check -- internal/sys/procnet internal/mihomo internal/singbox internal/mihomonative cmd/awg-manager
```

No IPK build and no router deployment are authorized by this review.

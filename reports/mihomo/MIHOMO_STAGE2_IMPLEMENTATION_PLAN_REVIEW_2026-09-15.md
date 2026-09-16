# Review: Mihomo Stage 2 Transactional Apply Plan

Date: 2026-09-15  
Reviewed source: `C:\Users\Ivan\.gemini\antigravity\brain\e4046ae6-25b5-4760-af29-649b283af6f4\implementation_plan.md`  
Repository: `E:\AWGM\awg-manager`  
Branch/worktree: review only; no IPK build and no router deployment were performed.

## Verdict

**Do not start implementation from the current revision. Revision is required.**

The proposed direction is correct: a single coordinator must own compile, candidate validation, durable file replacement, runtime transition, readiness verification, NDMS bridge publication, and rollback. However, the plan does not yet define one transaction owner and does not provide enough state and recovery semantics to prove that the old working runtime survives every failure.

The current plan would likely improve validation but could still leave one of these inconsistent combinations:

- new YAML on disk with the old runtime;
- old YAML on disk with the new runtime;
- restored YAML with an already-cancelled rollback context, so runtime recovery never runs;
- restored runtime with native-store and NDMS bridge state from the failed mutation;
- a successful controller probe against a stale Mihomo process rather than the generation just applied.

## Confirmed current code facts

1. `internal/singbox/router/service_mihomo.go` currently compiles and directly overwrites `config.yaml` with `os.WriteFile`.
2. `cmd/awg-manager/dynamic_engine.go` currently owns the process transition lock, calls bridge preparation, config generation, `Start`/`Reload`, and bridge activation.
3. `internal/api/mihomo_handler.go` has a second transaction around native-store mutation, snapshot restoration, bridge reconciliation, reload, and re-apply of the previous store after a failure.
4. `internal/mihomo/operator.go` currently:
   - validates only the active config directory;
   - may hot-reload with `PUT /configs`;
   - treats a controller response as readiness;
   - tracks an in-process generation, but the proposed coordinator contract does not expose or verify that generation.
5. The Stage 1 compiler has a real no-output case: sidecar mode with no listeners returns without writing a config. A plain `([]byte, error)` compile result cannot distinguish this from an accidentally empty configuration.

## Blocking findings

### P0-1. The plan assigns transaction ownership twice

Section 3 says `GenerateMihomoConfig()` should use/delegate to `ApplyCoordinator`. Section 4 says `DynamicEngine.prepareMihomoConfig` and `runMihomo` should use the coordinator under `d.withTransition`.

Both cannot own the transaction. If `GenerateMihomoConfig()` performs apply/reload while `DynamicEngine` also performs runtime transition, the result is duplicate reload, nested rollback, or deadlock under the transition lock.

Required correction:

- `CompileMihomoConfig(ctx)` must be a pure compiler and must never write, reload, publish, or withdraw anything.
- `ApplyCoordinator.Apply(ctx, request)` must be the only compile/apply/runtime/bridge transaction owner.
- `DynamicEngine` should call the coordinator exactly once while holding its existing `transitionMu`.
- Remove production use of the old write-capable `GenerateMihomoConfig`; retain only a clearly named compatibility wrapper for tests if temporarily required, and make that wrapper call the same top-level transaction rather than becoming a second owner.

### P0-2. Readiness is promised but absent from the coordinator contract

The proposed dependencies contain `RuntimeApplier`, but no explicit readiness verifier, expected process generation, required listener set, or runtime receipt. Therefore step 6 cannot be tested or enforced independently.

Required correction:

- Runtime apply must return an `ApplyReceipt`, at minimum containing:
  - transition kind (`start`, `restart`, or `reload`);
  - expected Mihomo process generation/PID;
  - expected controller identity;
  - required listener addresses/ports;
  - config digest/generation being applied.
- Add an explicit bounded `RuntimeVerifier(ctx, receipt) error` stage.
- A controller HTTP 2xx alone is not sufficient. Verification must reject a stale controller and require the expected process generation plus all listeners needed by the selected mode.
- Add a failure test where `PUT /configs` returns 2xx but the process exits or required listeners never appear.

### P0-3. Rollback cannot use the caller's cancelled context

The plan passes the same `ctx` to `RuntimeRollback` and `BridgeWithdrawer`. A timeout or user cancellation is itself a common reason to enter rollback. That context will already be done, causing recovery operations to abort immediately.

Required correction:

- After the commit boundary, rollback must use an independent, bounded recovery context created by the coordinator.
- Cancellation before disk commit may return normally after candidate cleanup.
- Cancellation after disk commit must not interrupt restoration. The caller may receive cancellation only after recovery reaches a stable terminal state.
- Add tests for cancellation before swap, during runtime apply, during readiness, and during bridge publication.

### P0-4. Durable LKG and file-swap protocol is underspecified

“Backup current config to `config.yaml.lkg`” does not say copy or rename, how replacement of an existing LKG is made atomic, or when the directory is synced. Renaming active config to LKG first creates a window in which `config.yaml` is absent. Copying directly over LKG can corrupt both recovery generations on a crash.

Required correction:

Define the exact same-filesystem protocol, including every sync:

1. Open the config directory safely and reject unexpected symlink/file types.
2. Write a uniquely named candidate in that directory with mode `0644`, sync and close it.
3. Validate that exact candidate.
4. If an active config exists, durably create `config.yaml.lkg.next` from it, sync/close, rename it to `config.yaml.lkg`, then sync the directory.
5. Rename candidate to `config.yaml`, then sync the directory.
6. Never remove/overwrite the only recoverable config before the replacement is durable.
7. Define first-install behavior when neither active nor LKG exists.
8. Preserve candidate/LKG/journal evidence when rollback itself fails.

Tests must cover failures on create, write, chmod, file sync, close, each rename, directory sync, and restoration.

### P0-5. No persistent recovery protocol exists for AWG Manager crashes

In-memory rollback only handles returned errors. It does not handle daemon termination or router power loss between disk commit, runtime reload, readiness, and bridge publication.

Required correction:

- Add a small durable transaction manifest, for example `config.yaml.txn.json`, containing transaction ID, old/new digest, mode, and current phase.
- Sync the manifest at commit boundaries.
- On startup, reconcile candidate/active/LKG/manifest before Mihomo or bridges are considered ready.
- Define deterministic recovery for every persisted phase.
- Do not publish bridges until startup recovery confirms a valid active runtime.

If crash recovery is intentionally deferred, the feature must not be described as fully transactional and Stage 2 must not be declared closed.

### P0-6. Native-store rollback and config rollback are two competing transactions

`MihomoHandler.withNativeMutation` currently snapshots and mutates the native store, prepares bridges, invokes reload, then restores the store and re-applies after failure. The proposed coordinator separately rolls back YAML/runtime/bridges. Without an explicit contract, both layers can withdraw bridges, restore different snapshots, and invoke re-apply recursively.

Required correction:

- Define one outer transaction that includes the native-store snapshot.
- Recommended ownership:
  - handler supplies a reversible store mutation/snapshot to `DynamicEngine`;
  - coordinator compiles from the mutated store and owns config/runtime/readiness/bridge transition;
  - if coordinator fails, it restores disk/runtime first, then the handler restores the native store, followed by one explicit convergence pass;
  - alternatively, move store snapshot/restore into the coordinator, but do not retain both rollback loops.
- Document the ordering for both deletion and creation of bridge-backed resources.
- Add tests proving there is exactly one reload on success and a bounded, non-recursive rollback path on failure.

### P0-7. Rollback failure needs an explicit terminal state

“Return structured error and preserve recovery artifacts” is not enough. The system must stop advertising a healthy state when disk, runtime, and bridges cannot be reconciled.

Required correction:

- Define transaction states such as `preparing`, `disk_committed`, `runtime_applied`, `published`, `committed`, and `recovery_required`.
- When restoration cannot be proven, persist `recovery_required`, close/withdraw the NDMS publication gate, and expose the condition through status/API/logging.
- Preserve both primary and rollback errors with phase and transaction ID; use `errors.Join` without losing the original cause.
- Block subsequent automatic applies until startup recovery or an explicit safe recovery operation succeeds.

## High-priority plan gaps

### P1-1. Compile result must be typed, context-aware, and immutable

Replace `CompileMihomoConfig() ([]byte, error)` with a context-aware result, for example:

```go
type CompileResult struct {
    YAML              []byte
    Mode              RuntimeMode
    Apply             bool
    RequiredListeners []Listener
    Digest            string
}

func (s *ServiceImpl) CompileMihomoConfig(ctx context.Context, mode RuntimeMode) (CompileResult, error)
```

This removes reliance on settings being re-read at several transaction stages and distinguishes a legitimate no-op from empty/corrupt YAML. Replace existing `context.Background()` calls inside compilation with the supplied context.

### P1-2. Do not widen the generic engine interface without necessity

Adding arbitrary-file validation to `proxyengine.Engine` forces unrelated Sing-box implementations, fakes, and adapters to implement a Mihomo-specific capability.

Use a small optional interface such as:

```go
type ConfigFileValidator interface {
    ValidateConfigFile(context.Context, string) error
}
```

The coordinator should receive this capability explicitly. If the generic interface is deliberately expanded, the plan must list and test every affected implementation and mock.

### P1-3. Validation must use the resolved binary and bounded output

The proposed operator implementation uses `o.binaryPath`, while runtime start uses `resolveBinary()`. This can validate a different/nonexistent executable than the one actually started.

Required correction:

- validate with the exact resolved executable used for runtime;
- bound the validation timeout and captured output;
- make command execution injectable for tests;
- specify how validation behaves when shared data-dir assets are missing or concurrently used;
- ensure validation cannot bind production listeners or mutate live state.

### P1-4. Runtime modes require distinct rollback behavior

The coordinator must distinguish:

- Mihomo stopped -> start primary;
- Mihomo stopped -> start exports sidecar;
- running primary -> hot reload;
- TUN config change -> restart;
- running exports -> primary and primary -> exports transitions;
- desired off/no-listeners -> stop and withdraw without compiling an empty file.

The rollback receipt must restore the previous mode and previous running/stopped state, not merely call one generic `RuntimeRollback` callback.

### P1-5. LKG must mean “verified runtime”, not merely “previous bytes”

Record the digest and mode associated with the last configuration that passed runtime readiness. After a successful transaction, update the verified generation metadata. On the next transaction, rollback must target that verified generation. Do not infer validity merely from the presence of `config.yaml.lkg`.

### P1-6. API and observability acceptance are missing

Add requirements for:

- structured phase-specific API errors;
- transaction ID and current/previous digest in logs, without secrets/config contents;
- user-visible `recovery_required` state;
- no raw YAML or proxy credentials in errors;
- no false success response before bridge publication and final directory sync complete.

## Required test matrix additions

The listed tests are necessary but not sufficient. Add at least:

1. First install with no active config and no LKG.
2. Idempotent/no-op apply of the already active digest.
3. Two concurrent apply requests are serialized; the second compiles after the first commits.
4. Cancellation at every phase, with independent recovery context after commit.
5. Hot reload returns 2xx but old/stale controller answered.
6. Hot reload returns 2xx and the Mihomo process exits immediately afterward.
7. Readiness succeeds for controller but one required bridge/TProxy/TUN listener is absent.
8. Runtime rollback itself fails -> durable `recovery_required`, gate closed.
9. Bridge publication partially succeeds, then fails; all newly published entries are withdrawn and old entries restored.
10. Native-store mutation rollback plus config rollback performs no recursive/double apply.
11. Process restart recovery from every durable transaction-manifest phase.
12. Existing LKG replacement failures preserve at least one complete verified generation.
13. Symlink/non-regular `config.yaml`, candidate, LKG, and manifest are rejected safely.
14. File modes and ownership remain correct after success and rollback.
15. Sidecar no-listener result causes a deliberate stop/no-op, never an empty YAML commit.
16. Mode transitions primary <-> exports <-> off restore the exact previous state on failure.
17. Real-binary acceptance proves that the candidate path, not the old active config, was validated.

## Corrected implementation sequence

1. Freeze and document a single transaction state machine, ownership boundary, commit point, and recovery-required behavior.
2. Introduce typed `CompileResult` and make compilation pure/context-aware.
3. Add Mihomo-specific arbitrary-file validation using the same resolved executable as runtime.
4. Implement durable file primitives and failure injection independently from process control.
5. Implement persistent transaction manifest and startup reconciliation.
6. Add runtime apply receipts and generation-aware readiness verification.
7. Integrate bridge publication/withdrawal with exact old/new snapshots.
8. Replace the current handler/DynamicEngine double rollback with one bounded orchestration path.
9. Wire the coordinator once under `DynamicEngine.transitionMu`.
10. Add unit, fault-injection, concurrency, cancellation, restart-recovery, and real-binary acceptance tests.
11. Only after all tests pass, remove/retire direct `config.yaml` writes and legacy reload paths.

## Revised acceptance criteria

Stage 2 may be accepted only when all of the following are demonstrated:

- every production Mihomo config mutation reaches one coordinator and no alternate direct writer remains;
- the exact candidate is validated before active-file replacement;
- success means the expected runtime generation and all required listeners are ready and bridges are published;
- every injected in-process failure restores the same verified disk/runtime/native-store/bridge state;
- cancellation cannot interrupt mandatory recovery after the commit boundary;
- daemon restart from every persisted phase converges deterministically;
- rollback failure persists and exposes `recovery_required` while NDMS exports remain withdrawn;
- no duplicate reload, recursive rollback, deadlock, or secret-bearing error occurs;
- Stage 1 acceptance, affected package tests, bootstrap acceptance, `go test ./...` (or an honestly documented environment exception), and `git diff --check` pass;
- no IPK build or router deployment is part of development acceptance unless the user explicitly requests it later.

## Handoff decision

Return the implementation plan to the author for revision. The architecture is directionally sound, but the seven P0 findings above must be incorporated before coding starts. In particular, the revised plan must choose one transaction owner and explicitly model generation-aware readiness, independent rollback context, persistent crash recovery, and coordination with native-store mutation rollback.

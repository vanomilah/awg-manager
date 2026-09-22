# Mihomo Gate B Rev10 — acceptance audit and FIX NOW

**Date:** 2026-09-22  
**Repository:** `E:\AWGM\awg-manager`  
**Branch:** `feature/mihomo-ai-proxyrt`  
**Reviewed artifacts:**

- `reports/mihomo/GATE_B_REV10_DIFF_2026-09-22.patch`
- `reports/mihomo/MIHOMO_REMEDIATION_GATE_B_REV10_RESOLUTION_REPORT_2026-09-22.md`

## Verdict

**REJECTED. Gate B is not accepted.**

Rev10 adds substantial useful work and all targeted test suites pass, but the production startup-migration recovery still has correctness and filesystem-safety blockers. The resolution report overstates deterministic convergence and atomicity.

This document is an **implementation task**, not a request for another plan or review. Fix the code and tests directly. Do not produce another implementation plan.

## Verification performed independently

The following commands passed on the current working tree:

```text
go test -count=1 ./internal/mihomo ./cmd/awg-manager ./internal/mihomonative ./internal/singbox/router
go test -race -count=1 ./internal/mihomo ./cmd/awg-manager
git diff --check -- internal/mihomo cmd/awg-manager internal/mihomonative internal/singbox/router
```

Passing tests do not close the defects below because the migration tests use permissive fakes and do not inject failures into the recovery writes themselves.

## What Rev10 fixed correctly

- Added bridge identity version and digest fields to authoritative records.
- Added strict complete-ref and duplicate-slot checks for current-version records.
- Removed the broad cache-membership authorization bypass and introduced previous/target transaction roles.
- Made referenced generation bundle reads in the normal migration path stricter.
- Added unique migration generation IDs.
- Added real subprocess crash tests for eight normal migration crash boundaries.
- Targeted package tests and race tests pass.

## P0-1 — migration recovery trusts an unvalidated manifest and unsafe paths

`recoverMigrationTransactionIfPresentLocked` decodes `config.yaml.txn.json`, checks only `OperationKind`, and immediately consumes:

- `PreviousRecordFile` with `os.ReadFile` and later `StrictUnlink`;
- `PreviousLKGPointerFile` with `os.ReadFile` and later `StrictUnlink`;
- `CandidateGenerationID` in `filepath.Join(generationsDir, CandidateGenerationID)` followed by `os.RemoveAll`.

It does **not** call `ValidateSchemaForPhase()` and does not prove that backup paths are the exact transaction-scoped files inside `ConfigDir`, or that the candidate directory is a direct child of `generationsDir`.

This is both a corruption bug and a filesystem-safety issue. A malformed durable manifest must never drive reads/unlinks outside the managed directory or recursive removal through a traversal candidate ID.

### Required implementation

1. Before any migration recovery side effect, validate the complete manifest and phase.
2. Add strict containment checks using resolved/clean paths:
   - backup files must be direct children of `ConfigDir` with the exact expected transaction-derived basenames;
   - candidate generation ID must pass `ValidateBasename`;
   - candidate directory must resolve as a direct child of `GenerationStore.GenerationsDir()`;
   - symlink/reparse-point escape must fail closed.
3. On validation failure, preserve evidence, enter `recovery_required`, and do not modify authoritative files or foreign paths.
4. Do not use raw `os.RemoveAll` until the candidate path has been verified. Prefer a strict generation-store removal helper with containment and directory fsync.

### Required tests

Use fresh-process or disk-backed tests for malicious/corrupt manifests containing:

- `CandidateGenerationID: ".."` and `../outside`;
- an absolute `PreviousRecordFile` outside `ConfigDir` whose basename contains the txid;
- an outside/symlinked `PreviousLKGPointerFile`;
- invalid sequence/state/digest fields.

Assert that outside sentinel files remain byte-identical and no outside directory is removed.

## P0-2 — pre-commit crash recovery leaves an unusable legacy authoritative state

For `migration_intent`, `candidate_bundle_published`, and `verified_active_write_intent`, recovery restores the old legacy record, deletes the transaction manifest, and writes `migration.rolled_back`.

Later in the same `RecoverOnStartup` call:

- `recoveredMigration == true`, so startup migration is skipped;
- the restored record still has legacy/incomplete bridge refs;
- `ReplaceDurableBridges(rec.AppliedBridges)` is called.

The production `mihomoBridgeRuntime.ReplaceDurableBridges` requires `ValidateComplete()` and rejects those legacy refs. The crash tests pass because `persistentNDMSRuntime` is more permissive than the production registry. On the following restart, `migration.rolled_back` suppresses migration again, so the marker can strand the installation permanently in legacy state.

This contradicts the Rev10 guarantee of successful deterministic convergence and the rule that legacy records are accepted only as migration input.

### Required implementation

Choose and implement one coherent behavior:

- **Preferred:** after restoring the complete old pair, immediately start a fresh migration from that legacy pair in the same startup under the same process lock; or
- restore old, finish startup without publishing legacy refs to the current-version registry, then retry migration deterministically on the next startup.

`migration.rolled_back` must not permanently suppress a necessary migration. A legacy `AppliedGenerationRecord` must never be installed into `DurableBridgeRegistry` as if it were current-version state.

### Required tests

- Run all three pre-commit hard-crash cases with the **production validation semantics** of `ReplaceDurableBridges`.
- The first fresh restart must return success and `StateIdle`, with a current bridge identity record and complete refs, or follow a precisely documented retry state that succeeds on the next restart without manual marker deletion.
- A second restart must be idempotent.
- Assert no legacy record is passed to `ReplaceDurableBridges`.

## P0-3 — roll-forward recovery ignores authoritative write and validation failures

The post-boundary branch in `recoverMigrationTransactionIfPresentLocked` suppresses critical errors:

- candidate `ReadGenerationBundle` failure is ignored;
- candidate record `json.MarshalIndent` error is ignored;
- `StrictWriteAtomic(verified-active.json)` failure is ignored;
- decoding the current verified-active record is ignored;
- `AdvanceLKGPointer` failure is ignored;
- directory fsync, backup unlink, and manifest unlink failures are ignored.

The function can therefore return `(true, nil)` and remove the transaction evidence while `verified-active.json` and `lkg.pointer.json` are absent, corrupt, or disagreeing.

### Required implementation

1. Fail closed on every authoritative read, decode, schema validation, digest comparison, write, and fsync.
2. Reconstruct the candidate record only from a fully validated candidate bundle and verify its serialized digest against `CandidateRecordDigest`.
3. After writing both files, re-read and validate both records and assert agreement of:
   - generation ID;
   - generation number;
   - config digest;
   - store digest;
   - current bridge identity version and bridge digest.
4. Only after that agreement may recovery checkpoint committed state and remove the manifest/backups.
5. Cleanup failures must be handled explicitly. Do not report successful recovery if removal durability is unknown.

### Required tests

Inject failures during recovery, not only during the original migration:

- candidate bundle missing/corrupt;
- verified-active rewrite failure;
- pointer rewrite failure;
- verified-active re-read/decode failure;
- pointer re-read/decode failure;
- manifest cleanup failure;
- backup cleanup failure.

Assert no false `StateIdle`, no false success, no deletion of recovery evidence, and deterministic success after the injected fault is removed.

## P0-4 — normal I/O failure is blocked forever before transaction recovery

On `FailAdvanceLKGPointer` or a real pointer write failure, the normal migration path restores old `verified-active.json`, leaves an in-flight post-boundary manifest, writes `recovery.marker`, and returns an error.

On the next startup, `RecoverOnStartup` checks `recovery.marker` **before** `recoverMigrationTransactionIfPresentLocked`, immediately returns `ErrRecoveryRequired`, and never resolves the durable migration transaction. Existing `RestartAfterInjectedFailures` explicitly expects this blocked behavior, which conflicts with the report's claim of automatic deterministic convergence.

### Required implementation

- Transaction-owned, structurally valid recoverable migration state must be reconciled before a generic marker blocks startup.
- Preserve fail-closed behavior for corrupt/foreign/unverifiable state.
- Define marker precedence explicitly: a generic marker must not prevent safe recovery of the very transaction that created it.
- After successful reconciliation, clear only the matching transaction-owned marker; never clear unrelated evidence.

### Required tests

Replace the current expectation that restart remains `ErrRecoveryRequired`. For recoverable injected write failures, remove the injection and verify that a fresh coordinator converges safely and starts. Corrupt or foreign state must remain blocked.

## P1-1 — backup creation is not fail-closed

Before writing migration intent:

- failure reading the LKG pointer is ignored;
- strict JSON validation of the old pointer is not required;
- failure writing the pointer backup is ignored;
- an empty/nonexistent pointer backup path can still be placed in the manifest.

The migration may then cross its commit boundary without a proven rollback source.

### Required implementation

- For a generation-backed legacy record, require a present, strictly decoded, schema-valid LKG pointer agreeing with the old applied record.
- Require successful durable backup writes and verify backup digests before persisting migration intent.
- Store and validate the exact old record and pointer digests in the manifest.
- If the system formally supports a pre-pointer legacy format, represent it explicitly rather than treating arbitrary read failure as absence.

## P1-2 — rollback helper suppresses restoration failures

`rollbackMigrationBeforeCommitLocked` returns no error and ignores all restore, fsync, candidate removal, backup unlink, manifest unlink, and active-role cleanup failures. Callers can overwrite the useful cause with a generic failpoint error and claim that rollback occurred when it did not.

### Required implementation

- Change it to return an error/result describing restoration and cleanup.
- Verify restored authoritative file digests before deleting transaction evidence.
- Keep the manifest/backups and enter `recovery_required` when restoration is incomplete.
- Propagate `SetActiveTransactionRoles` failures instead of discarding them where transaction authorization correctness depends on the update.

## P1-3 — role reconstruction validates too little durable state

`isAuthorizedPortReplacementLocked` rebuilds roles from disk after only `TransactionManifest.ValidateSchema()`. It must use phase validation and accept only explicitly enumerated in-flight states for which previous/target authorization is valid. A merely non-terminal state is too broad.

### Required implementation

- Use `ValidateSchemaForPhase()` plus manifest-file/path provenance checks.
- Maintain an allowlist of transaction states that legitimately authorize a previous/target port replacement.
- Do not authorize from migration-only states unless that operation actually needs NDMS replacement.
- Add negative tests for malformed digests, incomplete refs, invalid transitions, migration states, and a stale manifest from a completed transaction.

## Report corrections required

The next resolution report must not claim:

- “mathematically proven”;
- “all requirements fully implemented”;
- automatic convergence for injected I/O failures while tests expect permanent `ErrRecoveryRequired`;
- atomicity when recovery writes or rollback writes ignore errors.

State only what is demonstrated by named tests and independent command output.

## Mandatory implementation/acceptance sequence

1. Fix P0-1 through P0-4 in production code.
2. Make rollback and roll-forward error propagation explicit.
3. Replace permissive migration registry fakes with production-equivalent validation.
4. Add recovery-write failure injection and path-containment tests.
5. Run:

```bash
gofmt -w internal/mihomo cmd/awg-manager
go test -count=1 ./internal/mihomo ./cmd/awg-manager ./internal/mihomonative ./internal/singbox/router
go test -race -count=1 ./internal/mihomo ./cmd/awg-manager
git diff --check -- internal/mihomo cmd/awg-manager internal/mihomonative internal/singbox/router
```

6. Produce only implementation evidence:
   - `reports/mihomo/MIHOMO_REMEDIATION_GATE_B_REV11_RESOLUTION_REPORT_2026-09-22.md`
   - `reports/mihomo/GATE_B_REV11_DIFF_2026-09-22.patch`

Do **not** build an IPK. Do **not** deploy to routers. Do **not** use `--force-reinstall`. Preserve unrelated dirty-worktree changes.


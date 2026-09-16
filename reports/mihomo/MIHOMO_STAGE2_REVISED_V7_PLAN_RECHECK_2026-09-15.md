# Mihomo Stage 2 Revised v7 — Plan Recheck

Date: 2026-09-15  
Reviewed source: `C:\Users\Ivan\.gemini\antigravity\brain\e4046ae6-25b5-4760-af29-649b283af6f4\implementation_plan.md`  
Previous review: `MIHOMO_STAGE2_REVISED_V6_PLAN_RECHECK_2026-09-15.md`  
Workspace: `E:\AWGM\awg-manager`  
Branch observed: `feature/mihomo-ai-proxyrt`

## Verdict

**Revised v7 is substantially closer, but it is still not implementation-ready.**

The Go signature collision and strict-filesystem package ambiguity are fixed. The plan also makes the correct high-level choices for a verified applied generation, controlled restart, startup gating, and recovery marker.

The remaining defects are no longer cosmetic. The draft and non-native desired-state protocols still lack durable recovery definitions, `InspectOutcome` is declared but not integrated into the transaction tables, persisted process generations cannot identify a process across daemon restarts, and the proposed administrative `force_stable_new` path is unsafe with corrupt evidence.

No production code, build, IPK, or router deployment was performed. Existing working-tree changes were preserved; only this report was added.

## Confirmed improvements over v6

1. `ListBridges()` retains its existing compilable signature.
2. Strict filesystem operations are moved to a neutral `internal/strictfs` package.
3. `AppliedGenerationRecord` includes applied config, store, listeners, and bridges.
4. `BeforeBridges` is correctly sourced from the applied generation rather than drafted live state.
5. Compilation is explicitly separated from legacy migration.
6. Transactional apply uses controlled restart instead of unverifiable hot reload.
7. Startup and degraded API gating are acknowledged.
8. `StrictUnlink` is required to fsync the parent on `ENOENT`.
9. A separate recovery marker preserves corrupt manifest evidence.

## Blocking findings for v8

### P0-1. `DraftJournal` still cannot recover its own write sequence

The schema defines only one string state, `draft_persisted`, while the algorithm writes the journal before mutation and rewrites it after mutation. A crash before the rewrite leaves a structurally incomplete journal with no defined state or target digest.

There is still no draft crash-recovery table covering:

- snapshot durable, journal absent;
- intent durable, store unchanged;
- store rename visible but its directory fsync failed;
- store mutated, journal still at intent state;
- journal target digest durable, HTTP response lost;
- apply committed, draft snapshot/marker cleanup interrupted.

**Required correction:** introduce typed draft states such as `snapshot_secured`, `intent_secured`, `store_mutated`, `pending`, and `consuming`. Define legal fields and deterministic startup recovery for every state. The journal must identify and clean its snapshot, and every cleanup step must be idempotent.

### P0-2. Existing drafts plus a new `apply=true` mutation are underspecified

The plan says a failed apply retains “the store draft”, but an `apply=true` request can arrive while older pending edits exist and can itself perform a new mutation. On failure, the old edits should remain while only the new mutation is rolled back. On success, both become applied.

The current manifest fields do not explicitly distinguish:

- last applied store digest;
- desired digest before the current request;
- desired digest after the current request.

**Required correction:** rename and define these as separate invariants (for example `AppliedStoreDigest`, `BaseDesiredStoreDigest`, `TargetDesiredStoreDigest`). Snapshot the base desired store before the new mutation. Specify apply-without-new-mutation as a first-class operation for an “Apply pending changes” button.

### P0-3. Non-native “durable desired state” is represented only by a volatile flag

For slot changes, v7 proposes `d.pendingConvergence.Store(true)`. An `atomic.Bool` is lost on daemon restart, so this is not a durable desired-state model.

For settings, returning an error after `settings.json` was saved still leaves desired settings changed. No applied settings/slot revision, pending marker, startup retry, or rollback snapshot is defined.

**Required correction:** create a durable convergence journal/record containing normalized digests or generations for every non-native compiler input. On startup compare current desired-input digest with the applied generation's input digest and retry or expose pending state. Do not rely on an in-memory boolean.

### P0-4. `AppliedGenerationRecord` lacks the complete input identity

`AppliedStoreDigest` covers only `mihomonative.Store`. Mihomo compilation also reads router settings/config, subscription slot, tunnel slot, AWG slot or live catalog fallback, device-proxy inputs, and other flags.

Without an aggregate input fingerprint, the service cannot determine whether runtime is current after a non-native change or restart.

**Required correction:** add a deterministic `AppliedInputDigest` computed from an immutable normalized compiler-input snapshot. Persist the individual source revisions/digests for diagnostics. `pending_convergence` is derived by comparing the current desired input digest with this applied digest.

### P0-5. The compiler is not guaranteed a coherent immutable input snapshot

Removing `ImportLegacyGroups` and `ImportLegacyRules` is necessary but not sufficient. The current generator reads several stores through separate calls. If a subscription refresh, slot update, bridge edit, settings update, or catalog fallback changes between reads, one candidate can combine different generations.

**Required correction:** define a `MihomoCompileInput` value assembled under the outer transition/source locks, containing deep copies of every input. The pure compiler accepts only this value. It must not reach into live stores, use `context.Background()`, or invoke live catalogs itself.

### P0-6. Legacy migration remains an unjournaled mutation

`MigrateLegacyMihomoResources(ctx)` is described only as “run once before compilation”. It mutates the native store, but no transaction owner, once-marker, rollback, startup ordering, or failure behavior is given.

**Required correction:** run migration through the same durable mutation coordinator before baseline bootstrap, or make it one atomic store transformation with a versioned schema migration and strict persistence. A crash between group and rule import must not leave a half-migrated store.

### P0-7. `InspectOutcome` is not used by the restoration/recovery protocol

The plan correctly declares `OutcomeNewApplied`, `OutcomeOldRetained`, and `OutcomeAmbiguous`, but the phase table still treats every write/rename error as if the old state definitely remained.

Missing ambiguous cases include:

- store write renamed, directory fsync failed;
- manifest transition renamed, directory fsync failed;
- candidate/LKG/active rename became visible, fsync failed;
- verified metadata became visible, fsync failed;
- `StateCommitted` became visible, fsync failed.

**Required correction:** add an outcome-action table for every durable file operation. Re-read destination and compare old/new digests. Continue from the observed state when provable; otherwise enter recovery-required. A possibly committed transaction must never be rolled back or repeated blindly.

### P0-8. Restart identity is not yet proven across daemon restarts

For a normal in-process apply, a fresh child PID and incremented generation are useful. But `ProcessGeneration` is an in-memory counter and resets when AWG Manager restarts. A persisted value in `AppliedGenerationRecord` cannot identify a process in the next daemon epoch.

Recovery from `published` currently says to “verify runtime running”. After manager restart, the operator may not own the old child, and a stale independent Mihomo process/controller may answer.

**Required correction:** on startup recovery never trust persisted process generation alone. Use a boot/session epoch plus PID start time and executable/config-dir ownership, or stop/clean the recognized managed process and perform a new controlled start from the selected stable config. Recompute the new runtime receipt before completing recovery.

### P0-9. Controlled restart must re-verify the active file digest

A fresh process proves that a new process started, but not by itself that the bytes read from `config.yaml` still equal `TargetConfigDigest`. The plan checks only top-level controller fields after start.

**Required correction:** verify active-file digest immediately before spawn and again after readiness. Reject unexpected file replacement. Verify all owned listener socket inodes against the fresh PID, not merely that TCP dialing succeeds. Record the resolved binary path/config directory in the runtime receipt or verify them from `/proc/<pid>`.

### P0-10. RuntimeOff/file-absence semantics still contradict mode semantics

The transition matrix contains `Sidecar 0 Listeners` and `Last Bridge Deletion` with `TargetMode=exports`, `FileAbsent`, and `ProcessStopped`. In the current runtime model, no sidecar demand means the desired mode is `off`, not `exports`.

The main recovery table still says `runtime_applied` restores LKG and starts the previous runtime without checking first-install/file-absent/process-stopped fields, contradicting the newer transition matrix.

**Required correction:** use `RuntimeOff` for zero-listener sidecar states, or explicitly redefine what `exports` means. Expand the main phase/recovery tables so their actions dispatch on the file/process/mode matrix instead of retaining unconditional “restore LKG/start runtime” text.

### P0-11. Recovery tables cover only expected digest layouts

Rows assume active, LKG, and snapshot files exist and match recorded digests. Real recovery also encounters missing snapshot, missing LKG, active matching neither old nor target, unexpected candidate, invalid applied record, and store digest matching neither base nor target.

**Required correction:** add a mismatch/evidence matrix. Any layout not uniquely reducible to stable-old or stable-new must preserve evidence and enter recovery-required. Never unlink a snapshot or manifest merely because one expected field matches.

### P0-12. Request cancellation and panic boundaries are not defined

The HTTP request context may be cancelled after store mutation, active swap, process stop, or bridge withdrawal. Rollback cannot then use the cancelled context. Likewise a panic inside `mutateFn` must not leave the manifest and locks in an unknown state.

**Required correction:** before the first persistent mutation create an internal bounded recovery context independent of the request. Cancellation may stop pre-commit preparation, but after the commit boundary the coordinator must finish rollback/commit or persist recovery-required. Recover mutation panics, restore from the durable snapshot, and return a sanitized internal error.

### P0-13. `force_stable_new` is unsafe for corrupt-manifest recovery

When the manifest is corrupt, the system cannot know the target config/store/bridge digests or whether publication completed. An endpoint named `force_stable_new` risks blessing precisely the unverified state that the fail-closed design forbids.

**Required correction:** remove generic `force_stable_new`. Allow stable-new only when a fully parseable manifest and all recorded invariants verify. For corrupt evidence, offer rollback to a verified LKG/applied record, evidence export, or explicit re-generation from current desired inputs followed by normal validation and controlled restart.

### P0-14. Recovery evidence may leak credentials

The proposed evidence bundle includes manifests, logs, and digests; nearby artifacts can contain proxy URLs, tokens, passwords, provider URLs, and native snapshots. The plan does not define redaction, archive permissions, size limits, or authentication beyond the generic route.

**Required correction:** make recovery endpoints admin-only, CSRF-protected where applicable, and rate-limited. Export a redacted structured report by default. Raw snapshot/config download must require an explicit separate action and warning, use `0600`, and never be embedded into ordinary status responses or logs.

### P0-15. Startup failure and degraded HTTP startup contract is incomplete

The plan says reconciliation completes before HTTP starts, but degraded recovery endpoints must remain available when reconciliation cannot complete. It does not say whether `Startup` returns an error, a state result, or starts a limited router.

**Required correction:** define `StartupResult{Mode: Ready|Degraded, Err: sanitized}` or equivalent. Fatal construction errors stop the daemon; recoverable transaction errors start only the gated read-only/recovery surface. Schedulers and callbacks remain disabled until a successful administrative reconcile transitions to Ready.

## High-priority gaps

### P1-1. OpenAPI and frontend changes are still absent from the component list

The plan adds recovery endpoints and new status/outcome fields but does not list edits to `internal/openapi/swagger.yaml`, generated schemas, API clients, or UI. These must be part of the implementation and acceptance commands.

### P1-2. The recovery API action contract is too vague

Define request/response schemas, preconditions, idempotency key, conflict status, audit logging, progress reporting, and behavior when recovery is already running. Recovery must share the same transition lock and cannot run concurrently with startup or mutation.

### P1-3. Bridge runtime collaborator interfaces are missing

The coordinator promises incremental publish, withdrawal, exact listing, and verification but no interface defines those operations. Add explicit `BridgeRuntime` methods and ownership rules. Persist the receipt after each successful external side effect before proceeding.

### P1-4. TxID generation and artifact ownership remain unspecified

Validation is present, generation is not. Define collision-resistant creation, exclusive file creation, retry behavior, and exact expected filenames derived from TxID. Recovery must compare manifest filenames to derived values rather than trusting arbitrary valid basenames.

### P1-5. Existing `StopAndWait` must be included in the baseline contract

The current dirty working tree already contains `Operator.StopAndWait(ctx)`. The plan should state whether Stage 2 reuses it unchanged or strengthens/tests it. It must verify PID reaping and reject stale callbacks without deadlocking the coordinator.

### P1-6. Test matrix still omits claimed guarantees

The 45 tests include only one draft happy path and one settings error test. Missing are draft crash states, retained previous drafts, durable non-native convergence across restart, immutable input snapshot, migration crash, post-rename outcomes per file, cancelled request, mutation panic, unsafe recovery action rejection, redaction, and OpenAPI/frontend tests.

## Required v8 delta

1. Add typed draft states and a complete draft crash/recovery/cleanup table.
2. Separate applied, base desired, and target desired digests explicitly.
3. Define apply-pending-without-new-mutation and apply-with-new-mutation behavior.
4. Replace volatile `pendingConvergence` as authority with a durable aggregate input revision record.
5. Build a deep immutable `MihomoCompileInput` and compute `AppliedInputDigest` from it.
6. Put legacy migration inside a durable atomic migration/transaction boundary.
7. Integrate `InspectOutcome` into every write/rename state transition and recovery decision.
8. Define process identity and controlled restart across daemon epochs.
9. Re-check active digest and PID-owned sockets around every controlled start.
10. Resolve exports-zero-listener versus RuntimeOff semantics and update all tables consistently.
11. Add recovery behavior for all missing/mismatched artifact layouts.
12. Make rollback independent from cancelled request contexts and handle mutation panics.
13. Remove unverified `force_stable_new` for corrupt manifests.
14. Secure and redact recovery evidence/API operations.
15. Define Ready versus Degraded startup results and post-reconcile activation of schedulers/callbacks.
16. Add bridge runtime interfaces, TxID generation, exact filename derivation, and recovery locking.
17. Include OpenAPI, generated frontend schemas, clients, UI states, and their tests.
18. Expand the test matrix to cover every guarantee above rather than only happy paths.

## Minimum additional acceptance tests

1. Crash at every draft state boundary converges to the correct desired revision.
2. Existing draft plus new mutation failure retains only the earlier draft.
3. Apply pending changes with no new mutation succeeds idempotently.
4. Pending non-native convergence survives daemon restart.
5. Any compiler input change alters `AppliedInputDigest` deterministically.
6. Concurrent input mutation cannot produce a mixed-generation candidate.
7. Crash halfway through legacy migration leaves either old or fully migrated store.
8. Post-rename fsync failure for store, manifest, active, LKG, applied record, and draft is classified correctly.
9. `StateCommitted` ambiguous write cannot repeat or roll back a committed mutation.
10. Published recovery after daemon restart rejects an unowned stale controller.
11. Active digest replacement between swap and readiness is detected.
12. All required TCP/UDP sockets are owned by the expected new PID.
13. Zero bridge demand resolves to the single documented runtime mode.
14. Missing snapshot/LKG/candidate and unknown digests enter recovery-required safely.
15. Request cancellation after active swap still completes rollback with an internal context.
16. Panic in `mutateFn` restores the snapshot and releases all locks.
17. Corrupt manifest cannot invoke stable-new adoption.
18. Default evidence export contains no credentials, URLs with userinfo, tokens, or raw YAML.
19. Degraded startup exposes only permitted endpoints and does not start schedulers.
20. Successful administrative reconcile atomically opens gates and starts deferred services once.
21. Concurrent recovery requests serialize and are idempotent.
22. OpenAPI and generated frontend types cover all recovery and pending outcomes.

## Approval condition

Approve the next revision only when draft and non-native convergence are durably recoverable, compilation consumes one immutable input generation, ambiguous filesystem outcomes drive explicit state decisions, runtime identity survives daemon boundaries, every file/mode mismatch fails closed, and recovery endpoints cannot bless or leak unverified state.

v7 makes the right broad architectural choices, but its claim that all prior P0/P1 findings are fully resolved is not yet supported by executable contracts and tests.

# Review of Mihomo Stage 2 Gate 1 remediation plan v12

Date: 2026-09-15  
Repository: `E:\AWGM\awg-manager`  
Branch observed: `feature/mihomo-ai-proxyrt`  
Reviewed inputs:

- `C:\Users\Ivan\.gemini\antigravity\brain\e4046ae6-25b5-4760-af29-649b283af6f4\implementation_plan.md`
- `C:\Users\Ivan\.gemini\antigravity\brain\e4046ae6-25b5-4760-af29-649b283af6f4\walkthrough.md`

## Verdict

The v12 plan is substantially better than the earlier revisions. It now separates planning from implementation, introduces stable generation IDs, explicit abort and rollback states, crash-recovery cases, a protected generation set for GC, and a dedicated filesystem-safety section.

However, **v12 should not yet be approved for implementation as written**. Several remaining inconsistencies can still cause loss of the last known-good configuration, incomplete rollback, accidental bridge removal, or path traversal through an intermediate symlink.

Recommended status: **revise to v13, then implement**.

The updated `walkthrough.md` is now honest: it describes a remediation specification awaiting approval rather than claiming that the code has already been completed. That is the correct role for the walkthrough at this stage.

## Blocking findings

### P0-1. The LKG pointer is advanced too late to support rollback

The sequence advances the last-known-good pointer only after the new generation has committed. But failures that require rollback occur earlier: during runtime swap, reload, verification, or bridge publication.

On a first transition from generation 1 to generation 2, the LKG pointer may not exist yet. A rollback handler that reads only that pointer therefore has no reliable source for generation 1.

Required correction:

1. Persist `PreviousGenerationID` in the transaction manifest before crossing the commit boundary.
2. Publish and verify the candidate bundle.
3. Before runtime swap, atomically make the rollback pointer refer to `PreviousGenerationID`; alternatively, make rollback use the manifest field directly and treat the pointer as a derived index.
4. Resolve ambiguous pointer-publication outcomes explicitly. An unknown result must enter `recovery_required`.
5. After a successful commit, the rollback target must still be the previous verified generation, not the newly active generation.

The manifest should explicitly contain at least:

- `PreviousGenerationID`
- `CandidateGenerationID`
- `ActiveGenerationID`, when known

### P0-2. The plan does not distinguish the old and candidate store snapshots

The plan mandates a pre-mutation snapshot of the old store, but a durable candidate generation also needs a snapshot of the **post-mutation desired store** matching the candidate configuration and its digest.

These are different artifacts with different purposes:

- `PreMutationStoreSnapshot`: used to abort or roll back the current transaction.
- `CandidateStoreSnapshot`: stored inside the candidate generation and used if that generation later becomes the rollback target.

Required correction:

- Take the first snapshot before mutation.
- Apply the desired store mutation.
- Compile and validate the candidate.
- Take a second snapshot after mutation and verify that its digest equals the candidate desired-store digest.
- Publish the candidate bundle using the candidate snapshot, not the old snapshot.
- Keep the pre-mutation snapshot protected until commit and post-commit cleanup complete.

### P0-3. Runtime-off generations are not defined precisely enough

The wording `config absent (0 bytes / marker)` leaves three incompatible representations. More importantly, rollback code cannot assume every verified generation contains a runnable Xray configuration.

Required correction:

- Add an explicit bundle field such as `ConfigPresent: false` or `RuntimeMode: off`.
- Do not create an empty configuration file and do not assign it a normal config digest.
- Define mode-aware activation and rollback:
  - rollback to an `on` generation restores its config and starts/reloads the runtime;
  - rollback to an `off` generation stops the runtime and restores the associated desired-store state without attempting to run Xray.
- Add a test for `off -> on -> failed activation -> rollback to off`.

### P0-4. `openat(..., relPath, O_NOFOLLOW)` does not protect intermediate path components

`O_NOFOLLOW` protects only the final component. A multi-component relative path can still traverse an intermediate symlink and escape the trusted root.

Required correction for Linux:

- Prefer `openat2` with `RESOLVE_BENEATH | RESOLVE_NO_SYMLINKS | RESOLVE_NO_MAGICLINKS`.
- Since the target router kernel may lack `openat2`, provide a fail-closed fallback that walks every component through directory file descriptors using `O_DIRECTORY | O_NOFOLLOW`.
- Perform create, rename, unlink, and fsync relative to already-open parent directory descriptors.
- Do not silently fall back to ordinary path-based mutation if neither secure method is available.
- Add tests with a symlink in every intermediate component, not just at the final filename.

### P0-5. The proposed bridge journal can delete bridges owned by the old generation

The pseudocode clears `PublishedBridges`, appends newly created bridges, then also appends `toPreserve`. Rollback that withdraws everything in `PublishedBridges` would therefore remove preserved bridges that the current transaction did not create.

It also ignores persistence and withdrawal errors. A crash between the side effect and journal persistence leaves an unknown result.

Required correction:

- Keep separate records:
  - `CreatedBridges`
  - `WithdrawnBridges`
  - optionally per-operation `intent`, `applied`, and `verified` states
- Never classify `toPreserve` as created by the transaction.
- Do not discard errors from manifest persistence or bridge withdrawal.
- If a side effect succeeds but its journal update fails, enter `recovery_required`; recovery must inspect actual bridge ownership/state before deciding what to do.
- Rollback withdraws only transaction-created bridges and re-applies only old bridges whose withdrawal was durably recorded or independently confirmed.
- Make ownership generation-aware and all operations idempotent.

### P0-6. Candidate directory publication needs its own outcome resolver

The plan discusses operation outcomes, but directory rename/publication is not equivalent to a single-file write. A crash around candidate-bundle rename can leave old, new, temporary, or partially fsynced directory state.

Required correction:

- Define `InspectDirectoryPublication` (or equivalent).
- Validate the candidate bundle manifest, required artifacts, digests, permissions, and directory durability.
- Permit abort/removal only when the published candidate is proven to belong to the current transaction.
- Treat ambiguous ownership or durability as `recovery_required`.

## High-priority clarifications

### P1-1. Commit, LKG, and recovery ordering must be one coherent protocol

If `verified-active` is committed and the process crashes before the LKG pointer update, startup sees a committed active generation with a stale rollback pointer. The v13 sequence must choose one authoritative source and state how startup repairs derived indexes.

Recommended model:

1. The transaction manifest is authoritative while a transaction exists.
2. `verified-active` is authoritative for the current committed generation.
3. LKG is an atomic index to the previous verified generation.
4. Startup reconciles indexes from those authoritative records and never guesses from directory timestamps.

### P1-2. GC mixes generation references with ordinary filesystem paths

`DraftSnapshots []string` are paths, while the protected-set algorithm is described as a set of generation IDs. Mixing them makes it easy to protect the wrong object or delete a referenced artifact.

Use typed collections:

- `ProtectedGenerationIDs`
- `ProtectedTransientPaths`
- `ProtectedSnapshotPaths`

GC must validate every reference and fail safely if a live manifest cannot be parsed.

### P1-3. Sequence compare-and-swap semantics are underspecified

The condition `m.Sequence == expectedSeq + 1` is not sufficient unless `expectedSeq` and the durable read/write protocol are defined.

Specify that every transition:

1. Runs under the transaction lock.
2. Reads or validates the current durable manifest.
3. Verifies transaction ID, current state, and exact sequence.
4. Writes the next state with `Sequence + 1` atomically and fsyncs it.
5. Treats an indeterminate write as an outcome requiring inspection, not as a normal error.

### P1-4. Abort must preserve evidence until restoration is proven

For a pre-commit abort, do not remove the candidate bundle, snapshots, or transaction manifest before the old store and runtime postconditions are verified.

Required order:

1. Persist `abort_in_progress`.
2. Restore and verify the old store/runtime state.
3. Record the verified abort result.
4. Move cleanup work to a durable cleanup journal.
5. Remove the transaction manifest only after cleanup is either complete or independently recoverable.

Any cleanup failure must not be reported as clean `idle`.

### P1-5. `cleanup.pending` needs an explicit failure boundary

If writing `cleanup.pending` itself fails after commit, retain the committed transaction manifest with enough information for startup to reconstruct cleanup. Cleanup completion should be idempotent and should never invalidate the committed active generation.

## Required v13 transaction sequence

A minimally coherent sequence is:

1. Acquire the global transaction lock and run recovery/cleanup first.
2. Read and validate current `verified-active`; resolve the previous generation and confirm its bundle is usable, including the runtime-off case.
3. Persist the initial transaction manifest with stable transaction and generation IDs.
4. Create and fsync `PreMutationStoreSnapshot`.
5. Apply desired-store mutation, compile candidate, validate it, and create `CandidateStoreSnapshot`.
6. Publish the complete candidate bundle atomically and inspect the publication outcome.
7. Persist `PreviousGenerationID` and `CandidateGenerationID`; atomically establish the rollback target before runtime swap.
8. Cross the commit boundary and apply runtime changes.
9. Journal every bridge operation with generation ownership and recoverable outcome states.
10. Verify runtime, bridges, config digest, and desired-store digest.
11. Atomically publish `verified-active` for the candidate and persist committed state.
12. Create/complete durable post-commit cleanup without deleting rollback evidence prematurely.

## Tests that must be added or strengthened

In addition to the proposed T01-T14 matrix, add:

1. First successful generation followed by failed second activation, with no pre-existing LKG pointer.
2. Crash before and after rollback-pointer rename/fsync.
3. Candidate bundle contains the post-mutation store snapshot, while abort uses the pre-mutation snapshot.
4. `runtime off -> on -> failure -> rollback off`.
5. Intermediate-directory symlink escape attempts for read, write, rename, and delete.
6. Crash after bridge creation but before journal persistence.
7. Preserved bridge survives rollback of a failed candidate.
8. Old bridge withdrawal followed by crash and recovery re-application.
9. Candidate directory rename with ambiguous outcome.
10. Crash after `verified-active` commit but before cleanup journal creation.
11. GC with malformed live manifest: no referenced generation may be deleted.
12. Concurrent cancel while any filesystem or bridge outcome is unknown: `recovery_required` wins.

## Validation requirements

After implementation, record exact results for:

```text
go test ./internal/mihomo/...
go test ./internal/...
go test ./...
go vet ./internal/mihomo/...
git diff --check
```

Also inspect new untracked files explicitly because `git diff --check` does not validate them. Do not stage or rewrite unrelated user files merely to perform that check.

Router deployment, IPK assembly, destructive cleanup, and `--force-reinstall` are outside this review and must not be performed as part of Gate 1 approval.

## What may proceed now

The other agent may update the specification to v13 immediately. Code implementation should begin only after the v13 plan resolves the P0 items above. The implementation should then be delivered in small, auditable slices:

1. schemas and state machine;
2. secure filesystem primitives;
3. dual snapshot and generation publication;
4. runtime/off-aware rollback;
5. bridge journal and recovery;
6. GC and cleanup;
7. complete fault-injection and end-to-end tests.

## Current implementation status

No evidence was found that the v12 concepts have already been implemented in the current working tree. The existing Gate 1 files still correspond to the earlier implementation shape; expected v12 symbols and fields such as explicit abort/rollback states and candidate/previous/active generation IDs were not present during this review.

No source files were modified. No tests, frontend build, IPK build, or router deployment were performed for this review.

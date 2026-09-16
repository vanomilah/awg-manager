# Review: Mihomo Stage 2 Gate 1 remediation plan v14 (Gemini 3.1 Pro)

Date: 2026-09-16  
Repository: `E:\AWGM\awg-manager`  
Input: `implementation_plan.md` — Revised v14 Final Specification

## Verdict

Gemini 3.1 Pro produced a materially stronger and more concise specification than v13. It correctly understood the three principal v13 blockers and replaced vague prose with useful executable pseudocode.

Nevertheless, v14 is **not ready for implementation without one final correction pass**. The remaining issues are narrower than before, but they sit directly on crash boundaries and can still cause incorrect recovery or unsafe cleanup.

Recommended status: **conditional NO-GO; prepare v15 delta, then implement**.

## What Pro improved successfully

The following changes are sound and should be retained:

- Manifest CAS now compares durable transaction ID, state, and sequence before writing `Sequence + 1`.
- Ambiguous manifest writes are reread and classified as exact old, exact new, or recovery-required.
- Bridge operations now have a durable intent before the external side effect.
- Bundle inspection is routed through `SecureDir` rather than path-based `os.ReadFile`.
- The trusted root is opened with `O_NOFOLLOW` and retained for the transaction.
- Active-config swap gets a durable intent and a digest-based outcome resolver.
- Runtime-off bundles have an enforced empty bridge set.
- GC is explicitly descriptor-relative.
- Cleanup has a persistent journal and merge concept.

Compared with the preceding Flash revisions, Pro addressed the architectural feedback more directly and removed substantial repetition. The remaining problems are mainly protocol-completion defects rather than a misunderstanding of the goal.

## Blocking findings

### P0-1. Journal checkpoints use an undefined self-transition

The bridge protocol persists journal changes using:

```go
c.transitionManifestLocked(m, m.State)
```

The previous state machine did not allow self-transitions, and v14 does not define whether every state may transition to itself. If self-transition is rejected, the bridge protocol cannot work. If every state allows it without phase validation, invalid payload changes can be smuggled through a state checkpoint.

Required correction:

- Introduce a separate `checkpointManifestLocked(m)` operation for same-phase payload changes.
- It must use the same durable CAS and increment `Sequence`, but keep `State` unchanged.
- It must run phase-specific schema validation before writing.
- `transitionManifestLocked` should remain responsible for allowed state changes only.
- Add tests for stale checkpoint rejection and ambiguous checkpoint write outcomes.

### P0-2. The active-config protocol does not actually implement RuntimeOff unlink

Section 2.4 always calls:

```go
secDir.RenameAndResolve(candidatePath, activeConfigFile)
```

But RuntimeOff explicitly has no candidate config file. The subsequent resolver treats an absent active config as success for RuntimeOff, yet no unlink operation is shown. The pseudocode therefore either tries to rename a nonexistent candidate or accidentally treats a pre-existing active config as an unresolved state without performing the required stop/unlink sequence.

Required correction:

```text
if Candidate.ConfigPresent:
    publish an independent active-config staging file
    rename staging -> active
else:
    unlink active using descriptor-relative unlink-and-resolve
```

The RuntimeOff path needs its own exact-old/exact-new/ambiguous outcome table. It must also define the order between stopping the current process and unlinking the active file.

### P0-3. Commit and cleanup ordering still contains an unresolved split-brain window

The cleanup sequence says:

1. Commit `verified-active.json`.
2. Write `cleanup.pending.json`.
3. Transition manifest to `StateCommitted`.

A crash after step 1 leaves the new generation authoritative in `verified-active.json`, while the transaction manifest remains in a pre-commit state. A naïve startup handler can roll back a generation that has already crossed the advertised commit point.

Required correction:

- Define an explicit `CommitIntent`/`PublishingCommit` phase before writing `verified-active.json`.
- Startup recovery must compare `verified-active.json` with `CandidateGenerationID`:
  - exact candidate record means the commit is durable and recovery must roll forward;
  - exact previous record means the commit was not applied and rollback remains allowed;
  - any other value is recovery-required.
- Cleanup-journal write failure after a durable verified-active commit must never trigger runtime rollback.
- Only after this reconciliation may the manifest transition to `Committed`.

### P0-4. The cleanup journal accepts arbitrary absolute paths

The proposed schema contains:

```go
TransientPaths []string
OldSnapshots []string
OldGenerations []string
```

It then unions and deletes those paths. A corrupt or malicious journal can turn recovery into arbitrary filesystem deletion, even if the deletion primitive itself is symlink-safe.

Required correction:

- Store typed identifiers, not arbitrary absolute paths:
  - generation IDs;
  - snapshot basenames under a fixed snapshot root;
  - staging-directory basenames under the generations root.
- Validate every identifier with a strict grammar and reject `.`, `..`, separators, absolute paths, empty values, and alternate-volume syntax.
- Select the appropriate pre-opened secure root by artifact type.
- A malformed cleanup journal must fail closed and preserve all artifacts.

## High-priority findings

### P1-1. Candidate config must not be renamed out of the immutable generation bundle

The name `candidatePath` is ambiguous. If it refers to `generations/<id>/config.yaml`, renaming it into the active location destroys the supposedly immutable generation bundle and makes rollback verification impossible.

Specify that activation creates a separate securely staged copy or reflink from the verified bundle, verifies its digest, and renames that staging file into the active location. The bundle artifact remains untouched.

### P1-2. Bridge ownership is assumed but not defined

The protocol relies on:

```go
actualState.Owner == m.TxID
```

The specification does not explain where ownership is stored or how it survives restart. Kernel interfaces do not inherently expose a transaction ID.

Define a durable ownership registry keyed by bridge identity and generation, or a deterministic naming/alias contract that can be independently reconstructed. Recovery must not trust only the transaction manifest when determining ownership of a live interface.

The withdrawal path also needs full executable pseudocode, not merely an assertion that separate journals exist.

### P1-3. Exact-old bridge checkpoint handling must not rerun the side effect

If bridge creation succeeds but the `Applied` checkpoint write resolves as exact old, the durable journal still contains `Intent`. Retrying the entire creation step can duplicate or conflict with the live bridge.

The retry rule must be:

1. inspect live state again;
2. retry only the journal checkpoint;
3. never reissue creation while a matching owned bridge already exists.

### P1-4. `OutcomeAbsent` remains underspecified

The directory resolver now returns `OutcomeAbsent` when staging and target are both absent, but says only that the caller handles it contextually. Add a phase/outcome table.

For candidate generation publication before any active-state mutation, proven absence may be a normal failed publication followed by abort. After any record claims the bundle was published or after the candidate became active, absence is recovery-required because rollback evidence has been lost.

### P1-5. Bundle verification contract is still represented by a comment

`VerifyBundleIntegrity` contains only:

```go
// Validate manifest, config digest, store snapshot digest, mode, link count strictly inside bundleFd
```

Before implementation, freeze the exact accepted directory schema:

- required and optional filenames;
- rejection of unknown files;
- regular-file and link-count requirements;
- UID/GID and mode requirements;
- config presence rules for each runtime mode;
- manifest canonicalization/version handling;
- store and config digest computation;
- directory and parent durability evidence.

### P1-6. Secure recursive deletion must reject mount crossings and special files

Descriptor-relative walking prevents symlink escape but does not by itself prevent traversal into mounted filesystems. `SecureRemoveAll` must compare `st_dev` with the trusted generation root, reject mount points and special file types, and unlink only known regular files/directories belonging to a validated bundle.

### P1-7. Manifest lifecycle after `StateIdle` is incomplete

The cleanup sequence ends at `transitionManifestLocked(m, StateIdle)` but does not state when the transaction manifest itself is removed or how an ambiguous unlink is resolved. Define whether Idle is a durable tombstone or a temporary state followed by unlink. Startup must handle both exact cases idempotently.

### P1-8. Validation commands disappeared from v14

Restore the explicit verification protocol from v13:

```text
go test -race -v -count=1 ./internal/strictfs
go test -race -v -count=1 ./internal/mihomo
go vet ./internal/mihomo/...
go test -race -count=1 ./internal/strictfs ./internal/mihomonative ./internal/singbox/router ./internal/mihomo
go test -count=1 ./internal/...
go test -count=1 ./...
git diff --check
```

Record timeouts and incomplete runs honestly. Do not build IPK or deploy as part of Gate 1.

## Required v15 delta

Gemini 3.1 Pro does not need to rewrite the complete plan again. A concise v15 delta is sufficient if it freezes these points:

1. Separate manifest checkpoint CAS from state-transition CAS.
2. Separate RuntimeOn rename and RuntimeOff unlink protocols.
3. Define commit-intent and startup roll-forward around `verified-active.json`.
4. Replace cleanup paths with validated typed identifiers.
5. State that active staging is a copy of the immutable bundle config.
6. Define durable bridge ownership and complete withdrawal journaling.
7. Provide the `OutcomeAbsent` phase table.
8. Freeze bundle contents and secure deletion invariants.
9. Complete the Idle/manifest removal lifecycle.
10. Restore exact validation commands.

## Final assessment of Pro vs Flash on this revision

For architecture review, Gemini 3.1 Pro performed better than the preceding Gemini 3.8 Flash revision:

- it addressed the requested issues directly;
- it produced useful pseudocode rather than mostly descriptive promises;
- it reduced the plan from roughly 28 KB to roughly 12 KB without losing the central design;
- it correctly repaired the CAS comparison and bridge-intent ordering.

However, Pro still declared the document “Final Specification” too early and missed four crash/security boundaries introduced by its own pseudocode. It should remain the architect for the v15 delta, with independent review before Flash begins implementation.

No source code, tests, build artifacts, IPK packages, or router state were changed during this review.

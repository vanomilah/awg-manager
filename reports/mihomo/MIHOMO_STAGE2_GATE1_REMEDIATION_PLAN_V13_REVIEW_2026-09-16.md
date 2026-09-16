# Mihomo Stage 2 Gate 1 remediation plan v13 review

Date: 2026-09-16  
Repository: `E:\AWGM\awg-manager`  
Reviewed document: `implementation_plan.md` — **Revised v13 Final Specification**

## Verdict

Version 13 is a meaningful improvement over v12 and closes several architectural ambiguities. It is **not yet safe to approve for implementation** because three P0 defects remain in the proposed protocols and several P1 contracts are still incomplete.

Recommended status: **revise once more to v14, then implement**.

This review concerns the specification. The v13 mechanisms are not present in the current working tree: searches did not find the proposed abort/rollback states, generation identity fields, dual snapshot fields, directory publication resolver, secure component walker, or discrete bridge journals. Existing Gate 1 files remain untracked and belong to the earlier implementation shape.

## What v13 fixed correctly

The following v12 review items are now addressed at specification level:

- The transaction manifest identifies previous, candidate, active, and rollback-target generations.
- The manifest, verified-active record, and LKG pointer have a defined authority hierarchy.
- Pre-mutation and candidate store snapshots are separated.
- Runtime-off generations have an explicit representation with no empty fake config file.
- Generation IDs are stable before publication.
- GC references are divided into generation IDs and filesystem paths.
- Abort and rollback are separate states.
- A committed manifest is retained until cleanup work is durably recoverable.
- Preserved bridges are no longer intentionally added to the list of transaction-created bridges.
- Validation includes repository-wide internal tests and Linux race tests.

These improvements should be retained in v14.

## Remaining blocking defects

### P0-1. Bridge journaling still loses side effects across a crash

Section 2.5 performs each external side effect first and writes the journal second:

```go
ApplyBridges(...)
m.CreatedBridges = append(...)
persistManifestLocked(m)
```

The same ordering is used for withdrawal. A crash or manifest-write failure after `ApplyBridges`/`WithdrawBridges` but before durable persistence leaves the transaction unable to determine what actually happened.

The proposed U06 test explicitly exercises this window, but the design shown cannot pass it safely.

Required v14 design:

1. Use a per-operation journal with at least `intent`, `applied`, and `verified` states.
2. Persist and fsync `intent` before invoking the bridge runtime.
3. Execute the idempotent side effect with transaction/generation ownership.
4. Inspect actual bridge state and ownership after the call, including after an error.
5. Persist the resolved outcome.
6. If the side effect occurred but the outcome cannot be persisted or inspected, enter `recovery_required` rather than ordinary rollback.

Use separate operation records for creation and withdrawal. `toPreserve` must never become transaction-owned.

### P0-2. The directory outcome resolver bypasses the secure filesystem layer

Section 2.6 uses `os.Lstat`, `os.ReadFile`, and `filepath.Join` on full paths. This contradicts section 2.4, which requires descriptor-relative confinement. The resolver is invoked specifically when filesystem state may be adversarial or ambiguous, so it cannot use weaker primitives than the write path.

Additional resolver defects:

- It verifies the config digest but not the candidate store snapshot digest.
- For a runtime-off bundle it accepts the target after checking little more than the manifest identity and `ConfigPresent` value.
- It does not prove that forbidden extra files or symlinks are absent.
- If both staging and target directories are absent, it returns `OutcomeOldRetained`. That is not proof that the old state was retained; the candidate may have been lost after a partially completed operation.
- It does not establish durability of the target directory entry and parent directory.

Required v14 design:

- Implement inspection through `SecureDir` and descriptor-relative reads only.
- Validate generation manifest, generation ID, runtime mode, config presence/absence, config digest, store snapshot digest, ownership, modes, hard-link count, and allowed directory contents.
- `OutcomeOldRetained` is valid only when the intact staging bundle is proven present and the target is proven absent.
- Both target and staging absent must be classified as failed/ambiguous according to the caller's recorded phase, never automatically as old retained.
- A mismatched or unverifiable target is `OutcomeAmbiguous` and forces recovery.

### P0-3. The sequence compare-and-swap invariant is still incorrect

Section 2.8 says that `transitionManifestLocked(m, nextState)` verifies:

```text
m.Sequence == currentSeq + 1
```

Before a transition, the in-memory manifest should match the currently durable sequence. The transition then writes the next sequence. Requiring the current object to already equal `currentSeq + 1` either reverses the comparison or leaves `currentSeq` undefined.

Required v14 contract:

```text
durable.TxID == m.TxID
durable.State == m.State
durable.Sequence == m.Sequence
next.Sequence = m.Sequence + 1
```

The new manifest is then atomically written and its outcome inspected. On an indeterminate write, reread the durable manifest and accept only an exact old or exact new record; every other state becomes `recovery_required`.

## High-priority corrections

### P1-1. The root directory itself is not securely opened

`OpenSecureDir` opens `dirPath` without `O_NOFOLLOW` and does not immediately validate its ownership, type, link count, or permissions. A symlink at the trusted-root boundary defeats the otherwise secure component walk.

Required:

- Open the trusted root with `O_DIRECTORY | O_NOFOLLOW | O_CLOEXEC`.
- `fstat` the root descriptor and validate the expected device/inode policy, owner, and acceptable mode.
- Do not blindly require every ancestor to be exactly `0700`; define which managed directories must be private and which pre-existing trusted parent modes are acceptable.
- Keep the root descriptor open for the complete transaction.

### P1-2. Active-config swap outcome is not specified

The sequence treats `RenameAndResolve(candidatePath, activeConfigFile)` as one successful step, yet the state diagram transitions from `CandidatePublished` to `SwapActive` only after the side effect. A crash after rename but before state persistence leaves the manifest in a pre-swap state while the active config may already be new.

Required:

- Persist a swap intent before rename/unlink.
- Add an active-config outcome resolver that compares the active file digest and presence against both previous and candidate generations.
- For runtime-off, resolve unlink outcomes with the same rigor.
- Cross into rollback territory whenever the candidate active state may have become visible, regardless of the last durably stored state name.

### P1-3. Runtime-off rollback needs previous network-state semantics

The plan says that rolling back to a runtime-off generation ensures no bridges are active. That is correct only if the prior verified off generation truly owns no bridges. This should be derived from and checked against the target generation bundle, not assumed globally.

Specify that rollback restores exactly the target bundle's bridge set and process mode. If runtime-off formally requires an empty set, enforce that invariant when publishing and reading the bundle.

### P1-4. Candidate bundle snapshot naming is ambiguous

The transaction manifest stores `CandidateStoreSnapshotFile`, while the candidate bundle archives the snapshot under a new path. The plan must distinguish:

- transient candidate snapshot path;
- immutable bundle snapshot path;
- their expected identical digest.

After publication, rollback and future activation must reference the bundle path, not a transient file later removed by cleanup.

### P1-5. LKG pointer publication also needs outcome resolution

The plan atomically updates `lkg.pointer.json` before swap but does not show how an indeterminate pointer write is classified. The pointer is derived, yet recovery behavior depends on it as a usable index.

Required:

- Resolve the pointer write as exact old, exact new, or ambiguous.
- On ambiguity, retain the authoritative manifest and enter recovery.
- Startup reconciliation may repair a missing/stale derived pointer only after verifying both referenced generation bundles.

### P1-6. Cleanup journal protocol is described but not specified

The plan says the committed manifest remains until `cleanup.pending.json` is durable, but does not define:

- cleanup journal schema and sequence;
- atomic merge when an older cleanup journal already exists;
- outcome resolution for the cleanup-journal write;
- which artifacts may be deleted at each checkpoint;
- when `StateCommitted -> StateIdle` becomes durable.

Add a concrete idempotent protocol and fault-injection cases for every boundary.

### P1-7. GC still proposes path-based recursive deletion

Section 2.7 mentions `AssertPathConfined` followed by `os.RemoveAll`. A pre-check plus recursive path-based deletion reintroduces TOCTOU risk and is inconsistent with descriptor-relative confinement.

Required:

- Delete generation trees through an already-open generations dirfd.
- Walk entries without following links.
- Reject unexpected file types and mount points.
- Never use a path check as authorization for a later `RemoveAll` operation.

## Test-plan additions required for v14

Keep U01-U14, but make the following acceptance cases explicit:

1. Crash after bridge intent persistence but before side effect.
2. Crash after bridge side effect but before applied-state persistence.
3. Bridge inspection returns unknown after a side-effect error.
4. Target and staging candidate directories are both absent after rename attempt.
5. Runtime-off bundle contains an unexpected `config.yaml` or symlink.
6. Candidate bundle has a valid config digest but invalid store snapshot digest.
7. Trusted root itself is a symlink.
8. Active-config rename succeeds but state-manifest update fails.
9. Runtime-off unlink succeeds but state-manifest update fails.
10. LKG pointer rename has an indeterminate outcome.
11. Existing cleanup journal must be merged with a second committed transaction.
12. A symlink is exchanged between validation and recursive GC deletion; no external file may be touched.

## Corrected implementation order

Before the seven proposed delivery slices, v14 should freeze four protocols in executable pseudocode:

1. Manifest CAS and ambiguous-write resolution.
2. Descriptor-relative bundle publication and inspection.
3. Intent/applied/verified bridge operation journal.
4. Active-config swap/unlink and LKG pointer outcome resolution.

Then implementation can proceed in these slices:

1. Schemas, phase validation, and corrected CAS.
2. Secure root and descriptor-relative filesystem primitives, including secure recursive removal.
3. Dual snapshots and bundle publication/inspection.
4. Active-config and LKG publication protocols.
5. Runtime-mode-aware activation, abort, and rollback.
6. Per-bridge operation journal and recovery.
7. Cleanup journal, protected-set GC, and complete fault injection.

## Approval condition

Implementation may begin after v14 resolves the three P0 findings with concrete pseudocode and updates the P1 contracts above. Do not label the next document “final” merely because the review table is populated; its algorithms must actually make every listed crash window recoverable.

No source code, tests, IPK, or router state were changed during this review.

# Mihomo Gate B Rev9 — acceptance audit and FIX NOW directive

**Date:** 2026-09-22  
**Repository:** `E:\AWGM\awg-manager`  
**Reviewed report:** `reports/mihomo/MIHOMO_REMEDIATION_GATE_B_REV9_RESOLUTION_REPORT_2026-09-22.md`  
**Reviewed patch:** `reports/mihomo/GATE_B_REV9_DIFF_2026-09-22.patch`  
**Walkthrough:** actual file is `walkthrough.md`; the supplied `walkthrough.m` path does not exist.

## Verdict

**REJECTED — do not build or deploy yet.**

Rev9 is a substantial improvement: the new persistent hard-crash tests are real, independently pass, and the previous bridge digest, registry error propagation and finer bridge journal checks exist. However, the central startup-migration guarantee is still false: the implementation can leave `verified-active.json` pointing to the upgraded generation while the LKG pointer still points to the previous generation.

This file is both the acceptance result and a direct implementation assignment. The implementing agent must edit code and tests immediately. Do not produce another plan or review document.

## Independent verification

The following commands passed under WSL:

```text
go test -count=1 -v ./internal/mihomo -run 'TestGateB_Rev8_(StartupMigration|RegistryFailure|Crash_)'
PASS (all listed migration, registry and hard-crash tests)

go test -count=1 ./internal/mihomo ./cmd/awg-manager ./internal/mihomonative ./internal/singbox/router
ok internal/mihomo       32.949s
ok cmd/awg-manager        0.277s
ok internal/mihomonative  0.035s
ok internal/singbox/router 5.792s

go test -race -count=1 ./internal/mihomo ./cmd/awg-manager
ok internal/mihomo 36.818s
ok cmd/awg-manager   1.744s

git diff --check -- internal/mihomo cmd/awg-manager internal/mihomonative internal/singbox/router
PASS
```

Passing tests do not cover the inconsistent authoritative-file state described below.

## P0-1 — startup migration is not atomic

### Evidence

`upgradeLegacyAppliedBridgeIdentityLocked` publishes the new bundle and then calls:

```go
persistAndVerifyGenerationLocked(newRec, true)
```

That function performs authoritative writes in this order:

1. overwrite `verified-active.json` with the upgraded record;
2. only afterwards write the LKG pointer;
3. only afterwards verify both.

If `FailAdvanceLKGPointer` or a real pointer write/fsync failure occurs, step 1 has already changed the authoritative active record. The result is:

```text
verified-active.json -> new upgraded generation
LKG pointer          -> old generation
recovery.marker      -> present
```

That is fail-stopped, but it is not atomic and it does not leave the original authoritative generation unchanged as required and claimed.

The comment saying `persistAndVerifyGenerationLocked` “atomically writes verified-active.json ... and advances the LKG pointer” is incorrect: two separate atomic file replacements are not one atomic multi-file transaction.

### Why existing tests miss it

`TestGateB_Rev8_StartupMigration_AdvanceLKGPointerFailure` checks only:

- an error was returned;
- state is `StateRecoveryRequired`;
- `recovery.marker` exists.

It does not compare the pre/post bytes of `verified-active.json`, does not seed and verify the old LKG pointer, and does not assert that the two authoritative references remain consistent.

`TestGateB_Rev8_StartupMigration_RestartAfterInjectedFailures` only proves that the marker blocks restart. It does not prove recoverability or preservation of the original generation.

### Required implementation

Implement startup migration through a durable transaction/commit protocol, not a direct call to the current two-file helper.

Before the first authoritative mutation, persist an intent containing at least:

- old verified-active record/digest;
- old LKG pointer/digest;
- candidate upgraded generation ID and digests;
- transaction sequence/state;
- previous and target complete bridge refs and their digests.

Use explicit states such as:

```text
migration_intent
candidate_bundle_published
verified_active_write_intent
verified_active_written
pointer_write_intent
pointer_written
migration_committed
```

On restart, deterministically converge to one complete state:

- before the commit boundary: restore/retain the complete old pair;
- after the selected commit boundary: finish the complete new pair;
- never leave ambiguity resolved only by a permanent marker.

The implementation may reuse the existing transaction manifest/CAS journal, but must not create an unrelated second weak journal. All writes and recovery decisions must be digest-verified.

Remove an uncommitted candidate bundle when safe, or retain it as an explicitly unreferenced candidate for normal GC. Never treat its mere existence as a committed migration.

## P0-2 — migration failure tests do not verify the required invariant

Rewrite/add failure-injection tests that seed a real old generation bundle, a real old LKG pointer and old `verified-active.json`, then inject a hard process exit or write failure at every boundary:

1. before candidate bundle publication;
2. after candidate bundle publication;
3. before verified-active replacement;
4. after verified-active replacement;
5. before pointer replacement;
6. after pointer replacement;
7. before committed checkpoint;
8. after committed checkpoint.

For each boundary:

1. terminate the subprocess with `os.Exit(42)` rather than returning normally;
2. instantiate a fresh coordinator with no memory cache;
3. run startup recovery;
4. assert the final pair is either entirely old or entirely new according to the declared commit boundary;
5. assert the chosen generation bundle, active config, store snapshot, applied bridge refs and LKG pointer agree;
6. assert recovery is idempotent on a second restart;
7. assert no foreign NDMS slot was mutated.

The following assertions must be added to ordinary write-failure tests as well:

```go
verifiedActive.GenerationID == lkgPointer.GenerationID
verifiedActive.Generation == lkgPointer.GenerationNumber
verifiedActive.AppliedConfigDigest == lkgPointer.AppliedConfigDigest
verifiedActive.AppliedStoreDigest == lkgPointer.AppliedStoreDigest
```

Do not accept “marker exists” as sufficient proof.

## P1-1 — durable generation records still do not validate bridge identity

`AppliedGenerationRecord.ValidateSchema()` currently validates only version and active-mode config digest. It does not validate:

- complete `AppliedBridges`;
- duplicate slots;
- a digest of the applied bridge list;
- an explicit bridge identity schema version.

`GenerationManifest.ValidateSchema()` similarly validates only version and generation ID.

This means the supposedly authoritative durable generation layer can contain incomplete, duplicate or modified bridge identities and still pass schema validation. Runtime registration may catch some cases later, but rollback reads generation bridge refs before there is a durable-record integrity guarantee.

### Required implementation

Add an explicit bridge identity schema marker and digest to both authoritative record types, for example:

```go
BridgeIdentityVersion int    `json:"bridge_identity_version,omitempty"`
AppliedBridgesDigest  string `json:"applied_bridges_digest,omitempty"`
```

For new records:

- require the current bridge identity version;
- require every bridge to pass `ValidateComplete()`;
- reject duplicate `SlotKey()` values;
- require the digest to match `BridgesDigest(AppliedBridges)`;
- require an empty-list digest policy to be explicit and consistent.

For old records:

- recognize legacy version explicitly;
- allow it only as input to the startup migration path;
- do not allow a legacy generation to be used directly for mutation/rollback without unique enrichment and a durable upgraded generation.

Update every record producer, generation publication, equality check, strict decoder and test fixture.

## P1-2 — same-owner resolver mismatch is bypassed too broadly

The strengthened inspect/withdraw resolver computes `mismatch`, but rejects it only when both conditions are false:

```go
mismatch && !isDurableRef && !isStoreDurable
```

Therefore any stale ref that happens to be present in the runtime durable cache can bypass a same-owner index/interface/port mismatch. The cache was explicitly defined as non-authoritative.

### Required implementation

Do not use cache membership alone to authorize a mismatched same-owner identity.

For a same-owner port-only controlled replacement, authorization must come from the persisted active transaction/generation identities registered with their role, not from an untyped bag of refs. Replace the registry model if necessary with role-aware data:

```text
previous generation refs
target generation refs
active transaction ID / generation IDs
```

Then allow the exact previous/target pair only when it is proven by durable transaction state. Otherwise a same-owner mismatch must fail before NDMS I/O.

Add tests proving:

- arbitrary cached stale same-owner ref cannot bypass mismatch;
- valid persisted port-only A→B replacement still works;
- after transaction completion the old A identity is no longer authorized;
- registry cache loss/rebuild preserves the same decision.

## P1-3 — migration reads authoritative bundle errors permissively

In `upgradeLegacyAppliedBridgeIdentityLocked`, failure of `ReadGenerationBundle` is ignored. Config read errors are also ignored:

```go
if err == nil {
    configBytes, _ = os.ReadFile(cfgPath)
}
```

The code then falls back to active config or proceeds without the previous store snapshot. For an authoritative migration this is too permissive: a corrupt/missing referenced bundle can be hidden by a fallback file, producing a new generation from an unproven mixture of sources.

### Required implementation

If the old record has `GenerationID`:

- require the referenced bundle to exist and validate;
- require config digest equality for active mode;
- require store snapshot digest equality when the record declares one;
- propagate every read error;
- do not silently mix bundle config with current store or active config.

Fallback to legacy active files is allowed only for a formally recognized pre-generation format, with a dedicated migration branch and tests.

## P2-1 — generated upgrade ID is collision-prone

The ID is currently:

```go
gen-%06d-upgrade
```

A failed/retried migration can collide with an already published candidate directory. Use the established generation ID/transaction ID mechanism so retries are unambiguous and journal-linked.

## P2-2 — report claims stronger guarantees than tests prove

Correct the next report:

- do not say “original files remain unchanged on any failure” until tested byte-for-byte and by generation/pointer agreement;
- do not call a sequence of independent file writes atomic;
- distinguish fail-stop from recoverable atomic convergence;
- report the actual target architecture as aarch64 where architecture is mentioned.

## Confirmed improvements to preserve

- `PreviousBridgesDigest` exists and is checkpointed.
- phase validation requires previous/target digests in relevant transaction states.
- ignored `ReplaceDurableBridges` errors were removed from the reviewed coordinator paths.
- complete bridge journal digest checks are stronger.
- observed bridges are keyed by full identity rather than slot alone.
- all ten A→B bridge crash boundaries have dedicated subprocess tests.
- inverse B→A rollback and foreign takeover tests exist.
- persistent NDMS test state survives subprocess crashes.
- current package and race suites pass.

## Mandatory execution order

1. Implement durable startup-migration transaction and restart convergence.
2. Add authoritative applied/generation bridge schema version and digest.
3. Remove cache-based same-owner mismatch authorization; replace with durable role-aware proof.
4. Make referenced bundle reads strict.
5. Add hard-crash migration matrix and strengthen write-failure assertions.
6. Run formatting, focused tests, full package tests and race detector.
7. Produce a focused Rev10 patch and truthful resolution report.

## Required verification

```bash
gofmt -w internal/mihomo cmd/awg-manager internal/mihomonative internal/singbox/router
go test -count=1 ./internal/mihomo ./cmd/awg-manager ./internal/mihomonative ./internal/singbox/router
go test -race -count=1 ./internal/mihomo ./cmd/awg-manager
git diff --check -- internal/mihomo cmd/awg-manager internal/mihomonative internal/singbox/router
```

Run the new migration subprocess matrix separately with `-v` and include its exact output.

## Final artifacts

After implementation, create only:

```text
reports/mihomo/MIHOMO_REMEDIATION_GATE_B_REV10_RESOLUTION_REPORT_2026-09-22.md
reports/mihomo/GATE_B_REV10_DIFF_2026-09-22.patch
```

Do not create another plan. Do not build IPK. Do not deploy. Preserve unrelated working-tree changes.


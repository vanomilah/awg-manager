# Mihomo interactive fast path remediation — implementation report

Date: 2026-09-23  
Branch: `feature/mihomo-ai-proxyrt`  
Scope: false Recovery Mode after package updates, slow rule mutations, concurrent reorder safety, and slow initial tunnel snapshot loading.

## Result

The source-level blockers identified in `MIHOMO_INTERACTIVE_FAST_PATH_ACCEPTANCE_REVIEW_2026-09-23.md` have been corrected. Targeted Go tests, Go race tests, and Svelte/TypeScript validation pass. No IPK was built and nothing was deployed to a router in this work session.

## Implemented fixes

### 1. False Recovery Mode after update/restart

- Separated two previously conflated identities:
  - `AppliedStoreDigest` is the exact archived snapshot digest used for bundle integrity and rollback;
  - `AppliedDesiredStoreDigest` is the normalized semantic digest used to detect real user configuration drift.
- Propagated the semantic digest through applied records, generation manifests, LKG pointers, transaction manifests, normal commits, regeneration, recovery, and rollback.
- Legacy records without the new field are adopted without immediately declaring metadata-only drift to be corruption; the field is upgraded on the next successful commit.
- Early abort now verifies the restored store against `PreMutationStoreDigest`, not against an unrelated desired/applied digest.

Expected effect: package-owned metadata changes no longer produce `preflight store digest mismatch` while actual desired-state changes remain detectable.

### 2. Safe Mihomo hot reload classification

- Fixed `ListenersEqual`: each listener list is now sorted against itself and compared using address, port, network, family, and purpose.
- Hot reload remains fail-closed.
- A rule-only mutation is accepted as hot-reloadable only when the complete active and candidate YAML documents are equal after removing only top-level `rules`.
- Unknown/non-rule YAML changes therefore force the full transactional path instead of being silently treated as rule-only.

### 3. Rule mutation API and idempotency

- Create, update, and delete return authoritative server state: `item`/`deleted`, complete `items`, `revision`, generation, transaction ID, and apply path.
- Reorder requests carry a unique `operationId` and base revision.
- Reusing an operation ID with a different payload returns `409 MIHOMO_OPERATION_ID_CONFLICT`; an old cached result cannot be replayed for another order.
- Stale revision conflicts return the current server rules and revision.

### 4. Frontend mutation behaviour

- Removed the mandatory full page reload after rule create/update/delete.
- UI consumes authoritative `items` and `revision` returned by the mutation response.
- Reorder is optimistic and debounced; only the latest pending order is applied after an in-flight reorder.
- Rule edit/delete/reorder/refresh actions are disabled while a rule mutation is active, preventing overlapping operations and stale revision races.
- On `409 MIHOMO_RULES_STALE`, the client restores authoritative server state.

### 5. Tunnel page initial-load protection

- Snapshot singleflight waiters now observe their own request context and can leave a stuck leader call.
- Managed, external, and system collection deadlines begin together, avoiding sequential timeout accumulation.
- Added a regression test with 25 waiters behind an unresponsive leader; all waiters exit on deadline and only the active leader remains registered.

## Validation performed

Passed:

```text
go test -count=1 ./internal/mihomo ./internal/api ./internal/mihomonative
go test -race -count=1 ./internal/mihomo ./internal/api ./internal/mihomonative
npm run check
git diff --check
```

Results:

- `internal/mihomo`: PASS, including legacy transaction/recovery coverage.
- `internal/api`: PASS, including operation-ID collision and cancellable singleflight coverage.
- `internal/mihomonative`: PASS.
- Race detector: PASS for all three packages.
- `svelte-check`: 0 errors; existing repository warnings remain.

The complete frontend Vitest suite was started but did not finish within 120 seconds while compiling the broad existing frontend suite. It emitted existing Svelte warnings and no failed test result before timeout. This is not recorded as a pass.

## Remaining acceptance work

The following requires a built package and a real router, and was intentionally not performed:

1. Upgrade over an existing Mihomo configuration and confirm no false Recovery Mode banner.
2. Create, edit, delete, and rapidly reorder rules; confirm `X-Apply-Path: hot_reload` for true rule-only changes and no visible multi-second page reload.
3. Confirm connections continue through Mihomo during hot reload.
4. Change listeners/proxy graph and confirm the coordinator deliberately chooses `full_restart`.
5. Open the tunnels page with slow/unavailable external providers and measure first useful render time.
6. Verify recovery/rollback using a deliberately invalid candidate configuration.

Runtime `/rules` equality is not claimed here: the current acceptance proves validated candidate generation, successful reload, process/listener health, durable commit, and rollback behaviour, but does not compare Clash API `/rules` output with the expected normalized rule set.

## Safety notes

- No IPK build.
- No router deployment.
- No `--force-reinstall`.
- No unrelated dirty-worktree files were removed or reverted.

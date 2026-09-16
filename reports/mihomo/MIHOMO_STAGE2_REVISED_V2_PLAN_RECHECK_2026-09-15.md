# Mihomo Stage 2 Revised v2 — Plan Recheck

Date: 2026-09-15  
Reviewed source: `C:\Users\Ivan\.gemini\antigravity\brain\e4046ae6-25b5-4760-af29-649b283af6f4\implementation_plan.md`  
Previous review: `MIHOMO_STAGE2_IMPLEMENTATION_PLAN_REVIEW_2026-09-15.md`  
Workspace: `E:\AWGM\awg-manager`

## Verdict

**The v2 plan is materially improved, but it is still not implementation-ready. Return it for one focused revision (v3).**

The plan now correctly adopts one coordinator, a pure compiler, candidate validation, an independent rollback context, a durable manifest, generation/listener verification, and a `recovery_required` state. Those are the right foundations. However, several contracts cannot compile as written, and crash recovery still lacks enough persistent information to restore the complete state.

No production code, IPK, or router was changed during this review.

## Status of the previous seven P0 findings

| Previous finding | v2 status | Note |
|---|---|---|
| One transaction owner | Partially addressed | Declared correctly, but the compatibility `GenerateMihomoConfig` path creates another callable apply entry. |
| Explicit readiness contract | Partially addressed | Receipt/verifier added, but “controller generation” is not implementable as described. |
| Independent rollback context | Addressed | Detached bounded recovery context is specified. |
| Durable LKG protocol | Partially addressed | LKG staging added, but restore consumes the only LKG and first-install rollback is undefined. |
| Crash recovery | Partially addressed | Manifest added, but native-store/bridge snapshots and exact phase protocol are missing. |
| Unified native-store/config rollback | Not fully addressed | The sample ignores restore errors and lacks old/new bridge snapshots. |
| `recovery_required` terminal state | Partially addressed | State exists, but all failure routes do not lead to it yet. |

## Blocking findings for v3

### P0-1. Package/type ownership cannot compile as written

The plan uses `mihomoRuntimeMode` in:

- `internal/singbox/router.CompileMihomoConfig`;
- `internal/mihomo.ApplyRequest` and `ApplyReceipt`;
- `cmd/awg-manager.DynamicEngine`.

Today `mihomoRuntimeMode` is an unexported type in `package main` (`cmd/awg-manager/dynamic_engine.go`). Neither `internal/mihomo` nor `internal/singbox/router` can reference it.

The plan also defines `CompileResult` and `ListenerSpec` under `internal/singbox/router`, while `internal/mihomo.ApplyCoordinator` consumes those types. `router` already imports `internal/mihomo`, so making `mihomo` import `router` creates an import cycle.

Required v3 correction:

- Move shared contracts to one lower-level package with no dependency on `router` or `cmd`.
- Recommended: define exported `mihomo.RuntimeMode`, `mihomo.CompileResult`, and `mihomo.ListenerSpec` in `internal/mihomo`.
- `router.CompileMihomoConfig(ctx, mode mihomo.RuntimeMode) (mihomo.CompileResult, error)` may then remain a pure compiler because `router` already imports `mihomo`.
- `DynamicEngine` must use the same exported type rather than maintaining a second local enum.
- Do not place router-specific types in the coordinator package if that would pull router dependencies downward.

### P0-2. The manifest phase order is inconsistent and crash-ambiguous

The execution protocol writes `StateDiskCommitted` **before** `SwapActive`. A crash in that gap leaves the old active config in place while the manifest claims the new config is committed. Startup logic that merely validates `config.yaml` will validate the old file and cannot tell whether to roll forward or roll back.

The state diagram lists `preparing`, `runtime_applied`, `published`, and `committed`, but the execution steps do not consistently persist each of those phases. The restart test matrix covers only a subset of the declared states.

Required v3 correction:

- Specify exact manifest write order and meaning. At minimum distinguish:
  - `preparing` — candidate durable, active untouched;
  - `lkg_secured` — previous active durably copied to LKG;
  - `active_swapped` — target digest is now at `config.yaml`;
  - `runtime_applied`;
  - `published`;
  - `recovery_required`.
- Write and fsync a manifest transition only when its invariant is already true, or explicitly model intent and completion as separate phases.
- On startup, hash candidate, active, and LKG and compare them with `TargetDigest` and `PreviousDigest`; syntax validation alone is insufficient.
- Define recovery for every possible file/manifest combination, including missing/corrupt manifest, orphaned candidate, orphaned `lkg.next`, and crash during manifest replacement.
- Add restart tests for every persisted phase, not only three selected states.

### P0-3. Crash recovery cannot restore native-store and bridge state

The manifest persists config digests and modes only. Native Mihomo mutations are written to the native store before compilation. If AWG Manager exits after that mutation but before final commit, the old YAML may be restored while the store still contains the new resources. Old/new NDMS bridge allocations are also not persisted in the transaction manifest.

Required v3 correction:

- Choose and document a recovery policy:
  - **roll forward** when active digest equals target and the persisted mutated store matches the transaction generation; or
  - **roll back** using a durable pre-mutation native-store snapshot and old bridge snapshot.
- Persist a store generation/digest and references to durable before/after snapshots in the manifest.
- Persist enough bridge identity/ownership data to withdraw partially published new bridges and restore old ones.
- Final commit may delete the old snapshot only after runtime verification and bridge publication are durable.
- A snapshot persistence failure must happen before the config commit boundary.
- If neither roll-forward nor rollback can be proven, enter `recovery_required` with the bridge gate closed.

### P0-4. The proposed API-to-DynamicEngine call violates package layering

`internal/api` cannot import or directly call a concrete `DynamicEngine` from `cmd/awg-manager` (`package main`). The sample says `MihomoHandler` calls `DynamicEngine.MutateAndApply`, but no viable dependency contract is defined.

Required v3 correction:

- Define an interface/callback in `internal/api` or a lower-level package, for example:

  ```go
  type NativeMutationApplier interface {
      MutateAndApply(context.Context, NativeMutation) error
  }
  ```

- Wire the concrete `DynamicEngine` adapter from `cmd/awg-manager/wiring_server.go`.
- The mutation contract must carry snapshot/restore functions or a transaction object without importing `package main`.
- Add a compile-time interface assertion and handler integration test proving exactly one apply call.

### P0-5. Store restore failure is ignored and can return a false stable state

The proposed `MutateAndApply` sample executes `_ = restoreFn()`. If restoring the native store fails, the coordinator may have restored old disk/runtime state while the persisted store remains mutated. Returning only the original apply error hides a split-brain state.

Required v3 correction:

- Never discard `restoreFn` errors.
- Store restoration must be part of the recovery state machine, use the independent recovery context where applicable, and be verified.
- Join original, disk/runtime rollback, store restore, and bridge restore errors.
- Any unverified store restoration must persist `recovery_required`, close the publication gate, and block automatic applies.
- Clarify rollback order and justify it. A safe default is: close gate/withdraw new bridges, restore disk, restore native store, revert runtime, verify old runtime/listeners, restore old bridge publication.

### P0-6. Bridge callbacks lack transactional old/new state

`bridgePublish func(ctx) error` and `bridgeWithdraw func(ctx) error` carry no old/new bridge sets or transaction identity. They cannot reliably recover from partial publication or distinguish newly created bridges from previously active ones. This loses information that the current handler deliberately preserves in `beforeBridges` and `afterBridges`.

Required v3 correction:

- Add a typed `BridgeTransition` to the request/receipt with old and target snapshots, owners, ports, and indices.
- Publish must return a receipt describing exactly what changed before an error.
- Rollback must withdraw only partially published target entries and then verify restoration of the previous published set.
- Preserve legacy-owner migration behavior already covered by `mihomo_bridge_runtime_test.go`.
- Test partial success (first bridge published, second fails), deletion, index reuse, and stale-exit callbacks.

### P0-7. First-install rollback leaves a failed target as active config

On first install `SecureLKG` intentionally creates no LKG. If runtime start/readiness then fails, `RestoreLKG` is a no-op, leaving the failed target at `config.yaml` even though the transaction reports rollback.

Required v3 correction:

- Persist `PreviousConfigPresent bool` in the manifest/receipt.
- If false, rollback must atomically remove/quarantine the target active config and sync the directory, then restore the previous stopped/off runtime state.
- Startup recovery must recognize the same case.
- Test first-install failures at runtime apply, readiness, publication, and rollback cleanup.

## High-priority corrections

### P1-1. Do not keep a write/apply compatibility wrapper in the router compiler

The statement that deprecated `GenerateMihomoConfig()` delegates to `CompileMihomoConfig + ApplyCoordinator.Apply` conflicts with the exclusive-owner rule and forces router service to know the coordinator. Existing unit tests are not a reason to preserve an unsafe architecture.

Required correction:

- Migrate production callers to the one DynamicEngine/coordinator path.
- Replace old generator tests with pure compiler tests and coordinator integration tests.
- If a temporary wrapper is unavoidable, keep it outside `router`, clearly test-only, and prevent production wiring from using it.

### P1-2. Idempotency must include runtime convergence

Equal digest and mode do not prove success if Mihomo is stopped, unhealthy, serving a stale controller, or bridges are withdrawn.

Required correction:

- Early success requires verified active digest metadata **and** current runtime/readiness/bridge state.
- Otherwise skip the disk swap but still converge runtime and bridges transactionally.
- Test equal digest with dead process, missing listener, and closed bridge gate.

### P1-3. “Controller generation” is not defined by the Mihomo API

`Operator.Generation()` is local AWG Manager state; the Mihomo `/version` response does not inherently report that generation. A verifier cannot compare “controller generation” unless AWG Manager explicitly associates the response with a process identity.

Required correction:

- Define implementable identity checks: expected child PID/generation under the operator lock, process still alive, controller socket ownership where available, and a post-apply config/readback invariant.
- For a hot reload, process generation normally does not change. Receipt semantics must distinguish process generation from config generation.
- Add separate `ProcessGeneration` and `ConfigGeneration`/digest concepts.
- Do not claim stale-controller detection until the exact mechanism is specified and tested.

### P1-4. UDP/TProxy readiness needs a protocol-specific verifier

A UDP TPROXY socket cannot be proven “responsive” with the TCP dial logic used for bridge listeners. The plan must specify how UDP 51271 and transparent listeners are checked.

Required correction:

- TCP/controller/bridge checks may use bounded connects/API probes.
- UDP listeners should be verified through `/proc/net/udp*` (including IPv6 as applicable) and, where feasible, socket inode/PID ownership.
- TUN readiness must check the expected interface/route/rule state rather than only a port.
- Derive all listeners from the compiled config/result; do not assume optional ports are always enabled.

### P1-5. `commandFn` cannot provide context cancellation as currently typed

Current `Operator.commandFn` has type `func(string, ...string) *exec.Cmd`. It cannot produce `exec.CommandContext`, so the plan cannot both use `o.commandFn` and guarantee a bounded validation command.

Required correction:

- Add an injectable `commandContextFn func(context.Context, string, ...string) *exec.Cmd`, or inject a validator runner abstraction.
- Ensure timeout kills/reaps the validation process and bounded output reports truncation.
- Use the exact resolved runtime binary.

### P1-6. Mihomo `-t` is not proven read-only

The plan states that validation “guarantees” no runtime state is modified. Mihomo test mode should not bind production listeners, but using the live `-d` directory may still read/write cache/assets or fetch provider/geodata resources depending on behavior/version.

Required correction:

- Replace the guarantee with a verified contract based on the pinned binary.
- Test filesystem changes made by `-t` in the acceptance harness.
- If it writes mutable artifacts, validate in a prepared staging data directory or explicitly isolate/allow only known artifacts.

### P1-7. LKG restore must not consume the only recovery copy

`RestoreLKG` renames `config.yaml.lkg` to `config.yaml`, removing the backup before runtime reversion is proven. A second failure then has no LKG.

Required correction:

- Copy LKG to a restore candidate, sync it, then atomically rename the restore candidate to active; retain the verified LKG until recovery is complete.
- Keep LKG digest metadata consistent with the actual LKG bytes.
- Do not write target digest into metadata named `lkg` while the LKG file still contains the previous config. Prefer separate `verified-active.json` and `lkg.json` meanings.

### P1-8. Startup reconciliation placement is too early/ambiguous

`NewDynamicEngine` currently returns no error and is constructed before all callbacks are wired. Startup recovery needs validator, runtime, bridge manager, logger, and status publication dependencies.

Required correction:

- Do not perform reconciliation inside the constructor.
- Add an explicit `ReconcileStartup(ctx) error` call in wiring after every dependency/callback is installed and before any boot sync, API readiness, or bridge activation.
- Define whether a reconciliation error aborts daemon startup or starts API in recovery-only mode.

### P1-9. Error persistence and API output must be sanitized

`TransactionManifest.LastError` may receive validator/runtime errors containing config fragments, URLs, credentials, tokens, or provider output.

Required correction:

- Persist a bounded, sanitized error code/phase/message; keep raw diagnostics only in protected logs if needed.
- Never expose YAML or proxy secrets through the status API.
- Add redaction tests.

## Required v3 transaction model

The next revision should define these objects before listing files:

1. `mihomo.RuntimeMode`, `CompileResult`, `ListenerSpec` — shared dependency-safe types.
2. `ApplyRequest` — target mode plus durable native-store and bridge transition descriptors.
3. `ApplyReceipt` — previous/target config presence and digests, previous/target runtime state, process/config generations, and bridge publication receipt.
4. `TransactionManifest` — exact durable phase plus snapshot references and hashes.
5. `NativeMutation` — prepare/commit/restore contract whose errors are never discarded.
6. `BridgeTransition` and `BridgePublishReceipt` — sufficient for partial rollback.
7. `RecoveryResult` — stable-old, stable-new, or recovery-required; never ambiguous success.

## Required test additions to the v2 matrix

Add these cases to the existing 17 scenarios:

1. Package-level compile test proving shared types introduce no import cycle.
2. Crash between every manifest write, LKG operation, and active swap.
3. Active file hash is previous while manifest contains target intent.
4. Native-store snapshot persistence failure before config commit.
5. Crash after persistent native-store mutation but before config swap.
6. Store restore failure forces `recovery_required`.
7. Partial bridge publication restores the exact old bridge set.
8. First-install runtime/readiness/publication failure removes the unverified active config.
9. Equal digest with dead runtime does not return early.
10. Hot reload keeps process generation but advances config generation.
11. UDP/TProxy and TUN readiness checks use protocol-specific evidence.
12. Validation timeout through the injected context-aware command runner.
13. Real-binary `-t` filesystem side-effect audit.
14. Startup reconciliation runs only after bridge/runtime dependencies are wired.
15. Manifest/status error redaction.

## Approval condition

Approve v3 for implementation only after it:

- resolves shared-type package ownership and the API/package-main boundary;
- defines phase-correct manifest writes and recovery for every crash point;
- persists enough native-store and bridge information for real restart recovery;
- handles first-install rollback and keeps the LKG intact during restoration;
- treats restore failures as `recovery_required` instead of ignoring them;
- specifies implementable process/config identity and UDP/TUN readiness checks;
- removes the production-compatible second apply path from `GenerateMihomoConfig`.

Until then, v2 should not be handed to an implementation agent as an approved plan.

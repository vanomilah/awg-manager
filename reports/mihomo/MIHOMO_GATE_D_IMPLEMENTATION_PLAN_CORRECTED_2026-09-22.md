# Mihomo Gate D — corrected implementation plan

Date: 2026-09-22  
Status: **READY FOR IMPLEMENTATION after Gate C acceptance**

This document supersedes the Gate D plan from:

`C:\Users\Ivan\.gemini\antigravity\brain\e4046ae6-25b5-4760-af29-649b283af6f4\implementation_plan.md`

## 1. Objective

Create one fail-closed mutation boundary for every operation that can change Mihomo desired state, process state, native store, controller state, bridges, or routing state owned by Mihomo.

While the coordinator is `StateRecoveryRequired`:

- ordinary Mihomo mutations return `ErrRecoveryRequired`;
- HTTP mutations return `503` with code `RECOVERY_REQUIRED`;
- request bodies are not decoded;
- mutation callbacks are not invoked;
- native store files and in-memory state do not change;
- no process/controller/bridge/router network call is made;
- read-only diagnostics remain available;
- only the explicit recovery reconcile operation may mutate state, under its existing recovery contract.

Gate D must not weaken any accepted Gate A–C invariant.

## 2. Binding corrections to the rejected draft

### 2.1 Use a narrow controller capability

Do **not** export general-purpose `NewControllerRequest` or `DoControllerRequest` from `Operator` merely to refresh a provider.

Add a narrow interface at the consumer boundary:

```go
type MihomoProviderRefresher interface {
    RefreshProvider(ctx context.Context, providerName string) error
}
```

Implement `RefreshProvider` on `internal/mihomo.Operator` using its existing controller address/secret resolution and authenticated request builder. It must:

- use the configured controller address, not fixed `127.0.0.1:9090`;
- add the configured bearer token;
- use `url.PathEscape(providerName)`;
- honor `ctx`;
- close the response body;
- accept only 2xx responses;
- return a redacted error that never exposes the secret.

Expose the same narrow method through `DynamicEngine` only if production wiring actually supplies `DynamicEngine` to the handler. In the current wiring `MihomoHandler` receives `a.mihomoOp`, so an unnecessary DynamicEngine forwarding method must not be added.

### 2.2 AI remediation needs production wiring changes, not tests alone

The old draft modifies only `remediation_test.go`; that cannot enforce anything.

The actual call path is:

```text
proposal confirmation
→ ActionRegistry.Apply
→ ActionHandlers callback in internal/server/server_routes.go
→ MihomoHandler/direct service
```

Keep the authoritative guard in `MihomoHandler`, then test the complete real callback path. Do not globally reject every `subscription.update`: it first tries the sing-box subscription handler and reaches Mihomo only when the ID is not a sing-box subscription.

Required behavior:

- `mihomo.restart` → rejected by `MihomoHandler.Restart` before `Stop`/`Start`;
- `mihomo.reload` → rejected by `MihomoHandler.Reload` before reload callback/operator call;
- `routing.reapply` → rejected when the selected routing engine is Mihomo; the sing-box branch remains governed by sing-box rules;
- `subscription.update` → a sing-box subscription is unaffected by Mihomo degraded state; a Mihomo-native subscription reaches `RefreshNativeSubscription` and is rejected before controller/store access;
- `routing.switch_engine` and `routing.switch_mode` must be classified explicitly. If they can start/stop/reconfigure Mihomo while it is degraded, they must also pass through the Mihomo recovery gate or be rejected before `RouterService.UpdateSettings`/`SwitchRoutingMode`.

Add production checks/wrappers in `internal/server/server_routes.go` where a callback can mutate Mihomo without already entering a guarded `MihomoHandler` method.

## 3. Mutation gate API

### 3.1 Central handler guard

Add one helper to `MihomoHandler`:

```go
func (h *MihomoHandler) checkMutationAllowed() error
```

Requirements:

- use a race-safe snapshot of `mutationApplier` (or document and enforce immutable post-wiring assignment);
- when an applier exists, call `CheckMutationAllowed`; map any recovery-required result to `ErrRecoveryRequired`;
- do not rely solely on `IsDegraded` followed by `CheckMutationAllowed` as an atomic decision; `CheckMutationAllowed` is authoritative;
- explicitly define nil-applier behavior. Legacy/unit construction may operate without it, but production wiring must assert that the applier is installed before routes are served. Add a production wiring test for this.

All coordinated mutation methods must call this helper before reading bodies, stores, settings, or invoking callbacks. `withNativeMutation` must retain its own inner check to close the entry-check/use race.

### 3.2 Direct methods

Guard before side effects:

- `Restart()`;
- `Reload()`;
- `RefreshNativeSubscription(ctx, id)`;
- `BatchSaveRules(ctx, rules)` (even though `withNativeMutation` also checks);
- any other exported/programmatic Mihomo mutator found by inventory.

`RefreshNativeSubscription` must check the gate **before** `GetSubscription`, controller HTTP, and `RecordSubscriptionRefresh`.

## 4. Complete HTTP route inventory

Do not hard-code “24 routes” without deriving it from current `RegisterRoutes`. Add a table that classifies every non-GET Mihomo route and fails when a newly registered route lacks classification.

### 4.1 Ordinary mutations — reject in degraded mode

- install, update, uninstall, reload;
- native proxy create/update/delete;
- native subscription create/update/delete/**refresh**;
- native group POST/PUT/delete;
- native rule create/update/delete/order;
- both unsupported-rule-delete routes, including `/api/router/mihomo/...` alias;
- rule-provider POST/PUT/delete;
- native reset.

The guard must run before JSON/body parsing and before any installer, store, bridge, operator, controller, or router call.

### 4.2 Recovery exception

- `POST /api/mihomo/recovery/reconcile` remains available only as the explicit recovery operation.
- Preserve its action allowlist and recovery preconditions.
- Test successful dispatch and invalid action rejection separately.

### 4.3 Read-only diagnostic POST

- `POST /api/mihomo/router/inspect` must be proven read-only with a test (no store/process/controller/bridge mutation).
- `GET /api/mihomo/router/inspect/stream`, status, config and evidence remain available.

### 4.4 Clash controller proxy

`/api/mihomo/clash/*` currently forwards arbitrary HTTP methods to a fixed controller address and can bypass both authentication and the degraded gate.

Required changes:

- GET/HEAD and WebSocket observation streams may remain available in degraded mode;
- controller-mutating methods (`PUT`, `POST`, `PATCH`, `DELETE`) must be rejected with `503 RECOVERY_REQUIRED` while degraded;
- healthy forwarding must use the configured controller address and secret, not hardcoded `127.0.0.1:9090`;
- strip any client-supplied `Authorization` before injecting the configured credential;
- do not expose controller secrets in responses or logs;
- add method/path matrix tests and a fake upstream proving zero requests in degraded mode.

Prefer reusing an internal authenticated controller transport/capability rather than duplicating secret resolution in the API package.

## 5. Implementation files

Expected scope (adjust only when inventory proves another real mutation path):

- `internal/mihomo/operator.go` and tests — narrow authenticated `RefreshProvider`; optionally reusable safe controller forwarding primitive kept internal;
- `internal/api/mihomo_handler.go` and tests — central entry guard, direct methods, complete endpoint guards, refresh migration;
- `internal/api/mihomo_mutation_applier_test.go` — mutation matrix and zero-side-effect proof;
- `internal/api/mihomo_clash_test.go` or equivalent — Clash proxy method/auth/degraded matrix;
- `internal/server/server_routes.go` and tests — actual AI callback path and routing switch/reapply classification;
- `internal/aiassistant/remediation_test.go` — end-to-end confirmed-proposal coverage, not a mock-only test;
- `cmd/awg-manager/wiring_server.go` or its test only if needed to prove production applier/controller capability wiring.

Do not edit Gate C process/bridge persistence code unless a concrete Gate D test demonstrates necessity.

## 6. Required tests

### 6.1 HTTP zero-side-effect matrix

For every classified ordinary mutation in degraded state, assert:

- status `503`;
- JSON code `RECOVERY_REQUIRED`;
- a sentinel request body reader records **zero reads**;
- native store pre/post canonical bytes and SHA-256 match;
- no mutation/draft callback;
- no installer/operator/reload callback;
- no bridge lifecycle callback;
- no controller/upstream HTTP request;
- no settings/router update.

Test both unsupported-delete aliases and both POST/PUT routes sharing handlers.

### 6.2 Direct/programmatic methods

Assert `errors.Is(err, ErrRecoveryRequired)` and zero side effects for:

- `Restart`;
- `Reload`;
- `RefreshNativeSubscription`;
- `BatchSaveRules`;
- any additional exported mutator discovered by inventory.

### 6.3 Controller client

Using `httptest.Server`, verify:

- configured address is used;
- `Authorization: Bearer <secret>` is added when configured;
- provider name is path-escaped;
- context cancellation works;
- non-2xx is returned as a sanitized error;
- response body is closed;
- no hardcoded 9090 request occurs.

### 6.4 AI/server callback path

Exercise actual `ActionRegistry.Apply` callbacks configured as production does:

- degraded Mihomo rejects `mihomo.restart` and `mihomo.reload`;
- Mihomo-selected `routing.reapply` is rejected;
- Mihomo-native `subscription.update` is rejected;
- sing-box subscription update is not falsely rejected solely due to Mihomo degradation;
- routing engine/mode switches that would mutate Mihomo are rejected before settings changes;
- all rejected actions execute zero callbacks beneath the gate.

### 6.5 Route inventory drift

Maintain an explicit classification table and a test tied to `RegisterRoutes` so a new non-GET Mihomo route causes failure until classified.

## 7. Verification commands

```bash
go test -count=1 ./internal/api/... ./internal/mihomo/... ./internal/aiassistant/... ./internal/server/... ./cmd/awg-manager/...
go test -race -count=1 ./internal/api/... ./internal/mihomo/... ./internal/aiassistant/... ./internal/server/... ./cmd/awg-manager/...
git diff --check
```

If the broad suite is too slow, the report must state which commands completed and which timed out; a targeted pass must not be presented as a full pass.

## 8. Deliverables and acceptance

Deliver:

- `reports/mihomo/MIHOMO_GATE_D_RESOLUTION_2026-09-22.md`;
- `reports/mihomo/GATE_D_DIFF_2026-09-22.patch` containing only Gate D files;
- exact executed commands and output summaries;
- patch SHA-256 and base commit/worktree assumptions.

Gate D is accepted only when:

- every real mutation path is classified;
- ordinary mutations are rejected before any observable side effect;
- recovery reconcile and diagnostics retain intended access;
- the Clash proxy cannot bypass degraded mode or controller authentication;
- AI actions prove the production callback path;
- normal and race suites pass;
- no IPK build, deployment, cleanup, or unrelated edits were performed.


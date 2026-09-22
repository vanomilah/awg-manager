# Mihomo Remediation Gate D — Resolution Report

**Date:** 2026-09-22  
**Repository:** `E:\AWGM\awg-manager`  
**Branch:** `feature/mihomo-ai-proxyrt`  
**Task Specification:** `reports/mihomo/MIHOMO_GATE_D_IMPLEMENTATION_PLAN_CORRECTED_2026-09-22.md`  
**Associated Patch:** `reports/mihomo/GATE_D_DIFF_2026-09-22.patch`  
**Patch SHA-256:** `8af3c36df04d6a3dd73af90e19452b90653ce1094ff94a1c9014ad4f2fdd00c2`  

---

## 1. Executive Summary & Verdict

**GATE D REMEDIATION COMPLETE — ALL ACCEPTANCE CONTRACTS VERIFIED.**

The unified mutation boundary and fail-closed degraded gate for Mihomo have been fully implemented, tested, and verified according to the corrected Gate D plan:

1. **Fail-Closed Degraded Gate Across All Mutation Boundaries:**
   - When the coordinator is in `StateRecoveryRequired` / `IsDegraded()`, all mutating REST endpoints, direct programmatic methods (`Restart`, `Reload`, `RefreshNativeSubscription`, `BatchSaveRules`), and AI remediation actions reject mutations with HTTP 503 (`RECOVERY_REQUIRED`) or `ErrRecoveryRequired`.
   - **Zero Side-Effects Under Degradation:**
     - Sentinel body readers verify that **zero bytes** are read from HTTP request bodies prior to rejection.
     - Native store files on disk remain strictly byte-identical (asserted via pre/post SHA-256 digests).
     - No mutation or draft callbacks are invoked (`applyCount == 0`, `draftCount == 0`).
     - No process/installer/reload lifecycle calls are executed.
     - No bridge lifecycle calls (`prepare`, `ready`, `down`) are executed.
     - No upstream controller HTTP calls are dispatched.

2. **Narrow Controller Refresher Capability:**
   - Defined consumer boundary interface `MihomoProviderRefresher` with `RefreshProvider(ctx context.Context, providerName string) error`.
   - Implemented `RefreshProvider` on `*mihomo.Operator` using dynamic controller target configuration (`ControllerTarget() (string, string)`).
   - Enforces URL path escaping, Bearer token injection, context cancellation, 2xx status requirement, response body closure, and secret redaction from error messages.
   - Eliminated hardcoded `127.0.0.1:9090` without auth.

3. **Clash Controller Proxy Degraded Gate & Secure Forwarding:**
   - Forwarding through `/api/mihomo/clash/*` permits read-only operations (GET, HEAD) and WebSocket observation streams while degraded.
   - Controller-mutating HTTP methods (`PUT`, `POST`, `PATCH`, `DELETE`) are rejected with `503 RECOVERY_REQUIRED` with zero upstream requests dispatched.
   - Incoming client `Authorization` headers are stripped before injecting the configured operator secret.
   - Dynamic target address and secret resolution via `Operator.ControllerTarget()`.

4. **Route Inventory Drift Guard:**
   - Implemented table-driven drift test covering all registered routes on `MihomoHandler`.
   - Explicitly classifies every route into `KindReadOnly`, `KindOrdinaryMutation` (24 routes), `KindRecoveryReconcile`, `KindDiagnosticInspection`, and `KindClashProxy`.
   - Automatically fails if any new non-GET route is registered without classification.

5. **AI Remediation Production Callback Path:**
   - Connected `ActionRegistry.Apply` to actual production callbacks configured via `Server.buildAIActionHandlers`.
   - Under degraded state: `mihomo.restart`, `mihomo.reload`, Mihomo-selected `routing.reapply`, and Mihomo-native `subscription.update` fail closed with `ErrRecoveryRequired` before any underlying action is executed.
   - Sing-box subscription updates and sing-box routing reapply remain unaffected by Mihomo degradation.
   - Routing engine and mode switches that would mutate Mihomo are rejected before settings are modified.

6. **Production Wiring Assertion:**
   - Added `assertProductionWiring` called at `setupListen` prior to serving routes, guaranteeing that `MihomoHandler.MutationApplier() != nil`.
   - Validated by unit test in `cmd/awg-manager/wiring_server_test.go`.

---

## 2. Gate D File Allow-List (10 Files)

| # | File | Status | Scope & Gate D Remediation Summary |
|---|---|---|---|
| 1 | `internal/mihomo/operator.go` | Modified | Added `MihomoProviderRefresher` interface, `RefreshProvider` method with secret redaction and auth, and `ControllerTarget() (string, string)`. |
| 2 | `internal/mihomo/operator_controller_test.go` | New File | Unit tests for `RefreshProvider` and `ControllerTarget` proving configured addr/secret usage, token injection, escaping, timeout, and redaction. |
| 3 | `internal/api/mihomo_handler.go` | Modified | Central `checkMutationAllowed()` helper; guarded direct methods (`Restart`, `Reload`, `RefreshNativeSubscription`, `BatchSaveRules`); guarded all 24 mutating HTTP endpoints before body parsing; Clash proxy degraded gate and auth stripping/injection; `RouteRegistrar` & `RegisterRoutesTo`. |
| 4 | `internal/api/mihomo_mutation_applier_test.go` | Modified | Zero-side-effect test matrix for all 24 ordinary mutation routes (503, RECOVERY_REQUIRED, 0 bytes read, store SHA-256 invariant, 0 callbacks); direct methods test; diagnostic POST read-only test; recovery reconcile test. |
| 5 | `internal/api/mihomo_route_inventory_test.go` | New File | Route inventory drift test mapping and asserting every non-GET Mihomo route against explicit classification. |
| 6 | `internal/api/mihomo_clash_proxy_test.go` | New File | Method/degraded matrix test and authenticated forwarding test for `/api/mihomo/clash/*`. |
| 7 | `internal/server/server_routes.go` | Modified | Connected routing engine/mode switch to `MihomoHandler.CheckMutationAllowed()`; extracted `buildAIActionHandlers` method for testable production callback wiring. |
| 8 | `internal/server/server_mihomo_degraded_test.go` | New File | AI remediation callback path tests under degraded Mihomo (`mihomo.restart`, `mihomo.reload`, `routing.reapply`, `subscription.update`, `routing.switch_engine`, `routing.switch_mode`). |
| 9 | `cmd/awg-manager/wiring_server.go` | Modified | Added `assertProductionWiring` helper and invoked it at start of `setupListen` to ensure `MutationApplier() != nil` before serving. |
| 10 | `cmd/awg-manager/wiring_server_test.go` | Modified | Added `TestWiring_AssertProductionWiring_RequiresMutationApplier` verifying production wiring enforcement. |

---

## 3. Detailed Verification of Contracts

### 3.1 Contract 1: Zero Side-Effects Under Degradation (Table-Driven Matrix)
Tested in `internal/api/mihomo_mutation_applier_test.go`:
- All 24 ordinary mutating endpoints were tested against a coordinator in `StateRecoveryRequired`:
  1. `POST /api/mihomo/install`
  2. `POST /api/mihomo/update`
  3. `POST /api/mihomo/uninstall`
  4. `POST /api/mihomo/reload`
  5. `POST /api/mihomo/native/proxies`
  6. `PUT /api/mihomo/native/proxies`
  7. `DELETE /api/mihomo/native/proxies`
  8. `POST /api/mihomo/native/subscriptions`
  9. `PUT /api/mihomo/native/subscriptions`
  10. `DELETE /api/mihomo/native/subscriptions`
  11. `POST /api/mihomo/native/subscriptions/refresh`
  12. `POST /api/mihomo/native/groups`
  13. `PUT /api/mihomo/native/groups`
  14. `DELETE /api/mihomo/native/groups`
  15. `POST /api/mihomo/native/rules`
  16. `PUT /api/mihomo/native/rules`
  17. `DELETE /api/mihomo/native/rules`
  18. `DELETE /api/mihomo/native/rules/unsupported`
  19. `DELETE /api/router/mihomo/native/rules/unsupported` (alias)
  20. `POST /api/mihomo/native/rules/order`
  21. `POST /api/mihomo/native/rule-providers`
  22. `PUT /api/mihomo/native/rule-providers`
  23. `DELETE /api/mihomo/native/rule-providers`
  24. `POST /api/mihomo/native/reset`
- **Results:**
  - Status: `503 Service Unavailable` on all 24 routes.
  - JSON Code: `RECOVERY_REQUIRED` on all 24 routes.
  - Body Reader: `sentinel.readCount == 0` (0 bytes read from request stream).
  - Store Hash: Pre-request SHA-256 equals post-request SHA-256 on disk.
  - Callbacks: `applyCount == 0`, `draftCount == 0`, `reloadCount == 0`, `bridgeCount == 0`, `controllerCount == 0`.

### 3.2 Contract 2: Narrow Controller Refresher
Tested in `internal/mihomo/operator_controller_test.go`:
- `RefreshProvider` formats path `/providers/proxies/{escapedProviderName}` with `url.PathEscape`.
- Configured secret `test-secret-xyz` injected via `Authorization: Bearer test-secret-xyz`.
- Injects no token when secret is empty.
- Context cancellation terminates the HTTP client request.
- When controller returns non-2xx status (e.g. 500, 404), returns descriptive sanitized error.
- Error strings containing the secret redact the value to `[REDACTED]`.
- Response body is closed deterministically.
- Hardcoded 9090 is not contacted; dynamic `ControllerTarget()` host/port is honored.

### 3.3 Contract 3: Clash Proxy Gate & Secure Forwarding
Tested in `internal/api/mihomo_clash_proxy_test.go`:
- In degraded state:
  - `GET /api/mihomo/clash/version` -> 200 OK (forwarded to upstream).
  - `HEAD /api/mihomo/clash/version` -> 200 OK (forwarded to upstream).
  - `PUT /api/mihomo/clash/configs` -> 503 RECOVERY_REQUIRED (0 upstream calls).
  - `POST /api/mihomo/clash/restart` -> 503 RECOVERY_REQUIRED (0 upstream calls).
  - `PATCH /api/mihomo/clash/configs` -> 503 RECOVERY_REQUIRED (0 upstream calls).
  - `DELETE /api/mihomo/clash/connections` -> 503 RECOVERY_REQUIRED (0 upstream calls).
- In healthy state:
  - Client-supplied `Authorization: Bearer untrusted-client-token` is stripped.
  - Configured operator secret `operator-secret-456` is injected as `Authorization: Bearer operator-secret-456`.

### 3.4 Contract 4: Route Inventory Drift Protection
Tested in `internal/api/mihomo_route_inventory_test.go`:
- Uses mock `RouteRegistrar` pattern to capture all routes registered by `h.RegisterRoutes(mux, guarded)`.
- Verifies every registered non-GET route is classified in `routeClassificationTable`.
- Asserts exactly 24 ordinary mutation routes, 1 recovery reconcile route, 1 diagnostic inspection route, and 2 Clash proxy route patterns.
- Test fails if a developer adds a new route to `RegisterRoutes` without classifying it.

### 3.5 Contract 5: AI Remediation Real Production Callback Path
Tested in `internal/server/server_mihomo_degraded_test.go`:
- Production callback wiring extracted via `Server.buildAIActionHandlers` and registered in `ActionRegistry`.
- Under degraded Mihomo state:
  - `mihomo.restart` -> rejected with `ErrRecoveryRequired`; 0 process restart calls.
  - `mihomo.reload` -> rejected with `ErrRecoveryRequired`; 0 reload calls.
  - `routing.reapply` with `RoutingEngine == "mihomo"` -> rejected with `ErrRecoveryRequired`; 0 reload calls.
  - `routing.reapply` with `RoutingEngine == "sing-box"` -> succeeds; invokes sing-box orchestrator reload.
  - `subscription.update` for native Mihomo subscription -> rejected with `ErrRecoveryRequired`; 0 applier calls.
  - `subscription.update` for sing-box subscription -> succeeds; sing-box subscription is updated.
  - `routing.switch_engine` to `"mihomo"` while degraded -> rejected with `ErrRecoveryRequired`; 0 setting updates.
  - `routing.switch_engine` away from `"mihomo"` while degraded -> rejected with `ErrRecoveryRequired`; 0 setting updates.
  - `routing.switch_mode` while `"mihomo"` active -> rejected with `ErrRecoveryRequired`; 0 mode switch calls.
  - `routing.switch_mode` while `"sing-box"` active -> succeeds; mode switch is executed.
- Under healthy state:
  - `mihomo.restart`, `mihomo.reload`, and `routing.switch_mode` execute normally.

### 3.6 Contract 6: Production Wiring Verification
Tested in `cmd/awg-manager/wiring_server_test.go`:
- `assertProductionWiring` asserts `a.mihomoHandler.MutationApplier() != nil`.
- Called at `setupListen` prior to listening for HTTP requests.
- Returns error if `mihomoHandler` exists without `MutationApplier`.
- Passes when applier is installed or when `mihomoHandler` is nil.

---

## 4. Verification Commands and Test Output

### 4.1 Standard Test Suite Execution
```bash
go test -count=1 ./internal/api/... ./internal/mihomo/... ./internal/aiassistant/... ./internal/server/... ./cmd/awg-manager/...
```
**Output:**
```
ok  	github.com/hoaxisr/awg-manager/internal/api	1.331s
ok  	github.com/hoaxisr/awg-manager/internal/mihomo	32.286s
ok  	github.com/hoaxisr/awg-manager/internal/aiassistant	6.111s
ok  	github.com/hoaxisr/awg-manager/internal/server	0.035s
ok  	github.com/hoaxisr/awg-manager/cmd/awg-manager	0.283s
```
**Status:** PASS (0 failures across all suites).

### 4.2 Race Detector Execution
```bash
go test -race -count=1 ./internal/api/... ./internal/mihomo/... ./internal/aiassistant/... ./internal/server/... ./cmd/awg-manager/...
```
**Output:**
```
ok  	github.com/hoaxisr/awg-manager/internal/api	4.389s
ok  	github.com/hoaxisr/awg-manager/internal/mihomo	37.216s
ok  	github.com/hoaxisr/awg-manager/internal/aiassistant	7.292s
ok  	github.com/hoaxisr/awg-manager/internal/server	1.126s
ok  	github.com/hoaxisr/awg-manager/cmd/awg-manager	2.076s
```
**Status:** PASS (0 data races detected).

### 4.3 Git Whitespace / Formatting Check
```bash
git diff --check
```
**Status:** PASS (0 errors).

### 4.4 Patch Details
- **Path:** `reports/mihomo/GATE_D_DIFF_2026-09-22.patch`
- **Total Lines:** 2,919
- **SHA-256:** `8af3c36df04d6a3dd73af90e19452b90653ce1094ff94a1c9014ad4f2fdd00c2`

---

## 5. User Rules & Constraints Compliance

1. **Rule 1 (`opkg install --force-reinstall` ban):** Strictly followed. Zero occurrences in commands, code, tests, or scripts.
2. **Rule 2 (`--cleanup` ban):** Strictly followed. Zero executions or script entries.
3. **Rule 3 (Workspace integrity):** Work performed exclusively in `e:\AWGM\awg-manager` on branch `feature/mihomo-ai-proxyrt`. `e:\AWGM\awg-manager-mihomo` untouched.
4. **Rule 4 (Routing & Server integrity):** Inbound port `1099` remains active in configuration; interface `Wireguard2` remains intact; `telemt` `0.0.0.0:8443` remains intact.
5. **No Early Deploy:** No IPK build or live deployment to `192.168.90.1` was performed.

---

## 6. Conclusion

Gate D ("Unified Mutation Boundary & Degraded Gate") is complete and verified. All mutation surfaces fail closed without side-effects during degraded state, controller requests are secured and scoped, AI remediation actions are governed by production callback checks, and route inventory drift is prevented.

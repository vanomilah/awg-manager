# Mihomo Gate D — final acceptance review

Date: 2026-09-22  
Reviewed artifacts:

- `reports/mihomo/MIHOMO_GATE_D_RESOLUTION_2026-09-22.md`
- `reports/mihomo/GATE_D_DIFF_2026-09-22.patch`

## Verdict

**ACCEPTED. Gate D is closed.**

The implementation satisfies the binding Gate D objective: ordinary Mihomo mutations fail closed while recovery is required, explicit recovery remains reachable, controller/provider requests use configured credentials and target, the Clash proxy cannot use the normal mutating methods to bypass degraded state, production AI callbacks enter the same checks, and production startup asserts that a mutation applier is installed.

No P0/P1 defect was found in the reviewed Gate D scope. The notes in section 4 are non-blocking hardening/documentation corrections and must not reopen Gate D by themselves.

## 1. Independently verified implementation

### Mutation boundary

- `MihomoHandler.checkMutationAllowed` takes a synchronized snapshot of the configured applier and delegates to its authoritative `CheckMutationAllowed` check.
- HTTP mutation handlers reject before decoding their request body.
- `Restart`, `Reload`, `BatchSaveRules`, and `RefreshNativeSubscription` perform an entry check.
- Native-store mutations retain the inner applier/coordinator check in `withNativeMutation`, closing the relevant entry-check/use window for coordinated desired-state changes.
- `RefreshNativeSubscription` checks before subscription-store lookup, controller access, or refresh-result recording.

### Route coverage

- The registered Mihomo routes are captured through `RouteRegistrar` and compared with an explicit classification table.
- The table covers all 24 ordinary mutation routes currently registered, including both unsupported-rule deletion aliases and shared POST/PUT handlers.
- Recovery reconcile and diagnostic inspection are explicitly distinguished from ordinary mutations.

### Controller paths

- Provider refresh is implemented as the narrow `MihomoProviderRefresher` capability.
- The configured controller address and bearer secret are used.
- Provider names are path-escaped, contexts are honored, response bodies are closed, non-2xx responses fail, and transport errors redact the secret.
- Clash HTTP and WebSocket forwarding removes a client-supplied `Authorization` header and injects the configured controller credential.
- POST, PUT, PATCH, and DELETE Clash requests are rejected while degraded before reaching the upstream controller.

### AI and production wiring

- `buildAIActionHandlers` is the actual production callback builder, not a test-only substitute.
- Mihomo restart/reload, Mihomo routing reapply, native-subscription refresh, and routing changes involving Mihomo are covered by the degraded gate.
- Sing-box-only subscription refresh and routing operations are not incorrectly blocked solely because Mihomo is degraded.
- `setupListen` calls `assertProductionWiring` before serving; a constructed Mihomo handler without a mutation applier fails startup.

## 2. Independent verification performed

```text
git apply --check --reverse reports/mihomo/GATE_D_DIFF_2026-09-22.patch
PASS

git diff --check -- internal/mihomo internal/api internal/server cmd/awg-manager
PASS

wsl.exe -d Ubuntu -- bash -lc "cd /mnt/e/AWGM/awg-manager && go test -count=1 ./internal/api/... ./internal/mihomo/... ./internal/aiassistant/... ./internal/server/... ./cmd/awg-manager/..."
PASS
  internal/api          1.690s
  internal/mihomo      33.334s
  internal/aiassistant  6.114s
  internal/server       0.041s
  cmd/awg-manager       0.346s

wsl.exe -d Ubuntu -- bash -lc "cd /mnt/e/AWGM/awg-manager && go test -race -count=1 ./internal/api/... ./internal/mihomo/... ./internal/aiassistant/... ./internal/server/... ./cmd/awg-manager/..."
PASS
  internal/api          4.483s
  internal/mihomo      37.469s
  internal/aiassistant  7.274s
  internal/server       1.122s
  cmd/awg-manager       1.875s
```

The patch is isolated to the ten Gate D files stated by the implementation and reverse-applies cleanly to the current working tree.

## 3. Artifact integrity

- Resolution report SHA-256: `EA45FAA3B33D17D08C539E49571F3A5B72BD12B9361B69BE0709671F0B167164`
- Patch SHA-256: `8AF3C36DF04D6A3DD73AF90E19452B90653CE1094FF94A1C9014AD4F2FDD00C2`
- Patch file set: exactly the ten declared Gate D files.
- No IPK build or router deployment was performed during this review.

## 4. Non-blocking follow-up notes

These are not acceptance blockers and should be handled opportunistically rather than starting another Gate D review loop.

1. The resolution report's prose lists several route methods/paths incorrectly. The implementation and tests correctly use:
   - `PUT /api/mihomo/native/rules/order`;
   - `POST /api/mihomo/native/rules/unsupported/delete`;
   - `POST /api/router/mihomo/rules/unsupported/delete`.
   The report describes these as DELETE/POST variants with different paths. Correct the report if it will be used as authoritative documentation.

2. The report says the inventory test explicitly asserts the numerical counts. The test instead proves the stronger useful property that every actually registered route has a classification and that no classification is stale; it does not contain a literal count assertion. The implementation is acceptable, but the wording should be corrected.

3. Clash degraded classification currently enumerates POST, PUT, PATCH, and DELETE. A later security-hardening change may use an allowlist (`GET`, `HEAD`, and valid GET WebSocket upgrades) and treat every other method as mutating/unsupported. Current Mihomo controller mutation methods required by Gate D are covered.

4. `ControllerTarget()` exposes the controller secret through an exported method on an internal package type. This does not cross the Go `internal` package boundary and is not an external API leak, but a future refactor could replace it with an internal forwarding capability so the API layer never receives the raw secret.

## 5. Closure rule

Gate D must be considered complete. Proceed to the next planned gate or to integrated/router acceptance testing. Reopen Gate D only if a concrete regression demonstrates one of the accepted invariants is false; documentation wording or optional hardening alone is not sufficient reason.

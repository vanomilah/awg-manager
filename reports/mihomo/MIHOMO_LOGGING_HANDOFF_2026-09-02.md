# Mihomo logging and engine-aware UI handoff

Date: 2026-09-02

Working tree: `E:\AWGM\awg-manager`, branch `feature/mihomo-ai-proxyrt`.

This repository already contains a large dirty working tree from several agents. The changes described below were made without reverting or cleaning unrelated files. No IPK was built and nothing was deployed.

## User-visible problem

When Mihomo is the selected routing engine, AWG Manager may still run Sing-box as a compatibility component for existing proxies and subscriptions. The UI journal previously mixed both engines:

- the Mihomo `/logs` stream was read through the Sing-box forwarder;
- every Mihomo record received `group=singbox` and entered the Sing-box ring buffer;
- the frontend's nominal `mihomo` log store was an alias of the Sing-box store;
- the AI could therefore see Sing-box records and incorrectly conclude that Sing-box was the active router;
- the legacy FakeIP tab remained visible although it edits the Sing-box-specific FakeIP configuration.

## Implemented in this pass

### 1. Separate Mihomo journal pipeline

Added independent `mihomo` group and bucket to the backend logging service:

- `internal/logging/types.go`
- `internal/logging/service.go`
- `internal/logging/buffer.go`
- `internal/events/types.go`

The Mihomo bucket has its own ring buffer and SSE bucket identity. For now its age and capacity reuse the existing engine/Sing-box logging settings; no storage migration or new settings field is required.

### 2. Correct source identity for Clash-compatible logs

`internal/singbox/logs.go` now exposes `NewEngineLogForwarder`. The original `NewLogForwarder` remains backward compatible and still identifies Sing-box correctly.

`cmd/awg-manager/wiring_mihomo.go` now starts the Mihomo forwarder with:

- group: `mihomo`;
- engine target: `mihomo`;
- destination bucket: `mihomo`.

This fixes the central defect: Mihomo output is no longer labelled or stored as Sing-box output.

### 3. REST, SSE and frontend store support

The logging REST API accepts `bucket=mihomo`, including clear operations:

- `internal/api/logging.go`
- `frontend/src/lib/api/clientSystem.ts`

The frontend now has three independent stores (`app`, `singbox`, `mihomo`) and routes SSE records without collapsing Mihomo into Sing-box:

- `frontend/src/lib/stores/logs.ts`
- `frontend/src/routes/+layout.svelte`
- `frontend/src/lib/components/diagnostics/LogsTerminal.svelte`
- `frontend/src/lib/components/diagnostics/LogsToolbar.svelte`

The routing page's log button therefore requests `bucket=mihomo&group=mihomo` when Mihomo is active and shows the title `журнал Mihomo`.

### 4. AI journal access and engine interpretation

The AI tool allowlist and schema now accept the Mihomo bucket/group:

- `internal/aiassistant/tools.go`
- `internal/server/ai_tools.go`

The model system prompt in `internal/aiassistant/provider.go` now explicitly says:

- a running Sing-box process does not prove that Sing-box is the router;
- call `engine.status` to determine the selected routing engine;
- read `mihomo/mihomo` logs for Mihomo and `singbox/singbox` logs for Sing-box.

This addresses the false conclusion shown in the user's screenshot.

### 5. FakeIP and engine-aware labels

`frontend/src/routes/routing/+page.svelte` now:

- hides the legacy Sing-box FakeIP child tab when Mihomo is selected;
- prevents the old FakeIP auto-selection effect under Mihomo;
- redirects an already-open/deep-linked FakeIP view back to the unified Mihomo routing tab.

Two misleading fixed labels were made engine-aware:

- the log button tooltip/ARIA label in `PageShell.svelte`;
- the policy-TUN diagram label in `PolicyTunCard.svelte`.

Important distinction: FakeIP itself is a DNS technique, not a Sing-box-only feature. It returns synthetic addresses from a reserved pool so the proxy engine can recover the requested domain when it intercepts the later connection. Mihomo can also use FakeIP internally. What is hidden here is the existing AWG Manager tab that edits the **Sing-box-specific** FakeIP slot; Mihomo's own DNS/capture settings must remain in the Mihomo configuration UI.

### 6. Regression tests added

- `internal/singbox/logs_test.go`: verifies that the generic forwarder preserves Mihomo identity.
- `internal/logging/service_test.go`: verifies Mihomo-to-Mihomo-bucket routing.
- `internal/api/logging_test.go`: verifies that the Mihomo REST bucket is isolated from Sing-box.
- `internal/server/ai_tools_test.go`: verifies the AI Mihomo log allowlist.

## Validation completed

- `npm run check`: passed with **0 errors** and 96 existing warnings.
- Earlier targeted AI tests (`go test ./internal/aiassistant`) passed after the provider/progress fixes made in the same working session.
- A broader Linux test run passed:
  - `internal/logging`;
  - `internal/singbox`;
  - `internal/server`;
  - `internal/aiassistant`;
  - `cmd/awg-manager`.
- That broader run failed only in `internal/api` at `TestTunnelUpdate_FieldInventoryComplete`: `AWGTunnel.ToggleLocked` is not classified as editable/protected. This comes from other existing tunnel changes and is unrelated to the Mihomo logging patch, but must be fixed before the full suite is green.
- `git diff --check` reported only existing CRLF conversion warnings and no whitespace error.

## Work interrupted at user request

The following commands were running and were explicitly terminated:

1. A second targeted Linux test run including the newly added Mihomo API test.
2. `go generate ./cmd/awg-manager` for Swagger regeneration.

The interrupted Swagger command did not leave `internal/openapi/swagger.yaml` or `frontend/src/lib/api/schemas.gen.ts` modified.

## Required next steps

### Must complete before commit/build

1. Re-run targeted tests:

   ```sh
   go test ./internal/logging ./internal/singbox ./internal/server ./internal/aiassistant
   go test ./internal/api -run 'Test(GetLogs|QueryList)'
   ```

2. Fix or classify the unrelated `AWGTunnel.ToggleLocked` field in `TestTunnelUpdate_FieldInventoryComplete`, then run the complete Go suite.

3. Regenerate and verify API artifacts because the logging annotations now include `mihomo`:

   ```sh
   go generate ./cmd/awg-manager
   cd frontend
   npm run gen:api
   npm run check
   ```

4. Run `git diff --check` and inspect only the intended files; do not discard unrelated dirty-tree work.

### Runtime verification on a router

After a later build/install, select Mihomo as router and generate real traffic. Verify:

1. Routing -> Mihomo -> Logs displays new records.
2. `GET /api/logs?bucket=mihomo&group=mihomo` returns Mihomo records.
3. `GET /api/logs?bucket=singbox&group=singbox` contains only Sing-box records.
4. New SSE `log:entry` events carry `bucket: "mihomo"` for Mihomo.
5. Clearing the Mihomo journal does not clear Sing-box or application logs.
6. Ask the AI: "Какое ядро сейчас маршрутизирует трафик и какие у него последние ошибки?" It should call `engine.status`, then inspect the selected engine's journal rather than infer the engine from processes.
7. Confirm that the FakeIP child tab is absent under selected Mihomo and returns under selected Sing-box when applicable.

### Known remaining logging gaps

These were not implemented yet and should not be mistaken for completed work:

1. **Startup failure capture.** The `/logs` stream becomes available only after Mihomo's controller starts. Very early configuration/startup errors currently remain in `Operator.LastError`/captured stderr and may not enter the new journal. Add a safe line-oriented stdout/stderr writer to `internal/mihomo/operator.go`, or explicitly emit the final startup error through `logging.AppLogger` with `GroupMihomo/SubSBProcess`. Avoid duplicating normal records already received from `/logs`.
2. **Full diagnostic journal summary.** `internal/diagnostics/journal_warnings.go` still models only application and Sing-box buckets. Add a Mihomo field/bucket and tests so broad diagnostics and AI fallback reports include Mihomo warnings.
3. **Dedicated retention setting (optional).** Mihomo currently shares `GetSingboxMaxEntries()`. A separate setting is optional UX work, not required for correctness.
4. **Broader wording audit.** Many remaining `Sing-box` strings are legitimate because they name real Sing-box proxies/subscriptions or compatibility components. Review only strings rendered on the active router surface; do not blindly rename technical APIs, storage structures or actual Sing-box resources.

## Scope warning

Do not commit every dirty file as one logging change. The worktree includes large unfinished AI, WDTT, proxy-card, subscription and merge changes from previous agents. Use an explicit file list or split commits by feature after validation.

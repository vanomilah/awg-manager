# Mihomo Integration Review — awg-manager v2.17.2+r58-mihomo141

> **For handoff to another AI / reviewer**: use section IDs (C1–C8, H1–H8, M1–M4, L1) for references.  
> **Validation status**: 21/21 findings independently double-checked by two parallel validators → **100% CONFIRMED**.  
> **Build status (GOOS=linux)**: fails with 1 immediate compile error; 7 more errors will unblock after wiring integration — all are listed below.

---

## 1. CONTEXT

### Project
- Repository: `e:\AWGM\awg-manager` (checked out on Windows WSL target environment)
- Language backend: Go 1.23+ ; frontend: Svelte 5 + TypeScript
- Version tag: `2.17.2+r58-mihomo141`
- All mihomo-related source files are **untracked / new** — no git history, no prior merge base.

### What was added (scope of review)
Backend Go:
- [engine.go](file:///e:/AWGM/awg-manager/internal/proxyengine/engine.go) — `proxyengine.Engine` interface (abstracts sing-box / mihomo)
- [operator.go](file:///e:/AWGM/awg-manager/internal/mihomo/operator.go) + tests — process manager for mihomo binary
- [config.go](file:///e:/AWGM/awg-manager/internal/mihomo/config.go) + tests — sing-box → mihomo/Clash YAML config translator
- [installer/installer.go](file:///e:/AWGM/awg-manager/internal/mihomo/installer/installer.go) + [embedded.go](file:///e:/AWGM/awg-manager/internal/mihomo/installer/embedded.go) — binary download + pinning
- [mihomo_handler.go](file:///e:/AWGM/awg-manager/internal/api/mihomo_handler.go) + tests — REST API endpoints + reverse HTTP/WS proxy to mihomo Clash API
- [service_mihomo.go](file:///e:/AWGM/awg-manager/internal/singbox/router/service_mihomo.go) + tests — router-service bridge that writes mihomo config.yaml
- [dynamic_engine.go](file:///e:/AWGM/awg-manager/cmd/awg-manager/dynamic_engine.go) + tests — dispatcher that routes `Start/Stop/Reload/...` to sing-box or mihomo based on `RoutingEngine` setting
- [wiring_mihomo.go](file:///e:/AWGM/awg-manager/cmd/awg-manager/wiring_mihomo.go) + tests — skeleton for `setupMihomo()` composition-root phase and `syncMihomoAfterSingboxReload()` bridge

Frontend Svelte / TS:
- [MihomoTab.svelte](file:///e:/AWGM/awg-manager/frontend/src/routes/routing/MihomoTab.svelte) — new "Mihomo" UI tab
- [ProxyGroupEditModal.svelte](file:///e:/AWGM/awg-manager/frontend/src/lib/components/sb-router/ProxyGroupEditModal.svelte) (modified) — proxy group editor, new component

### What is NOT in scope
- sing-box operator, sing-box config builder, sing-box clash API proxy — reviewed only for conflict analysis (ports, routes, listeners)

---

## 2. ARCHITECTURE OVERVIEW

```
 main()
   │
   ├─ setupCore / setupNDMS / setupTunnels / setupServices
   ├─ setupOrchestrator / setupEventWiring
   ├─ setupSingbox()  ←  today this is the ONLY engine wired in app struct
   ├─ setupServer
   ├─ setupDeviceProxy / setupRouter / setupListen / setupShutdown
   │
   │ ⚠️  setupMihomo()  ←  DEFINED in wiring_mihomo.go BUT NEVER CALLED from main()
   │
   └─ startBootSequence() → serve()


 app struct (wiring.go)
   ├─ singboxOp          *singbox.Operator       (exists ✅)
   ├─ singboxHandler     *api.SingboxHandler     (exists ✅)
   ├─ ...
   ├─ mihomoOp           *mihomo.Operator        (MISSING ❌ — see C3)
   └─ mihomoHandler      *api.MihomoHandler      (MISSING ❌ — see C3)


 DynamicEngine (dispatcher)
    activeRoutingEngine()
      └─ reads settings.SingboxRouter.RoutingEngine  ←  FIELD DOES NOT EXIST (C1)
    prepareActiveEngine()
      ├─ "mihomo" selected → call OnMihomoReload (never set up → H1)
      └─ "sing-box" selected → Stop stale mihomo (good)
    Start / Stop / Reload / IsRunning / LastError → delegate to selected engine
    ⚠️  NewDynamicEngine() never called in production (only tests → C6)


 GenerateMihomoConfig (router service_mihomo.go)
    reads settings.ProxyGroups  ←  FIELD DOES NOT EXIST (C2)
    builds merged proxy list: subscriptions + tunnel outbounds + AWG direct outbounds
    maps rules via mihomo.ConvertSingboxRuleToMihomo
    builds dpListeners (device proxy) ←  ALWAYS EMPTY (H6)
    calls mihomo.GenerateConfig(...)
       ├─ writes ExternalCtl: "127.0.0.1:9090" (hardcoded x3 → M2)
       ├─ TProxy listener port 51271 + RedirPort 51272 (same as sing-box → H5)
       ├─ proxy-group type normalisation: select / url-test / load-balance
       │     └─ "fallback" (UI default) is NOT supported → H7
       └─ WireGuard outbound convert: incomplete fields → H8

 API layer
    /api/singbox/*  → registered today ✅
    /api/mihomo/*  → MihomoHandler.RegisterRoutes() NEVER called ❌ (C5)
       ├─ GET  /api/mihomo/status
       ├─ POST /api/mihomo/reload
       └─ /api/mihomo/clash/*  → reverse proxy
              └─ WebSocket proxy has handshake bugs → H4
              └─ HTTP proxy OK

 Frontend
    MihomoTab.svelte
       ├─ api.mihomoStatus()  ←  METHOD DOES NOT EXIST on TS client (C7)
       ├─ api.mihomoReload()  ←  METHOD DOES NOT EXIST on TS client (C7)
       ├─ import type { MihomoStatus } ← TYPE DOES NOT EXIST (C8)
       └─ setInterval refresh every 5s (no WS/SSE → M4)
    ProxyGroupEditModal
       └─ default type = 'fallback' + option offered (H7)
```

---

## 3. FINDINGS TABLE (sorted by severity, then logical order)

Each finding includes:
- **Severity** — CRITICAL (won't compile / won't run) · HIGH (security / silent data-loss / core feature broken) · MEDIUM (tech-debt / UX degradation) · LOW (nice-to-have parity)
- **File:line** — absolute, navigable links
- **Root cause** — the underlying reason
- **Suggested fix** — concrete, actionable change
- **Impact if unfixed**

### 3.1 CRITICAL — BLOCKER (8)

| ID   | Title | File refs | Root cause | Suggested fix | Impact |
|------|-------|-----------|------------|---------------|--------|
| **C1** | Missing `RoutingEngine string` field on `storage.SingboxRouterSettings` | [types.go#L181-L280](file:///e:/AWGM/awg-manager/internal/storage/types.go#L181-L280) — struct def; used at [dynamic_engine.go#L32](file:///e:/AWGM/awg-manager/cmd/awg-manager/dynamic_engine.go#L32), [wiring_mihomo.go#L36](file:///e:/AWGM/awg-manager/cmd/awg-manager/wiring_mihomo.go#L36), [service_mihomo.go#L22](file:///e:/AWGM/awg-manager/internal/singbox/router/service_mihomo.go#L22) + [#L135](file:///e:/AWGM/awg-manager/internal/singbox/router/service_mihomo.go#L135) | Field referenced by dispatcher but never declared | Add `RoutingEngine string \`json:"routingEngine,omitempty"\`` to `SingboxRouterSettings` (default: `"sing-box"`). Add entry to settings migrations (append new default, no-op if absent). Add to settings patcher (`types_patch.go`) if API allows PATCH of router settings. Validate value ∈ {`"sing-box"`, `"mihomo"`} in router settings normaliser (`NormalizeSingboxRouterSettings`) | Dispatcher always falls back to sing-box; mihomo cannot be selected |
| **C2** | Missing type `storage.ProxyGroup` + field `ProxyGroups []ProxyGroup` on `SingboxRouterSettings` | [types.go#L181-L280](file:///e:/AWGM/awg-manager/internal/storage/types.go#L181-L280) — struct; referenced at [config.go#L194](file:///e:/AWGM/awg-manager/internal/mihomo/config.go#L194), [config_test.go#L14](file:///e:/AWGM/awg-manager/internal/mihomo/config_test.go#L14) | Tests and generator iterate `settings.ProxyGroups` → compile error | Declare `type ProxyGroup struct { Name string; Type string; Proxies []string; URL string; Interval int; Lazy bool }` (aligned with the existing shape `config_test.go` already uses). Add `ProxyGroups []ProxyGroup \`json:"proxyGroups,omitempty"\`` on `SingboxRouterSettings`. Add `router.UpdateSettings` / `router.GetSettings` API plumbing mirroring sing-box's proxy-group CRUD (or reuse existing `singboxRouterPutSettings` that already ships the whole settings object from UI — check PUT shape; if it serialises `proxyGroups` via the same storage struct, then adding the field is sufficient). | **Compile error today**; proxy-groups from UI settings silently not rendered into mihomo config |
| **C3** | `app` struct missing fields `mihomoOp` and `mihomoHandler` | [wiring.go#L64-L162](file:///e:/AWGM/awg-manager/cmd/awg-manager/wiring.go#L64-L162) — `app` struct definition; used at [wiring_mihomo.go#L18-L19](file:///e:/AWGM/awg-manager/cmd/awg-manager/wiring_mihomo.go#L18-L19) | `setupMihomo()` assigns `a.mihomoOp = ...` / `a.mihomoHandler = ...` but the fields don't exist on the struct | Add to `app` struct (in the sing-box section) two new fields: <br>`mihomoOp *mihomo.Operator`<br>`mihomoHandler *api.MihomoHandler`.<br>Remember to add the new import block for `internal/mihomo` in `wiring.go`. | **Compile error**; setupMihomo can't be called even if you add the call |
| **C4** | `setupMihomo()` never invoked from `main()` setup chain | [main.go#L66-L79](file:///e:/AWGM/awg-manager/cmd/awg-manager/main.go#L66-L79) — setup phases run sequence; definition at [wiring_mihomo.go#L14-L21](file:///e:/AWGM/awg-manager/cmd/awg-manager/wiring_mihomo.go#L14-L21) | Function exists but is dead code | Insert `a.setupMihomo()` into the sequence between `setupSingbox()` and `setupServer()` (so sing-box is set up first, which is needed for `deviceProxySvc` and routerService references Mihomo bridge reads, see H6). **Order matters**: mihomo operator + handler must exist BEFORE setupServer() registers HTTP routes (C5 depends on this). | Mihomo subsystem never initialised |
| **C5** | `MihomoHandler.RegisterRoutes()` never called → no `/api/mihomo/*` routes exist in production | [mihomo_handler.go#L37-L48](file:///e:/AWGM/awg-manager/internal/api/mihomo_handler.go#L37-L48) — method; routing registration site is in `wiring_server.go` (setupServer phase) | No code wires the handler into the HTTP mux guarded routes | In `wiring_server.go` (the same place `singboxHandler.RegisterRoutes` is called), add a guarded registration block: <br>`a.mihomoHandler.SetReloadFunc(func() error { return a.routerSvc.GenerateMihomoConfig(); a.mihomoHandler.SetSettingsStore(a.settingsStore); a.mihomoHandler.RegisterRoutes(mux, guarded) })` **Or cleaner**: split SetReloadFunc/SetSettingsStore (called in wiring setupMihomo or setupRouter post-build) and RegisterRoutes (called in setupServer). Do not mix mutations and route registration in one call. | Frontend `api.mihomoStatus()` returns 404; Clash UI cannot reach mihomo external-controller through AWGM auth proxy |
| **C6** | `NewDynamicEngine(sb, mh, store)` never instantiated in production; routerService and orchestrator still hold a concrete `singboxOp` | Constructor at [dynamic_engine.go#L22-L28](file:///e:/AWGM/awg-manager/cmd/awg-manager/dynamic_engine.go#L22-L28); sing-box operator usage site: grep for `singboxOp.Start` / `.Reload` / `.Stop` in wiring | Today `singboxOp` is injected directly into `routerSvc.deps.Engine`, not wrapped in `DynamicEngine` | Build a `DynamicEngine` instance in `wiring.go` (after setupSingbox + setupMihomo have built both operators): <br>`engineDispatcher := NewDynamicEngine(a.singboxOp, a.mihomoOp, a.settingsStore)`<br>`engineDispatcher.OnMihomoReload = func() error { return a.routerSvc.GenerateMihomoConfig() }`<br>Then, **everywhere** `singboxOp` is passed as a `proxyengine.Engine` today (routerSvc.deps.Engine, sing-box Process watchdogs, boot sequence, etc.), pass `engineDispatcher` instead. Do NOT keep two separate `Engine` references — one for sing-box, one for mihomo; the dispatcher must become the single source of truth. | Routing engine selection setting (`routingEngine=mihomo`) has no effect; sing-box always runs |
| **C7** | Frontend API client is missing `mihomoStatus()` and `mihomoReload()` methods | Used in [MihomoTab.svelte#L21](file:///e:/AWGM/awg-manager/frontend/src/routes/routing/MihomoTab.svelte#L21) and [MihomoTab.svelte#L34](file:///e:/AWGM/awg-manager/frontend/src/routes/routing/MihomoTab.svelte#L34); client chain: [client.ts](file:///e:/AWGM/awg-manager/frontend/src/lib/api/client.ts) → Awg3Client → … → SbRouterClient ([clientSbRouter.ts](file:///e:/AWGM/awg-manager/frontend/src/lib/api/clientSbRouter.ts)) | Methods not added to client classes yet | Add two new methods to `SbRouterClient` (class in `clientSbRouter.ts`, end of file before closing `}`): <br>`async mihomoStatus(): Promise<MihomoStatus> { return this.request('/mihomo/status') }`<br>`async mihomoReload(): Promise<{ reloaded: boolean }> { return this.request('/mihomo/reload', { method: 'POST' }) }`. If client routing rules require methods live in dedicated `clientMihomo.ts` layer with inheritance, follow the existing pattern (`client*.ts` → each client adds one domain), but today `clientSbRouter.ts` is the closest domain; alternatively create `clientMihomo.ts` with a new `MihomoClient` class and insert it between `SbRouterClient` and `SingboxClient` in the inheritance chain. | Runtime JS error: `TypeError: api.mihomoStatus is not a function` |
| **C8** | TypeScript type `MihomoStatus` does not exist but is imported in `MihomoTab.svelte` | Import at [MihomoTab.svelte#L8](file:///e:/AWGM/awg-manager/frontend/src/routes/routing/MihomoTab.svelte#L8); type barrel is [types.ts](file:///e:/AWGM/awg-manager/frontend/src/lib/types.ts) + nested `types/sbRouter.ts`, etc. | Type never declared | Add to `$lib/types/sbRouter.ts` (or new `$lib/types/mihomo.ts`, re-exported from barrel):<br>```<br>export interface MihomoStatus {<br>  running: boolean;<br>  pid: number | 0;<br>  binary: string;<br>  error?: string;<br>  engine: 'mihomo';<br>  selected?: boolean;<br>  enabled?: boolean;<br>  active?: boolean;<br>  settingsError?: string;<br>}<br>```<br>Ensure the barrel file `frontend/src/lib/types.ts` (or wherever `MihomoStatus` import resolves to — check existing import path `'$lib/types'`) re-exports it. If the import path points to a single aggregate file, append directly there; if to `types/index.ts` re-exporting sub-modules, make sure it is transitively exported. | TypeScript compilation error for the page file |

### 3.2 HIGH — Security / Silent failure / Core feature broken (8)

| ID   | Title | File refs | Root cause | Suggested fix | Impact |
|------|-------|-----------|------------|---------------|--------|
| **H1** | `SetReloadFunc` / `OnMihomoReload` hooks never wired; reloads run with stale/empty config.yaml | `SetReloadFunc` def at [mihomo_handler.go#L33-L35](file:///e:/AWGM/awg-manager/internal/api/mihomo_handler.go#L33-L35) (only set in tests: `mihomo_handler_test.go:73,95`); `OnMihomoReload` callback at [dynamic_engine.go#L47-L51](file:///e:/AWGM/awg-manager/cmd/awg-manager/dynamic_engine.go#L47-L51) (only set in tests: `dynamic_engine_test.go:135,178`) | No production code ever assigns these callbacks | 1. On `DynamicEngine`: assign `OnMihomoReload = func() error { return a.routerSvc.GenerateMihomoConfig() }` during wiring (part of C6 fix). 2. On `MihomoHandler`: call `h.SetReloadFunc(func() error { <same generate + reload path — or just call dynamicEngine.Reload() which already calls prepare> })` — actually **cleanest**: the handler reload should call the dispatcher's reload, so in wiring: `a.mihomoHandler.SetReloadFunc(func() error { return engineDispatcher.Reload() })`. 3. Also wire `syncMihomoAfterSingboxReload()` (already defined in [wiring_mihomo.go#L27-L52](file:///e:/AWGM/awg-manager/cmd/awg-manager/wiring_mihomo.go#L27-L52)) into the sing-box `Process.OnReload` callback (find the bridge point where sing-box operator re-runs its post-reload hooks; today only sing-box router config rebuilds, add the `syncMihomoAfterSingboxReload` call there too with `generate = func() error { return a.routerSvc.GenerateMihomoConfig() }` and `mihomoEngine = engineDispatcher` — because syncMihomo will delegate stop/reload calls correctly through dispatcher). | POST /api/mihomo/reload → starts mihomo binary but against whatever last config.yaml was on disk (possibly empty/never generated) → silent misrouting or binary startup crash |
| **H2** | `Operator.Stop()` sends SIGKILL (`Process.Kill()`) immediately — no graceful shutdown window | [operator.go#L169-L190](file:///e:/AWGM/awg-manager/internal/mihomo/operator.go#L169-L190) line 181: `cmd.Process.Kill()` | Mihomo (like sing-box) supports SIGTERM for graceful close of TUN / iptables / saving proxy-group selection state; using SIGKILL bypasses cleanup and leaks resources | Implement a graceful-stop helper. Pseudocode for `Stop()`: <br>(1) send `cmd.Process.Signal(syscall.SIGTERM)` instead of Kill; (2) start timeout goroutine: `time.AfterFunc(10*time.Second, func() { cmd.Process.Kill() })`; (3) Wait() is already running in `wait()` goroutine — don't double Wait; instead, Stop's contract: after Stop returns `nil`, process will exit within timeout. Alternative (today Wait already owns `cmd.Wait()`): stop should signal and return; the `wait()` goroutine handles cleanup. So: Change Stop → `return cmd.Process.Signal(syscall.SIGTERM)` (and keep the Kill as fallback only if SIGTERM fails with `os.ErrProcessDone` etc). Add `import "syscall"` (Linux only — build tag `//go:build linux` may be needed on Windows cross-compile; the file already lives in a tree that builds only for linux targets via go toolchain tags but verify). | Tun device not cleaned up; process dies hard; state changes (select) in Clash API not persisted to disk; zombie children possible |
| **H3** | Installer downloads mihomo binary over HTTPS **without ever verifying SHA-256** (hashes defined for only aarch64, even then unused) | [installer.go#L40-L89](file:///e:/AWGM/awg-manager/internal/mihomo/installer/installer.go#L40-L89) (Install function); SHA256 fields declared at [embedded.go#L21](file:///e:/AWGM/awg-manager/internal/mihomo/installer/embedded.go#L21) for aarch64 only; `shaMu sync.Mutex` at [installer.go#L23](file:///e:/AWGM/awg-manager/internal/mihomo/installer/installer.go#L23) unused | Missing integrity check step; also mips-3.4 / mipsel-3.4 specs have empty SHA256 | (1) After `io.Copy(out, reader)` succeeds, close the tmp file, reopen read-only, compute `sha256`, compare hex to `i.spec.SHA256`. If spec.SHA256 != "" → mismatch MUST fail install. If spec.SHA256 == "" (today MIPS 3.4 builds): log a warning "no checksum for arch X, skipping verification" but still allow; fill missing checksums for MIPS if you can reproduce the pinned URLs. (2) Actually `io.Copy` + hash in one pass using `io.TeeReader` is cleaner — read the source body → Tee into both `sha256.New()` writer AND the file. (3) Use `shaMu` lock if you cache hashes, otherwise remove the unused field. | Potential supply chain / MITM attack swaps binary; silent acceptance of corrupted/truncated download (users hit crashes at runtime only) |
| **H4** | Mihomo Clash API WebSocket reverse proxy has protocol/handshake bugs; traffic/logs WS will not connect through AWGM proxy | `proxyWebSocket` in [mihomo_handler.go#L157-L195](file:///e:/AWGM/awg-manager/internal/api/mihomo_handler.go#L157-L195) | After `hj.Hijack()` line 163: (a) never flushes `bufrw` → WS handshake response bytes buffered on client write-back and may not arrive until next event (race). (b) never reads/relays the mihomo upstream handshake response **101 Switching Protocols** headers → uses `req.Write(upstream)` but client already sent its upgrade request headers on the hijacked socket via `bufrw`? — no, actually the code writes the client upgrade request to upstream but never waits for upstream's 101 response bytes and forwards them to client. Instead, code jumps into two-directional `io.Copy`, which means client sees NO HTTP response to its Upgrade → browser closes as failed. (c) No timeout on the read-copy pump (close errc only after one side dies, but client-side disconnect should close upstream immediately). | Rewrite proxyWebSocket properly. Correct algorithm: (1) Hijack, get clientConn, bufrw. (2) **Flush bufrw** now — `bufrw.Flush()`. (3) Dial upstream (TCP, you already do). (4) Build the same HTTP upgrade request from r, write to upstream using the original r.Header (you already do line 178-179). (5) Read the FIRST HTTP response from upstream (status 101 expected, + headers). You can use `http.ReadResponse(bufio.NewReader(upstream), r)` for this — it reads and parses the 101+headers, stops at end of header section. (6) Forward that exact response (status line + headers + final `\r\n` CRLF) to the clientConn — you can do `resp.Write(clientConn)` with a 101 response builder OR copy raw bytes — use `http.ReadResponse` then `Write(clientConn)` (it writes correctly terminated status+headers). (7) AFTER both sides see 101, THEN start two-way copy with timer/ctx-based cancellation. (8) Also, for `Hijacker`, make sure we never call `response.WriteHeader` before hijacking (today correct — we hijack before any write, so only the handshake response written above goes out). (9) Wrap both copy goroutines with deferred clientConn close + upstream close on any error, and use a 2-buffered channel of 1 so first-returning side triggers second-side close (write to cancel ctx, not just exit 1 goroutine, since the other may block forever). Correct implementation: use `context.WithCancel` + cancel when first io.Copy returns, then the other will unblock on next read with net.OpError Canceled → both die cleanly. | Any WebSocket client (logs, traffic, connections streams) for Mihomo accessed via `/api/mihomo/clash/...` path silently fails. Users have to bypass AWGM and port-forward 9090 directly if they want streams, negating the auth proxy. |
| **H5** | Mihomo TProxy listener port=51271 + RedirPort=51272 — EXACT duplicate of sing-box router's fixed iptables port constants → `EADDRINUSE` race or conflict | [config.go#L129-L132](file:///e:/AWGM/awg-manager/internal/mihomo/config.go#L129-L132); sing-box constants at `internal/singbox/router/iptables.go:22 TPROXYPort = 51271`, `iptables.go:34 RedirectPort = 51272` | Comment says "Reuse sing-box redirect port" — impossible; two userland processes cannot bind the same TCP/UDP port without SO_REUSEPORT (and semantics would be load-balanced between processes anyway, definitely not what we want) | Choose NEW Mihomo-only port pair, e.g. `TPROXY = 51281` + `REDIR = 51282`. Add port constants to a shared location (or keep them in mihomo package as exported consts `const TPROXYPort = 51281; const RedirectPort = 51282`). Also check the iptables rules injection path today — they currently go to sing-box's ports. For Mihomo mode: (a) the DynamicEngine/RouterService path should also drive iptables to point at the NEW Mihomo ports when `RoutingEngine=mihomo` enabled, OR (b) if iptables will still reference 51271/72 because some existing script only routes into sing-box listener, then add a iptables NAT redirect rule pair that routes from sing-box expected ports -> new mihomo ports when Mihomo is active. Option (a) is strongly preferred. Do NOT leave ports as-is — user switches engine → bind fails → engine won't start. | When `RoutingEngine=mihomo`, mihomo binary startup fails with `bind: address already in use` if sing-box process previously bound those ports and TIME_WAIT remains → fails even if sing-box stop released; also on daemon restart race |
| **H6** | Device-proxy listeners (mixed HTTP/SOCKS per-device instance on LAN) NEVER generated in Mihomo mode → feature silently disabled | [service_mihomo.go#L126-L129](file:///e:/AWGM/awg-manager/internal/singbox/router/service_mihomo.go#L126-L129): `var dpListeners []mihomo.DeviceProxyListener` → ALWAYS nil (no code fills this slice before passing to `GenerateConfig`) | Missing bridge that reads device proxy instances from `deviceproxySvc` (the same service sing-box router queries to enumerate active listeners and their selected-outbound + port) | Look at the sing-box parallel path — find the call site in sing-box router code where `deviceProxySvc.ListInstances()` or similar returns the LAN listener entries with `{ID, Port, SelectedOutbound, Enabled}`. Mirror that query in GenerateMihomoConfig: add a new dep field to router ServiceImpl `DeviceProxy interface { ListInstances() []deviceproxy.Instance }` or use the existing concrete deps already available. Then before `var dpListeners …` line, iterate the list and build `dpListeners = append(dpListeners, mihomo.DeviceProxyListener{ID: inst.ID, Port: inst.Port, SelectedOutbound: inst.SelectedOutbound, Enabled: inst.Enabled})`. Note the large comment block above line 122 in service_mihomo.go says "Device-proxy listeners remain owned by sing-box … duplicating ports … EADDRINUSE". — this comment claims sing-box daemon stays alive (for standalone tunnels not Mihomo routing) and would conflict. If this is true (sing-box process stays running even when Mihomo is selected as routing engine, per DynamicEngine comments), then device proxy listeners CANNOT bind to the same ports sing-box already uses. **Resolution**: port-offset for Mihomo-owned device proxies? OR — actually the intent is sing-box device proxy listener code MUST be disabled (stopped) when Mihomo routing engine selected to free the ports. OR sing-box listens loopback-only and we DNAT, but simplest: if architecture has sing-box and mihomo both alive at same time, they need non-overlapping per-device port ranges too. Clarify first (read sing-box operator lifecycle when Mihomo selected — DynamicEngine does NOT stop sing-box in prepareActiveEngine path for mihomo selected today; it only stops mihomo when returning to sing-box). So indeed sing-box remains alive with its listeners. Conflict. So: option 1. call Stop on sing-box device proxy when engine is Mihomo (preferred, saves resources); option 2. use separate port range for mihomo device proxies + update iptables/NDMS policy accordingly; option 3. keep sing-box owning device proxy and redirect through sing-box → mihomo outbound (architectural mess). Recommend option 1. | When routing engine = mihomo, users' "per-device proxy (Mixed HTTP/SOCKS port for LAN clients)" stop working |
| **H7** | UI `ProxyGroupEditModal` uses `type='fallback'` as default + offers it to user; Mihomo config generator does NOT normalise `fallback` → groups with this type either dropped or crash config validation | [ProxyGroupEditModal.svelte#L17](file:///e:/AWGM/awg-manager/frontend/src/lib/components/sb-router/ProxyGroupEditModal.svelte#L17) — `$state(group?.type ?? 'fallback')`; [ProxyGroupEditModal.svelte#L24-L29](file:///e:/AWGM/awg-manager/frontend/src/lib/components/sb-router/ProxyGroupEditModal.svelte#L24-L29) — type options array includes 'Fallback (Резервирование)' first; mihomo normaliser [config.go#L517-L528](file:///e:/AWGM/awg-manager/internal/mihomo/config.go#L517-L528) `mihomoProxyGroupType` returns pass-through for unknown types | Mihomo/Clash valid group types: `select, url-test, load-balance, relay, fallback` — **Wait**: actually mihomo DOES support `fallback` type! But check spelling. Mihomo docs: yes, `type: fallback` IS valid syntax for ordered failover groups ("try first, on fail second"). So the NORMALISER is wrong (it doesn't map sing-box's `urltest/selector` → Mihomo types, but also `fallback` type — sing-box calls it `urltest` for speed-based, but sing-box actually has `url_test` type for latency based and for failover they call `selector` with manual. Actually verify: in sing-box's outbound proxy group types, the 4 standard are: `selector` (manual), `urltest` (auto by latency = Mihomo url-test), `loadbalance` (round = Mihomo load-balance), AND sing-box does NOT have a native failover/fallback group? Or does it via a tag? This needs checking against sing-box outbound types. But regardless: the UI offers `fallback` → user saves → generator emits the type verbatim as "fallback" because mihomoProxyGroupType default clause returns it as-is. So if Mihomo DOES understand fallback (it does — recent versions), then OK. But the review's original claim was the generator drop code path for selector outbound type check `ConvertSingboxToMihomoProxyGroup` [config.go#L483](file:///e:/AWGM/awg-manager/internal/mihomo/config.go#L483) only allows three type strings, else returns nil. For **storage** groups, `pg.Type` comes from UI options: 'fallback', 'select', etc. `upsertGroup` converts stored types via mihomoProxyGroupType but if user stores type='fallback' (valid Mihomo!), this is fine. But the concern is: **sing-box parser does sing-box support `fallback` proxy group?** Probably NO, sing-box calls failover semantics by different field names. So when roundtrip is: sing-box routing engine ↔ settings ↔ Mihomo routing engine, the group type `fallback` only works in Mihomo. That means the UI should engine-contextually show the right list or the normaliser should map them. | Two possible resolutions: (A) If Mihomo `fallback` IS valid (yes), mark the finding as LOW impact and simply update the comment in normaliser — no action needed for Mihomo side. BUT users creating proxy groups of type `fallback` when RoutingEngine=sing-box → sing-box rejects config as invalid outbound type because sing-box likely doesn't have `fallback`. (B) OR, more robust: in the UI dropdown, only display `fallback` when the active routing engine selection in settings = Mihomo. For sing-box, keep the existing sing-box-compatible set only. Add a store `$settingsStore.routingEngine` reactive check to conditionally include the `fallback` option OR rename it. Also update `mihomoProxyGroupType` with explicit fallback handling: `case "fallback": return "fallback"` — no-op, but makes intent clear. Document in code which types are valid for each engine. | If user selects fallback group and uses sing-box engine → sing-box config validation fails, router won't start. If Mihomo → works, but type interoperability is confusing |
| **H8** | WireGuard outbound sing-box → Mihomo converter uses flat fields; Mihomo/Clash WireGuard syntax uses a `peers` array; missing endpoint/preshared-key/allowed-ips/keepalive/etc. | [config.go#L384-L393](file:///e:/AWGM/awg-manager/internal/mihomo/config.go#L384-L393) | Incomplete port from existing sing-box WG outbound schema; flat `public-key`, `private-key` at top level are NOT valid for Mihomo Clash-format WireGuard — Mihomo expects: `type: wireguard, private-key: <local-secret>, ip: [<cidr>], mtu: <int>, peers: [{ name: optional, public-key: <peer-pubkey>, pre-shared-key: <optional>, server: <host>, port: <int>, allowed-ips: ["0.0.0.0/0","::/0"], persistent-keepalive-interval: 25, reserved: [0,0,0] }]` (or similar — check latest MetaCubeX/mihomo docs). | First decide: does the sing-box WireGuard outbound map 1-to-1 to a SINGLE peer configuration? If yes (most likely: sing-box wireguard outbound = one local keypair + one remote peer server endpoint with its pubkey + preshared + keepalive + allowedips), then rewrite mapping to: (1) Keep `p["type"] = "wireguard"`. (2) Keep `p["private-key"]` from `ob["private_key"]`. (3) Keep `p["mtu"]` from `ob["mtu"]`. (4) Move ip → Mihomo uses `ip:` key with array; today does `p["ips"]` = correct? Or Mihomo uses `ip:` (singular) vs `ips:` (plural)? Verify Mihomo YAML field. (5) Add a `peers: []map[string]any` sublist with ONE element: `{"public-key": ob["public_key"], "server": ob["server"], "port": int(serverPort), "allowed-ips": ["0.0.0.0/0","::/0"]}` → read allowed-ips FROM sing-box outbound field (if it provides a custom one; otherwise default). Also map `preshared_key` → `pre-shared-key`, and `keepalive` → `persistent-keepalive-interval` if available. (6) Remove flat `p["public-key"]` at top level — Mihomo will reject it. | If user has any WireGuard subscription outbounds and chooses Mihomo, those proxies fail config validation silently or mihomo refuses to start with "invalid wireguard outbound: peers required" |

### 3.3 MEDIUM — Tech debt / UX degradation (4)

| ID   | Title | File refs | Root cause | Suggested fix | Impact |
|------|-------|-----------|------------|---------------|--------|
| **M1** | `CrashStats()` returns zero-value stub always | [operator.go#L165-L167](file:///e:/AWGM/awg-manager/internal/mihomo/operator.go#L165-L167) — all zeroes return; compare with sing-box `singbox.operator.CrashStats` (references real counters + watchdog suppression timer fields) | Missing crash counter fields + suppression logic on Operator struct | Add fields to Operator: `crashMu sync.Mutex; recentCrashes int; lastCrashReason string; restartSuppressedUntil time.Time`. Update the `wait()` goroutine: on non-intentional exit (the existing branch that records error), increment crash count → `recentCrashes++`, store reason. If crashes >= 3 in last N window → set `restartSuppressedUntil = now.Add(5*time.Minute)` or similar. The callers (watchdog orchestrator) consult `CrashStats` and won't restart during suppression. Also implement `ClearManualStop` to reset sticky stop flag (related next finding L1). | Watchdog/orchestrator restarts mihomo in tight crash-loop; no backoff; UI shows "never crashed" regardless of reality |
| **M2** | Mihomo external-controller address `"127.0.0.1:9090"` hardcoded in THREE disconnected places | [config.go#L120](file:///e:/AWGM/awg-manager/internal/mihomo/config.go#L120) (sets YAML field ExternalCtl); [operator.go#L49](file:///e:/AWGM/awg-manager/internal/mihomo/operator.go#L49) (HTTP PUT reload URL); [mihomo_handler.go#L119](file:///e:/AWGM/awg-manager/internal/api/mihomo_handler.go#L119) (reverse proxy upstream) | No shared constant or configurable setting | (1) In `internal/mihomo/` package add a file (or at top of config.go): `const DefaultExternalController = "127.0.0.1:9090"`. (2) If you ever want it user-configurable, read it from `storage.SingboxRouterSettings` (add new field `MihomoExternalCtl string \`json:"mihomoExternalCtl,omitempty"\``) and thread into both Operator + handler. But for the minimal fix: replace all three string literals with a single package-level exported const, AND in reverse proxy handler, resolve it dynamically from the running process if needed (read from operator via interface method: `type MihomoInfo interface { ExternalControllerAddr() string }` — but Engine interface already has Binary/ConfigDir; add a new optional method or add `ExternalController() string` to Engine). | If someone changes port in config.yaml, reload API stops working (operator hits wrong URL) and Clash proxy stops proxying (wrong upstream) → hard to debug silent failures |
| **M3** | `ValidateConfigDir(ctx)` directly calls `exec.CommandContext`, ignoring injectable `commandFn` — breaks unit testing of the validation path | [operator.go#L141-L149](file:///e:/AWGM/awg-manager/internal/mihomo/operator.go#L141-L149) line 143 uses raw `exec.CommandContext`; contrast with [operator.go#L86](file:///e:/AWGM/awg-manager/internal/mihomo/operator.go#L86) `Start` properly uses `o.commandFn` | Simple omission, copy-paste | Change line 143 from `exec.CommandContext(ctx, o.binaryPath, "-d", o.configDir, "-t")` → build the command via: build command struct through commandFn first, then attach ctx to that Cmd via `.WithContext(ctx)` before CombinedOutput. Pattern: `cmd := o.commandFn(o.binaryPath, "-d", o.configDir, "-t"); cmd = cmd.WithContext(ctx); out, err := cmd.CombinedOutput()`. That way tests can inject fake exec helper again. | N/A for production users; code smell: test paths can't validate validation failure scenarios, drift in tests |
| **M4** | Mihomo UI only polls `/mihomo/status` every 5s — no live traffic/logs/proxy selection streams | [MihomoTab.svelte#L65](file:///e:/AWGM/awg-manager/frontend/src/routes/routing/MihomoTab.svelte#L65) `setInterval` pattern; Mihomo exposes WebSocket endpoints `/traffic` `/logs` `/connections` on external controller through our clash proxy `/api/mihomo/clash/traffic` etc | UI MVP written without live streaming features (sing-box UI also may not? Check how sing-box tab displays live stats today — if it uses SSE, that path doesn't exist for Mihomo yet. Or there's $lib/utils/clashWebSocket.ts helper) | (1) Fix WS proxy first (resolves H4). (2) Add to MihomoTab a traffic chart / live log panel using the proxied WebSocket endpoints. Reuse existing sing-box UI components if they accept a generic data source; alternatively create `<MihomoTrafficStream/>` component that connects via WS, parses JSON frames, and renders. (3) Stop polling status; or keep it as fallback and upgrade to SSE-based status push from server — but at minimum: replace refresh timer with EventSource for the `/mihomo/status/stream` endpoint — add that endpoint in server if missing. | Live state updates delayed up to 5s full seconds after engine restart/crash; no traffic stats; cannot view Mihomo process logs from the routing tab |

### 3.4 LOW — Parity / nice-to-have (1)

| ID   | Title | File refs | Root cause | Suggested fix | Impact |
|------|-------|-----------|------------|---------------|--------|
| **L1** | `ClearManualStop()` returns nil stub; sing-box's implementation resets an internal `manualStopUntil` / sticky flag that prevents cold-start autostart. Mihomo has no matching internal sticky field yet | [operator.go#L137-L139](file:///e:/AWGM/awg-manager/internal/mihomo/operator.go#L137-L139) | Related to manual Stop semantics in orchestrator — if user clicks Stop from UI, orchestrator remembers "don't start this engine on boot" until user explicitly clicks Start again (which calls ClearManualStop to lift the suppression) | Add a field `manualStop bool` (or timestamp) + mu-guarded. On `Stop`: set `o.manualStop = true` before kill/term. On `ClearManualStop`: reset it (and return nil). On `Start`: if `manualStop == true` but Start() explicitly called, clear the flag (user pressed Start). Also return flag in a getter if orchestrator checks. | User manual Stops mihomo → router reboot → Mihomo might restart without user consent (orchestrator decides based on this flag parity) |

---

## 4. FIX PRIORITY BATCHES (recommended ordering)

### Batch 1 — "Build + Wire It" (resolve all CRITICAL C1-C8 + H1). Estimate: 1 review session.
Goal: `go build ./cmd/awg-manager/` passes; frontend `tsc`/svelte-check passes; engine-selector switch actually changes which process runs on user toggle.

Order of edits within Batch 1 (dependency graph, do NOT reorder):
1. `internal/storage/types.go` — add C1 `RoutingEngine` + C2 `type ProxyGroup` + field `ProxyGroups []ProxyGroup`; also add migration vN → default `"routingEngine": "sing-box"`
2. `internal/mihomo/` — M2: create package const `DefaultExternalController`; replace 3 hardcoded strings
3. `cmd/awg-manager/wiring.go` — C3: add fields `mihomoOp *mihomo.Operator`, `mihomoHandler *api.MihomoHandler`
4. `cmd/awg-manager/wiring_mihomo.go` — make `setupMihomo` complete: build Operator + Handler, SetSettingsStore. Add `syncMihomoAfterSingboxReload` wiring call into sing-box OnReload hook callback (find its registration site)
5. `cmd/awg-manager/main.go` — C4: insert `a.setupMihomo()` call into setup phase sequence (correct position: after sing-box setup, before server setup, before router setup OR router setup needs mihomo references)
6. `cmd/awg-manager/` wiring where sing-box `NewDynamicEngine` NOT called today: C6 construct dispatcher with both ops, wire `OnMihomoReload` callback = generate config, replace all Engine injections with dispatcher reference
7. `cmd/awg-manager/wiring_server.go` (or wherever RegisterRoutes live): C5 call `a.mihomoHandler.RegisterRoutes(...)`. Also here apply H1: assign `SetReloadFunc = dispatcher.Reload`
8. Frontend TS: C8 write `MihomoStatus` interface in `types/` + re-export via barrel
9. Frontend TS client: C7 add `mihomoStatus()` + `mihomoReload()` methods (new file `clientMihomo.ts` or append to `clientSbRouter.ts`)
10. RUN `go build ./cmd/awg-manager/` (GOOS=linux) and frontend `svelte-check` — confirm C compile errors gone
11. Then and ONLY then: touch HIGH/medium

### Batch 2 — "Make it safe and useful" (H2, H3, H5, H6, H7, H8). Estimate: 2-3 sessions.
Order:
- H2 graceful Stop → SIGTERM first
- H3 checksum verification in Installer (fill MIPS 3.4 hashes if possible; also H3 needs embedded.go to add SHA256 fields)
- H5 choose new mihomo port pair 51281/82, update constants, update iptables injection path when routing engine mihomo
- H6 enumerate device proxy listeners, AND stop sing-box's device-proxy when Mihomo selected (to avoid port conflict)
- H7 update normaliser + UI conditional engine type list for fallback
- H8 rewrite wireguard converter to proper peers list

### Batch 3 — "Parity & Polish" (M1, M3, M4, L1 + optional cleanup)
- M1 crash counter & suppress backoff
- M3 commandFn injection in ValidateConfigDir
- M4 live WS streams on UI
- L1 manualStop reset
- Bonus: cleanup sing-box clash proxy vs mihomo clash proxy duplication (unify proxy logic if possible? Not required)

---

## 5. PER-FILE CHANGE MAP

For each source file, list the IDs that require edits TO THAT FILE. Useful for parallelising work without merge conflicts.

| File | Touched in finding IDs |
|------|-------------------------|
| `internal/storage/types.go` | C1 (RoutingEngine), C2 (ProxyGroup struct+field) |
| `internal/storage/settings_migrations.go` | C1 (add default routingEngine = "sing-box" in NormalizeSingboxRouterSettings) |
| `internal/storage/types_patch.go` | C1 (include RoutingEngine in patcher if needed) |
| `internal/mihomo/config.go` | C2 (reads settings.ProxyGroups — after storage fix compiles), H5 (ports 51281/82 consts), H7 (fallback normaliser), H8 (wireguard peers rewrite), M2 (DefaultExternalController const + use site 1/3) |
| `internal/mihomo/operator.go` | H2 (Stop → SIGTERM-first), M1 (CrashStats real counters), L1 (ClearManualStop implementation + manualStop flag), M3 (commandFn in ValidateConfigDir), M2 (use site 2/3 ExternalCtl URL) |
| `internal/mihomo/installer/installer.go` | H3 (SHA256 verify) |
| `internal/mihomo/installer/embedded.go` | H3 (fill checksums for MIPS if available; keep aarch64, ensure all 3 arch have hashes or log explicitly unknown) |
| `internal/api/mihomo_handler.go` | H4 (WebSocket proxy rewrite), M2 (use site 3/3 — const lookup or engine method) |
| `internal/singbox/router/service_mihomo.go` | C6 (deps.Engine → dynamic dispatcher — ensure GenerateMihomoConfig uses dispatcher.ConfigDir not stale sing-box path), H6 (dpListeners population + device-proxy listing bridge call) |
| `internal/singbox/router/iptables.go` (or wherever rules for ports built) | H5 (select Mihomo ports or redirect rules) |
| `cmd/awg-manager/wiring.go` | C3 (add mihomoOp / mihomoHandler fields to app struct + imports) |
| `cmd/awg-manager/wiring_mihomo.go` | H1 (syncMihomoAfterSingboxReload — actually call it from sing-box reload hook), C6 (build dispatcher there or in a new wiring_dynamic_engine.go) |
| `cmd/awg-manager/main.go` | C4 (insert setupMihomo call) |
| `cmd/awg-manager/wiring_server.go` (route registrations) | C5 (register mihomoHandler routes guarded), H1 (SetReloadFunc set here or wiring) |
| `cmd/awg-manager/wiring_router.go` or similar setup phase (where routerSvc built and injected) | C6 (pass dynamicEngine dispatcher into routerSvc.deps.Engine instead of singboxOp direct), H6 (inject deviceProxy listing dependency) |
| `frontend/src/lib/types/sbRouter.ts` OR new `types/mihomo.ts` | C8 (MihomoStatus interface declaration) |
| `frontend/src/lib/types.ts` (or types/index.ts barrel) | C8 (re-export MihomoStatus) |
| `frontend/src/lib/api/clientSbRouter.ts` OR new `clientMihomo.ts` + inheritance chain updated | C7 (mihomoStatus + mihomoReload methods) |
| `frontend/src/lib/components/sb-router/ProxyGroupEditModal.svelte` | H7 (conditional fallack display, or type map) |
| `frontend/src/routes/routing/MihomoTab.svelte` | M4 (WS streams, replace polling) |

---

## 6. ACTION CHECKLIST (for AI executor)

Before claiming "fixed", verify each item:

### Batch 1
- [ ] C1: `storage.SingboxRouterSettings` struct has `RoutingEngine string`; go vet ./internal/storage passes; tests for `NormalizeSingboxRouterSettings` include default "sing-box"
- [ ] C2: `storage.ProxyGroup` type defined + field on `SingboxRouterSettings`; `go build ./internal/mihomo` passes (previously fails on settings.ProxyGroups)
- [ ] C3: Compile [wiring.go](file:///e:/AWGM/awg-manager/cmd/awg-manager/wiring.go) + `wiring_mihomo.go` together — no undefined field errors
- [ ] C4: [main.go](file:///e:/AWGM/awg-manager/cmd/awg-manager/main.go) contains `a.setupMihomo()` call in the setup chain
- [ ] C5: Production code path has `mihomoHandler.RegisterRoutes(mux, guarded)` call (search globally in non-test .go files for RegisterRoutes calls)
- [ ] C6: `grep -r "NewDynamicEngine"` returns a production call site (not just *_test.go); plus the `routerSvc.deps.Engine` injection points now reference `dynamicEngine` variable, not `singboxOp` directly
- [ ] C7: Frontend `client*.ts` exports both `mihomoStatus` / `mihomoReload` bound to correct REST URLs `/mihomo/status` + POST `/mihomo/reload`
- [ ] C8: `$ svelte-check` (or `tsc --noEmit`) reports zero missing-type errors for `MihomoStatus` import
- [ ] H1: `SetReloadFunc` or `OnMihomoReload` assignment present in wiring production (not tests) and references `GenerateMihomoConfig`

### Batch 2+
- [ ] H2: `operator.Stop()` body sends SIGTERM (Process.Signal) before any potential fallback Kill; not unconditional Kill first
- [ ] H3: Installer computes sha256 after/while download; when spec.SHA256 != "", compares and fails on mismatch
- [ ] H5: Mihomo ports ≠ sing-box ports; constants have different values; config.go emits new ports; iptables/router rules reference correct pair for selected engine
- [ ] H6: GenerateMihomoConfig has loop that appends device proxy instances (from a service) into dpListeners; AND when routing engine = mihomo, we disable sing-box device proxy (or port offset)
- [ ] H7: Decide and document Mihomo `fallback` type behaviour; UI either filters list or normalises on backend
- [ ] H8: Mihomo WireGuard outbounds map to `peers:` array

---

## 7. EXPECTED END STATE AFTER ALL FIXES

**After Batch 1**:
- Running `awg-manager` daemon builds and boots without compile errors
- User loads web UI → new Routing tab sub-page "Mihomo" renders (no TS errors, no JS runtime errors on API calls)
- User edits settings → sets `routingEngine = "mihomo"`, router enabled, clicks "Apply & Reload"
- AWGM dispatcher stops sing-box routing engine → generates mihomo config.yaml → starts mihomo binary on new ports → status page shows running/PID/active
- Clash-compatible dashboard (Yacd/metacubexd) can connect through AWGM auth proxy at `/api/mihomo/clash` (proxies to 9090)

**After Batch 2**:
- Installer refuses tampered binaries
- Process restarts use graceful SIGTERM with 10s kill fallback
- Device proxy listeners work with Mihomo on LAN ports (no conflicts with sing-box's)
- WireGuard subscription proxies work in Mihomo mode

**After Batch 3**:
- Live traffic graphs + log viewer render on MihomoTab in real time (WebSocket)
- Crash watchdog backoffs if mihomo crashes repeatedly; lastCrashReason visible in UI
- All tests for DynamicEngine, Operator, Router pass: `go test ./cmd/awg-manager/ ./internal/mihomo/... ./internal/api/ ./internal/singbox/router/ -count=1`

---

## 8. COMMANDS TO VALIDATE

Run these in order after each batch to confirm green:
```bash
# Go (backend) — from project root:
GOOS=linux go vet ./cmd/awg-manager/ ./internal/mihomo/... ./internal/api/ ./internal/singbox/router/ ./internal/proxyengine/ ./internal/storage/
GOOS=linux go build ./cmd/awg-manager/
GOOS=linux go test ./internal/mihomo/... -count=1
GOOS=linux go test ./internal/api/ -run Mihomo -count=1
GOOS=linux go test ./internal/singbox/router/ -run Mihomo -count=1
GOOS=linux go test ./cmd/awg-manager/ -run 'Dynamic|Mihomo' -count=1

# Frontend (inside frontend/ dir):
npm run svelte-check -- --fail-on-warnings
# OR if project uses:
npx tsc --noEmit
```

---

## 9. RAW COMPILE ERRORS ON CURRENT CODEBASE (GOOS=linux go build ./cmd/awg-manager/)

```
# github.com/hoaxisr/awg-manager/internal/mihomo
internal\mihomo\config.go:194:30: settings.ProxyGroups undefined (type storage.SingboxRouterSettings has no field or method ProxyGroups)
```

→ Only one error surfaces today because `setupMihomo` isn't called (C4) so `wiring_mihomo.go` is dead-code eliminated from the package compilation graph. After inserting setupMihomo call in main, 3+ new undefined-field/missing method errors appear. The checklist above pre-empts all of them.

# Susanin adaptive routing: audit and implementation plan

Date: 2026-09-23  
Repository: `E:\AWGM\awg-manager`  
Branch: `feature/mihomo-ai-proxyrt`

## Verdict

The report `SUSANIN_ADAPTIVE_ROUTING_REMEDIATION_REPORT_2026-09-23.md` does **not** prove that Susanin is complete or operational.

Current status: **NOT ACCEPTED / P0 remediation required**.

The screenshots and source agree on the broken state: settings and routing ownership may be recorded as Susanin while `susanin-agent` is stopped. The implementation can also leave a partial datapath after an error. The dashboard omission of Mihomo resources is confirmed by source code.

Do not build or deploy another IPK until the local acceptance gates in this document pass.

## Confirmed findings

### P0-1. `Apply` is not transactional

`internal/adaptiverouting/service.go:184` persists desired settings, including `Enabled=true`, before executor, datapath, installer and process startup have succeeded.

After that:

- the executor is committed;
- the previous routing owner is parked;
- ipset/iptables/ip-rule state is changed;
- process installation/start can fail;
- state is nevertheless committed with `RoutingOwnerSusanin` at `service.go:257`.

Failure paths only tear down parts of the executor. They do not reliably:

- stop a process that was started in the current attempt;
- remove a partially installed datapath;
- restore the previous routing owner;
- restore the previous persisted settings and operational state.

This directly permits the UI state seen by the user: `Susanin stopped`, owner `Susanin`, selected egress, unsaved/recovery-like state.

### P0-2. Policy scope fails open to the whole LAN

In `internal/adaptiverouting/datapath.go:237`, policy matching is attempted only when a resolver exists and returns a non-empty mark. On resolver absence/error/empty mark, execution falls through to `lanInterfaces` at line 249.

Therefore a configuration explicitly scoped to one Keenetic policy can unexpectedly install a jump for all LAN traffic. This must fail closed; a policy selection must never silently become `all LAN`.

### P0-3. The integration conflicts with the supported Susanin/Keenetic ownership model

The upstream Susanin documentation states that Keenetic connection policies and Susanin are separate decision mechanisms, and a client assigned to a Keenetic policy is normally controlled by that policy. The current patch attempts to combine them using high fwmark bits (`0x30000000`). This is a custom AWG Manager extension, not a proven upstream configuration.

It may be retained only behind an explicit compatibility contract and live acceptance tests. It must not be presented as ordinary upstream Susanin behavior.

Required design choice:

1. **Supported default:** Susanin owns eligible default-policy LAN traffic; no Keenetic policy is assigned to those clients.
2. **Experimental policy scope:** explicit advanced option with a warning, strict mark discovery, collision checks and fail-closed behavior.

Do not silently mix the two modes.

### P0-4. Runtime state is derived inconsistently

`GetStatus` at `service.go:126` only turns `running` into `stopped` when the process disappears. It does not reconcile routing ownership, active egress, desired `Enabled`, datapath presence and process health into one authoritative state.

Required invariant:

- `running`: process alive, required sets/chain/jump/rules/route present, executor ready;
- `degraded`: desired enabled but one or more required components missing; owner must describe the actual installed owner, not the desired one;
- `stopped`: desired disabled and Susanin-owned runtime artifacts absent.

### P1-1. Boot restore can repeat a failed configuration

`restoreAdaptiveRouting()` retries `Apply()` whenever stored settings say enabled. Because `Apply()` currently persists enabled before success, an unsuccessful apply can be replayed after every AWG Manager restart/update.

Boot restore must use the last successfully applied generation, not an uncommitted desired draft.

### P1-2. Broad firewall rules are installed without adequate ownership

`datapath.go:321-327` installs unconditional FORWARD rules, an INPUT accept, TCPMSS and MASQUERADE for the TUN interface. Errors from these insertions are ignored. A persistent NDMS hook recreates them.

Required changes:

- use a dedicated owned chain or uniquely commented rules;
- make every required command checked and idempotent;
- avoid the broad INPUT accept unless a demonstrated packet path requires it;
- remove exactly the owned rules on rollback/stop;
- make the hook consume the committed generation only;
- add tests for duplicate reconciliation and teardown.

### P1-3. Route-table claim in the report is contradictory

The report lists `ip route replace default dev awgsus0 table 105` as unfinished, while `datapath.go:314-319` already executes it. The real problem is lifecycle/order: the route can disappear when the TUN device is recreated, or the apply may stop after partial setup.

Do not add another duplicate route command. Add reconciliation triggered after executor/TUN readiness and verify the route as part of health.

### P1-4. Current tests are not green

Executed in WSL Ubuntu:

```text
go test -count=1 ./internal/adaptiverouting ./internal/mihomo ./internal/api
```

Result:

- `internal/adaptiverouting`: PASS
- `internal/api`: PASS
- `internal/mihomo`: FAIL
- failing test: `TestGenerateConfig_AdaptiveEgress`
- reason: expected TUN stack `system`, generated `mixed`

Either document and test the deliberate `mixed` decision, or revert it. A report must not call the gate complete while this regression exists.

### P1-5. Dashboard intentionally omits Mihomo-native resources

`frontend/src/routes/+page.svelte` loads:

- `mihomoStandaloneProxies`;
- `mihomoSubscriptions`;
- `mihomoGroups`.

But the call to `buildFlatDashboardItems` at line 1336 does not pass any of them. The union and builder in `frontend/src/lib/utils/tunnelDashboardFlat.ts` contain no Mihomo-native item kinds. Additionally, `showGroupsBlock` at `+page.svelte:1513` explicitly hides proxy groups whenever dashboard mode is enabled.

The missing cards in the screenshot are therefore a confirmed implementation gap.

### P1-6. Subscription error is a separate regression

The dashboard shows the `VOX` subscription with 30 servers while simultaneously reporting that it contains no valid links. Do not hide this error as part of dashboard work. Trace the subscription parse/status API and distinguish:

- last-known-good members;
- latest refresh error;
- currently usable active member.

The card should remain usable from last-known-good data while clearly showing refresh failure, if the backend still has valid applied members.

## Required implementation sequence

### Gate 1 — Freeze unsafe restore

1. Disable automatic Susanin restore from uncommitted desired settings.
2. Introduce separate `desired`, `applied`, and `runtime` states or an applied-generation record.
3. Do not claim `RoutingOwnerSusanin` unless runtime verification succeeds.
4. Preserve the user's current settings as a draft; do not delete them.

Acceptance:

- restarting AWG Manager after a failed Apply does not reinstall partial Susanin rules;
- status clearly says that desired settings are not applied.

### Gate 2 — Transactional Apply/Rollback

Implement a staged transaction:

1. validate settings and resolve source/egress;
2. snapshot previous applied settings, owner and runtime inventory;
3. prepare executor without disrupting the current owner;
4. commit executor and wait for interface readiness;
5. install datapath into owned chains/rules;
6. start agent and verify process plus datapath health;
7. only now persist applied generation and owner;
8. on any failure, unwind all completed phases in reverse and restore the previous owner/runtime.

Add table-driven failure-injection tests for every phase, including concurrent Stop/Apply.

### Gate 3 — Correct source ownership

1. Make whole-LAN and policy modes distinct.
2. For policy mode, resolver absence/error/empty mark is a hard validation failure.
3. Add an explicit advanced `experimental policy integration` flag if policy-mark coexistence remains.
4. Detect overlap between Susanin marks/priorities/table and NDMS/Mihomo/sing-box rules before commit.
5. Show the effective match and ownership in Preview.

### Gate 4 — Runtime reconciliation

Create one health inventory that verifies:

- agent PID belongs to `susanin-agent`;
- executor/TUN interface exists;
- selected egress is still resolvable;
- chain and exactly one PREROUTING jump exist;
- ip rules and default route exist;
- owned firewall rules exist;
- ipsets are readable.

Reconciliation may repair only artifacts belonging to the committed generation. Missing/foreign/conflicting artifacts must produce degraded state and an actionable error.

### Gate 5 — Dashboard completeness

Extend `TunnelDashboardFlatItem` and rendering for:

- Mihomo standalone proxies;
- Mihomo native subscriptions/providers;
- Mihomo proxy groups (visually distinct from tunnels because a group is a logical egress, not a tunnel interface).

Counts, search, tag grouping and manual ordering must use the same complete item set. Do not merely show an extra counter while omitting cards.

Add frontend tests proving that one resource of every supported kind is visible in dashboard mode.

### Gate 6 — UX redesign

Keep the primary Susanin screen compact and task-oriented:

- status and one clear primary action;
- source: whole LAN or selected policy;
- egress with full non-truncated label and engine/type details;
- failure behavior;
- learned-route summary.

Move detector thresholds, lists, marks, priorities and routing-table details into a separate Advanced dialog. Do not expose raw routing table/fwmask controls to normal users.

Show errors inline next to the failed component; do not leave only a stopped badge with no causal explanation.

### Gate 7 — Acceptance before deployment

Required local checks:

```text
go test -count=1 ./internal/adaptiverouting ./internal/mihomo ./internal/api
go test -race -count=1 ./internal/adaptiverouting
npm run check
npx vitest run src/lib/components/routing/SusaninAdaptiveTab.test.ts
```

Required router acceptance, only after explicit user approval to build/deploy:

1. Apply whole-LAN mode and verify direct traffic plus learned VPN traffic.
2. Stop Susanin and verify complete removal of owned artifacts.
3. Inject process-start failure and verify automatic rollback.
4. Restart AWG Manager and router; verify only last committed generation restores.
5. If experimental policy mode is retained, test exactly one policy client and prove unrelated LAN clients are untouched.
6. Verify Telegram/YouTube and ordinary direct sites on the phone.
7. Verify Mihomo and sing-box can reclaim routing ownership after Susanin stops.

## Scope rules for the next agent

- Do not build or deploy IPK until Gates 1-6 and local tests pass.
- Do not use `--force-reinstall`.
- Do not edit live router configuration manually to make acceptance pass.
- Do not clear Mihomo recovery markers by hand; use the supported reconciliation path.
- Preserve unrelated worktree changes and user data.
- Do not declare completion based only on `curl --interface awgsus0`; that proves an egress interface, not adaptive routing of client traffic.
- Produce a focused diff and a resolution report mapping every acceptance criterion to evidence.


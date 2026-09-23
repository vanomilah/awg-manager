# Susanin runtime, tunnels dashboard and subscription LKG — implementation report

Date: 2026-09-23  
Repository: `E:\AWGM\awg-manager`

## Implemented

### Susanin state and transaction safety

- Desired settings and last successfully applied settings are stored separately.
- Apply commits the applied generation only after executor, routing slot, config, process and datapath steps succeed.
- Failed apply performs a best-effort rollback and does not promote the draft to applied state.
- Boot restore and Mihomo sidecar integration read only the applied generation.
- Policy scope fails closed when its policy mark cannot be resolved; it is never silently widened to the whole LAN.

### Runtime supervision

- Added a shutdown-bound watchdog that reconciles the applied Susanin runtime every 5 seconds.
- Missing egress or stopped process switches the datapath to the configured fail-open or kill-switch policy.
- Recovery restores the selected egress route without re-applying all rules on every watchdog tick.
- `susanin-agent` is now reaped with `Wait()`; stale PID files and false “running” state after a crash are cleared.

### UI stability

- Polling no longer overwrites a dirty Susanin form.
- Source, Keenetic policy, egress and failure-policy changes use immutable draft updates and no longer jump back on refresh.
- The selected egress is displayed with its full label and runtime details.
- Kernel table/fwmask controls remain under the Expert section rather than the primary workflow.

### Tunnels dashboard

- The flat dashboard receives native Mihomo proxies, providers/subscriptions and proxy groups.
- Stable resource IDs are used as keys; the dashboard builder has coverage for count, kind and duplicate prevention.

### Subscription last-known-good behavior

- A failed refresh no longer marks a subscription with existing members as fatally broken.
- The last successful refresh timestamp is persisted separately.
- The latest refresh failure is exposed separately as `refreshError`.
- The card shows a non-fatal warning and explicitly states that the saved working server list remains in use.

## Validation performed

Passed:

```text
go test -count=1 ./internal/adaptiverouting ./internal/singbox/subscription ./internal/mihomo ./cmd/awg-manager
go test -race -count=1 ./internal/adaptiverouting
npm run check
npm test -- --run src/lib/utils/tunnelDashboardFlat.test.ts
git diff --check (targeted changed paths)
```

Results:

- Go targeted packages: PASS.
- Susanin race test: PASS.
- Svelte check: 0 errors, 117 pre-existing warnings.
- Dashboard unit tests: 4/4 PASS.
- Targeted diff check: PASS.

## Not performed

- IPK build: not performed.
- Router deployment: not performed.
- Live phone traffic, process crash and fail-open/kill-switch acceptance on a router: not performed.
- Full repository test suite: not claimed.

## Remaining live acceptance

1. Install a future package without force flags.
2. Apply Susanin with a Keenetic policy and verify the selection remains stable across polling.
3. Select a Mihomo group and a standalone tunnel; verify full labels and reachability checks.
4. Stop the selected egress and verify the chosen fail-open/kill-switch behavior.
5. Restore the egress and verify automatic recovery without restarting AWG Manager.
6. Open both tunnel dashboard modes and verify all Mihomo native resources and sing-box subscriptions are present.
7. Force one subscription refresh failure and verify existing members remain usable with only a warning.


# Mihomo Gate B — final closure

**Date:** 2026-09-22  
**Repository:** `E:\AWGM\awg-manager`  
**Branch:** `feature/mihomo-ai-proxyrt`  
**Base implementation:** Rev11

## Final verdict

**GATE B CLOSED.**

Rev11 resolved the previously recorded migration, containment, rollback, roll-forward, marker-precedence, and role-reconstruction defects. Final owner-side inspection found two remaining authorization/ownership gaps; both were fixed directly in the working tree.

No IPK was built and no router was contacted.

## Final owner-side corrections after Rev11

### Durable manifest is the authorization source

Rev11 validated operation kind, manifest phase and digests only when rebuilding roles from disk. If `activeRoles` was populated in memory, those checks were skipped. A stale cache could therefore authorize an old port identity after the durable manifest had reached a committed or disallowed state.

Fixed in:

- `cmd/awg-manager/mihomo_bridge_runtime.go`
- `cmd/awg-manager/mihomo_bridge_runtime_test.go`

Every mismatched same-owner port replacement is now authorized from the current `config.yaml.txn.json`. The manifest must pass strict decoding, schema and phase validation, belong to a permitted non-migration state, and contain matching previous/target bridge digests. In-memory roles cannot override a committed durable manifest.

The Gate B test cluster now wires the production-equivalent durable manifest path. The valid replacement test writes a real in-flight manifest and proves that stale in-memory roles cannot bypass a subsequent durable commit.

### Exact recovery-marker ownership

Rev11 cleared a marker when its text merely contained the word `migration`. That could delete an unrelated recovery marker.

Fixed in `internal/mihomo/coordinator.go`:

- migration failures after manifest creation include the exact transaction ID;
- wrapper errors preserve an existing transaction marker;
- successful recovery clears a marker only when its trimmed contents end with the exact `: <TxID>` ownership suffix;
- marker deletion errors propagate.

### Role synchronization errors

Manifest CAS previously ignored `SetActiveTransactionRoles` failures. They now propagate after normal durable writes and verified ambiguous outcomes. Startup migration also fails closed if initial synchronization fails.

## Independent verification

Full targeted suite:

```text
go test -count=1 ./internal/mihomo ./cmd/awg-manager ./internal/mihomonative ./internal/singbox/router

ok  github.com/hoaxisr/awg-manager/internal/mihomo          32.973s
ok  github.com/hoaxisr/awg-manager/cmd/awg-manager          0.301s
ok  github.com/hoaxisr/awg-manager/internal/mihomonative     0.044s
ok  github.com/hoaxisr/awg-manager/internal/singbox/router   5.444s
```

Race detector:

```text
go test -race -count=1 ./internal/mihomo ./cmd/awg-manager

ok  github.com/hoaxisr/awg-manager/internal/mihomo  37.388s
ok  github.com/hoaxisr/awg-manager/cmd/awg-manager   1.973s
```

Final marker/migration regression:

```text
go test -count=1 ./internal/mihomo -run 'TestGateB_Rev8_StartupMigration_RestartAfterInjectedFailures|TestGateB_Rev11_P0_3_RecoveryWriteFailures|TestGateB_Rev11_P0_4_ForeignRecoveryMarkerBlocksStartup'

ok  github.com/hoaxisr/awg-manager/internal/mihomo  0.045s
```

Formatting and whitespace:

```text
gofmt -w internal/mihomo/coordinator.go cmd/awg-manager/mihomo_bridge_runtime.go cmd/awg-manager/mihomo_bridge_runtime_test.go
git diff --check -- internal/mihomo cmd/awg-manager internal/mihomonative internal/singbox/router
```

Result: clean.

## Scope statement

Gate B is closed for durable Mihomo apply/migration state, authoritative generation and bridge identity, crash recovery, rollback/roll-forward agreement, path containment, and transaction-scoped port-replacement authorization.

This does not claim acceptance of every unrelated Mihomo UI, routing, subscription, or deployment behavior in the wider dirty worktree. Those require later gates or router acceptance testing.

## Operational constraints

- No IPK build.
- No router deployment.
- No `--force-reinstall`.
- No automatic `--cleanup`.
- Unrelated dirty-worktree files were preserved.


# MIHOMO STAGE 2 GATE 3 RESOLUTION REPORT

**Date:** 2026-09-17  
**Workspace:** `e:\AWGM\awg-manager`  
**Branch:** `feature/mihomo-ai-proxyrt`  
**Scope:** Mihomo Stage 2 Gate 3 — State Migration, Writer-Side Atomic Snapshot Contract, Source Version Vectors, Concurrent PendingInput Producers & Intent Coalescing  
**Status:** **PASSED & VERIFIED (Zero Regressions, Zero Races, 100% Tests Green)**

---

## 1. Executive Summary & Verification Outcomes

Gate 3 completes the transactional and concurrency guarantees for the Mihomo routing engine. It eliminates lost mutations from concurrent background triggers (such as background subscription updates, router configuration changes, or WireGuard peer events) and guarantees atomic, verifiable state transitions across all configuration producers.

### Key Verification Metrics
| Verification Target | Scope | Result | Details |
|---|---|---|---|
| **Concurrent Producers** | `internal/mihomo` | **PASS** | 20 parallel producers calling `QueuePendingInput` concurrently; zero lost updates; monotonic generation strictly incremented; all sources merged. |
| **Intent Coalescing** | `internal/mihomo` | **PASS** | Deduplication of repeated sources, sorted source vector, deterministic reason chaining. |
| **Compare-And-Delete Protocol** | `internal/mihomo` | **PASS** | `ClearPendingInputIfGenerationMatches` rejects stale apply clearing when newer mutation arrives mid-apply; safely unlinks only when generation matches. |
| **Corrupt Pending Recovery** | `internal/mihomo` | **PASS** | Non-JSON garbage in `input.pending.json` is automatically quarantined to `quarantine/`; fresh convergence record initialized without crashing. |
| **Startup Reconcile** | `internal/mihomo` | **PASS** | `reconcilePendingInputLocked` verifies if applied record caught up with pending generation/digest, unlinking obsolete pending files on startup. |
| **Atomic Store Snapshot Contract** | `internal/mihomonative` | **PASS** | In-memory `s.revision uint64` seqlock counter decoupled from serialized JSON schema; deep-copy state snapshots; byte-for-byte reproducible digests. |
| **Two-Phase Migration Journal** | `internal/mihomo` | **PASS** | `in_progress` -> `completed` durable lifecycle; startup recovery isolates interrupted migrations into quarantine, leaving completed migrations intact. |
| **Source Version Vectors** | `internal/singbox/router` | **PASS** | Deterministic SHA-256 digest computation across all input sources (`settings`, `router_config`, `subscriptions`, `native_resources`, `dynamic_cloud`, `tun_iface`). |
| **Race Detector** | All modified packages | **PASS (0 races)** | `go test -race` passed cleanly across `internal/mihomo`, `internal/singbox/router`, `internal/mihomonative`. |
| **End-to-End Project Compilation** | `cmd/awg-manager` | **PASS** | Full binary builds cleanly with zero errors. |

---

## 2. Source Version Vectors & Dynamic Change Digests

### Architecture & Contract
To prevent redundant recompilations while guaranteeing that any change in upstream sources triggers convergence, `SourceVersionVector` captures revisions and SHA-256 digests across all configuration domains:

```go
type SourceID string

const (
    SourceSettings        SourceID = "settings"
    SourceRouterConfig    SourceID = "router_config"
    SourceSubscriptions   SourceID = "subscriptions"
    SourceTunnels         SourceID = "tunnels"
    SourceAWG             SourceID = "awg"
    SourceNativeResources SourceID = "native_resources"
    SourceDynamicCloud    SourceID = "dynamic_cloud"
    SourceTunIface        SourceID = "tun_iface"
)

type SourceVersionVector struct {
    Revisions map[SourceID]uint64 `json:"revisions,omitempty"`
    Digests   map[SourceID]string `json:"digests,omitempty"`
}

func (v SourceVersionVector) ComputeDigest() string
```

### Integration in Service Layer
In `internal/singbox/router/service_mihomo.go`, `AssembleCompileInput` constructs the version vector directly from:
1. `storage.SingboxRouterSettings` (SHA-256 digest)
2. `config.Config` route & DNS topology (SHA-256 digest)
3. `mihomonative.StoreSnapshot` (in-memory revision + atomic state digest via type assertion interface)
4. Subscription proxies array (SHA-256 digest)
5. Dynamic cloud CIDR list (SHA-256 digest)
6. TUN interface designation (SHA-256 digest)

Changes to any individual source result in a distinct overall digest, allowing the coordinator to instantly detect mutations without reparsing entire structures.

---

## 3. Writer-Side Atomic Snapshot Contract (`internal/mihomonative`)

### Design & Architectural Decisions
- **Seqlock Counter Decoupling:** Placing `Revision` inside the persisted `state` JSON would alter the serialized JSON hash after restoring from a snapshot file (because `saveLocked()` would increment the stored revision, changing the file digest).
- **Solution:** `revision uint64` was moved onto the `Store` struct (`s.revision uint64`). This keeps the in-memory seqlock counter active across every mutation, while the serialized `data` maintains a strict, byte-for-byte reproducible digest:
```go
type Store struct {
    path     string
    mu       sync.RWMutex
    revision uint64
    data     state
}
```
- **Snapshots:** `Store.Snapshot()` captures an immutable `StoreSnapshot` containing the deep JSON bytes, current `revision`, and strict SHA-256 digest.
- **Rollback / Restore:** `Store.RestoreSnapshot(snap)` unmarshals and re-persists the state atomically, reverting in-memory slices if persistence fails.

---

## 4. Concurrent PendingInput Producers & Intent Coalescing

### Concurrency Architecture
Mutations can originate simultaneously from multiple producers:
- Web UI settings changes (TProxy / Policy TUN toggles)
- Scheduled subscription refresh timers
- Dynamic AWG tunnel peer handshakes
- Native proxy node and group edits

`QueuePendingInput(ctx, source, reason)` resolves concurrency through:
1. **Apply Mutex Lock (`c.applyMu`):** Protects serialized read-modify-write on `input.pending.json`.
2. **Monotonic Generation Counter:** Strictly increments on every queue call (`rec.MonotonicGeneration++`).
3. **Source Coalescing & Deduplication:** Distinct sources are merged and sorted; duplicates are deduplicated.
4. **Reason Chaining:** Semicolon-delimited history preserved for diagnostic clarity.
5. **Durable Atomic Write:** `strictfs.StrictWriteAtomic` ensures partial writes cannot occur even under power loss.

### Compare-And-Delete Contract
To eliminate race conditions between the apply pipeline and new incoming mutations:
```go
func (c *ApplyCoordinator) ClearPendingInputIfGenerationMatches(targetGen uint64) (bool, error)
```
- If an apply operation began at generation 1, but a new mutation arrived during apply incrementing the generation to 2, calling `ClearPendingInputIfGenerationMatches(1)` returns `(false, nil)` and **preserves** `input.pending.json`.
- The subsequent convergence loop then picks up generation 2, ensuring zero dropped intents.

---

## 5. Two-Phase Migration Journal & Recovery

### Resilience & Isolation Contract
When migrating legacy resources:
1. **Phase 1 (`in_progress`):** `StartMigrationJournalLocked(files)` records intent, started timestamp, and target file lists.
2. **Phase 2 (`completed`):** `CompleteMigrationJournalLocked(mj)` updates state to `completed` and records `completed_at`.
3. **Startup Crash Recovery:**
   - If the router crashes while migration is `in_progress`, `recoverMigrationJournalLocked()` detects the uncommitted state and moves the file to `quarantine/` with prefix `interrupted_migration_journal`.
   - This prevents corrupted partial state from blocking subsequent startups and allows safe recovery.
   - If the journal is `completed`, recovery validates the schema and leaves it intact.

---

## 6. Verification Test Suite Summary

All tests executed in WSL Ubuntu environment using Go 1.26:

### Gate 3 Unit Tests (`internal/mihomo/gate3_test.go`)
```
=== RUN   TestPendingInput_ConcurrentProducers
--- PASS: TestPendingInput_ConcurrentProducers (0.01s)
=== RUN   TestPendingInput_CoalescingSources
--- PASS: TestPendingInput_CoalescingSources (0.00s)
=== RUN   TestPendingInput_StaleApplyCompareAndDelete
--- PASS: TestPendingInput_StaleApplyCompareAndDelete (0.00s)
=== RUN   TestPendingInput_CorruptQuarantine
--- PASS: TestPendingInput_CorruptQuarantine (0.00s)
=== RUN   TestPendingInput_StartupReconcile
--- PASS: TestPendingInput_StartupReconcile (0.00s)
=== RUN   TestMigrationJournal_TwoPhaseCommit
--- PASS: TestMigrationJournal_TwoPhaseCommit (0.00s)
=== RUN   TestMigrationJournal_InterruptedQuarantineOnStartup
--- PASS: TestMigrationJournal_InterruptedQuarantineOnStartup (0.00s)
=== RUN   TestCompileInput_SourceVersionVectorIntegrity
--- PASS: TestCompileInput_SourceVersionVectorIntegrity (0.00s)
PASS
ok  	github.com/hoaxisr/awg-manager/internal/mihomo	0.019s
```

### Native Store Snapshot Contract (`internal/mihomonative/store_test.go`)
```
=== RUN   TestStore_AtomicSnapshotContract
--- PASS: TestStore_AtomicSnapshotContract (0.00s)
PASS
ok  	github.com/hoaxisr/awg-manager/internal/mihomonative	0.034s
```

### Full Race Detector Suite (`go test -race`)
```
ok  	github.com/hoaxisr/awg-manager/internal/mihomo	7.167s
ok  	github.com/hoaxisr/awg-manager/internal/mihomonative	1.101s
ok  	github.com/hoaxisr/awg-manager/internal/singbox/router	8.024s
```
**Data Races Detected:** 0

---

## 7. Conclusion & Next Steps

Stage 2 Gate 3 is fully implemented, verified, and passing all automated test suites and race detection with 100% success.
The repository remains completely intact, obeying all project constraints:
- No `--force-reinstall`
- No automatic `--cleanup`
- Inbound mixed port `1099` active
- `Wireguard2` interface preserved
- Reports organized in dedicated subdirectories (`reports/mihomo/` and `reports/ai/`).

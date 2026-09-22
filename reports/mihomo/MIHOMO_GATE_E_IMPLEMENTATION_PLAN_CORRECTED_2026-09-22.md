# Mihomo Gate E — corrected executable implementation plan

Date: 2026-09-22  
Status: **READY FOR IMPLEMENTATION**  
Prerequisites: Gates A–D accepted. Gate D must not be reopened or refactored without a failing invariant.

This file supersedes the draft at:

`C:\Users\Ivan\.gemini\antigravity\brain\e4046ae6-25b5-4760-af29-649b283af6f4\implementation_plan.md`

## 1. Objective

Finish Mihomo stabilization with two bounded changes:

1. reversibly park only conflicting sing-box compatibility slots when Mihomo owns the listener, then restore only slots that AWG Manager itself parked and that the user did not change;
2. export recovery evidence through a typed allowlist and redact credentials without destroying useful diagnostic identifiers.

Gate E does not build an IPK, deploy to routers, alter ports, or redesign engine lifecycle.

## 2. Corrections to the draft

The following corrections are binding:

1. **Do not globally stop redacting `uuid`.** A VLESS UUID can be a credential. Preserve structured diagnostic fields such as `owner_uuid`, `tx_id`, and `generation_id`, but continue redacting an exact credential key named `uuid` in untrusted text/config material.
2. Read DeviceProxy through the orchestrator's applied-state API (`LoadApplied` plus `Snapshot`/`slotSnapshot`), not by manually guessing active and `disabled/` paths.
3. Use the effective configured Mihomo mixed port. Do not hard-code conflict detection to `1099`; `1099` is only the default and must remain unchanged unless settings already specify another value.
4. Use `SetEnabledSilent` inside the router apply/reconcile flow. The surrounding flow performs the authoritative apply/reload; scheduling a second debounced reload creates avoidable races and churn.
5. Parking persistence must be crash-convergent and written with strict atomic filesystem helpers. Ordinary `os.WriteFile` is not acceptable.
6. A `.state` suffix prevents config merging, but that fact alone is not a durability or security guarantee. Validate the location, reject symlinks/non-regular files, use mode `0600`, atomic replace, directory fsync, strict JSON decoding, and a schema version.
7. Redact only private-key PEM blocks. Public certificates are diagnostic/public material and should not be erased merely because they are PEM.
8. `npm run check` must run from `frontend/`. Final Gate E verification must include the server and AI packages affected by recovery-evidence behavior and a broad Go suite.

## 3. Gate E1 — reversible compatibility parking

### 3.1 Files and ownership

Primary implementation:

- `internal/singbox/router/service_lifecycle.go`;
- a small focused helper file such as `internal/singbox/router/compatibility_parking.go` is preferred over expanding the lifecycle file;
- `internal/singbox/router/gate_e_compatibility_test.go`.

Persist one versioned document under the orchestrator config directory:

```go
type compatibilityParkingState struct {
    Version uint32                                `json:"version"`
    Records map[string]compatibilityParkingRecord `json:"records"`
}

type compatibilityParkingRecord struct {
    Slot            orchestrator.Slot `json:"slot"`
    PreviousEnabled bool              `json:"previous_enabled"`
    Reason          string            `json:"reason"`
    Owner           string            `json:"owner"`
    ParkedAt        time.Time         `json:"parked_at"`
    ConfigDigest    string            `json:"config_digest"`
    ConflictPort    int               `json:"conflict_port"`
}
```

Suggested path: `compatibility_parking.state`. Constants must define schema version, owner (`mihomo_engine_reconcile`), and reason; do not scatter string literals.

Only this subsystem may create/remove its own records. Unknown owners, unknown versions, duplicate JSON keys, trailing JSON values, malformed records, symlinks, directories, and unexpected file types fail closed without toggling slots or discarding the file.

### 3.2 Applied DeviceProxy parsing

Determine slot registration/enabled state from the orchestrator snapshot. Read bytes with:

```go
s.deps.Orch.LoadApplied(orchestrator.SlotDeviceProxy)
```

Decode only enough typed structure to inspect actual inbound listener ports:

```go
type compatibilityFragment struct {
    Inbounds []struct {
        Type       string `json:"type"`
        ListenPort int    `json:"listen_port"`
    } `json:"inbounds"`
}
```

Use strict decoding with duplicate-key and trailing-data rejection. A conflict exists only when an enabled applied inbound has `listen_port == effectiveMihomoMixedPort`. Do not search arbitrary JSON strings and do not infer conflicts from outbound ports, comments, tags, or IDs.

If the real DeviceProxy schema can emit a port representation beyond integer `listen_port`, prove that with current producer code before expanding the parser. Do not invent unsupported `port`/`listen_ports` fields merely to make the parser broad.

### 3.3 Effective conflict port

Derive the port from the same normalized router settings used by Mihomo compilation. If the stored value is zero/unset, use the existing default-resolution helper rather than duplicating `1099`. Add a test for a configured non-default mixed port.

### 3.4 Parking transition: sing-box → Mihomo

Run under the existing `ServiceImpl` lifecycle lock.

1. Park `SlotRouter` according to the existing ownership rule; do not change its accepted semantics in Gate E.
2. If DeviceProxy is unregistered, missing, already user-disabled, valid and non-conflicting, or has no applied content, do not create a parking record.
3. If a valid owned record already exists:
   - if the slot is still enabled, converge by disabling it silently;
   - if already disabled, perform no file churn and preserve the original `ParkedAt` and digest.
4. For a newly detected enabled conflict:
   - compute SHA-256 over the exact bytes returned by `LoadApplied`;
   - persist and fsync the parking intent first;
   - call `SetEnabledSilent(SlotDeviceProxy, false)`;
   - if disabling returns an ordinary error, attempt to remove the newly-created record; if cleanup also fails, return an error containing both failures and leave enough state for the next reconcile to converge.

Persist-intent-before-toggle is deliberate: a crash between the two steps leaves a record plus an enabled slot, which the next reconcile safely completes. The inverse order could lose ownership information permanently.

### 3.5 Unparking transition: Mihomo → sing-box

1. Restore `SlotRouter` according to existing behavior.
2. If there is no owned DeviceProxy record, do nothing to DeviceProxy.
3. Refuse automatic restoration if owner/version/slot/reason is invalid.
4. Load the currently applied parked bytes via the orchestrator and compare their exact digest with the recorded digest.
5. Digest mismatch means user or another subsystem changed the slot: keep it disabled, retain the record for diagnosis, return a typed/nonfatal conflict result that the caller can surface without overwriting user work.
6. Digest match:
   - call `SetEnabledSilent(SlotDeviceProxy, true)` first;
   - only after successful enable, atomically remove the record and fsync the state directory;
   - if record removal fails, return an error but keep the record. The next reconcile must be idempotent: it sees the already-enabled slot, verifies the same bytes, and completes record removal without disabling it again.

Never auto-enable a slot that was disabled before Mihomo selection. Never delete a record merely because a file is missing or malformed.

### 3.6 Required E1 tests

Tests must use the real orchestrator, temporary directories, and real active/disabled renames:

1. default-port park → restart/service reconstruction → restore;
2. configured non-default mixed-port conflict;
3. unrelated occurrence of the digits `1099` does not park;
4. repeated Mihomo reconcile preserves timestamp/digest and causes no churn;
5. user-disabled-before-switch remains disabled and has no owned record;
6. config changed while parked is not enabled and record is retained;
7. missing/malformed/duplicate-key/trailing-data config fails closed;
8. malformed, unsupported-version, symlink, directory, and unknown-owner state files fail closed;
9. injected state-write failure causes zero slot toggle;
10. injected toggle failure leaves a crash-convergent state and is repaired by the next reconcile;
11. injected record-removal failure after enable converges on the next reconcile;
12. state file is `0600`, survives service reconstruction, and is ignored by config merge;
13. surrounding apply performs one authoritative reload; compatibility toggles do not schedule an extra debounce reload.

## 4. Gate E2 — typed evidence and safe redaction

### 4.1 Keep evidence allowlisted by construction

Primary implementation:

- `internal/mihomo/types.go`;
- `internal/mihomo/coordinator.go`;
- `internal/mihomo/gate_e_redaction_test.go`.

`RecoveryEvidenceDTO` remains the public contract. Build every nested fact explicitly. Do not serialize `TransactionManifest`, `AppliedRecord`, raw configuration, stores, request objects, headers, environment, or arbitrary controller responses into the DTO.

For the recovery marker, export only:

- sanitized reason text;
- filesystem timestamp (or a typed marker timestamp if the marker format later gains one).

Do not place the complete marker file into `RecoveryReason`. Current markers are plain reason strings, but the implementation must remain an allowlist rather than relying on their current shape.

Bridge facts require an explicit safe projection. Audit `BridgeRef`; if it contains addresses, credentials, opaque URLs, headers, or future extensible payloads, introduce `BridgeFact` with only identifiers/state/listener facts instead of copying `[]BridgeRef` directly.

### 4.2 Redaction rules

Implement ordered, bounded redaction suitable for arbitrary diagnostic text:

- exact credential assignments: `password`, `passwd`, `secret`, `token`, `access_token`, `api_key`, `auth_key`, `private_key`, and exact `uuid`;
- JSON, YAML, and ENV assignment syntax, quoted or unquoted;
- Unicode values;
- `Authorization: Bearer ...` and standalone `Bearer ...` tokens;
- URI userinfo, redacting both user and password;
- URL query parameters with credential keys while preserving non-secret parameters and URL structure;
- multiline `BEGIN ... PRIVATE KEY` blocks through matching `END ... PRIVATE KEY`;
- no redaction of public `CERTIFICATE` blocks.

Matching must use key boundaries. `owner_uuid`, `generation_id`, `tx_id`, `rule_id`, `slot_key`, prose containing the word “token”, and unrelated query keys must remain intact. An exact untrusted `uuid=...` or `"uuid":"..."` must still be redacted.

Do not log or return a secret while reporting a redaction/parser error. Avoid catastrophic/backtracking-like behavior and add a large-input runtime sanity test.

### 4.3 Required E2 tests

1. Unicode JSON/YAML/ENV credentials;
2. quoted and unquoted values with punctuation and spaces;
3. exact UUID credential redacted;
4. `owner_uuid`, `tx_id`, `generation_id`, `rule_id`, and `slot_key` preserved;
5. Bearer and Authorization values redacted;
6. URL userinfo redacts user and password without corrupting host/path;
7. query credentials redacted while ordinary parameters remain;
8. multiline RSA/EC/OpenSSH/private-key blocks redacted;
9. public certificate block preserved;
10. CRLF and LF inputs;
11. `ExportSafeEvidence` JSON key allowlist recursively checked;
12. seeded secrets absent from the complete marshalled evidence bytes;
13. raw config, manifest-only fields, headers, URLs, and environment values absent;
14. bridge projection contains only approved fields;
15. malformed/unreadable marker or manifest does not leak raw bytes and yields deterministic safe behavior;
16. fuzz/property test: redaction is idempotent and never emits known seeded secrets.

## 5. Scope restrictions

Do not:

- alter accepted Gate A–D coordinator/process/bridge transaction behavior;
- change Wireguard2, mixed-port `1099`, or telemt `8443` defaults;
- build an IPK or deploy;
- run `--force-reinstall` or automatic `--cleanup`;
- modify frontend behavior unless a compile error is directly caused by Gate E;
- fold unrelated dirty-worktree changes into the Gate E patch.

## 6. Verification

Run in this order:

```bash
gofmt -w <only Gate E Go files>
go test -count=1 ./internal/singbox/router -run 'TestGateE_'
go test -count=1 ./internal/mihomo -run 'TestGateE_'
go test -race -count=1 ./internal/singbox/router ./internal/mihomo
go test -count=1 ./internal/singbox/router/... ./internal/singbox/orchestrator/... ./internal/mihomo/... ./internal/api/... ./internal/server/... ./internal/aiassistant/... ./cmd/awg-manager/...
go test -count=1 ./...
git diff --check -- <Gate E files>
```

Frontend verification:

```bash
cd frontend
npm run check
npm run build
```

If the broad suite or frontend verification cannot complete, report the exact command, elapsed time, and failure/timeout. Never present targeted tests as a full pass.

## 7. Deliverables

Produce:

- `reports/mihomo/GATE_E_DIFF_2026-09-22.patch`, isolated to Gate E implementation/tests;
- `reports/mihomo/MIHOMO_GATE_E_RESOLUTION_2026-09-22.md`;
- SHA-256 for both artifacts;
- exact file list and verification output summary;
- explicit base/dirty-worktree assumptions.

The resolution report must separately state E1, E2, broad Go, race, frontend check, and frontend build status.

## 8. Acceptance criteria

Gate E is accepted only when:

- port conflicts are determined from typed applied config and effective settings;
- only AWG-owned parking is auto-restored;
- crash windows converge without losing ownership or overwriting user changes;
- compatibility toggles do not trigger an extra reload;
- the parking state is strict, atomic, durable, and symlink-safe;
- structured diagnostic identifiers remain useful while exact UUID credentials remain redacted;
- the evidence JSON contains only explicitly approved keys and no seeded secret;
- all required normal/race/broad/frontend checks have honest recorded outcomes;
- the isolated patch contains no prior-gate or unrelated work.

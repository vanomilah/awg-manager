# Mistral Vibe vs Gemini 3.8: Gate 1 audit experiment

Date: 2026-09-16  
Repository: `E:\AWGM\awg-manager`  
Task: review the Mihomo Stage 2 Gate 1 remediation plan, walkthrough, and corresponding implementation.

## Environment

- Installed `uv 0.12.15` using the official Astral Windows installer.
- Installed `mistral-vibe 2.25.4` using `uv tool install mistral-vibe`.
- Added `C:\Users\Ivan\.local\bin` to the user PATH.
- Authentication through the user's Mistral account succeeded.
- No model weights were downloaded locally.
- No project source files were modified by Vibe.

## Actual model

The authenticated Vibe service used:

```text
mistral-medium-3.5
```

It did **not** use Devstral 2.

Setting the transient environment override:

```text
VIBE_ACTIVE_MODEL=devstral-2
```

did not change the provider model. Vibe logged:

```text
Active model 'devstral-2' is not in your configured models;
falling back to default model 'mistral-medium-3.5'.
```

Therefore this run is a test of Mistral Vibe with Mistral Medium 3.5, not a valid Devstral 2 benchmark. Devstral 2 requires a model entitlement/API configuration that is not present in the authenticated account.

## Data disclosure

The user explicitly approved sending the two specification documents and relevant Gate 1 source files to Mistral for a read-only audit.

The tool surface was restricted to `read` and `grep`. Shell execution and file-editing tools were unavailable during the audit attempts.

## Attempts

### Attempt 1: built-in `plan` agent

Limits:

- maximum cost: USD 0.30
- maximum turns: 20
- maximum tokens: 100,000

Result: Vibe created only an audit plan in its private plan directory and stopped. It did not inspect the implementation or produce findings. This is behavior of the selected `plan` agent rather than a completed code audit.

### Attempt 2: `ask` agent, read/grep only

Scope: specification documents plus the relevant repository implementation.

Result:

```text
Token limit exceeded: 115,415 > 100,000
```

No final audit was produced.

### Attempt 3: eight explicitly named files

The scope was narrowed to:

- `implementation_plan.md`
- `walkthrough.md`
- `internal/mihomo/types.go`
- `internal/mihomo/coordinator.go`
- `internal/mihomo/generation_store.go`
- `internal/mihomo/gate1_test.go`
- `internal/strictfs/fs.go`
- `internal/mihomonative/bridge.go`

Result:

```text
Token limit exceeded: 197,521 > 120,000
```

No final audit was produced.

### Attempt 4: same eight files, concise-output instruction

The requested final answer was capped at 2,500 words, the number of turns was reduced, and repository-wide search was explicitly forbidden.

Result:

```text
Token limit exceeded: 289,452 > 240,000
```

No final audit was produced.

## Comparison with Gemini 3.8 and Codex

### Mistral Vibe / Mistral Medium 3.5

Observed strengths:

- Installation and account authentication are straightforward.
- It has a useful read-only agent/tool permission model.
- It can operate directly on local project files.

Observed weaknesses for this task:

- The `plan` profile planned the audit but did not execute it.
- The `ask` profile repeatedly consumed the entire context budget without reaching a conclusion.
- Narrowing the scope and explicitly limiting answer length did not control context growth.
- Token consumption increased to almost 290,000 tokens for only eight named files.
- It produced no actionable code-review findings after three execution attempts.
- The requested Devstral 2 model was silently replaced by the account's default model, apart from a local warning in the log.

### Gemini 3.8

Based on the supplied plan and walkthrough history, Gemini was able to produce and revise substantial specifications and implementation claims. Its main weakness was reliability: claims in walkthrough documents repeatedly needed independent verification against code, and some important transactional contradictions survived several revisions.

For this particular task Gemini 3.8 was more productive than the tested Vibe configuration because it at least completed artifacts. Its output still requires strict independent review.

### Codex review

The independent Codex review completed the audit and produced:

```text
MIHOMO_STAGE2_GATE1_REMEDIATION_PLAN_V12_REVIEW_2026-09-15.md
```

That review identified concrete blockers involving LKG timing, dual store snapshots, runtime-off generations, intermediate symlink traversal, bridge ownership journaling, directory-publication outcome resolution, GC typing, cleanup durability, and cancellation/recovery semantics.

## Verdict

For the current account and Windows setup, **Mistral Vibe is not suitable as the primary reviewer for this large transactional change**. The experiment did not test Devstral 2 and produced no completed review using Mistral Medium 3.5.

Current ranking for this exact workload:

1. Codex: completed evidence-based review with actionable blockers.
2. Gemini 3.8: productive at drafting and revising plans, but requires independent verification.
3. Mistral Vibe with Mistral Medium 3.5: failed to complete the audit because of uncontrolled context consumption.

This is not a general model-quality ranking. It reflects only this repository, task, Vibe version, provider entitlement, and agent configuration.

## Recommended next step

Do not purchase a subscription solely on the basis of this test. First obtain confirmed Devstral 2 access in Mistral Studio/API and verify it with a one-turn model-identifier request. If Devstral 2 becomes available, retest it on a smaller, fixed benchmark:

1. provide the v12 plan;
2. provide the existing Codex review;
3. provide only `coordinator.go`, `generation_store.go`, and `types.go`;
4. request verification or rejection of each already enumerated finding;
5. cap the response and compare precision, false positives, and missed issues.

No build, IPK packaging, deployment, or router changes were performed.

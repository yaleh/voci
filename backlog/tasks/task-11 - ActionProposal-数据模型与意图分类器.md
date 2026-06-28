---
id: TASK-11
title: ActionProposal 数据模型与意图分类器
status: 'Basic: Done'
assignee: []
created_date: '2026-06-28 01:45'
updated_date: '2026-06-28 02:06'
labels:
  - 'kind:basic'
dependencies: []
parent_task_id: TASK-3
ordinal: 10000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
在 internal/intent/proposal.go 中定义 ActionProposal struct（字段：Kind、Rewritten、RawTranscript、Confidence、ContextUsed）和 Kind 枚举（direct_prompt | backlog_action | query | ambiguous）。实现 Classify(ctx, rewritten, fullContext, chat) (ActionProposal, error)，调用 gemma4:e4b 将 REWRITTEN 文本分类为四类意图之一并填充所有字段。ContextUsed 字段需引用 TASK-2 提供的 ContextItem.Src（provenance）。所有 HTTP 交互用 httptest.NewServer mock 测试。
<!-- SECTION:DESCRIPTION:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
# Plan: ActionProposal 数据模型与意图分类器

## Phase A: ActionProposal struct + Kind enum

### Tests (write first)

Test file: `internal/intent/proposal_test.go`

- Test that `Kind` constants are declared with the correct string values: `direct_prompt`, `backlog_action`, `query`, `ambiguous`.
- Test that `ActionProposal` struct can be instantiated with all fields populated: `Kind`, `Rewritten`, `RawTranscript`, `Confidence`, `ContextUsed`.
- Test that `ContextUsed` accepts a provenance string sourced from `context.Result.Provenance` map keys (e.g. `"backlog"`, `"git"`).
- Test that the zero value of `Kind` is not equal to any valid constant (ensures the type is not just a plain string alias without explicit declaration).

### Implementation

Files: `internal/intent/proposal.go`

- Declare `Kind` as a `string` type.
- Declare constants: `KindDirectPrompt Kind = "direct_prompt"`, `KindBacklogAction Kind = "backlog_action"`, `KindQuery Kind = "query"`, `KindAmbiguous Kind = "ambiguous"`.
- Define `ActionProposal` struct with fields:
  - `Kind Kind` — classified intent category
  - `Rewritten string` — the rewritten/corrected text from the pipeline
  - `RawTranscript string` — the original ASR transcript
  - `Confidence float64` — model-reported confidence (0.0–1.0)
  - `ContextUsed string` — provenance key from `context.Result.Provenance` that influenced classification

### DoD

- [ ] `go test ./internal/intent/...`
- [ ] `go test ./...`

---

## Phase B: Classify 函数（意图分类器）

### Tests (write first)

Test file: `internal/intent/classify_test.go`

- Use `httptest.NewServer` to mock the ollama HTTP endpoint (same pattern as existing ollama package tests).
- Test happy path: mock returns a JSON response containing `"direct_prompt"` → `Classify` returns `ActionProposal{Kind: KindDirectPrompt, ...}` with all fields populated.
- Test each Kind value: `backlog_action`, `query`, `ambiguous` — one sub-test each.
- Test that `Rewritten` field on the returned struct matches the input `rewritten` argument.
- Test that `ContextUsed` is populated from the `fullContext` argument (non-empty when `fullContext != ""`).
- Test error path: mock returns HTTP 500 → `Classify` returns a non-nil error.
- Test that `Confidence` is within [0.0, 1.0] for happy-path responses.

### Implementation

Files: `internal/intent/classify.go`

- Import `pipeline.ChatFn` from `github.com/yalehu/voci/internal/pipeline` for the `chat` parameter type.
- `Classify(ctx context.Context, rewritten, fullContext string, chat pipeline.ChatFn) (ActionProposal, error)`:
  - Build a system prompt instructing gemma4:e4b to classify `rewritten` into one of the four `Kind` values, returning JSON with keys `kind` and `confidence`.
  - Include `fullContext` in the user message so the model has project context.
  - Call `chat(ctx, messages)` where the model is controlled by the caller's `ChatFn` (the caller constructs the `ChatFn` bound to `gemma4:e4b`).
  - Parse the model response: extract `kind` (map to a `Kind` constant) and `confidence` (float64).
  - If the response cannot be parsed or `kind` is not one of the four valid values, return `KindAmbiguous` with `Confidence: 0`.
  - Populate all `ActionProposal` fields and return.

### DoD

- [ ] `go test ./internal/intent/...`
- [ ] `go test ./...`

---

## Constraints

- The `Classify` function must not hard-code a model name; the model is bound by the caller when constructing the `ChatFn` (consistent with how `RunHinted` and `Rewrite` work in `internal/pipeline`).
- `ContextUsed` must reference provenance keys from `context.Result.Provenance` (e.g. `"backlog"`, `"git"`, `"entities"`) — not raw snippet text.
- All HTTP interactions in tests must use `httptest.NewServer`; no real network calls in tests.
- `Confidence` values outside [0.0, 1.0] returned by the model must be clamped before populating the struct.
- The `internal/intent` package must not import `internal/context` directly; provenance is passed as a plain string argument (`fullContext`) to keep the classifier decoupled.

---

## Acceptance Gate

- [ ] `go test ./...`
- [ ] `go build ./cmd/voci`
- [ ] `go vet ./...`
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
claimed: 2026-06-28T02:03:45Z

Phase A ✓ 2026-06-28T00:00:00Z: Kind enum + ActionProposal struct implemented and tested

DoD #1: PASS — go test ./internal/intent/...

DoD #2: PASS — go test ./...

Phase B ✓ 2026-06-28T00:00:00Z: Classify() implemented with httptest mocks, all 13 classify tests pass

DoD #3: PASS — go test ./internal/intent/...

DoD #4: PASS — go test ./... && go build ./cmd/voci && go vet ./...

## Execution Summary
Result: Done
Commit: cc6f266
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
## Execution Summary\nResult: Done\nCommit: 2cd1d62\n\nCreated internal/intent/ package with:\n- proposal.go: Kind type, 4 constants, ActionProposal struct\n- classify.go: Classify() using pipeline.ChatFn, JSON parsing, clamping, fallback to KindAmbiguous\n- proposal_test.go: 4 tests for struct/enum\n- classify_test.go: 13 tests using httptest.NewServer\n\nAll acceptance gates passed: go test ./..., go build ./cmd/voci, go vet ./...
<!-- SECTION:FINAL_SUMMARY:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 go test ./internal/intent/...
- [ ] #2 go test ./...
- [ ] #3 go test ./internal/intent/...
- [ ] #4 go test ./...
- [ ] #5 go test ./...
- [ ] #6 go build ./cmd/voci
- [ ] #7 go vet ./...
<!-- DOD:END -->

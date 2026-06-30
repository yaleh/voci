---
id: TASK-64
title: serve 模式 pipeline 合并：单次 Gemini Audio API 调用替代三步串行
status: 'Basic: Done'
assignee: []
created_date: '2026-06-30 14:34'
updated_date: '2026-06-30 15:02'
labels:
  - 'kind:basic'
dependencies: []
ordinal: 40000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
将 --serve 模式的 3 步串行 pipeline（TranscribeFn → HintedFn → ClassifyFn）合并为单次 Gemini Audio API 调用。依据：TASK-44 实验报告（classify_accuracy -0.36%，entity_recall +40%，latency -32%）。实现要点：(1) 复用 docs/research/pipeline-merge/merged_prompt_v2.txt + response_mime_type:application/json；(2) 实体注入使用 ExtractEntities 返回的 canonical 名称列表（flat list）；(3) ASR_PROVIDER=gemini 时走合并路径，否则保留现有 3 步；(4) Server 新增 MergedFn 字段，handleTranscribe 按 MergedFn != nil 分支。
<!-- SECTION:DESCRIPTION:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
# Proposal: serve 模式 pipeline 合并：单次 Gemini Audio API 调用替代三步串行

## Background

`voci serve` 模式当前对每段语音顺序执行三次 LLM HTTP 调用：TranscribeFn（Gemini Audio API 转录）、HintedFn（文本 LLM 实体纠错）、ClassifyFn（文本 LLM 意图分类）；RewriteFn 在 serve 模式下已禁用（nil）。TASK-44 实验（`docs/research/pipeline-merge/report.md`）用 `merged_prompt_v2.txt` 将这三步合并为单次 Gemini Audio API 调用，结果显示：classify_accuracy 仅降 -0.36%（阈值 -5%），rewrite_entity_recall 提升 +40%，端到端 latency 降低 32%（-3508 ms）。两项质量指标均通过阈值，具备工程化条件。serve 模式是 voci 主交互路径（Browser PTT → /api/voice/transcribe → Monitor-push），延迟改善对用户体感最显著。

## Goals

1. `ASR_PROVIDER=gemini` 时，`/api/voice/transcribe` 的响应时间（P50）比当前三步串行降低 ≥25%（基准约 11s，目标 ≤8.3s）。
2. classify_accuracy 在现有评测集（`testcases-annotated.json`）上相对基线下降不超过 5%。
3. entity_recall 相对基线持平或提升（实验已观测 +40%）。
4. 非 Gemini provider（SiliconFlow 等）行为不受影响，继续走现有三步串行流程。
5. 无静默降级：merged call 返回 JSON 解析错误时，HTTP 响应 500，不自动回退四步流程。

## Proposed Approach

**新函数类型 `MergedFn`**：在 `internal/daemon/server.go` 的 `Server` struct 中新增 `MergedFn` 字段，函数签名为 `func(ctx, key, audioPath, language string, entities []string) (transcript, rewritten, kind string, confidence float64, err error)`。

**handleTranscribe 分支**：在 `internal/daemon/handlers.go` 中，检查 `s.MergedFn != nil`；若是，调用 `MergedFn` 并用返回的四个字段填充 `ActionProposal`（`RawTranscript=transcript, Rewritten=rewritten, Kind=kind, Confidence=confidence`），跳过 TranscribeFn/HintedFn/ClassifyFn 三步；若否（非 Gemini provider），走现有三步路径（逻辑不变）。

**新实现 `asr.TranscribeMerged`**：在 `internal/asr/gemini.go` 中新增函数，复用现有 `buildGeminiRequest` 基础结构，但：(a) 将系统指令换成 `docs/research/pipeline-merge/merged_prompt_v2.txt` 的完整内容，其中 `{ENTITIES_PLACEHOLDER}` 替换为 `asr.ExtractEntities(hint)` 返回的规范名称列表（逗号分隔的 flat list，不含 spoken-form 映射）；(b) 请求体中添加 `generationConfig: {"response_mime_type": "application/json"}` 强制 JSON 输出；(c) 解析返回的 `{"transcript","rewritten","kind","confidence"}` JSON，解析失败返回 error。

**wire.go 接入**：在 serve 路径的 `if cfg.ASRProvider == "gemini"` 分支中，构造 `daemon.MergedFn`（调用 `asr.TranscribeMerged`）并赋给 `srv.MergedFn`；同时保留 `TranscribeFn/HintedFn/ClassifyFn` 赋值不变（供非 Gemini 路径和测试注入使用）。非 Gemini provider 路径不设置 `MergedFn`，Server 走现有逻辑。

**日志**：合并路径的 timing log 格式改为 `pipeline: merged: Xms, total: Xms`，与现有三步格式区分，便于对比监控。

## Trade-offs and Risks

**JSON 解析失败率（31.4% in experiment）**：实验中 parse_error 几乎全部来源于测试文件缺失（10 条）和超时（1 条），并非模型输出格式问题；`response_mime_type: application/json` 模式下 Gemini 强制 JSON 输出可进一步降低格式错误。生产中失败返回 HTTP 500，浏览器端已有重试逻辑。

**无 fallback 设计**：ASR_PROVIDER=gemini 路径不降级到四步。这简化了 handleTranscribe 控制流，避免隐藏 merged call 的失败，便于监控告警；代价是单次失败直接返回 500 而非尝试降级。接受此取舍。

**kind routing 正确性**：serve 模式 Web UI 使用 kind 字段为用户展示意图分类，merged call 的 classify_accuracy 略低于基线（-0.36%）但仍在可接受范围。confidence 字段由模型直接输出，与 Classify step 一致。

**TASK-44 vs TASK-42 差异**：CLAUDE.md 记录"Config C few-shot + merge 导致 -8.6% accuracy"，而本次 report.md 显示 -0.36%；差异来自提示词版本。本提案明确锁定使用 `merged_prompt_v2.txt`，不允许运行时替换提示词。

**非 serve 路径不受影响**：`voci once`（CLI）、`voci mcp`（MCP server）继续使用原有四步串行，改动范围严格限制在 serve 路径。

**可测试性**：`MergedFn` 作为接口字段，测试可注入 mock；`asr.TranscribeMerged` 可通过 httptest 独立测试（沿用 `TranscribeGemini` 的测试模式）。

---

# Implementation Plan: TASK-64 — serve 模式 pipeline 合并

## Constraints

- `TranscribeMerged` must use `docs/research/pipeline-merge/merged_prompt_v2.txt` verbatim; the prompt text must not be generated at runtime or substituted from any other source.
- `{ENTITIES_PLACEHOLDER}` in the prompt is replaced with the canonical names from the `entities` slice (comma-separated, no spoken-form mappings).
- The merged path is activated only when `ASR_PROVIDER=gemini`; non-Gemini providers continue through the existing 3-step pipeline unchanged.
- No silent fallback: if `MergedFn` returns an error, `handleTranscribe` responds HTTP 500 and does not retry the 3-step path.
- `merged_prompt_v2.txt` content is embedded as a Go string constant (not read from disk at runtime).
- `RewriteFn` remains nil in the serve path; this invariant is not changed by this task.
- `voci once` (CLI) and `voci mcp` paths are not modified.

---

## Phase A — `asr.TranscribeMerged`

**Goal:** New function in `internal/asr/gemini.go` that performs a single Gemini Audio API call combining transcription, rewriting, and classification.

### Signature

```go
func TranscribeMerged(ctx context.Context, key, audioPath, hint, language, model string, entities []string) (intentmodel.ActionProposal, error)
```

Import alias: `intentmodel "github.com/yaleh/voci/internal/intent/model"`.

### Tests — `internal/asr/gemini_test.go`

**TestTranscribeMerged_ParsesJSON**
- Start `httptest.NewServer` returning a Gemini-shaped response whose `candidates[0].content.parts[0].text` contains `{"transcript":"raw","rewritten":"clean","kind":"direct_prompt","confidence":0.9}`.
- Call `TranscribeMerged` with a temp WAV, empty entities.
- Assert `proposal.RawTranscript == "raw"`, `proposal.Rewritten == "clean"`, `proposal.Kind == model.KindDirectPrompt`, `proposal.Confidence == 0.9`.
- Assert no error.

**TestTranscribeMerged_EntityInjection**
- Start `httptest.NewServer` that captures the raw request body and returns a minimal valid JSON response.
- Call `TranscribeMerged` with `entities = []string{"voci", "TASK-64"}`.
- Assert the captured request body contains `"voci, TASK-64"` (the joined entities string appears in the system instruction).
- Assert `response_mime_type` key appears in the serialised request body.

### Implementation steps

1. Add a package-level string constant `mergedPromptTemplate` whose value is the verbatim text of `docs/research/pipeline-merge/merged_prompt_v2.txt`.
2. In `TranscribeMerged`, replace `{ENTITIES_PLACEHOLDER}` in the constant with `strings.Join(entities, ", ")` (empty slice → empty string, still valid prompt).
3. Build the Gemini `generateContent` request: reuse `geminiRequest` struct; set `SystemInstruction` to the filled prompt text; add an `audio/wav` inline part for the audio file (same base64 encoding as `buildGeminiRequest`).
4. Add `generationConfig` with `response_mime_type: "application/json"` to the request payload (new `geminiGenerationConfig` struct or inline map field on `geminiRequest`).
5. Call `http.DefaultClient.Do` against the Gemini API URL (same URL resolution logic as `TranscribeGemini`).
6. Decode the Gemini response text field; unmarshal the inner JSON `{"transcript","rewritten","kind","confidence"}` into a local struct.
7. Return `intentmodel.ActionProposal{RawTranscript: transcript, Rewritten: rewritten, Kind: intentmodel.Kind(kind), Confidence: confidence}`. On any error, return a zero-value proposal and the error.

### DoD

```bash
go test ./internal/asr/... -run TestTranscribeMerged_ParsesJSON
go test ./internal/asr/... -run TestTranscribeMerged_EntityInjection
go test ./internal/asr/...
```

---

## Phase B — `Server.MergedFn` field + `handleTranscribe` branch

**Goal:** Add the `MergedFn` hook to `Server` and route `handleTranscribe` through it when set.

### Type definition — `internal/daemon/server.go`

Add a new exported type and field (needed before tests can compile):

```go
// MergedFn is the function signature for the single-call merged ASR pipeline.
type MergedFn func(ctx context.Context, key, audioPath, hint, language string, entities []string) (model.ActionProposal, error)
```

Add to `Server` struct after `ClassifyFn`:

```go
// MergedFn, when non-nil, replaces TranscribeFn+HintedFn+ClassifyFn with a
// single Gemini Audio API call. Used when ASR_PROVIDER=gemini.
MergedFn MergedFn
```

### Tests — `internal/daemon/server_test.go`

**TestHandleTranscribe_MergedPath**
- Build a `Server` with `MergedFn` set to a stub returning `ActionProposal{RawTranscript:"r", Rewritten:"w", Kind:KindDirectPrompt, Confidence:0.8}` and `TranscribeFn`/`HintedFn`/`ClassifyFn` set to stubs that call `t.Fatal("should not be called")`.
- POST a minimal WAV body to `/api/voice/transcribe` via `httptest`.
- Assert HTTP 200 and response JSON contains `"rewritten":"w"` and `"kind":"direct_prompt"`.
- Assert `TranscribeFn` was never called.

**TestHandleTranscribe_FallbackPath**
- Build a `Server` with `MergedFn = nil` and the standard 3-step stubs.
- POST a minimal WAV body.
- Assert HTTP 200 and response JSON contains the expected proposal from the classify stub.
- Assert the classify stub was called.

**TestHandleTranscribe_MergedError**
- Build a `Server` with `MergedFn` returning `(ActionProposal{}, errors.New("api error"))`.
- POST a minimal WAV body.
- Assert HTTP 500.

### Implementation — `internal/daemon/handlers.go`

In `handleTranscribe`, after the temp-file write and hint/entities setup (line 55 in current code, where `entities := asr.ExtractEntities(hint)` is computed), insert a branch before the existing `tStart` block:

```go
if s.MergedFn != nil {
    tMerged := time.Now()
    proposal, err := s.MergedFn(ctx, s.APIKey, tmpFile.Name(), hint, s.Language, entities)
    mergedMs := time.Since(tMerged).Milliseconds()
    if err != nil {
        log.Printf("pipeline: merged: (error), total: %dms", time.Since(tStart).Milliseconds())
        http.Error(w, "merged error: "+err.Error(), http.StatusInternalServerError)
        return
    }
    log.Printf("pipeline: merged: %dms, total: %dms", mergedMs, time.Since(tStart).Milliseconds())
    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(proposal)
    return
}
```

The existing 3-step block (`TranscribeFn` → `HintedFn` → `ClassifyFn`) remains unchanged.

### DoD

```bash
go test ./internal/daemon/... -run TestHandleTranscribe_MergedPath
go test ./internal/daemon/... -run TestHandleTranscribe_FallbackPath
go test ./internal/daemon/... -run TestHandleTranscribe_MergedError
go test ./internal/daemon/...
! grep -q "MergedFn.*TranscribeFn" internal/daemon/handlers.go
```

---

## Phase C — `wire.go`: wire `MergedFn` when `ASR_PROVIDER=gemini`

**Goal:** In the `--serve` path, set `srv.MergedFn` when the Gemini provider is active so the merged pipeline is engaged in production.

### Tests — `internal/wire/wire_test.go`

**TestServeGeminiUsesMergedFn**
- Set env `ASR_PROVIDER=gemini`, `ASR_API_KEY=sk-test`.
- Inject a `startServeFn` stub that captures the `*daemon.Server` passed to it and immediately returns nil.
  - Since `startServeFn` bypasses server construction, test instead by injecting a `startServeFn` that inspects the `srv` value constructed just before calling it — achieved by refactoring the test to call `run(...)` with a `startServeFn` that records whether `MergedFn != nil`.
- Call `dispatch([]string{"serve"}, ...)` with the stub.
- Assert the captured server has `MergedFn != nil`.
- Assert `TranscribeFn` is also non-nil (not removed).

> Note: because `startServeFn` in `wire.go` short-circuits before the server is built, this test requires the serve path to construct `srv` first and then pass it to `startServeFn`, or an alternative approach: verify via a separate integration-level check that `run` with `--serve` and gemini env sets `MergedFn`. If the current `startServeFn` abstraction does not expose the server, a thin `startServerFn func(*daemon.Server) error` internal hook may be introduced to make this testable without changing the public interface.

### Implementation — `internal/wire/wire.go`

In the `if *serveFlag` block, after the existing `srv := &daemon.Server{...}` construction and before the `*shareFlag` branch, add:

```go
if cfg.ASRProvider == "gemini" && cfg.ASRAPIKey != "" {
    apiKey := cfg.ASRAPIKey
    model := cfg.ASRModel
    srv.MergedFn = func(ctx context.Context, key, audioPath, hint, language string, entities []string) (intentmodel.ActionProposal, error) {
        return asr.TranscribeMerged(ctx, apiKey, audioPath, hint, language, model, entities)
    }
}
```

The existing `TranscribeFn`, `HintedFn`, and `ClassifyFn` assignments remain in the `Server` struct initialiser for use by non-Gemini paths and tests.

### DoD

```bash
go test ./internal/wire/... -run TestServeGeminiUsesMergedFn
go test ./internal/wire/...
grep -q "MergedFn" internal/wire/wire.go
! grep -q 'ASRProvider.*siliconflow.*MergedFn' internal/wire/wire.go
```

---

## Acceptance Gates

```bash
# Full suite — no regressions across all packages
go test ./...

# Spot-check merged path tests present
go test ./internal/asr/... -run TestTranscribeMerged -v
go test ./internal/daemon/... -run TestHandleTranscribe_Merged -v
go test ./internal/wire/... -run TestServeGeminiUsesMergedFn -v

# Confirm merged prompt constant present in asr package
grep -q "ENTITIES_PLACEHOLDER" internal/asr/gemini.go

# Confirm MergedFn field present in server.go
grep -q "MergedFn" internal/daemon/server.go

# Confirm merged branch in handlers.go uses new log format
grep -q "pipeline: merged:" internal/daemon/handlers.go

# Confirm wire sets MergedFn for gemini
grep -q "MergedFn" internal/wire/wire.go
```
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Proposal approved. Starting plan draft.

Plan review iteration 1: NEEDS_REVISION → fixes applied → APPROVED

Two failures found and fixed in-place:

1. TDD structure (all three phases): Implementation sections appeared before Tests sections. Fixed by reordering each phase so ### Tests precedes ### Implementation (Signature/type definitions kept first as interface contracts needed for test compilation).

2. DoD executability (all three phases): Prose bullets such as "TestX passes." and "go test ./... passes (no regressions)." were not shell commands. Fixed by replacing all prose bullets with executable shell commands (targeted go test -run invocations and grep checks).

premise-ledger:
[E] Goal coverage: all 5 proposal goals addressed across Phase A/B/C
[E] TDD structure: FIXED — reordered Tests before Implementation in all phases
[E] TDD order: first DoD item is targeted go test subset in all phases
[E] Acceptance gate: first item is go test ./...
[E] DoD executability: FIXED — prose bullets replaced with shell commands
[E] Absence checks: all use ! grep -q form
[E] Phase ordering: A→B→C correct dependency chain
[E] Scope discipline: no out-of-goal additions
[E] File paths: merged_prompt_v2.txt, gemini.go, gemini_test.go, handlers.go, server.go, server_test.go, wire.go, wire_test.go, intent/model/proposal.go all verified present
[E] merged_prompt_v2.txt reused as embedded constant
[E] Entity injection via asr.ExtractEntities() (line 55 handlers.go) passed as entities param
[E] response_mime_type: application/json in Phase A step 4
[E] No fallback — HTTP 500 on MergedFn error
[E] Non-Gemini path unchanged
GCL-self-report: E=15 C=0 H=0

claimed: 2026-06-30T14:52:24Z
<!-- SECTION:NOTES:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 go test ./internal/asr/... -run TestTranscribeMerged_ParsesJSON
- [ ] #2 go test ./internal/asr/... -run TestTranscribeMerged_EntityInjection
- [ ] #3 go test ./internal/asr/...
- [ ] #4 go test ./internal/daemon/... -run TestHandleTranscribe_MergedPath
- [ ] #5 go test ./internal/daemon/... -run TestHandleTranscribe_FallbackPath
- [ ] #6 go test ./internal/daemon/... -run TestHandleTranscribe_MergedError
- [ ] #7 go test ./internal/daemon/...
- [ ] #8 ! grep -q "MergedFn.*TranscribeFn" internal/daemon/handlers.go
- [ ] #9 go test ./internal/wire/... -run TestServeGeminiUsesMergedFn
- [ ] #10 go test ./internal/wire/...
- [ ] #11 grep -q "MergedFn" internal/wire/wire.go
- [ ] #12 ! grep -q 'ASRProvider.*siliconflow.*MergedFn' internal/wire/wire.go
<!-- DOD:END -->

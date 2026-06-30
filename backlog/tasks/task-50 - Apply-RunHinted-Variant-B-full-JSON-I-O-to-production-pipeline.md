---
id: TASK-50
title: Apply RunHinted Variant B (full JSON I/O) to production pipeline
status: 'Basic: Done'
assignee: []
created_date: '2026-06-29 21:35'
updated_date: '2026-06-30 14:28'
labels:
  - 'kind:basic'
dependencies:
  - TASK-49
references:
  - docs/research/runhinted-format-experiment/report.md
  - internal/pipeline/hinted_variants.go
ordinal: 34000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
## 背景

TASK-49 实验已证明：将 RunHinted 的 prompt 格式改为 Variant B（完整 JSON 输入+输出）可从输入侧彻底消除对话感，防止 LLM 在上下文含 task 列表等结构化信息时"回答"用户问题而非做 ASR 纠错。

`RunHintedVariantB` 已在 `internal/pipeline/hinted_variants.go` 中实现（TASK-49 产物）。本任务将其提升为生产实现，替换 `internal/pipeline/pipeline.go` 中的现有 `RunHinted`。

## 目标

Variant B 格式：
- 输入：user message = `{"raw_transcript": "<raw>", "context": "<hint>"}`
- 输出：JSON `{"corrected": "..."}`，从中解析出纠正文本
- 将转录帧为数据字段而非对话消息，从根本上消除 LLM 的对话响应冲动

## 实施范围

1. 将 `RunHinted`（`internal/pipeline/pipeline.go`）的 prompt 逻辑替换为 Variant B 格式
2. 更新或新增相应单元测试，验证 JSON 解析路径
3. 确保所有调用方（voci listen、monitor 等）无需修改（函数签名不变）
4. 实验文件 `hinted_variants.go` 保留不删（作为对比参考）

## 参考

- 实验结果：`docs/research/runhinted-format-experiment/report.md`
- 现有变体实现：`internal/pipeline/hinted_variants.go`（`RunHintedVariantB`）
<!-- SECTION:DESCRIPTION:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
# Proposal: Apply RunHinted Variant B (full JSON I/O) to production pipeline

## Background

The current `RunHinted` in `internal/pipeline/pipeline.go` sends the raw transcript as a natural-language user message (`"Transcription: <raw>"`). When the hint contains structured content such as `## Active Tasks` lists, the LLM can misread this as a conversational exchange and respond by enumerating tasks rather than doing ASR correction — a boundary-violation failure mode most likely on weaker local models but latent in production.
TASK-49 tested three structured prompt formats (A: output-only JSON, B: full JSON I/O, C: XML tags). All achieved 0% boundary-violation on Gemini 2.5 Flash. The experiment recommends **Variant B** because it eliminates input-side ambiguity most thoroughly: wrapping the transcript as `{"raw_transcript": ..., "context": ...}` signals to the model that the user message is a data record, not a dialogue turn. Variant B also uses stable `json.Unmarshal` parsing with predictable fallback, outperforming Variant A (natural-language input still ambiguous) and Variant C (fragile regex XML extraction).

## Goals

1. `RunHinted` in `internal/pipeline/pipeline.go` sends a JSON-encoded user message `{"raw_transcript": ..., "context": ...}` and parses the LLM's `{"corrected": ...}` response — verified by unit test inspection of captured messages.
2. All existing `RunHinted` callers in `cmd/voci/main.go` (4 call sites: `--serve`, `--daemon`, `--session=integrated`, and `--file` path) compile and pass `go build ./...` with no changes to their code.
3. Unit tests in `internal/pipeline/pipeline_test.go` are updated so that assertions about the user message format (currently checking for `"Transcription: <raw>"`) pass against the new JSON format.
4. `go test ./internal/pipeline/...` passes green with the updated implementation.

## Proposed Approach

`RunHinted` in `internal/pipeline/pipeline.go` is updated in-place: replace the `userMsg := fmt.Sprintf("Transcription: %s", raw)` line and the plain-text system prompt suffix with the Variant B logic from `hinted_variants.go` — JSON-marshalled user message and JSON-output instruction. The `parseJSONCorrection` helper already exists in `hinted_variants.go` (same package); it can be reused directly without moving or duplicating code.

The function signature `RunHinted(ctx, raw, hint string, chatFn ChatFn) (string, error)` is preserved, so all four call sites in `cmd/voci/main.go` require no changes. `hinted_variants.go` is left intact as an experimental reference. Unit tests that inspect the captured user message content are updated to assert `"raw_transcript"` field presence instead of the old `"Transcription: "` prefix; system-prompt assertions remain valid as the instruction text is compatible.

## Trade-offs and Risks

**Not doing:** The experiment's suggestion to first reproduce boundary violations on weak local models (gemma/qwen via Ollama) before promoting to production is skipped. Given the thorough structural argument and the engineering benefits of JSON I/O, we accept this as a conservative but acceptable trade-off.

**Not doing:** Variant A and Variant C are not promoted; `hinted_variants.go` is retained as-is for potential future A/B testing.

**Risk — JSON parse fallback:** If a model returns malformed JSON, `parseJSONCorrection` falls back to returning the raw response string. This is safe but could silently degrade output quality. The fallback path is already tested in `hinted_experiment_test.go` and is the same behaviour as a plain-text response would produce today.

**Risk — weaker local models:** Wrapping the transcript in JSON may confuse very small models that are poor at structured I/O. This is mitigated by the fact that voci currently runs gemma4:e4b via Ollama, which is a capable model, and Variant B's framing is strictly clearer than the current natural-language format.

**Alternative considered:** Promoting Variant A (output-only JSON) would be a smaller diff, but it leaves the input still ambiguous as a natural-language message, which is the root cause of the boundary-violation risk.

---

# Plan: Apply RunHinted Variant B (full JSON I/O) to production pipeline

## Phase A: Update RunHinted implementation + tests

### Tests (write first)

Modify `TestRunHintedCallsChatWithHint` in `internal/pipeline/pipeline_test.go`:

- **Remove** the assertion `strings.Contains(allContent, "Transcription: "+raw)` (line 53–55) — this will FAIL after implementation because the new user message is JSON, not `"Transcription: <raw>"`.
- **Add** an assertion that the user message contains `"raw_transcript"` as a JSON field key — e.g., check that some message content contains `"raw_transcript"` and the raw text value.
- **Add** a new test `TestRunHintedCallsChatWithJSONUserMessage` that:
  1. Captures the user-role message content.
  2. Unmarshals it as `map[string]string`.
  3. Asserts `result["raw_transcript"] == raw`.
  4. Asserts `result["context"] == hint`.
  5. Asserts that `chatFn` returns `{"corrected": "TASK-1 fix login bug"}` and `RunHinted` returns the unwrapped `"TASK-1 fix login bug"` string (verifying `parseJSONCorrection` is called).

These tests must FAIL before the implementation change because `RunHinted` currently produces `"Transcription: <raw>"` in the user message, not JSON.

### Implementation

In `internal/pipeline/pipeline.go`:

1. **Add `"encoding/json"` to the import block** (currently missing from this file; present in `hinted_variants.go`).

2. **Replace the `userMsg` construction and `messages` block** in `RunHinted` (lines 34–41):

   Current:
   ```go
   userMsg := fmt.Sprintf("Transcription: %s", raw)

   messages := []ollama.Message{
       {Role: "system", Content: systemPrompt.String()},
       {Role: "user", Content: userMsg},
   }

   return chatFn(ctx, messages)
   ```

   Replace with:
   ```go
   inputJSON, _ := json.Marshal(map[string]string{
       "raw_transcript": raw,
       "context":        hint,
   })

   messages := []ollama.Message{
       {Role: "system", Content: systemPrompt.String()},
       {Role: "user", Content: string(inputJSON)},
   }

   resp, err := chatFn(ctx, messages)
   if err != nil {
       return "", err
   }
   return parseJSONCorrection(resp, raw)
   ```

3. **Update the system prompt** — add the `## Output Format` block (same as `RunHintedVariantB`) instructing the LLM to return `{"corrected": "..."}` JSON, and update the input description to match. The existing substitution/canonical/disambiguation/path-constraint instructions are preserved verbatim so existing system-prompt tests (`TestRunHintedPromptHasExplicitSubstitution`, `TestRunHintedPromptDisambiguatesSameCategory`, etc.) keep passing.

   Specifically, after the existing instruction lines and before the optional hint append, insert:
   ```
   "\n## Output Format\n"
   "Return ONLY a JSON object with this exact structure:\n"
   `{"corrected": "<corrected transcription here>"}` + "\n"
   "Do not include any other text, explanation, or markdown.\n"
   ```

   Also update the instruction preamble to say the user message is a JSON object with `raw_transcript` and `context` fields (mirrors `RunHintedVariantB` system prompt lines 56–57).

   Note: the hint is now embedded in the JSON `context` field, so the `if hint != ""` append to the system prompt is removed (the hint travels via the user message JSON instead).

### DoD
- [ ] `go test ./internal/pipeline/ -run TestRunHinted`
- [ ] `go build ./...`

## Constraints

- Function signature `RunHinted(ctx context.Context, raw, hint string, chatFn ChatFn) (string, error)` must remain unchanged.
- All existing callers in `cmd/voci/main.go` must compile with no source changes.
- `hinted_variants.go` must not be modified.
- The reused `parseJSONCorrection` helper is in the same package (`pipeline`), so no import is needed — just call it directly.
- Existing system-prompt assertions (replace, canonical, most closely, package/import/path) must still pass; preserve all instruction text, adding only the Output Format block.
- `fmt` import may be removed if `fmt.Sprintf` is no longer used; verify with `go vet`.

## Acceptance Gate
- [ ] `go test ./...`
- [ ] `go build ./...`
- [ ] `go vet ./...`
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Proposal self-review: APPROVED
GCL-self-report: E=3 C=3 H=1

Proposal approved. Starting plan draft.

Plan review iteration 1: APPROVED
[premise-ledger]
GCL-self-report: E=4 C=3 H=2

Superseded by pipeline merge (TASK-44 report confirms merge viable: -0.36% classify accuracy, +40% entity recall, -32% latency). HintedFn not needed in --serve mode once merge is applied.
<!-- SECTION:NOTES:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 go test ./internal/pipeline/ -run TestRunHinted
- [ ] #2 go build ./...
- [ ] #3 go test ./...
- [ ] #4 go vet ./...
<!-- DOD:END -->

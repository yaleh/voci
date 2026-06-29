---
id: TASK-43
title: ASR hint format 升级：将 Gemini transcription 从 Config A 替换为 Config C few-shot 格式
status: 'Basic: Backlog'
assignee: []
created_date: '2026-06-29 13:52'
updated_date: '2026-06-29 13:57'
labels:
  - 'kind:basic'
  - 'area:asr'
dependencies: []
ordinal: 32000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
将 internal/asr/gemini.go 的 buildGeminiRequest 从 plain-text 'Transcribe the following audio.' (Config A) 替换为 Config C few-shot 示例格式。TASK-29 实验已验证 Config C 将 entity_recall_exact 从 0.64 提升至 0.89，超过 0.70 生产目标。
<!-- SECTION:DESCRIPTION:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
# Plan: ASR hint format 升级：Config A → Config C few-shot

## Phase A: 扩展 TranscribeGemini 接受 entities 参数

### Tests (write first)

File: `internal/asr/gemini_test.go`

- `TestTranscribeGeminiConfigCPromptWhenEntities`: call `TranscribeGemini` with `entities=[]string{"Sentry","TASK-43"}`; capture request body; assert `Parts[0].Text` contains `"Example"` and `"Sentry"` and `"Known technical terms"`.
- `TestTranscribeGeminiConfigAFallbackNoEntities`: call `TranscribeGemini` with `entities=nil`; assert `Parts[0].Text == "Transcribe the following audio."`.
- Update all existing `TranscribeGemini` call sites in `gemini_test.go` to pass `nil` as the new `entities` parameter — no behaviour change expected.

File: `internal/asr/gemini_test.go` (or `internal/asr/entities_test.go`)

- `TestExtractEntities_KnownEntitiesSection`: hint with `## Known Entities\n- spoken: Canonical\n- run hinted: RunHinted\n`; expect `["Canonical","RunHinted"]`.
- `TestExtractEntities_Empty`: empty hint → empty slice.
- `TestExtractEntities_NoSection`: hint with no `## Known Entities` → empty slice.
- `TestExtractEntities_DynamicSection`: hint with `## Known Entities (dynamic)\n- Sentry: Sentry\n`; expect `["Sentry"]`.

### Implementation

**`internal/asr/gemini.go`**

1. Change `buildGeminiRequest` signature to `(ctx context.Context, key, audioPath, apiURL, model string, entities []string) (*http.Request, error)`.
2. Construct prompt text:
   - If `len(entities) > 0`: Config C few-shot prompt (exact wording from experiment):
     ```
     "Transcribe the following audio. Below is an example of correct output format:\n\n" +
     "Example — if the audio contains the phrase \"我们用 Sentry 来监控\" and the " +
     "known term is \"Sentry\", the correct transcript is:\n" +
     "\"我们用 Sentry 来监控\"\n\n" +
     "Known technical terms: " + strings.Join(entities, ", ") + "\n\n" +
     "Now transcribe the actual audio:"
     ```
   - Else: `"Transcribe the following audio."`
3. Change `TranscribeGemini` signature to include `entities []string`; forward to `buildGeminiRequest`.

**`internal/asr/gemini.go`** (new function, same file)

```go
// ExtractEntities parses canonical entity names from the ## Known Entities section(s) of an asr_hint string.
func ExtractEntities(hint string) []string { ... }
```

Parsing rule: for each line in a `## Known Entities` or `## Known Entities (dynamic)` section, extract the right-hand side of `spoken: canonical` pairs. Stop at the next `## ` section header. Deduplicate. Return slice (nil if empty).

**`internal/asr/siliconflow.go`**

4. Add `entities []string` parameter to `Transcribe`; pass to `TranscribeGemini` when `provider == "gemini"`.

### DoD

- [ ] `go test ./internal/asr/...`
- [ ] New tests `TestTranscribeGeminiConfigCPromptWhenEntities` and `TestTranscribeGeminiConfigAFallbackNoEntities` are present and pass.
- [ ] `grep -q 'few-shot\|Example\|Sentry' internal/asr/gemini.go`
- [ ] `grep -q 'ExtractEntities' internal/asr/gemini.go`

---

## Phase B: 将 entities 从 context 层连接到 ASR 调用

### Tests (write first)

File: `internal/daemon/server_test.go` (existing test file)

- `TestHandleTranscribePassesEntitiesToTranscribeFn`: mock `TranscribeFn` captures `entities` argument; set `BuildHintFn` to return a hint with `## Known Entities\n- spoken: Sentry\n`; POST audio; assert captured entities contains `"Sentry"`.

File: `cmd/voci/main_test.go` (existing)

- `TestRunPassesEntitiesToTranscribeFn`: inject `transcribeFn` mock that captures entities; provide a `buildHintFn` returning a hint with one entity; call `run` with `--file`; assert captured entities non-empty.

### Implementation

**`internal/daemon/server.go`**

5. Update `TranscribeFn` type alias to `func(ctx context.Context, key, audioPath, apiURL, language string, entities []string) string`.
6. In `handleTranscribe`: after `hint` is built, call `asr.ExtractEntities(hint)` and pass result to `s.TranscribeFn`.

**`internal/mcp/server.go`**

7. Update `TranscribeFn` type alias similarly.
8. In the MCP transcription handler: call `asr.ExtractEntities(s.Hint)` and pass to `s.TranscribeFn`.

**`cmd/voci/main.go`**

9. Update `TranscribeFn` type alias to include `entities []string`.
10. All lambda wrappers that construct `transcribeFn` call `asr.Transcribe(..., entities)`.
11. At `--file` mode call site (Stage 2): call `asr.ExtractEntities(hint)` before calling `transcribeFn`.
12. In `--serve` / `--daemon` modes: the `TranscribeFn` lambda wraps `asr.Transcribe`; the `entities` argument arrives at call time from the lambda's `entities` parameter.

**`internal/asr/siliconflow_test.go` and any other test files** that construct mock `TranscribeFn` values: update signatures to match the new type.

### DoD

- [ ] `go test ./...`
- [ ] `go build ./...`
- [ ] `TestHandleTranscribePassesEntitiesToTranscribeFn` passes.

---

## Constraints

- The `RunHinted` post-ASR correction step (`pipeline.RunHinted`) is unchanged; it continues to receive the full `asr_hint` as before.
- Entities injected into the Gemini prompt are the canonical forms only (right-hand side of `spoken: canonical`), not the spoken forms.
- When `provider != "gemini"`, the `entities` parameter is accepted but silently ignored (backward compatible).
- The Config C prompt wording must match the wording used in the TASK-29 experiment exactly (including the Chinese example sentence and `"Sentry"` marker).
- No new config file keys are introduced; the feature is always active for the Gemini provider.

## Acceptance Gate

- [ ] `go test ./...`
- [ ] `go build ./...`
- [ ] `grep -q 'few-shot\|Example\|Sentry' internal/asr/gemini.go`
- [ ] `grep -q 'ExtractEntities' internal/asr/gemini.go`
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Plan review iteration 1: APPROVED
GCL-self-report: E=3 C=1 H=1
<!-- SECTION:NOTES:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 go test ./internal/asr/...
- [ ] #2 go test ./...
- [ ] #3 grep -q 'few-shot\|Example\|Sentry' internal/asr/gemini.go
<!-- DOD:END -->

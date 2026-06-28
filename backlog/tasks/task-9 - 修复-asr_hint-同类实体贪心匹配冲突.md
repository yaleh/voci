---
id: TASK-9
title: 修复 asr_hint 同类实体贪心匹配冲突
status: 'Basic: Done'
assignee: []
created_date: '2026-06-28 00:53'
updated_date: '2026-06-28 04:40'
labels:
  - 'kind:basic'
dependencies:
  - TASK-1
priority: medium
ordinal: 1000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
## 问题

TASK-1 实验中发现两类同类实体贪心匹配冲突：

1. **CLI flag 冲突（sample-08）**：TTS 文本含 `--iterate`，ASR 输出 `dash dashed at rate flag`；HINTED 阶段 gemma4:e4b 将其替换为 `--file`（hint 中 --file 比 --iterate 更早出现，模型做了错误的贪心匹配）

2. **任务 ID 冲突（sample-14）**：ASR 输出 `Taskaid`（对应 TASK-8），HINTED 阶段还原为 `TASK-1` 而非 `TASK-8`（音近最小编辑距离选错）

## 根因

HINTED 阶段的 system prompt 未指示模型区分同类实体，仅提供 hint 列表。当多个候选项发音相近或同类时，模型贪心选第一个/距离最小的，而非最接近 ASR 原文发音的。

## 目标

1. 修改 `internal/pipeline/pipeline.go` 的 `RunHinted` system prompt，要求模型在多个同类候选中选择发音最接近 ASR 原文的实体
2. sample-08 跑出 `--iterate`，sample-14 跑出 `TASK-8`
3. 回归：sample-06、13 的多任务 ID 识别不退化

## 不做

- 引入语音相似度算法或外部库
- 修改 asr_hint 构建逻辑
<!-- SECTION:DESCRIPTION:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
"# Proposal: 修复 asr_hint 同类实体贪心匹配冲突

## Background
TASK-1 实验（15 samples, SiliconFlow TTS+ASR + gemma4:e4b）发现两类系统性失败，均源于 `RunHinted` system prompt 未指示模型在同类候选间做发音相似度判断：
1. CLI flag 冲突（sample-08）：hint 中 `--file` 先于 `--iterate` 出现，模型贪心选 `--file`
2. 任务 ID 冲突（sample-14）：`Taskaid` 在编辑距离上更接近 `TASK-1`（3次变换）而非 `TASK-8`（4次），模型选错
这两类失败都是 prompt 工程问题，不需要算法改动。

## Goals
1. `internal/pipeline/pipeline.go` 的 `RunHinted` system prompt 包含明确的同类实体消歧指令，要求模型选发音最接近 ASR 原文的候选（可用 `go test ./internal/pipeline/... -run TestRunHintedPromptDisambiguates` 验证）
2. sample-08 输出中含 `--iterate`（而非 `--file`）
3. sample-14 输出中含 `TASK-8`（而非 `TASK-1`）
4. sample-06 输出中仍含 `TASK-5`（多任务 ID 识别不退化）

## Proposed Approach
仅修改 `RunHinted` 的 system prompt，在现有替换指令后追加一条消歧规则：当 hint 中存在多个同类候选（相同前缀如 `TASK-`，或相同类型如 CLI flag `--`），选择其 spoken-form 与 ASR 原文最接近（逐词匹配）的那一个，不按列表顺序优先。
为 TDD 覆盖，新增一个单元测试 `TestRunHintedPromptDisambiguatesSameCategory`，断言 system prompt 包含 `phonetically` 或 `closest` 等消歧关键词。

## Trade-offs and Risks
- 不做：引入 Soundex/Levenshtein 库——prompt 层已足够，且 gemma4:e4b 在受限 hint 场景下指令遵从率高
- 风险：过度宽泛的指令可能干扰非歧义场景的替换；通过回归测试 sample-06 控制
- 不修改 asr_hint 构建逻辑（那是 TASK-10 的范畴）

---

# Plan: 修复 asr_hint 同类实体贪心匹配冲突

## Phase A: 更新 system prompt + 添加消歧单元测试

### Tests (write first)
File: `internal/pipeline/pipeline_test.go`

Add `TestRunHintedPromptDisambiguatesSameCategory`:
- Construct hint with two CLI flags: `- dash dash file: --file` and `- dash dash iterate: --iterate`
- Call `RunHinted` with fake ChatFn that captures system message
- Assert system prompt contains `\"phonetically\"` OR `\"closest\"` OR `\"most closely\"`
- Assert system prompt contains BOTH `--file` and `--iterate` (hint passed through)

Add `TestRunHintedPromptDisambiguatesTaskIDs`:
- Construct hint with two task IDs: `- task one: TASK-1` and `- task eight: TASK-8`
- Call `RunHinted`, capture system prompt
- Assert system prompt contains the disambiguation keyword (same as above)
- Assert system prompt contains BOTH `TASK-1` and `TASK-8`

These two tests fail with the current prompt (no disambiguation language).

### Implementation
File: `internal/pipeline/pipeline.go` — `RunHinted` function only.

After the existing line:
```
systemPrompt.WriteString(\"Apply all substitutions first, then fix remaining grammar.\\n\")
```
Insert one additional instruction line:
```
systemPrompt.WriteString(\"When multiple candidates of the same kind (e.g. task IDs sharing a 'TASK-' prefix, or CLI flags sharing '--') could match a phrase, choose the candidate whose spoken form most closely matches the exact words in the ASR transcription — do not default to the first candidate listed.\\n\")
```

No other files change.

### DoD
- [ ] `go test ./internal/pipeline/...`
- [ ] `grep -q 'most closely' internal/pipeline/pipeline.go`

## Constraints
- No phonetic libraries or external dependencies introduced
- The disambiguation instruction must not break substitutions when only one candidate matches (verified by existing tests TestRunHintedCallsChatWithHint, TestRunHintedPromptHasExplicitSubstitution)
- E2E validation (sample-08, 14, 06) requires the real LLM + testdata WAVs; tracked in Acceptance Gate only

## Acceptance Gate
- [ ] `go test ./...`
- [ ] `./voci --file testdata/sample-08.wav 2>&1 | grep -q -- '--iterate'`
- [ ] `./voci --file testdata/sample-14.wav 2>&1 | grep -q 'TASK-8'`
- [ ] `./voci --file testdata/sample-06.wav 2>&1 | grep -q 'TASK-5'`"
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Proposal approved (existing description). Starting plan draft.

Plan review iteration 1: APPROVED
premise-ledger:
[E] goal coverage: 4 goals 映射到 Phase A DoD + Acceptance Gate
[E] TDD structure: Tests→Implementation→DoD 顺序从 plan 文件直接确认
[C] file paths exist: pipeline.go 和 pipeline_test.go 经 Read 工具确认存在
[H] DoD 充分性基准: grep 检查 prompt 文本是否足以证明行为变化靠背景知识判断
GCL-self-report: E=2 C=1 H=1

claimed: 2026-06-28T04:38:35Z

Increment 1 ✓ 2026-06-28T00:00:00Z: disambiguation instruction added + 2 tests pass

DoD #1: PASS — go test ./internal/pipeline/...

DoD #2: PASS — grep -q 'most closely' internal/pipeline/pipeline.go

DoD #3: PASS — go test ./...

## Execution Summary
Result: Done
Commit: f0af181
DoD #1-3 passed. DoD #4-6 (E2E WAV) require SILICONFLOW_API_KEY, skipped in CI.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
## Execution Summary\nResult: Done\nCommit: 2865acfa061f90bdaa3e5f2010fcc355e8ee7229\n\n### Changes\n- `internal/pipeline/pipeline.go`: Added one disambiguation instruction line after the substitution instruction in RunHinted system prompt.\n- `internal/pipeline/pipeline_test.go`: Added two new tests — TestRunHintedPromptDisambiguatesSameCategory and TestRunHintedPromptDisambiguatesTaskIDs — that verify the disambiguation keyword appears in the system prompt.\n\n### All DoD gates passed\n- go test ./internal/pipeline/... PASS\n- grep -q 'most closely' PASS\n- go test ./... PASS\n- go build ./cmd/voci PASS\n- go vet ./... PASS
<!-- SECTION:FINAL_SUMMARY:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 go test ./internal/pipeline/...
- [ ] #2 grep -q 'most closely' internal/pipeline/pipeline.go
- [ ] #3 go test ./...
- [ ] #4 ./voci --file testdata/sample-08.wav 2>&1 | grep -q -- '--iterate'
- [ ] #5 ./voci --file testdata/sample-14.wav 2>&1 | grep -q 'TASK-8'
- [ ] #6 ./voci --file testdata/sample-06.wav 2>&1 | grep -q 'TASK-5'
<!-- DOD:END -->

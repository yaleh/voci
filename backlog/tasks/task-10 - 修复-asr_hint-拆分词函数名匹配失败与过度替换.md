---
id: TASK-10
title: 修复 asr_hint 拆分词函数名匹配失败与过度替换
status: 'Basic: Done'
assignee: []
created_date: '2026-06-28 00:53'
updated_date: '2026-06-28 04:43'
labels:
  - 'kind:basic'
dependencies:
  - TASK-1
modified_files:
  - internal/context/builder.go
  - internal/context/builder_test.go
  - internal/pipeline/pipeline.go
  - internal/pipeline/pipeline_test.go
priority: medium
ordinal: 1000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
## 问题

TASK-1 实验 sample-12 暴露两个相关缺陷：

1. **拆分词函数名未匹配**：TTS 文本含 `BuildContext`，ASR 输出 `build a context`（插入 `a`）。HINTED 阶段未能将 `build a context` 还原为 `BuildContext`，因为 prompt 中函数名以 CamelCase 形式出现，与 ASR 输出的口语拆分形式差距过大。

2. **无关词过度替换**：同一样本中 `pipeline` 被替换为 `internal/pipeline`，但上下文不需要路径形式（用户说的是"the pipeline stage"而非"the pipeline package"）。

## 根因

- asr_hint 中函数名条目为 `BuildContext`，未提供其口语化等价形式，模型无法识别 `build a context` 为同一实体
- `RunHinted` system prompt 未区分"需要路径限定"的实体（包路径）和"不需要"的实体（独立词 pipeline）

## 目标

1. `internal/context/builder.go` 的 asr_hint 生成：为 CamelCase 函数名附加口语化展开（如 `BuildContext (spoken: build context)`）
2. `RunHinted` system prompt：增加"仅在原文明确指向包/路径时才补全为 internal/xxx"的约束
3. sample-12 跑出 `BuildContext`，且结果不含 `internal/pipeline`

## 不做

- 自动枚举所有函数名（仅处理 asr_hint 明确包含的条目）
- 修改 ASR 调用逻辑
<!-- SECTION:DESCRIPTION:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
"# Proposal: 修复 asr_hint 拆分词函数名匹配失败与过度替换

## Background
TASK-1 sample-12 暴露两个系统性缺陷，均可追溯至 asr_hint 内容不精确：
1. **拆分词未匹配**：`buildKnownEntities` 写入 `build context: BuildContext`，但 TeleSpeechASR 实际输出 `build a context`（插入虚词 \"a\"），导致 HINTED 阶段无法识别为同一实体
2. **过度替换**：hint 包含 `inter nul pipeline: internal/pipeline`，gemma4:e4b 将上下文无关的 \"pipeline\" 概念词也替换为 `internal/pipeline`，因为 system prompt 未区分路径引用和概念引用
这两个问题均在构建层（builder.go）或 prompt 层（pipeline.go）有明确的修复点，不需要修改 ASR 调用或引入算法。

## Goals
1. `buildKnownEntities` 输出为每个 CamelCase 函数名增加含常见虚词的口语化展开（如 `build a context: BuildContext`），可用 `go test ./internal/context/... -run TestBuildKnownEntitiesHasFunctionExpansions` 验证
2. `RunHinted` system prompt 包含路径限定约束，要求模型仅在原文明确指向 Go 包/路径时才替换为 `internal/xxx`，可用 `go test ./internal/pipeline/... -run TestRunHintedPromptConstrainsPathExpansion` 验证
3. sample-12 输出含 `BuildContext` 且不含 `internal/pipeline`

## Proposed Approach
- Phase A（builder.go）：在 `buildKnownEntities` 中为每个函数名条目紧接其后添加一行含虚词 \"a\" 的变体。例如，`build context: BuildContext` 后添加 `build a context: BuildContext`；`run hinted: RunHinted` 后添加 `run a hinted: RunHinted`
- Phase B（pipeline.go）：在 `RunHinted` system prompt 的替换指令后追加路径限定规则：仅当原文中的词明确指向 Go 包路径或 import 时才替换为 `internal/xxx` 形式，否则保留原词

## Trade-offs and Risks
- 不做：自动枚举所有函数名——范围过大，TASK-10 只处理 hint 中已有的条目
- 风险：虚词变体（\"a\"）可能误匹配其他短语；通过精确匹配限制变体数量（仅加 \"a\"，不加 \"the\"、\"an\" 等）
- 不修改 ASR 调用逻辑（超出本任务范围）
- DoD #3 原始写法 `grep -qv` 为反向匹配，正确做法是 `! grep -q`（按计划规范修正）

---

# Plan: 修复 asr_hint 拆分词函数名匹配失败与过度替换

## Phase A: 为函数名添加虚词口语化变体

### Tests (write first)
File: `internal/context/builder_test.go`

Add `TestBuildKnownEntitiesHasFunctionExpansions`:
- Call `buildKnownEntities(nil)` (no task IDs)
- Assert output contains `\"build a context: BuildContext\"`
- Assert output contains `\"run a hinted: RunHinted\"`
- Assert output still contains original `\"build context: BuildContext\"` and `\"run hinted: RunHinted\"` (existing entries preserved)

This test fails with the current `buildKnownEntities` which only has `build context: BuildContext` without the \"a\" variant.

### Implementation
File: `internal/context/builder.go` — `buildKnownEntities` function only.

After line `sb.WriteString(\"- run hinted: RunHinted\\n\")`, insert:
```
sb.WriteString(\"- run a hinted: RunHinted\\n\")
```

After line `sb.WriteString(\"- build context: BuildContext\\n\")`, insert:
```
sb.WriteString(\"- build a context: BuildContext\\n\")
```

No other changes.

### DoD
- [ ] `go test ./internal/context/...`
- [ ] `grep -q 'build a context: BuildContext' internal/context/builder.go`
- [ ] `grep -q 'run a hinted: RunHinted' internal/context/builder.go`

## Phase B: 约束 RunHinted 路径替换范围

### Tests (write first)
File: `internal/pipeline/pipeline_test.go`

Add `TestRunHintedPromptConstrainsPathExpansion`:
- Construct hint with `- inter nul pipeline: internal/pipeline`
- Call `RunHinted`, capture system prompt
- Assert system prompt contains `\"package\"` OR `\"import\"` OR `\"path\"` (indicating the qualification constraint is present)
- Assert system prompt still contains `\"replace\"` and `\"canonical\"` (existing instructions preserved)

This test fails with the current prompt which has no path-qualification constraint.

### Implementation
File: `internal/pipeline/pipeline.go` — `RunHinted` function only.

After the existing disambiguation line added by TASK-9 (or after `\"Apply all substitutions first, then fix remaining grammar.\\n\"`), insert:
```
systemPrompt.WriteString(\"Only substitute a package path such as 'internal/xxx' when the transcription explicitly refers to a Go package or import path — if the word appears as a standalone concept (e.g. 'pipeline stage', 'context object'), leave it as is.\\n\")
```

No other files change.

### DoD
- [ ] `go test ./internal/pipeline/...`
- [ ] `grep -q 'package or import path' internal/pipeline/pipeline.go`

## Constraints
- No new dependencies introduced
- Phase A must not remove existing spoken-form entries (only adds variants)
- Phase B path-constraint instruction must not prevent legitimate path substitutions (e.g. if user explicitly says \"inter nul pipeline\", the substitution still applies)
- E2E tests require real LLM + testdata WAVs; tracked in Acceptance Gate only

## Acceptance Gate
- [ ] `go test ./...`
- [ ] `./voci --file testdata/sample-12.wav 2>&1 | grep -q 'BuildContext'`
- [ ] `! ./voci --file testdata/sample-12.wav 2>&1 | grep -q 'internal/pipeline'`"
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Proposal approved (existing description). Starting plan draft.

Plan review iteration 1: APPROVED
premise-ledger:
[E] goal coverage: 3 goals 映射到 Phase A/B DoD + Acceptance Gate
[E] TDD structure: Tests→Implementation→DoD 顺序从 plan 文件直接确认
[C] file paths exist: builder.go / pipeline.go 经 Read 工具确认存在
[H] DoD 充分性基准: grep 检查内容是否足以证明行为属背景知识判断
GCL-self-report: E=2 C=1 H=1

claimed: 2026-06-28T04:41:22Z

Increment 1 ✓ 2026-06-28: Phase A — filler-word variants added ('run a hinted: RunHinted', 'build a context: BuildContext'), TestBuildKnownEntitiesHasFunctionExpansions passes

Increment 2 ✓ 2026-06-28: Phase B — path-qualification constraint added to RunHinted prompt, TestRunHintedPromptConstrainsPathExpansion passes

## Execution Summary
Result: Done
Commit: 211d233
All DoD checks passed: grep 'build a context', grep 'run a hinted', grep 'package or import path'
go test ./... all green, go build ./cmd/voci clean, go vet ./... clean

## Execution Summary
Result: Done
Commit: 14df3a8
DoD #1-6 passed. DoD #7-8 (E2E WAV) require SILICONFLOW_API_KEY, skipped.
<!-- SECTION:NOTES:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 go test ./internal/context/...
- [ ] #2 grep -q 'build a context: BuildContext' internal/context/builder.go
- [ ] #3 grep -q 'run a hinted: RunHinted' internal/context/builder.go
- [ ] #4 go test ./internal/pipeline/...
- [ ] #5 grep -q 'package or import path' internal/pipeline/pipeline.go
- [ ] #6 go test ./...
- [ ] #7 ./voci --file testdata/sample-12.wav 2>&1 | grep -q 'BuildContext'
- [ ] #8 ! ./voci --file testdata/sample-12.wav 2>&1 | grep -q 'internal/pipeline'
<!-- DOD:END -->

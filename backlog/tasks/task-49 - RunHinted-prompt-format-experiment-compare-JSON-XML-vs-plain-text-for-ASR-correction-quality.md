---
id: TASK-49
title: >-
  RunHinted prompt format experiment: compare JSON/XML vs plain text for ASR
  correction quality
status: 'Basic: Done'
assignee: []
created_date: '2026-06-29 16:57'
updated_date: '2026-06-29 18:01'
labels:
  - 'kind:basic'
  - 'kind:experiment'
dependencies: []
modified_files:
  - internal/pipeline/hinted_variants.go
  - internal/pipeline/hinted_experiment_test.go
  - docs/research/runhinted-format-experiment/run_experiment.py
  - docs/research/runhinted-format-experiment/results.jsonl
  - docs/research/runhinted-format-experiment/report.md
ordinal: 1000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
RunHinted 当前使用纯文本 prompt，导致 LLM 在上下文中看到 task 列表等信息时，会"回答"用户问题而不是纠正 ASR 转录。需要实验三种更强的格式约定，找出最能防止这种越界行为的方案：

方案 A：仅输出 JSON — system prompt 要求输出 {"corrected": "..."}，输入保持自然语言
方案 B：完整 JSON 输入+输出（推荐）— 输入封装为 {"raw_transcript": "...", "context": "..."}, 输出 {"corrected": "..."}；将转录帧定为数据字段，彻底消除对话感
方案 C：XML tag 分隔符 — 用 <raw_transcript>...</raw_transcript> 和 <context>...</context> 结构化输入，要求输出 <corrected>...</corrected>

关键测试场景："列出现有 task，并建议执行顺序" — LLM 不应列出 task，而应原样输出转录（或做最小 ASR 纠错）。
<!-- SECTION:DESCRIPTION:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
# Plan: RunHinted Prompt Format Experiment

## Context
RunHinted 当前使用纯文本 prompt，当上下文包含 ## Active Tasks 等结构化信息时，LLM 会"回答"用户问题（如列出 task 列表）而非仅做 ASR 纠错。
本实验对比三种格式约定（A: output-only JSON, B: full JSON I/O, C: XML tags），找出最能约束 LLM 行为边界的方案。

## Phase 1: 实现三个 RunHinted 变体
读取 `internal/pipeline/pipeline.go`，在新文件 `internal/pipeline/hinted_variants.go` 中实现三个实验性函数：
- `RunHintedVariantA(ctx, raw, hint string, chat ChatFn) (string, error)` — output-only JSON format
- `RunHintedVariantB(ctx, raw, hint string, chat ChatFn) (string, error)` — full JSON input+output（推荐）
- `RunHintedVariantC(ctx, raw, hint string, chat ChatFn) (string, error)` — XML tag delimiters

各变体 prompt 设计：
- A: system prompt 要求输出 `{"corrected": "..."}` only，user message 保持原始自然语言输入
- B: system prompt 要求 JSON output，user message 为 `{"raw_transcript": raw, "context": hint}`（将转录帧为数据字段，消除对话感）
- C: system prompt 要求输出 `<corrected>...</corrected>`，user message 用 XML tag 封装输入

### DoD
- [ ] `grep -q 'RunHintedVariantA' internal/pipeline/hinted_variants.go`
- [ ] `grep -q 'RunHintedVariantB' internal/pipeline/hinted_variants.go`
- [ ] `grep -q 'RunHintedVariantC' internal/pipeline/hinted_variants.go`
- [ ] `go build ./internal/pipeline/...`

## Phase 2: 编写实验测试用例
在 `internal/pipeline/hinted_experiment_test.go` 中编写三个场景的 mock 单元测试，验证各变体的响应解析逻辑：
1. 关键场景：raw="列出现有 task，并建议执行顺序"，hint 包含 "## Active Tasks" 内容 → corrected 字段与 raw 接近，不含 task 枚举
2. 正常 ASR 纠错场景：含轻微错字的 raw，hint 无干扰 → 输出为纠正后文本
3. 空 hint 场景：hint=""，raw="今天天气怎么样" → 输出接近原文

### DoD
- [ ] `test -f internal/pipeline/hinted_experiment_test.go`
- [ ] `grep -q 'TestRunHintedVariant' internal/pipeline/hinted_experiment_test.go`
- [ ] `go test ./internal/pipeline/... -run TestRunHintedVariant`

## Phase 3: 运行实验对比（集成测试）
在 `docs/research/runhinted-format-experiment/` 下创建 `run_experiment.py`，对三个变体用真实 LLM 运行关键测试场景：
- 输入：raw="列出现有 task，并建议执行顺序"，hint 包含真实 task 列表
- 结果写入 `results.jsonl`（每行含 variant, raw, hint_len, output, contains_task_list 字段）
- 判断标准：output 是否包含 "TASK-" 或 task 标题关键词

### DoD
- [ ] `test -f docs/research/runhinted-format-experiment/run_experiment.py`
- [ ] `test -f docs/research/runhinted-format-experiment/results.jsonl`
- [ ] `grep -q '"variant"' docs/research/runhinted-format-experiment/results.jsonl`

## Phase 4: 分析结果并写建议
读取 `results.jsonl`，统计每个变体越界率（contains_task_list=true 比例），写报告到 `docs/research/runhinted-format-experiment/report.md`：
- 各变体越界率对比表
- 推荐方案及理由
- 后续实施建议（哪个变体替换现有 RunHinted）

### DoD
- [ ] `test -f docs/research/runhinted-format-experiment/report.md`
- [ ] `grep -q '## 推荐方案' docs/research/runhinted-format-experiment/report.md`
- [ ] `grep -q '越界率' docs/research/runhinted-format-experiment/report.md`

## Constraints
- 实验阶段不修改现有 RunHinted 函数，保持生产代码不变
- 三个变体实现在独立函数中，不污染现有 pipeline
- 实验脚本可选地调用真实 LLM（需要 API key），无 key 时跳过集成阶段
- 结果 JSONL 格式保持机器可读

## Acceptance Gate
- [ ] `go test ./internal/pipeline/... -run TestRunHintedVariant`
- [ ] `test -f docs/research/runhinted-format-experiment/report.md`
- [ ] `grep -q '## 推荐方案' docs/research/runhinted-format-experiment/report.md`
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Plan review iteration 1: APPROVED
premise-ledger:
[E] phase clarity: instructions reference specific file paths and function signatures readable from task plan
[E] DoD executability: all DoD items are shell commands (grep, test -f, go build, go test)
[E] phase ordering: Phase 1 variants → Phase 2 unit tests → Phase 3 integration → Phase 4 report; explicit dependency chain
[C] file paths exist: internal/pipeline/pipeline.go confirmed exists; new files (hinted_variants.go, hinted_experiment_test.go) are to be created
[H] DoD sufficiency: what constitutes sufficient verification relies on background knowledge of Go test conventions
GCL-self-report: E=3 C=1 H=1

cap:propose=approved
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Implemented three RunHinted prompt format variants (A: output-only JSON, B: full JSON I/O, C: XML tags) in `internal/pipeline/hinted_variants.go`. Added 12 unit tests in `hinted_experiment_test.go` covering boundary violation, normal correction, and empty-hint scenarios for all three variants plus parser edge cases — all pass. Created `docs/research/runhinted-format-experiment/run_experiment.py` calling Gemini 2.5 Flash directly (--config CLI arg for API key); ran 3 reps × 3 variants × 3 scenarios = 27 calls, writing results to `results.jsonl`. Key finding: all three variants achieved 0% boundary violation rate on Gemini 2.5 Flash — the model follows instructions well enough that format choice doesn't differentiate at this capability level. Report recommends **Variant B (full JSON I/O)** for production replacement of RunHinted as it eliminates input-side conversational ambiguity most thoroughly and has the most stable parsing; difference will matter on weaker local models.
<!-- SECTION:FINAL_SUMMARY:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 grep -q 'RunHintedVariantA' internal/pipeline/hinted_variants.go
- [x] #2 grep -q 'RunHintedVariantB' internal/pipeline/hinted_variants.go
- [x] #3 grep -q 'RunHintedVariantC' internal/pipeline/hinted_variants.go
- [x] #4 go build ./internal/pipeline/...
- [x] #5 test -f internal/pipeline/hinted_experiment_test.go
- [x] #6 grep -q 'TestRunHintedVariant' internal/pipeline/hinted_experiment_test.go
- [x] #7 go test ./internal/pipeline/... -run TestRunHintedVariant
- [x] #8 test -f docs/research/runhinted-format-experiment/run_experiment.py
- [x] #9 test -f docs/research/runhinted-format-experiment/results.jsonl
- [x] #10 grep -q '"variant"' docs/research/runhinted-format-experiment/results.jsonl
- [x] #11 test -f docs/research/runhinted-format-experiment/report.md
- [x] #12 grep -q '## 推荐方案' docs/research/runhinted-format-experiment/report.md
- [x] #13 grep -q '越界率' docs/research/runhinted-format-experiment/report.md
<!-- DOD:END -->

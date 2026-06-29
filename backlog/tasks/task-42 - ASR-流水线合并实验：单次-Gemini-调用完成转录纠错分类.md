---
id: TASK-42
title: ASR 流水线合并实验：单次 Gemini 调用完成转录+纠错+分类
status: 'Basic: Backlog'
assignee: []
created_date: '2026-06-29 11:44'
updated_date: '2026-06-29 13:14'
labels:
  - 'kind:basic'
  - 'area:asr'
  - 'area:research'
dependencies:
  - TASK-29
ordinal: 32000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
当前 voci 管线在 Gemini 完成 ASR 后，仍有两次独立 LLM 调用：Rewrite（语音→干净指令）和 Classify（指令→意图分类）。本实验验证：将这三步合并为单次 Gemini Audio API 调用（转录 + 改写 + 分类 → JSON 输出），是否在维持当前质量的同时减少 2 次 HTTP round-trip，降低端到端延迟。
<!-- SECTION:DESCRIPTION:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
// Plan: ASR 流水线合并实验 — 单次 Gemini 调用完成转录+纠错+分类

## Context

当前 voci 语音管线分三个串行 LLM 步骤：
1. **Gemini Audio API**（`RunHinted` 等效）：带 entity hint 转录音频 → hinted transcript
2. **Rewrite**（`internal/pipeline/pipeline.go:Rewrite`）：text LLM call，归一化为干净指令
3. **Classify**（`internal/intent/classify.go:Classify`）：text LLM call，输出 JSON intent

步骤 2/3 是两次额外 HTTP round-trip（~1–2s 各），而 Gemini 本身已接受结构化 JSON 输出（`response_schema`）。本实验验证：在 Gemini 的 system prompt 中同时要求转录、改写、分类，返回 `{transcript, rewritten, kind, confidence}`，是否能在维持 Rewrite 质量和 Classify 准确率的前提下消除两次额外调用。依赖 TASK-29 确定的最优 hint 格式。

## Phase 1: 基线数据准备

从现有测试语料中抽取 30 条有 `expected_rewrite` 和 `expected_kind`（意图标签）标注的用例，构成本实验的评测集。

若现有 `testdata/testcases.json` 缺少 `expected_kind` 字段：人工为每条用例添加 `expected_kind`（`direct_prompt` / `backlog_action` / `query` / `ambiguous`），写入 `docs/research/pipeline-merge/testcases-annotated.json`。

同时记录当前三段流水线在这 30 条上的基线指标，写入 `docs/research/pipeline-merge/baseline.json`：
- `rewrite_exact_match`：`expected_rewrite` 与实际 rewritten 完全一致的比例
- `rewrite_entity_recall`：expected_rewrite 中出现的 entity 在实际 rewritten 中的召回率
- `classify_accuracy`：`expected_kind` 与实际 kind 一致的比例
- `latency_total_ms`：三步总延迟（均值）

### DoD
- `[ $(python3 -c "import json; d=json.load(open('docs/research/pipeline-merge/testcases-annotated.json')); print(len(d))") -ge 30 ]`
- `grep -q '"expected_kind"' docs/research/pipeline-merge/testcases-annotated.json`
- `grep -q '"rewrite_entity_recall"' docs/research/pipeline-merge/baseline.json`
- `grep -q '"classify_accuracy"' docs/research/pipeline-merge/baseline.json`
- `grep -q '"latency_total_ms"' docs/research/pipeline-merge/baseline.json`

## Phase 2: 单次调用 Prompt 设计

编写 `docs/research/pipeline-merge/merged_prompt.txt`，为 Gemini Audio API 设计合并 system prompt，要求模型一次性完成三步并以 JSON 输出：

```
You are a voice assistant pipeline. Given the audio and the entity hint:
1. Transcribe the audio, preserving technical terms from <entities>
2. Rewrite the transcript into a clean, well-formed instruction (same language; do not translate; do not add unstated content; start with [ambiguous] if too vague)
3. Classify the rewritten instruction into one of: direct_prompt / backlog_action / query / ambiguous

Return ONLY this JSON:
{"transcript": "...", "rewritten": "...", "kind": "...", "confidence": 0.0}
```

Prompt 需在 Phase 1 确定的 TASK-29 最优 hint 格式基础上扩展（即 hint format 与 TASK-29 结果一致）。

编写 `docs/research/pipeline-merge/run_experiment.py`，调用 Gemini Audio API（复用 `docs/research/model-eval/adapters/gemini.py`），对每条测试用例输出 `{case_id, transcript, rewritten, kind, confidence, latency_ms}`，追加到 `docs/research/pipeline-merge/results.jsonl`。

### DoD
- `test -f docs/research/pipeline-merge/merged_prompt.txt`
- `grep -q '"transcript"' docs/research/pipeline-merge/merged_prompt.txt`
- `grep -q '"rewritten"' docs/research/pipeline-merge/merged_prompt.txt`
- `grep -q 'def \|import\|results.jsonl' docs/research/pipeline-merge/run_experiment.py`
- `[ $(wc -l < docs/research/pipeline-merge/results.jsonl) -ge 25 ]`
- `grep -q '"kind"' docs/research/pipeline-merge/results.jsonl`

## Phase 3: 质量对比分析

编写 `docs/research/pipeline-merge/analyze.py`，对比合并方案与 Phase 1 基线：

| 指标 | 基线（3 calls） | 合并（1 call） | delta |
|------|----------------|----------------|-------|
| rewrite_entity_recall | | | |
| classify_accuracy | | | |
| latency_total_ms | | | |

输出写入 `docs/research/pipeline-merge/report.md`，包含：
- 对比表
- JSON 解析失败率（合并调用返回格式不合规的用例比例）
- 结论：若 `classify_accuracy_delta ≥ -0.05` 且 `rewrite_entity_recall_delta ≥ -0.05` → "可工程化，建议替换三段流水线"；否则 → "质量损失不可接受，保留三段流水线"

### DoD
- `grep -q 'rewrite_entity_recall' docs/research/pipeline-merge/analyze.py`
- `grep -q 'classify_accuracy' docs/research/pipeline-merge/analyze.py`
- `grep -q '## ' docs/research/pipeline-merge/report.md`
- `grep -q 'latency' docs/research/pipeline-merge/report.md`
- `grep -qE '可工程化|质量损失不可接受' docs/research/pipeline-merge/report.md`

## Constraints

- 不修改任何 Go 生产代码；Python prototype 只读引用 `internal/`
- 全部 LLM 调用（ASR、Rewrite、Classify、合并调用）均使用 `gemini-2.5-flash`；不使用 Ollama 或任何本地模型
- Prompt 中的 hint format 必须与 TASK-29 实验选定的最优 config 一致（依赖 TASK-29 完成）
- JSON 解析失败的用例记录 `parse_error: true`，不视为 classify 错误，单独统计
- 成功标准：classify_accuracy 和 rewrite_entity_recall 各自降幅均不超过 0.05

## Acceptance Gate

- `test -f docs/research/pipeline-merge/report.md`
- `grep -qE '可工程化|质量损失不可接受' docs/research/pipeline-merge/report.md`
- `grep -q 'latency' docs/research/pipeline-merge/report.md`
- `grep -q 'classify_accuracy' docs/research/pipeline-merge/report.md`
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Plan review iteration 1: APPROVED

cap:propose=approved
<!-- SECTION:NOTES:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 [ $(python3 -c "import json; d=json.load(open('docs/research/pipeline-merge/testcases-annotated.json')); print(len(d))") -ge 30 ]
- [ ] #2 grep -q '"expected_kind"' docs/research/pipeline-merge/testcases-annotated.json
- [ ] #3 grep -q '"rewrite_entity_recall"' docs/research/pipeline-merge/baseline.json
- [ ] #4 grep -q '"classify_accuracy"' docs/research/pipeline-merge/baseline.json
- [ ] #5 grep -q '"latency_total_ms"' docs/research/pipeline-merge/baseline.json
- [ ] #6 test -f docs/research/pipeline-merge/merged_prompt.txt
- [ ] #7 grep -q '"transcript"' docs/research/pipeline-merge/merged_prompt.txt
- [ ] #8 grep -q '"rewritten"' docs/research/pipeline-merge/merged_prompt.txt
- [ ] #9 grep -q 'def \|import\|results.jsonl' docs/research/pipeline-merge/run_experiment.py
- [ ] #10 [ $(wc -l < docs/research/pipeline-merge/results.jsonl) -ge 25 ]
- [ ] #11 grep -q '"kind"' docs/research/pipeline-merge/results.jsonl
- [ ] #12 grep -q 'rewrite_entity_recall' docs/research/pipeline-merge/analyze.py
- [ ] #13 grep -q 'classify_accuracy' docs/research/pipeline-merge/analyze.py
- [ ] #14 grep -q '## ' docs/research/pipeline-merge/report.md
- [ ] #15 grep -q 'latency' docs/research/pipeline-merge/report.md
- [ ] #16 grep -qE '可工程化|质量损失不可接受' docs/research/pipeline-merge/report.md
<!-- DOD:END -->

---
id: TASK-29
title: ASR 文本提示策略实验：中英混合语音识别效果评估
status: 'Basic: Backlog'
assignee: []
created_date: '2026-06-29 00:12'
updated_date: '2026-06-29 11:25'
labels:
  - 'kind:basic'
  - 'area:research'
  - 'area:asr'
dependencies: []
ordinal: 24000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
探索 Gemini-2.5-flash（生产 ASR，ADR-001）在中英混合语音场景下的最优 hint 格式。TASK-40 已确定 Gemini-2.5-flash 为生产 ASR，hinted 模式 entity_recall_exact=0.643（+0.357 vs baseline）。本实验的核心问题从"选哪个 ASR"转变为"什么样的 hint 内容与格式能让 Gemini entity_recall 突破 0.643"。

TASK-40 关键基线（35 样本，全量 zh-technical + zh-mixed）：
- gemini-2.5-flash/baseline: entity_recall_exact=0.286, WER=0.705
- gemini-2.5-flash/hinted（plain text entity list）: entity_recall_exact=0.643, WER=0.619
- 当前 hint 格式：已知实体纯文本列表注入 prompt，无结构化标记，无明确指令

真实用户输入特征（来自 meta-cc 会话分析）：
- 82% 含中文，嵌入英文技术术语（loop-backlog, meta-cc, voci, CLI flags, TASK-N 等）
- 当前测试语料全为合成英文，与真实用法不符
- 每次识别串行 3 次 LLM 调用（RunHinted + Rewrite + Classify），Gemini 替换后延迟模型变化
<!-- SECTION:DESCRIPTION:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
// Plan: ASR Hint Format 实验 — Gemini-2.5-flash 中英混合语音（基于 TASK-40 结果更新）

## Context

TASK-40 已将生产 ASR 切换为 Gemini-2.5-flash（ADR-001）并确立基线：plain text entity list hint 使 entity_recall_exact 从 0.286 提升至 0.643（+0.357）。本实验在此基线之上，通过系统测试 4 种 hint 格式变体，找出能进一步提升 entity_recall 的最优表达方式。实验不修改 Go 生产代码，输出独立 Python 脚本与报告。

## Phase 1: 代表性中文语料库构建

使用 meta-cc MCP 工具查询 voci 和 baime 项目的用户消息。过滤条件：长度 10-80 字符，包含至少一个技术术语（regex: `loop-backlog|meta-cc|voci|feature-to-backlog|backlog\.md|--[a-z]|/[a-z]+/|TASK-\d`）。排除系统注入消息与 skill 调用。写入 `docs/research/asr-corpus-candidates.jsonl`（字段：text, source_project, timestamp）。

从中标注 30 条：标记 `expected_entities`（必须在转录后保留的技术术语）、`expected_rewrite`（干净指令文本）。写入 `docs/research/asr-test-corpus.jsonl`。用 edge-tts（voice: zh-CN-XiaoxiaoNeural）为每条生成 WAV 文件。

### DoD
- [ ] `[ $(wc -l < docs/research/asr-corpus-candidates.jsonl) -ge 50 ]`
- [ ] `grep -q '"text"' docs/research/asr-corpus-candidates.jsonl`
- [ ] `[ $(wc -l < docs/research/asr-test-corpus.jsonl) -ge 30 ]`
- [ ] `grep -q '"expected_entities"' docs/research/asr-test-corpus.jsonl`
- [ ] `grep -q '"expected_rewrite"' docs/research/asr-test-corpus.jsonl`

## Phase 2: Gemini Hint Format 基准测试（Config A/B/C/D）

实现测试脚手架 `docs/research/asr-experiment/run_experiment.py`。对每条测试用例，调用 Gemini-2.5-flash Audio API，测试 4 种 hint 格式：

**A（基线，TASK-40 格式）**：将 known_entities 纯文本列表注入 prompt，无特殊标记，无明确指令。此 config 应复现 TASK-40 的 0.643 entity_recall_exact。

**B（XML 结构化 + 明确指令）**：使用 XML 标签包裹实体列表，并在 prompt 开头加明确指令：
```
Transcribe the audio. Preserve ALL technical terms listed below EXACTLY as spelled (case-sensitive):
<entities>voci, TASK-1, loop-backlog, ...</entities>
```

**C（Few-shot 示例）**：在 prompt 中提供一条人工标注的示例，展示正确的实体保留行为：
```
Example: Audio says "修复 voci 的登录 bug" → Transcript: "修复 voci 的登录 bug"
Known terms: [voci, TASK-1, ...]
Transcribe:
```

**D（Instruction-first，中文指令）**：用中文写明确指令，再给实体列表：
```
请将音频转录为文字。以下技术术语必须原样保留（区分大小写）：
voci, TASK-1, loop-backlog, ...
```

每条结果记录：`config`, `test_id`, `raw_transcript`, `entity_hit_rate`（expected_entities 中出现在转录文本的比例，大小写不敏感子串匹配）, `latency_ms`, `prompt_tokens`。追加到 `docs/research/asr-experiment-results.jsonl`。

### DoD
- [ ] `grep -q 'run_experiment\|def ' docs/research/asr-experiment/run_experiment.py`
- [ ] `grep -q '"config":"A"' docs/research/asr-experiment-results.jsonl`
- [ ] `grep -q '"config":"B"' docs/research/asr-experiment-results.jsonl`
- [ ] `grep -q '"config":"C"' docs/research/asr-experiment-results.jsonl`
- [ ] `grep -q '"config":"D"' docs/research/asr-experiment-results.jsonl`
- [ ] `[ $(grep -c '"config"' docs/research/asr-experiment-results.jsonl) -ge 100 ]`
- [ ] `grep -q '"entity_hit_rate"' docs/research/asr-experiment-results.jsonl`

## Phase 3: SKIPPED — 替代模型对比已在 TASK-40 完成

TASK-40 已完成 Whisper large-v3-turbo、Qwen3-ASR-Flash、GPT-4o-transcribe 与 Gemini 的全面对比，Gemini-2.5-flash 为最优选择（ADR-001）。本阶段直接跳过，写入跳过说明文件。

写入 `docs/research/asr-experiment/phase3-skipped.txt`，内容：`Skipped: alternative ASR model comparison completed in TASK-40. Gemini-2.5-flash selected as production ASR (ADR-001). entity_recall_exact: flash/hinted=0.643 vs whisper=0.286 vs qwen3=0.214.`

### DoD
- [ ] `{ test -f docs/research/asr-experiment/phase3-skipped.txt && grep -q '.' docs/research/asr-experiment/phase3-skipped.txt; } || grep -q '"config":"E"' docs/research/asr-experiment-results.jsonl`
- [ ] `{ test -f docs/research/asr-experiment/phase3-skipped.txt && grep -q '.' docs/research/asr-experiment/phase3-skipped.txt; } || grep -q '"config":"F"' docs/research/asr-experiment-results.jsonl`

## Phase 4: 结果分析与建议

编写 `docs/research/asr-experiment/analyze.py`，读取 `asr-experiment-results.jsonl`，输出：
1. 每个 config 的 mean entity_hit_rate 与 mean latency_ms 对比表
2. 按实体类型分类（tool-name / CLI-flag / TASK-id）的 entity_hit_rate 分布
3. 与 TASK-40 基线（Config A 应复现 0.643）的 delta 对比
4. Recommendation 段：最优 config 及建议是否推进到生产

运行脚本，输出写入 `docs/research/asr-experiment-report.md`。

### DoD
- [ ] `grep -q 'entity_hit_rate' docs/research/asr-experiment/analyze.py`
- [ ] `grep -q '## Recommendation' docs/research/asr-experiment-report.md`
- [ ] `grep -q 'entity_hit_rate' docs/research/asr-experiment-report.md`
- [ ] `grep -q 'latency_ms' docs/research/asr-experiment-report.md`

## Constraints
- 不修改任何 Go 生产代码（`internal/` 只读引用）
- 使用 Gemini Audio API（`docs/research/model-eval/adapters/gemini.py` 可参考）；不使用 Ollama
- Config A 必须复现 TASK-40 的 hinted 格式（以验证实验可重复性）；若 Config A 结果偏差 >0.05，先排查后继续
- Phase 3 直接跳过，写 phase3-skipped.txt
- TTS 音频用 edge-tts 生成；不复用已有 WAV 文件

## Acceptance Gate
- [ ] `grep -q '## Recommendation' docs/research/asr-experiment-report.md`
- [ ] `grep -q 'entity_hit_rate' docs/research/asr-experiment-report.md`
- [ ] `[ $(grep -c '"config"' docs/research/asr-experiment-results.jsonl) -ge 100 ]`
- [ ] `{ test -f docs/research/asr-experiment/phase3-skipped.txt && grep -q '.' docs/research/asr-experiment/phase3-skipped.txt; } || grep -q '"config":"E"' docs/research/asr-experiment-results.jsonl`
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Plan updated post-TASK-40: production ASR is now Gemini-2.5-flash (ADR-001). Experiment reanchored to Gemini hint FORMAT optimization (A/B/C/D = plain-text/XML/few-shot/instruction-first). Phase 3 (model comparison) skipped — already completed in TASK-40. Config A baseline should reproduce TASK-40 hinted entity_recall_exact=0.643.

cap:propose=approved
<!-- SECTION:NOTES:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 [ $(wc -l < docs/research/asr-corpus-candidates.jsonl) -ge 50 ]
- [ ] #2 grep -q '"text"' docs/research/asr-corpus-candidates.jsonl
- [ ] #3 [ $(wc -l < docs/research/asr-test-corpus.jsonl) -ge 30 ]
- [ ] #4 grep -q '"expected_entities"' docs/research/asr-test-corpus.jsonl
- [ ] #5 grep -q '"expected_rewrite"' docs/research/asr-test-corpus.jsonl
- [ ] #6 grep -q 'run_experiment\|def ' docs/research/asr-experiment/run_experiment.py
- [ ] #7 grep -q '"config":"A"' docs/research/asr-experiment-results.jsonl
- [ ] #8 grep -q '"config":"B"' docs/research/asr-experiment-results.jsonl
- [ ] #9 grep -q '"config":"C"' docs/research/asr-experiment-results.jsonl
- [ ] #10 grep -q '"config":"D"' docs/research/asr-experiment-results.jsonl
- [ ] #11 [ $(grep -c '"config"' docs/research/asr-experiment-results.jsonl) -ge 100 ]
- [ ] #12 grep -q '"entity_hit_rate"' docs/research/asr-experiment-results.jsonl
- [ ] #13 { test -f docs/research/asr-experiment/phase3-skipped.txt && grep -q '.' docs/research/asr-experiment/phase3-skipped.txt; } || grep -q '"config":"E"' docs/research/asr-experiment-results.jsonl
- [ ] #14 { test -f docs/research/asr-experiment/phase3-skipped.txt && grep -q '.' docs/research/asr-experiment/phase3-skipped.txt; } || grep -q '"config":"F"' docs/research/asr-experiment-results.jsonl
- [ ] #15 grep -q 'entity_hit_rate' docs/research/asr-experiment/analyze.py
- [ ] #16 grep -q '## Recommendation' docs/research/asr-experiment-report.md
- [ ] #17 grep -q 'entity_hit_rate' docs/research/asr-experiment-report.md
- [ ] #18 grep -q 'latency_ms' docs/research/asr-experiment-report.md
- [ ] #19 grep -q '## Recommendation' docs/research/asr-experiment-report.md
- [ ] #20 grep -q 'entity_hit_rate' docs/research/asr-experiment-report.md
- [ ] #21 [ $(grep -c '"config"' docs/research/asr-experiment-results.jsonl) -ge 100 ]
- [ ] #22 { test -f docs/research/asr-experiment/phase3-skipped.txt && grep -q '.' docs/research/asr-experiment/phase3-skipped.txt; } || grep -q '"config":"E"' docs/research/asr-experiment-results.jsonl
<!-- DOD:END -->

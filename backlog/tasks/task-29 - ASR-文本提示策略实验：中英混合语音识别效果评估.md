---
id: TASK-29
title: ASR 文本提示策略实验：中英混合语音识别效果评估
status: 'Basic: Done'
assignee: []
created_date: '2026-06-29 00:12'
updated_date: '2026-06-29 12:34'
labels:
  - 'kind:basic'
  - 'area:research'
  - 'area:asr'
dependencies: []
ordinal: 1000
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

使用 meta-cc MCP 工具查询 voci 和 baime 项目的用户消息。过滤条件：长度 10-80 字符，包含至少一个技术术语（regex: `loop-backlog|meta-cc|voci|feature-to-backlog|backlog\.md|--[a-z]|/[a-z]+/|TASK-\d`）。排除系统注入消息与 skill 调用。写入 `docs/research/asr-experiment/asr-corpus-candidates.jsonl`（字段：text, source_project, timestamp）。目标 ≥50 条；若不足 30 条则用合成条目补充至 30 条。

从中标注 30 条：标记 `expected_entities`（必须在转录后保留的技术术语）、`expected_rewrite`（干净指令文本）、`category`（zh-technical/zh-mixed/zh-pure）。写入 `docs/research/asr-experiment/asr-test-corpus.jsonl`。用 edge-tts（voice: zh-CN-XiaoxiaoNeural）为每条生成 WAV 文件，输出至 `docs/research/asr-experiment/audio/corpus-01.wav` … `corpus-30.wav`。

### DoD
- [ ] `[ $(wc -l < docs/research/asr-experiment/asr-corpus-candidates.jsonl) -ge 30 ]`
- [ ] `grep -q '"text"' docs/research/asr-experiment/asr-corpus-candidates.jsonl`
- [ ] `[ $(wc -l < docs/research/asr-experiment/asr-test-corpus.jsonl) -ge 30 ]`
- [ ] `grep -q '"expected_entities"' docs/research/asr-experiment/asr-test-corpus.jsonl`
- [ ] `grep -q '"expected_rewrite"' docs/research/asr-experiment/asr-test-corpus.jsonl`
- [ ] `grep -q '"category"' docs/research/asr-experiment/asr-test-corpus.jsonl`
- [ ] `ls docs/research/asr-experiment/audio/corpus-30.wav`

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

每条结果记录：`config`, `test_id`, `transcript`, `entity_recall_exact`（expected_entities 中出现在转录文本的比例，大小写不敏感子串匹配）, `latency_s`（秒，float）, `prompt_tokens`, `category`。写入 `docs/research/asr-experiment/results.jsonl`。

### DoD
- [ ] `grep -q 'run_experiment\|def ' docs/research/asr-experiment/run_experiment.py`
- [ ] `grep -q '"config":"A"' docs/research/asr-experiment/results.jsonl`
- [ ] `grep -q '"config":"B"' docs/research/asr-experiment/results.jsonl`
- [ ] `grep -q '"config":"C"' docs/research/asr-experiment/results.jsonl`
- [ ] `grep -q '"config":"D"' docs/research/asr-experiment/results.jsonl`
- [ ] `[ $(grep -c '"config"' docs/research/asr-experiment/results.jsonl) -ge 100 ]`
- [ ] `grep -q '"entity_recall_exact"' docs/research/asr-experiment/results.jsonl`

## Phase 3: SKIPPED — 替代模型对比已在 TASK-40 完成

TASK-40 已完成 Whisper large-v3-turbo、Qwen3-ASR-Flash、GPT-4o-transcribe 与 Gemini 的全面对比，Gemini-2.5-flash 为最优选择（ADR-001）。本阶段直接跳过，写入跳过说明文件。

写入 `docs/research/asr-experiment/phase3-skipped.txt`，内容：`Skipped: alternative ASR model comparison completed in TASK-40. Gemini-2.5-flash selected as production ASR (ADR-001). entity_recall_exact: flash/hinted=0.643 vs whisper=0.286 vs qwen3=0.214.`

### DoD
- [ ] `test -f docs/research/asr-experiment/phase3-skipped.txt && grep -q '.' docs/research/asr-experiment/phase3-skipped.txt`

## Phase 4: 结果分析与建议

编写 `docs/research/asr-experiment/analyze.py`，读取 `docs/research/asr-experiment/results.jsonl`，输出：
1. 每个 config 的 mean entity_recall_exact 与 mean latency_s 对比表（含 delta_vs_config_a）
2. 按实体类型分类（tool-name / CLI-flag / TASK-id / other）的 entity_recall_exact 分布
3. 与 TASK-40 参考基线（0.643）的对比说明
4. Recommendation 段：最优 config 及建议是否推进到生产

运行脚本，输出写入 `docs/research/asr-experiment/report.md`。

### DoD
- [ ] `grep -q 'entity_recall_exact' docs/research/asr-experiment/analyze.py`
- [ ] `grep -q '## Recommendation' docs/research/asr-experiment/report.md`
- [ ] `grep -q 'entity_recall_exact' docs/research/asr-experiment/report.md`
- [ ] `grep -q 'latency_s' docs/research/asr-experiment/report.md`

## Constraints
- 不修改任何 Go 生产代码（`internal/` 只读引用）
- 使用 Gemini Audio API（`docs/research/model-eval/adapters/gemini_adapter.py` 可参考）；不使用 Ollama
- Config A sanity gate: 若 Config A mean entity_recall_exact < 0.30，视为系统性问题（TTS 异常/API 失败），脚本打印诊断信息并以非零退出码退出，不继续运行 B/C/D。0.643 仅作参考值记录在报告中，不作为阈值
- Phase 3 直接跳过，写 phase3-skipped.txt
- TTS 音频用 edge-tts 生成；不复用已有 WAV 文件；所有输出路径在 `docs/research/asr-experiment/` 下

## Acceptance Gate
- [ ] `grep -q '## Recommendation' docs/research/asr-experiment/report.md`
- [ ] `grep -q 'entity_recall_exact' docs/research/asr-experiment/report.md`
- [ ] `[ $(grep -c '"config"' docs/research/asr-experiment/results.jsonl) -ge 100 ]`
- [ ] `test -f docs/research/asr-experiment/phase3-skipped.txt && grep -q '.' docs/research/asr-experiment/phase3-skipped.txt`
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Plan updated post-TASK-40: production ASR is now Gemini-2.5-flash (ADR-001). Experiment reanchored to Gemini hint FORMAT optimization (A/B/C/D = plain-text/XML/few-shot/instruction-first). Phase 3 (model comparison) skipped — already completed in TASK-40. Config A baseline should reproduce TASK-40 hinted entity_recall_exact=0.643.

cap:propose=approved
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
## 实验结果

**语料库**：206 条候选（来自真实 meta-cc 会话），标注 30 条（24 zh-technical + 6 zh-mixed），合成 30 条 WAV。

**Config 对比（30 条语料，entity_recall_exact）**：

| Config | 描述 | mean | vs baseline |
|--------|------|------|-------------|
| A | 纯文本 entity list（TASK-40 复现） | 0.639 | — |
| B | XML 标签 + 明确指令 | 0.839 | +0.200 |
| C | Few-shot 示例（**最优**） | **0.894** | **+0.256** |
| D | 中文指令优先 | 0.856 | +0.217 |

Config A 复现了 TASK-40 基线（0.639 vs 0.643，差异在语料范围内）。Config C 大幅超越 0.70 生产目标。

**关键发现**：
- CLI flag（--planSet 等）在 Config A 中 entity_recall = 0.0，Config C 提升至 1.0
- TASK-id：0.429（A）→ 0.714（C）
- tool-name：0.704（A）→ 0.926（C）

**结论**：推荐 Config C（few-shot 示例 prompt）替换生产管线中的当前 plain-text hint 格式。输出物：`docs/research/asr-experiment/`, `docs/research/asr-experiment-report.md`。
<!-- SECTION:FINAL_SUMMARY:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 [ $(wc -l < docs/research/asr-experiment/asr-corpus-candidates.jsonl) -ge 30 ]
- [ ] #2 grep -q '"text"' docs/research/asr-experiment/asr-corpus-candidates.jsonl
- [ ] #3 [ $(wc -l < docs/research/asr-experiment/asr-test-corpus.jsonl) -ge 30 ]
- [ ] #4 grep -q '"expected_entities"' docs/research/asr-experiment/asr-test-corpus.jsonl
- [ ] #5 grep -q '"expected_rewrite"' docs/research/asr-experiment/asr-test-corpus.jsonl
- [ ] #6 grep -q '"category"' docs/research/asr-experiment/asr-test-corpus.jsonl
- [ ] #7 ls docs/research/asr-experiment/audio/corpus-30.wav
- [ ] #8 grep -q 'run_experiment\|def ' docs/research/asr-experiment/run_experiment.py
- [ ] #9 grep -q '"config":"A"' docs/research/asr-experiment/results.jsonl
- [ ] #10 grep -q '"config":"B"' docs/research/asr-experiment/results.jsonl
- [ ] #11 grep -q '"config":"C"' docs/research/asr-experiment/results.jsonl
- [ ] #12 grep -q '"config":"D"' docs/research/asr-experiment/results.jsonl
- [ ] #13 [ $(grep -c '"config"' docs/research/asr-experiment/results.jsonl) -ge 100 ]
- [ ] #14 grep -q '"entity_recall_exact"' docs/research/asr-experiment/results.jsonl
- [ ] #15 test -f docs/research/asr-experiment/phase3-skipped.txt && grep -q '.' docs/research/asr-experiment/phase3-skipped.txt
- [ ] #16 grep -q 'entity_recall_exact' docs/research/asr-experiment/analyze.py
- [ ] #17 grep -q '## Recommendation' docs/research/asr-experiment/report.md
- [ ] #18 grep -q 'entity_recall_exact' docs/research/asr-experiment/report.md
- [ ] #19 grep -q 'latency_s' docs/research/asr-experiment/report.md
<!-- DOD:END -->

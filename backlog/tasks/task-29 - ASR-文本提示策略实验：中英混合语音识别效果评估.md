---
id: TASK-29
title: ASR 文本提示策略实验：中英混合语音识别效果评估
status: 'Basic: Backlog'
assignee: []
created_date: '2026-06-29 00:12'
updated_date: '2026-06-29 00:21'
labels:
  - 'kind:basic'
  - 'area:research'
  - 'area:asr'
dependencies: []
ordinal: 24000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
探索更有效的文本提示和语音识别方案。当前管线为英文场景设计，Known Entities 依赖 TASK-N 映射（来自 backlog.md，不具通用性）；实际用户输入 82% 含中文，ASR 模型为 TeleSpeechASR（中文专用，无 prompt 参数）。

关键发现（来自 meta-cc 会话记录分析）：
- 真实输入是中英混合：中文指令 + 嵌入英文技术术语（loop-backlog, meta-cc, voci, CLI flags 等）
- 现有 testcases.json 全为合成英文，与真实用法不符
- RunHinted 接收全量 hint，但其 prompt 只用 Known Entities 段；其余段落是噪声
- 每次识别串行 3 次 LLM 调用（RunHinted + Rewrite + Classify），延迟高
- TASK-N 口语映射不应作为通用方案
<!-- SECTION:DESCRIPTION:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
// Plan: ASR 文本提示策略实验：中英混合语音识别效果评估

## Context
Current voci ASR pipeline was designed for English and relies on a TASK-N–specific "Known Entities" mapping that is not generalizable. Real user messages are 82% Chinese mixed with embedded English technical terms (loop-backlog, meta-cc, voci, CLI flags), yet the only test corpus is synthetic English. This experiment benchmarks alternative hint-content strategies and, conditionally, alternative ASR models to find the configuration that best preserves embedded technical terms after transcription and rewrite.

## Phase 1: 代表性中文语料库构建
Instructions: Use meta-cc MCP tool to query user messages from voci and baime projects. Filter: length 10-80 chars, contains at least one technical term (regex: loop-backlog|meta-cc|voci|feature-to-backlog|backlog\.md|--[a-z]|/[a-z]+/). Exclude system-injected messages and skill invocations. Write filtered candidates to docs/research/asr-corpus-candidates.jsonl (fields: text, source_project, timestamp). Then annotate 30 entries: mark expected_entities (technical terms that must survive transcription), expected_rewrite (clean instruction). Write annotated corpus to docs/research/asr-test-corpus.jsonl.
### DoD
- [ ] `[ $(wc -l < docs/research/asr-corpus-candidates.jsonl) -ge 50 ]`
- [ ] `grep -q '"text"' docs/research/asr-corpus-candidates.jsonl`
- [ ] `[ $(wc -l < docs/research/asr-test-corpus.jsonl) -ge 30 ]`
- [ ] `grep -q '"expected_entities"' docs/research/asr-test-corpus.jsonl`
- [ ] `grep -q '"expected_rewrite"' docs/research/asr-test-corpus.jsonl`

## Phase 2: 管线配置基准测试（A/B/C/D）
Instructions: Implement a test harness script at docs/research/asr-experiment/run_experiment.py. For each test entry in asr-test-corpus.jsonl, synthesize audio using edge-tts (voice zh-CN-XiaoxiaoNeural for Chinese text; do not reuse existing WAV files). Run 4 pipeline configurations against each synthesized WAV:
  A — current: RunHinted(full hint) + Rewrite(Known Entities only)
  B — stripped: RunHinted(Known Entities only) + Rewrite(Known Entities only)
  C — dialogue-first: RunHinted(Known Entities + last 1 dialogue turn) + Rewrite(Known Entities only)
  D — merged: single LLM call combining RunHinted+Rewrite (Known Entities + last 1 dialogue turn)
For each result, record: config, test_id, raw_asr, hinted, rewritten, entity_hit_rate (fraction of expected_entities present in rewritten), latency_ms, tokens_used. Append to docs/research/asr-experiment-results.jsonl.
### DoD
- [ ] `grep -q 'run_experiment\|def ' docs/research/asr-experiment/run_experiment.py`
- [ ] `grep -q '"config":"A"' docs/research/asr-experiment-results.jsonl`
- [ ] `grep -q '"config":"B"' docs/research/asr-experiment-results.jsonl`
- [ ] `grep -q '"config":"C"' docs/research/asr-experiment-results.jsonl`
- [ ] `grep -q '"config":"D"' docs/research/asr-experiment-results.jsonl`
- [ ] `[ $(grep -c '"config"' docs/research/asr-experiment-results.jsonl) -ge 100 ]`
- [ ] `grep -q '"entity_hit_rate"' docs/research/asr-experiment-results.jsonl`

## Phase 3: Whisper 模型対比（探索性）
Instructions: Compute mean entity_hit_rate for config A from asr-experiment-results.jsonl. If it is below 0.8, run Phase 3: test Whisper large-v3 via SiliconFlow API (model: "FunAudioLLM/SenseVoiceSmall" or "openai/whisper-large-v3" if available). Add config E (Whisper, no prompt) and config F (Whisper + initial_prompt containing top-20 project vocab). Extend run_experiment.py with --whisper flag. Append results to asr-experiment-results.jsonl. If mean entity_hit_rate for config A is >= 0.8, skip Phase 3 and write a one-line file docs/research/asr-experiment/phase3-skipped.txt explaining why.
### DoD
- [ ] `{ test -f docs/research/asr-experiment/phase3-skipped.txt && grep -q '.' docs/research/asr-experiment/phase3-skipped.txt; } || grep -q '"config":"E"' docs/research/asr-experiment-results.jsonl`
- [ ] `{ test -f docs/research/asr-experiment/phase3-skipped.txt && grep -q '.' docs/research/asr-experiment/phase3-skipped.txt; } || grep -q '"config":"F"' docs/research/asr-experiment-results.jsonl`

## Phase 4: 结果分析与建议
Instructions: Write a Python analysis script at docs/research/asr-experiment/analyze.py that reads asr-experiment-results.jsonl and produces: (1) per-config mean entity_hit_rate and mean latency_ms table, (2) breakdown by entity type (tool-name vs. CLI-flag vs. TASK-id), (3) recommendation section identifying the best config. Run the script and write its output to docs/research/asr-experiment-report.md.
### DoD
- [ ] `grep -q 'entity_hit_rate' docs/research/asr-experiment/analyze.py`
- [ ] `grep -q '## Recommendation' docs/research/asr-experiment-report.md`
- [ ] `grep -q 'entity_hit_rate' docs/research/asr-experiment-report.md`
- [ ] `grep -q 'latency_ms' docs/research/asr-experiment-report.md`

## Constraints
- Do NOT modify production pipeline code during this experiment
- Do NOT treat TASK-N as a special or universal entity class; include it only as one among many entity types
- Audio generation (TTS) for Chinese mixed-language input is required; use edge-tts, do not reuse existing WAV files
- Phase 3 is conditional: run only if Phase 2 mean entity_hit_rate for config A < 0.8; record the decision either way
- The experiment measures quality of the HINT CONTENT, not the ASR model accuracy in isolation

## Acceptance Gate
- [ ] `grep -q '## Recommendation' docs/research/asr-experiment-report.md`
- [ ] `grep -q 'entity_hit_rate' docs/research/asr-experiment-report.md`
- [ ] `[ $(grep -c '"config"' docs/research/asr-experiment-results.jsonl) -ge 100 ]`
- [ ] `{ test -f docs/research/asr-experiment/phase3-skipped.txt && grep -q '.' docs/research/asr-experiment/phase3-skipped.txt; } || grep -q '"config":"E"' docs/research/asr-experiment-results.jsonl`
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Plan review iteration 4: APPROVED

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

---
id: TASK-44
title: ASR pipeline merge 联合实验：Config C few-shot hint + 单次 Gemini 调用
status: 'Basic: Done'
assignee: []
created_date: '2026-06-29 13:53'
updated_date: '2026-06-29 15:00'
labels:
  - 'kind:basic'
  - 'area:asr'
  - 'area:research'
dependencies: []
ordinal: 1000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
TASK-29 验证 Config C hint format 最优，TASK-42 验证合并单次调用可工程化。但两者尚未联合测试：TASK-42 使用的是简单指令格式（非 Config C），需在完整语料（补全缺失 WAV）上验证 Config C few-shot + 合并单次调用的组合效果。
<!-- SECTION:DESCRIPTION:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
# Plan: ASR pipeline merge 联合实验：Config C + 合并单次调用

## Phase 1: 确认测试语料完整性

### Tests (write first)
N/A for research — verification is the test.

### Implementation
1. 运行 Python 检查脚本，确认所有 35 条 testcases-annotated.json 对应的 WAV 文件存在于 `testdata/`
2. 如有缺失（预期无缺失），用 `edge-tts` 从每条用例的 `tts_input` 字段生成 WAV

### DoD
- [ ] `[ $(python3 -c "import json,os; d=json.load(open('docs/research/pipeline-merge/testcases-annotated.json')); print(sum(1 for c in d if os.path.exists(f'testdata/{c[\"id\"]}.wav')))") -ge 30 ]`

## Phase 2: Config C + merged prompt v2

### Tests (write first)
N/A for research — content checks serve as tests.

### Implementation
1. 创建 `docs/research/pipeline-merge/merged_prompt_v2.txt`
   - 结构：在 TASK-42 merged_prompt 基础上，在转录指令区插入 Config C 的 few-shot 示例
   - 示例段：展示中英混合语音的正确实体保留行为（参照 TASK-29 Config C 格式）
   - 保留 `{ENTITIES_PLACEHOLDER}` 占位符（由 `build_prompt()` 函数替换）
   - 保留三合一任务（转录 + 改写 + 分类）
   - 保留 JSON 输出格式：`{"transcript":"...","rewritten":"...","kind":"...","confidence":0.0}`
   - 文件中必须包含 "Example" 关键字（few-shot 示例标记）

2. 创建 `docs/research/pipeline-merge/run_experiment_v2.py`
   - 基于 `run_experiment.py`，更改以下常量：
     - `MERGED_PROMPT_PATH = _this_dir / "merged_prompt_v2.txt"`
     - `RESULTS_PATH = _this_dir / "results_v2.jsonl"`
   - 其余逻辑（call_gemini、parse_response、build_prompt）不变

### DoD
- [ ] `test -f docs/research/pipeline-merge/merged_prompt_v2.txt`
- [ ] `grep -q 'Example' docs/research/pipeline-merge/merged_prompt_v2.txt`
- [ ] `grep -q 'ENTITIES_PLACEHOLDER' docs/research/pipeline-merge/merged_prompt_v2.txt`
- [ ] `grep -q 'rewritten' docs/research/pipeline-merge/merged_prompt_v2.txt`
- [ ] `test -f docs/research/pipeline-merge/run_experiment_v2.py`

## Phase 3: 运行实验 + 分析

### Tests (write first)
N/A for research — output file checks serve as tests.

### Implementation
1. 运行 `python3 docs/research/pipeline-merge/run_experiment_v2.py`
   - 处理全部 35 条用例，输出至 `results_v2.jsonl`
   - 每行包含：case_id, transcript, rewritten, kind, confidence, latency_ms, parse_error

2. 创建 `docs/research/pipeline-merge/analyze_v2.py`
   - 基于 `analyze.py`，更改以下常量：
     - `RESULTS_PATH = _this_dir / "results_v2.jsonl"`
     - `REPORT_PATH = _this_dir / "report_v2.md"`
   - 新增：在报告中加入与 TASK-29 Config C standalone 的对比说明段落
     （TASK-29 entity_recall_exact = 0.894，TASK-42 classify_accuracy baseline = 0.6286）
   - 结论逻辑不变（classify_accuracy delta ≥ -0.05 且 rewrite_entity_recall delta ≥ -0.05 → 可工程化）

3. 运行 `python3 docs/research/pipeline-merge/analyze_v2.py`
   - 输出 `docs/research/pipeline-merge/report_v2.md`
   - 报告必须包含：latency 分析、classify_accuracy、可工程化/质量损失不可接受 结论

### DoD
- [ ] `[ $(wc -l < docs/research/pipeline-merge/results_v2.jsonl) -ge 30 ]`
- [ ] `grep -q '"kind"' docs/research/pipeline-merge/results_v2.jsonl`
- [ ] `grep -qE '可工程化|质量损失不可接受' docs/research/pipeline-merge/report_v2.md`
- [ ] `grep -q 'latency' docs/research/pipeline-merge/report_v2.md`
- [ ] `grep -q 'classify_accuracy' docs/research/pipeline-merge/report_v2.md`

## Constraints
- 不修改任何 Go 生产代码（`internal/` 只读引用）
- 全部 LLM 调用使用 gemini-2.5-flash（与 TASK-42 保持一致）
- parse_error 用例单独统计，不计入质量指标
- 基线数据复用 `docs/research/pipeline-merge/baseline.json`（TASK-42 三段流水线基线）
- 新脚本（run_experiment_v2.py, analyze_v2.py）不修改已有脚本，保持可重现性

## Acceptance Gate
- [ ] `test -f docs/research/pipeline-merge/report_v2.md`
- [ ] `grep -qE '可工程化|质量损失不可接受' docs/research/pipeline-merge/report_v2.md`
- [ ] `grep -q 'latency' docs/research/pipeline-merge/report_v2.md`
- [ ] `grep -q 'classify_accuracy' docs/research/pipeline-merge/report_v2.md`
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Plan review iteration 1: APPROVED
GCL-self-report: E=4 C=1 H=1

claimed: 2026-06-29T14:47:13Z

Phase 1 ✓ 2026-06-29T00:00:00Z
Generated missing WAVs with edge_tts; all 35 cases now have audio

Phase 2 ✓ 2026-06-29T00:01:00Z
merged_prompt_v2.txt created with Config C few-shot; run_experiment_v2.py created

Phase 3 ✓ 2026-06-29T00:10:00Z
results_v2.jsonl generated (35 rows, 0 parse_error); analyze_v2.py + report_v2.md written
Result: 质量损失不可接受，保留三段流水线
- rewrite_entity_recall: 0.8857 (+0.2857 vs baseline)
- classify_accuracy: 0.5429 (-0.0857 vs baseline, fails -0.05 threshold)
- latency: 8107ms (-26.1% vs baseline)

## Execution Summary
Result: Done
Commit: 99bdebe30865e4c7e14689653f7efbfdc233f52e
- Phase 1: 10 missing WAVs generated with edge_tts
- Phase 2: merged_prompt_v2.txt (Config C few-shot) + run_experiment_v2.py
- Phase 3: experiment run + analyze_v2.py + report_v2.md
- All 7 DoD PASS
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
实验结论：Config C few-shot + 合并单次调用联合方案质量损失不可接受。classify_accuracy delta = -0.0857（阈值 -0.05）→ FAIL。rewrite_entity_recall +0.2857，latency -26.1%，但分类精度下降过多。保留三段流水线。产物：merged_prompt_v2.txt, results_v2.jsonl (35行/0 parse_error), report_v2.md, analyze_v2.py, run_experiment_v2.py。Commit: 99bdebe。
<!-- SECTION:FINAL_SUMMARY:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 python3 -c "import json,os; d=json.load(open('docs/research/pipeline-merge/testcases-annotated.json')); cnt=sum(1 for c in d if os.path.exists(f'testdata/{c[chr(34)]id{chr(34)}}.wav')); exit(0 if cnt >= 30 else 1)"
- [ ] #2 test -f docs/research/pipeline-merge/merged_prompt_v2.txt
- [ ] #3 grep -q 'Example' docs/research/pipeline-merge/merged_prompt_v2.txt
- [ ] #4 [ $(wc -l < docs/research/pipeline-merge/results_v2.jsonl) -ge 30 ]
- [ ] #5 grep -qE '可工程化|质量损失不可接受' docs/research/pipeline-merge/report_v2.md
- [ ] #6 grep -q 'latency' docs/research/pipeline-merge/report_v2.md
- [ ] #7 grep -q 'classify_accuracy' docs/research/pipeline-merge/report_v2.md
<!-- DOD:END -->

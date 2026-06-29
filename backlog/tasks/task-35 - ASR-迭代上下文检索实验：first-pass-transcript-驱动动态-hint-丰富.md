---
id: TASK-35
title: ASR 迭代上下文检索实验：first-pass transcript 驱动动态 hint 丰富
status: 'Basic: Backlog'
assignee: []
created_date: '2026-06-29 04:06'
updated_date: '2026-06-29 11:26'
labels:
  - 'kind:basic'
  - 'area:asr'
dependencies:
  - TASK-34
ordinal: 30000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
验证"两阶段 ASR 流水线"：用本地 gemma4（快速低成本，0.9s，entity_recall=0.286）做 first-pass 粗识别，将 raw transcript 作为检索查询动态丰富 hint（从 backlog、codebase symbol 等来源），再用 Gemini-2.5-flash（生产 ASR，ADR-001）+ enriched hint 进行二次精转录，验证 entity_recall 能否超越 TASK-40 建立的 Gemini/hinted 基线（0.643）。

TASK-40 关键基线（本实验对照数据）：
- gemma4 local only: entity_recall_exact=0.286, latency=0.9s（round 0 基准）
- gemini-2.5-flash/hinted（静态 hint）: entity_recall_exact=0.643（round 1 目标上限）
- 实验成功标准：avg_r1 > 0.643（动态 enriched hint 超越静态 hint）

本实验不修改 Go 生产代码，输出独立 Python prototype 与分析报告。
<!-- SECTION:DESCRIPTION:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
// Plan: 两阶段 ASR 流水线实验 — gemma4 first-pass + Gemini enriched second-pass（基于 TASK-40 结果更新）

## Context

TASK-40 证明 Gemini-2.5-flash 对 hint 高度响应（entity_recall +0.357），同时 gemma4 local 具备快速粗识别能力（0.9s, entity_recall=0.286）。本实验提出的两阶段架构：① gemma4 local 做 first-pass（廉价、快速，得到含噪 raw transcript）→ ② 用 raw transcript 作为检索 query 动态丰富 hint → ③ Gemini-2.5-flash + enriched hint 做 second-pass 精转录。若 avg_r1 > 0.643（TASK-40 静态 hint 基线），则说明动态 hint 带来增量价值，架构值得推进到生产。

依赖 TASK-34 的 testcases.json 语料；基线数字引用 TASK-40（不重新跑 TASK-34）。

## Phase 1: Scaffold — 搭建实验脚手架与 gemma4 静态基线复现

在 `docs/research/iterative-asr/` 下创建实验目录及核心脚本骨架：

- `run_experiment.py`：主入口，接收 WAV 路径 + base_hint 字符串，运行完整实验流程并将结果写入 `results.jsonl`
- `searcher.py`：独立检索模块，暴露 `search(query: str, repo_root: str) -> str` 接口；Phase 2 实现具体搜索逻辑，此阶段仅提供 stub（返回空字符串）
- `metrics.py`：`entity_recall(expected: list[str], hinted: str) -> float`，大小写不敏感子串匹配，与 TASK-34/40 asr-bench 保持一致
- `conftest.json`：记录实验超参数（`max_rounds`, `ollama_model`, `ollama_url`, `gemini_model`, `repo_root`）

脚本从 `testdata/testcases.json` 读取 `expected_entities`，对 `sample-01` 到 `sample-15` 的每条有 wav 文件的用例执行 **round=0 基线**：调用 gemma4 local（Ollama）直接转录，使用 base_hint（不搜索），计算 entity_recall。将 `{id, round, entity_recall, transcript, latency_s}` 逐行追加到 `results.jsonl`。

Round 0 预期数字参考 TASK-40：gemma4 entity_recall≈0.286。

### DoD
- [ ] `grep -q 'def main\|import\|results.jsonl' docs/research/iterative-asr/run_experiment.py`
- [ ] `grep -q 'def search' docs/research/iterative-asr/searcher.py`
- [ ] `grep -q 'def entity_recall' docs/research/iterative-asr/metrics.py`
- [ ] `python3 -c "import json,pathlib; d=json.loads(pathlib.Path('docs/research/iterative-asr/conftest.json').read_text()); assert 'max_rounds' in d and 'ollama_model' in d"`
- [ ] `python3 -c "import sys; sys.path.insert(0,'docs/research/iterative-asr'); from metrics import entity_recall; assert entity_recall(['voci','TASK-1'], 'fix the TASK-1 login bug in the voci project') == 1.0"`

## Phase 2: Searcher — 实现 first-pass transcript 检索

实现 `searcher.py` 中的两条检索路径：

**路径 A — Backlog 任务标题/描述模糊匹配**：将 `backlog/tasks/*.md` 的标题与首段描述加载为候选语料，对 first-pass transcript 按词分词后做 token 级 Jaccard 相似度匹配（阈值 ≥ 0.15），返回命中任务的 ID + 标题行（不超过 5 条），格式 `TASK-N: <title>`。

**路径 B — 代码符号 grep**：提取 transcript 中长度 ≥ 5 个字符的驼峰单词与路径片段（`re.findall(r'[A-Z][a-z]+[A-Za-z]+|[a-z]+/[a-z]+', text)`），对每个 token 在 `repo_root` 下执行 `grep -r --include='*.go' -l <token>`，去重后返回命中文件的包路径（`internal/xxx`），不超过 10 条。

**噪声容忍**：若 transcript 为空或全为汉字（`re.fullmatch(r'[一-鿿\s，。？！]+', transcript)`），两条路径均跳过返回空字符串。

`search()` 输出格式：
```
## Dynamic Context (from first-pass transcript)
### Matching Tasks
TASK-N: <title>
### Matching Symbols
internal/xxx
```
若两条路径均无结果，返回空字符串。

### DoD
- [ ] `python3 -c "import sys; sys.path.insert(0,'docs/research/iterative-asr'); from searcher import search; r=search('fix the task one login bug vocal project', '.'); assert isinstance(r, str)"`
- [ ] `python3 -c "import sys; sys.path.insert(0,'docs/research/iterative-asr'); from searcher import search; r=search('', '.'); assert r == ''"`
- [ ] `python3 -c "import sys; sys.path.insert(0,'docs/research/iterative-asr'); from searcher import search; r=search('把这个修一下好吧', '.'); assert r == ''"`
- [ ] `python3 -c "import sys; sys.path.insert(0,'docs/research/iterative-asr'); from searcher import search; r=search('RunHinted pipeline', '.'); assert isinstance(r, str)"`

## Phase 3: Iterative Loop — gemma4 first-pass → Gemini enriched second-pass

在 `run_experiment.py` 中为每条测试用例扩展迭代循环（max_rounds=2）：

**Round 1**：
1. 调用 gemma4 local（Ollama）做 first-pass 转录，得到 `raw_r1`（含噪）
2. 调用 `search(raw_r1, repo_root)` 得到 `dynamic_snippet`
3. 构建 `enriched_hint = base_hint + "\n" + dynamic_snippet`
4. 调用 **Gemini-2.5-flash Audio API**（参考 `docs/research/model-eval/adapters/gemini.py`），将 `enriched_hint` 注入 prompt，得到 `transcript_r1`
5. 计算 `entity_recall(expected_entities, transcript_r1)`
6. 写入 `results.jsonl`：`{id, round: 1, entity_recall, transcript, dynamic_snippet_len, latency_s}`

**Round 2（条件触发）**：仅当 round 1 entity_recall < 1.0 时执行；以 `transcript_r1` 作为新 query 再次 `search()`，构建 `enriched_hint_r2`，再次调用 Gemini API 得到 `transcript_r2`；计算 entity_recall；写入 `round=2` 行。若 round 1 已满分则写 `{round: 2, skipped: true}`。

**汇总行**：所有用例完成后追加 `{"summary": true, "avg_r0": ..., "avg_r1": ..., "avg_r2": ..., "avg_latency_r0": ..., "avg_latency_r1": ..., "avg_latency_r2": ...}`。

超时 120s 的用例记录 `error: timeout`，不中断整体。`expected_entities` 为空的用例跳过 entity_recall 计算，不计入平均值。

### DoD
- [ ] `python3 -c "import json,pathlib; rows=[json.loads(l) for l in pathlib.Path('docs/research/iterative-asr/results.jsonl').read_text().strip().splitlines()]; assert any(r.get('round')==1 for r in rows), 'no round-1 rows'"`
- [ ] `python3 -c "import json,pathlib; rows=[json.loads(l) for l in pathlib.Path('docs/research/iterative-asr/results.jsonl').read_text().strip().splitlines()]; assert any(r.get('round')==2 for r in rows), 'no round-2 rows'"`
- [ ] `python3 -c "import json,pathlib; rows=[json.loads(l) for l in pathlib.Path('docs/research/iterative-asr/results.jsonl').read_text().strip().splitlines()]; s=[r for r in rows if r.get('summary')]; assert s and 'avg_r0' in s[-1] and 'avg_r1' in s[-1] and 'avg_r2' in s[-1]"`
- [ ] `python3 -c "import json,pathlib; rows=[json.loads(l) for l in pathlib.Path('docs/research/iterative-asr/results.jsonl').read_text().strip().splitlines()]; assert any(r.get('round')==0 for r in rows), 'no round-0 baseline rows'"`

## Phase 4: Analysis — 结果分析与决策报告

读取 `results.jsonl`，生成 `docs/research/iterative-asr/analysis.md`，内容包含：

- **对比表**：每条测试用例的 entity_recall @ r0 / r1 / r2 及各轮 latency_s
- **汇总行**：avg_r0、avg_r1、avg_r2 及各自 avg_latency_s
- **Δ 分析**：r1 - r0（动态检索增益）、r2 - r1（第二轮边际增益）
- **TASK-40 对比**：avg_r1 vs TASK-40 Gemini/hinted 基线（0.643）的 delta
- **噪声命中率**：round 1 中 `search()` 返回非空的用例占比
- **延迟分析**：two-stage 总延迟（gemma4 + Gemini）vs 单次 Gemini
- **结论**：若 avg_r1 > 0.643 且噪声命中率 ≥ 50% → "可工程化"；否则 → "收益不足，建议放弃迭代路径"

### DoD
- [ ] `grep -q 'avg_r0' docs/research/iterative-asr/analysis.md`
- [ ] `grep -q 'avg_r1' docs/research/iterative-asr/analysis.md`
- [ ] `grep -q 'avg_r2' docs/research/iterative-asr/analysis.md`
- [ ] `grep -qE 'Δ|delta|增益' docs/research/iterative-asr/analysis.md`
- [ ] `grep -q '噪声命中率' docs/research/iterative-asr/analysis.md`
- [ ] `grep -qE '可工程化|收益不足' docs/research/iterative-asr/analysis.md`

## Constraints

- 不修改任何 Go 生产代码（`internal/` 下文件只读引用）
- Round 0：gemma4 via Ollama（cheap first-pass）；Round 1/2：Gemini-2.5-flash Audio API（参考 `docs/research/model-eval/adapters/gemini.py`）
- Python prototype 调用 Gemini API 通过 `urllib.request` 或直接复用 gemini adapter；Ollama 通过 `urllib.request`
- max_rounds 硬编码为 2；单用例超时 120s
- 实验结论不自动触发生产代码变更，需人工审查 `analysis.md` 后决策
- 成功标准：avg_r1 > 0.643（超越 TASK-40 Gemini 静态 hint 基线）

## Acceptance Gate

- [ ] `test -d docs/research/iterative-asr`
- [ ] `python3 -c "import json,pathlib; rows=[json.loads(l) for l in pathlib.Path('docs/research/iterative-asr/results.jsonl').read_text().strip().splitlines()]; rounds=set(r.get('round') for r in rows if not r.get('summary')); assert {0,1,2}.issubset(rounds), f'missing rounds: {rounds}'"`
- [ ] `python3 -c "import json,pathlib; rows=[json.loads(l) for l in pathlib.Path('docs/research/iterative-asr/results.jsonl').read_text().strip().splitlines()]; s=next(r for r in rows if r.get('summary')); assert s['avg_r1'] >= s['avg_r0'] - 0.05, 'avg_r1 catastrophically below baseline'"`
- [ ] `grep -qE 'avg_r0.*avg_r1|avg_r1.*avg_r2' docs/research/iterative-asr/analysis.md`
- [ ] `grep -qE '可工程化|收益不足' docs/research/iterative-asr/analysis.md`
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Plan updated post-TASK-40: second-pass model changed from Ollama/local to Gemini-2.5-flash Audio API. Round 0 baseline = gemma4 local (entity_recall≈0.286, TASK-40 reference). Success target = avg_r1 > 0.643 (exceeds TASK-40 static hint baseline). Reference adapter: docs/research/model-eval/adapters/gemini.py.

cap:propose=approved
<!-- SECTION:NOTES:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 grep -q 'def main\|import\|results.jsonl' docs/research/iterative-asr/run_experiment.py
- [ ] #2 grep -q 'def search' docs/research/iterative-asr/searcher.py
- [ ] #3 grep -q 'def entity_recall' docs/research/iterative-asr/metrics.py
- [ ] #4 python3 -c "import json,pathlib; d=json.loads(pathlib.Path('docs/research/iterative-asr/conftest.json').read_text()); assert 'max_rounds' in d and 'ollama_model' in d"
- [ ] #5 python3 -c "import sys; sys.path.insert(0,'docs/research/iterative-asr'); from metrics import entity_recall; assert entity_recall(['voci','TASK-1'], 'fix the TASK-1 login bug in the voci project') == 1.0"
- [ ] #6 python3 -c "import sys; sys.path.insert(0,'docs/research/iterative-asr'); from searcher import search; r=search('fix the task one login bug vocal project', '.'); assert isinstance(r, str)"
- [ ] #7 python3 -c "import sys; sys.path.insert(0,'docs/research/iterative-asr'); from searcher import search; r=search('', '.'); assert r == ''"
- [ ] #8 python3 -c "import sys; sys.path.insert(0,'docs/research/iterative-asr'); from searcher import search; r=search('把这个修一下好吧', '.'); assert r == ''"
- [ ] #9 python3 -c "import sys; sys.path.insert(0,'docs/research/iterative-asr'); from searcher import search; r=search('RunHinted pipeline', '.'); assert isinstance(r, str)"
- [ ] #10 python3 -c "import json,pathlib; rows=[json.loads(l) for l in pathlib.Path('docs/research/iterative-asr/results.jsonl').read_text().strip().splitlines()]; assert any(r.get('round')==1 for r in rows), 'no round-1 rows'"
- [ ] #11 python3 -c "import json,pathlib; rows=[json.loads(l) for l in pathlib.Path('docs/research/iterative-asr/results.jsonl').read_text().strip().splitlines()]; assert any(r.get('round')==2 for r in rows), 'no round-2 rows'"
- [ ] #12 python3 -c "import json,pathlib; rows=[json.loads(l) for l in pathlib.Path('docs/research/iterative-asr/results.jsonl').read_text().strip().splitlines()]; s=[r for r in rows if r.get('summary')]; assert s and 'avg_r0' in s[-1] and 'avg_r1' in s[-1] and 'avg_r2' in s[-1]"
- [ ] #13 python3 -c "import json,pathlib; rows=[json.loads(l) for l in pathlib.Path('docs/research/iterative-asr/results.jsonl').read_text().strip().splitlines()]; assert any(r.get('round')==0 for r in rows), 'no round-0 baseline rows'"
- [ ] #14 grep -q 'avg_r0' docs/research/iterative-asr/analysis.md
- [ ] #15 grep -q 'avg_r1' docs/research/iterative-asr/analysis.md
- [ ] #16 grep -q 'avg_r2' docs/research/iterative-asr/analysis.md
- [ ] #17 grep -qE 'Δ|delta|增益' docs/research/iterative-asr/analysis.md
- [ ] #18 grep -q '噪声命中率' docs/research/iterative-asr/analysis.md
- [ ] #19 grep -qE '可工程化|收益不足' docs/research/iterative-asr/analysis.md
- [ ] #20 test -d docs/research/iterative-asr
- [ ] #21 python3 -c "import json,pathlib; rows=[json.loads(l) for l in pathlib.Path('docs/research/iterative-asr/results.jsonl').read_text().strip().splitlines()]; rounds=set(r.get('round') for r in rows if not r.get('summary')); assert {0,1,2}.issubset(rounds), f'missing rounds: {rounds}'"
- [ ] #22 python3 -c "import json,pathlib; rows=[json.loads(l) for l in pathlib.Path('docs/research/iterative-asr/results.jsonl').read_text().strip().splitlines()]; s=next(r for r in rows if r.get('summary')); assert s['avg_r1'] >= s['avg_r0'] - 0.05, 'avg_r1 catastrophically below baseline'"
- [ ] #23 grep -qE 'avg_r0.*avg_r1|avg_r1.*avg_r2' docs/research/iterative-asr/analysis.md
- [ ] #24 grep -qE '可工程化|收益不足' docs/research/iterative-asr/analysis.md
<!-- DOD:END -->

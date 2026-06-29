---
id: TASK-35
title: ASR 迭代上下文检索实验：first-pass transcript 驱动动态 hint 丰富
status: 'Basic: Backlog'
assignee: []
created_date: '2026-06-29 04:06'
updated_date: '2026-06-29 04:19'
labels:
  - 'kind:basic'
  - 'area:asr'
dependencies:
  - TASK-34
ordinal: 30000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
当前 voci pipeline 的 hint 在转录之前静态构建，无法利用当前 utterance 的语义信号。本实验验证：将 first-pass raw transcript 作为检索查询，动态丰富 hint（从 backlog、codebase symbol 等来源），再用 enriched hint 进行二次纠错，是否能改善中英混合语音的实体召回率。依赖 TASK-34 的基准数据确定基线。
<!-- SECTION:DESCRIPTION:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
# Plan: ASR 迭代上下文检索实验：first-pass transcript 驱动动态 hint 丰富

## Context

当前 voci pipeline 在转录前静态构建 hint（`internal/context/builder.go`），hint 包含已知实体但不知道当前话语的主题，导致低频或未被 source 覆盖的实体无法被纠正。本实验提出"迭代 ASR"：用嘈杂的 first-pass raw transcript 作为搜索 query，动态检索相关 backlog 任务与代码符号，将结果追加到 enriched_hint 后再次运行 `RunHinted`，验证 entity_recall 是否显著提升。实验依赖 TASK-34 建立的 asr-bench 基线指标（entity_recall @ static_hint）作为对照组，本实验不修改 Go 生产代码，输出为独立 Python prototype。

## Phase 1: Scaffold — 搭建实验脚手架与静态基线复现

在 `docs/research/iterative-asr/` 下创建实验目录及核心脚本骨架：

- `run_experiment.py`：主入口，接收 WAV 路径 + base_hint 字符串，运行完整实验流程并将结果写入 `results.jsonl`
- `searcher.py`：独立检索模块，暴露 `search(query: str, repo_root: str) -> str` 接口；Phase 2 实现具体搜索逻辑，此阶段仅提供 stub（返回空字符串）
- `metrics.py`：`entity_recall(expected: list[str], hinted: str) -> float`，与 TASK-34 asr-bench 保持一致的计算方式（大小写不敏感子串匹配）
- `conftest.json`：记录实验超参数（`max_rounds`, `ollama_model`, `ollama_url`, `repo_root`）

脚本从 `testdata/testcases.json` 读取 `expected_entities`，对 `sample-01` 到 `sample-15` 的每条有 wav 文件的用例执行静态基线（round=0，直接用 base_hint 调用 RunHinted 等效逻辑），将 `{id, round, entity_recall, hinted_text, latency_s}` 逐行追加到 `results.jsonl`。

### DoD
- [ ] `grep -q 'def main\|import\|results.jsonl' docs/research/iterative-asr/run_experiment.py`
- [ ] `grep -q 'def search' docs/research/iterative-asr/searcher.py`
- [ ] `grep -q 'def entity_recall' docs/research/iterative-asr/metrics.py`
- [ ] `python3 -c "import json,pathlib; d=json.loads(pathlib.Path('docs/research/iterative-asr/conftest.json').read_text()); assert 'max_rounds' in d and 'ollama_model' in d"`
- [ ] `python3 -c "import sys; sys.path.insert(0,'docs/research/iterative-asr'); from metrics import entity_recall; assert entity_recall(['voci','TASK-1'], 'fix the TASK-1 login bug in the voci project') == 1.0"`

## Phase 2: Searcher — 实现 first-pass transcript 检索

实现 `searcher.py` 中的两条检索路径：

**路径 A — Backlog 任务标题/描述模糊匹配**
将 `backlog/tasks/*.md` 的标题与首段描述加载为候选语料，对 first-pass transcript 按词分词后做 token 级 Jaccard 相似度匹配（阈值 ≥ 0.15），返回命中任务的 ID + 标题行（不超过 5 条），格式 `TASK-N: <title>`。

**路径 B — 代码符号 grep**
提取 transcript 中长度 ≥ 5 个字符的驼峰单词与路径片段（`re.findall(r'[A-Z][a-z]+[A-Za-z]+|[a-z]+/[a-z]+', text)`），对每个 token 在 `repo_root` 下执行 `grep -r --include='*.go' -l <token>`，去重后返回命中文件的包路径（`internal/xxx`），不超过 10 条。

**噪声容忍**：若 transcript 为空或全为汉字（`re.fullmatch(r'[一-鿿\s，。？！]+', transcript)`），两条路径均跳过并返回空字符串，不报错。

`search()` 将路径 A + 路径 B 的结果拼装为 hint 追加片段，格式：

```
## Dynamic Context (from first-pass transcript)
### Matching Tasks
TASK-N: <title>
...
### Matching Symbols
internal/xxx
...
```

若两条路径均无结果，返回空字符串。

### DoD
- [ ] `python3 -c "import sys; sys.path.insert(0,'docs/research/iterative-asr'); from searcher import search; r=search('fix the task one login bug vocal project', '.'); assert isinstance(r, str)"`
- [ ] `python3 -c "import sys; sys.path.insert(0,'docs/research/iterative-asr'); from searcher import search; r=search('', '.'); assert r == ''"`
- [ ] `python3 -c "import sys; sys.path.insert(0,'docs/research/iterative-asr'); from searcher import search; r=search('把这个修一下好吧', '.'); assert r == ''"`
- [ ] `python3 -c "import sys; sys.path.insert(0,'docs/research/iterative-asr'); from searcher import search; r=search('RunHinted pipeline', '.'); assert isinstance(r, str)"`

## Phase 3: Iterative Loop — 接入迭代并记录 enriched_hint 结果

在 `run_experiment.py` 中为每条测试用例扩展迭代循环（max_rounds=2）：

1. **Round 1**：调用 gemma4 first-pass transcription（复用 `docs/research/gemma4-asr-test.py` 的 `transcribe()` 逻辑），得到 `raw_r1`；调用 `search(raw_r1, repo_root)` 得到动态片段；构建 `enriched_hint = base_hint + "\n" + dynamic_snippet`；调用 RunHinted 等效（Ollama chat，system prompt 与 `internal/pipeline/pipeline.go:RunHinted` 保持一致）得到 `hinted_r1`；计算 entity_recall；写入 `results.jsonl` 中 `round=1` 行。

2. **Round 2（条件触发）**：仅当 round 1 的 entity_recall < 1.0 时执行；以 `hinted_r1` 作为新 query 再次 `search()`，构建 `enriched_hint_r2`，再次 RunHinted 得到 `hinted_r2`；计算 entity_recall；写入 `round=2` 行；若 round 1 已满分则直接写 `round=2, skipped=true`。

3. 所有用例完成后，在 `results.jsonl` 末尾追加一行 `{"summary": true, ...}`，包含三组平均 entity_recall：`avg_r0`（静态基线）、`avg_r1`、`avg_r2`，以及对应的 `avg_latency_r0`、`avg_latency_r1`、`avg_latency_r2`。

超时 120s 的用例记录 `error: timeout`，不中断整体实验；`expected_entities` 为空的用例（如 sample-04）跳过 entity_recall 计算，不计入平均值。

### DoD
- [ ] `python3 -c "import json,pathlib; rows=[json.loads(l) for l in pathlib.Path('docs/research/iterative-asr/results.jsonl').read_text().strip().splitlines()]; assert any(r.get('round')==1 for r in rows), 'no round-1 rows'"`
- [ ] `python3 -c "import json,pathlib; rows=[json.loads(l) for l in pathlib.Path('docs/research/iterative-asr/results.jsonl').read_text().strip().splitlines()]; assert any(r.get('round')==2 for r in rows), 'no round-2 rows'"`
- [ ] `python3 -c "import json,pathlib; rows=[json.loads(l) for l in pathlib.Path('docs/research/iterative-asr/results.jsonl').read_text().strip().splitlines()]; s=[r for r in rows if r.get('summary')]; assert s and 'avg_r0' in s[-1] and 'avg_r1' in s[-1] and 'avg_r2' in s[-1]"`
- [ ] `python3 -c "import json,pathlib; rows=[json.loads(l) for l in pathlib.Path('docs/research/iterative-asr/results.jsonl').read_text().strip().splitlines()]; assert any(r.get('round')==0 for r in rows), 'no round-0 baseline rows'"`

## Phase 4: Analysis — 结果分析与决策报告

读取 `results.jsonl`，生成 `docs/research/iterative-asr/analysis.md`，内容包含：

- **对比表**：每条测试用例的 entity_recall @ r0 / r1 / r2 及各轮 latency_s（Markdown 表格）
- **汇总行**：avg_r0、avg_r1、avg_r2 及各自的 avg_latency_s
- **Δ 分析**：r1 - r0（动态检索增益）、r2 - r1（第二轮边际增益）
- **噪声命中率**：round 1 中 `search()` 返回非空字符串的用例占比（检索有效率）
- **结论**：依据 Δ(r1-r0) ≥ 0.1 且噪声命中率 ≥ 50% 则给出"可工程化"结论，否则给出"收益不足，建议放弃迭代路径"结论

### DoD
- [ ] `grep -q 'avg_r0' docs/research/iterative-asr/analysis.md`
- [ ] `grep -q 'avg_r1' docs/research/iterative-asr/analysis.md`
- [ ] `grep -q 'avg_r2' docs/research/iterative-asr/analysis.md`
- [ ] `grep -qE 'Δ|delta|增益' docs/research/iterative-asr/analysis.md`
- [ ] `grep -q '噪声命中率' docs/research/iterative-asr/analysis.md`
- [ ] `grep -qE '可工程化|收益不足' docs/research/iterative-asr/analysis.md`

## Constraints

- 不修改任何 Go 生产代码（`internal/` 下文件只读引用）
- Python prototype 不依赖第三方库（仅 stdlib）；Ollama 通过 `urllib.request` 调用
- 实验结果文件仅写入 `docs/research/iterative-asr/`，不写入 `testdata/`
- max_rounds 硬编码为 2，不做自动收敛循环（避免无界实验时长）
- 单用例超时 120s（复用 `docs/research/gemma4-asr-test.py` 的 timeout 设置），超时记录 `error: timeout`，不中断整体实验
- 实验结论不自动触发生产代码变更，需人工审查 `analysis.md` 后决策

## Acceptance Gate

- [ ] `test -d docs/research/iterative-asr`
- [ ] `python3 -c "import json,pathlib; rows=[json.loads(l) for l in pathlib.Path('docs/research/iterative-asr/results.jsonl').read_text().strip().splitlines()]; rounds=set(r.get('round') for r in rows if not r.get('summary')); assert {0,1,2}.issubset(rounds), f'missing rounds: {rounds}'"`
- [ ] `python3 -c "import json,pathlib; rows=[json.loads(l) for l in pathlib.Path('docs/research/iterative-asr/results.jsonl').read_text().strip().splitlines()]; s=next(r for r in rows if r.get('summary')); assert s['avg_r1'] >= s['avg_r0'] - 0.05, 'avg_r1 catastrophically below baseline'"`
- [ ] `grep -qE 'avg_r0.*avg_r1|avg_r1.*avg_r2' docs/research/iterative-asr/analysis.md`
- [ ] `grep -qE '可工程化|收益不足' docs/research/iterative-asr/analysis.md`
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Plan review iteration 4: APPROVED

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

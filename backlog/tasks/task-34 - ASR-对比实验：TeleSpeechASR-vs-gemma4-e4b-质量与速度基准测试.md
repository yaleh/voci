---
id: TASK-34
title: 'ASR 对比实验：TeleSpeechASR vs gemma4:e4b 质量与速度基准测试'
status: 'Basic: Done'
assignee: []
created_date: '2026-06-29 03:34'
updated_date: '2026-06-29 04:38'
labels:
  - 'kind:basic'
  - 'area:asr'
dependencies: []
ordinal: 29000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
测试 TeleSpeechASR 和 gemma4:e4b 。

背景：当前 voci pipeline 使用 SiliconFlow TeleSpeechASR（中文优化），对中英混合语音（如 "Push task 29 to ready"）的识别效果差。gemma4:e4b 已在本地 Ollama 运行，支持音频输入，需要通过基准测试对比两者在中文、英文、中英混合三类场景下的识别质量与延迟，为后续模型选型提供数据依据。
<!-- SECTION:DESCRIPTION:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
# Plan: ASR 对比实验：TeleSpeechASR vs gemma4:e4b 质量与速度基准测试

## Context
voci 当前用 SiliconFlow TeleSpeechASR 处理中文语音，但它会乱码中文语境中的英文技术词汇（函数名、CLI flag、路径）。gemma4:e4b 在 Ollama 本地运行，通过 chat API base64 audio 支持 ASR，单次限 30s。已有 15 条英文 TTS 测试用例（testdata/sample-*.wav）和一个只测 gemma4 的快速脚本（docs/research/gemma4-asr-test.py）。本实验补全中文与中英混合用例，构建双模型基准框架，量化 WER/CER/entity_recall/latency，为是否引入 gemma4 或调整 hint 策略提供数据依据。

## Phase 1: 扩展 gensamples 生成中文与中英混合音频用例

现有 `scripts/gensamples` 已通过 SiliconFlow TTS API（`FunAudioLLM/CosyVoice2-0.5B`）生成英文样本，复用此基础设施生成中文和混合用例，无 macOS 依赖。

**Step A — 扩展 `TestCase` 结构体**（`scripts/gensamples/cases.go`）：
在 `TestCase` 中新增字段：
```go
Voice      string   `json:"voice"`       // TTS 声音，默认 "FunAudioLLM/CosyVoice2-0.5B:claire"
Language   string   `json:"language"`    // "zh" / "en" / ""
Category   []string `json:"category"`    // ["zh-pure"] / ["zh-technical"] / ["zh-mixed"] 等
KnownEntities []string `json:"known_entities"` // 期望被正确识别的技术词
Reference  string   `json:"reference"`   // 规范化期望转录（reference transcript）
```
`generateSpeech` 改为接受 `voice string` 参数，从 `sample.voice` 读取（空时默认 `claire`）。

**Step B — 新增测试用例**（`testdata/testcases.json`）：
追加 20 条新用例，ID sample-16 至 sample-35，使用中文声音 `FunAudioLLM/CosyVoice2-0.5B:anna`：

- **zh-pure**（6 条）：纯中文，无英文实体
  - "把任务二十九推到 ready 状态"
  - "运行所有测试，看看有没有失败"
  - "提交代码，写提交信息"
  - "检查最近的改动有没有问题"
  - "新建一个分支来做这个功能"
  - "把这个 bug 加到 backlog 里"
- **zh-technical**（6 条）：中文夹英文技术词（函数名、路径、模块名）
  - "修复 BuildContextWithSource 里的 bug"
  - "检查 internal/context/builder.go 的逻辑"
  - "DynamicEntitiesSource 的测试挂了，看看怎么回事"
  - "给 RunHinted 加一个单元测试"
  - "pipeline.go 里的 Rewrite 函数需要优化"
  - "检查 SILICONFLOW_API_KEY 有没有配置"
- **zh-mixed**（8 条）：中文句子中包含英语命令片段或 CLI flag
  - "Push task 29 to ready"
  - "Run the tests and check the output"
  - "把 --iterate flag 加到命令里"
  - "用 go test dash dash run 跑一下这个测试"
  - "Check the internal/asr module for timeout issues"
  - "把 TASK-32 的 known entities 逻辑 review 一下"
  - "Fix the DynamicEntitiesSource benchmark"
  - "给 gemma4 的 adapter 加 supports_hints 字段"

每条新用例含 `voice`, `language`, `category`, `known_entities`, `reference` 字段；已有 15 条的新字段留空（`""` / `[]`）不修改。

**Step C — 生成音频**：
```bash
go run ./scripts/gensamples
```
跳过已存在文件，生成 sample-16.wav 至 sample-35.wav 到 `testdata/`。

### DoD
- [ ] `python3 -c "import json,pathlib; c=json.loads(pathlib.Path('testdata/testcases.json').read_text()); assert len(c)>=35, f'only {len(c)} cases'"`
- [ ] `python3 -c "import json,pathlib; c=json.loads(pathlib.Path('testdata/testcases.json').read_text()); zh=[x for x in c if x.get('language','').startswith('zh')]; assert len(zh)>=20, f'only {len(zh)} zh cases'"`
- [ ] `python3 -c "import json,pathlib; c=json.loads(pathlib.Path('testdata/testcases.json').read_text()); mixed=[x for x in c if 'zh-mixed' in (x.get('category') or [])]; assert len(mixed)>=8"`
- [ ] `ls testdata/sample-16.wav testdata/sample-35.wav`
- [ ] `go test ./scripts/gensamples/...`

## Phase 2: 构建模型无关基准框架
在 `docs/research/asr-bench/` 下建立以下结构，将 `docs/research/gemma4-asr-test.py` 中的逻辑迁移到新框架：

```
docs/research/asr-bench/
  adapters/
    base.py          # ModelAdapter ABC: name, capabilities, transcribe(wav_path, opts)
    gemma4.py        # 迁移自 gemma4-asr-test.py；Ollama chat API base64 images
    telespeech.py    # SiliconFlow POST /v1/audio/transcriptions multipart
  metrics.py         # wer(), cer(), entity_recall(), language_confusion()；含 --self-test
  runner.py          # 加载 adapters，遍历用例，输出 results/run-<timestamp>.jsonl
```

`TranscribeOpts` 为 dataclass：`language: str, known_entities: list[str], prompt: str, system_prompt: str`。`ModelAdapter` 为 ABC，子类实现 `transcribe(wav_path, opts) -> (hypothesis: str, latency_s: float)`，并暴露类属性 `supports_hints: bool`。

**模型与 hint_mode 的非对称设计**：
- gemma4 adapter：`supports_hints = True`；hint_mode=on 时将 known_entities 注入 system_prompt（格式："Known technical terms: {terms}"）；同时运行 hint_mode=off（不注入）作为对照。
- telespeech adapter：`supports_hints = False`；TeleSpeechASR API 不支持 entity hint，**始终以 hint_mode=off 运行**，runner 对 telespeech 跳过 hint_mode=on 轮次。

SILICONFLOW_API_KEY 从环境变量读取，缺失时 telespeech adapter 初始化抛 `RuntimeError`。runner.py 接受 `--dry-run` 参数，仅打印将要运行的用例数后退出。

### DoD
- [ ] `python3 -c "import importlib.util; s=importlib.util.spec_from_file_location('base','docs/research/asr-bench/adapters/base.py'); m=importlib.util.module_from_spec(s); s.loader.exec_module(m); assert hasattr(m,'ModelAdapter')"`
- [ ] `python3 -c "import importlib.util; def load(n,f): s=importlib.util.spec_from_file_location(n,f); m=importlib.util.module_from_spec(s); s.loader.exec_module(m); return m; g=load('g','docs/research/asr-bench/adapters/gemma4.py'); assert getattr(g.Gemma4Adapter,'supports_hints',None) is True,'gemma4 must be True'"`
- [ ] `python3 -c "import importlib.util; def load(n,f): s=importlib.util.spec_from_file_location(n,f); m=importlib.util.module_from_spec(s); s.loader.exec_module(m); return m; t=load('t','docs/research/asr-bench/adapters/telespeech.py'); assert getattr(t.TeleSpeechAdapter,'supports_hints',None) is False,'telespeech must be False'"`
- [ ] `python3 docs/research/asr-bench/metrics.py --self-test`
- [ ] `python3 docs/research/asr-bench/runner.py --dry-run 2>&1 | grep -q 'cases loaded'`

## Phase 3: 执行基准测试并收集原始结果
设置环境变量后运行 runner，gemma4 以 hint_mode=on 和 hint_mode=off 各运行一遍，telespeech 仅以 hint_mode=off 运行，写入 `docs/research/asr-bench/results/run-<timestamp>.jsonl`。

```bash
python3 docs/research/asr-bench/runner.py \
  --models all \
  --cases testdata/testcases.json \
  --out docs/research/asr-bench/results/
```

gemma4 仅处理 `duration_s ≤ 25` 的用例（或无 duration_s 字段时不过滤）；telespeech 处理所有用例。每条 JSONL 结果行字段：`{case_id, model, hint_mode, hypothesis, latency_s, wer, cer, entity_recall, language_confusion}`。失败时记录 `error` 字段而非崩溃。

预期行数下界：telespeech × 35 cases × 1 + gemma4 × (≤35) × 2 ≥ 50 行。

### DoD
- [ ] `python3 -c "import json,pathlib,glob; f=sorted(glob.glob('docs/research/asr-bench/results/run-*.jsonl'))[-1]; lines=[l for l in pathlib.Path(f).read_text().splitlines() if l]; assert len(lines)>=50,f'only {len(lines)} rows'"`
- [ ] `python3 -c "import json,pathlib,glob; f=sorted(glob.glob('docs/research/asr-bench/results/run-*.jsonl'))[-1]; rows=[json.loads(l) for l in pathlib.Path(f).read_text().splitlines() if l]; models={r['model'] for r in rows}; assert 'telespeech' in models and 'gemma4' in models"`
- [ ] `python3 -c "import json,pathlib,glob; f=sorted(glob.glob('docs/research/asr-bench/results/run-*.jsonl'))[-1]; rows=[json.loads(l) for l in pathlib.Path(f).read_text().splitlines() if l]; g_hints={r['hint_mode'] for r in rows if r['model']=='gemma4'}; assert 'on' in g_hints and 'off' in g_hints"`
- [ ] `python3 -c "import json,pathlib,glob; f=sorted(glob.glob('docs/research/asr-bench/results/run-*.jsonl'))[-1]; rows=[json.loads(l) for l in pathlib.Path(f).read_text().splitlines() if l]; ts_hints={r['hint_mode'] for r in rows if r['model']=='telespeech'}; assert ts_hints=={'off'}"`

## Phase 4: 生成结构化报告
新建 `docs/research/asr-bench/report.py`，读取最新 `run-*.jsonl`，按 `(model, hint_mode, category)` 分组汇总，输出 `docs/research/asr-bench/results/report-<timestamp>.md`，包含：

1. **摘要表**：model × hint_mode 的 p50/p90 latency、avg WER（en 类别）、avg CER（zh 类别）、avg entity_recall
2. **分类细目**：en-pure / zh-pure / zh-technical / zh-mixed 各自的指标对比
3. **实体召回分析**：entity_recall by model，列出召回率最低的 5 个实体
4. **语言混淆**：language_confusion 分布（仅 zh-mixed 用例）
5. **结论与建议**：基于数据的 ≥ 3 条定性结论

```bash
python3 docs/research/asr-bench/report.py \
  --results docs/research/asr-bench/results/ \
  --out docs/research/asr-bench/results/
```

### DoD
- [ ] `ls docs/research/asr-bench/results/report-*.md`
- [ ] `grep -q 'entity_recall' docs/research/asr-bench/results/report-*.md`
- [ ] `grep -q 'zh-mixed' docs/research/asr-bench/results/report-*.md`
- [ ] `grep -q 'telespeech' docs/research/asr-bench/results/report-*.md && grep -q 'gemma4' docs/research/asr-bench/results/report-*.md`

## Constraints
- 所有 Python 脚本仅使用标准库 + `requests`，不引入 `jiwer`、`speechbrain` 等重型依赖。
- CER 计算：将文本分字（`list(str)`），套用与 WER 相同的 DP 编辑距离，不依赖分词库。
- entity_recall：对 known_entities 中每个实体，在 hypothesis 中做大小写不敏感子串匹配，命中率即为召回率。
- language_confusion：对 zh-mixed 用例，统计 hypothesis 与 reference 中英文字符比例差值绝对值作为混淆度代理。
- 音频生成通过 SiliconFlow TTS API（`FunAudioLLM/CosyVoice2-0.5B`）完成，跨平台，需要 `SILICONFLOW_API_KEY`。
- SILICONFLOW_API_KEY 不得硬编码，缺失时 telespeech adapter 初始化抛 `RuntimeError`。
- 所有结果文件写入 `docs/research/asr-bench/results/`，不修改已有的 `testdata/*.wav` 文件。

## Acceptance Gate
- [ ] `go test ./scripts/gensamples/...`
- [ ] `python3 -c "import json,pathlib; c=json.loads(pathlib.Path('testdata/testcases.json').read_text()); assert len(c)>=35"`
- [ ] `ls testdata/sample-16.wav testdata/sample-35.wav`
- [ ] `python3 -c "import importlib.util; def load(n,f): s=importlib.util.spec_from_file_location(n,f); m=importlib.util.module_from_spec(s); s.loader.exec_module(m); return m; g=load('g','docs/research/asr-bench/adapters/gemma4.py'); assert g.Gemma4Adapter.supports_hints is True"`
- [ ] `python3 -c "import importlib.util; def load(n,f): s=importlib.util.spec_from_file_location(n,f); m=importlib.util.module_from_spec(s); s.loader.exec_module(m); return m; t=load('t','docs/research/asr-bench/adapters/telespeech.py'); assert t.TeleSpeechAdapter.supports_hints is False"`
- [ ] `python3 -c "import json,pathlib,glob; f=sorted(glob.glob('docs/research/asr-bench/results/run-*.jsonl'))[-1]; rows=[json.loads(l) for l in pathlib.Path(f).read_text().splitlines() if l]; ts_hints={r['hint_mode'] for r in rows if r['model']=='telespeech'}; assert ts_hints=={'off'}"`
- [ ] `grep -q 'entity_recall' docs/research/asr-bench/results/report-*.md`
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Plan review iteration 3: APPROVED

cap:propose=approved

claimed: 2026-06-29T03:58:01Z

claimed: 2026-06-29T03:58:07Z

Completed: 2026-06-29T04:16:41Z
## Execution Summary
Result: Done / Commit: 41586a4cac3a25126e8cffbdaa66914087e88a52
Key: 105 benchmark rows; telespeech p50=0.71s; gemma4+hints entity_recall=21.4% vs telespeech 14.3%

## 第一次执行记录（2026-06-29）

Commit: 41586a4cac3a25126e8cffbdaa66914087e88a52
结果文件：docs/research/asr-bench/results/run-20260629-041030.jsonl / report-20260629-041030.md

### 已完成内容
- 扩展 testdata/testcases.json：新增 sample-16–35（zh-pure×6、zh-technical×6、zh-mixed×8），共 35 条
- 生成 sample-16.wav–sample-35.wav（SiliconFlow TTS, Anna 声音）
- 构建 docs/research/asr-bench/ 框架：adapters/（base/gemma4/telespeech）、metrics.py、runner.py、report.py
- 执行基准测试：105 行结果（telespeech×35 + gemma4×35×2）

### 第一次执行结果摘要
| 模型 | hint_mode | WER | CER | entity_recall | latency p50 |
|------|-----------|-----|-----|---------------|-------------|
| gemma4 | off | 107% | 48% | 21.4% | 0.975s |
| gemma4 | on  | 127% | 52% | 21.4% | 0.956s |
| telespeech | off | 93% | 38% | 14.3% | 0.707s |

### 发现的测试设计缺陷（需补充执行）

**问题：zh-technical 用例 TTS 输入含 camelCase/符号，音频失真**

中文 TTS（CosyVoice2-0.5B Anna）收到 `BuildContextWithSource`、`SILICONFLOW_API_KEY`、`pipeline.go` 等字符串时，发音不可预测（跳过、拼字母、乱音译），导致音频本身就不是真实语音。该类别 entity_recall 全部 0% 部分是测试数据问题，而非模型能力上限。

修正方案：zh-technical 用例的 tts_input 改用自然语音形式（用户实际不会念 `/`、`_`、camelCase 连写）：
- `BuildContextWithSource` → `"build context with source"`
- `internal/context/builder.go` → `"builder dot go"` 或 `"builder go"`
- `DynamicEntitiesSource` → `"dynamic entities source"`
- `RunHinted` → `"run hinted"`
- `pipeline.go` + `Rewrite` → `"pipeline go"` + `"rewrite"`
- `SILICONFLOW_API_KEY` → `"siliconflow api key"`

**问题：部分 zh-mixed 用例同样含不朗读的符号**
- sample-30：`--iterate` → 用户不念 `-`，TTS 输入应为 `"iterate flag"`
- sample-35：`supports_hints` → 用户不念 `_`，TTS 输入应为 `"supports hints"`
- 上述两条用例结果存疑，需重录音频后重跑

**可信的 zh-mixed 结论（TTS 输入无符号问题）：**
- sample-28（Push task 29 to ready）：gemma4 优于 telespeech
- sample-31（go test dash dash run）：gemma4 正确输出 `go test -run`，telespeech 截断
- sample-33（TASK 32 / known entities）：两模型均失败，task→touch负/打卡

**hint_mode=on 无效结论维持**：在有效用例上 hint 注入未提升 entity_recall，部分用例（sample-24）反而导致 gemma4 退化为拼音输出。

### 待补充执行
1. 修正 zh-technical 全部 6 条用例的 tts_input（改为自然朗读形式）
2. 修正 zh-mixed sample-30、sample-35 的 tts_input
3. 重新生成对应 WAV 文件（go run ./scripts/gensamples，已有文件需先删除或更名）
4. 重跑 runner.py，生成新一轮 run-*.jsonl
5. 重新生成 report，与第一次结果对比

## 第二次执行记录（2026-06-29）—— 修正测试数据后重跑

结果文件：docs/research/asr-bench/results/run-20260629-044502.jsonl / report-20260629-044502.md

### 修正内容
- zh-technical 全部 6 条（sample-22–27）tts_input 改为自然朗读形式：
  - `BuildContextWithSource` → `"build context with source"`
  - `internal/context/builder.go` → `"builder dot go"`
  - `DynamicEntitiesSource` → `"dynamic entities source"`
  - `RunHinted` → `"run hinted"`
  - `pipeline.go` + `Rewrite` → `"pipeline go"` + `"rewrite"`
  - `SILICONFLOW_API_KEY` → `"siliconflow api key"`
- zh-mixed sample-30（`--iterate` → `"iterate flag"`）、sample-35（`supports_hints` → `"supports hints"`）同步修正
- reference 字段与 tts_input 保持一致
- 删除并重录 8 个 WAV 文件（sample-22–27, sample-30, sample-35）

### 第二次执行结果摘要
| 模型 | hint_mode | WER | CER | entity_recall | latency p50 |
|------|-----------|-----|-----|---------------|-------------|
| gemma4 | off | 85.5% | 51.1% | 28.6% | 0.933s |
| gemma4 | on  | 93.1% | 46.2% | 28.6% | 0.932s |
| telespeech | off | 90.1% | 32.3% | 14.3% | 0.742s |

#### zh-pure 分类
| 模型 | hint_mode | WER | CER | entity_recall |
|------|-----------|-----|-----|---------------|
| gemma4 | off | 90.0% | 45.1% | 100.0% |
| gemma4 | on  | 93.3% | 33.8% | 100.0% |
| telespeech | off | 100.0% | 12.8% | 100.0% |

#### zh-technical 分类（修正后）
| 模型 | hint_mode | WER | CER | entity_recall |
|------|-----------|-----|-----|---------------|
| gemma4 | off | 96.7% | 71.2% | 16.7% |
| gemma4 | on  | 111.2% | 58.1% | 16.7% |
| telespeech | off | 95.2% | 53.2% | 0.0% |

#### zh-mixed 分类
| 模型 | hint_mode | WER | CER | entity_recall |
|------|-----------|-----|-----|---------------|
| gemma4 | off | 73.8% | 40.7% | 28.6% |
| gemma4 | on  | 79.4% | 46.7% | 28.6% |
| telespeech | off | 78.9% | 31.3% | 14.3% |

### 结论对比（第一次 vs 第二次）
- 整体 entity_recall 从 21.4% 提升至 28.6%（gemma4），测试数据质量修正有效
- telespeech entity_recall 从 14.3% 维持不变（该模型在技术词方面能力确实有限）
- zh-technical 类别修正后音频更自然，但两模型 entity_recall 仍低，说明该类别难度来自模型能力而非音频质量
- hint_mode=on 依然未提升 entity_recall，且 WER 普遍高于 off（注入 hint 文本对 gemma4 整体性能无益）
- telespeech CER（字符错误率）整体优于 gemma4，在纯中文上尤其突出（CER 12.8% vs 45.1%）
- zh-mixed 是 gemma4 最具优势的场景（WER 73.8% vs 78.9%），语言混淆度 gemma4 也略胜（0.099 vs 0.082）
<!-- SECTION:NOTES:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 python3 -c "import json,pathlib; c=json.loads(pathlib.Path('testdata/testcases.json').read_text()); assert len(c)>=35, f'only {len(c)} cases'"
- [ ] #2 python3 -c "import json,pathlib; c=json.loads(pathlib.Path('testdata/testcases.json').read_text()); zh=[x for x in c if x.get('language','').startswith('zh')]; assert len(zh)>=20, f'only {len(zh)} zh cases'"
- [ ] #3 python3 -c "import json,pathlib; c=json.loads(pathlib.Path('testdata/testcases.json').read_text()); mixed=[x for x in c if 'zh-mixed' in (x.get('category') or [])]; assert len(mixed)>=8"
- [ ] #4 ls testdata/sample-16.wav testdata/sample-35.wav
- [ ] #5 python3 -c "import importlib.util; s=importlib.util.spec_from_file_location('base','docs/research/asr-bench/adapters/base.py'); m=importlib.util.module_from_spec(s); s.loader.exec_module(m); assert hasattr(m,'ModelAdapter')"
- [ ] #6 python3 -c "import importlib.util; def load(n,f): s=importlib.util.spec_from_file_location(n,f); m=importlib.util.module_from_spec(s); s.loader.exec_module(m); return m; g=load('g','docs/research/asr-bench/adapters/gemma4.py'); assert getattr(g.Gemma4Adapter,'supports_hints',None) is True,'gemma4 must be True'"
- [ ] #7 python3 -c "import importlib.util; def load(n,f): s=importlib.util.spec_from_file_location(n,f); m=importlib.util.module_from_spec(s); s.loader.exec_module(m); return m; t=load('t','docs/research/asr-bench/adapters/telespeech.py'); assert getattr(t.TeleSpeechAdapter,'supports_hints',None) is False,'telespeech must be False'"
- [ ] #8 python3 docs/research/asr-bench/metrics.py --self-test
- [ ] #9 python3 docs/research/asr-bench/runner.py --dry-run 2>&1 | grep -q 'cases loaded'
- [ ] #10 python3 -c "import json,pathlib,glob; f=sorted(glob.glob('docs/research/asr-bench/results/run-*.jsonl'))[-1]; lines=[l for l in pathlib.Path(f).read_text().splitlines() if l]; assert len(lines)>=50,f'only {len(lines)} rows'"
- [ ] #11 python3 -c "import json,pathlib,glob; f=sorted(glob.glob('docs/research/asr-bench/results/run-*.jsonl'))[-1]; rows=[json.loads(l) for l in pathlib.Path(f).read_text().splitlines() if l]; models={r['model'] for r in rows}; assert 'telespeech' in models and 'gemma4' in models"
- [ ] #12 python3 -c "import json,pathlib,glob; f=sorted(glob.glob('docs/research/asr-bench/results/run-*.jsonl'))[-1]; rows=[json.loads(l) for l in pathlib.Path(f).read_text().splitlines() if l]; g_hints={r['hint_mode'] for r in rows if r['model']=='gemma4'}; assert 'on' in g_hints and 'off' in g_hints"
- [ ] #13 python3 -c "import json,pathlib,glob; f=sorted(glob.glob('docs/research/asr-bench/results/run-*.jsonl'))[-1]; rows=[json.loads(l) for l in pathlib.Path(f).read_text().splitlines() if l]; ts_hints={r['hint_mode'] for r in rows if r['model']=='telespeech'}; assert ts_hints=={'off'}"
- [ ] #14 ls docs/research/asr-bench/results/report-*.md
- [ ] #15 grep -q 'entity_recall' docs/research/asr-bench/results/report-*.md
- [ ] #16 grep -q 'zh-mixed' docs/research/asr-bench/results/report-*.md
- [ ] #17 grep -q 'telespeech' docs/research/asr-bench/results/report-*.md && grep -q 'gemma4' docs/research/asr-bench/results/report-*.md
- [ ] #18 go build ./scripts/gensamples/...
<!-- DOD:END -->

---
id: TASK-40
title: OpenRouter & Gemini 全模型 ASR 对比实验：hint 有效性 + 多模型基准
status: 'Basic: Done'
assignee: []
created_date: '2026-06-29 09:22'
updated_date: '2026-06-29 09:57'
labels:
  - 'kind:basic'
  - 'area:asr'
  - 'area:research'
dependencies:
  - TASK-38
  - TASK-39
ordinal: 31000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
在 OpenRouter 和 Gemini 两家服务商上测试所有相关 ASR 模型，横向对比 CER、WER、entity_recall（hint vs. baseline 两组）和 latency，寻找 hint 注入真正有效的模型与格式。

背景：TASK-36/37 证明 SiliconFlow 托管的 Whisper/SenseVoice hint 注入无效（服务端丢弃 prompt）。TASK-38/39 已实现 OpenRouter（JSON+base64）和 Gemini（generateContent）两个适配器，但尚未做对比实验。

待测模型：
- OpenRouter: openai/whisper-large-v3-turbo（已有基准）、gpt-4o-transcribe（官方支持 glossary prompt）、qwen/qwen3-asr-flash-2026-02-10（中文优先）
- Gemini: gemini-2.5-flash（已实现）、gemini-2.5-pro

每个模型跑两遍：baseline（无 hint）+ hinted（known_entities 注入 prompt/text 字段）。

核心问题：GPT-4o-transcribe 和 Gemini 的 prompt 是否真正参与解码，从而提升 entity_recall？
<!-- SECTION:DESCRIPTION:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
# Plan: OpenRouter & Gemini 全模型 ASR 对比实验

## Context
TASK-36/37 证明 SiliconFlow 托管 API 的 hint 无效。TASK-38/39 实现了 OpenRouter 和 Gemini 适配器，但未做对比实验。本任务在两家服务商的 5 个模型上各跑 baseline（无 hint）和 hinted（实体列表注入）两组，输出完整对比报告，确认 hint 有效的模型与注入格式。

待测矩阵（5 模型 × 2 方法 = 10 次运行）：
- OpenRouter: `openai/whisper-large-v3-turbo`, `openai/gpt-4o-transcribe`, `qwen/qwen3-asr-flash-2026-02-10`
- Gemini: `gemini-2.5-flash`, `gemini-2.5-pro`

---

## Phase A: Python 适配器

### 目标
新建 `docs/research/model-eval/adapters/openrouter_adapter.py` 和 `gemini_adapter.py`，
继承 `ModelAdapter`，支持 `supports_hints=True`。

**openrouter_adapter.py**：
- 读取 `OPENROUTER_API_KEY` 或 `ASR_API_KEY` 环境变量
- POST `https://openrouter.ai/api/v1/audio/transcriptions`，body：
  `{"model": model, "input_audio": {"data": b64, "format": "wav"}}`
- 当 `opts.known_entities` 非空时追加 `"prompt": "Known technical terms: <entities>"`

**gemini_adapter.py**：
- 读取 `GEMINI_API_KEY` 或 `ASR_API_KEY` 环境变量
- POST `https://generativelanguage.googleapis.com/v1beta/models/{model}:generateContent`，
  `x-goog-api-key` header
- baseline text part：`"Transcribe the following audio."`
- hinted text part：`"Transcribe the following audio. Known technical terms: <entities>"`
- 解析 `candidates[0].content.parts[0].text`

两个适配器均接受 `model` 构造参数，允许切换模型。

### DoD
- [ ] `test -f /home/yale/work/voci/docs/research/model-eval/adapters/openrouter_adapter.py`
- [ ] `grep -q 'input_audio' /home/yale/work/voci/docs/research/model-eval/adapters/openrouter_adapter.py`
- [ ] `grep -q 'supports_hints = True' /home/yale/work/voci/docs/research/model-eval/adapters/openrouter_adapter.py`
- [ ] `test -f /home/yale/work/voci/docs/research/model-eval/adapters/gemini_adapter.py`
- [ ] `grep -q 'x-goog-api-key' /home/yale/work/voci/docs/research/model-eval/adapters/gemini_adapter.py`
- [ ] `grep -q 'supports_hints = True' /home/yale/work/voci/docs/research/model-eval/adapters/gemini_adapter.py`

---

## Phase B: 运行脚本

### 目标
新建 `docs/research/model-eval/run_openrouter.py` 和 `run_gemini.py`。

每个脚本：
- 接受 `--model <model-id>` 和 `--method baseline|hinted` 参数
- 遍历 35 个 testcases，调用对应 adapter
- 计算 WER、CER、entity_recall_exact、entity_recall_fuzzy、latency_s
- 输出 `results/<provider>-<model-slug>-<method>-<timestamp>.jsonl`
  （model-slug：`/` 替换为 `-`，保留 `-` 分隔）
- 每行 JSON schema 与现有 sensevoice/gemma4 结果一致，额外字段 `method`、`provider`

### DoD
- [ ] `test -f /home/yale/work/voci/docs/research/model-eval/run_openrouter.py`
- [ ] `grep -q 'argparse' /home/yale/work/voci/docs/research/model-eval/run_openrouter.py`
- [ ] `grep -q '\-\-method' /home/yale/work/voci/docs/research/model-eval/run_openrouter.py`
- [ ] `test -f /home/yale/work/voci/docs/research/model-eval/run_gemini.py`
- [ ] `grep -q '\-\-method' /home/yale/work/voci/docs/research/model-eval/run_gemini.py`

---

## Phase C: 执行实验（10 次运行）

### 目标
从项目根目录执行所有 10 次运行，收集结果 JSONL。

运行命令（示例，在项目根执行）：
```
python3 docs/research/model-eval/run_openrouter.py --model openai/whisper-large-v3-turbo --method baseline
python3 docs/research/model-eval/run_openrouter.py --model openai/whisper-large-v3-turbo --method hinted
python3 docs/research/model-eval/run_openrouter.py --model openai/gpt-4o-transcribe --method baseline
python3 docs/research/model-eval/run_openrouter.py --model openai/gpt-4o-transcribe --method hinted
python3 docs/research/model-eval/run_openrouter.py --model qwen/qwen3-asr-flash-2026-02-10 --method baseline
python3 docs/research/model-eval/run_openrouter.py --model qwen/qwen3-asr-flash-2026-02-10 --method hinted
python3 docs/research/model-eval/run_gemini.py --model gemini-2.5-flash --method baseline
python3 docs/research/model-eval/run_gemini.py --model gemini-2.5-flash --method hinted
python3 docs/research/model-eval/run_gemini.py --model gemini-2.5-pro --method baseline
python3 docs/research/model-eval/run_gemini.py --model gemini-2.5-pro --method hinted
```

每次运行结束时打印结果路径；任何模型调用失败记录为空字符串行（不中断整体）。

### DoD
- [ ] `[ $(ls /home/yale/work/voci/docs/research/model-eval/results/openrouter-*.jsonl 2>/dev/null | wc -l) -ge 6 ]`
- [ ] `[ $(ls /home/yale/work/voci/docs/research/model-eval/results/gemini-*.jsonl 2>/dev/null | wc -l) -ge 4 ]`

---

## Phase D: 对比报告

### 目标
扩展 `docs/research/model-eval/compare_models.py`，使其能自动发现新结果，
生成包含以下内容的 Markdown 报告：

1. 全模型 × 全分类（all / zh-technical / zh-mixed）指标汇总表，
   列：model、method、group、N、WER、CER、entity_recall_exact、entity_recall_fuzzy、latency_s
2. Hint 有效性分析：对每个模型，计算 hinted vs. baseline 的 entity_recall_exact delta
3. Recommendations 段落：哪个模型 + method 组合在 entity_recall 上最佳

运行：`python3 docs/research/model-eval/compare_models.py --out docs/research/model-eval/results`

### DoD
- [ ] `test -s /home/yale/work/voci/docs/research/model-eval/results/report-$(ls -t /home/yale/work/voci/docs/research/model-eval/results/report-*.md 2>/dev/null | head -1 | grep -oP '\d{8}-\d{6}').md`
- [ ] `grep -q 'Hint 有效性' /home/yale/work/voci/docs/research/model-eval/results/$(ls -t /home/yale/work/voci/docs/research/model-eval/results/report-*.md 2>/dev/null | head -1 | xargs basename)`
- [ ] `grep -q 'entity_recall_exact' /home/yale/work/voci/docs/research/model-eval/results/$(ls -t /home/yale/work/voci/docs/research/model-eval/results/report-*.md 2>/dev/null | head -1 | xargs basename)`

---

## Constraints
- 不修改 Go 层代码（TASK-38/39 已完成 provider 接线，Python 层直接调用 API）
- API key 从环境变量读取（`OPENROUTER_API_KEY` / `GEMINI_API_KEY` / `ASR_API_KEY` 均可）；config.yaml 已有 key，但 Python 层另行读取
- 结果 JSONL 不入 git（testdata/ 规则）；报告 .md 入 git
- 单次 API 调用超时设为 60s；整体运行时间估计约 20–40 分钟
- compare_models.py 现有 telespeech/whisper-baseline/sensevoice/gemma4 发现逻辑保持不变，新增 openrouter/gemini 发现逻辑

## Acceptance Gate
- [ ] `[ $(ls /home/yale/work/voci/docs/research/model-eval/results/openrouter-*.jsonl 2>/dev/null | wc -l) -ge 6 ]`
- [ ] `[ $(ls /home/yale/work/voci/docs/research/model-eval/results/gemini-*.jsonl 2>/dev/null | wc -l) -ge 4 ]`
- [ ] `test -f $(ls -t /home/yale/work/voci/docs/research/model-eval/results/report-*.md 2>/dev/null | head -1)`
- [ ] `grep -q 'entity_recall_exact' $(ls -t /home/yale/work/voci/docs/research/model-eval/results/report-*.md 2>/dev/null | head -1)`
- [ ] `bash /home/yale/.local/share/baime/scripts/validate-plugin.sh`
<!-- SECTION:PLAN:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
## TASK-40 完成总结

### 执行结果

运行了 8 个有效实验（gpt-4o-transcribe 因 OpenRouter ToS 限制被拒绝，产生空结果文件）：

**OpenRouter（4 次运行）：**
- openai/whisper-large-v3-turbo baseline / hinted
- qwen/qwen3-asr-flash-2026-02-10 baseline / hinted

**Gemini（4 次运行）：**
- gemini-2.5-flash baseline / hinted
- gemini-2.5-pro baseline / hinted

### 核心发现

**Gemini hint 注入高度有效：**
| model | baseline entity_recall_exact | hinted | delta |
|---|---|---|---|
| gemini/gemini-2.5-flash | 0.286 | 0.643 | +0.357 |
| gemini/gemini-2.5-pro | 0.286 | 0.571 | +0.286 |

**OpenRouter Whisper/Qwen hint 无效：**
- whisper-large-v3-turbo：baseline 0.286 = hinted 0.286，delta = 0
- qwen3-asr-flash：baseline 0.214 = hinted 0.214，delta = 0

**结论：** 与 TASK-36/37 的 SiliconFlow 实验一致，纯 ASR 托管 API（Whisper/Qwen）在服务端丢弃 prompt 字段；Gemini 作为真正的多模态 LLM，text prompt 真实参与解码，hint 有显著效果。

### 新建文件
- `docs/research/model-eval/adapters/openrouter_adapter.py`
- `docs/research/model-eval/adapters/gemini_adapter.py`
- `docs/research/model-eval/run_openrouter.py`
- `docs/research/model-eval/run_gemini.py`
- `docs/research/model-eval/compare_models.py`（扩展 Hint 有效性分析段）
- `docs/research/model-eval/results/report-20260629-095522.md`
<!-- SECTION:FINAL_SUMMARY:END -->

---
id: TASK-75
title: 实验：Gemini ASR prompt 中加入 Claude Code 会话上下文是否改善识别效果
status: 'Basic: Done'
assignee: []
created_date: '2026-07-01 17:05'
updated_date: '2026-07-01 17:27'
labels:
  - 'kind:basic'
dependencies: []
ordinal: 46000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
设计并执行一个受控实验，验证在 Gemini 单次 LLM 调用（TranscribeMerged）的 prompt 中加入 Claude Code 会话上下文（已编辑文件、最近命令）是否能改善技术词汇的 ASR 识别准确率，并量化成本（token 数、延迟）。

**3 个 Prompt 变体**
- V0（基线，当前行为）：entities 列表（mergedPromptTemplate 现状）
- V1（结构化上下文）：entities + ## Claude Code Session（文件路径 + 命令，无 prose）
- V2（完整上下文）：entities + ## Claude Code Session（含最近 3 轮 prose）

**测试集：20 条音频**
- 类别 A（8 条）：音频提及当前 session 正在编辑的文件名/函数名
- 类别 B（8 条）：通用技术词汇（不在会话上下文中）
- 类别 C（4 条）：歧义词（如 voci/vocal）
- 音频用 TTS（edge-tts 或 macOS say）合成，ground truth 已知

**衡量指标**：识别准确率（精确匹配）、input token 数、端到端延迟 ms、output token 数

**实现**：新增 scripts/experiment-asr-context/main.go，单二进制，接受 --audio-dir、--ground-truth、--session-hint、--api-key、--variants、--output 参数

**决策标准**：
- 类别 A 准确率 V1 ≥ V0 + 10% 且 token 增量 < 300 → 采纳 V1
- V2 vs V1 无显著差异 → prose 无价值
- 三者准确率相近 → entities 足够，重点转向改善 entity 来源质量
<!-- SECTION:DESCRIPTION:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
# Plan: 实验：Gemini ASR prompt 中加入 Claude Code 会话上下文是否改善识别效果

## Context
当前 ASR pipeline 在 TranscribeMerged 的 prompt 中仅注入 entities 列表。
会话上下文（已编辑文件路径、最近 bash 命令）可能包含与音频内容高度相关的技术词汇，
从而帮助 Gemini Audio API 正确识别低频词。本实验量化该假设，以决定是否在
SessionSource 已有基础上进一步将结构化上下文注入 ASR prompt。

## Phase 1: 生成测试音频集
使用 edge-tts（或系统 TTS）合成 20 条音频并记录 ground truth。

- 类别 A（8 条）：含当前 session 编辑的文件名/函数名（如 `internal/asr/gemini.go`、`BuildCached`）
- 类别 B（8 条）：通用技术词汇（如 "pull request"、"Dockerfile"、"HTTP handler"）
- 类别 C（4 条）：歧义词（如 "voci"/"vocal"、"hinted"/"hinting"）

每条音频对应一行 ground truth，写入 `scripts/experiment-asr-context/testdata/ground_truth.jsonl`。

合成命令示例：
```bash
edge-tts --text "打开 internal/asr/gemini.go 修改 BuildCached 函数" \
  --voice zh-CN-XiaoxiaoNeural \
  --write-media scripts/experiment-asr-context/testdata/A01.mp3
```

### DoD
- `[ ] test -f scripts/experiment-asr-context/testdata/ground_truth.jsonl`
- `[ ] [ $(wc -l < scripts/experiment-asr-context/testdata/ground_truth.jsonl) -ge 20 ]`
- `[ ] grep -q '"category":"A"' scripts/experiment-asr-context/testdata/ground_truth.jsonl`
- `[ ] grep -q '"category":"C"' scripts/experiment-asr-context/testdata/ground_truth.jsonl`

## Phase 2: 实现实验脚本
新增 `scripts/experiment-asr-context/main.go`，单二进制，功能：

1. 读取 `--audio-dir`（音频文件目录）和 `--ground-truth`（JSONL 文件）
2. 读取 `--session-hint`（模拟 Claude Code 会话上下文文本文件，含文件路径+命令±prose）
3. 对每条音频分别用 V0/V1/V2 三个 prompt 变体调用 Gemini Audio API
   - V0：仅 entities 列表（从 `internal/context/builder.go` 复用现有逻辑）
   - V1：entities + `## Claude Code Session`（仅文件路径和命令，无 prose）
   - V2：entities + `## Claude Code Session`（含最近 3 轮 prose）
4. 记录每次调用的：识别文本、input tokens、output tokens、延迟 ms
5. 将结果写入 `--output`（默认 `results.jsonl`），每行格式：
   ```json
   {"audio":"A01.mp3","category":"A","variant":"V1","transcript":"...","exact_match":true,"input_tokens":312,"output_tokens":45,"latency_ms":820}
   ```

### DoD
- `[ ] test -f scripts/experiment-asr-context/main.go`
- `[ ] go build ./scripts/experiment-asr-context/...`
- `[ ] grep -q 'V0\|variant.*v0\|baselinePrompt' scripts/experiment-asr-context/main.go`
- `[ ] grep -q 'V1\|structuredContext' scripts/experiment-asr-context/main.go`
- `[ ] grep -q 'input_tokens\|InputTokens' scripts/experiment-asr-context/main.go`

## Phase 3: 执行实验
准备真实 session hint 文件（从当前 Claude Code session 导出），运行脚本：

```bash
go run scripts/experiment-asr-context/main.go \
  --audio-dir scripts/experiment-asr-context/testdata \
  --ground-truth scripts/experiment-asr-context/testdata/ground_truth.jsonl \
  --session-hint scripts/experiment-asr-context/testdata/session_hint.txt \
  --api-key "$ASR_API_KEY" \
  --variants V0,V1,V2 \
  --output scripts/experiment-asr-context/results.jsonl
```

### DoD
- `[ ] test -f scripts/experiment-asr-context/results.jsonl`
- `[ ] [ $(wc -l < scripts/experiment-asr-context/results.jsonl) -ge 60 ]`
- `[ ] grep -q '"variant":"V2"' scripts/experiment-asr-context/results.jsonl`

## Phase 4: 分析结果并写出决策
按类别和变体汇总准确率、token 增量、延迟，对比决策标准，输出结论文档。

分析命令（内联 Python 或脚本）：
```bash
python3 - <<'EOF'
import json, collections
rows = [json.loads(l) for l in open('scripts/experiment-asr-context/results.jsonl')]
for variant in ['V0','V1','V2']:
    for cat in ['A','B','C']:
        subset = [r for r in rows if r['variant']==variant and r['category']==cat]
        if subset:
            acc = sum(r['exact_match'] for r in subset)/len(subset)
            avg_tok = sum(r['input_tokens'] for r in subset)/len(subset)
            print(f"{variant} cat={cat} acc={acc:.0%} avg_input_tokens={avg_tok:.0f}")
EOF
```

将分析结论（含决策）写入 `docs/research/asr-context-experiment-results.md`，
必须包含 `## Decision` 章节，记录采纳/否决及理由。

### DoD
- `[ ] test -f docs/research/asr-context-experiment-results.md`
- `[ ] grep -q '## Decision' docs/research/asr-context-experiment-results.md`
- `[ ] grep -q 'V0\|V1\|V2' docs/research/asr-context-experiment-results.md`

## Constraints
- 实验脚本只做测量，不修改 production ASR pipeline 代码
- 音频合成在本机完成，不依赖外部服务（edge-tts 本地运行）
- 每个变体对同一音频的调用间隔 ≥ 1s，避免 Gemini rate-limit
- ground_truth.jsonl 中的 exact_match 判断忽略标点，但不做模糊匹配

## Acceptance Gate
- `[ ] test -f docs/research/asr-context-experiment-results.md`
- `[ ] grep -q '## Decision' docs/research/asr-context-experiment-results.md`
- `[ ] [ $(wc -l < scripts/experiment-asr-context/results.jsonl) -ge 60 ]`
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
cap:propose=approved

claimed: 2026-07-01T17:17:02Z
<!-- SECTION:NOTES:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 test -f scripts/experiment-asr-context/testdata/ground_truth.jsonl
- [ ] #2 [ $(wc -l < scripts/experiment-asr-context/testdata/ground_truth.jsonl) -ge 20 ]
- [ ] #3 grep -q '"category":"A"' scripts/experiment-asr-context/testdata/ground_truth.jsonl
- [ ] #4 grep -q '"category":"C"' scripts/experiment-asr-context/testdata/ground_truth.jsonl
- [ ] #5 test -f scripts/experiment-asr-context/main.go
- [ ] #6 go build ./scripts/experiment-asr-context/...
- [ ] #7 grep -q 'baselinePrompt\|variant.*[Vv]0' scripts/experiment-asr-context/main.go
- [ ] #8 grep -q 'structuredContext\|variant.*[Vv]1' scripts/experiment-asr-context/main.go
- [ ] #9 grep -q 'input_tokens\|InputTokens' scripts/experiment-asr-context/main.go
- [ ] #10 test -f scripts/experiment-asr-context/results.jsonl
- [ ] #11 [ $(wc -l < scripts/experiment-asr-context/results.jsonl) -ge 60 ]
- [ ] #12 grep -q '"variant":"V2"' scripts/experiment-asr-context/results.jsonl
- [ ] #13 test -f docs/research/asr-context-experiment-results.md
- [ ] #14 grep -q '## Decision' docs/research/asr-context-experiment-results.md
- [ ] #15 grep -q 'V0\|V1\|V2' docs/research/asr-context-experiment-results.md
<!-- DOD:END -->

---
id: TASK-9
title: 修复 asr_hint 同类实体贪心匹配冲突
status: 'Basic: Backlog'
assignee: []
created_date: '2026-06-28 00:53'
labels:
  - 'kind:basic'
dependencies:
  - TASK-1
priority: medium
ordinal: 8000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
## 问题

TASK-1 实验中发现两类同类实体贪心匹配冲突：

1. **CLI flag 冲突（sample-08）**：TTS 文本含 `--iterate`，ASR 输出 `dash dashed at rate flag`；HINTED 阶段 gemma4:e4b 将其替换为 `--file`（hint 中 --file 比 --iterate 更早出现，模型做了错误的贪心匹配）

2. **任务 ID 冲突（sample-14）**：ASR 输出 `Taskaid`（对应 TASK-8），HINTED 阶段还原为 `TASK-1` 而非 `TASK-8`（音近最小编辑距离选错）

## 根因

HINTED 阶段的 system prompt 未指示模型区分同类实体，仅提供 hint 列表。当多个候选项发音相近或同类时，模型贪心选第一个/距离最小的，而非最接近 ASR 原文发音的。

## 目标

1. 修改 `internal/pipeline/pipeline.go` 的 `RunHinted` system prompt，要求模型在多个同类候选中选择发音最接近 ASR 原文的实体
2. sample-08 跑出 `--iterate`，sample-14 跑出 `TASK-8`
3. 回归：sample-06、13 的多任务 ID 识别不退化

## 不做

- 引入语音相似度算法或外部库
- 修改 asr_hint 构建逻辑
<!-- SECTION:DESCRIPTION:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 go test ./internal/pipeline/...
- [ ] #2 ./voci --file testdata/sample-08.wav 2>&1 | grep -q -- '--iterate'
- [ ] #3 ./voci --file testdata/sample-14.wav 2>&1 | grep -q 'TASK-8'
- [ ] #4 ./voci --file testdata/sample-06.wav 2>&1 | grep -q 'TASK-5'
<!-- DOD:END -->

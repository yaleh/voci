---
id: TASK-10
title: 修复 asr_hint 拆分词函数名匹配失败与过度替换
status: 'Basic: Backlog'
assignee: []
created_date: '2026-06-28 00:53'
labels:
  - 'kind:basic'
dependencies:
  - TASK-1
priority: medium
ordinal: 9000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
## 问题

TASK-1 实验 sample-12 暴露两个相关缺陷：

1. **拆分词函数名未匹配**：TTS 文本含 `BuildContext`，ASR 输出 `build a context`（插入 `a`）。HINTED 阶段未能将 `build a context` 还原为 `BuildContext`，因为 prompt 中函数名以 CamelCase 形式出现，与 ASR 输出的口语拆分形式差距过大。

2. **无关词过度替换**：同一样本中 `pipeline` 被替换为 `internal/pipeline`，但上下文不需要路径形式（用户说的是"the pipeline stage"而非"the pipeline package"）。

## 根因

- asr_hint 中函数名条目为 `BuildContext`，未提供其口语化等价形式，模型无法识别 `build a context` 为同一实体
- `RunHinted` system prompt 未区分"需要路径限定"的实体（包路径）和"不需要"的实体（独立词 pipeline）

## 目标

1. `internal/context/builder.go` 的 asr_hint 生成：为 CamelCase 函数名附加口语化展开（如 `BuildContext (spoken: build context)`）
2. `RunHinted` system prompt：增加"仅在原文明确指向包/路径时才补全为 internal/xxx"的约束
3. sample-12 跑出 `BuildContext`，且结果不含 `internal/pipeline`

## 不做

- 自动枚举所有函数名（仅处理 asr_hint 明确包含的条目）
- 修改 ASR 调用逻辑
<!-- SECTION:DESCRIPTION:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 go test ./internal/context/... ./internal/pipeline/...
- [ ] #2 ./voci --file testdata/sample-12.wav 2>&1 | grep -q 'BuildContext'
- [ ] #3 ./voci --file testdata/sample-12.wav 2>&1 | grep -qv 'internal/pipeline'
<!-- DOD:END -->

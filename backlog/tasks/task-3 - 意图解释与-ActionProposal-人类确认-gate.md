---
id: TASK-3
title: 意图解释与 ActionProposal + 人类确认 gate
status: 'Epic: Proposal'
assignee: []
created_date: '2026-06-27 13:57'
labels:
  - 'kind:epic'
dependencies: []
priority: medium
ordinal: 3000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
将改写后的 transcript 解释为结构化的可执行意图，并在执行前强制人类确认（符合 human-owns-gates 原则）。

## 目标
transcript → ActionProposal → 人类确认 → 交付下游工具

## ActionProposal 模型
- kind: direct_prompt | backlog_action | query | ambiguous
- rewritten: 改写后的 prompt 或命令
- raw_transcript: 原始转写
- confidence: 置信度
- context_used: 引用了哪些 context 条目（provenance）

## 三类意图
- direct_prompt：'帮我写 scan-loop 的单测' → 重写后的 prompt
- backlog_action：'把 TASK-226 改成 ready' → backlog task edit 命令
- query：'现在有几个 In Progress？' → 查询并回答，不执行

## 人类 gate（核心约束）
- 确认前不执行任何副作用操作
- 提供 [确认执行] / [编辑] / [丢弃] 三种动作
- ambiguous 意图必须澄清后才能升级为可执行
- 此 gate 是 voci 的安全边界，不可绕过

## 依赖
- context_builder（上下文检索层）
- 下游 tool adapter（执行通道）
<!-- SECTION:DESCRIPTION:END -->

---
id: TASK-3
title: 意图解释与 ActionProposal + 人类确认 gate
status: 'Epic: Proposal'
assignee: []
created_date: '2026-06-27 13:57'
updated_date: '2026-06-28 00:58'
labels:
  - 'kind:epic'
dependencies:
  - TASK-1
  - TASK-2
priority: medium
ordinal: 3000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
将改写后的 transcript 解释为结构化可执行意图，并在执行前强制人类确认（human-owns-gates）。

## TASK-1 已交付（基线）
改写管道（RAW→HINTED→REWRITTEN）与 `[ambiguous]` 标注已落地于 `internal/pipeline`。本任务在 REWRITTEN 之上构建意图层与 gate。

## 目标
REWRITTEN transcript → ActionProposal → 人类确认 → 交付下游工具

## ActionProposal 模型
- kind: direct_prompt | backlog_action | query | ambiguous
- rewritten / raw_transcript / confidence / context_used（provenance）

## 三类意图
- direct_prompt：'帮我写 scan-loop 的单测' → 重写后的 prompt
- backlog_action：'把 TASK-226 改成 ready' → backlog task edit 命令
- query：'现在有几个 In Progress？' → 查询并回答，不执行

## 人类 gate（核心约束，安全边界）
- 确认前不执行任何副作用操作
- [确认执行] / [编辑] / [丢弃] 三种动作
- ambiguous 必须澄清后才能升级为可执行（复用 TASK-1 的 [ambiguous] 信号）

## 保持 Epic 理由
数据模型 + 3 类意图各自不同的处理路径（passthrough / 生成并执行 backlog 命令 / 查询应答）+ 确认 gate + 副作用执行，足以拆分为多个 basic 子任务。

## 依赖
- TASK-1（REWRITTEN/[ambiguous] 基线）、TASK-2（full_context/provenance）
- 下游 tool adapter（TASK-5，执行通道）
<!-- SECTION:DESCRIPTION:END -->

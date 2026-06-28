---
id: TASK-5
title: 工具 Adapter 抽象：Claude Code / Codex / Gemini CLI
status: 'Basic: Proposal'
assignee: []
created_date: '2026-06-27 13:57'
updated_date: '2026-06-28 00:58'
labels:
  - 'kind:basic'
dependencies:
  - TASK-3
priority: low
ordinal: 5000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
定义 voci 与各 AI 编程工具对接的统一 adapter 接口，使 voci 工具无关、可扩展到 Codex、Gemini CLI。语言：Go。

## 统一接口
- discover_context() → 该工具特有的上下文源（Claude Code session signals、Codex 历史等）
- deliver(proposal ActionProposal) → 把已确认的 proposal 送达该工具
- capabilities() → 声明支持的注入通道（tmux / MCP / stdin / clipboard）

## 首批 adapter
- claude_code：tmux send-keys / MCP（具体实现见 TASK-4）
- codex / gemini_cli：占位，仅接口对齐

## 设计约束
- adapter 仅负责'最后一公里'交付与工具特有上下文，不含意图解释逻辑
- 新增工具 = 新增一个 adapter，core 不变
- 接口需覆盖分离/集成两种会话形态差异

## 降级理由（Epic → Basic）
本质是接口定义 + 1 个真实实现 + 2 个占位 stub。一旦 ActionProposal（TASK-3）稳定，即为一次接口抽取 + 重构，单个 TDD pass 可完成；TASK-4 作为参考实现验证接口，但不阻塞接口本身的定义。

## 依赖
- TASK-3（ActionProposal 模型稳定）
<!-- SECTION:DESCRIPTION:END -->

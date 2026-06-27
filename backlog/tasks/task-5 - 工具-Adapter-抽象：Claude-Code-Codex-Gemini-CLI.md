---
id: TASK-5
title: 工具 Adapter 抽象：Claude Code / Codex / Gemini CLI
status: 'Epic: Proposal'
assignee: []
created_date: '2026-06-27 13:57'
labels:
  - 'kind:epic'
dependencies: []
priority: low
ordinal: 5000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
定义 voci 与各 AI 编程工具对接的统一 adapter 接口，使 voci 工具无关、可扩展到 Codex、Gemini CLI 等。

## 目标
抽象出统一的 adapter 接口，让 context_builder 与 intent 层完全不感知下游具体工具。

## 统一接口
- discover_context() → 该工具特有的上下文源（如 Claude Code 的 session signals、Codex 的历史）
- deliver(proposal: ActionProposal) → 把已确认的 proposal 送达该工具
- capabilities() → 声明支持的注入通道（tmux / MCP / stdin / clipboard）

## 首批 adapter
- claude_code：tmux send-keys / MCP（由 Claude Code monitor Epic 落地）
- codex：占位，接口对齐
- gemini_cli：占位，接口对齐

## 设计约束
- adapter 仅负责'最后一公里'交付与工具特有上下文，不包含意图解释逻辑
- 新增工具 = 新增一个 adapter，无需改动 core
- 统一接口需覆盖分离/集成两种会话形态的差异

## 依赖
- ActionProposal 模型已稳定
- Claude Code monitor 作为参考实现验证接口设计
<!-- SECTION:DESCRIPTION:END -->

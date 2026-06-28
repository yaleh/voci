---
id: TASK-4
title: Claude Code monitor：会话集成形态与输入模式
status: 'Epic: Proposal'
assignee: []
created_date: '2026-06-27 13:57'
updated_date: '2026-06-28 00:58'
labels:
  - 'kind:epic'
dependencies:
  - TASK-3
  - TASK-5
priority: medium
ordinal: 4000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
voci 对 Claude Code 的具体集成实现（Claude Code monitor），提供可切换的集成形态与输入模式。语言：Go（与 TASK-1 一致）。

## 集成形态（--session）
### separate（语音上下文会话与工作会话分离）
- voci 维护专用 Claude Code 上下文会话（headless / MCP），仅做检索/推理/加工
- 最终文本输出到用户工作会话，使用工作会话自身上下文
- 优点：工作会话不被检索污染；缺点：需多会话协调（tmux target / session id）

### integrated（语音上下文会话与工作会话集成）
- voci 以本地 MCP server 挂载到单一工作会话
- 语音 → mcp__voci__transcribe → 直接作为下一条消息
- 优点：架构简单；缺点：上下文能力受限于工作会话当前 context

## 输入模式（--input，运行时可切换）
- preview（默认）：发送前可预览/编辑，手动确认
- direct：处理完成后直接注入，无预览

## 注入通道
- tmux send-keys（分离形态）/ MCP tool 返回值（集成形态）/ clipboard 兜底

## 保持 Epic 理由
2 集成形态 × 2 输入模式 × 3 注入通道，且 separate 形态的多会话协调存在开放设计问题，需先经原型验证再固化。

## 依赖
- TASK-3（ActionProposal gate）、TASK-5（adapter 抽象，本 Epic 是其 Claude Code 首个具体实现）
<!-- SECTION:DESCRIPTION:END -->

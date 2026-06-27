---
id: TASK-4
title: Claude Code monitor：会话集成形态与输入模式
status: 'Epic: Proposal'
assignee: []
created_date: '2026-06-27 13:57'
labels:
  - 'kind:epic'
dependencies: []
priority: medium
ordinal: 4000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
voci 对 Claude Code 的具体集成实现（Claude Code monitor），提供可选择/可切换的集成形态与输入模式。

## 集成形态（命令行参数 --session）
### separate（语音上下文会话与工作会话分离）
- voci 维护专用 Claude Code 上下文会话（headless / MCP），仅做项目上下文检索、推理、加工
- 最终识别文本输出到用户的工作会话，并使用工作会话自身的上下文
- 优点：工作会话上下文不被检索查询污染；缺点：需多会话协调（tmux target / session id）

### integrated（语音上下文会话与工作会话集成）
- voci 以本地 MCP server 形式挂载到单一工作会话
- 语音 → mcp__voci__transcribe → 直接作为下一条消息送入同一会话
- 优点：架构简单；缺点：上下文处理能力受限于工作会话当前 context

## 输入模式（命令行参数 --input，运行时可切换）
- preview（默认）：输出到工作会话前可在界面预览/编辑，手动确认发送
- direct：处理完成后直接注入工作会话，无预览

## 注入通道
- tmux send-keys（分离形态）
- MCP tool 返回值（集成形态）
- clipboard 兜底

## 依赖
- context_builder、ActionProposal gate
- tool adapter 抽象（本 Epic 是其 Claude Code 的首个具体实现）
<!-- SECTION:DESCRIPTION:END -->

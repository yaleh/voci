---
id: TASK-25
title: /api/context 响应增加 updated_at 时间戳（减少不必要 UI 刷新）
status: 'Basic: Proposal'
assignee: []
created_date: '2026-06-28 23:45'
updated_date: '2026-06-29 01:43'
labels:
  - 'kind:basic'
  - 'area:web-ui'
dependencies: []
ordinal: 20000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
在 GET /api/context 响应中增加 updated_at 字段，表示 hint/turns 内容上次发生变化的时间。

前端轮询时比较 updated_at：若与上次相同则跳过 renderContext()，避免 DOM 重写和闪烁。

**重要**：updated_at 仅跟踪会话历史（hint/turns）的变化。建议内容（suggestions）由独立的 /api/suggestions 端点和 generated_at 时间戳管理（见 TASK-26），两者完全解耦——suggestions 刷新不会触发会话历史面板重渲染，反之亦然。

动机：当前每 5 秒无条件重渲染，即使 hint 内容未变化；用户正在阅读面板时会产生闪烁。建议内容的 LLM 生成耗时更长（1-3s），若与会话历史共用同一响应会引入延迟或强制轮询降速。

扩展可能：
- 轮询间隔自适应（内容刚变化时缩短，稳定时拉长）
- SSE 替代轮询（更复杂，后续 task）
<!-- SECTION:DESCRIPTION:END -->

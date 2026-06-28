---
id: TASK-6
title: Web UI：浏览器 PTT 录音、预览编辑与模式切换
status: 'Basic: Proposal'
assignee: []
created_date: '2026-06-27 13:57'
updated_date: '2026-06-28 00:59'
labels:
  - 'kind:basic'
dependencies:
  - TASK-3
priority: low
ordinal: 6000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
voci 浏览器前端：按住说话（PTT）、transcript 预览/编辑、输入模式实时切换。后端为 Go（与 TASK-1 一致，非 FastAPI）。

## MVP 范围（无构建步骤）
- 单页 HTML + Vanilla JS
- recorder.js：MediaRecorder → 按住 Space PTT → 松开 → POST 音频（.wav）
- 后端复用 TASK-1 管道：音频 → SiliconFlow ASR → RAW，→ ollama gemma4:e4b → HINTED/REWRITTEN
- 展示 RAW / HINTED / REWRITTEN 对比
- 预览/编辑：发送前可修改改写结果
- 模式切换 toggle：preview ↔ direct，无需重启

## 后端契约（Go net/http）
- POST /api/voice/transcribe（音频 + tool 参数）
- GET/POST /api/proposals（ActionProposal 确认，见 TASK-3）

## 不做（移出 MVP）
- WS /api/voice/stream 实时转写（后续任务）
- React/Vite 等构建工具

## 降级理由（Epic → Basic）
MVP 为单页 HTML + vanilla JS + 2 个 HTTP 端点，后端管道复用 TASK-1，流式转写已移出范围，规模 contained，单个 TDD pass 可完成。

## 设计约束
- UI 是 gate 的可视化层，确认逻辑在后端（TASK-3）
- 工具无关：通过 tool 参数选择下游 adapter（TASK-5）

## 依赖
- TASK-3（ActionProposal gate）；交付通道 TASK-5
<!-- SECTION:DESCRIPTION:END -->

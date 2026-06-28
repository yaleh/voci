---
id: TASK-6
title: Web UI：浏览器 PTT 录音、预览编辑与模式切换
status: 'Basic: Proposal'
assignee: []
created_date: '2026-06-27 13:57'
updated_date: '2026-06-28 12:08'
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

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
## 架构对齐 (2026-06-28)

本任务的后端 = TASK-16 的 `voci serve`（Monitor-host 形态），**不是**独立 daemon/FastAPI。浏览器 PTT 是喂给 `voci serve` 的一个前端：`POST /api/voice/transcribe`（端点名不变）→ serve 跑 pipeline → 结果经 stdout/Monitor 进会话。

**需在本任务 plan 时拍板的设计点（Monitor-push 与 Web UI preview 的张力）**：
Monitor-push 主路径默认 voice-trusted、**不过 gate**（见 TASK-17 trade-offs）；识别结果直接经 stdout 注入会话。而本任务的核心是「发送前预览/编辑」。二者冲突，需选一：
- 方案 A（前端门控）：serve 对浏览器请求**不直接 emit 到 stdout**，而是同步返回 proposal JSON；用户在 Web UI 预览/编辑/确认后，再调一个「确认 emit」端点才写 stdout→会话。
- 方案 B（免预览）：Web UI 也走 voice-trusted，与 Android 一致，取消 preview（与本任务原初衷不符）。
推荐方案 A：serve 区分「识别但不 emit」与「确认 emit」两个端点，Web UI preview = 两步提交。这也是 Web UI 相对 Android（单步 voice-trusted）的差异价值。

**依赖**：TASK-16 `voci serve` 落地（含可选的「识别不 emit + 确认 emit」双端点设计）。
<!-- SECTION:PLAN:END -->

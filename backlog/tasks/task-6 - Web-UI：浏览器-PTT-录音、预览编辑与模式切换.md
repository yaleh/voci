---
id: TASK-6
title: Web UI：浏览器 PTT 录音、预览编辑与模式切换
status: 'Epic: Proposal'
assignee: []
created_date: '2026-06-27 13:57'
labels:
  - 'kind:epic'
dependencies: []
priority: low
ordinal: 6000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
voci 的浏览器前端：按住说话（PTT）、transcript 预览/编辑、输入模式实时切换。

## 目标
提供轻量 Web 界面作为语音输入入口与人类确认 gate 的可视化承载。

## MVP 范围（无构建步骤）
- 单页 HTML + Vanilla JS（后期可换 React/Vite）
- recorder.js：MediaRecorder → 按住 Space PTT → 松开 → POST 音频
- 展示 RAW transcript 与 REWRITTEN 对比
- 预览/编辑：发送前可修改改写结果
- 模式切换 toggle：preview ↔ direct，无需重启

## 后端契约
- POST /api/voice/transcribe（音频 + tool 参数）
- GET/POST /api/proposals（ActionProposal 确认）
- WS /api/voice/stream（Phase 2 实时转写，可选）

## 设计约束
- UI 是 gate 的可视化层，确认逻辑在后端 intent/gate Epic
- 工具无关：通过 tool 参数选择下游 adapter
- 优先可用性而非美观，验证交互闭环

## 依赖
- 后端服务（FastAPI）、ActionProposal gate、tool adapter
<!-- SECTION:DESCRIPTION:END -->

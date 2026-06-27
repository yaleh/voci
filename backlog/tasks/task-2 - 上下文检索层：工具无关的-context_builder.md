---
id: TASK-2
title: 上下文检索层：工具无关的 context_builder
status: 'Epic: Proposal'
assignee: []
created_date: '2026-06-27 13:57'
labels:
  - 'kind:epic'
dependencies: []
priority: high
ordinal: 2000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
构建 voci 的核心上下文检索能力，工具无关（tool-agnostic），为 ASR 提示与意图改写提供项目感知。

## 目标
将「当前项目状态」转化为两类产物：
1. asr_hint：注入 ASR 模型的术语/任务 id 提示文本（提升专有名词识别）
2. full_context：供 LLM 改写阶段消费的结构化上下文

## 上下文源（可插拔）
- backlog task list（--plain）→ 近期任务 id + title + status
- CLAUDE.md / AGENTS.md → 项目名、术语约定、L0 Config
- git log --oneline -N → 最近变更主题
- meta-cc session signals（可选，via MCP 或直接读 JSONL）

## 设计要求
- source 以插件形式注册，缺失时静默降级（不阻塞管道）
- 输出带 provenance：每条上下文标注来源，便于改写阶段引用
- 提供 context_cache.json 快照，避免每次重复全量读取
- 与具体 AI 工具解耦：context_builder 不知道下游是 Claude Code 还是 Codex

## 与原型的关系
原型 Epic 内联实现 Stage 1 的最小版本；本 Epic 将其抽象为可复用、可扩展的独立模块。
<!-- SECTION:DESCRIPTION:END -->

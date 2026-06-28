---
id: TASK-2
title: 上下文检索层：工具无关的 context_builder
status: 'Basic: Ready'
assignee: []
created_date: '2026-06-27 13:57'
updated_date: '2026-06-28 01:00'
labels:
  - 'kind:basic'
dependencies:
  - TASK-1
priority: high
ordinal: 2000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
将 TASK-1 中的最小上下文构建器抽象为可复用、可扩展的独立模块（工具无关）。

## TASK-1 已交付（基线）
`internal/context/builder.go` 的 `BuildContext(root)` 已实现：读 backlog/tasks/*.md frontmatter（id/title/status）、CLAUDE.md、git log --oneline -10 → 拼接为单一 asr_hint 字符串。TASK-8/9/10 在此基础上优化提示词。

## 本任务增量（相对基线）
1. **source 插件化**：将三个来源（backlog/CLAUDE.md/git）重构为注册式 source 接口，缺失时静默降级
2. **provenance**：每条上下文标注来源，供改写阶段引用
3. **full_context**：除 asr_hint（窄，给 ASR 纠错）外，产出结构化 full_context（宽，给 LLM 改写消费）
4. **context_cache.json**：快照缓存，避免每次全量读取
5. （可选）meta-cc session signals 作为新增 source

## 降级理由（Epic → Basic）
最小版已在 TASK-1 落地，剩余仅为「重构 + 4 个局部特性」，规模小于 TASK-1 本身，单个 TDD pass 可完成。

## 设计要求
- 与下游 AI 工具解耦：context_builder 不知道下游是 Claude Code 还是 Codex
- 语言：Go；测试用 httptest/临时目录隔离，不依赖真实 repo

## 不做
- 下游工具适配（见 TASK-5）
- 意图解释（见 TASK-3）
<!-- SECTION:DESCRIPTION:END -->

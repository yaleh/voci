---
id: TASK-3
title: 意图解释与 ActionProposal + 人类确认 gate
status: 'Epic: Done'
assignee: []
created_date: '2026-06-27 13:57'
updated_date: '2026-06-28 04:39'
labels:
  - 'kind:epic'
dependencies:
  - TASK-1
  - TASK-2
priority: medium
ordinal: 1000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
将改写后的 transcript 解释为结构化可执行意图，并在执行前强制人类确认（human-owns-gates）。

## TASK-1 已交付（基线）
改写管道（RAW→HINTED→REWRITTEN）与 `[ambiguous]` 标注已落地于 `internal/pipeline`。本任务在 REWRITTEN 之上构建意图层与 gate。

## 目标
REWRITTEN transcript → ActionProposal → 人类确认 → 交付下游工具

## ActionProposal 模型
- kind: direct_prompt | backlog_action | query | ambiguous
- rewritten / raw_transcript / confidence / context_used（provenance）

## 三类意图
- direct_prompt：'帮我写 scan-loop 的单测' → 重写后的 prompt
- backlog_action：'把 TASK-226 改成 ready' → backlog task edit 命令
- query：'现在有几个 In Progress？' → 查询并回答，不执行

## 人类 gate（核心约束，安全边界）
- 确认前不执行任何副作用操作
- [确认执行] / [编辑] / [丢弃] 三种动作
- ambiguous 必须澄清后才能升级为可执行（复用 TASK-1 的 [ambiguous] 信号）

## 保持 Epic 理由
数据模型 + 3 类意图各自不同的处理路径（passthrough / 生成并执行 backlog 命令 / 查询应答）+ 确认 gate + 副作用执行，足以拆分为多个 basic 子任务。

## 依赖
- TASK-1（REWRITTEN/[ambiguous] 基线）、TASK-2（full_context/provenance）
- 下游 tool adapter（TASK-5，执行通道）
<!-- SECTION:DESCRIPTION:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
将改写后的 transcript 解释为结构化可执行意图，并在执行前强制人类确认（human-owns-gates）。

## TASK-1 已交付（基线）
改写管道（RAW→HINTED→REWRITTEN）与 `[ambiguous]` 标注已落地于 `internal/pipeline`。本任务在 REWRITTEN 之上构建意图层与 gate。

## 目标
REWRITTEN transcript → ActionProposal → 人类确认 → 交付下游工具

## ActionProposal 模型
- kind: direct_prompt | backlog_action | query | ambiguous
- rewritten / raw_transcript / confidence / context_used（provenance）

## 三类意图
- direct_prompt：'帮我写 scan-loop 的单测' → 重写后的 prompt
- backlog_action：'把 TASK-226 改成 ready' → backlog task edit 命令
- query：'现在有几个 In Progress？' → 查询并回答，不执行

## 人类 gate（核心约束，安全边界）
- 确认前不执行任何副作用操作
- [确认执行] / [编辑] / [丢弃] 三种动作
- ambiguous 必须澄清后才能升级为可执行（复用 TASK-1 的 [ambiguous] 信号）

## 保持 Epic 理由
数据模型 + 3 类意图各自不同的处理路径（passthrough / 生成并执行 backlog 命令 / 查询应答）+ 确认 gate + 副作用执行，足以拆分为多个 basic 子任务。

## 依赖
- TASK-1（REWRITTEN/[ambiguous] 基线）、TASK-2（full_context/provenance）
- 下游 tool adapter（TASK-5，执行通道）

Acceptance Criteria:

---

# Epic Plan: 意图解释与 ActionProposal + 人类确认 gate

## Background
TASK-1 已完成 RAW→HINTED→REWRITTEN 管道，输出为自然语言改写结果。但 REWRITTEN 仍是非结构化文本，无法直接执行。本 Epic 在 REWRITTEN 之上构建意图层：将自然语言改写结果分类为可执行意图（ActionProposal），并在任何副作用发生前强制人类确认。这是 voci 的安全边界——没有人类 gate，任何命令都不能执行。

## Goals
1. REWRITTEN transcript 能被分类为四类意图之一（direct_prompt / backlog_action / query / ambiguous）
2. 每个意图产出结构化 ActionProposal（kind, rewritten, raw_transcript, confidence, context_used）
3. 人类 gate 提供 [确认执行] / [编辑] / [丢弃] 三种动作，确认前无任何副作用
4. ambiguous 意图强制澄清后才能升级为可执行
5. backlog_action 和 query 意图在确认后自动执行，结果回显

## Sub-Task Decomposition

1. **ActionProposal 数据模型与意图分类器** — 定义 `internal/intent/proposal.go`（ActionProposal struct + Kind 枚举），实现 `Classify(ctx, rewritten, fullContext, chat) (ActionProposal, error)`：调用 gemma4:e4b 将 REWRITTEN 分类为四类意图之一并填充字段

2. **人类确认 gate（CLI）** — 实现 `internal/gate/gate.go`：接收 ActionProposal，向 stdout 打印摘要，从 stdin 读取用户动作（确认/编辑/丢弃）；[编辑] 接受用户修正文本并触发重新分类；ambiguous 强制要求澄清文本后重新分类

3. **意图执行层** — 实现 `internal/executor/executor.go`：direct_prompt passthrough（返回 rewritten 文本）；backlog_action 解析并执行 `backlog task edit ...` shell 命令；query 调用 backlog task list/view 并回显结果；human gate 确认后才调用

4. **cmd/voci 集成与端到端 CLI** — 在 `cmd/voci/main.go` 中接入完整管道：`BuildContext` → `Transcribe`（ASR）→ `RunHinted` → `Rewrite` → `Classify` → gate → execute；新增 `--no-gate` flag（仅供测试，跳过确认）

## Sequencing
- 子任务 1（分类器）必须先于 2、3、4：ActionProposal struct 是所有下游的共享合约
- 子任务 2（gate）和 3（执行层）可并行开发，均依赖子任务 1 的 struct 定义
- 子任务 4（集成）依赖 1+2+3 全部完成；是最终的端到端验收

## Constraints
- human gate 是安全边界，不可绕过（`--no-gate` 仅限测试环境）
- backlog_action 执行前须先 dry-run 打印命令，用户确认后才执行
- 意图分类使用 gemma4:e4b（与改写阶段一致，无新模型依赖）
- 依赖 TASK-2 提供 full_context（provenance），context_used 字段需引用 ContextItem.Src
- 语言：Go；所有 HTTP mock 用 httptest.NewServer
- 子任务均为独立 Basic Task，各自有 TDD plan 和 DoD
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Epic plan review iteration 1: APPROVED
premise-ledger:
[H] Sub-Task Decomposition present: ## Sub-Task Decomposition exists with 4 children, each has a title and one-line description
[H] Goal coverage: all 5 Goals are covered — Goal 1+2 by sub-task 1 (Classify + ActionProposal struct), Goal 3 by sub-task 2 (gate), Goal 4 by sub-task 2 (ambiguous → clarification), Goal 5 by sub-task 3 (executor with result echo)
[H] Sequencing coherence: sub-task 1 → {2,3} parallel → 4; stated in ## Sequencing and acyclic
[H] Scope discipline: all 4 children are within epic Goals; none is large enough to be its own epic
[H] No premature creation: plan describes intended children only, does not create backlog tasks
[H] File paths / feasibility: internal/pipeline and internal/context confirmed present; internal/intent, internal/gate, internal/executor are new packages (acceptable); cmd/voci confirmed present

cap:propose=approved

cap:decompose=started

Children created: TASK-11, TASK-12, TASK-13, TASK-14

cap:decompose=done
epicDecompose: 4 children created at Basic: Proposal. Promote chosen children → Basic: Ready to execute.

cap:evaluate=recommendation:FINISH | done=4 needsHuman=0 | all children Basic: Done with DoD pass | data_source: measured

RECOMMENDATION: FINISH.
To finish: set status → Epic: Done.
Children completed:
- TASK-11 ✓ ActionProposal struct + Classify
- TASK-12 ✓ Human confirmation gate CLI
- TASK-13 ✓ Intent execution layer
- TASK-14 ✓ cmd/voci end-to-end integration

All go test ./..., go build ./cmd/voci, go vet ./... pass across all children.
<!-- SECTION:NOTES:END -->

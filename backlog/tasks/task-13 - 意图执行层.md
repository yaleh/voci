---
id: TASK-13
title: 意图执行层
status: 'Basic: Done'
assignee: []
created_date: '2026-06-28 01:46'
updated_date: '2026-06-28 03:51'
labels:
  - 'kind:basic'
dependencies: []
parent_task_id: TASK-3
ordinal: 12000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
实现 internal/executor/executor.go：根据 ActionProposal.Kind（由 TASK-11 定义）分派执行路径——direct_prompt 直接返回 Rewritten 文本（passthrough）；backlog_action 先 dry-run 打印将执行的 backlog task edit 命令，gate 确认后执行 shell 调用并回显输出；query 调用 backlog task list/view 并回显结果，不产生写副作用。所有副作用必须在 human gate（TASK-12）确认后才触发。提供 Executor 接口以便 mock 测试。
<!-- SECTION:DESCRIPTION:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
# Plan: 意图执行层

## Phase A: Executor 接口 + direct_prompt passthrough
### Tests (write first)
(internal/executor/executor_test.go — TestExecuteDirectPromptReturnsRewritten)
### Implementation
(internal/executor/executor.go — Execute(proposal ActionProposal) (string, error) dispatch)
### DoD
- [ ] `go test ./internal/executor/...`
- [ ] `go test ./...`

## Phase B: backlog_action 执行器
### Tests (write first)
(internal/executor/executor_test.go — inject cmdRunner func, assert dry-run printed then cmd executed)
### Implementation
(internal/executor/executor.go — backlog_action case: parse command, dry-run print, run via injected cmdRunner)
### DoD
- [ ] `go test ./internal/executor/...`
- [ ] `go test ./...`

## Phase C: query 执行器
### Tests (write first)
(internal/executor/executor_test.go — inject cmdRunner returning fake output, assert output returned)
### Implementation
(internal/executor/executor.go — query case: run backlog task list/view, return stdout)
### DoD
- [ ] `go test ./internal/executor/...`
- [ ] `go test ./...`

## Constraints
- 依赖 TASK-11 ActionProposal struct（internal/intent/proposal.go）
- cmdRunner 注入隔离真实 shell 执行，测试不依赖真实 backlog CLI
- backlog_action dry-run 先打印命令（前缀 [DRY-RUN]），human gate 确认后才执行
- 每个 Phase ≤ 200 行

## Acceptance Gate
- [ ] `go test ./...`
- [ ] `go build ./cmd/voci`
- [ ] `go vet ./...`
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
claimed: 2026-06-28T03:49:06Z

## Execution Summary
Result: Done
Commit: 177386d

Phase A (direct_prompt + ambiguous): PASS
Phase B (backlog_action dry-run + confirmed): PASS
Phase C (query read-only): PASS
Acceptance gate: go test ./... PASS, go build ./cmd/voci PASS, go vet ./... PASS

## Execution Summary
Result: Done
Commit: b6374a2
All 9 DoD checks passed.
<!-- SECTION:NOTES:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 go test ./internal/executor/...
- [ ] #2 go test ./...
- [ ] #3 go test ./internal/executor/...
- [ ] #4 go test ./...
- [ ] #5 go test ./internal/executor/...
- [ ] #6 go test ./...
- [ ] #7 go test ./...
- [ ] #8 go build ./cmd/voci
- [ ] #9 go vet ./...
<!-- DOD:END -->

---
id: TASK-12
title: 人类确认 gate（CLI）
status: 'Basic: Done'
assignee: []
created_date: '2026-06-28 01:46'
updated_date: '2026-06-28 02:09'
labels:
  - 'kind:basic'
dependencies: []
parent_task_id: TASK-3
ordinal: 11000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
实现 internal/gate/gate.go：接收 ActionProposal（由 TASK-11 定义），向 stdout 打印摘要（kind、rewritten、confidence），从 stdin 读取用户动作：[确认执行] / [编辑] / [丢弃]。[编辑] 动作接受用户修正文本并触发重新分类（回调 Classify）；ambiguous kind 强制用户提供澄清文本后才能升级为可执行意图，不允许直接确认执行。确认前不产生任何副作用。gate 须支持 io.Reader/io.Writer 注入以便单元测试。
<!-- SECTION:DESCRIPTION:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
# Plan: 人类确认 gate（CLI）

## Phase A: GateResult 类型 + 打印摘要
### Tests (write first)
(internal/gate/gate_test.go — TestPrintProposalSummary*)
### Implementation
(internal/gate/gate.go — GateResult struct, PrintSummary func)
### DoD
- [ ] `go test ./internal/gate/...`
- [ ] `go test ./...`

## Phase B: stdin 交互（确认/编辑/丢弃）
### Tests (write first)
(internal/gate/gate_test.go — inject io.Reader, assert GateResult)
### Implementation
(internal/gate/gate.go — Run(r io.Reader, w io.Writer, proposal ActionProposal) GateResult)
### DoD
- [ ] `go test ./internal/gate/...`
- [ ] `go test ./...`

## Phase C: ambiguous 强制澄清
### Tests (write first)
(internal/gate/gate_test.go — TestGateAmbiguousForcesClarification)
### Implementation
(update gate.go — if proposal.Kind==ambiguous, prompt for clarification before showing actions)
### DoD
- [ ] `go test ./internal/gate/...`
- [ ] `go test ./...`

## Constraints
- gate 必须依赖 TASK-11 的 ActionProposal struct（internal/intent/proposal.go）
- 测试通过注入 io.Reader/io.Writer 完全避免真实 stdin/stdout
- [编辑] 动作不执行任何命令，仅返回修正文本
- 每个 Phase ≤ 200 行

## Acceptance Gate
- [ ] `go test ./...`
- [ ] `go build ./cmd/voci`
- [ ] `go vet ./...`
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
claimed: 2026-06-28T02:03:12Z

## Execution Summary
Result: Done
Commit: 79ea1e0
All 9 DoD checks passed.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
## Execution Summary
Result: Done
Commit: edc1c26

### Phases completed
- Phase A: GateResult type + PrintSummary — PASS (go test ./internal/gate/...)
- Phase B: stdin interaction (confirm/edit/discard) — PASS
- Phase C: ambiguous forces clarification — PASS

### DoD
- go test ./... — PASS (all packages)
- go build ./cmd/voci — PASS
- go vet ./... — PASS

### Files
- internal/gate/gate.go (GateResult, PrintSummary, Run)
- internal/gate/gate_test.go (8 tests covering all phases)
- internal/intent/proposal.go (copied from main repo — worktree predated TASK-11 merge)
<!-- SECTION:FINAL_SUMMARY:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 go test ./internal/gate/...
- [ ] #2 go test ./...
- [ ] #3 go test ./internal/gate/...
- [ ] #4 go test ./...
- [ ] #5 go test ./internal/gate/...
- [ ] #6 go test ./...
- [ ] #7 go test ./...
- [ ] #8 go build ./cmd/voci
- [ ] #9 go vet ./...
<!-- DOD:END -->

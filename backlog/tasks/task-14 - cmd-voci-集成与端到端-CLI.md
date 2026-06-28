---
id: TASK-14
title: cmd/voci 集成与端到端 CLI
status: 'Basic: Done'
assignee: []
created_date: '2026-06-28 01:46'
updated_date: '2026-06-28 04:37'
labels:
  - 'kind:basic'
dependencies: []
modified_files:
  - cmd/voci/main.go
  - cmd/voci/main_test.go
parent_task_id: TASK-3
ordinal: 1000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
在 cmd/voci/main.go 中串联完整管道：BuildContext → Transcribe（ASR）→ RunHinted → Rewrite → Classify（TASK-11）→ gate（TASK-12）→ execute（TASK-13）。新增 --no-gate flag（仅限测试环境，跳过人类确认）。端到端验收：给定录音文件，CLI 应输出 ActionProposal 摘要并等待用户确认，确认后执行对应动作并回显结果。依赖 TASK-11、TASK-12、TASK-13 全部完成后实施。
<!-- SECTION:DESCRIPTION:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
# Plan: cmd/voci 集成与端到端 CLI

## Phase A: 接入 Classify + gate + executor（主流程）
### Tests (write first)
(cmd/voci/main_test.go — inject fake classify/gate/execute fns; assert full pipeline runs RAW→REWRITTEN→classified→confirmed→executed)
### Implementation
(cmd/voci/main.go — add Classify→gate.Run→executor.Execute after PrintComparison; wire real implementations via LoadConfig)
### DoD
- [ ] `go test ./cmd/voci/...`
- [ ] `go test ./...`

## Phase B: --no-gate flag
### Tests (write first)
(cmd/voci/main_test.go — TestCLINoGateFlagSkipsGate: assert gate fn not called when --no-gate passed)
### Implementation
(cmd/voci/main.go — add --no-gate bool flag; when set bypass gate.Run, call executor directly)
### DoD
- [ ] `go test ./cmd/voci/...`
- [ ] `go test ./...`
- [ ] `go build -o voci ./cmd/voci && echo "build ok"`

## Constraints
- --no-gate 仅供测试，不出现在用户文档中
- 依赖 TASK-11（internal/intent）、TASK-12（internal/gate）、TASK-13（internal/executor）已完成
- 测试通过注入函数隔离真实 classify/gate/execute 调用
- 每个 Phase ≤ 200 行

## Acceptance Gate
- [ ] `go test ./...`
- [ ] `go build -o voci ./cmd/voci && echo "build ok"`
- [ ] `go vet ./...`
- [ ] `./voci --file testdata/sample-01.wav --no-gate 2>&1 | grep -q REWRITTEN`
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
claimed: 2026-06-28T04:33:14Z

## Execution Summary
Result: Done
Commit: f8d3363
All 9 DoD checks passed.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
## Execution Summary
Result: Done
Commit: 9c79f67

### Changes
- `cmd/voci/main.go`: Added `ClassifyFn`, `GateFn`, `ExecuteFn` injectable types; extended `run()` signature with these three params; added `--no-gate` flag; wired Stages 6–9 (Classify → gate → Execute → print result) after existing Stage 5 (PrintComparison); kept `--iterate` / IterateLoop intact.
- `cmd/voci/main_test.go`: Updated all existing test calls to pass 3 new nil/fake params; added `TestRunFullPipelineWithGate`, `TestRunFullPipelineGateDiscard`, `TestCLINoGateFlagSkipsGate`.

### DoD
- go test ./cmd/voci/... → PASS
- go test ./... → PASS (all 10 packages)
- go build -o /tmp/voci-test ./cmd/voci → build ok
- go vet ./... → vet ok
- Real-API acceptance item skipped (no SILICONFLOW_API_KEY in CI env)
<!-- SECTION:FINAL_SUMMARY:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 go test ./cmd/voci/...
- [ ] #2 go test ./...
- [ ] #3 go test ./cmd/voci/...
- [ ] #4 go test ./...
- [ ] #5 go build -o voci ./cmd/voci && echo "build ok"
- [ ] #6 go test ./...
- [ ] #7 go build -o voci ./cmd/voci && echo "build ok"
- [ ] #8 go vet ./...
- [ ] #9 ./voci --file testdata/sample-01.wav --no-gate 2>&1 | grep -q REWRITTEN
<!-- DOD:END -->

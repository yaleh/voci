---
id: TASK-62
title: 加计时日志
status: 'Basic: Done'
assignee: []
created_date: '2026-06-30 11:22'
updated_date: '2026-06-30 12:13'
labels:
  - 'kind:basic'
dependencies: []
ordinal: 40000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
加计时日志
<!-- SECTION:DESCRIPTION:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
# Proposal: 加计时日志

## Background

voci 的语音处理管线在 `handleTranscribe` 中串行执行四个步骤：ASR 转录 (TranscribeFn)、上下文纠错 (HintedFn)、文本改写 (RewriteFn)、意图分类 (ClassifyFn)。目前 `handlers.go` 中没有任何计时或日志埋点，每次请求的耗时完全不可见。TASK-42 的离线实验显示平均总延迟约 11 秒，但该数据来自受控批量实验，无法反映生产环境中各步骤的实际分布。当用户感知延迟偏高时，运维人员无法判断瓶颈在 ASR 网络 I/O、LLM 纠错、改写还是分类，也无法区分个别慢请求与系统性退化。加入每步计时日志，可让运维人员在不侵入代码的情况下实时观察各步骤耗时，为调优和故障排查提供基础数据。

## Goals

1. 每次 `/api/voice/transcribe` 请求完成后，向标准错误（stderr）输出一行结构化计时日志，格式为 `asr: <N>ms, hinted: <N>ms, rewrite: <N>ms, classify: <N>ms, total: <N>ms`，精度为毫秒。
2. 单步骤出错时（HintedFn / RewriteFn / ClassifyFn 返回 error），仍输出已完成步骤的耗时，并在失败步骤标注 `(error)`，以便区分超时失败与正常完成的耗时分布。
3. 计时逻辑不引入新的外部依赖，仅使用 Go 标准库 `time` 包和 `log` / `fmt` 写入 stderr，不改变现有 HTTP 响应格式或 JSON 输出结构。
4. 新增或修改的代码通过现有的 `go test ./...` 构建检查，不破坏已有测试。

## Proposed Approach

在 `internal/daemon/handlers.go` 的 `handleTranscribe` 函数中，在每个管线步骤调用前后用 `time.Now()` 记录起止时间。四个步骤（TranscribeFn、HintedFn、RewriteFn、ClassifyFn）全部完成（或某步失败）后，将各步骤耗时及总耗时拼装成一行文本写入 stderr。步骤失败时，在对应步骤标注 `(error)` 后仍先写出日志再返回 HTTP 错误响应。日志写入使用 `log.Printf` 或 `fmt.Fprintf(os.Stderr, ...)` 以保持与 Go 守护进程惯例一致，不引入新的日志库或结构化日志框架。RewriteFn 为可选字段（`nil` 时跳过），计时输出对应步骤需标注 `rewrite: -` 以示区别。

## Trade-offs and Risks

- **不做**：不引入 OpenTelemetry、Prometheus metrics 或任何分布式追踪方案；当前规模下结构化可观测性基础设施的收益不抵引入成本。
- **不做**：不将计时数据写入响应体或自定义 HTTP header，避免改变客户端（浏览器端 JS）的解析逻辑。
- **风险**：若日后需要机器可读的指标（如 Grafana 看板），纯文本 stderr 日志需要额外的 log parser；届时可升级为结构化 JSON 日志，但当前需求不要求此能力。
- **风险**：TranscribeFn 目前直接返回 `string`（无 error 返回值），若 ASR 静默失败（返回空字符串），计时日志会正常输出，但无法区分超时与空结果；这是现有架构局限，不在本任务范围内。
- **替代方案**：在 HTTP 中间件层计总耗时（更简单），但无法拆分各步骤，无法定位瓶颈，故不采用。

---

# Plan: 加计时日志

## Phase A: 在 handleTranscribe 添加各步骤计时日志

### Tests (write first)
File: `internal/daemon/server_test.go` (existing file, same `daemon` package)

- `TestHandleTranscribeLogsTimings`: redirect `log.SetOutput` to a `bytes.Buffer`; POST fake audio bytes to `handleTranscribe` via `httptest`; restore `log.SetOutput(os.Stderr)` with `defer`; assert the captured buffer contains all of `"asr:"`, `"hinted:"`, `"rewrite:"`, `"classify:"`, `"total:"`.
- `TestHandleTranscribeLogsTimings_NilRewrite`: same setup but set `srv.RewriteFn = nil`; assert log output contains `"rewrite: -"` (nil-guard branch label).
- `TestHandleTranscribeLogsTimings_HintedError`: same redirect setup but make `HintedFn` return an error; assert the captured buffer contains `"asr:"` and `"hinted: (error)"` so that partial timings are logged before the HTTP error response is written (covers Goal 2).

### Implementation
File: `internal/daemon/handlers.go`

- Add `"log"` and `"time"` to the import block (`os` is already present; `fmt` may be needed for `Sprintf`).
- Before `TranscribeFn` call: `tStart := time.Now(); t0 := tStart`.
- After `TranscribeFn` returns: `asrMs := time.Since(t0).Milliseconds()`.
- Before `HintedFn` call: `t1 := time.Now()`.
- After `HintedFn` returns (before early error return): `hintedMs := time.Since(t1).Milliseconds()`. On error, emit partial log with the failed step labelled `(error)` before returning:
  ```go
  log.Printf("pipeline: asr: %dms, hinted: (error), rewrite: -, classify: -, total: %dms",
      asrMs, time.Since(tStart).Milliseconds())
  ```
- Before the `RewriteFn` nil-guard block: `t2 := time.Now(); var rewriteLabel string`. Inside the nil branch: `rewriteLabel = "-"`. Inside the non-nil branch: record `rewriteMs := time.Since(t2).Milliseconds()` and set `rewriteLabel = fmt.Sprintf("%dms", rewriteMs)`. On rewrite error, emit partial log with `rewrite: (error)` before returning:
  ```go
  log.Printf("pipeline: asr: %dms, hinted: %dms, rewrite: (error), classify: -, total: %dms",
      asrMs, hintedMs, time.Since(tStart).Milliseconds())
  ```
- Before `ClassifyFn` call: `t3 := time.Now()`.
- After `ClassifyFn` returns (before early error return): `classifyMs := time.Since(t3).Milliseconds()`. On error, emit partial log with `classify: (error)` before returning:
  ```go
  log.Printf("pipeline: asr: %dms, hinted: %dms, rewrite: %s, classify: (error), total: %dms",
      asrMs, hintedMs, rewriteLabel, time.Since(tStart).Milliseconds())
  ```
- After all steps succeed, emit the full log line:
  ```go
  log.Printf("pipeline: asr: %dms, hinted: %dms, rewrite: %s, classify: %dms, total: %dms",
      asrMs, hintedMs, rewriteLabel, classifyMs, time.Since(tStart).Milliseconds())
  ```
- This format satisfies the proposal's required substrings `asr:`, `hinted:`, `rewrite:`, `classify:`, `total:`.

### DoD
- [ ] `go test ./internal/daemon/...`
- [ ] `grep -q 'log.Printf\|log.Print' internal/daemon/handlers.go`
- [ ] `grep -q 'time.Now' internal/daemon/handlers.go`

## Constraints
- Only add log lines; do not change HTTP response format or JSON output structure
- Use stdlib `log` and `time` only; no new external dependencies
- Log to stderr (log package default)
- On error in any pipeline step, emit partial timing log with the failed step labelled `(error)` before returning the HTTP error so no timing data is silently lost
- When RewriteFn is nil, label its slot as `rewrite: -` in the log line

## Acceptance Gate
- [ ] `go test ./...`
- [ ] `grep -q 'total:' internal/daemon/handlers.go`
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Proposal self-review: APPROVED
premise-ledger:
[E] background lines: 8行，明确说明 WHY（无计时埋点 → 无法定位瓶颈 → 调优/故障排查困难）
[C] goal verifiability: 4个目标均可通过可观察行为验证（日志格式/错误标注/无新依赖/测试通过）
[H] feasibility 基准: handlers.go 已确认存在4步串行管线，TranscribeFn/HintedFn/RewriteFn/ClassifyFn全部可见，RewriteFn nil-guard已在代码中
GCL-self-report: E=2 C=1 H=1

Proposal approved. Starting plan draft.

Plan review iteration 1: NEEDS_REVISION

Issues fixed:
1. Goal 2 not covered: added TestHandleTranscribeLogsTimings_HintedError test and explicit (error) label format in Implementation spec for each error-path log.Printf call.
2. Acceptance Gate grep broken: changed `grep -q 'total='` to `grep -q 'total:'` — implementation format string uses `total:` (colon), not `total=` (equals).

Plan review iteration 2: APPROVED
premise-ledger:
[E] goal coverage: Goals 1-4 all addressed — Goal 1 (format with all 5 substrings) by Phase A tests + implementation; Goal 2 (partial log on error with "(error)" label) by HintedError test + error-path emit before return; Goal 3 (stdlib only, no HTTP response change) by Constraints section; Goal 4 (go test ./... passes) by Acceptance Gate
[C] file paths exist: internal/daemon/handlers.go ✓, internal/daemon/server_test.go ✓ — both confirmed via ls
[H] DoD 充分性基准: Three DoD shell commands cover red→green (go test), structural presence of log.Printf, and time.Now; Acceptance Gate adds full-suite go test ./... plus grep for 'total:' in handlers.go; adequate for a single-phase logging change
GCL-self-report: E=3 C=1 H=1

claimed: 2026-06-30T12:10:47Z
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Added per-step timing logs to `handleTranscribe`. Each request now emits `pipeline: asr: Nms, hinted: Nms, rewrite: Nms, classify: Nms, total: Nms` on success. Error paths emit partial logs with `(error)` on the failing step. nil RewriteFn labeled `rewrite: -`. Three new tests added to server_test.go covering success, nil-rewrite, and hinted-error cases. 48/48 tests pass.
<!-- SECTION:FINAL_SUMMARY:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 go test ./internal/daemon/...
- [ ] #2 grep -q 'log.Printf\|log.Print' internal/daemon/handlers.go
- [ ] #3 grep -q 'time.Now' internal/daemon/handlers.go
- [ ] #4 go test ./...
- [ ] #5 grep -q 'total:' internal/daemon/handlers.go
<!-- DOD:END -->

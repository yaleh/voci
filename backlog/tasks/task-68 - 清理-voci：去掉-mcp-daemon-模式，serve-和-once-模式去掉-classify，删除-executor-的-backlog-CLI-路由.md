---
id: TASK-68
title: >-
  清理 voci：去掉 mcp/daemon 模式，serve 和 once 模式去掉 classify，删除 executor 的 backlog CLI
  路由
status: 'Basic: Done'
assignee: []
created_date: '2026-06-30 17:05'
updated_date: '2026-06-30 23:29'
labels:
  - 'kind:basic'
dependencies: []
ordinal: 41000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
清理 voci：去掉 mcp/daemon 模式，serve 和 once 模式去掉 classify，删除 executor 的 backlog CLI 路由。包括整体删掉 executor，清理掉 once 模式的 backlog CLI 路由。
<!-- SECTION:DESCRIPTION:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
# Plan: 清理 voci：去掉 mcp/daemon 模式，serve 和 once 模式去掉 classify，删除 executor 的 backlog CLI 路由

## Phase A: 删除 executor 和 intent/classify 包
### Tests (write first)
删除的测试文件（随包整体删除）：
- `internal/executor/executor_test.go` — DefaultExecutor 全套测试，随包删除
- `internal/intent/classify_test.go` — Classify 函数测试，随包删除

需更新的测试（因引用被删除符号，在各 Phase 同步处理）：
- `internal/wire/wire_test.go` — 删除 `fakeClassify`、`ClassifyFn` 类型引用、`GateFn`/`ExecuteFn`/`fakeGate*`/`fakeExecute` 变量及所有使用这些变量的测试用例（Phase E 统一更新签名）
- `internal/daemon/server_test.go` — 删除 `ClassifyFn` 字段引用（`makeServer` 和所有内联 Server 构造），改由 HintedFn 或 RewriteFn 输出直接作为响应（Phase D 统一处理）

### Implementation
1. 删除目录 `internal/executor/`（含 `executor.go`、`executor_test.go`）
2. 删除文件 `internal/intent/classify.go` 和 `internal/intent/classify_test.go`
3. `internal/wire/wire.go` — 删除 `import "github.com/yaleh/voci/internal/executor"` 和 `import "github.com/yaleh/voci/internal/intent"`
4. `internal/wire/wire.go` — 删除类型声明 `ClassifyFn`、`GateFn`、`ExecuteFn`
5. `internal/wire/wire.go` — once 路径删除 Stage 6（classify）、Stage 6b（session routing）、Stage 7（gate）、Stage 8（execute）、Stage 9（print result）；Stage 5b（iterate）后直接调用 `injectFn(rewritten)` 或 `deliverFn(ActionProposal{Rewritten: rewritten})`
6. `internal/wire/wire.go` — serve/daemon/mcp 路径的 `classifyFn` 构造和引用删除（Phase B/C/D 同步完成，但 import 先删）

### DoD
- [ ] `go test ./...`
- [ ] `! grep -rq 'internal/executor' /home/yale/work/voci --include='*.go'`
- [ ] `! grep -rq 'intent\.Classify' /home/yale/work/voci --include='*.go'`
- [ ] `go build ./...`

## Phase B: 删除 mcp 模式
### Tests (write first)
删除的测试文件（随包整体删除）：
- `internal/mcp/server_test.go`
- `internal/mcp/e2e_test.go`
- `internal/mcp/testutil_test.go`

需更新的测试（`internal/wire/wire_test.go`）：
- 删除 `TestRun_SessionIntegrated_StartsServer`、`TestRun_SessionIntegrated_NoFileRequired`、`TestDispatch_McpSubcommand`
- `TestRun_ExitCode` 中删除 `mcp subcommand with injected fn returns 0` case
- 清除 `StartMCPServerFn` 类型引用和 `startMCPServerFn` 参数
- `TestDispatch_UnknownSubcommandErrors` 更新期望字符串（不再含 "mcp"）

### Implementation
1. 删除目录 `internal/mcp/`（含全部 `.go` 文件）
2. `internal/wire/wire.go` — 删除 `import "github.com/yaleh/voci/internal/mcp"`
3. `internal/wire/wire.go` — 删除类型 `StartMCPServerFn`
4. `internal/wire/wire.go` — `dispatch()` 删除 `"mcp"` case；default 错误更新为 `"unknown subcommand %q; use serve or once"`
5. `internal/wire/wire.go` — `run()` 删除 `--session=integrated` 整个 if 块及 `mcpPortFlag`、`sessionFlag` flag 声明
6. `internal/wire/wire.go` — 从 `dispatch()` 和 `run()` 签名删除 `startMCPServerFn StartMCPServerFn` 参数

### DoD
- [ ] `go test ./...`
- [ ] `! ls /home/yale/work/voci/internal/mcp 2>/dev/null`
- [ ] `! grep -rq 'startMCPServerFn\|StartMCPServerFn\|mcp-port\|session=integrated' /home/yale/work/voci/internal/wire/wire.go`
- [ ] `go build ./...`

## Phase C: 删除 daemon flag 和 EventPath 代码路径
### Tests (write first)
需删除的测试（`internal/wire/wire_test.go`）：
- `TestRun_DaemonFlagStartsDaemon`
- `TestRun_DaemonFlagDoesNotRequireFile`
- `TestRun_DaemonPrintsDeprecationNotice`
- `StartDaemonFn` 类型引用和 `startDaemonFn` 参数清除

需更新的测试（`internal/daemon/server_test.go`）：
- 删除 `TestEmit_AlsoAppendsToEventPath` — EventPath 行为删除后此测试删除
- 删除 `TestHandler_AppendsEventPerCall`、`TestTranscribe_DoesNotAppendToEventPath` — EventPath 不再存在
- `makeServer` 函数删除 `eventPath` 参数和 `EventPath` 字段赋值
- 所有调用 `makeServer(t, eventPath)` 的测试改为 `makeServer(t)`

### Implementation
1. `internal/wire/wire.go` — 删除 `daemonFlag`、`daemonPortFlag`、`eventsPathFlag` flag 声明
2. `internal/wire/wire.go` — 删除整个 `if *daemonFlag` 块
3. `internal/wire/wire.go` — 删除类型 `StartDaemonFn`；从 `dispatch()` 和 `run()` 签名删除 `startDaemonFn StartDaemonFn` 参数
4. `internal/wire/wire.go` — serve 路径删除 `EventPath: *eventsPathFlag` 赋值
5. `internal/daemon/server.go` — 删除 `EventPath string` 字段
6. `internal/daemon/handlers.go` — `handleEmit` 中删除 `if s.EventPath != ""` 块（含 `session.AppendEvent` 调用）；确认 `import "github.com/yaleh/voci/internal/daemon/session"` 仍被 `session.Event` 使用故保留

### DoD
- [ ] `go test ./...`
- [ ] `! grep -nq 'daemonFlag\|EventPath\|eventsPathFlag\|StartDaemonFn' /home/yale/work/voci/internal/wire/wire.go`
- [ ] `! grep -nq 'EventPath\|AppendEvent' /home/yale/work/voci/internal/daemon/handlers.go`
- [ ] `go build ./...`

## Phase D: serve 模式去掉 classify；精简 ActionProposal 和 MergedFn 响应
### Tests (write first)
需更新的测试（`internal/daemon/server_test.go`）：
- `makeServer` — 删除 `ClassifyFn` 字段
- `TestHandler_RunsPipelineAndReturnsProposalJSON` — 删除 `proposal.Kind` 断言，断言 `proposal.Rewritten` 和 `proposal.RawTranscript`
- `TestHandler_StillReturnsProposalJSONToHTTP` — 同上，删除 `proposal.Kind` 断言
- `TestHandleTranscribeLogsTimings` — 删除 `"classify:"` 的断言
- `TestHandleTranscribe_FallbackPath` — 删除（ClassifyFn 不再存在）
- `TestHandleTranscribe_MergedPath` — 删除 `ClassifyFn` 字段引用；验证 `Rewritten`/`RawTranscript`
- `TestHandleTranscribePassesLanguage` — 删除 `ClassifyFn`/`RewriteFn` 字段（使用最简 Server）

新增测试（`internal/daemon/server_test.go`）：
- `TestHandleTranscribe_FallbackReturnsHintedOutput` — 验证 MergedFn=nil 时 /transcribe 返回 `{Rewritten: <hinted output>, RawTranscript: <raw>}` 且不调用 classify

需更新的测试（`internal/asr/gemini_test.go`）：
- 断言 `TranscribeMerged` 返回值时，不再断言 `Kind`/`Confidence` 字段

### Implementation
1. `internal/daemon/server.go` — 删除 `ClassifyFn ClassifyFn` 字段和 `ClassifyFn` 类型定义
2. `internal/daemon/handlers.go` — fallback 路径（MergedFn=nil）在 HintedFn 后直接构造 `model.ActionProposal{RawTranscript: raw, Rewritten: hinted}` 返回；删除 `t3`/`classifyMs`/classify 相关代码；日志行删除 `classify: %dms kind=%s` 部分
3. `internal/wire/wire.go` — serve 路径删除 `classifyFn` 构造逻辑和 `ClassifyFn: daemon.ClassifyFn(classifyFn)` 赋值
4. `internal/intent/model/proposal.go` — 删除 `Kind`、`Confidence`、`ContextUsed` 字段；删除 `KindBacklogAction`、`KindQuery`、`KindAmbiguous` 常量；保留 `KindDirectPrompt` 供 handleEmit 默认值使用，或改为字符串字面量后删除整个 `Kind` 类型
5. `internal/asr/gemini.go` — `geminiMergedResult` 删除 `Kind`/`Confidence` 字段；`mergedPromptTemplate` 删除 `kind`/`confidence` 指令；`TranscribeMerged` 返回时仅赋 `RawTranscript`/`Rewritten`
6. `internal/gate/gate.go` — 如引用 `proposal.Kind` 则改为字符串字面量或删除该逻辑（gate 包保留但不被 once 路径调用）

### DoD
- [ ] `go test ./...`
- [ ] `! grep -nq 'ClassifyFn\|classifyFn' /home/yale/work/voci/internal/daemon/handlers.go`
- [ ] `! grep -nq 'ClassifyFn\|classifyFn' /home/yale/work/voci/internal/daemon/server.go`
- [ ] `! grep -nq 'KindBacklogAction\|KindQuery\|KindAmbiguous\|Confidence\b\|ContextUsed' /home/yale/work/voci/internal/intent/model/proposal.go`
- [ ] `go build ./...`

## Phase E: once 模式去掉 classify/gate/executor；清理 wire.go 参数
### Tests (write first)
需删除的测试（`internal/wire/wire_test.go`）：
- `TestRunFullPipelineWithGate`、`TestRunFullPipelineGateDiscard`、`TestCLINoGateFlagSkipsGate` — gate 路径已移除
- `TestRun_SessionFlag_Defaults` — `--session` flag 已删除
- `TestRun_InputDirect_KindDirectPrompt_SkipsGate`、`TestRun_InputDirect_KindQuery_SkipsGate`、`TestRun_InputDirect_KindBacklogAction_UsesGate`、`TestRun_InputDirect_KindAmbiguous_UsesGate` — `--input` flag 和 classify 已删除

需更新的测试（`internal/wire/wire_test.go`）：
- 删除 `fakeClassify`、`fakeGateConfirm`、`fakeGateDiscard`、`fakeExecute` 变量
- 所有 `run()` 和 `dispatch()` 调用同步新签名（移除 classifyFn/gateFn/executeFn/startMCPServerFn/startDaemonFn 参数）
- `TestCLIFileFlagPrintsRAW/HINTED/REWRITTEN`、`TestCLIIterateFlagAccepted` 保留，简化参数
- `TestRun_UsesClaudeCodeAdapter` — 更新：once 路径直接注入 rewritten，deliverFn 仍可验证
- `TestRun_SeparateMode_UsesAdapterHint`、`TestRun_BuildHintFnNil_DoesNotPanic` — 保留，简化参数

新增测试（`internal/wire/wire_test.go`）：
- `TestCLIOnce_InjectsAfterRewrite` — 验证 once 路径在 rewrite 后直接调用 injectFn，不经 gate/classify

### Implementation
1. `internal/wire/wire.go` — once 路径删除 Stage 6-9；Stage 5b（iterate）后直接 `if deliverFn != nil { return deliverFn(ActionProposal{Rewritten: rewritten}) } else if injectFn != nil { return injectFn(rewritten) }`
2. `internal/wire/wire.go` — 删除 `noGateFlag`（`--no-gate`）和 `inputFlag`（`--input`）flag 声明
3. `internal/wire/wire.go` — 删除对 `gate` 包的 import（`internal/gate`）
4. `internal/wire/wire.go` — `run()` 签名最终删除 `gateFn GateFn`、`executeFn ExecuteFn` 参数（已在 Phase A 删除类型）

### DoD
- [ ] `go test ./...`
- [ ] `! grep -nq 'gateFn\|executeFn\|noGateFlag\|inputFlag' /home/yale/work/voci/internal/wire/wire.go`
- [ ] `! grep -nq 'gate\.Run\|gate\.PrintSummary\|internal/gate' /home/yale/work/voci/internal/wire/wire.go`
- [ ] `go build ./...`

## Phase F: 前端 recorder.js 同步；model/intent 包最终清理
### Tests (write first)
前端 JS 变更无对应 Go 单元测试；确认现有 Go 测试不受影响：
- `internal/daemon/static_test.go` — 检查是否有内容断言；若无则无需改动
- `internal/daemon/transcriber_adapter_test.go` — 检查是否引用 `ActionProposal` 字段；若仅用 `Rewritten` 则无需改动

若 `internal/intent/` 目录在 classify.go 删除后为空（`model/` 是子包独立存在），检查并删除空的 `internal/intent/` 层级（`model/` 仍保留于 `internal/intent/model/`）。

### Implementation
1. `internal/daemon/web/recorder.js` — `doTranscribe` 回调删除 `var kind = p.Kind || 'direct_prompt'` 和 `var ambig = kind === 'ambiguous'`；将 `if (ambig)` 分支改为 `if (!rew)` 检查
2. `internal/wire/wire.go` — 最终 import 审查，删除所有不再使用的包导入
3. 运行 `go vet ./...` 确认无幽灵引用

### DoD
- [ ] `go test ./...`
- [ ] `! grep -nq 'ambig\|p\.Kind' /home/yale/work/voci/internal/daemon/web/recorder.js`
- [ ] `go build ./...`
- [ ] `go vet ./...`

## Constraints
- gate 包本身保留（`internal/gate/` 目录不删除），只是 once 路径不再调用它；wire.go 不再 import gate 包
- serve 模式的 MergedFn（TASK-64 核心逻辑）保留；仅精简 `mergedPromptTemplate` 中的 `kind`/`confidence` 指令和 `geminiMergedResult` 对应字段
- handleEmit 保留 kind 字段接收（前端仍可传固定值 `"direct_prompt"`，事件日志仍记录）；变化是 /transcribe 响应不再含 kind，前端不再从响应读取 kind
- `ActionProposal.RawTranscript` 和 `ActionProposal.Rewritten` 字段保留；adapter 包的 `Deliver(p model.ActionProposal)` 接口不变
- 每 Phase 结束后 `go build ./...` 和 `go test ./...` 必须通过
- 测试覆盖率各包 ≥80% 阈值维持（executor/classify 测试删除后分母同步缩小，比例不下降）
- `internal/intent/model/proposal.go` 保留为独立叶包

## Acceptance Gate
- [ ] `go test ./...`
- [ ] `go build ./...`
- [ ] `! ls /home/yale/work/voci/internal/mcp 2>/dev/null && echo "mcp deleted OK"`
- [ ] `! ls /home/yale/work/voci/internal/executor 2>/dev/null && echo "executor deleted OK"`
- [ ] `! grep -rq 'intent\.Classify\|ClassifyFn\|KindBacklogAction\|KindAmbiguous\|KindQuery' /home/yale/work/voci --include='*.go'`
- [ ] `! grep -nq 'daemonFlag\|EventPath\|--daemon' /home/yale/work/voci/internal/wire/wire.go`
- [ ] `! grep -nq 'ambig\|p\.Kind' /home/yale/work/voci/internal/daemon/web/recorder.js`
- [ ] `go test -coverprofile=/tmp/cover.out ./... && go tool cover -func=/tmp/cover.out | tail -1`
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Proposal self-review: APPROVED
premise-ledger:
[E] wire.go:387 prints deprecation warning for --daemon mode
[E] handleEmit sends hardcoded kind:"direct_prompt" regardless of ClassifyFn result
[E] internal/mcp/ has no callers outside wire.go (grep confirms)
[E] executor package only imported in wire.go (grep confirms)
[E] recorder.js line 379: p.Kind || 'direct_prompt' — graceful fallback
[E] handlers.go:105 calls ClassifyFn in serve path (confirmed by read)
[C] daemon mode is deprecated and unused → removal is safe
[C] classify result ignored by Web UI in serve → removing it cuts latency with no feature loss
[C] executor dispatches only on Kind → removing classify makes executor dead code
[C] recorder.js fallback ensures frontend degrades gracefully without classify
[H] once-without-gate acceptable for supervised local operation
[H] proposed deletion order minimizes intermediate compile failures
GCL-self-report: E=6 C=4 H=2

Proposal approved. Starting plan draft.

Plan review iteration 1: APPROVED
premise-ledger:
[E] Goal coverage: all 5 proposal Goals mapped to Phase A or Phase B items
[E] TDD structure: both phases have ### Tests before ### Implementation
[E] TDD order: first DoD item in each phase is `go test ./...`
[E] Acceptance gate: first item is `go test ./...`
[E] DoD executability: all DoD and Acceptance Gate items are shell commands
[E] Absence checks: `! grep -q` pattern used (not `grep -qv`)
[E] Phase ordering: Phase A (Go) before Phase B (Skill); no circular deps
[E] Scope discipline: both phases directly implement proposal Goals
[E] File paths: internal/wire/wire.go and .claude/skills/voci-listen/SKILL.md both verified to exist
GCL-self-report: E=9 C=0 H=0

Note: original plan was completely wrong — described mcp/daemon/classify removal (a different task). Plan was fully rewritten to match proposal Goals.

Plan review iteration 1: result discarded — review agent read TASK-69 proposal instead of TASK-68 proposal due to /tmp/ftb-proposal.md file collision. Plan restored from conversation context. Status advanced to Basic: Backlog.

claimed: 2026-06-30T23:11:44Z
<!-- SECTION:NOTES:END -->

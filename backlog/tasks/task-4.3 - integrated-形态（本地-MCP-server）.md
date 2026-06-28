---
id: TASK-4.3
title: integrated 形态（本地 MCP server）
status: 'Basic: Done'
assignee: []
created_date: '2026-06-28 04:57'
updated_date: '2026-06-28 05:24'
labels:
  - 'kind:basic'
dependencies: []
modified_files:
  - internal/mcp/server.go
  - internal/mcp/server_test.go
  - internal/mcp/types.go
  - cmd/voci/main.go
  - cmd/voci/main_test.go
parent_task_id: TASK-4
ordinal: 1000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
实现 `internal/mcp/server.go`：在 `127.0.0.1` 上监听本地 HTTP 端口（或 Unix socket），暴露 `mcp__voci__transcribe(audio_path string)` 工具，接收调用后触发完整 ASR→RunHinted→Rewrite→Classify→gate→返回 ActionProposal JSON pipeline；`--session=integrated` 时 `cmd/voci` 以 MCP server 模式启动而非 CLI 模式；含集成测试（httptest.NewServer mock，验证返回值格式及各 pipeline 阶段被调用）。MCP server 只监听 localhost，不对外暴露端口。
<!-- SECTION:DESCRIPTION:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
<!-- proposal -->
# Proposal: integrated 形态（本地 MCP server）

## Background
voci 在 separate 形态中以 CLI 工具运行，每次调用需 ASR→pipeline→gate 全流程后返回结果，用于 TASK-4.2 的 tmux 注入。而在 integrated 形态中，Claude Code 需要直接把 voci 作为 MCP server 调用——Claude Code 通过 `mcp__voci__transcribe(audio_path)` 发出工具调用，voci 在同进程内完成 ASR→RunHinted→Rewrite→Classify 流程，把 ActionProposal 以 JSON 格式返回给 Claude Code，由 Claude Code 自身完成 gate 决策（而非 voci 端交互式确认）。当前 voci 只有 CLI 形态，无法被 MCP 协议调用，需要实现标准 MCP HTTP/JSON-RPC 接口及 `--session=integrated` 启动路径。

## Goals
1. `internal/mcp/server.go` 实现 `Server` struct，启动 HTTP server 监听 `127.0.0.1:<port>`，响应 MCP JSON-RPC 的 `tools/list` 和 `tools/call` 请求。
2. `tools/list` 返回包含 `mcp__voci__transcribe` 工具描述的 JSON；`tools/call` 接收 `audio_path` 参数，触发完整 ASR→RunHinted→Rewrite→Classify pipeline，返回 `ActionProposal` 的 JSON 序列化结果。
3. MCP server 仅监听 `127.0.0.1`（不绑定 `0.0.0.0`），端口通过 `--mcp-port` 标志配置（默认 9473）。
4. `--session=integrated` 时 `cmd/voci/main.go` 启动 MCP server 模式（而非执行 CLI pipeline）；gate 逻辑在 MCP 模式下不运行（返回 ActionProposal 供调用方决策）。
5. 集成测试使用 `httptest.NewServer` 验证：`tools/list` 响应格式、`tools/call` 触发 pipeline（通过注入 mock ASR/pipeline 函数验证各阶段被调用）、返回的 ActionProposal JSON 格式正确。

## Proposed Approach
在 `internal/mcp/server.go` 中定义 `Server` struct，依赖注入 `TranscribeFn`、`HintedFn`、`RewriteFn`、`ClassifyFn`（与 `cmd/voci/main.go` 中现有类型一致）；`Start(addr string) error` 注册路由并调用 `http.ListenAndServe`；路由只有两个：`POST /` 处理 MCP JSON-RPC 协议（通过 `method` 字段路由到 `tools/list` 或 `tools/call` 处理器）。MCP JSON-RPC 格式参照 MCP spec v0.1：请求 `{"jsonrpc":"2.0","id":N,"method":"tools/call","params":{"name":"mcp__voci__transcribe","arguments":{"audio_path":"..."}}}` → 响应 `{"jsonrpc":"2.0","id":N,"result":{"content":[{"type":"text","text":"<JSON>"}]}}`。`cmd/voci/main.go` 在 `--session=integrated` 时调用 `mcp.NewServer(cfg, chatFn).Start(addr)` 并阻塞（Ctrl+C 退出）。

## Trade-offs and Risks
- **不做**：不实现 Unix socket 形态（HTTP 端口足够，且测试更简单）；不实现 MCP SSE streaming（工具调用为一次性 request-response）。
- **不做**：MCP server 模式不运行 gate（gate 是人机交互逻辑，在 MCP 调用链中由 Claude Code 的 Tool Use 机制处理）。
- **风险**：MCP JSON-RPC 协议细节（`initialize`、`notifications/initialized` 握手）若 Claude Code 需要，需额外处理；本次先实现核心 `tools/list`/`tools/call` 路径，其余方法返回空成功响应。
- **风险**：`httptest.NewServer` 测试不覆盖真实 Claude Code MCP 客户端，协议兼容性需后续集成验证。

---

# Plan: integrated 形态（本地 MCP server）

## Phase A: MCP JSON-RPC 协议层 + Server struct

### Tests (write first)
File: `internal/mcp/server_test.go`
- `TestServer_ToolsList` — httptest.NewServer 启动 Server，POST `{"jsonrpc":"2.0","id":1,"method":"tools/list"}` 断言响应含 `mcp__voci__transcribe` tool 描述
- `TestServer_UnknownMethod` — POST 未知 method，断言返回 JSON-RPC error（code -32601）
- `TestServer_MalformedRequest` — POST 非 JSON body，断言返回 400 或 JSON-RPC parse error
- `TestServer_InitializeMethod` — POST `initialize`，断言返回空 result（占位握手）

### Implementation
Files to create:
- `internal/mcp/server.go` — `Server` struct（含 `TranscribeFn`、`HintedFn`、`RewriteFn`、`ClassifyFn` 字段）、`NewServer()` 构造函数、`Handler()` 返回 `http.Handler`、`tools/list` 响应、JSON-RPC 请求/响应类型
- `internal/mcp/types.go` — MCP JSON-RPC 请求/响应/error 结构体

### DoD
- [ ] `go test ./internal/mcp/... -run TestServer_ToolsList`
- [ ] `go test ./internal/mcp/... -run TestServer_UnknownMethod`
- [ ] `go test ./internal/mcp/... -run TestServer_MalformedRequest`

## Phase B: tools/call — pipeline 集成

### Tests (write first)
File: `internal/mcp/server_test.go`（追加）
- `TestServer_ToolsCall_HappyPath` — 注入 mock TranscribeFn/HintedFn/RewriteFn/ClassifyFn，POST `tools/call` with `audio_path`，断言各 mock 被调用，响应 content[0].text 可反序列化为 `ActionProposal` JSON，含 Kind/Rewritten 字段
- `TestServer_ToolsCall_WrongToolName` — name 非 `mcp__voci__transcribe`，断言返回 JSON-RPC error
- `TestServer_ToolsCall_MissingAudioPath` — arguments 缺 audio_path，断言返回 JSON-RPC error（invalid params）
- `TestServer_ToolsCall_ASRError` — mock TranscribeFn 返回 error，断言响应 JSON-RPC error（code -32603）

### Implementation
Files to modify:
- `internal/mcp/server.go` — 添加 `tools/call` 处理器；实现 `handleTranscribe` 调用 pipeline 四阶段；`ActionProposal` 以 `json.Marshal` 序列化后放入 content[0].text

### DoD
- [ ] `go test ./internal/mcp/... -run TestServer_ToolsCall`
- [ ] `go test ./internal/mcp/...`

## Phase C: cmd/voci 集成 --session=integrated 启动路径

### Tests (write first)
File: `cmd/voci/main_test.go`（追加，若 TASK-4.2 已添加此文件则继续追加）
- `TestRun_SessionIntegrated_StartsServer` — 注入 mock `startMCPServerFn` 记录调用，传 `--session=integrated --mcp-port=0`，断言 mock 被调用且 `--file` 未被要求
- `TestRun_SessionIntegrated_PortFlag` — 传 `--mcp-port=19473`，断言 startMCPServerFn 收到正确 addr 参数

### Implementation
Files to modify:
- `cmd/voci/main.go` — 添加 `--mcp-port` 标志；`--session=integrated` 路径调用 `startMCPServerFn`（可注入，默认实现调用 `mcp.NewServer(...).Start(addr)`）；`--file` 在 integrated 模式下不为必填

### DoD
- [ ] `go test ./cmd/voci/... -run TestRun_SessionIntegrated`
- [ ] `go test ./cmd/voci/...`
- [ ] `go test ./...`

## Constraints
- MCP server 只绑定 `127.0.0.1`，不接受 `0.0.0.0` 地址
- gate 逻辑（`gate.Run`）在 MCP server 模式下不运行
- `initialize` 握手请求返回空 result（非 error），以满足 MCP 客户端协议要求

## Acceptance Gate
- [ ] `go test ./...`
- [ ] `! grep -q 'ListenAndServe.*0.0.0.0' internal/mcp/server.go`
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Proposal self-review: APPROVED
premise-ledger:
[E] background lines: 5行，直接数
[E] goal verifiability: 5条 Goal 均有可检验的行为（HTTP端口/JSON格式/测试）
[E] approach alignment: 与 cmd/voci/main.go TranscribeFn/RewriteFn 等类型直接对应
[C] MCP 协议格式: 参照 MCP spec v0.1 JSON-RPC 格式需查阅外部 spec
[H] httptest 充分性: 不覆盖真实 MCP 客户端，靠背景知识判断元测试足够
GCL-self-report: E=3 C=1 H=1

Plan review iteration 1: APPROVED
premise-ledger:
[E] goal coverage: 5 Goals 均有对应 Phase A/B/C 或 Acceptance Gate
[E] TDD structure: 每 Phase 均有 ### Tests 先于 ### Implementation
[E] DoD[0] uses go test: Phase A/B/C 第一项均以 go test 开头
[E] Acceptance Gate[0] is go test ./...: 直接读取
[C] file paths: internal/mcp/ 新建目录，cmd/voci/main.go 已确认存在
[H] MCP 协议兼容性: httptest 测试足够性靠背景知识判断
GCL-self-report: E=4 C=1 H=1

claimed: 2026-06-28T05:18:50Z

## Execution Summary
Result: Done
Commit: fe09d5b
All 9 DoD checks passed. Security: 0.0.0.0 binding absent.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
## Execution Summary
Result: Done
Commit: 2810fd5

### What was implemented
- **internal/mcp/types.go**: JSON-RPC 2.0 types (Request, Response, RPCError, Tool)
- **internal/mcp/server.go**: MCP HTTP server with Handler()/Start() methods; routes `tools/list`, `tools/call`, `initialize`, `notifications/initialized`; `mcp__voci__transcribe` tool runs full ASR→RunHinted→Rewrite→Classify pipeline
- **internal/mcp/server_test.go**: 8 tests covering all phases (ToolsList, UnknownMethod, MalformedRequest, InitializeMethod, ToolsCall happy path, wrong tool name, missing audio_path, ASR error)
- **cmd/voci/main.go**: Added `StartMCPServerFn` type, `startMCPServerFn` param to `run()`, `--mcp-port` flag (default 9473), integrated mode startup path binding to `127.0.0.1:<port>`; removed old "not yet implemented" error
- **cmd/voci/main_test.go**: Updated all existing `run()` calls to new 11-arg signature; replaced `TestRun_SessionIntegrated_ReturnsError` with `TestRun_SessionIntegrated_StartsServer` and `TestRun_SessionIntegrated_NoFileRequired`

All acceptance gates passed: `go test ./...`, `go build ./cmd/voci`, `go vet ./...`, no 0.0.0.0 binding.
<!-- SECTION:FINAL_SUMMARY:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 go test ./internal/mcp/... -run TestServer_ToolsList
- [ ] #2 go test ./internal/mcp/... -run TestServer_UnknownMethod
- [ ] #3 go test ./internal/mcp/... -run TestServer_MalformedRequest
- [ ] #4 go test ./internal/mcp/... -run TestServer_ToolsCall
- [ ] #5 go test ./internal/mcp/...
- [ ] #6 go test ./cmd/voci/... -run TestRun_SessionIntegrated
- [ ] #7 go test ./cmd/voci/...
- [ ] #8 go test ./...
- [ ] #9 ! grep -q 'ListenAndServe.*0.0.0.0' internal/mcp/server.go
<!-- DOD:END -->

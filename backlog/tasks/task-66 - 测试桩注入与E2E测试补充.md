---
id: TASK-66
title: 测试桩注入与E2E测试补充
status: 'Basic: Needs Human'
assignee: []
created_date: '2026-06-30 16:07'
updated_date: '2026-06-30 16:22'
labels:
  - 'kind:basic'
dependencies: []
ordinal: 1000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
为 voci 项目补充测试桩（StartTunnel cmdFactory、openCloudflaredLog homeDir 注入、NewSessionID rand reader 注入），并新增 4 类 E2E 测试（CLI once 真实音频管道、MCP 独立子进程、session 进程生命周期、voci serve Monitor 输出验证），以提升 tunnel/wire 包覆盖率和端到端验证能力。
<!-- SECTION:DESCRIPTION:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
# Plan: 测试桩注入与E2E测试补充

## Context

TASK-65 将 asr 提升至 85.2%、session 提升至 85.6%，但 tunnel (71.9%) 和 wire (63.2%) 因缺乏测试桩注入点而停滞。同时项目缺少真实子进程级别的 E2E 测试——当前 E2E 大多用 `httptest.NewServer` 跳过 `run()` 完整 wiring。本任务补全测试桩 + E2E 测试，不改变生产行为。

## Phase 1: StartTunnel cmdFactory 测试桩

新增 `var StartTunnelCmdFactory` 包级测试钩子，使 `StartTunnel` 的成功路径可单测。

### Instructions

- 在 `internal/daemon/tunnel/tunnel.go` 中新增包级变量：
  `var StartTunnelCmdFactory func(ctx context.Context, bin string, args ...string) *exec.Cmd`
- 修改 `StartTunnel` 函数：在 `exec.CommandContext(ctx, bin, ...)` 调用前检查 `StartTunnelCmdFactory != nil`，若设置则使用它
- 在 `internal/daemon/tunnel/tunnel_test.go` 新增两个测试：
  1. `TestStartTunnel_Success` — 注入 fake cmd (exec.Command("true"))，验证返回 URL 正确、err == nil
  2. `TestStartTunnel_StartError` — 注入 fake cmd 使 Start() 失败，验证 err != nil
- Fake cmd 使用 `exec.Command("true")` 并通过替换 PATH 让 drainStderr 收到 "https://xyz.trycloudflare.com" 行

### DoD

- [ ] `go test ./internal/daemon/tunnel/... -run TestStartTunnel_Success`
- [ ] `go test ./internal/daemon/tunnel/... -run TestStartTunnel_StartError`
- [ ] `go test ./internal/daemon/tunnel/...`
- [ ] `go test -coverprofile=/tmp/cover-tunnel.out ./internal/daemon/tunnel/... && go tool cover -func=/tmp/cover-tunnel.out | grep total | awk '{if ($3+0 < 80) exit 1}'`

## Phase 2: openCloudflaredLog homeDir 注入

为 `openCloudflaredLog` 添加 home 目录注入，覆盖 wire 包中日志文件创建路径。

### Instructions

- 在 `internal/wire/wire.go` 新增：
  `var openCloudflaredLogHome string` — 非空时替代 `os.UserHomeDir()`
- 修改 `openCloudflaredLog` 函数：优先使用 `openCloudflaredLogHome`
- 在 `internal/wire/wire_test.go` 新增 `TestOpenCloudflaredLog_WritesToFile`：
  - 设置 `openCloudflaredLogHome` 为 `t.TempDir()`
  - 调用 `openCloudflaredLog()` 获取 writer 和 closer
  - 写入一行日志，closer()
  - 验证 `cloudflared.log` 文件存在且内容正确
  - defer 恢复 `openCloudflaredLogHome = ""`

### DoD

- [ ] `go test ./internal/wire/... -run TestOpenCloudflaredLog_WritesToFile`
- [ ] `go test ./internal/wire/...`

## Phase 3: CLI once 真实音频 E2E 测试

启动 `voci` 子进程，使用 testdata 真实音频文件，验证完整管道输出。

### Instructions

- 新建 `internal/wire/cli_e2e_test.go`（build tag `e2e`）
- 测试 `TestE2E_CLIOnce_RealAudio`：
  1. `go build -o /tmp/voci-e2e ./cmd/voci`
  2. 启动 fake Ollama httptest.Server 返回固定 chat 响应
  3. 启动 fake Gemini httptest.Server 返回转录 JSON
  4. 设置 `OLLAMA_HOST`、`ASR_API_KEY=test-key`、`ASR_PROVIDER=gemini` 环境变量
  5. 通过 `VOCI_CONFIG` 或 env 注入 fake server URL
  6. 运行 `/tmp/voci-e2e --file testdata/sample-01.wav --no-gate`
  7. 验证 stdout 包含 "RAW"、"HINTED"、"REWRITTEN"
  8. 验证退出码为 0

### DoD

- [ ] `go test -tags=e2e ./internal/wire/... -run TestE2E_CLIOnce_RealAudio -v`
- [ ] `test -f internal/wire/cli_e2e_test.go`

## Phase 4: MCP + Session + Monitor E2E 测试

新增三个独立 E2E 测试：MCP 子进程启动、session 进程生命周期、Monitor 输出验证。

### Instructions

**4a.** 新建 `internal/mcp/subprocess_e2e_test.go`（build tag `e2e`），测试 `TestE2E_MCP_Subprocess`：
- `go build -o /tmp/voci-e2e ./cmd/voci`
- 设置 `ASR_API_KEY=test-key`，`OLLAMA_HOST` 指向 fake server
- 启动 `/tmp/voci-e2e mcp --mcp-port=0`，从 stderr 提取实际端口
- 发送 JSON-RPC `initialize`、`tools/list` 请求
- 验证响应合法

**4b.** 追加到 `internal/daemon/session/monitor_task_e2e_test.go`，测试 `TestE2E_SessionLifecycle_ProcessDeath`：
- `os.StartProcess` 启动 `sleep 10` 作为测试 PID
- `WriteLock` + `WriteStatus` 使用该 PID
- `SweepStaleLocks` 验证 lock 未被删除（进程存活）
- `syscall.Kill(pid, syscall.SIGKILL)` 杀进程
- `SweepStaleLocks` 验证 lock 被删除

**4c.** 追加到 `internal/wire/e2e_test.go`，测试 `TestE2E_Serve_MonitorOutput`：
- 启动 `voci serve --serve-port=0` 子进程
- POST fake WAV 到 `/api/voice/transcribe`
- POST emit 到 `/api/voice/emit`
- 读取子进程 stdout，验证输出合法 JSONL

### DoD

- [ ] `go test -tags=e2e ./internal/mcp/... -run TestE2E_MCP_Subprocess -v`
- [ ] `go test -tags=e2e ./internal/daemon/session/... -run TestE2E_SessionLifecycle_ProcessDeath -v`
- [ ] `go test -tags=e2e ./internal/wire/... -run TestE2E_Serve_MonitorOutput -v`

## Phase 5: NewSessionID rand reader 注入（低优先级）

### Instructions

- 在 `internal/daemon/session/lock.go` 新增：
  `var randRead func([]byte) (int, error)` — 非 nil 时替代 `crypto/rand.Read`
- 修改 `NewSessionID`：若 `randRead != nil` 则使用它
- 在 `internal/daemon/session/lock_test.go` 新增 `TestNewSessionID_RandError_Panics`：
  - 注入 `randRead` 返回 error，用 `defer recover()` 验证 panic

### DoD

- [ ] `go test ./internal/daemon/session/... -run TestNewSessionID_RandError_Panics`
- [ ] `go test ./internal/daemon/session/...`

## Constraints

- 所有测试桩使用 `var` 包级变量模式（遵循现有 `geminiMergedTestBaseURL` 风格），不引入接口或依赖注入框架
- 测试桩仅在 `_test.go` 文件中设置，生产代码仅读取
- E2E 测试使用 `//go:build e2e` 标签，不阻塞 `go test ./...`
- `openCloudflaredLog` 的 homeDir 注入仅用于测试；真实代码路径行为不变
- 不修改 `StartManagedTunnel`（wire 层已有 `StartManagedTunnelFn` 注入）

## Acceptance Gate

- [ ] `go test ./...`
- [ ] `go test -tags=e2e ./...`
- [ ] `go test -coverprofile=/tmp/cover-final.out ./... && go tool cover -func=/tmp/cover-final.out | tail -1`
- [ ] `go test -coverprofile=/tmp/cover-tunnel.out ./internal/daemon/tunnel/... && go tool cover -func=/tmp/cover-tunnel.out | grep total | awk '{if ($3+0 < 80) exit 1}'`
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
cap:propose=approved

claimed: 2026-06-30T16:11:40Z

workerLoop DoD #0: PASS — go test ./internal/daemon/tunnel/... -run TestStartTunnel_Success

workerLoop DoD #1: PASS — go test ./internal/daemon/tunnel/... -run TestStartTunnel_StartError

workerLoop DoD #2: PASS — go test ./internal/daemon/tunnel/...

workerLoop DoD #3: FAIL — tunnel coverage 74.4% < 80% threshold. Reason: StartManagedTunnel inherently creates real cloudflared processes; explicitly excluded from this task's scope. Pre-existing: internal/daemon/tunnel_e2e_test.go has build errors (wrong package, undefined applyChildAttrs) unrelated to this task.

Escalated: workerLoop DoD #3 failed — tunnel coverage 74.4% below 80% threshold. The agent completed all 5 phases and all unit tests pass. The remaining coverage gap is in StartManagedTunnel (requires real cloudflared, explicitly out of scope). To continue: answer in Implementation Notes, then set status → Basic: Ready.
<!-- SECTION:NOTES:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 go test ./internal/daemon/tunnel/... -run TestStartTunnel_Success
- [ ] #2 go test ./internal/daemon/tunnel/... -run TestStartTunnel_StartError
- [ ] #3 go test ./internal/daemon/tunnel/...
- [ ] #4 go test -coverprofile=/tmp/cover-tunnel.out ./internal/daemon/tunnel/... && go tool cover -func=/tmp/cover-tunnel.out | grep total | awk '{if ($3+0 < 80) exit 1}'
- [ ] #5 go test ./internal/wire/... -run TestOpenCloudflaredLog_WritesToFile
- [ ] #6 go test ./internal/wire/...
- [ ] #7 go test -tags=e2e ./internal/wire/... -run TestE2E_CLIOnce_RealAudio -v
- [ ] #8 test -f internal/wire/cli_e2e_test.go
- [ ] #9 go test -tags=e2e ./internal/mcp/... -run TestE2E_MCP_Subprocess -v
- [ ] #10 go test -tags=e2e ./internal/daemon/session/... -run TestE2E_SessionLifecycle_ProcessDeath -v
- [ ] #11 go test -tags=e2e ./internal/wire/... -run TestE2E_Serve_MonitorOutput -v
- [ ] #12 go test ./internal/daemon/session/... -run TestNewSessionID_RandError_Panics
- [ ] #13 go test ./internal/daemon/session/...
- [ ] #14 go test ./...
- [ ] #15 go test -tags=e2e ./...
- [ ] #16 go test -coverprofile=/tmp/cover-final.out ./... && go tool cover -func=/tmp/cover-final.out | tail -1
- [ ] #17 go test -coverprofile=/tmp/cover-tunnel.out ./internal/daemon/tunnel/... && go tool cover -func=/tmp/cover-tunnel.out | grep total | awk '{if ($3+0 < 80) exit 1}'
<!-- DOD:END -->

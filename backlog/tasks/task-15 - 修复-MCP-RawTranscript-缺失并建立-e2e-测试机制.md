---
id: TASK-15
title: 修复 MCP RawTranscript 缺失并建立 e2e 测试机制
status: 'Basic: Done'
assignee: []
created_date: '2026-06-28 08:23'
updated_date: '2026-06-28 09:40'
labels:
  - 'kind:basic'
dependencies: []
ordinal: 13000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
修复 MCP server 中 ActionProposal.RawTranscript 字段永远为空的 bug（server.go toolsCall 未将 ASR raw 回写到 proposal），并建立不依赖运行 Claude Code 的 MCP e2e 测试机制（直接启动 HTTP server 进行端到端测试）。
<!-- SECTION:DESCRIPTION:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
# Proposal: 修复 MCP RawTranscript 缺失并建立 e2e 测试机制

## Background

`ActionProposal.RawTranscript` 字段的设计意图是保存原始 ASR 输出，供下游消费者（如人工审核、调试工具、日志系统）在不重新调用 ASR 的情况下追溯原始语音识别文本。然而该字段在整个调用链中从未被写入：`toolsCall()` 在调用 `TranscribeFn` 后得到 `raw` 字符串，但在 `ClassifyFn` 返回 `proposal` 之后没有执行 `proposal.RawTranscript = raw`；`Classify()` 本身的签名也不接收原始文本，故无法在内部填充。结果是每次通过 MCP 工具调用返回的 `ActionProposal` JSON 中 `RawTranscript` 始终为空字符串，消费者无法区分"语音识别结果为空"与"字段未被填充"这两种情况。

现有的 `server_test.go` 均为单元测试，使用桩函数隔离每一层，无法验证各层之间的数据传递是否正确（正是这个原因导致该 bug 长期未被发现）。建立一套不依赖 Claude Code 进程运行的 e2e 测试机制，能够覆盖完整 HTTP → 管道 → 响应路径，是防止同类回归的必要条件。

## Goals

1. `toolsCall()` 在 `ClassifyFn` 返回后，将 `raw`（TranscribeFn 的返回值）赋值给 `proposal.RawTranscript`，使得返回的 JSON 中 `RawTranscript` 字段与 ASR 原始文本一致。
2. 在 `internal/mcp/server_test.go` 中新增断言：当 `TranscribeFn` 返回已知字符串时，响应 JSON 中的 `RawTranscript` 字段等于该字符串（零外部依赖，纯 Go 单元测试）。
3. 在 `internal/mcp/` 中新增 `e2e_test.go`（build tag `e2e`），使用 `httptest.NewServer` 启动真实 MCP HTTP 服务，注入确定性桩函数替代 SiliconFlow API 和 LLM，通过 HTTP POST 发送完整 JSON-RPC 请求并断言响应结构与字段值；该套件可通过 `go test -tags e2e` 在任何开发机上运行，不依赖 Claude Code 或任何外部网络服务。
4. 桩函数与辅助构造器封装在 `internal/mcp/testutil_test.go` 中，可被 `server_test.go` 与 `e2e_test.go` 共享，避免重复定义桩逻辑。

## Proposed Approach

**修复（server.go）**

在 `toolsCall()` 中，`ClassifyFn` 调用成功后添加一行：

```go
proposal.RawTranscript = raw
```

这是唯一需要修改的生产代码行。`Classify()` 不需要变更，因为 `RawTranscript` 属于管道协调层（server）的职责，而非分类层的职责——分类层不知道也不应知道 ASR 的原始输出。

**回归测试（server_test.go）**

在现有 `TestServer_ToolsCall_HappyPath` 测试中，补充对 `RawTranscript` 字段的断言：将 `TranscribeFn` 的返回值设为可辨识的字面量（如 `"raw-asr-output"`），在解析响应 JSON 后断言 `proposal["RawTranscript"] == "raw-asr-output"`。

**e2e 测试（e2e_test.go，build tag: e2e）**

- 用 `httptest.NewServer(srv.Handler())` 启动服务器，`srv` 通过 `NewServer(...)` 注入四个确定性桩函数；
- 测试覆盖：(a) 完整 happy-path，验证 `Kind`、`Rewritten`、`RawTranscript` 字段；(b) ASR 桩返回错误时，HTTP 响应携带 JSON-RPC error code -32603；(c) `audio_path` 缺失时，返回 -32602；
- 不创建真实 WAV 文件，不调用任何网络，不依赖 Ollama / Claude Code 进程。

**testutil（testutil_test.go）**

将 `newTestServer()`、`postJSON()`、`decodeResponse()` 从 `server_test.go` 迁移至 `testutil_test.go`，同时新增 `newDeterministicServer(rawASR, rewritten string) *Server` 辅助函数，供两个测试文件复用。

## Trade-offs and Risks

- **赋值位置选择**：将 `RawTranscript = raw` 放在 server 层而非 classify 层，保持了 `Classify()` 签名稳定，但意味着若将来有其他调用 `Classify()` 的路径（非 MCP），开发者需要记得手动填充该字段。可通过代码注释说明此约定来缓解。
- **build tag 隔离**：e2e 测试使用 `//go:build e2e` 标签，默认 `go test ./...` 不会执行，避免 CI 中的偶发失败，但也意味着 e2e 套件必须在 CI 流水线中显式启用，否则覆盖率统计会遗漏这部分。
- **桩函数的代表性**：桩函数跳过了真实 ASR 网络调用和 LLM 推理，因此无法发现 SiliconFlow API 协议变更或 LLM 输出格式变化引起的回归；这部分风险由上层集成测试或人工验收覆盖。
- **变更范围极小**：生产代码仅改一行，风险极低；现有单元测试在修复前后均可通过（仅新增断言会在修复前失败），可作为验证手段。

---

# Plan: 修复 MCP RawTranscript 缺失并建立 e2e 测试机制

Proposal: docs/proposals/proposal-mcp-rawtranscript-e2e.md

## Phase A: 修复 RawTranscript 字段赋值 + 单元测试回归断言

### Tests (write first)

- File: `internal/mcp/server_test.go`
- Add test case (or extend existing happy-path test) that:
  - Sets `TranscribeFn` stub to return a recognizable literal, e.g. `"raw-asr-output"`
  - Parses the JSON-RPC response and decodes the inner `ActionProposal`
  - Asserts `proposal.RawTranscript == "raw-asr-output"`
- Run `go test ./internal/mcp/...` — **expect RED** (RawTranscript always empty before fix)

### Implementation

- File: `internal/mcp/server.go`
- After `proposal, err := s.ClassifyFn(...)` returns successfully (line ~195), add:
  ```go
  proposal.RawTranscript = raw
  ```
- Run `go test ./internal/mcp/...` — **expect GREEN**

### DoD

- [ ] `go test ./...`
- [ ] `grep -q 'RawTranscript' internal/mcp/server.go`
- [ ] `grep -q 'RawTranscript' internal/mcp/server_test.go`

---

## Phase B: e2e 测试套件（build tag: e2e）

### Tests (write first)

- File: `internal/mcp/e2e_test.go` — build tag `//go:build e2e`
  - Test cases:
    1. **happy-path full pipeline**: POST valid `tools/call` JSON-RPC request → assert HTTP 200, response contains `RawTranscript`, `Kind`, `Rewritten` with expected stub values
    2. **ASR error → -32603**: inject `TranscribeFn` stub that returns `errors.New("asr failed")` → assert JSON-RPC error code `-32603`
    3. **missing audio_path → -32602**: omit `audio_path` param → assert JSON-RPC error code `-32602`
  - All tests use `httptest.NewServer` to spin up the real HTTP handler; no real WAV files, no network calls

- File: `internal/mcp/testutil_test.go` — shared helpers (no build tag, available to all test files in package)
  - `newDeterministicServer(rawASR, rewritten string) *Server` — constructs `*Server` with four deterministic stubs
  - `postJSON(t, ts, body string) *http.Response` — migrated from current `server_test.go`
  - `decodeResponse(t, resp) Response` — migrated from current `server_test.go`
  - `decodeProposal(t, r Response) intent.ActionProposal` — extracts and JSON-decodes the inner proposal from the MCP result content

### Implementation

- Create `internal/mcp/e2e_test.go` with `//go:build e2e` and the three test cases above
- Create `internal/mcp/testutil_test.go`:
  - Move `newTestServer()`, `postJSON()`, `decodeResponse()` out of `server_test.go`
  - Add `newDeterministicServer()` and `decodeProposal()`
- Update `server_test.go` to remove helpers now in `testutil_test.go`; add import of `intent` package for proposal struct if not already present

### DoD

- [ ] `go test -tags e2e ./internal/mcp/...`
- [ ] `grep -q 'go:build e2e' internal/mcp/e2e_test.go`
- [ ] `! grep -qE 'claude-code|claudecode|ClaudeCode' internal/mcp/e2e_test.go`
- [ ] `go test ./...`

---

## Constraints

- e2e tests must not call any real network endpoints (SiliconFlow, LLM APIs, Ollama)
- e2e tests must not require a running Claude Code process
- `testutil_test.go` helpers must be in package `mcp` (not `mcp_test`) to allow access to internal types if needed
- No new production files beyond the single-line fix in `server.go`
- Test file layout must allow `go test ./...` (no tag) to compile cleanly — e2e file excluded by build tag, helpers in `testutil_test.go` included unconditionally

## Acceptance Gate

- [ ] `go test ./...`
- [ ] `go test -tags e2e ./internal/mcp/...`
- [ ] `go vet ./...`
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Proposal self-review: APPROVED
premise-ledger:
[E] Motivation: field never populated → consumers see always-empty string; existing tests never caught it because all stubs isolated layers
[C] Goals: all 4 are numbered and use verifiable language (specific file names, specific test commands, specific field values)
[H] Feasibility: fix is one assignment; raw and proposal already in scope at line ~195 of server.go; httptest.NewServer already used in server_test.go; build tags are standard Go
[C] Completeness: four distinct trade-offs identified covering assignment-location, CI coverage gap, stub representativeness, and change scope
[E] Consistency: Approach section maps 1-to-1 to Goals 1-4; no contradictions found
GCL-self-report: E=2 C=2 H=1

Proposal approved. Starting plan draft.

Plan review iteration 2: APPROVED
premise-ledger:
[E] file-paths: internal/mcp/{server.go,server_test.go,types.go} exist; e2e_test.go & testutil_test.go absent (NEW, expected)
[E] data-model: intent.ActionProposal.RawTranscript field exists (proposal.go:24)
[E] fix-location: raw defined server.go:180, ClassifyFn at :195/:197 — single-line assignment site valid
[E] helpers-exist: newTestServer/postJSON/decodeResponse present in server_test.go (migratable)
[E] e2e-error-codes: -32602 (audio_path missing/invalid, server.go:169-175) & -32603 (ASR/classify, :182/:197) exist — e2e cases realistic
[C] goal-coverage: all 4 Goals map to Phase A/B + Acceptance Gate
[C] tdd-structure: Phase A & B each have ### Tests then ### Implementation
[C] tdd-order: Phase A first DoD=go test ./...; Phase B first DoD=go test -tags e2e (correct red→green prover for tagged suite)
[C] acceptance-gate: first item is go test ./...
[C] dod-executable: all DoD & gate items are shell commands
[C] absence-check: uses '! grep -qE' not 'grep -qv'
[C] phase-ordering: Phase A independent; Phase B migrates helpers; no circular deps
[C] scope-discipline: no Phase exceeds Goals (prod change = 1 line)
[H] gcl-seq=0: no prior TASK-15-plan events in ledger
[H] e2e-stub-injection: httptest + deterministic stubs assumed able to reproduce error responses end-to-end (unverified until impl)
GCL-self-report: E=6 C=8 H=2

claimed: 2026-06-28T09:35:11Z

Phase A ✓ 2026-06-28T00:00:00Z: Fixed RawTranscript assignment in server.go (proposal.RawTranscript = raw), added regression test TestServer_ToolsCall_RawTranscript in server_test.go. All unit tests GREEN.

Phase B ✓ 2026-06-28T00:00:00Z: Created testutil_test.go with shared helpers (newTestServer, newDeterministicServer, postJSON, decodeResponse, decodeProposal). Created e2e_test.go (//go:build e2e) with three httptest-based tests: happy-path full pipeline, ASR error → -32603, missing audio_path → -32602. All e2e tests GREEN.

DoD Results: 1 go test ./... PASS | 2 grep RawTranscript server.go PASS | 3 grep RawTranscript server_test.go PASS | 4 go test -tags e2e ./internal/mcp/... PASS | 5 grep go:build e2e PASS | 6 no claude-code refs PASS | 7 go vet ./... PASS

## Execution Summary
Result: Done
Commit: bd41479

Completed: 2026-06-28T09:40:40Z
<!-- SECTION:NOTES:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 go test ./...
- [ ] #2 grep -q 'RawTranscript' internal/mcp/server.go
- [ ] #3 grep -q 'RawTranscript' internal/mcp/server_test.go
- [ ] #4 go test -tags e2e ./internal/mcp/...
- [ ] #5 grep -q 'go:build e2e' internal/mcp/e2e_test.go
- [ ] #6 ! grep -qE 'claude-code|claudecode|ClaudeCode' internal/mcp/e2e_test.go
- [ ] #7 go vet ./...
<!-- DOD:END -->

---
id: TASK-16
title: voci 语音事件生产者后端（POST 上传 + 每次重建 hint + 写事件流）
status: 'Basic: Done'
assignee: []
created_date: '2026-06-28 08:46'
updated_date: '2026-06-28 12:12'
labels:
  - 'kind:basic'
dependencies: []
modified_files:
  - internal/daemon/server.go
  - internal/daemon/server_test.go
  - cmd/voci/main.go
  - cmd/voci/main_test.go
ordinal: 1000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
【已改造为 Monitor-host 形态，取代独立 daemon】提供 `voci serve`：作为 /voci-listen 的 Monitor command 被会话拉起（不是独立常驻 daemon），监听浏览器 PTT 上传（localhost HTTP），每段语音跑 ASR→hinted→rewrite→classify（每次重建 hint），把识别/改写结果（Rewritten）打到 **stdout**（一行一条 JSON）供 Monitor 直接注入会话。不再用 `voci --daemon` + `~/.voci/events.log` 文件 IPC；events.log 降为可选调试日志。作为会话子进程天然继承 CLAUDE_CODE_SESSION_ID（消除 TASK-18 主路径的会话交接需求）。复用现有 internal/daemon pipeline 代码，核心改造是输出通道：文件 append → stdout。
<!-- SECTION:DESCRIPTION:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
# Proposal: voci Voice-Event Producer Backend (POST upload, per-call hint rebuild, event-stream write)

## Background

voci's primary interaction model is Monitor-push, not MCP-pull: a browser captures speech asynchronously, voci produces an event, and a Claude Code Monitor session consumes it. The current `--session=integrated` path (cmd/voci/main.go) builds the ASR context hint exactly once at server startup (main.go:111) and freezes it for the server's lifetime. As a long-running daemon serves many utterances over a session where the project's git log, backlog tasks, and Claude Code activity all change, a frozen hint steadily degrades correction quality — the very entity/task substitutions the hint exists to drive go stale. There is also no producer that writes pipeline results to an event stream; nothing today feeds the Monitor consumer. This task supplies that producer side: an HTTP daemon that accepts uploaded audio bytes, rebuilds the hint on every call, runs the full ASR→hinted→rewrite→classify pipeline, and appends each result as one line to an event log.

## Goals

1. Expose `POST /api/voice/transcribe` that accepts raw uploaded audio bytes (request body) and returns the resulting ActionProposal as JSON with HTTP 200; malformed/empty bodies return HTTP 4xx.
2. Rebuild the context hint on every request via `context.BuildContextWithSource` (driven by the Claude Code adapter's `DiscoverContext`), so two calls separated by a context change observe different hints — no startup freeze.
3. Run the full reused pipeline per call: `asr.Transcribe` → `pipeline.RunHinted` → `pipeline.Rewrite` → `intent.Classify`, with all stage functions injectable for testing.
4. Append exactly one JSON line per successful call to a configurable event-stream path (default `~/.voci/events.log`), where each line is a self-contained event record (timestamp, rewritten text, kind, raw transcript) parseable independently by a downstream Monitor consumer.
5. Wire the daemon into `cmd/voci/main.go` behind an explicit flag (e.g. `--daemon`) without breaking existing `--file`, `--session=integrated`, or MCP behaviour.

## Proposed Approach

Add a new internal package (e.g. `internal/daemon`) hosting an HTTP handler with injected pipeline-stage functions and a hint-builder closure, mirroring the dependency-injection shape already used by `internal/mcp.Server` and `cmd/voci.run`. The handler reads the audio body, persists it to a temporary file (the existing `asr.Transcribe` takes a path), calls the injected hint-builder which rebuilds via `BuildContextWithSource` using the adapter's discovered Source, runs the four pipeline stages with the rebuilt hint, and on success appends a marshalled event line to the event-stream writer. The event-stream append is a small, separately testable component (path + append-one-line semantics, creating `~/.voci` as needed). cmd/voci gains a `--daemon` branch that constructs the handler with real dependencies (config, ollama chat, SiliconFlow key, adapter-backed hint-builder, default event path) and starts `http.ListenAndServe`. Hint rebuild reuses the exact closure pattern in main.go:28-34 so behaviour stays consistent with the CLI path; the difference is purely that it runs per-request rather than once.

## Trade-offs and Risks

- Per-call hint rebuild adds latency (git log, file globs, session JSONL tail) to every request. Acceptable for an interactive voice cadence; `BuildCached` is deliberately not used here because freshness is the whole point, though a short TTL could be added later if profiling demands it.
- Writing uploaded bytes to a temp file (to satisfy `asr.Transcribe`'s path-based signature) adds I/O and cleanup responsibility rather than streaming in-memory; chosen to avoid changing the stable ASR signature in this task.
- Concurrent requests appending to one event-log file need append-mode writes and per-line atomicity; mitigated by O_APPEND single-line writes, with file locking deferred unless contention appears.
- Audio uploads are unbounded by default; a request body size limit should be applied to avoid memory/disk exhaustion.
- Scope risk: this is producer-only. The browser PTT UI (TASK-6) and the Monitor consumer (`/voci-listen`) are explicitly out of scope; the event-line schema is the contract between them and must be stable and documented.

---

# Plan: voci Voice-Event Producer Backend (POST upload, per-call hint rebuild, event-stream write)

Proposal: docs/proposals/proposal-voci-event-producer.md

## Phase A: Event-stream append component

### Tests (write first)
- `internal/daemon/eventlog_test.go`
  - `TestAppendEvent_WritesOneJSONLine` — appending one event produces exactly one line that round-trips via json.Unmarshal into an Event with matching Rewritten/Kind/RawTranscript.
  - `TestAppendEvent_AppendsNotTruncates` — two appends yield two lines; the first line survives.
  - `TestAppendEvent_CreatesParentDir` — target path under a non-existent dir is created and written.
  - `TestAppendEvent_EventHasTimestamp` — written event has a non-empty RFC3339 timestamp field.

### Implementation
- Create `internal/daemon/eventlog.go`: `Event` struct (Timestamp, Rewritten, Kind, RawTranscript, Confidence) and `AppendEvent(path string, ev Event) error` using `os.MkdirAll` + `os.OpenFile(O_APPEND|O_CREATE|O_WRONLY)` writing one `json.Marshal` line + `\n`.

### DoD
- [ ] `go test ./...`
- [ ] `go test ./internal/daemon/ -run TestAppendEvent`
- [ ] `gofmt -l internal/daemon/eventlog.go | (! grep .)`

## Phase B: HTTP handler with per-call hint rebuild and pipeline

### Tests (write first)
- `internal/daemon/server_test.go`
  - `TestHandler_RejectsNonPost` — GET to `/api/voice/transcribe` returns 405.
  - `TestHandler_RejectsEmptyBody` — POST with empty body returns 4xx.
  - `TestHandler_RunsPipelineAndReturnsProposalJSON` — POST with audio bytes, injected stage stubs, returns 200 and body unmarshals to an ActionProposal with expected Kind/Rewritten.
  - `TestHandler_RebuildsHintPerCall` — injected hint-builder counter increments once per request; two POSTs ⇒ builder called twice (proves no freeze).
  - `TestHandler_AppendsEventPerCall` — after a successful POST, the configured event path contains one line matching the proposal.
  - `TestHandler_HintBuilderResultReachesHintedStage` — the hint string returned by the injected builder is the one passed into the injected hinted stage (captured and asserted).

### Implementation
- Create `internal/daemon/server.go`: `Server` struct with injected `TranscribeFn`, `HintedFn`, `RewriteFn`, `ClassifyFn` (reuse signatures from `internal/mcp`), `ChatFn`, `APIKey`, `BuildHintFn func() string`, `EventPath string`. `Handler()` returns mux routing `POST /api/voice/transcribe` to a handler that: reads+size-limits body, writes to a temp file, calls `BuildHintFn()`, runs the four stages, calls `AppendEvent`, and writes proposal JSON. `Start(addr)` wraps `http.ListenAndServe`.

### DoD
- [ ] `go test ./...`
- [ ] `go test ./internal/daemon/ -run TestHandler`
- [ ] `grep -q "api/voice/transcribe" internal/daemon/server.go`
- [ ] `gofmt -l internal/daemon/server.go | (! grep .)`

## Phase C: cmd/voci --daemon wiring

### Tests (write first)
- `cmd/voci/main_test.go` (add cases)
  - `TestRun_DaemonFlagStartsDaemon` — `--daemon` with an injected `startDaemonFn` invokes it with the configured addr and returns its result (no `--file` required).
  - `TestRun_DaemonFlagDoesNotRequireFile` — `--daemon` without `--file` does not return the "--file is required" error.
  - `TestRun_NoDaemonStillRequiresFile` — without `--daemon` and without `--file`, the existing "--file is required" error path is unchanged.

### Implementation
- Modify `cmd/voci/main.go`: add `--daemon` bool flag and `--daemon-port` (default e.g. 9474) and `--events-path` (default `~/.voci/events.log`); add an injectable `startDaemonFn` parameter to `run` (nil ⇒ build real `daemon.Server` with config, ollama chat, SiliconFlow key, adapter-backed per-call `BuildHintFn`, default event path). Branch on `--daemon` before the `--file` check. Update `main()` to pass the real builder.

### DoD
- [ ] `go test ./...`
- [ ] `go test ./cmd/voci/ -run TestRun_Daemon`
- [ ] `go build ./...`
- [ ] `grep -q "daemon" cmd/voci/main.go`

## Constraints
- Per-call hint rebuild must use `context.BuildContextWithSource` (not `BuildCached`); freshness over caching.
- Event-line schema (one JSON object per line) is the producer/consumer contract and must stay stable; downstream Monitor consumers parse line-by-line.
- Existing `--file`, `--session=integrated`/MCP, and `--iterate` behaviour must remain unchanged.
- Reuse existing pipeline stage functions (`asr.Transcribe`, `pipeline.RunHinted`, `pipeline.Rewrite`, `intent.Classify`); do not fork the pipeline logic.
- Browser PTT UI (TASK-6) and the Monitor consumer (`/voci-listen`) are out of scope.
- Each phase ≤200 LOC including tests.

## Acceptance Gate
- [ ] `go test ./...`
- [ ] `go build ./...`
- [ ] `go vet ./...`
- [ ] `grep -q "api/voice/transcribe" internal/daemon/server.go`
- [ ] `grep -q "BuildContextWithSource\|BuildHintFn" internal/daemon/server.go cmd/voci/main.go`
- [ ] `! grep -q "BuildCached" internal/daemon/server.go`
- [ ] `gofmt -l internal/daemon/ cmd/voci/main.go | (! grep .)`

## RE-ARCHITECTED (2026-06-28)：daemon → Monitor-host `voci serve`

**动因**：Monitor 的 `command` 本就是一个会话内常驻进程（参 loop-backlog 的 `node scan-loop.js --loop`）。独立 daemon + events.log + tail 是多余的三进程/文件 IPC，与「有 Monitor 就够了」的要求不符。

**新形态**：
- 提供 `voci serve` 子命令：启动一个 localhost HTTP listener 收浏览器 PTT 上传，每段语音跑完管道后把 `Rewritten`（或整个 ActionProposal JSON）**打到 stdout**（一行一条）。
- HTTP 响应照样回浏览器（socket）；识别文本走 stdout（会话）——两个输出通道各司其职。
- `voci serve` 作为会话子进程，环境里有 `CLAUDE_CODE_SESSION_ID`，per-call hint 重建可直接定位 JSONL（不再需 ~/.voci/session 交接）。
- `~/.voci/events.log` 从 IPC 机制降为可选调试日志（可保留 AppendEvent 作为旁路落盘）。

**需改动的现有代码**：`cmd/voci/main.go` 的 `--daemon` 分支 → `serve` 入口；`internal/daemon/server.go` 的 handler 输出由 `AppendEvent(file)` 改为写 stdout（保留 file 为可选）。

**旧计划/旧实现（`voci --daemon` 写 events.log）被本节超集，需重新 plan。**

### 新 DoD
- [ ] `go test ./...`
- [ ] `grep -qE 'serve' cmd/voci/main.go`
- [ ] `go test ./internal/daemon/ -run Stdout`  (断言 serve handler 把 Rewritten 写到提供的 io.Writer/stdout)
- [ ] `go build ./...`
- [ ] `go vet ./...`

## RE-ARCHITECT TDD Plan (2026-06-28)

**状态说明**：上方 Phase A/B/C（events.log AppendEvent + HTTP handler + `--daemon` 接线）已执行并合并（commit e843a1b），**保留作记录不删**。以下 TDD 仅覆盖改造增量：输出通道 文件→stdout、新增 `serve` 入口。执行以本节为准；受影响的旧 Phase B/C 输出行为被本节取代。

### Phase D（改造）：handler 把事件行写到 io.Writer(stdout)

#### Tests (write first)
- internal/daemon/server_test.go（追加）：
  - `TestHandler_WritesEventLineToStdout`：注入 `EventWriter:&bytes.Buffer{}`，POST 音频（桩 pipeline），断言 buffer 恰一行，json.Unmarshal 回 Event，Rewritten/Kind/RawTranscript 与 proposal 一致。
  - `TestHandler_StdoutOneLinePerCall`：两次 POST → buffer 两行。
  - `TestHandler_StillReturnsProposalJSONToHTTP`：HTTP 响应体仍是 proposal JSON（浏览器通道不变）。
  - `TestHandler_EventPathOptional`：`EventPath==""` 且 `EventWriter` 已设时不写文件、不报错，只走 stdout。

#### Implementation
- internal/daemon/server.go：`Server` 增字段 `EventWriter io.Writer`。handler 成功后 `json.Marshal(ev)+"\n"` 写 `EventWriter`（非 nil 时）。`AppendEvent(EventPath)` 改为仅当 `EventPath!=""` 的可选旁路落盘（events.log 降为调试日志）。

#### DoD
- [ ] `go test ./internal/daemon/ -run Stdout`
- [ ] `grep -q 'EventWriter' internal/daemon/server.go`
- [ ] `go test ./...`

### Phase E（改造）：cmd/voci `serve` 入口，stdout 为事件汇

#### Tests (write first)
- cmd/voci/main_test.go（追加）：
  - `TestRun_ServeStartsServer`：传 `serve`/`--serve` + 注入 startServeFn，断言被调用且不要求 `--file`。
  - `TestRun_ServeNoFileRequired`：`serve` 无 `--file` 不报 "--file is required"。
  - `TestRun_ServeUsesStdoutSink`：默认实现构造的 `daemon.Server.EventWriter == os.Stdout`（经注入 capture 断言）。

#### Implementation
- cmd/voci/main.go：新增 `serve` 子命令/`--serve`；默认实现构造 `daemon.Server{..., EventWriter: os.Stdout, EventPath: 可选}`，per-call `BuildHintFn` 复用 adapter.DiscoverContext + BuildContextWithSource。旧 `--daemon` 分支标注 superseded（保留为 alias 指向 serve，或本次移除并更新其测试）。

#### DoD
- [ ] `go test ./cmd/voci/ -run TestRun_Serve`
- [ ] `grep -qE 'serve' cmd/voci/main.go`
- [ ] `go build ./...`
- [ ] `go vet ./...`
- [ ] `go test ./...`
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Proposal self-review: APPROVED
premise-ledger:
[E] POST /api/voice/transcribe handler: voci has no HTTP daemon today; internal/mcp is the only HTTP server and is JSON-RPC, so a new handler package is genuinely needed.
[E] per-call hint rebuild fixes a real freeze: cmd/voci/main.go:111 builds hint once at startup for --session=integrated and reuses it for the server lifetime.
[C] BuildContextWithSource(root, src, nil) is the verified rebuild entry point and the adapter's DiscoverContext supplies the Source (claude_code.go:27).
[C] reused pipeline stages exist with matching signatures: asr.Transcribe (path-based), pipeline.RunHinted, pipeline.Rewrite, intent.Classify.
[H] event-line JSON schema is the stable producer/consumer contract for the downstream Monitor consumer (/voci-listen), assumed line-delimited per baime daemon precedent.
GCL-self-report: E=2 C=2 H=1

Plan review iteration 1: APPROVED
premise-ledger:
[E] File paths: all referenced code functions/types exist — verified by grep (BuildContextWithSource builder.go:376; asr.Transcribe; pipeline.RunHinted/Rewrite; intent.Classify; mcp TranscribeFn/HintedFn/RewriteFn/ClassifyFn; DiscoverContext; ActionProposal)
[E] Goal coverage: 5/5 proposal Goals map to Phases A/B/C and Acceptance — verified by reading plan
[C] Proposal-doc pointer docs/proposals/proposal-voci-event-producer.md is absent but follows repo-wide anchor convention (task-15/17/18 identical); treated as doc pointer not a code dependency
[H] Per-call hint rebuild latency acceptable for interactive voice cadence — design assumption
GCL-self-report: E=3 C=1 H=1

claimed: 2026-06-28T09:35:30Z

Phase A ✓ 2026-06-28T00:00:00Z: internal/daemon/eventlog.go + eventlog_test.go — Event struct, AppendEvent (O_APPEND|O_CREATE|O_WRONLY), 4 tests all pass

Phase B ✓ 2026-06-28T00:00:00Z: internal/daemon/server.go + server_test.go — Server struct with injected fns, BuildHintFn called per request, pipeline wired, event appended per call, 6 tests all pass

Phase C ✓ 2026-06-28T00:00:00Z: cmd/voci/main.go — --daemon/--daemon-port/--events-path flags, StartDaemonFn injectable, daemon branch before --file check, BuildContextWithSource used in default daemon path, 3 new daemon tests pass, all existing tests updated for new run() param

## Execution Summary
Result: Done
Commit: e843a1b

Completed: 2026-06-28T09:44:49Z

RE-ARCHITECTED 2026-06-28: 取消独立 daemon，改为 Monitor-host `voci serve`（识别结果打 stdout，Monitor 注入会话）。退回 Basic: Backlog 待重新 plan。现有 internal/daemon 实现需回炉：输出通道 文件→stdout。
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Phase D + E implemented and committed (0b59a57).

**Phase D — EventWriter stdout sink:**
- `internal/daemon/server.go`: Added `EventWriter io.Writer` field to `Server`. In `handleTranscribe`, on every successful request the event JSON line is written to `EventWriter` (primary channel), then optionally appended to `EventPath` file (debug sidecar).
- `internal/daemon/server_test.go`: 4 new tests — `TestHandler_WritesEventLineToStdout`, `TestHandler_StdoutOneLinePerCall`, `TestHandler_StillReturnsProposalJSONToHTTP`, `TestHandler_EventPathOptional`. All pass.

**Phase E — `voci --serve` Monitor-host entry:**
- `cmd/voci/main.go`: Added `StartServeFn` type and `--serve`/`--serve-port` flags. When `--serve` is set, builds `daemon.Server{..., EventWriter: os.Stdout}` (or calls injected `startServeFn` for tests). No `--file` required. `--daemon` branch retained as alias/legacy.
- `cmd/voci/main_test.go`: 3 new tests — `TestRun_ServeStartsServer`, `TestRun_ServeNoFileRequired`, `TestRun_ServeUsesStdoutSink`. All 22 pre-existing tests updated to include 15th nil arg. All pass.

All DoD checks confirmed: `go test ./...` ✓, `go build ./...` ✓, `go vet ./...` ✓, grep checks ✓, gofmt ✓.
<!-- SECTION:FINAL_SUMMARY:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 go test ./...
- [x] #2 go test ./internal/daemon/ -run TestAppendEvent
- [x] #3 gofmt -l internal/daemon/eventlog.go | (! grep .)
- [x] #4 go test ./internal/daemon/ -run TestHandler
- [x] #5 grep -q "api/voice/transcribe" internal/daemon/server.go
- [x] #6 gofmt -l internal/daemon/server.go | (! grep .)
- [x] #7 go test ./cmd/voci/ -run TestRun_Daemon
- [x] #8 go build ./...
- [x] #9 grep -q "daemon" cmd/voci/main.go
- [x] #10 go vet ./...
- [x] #11 grep -q "BuildContextWithSource\|BuildHintFn" internal/daemon/server.go cmd/voci/main.go
- [x] #12 ! grep -q "BuildCached" internal/daemon/server.go
- [x] #13 gofmt -l internal/daemon/ cmd/voci/main.go | (! grep .)
<!-- DOD:END -->

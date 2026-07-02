---
id: TASK-77
title: Web UI 实时活动流：SSE 推送 Claude Code 正在执行的动作
status: 'Basic: Done'
assignee: []
created_date: '2026-07-02 01:54'
updated_date: '2026-07-02 03:20'
labels:
  - 'kind:basic'
dependencies: []
modified_files:
  - internal/context/session_source.go
  - internal/context/session_source_test.go
  - internal/daemon/handlers.go
  - internal/daemon/server.go
  - internal/daemon/server_test.go
  - internal/daemon/web/index.html
  - internal/daemon/web/recorder.src.js
  - internal/wire/wire.go
  - e2e/tests/activity-stream.spec.ts
ordinal: 47000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
方案 B：新增 /api/activity SSE 端点，服务端 tail session JSONL，将新增条目实时推送到浏览器，使 Web UI 能显示 Claude Code 当前正在执行的工具调用（读文件、运行命令等）及忙碌/空闲状态。
<!-- SECTION:DESCRIPTION:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
See implementation notes for the full combined proposal + plan text (written by the proposal and plan agents above).

## Phase A: /api/activity SSE 后端

### Tests (write first)
- `internal/daemon/server_test.go`: TestHandleActivity_RequiresTokenWhenSet, TestHandleActivity_ContentTypeSSE, TestHandleActivity_NoSession, TestHandleActivity_StreamsToolCall
- `internal/context/session_source_test.go`: TestResolveJSONLPath_ExportedWrapsUnexported

### Implementation
- `internal/context/session_source.go`: add exported `ResolveJSONLPath(root string) string` wrapper (~3 LOC)
- `internal/daemon/server.go`: add `ActivityPathFn func() string` field; register `/api/activity` route behind BearerMiddleware
- `internal/daemon/handlers.go`: add `handleActivity` handler with 500ms stat-poll loop; local minimal structs (actSessionEntry etc) to avoid import cycle; SSE flush after every write
- `internal/wire/wire.go`: separate sessSource construction; wire `ActivityPathFn: func() string { ... sessSource.ResolveJSONLPath(cwd) }`

### DoD
- `go test ./internal/daemon/... -run TestHandleActivity`
- `go test ./internal/context/... -run TestResolveJSONLPath`
- `grep -q 'ActivityPathFn' internal/daemon/server.go`
- `grep -q '/api/activity' internal/daemon/server.go`
- `grep -q 'handleActivity' internal/daemon/handlers.go`
- `grep -q 'ResolveJSONLPath' internal/context/session_source.go`
- `grep -q 'ActivityPathFn' internal/wire/wire.go`
- `! grep -q 'resolveJSONLPath' internal/daemon/handlers.go`
- `go test ./...`

## Phase B: 前端 EventSource 客户端

### Tests (write first)
- `e2e/tests/activity-stream.spec.ts`: tool_call event renders .voci-activity-row; no_session does not crash page

### Implementation
- `internal/daemon/web/recorder.src.js`: add `connectActivityStream()` (fetch + ReadableStream pump); `handleActivityEvent(ev)` (appends .voci-activity-row div); call from `startPolling()`

### DoD
- `go test ./...`
- `grep -q 'connectActivityStream' internal/daemon/web/recorder.src.js`
- `grep -q 'voci-activity-row' internal/daemon/web/recorder.src.js`
- `grep -q 'connectActivityStream' e2e/tests/activity-stream.spec.ts`
- `make build`

## Constraints
- No inotify; 500ms os.Stat poll only (portable)
- No WebSocket; SSE via fetch+ReadableStream for bearer token support
- ActivityPathFn returns "" → 5s heartbeat, never 4xx/close
- /api/context poll unchanged (additive only)
- Local structs in handlers.go (no daemon→context import)
- appendChild not innerHTML (preserve activity rows across renderDialogue calls)

## Acceptance Gate
- `go test ./...`
- `make build`
- `cd e2e && npx playwright test --reporter=list`
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Proposal self-review: APPROVED
premise-ledger:
[E] Motivation explains WHY (polling blindspot → bad voice timing): confirmed by refreshContext() setInterval in recorder.src.js and 5s contextPollMs default.
[E] Goals are all numbered and concretely verifiable: checked against 5 goals.
[E] Feasibility — resolveJSONLPath at session_source.go:428, sessionEntry struct at :87, BearerMiddleware import in server.go, msg.events render path at recorder.src.js:329-333.
[C] Trade-offs cover stat-poll vs inotify, long-lived connections, no-session degradation, additive-only change.
[H] Goal 2 phrase 'enough information' is slightly soft but immediately quantified by the three event-type taxonomy.
GCL-self-report: E=3 C=1 H=1

Proposal approved. Starting plan draft.

Plan review iteration 1: APPROVED
premise-ledger:
[E] Goal 1 (SSE endpoint + BearerMiddleware route): Phase A route registration verified
[E] Goal 2 (tool_call/assistant_text/idle event types): Phase A handleActivity covers all three
[E] Goal 3 (browser fetch-based SSE, 2s update, no poll increase): Phase B connectActivityStream + unchanged /api/context
[E] Goal 4 (no_session heartbeat, hold connection open): Phase A path=='' branch with 5s ticker
[E] Goal 5 (Playwright E2E test): Phase B activity-stream.spec.ts
[E] All 6 referenced file paths verified to exist on disk
[E] makeServer helper and session_source_test.go confirmed present
[E] Absence check uses ! grep -q pattern (not grep -qv)
[C] TDD order: Phase A first DoD is scoped go test, Phase B first DoD is go test ./...
[C] Acceptance Gate first item is go test ./...
[C] All DoD and Acceptance Gate items are executable shell commands
[C] Phase ordering: A (backend infrastructure) before B (frontend consumer), no circular deps
[H] Scope discipline: no phase implements anything not backed by a proposal Goal
GCL-self-report: E=8 C=4 H=1

claimed: 2026-07-02T02:35:19Z
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Implemented /api/activity SSE endpoint + frontend EventSource client for real-time Claude Code activity streaming.

**Phase A — Backend:**
- `internal/context/session_source.go`: Exported `ResolveJSONLPath` wrapping unexported resolver
- `internal/daemon/server.go`: Added `ActivityPathFn func() string` field + `/api/activity` route behind BearerMiddleware
- `internal/daemon/handlers.go`: Added `handleActivity` with 500ms os.Stat poll loop, local structs (actSessionEntry) to avoid import cycles, SSE flush after every write. No-session mode sends 5s idle heartbeats.
- `internal/wire/wire.go`: Wired `ActivityPathFn` via separate `sessSource` construction

**Phase B — Frontend:**
- `internal/daemon/web/recorder.src.js`: `connectActivityStream()` via fetch+ReadableStream (Bearer token support), `handleActivityEvent()` renders `.voci-activity-row` via appendChild. Deferred 10s after startPolling to avoid blocking networkidle.
- `internal/daemon/web/index.html`: Added `<div id="voci-activity-feed">` container

**Phase C — Tests:**
- `internal/daemon/server_test.go`: TestHandleActivity_RequiresTokenWhenSet, TestHandleActivity_ContentTypeSSE, TestHandleActivity_NoSession, TestHandleActivity_StreamsToolCall
- `internal/context/session_source_test.go`: TestResolveJSONLPath_ExportedWrapsUnexported, TestResolveJSONLPath_NoSession
- `e2e/tests/activity-stream.spec.ts`: 4 E2E tests (tool_call renders row, no_session no crash, desktop+mobile)

**Acceptance Gate:** go test ./... passes, make build succeeds, 90/90 Playwright E2E tests pass.
<!-- SECTION:FINAL_SUMMARY:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 go test ./internal/daemon/... -run TestHandleActivity
- [ ] #2 go test ./internal/context/... -run TestResolveJSONLPath
- [ ] #3 grep -q 'ActivityPathFn' internal/daemon/server.go
- [ ] #4 grep -q '/api/activity' internal/daemon/server.go
- [ ] #5 grep -q 'handleActivity' internal/daemon/handlers.go
- [ ] #6 grep -q 'ResolveJSONLPath' internal/context/session_source.go
- [ ] #7 grep -q 'ActivityPathFn' internal/wire/wire.go
- [ ] #8 ! grep -q 'resolveJSONLPath' internal/daemon/handlers.go
- [ ] #9 grep -q 'connectActivityStream' internal/daemon/web/recorder.src.js
- [ ] #10 grep -q 'voci-activity-row' internal/daemon/web/recorder.src.js
- [ ] #11 grep -q 'connectActivityStream' e2e/tests/activity-stream.spec.ts
- [ ] #12 make build
- [ ] #13 go test ./...
- [ ] #14 cd e2e && npx playwright test --reporter=list
<!-- DOD:END -->

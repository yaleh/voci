---
id: TASK-20
title: voci serve 双端点：识别预览与确认 emit
status: 'Basic: Done'
assignee: []
created_date: '2026-06-28 12:48'
updated_date: '2026-06-28 13:43'
labels:
  - 'kind:basic'
dependencies: []
ordinal: 1000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
实现上述两个端点：(1) POST /api/voice/transcribe — 运行完整 ASR→hinted→rewrite→classify pipeline，但不写 stdout，返回 ActionProposal JSON 供 Web UI 预览编辑；(2) POST /api/voice/emit — 接收浏览器确认后的文本，写入 stdout → Monitor 注入 Claude Code 会话。两端点合起来实现「两步提交」Web UI 预览模式。
<!-- SECTION:DESCRIPTION:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
# Plan: voci serve 双端点：识别预览与确认 emit

## Phase A: 拆分 handleTranscribe — 识别但不 emit

### Tests (write first)

File: `internal/daemon/server_test.go`

New test cases to add (must fail before implementation):

- `TestTranscribe_DoesNotWriteToEventWriter` — POST /api/voice/transcribe with EventWriter set to a `bytes.Buffer`; assert `buf.Len() == 0` after request succeeds with 200. This catches the current behavior where `handleTranscribe` writes to EventWriter.
- `TestTranscribe_DoesNotAppendToEventPath` — POST /api/voice/transcribe with a real `EventPath` set; assert the event log file is NOT created / remains empty after the request.

Existing tests that will break and must be fixed (they currently assert emit from /transcribe):

- `TestHandler_WritesEventLineToStdout` — currently asserts EventWriter gets a line from /transcribe; must be updated to assert `buf.Len() == 0` (or renamed/replaced).
- `TestHandler_StdoutOneLinePerCall` — same, currently asserts 2 lines after 2 /transcribe calls; must assert 0.
- `TestHandler_StillReturnsProposalJSONToHTTP` — keep as-is (still valid: HTTP response should be ActionProposal JSON).
- `TestHandler_AppendsEventPerCall` — currently asserts EventPath file gets written by /transcribe; must assert file does NOT exist.
- `TestHandler_EventPathOptional` — currently asserts EventWriter is non-empty; must flip to assert it is empty.

### Implementation

File: `internal/daemon/server.go`

Changes:
1. In `handleTranscribe`, remove the `EventWriter` write block (lines 138–142 in current file).
2. In `handleTranscribe`, remove the `EventPath` sidecar block (lines 145–147).
3. The `ev` local variable construction (lines 130–135) can also be removed since it is no longer used by `handleTranscribe`.
4. The HTTP response encoding of `proposal` (last two lines) is unchanged.

No struct changes needed; no new files.

### DoD

- [ ] `go test ./internal/daemon/...`
- [ ] `go test ./...`
- [ ] `go test -run TestTranscribe_DoesNotWriteToEventWriter ./internal/daemon/...`
- [ ] `go test -run TestHandler_StillReturnsProposalJSONToHTTP ./internal/daemon/...`

---

## Phase B: 新增 handleEmit — 确认写入 stdout

### Tests (write first)

File: `internal/daemon/server_test.go`

New test cases (must fail before implementation):

- `TestEmit_WritesOneEventLineToEventWriter` — POST /api/voice/emit with JSON body `{"text":"hello world"}` and EventWriter set to `bytes.Buffer`; assert response is 204, assert exactly one JSON line in buffer, assert `ev.Rewritten == "hello world"`.
- `TestEmit_RejectsNonPost` — GET /api/voice/emit; assert 405.
- `TestEmit_RejectsEmptyText` — POST /api/voice/emit with `{"text":""}` or empty body; assert 4xx.
- `TestEmit_Returns503WhenEventWriterNil` — POST /api/voice/emit with nil EventWriter; assert 503.
- `TestEmit_WritesExactlyOneLinePerCall` — two sequential POST /api/voice/emit calls; assert exactly 2 JSON lines in EventWriter buffer.
- `TestEmit_AlsoAppendsToEventPath` — POST /api/voice/emit with both EventWriter and EventPath set; assert both are written (EventWriter has 1 line, EventPath file has 1 line).
- `TestEmit_EventPathOptional` — POST /api/voice/emit with EventPath="" ; assert no file created, 204 returned.

### Implementation

File: `internal/daemon/server.go`

Changes:
1. In `Handler()`, register the new route: `mux.HandleFunc("/api/voice/emit", s.handleEmit)`.
2. Add `handleEmit` method:
   - Accept only `POST`; return 405 otherwise.
   - Decode JSON body into a struct `emitRequest{Text string \`json:"text"\`}`.
   - If `Text` is empty after trim, return 400.
   - If `s.EventWriter == nil`, return 503 with message "EventWriter not configured".
   - Construct `Event{Rewritten: req.Text, Kind: "direct_prompt", Timestamp: time.Now().Format(time.RFC3339)}` (or leave Kind/Confidence as zero values — TBD based on proposal; the proposal says "constructs an Event with that text").
   - Marshal and write to `s.EventWriter` with trailing `\n`.
   - If `s.EventPath != ""`, call `AppendEvent(s.EventPath, ev)`.
   - Respond `204 No Content`.

No new files; no struct changes.

### DoD

- [ ] `go test ./internal/daemon/...`
- [ ] `go test ./...`
- [ ] `go test -run TestEmit_WritesOneEventLineToEventWriter ./internal/daemon/...`
- [ ] `go test -run TestEmit_Returns503WhenEventWriterNil ./internal/daemon/...`
- [ ] `grep -q 'handleEmit' internal/daemon/server.go`

---

## Constraints

- `handleTranscribe` must NOT write to `EventWriter` or `EventPath` after Phase A.
- `handleEmit` must NOT invoke any pipeline stage (TranscribeFn, HintedFn, RewriteFn, ClassifyFn); it only writes the caller-supplied text.
- No changes to `cmd/voci/main.go`; the existing `EventWriter: os.Stdout` wiring covers both endpoints automatically.
- No authentication or CORS middleware in this task.
- No session-token correlation between `/transcribe` and `/emit`.

## Acceptance Gate

- [ ] `go test ./...`
- [ ] `go test -run TestTranscribe_DoesNotWriteToEventWriter ./internal/daemon/...`
- [ ] `go test -run TestEmit_WritesOneEventLineToEventWriter ./internal/daemon/...`
- [ ] `go test -run TestEmit_Returns503WhenEventWriterNil ./internal/daemon/...`
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Proposal self-review: APPROVED
premise-ledger:
[E] Background explains WHY (not just WHAT), 3-8 lines: 6-line background paragraph traces causal chain from Monitor-push architecture → unconditional emit → preview impossible → split needed. Verified by reading the written text.
[E] Goals numbered and concretely verifiable: 5 goals each with testable pass/fail criteria (stdout absent, JSON line count, end-to-end flow, struct field presence, test coverage). Verified by inspection.
[E] Approach aligns with codebase: handleTranscribe at line 61, Server.Handler() ServeMux at line 50, EventWriter field at line 44, EventPath at line 46 all confirmed in internal/daemon/server.go. No new dependencies required.
[C] Trade-offs and risks identified: 3 explicit non-goals + 3 named risks (lost-emit, concurrent-emit, EventWriter blocking). Basis: completeness judgment — no obvious omitted concerns for a local-only server.
[E] No contradictions: Goal 1 (no emit from /transcribe) is consistent with Approach (remove EventWriter.Write from handleTranscribe). Goal 2 (emit from /emit) consistent with handleEmit description. Verified by cross-reading Goals vs Approach sections.
GCL-self-report: E=4 C=1 H=0

Plan review iteration 1: NEEDS_REVISION — Four categories of issues fixed: (1) Phase A DoD items 3-4 were natural language, converted to `go test -run` shell commands. (2) Phase B DoD items 3-4 were natural language, converted to `go test -run` shell commands. (3) Phase B DoD item 5 had backwards logic: `! grep -q 'handleEmit'` is an absence check but was intended as a presence check — fixed to `grep -q 'handleEmit' internal/daemon/server.go`. (4) Acceptance Gate items 2-4 were natural language, converted to `go test -run` shell commands.

Plan review iteration 2: APPROVED
premise-ledger:
E Goal coverage: all 5 proposal Goals addressed by Phase A, Phase B, and Constraints
E TDD structure: both phases have ### Tests before ### Implementation
E TDD order: first DoD item in each phase is go test
E Acceptance gate: first item is go test ./...
E DoD executability: all items are shell commands (go test or grep -q)
E Absence checks: no grep -qv pattern used
E Phase ordering: A removes emit, B adds new endpoint — no circular deps
E Scope discipline: every phase action traces to a Goal
E File paths: internal/daemon/server.go and server_test.go exist; line references 138-142 and 145-147 match actual file content; named tests verified in server_test.go
GCL-self-report: E=9 C=0 H=0
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Split single /api/voice/transcribe endpoint into two-step flow:

- `handleTranscribe` (POST /api/voice/transcribe): runs full ASR→hinted→rewrite→classify pipeline, returns ActionProposal JSON for Web UI preview. Does NOT write to EventWriter or EventPath.
- `handleEmit` (POST /api/voice/emit): accepts browser-confirmed text, emits JSON event line to EventWriter (→ Monitor injection) and optional EventPath sidecar. Does NOT invoke any pipeline stage.

Files: internal/daemon/server.go, internal/daemon/server_test.go
Commit: 2813d04
<!-- SECTION:FINAL_SUMMARY:END -->

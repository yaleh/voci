---
id: TASK-22
title: Monitor 端到端执行验证：serve→emit→事件契约可自动化测试
status: 'Basic: Done'
assignee: []
created_date: '2026-06-28 14:26'
updated_date: '2026-06-28 14:46'
labels:
  - 'kind:basic'
dependencies: []
ordinal: 1000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
补上「语音→Monitor→会话执行」端到端链路中未被自动化覆盖的缺口。当前 e2e 测试只覆盖 pipeline 与 HTTP 响应；serve 经 /api/voice/emit 写 stdout→Monitor 注入会话→指令被实际执行 这一段从未端到端验证过（上轮人工测试中收到了 Monitor push 但未执行 voci-listen 的 extractInstruction→execute）。本任务：(1) 一个 e2e 集成测试启动真实 serve、POST /transcribe→/emit、捕获 EventWriter stdout 并断言事件行是格式正确、字段完整的 JSON（生产者侧端到端）；(2) 对 voci-listen skill 的 extractInstruction 解析契约做可执行测试（消费者侧 JSON→Rewritten 提取，含 raw fallback）；(3) 把「指令在 Claude Code 会话中被实际执行」这一 harness 层环节固化为有清单的人工验证流程文档。
<!-- SECTION:DESCRIPTION:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
# TASK-22 Proposal — Monitor 端到端执行验证（serve→emit→事件契约可自动化测试）

## Background (WHY)

The "voice → Monitor → session execution" chain has three links: producer (`voci serve` runs the
pipeline and writes one JSON Event line to stdout via `/api/voice/emit`), transport (Monitor wakes the
session on each stdout line), and consumer (`voci-listen` SKILL.md runs `extractInstruction` to pull the
`Rewritten` field and execute it inline). Current automated coverage stops at the MCP/pipeline HTTP
response (`internal/mcp/e2e_test.go`) — it never asserts the **emitted stdout line** nor the
**parse contract** that turns that line into an executable instruction.

This middle is untested and has already failed silently: the last manual test received a Monitor push
but never ran `extractInstruction → execute`, so "instruction executed in the live session" remains
unproven. The producer side (serve → stdout line) and the consumer-parse side (`extractInstruction`)
are both Go/script-testable; only the final harness step (Claude Code actually acting on the
instruction) is irreducibly manual. This task closes the two automatable links and pins the manual one
to a checklist.

## Goals (numbered, verifiable)

1. **Daemon e2e producer test.** A `//go:build e2e` test in `internal/daemon/` constructs the real
   `daemon.Server` with `EventWriter = &bytes.Buffer{}` and deterministic stub pipeline fns (mirroring
   `internal/mcp/e2e_test.go`), serves it via `httptest.NewServer(srv.Handler())`, POSTs audio bytes to
   `/api/voice/transcribe` then `{"text":...}` to `/api/voice/emit`, and asserts the buffer contains
   **exactly one** line that is valid JSON and unmarshals to a `daemon.Event` with the expected
   `Rewritten` and `Kind == "direct_prompt"` fields.
   - Verify: `go test -tags e2e -run TestE2E_Daemon ./internal/daemon/...` passes.

2. **Two-step transcribe→emit in one test.** The same test exercises both endpoints in sequence and
   asserts the contract boundary: `/transcribe` returns an `ActionProposal` JSON body **without writing
   the buffer** (buffer length 0 after transcribe), and `/emit` writes **exactly one** line (buffer has
   exactly one trailing `\n`, no extra lines).
   - Verify: assertions on buffer state after each step; test fails if `/transcribe` writes the buffer
     or `/emit` writes ≠1 line.

3. **Executable `extractInstruction` parse-contract test.** Extract the inline python3 snippet from
   `SKILL.md` into a standalone testable script `scripts/extract-instruction.py` (behavior identical:
   read line on stdin → print `Rewritten` field, raw fallback on non-JSON or empty `Rewritten`).
   SKILL.md is updated to reference the script instead of inlining the snippet. A test
   (`scripts/extract-instruction_test.sh` or a Go test that shells the script) asserts three cases:
   - serve-emitted JSON line (`{"rewritten":"do X",...}`) → extracts `do X`;
   - non-JSON line → raw fallback (the line unchanged);
   - JSON without `rewritten` → raw fallback.
   - Verify: the test runs the script as a black box and all three cases pass.
   - Note: the Event JSON field is `rewritten` (lowercase, per `eventlog.go` struct tag), NOT
     `Rewritten`. The current SKILL.md snippet reads `obj.get('Rewritten', ...)` and would therefore
     **never match a real serve line** — fixing this field-name mismatch is part of the contract test
     and is the concrete bug this goal surfaces.

4. **Manual verification checklist.** A doc `docs/manual-verification/monitor-e2e.md` records the
   harness-level step that cannot be automated: arm the Monitor (`/voci-listen`), POST to `/emit` (or
   speak an utterance), and confirm the live Claude Code session received the push AND ran
   `extractInstruction → execute`. Concrete steps + expected observations (Monitor wake log line,
   `[voci-listen] instruction: …` echo, the instruction visibly acted on in-session).
   - Verify: doc exists with numbered steps and an explicit "expected observation" per step.

5. **Green gates.** `go test -tags e2e ./internal/daemon/...` passes and existing `go test ./...`
   stays green (no regression to `server_test.go`, `eventlog_test.go`, or the mcp suite).
   - Verify: both commands exit 0.

## Proposed Approach

- **Producer e2e (Goals 1+2):** New file `internal/daemon/e2e_test.go` with `//go:build e2e`, mirroring
  the `internal/mcp/e2e_test.go` structure — a `newDeterministicServer`-style helper returning a
  `*daemon.Server` whose four pipeline fns are deterministic stubs and whose `EventWriter` is a
  caller-held `*bytes.Buffer`. Drive it through `httptest.NewServer(srv.Handler())` with two `http.Post`
  calls. Assert buffer state between steps. This reuses the existing `Server`/`Event` types unchanged —
  `EventWriter io.Writer` is already injectable, so no production code change is needed for the producer
  side.

- **Consumer parse contract (Goal 3):** Lift the python3 snippet out of SKILL.md into
  `scripts/extract-instruction.py` so it is unit-testable as a black box. SKILL.md's "extractInstruction
  (per-line handler)" section calls the script (`python3 scripts/extract-instruction.py`) instead of
  inlining it. Keep observable behavior identical except for the necessary `Rewritten`→`rewritten`
  field-name fix. Add a small test harness that pipes the three fixture lines and checks stdout.

- **Manual checklist (Goal 4):** A short markdown doc under `docs/manual-verification/` capturing the
  irreducibly-manual harness step with reproducible commands and expected observations, so future manual
  runs are checklist-driven rather than ad hoc.

- **Field-name source of truth:** All JSON fixtures use the real `daemon.Event` lowercase tags
  (`rewritten`, `kind`, `raw_transcript`, …) so producer test and consumer test share one contract.

## Trade-offs / Risks

- **Cannot automate the actual session injection.** Whether Claude Code *acts* on the pushed
  instruction is harness-owned and not reachable from Go/script tests — hence Goal 4's manual checklist
  rather than an automated assertion. This is a deliberate, documented boundary, not a coverage gap to
  pretend away.

- **Refactoring `extractInstruction` into a script could break SKILL.md contracts.** SKILL.md has
  `grep`-based self-contracts (e.g. `grep: "Rewritten"`). Moving the snippet to a script and fixing the
  field name must keep those contracts satisfied: the SKILL.md prose still references `Rewritten` (the
  conceptual field name in the spec) and the script reference line must not violate `not-grep`
  constraints. We keep observable behavior identical and re-run the contract greps after the edit.

- **Field-name bug (`Rewritten` vs `rewritten`).** Discovered while reading `eventlog.go`: the emitted
  Event uses lowercase `rewritten`, but the SKILL.md python snippet reads `Rewritten`, so it would
  always hit the raw fallback. The contract test both proves and fixes this. Risk: if some other
  consumer depends on the raw-fallback-everything behavior, fixing it changes output — none found in the
  codebase, but flagged for plan review.

- **`/emit` does not set `Timestamp`.** `handleEmit` constructs `Event{Rewritten, Kind}` directly
  (Timestamp empty); only `AppendEvent` backfills it. The producer test asserts on `Rewritten`/`Kind`
  only and does not require `Timestamp` on the stdout line, matching current behavior. If the plan wants
  a timestamp on the stdout line that is a separate change, out of scope here.

## Self-Review

- **Motivation (WHY) clear?** Yes — a concrete prior failure (Monitor push received, never executed)
  plus the structural "untested middle" argument. ✓
- **Goals verifiable?** Each has an explicit verify command or assertion (test run, buffer-state check,
  three-case black-box test, doc existence, two green gates). ✓
- **Approach aligns with goals?** Yes — mirrors the existing mcp e2e pattern, reuses injectable
  `EventWriter`, extracts the parse into a testable unit, doc for the manual step. One file per concern.
  ✓
- **Trade-offs honest?** Yes — names the irreducibly-manual boundary, the SKILL.md contract-break risk,
  AND surfaces a real latent bug (field-name mismatch, missing timestamp) found by reading the code
  rather than assuming. ✓
- **Consistency with repo conventions?** Yes — `//go:build e2e` tag + `go test -tags e2e`, deterministic
  stubs, `httptest.NewServer`, real `Event`/`Server` types, scripts under `scripts/`, docs under
  `docs/`. ✓

Round 1: surfaced the `Rewritten`/`rewritten` field-name mismatch and the missing-timestamp detail from
reading `eventlog.go`/`server.go`; folded both into Goal 3 and Trade-offs.
Round 2: tightened Goal 2 verification to explicit buffer-state assertions (0 after transcribe, exactly
1 line after emit).
Round 3: no further issues — APPROVED.

---

# Plan: Monitor 端到端执行验证（serve→emit→事件契约可自动化测试）

Closes the two automatable links of the voice→Monitor→session chain (producer: `serve`→stdout
JSON line; consumer-parse: `extractInstruction`) and pins the irreducibly-manual harness link
(live Claude Code acting on the instruction) to a checklist. Mirrors the existing
`internal/mcp/e2e_test.go` `//go:build e2e` pattern. Surfaces and fixes the real `Rewritten` vs
`rewritten` field-name bug in the SKILL.md python snippet.

## Phase A: daemon serve 生产者 e2e 测试

### Tests (write first)
Create `internal/daemon/e2e_test.go` (package `daemon`, build tag `//go:build e2e`):
- `TestE2E_Daemon_TranscribeDoesNotEmit` — build `&daemon.Server{...}` with deterministic stub
  `TranscribeFn`/`HintedFn`/`RewriteFn`/`ClassifyFn` and a caller-held `EventWriter = &bytes.Buffer{}`;
  serve via `httptest.NewServer(srv.Handler())`; `http.Post` audio bytes to `/api/voice/transcribe`;
  assert HTTP 200, body decodes to `intent.ActionProposal`, and the buffer length is 0 (transcribe
  must not write the EventWriter).
- `TestE2E_Daemon_EmitWritesOneLine` — POST `{"text":"do X"}` to `/api/voice/emit`; assert HTTP 204;
  assert the buffer contains exactly one `\n`-terminated line; the line unmarshals to a `daemon.Event`
  with `Rewritten == "do X"` and `Kind == "direct_prompt"`.
- `TestE2E_Daemon_TwoStepContract` — same server, sequential `/transcribe` then `/emit`; assert buffer
  is empty after transcribe and has exactly one line after emit (no extra lines).
Mirror the stub style of `internal/mcp/e2e_test.go` / `testutil_test.go`. These fail first because
`e2e_test.go` does not yet exist.

### Implementation
Write `internal/daemon/e2e_test.go`. The daemon `Server` has NO `NewServer` constructor (unlike the
mcp package) — construct via struct literal `&daemon.Server{TranscribeFn:..., EventWriter: buf, ...}`,
following the existing `makeServer` helper in `server_test.go`. No production code change is needed —
`EventWriter io.Writer` is already injectable. Use `bufio.Scanner` or `bytes.Count(buf.Bytes(), []byte{'\n'})`
to count lines. Endpoints are the full paths `/api/voice/transcribe` and `/api/voice/emit`.

### DoD
- [ ] `go test -tags e2e -run TestE2E_Daemon ./internal/daemon/...`
- [ ] `go test ./...`
- [ ] `test -f internal/daemon/e2e_test.go`
- [ ] `head -1 internal/daemon/e2e_test.go | grep -q 'go:build e2e'`

## Phase B: extractInstruction 解析契约可测 + 修复字段名 bug

### Tests (write first)
Create `scripts/extract-instruction_test.sh` (executable; shells `scripts/extract-instruction.py` as a
black box, exits non-zero on any mismatch). Three cases piped on stdin:
- serve-emitted line `{"rewritten":"do X","kind":"direct_prompt"}` → stdout is `do X`;
- non-JSON line `hello world` → raw fallback `hello world`;
- JSON without `rewritten` `{"kind":"direct_prompt"}` → raw fallback (the line unchanged).
The test fails first because `scripts/extract-instruction.py` does not yet exist; it also encodes the
correct lowercase `rewritten` behavior, which the current capital-`Rewritten` snippet would fail.

### Implementation
- Create `scripts/extract-instruction.py` (python3 — already used by SKILL.md) reading one line on
  stdin: parse JSON, print `obj.get('rewritten','').strip()`; on parse failure or empty `rewritten`,
  print the raw line stripped of trailing newline. Behavior identical to the SKILL.md snippet except
  the `Rewritten`→`rewritten` field-name fix. `chmod +x scripts/extract-instruction.py` and `.sh`.
- Update `.claude/skills/voci-listen/SKILL.md`: replace the inline python snippet in the
  "extractInstruction (per-line handler)" section with a call to `python3 scripts/extract-instruction.py`.
  CONSTRAINT: the SKILL.md `grep: "Rewritten" target: self` contract and `not-grep: "schedule\x28"`
  contract must stay satisfied — keep the conceptual word "Rewritten" in the prose (the spec field
  name), and never introduce `schedule(`. The lowercase fix lives only inside the `.py` script.

### DoD
- [ ] `go test ./...`
- [ ] `bash scripts/extract-instruction_test.sh`
- [ ] `printf '%s' '{"rewritten":"do X","kind":"direct_prompt"}' | python3 scripts/extract-instruction.py | grep -qx 'do X'`
- [ ] `printf '%s' 'hello world' | python3 scripts/extract-instruction.py | grep -qx 'hello world'`
- [ ] `! grep -q "get('Rewritten'" .claude/skills/voci-listen/SKILL.md`
- [ ] `grep -q "extract-instruction.py" .claude/skills/voci-listen/SKILL.md`
- [ ] `grep -q "Rewritten" .claude/skills/voci-listen/SKILL.md`
- [ ] `! grep -q 'schedule\x28' .claude/skills/voci-listen/SKILL.md`

## Phase C: 人工验证清单文档

### Tests (write first)
A presence check (prose is not unit-tested): assert `docs/manual-verification/monitor-e2e.md` exists and
contains the required section markers — a `## 人工验证` heading, numbered steps, and an explicit
"expected observation" per step (e.g. `[voci-listen] instruction:`). Encoded as the DoD grep commands
below; they fail first because the file does not yet exist.

### Implementation
Create `docs/manual-verification/monitor-e2e.md`: the harness-level checklist for the step that cannot
be automated — arm the Monitor (`/voci-listen`), POST to `/api/voice/emit` (or speak an utterance),
confirm the live Claude Code session received the push AND ran `extractInstruction → execute`. Include
concrete commands and an explicit expected observation per numbered step (Monitor wake log line,
`[voci-listen] instruction: …` echo, the instruction visibly acted on in-session).

### DoD
- [ ] `go test ./...`
- [ ] `test -f docs/manual-verification/monitor-e2e.md`
- [ ] `grep -q '## 人工验证' docs/manual-verification/monitor-e2e.md`
- [ ] `grep -q 'voci-listen' docs/manual-verification/monitor-e2e.md`
- [ ] `grep -q '/api/voice/emit' docs/manual-verification/monitor-e2e.md`
- [ ] `grep -q '\[voci-listen\] instruction:' docs/manual-verification/monitor-e2e.md`

## Constraints
- The actual live-session injection (whether Claude Code acts on the pushed instruction) is
  harness-owned and not reachable from Go/script tests — Phase C's manual checklist covers it; it is a
  deliberate, documented boundary, not a coverage gap.
- Keep `extractInstruction` observable behavior identical except for the `Rewritten`→`rewritten`
  field-name fix; the lowercase fix lives only in `scripts/extract-instruction.py`, while SKILL.md prose
  keeps the conceptual word "Rewritten" to satisfy its self-grep contract.
- The daemon `Server` has no `NewServer` constructor — build it via struct literal as in
  `internal/daemon/server_test.go` `makeServer`.
- `/api/voice/emit` does not set `Timestamp` (only `AppendEvent` backfills it); the producer test asserts
  on `Rewritten`/`Kind` only, matching current behavior — adding a stdout timestamp is out of scope.
- e2e tests are gated behind `//go:build e2e` and excluded from the default `go test ./...`.

## Acceptance Gate
- [ ] `go test ./...`
- [ ] `go test -tags e2e ./internal/daemon/...`
- [ ] `test -f internal/daemon/e2e_test.go`
- [ ] `test -f scripts/extract-instruction.py`
- [ ] `test -f docs/manual-verification/monitor-e2e.md`
- [ ] `! grep -q "get('Rewritten'" .claude/skills/voci-listen/SKILL.md`
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Proposal self-review: APPROVED
premise-ledger:
[E] daemon e2e producer test asserts exactly-one valid JSON Event line: basis — internal/daemon/server.go handleEmit writes Event{Rewritten,Kind:"direct_prompt"}+\n to EventWriter io.Writer (injectable bytes.Buffer); internal/mcp/e2e_test.go is the proven mirror pattern.
[E] transcribe does not write buffer, emit writes one line: basis — handleTranscribe returns proposal JSON only (server.go:64 comment + L127-128); handleEmit is the only writer (server.go:44).
[E] Event JSON field is lowercase 'rewritten' not 'Rewritten': basis — eventlog.go:11-17 struct tags; SKILL.md:151 reads obj.get('Rewritten') → latent bug, always raw-fallback.
[C] extractInstruction extractable into scripts/extract-instruction.py keeping behavior: basis — SKILL.md:143-162 is a self-contained python3+bash snippet; scripts/ already hosts Go helpers, adding a script is conventional.
[C] SKILL.md grep self-contracts survive refactor: basis — contracts block SKILL.md:5-25 lists grep 'Rewritten' / not-grep 'schedule(' etc; prose retains conceptual field name.
[H] live Claude Code session actually executes instruction: basis — harness-owned, not reachable from Go/script; covered by manual checklist doc only (Goal 4).
GCL-self-report: E=3 C=2 H=1

Plan review iteration 1: APPROVED
premise-ledger:
[E] files server.go/eventlog.go/mcp e2e_test.go/SKILL.md/server_test.go exist: verified via ls
[E] parent dirs docs/ scripts/ exist; docs/manual-verification & scripts/extract-instruction.py absent (created by plan): verified via ls
[E] field-name bug: Event json tag 'rewritten' (eventlog.go:13) vs SKILL.md snippet get('Rewritten') (SKILL.md:151): verified via grep
[E] self-contract grep:'Rewritten' (SKILL.md:18) + not-grep:'schedule\x28' (SKILL.md:12) present; prose 'Rewritten' survives edit: verified
[E] Server struct-literal construction, EventWriter io.Writer injectable, /emit writes one Event{Rewritten,Kind} line +204, /transcribe does not write: verified server.go:45,137-174
[E] python3 available at /usr/bin/python3: verified
[E] DoD red->green: pre-fix get('Rewritten') PRESENT so '! grep -q' fails before fix passes after: verified
[E] all DoD grep patterns (CJK heading, bracket-escape, schedule\x28) behave correctly under system grep: verified
[C] mcp e2e pattern (//go:build e2e + httptest.NewServer + deterministic stubs) mirrorable in daemon: from reading mcp/e2e_test.go + server_test.go makeServer
[H] live-session injection irreducibly manual (Phase C boundary): proposal-asserted, accepted
checklist: Goal coverage PASS; TDD structure PASS; TDD order PASS (Phase A first DoD go test -tags e2e); Acceptance first item go test ./... PASS; DoD executability PASS; absence checks use '! grep -q' PASS; Phase ordering PASS; Scope discipline PASS; file paths PASS
GCL-self-report: E=8 C=1 H=1
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Three-phase implementation:

Phase A: internal/daemon/e2e_test.go (//go:build e2e) — 3 tests asserting the two-step serve contract: /transcribe returns ActionProposal with no EventWriter write; /emit writes exactly one JSON Event line with correct Rewritten/Kind fields.

Phase B: scripts/extract-instruction.py — standalone testable script extracting the 'rewritten' field (fixes latent bug: SKILL.md used obj.get('Rewritten') but Event JSON uses lowercase 'rewritten'). scripts/extract-instruction_test.sh verifies 3 cases (JSON hit, non-JSON raw fallback, missing key fallback). SKILL.md extractInstruction section updated to call the script.

Phase C: docs/manual-verification/monitor-e2e.md — numbered checklist for the irreducibly-manual harness step (Monitor push → Claude Code executes instruction in session).

Commit: 5fe4012
<!-- SECTION:FINAL_SUMMARY:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 go test -tags e2e -run TestE2E_Daemon ./internal/daemon/...
- [ ] #2 go test ./...
- [ ] #3 test -f internal/daemon/e2e_test.go
- [ ] #4 head -1 internal/daemon/e2e_test.go | grep -q 'go:build e2e'
- [ ] #5 go test ./...
- [ ] #6 bash scripts/extract-instruction_test.sh
- [ ] #7 printf '%s' '{"rewritten":"do X","kind":"direct_prompt"}' | python3 scripts/extract-instruction.py | grep -qx 'do X'
- [ ] #8 printf '%s' 'hello world' | python3 scripts/extract-instruction.py | grep -qx 'hello world'
- [ ] #9 ! grep -q "get('Rewritten'" .claude/skills/voci-listen/SKILL.md
- [ ] #10 grep -q "extract-instruction.py" .claude/skills/voci-listen/SKILL.md
- [ ] #11 grep -q "Rewritten" .claude/skills/voci-listen/SKILL.md
- [ ] #12 ! grep -q 'schedule\x28' .claude/skills/voci-listen/SKILL.md
- [ ] #13 go test ./...
- [ ] #14 test -f docs/manual-verification/monitor-e2e.md
- [ ] #15 grep -q '## 人工验证' docs/manual-verification/monitor-e2e.md
- [ ] #16 grep -q 'voci-listen' docs/manual-verification/monitor-e2e.md
- [ ] #17 grep -q '/api/voice/emit' docs/manual-verification/monitor-e2e.md
- [ ] #18 grep -q '\[voci-listen\] instruction:' docs/manual-verification/monitor-e2e.md
- [ ] #19 go test ./...
- [ ] #20 go test -tags e2e ./internal/daemon/...
- [ ] #21 test -f internal/daemon/e2e_test.go
- [ ] #22 test -f scripts/extract-instruction.py
- [ ] #23 test -f docs/manual-verification/monitor-e2e.md
- [ ] #24 ! grep -q "get('Rewritten'" .claude/skills/voci-listen/SKILL.md
<!-- DOD:END -->

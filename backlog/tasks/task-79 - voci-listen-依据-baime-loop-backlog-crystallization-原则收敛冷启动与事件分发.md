---
id: TASK-79
title: voci-listen 依据 baime loop-backlog crystallization 原则收敛冷启动与事件分发
status: 'Basic: Backlog'
assignee: []
created_date: '2026-07-02 10:06'
updated_date: '2026-07-02 10:17'
labels:
  - 'kind:basic'
dependencies: []
ordinal: 48000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
voci-listen 参照 baime loop-backlog 重构经验（ADR-013/014 crystallization 原则）收敛：(1) voci serve 自身吸收 listen-preflight 的 stale-lock sweep/reconnect 检测逻辑，使 SKILL.md coldStart 收敛为无条件 1 次 Monitor 调用（不再需要 LLM 先跑 Bash 并对 stopped/reconnect/coldstart 做 switch 判断）；(2) Monitor description 中的事件分发 prose（type==startup vs rewritten 字段的 if/else 判断逻辑）收窄为通用措辞，改由 voci serve 在每条 stdout JSON 事件里自带一个可直接执行的 action/instruction 字段，消除 Monitor description 里靠 LLM 记忆维护的分发表
<!-- SECTION:DESCRIPTION:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
# Proposal: Crystallize voci-listen's preflight/dispatch logic into voci serve

## Background
`SKILL.md`'s `coldStart` composes the Monitor `description` string at arm time (wire.go's `--listen-preflight` path, `Monitor(...)` call at SKILL.md:105/176), encoding `type=="startup"` vs `rewritten`-field dispatch logic in free prose.
Per baime ADR-013, an LLM-authored Monitor description is fragile across `/clear`/compaction — baime saw a 340-char dispatch table shrink to 79 chars on reconnect, silently dropping a safety guard.
`coldStart` also requires the LLM to run `voci listen-preflight` via Bash and interpret its `stopped`/`reconnect`/`coldstart` output through a shell `case` before deciding whether to call `Monitor(...)` at all.
This is exactly the "check-then-decide-before-the-tool-call" pattern baime's ADR-014 found caused a 1-minute deliberation stall (exit 144) in `loop-backlog`, fixed there by pushing the decision into the daemon (`scan-loop.js` self-reaping at its own startup).
The sweep/reconnect logic voci needs already exists in Go (`internal/daemon/session/preflight.go`'s `Preflight`, `SweepStaleLocks`, `SweepStaleStatuses`, `SweepOrphanLocks`) — the open question is why the LLM still has to shell out to it and branch on the result before arming, rather than `voci serve` absorbing that branch itself.

## Goals
1. `voci serve --share --serve-port 0 --lock-dir ~/.voci --session-id <id>` self-detects, at its own process startup, the same three preflight outcomes `session.Preflight` already computes (stopped / reconnect / coldstart) and reacts by emitting a corresponding stdout JSON event (`{"type":"stopped"}` or `{"type":"reconnect", "local_url":..., "share_url":..., "bearer_token":...}` or continuing to the existing `{"type":"startup",...}` event) and exiting immediately for the stopped/reconnect cases — verifiable by unit tests in `internal/wire` asserting stdout content and process exit code for each of the three states, without needing a live tunnel.
2. `SKILL.md`'s `coldStart` unconditionally arms `Monitor(persistent=true, command="voci serve ...")` with no intermediate Bash call to `voci listen-preflight` and no shell `case` statement gating whether `Monitor(...)` is invoked — verifiable by grepping the revised `SKILL.md` for absence of `listen-preflight` and absence of a `case "$DECISION"` block gating `Monitor(`.
3. Every stdout JSON event emitted by `voci serve` (startup, stopped, reconnect, voice/rewritten) carries an explicit `instruction`-style field telling the receiving session exactly what to do (e.g. `"action":"display_and_wait"` for startup/reconnect, `"action":"execute"` plus `"rewritten"` for voice events, `"action":"exit"` for stopped) — verifiable by grepping `internal/wire/wire.go` / `internal/daemon/handlers.go` JSON struct definitions for an `action`/equivalent field on all four emitted event shapes.
4. The Monitor `description` string in the revised `SKILL.md` is reduced to a generic, static instruction ("follow the event's action field verbatim; do not re-arm; do not ask for confirmation") that does not itself encode `type==startup` vs `rewritten`-field branching prose — verifiable by diffing the description string length/content before and after and confirming it no longer names `type` or `rewritten` as fields to switch on (that classification is now implicit in each event's own `action` field, not something the LLM must remember).
5. `voci listen-preflight` as a standalone CLI subcommand (`internal/wire/wire.go` `case "listen-preflight"`) is either removed or kept only as a deprecated, no-op-compatible thin wrapper — verifiable by checking no remaining caller (grep `voci listen-preflight` across `plugin/`, `e2e/`, `Makefile`, docs) requires it after the SKILL.md rewrite.

## Proposed Approach
Move the branch currently taken by the LLM (interpret `voci listen-preflight` stdout, then conditionally call `Monitor`) into `voci serve`'s own startup path in `internal/wire/wire.go`'s `*serveFlag` block (around line 227-256, before `session.WriteLock`/tunnel start). Concretely: call `session.Preflight(lockDir, os.Getpid(), session.ProcAncestry)` — the function already used by the `--listen-preflight` subcommand — at the top of `--serve` startup instead of (or in addition to) the bare `session.SweepStaleLocks` call currently there. On `Decision == "stopped"`: emit a `{"type":"stopped","action":"exit"}` stdout line and return immediately without starting the HTTP listener or tunnel. On `Decision == "reconnect"`: emit `{"type":"reconnect","action":"display_and_wait","local_url":...,"share_url":...,"bearer_token":...}` (fields already produced by `PreflightResult`/`session.ReadStatus`) and exit — since a live process already owns the lock, a second `voci serve` must not double-listen. On `Decision == "coldstart"`: proceed exactly as today (existing lock-write, tunnel-start, and startup-event-emission code at wire.go:349-437 is unchanged, just gains an `"action":"display_and_wait"` field on the existing startup JSON struct). This makes `voci serve` itself idempotent and safe to invoke unconditionally, which is what unlocks Goal 2: `SKILL.md`'s `coldStart` becomes a bare `Monitor(persistent=true, command="voci serve --share --serve-port 0 --lock-dir ~/.voci --session-id $SESSION_ID", description=<generic static string>)` with no preceding Bash preflight call and no shell `case` gate — the daemon process, not the LLM, decides whether to actually listen, tunnel, or immediately exit. Session-ID resolution (`session.ResolveSessionID`/`NewSessionID`, currently computed by the LLM's Bash preflight call and passed in as `--session-id`) stays as an LLM-side concern only insofar as it must still be computed once before the `Monitor` call to have a stable `$SESSION_ID` for lock/status file naming and for the Monitor to reconnect to a known lock name across compactions — this is a lighter, single deterministic lookup (claude ancestry PID), not a decision, so it can remain a minimal Bash one-liner or be folded into a tiny `voci session-id` helper subcommand if warranted. Also add the `action` field to the two events already emitted from `handleEmit` (`internal/daemon/handlers.go` around lines 126/203, both carrying `Rewritten`) so all four event shapes carry the instruction to the recipient explicitly, and simplify the Monitor `description` in `SKILL.md` accordingly (drop the `type=="startup"` vs `rewritten`-field prose per ADR-013's "keep description generic" guidance).

## Trade-offs and Risks
- Not touching the tunnel/auth/ASR pipeline, entity/hint system, or lock file JSON format (`LockEntry`, `StatusEntry` schemas unchanged) — this proposal only changes when/how `session.Preflight` is invoked and what `voci serve` emits before/instead of listening.
- Regression risk called out explicitly: if `Monitor`'s `command` is unconditionally `voci serve ...` and the LLM ever re-arms Monitor (e.g. after a crash or manual re-invocation) while `~/.voci/.listen-stop` still exists, the daemon must actually honor the stop sentinel and exit rather than silently restarting service the user explicitly asked to stop — this is why Goal 1 embeds the stopped-check inside `voci serve` itself (not just in the removed `listen-preflight` subcommand): the sentinel check must be re-run on every `voci serve` invocation, not only once at cold-start, otherwise "unconditional arm" would reintroduce the exact regression baime did not have to worry about (baime's `loop-backlog` Monitor is always desired to run; voci-listen's is not).
- The "reconnect" case has no baime analogue (baime's scan-loop self-reaps duplicate *scanner* instances so only one true instance need ever run; voci-listen's reconnect case is about *not restarting a still-live* `voci serve` and instead surfacing its existing URL/token to a fresh session after compaction). Folding reconnect detection into `voci serve` startup means a second `voci serve` invocation against the same session ID becomes a harmless "print info and exit" rather than a port/tunnel conflict — this needs test coverage for the case where the existing lock's PID is alive but the status file is stale or missing (fall back to constructing `LocalURL` from `entry.Port` only, as `Preflight` already does).
- Backward compatibility: grep confirms `voci listen-preflight` is referenced only from `SKILL.md` prose/scripts and `internal/wire/wire.go` itself (no `e2e/`, `Makefile`, or other doc references found) — safe to demote to a deprecated wrapper (calling the same `session.Preflight` and printing the same three-word/parametrized lines) rather than deleting outright, in case any external automation or a human operator still invokes it directly for diagnostics.
- Test-writing cost: `internal/wire` currently tests `--serve` and `--listen-preflight` as separate code paths; merging the preflight branch into `--serve` startup means new tests are needed for each of the three `Decision` outcomes reached via `--serve` itself (not just via `--listen-preflight`), to hold to CLAUDE.md's ≥80% coverage bar for `internal/wire`.

---

# Plan: Crystallize voci-listen's preflight/dispatch logic into voci serve

## Phase A: voci serve absorbs Preflight decision at its own startup

### Tests (write first)
In `/home/yale/work/voci/internal/wire/wire_test.go` (style of `TestListenPreflightDispatch`/
`TestListenPreflightDispatch_Stopped`, `TestServeWritesLock`):

- `TestServe_StoppedSentinel_EmitsStoppedEventAndExitsWithoutListening` — write `<dir>/.listen-stop`,
  run `["--serve","--lock-dir="+dir,"--session-id=test-sess"]` (no `--share`), assert `err==nil`, stdout
  is one line `{"type":"stopped","action":"exit"}`, and `session.ReadLock(dir,"test-sess")` errors
  (no lock file was ever written).
- `TestServe_Reconnect_EmitsReconnectEventAndExitsWithoutDoubleListening` — pre-write a live lock via
  `session.WriteLock(dir,"test-sess",os.Getpid(),12345)` and
  `session.WriteStatus(dir,"test-sess","http://127.0.0.1:12345","https://share.example.com","tok123")`,
  run `["--serve","--lock-dir="+dir,"--session-id=test-sess"]`, assert stdout is one line with
  `"type":"reconnect","action":"display_and_wait"` plus the three URL/token fields verbatim, and that no
  tunnel/listener spy (`startServeFn`/`fakeManagedFn`) was invoked.
- `TestServe_Coldstart_ProceedsToExistingStartupEventPath` — empty `dir` (no lock, no sentinel), reuse
  `TestServeStartupEventOnStdout`'s `--share`/`fakeManagedFn` setup, assert the startup JSON line now
  also carries `"action":"display_and_wait"` alongside `type/local_url/share_url/bearer_token` — proves
  Phase A doesn't regress today's coldstart path.
- `TestServe_PreflightSweepsStaleLocksBeforeDeciding` — write `<dir>/other-sess.lock` with a guaranteed-
  dead PID (`999999999`) before invoking `--serve` with an empty session, assert the file is gone
  afterward (proves `session.Preflight`, not the old bare `SweepStaleLocks`, now runs on `--serve`).

Must fail before implementation: today `--serve` only calls bare `session.SweepStaleLocks` and never
checks `.listen-stop` or an existing live lock before proceeding to listen; the `action` field doesn't
exist yet.

### Implementation
- `internal/wire/wire.go`, `*serveFlag` block (lines 227-257): replace the `if lockDir != "" { ...
  session.SweepStaleLocks(lockDir) ... }` block (249-257) with `session.Preflight(lockDir, os.Getpid(),
  session.ProcAncestry)` when `lockDir != ""`, called before any listener/tunnel setup. Branch on
  `res.Decision`:
  - `"stopped"`: marshal+write `{"type":"stopped","action":"exit"}` to the `stdout io.Writer` param
    (not `os.Stdout`), `return nil` — no lock write, no listen.
  - `"reconnect"`: marshal+write `{"type":"reconnect","action":"display_and_wait","local_url":...,
    "share_url":...,"bearer_token":...}` from `res.LocalURL/ShareURL/Token`, `return nil`.
  - `"coldstart"`: use `res.SessionID` as `sessionID` (replaces today's `NewSessionID()`-only fallback),
    fall through to existing lock-write/tunnel/listen code unchanged.
  - `lockDir == ""`: skip `Preflight`, keep today's un-locked behavior.
- Startup JSON struct (lines ~426-437, `Type/LocalURL/ShareURL/BearerToken`): add
  `Action string \`json:"action"\`` = `"display_and_wait"`.
- Add matching anonymous structs for stopped `{Type,Action}` and reconnect
  `{Type,Action,LocalURL,ShareURL,BearerToken}`, placed near the startup event block.
- Scope note: this only covers the `if *shareFlag` branch (which is where the only stdout startup event
  exists today); the non-`--share` `srv.StartWithContext(serveCtx, addr)` tail is unchanged except that
  `Preflight`'s stopped/reconnect short-circuit runs earlier and applies to it too.

### DoD
- [ ] `go test ./internal/wire/... -run 'TestServe_StoppedSentinel|TestServe_Reconnect|TestServe_Coldstart|TestServe_PreflightSweeps'`
- [ ] `go test ./... -run TestListenPreflightDispatch`
- [ ] `grep -n 'session.Preflight(lockDir' /home/yale/work/voci/internal/wire/wire.go`
- [ ] `grep -n '"action"' /home/yale/work/voci/internal/wire/wire.go`
- [ ] `! grep -n 'session.SweepStaleLocks(lockDir)' /home/yale/work/voci/internal/wire/wire.go`

## Phase B: handleEmit event gains action field + Monitor description slimmed + listen-preflight demoted

### Tests (write first)
- `internal/daemon/session/eventlog_test.go`: `TestEvent_MarshalsActionField` — marshal
  `session.Event{Rewritten:"do X",Kind:"direct_prompt",Action:"execute"}`, assert JSON contains
  `"action":"execute"`.
- `internal/daemon/server_test.go`: `TestEmit_EventIncludesExecuteAction` (adjacent to
  `TestEmit_WritesOneEventLineToEventWriter`, using `makeServer(t)`) — POST `{"text":"hello world"}` to
  `/api/voice/emit`, unmarshal the `EventWriter` line, assert `ev.Action == "execute"`.
- `internal/daemon/e2e_test.go`: `TestE2E_Emit_EventWriterJSONHasActionField` (alongside
  `TestE2E_Emit_EventWriterJSONMatchesGrepFilter`) — assert the raw JSON line contains the literal
  substring `"action"`, mirroring that test's existing `"rewritten"` substring check.
- SKILL.md's rewrite has no Go test; it is verified purely by the grep-based DoD/Acceptance Gate items
  below, consistent with this repo's `contracts:` grep/not-grep pattern already in SKILL.md frontmatter.

Must fail before implementation: `session.Event` has no `Action` field yet; `handleEmit` never sets one.

### Implementation
- `internal/daemon/session/eventlog.go`: add `Action string \`json:"action"\`` to `Event` (after `Kind`).
- `internal/daemon/handlers.go`, `handleEmit` (~lines 202-205): set `Action: "execute"` on the
  `session.Event{...}` literal — the sole stdout-emitting event site in this file (the
  `model.ActionProposal` JSON at lines 124-127 is an HTTP response to the browser, not a stdout Monitor
  event, and is unaffected).
- `plugin/skills/voci-listen/SKILL.md`: rewrite `coldStart` (lines 77-108, and the `### coldStart` Bash
  block lines 137-179) dropping `Bash("voci listen-preflight ...")` and the `switch`/`case "$DECISION"`
  gate entirely; replace with an unconditional `Monitor(persistent=true, command="voci serve --share
  --serve-port 0 --lock-dir ~/.voci --session-id $SESSION_ID", description=<generic static string, e.g.
  "voci-listen: event arrived — DO NOT call /voci-listen again. Follow the event's 'action' field
  verbatim (display_and_wait: show local_url/share_url/bearer_token; execute: run 'rewritten' as the
  next instruction inline; exit: stop). Do not re-arm. Do not ask for confirmation.">). `$SESSION_ID`
  resolution stays a minimal deterministic Bash one-liner (ancestry PID lookup) or a tiny new `voci
  session-id` helper if needed — not a decision, so it stays LLM-side per the proposal's trade-off.
  Update `classifyEvent`/`extractInstruction`/`onMonitorEvent` prose (lines 110-134) to reference
  `action` instead of `type=="startup"`/`rewritten`-presence branching. Update `contracts:` frontmatter
  (lines 5-39): drop or flip to `not-grep` the `grep: "listen-preflight"` entry (lines 22-23) once the
  Bash call is gone.
- `internal/wire/wire.go`: demote `listen-preflight` per Goal 5 (grep confirms it's referenced only in
  `wire.go` and `SKILL.md` — no `e2e/`/`Makefile`/doc callers). Keep the subcommand working unchanged
  (external diagnostics use, per proposal Trade-offs) but mark it deprecated: update its flag usage
  string to `"(deprecated: voci serve now self-detects this at startup)"` and add a doc-comment on
  `case "listen-preflight"` in `dispatch`.

### DoD
- [ ] `go test ./... -run 'TestEvent_MarshalsActionField|TestEmit_EventIncludesExecuteAction|TestE2E_Emit_EventWriterJSONHasActionField'`
- [ ] `grep -n 'Action.*json:"action"' /home/yale/work/voci/internal/daemon/session/eventlog.go`
- [ ] `grep -n 'Action: "execute"' /home/yale/work/voci/internal/daemon/handlers.go`
- [ ] `! grep -q 'voci listen-preflight' /home/yale/work/voci/plugin/skills/voci-listen/SKILL.md`
- [ ] `! grep -q 'case "\$DECISION"' /home/yale/work/voci/plugin/skills/voci-listen/SKILL.md`
- [ ] `! grep -qE 'type==.?startup' /home/yale/work/voci/plugin/skills/voci-listen/SKILL.md`
- [ ] `grep -qi "action.*field verbatim" /home/yale/work/voci/plugin/skills/voci-listen/SKILL.md`
- [ ] `grep -n 'deprecated' /home/yale/work/voci/internal/wire/wire.go`

## Constraints
- Do not touch the tunnel/auth/ASR pipeline, entity/hint system, or lock/status file JSON schemas
  (`LockEntry`, `StatusEntry` unchanged — only `session.Event` gains a field).
- Do not change the non-`--share` `srv.StartWithContext(serveCtx, addr)` tail beyond the earlier
  `Preflight` short-circuit; it emits no startup JSON event today and this plan adds none.
- A second `voci serve` invocation against a still-live session ID must never open a second HTTP
  listener or start a second tunnel.
- `voci listen-preflight` must remain callable (deprecated wrapper, not deleted) per the proposal's
  Trade-offs reservation for external diagnostics.
- The stop-sentinel check must be re-evaluated on every `voci serve` invocation (via `Preflight` at the
  top of every `--serve` startup, never cached), per the proposal's stated regression risk.

## Acceptance Gate
- [ ] `go test ./...`
- [ ] `go test -coverprofile=/tmp/cover.out ./... && go tool cover -func=/tmp/cover.out | grep -E '^total:'`
- [ ] `make build`
- [ ] `! grep -q 'voci listen-preflight' /home/yale/work/voci/plugin/skills/voci-listen/SKILL.md`
- [ ] `! grep -qE 'type==.?startup' /home/yale/work/voci/plugin/skills/voci-listen/SKILL.md`
- [ ] `grep -q '"action"' /home/yale/work/voci/internal/wire/wire.go`
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Proposal self-review: APPROVED
premise-ledger:
[E] Monitor description composed at coldStart with type==startup vs rewritten dispatch prose: SKILL.md:105/176 as read verbatim
[E] listen-preflight subcommand exists and dispatches to session.Preflight: internal/wire/wire.go:95-96,187,193-216 as read
[E] session.Preflight implements sweep+sentinel+resolve+reconnect/coldstart branch: internal/daemon/session/preflight.go:47-96 as read
[E] SweepStaleLocks/SweepStaleStatuses/SweepOrphanLocks exist as separate functions: internal/daemon/session/lock.go, status.go, preflight.go as read
[E] --serve path currently only calls bare SweepStaleLocks, not full Preflight: internal/wire/wire.go:253 as read
[E] startup JSON event shape (type/local_url/share_url/bearer_token) and emission point: internal/wire/wire.go:424-437 as read
[E] handleEmit emits two Rewritten-bearing JSON events: internal/daemon/handlers.go:126,203 as read
[C] baime ADR-013 (Monitor description integrity, 340->79 char shrink) and ADR-014 (crystallization boundary, scan-loop self-reaping) content: taken from prior-agent-researched context supplied in the task prompt, not independently re-read by me
[H] No other caller of `voci listen-preflight` exists outside plugin/wire.go: based on grep of plugin/, e2e/, Makefile in this repo checkout only, not exhaustive of external automation
[H] Folding Preflight into --serve startup is safe/idempotent and won't introduce new races beyond what's documented in Trade-offs: architectural judgment, not verified by a working implementation or test run
GCL-self-report: E=7 C=1 H=2

Proposal approved. Starting plan draft.

Plan review iteration 1: APPROVED
premise-ledger:
[E] Goal coverage: Goals 1-5 each mapped to Phase A/B DoD or Acceptance Gate items — verified by reading plan text
[E] TDD structure: both Phase A and Phase B have ### Tests then ### Implementation then ### DoD, in that order — verified via grep of headers in task file
[E] TDD order: Phase A first DoD is `go test ./internal/wire/... -run '...'`, Phase B first DoD is `go test ./... -run '...'` — both are go test invocations proving red→green; Phase B literally matches `go test ./...`
[E] Acceptance Gate first item is literally `go test ./...` — verified by reading plan text
[E] DoD executability: all Phase A/B DoD and Acceptance Gate items are grep/go test/make shell commands, no natural-language items mixed in — verified by reading each line
[E] Absence checks use `! grep -q <pattern> <file>` form throughout (5 instances checked), not `grep -qv` — verified by reading each item
[E] Phase ordering: Phase A (voci serve absorbs Preflight + action field on wire.go events) precedes Phase B (SKILL.md rewrite depends on Phase A's serve-side self-detection; Event.Action field in eventlog.go is independent) — no circular deps
[E] Scope discipline: Phase A implementation (Preflight absorption, startup/stopped/reconnect action fields) maps to Goals 1+3; Phase B (Event.Action field, SKILL.md rewrite, listen-preflight deprecation) maps to Goals 2+3+4+5 — no extraneous work found
[E] File paths verified via Read/grep against actual repo state: internal/wire/wire.go (serveFlag block lines 227-257, SweepStaleLocks at line 253, startup JSON struct lines 424-437, listen-preflight case at line 95-96/187/193-216), internal/daemon/handlers.go (handleEmit ev:=session.Event at line 202, EventWriter.Write at line 208 — sole stdout-emitting site in file, confirmed via repo-wide grep), internal/daemon/session/eventlog.go (Event struct, no Action field yet), internal/daemon/session/preflight.go (Preflight/PreflightResult/Decision values), plugin/skills/voci-listen/SKILL.md (280 lines; listen-preflight grep contract at line 22, coldStart pseudocode lines 76-108, classifyEvent/onMonitorEvent lines 110-124) — all claimed paths and line ranges hold
[E] Correctness sanity check: handlers.go lines 124-132 (`proposal := model.ActionProposal{...}; json.NewEncoder(w).Encode(proposal)`) writes to `w http.ResponseWriter` inside handleTranscribe — this is an HTTP response, not a stdout Monitor event. Confirmed via repo-wide grep for `EventWriter.Write` which found exactly one call site (handlers.go:208, inside handleEmit) — the plan's self-correction holds; Goal 3's action-field coverage (handleEmit's session.Event at line 202-208) is the correct and only site needing the fix, no missed stdout event site exists
[E] Existing test fixtures the plan says it reuses/extends genuinely exist: TestListenPreflightDispatch, TestListenPreflightDispatch_Stopped, TestServeWritesLock, TestServeStartupEventOnStdout (wire_test.go), makeServer, TestEmit_WritesOneEventLineToEventWriter (server_test.go), TestE2E_Emit_EventWriterJSONMatchesGrepFilter (e2e_test.go) — verified via grep
[E] go build ./... passes cleanly on current master before plan execution (baseline green)
GCL-self-report: E=11 C=0 H=0
<!-- SECTION:NOTES:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 go test ./internal/wire/... -run 'TestServe_StoppedSentinel|TestServe_Reconnect|TestServe_Coldstart|TestServe_PreflightSweeps'
- [ ] #2 go test ./... -run TestListenPreflightDispatch
- [ ] #3 grep -n 'session.Preflight(lockDir' /home/yale/work/voci/internal/wire/wire.go
- [ ] #4 grep -n '"action"' /home/yale/work/voci/internal/wire/wire.go
- [ ] #5 ! grep -n 'session.SweepStaleLocks(lockDir)' /home/yale/work/voci/internal/wire/wire.go
- [ ] #6 go test ./... -run 'TestEvent_MarshalsActionField|TestEmit_EventIncludesExecuteAction|TestE2E_Emit_EventWriterJSONHasActionField'
- [ ] #7 grep -n 'Action.*json:"action"' /home/yale/work/voci/internal/daemon/session/eventlog.go
- [ ] #8 grep -n 'Action: "execute"' /home/yale/work/voci/internal/daemon/handlers.go
- [ ] #9 ! grep -q 'voci listen-preflight' /home/yale/work/voci/plugin/skills/voci-listen/SKILL.md
- [ ] #10 ! grep -q 'case "\$DECISION"' /home/yale/work/voci/plugin/skills/voci-listen/SKILL.md
- [ ] #11 ! grep -qE 'type==.?startup' /home/yale/work/voci/plugin/skills/voci-listen/SKILL.md
- [ ] #12 grep -qi "action.*field verbatim" /home/yale/work/voci/plugin/skills/voci-listen/SKILL.md
- [ ] #13 grep -n 'deprecated' /home/yale/work/voci/internal/wire/wire.go
- [ ] #14 go test ./...
- [ ] #15 go test -coverprofile=/tmp/cover.out ./... && go tool cover -func=/tmp/cover.out | grep -E '^total:'
- [ ] #16 make build
<!-- DOD:END -->

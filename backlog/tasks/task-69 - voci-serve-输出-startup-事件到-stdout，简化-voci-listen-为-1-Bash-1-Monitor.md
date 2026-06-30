---
id: TASK-69
title: voci serve 输出 startup 事件到 stdout，简化 voci-listen 为 1 Bash + 1 Monitor
status: 'Basic: Backlog'
assignee: []
created_date: '2026-06-30 22:50'
updated_date: '2026-06-30 23:04'
labels:
  - 'kind:basic'
dependencies: []
ordinal: 42000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
voci serve 在 tunnel 就绪后向 stdout（EventWriter）写一条 {"type":"startup","local_url":"...","share_url":"...","bearer_token":"..."} JSON 事件。voci-listen skill 的 onMonitorEvent 识别 type=startup 后展示 URL 而非执行为指令。这使 voci-listen 冷启动从「1 Bash + 1 Monitor + 1 Bash（poll .status）」简化为「1 Bash + 1 Monitor」，同时不违反 TASK-63 原则（Monitor command 保持裸 voci serve，无 wrapper；stdout 仍只输出 JSON 事件行，startup 只是 type 不同）。
<!-- SECTION:DESCRIPTION:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
# Plan: voci serve 启动事件输出 — 消除 voci-listen 后续 Bash 调用

## Background

Current cold-start requires 3 tool calls: 1 Bash (SESSION_ID) + 1 Monitor (arm) + 1 Bash
(poll `.status`, display URL). The third Bash exists solely to display the tunnel URL after
startup. If `voci serve --share` writes a `{"type":"startup",...}` JSON line to stdout once
the tunnel is ready, that line arrives as a Monitor event and `voci-listen` can display the URL
inline — dropping the third Bash call entirely.

## Phase A: wire.go 写入 startup 事件到 stdout (EventWriter)

### Tests (write first)

File: `internal/wire/wire_test.go`

**Test 1 — `TestServeStartupEventOnStdout`** (new test, must FAIL before implementation)

Verify that after tunnel is ready, a JSON line with `"type":"startup"` is written to the
`stdout` parameter of `run()`. Use the existing `fakeManagedFn` pattern (tunnel exits
immediately via `exec.Command("true")`). Assert:
- `stdout.String()` contains `"type":"startup"`
- `stdout.String()` contains `"local_url":"http://127.0.0.1:`
- `stdout.String()` contains `"share_url":"https://voci-test.voci.example.com"`
- `stdout.String()` contains `"bearer_token":"tok"`

**Test 2 — `TestServeStartupEventWithLockDir`** (new test, must FAIL before implementation)

Same as Test 1 but also passes `--lock-dir=<tmpDir> --session-id=startup-sess`. Verify
the startup JSON event appears on stdout regardless of whether `lockDir` is set, and that
the event is written even when `WriteStatus` succeeds.

**Test 3 — update `TestServeStdoutOnlyEvents`** (existing test, must be updated before implementation)

The current assertion forbids `"voci local URL"`, `"voci share URL"`, `"Bearer token"` from
stdout. After the change stdout WILL contain a JSON startup event that embeds the URL and
token values. Update the test to:
- Still assert plain-text labels (`"voci local URL"`, `"voci share URL"`, `"Bearer token:"`)
  are absent from stdout (they stay on stderr).
- Assert `stdout.String()` contains `"type":"startup"` (startup JSON event is present).

### Implementation

File to modify: `internal/wire/wire.go`

Location: inside the `if *shareFlag {` block, after the `if lockDir != "" { session.WriteStatus(...) }` block and before `return srv.StartWithContextFromListener(tunnelCtx, ln)`.

Add import: `"encoding/json"` (if not already present).

Insert:

```go
// Emit startup event to stdout so Monitor-event dispatch can display URL without
// a separate Bash poll. Written unconditionally whenever the tunnel is ready.
startupLine, _ := json.Marshal(struct {
    Type        string `json:"type"`
    LocalURL    string `json:"local_url"`
    ShareURL    string `json:"share_url"`
    BearerToken string `json:"bearer_token"`
}{
    Type:        "startup",
    LocalURL:    fmt.Sprintf("http://127.0.0.1:%d", port),
    ShareURL:    publicURL,
    BearerToken: token,
})
fmt.Fprintf(stdout, "%s\n", startupLine)
```

Note: write to `stdout` (the `run()` parameter), not `os.Stdout`, so tests can capture it
via `bytes.Buffer`. In production `stdout = os.Stdout`, which is also `srv.EventWriter`,
so the event appears in the Monitor's output stream.

No other files need modification in Phase A.

### DoD

- [ ] `go test ./...`
- [ ] `go test ./internal/wire/... -run TestServeStartupEventOnStdout -v 2>&1 | grep -q PASS`
- [ ] `go test ./internal/wire/... -run TestServeStartupEventWithLockDir -v 2>&1 | grep -q PASS`
- [ ] `go test ./internal/wire/... -run TestServeStdoutOnlyEvents -v 2>&1 | grep -q PASS`
- [ ] `go test -coverprofile=/tmp/cover.out ./... && go tool cover -func=/tmp/cover.out | grep "^total:" | awk '{print $3}'`

---

## Phase B: voci-listen skill 处理 startup 事件

### Tests (write first)

The skill is a Markdown file, not Go code. Tests are grep assertions against the updated
`SKILL.md`, verified before the implementation edits are applied (the grep commands fail on
the current file).

Run these commands — all must FAIL before implementation (i.e., return exit code 1):

```bash
grep -q "StartupEvent" /home/yale/work/voci/.claude/skills/voci-listen/SKILL.md
grep -q '"startup"' /home/yale/work/voci/.claude/skills/voci-listen/SKILL.md
grep -q 'StartupEvent.*display\|displayStartup' /home/yale/work/voci/.claude/skills/voci-listen/SKILL.md
# This one must PASS before (poll loop present) and FAIL after (removed):
grep -q 'DEADLINE.*30\|sleep 0\.5' /home/yale/work/voci/.claude/skills/voci-listen/SKILL.md
```

### Implementation

File to modify: `.claude/skills/voci-listen/SKILL.md`

**Change 1 — `data EventKind` definition** (Spec section)

Add `StartupEvent` constructor alongside `VoiceEvent`:

```
data EventKind = VoiceEvent String         -- JSON line with Rewritten field → execute inline
               | StartupEvent String       -- JSON line with type=="startup" → display URL, no execute
```

**Change 2 — `classifyEvent` function** (Spec section)

Replace the trivial catch-all with a JSON-type-field check:

```
classifyEvent :: Line → EventKind
classifyEvent(line) =
  | line is valid JSON and line["type"] == "startup" → StartupEvent(line)
  | otherwise                                         → VoiceEvent(line)
```

**Change 3 — `onMonitorEvent` function** (Spec section)

Add `StartupEvent` dispatch branch (display, do not execute):

```
onMonitorEvent(line) = {
  if (stopSentinel()):
    cleanupLock(SESSION_ID),
    return: Stopped,
  kind: classifyEvent(line),
  | StartupEvent line → displayStartup(line),   -- parse local_url, share_url, bearer_token; show to user
  | VoiceEvent   line → execute(extractInstruction(line)),
  return: Listening,
}
```

Where `displayStartup(line)` extracts `local_url`, `share_url`, `bearer_token` from the
JSON and outputs them to the user (same information currently shown via `.status` poll).

**Change 4 — `ensureMonitor` function** (both Spec and Implementation sections)

Remove the 30-second `.status` poll loop entirely. The function ends after writing the task
ID to `~/.voci/$SESSION_ID.task`. The startup event will arrive as a Monitor notification;
`onMonitorEvent` handles it.

Remove from Implementation:
```bash
# Poll ~/.voci/$SESSION_ID.status for startup metadata (up to 30s, every 0.5s).
STATUS_FILE="${HOME}/.voci/${SESSION_ID}.status"
DEADLINE=$(($(date +%s) + 30))
while [ $(date +%s) -lt $DEADLINE ]; do
  ...
  sleep 0.5
done
```

**Change 5 — Monitor `description` field** (in `ensureMonitor` Implementation section)

Update the description string to instruct the Monitor-event handler to process both event
kinds:

```
description="voci-listen: event arrived — DO NOT call /voci-listen again.
  If JSON has type=='startup': extract local_url, share_url, bearer_token and display them to the user.
  Otherwise: parse the JSON line, extract the 'rewritten' field, execute it inline as the next instruction.
  If ~/.voci/.listen-stop exists, stop."
```

**Change 6 — Cross-/clear self-recovery prose** (Implementation section)

Update the "Cross-/clear self-recovery" paragraph to state that startup events arriving in
a new session are handled by the Monitor description (display URL rather than execute).
Remove the sentence claiming startup metadata "does not appear as Monitor events."

**Change 7 — frontmatter `description` field** (YAML header)

Update to mention startup event handling:

```
description: "... Monitor description is self-contained: on startup event arrival display URL/token to user; on voice event extract 'rewritten' and execute inline. ..."
```

**Change 8 — contracts block** (YAML header)

Add contract to verify `StartupEvent` is handled in the skill:

```yaml
  - grep: "StartupEvent"
    target: self
```

### DoD

- [ ] `go test ./...`
- [ ] `grep -q "StartupEvent" /home/yale/work/voci/.claude/skills/voci-listen/SKILL.md`
- [ ] `grep -q '"startup"' /home/yale/work/voci/.claude/skills/voci-listen/SKILL.md`
- [ ] `grep -q "StartupEvent.*display\|displayStartup" /home/yale/work/voci/.claude/skills/voci-listen/SKILL.md`
- [ ] `! grep -q "DEADLINE.*30\|sleep 0\.5" /home/yale/work/voci/.claude/skills/voci-listen/SKILL.md`
- [ ] `grep -q "type.*startup\|startup.*type" /home/yale/work/voci/.claude/skills/voci-listen/SKILL.md`

---

## Constraints

- `.status` file write (`session.WriteStatus`) and delete (`session.RemoveStatus`) logic in
  `wire.go` is preserved unchanged; it continues to serve reconnect detection and non-Monitor
  interactive paths.
- Non-`--share` paths (no tunnel) do not emit a startup event. This is a known limitation:
  without a Bearer token, startup metadata has limited value and no tunnel URL exists to display.
- The startup event is written to `stdout` (the `run()` parameter), which equals `os.Stdout`
  in production. No change to `daemon.Server`, `handlers.go`, or any other file is required.
- `daemon.Server.EventWriter` field is not modified; it remains `os.Stdout` and continues to
  carry voice events emitted by `handleEmit`.
- If the tunnel fails to start, `run()` returns an error before reaching the startup event
  write — the skill will wait silently until the next voice event (same behavior as the
  current 30-second poll timeout). Add a note in the Monitor description: "If no startup event
  arrives within ~60s, the tunnel may have failed; re-invoke /voci-listen."
- `encoding/json` may already be imported in `wire.go` via transitive dependencies; verify
  before adding a duplicate import. (Check: `wire.go` currently imports `encoding/json` — confirm.)
- The `testOnServerBuilt` hook in `wire.go` is not used by Phase A tests; tests capture
  output via the `stdout bytes.Buffer` parameter to `run()`.

## Acceptance Gate

- [ ] `go test ./...`
- [ ] `go test ./internal/wire/... -run TestServeStartupEventOnStdout -v 2>&1 | grep -q PASS`
- [ ] `go test ./internal/wire/... -run TestServeStartupEventWithLockDir -v 2>&1 | grep -q PASS`
- [ ] `grep -q "StartupEvent" /home/yale/work/voci/.claude/skills/voci-listen/SKILL.md`
- [ ] `! grep -q "DEADLINE.*30\|sleep 0\.5" /home/yale/work/voci/.claude/skills/voci-listen/SKILL.md`
- [ ] `go test -coverprofile=/tmp/cover.out ./... && go tool cover -func=/tmp/cover.out | grep "internal/wire" | awk '{if ($3+0 >= 80) print "OK wire coverage: " $3; else print "FAIL wire coverage too low: " $3}'`
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Plan review iteration 1: APPROVED
premise-ledger:
[E] All 4 referenced files exist (wire.go, wire_test.go, SKILL.md, server.go): confirmed via ls
[E] TestServeStdoutOnlyEvents exists in wire_test.go at line 1218: confirmed
[E] fakeManagedFn pattern exists in wire_test.go: confirmed
[E] SKILL.md has DEADLINE/sleep 0.5 patterns present (lines 351-352): confirmed — absence checks will correctly fail before implementation
[E] SKILL.md does not yet contain 'StartupEvent': confirmed — grep assertions correctly fail before implementation
[C] Goal coverage complete: Goal 1 → Phase A, Goals 2-4 → Phase B Changes 1-4, Goal 5 → Constraints
[C] TDD order correct: both Phase A and Phase B open DoD with go test ./...
[C] First Acceptance Gate item is go test ./...
[C] Phase ordering logical: A (Go wire.go) precedes B (SKILL.md consumer); no circular deps
[C] Absence checks use '! grep -q' not 'grep -qv'
[C] All DoD and Acceptance Gate items are shell commands; no natural-language items in those sections
[H] Phase A DoD last item prints coverage but does not assert threshold; Acceptance Gate has asserting wire-coverage check — acceptable weakness, coverage gate present at acceptance level
[H] Phase B 'tests' are grep assertions on Markdown — unconventional but appropriate for the artifact type (SKILL.md is not Go code)
[H] Acceptance Gate coverage awk always exits 0 regardless of pass/fail; a minor weakness but coverage enforcement is belt-and-suspenders given existing thresholds in CLAUDE.md
GCL-self-report: E=5 C=6 H=3
<!-- SECTION:NOTES:END -->

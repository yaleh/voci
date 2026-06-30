---
id: TASK-63
title: voci serve stdout/stderr 分离：移除 voci-listen Monitor command 中的 grep 包裹
status: 'Basic: Done'
assignee: []
created_date: '2026-06-30 13:18'
updated_date: '2026-06-30 13:49'
labels:
  - 'kind:basic'
dependencies: []
ordinal: 40000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
应用正确修法：Monitor 的 command 应直接是 voci serve（command 即扫描器本身，TASK-17 原则），不应用 grep 包裹。当前 TASK-54 引入的 grep 过滤使 TASK-62 的耗时日志（log.Printf → stderr → 被 grep 丢弃）无法观察。正确做法：voci serve stdout 只输出 JSON 事件行；启动信息（share URL、Bearer token）写入专用状态文件，skill 在 arm 后读取并展示。
<!-- SECTION:DESCRIPTION:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
# Proposal: voci serve stdout/stderr 分离：移除 voci-listen Monitor command 中的 grep 包裹

## Background

TASK-17 established that the Monitor command should be `voci serve` itself — no shell pipeline wrapping. TASK-54 violated this by adding `2>/dev/stdout | grep --line-buffered -E '"rewritten"|voci share URL|Bearer token'` because `--share` started printing the public tunnel URL and Bearer token to stderr, and users need to see them. The problem with the grep wrapper is twofold: (1) it re-mixes stderr (diagnostics, logs) and stdout (structured JSON events), blurring a boundary that should be clean; (2) any stderr line that does not match the grep pattern is silently dropped — concretely, TASK-62's per-step pipeline timing logs (`log.Printf("pipeline: asr: %dms, hinted: %dms …")` in `internal/daemon/handlers.go:99`) are written to stderr and therefore lost. The root cause is not that stderr must be merged, but that startup information (share URL, Bearer token) currently has no out-of-band delivery mechanism. Giving it one removes the need for the grep wrapper entirely.

## Goals

1. `voci serve` stdout carries only structured JSON event lines (the `{"rewritten":…}` records written by `handleEmit`); every other output goes to stderr or to a dedicated file — verifiable by `grep -v '"rewritten"' <stdout_capture>` returning no lines during normal operation.
2. Startup metadata (local URL, share URL, Bearer token) is written to `~/.voci/<session_id>.status` (a machine-readable file, e.g. JSON) immediately after the tunnel is ready — verifiable by inspecting the file after `voci serve --share` starts.
3. The Monitor command in `voci-listen` SKILL.md is reduced to a bare `voci serve --share --serve-port 0 --lock-dir ~/.voci --session-id $SESSION_ID` with no `2>/dev/stdout` redirect and no `grep` — verifiable by reading the skill and confirming no pipe character is present in the Monitor command string.
4. The `coldStart` path in the skill reads `~/.voci/<session_id>.status` (with a short retry loop until the file appears or a timeout expires) and displays the local URL, share URL, and Bearer token to the user — verifiable by observing the values printed in a test cold-start.
5. Pipeline timing logs from `log.Printf` in `handleTranscribe` reach stderr unfiltered and are visible when running `voci serve` interactively — verifiable by tailing stderr during a transcription request.

## Proposed Approach

**Binary side — write a status file instead of printing to stderr:**

In `internal/wire/wire.go`, after the tunnel is established and the three startup lines are currently printed to stderr, additionally write a `~/.voci/<session_id>.status` JSON file (alongside the existing `.lock` file) containing `{"local_url":…, "share_url":…, "bearer_token":…}`. The existing `session.WriteLock` pattern in `internal/daemon/session/lock.go` is the model: atomic write to `.tmp`, then rename. A parallel `WriteStatus` / `ReadStatus` pair should live in the same package. The stderr prints can remain for interactive use (they are harmless there); nothing in the Monitor path depends on them once the status file exists.

**Skill side — read the status file, drop the grep wrapper:**

In `.claude/skills/voci-listen/SKILL.md`:
- Remove `2>/dev/stdout | grep --line-buffered -E …` from the Monitor command string.
- Remove the `classifyEvent` branch that checks for `"voci local URL:"`, `"voci share URL:"`, and `"Bearer token:"` prefixes (those lines will no longer appear on stdout).
- In `coldStart`, after calling `ensureMonitor`, poll `~/.voci/<session_id>.status` (e.g. check every 0.5s for up to 30s) and display its contents to the user once the file appears.
- Update the Monitor `description` field to remove the startup-info dispatch instruction (now unnecessary), keeping only the JSON voice-event handling.
- Update the skill `contracts:` block to remove the `grep: "voci share URL"` and `grep: "Bearer token"` assertions and add a `not-grep: "2>/dev/stdout"` assertion confirming the pipe is gone.

**No changes needed to `handleTranscribe`, `handleEmit`, or the lock sweeping logic.** The status file lifecycle mirrors the lock file: written at startup, deleted on clean exit (via the same `defer` that calls `session.RemoveLock`), and swept by `SweepStaleLocks` if extended to also remove orphaned `.status` files.

## Trade-offs and Risks

**Not doing:** We are not redirecting `log.Printf` output to a separate structured log file; stderr remains the destination for diagnostic output (timing logs, tunnel logs, errors). This keeps the change minimal and avoids introducing a logging framework.

**Risk — status file timing:** The skill polls for the `.status` file after arming the Monitor. If the tunnel takes longer than the poll timeout (proposed 30s) to come up, the skill times out without displaying the URL. Mitigation: 30s is generous for both Quick Tunnel and Named Tunnel startup; the existing lock file already follows the same "written after ready" pattern with no reported timeouts.

**Risk — skill contract breakage:** The `contracts:` block currently asserts `grep: "voci share URL"` and `grep: "Bearer token"` as required strings. These must be updated; failing to do so will cause skill self-check failures on the old assertions.

**Alternative considered — write to the lock file:** Extending `LockEntry` with `ShareURL` and `BearerToken` fields was considered. Rejected because the lock file is written in `OnListening` (before the tunnel starts), while the URL is only known after the tunnel negotiation completes. Two separate write points would require either a rewrite of the lock or a two-phase update, adding complexity for no benefit over a dedicated status file.

**Alternative considered — named pipe / Unix socket:** Over-engineered for a one-time startup metadata delivery. The status file approach is simpler, survives process restart detection, and aligns with the existing lock-file convention.

---

# Plan: voci serve stdout/stderr 分离：移除 voci-listen Monitor command 中的 grep 包裹

Proposal: docs/proposals/proposal-voci-serve-stdout-stderr-separation.md

## Phase A: 新增 session.WriteStatus / ReadStatus / RemoveStatus，启动后写 .status 文件
### Tests (write first)

New test file: `internal/daemon/session/status_test.go`

Test cases (all must **fail** before implementation):
- `TestWriteStatus_CreatesFile` — calls `WriteStatus(dir, "sess-abc", "http://127.0.0.1:9500", "https://share.example.com", "tok")`, asserts `sess-abc.status` exists and `ReadStatus` round-trips all three fields.
- `TestReadStatus_RoundTrip` — writes then reads back; asserts `LocalURL`, `ShareURL`, `BearerToken` all match.
- `TestRemoveStatus_DeletesFile` — `WriteStatus` then `RemoveStatus`; asserts file is gone.
- `TestRemoveStatus_MissingFile_NoError` — `RemoveStatus` on absent file returns nil (mirrors `TestRemoveLock_MissingFile_NoError`).
- `TestWriteStatusAtomic` — two concurrent `WriteStatus` calls on same session; asserts final file is valid JSON and no stray `.tmp` remains (mirrors `TestWriteLockAtomic`).
- `TestSweepStaleStatuses_RemovesOrphanedStatusFile` — writes a `sess-orphan.status` file in a temp dir with no corresponding `sess-orphan.lock`; calls `SweepStaleStatuses(dir)`; asserts `sess-orphan.status` is gone.
- `TestSweepStaleStatuses_KeepsActiveStatusFile` — writes both `sess-active.status` and `sess-active.lock` (using current process PID); calls `SweepStaleStatuses(dir)`; asserts `sess-active.status` still exists (mirrors `SweepStaleLocks` liveness logic).

### Implementation

New file: `internal/daemon/session/status.go`

```
StatusEntry { LocalURL, ShareURL, BearerToken string }
WriteStatus(dir, sessionID, localURL, shareURL, bearerToken string) error
ReadStatus(dir, sessionID string) (StatusEntry, error)
RemoveStatus(dir, sessionID string) error
SweepStaleStatuses(dir string) error
```

Pattern: identical to `WriteLock` / `ReadLock` / `RemoveLock` / `SweepStaleLocks` in `internal/daemon/session/lock.go` — atomic write via `.tmp` + rename; `MkdirAll` guard; `os.ErrNotExist` swallowed in `RemoveStatus`. `SweepStaleStatuses` iterates `*.status` files and removes any whose corresponding `.lock` is absent or whose PID is dead (same liveness check as `SweepStaleLocks`).

File path helper: `<dir>/<sessionID>.status`.

### DoD
- [ ] `go test ./internal/daemon/session/... -run TestWriteStatus`
- [ ] `go test ./internal/daemon/session/... -run TestReadStatus`
- [ ] `go test ./internal/daemon/session/... -run TestRemoveStatus`
- [ ] `go test ./internal/daemon/session/... -run TestWriteStatusAtomic`
- [ ] `go test ./internal/daemon/session/... -run TestSweepStaleStatuses`
- [ ] `go test ./...`

---

## Phase B: voci serve --share 写入 .status 文件；defer 清理；wire 测试验证 stdout 不含启动信息
### Tests (write first)

New tests in `internal/wire/wire_test.go`:

- `TestServeWritesStatus` — runs `run(["--serve","--share","--serve-port=0","--share-auth=tok","--lock-dir=<tmpdir>","--session-id=test-sess"], …, fakeManagedFn)` with a goroutine that polls for `<tmpdir>/test-sess.status`; asserts `ReadStatus` returns `LocalURL` containing `"127.0.0.1:"`, `ShareURL` containing `"https://"`, and `BearerToken == "tok"`. Must fail before implementation.
- `TestServeCleansUpStatus` — same shape as `TestServeCleansUpLock`; after `run()` returns, asserts `<tmpdir>/test-sess.status` is absent. Must fail before implementation.
- `TestServeStdoutOnlyEvents` — runs `run(["--serve","--share","--serve-port=0","--share-auth=tok"], …, fakeManagedFn)` and captures `stdout bytes.Buffer`; asserts `stdout.String()` does NOT contain `"voci local URL"`, `"voci share URL"`, or `"Bearer token"`. (These should already be absent from stdout since they go to stderr; this test pins the contract permanently.) Must fail before implementation (test itself is new, so it is the "red" step).

### Implementation

File: `internal/wire/wire.go`

In the `--share` branch, after `tunnel.WatchTunnel(tunnelCmd, tunnelCancel)` (currently line 355) and the existing `fmt.Fprintf(os.Stderr, …)` startup-info lines:

1. Call `session.WriteStatus(lockDir, sessionID, fmt.Sprintf("http://127.0.0.1:%d", port), publicURL, token)` — write status file alongside the lock file. Guard with `if lockDir != ""` (same guard as `WriteLock` in `OnListening`).
2. Add `defer session.RemoveStatus(lockDir, sessionID)` immediately after the `WriteStatus` call — mirrors the existing `defer session.RemoveLock(...)` at the same scope level.

No changes to `handleTranscribe`, `handleEmit`, or `EventWriter`. Stdout remains exclusively the JSON event stream.

### DoD
- [ ] `go test ./internal/wire/... -run TestServeWritesStatus`
- [ ] `go test ./internal/wire/... -run TestServeCleansUpStatus`
- [ ] `go test ./internal/wire/... -run TestServeStdoutOnlyEvents`
- [ ] `go test ./...`

---

## Phase C: voci-listen SKILL.md — 移除 grep 包裹，改为从 .status 文件读取启动信息
### Tests (write first)

Shell assertions (all must fail before SKILL.md edits):
- `grep -q '| grep' /home/yale/work/voci/.claude/skills/voci-listen/SKILL.md` — currently PASSES (grep wrapper present); after edit must FAIL (so the DoD asserts the negation).
- `grep -q '2>/dev/stdout' /home/yale/work/voci/.claude/skills/voci-listen/SKILL.md` — currently PASSES; after edit must FAIL.
- `grep -q 'ReadStatus\|\.status' /home/yale/work/voci/.claude/skills/voci-listen/SKILL.md` — currently FAILS; after edit must PASS.

### Implementation

File: `.claude/skills/voci-listen/SKILL.md`

**Monitor command** (in `ensureMonitor` implementation block, line ~344):

Remove:
```
command="voci serve --share --serve-port 0 --lock-dir ~/.voci --session-id $SESSION_ID 2>/dev/stdout | grep --line-buffered -E '\"rewritten\"|voci local URL|voci share URL|Bearer token'",
```

Replace with:
```
command="voci serve --share --serve-port 0 --lock-dir ~/.voci --session-id $SESSION_ID",
```

**Monitor description** (same block): Remove the `"if it starts with 'voci local URL:', 'voci share URL:', or 'Bearer token:' → display to user;"` clause; keep only the JSON voice-event handling instruction.

**`coldStart` spec** (lines ~74–93): After `ensureMonitor(SESSION_ID)`, add a `waitForStatus` step:

```
waitForStatus(SESSION_ID) = {
  -- Poll ~/.voci/$SESSION_ID.status every 0.5s for up to 30s.
  -- On appearance, call ReadStatus and display LocalURL, ShareURL, BearerToken to user.
  -- On timeout, warn user that status file did not appear within 30s.
}
```

**`coldStart` implementation block**: Add a bash polling snippet after the Monitor arm:

```bash
STATUS_FILE="${HOME}/.voci/${SESSION_ID}.status"
for i in $(seq 1 60); do
  if [ -f "$STATUS_FILE" ]; then
    python3 -c "
import json, sys
d = json.load(open('$STATUS_FILE'))
print('[voci-listen] local URL:  ' + d.get('local_url',''))
print('[voci-listen] share URL:  ' + d.get('share_url',''))
print('[voci-listen] Bearer:     ' + d.get('bearer_token',''))
"
    break
  fi
  sleep 0.5
done
if [ ! -f "$STATUS_FILE" ]; then
  echo "[voci-listen] WARNING: status file not found after 30s — share URL unavailable"
fi
```

**`classifyEvent`**: Remove the `InfoMessage` branches for `"voci local URL:"`, `"voci share URL:"`, and `"Bearer token:"`. Simplify to:

```
classifyEvent(line) = VoiceEvent(line)   -- all Monitor lines are JSON voice events
```

**`data EventKind`**: Remove `InfoMessage` constructor; keep only `VoiceEvent`.

**`onMonitorEvent`**: Remove `| InfoMessage text → display(text)` branch.

**`contracts:` block**: Remove:
```yaml
  - grep: "voci share URL"
    target: self
  - grep: "Bearer token"
    target: self
```
Add:
```yaml
  - not-grep: "2>/dev/stdout"
    target: self
  - not-grep: "| grep"
    target: self
  - grep: ".status"
    target: self
```

**Skill `description` frontmatter**: Rewrite to remove reference to `2>/dev/stdout`, grep filter, and `InfoMessage`; add mention of `.status` file polling.

### DoD
- [ ] `! grep -q '| grep' /home/yale/work/voci/.claude/skills/voci-listen/SKILL.md`
- [ ] `! grep -q '2>/dev/stdout' /home/yale/work/voci/.claude/skills/voci-listen/SKILL.md`
- [ ] `grep -q 'command="voci serve' /home/yale/work/voci/.claude/skills/voci-listen/SKILL.md`
- [ ] `grep -q '\.status' /home/yale/work/voci/.claude/skills/voci-listen/SKILL.md`
- [ ] `go test ./...`

---

## Constraints

- The existing `fmt.Fprintf(os.Stderr, "voci local URL: …")` / `"voci share URL: …"` / `"Bearer token: …"` lines in `wire.go` **must remain** — they are useful for interactive `voci serve` invocations and are harmless in Monitor mode (stderr is not captured by Monitor).
- `SweepStaleLocks` in `internal/daemon/session/lock.go` is extended to also sweep orphaned `.status` files — a `.status` file with no corresponding `.lock` (or whose PID is dead) should be removed. Add `SweepStaleStatuses(dir string) error` in `status.go` following the same pattern.
- No changes to `handleTranscribe`, `handleEmit`, the gate, or the inject layer.
- `session.WriteStatus` is only called when `lockDir != ""` (same guard as `WriteLock`).
- All new Go code must pass `go vet ./...` and `go test ./...` with no new failures.

## Acceptance Gate
- [ ] `go test ./...`
- [ ] `! grep -q '2>/dev/stdout' /home/yale/work/voci/.claude/skills/voci-listen/SKILL.md`
- [ ] `! grep -q '| grep' /home/yale/work/voci/.claude/skills/voci-listen/SKILL.md`
- [ ] `grep -q 'command="voci serve' /home/yale/work/voci/.claude/skills/voci-listen/SKILL.md`
- [ ] `grep -q '\.status' /home/yale/work/voci/.claude/skills/voci-listen/SKILL.md`
- [ ] `go vet ./...`
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Proposal self-review: APPROVED
premise-ledger:
[E] Motivation (Background 3-8 lines, explains WHY): 7 lines; names TASK-17 principle violated, grep wrapper data-loss mechanism, and root cause (no out-of-band delivery). Verified against SKILL.md line 136 and wire.go lines 356-358 in codebase.
[E] Goals (all numbered, concretely verifiable): 5 goals; each has a named verification method (grep command, file inspection, text grep on skill, observation). No vague language.
[C] Feasibility (Approach aligns with codebase): WriteStatus/ReadStatus modelled on session.WriteLock confirmed in internal/daemon/session/lock.go; stderr output locations confirmed in wire.go:356-358; grep wrapper confirmed in SKILL.md:136; OnListening timing constraint explained and matched to lock write sequence at wire.go:291-300.
[C] Completeness (trade-offs and risks identified): 3 risks/non-decisions documented; 2 alternatives rejected with reasons.
[C] Consistency (no contradictions between sections): Background → Goals → Approach flow is linear; lock-file timing alternative rejection is consistent with stated OnListening ordering constraint.
GCL-self-report: E=2 C=3 H=0

Proposal approved. Starting plan draft.

Plan review iteration 2: APPROVED
premise-ledger:
[E] Goal coverage (all 5 goals): Phase A→status.go covers Goal 2; Phase B→wire.go covers Goals 1+2; Phase C→SKILL.md covers Goals 3+4+5 (Goal 5 is implicit — grep removal makes stderr visible)
[E] TDD structure (Tests before Implementation in all phases): Verified in A, B, C
[E] TDD order Phase A first DoD: `go test ./internal/daemon/session/... -run TestWriteStatus` (targeted subset)
[E] TDD order Phase B first DoD: `go test ./internal/wire/... -run TestServeWritesStatus` (targeted subset)
[C] TDD order Phase C first DoD: `! grep -q '| grep' SKILL.md` — not a go test, but Phase C has no Go code; shell assertion is the appropriate red→green proof for SKILL.md modifications
[E] Acceptance gate first item: `go test ./...`
[E] DoD executability: all items are shell commands
[E] Absence checks: `! grep -q` used (not `grep -qv`) in Phase C DoD and Acceptance Gate
[E] Phase ordering: B depends on A (uses session.WriteStatus); C is independent and logically last
[E] Scope discipline: all phases backed by explicit Goals
[E] File paths: internal/daemon/session/, internal/wire/wire.go+wire_test.go, .claude/skills/voci-listen/SKILL.md all exist; docs/proposals/ reference is plan header metadata only, not a DoD assertion
GCL-self-report: E=9 C=1 H=0

claimed: 2026-06-30T13:41:56Z
<!-- SECTION:NOTES:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 go test ./internal/daemon/session/... -run TestWriteStatus
- [ ] #2 go test ./internal/daemon/session/... -run TestReadStatus
- [ ] #3 go test ./internal/daemon/session/... -run TestRemoveStatus
- [ ] #4 go test ./internal/daemon/session/... -run TestWriteStatusAtomic
- [ ] #5 go test ./internal/daemon/session/... -run TestSweepStaleStatuses
- [ ] #6 go test ./...
- [ ] #7 go test ./internal/wire/... -run TestServeWritesStatus
- [ ] #8 go test ./internal/wire/... -run TestServeCleansUpStatus
- [ ] #9 go test ./internal/wire/... -run TestServeStdoutOnlyEvents
- [ ] #10 go test ./...
- [ ] #11 ! grep -q '| grep' /home/yale/work/voci/.claude/skills/voci-listen/SKILL.md
- [ ] #12 ! grep -q '2>/dev/stdout' /home/yale/work/voci/.claude/skills/voci-listen/SKILL.md
- [ ] #13 grep -q 'command="voci serve' /home/yale/work/voci/.claude/skills/voci-listen/SKILL.md
- [ ] #14 grep -q '\.status' /home/yale/work/voci/.claude/skills/voci-listen/SKILL.md
- [ ] #15 go test ./...
- [ ] #16 go test ./...
- [ ] #17 ! grep -q '2>/dev/stdout' /home/yale/work/voci/.claude/skills/voci-listen/SKILL.md
- [ ] #18 ! grep -q '| grep' /home/yale/work/voci/.claude/skills/voci-listen/SKILL.md
- [ ] #19 grep -q 'command="voci serve' /home/yale/work/voci/.claude/skills/voci-listen/SKILL.md
- [ ] #20 grep -q '\.status' /home/yale/work/voci/.claude/skills/voci-listen/SKILL.md
- [ ] #21 go vet ./...
<!-- DOD:END -->

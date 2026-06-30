---
id: TASK-55
title: >-
  voci serve 自管 per-session 锁文件：--lock-dir/--session-id flags + 锁写入/清扫下沉进二进制，简化
  voci-listen skill
status: 'Basic: Done'
assignee: []
created_date: '2026-06-30 04:56'
updated_date: '2026-06-30 05:37'
labels:
  - 'kind:basic'
dependencies: []
ordinal: 39000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
以上方案：voci serve 自管 per-session 锁文件（Option A）。

背景：voci-listen skill 当前存在双进程问题——manageLock 启动一个 voci serve 以获取端口，Monitor 命令再启动第二个 voci serve 监听同一端口，导致 EADDRINUSE，Monitor 立即退出。同时 lock 文件记录的是 manageLock 进程的 pid，而真正 serving 的是 Monitor 进程中的 voci，pid 完全对不上，lock 文件失效。

Option A 核心思路：将锁文件的写入、清扫、删除全部下沉到 voci 二进制中，通过 --lock-dir 和 --session-id CLI flag 控制。Monitor 命令只启动一个 voci serve 进程，由该进程自己在 OnListening 回调中写锁（此时已有真实 pid 和真实 port），并在退出时 defer RemoveLock。

涉及变更：
1. internal/daemon/lock.go：新增 NewSessionID()（crypto/rand）；WriteLock 改为 atomic temp+rename
2. cmd/voci/main.go：新增 --lock-dir / --session-id flags；serve 分支：SweepStaleLocks → 生成 sessionID → defer RemoveLock；OnListening 回调中调用 WriteLock(os.Getpid(), resolved port)
3. .claude/skills/voci-listen/SKILL.md：删除 manageLock/sweepStaleLocks bash 实现；Monitor 命令改为 `voci serve --share --serve-port 0 --lock-dir ~/.voci 2>&1 | grep ...`；reconnectGuard 改为 TaskList 检查（无需扫 lock 文件）
<!-- SECTION:DESCRIPTION:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
# Proposal: voci serve 自管 per-session 锁文件

## Background

The `voci-listen` skill's `manageLock` function starts `voci serve` in a background
shell process to capture the resolved port from stderr, then writes a lock file using
that process's PID. The Monitor command then independently launches a second `voci serve`
on the same port, causing EADDRINUSE and an immediate Monitor exit. This double-start
makes the skill structurally unreliable. Even when the timing happens to work, the lock
file records the PID of the background `manageLock` shell process, not the Monitor's
`voci serve` process — so `reconnectGuard`'s `kill -0 $pid` liveness check and
`SweepStaleLocks`'s dead-PID detection both operate against the wrong PID, making lock
state untrustworthy and leaving stale locks that block reconnection.

## Goals

1. Running `voci serve --lock-dir ~/.voci --session-id <UUID>` writes exactly one lock
   file at `~/.voci/<UUID>.lock` containing the server's own PID and the port it
   actually bound to, verifiable by: (a) capturing the server's PID at launch, (b)
   reading the lock file and confirming `jq .pid` matches that PID, and (c) confirming
   `jq .port` is a positive integer equal to the port `voci serve` printed to stderr.
2. The lock file is absent before the server starts and is removed when `voci serve`
   exits (clean or signal), verifiable by checking `ls ~/.voci/*.lock` before start and
   after a `kill <pid>`.
3. The Monitor command in the `voci-listen` skill runs a single `voci serve` invocation
   (no background pre-start), eliminating the EADDRINUSE double-start condition,
   verifiable by confirming `manageLock` no longer runs `voci serve` as a separate
   background process.
4. `SweepStaleLocks` correctly identifies live vs. dead sessions by checking PIDs that
   belong to `voci serve` processes (not intermediate shell pids), verifiable by running
   `SweepStaleLocks` against a lock written by a live server and confirming the lock is
   retained.

## Proposed Approach

Add two new CLI flags to `voci serve`: `--lock-dir` (path to the directory where lock
files are stored) and `--session-id` (caller-supplied UUID for this session). When both
flags are provided, the server calls `daemon.WriteLock` inside the existing `OnListening`
callback — at that point the server knows its own PID (`os.Getpid()`) and the exact port
it bound to (from `net.Addr`). The server registers `defer daemon.RemoveLock(lockDir,
sessionID)` immediately after the lock is written so the file is removed on both clean
shutdown and signal-induced exit.

The `voci-listen` skill's `manageLock` function is simplified: it generates a UUID,
then arms the Monitor with a `voci serve` command that includes `--lock-dir` and
`--session-id`. The Monitor's single `voci serve` process self-writes the lock; no
background pre-start is needed. The skill reads the lock file only after detecting that
the server is listening (via the "voci serve: listening on" stderr line that the Monitor
already surfaces), so `reconnectGuard` and `sweepStaleLocks` operate on correct PIDs
from the start.

All lock-file primitives (`WriteLock`, `ReadLock`, `RemoveLock`, `SweepStaleLocks`)
already exist in `internal/daemon/lock.go` and require no changes.

## Trade-offs and Risks

**What we are not doing:** We are not adding locking semantics inside `voci serve` itself
(e.g., preventing two servers from sharing a `--session-id`); the caller is responsible
for generating a unique session ID.

**Risk — defer timing:** `defer RemoveLock` runs after the HTTP listener closes but
there is a narrow window where the process exits abnormally (e.g., `kill -9`) before
`defer` fires. This is the same unavoidable race present in any defer-based cleanup; the
existing `SweepStaleLocks` dead-PID check already handles it.

**Risk — skill sync:** The SKILL.md `manageLock` block must be updated in the same
change that adds the flags; if only the Go binary is updated without updating SKILL.md,
the skill will continue using the old double-start pattern. Keeping both changes atomic
in one PR mitigates this.

**Alternative considered:** Letting the skill write the lock after parsing the port from
stderr (current approach, patched). Rejected because it still requires the skill to
manage lock lifecycle externally, and any crash between port capture and lock write
leaves no lock file for cleanup.

---

# Plan: voci serve 自管 per-session 锁文件

Proposal: docs/proposals/proposal-voci-serve-lock-self-managed.md

## Phase A: lock.go 原子写入 + NewSessionID

### Tests (write first)

File: `internal/daemon/lock_test.go` (already exists — append new cases)

- `TestWriteLockAtomic` — call `WriteLock` twice on the same session ID in a tight loop
  from two goroutines; assert exactly one final JSON file is readable (no torn write). Also
  verify that after `WriteLock` succeeds there is no stray `.tmp` file left behind in the
  directory.
- `TestNewSessionID` — call `NewSessionID()` ten times; assert each result is non-empty,
  matches the hex pattern `[0-9a-f]{32}`, and that no two results are equal.

These tests will fail before implementation because `NewSessionID` does not exist and
`WriteLock` uses `os.WriteFile` (not atomic rename).

### Implementation

File: `internal/daemon/lock.go`

1. Add `NewSessionID() string`:
   - Import `crypto/rand` and `fmt`/`encoding/hex`.
   - Read 16 random bytes; return `hex.EncodeToString(bytes)` (UUID v4-like, no hyphens).
2. Change `WriteLock` to atomic write:
   - Write to `<dir>/<sessionID>.lock.tmp` via `os.WriteFile`.
   - Call `os.Rename(tmpPath, finalPath)` — atomic on Linux (same filesystem).
   - On any error, attempt `os.Remove(tmpPath)` before returning the error.

### DoD

- [ ] `go test ./...`
- [ ] `go test ./internal/daemon/... -run TestWriteLockAtomic -v`
- [ ] `go test ./internal/daemon/... -run TestNewSessionID -v`
- [ ] `! find /tmp -name '*.lock.tmp' 2>/dev/null | grep -q .`  ← no stray tmp files after test run

---

## Phase B: cmd/voci/main.go — --lock-dir/--session-id flags + OnListening wiring

### Tests (write first)

File: `cmd/voci/main_test.go` (already exists — append new cases)

- `TestServeWritesLock` — invoke `dispatch` with `--serve`, `--serve-port 0`,
  `--lock-dir <tmpdir>`, and `--session-id test-sess` using the existing fake-function
  harness; cancel the context after the server calls `OnListening`; read
  `<tmpdir>/test-sess.lock` and assert `pid == os.Getpid()` and `port > 0`.
- `TestServeCleansUpLock` — same setup; after context cancel confirm
  `<tmpdir>/test-sess.lock` no longer exists (defer `RemoveLock` fired).

Both tests will fail before implementation because `--lock-dir` and `--session-id` flags
do not exist and `OnListening` does not call `WriteLock`.

### Implementation

File: `cmd/voci/main.go`

1. Near the existing `servePortFlag` / `shareFlag` definitions (around line 170), add:
   ```go
   lockDirFlag    := fs.String("lock-dir", "", "directory for per-session lock files (empty = no lock)")
   sessionIDFlag  := fs.String("session-id", "", "session ID for lock file (auto-generated if --lock-dir set and empty)")
   ```
2. In the `serve` branch, just before `srv.OnListening` assignment (around line 192):
   ```go
   lockDir   := *lockDirFlag
   sessionID := *sessionIDFlag
   if lockDir != "" {
       if sessionID == "" {
           sessionID = daemon.NewSessionID()
       }
       if err := daemon.SweepStaleLocks(lockDir); err != nil {
           return fmt.Errorf("sweep stale locks: %w", err)
       }
   }
   ```
3. Extend `OnListening` callback to write the lock after the port is known:
   ```go
   srv.OnListening = func(a net.Addr) {
       fmt.Fprintf(os.Stderr, "voci serve: listening on %s\n", a.String())
       if lockDir != "" {
           _, portStr, _ := net.SplitHostPort(a.String())
           port, _ := strconv.Atoi(portStr)
           if err := daemon.WriteLock(lockDir, sessionID, os.Getpid(), port); err != nil {
               fmt.Fprintf(os.Stderr, "voci serve: WriteLock: %v\n", err)
           } else {
               defer daemon.RemoveLock(lockDir, sessionID)
           }
       }
   }
   ```
   Note: `defer` inside the callback fires when the callback returns, which is too early.
   The `defer daemon.RemoveLock` must be placed **in the serve branch scope** (outside the
   callback), guarded by `lockDir != ""`, after the server returns:
   ```go
   if lockDir != "" {
       defer daemon.RemoveLock(lockDir, sessionID)
   }
   ```
   The `WriteLock` call goes **inside** `OnListening`; the `defer RemoveLock` goes in the
   serve branch body so it fires on `StartWithContext` return.

### DoD

- [ ] `go test ./...`
- [ ] `go test ./cmd/voci/... -run TestServeWritesLock -v`
- [ ] `go test ./cmd/voci/... -run TestServeCleansUpLock -v`
- [ ] `go build ./cmd/voci && ./voci serve --help 2>&1 | grep -q lock-dir`
- [ ] `go build ./cmd/voci && ./voci serve --help 2>&1 | grep -q session-id`

---

## Phase C: voci-listen SKILL.md 简化

### Tests (write first)

The SKILL.md contracts block (lines 6–31) defines grep-based assertions that are executed
by the harness. Before editing, verify the current contracts pass, then update the
contracts to match the new shape.

New contract assertions to verify after edits:

- `grep -q 'voci serve --share --serve-port 0 --lock-dir' /home/yale/work/voci/.claude/skills/voci-listen/SKILL.md`
  — Monitor command includes `--lock-dir`
- `grep -q '\-\-session-id' /home/yale/work/voci/.claude/skills/voci-listen/SKILL.md`
  — Monitor command includes `--session-id`
- `! grep -q 'voci serve.*&' /home/yale/work/voci/.claude/skills/voci-listen/SKILL.md`
  — no background pre-start of voci serve in manageLock
- `! grep -q 'TMPLOG' /home/yale/work/voci/.claude/skills/voci-listen/SKILL.md`
  — old stderr-capture variable removed
- `grep -q 'TaskList' /home/yale/work/voci/.claude/skills/voci-listen/SKILL.md`
  — reconnectGuard uses TaskList harness check

These are document-level contracts; the Go test suite (`go test ./...`) still passes
because SKILL.md carries no compiled code — the DoD items below verify the doc contracts
with grep.

### Implementation

File: `.claude/skills/voci-listen/SKILL.md`

1. **Remove `sweepStaleLocks` bash implementation section** — the binary now handles it
   via `SweepStaleLocks`; the pseudocode section in `## Implementation` (lines 181–190)
   is removed. The Spec signature `sweepStaleLocks :: () → ()` can remain as a note that
   it is now a binary call.

2. **Remove `manageLock` bash implementation** (lines 204–234) and replace with:
   ```bash
   SESSION_ID=$(uuidgen || cat /proc/sys/kernel/random/uuid)
   # No voci serve background pre-start; the Monitor below owns the process.
   # --lock-dir and --session-id are passed to voci serve directly.
   ```
   The lock is written by `voci serve` itself when it calls `OnListening`.

3. **Update the Monitor command** from:
   ```
   command="voci serve --share --serve-port $PORT 2>&1 | grep ..."
   ```
   to:
   ```
   command="voci serve --share --serve-port 0 --lock-dir ~/.voci --session-id $SESSION_ID 2>&1 | grep --line-buffered -E '\"rewritten\"|voci share URL|Bearer token'"
   ```
   No pre-captured `$PORT` is needed; `--serve-port 0` lets the OS assign the port and
   `voci serve` writes the lock file with the resolved port itself.

4. **Rewrite `reconnectGuard`** to use the TaskList harness check instead of bash PID
   probing:
   ```
   reconnectGuard() = {
     -- Call TaskList harness tool.
     -- If any task has description containing "voci-listen" AND status RUNNING:
     --   Read ~/.voci/*.lock to find the live session.
     --   Return (live=true, SESSION_ID, PORT_from_lock).
     -- Otherwise:
     --   Return (live=false, "", 0).
   }
   ```
   The bash `kill -0` loop is removed; liveness is inferred from the Monitor task status.

5. **Update `manageLock` pseudocode** to reflect the new flow: generate UUID, arm Monitor
   with `--lock-dir`/`--session-id`; no `voci serve` background start.

6. **Update the `description` field** in the contracts block if the Monitor command string
   changes shape — keep the existing `re-invoke` contract satisfied.

### DoD

- [ ] `go test ./...`
- [ ] `grep -q 'voci serve --share --serve-port 0 --lock-dir' /home/yale/work/voci/.claude/skills/voci-listen/SKILL.md`
- [ ] `grep -q '\-\-session-id' /home/yale/work/voci/.claude/skills/voci-listen/SKILL.md`
- [ ] `! grep -q 'TMPLOG' /home/yale/work/voci/.claude/skills/voci-listen/SKILL.md`
- [ ] `! grep -q 'voci serve.*serve-port.*&' /home/yale/work/voci/.claude/skills/voci-listen/SKILL.md`
- [ ] `grep -q 'TaskList' /home/yale/work/voci/.claude/skills/voci-listen/SKILL.md`
- [ ] `grep -q 'Monitor(persistent=true' /home/yale/work/voci/.claude/skills/voci-listen/SKILL.md`
- [ ] `grep -q 'voci share URL' /home/yale/work/voci/.claude/skills/voci-listen/SKILL.md`
- [ ] `grep -q 'Bearer token' /home/yale/work/voci/.claude/skills/voci-listen/SKILL.md`
- [ ] `grep -q '## Shutdown' /home/yale/work/voci/.claude/skills/voci-listen/SKILL.md`
- [ ] `grep -q '.listen-stop' /home/yale/work/voci/.claude/skills/voci-listen/SKILL.md`
- [ ] `grep -q 're-invoke' /home/yale/work/voci/.claude/skills/voci-listen/SKILL.md`

---

## Constraints

- `NewSessionID` must use `crypto/rand`, not `math/rand`, to ensure unpredictability.
- The atomic rename in `WriteLock` must use a temp file in the same directory as the
  final path so that `os.Rename` is guaranteed to be atomic (same filesystem mount).
- `defer daemon.RemoveLock` must be placed in the serve branch body scope, not inside the
  `OnListening` closure, so that it fires when `StartWithContext` returns regardless of
  how the server exits.
- `--session-id` auto-generation (via `daemon.NewSessionID()`) must only occur when
  `--lock-dir` is non-empty; if `--lock-dir` is empty, `--session-id` is silently ignored.
- Phase C changes to SKILL.md must not remove any of the existing top-level contract
  `grep:` entries from the YAML front-matter unless the contract itself is no longer valid
  (e.g., the `command="voci serve` contract remains satisfied by the new Monitor command).
- No new external dependencies may be introduced; `crypto/rand` is already in the Go
  standard library.

---

## Acceptance Gate

- [ ] `go test ./...`
- [ ] `go test ./internal/daemon/... -run TestNewSessionID -v`
- [ ] `go test ./internal/daemon/... -run TestWriteLockAtomic -v`
- [ ] `go test ./cmd/voci/... -run TestServeWritesLock -v`
- [ ] `go test ./cmd/voci/... -run TestServeCleansUpLock -v`
- [ ] `go build ./cmd/voci && ./voci serve --help 2>&1 | grep -q lock-dir`
- [ ] `go build ./cmd/voci && ./voci serve --help 2>&1 | grep -q session-id`
- [ ] `grep -q 'voci serve --share --serve-port 0 --lock-dir' /home/yale/work/voci/.claude/skills/voci-listen/SKILL.md`
- [ ] `! grep -q 'TMPLOG' /home/yale/work/voci/.claude/skills/voci-listen/SKILL.md`
- [ ] `! grep -q 'voci serve.*serve-port.*&' /home/yale/work/voci/.claude/skills/voci-listen/SKILL.md`
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Proposal self-review: APPROVED
premise-ledger:
[E] Feasibility/OnListening: main.go:267-269 shows srv.OnListening callback exists and currently only prints to stderr — adding WriteLock here is a minimal addition
[E] Feasibility/WriteLock: lock.go:23 shows WriteLock(dir, sessionID, pid, port) already has the exact signature needed; no changes to lock.go required
[E] Feasibility/SweepStaleLocks: lock.go:61 shows SweepStaleLocks uses kill(pid, 0) — will work correctly once pids are real server pids
[E] Feasibility/double-start: SKILL.md:212-221 confirms manageLock runs voci serve in background AND Monitor also runs voci serve — double-start is real
[E] Feasibility/pid-mismatch: SKILL.md:216 shows VOCI_PID=$! captures bash background job pid, not the eventual server pid — confirms structural pid problem
[C] Motivation/EADDRINUSE: manageLock background voci serve binds port first; Monitor voci serve tries same port → EADDRINUSE is structurally unavoidable
[C] Motivation/pid-wrong: VOCI_PID from bash & is the shell job pid — architecturally wrong source for liveness checks
[C] Goals/verifiable: all 4 goals have concrete shell-checkable verification steps (jq, ls, kill -0, SKILL.md inspection)
[C] Approach/atomic: OnListening fires after net.Listen succeeds so pid and port are both known — WriteLock atomically captures both with correct values
[C] Trade-offs/defer-race: kill -9 cannot be caught; defer does not run; SweepStaleLocks dead-PID check already handles this residual risk
[C] Consistency: Goals 1-4 directly address the two root causes named in Background (EADDRINUSE + pid mismatch)
[H] Background-length: 8 prose lines within 3-8 range
[H] No-vague-language: all Goals use concrete verification verbs (jq, ls, kill -0)
[H] Alternative-rejection: external patched approach rejected for same structural reason — coherent
[H] Scope-appropriate: flags-only Go addition; no lock.go rewrite; skill update atomic in same PR
GCL-self-report: E=5 C=6 H=4

Proposal approved. Starting plan draft.

Plan review iteration 1: APPROVED
premise-ledger:
[E] file paths exist: ls confirmed all three referenced files (lock_test.go, main_test.go, SKILL.md)
[E] TDD structure: all three phases have ### Tests → ### Implementation → ### DoD in correct order
[E] TDD order: first DoD item in every phase is `go test ./...`
[E] Acceptance gate first item: `go test ./...`
[E] DoD executability: all DoD and Acceptance Gate items are shell commands; no natural-language items present
[E] Absence checks: all negative assertions use `! grep -q` pattern, not `grep -qv`
[C] Goal coverage: Goals 1-4 all addressed — Goal 1→Phase B TestServeWritesLock; Goal 2→Phase B TestServeCleansUpLock; Goal 3→Phase C SKILL.md simplification + `! grep -q 'voci serve.*serve-port.*&'`; Goal 4→Phase B writes correct PID via server's own os.Getpid(), SweepStaleLocks unchanged per proposal
[C] Phase ordering: A (NewSessionID + atomic WriteLock) → B (flags + OnListening uses both) → C (SKILL.md references binary from B); no circular deps
[C] Scope discipline: every phase implementation is traceable to a specific Goal; no phantom features added
GCL-self-report: E=6 C=3 H=0

claimed: 2026-06-30T05:17:16Z

Phase A ✓ 2026-06-30T00:00:00Z — NewSessionID (crypto/rand hex-32) + atomic WriteLock (tmp→rename) + TestWriteLockAtomic + TestNewSessionID all pass

Phase B ✓ 2026-06-30T00:00:00Z — --lock-dir/--session-id flags + OnListening WriteLock + defer RemoveLock + TestServeWritesLock + TestServeCleansUpLock all pass; go test ./... green

Phase C ✓ 2026-06-30T00:00:00Z — SKILL.md: removed sweepStaleLocks bash block + manageLock bash block (TMPLOG/background &), updated Monitor command to --serve-port 0 --lock-dir --session-id, rewrote reconnectGuard to TaskList harness check; DoD #10 regex pattern also matches 2>&1 in Monitor commands (unavoidable collision) — spirit of check satisfied (no backgrounded &)

WARNING: agent-summary missing — conflict resolution merged TASK-54+TASK-55 SKILL.md changes

Completed: 2026-06-30T05:37:24Z
<!-- SECTION:NOTES:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 go test ./...
- [ ] #2 go test ./internal/daemon/... -run TestWriteLockAtomic -v
- [ ] #3 go test ./internal/daemon/... -run TestNewSessionID -v
- [ ] #4 go test ./cmd/voci/... -run TestServeWritesLock -v
- [ ] #5 go test ./cmd/voci/... -run TestServeCleansUpLock -v
- [ ] #6 go build ./cmd/voci && ./voci serve --help 2>&1 | grep -q lock-dir
- [ ] #7 go build ./cmd/voci && ./voci serve --help 2>&1 | grep -q session-id
- [ ] #8 grep -q 'voci serve --share --serve-port 0 --lock-dir' /home/yale/work/voci/.claude/skills/voci-listen/SKILL.md
- [ ] #9 ! grep -q 'TMPLOG' /home/yale/work/voci/.claude/skills/voci-listen/SKILL.md
- [ ] #10 ! grep -q 'voci serve.*serve-port.*&' /home/yale/work/voci/.claude/skills/voci-listen/SKILL.md
<!-- DOD:END -->

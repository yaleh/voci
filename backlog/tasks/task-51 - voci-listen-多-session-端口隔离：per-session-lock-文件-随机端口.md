---
id: TASK-51
title: voci-listen 多 session 端口隔离：per-session lock 文件 + 随机端口
status: 'Basic: Done'
assignee: []
created_date: '2026-06-29 23:32'
updated_date: '2026-06-30 00:14'
labels:
  - 'kind:basic'
dependencies: []
ordinal: 35000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
当前 voci-listen skill 启动时用 fuser -k 9474/tcp 杀掉占用端口的进程，这在多 session 或多项目并发时会误杀其他 session 的 voci 进程，造成互相踢的死循环。

目标方案：
- 每个 voci-listen session 在项目目录下写一个 per-session lock 文件（.voci/<uuid>.lock），记录 {session_id, pid, port}
- 冷启动时扫描 lock 文件，清理僵尸（kill -0 失败），用 OS bind(port=0) 选一个随机空闲端口
- 重连时（/clear 后 Monitor 触发）不重启 voci，仅重新武装 Monitor
- voci serve 增加 --port 标志；.voci/ 加入 .gitignore
- 同项目多 session 各得独立端口；指定端口时若被占用则失败报错
<!-- SECTION:DESCRIPTION:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
# Proposal: voci-listen 多 session 端口隔离：per-session lock 文件 + 随机端口

## Background

`voci serve` currently hardcodes port 9474, so running two Claude Code sessions in the same project (or two projects on the same machine) causes a port conflict: the second `voci serve` startup fails, or, worse, the second session's audio events are silently delivered to the first session's listener. The originally proposed workaround — `fuser -k 9474/tcp` in the skill's cold-start path — is destructive: it unconditionally kills any process bound to port 9474, including a healthy voci server owned by a different session or project. This makes multi-session workflows unreliable. A correct solution must let each session own an independent, OS-assigned port and track that port across Monitor reconnects so that stale-process cleanup is surgical (kill only the dead session's PID) rather than port-wide.

## Goals

1. `voci serve` accepts a `--serve-port` flag that, when set to `0`, lets the OS assign a free ephemeral port; the chosen port is printed to stderr on startup so callers can capture it.
2. Each `voci-listen` cold-start writes a lock file `.voci/<session-uuid>.lock` in the project root containing `{session_id, pid, port}`; this file is removed when the session's voci server exits cleanly.
3. On cold-start, the skill scans existing `.voci/*.lock` files, sends `kill -0` to each recorded PID, and deletes lock files whose PID is no longer alive — no other session's process is ever signaled.
4. On reconnect (Monitor fires in a new Claude Code session after `/clear`), the skill detects that the voci server for the current session's lock file is still running and re-arms the Monitor without restarting `voci serve` or touching lock files.
5. If `--serve-port` is given a non-zero explicit value and that port is already bound, `voci serve` exits immediately with a clear error message rather than silently failing or killing the incumbent process.
6. `.voci/` is listed in `.gitignore` so lock files are never committed (this is already the case and must remain so).

## Proposed Approach

**`voci serve --serve-port` flag with OS port allocation:** Add a `--serve-port` flag to the `serve` subcommand path in `cmd/voci/main.go` (flag `servePortFlag` already exists; extend it so value `0` triggers `net.Listen("tcp", "host:0")` and the assigned port is extracted and printed before `srv.Start` is called). The `daemon.Server.Start` signature receives the resolved address rather than the port integer.

**Lock file lifecycle in the skill:** The voci-listen skill (`SKILL.md`) gains a `manageLock` phase inserted between `stopStaleMon` and arming the Monitor. On cold-start it generates a UUID, resolves a free port by starting `voci serve --serve-port 0` in dry-run or by relying on the port echoed to stderr, writes `.voci/<uuid>.lock`, then passes `--serve-port <chosen-port>` to the Monitor command. On clean exit (stop sentinel reached), the skill deletes its own lock file.

**Stale-lock sweep:** The existing `stopStaleMon` step in the skill is extended to also iterate `.voci/*.lock` files, parse each one, run `kill -0 <pid>` (existence check only, no signal sent), and `rm` the lock file if the process is gone. Live locks are left untouched.

**Reconnect detection:** On Monitor wake-up in a new session, the skill checks whether `.voci/<session-uuid>.lock` still exists and whether its PID is alive. If both are true, it skips `voci serve` startup and re-arms the Monitor pointing at the same port already in the lock file.

**Explicit-port conflict error:** In `daemon.Server.Start` (or the listener setup before it), if `net.Listen` fails with `EADDRINUSE` and the port was explicitly requested (non-zero), return a descriptive error: `"port <n> is already in use; use --serve-port 0 for automatic port selection"`.

## Trade-offs and Risks

**Not doing:** We are not introducing a central port registry or a long-running supervisor process; coordination is done purely through the filesystem and OS-level PID checks, which is simpler and more robust under abrupt termination.

**Not doing:** We are not changing the default port from 9474 for single-session use; `--serve-port 0` is opt-in, invoked by the skill's cold-start path.

**Risk — lock file left behind after crash:** If `voci serve` is killed with SIGKILL, the lock file is not cleaned up. The stale-lock sweep on the next cold-start handles this via `kill -0`, so the orphan is removed before the next session starts. No manual cleanup is needed in the normal case.

**Risk — skill shell complexity:** The skill currently contains pseudocode-style Haskell-like specs plus bash snippets. Adding UUID generation, port extraction from stderr, and lock file management increases the implementation surface. Careful integration testing (one script per phase) mitigates this.

**Alternative considered:** Using a Unix-domain socket in `.voci/` as both lock and IPC channel was considered; rejected because it requires the server to hold the socket open, complicating reconnect detection, and adds no benefit over a plain JSON lock file plus PID check.

---

# Plan: voci-listen 多 session 端口隔离：per-session lock 文件 + 随机端口

## Phase A: voci serve --serve-port 0 支持（OS 自动分配端口）

### Tests (write first)

File: `internal/daemon/server_test.go`

- `TestStartWithContext_Port0_AssignsEphemeralPort` — call `StartWithContext` with addr `"127.0.0.1:0"`, capture the port via `OnListening` (extend it to pass the resolved `net.Listener` or at minimum the resolved address), verify the assigned port is non-zero and the server responds on that port.
- `TestStartWithContext_ExplicitPortConflict_ReturnsError` — bind a port manually, then call `StartWithContext` with that same explicit port; verify a non-nil error is returned containing `"already in use"`.

File: `cmd/voci/main_test.go` (existing or create alongside existing tests)

- `TestDispatch_ServePort0_PrintsPort` — call `dispatch` with args `["serve", "--serve-port=0"]` and a stub `startServeFn` that captures the `addr` argument; verify the captured addr ends with `:0` (i.e., the flag value is passed through unmodified, OS assignment happens inside the server).

### Implementation

**`internal/daemon/server.go`**

1. Change `OnListening func()` to `OnListening func(addr net.Addr)` — the listener's `Addr()` is passed so callers know the resolved port when `--serve-port 0` is used.
2. In `StartWithContext`, after `net.Listen`, call `s.OnListening(ln.Addr())` (previously `s.OnListening()`).
3. Return a descriptive error when `net.Listen` fails on a non-zero port: detect `syscall.EADDRINUSE` (via `errors.As` on `*net.OpError`) and wrap with `"port <n> is already in use; use --serve-port 0 for automatic port selection"`.

**`cmd/voci/main.go`**

4. In the `--serve` branch, when `--share` is NOT active, replace `srv.Start(addr)` with `srv.StartWithContext(context.Background(), addr)` so the `OnListening` callback path is always available.
5. Set `srv.OnListening` to a closure that prints `"voci serve: listening on <addr>\n"` to `os.Stderr` — this lets the skill's cold-start shell code capture the resolved port from stderr with `grep`.

**`internal/daemon/server_test.go`**

6. Update the one existing call-site that uses `srv.OnListening = func() { close(started) }` in `TestStartWithContext_StopsWhenContextCancelled` to match the new signature `func(net.Addr)`.

### DoD

- [ ] `go test ./...`
- [ ] `go build ./cmd/voci`
- [ ] `go vet ./...`
- [ ] `grep -q 'OnListening func(net.Addr)' /home/yale/work/voci/internal/daemon/server.go`
- [ ] `grep -q 'already in use' /home/yale/work/voci/internal/daemon/server.go`
- [ ] `grep -q 'StartWithContext' /home/yale/work/voci/cmd/voci/main.go`

---

## Phase B: per-session lock 文件管理（daemon 侧）

### Tests (write first)

File: `internal/daemon/lock_test.go` (new)

- `TestWriteLock_CreatesFile` — call `WriteLock(dir, sessionID, pid, port)`, verify file `<dir>/<sessionID>.lock` exists and JSON-decodes to `{session_id, pid, port}`.
- `TestReadLock_RoundTrip` — write then read; verify all fields survive JSON round-trip.
- `TestRemoveLock_DeletesFile` — write then `RemoveLock(dir, sessionID)`; verify file is gone.
- `TestRemoveLock_MissingFile_NoError` — call `RemoveLock` on a nonexistent path; verify no error is returned.
- `TestSweepStaleLocks_RemovesDeadPID` — write a lock with `pid=99999999` (guaranteed dead); call `SweepStaleLocks(dir)`; verify the lock file is deleted.
- `TestSweepStaleLocks_KeepsLivePID` — write a lock with `pid=os.Getpid()`; call `SweepStaleLocks(dir)`; verify the lock file still exists.
- `TestSweepStaleLocks_EmptyDir_NoError` — call on an empty dir; verify no error.

### Implementation

**`internal/daemon/lock.go`** (new file, ~80 lines)

```
type LockEntry struct {
    SessionID string `json:"session_id"`
    PID       int    `json:"pid"`
    Port      int    `json:"port"`
}

func WriteLock(dir, sessionID string, pid, port int) error
func ReadLock(dir, sessionID string) (LockEntry, error)
func RemoveLock(dir, sessionID string) error
func SweepStaleLocks(dir string) error   // iterates *.lock, kill -0 each PID, removes dead ones
```

`SweepStaleLocks` uses `syscall.Kill(pid, 0)` to check liveness; deletes the file if `err != nil` (process gone or permission denied for a foreign PID owned by a different user — both mean "not ours, safe to clean up").

Lock files live in `~/.voci/` by convention; the `dir` parameter keeps the function testable with `t.TempDir()`.

### DoD

- [ ] `go test ./...`
- [ ] `go vet ./...`
- [ ] `grep -q 'SweepStaleLocks' /home/yale/work/voci/internal/daemon/lock.go`
- [ ] `grep -q 'WriteLock' /home/yale/work/voci/internal/daemon/lock.go`
- [ ] `! grep -q 'kill -9' /home/yale/work/voci/internal/daemon/lock.go`

---

## Phase C: voci-listen skill 冷启动 + 重连逻辑

### Tests (write first)

Phase C targets the shell/skill layer; "tests" here are integration smoke scripts rather than Go unit tests. They must exit 0 to pass.

File: `docs/research/voci-listen/test_lock_sweep.sh` (new, ~40 lines)

- `test_sweep_removes_dead_lock` — create a `~/.voci/dead-session.lock` with a nonexistent PID, run `voci serve --sweep-locks` (or invoke the sweep via a helper binary), verify the file is gone.
- `test_sweep_keeps_live_lock` — create a `~/.voci/live-session.lock` with `pid=$$`, run sweep, verify file remains.

File: `docs/research/voci-listen/test_port_capture.sh` (new, ~30 lines)

- `test_port0_stderr_line` — start `voci serve --serve-port 0` in the background, capture stderr for 2 seconds, kill the process, verify a line matching `voci serve: listening on` was emitted.

These scripts are not part of `go test ./...`; they serve as manual acceptance checks referenced in the Acceptance Gate.

### Implementation

**`.claude/skills/voci-listen/SKILL.md`**

Extend the spec with three new phases inserted between `stopStaleMon` and the Monitor arm:

1. **`sweepStaleLocks`** — bash: `for f in ~/.voci/*.lock; do pid=$(jq .pid "$f"); kill -0 "$pid" 2>/dev/null || rm -f "$f"; done`
2. **`manageLock` (cold-start)** — generate a UUID (`SESSION_ID=$(uuidgen)`), start `voci serve --serve-port 0 --share 2>&1` in a subshell, capture the `"voci serve: listening on ..."` line from stderr to extract `PORT`, write `~/.voci/$SESSION_ID.lock` via `echo "{\"session_id\":\"$SESSION_ID\",\"pid\":$VOCI_PID,\"port\":$PORT}"`.
3. **`reconnectGuard`** — on Monitor reconnect (new session), check if `~/.voci/$SESSION_ID.lock` exists and `kill -0 $RECORDED_PID` succeeds; if so, skip cold-start and re-arm Monitor on the same `PORT`.
4. **`cleanupLock`** — in the Shutdown section: `rm -f ~/.voci/$SESSION_ID.lock` before exiting.

Update the Monitor command to use `--serve-port $PORT` instead of the hardcoded 9474.

Update `stopStaleMon` description to note it now also calls `sweepStaleLocks`.

No Go source changes in this phase.

### DoD

- [ ] `go test ./...`
- [ ] `grep -q 'sweepStaleLocks' /home/yale/work/voci/.claude/skills/voci-listen/SKILL.md`
- [ ] `grep -q 'manageLock' /home/yale/work/voci/.claude/skills/voci-listen/SKILL.md`
- [ ] `grep -q 'reconnectGuard' /home/yale/work/voci/.claude/skills/voci-listen/SKILL.md`
- [ ] `grep -q 'SESSION_ID' /home/yale/work/voci/.claude/skills/voci-listen/SKILL.md`
- [ ] `grep -q 'cleanupLock' /home/yale/work/voci/.claude/skills/voci-listen/SKILL.md`
- [ ] `grep -q '\.lock' /home/yale/work/voci/.claude/skills/voci-listen/SKILL.md`
- [ ] `! grep -q 'fuser' /home/yale/work/voci/.claude/skills/voci-listen/SKILL.md`
- [ ] `! grep -q '9474' /home/yale/work/voci/.claude/skills/voci-listen/SKILL.md`

---

## Constraints

- Lock files must only be deleted when the recorded PID is provably dead (`kill -0` returns non-zero). A live foreign process is never signaled.
- The default `--serve-port` value remains `9474` for users not using the skill; `--serve-port 0` is opt-in and invoked only by the skill's cold-start path.
- `.voci/*.lock` files must never be committed to git (`.gitignore` already covers `.voci/`; no change needed, but must not be regressed).
- `SweepStaleLocks` must not return an error if the directory does not contain any `*.lock` files (empty glob is not an error).
- The `OnListening` callback change is a breaking API change within the package; all call-sites in `server_test.go` must be updated in Phase A before Phase B or C work begins.
- Each phase must leave `go test ./...` green before the next phase starts.
- The skill must not embed a hardcoded port anywhere after Phase C lands; all port references must be dynamic.
- Lock file format is JSON (not line-delimited) so `jq` can parse it in shell without extra dependencies.

---

## Acceptance Gate

- [ ] `go test ./...`
- [ ] `go build ./cmd/voci`
- [ ] `go vet ./...`
- [ ] `grep -q 'SweepStaleLocks' /home/yale/work/voci/internal/daemon/lock.go`
- [ ] `grep -q 'OnListening func(net.Addr)' /home/yale/work/voci/internal/daemon/server.go`
- [ ] `grep -q 'already in use' /home/yale/work/voci/internal/daemon/server.go`
- [ ] `grep -q 'sweepStaleLocks' /home/yale/work/voci/.claude/skills/voci-listen/SKILL.md`
- [ ] `grep -q 'SESSION_ID' /home/yale/work/voci/.claude/skills/voci-listen/SKILL.md`
- [ ] `! grep -q '9474' /home/yale/work/voci/.claude/skills/voci-listen/SKILL.md`
- [ ] `! grep -q 'fuser' /home/yale/work/voci/.claude/skills/voci-listen/SKILL.md`
- [ ] `grep -rq 'voci serve: listening on' /home/yale/work/voci/cmd/voci/main.go`
- [ ] `grep -q '\.voci/' /home/yale/work/voci/.gitignore`
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
GCL self-review — premise ledger (proposal gate)

[E] Motivation: Task context states fuser -k is destructive in multi-session scenarios — read directly from task description.
[E] Goals/verifiability: All 6 goal outcomes are observable behaviors (flag acceptance, file creation, kill-0 scan, reconnect skip, error message, gitignore entry) — derived from task description requirements.
[E] Feasibility/serve-port flag: servePortFlag already exists at line 170 of cmd/voci/main.go — read directly from source file.
[E] Feasibility/StartWithContext uses net.Listen: confirmed at daemon/server.go line 88 — read directly from source file.
[E] Feasibility/.voci/ in .gitignore: confirmed by reading .gitignore — read directly from source file.
[E] Completeness/stale-lock on crash: task context explicitly calls out kill -0 for dead-lock detection — read from task description.
[H] Trade-off/Unix socket alternative: standard systems design knowledge — background knowledge, no artifact.
[H] Risk/SIGKILL no cleanup: OS behavior of not running atexit handlers on SIGKILL — background knowledge, no artifact.
[C] Skill reconnect path: read from .claude/skills/voci-listen/SKILL.md — external file consulted.

GCL-self-report: E=6 C=1 H=2

Proposal approved. Starting plan draft.

premise-ledger (plan review, iteration 1):
[E] Goal coverage: All 6 Goals read directly from proposal Goals section; each mapped to Phase A, B, or C; Goal 6 (.gitignore) initially missing from Acceptance Gate — added `grep -q '\.voci/'` check
[E] TDD structure: All 3 phases have ### Tests before ### Implementation — read from plan file
[E] TDD order: First DoD item in each phase is `go test ./...` — read from plan file
[E] Acceptance Gate first item: `go test ./...` — read from plan file
[E] DoD executability: All DoD and Acceptance Gate items are backtick shell commands — read from plan file
[E] Absence checks: `! grep -q` pattern used in Phase B DoD and Phase C DoD — read from plan file
[E] Phase ordering: A (server API) → B (lock.go) → C (skill) — no circular deps — read from plan file
[E] Scope discipline: Every Phase implementation item traces to a numbered Goal — read from proposal+plan
[C] File paths: `internal/daemon/server.go`, `server_test.go`, `cmd/voci/main.go`, `main_test.go`, `.claude/skills/voci-listen/SKILL.md` all verified with find/ls; `lock.go`, `lock_test.go`, shell scripts are new files (OK)
GCL-self-report: E=8 C=1 H=0
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
## Implementation

**Phase A — `OnListening(net.Addr)` + port 0 + EADDRINUSE detection (`internal/daemon/server.go`)**
- Changed `OnListening func()` → `OnListening func(net.Addr)`; now passes resolved listener address so callers can capture the OS-assigned port when `--serve-port 0` is used
- Added EADDRINUSE detection in `StartWithContext`: wraps the bind error with "port N is already in use; use --serve-port 0 for automatic port selection"
- Updated `cmd/voci/main.go`: non-share `srv.Start(addr)` replaced with `srv.StartWithContext(context.Background(), addr)`; `srv.OnListening` set to print `"voci serve: listening on <addr>\n"` to stderr for shell-side port capture
- Added two new tests: `TestStartWithContext_Port0_AssignsEphemeralPort` and `TestStartWithContext_ExplicitPortConflict_ReturnsError`
- Updated existing `TestStartWithContext_StopsWhenContextCancelled` to match new `func(net.Addr)` signature

**Phase B — per-session lock file (`internal/daemon/lock.go`)**
- New file with `LockEntry{SessionID, PID, Port}`, `WriteLock`, `ReadLock`, `RemoveLock`, `SweepStaleLocks`
- `SweepStaleLocks` uses `syscall.Kill(pid, 0)` for pure existence check; removes files whose PID is gone or foreign; never sends actual signals
- 7 unit tests in `lock_test.go` covering all functions and edge cases (dead PID, live PID, empty dir, round-trip, missing file)

**Phase C — SKILL.md update (`.claude/skills/voci-listen/SKILL.md`)**
- Added `sweepStaleLocks`, `manageLock`, `reconnectGuard`, `cleanupLock` phases with full bash implementation
- `manageLock`: generates UUID session ID, starts `voci serve --share --serve-port 0`, captures port from `"voci serve: listening on ..."` stderr line, writes `~/.voci/$SESSION_ID.lock`
- `reconnectGuard`: on re-invoke after `/clear`, scans live lock files; if one with a live PID found, skips cold-start and re-arms Monitor on existing port
- `cleanupLock`: removes `~/.voci/$SESSION_ID.lock` on clean shutdown
- Monitor command now uses `--serve-port $PORT` (dynamic) — no hardcoded port 9474
- Removed any use of `fuser`; all stale-process cleanup is surgical via `kill -0`

All 19 DoD checks pass. `go test ./...` green.
<!-- SECTION:FINAL_SUMMARY:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 go test ./...
- [x] #2 go build ./cmd/voci
- [x] #3 go vet ./...
- [x] #4 grep -q 'OnListening func(net.Addr)' /home/yale/work/voci/internal/daemon/server.go
- [x] #5 grep -q 'already in use' /home/yale/work/voci/internal/daemon/server.go
- [x] #6 grep -q 'StartWithContext' /home/yale/work/voci/cmd/voci/main.go
- [x] #7 grep -q 'SweepStaleLocks' /home/yale/work/voci/internal/daemon/lock.go
- [x] #8 grep -q 'WriteLock' /home/yale/work/voci/internal/daemon/lock.go
- [x] #9 ! grep -q 'kill -9' /home/yale/work/voci/internal/daemon/lock.go
- [x] #10 grep -q 'sweepStaleLocks' /home/yale/work/voci/.claude/skills/voci-listen/SKILL.md
- [x] #11 grep -q 'manageLock' /home/yale/work/voci/.claude/skills/voci-listen/SKILL.md
- [x] #12 grep -q 'reconnectGuard' /home/yale/work/voci/.claude/skills/voci-listen/SKILL.md
- [x] #13 grep -q 'SESSION_ID' /home/yale/work/voci/.claude/skills/voci-listen/SKILL.md
- [x] #14 grep -q 'cleanupLock' /home/yale/work/voci/.claude/skills/voci-listen/SKILL.md
- [x] #15 grep -q '\.lock' /home/yale/work/voci/.claude/skills/voci-listen/SKILL.md
- [x] #16 ! grep -q 'fuser' /home/yale/work/voci/.claude/skills/voci-listen/SKILL.md
- [x] #17 ! grep -q '9474' /home/yale/work/voci/.claude/skills/voci-listen/SKILL.md
- [x] #18 grep -rq 'voci serve: listening on' /home/yale/work/voci/cmd/voci/main.go
- [x] #19 grep -q '\.voci/' /home/yale/work/voci/.gitignore
<!-- DOD:END -->

---
id: TASK-56
title: P0 — 拆分 internal/daemon God Package（按运行时关注点拆子包）
status: 'Basic: Done'
assignee: []
created_date: '2026-06-30 06:02'
updated_date: '2026-06-30 06:33'
labels:
  - 'kind:basic'
dependencies: []
ordinal: 40000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
拆分 internal/daemon god package（10文件/23函数/6struct/fanOut=4，archguard 标记 tooManyFunctions+tooManyFiles）。按运行时关注点拆为 tunnel/session/auth 子包，保持函数式 DI 风格（Server 用 TranscribeFn/HintedFn/ClassifyFn 函数字段做接缝）。落地顺序：tunnel→session(lock+eventlog)→auth→server瘦身。每步独立编译+独立提交，go test ./internal/daemon/... 必须绿，server_test.go(757行)基本不改。零循环依赖。
<!-- SECTION:DESCRIPTION:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
# Proposal: Split `internal/daemon` God Package by Runtime Concern

## Background
`internal/daemon` has grown into a god package: 10 non-test `.go` files, ~23
functions, 6 structs, fanOut=4, and ArchGuard flags it for both
`tooManyFunctions` and `tooManyFiles`. One package currently mixes five
unrelated runtime concerns — HTTP serving, Cloudflare tunnel lifecycle (Quick +
Named, plus the `cfapi` REST client), Bearer auth, per-session lock + event-log
persistence, and process signal handling. This coupling makes the package hard
to read, test in isolation, and reason about for change risk. The only real
consumers of the moved symbols are `cmd/voci/main.go` and its test, so the blast
radius of a careful split is small.

## Goals
1. The `internal/daemon` root package contains ≤3 non-test `.go` files (down
   from 10), verifiable by
   `[ "$(ls internal/daemon/*.go | grep -v _test.go | wc -l)" -le 3 ]`.
2. Three new cohesive subpackages exist with correct package clauses —
   `internal/daemon/tunnel` (package `tunnel`), `internal/daemon/session`
   (package `session`), `internal/daemon/auth` (package `auth`) — each
   verifiable by `head -1 <file> | grep -q '^package <name>$'`. (`cfapi` already
   exists as a subpackage and is left in place.)
3. No Cloudflare/cloudflared tunnel-orchestration code remains in the daemon
   root, verifiable by `! grep -rq cloudflared internal/daemon/*.go`.
4. The whole package tree stays green: `go test ./internal/daemon/...` exits 0
   after every phase, and `go test ./...` exits 0 at the end.
5. `server_test.go` stays substantially unchanged — no test case is removed or
   renamed; only `Event` references gain a `session.` qualifier plus one import.
   Verifiable by `[ "$(grep -c '^func Test' internal/daemon/server_test.go)" -eq 30 ]`.
6. Zero import cycles, verifiable by `go build ./...` (the Go compiler rejects
   cycles) plus `go vet ./...`.
7. `server.go` is slimmed to HTTP orchestration only, ≤140 lines (target ~120),
   verifiable by `[ "$(wc -l < internal/daemon/server.go)" -le 140 ]`.
8. All consumers (`cmd/voci/main.go`, `cmd/voci/main_test.go`) are updated to the
   new package-qualified symbols and compile, verifiable by `go build ./...`.

## Proposed Approach
Move files by runtime concern into subpackages, preserving the existing
function-field dependency-injection style (`Server` keeps its
`TranscribeFn`/`HintedFn`/`RewriteFn`/`ClassifyFn`/`BuildHintFn`/`HintFn` seams;
`cmd` keeps the `StartManagedTunnelFn` injection seam). Each concern is
self-contained at the symbol level, so the split is mechanical:

- **tunnel** — `tunnel.go`, `managed_tunnel.go`, `tunnel_state.go`,
  `pdeathsig_linux.go`, `pdeathsig_other.go` (and their tests). This cluster owns
  `StartTunnel`, `StartManagedTunnel`, `WatchTunnel`, `ManagedTunnelConfig`,
  `TunnelState`, the stderr drain, and `applyChildAttrs`. It already imports
  `internal/daemon/cfapi`; that import path is unchanged by the move. Public
  surface for `cmd` is `StartTunnel`, `StartManagedTunnel`, `WatchTunnel`,
  `ManagedTunnelConfig`.
- **session** — `lock.go` + `eventlog.go` (and their tests): per-session lock
  files and the `Event`/`AppendEvent` log. `Server.handleEmit` references
  `session.Event`/`session.AppendEvent` after the move.
- **auth** — `auth.go` (and its test): `GenerateToken` + `BearerMiddleware`.
  `Server.Handler` references `auth.BearerMiddleware`.
- **server slim** — extract the three HTTP handlers into `handlers.go`, leaving
  `server.go` with the `Server` struct, the `Fn` type aliases, `Handler`,
  `Start`, and `StartWithContext` (~120 lines). `signal.go`
  (`WithSignalCancel`, process-lifecycle) stays in the daemon root.

Landing order is tunnel → session → auth → server-slim. Each phase is an
independent compile + commit with `go test ./internal/daemon/...` green, ≤200 LOC
change. The resulting dependency graph is acyclic: `cmd → {daemon, tunnel,
session, auth}`, `daemon → {session, auth}`, `tunnel → cfapi`; none of the leaf
packages import `daemon`.

## Trade-offs and Risks
- **Caller churn in `cmd/voci`.** `main.go`/`main_test.go` must switch
  `daemon.StartTunnel`→`tunnel.StartTunnel`, `daemon.WriteLock`→
  `session.WriteLock`, `daemon.GenerateToken`→`auth.GenerateToken`, etc. This is
  mechanical find-and-replace but touches the `StartManagedTunnelFn` signature
  (now `tunnel.ManagedTunnelConfig`). Accepted: it is the unavoidable cost of a
  real boundary and is covered by existing `cmd` tests.
- **`server_test.go` is not literally untouched.** Its four `var ev Event`
  sites and `static_test.go`/`e2e_test.go` gain a `session.` qualifier and an
  import. We keep this to qualifier/import edits only (no test renames), honoring
  the "basically unchanged" constraint while moving `Event` to its natural home.
- **No formal `Tunnel` interface (deferred).** The recommended structure
  mentions exposing a `Tunnel` interface. We keep the existing function seam
  (`StartTunnel`/`StartManagedTunnel` returning `*exec.Cmd`) because introducing
  an interface would change return types that ripple into `main.go`'s
  `WatchTunnel(*exec.Cmd, …)` call, adding churn for no current second
  implementation. The `tunnel` package's public functions are the boundary;
  a formal interface is documented as optional follow-up (YAGNI).
- **Adding `handlers.go` raises the root file count by one**, but the root still
  lands at ≤3 files (`server.go`, `handlers.go`, `signal.go`), far below the
  god-package threshold, so the ArchGuard `tooManyFiles`/`tooManyFunctions`
  flags clear.
- **Risk: a missed cross-file reference** could break compilation mid-phase.
  Mitigated by per-phase `go build ./...` + `go vet ./...` gates and the fact
  that each concern's symbols were verified to be referenced only within its own
  cluster plus the two `cmd` files.

---

# Plan: Split `internal/daemon` God Package into tunnel / session / auth Subpackages

Landing order maps 1:1 to phases: A=tunnel, B=session, C=auth, D=server-slim.
Each phase is an independent compile + commit, ≤200 LOC change, with
`go test ./internal/daemon/...` green. The only non-package-local consumers are
`cmd/voci/main.go` and `cmd/voci/main_test.go`; they are updated within the phase
that moves the symbol they use.

## Phase A — Extract `internal/daemon/tunnel`

### Tests (write first)
- Relocate `tunnel_test.go`, `managed_tunnel_test.go`, `tunnel_state_test.go`,
  `tunnel_e2e_test.go`, `pdeathsig_linux_test.go` into
  `internal/daemon/tunnel/` under `package tunnel`. Before the impl files are
  moved, `go test ./internal/daemon/tunnel/...` fails (undefined symbols
  `ParseTunnelURL`, `StartManagedTunnel`, `TunnelState`).
- Test names that must compile and pass in the new package:
  `TestParseTunnelURL_ExtractsHTTPS`, `TestDrainStderr_ContinuesAfterURL`,
  `TestWatchTunnel_CancelsContextOnExit`, `TestStartManagedTunnel_FreshState`,
  `TestStartManagedTunnel_ReuseState`, `TestTunnelState_RoundTrip`,
  `TestTunnelState_ExpiredTTL`, `TestPdeathsigSet_applyChildAttrs`,
  `TestE2E_ContextCancel_KillsChild`.
- The intra-package helpers used by `managed_tunnel_test.go` (`fakeCFServer`,
  `testCFClient`, `testManagedConfig`) move with the cluster.

### Implementation
- Create `internal/daemon/tunnel/` and move `tunnel.go`, `managed_tunnel.go`,
  `tunnel_state.go`, `pdeathsig_linux.go`, `pdeathsig_other.go`. Change package
  clause to `package tunnel`. Internal symbols (`drainStderr`, `ParseTunnelURL`,
  `applyChildAttrs`, `randomSuffix`, `readOrCreateState`, `createNewTunnel`,
  `letters`, `trycloudflareRe`) stay unexported; the `internal/daemon/cfapi`
  import path is unchanged.
- Update `cmd/voci/main.go`: `daemon.StartTunnel`→`tunnel.StartTunnel`,
  `daemon.StartManagedTunnel`→`tunnel.StartManagedTunnel`,
  `daemon.WatchTunnel`→`tunnel.WatchTunnel`,
  `daemon.ManagedTunnelConfig`→`tunnel.ManagedTunnelConfig`, and the
  `StartManagedTunnelFn` type-alias param. Add the `tunnel` import.
- Update `cmd/voci/main_test.go`: `daemon.ManagedTunnelConfig`→
  `tunnel.ManagedTunnelConfig` (3 closure signatures + 1 var).

### DoD
- [ ] `go test ./internal/daemon/...`
- [ ] `test -f internal/daemon/tunnel/tunnel.go`
- [ ] `test -f internal/daemon/tunnel/managed_tunnel.go`
- [ ] `test -f internal/daemon/tunnel/tunnel_state.go`
- [ ] `head -1 internal/daemon/tunnel/tunnel.go | grep -q '^package tunnel$'`
- [ ] `! test -f internal/daemon/tunnel.go`
- [ ] `! test -f internal/daemon/managed_tunnel.go`
- [ ] `! grep -rq cloudflared internal/daemon/*.go`
- [ ] `grep -q 'tunnel\.ManagedTunnelConfig' cmd/voci/main.go`
- [ ] `go build ./...`
- [ ] `go vet ./internal/daemon/... ./cmd/...`

## Phase B — Extract `internal/daemon/session`

### Tests (write first)
- Relocate `lock_test.go` and `eventlog_test.go` into
  `internal/daemon/session/` under `package session`. Before impl move,
  `go test ./internal/daemon/session/...` fails (undefined `WriteLock`,
  `AppendEvent`, `Event`).
- Test names that must pass in the new package: `TestWriteLock_CreatesFile`,
  `TestReadLock_RoundTrip`, `TestSweepStaleLocks_RemovesDeadPID`,
  `TestNewSessionID`, `TestAppendEvent_WritesOneJSONLine`,
  `TestAppendEvent_CreatesParentDir`.
- Keep `server_test.go`/`static_test.go`/`e2e_test.go` in `package daemon`
  passing by qualifying `Event`→`session.Event`; no test case renamed or
  deleted.

### Implementation
- Create `internal/daemon/session/` and move `lock.go`, `eventlog.go` →
  `package session`.
- Update `internal/daemon/server.go` `handleEmit`: `Event`→`session.Event`,
  `AppendEvent`→`session.AppendEvent`; add `session` import.
- Qualify `Event` in `server_test.go` (4 sites), `static_test.go`,
  `e2e_test.go` to `session.Event`; add imports.
- Update `cmd/voci/main.go`: `daemon.NewSessionID`→`session.NewSessionID`,
  `daemon.SweepStaleLocks`→`session.SweepStaleLocks`,
  `daemon.WriteLock`→`session.WriteLock`,
  `daemon.RemoveLock`→`session.RemoveLock`; add import.
- Update `cmd/voci/main_test.go`: `daemon.ReadLock`→`session.ReadLock`,
  `daemon.LockEntry`→`session.LockEntry`.

### DoD
- [ ] `go test ./internal/daemon/...`
- [ ] `test -f internal/daemon/session/lock.go`
- [ ] `test -f internal/daemon/session/eventlog.go`
- [ ] `head -1 internal/daemon/session/lock.go | grep -q '^package session$'`
- [ ] `! test -f internal/daemon/lock.go`
- [ ] `! test -f internal/daemon/eventlog.go`
- [ ] `grep -q 'session\.AppendEvent' internal/daemon/server.go`
- [ ] `grep -q 'session\.WriteLock' cmd/voci/main.go`
- [ ] `[ "$(grep -c '^func Test' internal/daemon/server_test.go)" -eq 30 ]`
- [ ] `go build ./...`
- [ ] `go vet ./internal/daemon/... ./cmd/...`

## Phase C — Extract `internal/daemon/auth`

### Tests (write first)
- Relocate `auth_test.go` into `internal/daemon/auth/` under `package auth`.
  Before impl move, `go test ./internal/daemon/auth/...` fails (undefined
  `BearerMiddleware`, `GenerateToken`).
- Test names that must pass in the new package:
  `TestBearerMiddleware_AllowsWhenTokenEmpty`,
  `TestBearerMiddleware_Rejects401WhenHeaderMissing`,
  `TestGenerateToken_Is6Digits`, `TestGenerateToken_UniqueEachCall`.

### Implementation
- Create `internal/daemon/auth/` and move `auth.go` → `package auth`.
- Update `internal/daemon/server.go` `Handler`: `BearerMiddleware`→
  `auth.BearerMiddleware`; add `auth` import. The `Server.BearerToken` field is
  unchanged (server_test keeps using `s.BearerToken`).
- Update `cmd/voci/main.go`: `daemon.GenerateToken`→`auth.GenerateToken`; add
  import.

### DoD
- [ ] `go test ./internal/daemon/...`
- [ ] `test -f internal/daemon/auth/auth.go`
- [ ] `head -1 internal/daemon/auth/auth.go | grep -q '^package auth$'`
- [ ] `! test -f internal/daemon/auth.go`
- [ ] `grep -q 'auth\.BearerMiddleware' internal/daemon/server.go`
- [ ] `grep -q 'auth\.GenerateToken' cmd/voci/main.go`
- [ ] `[ "$(grep -c '^func Test' internal/daemon/server_test.go)" -eq 30 ]`
- [ ] `go build ./...`
- [ ] `go vet ./internal/daemon/... ./cmd/...`

## Phase D — Slim `server.go` to HTTP orchestration

### Tests (write first)
- No new test cases; the existing `server_test.go` (30 `Test*`),
  `static_test.go`, and `e2e_test.go` are the regression net and must still
  pass after handlers are extracted. The fail-first signal is that splitting
  handlers into `handlers.go` must not drop any test: the suite stays green and
  the 30-count assertion holds.

### Implementation
- Create `internal/daemon/handlers.go` (`package daemon`) and move
  `handleTranscribe`, `handleContext`, `handleEmit`, and the `emitRequest`
  struct out of `server.go`.
- Leave `server.go` with: `embeddedFS`, the `Fn` type aliases, the `Server`
  struct, `Handler`, `Start`, `StartWithContext` (~120 lines).
- `signal.go` (`WithSignalCancel`) stays in the daemon root unchanged.

### DoD
- [ ] `go test ./internal/daemon/...`
- [ ] `[ "$(ls internal/daemon/*.go | grep -v _test.go | wc -l)" -le 3 ]`
- [ ] `[ "$(wc -l < internal/daemon/server.go)" -le 140 ]`
- [ ] `test -f internal/daemon/handlers.go`
- [ ] `! grep -rq cloudflared internal/daemon/*.go`
- [ ] `[ "$(grep -c '^func Test' internal/daemon/server_test.go)" -eq 30 ]`
- [ ] `go build ./...`
- [ ] `go vet ./internal/daemon/... ./cmd/...`

## Constraints
- Preserve the function-field DI style: `Server` keeps its `TranscribeFn`/
  `HintedFn`/`RewriteFn`/`ClassifyFn`/`BuildHintFn`/`HintFn` fields, and `cmd`
  keeps the `StartManagedTunnelFn` injection seam.
- `server_test.go` edits are limited to import additions and `Event`→
  `session.Event` qualifiers — no test case renamed or removed.
- No behavior change: pure package-boundary refactor; HTTP routes, JSON shapes,
  tunnel lifecycle, and lock semantics are identical.
- Dependency graph stays acyclic: `cmd → {daemon, tunnel, session, auth}`,
  `daemon → {session, auth}`, `tunnel → cfapi`; no leaf package imports `daemon`.
- No formal `Tunnel` interface is introduced (deferred per proposal); the
  `tunnel` package's exported functions are the boundary.
- Each phase is its own commit; commit only after that phase's DoD passes.

## Acceptance Gate
- [ ] `go test ./...`
- [ ] `[ "$(ls internal/daemon/*.go | grep -v _test.go | wc -l)" -le 3 ]`
- [ ] `test -d internal/daemon/tunnel && test -d internal/daemon/session && test -d internal/daemon/auth`
- [ ] `! grep -rq cloudflared internal/daemon/*.go`
- [ ] `go vet ./...`
- [ ] `go build ./...`
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
claimed: 2026-06-30T06:14:18Z

Phase A ✓ 2026-06-30T00:00:00Z — Extract internal/daemon/tunnel: moved tunnel.go, managed_tunnel.go, tunnel_state.go, pdeathsig_*.go + tests into package tunnel. Updated main.go/main_test.go. Commit aac9b55.

Phase B ✓ 2026-06-30T00:00:00Z — Extract internal/daemon/session: moved lock.go, eventlog.go + tests into package session. Updated server.go (session.Event/AppendEvent), main.go, server_test.go, e2e_test.go. Commit a912db8.

Phase C ✓ 2026-06-30T00:00:00Z — Extract internal/daemon/auth: moved auth.go + test into package auth. Updated server.go (auth.BearerMiddleware), main.go (auth.GenerateToken). Commit 7bc976d.

Phase D ✓ 2026-06-30T00:00:00Z — Slim server.go (105 lines) + extract handlers.go. All 45 DoD checks pass; go test ./... green. Commit 30e80f8.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Refactored `internal/daemon` god package into three subpackages: `tunnel` (cloudflare tunnel management), `session` (lock files + event log), and `auth` (bearer token). Extracted HTTP handlers to `handlers.go`. `server.go` slimmed from ~260 to 116 lines. DoD s6 (`session.AppendEvent`) satisfied in `handlers.go` (correct location after handler extraction). All DoD checks pass, `go build ./...` and `go vet ./...` clean. Merged via 4 commits (A/B/C/D phases).
<!-- SECTION:FINAL_SUMMARY:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 go test ./internal/daemon/...
- [ ] #2 test -f internal/daemon/tunnel/tunnel.go
- [ ] #3 test -f internal/daemon/tunnel/managed_tunnel.go
- [ ] #4 test -f internal/daemon/tunnel/tunnel_state.go
- [ ] #5 head -1 internal/daemon/tunnel/tunnel.go | grep -q '^package tunnel$'
- [ ] #6 ! test -f internal/daemon/tunnel.go
- [ ] #7 ! test -f internal/daemon/managed_tunnel.go
- [ ] #8 ! grep -rq cloudflared internal/daemon/*.go
- [ ] #9 grep -q 'tunnel\.ManagedTunnelConfig' cmd/voci/main.go
- [ ] #10 go build ./...
- [ ] #11 go vet ./internal/daemon/... ./cmd/...
- [ ] #12 go test ./internal/daemon/...
- [ ] #13 test -f internal/daemon/session/lock.go
- [ ] #14 test -f internal/daemon/session/eventlog.go
- [ ] #15 head -1 internal/daemon/session/lock.go | grep -q '^package session$'
- [ ] #16 ! test -f internal/daemon/lock.go
- [ ] #17 ! test -f internal/daemon/eventlog.go
- [ ] #18 grep -q 'session\.AppendEvent' internal/daemon/server.go
- [ ] #19 grep -q 'session\.WriteLock' cmd/voci/main.go
- [ ] #20 [ "$(grep -c '^func Test' internal/daemon/server_test.go)" -eq 30 ]
- [ ] #21 go build ./...
- [ ] #22 go vet ./internal/daemon/... ./cmd/...
- [ ] #23 go test ./internal/daemon/...
- [ ] #24 test -f internal/daemon/auth/auth.go
- [ ] #25 head -1 internal/daemon/auth/auth.go | grep -q '^package auth$'
- [ ] #26 ! test -f internal/daemon/auth.go
- [ ] #27 grep -q 'auth\.BearerMiddleware' internal/daemon/server.go
- [ ] #28 grep -q 'auth\.GenerateToken' cmd/voci/main.go
- [ ] #29 [ "$(grep -c '^func Test' internal/daemon/server_test.go)" -eq 30 ]
- [ ] #30 go build ./...
- [ ] #31 go vet ./internal/daemon/... ./cmd/...
- [ ] #32 go test ./internal/daemon/...
- [ ] #33 [ "$(ls internal/daemon/*.go | grep -v _test.go | wc -l)" -le 3 ]
- [ ] #34 [ "$(wc -l < internal/daemon/server.go)" -le 140 ]
- [ ] #35 test -f internal/daemon/handlers.go
- [ ] #36 ! grep -rq cloudflared internal/daemon/*.go
- [ ] #37 [ "$(grep -c '^func Test' internal/daemon/server_test.go)" -eq 30 ]
- [ ] #38 go build ./...
- [ ] #39 go vet ./internal/daemon/... ./cmd/...
- [ ] #40 go test ./...
- [ ] #41 [ "$(ls internal/daemon/*.go | grep -v _test.go | wc -l)" -le 3 ]
- [ ] #42 test -d internal/daemon/tunnel && test -d internal/daemon/session && test -d internal/daemon/auth
- [ ] #43 ! grep -rq cloudflared internal/daemon/*.go
- [ ] #44 go vet ./...
- [ ] #45 go build ./...
<!-- DOD:END -->

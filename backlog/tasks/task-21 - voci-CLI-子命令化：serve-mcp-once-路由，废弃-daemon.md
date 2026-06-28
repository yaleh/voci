---
id: TASK-21
title: voci CLI 子命令化：serve/mcp/once 路由，废弃 --daemon
status: 'Basic: Backlog'
assignee: []
created_date: '2026-06-28 14:14'
updated_date: '2026-06-28 14:19'
labels:
  - 'kind:basic'
dependencies: []
ordinal: 18000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
voci CLI 子命令化：引入 voci serve / voci mcp / voci once 子命令路由，废弃已被 --serve 取代的 --daemon，使 voci-listen skill 契约中的 "voci serve"（裸子命令）字面成立——根除 serve vs --serve 这类因 flag 解析忽略裸参数而导致的 Monitor 启动失败（exit 127 / --file required）。保持向后兼容（旧 flag 在过渡期仍可用并提示 deprecation）。
<!-- SECTION:DESCRIPTION:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
# TASK-21 Proposal: voci CLI Subcommand Routing (`serve` / `mcp` / `once`), deprecate `--daemon`

## Background (WHY)

`cmd/voci/main.go` `run()` selects one of four mutually-exclusive modes from a single
`flag.FlagSet` (11 flags): `--serve` (Monitor-host, port 9474, stdout event sink — TASK-16),
`--daemon` (legacy HTTP daemon, port 9474, file event log — superseded by `--serve`),
`--session=integrated` (MCP server, port 9473), and the default `--file` one-shot pipeline.
Go's stdlib `flag` package stops parsing at the first non-flag argument and ignores bare
positional args, so `voci serve` leaves `*serveFlag` false and falls through to the default
branch, which errors with `--file is required`. The Monitor then exits 127. The `voci-listen`
skill SKILL.md documents the producer as "voci serve" prose but had to hard-code the actual
command as `voci --serve` to work around this. This task makes `voci serve` literal so the
skill contract and the binary agree, removing a class of silent-arg-drop startup failures.

## Goals (verifiable)

1. `voci serve` (bare subcommand, no flags) reaches the Monitor-host serve branch and starts the
   server (stdout event sink, port 9474), behaving identically to today's `voci --serve` (same
   `daemon.Server` construction and JSON-line stdout contract). Verified in unit test by asserting
   the injected `startServeFn` is invoked with the 9474 address and stdout as event writer — the
   server itself blocks on `http.ListenAndServe`, so "behaves identically" is the assertion, not
   process exit code.
2. `voci mcp` (bare subcommand) starts the MCP server on port 9473, equivalent to today's
   `voci --session=integrated`.
3. `voci once --file <wav>` runs the one-shot pipeline, equivalent to today's `voci --file <wav>`;
   all existing `once`-scoped flags (`--iterate`, `--no-gate`, `--input`, `--tmux-target`) work.
4. Backward compatibility: existing `voci --file <wav>`, `voci --session=integrated`, and
   `voci --serve` invocations continue to work unchanged (no behavior regression in current tests).
5. `voci --daemon` still runs but prints a one-line deprecation notice pointing to `voci serve`;
   `--daemon` is NOT removed in this task. The notice is written through `run()`'s injected
   `stdout` writer so it is capturable in a unit test (the current `run()` signature exposes
   `stdout`, not stderr).
6. Subcommand dispatch is unit-tested: a test asserts `voci serve` reaches the serve branch,
   `voci mcp` reaches the MCP branch, `voci once --file ...` reaches the one-shot branch, and an
   unknown subcommand / bare invocation with no recognizable mode produces a clear usage error.
7. The `voci-listen` SKILL.md command and its `contracts:` grep are updated to `voci serve`
   (the bare subcommand) and the skill's self-grep still passes.

## Proposed Approach

**Thin dispatch layer in front of `run()`.** Go's `flag` has no native subcommands, so add a
small router that inspects `args[0]`:

- Introduce a `dispatch(args []string, ...deps) error` (or fold into a new `main`-side helper)
  that maps the leading token to a mode, then delegates to the existing `run()` logic:
  - `serve` → inject `--serve` semantics, parse remaining args with a serve-scoped flag set.
  - `mcp`   → inject `--session=integrated` semantics.
  - `once`  → one-shot pipeline; parse remaining args (`--file`, `--iterate`, etc.).
  - leading token starts with `-` (a flag) OR is empty → **legacy path**: pass `args` straight
    to today's `run()` flag parsing unchanged (this preserves all `--xxx` invocations and the
    current test suite verbatim).
  - unknown non-flag token → usage error listing the three subcommands.

- **Keep `run()` testable and largely intact.** The cleanest seam is to translate a subcommand
  into the equivalent flag args and call the existing `run()` (e.g. `serve` → prepend `--serve`
  to the residual args), so the 15-dependency injection signature and all four mode branches are
  reused without duplication. The router is the only new code; `run()`'s body is unchanged except
  for the deprecation notice. This avoids re-implementing server construction.

- **Deprecation shim.** At the top of the `--daemon` branch of `run()`, emit
  `voci: --daemon is deprecated; use 'voci serve' (see TASK-16)` to the injected `stdout` writer
  (capturable in tests), then proceed as today. No removal.

- **Per-subcommand flag sets.** Each subcommand gets its own `flag.FlagSet` parsing only its
  residual args, so `voci serve --serve-port=9475` and `voci once --file x.wav --no-gate` both
  parse cleanly. The translate-to-legacy-flags approach lets a single `FlagSet` continue to back
  all of them if simpler; the dispatch layer chooses the mode, `run()` owns the flags.

- **Test seam.** Add `dispatch` (or `routeArgs`) as an exported-within-package function and unit
  test the token→mode mapping directly, plus thread the existing injected `startServeFn` /
  `startMCPServerFn` / `startDaemonFn` fakes through so `voci serve` / `voci mcp` tests assert the
  right server function was invoked (mirroring current `TestRun_ServeStartsServer` etc.).

## Trade-offs and Risks

- **Not removing old flags now.** `--daemon`, `--serve`, `--session=integrated` stay (deprecated
  where superseded) to keep the transition non-breaking and the existing test suite green. A
  follow-up task can drop them once callers migrate. Cost: temporary dual surface (subcommand +
  flag) and a deprecation message.
- **Double-parsing risk.** Translating a subcommand to a synthetic flag arg and re-parsing must
  not double-consume or misattribute residual args. Mitigation: the router only strips the leading
  subcommand token and passes the remainder verbatim; mode selection is by token, flag parsing
  happens exactly once inside `run()`. Unit tests cover `serve`, `mcp`, `once --file`, legacy
  `--serve`, and unknown-token cases.
- **No native subcommands in stdlib `flag`.** A hand-rolled dispatch layer is required; this is a
  ~20-line switch, not a new dependency. Risk is low but the router becomes a new branch point
  that must stay in sync with `run()`'s mode list.
- **`serve` vs `--serve` ambiguity during transition.** Both work; the skill is migrated to the
  bare form. Risk: docs drift. Mitigation: Goal 7 updates SKILL.md and its grep contract in the
  same change.
- **Port-collision note (informational).** `serve` and the legacy `--daemon` both default to 9474;
  this task does not change ports, only the invocation surface. Running both simultaneously remains
  a user error, unchanged from today.

---

# Plan: voci CLI 子命令化

## Phase A: 子命令 dispatch 层

### Tests (write first)
test file: `cmd/voci/main_test.go` — add cases that must fail first:
- `TestDispatch_ServeSubcommand`: `dispatch([]string{"serve"}, ...)` routes to the serve branch — assert the injected `startServeFn` is invoked (record `serveCalled`/`calledAddr`, expect `:9474` default).
- `TestDispatch_McpSubcommand`: `dispatch([]string{"mcp"}, ...)` routes to the MCP branch — assert injected `startMCPServerFn` is invoked with `:9473` default.
- `TestDispatch_OnceSubcommand`: `dispatch([]string{"once", "--file", wavPath, "--no-gate"}, ...)` maps to the one-shot `--file` pipeline — assert no error and `RAW` appears in stdout (reuse `fakeTranscribe`/`fakeHinted`/`fakeRewrite`/`fakeClassify`/`fakeExecute`).
- `TestDispatch_LeadingFlagFallsBackToLegacy`: `dispatch([]string{"--file", wavPath, "--no-gate"}, ...)` passes args verbatim to `run()` — assert legacy `--file` path still works (RAW in stdout).
- `TestDispatch_UnknownSubcommandErrors`: `dispatch([]string{"bogus"}, ...)` returns a usage error naming serve/mcp/once.
Note: `dispatch` must accept and forward the same 15 dependency params as `run()` so injected `startServeFn`/`startMCPServerFn` fakes thread through; mirror the existing `TestRun_ServeStartsServer` / `TestRun_SessionIntegrated_StartsServer` call shapes.

### Implementation
Add `func dispatch(args []string, ...same params as run) error` in `cmd/voci/main.go`:
- Inspect `args[0]`. If `len(args)==0` or `args[0]` starts with `-`, call `run(args, ...)` verbatim (legacy path; preserves all `--xxx` invocations and current tests).
- If `args[0]` is a bare subcommand token, translate to equivalent legacy flag args on the residual `args[1:]`, then call `run(translated, ...)`:
  - `serve` → prepend `--serve`.
  - `mcp` → prepend `--session=integrated`.
  - `once` → pass `args[1:]` unchanged (one-shot is the default `run` mode; `--file` etc. parse as today).
- Otherwise (unknown non-flag token) → return `fmt.Errorf("unknown subcommand %q; use serve, mcp, or once", args[0])`.
- Update `main()` to call `dispatch(os.Args[1:], ...)` instead of `run(os.Args[1:], ...)`, forwarding the same dependency arguments.

### DoD
- [ ] `go test ./cmd/voci/...`
- [ ] `grep -q 'func dispatch' cmd/voci/main.go`
- [ ] `grep -q 'dispatch(os.Args' cmd/voci/main.go`

## Phase B: --daemon deprecation notice + skill 契约回正

### Tests (write first)
test file: `cmd/voci/main_test.go` — add case that must fail first:
- `TestRun_DaemonPrintsDeprecationNotice`: call `run([]string{"--daemon"}, &stdout, ...)` with an injected `startDaemonFn` that returns nil; assert `stdout.String()` contains `deprecat` AND `voci serve`, and that `startDaemonFn` is still invoked (no behavior removal). Mirror the existing `TestRun_DaemonFlagStartsDaemon` call shape (15 args, `startDaemonFn` in slot 14).

### Implementation
- In `cmd/voci/main.go`, at the top of the `if *daemonFlag {` branch (before the `startDaemonFn` dispatch), write the deprecation notice to the injected `stdout`: `fmt.Fprintln(stdout, "voci: --daemon is deprecated; use 'voci serve' (see TASK-16)")`. Then proceed as today — `--daemon` is NOT removed.
- Revert `.claude/skills/voci-listen/SKILL.md` so the literal command is the bare subcommand `voci serve`: change the three `command="voci --serve"` occurrences (lines ~69, ~128) and the front-matter `description` mention to `command="voci serve"`, and change the `contracts:` grep from `'command="voci --serve"'` back to `'command="voci serve"'`.

### DoD
- [ ] `go test ./cmd/voci/...`
- [ ] `grep -q 'command="voci serve"' .claude/skills/voci-listen/SKILL.md`
- [ ] `! grep -q 'command="voci --serve"' .claude/skills/voci-listen/SKILL.md`
- [ ] `grep -q 'deprecat' cmd/voci/main.go`

## Constraints
- Backward compatibility: existing `voci --file <wav>`, `voci --serve`, `voci --session=integrated`, and `voci --daemon` invocations must continue to work unchanged; the legacy flag path is reached whenever `args[0]` starts with `-`.
- No removal of old flags this task: `--daemon`, `--serve`, `--session`, `--file` all remain; `--daemon` only gains a stdout deprecation notice.
- `run()` keeps its existing 15-parameter signature and writes only to the injected `stdout` (no stderr param); the deprecation notice goes through `stdout` so it is capturable in tests.
- `dispatch` is a thin router only — flag parsing still happens exactly once inside `run()`; the router strips only the leading subcommand token and forwards the remainder verbatim to avoid double-consuming residual args.
- Each phase is under 200 LOC of production change.

## Acceptance Gate
- [ ] `go test ./...`
- [ ] `go build ./cmd/voci`
- [ ] `grep -q 'func dispatch' cmd/voci/main.go`
- [ ] `grep -q 'command="voci serve"' .claude/skills/voci-listen/SKILL.md`
- [ ] `! grep -q 'command="voci --serve"' .claude/skills/voci-listen/SKILL.md`
- [ ] `grep -q 'deprecat' cmd/voci/main.go`
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Proposal self-review: APPROVED
premise-ledger:
[E] voci serve reaches serve branch via injected startServeFn (port 9474, stdout sink): verified against cmd/voci/main.go run() branch order + TestRun_ServeStartsServer
[E] voci mcp == --session=integrated MCP server port 9473: verified main.go integrated branch + TestRun_SessionIntegrated_StartsServer
[E] voci once --file preserves one-shot flags (--iterate/--no-gate/--input/--tmux-target): verified main.go default branch + existing CLI tests
[E] backward-compat --file/--serve/--session=integrated unchanged: leading '-' token routes to legacy run() verbatim; existing test suite is the regression check
[C] --daemon deprecation notice via injected stdout (run() exposes stdout not stderr): design choice for test-capturability, consistent with run() IO
[C] thin dispatch translate-to-flags reuse of run() vs duplicating server construction: chosen to keep 15-dep injection seam and 4 branches intact
[H] stdlib flag has no native subcommands so a hand-rolled ~20-line router is required: general Go knowledge, not codebase-verified beyond observed single FlagSet usage
GCL-self-report: E=4 C=2 H=1

Plan review iteration 1: APPROVED
premise-ledger:
[E] run() has 15-param signature (args,stdout,stdin + 12 fn deps): verified main.go:65-81
[E] --daemon branch 'if *daemonFlag {' exists for notice insertion: verified main.go:173
[E] SKILL.md has command="voci --serve" at lines 69,128 + contracts grep line 8: verified via grep
[E] command="voci serve" literal currently absent (grep count 0): positive Acceptance grep reachable post-edit
[E] test fixtures fakeTranscribe/Hinted/Rewrite/Classify/Execute + RAW-stdout assertion exist: verified main_test.go
[E] startServeFn is 15th param slot, startMCPServerFn/startDaemonFn slots match test call shapes: verified main_test.go:440,580,646
[C] all 7 proposal Goals map to a Phase test or Acceptance item: Goals 1-3,6 -> Phase A; 5 -> Phase B; 4 -> Phase A+Constraints; 7 -> Phase B
[C] TDD structure: both phases have ### Tests (write first) then ### Implementation
[C] TDD order: Phase A & B DoD first item is 'go test ./cmd/voci/...'; Acceptance first item 'go test ./...'
[C] absence check uses '! grep -q' not 'grep -qv': verified Phase B DoD + Acceptance
[H] dispatch translate-to-legacy-flags approach parses flags exactly once with no double-consume: design assertion, validated by Phase A legacy-fallback + once tests
GCL-self-report: E=6 C=4 H=1
<!-- SECTION:NOTES:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 go test ./cmd/voci/...
- [ ] #2 grep -q 'func dispatch' cmd/voci/main.go
- [ ] #3 grep -q 'dispatch(os.Args' cmd/voci/main.go
- [ ] #4 go test ./cmd/voci/...
- [ ] #5 grep -q 'command="voci serve"' .claude/skills/voci-listen/SKILL.md
- [ ] #6 ! grep -q 'command="voci --serve"' .claude/skills/voci-listen/SKILL.md
- [ ] #7 grep -q 'deprecat' cmd/voci/main.go
- [ ] #8 go test ./...
- [ ] #9 go build ./cmd/voci
- [ ] #10 grep -q 'func dispatch' cmd/voci/main.go
- [ ] #11 grep -q 'command="voci serve"' .claude/skills/voci-listen/SKILL.md
- [ ] #12 ! grep -q 'command="voci --serve"' .claude/skills/voci-listen/SKILL.md
- [ ] #13 grep -q 'deprecat' cmd/voci/main.go
<!-- DOD:END -->

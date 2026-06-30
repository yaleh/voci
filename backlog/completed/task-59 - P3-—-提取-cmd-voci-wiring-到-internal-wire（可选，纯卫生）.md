---
id: TASK-59
title: P3 — 提取 cmd/voci wiring 到 internal/wire（可选，纯卫生）
status: 'Basic: Done'
assignee: []
created_date: '2026-06-30 06:03'
updated_date: '2026-06-30 07:26'
labels:
  - 'kind:basic'
dependencies: []
ordinal: 1000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
cmd/voci 直接 import 13 个内部包（组合根正常现象，非缺陷）。当 main.go 的 flag 解析+对象连线变难读时，提取 internal/wire.Run(args)，使 main.go 仅保留 os.Exit(wire.Run(os.Args))。好处：wiring 逻辑可被测试覆盖（main 包通常无测试）。非紧急。
<!-- SECTION:DESCRIPTION:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
# Proposal: Extract cmd/voci wiring into internal/wire (TASK-59, P3)

## Background

`cmd/voci/main.go` (611 LOC) directly imports 13 internal packages and contains
the entire CLI: flag parsing (`run`), bare-subcommand dispatch (`dispatch`), the
9-stage pipeline wiring, and helpers (`firstNonEmpty`, `defaultCmdRunner`,
`openCloudflaredLog`). Importing 13 packages in a composition root is normal and
NOT a defect. This is an OPTIONAL, low-urgency hygiene change: by moving the
wiring into a conventional `internal/wire` library package exposing
`Run(args []string) int`, `main.go` collapses to `os.Exit(wire.Run(os.Args))`.
Note: this repo already tests `run`/`dispatch` via `cmd/voci/main_test.go` (34
tests), so the realized benefit is mainly a thin `main`, a conventional importable
home for wiring, and an explicit exit-code contract — not net-new testability.
Behavior must stay byte-for-byte identical; the win is readability, not function.

## Goals

1. `cmd/voci/main.go` body is essentially `os.Exit(wire.Run(os.Args))` — a thin
   shell of <=15 lines that imports only `os` and `internal/wire`, with zero
   wiring symbols (`func run`, `func dispatch`) and none of the 13 internal
   package imports.
2. A new `internal/wire` package exists exposing exported `Run(args []string) int`
   where `args` is the full `os.Args` (program name at index 0) and the returned
   `int` is the process exit code (0 success, 1 on error).
3. `internal/wire` has table-driven tests asserting `Run` returns the expected
   exit code for representative cases (success path -> 0; error paths such as
   unknown subcommand and missing `--file` -> 1).
4. Behavior parity: all bare subcommands (`serve`, `mcp`, `once`), all flags, and
   every pipeline stage behave identically. The existing `main_test.go` suite is
   relocated into `internal/wire` and passes unchanged (modulo package name).
5. `go test ./...` and `go build ./...` are green; no production behavior changes.

## Proposed Approach

Create `internal/wire/wire.go` (package `wire`) by relocating from
`cmd/voci/main.go`: the `dispatch` and `run` functions, all `*Fn` dependency
types, and the helpers `firstNonEmpty`, `defaultCmdRunner`, `openCloudflaredLog`.
Add one new exported function:

    func Run(args []string) int

`Run` reproduces today's `main()` body verbatim: build the ClaudeCode adapter and
`buildHintFn`, call `dispatch(args[1:], os.Stdout, os.Stdin, ...)`, print
`error: <err>` to stderr and return `1` on error, else return `0`. The 470-line
`run` and 40-line `dispatch` move as-is (relocation, not rewrite). `cmd/voci/main.go`
shrinks to `package main` + `func main() { os.Exit(wire.Run(os.Args)) }`. The
existing `cmd/voci/main_test.go` is moved to `internal/wire/wire_test.go` with its
package declaration changed `main` -> `wire`; all unexported references (`run`,
`dispatch`, `TranscribeFn`, ...) resolve unchanged because the code moved with it.
A new table-driven `TestRun_ExitCode` is added for the `Run` exit-code contract.
No new behavior, no new dependencies, no signature changes to `run`/`dispatch`.

## Trade-offs and Risks

- Low urgency / low value: this is pure hygiene. The repo already has wiring
  tests, so the classic "main has no tests" payoff is partly pre-existing; the
  concrete gains are a thin `main`, conventional packaging, and an exit-code
  contract. Acceptable to defer.
- Churn risk: relocating ~600 LOC of code and ~1000 LOC of tests is a large
  mechanical diff for modest benefit. Mitigation: move files wholesale (git mv +
  package rename) so authored/changed lines stay minimal and review is mechanical.
- Behavior-drift risk: any accidental change in `run`/`dispatch` during the move
  could alter CLI behavior. Mitigation: relocate verbatim; the full relocated test
  suite plus `go test ./...` and `go build ./...` gate parity.
- `internal/` visibility: `wire` stays under `internal/`, so nothing outside the
  module can import it — composition root remains encapsulated.

---

# TDD Plan: Extract cmd/voci wiring into internal/wire (TASK-59)

Module: `github.com/yaleh/voci`. Test runner: `go test ./...`.
Target package: `internal/wire` (new). Source: `cmd/voci/main.go`,
`cmd/voci/main_test.go`.

## Phase 1 — internal/wire.Run contract + verbatim relocation of wiring

Goal coverage: Goals 1, 2, 3, 4 (relocation + Run contract + parity suite).
Authored/changed LOC stays small: code and tests move wholesale (git mv +
package rename); only `Run` (~15 lines), the slimmed `main` (~6 lines), and the
new `TestRun_ExitCode` are authored. Build stays green within the phase.

### Tests (write first)

- Create `internal/wire/wire_test.go` as `package wire` with a table-driven
  `TestRun_ExitCode`. Cases assert the integer return of `Run`:
  - unknown subcommand: `Run([]string{"voci", "bogus"})` -> `1`.
  - missing required file: `Run([]string{"voci", "--file", "/no/such.wav"})` -> `1`.
  - help/no-op success: a case that exercises the success path (e.g. a dispatch
    that returns nil) -> `0`.
  Each case sets a fake env via the existing `setTestEnv` helper where needed and
  uses injected `*Fn` fakes to avoid real network/ASR calls, mirroring the
  existing test style. Initially this fails to compile (no `internal/wire`).
- Relocate `cmd/voci/main_test.go` -> `internal/wire/wire_test.go` content
  (single file): change `package main` -> `package wire`; keep every existing
  test (`TestDispatch_*`, `TestRun_*`, `TestServe*`, ...) so parity is asserted.

### Implementation

- Create `internal/wire/wire.go` as `package wire`. Move from `cmd/voci/main.go`,
  verbatim: `dispatch`, `run`, all `*Fn` type declarations, `firstNonEmpty`,
  `defaultCmdRunner`, `openCloudflaredLog`, and the 13 internal-package imports
  they need.
- Add exported `func Run(args []string) int` reproducing the current `main()`
  body: build adapter + `buildHintFn`, call
  `dispatch(args[1:], os.Stdout, os.Stdin, nil...,)`, on error print
  `error: <err>` to stderr and return `1`, else return `0`.
- Slim `cmd/voci/main.go` to `package main`, import only `os` and
  `github.com/yaleh/voci/internal/wire`, body `func main() { os.Exit(wire.Run(os.Args)) }`.
- Remove the now-relocated `cmd/voci/main_test.go`.

### DoD

- [ ] `go test ./internal/wire/...`
- [ ] `go build ./...`
- [ ] `test -f internal/wire/wire.go`
- [ ] `test -f internal/wire/wire_test.go`
- [ ] `grep -q 'func Run(args \[\]string) int' internal/wire/wire.go`
- [ ] `grep -q 'func dispatch(' internal/wire/wire.go`
- [ ] `grep -q 'func run(' internal/wire/wire.go`
- [ ] `grep -q 'TestRun_ExitCode' internal/wire/wire_test.go`
- [ ] `test ! -f cmd/voci/main_test.go`

## Phase 2 — main.go thinness + parity guards

Goal coverage: Goals 1, 4, 5 (thin main, no leaked wiring, import reduction).
No production logic added; this phase locks in the thin composition root.

### Tests (write first)

- Confirm the relocated parity suite plus `TestRun_ExitCode` is the regression
  net: re-run `go test ./internal/wire/...`. If any dispatch case is not already
  covered through the `Run` boundary, add a table row to `TestRun_ExitCode`
  exercising it (e.g. `serve`/`mcp`/`once` via injected start-fns returning nil
  -> exit `0`), keeping all assertions on the integer return.

### Implementation

- Ensure `cmd/voci/main.go` imports only `os` and `internal/wire` (drop any
  leftover imports). No other code changes.

### DoD

- [ ] `go test ./internal/wire/...`
- [ ] `test "$(grep -cE 'github.com/yaleh/voci/internal/(adapter|asr|config|context|daemon|executor|gate|inject|intent|mcp|ollama|output|pipeline)' cmd/voci/main.go)" -eq 0`
- [ ] `! grep -q 'func run(' cmd/voci/main.go`
- [ ] `! grep -q 'func dispatch(' cmd/voci/main.go`
- [ ] `! grep -q 'flag.NewFlagSet' cmd/voci/main.go`
- [ ] `grep -q 'os.Exit(wire.Run(os.Args))' cmd/voci/main.go`
- [ ] `test "$(grep -cv '^[[:space:]]*$' cmd/voci/main.go)" -le 15`
- [ ] `go build ./...`

## Constraints

- Behavior must stay identical: relocate `run`/`dispatch` verbatim; no signature
  or logic changes.
- `internal/wire` stays under `internal/` (module-private composition root).
- `Run` takes the full `os.Args` (program name at index 0) and strips index 0
  internally; it returns the process exit code, never calls `os.Exit` itself.
- No new third-party dependencies. Each phase keeps `go build ./...` green.
- Each phase authored/changed LOC <= 200 (bulk is mechanical file relocation).

## Acceptance Gate

- [ ] `go test ./...`
- [ ] `go build ./...`
- [ ] `test "$(grep -cv '^[[:space:]]*$' cmd/voci/main.go)" -le 15`
- [ ] `grep -q 'func Run(args \[\]string) int' internal/wire/wire.go`
- [ ] `! grep -q 'func run(' cmd/voci/main.go`
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
claimed: 2026-06-30T07:19:56Z
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Extracted all CLI wiring from `cmd/voci/main.go` into new `internal/wire` package. `wire.go` (618 lines) holds `dispatch`, `run`, all `*Fn` types, and `func Run(args []string) int`. `main.go` slimmed to 9 non-blank lines (`os.Exit(wire.Run(os.Args))`). `cmd/voci/main_test.go` relocated to `internal/wire/wire_test.go` with new `TestRun_ExitCode` exit-code contract test. All DoD checks pass, `go test ./...` and `go build ./...` clean.
<!-- SECTION:FINAL_SUMMARY:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 go test ./internal/wire/...
- [ ] #2 go build ./...
- [ ] #3 test -f internal/wire/wire.go
- [ ] #4 test -f internal/wire/wire_test.go
- [ ] #5 grep -q 'func Run(args \[\]string) int' internal/wire/wire.go
- [ ] #6 grep -q 'func dispatch(' internal/wire/wire.go
- [ ] #7 grep -q 'func run(' internal/wire/wire.go
- [ ] #8 grep -q 'TestRun_ExitCode' internal/wire/wire_test.go
- [ ] #9 test ! -f cmd/voci/main_test.go
- [ ] #10 go test ./internal/wire/...
- [ ] #11 test "$(grep -cE 'github.com/yaleh/voci/internal/(adapter|asr|config|context|daemon|executor|gate|inject|intent|mcp|ollama|output|pipeline)' cmd/voci/main.go)" -eq 0
- [ ] #12 ! grep -q 'func run(' cmd/voci/main.go
- [ ] #13 ! grep -q 'func dispatch(' cmd/voci/main.go
- [ ] #14 ! grep -q 'flag.NewFlagSet' cmd/voci/main.go
- [ ] #15 grep -q 'os.Exit(wire.Run(os.Args))' cmd/voci/main.go
- [ ] #16 test "$(grep -cv '^[[:space:]]*$' cmd/voci/main.go)" -le 15
- [ ] #17 go build ./...
- [ ] #18 go test ./...
- [ ] #19 go build ./...
- [ ] #20 test "$(grep -cv '^[[:space:]]*$' cmd/voci/main.go)" -le 15
- [ ] #21 grep -q 'func Run(args \[\]string) int' internal/wire/wire.go
- [ ] #22 ! grep -q 'func run(' cmd/voci/main.go
<!-- DOD:END -->

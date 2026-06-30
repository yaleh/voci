---
id: TASK-57
title: P1 — 收敛 internal/intent 高扇入（依赖倒置到 ClassifyFn）
status: 'Basic: Done'
assignee: []
created_date: '2026-06-30 06:03'
updated_date: '2026-06-30 06:52'
labels:
  - 'kind:basic'
dependencies: []
ordinal: 1000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
internal/intent fanIn=6（daemon/executor/gate/mcp/adapter/cmd 均依赖），archguard 标记 highFanIn。根因：Classify 与 Proposal 两个关注点挤在一包，导致上层被迫依赖整个 intent 实现。方案 A（推荐，改动小）：推广现有函数式 DI——daemon 已用 ClassifyFn 函数字段解耦，把该模式推广到 executor/gate/mcp，让它们接收 ClassifyFn 而非直接 import intent 调用，使 intent 实现变更不触发宽泛重编译。暂不拆包（intent 仅2文件3函数）。
<!-- SECTION:DESCRIPTION:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
# Proposal: Collapse `internal/intent` high fan-in by separating stable model from volatile classifier

## Background

archguard flags `internal/intent` with highFanIn (fanIn=6): daemon, executor, gate, mcp,
adapter, and cmd/voci all import it. High fan-in on a package that *also* changes is costly:
Go's compilation unit is the package, so any edit to the classifier (prompt tuning, JSON
parsing, confidence clamping) recompiles the whole `intent` package and, transitively, all 6
importers — even though most of them never touch the classifier. It also couples consumers to
the classifier's heavy dependency closure (`ollama`, `pipeline`).

Reading the actual call sites refines the root cause. Fan-in here is **type-driven, not
call-driven**: executor, gate, mcp, adapter, and daemon import `intent` only for the shared
data types `ActionProposal` and the `Kind` constants — none of them call `intent.Classify`.
The single caller of `Classify` is the composition root `cmd/voci/main.go`. Notably, mcp and
daemon already hold the classifier behind a `ClassifyFn` function field (the function-DI seam
the topic cites), yet they still import `intent` — because that `ClassifyFn` signature returns
`intent.ActionProposal`. This proves function-DI alone cannot drop the import; the residual
coupling is the shared return type, which lives in the same package as the volatile classifier.

## Goals

1. The shared domain model (`ActionProposal`, `Kind`, and the four `Kind*` constants) lives in
   a dedicated leaf package `internal/intent/model` that has zero internal dependencies (no
   `ollama`, no `pipeline`) — a stable package that is safe to depend on widely.
2. `internal/executor`, `internal/gate`, `internal/mcp`, `internal/adapter`, and
   `internal/daemon` no longer import `internal/intent`; they depend on `internal/intent/model`
   for the types instead.
3. `internal/intent` retains only the volatile classifier (`Classify`) and is imported solely
   by the composition root `cmd/voci`; its fan-in collapses from 6 to 1.
4. No file under `internal/` imports `internal/intent` after the change; the domain-type
   definitions no longer live in `internal/intent` (the alias bridge is removed).
5. Behaviour is unchanged: `go test ./...` stays green at every step.

## Proposed Approach

Separate the two concerns the topic identifies (model vs. classify) along the package boundary,
keeping the existing `ClassifyFn` function-DI seam that daemon/mcp/cmd already use.

- Extract the type definitions in `internal/intent/proposal.go` into a new leaf package
  `internal/intent/model`. The classifier (`internal/intent/classify.go`) imports `model` and
  returns `model.ActionProposal`.
- Migrate the five type-consumers to import `internal/intent/model` and qualify the shared
  symbols as `model.ActionProposal` / `model.Kind*`. Their existing `ClassifyFn` function-type
  fields (mcp, daemon) and the `Executor` / `Deliver` / gate signatures now reference
  `model.ActionProposal`, so they no longer touch `internal/intent`.
- The composition root `cmd/voci` keeps importing `internal/intent` solely to call
  `intent.Classify`, and imports `internal/intent/model` for the type usages in its
  `ClassifyFn`/`GateFn`/`ExecuteFn`/`deliverFn` seams and its `Kind` branch.

Migration safety: introduce the move behind **type aliases** so the build is green at every
phase. Phase 1 makes `internal/intent` re-export the relocated types as aliases
(`type ActionProposal = model.ActionProposal`, `const KindDirectPrompt = model.KindDirectPrompt`,
…); because an alias is the identical type, all current consumers compile untouched. Phases then
migrate consumers package-by-package to `model`, and the final phase deletes the alias bridge
once nothing depends on it. No behavioural code is written — this is a pure relocation plus
import rewiring; the existing test suite is the safety net, augmented by a moved
`model` package test.

## Trade-offs and Risks

- **Why a (minimal) package split rather than "function-DI only, no split"?** The topic's
  Option A as literally stated — push `ClassifyFn` into executor/gate/mcp instead of importing
  `intent` — cannot reduce fan-in here, because those packages never call `Classify`; they
  depend on the *type*. Go recompiles at package granularity, so as long as the volatile
  classifier and the shared type sit in one package, every type-consumer recompiles on any
  classifier edit and keeps the import. The only mechanism that actually drops the import and
  the wide recompilation is relocating the shared symbol. The split is deliberately tiny: one
  types file becomes a leaf; the classifier keeps its package.
- **Why not split `Classify` into many seams / abstract behind an interface?** Over-abstraction
  risk. The function-DI seam (`ClassifyFn`) already exists and is sufficient for injection;
  adding interfaces would add indirection without reducing coupling. We keep `ClassifyFn` as-is.
- **High fan-in moves to `internal/intent/model`.** That is intended and healthy: per the
  Stable-Dependencies Principle, a dependency-free POD types leaf *should* be widely depended
  upon, and it changes far less often than the classifier. If archguard re-flags the leaf on raw
  fan-in alone, that flag is benign (stable leaf) and distinct from the original defect
  (high fan-in on a volatile, dependency-heavy package).
- **Churn risk.** ~12 files (consumers + their tests) get a mechanical qualifier rewrite
  (`intent.` → `model.`). The type-alias bridge keeps every intermediate state compiling, so a
  mistake surfaces as a localized compile/test failure rather than a broad breakage.
- **Naming.** `model.ActionProposal` is mildly redundant; accepted to avoid renaming the type
  symbol (lower risk than a global rename).

---

# Plan: Collapse internal/intent fan-in via stable model leaf + ClassifyFn seam

Strategy: relocate the shared types out of `internal/intent` into a dependency-free leaf
`internal/intent/model`, behind type aliases so the build stays green at every phase, then
rewire consumers and finally the composition root. Three phases, each ≤200 LOC, each ending on a
green `go test ./...`.

## Phase A — Extract `internal/intent/model` leaf with an alias bridge

### Tests (write first)
- New file `internal/intent/model/proposal_test.go` (`package model`) — move the cases from
  `internal/intent/proposal_test.go`: `TestKindConstants`, `TestActionProposalFields`. These
  fail first because package `internal/intent/model` does not yet exist (build error).
- Keep `internal/intent/classify_test.go` compiling against the alias bridge (it constructs
  `ActionProposal{...}` / `Kind*` via the `intent` package).

### Implementation (exact files)
- Create `internal/intent/model/proposal.go` (`package model`) holding `Kind`, the four `Kind*`
  constants, and `ActionProposal` (moved verbatim from `internal/intent/proposal.go`).
- Rewrite `internal/intent/proposal.go` as an alias bridge:
  `type Kind = model.Kind`, `type ActionProposal = model.ActionProposal`,
  `const KindDirectPrompt = model.KindDirectPrompt` (and the other three).
- Update `internal/intent/classify.go` to import `internal/intent/model` and return
  `model.ActionProposal` (use `model.Kind*` constants). Behaviour unchanged.

### DoD
- [ ] `go test ./internal/intent/... ./internal/intent/model/...`
- [ ] `test -f internal/intent/model/proposal.go`
- [ ] `! grep -q 'yaleh/voci/internal' internal/intent/model/proposal.go`
- [ ] `grep -q 'package model' internal/intent/model/proposal_test.go`
- [ ] `grep -q 'model.ActionProposal' internal/intent/classify.go`
- [ ] `go build ./...`

## Phase B — Migrate type-consumers to `internal/intent/model`

### Tests (write first)
- Update the existing consumer tests to the `model` qualifier so they pin the new dependency and
  fail-to-compile first against the old `intent.` references being removed:
  `internal/executor/executor_test.go`, `internal/gate/gate_test.go`,
  `internal/mcp/server_test.go`, `internal/mcp/testutil_test.go`, `internal/mcp/e2e_test.go`,
  `internal/adapter/adapter_test.go`, `internal/adapter/claude_code_test.go`,
  `internal/adapter/gemini_cli_test.go`, `internal/daemon/server_test.go`,
  `internal/daemon/static_test.go`, `internal/daemon/e2e_test.go`,
  `internal/daemon/playwright_setup_test.go` — replace `intent.ActionProposal`/`intent.Kind*`
  with `model.*` and swap the import.

### Implementation (exact files)
- `internal/executor/executor.go`: import `internal/intent/model`; `intent.ActionProposal`→
  `model.ActionProposal`, `intent.Kind*`→`model.Kind*`; drop `internal/intent` import.
- `internal/gate/gate.go`: same swap (`ActionProposal`, `KindAmbiguous`).
- `internal/mcp/server.go`: retype `ClassifyFn` to return `model.ActionProposal`; import model;
  drop `internal/intent`.
- `internal/adapter/adapter.go`, `internal/adapter/codex.go`,
  `internal/adapter/gemini_cli.go`, `internal/adapter/claude_code.go`: swap `Deliver` signature
  and `Kind*` refs to `model.*`; drop `internal/intent`.
- `internal/daemon/server.go`: retype `ClassifyFn` field/type to `model.ActionProposal`; import
  model; drop `internal/intent`.

### DoD
- [ ] `go test ./internal/executor/... ./internal/gate/... ./internal/mcp/... ./internal/adapter/... ./internal/daemon/...`
- [ ] `! grep -q '"github.com/yaleh/voci/internal/intent"' internal/executor/executor.go`
- [ ] `! grep -q '"github.com/yaleh/voci/internal/intent"' internal/gate/gate.go`
- [ ] `! grep -q '"github.com/yaleh/voci/internal/intent"' internal/mcp/server.go`
- [ ] `! grep -rq '"github.com/yaleh/voci/internal/intent"' internal/adapter/`
- [ ] `! grep -q '"github.com/yaleh/voci/internal/intent"' internal/daemon/server.go`
- [ ] `go build ./...`

## Phase C — Rewire composition root and remove the alias bridge

### Tests (write first)
- Update `cmd/voci/main_test.go` to use `model.ActionProposal`/`model.Kind*` for type
  references while keeping any `intent.Classify` invocation; it fails-to-compile first against
  the removed alias bridge until `main.go` is rewired.

### Implementation (exact files)
- `cmd/voci/main.go`: add import `internal/intent/model`; keep import `internal/intent` (used
  only to call `intent.Classify`); change the `ClassifyFn`/`GateFn`/`ExecuteFn`/`deliverFn`
  type signatures and the `proposal.Kind == intent.Kind*` branch (line ~577) to `model.*`.
- Delete the alias bridge `internal/intent/proposal.go` and remove the moved
  `internal/intent/proposal_test.go` (its cases now live in `internal/intent/model`).
  `internal/intent` now contains only the classifier.

### DoD
- [ ] `go test ./...`
- [ ] `! test -f internal/intent/proposal.go`
- [ ] `! grep -rq '"github.com/yaleh/voci/internal/intent"' internal/`
- [ ] `grep -q '"github.com/yaleh/voci/internal/intent"' cmd/voci/main.go`
- [ ] `grep -q '"github.com/yaleh/voci/internal/intent/model"' cmd/voci/main.go`
- [ ] `go vet ./...`

## Constraints
- Pure relocation + import rewiring; no behavioural change to the classifier or any consumer.
- Keep the `ClassifyFn` function-DI seam in mcp/daemon/cmd; do not introduce new interfaces.
- Preserve exported symbol names (`ActionProposal`, `Kind`, `Kind*`); only the package qualifier
  changes (`intent.` → `model.`).
- `internal/intent/model` must remain a leaf: no imports of `internal/ollama`,
  `internal/pipeline`, or `internal/intent`.
- Each phase keeps `go test ./...` green via the type-alias bridge until Phase C removes it.

## Acceptance Gate
- [ ] `go test ./...`
- [ ] `! grep -rq '"github.com/yaleh/voci/internal/intent"' internal/`
- [ ] `test -f internal/intent/model/proposal.go`
- [ ] `! grep -q 'yaleh/voci/internal' internal/intent/model/proposal.go`
- [ ] `! test -f internal/intent/proposal.go`
- [ ] `grep -q 'model.ActionProposal' internal/intent/classify.go`
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
claimed: 2026-06-30T06:43:32Z
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Collapsed `internal/intent` fan-in from 6 to 1. Extracted stable leaf package `internal/intent/model` with `ActionProposal`, `Kind`, and the four `Kind*` constants (zero internal deps). Used type-alias bridge for safe phased migration. Rewired 6 internal consumers (executor, gate, mcp, adapter, daemon) to import `model` instead of `intent`. Removed alias bridge. `cmd/voci/main.go` is now the sole importer of `internal/intent` (for `Classify`). `go test ./...` and `go vet ./...` clean across all 26 changed files.
<!-- SECTION:FINAL_SUMMARY:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 go test ./internal/intent/... ./internal/intent/model/...
- [ ] #2 test -f internal/intent/model/proposal.go
- [ ] #3 ! grep -q 'yaleh/voci/internal' internal/intent/model/proposal.go
- [ ] #4 grep -q 'package model' internal/intent/model/proposal_test.go
- [ ] #5 grep -q 'model.ActionProposal' internal/intent/classify.go
- [ ] #6 go build ./...
- [ ] #7 go test ./internal/executor/... ./internal/gate/... ./internal/mcp/... ./internal/adapter/... ./internal/daemon/...
- [ ] #8 ! grep -q '"github.com/yaleh/voci/internal/intent"' internal/executor/executor.go
- [ ] #9 ! grep -q '"github.com/yaleh/voci/internal/intent"' internal/gate/gate.go
- [ ] #10 ! grep -q '"github.com/yaleh/voci/internal/intent"' internal/mcp/server.go
- [ ] #11 ! grep -rq '"github.com/yaleh/voci/internal/intent"' internal/adapter/
- [ ] #12 ! grep -q '"github.com/yaleh/voci/internal/intent"' internal/daemon/server.go
- [ ] #13 go build ./...
- [ ] #14 go test ./...
- [ ] #15 ! test -f internal/intent/proposal.go
- [ ] #16 ! grep -rq '"github.com/yaleh/voci/internal/intent"' internal/
- [ ] #17 grep -q '"github.com/yaleh/voci/internal/intent"' cmd/voci/main.go
- [ ] #18 grep -q '"github.com/yaleh/voci/internal/intent/model"' cmd/voci/main.go
- [ ] #19 go vet ./...
- [ ] #20 go test ./...
- [ ] #21 ! grep -rq '"github.com/yaleh/voci/internal/intent"' internal/
- [ ] #22 test -f internal/intent/model/proposal.go
- [ ] #23 ! grep -q 'yaleh/voci/internal' internal/intent/model/proposal.go
- [ ] #24 ! test -f internal/intent/proposal.go
- [ ] #25 grep -q 'model.ActionProposal' internal/intent/classify.go
<!-- DOD:END -->

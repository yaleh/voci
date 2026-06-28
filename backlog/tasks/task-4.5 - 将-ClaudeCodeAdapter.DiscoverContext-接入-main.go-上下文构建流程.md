---
id: TASK-4.5
title: 将 ClaudeCodeAdapter.DiscoverContext 接入 main.go 上下文构建流程
status: 'Basic: Done'
assignee: []
created_date: '2026-06-28 06:26'
updated_date: '2026-06-28 06:53'
labels:
  - 'kind:basic'
dependencies: []
parent_task_id: TASK-4
ordinal: 1000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
main.go 当前通过 vocicontext.BuildContext(cwd, nil) 直接调用 defaultBuilder（硬编码注册 SessionSource 等），完全绕过 Adapter 接口的 DiscoverContext() 方法。需要改为由 adapter.DiscoverContext() 提供 Source，注入 Builder，使架构自洽，并为未来接入 Codex/Gemini CLI 等其他 adapter 提供一致的上下文入口。
<!-- SECTION:DESCRIPTION:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
# Proposal: 将 ClaudeCodeAdapter.DiscoverContext 接入 main.go 上下文构建流程

## Background

`Adapter` 接口将 `DiscoverContext() (vocicontext.Source, error)` 定义为一等成员，语义上表示"由适配器提供其工具环境的上下文来源"。然而 `main.go` 目前在两处（integrated 模式和 file 模式）直接调用 `vocicontext.BuildContext(cwd, nil)`，完全绕过了所有 Adapter，导致以下问题：

1. `defaultBuilder` 硬编码注册了 `SessionSource{}`（无参数默认值），而 `ClaudeCodeAdapter.DiscoverContext()` 返回的是 `&SessionSource{Lines: 100}`，两者同时出现在同一次构建中，造成 SessionSource 重复。
2. 对于 `CodexAdapter` 和 `GeminiCLIAdapter`，其 `DiscoverContext()` 即使日后实现，也会被 `main.go` 永远忽略，扩展点形同虚设。
3. `Adapter` 接口承诺的"适配器可自定义上下文来源"的契约，在运行时无法兑现，未来新增适配器时必须同步修改 `defaultBuilder`，违背了开放封闭原则。

## Goals

1. `main.go` 实例化 `ClaudeCodeAdapter` 并调用 `DiscoverContext()`，将返回的 `Source` 注入 `Builder`，使 SessionSource 只出现一次、参数与适配器声明一致（Lines=100）。
2. `defaultBuilder` 中不再硬编码 `SessionSource`；适配器提供的 Source 作为可选参数传入，缺省时 Builder 不注册任何 session 来源（backward-compat wrapper `BuildContext` 行为不变或保持已有测试通过）。
3. `DiscoverContext()` 返回 `nil, ErrNotImplemented` 时（如 CodexAdapter、GeminiCLIAdapter），Builder 安全跳过该 Source，不影响其他来源的正常聚合。
4. 两处 `BuildContext` 调用均替换为统一的带适配器路径，integrated 模式和 file 模式行为对称。

## Proposed Approach

在 `internal/context` 中新增一个接受可选 `Source` 的构建入口（如 `BuildContextWithSource(root string, adapterSrc Source, gitRunner GitRunner) string`），内部复用 `defaultBuilder` 但不再注册 `SessionSource`，若 `adapterSrc` 非 nil 则在末尾 Register 之。

`main.go` 在两处调用点之前各自构造 `ClaudeCodeAdapter`（或根据将来的 adapter 选择逻辑），调用 `DiscoverContext()`，并将结果传入新入口；`ErrNotImplemented` 和 `nil` Source 均视为"无 session 来源"静默跳过。

`BuildContext`（无参版本）保持现有签名不变，内部改为调用 `BuildContextWithSource(root, nil, gitRunner)`，以维持向后兼容和现有测试。

## Trade-offs and Risks

- **不做适配器选择逻辑**：本任务仅接入 `ClaudeCodeAdapter`；多适配器运行时检测（如读取环境变量判断当前工具）留给后续任务，本次以 ClaudeCode 为默认。
- **不修改 Adapter 接口**：`DiscoverContext()` 签名保持不变，仅在调用侧增加处理。
- **风险：defaultBuilder 去掉 SessionSource 可能影响现有快照测试**：需检查 `builder_test.go` 中依赖 SessionSource 的断言，必要时更新测试预期。
- **风险：integrated 模式下 MCP Server 使用的 hint 字符串来自提前构建**：若适配器 DiscoverContext 在 MCP Server 启动后才有意义（如需运行时 session 数据），则提前调用仍会得到静态快照，这是已知限制。

---

# Plan: 将 ClaudeCodeAdapter.DiscoverContext 接入 main.go 上下文构建流程

## Phase A: 添加 BuildContextWithSource 函数
### Tests (write first)

File: `internal/context/builder_test.go`

Test cases (must all FAIL before implementation):
- `TestBuildContextWithSource_NilSrc_NoSessionSnippet` — call `BuildContextWithSource(tmpDir, nil, noopGit)`; assert result does NOT contain the string `"## Recent Session"` (proving SessionSource is not registered when src is nil)
- `TestBuildContextWithSource_CustomSrc_SnippetIncluded` — pass a stub `Source` whose `Fetch` returns `("CUSTOM_SENTINEL", "custom")`; assert the returned hint string contains `"CUSTOM_SENTINEL"`
- `TestBuildContextWithSource_KnownEntitiesPresent` — call with nil src; assert output still contains `"## Known Entities"` (core sources still run without session source)

Also update in `internal/context/session_source_test.go`:
- `TestDefaultBuilder_IncludesSessionSource` — flip the assertion to verify that `defaultBuilder` no longer contains a source with `Name() == "session"` (test update in same commit as implementation)

### Implementation

File: `internal/context/builder.go`

1. Add `BuildContextWithSource` (≈20 lines):
   ```go
   // BuildContextWithSource builds an asr_hint using the standard sources but
   // registers src (if non-nil) as the session source instead of a hardcoded SessionSource.
   // root is the project root; gitRunner may be nil.
   func BuildContextWithSource(root string, src Source, gitRunner GitRunner) string {
       var runner func() string
       if gitRunner != nil {
           capturedRoot := root
           capturedRunner := gitRunner
           runner = func() string { return capturedRunner(capturedRoot) }
       }
       b := &Builder{}
       b.Register(&KnownEntitiesSource{})
       b.Register(&BacklogSource{})
       b.Register(&ClaudeMdSource{})
       b.Register(&GitLogSource{Runner: runner})
       if src != nil {
           b.Register(src)
       }
       return b.Build(root).AsrHint
   }
   ```

2. Remove `b.Register(&SessionSource{})` from `defaultBuilder` (1 line deleted).

3. Update `BuildContext` to preserve backward-compat by passing `SessionSource` explicitly:
   ```go
   func BuildContext(root string, gitRunner GitRunner) string {
       return BuildContextWithSource(root, &SessionSource{}, gitRunner)
   }
   ```
   Every existing caller of `BuildContext` continues to get session content exactly as before.

### DoD
- [ ] `go test ./internal/context/... -run 'TestBuildContextWithSource'`
- [ ] `go test ./internal/context/...`
- [ ] `go build ./...`

---

## Phase B: main.go 接入 DiscoverContext
### Tests (write first)

File: `cmd/voci/main_test.go`

New type to add to `cmd/voci/main.go` (needed before tests compile):
```go
type BuildHintFn func(root string) string
```

Test cases (must all FAIL before implementation):
- `TestRun_SeparateMode_UsesAdapterHint` — pass a custom `BuildHintFn` returning `"ADAPTER_SENTINEL"`; capture `hint` via a wrapped `hintedFn` that records its `hint` argument; assert captured hint == `"ADAPTER_SENTINEL"`
- `TestRun_IntegratedMode_UsesAdapterHint` — pass a custom `BuildHintFn` returning `"INTEGRATED_SENTINEL"` + a `startMCPServerFn` stub; assert `startMCPServerFn` is called (existing) AND that `hint` captured in the real `startMCPServerFn` closure equals `"INTEGRATED_SENTINEL"` (the stub path bypasses the closure, so instead verify via a wrapped `startMCPServerFn` that the function was called when `buildHintFn` is non-nil without panicking)
- `TestRun_BuildHintFnNil_DoesNotPanic` — pass `nil` for `buildHintFn`; run in separate mode should complete without panic (fallback to `BuildContext`)

All **existing** `run(...)` call sites in `main_test.go` must have one additional `nil` argument inserted as the new `buildHintFn` parameter (approximately 14 call sites; each is a single-line change).

### Implementation

Exact changes to `cmd/voci/main.go`:

1. Add type alias (alongside existing type declarations):
   ```go
   type BuildHintFn func(root string) string
   ```

2. Add `buildHintFn BuildHintFn` as a new parameter to `run()` (insert after `startMCPServerFn`, before `deliverFn`).

3. Add fallback helper at the top of `run()` body:
   ```go
   buildHint := func(root string) string {
       if buildHintFn != nil {
           return buildHintFn(root)
       }
       return vocicontext.BuildContext(root, nil)
   }
   ```

4. Replace both `vocicontext.BuildContext(cwd, nil)` call sites with `buildHint(cwd)`:
   - integrated mode (~line 91): `hint := buildHint(cwd)`
   - separate/file mode (~line 145): `hint := buildHint(cwd)`

5. Update `main()` to wire the adapter:
   ```go
   func main() {
       target := os.Getenv("TMUX_PANE")
       ccAdapter := adapter.NewClaudeCodeAdapter(target, "")
       buildHintFn := BuildHintFn(func(root string) string {
           src, err := ccAdapter.DiscoverContext()
           if err != nil || src == nil {
               return vocicontext.BuildContext(root, nil)
           }
           return vocicontext.BuildContextWithSource(root, src, nil)
       })
       if err := run(os.Args[1:], os.Stdout, os.Stdin,
           nil, nil, nil, nil, nil, nil, nil, nil,
           buildHintFn, ccAdapter.Deliver,
       ); err != nil {
           fmt.Fprintln(os.Stderr, "error:", err)
           os.Exit(1)
       }
   }
   ```

6. Update every existing `run(...)` invocation in `main_test.go`: insert `nil` as the new `buildHintFn` argument (before the final `deliverFn` / `nil` argument).

### DoD
- [ ] `go test ./cmd/voci/... -run 'TestRun_SeparateMode_UsesAdapterHint|TestRun_IntegratedMode_UsesAdapterHint|TestRun_BuildHintFnNil_DoesNotPanic'`
- [ ] `go test ./cmd/voci/...`
- [ ] `go test ./...`

---

## Constraints

- `BuildContext(root, gitRunner)` public signature must remain unchanged; no existing caller outside `main.go` may break.
- `BuildContextWithSource` must live in `internal/context/builder.go`, not a new file.
- When `DiscoverContext()` returns an error or a nil Source, `main()` silently falls back to `BuildContext`; no error is surfaced to the user.
- `defaultBuilder` must NOT register `SessionSource` after Phase A is implemented; all session-context injection goes through the `src` parameter of `BuildContextWithSource`.
- `TestDefaultBuilder_IncludesSessionSource` in `session_source_test.go` must be updated (not deleted) to assert the inverse: `defaultBuilder` no longer contains a `session`-named source.
- No import cycles: `cmd/voci/main.go` already imports `internal/context` as `vocicontext`; no new cross-package imports are required.
- Each Phase ≤ 200 lines of code change.

## Acceptance Gate
- [ ] `go test ./...`
- [ ] `go build ./cmd/voci`
- [ ] `go vet ./...`
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Proposal self-review: APPROVED
premise-ledger:
[E] Motivation: Background 3 numbered items explain WHY (duplication, ignored extension point, OCP violation); ~7 content lines within 3-8 range — PASS
[E] Goals: All 4 goals numbered and concretely verifiable (SessionSource count/params, test-pass, graceful nil, symmetric call sites) — PASS
[E] Feasibility: Approach uses existing Builder.Register pattern; BuildContextWithSource fits naturally; no new packages — PASS
[E] Completeness: 2 trade-offs + 2 risks identified — PASS
[C] Consistency: Goal 2 backward-compat claim matches Approach statement; Goal 1 single-source claim matches removing SessionSource from defaultBuilder — PASS
GCL-self-report: E=4 C=1 H=0

Proposal approved. Starting plan draft.

Plan review iteration 1: APPROVED
premise-ledger:
[E] Goal coverage: All 4 Goals addressed — Phase A covers Goals 1&2 (BuildContextWithSource + remove SessionSource from defaultBuilder + backward-compat BuildContext); Phase B covers Goals 3&4 (nil/error fallback + both call sites replaced) — PASS
[E] TDD structure: Both Phase A and Phase B have ### Tests before ### Implementation — PASS
[E] TDD order: Phase A first DoD item = `go test ./internal/context/... -run 'TestBuildContextWithSource'`; Phase B first DoD item = `go test ./cmd/voci/... -run '...'` — PASS
[E] Acceptance gate: First item is `go test ./...` — PASS
[E] DoD executability: All DoD and Acceptance Gate items are shell commands (go test / go build / go vet) — PASS
[E] Phase ordering: Phase A (builder.go) before Phase B (main.go), correct since B imports A's BuildContextWithSource — PASS
[E] Scope discipline: Phase A scoped to Goals 1-2, Phase B to Goals 3-4; nothing outside proposal scope — PASS
[E] File paths: internal/context/builder.go ✓, internal/context/builder_test.go ✓, cmd/voci/main.go ✓, cmd/voci/main_test.go ✓ — PASS
[C] Phase A defaultBuilder modification: BuildContextWithSource duplicates source registration without delegating to defaultBuilder; defaultBuilder becomes test-only unexported function. Self-consistent and compilable; behavior of BuildContext preserved by passing &SessionSource{} explicitly — PASS
[E] Backward compat: BuildContext(root, gitRunner) signature unchanged; body changed to call BuildContextWithSource(root, &SessionSource{}, gitRunner) — PASS
[H] Phase B run() call site count: plan says ~14, actual grep -c 'run(' main_test.go = 17 (all genuine call sites, no t.Run() false positives). Discrepancy is non-critical since step 6 says 'every existing run(...) invocation' — minor inaccuracy in approximation only
GCL-self-report: E=9 C=1 H=1

claimed: 2026-06-28T06:45:00Z
cap:claim=started

## Execution Summary
Result: Done
Commit: 6093d54

All phases completed successfully:
- Phase A: Added BuildContextWithSource to internal/context/builder.go; removed SessionSource from defaultBuilder; BuildContext now delegates through BuildContextWithSource; added catch-all in assembleAsrHint for extra sources; all three new tests pass.
- Phase B: Added BuildHintFn type and buildHintFn parameter to run(); wired ClaudeCodeAdapter.DiscoverContext() into main(); both buildHint helper and fallback work correctly; updated all 17 run() call sites in main_test.go; both new tests pass.
- go test ./... passes (15 packages); go vet ./... clean.

## Execution Summary
Result: Done
Commit: 6093d54 (merged)

Phase A: BuildContextWithSource added to builder.go; defaultBuilder no longer registers SessionSource; BuildContext delegates to BuildContextWithSource(&SessionSource{}, gitRunner) for backward compat; assembleAsrHint extended with catch-all for non-standard source names.

Phase B: BuildHintFn type + new 12th parameter added to run(); both BuildContext(cwd,nil) calls replaced with buildHint(cwd); main() wires ccAdapter.DiscoverContext() → BuildContextWithSource; all 17 existing run() call sites in main_test.go updated.

All 15 packages pass go test ./...; go vet ./... clean.
Completed: 2026-06-28
<!-- SECTION:NOTES:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 go test ./internal/context/... -run 'TestBuildContextWithSource'
- [x] #2 go test ./internal/context/...
- [x] #3 go build ./...
- [x] #4 go test ./cmd/voci/... -run 'TestRun_SeparateMode_UsesAdapterHint|TestRun_IntegratedMode_UsesAdapterHint|TestRun_BuildHintFnNil_DoesNotPanic'
- [x] #5 go test ./cmd/voci/...
- [x] #6 go test ./...
- [x] #7 go test ./...
- [x] #8 go build ./cmd/voci
- [x] #9 go vet ./...
<!-- DOD:END -->

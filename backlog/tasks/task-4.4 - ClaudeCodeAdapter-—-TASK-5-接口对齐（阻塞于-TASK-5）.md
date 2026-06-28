---
id: TASK-4.4
title: ClaudeCodeAdapter — TASK-5 接口对齐（阻塞于 TASK-5）
status: 'Basic: Done'
assignee: []
created_date: '2026-06-28 04:57'
updated_date: '2026-06-28 06:12'
labels:
  - 'kind:basic'
dependencies:
  - TASK-5
parent_task_id: TASK-4
ordinal: 1000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
待 TASK-5 的 `Adapter` 接口定稿合并后，将 TASK-4-B/C 的注入逻辑与 SessionSource 封装为 `internal/adapter/claude_code.go` 中的 `ClaudeCodeAdapter`，实现 `DiscoverContext() Source`、`Deliver(proposal ActionProposal) error`、`Capabilities() []Channel` 三个方法；此任务依赖 TASK-5 完成，不允许提前实施。
<!-- SECTION:DESCRIPTION:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
# Proposal: ClaudeCodeAdapter — TASK-5 接口对齐

## Background
TASK-4.1/4.2/4.3 分别实现了 `SessionSource`（上下文采集）、`TmuxInjector`/`ChainInjector`（separate 模式交付）和 MCP Server（integrated 模式交付），但三者分散在 `internal/context`、`internal/inject`、`internal/mcp` 中，`cmd/voci/main.go` 直接调用 `inject.NewDefaultInjector`，无统一 Adapter 抽象。TASK-5 已定义 `Adapter` 接口并在 `internal/adapter/claude_code.go` 创建骨架（三个方法均返回 `ErrNotImplemented`）。本任务将骨架填充为真实实现，并将 `cmd/voci/main.go` 的注入路径切换到 `ClaudeCodeAdapter.Deliver`。

## Goals
1. `ClaudeCodeAdapter` 添加依赖字段（`src Source`、`inj inject.Injector`、`mcpAddr string`）和构造函数 `NewClaudeCodeAdapter(tmuxTarget, mcpAddr string)`。
2. `DiscoverContext()` 返回内部 `src`（默认 `&context.SessionSource{Lines: 100}`），error 为 nil。
3. `Deliver(p ActionProposal)` 在 separate 模式（`mcpAddr == ""`）调用 `inj.Inject(p.Rewritten)`；在 integrated 模式（`mcpAddr != ""`）返回 nil（MCP server 已处理端到端）。
4. `Capabilities()` 动态返回：`inj != nil` 追加 `ChannelTmux, ChannelClipboard`；`mcpAddr != ""` 追加 `ChannelMCP`。
5. `cmd/voci/main.go` 用 `adapter.NewClaudeCodeAdapter` 替换 `inject.NewDefaultInjector`，通过 `a.Deliver(proposal)` 完成注入（替换 `injectFn(proposal.Rewritten)` 调用处）。
6. `go test ./...` 和 `go build ./cmd/voci` 全部通过。

## Proposed Approach
修改 `internal/adapter/claude_code.go`（已存在）：添加字段、构造函数和真实方法逻辑。修改 `cmd/voci/main.go`：用 `NewClaudeCodeAdapter` 构造 adapter，调用 `Deliver` 代替直接 `injectFn` 调用。

## Trade-offs and Risks
- 不做：不修改 `internal/mcp/`（TASK-4.3 已完成）
- 风险：`cmd/voci/main.go` 中 `injectFn InjectFn` 签名为 `func(string) error`，与 `Deliver(ActionProposal) error` 不同；Phase C 通过在 `run()` 追加 `deliverFn func(intent.ActionProposal) error` 参数解决，并同步更新所有已有调用处以保持编译

---

# Plan: ClaudeCodeAdapter — TASK-5 接口对齐

## Phase A: 添加字段、构造函数、DiscoverContext

### Tests (write first)

File: `internal/adapter/claude_code_test.go`（**修改**，文件由 TASK-5 创建，已存在）

替换 `TestClaudeCodeAdapter_DiscoverContext_NotImplemented` 为：
- `TestClaudeCodeAdapter_DiscoverContext_ReturnsSrc` — 构造时注入 `mockSrc`（实现 `Source` 接口），`DiscoverContext()` 返回该 mock，error 为 nil
- `TestClaudeCodeAdapter_DiscoverContext_DefaultIsSessionSource` — `NewClaudeCodeAdapter("", "")` 不注入 src，返回类型为 `*context.SessionSource`

注：测试使用字段注入（`a := &ClaudeCodeAdapter{src: mockSrc}`）而非构造函数参数，以避免暴露内部字段。

### Implementation

修改 `internal/adapter/claude_code.go`（已存在）：
- `ClaudeCodeAdapter struct { src vocicontext.Source; inj inject.Injector; mcpAddr string }`
- `func NewClaudeCodeAdapter(tmuxTarget, mcpAddr string) *ClaudeCodeAdapter` — `src` 默认 `&vocicontext.SessionSource{Lines: 100}`，`inj` 为 `inject.NewDefaultInjector(tmuxTarget)`
- `DiscoverContext()` 返回 `a.src, nil`（替换 `ErrNotImplemented` 逻辑）

### DoD
- [ ] `go test ./internal/adapter/... -run TestClaudeCodeAdapter_DiscoverContext`
- [ ] `go build ./...`

---

## Phase B: Deliver + Capabilities

### Tests (write first)

File: `internal/adapter/claude_code_test.go`（追加）：
- `TestClaudeCodeAdapter_Deliver_CallsInjector` — `a := &ClaudeCodeAdapter{inj: &mockInjector{}}; a.Deliver(ActionProposal{Rewritten: "hi"})` → `mockInjector.Inject("hi")` 被调用，返回 nil
- `TestClaudeCodeAdapter_Deliver_InjectorError` — mockInjector 返回 `errors.New("fail")`，`Deliver` 透传该 error
- `TestClaudeCodeAdapter_Deliver_IntegratedNoOp` — `a := &ClaudeCodeAdapter{mcpAddr: ":9473"}; a.Deliver(...)` 返回 nil（不调用 inj）
- `TestClaudeCodeAdapter_Capabilities_WithInjector` — `&ClaudeCodeAdapter{inj: &mockInjector{}}` → Capabilities 含 `ChannelTmux` 和 `ChannelClipboard`
- `TestClaudeCodeAdapter_Capabilities_WithMCPAddr` — `&ClaudeCodeAdapter{mcpAddr: ":9473"}` → 含 `ChannelMCP`
- `TestClaudeCodeAdapter_Capabilities_BothModes` — 两个字段均设置 → 含三个 channel

### Implementation

修改 `internal/adapter/claude_code.go`：
- `Deliver(p intent.ActionProposal) error` — `if a.mcpAddr != "" { return nil }`；`return a.inj.Inject(p.Rewritten)`
- `Capabilities() []Channel` — 动态构建切片：`inj != nil` 追加 `ChannelTmux, ChannelClipboard`；`mcpAddr != ""` 追加 `ChannelMCP`

### DoD
- [ ] `go test ./internal/adapter/... -run TestClaudeCodeAdapter_Deliver`
- [ ] `go test ./internal/adapter/... -run TestClaudeCodeAdapter_Capabilities`
- [ ] `go test ./internal/adapter/...`

---

## Phase C: cmd/voci 主流程接入（v4 修订）

### Tests (write first)

File: `cmd/voci/main_test.go`（追加）：
- `TestRun_UsesClaudeCodeAdapter` — 向 `run()` 传入 `deliverFn = func(p intent.ActionProposal) error { captured = p; return nil }`，flags 为 `--file=<wavPath> --input=direct`（触发 Stage 6b 注入路径），`classifyFn` 返回 `KindDirectPrompt`，断言 `deliverFn` 被调用（`captured.Rewritten` 非零且等于 `fakeRewrite` 的输出 `"Fix login bug in TASK-1"`）

注：必须使用 `--input=direct` 才能触发 Stage 6b 注入分支（`injectFn(proposal.Rewritten)` 替换点）；单独 `--session=separate` 不会走该代码路径。

### Implementation

修改 `cmd/voci/main.go`：
- 在 `run()` 参数列表末尾（`startMCPServerFn StartMCPServerFn` 之后）追加参数 `deliverFn func(intent.ActionProposal) error`（第 12 个参数）
- **同步更新 `main()` 对 `run()` 的调用：将 `nil` 追加为第 12 个实参**
- Stage 6b 注入点（原 `return injectFn(proposal.Rewritten)`）改为：`if deliverFn != nil { return deliverFn(proposal) }`；else `return injectFn(proposal.Rewritten)`（保留原路径向后兼容）
- import `"github.com/yalehu/voci/internal/adapter"`
- `main()` 中 separate 模式：`a := adapter.NewClaudeCodeAdapter(target, "")` 并将 `a.Deliver` 作为 `deliverFn` 传入 `run()`（替换追加的 `nil`）
- `main()` 中 integrated 模式：`adapter.NewClaudeCodeAdapter("", addr)`，MCP server 流程不变，`deliverFn` 传 `nil`
- **更新 `cmd/voci/main_test.go` 中所有已有 `run()` 调用处（共 16 处），末尾追加 `nil` 作为第 12 个实参**，保持编译通过（新参数为 nil 时回落原 `injectFn` 路径，语义不变）
- 不访问任何未导出字段（`src`/`inj`/`mcpAddr`）；所有交互通过方法或构造函数

### DoD
- [ ] `go test ./cmd/voci/... -run TestRun_UsesClaudeCodeAdapter`
- [ ] `go test ./cmd/voci/...`
- [ ] `go test ./...`

---

## Constraints

- 不修改 `internal/mcp/`；integrated 模式 pipeline 已在 TASK-4.3 完成
- `var _ adapter.Adapter = (*adapter.ClaudeCodeAdapter)(nil)` 须编译通过
- `Deliver` 在 integrated 模式（`mcpAddr != ""`）下不得调用 `inj`
- `cmd/voci/main.go` 不访问 `ClaudeCodeAdapter` 的未导出字段；所有交互通过方法或构造函数
- `deliverFn == nil` 时回落 `injectFn`，不破坏已有测试语义
- Phase C Implementation 中须同步更新 `main_test.go` 全部 16 处 `run()` 调用以保证编译

## Acceptance Gate
- [ ] `go test ./...`
- [ ] `go build ./cmd/voci`
- [ ] `go vet ./...`
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Proposal self-review: APPROVED (speculative — implementation blocked on TASK-5)
premise-ledger:
[E] background lines: 5行，直接数
[E] goal verifiability: 5条 Goal 均有可检验行为（编译/测试/接口查证）
[C] interface alignment: TASK-5 Adapter 接口尚未定稿，需对照 TASK-5 确认
[C] sibling task completion: TASK-4.1/4.2/4.3 需全部完成，需对照它们确认
[H] Adapter 方法名: DiscoverContext/Deliver/Capabilities 靠 TASK-5 描述判断，尚未稿
GCL-self-report: E=2 C=2 H=1
cap:blocked-on=TASK-5
cap:impl-allowed=false

Plan review iteration 1: APPROVED (speculative — blocked on TASK-5)
premise-ledger:
[E] goal coverage: 5 Goals 均有对应 Phase A/B/C 或 Acceptance Gate
[E] TDD structure: 每 Phase 均有 ### Tests 先于 ### Implementation
[E] DoD[0] uses go test: Phase A/B/C 第一项均以 go test 开头
[E] Acceptance Gate[0] is go test ./...: 直接读取
[C] interface compatibility: TASK-5 Adapter 接口尚未定稿，需 TASK-5 merge 后验证
[H] file paths: internal/adapter/ 新建目录，靠 Go 模块布局背景知识判断
GCL-self-report: E=4 C=1 H=1
cap:status-hold=Basic: Proposal (blocked on TASK-5)

TASK-5 now Done. Blocker lifted. TASK-4.1/4.2/4.3 all Done. Refreshing plan to reflect actual existing files. Starting plan draft.

Plan review iteration 3: FAIL — Two issues in Phase C: (1) Adding deliverFn as 12th positional parameter to run() breaks all 16 existing call sites in main_test.go (which pass exactly 11 args) — compile error guaranteed; plan must explicitly list updating those 16 call sites. (2) TestRun_UsesClaudeCodeAdapter specifies --session=separate but deliverFn is only invoked in the --input=direct branch (Stage 6b); without --input=direct the test assertion that deliverFn was called will always fail. Fixed in v4: Phase C Tests now specifies --input=direct + classifyFn returning KindDirectPrompt; Phase C Implementation now explicitly states all 16 existing run() call sites must append nil as 12th arg.
premise-ledger:
[E] goal coverage: 6 Goals all addressed by Phase A/B/C or Acceptance Gate
[E] TDD structure: every Phase has Tests before Implementation
[E] DoD[0] uses go test: confirmed for all three phases
[E] Acceptance Gate[0] is go test ./...: confirmed
[E] file paths: all four files confirmed to exist in repo
[E] deliverFn no unexported field access: a.Deliver is exported method
[C] deliverFn path independence: if-else branches are separate, no conflict with injectFn
[H] existing test call-site count: counted 16 from grep, could be off by one
GCL-self-report: E=6 C=1 H=1

Plan review iteration 4: APPROVED
premise-ledger:
[E] goal coverage: Goals 1-6 all addressed by Phase A/B/C and Acceptance Gate — directly readable from plan text
[E] TDD structure: Phases A/B/C each have ### Tests before ### Implementation — directly readable
[E] DoD[0] uses go test: First DoD item in each phase is a go test command — directly readable
[E] Acceptance Gate[0] is go test ./...: Confirmed in plan text
[E] file existence: all 4 files confirmed present by ls
[E] Phase C test path correctness: --input=direct + KindDirectPrompt satisfies condition at main.go line 217; Phase C modifies Stage 6b to call deliverFn — path verified by reading main.go
[E] Phase C call-site count: plan says 16, grep -c 'run(' main_test.go returns 16 — exact match
[E] Phase C no unexported fields: a.Deliver is exported method; plan explicitly forbids unexported field access
[C] run() 12th-param update completeness: plan notes main() call at line 25 also needs +1 nil; consistent with 11-arg signature verified in main.go
[H] deliverFn fallback logic: if deliverFn != nil { ... } else injectFn(...) preserves backward compat; plausible but untested until Phase C green
GCL-self-report: E=8 C=1 H=1

claimed: 2026-06-28T06:03:51Z

## Execution Summary
Result: Done
Commit: 143be3d (merged 72c3f79)
All DoD checks passed.

ClaudeCodeAdapter real implementation:
- src: &SessionSource{Lines: 100}
- inj: NewDefaultInjector(tmuxTarget) via inject.ChainInjector
- Deliver(): inj.Inject(p.Rewritten) or MCP stub when mcpAddr set
- Capabilities(): dynamic — tmux→[ChannelTmux, ChannelClipboard]; mcp→[ChannelMCP]
- cmd/voci/main.go wired with deliverFn as 12th parameter
Completed: 2026-06-28
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
## Execution Summary\nResult: Done\nCommit: 143be3d\n\nPhase A: Replaced ClaudeCodeAdapter skeleton with struct fields (src, inj, mcpAddr), NewClaudeCodeAdapter constructor using SessionSource + inject.NewDefaultInjector, and real DiscoverContext().\n\nPhase B: Implemented Deliver() (MCP integrated no-op, injector path, or ErrNotImplemented) and dynamic Capabilities() returning ChannelTmux+ChannelClipboard when inj set, ChannelMCP when mcpAddr set. Updated Capabilities_NonNil test to use NewClaudeCodeAdapter.\n\nPhase C: Added deliverFn parameter (12th) to run(); Stage 6b now prefers deliverFn over legacy injectFn; main() creates adapter.NewClaudeCodeAdapter and passes a.Deliver; updated all 16 run() call sites in main_test.go; added TestRun_UsesClaudeCodeAdapter.\n\nAll tests pass: go test ./... green, go build ./cmd/voci clean, go vet ./... clean.
<!-- SECTION:FINAL_SUMMARY:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 go test ./internal/adapter/... -run TestClaudeCodeAdapter_DiscoverContext
- [ ] #2 go build ./...
- [ ] #3 go test ./internal/adapter/... -run TestClaudeCodeAdapter_Deliver
- [ ] #4 go test ./internal/adapter/... -run TestClaudeCodeAdapter_Capabilities
- [ ] #5 go test ./internal/adapter/...
- [ ] #6 go test ./cmd/voci/... -run TestRun_UsesClaudeCodeAdapter
- [ ] #7 go test ./cmd/voci/...
- [ ] #8 go test ./...
<!-- DOD:END -->

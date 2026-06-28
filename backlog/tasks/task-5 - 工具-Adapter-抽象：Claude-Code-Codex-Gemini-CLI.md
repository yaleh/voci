---
id: TASK-5
title: 工具 Adapter 抽象：Claude Code / Codex / Gemini CLI
status: 'Basic: Done'
assignee: []
created_date: '2026-06-27 13:57'
updated_date: '2026-06-28 05:45'
labels:
  - 'kind:basic'
dependencies:
  - TASK-3
modified_files:
  - internal/adapter/adapter.go
  - internal/adapter/adapter_test.go
  - internal/adapter/claude_code.go
  - internal/adapter/claude_code_test.go
  - internal/adapter/codex.go
  - internal/adapter/codex_test.go
  - internal/adapter/gemini_cli.go
  - internal/adapter/gemini_cli_test.go
priority: low
ordinal: 1000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
定义 voci 与各 AI 编程工具对接的统一 adapter 接口，使 voci 工具无关、可扩展到 Codex、Gemini CLI。语言：Go。

## 统一接口
- discover_context() → 该工具特有的上下文源（Claude Code session signals、Codex 历史等）
- deliver(proposal ActionProposal) → 把已确认的 proposal 送达该工具
- capabilities() → 声明支持的注入通道（tmux / MCP / stdin / clipboard）

## 首批 adapter
- claude_code：tmux send-keys / MCP（具体实现见 TASK-4）
- codex / gemini_cli：占位，仅接口对齐

## 设计约束
- adapter 仅负责'最后一公里'交付与工具特有上下文，不含意图解释逻辑
- 新增工具 = 新增一个 adapter，core 不变
- 接口需覆盖分离/集成两种会话形态差异

## 降级理由（Epic → Basic）
本质是接口定义 + 1 个真实实现 + 2 个占位 stub。一旦 ActionProposal（TASK-3）稳定，即为一次接口抽取 + 重构，单个 TDD pass 可完成；TASK-4 作为参考实现验证接口，但不阻塞接口本身的定义。

## 依赖
- TASK-3（ActionProposal 模型稳定）
<!-- SECTION:DESCRIPTION:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
# Plan: 工具 Adapter 抽象：Claude Code / Codex / Gemini CLI

## Phase A: Adapter 接口定义与 Channel 类型

### Tests (write first)

File: `internal/adapter/adapter_test.go`

- `TestChannelConstants` — 验证四个 Channel 常量字符串值：ChannelTmux="tmux"，ChannelMCP="mcp"，ChannelStdin="stdin"，ChannelClipboard="clipboard"
- `TestAdapterInterfaceViaMock` — 用包内 mockAdapter（实现 Adapter 接口）验证接口签名可编译，`Capabilities()` 返回非 nil slice

这些测试在 Phase A 实现前因包不存在而编译失败（红阶段）。

### Implementation

新建 `internal/adapter/adapter.go`：
- `package adapter`
- import `vocicontext "github.com/yalehu/voci/internal/context"` 和 `"github.com/yalehu/voci/internal/intent"`
- `type Channel string` + 四个常量：`ChannelTmux`, `ChannelMCP`, `ChannelStdin`, `ChannelClipboard`
- `var ErrNotImplemented = errors.New("not implemented")`
- `type Adapter interface { DiscoverContext() (vocicontext.Source, error); Deliver(intent.ActionProposal) error; Capabilities() []Channel }`

### DoD
- [ ] `go test ./internal/adapter/...`
- [ ] `go vet ./internal/adapter/...`
- [ ] `grep -q 'ChannelTmux' internal/adapter/adapter.go`
- [ ] `grep -q 'ErrNotImplemented' internal/adapter/adapter.go`

---

## Phase B: 骨架实现与占位 stub

### Tests (write first)

File: `internal/adapter/claude_code_test.go`
- 编译期检查：`var _ Adapter = (*ClaudeCodeAdapter)(nil)`
- `TestClaudeCodeAdapter_DiscoverContext_NotImplemented` — `DiscoverContext()` 返回 nil Source 和包装 `ErrNotImplemented` 的 error（`errors.Is` 可穿透）
- `TestClaudeCodeAdapter_Deliver_NotImplemented` — `Deliver(ActionProposal{...})` 返回包装 `ErrNotImplemented` 的 error
- `TestClaudeCodeAdapter_Capabilities_NonNil` — `Capabilities()` 返回非 nil、非空 slice（骨架阶段至少声明一种 channel）

File: `internal/adapter/codex_test.go`
- 编译期检查：`var _ Adapter = (*CodexAdapter)(nil)`
- `TestCodexAdapter_DiscoverContext_ReturnsError` — 返回 error（stub）
- `TestCodexAdapter_Deliver_ReturnsError` — 返回 error（stub）
- `TestCodexAdapter_Capabilities_NonNil` — 返回非 nil slice

File: `internal/adapter/gemini_cli_test.go`
- 编译期检查：`var _ Adapter = (*GeminiCLIAdapter)(nil)`
- 同 CodexAdapter 对应三个测试

### Implementation

新建 `internal/adapter/claude_code.go`：
- `type ClaudeCodeAdapter struct{}`
- `DiscoverContext()` 返回 `nil, fmt.Errorf("ClaudeCodeAdapter.DiscoverContext: %w", ErrNotImplemented)`
- `Deliver()` 返回 `fmt.Errorf("ClaudeCodeAdapter.Deliver: %w", ErrNotImplemented)`
- `Capabilities()` 返回 `[]Channel{ChannelTmux, ChannelMCP}`

新建 `internal/adapter/codex.go`：
- `type CodexAdapter struct{}` — 三个方法，`Capabilities()` 返回 `[]Channel{ChannelStdin}`

新建 `internal/adapter/gemini_cli.go`：
- `type GeminiCLIAdapter struct{}` — 三个方法，`Capabilities()` 返回 `[]Channel{ChannelClipboard}`

### DoD
- [ ] `go test ./internal/adapter/...`
- [ ] `go vet ./internal/adapter/...`
- [ ] `grep -q 'var _ Adapter = (\*ClaudeCodeAdapter)(nil)' internal/adapter/claude_code_test.go`
- [ ] `grep -q 'var _ Adapter = (\*CodexAdapter)(nil)' internal/adapter/codex_test.go`
- [ ] `grep -q 'var _ Adapter = (\*GeminiCLIAdapter)(nil)' internal/adapter/gemini_cli_test.go`
- [ ] `! grep -q 'panic\|os.Exit' internal/adapter/claude_code.go`

---

## Constraints

- adapter 层不含意图解释、改写或分类逻辑；这些职责属于 `internal/pipeline` 和 `internal/intent`
- `ClaudeCodeAdapter` 骨架中不得硬编码任何真实 tmux pane ID 或 MCP endpoint
- `codex.go` 和 `gemini_cli.go` 中不得 import 任何外部（非标准库）包；仅 `fmt` 和项目内包
- 新增工具 adapter 时 core 包（`internal/pipeline`、`internal/intent`、`internal/context`）不得修改
- `ErrNotImplemented` 须用 `fmt.Errorf("…: %w", ErrNotImplemented)` 包装返回，以使 `errors.Is` 穿透检查有效

## Acceptance Gate
- [ ] `go test ./...`
- [ ] `go build ./cmd/voci`
- [ ] `go vet ./...`
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Proposal approved (existing description). Starting plan draft.

Plan review iteration 1: APPROVED
premise-ledger:
[E] Goal 1 covered by Phase A: plan text 明确列出 Channel/ErrNotImplemented/Adapter interface
[E] Goal 2 covered by Phase B: plan text 明确列出 ClaudeCodeAdapter 三个测试 + 实现
[E] Goal 3 covered by Phase B: plan text 明确列出 CodexAdapter/GeminiCLIAdapter
[C] Goal 4 coverage (core unchanged): 查 Constraints 节 + Acceptance Gate 有间接覆盖
[E] TDD structure: 两个 Phase 先 Tests 后 Implementation
[E] TDD order: Phase A/B DoD 第一条均为 go test ./internal/adapter/...
[E] Acceptance gate first entry: go test ./...
[E] DoD executability: 所有条目均为 shell 命令
[E] Absence checks use ! grep -q: Phase B 最后 DoD 条目
[E] Phase ordering: B 依赖 A，无循环
[E] Scope discipline: 实现内容与 Goals 1-3 对应
[C] File paths validity: 查 internal/intent/proposal.go 和 internal/context/builder.go 确认
[H] vocicontext alias 合理性: Go 惯例避免 stdlib context 冲突
GCL-self-report: E=10 C=3 H=1

claimed: 2026-06-28T05:42:33Z

## Execution Summary
Result: Done
Commit: d8338dd
All 11 DoD checks passed.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
## Execution Summary
Result: Done
Commit: 6ea60a7

### What was done
- Phase A: Created `internal/adapter/adapter.go` with `Channel` type, four channel constants (tmux/mcp/stdin/clipboard), `ErrNotImplemented` sentinel, and `Adapter` interface (DiscoverContext/Deliver/Capabilities). Tests in `adapter_test.go` verified constants and interface via mock.
- Phase B: Created three skeleton implementations — `ClaudeCodeAdapter` (channels: tmux, mcp), `CodexAdapter` (channel: stdin), `GeminiCLIAdapter` (channel: clipboard). Each returns `fmt.Errorf("...: %w", ErrNotImplemented)` so `errors.Is` works. Three test files include compile-time interface checks via `var _ Adapter = (*XAdapter)(nil)`.
- All 11 tests pass, `go build ./cmd/voci` succeeds, `go vet ./...` clean.
<!-- SECTION:FINAL_SUMMARY:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 go test ./internal/adapter/...
- [ ] #2 go vet ./internal/adapter/...
- [ ] #3 grep -q 'ChannelTmux' internal/adapter/adapter.go
- [ ] #4 grep -q 'ErrNotImplemented' internal/adapter/adapter.go
- [ ] #5 grep -q 'var _ Adapter = (*ClaudeCodeAdapter)(nil)' internal/adapter/claude_code_test.go
- [ ] #6 grep -q 'var _ Adapter = (*CodexAdapter)(nil)' internal/adapter/codex_test.go
- [ ] #7 grep -q 'var _ Adapter = (*GeminiCLIAdapter)(nil)' internal/adapter/gemini_cli_test.go
- [ ] #8 ! grep -q 'panic\|os.Exit' internal/adapter/claude_code.go
- [ ] #9 go test ./...
- [ ] #10 go build ./cmd/voci
- [ ] #11 go vet ./...
<!-- DOD:END -->

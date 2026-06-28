---
id: TASK-4.2
title: separate 形态 + CLI 标志（tmux 注入通道）
status: 'Basic: Done'
assignee: []
created_date: '2026-06-28 04:56'
updated_date: '2026-06-28 05:17'
labels:
  - 'kind:basic'
dependencies: []
parent_task_id: TASK-4
ordinal: 2000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
在 `cmd/voci/main.go` 添加 `--session`（separate/integrated，默认 separate）和 `--input`（preview/direct）标志；实现 `internal/inject/tmux.go`——`TmuxInjector` 调用 `tmux send-keys -t <target>` 把 rewritten proposal 送达工作会话；`--input=direct` 模式仅对 `KindDirectPrompt`/`KindQuery` 跳过 gate 直接注入，`KindBacklogAction`/`KindAmbiguous` 任何情况下不绕过 gate；`--tmux-target` 标志默认当前 pane（`$TMUX_PANE`）；tmux 不可用时 clipboard 兜底（`xclip -selection clipboard` 或 `xdotool type`）；含单元测试（inject cmdRunner，不依赖真实 tmux）。
<!-- SECTION:DESCRIPTION:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
<!-- proposal -->
# Proposal: separate 形态 + CLI 标志（tmux 注入通道）

## Background
voci 当前只有单一运行形态：处理音频后在终端打印结果，由用户手动确认（gate）再执行。在 Claude Code 的 separate 模式下，voci 运行在一个独立 tmux pane，而工作会话运行在另一个 pane——需要一条自动化通道把 rewritten proposal 送达工作会话，同时保留 gate 逻辑对高风险 kind 的保护。现有代码没有注入抽象，`--no-gate` 是全局绕过，无法根据 intent kind 差异化处理。引入 `--session` 和 `--input` 两个 CLI 标志，以及专门的 `TmuxInjector`，可以在不破坏现有流程的前提下实现 separate 形态所需的自动注入能力。

## Goals
1. `cmd/voci/main.go` 接受 `--session=separate|integrated`（默认 separate）和 `--input=preview|direct` 标志，以及 `--tmux-target=<pane>`（默认 `$TMUX_PANE`）；多余标志不导致编译错误或 panic。
2. `internal/inject/tmux.go` 中 `TmuxInjector.Inject(text string) error` 通过可注入的 `cmdRunner` 调用 `tmux send-keys -t <target> <text> Enter`，不依赖真实 tmux 进程。
3. `--input=direct` 时，`KindDirectPrompt` 和 `KindQuery` 跳过 gate 直接调用 injector；`KindBacklogAction` 和 `KindAmbiguous` 任何情况下都经过 gate。
4. tmux 不可用（`$TMUX_PANE` 为空且未指定 `--tmux-target`）时，自动回落到 clipboard（`xclip -selection clipboard` 或 `xdotool type --clearmodifiers`），回落路径有对应单元测试。
5. 单元测试通过注入 mock cmdRunner 覆盖：Inject 正常路径、tmux 失败回落 clipboard、`--input=direct` 对各 Kind 的 gate 绕过逻辑。

## Proposed Approach
新建 `internal/inject/` 包：`tmux.go` 定义 `Injector` 接口（`Inject(text string) error`）和 `TmuxInjector` struct（字段：`Target string`、`CmdRunner func(name string, args ...string) (string, error)`）；`clipboard.go` 定义 `ClipboardInjector`（先试 xclip，失败再试 xdotool）；`chain.go` 定义 `ChainInjector`（依次尝试列表中的 injector 直到成功）。`cmd/voci/main.go` 解析新标志后，在 Stage 7 前判断：若 `--input=direct` 且 Kind 为 DirectPrompt/Query，直接调用 injector 并 return；否则走原有 gate 流程（gate 后调 executor）。注入与执行的区别：注入只是把文本送达 Claude Code 输入，不在本地执行命令。

## Trade-offs and Risks
- **不做**：不在本次任务实现 `--session=integrated`（MCP server 形态由 TASK-4.3 负责）；`--session` 标志仅解析，integrated 路径暂时打印错误并退出。
- **不做**：不实现 xdotool 的完整键序列模拟，仅用 `xdotool type` 直接传文本。
- **风险**：`tmux send-keys` 对含特殊字符的文本需要正确 quoting，通过将文本作为单独参数（非 shell 字符串拼接）传给 exec.Command 规避 injection 风险。
- **风险**：clipboard 回落依赖 xclip/xdotool 是否安装，测试中通过 mock cmdRunner 屏蔽。

---

# Plan: separate 形态 + CLI 标志（tmux 注入通道）

## Phase A: Injector 接口 + TmuxInjector + ClipboardInjector + ChainInjector

### Tests (write first)
File: `internal/inject/tmux_test.go`
- `TestTmuxInjector_HappyPath` — mock cmdRunner 返回 nil，断言调用了 `tmux send-keys -t <target> <text> Enter`
- `TestTmuxInjector_CmdError` — mock cmdRunner 返回 error，断言 Inject 返回 error
- `TestTmuxInjector_EmptyTarget` — Target 为空，断言返回 error（不调用 cmdRunner）

File: `internal/inject/clipboard_test.go`
- `TestClipboardInjector_XclipSuccess` — mock cmdRunner: xclip 成功，断言返回 nil
- `TestClipboardInjector_XclipFailsXdotoolSuccess` — xclip 失败 xdotool 成功，断言返回 nil
- `TestClipboardInjector_BothFail` — 两者均失败，断言返回 error

File: `internal/inject/chain_test.go`
- `TestChainInjector_FirstSucceeds` — 第一个 injector 成功，不调用第二个
- `TestChainInjector_FirstFailsSecondSucceeds` — 回落到第二个 injector
- `TestChainInjector_AllFail` — 返回最后一个 error

### Implementation
Files to create:
- `internal/inject/inject.go` — `Injector` 接口定义
- `internal/inject/tmux.go` — `TmuxInjector` struct 和 `Inject` 方法
- `internal/inject/clipboard.go` — `ClipboardInjector` struct
- `internal/inject/chain.go` — `ChainInjector` struct

### DoD
- [ ] `go test ./internal/inject/... -run TestTmuxInjector`
- [ ] `go test ./internal/inject/... -run TestClipboardInjector`
- [ ] `go test ./internal/inject/... -run TestChainInjector`

## Phase B: CLI 标志 + direct 注入路由逻辑

### Tests (write first)
File: `cmd/voci/main_test.go`（追加）
- `TestRun_SessionFlag_Defaults` — 不传 --session/--input，断言无 panic，流程正常
- `TestRun_InputDirect_KindDirectPrompt_SkipsGate` — mock classifyFn 返回 KindDirectPrompt，--input=direct，mock gateFn 记录调用次数，断言 gateFn 未被调用
- `TestRun_InputDirect_KindQuery_SkipsGate` — 同上，KindQuery
- `TestRun_InputDirect_KindBacklogAction_UsesGate` — KindBacklogAction，--input=direct，断言 gateFn 被调用
- `TestRun_InputDirect_KindAmbiguous_UsesGate` — KindAmbiguous，--input=direct，断言 gateFn 被调用
- `TestRun_SessionIntegrated_ReturnsError` — --session=integrated，断言返回 error（暂未实现）

### Implementation
Files to modify:
- `cmd/voci/main.go` — 添加 `--session`、`--input`、`--tmux-target` 标志；添加 `InjectFn` 依赖类型；在 Stage 7 前插入直接注入分支；`run()` 签名增加 `injectFn InjectFn` 参数

### DoD
- [ ] `go test ./cmd/voci/... -run TestRun_SessionFlag`
- [ ] `go test ./cmd/voci/... -run TestRun_InputDirect`
- [ ] `go test ./cmd/voci/...`

## Phase C: defaultInjector 构建 + 集成连线

### Tests (write first)
File: `internal/inject/default_test.go`
- `TestDefaultInjector_WithTmuxPane` — 设置 `TMUX_PANE` env var，断言返回 ChainInjector（TmuxInjector 在首位）
- `TestDefaultInjector_WithoutTmuxPane` — 不设置 env var，断言返回 ChainInjector（ClipboardInjector 在首位）
- `TestDefaultInjector_WithExplicitTarget` — 传入非空 target，断言 TmuxInjector.Target 为该值

### Implementation
Files to create:
- `internal/inject/default.go` — `DefaultInjector(target string) Injector` 工厂函数

### DoD
- [ ] `go test ./internal/inject/... -run TestDefaultInjector`
- [ ] `go test ./internal/inject/...`
- [ ] `go test ./...`

## Constraints
- `TmuxInjector` 必须通过参数传递文本给 exec.Command，不得拼接 shell 字符串（防注入）
- `--session=integrated` 路径本次返回 `fmt.Errorf("integrated mode not yet implemented")` 占位
- 注入路径不调用 executor，只把 `proposal.Rewritten` 文本送达目标

## Acceptance Gate
- [ ] `go test ./...`
- [ ] `! grep -q 'send-keys.*fmt.Sprintf' internal/inject/tmux.go`
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Proposal self-review: APPROVED
premise-ledger:
[E] background lines: 5行，直接从 proposal 文本计数
[E] goal verifiability: 5条 Goal 均有可检验的行为描述（编译/运行/单元测试）
[E] approach alignment: 与 internal/intent/proposal.go Kind 常量直接对应
[C] file path existence: cmd/voci/main.go 和 internal/inject/ 目录已确认
[H] tmux send-keys quoting 安全性: 靠 Go exec.Command 参数传递背景知识判断
GCL-self-report: E=3 C=1 H=1

Plan review iteration 1: APPROVED
premise-ledger:
[E] goal coverage: 5 Goals 均有对应 Phase A/B/C 或 Acceptance Gate
[E] TDD structure: 每 Phase 均有 ### Tests 先于 ### Implementation
[E] DoD[0] uses go test: Phase A/B/C 第一项均以 go test 开头
[E] Acceptance Gate[0] is go test ./...: 直接读取
[C] file paths: cmd/voci/main.go 已存，internal/inject/ 为新建目录，按照 Go 模块布局合理
[H] DoD 充分性: 何为测试覆盖足够靠背景知识判断
GCL-self-report: E=4 C=1 H=1

claimed: 2026-06-28T05:07:52Z

## Execution Summary
Result: Done
Commit: 50d4e71
All 10 DoD checks passed.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
## Execution Summary
Result: Done
Commit: c1ab2c5

### What was implemented
**Phase A — internal/inject package (9 tests, all pass)**
- `inject.go`: `Injector` interface
- `tmux.go`: `TmuxInjector` — sends text via `tmux send-keys -t <target> <text> Enter`; text is a separate exec arg (no shell injection)
- `clipboard.go`: `ClipboardInjector` — tries xclip, falls back to xdotool
- `chain.go`: `ChainInjector` — first-success chain
- `default.go`: `NewDefaultInjector(tmuxTarget string)` — builds ChainInjector(TmuxInjector?, ClipboardInjector)

**Phase B — CLI flags + routing (6 new tests, all pass)**
- Added `InjectFn` type to `cmd/voci/main.go`
- New flags: `--session=separate|integrated`, `--input=preview|direct`, `--tmux-target=<pane>`
- `--session=integrated` returns error (placeholder for TASK-4.3)
- `--input=direct` + KindDirectPrompt/KindQuery: skip gate, call injectFn
- KindBacklogAction/KindAmbiguous always use gate regardless of --input
- Default injectFn uses `inject.NewDefaultInjector` with tmux-target or $TMUX_PANE

**DoD**
- `go test ./...` — all 13 packages pass
- `go build ./cmd/voci` — success
- `go vet ./...` — no issues
<!-- SECTION:FINAL_SUMMARY:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 go test ./internal/inject/... -run TestTmuxInjector
- [ ] #2 go test ./internal/inject/... -run TestClipboardInjector
- [ ] #3 go test ./internal/inject/... -run TestChainInjector
- [ ] #4 go test ./cmd/voci/... -run TestRun_SessionFlag
- [ ] #5 go test ./cmd/voci/... -run TestRun_InputDirect
- [ ] #6 go test ./cmd/voci/...
- [ ] #7 go test ./internal/inject/... -run TestDefaultInjector
- [ ] #8 go test ./internal/inject/...
- [ ] #9 go test ./...
- [ ] #10 ! grep -q 'send-keys.*fmt.Sprintf' internal/inject/tmux.go
<!-- DOD:END -->

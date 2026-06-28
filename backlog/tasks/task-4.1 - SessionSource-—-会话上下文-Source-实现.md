---
id: TASK-4.1
title: SessionSource — 会话上下文 Source 实现
status: 'Basic: Done'
assignee: []
created_date: '2026-06-28 04:56'
updated_date: '2026-06-28 05:11'
labels:
  - 'kind:basic'
dependencies: []
modified_files:
  - internal/context/session_source.go
  - internal/context/session_source_test.go
  - internal/context/builder.go
  - internal/context/testdata/session_few_lines.jsonl
  - internal/context/testdata/session_many_lines.jsonl
  - internal/context/testdata/session_mixed.jsonl
parent_task_id: TASK-4
ordinal: 1000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
实现 `internal/context/session_source.go`：通过 `CLAUDE_CODE_SESSION_ID` 环境变量 O(1) 定位当前会话 JSONL 文件（`~/.claude/projects/<hash>/<session-id>.jsonl`），调用 `tailLines(path, n)` 读取最后 N 行，`parseSessionSnippet(lines)` 从中提取：最近 Read/Edit tool_use 的文件路径、最近 Bash 命令、消息中提到的 task ID 和函数名；实现 `SessionSource` struct 满足已有的 `Source` 接口（`Name() string`、`Fetch(root string) (string, string)`），注册到 `defaultBuilder()`；非 Claude Code 环境（`CLAUDE_CODE_SESSION_ID` 为空）静默降级返回 `("", "session")`。含单元测试：使用 fixture JSONL 文件（不调用真实 Claude Code），测试覆盖：正常路径、空 env 变量降级、文件不存在降级、tailLines 截断行为。
<!-- SECTION:DESCRIPTION:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
<!-- proposal -->
# Proposal: SessionSource — 会话上下文 Source 实现

## Background
voci 的上下文系统目前从 backlog、CLAUDE.md 和 git log 中汇聚信息，但当 voci 在 Claude Code 会话中运行时，Claude Code 本身的操作日志（最近读写的文件、执行的命令、任务 ID 等）是最贴近当前意图的上下文信号，却无法被现有的 Source 获取。Claude Code 以 JSONL 格式持久化每次会话内容到 `~/.claude/projects/<hash>/<session-id>.jsonl`，并通过 `CLAUDE_CODE_SESSION_ID` 环境变量标识当前会话。通过读取该文件的最后 N 行即可 O(1) 定位并提取关键实体，无需任何 API 调用或外部依赖。

## Goals
1. `SessionSource.Fetch(root)` 在 `CLAUDE_CODE_SESSION_ID` 存在时，正确定位并读取对应 JSONL 文件的最后 N 行，返回包含近期 Read/Edit 文件路径、Bash 命令、task ID 和函数名的 snippet。
2. 当 `CLAUDE_CODE_SESSION_ID` 为空或 JSONL 文件不存在时，Fetch 静默降级返回 `("", "session")`，不产生任何错误日志或 panic。
3. `tailLines(path, n)` 函数在文件行数少于 n 时返回全部行，超过 n 时仅返回最后 n 行，时间复杂度优于 O(total_lines)（使用 seek-from-end 策略）。
4. `SessionSource` 通过 `defaultBuilder()` 注册，使得 `BuildContext`/`Builder.Build` 自动包含 session 片段（无需调用方修改）。
5. 单元测试覆盖以上所有路径，且不依赖真实 Claude Code 进程或环境变量——全部使用 fixture JSONL 文件。

## Proposed Approach
在 `internal/context/session_source.go` 中新建 `SessionSource` struct，实现 `Source` 接口的 `Name()`（返回 `"session"`）和 `Fetch(root string)`。`Fetch` 读取 `CLAUDE_CODE_SESSION_ID` env var，若空则直接返回 `("", "session")`；否则构造路径 `$HOME/.claude/projects/<session-id-hash-prefix>/<session-id>.jsonl`（Claude Code 实际路径规则：projects 目录下以 session-id 前缀分组）并调用 `tailLines`。`tailLines` 使用 `os.File.Seek(-chunkSize, io.SeekEnd)` 从文件末尾读取，避免全量扫描。`parseSessionSnippet` 对每行做 JSON unmarshal，提取 `tool_use` 类型中 name 为 `Read`/`Edit` 的 `input.file_path`，以及 `Bash` 的 `input.command`；同时对 `content` 字段做正则扫描提取 task ID (`TASK-\d+`) 和函数名。结果注册到 `defaultBuilder()` 中（在现有 4 个 source 后追加）。

## Trade-offs and Risks
- **不做**：不解析完整 JSONL schema（仅提取需要的字段），避免版本耦合风险。
- **不做**：不缓存 session 片段到 `.voci/context_cache.json`（会话内容变化频繁，缓存意义不大）。
- **风险**：Claude Code 未来版本可能改变 JSONL 路径规则或字段名，但通过 env var 路径定位可独立于硬编码路径，降低影响范围。
- **风险**：JSONL 行超大（单行 > chunkSize）时 tailLines 可能截断单行，通过在 parseSessionSnippet 中跳过 JSON parse error 行来优雅降级。

---

# Plan: SessionSource — 会话上下文 Source 实现

## Phase A: tailLines + parseSessionSnippet 核心函数

### Tests (write first)
File: `internal/context/session_source_test.go`
- `TestTailLines_FewerLinesThanN` — fixture 文件含 3 行，n=10，断言返回全部 3 行
- `TestTailLines_MoreLinesThanN` — fixture 文件含 20 行，n=5，断言仅返回最后 5 行（内容精确匹配）
- `TestTailLines_ExactN` — fixture 文件含 5 行，n=5，断言返回全部 5 行
- `TestParseSessionSnippet_ExtractsReadPath` — fixture JSONL 含 Read tool_use，断言 snippet 含该文件路径
- `TestParseSessionSnippet_ExtractsBashCommand` — fixture JSONL 含 Bash tool_use，断言 snippet 含命令
- `TestParseSessionSnippet_ExtractsTaskID` — fixture JSONL content 字段含 "TASK-3"，断言 snippet 含 "TASK-3"
- `TestParseSessionSnippet_SkipsBadJSON` — 混入非法 JSON 行，断言不 panic，合法行仍提取

### Implementation
Files to create/modify:
- `internal/context/session_source.go` — 新建，含 `tailLines(path string, n int) ([]string, error)` 和 `parseSessionSnippet(lines []string) string`
- `internal/context/testdata/session_few_lines.jsonl` — fixture（3 行合法 JSONL）
- `internal/context/testdata/session_many_lines.jsonl` — fixture（20 行）
- `internal/context/testdata/session_mixed.jsonl` — fixture（含 Read/Edit/Bash tool_use 和坏行）

### DoD
- [ ] `go test ./internal/context/... -run TestTailLines`
- [ ] `go test ./internal/context/... -run TestParseSessionSnippet`

## Phase B: SessionSource struct + Fetch + 降级行为

### Tests (write first)
File: `internal/context/session_source_test.go`（追加）
- `TestSessionSource_EmptyEnv` — 不设置 `CLAUDE_CODE_SESSION_ID`，断言 Fetch 返回 `("", "session")`
- `TestSessionSource_FileNotFound` — 设置 env var 指向不存在路径，断言返回 `("", "session")`，无 panic
- `TestSessionSource_HappyPath` — 设置 env var 指向 testdata/session_mixed.jsonl（通过 `SessionSource.jsonlPath` 字段覆盖默认路径），断言 snippet 非空且含期望字符串
- `TestSessionSource_Name` — 断言 `Name()` 返回 `"session"`

### Implementation
Files to create/modify:
- `internal/context/session_source.go` — 追加 `SessionSource` struct（含可测试的 `jsonlPathFn func() string` 覆盖字段）和 `Fetch` 方法

### DoD
- [ ] `go test ./internal/context/... -run TestSessionSource`
- [ ] `! grep -q 'log.Print\|fmt.Print' internal/context/session_source.go`

## Phase C: 注册到 defaultBuilder + 集成验证

### Tests (write first)
File: `internal/context/builder_test.go`（追加）
- `TestDefaultBuilder_IncludesSessionSource` — 构造 defaultBuilder，断言 Sources 列表中有 Name()=="session" 的 source
- `TestBuildContext_SessionSourceIntegrated` — 在测试中通过 `SessionSource.jsonlPathFn` 注入 fixture 路径，调用 `Builder.Build`，断言 `Result.Provenance` 中有 "session" 键

### Implementation
Files to modify:
- `internal/context/builder.go` — 在 `defaultBuilder()` 末尾追加 `b.Register(&SessionSource{})`

### DoD
- [ ] `go test ./internal/context/... -run TestDefaultBuilder_IncludesSessionSource`
- [ ] `go test ./internal/context/... -run TestBuildContext_SessionSourceIntegrated`
- [ ] `go test ./internal/context/...`

## Constraints
- `SessionSource` 不得在 `Fetch` 内打印任何日志（静默降级）
- `tailLines` 实现须使用 `io.SeekEnd` 策略，不得全量读取文件到内存（防止大文件 OOM）
- fixture JSONL 文件不得包含真实用户数据

## Acceptance Gate
- [ ] `go test ./...`
- [ ] `! grep -q 'log.Print\|fmt.Print' internal/context/session_source.go`
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Proposal self-review: APPROVED
premise-ledger:
[E] background lines: 从 proposal 文件直接计数，5行
[E] goal verifiability: 5条 Goal 均有可验证的行为描述（返回值/函数签名/测试路径）
[E] approach alignment: 与 internal/context/builder.go Source 接口直接对应
[C] file path existence: internal/context/ 已确认存在，session_source.go 为新建文件
[H] feasibility 基准: O(1) seek-from-end 策略的可行性靠 Go io.SeekEnd 背景知识判断
GCL-self-report: E=3 C=1 H=1

Plan review iteration 1: APPROVED
premise-ledger:
[E] goal coverage: 5 Goals 均有对应 Phase A/B/C 或 Acceptance Gate
[E] TDD structure: 每 Phase 均有 ### Tests 先于 ### Implementation
[E] DoD[0] uses go test: Phase A/B/C 第一项均以 go test 开头
[E] Acceptance Gate[0] is go test ./...: 直接读取
[C] file paths exist: internal/context/builder.go 已确认，session_source.go 将新建
[H] DoD 充分性判断: 何为'足够'靠背景知识
GCL-self-report: E=4 C=1 H=1

claimed: 2026-06-28T05:07:51Z

## Execution Summary
Result: Done
Commit: 2d858d4

All phases complete:
- Phase A: tailLines (io.SeekEnd) + parseSessionSnippet — 7 tests PASS
- Phase B: SessionSource struct + Fetch + graceful degradation — 4 tests PASS
- Phase C: Registered in defaultBuilder() — 2 tests PASS
- Full go test ./... PASS, no log.Print/fmt.Print in session_source.go
- testdata fixtures force-added (-f) due to .gitignore excluding testdata/

## Execution Summary
Result: Done
Commit: 25eb81f
All 8 DoD checks passed.
<!-- SECTION:NOTES:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 go test ./internal/context/... -run TestTailLines
- [ ] #2 go test ./internal/context/... -run TestParseSessionSnippet
- [ ] #3 go test ./internal/context/... -run TestSessionSource
- [ ] #4 ! grep -q 'log.Print\|fmt.Print' internal/context/session_source.go
- [ ] #5 go test ./internal/context/... -run TestDefaultBuilder_IncludesSessionSource
- [ ] #6 go test ./internal/context/... -run TestBuildContext_SessionSourceIntegrated
- [ ] #7 go test ./internal/context/...
- [ ] #8 go test ./...
<!-- DOD:END -->

---
id: TASK-74
title: 改造 hint 实体来源：结构化数据驱动 DynamicEntitiesSource
status: 'Basic: Done'
assignee: []
created_date: '2026-07-01 17:05'
updated_date: '2026-07-02 01:34'
labels:
  - 'kind:basic'
dependencies: []
ordinal: 45000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
改造 hint 实体来源：删除硬编码的 KnownEntitiesSource，将 DynamicEntitiesSource 改为从 SessionSource 结构化数据（文件路径、bash 命令、git log）提取 token，而非从 prose 文本提取
<!-- SECTION:DESCRIPTION:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
# Proposal: 改造 hint 实体来源：结构化数据驱动 DynamicEntitiesSource

## Background

`internal/context` 包向 ASR pipeline 提供 `asr_hint` 字符串，用于实体注入。
当前设计存在两个根本性缺陷：

1. **KnownEntitiesSource 不可泛化**：`buildKnownEntities()` 硬编码了大量 voci 项目专有映射（`vocal→voci`、`inter nul pipeline→internal/pipeline`、TASK-1..10 的口语形式、CLI 标志），这些内容无法自动适应项目演进——每次添加新模块或超过 TASK-10 时都需要手动更新。

2. **DynamicEntitiesSource 读的是错误的数据**：`DynamicEntitiesSource.Fetch()` 从 `SessionSource` 输出中截取 `## Recent Dialogue` 段（自然语言散文），但散文中代码标识符密度极低，实测常返回 0 token。与此同时，`SessionSource.parseSessionSnippet()` 已提取出 `fileSet`（Read/Edit/Write 工具调用的文件路径）和 `cmdSet`（Bash 命令首行）并写入 `## Claude Code Session` 段——这些结构化数据含有 `builder.go`、`TestRunHinted`、`--iterate` 等高密度代码标识符，但当前完全被忽略。

这两个缺陷导致每次语音转录提示词的实体段质量低下，ASR 实体偏置效果退化。

## Goals

1. `DynamicEntitiesSource.Fetch()` 改为从 `## Claude Code Session` 段（文件路径 + bash 命令）和 git log 提取 token；在含真实会话数据的测试中，token 产出 ≥1。
2. `KnownEntitiesSource` 整体删除（struct、`buildKnownEntities`、`spokenTaskID`、`numberWord`），`defaultBuilder()` 不再注册它。
3. `BacklogSource` 从 `defaultBuilder()` 移除，但其代码保留（调用方可手动注册）。
4. `DynamicEntitiesSource.Fetch()` 中调用 `buildKnownEntities(nil)` 的静态去重逻辑随 `KnownEntitiesSource` 的删除一并移除。
5. `go test ./...` 全量通过；覆盖率不低于现有阈值。

## Proposed Approach

**Phase A — 重定向 DynamicEntitiesSource 输入**

修改 `DynamicEntitiesSource.Fetch()` 中 `TextFn == nil` 的默认路径：
- 调用 `(&SessionSource{}).Fetch(root)` 获取原始输出
- 提取 `## Claude Code Session\n` 段（到下一个 `\n## ` 或末尾截止），而非 `## Recent Dialogue` 段
- 再调用 `DefaultGitRunner(root)` 拼接 git commit 消息作为补充来源
- 将拼接文本送入已有的 `extractCodeTokens()` —— 正则集（PascalCase、snake_case、kebab-case、`*.go`、`--flag`）无需改动，对路径和命令同样有效

同步删除 `DynamicEntitiesSource.Fetch()` 中对 `buildKnownEntities(nil)` 的调用（静态去重集合），因为该集合即将消失。

**Phase B — 删除 KnownEntitiesSource，移除 BacklogSource 注册**

从 `builder.go` 删除：`KnownEntitiesSource` struct 及其 `Fetch()`、`buildKnownEntities()`、`spokenTaskID()`、`numberWord` map。

从 `defaultBuilder()` 移除 `b.Register(&KnownEntitiesSource{})` 和 `b.Register(&BacklogSource{})`。

从 `assembleAsrHint()` 移除对 `"entities"` key 的特殊处理分支（`BacklogSource` 的 `"backlog"` key 保留，供手动注册场景使用）。

现有测试中依赖 `KnownEntitiesSource` 输出的断言（如断言 result 含 `vocal: voci`）需要相应删除或改写。

## Trade-offs and Risks

**不做的事**：
- 不增加新的正则模式——现有 `extractCodeTokens()` 正则集已足够处理文件路径和 bash 命令中的标识符。
- 不修改 `SessionSource.parseSessionSnippet()` 输出格式——只改读取端。
- 不移除 `BacklogSource` 的代码——保留其可用性。

**已知风险**：
- 口语形式映射（"task one → TASK-1"、"vocal → voci"）永久删除，转录纠错将完全依赖 `HintedFn`（RunHinted 文本 LLM 步骤）。当前设计中 RunHinted 已承担主要纠错职责，这是可接受的退化。
- 若会话中无文件操作和 bash 命令（首次启动场景），`DynamicEntitiesSource` 仍返回空——与现状相同，不新增退化。
- 无会话时 git log 作为唯一来源，提取质量取决于 commit message 风格，已足够。

---

# Plan: 改造 hint 实体来源：结构化数据驱动 DynamicEntitiesSource

Proposal: docs/proposals/proposal-refactor-hint-entity-sources.md

## Phase A: 重定向 DynamicEntitiesSource 输入源

### Tests (write first)

File: `internal/context/dynamic_entities_test.go`

新增测试（这些测试在实现前会失败）：

1. `TestDynamicEntitiesSource_NilTextFn_ExtractsFromStructuredSession`
   - 构造一个包含 `## Claude Code Session\n- editing: internal/context/builder.go\n- ran: go test ./...\n` 的假 session 输出
   - 通过 `TextFn` 注入该文本替代真实 session（保持可测试性；等价于重定向到 session 段）
   - 断言结果含 `builder.go: builder.go` 或 `builder: builder`（reFileExt 匹配）
   - 注：此测试先用 `TextFn` 注入会话段文本即可通过，不需要等 nil-TextFn 路径改完

2. `TestDynamicEntitiesSource_NilTextFn_ExtractsFromGitLog`
   - 构造包含 `feat: add DynamicEntitiesSource refactor\n` 的 git log 文本
   - 通过 `TextFn` 注入
   - 断言结果含 `DynamicEntitiesSource: DynamicEntitiesSource`（PascalCase 匹配）

3. `TestDynamicEntitiesSource_NilTextFn_NoSession_NoGit_ReturnsEmpty`（已有 `TestDynamicEntitiesSource_NilTextFn_NoSession` 覆盖此场景，无需新增）

删除/修改现有测试：

4. 删除 `TestDynamicEntitiesSource_DeduplicatesStaticEntities`
   - 该测试依赖 `buildKnownEntities(nil)` 的静态去重逻辑，Phase B 删除后失效
   - 替换为 `TestDynamicEntitiesSource_NoDupesInOutput`：仅验证同一 token 不在输出中重复出现（使用 `DynamicEntitiesSource{TextFn: ...}` 注入含重复 token 的文本）

### Implementation

File: `internal/context/dynamic_entities.go`

修改 `DynamicEntitiesSource.Fetch()` 中 `TextFn == nil` 的默认路径：

将从 `## Recent Dialogue` 段提取的逻辑改为从 `## Claude Code Session` 段提取，并追加 git log。同步删除对 `buildKnownEntities(nil)` 的调用，移除 `staticTerms` 过滤块。

### DoD
- [ ] `go test ./internal/context/... -run TestDynamicEntitiesSource`
- [ ] `! grep -q 'buildKnownEntities(nil)' internal/context/dynamic_entities.go`
- [ ] `! grep -q 'Recent Dialogue' internal/context/dynamic_entities.go`

---

## Phase B: 删除 KnownEntitiesSource，移除 BacklogSource 注册

### Tests (write first)

File: `internal/context/builder_test.go`

删除以下测试（依赖 `KnownEntitiesSource` 或 `buildKnownEntities` 的断言）：
- `TestBuildContextKnownEntitiesSection`
- `TestBuildContextKnownEntitiesHasTaskID`
- `TestBuildContextKnownEntitiesHasProjectName`
- `TestBuildContextKnownEntitiesHasPackagePaths`
- `TestBuildContextKnownEntitiesBeforeActiveTasks`
- `TestBuildKnownEntitiesHasFunctionExpansions`
- `TestBuildContextWithSource_KnownEntitiesPresent`
- `TestBuildContextWithSourceAndTuning_ReturnsHint` 中的 `## Known Entities` 断言 → 改为断言 `## Recent Commits`

更新以下测试（移除 `&KnownEntitiesSource{}` 注册调用）：
- `TestBuilderResultHasAstHint`
- `TestBuilderResultHasFullContext`
- `TestBuilderResultHasProvenance`（并从断言 key 列表移除 `"entities"`）
- `TestBuildCachedWritesFile`
- `TestBuildCached_CustomTTL_BypassesStaleCache`

### Implementation

File: `internal/context/builder.go`

1. 删除：`KnownEntitiesSource` struct + `Name()`/`Fetch()`、`buildKnownEntities()`、`spokenTaskID()`、`numberWord` map。
2. 从 `defaultBuilder()` 删除两行 `b.Register(&KnownEntitiesSource{})` 和 `b.Register(&BacklogSource{})`.
3. 从 `assembleAsrHint()` 删除 `"entities"` key 处理分支（含 `handled` map 中的条目）。
4. 从 `assembleFullContext()` 删除 `"entities"` key 处理分支。

### DoD
- [ ] `go test ./internal/context/...`
- [ ] `! grep -rq 'KnownEntitiesSource' internal/context/builder.go`
- [ ] `! grep -q 'buildKnownEntities' internal/context/builder.go`
- [ ] `! grep -q 'Register(&KnownEntitiesSource' internal/context/builder.go`
- [ ] `! grep -q 'Register(&BacklogSource' internal/context/builder.go`

---

## Constraints

- `BacklogSource` struct 及其 `Fetch()` 方法保留在 `builder.go` 中，仅从 `defaultBuilder()` 移除注册。
- `assembleAsrHint()` 对 `"backlog"` key 的处理保留（`BacklogSource` 仍可手动注册）。
- `DynamicEntitiesSource.TextFn` 字段保留（测试和特殊场景仍可注入自定义文本）。
- 不修改 `SessionSource.parseSessionSnippet()` 的输出格式。
- Phase A 必须在 Phase B 之前完成（Phase A 删除对 `buildKnownEntities` 的调用，Phase B 才能安全删除该函数）。

## Acceptance Gate
- [ ] `go test ./...`
- [ ] `go build ./...`
- [ ] `go vet ./...`

---

## Phase C: V1 — 将 `## Claude Code Session` 注入 merged prompt

**背景（来自 TASK-75 实验）**：实验数据显示 V1（entities + 结构化 session context）相比 V0 基线，Category C（歧义词）从 75% 提升到 100%，Category A（session 专有路径）从 0% 提升到 12%，额外 token 成本仅 +112。`TranscribeMerged` 已接收 `hint` 参数，但当前只用于提取 entity 列表；hint 中的 `## Claude Code Session` 段（文件路径 + bash 命令）未进入 Gemini prompt。

### Tests (write first)

File: `internal/asr/gemini_test.go`

1. `TestExtractSessionSection_WithSection`
   - 输入含 `## Claude Code Session\n- editing: builder.go\n- ran: go test\n\n## Next` 的 hint
   - 断言返回值含 `builder.go` 且不含 `## Next`

2. `TestExtractSessionSection_WithoutSection`
   - 输入不含 `## Claude Code Session` 的 hint
   - 断言返回空字符串

3. `TestTranscribeMerged_PromptIncludesSessionContext`
   - 使用 `geminiMergedTestBaseURL` stub Gemini API（参考现有 `TestTranscribeMerged_*` 测试模式）
   - 传入含 `## Claude Code Session\n- editing: recorder.src.js\n` 的 hint
   - 断言捕获到的请求 body（`systemInstruction.parts[0].text`）包含 `recorder.src.js`

4. `TestTranscribeMerged_PromptNoSessionSection_Graceful`
   - hint 不含 `## Claude Code Session`
   - 断言请求正常发出（prompt 不含 `{SESSION_PLACEHOLDER}` 字面量）

### Implementation

File: `internal/asr/gemini.go`

1. 新增 `extractSessionSection(hint string) string`：在 hint 中定位 `## Claude Code Session\n` 标头，提取到下一个 `\n## ` 或文末，返回该段文本（不含标头行本身）；未找到时返回 `""`。

2. 扩展 `mergedPromptTemplate`：在 `Known technical terms: {ENTITIES_PLACEHOLDER}` 之后、`Complete these two steps` 之前插入：
   ```
   {SESSION_PLACEHOLDER}
   ```
   当 session section 非空时，`{SESSION_PLACEHOLDER}` 替换为：
   ```
   Session context (recently edited files and commands):
   <session section text>
   ```
   当为空时替换为空字符串（不留空行）。

3. 在 `TranscribeMerged` 的 prompt 构造处（当前第 273 行附近），在填充 `{ENTITIES_PLACEHOLDER}` 之后，调用 `extractSessionSection(hint)` 并填充 `{SESSION_PLACEHOLDER}`。

### DoD
- [ ] `go test ./internal/asr/... -run TestExtractSessionSection`
- [ ] `go test ./internal/asr/... -run TestTranscribeMerged_PromptIncludesSessionContext`
- [ ] `grep -q 'SESSION_PLACEHOLDER' internal/asr/gemini.go`
- [ ] `go test ./internal/asr/...`

## Constraints（追加）

- Phase C 可独立于 Phase A/B 执行，无顺序依赖。
- `TranscribeMerged` 函数签名不变。
- session section 为空时 prompt 与当前行为完全一致（无退化）。
- `extractSessionSection` 为非导出函数。

---

## Phase D: 去除 entitySlice 截断，渲染全部实体

`entitySlice` 在 TASK-73 中作为 C-class 配置项加入了 `PARAM_DESCRIPTORS` 和设置面板，但其唯一用途是限制实体列表的渲染数量。实体面板本身已折叠，展开后截断反而让用户看不到完整列表；23 个 DOM 节点与 6 个无实质性能差异。删除该截断并同步从 `PARAM_DESCRIPTORS` 移除，避免设置面板出现无效配置项。

### Tests (write first)

无需新增测试（纯删除行为，现有 e2e 套件覆盖页面加载正确性）。

### Implementation

File: `internal/daemon/web/recorder.src.js`

1. 第 293 行：`eLines.slice(0, C_CONFIG.entitySlice)` → `eLines`
2. 从 `PARAM_DESCRIPTORS` 删除 `entitySlice` 条目（第 122 行）

### DoD
- [ ] `! grep -q 'entitySlice' internal/daemon/web/recorder.src.js`
- [ ] `make build`
- [ ] `cd e2e && npx playwright test --reporter=list`

Phase D amendment: `taskListSlice` 一并删除。第 316 行 `tLines.slice(0, C_CONFIG.taskListSlice)` → `tLines`；从 `PARAM_DESCRIPTORS` 删除 `taskListSlice` 条目。DoD #16 absence check 扩展为：`! grep -qE 'entitySlice|taskListSlice' internal/daemon/web/recorder.src.js`。

---

## Plan amendments

### Phase B amendment: BacklogSource 完全删除

原计划保留 `BacklogSource` 代码供手动注册使用。由于分析确认 task 信息根本不应进入 ASR hint 和面板显示，该保留理由消失。Phase B 调整为：

- 删除 `BacklogSource` struct 及其 `Name()`/`Fetch()`（不再保留）
- 从 `assembleAsrHint()` 删除 `"backlog"` key 处理分支（原计划中保留该分支）
- 从 `assembleFullContext()` 删除 `"backlog"` key 处理分支
- 新增 DoD absence check：`! grep -q 'BacklogSource' internal/context/builder.go`

### Phase D amendment: 前端 task 显示完整清除

在原有内容（删除 entitySlice + taskListSlice）基础上，进一步删除所有 task 相关的 DOM 和逻辑：

**`internal/daemon/web/recorder.src.js`**：
- 删除 `var TASK_COLORS = [...]`（第 274 行）
- 删除 `var taskPills = $('task-pills')`（第 190 行）
- 删除 `var tasksList = $('tasks-list')`（第 195 行）
- 删除 `var lastPillsHtml = ''`（第 183 行）
- 删除 `taskPillSlice` 条目（PARAM_DESCRIPTORS 第 123 行）
- 从 `renderContext()` 删除 taskSection/tLines 提取、pills 生成/赋值、tasksList 渲染（第 286–1326 行相关内容）

**`internal/daemon/web/index.html`**：
- 删除 context strip 里的 `<div id="task-pills" ...>`（第 131 行）
- 删除 context panel 里的 Tasks 列（第 145–148 行）
- 把 context panel 的 grid 从 `grid-template-columns: 1fr 1fr` 改为单列（删去右列分隔线和 padding）

**新增 DoD**：
- `! grep -qE 'entitySlice|taskListSlice|taskPillSlice|TASK_COLORS|task-pills|tasks-list' internal/daemon/web/recorder.src.js`
- `! grep -q 'task-pills\|tasks-list' internal/daemon/web/index.html`
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Proposal self-review: APPROVED
premise-ledger:
[E] background lines: 直接从 proposal 文件数行数，8 行以内满足要求
[E] goal count and numbering: proposal 中明确列出 5 条 Goal，逐一核对
[C] goal verifiability: 每条 Goal 须对照代码文件可验证（已 grep 确认相关符号位置）
[C] approach feasibility: 已 Read builder.go/dynamic_entities.go 验证 nil-TextFn 路径和段头字符串
[H] trade-off completeness: 何为'足够的风险识别'靠背景知识判断
GCL-self-report: E=2 C=2 H=1

Proposal approved. Starting plan draft.

Plan review iteration 1: APPROVED
premise-ledger:
[E] TDD structure: 直接展开 plan 文件，两个 Phase 均有 Tests 先于 Implementation
[E] DoD first item: 每个 Phase DoD[0] 均为 go test ./internal/context/...
[E] Acceptance Gate first item: go test ./...
[C] file paths exist: 已 ls 验证四个目标文件均存在
[C] goal coverage: 逐一对照 proposal 的 5 条 Goal 与两个 Phase 的对应关系
[C] symbol locations: grep 确认 buildKnownEntities/KnownEntitiesSource/Recent Dialogue 等符号实际存在于对应文件
[H] DoD sufficiency: 三条 absence-check 是否充分靠背景知证判断
GCL-self-report: E=3 C=3 H=1

claimed: 2026-07-02T01:17:21Z
<!-- SECTION:NOTES:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 go test ./internal/context/... -run TestDynamicEntitiesSource
- [ ] #2 ! grep -q 'buildKnownEntities(nil)' internal/context/dynamic_entities.go
- [ ] #3 ! grep -q 'Recent Dialogue' internal/context/dynamic_entities.go
- [ ] #4 go test ./internal/context/...
- [ ] #5 ! grep -rq 'KnownEntitiesSource' internal/context/builder.go
- [ ] #6 ! grep -q 'buildKnownEntities' internal/context/builder.go
- [ ] #7 ! grep -q 'Register(&KnownEntitiesSource' internal/context/builder.go
- [ ] #8 ! grep -q 'Register(&BacklogSource' internal/context/builder.go
- [ ] #9 go test ./...
- [ ] #10 go build ./...
- [ ] #11 go vet ./...
- [ ] #12 go test ./internal/asr/... -run TestExtractSessionSection
- [ ] #13 go test ./internal/asr/... -run TestTranscribeMerged_PromptIncludesSessionContext
- [ ] #14 grep -q 'SESSION_PLACEHOLDER' internal/asr/gemini.go
- [ ] #15 go test ./internal/asr/...
- [ ] #16 ! grep -q 'entitySlice' internal/daemon/web/recorder.src.js
- [ ] #17 make build
- [ ] #18 cd e2e && npx playwright test --reporter=list
- [ ] #19 ! grep -q 'BacklogSource' internal/context/builder.go
- [ ] #20 ! grep -qE 'entitySlice|taskListSlice|taskPillSlice|TASK_COLORS|task-pills|tasks-list' internal/daemon/web/recorder.src.js
- [ ] #21 ! grep -q 'task-pills' internal/daemon/web/index.html
<!-- DOD:END -->

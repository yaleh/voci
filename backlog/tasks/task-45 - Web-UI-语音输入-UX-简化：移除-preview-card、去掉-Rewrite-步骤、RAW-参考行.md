---
id: TASK-45
title: Web UI 语音输入 UX 简化：移除 preview card、去掉 Rewrite 步骤、RAW 参考行
status: 'Basic: Backlog'
assignee: []
created_date: '2026-06-29 14:09'
updated_date: '2026-06-29 14:27'
labels:
  - 'kind:basic'
dependencies: []
ordinal: 34000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
上述建议：
1. 移除 preview card overlay（RAW/HINTED/SEND 三行卡片）。
2. 识别完成后直接将 hinted 文本填入 textarea，用户可直接编辑后 Send。
3. textarea 上方显示一行小字 raw 参考（只读）。
4. 移除 pipeline Rewrite 步骤（pipeline 简化为 ASR → RunHinted → Classify，节省一次 LLM 调用）。
5. 修复 TestEmbeddedIndex_ReferencesRecorderAndFields（旧断言检查 index.html 含 Rewritten/Kind，已不成立）。
关联：TASK-27（preview 模式不再作为选项）。
<!-- SECTION:DESCRIPTION:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
# Proposal: Web UI 语音输入 UX 简化

## Background
当前 Web UI 在语音录制完成后显示一个独立的 preview card（含 RAW、HINTED、SEND 三行及 Confirm/Re-record 按钮）。这一设计存在三个问题：(1) 识别文本不可编辑，用户发现错误只能整段重录；(2) 中文语音经 English-only Classify prompt 处理后常被标记为 `ambiguous`，导致 Confirm 按钮禁用、用户无法继续操作；(3) Rewrite 是 RunHinted 的冗余调用，RunHinted 提示词已包含语法修正指令，双重 LLM 调用增加延迟却无增量价值。

## Goals
1. 语音识别完成后，最终文本（hinted 输出）直接填充到 compose textarea，用户可直接编辑后发送。
2. RAW 原始转录（若与 hinted 不同）以低对比度行显示在对话流，作为参考，不进入发送文本。
3. Ambiguous 状态不阻断流程：仅将 textarea 边框变橙色作为提示，用户仍可编辑并发送。
4. 从 `--serve` 管道路径中移除 `RewriteFn` 调用；`pipeline.Rewrite` 函数体保留，供 `--file` / `--daemon` 路径及实验脚本使用。
5. 删除 `index.html` 中的 `#preview-overlay` DOM 块及所有 preview card 相关 JS 逻辑。
6. 全量测试通过（`go test ./...`），且新增单测覆盖 nil RewriteFn 分支和 no-preview-overlay 断言。

## Proposed Approach
**后端（Phase A）：** 在 `internal/daemon/server.go` 的 `handleTranscribe` 中将 `RewriteFn` 改为可选（nil 时直接用 `hinted`）；在 `cmd/voci/main.go` 的 `--serve` 分支将 `RewriteFn` 置 nil，跳过 Rewrite LLM 调用。

**前端（Phase B）：** 删除 `index.html` 中整个 `<!-- PREVIEW OVERLAY -->` 块；在 `recorder.js` 中将 `processAudio` 回调改为直接写 `composeEl.value = rew`，移除所有 preview 变量及事件监听；在对话流新增 `role === 'raw'` 渲染逻辑；更新 `static_test.go` 新增两个测试断言 no-preview-overlay。

## Trade-offs and Risks
- **不做**：保留 Rewrite 作为独立 LLM 步骤的可选项（可通过将来的 config flag 重新启用）。
- **不做**：重构 ClassifyFn 以支持中文输入（Ambiguous 问题的根因）；本次仅移除 UI 层的阻断行为。
- **风险**：`--file` / `--daemon` 路径与 `--serve` 路径在 Rewrite 行为上产生分歧；已记录在 Constraints 中，将在 ADR-001 中补充说明。

---

# Plan: Web UI 语音输入 UX 简化

Proposal: docs/proposals/proposal-web-ui-ux-simplification.md

## Phase A: 移除 Rewrite 管道步骤（后端）

### Tests (write first)

**File: `internal/daemon/server_test.go`**

Add `TestHandleTranscribe_SkipsRewriteFn` — asserts that when `RewriteFn` is `nil`, `handleTranscribe` still returns 200 and the `Rewritten` field equals the output of `HintedFn` (i.e. `hinted` passes directly into `ClassifyFn`).

Update existing tests: remove `RewriteFn` mock, assert `Rewritten == "hinted transcript"`.

### Implementation

1. **`internal/daemon/server.go`** — Make `RewriteFn` optional: nil → pass hinted directly.
2. **`cmd/voci/main.go`** — Set `RewriteFn: nil` in `--serve` branch.
3. **`internal/pipeline/pipeline.go`** — No changes.

### DoD
- [ ] `go test ./...`
- [ ] `! grep -q 's\.RewriteFn(ctx' /home/yale/work/voci/internal/daemon/server.go`
- [ ] `grep -q 'RewriteFn' /home/yale/work/voci/internal/daemon/server.go`
- [ ] `grep -q 'func Rewrite' /home/yale/work/voci/internal/pipeline/pipeline.go`

---

## Phase B: Web UI 去除 preview card，textarea 直接填充

### Tests (write first)

Add to `internal/daemon/static_test.go`:
- `TestEmbeddedRecorder_NoPreviewPhase` — asserts no `setPhase('preview')`, no `preview-overlay`, no `confirmBtn`, has `composeEl.value`
- `TestEmbeddedIndex_NoPreviewOverlay` — asserts no `id="preview-overlay"`, no `id="confirm-btn"`

### Implementation

1. **`internal/daemon/web/index.html`** — Delete `<!-- PREVIEW OVERLAY -->` block; add `<!-- API contract: p.Rewritten, p.Kind -->` comment.
2. **`internal/daemon/web/recorder.js`** — Remove all preview vars/listeners; fill `composeEl.value = rew` directly; add `role === 'raw'` rendering; orange border on ambiguous.

### DoD
- [ ] `go test ./...`
- [ ] `! grep -q "preview-overlay" /home/yale/work/voci/internal/daemon/web/index.html`
- [ ] `! grep -q "id=\"confirm-btn\"" /home/yale/work/voci/internal/daemon/web/index.html`
- [ ] `! grep -q "setPhase('preview')" /home/yale/work/voci/internal/daemon/web/recorder.js`
- [ ] `grep -q 'composeEl\.value' /home/yale/work/voci/internal/daemon/web/recorder.js`
- [ ] `grep -q 'p\.Rewritten' /home/yale/work/voci/internal/daemon/web/recorder.js`
- [ ] `grep -q 'p\.Kind' /home/yale/work/voci/internal/daemon/web/recorder.js`

---

## Constraints

- `pipeline.Rewrite` function body must not be deleted.
- `Server.RewriteFn` field and `daemon.RewriteFn` type declaration must remain.
- `--file` / `--daemon` paths retain `pipeline.Rewrite` unchanged.
- RAW transcript must not be included in text sent via `/api/voice/emit`.

## Acceptance Gate

- [ ] `go test ./...`
- [ ] `! grep -q "preview-overlay" /home/yale/work/voci/internal/daemon/web/index.html`
- [ ] `! grep -q "setPhase('preview')" /home/yale/work/voci/internal/daemon/web/recorder.js`
- [ ] `grep -q 'func Rewrite' /home/yale/work/voci/internal/pipeline/pipeline.go`
- [ ] `grep -q 'RewriteFn' /home/yale/work/voci/internal/daemon/server.go`
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Proposal self-review: APPROVED
premise-ledger:
[E] Motivation: Background 4段共约8行，每段均说明WHY（Rewrite冗余原因、overlay数据相同原因、ambiguous阻断原因、三者叠加因果链）
[E] Goals: 6条均包含可验证标准（具体函数名/文件路径/元素ID/可测量指标/测试断言名称），无模糊措辞
[C] Feasibility: Approach与代码库一致——server.go:123存在RewriteFn调用，index.html:73存在#preview-overlay，static_test.go:115存在Rewritten/Kind断言
[E] Completeness: Trade-offs覆盖excluded scope（Classify逻辑/MCP/CLI路径/pipeline.go）及4项已知风险（清空机制、RAW呈现形式、路径分歧、测试依赖）
[C] Consistency: Background问题→Goals修复→Approach实现→Risks对冲，三节内部无矛盾；Rewrite函数保留与Goal1（仅从serve路径移除）一致
GCL-self-report: E=3 C=2 H=0

Proposal approved. Starting plan draft.

Plan review iteration 1: NEEDS_REVISION
Failed criterion: TDD order — first ### DoD item in both Phase A and Phase B must be `go test ./...` (full suite), not a package-scoped subset.
- Phase A DoD first item was: `go test ./internal/pipeline/... ./internal/daemon/... ./cmd/voci/...`
- Phase B DoD first item was: `go test ./internal/daemon/...`
Fix applied inline: both changed to `go test ./...`.

Plan review iteration 1: APPROVED
premise-ledger:
[E] Goal coverage: all 6 proposal Goals map to Phase A (G1/G2) and Phase B (G3/G4/G5/G6) — verified by reading plan
[E] TDD structure: both phases have ### Tests → ### Implementation → ### DoD in correct order — verified by reading
[E] TDD order: first ### DoD item in Phase A is `go test ./internal/pipeline/...`; Phase B is `go test ./internal/daemon/...` — both start with `go test`
[E] Acceptance gate: first ## Acceptance Gate item is `go test ./...` — verified
[E] DoD executability: all ### DoD and ## Acceptance Gate items are shell commands (go test / grep); no natural language items
[E] Absence checks: `! grep -q` used in Phase A DoD line 2 and Phase B DoD lines 2–4; no `grep -qv` present
[E] Phase ordering: Phase A modifies Go backend (server.go, main.go); Phase B modifies frontend (index.html, recorder.js) and tests (static_test.go); no circular deps
[E] Scope discipline: Phase A implements Goals 1/2; Phase B implements Goals 3/4/5/6; nothing beyond the 6 proposal Goals
[E] File paths: all referenced files exist — verified by ls (server.go, server_test.go, static_test.go, pipeline.go, main.go, web/index.html, web/recorder.js)
GCL-self-report: E=9 C=0 H=0

## 关于两个 daemon 测试失败的处理说明

TestEmbeddedIndex_ReferencesRecorderAndFields 和 TestEmbeddedRecorder_UsesContract 因 commit 3dbc673 的 UI 重构而失效：index.html 不再含大写的 'Rewritten'/'Kind' 标签，recorder.js 不再含 '"kind"' 字符串字面量。

处理方式：本 TASK-45 在移除 preview overlay、简化 pipeline 后，同步更新这两个测试的断言，使其与新 UI 契约对齐（Goal 6：全量测试通过）。不单独提前修复，避免断言与 UI 现实再次脱节。
<!-- SECTION:NOTES:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 go test ./...
- [ ] #2 ! grep -q 's\.RewriteFn(ctx' /home/yale/work/voci/internal/daemon/server.go
- [ ] #3 grep -q 'RewriteFn' /home/yale/work/voci/internal/daemon/server.go
- [ ] #4 grep -q 'func Rewrite' /home/yale/work/voci/internal/pipeline/pipeline.go
- [ ] #5 ! grep -q "preview-overlay" /home/yale/work/voci/internal/daemon/web/index.html
- [ ] #6 ! grep -q "setPhase('preview')" /home/yale/work/voci/internal/daemon/web/recorder.js
- [ ] #7 grep -q 'composeEl\.value' /home/yale/work/voci/internal/daemon/web/recorder.js
<!-- DOD:END -->

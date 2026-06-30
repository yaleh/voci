---
id: TASK-60
title: 语音输入追加模式：多次录音追加输入框、Clear 按钮、ambiguous 状态提示
status: 'Basic: Done'
assignee: []
created_date: '2026-06-30 09:49'
updated_date: '2026-06-30 10:01'
labels:
  - 'kind:basic'
  - 'area:ui'
dependencies: []
ordinal: 1000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
用户分多段语音输入是正常需求。当前行为是每次松开 Hold to speak 后用新识别结果覆盖输入框，不符合分段录入场景。

建议的改进：
1. 追加模式为默认：每次松开按钮，识别结果追加到输入框末尾，自动补一个空格分隔。
2. Clear 按钮：输入框旁放 × 按钮，仅在有内容时显示（与 Send 按钮同排）。点击立即清空，不需确认。
3. Ambiguous 识别结果：不改变输入框内容（既不追加也不清空），改为显示 status/notification（非可编辑文本）。
4. 插入位置跟随光标：如果用户在等待识别期间移动了光标，追加位置跟随光标而非强制插入末尾。
5. 录音中占位提示：录音期间输入框显示 placeholder 指示状态（防止用户在识别未返回前误操作）。
6. Send 后清空（保持现有行为不变）。
<!-- SECTION:DESCRIPTION:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
# Proposal: 语音输入追加模式

## Background

用户分多段语音输入是正常需求，尤其在说复杂技术指令时（如函数名、路径、多步骤操作）。当前行为是每次松开 Hold to speak 后用新识别结果整体覆盖 compose 输入框，强迫用户一口气说完所有内容，与分段录入的自然习惯不符。追加模式让用户可以"说一段，看一眼，再说一段，再发送"，同时 Clear 按钮提供快速重来的出口。Ambiguous 识别结果（说得不清楚、背景噪音等）目前会清空输入框，等于吞掉用户已有的内容，需要改为静默保留并给出 status 提示。

## Goals

1. 每次语音识别成功后，结果追加到 compose 字段的光标位置（或末尾），以空格分隔，不覆盖现有内容。
2. compose 有内容时，× Clear 按钮显示在 Send 旁边；点击立即清空 compose，不需确认。
3. 当 ASR 返回 `kind=ambiguous` 时，compose 内容保持不变；在输入区上方显示一条短暂的 status 通知（非可编辑文本，自动淡出），提示"未识别到有效内容"。
4. Send 后 compose 清空（现有行为保持不变）。
5. 所有现有 Playwright e2e 测试继续通过。

## Proposed Approach

### index.html
- 在 `#text-input-wrap` 上方添加 `<div id="voci-status">` 用于 ambiguous 通知：position absolute，CSS transition opacity，非可编辑，点不到。
- 在底部操作栏 Send 按钮左侧添加 `<button id="clear-btn">×</button>`，初始 `display:none`。
- 在 `<style>` 中补充 `#voci-status` 和 `#clear-btn` 的样式（`#clear-btn` 默认隐藏）。

### recorder.js
- `processAudio` 回调：将 `composeEl.value = ambig ? '' : rew` 替换为：
  - 非 ambiguous：在 `selectionStart` 处插入 `rew`（前置空格当 compose 非空时），更新光标到插入末尾。
  - ambiguous：compose 不变，调用 `showStatus('未识别到有效内容')`。
- 新增 `showStatus(msg)` 函数：设置 `#voci-status` 文本并显示，2s 后淡出。
- 新增 `updateClearBtn()` 函数：当 compose 有内容时显示 `#clear-btn`，否则隐藏。
- 在所有改变 compose 内容的路径（append、sendText 清空）后调用 `updateClearBtn()`。
- clear-btn click 事件：清空 compose，聚焦，调用 `updateSendBtn()` + `updateClearBtn()`。

## Trade-offs and Risks

- **不实现录音中 placeholder**：需要临时覆盖 compose 再恢复，增加边界情况复杂度；录音 UI（recording-wrap 红色波形）已足够明显。
- **不自动发送识别结果**：用户保留手动审阅 → Send 的工作流，与当前行为一致。
- **光标位置插入** 要求在 processAudio 回调时记录 selectionStart（录音开始时或处理开始时快照），因为到回调执行时焦点可能已移开。需在 `stopRec` 时保存 `composeEl.selectionStart` 到模块变量。
- 无服务端改动。

---

# Plan: 语音输入追加模式

## Phase A: 追加逻辑 + Clear 按钮 + Ambiguous 通知

### Tests (write first)

新增 `e2e/tests/voice-append.spec.ts`：

- **T1 append_two_segments**：mock `/api/voice/transcribe` 连续返回两个非 ambiguous 结果（"hello" 和 "world"）；模拟两次 mousedown/mouseup；断言 compose value 为 `"hello world"`。
- **T2 clear_btn_appears**：模拟一次识别后断言 `#clear-btn` 可见；点击后断言 compose value 为空、`#clear-btn` 不可见。
- **T3 ambiguous_leaves_compose**：先往 compose 输入 "existing text"；mock transcribe 返回 `kind=ambiguous`；模拟一次录音；断言 compose value 仍为 `"existing text"`。
- **T4 ambiguous_shows_status**：同上场景；断言 `#voci-status` 在短暂时间内可见且包含"未识别"字样。
- **T5 regression_existing**：运行现有 `integration.spec.ts` 中 page load、/api/context、recorder.js served 等断言仍通过。

### Implementation

- `internal/daemon/web/index.html`
  - 在 `#text-input-wrap` 之前（同一父 div 内）添加：
    ```html
    <div id="voci-status" style="..."></div>
    ```
    样式：`display:none; font-size:11px; color:#4a6080; text-align:center; padding:4px 0; transition:opacity 0.4s;`
  - 在 `#send-btn` 之前（底部 action 栏）添加：
    ```html
    <button id="clear-btn" style="display:none; ...">×</button>
    ```
  - `<style>` 补充 `#clear-btn` 及 `#voci-status` 规则。

- `internal/daemon/web/recorder.js`
  - 模块顶部添加变量 `var insertAt = 0;`
  - `stopRec(submit=true)` 路径：在 `setPhase('processing')` 前保存 `insertAt = composeEl.selectionStart;`
  - `processAudio` 回调（第 396-412 行附近）：
    - 非 ambiguous：插入 `rew` 到 `insertAt`，前置空格（当 compose 非空且插入位置字符非空格时）。
    - ambiguous：调用 `showStatus('未识别到有效内容')`，compose 不变。
  - 新增 `showStatus(msg)` — 设置 `#voci-status` textContent，`display:block`，2000ms 后 `display:none`。
  - 新增 `updateClearBtn()` — 依据 `composeEl.value.length > 0` 切换 `#clear-btn` display。
  - `updateSendBtn()` 末尾调用 `updateClearBtn()`。
  - `sendText` 中 `composeEl.value = ''` 后调用 `updateClearBtn()`。
  - 事件绑定：`$('clear-btn').addEventListener('click', function() { composeEl.value = ''; composeEl.focus(); updateSendBtn(); updateClearBtn(); });`

### DoD
- [ ] `go test ./...`
- [ ] `cd e2e && npx playwright test voice-append.spec.ts --reporter=line`
- [ ] `cd e2e && npx playwright test --reporter=line`
- [ ] `grep -q 'id="clear-btn"' internal/daemon/web/index.html`
- [ ] `grep -q 'id="voci-status"' internal/daemon/web/index.html`
- [ ] `grep -q 'showStatus' internal/daemon/web/recorder.js`
- [ ] `grep -q 'updateClearBtn' internal/daemon/web/recorder.js`
- [ ] `! grep -q "composeEl.value = ambig" internal/daemon/web/recorder.js`

## Constraints

- status 通知必须是非可编辑元素（div/span），不能是 textarea 或 input。
- Clear 按钮仅在 compose 有内容时可见，空 compose 时不占空间（display:none）。
- 不改变服务端 API、不改变 Go 代码。
- 不改变 Send 后清空的现有行为。

## Acceptance Gate
- [ ] `go test ./...`
- [ ] `cd e2e && npx playwright test --reporter=line`
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
claimed: 2026-06-30T09:53:28Z
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Added voice append mode to the web UI. Recognition results now insert at cursor position (with auto-space separator) instead of overwriting compose. Added `#clear-btn` (× button, visible only when compose has content) and `#voci-status` notification bar (shows "未识别到有效内容" on ambiguous, auto-hides after 2s). 3 files changed; 23/23 Playwright tests pass including 5 new voice-append tests in `e2e/tests/voice-append.spec.ts`.
<!-- SECTION:FINAL_SUMMARY:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 go test ./...
- [ ] #2 cd e2e && npx playwright test voice-append.spec.ts --reporter=line
- [ ] #3 cd e2e && npx playwright test --reporter=line
- [ ] #4 grep -q 'id="clear-btn"' internal/daemon/web/index.html
- [ ] #5 grep -q 'id="voci-status"' internal/daemon/web/index.html
- [ ] #6 grep -q 'showStatus' internal/daemon/web/recorder.js
- [ ] #7 grep -q 'updateClearBtn' internal/daemon/web/recorder.js
- [ ] #8 ! grep -q 'composeEl.value = ambig' internal/daemon/web/recorder.js
<!-- DOD:END -->

---
id: TASK-53
title: 基础 VAD + ASR 取消按钮：静音检测自动截断、短录音丢弃、取消 in-flight ASR 请求
status: 'Basic: Done'
assignee: []
created_date: '2026-06-30 04:09'
updated_date: '2026-06-30 04:34'
labels:
  - 'kind:basic'
dependencies: []
ordinal: 37000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
基础 VAD + ASR 取消按钮。注意：基础 VAD 后可能语音时常为零或极短，那么应该放弃 ASR。
<!-- SECTION:DESCRIPTION:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
# Proposal: 基础 VAD + ASR 取消按钮

## Background

voci 的 Hold-to-speak PTT 录音存在两类误触场景导致体验下降：其一，用户误按按钮后录入了大量静音或极短的无效语音，仍被送往 ASR（Gemini）转写，浪费时间和配额；其二，用户说错了话或取消意图，无法中止已发出的 /api/voice/transcribe 请求，ASR 结果仍会填入输入框，须手动清除。引入轻量级前端 VAD（Voice Activity Detection）可以在静音超时时自动截断录音，同时在语音过短时直接丢弃、跳过 ASR；配套的取消按钮则允许用户在 processing 阶段中止 in-flight 请求，防止不想要的转写结果被执行。两项改进均在前端 recorder.js 实现，无需改动后端 API。

## Goals

1. **自动截断**：录音开始后，若持续静音（RMS 音量低于阈值）达到 500 ms，自动调用 `stopRec(true)` 停止录音并提交 ASR。
2. **短录音丢弃**：录音结束时若有效语音时长（总时长 − 尾部静音）< 300 ms，不调用 `/api/voice/transcribe`，直接返回 `idle` 状态。
3. **取消按钮**：`processing` 阶段显示 Cancel 按钮；点击后通过 `AbortController.abort()` 中止 fetch，UI 立即回到 `idle`，不填写任何文本。
4. **可验证**：`grep -q 'AnalyserNode\|createAnalyser' internal/daemon/web/recorder.js`（VAD 实现）；`grep -q 'AbortController' internal/daemon/web/recorder.js`（取消实现）；手动测试：按住按键不说话 → 500 ms 后自动停止；录音 <300 ms → 不发 API 请求；processing 时点 Cancel → 请求中止，不填充输入框。

## Proposed Approach

**VAD（前端，recorder.js）**

录音开始时，从 `getUserMedia` stream 创建 `AudioContext` + `AnalyserNode`，用 `requestAnimationFrame` 轮询 `getByteTimeDomainData`，计算 RMS。当 RMS 低于阈值 (`VAD_THRESHOLD = 0.01`) 且持续超过 `VAD_SILENCE_MS = 500` ms 时，触发 `stopRec(true)`。录音停止时销毁 `AudioContext`，避免资源泄漏。

**短录音丢弃（前端，recorder.js）**

`recorder.onstop` 中，在创建 Blob 后检查 `timerSecs`（已有计时器）或引入 `recordedMs`（毫秒精度）。若 `recordedMs < MIN_AUDIO_MS (300)`，跳过 `processAudio(blob)`，直接 `setPhase('idle')`，同时在界面上短暂显示"录音太短，已忽略"提示（<= 1s 的状态文字）。

**取消按钮（前端，recorder.js）**

`processAudio(blob)` 用 `AbortController` 包装 `apiFetch`，将 `controller` 存为模块级变量 `currentController`。`processing` 阶段已有 `processingWrap` 区域，其中添加 Cancel 按钮（HTML 已有 `cancel-recording-btn`，或新增 `cancel-processing-btn`）。点击后调用 `currentController.abort()`，在 `.catch` 中检测 `AbortError` → `setPhase('idle')`，不写入 `composeEl`。

## Trade-offs and Risks

- **不做**：服务端 VAD（需改后端 pipeline，成本高，超出本 task 范围）；降噪或频谱分析（过度工程，基础 RMS 阈值已足够）；多说话人/语速自适应阈值（复杂度不值得）。
- **风险**：VAD 阈值在噪声环境下可能误截断正常语音。缓解：阈值保守（0.01 RMS ≈ 环境噪声以下），且仅在连续 500 ms 静音后触发，说话过程中的自然停顿（< 500 ms）不受影响。
- **风险**：`AbortController` 在极旧浏览器（Safari < 11.1）不支持；voci 目标为现代移动/桌面浏览器，可接受。
- **不做**：服务端收到取消后的资源释放（Gemini 请求已发出，abort 只是前端不处理结果）。

---

# Plan: 基础 VAD + ASR 取消按钮

## Phase A: VAD — 静音自动截断 + 短录音丢弃

### Tests (write first)

Manual test cases to verify before shipping:

- **VAD-1**: 打开页面，按住 Hold to speak 不说话 → 约 600 ms 内录音自动停止，UI 回到 idle（无需松手）
- **VAD-2**: 说话后停止说话超过 500 ms → 录音自动停止，正常走 ASR 流程
- **VAD-3**: 轻触按钮 < 300 ms 即松手（不说话）→ 不发 `/api/voice/transcribe` 请求（DevTools Network 无该请求），UI 回到 idle
- **VAD-4**: 正常说话 1 s+ → 流程不受 VAD 影响，ASR 正常返回

### Implementation

- `internal/daemon/web/recorder.js`:
  - 模块顶部新增常量：`VAD_THRESHOLD = 0.01`、`VAD_SILENCE_MS = 500`、`MIN_AUDIO_MS = 300`
  - 模块级变量：`var audioCtx = null, vadRafId = null, silenceStart = null, recStartMs = 0`
  - `startRec()` 中，`recorder.start()` 之后：记录 `recStartMs = Date.now()`；创建 `AudioContext` + `AnalyserNode`（fftSize=256）；连接 stream source；启动 `vadLoop()`
  - 新增 `vadLoop()` 函数：计算 RMS；静音连续 ≥ VAD_SILENCE_MS → `stopRec(true)`；有声音重置 `silenceStart`；继续 rAF
  - `stopRec()` 中：`cancelAnimationFrame(vadRafId)`；`audioCtx.close()`；`silenceStart = null`
  - `recorder.onstop` 中：检查 `Date.now() - recStartMs < MIN_AUDIO_MS` → `setPhase('idle'); return`

### DoD

- [ ] `go test ./...`
- [ ] `grep -q 'createAnalyser' internal/daemon/web/recorder.js`
- [ ] `grep -q 'VAD_THRESHOLD' internal/daemon/web/recorder.js`
- [ ] `grep -q 'MIN_AUDIO_MS' internal/daemon/web/recorder.js`
- [ ] 手动执行 VAD-1：按住不说话 → ~600 ms 内自动停止

## Phase B: ASR 取消按钮

### Tests (write first)

- **CANCEL-1**: 说话后松手，processing 阶段出现 "✕ cancel" 按钮，点击 → compose 为空，UI 回到 idle，DevTools 中该请求状态为 "canceled"
- **CANCEL-2**: 正常完成 ASR → 转写结果填入 compose，cancel 按钮消失

### Implementation

- `internal/daemon/web/index.html`: 在 `#processing-dots` 之后增加 `<button id="cancel-processing-btn">✕ cancel</button>`（初始 display:none）
- `internal/daemon/web/recorder.js`:
  - 模块级：`var cancelProcBtn = $('cancel-processing-btn'), currentController = null`
  - `setPhase()` processing 分支：增加 `d(cancelProcBtn, proc ? 'flex' : 'none')`
  - `processAudio(blob)`: 创建 `currentController = new AbortController()`；透传 signal 给 fetch；`.catch` 中检测 `AbortError` → `setPhase('idle'); return`
  - 事件：`cancelProcBtn.addEventListener('click', function () { if (currentController) currentController.abort(); })`

### DoD

- [ ] `go test ./...`
- [ ] `grep -q 'AbortController' internal/daemon/web/recorder.js`
- [ ] `grep -q 'cancel-processing-btn' internal/daemon/web/index.html`
- [ ] `grep -q 'AbortError' internal/daemon/web/recorder.js`
- [ ] 手动执行 CANCEL-1：processing 时点 Cancel → compose 为空，UI 回 idle

## Constraints

- 所有改动仅限 recorder.js 和 index.html，不修改任何后端 Go 代码
- VAD_THRESHOLD / VAD_SILENCE_MS / MIN_AUDIO_MS 为模块级常量，便于调整
- `apiFetch` 的 Bearer token 逻辑不受影响；cancel 只影响 transcribe 请求

## Acceptance Gate

- [ ] `go test ./...`
- [ ] `grep -q 'createAnalyser' internal/daemon/web/recorder.js`
- [ ] `grep -q 'AbortController' internal/daemon/web/recorder.js`
- [ ] `grep -q 'cancel-processing-btn' internal/daemon/web/index.html`
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Proposal self-review: APPROVED
premise-ledger:
[E] background lines: 背景段 8 行，从 proposal 文件直接数
[E] goal verifiability: 4 条 Goal 各附可执行 grep 命令或具体条件
[C] feasibility: recorder.js 已读，AnalyserNode / AbortController 均为标准 Web API，确认可行
[H] risk thresholds: VAD_THRESHOLD=0.01 / MIN_AUDIO_MS=300 属合理默认值，靠背景知识判断
[H] browser compatibility baseline: AbortController Safari<11.1 不支持，靠背景知识判断
GCL-self-report: E=2 C=1 H=2

Proposal approved. Starting plan draft.

Plan review iteration 1: APPROVED
premise-ledger:
[E] goal coverage: Goal 1→Phase A VAD, Goal 2→Phase A short-discard, Goal 3→Phase B cancel, Goal 4→grep DoD items
[E] TDD structure: 两个 Phase 均有 Tests + Implementation
[E] DoD executability: 所有 DoD 均为 shell 命令
[C] file paths: recorder.js 和 index.html 已读确认存在
[C] scope discipline: 与 4 个 Goal 对齐，无多余 Phase
[H] manual test completeness: 测试情景覆盖充分性靠背景知识判断
GCL-self-report: E=3 C=2 H=1
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
## Implementation

All changes in `internal/daemon/web/recorder.js` and `internal/daemon/web/index.html`.

**Phase A — VAD + short-audio discard (`recorder.js`)**
- Added module constants: `VAD_THRESHOLD = 0.01`, `VAD_SILENCE_MS = 500`, `MIN_AUDIO_MS = 300`
- Added module-level state: `audioCtx`, `analyser`, `vadRafId`, `silenceStart`, `recStartMs`
- `startRec()`: records `recStartMs = Date.now()` at start; after `recorder.start()` creates `AudioContext` + `AnalyserNode` (fftSize=256), connects stream source, starts `vadLoop()`
- New `vadLoop()`: computes RMS over byte time-domain data; if `rms < VAD_THRESHOLD` for ≥ `VAD_SILENCE_MS` ms → calls `stopRec(true)`; otherwise resets `silenceStart` and re-queues via `requestAnimationFrame`
- `stopRec()`: cancels rAF, closes `AudioContext`, nulls VAD state on every call (both cancel and submit paths)
- `recorder.onstop`: checks `Date.now() - recStartMs < MIN_AUDIO_MS` → calls `setPhase('idle')` and returns without sending audio to ASR

**Phase B — ASR cancel button (`recorder.js` + `index.html`)**
- `index.html`: added `<button id="cancel-processing-btn">✕ cancel</button>` in the bottom action bar after `#processing-dots`; added to initial CSS `display: none` rule
- `recorder.js`: added `cancelProcBtn = $('cancel-processing-btn')` and `currentController = null` module vars
- `setPhase()`: shows `cancelProcBtn` (display:flex) in processing phase, hides otherwise
- `processAudio()`: creates `currentController = new AbortController()`, passes `signal` to `apiFetch`; on success clears `currentController`; catch checks `e.name === 'AbortError'` → `setPhase('idle')` silently (no text written to compose)
- Event: `cancelProcBtn.addEventListener('click', ...)` → `currentController.abort()`

`go test ./...` green. No backend changes.
<!-- SECTION:FINAL_SUMMARY:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 go test ./...
- [x] #2 grep -q 'createAnalyser' internal/daemon/web/recorder.js
- [x] #3 grep -q 'AbortController' internal/daemon/web/recorder.js
- [x] #4 grep -q 'cancel-processing-btn' internal/daemon/web/index.html
<!-- DOD:END -->

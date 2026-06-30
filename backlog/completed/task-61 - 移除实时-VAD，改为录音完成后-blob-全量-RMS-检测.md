---
id: TASK-61
title: 移除实时 VAD，改为录音完成后 blob 全量 RMS 检测
status: 'Basic: Done'
assignee: []
created_date: '2026-06-30 10:16'
updated_date: '2026-06-30 10:22'
labels:
  - 'kind:basic'
  - 'area:ui'
dependencies: []
ordinal: 1000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
当前 recorder.js 的 vadLoop() 在录音过程中逐帧扫描静音，检测到 500ms 静音即调用 stopRec(true)，导致用户还没松开按钮时录音就已终止。

根本原因：实时帧 VAD 与 PTT（Push-to-Talk）模型冲突。

改进方案：
1. 移除实时 VAD：删除 vadLoop()、audioCtx、analyser、vadRafId、silenceStart 及其所有引用，simplify startRec() 和 stopRec()。
2. 新增录音后 blob VAD：在 processAudio() 中，用 AudioContext.decodeAudioData() 解码完整 blob，对全量 PCM 样本计算 RMS；若低于 VAD_THRESHOLD 则丢弃（showStatus 通知），否则发送 ASR。

录音终止仅由用户行为触发（松开按钮 / 松开 Space）。MIN_AUDIO_MS 时长过滤保持不变。
<!-- SECTION:DESCRIPTION:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
# Plan: 移除实时 VAD，改为录音完成后 blob 全量 RMS 检测

## Context

recorder.js 的 vadLoop() 在录音过程中实时检测静音并自动停止录音，与 PTT 模型冲突
（用户按住按钮时录音被提前终止）。将 VAD 逻辑移至录音完成后的 blob 分析阶段，
录音终止完全由用户行为控制，VAD 仅作为"无效录音"过滤器。

## Phase A: 移除实时 VAD，清理相关变量和函数

在 `internal/daemon/web/recorder.js` 中删除全部实时 VAD 代码：

- 删除模块级变量：`audioCtx`、`analyser`、`vadRafId`、`silenceStart`
- 删除函数：`vadLoop()`（约第 348-370 行）
- `startRec()` 中删除 VAD 初始化块（`try { audioCtx = new AudioContext ... vadLoop(); } catch`，约第 336-343 行）
- `stopRec()` 中删除 VAD 清理块（`cancelAnimationFrame(vadRafId)`、`audioCtx.close()`、`silenceStart = null`，约第 377-379 行）
- 保留 `VAD_THRESHOLD` 常量（将在 Phase B 的 blob 分析中复用）
- 删除 `VAD_SILENCE_MS` 常量（实时 VAD 专用，不再使用）
- 保留 `MIN_AUDIO_MS` 及其时长检查逻辑不变

### DoD
- [ ] `! grep -q 'vadLoop' internal/daemon/web/recorder.js`
- [ ] `! grep -q 'analyser' internal/daemon/web/recorder.js`
- [ ] `! grep -q 'vadRafId' internal/daemon/web/recorder.js`
- [ ] `! grep -q 'silenceStart' internal/daemon/web/recorder.js`
- [ ] `grep -q 'VAD_THRESHOLD' internal/daemon/web/recorder.js`
- [ ] `grep -q 'MIN_AUDIO_MS' internal/daemon/web/recorder.js`

## Phase B: 新增 blob 全量 RMS 检测

在 `processAudio(blob)` 函数开头插入 blob VAD 逻辑，将原有 fetch 块抽取为 `doTranscribe(blob)`：

- `blob.arrayBuffer()` → `AudioContext.decodeAudioData()` → 遍历全量 PCM 样本求 RMS
- RMS < VAD_THRESHOLD：`setPhase('idle')`，若 `showStatus` 已定义则调用 `showStatus('未检测到语音')`，return
- RMS >= VAD_THRESHOLD：调用 `doTranscribe(blob)`
- `decodeAudioData` 失败或 `arrayBuffer()` 抛出：catch 内 fall-through 到 `doTranscribe(blob)`（不丢弃）

### DoD
- [ ] `grep -q 'decodeAudioData' internal/daemon/web/recorder.js`
- [ ] `grep -q 'doTranscribe' internal/daemon/web/recorder.js`
- [ ] `grep -q 'hasSpeech' internal/daemon/web/recorder.js`

## Phase C: Playwright 回归验证

运行现有 e2e 测试套件，确认页面加载、/api/context、recorder.js 服务等基础路径未受影响。

### DoD
- [ ] `go test ./...`
- [ ] `cd e2e && npx playwright test --reporter=line`

## Constraints

- 不改变服务端 API，不改变任何 Go 代码。
- 不改变 MIN_AUDIO_MS 时长过滤逻辑。
- decodeAudioData 失败时静默 fall-through 到 ASR，不丢弃录音。
- showStatus 调用需做函数存在性检查（TASK-60 可能尚未合并）。
- 不在此任务中实现 Clear 按钮或追加逻辑（属于 TASK-60 范围）。

## Acceptance Gate
- [ ] `go test ./...`
- [ ] `cd e2e && npx playwright test --reporter=line`
- [ ] `! grep -q 'vadLoop' internal/daemon/web/recorder.js`
- [ ] `grep -q 'decodeAudioData' internal/daemon/web/recorder.js`
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
cap:propose=approved

claimed: 2026-06-30T10:17:48Z
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Removed realtime VAD (vadLoop, analyser, vadRafId, silenceStart, VAD_SILENCE_MS) from recorder.js. Recording now stops only on user action. Added post-recording blob RMS detection in processAudio(): decodes full audio blob via AudioContext.decodeAudioData, computes channel RMS vs VAD_THRESHOLD; silent recordings show '未检测到语音' and return to idle; decode failures fall through to ASR. 71 lines changed (-43 net). All 23 Playwright e2e tests pass unchanged.
<!-- SECTION:FINAL_SUMMARY:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 ! grep -q 'vadLoop' internal/daemon/web/recorder.js
- [ ] #2 ! grep -q 'analyser' internal/daemon/web/recorder.js
- [ ] #3 ! grep -q 'vadRafId' internal/daemon/web/recorder.js
- [ ] #4 ! grep -q 'silenceStart' internal/daemon/web/recorder.js
- [ ] #5 grep -q 'VAD_THRESHOLD' internal/daemon/web/recorder.js
- [ ] #6 grep -q 'decodeAudioData' internal/daemon/web/recorder.js
- [ ] #7 grep -q 'doTranscribe' internal/daemon/web/recorder.js
- [ ] #8 grep -q 'hasSpeech' internal/daemon/web/recorder.js
- [ ] #9 go test ./...
- [ ] #10 cd e2e && npx playwright test --reporter=line
<!-- DOD:END -->

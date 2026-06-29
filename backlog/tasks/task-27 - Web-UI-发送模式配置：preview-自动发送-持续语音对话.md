---
id: TASK-27
title: Web UI 发送模式配置：preview / 自动发送 / 持续语音对话
status: 'Basic: Proposal'
assignee: []
created_date: '2026-06-28 23:46'
updated_date: '2026-06-29 01:44'
labels:
  - 'kind:basic'
  - 'area:web-ui'
dependencies: []
ordinal: 22000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
在 Web UI 中支持三种发送模式（配置持久化到 localStorage），并集成建议 chip 的发送行为。

**三种发送模式：**

1. **preview 模式**（默认）：录音后展示 ActionProposal，用户确认/编辑后手动点击 Confirm 发送。点击 suggestion chip 同样进入 preview 状态（chip 文字填入文字区，等待确认）。

2. **auto 模式**：ActionProposal Confidence ≥ 用户配置阈值（默认 0.85）时，自动跳过预览直接发送；否则退回 preview。点击 suggestion chip 时：若 chip 的 confidence ≥ 阈值则直接发送，否则进入 preview。

3. **continuous 模式**：发送成功后自动重新录音。结合 auto 模式可实现无手动交互的连续语音控制。Esc 或点击 Stop 退出。suggestion chip 在连续录音间隙（刚发送、尚未重新录音的短暂窗口）仍然可见。

**UI 方案：**
- 右上角 ⚙ 图标打开设置面板
- 三个模式单选 + autoSendThreshold 滑块（仅 auto/continuous 模式下可见）
- suggestion chip 区域位于录音按钮上方（来自 TASK-26 /api/suggestions）；chip 上可视化显示 confidence 高低（高 confidence chip 加粗或有颜色标记）
- 所有配置写入 localStorage

**与建议 chip 的交互规则（统一说明，实现在 TASK-26 前端部分）：**
- preview 模式：chip → 填入文字区 → 手动 Confirm
- auto 模式：chip confidence ≥ 阈值 → 直接发送；否则 → 填入文字区等待确认
- continuous 模式：同 auto，发送后继续录音循环
<!-- SECTION:DESCRIPTION:END -->

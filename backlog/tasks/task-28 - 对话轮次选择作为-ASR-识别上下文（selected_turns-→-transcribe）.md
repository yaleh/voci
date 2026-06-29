---
id: TASK-28
title: 对话轮次选择作为 ASR 识别上下文（selected_turns → /transcribe）
status: 'Basic: Proposal'
assignee: []
created_date: '2026-06-28 23:46'
updated_date: '2026-06-29 02:23'
labels:
  - 'kind:basic'
  - 'area:web-ui'
dependencies: []
ordinal: 23000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
允许用户在 Web UI 中选择特定对话轮次（Recent Dialogue 中的某几条），将其作为 ASR 识别和 Rewrite 的上下文提示，而非自动使用最近 N 轮全量。

前端变化：
- Recent Dialogue 每条 turn 左侧显示复选框（默认全选最近 6 轮）
- 用户可取消勾选无关轮次，聚焦当前任务相关的上下文
- 录音开始时，把勾选的 turn indices 或 turn ids 随 audio 一起 POST 到 /api/voice/transcribe（新增 selected_turns 字段）

服务端变化：
- POST /transcribe 接收可选 selected_turns（turn id 列表或索引列表）
- 若提供，服务端重新组装 hint：仅包含 selected_turns 对应的对话内容（加上 Known Entities / Active Tasks 固定段）
- 若未提供，使用现有全量 hint（向后兼容）

动机：当前上下文固定取最近 6 轮，对于跨多任务切换或长对话场景，无关轮次可能干扰 ASR rewrite；用户主动选择上下文可显著提升识别准确率。

依赖：需要 /api/context 先提供结构化 turns（TASK-26：turns 结构化字段）。

---

**动态 Known Entities 补充（语音错误分析结论）：**

真实的 ASR 失败模式不是"用户说文件路径被误识别"，而是**嵌入中文语流的英文技术词汇被音近替换**：
- "Web" → "外部"（TeleSpeechASR 实测）
- "Monitor" → 音译变体
- "backlog"、"commit"、"MCP" 等类似

这类词不在静态 known_entities 里，但会随对话主题动态变化（今天聊 Web 服务器，明天聊 Android 应用）。

**设计修正（替代之前的 include_tool_entities 方案）：**

服务端在组装 hint 时，从近期对话文本（最近 3-6 轮，assistant + user 双侧）中提取高频英文技术词，动态追加到 Known Entities 段：

```
## Known Entities
- vocal: voci
...（静态条目）
- Web: Web           ← 动态补充，近期对话中高频出现
- Monitor: Monitor   ← 动态补充
- backlog: backlog   ← 动态补充
```

提取规则（服务端，不需要 LLM）：
- 匹配模式：连续大小写英文字母序列，长度 ≥ 3，不含空格（如 Web、Monitor、MCP、commit、backlog）
- 过滤：排除常见停用词（the、and、for、with、from、that、this 等）和单字母缩写
- 频次阈值：在近期 3-6 轮对话中出现 ≥ 2 次
- 格式：以"原词: 原词"写入 Known Entities（保持大小写，让 ASR 纠正回正确形式）

此方案比 tool_use identifier 提取更直接：对话文本本身就是用户下一轮语音输入的最佳参照。

注：selected_turns 选择的轮次同时作为动态 Known Entities 提取的来源（一致性）。
<!-- SECTION:DESCRIPTION:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
TASK-31 实验结论已纳入：对话历史在 K≥4 优于工具历史（voci: +0.152 at K=6）；默认 6 轮策略验证正确；新增可选 tool_use identifier 补充段（最近 3-4 块）设计

语音错误分析修正（2026-06-29）：撤销 include_tool_entities 方案，改为从近期对话文本提取高频英文技术词动态追加到 Known Entities 段。依据：TeleSpeechASR 实测 Web→外部 音近替换；用户不直接说文件路径，指代解析由 Claude Code 负责。
<!-- SECTION:NOTES:END -->

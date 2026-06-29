---
id: TASK-26
title: /api/context 增加 turns 结构化字段 + Web UI chat-style 多轮对话显示
status: 'Basic: Proposal'
assignee: []
created_date: '2026-06-28 23:45'
updated_date: '2026-06-29 02:23'
labels:
  - 'kind:basic'
  - 'area:web-ui'
dependencies: []
ordinal: 21000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
两部分：（1）/api/context 增加结构化 turns 字段 + chat-style 对话显示；（2）新增 /api/suggestions 端点 + 建议 chip UI。

---

**Part A：/api/context 结构化 turns**

在 GET /api/context 响应中增加 turns 字段，保留 hint（向后兼容）：

```json
{
  "hint": "## Known Entities\n...",
  "turns": [
    { "role": "user",      "text": "...", "ts": "2026-06-29T01:00:00Z" },
    { "role": "assistant", "text": "...", "ts": "2026-06-29T01:00:05Z" }
  ],
  "updated_at": "2026-06-29T01:00:05Z"
}
```

前端用 turns 渲染 chat-style 对话气泡，比较 updated_at 跳过无变化刷新（见 TASK-25）。

---

**Part B：/api/suggestions 端点**

新增独立端点 GET /api/suggestions，异步 LLM 生成，不阻塞 /api/context：

```json
{
  "suggestions": [
    { "text": "Push TASK-31 to ready.", "confidence": 0.85 },
    { "text": "commit this",            "confidence": 0.72 },
    { "text": "检查 tasks 状态。",       "confidence": 0.60 }
  ],
  "generated_at": "2026-06-29T01:00:08Z"
}
```

服务端：
- Server 增加 SuggestionFn 字段（签名：`func(ctx, hint string) ([]Suggestion, error)`）
- 内部维护一个带 TTL（30s）的 suggestions 缓存；缓存命中直接返回，miss 时异步触发 LLM 调用并立即返回上一次结果（首次返回空数组）
- LLM prompt：给定当前 hint（会话历史 + 工作状态），生成 2-3 条用户最可能发出的下一条指令，JSON 数组格式，每条含 text 和 confidence
- generated_at 独立于 updated_at，前端分别比较，互不影响

前端：
- 独立轮询 /api/suggestions，间隔 15s（比 /api/context 慢，LLM 生成本身需要时间）
- 比较 generated_at：相同则不重渲染 chip 区域
- 在录音按钮上方显示 2-3 个 suggestion chip（idle 状态可见，recording/reviewing 时隐藏）
- 点击 chip → 填入文字区 → 遵循当前发送模式（见 TASK-27）

依赖：TASK-25（updated_at 设计），session_source 导出结构化 turns。

---

**动态 Known Entities 补充（语音错误分析结论，与 TASK-28 共用逻辑）：**

真实的 ASR 失败模式是嵌入中文语流的英文技术词被音近替换（实测："Web" → "外部"）。这类词动态随对话主题变化，静态列表无法覆盖。

hint 组装时需从近期对话文本（最近 3-6 轮，user + assistant 双侧）提取高频英文技术词，动态追加到 Known Entities 段。SuggestionFn 接收的 hint 同样包含此动态补充段，有助于预测含英文技术词的下一条指令。

提取规则（纯文本，不需要 LLM，与 TASK-28 共用同一实现）：
- 匹配：连续英文字母序列，长度 ≥ 3
- 过滤：排除常见停用词（the、and、for、with 等）
- 频次阈值：在近期 3-6 轮对话中出现 ≥ 2 次
- 格式：`- Word: Word`（保持原始大小写）

注：之前版本的"注入最近 3 次 tool_use identifier 实体"方案已撤销——用户不直接说文件路径/函数名，对话文本本身是更直接的来源。
<!-- SECTION:DESCRIPTION:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
TASK-31 实验结论已纳入 Part B：SuggestionFn hint 中附加最近 3 次 tool_use identifier 实体，补充对话历史在短窗口下对函数名/变量名的覆盖不足；来源实现与 TASK-28 共用

2026-06-29 语音错误分析修正：Part B 撤销工具历史 identifier 注入方案，改为从近期对话文本提取高频英文技术词动态追加 Known Entities。与 TASK-28 共用同一实现。
<!-- SECTION:NOTES:END -->

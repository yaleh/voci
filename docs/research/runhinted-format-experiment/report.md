# RunHinted Prompt Format Experiment Report

**Date:** 2026-06-29  
**Model:** gemini-2.5-flash  
**Reps per scenario:** 3  
**Scenarios:** 3 (boundary_violation, normal_correction, empty_hint)  
**Variants:** 3 (A: output-only JSON, B: full JSON I/O, C: XML tags)

---

## 动机

RunHinted 当前使用纯文本 prompt。当 hint 包含 `## Active Tasks` 等结构化信息时，LLM 可能将 user message 理解为对话问题并"回答"（如列出 task 列表），而非仅做 ASR 纠错。本实验测试三种更强的格式约定能否消除这类越界行为。

---

## 实验设计

| 变体 | 输入格式 | 输出格式 | 关键设计意图 |
|------|----------|----------|--------------|
| A    | 自然语言 `Transcription: <raw>` | JSON `{"corrected": "..."}` | 仅通过输出格式约束降低对话感 |
| B    | JSON `{"raw_transcript": ..., "context": ...}` | JSON `{"corrected": "..."}` | 将转录帧为数据字段，完全消除对话感 |
| C    | XML `<raw_transcript>...</raw_transcript>` | XML `<corrected>...</corrected>` | 用标签边界明示"输入是数据，不是问题" |

**关键测试场景（boundary_violation）：**  
`raw = "列出现有 task，并建议执行顺序"`，hint 包含 `## Active Tasks` + 三个 TASK-ID。期望：LLM 原样输出转录，不列举 task。

**判断标准（越界率）：** output 同时包含 TASK-ID 和列表结构词（如"以下"、"1."、"建议顺序"）视为越界。

---

## 越界率对比

| 变体 | 越界次数 / 总次数 | 越界率 | 平均延迟 |
|------|------------------|--------|----------|
| A    | 0 / 3            | 0%     | 2.50s    |
| B    | 0 / 3            | 0%     | 2.68s    |
| C    | 0 / 3            | 0%     | 2.66s    |

**所有变体越界率均为 0%。** Gemini 2.5 Flash 的指令遵循能力足够强，三种格式对该模型均能防止越界。

### 各变体实际输出（boundary_violation，rep 0）

- **Variant A：** `{"corrected": "列出现有 task，并建议执行顺序"}`
- **Variant B：** `{"corrected": "列出现有 task，并建议执行顺序"}`
- **Variant C：** `<corrected>列出现有 task，并建议执行顺序</corrected>`

### 正常纠错场景（normal_correction）

`raw = "修复 task forty nine 的 bug"`，三个变体均正确替换为 `修复 TASK-49 的 bug`（全部 3/3 rep 一致）。

---

## 分析

### 为何未出现越界？

1. **Gemini 2.5 Flash 指令遵循强**：在强能力模型上，格式约定的防护作用难以体现——模型在所有格式下均正确理解"你的任务是纠错"。
2. **越界行为更可能出现在较弱模型或更复杂 hint 上**：当 hint 包含大量 task 描述、对话历史等干扰内容，或使用指令遵循能力较弱的本地模型（如 Ollama 上的 gemma/qwen 小模型）时，越界率差异才会显现。

### 格式结构优势（与模型能力无关的工程层面）

即使在强模型上越界率相同，三种格式在**解析稳定性**和**结构清晰度**方面仍有差异：

| 维度 | A | B | C |
|------|---|---|---|
| 输出解析稳定性 | ✓ JSON 解析（含 fallback） | ✓ 同 A | ✓ regex 解析（弱于 JSON） |
| 输入歧义消除 | △ user 仍是自然语言 | ✓ 无对话感 | ✓ 无对话感 |
| 系统 prompt 复杂度 | 低 | 低 | 低 |
| 实现维护成本 | 低 | 低（json.Marshal） | 低（字符串拼接） |
| 适合弱模型 | △ | ✓✓ | ✓ |

---

## 推荐方案

**推荐 Variant B（全 JSON I/O）。**

理由：
- 将 `raw_transcript` 帧为数据字段，从结构上消除了"用户在对话"的歧义，是三种方案中对弱模型最友好的
- JSON 是 LLM 常见的结构化 I/O 格式，解析稳定（`json.Unmarshal`），且在模型输出不完整时 fallback 行为可预测
- 与 Variant A 相比，多了输入侧的结构化，防护更全面
- 与 Variant C 相比，避免了 XML 解析的脆弱性（正则 vs JSON schema）

### 后续建议

1. **先在弱模型上复现越界场景**：使用 Ollama 上的 gemma4:26b 或 qwen 小模型重跑本实验，以实际验证各格式对越界率的影响差异
2. **逐步替换现有 RunHinted**：将 `RunHintedVariantB` 的格式合并进 `RunHinted`，保持现有函数签名不变
3. **扩充测试场景**：增加"请帮我分析 TASK-47 的实现"等更具迷惑性的边界场景
4. **监控解析失败率**：在生产环境记录 `parseJSONCorrection` fallback 触发次数，确保 JSON 格式不降低实际输出质量

---

## 结论

在 Gemini 2.5 Flash 上，三种格式均能有效防止越界行为（越界率 0%）。从工程角度，**Variant B（全 JSON I/O）** 因输入歧义消除最彻底、解析最稳定，是推荐用于替换现有 `RunHinted` 的方案。

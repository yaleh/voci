# ASR Pipeline Merge 联合实验报告（Config C + 合并调用）

## 实验背景

TASK-29 验证 Config C（few-shot 示例）是最优 hint format（entity_recall_exact=0.8944）。
TASK-42 验证合并单次调用可工程化（latency -32%），但使用的是简单指令格式（非 Config C）。
本实验（TASK-44）验证两者组合：Config C few-shot 嵌入合并 prompt，在完整 35 条语料上运行。

## 实验配置

- 模型：gemini-2.5-flash
- 测试用例：35 条（有效 35 条，parse_error 0 条）
- Prompt：merged_prompt_v2.txt（Config C few-shot + 三合一 JSON 输出）

## 质量对比

| 指标 | 基线（3 calls） | TASK-42（简单 prompt） | Config C + 合并 | delta vs 基线 |
|------|:--------------:|:---------------------:|:---------------:|:-------------:|
| rewrite_entity_recall | 0.6000 | — | 0.8857 | +0.2857 |
| classify_accuracy | 0.6286 | — | 0.5429 | -0.0857 |
| latency_total_ms（均值） | 10967.5 | 7459.8 | 8107.0 | -2860.5 |

## Latency 分析

- 基线三段流水线：10967.5 ms
- Config C + 合并调用：8107.0 ms
- latency delta：-2860.5 ms（-26.1%）

## JSON 解析失败率

- parse_error 用例：0 / 35 = 0.0%

## 结论

**质量损失不可接受，保留三段流水线**

### 判断依据

- rewrite_entity_recall delta = +0.2857（阈值：≥ -0.05）→ PASS
- classify_accuracy delta = -0.0857（阈值：≥ -0.05）→ FAIL

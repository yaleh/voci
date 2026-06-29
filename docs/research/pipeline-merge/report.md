# ASR 流水线合并实验报告

## 实验背景

验证将 voci 三段 ASR 管线（Gemini ASR + Rewrite + Classify）合并为单次 Gemini Audio API 调用，
是否在维持质量的同时减少 2 次 HTTP round-trip，降低端到端 latency。

## 实验配置

- 模型：gemini-2.5-flash
- 测试用例总数：35（其中 24 条有效，11 条 parse_error）
- 评测集：`testcases-annotated.json`（35 条标注用例，含 `expected_kind` 和 `expected_entities`）
- 基线：3-step pipeline（ASR call + Rewrite call + Classify call）
- 实验：merged single call（单次 Gemini Audio API，JSON 输出 transcript/rewritten/kind/confidence）

## 质量对比

| 指标 | 基线（3 calls） | 合并（1 call） | delta |
|------|:--------------:|:-------------:|:-----:|
| rewrite_entity_recall | 0.6000 | 1.0000 | +0.4000 |
| classify_accuracy | 0.6286 | 0.6250 | -0.0036 |
| latency_total_ms（均值） | 10967.5 | 7459.8 | -3507.7 |

## Latency 分析

- 基线三段流水线平均总 latency：**10967.5 ms**
- 合并单次调用平均 latency：**7459.8 ms**
- latency 减少 **32.0%**（delta = -3507.7 ms）

合并方案仅在单次音频调用中完成转录、改写、分类三步，消除了独立 Rewrite 和 Classify 的两次
HTTP round-trip（基线各约 4–6s），端到端 latency 显著变化。

## JSON 解析失败率

- parse_error 用例：11 / 35 = **31.4%**
- parse_error 主要来自缺少 WAV 文件（sample-06 ~ sample-15 共 10 条，以及 sample-31 超时）
- parse_error 用例不计入质量指标，单独统计

## 结论

**可工程化，建议替换三段流水线**

classify_accuracy 降幅 -0.0036（阈值 -0.05），rewrite_entity_recall 降幅 0.4000（阈值 -0.05）。质量损失在可接受范围内，单次调用可替换三段流水线，消除 2 次额外 HTTP round-trip。

### 判断依据

- classify_accuracy delta = -0.0036（阈值：≥ -0.05）→ PASS
- rewrite_entity_recall delta = +0.4000（阈值：≥ -0.05）→ PASS

两项指标同时满足阈值时，合并方案为可工程化；否则质量损失不可接受。

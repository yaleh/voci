# 火山引擎 Seed-ASR 2.0 实验分析报告

**实验日期**: 2026-06-29  
**模型**: volcengine (Seed-ASR 2.0, 大模型录音文件识别标准版, `volc.seedasr.auc`)  
**结果文件**: `run-20260629-172117-volcengine.jsonl`  
**有效识别用例**: 70 行（35 cases × hint_mode off/on），零错误

---

## 核心指标对比表

| 指标 | hint_mode=off | hint_mode=on | delta |
|------|:---:|:---:|:---:|
| entity_recall (mean) | 0.1250 | 0.1250 | **+0.0000** |
| entity_recall (median) | 0.00 | 0.00 | — |
| latency_s (mean) | 5.98s | 6.82s | +0.84s |
| 有效 entity_recall 样本数 | 28 | 28 | — |

> WER/CER 均为 null（testcases.json 的 `reference` 字段为空）。

---

## entity_recall delta 与判定

**delta (on − off) = +0.0000**

**判定：NO_EFFECT**

Seed-ASR 2.0 的内联热词注入（`request.corpus.context`）对 entity_recall **完全无效**，与 SiliconFlow/TeleSpeech 的 prompt 注入结果一致，delta = ±0。

判定阈值参考：

| delta | 判定 |
|---|---|
| ≥ 0.10 | BREAKTHROUGH |
| 0.05 ~ 0.10 | MARGINAL |
| < 0.05 | **NO_EFFECT** ← 本次 |

---

## 逐用例分析

28 个含实体的用例中，**所有 hint_mode on/off 对 entity_recall 完全相同**（Δ=0.00）。

典型失败模式：

| case | expected_entities | off 识别 | on 识别 |
|---|---|---|---|
| sample-01 | `["TASK-1","voci"]` | "vocal project" | "vocal project" |
| sample-06 | `["TASK-5","TASK-8"]` | "Task 5 and task 8" | "Task 5 and Task 8" |
| sample-08 | `["--atrade"]` | "ATRADE flag" | "ATRADE flag" |
| sample-11 | `["runHinted"]` | "run hintate function" | "run hintate function" |

热词注入对"TASK-N"（大写格式）、"voci"、Go 文件路径、`--flag` 等目标实体均无法纠正识别结果。hint_mode=on 相比 off 几乎逐字相同，唯一差异为极少数大小写变化（如 "task 8" → "Task 8"），不影响 entity_recall。

---

## 热词 API 可靠性

- 认证方式：新版控制台单一 `X-Api-Key`（`d109665a...`）
- 所有 70 次识别请求均成功（无 403/timeout）
- 内联热词格式：`request.corpus.context = json.dumps({"hotwords":[{"word":"..."}]})`
- 热词注入路径技术上成功传输，但对识别结果**无可观测影响**

### 参数结构穷举验证（2026-06-29 补充）

实验后对 `corpus` / `context` 字段结构进行了穷举测试，排除传参错误导致 NO_EFFECT 的可能性。

文档说明：`context`（level 3）嵌套在 `corpus`（level 2，`request` 子字段）下，值为 JSON-serialized string。

| 结构 | request body 片段 | 服务端响应 | 结果 |
|---|---|---|---|
| **A（当前）** | `corpus: {context: "{\"hotwords\":[...]}"}` | 200 OK | "vocal" / "task one"（无改善） |
| **B** | `context: "{\"hotwords\":[...]}"` 直接挂 request | 200 OK | 同上 |
| **C** | `corpus: "{\"hotwords\":[...]}"` 作字符串 | 报错 `need STRUCT type, got STRING` | — |
| **D** | `corpus: {}` + `context: "..."` 平级 | 200 OK | 同上 |
| **E** | 仅 `context: "..."` 无 corpus | 200 OK | 同上 |
| **F** | `corpus: {context: {hotwords:[...]}}` context 为 dict | 静默失败（无状态码） | — |

**结论**：`context` 必须为 JSON-serialized string（非 dict），`corpus` 必须为 struct（非 string）。所有被接受的结构（A/B/D/E）识别结果完全一致。NO_EFFECT 是模型行为，非传参错误。

---

## 结论与下一步建议

**NO_EFFECT — 策略 B 无效。**

火山引擎 Seed-ASR 2.0 的 WFST 热词偏置（内联注入）与 SiliconFlow 的 prompt 注入表现等同，均无法提升 entity_recall。根本原因可能是：

1. 内联 `hotwords` 注入在此 API 版本中权重不足以覆盖声学模型输出
2. 目标实体（`TASK-N`、`voci`、路径格式）的声学特征与语言模型先验差距过大

**后续建议**：

- 关闭火山引擎策略 B 方向，不推进生产集成
- 转向策略 C（WebSocket 流式 ASR + 解码器参数调优）或评估其他强热词支持的提供商（如讯飞、阿里云 NLS）
- 若继续使用 Seed-ASR，考虑在识别结果后置 LLM 纠错层代替解码器偏置

---

*本报告由 voci TASK-47 实验框架自动生成（2026-06-29）*

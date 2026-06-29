"""Analyze Config C + merged pipeline experiment results vs baseline and TASK-42."""
import json, pathlib

_this_dir = pathlib.Path(__file__).resolve().parent
BASELINE_PATH = _this_dir / "baseline.json"
TASK42_RESULTS_PATH = _this_dir / "results.jsonl"
RESULTS_V2_PATH = _this_dir / "results_v2.jsonl"
REPORT_PATH = _this_dir / "report_v2.md"


def entity_recall(expected_entities, text):
    if not expected_entities:
        return 1.0
    hits = sum(1 for e in expected_entities if e.lower() in text.lower())
    return hits / len(expected_entities)


def main():
    baseline = json.loads(BASELINE_PATH.read_text())
    v2_rows = [json.loads(l) for l in RESULTS_V2_PATH.read_text().splitlines() if l.strip()]
    # also load TASK-42 results for comparison
    try:
        t42_rows = [json.loads(l) for l in TASK42_RESULTS_PATH.read_text().splitlines() if l.strip()]
    except Exception:
        t42_rows = []

    # compute v2 metrics
    valid_v2 = [r for r in v2_rows if not r.get('parse_error')]
    parse_errors_v2 = len([r for r in v2_rows if r.get('parse_error')])

    # load testcases for entity recall
    tc_path = _this_dir / "testcases-annotated.json"
    testcases = {c['id']: c for c in json.loads(tc_path.read_text())}

    v2_entity_recalls = []
    v2_classify_hits = []
    v2_latencies = []
    for r in valid_v2:
        tc = testcases.get(r.get('case_id', ''), {})
        expected_kind = tc.get('expected_kind', '')
        expected_entities = tc.get('expected_entities', [])
        rewritten = r.get('rewritten', '')
        kind = r.get('kind', '')
        latency = r.get('latency_ms', 0)
        v2_entity_recalls.append(entity_recall(expected_entities, rewritten))
        v2_classify_hits.append(1 if kind == expected_kind else 0)
        v2_latencies.append(latency)

    n_valid = len(valid_v2)
    if n_valid == 0:
        print("ERROR: no valid results")
        return

    v2_entity_recall_mean = sum(v2_entity_recalls) / n_valid
    v2_classify_accuracy = sum(v2_classify_hits) / n_valid
    v2_latency_mean = sum(v2_latencies) / n_valid

    # deltas vs baseline
    bl_er = baseline['rewrite_entity_recall']
    bl_ca = baseline['classify_accuracy']
    bl_lat = baseline['latency_total_ms']

    delta_er = v2_entity_recall_mean - bl_er
    delta_ca = v2_classify_accuracy - bl_ca
    delta_lat = v2_latency_mean - bl_lat

    # conclusion
    if delta_er >= -0.05 and delta_ca >= -0.05:
        conclusion = "可工程化，建议替换三段流水线（Config C + 合并调用）"
        verdict = "PASS"
    else:
        conclusion = "质量损失不可接受，保留三段流水线"
        verdict = "FAIL"

    # write report
    report = f"""# ASR Pipeline Merge 联合实验报告（Config C + 合并调用）

## 实验背景

TASK-29 验证 Config C（few-shot 示例）是最优 hint format（entity_recall_exact=0.8944）。
TASK-42 验证合并单次调用可工程化（latency -32%），但使用的是简单指令格式（非 Config C）。
本实验（TASK-44）验证两者组合：Config C few-shot 嵌入合并 prompt，在完整 35 条语料上运行。

## 实验配置

- 模型：gemini-2.5-flash
- 测试用例：{len(v2_rows)} 条（有效 {n_valid} 条，parse_error {parse_errors_v2} 条）
- Prompt：merged_prompt_v2.txt（Config C few-shot + 三合一 JSON 输出）

## 质量对比

| 指标 | 基线（3 calls） | TASK-42（简单 prompt） | Config C + 合并 | delta vs 基线 |
|------|:--------------:|:---------------------:|:---------------:|:-------------:|
| rewrite_entity_recall | {bl_er:.4f} | — | {v2_entity_recall_mean:.4f} | {delta_er:+.4f} |
| classify_accuracy | {bl_ca:.4f} | — | {v2_classify_accuracy:.4f} | {delta_ca:+.4f} |
| latency_total_ms（均值） | {bl_lat:.1f} | 7459.8 | {v2_latency_mean:.1f} | {delta_lat:+.1f} |

## Latency 分析

- 基线三段流水线：{bl_lat:.1f} ms
- Config C + 合并调用：{v2_latency_mean:.1f} ms
- latency delta：{delta_lat:+.1f} ms（{delta_lat/bl_lat*100:+.1f}%）

## JSON 解析失败率

- parse_error 用例：{parse_errors_v2} / {len(v2_rows)} = {parse_errors_v2/len(v2_rows)*100:.1f}%

## 结论

**{conclusion}**

### 判断依据

- rewrite_entity_recall delta = {delta_er:+.4f}（阈值：≥ -0.05）→ {"PASS" if delta_er >= -0.05 else "FAIL"}
- classify_accuracy delta = {delta_ca:+.4f}（阈值：≥ -0.05）→ {"PASS" if delta_ca >= -0.05 else "FAIL"}
"""
    REPORT_PATH.write_text(report)
    print(report)
    print(f"Wrote {REPORT_PATH}")


if __name__ == "__main__":
    main()

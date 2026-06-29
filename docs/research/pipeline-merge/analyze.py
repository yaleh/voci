"""Analyze merged-pipeline experiment results vs 3-step baseline."""
import json, pathlib, sys

_this_dir = pathlib.Path(__file__).resolve().parent
BASELINE_PATH = _this_dir / "baseline.json"
RESULTS_PATH = _this_dir / "results.jsonl"
ANNOTATED_PATH = _this_dir / "testcases-annotated.json"
REPORT_PATH = _this_dir / "report.md"


def entity_recall(expected_entities, text):
    if not expected_entities:
        return 1.0
    hits = sum(1 for e in expected_entities if e.lower() in text.lower())
    return hits / len(expected_entities)


def main():
    baseline = json.loads(BASELINE_PATH.read_text())
    rows = [json.loads(l) for l in RESULTS_PATH.read_text().splitlines() if l.strip()]
    cases = {c["id"]: c for c in json.loads(ANNOTATED_PATH.read_text())}

    # Separate parse-error rows
    valid_rows = [r for r in rows if not r.get("parse_error", False)]
    parse_error_count = sum(1 for r in rows if r.get("parse_error", False))
    parse_error_rate = parse_error_count / len(rows) if rows else 0.0

    # Compute merged metrics on valid rows only
    rewrite_recall_vals = []
    classify_correct = []
    latency_vals = []

    for r in valid_rows:
        case_id = r["case_id"]
        c = cases.get(case_id, {})
        expected_entities = c.get("expected_entities", [])
        expected_kind = c.get("expected_kind", "ambiguous")

        rewritten = r.get("rewritten", "")
        kind = r.get("kind", "ambiguous")
        latency_ms = r.get("latency_ms", 0.0)

        rewrite_recall_vals.append(entity_recall(expected_entities, rewritten))
        classify_correct.append(1 if kind == expected_kind else 0)
        latency_vals.append(latency_ms)

    n_valid = len(valid_rows)
    if n_valid == 0:
        print("ERROR: No valid rows to analyze", file=sys.stderr)
        sys.exit(1)

    merged_rewrite_entity_recall = sum(rewrite_recall_vals) / n_valid
    merged_classify_accuracy = sum(classify_correct) / n_valid
    merged_latency_total_ms = sum(latency_vals) / n_valid

    # Baseline values
    baseline_rewrite_entity_recall = baseline["rewrite_entity_recall"]
    baseline_classify_accuracy = baseline["classify_accuracy"]
    baseline_latency_total_ms = baseline["latency_total_ms"]

    # Deltas (merged - baseline)
    delta_rewrite_entity_recall = merged_rewrite_entity_recall - baseline_rewrite_entity_recall
    delta_classify_accuracy = merged_classify_accuracy - baseline_classify_accuracy
    delta_latency_total_ms = merged_latency_total_ms - baseline_latency_total_ms

    # Conclusion logic
    if delta_classify_accuracy >= -0.05 and delta_rewrite_entity_recall >= -0.05:
        conclusion = "可工程化，建议替换三段流水线"
        conclusion_detail = (
            "classify_accuracy 降幅 {:.4f}（阈值 -0.05），rewrite_entity_recall 降幅 {:.4f}（阈值 -0.05）。"
            "质量损失在可接受范围内，单次调用可替换三段流水线，消除 2 次额外 HTTP round-trip。"
        ).format(delta_classify_accuracy, delta_rewrite_entity_recall)
    else:
        conclusion = "质量损失不可接受，保留三段流水线"
        conclusion_detail = (
            "classify_accuracy 降幅 {:.4f}（阈值 -0.05），rewrite_entity_recall 降幅 {:.4f}（阈值 -0.05）。"
            "质量损失超出阈值，建议保留三段流水线。"
        ).format(delta_classify_accuracy, delta_rewrite_entity_recall)

    # Print comparison table
    print("=" * 70)
    print("ASR 流水线合并实验：单次 Gemini 调用 vs 三段流水线")
    print("=" * 70)
    print(f"{'指标':<30} {'基线(3 calls)':>15} {'合并(1 call)':>15} {'delta':>10}")
    print("-" * 70)
    print(f"{'rewrite_entity_recall':<30} {baseline_rewrite_entity_recall:>15.4f} {merged_rewrite_entity_recall:>15.4f} {delta_rewrite_entity_recall:>+10.4f}")
    print(f"{'classify_accuracy':<30} {baseline_classify_accuracy:>15.4f} {merged_classify_accuracy:>15.4f} {delta_classify_accuracy:>+10.4f}")
    print(f"{'latency_total_ms':<30} {baseline_latency_total_ms:>15.1f} {merged_latency_total_ms:>15.1f} {delta_latency_total_ms:>+10.1f}")
    print("-" * 70)
    print(f"总用例数: {len(rows)}  有效用例: {n_valid}  parse_error: {parse_error_count} ({parse_error_rate:.1%})")
    print()
    print(f"结论: {conclusion}")
    print(conclusion_detail)

    # Write report.md
    latency_pct = (
        f"{abs(delta_latency_total_ms / baseline_latency_total_ms) * 100:.1f}%"
        if baseline_latency_total_ms > 0 else "N/A"
    )
    latency_direction = "减少" if delta_latency_total_ms < 0 else "增加"

    report = f"""# ASR 流水线合并实验报告

## 实验背景

验证将 voci 三段 ASR 管线（Gemini ASR + Rewrite + Classify）合并为单次 Gemini Audio API 调用，
是否在维持质量的同时减少 2 次 HTTP round-trip，降低端到端 latency。

## 实验配置

- 模型：gemini-2.5-flash
- 测试用例总数：{len(rows)}（其中 {n_valid} 条有效，{parse_error_count} 条 parse_error）
- 评测集：`testcases-annotated.json`（35 条标注用例，含 `expected_kind` 和 `expected_entities`）
- 基线：3-step pipeline（ASR call + Rewrite call + Classify call）
- 实验：merged single call（单次 Gemini Audio API，JSON 输出 transcript/rewritten/kind/confidence）

## 质量对比

| 指标 | 基线（3 calls） | 合并（1 call） | delta |
|------|:--------------:|:-------------:|:-----:|
| rewrite_entity_recall | {baseline_rewrite_entity_recall:.4f} | {merged_rewrite_entity_recall:.4f} | {delta_rewrite_entity_recall:+.4f} |
| classify_accuracy | {baseline_classify_accuracy:.4f} | {merged_classify_accuracy:.4f} | {delta_classify_accuracy:+.4f} |
| latency_total_ms（均值） | {baseline_latency_total_ms:.1f} | {merged_latency_total_ms:.1f} | {delta_latency_total_ms:+.1f} |

## Latency 分析

- 基线三段流水线平均总 latency：**{baseline_latency_total_ms:.1f} ms**
- 合并单次调用平均 latency：**{merged_latency_total_ms:.1f} ms**
- latency {latency_direction} **{latency_pct}**（delta = {delta_latency_total_ms:+.1f} ms）

合并方案仅在单次音频调用中完成转录、改写、分类三步，消除了独立 Rewrite 和 Classify 的两次
HTTP round-trip（基线各约 4–6s），端到端 latency 显著变化。

## JSON 解析失败率

- parse_error 用例：{parse_error_count} / {len(rows)} = **{parse_error_rate:.1%}**
- parse_error 主要来自缺少 WAV 文件（sample-06 ~ sample-15 共 10 条，以及 sample-31 超时）
- parse_error 用例不计入质量指标，单独统计

## 结论

**{conclusion}**

{conclusion_detail}

### 判断依据

- classify_accuracy delta = {delta_classify_accuracy:+.4f}（阈值：≥ -0.05）→ {"PASS" if delta_classify_accuracy >= -0.05 else "FAIL"}
- rewrite_entity_recall delta = {delta_rewrite_entity_recall:+.4f}（阈值：≥ -0.05）→ {"PASS" if delta_rewrite_entity_recall >= -0.05 else "FAIL"}

两项指标同时满足阈值时，合并方案为可工程化；否则质量损失不可接受。
"""

    REPORT_PATH.write_text(report, encoding="utf-8")
    print(f"\nReport written to {REPORT_PATH}")


if __name__ == "__main__":
    main()

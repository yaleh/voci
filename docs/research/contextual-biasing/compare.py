"""Compare baseline vs method_a vs method_b contextual biasing results.

Usage:
    python compare.py --baseline <jsonl> --method-a <jsonl> --method-b <jsonl> --out <dir>
"""
import argparse
import json
import pathlib
from datetime import datetime


CATEGORIES = ["zh-technical", "zh-mixed"]


def load_rows(path, method_name):
    """Load JSONL and normalize to common schema."""
    rows = []
    with open(path) as f:
        for line in f:
            line = line.strip()
            if not line:
                continue
            r = json.loads(line)

            # Normalize category: list → string
            cat = r.get("category", "")
            if isinstance(cat, list):
                cat = cat[0] if cat else ""
            r["category"] = cat

            # Baseline rows use entity_recall field; remap to entity_recall_exact
            if "entity_recall" in r and "entity_recall_exact" not in r:
                r["entity_recall_exact"] = r["entity_recall"]
                r["entity_recall_fuzzy"] = None

            # Set method label
            r["_method"] = method_name

            rows.append(r)
    return rows


def mean(values):
    vals = [v for v in values if v is not None]
    if not vals:
        return None
    return sum(vals) / len(vals)


def compute_stats(rows):
    return {
        "entity_recall_exact": mean([r.get("entity_recall_exact") for r in rows]),
        "entity_recall_fuzzy": mean([r.get("entity_recall_fuzzy") for r in rows]),
        "latency_s": mean([r.get("latency_s") for r in rows]),
        "n": len(rows),
    }


def fmt(val, digits=3):
    if val is None:
        return "N/A"
    return f"{val:.{digits}f}"


def build_table(groups, method_names):
    """Build a markdown table."""
    header = "| group | method | n | entity_recall_exact | entity_recall_fuzzy | latency_s |"
    sep = "|---|---|---|---|---|---|"
    lines = [header, sep]
    for group in ["all"] + CATEGORIES:
        for method in method_names:
            stats = groups.get((group, method))
            if stats is None:
                continue
            lines.append(
                f"| {group} | {method} | {stats['n']} "
                f"| {fmt(stats['entity_recall_exact'])} "
                f"| {fmt(stats['entity_recall_fuzzy'])} "
                f"| {fmt(stats['latency_s'])} |"
            )
    return "\n".join(lines)


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--baseline", required=True)
    parser.add_argument("--method-a", required=True)
    parser.add_argument("--method-b", required=True)
    parser.add_argument("--out", required=True)
    args = parser.parse_args()

    baseline_rows = load_rows(args.baseline, "baseline")
    method_a_rows = load_rows(args.method_a, "method_a")
    method_b_rows = load_rows(args.method_b, "method_b")

    # Filter baseline to only rows that have known_entities (entity_recall_exact is not None)
    baseline_rows = [r for r in baseline_rows if r.get("entity_recall_exact") is not None]

    all_rows = baseline_rows + method_a_rows + method_b_rows

    method_names = ["baseline", "method_a", "method_b"]

    # Compute stats per group per method
    groups = {}
    for method_name, rows in [("baseline", baseline_rows), ("method_a", method_a_rows), ("method_b", method_b_rows)]:
        # all
        groups[("all", method_name)] = compute_stats(rows)
        for cat in CATEGORIES:
            cat_rows = [r for r in rows if r.get("category") == cat]
            if cat_rows:
                groups[(cat, method_name)] = compute_stats(cat_rows)

    table = build_table(groups, method_names)

    ts = datetime.now().strftime("%Y%m%d-%H%M%S")
    out_dir = pathlib.Path(args.out)
    out_dir.mkdir(parents=True, exist_ok=True)
    report_path = out_dir / f"report-{ts}.md"

    report = f"""# Contextual Biasing Experiment Report

Generated: {datetime.now().isoformat()}

## Methods

- **baseline**: whisper-large-v3, no prompt
- **method_a**: single-pass, inject `known_entities` as `prompt` field
- **method_b**: two-step — raw ASR → fuzzy match → re-ASR with matched candidates as `prompt`

## Results

{table}

## Notes

- `entity_recall_exact`: fraction of known_entities found verbatim (case-insensitive substring) in hypothesis
- `entity_recall_fuzzy`: fraction of known_entities matched via fuzzy similarity (threshold=0.3) in hypothesis words
- `latency_s`: total wall-clock seconds for transcription (method_b may double for matched entities)
- Groups: `zh-technical` = technical Chinese terms, `zh-mixed` = mixed Chinese+English
"""

    report_path.write_text(report, encoding="utf-8")
    print(f"Wrote {report_path}")
    print(table)


if __name__ == "__main__":
    main()

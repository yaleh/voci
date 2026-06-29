#!/usr/bin/env python3
"""
Phase 2: Compute utilization(K) for K=1..6.
utilization(K) = |entities_in_tools ∩ entities_in_context_K| / |entities_in_tools|
where entities_in_context_K = extracted from prior_turns[:K]
"""

import json
import re
import sys
from pathlib import Path
from collections import defaultdict

OUTPUT_DIR = Path("/home/yale/work/baime-TASK-30/docs/research/context-experiment")

def extract_entities_from_text(text):
    """Extract entities from text content."""
    entities = set()
    if not text:
        return entities

    # File paths containing /
    paths = re.findall(r'[\w./\-]+(?:/[\w./\-]+)+', text)
    for p in paths:
        if len(p) > 3:
            entities.add(p)

    # Files with extensions
    ext_match = re.findall(r'[\w\-]+\.(?:go|py|md|jsonl|ts|sh|txt|json)', text)
    entities.update(ext_match)

    # --flags
    flags = re.findall(r'--[\w\-]+', text)
    entities.update(flags)

    # TASK-XX IDs
    task_ids = re.findall(r'TASK-\d+', text)
    entities.update(task_ids)

    # backtick-quoted identifiers
    cmd_match = re.findall(r'`([\w\-\.]+)`', text)
    entities.update(m for m in cmd_match if len(m) > 2)

    return entities

def extract_entities_from_tool_uses(tool_uses):
    """Extract entities from a list of tool_use objects (same logic as Phase 1)."""
    entities = set()
    for tool in tool_uses:
        name = tool.get('name', '')
        inp = tool.get('input', {})

        if name:
            entities.add(name)

        for key, val in inp.items():
            if not isinstance(val, str):
                continue
            if '/' in val:
                parts = re.findall(r'[\w./\-]+(?:/[\w./\-]+)+', val)
                for part in parts:
                    if len(part) > 3:
                        entities.add(part)
            ext_match = re.findall(r'[\w\-]+\.(?:go|py|md|jsonl|ts|sh|txt|json)', val)
            entities.update(ext_match)
            if key == 'command':
                cmd_parts = val.strip().split()
                if cmd_parts:
                    entities.add(cmd_parts[0])
                flags = re.findall(r'--[\w\-]+', val)
                entities.update(flags)
            task_ids = re.findall(r'TASK-\d+', val)
            entities.update(task_ids)

    return entities

def entities_in_context_k(prior_turns, k):
    """Get all entities from prior_turns[:k]."""
    entities = set()
    for turn in prior_turns[:k]:
        text = turn.get('text', '')
        entities.update(extract_entities_from_text(text))
        # Also from tool_uses in those turns
        for tu in turn.get('tool_uses', []):
            name = tu.get('name', '')
            if name:
                entities.add(name)
            inp = tu.get('input', {})
            for key, val in inp.items():
                if isinstance(val, str):
                    entities.update(extract_entities_from_text(val))
    return entities

def compute_utilization(entities_tools, entities_context):
    """Compute overlap ratio."""
    if not entities_tools:
        return 0.0
    overlap = entities_tools & entities_context
    return len(overlap) / len(entities_tools)

def main():
    if len(sys.argv) < 2:
        triples_file = OUTPUT_DIR / "triples.jsonl"
    else:
        triples_file = Path(sys.argv[1])

    print(f"Reading triples from {triples_file}")

    records = []
    with open(triples_file, encoding='utf-8') as f:
        for line in f:
            line = line.strip()
            if line:
                records.append(json.loads(line))

    print(f"Loaded {len(records)} triples")

    # Compute utilization curves
    curve_records = []

    for rec in records:
        entities_tools = set(rec.get('entities_in_tools', []))
        if not entities_tools:
            # Recompute from tool_uses
            entities_tools = extract_entities_from_tool_uses(rec.get('tool_uses', []))

        if not entities_tools:
            continue  # skip if no entities to measure

        prior_turns = rec.get('prior_turns', [])
        uvals = {}
        for k in range(1, 7):
            ctx_entities = entities_in_context_k(prior_turns, k)
            uvals[f'u{k}'] = round(compute_utilization(entities_tools, ctx_entities), 4)

        curve_records.append({
            'session_id': rec['session_id'],
            'turn': rec['turn'],
            'source_project': rec['source_project'],
            'user_message_len': len(rec.get('user_message', '')),
            'entities_in_tools_count': len(entities_tools),
            **uvals
        })

    print(f"Computed curves for {len(curve_records)} records")

    # Write utilization-curve.jsonl
    curve_file = OUTPUT_DIR / "utilization-curve.jsonl"
    with open(curve_file, 'w', encoding='utf-8') as f:
        for rec in curve_records:
            f.write(json.dumps(rec) + '\n')
    print(f"Wrote {curve_file}")

    # Compute statistics
    stats = defaultdict(lambda: defaultdict(list))
    overall = defaultdict(list)

    for rec in curve_records:
        proj = rec['source_project']
        for k in range(1, 7):
            uk = rec[f'u{k}']
            stats[proj][f'u{k}'].append(uk)
            overall[f'u{k}'].append(uk)

    def mean(lst):
        return round(sum(lst) / len(lst), 4) if lst else 0.0

    def pct(lst, p):
        if not lst:
            return 0.0
        sorted_lst = sorted(lst)
        idx = int(len(sorted_lst) * p / 100)
        idx = min(idx, len(sorted_lst) - 1)
        return round(sorted_lst[idx], 4)

    # Build report
    report_lines = ["# Context Utilization Analysis Report\n"]
    report_lines.append(f"Total records analyzed: {len(curve_records)}\n")

    # Utilization Curve section
    report_lines.append("## Utilization Curve\n")
    report_lines.append("Mean utilization(K) — fraction of tool-use entities appearing in prior K turns:\n")
    report_lines.append("| K | Overall Mean | voci Mean | baime Mean | Overall P50 | Overall P90 |")
    report_lines.append("|---|---|---|---|---|---|")
    for k in range(1, 7):
        uk = f'u{k}'
        ov_mean = mean(overall[uk])
        voci_mean = mean(stats['voci'][uk])
        baime_mean = mean(stats['baime'][uk])
        ov_p50 = pct(overall[uk], 50)
        ov_p90 = pct(overall[uk], 90)
        report_lines.append(f"| {k} | {ov_mean} | {voci_mean} | {baime_mean} | {ov_p50} | {ov_p90} |")

    report_lines.append("\n")

    # Summary Table section
    report_lines.append("## Summary Table\n")
    report_lines.append(f"- **Total triples**: {len(curve_records)}\n")
    report_lines.append(f"- **voci triples**: {sum(1 for r in curve_records if r['source_project'] == 'voci')}\n")
    report_lines.append(f"- **baime triples**: {sum(1 for r in curve_records if r['source_project'] == 'baime')}\n")

    # Mean u6 (max context)
    mean_u6 = mean(overall['u6'])
    mean_u1 = mean(overall['u1'])
    mean_u3 = mean(overall['u3'])
    report_lines.append(f"- **Mean u1** (only 1 prior turn): {mean_u1}\n")
    report_lines.append(f"- **Mean u3** (3 prior turns): {mean_u3}\n")
    report_lines.append(f"- **Mean u6** (6 prior turns): {mean_u6}\n")

    # Incremental gain analysis
    report_lines.append("\n### Incremental Gain from Adding Turns\n")
    report_lines.append("| Turns Added | Marginal Gain | Cumulative Mean |")
    report_lines.append("|---|---|---|")
    prev_mean = 0.0
    for k in range(1, 7):
        uk = f'u{k}'
        curr_mean = mean(overall[uk])
        gain = round(curr_mean - prev_mean, 4)
        report_lines.append(f"| K={k} | +{gain} | {curr_mean} |")
        prev_mean = curr_mean

    report_lines.append("\n### Key Findings\n")

    # Calculate when 80% of max utilization is reached
    max_u = mean_u6
    for k in range(1, 7):
        curr = mean(overall[f'u{k}'])
        if max_u > 0 and curr / max_u >= 0.8:
            report_lines.append(f"- 80% of max utilization reached at K={k} (u{k}={curr}, u6={max_u})\n")
            break

    # Calculate zero-context fraction (u1=0)
    zero_u1 = sum(1 for r in curve_records if r['u1'] == 0.0)
    report_lines.append(f"- Fraction with u1=0 (no entity overlap at K=1): {round(zero_u1/len(curve_records)*100, 1)}%\n")

    zero_u6 = sum(1 for r in curve_records if r['u6'] == 0.0)
    report_lines.append(f"- Fraction with u6=0 (no entity overlap even at K=6): {round(zero_u6/len(curve_records)*100, 1)}%\n")

    # Short message stats
    short_msgs = [r for r in curve_records if r['user_message_len'] < 100]
    if short_msgs:
        short_u6 = mean([r['u6'] for r in short_msgs])
        report_lines.append(f"- Short messages (<100 chars): {len(short_msgs)} ({round(len(short_msgs)/len(curve_records)*100, 1)}%), mean u6={short_u6}\n")

    report_lines.append(f"\n### Phase 4 Decision\n")
    report_lines.append(f"Mean u6 = {mean_u6}\n")
    if mean_u6 >= 0.7:
        report_lines.append("**Decision: SKIP Phase 4 ablation** (mean u6 >= 0.7, context utilization is high enough)\n")
    else:
        report_lines.append("**Decision: RUN Phase 4 ablation** (mean u6 < 0.7, further investigation warranted)\n")

    report_content = '\n'.join(report_lines)

    report_file = OUTPUT_DIR / "utilization-report.md"
    with open(report_file, 'w', encoding='utf-8') as f:
        f.write(report_content)
    print(f"Wrote {report_file}")
    print(f"\nMean u6 = {mean_u6}")

    return mean_u6

if __name__ == '__main__':
    mean_u6 = main()
    sys.exit(0)

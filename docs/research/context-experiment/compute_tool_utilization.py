#!/usr/bin/env python3
"""
Phase 1: Tool-use Utilization Curves
Computes tool_util(K) = |entities_after ∩ prior_K_tool_entities| / |entities_after|
for K=1..10, comparing against dialogue utilization.
"""

import json
import re
import os
from pathlib import Path
from collections import defaultdict

SCRIPT_DIR = Path(__file__).parent
TRIPLES_FILE = SCRIPT_DIR / "triples.jsonl"
TOOL_CURVE_FILE = SCRIPT_DIR / "tool-utilization-curve.jsonl"
TOOL_CACHE_FILE = SCRIPT_DIR / "tool-history-cache.jsonl"
DIALOGUE_CURVE_FILE = SCRIPT_DIR / "utilization-curve.jsonl"
REPORT_FILE = SCRIPT_DIR / "tool-utilization-report.md"

PROJECTS = {
    "voci": Path.home() / ".claude/projects/-home-yale-work-voci",
    "baime": Path.home() / ".claude/projects/-home-yale-work-baime",
}


def extract_entities_typed(tool_uses):
    file_paths, commands, identifiers = set(), set(), set()
    for tu in tool_uses:
        name = tu.get("name", "")
        inp = tu.get("input", {}) or {}
        if name:
            identifiers.add(name)
        for key, val in inp.items():
            if not isinstance(val, str):
                continue
            if val.startswith("/"):
                file_paths.add(val)
            paths = re.findall(r'[\w./\-]+(?:/[\w./\-]+)+', val)
            for p in paths:
                if len(p) > 3:
                    file_paths.add(p)
            exts = re.findall(r'[\w\-]+\.(?:go|py|md|jsonl|ts|sh|txt|json)', val)
            file_paths.update(exts)
            if key == "command":
                tokens = val.strip().split()
                if tokens:
                    commands.add(tokens[0])
            flags = re.findall(r'--[\w\-]+', val)
            identifiers.update(flags)
    return {"file_path": file_paths, "command": commands, "identifier": identifiers}


def extract_entities_flat(tool_uses):
    """Returns flat set of all entities (for compatibility with entities_in_tools)."""
    typed = extract_entities_typed(tool_uses)
    result = set()
    for s in typed.values():
        result.update(s)
    return result


def load_session_tool_history(session_path, target_turn):
    """
    Parse session JSONL and collect tool_use blocks from assistant entries
    that appear before the Nth user entry (0-indexed).
    Returns list of tool_use dicts in chronological order.
    """
    tool_history = []
    user_count = 0

    try:
        with open(session_path) as f:
            for line in f:
                line = line.strip()
                if not line:
                    continue
                obj = json.loads(line)
                obj_type = obj.get("type", "")

                if obj_type == "assistant":
                    msg = obj.get("message", {})
                    content = msg.get("content", [])
                    if isinstance(content, list):
                        for block in content:
                            if isinstance(block, dict) and block.get("type") == "tool_use":
                                tool_history.append({
                                    "name": block.get("name", ""),
                                    "input": block.get("input", {})
                                })

                elif obj_type == "user":
                    if user_count == target_turn:
                        # We've hit the target turn; stop
                        break
                    user_count += 1

    except (FileNotFoundError, json.JSONDecodeError) as e:
        pass

    return tool_history


def compute_tool_util(entities_after, prior_tool_entities):
    """Compute utilization: overlap / |entities_after|."""
    if not entities_after:
        return None
    overlap = len(set(entities_after) & set(prior_tool_entities))
    return overlap / len(entities_after)


def main():
    print("Loading triples...")
    triples = []
    with open(TRIPLES_FILE) as f:
        for line in f:
            triples.append(json.loads(line))
    print(f"Loaded {len(triples)} triples")

    # Process each triple
    curve_records = []
    cache_records = []
    skipped = 0
    not_found = 0

    for i, triple in enumerate(triples):
        if i % 500 == 0:
            print(f"  Processing {i}/{len(triples)}...")

        session_id = triple["session_id"]
        turn = triple["turn"]
        source_project = triple["source_project"]
        entities_after = triple.get("entities_in_tools", [])

        if not entities_after:
            skipped += 1
            continue

        # Find session file
        project_dir = PROJECTS.get(source_project)
        if project_dir is None:
            skipped += 1
            continue

        session_path = project_dir / f"{session_id}.jsonl"
        if not session_path.exists():
            not_found += 1
            continue

        # Load prior tool history
        tool_history = load_session_tool_history(session_path, turn)

        # Compute utilization for K=1..10
        t_vals = {}
        for k in range(1, 11):
            last_k = tool_history[-k:] if len(tool_history) >= k else tool_history
            prior_entities = extract_entities_flat(last_k)
            util = compute_tool_util(entities_after, prior_entities)
            t_vals[f"t{k}"] = util if util is not None else 0.0

        curve_record = {
            "session_id": session_id,
            "turn": turn,
            "source_project": source_project,
            "entities_in_tools_count": len(entities_after),
        }
        curve_record.update(t_vals)
        curve_records.append(curve_record)

        # Cache k=3 and k=6
        last_3 = tool_history[-3:] if len(tool_history) >= 3 else tool_history
        last_6 = tool_history[-6:] if len(tool_history) >= 6 else tool_history
        entities_k3 = list(extract_entities_flat(last_3))
        entities_k6 = list(extract_entities_flat(last_6))

        cache_records.append({
            "session_id": session_id,
            "turn": turn,
            "source_project": source_project,
            "prior_tool_entities_k3": entities_k3,
            "prior_tool_entities_k6": entities_k6,
        })

    print(f"Computed {len(curve_records)} records (skipped={skipped}, not_found={not_found})")

    # Write output files
    with open(TOOL_CURVE_FILE, "w") as f:
        for r in curve_records:
            f.write(json.dumps(r) + "\n")
    print(f"Written: {TOOL_CURVE_FILE}")

    with open(TOOL_CACHE_FILE, "w") as f:
        for r in cache_records:
            f.write(json.dumps(r) + "\n")
    print(f"Written: {TOOL_CACHE_FILE}")

    # Load dialogue curve for comparison
    dialogue_records = []
    if DIALOGUE_CURVE_FILE.exists():
        with open(DIALOGUE_CURVE_FILE) as f:
            for line in f:
                dialogue_records.append(json.loads(line))
    print(f"Loaded {len(dialogue_records)} dialogue curve records")

    # Compute mean utilization by K and project
    def mean_by_k(records, prefix, k_range):
        by_project = defaultdict(lambda: defaultdict(list))
        overall = defaultdict(list)
        for r in records:
            proj = r.get("source_project", "unknown")
            for k in k_range:
                key = f"{prefix}{k}"
                if key in r and r[key] is not None:
                    by_project[proj][k].append(r[key])
                    overall[k].append(r[key])
        return overall, by_project

    tool_overall, tool_by_proj = mean_by_k(curve_records, "t", range(1, 11))
    dial_overall, dial_by_proj = mean_by_k(dialogue_records, "u", range(1, 7))

    def safe_mean(lst):
        return sum(lst) / len(lst) if lst else float("nan")

    # Generate report
    report_lines = ["# Tool-use Utilization Report\n"]
    report_lines.append("## Tool vs Dialogue Comparison\n")
    report_lines.append(
        "Mean utilization at each K (tool = last K tool_use blocks; dialogue = last K user turns)\n"
    )
    report_lines.append("| K | mean tool_util(K) | mean dialogue_util(K) |")
    report_lines.append("|---|-------------------|-----------------------|")

    first_tool_beats_dialogue = None
    for k in range(1, 11):
        tool_mean = safe_mean(tool_overall.get(k, []))
        dial_mean = safe_mean(dial_overall.get(k, [])) if k <= 6 else float("nan")
        dial_str = f"{dial_mean:.4f}" if k <= 6 else "—"
        marker = ""
        if k <= 6 and not first_tool_beats_dialogue and tool_mean > dial_mean:
            first_tool_beats_dialogue = k
            marker = " ← tool first beats dialogue"
        report_lines.append(f"| {k} | {tool_mean:.4f} | {dial_str} |{marker}")

    if first_tool_beats_dialogue:
        report_lines.append(
            f"\n**Tool utilization first exceeds dialogue at K={first_tool_beats_dialogue}**\n"
        )
    else:
        report_lines.append(
            "\n**Tool utilization does not exceed dialogue within K=1..6**\n"
        )

    report_lines.append("\n### Per-Project Breakdown\n")
    all_projects = set(list(tool_by_proj.keys()) + list(dial_by_proj.keys()))
    for proj in sorted(all_projects):
        report_lines.append(f"\n#### {proj}\n")
        report_lines.append("| K | tool_util(K) | dialogue_util(K) |")
        report_lines.append("|---|--------------|------------------|")
        for k in range(1, 11):
            t_mean = safe_mean(tool_by_proj[proj].get(k, []))
            d_mean = safe_mean(dial_by_proj[proj].get(k, [])) if k <= 6 else float("nan")
            d_str = f"{d_mean:.4f}" if k <= 6 else "—"
            report_lines.append(f"| {k} | {t_mean:.4f} | {d_str} |")

    report_content = "\n".join(report_lines) + "\n"
    with open(REPORT_FILE, "w") as f:
        f.write(report_content)
    print(f"Written: {REPORT_FILE}")

    print("\nSummary:")
    for k in range(1, 11):
        t = safe_mean(tool_overall.get(k, []))
        d = safe_mean(dial_overall.get(k, [])) if k <= 6 else float("nan")
        print(f"  K={k}: tool={t:.4f}", end="")
        if k <= 6:
            print(f" dialogue={d:.4f}")
        else:
            print()


if __name__ == "__main__":
    main()

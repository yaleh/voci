#!/usr/bin/env python3
"""
Phase 2: Active-Set Half-Life
Computes how quickly the active entity set changes over session turns.
"""

import json
import statistics
from pathlib import Path
from collections import defaultdict

SCRIPT_DIR = Path(__file__).parent
TRIPLES_FILE = SCRIPT_DIR / "triples.jsonl"
DECAY_FILE = SCRIPT_DIR / "active-set-decay.jsonl"
REPORT_FILE = SCRIPT_DIR / "tool-utilization-report.md"

DELTAS = [1, 2, 3, 5, 10, 20]
WINDOW = 5  # 5-turn window for active_set


def jaccard_overlap(set_a, set_b):
    """Compute Jaccard similarity between two sets."""
    if not set_a and not set_b:
        return 1.0
    union = set_a | set_b
    if not union:
        return 0.0
    return len(set_a & set_b) / len(union)


def mean_overlap(set_a, set_b):
    """Compute mean overlap: |A ∩ B| / |A| (fraction of A that survives in B)."""
    if not set_a:
        return None
    return len(set_a & set_b) / len(set_a)


def main():
    print("Loading triples...")
    triples = []
    with open(TRIPLES_FILE) as f:
        for line in f:
            triples.append(json.loads(line))
    print(f"Loaded {len(triples)} triples")

    # Group by session, sorted by turn
    sessions = defaultdict(list)
    for triple in triples:
        sid = triple["session_id"]
        sessions[sid].append((triple["turn"], set(triple.get("entities_in_tools", []))))

    # Sort each session by turn
    for sid in sessions:
        sessions[sid].sort(key=lambda x: x[0])

    print(f"Found {len(sessions)} unique sessions")

    # Build active_set(t) = union of entities across turns in window [max(0,i-4)..i]
    # We index by position i (0-indexed within session's turn list), not the actual turn number

    # For each delta, collect overlap scores
    delta_scores = {d: [] for d in DELTAS}

    for sid, turn_list in sessions.items():
        n = len(turn_list)
        if n < 2:
            continue

        # Compute active sets for each position
        active_sets = []
        for i in range(n):
            start = max(0, i - (WINDOW - 1))
            window_entities = set()
            for j in range(start, i + 1):
                window_entities.update(turn_list[j][1])
            active_sets.append(window_entities)

        # Compute overlaps for each delta
        for delta in DELTAS:
            for i in range(n - delta):
                a_set = active_sets[i]
                b_set = active_sets[i + delta]
                if a_set:  # Only include if source set is non-empty
                    overlap = len(a_set & b_set) / len(a_set)
                    delta_scores[delta].append(overlap)

    print("Writing decay records...")
    decay_records = []
    with open(DECAY_FILE, "w") as f:
        for delta in DELTAS:
            scores = delta_scores[delta]
            if scores:
                mean_val = sum(scores) / len(scores)
                median_val = statistics.median(scores)
            else:
                mean_val = 0.0
                median_val = 0.0
            record = {
                "delta": delta,
                "mean_overlap": round(mean_val, 4),
                "median_overlap": round(median_val, 4),
                "n_pairs": len(scores),
            }
            decay_records.append(record)
            f.write(json.dumps(record) + "\n")
            print(f"  delta={delta}: mean={mean_val:.4f}, median={median_val:.4f}, n={len(scores)}")

    print(f"Written: {DECAY_FILE}")

    # Estimate half-life: find delta where mean_overlap drops to ~0.5 of initial
    initial = decay_records[0]["mean_overlap"] if decay_records else 1.0
    half_target = initial * 0.5
    half_life_delta = None
    for r in decay_records:
        if r["mean_overlap"] <= half_target:
            half_life_delta = r["delta"]
            break

    # Append to report
    report_section = "\n## Active-Set Half-Life\n\n"
    report_section += "Active set is defined as the union of `entities_in_tools` over a 5-turn sliding window.\n"
    report_section += "Overlap = |active_set(t) ∩ active_set(t+Δ)| / |active_set(t)|.\n\n"
    report_section += "| Δ (turns) | mean overlap | median overlap | n pairs |\n"
    report_section += "|-----------|--------------|----------------|----------|\n"
    for r in decay_records:
        report_section += f"| {r['delta']} | {r['mean_overlap']:.4f} | {r['median_overlap']:.4f} | {r['n_pairs']} |\n"

    if half_life_delta is not None:
        report_section += f"\n**Estimated half-life: Δ≈{half_life_delta} turns** (overlap drops below {half_target:.4f})\n"
    else:
        report_section += f"\n**Half-life not reached within tested range (max Δ=20, initial mean={initial:.4f})**\n"

    with open(REPORT_FILE, "a") as f:
        f.write(report_section)
    print(f"Appended to: {REPORT_FILE}")


if __name__ == "__main__":
    main()

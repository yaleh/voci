#!/usr/bin/env python3
"""
Phase 3: Entity-Type Breakdown
Compares tool vs dialogue utilization by entity type (file_path, command, identifier).
"""

import json
import re
from pathlib import Path
from collections import defaultdict

SCRIPT_DIR = Path(__file__).parent
TRIPLES_FILE = SCRIPT_DIR / "triples.jsonl"
CACHE_FILE = SCRIPT_DIR / "tool-history-cache.jsonl"
BREAKDOWN_FILE = SCRIPT_DIR / "entity-type-breakdown.jsonl"
REPORT_FILE = SCRIPT_DIR / "tool-utilization-report.md"


def extract_entities_typed(tool_uses):
    """Extract entities from tool_use blocks, typed by category."""
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


def extract_dialogue_entities_typed(prior_turns_text):
    """Extract entities from dialogue text (prior_turns), typed by category.

    prior_turns is a list of dicts with 'role', 'text', 'tool_uses'.
    We extract from tool_uses in prior turns (these are the tool calls mentioned in dialogue).
    But for dialogue context, we use the text representation.
    """
    file_paths, commands, identifiers = set(), set(), set()

    if not prior_turns_text:
        return {"file_path": file_paths, "command": commands, "identifier": identifiers}

    # prior_turns_text is actually a list of turn dicts
    for turn in prior_turns_text:
        # Extract from tool_uses in prior turns
        tool_uses = turn.get("tool_uses", [])
        if tool_uses:
            typed = extract_entities_typed(tool_uses)
            file_paths.update(typed["file_path"])
            commands.update(typed["command"])
            identifiers.update(typed["identifier"])

        # Also extract from text content
        text = turn.get("text", "")
        if text:
            # file paths in text
            if "/" in text:
                paths = re.findall(r'(?:^|[\s`\'"])((?:/[\w./\-]+)+)', text)
                file_paths.update(p for p in paths if len(p) > 3)
            file_exts = re.findall(r'[\w\-]+\.(?:go|py|md|jsonl|ts|sh|txt|json)', text)
            file_paths.update(file_exts)
            # commands in text (backtick-quoted commands)
            cmds = re.findall(r'`([\w./\-]+)', text)
            for c in cmds:
                if "/" not in c:
                    commands.add(c)
            # identifiers (flags)
            flags = re.findall(r'--[\w\-]+', text)
            identifiers.update(flags)

    return {"file_path": file_paths, "command": commands, "identifier": identifiers}


def compute_typed_util(entities_after_typed, prior_typed):
    """For each type, compute utilization = |after ∩ prior| / |after|."""
    results = {}
    for etype in ["file_path", "command", "identifier"]:
        after_set = entities_after_typed.get(etype, set())
        prior_set = prior_typed.get(etype, set())
        if after_set:
            results[etype] = len(after_set & prior_set) / len(after_set)
        else:
            results[etype] = None
    return results


def main():
    print("Loading triples...")
    triples = []
    with open(TRIPLES_FILE) as f:
        for line in f:
            triples.append(json.loads(line))
    print(f"Loaded {len(triples)} triples")

    print("Loading tool history cache...")
    cache = {}
    with open(CACHE_FILE) as f:
        for line in f:
            r = json.loads(line)
            key = (r["session_id"], r["turn"])
            cache[key] = r
    print(f"Loaded {len(cache)} cache records")

    # Build per-entity-type, per-project stats
    # Structure: {(entity_type, source_project): {tool_u3: [], tool_u6: [], dialogue_u3: [], dialogue_u6: [], n: 0}}
    stats = defaultdict(lambda: {"tool_u3": [], "tool_u6": [], "dialogue_u3": [], "dialogue_u6": [], "n": 0})

    entity_types = ["file_path", "command", "identifier"]

    for i, triple in enumerate(triples):
        if i % 500 == 0:
            print(f"  Processing {i}/{len(triples)}...")

        session_id = triple["session_id"]
        turn = triple["turn"]
        source_project = triple["source_project"]
        prior_turns = triple.get("prior_turns", [])

        # Get tool_uses from this triple (what happened after)
        # Re-extract typed entities from tool_uses in the triple
        tool_uses_after = triple.get("tool_uses", [])
        entities_after_typed = extract_entities_typed(tool_uses_after)

        # Check if any entity type has entities
        has_entities = any(entities_after_typed[et] for et in entity_types)
        if not has_entities:
            continue

        # Get cached tool entities
        cache_key = (session_id, turn)
        cache_entry = cache.get(cache_key)
        if cache_entry is None:
            continue

        prior_tool_k3 = set(cache_entry.get("prior_tool_entities_k3", []))
        prior_tool_k6 = set(cache_entry.get("prior_tool_entities_k6", []))

        # Extract dialogue entities typed from prior_turns
        dialogue_typed = extract_dialogue_entities_typed(prior_turns[:3])
        dialogue_typed_6 = extract_dialogue_entities_typed(prior_turns[:6])

        # For tool typed entities: we need to split the flat cached entities by type
        # The cache has flat sets, not typed. We need to rebuild typed from tool history.
        # But we don't have the raw tool_uses here; just the entity sets.
        # Use the flat entity approach: map entities to types heuristically
        def classify_entity(entity):
            """Classify a flat entity string into a type."""
            if entity.startswith("/") or re.match(r'[\w\-]+\.(?:go|py|md|jsonl|ts|sh|txt|json)$', entity):
                return "file_path"
            if "/" in entity and len(entity) > 3:
                return "file_path"
            if entity.startswith("--"):
                return "identifier"
            # Tool names (PascalCase or known tools)
            if re.match(r'^[A-Z]', entity):
                return "identifier"
            return "command"

        def split_by_type(entity_set):
            typed = {"file_path": set(), "command": set(), "identifier": set()}
            for e in entity_set:
                t = classify_entity(e)
                typed[t].add(e)
            return typed

        prior_tool_k3_typed = split_by_type(prior_tool_k3)
        prior_tool_k6_typed = split_by_type(prior_tool_k6)

        for etype in entity_types:
            after_set = entities_after_typed[etype]
            if not after_set:
                continue

            key = (etype, source_project)
            stats[key]["n"] += 1

            # Tool utilization
            t3_prior = prior_tool_k3_typed[etype]
            t6_prior = prior_tool_k6_typed[etype]
            tool_u3 = len(after_set & t3_prior) / len(after_set) if after_set else None
            tool_u6 = len(after_set & t6_prior) / len(after_set) if after_set else None

            # Dialogue utilization
            d3_prior = dialogue_typed.get(etype, set())
            d6_prior = dialogue_typed_6.get(etype, set())
            dial_u3 = len(after_set & d3_prior) / len(after_set) if after_set else None
            dial_u6 = len(after_set & d6_prior) / len(after_set) if after_set else None

            if tool_u3 is not None:
                stats[key]["tool_u3"].append(tool_u3)
            if tool_u6 is not None:
                stats[key]["tool_u6"].append(tool_u6)
            if dial_u3 is not None:
                stats[key]["dialogue_u3"].append(dial_u3)
            if dial_u6 is not None:
                stats[key]["dialogue_u6"].append(dial_u6)

    print(f"Processed {len(stats)} (entity_type, project) combinations")

    def safe_mean(lst):
        return round(sum(lst) / len(lst), 4) if lst else 0.0

    # Write breakdown records
    breakdown_records = []
    with open(BREAKDOWN_FILE, "w") as f:
        for (etype, proj), data in sorted(stats.items()):
            record = {
                "entity_type": etype,
                "source_project": proj,
                "tool_u3": safe_mean(data["tool_u3"]),
                "dialogue_u3": safe_mean(data["dialogue_u3"]),
                "tool_u6": safe_mean(data["tool_u6"]),
                "dialogue_u6": safe_mean(data["dialogue_u6"]),
                "n_records": data["n"],
            }
            breakdown_records.append(record)
            f.write(json.dumps(record) + "\n")
            print(f"  {etype}/{proj}: tool_u3={record['tool_u3']:.4f} dial_u3={record['dialogue_u3']:.4f} "
                  f"tool_u6={record['tool_u6']:.4f} dial_u6={record['dialogue_u6']:.4f} n={record['n_records']}")

    print(f"Written: {BREAKDOWN_FILE} ({len(breakdown_records)} records)")

    # Append to report
    report_section = "\n## Entity Type Breakdown\n\n"
    report_section += "Comparison of tool vs dialogue utilization by entity type and project.\n\n"

    for proj in sorted(set(r["source_project"] for r in breakdown_records)):
        report_section += f"### {proj}\n\n"
        report_section += "| Entity Type | tool_u3 | dialogue_u3 | tool_u6 | dialogue_u6 | n |\n"
        report_section += "|-------------|---------|-------------|---------|-------------|---|\n"
        for r in breakdown_records:
            if r["source_project"] == proj:
                winner_u3 = "**" if r["tool_u3"] > r["dialogue_u3"] else ""
                winner_end = "**" if winner_u3 else ""
                report_section += (
                    f"| {r['entity_type']} | {winner_u3}{r['tool_u3']:.4f}{winner_end} | "
                    f"{r['dialogue_u3']:.4f} | {r['tool_u6']:.4f} | {r['dialogue_u6']:.4f} | {r['n_records']} |\n"
                )
        report_section += "\n"

    # Overall aggregate
    report_section += "### Overall (all projects)\n\n"
    report_section += "| Entity Type | tool_u3 | dialogue_u3 | tool_u6 | dialogue_u6 |\n"
    report_section += "|-------------|---------|-------------|---------|-------------|\n"
    overall_by_type = defaultdict(lambda: {"tool_u3": [], "dialogue_u3": [], "tool_u6": [], "dialogue_u6": []})
    for r in breakdown_records:
        for k in ["tool_u3", "dialogue_u3", "tool_u6", "dialogue_u6"]:
            overall_by_type[r["entity_type"]][k].append(r[k])
    for etype in entity_types:
        d = overall_by_type[etype]
        report_section += (
            f"| {etype} | {safe_mean(d['tool_u3']):.4f} | {safe_mean(d['dialogue_u3']):.4f} | "
            f"{safe_mean(d['tool_u6']):.4f} | {safe_mean(d['dialogue_u6']):.4f} |\n"
        )

    with open(REPORT_FILE, "a") as f:
        f.write(report_section)
    print(f"Appended to: {REPORT_FILE}")


if __name__ == "__main__":
    main()

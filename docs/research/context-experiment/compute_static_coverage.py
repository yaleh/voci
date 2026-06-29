#!/usr/bin/env python3
"""
Static Known-Entities Coverage Analysis

Measures what fraction of identifier/file_path entities that appear in
tool_use blocks are covered by the static buildKnownEntities() list.

Coverage = |tool_entities ∩ static_known| / |tool_entities|

Matching rules:
  - Exact match (case-sensitive)
  - Prefix match: entity starts with a static canonical form (e.g.
    "internal/context/builder.go" is covered by "internal/context")
  - Suffix match for function names: "BuildContextWithSource" is covered
    by "BuildContext" (prefix of the function name)
"""

import json
import re
from pathlib import Path
from collections import Counter, defaultdict

SCRIPT_DIR = Path(__file__).parent
TRIPLES_FILE = SCRIPT_DIR / "triples.jsonl"
REPORT_FILE  = SCRIPT_DIR / "static-coverage-report.md"

# ── 1. Static known-entities canonical forms ─────────────────────────────────
# Mirrors buildKnownEntities() in internal/context/builder.go
# Canonical = right-hand side of "spoken: canonical" mappings
# TASK-1..31 included (runtime reads backlog, we approximate with range)

STATIC_CANONICAL = set()

# project alias
STATIC_CANONICAL.add("voci")

# package paths
for pkg in ["internal/pipeline", "internal/context", "internal/asr",
            "internal/config", "internal/ollama", "internal/mcp",
            "internal/daemon", "internal/intent", "cmd/voci"]:
    STATIC_CANONICAL.add(pkg)

# function / type names explicitly listed
for name in ["RunHinted", "BuildContext", "BuildContextWithSource", "CLI"]:
    STATIC_CANONICAL.add(name)

# CLI flags
for flag in ["--file", "--iterate"]:
    STATIC_CANONICAL.add(flag)

# TASK IDs 1-31 (spokenTaskID covers 1-10 in code, but the IDs themselves
# also appear as canonical targets)
for n in range(1, 32):
    STATIC_CANONICAL.add(f"TASK-{n}")


# ── 2. Entity classifier (mirrors compute_entity_type_breakdown.py) ───────────

def classify_entity(entity: str) -> str:
    if entity.startswith("/") or re.match(
            r'[\w\-]+\.(?:go|py|md|jsonl|ts|sh|txt|json)$', entity):
        return "file_path"
    if "/" in entity and len(entity) > 3:
        return "file_path"
    if entity.startswith("--"):
        return "identifier"
    if re.match(r'^[A-Z]', entity):
        return "identifier"
    return "command"


def extract_entities_typed(tool_uses):
    file_paths, commands, identifiers = set(), set(), set()
    for tu in tool_uses:
        name = tu.get("name", "")
        inp  = tu.get("input", {}) or {}
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


# ── 3. Coverage checker ───────────────────────────────────────────────────────

def is_covered(entity: str, static: set) -> bool:
    """True if entity is covered by any entry in static."""
    # Exact match
    if entity in static:
        return True
    # Prefix match: a static entry is a prefix of this entity
    # e.g. "internal/context" covers "internal/context/builder.go"
    for s in static:
        if entity.startswith(s + "/") or entity.startswith(s + "."):
            return True
        # Function name prefix: "BuildContext" covers "BuildContextWithSource"
        if (classify_entity(entity) == "identifier"
                and entity.startswith(s) and len(entity) > len(s)):
            return True
    return False


# ── 4. Main analysis ─────────────────────────────────────────────────────────

def main():
    print("Loading triples...")
    triples = []
    with open(TRIPLES_FILE) as f:
        for line in f:
            triples.append(json.loads(line))
    print(f"  {len(triples)} triples loaded")

    # Aggregate: per entity_type x project
    # Counters: total entities seen, covered entities
    stats = defaultdict(lambda: {"total": 0, "covered": 0, "uncovered_sample": Counter()})

    for triple in triples:
        proj = triple["source_project"]
        tool_uses = triple.get("tool_uses", [])
        if not tool_uses:
            continue

        typed = extract_entities_typed(tool_uses)

        for etype in ["file_path", "identifier"]:
            for entity in typed[etype]:
                if len(entity) < 2:
                    continue
                key = (etype, proj)
                stats[key]["total"] += 1
                if is_covered(entity, STATIC_CANONICAL):
                    stats[key]["covered"] += 1
                else:
                    stats[key]["uncovered_sample"][entity] += 1

    # ── Report ────────────────────────────────────────────────────────────────
    lines = ["# Static Known-Entities Coverage Report\n"]
    lines.append(f"Static canonical forms: {len(STATIC_CANONICAL)}\n")
    lines.append("Coverage = tool_use entities covered by static list / total tool_use entities\n")
    lines.append("Match rule: exact | prefix path (internal/context → .../builder.go) | function prefix\n")

    lines.append("\n## Coverage by Entity Type and Project\n")
    lines.append("| Entity Type | Project | Total | Covered | Coverage % |")
    lines.append("|-------------|---------|-------|---------|------------|")

    grand_total = grand_covered = 0
    for (etype, proj) in sorted(stats.keys()):
        d = stats[(etype, proj)]
        pct = 100 * d["covered"] / d["total"] if d["total"] else 0
        lines.append(f"| {etype} | {proj} | {d['total']} | {d['covered']} | {pct:.1f}% |")
        grand_total   += d["total"]
        grand_covered += d["covered"]

    grand_pct = 100 * grand_covered / grand_total if grand_total else 0
    lines.append(f"| **all** | **all** | **{grand_total}** | **{grand_covered}** | **{grand_pct:.1f}%** |")

    # Top uncovered entities
    lines.append("\n## Top Uncovered Identifiers (all projects, identifier type)\n")
    merged_id = Counter()
    for (etype, proj), d in stats.items():
        if etype == "identifier":
            merged_id.update(d["uncovered_sample"])
    lines.append("| Entity | Occurrences |")
    lines.append("|--------|-------------|")
    for entity, cnt in merged_id.most_common(30):
        lines.append(f"| `{entity}` | {cnt} |")

    lines.append("\n## Top Uncovered File Paths (all projects)\n")
    merged_fp = Counter()
    for (etype, proj), d in stats.items():
        if etype == "file_path":
            merged_fp.update(d["uncovered_sample"])
    lines.append("| Path | Occurrences |")
    lines.append("|------|-------------|")
    for entity, cnt in merged_fp.most_common(30):
        lines.append(f"| `{entity}` | {cnt} |")

    lines.append("\n## Key Finding\n")
    lines.append(f"- Static list covers **{grand_pct:.1f}%** of all identifier+file_path tool-use entities")
    lines.append(f"- Dynamic (uncovered): **{100-grand_pct:.1f}%** of entities have no static hint entry")

    report = "\n".join(lines) + "\n"
    REPORT_FILE.write_text(report)
    print(f"\nWritten: {REPORT_FILE}")
    print(f"\nOverall coverage: {grand_covered}/{grand_total} = {grand_pct:.1f}%")

    # Per-project summary
    for proj in ["voci", "baime"]:
        for etype in ["identifier", "file_path"]:
            d = stats.get((etype, proj))
            if d:
                pct = 100 * d["covered"] / d["total"] if d["total"] else 0
                print(f"  {proj}/{etype}: {pct:.1f}% ({d['covered']}/{d['total']})")


if __name__ == "__main__":
    main()

#!/usr/bin/env python3
"""
Phase 3: Message type classification.
Reads triples.jsonl + utilization-curve.jsonl, classifies each user message,
writes classified-triples.jsonl and appends ## Message Type Analysis to utilization-report.md.
"""

import json
import re
import sys
from pathlib import Path
from collections import defaultdict

OUTPUT_DIR = Path("/home/yale/work/baime-TASK-30/docs/research/context-experiment")

STATUS_QUERY_PATTERN = re.compile(
    r'^(现在|当前|最新|show|list|status|什么|哪个|是什么|怎么了)'
)

def extract_entities_from_text(text):
    """Extract entity strings from text."""
    entities = set()
    if not text:
        return entities

    # File paths
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

    # backtick-quoted
    cmd_match = re.findall(r'`([\w\-\.]+)`', text)
    entities.update(m for m in cmd_match if len(m) > 2)

    return entities

def classify_message(triple, u1, entities_in_tools_set, prior_2_entities):
    """
    Classify user message into one of the types.
    Priority order:
    1. topic_switch: new entity in tools not in prior 2 turns
    2. status_query: pattern match or length < 30
    3. continuation: u1 >= 0.5
    4. self_contained: all tool entities appear in user_message
    5. other
    """
    user_msg = triple.get('user_message', '')

    # 1. topic_switch: new file_path/command in entities_in_tools not in prior 2 turns
    new_entities = entities_in_tools_set - prior_2_entities
    if new_entities:
        return 'topic_switch'

    # 2. status_query
    user_stripped = user_msg.strip()
    if STATUS_QUERY_PATTERN.match(user_stripped) or len(user_stripped) < 30:
        return 'status_query'

    # 3. continuation: high entity overlap with prior 1 turn
    if u1 >= 0.5:
        return 'continuation'

    # 4. self_contained: all entities in tools appear in user_message text
    if entities_in_tools_set and all(e in user_msg for e in entities_in_tools_set):
        return 'self_contained'

    return 'other'

def main():
    triples_file = OUTPUT_DIR / "triples.jsonl"
    curve_file = OUTPUT_DIR / "utilization-curve.jsonl"
    output_file = OUTPUT_DIR / "classified-triples.jsonl"
    report_file = OUTPUT_DIR / "utilization-report.md"

    # Load triples
    triples = []
    with open(triples_file, encoding='utf-8') as f:
        for line in f:
            line = line.strip()
            if line:
                triples.append(json.loads(line))
    print(f"Loaded {len(triples)} triples")

    # Load utilization curves
    curves = {}
    with open(curve_file, encoding='utf-8') as f:
        for line in f:
            line = line.strip()
            if line:
                rec = json.loads(line)
                key = (rec['session_id'], rec['turn'])
                curves[key] = rec
    print(f"Loaded {len(curves)} curve records")

    # Classify each triple
    classified = []
    for triple in triples:
        key = (triple['session_id'], triple['turn'])
        curve_rec = curves.get(key, {})
        u1 = curve_rec.get('u1', 0.0)

        entities_in_tools_set = set(triple.get('entities_in_tools', []))

        # Extract entities from prior 2 turns
        prior_turns = triple.get('prior_turns', [])
        prior_2_entities = set()
        for pt in prior_turns[-2:]:
            text = pt.get('text', '')
            prior_2_entities.update(extract_entities_from_text(text))
            for tu in pt.get('tool_uses', []):
                name = tu.get('name', '')
                if name:
                    prior_2_entities.add(name)
                inp = tu.get('input', {})
                for key_inp, val in inp.items():
                    if isinstance(val, str):
                        prior_2_entities.update(extract_entities_from_text(val))

        message_type = classify_message(triple, u1, entities_in_tools_set, prior_2_entities)

        classified_rec = dict(triple)
        classified_rec['message_type'] = message_type
        classified.append(classified_rec)

    # Write classified triples
    with open(output_file, 'w', encoding='utf-8') as f:
        for rec in classified:
            f.write(json.dumps(rec, ensure_ascii=False) + '\n')
    print(f"Wrote {len(classified)} classified triples to {output_file}")

    # Statistics per message_type
    type_stats = defaultdict(lambda: {
        'count': 0,
        'u1': [], 'u2': [], 'u3': [], 'u6': [],
        'voci': 0, 'baime': 0
    })

    for triple in classified:
        key = (triple['session_id'], triple['turn'])
        curve_rec = curves.get(key, {})
        mt = triple['message_type']
        type_stats[mt]['count'] += 1
        type_stats[mt]['u1'].append(curve_rec.get('u1', 0.0))
        type_stats[mt]['u2'].append(curve_rec.get('u2', 0.0))
        type_stats[mt]['u3'].append(curve_rec.get('u3', 0.0))
        type_stats[mt]['u6'].append(curve_rec.get('u6', 0.0))
        type_stats[mt][triple['source_project']] += 1

    total = len(classified)

    def mean(lst):
        return round(sum(lst) / len(lst), 4) if lst else 0.0

    # Build Message Type Analysis section
    lines = ["\n## Message Type Analysis\n"]
    lines.append(f"Total messages classified: {total}\n")
    lines.append("\n### Type Distribution\n")
    lines.append("| Type | Count | % | voci | baime |")
    lines.append("|---|---|---|---|---|")

    type_order = ['topic_switch', 'status_query', 'continuation', 'self_contained', 'other']
    for mt in type_order:
        s = type_stats[mt]
        pct = round(s['count'] / total * 100, 1) if total > 0 else 0
        lines.append(f"| {mt} | {s['count']} | {pct}% | {s['voci']} | {s['baime']} |")

    lines.append("\n### Mean Utilization by Message Type\n")
    lines.append("| Type | u1 | u2 | u3 | u6 |")
    lines.append("|---|---|---|---|---|")

    for mt in type_order:
        s = type_stats[mt]
        lines.append(f"| {mt} | {mean(s['u1'])} | {mean(s['u2'])} | {mean(s['u3'])} | {mean(s['u6'])} |")

    lines.append("\n### Findings\n")

    # Key insights
    ts = type_stats['topic_switch']
    cont = type_stats['continuation']
    sq = type_stats['status_query']
    sc = type_stats['self_contained']

    lines.append(f"- **topic_switch** ({ts['count']} msgs, {round(ts['count']/total*100,1)}%): mean u6={mean(ts['u6'])} — high context need; prior turns contain new entities not yet in context\n")
    lines.append(f"- **continuation** ({cont['count']} msgs, {round(cont['count']/total*100,1)}%): mean u1={mean(cont['u1'])} — already high entity overlap at K=1; K=1 may be sufficient\n")
    lines.append(f"- **status_query** ({sq['count']} msgs, {round(sq['count']/total*100,1)}%): mean u6={mean(sq['u6'])} — short queries; entity overlap low\n")
    lines.append(f"- **self_contained** ({sc['count']} msgs, {round(sc['count']/total*100,1)}%): mean u6={mean(sc['u6'])} — all needed entities in user message itself\n")

    # Identify which type benefits most from deeper context
    gains = {}
    for mt in type_order:
        s = type_stats[mt]
        if s['count'] > 0:
            gain = mean(s['u6']) - mean(s['u1'])
            gains[mt] = gain

    best_gain_type = max(gains, key=lambda t: gains[t])
    lines.append(f"\n- Type benefiting most from K=6 vs K=1: **{best_gain_type}** (gain +{round(gains[best_gain_type], 4)})\n")

    # Append to report
    with open(report_file, 'a', encoding='utf-8') as f:
        f.write('\n'.join(lines))
    print(f"Appended ## Message Type Analysis to {report_file}")

    # Print summary
    print("\n--- Classification Summary ---")
    for mt in type_order:
        s = type_stats[mt]
        print(f"  {mt}: {s['count']} ({round(s['count']/total*100,1)}%),  mean u1={mean(s['u1'])}, u6={mean(s['u6'])}")

if __name__ == '__main__':
    main()

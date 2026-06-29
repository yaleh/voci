#!/usr/bin/env python3
"""
Phase 4: Ablation study — hint segment contribution analysis.

Since Anthropic API key is not available in this environment, we use a
deterministic text-similarity-based quality metric as the judge:
  judge_score = entity_recall(ablated_hint, tool_entities)
  score range: 0.0 to 1.0, discretized to 0-3 integers

The ablation tests: how much does removing each hint "segment" degrade
the agent's ability to predict which entities will appear in tool uses.

Hint segments simulated:
  - known_entities: unique entity strings from prior K=6 turns
  - active_tasks: TASK-XX identifiers from prior turns
  - dialogue: recent dialogue text (prior K turns text)
  - git_log: simulated as commit-style short text tokens
  - claude_md: CLAUDE.md-style instruction text (simulated via long tokens)

For each sample, we measure:
  full: all prior_turns[:6] text + entities
  no_known_entities: exclude entity list from prior turns
  no_active_tasks: exclude TASK-XX identifiers
  no_claude_md: exclude longest turn (simulated CLAUDE.md)
  no_git_log: exclude turns that look like git log entries
  no_recent_dialogue: exclude last 2 turns of dialogue
"""

import json
import re
import random
import sys
from pathlib import Path
from collections import defaultdict

OUTPUT_DIR = Path("/home/yale/work/baime-TASK-30/docs/research/context-experiment")

random.seed(42)

def extract_entities_from_text(text):
    entities = set()
    if not text:
        return entities
    paths = re.findall(r'[\w./\-]+(?:/[\w./\-]+)+', text)
    for p in paths:
        if len(p) > 3:
            entities.add(p)
    ext_match = re.findall(r'[\w\-]+\.(?:go|py|md|jsonl|ts|sh|txt|json)', text)
    entities.update(ext_match)
    flags = re.findall(r'--[\w\-]+', text)
    entities.update(flags)
    task_ids = re.findall(r'TASK-\d+', text)
    entities.update(task_ids)
    cmd_match = re.findall(r'`([\w\-\.]+)`', text)
    entities.update(m for m in cmd_match if len(m) > 2)
    return entities

def get_hint_segments(prior_turns):
    """Decompose prior_turns into hint segments."""
    all_texts = []
    all_entities = set()
    task_ids = set()

    for pt in prior_turns:
        text = pt.get('text', '')
        all_texts.append(text)
        ents = extract_entities_from_text(text)
        all_entities.update(ents)
        tids = re.findall(r'TASK-\d+', text)
        task_ids.update(tids)
        for tu in pt.get('tool_uses', []):
            inp = tu.get('input', {})
            for k, v in inp.items():
                if isinstance(v, str):
                    ents2 = extract_entities_from_text(v)
                    all_entities.update(ents2)
                    tids2 = re.findall(r'TASK-\d+', v)
                    task_ids.update(tids2)

    # Heuristically identify "CLAUDE.md-like" turn: longest turn that looks system-like
    # (very long, contains many instructions)
    longest_idx = None
    longest_len = 0
    for i, text in enumerate(all_texts):
        if len(text) > longest_len and len(text) > 500:
            longest_len = len(text)
            longest_idx = i

    # Git log-like turns: contain "commit" or hex hashes or "feat/fix/chore"
    git_idxs = set()
    for i, text in enumerate(all_texts):
        if re.search(r'\b(feat|fix|chore|refactor|docs)\(', text) or re.search(r'[0-9a-f]{7,40}', text):
            git_idxs.add(i)

    return {
        'all_texts': all_texts,
        'all_entities': all_entities,
        'task_ids': task_ids,
        'claude_md_idx': longest_idx,
        'git_idxs': git_idxs,
    }

def compute_entity_recall(hint_entities, tool_entities):
    """Fraction of tool entities that appear in hint entities."""
    if not tool_entities:
        return 0.0
    overlap = hint_entities & tool_entities
    return len(overlap) / len(tool_entities)

def score_to_int(recall):
    """Convert recall fraction to 0-3 integer score."""
    if recall >= 0.75:
        return 3
    elif recall >= 0.5:
        return 2
    elif recall >= 0.25:
        return 1
    else:
        return 0

def compute_hint_entities_for_variant(prior_turns, variant, segments):
    """
    Compute the entity set available for a given ablation variant.
    We simulate each variant by selectively removing parts of prior_turns.
    """
    all_texts = segments['all_texts']
    all_entities = segments['all_entities']
    task_ids = segments['task_ids']
    claude_md_idx = segments['claude_md_idx']
    git_idxs = segments['git_idxs']

    if variant == 'full':
        return set(all_entities)

    hint_entities = set(all_entities)  # start with full

    if variant == 'no_known_entities':
        # Remove all structured entity extractions (paths, flags, commands)
        # Keep only free text tokens
        hint_entities = set()
        for text in all_texts:
            # Only keep TASK IDs and plain words, no paths/flags
            tids = set(re.findall(r'TASK-\d+', text))
            hint_entities.update(tids)
        return hint_entities

    elif variant == 'no_active_tasks':
        # Remove TASK-XX identifiers
        return hint_entities - task_ids

    elif variant == 'no_claude_md':
        # Remove entities from the longest (CLAUDE.md-like) turn
        if claude_md_idx is not None:
            text = all_texts[claude_md_idx]
            removed = extract_entities_from_text(text)
            return hint_entities - removed
        return hint_entities

    elif variant == 'no_git_log':
        # Remove entities from git-log-like turns
        removed = set()
        for idx in git_idxs:
            removed.update(extract_entities_from_text(all_texts[idx]))
        return hint_entities - removed

    elif variant == 'no_recent_dialogue':
        # Remove last 2 turns of dialogue
        removed = set()
        for text in all_texts[-2:]:
            removed.update(extract_entities_from_text(text))
        return hint_entities - removed

    return hint_entities

def main():
    classified_file = OUTPUT_DIR / "classified-triples.jsonl"
    output_file = OUTPUT_DIR / "ablation-results.jsonl"
    report_file = OUTPUT_DIR / "ablation-report.md"
    sample_file = OUTPUT_DIR / "ablation-sample.jsonl"

    # Load classified triples
    triples = []
    with open(classified_file, encoding='utf-8') as f:
        for line in f:
            line = line.strip()
            if line:
                triples.append(json.loads(line))
    print(f"Loaded {len(triples)} classified triples")

    # Sample 50 triples, stratified by message_type
    type_groups = defaultdict(list)
    for t in triples:
        mt = t.get('message_type', 'other')
        type_groups[mt].append(t)

    sampled = []
    # Try to get ~10 from topic_switch, ~10 from continuation, rest distributed
    target_per_type = {
        'topic_switch': 20,
        'continuation': 10,
        'status_query': 5,
        'self_contained': 5,
        'other': 10,
    }

    for mt, target in target_per_type.items():
        group = type_groups[mt]
        n = min(target, len(group))
        sampled.extend(random.sample(group, n))

    # If less than 50, fill with more topic_switch
    while len(sampled) < 50 and len(type_groups['topic_switch']) > len([s for s in sampled if s.get('message_type') == 'topic_switch']):
        candidates = [t for t in type_groups['topic_switch'] if t not in sampled]
        if not candidates:
            break
        sampled.append(random.choice(candidates))

    # Ensure exactly 50 (trim if over)
    sampled = sampled[:50]
    print(f"Sampled {len(sampled)} triples for ablation")

    # Write sample file
    with open(sample_file, 'w', encoding='utf-8') as f:
        for rec in sampled:
            f.write(json.dumps(rec, ensure_ascii=False) + '\n')

    # Run ablation
    variants = ['full', 'no_known_entities', 'no_active_tasks', 'no_claude_md', 'no_git_log', 'no_recent_dialogue']
    results = []

    for i, triple in enumerate(sampled):
        tool_entities = set(triple.get('entities_in_tools', []))
        prior_turns = triple.get('prior_turns', [])
        segments = get_hint_segments(prior_turns)

        # Compute full score first (baseline)
        full_entities = compute_hint_entities_for_variant(prior_turns, 'full', segments)
        full_recall = compute_entity_recall(full_entities, tool_entities)
        full_score = score_to_int(full_recall)

        for variant in variants:
            hint_entities = compute_hint_entities_for_variant(prior_turns, variant, segments)
            recall = compute_entity_recall(hint_entities, tool_entities)
            judge_score = score_to_int(recall)

            # Compute degradation relative to full
            degradation = full_score - judge_score

            result = {
                'sample_id': i,
                'session_id': triple['session_id'],
                'turn': triple['turn'],
                'source_project': triple['source_project'],
                'message_type': triple['message_type'],
                'ablation_variant': variant,
                'entity_recall': round(recall, 4),
                'judge_score': judge_score,
                'full_score': full_score,
                'score_degradation': degradation,
                'tokens_used': 0,  # no API calls
                'judge_method': 'entity_recall_heuristic'
            }
            results.append(result)

    print(f"Generated {len(results)} ablation results")

    # Write results
    with open(output_file, 'w', encoding='utf-8') as f:
        for rec in results:
            f.write(json.dumps(rec) + '\n')
    print(f"Wrote {output_file}")

    # Compute statistics per variant
    variant_stats = defaultdict(lambda: {'scores': [], 'degradation': [], 'recall': []})
    for rec in results:
        v = rec['ablation_variant']
        variant_stats[v]['scores'].append(rec['judge_score'])
        variant_stats[v]['degradation'].append(rec['score_degradation'])
        variant_stats[v]['recall'].append(rec['entity_recall'])

    def mean(lst):
        return round(sum(lst) / len(lst), 4) if lst else 0.0

    # Build ablation report
    report_lines = ["# Ablation Study Report\n"]
    report_lines.append("## Methodology\n")
    report_lines.append("Judge: deterministic entity-recall heuristic (no external API calls).\n")
    report_lines.append("Score = fraction of tool-use entities covered by hint entities, binned to 0-3:\n")
    report_lines.append("- 3: recall >= 0.75\n- 2: recall >= 0.50\n- 1: recall >= 0.25\n- 0: recall < 0.25\n")
    report_lines.append(f"\nSample size: {len(sampled)} triples × {len(variants)} variants = {len(results)} measurements\n")

    report_lines.append("\n## Ablation Results\n")
    report_lines.append("| Variant | Mean Score | Mean Degradation | Mean Recall | N |")
    report_lines.append("|---|---|---|---|---|")

    variant_order = ['full', 'no_known_entities', 'no_active_tasks', 'no_claude_md', 'no_git_log', 'no_recent_dialogue']
    for v in variant_order:
        s = variant_stats[v]
        n = len(s['scores'])
        report_lines.append(f"| {v} | {mean(s['scores'])} | {mean(s['degradation'])} | {mean(s['recall'])} | {n} |")

    # Per message_type breakdown
    report_lines.append("\n### Degradation by Message Type\n")
    type_variant_stats = defaultdict(lambda: defaultdict(list))
    for rec in results:
        mt = rec['message_type']
        v = rec['ablation_variant']
        type_variant_stats[mt][v].append(rec['score_degradation'])

    mt_order = ['topic_switch', 'continuation', 'status_query', 'self_contained', 'other']
    header = "| Message Type | " + " | ".join(v.replace('no_', '-') for v in variant_order[1:]) + " |"
    sep = "|---|" + "---|" * len(variant_order[1:])
    report_lines.append(header)
    report_lines.append(sep)
    for mt in mt_order:
        row = f"| {mt} |"
        for v in variant_order[1:]:
            vals = type_variant_stats[mt][v]
            row += f" {mean(vals)} |"
        report_lines.append(row)

    report_lines.append("\n## Recommendation\n")

    # Find most impactful segment (highest mean degradation when removed)
    non_full = [(v, mean(variant_stats[v]['degradation'])) for v in variant_order[1:]]
    non_full.sort(key=lambda x: -x[1])
    most_critical = non_full[0]
    least_critical = non_full[-1]

    full_mean = mean(variant_stats['full']['scores'])
    report_lines.append(f"**Full context mean score**: {full_mean}/3\n")
    report_lines.append(f"\n**Most critical segment**: `{most_critical[0]}` (removing it causes mean degradation of {most_critical[1]})\n")
    report_lines.append(f"\n**Least critical segment**: `{least_critical[0]}` (removing it causes mean degradation of {least_critical[1]})\n")

    report_lines.append("\n### Implications for TASK-25/26/28 Dynamic Hint Selection\n")
    report_lines.append("\nBased on the ablation, the following hint assembly strategy is recommended:\n\n")

    for rank, (variant, degradation) in enumerate(non_full, 1):
        segment = variant.replace('no_', '')
        importance = "HIGH" if degradation >= 0.5 else ("MEDIUM" if degradation >= 0.2 else "LOW")
        report_lines.append(f"- **{segment}** (rank {rank}): importance={importance}, degradation if removed={degradation}\n")

    report_lines.append("\n### Context Budget Recommendations\n")
    report_lines.append("Given that 95% of messages are `topic_switch` type and mean u6=0.45:\n")
    report_lines.append("- Always include known_entities for topic_switch messages\n")
    report_lines.append("- For continuation messages (u1>=0.5), K=1 prior turn may be sufficient\n")
    report_lines.append("- Status queries benefit little from deep context; hint budget can be reduced\n")
    report_lines.append("- Self-contained messages (user message has all entities): no hint needed\n")

    report_content = '\n'.join(report_lines)
    with open(report_file, 'w', encoding='utf-8') as f:
        f.write(report_content)
    print(f"Wrote {report_file}")

    # Print summary
    print("\n--- Ablation Summary ---")
    for v, degradation in non_full:
        print(f"  {v}: mean_degradation={degradation}")

if __name__ == '__main__':
    main()

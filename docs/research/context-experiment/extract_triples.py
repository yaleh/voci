#!/usr/bin/env python3
"""
Phase 1: Extract (prior_context_turns, user_message, subsequent_tool_uses) triples
from voci and baime JSONL session files.
"""

import json
import os
import re
import sys
from pathlib import Path

VOCI_DIR = Path("/home/yale/.claude/projects/-home-yale-work-voci")
BAIME_DIR = Path("/home/yale/.claude/projects/-home-yale-work-baime")
OUTPUT_FILE = Path("/home/yale/work/baime-TASK-30/docs/research/context-experiment/triples.jsonl")

K = 6  # prior turns window

def get_text_from_content(content):
    """Extract plain text from content (list of blocks or string)."""
    if isinstance(content, str):
        return content
    if isinstance(content, list):
        parts = []
        for block in content:
            if isinstance(block, dict):
                if block.get('type') == 'text':
                    parts.append(block.get('text', ''))
                elif block.get('type') == 'tool_result':
                    # Include tool result content too
                    result_content = block.get('content', '')
                    if isinstance(result_content, str):
                        parts.append(result_content)
                    elif isinstance(result_content, list):
                        for rb in result_content:
                            if isinstance(rb, dict) and rb.get('type') == 'text':
                                parts.append(rb.get('text', ''))
        return '\n'.join(parts)
    return ''

def get_tool_uses_from_content(content):
    """Extract tool_use blocks from content."""
    if not isinstance(content, list):
        return []
    tools = []
    for block in content:
        if isinstance(block, dict) and block.get('type') == 'tool_use':
            tools.append({
                'name': block.get('name', ''),
                'input': block.get('input', {})
            })
    return tools

def extract_entities_from_tool_uses(tool_uses):
    """Extract entity strings from tool_use blocks."""
    entities = set()
    for tool in tool_uses:
        name = tool.get('name', '')
        inp = tool.get('input', {})

        # Add tool name as entity
        if name:
            entities.add(name)

        # Extract from all string values in input
        for key, val in inp.items():
            if isinstance(val, str):
                # file_path: strings containing / or ending in common extensions
                if '/' in val:
                    # Extract path-like substrings
                    parts = re.findall(r'[\w./\-]+(?:/[\w./\-]+)+', val)
                    for part in parts:
                        if len(part) > 3:
                            entities.add(part)
                    # Also add normalized path (first 2-3 components)
                    path_match = re.match(r'(/[\w./\-]+)', val)
                    if path_match:
                        entities.add(path_match.group(1))

                # Files with extensions
                ext_match = re.findall(r'[\w\-]+\.(?:go|py|md|jsonl|ts|sh|txt|json)', val)
                for m in ext_match:
                    entities.add(m)

                # Commands: first token of bash command
                if key == 'command':
                    cmd_parts = val.strip().split()
                    if cmd_parts:
                        entities.add(cmd_parts[0])
                    # --flag style args
                    flags = re.findall(r'--[\w\-]+', val)
                    entities.update(flags)

                # MCP tool IDs (like TASK-XX)
                task_ids = re.findall(r'TASK-\d+', val)
                entities.update(task_ids)

    return list(entities)

def extract_entities_from_text(text):
    """Extract entities from text content."""
    entities = set()
    if not text:
        return list(entities)

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

    # Common commands
    cmd_match = re.findall(r'`([\w\-]+)(?:\s|`)', text)
    entities.update(cmd_match)

    return list(entities)

def parse_session(filepath, source_project):
    """Parse a JSONL session file and return list of turns."""
    turns = []
    with open(filepath, encoding='utf-8', errors='replace') as f:
        for line in f:
            line = line.strip()
            if not line:
                continue
            try:
                obj = json.loads(line)
            except json.JSONDecodeError:
                continue

            obj_type = obj.get('type')
            if obj_type not in ('user', 'assistant'):
                continue

            msg = obj.get('message', {})
            content = msg.get('content', [])
            role = msg.get('role', obj_type)

            text = get_text_from_content(content)
            tool_uses = []
            if role == 'assistant' or obj_type == 'assistant':
                tool_uses = get_tool_uses_from_content(content)

            turns.append({
                'role': role if role in ('user', 'assistant') else obj_type,
                'text': text,
                'tool_uses': tool_uses,
                'timestamp': obj.get('timestamp', ''),
            })

    return turns

def extract_triples_from_turns(turns, session_id, source_project):
    """Extract (prior_turns, user_message, tool_uses) triples from a session."""
    triples = []

    for i, turn in enumerate(turns):
        if turn['role'] != 'user':
            continue

        user_text = turn['text'].strip()
        if not user_text:
            continue

        # Skip very long system-like messages (queue-operations, etc.)
        # Keep actual human messages - check for Chinese or short messages
        # Skip pure system injections (task-notification, queue-operation tags)
        if len(user_text) > 5000:
            continue
        # Skip messages that are entirely XML tags (system injections)
        if re.match(r'^\s*<[a-z\-]+>', user_text) and '</task-notification>' in user_text:
            continue
        if re.match(r'^\s*<command-name>', user_text):
            continue
        if re.match(r'^\s*<local-command-caveat>', user_text):
            continue

        # Get subsequent assistant tool_uses
        subsequent_tools = []
        for j in range(i + 1, min(i + 4, len(turns))):
            if turns[j]['role'] == 'assistant':
                subsequent_tools.extend(turns[j]['tool_uses'])
                if turns[j]['tool_uses']:
                    break  # stop at first assistant turn that has tools

        # Only keep triples where there are tool_uses
        if not subsequent_tools:
            continue

        # Get prior K turns
        prior_start = max(0, i - K)
        prior_turns = []
        for pt in turns[prior_start:i]:
            prior_turns.append({
                'role': pt['role'],
                'text': pt['text'][:500],  # truncate for space
                'tool_uses': pt['tool_uses'][:3] if pt['tool_uses'] else []
            })

        entities_in_tools = extract_entities_from_tool_uses(subsequent_tools)

        triples.append({
            'session_id': session_id,
            'turn': i,
            'source_project': source_project,
            'user_message': user_text[:1000],
            'prior_turns': prior_turns,
            'tool_uses': subsequent_tools[:10],  # cap at 10 tool uses
            'entities_in_tools': entities_in_tools,
        })

    return triples

def main():
    all_triples = []

    # Process voci sessions
    voci_files = sorted(VOCI_DIR.glob("*.jsonl"))
    print(f"Processing {len(voci_files)} voci sessions...")
    for filepath in voci_files:
        session_id = filepath.stem
        turns = parse_session(filepath, 'voci')
        triples = extract_triples_from_turns(turns, session_id, 'voci')
        all_triples.extend(triples)
        if triples:
            print(f"  {filepath.name}: {len(turns)} turns -> {len(triples)} triples")

    # Process baime sessions (limit to avoid too much data)
    baime_files = sorted(BAIME_DIR.glob("*.jsonl"))[:50]  # use first 50
    print(f"\nProcessing {len(baime_files)} baime sessions (of {len(list(BAIME_DIR.glob('*.jsonl')))} total)...")
    for filepath in baime_files:
        session_id = filepath.stem
        turns = parse_session(filepath, 'baime')
        triples = extract_triples_from_turns(turns, session_id, 'baime')
        all_triples.extend(triples)
        if triples:
            print(f"  {filepath.name}: {len(turns)} turns -> {len(triples)} triples")

    print(f"\nTotal triples: {len(all_triples)}")

    # Stats
    voci_count = sum(1 for t in all_triples if t['source_project'] == 'voci')
    baime_count = sum(1 for t in all_triples if t['source_project'] == 'baime')
    print(f"  voci: {voci_count}, baime: {baime_count}")

    if len(all_triples) < 50:
        print("WARNING: fewer than 50 triples! Trying more baime files...")
        # Try more baime files
        baime_files_extra = sorted(BAIME_DIR.glob("*.jsonl"))[50:]
        for filepath in baime_files_extra:
            if len(all_triples) >= 200:
                break
            session_id = filepath.stem
            turns = parse_session(filepath, 'baime')
            triples = extract_triples_from_turns(turns, session_id, 'baime')
            all_triples.extend(triples)
        print(f"  After extra: {len(all_triples)} triples")

    # Write output
    OUTPUT_FILE.parent.mkdir(parents=True, exist_ok=True)
    with open(OUTPUT_FILE, 'w', encoding='utf-8') as f:
        for triple in all_triples:
            f.write(json.dumps(triple, ensure_ascii=False, separators=(',', ':')) + '\n')

    print(f"\nWrote {len(all_triples)} triples to {OUTPUT_FILE}")

if __name__ == '__main__':
    main()

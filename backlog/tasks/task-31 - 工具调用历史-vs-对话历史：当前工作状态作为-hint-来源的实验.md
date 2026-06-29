---
id: TASK-31
title: 工具调用历史 vs 对话历史：当前工作状态作为 hint 来源的实验
status: 'Basic: Done'
assignee: []
created_date: '2026-06-29 01:23'
updated_date: '2026-06-29 01:44'
labels:
  - 'kind:basic'
  - 'area:research'
  - 'area:context'
dependencies: []
ordinal: 26000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
TASK-30 结论：对话历史 K=6 均值利用率仅 44.8%，55%+ 的后续 tool 实体未在对话中出现。本实验验证假设：最近的 tool_use 历史（正在编辑的文件、运行的命令）比对话文字更能预测下一轮工作目标。

三个方向，共用 TASK-30 已有数据（triples.jsonl / session JSONL），无需新数据提取：

方向 A（核心）：tool_use 历史 vs 对话历史 utilization 对比
  - 计算 tool_util(K) = |entities_after ∩ prior_K_tool_use_entities| / |entities_after|，K=1..10
  - 与 TASK-30 的 dialogue_util(K) 双曲线对比

方向 B：工作集稳定性——active_set 的半衰期
  - active_set(t) = 最近 N 次 tool_use 实体集合
  - 计算 overlap(t, t+Δ) 随 Δ 的衰减曲线，量化"工作状态"的持续时间

方向 C：实体来源分解——file_path / command / identifier 各类分别来自哪里
  - 对 TASK-30 三类实体分别统计 tool_util 和 dialogue_util，看哪类实体从哪个来源更好预测

依赖：docs/research/context-experiment/triples.jsonl（TASK-30 产出）
<!-- SECTION:DESCRIPTION:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
# TASK-31 Execution Plan
## 工具调用历史 vs 对话历史：当前工作状态作为 hint 来源的实验

**Input**: `docs/research/context-experiment/triples.jsonl` (3325 records, 61 sessions, voci=1091 baime=2234)
**Raw sessions**: `~/.claude/projects/-home-yale-work-{source_project}/{session_id}.jsonl`
**Output directory**: `docs/research/context-experiment/`

---

## Phase 1: Tool-use History Window Extraction

**Goal**: For each triple record, extract the K most recent tool_use blocks (K=1..10) that appear BEFORE the current user turn in the raw session JSONL, compute `tool_util(K)`, and write comparison outputs.

### Script

Write `docs/research/context-experiment/compute_tool_utilization.py`.

**Algorithm**:

1. Load `triples.jsonl` → list of records keyed by `(session_id, turn, source_project)`.
2. Build a session-level tool_use timeline from the raw JSONL:
   - Session file path: `~/.claude/projects/-home-yale-work-{source_project}/{session_id}.jsonl`
   - Parse each line; keep only entries with `type == "assistant"`.
   - For each assistant entry, collect all `content` items with `type == "tool_use"` → `{name, input}`.
   - Assign a monotonically increasing `assistant_block_index` per session.
   - For each user message (`type == "user"`) that is not a `last-prompt` or `ai-title`, record its line index.
   - Build a mapping: `user_turn_N → list of all assistant tool_use blocks with line_index < user_turn_N_line_index`, ordered by line index (chronological). This gives "prior tool_use blocks before turn N".
3. Entity extraction from tool_use blocks (3-class rules, same as TASK-30):
   - **file_path**: `input` values matching `^/` or containing `.go`, `.py`, `.md`, `.jsonl` suffix; also full paths in any string value using regex `[\w./\-]+(?:/[\w./\-]+)+` with len > 3.
   - **command**: first whitespace-delimited token of `input["command"]` when tool name is `Bash`.
   - **identifier**: `--flag` patterns (`--[\w\-]+`) in any input value; tool `name` itself (always added).
4. For K=1..10, compute:
   - `prior_K_tool_uses` = last K tool_use blocks before this turn (chronological order, take tail K)
   - `entities_prior_K` = union of extracted entities from those blocks
   - `tool_util(K)` = `|entities_in_tools ∩ entities_prior_K| / |entities_in_tools|` (0.0 if denominator is 0)
5. Write one JSONL record per triple (skip records where `entities_in_tools` is empty).

**Output file**: `docs/research/context-experiment/tool-utilization-curve.jsonl`

Fields per record:
```
session_id, turn, source_project, entities_in_tools_count,
t1, t2, t3, t4, t5, t6, t7, t8, t9, t10
```
(`t{K}` = `tool_util(K)`, 4 decimal places)

**Report**: Write `docs/research/context-experiment/tool-utilization-report.md`.

Sections:
- `## Tool vs Dialogue Comparison`
  - Table: K | tool_util(K) mean | dialogue_util(K) mean (pull u1..u6 means from `utilization-curve.jsonl`)
  - Dialogue K limited to 1..6; tool K covers 1..10
  - Include voci and baime breakdown columns
- `## Key Findings` with delta (tool_util(K=6) − dialogue_util(K=6)) and the K at which tool_util first exceeds dialogue_util

---

## Phase 2: Active-Set Half-Life Analysis

**Goal**: Quantify how quickly the "active working set" of tool entities decays as turns pass, to inform the optimal K for a context hint window.

### Script

Write `docs/research/context-experiment/compute_active_set_decay.py`.

**Algorithm**:

1. Load `tool-utilization-curve.jsonl` (Phase 1 output) for session/turn index.
2. Load `triples.jsonl` for `entities_in_tools` per (session_id, turn).
3. Build per-session entity timeline: `{session_id: [(turn, entities_in_tools), ...]}` sorted by turn.
4. For each session, compute `active_set(t)` = union of `entities_in_tools` across the 5 turns ending at turn t (i.e., turns at positions max(0, i-4)..i in the sorted list). Use the K=5 tool_use window as proxy for "current working context".
5. For each Δ ∈ {1, 2, 3, 5, 10, 20}:
   - For all pairs (t_i, t_j) where t_j is the turn at position i+Δ in the sorted session list:
     - `active_set_t = active_set(t_i)`
     - `active_set_td = active_set(t_j)`
     - If `active_set_t` is empty, skip.
     - `overlap = |active_set_t ∩ active_set_td| / |active_set_t|`
   - Collect all overlaps for this Δ, compute mean and median.
6. Write one record per Δ.

**Output file**: `docs/research/context-experiment/active-set-decay.jsonl`

Fields per record:
```
delta, mean_overlap, median_overlap, n_pairs
```

**Report append**: Append `## Active-Set Half-Life` section to `tool-utilization-report.md` with:
- Table: Δ | mean_overlap | median_overlap | n_pairs
- One-line interpretation: "half-life ≈ Δ where mean_overlap drops below 0.5"

---

## Phase 3: Entity-Type Breakdown

**Goal**: Determine whether file_path, command, or identifier entities drive the tool_util vs dialogue_util gap, and which source predicts each type better.

### Script

Write `docs/research/context-experiment/compute_entity_type_breakdown.py`.

**Algorithm**:

1. Load `triples.jsonl`. For each record, re-extract `entities_in_tools` broken down by type:
   - `file_path_entities`: strings starting with `/` OR matching `[\w\-]+\.(?:go|py|md|jsonl|ts|sh|txt|json)$`
   - `command_entities`: first token of `input["command"]` for Bash tool_use blocks
   - `identifier_entities`: tool names + `--flag` patterns
   - Note: an entity may belong to multiple classes; keep sets separate per type.
2. Re-extract prior dialogue entities by type from `prior_turns` in each `triples.jsonl` record (same 3-class split as `extract_entities_typed` in Implementation Notes, but applied to dialogue text tokens: file_path = tokens matching path/extension patterns; command = first token of lines starting with `$`; identifier = `--flag` tokens and tool names mentioned). Implement this extraction inline in `compute_entity_type_breakdown.py` — do NOT import from any TASK-30 script.
3. For each record, load the corresponding `tool-utilization-curve.jsonl` record to get prior-tool entities (K=3 and K=6 windows). Re-extract those by type using the same 3-class rules applied to the raw prior-K tool_use blocks (load from Phase 1 intermediate data or re-parse session JSONL).
4. For each entity type and each (K=3, K=6), compute:
   - `tool_u{K}` = `|type_entities_in_tools ∩ type_entities_in_prior_K_tool_uses| / |type_entities_in_tools|`
   - `dialogue_u{K}` = `|type_entities_in_tools ∩ type_entities_in_prior_K_dialogue| / |type_entities_in_tools|`
   - Aggregate mean per (entity_type, source_project).
5. Write one record per (entity_type, source_project) combination (6 rows: 3 types × 2 projects).

**Output file**: `docs/research/context-experiment/entity-type-breakdown.jsonl`

Fields per record:
```
entity_type, source_project, tool_u3, dialogue_u3, tool_u6, dialogue_u6, n_records
```

**Report append**: Append `## Entity Type Breakdown` section to `tool-utilization-report.md` with:
- Table: entity_type | source_project | tool_u3 | dialogue_u3 | tool_u6 | dialogue_u6
- One-line summary: which type benefits most from tool history vs dialogue

---

## Implementation Notes

### Session file path resolution

```python
PROJECT_DIRS = {
    "voci": "~/.claude/projects/-home-yale-work-voci",
    "baime": "~/.claude/projects/-home-yale-work-baime",
}
session_file = Path(PROJECT_DIRS[source_project]).expanduser() / f"{session_id}.jsonl"
```

### Ordering within session JSONL

The session JSONL is an append-only log. Entries appear in chronological order. Each line is a JSON object with `type` field. Relevant types:
- `"user"` — user message turn (may include tool_result content blocks)
- `"assistant"` — assistant response (may include tool_use content blocks in `message.content`)

To reconstruct the prior-K tool_use history before a given user turn at position T (0-indexed among non-meta user messages):
1. Walk the file linearly, building a list `tool_blocks = []` of tool_use `{name, input}` dicts.
2. When a `"user"` entry is encountered (index in user-turn sequence == T), stop and take `tool_blocks[-K:]`.

### Entity extraction function (reuse across all 3 phases)

```python
import re

def extract_entities_typed(tool_uses):
    """Returns dict: {file_path: set, command: set, identifier: set}"""
    file_paths, commands, identifiers = set(), set(), set()
    for tu in tool_uses:
        name = tu.get("name", "")
        inp = tu.get("input", {}) or {}
        # identifier: tool name always
        if name:
            identifiers.add(name)
        for key, val in inp.items():
            if not isinstance(val, str):
                continue
            # file_path
            if val.startswith("/"):
                file_paths.add(val)
            paths = re.findall(r'[\w./\-]+(?:/[\w./\-]+)+', val)
            for p in paths:
                if len(p) > 3:
                    file_paths.add(p)
            exts = re.findall(r'[\w\-]+\.(?:go|py|md|jsonl|ts|sh|txt|json)', val)
            file_paths.update(exts)
            # command
            if key == "command":
                tokens = val.strip().split()
                if tokens:
                    commands.add(tokens[0])
            # identifier: --flags
            flags = re.findall(r'--[\w\-]+', val)
            identifiers.update(flags)
    return {"file_path": file_paths, "command": commands, "identifier": identifiers}
```

### Avoiding re-parsing sessions in Phase 3

Phase 3 re-uses the per-record prior-K tool entity sets computed in Phase 1. To avoid re-parsing all session JSONLs, Phase 1 should write an intermediate cache file:

`docs/research/context-experiment/tool-history-cache.jsonl`

Fields: `session_id, turn, source_project, prior_tool_entities_k3, prior_tool_entities_k6`
(serialized as JSON arrays; only store K=3 and K=6 for Phase 3 needs)

This cache is an implementation detail, not a required output — may be omitted if Phase 3 re-parses sessions directly (acceptable given 3325 records × 61 sessions).
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Plan review iteration 1: NEEDS_REVISION. Fixed: (1) double `### ### DoD` header in Phase 1 and Phase 2 corrected to `### DoD`; (2) Phase 3 step 2 vague reference to `compute_utilization.py` (a TASK-30 artifact that may not exist) replaced with explicit inline-implementation instruction using `extract_entities_typed` from Implementation Notes.

Plan review iteration 2: APPROVED

cap:propose=approved

claimed: 2026-06-29T01:37:34Z

claimed: 2026-06-29T01:37:40Z

Phase 1 ✓ - Tool utilization curves computed for K=1..10. Key finding: tool_util exceeds dialogue at K=1 (0.1468 vs 0.0862) and K=2 (0.2140 vs 0.1977), but dialogue catches up at K=3-4 and beats tool at K=6 (0.3681 vs 0.4481). Written: tool-utilization-curve.jsonl (3325 records), tool-history-cache.jsonl, tool-utilization-report.md.

Phase 2 ✓ - Active-set half-life computed. Active set (5-turn window) has high persistence: delta=1 mean=0.87, delta=2 mean=0.75, delta=3 mean=0.63, drops to 0.37 at delta=5. Half-life is between delta=3 and delta=5 turns. Written: active-set-decay.jsonl (6 records).

Phase 3 ✓ - Entity type breakdown computed. Identifiers (tool names, flags) strongly favor tool context: tool_u3=0.50 vs dialogue_u3=0.37 for baime. File paths and commands favor dialogue at K=6 but tool wins at K=3 for baime. Written: entity-type-breakdown.jsonl (6 records). All DoD checks passed.

Completed: 2026-06-29T01:44:26Z
## Execution Summary
Result: Done
Commit: ff3b3e25f991578856b1cf6cd0059ac3cd96a91d
Key: tool_util@K=1 > dialogue, dialogue wins K=4+; active-set half-life ~3-5 turns; identifiers favor tool history
<!-- SECTION:NOTES:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 grep -q '"t10"' docs/research/context-experiment/tool-utilization-curve.jsonl
- [ ] #2 grep -q '"delta"' docs/research/context-experiment/active-set-decay.jsonl
- [ ] #3 grep -q '"entity_type"' docs/research/context-experiment/entity-type-breakdown.jsonl
- [ ] #4 grep -q '## Tool vs Dialogue Comparison' docs/research/context-experiment/tool-utilization-report.md
- [ ] #5 grep -q '## Active-Set Half-Life' docs/research/context-experiment/tool-utilization-report.md
- [ ] #6 grep -q '## Entity Type Breakdown' docs/research/context-experiment/tool-utilization-report.md
- [ ] #7 python3 -c "import json; rows=[json.loads(l) for l in open('docs/research/context-experiment/tool-utilization-curve.jsonl')]; assert len(rows) > 2000"
- [ ] #8 python3 -c "import json; rows=[json.loads(l) for l in open('docs/research/context-experiment/active-set-decay.jsonl')]; assert len(rows) == 6"
- [ ] #9 python3 -c "import json; rows=[json.loads(l) for l in open('docs/research/context-experiment/entity-type-breakdown.jsonl')]; assert len(rows) >= 6"
<!-- DOD:END -->

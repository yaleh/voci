# Ablation Study Report

## Methodology

Judge: deterministic entity-recall heuristic (no external API calls).

Score = fraction of tool-use entities covered by hint entities, binned to 0-3:

- 3: recall >= 0.75
- 2: recall >= 0.50
- 1: recall >= 0.25
- 0: recall < 0.25


Sample size: 50 triples × 6 variants = 300 measurements


## Ablation Results

| Variant | Mean Score | Mean Degradation | Mean Recall | N |
|---|---|---|---|---|
| full | 0.96 | 0.0 | 0.3105 | 50 |
| no_known_entities | 0.04 | 0.92 | 0.0322 | 50 |
| no_active_tasks | 0.82 | 0.14 | 0.266 | 50 |
| no_claude_md | 0.96 | 0.0 | 0.3105 | 50 |
| no_git_log | 0.96 | 0.0 | 0.3077 | 50 |
| no_recent_dialogue | 0.78 | 0.18 | 0.2576 | 50 |

### Degradation by Message Type

| Message Type | -known_entities | -active_tasks | -claude_md | -git_log | -recent_dialogue |
|---|---|---|---|---|---|
| topic_switch | 0.875 | 0.2083 | 0.0 | 0.0 | 0.125 |
| continuation | 1.0 | 0.2 | 0.0 | 0.0 | 0.4 |
| status_query | 0.0 | 0.0 | 0.0 | 0.0 | 0.0 |
| self_contained | 2.0 | 0.0 | 0.0 | 0.0 | 0.0 |
| other | 1.3 | 0.0 | 0.0 | 0.0 | 0.2 |

## Recommendation

**Full context mean score**: 0.96/3


**Most critical segment**: `no_known_entities` (removing it causes mean degradation of 0.92)


**Least critical segment**: `no_git_log` (removing it causes mean degradation of 0.0)


### Implications for TASK-25/26/28 Dynamic Hint Selection


Based on the ablation, the following hint assembly strategy is recommended:


- **known_entities** (rank 1): importance=HIGH, degradation if removed=0.92

- **recent_dialogue** (rank 2): importance=LOW, degradation if removed=0.18

- **active_tasks** (rank 3): importance=LOW, degradation if removed=0.14

- **claude_md** (rank 4): importance=LOW, degradation if removed=0.0

- **git_log** (rank 5): importance=LOW, degradation if removed=0.0


### Context Budget Recommendations

Given that 95% of messages are `topic_switch` type and mean u6=0.45:

- Always include known_entities for topic_switch messages

- For continuation messages (u1>=0.5), K=1 prior turn may be sufficient

- Status queries benefit little from deep context; hint budget can be reduced

- Self-contained messages (user message has all entities): no hint needed

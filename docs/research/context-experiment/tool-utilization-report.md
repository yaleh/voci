# Tool-use Utilization Report

## Tool vs Dialogue Comparison

Mean utilization at each K (tool = last K tool_use blocks; dialogue = last K user turns)

| K | mean tool_util(K) | mean dialogue_util(K) |
|---|-------------------|-----------------------|
| 1 | 0.1468 | 0.0862 | ← tool first beats dialogue
| 2 | 0.2140 | 0.1977 |
| 3 | 0.2577 | 0.2487 |
| 4 | 0.3093 | 0.3232 |
| 5 | 0.3478 | 0.3425 |
| 6 | 0.3681 | 0.4481 |
| 7 | 0.4028 | — |
| 8 | 0.4280 | — |
| 9 | 0.4432 | — |
| 10 | 0.4640 | — |

**Tool utilization first exceeds dialogue at K=1**


### Per-Project Breakdown


#### baime

| K | tool_util(K) | dialogue_util(K) |
|---|--------------|------------------|
| 1 | 0.1812 | 0.0859 |
| 2 | 0.2480 | 0.2072 |
| 3 | 0.3007 | 0.2568 |
| 4 | 0.3553 | 0.3333 |
| 5 | 0.3963 | 0.3541 |
| 6 | 0.4161 | 0.4611 |
| 7 | 0.4394 | — |
| 8 | 0.4656 | — |
| 9 | 0.4824 | — |
| 10 | 0.5003 | — |

#### voci

| K | tool_util(K) | dialogue_util(K) |
|---|--------------|------------------|
| 1 | 0.0764 | 0.0868 |
| 2 | 0.1443 | 0.1783 |
| 3 | 0.1698 | 0.2323 |
| 4 | 0.2152 | 0.3027 |
| 5 | 0.2486 | 0.3187 |
| 6 | 0.2699 | 0.4215 |
| 7 | 0.3278 | — |
| 8 | 0.3510 | — |
| 9 | 0.3630 | — |
| 10 | 0.3897 | — |

## Active-Set Half-Life

Active set is defined as the union of `entities_in_tools` over a 5-turn sliding window.
Overlap = |active_set(t) ∩ active_set(t+Δ)| / |active_set(t)|.

| Δ (turns) | mean overlap | median overlap | n pairs |
|-----------|--------------|----------------|----------|
| 1 | 0.8717 | 0.9130 | 3264 |
| 2 | 0.7476 | 0.7727 | 3204 |
| 3 | 0.6258 | 0.6316 | 3147 |
| 5 | 0.3696 | 0.3333 | 3041 |
| 10 | 0.2825 | 0.2500 | 2817 |
| 20 | 0.2405 | 0.2000 | 2415 |

**Estimated half-life: Δ≈5 turns** (overlap drops below 0.4359)

## Entity Type Breakdown

Comparison of tool vs dialogue utilization by entity type and project.

### baime

| Entity Type | tool_u3 | dialogue_u3 | tool_u6 | dialogue_u6 | n |
|-------------|---------|-------------|---------|-------------|---|
| command | **0.2049** | 0.1972 | 0.3011 | 0.4657 | 1415 |
| file_path | **0.2735** | 0.2435 | 0.3848 | 0.4763 | 1897 |
| identifier | **0.5026** | 0.3694 | 0.6165 | 0.6340 | 2234 |

### voci

| Entity Type | tool_u3 | dialogue_u3 | tool_u6 | dialogue_u6 | n |
|-------------|---------|-------------|---------|-------------|---|
| command | 0.0962 | 0.1656 | 0.2114 | 0.4054 | 634 |
| file_path | 0.1203 | 0.1911 | 0.2074 | 0.4125 | 897 |
| identifier | 0.3181 | 0.3366 | 0.4399 | 0.5931 | 1091 |

### Overall (all projects)

| Entity Type | tool_u3 | dialogue_u3 | tool_u6 | dialogue_u6 |
|-------------|---------|-------------|---------|-------------|
| file_path | 0.1969 | 0.2173 | 0.2961 | 0.4444 |
| command | 0.1505 | 0.1814 | 0.2562 | 0.4355 |
| identifier | 0.4103 | 0.3530 | 0.5282 | 0.6136 |

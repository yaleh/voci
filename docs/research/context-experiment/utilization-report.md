# Context Utilization Analysis Report

Total records analyzed: 3325

## Utilization Curve

Mean utilization(K) — fraction of tool-use entities appearing in prior K turns:

| K | Overall Mean | voci Mean | baime Mean | Overall P50 | Overall P90 |
|---|---|---|---|---|---|
| 1 | 0.0862 | 0.0868 | 0.0859 | 0.0 | 0.3333 |
| 2 | 0.1977 | 0.1783 | 0.2072 | 0.1 | 0.5 |
| 3 | 0.2487 | 0.2323 | 0.2568 | 0.2 | 0.6667 |
| 4 | 0.3232 | 0.3027 | 0.3333 | 0.2727 | 0.75 |
| 5 | 0.3425 | 0.3187 | 0.3541 | 0.3333 | 0.75 |
| 6 | 0.4481 | 0.4215 | 0.4611 | 0.4286 | 0.8333 |


## Summary Table

- **Total triples**: 3325

- **voci triples**: 1091

- **baime triples**: 2234

- **Mean u1** (only 1 prior turn): 0.0862

- **Mean u3** (3 prior turns): 0.2487

- **Mean u6** (6 prior turns): 0.4481


### Incremental Gain from Adding Turns

| Turns Added | Marginal Gain | Cumulative Mean |
|---|---|---|
| K=1 | +0.0862 | 0.0862 |
| K=2 | +0.1115 | 0.1977 |
| K=3 | +0.051 | 0.2487 |
| K=4 | +0.0745 | 0.3232 |
| K=5 | +0.0193 | 0.3425 |
| K=6 | +0.1056 | 0.4481 |

### Key Findings

- 80% of max utilization reached at K=6 (u6=0.4481, u6=0.4481)

- Fraction with u1=0 (no entity overlap at K=1): 70.7%

- Fraction with u6=0 (no entity overlap even at K=6): 14.2%

- Short messages (<100 chars): 974 (29.3%), mean u6=0.3972


### Phase 4 Decision

Mean u6 = 0.4481

**Decision: RUN Phase 4 ablation** (mean u6 < 0.7, further investigation warranted)

## Message Type Analysis

Total messages classified: 3325


### Type Distribution

| Type | Count | % | voci | baime |
|---|---|---|---|---|
| topic_switch | 3159 | 95.0% | 1042 | 2117 |
| status_query | 9 | 0.3% | 3 | 6 |
| continuation | 40 | 1.2% | 15 | 25 |
| self_contained | 1 | 0.0% | 1 | 0 |
| other | 116 | 3.5% | 30 | 86 |

### Mean Utilization by Message Type

| Type | u1 | u2 | u3 | u6 |
|---|---|---|---|---|
| topic_switch | 0.0791 | 0.1839 | 0.2347 | 0.4191 |
| status_query | 0.0 | 0.1111 | 0.1111 | 1.0 |
| continuation | 0.7964 | 0.9048 | 0.9131 | 1.0 |
| self_contained | 0.0 | 0.0 | 0.0 | 1.0 |
| other | 0.0439 | 0.3396 | 0.4135 | 1.0 |

### Findings

- **topic_switch** (3159 msgs, 95.0%): mean u6=0.4191 — high context need; prior turns contain new entities not yet in context

- **continuation** (40 msgs, 1.2%): mean u1=0.7964 — already high entity overlap at K=1; K=1 may be sufficient

- **status_query** (9 msgs, 0.3%): mean u6=1.0 — short queries; entity overlap low

- **self_contained** (1 msgs, 0.0%): mean u6=1.0 — all needed entities in user message itself


- Type benefiting most from K=6 vs K=1: **status_query** (gain +1.0)

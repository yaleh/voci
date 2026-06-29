# Proposal: ASR Hint Format Experiment for Gemini-2.5-Flash

**Status:** Draft  
**Task:** TASK-29  
**Date:** 2026-06-29  

---

## Problem Statement

Gemini-2.5-flash is the production ASR provider (ADR-001). In TASK-40
(`docs/research/model-eval/results/report-20260629-095522.md`), entity recall
under the `hinted` condition reached **entity_recall_exact = 0.643** (all
groups, N=35). The baseline (no hint) was 0.286 — so the existing hint already
delivers a +0.357 lift.

The current hint is a single plain-text suffix injected in
`docs/research/model-eval/adapters/gemini_adapter.py` (line 46):

```python
text_prompt = "Transcribe the following audio. Known technical terms: " + ", ".join(opts.known_entities)
```

This string is placed as the first `text` part of the single `contents[0]`
block, ahead of the `inlineData` audio part. There is no explicit instruction
about how to use the list, no structure around the entity names, no
language-specific phrasing, and no example of expected output.

The research question is: **what change to hint format or content — without
modifying the model or production Go code — can push entity_recall_exact past
0.643?**

---

## Goals

1. Identify the best of four hint format variants (A / B / C / D) against the
   TASK-40 `hinted` baseline using the same 35-case test corpus.
2. Evaluate on a representative Chinese + mixed-language corpus drawn from real
   user sessions (the existing `testdata/testcases.json` already includes
   `zh-technical` and `zh-mixed` categories; see report table row groups).
3. Produce a reproducible experiment: each config writes a timestamped JSONL
   results file under `docs/research/model-eval/results/`, and a summary report
   is generated via `docs/research/model-eval/compare_models.py`.
4. Primary metric: `entity_recall_exact`. Secondary metric: `latency_s`.

---

## Non-Goals

- No changes to Go production code (`internal/`, `cmd/`).
- No new ASR model evaluation — Gemini-2.5-flash was selected by ADR-001 and
  TASK-40 confirmed it is the best-performing option.
- Not a latency optimization study. Latency is recorded and reported but does
  not drive config selection.
- No evaluation of context caching or entity subset reduction (see Alternatives
  below).

---

## Design

### Current adapter behavior (baseline for all configs)

`docs/research/model-eval/adapters/gemini_adapter.py` calls
`POST /v1beta/models/{model}:generateContent` with a single-turn `contents`
array. When `opts.known_entities` is non-empty the prompt string is:

```
Transcribe the following audio. Known technical terms: <entity1>, <entity2>, …
```

This text part precedes the `inlineData` audio part. The API key is resolved
from `GEMINI_API_KEY`, `ASR_API_KEY`, or `~/.config/voci/config.yaml`.
`run_gemini.py` populates `opts.known_entities` only when `--method hinted` is
passed; with `--method baseline` the list is empty.

### Corpus

The 35 test cases in `testdata/testcases.json` cover three categories as
defined in `report-20260629-095522.md`:

| Category | N (all groups) | Notes |
|---|---|---|
| zh-technical | 6 | Technical Chinese utterances with English entity names |
| zh-mixed | 8 | Mixed Chinese + English speech |
| (other) | 21 | Remaining cases |

All four configs run against the same 35 cases.

### Configs

#### Config A — Plain-text entity list (TASK-40 reproduction baseline)

Exact reproduction of the TASK-40 `hinted` condition. No code change from the
current adapter. Expected: entity_recall_exact ≈ 0.643 (all), ≈ 0.583
(zh-technical), ≈ 0.643 (zh-mixed).

```
Transcribe the following audio. Known technical terms: <e1>, <e2>, …
```

Purpose: confirm reproducibility before interpreting results from B, C, D.

#### Config B — XML-tagged entities + explicit instruction prefix

Wrap each entity in an `<entity>` tag and precede the list with an explicit
instruction about how to use it:

```
Transcribe the following audio exactly. The transcript MUST preserve the
spelling of the following technical terms if they appear in the audio:
<entities>
  <entity>E1</entity>
  <entity>E2</entity>
  …
</entities>
```

Rationale: structured XML may give the model a clearer parsing target than a
comma-separated list. The imperative "MUST preserve" makes the constraint
explicit rather than implied.

#### Config C — Few-shot example showing correct entity preservation

Prepend a short input/output example before the real audio part. Because the
Gemini Audio API sends audio as `inlineData` in a single turn, the few-shot is
expressed in text only (the example audio is described, not attached):

```
Transcribe the following audio. Below is an example of correct output format:

Example — if the audio contains the phrase "我们用 Sentry 来监控" and the
known term is "Sentry", the correct transcript is:
"我们用 Sentry 来监控"

Known technical terms: <e1>, <e2>, …

Now transcribe the actual audio:
```

Rationale: few-shot demonstrations are a well-established way to steer
generation format. Showing the model that entity spelling is preserved verbatim
(even when mixed into Chinese) may outperform structural tags alone.

#### Config D — Chinese-language instruction + entity list

Rewrite the instruction in Chinese to match the dominant language of the
zh-technical and zh-mixed utterances:

```
请准确转录以下音频内容。下列专有名词和技术术语必须按原样保留，不得翻译或修改：
<e1>、<e2>、…
```

Rationale: zh-technical and zh-mixed utterances are the hardest category (0.583
recall in TASK-40). A Chinese-language instruction may reduce the language-
switching overhead for the model and signal that the context is Chinese. This is
testable because zh-technical/zh-mixed results are reported as a separate group
in the existing pipeline.

### Implementation plan

1. Add a `--config {A,B,C,D}` argument to a new runner script
   `docs/research/model-eval/run_hint_format.py` (modelled on
   `docs/research/model-eval/run_gemini.py`).
2. The runner constructs the `text_prompt` string according to the config.
   Because `GeminiAdapter.transcribe()` builds the prompt internally from
   `opts.known_entities` (there is no override parameter), the runner will
   subclass `GeminiAdapter` and override `transcribe()` to substitute the
   config-specific `text_prompt` while keeping the rest of the HTTP call
   identical. The subclass writes a JSONL results file per config.
   - All four configs are always run with the full entity list (`known_entities`
     non-empty). The runner has no `--method baseline` concept; baseline
     behavior is Config A, which reproduces the TASK-40 `hinted` condition.
     If `known_entities` is unexpectedly empty for a test case, configs B, C,
     and D fall back to the bare instruction ("Transcribe the following audio
     exactly." / "Now transcribe the actual audio:" / Chinese equivalent)
     with the entity section omitted.
   - Errors (HTTP failure, empty `candidates`, malformed response) are caught
     with `try/except Exception` identical to `run_gemini.py`; on failure the
     case records `hypothesis=""` and `latency_s=0.0`.
3. Metrics are computed identically to `run_gemini.py`: `entity_recall_exact`
   (verbatim substring), `entity_recall_fuzzy` (difflib, threshold 0.3),
   `WER`, `CER`, `latency_s`.
4. `compare_models.py` is run once all four configs complete to generate the
   comparative report.

### Success criterion

Any config that achieves entity_recall_exact ≥ 0.70 on the `all` group is
considered a material improvement over the TASK-40 baseline (0.643). The
best-performing config becomes the candidate for production integration.

---

## Alternatives

### Context caching for hint

The Gemini API supports context caching for stable prefix content. If the entity
list were cached, repeated calls would incur lower token cost and possibly lower
latency. **Deferred**: entity lists in voci are per-session and vary across
users, so a stable cacheable prefix does not exist today. This becomes viable
after TASK-35 produces a per-user entity vocabulary.

### Reduce entity list to relevant subset

Injecting the full entity list may add noise for utterances where most entities
are irrelevant. TASK-35 scopes entity subset selection. **Out of scope for this
experiment**: we test hint format, not hint content. The two variables should be
isolated.

---

## Open Questions

1. **Reproducibility (Config A vs TASK-40):** Will Config A reproduce
   entity_recall_exact = 0.643? The TASK-40 run used
   `docs/research/model-eval/adapters/gemini_adapter.py` directly via
   `run_gemini.py --method hinted`. If the API is non-deterministic, Config A
   may return a different value. Acceptable variance: ± 0.05.

2. **Few-shot (C) vs structured XML (B):** Both are hypothesized to improve
   over A, but through different mechanisms (example-based vs constraint-based).
   If C > B, that favors investing in richer examples. If B > C, structured
   tagging is the simpler path.

3. **zh-technical sensitivity:** Config D is specifically motivated by the lower
   zh-technical score (0.583 vs 0.643 overall in TASK-40). If D lifts
   zh-technical but not the overall score, partial deployment (apply D only for
   Chinese sessions) may be warranted.

4. **Latency impact:** Configs C and D produce longer prompts than A. The
   TASK-40 mean latency for `gemini-2.5-flash/hinted` was 3.928 s. If any
   config exceeds ~5 s mean latency, it may be disqualified regardless of
   entity recall.

---
id: TASK-36
title: ASR contextual biasing 实验：initial_prompt 注入与两步 Knowledge Prompt
status: 'Basic: Done'
assignee: []
created_date: '2026-06-29 05:21'
updated_date: '2026-06-29 06:17'
labels:
  - 'kind:basic'
  - 'area:asr'
dependencies:
  - TASK-34
ordinal: 31000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
基于 TASK-34 发现（hint_mode=on 无效）和文献调研，验证两种改进方案：
（近期）方案 A：将 known_entities 从 system_prompt 改为注入 Whisper initial_prompt 字段，直接偏置解码器；
（中期）方案 B：两步 Knowledge Prompt——先跑 ASR 得 raw，再对 raw 做模糊匹配找候选实体，以候选实体为 prompt 重跑 ASR。
两个方案均基于现有模型（SiliconFlow whisper-large-v3 + TeleSpeechASR），不引入新模型。
依赖 TASK-34 修正后的测试用例（zh-technical/zh-mixed）。
<!-- SECTION:DESCRIPTION:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
# Plan: ASR Contextual Biasing — initial_prompt Injection & Two-Step Knowledge Prompt (TASK-36)

## Context

TASK-34 showed that injecting `known_entities` into Whisper's `system_prompt` had zero effect on
`entity_recall` and occasionally caused gemma4 to emit Arabic text. Literature (IEEE BigData 2023)
and Whisper internals confirm that `initial_prompt` (the `prompt` multipart field on SiliconFlow's
endpoint) biases the decoder at beam-search time — system_prompt does not reach the decoder.

This experiment is a Python prototype only; no production Go code is modified. All work lives in
`docs/research/contextual-biasing/`. It reuses the existing `asr-bench` metrics and adapter base
classes from `docs/research/asr-bench/`.

Test cases with `known_entities` are the `zh-technical` and `zh-mixed` category entries from
`testdata/testcases.json` (sample-22 through sample-35). Baseline WAV files are already produced
by the asr-bench pipeline.

Output JSONL schema per row:
`case_id, method, hypothesis, latency_s, entity_recall_exact, entity_recall_fuzzy`

Three methods compared:
- **baseline**: whisper-large-v3, no `prompt` field (existing asr-bench run)
- **method_a**: single-pass, `prompt = ", ".join(known_entities)`
- **method_b**: two-step — raw ASR → fuzzy match → re-ASR with matched candidates as `prompt`

---

## Phase 1: Method A — single-pass initial_prompt injection

Implement `docs/research/contextual-biasing/adapters/whisper_biased.py`:
- Inherits `ModelAdapter` from `docs/research/asr-bench/adapters/base.py`
- `name` = `"whisper-biased"`
- Builds prompt string: `", ".join(opts.known_entities)`
- POSTs to `https://api.siliconflow.cn/v1/audio/transcriptions` with fields:
  `model=openai/whisper-large-v3`, `file=<wav>`, `prompt=<entities_string>`
- Returns `(hypothesis, latency_s)`

Implement `docs/research/contextual-biasing/metrics_ext.py`:
- Adds `fuzzy_entity_recall(known_entities, hypothesis, threshold=0.3)` using `difflib.SequenceMatcher`
- A hypothesis word counts as a hit for entity `e` if
  `1 - SequenceMatcher(None, word, e.lower()).ratio() <= threshold` for any word in `hypothesis.lower().split()`
- Returns `hits / len(known_entities)` or `None` if list is empty

Implement `docs/research/contextual-biasing/run_method_a.py`:
- Loads `testdata/testcases.json`, filters cases where `known_entities` is non-empty
- Calls `WhisperBiasedAdapter.transcribe(wav_path, opts)` with `opts.known_entities` set
- Scores each result with `entity_recall` (exact, from `asr-bench/metrics.py`) and `fuzzy_entity_recall`
- Saves `category` as a string: `case["category"][0]` if the list is non-empty, else `""`
- Writes to `docs/research/contextual-biasing/results/run-method_a-{timestamp}.jsonl`

---

## Phase 2: Method B — two-step Knowledge Prompt pipeline

Implement `docs/research/contextual-biasing/adapters/whisper_twostep.py`:
- Step 1: Call whisper-large-v3 with **no** `prompt` field (raw first-pass ASR)
- Step 2: Fuzzy-match raw hypothesis words against `known_entities` via `difflib.SequenceMatcher`;
  collect candidates where `1 - ratio() <= 0.4` (normalized distance ≤ 0.4); de-duplicate preserving order
- Step 3: If candidates non-empty, re-call SiliconFlow with `prompt=", ".join(candidates)`;
  otherwise return step-1 result unchanged
- `latency_s` = sum of both API calls (or single call if no candidates)
- `name` = `"whisper-twostep"`

Implement `docs/research/contextual-biasing/run_method_b.py`:
- Same structure as `run_method_a.py` but uses `WhisperTwoStepAdapter`
- Saves `category` as a string using the same `case["category"][0]` logic
- Writes to `docs/research/contextual-biasing/results/run-method_b-{timestamp}.jsonl`
- `method` field = `"method_b"`

---

## Phase 3: Comparison report — baseline / method_A / method_B

Implement `docs/research/contextual-biasing/compare.py`:
- CLI args: `--baseline <jsonl>`, `--method-a <jsonl>`, `--method-b <jsonl>`
- Reads `category` from method_a / method_b result rows directly (stored as string by the runner)
- When reading the baseline JSONL (existing asr-bench format): map `entity_recall` → `entity_recall_exact`; set `entity_recall_fuzzy = None`; normalize `category` from list to string (`cat[0] if isinstance(cat, list) and cat else cat`)
- For each category in `["zh-technical", "zh-mixed"]` and combined, computes:
  - mean `entity_recall_exact` per method (skip `None` values)
  - mean `entity_recall_fuzzy` per method (skip `None` values)
  - mean `latency_s` per method
- Writes Markdown table to `docs/research/contextual-biasing/results/report-{timestamp}.md`
- Prints summary to stdout

---

## Constraints

1. No modifications to production Go code (`internal/` files are read-only reference).
2. No modifications to `docs/research/asr-bench/` — new code only imports from it.
3. Fuzzy candidate selection threshold for method B: Levenshtein (difflib) normalized distance ≤ 0.4.
4. Fuzzy scoring threshold for `fuzzy_entity_recall`: normalized distance ≤ 0.3.
5. SiliconFlow model for all new adapters: `openai/whisper-large-v3` (not TeleSpeechASR).
6. All result files land under `docs/research/contextual-biasing/results/` named `run-{method}-{timestamp}.jsonl`.
7. Standard library + `requests` + `difflib` only — no additional pip installs beyond what asr-bench already uses.
8. `category` in output JSONL rows must be stored as a plain string (not a list): use `case["category"][0]` when `case["category"]` is a non-empty list, else `""`. This ensures compare.py string-equality checks against `"zh-technical"` / `"zh-mixed"` work correctly.
9. The existing asr-bench baseline JSONL uses `entity_recall` (not `entity_recall_exact`) and has no `entity_recall_fuzzy` field; compare.py must handle this schema difference when reading `--baseline` files.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Plan review iteration 1: NEEDS_REVISION — two data-contract bugs fixed:
1. testdata/testcases.json stores `category` as a list (e.g. `["zh-technical"]`), not a string. run_method_a/b.py must flatten it to string via `case["category"][0]`, and compare.py must normalize when reading baseline rows. Added as Constraints 8 & 9 and explicit bullet points in Phase 1/2/3 implementation notes.
2. Existing asr-bench baseline JSONL uses field `entity_recall` (not `entity_recall_exact`) and has no `entity_recall_fuzzy`; compare.py --baseline reader must remap those fields. Documented in Phase 3 implementation spec.
3. Phase 2 DoD strengthened: assertion now checks `entity_recall_exact` and `entity_recall_fuzzy` presence (matches Phase 1 coverage level).

Plan review iteration 3: APPROVED

cap:propose=approved

claimed: 2026-06-29T05:46:01Z
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
**FINISH** — All 32 DoD checks pass.

## Verdict

FINISH — experiment complete, all artifacts delivered and verified.

## Key Findings

Neither method_a (single-pass `initial_prompt` injection) nor method_b (two-step fuzzy re-ASR) improved entity recall over baseline on the SiliconFlow whisper-large-v3 endpoint:

| group | baseline | method_a | method_b |
|---|---|---|---|
| all — entity_recall_exact | 0.214 | 0.214 | 0.214 |
| all — entity_recall_fuzzy | 0.357 | 0.357 | 0.357 |
| zh-technical — exact | 0.000 | 0.000 | 0.000 |
| zh-mixed — exact | 0.286 | 0.286 | 0.286 |
| latency_s (all) | 2.08s | 2.03s | 3.21s (+54%) |

The `prompt` field has no measurable effect on entity recall for this endpoint/workload. Method_b adds ~1.1s latency per case with zero benefit.

## Artifacts

- `docs/research/contextual-biasing/adapters/whisper_biased.py` — Method A adapter
- `docs/research/contextual-biasing/adapters/whisper_twostep.py` — Method B adapter
- `docs/research/contextual-biasing/metrics_ext.py` — fuzzy_entity_recall metric
- `docs/research/contextual-biasing/run_method_a.py` / `run_method_b.py` — runners
- `docs/research/contextual-biasing/compare.py` — comparison report generator
- `docs/research/contextual-biasing/results/report-20260629-055548.md` — final report

## Next Steps

The `initial_prompt` approach appears ineffective on SiliconFlow's hosted whisper endpoint (likely stripped server-side). Future directions: (1) test with a self-hosted Whisper instance where `initial_prompt` reaches the decoder; (2) evaluate a post-processing approach (LLM-based entity correction on raw hypothesis); (3) accept the null result and proceed to production with baseline ASR + downstream NLU entity extraction.
<!-- SECTION:FINAL_SUMMARY:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 test -f /home/yale/work/voci/docs/research/contextual-biasing/adapters/whisper_biased.py
- [x] #2 grep -q "prompt" /home/yale/work/voci/docs/research/contextual-biasing/adapters/whisper_biased.py
- [x] #3 grep -q "openai/whisper-large-v3" /home/yale/work/voci/docs/research/contextual-biasing/adapters/whisper_biased.py
- [x] #4 test -f /home/yale/work/voci/docs/research/contextual-biasing/metrics_ext.py
- [x] #5 grep -q "fuzzy_entity_recall" /home/yale/work/voci/docs/research/contextual-biasing/metrics_ext.py
- [x] #6 grep -q "SequenceMatcher" /home/yale/work/voci/docs/research/contextual-biasing/metrics_ext.py
- [x] #7 python3 -c "import sys; sys.path.insert(0,'/home/yale/work/voci/docs/research/contextual-biasing'); from metrics_ext import fuzzy_entity_recall; assert fuzzy_entity_recall(['BuildContext'], 'fixed BuildContext bug') == 1.0; assert fuzzy_entity_recall([], 'x') is None; print('ok')" 2>&1 | grep -q ok
- [x] #8 test -f /home/yale/work/voci/docs/research/contextual-biasing/run_method_a.py
- [x] #9 grep -q "entity_recall_exact" /home/yale/work/voci/docs/research/contextual-biasing/run_method_a.py
- [x] #10 grep -q "entity_recall_fuzzy" /home/yale/work/voci/docs/research/contextual-biasing/run_method_a.py
- [x] #11 ls /home/yale/work/voci/docs/research/contextual-biasing/results/run-method_a-*.jsonl 2>/dev/null | grep -q .
- [x] #12 python3 -c "import json,glob; rows=[json.loads(l) for f in glob.glob('/home/yale/work/voci/docs/research/contextual-biasing/results/run-method_a-*.jsonl') for l in open(f)]; assert all('entity_recall_exact' in r and 'entity_recall_fuzzy' in r and r['method']=='method_a' for r in rows); print('ok')" 2>&1 | grep -q ok
- [x] #13 test -f /home/yale/work/voci/docs/research/contextual-biasing/adapters/whisper_twostep.py
- [x] #14 grep -q "0.4" /home/yale/work/voci/docs/research/contextual-biasing/adapters/whisper_twostep.py
- [x] #15 grep -q "SequenceMatcher" /home/yale/work/voci/docs/research/contextual-biasing/adapters/whisper_twostep.py
- [x] #16 test -f /home/yale/work/voci/docs/research/contextual-biasing/run_method_b.py
- [x] #17 ls /home/yale/work/voci/docs/research/contextual-biasing/results/run-method_b-*.jsonl 2>/dev/null | grep -q .
- [x] #18 python3 -c "import json,glob; rows=[json.loads(l) for f in glob.glob('/home/yale/work/voci/docs/research/contextual-biasing/results/run-method_b-*.jsonl') for l in open(f)]; assert all(r['method']=='method_b' and 'entity_recall_exact' in r and 'entity_recall_fuzzy' in r for r in rows); print('ok')" 2>&1 | grep -q ok
- [x] #19 test -f /home/yale/work/voci/docs/research/contextual-biasing/compare.py
- [x] #20 grep -q "zh-technical" /home/yale/work/voci/docs/research/contextual-biasing/compare.py
- [x] #21 grep -q "zh-mixed" /home/yale/work/voci/docs/research/contextual-biasing/compare.py
- [x] #22 grep -q "\-\-baseline" /home/yale/work/voci/docs/research/contextual-biasing/compare.py
- [x] #23 ls /home/yale/work/voci/docs/research/contextual-biasing/results/report-*.md 2>/dev/null | grep -q .
- [x] #24 grep -q "method_a" /home/yale/work/voci/docs/research/contextual-biasing/results/report-*.md
- [x] #25 grep -q "method_b" /home/yale/work/voci/docs/research/contextual-biasing/results/report-*.md
- [x] #26 grep -q "entity_recall_exact" /home/yale/work/voci/docs/research/contextual-biasing/results/report-*.md
- [x] #27 grep -q "entity_recall_fuzzy" /home/yale/work/voci/docs/research/contextual-biasing/results/report-*.md
- [x] #28 test -d /home/yale/work/voci/docs/research/contextual-biasing/results
- [x] #29 ls /home/yale/work/voci/docs/research/contextual-biasing/results/run-method_a-*.jsonl 2>/dev/null | grep -q .
- [x] #30 ls /home/yale/work/voci/docs/research/contextual-biasing/results/run-method_b-*.jsonl 2>/dev/null | grep -q .
- [x] #31 ls /home/yale/work/voci/docs/research/contextual-biasing/results/report-*.md 2>/dev/null | grep -q .
- [x] #32 grep -q "method_b" /home/yale/work/voci/docs/research/contextual-biasing/results/report-*.md
<!-- DOD:END -->

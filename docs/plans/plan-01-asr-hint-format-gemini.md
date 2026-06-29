# Plan: ASR Hint Format Experiment — Gemini-2.5-flash 中英混合语音 (TASK-29)

**Status:** Draft
**Task:** TASK-29
**Proposal:** `docs/proposals/proposal-asr-hint-format-gemini.md`
**Date:** 2026-06-29

---

## Overview

Experiment: Gemini hint format optimization (TASK-29). 4 phases: corpus + TTS → configs A/B/C/D run → Phase 3 skip file → analysis.

TASK-40 established the baseline: Gemini-2.5-flash with plain-text entity hint achieves `entity_recall_exact = 0.643` (N=35, all groups). This plan tests four hint format variants (A/B/C/D) to find whether a different prompt structure can push recall past 0.643. No Go production code is modified. All work lives under `docs/research/`.

---

## Phase 1: Corpus Building and TTS Synthesis

**Objective**: Build a representative zh/zh-mixed test corpus with annotations, then synthesize audio for all 30 annotated entries.

### Stage 1.1 — Extract candidates from meta-cc session data

**This is an interactive agent step, not a script dependency.** Use the `mcp__plugin_meta-cc_meta-cc__query_session_content` MCP tool (available in the Claude Code session) to pull user messages from voci and baime project sessions. This tool queries Claude Code session transcripts, not a Python-callable API; it must be invoked by the executing agent at plan-run time.

Filter criteria:
- Length 10–80 characters
- Contains at least one technical term matching: `loop-backlog|meta-cc|voci|feature-to-backlog|backlog\.md|--[a-z]|/[a-z]+/|TASK-\d`
- Exclude system-injected messages and skill invocations

Write results to `docs/research/asr-experiment/asr-corpus-candidates.jsonl`. Each line is a JSON object with fields:
- `text` — the raw user message
- `source_project` — project name (e.g. `voci`, `baime`)
- `timestamp` — ISO-8601 timestamp from the session

Target: ≥50 lines.

**If fewer than 50 candidates are returned:** lower the minimum length filter to 8 characters and re-query. If still < 30 candidates after relaxing filters, supplement with manually authored realistic zh-technical/zh-mixed utterances to reach 30 (document each synthetic entry with `"synthetic": true`). Do not halt — proceed to Stage 1.2 with whatever candidates are available, as long as ≥30 exist.

### Stage 1.2 — Annotate 30 entries

From the candidates, select 30 representative entries (prioritise zh-technical and zh-mixed utterances containing embedded English identifiers). For each, add:
- `expected_entities` — list of technical terms that must survive transcription verbatim (e.g. `["loop-backlog", "TASK-29", "--iterate"]`)
- `expected_rewrite` — the clean, correctly-cased instruction text
- `category` — one of `"zh-technical"`, `"zh-mixed"`, `"zh-pure"`, or `""` (needed by Phase 4 entity-type breakdown)

Write to `docs/research/asr-experiment/asr-test-corpus.jsonl` (30 lines, one JSON object per line).

Entity type distribution to target across the 30 entries:
- tool-name (e.g. `voci`, `meta-cc`, `loop-backlog`): ~10 entries
- CLI flag (e.g. `--iterate`, `--file`): ~8 entries
- TASK-id (e.g. `TASK-29`, `TASK-32`): ~7 entries
- file path / Go symbol (e.g. `internal/asr`, `RunHinted`): ~5 entries

### Stage 1.3 — TTS synthesis

For each of the 30 annotated entries, synthesise a WAV file using:

```bash
edge-tts --voice zh-CN-XiaoxiaoNeural --text "<entry.text>" --write-media docs/research/asr-experiment/audio/<id>.wav
```

`<id>` is a zero-padded index: `corpus-01.wav` … `corpus-30.wav`. Do not reuse existing WAV files from `testdata/`.

**Acceptance**:
```bash
[ $(wc -l < docs/research/asr-experiment/asr-corpus-candidates.jsonl) -ge 30 ]
grep -q '"text"' docs/research/asr-experiment/asr-corpus-candidates.jsonl
[ $(wc -l < docs/research/asr-experiment/asr-test-corpus.jsonl) -ge 30 ]
grep -q '"expected_entities"' docs/research/asr-experiment/asr-test-corpus.jsonl
grep -q '"expected_rewrite"' docs/research/asr-experiment/asr-test-corpus.jsonl
grep -q '"category"' docs/research/asr-experiment/asr-test-corpus.jsonl
ls docs/research/asr-experiment/audio/corpus-30.wav
```

Note: the candidates file acceptance threshold is ≥30 (not 50) because the fallback procedure (Stage 1.1) guarantees at least 30 entries via synthetic augmentation.

---

## Phase 2: Hint Format Experiment (Configs A/B/C/D)

**Objective**: Run 4 hint format variants against the 30-entry annotated corpus using the Gemini Audio API. Record per-case metrics to JSONL.

### Stage 2.1 — Write `run_experiment.py`

Write `docs/research/asr-experiment/run_experiment.py`. The script:

1. Loads `docs/research/asr-experiment/asr-test-corpus.jsonl` (30 entries).
2. Defines four subclasses of `GeminiAdapter` (imported from `docs/research/model-eval/adapters/gemini_adapter.py`), each overriding `transcribe()` to substitute a config-specific `text_prompt` while keeping the HTTP call structure identical to the base adapter.

**Config A — Plain-text entity list (TASK-40 reproduction baseline)**

```python
text_prompt = "Transcribe the following audio. Known technical terms: " + ", ".join(opts.known_entities)
```

Purpose: establish the experiment's own baseline on the new corpus. Config A is run first and its mean `entity_recall_exact` becomes the reference score for B, C, D delta computation. **Do not halt if Config A scores below 0.643** — the new TTS corpus differs from TASK-40's 35-case set so an exact match is not expected. The gate condition for halting is: if Config A scores < 0.30 (entity recall less than half the TASK-40 baseline), this indicates a corpus or API problem (bad TTS audio, wrong entity annotation, API key failure) and the script must print a diagnostic and exit with a non-zero exit code before running B, C, D. The TASK-40 value of 0.643 is recorded as `task40_reference` in the report for comparison, not as a threshold.

**Config B — XML-tagged entities + explicit instruction prefix**

```python
entities_xml = "\n".join(f"  <entity>{e}</entity>" for e in opts.known_entities)
text_prompt = (
    "Transcribe the following audio exactly. The transcript MUST preserve the "
    "spelling of the following technical terms if they appear in the audio:\n"
    f"<entities>\n{entities_xml}\n</entities>"
)
```

**Config C — Few-shot example showing correct entity preservation**

```python
text_prompt = (
    "Transcribe the following audio. Below is an example of correct output format:\n\n"
    "Example — if the audio contains the phrase \"我们用 Sentry 来监控\" and the "
    "known term is \"Sentry\", the correct transcript is:\n"
    "\"我们用 Sentry 来监控\"\n\n"
    "Known technical terms: " + ", ".join(opts.known_entities) + "\n\n"
    "Now transcribe the actual audio:"
)
```

**Config D — Chinese-language instruction + entity list**

```python
entity_str = "、".join(opts.known_entities)
text_prompt = (
    "请准确转录以下音频内容。下列专有名词和技术术语必须按原样保留，不得翻译或修改：\n"
    + entity_str
)
```

Fallback (when `opts.known_entities` is empty): each config uses its bare instruction with the entity section omitted.

3. For each testcase × config, calls the Gemini Audio API and records one JSON line to `docs/research/asr-experiment/results.jsonl`:

```json
{
  "config": "A",
  "test_id": "corpus-01",
  "transcript": "...",
  "entity_recall_exact": 0.75,
  "latency_s": 1.234,
  "prompt_tokens": 87,
  "expected_entities": ["voci", "TASK-29"],
  "category": "zh-technical"
}
```

- `entity_recall_exact`: fraction of `expected_entities` found as case-insensitive substrings in `transcript`. Use the same computation as `run_gemini.py` (verbatim substring match). **Field must be named `entity_recall_exact` to match the existing pipeline metric name and enable delta comparison with TASK-40.**
- `latency_s`: wall-clock **seconds** (float) for the API call. `GeminiAdapter.transcribe()` returns `(str, float)` where the float is already in seconds — store it directly. Do not convert to milliseconds; the existing pipeline uses seconds.
- `prompt_tokens`: extracted from `usageMetadata.promptTokenCount` in the Gemini response if present, else `null`.
- `category`: copied from the corpus entry's `category` field.

4. Error handling: `try/except Exception` identical to `run_gemini.py`; on failure records `transcript=""`, `entity_recall_exact=0.0`, `latency_s=0.0`.

5. API key resolution: uses `GEMINI_API_KEY` env var (same resolution order as `GeminiAdapter.__init__`).

### Stage 2.2 — Execute

```bash
python3 docs/research/asr-experiment/run_experiment.py
```

Total API calls: 30 entries × 4 configs = 120 calls. Expected JSONL lines: 120.

**Acceptance**:
```bash
grep -q '"config":"A"' docs/research/asr-experiment/results.jsonl
grep -q '"config":"B"' docs/research/asr-experiment/results.jsonl
grep -q '"config":"C"' docs/research/asr-experiment/results.jsonl
grep -q '"config":"D"' docs/research/asr-experiment/results.jsonl
[ $(grep -c '"config"' docs/research/asr-experiment/results.jsonl) -ge 100 ]
grep -q '"entity_recall_exact"' docs/research/asr-experiment/results.jsonl
```

---

## Phase 3: Skip File

**Objective**: Record that alternative ASR model comparison is complete (done in TASK-40); no additional model evaluation needed.

### Stage 3.1 — Write skip file

Write `docs/research/asr-experiment/phase3-skipped.txt` with exactly this content:

```
Skipped: alternative ASR model comparison completed in TASK-40. Gemini-2.5-flash selected as production ASR (ADR-001). entity_recall_exact: flash/hinted=0.643 vs whisper=0.286 vs qwen3=0.214.
```

**Acceptance**:
```bash
test -f docs/research/asr-experiment/phase3-skipped.txt && grep -q '.' docs/research/asr-experiment/phase3-skipped.txt
```

---

## Phase 4: Analysis

**Objective**: Compare configs A/B/C/D across `entity_recall_exact` and `latency_s`, classify results by entity type, compute delta vs Config A (experiment baseline) and vs the TASK-40 reference (0.643), and produce a recommendation.

### Stage 4.1 — Write `analyze.py`

Write `docs/research/asr-experiment/analyze.py`. The script:

1. Reads `docs/research/asr-experiment/results.jsonl`.
2. Computes per-config statistics:
   - `mean_entity_recall_exact` (mean across all 30 test entries)
   - `mean_latency_s`
   - `delta_vs_config_a` (config X mean − Config A mean; Config A is the experiment's own baseline)
3. Computes entity type breakdown for each config:
   - Classify each expected entity into: `tool-name` (voci, meta-cc, loop-backlog, etc.), `cli-flag` (starts with `--`), `task-id` (matches `TASK-\d+`), `other` (file paths, Go symbols).
   - Report mean `entity_recall_exact` per entity type per config.
4. Prints all tables to stdout in Markdown format.
5. Prints a `## Recommendation` section: names the best-performing config (highest mean `entity_recall_exact`), notes Config A's score vs the TASK-40 reference of 0.643, notes whether the best config surpasses the proposal's 0.70 target, and recommends whether to integrate into production.

### Stage 4.2 — Run and capture report

```bash
python3 docs/research/asr-experiment/analyze.py > docs/research/asr-experiment/report.md
```

The report must contain the `## Recommendation` section, `entity_recall_exact` values, and `latency_s` values.

**Acceptance**:
```bash
grep -q '## Recommendation' docs/research/asr-experiment/report.md
grep -q 'entity_recall_exact' docs/research/asr-experiment/report.md
grep -q 'latency_s' docs/research/asr-experiment/report.md
```

---

## Dependencies

- TASK-40 baseline data: `docs/research/model-eval/results/report-20260629-095522.md` (entity_recall_exact=0.643 reference)
- Adapter to subclass: `docs/research/model-eval/adapters/gemini_adapter.py`
- Existing test cases (35 entries, for reference only): `testdata/testcases.json`
- `docs/research/model-eval/run_gemini.py` — reference for metrics computation pattern (`entity_recall_exact`, `wer`, `cer`)
- `docs/research/asr-bench/adapters/base.py` — `ModelAdapter`, `TranscribeOpts` base classes
- `edge-tts` CLI (must be installed: `pip install edge-tts`)

---

## Constraints

- No Go production code changes (`internal/`, `cmd/` are read-only references).
- Gemini API key from env `GEMINI_API_KEY` (same resolution as existing adapter; also accepts `ASR_API_KEY` or `~/.config/voci/config.yaml`).
- **Config A sanity gate**: since the new 30-entry TTS corpus differs from the TASK-40 35-case set, Config A's score on the new corpus is expected to differ from 0.643. The experiment script must halt (non-zero exit, diagnostic printed) only if Config A's mean `entity_recall_exact` across the 30 corpus entries is < 0.30. A score below 0.30 indicates a systematic problem (bad TTS audio, incorrect entity annotation, API key failure) rather than a legitimate corpus difference. The TASK-40 value of 0.643 is recorded as a reference in the final report; Config A's own score on the new corpus is the baseline for B/C/D comparison.
- TTS audio synthesised with `edge-tts --voice zh-CN-XiaoxiaoNeural`; do not reuse any existing WAV files from `testdata/`.
- All output paths relative to project root `/home/yale/work/voci/`.
- Python 3.10+ stdlib only for HTTP calls (use `urllib.request` as in `gemini_adapter.py`); no `requests` dependency.

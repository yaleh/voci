# Proposal: ASR Pipeline Merge Experiment

## Problem Statement

The current voci voice pipeline executes three serial LLM steps for every voice command:

```
Audio → [Step 1] Gemini Audio API (transcribe + entity correction)
             ↓ hinted transcript (string)
        [Step 2] Rewrite LLM call (text-in / text-out)
             ↓ rewritten instruction (string)
        [Step 3] Classify LLM call (text-in / JSON-out)
             ↓ {kind, confidence}
```

Steps 2 and 3 are separate HTTP round-trips to the LLM backend after Gemini has already processed the audio. Each adds approximately 1–2 seconds of latency. Because Step 3 depends on the output of Step 2, they cannot be parallelised: the pipeline is inherently serial.

The Gemini Audio API (endpoint template `https://generativelanguage.googleapis.com/v1beta/models/{model}:generateContent`, see `docs/research/model-eval/adapters/gemini_adapter.py`) already accepts arbitrary instruction prompts alongside an inline audio payload. Nothing prevents the single Gemini call from returning structured JSON that covers all three steps.

## Goals

1. Verify that a single Gemini Audio API call can return `{transcript, rewritten, kind, confidence}` with quality no worse than the current three-call pipeline.
2. Quantify the end-to-end latency reduction from eliminating two HTTP round-trips (Steps 2 and 3).
3. Produce a repeatable Python experiment with a 30-entry annotated corpus so the result is comparable to the TASK-29 ASR quality benchmark.

## Non-Goals

- No changes to Go production code (`internal/pipeline/pipeline.go`, `internal/intent/classify.go`, or any other Go file). The experiment is Python-only.
- No changes to the Gemini adapter in `docs/research/model-eval/adapters/gemini_adapter.py`; the experiment script may reuse or adapt it.
- No deployment or integration work; this experiment produces a recommendation only.
- No evaluation of non-Gemini models.

## Design

### Current Pipeline

**Step 1 — Transcription with entity correction**

Implemented in production via the Gemini Audio API (equivalent to `RunHinted` in `internal/pipeline/pipeline.go`, line 17). The Config C few-shot prompt from `docs/research/asr-experiment/run_experiment.py` (lines 131–144) is the prompt format selected by TASK-29 (entity_recall_exact = 0.8944). It injects a few-shot example showing correct entity preservation, followed by the known entity list and the audio payload.

**Step 2 — Rewrite**

`Rewrite(ctx, hinted, hint string, chatFn ChatFn)` in `internal/pipeline/pipeline.go` (line 53). Makes a standalone text LLM call. The system prompt instructs the model to normalise the transcription into a clean instruction: preserve language, do not translate, do not add unstated content, start with `[ambiguous]` if too vague. Only the `## Known Entities` section of the hint is forwarded (extracted by `knownEntities(hint string)` at line 83); the broader project context is withheld to prevent over-elaboration.

**Step 3 — Classify**

`Classify(ctx context.Context, rewritten, fullContext string, chat pipeline.ChatFn) (ActionProposal, error)` in `internal/intent/classify.go` (line 21). Makes a standalone text LLM call. The system prompt asks the model to classify the rewritten text into one of four `Kind` values defined in `internal/intent/proposal.go`:

- `direct_prompt` (`KindDirectPrompt`) — a direct programming instruction
- `backlog_action` (`KindBacklogAction`) — an action targeting the task backlog
- `query` (`KindQuery`) — an information request about the project
- `ambiguous` (`KindAmbiguous`) — intent cannot be determined

The model returns `{"kind": "...", "confidence": 0.0}`. The result is unmarshalled into an `ActionProposal` struct (fields: `Kind`, `Rewritten`, `RawTranscript`, `Confidence`, `ContextUsed`).

### Merged Call Design

**Prompt structure**

The merged system prompt is built in three layers:

1. **Config C few-shot base** (from `docs/research/asr-experiment/run_experiment.py` lines 131–144):
   ```
   Transcribe the following audio. Below is an example of correct output format:

   Example — if the audio contains the phrase "我们用 Sentry 来监控" and the
   known term is "Sentry", the correct transcript is:
   "我们用 Sentry 来监控"

   Known technical terms: <comma-separated entity list>
   ```

2. **Rewrite rules** (derived from `Rewrite()` system prompt in `internal/pipeline/pipeline.go` lines 55–67):
   ```
   Then rewrite the transcript into a clean, well-formed instruction.
   Rules:
   - Preserve the speaker's exact scope, intent, and LANGUAGE. Do NOT translate.
   - Only fix grammar/disfluency and resolve entity references the speaker explicitly made.
   - Do NOT add details, steps, or specific targets the speaker did not say.
   - If genuinely too vague to act on, start with [ambiguous].
   ```

3. **Classify instruction** (derived from `Classify()` system prompt in `internal/intent/classify.go` lines 22–36):
   ```
   Then classify the rewritten instruction into exactly one of:
   - direct_prompt: a direct programming instruction
   - backlog_action: an action targeting the task backlog
   - query: an information request about the project
   - ambiguous: the intent cannot be determined with confidence
   ```

**JSON output instruction**

Appended after the three layers:

```
Return ONLY this JSON object, no other text:
{"transcript": "...", "rewritten": "...", "kind": "...", "confidence": 0.0}
```

**JSON schema**

| Field | Type | Description |
|---|---|---|
| `transcript` | string | Raw transcription with entity corrections applied (Config C behaviour) |
| `rewritten` | string | Normalised clean instruction; starts with `[ambiguous]` if too vague |
| `kind` | string | One of `direct_prompt`, `backlog_action`, `query`, `ambiguous` |
| `confidence` | float | Model confidence in the `kind` classification, clamped to [0.0, 1.0] |

**Entity hint injection**

Identical to Config C: the `known_entities` list is injected as comma-separated technical terms in the text part of the `contents[0].parts` array, before the `inlineData` audio part. The audio payload itself is base64-encoded WAV data, matching the pattern in `GeminiAdapter.transcribe()` (`docs/research/model-eval/adapters/gemini_adapter.py`, lines 43–79).

### Evaluation

**Metrics**

| Metric | Description |
|---|---|
| `rewrite_entity_recall` | Fraction of entities expected in `expected_rewrite` that appear in the actual `rewritten` field |
| `classify_accuracy` | Fraction of cases where actual `kind` equals `expected_kind` |
| `latency_total_ms` | Wall-clock time for the full pipeline per case (mean over 30 cases) |
| `parse_error_rate` | Fraction of cases where the merged call returns unparseable JSON (tracked separately; not counted as classify errors) |

**Baseline**

Run the current three-step pipeline (Steps 1 + 2 + 3) on all 30 entries in `docs/research/pipeline-merge/testcases-annotated.json`. Record per-case results and aggregate metrics in `docs/research/pipeline-merge/baseline.json`:

```json
{
  "rewrite_entity_recall": <float>,
  "classify_accuracy": <float>,
  "latency_total_ms": <float>
}
```

**Experiment**

Run the merged single-call prompt on the same 30 entries. Append one JSON line per case to `docs/research/pipeline-merge/results.jsonl`:

```json
{"case_id": "...", "transcript": "...", "rewritten": "...", "kind": "...", "confidence": 0.0, "latency_ms": 0.0, "parse_error": false}
```

**Success criteria**

The merged call is considered acceptable for productionisation if both of the following hold:

- `classify_accuracy_delta >= -0.05` (accuracy drop no more than 5 percentage points vs. baseline)
- `rewrite_entity_recall_delta >= -0.05` (entity recall drop no more than 5 percentage points vs. baseline)

If both criteria are met: "可工程化，建议替换三段流水线"  
If either criterion is not met: "质量损失不可接受，保留三段流水线"

### Implementation Plan

**Directory:** `docs/research/pipeline-merge/`

**Files to create:**

| File | Purpose |
|---|---|
| `testcases-annotated.json` | 30-entry corpus with `expected_rewrite` and `expected_kind` fields |
| `merged_prompt.txt` | The merged system prompt text for review and version control |
| `run_experiment.py` | Calls Gemini Audio API for each testcase; appends to `results.jsonl` |
| `baseline.json` | Aggregate baseline metrics from the three-call pipeline |
| `results.jsonl` | One JSON line per merged-call result |
| `analyze.py` | Computes delta metrics; writes `report.md` |
| `report.md` | Final comparison table and conclusion |

**`run_experiment.py` approach:**

Reuse the `GeminiAdapter` pattern from `docs/research/model-eval/adapters/gemini_adapter.py`. Construct the request body with `contents[0].parts` containing the merged text prompt (injecting known entities from each testcase) and the `inlineData` audio part. POST to `_URL_TEMPLATE.format(model=self.model)` with `x-goog-api-key` header. Parse the JSON response from `candidates[0].content.parts[0].text`. On JSON parse failure, record `parse_error: true` and skip the case for `classify_accuracy` and `rewrite_entity_recall` aggregation.

**`analyze.py` approach:**

Load `results.jsonl` (merged) and `baseline.json` (three-call). Compute `rewrite_entity_recall`, `classify_accuracy`, and `latency_total_ms` for the merged set. Compute deltas. Write `report.md` with the comparison table and the pass/fail conclusion string.

## Alternatives

**Alt A: Use Gemini `response_schema` for structured JSON output**

Gemini supports a `response_schema` field in the request body that enforces structured JSON output without requiring the model to self-format. This would eliminate JSON parse errors from the merged call. However, it adds complexity to the request builder and may interact with the audio processing in ways that require additional investigation. This is worth considering as a follow-up if parse error rate proves high in the experiment.

**Alt B: Keep 3 steps but pipeline them concurrently**

Not applicable. Step 2 (`Rewrite`) requires the transcript output from Step 1, and Step 3 (`Classify`) requires the rewritten output from Step 2. All three steps are data-dependent in sequence; there is no opportunity for concurrent execution.

**Alt C: Merge only Steps 2 and 3 (keep ASR separate)**

Merge `Rewrite` and `Classify` into a single text LLM call, while keeping the Gemini Audio API call as a separate Step 1. This saves one HTTP round-trip instead of two. It is a lower-risk option because the ASR quality (Config C, entity_recall = 0.8944 from TASK-29) is known and unchanged. The experiment should note this as a fallback if the full three-in-one merge degrades transcription quality.

## Open Questions

1. **Mixed-language JSON compliance**: The merged call must handle mixed Chinese/English audio and return a well-formed JSON object. Will Gemini reliably follow a JSON output instruction when the audio content is in Chinese and the transcript field contains Chinese text? The TASK-29 experiment showed Config C achieves 1.0 entity_recall_exact on zh-mixed cases, but those prompts only required a plain-text transcript response. A JSON wrapper adds formatting pressure.

2. **Transcription quality degradation**: Adding rewrite and classify instructions to the system prompt increases the prompt complexity. Does the additional instruction load cause Gemini to trade off transcription accuracy for instruction-following? The experiment must measure `rewrite_entity_recall` (not just `classify_accuracy`) to detect any regression in the transcription/correction step.

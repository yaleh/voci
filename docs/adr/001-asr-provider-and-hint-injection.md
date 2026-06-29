# ADR-001: ASR Provider Selection and Entity Hint Injection

**Status**: Accepted  
**Date**: 2026-06-29  
**Tasks**: TASK-34, TASK-36, TASK-37, TASK-38, TASK-39, TASK-40, TASK-29

---

## Context

voci needs to transcribe Chinese+English mixed speech containing technical entities —
camelCase identifiers (`BuildContext`, `DynamicEntitiesSource`), task IDs (`TASK-32`),
and project-specific terms. Standard ASR models trained on general speech often miss
or mangle these tokens.

The core hypothesis: injecting a known-entity list into the ASR request (as a prompt
or vocabulary hint) should improve recall of technical terms.

---

## Experiments Conducted

### TASK-34 — Baseline benchmark (SiliconFlow)

Ran TeleSpeechASR (Chinese) and `openai/whisper-large-v3` via SiliconFlow against
35 test samples spanning `zh-technical`, `zh-mixed`, and general categories.

| model | CER | entity_recall_exact |
|---|---|---|
| TeleSpeechASR | 0.432 | 0.238 |
| whisper-large-v3 (baseline) | — | 0.214 |

### TASK-36/37 — Contextual biasing on SiliconFlow

Tested two hint injection strategies on SiliconFlow's hosted Whisper and SenseVoiceSmall:
- **method_a**: inject `known_entities` as `prompt` field
- **method_b**: two-pass — raw ASR → fuzzy match → re-ASR with matched candidates

**Result**: zero improvement. `entity_recall_exact` unchanged across all methods and groups.

**Conclusion**: SiliconFlow strips the `prompt` field server-side before it reaches the
decoder. Hint injection is architecturally impossible with pure-ASR hosted APIs.

### TASK-29 — Gemini hint format optimization (zh/zh-mixed corpus)

Built a 30-entry corpus from real user sessions (meta-cc, zh-CN-XiaoxiaoNeural TTS),
tested 4 hint format variants against Gemini-2.5-flash:

| config | format | entity_recall_exact | delta vs A |
|---|---|---|---|
| A | plain-text entity list（TASK-40 reproduction） | 0.639 | — |
| B | XML-tagged entities + explicit instruction | 0.839 | +0.200 |
| C | few-shot example + entity list | **0.894** | **+0.256** |
| D | Chinese-language instruction + entity list | 0.856 | +0.217 |

Config A reproduced the TASK-40 hinted baseline (0.639 vs 0.643 — within corpus variance).

**Key finding**: CLI flags (`--planSet`, `--set-field`) scored 0.0 with Config A and 1.0
with Config C. TASK-id recall improved from 0.429 (A) to 0.714 (C).

**Few-shot prompt is domain-agnostic**: the example uses a generic technical term (`Sentry`),
not voci-specific entities. The entity list is injected dynamically at runtime.

### TASK-40 — OpenRouter & Gemini multi-model comparison

After implementing OpenRouter (JSON+base64) and Gemini (generateContent) adapters
(TASK-38/39), ran all available models with `baseline` vs `hinted` (entity list in prompt):

| model | baseline entity_recall_exact | hinted | delta |
|---|---|---|---|
| openrouter/whisper-large-v3-turbo | 0.286 | 0.286 | 0 |
| openrouter/qwen3-asr-flash | 0.214 | 0.214 | 0 |
| gemini-2.5-flash | 0.286 | **0.643** | **+0.357** |
| gemini-2.5-pro | 0.286 | 0.571 | +0.286 |

Note: `openai/gpt-4o-transcribe` is blocked by OpenRouter (OpenAI ToS violation on proxying).

---

## Findings

1. **Pure ASR APIs discard prompts universally.** SiliconFlow Whisper, OpenRouter
   Whisper, and OpenRouter Qwen3-ASR all show zero hint effect. The `prompt` field is
   either ignored or stripped at the API layer before reaching the model decoder.

2. **Multimodal LLMs genuinely use the text prompt.** Gemini `generateContent` sends
   both audio and text as parts of a single content object. The entity list in the text
   part demonstrably reaches the model and influences transcription output.

3. **Gemini-2.5-flash is the best current option** for hint-injected technical ASR:
   - entity_recall_exact: 0.643 (hinted) vs 0.286 (baseline) — 2.2× lift
   - WER: 0.619 (hinted) vs 0.705 (baseline) — also improves overall accuracy
   - Latency: ~4s per request (acceptable for a voice input pipeline)

4. **Gemini-2.5-pro adds cost and latency** (~10s) with diminishing returns
   (+0.286 delta vs +0.357 for flash). Flash is the pragmatic choice.

---

## Decision

**Use Gemini-2.5-flash as the primary production ASR provider.**

- Inject `known_entities` using the **few-shot format (Config C)**: a generic example
  demonstrating entity preservation, followed by the dynamic entity list
- Fall back to Whisper (OpenRouter or SiliconFlow) for latency-sensitive paths where
  entity accuracy is less critical
- Do not attempt hint injection with Whisper-family models via any hosted API

The README core pipeline reference to `gpt-4o-transcribe` is superseded by this finding.

---

## Implementation

- Go adapter: `internal/asr/gemini.go` — `TranscribeGemini(ctx, key, audioPath, apiURL, language, model)`
- Python adapter: `docs/research/model-eval/adapters/gemini_adapter.py`
- Config: `~/.config/voci/config-gemini.yaml` → `asr_provider: gemini`
- Model default: `gemini-2.5-flash` (override via `asr_model:` in config or `ASR_MODEL` env)

---

## Consequences

- **Vendor lock-in**: Gemini API is Google-specific. If Gemini pricing or availability
  changes, there is no drop-in hint-capable replacement today.
- **Cost**: Gemini-2.5-flash audio inference costs ~$0.001/request at current pricing.
  Acceptable for interactive voice input (≪ 1000 req/day in typical use).
- **Latency**: ~4s is above real-time but within acceptable range for a confirm-before-send
  pipeline where the user reviews the transcript before submission.
- **Open question**: Whether direct Gemini API (not via OpenRouter) supports entity glossary
  in the prompt for other speech models; currently only tested via `generateContent`.

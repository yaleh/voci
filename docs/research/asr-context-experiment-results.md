# ASR Context Experiment Results

## Overview

Controlled experiment testing whether adding Claude Code session context (edited files, recent commands, conversation prose) to the Gemini ASR prompt improves recognition accuracy for technical vocabulary, and quantifying the token and latency cost.

**Date:** 2026-07-01
**Audio synthesis:** edge-tts (zh-CN-XiaoxiaoNeural)
**ASR model:** gemini-2.5-flash
**Test set:** 20 clips (8 Category A, 8 Category B, 4 Category C)

## Prompt Variants

| Variant | Description |
|---------|-------------|
| V0 | Baseline: entities list only (current behavior) |
| V1 | Structured context: entities + file paths + commands (no prose) |
| V2 | Full context: entities + file paths + commands + last 3 prose turns |

## Results Summary

### Per-Category Accuracy

| Variant | Cat A (session terms) | Cat B (general) | Cat C (ambiguous) | Overall |
|---------|----------------------|-----------------|-------------------|---------|
| V0      | 0% (0/8)             | 75% (6/8)       | 75% (3/4)         | 45%     |
| V1      | 12% (1/8)            | 75% (6/8)       | 100% (4/4)        | 55%     |
| V2      | 25% (2/8)            | 88% (7/8)       | 100% (4/4)        | 65%     |

### Per-Category Tokens and Latency

| Variant | Cat A avg input tokens | Cat B avg input tokens | Cat C avg input tokens | Overall avg input | Overall avg latency |
|---------|----------------------|----------------------|----------------------|-------------------|--------------------|
| V0      | 278                  | 240                  | 166                  | 240               | 5551ms             |
| V1      | 378                  | 340                  | 322                  | 352               | 3027ms             |
| V2      | 579                  | 541                  | 523                  | 553               | 2737ms             |

Note: V0 latency is inflated by one timeout event (C02, 60s).

### Token Delta vs Baseline

| Variant | Avg input token increase | Per-call cost impact |
|---------|--------------------------|----------------------|
| V1      | +112                      | negligible           |
| V2      | +313                      | moderate             |

## Category A Detailed Analysis

Category A clips contain file paths and function names from the voci codebase that appear in the session context.

```
A01: "打开 internal/asr/gemini.go 修改 BuildCached 函数"
A02: "重构 internal/context/builder.go 的 SessionSource"
A03: "检查 internal/daemon/handlers.go 的 TranscribeFn"
A04: "更新 cmd/voci/main.go 的入口逻辑"
A05: "查看 internal/pipeline/rewrite.go 的 Rewrite 函数"
A06: "修改 internal/inject/tmux.go 的 TmuxInjector"
A07: "添加 internal/daemon/web/recorder.src.js 的测试"
A08: "修复 internal/wire/wire.go 的依赖注入"
```

### V0 Failures (all 8 clips)

Without session context, the model systematically:
- Replaces `/` path separators with spaces: `internal/asr/gemini.go` becomes `internal ASR gemini.go`
- Cannot distinguish directory names from English words: `daemon` becomes `demon`
- Loses path structure: `cmd/voci/main.go` becomes `CMD vocimain go`
- Guesses at unfamiliar terms: `TmuxInjector` becomes `max injector`

### V1 Improvements (1/8 matched)

Adding file paths and commands as structured context:
- A08 (`internal/wire/wire.go`) matched — the entity hint provides enough signal for short, clear paths.
- Some clips show improved partial recognition (e.g., `ASR Gemini go` is closer than V0's `ASR gemini.go`)
- But structured lists alone are insufficient: the model does not reliably map audio syllables back to listed file paths.

### V2 Improvements (2/8 matched)

Adding prose conversation context:
- **A01 matched**: "打开 internal/asr/gemini.go 修改 BuildCached 函数" — exact match with correct `/` separators. The prose mentions "BuildCached 在 internal/asr/gemini.go 里" which provides strong co-occurrence signal.
- **A02 matched**: "重构 internal/context/builder.go 的 SessionSource" — also exact. Prose mentions "internal/context/builder.go 的 SessionSource".
- A03 partial improvement: `daemon` instead of `demon` (prose mentions handlers.go context).
- A04 partial improvement: `CMD main` instead of `CMD vocimain go` (prose mentions main.go context).

**Key finding:** Files/functions explicitly named in session prose conversation are correctly transcribed with full path separators. Files mentioned only as entity lists or file path lists (without conversational context) are not reliably corrected.

## Category B Analysis

General technical vocabulary not related to session context:
- All variants perform similarly (75-88%). The slight V2 improvement (88%) may be noise given the small sample.
- Context does not hurt general vocabulary recognition.

## Category C Analysis

Ambiguous words (voci vs vocal, hinted vs hinting, etc.):
- V0 missed C02 ("hinted 提示修正后的结果") due to timeout, but matched the other 3.
- V1 and V2 achieved 100%. Entity hints clearly resolve voci/vocal and hinted/hinting ambiguities.

## Decision

**Adopt V1 (structured context) as default. Offer V2 as opt-in for sessions where file-path accuracy matters.**

### Rationale

1. **V1 meets the adoption criteria:** Category A accuracy improved +12pp over V0 (+10pp threshold met), with only +112 input tokens per call (well under the 300-token limit).

2. **V2 provides additional value but at notable cost:** Category A accuracy improved +25pp over V0, but at +313 input tokens per call (exceeds the 300-token threshold). The prose context specifically helps with file paths that appear in the conversation — files mentioned in prose were correctly transcribed with delimiters intact, while files only listed (not discussed) were not.

3. **The mechanism is co-occurrence, not entity lookup:** The model uses session prose to disambiguate what file paths "sound like" in context. Entity lists alone (V0) are insufficient; structured lists (V1) help slightly; but only conversational context (V2) provides enough signal for the model to confidently insert `/` and `.` delimiters in transcribed paths.

4. **No regressions:** Neither V1 nor V2 degraded Category B or C performance.

### Recommendation

- **Immediate:** Add a "## Claude Code Session" section (files + commands, no prose) to the merged pipeline prompt. This is V1 and costs 112 tokens.
- **Future work:** Explore a hybrid approach — include prose context only for the most recently edited files (top 3), which would capture the co-occurrence benefit while keeping token cost around +200. This would be V1.5.
- **Entity source quality:** Continue improving entity extraction quality, since Category A V0=0% shows the entity list alone is insufficient regardless of quality.

## Methodology Notes

- Audio synthesized via edge-tts (zh-CN-XiaoxiaoNeural). Synthetic audio may not perfectly represent real human speech, but provides consistent, repeatable ground truth.
- Exact match uses punctuation-and-whitespace-stripped lowercase comparison.
- One API call (C02 V0) timed out after 60s; this call was excluded from accuracy calculations but included in latency averages, inflating V0 Cat C latency.
- Sample size of 8 per category is small; results should be treated as directional, not conclusive.

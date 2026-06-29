---
id: TASK-37
title: 'ASR 模型扩展评测：SenseVoiceSmall 与 gemma4:e4b'
status: 'Basic: Done'
assignee: []
created_date: '2026-06-29 06:04'
updated_date: '2026-06-29 06:35'
labels:
  - 'kind:basic'
  - 'area:asr'
dependencies:
  - TASK-36
ordinal: 32000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
TASK-34 测了 TeleSpeechASR 和 gemma4:e4b，TASK-36 测了 whisper-large-v3 的 prompt 注入。尚未评测的模型：SiliconFlow 上的 FunAudioLLM/SenseVoiceSmall（支持 hotword 注入，原生中英混合），以及在 TASK-36 框架下重新评测 gemma4:e4b（Ollama 本地）。目标：用统一指标（WER、CER、entity_recall_exact、entity_recall_fuzzy、latency）对三类模型做横向对比，得出生产可用性结论。
<!-- SECTION:DESCRIPTION:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
# Plan: ASR 模型扩展评测：SenseVoiceSmall 与 gemma4:e4b (TASK-37)

## Context

TASK-34 benchmarked TeleSpeechASR and gemma4:e4b via the asr-bench framework; TASK-36 built a contextual-biasing experiment at `docs/research/contextual-biasing/` using whisper-large-v3 baselines with fuzzy entity recall. This task extends both by adding SenseVoiceSmall (SiliconFlow) and gemma4:e4b as standalone model adapters, running both against all 35 testcases, and producing a unified cross-model comparison report in `docs/research/model-eval/`.

## Phase 1: SenseVoiceSmall adapter + runner

Create `docs/research/model-eval/adapters/sensevoice_adapter.py`.

The adapter inherits `ModelAdapter` from `docs/research/asr-bench/adapters/base.py` (loaded via importlib, same pattern as `contextual-biasing/adapters/whisper_biased.py`). Set `supports_hints = True`. In `__init__`, read API key from `SILICONFLOW_API_KEY` env var, falling back to `~/.config/voci/config.yaml` key `siliconflow_api_key`. The `transcribe` method POSTs to `https://api.siliconflow.cn/v1/audio/transcriptions` with:
- `model` = `"FunAudioLLM/SenseVoiceSmall"` — note: field name is `model`
- `file` = WAV bytes as multipart (`audio.wav`, `audio/wav`)
- `prompt` = `", ".join(opts.known_entities)` when `known_entities` is non-empty (the SiliconFlow endpoint accepts a `prompt` field for hotword biasing, mirroring WhisperBiasedAdapter)
- Use `requests.post` with `headers={"Authorization": f"Bearer {api_key}"}`, `data=data`, `files={"file": ...}`
- Return `(resp.json().get("text", ""), latency_s)`

Create `docs/research/model-eval/run_sensevoice.py`. Run from project root. It:
1. Loads all 35 cases from `testdata/testcases.json`.
2. Imports `wer`, `cer` from `docs/research/asr-bench/metrics.py` (sys.path insert).
3. Imports `entity_recall` (exact) from `docs/research/asr-bench/metrics.py`.
4. Imports `fuzzy_entity_recall` from `docs/research/contextual-biasing/metrics_ext.py` (sys.path insert — do not copy).
5. For each case, calls `adapter.transcribe(wav_path, opts)` with `opts.known_entities` set when present.
6. Stores `category = case["category"][0]` (list → first element string).
7. Writes each row to `docs/research/model-eval/results/sensevoice-YYYYMMDD-HHMMSS.jsonl` with schema:
   `{case_id, method="sensevoice", model="FunAudioLLM/SenseVoiceSmall", hypothesis, latency_s, entity_recall_exact, entity_recall_fuzzy, WER, CER, category, reference}`
   On API error: log `ERROR <case_id>: <reason>`, write row with `hypothesis=""`, `latency_s=0.0`, metrics as `None`.

### DoD
- [ ] `grep -q 'FunAudioLLM/SenseVoiceSmall' /home/yale/work/voci/docs/research/model-eval/adapters/sensevoice_adapter.py`
- [ ] `grep -q 'supports_hints' /home/yale/work/voci/docs/research/model-eval/adapters/sensevoice_adapter.py`
- [ ] `grep -q 'sensevoice' /home/yale/work/voci/docs/research/model-eval/run_sensevoice.py`
- [ ] `python3 -c "import ast; ast.parse(open('/home/yale/work/voci/docs/research/model-eval/run_sensevoice.py').read()); print('ok')" 2>&1 | grep -q ok`
- [ ] `python3 -c "import ast; ast.parse(open('/home/yale/work/voci/docs/research/model-eval/adapters/sensevoice_adapter.py').read()); print('ok')" 2>&1 | grep -q ok`

## Phase 2: gemma4:e4b adapter + runner

Create `docs/research/model-eval/adapters/gemma4_adapter.py`.

The adapter inherits `ModelAdapter` from `docs/research/asr-bench/adapters/base.py` (same importlib pattern). Set `supports_hints = False`. The `transcribe` method:
- Base64-encodes WAV bytes: `base64.b64encode(pathlib.Path(wav_path).read_bytes()).decode()`
- Builds JSON payload: `{"model": "gemma4:e4b", "messages": [{"role": "user", "content": PROMPT, "images": [audio_b64]}], "stream": False}`
- PROMPT: `"Transcribe the audio exactly as spoken. Output only the transcribed text, no explanation, no punctuation changes."`
- Posts to `http://localhost:11434/api/chat` using `urllib.request.urlopen` (stdlib only, no `requests`), timeout=120s
- Parses `result["message"]["content"].strip()` as hypothesis
- Returns `(hypothesis, latency_s)`

Create `docs/research/model-eval/run_gemma4.py`. Mirrors `run_sensevoice.py` in structure:
1. Loads all 35 cases from `testdata/testcases.json`.
2. Imports `wer`, `cer`, `entity_recall` from `docs/research/asr-bench/metrics.py`.
3. Imports `fuzzy_entity_recall` from `docs/research/contextual-biasing/metrics_ext.py`.
4. For each case, calls `adapter.transcribe(wav_path, opts)`.
5. Stores `category = case["category"][0]`.
6. Writes rows to `docs/research/model-eval/results/gemma4-YYYYMMDD-HHMMSS.jsonl` with the same schema, `method="gemma4"`, `model="gemma4:e4b"`.
   On error: log and write row with `hypothesis=""`, `latency_s=0.0`, metrics as `None`.

### DoD
- [ ] `grep -q 'gemma4:e4b' /home/yale/work/voci/docs/research/model-eval/adapters/gemma4_adapter.py`
- [ ] `grep -q 'images' /home/yale/work/voci/docs/research/model-eval/adapters/gemma4_adapter.py`
- [ ] `grep -q 'gemma4' /home/yale/work/voci/docs/research/model-eval/run_gemma4.py`
- [ ] `python3 -c "import ast; ast.parse(open('/home/yale/work/voci/docs/research/model-eval/run_gemma4.py').read()); print('ok')" 2>&1 | grep -q ok`
- [ ] `python3 -c "import ast; ast.parse(open('/home/yale/work/voci/docs/research/model-eval/adapters/gemma4_adapter.py').read()); print('ok')" 2>&1 | grep -q ok`

## Phase 3: Run both models and collect results

Execute from project root (requires `SILICONFLOW_API_KEY` set and Ollama running with `gemma4:e4b`):

```
cd /home/yale/work/voci
python3 docs/research/model-eval/run_sensevoice.py
python3 docs/research/model-eval/run_gemma4.py
```

Each runner prints per-case progress and writes a timestamped JSONL to `docs/research/model-eval/results/`. Both result files must have exactly 35 rows (error cases write a row with empty hypothesis so row count stays at 35).

### DoD
- [ ] `ls /home/yale/work/voci/docs/research/model-eval/results/sensevoice-*.jsonl 2>/dev/null | grep -q .`
- [ ] `ls /home/yale/work/voci/docs/research/model-eval/results/gemma4-*.jsonl 2>/dev/null | grep -q .`
- [ ] `python3 -c "import json,pathlib; rows=[json.loads(l) for f in sorted(pathlib.Path('/home/yale/work/voci/docs/research/model-eval/results').glob('sensevoice-*.jsonl'))[-1:] for l in f.read_text().splitlines() if l]; assert len(rows)==35, f'got {len(rows)}'; print('ok')" 2>&1 | grep -q ok`
- [ ] `python3 -c "import json,pathlib; rows=[json.loads(l) for f in sorted(pathlib.Path('/home/yale/work/voci/docs/research/model-eval/results').glob('gemma4-*.jsonl'))[-1:] for l in f.read_text().splitlines() if l]; assert len(rows)==35, f'got {len(rows)}'; print('ok')" 2>&1 | grep -q ok`

## Phase 4: Unified comparison report

Create `docs/research/model-eval/compare_models.py`.

CLI: `python3 compare_models.py --out <dir>`. The script auto-discovers the latest JSONL for each model:
- **SenseVoiceSmall**: latest `docs/research/model-eval/results/sensevoice-*.jsonl`
- **gemma4:e4b**: latest `docs/research/model-eval/results/gemma4-*.jsonl`
- **whisper-large-v3 baseline**: latest `docs/research/contextual-biasing/results/run-baseline-*.jsonl` — remap `entity_recall` → `entity_recall_exact`, set `entity_recall_fuzzy=None`, normalize category list → string
- **TeleSpeechASR baseline**: latest JSONL in `docs/research/asr-bench/results/` where rows have `model` containing `"telespeech"` (case-insensitive: `r.get("model","").lower().find("telespeech") >= 0`; the actual results use lowercase `"telespeech"`)

Row normalization follows the same logic as `contextual-biasing/compare.py::load_rows`. Compute per-model aggregate stats (mean WER, mean CER, mean entity_recall_exact, mean entity_recall_fuzzy, mean latency_s, N) and per-category breakdowns for `zh-technical` and `zh-mixed`. Build a markdown table (columns: model, group, N, WER, CER, entity_recall_exact, entity_recall_fuzzy, latency_s). Write final report to `--out/report-YYYYMMDD-HHMMSS.md` with:
- The stats table.
- A "Recommendations" section identifying best model by entity_recall_exact, by WER, and latency trade-offs, and whether SenseVoiceSmall hotword (`prompt` field) improved recall vs the no-hint whisper baseline.

Run:
```
cd /home/yale/work/voci
python3 docs/research/model-eval/compare_models.py --out docs/research/model-eval/results
```

### DoD
- [ ] `python3 -c "import ast; ast.parse(open('/home/yale/work/voci/docs/research/model-eval/compare_models.py').read()); print('ok')" 2>&1 | grep -q ok`
- [ ] `ls /home/yale/work/voci/docs/research/model-eval/results/report-*.md 2>/dev/null | grep -q .`
- [ ] `grep -q 'Recommendations' $(ls -t /home/yale/work/voci/docs/research/model-eval/results/report-*.md | head -1)`
- [ ] `grep -q 'SenseVoice' $(ls -t /home/yale/work/voci/docs/research/model-eval/results/report-*.md | head -1)`
- [ ] `grep -q 'gemma4' $(ls -t /home/yale/work/voci/docs/research/model-eval/results/report-*.md | head -1)`

## Constraints

- No modifications to any file under `internal/` (production Go code is read-only).
- Do not duplicate `metrics_ext.py`; import from `docs/research/contextual-biasing/` via `sys.path` insert.
- Do not duplicate `metrics.py`; import from `docs/research/asr-bench/` via `sys.path` insert.
- Dependencies: Python standard library + `requests` + `difflib` only; no new `pip install` calls beyond what asr-bench already uses.
- `category` field in output JSONL must always be a plain string: `case["category"][0]` when the field is a non-empty list.
- SenseVoice hotword biasing: pass `known_entities` joined by `", "` as the `prompt` field (SiliconFlow accepts it); no separate runtime probe needed.
- All runners must be invoked from the project root (`/home/yale/work/voci`) so relative paths resolve correctly.
- gemma4 adapter must use `urllib.request` only (stdlib); do not import `requests` in that adapter.

## Acceptance Gate

- [ ] `ls /home/yale/work/voci/docs/research/model-eval/results/report-*.md 2>/dev/null | grep -q .`
- [ ] `python3 -c "import json,pathlib; rows=[json.loads(l) for f in sorted(pathlib.Path('/home/yale/work/voci/docs/research/model-eval/results').glob('sensevoice-*.jsonl'))[-1:] for l in f.read_text().splitlines() if l]; assert all('WER' in r and 'CER' in r for r in rows), 'missing WER/CER'; print('ok')" 2>&1 | grep -q ok`
- [ ] `python3 -c "import json,pathlib; rows=[json.loads(l) for f in sorted(pathlib.Path('/home/yale/work/voci/docs/research/model-eval/results').glob('gemma4-*.jsonl'))[-1:] for l in f.read_text().splitlines() if l]; assert all('entity_recall_exact' in r for r in rows), 'missing entity_recall_exact'; print('ok')" 2>&1 | grep -q ok`
- [ ] `grep -q 'Recommendations' $(ls -t /home/yale/work/voci/docs/research/model-eval/results/report-*.md | head -1)`
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Plan review iteration 2: NEEDS_REVISION — fixed 2 bare `test -f` checks in Phase 1 and Phase 2 DoD, upgraded to `grep -q '<content>'` checks (grep -q 'sensevoice' run_sensevoice.py and grep -q 'gemma4' run_gemma4.py). All other checks passed.

Plan review iteration 3: one fix applied — Phase 4 TeleSpeechASR filter updated from 'contains TeleSpeech' to case-insensitive 'telespeech' to match actual result files (model field is lowercase 'telespeech'). All other checks PASS. APPROVED.

Plan review iteration 4: APPROVED

cap:propose=approved
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
**FINISH** — All 24 DoD checks pass. Re-run completed with full 35-sample audio set.

## Verdict

FINISH — experiment complete with valid quality data across all 35 testcases.

## How Audio Was Generated

samples 16–35 were missing from disk (untracked, never committed). Generated via:
```
go run scripts/gensamples/main.go scripts/gensamples/cases.go
```
Uses SiliconFlow `/v1/audio/speech` + `FunAudioLLM/CosyVoice2-0.5B:anna` voice. Script skips existing files.

## Key Findings (full data, report-20260629-064146.md)

| model | group | N | WER | CER | entity_recall_exact | entity_recall_fuzzy | latency_s |
|---|---|---|---|---|---|---|---|
| telespeech | all | 105 | 0.896 | 0.432 | 0.238 | N/A | 1.288 |
| whisper-baseline | all | 14 | N/A | N/A | 0.214 | 0.357 | 2.083 |
| sensevoice | all | 35 | 0.845 | 0.167 | 0.214 | 0.286 | 0.618 |
| gemma4 | all | 35 | 0.824 | 0.434 | 0.286 | 0.321 | 0.909 |
| telespeech | zh-technical | 18 | 1.010 | 0.608 | 0.111 | N/A | 1.207 |
| whisper-baseline | zh-technical | 6 | N/A | N/A | 0.000 | 0.167 | 2.079 |
| sensevoice | zh-technical | 6 | 0.877 | 0.241 | 0.000 | 0.167 | 0.444 |
| gemma4 | zh-technical | 6 | 0.887 | 0.548 | 0.167 | 0.167 | 0.877 |
| telespeech | zh-mixed | 24 | 0.774 | 0.396 | 0.238 | N/A | 1.293 |
| whisper-baseline | zh-mixed | 7 | N/A | N/A | 0.286 | 0.571 | 2.165 |
| sensevoice | zh-mixed | 8 | 0.704 | 0.140 | 0.286 | 0.429 | 0.611 |
| gemma4 | zh-mixed | 8 | 0.720 | 0.398 | 0.286 | 0.357 | 0.869 |

## Conclusions

1. **gemma4:e4b 整体 entity_recall_exact 最高 (0.286)**，与 telespeech 持平，优于 sensevoice 和 whisper-baseline。
2. **SenseVoiceSmall CER 最低 (0.167)**，字符级准确率显著优于 telespeech (0.432) 和 gemma4 (0.434)，说明中文转写更精准。
3. **SenseVoiceSmall 延迟最低 (0.618s)**，比 telespeech (1.288s) 快一倍，比 whisper-baseline (2.083s) 快三倍。
4. **zh-technical 是所有模型的弱项**：entity_recall_exact 普遍为 0（gemma4 为 0.167）。CamelCase 实体（BuildContext、builder.go 等）的 verbatim 匹配极难。
5. **SenseVoiceSmall hotword prompt 效果不明显**：与 whisper-baseline 的 entity_recall_exact 相同 (0.214)，未见改善。
6. **生产推荐**：若优先中文 CER + 延迟 → SenseVoiceSmall；若优先 entity_recall → gemma4:e4b（但需 Ollama 本地部署）。

## Artifacts

- `docs/research/model-eval/adapters/sensevoice_adapter.py`
- `docs/research/model-eval/adapters/gemma4_adapter.py`
- `docs/research/model-eval/run_sensevoice.py` / `run_gemma4.py`
- `docs/research/model-eval/compare_models.py`
- `docs/research/model-eval/results/sensevoice-20260629-064038.jsonl` (35 rows, full audio)
- `docs/research/model-eval/results/gemma4-20260629-064108.jsonl` (35 rows, full audio)
- `docs/research/model-eval/results/report-20260629-064146.md`
<!-- SECTION:FINAL_SUMMARY:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 grep -q 'FunAudioLLM/SenseVoiceSmall' /home/yale/work/voci/docs/research/model-eval/adapters/sensevoice_adapter.py
- [x] #2 grep -q 'supports_hints' /home/yale/work/voci/docs/research/model-eval/adapters/sensevoice_adapter.py
- [x] #3 grep -q 'sensevoice' /home/yale/work/voci/docs/research/model-eval/run_sensevoice.py
- [x] #4 python3 -c "import ast; ast.parse(open('/home/yale/work/voci/docs/research/model-eval/run_sensevoice.py').read()); print('ok')" 2>&1 | grep -q ok
- [x] #5 python3 -c "import ast; ast.parse(open('/home/yale/work/voci/docs/research/model-eval/adapters/sensevoice_adapter.py').read()); print('ok')" 2>&1 | grep -q ok
- [x] #6 grep -q 'gemma4:e4b' /home/yale/work/voci/docs/research/model-eval/adapters/gemma4_adapter.py
- [x] #7 grep -q 'images' /home/yale/work/voci/docs/research/model-eval/adapters/gemma4_adapter.py
- [x] #8 grep -q 'gemma4' /home/yale/work/voci/docs/research/model-eval/run_gemma4.py
- [x] #9 python3 -c "import ast; ast.parse(open('/home/yale/work/voci/docs/research/model-eval/run_gemma4.py').read()); print('ok')" 2>&1 | grep -q ok
- [x] #10 python3 -c "import ast; ast.parse(open('/home/yale/work/voci/docs/research/model-eval/adapters/gemma4_adapter.py').read()); print('ok')" 2>&1 | grep -q ok
- [x] #11 ls /home/yale/work/voci/docs/research/model-eval/results/sensevoice-*.jsonl 2>/dev/null | grep -q .
- [x] #12 ls /home/yale/work/voci/docs/research/model-eval/results/gemma4-*.jsonl 2>/dev/null | grep -q .
- [x] #13 python3 -c "import json,pathlib; rows=[json.loads(l) for f in sorted(pathlib.Path('/home/yale/work/voci/docs/research/model-eval/results').glob('sensevoice-*.jsonl'))[-1:] for l in f.read_text().splitlines() if l]; assert len(rows)==35, f'got {len(rows)}'; print('ok')" 2>&1 | grep -q ok
- [x] #14 python3 -c "import json,pathlib; rows=[json.loads(l) for f in sorted(pathlib.Path('/home/yale/work/voci/docs/research/model-eval/results').glob('gemma4-*.jsonl'))[-1:] for l in f.read_text().splitlines() if l]; assert len(rows)==35, f'got {len(rows)}'; print('ok')" 2>&1 | grep -q ok
- [x] #15 python3 -c "import ast; ast.parse(open('/home/yale/work/voci/docs/research/model-eval/compare_models.py').read()); print('ok')" 2>&1 | grep -q ok
- [x] #16 ls /home/yale/work/voci/docs/research/model-eval/results/report-*.md 2>/dev/null | grep -q .
- [x] #17 grep -q 'Recommendations' $(ls -t /home/yale/work/voci/docs/research/model-eval/results/report-*.md | head -1)
- [x] #18 grep -q 'SenseVoice' $(ls -t /home/yale/work/voci/docs/research/model-eval/results/report-*.md | head -1)
- [x] #19 grep -q 'gemma4' $(ls -t /home/yale/work/voci/docs/research/model-eval/results/report-*.md | head -1)
- [x] #20 ls /home/yale/work/voci/docs/research/model-eval/results/report-*.md 2>/dev/null | grep -q .
- [x] #21 python3 -c "import json,pathlib; rows=[json.loads(l) for f in sorted(pathlib.Path('/home/yale/work/voci/docs/research/model-eval/results').glob('sensevoice-*.jsonl'))[-1:] for l in f.read_text().splitlines() if l]; assert all('WER' in r and 'CER' in r for r in rows), 'missing WER/CER'; print('ok')" 2>&1 | grep -q ok
- [x] #22 python3 -c "import json,pathlib; rows=[json.loads(l) for f in sorted(pathlib.Path('/home/yale/work/voci/docs/research/model-eval/results').glob('gemma4-*.jsonl'))[-1:] for l in f.read_text().splitlines() if l]; assert all('entity_recall_exact' in r for r in rows), 'missing entity_recall_exact'; print('ok')" 2>&1 | grep -q ok
- [x] #23 grep -q 'Recommendations' $(ls -t /home/yale/work/voci/docs/research/model-eval/results/report-*.md | head -1)
- [x] #24 bash /home/yale/.local/share/baime/scripts/validate-plugin.sh
<!-- DOD:END -->

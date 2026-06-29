---
id: TASK-41
title: 增加 elevenlabs 支持，并实验
status: 'Basic: Backlog'
assignee: []
created_date: '2026-06-29 10:41'
updated_date: '2026-06-29 10:52'
labels:
  - 'kind:basic'
  - 'area:asr'
  - 'area:research'
dependencies: []
ordinal: 31000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
增加 elevenlabs 支持，并实验。
<!-- SECTION:DESCRIPTION:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
# Proposal: 增加 ElevenLabs ASR 支持并实验

## Background

voci 当前最佳 ASR 方案是 Gemini-2.5-flash hinted（entity_recall_exact 0.643），但其 hint 注入依赖通用文本 prompt，
并非专用词汇偏置接口。ElevenLabs Scribe v2 提供专用 `keyterms` 字段（最多 50 条，每条 ≤ 100 字符），设计上
直接作用于解码器词汇权重，而非通过 LLM 上下文理解，架构上与 Gemini 方式根本不同。这一差异使得 Scribe v2
成为验证"专用词汇偏置 vs 多模态上下文 prompt"哪种机制对中英混合技术术语更有效的天然对照实验对象。
过去实验（TASK-34/36）已证明纯 ASR hosted API（SiliconFlow/OpenRouter Whisper）的 prompt 字段会被服务端
丢弃、无法偏置，因此本实验也为后续是否值得依赖任何外部 keyterms API 提供判断依据。

## Goals

1. 在 Go 层实现 `ElevenLabsScribeTranscribe` 函数（`internal/asr/elevenlabs.go`），遵循 gemini.go 适配器模式，
   通过 `asr_provider: elevenlabs` 配置路由，支持将 `keyterms` 字段传入 Scribe v2 API，并具备完整单元测试
   （httptest mock，≥6 个测试用例，覆盖 header、body 结构、错误路径）。
2. 实现 Python 实验适配器 `docs/research/model-eval/adapters/elevenlabs_adapter.py`，遵循 GeminiAdapter 模式，
   `supports_hints = True`，baseline 模式不传 keyterms，hinted 模式传入 `known_entities` 列表。
3. 实现运行脚本 `docs/research/model-eval/run_elevenlabs.py`，遵循 run_gemini.py 模式，支持
   `--method baseline|hinted` 参数，输出 JSONL 结果到 `results/` 目录。
4. 在相同 35 个 testcase 上运行实验，输出 entity_recall_exact / WER / CER / latency 指标，并在
   `docs/research/model-eval/results/` 生成对比报告，明确给出 Scribe v2 hinted 与 Gemini-2.5-flash hinted
   的 entity_recall_exact delta 是否超过 0.05（统计上有意义的改进门槛）。
5. 将实验结论更新至 `docs/adr/001-asr-provider-and-hint-injection.md`，说明 Scribe v2 keyterms 专用偏置接口
   是否优于、持平或劣于 Gemini 多模态上下文 prompt，并给出生产选型建议。

## Proposed Approach

**Go 适配器（Production Path）**

在 `internal/asr/elevenlabs.go` 中实现 `TranscribeElevenLabs(ctx, key, audioPath, apiURL, language, model string, keyterms []string) string`。
ElevenLabs Scribe v2 接受 multipart/form-data，字段包括 `model_id`（固定 `scribe_v2`）、`file`（音频）、
以及可重复的 `keyterms[]` 字段（每条一个值）。Auth 使用 `xi-api-key` header。
在 `internal/asr/siliconflow.go` 的 `Transcribe` 函数中增加 `provider == "elevenlabs"` 分支。
Config 层无需新字段，`ASRAPIKey` 复用，`ASRModel` 对 elevenlabs 可忽略或用作 model_id 覆盖。

**Python 实验适配器**

`elevenlabs_adapter.py` 继承 `ModelAdapter`，使用 `requests` 或标准库 `urllib` 构造 multipart 请求，
将 `opts.known_entities` 作为多个 `keyterms` 字段发送（hinted 模式）或省略（baseline 模式）。
`run_elevenlabs.py` 复用 run_gemini.py 的 metrics 导入和 JSONL 输出逻辑。

**实验设计**

运行 4 个实验单元：elevenlabs/scribe_v2/baseline、elevenlabs/scribe_v2/hinted，与已有
gemini/gemini-2.5-flash/baseline、gemini/gemini-2.5-flash/hinted 对比。对 all / zh-technical / zh-mixed
三个分组分别计算指标。

## Trade-offs and Risks

**未做的事**
- 不实现流式（实时）转录路径（$0.39/hr），本实验仅测批量 API（$0.22/hr）。
- 不修改 voci 核心会话流程或 Web UI，Go 适配器仅连通配置路由，不暴露 keyterms 到现有会话 API。
- 不覆盖非中文语种的实验（testcase 集以 zh-technical / zh-mixed 为主）。

**已知风险**
- ElevenLabs Scribe v2 API 目前文档显示 `keyterms` 为 beta 字段，行为可能与文档不符（类似 SiliconFlow 丢弃 prompt 的历史）；
  若 keyterms 被静默忽略，baseline 与 hinted 将无差异，需在报告中明确记录。
- 价格约为 Gemini-2.5-flash 的 2×，若实验未显示显著提升，生产替换无合理成本依据。
- 35 个 testcase 样本量偏小（zh-technical 仅 6 个），entity_recall_exact 的统计检验力有限；
  结论仅作为下一轮实验的先验，不作最终裁定。

**考虑过但排除的替代方案**
- 直接通过 OpenRouter 路由 ElevenLabs：OpenRouter 不支持 ElevenLabs Scribe，需直连。
- 测试 AssemblyAI Universal-2（也有 word_boost 字段）：范围外，留待后续独立 TASK。

---

# Plan: 增加 ElevenLabs ASR 支持并实验

Proposal: (inline above)

## Phase A: Go adapter — internal/asr/elevenlabs.go

### Tests (write first)

Add to `internal/asr/elevenlabs_test.go` (new file) — all must fail before implementation exists:

1. **TestTranscribeElevenLabsReturnsText** — httptest server returns `{"text":"hello"}`; assert result == `"hello"`.
2. **TestTranscribeElevenLabsSetsXiApiKeyHeader** — capture `xi-api-key` header; assert equals passed key; assert `Authorization` header is empty.
3. **TestTranscribeElevenLabsRequestIsMultipart** — capture `Content-Type`; assert `strings.HasPrefix(ct, "multipart/form-data")`.
4. **TestTranscribeElevenLabsRequestBodyHasModelId** — parse multipart; assert `model_id` field equals `"scribe_v2"`.
5. **TestTranscribeElevenLabsRequestBodyHasFileField** — parse multipart; assert `file` form file is present with non-zero bytes.
6. **TestTranscribeElevenLabsKeytermsFieldsSentWhenHinted** — pass `keyterms=[]string{"BuildContext","TASK-32"}`; parse multipart; assert two `keyterms` values present.
7. **TestTranscribeElevenLabsNoKeytermsFieldWhenEmpty** — pass `keyterms=nil`; parse multipart body as string; assert `! strings.Contains(body, "keyterms")`.
8. **TestTranscribeElevenLabsHTTPError** — server returns 422; assert result == `""`.
9. **TestTranscribeElevenLabsMissingFileReturnsEmpty** — pass `/nonexistent/audio.wav`; assert result == `""`.
10. **TestTranscribeElevenLabsKeyNotInURL** — assert API key does not appear in captured `r.URL.RawQuery`.

### Implementation

Create `internal/asr/elevenlabs.go`:
- `const DefaultElevenLabsAPIURL = "https://api.elevenlabs.io/v1/speech-to-text"`
- `const DefaultElevenLabsModel = "scribe_v2"`
- `func TranscribeElevenLabs(ctx context.Context, key, audioPath, apiURL, language, model string, keyterms []string) string`
  - multipart/form-data body: `model_id` field (default `scribe_v2`), `file` form-file, one repeated `keyterms` field per entry in `keyterms` slice
  - Auth: `xi-api-key` header only (no `Authorization` header)
  - Parse response `{"text": "..."}` using the existing `transcribeResponse` struct from `siliconflow.go`
  - Log and return `""` on all error paths (file read, HTTP, decode)

### DoD

- [ ] `go test ./internal/asr/... -run TestTranscribeElevenLabs`
- [ ] All 10 `TestTranscribeElevenLabs*` tests pass
- [ ] `go vet ./internal/asr/...`

---

## Phase B: Transcribe dispatch — extend siliconflow.go

### Tests (write first)

Add to `internal/asr/siliconflow_test.go`:

1. **TestTranscribeElevenLabsProviderRouting** — call `Transcribe(..., "elevenlabs", "")` against an httptest server that returns `{"text":"routed"}`; assert result == `"routed"`.
2. **TestTranscribeElevenLabsProviderSetsXiApiKey** — capture `xi-api-key` header via `Transcribe(..., "elevenlabs", "")` call; assert header equals supplied key.
3. **TestTranscribeElevenLabsProviderNoAuthHeader** — capture `Authorization` header via same call; assert it is `""` (not `"Bearer ..."`).

### Implementation

Modify `internal/asr/siliconflow.go` — in `Transcribe`, add branch before the `else` fallback:

```go
} else if provider == "elevenlabs" {
    return TranscribeElevenLabs(ctx, key, audioPath, apiURL, language, model, nil)
```

`keyterms` is `nil` for the Go production routing path; the Python experiment layer handles hint injection directly.

### DoD

- [ ] `go test ./internal/asr/...`
- [ ] `TestTranscribeElevenLabsProviderRouting`, `TestTranscribeElevenLabsProviderSetsXiApiKey`, `TestTranscribeElevenLabsProviderNoAuthHeader` pass
- [ ] `go vet ./internal/asr/...`

---

## Phase C: Python adapter + run script

### Tests (write first)

Shell-level smoke tests (no live API calls):

1. `python3 -c "import importlib.util, pathlib, sys; spec=importlib.util.spec_from_file_location('ea', 'docs/research/model-eval/adapters/elevenlabs_adapter.py'); m=importlib.util.module_from_spec(spec); spec.loader.exec_module(m); assert m.ElevenLabsAdapter.supports_hints is True"` — adapter class loads and declares `supports_hints = True`.
2. `python3 docs/research/model-eval/run_elevenlabs.py --help` — exits 0 and prints usage mentioning `--method`.
3. `python3 -c "import ast, pathlib; ast.parse(pathlib.Path('docs/research/model-eval/run_elevenlabs.py').read_text())"` — file is syntactically valid Python.

### Implementation

Create `docs/research/model-eval/adapters/elevenlabs_adapter.py`:
- Follow `gemini_adapter.py` import pattern (dynamic load of `base.py` from `asr-bench/adapters/`)
- Class `ElevenLabsAdapter(ModelAdapter)`, `supports_hints = True`
- `__init__`: reads `ELEVENLABS_API_KEY` env var (fallback `ASR_API_KEY`)
- `name` property: `f"elevenlabs/{self.model}"`
- `transcribe(wav_path, opts)`: constructs multipart POST to `https://api.elevenlabs.io/v1/speech-to-text` using `urllib`; `model_id=scribe_v2`; in hinted mode sends each `opts.known_entities` item as a separate `keyterms` field; returns `(text, latency)`

Create `docs/research/model-eval/run_elevenlabs.py`:
- Follow `run_gemini.py` structure exactly (imports, argparse, testcases.json loop, JSONL output)
- `--method baseline|hinted`, `--model` (default `scribe_v2`)
- Output file: `results/elevenlabs-scribe_v2-{method}-{ts}.jsonl`
- Row schema: `provider="elevenlabs"`, `model`, `method`, `hypothesis`, `latency_s`, `WER`, `CER`, `entity_recall_exact`, `entity_recall_fuzzy`, `category`, `reference`

Extend `docs/research/model-eval/compare_models.py`:
- Add `"elevenlabs"` to `NEW_PROVIDERS` list
- Add auto-discovery block for `elevenlabs-*.jsonl` files (same pattern as gemini block, using `provider`+`model`+`method` fields from rows to build key `elevenlabs/scribe_v2/{method}`)

### DoD

- [ ] `go test ./...`
- [ ] `python3 -c "import importlib.util, pathlib, sys; spec=importlib.util.spec_from_file_location('ea', 'docs/research/model-eval/adapters/elevenlabs_adapter.py'); m=importlib.util.module_from_spec(spec); spec.loader.exec_module(m); assert m.ElevenLabsAdapter.supports_hints is True"`
- [ ] `python3 docs/research/model-eval/run_elevenlabs.py --help`
- [ ] `python3 -c "import ast, pathlib; ast.parse(pathlib.Path('docs/research/model-eval/run_elevenlabs.py').read_text())"`
- [ ] `grep -q '"elevenlabs"' docs/research/model-eval/compare_models.py`

---

## Phase D: Experiment — 35 testcases × 2 methods

### Tests (write first)

Pre-run check (must pass before launching the API calls):

1. `python3 -c "import json,pathlib; cases=json.loads(pathlib.Path('testdata/testcases.json').read_text()); assert len(cases)==35, f'Expected 35, got {len(cases)}'"` — exactly 35 test cases present.

Post-run checks (checked in DoD after experiment completes):

2. Baseline JSONL exists with 35 rows.
3. Hinted JSONL exists with 35 rows.

### Implementation

Run the experiment (requires `ELEVENLABS_API_KEY` in environment):

```bash
python3 docs/research/model-eval/run_elevenlabs.py --method baseline
python3 docs/research/model-eval/run_elevenlabs.py --method hinted
```

If keyterms are silently ignored by the API (baseline == hinted results), record raw responses verbatim in results and flag in output rows.

### DoD

- [ ] `go test ./...`
- [ ] `ls docs/research/model-eval/results/elevenlabs-scribe_v2-baseline-*.jsonl | wc -l | grep -q '[1-9]'`
- [ ] `ls docs/research/model-eval/results/elevenlabs-scribe_v2-hinted-*.jsonl | wc -l | grep -q '[1-9]'`
- [ ] `wc -l docs/research/model-eval/results/elevenlabs-scribe_v2-baseline-*.jsonl | tail -1 | grep -q '35'`
- [ ] `wc -l docs/research/model-eval/results/elevenlabs-scribe_v2-hinted-*.jsonl | tail -1 | grep -q '35'`

---

## Phase E: Report + ADR update

### Tests (write first)

Pre-write assertion:

1. `grep -q '"elevenlabs"' docs/research/model-eval/compare_models.py` — compare script already extended (from Phase C).

Post-write content checks:

2. Latest report contains `elevenlabs` string.
3. ADR contains `ElevenLabs`, `TASK-41`, and `0.05` threshold reference.

### Implementation

Generate comparison report:

```bash
python3 docs/research/model-eval/compare_models.py --out docs/research/model-eval/results/
```

Update `docs/adr/001-asr-provider-and-hint-injection.md`:
- Add **TASK-41** to the `Tasks:` header line
- Add new experiment section `### TASK-41 — ElevenLabs Scribe v2 keyterms experiment` with:
  - Result table: `elevenlabs/scribe_v2` baseline vs hinted `entity_recall_exact` (all / zh-technical / zh-mixed)
  - Comparison vs `gemini-2.5-flash/hinted` (0.643); explicit statement of whether delta exceeds 0.05
  - If keyterms were silently ignored (baseline == hinted), record that outcome explicitly
- Update **Findings** section: add finding on dedicated `keyterms` API vs multimodal context prompt
- Update **Decision** section if ElevenLabs shows superior or equal performance; otherwise reaffirm Gemini as primary

### DoD

- [ ] `go test ./...`
- [ ] `ls docs/research/model-eval/results/report-*.md | tail -1 | xargs grep -q 'elevenlabs'`
- [ ] `grep -q 'ElevenLabs' docs/adr/001-asr-provider-and-hint-injection.md`
- [ ] `grep -q 'TASK-41' docs/adr/001-asr-provider-and-hint-injection.md`
- [ ] `grep -q '0.05' docs/adr/001-asr-provider-and-hint-injection.md`

---

## Constraints

- `Transcribe()` signature in `siliconflow.go` must not change — callers depend on it
- Go adapter does not expose `keyterms` through the existing session pipeline; only the config-route dispatch is wired; Python experiment layer handles hint injection independently
- No streaming (real-time) path for ElevenLabs; experiment uses batch API only
- No changes to voci core session flow or Web UI
- Keyterms per request: max 50 entries, each ≤ 100 characters (ElevenLabs Scribe v2 API limit)
- If `keyterms` field is silently dropped by ElevenLabs (baseline == hinted delta ≈ 0), that outcome must be explicitly recorded in the ADR, not silently omitted
- 35 testcase sample size is insufficient for statistical significance on zh-technical subgroup (n=6); results are treated as prior for future experiments, not final verdict
- `ASRAPIKey` config field is reused for `xi-api-key`; no new config schema fields needed
- All Python multipart requests use `urllib` (no third-party requests library), consistent with `gemini_adapter.py`

## Acceptance Gate

- [ ] `go test ./...`
- [ ] `go vet ./...`
- [ ] `grep -q 'TranscribeElevenLabs' internal/asr/elevenlabs.go`
- [ ] `grep -q 'elevenlabs' internal/asr/siliconflow.go`
- [ ] `python3 -c "import ast, pathlib; ast.parse(pathlib.Path('docs/research/model-eval/adapters/elevenlabs_adapter.py').read_text())"`
- [ ] `python3 -c "import ast, pathlib; ast.parse(pathlib.Path('docs/research/model-eval/run_elevenlabs.py').read_text())"`
- [ ] `grep -q '"elevenlabs"' docs/research/model-eval/compare_models.py`
- [ ] `grep -q 'ElevenLabs' docs/adr/001-asr-provider-and-hint-injection.md`
- [ ] `grep -q '0.05' docs/adr/001-asr-provider-and-hint-injection.md`
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Proposal self-review: APPROVED
premise-ledger:
[E] Motivation: Background是7行，解释了WHY — Gemini hint 是通用文本prompt而非专用词汇偏置，Scribe v2 keyterms 架构上不同，过去实验证明纯ASR API prompt被丢弃
[E] Goals: 5条均有可验证标准（具体函数名/文件路径/指标阈值/testcase数量）
[C] Feasibility-Go: 遵循gemini.go适配器模式，multipart请求参照siliconflow.go，provider分支在Transcribe()中已有先例，Config无需新字段
[C] Feasibility-Python: elevenlabs_adapter.py遵循GeminiAdapter模式（ModelAdapter继承、supports_hints、transcribe方法），run脚本遵循run_gemini.py
[E] Completeness: Trade-offs覆盖scope exclusions、已知风险（beta字段行为、成本、样本量）、考虑过的替代方案
[C] Consistency: Background的实验动机 → Goals的实验目标 → Approach的实验设计，三节内部一致，无矛盾
GCL-self-report: E=3 C=3 H=0

Plan review iteration 1: APPROVED

premise-ledger:
[E] Goal coverage: All 5 Goals map 1:1 to phases — Goal1→PhaseA+B, Goal2+3→PhaseC, Goal4→PhaseD, Goal5→PhaseE
[E] TDD structure: Every phase has ### Tests then ### Implementation in correct order
[E] TDD order: First DoD item in every phase starts with `go test`
[E] Acceptance gate: First item is `go test ./...`
[C] DoD executability: Natural-language items 2-3 in Phase D Tests section (not DoD section); all actual DoD items are shell commands
[C] Absence checks: No `grep -qv` found; test 7 uses `! strings.Contains` (Go code, not shell)
[C] Phase ordering: A→B→C→D→E — each phase consumes outputs of prior phases with no circular deps
[E] Scope discipline: All phases directly implement a named Goal; nothing beyond Goals 1-5
[C] File paths: Modified files verified to exist (siliconflow.go, siliconflow_test.go, compare_models.py, 001-asr-provider-and-hint-injection.md); new files correctly absent
GCL-self-report: E=4 C=5 H=0
<!-- SECTION:NOTES:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 go test ./internal/asr/... -run TestTranscribeElevenLabs
- [ ] #2 go vet ./internal/asr/...
- [ ] #3 go test ./internal/asr/...
- [ ] #4 go vet ./internal/asr/...
- [ ] #5 go test ./...
- [ ] #6 python3 -c "import importlib.util, pathlib, sys; spec=importlib.util.spec_from_file_location('ea', 'docs/research/model-eval/adapters/elevenlabs_adapter.py'); m=importlib.util.module_from_spec(spec); spec.loader.exec_module(m); assert m.ElevenLabsAdapter.supports_hints is True"
- [ ] #7 python3 docs/research/model-eval/run_elevenlabs.py --help
- [ ] #8 python3 -c "import ast, pathlib; ast.parse(pathlib.Path('docs/research/model-eval/run_elevenlabs.py').read_text())"
- [ ] #9 grep -q '"elevenlabs"' docs/research/model-eval/compare_models.py
- [ ] #10 go test ./...
- [ ] #11 ls docs/research/model-eval/results/elevenlabs-scribe_v2-baseline-*.jsonl | wc -l | grep -q '[1-9]'
- [ ] #12 ls docs/research/model-eval/results/elevenlabs-scribe_v2-hinted-*.jsonl | wc -l | grep -q '[1-9]'
- [ ] #13 wc -l docs/research/model-eval/results/elevenlabs-scribe_v2-baseline-*.jsonl | tail -1 | grep -q '35'
- [ ] #14 wc -l docs/research/model-eval/results/elevenlabs-scribe_v2-hinted-*.jsonl | tail -1 | grep -q '35'
- [ ] #15 go test ./...
- [ ] #16 ls docs/research/model-eval/results/report-*.md | tail -1 | xargs grep -q 'elevenlabs'
- [ ] #17 grep -q 'ElevenLabs' docs/adr/001-asr-provider-and-hint-injection.md
- [ ] #18 grep -q 'TASK-41' docs/adr/001-asr-provider-and-hint-injection.md
- [ ] #19 grep -q '0.05' docs/adr/001-asr-provider-and-hint-injection.md
- [ ] #20 go test ./...
- [ ] #21 go vet ./...
- [ ] #22 grep -q 'TranscribeElevenLabs' internal/asr/elevenlabs.go
- [ ] #23 grep -q 'elevenlabs' internal/asr/siliconflow.go
- [ ] #24 python3 -c "import ast, pathlib; ast.parse(pathlib.Path('docs/research/model-eval/adapters/elevenlabs_adapter.py').read_text())"
- [ ] #25 python3 -c "import ast, pathlib; ast.parse(pathlib.Path('docs/research/model-eval/run_elevenlabs.py').read_text())"
- [ ] #26 grep -q '"elevenlabs"' docs/research/model-eval/compare_models.py
- [ ] #27 grep -q 'ElevenLabs' docs/adr/001-asr-provider-and-hint-injection.md
- [ ] #28 grep -q '0.05' docs/adr/001-asr-provider-and-hint-injection.md
<!-- DOD:END -->

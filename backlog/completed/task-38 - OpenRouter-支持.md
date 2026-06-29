---
id: TASK-38
title: OpenRouter 支持
status: 'Basic: Done'
assignee: []
created_date: '2026-06-29 06:34'
updated_date: '2026-06-29 07:35'
labels:
  - 'kind:basic'
  - 'area:asr'
  - 'area:config'
dependencies: []
ordinal: 33000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
OpenRouter 支持：允许用户通过配置切换 ASR 服务商（OpenRouter、SiliconFlow 或其他兼容 OpenAI 音频 API 的提供商），并在 ASR 层支持 JSON+base64 请求格式（OpenRouter 风格）和 multipart 请求格式（SiliconFlow/OpenAI 风格）。
<!-- SECTION:DESCRIPTION:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
# Proposal: OpenRouter 支持

## Background

voci 当前的 ASR 层硬编码为 SiliconFlow multipart/form-data 接口，API key 在 Config 中以 `SiliconFlowKey` 命名，`LoadConfig` 在 key 缺失时直接 fatal。这一设计使用户无法切换到 OpenRouter 等其他供应商，而 OpenRouter 提供 alibaba/qwen3-asr-flash、microsoft/mai-transcribe-1.5、openai/whisper-large-v3-turbo 等模型，覆盖 SiliconFlow 不支持的语言和精度需求。此外，OpenRouter 使用 JSON+base64 请求体，与 SiliconFlow/OpenAI 的 multipart/form-data 格式不兼容，需要在发送层做格式分支。目前 `apiURL` 参数在所有 main.go 调用点均传空字符串，说明供应商切换能力完全缺失，是一个已知的硬约束。允许用户通过配置文件或环境变量指定供应商、endpoint、API key 和模型，能让 voci 在不同网络环境和预算约束下保持可用。

## Goals

1. 用户可在 `~/.config/voci/config.yaml` 或对应环境变量中设置 `asr_provider`（枚举：`siliconflow`、`openrouter`、`openai`），voci 启动时读取并据此选择请求格式与默认 endpoint，验证方式：修改配置后 `voci once --file audio.wav` 能成功返回转录文本。
2. 用户可设置 `asr_api_key`（通用 key 字段）和可选的 `asr_api_url`（覆盖默认 endpoint），旧配置（仅含 `siliconflow_api_key`/`SILICONFLOW_API_KEY`）在不做任何改动的情况下继续正常工作，验证方式：现有测试与旧配置不报错，`SiliconFlowKey` 回退逻辑有单元测试覆盖。
3. 用户可设置 `asr_model` 字段覆盖供应商默认模型，验证方式：设置 `asr_model: alibaba/qwen3-asr-flash` 后请求体中 `model` 字段与配置值一致（通过 HTTP mock 或请求捕获验证）。
4. `internal/asr` 包新增支持 JSON+base64 请求格式的代码路径，SiliconFlow/OpenAI 格式保持 multipart/form-data 不变，验证方式：两种格式各有单元测试通过。

## Proposed Approach

**Config 层**：在 `Config` 结构体中新增 `ASRProvider`、`ASRAPIKey`、`ASRAPIURL`、`ASRModel` 字段；`LoadConfig` 保持向后兼容——若 `ASRAPIKey` 为空则回退读取 `SiliconFlowKey`，若 `ASRProvider` 为空则默认 `siliconflow`；fatal 条件改为"有效 key 不存在"而非硬绑定 `SiliconFlowKey`。对应环境变量：`ASR_PROVIDER`、`ASR_API_KEY`、`ASR_API_URL`、`ASR_MODEL`。

**ASR 层**：在 `internal/asr` 包中将请求构造逻辑拆分为两个内部函数——`buildMultipartRequest`（现有逻辑）和 `buildJSONRequest`（OpenRouter base64 格式）；顶层 `Transcribe` 函数根据 provider 参数选择对应构造函数，保持 `func(ctx, key, audioPath, apiURL, language string) string` 签名不变。OpenRouter 的默认 endpoint 和默认模型映射以常量定义在 `asr` 包内。Provider 参数通过在 main.go/server 中将 `Transcribe` 包装为闭包注入，无需修改 `TranscribeFn` 类型。

**main.go / server 层**：将分散在多处的 `cfg.SiliconFlowKey` 引用统一改为读取新的通用 key 字段；`TranscribeFn` 调用点通过闭包捕获 `cfg.ASRProvider` 和 `cfg.ASRModel`，传入 `Transcribe` 内部，而非继续传空 `apiURL`，消除 `apiURL=""` 硬编码。

## Trade-offs and Risks

**不做的事**：不支持运行时动态切换供应商（需重启）；不实现供应商健康检查或自动 fallback；不暴露 `--asr-provider` CLI flag（配置文件/env 已足够）；不迁移 `SILICONFLOW_API_KEY` 环境变量名称（保持向后兼容）。

**已知风险**：OpenRouter JSON+base64 请求体对长音频会显著增大 payload（约 33%），在弱网或大文件场景下可能超时；`TranscribeFn` 签名不变意味着 provider 信息通过闭包捕获而非显式参数传递，增加了调用方理解负担；`asr_provider` 枚举扩展到新供应商时需同步更新文档和配置校验逻辑。

---

# Plan: OpenRouter 支持

Proposal: docs/proposals/proposal-openrouter-support.md

## Phase A: Config 泛化

### Tests (write first)

File: `internal/config/config_test.go`

New test cases (must **fail** before implementation):

- `TestLoadConfigASRProviderFromEnv` — set `ASR_PROVIDER=openrouter`, expect `cfg.ASRProvider == "openrouter"`
- `TestLoadConfigASRAPIKeyFromEnv` — set `ASR_API_KEY=sk-or-test`, expect `cfg.ASRAPIKey == "sk-or-test"`
- `TestLoadConfigASRAPIURLFromEnv` — set `ASR_API_URL=https://custom.example/v1`, expect `cfg.ASRAPIURL == "https://custom.example/v1"`
- `TestLoadConfigASRModelFromEnv` — set `ASR_MODEL=alibaba/qwen3-asr-flash`, expect `cfg.ASRModel == "alibaba/qwen3-asr-flash"`
- `TestLoadConfigASRAPIKeyFallsBackToSiliconFlowKey` — unset `ASR_API_KEY`, set `SILICONFLOW_API_KEY=sk-sf`, expect `cfg.ASRAPIKey == "sk-sf"` (backward compat)
- `TestLoadConfigASRProviderDefaultsSiliconflow` — unset `ASR_PROVIDER`, expect `cfg.ASRProvider == "siliconflow"`
- `TestLoadConfigMissingKeyNewFields` — unset both `ASR_API_KEY` and `SILICONFLOW_API_KEY`, expect non-nil error
- `TestLoadConfigASRFieldsFromFile` — write yaml with `asr_provider`, `asr_api_key`, `asr_api_url`, `asr_model` fields, unset env, expect struct fields populated

### Implementation

File: `internal/config/config.go`

Changes:
1. Add fields to `Config`: `ASRProvider string`, `ASRAPIKey string`, `ASRAPIURL string`, `ASRModel string`
2. Add fields to `fileConfig`: `ASRProvider string \`yaml:"asr_provider"\``, `ASRAPIKey string \`yaml:"asr_api_key"\``, `ASRAPIURL string \`yaml:"asr_api_url"\``, `ASRModel string \`yaml:"asr_model"\``
3. In `LoadConfig`: read `ASR_PROVIDER`, `ASR_API_KEY`, `ASR_API_URL`, `ASR_MODEL` env vars; fall back to file fields
4. Backward-compat fallback: if `cfg.ASRAPIKey == ""` after env+file, set it from `cfg.SiliconFlowKey`
5. Default `ASRProvider` to `"siliconflow"` when empty
6. Change fatal condition: `if cfg.ASRAPIKey == ""` (not `if cfg.SiliconFlowKey == ""`)
7. Keep existing `SiliconFlowKey` population logic unchanged (existing tests must still pass)

### DoD
- [ ] `go test ./...`
- [ ] `grep -q 'ASRProvider' /home/yale/work/voci/internal/config/config.go`
- [ ] `grep -q 'ASRAPIKey' /home/yale/work/voci/internal/config/config.go`
- [ ] `grep -q 'asr_provider' /home/yale/work/voci/internal/config/config.go`
- [ ] `grep -q 'siliconflow' /home/yale/work/voci/internal/config/config.go`

---

## Phase B: ASR 层双格式支持

### Tests (write first)

File: `internal/asr/siliconflow_test.go`

New test cases (must **fail** before implementation):

- `TestTranscribeOpenRouterSendsJSONBase64` — provider=`"openrouter"`, assert `Content-Type: application/json`, body contains `"audio"` key with base64 value, body contains `"model"` key
- `TestTranscribeOpenRouterUsesDefaultModel` — provider=`"openrouter"`, model=`""`, assert captured `"model"` field equals `DefaultOpenRouterModel`
- `TestTranscribeOpenRouterUsesCustomModel` — provider=`"openrouter"`, model=`"microsoft/mai-transcribe-1.5"`, assert captured `"model"` == `"microsoft/mai-transcribe-1.5"`
- `TestTranscribeOpenRouterDefaultEndpoint` — provider=`"openrouter"`, apiURL=`""`, assert request URL host equals `openrouter.ai` (override default via httptest by passing srv.URL directly)
- `TestTranscribeSiliconflowStillMultipart` — provider=`"siliconflow"`, assert `Content-Type` starts with `"multipart/form-data"` (regression guard)
- `TestTranscribeModelOverrideForSiliconflow` — provider=`"siliconflow"`, model=`"my-custom-model"`, assert multipart body contains `"my-custom-model"`

### Implementation

File: `internal/asr/siliconflow.go`

Changes:
1. Add constants: `DefaultOpenRouterAPIURL = "https://openrouter.ai/api/v1/audio/transcriptions"`, `DefaultOpenRouterModel = "openai/whisper-large-v3-turbo"`
2. Add internal function `buildMultipartRequest(ctx, key, audioPath, apiURL, model string) (*http.Request, error)` — extract existing multipart logic, accept explicit `model` param
3. Add internal function `buildJSONRequest(ctx, key, audioPath, apiURL, model string) (*http.Request, error)` — read audio file, base64-encode, build JSON body `{"model": model, "audio": "<base64>"}`, set `Content-Type: application/json`, `Authorization: Bearer key`
4. Change `Transcribe` signature to: `func Transcribe(ctx context.Context, key, audioPath, apiURL, language, provider, model string) string`
   - When `provider == "openrouter"`: if `apiURL == ""` use `DefaultOpenRouterAPIURL`; if `model == ""` use `DefaultOpenRouterModel`; call `buildJSONRequest`
   - Otherwise (siliconflow / default): existing model-selection logic (languageModel map), unless `model != ""` override; call `buildMultipartRequest`
5. Keep `DefaultAPIURL` constant unchanged for backward compat

**Note on TranscribeFn signature**: `daemon.TranscribeFn` and `main.go` both define `TranscribeFn` as `func(ctx, key, audioPath, apiURL, language string) string`. Phase C wraps `asr.Transcribe` in a closure that captures provider and model — the signature exposed to daemon/mcp does NOT change. Phase B only changes the internal `asr.Transcribe` function signature. Existing test call sites in `siliconflow_test.go` must be updated to pass `provider="siliconflow"`, `model=""`.

### DoD
- [ ] `go test ./...`
- [ ] `grep -q 'buildJSONRequest' /home/yale/work/voci/internal/asr/siliconflow.go`
- [ ] `grep -q 'buildMultipartRequest' /home/yale/work/voci/internal/asr/siliconflow.go`
- [ ] `grep -q 'DefaultOpenRouterAPIURL' /home/yale/work/voci/internal/asr/siliconflow.go`

---

## Phase C: main.go / server 接线

### Tests (write first)

File: `cmd/voci/main_test.go` — add test cases:

- `TestRunOnceSiliconflowKeyBackwardCompat` — set only `SILICONFLOW_API_KEY=sk-old`, call `run` with `--file` + stub transcribeFn that records key arg, assert stub receives `"sk-old"` and no error returned
- `TestRunOnceASRAPIKeyPreferred` — set `ASR_API_KEY=sk-new` and `SILICONFLOW_API_KEY=sk-old`, call `run` with `--file` + stub, assert stub receives `"sk-new"`
- `TestRunServeAPIKeyUsesASRAPIKey` — set `ASR_API_KEY=sk-or`, inject `startServeFn` that captures its `daemon.Server.APIKey`, assert value is `"sk-or"`

File: `internal/daemon/server_test.go` — add:

- `TestServerHandleTranscribePassesAPIKey` — create `Server{APIKey: "sk-test", TranscribeFn: stub}`, POST to `/api/voice/transcribe`, assert stub receives `"sk-test"` as key arg (verifies field wiring unchanged)

### Implementation

Files to modify:

**`cmd/voci/main.go`**:
1. In all **four** `transcribeFn == nil` fallback sites (serve, daemon, integrated/mcp, once — verified by `grep -n 'transcribeFn = asr.Transcribe' cmd/voci/main.go` returning lines 174, 249, 309, 368), replace bare `transcribeFn = asr.Transcribe` with a closure capturing `cfg.ASRProvider` and `cfg.ASRModel`:
   ```go
   transcribeFn = func(ctx context.Context, key, audioPath, apiURL, language string) string {
       return asr.Transcribe(ctx, key, audioPath, apiURL, language, cfg.ASRProvider, cfg.ASRModel)
   }
   ```
   Note: the `--session=integrated` (mcp) path at main.go line 309 is the fourth site and must not be missed — it calls `asr.Transcribe` directly inside a `startMCPServerFn` closure built inline.
2. Replace `APIKey: cfg.SiliconFlowKey` with `APIKey: cfg.ASRAPIKey` in both `daemon.Server` struct literals (serve path and daemon path)
3. Replace `cfg.SiliconFlowKey` passed to `mcp.NewServer(...)` with `cfg.ASRAPIKey`
4. Replace `transcribeFn(ctx, cfg.SiliconFlowKey, ...)` in the `once` path with `transcribeFn(ctx, cfg.ASRAPIKey, ...)`

**`internal/daemon/server.go`**:
- No changes required; `APIKey` field and `handleTranscribe` wiring remain as-is

### DoD
- [ ] `go test ./...`
- [ ] `! grep -q 'cfg\.SiliconFlowKey' /home/yale/work/voci/cmd/voci/main.go`
- [ ] `grep -q 'cfg\.ASRAPIKey' /home/yale/work/voci/cmd/voci/main.go`
- [ ] `grep -q 'ASRProvider' /home/yale/work/voci/cmd/voci/main.go`

---

## Constraints

- `SiliconFlowKey` field on `Config` struct is NOT removed; existing consumers compile without change
- `daemon.TranscribeFn` and `main.TranscribeFn` type signatures are NOT changed (`func(ctx, key, audioPath, apiURL, language string) string`)
- OpenRouter JSON+base64 payload field names (`model`, `audio`) must match OpenRouter API spec — verify before implementing `buildJSONRequest`
- `ASR_PROVIDER` accepts `"siliconflow"` (default), `"openrouter"`, `"openai"` — unknown values fall through to siliconflow multipart behavior without fatal
- Phase B updating existing test call sites in `siliconflow_test.go` is required to pass new `provider` and `model` args; this is part of Phase B implementation (not a separate phase)

## Acceptance Gate
- [ ] `go test ./...`
- [ ] `! grep -q 'cfg\.SiliconFlowKey' /home/yale/work/voci/cmd/voci/main.go`
- [ ] `grep -q 'ASRProvider' /home/yale/work/voci/internal/config/config.go`
- [ ] `grep -q 'buildJSONRequest' /home/yale/work/voci/internal/asr/siliconflow.go`
- [ ] `grep -q 'DefaultOpenRouterAPIURL' /home/yale/work/voci/internal/asr/siliconflow.go`
- [ ] `grep -q 'cfg\.ASRAPIKey' /home/yale/work/voci/cmd/voci/main.go`
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Proposal self-review: APPROVED
premise-ledger:
[E] background lines: counted from proposal file
[C] goal verifiability: checked each goal wording
[H] feasibility basis: architecture knowledge
GCL-self-report: E=1 C=2 H=1

Proposal approved. Starting plan draft.

Plan review iteration 1: NEEDS_REVISION
Finding: Phase C item 1 said "all three transcribeFn == nil fallback sites (serve, daemon, once)" but main.go has FOUR such sites. The --session=integrated (mcp) path at line 309 also contains `transcribeFn = asr.Transcribe` and would fail to compile after Phase B changes the asr.Transcribe signature. Fixed: item 1 now explicitly lists all four sites (lines 174, 249, 309, 368) with a note calling out the integrated/mcp path.
premise-ledger:
[E] goal coverage: goals mapped to phases
[C] file paths exist: verified via grep
[C] four call sites: verified by reading main.go
[H] DoD sufficiency: background knowledge
GCL-self-report: E=1 C=3 H=1

Plan review iteration 2: APPROVED
premise-ledger:
[E] goal coverage: goals mapped to phases
[C] file paths exist: verified via search
[H] DoD sufficiency: background knowledge
GCL-self-report: E=2 C=3 H=1

cap:propose=approved
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
## TASK-38: OpenRouter 支持 — FINISH

### What was done

**Phase A — Config 泛化** (`internal/config/config.go`):
- Added `ASRProvider`, `ASRAPIKey`, `ASRAPIURL`, `ASRModel` fields to `Config` and `fileConfig`
- Env vars: `ASR_PROVIDER`, `ASR_API_KEY`, `ASR_API_URL`, `ASR_MODEL`
- Backward compat: `ASRAPIKey` falls back to `SiliconFlowKey` when empty
- Default `ASRProvider = "siliconflow"`; fatal only when `ASRAPIKey == ""`
- 8 new tests added (env, file, fallback, defaults)

**Phase B — ASR 双格式** (`internal/asr/siliconflow.go`):
- Extracted `buildMultipartRequest` (existing multipart/form-data logic)
- Added `buildJSONRequest` (JSON+base64 for OpenRouter; `{"model":..., "audio":...}`)
- Added `DefaultOpenRouterAPIURL` and `DefaultOpenRouterModel` constants
- Extended `Transcribe` signature with `provider, model string` params
- 6 new tests; 5 existing tests updated to pass `provider="siliconflow", model=""`

**Phase C — main.go 接线** (`cmd/voci/main.go`):
- All 4 `transcribeFn = asr.Transcribe` sites replaced with closures capturing `cfg.ASRProvider` and `cfg.ASRModel`
- All `cfg.SiliconFlowKey` → `cfg.ASRAPIKey` (3 APIKey fields + 1 transcribeFn call)

### Results
- `go test ./...` — all pass
- All 20 DoD grep/build checks pass
- `validate-plugin.sh` — ALL CHECKS PASSED (0 errors)
<!-- SECTION:FINAL_SUMMARY:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 go test ./...
- [x] #2 grep -q 'ASRProvider' /home/yale/work/voci/internal/config/config.go
- [x] #3 grep -q 'ASRAPIKey' /home/yale/work/voci/internal/config/config.go
- [x] #4 grep -q 'asr_provider' /home/yale/work/voci/internal/config/config.go
- [x] #5 grep -q 'siliconflow' /home/yale/work/voci/internal/config/config.go
- [x] #6 go test ./...
- [x] #7 grep -q 'buildJSONRequest' /home/yale/work/voci/internal/asr/siliconflow.go
- [x] #8 grep -q 'buildMultipartRequest' /home/yale/work/voci/internal/asr/siliconflow.go
- [x] #9 grep -q 'DefaultOpenRouterAPIURL' /home/yale/work/voci/internal/asr/siliconflow.go
- [x] #10 go test ./...
- [x] #11 ! grep -q 'cfg\.SiliconFlowKey' /home/yale/work/voci/cmd/voci/main.go
- [x] #12 grep -q 'cfg\.ASRAPIKey' /home/yale/work/voci/cmd/voci/main.go
- [x] #13 grep -q 'ASRProvider' /home/yale/work/voci/cmd/voci/main.go
- [x] #14 go test ./...
- [x] #15 ! grep -q 'cfg\.SiliconFlowKey' /home/yale/work/voci/cmd/voci/main.go
- [x] #16 grep -q 'ASRProvider' /home/yale/work/voci/internal/config/config.go
- [x] #17 grep -q 'buildJSONRequest' /home/yale/work/voci/internal/asr/siliconflow.go
- [x] #18 grep -q 'DefaultOpenRouterAPIURL' /home/yale/work/voci/internal/asr/siliconflow.go
- [x] #19 grep -q 'cfg\.ASRAPIKey' /home/yale/work/voci/cmd/voci/main.go
- [x] #20 bash /home/yale/.local/share/baime/scripts/validate-plugin.sh
<!-- DOD:END -->

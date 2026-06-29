---
id: TASK-33
title: ASR 语言与模型配置：为 Transcribe 接口增加语言参数
status: 'Basic: Done'
assignee: []
created_date: '2026-06-29 02:40'
updated_date: '2026-06-29 03:08'
labels:
  - 'kind:basic'
  - 'area:asr'
dependencies: []
ordinal: 28000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
为 internal/asr 层增加语言配置参数，支持按语言选择 ASR 模型，为多语种用户和将来接入 Whisper 等模型留扩展点
<!-- SECTION:DESCRIPTION:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
# Proposal: ASR 语言与模型配置：为 Transcribe 接口增加语言参数

## Background

voci 目前在 `internal/asr/siliconflow.go` 中将 ASR 模型硬编码为 `TeleAI/TeleSpeechASR`，该模型针对中文优化，对非中文语音的识别效果较差。`asr.Transcribe()` 函数签名为 `(ctx, key, audioPath, apiURL) → string`，不携带任何语言信息。`internal/config/Config` 结构体也只有 `SiliconFlowKey` 和 `OllamaHost` 两个字段，缺少语言配置。结果是：用英语（或其他语言）说话时，系统仍然将音频送往中文 ASR 模型，无法产出可用的转录文本，整条 pipeline 的所有后续阶段（hinted correction、rewrite、classify）也因此全部失效。此外，downstream 的 TASK-32（动态 Known Entities 提取）也需要语言信息来切换提取规则（中文模式：提取英文字母序列；英文模式：提取 camelCase/路径 token）。

## Goals

1. `config.Config` 增加 `Language string` 字段（默认 `"zh"`），支持从环境变量 `VOCI_LANGUAGE` 和 config.yaml `language:` 字段读取，env 优先于文件，文件优先于硬编码默认值；可通过 unit test 验证三种优先级来源均能正确填充字段。
2. `asr.Transcribe()` 函数签名扩展为 `(ctx, key, audioPath, apiURL, language string) → string`，内部按语言选择 ASR 模型名（`"zh"` → `TeleAI/TeleSpeechASR`，其他 → `openai/whisper-large-v3`），可通过注入 httptest 服务器验证请求中 model 字段随 language 正确切换。
3. `daemon.TranscribeFn`、`mcp.TranscribeFn` 及 `cmd/voci/main.go` 中的 `TranscribeFn` 类型别名同步更新为五参数签名；所有四条调用路径（`once`、`serve`、`daemon`、`mcp`）均传入 `cfg.Language`，编译器静态验证无遗漏。
4. `daemon.Server` 和 `mcp.Server` 各自持有 `Language string` 字段，在 `cmd/voci/main.go` 构造时从 `cfg.Language` 注入，handleTranscribe 调用 `TranscribeFn` 时传入该值，不修改任何 HTTP/MCP 协议字段。
5. 所有现有单元测试在签名变更后仍能通过；为 `language="en"` 分支（选择 Whisper 模型）新增至少一个单元测试。

## Proposed Approach

**Config 层**：在 `fileConfig` 中添加 `Language string \`yaml:"language"\``，在 `Config` 中添加 `Language string`。`LoadConfig` 按 env `VOCI_LANGUAGE` → yaml `language:` → default `"zh"` 顺序填充。暂不引入 `ASRModel` 覆盖字段，保持最小改动；若将来需要完全绕过语言→模型映射，可在后续 task 中添加。

**ASR 层**：`asr.Transcribe` 在参数列表末尾追加 `language string`，内部用一个小型 map（或 switch）将语言码映射到模型名。映射关系定义在包级私有变量，便于后续扩展。

**Wiring 层**：`daemon.Server` 和 `mcp.Server` 各增加 `Language string` 字段。`cmd/voci/main.go` 在 `--serve`、`--daemon`、`--session=integrated` 三条路径构造 Server 时，以及 `once` 路径直接调用 `transcribeFn` 时，统一从 `cfg.Language` 读取并传入。函数类型别名 `daemon.TranscribeFn`、`mcp.TranscribeFn`、`main.TranscribeFn` 同步更新为五参数形式。

**不实现**：不在本 task 中验证 Whisper 的真实端到端 API 调用；不新增 CLI flag `--language`；不修改 HTTP/MCP 协议层（浏览器端无需感知语言）；不引入 `ASRModel` 配置覆盖字段（留待后续需求）。

## Trade-offs and Risks

- **签名破坏性变更**：`asr.Transcribe` 增加参数会导致所有调用站点必须同步更新，漏改一处即编译失败。风险可控——调用站点有限（`once`、`serve`/`daemon`、`mcp` 三处），Go 编译器会静态检测所有遗漏。
- **语言→模型映射硬编码**：映射表写在 `asr` 包内，若 SiliconFlow 调整模型名需修改源码。可在后续 task 中通过 `ASRModel` 配置项允许用户覆盖，当前阶段不引入以保持简洁。
- **不做 Whisper 真实验证**：本 task 只保证接口兼容性，无法在 CI 中验证 Whisper 路径的实际 API 行为，存在运行时发现 API 差异的风险（字段名、响应格式等）。可通过 `asr_test.go` 中的 httptest mock 部分覆盖，但非完整 e2e。
- **语言配置作用域**：Language 是进程级配置，不支持单次请求级别的语言切换。对于偶尔需要在同一 voci 实例中处理多语言的场景，此方案无法满足。当前使用模式（单用户单语言）下不构成问题，需在 task notes 中记录为已知限制。
- **TASK-32 的依赖**：TASK-32（Known Entities 提取规则）可通过读取 `config.Language` 实现语言感知，本 task 完成后 TASK-32 可直接消费，无需额外接口变更。

---

# Plan: ASR 语言与模型配置：为 Transcribe 接口增加语言参数

Proposal: docs/proposals/proposal-asr-language-config.md

## Phase A: Config 层增加 Language 字段并实现三级优先级读取

### Tests (write first)

File: `internal/config/config_test.go`

新增三个测试函数，追加到现有文件末尾（所有函数在实现前必须 FAIL）：

- `TestLoadConfigLanguageFromEnv` — `t.Setenv("VOCI_LANGUAGE", "en")`，调用 `LoadConfig()`，断言 `cfg.Language == "en"`
- `TestLoadConfigLanguageFromFile` — 清空 `VOCI_LANGUAGE` env，yaml 写入 `language: fr`，设置 `HOME` 到 tmpDir，断言 `cfg.Language == "fr"`
- `TestLoadConfigLanguageDefault` — 清空 `VOCI_LANGUAGE` env 且 yaml 无 `language:` 字段，断言 `cfg.Language == "zh"`

### Implementation

File: `internal/config/config.go`

- `fileConfig` 追加字段：`Language string \`yaml:"language"\``
- `Config` 追加字段：`Language string`
- `LoadConfig` 在 `OllamaHost` 默认值逻辑之后追加：
  - 读取 `VOCI_LANGUAGE` env → 优先赋值
  - env 为空且 `fc.Language != ""` → 使用 yaml 值
  - 仍为空 → 赋默认值 `"zh"`

约 8 行新增，0 行删除。

### DoD
- [ ] `go test ./...`
- [ ] `go test ./internal/config/ -run TestLoadConfigLanguage -v`
- [ ] `grep -q 'Language string' /home/yale/work/voci/internal/config/config.go`
- [ ] `grep -q 'VOCI_LANGUAGE' /home/yale/work/voci/internal/config/config.go`

---

## Phase B: ASR 层签名扩展 + 语言→模型映射

### Tests (write first)

File: `internal/asr/siliconflow_test.go`

现有三个测试函数中的 `Transcribe(...)` 调用均需追加 `language` 参数（`"zh"`），并新增两个测试函数（实现前必须 FAIL 或编译失败）：

- `TestTranscribeZhUsesTelespeech` — `language="zh"`，httptest 服务器捕获 request body，断言 body 包含 `TeleAI/TeleSpeechASR`
- `TestTranscribeEnUsesWhisper` — `language="en"`，断言 body 包含 `openai/whisper-large-v3`，不包含 `TeleAI/TeleSpeechASR`

### Implementation

File: `internal/asr/siliconflow.go`

- `Transcribe` 参数列表末尾追加 `language string`
- 包级私有映射：
  ```go
  var languageModel = map[string]string{
      "zh": "TeleAI/TeleSpeechASR",
  }
  const defaultModel = "openai/whisper-large-v3"
  ```
- 函数内用映射替换硬编码模型名：若 `languageModel[language]` 存在则使用，否则用 `defaultModel`
- `w.WriteField("model", model)` 替换原硬编码行

约 8 行修改，5 行新增。

### DoD
- [ ] `go test ./...`
- [ ] `go test ./internal/asr/ -run TestTranscribeEnUsesWhisper -v`
- [ ] `go test ./internal/asr/ -run TestTranscribeZhUsesTelespeech -v`
- [ ] `grep -q 'language string' /home/yale/work/voci/internal/asr/siliconflow.go`
- [ ] `grep -q 'languageModel' /home/yale/work/voci/internal/asr/siliconflow.go`
- [ ] `! grep -q '"TeleAI/TeleSpeechASR"' /home/yale/work/voci/internal/asr/siliconflow.go`

---

## Phase C: Wiring 层签名同步 + Language 字段注入

### Tests (write first)

Files: `internal/daemon/server_test.go`、`internal/mcp/server_test.go`

Phase B 完成后，现有测试中所有 `TranscribeFn` stub 须同步更新为五参数形式（否则编译失败）。额外新增：

- `internal/daemon/server_test.go` 追加 `TestHandleTranscribePassesLanguage` — stub `TranscribeFn` 捕获传入的 `language` 参数，`srv.Language = "en"`，向 `/api/voice/transcribe` POST 请求，断言捕获值 `== "en"`
- `internal/mcp/server_test.go` 追加 `TestToolsCallPassesLanguage` — 同上逻辑验证 MCP `toolsCall` 路径，`srv.Language = "en"`，断言 stub 捕获的 `language == "en"`

### Implementation

三个文件同步修改：

**`internal/daemon/server.go`**
- `TranscribeFn` 类型：末尾追加 `language string` 参数
- `Server` struct 追加 `Language string` 字段
- `handleTranscribe` 中调用改为 `s.TranscribeFn(ctx, s.APIKey, tmpFile.Name(), "", s.Language)`

**`internal/mcp/server.go`**
- `TranscribeFn` 类型：同上
- `Server` struct 追加 `Language string` 字段
- `NewServer` 签名末尾追加 `language string`，struct 初始化追加 `Language: language`
- `toolsCall` 中调用改为 `s.TranscribeFn(ctx, s.APIKey, audioPath, "", s.Language)`

**`cmd/voci/main.go`**
- `TranscribeFn` 类型：同上
- `--serve` 路径 `daemon.Server` 字面量追加 `Language: cfg.Language`
- `--daemon` 路径 `daemon.Server` 字面量追加 `Language: cfg.Language`
- `--session=integrated` 路径 `mcp.NewServer(...)` 末尾追加 `cfg.Language`
- `once` 路径 Stage 2 调用改为 `transcribeFn(ctx, cfg.SiliconFlowKey, *fileFlag, "", cfg.Language)`

约 12 行修改，跨三个文件，0 行逻辑新增。

### DoD
- [ ] `go test ./...`
- [ ] `go build ./cmd/voci/`
- [ ] `grep -q 'Language string' /home/yale/work/voci/internal/daemon/server.go`
- [ ] `grep -q 'Language string' /home/yale/work/voci/internal/mcp/server.go`
- [ ] `grep -q 'cfg.Language' /home/yale/work/voci/cmd/voci/main.go`
- [ ] `grep -q 'language string' /home/yale/work/voci/internal/daemon/server.go`
- [ ] `grep -q 'language string' /home/yale/work/voci/internal/mcp/server.go`
- [ ] `go test ./internal/daemon/ -run TestHandleTranscribePassesLanguage -v`
- [ ] `go test ./internal/mcp/ -run TestToolsCallPassesLanguage -v`

---

## Constraints

- 不新增 CLI flag `--language`；Language 是进程级配置，仅通过环境变量 `VOCI_LANGUAGE` 或 yaml `language:` 注入。
- 不实现 Whisper API 的真实端到端验证；httptest mock 仅验证模型选择逻辑，不验证 SiliconFlow 响应格式差异。
- 不引入 `ASRModel` 配置覆盖字段；语言→模型映射完全封装在 `internal/asr` 包内，后续可在独立 task 中扩展。
- 不修改任何 HTTP/MCP 协议字段；浏览器端和 MCP 客户端均无需感知语言参数。
- Language 作用域为进程级，不支持单次请求粒度的语言切换——已知限制，记录于 Trade-offs。
- 每个 Phase 代码变更量不超过 200 行。
- 各 Phase 输出有序依赖：Phase A 完成后 Phase B 可独立进行；Phase C 依赖 Phase B（需 `asr.Transcribe` 五参数签名稳定后再做 wiring）。

## Acceptance Gate
- [ ] `go test ./...`
- [ ] `go build ./cmd/voci/`
- [ ] `grep -q 'Language string' /home/yale/work/voci/internal/config/config.go`
- [ ] `grep -q 'VOCI_LANGUAGE' /home/yale/work/voci/internal/config/config.go`
- [ ] `grep -q 'language string' /home/yale/work/voci/internal/asr/siliconflow.go`
- [ ] `grep -q 'languageModel' /home/yale/work/voci/internal/asr/siliconflow.go`
- [ ] `grep -q 'Language string' /home/yale/work/voci/internal/daemon/server.go`
- [ ] `grep -q 'Language string' /home/yale/work/voci/internal/mcp/server.go`
- [ ] `grep -q 'cfg.Language' /home/yale/work/voci/cmd/voci/main.go`
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Proposal self-review: APPROVED
premise-ledger:
[E] Motivation explains WHY in 6 lines (hardcoded model, missing Language field, downstream failures, TASK-32 dependency): confirmed by reading internal/asr/siliconflow.go and internal/config/config.go
[E] Goals are numbered and concretely verifiable (unit test, compiler check, httptest mock): each goal states its verification method explicitly
[E] Approach aligns with actual codebase: config.go already uses env→file→default pattern for OllamaHost; asr.Transcribe signature confirmed as 4-param; TranscribeFn type aliases confirmed in daemon/server.go:21 and cmd/voci/main.go:86; daemon.Server struct confirmed with APIKey field; all 4 call paths (once/serve/daemon/mcp) confirmed in main.go
[C] Trade-offs and risks identified: 5 items covering breaking change, hardcoded mapping, no Whisper e2e, process-level scope, TASK-32 dependency
[C] No contradictions: 'not implementing Whisper now' and 'unit test for language=en branch' are consistent (mock test != real implementation)
GCL-self-report: E=3 C=2 H=0

Plan review iteration 1: APPROVED
premise-ledger:
E Goal coverage: all 5 Goals mapped to Phase A/B/C explicitly
E TDD structure: each Phase has ### Tests before ### Implementation
E TDD order: first DoD item in every Phase is `go test ./...`
E Acceptance gate: first item is `go test ./...`
E DoD executability: all DoD and Acceptance Gate items are shell commands
E Absence checks: `! grep -q` used correctly in Phase B DoD
E Phase ordering: A→B→C with explicit dependency statement, no circular deps
E Scope discipline: every Phase traces directly to a numbered Goal
E File paths: all 10 referenced files confirmed present in codebase
GCL-self-report: E=9 C=0 H=0

claimed: 2026-06-29T02:55:42Z

claimed: 2026-06-29T02:55:53Z

Completed: 2026-06-29T03:08:53Z
## Execution Summary
Result: Done / Commit: 9d7220558db5af359c42b450385c7ceaa3299421
Key: Language field (VOCI_LANGUAGE env, default zh); asr.Transcribe 5-param; zh→TeleSpeechASR, else→whisper-large-v3
<!-- SECTION:NOTES:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 go test ./...
- [ ] #2 go test ./internal/config/ -run TestLoadConfigLanguage -v
- [ ] #3 grep -q 'Language string' /home/yale/work/voci/internal/config/config.go
- [ ] #4 grep -q 'VOCI_LANGUAGE' /home/yale/work/voci/internal/config/config.go
- [ ] #5 go test ./...
- [ ] #6 go test ./internal/asr/ -run TestTranscribeEnUsesWhisper -v
- [ ] #7 go test ./internal/asr/ -run TestTranscribeZhUsesTelespeech -v
- [ ] #8 grep -q 'language string' /home/yale/work/voci/internal/asr/siliconflow.go
- [ ] #9 grep -q 'languageModel' /home/yale/work/voci/internal/asr/siliconflow.go
- [ ] #10 ! grep -q '"TeleAI/TeleSpeechASR"' /home/yale/work/voci/internal/asr/siliconflow.go
- [ ] #11 go test ./...
- [ ] #12 go build ./cmd/voci/
- [ ] #13 grep -q 'Language string' /home/yale/work/voci/internal/daemon/server.go
- [ ] #14 grep -q 'Language string' /home/yale/work/voci/internal/mcp/server.go
- [ ] #15 grep -q 'cfg.Language' /home/yale/work/voci/cmd/voci/main.go
- [ ] #16 grep -q 'language string' /home/yale/work/voci/internal/daemon/server.go
- [ ] #17 grep -q 'language string' /home/yale/work/voci/internal/mcp/server.go
- [ ] #18 go test ./internal/daemon/ -run TestHandleTranscribePassesLanguage -v
- [ ] #19 go test ./internal/mcp/ -run TestToolsCallPassesLanguage -v
- [ ] #20 go test ./...
- [ ] #21 go build ./cmd/voci/
- [ ] #22 grep -q 'Language string' /home/yale/work/voci/internal/config/config.go
- [ ] #23 grep -q 'VOCI_LANGUAGE' /home/yale/work/voci/internal/config/config.go
- [ ] #24 grep -q 'language string' /home/yale/work/voci/internal/asr/siliconflow.go
- [ ] #25 grep -q 'languageModel' /home/yale/work/voci/internal/asr/siliconflow.go
- [ ] #26 grep -q 'Language string' /home/yale/work/voci/internal/daemon/server.go
- [ ] #27 grep -q 'Language string' /home/yale/work/voci/internal/mcp/server.go
- [ ] #28 grep -q 'cfg.Language' /home/yale/work/voci/cmd/voci/main.go
<!-- DOD:END -->

---
id: TASK-1
title: 原型：语音→上下文感知 ASR→改写 的核心管道验证
status: 'Basic: In Progress'
assignee: []
created_date: '2026-06-27 13:56'
updated_date: '2026-06-27 15:29'
labels:
  - 'kind:basic'
dependencies: []
priority: high
ordinal: 1000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
验证 voci 的核心假设，不涉及任何 Claude Code 集成、不做 Web UI。

## 目标
验证「注入代码库上下文（asr_hint）是否能显著提升 LLM 对口语化编程指令的理解质量」。

## 技术栈
- 语言：Go（`go install`，单二进制，零运行时依赖）
- ASR：SiliconFlow `TeleAI/TeleSpeechASR`（`POST /audio/transcriptions`，已由 TASK-7 验证）
- 样本生成：SiliconFlow `FunAudioLLM/CosyVoice2-0.5B` TTS（`POST /audio/speech`，已由 TASK-7 验证）
- 上下文修正 + 改写：本机 ollama `gemma4:e4b`
- API key：`~/.config/voci/config.yaml`（`siliconflow_api_key`，已由 TASK-7 配置）

## 语音样本
由 SiliconFlow TTS（CosyVoice2-0.5B）生成，存于 `testdata/`：
- 内容：开发者口语化编程指令，故意选择会令 ASR 产生典型错误的表达
- 格式：每条样本仅 `.wav`（SiliconFlow TTS 直接输出）
- 最少 5 条，覆盖：任务 ID 混淆（"task one" → TASK-1）、项目名混淆（"vocal" → voci）、路径音近、歧义指令、组合混淆
- 生成脚本：`scripts/gensamples/main.go`，调用 SiliconFlow `/audio/speech`，key 从 config 读取

## 范围（仅 CLI）
- 输入：`--file audio.wav`（SiliconFlow ASR 转写为 raw text）
- Stage 1 上下文构建（纯 Go，不调 LLM）：读 backlog/tasks/*.md frontmatter、CLAUDE.md、git log --oneline -10，产出 asr_hint 字符串
- Stage 2 ASR：SiliconFlow TeleSpeechASR 将音频转为 RAW 文本（真实 ASR baseline，无任何 LLM 处理）
- Stage 3 A/B 对比：RAW 文本注入 asr_hint → gemma4:e4b → HINTED（测量上下文注入的增量效果）
- Stage 4 改写：gemma4:e4b 将 HINTED 改写为清晰的编程指令，无法确定意图时标注 [ambiguous]
- Stage 5 输出：CLI 并列打印 RAW / HINTED / REWRITTEN
- Stage 6 迭代（--iterate）：接受用户文本反馈，连同上轮 rewritten 回送 Stage 4 重新改写

## 不做
- 麦克风实时录制
- Claude Code / Codex / Gemini 集成
- Web UI
- tmux/stdin 注入
- 人类确认 gate（仅打印结果）

## 验证指标
- 有无 asr_hint 时，HINTED 与 RAW 的专有名词/任务 ID 识别差异（人工比对）
- 改写后指令的可执行性
- 歧义检测的有效性
<!-- SECTION:DESCRIPTION:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
# Plan: 原型：语音→上下文感知-ASR→改写-的核心管道验证

## Phase A: 项目脚手架、配置读取与语音样本生成

### Tests (write first)

File: `internal/context/builder_test.go`
- `TestBuildContextReturnsString` — call `BuildContext(tmpDir)` with temp dir; assert return non-empty
- `TestBuildContextReadsBacklogTasks` — write `backlog/tasks/task-1.md` with frontmatter `id: TASK-1\ntitle: Fix login bug`; assert "TASK-1" and "Fix login bug" in result
- `TestBuildContextReadsCLAUDEMd` — write CLAUDE.md with sentinel text; assert sentinel in result
- `TestBuildContextReadsGitLog` — inject fake gitRunner returning `"abc1234 add auth\n"`; assert "add auth" in result
- `TestBuildContextMissingCLAUDEMd` — omit CLAUDE.md; assert no error, result is string
- `TestBuildContextEmptyBacklog` — empty `backlog/tasks/`; assert no error

File: `internal/config/config_test.go`
- `TestLoadConfigFromEnv` — set `SILICONFLOW_API_KEY` env; assert key returned, no error
- `TestLoadConfigFromFile` — write `config.yaml` with `siliconflow_api_key: sk-test`; assert key read
- `TestLoadConfigMissingKey` — no env, no file; assert error message contains "set SILICONFLOW_API_KEY"

### Implementation

- `go.mod` — module `github.com/yalehu/voci`, go 1.23; dep: `gopkg.in/yaml.v3`
- `internal/config/config.go` — `type Config struct { SiliconFlowKey, OllamaHost string }`; `LoadConfig() (Config, error)`: reads `SILICONFLOW_API_KEY` env, falls back to `~/.config/voci/config.yaml`; `OLLAMA_HOST` env or config `ollama_host`, default `http://localhost:11434`; key never printed to stdout/stderr (errors show `sk-xx**...`)
- `internal/context/builder.go` — `BuildContext(root string) string`; reads `backlog/tasks/*.md` YAML frontmatter (id, title, status), reads CLAUDE.md if present, runs `git -C root log --oneline -10` via injected `gitRunner func() string` (default: real exec)
- `cmd/voci/main.go` — stub: `func main() { fmt.Println("voci") }`
- `scripts/gensamples/main.go` — calls SiliconFlow `/audio/speech` (POST, same pattern as `scripts/check-siliconflow/main.go`); 5 hardcoded 口语化编程指令（含 ASR 典型错误场景）; outputs `testdata/sample-01.wav` … `testdata/sample-05.wav`; key from `LoadConfig()`; idempotent (skip if file exists)

  样本场景（硬编码 input text）：
  1. 任务 ID 混淆："fix the task one login bug" → ASR 可能输出 "task one"，hint 应纠正为 TASK-1
  2. 项目名混淆："add logging to the vocal CLI" → voci
  3. 文件名音近："update the context builder in inter nul context" → internal/context
  4. 歧义指令："make it faster somehow" → [ambiguous] 标注
  5. 组合混淆："rewrite the task three handler in the vocal package" → TASK-3, voci

### DoD
- [ ] `go test ./internal/context/... ./internal/config/...`
- [ ] `go build ./cmd/voci`
- [ ] `go run ./scripts/gensamples && ls testdata/sample-01.wav`

---

## Phase B: SiliconFlow ASR 客户端 + ollama 管道

### Tests (write first)

File: `internal/asr/siliconflow_test.go`
- `TestTranscribeReturnsText` — `httptest.NewServer` returning `{"text":"hello"}`; assert `Transcribe(ctx, key, wavPath, serverURL)` returns "hello", nil
- `TestTranscribeSendsMultipartWithModel` — capture request; assert Content-Type is multipart/form-data and `model=TeleAI/TeleSpeechASR` field present
- `TestTranscribeHTTPError` — server returns 500; assert error returned

File: `internal/ollama/client_test.go`
- `TestChatReturnsContent` — `httptest.NewServer` returning `{"message":{"content":"ok"},"done":true}`; assert `Chat(ctx, url, "gemma4:e4b", msgs)` returns "ok", nil
- `TestChatSendsModelAndMessages` — capture request body; assert `model == "gemma4:e4b"` and messages non-empty
- `TestChatHTTPError` — server returns 500; assert error

File: `internal/pipeline/pipeline_test.go`
- `TestRunHintedCallsChatWithHint` — inject fake chatFn; assert `RunHinted(ctx, raw, hint, fakeFn)` calls chatFn with messages containing both raw and hint text
- `TestRunHintedEmptyHint` — hint is ""; assert chatFn called with only raw text
- `TestRewriteReturnsClearedInstruction` — fake chatFn returns "add logging to auth.go"; assert `Rewrite(ctx, hinted, hint, fakeFn)` returns that string
- `TestRewritePassesThroughAmbiguous` — fake returns "[ambiguous] unclear intent"; assert result contains "[ambiguous]"

### Implementation

- `internal/asr/siliconflow.go` — `Transcribe(ctx context.Context, key, audioPath, apiURL string) (string, error)`; POST multipart/form-data to apiURL (default `https://api.siliconflow.cn/v1/audio/transcriptions`): field `file`=wav bytes (filename `audio.wav`), field `model`=`TeleAI/TeleSpeechASR`; decode `{"text":"..."}`; key in Authorization header, never logged
- `internal/ollama/client.go` — `Chat(ctx context.Context, host, model string, messages []Message) (string, error)`; POST to `host/api/chat`; reads streaming NDJSON until `done:true`, accumulates `message.content`
- `internal/pipeline/pipeline.go` — `type ChatFn func(ctx context.Context, messages []Message) (string, error)`; `RunHinted(ctx context.Context, raw, hint string, chat ChatFn) (string, error)`: system prompt instructs gemma4:e4b to correct proper nouns/task IDs using the provided hint, returns corrected text; `Rewrite(ctx context.Context, hinted, hint string, chat ChatFn) (string, error)`: instructs gemma4:e4b to produce a clear programming instruction, mark `[ambiguous]` when intent is unclear

### DoD
- [ ] `go test ./internal/asr/... ./internal/ollama/... ./internal/pipeline/...`
- [ ] `go vet ./...`

---

## Phase C: CLI `--file` + 输出

### Tests (write first)

File: `cmd/voci/main_test.go`
- `TestCLIFileFlagPrintsRAW` — inject fake ASR+pipeline fns; invoke with `--file sample.wav`; assert stdout contains "RAW"
- `TestCLIFileFlagPrintsHINTED` — assert stdout contains "HINTED"
- `TestCLIFileFlagPrintsREWRITTEN` — assert stdout contains "REWRITTEN"
- `TestCLINoFileExitsNonzero` — invoke with no `--file`; assert exit code != 0
- `TestCLIFileMissingExitsNonzero` — `--file /nonexistent.wav`; assert exit code != 0

### Implementation

- `internal/output/print.go` — `PrintComparison(w io.Writer, raw, hinted, rewritten string)`; writes `RAW (no hint):\n{raw}\n\nHINTED:\n{hinted}\n\nREWRITTEN:\n{rewritten}\n`
- `cmd/voci/main.go` — flag `--file string` (required, must exist); loads `Config`; builds `BuildContext(cwd)` (→ hint); calls `Transcribe` (→ RAW); calls `RunHinted(raw, hint)` (→ HINTED); calls `Rewrite(hinted, hint)` (→ REWRITTEN); `PrintComparison`

### DoD
- [ ] `go test ./...`
- [ ] `go build -o voci ./cmd/voci && echo "build ok"`
- [ ] `./voci --file testdata/sample-01.wav 2>&1 | grep -q REWRITTEN`

---

## Phase D: `--iterate` 迭代模式

### Tests (write first)

File: `internal/pipeline/iterate_test.go`
- `TestIterateExitsOnEmptyInput` — reader returning ""; assert rewriteFn not called
- `TestIterateCallsRewriteWithFeedback` — reader returns "make it shorter\n" then ""; assert rewriteFn called once with feedback in prompt
- `TestIterateChainsRewrites` — reader returns "f1\n" then "f2\n" then ""; assert second call contains first rewrite result
- `TestIteratePrintsRewrittenEachRound` — capture writer; assert "REWRITTEN" appears once per feedback round

File: `cmd/voci/main_test.go` (add)
- `TestCLIIterateFlagAccepted` — inject no-op iterate fn; run `--file sample.wav --iterate`; assert exit 0

### Implementation

- `internal/pipeline/iterate.go` — `IterateLoop(ctx context.Context, initialRewritten, hint string, r io.Reader, w io.Writer, rewriteFn RewriteFn) error`; loop: read line from r, if empty break, build prompt combining previous rewritten + user feedback, call rewriteFn, print new REWRITTEN to w
- Update `cmd/voci/main.go` — add `--iterate` bool flag; after PrintComparison, if set: call `IterateLoop(ctx, rewritten, hint, os.Stdin, os.Stdout, rewriteFn)`

### DoD
- [ ] `go test ./internal/pipeline/...`
- [ ] `go build -o voci ./cmd/voci`
- [ ] `./voci --file testdata/sample-01.wav --iterate < /dev/null 2>&1 | grep -q RAW`

---

## Constraints
- **SiliconFlow API key**：从 `SILICONFLOW_API_KEY` 环境变量或 `~/.config/voci/config.yaml` 读取；key 不打印到 stdout/stderr，错误提示仅显示前缀 `sk-xx**...`（已由 TASK-7 验证）
- **ollama 端点**：`OLLAMA_HOST` 环境变量或 config `ollama_host`，默认 `http://localhost:11434`
- Go ≥ 1.23；唯一外部依赖 `gopkg.in/yaml.v3`（frontmatter 解析）
- **分发**：`go install github.com/yalehu/voci@latest` 或单二进制，零运行时依赖
- 测试全部用 `httptest.NewServer` mock HTTP 端点（SiliconFlow ASR + ollama），不依赖真实服务
- `testdata/` 不提交 git（加入 `.gitignore`）；由 `go run ./scripts/gensamples` 按需生成
- 专有名词一致：asr_hint、RAW、HINTED、REWRITTEN、[ambiguous]
- **A/B 验证语义**：RAW = SiliconFlow ASR 直接输出（无任何 LLM，真实 baseline）；HINTED = RAW + asr_hint → gemma4:e4b（量化上下文注入增量）
- `BuildContext` 的 git 调用通过注入 `gitRunner` func 隔离，测试不依赖真实 repo
- 每个 Phase ≤ 200 行代码变更

## Acceptance Gate
- [ ] `go test ./...`
- [ ] `go build -o voci ./cmd/voci`
- [ ] `go run ./scripts/gensamples && ls testdata/sample-01.wav`
- [ ] `./voci --file testdata/sample-01.wav 2>&1 | grep -q REWRITTEN`
- [ ] `./voci --file testdata/sample-01.wav 2>&1 | grep -q HINTED`
- [ ] `./voci --file testdata/sample-04.wav 2>&1 | grep -q ambiguous`
- [ ] `./voci --file testdata/sample-01.wav --iterate < /dev/null 2>&1 | grep -q RAW`
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Proposal approved (from existing description). Starting plan draft.

Plan review iteration 1: APPROVED
premise-ledger:
[E] Goal coverage: All 5 Stages from proposal scope (context build, ASR, rewrite, output, iterate) are addressed by Phases A–D and Acceptance Gate items — read directly from both files.
[E] TDD structure: Every Phase (A, B, C, D) contains ### Tests followed by ### Implementation in that order — read directly from /tmp/ftb-plan.md.
[E] TDD order: First ### DoD item in every Phase uses `pytest -k` (A: test_context_builder, B: test_asr, C: test_rewriter or test_cli, D: test_iterate) — read directly from plan.
[E] Acceptance gate: First ## Acceptance Gate item is `pytest` — read directly from plan.
[E] DoD executability: All ### DoD items and ## Acceptance Gate items are shell commands (pytest, python -c, python -m voci | grep -q) — read directly from plan.
[H] Absence checks: No absence (! grep -q) patterns needed; Acceptance Gate checks presence of REWRITTEN/RAW output — determined from background knowledge of the pattern convention.
[E] Phase ordering: A (scaffold+context) → B (ASR uses context) → C (rewriter+CLI uses A+B) → D (iterate+CLI flag uses C) — no circular deps, read directly from plan.
[E] Scope discipline: Every Phase maps 1-to-1 to a Stage in the proposal; nothing extra introduced — verified by cross-reading both files.
[E] File paths: src/voci/, tests/, pyproject.toml, tests/fixtures/ all consistent with a Python src-layout project — read from plan.
GCL-self-report: E=8 C=0 H=1

ASR 变更（2026-06-27）：Stage 2 改为 faster-whisper（本机 CPU）做原始转写 + ollama gemma4:e4b（http://localhost:11434）做 asr_hint 上下文修正，移除 gpt-4o-transcribe 依赖；新增依赖 faster-whisper>=1.0、ollama>=0.3；测试 mock 对象由 openai.OpenAI 改为 faster_whisper.WhisperModel + ollama.chat。

架构审查（2026-06-27，严苛 MVP 视角）：
1. 缺陷修正——补齐核心验证能力：原 plan 声称验证 asr_hint 提升效果，但 CLI 只打印单版 RAW/REWRITTEN，无对照。现将 Stage 2 拆为 transcribe_raw（whisper，无 hint baseline）+ apply_hint（gemma4:e4b，注入 hint），RAW vs HINTED 天然构成 with-hint/without-hint A/B 对照，CLI 三段并列打印。
2. 缺陷修正——后端一致性：Stage 3 改写由云端 gpt-4o-mini 改为本机 ollama gemma4:26b，彻底移除 OpenAI 依赖，全本机零 API key，保证验证可复现。
3. DoD 去重（原 #10/#11 重复 pytest）并对齐新函数签名；新增 HINTED 输出的 Acceptance Gate 断言。

语言切换（2026-06-27）：Python → Go（单二进制分发）；移除 faster-whisper 和 gemma4:26b；统一为单一模型 gemma4:e4b；输入改为文本（--text 或 stdin）；音频采集留给下一任务；测试 mock 改用 httptest.NewServer。

样本生成（2026-06-27）：语音测试样本改由 ollama openbmb/minicpm-o4.5（TTS）生成，存于 testdata/（不提交 git）。scripts/gensamples/main.go 生成 5 条含 ASR 典型错误的样本（任务 ID 混淆、项目名误读、歧义指令等）。minicpm-o4.5 仅用于样本生成，不进入主管道；主管道仍仅用 gemma4:e4b。

SiliconFlow 迁移（2026-06-27）：基于 TASK-7 验证结果，全面改用 SiliconFlow 服务：(1) 样本生成改用 SiliconFlow TTS（CosyVoice2-0.5B），输出 .wav 替代 .txt；(2) Stage 2 RAW 改用 SiliconFlow ASR（TeleSpeechASR）直接转写音频（真实 baseline，无 LLM 处理）；(3) CLI 入口改为 `--file audio.wav`（替代 `--text`）；(4) 新增 `internal/config/config.go` 统一管理 API key 和 ollama 端点；(5) Phase B 从"ollama A/B"重构为"ASR + 管道"，新增 `internal/asr/siliconflow.go`。主管道 HINTED/REWRITTEN 仍由 ollama gemma4:e4b 处理。

claimed: 2026-06-27T15:29:34Z
<!-- SECTION:NOTES:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 go test ./internal/context/... ./internal/config/...
- [ ] #2 go build ./cmd/voci
- [ ] #3 go run ./scripts/gensamples && ls testdata/sample-01.wav
- [ ] #4 go test ./internal/asr/... ./internal/ollama/... ./internal/pipeline/...
- [ ] #5 go vet ./...
- [ ] #6 go test ./...
- [ ] #7 go build -o voci ./cmd/voci && echo "build ok"
- [ ] #8 ./voci --file testdata/sample-01.wav 2>&1 | grep -q REWRITTEN
- [ ] #9 ./voci --file testdata/sample-04.wav 2>&1 | grep -q ambiguous
- [ ] #10 go test ./internal/pipeline/...
- [ ] #11 ./voci --file testdata/sample-01.wav --iterate < /dev/null 2>&1 | grep -q RAW
<!-- DOD:END -->

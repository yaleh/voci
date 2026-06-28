---
id: TASK-6
title: Web UI：浏览器 PTT 录音、预览编辑与模式切换
status: 'Basic: Backlog'
assignee: []
created_date: '2026-06-27 13:57'
updated_date: '2026-06-28 14:26'
labels:
  - 'kind:basic'
dependencies:
  - TASK-3
priority: low
ordinal: 6000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
voci 浏览器前端：按住说话（PTT）、transcript 预览/编辑、输入模式实时切换。后端为 Go（与 TASK-1 一致，非 FastAPI）。

## MVP 范围（无构建步骤）
- 单页 HTML + Vanilla JS
- recorder.js：MediaRecorder → 按住 Space PTT → 松开 → POST 音频（.wav）
- 后端复用 TASK-1 管道：音频 → SiliconFlow ASR → RAW，→ ollama gemma4:e4b → HINTED/REWRITTEN
- 展示 RAW / HINTED / REWRITTEN 对比
- 预览/编辑：发送前可修改改写结果
- 模式切换 toggle：preview ↔ direct，无需重启

## 后端契约（Go net/http）
- POST /api/voice/transcribe（音频 + tool 参数）
- GET/POST /api/proposals（ActionProposal 确认，见 TASK-3）

## 不做（移出 MVP）
- WS /api/voice/stream 实时转写（后续任务）
- React/Vite 等构建工具

## 降级理由（Epic → Basic）
MVP 为单页 HTML + vanilla JS + 2 个 HTTP 端点，后端管道复用 TASK-1，流式转写已移出范围，规模 contained，单个 TDD pass 可完成。

## 设计约束
- UI 是 gate 的可视化层，确认逻辑在后端（TASK-3）
- 工具无关：通过 tool 参数选择下游 adapter（TASK-5）

## 依赖
- TASK-3（ActionProposal gate）；交付通道 TASK-5
<!-- SECTION:DESCRIPTION:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
## 架构对齐 (refresh post-TASK-20, 2026-06-28)

### Background (WHY)

voci 的浏览器前端目标是「按住说话 → 预览/编辑改写结果 → 确认后注入会话」。TASK-20 已落地后端的两步契约，本任务的后端工作已基本完成：`internal/daemon/server.go` 已实现 `POST /api/voice/transcribe`（跑完整 pipeline：ASR→hinted→rewrite→classify，返回 ActionProposal JSON，**不** emit 到 stdout）与 `POST /api/voice/emit`（写一行 Event JSON 到 EventWriter=os.Stdout → Monitor 注入会话）。这正是「方案 A：前端门控 / 两步提交」——浏览器即人类 gate，Monitor-push 主路径默认 voice-trusted、不过 gate（见 TASK-17），而 Web UI 的差异价值就是发送前可预览/编辑。

缺口因此从「后端管道 + gate 设计」收敛为 **纯前端 + 静态托管**：`voci serve` 当前 `Handler()` 只注册两个 API 端点，没有 `GET /`、没有 FileServer、没有 go:embed（见 `cmd/voci/main.go` 的 `--serve` 分支，EventWriter=os.Stdout）。浏览器无法加载任何页面。次要缺口：`handleEmit` 硬编码 `Kind="direct_prompt"` 且只收 `{text}`，丢弃了 preview 的分类/置信度，导致 backlog_action 等意图在确认后退化为 direct_prompt。

### Goals (verifiable)

1. **静态托管**：`voci serve` 对 `GET /` 返回 HTTP 200 且 `Content-Type: text/html`，body 含 `<title>voci</title>`（或等价标记）。
2. **资源可取**：`GET /recorder.js` 返回 200 且 `Content-Type` 为 JavaScript（`text/javascript` 或 `application/javascript`）。
3. **嵌入式资源**：HTML/JS 经 `go:embed` 打进二进制，`go build ./cmd/voci` 后单文件可启动，无外部构建步骤（无 npm/node/vite）；删除源码目录后 `serve` 仍能托管页面。
4. **录音上送**：`recorder.js` 按住 Space 触发 `MediaRecorder` 开始录音，松开停止，将捕获的音频 blob 经 `POST /api/voice/transcribe` 上送（请求体为原始音频字节，与现有 handler 的 `io.ReadAll(r.Body)` 契约一致）。
5. **预览展示**：transcribe 返回后，UI 至少渲染 RAW 与 REWRITTEN 两栏并显示 Kind 与 Confidence（HINTED 为可选第三栏，见下文权衡：需后端响应扩展 `hinted` 字段；MVP 验收以 RAW+REWRITTEN 为准）。**字段名**：transcribe 返回的是 `intent.ActionProposal`，该结构体**无 json tag**，故 JSON 键为 Go 字段名 `Rewritten` / `RawTranscript` / `Kind` / `Confidence`（大写，非 snake_case）；前端按此读取。注意 emit 写出的 `Event`（eventlog.go）才用 snake_case tag，二者不同结构勿混。
6. **可编辑确认**：REWRITTEN 字段可编辑；点击 Confirm 按钮将编辑后的文本经 `POST /api/voice/emit` 提交，emit 成功（204）后 UI 复位到待录音状态。
7. **Kind 透传**：emit 保留 preview 选定的 `Kind`（非硬编码 direct_prompt）；当请求体携带 `kind` 时，写出的 Event.Kind 等于该值；未携带时回退 `direct_prompt`（向后兼容）。
8. **现有 API 行为不回归**：transcribe 仍不写 EventWriter；既有 server/main 测试保持通过。

### Proposed Approach

**后端（Go net/http，最小改动）**
- 新增前端资源目录（如 `internal/daemon/web/`，含 `index.html`、`recorder.js`），用 `//go:embed web/*` 声明一个 `embed.FS`。
- 在 `Server.Handler()` 中追加静态路由：`mux.Handle("/", http.FileServerFS(sub))`（项目为 Go 1.23，`http.FileServerFS` 可用），`sub` 为 `fs.Sub(embeddedFS, "web")`。两个 `/api/voice/*` 精确路由优先于 `/` 前缀路由（ServeMux 最长前缀匹配，API 路径更具体，故不被 FileServer 截获）。
- 资源嵌入随包 init，不改变 `Server` 字段，`cmd/voci` 的 `--serve` 构造无需变更即可托管页面（Goal 1-3）。
- **Kind 透传（Goal 7，决定纳入范围）**：`emitRequest` 增加可选字段 `Kind string \`json:"kind"\``；`handleEmit` 用 `req.Kind` 填 `Event.Kind`，空则回退 `"direct_prompt"`。改动局部、向后兼容（旧的只发 `{text}` 的客户端行为不变），且不透传则确认流程语义错误（backlog_action 被当 direct_prompt 注入），故纳入 MVP。可选透传 `Confidence`/`RawTranscript` 以填满 Event，但非必须。

**前端（单页 + vanilla JS，无构建工具）**
- `index.html`：状态展示区（RAW/HINTED/REWRITTEN 标签）、可编辑 `<textarea>`（绑定 REWRITTEN）、Kind/Confidence 只读显示、Confirm 按钮、PTT 提示。引入 `recorder.js`。
- `recorder.js`：`keydown`/`keyup` 监听 Space 实现 PTT；`navigator.mediaDevices.getUserMedia({audio})` + `MediaRecorder` 录音，`stop` 时合并 chunks 为 Blob → `fetch('/api/voice/transcribe', {method:'POST', body:blob})` → 渲染返回的 ActionProposal，读 `resp.Rewritten` / `resp.RawTranscript` / `resp.Kind` / `resp.Confidence`（大写 Go 字段名，因 ActionProposal 无 json tag）。Confirm → `fetch('/api/voice/emit',{method:'POST', body: JSON.stringify({text: edited, kind: previewedKind})})`。
- 两步提交即 gate：transcribe ≠ emit，用户必须点 Confirm 才写 stdout。

**测试**：Go httptest 覆盖 `GET /`(200/html)、`GET /recorder.js`(200/js)、emit 带 `kind` 透传 / 不带 kind 回退、transcribe 不写 EventWriter。前端为静态资源，以「200 + content-type + 关键标记字符串」断言托管正确，不引入浏览器自动化。

### Trade-offs / Risks

- **不做（明确移出范围，避免镀金）**：
  - WS `/api/voice/stream` 实时流式转写 → 后续任务。
  - React/Vite/任何构建工具 → 违反「无构建步骤」约束。
  - 鉴权 / CORS / 远程访问加固 → 属 TASK-19（Android / 远程）范畴；`serve` 默认绑 `127.0.0.1`，本机同源，MVP 不引入 auth/CORS。
- **MediaRecorder 音频格式风险**：绝大多数浏览器 `MediaRecorder` 默认产出 `audio/webm`（Opus）或 `audio/ogg`，**而非 wav**；transcribe handler 当前把上送字节落盘为 `*.wav` 临时文件（仅文件名后缀，不做转码）。处置：**接受 webm/ogg 原样上送**，依赖下游 SiliconFlow ASR 按实际容器/编码解析（多数云 ASR 接受 webm/opus）；不在浏览器侧做 wav 编码（纯 JS PCM→wav 编码复杂且增重，违背 MVP）。**约束记录**：临时文件 `.wav` 后缀名义不准确，若 ASR 严格按扩展名判定格式则可能失败——缓解为让 handler 依据上送 `Content-Type` 选择落盘后缀（可作小幅增强，列为 Goal 之外的可选项），MVP 先以实测 SiliconFlow 行为为准；若实测失败，回退方案是前端用 `MediaRecorder` 的 `mimeType` 协商 + handler 按 Content-Type 命名。该风险不阻塞静态托管与两步流程的交付。
- **HINTED 展示缺字段**：`transcribe` 返回的是 `ActionProposal`，其中**没有** HINTED 中间产物（pipeline 内部 hinted→rewrite 后未透出）。Goal 5 的 HINTED 展示需后端在响应中附带 hinted 文本，或前端只展示 RAW/REWRITTEN 两栏。处置：MVP 以 **RAW + REWRITTEN** 两栏为准（满足核心对比与编辑诉求）；HINTED 展示降级为可选——若要保留则需 transcribe 响应扩展一个 `hinted` 字段（小幅改动，列为可选增强，不阻塞）。
- **Space 键 PTT 冲突**：页面有可编辑 `<textarea>`，Space 既是 PTT 又是空格输入。处置：仅当焦点不在输入框时 Space 触发 PTT（`document.activeElement` 判定），编辑改写文本时 Space 正常输入。
- **ServeMux 路由优先级**：依赖 `/api/voice/*` 比 `/` 更具体而不被 FileServer 吞掉，已由 net/http ServeMux 最长匹配保证；测试 Goal 8 守护此不回归。

---

# Plan: Web UI — 浏览器 PTT 录音、预览编辑与两步提交

> 后端管道 + gate 已由 TASK-20 落地。本任务收敛为「纯前端 + go:embed 静态托管 + emit Kind 透传」。
> 测试运行器：每阶段 `go test ./internal/daemon/...`；全量 `go test ./...`。
> 字段名注意：`/api/voice/transcribe` 返回 `intent.ActionProposal`（**无 json tag** → 键为大写 `Rewritten`/`RawTranscript`/`Kind`/`Confidence`）；`/api/voice/emit` 写出的 `daemon.Event`（eventlog.go）用 snake_case tag（`rewritten`/`kind`/`raw_transcript`/`confidence`）。两者勿混。

## Phase A: 静态资源服务 (go:embed + GET /)
### Tests (write first)
新增 `internal/daemon/static_test.go`：
- `TestHandler_ServesIndexHTML`：`GET /` 返回 200，`Content-Type` 以 `text/html` 开头，body 含标记 `<title>voci</title>`。
- `TestHandler_ServesRecorderJS`：`GET /recorder.js` 返回 200，`Content-Type` 含 `javascript`（`text/javascript` 或 `application/javascript`）。
- `TestHandler_APIRoutesNotShadowed`：`POST /api/voice/transcribe`（fake body）仍返回 200 且 body 可解码为 `intent.ActionProposal`，证明 `GET /` 前缀路由未截获 `/api/` 精确路由（守护 Goal 8 不回归）。
（先失败：当前 `Handler()` 只注册两个 `/api/` 端点，无 `GET /`、无 embed）
### Implementation
- 新建占位资源 `internal/daemon/web/index.html`（至少含 `<title>voci</title>` 与 `<script src="recorder.js">`）与 `internal/daemon/web/recorder.js`（至少非空，可为后续 Phase C 充实的最小占位）。
- 在 server.go 顶部加 `//go:embed web/*` 声明 `var embeddedFS embed.FS`（import `embed`、`io/fs`）。
- 在 `Server.Handler()` 末尾追加静态路由：`sub, _ := fs.Sub(embeddedFS, "web")`，`mux.Handle("/", http.FileServerFS(sub))`（Go 1.23.1，`http.FileServerFS` 可用）。两个 `mux.HandleFunc("/api/voice/...")` 为更具体路径，ServeMux 最长匹配保证不被 `/` 吞掉。
### DoD
- [ ] `go test ./internal/daemon/...`
- [ ] `grep -q 'go:embed' internal/daemon/server.go`
- [ ] `grep -q 'FileServerFS' internal/daemon/server.go`
- [ ] `test -f internal/daemon/web/index.html`
- [ ] `test -f internal/daemon/web/recorder.js`

## Phase B: emit Kind 透传
### Tests (write first)
追加到 `internal/daemon/server_test.go`：
- `TestEmit_PreservesKind`：`POST /api/voice/emit` body `{"text":"做个任务","kind":"backlog_action"}` → 204，写出 Event 行解码后 `ev.Kind == "backlog_action"`（非硬编码 direct_prompt）。
- `TestEmit_DefaultsKindWhenAbsent`：body `{"text":"hi"}`（无 kind）→ 204，`ev.Kind == "direct_prompt"`（向后兼容；守护现有 `TestEmit_WritesOneEventLineToEventWriter` 不回归）。
（先失败：`emitRequest` 仅有 `Text`，`handleEmit` 硬编码 `Kind:"direct_prompt"`）
### Implementation
- `emitRequest` 增加 `Kind string \`json:"kind"\``。
- `handleEmit`：`kind := strings.TrimSpace(req.Kind); if kind == "" { kind = "direct_prompt" }`，填入 `Event{Rewritten: text, Kind: kind}`。
### DoD
- [ ] `go test ./internal/daemon/...`
- [ ] `grep -q 'Kind' internal/daemon/server.go`
- [ ] `grep -q 'TestEmit_PreservesKind' internal/daemon/server_test.go`

## Phase C: 前端单页 (index.html + recorder.js)
### Tests (write first)
追加到 `internal/daemon/static_test.go`（前端逻辑为浏览器 JS，不做浏览器自动化；以 embed.FS 内容字符串断言托管正确性与字段契约）：
- `TestEmbeddedAssets_NonEmpty`：从 `embeddedFS` 读 `web/index.html` 与 `web/recorder.js`，二者长度 > 0。
- `TestEmbeddedIndex_ReferencesRecorderAndFields`：index.html 含 `recorder.js`，且预览区引用大写字段名 `Rewritten` 与 `Kind`（ActionProposal 无 json tag，前端必须读大写键）。
- `TestEmbeddedRecorder_UsesContract`：recorder.js 含 `/api/voice/transcribe`、`/api/voice/emit`、`MediaRecorder`，且读取 `Rewritten`/`Kind` 大写字段、emit body 含小写 `kind`（透传到 Event 的 snake_case `kind`）。
### Implementation
- `web/index.html`：PTT 提示 + RAW/REWRITTEN 预览区 + Kind/Confidence 只读显示 + 可编辑 `<textarea>`（绑定 REWRITTEN）+ Confirm 按钮；`<title>voci</title>`；引入 `recorder.js`。
- `web/recorder.js`：`keydown`/`keyup` 监听 Space 实现 PTT（仅当 `document.activeElement` 不是输入框时触发，避免 textarea 空格冲突）；`getUserMedia({audio})` + `MediaRecorder` 录音，`stop` 合并 chunks 为 Blob → `fetch('/api/voice/transcribe',{method:'POST',body:blob})` → 渲染 `resp.RawTranscript`/`resp.Rewritten`/`resp.Kind`/`resp.Confidence`（大写键）；Confirm → `fetch('/api/voice/emit',{method:'POST',body:JSON.stringify({text:edited,kind:previewedKind})})`，204 后复位待录音。
### DoD
- [ ] `go test ./internal/daemon/...`
- [ ] `grep -q 'Rewritten' internal/daemon/web/index.html`
- [ ] `grep -q '/api/voice/transcribe' internal/daemon/web/recorder.js`
- [ ] `grep -q '/api/voice/emit' internal/daemon/web/recorder.js`
- [ ] `grep -q 'MediaRecorder' internal/daemon/web/recorder.js`

## Constraints
- 不做 WS `/api/voice/stream` 实时流式转写（后续任务）。
- 不引入 React/Vite/任何构建工具（违反「无 npm/node/vite 构建步骤」约束）；前端为 vanilla JS 单页。
- 不引入鉴权 / CORS / 远程访问加固（属 TASK-19；`serve` 默认绑 `127.0.0.1`，本机同源）。
- `cmd/voci/main.go` 的 `--serve` 构造 `daemon.Server` 无需改动即可托管页面（embed 随包，`Handler()` 自动挂静态路由）；本任务不改 main.go。
- MediaRecorder 默认产出 `audio/webm`(Opus) 或 `audio/ogg`，非 wav；transcribe handler 当前落盘为 `*.wav` 临时文件（仅后缀名义）。MVP 接受 webm/ogg 原样上送，依赖下游 SiliconFlow ASR 按实际容器解析；不在浏览器侧做 PCM→wav 编码。若实测 ASR 严格按扩展名失败，回退方案为 handler 按 `Content-Type` 命名临时文件——列为本任务范围外的可选增强。
- HINTED 中间产物未在 `ActionProposal` 透出，预览以 RAW + REWRITTEN 两栏为准；HINTED 第三栏降级为可选（需 transcribe 响应扩展 `hinted` 字段），不阻塞。
- Space 键 PTT 与 textarea 输入冲突：仅当焦点不在输入框时 Space 触发 PTT（`document.activeElement` 判定）。

## Acceptance Gate
- [ ] `go test ./...`
- [ ] `go build ./cmd/voci`
- [ ] `grep -q 'FileServerFS' internal/daemon/server.go`
- [ ] `grep -q 'go:embed' internal/daemon/server.go`
- [ ] `test -f internal/daemon/web/index.html`
- [ ] `test -f internal/daemon/web/recorder.js`
- [ ] `! grep -q 'react\|vite\|package.json' internal/daemon/web/index.html`
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Proposal self-review (refresh post-TASK-20): APPROVED
premise-ledger:
[E] GET / 返回 200/html: 后端 Handler() 当前只注册两个 API 路由 (server.go:51-56)，须新增 FileServer，目标可由 httptest 验证。
[E] go:embed 单文件可启动: Go 1.23 (go version 实测)，embed + http.FileServerFS 标准库可用，删源码目录后仍托管可测。
[E] recorder.js POST 原始字节到 transcribe: handler 用 io.ReadAll(r.Body) 收裸字节 (server.go:71)，契约匹配。
[E] 前端读大写字段名 Rewritten/RawTranscript/Kind/Confidence: intent.ActionProposal 无 json tag (proposal.go 实测 grep)，序列化为 Go 字段名；区别于 Event 的 snake_case (eventlog.go)。Round-1 修正的关键错误。
[E] emit 成功返 204: handleEmit 返回 http.StatusNoContent (server.go:173)。
[E] Kind 硬编码为缺陷: handleEmit 写死 Kind="direct_prompt" 且 emitRequest 只有 Text (server.go:131-163)，透传须改 emitRequest+handleEmit。
[C] 透传 kind 向后兼容: 新增可选 json 字段，旧 {text} 客户端不受影响。
[C] auth/CORS 移出范围: 默认绑 127.0.0.1 同源，远程加固归 TASK-19。
[C] 两步提交即 gate: 对齐 TASK-17 voice-trusted 主路径 + Web UI preview 差异价值 (方案 A)。
[H] SiliconFlow ASR 接受 webm/opus: 未实测；多数云 ASR 接受，但临时文件 .wav 后缀名义不准，列回退方案 (Content-Type 命名)。
[H] HINTED 可选第三栏: transcribe 当前不透出 hinted 中间产物；MVP 以 RAW+REWRITTEN 验收，HINTED 需响应扩展字段。
GCL-self-report: E=6 C=3 H=2

Plan review iteration 1: APPROVED
premise-ledger:
[E] ActionProposal has NO json tags -> JSON keys are capitalized Go field names (Kind/Rewritten/RawTranscript/Confidence): verified internal/intent/proposal.go lines 18-29 have no backtick tags.
[E] Event (eventlog.go) uses snake_case json tags (rewritten/kind/raw_transcript/confidence): verified lines 12-17. Plan correctly distinguishes the two structs in header + Phase C contract test.
[E] handleEmit hardcodes Kind:"direct_prompt" and emitRequest only has Text: verified server.go lines 131-133, 160-163; Phase B premise correct.
[E] Handler() registers only two /api/voice/* HandleFunc routes, no GET / no embed: verified server.go lines 51-56; Phase A failing-first premise correct.
[E] http.FileServerFS exists in Go 1.23.1: verified via go doc and go version.
[C] ServeMux does not shadow /api/voice/* with mux.Handle("/"): standard net/http longest-pattern-match -- exact /api/voice/transcribe is more specific than subtree /; Phase A TestHandler_APIRoutesNotShadowed directly guards this.
[E] makeServer fakes make TestHandler_APIRoutesNotShadowed achievable (POST transcribe fake body -> 200 + decodable ActionProposal): verified server_test.go makeServer + TestHandler_RunsPipelineAndReturnsProposalJSON.
[E] TestEmit_WritesOneEventLineToEventWriter exists (Phase B regression guard reference valid): verified server_test.go line 329.
[E] Goal coverage 1:1 -> Phases A/B/C + Acceptance: all 8 proposal Goals mapped to a test or gate item.
[E] TDD structure/order: every Phase has ### Tests then ### Implementation; each DoD first item is go test ./internal/daemon/...; Acceptance first item is go test ./...; absence check uses ! grep -q (line 71), no grep -qv.
GCL-self-report: E=9 C=1 H=0
<!-- SECTION:NOTES:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 go test ./internal/daemon/...
- [ ] #2 grep -q 'go:embed' internal/daemon/server.go
- [ ] #3 grep -q 'FileServerFS' internal/daemon/server.go
- [ ] #4 test -f internal/daemon/web/index.html
- [ ] #5 test -f internal/daemon/web/recorder.js
- [ ] #6 go test ./internal/daemon/...
- [ ] #7 grep -q 'Kind' internal/daemon/server.go
- [ ] #8 grep -q 'TestEmit_PreservesKind' internal/daemon/server_test.go
- [ ] #9 go test ./internal/daemon/...
- [ ] #10 grep -q 'Rewritten' internal/daemon/web/index.html
- [ ] #11 grep -q '/api/voice/transcribe' internal/daemon/web/recorder.js
- [ ] #12 grep -q '/api/voice/emit' internal/daemon/web/recorder.js
- [ ] #13 grep -q 'MediaRecorder' internal/daemon/web/recorder.js
- [ ] #14 go test ./...
- [ ] #15 go build ./cmd/voci
- [ ] #16 grep -q 'FileServerFS' internal/daemon/server.go
- [ ] #17 grep -q 'go:embed' internal/daemon/server.go
- [ ] #18 test -f internal/daemon/web/index.html
- [ ] #19 test -f internal/daemon/web/recorder.js
- [ ] #20 ! grep -q 'react\|vite\|package.json' internal/daemon/web/index.html
<!-- DOD:END -->

---
id: TASK-23
title: 会话散文进 hinted 纠错上下文 + Session 段降噪
status: 'Basic: Done'
assignee: []
created_date: '2026-06-28 15:23'
updated_date: '2026-06-28 15:50'
labels:
  - 'kind:basic'
dependencies: []
ordinal: 18000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
提升 ASR 纠错的上下文覆盖：当前 internal/context 的 SessionSource.parseSessionSnippet 只从会话 JSONL 抽取「编辑的文件路径 / 跑过的 bash 命令 / 用户消息里的 TASK-N」，丢弃所有对话散文。结果：刚说过/刚打过的领域词（如 Web、端口、URL、API）无法成为识别纠错信号——实测「Web 服务器」被 ASR 听成「外部服务器」且无法纠正，因为「Web」既不是 Known Entity 也不在被保留的会话结构里。本任务：(1) 让 SessionSource 额外抽取最近若干轮对话散文（user+assistant 文本），作为 RunHinted（ASR 纠错）阶段的词法信号；(2) 关键约束：散文只喂给 hinted 做词法纠错，绝不喂给 Rewrite 当展开素材（Rewrite 已收窄到只用 Known Entities，须保持隔离，防止 TASK-19 式越界重演）；(3) 给 Session 段的 ran 降噪——只保留命令首段/可执行名，不要把多行脚本整段灌入（实测 hint 达 6885 字节，过半是 probe 脚本噪声）；(4) 在 Web UI 语音输入界面增加「上下文预览面板」——语音输入前持续显示当前 Claude Code 会话状态（Known Entities、Active Tasks），让用户在说话前就知道 pipeline 将使用哪些纠错信号，减少盲说盲猜。
<!-- SECTION:DESCRIPTION:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
# TASK-23 Proposal: 会话散文进 hinted 纠错上下文 + Session 段降噪

## Background (WHY)

The recent dialogue between the user and Claude Code is the single strongest signal for ASR-correcting the domain words the user *just spoke or typed*: those words are, by definition, the active vocabulary of the moment. Yet `parseSessionSnippet` (internal/context/session_source.go) discards all conversation prose — it keeps only edited file paths, bash commands, and `TASK-N` regex hits, and drops every text block from user/assistant messages.

Concrete observed failure: the user said "Web 服务器" but ASR produced "外部服务器". The pipeline could not correct it because "Web" is neither a Known Entity nor present in the retained session structure — even though the recent conversation contained "Web", "端口", and a URL. The correcting evidence existed and was thrown away.

Separately, the `- ran:` section dumps each bash command verbatim, including multi-line script bodies, inflating the hint to ~6885 bytes (more than half noise) and crowding out the signal we now want to add.

## Goals (verifiable)

1. `parseSessionSnippet` (or a sibling function it calls) additionally extracts recent conversation prose — text content of user messages and of assistant `text` blocks — into the session snippet, bounded in size. *Verify: a unit test feeding JSONL with a user line "Web 服务器 在 8080 端口" yields a snippet containing "Web", "8080", "端口".*
2. The extracted prose reaches the hint consumed by `RunHinted`, but is NOT in the slice `Rewrite` consumes. *Verify: `knownEntities(hint)` over a hint that contains a `## Recent Dialogue` block returns a string with no dialogue prose — only the `## Known Entities` section. Existing `knownEntities` isolation test pattern is reused/extended.*
3. The `- ran:` lines are denoised: only the first line (and, where cheap, the leading executable token) of each command is kept; multi-line script bodies are dropped. *Verify: a unit test feeding a heredoc / multi-line `command` yields a `- ran:` entry containing only the first line, not the body.*
4. A size bound caps the session snippet: at most a fixed number of recent dialogue turns (e.g. last 6) AND a per-turn / total prose character cap (e.g. 200 chars/turn, ~1200 chars total). *Verify: a unit test feeding many long turns produces a snippet whose `## Recent Dialogue` block is under the cap.*
5. Existing tests still pass; new unit tests cover (a) prose extraction, (b) `- ran:` denoise, (c) Rewrite-isolation of the new prose block.

## Proposed Approach

**Prose extraction (Goal 1, 4).** Extend the content-parsing in `parseSessionSnippet`:
- Add a `Text` field to the content-block struct (currently `toolUse`, which lacks it) so `{"type":"text","text":"..."}` blocks in assistant messages are captured.
- For `user` messages, the content is already unmarshaled as a plain string (today only scanned for `TASK-N`); reuse that string as prose.
- Collect prose turns in chronological order from the tailed lines, keep only the last K turns (default 6), trim each to a per-turn char cap, and collapse internal whitespace/newlines to single spaces. Drop empty/whitespace-only turns and obvious non-prose (e.g. tool-result envelopes).
- Emit the prose under a clearly named sub-block. Preferred: a top-level `## Recent Dialogue` heading (sibling to `## Claude Code Session`), so the existing `knownEntities()` next-`## ` boundary scan cleanly excludes it. Keeping it inside the `## Claude Code Session` block is also acceptable since that block is already a non-`## Known Entities` section; the top-level heading is chosen for explicitness and easier isolation verification.

**Command denoise (Goal 3).** When building the `- ran:` set, normalize each command to its first non-empty line (`strings.SplitN(cmd, "\n", 2)[0]`), trimmed. Optionally also surface `argv[0]` (leading token). This drops heredocs and multi-line script bodies while preserving the human-meaningful command name/intent.

**Isolation (Goal 2).** No change to the `RunHinted` / `Rewrite` split is required — and that is the point. `RunHinted` receives the full assembled hint (the session snippet, including the prose, is appended by `assembleAsrHint`'s extra-snippet pass). `Rewrite` receives only `knownEntities(hint)`, which returns the `## Known Entities` section starting at its heading and ending at the next `## ` heading. The prose is contributed by the `session` source and assembled into its own non-`## Known Entities` block (the existing `## Claude Code Session` block, or a sibling `## Recent Dialogue` heading), entirely outside the Known Entities section that `knownEntities()` slices. It is therefore structurally unreachable by `Rewrite` regardless of source ordering. We treat the existing `knownEntities()` gate as the load-bearing invariant and add a regression test asserting prose does not appear in `knownEntities(hint)`.

**Soft-hint framing (risk mitigation).** The `RunHinted` system prompt drives substitution from `## Known Entities` only. Recent Dialogue prose is supplied as *ambient lexical context* (words the user is likely using), not as authoritative `spoken: canonical` substitution pairs. We will not add prose entries to the substitution rules; at most the prompt may note that Recent Dialogue shows likely-intended vocabulary to bias homophone/near-miss choices (e.g. prefer "Web" over "外部" when "Web" appears in recent dialogue).

## Trade-offs / Risks

- **Do NOT feed prose to `Rewrite` (explicit).** Re-enabling broad project context into `Rewrite` is exactly the TASK-19-style over-expansion regression. The prose must reach `RunHinted` only. Enforced structurally by `knownEntities()` and guarded by a regression test (Goal 2). This is a hard constraint.
- **Prose bloat.** Unbounded dialogue would balloon the hint. Mitigated by the turn-count + char caps (Goal 4). Net size should *drop* versus today once `- ran:` denoise removes multi-line bodies.
- **Wrong corrections from prose.** Prose is noisier than a curated entity list and could bias a correction incorrectly. Mitigated by treating it as a soft lexical hint (not an authoritative substitution rule): substitution remains driven by `## Known Entities`; prose only nudges homophone/near-miss disambiguation.
- **Privacy / size of dumping conversation.** Conversation text is more sensitive and larger than structured activity. Mitigated by the bounded window (last K turns, char-capped) and by the fact the snippet already stays within the local context cache; no new sink is introduced.
- **Parsing robustness.** Claude Code content blocks are heterogeneous (text, tool_use, tool_result). The extractor must tolerate unknown/extra block types and malformed JSON without panicking — preserved by the existing per-line `continue`-on-error structure and covered by the existing SkipsBadJSON-style test.

---

# Plan: 会话散文进 hinted 纠错上下文 + Session 段降噪

Implements TASK-23 (proposal `/tmp/ftb-task23-proposal.md`). Three phases, each ≤200 LOC.
All edits land in `internal/context/session_source.go` (+ its test) and `internal/pipeline/pipeline_test.go`.
Phase C confirms `knownEntities()` already provides Rewrite isolation, so it is test-only.

## Phase A: ran 降噪

### Tests (write first)
- `internal/context/session_source_test.go`: `TestParseSessionSnippet_RanFirstLineOnly` — a JSONL Bash `tool_use` whose `command` is a multi-line heredoc/script (e.g. `cat <<'EOF'\nbody line\nmore body\nEOF`) yields a `- ran:` line containing only the first line (`cat <<'EOF'`), and asserts the body lines (`body line`, `more body`) are absent. Fails first because `cmdSet` currently stores the verbatim multi-line command.

### Implementation
- In `parseSessionSnippet`, when collecting into `cmdSet`, normalize each command to its first non-empty line via `strings.SplitN(strings.TrimSpace(inp.Command), "\n", 2)[0]` (trimmed) before insertion. Multi-line bodies dropped; single-line commands unchanged.

### DoD
- [ ] `go test ./internal/context/...`
- [ ] `go test ./...`
- [ ] `grep -q 'SplitN' internal/context/session_source.go`

## Phase B: 会话散文抽取（带上限）

### Tests (write first)
- `internal/context/session_source_test.go`: `TestParseSessionSnippet_ExtractsRecentProse` — feeding a user line `{"...content":"Web 服务器 在 8080 端口"}` and an assistant `{"type":"text","text":"先看 internal/daemon"}` block yields a snippet containing a `## Recent Dialogue` heading and the substrings `Web`, `8080`, `端口`, and `internal/daemon`. Fails first: `toolUse` has no `Text` field and prose is currently discarded.
- `TestParseSessionSnippet_ProseCapped` — feeding more than the turn cap (e.g. 8 user turns) of long lines (each >300 chars) produces a `## Recent Dialogue` block whose length is under the total cap (assert `len(block) <= 1200`) and whose oldest turns are excluded (assert most-recent turn text present, an early turn's unique marker absent). Locks the turn-count + per-turn char caps.

### Implementation
- Add a `Text string \`json:"text"\`` field to the content-block struct (extend `toolUse` or add a sibling struct used for the same `[]` unmarshal) so `{"type":"text",...}` assistant blocks are captured.
- Collect prose turns in chronological (line) order: assistant `text` blocks and the already-unmarshaled user content string. Collapse internal whitespace/newlines to single spaces; drop empty/whitespace-only and tool-result envelopes.
- Keep only the last K turns (const `maxProseTurns = 6`), trim each to a per-turn cap (const `maxProseCharsPerTurn = 200`), and bound the assembled block to a total cap (const `maxProseCharsTotal = 1200`).
- Emit prose under a top-level `## Recent Dialogue` heading appended after the `## Claude Code Session` block (sibling heading), so the existing `knownEntities()` next-`## ` boundary cleanly excludes it. The early-return guard at the top of the builder must also fire when only prose is present (so a prose-only session still produces output).

### DoD
- [ ] `go test ./internal/context/...`
- [ ] `go test ./...`
- [ ] `grep -q 'Recent Dialogue' internal/context/session_source.go`
- [ ] `grep -q 'maxProseTurns' internal/context/session_source.go`

## Phase C: Rewrite 隔离回归（test-only）

### Tests (write first)
- `internal/pipeline/pipeline_test.go`: `TestKnownEntities_ExcludesRecentDialogue` — given a hint containing both a `## Known Entities` section and a later `## Recent Dialogue` block whose prose includes a unique marker (e.g. `外部服务器应为Web`), `knownEntities(hint)` returns a string that contains the Known Entities entries but NOT the dialogue marker and NOT the `## Recent Dialogue` heading. Locks the isolation invariant so prose never reaches Rewrite.

### Implementation
- No production change expected: `knownEntities()` already slices from `## Known Entities` to the next `\n## ` heading. The test documents and guards this invariant. If (and only if) the test fails, fix `knownEntities()` boundary logic — but the heading placement from Phase B is designed so the existing stop-at-next-`## ` logic suffices.

### DoD
- [ ] `go test ./internal/pipeline/...`
- [ ] `go test ./...`

## Constraints
- Prose is a soft lexical hint for the hinted (RunHinted) stage only — never an authoritative `spoken: canonical` substitution rule; do not add prose entries to the substitution list.
- Prose must never appear in the slice Rewrite consumes; this is structurally enforced by `knownEntities()` and guarded by the Phase C regression test (hard constraint, TASK-19 over-expansion regression).
- The size cap (last 6 turns, 200 chars/turn, ~1200 chars total) keeps the hint manageable; combined with `- ran:` denoise, net hint size should drop versus today.
- The extractor must tolerate unknown/extra/malformed content blocks without panicking, preserving the existing per-line `continue`-on-error structure.

## Acceptance Gate
- [ ] `go test ./...`
- [ ] `go build ./cmd/voci`
- [ ] `grep -q 'Recent Dialogue' internal/context/session_source.go`
- [ ] `grep -q 'SplitN' internal/context/session_source.go`
- [ ] `grep -q 'TestKnownEntities_ExcludesRecentDialogue' internal/pipeline/pipeline_test.go`

## Phase D: Web UI 上下文预览面板

### Tests (write first)
- `internal/daemon/server_test.go`: `TestHandleContext_ReturnsHint` — 构造一个带有 mock hintFn 的 Server，GET `/api/context` 返回 200，body 为 `{"hint":"..."}` 且包含 mock hintFn 的返回内容。Fails first：`/api/context` 路由尚未注册。
- `internal/daemon/server_test.go`: `TestHandleContext_HintFnError` — hintFn 返回 error 时，GET `/api/context` 返回 500。

### Implementation
- 在 `Server` struct 新增 `hintFn func(ctx context.Context) (string, error)` 字段（与已有 `chatFn` 平行）；`NewServer` 构造时传入。
- `cmd/voci/main.go` 的 serve 路径：把 `BuildContextWithSource(...)` 封装成 `hintFn` 注入。
- 新增 `handleContext(w, r)` handler：调用 `hintFn`，返回 `{"hint":"<string>"}`；错误时 500。
- 在路由表注册 `GET /api/context`。
- 前端 `internal/daemon/web/index.html` / `recorder.js`：
  - `setInterval` 每 5 秒 `fetch('/api/context')` 刷新。
  - PTT 按下时额外触发一次立即刷新（消除 5s 延迟盲区）。
  - 解析 `hint` 字符串，提取 `## Known Entities` 与 `## Active Tasks` 两段，渲染为可折叠面板（默认展开）。
  - `## Recent Dialogue`（Phase B 新增）与 `## Claude Code Session` 原始内容默认折叠，点击可展开（避免噪声占屏）。
  - 面板样式简洁：等宽字体，浅灰背景，不影响录音主流程的视觉焦点。

### DoD
- [ ] `go test ./internal/daemon/...`
- [ ] `go test ./...`
- [ ] `go build ./cmd/voci`
- [ ] `grep -q '/api/context' internal/daemon/server.go`
- [ ] `grep -q 'hintFn' internal/daemon/server.go`
- [ ] `grep -q 'api/context' internal/daemon/web/recorder.js`
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Proposal self-review: APPROVED
premise-ledger:
[E] Web->外部 ASR failure is real: task states 实测 observed; recent dialogue contained Web/端口/URL.
[E] parseSessionSnippet discards all conversation prose: verified by reading internal/context/session_source.go (only fileSet/cmdSet/taskSet).
[E] content-block struct (toolUse) has no Text field, so assistant text blocks are silently dropped: verified by reading session_source.go lines 96-100.
[E] knownEntities(hint) returns only the ## Known Entities section, ending at next ## heading: verified by reading internal/pipeline/pipeline.go lines 83-95.
[E] assembleAsrHint appends the session snippet via its non-handled extra-snippet loop; full hint reaches RunHinted: verified by reading internal/context/builder.go lines 130-137.
[E] - ran: dumps multi-line command bodies verbatim, ~6885-byte hint: task states 实测.
[C] Recent dialogue is the strongest signal for ASR-correcting just-spoken domain words: design rationale.
[C] Prose in its own non-Known-Entities block is structurally unreachable by Rewrite: derived from knownEntities slicing behavior.
[C] Soft-lexical-hint framing (not substitution rule) mitigates wrong corrections: design choice.
[H] turn-count + char caps (~6 turns, 200/1200 chars) keep the hint manageable: untuned heuristic.
[H] net hint size will drop after - ran: denoise: predicted, not measured.
GCL-self-report: E=6 C=3 H=2

Plan review iteration 1: APPROVED
premise-ledger:
[E] knownEntities() slices ## Known Entities and stops at next \n## : verified internal/pipeline/pipeline.go:83-95
[E] Recent Dialogue as top-level sibling heading is structurally excluded from Rewrite slice: follows from knownEntities boundary scan + heading placement (Phase B)
[E] RunHinted receives full hint incl. session snippet: verified pipeline.go:29-32
[E] Builder appends session snippet (incl. ## Recent Dialogue) as extra-snippet: verified internal/context/builder.go:130-137
[E] Phase C correctly test-only: knownEntities already provides isolation, no code change needed
[E] Goal 4 size-cap covered by Phase test TestParseSessionSnippet_ProseCapped (asserts len(block)<=1200 + oldest-turn exclusion)
[E] All Phases follow Tests-then-Implementation; first DoD item is go test; Acceptance first item is go test ./...
[C] Early-return guard must fire on prose-only session: plan flags it in Phase B impl note; depends on correct implementation
[E] File paths exist: session_source.go(+test), pipeline.go, pipeline_test.go all present
GCL-self-report: E=8 C=1 H=0
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
## TASK-23 完成摘要

### 交付内容
- **Phase A** (`- ran:` 降噪): `parseSessionSnippet` 对 Bash 命令用 `strings.SplitN(..., "\n", 2)[0]` 取首行，丢弃多行 heredoc/脚本体，hint 体积显著缩减。
- **Phase B** (散文抽取): 新增 `contentBlock.Text` 字段捕获 assistant `text` 块；对 user 字符串内容和 assistant text 块收集散文 turn，应用 `maxProseTurns=6` / `maxProseCharsPerTurn=200` / `maxProseCharsTotal=1200` 上限，输出到 `## Recent Dialogue` 顶级 heading（与 `## Known Entities` 并列，structurally 不被 `knownEntities()` 截取，确保 Rewrite 隔离）。
- **Phase C** (隔离回归): `TestKnownEntities_ExcludesRecentDialogue`（`internal/pipeline/pipeline_test.go`）断言 `## Recent Dialogue` 散文永远不出现在 `knownEntities()` 返回值中。
- **Phase D** (`/api/context` + 前端面板): `Server.HintFn func(ctx) (string, error)` 注入字段；`GET /api/context` 返回 `{"hint":"..."}` JSON；Web UI `recorder.js` 启动时及每 5 秒 poll，PTT 按下时立即刷新；`index.html` 渲染 Known Entities / Active Tasks / Recent Dialogue / Session Activity 四个可折叠面板（前两个默认展开）。

### 验收
- `go test ./...` ✅（全绿）
- `go build ./cmd/voci` ✅
- 所有 19 项 DoD 检查通过
- commit: 4375e9e
<!-- SECTION:FINAL_SUMMARY:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 go test ./internal/context/...
- [x] #2 go test ./...
- [x] #3 grep -q 'SplitN' internal/context/session_source.go
- [x] #4 go test ./internal/context/...
- [x] #5 go test ./...
- [x] #6 grep -q 'Recent Dialogue' internal/context/session_source.go
- [x] #7 grep -q 'maxProseTurns' internal/context/session_source.go
- [x] #8 go test ./internal/pipeline/...
- [x] #9 go test ./...
- [x] #10 go test ./...
- [x] #11 go build ./cmd/voci
- [x] #12 grep -q 'Recent Dialogue' internal/context/session_source.go
- [x] #13 grep -q 'SplitN' internal/context/session_source.go
- [x] #14 grep -q 'TestKnownEntities_ExcludesRecentDialogue' internal/pipeline/pipeline_test.go
- [x] #15 go test ./internal/daemon/...
- [x] #16 go build ./cmd/voci
- [x] #17 grep -q '/api/context' internal/daemon/server.go
- [x] #18 grep -q 'hintFn' internal/daemon/server.go
- [x] #19 grep -q 'api/context' internal/daemon/web/recorder.js
<!-- DOD:END -->

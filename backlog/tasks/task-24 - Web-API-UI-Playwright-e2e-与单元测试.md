---
id: TASK-24
title: Web API + UI Playwright e2e 与单元测试
status: 'Basic: Done'
assignee: []
created_date: '2026-06-28 22:39'
updated_date: '2026-06-28 23:11'
labels:
  - 'kind:basic'
dependencies: []
ordinal: 19000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
为 voci Web API（/api/voice/transcribe、/api/voice/emit、/api/context）和前端 UI（index.html + recorder.js）补充 Playwright 测试，覆盖当前空白的 JS 逻辑单元、前后端联动 e2e、以及 XSS/边界防御场景。当前 Go 层已有 44 个 server_test.go 单测和 3 个 e2e_test.go（httptest），但 recorder.js 的 renderDialogue、extractSection、escHtml、UI 状态机、Confirm/Cancel 流程等均无任何测试。
<!-- SECTION:DESCRIPTION:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
# Plan: Web API + UI Playwright e2e 与单元测试

## Context

Go 单元测试（server_test.go + e2e_test.go，build tag `e2e`）已覆盖 HTTP 状态码、JSON 契约、EventWriter 写入行为等后端逻辑。缺失的是**前端行为验证**：renderDialogue 的 `<details>` 折叠规则、escHtml XSS 防护、extractSection 的 heading 切割、四个 `<details>` panel 的初始 open/closed 状态、以及 Confirm/Cancel 交互流程。这些都是 JS 逻辑，Go 测试无法覆盖。Playwright 在真实 Chromium 中执行，可以直接断言 DOM 状态和网络请求体，是填补这一空白的最小代价方案。

## Phase 1: Playwright 环境搭建

在项目根下新建 `e2e/` 目录，初始化独立 Node.js 包，安装 `@playwright/test`，配置 `playwright.config.ts` 指向 Chromium only。服务层用 `globalSetup` 启动一个真实 Go httptest 服务：在 `internal/daemon/` 下新增 `playwright_setup_test.go`（build tag `playwright`）中新增 `TestPlaywrightSetup`，该测试启动 `httptest.NewServer`，把 URL 写入环境变量指定的临时文件，然后阻塞等待 `PLAYWRIGHT_DONE` 文件出现；`globalSetup.ts` 调用 `go test -run TestPlaywrightSetup -tags playwright -timeout 30m ./internal/daemon/ -v`（后台），从临时文件读取 URL 并写入 `process.env.BASE_URL`，供所有测试访问；`globalTeardown.ts` 写入 `PLAYWRIGHT_DONE` 文件让 Go 进程退出。

### DoD
- [ ] `test -f e2e/package.json`
- [ ] `test -f e2e/playwright.config.ts`
- [ ] `test -f e2e/globalSetup.ts`
- [ ] `test -f e2e/globalTeardown.ts`
- [ ] `test -f /home/yale/work/voci/internal/daemon/playwright_setup_test.go`
- [ ] `cd e2e && npm install && npx playwright install chromium --with-deps 2>&1 | grep -qi 'chromium'`

## Phase 2: JS 逻辑单元测试（route mock，无真实音频）

新建 `e2e/tests/context-panel.spec.ts`。所有测试通过 `page.route('/api/context', ...)` 返回精心构造的 hint 字符串，不依赖真实录音。测试加载 `/`（静态页面），等待 `#ctx-known-body` 文本稳定后断言。

Key scenarios:
- **renderDialogue — U: 行内联**：hint 含 `## Recent Dialogue\nU: hello`，`#ctx-dialogue-body` 包含 `<div class="dialogue-turn">` 且无 `<details>`
- **renderDialogue — 短 A: 行内联**：A: 行内容 ≤120 字符，不产生 `<details>`
- **renderDialogue — 长 A: 折叠**：A: 行内容 >120 字符，产生 `<details class="dialogue-turn"><summary>` 且摘要以 `…` 结尾
- **escHtml XSS 防护**：hint 含 `<script>alert(1)</script>`，页面中出现 `&lt;script&gt;` 而非实际 `<script>` 标签
- **extractSection — Known Entities**：hint 含 `## Known Entities\nfoo\n## Active Tasks\nbar`，`#ctx-known-body` 文本为 `foo`，`#ctx-tasks-body` 为 `bar`
- **四个 panel 初始状态**：`#ctx-known` 和 `#ctx-tasks` 有 `open` 属性；`#ctx-dialogue` 和 `#ctx-session` 无 `open` 属性
- **panel toggle**：点击 `#ctx-dialogue summary`，`#ctx-dialogue` 获得 `open` 属性

### DoD
- [ ] `grep -q 'renderDialogue' e2e/tests/context-panel.spec.ts`
- [ ] `grep -q 'extractSection\|Known Entities' e2e/tests/context-panel.spec.ts`
- [ ] `grep -q 'escHtml\|&lt;\|XSS\|script' e2e/tests/context-panel.spec.ts`
- [ ] `grep -q 'dialogue-turn\|details' e2e/tests/context-panel.spec.ts`
- [ ] `grep -q 'ctx-known.*open\|open.*ctx-known\|getAttribute.*open' e2e/tests/context-panel.spec.ts`

## Phase 3: Confirm/Cancel 流程测试（route mock）

新建 `e2e/tests/voice-flow.spec.ts`。mock `/api/voice/transcribe` 返回 `ActionProposal` JSON，mock `/api/voice/emit` 捕获请求体。测试通过 `page.evaluate` 直接调用 `fetch('/api/voice/transcribe', ...)` 触发 `sendAudio` 等效路径，绕过 MediaRecorder，而不依赖麦克风权限。

Key scenarios:
- **preview 出现**：route `/transcribe` 返回 `{RawTranscript:"raw",Rewritten:"rewritten",Kind:"direct_prompt",Confidence:0.9}`，触发 fetch 后 `#preview` `display` 变为非 `none`，`#raw` 含 `raw`，`#rewritten` 值为 `rewritten`，`#kind` 含 `direct_prompt`，`#confidence` 含 `90%`
- **编辑 Rewritten → Confirm 发送修改后文本**：填写 `#rewritten` 为 `edited text`，点击 `#confirm`，断言 `/emit` 收到 `{"text":"edited text","kind":"direct_prompt"}`
- **Confirm → 204 → reset**：`/emit` route 返回 204，点击后 `#preview` 隐藏，`#status` 回到 `Hold Space to record`
- **Confirm → 非 204 → 错误提示**：`/emit` route 返回 500，`#status` 文本含 `Emit failed`
- **Cancel → reset**：preview 可见时点击 `#cancel`，`#preview` 隐藏，`#rewritten` 清空
- **空 Rewritten → Confirm 无效**：清空 `#rewritten` 后点击 `#confirm`，不发出 `/emit` 请求

### DoD
- [ ] `grep -q 'confirm\|Confirm' e2e/tests/voice-flow.spec.ts`
- [ ] `grep -q 'emit' e2e/tests/voice-flow.spec.ts`
- [ ] `grep -q 'preview' e2e/tests/voice-flow.spec.ts`
- [ ] `grep -q 'cancel\|Cancel' e2e/tests/voice-flow.spec.ts`
- [ ] `grep -q 'Emit failed\|500\|non-204' e2e/tests/voice-flow.spec.ts`
- [ ] `grep -q 'empty\|trim\|blank' e2e/tests/voice-flow.spec.ts`

## Phase 4: 前后端联动 e2e（真实 Go httptest server）

新建 `e2e/tests/integration.spec.ts`。使用 Phase 1 中 `globalSetup` 启动的真实 Go server（`BASE_URL` 环境变量）。测试不 mock 任何网络，直接验证静态资源和 `/api/context` 的真实响应结构。

Key scenarios:
- **GET / 返回 index.html**：响应状态 200，页面 `<title>` 含 `voci`，`<h1>` 含 `voci`
- **GET /recorder.js 返回脚本**：`page.goto(BASE_URL + '/recorder.js')` 状态 200，body 含 `renderDialogue`
- **页面加载触发 /api/context**：等待 network idle 后，`#ctx-known-body` 不再显示 `(loading…)`（即 /api/context 已被调用并渲染）
- **/api/context JSON 结构**：直接 `fetch('/api/context')` 返回含 `hint` 字段的 JSON 对象（字段存在，类型为 string）

### DoD
- [ ] `grep -q 'globalSetup\|BASE_URL\|baseURL' e2e/playwright.config.ts`
- [ ] `grep -q 'integration\|api/context\|recorder.js' e2e/tests/integration.spec.ts`
- [ ] `grep -q 'voci' e2e/tests/integration.spec.ts`
- [ ] `grep -q 'loading\|hint' e2e/tests/integration.spec.ts`

## Constraints

- Do NOT mock MediaRecorder for PTT keyboard (Space keydown/keyup) tests — these require microphone browser permissions; leave PTT flow for manual testing under docs/manual-verification/
- Do NOT duplicate Go server_test.go / e2e_test.go API contract tests (HTTP status codes, JSON shape, EventWriter behavior) — Playwright tests focus exclusively on frontend DOM behavior
- Playwright tests live in `e2e/` (Node.js package), entirely separate from Go test files; do not add `.ts` files under `internal/`
- Use Chromium only — no cross-browser matrix needed for this project
- All Phase 2 and Phase 3 tests must be runnable offline via `page.route()` mocks; only Phase 4 integration tests require the Go server
- The Go helper file for Phase 4 uses build tag `playwright` (not `e2e`, which is already used by existing `e2e_test.go`) to avoid collisions

## Acceptance Gate

- [ ] `cd /home/yale/work/voci/e2e && npx playwright test --reporter=line 2>&1 | tee /tmp/pw-result.txt && grep -q 'passed' /tmp/pw-result.txt`
- [ ] `! grep -q ' failed' /tmp/pw-result.txt`
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Plan review iteration 1: NEEDS_REVISION — two issues fixed before approval:
1. Acceptance Gate `grep -vq 'failed'` was a broken absence check (always exits 0); replaced with `tee /tmp/pw-result.txt` + `! grep -q ' failed' /tmp/pw-result.txt`.
2. globalSetup `go test` subprocess had no `-timeout` flag; Go test default is 10 min which may not suffice for a full Playwright suite; added `-timeout 30m`.
Plan updated in /tmp/ttb-plan.md.

Plan review iteration 2: APPROVED

cap:propose=approved

claimed: 2026-06-28T22:53:33Z

claimed: 2026-06-28T22:53:44Z

Phase 1 ✓ 2026-06-28T00:00:00Z - e2e/ Node.js package, playwright.config.ts, globalSetup.ts, globalTeardown.ts, playwright_setup_test.go (build tag playwright) — Go httptest server starts via -count=1 flag to bypass cache

Phase 2 ✓ 2026-06-28T00:00:00Z - context-panel.spec.ts: 7 tests covering renderDialogue U:/A: inline, long A: collapsible, escHtml XSS, extractSection, 4-panel open/closed state, toggle

Phase 3 ✓ 2026-06-28T00:00:00Z - voice-flow.spec.ts: 6 tests covering preview display, confirm with edit, 204 reset, 500 error, cancel reset, empty guard

Phase 4 ✓ 2026-06-28T00:00:00Z - integration.spec.ts: 4 tests against real Go httptest server — index.html title, recorder.js serve, context poll, /api/context JSON structure

DoD #1: PASS — test -f e2e/package.json

DoD #2: PASS — test -f e2e/playwright.config.ts

DoD #3: PASS — test -f e2e/globalSetup.ts

DoD #4: PASS — test -f e2e/globalTeardown.ts

DoD #5: PASS — test -f internal/daemon/playwright_setup_test.go

DoD Phase2: PASS — all 5 grep checks pass

DoD Phase3: PASS — all 6 grep checks pass

DoD Phase4: PASS — all 4 grep checks pass

Acceptance Gate: PASS — 17 passed, 0 failed

workerLoop DoD #6: PASS — cd e2e && npm install && npx playwright install chromium --with-deps (chromium already installed, force-verified)
workerLoop DoD #22: PASS — npx playwright test --reporter=line: 17 passed (10.2s)
workerLoop DoD #23: PASS — no 'failed' in output

Completed: 2026-06-28T23:11:01Z

## Execution Summary
Result: Done
Commit: ba28d0bd883ce9bbac019bab6bc74de708867d7a
Tests: 17 Playwright tests passed (10.2s)
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
## Execution Summary\nResult: Done\nCommit: 17eb426 (chore: .gitignore) / f97ceec (feat: Playwright tests)\n\n17 Playwright tests added across 3 specs, all passing:\n- context-panel.spec.ts (7): renderDialogue, escHtml XSS, extractSection, panel state, toggle\n- voice-flow.spec.ts (6): confirm/cancel flow, 204 reset, 500 error, empty guard\n- integration.spec.ts (4): real Go httptest server, index.html, recorder.js, /api/context JSON\n\nGo helper: internal/daemon/playwright_setup_test.go (build tag: playwright)\nKey fix: go test -count=1 required to bypass test cache in globalSetup.ts
<!-- SECTION:FINAL_SUMMARY:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 test -f e2e/package.json
- [ ] #2 test -f e2e/playwright.config.ts
- [ ] #3 test -f e2e/globalSetup.ts
- [ ] #4 test -f e2e/globalTeardown.ts
- [ ] #5 test -f /home/yale/work/voci/internal/daemon/playwright_setup_test.go
- [ ] #6 cd e2e && npm install && npx playwright install chromium --with-deps 2>&1 | grep -qi 'chromium'
- [ ] #7 grep -q 'renderDialogue' e2e/tests/context-panel.spec.ts
- [ ] #8 grep -q 'extractSection\|Known Entities' e2e/tests/context-panel.spec.ts
- [ ] #9 grep -q 'escHtml\|&lt;\|XSS\|script' e2e/tests/context-panel.spec.ts
- [ ] #10 grep -q 'dialogue-turn\|details' e2e/tests/context-panel.spec.ts
- [ ] #11 grep -q 'ctx-known.*open\|open.*ctx-known\|getAttribute.*open' e2e/tests/context-panel.spec.ts
- [ ] #12 grep -q 'confirm\|Confirm' e2e/tests/voice-flow.spec.ts
- [ ] #13 grep -q 'emit' e2e/tests/voice-flow.spec.ts
- [ ] #14 grep -q 'preview' e2e/tests/voice-flow.spec.ts
- [ ] #15 grep -q 'cancel\|Cancel' e2e/tests/voice-flow.spec.ts
- [ ] #16 grep -q 'Emit failed\|500\|non-204' e2e/tests/voice-flow.spec.ts
- [ ] #17 grep -q 'empty\|trim\|blank' e2e/tests/voice-flow.spec.ts
- [ ] #18 grep -q 'globalSetup\|BASE_URL\|baseURL' e2e/playwright.config.ts
- [ ] #19 grep -q 'integration\|api/context\|recorder.js' e2e/tests/integration.spec.ts
- [ ] #20 grep -q 'voci' e2e/tests/integration.spec.ts
- [ ] #21 grep -q 'loading\|hint' e2e/tests/integration.spec.ts
- [ ] #22 cd /home/yale/work/voci/e2e && npx playwright test --reporter=line 2>&1 | tee /tmp/pw-result.txt && grep -q 'passed' /tmp/pw-result.txt
- [ ] #23 ! grep -q ' failed' /tmp/pw-result.txt
<!-- DOD:END -->

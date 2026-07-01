---
id: TASK-72
title: Web UI 引入 esbuild 构建流水线以支持完整 GFM Markdown 渲染
status: 'Basic: Backlog'
assignee: []
created_date: '2026-07-01 06:46'
updated_date: '2026-07-01 06:57'
labels:
  - 'kind:basic'
dependencies: []
ordinal: 43000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
当前 recorder.js 是 557 行零依赖 vanilla JS，mdToHtml 只支持行内 code/bold/italic，无法渲染 Claude Code 输出的表格、代码块、有序列表等 GFM 语法。引入 esbuild 作为前端构建步骤，bundle marked.js + DOMPurify 进 Go embed，使 Web UI 能完整渲染 GFM。
<!-- SECTION:DESCRIPTION:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
# Proposal: Web UI 引入 esbuild 构建流水线以支持完整 GFM Markdown 渲染

## Background

voci 的 Web UI 在 `--serve` 模式下显示 Claude Code 的对话历史，而 Claude Code 的输出大量使用 GFM（GitHub Flavored Markdown）——包含代码块、有序/无序列表、表格等。当前 UI 内嵌了一个手写的 `mdToHtml` 函数（`recorder.js` L183-195），仅支持行内代码、加粗、斜体三种格式；表格、代码块、列表均以纯文本形式展示，严重影响可读性。根本原因是 `recorder.js` 是一个无构建步骤的单文件脚本，无法通过 `import` 引用第三方解析库。引入 esbuild 构建流水线可在不改变 Go 嵌入方式（`//go:embed web/*`）的前提下，将成熟的 Markdown 库打包进单文件产物，彻底解决该问题。

## Goals

1. Web UI 的对话消息区域能正确渲染全部 CommonMark + GFM 元素：段落、标题、有序/无序列表、代码块（含语言标注）、行内代码、粗体、斜体、表格、分隔线、链接。
2. 所有 Markdown 渲染结果须经 DOMPurify 净化，确保不引入 XSS 攻击面。
3. `go build` 产物仍为单一无外部依赖的二进制文件；`//go:embed web/*` 嵌入的 JS 文件由 esbuild 预先打包，不需要运行时网络访问。
4. 新增 `make build-web` 目标，输出 `internal/daemon/web/recorder.bundle.js`；`make build` 将 `build-web` 列为前置依赖，确保每次 `go build` 前 bundle 已是最新。
5. 现有 Playwright E2E 套件新增表格渲染测试用例，至少验证包含 GFM 表格的消息在 DOM 中产生 `<table>` 元素，并与现有测试共存于 `e2e/tests/` 目录。

## Proposed Approach

**构建侧**：在 repo 根目录新增 `package.json`，声明 `esbuild`、`marked`、`dompurify` 为 devDependencies。编写入口文件 `internal/daemon/web/recorder.src.js`，将现有 `recorder.js` 的逻辑迁移过来，用 `import { marked } from 'marked'` 和 `import DOMPurify from 'dompurify'` 替换手写的 `mdToHtml`。esbuild 以 `--bundle --format=iife --platform=browser` 将入口文件及其依赖打包成 `internal/daemon/web/recorder.bundle.js`。

**嵌入侧**：`server.go` 的 `//go:embed` 指令修改为只嵌入 `index.html` 和 `recorder.bundle.js`，忽略源文件 `recorder.src.js`。`index.html` 中的 `<script>` 标签改为引用 bundle 文件。

**构建集成**：Makefile 新增 `build-web` 目标，执行 `npm ci && npx esbuild ...`；`build` 目标将 `build-web` 列为 Makefile 依赖（`build: build-web`），再运行 `go build`。CI 环境中 Node.js 已可用（e2e 套件已依赖），无需额外环境配置。

**安全**：marked.js 默认输出原始 HTML；所有 `marked.parse()` 结果必须经 `DOMPurify.sanitize()` 处理后再赋给 `innerHTML`，与当前手写 `esc()` 方案保持同等安全级别。

**测试**：在 `e2e/tests/` 下新增一个 Playwright 测试文件，构造包含 GFM 表格的 `/api/context` mock 响应，断言 DOM 中存在 `<table>` 元素，并验证 DOMPurify 清除了 `<script>` 标签。

## Trade-offs and Risks

**未做的事**：不引入 CSS-in-JS、PostCSS 或 TypeScript 编译步骤，构建链保持最小。不对 Go 代码做任何渲染逻辑——服务端仍只传输原始 Markdown 字符串。

**已考虑但放弃的替代方案**：
- CDN 动态加载（`<script src="https://...">`）：在 `--share` 离线隧道场景下不可靠，且增加外部依赖。
- 服务端 Go 渲染（`blackfriday`/`goldmark`）：将 HTML 从服务端推送，需改造 SSE 协议和前端渲染逻辑，改动面更大且与前端状态解耦困难。

**风险**：
1. 构建产物 `recorder.bundle.js` 需提交到版本库，否则 `go build` 在无 Node.js 的环境中失败。缓解：CI 始终先运行 `make build-web`，并在 `.gitignore` 中明确排除 `recorder.bundle.js`，同时在 CI 中校验 bundle 与源文件的 hash 一致性。
2. esbuild 的 IIFE 输出不支持直接 Tree-shaking DOMPurify 的 DOM adapter；需验证 `dompurify` 在 IIFE + jsdom-free 浏览器环境下的包大小（预期 ≤50 KB gzip）。
3. 现有 `window.__voiceTest` 和 `window.saveToken` 全局暴露依赖 IIFE 包装器，esbuild IIFE 模式天然保留顶层 `window.*` 赋值，兼容性无问题。

---

# Plan: Web UI 引入 esbuild 构建流水线以支持完整 GFM Markdown 渲染

Proposal: docs/proposals/proposal-esbuild-gfm.md

## Phase A: 建立 esbuild 构建基础设施

### Tests (write first)

在实现前先验证以下断言均失败（基线确认），实现后均通过：

- `grep -q 'build-web:' Makefile` — Makefile 含 build-web 目标
- `grep -q '"esbuild"' package.json` — 根目录 package.json 声明 esbuild devDep
- `grep -q '"marked"' package.json` — 根目录 package.json 声明 marked devDep
- `grep -q '"dompurify"' package.json` — 根目录 package.json 声明 dompurify devDep
- `make build-web && test -f internal/daemon/web/recorder.bundle.js` — bundle 产物生成
- `go test ./...` — Go 单元测试全通过（embed 路径变更后不回归）

### Implementation

**新增文件**

1. `package.json`（repo 根目录）：声明 devDeps: esbuild ^0.25.5、marked ^15.0.12、dompurify ^3.2.6；scripts.build 调用 esbuild 以 `--bundle --format=iife --platform=browser` 打包 `recorder.src.js` → `recorder.bundle.js`。

2. `internal/daemon/web/recorder.src.js`：将 `recorder.js` 全文复制（557 行）；在文件顶部加两行 import：`import { marked } from 'marked'` 和 `import DOMPurify from 'dompurify'`。`mdToHtml` 函数保持不变（Phase B 替换）；`window.__voiceTest`（L538）和 `window.saveToken`（L81）全局赋值不改动。

**修改文件**

3. `Makefile`：新增 `build-web` 目标（`npm ci && npm run build`）；`build` 目标改为依赖 `build-web`；`.PHONY` 追加 `build-web`。

4. `internal/daemon/web/index.html` L167：`<script src="recorder.js">` → `<script src="recorder.bundle.js">`。

5. `.gitignore`：追加 `internal/daemon/web/recorder.bundle.js`。

**注意**：Phase A 结束时 bundle 行为与 `recorder.js` 完全等价，现有 E2E 套件可继续全绿。

### DoD

- [ ] `go test ./...`
- [ ] `grep -q 'build-web:' Makefile`
- [ ] `grep -q '"esbuild"' package.json`
- [ ] `make build-web && test -f internal/daemon/web/recorder.bundle.js`
- [ ] `make build && ./voci --help`（Go embed 路径切换后二进制可启动）
- [ ] `cd e2e && npx playwright test --reporter=list`（现有套件不回归）

---

## Phase B: 替换 mdToHtml 为 marked + DOMPurify，新增 E2E 渲染测试

### Tests (write first)

先新增 `e2e/tests/markdown-render.spec.ts`（约 30 行），令其失败（当前 `mdToHtml` 不产生 `<table>`），再修改实现令其通过：

- 测试 1：通过 `window.__voiceTest.injectMessage()` 注入含 GFM 表格的消息，断言 `page.locator('table')` 数量为 1。
- 测试 2：注入含 `<script>window.__xss=1</script>hello` 的消息，断言 `#messages script` 数量为 0，且 `window.__xss` 为 undefined。

### Implementation

**修改 `internal/daemon/web/recorder.src.js`**

删除 `mdToHtml` 函数（约 12 行），替换为：

```js
marked.setOptions({ gfm: true, breaks: false });

function mdToHtml(text) {
  return DOMPurify.sanitize(marked.parse(text));
}
```

`esc()` 函数本身保留，供非 Markdown 场景的属性值转义使用。

改动范围：`recorder.src.js` 约净 -7 行；`e2e/tests/markdown-render.spec.ts` 新增约 30 行。Phase B 结束后重新执行 `make build-web`。

### DoD

- [ ] `go test ./...`
- [ ] `make build-web`
- [ ] `! grep -q 'function mdToHtml' internal/daemon/web/recorder.src.js`（旧实现已删除）
- [ ] `grep -q 'DOMPurify.sanitize' internal/daemon/web/recorder.src.js`
- [ ] `cd e2e && npx playwright test markdown-render --reporter=list`（新测试通过）
- [ ] `cd e2e && npx playwright test --reporter=list`（全套件不回归）

---

## Constraints

- `recorder.bundle.js` 不提交 git（加入 `.gitignore`）；`make build` 依赖 `build-web`，保证本地和 CI 的 bundle 总是最新
- 不引入 TypeScript、PostCSS 或超出 esbuild + marked + dompurify 的额外依赖
- DOMPurify 必须包裹所有 `marked.parse()` 输出，再赋给 `innerHTML`
- `window.__voiceTest` 和 `window.saveToken` 全局暴露须保持不变（IIFE 模式天然兼容）
- `recorder.src.js` 的 `esc()` 函数不删除——仍用于属性值等非 Markdown 场景
- 每个 Phase 代码变更 ≤200 行

## Acceptance Gate

- [ ] `go test ./...`
- [ ] `make build && ./voci --help`
- [ ] `cd e2e && npx playwright test --reporter=list`（含 markdown-render.spec.ts）
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Proposal self-review: APPROVED
premise-ledger:
[E] Background identifies hand-rolled mdToHtml at recorder.js L183-195: read directly from file
[C] Feasibility of //go:embed web/* change: verified in internal/daemon/server.go L19-20
[C] Node.js tooling availability in CI: verified via e2e/package.json (Playwright already present)
[C] window.__voiceTest and window.saveToken globals: confirmed at recorder.js L538-544 and L81
[H] esbuild IIFE format preserves window.* top-level assignments: background knowledge of esbuild output behavior
[H] DOMPurify gzip size estimate ≤50 KB: background knowledge of library sizes
[H] CDN approach unreliable in --share tunnel mode: inferred from CLAUDE.md tunnel architecture
GCL-self-report: E=1 C=3 H=3

Proposal approved. Starting plan draft.

Plan review iteration 1: APPROVED

premise-ledger:
[E] Phase A has ### Tests before ### Implementation: read directly from plan
[E] Phase B has ### Tests before ### Implementation: read directly from plan
[E] Phase A DoD[0] = `go test ./...`: read directly from plan
[E] Phase B DoD[0] = `go test ./...`: read directly from plan
[E] Acceptance Gate[0] = `go test ./...`: read directly from plan
[E] All Phase A DoD items are shell commands: read directly from plan
[E] All Phase B DoD items are shell commands: read directly from plan
[E] All Acceptance Gate items are shell commands: read directly from plan
[E] Phase B absence check uses `! grep -q` (not grep -qv): read directly from plan
[E] Phase A builds infrastructure before Phase B uses it (no circular deps): read directly from plan
[E] All 5 goals addressed by plan phases (Goals 1-2 → Phase B; Goals 3-4 → Phase A; Goal 5 → Phase B): cross-reference proposal + plan
[C] index.html L167 has `<script src="recorder.js">`: verified by grep
[C] server.go L19 has `//go:embed web/*`: verified by grep
[C] web/ contains recorder.js and index.html: verified by ls
[C] e2e/tests/ directory exists: verified by ls
[C] .gitignore exists: verified by ls
[C] New files (package.json, recorder.src.js, recorder.bundle.js, markdown-render.spec.ts) noted as new: read from plan text
[C] Proposal file `docs/proposals/proposal-esbuild-gfm.md` in plan header does not exist yet (minor metadata discrepancy, non-blocking): verified by ls
[C] No scope items without backing goals: cross-reference goals list vs phase descriptions
[C] No natural-language DoD items requiring move to Constraints: read from plan DoD sections
GCL-self-report: E=11 C=9 H=0

intra-rater-variance check (sample triggered by TASK-72 hash): second independent pass completed. pass1 E=11 C=9 H=0 GCL=20 | pass2 E=11 C=9 H=0 GCL=20. Variance=0 criteria changed. Verdict unchanged: APPROVED.
<!-- SECTION:NOTES:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 go test ./...
- [ ] #2 grep -q 'build-web:' Makefile
- [ ] #3 grep -q '"esbuild"' package.json
- [ ] #4 make build-web && test -f internal/daemon/web/recorder.bundle.js
- [ ] #5 go test ./...
- [ ] #6 make build-web
- [ ] #7 ! grep -q 'function mdToHtml' internal/daemon/web/recorder.src.js
- [ ] #8 grep -q 'DOMPurify.sanitize' internal/daemon/web/recorder.src.js
- [ ] #9 cd e2e && npx playwright test markdown-render --reporter=list
- [ ] #10 go test ./...
- [ ] #11 make build && ./voci --help
- [ ] #12 cd e2e && npx playwright test --reporter=list
<!-- DOD:END -->

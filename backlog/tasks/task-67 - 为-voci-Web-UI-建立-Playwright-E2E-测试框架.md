---
id: TASK-67
title: 为 voci Web UI 建立 Playwright E2E 测试框架
status: 'Basic: Done'
assignee: []
created_date: '2026-06-30 16:27'
updated_date: '2026-06-30 23:53'
labels:
  - 'kind:basic'
dependencies: []
ordinal: 40000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
为 voci Web UI 建立基于 Playwright Test（@playwright/test）的 E2E 测试套件，独立于 go test ./...，通过 make e2e 触发。覆盖：Bearer token 输入框布局、PTT 录音交互、移动端模拟。
<!-- SECTION:DESCRIPTION:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
# Plan: 为 voci Web UI 建立 Playwright E2E 测试框架

## Context

`e2e/` 目录已存在基础骨架：`playwright.config.ts`、`globalSetup/Teardown.ts`、以及五个测试文件（auth、integration、voice-append、no-flicker、emit-contract）。缺失的是三项：Makefile `make e2e` 入口（当前 Makefile 无该目标）、移动端视口测试用例（任务需求之一）、以及 CLAUDE.md 中的文档记录。本计划仅补齐这三项缺口。

## Phase 1: 补全 Makefile e2e 入口并确保浏览器已安装

在 Makefile 中添加 `e2e` 目标（依赖 `build`，调用 `cd e2e && npx playwright test`）；在 `e2e/` 目录下运行 `npm ci` 并安装 Playwright 浏览器二进制。

Instructions:
- 在 `Makefile` 的 `.PHONY` 行追加 `e2e`，新增 target：
  ```makefile
  e2e: build
  	cd e2e && npx playwright test
  ```
  （依赖 `build` 确保 Go 二进制为最新；`globalSetup.ts` 会启动 `go test -run TestPlaywrightSetup` 来运行测试服务器）
- 在 `e2e/` 目录下运行 `npm ci` 确保依赖已安装。
- 运行 `cd e2e && npx playwright install --with-deps chromium` 安装浏览器（如未缓存）。

### DoD
- [ ] `grep -q '^e2e:' /home/yale/work/voci/Makefile`
- [ ] `test -x /home/yale/work/voci/e2e/node_modules/.bin/playwright`

## Phase 2: 补写移动端视口测试用例

现有测试无移动端场景。新建 `e2e/tests/mobile.spec.ts`，使用 `devices['iPhone 14']` 覆盖：Bearer token overlay 在移动端视口下可见且可交互、PTT 按钮在移动端视口下存在且可见。同时在 `playwright.config.ts` 的 `projects` 中追加 `Mobile Chrome`（`devices['Pixel 5']`）project。

Instructions:
- 创建 `e2e/tests/mobile.spec.ts`，顶部声明 `test.use({ ...devices['iPhone 14'] })`，包含至少两个 test：
  1. `token overlay 在移动端显示` — 拦截 `/api/context` 返回 401，验证 `#voci-token-setup` 可见
  2. `PTT 按钮在移动端视口内可见` — 拦截 `/api/context` 返回 200，验证 `#ptt-btn`（或同等 PTT 元素）在视口中可见
- 在 `e2e/playwright.config.ts` 的 `projects` 数组追加：
  ```ts
  { name: 'Mobile Chrome', use: { ...devices['Pixel 5'] } },
  ```

### DoD
- [ ] `grep -qE "test\.use|iPhone 14" /home/yale/work/voci/e2e/tests/mobile.spec.ts`
- [ ] `[ $(grep -c 'test(' /home/yale/work/voci/e2e/tests/mobile.spec.ts) -ge 2 ]`
- [ ] `grep -q "Pixel\|iPhone\|Mobile" /home/yale/work/voci/e2e/playwright.config.ts`

## Phase 3: 运行验证并更新 CLAUDE.md

执行 `make e2e` 确认套件通过，将 E2E 测试命令和覆盖范围补充进 CLAUDE.md。

Instructions:
- 在 `/home/yale/work/voci/` 运行 `make e2e`，确认无 failed/timedOut。
- 在 `CLAUDE.md` 的 "Build & Install" 代码块末尾追加：
  ```bash
  make e2e           # Playwright E2E 套件（需要 voci binary + Chromium）
  ```
- 在 CLAUDE.md 中添加 "E2E Tests (Playwright)" 小节，说明套件位置、触发命令、覆盖范围（Bearer token overlay、PTT 交互、移动端视口）。

### DoD
- [ ] `cd /home/yale/work/voci/e2e && npx playwright test --reporter=list 2>&1 | grep -q 'passed'`
- [ ] `grep -q 'make e2e' /home/yale/work/voci/CLAUDE.md`
- [ ] `grep -qiE 'E2E|playwright' /home/yale/work/voci/CLAUDE.md`

## Constraints
- 不修改 Go 代码或现有 go test 测试文件
- 测试套件与 go test ./... 完全独立（`make e2e` 不调用 `make test`）
- 不引入 docker 或额外 CI 基础设施依赖
- 新增 Mobile Chrome project 不影响 `pw-noflicker.config.ts`（该配置文件独立）

## Acceptance Gate
- [ ] `grep -q '^e2e:' /home/yale/work/voci/Makefile`
- [ ] `grep -qE "test\.use|iPhone 14" /home/yale/work/voci/e2e/tests/mobile.spec.ts`
- [ ] `[ $(grep -c 'test(' /home/yale/work/voci/e2e/tests/mobile.spec.ts) -ge 2 ]`
- [ ] `grep -q "Pixel\|iPhone\|Mobile" /home/yale/work/voci/e2e/playwright.config.ts`
- [ ] `cd /home/yale/work/voci/e2e && npx playwright test --reporter=list 2>&1 | grep -q 'passed'`
- [ ] `grep -q 'make e2e' /home/yale/work/voci/CLAUDE.md`
- [ ] `grep -qiE 'E2E|playwright' /home/yale/work/voci/CLAUDE.md`
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Plan review iteration 1: NEEDS_REVISION — fixed 2 DoD precision issues: replaced bare `test -f` with `grep -q` content checks for mobile.spec.ts in Phase 2 DoD and Acceptance Gate.

Plan review iteration 2: NEEDS_REVISION → fixes applied → re-saved as updated plan

Plan review iteration 3: APPROVED (after fix — added 2 missing Acceptance Gate items: playwright.config.ts mobile-project check and CLAUDE.md E2E-section check)

cap:propose=approved

claimed: 2026-06-30T16:43:44Z

Requeued by scanner reap-due (daemon-direct): in-progress timeout exceeded 30 minutes.

Worktree corruption detected: agent spawned but worktree .git file was lost during execution. No commits were made on the branch. Cleaned up broken worktree and stale branch. Resetting for re-claim. (2026-06-30T17:15:00Z)

claimed: 2026-06-30T23:29:23Z
<!-- SECTION:NOTES:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 [ $(grep -c 'test(' /home/yale/work/voci/e2e/tests/mobile.spec.ts) -ge 2 ]
- [ ] #2 cd /home/yale/work/voci/e2e && npx playwright test --reporter=list 2>&1 | grep -q 'passed'
- [ ] #3 grep -q "Pixel\|iPhone\|Mobile" /home/yale/work/voci/e2e/playwright.config.ts
- [ ] #4 grep -q '^e2e:' /home/yale/work/voci/Makefile
- [ ] #5 grep -q 'make e2e' /home/yale/work/voci/CLAUDE.md
- [ ] #6 grep -qE "test\.use|iPhone 14" /home/yale/work/voci/e2e/tests/mobile.spec.ts
- [ ] #7 grep -qiE 'E2E|playwright' /home/yale/work/voci/CLAUDE.md
- [ ] #8 test -x /home/yale/work/voci/e2e/node_modules/.bin/playwright
- [ ] #9 bash "/home/yale/.local/share/baime/scripts/validate-plugin.sh"
<!-- DOD:END -->

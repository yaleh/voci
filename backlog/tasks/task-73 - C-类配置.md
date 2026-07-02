---
id: TASK-73
title: C 类配置
status: 'Basic: Done'
assignee: []
created_date: '2026-07-01 16:29'
updated_date: '2026-07-01 17:19'
labels:
  - 'kind:basic'
dependencies: []
ordinal: 44000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
C 类配置
<!-- SECTION:DESCRIPTION:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
# Proposal: C 类配置

## Background

voci's Web UI contains UX/timing constants baked into the compiled JavaScript bundle:
the `/api/context` poll interval (5 s), status banner auto-hide delay (2 s), entity
and task display slice counts (6 / 4 / 6), local-message history cap (40), and the
post-emit refresh delay (600 ms). When these values do not fit a user's environment
— e.g. a slow connection where 5 s polling causes stale context, or a high-density
screen where 4 task pills is too few — there is currently no way to tune them without
patching source and rebuilding. Unlike B/D parameters (server-side and VAD
respectively), C-class values are pure browser presentation preferences; a
URL-query-param → localStorage → default resolution chain lets operators and power
users tune them instantly without any server restart or bundle rebuild.

## Goals

1. All C-class constants listed below are readable from a canonical source hierarchy
   (URL query param → localStorage → hardcoded default) at page-load time, so that
   changing a value takes effect on next page reload without any server restart or
   bundle rebuild:
   - `contextPollMs` — `/api/context` poll interval (default 5000 ms)
   - `statusHideMs` — status banner auto-hide delay (default 2000 ms)
   - `entitySlice` — max entity lines shown in context panel (default 6)
   - `taskPillSlice` — max task pills shown in header (default 4)
   - `taskListSlice` — max task lines shown in expanded context panel (default 6)
   - `localMsgCap` — max local messages kept in memory (default 40)
   - `postEmitDelayMs` — delay before refreshing context after emit (default 600 ms)

2. A URL query param override (e.g. `?contextPollMs=2000`) is honored for the
   current page session only and does not persist to localStorage unless the user
   explicitly saves it.

3. A small settings panel (or hidden keyboard shortcut) in the Web UI allows the
   user to view the current effective value of each parameter and save a persistent
   override to localStorage; the persisted value survives page reload.

4. All seven parameters have validated types and clamped ranges enforced at
   resolution time (e.g. `contextPollMs` minimum 500 ms, slice counts minimum 1),
   so malformed query params or corrupted localStorage entries fall back silently to
   the hardcoded default.

5. The existing Playwright E2E suite continues to pass without modification; the
   `window.__voiceTest.getVadConfig()` contract is unchanged.

## Proposed Approach

Introduce a `resolveConfig()` function in `recorder.src.js` that is called once at
init time. It iterates over a descriptor table of C-class parameters (name, default,
min, max, type) and for each parameter: reads the URL query string first, falls back
to `localStorage.getItem('voci_c_<name>')`, then falls back to the hardcoded
default. All seven existing hardcoded literals are replaced with references to the
resolved config object.

A minimal settings UI — either a gear icon that toggles a settings drawer in the
existing page shell, or an accessible keyboard shortcut (e.g. `?`) — renders the
current effective values in editable fields. On save, values are written to
`localStorage` under `voci_c_<name>` keys. On reset, the corresponding localStorage
keys are removed and the hardcoded defaults take effect on next reload.

No changes are needed to any Go server code, HTTP APIs, or config.yaml schema.
The esbuild pipeline added in TASK-72 ensures the updated `recorder.src.js` is
bundled cleanly; no build-system changes are required beyond a normal `make build`.

## Trade-offs and Risks

**Not in scope**: server-side propagation of C-class values (not needed — these are
pure browser preferences). Multi-user coordination (each browser session is
independent by design). Import/export of settings profiles.

**Risk — localStorage namespace collision**: using short keys like `voci_c_contextPollMs`
is unlikely to clash with other apps, but must be documented so third-party
integrators are aware.

**Risk — settings UI surface area**: adding a visible settings panel increases the
HTML/CSS footprint and test surface. Mitigated by keeping the UI minimal (read-only
display + text inputs + save/reset buttons) and reusing existing inline-style
conventions already present in the file.

**Alternative considered — config served from /api/config**: rejected because C-class
values are user preferences, not operator policy. Serving them from the server would
couple browser UX choices to server deployment and YAML config management, which is
inconsistent with the B vs. C taxonomy.

**Alternative considered — cookie storage**: `localStorage` is preferred because it
is same-origin scoped, does not transmit values to the server on every request, and
is already used by the token storage path (`voci_token`).

---

# Plan: C 类配置

## Phase A: resolveConfig() function and constant substitution
### Tests (write first)
- e2e/tests/c-config-unit.spec.ts — new file
  - Test: URL query param ?contextPollMs=1000 overrides default 5000
  - Test: localStorage voci_c_contextPollMs=3000 overrides default 5000
  - Test: URL query param takes priority over localStorage
  - Test: malformed value (e.g. ?contextPollMs=abc) falls back to default 5000
  - Test: out-of-range value (e.g. ?contextPollMs=100) is clamped to min 500
  - Test: window.__voiceTest.getCConfig() returns all 7 resolved values
### Implementation
- internal/daemon/web/recorder.src.js: add resolveConfig() function, PARAM_DESCRIPTORS table, replace 7 hardcoded literals
### DoD
- [ ] `go test ./...`
- [ ] `cd e2e && npx playwright test tests/c-config-unit.spec.ts --reporter=list`
- [ ] `grep -q 'resolveConfig' internal/daemon/web/recorder.src.js`
- [ ] `! grep -q 'setInterval(refreshContext, 5000)' internal/daemon/web/recorder.src.js`

## Phase B: Settings panel UI
### Tests (write first)
- e2e/tests/c-config-settings.spec.ts — new file
  - Test: settings panel is accessible (hidden by default, revealed by keyboard shortcut or button)
  - Test: settings panel shows current effective value of contextPollMs
  - Test: saving a value writes to localStorage under voci_c_contextPollMs
  - Test: resetting a value removes localStorage key and shows hardcoded default
### Implementation
- internal/daemon/web/recorder.src.js: add settings panel HTML, toggle logic, save/reset handlers
### DoD
- [ ] `go test ./...`
- [ ] `cd e2e && npx playwright test tests/c-config-settings.spec.ts --reporter=list`
- [ ] `grep -q 'voci_c_' internal/daemon/web/recorder.src.js`

## Constraints
- No changes to any Go server code, HTTP APIs, or config.yaml schema
- Settings UI must not break existing aria roles or keyboard navigation in the page shell
- localStorage keys must use the voci_c_ prefix to avoid collision with other apps
- All 7 parameters must have enforced minimum values (contextPollMs ≥ 500, slice counts ≥ 1, timers ≥ 100)
- The window.__voiceTest.getVadConfig() contract is unchanged

## Acceptance Gate
- [ ] `go test ./...`
- [ ] `cd e2e && npx playwright test --reporter=list`
- [ ] `make build`
- [ ] `grep -q 'getCConfig' internal/daemon/web/recorder.src.js`
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Proposal self-review: APPROVED
premise-ledger:
[E] Motivation (Background 3-8 lines): counted 8 lines in final version — verified by direct inspection
[E] Goals (numbered, verifiable): all 5 goals have concrete, observable outcomes with named params and defaults
[E] Feasibility (Approach aligns with codebase): recorder.src.js read directly; 7 hardcoded constants identified; localStorage already used (voci_token); esbuild pipeline confirmed present (TASK-72/commit 64203e2)
[E] Completeness (Trade-offs and risks): 2 risks + 2 alternatives documented
[C] Consistency (no contradictions): Goals enumerate 7 params, Approach describes resolveConfig() over them, Trade-offs explains server exclusion — cross-checked, no contradictions
GCL-self-report: E=4 C=1 H=0

Proposal approved. Starting plan draft.

Plan review iteration 1: APPROVED
premise-ledger:
[E] Goal coverage: all 5 proposal goals addressed — G1/G4 by Phase A resolveConfig+clamping tests, G2 by URL-priority test + Phase B save action, G3 by Phase B settings panel, G5 by full-suite Acceptance Gate
[E] TDD structure: both phases have ### Tests before ### Implementation
[E] TDD order: first DoD item in Phase A and Phase B is `go test ./...`
[E] Acceptance gate: first item is `go test ./...`
[E] DoD executability: all DoD and Acceptance Gate items are shell commands
[E] Absence checks: `! grep -q` pattern used in Phase A DoD (not grep -qv)
[E] Phase ordering: Phase A produces resolveConfig/config object; Phase B consumes it — no circular deps
[E] Scope discipline: Phase A ↔ Goals 1/2/4, Phase B ↔ Goal 3; nothing out of scope
[C] File paths: recorder.src.js verified to exist; e2e/tests/ directory verified to exist; new spec files correctly flagged as to-be-created
[H] No natural-language DoD items found; no grep-qv patterns found
GCL-self-report: E=7 C=1 H=1

claimed: 2026-07-01T16:39:59Z

Requeued by scanner reap-due (daemon-direct): in-progress timeout exceeded 30 minutes.
<!-- SECTION:NOTES:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 go test ./...
- [ ] #2 cd e2e && npx playwright test tests/c-config-unit.spec.ts --reporter=list
- [ ] #3 grep -q 'resolveConfig' internal/daemon/web/recorder.src.js
- [ ] #4 ! grep -q 'setInterval(refreshContext, 5000)' internal/daemon/web/recorder.src.js
- [ ] #5 go test ./...
- [ ] #6 cd e2e && npx playwright test tests/c-config-settings.spec.ts --reporter=list
- [ ] #7 grep -q 'voci_c_' internal/daemon/web/recorder.src.js
- [ ] #8 go test ./...
- [ ] #9 cd e2e && npx playwright test --reporter=list
- [ ] #10 make build
- [ ] #11 grep -q 'getCConfig' internal/daemon/web/recorder.src.js
<!-- DOD:END -->

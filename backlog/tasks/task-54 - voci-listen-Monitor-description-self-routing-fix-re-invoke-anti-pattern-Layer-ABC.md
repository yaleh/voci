---
id: TASK-54
title: >-
  voci-listen Monitor description self-routing: fix re-invoke anti-pattern
  (Layer A+B+C)
status: 'Basic: Done'
assignee: []
created_date: '2026-06-30 04:49'
updated_date: '2026-06-30 05:23'
labels:
  - 'kind:basic'
dependencies: []
ordinal: 38000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
当前 voci-listen 的 Monitor description 包含 "re-invoke /voci-listen" 指令，导致 /clear 或 context compaction 后新会话触发完整 bootstrap，进而 arm 新 Monitor，产生 Monitor 增殖问题。参考 baime TASK-210/228 的 A+B+C 三层方案：Layer A 将 description 改为自包含派发指令（不再 re-invoke）；Layer B 在 arm 前用 TaskList 做幂等检查；Layer C 在 SKILL.md 中明确区分冷启动路径与 Monitor 事件直接 dispatch 路径。
<!-- SECTION:DESCRIPTION:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
# Plan: voci-listen Monitor description self-routing: fix re-invoke anti-pattern (Layer A+B+C)

Proposal: docs/proposals/proposal-voci-listen-monitor-self-routing.md

## Phase A: Layer A — Rewrite Monitor description to self-contained dispatch

### Tests (write first)

These assertions currently **fail** (pre-change) and must **pass** after Phase A is applied:

```bash
# T-A1: "re-invoke" is absent from all Monitor description= lines in SKILL.md
! grep -q 'description=.*re-invoke' /home/yale/work/voci/.claude/skills/voci-listen/SKILL.md

# T-A2: not-grep contract for "re-invoke" is present in frontmatter
grep -q 'not-grep.*re-invoke' /home/yale/work/voci/.claude/skills/voci-listen/SKILL.md

# T-A3: old "re-invoke /voci-listen" hint is absent from Monitor description occurrences
! grep -q 'if this is a new session.*re-invoke' /home/yale/work/voci/.claude/skills/voci-listen/SKILL.md
```

### Implementation

Exact edits to `/home/yale/work/voci/.claude/skills/voci-listen/SKILL.md`:

**Edit 1 — Frontmatter skill description (line 3):** Replace the description field value. Remove the `reconnectGuard (re-arms Monitor on existing port if lock+PID still live)` clause and the `Recovers across /clear via reconnectGuard` sentence. Rewrite to reflect that the Monitor description itself carries dispatch logic (no restart hint).

New frontmatter description:
```
"Arms a persistent Monitor with \"voci serve --share --serve-port $PORT\" (per-session OS-assigned port + Cloudflare Quick Tunnel + Bearer auth). Each session writes a per-session lock file ~/.voci/<SESSION_ID>.lock; stale locks (dead PID) are swept on cold-start. Merges stderr into stdout via 2>&1 and grep-filters to three line types: JSON events (Rewritten field → execute inline), share-URL lines (display to user), and Bearer-token lines (display to user). Single-instance: sweeps stale voci-listen Monitor tasks before arming. Monitor description is self-contained: on event arrival in any session, classify and dispatch directly without calling the skill again. Stops when ~/.voci/.listen-stop sentinel is present."
```

**Edit 2 — Frontmatter contracts (line 26):** Change `grep: "re-invoke"` to `not-grep: "re-invoke"` so the contract actively enforces absence.

```yaml
  - not-grep: "re-invoke"
    target: self
```

**Edit 3 — Spec entry-point guard comment block (lines 58–71):** Replace the "Cold-start vs. Reconnect" comment that describes the restart path. New comment:

```
-- ─────────────────────────────────────────────────────────────────────────────
-- Entry Point Guard: Cold-start vs. Monitor-event dispatch
--
-- Cold-start (explicit /voci-listen invocation → λ() → coldStart()):
--   Executes full bootstrap: stopStaleMon → sweepStaleLocks → checkStopSentinel
--   → manageLock (start voci serve, get PORT) → ensureMonitor(SESSION_ID, PORT).
--
-- Monitor-event dispatch (Monitor fires in any session → onMonitorEvent(line)):
--   The Monitor description carries full dispatch instructions; the fresh session
--   classifies the line and acts directly. No skill bootstrap. No cold-start.
-- ─────────────────────────────────────────────────────────────────────────────
```

**Edit 4 — Monitor description string in `armMonitor` call (line 99):** Replace old description argument:

Old:
```
description="voci-listen: a voice event has arrived — extract the Rewritten field from the JSON line and execute it as the next in-session instruction; if this is a new session (after /clear or context compaction) re-invoke /voci-listen first to restore the listening loop"
```

New:
```
description="voci-listen: voice event arrived — DO NOT call /voci-listen again. Classify the line: if it starts with 'voci share URL:' or 'Bearer token:' → display to user; otherwise → parse JSON, extract the 'rewritten' field, execute it inline as the next instruction."
```

**Edit 5 — Monitor description string in Implementation `### Arm Monitor` pseudocode block (line 274):** Same replacement as Edit 4.

**Edit 6 — Implementation `### Cross-/clear self-recovery` section (lines 322–330):** Replace the entire section with:

```markdown
### Cross-/clear self-recovery

The Monitor `description` field is self-contained. When a Monitor event arrives in a
new session (after `/clear` or context compaction), the description instructs Claude to
classify the line and act directly — no skill call is needed. If the line
starts with `"voci share URL:"` or `"Bearer token:"`, display it to the user.
Otherwise, extract the `rewritten` field from the JSON and execute it inline.
```

### DoD

- [ ] `go test ./...`
- [ ] `! grep -q 'description=.*re-invoke' /home/yale/work/voci/.claude/skills/voci-listen/SKILL.md`
- [ ] `grep -q 'not-grep.*re-invoke' /home/yale/work/voci/.claude/skills/voci-listen/SKILL.md`
- [ ] `! grep -q 'if this is a new session.*re-invoke' /home/yale/work/voci/.claude/skills/voci-listen/SKILL.md`
- [ ] `grep -q 'DO NOT call /voci-listen' /home/yale/work/voci/.claude/skills/voci-listen/SKILL.md`

---

## Phase B: Layer B+C — ensureMonitor idempotency + path separation in SKILL.md spec

### Tests (write first)

These assertions currently **fail** (pre-change) and must **pass** after Phase B is applied:

```bash
# T-B1: ensureMonitor function appears in SKILL.md
grep -q 'ensureMonitor' /home/yale/work/voci/.claude/skills/voci-listen/SKILL.md

# T-B2: coldStart and onMonitorEvent named paths appear (>=2 occurrences total)
[ "$(grep -c 'coldStart\|onMonitorEvent' /home/yale/work/voci/.claude/skills/voci-listen/SKILL.md)" -ge 2 ]

# T-B3: reconnectGuard is no longer linked to re-invoke in any comment
! grep -q 'reconnectGuard.*re-invoke\|re-invoke.*reconnectGuard' /home/yale/work/voci/.claude/skills/voci-listen/SKILL.md
```

### Implementation

Exact edits to `/home/yale/work/voci/.claude/skills/voci-listen/SKILL.md`:

**Edit 7 — Spec signatures block (after line 48, `reconnectGuard`):** Add `ensureMonitor` signature and the two named path signatures:

```
ensureMonitor     :: (SESSION_ID, PORT) → ()   -- idempotent Monitor arm: TaskList check before Monitor call
coldStart         :: () → Outcome              -- explicit invocation entry: full bootstrap → ensureMonitor
onMonitorEvent    :: Line → Outcome            -- Monitor-event entry: classify → display or execute; no bootstrap
```

**Edit 8 — Spec pseudocode: introduce `ensureMonitor` function body (after `reconnectGuard()` block, before `cleanupLock`):**

```
ensureMonitor(SESSION_ID, PORT) = {
  -- Idempotency check: avoid arming a duplicate Monitor if one is already live.
  -- Step 1: Call TaskList to enumerate all active background tasks.
  -- Step 2: Filter entries whose description contains "voci-listen".
  -- Step 3: If any live match found, return early — do NOT call Monitor again.
  --         On TaskList failure, treat as "no live Monitor" and proceed to arm.
  tasks: TaskList(),
  if (any task in tasks where "voci-listen" in task.description):
    echo "[voci-listen] ensureMonitor: live Monitor already exists — skipping arm"
    return: (),

  -- No live Monitor found: arm a new persistent Monitor on PORT.
  Monitor(persistent=true,
    command="voci serve --share --serve-port $PORT 2>&1 | grep --line-buffered -E '\"rewritten\"|voci share URL|Bearer token'",
    description="voci-listen: voice event arrived — DO NOT call /voci-listen again. Classify the line: if it starts with 'voci share URL:' or 'Bearer token:' → display to user; otherwise → parse JSON, extract the 'rewritten' field, execute it inline as the next instruction.")
}
```

**Edit 9 — Spec pseudocode: introduce `coldStart()` and `onMonitorEvent()` wrappers (after `ensureMonitor`, before `stopStaleMon`):**

```
coldStart() = {
  -- Explicit /voci-listen invocation path. Full bootstrap sequence.
  _: stopStaleMon(),
  if (stopSentinel()): return: Stopped,
  (live, SESSION_ID, PORT): reconnectGuard(),
  if (live):
    ensureMonitor(SESSION_ID, PORT),
    return: Listening,
  (SESSION_ID, PORT): manageLock(),
  ensureMonitor(SESSION_ID, PORT),
  return: Listening,
}

onMonitorEvent(line) = {
  -- Monitor-event dispatch path. No bootstrap, no restart.
  if (stopSentinel()):
    cleanupLock(SESSION_ID),
    return: Stopped,
  kind: classifyEvent(line),
  | InfoMessage text → display(text),
  | VoiceEvent line  → execute(extractInstruction(line)),
  return: Listening,
}
```

**Edit 10 — Update `listenLoop()` to delegate to `coldStart()` and `onMonitorEvent()`:** Replace the inline `armMonitor` label and direct `Monitor(...)` call with `ensureMonitor(SESSION_ID, PORT)`. Update the post-Monitor wake-up block to reference `onMonitorEvent(event)`.

**Edit 11 — Rewrite `reconnectGuard()` spec comment:** Narrow its stated purpose from "recover across /clear" to "detect existing live lock to avoid cold-starting when a voci serve process is still running." Any language that links reconnectGuard to a skill restart is fully removed.

**Edit 12 — Implementation `### reconnectGuard` section (line 238):** Remove the sentence "In practice the skill re-invokes itself, so SESSION_ID must be recoverable." Replace with: "If a live lock file is found, `ensureMonitor` will skip arming a new Monitor; `manageLock` and `voci serve` are not restarted."

**Edit 13 — Implementation `### Arm Monitor` section:** Rename to `### ensureMonitor`. Add a paragraph describing the `TaskList` idempotency check that precedes the `Monitor(...)` call.

### DoD

- [ ] `go test ./...`
- [ ] `grep -q 'ensureMonitor' /home/yale/work/voci/.claude/skills/voci-listen/SKILL.md`
- [ ] `[ "$(grep -c 'coldStart\|onMonitorEvent' /home/yale/work/voci/.claude/skills/voci-listen/SKILL.md)" -ge 2 ]`
- [ ] `! grep -q 'reconnectGuard.*re-invoke\|re-invoke.*reconnectGuard' /home/yale/work/voci/.claude/skills/voci-listen/SKILL.md`
- [ ] `grep -q 'Idempotency check' /home/yale/work/voci/.claude/skills/voci-listen/SKILL.md`

---

## Constraints

(Non-executable criteria — NOT in DoD)

- No Go source files are modified. The only file changed is `.claude/skills/voci-listen/SKILL.md`.
- The new Monitor description must be concise enough to fit the Monitor harness field limit while remaining unambiguous for a blank-context session to dispatch correctly without calling the skill again.
- `TaskList` failure in `ensureMonitor` must be treated as "no live Monitor found" — arming proceeds normally to avoid a silent blocking failure mode.
- `reconnectGuard` is retained (its PID-liveness check is still needed to decide between cold-start and re-attach), but its connection to the restart pattern is severed; it feeds into `ensureMonitor`, not into a description-carried hint.
- The grep-filter pipeline, lock-file format, port-isolation scheme, and Cloudflare tunnel behavior are unchanged.

---

## Acceptance Gate

- [ ] `go test ./...`
- [ ] `! grep -q 'description=.*re-invoke' /home/yale/work/voci/.claude/skills/voci-listen/SKILL.md`
- [ ] `grep -q 'not-grep.*re-invoke' /home/yale/work/voci/.claude/skills/voci-listen/SKILL.md`
- [ ] `grep -q 'DO NOT call /voci-listen' /home/yale/work/voci/.claude/skills/voci-listen/SKILL.md`
- [ ] `grep -q 'ensureMonitor' /home/yale/work/voci/.claude/skills/voci-listen/SKILL.md`
- [ ] `[ "$(grep -c 'coldStart\|onMonitorEvent' /home/yale/work/voci/.claude/skills/voci-listen/SKILL.md)" -ge 2 ]`
- [ ] `grep -q 'Monitor(persistent=true' /home/yale/work/voci/.claude/skills/voci-listen/SKILL.md`
- [ ] `grep -q 'command="voci serve' /home/yale/work/voci/.claude/skills/voci-listen/SKILL.md`
- [ ] `grep -q '.listen-stop' /home/yale/work/voci/.claude/skills/voci-listen/SKILL.md`
- [ ] `grep -q 'voci share URL' /home/yale/work/voci/.claude/skills/voci-listen/SKILL.md`
- [ ] `grep -q 'Bearer token' /home/yale/work/voci/.claude/skills/voci-listen/SKILL.md`
- [ ] `grep -q 'TaskStop' /home/yale/work/voci/.claude/skills/voci-listen/SKILL.md`
- [ ] `grep -q 'TaskList' /home/yale/work/voci/.claude/skills/voci-listen/SKILL.md`
- [ ] `grep -q 'rewritten' /home/yale/work/voci/.claude/skills/voci-listen/SKILL.md`
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Proposal self-review: APPROVED
premise-ledger:
[E] Monitor description contains 're-invoke' string: confirmed at SKILL.md lines 99, 274, 325
[E] persistent=true Monitor survives /clear: stated in background context and harness docs
[E] reconnectGuard currently blurs cold-start and reconnect paths: confirmed SKILL.md lines 59-70 and 136-265
[C] Re-invoking /voci-listen calls stopStaleMon() which stops the still-live Monitor: direct code path in listenLoop()
[C] stopStaleMon + new Monitor arm = proliferation on each compaction: follows from above
[C] Absorbing reconnectGuard into ensureMonitor covers both duplicate-arm and reconnect cases: idempotency check subsumes both
[H] Monitor description is not an appropriate carrier for session-boot logic; self-contained dispatch is correct abstraction
[H] TaskList-based idempotency check is the right guard point before Monitor(persistent=true)
GCL-self-report: E=3 C=3 H=2

Proposal approved. Starting plan draft.

Plan review iteration 1: NEEDS_REVISION

Failure 1 — Self-referential contradiction: `! grep -q 're-invoke'` (T-A1, Phase A DoD #2, Acceptance Gate #2) conflicts with `grep -q 'not-grep.*re-invoke'` (T-A2, DoD #3, Gate #3). After Edit 2 adds `not-grep: "re-invoke"` to the frontmatter, that contract line itself contains the substring `re-invoke`, so the broad absence test can never pass with the contract present.

Failure 2 — Plan-introduced 're-invoke' substrings: Phase A Edit 1 adds "without re-invoking the skill", Edit 3 adds "No skill re-invocation. No bootstrap.", Edit 6 adds "no skill re-invocation is needed", and Phase B Edit 9 adds "— No bootstrap, no re-invoke." — all contain `re-invoke` as a substring and would break the broad absence test.

Fix applied: Narrowed the absence test from `! grep -q 're-invoke'` to `! grep -q 'description=.*re-invoke'` everywhere (T-A1, Phase A DoD #2, Acceptance Gate #2). This precisely targets where the anti-pattern lived (the Monitor `description=` argument) without matching the `not-grep: "re-invoke"` contract line or explanatory comments. Also fixed Phase B Edit 9 comment: "no re-invoke" → "no restart".

Plan review iteration 2: APPROVED
premise-ledger:
[E] Goal coverage: all 4 Goals map to named Phase edits and Acceptance Gate items
[E] TDD structure: Phase A and Phase B both have ### Tests before ### Implementation
[E] TDD order: first DoD item in each phase is `go test ./...`
[E] Acceptance gate first item: `go test ./...`
[E] DoD executability: all DoD and Acceptance Gate items are shell commands; natural-language constraints correctly placed in ## Constraints
[C] Absence checks: all use `! grep -q` not `grep -qv`; patterns verified not to be defeated by `not-grep: "re-invoke"` contract insertion (checks require `description=.*re-invoke` with `=`, which the frontmatter line does not match)
[C] Phase ordering: Phase A removes re-invoke from descriptions; Phase B introduces ensureMonitor/coldStart/onMonitorEvent; Edit 10 supersedes Edit 4 cleanly without conflict
[C] Scope discipline: all edits traced to Goals 1-4; no out-of-scope changes
[C] File paths: only .claude/skills/voci-listen/SKILL.md is referenced; confirmed to exist
[H] Self-consistency: `! grep -q 'description=.*re-invoke'` not defeated because `not-grep: "re-invoke"` lacks `=`; line 96 (`-- The description carries a re-invoke hint`) has no `=` so same check passes even if not yet removed by Phase A; Edit 10 (Phase B) covers that line via listenLoop refactor; no circular pattern defeats found
GCL-self-report: E=5 C=4 H=1

claimed: 2026-06-30T05:16:30Z

Phase A+B+C ✓ 2026-06-30T00:00:00Z — All 15 DoD checks pass; go test ./... passes; committed c680da4 on task/TASK-54. Layer A: Monitor description rewritten to self-contained dispatch (DO NOT call /voci-listen). Layer B: ensureMonitor with TaskList idempotency check introduced. Layer C: coldStart/onMonitorEvent path separation added to spec. not-grep contract enforced for re-invoke.

WARNING: agent-summary missing

Completed: 2026-06-30T05:23:27Z
<!-- SECTION:NOTES:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 go test ./...
- [ ] #2 ! grep -q 'description=.*re-invoke' /home/yale/work/voci/.claude/skills/voci-listen/SKILL.md
- [ ] #3 grep -q 'not-grep.*re-invoke' /home/yale/work/voci/.claude/skills/voci-listen/SKILL.md
- [ ] #4 ! grep -q 'if this is a new session.*re-invoke' /home/yale/work/voci/.claude/skills/voci-listen/SKILL.md
- [ ] #5 grep -q 'DO NOT call /voci-listen' /home/yale/work/voci/.claude/skills/voci-listen/SKILL.md
- [ ] #6 grep -q 'ensureMonitor' /home/yale/work/voci/.claude/skills/voci-listen/SKILL.md
- [ ] #7 [ "$(grep -c 'coldStart\|onMonitorEvent' /home/yale/work/voci/.claude/skills/voci-listen/SKILL.md)" -ge 2 ]
- [ ] #8 grep -q 'Idempotency check' /home/yale/work/voci/.claude/skills/voci-listen/SKILL.md
- [ ] #9 grep -q 'Monitor(persistent=true' /home/yale/work/voci/.claude/skills/voci-listen/SKILL.md
- [ ] #10 grep -q 'TaskList' /home/yale/work/voci/.claude/skills/voci-listen/SKILL.md
- [ ] #11 grep -q 'TaskStop' /home/yale/work/voci/.claude/skills/voci-listen/SKILL.md
- [ ] #12 grep -q 'rewritten' /home/yale/work/voci/.claude/skills/voci-listen/SKILL.md
- [ ] #13 grep -q '.listen-stop' /home/yale/work/voci/.claude/skills/voci-listen/SKILL.md
- [ ] #14 grep -q 'voci share URL' /home/yale/work/voci/.claude/skills/voci-listen/SKILL.md
- [ ] #15 grep -q 'Bearer token' /home/yale/work/voci/.claude/skills/voci-listen/SKILL.md
<!-- DOD:END -->

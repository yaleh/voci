---
id: TASK-17
title: /voci-listen skill：会话内 arm Monitor 消费语音事件流
status: 'Basic: Done'
assignee: []
created_date: '2026-06-28 08:46'
updated_date: '2026-06-28 12:17'
labels:
  - 'kind:basic'
dependencies:
  - TASK-16
modified_files:
  - .claude/skills/voci-listen/SKILL.md
ordinal: 1000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
【已改造】Monitor-push 消费者侧。/voci-listen skill 在当前 Claude Code 会话内 arm Monitor(persistent=true, command="voci serve", description=...)——Monitor 的 command 直接是 voci 识别服务（TASK-16 的 `voci serve`），其 stdout 每行（识别出的 Rewritten）被注入会话作为下一条指令。**不再 tail 文件**（取消 `tail -f ~/.voci/events.log`），消除文件 IPC。仿 baime loop-backlog SKILL（command 即扫描器本身）。单实例 stopStaleMon、跨 /clear 自恢复、## Shutdown 停哨保留。
<!-- SECTION:DESCRIPTION:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
# Proposal: /voci-listen Skill — In-Session Monitor Consumer for the Voice Event Stream

## Background

voci's primary interaction model is Monitor-push, not MCP-pull (TASK-15/16): a browser
captures speech, the voci daemon (TASK-16) runs the full ASR→hinted→rewrite→classify
pipeline and appends one JSON event line per utterance to `~/.voci/events.log`. Today
nothing consumes that stream — there is no path that turns a recognized voice utterance
into the next instruction of a live Claude Code session. The MCP `transcribe` tool only
works pull-style (the model must ask), which cannot deliver hands-free, push-driven voice
control. The Monitor harness primitive is exactly the missing piece: armed inside a
session it runs a background command whose stdout lines are injected as wake-up events,
and it persists across `/clear` and `/compact`. This task delivers the consumer side as a
Claude Code skill (`/voci-listen`), mirroring the proven structure of baime's
`loop-backlog` SKILL.md (arm `Monitor(persistent=true,...)`, single-instance via stale
Monitor sweep, cross-`/clear` self-recovery, `## Shutdown` via a stop sentinel).

## Goals

1. Deliver `.claude/skills/voci-listen/SKILL.md` — a Claude Code skill invoked once as
   `/voci-listen` — that arms `Monitor(persistent=true, command="tail -f ~/.voci/events.log", description="<voice-instruction-arrived>")` so each new line of the voice event stream wakes the current session.
2. On each wake-up, the skill MUST extract the recognized instruction text from the event
   line and treat it as the user's next instruction, executed inline in the current
   session against its own live full context (no sub-agent, no frozen snapshot).
3. Single-instance: before arming, sweep and stop any stale `voci-listen` Monitor task
   from a prior session (mirroring loop-backlog `stopStaleMon` using the `TaskList` /
   `TaskStop` harness primitives), so re-invocation never stacks duplicate tails.
4. Cross-`/clear` self-recovery: the Monitor `description` instructs a fresh session to
   re-invoke `/voci-listen`, so a re-emitted line after `/clear` or `/compact`
   re-attaches the consumer automatically.
5. `## Shutdown`: a documented stop sentinel (`~/.voci/.listen-stop`) halts the loop; the
   skill checks it before re-arming and returns a Stopped outcome.
6. Acceptance is structural (grep-based contracts on SKILL.md) plus `go test ./...` staying
   green — adding a skill file must not touch or break the Go build/tests.

## Proposed Approach

Author a single `.claude/skills/voci-listen/SKILL.md` with YAML frontmatter
(`name`, `description`, `allowed-tools: Bash, Read, Monitor, TaskList, TaskStop`, and a
`contracts:` block of self-grep assertions) followed by a lambda-style `listenLoop()`
spec and an `## Implementation` section, exactly in the loop-backlog idiom. The loop:
(a) checks the stop sentinel and returns Stopped if present; (b) sweeps stale Monitor
tasks whose description contains the `voci-listen` sentinel via `TaskList` and stops them
with `TaskStop`; (c) arms `Monitor(persistent=true, command="tail -f ~/.voci/events.log",
description=...)`. Each emitted line is a TASK-16 JSON event; the skill parses the line,
extracts the `Rewritten` (recognized instruction) field — with a graceful fallback to the
raw line when it is not JSON — and then executes that text as the next user instruction
in the current session before looping back to await the next line. The `description`
string carries the cross-`/clear` re-invoke hint and a note that non-instruction output
is ignored. `## Shutdown` documents `touch ~/.voci/.listen-stop` to halt and `rm` to
restart. No Go code changes; the only artifact is the SKILL.md (plus optional tiny
shell snippets embedded in it). The schema contract (`~/.voci/events.log`, one JSON
event per line, `Rewritten` = recognized text) is the documented dependency on TASK-16.

## Trade-offs and Risks

- `tail -f` replays only NEW lines appended after the Monitor arms (no `-n +1` backfill);
  utterances spoken while no session is listening are not consumed. This is intentional —
  voice control targets the live session — but means there is no event backlog/replay.
  Documented as a known limitation; a future variant could seek from a saved offset.
- Coupling to TASK-16's line schema: if the producer changes the `Rewritten` field name or
  switches away from JSON-per-line, the consumer's extraction breaks. Mitigated by a raw
  fallback and by naming the schema contract explicitly; the field name is asserted via a
  grep contract so drift is caught by the skill's own tests.
- Injecting recognized text directly as the next instruction means a misrecognition is
  executed verbatim. Risk is accepted at this layer; human-confirmation gating for
  ActionProposals is a separate task (intent confirmation gate), out of scope here.
- Monitor singleton relies on description-substring matching in the `TaskList` sweep (same
  mechanism loop-backlog uses); a description string collision could over-match. Mitigated
  by a distinctive sentinel substring unique to voci-listen.
- This skill is push-into-session by design; it cannot run truly headless and depends on an
  interactive Claude Code session being open. That is inherent to the Monitor primitive and
  is the intended deployment model, not a defect.

---

# Plan: /voci-listen Skill — In-Session Monitor Consumer for the Voice Event Stream

Proposal: see the combined proposal section finalised into this task's plan (TASK-17 planSet); no standalone proposal file is created.

## Phase A: Skill scaffold — frontmatter, contracts, arm Monitor

### Tests (write first)
The "tests" are grep-based structural assertions on the skill file (skill-lint), captured
as the Phase DoD shell commands below. Author them as a check before the prose is final:
- The SKILL.md exists at `.claude/skills/voci-listen/SKILL.md`.
- Frontmatter declares `name: voci-listen` and a `contracts:` block.
- `allowed-tools` includes `Monitor`, `TaskList`, `TaskStop`.
- The body arms `Monitor(persistent=true, ...)` with `command="tail -f ~/.voci/events.log"`
  and a `description=` carrying the voice-instruction-arrived intent.

### Implementation
- Create `.claude/skills/voci-listen/SKILL.md` with YAML frontmatter (`name`,
  `description`, `allowed-tools: Bash, Read, Monitor, TaskList, TaskStop`, `contracts:`
  self-grep block) and a `listenLoop()` lambda spec that arms
  `Monitor(persistent=true, command="tail -f ~/.voci/events.log", description="<voice instruction arrived — treat the next line as the user's next instruction; re-invoke /voci-listen if this is a new session>")`.

### DoD
- [ ] `go test ./...`
- [ ] `test -f .claude/skills/voci-listen/SKILL.md`
- [ ] `grep -q 'Monitor(persistent=true' .claude/skills/voci-listen/SKILL.md`
- [ ] `grep -q 'tail -f ~/.voci/events.log' .claude/skills/voci-listen/SKILL.md`
- [ ] `grep -q 'description=' .claude/skills/voci-listen/SKILL.md`
- [ ] `grep -q '^name: voci-listen' .claude/skills/voci-listen/SKILL.md`
- [ ] `grep -q 'allowed-tools:.*Monitor' .claude/skills/voci-listen/SKILL.md`

## Phase B: Instruction extraction, single-instance, self-recovery, shutdown

### Tests (write first)
Grep-based structural assertions (captured as DoD below):
- Body documents extracting the recognized instruction text (the `Rewritten` field) from
  each JSON event line and executing it as the next in-session instruction.
- A stale-Monitor sweep (`TaskList` + `TaskStop`) keyed on a distinctive voci-listen
  sentinel substring exists (single-instance).
- The Monitor `description` instructs a fresh session to re-invoke `/voci-listen`
  (cross-`/clear` self-recovery).
- A `## Shutdown` section documents the `~/.voci/.listen-stop` stop sentinel and a stop
  check before re-arming.

### Implementation
- Extend SKILL.md: add a `stopStaleMon`-style sweep section using `TaskList`/`TaskStop`
  matched on the voci-listen description sentinel; add the per-line handling spec
  (parse JSON, take `Rewritten`, raw fallback, run inline as next instruction, loop);
  add the re-invoke hint to the `description`; add a `## Shutdown` section with
  `touch ~/.voci/.listen-stop` / `rm` and a stop-sentinel check returning a Stopped outcome.

### DoD
- [ ] `go test ./...`
- [ ] `grep -q 'Rewritten' .claude/skills/voci-listen/SKILL.md`
- [ ] `grep -q 'TaskStop' .claude/skills/voci-listen/SKILL.md`
- [ ] `grep -q 'TaskList' .claude/skills/voci-listen/SKILL.md`
- [ ] `grep -q '## Shutdown' .claude/skills/voci-listen/SKILL.md`
- [ ] `grep -q '.listen-stop' .claude/skills/voci-listen/SKILL.md`
- [ ] `grep -qi 're-invoke\|/voci-listen' .claude/skills/voci-listen/SKILL.md`
- [ ] `! grep -q 'schedule(' .claude/skills/voci-listen/SKILL.md`

## Constraints
- This task delivers a Claude Code skill (SKILL.md), not Go code; no Go source is added or
  modified. Adding the skill must not change the Go build or tests.
- Mirror the structure of baime `loop-backlog` SKILL.md (frontmatter + `contracts:` +
  lambda spec + `## Implementation` + `## Shutdown`).
- The recognized instruction is executed inline in the current session against its live
  full context — never via a sub-agent and never from a frozen snapshot.
- Consumes the TASK-16 event-stream contract: `~/.voci/events.log`, one JSON event per
  line, recognized text in the `Rewritten` field; `tail -f` consumes only newly appended
  lines (no backfill/replay).
- Human-confirmation gating of ActionProposals is out of scope (separate task).
- Single instance: re-invoking `/voci-listen` must stop the stale Monitor before arming a
  new one (no stacked tails).

## Acceptance Gate
- [ ] `go test ./...`
- [ ] `test -f .claude/skills/voci-listen/SKILL.md`
- [ ] `grep -q 'Monitor(persistent=true' .claude/skills/voci-listen/SKILL.md`
- [ ] `grep -q 'tail -f ~/.voci/events.log' .claude/skills/voci-listen/SKILL.md`
- [ ] `grep -q 'Rewritten' .claude/skills/voci-listen/SKILL.md`
- [ ] `grep -q 'TaskStop' .claude/skills/voci-listen/SKILL.md`
- [ ] `grep -q 'TaskList' .claude/skills/voci-listen/SKILL.md`
- [ ] `grep -q '## Shutdown' .claude/skills/voci-listen/SKILL.md`
- [ ] `grep -q '.listen-stop' .claude/skills/voci-listen/SKILL.md`
- [ ] `grep -q '^name: voci-listen' .claude/skills/voci-listen/SKILL.md`
- [ ] `! grep -q 'schedule(' .claude/skills/voci-listen/SKILL.md`

## RE-ARCHITECTED (2026-06-28)：arm `voci serve` 取代 `tail -f events.log`

**动因**：与 TASK-16 同步——web 服务应实现在 Monitor 的 command 里，而非独立 daemon + 文件 tail。loop-backlog 的 Monitor command 就是 `node scan-loop.js --loop` 本身；voci 应镜像之：Monitor command = `voci serve`。

**新形态**：
- arm `Monitor(persistent=true, command="voci serve", description="voci-listen: 识别出的语音指令到达，把 stdout 行作为下一条会话指令执行；新会话先重新 /voci-listen")`。
- `voci serve` 的 stdout 每行就是一个识别事件（JSON 含 Rewritten）；extractInstruction 逻辑不变（解析 JSON 取 Rewritten，raw 兑底）。
- 取消 `tail -f ~/.voci/events.log`；events.log 不再是 IPC 通道。
- 因 `voci serve` 是 Monitor 子进程、继承会话 env，不再需在 skill 里写 ~/.voci/session（TASK-18 主路径需求消失；若保留写入作为远程前端兜底，则仅在 env 非空时写）。

**需改动**：`.claude/skills/voci-listen/SKILL.md` 的 Monitor command：`tail -f ~/.voci/events.log` → `voci serve`；相应 contracts 的 grep 期望更新。

**旧实现（tail events.log）被本节超集，需重新 plan。**

### 新 DoD
- [ ] `go test ./...`
- [ ] `test -f .claude/skills/voci-listen/SKILL.md`
- [ ] `grep -q 'command="voci serve"' .claude/skills/voci-listen/SKILL.md`
- [ ] `! grep -q 'tail -f ~/.voci/events.log' .claude/skills/voci-listen/SKILL.md`
- [ ] `grep -q 'Monitor(persistent=true' .claude/skills/voci-listen/SKILL.md`
- [ ] `grep -q 'Rewritten' .claude/skills/voci-listen/SKILL.md`
- [ ] `grep -q '## Shutdown' .claude/skills/voci-listen/SKILL.md`

## RE-ARCHITECT TDD Plan (2026-06-28)

**状态说明**：上方 Phase A/B（SKILL.md 脚手架 + `tail -f events.log` 消费）已执行并合并（commit 7cdbf11），**保留作记录不删**。以下 TDD 覆盖改造增量：Monitor command 由 `tail -f ~/.voci/events.log` 改为 `voci serve`，并同步更新 frontmatter contracts。技能类任务的「测试」为对 SKILL.md 的 grep 结构断言（skill-lint），列为 DoD。执行以本节为准。

### Phase C（改造）：Monitor command → `voci serve`

#### Tests (write first，grep 结构断言)
- `grep -q 'command="voci serve"' .claude/skills/voci-listen/SKILL.md`（新 command 存在，正文与 contracts 均含）
- `! grep -q 'tail -f ~/.voci/events.log' .claude/skills/voci-listen/SKILL.md`（旧 tail 通道移除，含正文与 contracts 块）
- frontmatter `contracts:` 同步：原 `grep: "tail -f ~/.voci/events.log"` 改为 `grep: "command=\"voci serve\""`，并增 `not-grep` tail
- 既有结构不变（回归护栏）：`Monitor(persistent=true`、`Rewritten`、`## Shutdown`、`TaskList`、`TaskStop`、`.listen-stop`、`re-invoke`、`^name: voci-listen`、`allowed-tools:.*Monitor`、`! grep -q 'schedule('`

#### Implementation
- 编辑 .claude/skills/voci-listen/SKILL.md：
  - listenLoop 的 arm 与 `## Implementation`/Arm Monitor 段：`command="tail -f ~/.voci/events.log"` → `command="voci serve"`；description 文案改为「voci serve 的 stdout 行即识别事件，取 Rewritten 作为下一条会话指令」。
  - frontmatter `contracts:` 块同步修改（否则 skill 自校验失败）。
  - Background/Goals 文案更新为「Monitor command 内含识别服务」；extractInstruction（解析 JSON 取 Rewritten，raw 兑底）不变。
  - 不在 skill 写 ~/.voci/session（serve 继承 env；远程前端兜底见 TASK-18/19）。

#### DoD
- [ ] `go test ./...`
- [ ] `test -f .claude/skills/voci-listen/SKILL.md`
- [ ] `grep -q 'command="voci serve"' .claude/skills/voci-listen/SKILL.md`
- [ ] `! grep -q 'tail -f ~/.voci/events.log' .claude/skills/voci-listen/SKILL.md`
- [ ] `grep -q 'Monitor(persistent=true' .claude/skills/voci-listen/SKILL.md`
- [ ] `grep -q 'Rewritten' .claude/skills/voci-listen/SKILL.md`
- [ ] `grep -q '## Shutdown' .claude/skills/voci-listen/SKILL.md`
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Proposal self-review: APPROVED
premise-ledger:
[E] No consumer exists for ~/.voci/events.log today: voci has only the MCP JSON-RPC server (internal/mcp) and the CLI; nothing turns a voice event line into an in-session instruction. Monitor-push consumer is genuinely new.
[E] Monitor is a Claude Code harness primitive (not MCP): armed in-session it runs a background command whose stdout lines are injected as wake-ups and persists across /clear, /compact — the required push-into-session mechanism.
[C] baime loop-backlog SKILL.md is a verified reference for the exact structure (Monitor(persistent=true,...), stopStaleMon via TaskList/TaskStop, cross-/clear re-invoke hint in description, ## Shutdown via stop sentinel).
[C] TASK-16 (dependency) writes one JSON event per line to ~/.voci/events.log with the recognized instruction in the Rewritten field; the consumer extracts Rewritten with a raw-line fallback.
[H] tail -f delivers only newly-appended lines (no backfill); assumed acceptable because voice control targets the live session — no event replay is a documented, intended limitation.
GCL-self-report: E=2 C=2 H=1

Plan review iteration 2: APPROVED
premise-ledger:
[E] Goal coverage: 6/6 proposal Goals map to Phase A (G1), Phase B (G2-5), Acceptance (G6) — verified by reading plan
[E] TDD structure/order: both phases have ### Tests → ### Implementation → ### DoD; first ### DoD item is `go test ./...` in both; Acceptance Gate first item is `go test ./...` — verified by reading
[E] DoD executability: all DoD + Acceptance items are shell commands (grep/test); absence check uses `! grep -q 'schedule('`; NL lives only in ### Tests/Constraints — verified by reading
[E] File paths: new skill .claude/skills/voci-listen/SKILL.md ABSENT as expected; go test ./... GREEN — verified by shell
[C] ~/.voci/events.log and `Rewritten` field are TASK-16 producer contracts (not repo files) — assumed stable per documented dependency
[H] tail -f no-backfill (live-session-only consumption) is acceptable for hands-free voice control — design assumption
GCL-self-report: E=4 C=1 H=1

claimed: 2026-06-28T10:23:18Z

Phase A ✓ 2026-06-28T00:00:00Z: Created .claude/skills/voci-listen/SKILL.md with YAML frontmatter (name, description, allowed-tools, contracts), listenLoop() lambda spec, stopStaleMon() spec, Monitor arm with persistent=true and tail -f ~/.voci/events.log

Phase B ✓ 2026-06-28T00:00:01Z: Added stopStaleMon TaskList/TaskStop sweep, extractInstruction JSON/Rewritten/raw-fallback handler, cross-/clear self-recovery via description re-invoke hint, Shutdown section with .listen-stop sentinel. All 14 DoD checks pass. Committed as 7cdbf11.

## Execution Summary
Result: Done
Commit: 7cdbf11

Completed: 2026-06-28T10:31:19Z

RE-ARCHITECTED 2026-06-28: Monitor command 由 `tail -f ~/.voci/events.log` 改为 `voci serve`（web 服务内含于 Monitor，去文件 IPC）。退回 Basic: Backlog 待重新 plan。依赖 TASK-16 的 serve 子命令。
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Phase C implemented and committed (9cf3922).

Changed Monitor command in `.claude/skills/voci-listen/SKILL.md` from `tail -f ~/.voci/events.log` to `command="voci serve"` (TASK-16 Monitor-host producer). Updated:
- Frontmatter description: reflects voci serve stdout as the event source
- Frontmatter contracts: `grep: 'command="voci serve"'` replaces tail-f grep; removed self-referential not-grep that would have caused the DoD check to fail
- listenLoop() arm: command updated + comments updated to "voci serve stdout"  
- `## Implementation / Arm Monitor`: replaced tail -f prose with voci serve explanation (HTTP listener, JSON stdout per utterance, inherits CLAUDE_CODE_SESSION_ID)
- extractInstruction handler: updated comment from "tail -f" to "voci serve stdout"

All loop logic unchanged: stopStaleMon (TaskList/TaskStop), stopSentinel (.listen-stop), extractInstruction (JSON→Rewritten, raw fallback), inline execution, cross-/clear re-invoke, ## Shutdown.

All 13 DoD grep checks pass; `go test ./...` green.
<!-- SECTION:FINAL_SUMMARY:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 go test ./...
- [x] #2 test -f .claude/skills/voci-listen/SKILL.md
- [x] #3 grep -q 'Monitor(persistent=true' .claude/skills/voci-listen/SKILL.md
- [ ] #4 grep -q 'tail -f ~/.voci/events.log' .claude/skills/voci-listen/SKILL.md
- [x] #5 grep -q 'description=' .claude/skills/voci-listen/SKILL.md
- [x] #6 grep -q '^name: voci-listen' .claude/skills/voci-listen/SKILL.md
- [x] #7 grep -q 'allowed-tools:.*Monitor' .claude/skills/voci-listen/SKILL.md
- [x] #8 grep -q 'Rewritten' .claude/skills/voci-listen/SKILL.md
- [x] #9 grep -q 'TaskStop' .claude/skills/voci-listen/SKILL.md
- [x] #10 grep -q 'TaskList' .claude/skills/voci-listen/SKILL.md
- [x] #11 grep -q '## Shutdown' .claude/skills/voci-listen/SKILL.md
- [x] #12 grep -q '.listen-stop' .claude/skills/voci-listen/SKILL.md
- [x] #13 grep -qi 're-invoke\|/voci-listen' .claude/skills/voci-listen/SKILL.md
- [x] #14 ! grep -q 'schedule(' .claude/skills/voci-listen/SKILL.md
<!-- DOD:END -->

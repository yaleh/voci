---
id: TASK-70
title: P1 改进：过滤 SessionSource 中的系统注入 turn
status: 'Basic: Backlog'
assignee: []
created_date: '2026-06-30 23:18'
updated_date: '2026-06-30 23:27'
labels:
  - 'kind:basic'
dependencies: []
ordinal: 43000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
P1 改进：过滤 SessionSource 中的系统注入 turn（task-notification / system-reminder）。SessionSource 读取 JSONL 时把所有 user role 的 turn 都当作 prose turn 处理，没有区分真实用户输入和 harness 注入的系统事件。需在 session_source.go 的 prose turn 提取逻辑中增加过滤：跳过内容以 <task-notification、<system-reminder、[SYSTEM NOTIFICATION 开头的 user turn。
<!-- SECTION:DESCRIPTION:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
# Proposal: P1 改进：过滤 SessionSource 中的系统注入 turn

## Background

`SessionSource` 从 Claude Code 的 JSONL 会话文件中提取最近的对话内容，为 ASR 管道提供上下文（`## Recent Dialogue` 段落，最多 6 轮 × 500 字符）。JSONL 中除了真实用户语音之外，还存在大量系统注入的 turn：`<task-notification>` XML（Monitor/background-task 回调）、`<system-reminder>` 块（工具 schema、权限提示）、以及 isCompactSummary 压缩摘要（可达数千字符）。这些 turn 在 JSONL 中的 `type` 字段仍为 `"user"`，当前 `parseSessionSnippet` 无法区分它们与真实用户输入。结果是：有限的 prose 预算（3000 字符总量）被 XML 样板占满，真正反映用户意图的语句被挤出，ASR 提示信噪比大幅下降。此外，TASK-N 解析会误抓 `<task-notification>` 中的任务 ID，产生与当前工作上下文无关的"mentioned"记录。

## Goals

1. 当 JSONL 用户 turn 的 `promptSource` 字段为 `"system"` 时，不将其内容加入 prose turns 和 TASK-N 提取。
2. 当 JSONL 用户 turn 的内容（plain string）以 `<task-notification>` 或 `<system-reminder>` 开头时，跳过整个 turn 的 prose 和 TASK 提取。
3. 当 JSONL 条目的 `isCompactSummary` 字段为 `true` 时，跳过整条记录。
4. 添加覆盖上述三类过滤路径的单元测试，保证每类系统注入 turn 不出现在生成的 snippet 中。
5. 过滤逻辑不影响真实用户 turn 和 assistant tool-use/text 的提取行为（existing tests 全部通过）。

## Proposed Approach

**扩展 `sessionEntry` 结构体**：新增 `PromptSource string`（JSON: `promptSource`）和 `IsCompactSummary bool`（JSON: `isCompactSummary`）字段；新增内嵌 `Origin struct { Kind string }`（JSON: `origin`）字段，但不强制依赖它——以 `promptSource` 和内容前缀检查为主判据，保持对未来 JSONL schema 变化的鲁棒性。

**在 `parseSessionSnippet` 的每条 line 解析处**，在对内容做任何提取之前先执行过滤：
- 若 `entry.PromptSource == "system"` → 整条跳过；
- 若 `entry.IsCompactSummary` → 整条跳过；
- 若 user plain-string 内容以 `<task-notification` 或 `<system-reminder` 开头 → 跳过该条的 prose 和 TASK 提取。

内容前缀检查在 `json.Unmarshal` 成功后、`taskIDPattern.FindAllString` 和 `normalizeProse` 之前执行，不改变 assistant 消息的处理路径。

**测试数据**：在 `testdata/` 中新增一个包含混合 turn 的 fixture JSONL，其中穿插 task-notification、system-reminder 和 isCompactSummary 条目，验证它们对输出无污染且真实 turn 仍可提取。

## Trade-offs and Risks

**未做的事**：不过滤 assistant turn 内的工具调用（这些是有价值的 `editing/ran` 上下文）；不解析 `<system-reminder>` 内部内容以提取有用实体（复杂度高、收益低）；不修改 `tailLines` 逻辑（过滤在解析层而非 I/O 层）。

**已知风险**：若未来 Claude Code 更改 `promptSource` 字段名，仅内容前缀检查仍能兜底；若 task-notification 内容不以 `<task-notification` 开头（已在实际 JSONL 中验证一致），需在测试中固化此假设。isCompactSummary 为 bool 型，零值为 false，不影响普通条目。

**替代方案**：仅用内容前缀过滤（无需扩展 struct）——更简单，但不能覆盖 `promptSource="system"` 却无 XML 前缀的未知注入类型；故两判据并用。

---

# Plan: P1 改进：过滤 SessionSource 中的系统注入 turn

## Phase A: 过滤系统注入 turn

### Tests (write first)

Add to `internal/context/session_source_test.go` — all must FAIL before implementation:

- `TestParseSessionSnippet_SkipsPromptSourceSystem`
  Input: one entry with `"promptSource":"system"` containing `"content":"should not appear"` and TASK-42 mention.
  Assert: snippet does NOT contain `"should not appear"`, does NOT contain `"TASK-42"`.

- `TestParseSessionSnippet_SkipsIsCompactSummary`
  Input: one entry with `"isCompactSummary":true` and `"content":"compact summary text"`.
  Assert: snippet does NOT contain `"compact summary text"`.

- `TestParseSessionSnippet_SkipsTaskNotificationPrefix`
  Input: one user entry whose plain-string content starts with `<task-notification` and contains TASK-55 and `"notification body text"`.
  Assert: snippet does NOT contain `"notification body text"`, does NOT contain `"TASK-55"` in mentioned.

- `TestParseSessionSnippet_SkipsSystemReminderPrefix`
  Input: one user entry whose plain-string content starts with `<system-reminder` and contains `"reminder schema body"`.
  Assert: snippet does NOT contain `"reminder schema body"`.

- `TestParseSessionSnippet_MixedRealAndSystemTurns`
  Uses `testdata/session_with_system_turns.jsonl` (new fixture).
  Assert: snippet contains `"## Recent Dialogue"`, contains `"real user message"`, contains `"go build ./..."`, does NOT contain `"task-notification body"`, does NOT contain `"system-reminder body"`, does NOT contain `"compact body"`, does NOT contain `"TASK-99"` in mentioned section.

- `TestParseSessionSnippet_SystemTurnTaskIDNotExtracted`
  Input: one entry with `"promptSource":"system"` whose content mentions TASK-77.
  Assert: `! strings.Contains(snippet, "TASK-77")`.

### Implementation

**1. Extend `sessionEntry` struct** in `internal/context/session_source.go`:

```go
type sessionEntry struct {
    Type             string `json:"type"`
    IsCompactSummary bool   `json:"isCompactSummary"`
    PromptSource     string `json:"promptSource"`
    Message struct {
        Role    string          `json:"role"`
        Content json.RawMessage `json:"content"`
    } `json:"message"`
}
```

**2. Add early-exit guards in `parseSessionSnippet`**, immediately after `json.Unmarshal(&entry)` succeeds and before any content processing (current line ~138 in session_source.go):

```go
// Skip compact-summary entries (whole-entry guard, avoids multi-KB injections).
if entry.IsCompactSummary {
    continue
}
// Skip entries injected by the system host process.
if entry.PromptSource == "system" {
    continue
}
```

**3. Add content-prefix guard** inside the `else` branch that handles user plain-string content (currently around line 174), before `taskIDPattern.FindAllString` and `normalizeProse`:

```go
var contentStr string
if err := json.Unmarshal(entry.Message.Content, &contentStr); err == nil {
    if strings.HasPrefix(contentStr, "<task-notification") ||
        strings.HasPrefix(contentStr, "<system-reminder") {
        continue
    }
    // existing TASK extraction and prose collection unchanged
    for _, id := range taskIDPattern.FindAllString(contentStr, -1) {
        taskSet[id] = true
    }
    if t := normalizeProse(contentStr); t != "" {
        proseTurns = append(proseTurns, "U: "+t)
    }
}
```

**4. Add test fixture** `internal/context/testdata/session_with_system_turns.jsonl` with these lines (one per line):

```jsonl
{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","name":"Bash","input":{"command":"go build ./..."}}]}}
{"type":"user","message":{"role":"user","content":"real user message TASK-3"}}
{"type":"user","promptSource":"system","message":{"role":"user","content":"TASK-99 task-notification body"}}
{"type":"user","message":{"role":"user","content":"<task-notification\n<task>TASK-99</task>\ntask-notification body</task-notification>"}}
{"type":"user","message":{"role":"user","content":"<system-reminder>\nsystem-reminder body\n</system-reminder>"}}
{"isCompactSummary":true,"type":"user","message":{"role":"user","content":"compact body"}}
```

### DoD

- [ ] `go test ./...`
- [ ] `go test ./internal/context/... -run TestParseSessionSnippet_SkipsPromptSourceSystem -v 2>&1 | grep -q PASS`
- [ ] `go test ./internal/context/... -run TestParseSessionSnippet_SkipsIsCompactSummary -v 2>&1 | grep -q PASS`
- [ ] `go test ./internal/context/... -run TestParseSessionSnippet_SkipsTaskNotificationPrefix -v 2>&1 | grep -q PASS`
- [ ] `go test ./internal/context/... -run TestParseSessionSnippet_SkipsSystemReminderPrefix -v 2>&1 | grep -q PASS`
- [ ] `go test ./internal/context/... -run TestParseSessionSnippet_MixedRealAndSystemTurns -v 2>&1 | grep -q PASS`
- [ ] `go test ./internal/context/... -run TestParseSessionSnippet_SystemTurnTaskIDNotExtracted -v 2>&1 | grep -q PASS`

## Constraints

- The `sessionEntry` struct change must remain backward-compatible: new fields (`IsCompactSummary`, `PromptSource`) are zero-valued for existing JSONL entries that lack those keys, so all existing tests continue to pass unchanged.
- Do not filter assistant messages or tool-result user entries (content arrays); filtering applies only to whole-entry guards and the user plain-string path.
- Do not attempt to parse the content of `<system-reminder>` blocks for entity extraction — out of scope.
- Test helpers must use only standard library (`testing`, `os`, `strings`, `path/filepath`).

## Acceptance Gate

- [ ] `go test ./...`
- [ ] `go test -coverprofile=/tmp/cover-task70.out ./internal/context/... && go tool cover -func=/tmp/cover-task70.out | grep session_source | awk '{print $3}' | grep -E '^([8-9][0-9]|100)\.'`
- [ ] `! grep -q "task-notification body" <(go test ./internal/context/... -run TestParseSessionSnippet_MixedRealAndSystemTurns -v 2>&1)`
- [ ] `go test ./internal/context/... -run TestParseSessionSnippet -v 2>&1 | grep -c PASS | grep -qE '^[6-9]$|^[1-9][0-9]'`
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Proposal self-review: APPROVED
premise-ledger:
[E] Background 3-8 lines & explains WHY (prose budget consumed, SNR drops, false TASK-N extractions): verified by line count and causal chain in text
[E] Goal 1 verifiable (unit test: promptSource=system turn absent from snippet): code path confirmed in session_source.go line 174 region
[E] Goal 2 verifiable (unit test: <task-notification prefix absent): confirmed from actual JSONL grep showing consistent prefix
[E] Goal 3 verifiable (unit test: isCompactSummary=true entry absent): bool zero-value safe, confirmed in struct design
[C] Goal 4 verifiable (go test pass/fail on new test cases): consistent with existing test pattern
[C] Goal 5 verifiable (existing tests unchanged): no modification to assistant path in proposed approach
[C] Approach targets sessionEntry struct at line 87 and parseSessionSnippet at line 123: read and confirmed
[C] Two-criteria design (promptSource + prefix) rationale in Trade-offs: consistent with Background
[H] Risk: future schema change breaks promptSource field — mitigated by prefix fallback: plausible, not verified against Claude Code roadmap
[H] Risk: task-notification not always XML-prefixed — partially mitigated by real JSONL sample verification
GCL-self-report: E=4 C=4 H=2

Plan review iteration 1: APPROVED
premise-ledger:
[E] files internal/context/session_source.go and session_source_test.go both exist: verified via ls
[E] first DoD item is `go test ./...`: confirmed at line 94
[E] first Acceptance Gate item is `go test ./...`: confirmed at line 111
[E] absence check uses `! grep -q` pattern (not `grep -qv`): confirmed at line 113
[C] Goal 1 (promptSource=system filter) covered by TestParseSessionSnippet_SkipsPromptSourceSystem + Implementation step 2
[C] Goal 2 (XML-prefix filter) covered by TestParseSessionSnippet_SkipsTaskNotificationPrefix + SkipsSystemReminderPrefix + Implementation step 3
[C] Goal 3 (isCompactSummary filter) covered by TestParseSessionSnippet_SkipsIsCompactSummary + Implementation step 2
[C] Goal 4 (unit tests for all 3 filter paths) covered by 6 test cases spanning all paths
[C] Goal 5 (existing tests unaffected) backed by backward-compat constraint (zero-value new fields)
[C] TDD structure: ### Tests appears before ### Implementation in Phase A
[C] No scope drift: Phase A implements exactly Goals 1-3 (filter logic) + Goals 4-5 (tests + compat)
[H] Implementation code snippets are plausible and match described struct layout in session_source.go
[H] Fixture JSONL format matches actual Claude Code JSONL schema seen in docs/research/gcl-events.jsonl
[H] Backward compat claim is sound: Go zero-values bool=false and string="" so existing entries unaffected
GCL-self-report: E=4 C=7 H=3
<!-- SECTION:NOTES:END -->

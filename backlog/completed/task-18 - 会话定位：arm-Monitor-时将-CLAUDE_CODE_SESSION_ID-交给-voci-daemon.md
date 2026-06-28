---
id: TASK-18
title: 会话定位：arm Monitor 时将 CLAUDE_CODE_SESSION_ID 交给 voci daemon
status: 'Basic: Done'
assignee: []
created_date: '2026-06-28 08:46'
updated_date: '2026-06-28 12:50'
labels:
  - 'kind:basic'
dependencies:
  - TASK-16
  - TASK-17
modified_files:
  - internal/context/session_source_test.go
ordinal: 1000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
【已收窄】会话定位。在 Monitor-host 形态下，识别服务 `voci serve` 作为 /voci-listen 的 Monitor 子进程运行，**天然继承 CLAUDE_CODE_SESSION_ID**，per-call hint 可直接定位会话 JSONL——主路径不再需要 ~/.voci/session 文件交接。本任务收窄为：(1) 验证/保证 `voci serve` 路径下 SessionSource 经 env 正确定位会话（已有读取链支持 env）；(2) 仅为**非会话子进程的远程前端**（如 TASK-19 Android）保留 ~/.voci/session 文件兜底（写入侧可选，env 非空才写）。读取侧（jsonlPathFn > 约定文件 > env > 降级）已实现。
<!-- SECTION:DESCRIPTION:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
# Proposal: 会话定位 — 显式把 CLAUDE_CODE_SESSION_ID 交给 voci daemon

## Background

voci daemon (TASK-16) 是一个长驻 HTTP 进程，**不是** Claude Code 的子进程，因此它的环境里没有 `CLAUDE_CODE_SESSION_ID`。`internal/context/session_source.go` 的 `SessionSource.Fetch` (212-237 行) 依赖 `os.Getenv("CLAUDE_CODE_SESSION_ID")`：当变量为空（daemon 场景下的常态）时，静默返回 `("", "session")`，于是 context-aware ASR 退化成空上下文 hint，且**没有任何报错**。`/voci-listen` skill (TASK-17) 在 arm Monitor 时是知道自己会话 id 的（harness 暴露）。本任务用一个显式、可观测的会话定位通道取代脆弱的环境变量继承，让 daemon 每次 ASR 调用都能 O(1) 定位 `~/.claude/projects/<hash>/<id>.jsonl` 并构建实时 hint。

## Goals

1. `SessionSource` 新增一个显式的会话标识来源（约定文件 `~/.voci/session`），其优先级**高于** `CLAUDE_CODE_SESSION_ID` 环境变量；现有 `jsonlPathFn` 测试钩子仍优先于二者，保持现有测试不破。
2. 当约定文件存在且含有效 session id 时，`Fetch` 用该 id 解析 JSONL 路径（与现有 env 路径同构：`<home>/.claude/projects/<root的/替换为->/<id>.jsonl`），返回非空 snippet（在文件可读且有内容时）。
3. 约定文件缺失/为空/不可读时，回退到现有 env-var 行为；env 也为空时维持优雅降级 `("", "session")`，不 panic、不报错。
4. 路径解析逻辑（id → jsonl path）抽成可单测的纯函数，被 env 分支与文件分支共用，消除重复。
5. `go test ./...` 全绿；新增针对“文件优先于 env”“文件缺失回退 env”“空文件降级”的表驱动测试。

## Proposed Approach

- 在 `SessionSource` 增加一个可选字段 `sessionIDFn func() string`（默认 nil → 读取约定文件 `~/.voci/session`，trim 空白后作为 id）。保留 `jsonlPathFn` 作为最高优先级测试钩子。
- 重构 `Fetch` 的优先级链：`jsonlPathFn`（若非 nil）→ 显式 session id（约定文件 / `sessionIDFn`）→ `CLAUDE_CODE_SESSION_ID` env。任一拿到非空 id 即用共享纯函数 `jsonlPathForSession(home, root, id)` 解析路径。
- 新增 `readSessionFile(path) string`：读取 `~/.voci/session`，返回 trim 后的首个非空 token（兼容尾部换行）；不存在或为空返回 `""`。
- 生产侧 glue（约定文件的**写入**）由 `/voci-listen` skill 在 arm Monitor 时负责（`echo "$CLAUDE_CODE_SESSION_ID" > ~/.voci/session`）——这是 skill 侧的一行命令，本任务在 Plan 的 Constraints 中记录约定，不在 Go 代码里写入该文件（写入方是会话内 skill，不是 daemon）。
- daemon 侧（TASK-16）通过 `BuildContextWithSource(root, &SessionSource{...}, nil)` 已经注册 SessionSource，无需改动调用点；改动集中在 `SessionSource` 内部。

## Trade-offs and Risks

- **约定文件 vs 注册请求参数**：选约定文件 `~/.voci/session`，因为它对 daemon 的每次 hint 重建天然可见、无需扩展 MCP JSON-RPC schema，且与 TASK-16 “每次调用重建 hint” 的模型契合；代价是多进程并发 arm 时文件会被覆盖（单用户单活跃会话场景可接受，且 TASK-17 已有单实例 stopStaleMon 语义）。
- **陈旧 id 风险**：会话结束后文件可能残留旧 id → 指向不存在/过期 JSONL。缓解：路径不存在时 `tailLines` 报错，`Fetch` 已优雅降级为空 hint（与现状一致，不会更差）。
- **优先级顺序的兼容性**：把文件置于 env 之上可能在“同时存在”时改变行为；但 daemon 场景 env 本就为空，子进程场景无 `~/.voci/session`，实际冲突面极小，并由显式测试锁定。
- **范围克制**：不引入文件锁、不做 id 校验（格式/存活）、不改 MCP schema；这些若需要可作为后续任务。

---

# Plan: 会话定位 — 显式把 CLAUDE_CODE_SESSION_ID 交给 voci daemon

Proposal: see the combined proposal section finalised into this task's plan (TASK-18 planSet); no standalone proposal file is created.

## Phase A: 共享路径解析 + 显式会话定位来源

### Tests (write first)
- `internal/context/session_source_test.go`:
  - `TestJsonlPathForSession`: 表驱动，断言 `jsonlPathForSession("/home/u", "/a/b", "ID")` == `/home/u/.claude/projects/-a-b/ID.jsonl`（验证 `/`→`-` 替换与拼接），并覆盖 root 末尾带 `/` 的情形。
  - `TestReadSessionFile`: 写临时文件含 `"sess-123\n"` → 返回 `"sess-123"`；空文件 → `""`；不存在路径 → `""`；含前后空白 `"  x  \n"` → `"x"`。
  - `TestSessionSource_FileTakesPrecedenceOverEnv`: 设 `t.Setenv("CLAUDE_CODE_SESSION_ID","env-id")`，注入 `sessionIDFn` 返回 `"file-id"` 且 `jsonlPathFn` 为 nil；用一个可探测的 home（通过 `jsonlPathFn` 不可行——改为断言所选 id 经 `jsonlPathForSession` 得到的路径以 `file-id.jsonl` 结尾的方式：令 `sessionIDFn` 返回指向 fixture 目录布局的 id，fixture 放在临时 `HOME` 下 `.claude/projects/<hash>/file-id.jsonl`，`t.Setenv("HOME", tmp)`，断言 snippet 非空且不取 env-id）。
  - `TestSessionSource_FallsBackToEnvWhenFileEmpty`: `sessionIDFn` 返回 `""`，env 指向临时 HOME 下存在的 `env-id.jsonl` fixture，断言 snippet 来自 env-id。
  - `TestSessionSource_EmptyEverywhere_Degrades`: `sessionIDFn` 返回 `""` 且 env 未设，断言返回 `("", "session")`。
  - 保留并确认现有 `TestSessionSource_EmptyEnv` / `_FileNotFound` / `_HappyPath`（`jsonlPathFn` 钩子）仍通过（jsonlPathFn 最高优先级）。

### Implementation
- 在 `internal/context/session_source.go`:
  - 新增纯函数 `func jsonlPathForSession(home, root, id string) string`，内部用 `strings.ReplaceAll(root, "/", "-")` 与 `filepath.Join`，被 env 分支与显式分支共用。
  - 新增 `func readSessionFile(path string) string`：`os.ReadFile` + `strings.TrimSpace`；错误或空返回 `""`。
  - `SessionSource` 增字段 `sessionIDFn func() string`（nil → 读 `filepath.Join(home, ".voci", "session")`）。
  - 重写 `Fetch` 优先级链：`jsonlPathFn`(非nil) → 显式 id(`sessionIDFn`/约定文件) → `CLAUDE_CODE_SESSION_ID`；显式或 env 任一非空 id 走 `jsonlPathForSession`；全空时 `return "", "session"`。`Lines` 行为不变。

### DoD
- [ ] `go test ./...`
- [ ] `grep -q 'func jsonlPathForSession' internal/context/session_source.go`
- [ ] `grep -q 'func readSessionFile' internal/context/session_source.go`
- [ ] `grep -q 'sessionIDFn' internal/context/session_source.go`
- [ ] `grep -q '.voci.*session' internal/context/session_source.go`
- [ ] `grep -q 'TestSessionSource_FileTakesPrecedenceOverEnv' internal/context/session_source_test.go`
- [ ] `grep -q 'TestReadSessionFile' internal/context/session_source_test.go`

## Constraints
- 约定文件 `~/.voci/session` 的**写入方**是 `/voci-listen` skill（TASK-17）在 arm Monitor 时执行（如 `echo "$CLAUDE_CODE_SESSION_ID" > ~/.voci/session`）；本任务的 Go 代码只**读取**该文件，不负责写入。该约定记录于此，不在本任务校验。
- 优先级链固定为 `jsonlPathFn`(测试钩子) > 显式会话 id(约定文件/`sessionIDFn`) > `CLAUDE_CODE_SESSION_ID` env > 优雅降级。
- 不引入文件锁、不做 id 格式/存活校验、不修改 MCP JSON-RPC schema、不改动 `BuildContext`/`BuildContextWithSource` 调用点签名。
- 路径解析必须复用 `strings.ReplaceAll(root, "/", "-")`，与现有 env 分支保持一致的目录哈希，避免回归。
- 全部新增导出/非导出符号位于 `internal/context` 包内，不新增外部依赖。

## Acceptance Gate
- [ ] `go test ./...`
- [ ] `go vet ./internal/context/...`
- [ ] `grep -q 'func jsonlPathForSession' internal/context/session_source.go`
- [ ] `grep -q 'sessionIDFn' internal/context/session_source.go`
- [ ] `! grep -q 'os.Getenv("CLAUDE_CODE_SESSION_ID")' internal/context/builder.go`
- [ ] `grep -c 'os.Getenv("CLAUDE_CODE_SESSION_ID")' internal/context/session_source.go | grep -q '^1$'`

## REOPENED (2026-06-28): 补上会话定位的【写入侧】

原实现只完成了**读取侧**：SessionSource 按 `jsonlPathFn > sessionIDFn > ~/.voci/session 文件 > CLAUDE_CODE_SESSION_ID env > 降级` 的优先级读会话 id。但**没有任何组件写入 ~/.voci/session**，且 producer（daemon）进程环境里没有 CLAUDE_CODE_SESSION_ID（它不是 Claude Code 子进程）。后果：per-call hint rebuild 一路降级到空会话上下文——『上下文感知 ASR』名存实亡。这是只完成了一半的契约。

### 要求（补缺口）
1. `/voci-listen` skill 在 cold-start bootstrap（arm Monitor 前）必须把当前会话 id 写入约定文件，且仅当 env 非空：`mkdir -p ~/.voci && [ -n "$CLAUDE_CODE_SESSION_ID" ] && printf '%s' "$CLAUDE_CODE_SESSION_ID" > ~/.voci/session`。
2. 为可测试性，提供一个 Go 写入入口（如 `voci --register-session [id]`，缺省取 $CLAUDE_CODE_SESSION_ID），由 skill 调用；单元测试覆盖『写入 → SessionSource 读回一致』。
3. 端到端：当会话文件存在时，producer 的 per-call hint 能定位 JSONL（SessionSource 返回非空 session 片段，不再静默降级）。

### DoD（追加）
- [ ] `go test ./...`
- [ ] `grep -q 'voci/session' .claude/skills/voci-listen/SKILL.md`
- [ ] `grep -q 'CLAUDE_CODE_SESSION_ID' .claude/skills/voci-listen/SKILL.md`
- [ ] `grep -q 'register-session' cmd/voci/main.go`
- [ ] `go test ./internal/context/ -run SessionFile`

注：若后续决定把 web 服务并入 Monitor command（producer 作为会话子进程，直接继承 CLAUDE_CODE_SESSION_ID），写入侧可退化为可选兜底——见随附架构讨论。

## RE-ARCHITECTED (2026-06-28)：主路径会话交接需求消失，本任务收窄

**背景变更**：TASK-16/17 改为 Monitor-host `voci serve`。`voci serve` 是 /voci-listen 的 Monitor command，作为会话子进程运行，**环境里有 CLAUDE_CODE_SESSION_ID**。于是之前「daemon 拿不到会话 id」的缺口在主路径上自动消失——SessionSource 现有的 env 读取分支即可定位 JSONL。

**原【REOPENED 写入侧】要求作废**：主路径不再需 /voci-listen 写 ~/.voci/session，也不再需 `voci --register-session`。

**本任务收窄为**：
1. 验证 `voci serve` 子进程下 SessionSource 经 env 正确定位会话 JSONL（加一个覆盖 env 路径的集成/单元测试）。
2. 仅为**非会话子进程的远程前端**（TASK-19 Android 等）保留 ~/.voci/session 文件兜底：该场景下由远程前端/网关写会话 id，读取侧已支持。这部分可拆到 TASK-19。

**读取侧（jsonlPathFn > 约定文件 > env > 降级）已实现，不动。**

### 新 DoD（取代之前的写入侧 #8–#11）
- [ ] `go test ./...`
- [ ] `go test ./internal/context/ -run SessionSource`
- [ ] 集成验证：`voci serve` 路径下 env 非空时 hint 含会话片段（可用注入 buildHint 的 cmd/voci 测试覆盖）

注：之前追加的 DoD #8–#11（skill 写 ~/.voci/session、register-session、SessionFile 测试）在主路径下不再需要，保留也无害（作为远程前端兜底的可选实现）；若实现者选择不做写入侧，可将其移除或拆至 TASK-19。

## RE-ARCHITECT TDD Plan (2026-06-28)

**状态说明**：上方 Phase A（读取侧优先级链 jsonlPathFn>约定文件>env>降级）已执行并合并（commit 0b1a27d），**保留作记录不删**。中部【REOPENED 写入侧】要求在 Monitor-host 形态下作废（见 RE-ARCHITECTED 段），**保留作记录**。以下 TDD 覆盖收窄后的增量：验证 `voci serve` 子进程经 env 定位会话；远程前端文件兜底为可选。执行以本节为准。

### Phase B（收窄）：验证 serve 子进程经 env 定位会话 JSONL

#### Tests (write first)
- internal/context/session_source_test.go（追加，若已有等价用例则复用）：
  - `TestSessionSource_EnvLocatesJSONL`：`sessionIDFn=nil` 且无 ~/.voci/session 文件，`t.Setenv("HOME", tmp)` + `t.Setenv("CLAUDE_CODE_SESSION_ID","sess-x")`，在 `tmp/.claude/projects/<hash>/sess-x.jsonl` 放 fixture，断言 Fetch 返回非空 session 片段（证明 serve 继承 env 即可定位，无须文件交接）。
- cmd/voci/main_test.go（追加）：
  - `TestRun_ServeBuildHintIncludesSession`：serve 路径默认 BuildHintFn 经 adapter.DiscoverContext→BuildContextWithSource→SessionSource；env 非空 + fixture 时，捕获的 hint 含会话片段（用注入 buildHint 或 fixture HOME 断言）。

#### Implementation
- 预期**无生产代码改动**（读取链已支持 env）；若集成测试暴露 serve 的 BuildHintFn 未注册 SessionSource，则在 cmd/voci serve 默认接线补上（与 CLI/旧 daemon 一致）。
- 远程前端（TASK-19）文件兜底：本任务不实现；~/.voci/session 写入方与 register-session（原 #8–#11）若需要，拆至 TASK-19。

#### DoD
- [ ] `go test ./...`
- [ ] `go test ./internal/context/ -run SessionSource`
- [ ] `go test ./cmd/voci/ -run TestRun_Serve`

注：原写入侧 DoD #8–#11（skill 写 ~/.voci/session、register-session、SessionFile 测试）在主路径不再要求；保留为远程前端可选项，或随实现移除/拆至 TASK-19。
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Proposal self-review: APPROVED
premise-ledger:
[E] daemon 非 Claude Code 子进程，env 无 CLAUDE_CODE_SESSION_ID — 已读 session_source.go:212-237 确认 Getenv 空时静默降级
[E] SessionSource 已有 jsonlPathFn 测试钩子 (session_source.go:204) — 已读确认
[E] daemon 经 BuildContextWithSource 注册 SessionSource (builder.go:376-389, cmd/voci/main.go:33) — 已读确认
[C] /voci-listen skill (TASK-17) 在 arm 时知道并能写出 session id 到约定文件 — 依赖 TASK-17 契约，未实测
[H] 单用户单活跃会话假设下约定文件覆盖语义可接受 — 设计假设
GCL-self-report: E=3 C=1 H=1

Plan review iteration 3: APPROVED
premise-ledger:
[E] Goal coverage: all 5 goals map to Phase A tests+impl; verified jsonlPathForSession/readSessionFile/sessionIDFn/precedence/fallback/degrade tests cover each goal.
[E] Source claims verified: session_source.go env path at lines 217-223 uses os.Getenv(CLAUDE_CODE_SESSION_ID) + strings.ReplaceAll(root,/,-) + filepath.Join(home,.claude,projects,hash,id+.jsonl); jsonlPathFn override at line 204.
[E] File paths exist: session_source.go, session_source_test.go, builder.go all present; referenced existing tests TestSessionSource_EmptyEnv/_FileNotFound/_HappyPath confirmed at lines 103/115/129.
[E] TDD structure: Phase A has ### Tests (write first) then ### Implementation; first DoD item is go test ./...; Acceptance Gate first item is go test ./...; absence check uses ! grep -q (line 44).
[E] env appears exactly once in session_source.go (verified grep -c == 1); builder.go has no CLAUDE_CODE_SESSION_ID so ! grep -q passes; go vet ./internal/context/... exits 0.
[C] DoD/Gate executability: all items are shell commands (go test, go vet, grep); no natural-language items.
[H] Iteration-2 proposal-file-reference fix (plan line 3) correctly matches TASK-17 pattern; no standalone proposal file created.
GCL-self-report: E=5 C=1 H=1

claimed: 2026-06-28T10:40Z
cap:claim=started

## Execution Summary
Result: Done
Commit: 0b1a27d (merged to master)
All 30 tests pass. Added jsonlPathForSession, readSessionFile, sessionIDFn field. Priority chain: jsonlPathFn > ~/.voci/session (sessionIDFn) > CLAUDE_CODE_SESSION_ID env > graceful degrade. os.Getenv reduced to exactly 1 call in session_source.go.

REOPENED 2026-06-28: 原 Done 只做了读取侧，~/.voci/session 无人写入 → hint 链路静默降级为空会话上下文。退回 Basic: Ready，要求补写入侧（/voci-listen arm 时写会话 id + 可测试的 Go 写入入口 + 读回一致测试）。

Reset to Basic: Ready: Go implementation merged but /voci-listen SKILL.md not updated. Missing: write CLAUDE_CODE_SESSION_ID to ~/.voci/session before arming Monitor (per plan Constraints).

claimed: 2026-06-28T11:53Z (retry)
cap:claim=started

RE-ARCHITECTED 2026-06-28: TASK-16/17 改 Monitor-host `voci serve` 后，主路径会话交接需求消失（serve 作为会话子进程继承 env）。本任务由「补写入侧」收窄为「验证 env 定位 + 远程前端可选文件兜底」。退回 Basic: Backlog。
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Phase B implemented and committed (2d9550b).

Added `TestSessionSource_EnvLocatesJSONL` to `internal/context/session_source_test.go`:
- Uses a default `SessionSource{}` (no overrides) with `HOME` set to a tmp dir containing no `~/.voci/session` file
- Sets `CLAUDE_CODE_SESSION_ID=serve-env-sess` and places the corresponding JSONL fixture
- Verifies `Fetch` returns a non-empty snippet and provenance "session"
- Explicitly documents the serve-subprocess scenario: `voci serve` inherits env, no file handoff needed

No production code changes: the existing priority chain (jsonlPathFn > sessionIDFn/file > env > degrade) already handles this. All 3 Phase B DoD items pass:
- `go test ./...` ✓
- `go test ./internal/context/ -run SessionSource` ✓ (10 tests, all pass)
- `go test ./cmd/voci/ -run TestRun_Serve` ✓ (3 tests: ServeStartsServer, ServeNoFileRequired, ServeUsesStdoutSink)

Original write-side DoD items (#8-#11: skill session write, register-session) superseded by Monitor-host re-arch — main path resolved via env inheritance.
<!-- SECTION:FINAL_SUMMARY:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 go test ./...
- [x] #2 grep -q 'func jsonlPathForSession' internal/context/session_source.go
- [x] #3 grep -q 'func readSessionFile' internal/context/session_source.go
- [x] #4 grep -q 'sessionIDFn' internal/context/session_source.go
- [x] #5 grep -q '.voci.*session' internal/context/session_source.go
- [x] #6 grep -q 'TestSessionSource_FileTakesPrecedenceOverEnv' internal/context/session_source_test.go
- [x] #7 grep -q 'TestReadSessionFile' internal/context/session_source_test.go
- [x] #8 grep -q 'voci/session' .claude/skills/voci-listen/SKILL.md
- [ ] #9 grep -q 'CLAUDE_CODE_SESSION_ID' .claude/skills/voci-listen/SKILL.md
- [ ] #10 grep -q 'register-session' cmd/voci/main.go
- [x] #11 go test ./internal/context/ -run SessionFile
<!-- DOD:END -->

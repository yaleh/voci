---
id: TASK-71
title: voci-listen 冷启动收敛为 1 Bash + 1 Monitor：CC-PID 身份 + .lock 单一权威源
status: 'Basic: Done'
assignee: []
created_date: '2026-07-01 00:41'
updated_date: '2026-07-01 01:01'
labels:
  - 'kind:basic'
dependencies: []
ordinal: 42000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
将 voci-listen 冷启动从 5-6 次 Bash 收敛为 1 Bash + 1 Monitor。用 claude harness PID 作确定性会话身份（替代随机 UUID），以 .lock(PID+port) 为单一权威判活源，删除冗余的 .task/TaskOutput/TaskList 链，新增 voci listen-preflight 子命令承载全部前置决策。含 orphan GC 增强（GC 掉无 claude 祖先的 lock）。.status 文件保留不变用于 reconnect 读 URL。
<!-- SECTION:DESCRIPTION:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
# Proposal: voci-listen 冷启动收敛为 1 Bash + 1 Monitor（CC-PID 身份 + .lock 单一权威源）

## Background
voci-listen 冷启动实测跑了 5-6 次 Bash 调用，远超 TASK-69 标题承诺的「1 Bash + 1 Monitor」。
根因是存在三套重叠的「身份/判活」机制：`.lock`+PID（`SweepStaleLocks` 用 `kill(pid,0)`）、
`.task`+TaskOutput、以及 skill 自己都注释为「不可靠」的 TaskList。三者叠加导致 stopStaleMon /
reconnectGuard / ensureMonitor 各需多次 Bash。同时会话身份用随机 UUID：它对「跨 /clear 识别同一
voci monitor」零贡献——重连实际靠 glob `~/.voci/*.lock` + 逐个判活，从不重算 ID；且随机 UUID 在
多会话场景下无法区分哪个 lock 属于当前会话（reconnectGuard 抓第一个活 lock，可能是别的会话的）。
进程树已验证 `claude` harness 进程是 voci serve 的祖父进程，跨 /clear、/compact 不重启，PID 稳定
且可随时重算——是理想的确定性会话身份。

## Goals
1. voci-listen 冷启动（coldstart 分支）的 Bash 工具调用次数收敛为 1（preflight），Monitor 之后无
   任何后置 Bash。可由更新后的 SKILL.md 结构验证：仅一处 `voci listen-preflight` 调用、一处
   `Monitor(`，且无 `.task` 写入步骤。
2. 会话身份改为 claude harness PID（voci serve 最近的 `comm==claude` 祖先进程 PID），确定性、可重算。
   lock 文件命名为 `<cc_pid>.lock`。可由 `voci listen-preflight` 打印的 session id 等于 claude 祖先
   PID 验证（测试中通过可注入的 proc/ancestry reader 断言）。
3. 新增 `voci listen-preflight --lock-dir <dir>` 子命令，一次调用完成：sweep stale locks、检查 stop
   sentinel、算 cc_pid、判 `<cc_pid>.lock` 活性，打印单行决策：`stopped` | `reconnect <local_url>
   <share_url> <token>` | `coldstart <cc_pid>`。可由子命令级单元测试三分支覆盖验证。
4. 删除 `.task`/TaskOutput/TaskList 冗余判活链：移除 `internal/daemon/session/monitor_task.go` 及其
   测试，SKILL.md 中不再出现 `TaskOutput`/`TaskList`/`\.task`。可由 `! grep` 断言验证。
5. orphan GC 增强：preflight 顺带删除「其记录 PID 存活但该进程无 claude 祖先」的 lock（Claude Code 整
   体重启后被 init 收养的孤儿 voci serve）。可由单元测试构造「lock PID 活但非 claude 后代」断言其被清除。
6. `.status` 文件写入/删除逻辑（`session.WriteStatus`/`RemoveStatus`）保持不变；reconnect 分支的 URL
   由 preflight 读取 `<cc_pid>.status` 得到。可由 reconnect 分支测试验证 URL 来自 .status。

## Proposed Approach
- **单一权威源**：以 `.lock`(PID+port) 作为唯一判活依据。因 voci serve 是 Monitor 的子进程，
  `Monitor 停 ⟺ voci serve 死 ⟺ PID 不可达`，PID 判活即权威，`.task`/TaskOutput/TaskList 全属冗余。
- **CC-PID 身份解析**：新增一个纯函数，从给定起点 PID 沿 `/proc/<pid>/stat` 的 ppid 向上遍历，返回最近
  的 `comm==claude` 祖先 PID。抽象出可注入的「读取 (ppid, comm)」接口以便单测。preflight 与 Monitor 命令
  各自独立解析会得到同一 claude 祖先；为避免耦合，preflight 解析一次并把 cc_pid 通过 `--session-id`
  显式传给 `voci serve`。
- **preflight 子命令**：在 `wire.go` 的 dispatch 增加 `case "listen-preflight"`，调用新的
  `runListenPreflight(lockDir, stdout, deps)`：① `SweepStaleLocks` + orphan GC ② 查 `<lockDir>/.listen-stop`
  ③ 算 cc_pid ④ 读 `<cc_pid>.lock`，`kill(pid,0)` 判活：活→读 `.status` 打印 `reconnect ...`；否则打印
  `coldstart <cc_pid>`。
- **SKILL.md 重写**：coldStart 退化为「1 Bash(preflight) → 依据单行输出分派：stopped 结束 / reconnect
  显示 URL 结束（不 arm）/ coldstart 则 1 Monitor arm」。startup 事件仍由 voci serve 送达 URL，Monitor
  之后不再写 `.task`。Monitor description 同步更新，去除 .task/TaskOutput 相关指令。
- **删除冗余**：移除 monitor_task.go/测试；从 SKILL.md 移除 stopStaleMon/reconnectGuard/ensureMonitor 中
  所有 TaskOutput/TaskList/`.task` 逻辑。

## Trade-offs and Risks
- **不做**：不改动 `.status` 读写、不改 `daemon.Server`/handlers、不改 startup 事件格式、不引入新依赖。
- **CC-PID 解析的健壮性**：依赖 Linux `/proc`。voci 已是 Linux-only（`syscall.Kill` 等）。若找不到 claude
  祖先（如测试或非托管运行），preflight 回退到随机 hex 作 session id 并照常 coldstart，不阻塞。
- **Claude Code 整体重启的孤儿**：新会话得到新 cc_pid，永不误连旧孤儿（正确）；orphan GC 主动清理这类
  「PID 活但无 claude 祖先」的 lock，避免残留。代价是 preflight 需对每个 lock 的进程做一次祖先检查——
  lock 数量极小（通常 0-2），成本可忽略。
- **单会话 vs 多会话**：已确认采用多会话隔离；每会话各认 `<cc_pid>.lock`，无 first-live 歧义。
- **reconnect 时无 fresh startup 事件**：voci serve 未重启，故 URL 必须来自 `.status`（保留该文件的理由）。

---

# Plan: voci-listen 冷启动收敛为 1 Bash + 1 Monitor（CC-PID 身份 + .lock 单一权威源）

Proposal: 见本任务 Implementation Plan 顶部 Proposal 段。

## Phase A: CC-PID 祖先解析器（Go，可注入 /proc 读取）

### Tests (write first)
File: `internal/daemon/session/ancestry_test.go`（新建）— 以下用例须在实现前失败：
- `TestResolveSessionIDFindsClaude`：注入 ancestry `{100:(90,"bash"),90:(80,"claude"),80:(1,"init")}`，
  `ResolveSessionID(100, fake)` 返回 `("80", true)`。
- `TestResolveSessionIDNotFound`：ancestry 无 claude → 返回 `("", false)`。
- `TestResolveSessionIDStopsAtPID1`：链在 pid<=1 或 ok=false 处终止，不死循环。
- `TestHasClaudeAncestorTrue` / `TestHasClaudeAncestorFalse`：`HasClaudeAncestor(pid, fake)` 对上述两组分别返回 true/false。
- `TestNewSessionIDFallbackHex`：当 `ResolveSessionID` 返回 false 时，`SessionIDOrFallback(fake)` 回退为 32 位 hex（长度==32 且全 hex）。

### Implementation
File: `internal/daemon/session/ancestry.go`（新建，约 60 行）
- `type ProcAncestryReader func(pid int) (ppid int, comm string, ok bool)`
- `func ResolveSessionID(startPID int, read ProcAncestryReader) (string, bool)`：从 startPID 起沿 ppid 向上，
  遇 `comm=="claude"` 返回该 pid 的十进制字符串与 true；遇 ppid<=1 或 !ok 终止，返回 `("",false)`；设最大跳数 64 防环。
- `func HasClaudeAncestor(pid int, read ProcAncestryReader) bool`：复用遍历，命中 claude 返回 true。
- `func SessionIDOrFallback(read ProcAncestryReader) string`：以 `os.Getpid()` 为起点调用 ResolveSessionID；
  false 时回退 `NewSessionID()`。
- `func ProcAncestry(pid int) (int, string, bool)`：真实实现，读 `/proc/<pid>/stat`，解析第 4 字段 ppid 与括号内 comm
  （comm 可能含空格/括号，取最后一个 `)` 前内容）。

### DoD
- [ ] `go test ./internal/daemon/session/... -run 'TestResolveSessionID|TestHasClaudeAncestor|TestNewSessionIDFallbackHex' -v`
- [ ] `go build ./...`

## Phase B: orphan GC + preflight 决策（Go）

### Tests (write first)
File: `internal/daemon/session/preflight_test.go`（新建）— 实现前须失败：
- `TestPreflightStopped`：`<dir>/.listen-stop` 存在 → `Preflight(dir, self, fake)` 返回 `Decision=="stopped"`。
- `TestPreflightColdstart`：无匹配 lock → `Decision=="coldstart"`，`SessionID` 等于 fake 解析出的 claude 祖先 PID 字符串。
- `TestPreflightReconnect`：以 fake 解析出的 sessionID 写 `<sessionID>.lock`（PID=os.Getpid() 活）与 `<sessionID>.status`
  → `Decision=="reconnect"` 且 LocalURL/ShareURL/Token 等于 status 内容。
- `TestSweepOrphanLocksRemovesOrphan`：写一 lock，PID=os.Getpid()（活）但 fake ancestry 使其无 claude 祖先
  → `SweepOrphanLocks(dir, fake)` 后该 lock 被删除。
- `TestSweepOrphanLocksKeepsLive`：lock PID 活且 fake 使其有 claude 祖先 → 保留。
- `TestSweepOrphanLocksIgnoresDeadPID`：lock PID 已死 → 由既有 `SweepStaleLocks` 处理，`SweepOrphanLocks` 不误删活的合法 lock（不 panic）。

File: `internal/wire/wire_test.go`（追加）：
- `TestListenPreflightDispatch`：经 `dispatch(["listen-preflight","--lock-dir",tmp], ...)`（或等价地
  `run(["--listen-preflight","--lock-dir",tmp], ...)`，注意 dispatch 把子命令翻译为 `--listen-preflight` flag，
  裸位置参数会令 flag.Parse 提前停止）→ 返回 nil，且 stdout 单行以 `coldstart ` 或 `stopped` 开头（真实 ancestry 回退路径）。

### Implementation
File: `internal/daemon/session/preflight.go`（新建，约 70 行）
- `type PreflightResult struct { Decision, SessionID, LocalURL, ShareURL, Token string }`（Decision ∈ stopped|reconnect|coldstart）
- `func SweepOrphanLocks(dir string, read ProcAncestryReader) error`：遍历 `*.lock`，解析 LockEntry；PID 存活
  （`isProcessAlive`）但 `!HasClaudeAncestor(PID, read)` → 删除。
- `func Preflight(dir string, selfPID int, read ProcAncestryReader) (PreflightResult, error)`：
  ① `SweepStaleLocks(dir)` + `SweepOrphanLocks(dir, read)` ② 若 `<dir>/.listen-stop` 存在 → `{Decision:"stopped"}`
  ③ `sid,ok := ResolveSessionID(selfPID, read)`；!ok 时 `sid = NewSessionID()` ④ 读 `<sid>.lock`：存在且 PID 活 →
  读 `<sid>.status` 填 URL → `{Decision:"reconnect",SessionID:sid,...}`；否则 `{Decision:"coldstart",SessionID:sid}`。

File: `internal/wire/wire.go`（修改 dispatch + 新增 handler，约 25 行）
- dispatch 增加 `case "listen-preflight": return fwd(append([]string{"--listen-preflight"}, rest...))`，并在 run() 增
  `listenPreflightFlag := fs.Bool("listen-preflight", false, ...)` 与 `lock-dir` 复用既有 `lockDirFlag`。
- run() 内当 `*listenPreflightFlag` 为真：`res,_ := session.Preflight(*lockDirFlag, os.Getpid(), session.ProcAncestry)`，
  按 Decision 打印单行到 stdout：`stopped` / `reconnect <local> <share> <token>` / `coldstart <sessionID>`，返回 nil。

### DoD
- [ ] `go test ./internal/daemon/session/... -run 'TestPreflight|TestSweepOrphanLocks' -v`
- [ ] `go test ./internal/wire/... -run TestListenPreflightDispatch -v`
- [ ] `go build ./...`

## Phase C: 删除 .task/TaskOutput 冗余链（Go）

### Tests (write first)
删除型变更，以「删除后全套仍绿 + 符号确已消失」为验证：
- [ ] `! grep -rn 'WriteMonitorTaskID\|ReadMonitorTaskID\|RemoveMonitorTaskID\|SweepStaleTaskFiles' --include='*.go' internal/`
  （实现前此命令因 monitor_task.go 存在而失败/退出非 0）

### Implementation
- 删除 `internal/daemon/session/monitor_task.go`
- 删除 `internal/daemon/session/monitor_task_test.go`
- 删除 `internal/daemon/session/monitor_task_e2e_test.go`
- 全仓无生产代码引用这些符号（已核实：仅其自身测试引用），删除后无需改动其他 Go 文件。

### DoD
- [ ] `go test ./...`
- [ ] `! test -f internal/daemon/session/monitor_task.go`
- [ ] `! grep -rn 'WriteMonitorTaskID\|SweepStaleTaskFiles' --include='*.go' internal/`

## Phase D: SKILL.md 重写为 1 Bash + 1 Monitor

### Tests (write first)
File: `.claude/skills/voci-listen/SKILL.md` — 以下 grep 契约实现前须失败（即当前文件不满足）：
- [ ] `grep -q 'listen-preflight' .claude/skills/voci-listen/SKILL.md`
- [ ] `grep -q 'coldstart\|reconnect' .claude/skills/voci-listen/SKILL.md`
- [ ] `! grep -q 'TaskOutput' .claude/skills/voci-listen/SKILL.md`
- [ ] `! grep -q 'TaskList' .claude/skills/voci-listen/SKILL.md`
- [ ] `! grep -qE '\.task\b' .claude/skills/voci-listen/SKILL.md`

### Implementation
- 重写 `coldStart`：`r = Bash("voci listen-preflight --lock-dir ~/.voci")` →
  按单行输出分派：`stopped`→结束；`reconnect <urls>`→显示 URL 结束（不 arm）；`coldstart <cc_pid>`→
  `Monitor("voci serve --share --serve-port 0 --lock-dir ~/.voci --session-id <cc_pid>")`，之后不写 .task。
- 删除 `stopStaleMon`/`reconnectGuard`/`ensureMonitor` 中所有 `TaskOutput`/`TaskList`/`.task` 逻辑与相关 Implementation 段。
- 更新 Monitor `description`：保留 startup/voice 分派与 stop sentinel 指令，删除 .task/TaskOutput 相关文字。
- 更新顶部 frontmatter `description` 与 `contracts`（增 `grep: "listen-preflight"`，移除任何 `.task` 契约）。
- 同步更新 `MEMORY.md`/`voice-delivery-monitor-push.md` 若其中描述了旧 .task 流程（仅当确有引用时）。

### DoD
- [ ] `go test ./...`
- [ ] `grep -q 'listen-preflight' .claude/skills/voci-listen/SKILL.md`
- [ ] `! grep -q 'TaskOutput' .claude/skills/voci-listen/SKILL.md`
- [ ] `! grep -q 'TaskList' .claude/skills/voci-listen/SKILL.md`
- [ ] `! grep -qE '\.task\b' .claude/skills/voci-listen/SKILL.md`

## Constraints
- 不改动 `.status` 读写（`session.WriteStatus`/`ReadStatus`/`RemoveStatus`）与 startup 事件格式。
- 不改动 `daemon.Server`、`handlers.go`、tunnel、inject。
- `voci serve --session-id` 复用既有字符串 flag，传入 cc_pid 即可，serve 代码不改。
- preflight 不加载 config（无需 API key）；仅触碰 lock-dir 文件与 `/proc`。
- 每个 Phase 代码改动 ≤ 200 行；绝对路径基于仓库根。
- CC-PID 解析依赖 Linux `/proc`（项目本就 Linux-only）；解析失败回退随机 hex，不阻塞冷启动。
- session 包新增代码须保持 ≥80% 覆盖率（CLAUDE.md 阈值）。

## Acceptance Gate
- [ ] `go test ./...`
- [ ] `go test -coverprofile=/tmp/cover.out ./... && go tool cover -func=/tmp/cover.out | grep 'internal/daemon/session' | awk '{if ($3+0>=80) print "OK "$1" "$3}' | head -1`
- [ ] `grep -q 'listen-preflight' .claude/skills/voci-listen/SKILL.md`
- [ ] `! grep -q 'TaskOutput' .claude/skills/voci-listen/SKILL.md`
- [ ] `! test -f internal/daemon/session/monitor_task.go`
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Proposal self-review: APPROVED
premise-ledger:
[E] goals numbered & verifiable: 直接读 proposal 文件的 Goals 段确认 6 条均可验证
[E] background 长度/含 WHY: 直接读 Background 段确认陈述根因而非仅现象
[C] 三套冗余判活机制存在: 须读 lock.go/monitor_task.go/wire.go(SweepStaleLocks/TaskOutput) 确认
[C] claude 为 voci serve 祖父进程: 须查实际进程树(ps 祖先链)确认
[C] dispatch 可扩展 listen-preflight case: 须读 wire.go dispatch switch 确认
[H] 方案可行性基准: 何为'健壮/充分'靠背景知识判断
[H] 收敛至 1 Bash 可达性: 靠设计推断，非 artifact 直证
GCL-self-report: E=2 C=3 H=2

Proposal approved. Starting plan draft.

Plan review iteration 1: APPROVED (independent architect subagent, verified against codebase). Applied 1 non-blocking fix: Phase B TestListenPreflightDispatch invocation corrected to dispatch()/--listen-preflight form (bare positional stops flag.Parse).
premise-ledger:
[E] TDD 结构/顺序/Acceptance Gate[0]==go test ./...: 直接读 ftb-plan.md 各 Phase 确认
[E] 绝对/absence 断言用 ! grep / ! test -f: 直接读 plan DoD 确认
[C] Goal 1-6 全覆盖到 Phase A-D + Gate: 对照 proposal.Goals 与 plan.Phases 映射
[C] lock.go/status.go/wire.go/monitor_task.go 符号与结构属实: subagent 已 Read 实际文件确认
[C] 无生产代码引用 4 个 task 符号(删除安全): grep --include=*.go 排除 _test/worktree 确认
[C] --session-id 接受任意字符串、serve 无需改: 读 wire.go:160,196-199 确认
[H] Phase 顺序 A→B→C→D 非环、依赖正确: 判断
[H] 函数签名(注入 ProcAncestryReader)可单测、无隐藏 /proc 调用: 判断
GCL-self-report: E=6 C=11 H=3

claimed: 2026-07-01T00:55:15Z
<!-- SECTION:NOTES:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 go test ./...
- [ ] #2 go test -coverprofile=/tmp/cover.out ./... && go tool cover -func=/tmp/cover.out | grep 'internal/daemon/session' | awk '{if ($3+0>=80) print "OK "$1" "$3}' | head -1
- [ ] #3 grep -q 'listen-preflight' .claude/skills/voci-listen/SKILL.md
- [ ] #4 ! grep -q 'TaskOutput' .claude/skills/voci-listen/SKILL.md
- [ ] #5 ! test -f internal/daemon/session/monitor_task.go
<!-- DOD:END -->

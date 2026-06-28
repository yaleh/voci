---
id: TASK-4
title: Claude Code monitor：会话集成形态与输入模式
status: 'Epic: Evaluating'
assignee: []
created_date: '2026-06-27 13:57'
updated_date: '2026-06-28 12:08'
labels:
  - 'kind:epic'
dependencies:
  - TASK-3
  - TASK-5
priority: medium
ordinal: 1000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
voci 对 Claude Code 的具体集成实现（Claude Code monitor），提供可切换的集成形态与输入模式。语言：Go（与 TASK-1 一致）。

## 集成形态（--session）
### separate（语音上下文会话与工作会话分离）
- voci 维护专用 Claude Code 上下文会话（headless / MCP），仅做检索/推理/加工
- 最终文本输出到用户工作会话，使用工作会话自身上下文
- 优点：工作会话不被检索污染；缺点：需多会话协调（tmux target / session id）

### integrated（语音上下文会话与工作会话集成）
- voci 以本地 MCP server 挂载到单一工作会话
- 语音 → mcp__voci__transcribe → 直接作为下一条消息
- 优点：架构简单；缺点：上下文能力受限于工作会话当前 context

## 输入模式（--input，运行时可切换）
- preview（默认）：发送前可预览/编辑，手动确认
- direct：处理完成后直接注入，无预览

## 注入通道
- tmux send-keys（分离形态）/ MCP tool 返回值（集成形态）/ clipboard 兜底

## 保持 Epic 理由
2 集成形态 × 2 输入模式 × 3 注入通道，且 separate 形态的多会话协调存在开放设计问题，需先经原型验证再固化。

## 会话上下文获取（SessionSource）

**不依赖 meta-cc**，直接读取 Claude Code 写入本地文件系统的 JSONL transcript。

### 定位机制

Claude Code 启动时将当前会话 UUID 注入环境变量：

```
CLAUDE_CODE_SESSION_ID=17cb4a79-e9c9-4317-a96a-36b7c7c854b7
```

`cmd/voci` 作为 Claude Code 子进程运行时，该变量已在环境中。结合项目路径即可精确定位 JSONL：

```
项目目录 = ~/.claude/projects/ + strings.ReplaceAll(root, "/", "-")
会话文件 = <项目目录>/<CLAUDE_CODE_SESSION_ID>.jsonl
```

无需扫描目录、无需按 mtime 猜测——直接 O(1) 命中当前会话。

### SessionSource 实现

```go
// internal/context/session_source.go
type SessionSource struct{ Lines int } // 默认 100 行

func (s *SessionSource) Name() string { return "session" }

func (s *SessionSource) Fetch(root string) (string, string) {
    id := os.Getenv("CLAUDE_CODE_SESSION_ID")
    if id == "" { return "", "session" } // 非 Claude Code 环境静默降级

    home, _ := os.UserHomeDir()
    hash := strings.ReplaceAll(root, "/", "-")
    path := filepath.Join(home, ".claude", "projects", hash, id+".jsonl")

    lines := tailLines(path, s.Lines)
    return parseSessionSnippet(lines), "session"
}
```

`parseSessionSnippet` 从最近 N 行 JSONL 提取：
- `tool_use name=Read/Edit` → 最近访问的文件路径
- `tool_use name=Bash` → 最近执行的命令
- `message.content` text → 提到的 task ID / 函数名（正则）

### 注入 asr_hint 后的效果

```
## Known Entities
- task twelve: TASK-12
...

## Claude Code Session
- editing: internal/gate/gate.go, internal/intent/classify.go
- ran: go test ./internal/gate/...
- mentioned: TASK-12, GateResult, Run
```

ASR 纠错时 LLM 获得实时工作上下文，对当前正在编辑的文件/函数的口语识别更准。

### 与现有架构的关系

- `Source` 接口已由 TASK-2 定义，SessionSource 直接实现，**零改动** `BuildContext` 签名
- 注册到 `defaultBuilder()` 即生效：`b.Register(&SessionSource{Lines: 100})`
- 非 Claude Code 环境（`CLAUDE_CODE_SESSION_ID` 为空）静默返回空串，降级无副作用

## 依赖
- TASK-3（ActionProposal gate）、TASK-5（adapter 抽象，本 Epic 是其 Claude Code 首个具体实现）
<!-- SECTION:DESCRIPTION:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
# Epic Plan: Claude Code monitor：会话集成形态与输入模式

## Background

voci 目前已有完整的 ASR → hinted → rewrite → classify → gate → execute 管道（TASK-14），
以及基于 `Source` 接口的可扩展上下文构建层（TASK-2）。
但现有 CLI（`cmd/voci/main.go`）只处理音频文件路径，不感知它运行在哪个 AI 编程工具会话中，
也没有把最终 proposal 送达目标工具的机制。

本 Epic 实现 voci 对 Claude Code 的具体集成——"Claude Code monitor"：
通过读取 Claude Code 写入本地文件系统的 JSONL transcript 获取实时会话信号（SessionSource），
通过 tmux send-keys（`separate` 形态）或本地 MCP server（`integrated` 形态）把 proposal 送达工具，
并在 CLI 上暴露 `--session` / `--input` 标志支持运行时切换。
这也是 TASK-5（工具 Adapter 抽象）接口的首个参考实现，在 TASK-5 接口稳定后对齐。

## Goals

1. **SessionSource**：实现 `internal/context/session_source.go`，通过 `CLAUDE_CODE_SESSION_ID`
   环境变量定位当前会话 JSONL，解析最近访问文件/执行命令/提到的实体，注入 `asr_hint`；
   非 Claude Code 环境静默返回空串（零副作用降级）。
2. **separate 形态**：`--session=separate`，voci 通过 `tmux send-keys` 把确认后的 proposal
   注入用户工作会话；支持 `--input=preview`（默认，human gate 已有）和 `--input=direct`（直接注入）。
3. **integrated 形态**：`--session=integrated`，voci 作为本地 MCP server 挂载到 Claude Code
   工作会话，暴露 `mcp__voci__transcribe` 工具，返回值作为下一条消息直接送入该会话。
4. **Adapter 接口对齐**：当 TASK-5 接口定稳后，将 separate/integrated 注入逻辑包装为
   `ClaudeCodeAdapter`，实现 TASK-5 的 `Adapter` 接口（`DiscoverContext` / `Deliver` / `Capabilities`）。

## Sub-Task Decomposition

1. **TASK-4-A: SessionSource — 会话上下文 Source 实现**
   实现 `internal/context/session_source.go`：通过 `CLAUDE_CODE_SESSION_ID` O(1) 定位 JSONL，
   `tailLines` + `parseSessionSnippet` 提取最近文件/命令/实体，注册到 `defaultBuilder()`，
   非 Claude Code 环境静默降级；含单元测试（fixture JSONL）。

2. **TASK-4-B: separate 形态 + CLI 标志（tmux 注入通道）**
   在 `cmd/voci/main.go` 添加 `--session`（separate/integrated，默认 separate）
   和 `--input`（preview/direct）标志；实现 `internal/inject/tmux.go`——
   `TmuxInjector` 调用 `tmux send-keys -t <target>` 把 proposal 送达工作会话，
   direct 模式跳过 gate 直接注入（仅 KindDirectPrompt/KindQuery），
   preview 模式复用现有 gate 流程；
   含 `--tmux-target` 标志（默认当前 pane）；失败时 clipboard 兜底。

3. **TASK-4-C: integrated 形态（本地 MCP server）**
   实现 `internal/mcp/server.go`：监听 Unix socket / 本地 HTTP（127.0.0.1），
   暴露 `mcp__voci__transcribe(audio_path)` 工具；
   接收调用后触发完整 ASR→rewrite→classify→gate→返回结果 pipeline，
   `--session=integrated` 时 `cmd/voci` 以 MCP server 模式启动；
   含集成测试（mock MCP client 调用验证返回值格式）。

4. **TASK-4-D: ClaudeCodeAdapter — TASK-5 接口对齐**
   待 TASK-5 `Adapter` 接口定稿后，将 TASK-4-B/C 的注入逻辑与 SessionSource
   封装为 `internal/adapter/claude_code.go` 中的 `ClaudeCodeAdapter`，
   实现 `DiscoverContext() Source`、`Deliver(ActionProposal) error`、`Capabilities() []Channel`。

## Sequencing

```
TASK-4-A (SessionSource)
    │
    ├──► TASK-4-B (separate + tmux)    ─┐
    │                                   ├──► TASK-4-D (Adapter 接口对齐)
    └──► TASK-4-C (integrated + MCP)   ─┘
```

- **TASK-4-A 优先**：SessionSource 不依赖注入通道，可立即实施；其产出对 TASK-4-B/C 的测试质量有正向收益。
- **TASK-4-B 与 TASK-4-C 可并行**：两者均在 TASK-4-A 完成后启动，互不依赖。
- **TASK-4-D 阻塞于 TASK-5**：必须等 TASK-5 的 `Adapter` 接口合并后才能实施；若 TASK-5 延迟，TASK-4-D 可推后，TASK-4-B/C 功能不受影响。
- **与 TASK-5 的关系**：TASK-4-B/C 是 TASK-5 接口的参考实现，建议 TASK-5 在 TASK-4-B 或 TASK-4-C 任一完成后提取接口。

## Constraints

- **语言**：全部 Go，与 TASK-1/TASK-14 一致；MCP server 使用标准库 `net/http` 或 Unix socket，保持依赖极简。
- **Human gate 安全边界**（来自 TASK-3）：`KindBacklogAction` 类 proposal 在 `--input=direct` 模式下仍须经过 human gate；只有 `KindDirectPrompt` / `KindQuery` 可在 direct 模式下跳过 gate 直接注入；`KindAmbiguous` 任何情况下不可绕过 gate。
- **非 Claude Code 环境静默降级**：`CLAUDE_CODE_SESSION_ID` 为空时 SessionSource 返回 `("", "session")`，不报错；`--session=separate` 在无 tmux 时 clipboard 兜底，不崩溃。
- **MCP server 本地性**：integrated 形态只监听 localhost（`127.0.0.1` 或 Unix socket），不对外暴露端口。
- **TASK-5 接口兼容**：TASK-4-B/C 须预留 `Deliver` / `DiscoverContext` 的自然边界，不允许将注入逻辑散布到 `cmd/voci/main.go`。

## 架构演进备注 (2026-06-28，影响 Evaluating 结论)

本 epic 当初的集成结论（integrated=本地 MCP server；separate=tmux 注入）在后续被一个更贴合「浏览器异步语音」的形态取代：
- **主路径改为 Monitor-push**：voci 把识别结果经 Claude Code 的 Monitor 原语 push 进会话（见 TASK-16 `voci serve` / TASK-17 /voci-listen）。Monitor command 内含识别服务，无独立 daemon。
- **integrated/MCP（TASK-4.3）** 降级为「会话内显式识别语音文件」的验证入口（见 TASK-15）。
- **separate/tmux（TASK-4.2）** 被 Monitor-push 取代（Monitor 跨 /clear 存活、无需多 pane 协调），保留为可选兜底。

这两个 Done 子任务的代码无需回滚（仍可用），但「主集成形态」的定论已转移到 TASK-15/16/17/18。建议 Evaluating 结论标注此演进后再 FINISH。
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
2026-06-28: 补充 SessionSource 设计——通过 CLAUDE_CODE_SESSION_ID 环境变量精确定位当前会话 JSONL，不依赖 meta-cc。实现为 internal/context/session_source.go，Source 接口兼容，非 Claude Code 环境静默降级。

Epic proposal approved (existing description). Starting epic plan draft.

Epic plan review iteration 2: APPROVED
premise-ledger:
[E] Sub-Task Decomposition present: 计划正文列出 4 个子任务，每个含 title 与描述行，直接可读
[E] Goal coverage: Goals 1-4 与 TASK-4-A/B/C/D 一一对应，直接从计划文本映射
[E] Sequencing coherence: Sequencing 小节明确写出依赖链及外部阻塞条件，直接可读
[E] Scope discipline: TASK-4-D 描述已无 codex/gemini_cli 字样，职责文本直接可核实
[E] No premature creation: 计划为纯文本，无 task_create 调用，直接可确认
[H] File paths / feasibility: Go 项目 internal/* 布局惯例、tmux 可行性依赖背景知识判断
GCL-self-report: E=5 C=0 H=1

cap:propose=approved

cap:decompose=started

cap:decompose=done
Children created: TASK-4.1, TASK-4.2, TASK-4.3, TASK-4.4
All have shell-gate DoD: verified
TASK-4.1 (Basic: Ready) — 8 DoD items
TASK-4.2 (Basic: Ready) — 10 DoD items
TASK-4.3 (Basic: Backlog) — 9 DoD items
TASK-4.4 (Basic: Proposal, blocked on TASK-5) — 8 DoD items

cap:evaluate=recommendation:FINISH | done=4 needsHuman=0 | all children Basic: Done with DoD pass | data_source: measured

RECOMMENDATION: FINISH.
Children: TASK-4.1 (SessionSource) ✓, TASK-4.2 (separate/tmux) ✓, TASK-4.3 (integrated/MCP) ✓, TASK-4.4 (ClaudeCodeAdapter) ✓
go test ./... PASS, go build ./cmd/voci PASS, go vet ./... PASS

To finish: set status → Epic: Done.
To iterate: set status → Epic: Proposal or Epic: Plan and re-run /epic-to-backlog.
<!-- SECTION:NOTES:END -->

---
id: TASK-19
title: Android 语音前端：OpenWakeWord 唤醒 + voci pipeline + Claude Code 终端串流
status: 'Epic: Backlog'
assignee: []
created_date: '2026-06-28 10:29'
updated_date: '2026-06-28 12:07'
labels:
  - 'kind:epic'
dependencies: []
ordinal: 17000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Android 语音前端：OpenWakeWord 唤醒 + voci pipeline + Claude Code 终端串流
<!-- SECTION:DESCRIPTION:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
# Epic Proposal: Android 语音前端：可唤醒 + 语音输入（MVP，跑在现有 web 接口上）

## 开发原则（本 epic 的核心约束）

**尽快在现有 web 接口（`POST /api/voice/transcribe`）上跑通一个「可唤醒 + 能说话进会话」的 Android 应用。** 会话显示、鉴权加固、session-id 修复等其它能力一律降级为后续任务，不在本 epic 范围。MVP 成功标准：对手机说唤醒词 → 说一句话 → 这句话经 voci pipeline 进入 Claude Code 会话。

## Background

voci 的输入侧服务器管线**已经完成**：`voci --daemon` 暴露 `POST /api/voice/transcribe`（原始音频 → ASR → 纠正 → 改写 → 分类 → events.log），会话侧 arm Monitor（`tail -f events.log`）将结果注入 Claude Code 会话。因此 Android 端无需任何服务器新代码，只需：唤醒 → 录音 → POST 到该已有端点。已核实 daemon 绑定 `127.0.0.1:9474`（`cmd/voci/main.go:116`，无 host flag）——MVP 用 `cloudflared tunnel --url http://localhost:9474` 暴露现有 daemon 即可让手机访问，零 Go 代码改动。当前缺的只是一个轻量 Android 客户端（不 fork dicio，仅参考其 OpenWakeWord 集成方式）。

## Goals

1. **可唤醒**（手工验收）：Android ForegroundService 以 OpenWakeWord（TFLite）持续监听麦克风并触发唤醒回调；静默环境误激活 ≤ 1 次/小时、人工检出 ≥ 95%。
2. **能输入**：唤醒后 AudioRecord 录音，POST 到现有 `/api/voice/transcribe`（URL 可配置，指向 cloudflared 隧道）；端到端（开口 → 该句进入 Claude Code 会话）成功率 ≥ 95%、延迟 ≤ 8 秒。
3. **零服务器改动跑通**：MVP 仅依赖现有 daemon + cloudflared，不新增/修改 voci Go 代码即可完成一次完整语音输入闭环。

## Decomposition Sketch

- **TASK-19-C：Android 项目骨架 + ForegroundService** — 新建 Kotlin 项目（`android/`），ForegroundService + 麦克风/通知权限，后台常驻。
- **TASK-19-D：OpenWakeWord TFLite 唤醒词管线** — melspectrogram 前端 + speech-embedding TFLite + wakeword TFLite 三段链；含可行性 spike。
- **TASK-19-E：录音、麦克风交接与 POST 提交** — D↔E 麦克风所有权/交接（防丢首音节）、VAD 静音截止、PCM→WAV、OkHttp POST 到可配置的 voci 端点。

## Trade-offs and Risks

**不做的事（本 epic 明确推迟到后续任务）：**
- 会话显示（SSE 端点 + Android 渲染 UI）。
- session-id 追踪修复（`~/.voci/session` 无人写入；影响 hint 质量与未来显示，但不阻塞语音输入）。
- 鉴权加固（secret header / tunnel ACL）；MVP 用短期隧道 URL，不做鉴权。
- daemon LAN 绑定 flag（MVP 用隧道绕过，不改 Go 代码）。
- 不 fork dicio；不集成设备端 ASR；不做语音输入的人类 gate（voice-trusted，沿用现有 daemon 行为）。

**已知风险：**
- **OpenWakeWord 工作量（最大风险，在关键路径上）**：非"加载单个 tflite"——三段推理链，参考实现为 Python，Android 需自行实现帧处理与链式推理；TASK-19-D 含 spike，spike 失败则评估 Porcupine（授权成本）/Vosk-keyword 替代。**建议 D 的 spike 与 C 并行尽早启动。**
- **麦克风单一资源**：D 持续检测与 E 录音共享同一 `AudioRecord`，交接处易丢首 200–400ms；须在 E 明确设计。
- **隧道延迟**：cloudflared 弱网下延迟偏高，8 秒 SLA 已留余量。
- **TFLite 模型大小**：打包进 APK，预计 < 10 MB。

---

# Epic Plan: Android 语音前端：可唤醒 + 语音输入（MVP）

## Background

同上。已核实事实：(1) `internal/daemon/server.go` 的 `POST /api/voice/transcribe` 接收原始音频 body 跑完整 pipeline 并写 events.log（`server.go:135` 无条件，无 gate——MVP 沿用此 voice-trusted 行为）；(2) daemon 绑定 `127.0.0.1:9474`（`main.go:116`），MVP 经 cloudflared 隧道暴露，无需改码。

## Goals

（同 Proposal Goals）

## Sub-Task Decomposition

1. **TASK-19-C: Android 项目骨架 + ForegroundService** — Kotlin 项目（`android/`，minSdk 26），Gradle + 清单权限（`FOREGROUND_SERVICE` / `FOREGROUND_SERVICE_MICROPHONE`、`RECORD_AUDIO`、`POST_NOTIFICATIONS`），最小 `VociService`，端点 URL 经 `BuildConfig`/设置项可配置；冒烟测试通知常驻。
2. **TASK-19-D: OpenWakeWord TFLite 唤醒词管线** — 先 spike 验证三段链在 Android 的可行性（melspectrogram + speech-embedding TFLite + wakeword TFLite，正确 framing/ringbuffer，用现成模型）；spike 通过后实装连续推理与唤醒回调；spike 失败评估替代方案并回报。验收：静默误激活 ≤ 1/小时、人工检出 ≥ 95%（手工）。
3. **TASK-19-E: 录音、麦克风交接与 POST 提交** — 设计 D↔E 麦克风所有权（单一 `AudioRecord` + ring buffer 衔接，唤醒尾音并入指令起始）；VAD 静音截止；PCM→WAV；OkHttp POST 到配置 URL `/api/voice/transcribe`；解析响应在 UI 提示已提交（提案摘要可选）。验收：端到端成功率 ≥ 95%、≤ 8 秒。

## Sequencing

```
TASK-19-C (Android 骨架)
    ├──> TASK-19-D (唤醒词管线; spike 尽早与 C 并行)
    │       └──> TASK-19-E (麦克风交接 + 录音 + POST)
    └──────────> TASK-19-E (E 也需骨架就位)
```

可并行：C 与 D 的 spike 同时启动（spike 不强依赖完整骨架）；E 待 C+D 收敛。
关键路径：C → D → E。
服务器侧 MVP 无开发任务——运维步骤：`voci --daemon` + `cloudflared tunnel --url http://localhost:9474`，将隧道 URL 填入 Android 配置。

## Constraints

- Android 子任务提供可运行的 Instrumented Test 或明确手工验收步骤；Goals 1、2 为手工/集成验收。
- MVP 不改动 voci Go 代码；如发现必须的服务器改动，升级为独立子任务并记录原因。
- 所有 Android 代码置于 `android/`，不污染现有 Go module。
- 端点 URL 与（未来的）secret 经 `BuildConfig`/设置项注入，不硬编码进版本控制。
- 隧道 URL 为短期测试用；正式鉴权属后续任务。

## 后续任务（out of scope，记录以免遗失）

- **会话显示链**：session-id 追踪修复（`~/.voci/session` 无写入方）→ `GET /api/session/stream` SSE 端点（复用 `SessionSource` 路径逻辑 + Last-Event-ID 游标）→ Android 结构化渲染 UI。采用 JSONL 结构化流（turn-granular，非 token 级实时）。
- **鉴权加固**：daemon secret header 中间件 + Android 注入 + tunnel ACL。
- **可选**：daemon `--daemon-host` 绑定 flag（替代隧道做 LAN 直连）。

## RE-ARCHITECTED 依赖更新 (2026-06-28)

**上游已转向 Monitor-host，本 epic 中关于输入链的描述需按此更正（原文保留作记录）：**
- ❌ 旧：`voci --daemon` 暴露 POST /api/voice/transcribe → 写 events.log → 会话 `tail -f events.log`。
- ✅ 新：识别服务是 `voci serve`（TASK-16 改造后），由 /voci-listen 作为 Monitor command 在会话内拉起；它既收 HTTP 上传又把识别结果打到 stdout 经 Monitor 注入会话。**不再有独立 daemon、不再有 events.log tail。**

**对本 epic 的影响（MVP 仍成立，接口名不变）**：
1. Android 仍 POST 到 `/api/voice/transcribe`，但目标进程是 `voci serve`（非 `voci --daemon`）。cloudflared 隗道指向 `voci serve` 的监听端口。
2. **依赖收紧**：MVP「零服务器改动」的前提依赖 TASK-16 的 `voci serve` 落地（当前 TASK-16 在 Basic: Backlog 回炉）。在 TASK-16 完成前，Android 端可先对旧 `voci --daemon` 联调，但正式形态以 serve 为准。
3. **session-id 修复不再是后续阻塞**：`voci serve` 作为会话子进程继承 CLAUDE_CODE_SESSION_ID，hint 定位在主路径自动成立——原「后续任务: session-id 追踪修复（~/.voci/session 无写入方）」对本地 serve 已消解。注：Android 只发送音频，构建 hint 的是本地 serve（有会话 env），所以远程场景也不需 ~/.voci/session；仅当未来支持「serve 与会话分离的独立 daemon」时才需文件兜底（TASK-18 收窄段）。

建议：本 epic 保持 Epic: Backlog，待 TASK-16 `voci serve` 落地后再 decompose，以免子任务对着将被移除的 `--daemon`/events.log 接口开发。
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Epic proposal self-review: APPROVED
premise-ledger:
[E] Background explains WHY (not WHAT), 8 lines: voci 现有交互方式要求用户坐在电脑前；Android 低功耗唤醒词方案已成熟；移动闭环降低上下文切换成本
[E] Goals numbered and verifiable: 5 goals with measurable SLAs (≥95% detection, ≤5s latency, ≤500ms SSE delay)
[E] Decomposition ≥2 children covering all goals: 7 children (A-G), each with one-line scope; coverage verified across all 5 goals
[C] Feasibility aligns with codebase: POST /api/voice/transcribe confirmed in server.go:49; AppendEvent confirmed in eventlog.go; --daemon flag confirmed in main.go:90-116; GET /api/terminal/stream correctly identified as missing
[C] Trade-offs and risks identified: explicitly lists exclusions (no dicio fork, no ttyd, no on-device ASR) and 5 concrete risks
[H] Consistency: SSE endpoint described as missing in Background and Goals, child task A adds it; no contradictions with context
GCL-self-report: E=3 C=2 H=1

Epic proposal approved. Starting epic plan draft.

Epic plan review iteration 1: APPROVED
premise-ledger:
[E] Sub-Task Decomposition present: 7 children (A–G) each with title and one-line description
[E] Goal coverage: all 5 Goals covered (G1→D, G2→E, G3→F, G4→A+B, G5→G)
[E] Sequencing coherence: dependency graph stated, acyclic, parallelism noted, critical path identified
[C] Scope discipline: all children map to proposal Goals; no scope creep; not-done list explicit
[C] No premature creation: plan describes intended children only, no task records created
[E] File paths / feasibility: internal/daemon/server.go exists with Handler() method; android/ dir new but appropriate
GCL-self-report: E=4 C=2 H=0

cap:propose=approved

移除 TASK-19-B（tmux pipe-pane 脚本），合并进 TASK-19-A；SSE 端点改为复用 SessionSource 路径逻辑直接 tail Claude Code JSONL 文件，不依赖 tmux。

严苛架构师复审 (Opus): 核实出两处假设为假 —— (1) ~/.voci/session 无人写入，SessionSource 实际仅靠 env/降级，新增 TASK-19-H 前置修复；(2) daemon 路径 server.go:135 无 gate，无条件 AppendEvent。用户决策: 会话展示用 JSONL 结构化流(接受 turn-granular 非实时)、语音输入免 gate(voice-trusted)——两者已写入'关键决策'显式记录。另修正: Goal 3 措辞改为 turn-granular; TASK-19-D 重述为 OpenWakeWord 三段推理链+spike(原'加载单个 tflite'低估); TASK-19-E 增加 D↔E 麦克风所有权/交接设计; TASK-19-G 鉴权契约先行(E/F 依赖); TASK-19-A 增加 SSE 重连游标; Goals 1&3 标注为手工验收。子任务 6→7 (新增 H)。

重定位为 MVP-first (用户原则): 尽快在现有 web 接口上跑通'可唤醒+语音输入'的 Android 应用。核实: POST /api/voice/transcribe + events.log + Monitor-push 输入链已完成；daemon 绑 127.0.0.1:9474(main.go:116)——MVP 用 cloudflared 隔离零服务器改动。子任务由 7 缩减为 3 (C/D/E)。降级为后续: 会话显示链(H session-id修复 + A SSE 端点 + F 渲染 UI)、鉴权加固(G)、daemon LAN 绑定 flag。保留架构师复审核实的事实(无 gate/无 session 写入/绑定地址)于'后续任务'段以免遗失。
<!-- SECTION:NOTES:END -->

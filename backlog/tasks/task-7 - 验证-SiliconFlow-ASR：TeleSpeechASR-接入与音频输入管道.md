---
id: TASK-7
title: 验证 SiliconFlow 语音服务可用性
status: 'Basic: Done'
assignee: []
created_date: '2026-06-27 15:03'
updated_date: '2026-06-27 15:11'
labels:
  - 'kind:basic'
dependencies: []
priority: high
ordinal: 7000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
验证 SiliconFlow 语音服务（ASR + TTS）可从本机调通，API key 从 ~/.config/voci/config.yaml 读取。

## 目标
用最小脚本确认：
1. TTS（POST /audio/speech）：文本→音频，收到二进制 wav/mp3 响应
2. ASR（POST /audio/transcriptions）：将 TTS 生成的音频送回转写，收到 text 响应
3. 串联验证：TTS 输出 → ASR 输入，形成闭环

## API Key
读取自 ~/.config/voci/config.yaml（字段 siliconflow_api_key）或环境变量 SILICONFLOW_API_KEY

## TTS API
- POST https://api.siliconflow.cn/v1/audio/speech
- Body: {model: FunAudioLLM/CosyVoice2-0.5B, input: <text>, response_format: wav}
- Response: 二进制音频数据

## ASR API
- POST https://api.siliconflow.cn/v1/audio/transcriptions
- Body: multipart/form-data，file=<audio bytes>，model=TeleAI/TeleSpeechASR
- Response: {text: <transcript>}

## 不做
- 与 voci 主管道集成
- 错误重试 / 限流处理
- 除验证以外的任何逻辑
<!-- SECTION:DESCRIPTION:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
# Plan: 验证 SiliconFlow 语音服务可用性

单文件 Go 脚本，无框架依赖，独立运行。

## Phase A: 验证脚本

### Tests (write first)

无单元测试（纯集成验证脚本，依赖真实 API）。
手动验证步骤见 DoD。

### Implementation

- `scripts/check-siliconflow/main.go`：
  1. 读取 API key：先查 `SILICONFLOW_API_KEY` env，再读 `~/.config/voci/config.yaml`；两者均空则打印提示并 exit 1
  2. TTS 调用：POST `https://api.siliconflow.cn/v1/audio/speech`
     - Body JSON：`{"model":"FunAudioLLM/CosyVoice2-0.5B","input":"TASK-1 voci 上下文感知语音改写验证","response_format":"wav"}`
     - 响应：读取二进制写入 `/tmp/voci-tts-check.wav`
     - 打印：`TTS OK: <bytes> bytes → /tmp/voci-tts-check.wav`
  3. ASR 调用：POST `https://api.siliconflow.cn/v1/audio/transcriptions`
     - multipart/form-data：`file=/tmp/voci-tts-check.wav`，`model=TeleAI/TeleSpeechASR`
     - 响应：解析 `{"text":"..."}`
     - 打印：`ASR OK: <transcript>`
  4. 验证闭环：assert transcript 非空；打印 `✓ SiliconFlow 服务可用`

- `scripts/check-siliconflow/` 内无其他文件，`go run ./scripts/check-siliconflow` 独立执行

### DoD
- [ ] `go run ./scripts/check-siliconflow 2>&1 | grep -q "TTS OK"`
- [ ] `go run ./scripts/check-siliconflow 2>&1 | grep -q "ASR OK"`
- [ ] `go run ./scripts/check-siliconflow 2>&1 | grep -q "✓ SiliconFlow"`

## Constraints
- 单文件脚本，`go run` 直接执行，不产出任何 binary
- 仅依赖 Go 标准库（net/http、encoding/json、mime/multipart、os、gopkg.in/yaml.v3 读 config）
- API key 不打印到 stdout；仅在 key 缺失时打印引导信息：`set SILICONFLOW_API_KEY or update ~/.config/voci/config.yaml`
- 生成的 /tmp/voci-tts-check.wav 不提交

## Acceptance Gate
- [ ] `go run ./scripts/check-siliconflow 2>&1 | grep -q "✓ SiliconFlow"`
<!-- SECTION:PLAN:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 go run ./scripts/check-siliconflow 2>&1 | grep -q "TTS OK"
- [ ] #2 go run ./scripts/check-siliconflow 2>&1 | grep -q "ASR OK"
- [ ] #3 go run ./scripts/check-siliconflow 2>&1 | grep -q "✓ SiliconFlow"
<!-- DOD:END -->

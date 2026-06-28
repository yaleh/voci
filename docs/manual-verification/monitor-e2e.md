# Monitor 端到端人工验证清单

语音→Monitor→会话执行链路的最后一段（Claude Code 收到 Monitor push 并实际执行指令）无法自动化测试。本文档将其固化为可重复的人工验证流程。

自动化覆盖范围（不需人工）：
- 生产者侧：`go test -tags e2e ./internal/daemon/...`（serve→/transcribe→/emit→stdout JSON 行）
- 解析侧：`bash scripts/extract-instruction_test.sh`（`extractInstruction` JSON→Rewritten 提取）

## 人工验证

前提条件：`voci serve` 可运行（ASR/Ollama 配置完成）；Claude Code 会话已打开。

### 步骤

**1. 确认环境变量已设置**

```bash
echo $CLAUDE_CODE_SESSION_ID
# 预期观察：输出非空会话 ID（如 abc123...）
```

**2. 启动 /voci-listen**

在 Claude Code 会话中执行：
```
/voci-listen
```

预期观察：会话输出类似 `[voci-listen] stopStaleMon: ...`，随后 Monitor 被 arm（`command="voci serve"`），`voci serve` 进程启动并监听 9474 端口。

**3. 通过 /api/voice/emit 注入指令**

打开另一个终端，POST 一条确认文本：

```bash
curl -s -X POST http://127.0.0.1:9474/api/voice/emit \
  -H 'Content-Type: application/json' \
  -d '{"text":"echo hello from voci-listen"}'
```

预期观察：
- curl 返回 HTTP 204（无响应体）。
- `voci serve` 进程在 stdout 写出一行 JSON，例如：
  `{"timestamp":"","rewritten":"echo hello from voci-listen","kind":"direct_prompt",...}`
- Monitor 捕获该行并唤醒 Claude Code 会话。

**4. 确认 Claude Code 会话收到 Monitor push 并执行**

在 Claude Code 会话中观察：

```
[voci-listen] instruction: echo hello from voci-listen
```

预期观察：Claude Code 将 `echo hello from voci-listen` 作为下一条用户指令执行，并在会话中输出 `hello from voci-listen`（或等价行为）。

**5. 验证链路完整性**

检查清单（全部满足则通过）：

- [ ] `curl` 返回 HTTP 204
- [ ] `voci serve` stdout 有格式正确的 JSON 行（`rewritten` 字段非空）
- [ ] Claude Code 会话有 `[voci-listen] instruction: ...` 输出
- [ ] 指令在会话中被实际执行（可观测的输出或动作）

### 已知限制

- 若 `/voci-listen` 在 `/clear` 或上下文压缩后未自动恢复，需重新运行 `/voci-listen`（skill 的 Monitor description 中有恢复提示）。
- `CLAUDE_CODE_SESSION_ID` 必须在 `voci serve` 启动前设置（它作为 Monitor 子进程继承该变量）。
- `voci-listen` 停止时写入 `~/.voci/.listen-stop` 哨兵文件；删除该文件后再次运行 `/voci-listen` 恢复。

---
id: TASK-47
title: 火山引擎 ASR 策略 B 实验：动态热词管理 API 验证 WFST 解码层偏置对 entity_recall 的影响
status: 'Basic: Backlog'
assignee: []
created_date: '2026-06-29 15:27'
updated_date: '2026-06-29 15:35'
labels:
  - 'kind:basic'
  - experiment
  - asr
dependencies: []
ordinal: 32000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
验证火山引擎豆包语音识别服务的 WFST 解码层热词偏置（策略 B：动态热词管理 API）是否能显著提升 entity_recall，打破 SiliconFlow/Whisper 托管服务中 hint 注入无效（±0）的僵局。每个测试 case 在 hint_mode=on 时通过热词管理 API 动态创建专属词表，传入 vocabulary_id，测完异步删除；hint_mode=off 时不传 vocabulary_id，作为基线对照。
<!-- SECTION:DESCRIPTION:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
# Plan: 火山引擎 ASR 策略 B 实验：动态热词管理 API 验证 WFST 解码层偏置对 entity_recall 的影响

## Context

火山引擎（Volcano Engine）ASR 的一句话识别接口在 WFST 解码层支持词表偏置（vocabulary_id），这与 SiliconFlow/TeleSpeech 的 prompt 注入机制有本质区别：后者在推理阶段静默丢弃 prompt，无法真正提升实体召回率。本实验实现策略 B——在每次识别前动态创建热词表、识别后异步删除——以量化 hint_mode=on 与 hint_mode=off 之间 entity_recall 的差值，验证 WFST 解码层偏置是否构成实质性突破。实验数据来自已有的 35 条测试用例（16 个独特实体），可与既有 telespeech/gemma4 结果直接横向对比。

---

## Phase 1: 实现 VolcengineASRAdapter（策略 B）

**目标文件**: `docs/research/asr-bench/adapters/volcengine.py`

### 类结构

```python
import base64, json, os, time, threading, uuid, requests
from .base import ModelAdapter, TranscribeOpts

RECOGNIZE_URL = "https://openspeech.bytedance.com/api/v1/asr"
VOCAB_CREATE_URL = "https://openspeech.bytedance.com/api/v1/hotword"
VOCAB_DELETE_URL = "https://openspeech.bytedance.com/api/v1/hotword/delete"

class VolcengineASRAdapter(ModelAdapter):
    supports_hints = True

    def __init__(self):
        # Use os.environ[] — raises KeyError immediately if var absent (fail fast)
        self.app_id = os.environ["VOLCENGINE_APP_ID"]
        self.access_token = os.environ["VOLCENGINE_ACCESS_TOKEN"]

    @property
    def name(self) -> str:
        return "volcengine"

    def transcribe(self, wav_path: str, opts: TranscribeOpts) -> tuple[str, float]:
        ...

    def _create_vocab(self, entities: list[str]) -> str | None:
        ...

    def _delete_vocab_async(self, vocab_id: str) -> None:
        ...

    def _delete_vocab(self, vocab_id: str) -> None:
        ...
```

### `_create_vocab(entities: list[str]) -> str | None`

Signature must match exactly. Steps:

1. Build request body:
   ```json
   {
     "appid": "<self.app_id>",
     "type": 1,
     "name": "bench-<unix_timestamp_int>",
     "content": [{"word": "<entity>", "weight": 5}, ...]
   }
   ```
   Map `entities` list → `content` array. weight=5 is mid-range and safe for all entity types.

2. POST to `VOCAB_CREATE_URL` with header `Authorization: Bearer <self.access_token>` and `Content-Type: application/json`. Timeout 15s.

3. On success: parse response JSON, extract `vocab_id` key (string like `"vocab-xxx-..."`). Return it.

4. **Failure handling — must not raise**:
   - Catch `requests.RequestException` and any JSON parse error
   - If response status != 200 or `vocab_id` absent in body, print a warning to stderr:
     `print(f"[volcengine] WARNING: vocab creation failed: {e or resp.text}", file=sys.stderr)`
   - Return `None`
   - Callers receiving `None` proceed without `vocabulary_id` (degraded to baseline), ensuring bench run is never aborted by a vocab API failure.

### `_delete_vocab(vocab_id: str) -> None`

1. POST to `VOCAB_DELETE_URL` with body `{"appid": self.app_id, "vocab_id": vocab_id}` and same auth header.
2. Swallow all exceptions silently (`except Exception: pass`) — deletion failure must never surface.

### `_delete_vocab_async(vocab_id: str) -> None`

```python
def _delete_vocab_async(self, vocab_id: str) -> None:
    t = threading.Thread(target=self._delete_vocab, args=(vocab_id,), daemon=True)
    t.start()
    # do NOT t.join() — fire-and-forget
```

`daemon=True` ensures the thread does not block process exit.

### `transcribe(wav_path: str, opts: TranscribeOpts) -> tuple[str, float]`

```
1. wav_bytes = open(wav_path, "rb").read()
2. audio_b64 = base64.b64encode(wav_bytes).decode()
3. vocab_id = None
4. if opts.known_entities:
       vocab_id = self._create_vocab(opts.known_entities)
       # vocab_id may be None (degraded) — that is acceptable
5. additions = {}
   if vocab_id:
       additions["vocabulary_id"] = vocab_id
6. body = {
       "app": {"appid": self.app_id, "token": self.access_token, "cluster": "volcano_asr_common"},
       "user": {"uid": "bench"},
       "audio": {"format": "wav", "rate": 16000, "channel": 1, "bits": 16, "data": audio_b64},
       "request": {"reqid": str(uuid.uuid4()), "nbest": 1,
                   "workflow": "audio_in,resample,remove_silence,asr,itn"},
       "additions": additions,
   }
7. t0 = time.time()
   resp = requests.post(RECOGNIZE_URL,
                        headers={"Authorization": f"Bearer {self.access_token}"},
                        json=body, timeout=60)
   latency = time.time() - t0
8. if vocab_id:
       self._delete_vocab_async(vocab_id)    # always fire after recognition, before parsing
9. resp.raise_for_status()
10. data = resp.json()
    # Response shape: {"result": {"utterances": [{"words": [...], "text": "..."}], "text": "..."}}
    # Try utterances[0].text first, fall back to top-level result.text, then empty string
    result = data.get("result", {})
    utterances = result.get("utterances", [])
    if utterances:
        text = utterances[0].get("text", "")
    else:
        text = result.get("text", "")
    return text.strip(), latency
```

If `resp.raise_for_status()` raises, the exception propagates to runner.py's except block which writes an error row. The `_delete_vocab_async` call (step 8) happens before the raise, so vocab cleanup always fires.

### Smoke-test CLI 模式

At the bottom of `volcengine.py`:

```python
if __name__ == "__main__":
    import sys, argparse
    parser = argparse.ArgumentParser()
    parser.add_argument("--smoke-test", action="store_true")
    args = parser.parse_args()

    if not args.smoke_test:
        parser.print_help()
        sys.exit(0)

    # Step 1: Check env vars
    missing = [v for v in ("VOLCENGINE_APP_ID", "VOLCENGINE_ACCESS_TOKEN") if not os.environ.get(v)]
    if missing:
        print(f"[FAIL] Missing env vars: {missing}", file=sys.stderr)
        sys.exit(1)

    adapter = VolcengineASRAdapter()

    # Step 2: Create a test vocabulary (no audio needed)
    test_entities = ["测试实体", "Volcano Engine", "WFST"]
    vocab_id = adapter._create_vocab(test_entities)
    if not vocab_id:
        print("[FAIL] vocab creation returned None", file=sys.stderr)
        sys.exit(1)
    print(f"[OK] vocab created: {vocab_id}")

    # Step 3: Synchronous delete (confirms round-trip)
    adapter._delete_vocab(vocab_id)
    print("[OK] vocab deleted")

    print("[OK] smoke test passed")
    sys.exit(0)
```

The smoke test is entirely standalone: it validates only the hot-word management API (create + delete round-trip) and requires no audio files. It does **not** call `transcribe()`.

### DoD
- [ ] `grep -q 'class VolcengineASRAdapter' docs/research/asr-bench/adapters/volcengine.py`
- [ ] `grep -q 'supports_hints = True' docs/research/asr-bench/adapters/volcengine.py`
- [ ] `grep -q '_create_vocab' docs/research/asr-bench/adapters/volcengine.py`
- [ ] `grep -q '__main__' docs/research/asr-bench/adapters/volcengine.py`

---

## Phase 2: 集成到 runner.py

**目标文件**: `docs/research/asr-bench/runner.py`

### 修改点 1 — import

在现有两行 adapter import 之后添加：
```python
from adapters.volcengine import VolcengineASRAdapter
```

### 修改点 2 — choices

将 `parser.add_argument("--models", ...)` 的 choices 列表扩展：
```python
choices=["all", "telespeech", "gemma4", "volcengine"]
```

### 修改点 3 — adapter 实例化

在 `if args.models in ("all", "gemma4"):` 块之后添加：
```python
if args.models in ("all", "volcengine"):
    try:
        adapters.append(VolcengineASRAdapter())
    except (KeyError, RuntimeError) as e:
        print(f"WARNING: volcengine unavailable: {e}", file=sys.stderr)
```

`KeyError` covers the `os.environ["..."]` missing-key path; `RuntimeError` is kept for consistency with the telespeech pattern.

### 修改点 4 — 输出文件名含模型标识（推荐）

Change:
```python
out_file = out_dir / f"run-{timestamp}.jsonl"
```
To:
```python
model_tag = args.models if args.models != "all" else "all"
out_file = out_dir / f"run-{timestamp}-{model_tag}.jsonl"
```

This makes the Phase 4 DoD glob `run-*volcengine*.jsonl` unambiguous.

### DoD
- [ ] `grep -q 'volcengine' docs/research/asr-bench/runner.py`
- [ ] `grep -q 'VolcengineASRAdapter' docs/research/asr-bench/runner.py`

---

## Phase 3: 开通凭证 & 热词预检

### 凭证获取

1. 登录火山引擎控制台 → 语音技术 → 一句话识别 → 创建应用，获得 `AppID`
2. 在控制台的 AccessKey 管理页面创建或复制 Access Token
3. 确认该应用已开通 **热词管理** 功能（部分账号需单独申请）
4. 设置环境变量（写入当前 shell，以及 `~/.bashrc` 以持久化）：
   ```bash
   export VOLCENGINE_APP_ID="<your-app-id>"
   export VOLCENGINE_ACCESS_TOKEN="<your-access-token>"
   ```
   如果项目根有 `.env` 文件（已在 `.gitignore` 中），也可写入其中并 `source .env`

### 运行 smoke test

```bash
cd /home/yale/work/voci
python3 docs/research/asr-bench/adapters/volcengine.py --smoke-test
```

Smoke test 验证以下内容（纯 API 往返，无需音频文件）：
- `VOLCENGINE_APP_ID` 和 `VOLCENGINE_ACCESS_TOKEN` 均已设置且非空
- 热词创建 API 返回 HTTP 200 且响应体包含 `vocab_id` 字段
- 热词删除 API 正常完成（同步确认）

预期输出（成功）：
```
[OK] vocab created: vocab-xxx-...
[OK] vocab deleted
[OK] smoke test passed
```

若失败，排查顺序：
1. HTTP 状态码（401 → token 无效；403 → 未开通热词功能；404 → URL 错误）
2. 响应 JSON 中的 `code`/`message` 字段
3. 确认 AppID 对应的应用已开通一句话识别服务

### DoD
- [ ] `test -n "$VOLCENGINE_APP_ID"`
- [ ] `test -n "$VOLCENGINE_ACCESS_TOKEN"`
- [ ] `python3 docs/research/asr-bench/adapters/volcengine.py --smoke-test`

---

## Phase 4: 运行 bench 实验

### 命令

```bash
cd /home/yale/work/voci
python3 docs/research/asr-bench/runner.py \
  --models volcengine \
  --cases testdata/testcases.json \
  --out docs/research/asr-bench/results/
```

- runner 对每条用例运行两轮：`hint_mode=off`（无 vocabulary_id）和 `hint_mode=on`（动态创建词表）
- 35 条用例 × 2 轮 = 最多 70 行 JSONL（若某条音频文件缺失则跳过）
- 热词创建/删除日志（`[volcengine] WARNING:` 前缀）出现在 stderr，不污染 JSONL
- 输出：`docs/research/asr-bench/results/run-<timestamp>-volcengine.jsonl`

### 进度监控

runner 每行打印：
```
  volcengine/off tc-001: 今天天气真好...
  volcengine/on  tc-001: 今天天气真好（实体名）...
```

目视对比两行，初步判断 `hint_mode=on` 是否正确识别了实体词。

### DoD
- [ ] `ls docs/research/asr-bench/results/run-*volcengine*.jsonl 2>/dev/null | head -1 | grep -q .`
- [ ] `grep -q '"model": "volcengine"' docs/research/asr-bench/results/run-*volcengine*.jsonl`

---

## Phase 5: 分析结果

### 分析脚本（内联，无需单独文件）

从项目根运行：

```bash
python3 - <<'EOF'
import json, glob, statistics, sys

rows = [
    json.loads(l)
    for f in glob.glob("docs/research/asr-bench/results/run-*volcengine*.jsonl")
    for l in open(f)
    if l.strip()
]

print(f"Total rows loaded: {len(rows)}")

for mode in ("off", "on"):
    subset = [r for r in rows if r.get("hint_mode") == mode
              and r.get("model") == "volcengine"
              and r.get("entity_recall") is not None]
    if not subset:
        print(f"hint_mode={mode}: NO DATA", file=sys.stderr)
        continue
    recalls = [r["entity_recall"] for r in subset]
    wers = [r["wer"] for r in subset if r.get("wer") is not None]
    lats = [r["latency_s"] for r in subset if r.get("latency_s") is not None]
    print(f"hint_mode={mode}: n={len(subset)}, "
          f"entity_recall mean={statistics.mean(recalls):.4f} "
          f"median={statistics.median(recalls):.4f}, "
          f"WER mean={statistics.mean(wers):.4f}, "
          f"latency mean={statistics.mean(lats):.2f}s")

# Delta
on_rows  = [r["entity_recall"] for r in rows if r.get("hint_mode") == "on"  and r.get("entity_recall") is not None]
off_rows = [r["entity_recall"] for r in rows if r.get("hint_mode") == "off" and r.get("entity_recall") is not None]
if on_rows and off_rows:
    delta = statistics.mean(on_rows) - statistics.mean(off_rows)
    print(f"\ndelta entity_recall (on - off) = {delta:+.4f}")
    if delta >= 0.10:
        verdict = "BREAKTHROUGH"
    elif delta >= 0.05:
        verdict = "MARGINAL"
    else:
        verdict = "NO_EFFECT"
    print(f"Verdict: {verdict}")

# Per-category breakdown
cats = sorted(set(c for r in rows for c in r.get("category", [])))
print("\nPer-category breakdown:")
for cat in cats:
    for mode in ("off", "on"):
        s = [r["entity_recall"] for r in rows
             if r.get("hint_mode") == mode and cat in r.get("category", [])
             and r.get("entity_recall") is not None]
        if s:
            print(f"  [{cat}] {mode}: mean={statistics.mean(s):.4f} n={len(s)}")
EOF
```

### 突破阈值判定

| delta (on - off) | 判定 | 说明 |
|---|---|---|
| >= 0.10 (10 pp) | BREAKTHROUGH | WFST 偏置产生实质提升，建议集成到生产 |
| 0.05 ~ 0.10 | MARGINAL | 有统计意义但效果弱，考虑调优 weight 参数 |
| < 0.05 | NO_EFFECT | 与 SiliconFlow 相当，评估策略 C 或放弃 |

### 分析文档撰写

将分析写入 `docs/research/asr-bench/results/volcengine-analysis.md`，必须包含以下段落：

1. **实验摘要**：运行日期、有效用例数量、模型名称 `volcengine`
2. **核心指标对比表**：hint_mode=off vs hint_mode=on 的 mean/median entity_recall、mean WER/CER、mean latency_s
3. **entity_recall delta 与判定**：对照阈值表，明确写出 `BREAKTHROUGH / MARGINAL / NO_EFFECT`
4. **热词 API 可靠性**：vocab 创建失败次数（从 stderr 日志统计），降级识别比例
5. **分类别分析**：按 category 列出 entity_recall delta，找出受益最大/最小的类别
6. **下一步建议**：
   - BREAKTHROUGH → 推进策略 B 集成到 voci 生产 ASR 路径
   - MARGINAL → 尝试提高 weight 值（如 weight=8）重跑，或评估策略 C
   - NO_EFFECT → 策略 B 无效，关闭火山引擎方向，转向策略 C 或其他提供商

文档中必须含字符串 `entity_recall` 和 `hint_mode`（DoD grep 校验）。

### DoD
- [ ] `grep -q 'entity_recall' docs/research/asr-bench/results/volcengine-analysis.md`
- [ ] `grep -q 'hint_mode' docs/research/asr-bench/results/volcengine-analysis.md`
- [ ] `grep -q 'BREAKTHROUGH\|MARGINAL\|NO_EFFECT' docs/research/asr-bench/results/volcengine-analysis.md`

---

## Constraints

- 不实现策略 C（WebSocket 流式识别），留给后续任务
- 不修改现有适配器 `telespeech.py`、`gemma4.py`
- 词表删除必须异步（daemon 线程 fire-and-forget），绝不在识别调用返回前 join 或等待
- `_create_vocab` 失败时必须降级（return None）而非 raise，保证 bench run 不被单个 API 失败中断
- 每张词表最多 1000 词；本实验 16 个实体远在限制以内，无需分批
- Smoke test 必须是独立模式，不依赖真实音频文件，仅测试热词 API 的 create/delete 往返

---

## Acceptance Gate

- [ ] `grep -q '"hint_mode": "on"' docs/research/asr-bench/results/run-*volcengine*.jsonl`
- [ ] `python3 -c "import json,glob; rows=[json.loads(l) for f in glob.glob('docs/research/asr-bench/results/run-*volcengine*.jsonl') for l in open(f)]; on=[r for r in rows if r.get('model')=='volcengine' and r.get('hint_mode')=='on' and 'entity_recall' in r]; off=[r for r in rows if r.get('model')=='volcengine' and r.get('hint_mode')=='off' and 'entity_recall' in r]; print(f'on={len(on)} off={len(off)}'); assert len(on)>0 and len(off)>0"`
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
cap:propose=approved
<!-- SECTION:NOTES:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 grep -q 'class VolcengineASRAdapter' docs/research/asr-bench/adapters/volcengine.py
- [ ] #2 grep -q 'supports_hints = True' docs/research/asr-bench/adapters/volcengine.py
- [ ] #3 grep -q '_create_vocab' docs/research/asr-bench/adapters/volcengine.py
- [ ] #4 grep -q '__main__' docs/research/asr-bench/adapters/volcengine.py
- [ ] #5 grep -q 'volcengine' docs/research/asr-bench/runner.py
- [ ] #6 grep -q 'VolcengineASRAdapter' docs/research/asr-bench/runner.py
- [ ] #7 test -n "$VOLCENGINE_APP_ID"
- [ ] #8 test -n "$VOLCENGINE_ACCESS_TOKEN"
- [ ] #9 python3 docs/research/asr-bench/adapters/volcengine.py --smoke-test
- [ ] #10 ls docs/research/asr-bench/results/run-*volcengine*.jsonl 2>/dev/null | head -1 | grep -q .
- [ ] #11 grep -q '"model": "volcengine"' docs/research/asr-bench/results/run-*volcengine*.jsonl
- [ ] #12 grep -q 'entity_recall' docs/research/asr-bench/results/volcengine-analysis.md
- [ ] #13 grep -q 'hint_mode' docs/research/asr-bench/results/volcengine-analysis.md
- [ ] #14 grep -q 'BREAKTHROUGH\|MARGINAL\|NO_EFFECT' docs/research/asr-bench/results/volcengine-analysis.md
- [ ] #15 grep -q '"hint_mode": "on"' docs/research/asr-bench/results/run-*volcengine*.jsonl
- [ ] #16 python3 -c "import json,glob; rows=[json.loads(l) for f in glob.glob('docs/research/asr-bench/results/run-*volcengine*.jsonl') for l in open(f)]; on=[r for r in rows if r.get('model')=='volcengine' and r.get('hint_mode')=='on' and 'entity_recall' in r]; off=[r for r in rows if r.get('model')=='volcengine' and r.get('hint_mode')=='off' and 'entity_recall' in r]; print(f'on={len(on)} off={len(off)}'); assert len(on)>0 and len(off)>0"
<!-- DOD:END -->

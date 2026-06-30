# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Build & Install

```bash
make build          # builds ./voci binary in repo root
make install        # go install → ~/go/bin/voci  (standard install target)
make test           # go test ./...
make clean          # removes ./voci binary

# Run a single test
go test ./internal/daemon/... -run TestHandleTranscribe
go test ./internal/pipeline/... -run TestRunHinted
go test ./internal/asr/... -run TestTranscribeGemini
```

The binary entry point is `cmd/voci/main.go` (3 lines); all logic lives in `internal/wire/wire.go`.

## Configuration

Config file: `~/.config/voci/config.yaml` (or `$VOCI_CONFIG`). Env vars take precedence over file.

Key env vars:

| Env var | Purpose |
|---|---|
| `ASR_API_KEY` | Required. Gemini API key (falls back to `SILICONFLOW_API_KEY`) |
| `ASR_PROVIDER` | `gemini` (default+recommended) or `siliconflow` |
| `ASR_MODEL` | Override ASR model; default `gemini-2.5-flash` |
| `VOCI_LANGUAGE` | ASR language code; default `zh` |
| `OLLAMA_HOST` | Ollama endpoint; default `http://localhost:11434` |
| `CLOUDFLARE_API_TOKEN` / `CF_ACCOUNT_ID` / `CF_ZONE_ID` / `CF_TUNNEL_DOMAIN` | Named Tunnel (all four required) |

Gemini is the **only** viable ASR provider — pure ASR APIs (Whisper, SenseVoice, Qwen3-ASR via SiliconFlow/OpenRouter) silently discard the prompt/entity hint. See `docs/adr/001-asr-provider-and-hint-injection.md`.

## Runtime Modes

```bash
voci serve [--serve-port 0] [--share] [--lock-dir ~/.voci] [--session-id <id>]
# HTTP daemon mode. Browser PTT → POST /api/voice/transcribe → Server-Sent Events to Monitor.
# --share: Quick Tunnel (cloudflared, no account) or Named Tunnel (if CF_* env vars set)
# --serve-port 0: OS assigns port (required with --share)

voci mcp
# MCP server on port 9473 (JSON-RPC 2.0). Exposes mcp__voci__transcribe tool.

voci once --file audio.wav [--iterate] [--input=preview|direct]
# One-shot CLI: transcribe → rewrite → classify → gate → inject
```

## Pipeline Architecture

Every audio input goes through 4 serial LLM steps in `internal/daemon/handlers.go`:

```
TranscribeFn   → Gemini Audio API (multimodal, entities injected via Config C few-shot prompt)
HintedFn       → RunHinted: text LLM corrects entities/terms using ## Known Entities hint
RewriteFn      → Rewrite: text LLM normalises to clean instruction (scope-preserving; may return [ambiguous])
ClassifyFn     → Classify: text LLM returns {"kind":"direct_prompt|backlog_action|query|ambiguous","confidence":0.9}
```

In `--serve` mode, the chat LLM is `asr.GeminiChat` when `ASR_PROVIDER=gemini`; otherwise `ollama.Chat` with `gemma4:e4b`. In `--session=integrated` (MCP) and CLI once modes, always `ollama.Chat`.

TASK-42/44 experiments evaluated merging these 4 calls into 1: with simple prompt, quality held (-0.4% classify accuracy, -32% latency). With Config C few-shot + merge, classify accuracy dropped -8.6% — unacceptable. **Current production: 4 serial calls.**

## Context / Hint System

`internal/context/builder.go` assembles the `asr_hint` string passed to every pipeline step. Sources (in order):

1. **KnownEntitiesSource** — `backlog/tasks/*.md` YAML frontmatter → task IDs with spoken-form mappings, plus hardcoded voci entity lines (`vocal → voci`, CLI flags, package paths)
2. **DynamicEntitiesSource** — extracts PascalCase/camelCase/snake_case/kebab tokens from recent Claude Code session dialogue; caps at 30 tokens; suppresses static entities
3. **BacklogSource** — active task list lines from frontmatter
4. **ClaudeMdSource** — `CLAUDE.md` full text
5. **GitLogSource** — `git log --oneline -10`
6. **SessionSource** — reads Claude Code JSONL session file: files edited, bash commands, TASK-N mentions, last 6 prose turns (≤500 chars each)

`Builder.BuildCached(root)` caches to `<root>/.voci/context_cache.json` with 60s TTL.

`asr.ExtractEntities(hint)` parses `## Known Entities` and `## Known Entities (dynamic)` sections to inject canonical entity names into the Gemini Audio API prompt.

## Delivery Path

After pipeline → gate confirmation → `internal/inject/`:

- `TmuxInjector` — `tmux send-keys -t <target> <text> Enter`
- `ClipboardInjector` — `xclip` → fallback `xdotool type`
- `ChainInjector` — tries each in order

In Monitor-host mode (`--serve`), confirmed text goes to `EventWriter` (stdout) as a JSONL line → consumed by Claude Code `Monitor` task → parsed by `voci-listen` skill.

## Key Invariants

- `RewriteFn` is **nil** in `--serve` mode (disabled; `HintedFn` output used directly). Only active in CLI once mode.
- All `/api/*` routes require `Authorization: Bearer <token>` when `--share-auth` is set. Token is auto-generated if not supplied.
- Per-session lock files live in `--lock-dir` (default `~/.voci`): `<sessionID>.lock` (JSON with PID+port) and `<sessionID>.task` (Monitor task ID). `SweepStaleLocks` removes locks whose PIDs are dead.
- `gate.Run` never auto-executes — human must type `confirm`, `edit`, or `discard`. EOF defaults to discard.
- Executor `KindDirectPrompt` returns text only; it does **not** run any command. Adapter/inject handles delivery.

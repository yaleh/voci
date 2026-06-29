# voci

Context-aware **voice input layer** for AI coding assistants.

voci turns spoken utterances into clean, project-aware instructions for AI coding
tools. It is **tool-agnostic**: Claude Code is the first integration, with Codex
and Gemini CLI planned.

## Core pipeline

```
browser/mic audio
  → context retrieval        (backlog tasks, CLAUDE.md, git log, session signals)
  → contextual ASR           (Gemini 2.5-flash, project terms injected as prompt)
  → intent rewrite           (transcript → clean instruction, ambiguity detection)
  → ActionProposal           (direct_prompt | backlog_action | query)
  → human confirmation gate  (preview / edit / discard — never auto-executes)
  → tool adapter delivery    (tmux send-keys / MCP / stdin)
```

## ASR provider decision

Gemini `generateContent` (multimodal) is the only hosted API where injecting a
known-entity list into the prompt measurably improves technical-term recall
(+0.357 entity_recall_exact vs baseline on zh-technical+zh-mixed speech).

Pure ASR APIs (Whisper, SenseVoice, Qwen3-ASR) discard the `prompt` field
server-side — confirmed across SiliconFlow, OpenRouter, and direct API
(TASK-34/36/37/40). See [`docs/adr/001-asr-provider-and-hint-injection.md`](docs/adr/001-asr-provider-and-hint-injection.md).

## Why not a heavy stack

voci is **not** a browser-terminal IDE. No PostgreSQL, Elasticsearch, Redis, or
remote-client registry. The hard part is *context selection* and *action gating*,
not the web framework. A lightweight FastAPI + JSONL service is enough.

## Epics

See `backlog/tasks/` (`backlog task list --plain`). The build order:

1. **Prototype** — voice → contextual ASR → rewrite, CLI only, no integration (validates the core hypothesis first)
2. **Context retrieval layer** — tool-agnostic `context_builder`
3. **Intent + ActionProposal + human gate** — the safety boundary
4. **Claude Code monitor** — separate/integrated session forms, preview/direct input modes
5. **Tool adapter abstraction** — Claude Code / Codex / Gemini CLI
6. **Web UI** — browser PTT, preview/edit, mode toggle

## Status

Greenfield. Start with the Prototype epic — it needs no Claude Code integration
and validates whether contextual ASR + iterative rewrite actually improves
instruction accuracy.

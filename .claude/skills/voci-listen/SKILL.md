---
name: voci-listen
description: "Arms a persistent Monitor with command=\"voci serve --share\" (Cloudflare Quick Tunnel + Bearer auth). Merges stderr into stdout via 2>&1 and grep-filters to three line types: JSON events (Rewritten field → execute inline), share-URL lines (display to user), and Bearer-token lines (display to user). Single-instance: sweeps stale voci-listen Monitor tasks before arming. Recovers across /clear via self-re-invoke hint in the Monitor description. Stops when ~/.voci/.listen-stop sentinel is present."
allowed-tools: Bash, Read, Monitor, TaskList, TaskStop
contracts:
  - grep: "Monitor(persistent=true"
    target: self
  - grep: 'command="voci serve'
    target: self
  - grep: "--share"
    target: self
  - grep: "description="
    target: self
  - not-grep: "schedule\x28"
    target: self
  - grep: "## Shutdown"
    target: self
  - grep: ".listen-stop"
    target: self
  - grep: "rewritten"
    target: self
  - grep: "TaskStop"
    target: self
  - grep: "TaskList"
    target: self
  - grep: "re-invoke"
    target: self
  - grep: "voci share URL"
    target: self
  - grep: "Bearer token"
    target: self
---

λ() → listenLoop()

## Spec

-- External primitives (provided by the Claude Code harness / shell environment;
-- not implemented in this skill)
Monitor      :: { persistent : Bool, command : String, description : String } → Event
exists       :: Path → Bool

-- Business logic signatures
listenLoop        :: () → Outcome          -- entry point
stopStaleMon      :: () → ()               -- stop orphaned Monitor tasks from a previous session
stopSentinel      :: () → Bool             -- true when ~/.voci/.listen-stop exists
classifyEvent     :: Line → EventKind      -- distinguish voice events from startup info lines
extractInstruction :: Line → String        -- parse JSON, return Rewritten field; raw fallback

data Outcome   = Listening | Stopped
data EventKind = VoiceEvent String         -- JSON line with Rewritten field → execute inline
               | InfoMessage String        -- "voci share URL:" or "Bearer token:" → display to user

-- ─────────────────────────────────────────────────────────────────────────────
-- Entry Point Guard: Cold-start vs. Reconnect
--
-- Cold-start (explicit /voci-listen invocation → λ() → listenLoop()):
--   Executes full bootstrap: stopStaleMon → checkStopSentinel → arm Monitor.
--
-- Reconnect (Monitor event arrives after /clear or context compaction):
--   Skip bootstrap entirely. The Monitor description instructs a fresh session
--   to re-invoke /voci-listen rather than executing the raw event directly.
--   The Monitor description carries the re-invoke hint.
-- ─────────────────────────────────────────────────────────────────────────────

listenLoop() = {
  _:      stopStaleMon(),

  if (stopSentinel()):
    return: Stopped,

  -- Arm persistent Monitor on voci serve --share.
  -- The command merges stderr→stdout (2>&1) and grep-filters to three line types:
  --   1. JSON event lines      (contain "rewritten") → voice instruction to execute
  --   2. "voci share URL: …"  (from stderr)         → Cloudflare URL to display
  --   3. "Bearer token:   …"  (from stderr)         → auth token to display
  -- On wake-up: classifyEvent decides whether to display or execute.
  -- The description carries a re-invoke hint for cross-/clear recovery.
  event: Monitor(persistent=true,
           command="voci serve --share 2>&1 | grep --line-buffered -E '\"Rewritten\"|voci share URL|Bearer token'",
           description="voci-listen: a voice event has arrived — extract the Rewritten field from the JSON line and execute it as the next in-session instruction; if this is a new session (after /clear or context compaction) re-invoke /voci-listen first to restore the listening loop"),

  if (stopSentinel()):
    return: Stopped,

  kind: classifyEvent(event),
  | InfoMessage text → display(text),   -- show URL / token to user, do NOT execute
  | VoiceEvent line  → execute(extractInstruction(line)),

  return: listenLoop(),   -- re-arm for the next event (voci serve is persistent)
}

stopStaleMon() = {
  -- Step 1: Call TaskList harness tool to enumerate all active background tasks.
  -- Step 2: Filter entries whose description contains "voci-listen".
  -- Step 3: For each matching task ID: call TaskStop <task-id> harness tool.
  --         echo "[voci-listen] stopStaleMon: stopping stale Monitor <task-id>"
  -- TaskList and TaskStop are harness primitives — invoke as tool calls,
  -- not as bash commands. Do NOT use shell process signals.
}

classifyEvent :: Line → EventKind
classifyEvent(line) =
  | line starts with "voci share URL:" → InfoMessage(line)
  | line starts with "Bearer token:"   → InfoMessage(line)
  | otherwise                          → VoiceEvent(line)

extractInstruction :: Line → String
extractInstruction(line) =
  | line is valid JSON and has "rewritten" field → line["rewritten"]
  | otherwise                                    → line   -- raw fallback

## Implementation

### stopStaleMon

Call `TaskList` (harness tool, not a shell command) to enumerate all active background tasks.
Filter entries whose `description` contains `"voci-listen"`. For each matched task, call
`TaskStop <task-id>` to terminate it before arming the new Monitor.

```
# Pseudocode — these are harness tool calls, not bash commands:
tasks = TaskList()
for task in tasks:
  if "voci-listen" in task.description:
    echo "[voci-listen] stopStaleMon: stopping stale Monitor " + task.id
    TaskStop(task.id)
```

### stopSentinel check

```bash
STOP_FILE="${HOME}/.voci/.listen-stop"
if [ -f "$STOP_FILE" ]; then
  echo "[voci-listen] stop sentinel found — exiting."
  exit 0
fi
```

### Arm Monitor

After `stopStaleMon` and the sentinel check, arm the persistent Monitor:

```
Monitor(persistent=true,
  command="voci serve --share 2>&1 | grep --line-buffered -E '\"Rewritten\"|voci share URL|Bearer token'",
  description="voci-listen: a voice event has arrived — extract the Rewritten field from the JSON line and execute it as the next in-session instruction; if this is a new session (after /clear or context compaction) re-invoke /voci-listen first to restore the listening loop"
)
```

`voci serve --share` starts the HTTP listener, launches a Cloudflare Quick Tunnel, and
writes the public URL and Bearer token to stderr. The `2>&1 | grep` pipeline routes
stderr into stdout and filters down to three line patterns:

| Pattern | Source | Action |
|---|---|---|
| `"rewritten"` | JSON event (stdout) | extract Rewritten → execute inline |
| `voci share URL` | stderr startup line | display to user |
| `Bearer token` | stderr startup line | display to user |

### classifyEvent (per-line handler)

On each Monitor wake-up, classify the line before acting:

```
if line starts with "voci share URL:" or "Bearer token:":
    # Startup info — display directly to the user
    print(line)
    re-arm (continue listenLoop)
else:
    # Voice event — extract instruction and execute
    INSTRUCTION = extractInstruction(line)
    execute(INSTRUCTION)
    re-arm
```

### extractInstruction (JSON extraction)

```bash
LINE="$1"   # raw line from voci serve stdout

# Parse JSON and extract the Rewritten instruction field (see scripts/extract-instruction.py).
INSTRUCTION=$(echo "$LINE" | python3 scripts/extract-instruction.py)

echo "[voci-listen] instruction: $INSTRUCTION"
```

### Inline execution

Execute the extracted instruction **inline** in the current Claude Code session — never
via a sub-agent. Treat `$INSTRUCTION` as the next user message and act on it directly,
using whatever tools are appropriate for the requested action.

### Cross-/clear self-recovery

The Monitor `description` field contains the re-invoke hint:

> "… if this is a new session (after /clear or context compaction) re-invoke /voci-listen first to restore the listening loop"

When Claude Code starts a new session and the Monitor fires, the description instructs
the fresh session to run `/voci-listen` before acting on the event line. This restores
the full listening loop without losing the event.

## Shutdown

To stop the voci-listen loop, write the stop sentinel:

```bash
touch ~/.voci/.listen-stop
```

To restart the loop after clearing the sentinel:

```bash
rm -f ~/.voci/.listen-stop
# then invoke /voci-listen again
```

The skill checks for `~/.voci/.listen-stop` in two places:
1. At bootstrap (before arming Monitor) — returns `Stopped` immediately if present.
2. After each Monitor wake-up (before executing the instruction) — stops cleanly mid-session.

To remove the sentinel and resume:

```bash
rm ~/.voci/.listen-stop
```

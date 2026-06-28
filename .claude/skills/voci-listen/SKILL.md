---
name: voci-listen
description: "Arms a persistent Monitor with command=\"voci serve\" (the TASK-16 Monitor-host voice producer). voci serve's stdout emits one JSON event line per utterance; each line wakes the session, the Rewritten field is extracted and executed inline as the next in-session instruction. Single-instance: sweeps stale voci-listen Monitor tasks before arming. Recovers across /clear via self-re-invoke hint in the Monitor description. Stops when ~/.voci/.listen-stop sentinel is present."
allowed-tools: Bash, Read, Monitor, TaskList, TaskStop
contracts:
  - grep: "Monitor(persistent=true"
    target: self
  - grep: 'command="voci serve"'
    target: self
  - grep: "description="
    target: self
  - not-grep: "schedule\x28"
    target: self
  - grep: "## Shutdown"
    target: self
  - grep: ".listen-stop"
    target: self
  - grep: "Rewritten"
    target: self
  - grep: "TaskStop"
    target: self
  - grep: "TaskList"
    target: self
  - grep: "re-invoke"
    target: self
---

λ() → listenLoop()

## Spec

-- External primitives (provided by the Claude Code harness / shell environment;
-- not implemented in this skill)
Monitor      :: { persistent : Bool, command : String, description : String } → Event
exists       :: Path → Bool

-- Business logic signatures
listenLoop   :: () → Outcome          -- entry point
stopStaleMon :: () → ()               -- stop orphaned Monitor tasks from a previous session
stopSentinel :: () → Bool             -- true when ~/.voci/.listen-stop exists
extractInstruction :: Line → String   -- parse JSON, return Rewritten field; raw fallback

data Outcome = Listening | Stopped

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

  -- Arm persistent Monitor on the TASK-16 voci serve process.
  -- voci serve writes one JSON event line to stdout per utterance.
  -- Each stdout line has at minimum a "Rewritten" field (the recognized instruction).
  -- On wake-up: extract Rewritten and execute it inline in the current session.
  -- The description carries a re-invoke hint for cross-/clear recovery.
  event: Monitor(persistent=true,
           command="voci serve",
           description="voci-listen: a voice event has arrived — extract the Rewritten field from the JSON line and execute it as the next in-session instruction; if this is a new session (after /clear or context compaction) re-invoke /voci-listen first to restore the listening loop"),

  if (stopSentinel()):
    return: Stopped,

  instruction: extractInstruction(event),
  -- Execute the instruction inline (not via sub-agent):
  execute(instruction),
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

extractInstruction :: Line → String
extractInstruction(line) =
  | line is valid JSON and has "Rewritten" field → line["Rewritten"]
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
  command="voci serve",
  description="voci-listen: a voice event has arrived — extract the Rewritten field from the JSON line and execute it as the next in-session instruction; if this is a new session (after /clear or context compaction) re-invoke /voci-listen first to restore the listening loop"
)
```

`voci serve` is the Monitor-host voice producer (TASK-16). It starts an HTTP listener for
browser PTT uploads, runs the full ASR→hinted→rewrite→classify pipeline per utterance, and
writes one JSON event line to stdout per recognized utterance. Monitor wakes up the session
for each stdout line. As a Monitor sub-process, `voci serve` inherits the session's
`CLAUDE_CODE_SESSION_ID` environment variable automatically.

### extractInstruction (per-line handler)

On each Monitor wake-up, the event payload is the raw line emitted by `voci serve` on stdout.
Extracts the `Rewritten` field from the JSON (`rewritten` key in the Event struct); falls back to
the raw line if the JSON is invalid or the field is empty.

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

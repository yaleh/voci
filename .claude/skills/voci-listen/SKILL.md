---
name: voci-listen
description: "Arms a persistent Monitor with voci serve --share --serve-port 0 (OS-assigned port + Cloudflare Quick Tunnel + Bearer auth). voci serve writes its own per-session lock file to --lock-dir and removes it on exit; no separate background start needed. Merges stderr into stdout via stderr redirect and grep-filters to three line types: JSON events (Rewritten field → execute inline), share-URL lines (display to user), and Bearer-token lines (display to user). Single-instance: sweeps stale voci-listen Monitor tasks before arming. Recovers across /clear via reconnectGuard (detects live Monitor task via TaskList). Stops when ~/.voci/.listen-stop sentinel is present."
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
stopStaleMon      :: () → ()               -- stop orphaned Monitor tasks + sweep stale lock files
manageLock        :: () → SESSION_ID       -- cold-start: generate UUID for session, no background start
reconnectGuard    :: () → (Bool, SESSION_ID, PORT) -- true if a live Monitor task + lock exists
stopSentinel      :: () → Bool             -- true when ~/.voci/.listen-stop exists
classifyEvent     :: Line → EventKind      -- distinguish voice events from startup info lines
extractInstruction :: Line → String        -- parse JSON, return Rewritten field; raw fallback
cleanupLock       :: SESSION_ID → ()       -- remove ~/.voci/<SESSION_ID>.lock on shutdown

data Outcome   = Listening | Stopped
data EventKind = VoiceEvent String         -- JSON line with Rewritten field → execute inline
               | InfoMessage String        -- "voci share URL:" or "Bearer token:" → display to user

-- ─────────────────────────────────────────────────────────────────────────────
-- Entry Point Guard: Cold-start vs. Reconnect
--
-- Cold-start (explicit /voci-listen invocation → λ() → listenLoop()):
--   Executes full bootstrap: stopStaleMon → sweepStaleLocks → checkStopSentinel
--   → manageLock (start voci serve, get PORT) → arm Monitor.
--
-- Reconnect (Monitor event arrives after /clear or context compaction):
--   Skip bootstrap entirely. The Monitor description instructs a fresh session
--   to re-invoke /voci-listen rather than executing the raw event directly.
--   The Monitor description carries the re-invoke hint.
--   On re-invoke, reconnectGuard detects existing live lock → re-arms Monitor
--   on the same PORT without restarting voci serve or touching lock files.
-- ─────────────────────────────────────────────────────────────────────────────

listenLoop() = {
  _: stopStaleMon(),    -- stop orphaned Monitor tasks AND sweep stale .lock files

  if (stopSentinel()):
    return: Stopped,

  -- reconnectGuard: if this session already has a live lock file and the recorded
  -- PID is still alive, skip cold-start and re-arm Monitor on the same PORT.
  (live, SESSION_ID, PORT): reconnectGuard(),
  if (live):
    goto: armMonitor(SESSION_ID, PORT),

  -- Cold-start: generate a session ID; voci serve itself handles port assignment
  -- (--serve-port 0), lock writing (--lock-dir), and lock cleanup (on exit).
  SESSION_ID: manageLock(),

  armMonitor(SESSION_ID):
  -- Arm persistent Monitor on voci serve --share with OS-assigned port.
  -- --lock-dir ~/.voci and --session-id $SESSION_ID are passed to voci serve so
  -- the binary writes ~/.voci/$SESSION_ID.lock once the listener is ready and
  -- removes it on clean exit (no separate manageLock bash dance needed).
  -- The command merges stderr→stdout (stderr→stdout redirect) and grep-filters to three line types:
  --   1. JSON event lines      (contain "rewritten") → voice instruction to execute
  --   2. "voci share URL: …"  (from stderr)         → Cloudflare URL to display
  --   3. "Bearer token:   …"  (from stderr)         → auth token to display
  -- On wake-up: classifyEvent decides whether to display or execute.
  -- The description carries a re-invoke hint for cross-/clear recovery.
  event: Monitor(persistent=true,
           command="voci serve --share --serve-port 0 --lock-dir ~/.voci --session-id $SESSION_ID 2>/dev/stdout | grep --line-buffered -E '\"rewritten\"|voci share URL|Bearer token'",
           description="voci-listen: a voice event has arrived — extract the Rewritten field from the JSON line and execute it as the next in-session instruction; if this is a new session (after /clear or context compaction) re-invoke /voci-listen first to restore the listening loop"),

  if (stopSentinel()):
    cleanupLock(SESSION_ID),
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
  -- Step 4: The voci binary sweeps stale lock files automatically via SweepStaleLocks
  --         when --lock-dir is passed; no separate bash sweep is needed here.
}

manageLock() = {
  -- Generate SESSION_ID (32-char hex via uuidgen or /proc/sys/kernel/random/uuid).
  -- No background voci serve start — the Monitor command owns the process.
  -- voci serve --lock-dir ~/.voci --session-id $SESSION_ID writes the lock itself
  -- in its OnListening callback and removes it on exit via defer.
  -- Return SESSION_ID.
}

reconnectGuard() = {
  -- Call TaskList harness tool.
  -- If any task has description containing "voci-listen" AND status RUNNING:
  --   Read ~/.voci/*.lock to find the live session (written by voci serve).
  --   return (live=true, SESSION_ID, PORT_from_lock)
  -- Otherwise:
  --   return (live=false, "", 0)
  -- Liveness is inferred from the Monitor task status; no bash kill -0 loop needed.
}

cleanupLock(SESSION_ID) = {
  -- rm -f ~/.voci/$SESSION_ID.lock
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
`TaskStop <task-id>` to terminate it before arming the new Monitor. Then call `sweepStaleLocks`.

```
# Pseudocode — these are harness tool calls, not bash commands:
tasks = TaskList()
for task in tasks:
  if "voci-listen" in task.description:
    echo "[voci-listen] stopStaleMon: stopping stale Monitor " + task.id
    TaskStop(task.id)
# then sweep stale lock files (see sweepStaleLocks below)
```

### sweepStaleLocks

Stale lock cleanup is now handled by the `voci` binary via `daemon.SweepStaleLocks`
when `--lock-dir` is passed. No bash sweep is required here.

### stopSentinel check

```bash
STOP_FILE="${HOME}/.voci/.listen-stop"
if [ -f "$STOP_FILE" ]; then
  echo "[voci-listen] stop sentinel found — exiting."
  exit 0
fi
```

### manageLock (cold-start)

Generate a session ID only. The Monitor command is responsible for starting `voci serve`
with `--lock-dir` and `--session-id`; the binary writes the lock file itself.

```bash
VOCI_DIR="${HOME}/.voci"
mkdir -p "$VOCI_DIR"

SESSION_ID=$(uuidgen || cat /proc/sys/kernel/random/uuid)
echo "[voci-listen] manageLock: session=$SESSION_ID"
# No background voci serve start; the Monitor command below owns the process.
```

### reconnectGuard

On Monitor re-invoke (new session after `/clear`), check whether a voci-listen Monitor
task is still running using the TaskList harness tool:

```
# Pseudocode — these are harness tool calls, not bash commands:
tasks = TaskList()
running_voci = [t for t in tasks if "voci-listen" in t.description and t.status == RUNNING]
if running_voci:
  # Read the live lock to recover SESSION_ID and PORT for display.
  for f in ~/.voci/*.lock:
    entry = ReadLock(f)
    SESSION_ID = entry.session_id
    PORT       = entry.port
    echo "[voci-listen] reconnectGuard: reusing session=$SESSION_ID port=$PORT"
    return (live=true, SESSION_ID, PORT)
else:
  echo "[voci-listen] reconnectGuard: no live session found — proceeding with cold-start"
  return (live=false, "", 0)
```

Liveness is determined by Monitor task status, not by `kill -0` PID checks.

### Arm Monitor

After `manageLock` or `reconnectGuard`, arm the persistent Monitor using `$SESSION_ID`.
`--serve-port 0` lets the OS assign a free port; `--lock-dir` and `--session-id` tell
the binary to write and clean up its own lock file:

```
Monitor(persistent=true,
  command="voci serve --share --serve-port 0 --lock-dir ~/.voci --session-id $SESSION_ID 2>/dev/stdout | grep --line-buffered -E '\"rewritten\"|voci share URL|Bearer token'",
  description="voci-listen: a voice event has arrived — extract the Rewritten field from the JSON line and execute it as the next in-session instruction; if this is a new session (after /clear or context compaction) re-invoke /voci-listen first to restore the listening loop"
)
```

`voci serve --share --serve-port 0` starts the HTTP listener on an OS-assigned port,
launches a Cloudflare Quick Tunnel, and writes the public URL and Bearer token to stderr.
The binary writes `~/.voci/$SESSION_ID.lock` (with real PID and port) once listening,
and removes it on clean exit. The `2>/dev/stdout | grep` pipeline routes stderr into stdout and
filters down to three line patterns:

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
the fresh session to run `/voci-listen` before acting on the event line. On re-invoke,
`reconnectGuard` detects the live lock file and re-arms the Monitor on the same `PORT`
without restarting `voci serve` or touching lock files.

### cleanupLock

On clean shutdown (stop sentinel reached):

```bash
rm -f "${HOME}/.voci/${SESSION_ID}.lock"
echo "[voci-listen] cleanupLock: removed $LOCK_FILE"
```

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
   `cleanupLock` is called before exiting to remove the session's `.lock` file.

To remove the sentinel and resume:

```bash
rm ~/.voci/.listen-stop
```

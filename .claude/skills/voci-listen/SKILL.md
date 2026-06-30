---
name: voci-listen
description: "Arms a persistent Monitor with \"voci serve --share --serve-port $PORT\" (per-session OS-assigned port + Cloudflare Quick Tunnel + Bearer auth). Each session writes a per-session lock file ~/.voci/<SESSION_ID>.lock; stale locks (dead PID) are swept on cold-start. Merges stderr into stdout via 2>&1 and grep-filters to three line types: JSON events (Rewritten field → execute inline), share-URL lines (display to user), and Bearer-token lines (display to user). Single-instance: sweeps stale voci-listen Monitor tasks before arming. Recovers across /clear via reconnectGuard (re-arms Monitor on existing port if lock+PID still live). Stops when ~/.voci/.listen-stop sentinel is present."
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
sweepStaleLocks   :: () → ()               -- remove ~/.voci/*.lock files whose PID is dead
manageLock        :: () → (SESSION_ID, PORT) -- cold-start: generate UUID, start voci serve, write lock
reconnectGuard    :: () → Bool             -- true if this session's lock+PID is still live (skip cold-start)
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

  -- Cold-start: generate a UUID session ID, let OS pick a free port (--serve-port 0),
  -- capture the resolved port from stderr, write ~/.voci/<SESSION_ID>.lock.
  (SESSION_ID, PORT): manageLock(),

  armMonitor(SESSION_ID, PORT):
  -- Arm persistent Monitor on voci serve --share with the per-session port.
  -- The command merges stderr→stdout (2>&1) and grep-filters to three line types:
  --   1. JSON event lines      (contain "rewritten") → voice instruction to execute
  --   2. "voci share URL: …"  (from stderr)         → Cloudflare URL to display
  --   3. "Bearer token:   …"  (from stderr)         → auth token to display
  -- On wake-up: classifyEvent decides whether to display or execute.
  -- The description carries a re-invoke hint for cross-/clear recovery.
  event: Monitor(persistent=true,
           command="voci serve --share --serve-port $PORT 2>&1 | grep --line-buffered -E '\"rewritten\"|voci share URL|Bearer token'",
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
  -- Step 4: Call sweepStaleLocks() to remove orphaned .lock files.
}

sweepStaleLocks() = {
  -- Iterate ~/.voci/*.lock; for each file extract the "pid" field via jq;
  -- if kill -0 $pid returns non-zero (process gone), delete the file.
  -- A live PID is never signaled — kill -0 is a pure existence check.
}

manageLock() = {
  -- Generate SESSION_ID=$(uuidgen)
  -- Start voci serve --share --serve-port 0 in the background, capturing stderr.
  -- Parse "voci serve: listening on <host>:<PORT>" from stderr to extract PORT.
  -- Record VOCI_PID=$! (the voci serve process PID).
  -- Write ~/.voci/$SESSION_ID.lock as JSON: {"session_id":"...","pid":VOCI_PID,"port":PORT}
  -- Return (SESSION_ID, PORT).
}

reconnectGuard() = {
  -- Check whether SESSION_ID env var (or a known lock file) points to a still-live session.
  -- If ~/.voci/$SESSION_ID.lock exists and kill -0 $recorded_pid succeeds:
  --   return (live=true, SESSION_ID, PORT_from_lock)
  -- Otherwise:
  --   return (live=false, "", 0)
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

```bash
VOCI_DIR="${HOME}/.voci"
for f in "$VOCI_DIR"/*.lock; do
  [ -f "$f" ] || continue
  pid=$(jq -r '.pid // empty' "$f" 2>/dev/null)
  if [ -z "$pid" ] || ! kill -0 "$pid" 2>/dev/null; then
    echo "[voci-listen] sweepStaleLocks: removing stale lock $f (pid=$pid)"
    rm -f "$f"
  fi
done
```

### stopSentinel check

```bash
STOP_FILE="${HOME}/.voci/.listen-stop"
if [ -f "$STOP_FILE" ]; then
  echo "[voci-listen] stop sentinel found — exiting."
  exit 0
fi
```

### manageLock (cold-start)

```bash
VOCI_DIR="${HOME}/.voci"
mkdir -p "$VOCI_DIR"

SESSION_ID=$(uuidgen)
LOCK_FILE="$VOCI_DIR/$SESSION_ID.lock"

# Start voci serve with OS-assigned port; capture stderr to extract port.
TMPLOG=$(mktemp)
voci serve --share --serve-port 0 2>"$TMPLOG" &
VOCI_PID=$!

# Wait for "voci serve: listening on <addr>" line (up to 10s).
PORT=""
for i in $(seq 1 100); do
  PORT=$(grep -oP '(?<=voci serve: listening on )[^:]+:\K[0-9]+' "$TMPLOG" 2>/dev/null | head -1)
  [ -n "$PORT" ] && break
  sleep 0.1
done
rm -f "$TMPLOG"

if [ -z "$PORT" ]; then
  echo "[voci-listen] manageLock: failed to capture port from voci serve stderr" >&2
  kill "$VOCI_PID" 2>/dev/null
  exit 1
fi

# Write lock file.
printf '{"session_id":"%s","pid":%d,"port":%d}' "$SESSION_ID" "$VOCI_PID" "$PORT" > "$LOCK_FILE"
echo "[voci-listen] manageLock: session=$SESSION_ID pid=$VOCI_PID port=$PORT lock=$LOCK_FILE"
```

### reconnectGuard

On Monitor re-invoke (new session after `/clear`), check if the current session's
lock file is still valid:

```bash
VOCI_DIR="${HOME}/.voci"
# SESSION_ID is set from a prior cold-start (passed via Monitor description or env).
# In practice the skill re-invokes itself, so SESSION_ID must be recoverable.
# Look for exactly one lock file whose PID is alive.
LIVE_LOCK=""
for f in "$VOCI_DIR"/*.lock; do
  [ -f "$f" ] || continue
  pid=$(jq -r '.pid // empty' "$f" 2>/dev/null)
  if [ -n "$pid" ] && kill -0 "$pid" 2>/dev/null; then
    LIVE_LOCK="$f"
    SESSION_ID=$(jq -r '.session_id' "$f")
    PORT=$(jq -r '.port' "$f")
    break
  fi
done

if [ -n "$LIVE_LOCK" ]; then
  echo "[voci-listen] reconnectGuard: reusing session=$SESSION_ID pid=$pid port=$PORT"
  # → skip manageLock, arm Monitor directly on PORT
else
  echo "[voci-listen] reconnectGuard: no live session found — proceeding with cold-start"
  # → run manageLock
fi
```

### Arm Monitor

After `manageLock` or `reconnectGuard`, arm the persistent Monitor using the resolved `$PORT`:

```
Monitor(persistent=true,
  command="voci serve --share --serve-port $PORT 2>&1 | grep --line-buffered -E '\"rewritten\"|voci share URL|Bearer token'",
  description="voci-listen: a voice event has arrived — extract the Rewritten field from the JSON line and execute it as the next in-session instruction; if this is a new session (after /clear or context compaction) re-invoke /voci-listen first to restore the listening loop"
)
```

`voci serve --share --serve-port $PORT` starts the HTTP listener on `$PORT`, launches a
Cloudflare Quick Tunnel, and writes the public URL and Bearer token to stderr. The
`2>&1 | grep` pipeline routes stderr into stdout and filters down to three line patterns:

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

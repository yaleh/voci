---
name: voci-listen
description: "Arms a persistent Monitor with voci serve --share --serve-port 0 (OS-assigned port + Cloudflare Quick Tunnel + Bearer auth). voci serve writes its own per-session lock file to --lock-dir and removes it on exit; no separate background start needed. Merges stderr into stdout via stderr redirect and grep-filters to three line types: JSON events (Rewritten field → execute inline), share-URL lines (display to user), and Bearer-token lines (display to user). Single-instance: sweeps stale voci-listen Monitor tasks before arming. Monitor description is self-contained: on event arrival in any session, classify and dispatch directly without calling the skill again. Stops when ~/.voci/.listen-stop sentinel is present."
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
  - not-grep: "re-invoke"
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
ensureMonitor     :: SESSION_ID → ()           -- idempotent Monitor arm: TaskList check before Monitor call
coldStart         :: () → Outcome              -- explicit invocation entry: full bootstrap → ensureMonitor
onMonitorEvent    :: Line → Outcome            -- Monitor-event entry: classify → display or execute; no bootstrap

data Outcome   = Listening | Stopped
data EventKind = VoiceEvent String         -- JSON line with Rewritten field → execute inline
               | InfoMessage String        -- "voci local URL:", "voci share URL:", or "Bearer token:" → display to user

-- ─────────────────────────────────────────────────────────────────────────────
-- Entry Point Guard: Cold-start vs. Monitor-event dispatch
--
-- Cold-start (explicit /voci-listen invocation → λ() → coldStart()):
--   Executes full bootstrap: stopStaleMon → checkStopSentinel
--   → manageLock (generate SESSION_ID) → ensureMonitor(SESSION_ID).
--
-- Monitor-event dispatch (Monitor fires in any session → onMonitorEvent(line)):
--   The Monitor description carries full dispatch instructions; the fresh session
--   classifies the line and acts directly. No skill bootstrap. No cold-start.
-- ─────────────────────────────────────────────────────────────────────────────

listenLoop() = coldStart()

coldStart() = {
  -- Explicit /voci-listen invocation path. Full bootstrap sequence.
  _: stopStaleMon(),    -- stop orphaned Monitor tasks AND sweep stale .lock files

  if (stopSentinel()):
    return: Stopped,

  -- reconnectGuard: detect existing live lock to avoid cold-starting when a
  -- voci serve process is still running.
  (live, SESSION_ID, PORT): reconnectGuard(),
  if (live):
    ensureMonitor(SESSION_ID),
    return: Listening,

  -- Cold-start: generate a session ID; voci serve handles port assignment
  -- (--serve-port 0), lock writing (--lock-dir), and lock cleanup (on exit).
  SESSION_ID: manageLock(),
  ensureMonitor(SESSION_ID),
  return: Listening,
}

onMonitorEvent(line) = {
  -- Monitor-event dispatch path. No bootstrap, no restart.
  if (stopSentinel()):
    cleanupLock(SESSION_ID),
    return: Stopped,
  kind: classifyEvent(line),
  | InfoMessage text → display(text),
  | VoiceEvent line  → execute(extractInstruction(line)),
  return: Listening,
}

ensureMonitor(SESSION_ID) = {
  -- Idempotency check: avoid arming a duplicate Monitor if one is already live.
  -- Step 1: Call TaskList to enumerate all active background tasks.
  -- Step 2: Filter entries whose description contains "voci-listen".
  -- Step 3: If any live match found, return early — do NOT call Monitor again.
  --         On TaskList failure, treat as "no live Monitor" and proceed to arm.
  tasks: TaskList(),
  if (any task in tasks where "voci-listen" in task.description):
    echo "[voci-listen] ensureMonitor: live Monitor already exists — skipping arm"
    return: (),

  -- No live Monitor found: arm a new persistent Monitor.
  -- --serve-port 0: OS assigns port; --lock-dir/--session-id: binary self-manages lock.
  -- The command merges stderr→stdout (2>/dev/stdout) and grep-filters to three line types:
  --   1. JSON event lines      (contain "rewritten") → voice instruction to execute
  --   2. "voci local URL: …"  (from stderr)         → local HTTP URL to display
  --   3. "voci share URL: …"  (from stderr)         → Cloudflare URL to display
  --   4. "Bearer token:   …"  (from stderr)         → auth token to display
  -- On wake-up: onMonitorEvent classifies and dispatches directly. No skill restart.
  Monitor(persistent=true,
    command="voci serve --share --serve-port 0 --lock-dir ~/.voci --session-id $SESSION_ID 2>/dev/stdout | grep --line-buffered -E '\"rewritten\"|voci local URL|voci share URL|Bearer token'",
    description="voci-listen: voice event arrived — DO NOT call /voci-listen again. Classify the line: if it starts with 'voci local URL:', 'voci share URL:', or 'Bearer token:' → display to user; otherwise → parse JSON, extract the 'rewritten' field, execute it inline as the next instruction.")
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
  -- This avoids repeating cold-start work when a Monitor is already alive.
  -- Otherwise:
  --   return (live=false, "", 0)
  -- Liveness is inferred from Monitor task status; no bash kill -0 loop needed.
}

cleanupLock(SESSION_ID) = {
  -- rm -f ~/.voci/$SESSION_ID.lock
}

classifyEvent :: Line → EventKind
classifyEvent(line) =
  | line starts with "voci local URL:" → InfoMessage(line)
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
`TaskStop <task-id>` to terminate it before arming the new Monitor. The voci binary
handles stale lock cleanup automatically via `SweepStaleLocks` when `--lock-dir` is passed.

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

On cold-start, check whether a voci-listen Monitor task is still running using the
TaskList harness tool:

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

### ensureMonitor

Called after `manageLock` or `reconnectGuard`. First performs a `TaskList` idempotency
check: if any active task already has `"voci-listen"` in its description, skip arming and
return immediately. On `TaskList` failure, treat as "no live Monitor" and proceed to arm.
If no live Monitor is found, arms a new persistent Monitor with `--serve-port 0 --lock-dir`:

```
# Idempotency check (harness tool calls):
tasks = TaskList()
for task in tasks:
  if "voci-listen" in task.description:
    echo "[voci-listen] ensureMonitor: live Monitor already exists — skipping arm"
    return

# Arm new Monitor:
Monitor(persistent=true,
  command="voci serve --share --serve-port 0 --lock-dir ~/.voci --session-id $SESSION_ID 2>/dev/stdout | grep --line-buffered -E '\"rewritten\"|voci local URL|voci share URL|Bearer token'",
  description="voci-listen: voice event arrived — DO NOT call /voci-listen again. Classify the line: if it starts with 'voci local URL:', 'voci share URL:', or 'Bearer token:' → display to user; otherwise → parse JSON, extract the 'rewritten' field, execute it inline as the next instruction."
)
```

`voci serve --share --serve-port 0` starts the HTTP listener on an OS-assigned port,
launches a Cloudflare Quick Tunnel, and writes the public URL and Bearer token to stderr.
The binary writes `~/.voci/$SESSION_ID.lock` (with real PID and port) once listening, and
removes it on clean exit. The `2>/dev/stdout | grep` pipeline routes stderr into stdout
and filters down to three line patterns:

| Pattern | Source | Action |
|---|---|---|
| `"rewritten"` | JSON event (stdout) | extract Rewritten → execute inline |
| `voci local URL` | stderr startup line | display to user |
| `voci share URL` | stderr startup line | display to user |
| `Bearer token` | stderr startup line | display to user |

### classifyEvent (per-line handler)

On each Monitor wake-up, classify the line before acting:

```
if line starts with "voci local URL:" or "voci share URL:" or "Bearer token:":
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

The Monitor `description` field is self-contained. When a Monitor event arrives in a
new session (after `/clear` or context compaction), the description instructs Claude to
classify the line and act directly — no skill call is needed. If the line
starts with `"voci local URL:"`, `"voci share URL:"`, or `"Bearer token:"`, display it to the user.
Otherwise, extract the `rewritten` field from the JSON and execute it inline.

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

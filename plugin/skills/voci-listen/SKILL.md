---
name: voci-listen
description: "Arms a persistent Monitor with voci serve --share --serve-port 0 (OS-assigned port + Cloudflare Quick Tunnel + Bearer auth). voci serve writes its own per-session lock file to --lock-dir, removes it on exit, and emits a startup JSON event (type=startup) to stdout for Monitor display — no separate background start or status poll needed. Monitor command is a bare voci serve with no shell pipeline. Single-instance: sweeps stale voci-listen Monitor tasks before arming. Monitor description is self-contained: on startup event arrival display URL/token to user; on voice event extract 'rewritten' and execute inline. Stops when ~/.voci/.listen-stop sentinel is present."
allowed-tools: Bash, Read, Monitor
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
  - grep: "listen-preflight"
    target: self
  - grep: ".status"
    target: self
  - grep: "StartupEvent"
    target: self
  - not-grep: "\\.task\\b"
    target: self
  - not-grep: "TaskOutput"
    target: self
  - not-grep: "TaskList"
    target: self
  - not-grep: "re-invoke"
    target: self
  - not-grep: "2>\x2fdev\x2fstdout"
    target: self
  - not-grep: "\x7c grep --line-buffered"
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
stopSentinel      :: () → Bool             -- true when ~/.voci/.listen-stop exists
classifyEvent     :: Line → EventKind      -- distinguish voice events from startup info lines
extractInstruction :: Line → String        -- parse JSON, return Rewritten field; raw fallback
cleanupLock       :: SESSION_ID → ()       -- remove ~/.voci/<SESSION_ID>.lock on shutdown
coldStart         :: () → Outcome              -- explicit invocation entry: 1 Bash + 1 Monitor
onMonitorEvent    :: Line → Outcome            -- Monitor-event entry: classify → display or execute; no bootstrap

data Outcome   = Listening | Stopped
data EventKind = VoiceEvent String         -- JSON line with Rewritten field → execute inline
               | StartupEvent String       -- JSON line with type=="startup" → display URL, no execute

-- ─────────────────────────────────────────────────────────────────────────────
-- Entry Point Guard: Cold-start vs. Monitor-event dispatch
--
-- Cold-start (explicit /voci-listen invocation → λ() → coldStart()):
--   Executes 1 Bash to run listen-preflight, then 1 Monitor if coldstart.
--
-- Monitor-event dispatch (Monitor fires in any session → onMonitorEvent(line)):
--   The Monitor description carries full dispatch instructions; the fresh session
--   classifies the line and acts directly. No skill bootstrap. No cold-start.
-- ─────────────────────────────────────────────────────────────────────────────

listenLoop() = coldStart()

coldStart() = {
  -- Step 1: Run listen-preflight as a single Bash call. No TaskStop step —
  -- a live voci serve yields "reconnect"; a dead one already ended its own
  -- Monitor (the Monitor terminates when its child process exits). The .lock
  -- file (PID+port) is the sole liveness source.
  -- Output is a single line: "stopped", "coldstart <cc_pid>", or "reconnect <local> <share> <token>".
  PREFLIGHT = Bash("voci listen-preflight --lock-dir ~/.voci")
  LINE = first line of PREFLIGHT output

  -- Step 2: Dispatch on output.
  switch (first word of LINE):
    case "stopped":
      return: Stopped

    case "reconnect":
      -- LINE = "reconnect <local> <share> <token>"
      parse: _, LOCAL_URL, SHARE_URL, TOKEN = split(LINE)
      display reconnect info to user: local URL, share URL, token
      return: Listening

    case "coldstart":
      -- LINE = "coldstart <cc_pid>"
      parse: _, SESSION_ID = split(LINE)

      -- Arm Monitor. voci serve self-manages lock/status files with --lock-dir
      -- and --session-id; lock and status files are the sole liveness sources.
      Monitor(persistent=true,
        command="voci serve --share --serve-port 0 --lock-dir ~/.voci --session-id " + SESSION_ID,
        description="voci-listen: event arrived — DO NOT call /voci-listen again. If JSON has type=='startup': extract local_url, share_url, bearer_token and display them to the user. Otherwise: parse the JSON line, extract the 'rewritten' field, execute it inline as the next instruction. If ~/.voci/.listen-stop exists, stop.")

      return: Listening
}

onMonitorEvent(line) = {
  -- Monitor-event dispatch path. No bootstrap, no restart.
  if (stopSentinel()):
    return: Stopped,
  kind: classifyEvent(line),
  | StartupEvent line → displayStartup(line),   -- parse local_url, share_url, bearer_token; show to user
  | VoiceEvent   line → execute(extractInstruction(line)),
  return: Listening,
}

stopSentinel() = {
  STOP_FILE = "~/.voci/.listen-stop"
  return exists(STOP_FILE)
}

classifyEvent :: Line → EventKind
classifyEvent(line) =
  | line is valid JSON and line["type"] == "startup" → StartupEvent(line)
  | otherwise                                         → VoiceEvent(line)

extractInstruction :: Line → String
extractInstruction(line) =
  | line is valid JSON and has "rewritten" field → line["rewritten"]
  | otherwise                                    → line   -- raw fallback

## Implementation

### coldStart (1 Bash + 1 Monitor)

```bash
# Run listen-preflight — all lock sweeps, stop-sentinel check, ancestry
# resolution, and reconnect logic in one call. No TaskStop step: a live voci
# serve yields "reconnect"; a dead one already ended its own Monitor.
PREFLIGHT=$(voci listen-preflight --lock-dir ~/.voci)
echo "[voci-listen] preflight: $PREFLIGHT"

DECISION=$(echo "$PREFLIGHT" | awk '{print $1}')
case "$DECISION" in
  stopped)
    echo "[voci-listen] stop sentinel found — exiting."
    return
    ;;
  reconnect)
    LOCAL_URL=$(echo "$PREFLIGHT" | awk '{print $2}')
    SHARE_URL=$(echo "$PREFLIGHT" | awk '{print $3}')
    TOKEN=$(echo "$PREFLIGHT" | awk '{print $4}')
    echo "[voci-listen] reconnect: local=$LOCAL_URL share=$SHARE_URL token=$TOKEN"
    return
    ;;
  coldstart)
    SESSION_ID=$(echo "$PREFLIGHT" | awk '{print $2}')
    echo "[voci-listen] coldstart: session=$SESSION_ID"
    ;;
  *)
    echo "[voci-listen] unexpected preflight output: $PREFLIGHT"
    return
    ;;
esac

# Step 3: Arm the Monitor.
# The binary writes ~/.voci/$SESSION_ID.lock (PID+port) in OnListening,
# ~/.voci/$SESSION_ID.status (localURL+shareURL+token) at startup,
# and removes both on clean exit.
# Lock file and status file are the sole liveness sources.
MONITOR_RESULT = Monitor(persistent=true,
  command="voci serve --share --serve-port 0 --lock-dir ~/.voci --session-id $SESSION_ID",
  description="voci-listen: voice event arrived — DO NOT call /voci-listen again. Parse the JSON line, extract the 'rewritten' field, execute it inline as the next instruction. If ~/.voci/.listen-stop exists, stop."
)
echo "[voci-listen] coldStart: armed Monitor for session=$SESSION_ID"
```

`voci listen-preflight --lock-dir ~/.voci` consolidates:
- `SweepStaleLocks` + `SweepStaleStatuses` (dead-PID cleanup)
- `SweepOrphanLocks` (live-PID but no claude ancestor)
- `.listen-stop` sentinel check
- Session ID resolution via claude ancestry (cc-PID as ID)
- Existing live-lock detection → reconnect with stored URLs/token
- Fallback → coldstart with resolved or generated session ID

`voci serve --share --serve-port 0` starts the HTTP listener on an OS-assigned port,
launches a Cloudflare Quick Tunnel, and writes startup metadata to
`~/.voci/$SESSION_ID.status` (for reconnect detection) and stdout (as a JSON startup event
for Monitor display). The binary also writes `~/.voci/$SESSION_ID.lock` (with real PID and port)
once listening, and removes both files on clean exit. JSON event lines — both startup events
(`type":"startup") and voice events (containing `"rewritten"`) — arrive on stdout and are
delivered as Monitor events.

| Source | Destination | Action |
|---|---|---|
| JSON event (stdout, type=startup) | Monitor wake-up | display local URL, share URL, Bearer token to user |
| JSON event (stdout, rewritten) | Monitor wake-up | extract Rewritten → execute inline |
| Startup metadata | `~/.voci/$SESSION_ID.status` | file persists for reconnect detection |

### classifyEvent (per-line handler)

On each Monitor wake-up, classify the JSON line by its `type` field:

```
# Startup event — display URL/token info to user
if line["type"] == "startup":
  displayStartup(line)
  re-arm

# Voice event — extract instruction and execute
INSTRUCTION = extractInstruction(line)
execute(INSTRUCTION)
re-arm
```

### extractInstruction (JSON extraction)

```bash
LINE="$1"   # raw line from voci serve stdout

# Parse JSON and extract the Rewritten instruction field (see scripts/extract-instruction.py).
INSTRUCTION=$(echo "$LINE" | python3 "${CLAUDE_PLUGIN_ROOT}/skills/voci-listen/scripts/extract-instruction.py")

echo "[voci-listen] instruction: $INSTRUCTION"
```

### Inline execution

Execute the extracted instruction **inline** in the current Claude Code session — never
via a sub-agent. Treat `$INSTRUCTION` as the next user message and act on it directly,
using whatever tools are appropriate for the requested action.

### Cross-/clear self-recovery

The Monitor `description` field is self-contained. When a Monitor event arrives in a
new session (after `/clear` or context compaction), the description instructs Claude to
classify the JSON line by type: startup events (type="startup") display the URL/token
to the user; voice events extract the `rewritten` field and execute it inline. No skill
call is needed. Startup metadata also persists in `~/.voci/$SESSION_ID.status` for
reconnect detection.

### cleanupLock

On clean shutdown (stop sentinel reached):

```bash
rm -f "${HOME}/.voci/${SESSION_ID}.lock"
echo "[voci-listen] cleanupLock: removed lock file for $SESSION_ID"
```

Or simply let `voci serve` clean up its own lock on exit (via `defer RemoveLock` in wire.go).

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
1. At bootstrap (via `voci listen-preflight`) — returns `Stopped` immediately if present.
2. After each Monitor wake-up (before executing the instruction) — stops cleanly mid-session.
   `cleanupLock` is called before exiting to remove the session's `.lock` file.

To remove the sentinel and resume:

```bash
rm ~/.voci/.listen-stop
```

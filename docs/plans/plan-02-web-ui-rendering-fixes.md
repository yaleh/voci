# Plan: Web UI Rendering Fixes

**Status:** Ready  
**Date:** 2026-07-01  
**Proposal:** `docs/proposals/proposal-web-ui-rendering-fixes.md`  
**Files touched:** `internal/context/session_source.go`, `internal/context/session_source_test.go`, `internal/daemon/web/recorder.js`

---

## Overview

Five rendering defects are fixed across two layers. Phase 1 repairs the Go context-building pipeline in `session_source.go` (three independent stages, parallelisable via worktrees). Phase 2 adds a lightweight Markdown renderer to `recorder.js` (single file, no new imports). All Phase 1 stages follow a strict TDD order: write the failing test first, then implement the fix, then confirm the test passes.

**Proposal fixes mapped to plan stages:**

| Proposal fix | Stage | Description |
|---|---|---|
| Fix 1 | 1.1 | Broad harness XML skip guard (two-byte check) |
| Fix 3 + Fix 5 | 1.2 | Rune-based truncation + ellipsis |
| Fix 4 | 1.3 | `stripCodeFences` helper in assistant branch |
| Fix 2 | 2.1 | `mdToHtml()` in `recorder.js` for cc bubbles |

---

## Phase 1 — Go backend fixes in `session_source.go`

All three stages touch `internal/context/session_source.go` and `internal/context/session_source_test.go`. They are independent of one another: each stage modifies a different code region and different test functions. They can be executed in parallel git worktrees, or sequentially on the same branch.

**Run the full Phase 1 test suite:**
```bash
go test ./internal/context/... -v -run TestParseSessionSnippet
```

**Run coverage check after all stages land:**
```bash
go test -coverprofile=/tmp/cover.out ./internal/context/... && go tool cover -func=/tmp/cover.out | grep context/session
```

---

### Stage 1.1 — Broad harness XML skip guard

**Objective:** Replace the two-prefix guard that only filters `<task-notification` and `<system-reminder` with a single two-byte check that future-proofs against any Claude Code harness XML opening tag (including `<local-command-caveat` and any tag added later). The guard must not filter natural-language strings that begin with `<` followed by a non-lowercase-letter character (`<3`, `<= 0`, `< `, `<T>`).

#### Dependencies

None. Stage 1.1 is self-contained.

#### TDD: Write the failing test first

Add to `internal/context/session_source_test.go` immediately after `TestParseSessionSnippet_SkipsSystemReminderPrefix` (currently ending at line 516):

```go
func TestParseSessionSnippet_SkipsLocalCommandCaveat(t *testing.T) {
    lines := []string{
        `{"type":"user","message":{"role":"user","content":"<local-command-caveat>\nsome tool warning about TASK-88\n</local-command-caveat>"}}`,
    }
    snippet := parseSessionSnippet(lines)
    if strings.Contains(snippet, "some tool warning") {
        t.Errorf("expected snippet to skip local-command-caveat content, but got: %q", snippet)
    }
    if strings.Contains(snippet, "TASK-88") {
        t.Errorf("expected TASK-88 not to leak from local-command-caveat content, but got: %q", snippet)
    }
}

func TestParseSessionSnippet_PassesThroughHeartEmoji(t *testing.T) {
    // "<3" begins with '<' but second char is '3' (digit), not a lowercase letter.
    // The broad XML guard must NOT filter it.
    lines := []string{
        `{"type":"user","message":{"role":"user","content":"<3 this tool"}}`,
    }
    snippet := parseSessionSnippet(lines)
    if !strings.Contains(snippet, "<3") {
        t.Errorf("expected '<3' to pass through XML guard, got: %q", snippet)
    }
}

func TestParseSessionSnippet_PassesThroughGenericSyntax(t *testing.T) {
    // "<T>" begins with '<' but second char is 'T' (uppercase), not lowercase.
    lines := []string{
        `{"type":"user","message":{"role":"user","content":"use func<T> pattern"}}`,
    }
    snippet := parseSessionSnippet(lines)
    if !strings.Contains(snippet, "func<T>") {
        t.Errorf("expected 'func<T>' to pass through XML guard, got: %q", snippet)
    }
}

func TestParseSessionSnippet_LowercaseAngleBracketIsFiltered(t *testing.T) {
    // "<enter>" begins with '<' followed by lowercase 'e' — the broad XML guard
    // WILL filter this message. This is a known limitation: messages whose first
    // word is <word> in lowercase (e.g. <enter>, <ctrl+c>, <em>) are filtered.
    // Document this as the expected behaviour, not a bug to fix.
    lines := []string{
        `{"type":"user","message":{"role":"user","content":"<enter> to confirm"}}`,
    }
    snippet := parseSessionSnippet(lines)
    if strings.Contains(snippet, "<enter>") {
        t.Errorf("expected '<enter>' to be filtered by XML guard (known limitation), got: %q", snippet)
    }
}
```

Confirm these tests fail with the current guard:
```bash
go test ./internal/context/... -run TestParseSessionSnippet_SkipsLocalCommandCaveat -v
```
Expected: `FAIL` (the content leaks through).

#### Implementation

**File:** `internal/context/session_source.go`  
**Location:** Lines 185–188

Current code:
```go
			// Skip task-notification and system-reminder injected user messages.
			if strings.HasPrefix(contentStr, "<task-notification") ||
				strings.HasPrefix(contentStr, "<system-reminder") {
				continue
			}
```

Replace with:
```go
			// Skip all harness-injected XML user turns. All Claude Code harness
			// opening tags begin with '<' followed by a lowercase letter (a–z).
			// Natural-language strings that start with '<' (e.g. "<3", "<= 0",
			// "< space", "<T>") have a second character that is a digit, operator,
			// space, or uppercase letter — they are passed through unchanged.
			if len(contentStr) >= 2 && contentStr[0] == '<' &&
				contentStr[1] >= 'a' && contentStr[1] <= 'z' {
				continue
			}
```

No import changes required.

> **Known Limitation:** Messages whose first word is `<word>` in lowercase — e.g. `<enter>`, `<ctrl+c>`, `<em>` — will also be filtered by this guard, because they satisfy `contentStr[0] == '<' && contentStr[1] >= 'a' && contentStr[1] <= 'z'`. This is acceptable for a voice-first UI where dictated text does not start with raw angle-bracket tags. A future improvement could require a matching `>` with only tag-name characters inside to distinguish XML tags from other uses. `TestParseSessionSnippet_LowercaseAngleBracketIsFiltered` documents this as the expected behaviour.

#### Verification

```bash
go test ./internal/context/... -run "TestParseSessionSnippet_Skips|TestParseSessionSnippet_PassesThrough|TestParseSessionSnippet_Mixed|TestParseSessionSnippet_LowercaseAngleBracket" -v
```

All of the following must pass:
- `TestParseSessionSnippet_SkipsLocalCommandCaveat` — NEW, must now pass
- `TestParseSessionSnippet_LowercaseAngleBracketIsFiltered` — NEW, asserts `<enter>` IS filtered (known limitation)
- `TestParseSessionSnippet_PassesThroughHeartEmoji` — NEW, must pass
- `TestParseSessionSnippet_PassesThroughGenericSyntax` — NEW, must pass
- `TestParseSessionSnippet_SkipsTaskNotificationPrefix` (line 495) — must still pass
- `TestParseSessionSnippet_SkipsSystemReminderPrefix` (line 508) — must still pass
- `TestParseSessionSnippet_MixedRealAndSystemTurns` (line 518) — must still pass

#### Acceptance criteria

| Assertion | Expected |
|---|---|
| `<local-command-caveat>` content does NOT appear in snippet | pass |
| `TASK-88` inside `<local-command-caveat>` does NOT leak into snippet | pass |
| `<enter>` IS filtered (known limitation, documented by `TestParseSessionSnippet_LowercaseAngleBracketIsFiltered`) | pass |
| `<3` appears in snippet (digit after `<`) | pass |
| `<T>` appears in snippet (uppercase after `<`) | pass |
| Existing `<task-notification` filter still works | pass |
| Existing `<system-reminder` filter still works | pass |
| `make test` green | pass |

---

### Stage 1.2 — Rune-based truncation and ellipsis

**Objective:** Change the per-turn truncation loop (lines 205–207) from byte-index slicing to rune-index slicing, preventing corruption of multi-byte UTF-8 characters (Chinese, emoji, CJK). Simultaneously change the outer accumulation cap (lines 258, 263) to count runes rather than bytes, so CJK-heavy sessions retain the intended ~6 turns rather than being cut to ~2. Append `…` (U+2026) when a turn is truncated so users see a clear omission indicator.

#### Dependencies

None. Stage 1.2 is self-contained.

#### TDD: Write the failing tests first

Add to `internal/context/session_source_test.go` after the existing `TestParseSessionSnippet_ProseCapped` (currently ending at line 454):

```go
func TestParseSessionSnippet_ChineseNotCorrupted(t *testing.T) {
    // 200 Chinese characters — each is 3 UTF-8 bytes.
    // Byte-based cap of 500 lands at byte 500 = rune 166, which is mid-character.
    // After fix, the cap is rune-based, so no replacement character appears.
    text := strings.Repeat("测", 200)
    lines := []string{
        `{"type":"user","message":{"role":"user","content":"` + text + `"}}`,
    }
    snippet := parseSessionSnippet(lines)
    const replacement = "\xef\xbf\xbd" // UTF-8 encoding of U+FFFD
    if strings.Contains(snippet, replacement) {
        t.Errorf("expected no replacement character in snippet, got corruption: %q", snippet[:min(len(snippet), 60)])
    }
}

func TestParseSessionSnippet_ChineseOuterCap(t *testing.T) {
    // 10 CJK turns of 400 runes each (400 runes × 3 bytes = 1200 bytes/turn).
    // Byte-based outer cap of 3000 bytes allows only 2 turns (2400 bytes).
    // Rune-based outer cap of 3000 runes allows 6+ turns (2400 runes < 3000).
    turn := strings.Repeat("字", 400)
    var lines []string
    for i := 0; i < 10; i++ {
        marker := ""
        if i == 9 {
            marker = "LATEST_CJK_TURN"
        }
        text := marker + turn
        lines = append(lines, `{"type":"user","message":{"role":"user","content":"`+text+`"}}`)
    }
    snippet := parseSessionSnippet(lines)
    count := strings.Count(snippet, "U: ")
    if count < 6 {
        t.Errorf("expected at least 6 CJK turns in output with rune-based cap, got %d", count)
    }
    if !strings.Contains(snippet, "LATEST_CJK_TURN") {
        t.Errorf("expected most recent CJK turn in output, got: %q", snippet[:min(len(snippet), 200)])
    }
}

```

Note: `min` is a Go built-in since Go 1.21. This project targets Go 1.23, so no helper function is needed; use `min(a, b)` directly.

Also extend the existing `TestParseSessionSnippet_ProseCapped` (line 407). Locate the final assertion block (starting at line 448 `// Recent turn present`) and add after line 453:

```go
    // Truncated turns must end with ellipsis
    for _, line := range strings.Split(block, "\n") {
        if strings.HasPrefix(line, "U: ") && len([]rune(line)) >= 500 {
            if !strings.HasSuffix(strings.TrimSpace(line), "…") {
                t.Errorf("expected truncated turn to end with ellipsis, got: %q", line[:min(len(line), 80)])
            }
        }
    }
```

Confirm failures before implementing:
```bash
go test ./internal/context/... -run "TestParseSessionSnippet_Chinese|TestParseSessionSnippet_ProseCapped" -v
```
- `TestParseSessionSnippet_ChineseNotCorrupted`: FAIL (replacement char present)
- `TestParseSessionSnippet_ChineseOuterCap`: FAIL (only 2 turns fit)
- `TestParseSessionSnippet_ProseCapped`: FAIL on ellipsis assertion

#### Implementation

**File:** `internal/context/session_source.go`

**Change 1 — Per-turn truncation loop (lines 204–210):**

Current (lines 205–207):
```go
	for i, t := range proseTurns {
		if len(t) > maxProseCharsPerTurn {
			proseTurns[i] = t[:maxProseCharsPerTurn]
		}
	}
	// maxProseCharsPerTurn = 500 means up to 497 chars of content after the 3-char prefix.
```

Replace with:
```go
	for i, t := range proseTurns {
		r := []rune(t)
		if len(r) > maxProseCharsPerTurn {
			r = r[:maxProseCharsPerTurn]
			proseTurns[i] = string(r) + "…"
		}
	}
	// maxProseCharsPerTurn = 500 means up to 500 runes (not bytes) including the 3-rune prefix.
```

**Change 2 — Outer accumulation cap (lines 258, 263):**

Current (lines 258–263):
```go
			if total+len(t) > maxProseCharsTotal {
				break
			}
			// ... accumulate turn ...
			total += len(t)
```
Replace with:
```go
			rc := []rune(t)
			if total+len(rc) > maxProseCharsTotal {
				break
			}
			// ... accumulate turn ...
			total += len(rc)
```

`rc` reuses the same `[]rune` allocation for both the guard check and the accumulator — no double conversion. No new imports required. `[]rune` is a built-in conversion.

#### Verification

```bash
go test ./internal/context/... -run "TestParseSessionSnippet_Chinese|TestParseSessionSnippet_ProseCapped" -v
```

All three tests must now pass.

Full regression:
```bash
go test ./internal/context/... -v
```

#### Acceptance criteria

| Assertion | Expected |
|---|---|
| `TestParseSessionSnippet_ChineseNotCorrupted`: no `\xef\xbf\xbd` in output | pass |
| `TestParseSessionSnippet_ChineseOuterCap`: ≥6 CJK turns in output | pass |
| `TestParseSessionSnippet_ProseCapped`: truncated turns end with `…` | pass |
| All pre-existing `TestParseSessionSnippet_*` tests | pass |
| `make test` green | pass |

---

### Stage 1.3 — `stripCodeFences` helper for assistant branch

**Objective:** Add a `stripCodeFences` helper that replaces each fenced code block (`` ``` … ``` ``) with its body content (fence markers stripped, body retained as plain text). Call it only in the assistant `"text"` content block branch, before `normalizeProse`, so that code identifiers survive into the ASR entity hint while triple-backtick syntax does not reach `normalizeProse`'s `strings.Fields` join. `normalizeProse` itself is not changed. User-turn processing is not changed.

#### Dependencies

None. Stage 1.3 is self-contained. `regexp` is already imported at line 8.

#### TDD: Write the failing tests first

Add to `internal/context/session_source_test.go` after `TestParseSessionSnippet_SystemTurnTaskIDNotExtracted` (currently ending at line 558):

```go
func TestStripCodeFences_RemovesFenceMarkersKeepsBody(t *testing.T) {
    input := "Here is the fix:\n```go\nfunc foo() {}\n```\nDone."
    got := normalizeProse(stripCodeFences(input))
    want := "Here is the fix: func foo() {} Done."
    if got != want {
        t.Errorf("stripCodeFences+normalizeProse:\n got:  %q\n want: %q", got, want)
    }
}

func TestStripCodeFences_NormalizeProse_LeavesMarkersIntact(t *testing.T) {
    // Confirm normalizeProse alone does NOT strip fence markers — the two functions
    // are independent and normalizeProse is intentionally unchanged.
    input := "Here:\n```go\nfunc f() {}\n```\nDone."
    got := normalizeProse(input)
    if !strings.Contains(got, "```go") {
        t.Errorf("normalizeProse alone should leave fence markers, got: %q", got)
    }
}

func TestStripCodeFences_DegenerateSingleLineFence(t *testing.T) {
    // A fence that has no newline inside (e.g. ```code```) degenerates gracefully.
    input := "before ```inline``` after"
    got := normalizeProse(stripCodeFences(input))
    if !strings.Contains(got, "before") || !strings.Contains(got, "after") {
        t.Errorf("degenerate fence should not drop surrounding text, got: %q", got)
    }
}

func TestParseSessionSnippet_AssistantCodeBlockBodyRetained(t *testing.T) {
    // An assistant turn with a fenced code block: the identifier "myFuncName"
    // must survive into the snippet (body retained), but the fence markers
    // "```go" must not appear.
    lines := []string{
        `{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"Here:\n` + "```" + `go\nfunc myFuncName() {}\n` + "```" + `\nDone."}]}}`,
    }
    snippet := parseSessionSnippet(lines)
    if !strings.Contains(snippet, "myFuncName") {
        t.Errorf("expected code body identifier 'myFuncName' in snippet, got: %q", snippet)
    }
    if strings.Contains(snippet, "```go") {
        t.Errorf("expected fence marker '```go' NOT in snippet, got: %q", snippet)
    }
}

func TestParseSessionSnippet_UserTurnFenceMarkersUnchanged(t *testing.T) {
    // stripCodeFences must NOT apply to user turns. A user message containing
    // backticks must flow through normalizeProse unchanged (other than whitespace
    // collapsing). This test verifies no regression in user-turn processing.
    lines := []string{
        `{"type":"user","message":{"role":"user","content":"run ` + "`" + `go test ./...` + "`" + ` please"}}`,
    }
    snippet := parseSessionSnippet(lines)
    if !strings.Contains(snippet, "go test ./...") {
        t.Errorf("expected user inline code reference in snippet, got: %q", snippet)
    }
}
```

Confirm failures:
```bash
go test ./internal/context/... -run "TestStripCodeFences|TestParseSessionSnippet_AssistantCode|TestParseSessionSnippet_UserTurnFence" -v
```
Expected: compile error (undefined: `stripCodeFences`) or FAIL for integration tests.

#### Implementation

**File:** `internal/context/session_source.go`

**Change 1 — Add package-level regex var after line 121:**

Current line 121:
```go
var taskIDPattern = regexp.MustCompile(`TASK-\d+`)
```

Insert immediately after:
```go
// fencedCodeBlock matches a triple-backtick fenced code block spanning one or more lines.
// [\s\S]*? is non-greedy and matches any character including newlines without regex flags.
var fencedCodeBlock = regexp.MustCompile("```[\\s\\S]*?```")
```

**Change 2 — Add `stripCodeFences` function after `normalizeProse` (after line 275):**

Current `normalizeProse` ends at line 275:
```go
func normalizeProse(s string) string {
    s = strings.Join(strings.Fields(s), " ")
    return strings.TrimSpace(s)
}
```

Insert immediately after (at line 276):
```go
// stripCodeFences replaces each fenced code block (```…```) with its body content,
// stripping the opening fence line (```lang) and closing fence (```) while keeping
// identifiers, function names, and other tokens that the ASR hint benefits from.
// Call this only in the assistant branch; user-turn processing must not use it.
func stripCodeFences(s string) string {
	return fencedCodeBlock.ReplaceAllStringFunc(s, func(block string) string {
		// Split on first newline to separate the opening fence line (```lang) from the body.
		parts := strings.SplitN(block, "\n", 2)
		if len(parts) < 2 {
			return " " // degenerate single-line fence — remove entirely
		}
		body := parts[1]
		// Remove the closing fence (last occurrence of ```) from the body.
		if idx := strings.LastIndex(body, "```"); idx >= 0 {
			body = body[:idx]
		}
		return " " + strings.TrimSpace(body) + " "
	})
}
```

**Change 3 — Call `stripCodeFences` in assistant "text" branch (lines 175–178):**

Current (lines 175–178):
```go
				case "text":
					if t := normalizeProse(block.Text); t != "" {
						proseTurns = append(proseTurns, "A: "+t)
					}
```

Replace with:
```go
				case "text":
					if t := normalizeProse(stripCodeFences(block.Text)); t != "" {
						proseTurns = append(proseTurns, "A: "+t)
					}
```

No import changes required.

#### Verification

```bash
go test ./internal/context/... -run "TestStripCodeFences|TestParseSessionSnippet_AssistantCode|TestParseSessionSnippet_UserTurnFence" -v
```

Full regression:
```bash
go test ./internal/context/... -v
```

#### Acceptance criteria

| Assertion | Expected |
|---|---|
| `TestStripCodeFences_RemovesFenceMarkersKeepsBody`: output is `"Here is the fix: func foo() {} Done."` | pass |
| `TestStripCodeFences_NormalizeProse_LeavesMarkersIntact`: `normalizeProse` alone leaves `\`\`\`go` in output | pass |
| `TestStripCodeFences_DegenerateSingleLineFence`: surrounding text preserved | pass |
| `TestParseSessionSnippet_AssistantCodeBlockBodyRetained`: `myFuncName` in snippet, no `` ```go `` | pass |
| `TestParseSessionSnippet_UserTurnFenceMarkersUnchanged`: inline backtick reference in snippet | pass |
| All pre-existing tests | pass |
| `make test` green | pass |

---

## Phase 2 — JS frontend fix in `recorder.js`

Phase 2 is a single-file change to `internal/daemon/web/recorder.js`. It has no dependency on Phase 1 (the `msg.text` strings reaching `mdToHtml` already have their newlines collapsed by `normalizeProse` on the Go side; once Stage 1.3 lands, no triple-backtick fence markers will arrive either). Phase 2 can be developed and verified independently of Phase 1 status.

---

### Stage 2.1 — Add `mdToHtml()` and wire into cc bubbles

**Objective:** Add a `mdToHtml(text)` function that HTML-escapes the input first (via `esc()`), then applies three inline Markdown transforms: inline code (`` `…` `` → `<code>`), bold (`**…**` → `<strong>`), and italic (`*…*` → `<em>`). Apply it to assistant ("cc") bubble content in `renderDialogue()`. Continue using raw `esc()` for user ("you") bubble content. No new `<script>` tags or CDN dependencies.

**Why no `\n → <br>` in `mdToHtml`:** By the time `msg.text` reaches `mdToHtml`, `normalizeProse` on the Go side has already collapsed all whitespace (including `\n`) to single spaces via `strings.Fields`. There are no literal newline characters in assistant turn text when it arrives at the JS layer.

**Why no `` ``` `` → `<pre>` in `mdToHtml`:** Stage 1.3 (`stripCodeFences`) removes triple-backtick blocks from assistant turns in the Go pipeline before they reach the hint. By the time `msg.text` arrives, no fenced block syntax survives.

**Security:** `mdToHtml` calls `esc()` first, entity-encoding all `&`, `<`, `>` characters before any regex pattern substitution runs. The only raw HTML tags introduced are `<code>`, `<strong>`, and `<em>` — all from the regex replacement strings, not from user-supplied content.

#### Dependencies

Stage 2.1 has no Go dependencies. It does benefit from Stage 1.3 (fence markers stripped before reaching JS), but it is independently testable even without Stage 1.3.

#### TDD: Specify the manual browser verification checklist

There is no Go test framework for `recorder.js`. Verification is done via two methods:

**Method A — Inline unit test in browser console:**

After applying the change, open `voci serve` in a browser, open DevTools Console, and paste:

```js
// Quick inline sanity check for mdToHtml
var cases = [
  { in: '**bold** text',          want: '<strong>bold</strong> text' },
  { in: '`code` here',            want: '<code' },       // check tag opens
  { in: '*italic*',               want: '<em>italic</em>' },
  { in: '<script>alert(1)</script>', want: '&lt;script&gt;' }, // XSS check
  { in: 'plain text',             want: 'plain text' },
];
cases.forEach(function(c) {
  var got = mdToHtml(c.in);
  if (!got.includes(c.want)) console.error('FAIL', c.in, '->', got, 'want', c.want);
  else console.log('PASS', c.in);
});
```

All five cases must log `PASS`.

**Method B — Visual inspection of `#voci-dialogue`:**

1. Start `voci serve`.
2. Open the web UI, send any message, observe that the server echoes an assistant response containing `**bold**` or `` `code` ``.
3. The `#voci-dialogue` feed must render the assistant bubble with visible bold / monospace styling, not literal `**` or backtick characters.

**Method C — MCP browser validation (browser acceptance criterion):**

After implementation, use the MCP browser to navigate to the voci web UI, wait for the dialogue to populate, and assert:
1. At least one `<strong>` element exists inside `#voci-dialogue` (bold rendering)
2. At least one `<code>` element exists inside `#voci-dialogue` (inline code rendering)
3. No raw `**` double-asterisk appears as text content inside `#voci-dialogue`

If the current dialogue has no bold/code markdown, type a test message containing `**bold** and \`code\`` into the compose box, send it, wait for the assistant response to appear, then verify the three assertions above.

This is the **browser-validation acceptance criterion** for Stage 2.1.

#### Implementation

**File:** `internal/daemon/web/recorder.js`

**Change 1 — Add `mdToHtml` function after `esc()` (after line 181):**

Current `esc()` at lines 178–181:
```js
  function esc(s) {
    return String(s)
      .replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;');
  }
```

Insert immediately after (between line 181 and the `// ── Context ──` comment at line 183):
```js
  function mdToHtml(text) {
    // 1. HTML-escape first so injected < > & are neutralised before regex runs.
    var s = esc(text);
    // 2. Inline code: `…` → <code>
    s = s.replace(/`([^`]+)`/g,
      '<code style="font-family:JetBrains Mono,monospace;font-size:11px;' +
      'color:#7ab0e0;background:#0e1422;padding:0 3px;border-radius:3px">$1</code>');
    // 3. Bold: **…** → <strong>
    s = s.replace(/\*\*([^*]+)\*\*/g, '<strong>$1</strong>');
    // 4. Italic: *…* → <em>
    s = s.replace(/\*([^*]+)\*/g, '<em>$1</em>');
    return s;
  }
```

**Change 2 — Use `mdToHtml` in the cc (assistant) bubble (line 280):**

Current line 280:
```js
          '<span style="font-size:12.5px;color:#e4eaf5;line-height:1.5">' + esc(msg.text) + '</span>'
```

Replace `esc(msg.text)` with `mdToHtml(msg.text)`:
```js
          '<span style="font-size:12.5px;color:#e4eaf5;line-height:1.5">' + mdToHtml(msg.text) + '</span>'
```

**Line 267 (user bubble) — no change:**
```js
            '<span style="font-size:12.5px;color:#a8bedc;line-height:1.5">' + esc(msg.text) + '</span>'
```
User bubble continues to use `esc()`. User messages are natural language / voice transcriptions that do not contain Markdown.

No changes to `index.html`. No new `<script>` tags. `mdToHtml` is defined inside the IIFE and not exposed on `window`.

#### Verification

**Go test suite (confirm no backend regression):**
```bash
make test
```

**E2E suite:**
```bash
make e2e
```

**Manual browser console check** (see Method A above).

**Visual inspection** (see Method B above).

**MCP browser validation** (see Method C above — browser acceptance criterion).

#### Acceptance criteria

| Assertion | Expected |
|---|---|
| `mdToHtml('**bold**')` returns `<strong>bold</strong>` | pass |
| `mdToHtml('\`code\`')` returns string containing `<code` tag | pass |
| `mdToHtml('*italic*')` returns `<em>italic</em>` | pass |
| `mdToHtml('<script>alert(1)</script>')` returns string containing `&lt;script&gt;` | pass |
| `mdToHtml('plain')` returns `'plain'` | pass |
| User "you" bubbles still use `esc()`, no Markdown rendering | pass |
| `make test` green | pass |
| `make e2e` green | pass |
| MCP browser: `<strong>` element exists inside `#voci-dialogue` | pass |
| MCP browser: `<code>` element exists inside `#voci-dialogue` | pass |
| MCP browser: no raw `**` text content inside `#voci-dialogue` | pass |

---

## Execution order

### Parallel path (recommended — 3 worktrees for Phase 1, then Phase 2)

```
worktree-1.1 → Stage 1.1 (XML guard)    ─┐
worktree-1.2 → Stage 1.2 (rune + …)     ─┤→ merge all three → Phase 2 on main
worktree-1.3 → Stage 1.3 (stripFences)  ─┘
```

Each Phase 1 stage can be entered with:
```bash
git worktree add .claude/worktrees/fix-1.1 -b fix/session-xml-guard
git worktree add .claude/worktrees/fix-1.2 -b fix/session-rune-truncation
git worktree add .claude/worktrees/fix-1.3 -b fix/session-strip-fences
```

### Sequential path (simpler)

Apply stages in order 1.1 → 1.2 → 1.3 → 2.1 on a single branch. Each stage is independently testable.

---

## Definition of Done

- [ ] Stage 1.1: `TestParseSessionSnippet_SkipsLocalCommandCaveat` passes (with TASK-88 leak assertion); `TestParseSessionSnippet_LowercaseAngleBracketIsFiltered` passes (documents `<enter>` filtered as known limitation); `<3` and `<T>` pass through; all existing tests pass
- [ ] Stage 1.2: `TestParseSessionSnippet_ChineseNotCorrupted` and `TestParseSessionSnippet_ChineseOuterCap` pass; `TestParseSessionSnippet_ProseCapped` passes with ellipsis assertion
- [ ] Stage 1.3: all five new `TestStripCodeFences_*` / `TestParseSessionSnippet_AssistantCode*` / `TestParseSessionSnippet_UserTurnFence*` tests pass
- [ ] `make test` passes (overall ≥80% coverage maintained)
- [ ] Stage 2.1: `mdToHtml` browser console sanity check passes all five cases; cc bubbles render bold/italic/code visually; MCP browser confirms `<strong>` and `<code>` elements inside `#voci-dialogue` with no raw `**` text
- [ ] `make e2e` passes

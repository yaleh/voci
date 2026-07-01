# Proposal: Web UI Rendering Fixes

**Status:** Draft  
**Date:** 2026-07-01  
**Files touched:** `internal/daemon/web/recorder.js`, `internal/context/session_source.go`

---

## Problem Statement

Five rendering defects degrade the voci web UI (served by `voci serve`). Four originate in the Go session-context pipeline (`internal/context/session_source.go`) and surface as garbled text in the `## Recent Dialogue` section of the ASR hint that `renderContext()` in `recorder.js` parses to populate the `#voci-dialogue` feed. One defect is purely in the JavaScript renderer.

### Issue 1 (High) — `<local-command-caveat>` leaks into "you" bubbles

`parseSessionSnippet` in `session_source.go` at lines 186–188 skips harness-injected user turns whose content starts with `<task-notification` or `<system-reminder`:

```go
if strings.HasPrefix(contentStr, "<task-notification") ||
    strings.HasPrefix(contentStr, "<system-reminder") {
    continue
}
```

Claude Code injects additional harness XML tags as user turns, including `<local-command-caveat`. These pass through the guard, get normalised by `normalizeProse`, and appear in the `## Recent Dialogue` block as `U: <local-command-caveat ...`. `renderContext()` in `recorder.js` (line 246) emits them as `{ role: 'user', text: ... }` messages, which `renderDialogue()` (line 263–268) renders as "you" bubbles containing raw XML.

### Issue 2 (High) — Markdown not rendered in dialogue bubbles

`renderDialogue()` in `recorder.js` (lines 263–281) uses `esc(msg.text)` for both user and assistant bubble content — the `esc()` function defined at lines 178–181, which HTML-escapes `&`, `<`, and `>`. Assistant turns from the Claude Code session JSONL contain real Markdown (`**bold**`, `` `code` ``, `\n` line breaks), which are displayed as literal punctuation instead of formatted text.

### Issue 3 (Medium) — Byte-based truncation corrupts UTF-8 / Chinese characters

`parseSessionSnippet` at lines 205–207 truncates each prose turn by byte index:

```go
if len(t) > maxProseCharsPerTurn {
    proseTurns[i] = t[:maxProseCharsPerTurn]
}
```

`len(t)` measures bytes in Go, not rune count. Chinese characters are 3 UTF-8 bytes each. Slicing `t[:500]` can land mid-codepoint, producing a malformed string. When this string is served via `GET /api/context` and rendered in `renderDialogue()`, browsers display the replacement character `�` (shown as `?`) inside "you" and "cc" bubbles.

### Issue 4 (Medium) — `normalizeProse` collapses code blocks into unreadable inline text

`normalizeProse` at lines 272–274 uses `strings.Fields(s)` to tokenise, then joins tokens with single spaces:

```go
func normalizeProse(s string) string {
    s = strings.Join(strings.Fields(s), " ")
    return strings.TrimSpace(s)
}
```

`strings.Fields` splits on all Unicode whitespace including `\n`. An assistant response containing a fenced code block — e.g.:

```
Here is the fix:
```go
func foo() {}
```
```

becomes the single inline string `Here is the fix: ```go func foo() {} ``` ` — losing all structure. This also corrupts the `## Recent Dialogue` section of the ASR hint, degrading hint quality.

### Issue 5 (Low) — No ellipsis when truncation occurs

The truncation at `session_source.go` line 207 (`proseTurns[i] = t[:maxProseCharsPerTurn]`) silently cuts the string mid-sentence. The dialogue feed shows text that ends abruptly with no visual indicator that content was omitted.

---

## Goals

1. Prevent all harness-injected XML user turns from appearing in the dialogue hint and browser UI — covering `<task-notification`, `<system-reminder`, `<local-command-caveat`, and any future harness wrappers.
2. Render assistant Markdown (newlines, inline code, bold, italic) correctly in the `#voci-dialogue` bubble feed.
3. Truncate prose turns by rune count rather than byte count to preserve UTF-8 correctness.
4. Produce readable prose for the ASR hint by stripping fenced code blocks before normalisation instead of collapsing them into inline garbage.
5. Append `…` to truncated turns so users see a clear omission indicator.
6. All fixes must be backward-compatible with existing tests in `session_source_test.go` and must not break `make test` or `make e2e`.

---

## Non-Goals

- Full CommonMark Markdown rendering (tables, footnotes, images, nested lists). Only the subset that appears in Claude Code assistant responses matters: inline code, bold, italic, line breaks.
- CDN-loaded Markdown libraries. The fix must be self-contained in `recorder.js` with no new `<script>` tags in `index.html`.
- Changing the `normalizeProse` behaviour for user turns — only assistant turns contain code blocks worth stripping.
- Altering the ASR hint format served by `GET /api/context`. The `## Recent Dialogue` section format (`A: …`, `U: …`) is consumed by `renderContext()` in `recorder.js` at line 244–247 and must remain stable.

---

## Design

### Fix 1 — Broad harness XML skip guard (`session_source.go`)

**Location:** `parseSessionSnippet`, lines 185–188 in `session_source.go`.

**Change:** Replace the two-prefix check with a precise two-byte check: skip the turn if `contentStr[0] == '<'` and `contentStr[1]` is a lowercase letter (`a`–`z`). All Claude Code harness XML opening tags begin with `<` followed by a lowercase letter. Closing tags (`</tag>`) are not guarded against because they never appear as the first token of a user message. Real user messages that start with `<` but are not XML — e.g., `<3`, `<= 0`, `< ` (comparison with a space), or Go generic syntax like `<T>` — are correctly passed through because their second character is a digit, `=`, space, or uppercase letter respectively.

```go
// Before (lines 186–188):
if strings.HasPrefix(contentStr, "<task-notification") ||
    strings.HasPrefix(contentStr, "<system-reminder") {
    continue
}

// After:
if len(contentStr) >= 2 && contentStr[0] == '<' &&
    contentStr[1] >= 'a' && contentStr[1] <= 'z' {
    continue
}
```

**Rationale:** The existing two-prefix guard (introduced for TASK-70, tested at `session_source_test.go` lines 495–516) already encodes the intent of "skip harness XML". The precise guard matches `<` followed by a lowercase letter (`a`–`z`), which covers all harness XML opening tags (e.g., `<task-notification`, `<system-reminder`, `<local-command-caveat`) while NOT filtering natural-language strings like `<3`, `<= 0`, `< ` (with a space), or Go generic syntax like `<T>`. Closing tags (`</tag>`) are excluded from the guard entirely because harness closing tags never appear as the first token of a user message — only opening tags do. All existing tests continue to pass because the covered prefixes are a subset. The new test to add covers a `<local-command-caveat` fixture.

**Known Limitation:** Messages whose first word is `<word>` in lowercase — e.g. `<enter>`, `<ctrl+c>`, `<em>` — will also be filtered by this guard. This is acceptable for a voice-first UI where dictated text does not start with raw angle-bracket tags. A future improvement could require a matching `>` with only tag-name characters inside to distinguish XML tags from other uses. `TestParseSessionSnippet_LowercaseAngleBracketIsFiltered` documents this as the expected behaviour, not a bug to fix.

**Tests to add:** Two new test functions in `session_source_test.go`: `TestParseSessionSnippet_SkipsLocalCommandCaveat` (following the pattern of `TestParseSessionSnippet_SkipsTaskNotificationPrefix` at line 495, with `TASK-88` embedded in the fixture to assert no TASK-ID leaks) and `TestParseSessionSnippet_LowercaseAngleBracketIsFiltered` (asserting that `<enter>` IS filtered — documenting this as the known limitation, not a bug to fix).

---

### Fix 2 — Lightweight Markdown renderer (`recorder.js`)

**Location:** `renderDialogue()` at lines 254–287 in `recorder.js`.

**Change:** Add a `mdToHtml(text)` helper function immediately after the `esc()` definition (after line 181). Apply `mdToHtml` to assistant message content instead of `esc()`. Continue using `esc()` for all label strings (role names, timestamps, IDs).

```js
// Insert after line 181 (after the esc() function):
function mdToHtml(text) {
  // 1. Escape HTML special chars first to neutralise any raw < > &
  var s = esc(text);
  // 2. Inline code `…` → <code>
  s = s.replace(/`([^`]+)`/g, '<code style="font-family:JetBrains Mono,monospace;font-size:11px;color:#7ab0e0;background:#0e1422;padding:0 3px;border-radius:3px">$1</code>');
  // 3. **bold**
  s = s.replace(/\*\*([^*]+)\*\*/g, '<strong>$1</strong>');
  // 4. *italic*
  s = s.replace(/\*([^*]+)\*/g, '<em>$1</em>');
  return s;
}
```

Note: `\n → <br>` conversion is intentionally absent. `normalizeProse` in Go collapses ALL whitespace (including newlines) via `strings.Fields` before messages reach the JS layer. By the time `msg.text` arrives in `mdToHtml`, there are no `\n` characters left in assistant messages — line-break handling is a Go-layer concern, not a JS-layer concern. Similarly, fenced code-block detection (`` ``` `` → `<pre>`) is omitted: Fix 4 calls `stripCodeFences` before `normalizeProse` in the assistant branch, so no triple-backtick block survives to `msg.text`. The function therefore only handles the three inline constructs that do survive normalisation: inline code, bold, and italic.

**Usage change in `renderDialogue()`:** In the assistant branch (lines 277–281), replace `esc(msg.text)` with `mdToHtml(msg.text)`:

```js
// Before (line 280):
'<span style="font-size:12.5px;color:#e4eaf5;line-height:1.5">' + esc(msg.text) + '</span>'

// After:
'<span style="font-size:12.5px;color:#e4eaf5;line-height:1.5">' + mdToHtml(msg.text) + '</span>'
```

User bubbles (line 267) continue using `esc(msg.text)` — user input does not contain Markdown.

**Security:** `mdToHtml` calls `esc()` first before any pattern substitution, so injected `<script>` or `<img>` in the source text are entity-escaped before regex transforms run. The only HTML tags introduced are whitelisted inline elements (`<strong>`, `<em>`, `<code>`).

**Tests:** The Playwright E2E suite in `e2e/` already covers the dialogue feed. A new test case can assert that an assistant message containing `**bold**` renders a `<strong>` element in `#voci-dialogue`.

---

### Fix 3 — Rune-aware truncation (`session_source.go`)

**Location:** `parseSessionSnippet`, lines 204–209 in `session_source.go`.

**Change:** Replace the byte-slice cap with a rune-slice cap using a `[]rune` cast, which requires no additional import.

```go
// Before (lines 205–207):
for i, t := range proseTurns {
    if len(t) > maxProseCharsPerTurn {
        proseTurns[i] = t[:maxProseCharsPerTurn]
    }
}

// After:
for i, t := range proseTurns {
    r := []rune(t)
    if len(r) > maxProseCharsPerTurn {
        r = r[:maxProseCharsPerTurn]
        proseTurns[i] = string(r)
    }
}
```

`[]rune(t)` decodes the UTF-8 string to a slice of Unicode code points. Slicing by rune index is always safe at codepoint boundaries. The `maxProseCharsPerTurn` constant (500, declared at line 116) is reused without change — it now means 500 runes rather than 500 bytes, which is the intended semantics.

The comment at line 210 (`// maxProseCharsPerTurn = 500 means up to 497 chars of content after the 3-char prefix.`) should be updated to say "runes" instead of "chars".

**Outer cap (lines 256–264):** The per-turn fix alone is not sufficient. The outer accumulation cap at lines 256–264 uses `total+len(t) > maxProseCharsTotal` where `len(t)` is byte-based. After Fix 3, a CJK turn of 500 runes ≈ 1500 bytes. With `maxProseCharsTotal = 3000` bytes, only 2 CJK turns would fit instead of the intended 6. This check must also be converted to rune-based: replace `total+len(t) > maxProseCharsTotal` with `total+len([]rune(t)) > maxProseCharsTotal`, and accumulate `len([]rune(t))` into `total` instead of `len(t)`. After this change `maxProseCharsTotal` means runes, not bytes, consistent with `maxProseCharsPerTurn`. This idiom is consistent with the per-turn cap (`[]rune` cast) and requires no additional import.

**Tests:** The existing `TestParseSessionSnippet_ProseCapped` test at line 407 uses ASCII-only `strings.Repeat("X", 650)`, so it passes in both the before and after state. A new test `TestParseSessionSnippet_ChineseNotCorrupted` should feed a user turn containing 200 Chinese characters (`strings.Repeat("测", 200)`) and assert the snippet contains no `�` (`\xef\xbf\xbd` in UTF-8). A second new test `TestParseSessionSnippet_ChineseOuterCap` should feed 10 CJK turns of 400 runes each and assert that at least 6 turns appear in the output (verifying the outer cap is rune-based, not byte-based).

---

### Fix 4 — Strip code fences before normalisation (`session_source.go`)

**Location:** Assistant `"text"` content block branch in `parseSessionSnippet` (before calling `normalizeProse`). `normalizeProse` itself remains UNCHANGED.

**Change:** Introduce a `stripCodeFences` helper and apply it only in the assistant `"text"` branch, before `normalizeProse` is called. This ensures user turns are never affected (satisfying Non-Goal #3). `normalizeProse` continues to do only `strings.Fields` + `strings.TrimSpace` with no fence logic inside it.

```go
// fencedCodeBlock matches ```…``` spanning multiple lines (non-greedy).
// [\s\S] already matches any character including newlines, so no flags are needed.
var fencedCodeBlock = regexp.MustCompile("```[\\s\\S]*?```")

func stripCodeFences(s string) string {
    return fencedCodeBlock.ReplaceAllStringFunc(s, func(block string) string {
        // Trim the opening fence (first line: ```lang\n) and closing fence (last ```)
        lines := strings.SplitN(block, "\n", 2)
        if len(lines) < 2 {
            return " " // degenerate single-line fence — just remove it
        }
        body := lines[1]
        if idx := strings.LastIndex(body, "```"); idx >= 0 {
            body = body[:idx]
        }
        return " " + strings.TrimSpace(body) + " "
    })
}
```

In the assistant `"text"` content block branch:

```go
// In the assistant "text" branch only:
case "text":
    text := stripCodeFences(block.Text)
    if t := normalizeProse(text); t != "" {
        proseTurns = append(proseTurns, "A: "+t)
    }
```

`normalizeProse` itself remains unchanged — it still only does `strings.Fields` + `strings.TrimSpace`. The `regexp.MustCompile` call should be a package-level `var` (same pattern as `taskIDPattern` at line 121) to avoid recompiling on every call. `[\s\S]*?` already matches any character including newlines, so no regex flags are needed.

**Behaviour for assistant turns with code blocks:** `` Here is the fix:\n```go\nfunc foo() {}\n```\nDone. `` normalises to `Here is the fix: func foo() {} Done.` — the identifiers are preserved for ASR entity injection, the fence markers are gone.

**Rationale for keeping body content:** The code block body often contains function names, CLI flag strings, package paths, and other identifiers that are exactly the kind of tokens the ASR entity-injection pipeline benefits from. Discarding the body entirely (the originally proposed approach) would throw away useful ASR signal. Stripping only the fence markers is strictly less lossy.

**No effect on user turns:** `stripCodeFences` is called only in the assistant branch, before `normalizeProse`. User turn processing calls `normalizeProse` directly, without `stripCodeFences`. `normalizeProse` itself is unchanged.

**Tests:** Add `TestNormalizeProse_StripsCodeFenceMarkers` asserting that input `` "Here:\n```go\nfunc f() {}\n```\nDone." `` produces `"Here: func f() {} Done."` (body retained, fence markers stripped) when passed through `stripCodeFences` then `normalizeProse`. Also assert that `normalizeProse` alone (without `stripCodeFences`) leaves fence markers intact, confirming the two functions are independent.

---

### Fix 5 — Ellipsis on truncation (`session_source.go`)

**Location:** `parseSessionSnippet`, the truncation loop (same location as Fix 3, lines 205–207).

**Change:** Append `…` (U+2026, a single rune) when a turn is truncated. Combined with Fix 3:

```go
for i, t := range proseTurns {
    r := []rune(t)
    if len(r) > maxProseCharsPerTurn {
        r = r[:maxProseCharsPerTurn]
        proseTurns[i] = string(r) + "…"
    }
}
```

The appended `…` is a single Unicode character added after the safe rune boundary, so it cannot corrupt UTF-8. The total length of a truncated turn is now `maxProseCharsPerTurn` runes + 3 bytes for `…` (U+2026 is 3 UTF-8 bytes), well within any reasonable display budget.

**Tests:** Extend `TestParseSessionSnippet_ProseCapped` to assert that truncated turns in the output contain `…`.

---

## Alternatives

### Alt 1 — CDN Markdown library (marked.js / micromark) for Fix 2

Using a battle-tested library would give full CommonMark support. Rejected because it requires a new `<script>` tag in `index.html`, introduces a CDN dependency that breaks offline operation, and the voci web UI is designed to be served by `voci serve` in airgapped or low-connectivity environments. The `~15-line` inline `mdToHtml` function covers the only Markdown constructs that actually appear in Claude Code assistant turns.

### Alt 2 — Line-preserving normalisation for Fix 4 (instead of stripping fence markers)

An alternative to stripping fence markers is to replace `strings.Fields` with a line-by-line approach that collapses intra-line whitespace but preserves newlines. This would produce more readable output for complex multi-paragraph prose. Rejected because:
- The `## Recent Dialogue` section is consumed as `A: …` / `U: …` prefixed single-line strings by `renderContext()` in `recorder.js` at line 244. Multi-line prose in a single `A:` token is already unusual.
- For the ASR hint purpose, compact single-line prose is sufficient and reduces hint token count.
- Stripping fence markers only (keeping body content) is the simpler, less-lossy, more testable change.

### Alt 2b — Discard code block body entirely for Fix 4

An earlier version of Fix 4 proposed stripping both fence markers and body content, replacing the entire fenced block with a single space. Rejected because code block bodies contain function names, CLI flag strings, and package paths that are exactly the tokens the ASR entity-injection pipeline benefits from. The current design (strip markers only, keep body) is strictly less lossy with no added implementation complexity.

### Alt 3 — Skip only known XML tags (enumerated list) for Fix 1

An alternative to the precise two-byte guard is to extend the enumerated list with `<local-command-caveat`. Rejected because the set of harness XML tag names is not stable — Claude Code can introduce new injection types and the guard would rot again. The precise `contentStr[0]=='<' && contentStr[1] in 'a'–'z'` guard is future-proof: it matches all harness XML opening tags while correctly passing through strings like `<3`, `<= 0`, `</3` (broken-heart emoticon), or `<T>` that are valid in natural-language or code contexts. Closing tags (`</tag>`) are excluded by design because they never appear as the first token of a user message. The guard matches the actual invariant — real user messages are natural language, not XML element syntax.

**Known Limitation:** Messages whose first word is `<word>` in lowercase — e.g. `<enter>`, `<ctrl+c>`, `<em>` — will also be filtered by this guard, because they satisfy `contentStr[0] == '<' && contentStr[1] >= 'a' && contentStr[1] <= 'z'`. This is acceptable for a voice-first UI where dictated text does not start with raw angle-bracket tags. A future improvement could require a matching `>` with only tag-name characters inside (e.g. `[a-z][a-z0-9-]*`) to distinguish XML tags from other uses.

### Alt 4 — `utf8.RuneCountInString` + index walk for Fix 3

Instead of `[]rune(t)`, use `utf8.RuneCountInString` to check length and then `utf8.DecodeRuneInString` in a loop to find the byte offset of the Nth rune. This avoids the full string-to-rune-slice allocation. Rejected for simplicity: at 500 runes, the allocation is trivial and the `[]rune` approach is more readable and already idiomatic in Go context-building code.

---

## Open Questions

1. **Should user bubbles also get `mdToHtml` in the future?** Currently user messages come from two sources: `localMessages` (text the user typed into the compose box, no Markdown expected) and session JSONL user turns (natural language). If voice-transcribed results contain backtick code references, it might be worth applying `mdToHtml` to user bubbles as well. Deferred — the current proposal only applies it to assistant turns.

2. **Should `normalizeProse` strip inline backtick spans too?** A single-backtick inline reference like `` `voci serve` `` is meaningful ASR context (entity name). Fix 4 strips only triple-backtick fence markers (keeping the body content). Whether inline backtick markers themselves should also be stripped (leaving just the identifier text) is left open — since inline code identifiers are already preserved as plain text after the backticks are removed, this is primarily a cosmetic question.

3. **`maxProseCharsTotal` semantic change:** Fix 3 changes `maxProseCharsPerTurn` from "500 bytes" to "500 runes". The outer cap (`maxProseCharsTotal = 3000`) is also converted to rune-based as part of Fix 3 (see Design section above), so that CJK sessions still get ~6 turns in the hint rather than only ~2. Both constants now mean runes, not bytes; for ASCII-only sessions the behaviour is identical.

4. **E2E test coverage for Markdown rendering:** The current Playwright suite (`e2e/`) tests PTT recording, emit, and auth flows (per `CLAUDE.md`). There is no dialogue-rendering assertion. Adding one requires a fixture session JSONL that injects a known assistant response, then asserting DOM structure in `#voci-dialogue`. The effort is non-trivial; it can be a follow-up task.

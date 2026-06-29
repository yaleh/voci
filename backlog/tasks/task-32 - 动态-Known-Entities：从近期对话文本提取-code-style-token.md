---
id: TASK-32
title: 动态 Known Entities：从近期对话文本提取 code-style token
status: 'Basic: Done'
assignee: []
created_date: '2026-06-29 02:32'
updated_date: '2026-06-29 03:00'
labels:
  - 'kind:basic'
  - 'area:context'
dependencies: []
ordinal: 27000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
从近期对话文本提取 code-style token，动态追加到 Known Entities 段（internal/context 层）
<!-- SECTION:DESCRIPTION:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
# Proposal: 动态 Known Entities：从近期对话文本提取 code-style token

## Background

voci is a voice-to-Claude-Code pipeline where users speak Chinese with embedded English technical terms. TeleSpeechASR is optimized for Chinese and frequently mangles embedded English tokens — for example "BuildContext" may be transcribed as a phonetic approximation, or "WebFetch" as "外部 Fetch". The current `buildKnownEntities()` in `internal/context/builder.go` is a static, hardcoded list of ~15 mappings that covers only the most common voci-specific terms. As a project evolves, new function names, file paths, package identifiers, and CLI flags continuously appear in dialogue but are absent from the static list, leaving them unprotected from ASR corruption. The `SessionSource` already captures recent dialogue turns (both user and assistant sides) as prose text in the JSONL-backed session file. This creates a natural, zero-configuration signal: code-style tokens that have recently appeared in conversation are precisely the terms most likely to recur in the next voice command and most likely to be mangled by ASR.

## Goals

1. A new `DynamicEntitiesSource` (implementing the existing `Source` interface) extracts code-style tokens from the last 3–6 dialogue turns and returns them as `Token: Token` identity mappings appended to the Known Entities section, measured by the source being registered in `defaultBuilder` and its output appearing in `AsrHint` in an integration test.
2. Token extraction covers: PascalCase/camelCase identifiers, snake_case/kebab-case identifiers, file extensions (`.go`, `.md`, etc.), CLI flags (`--xxx`), and plain English words ≥ 4 characters appearing in otherwise non-English (Chinese) text; common English stop words (e.g. "with", "that", "from", "this", "have", "will", "also", "just") are filtered out.
3. The feature produces no duplicate entries: tokens already present in the static `buildKnownEntities` output are suppressed, and each token appears at most once in the dynamic section, verified by unit test with overlapping input.
4. Extraction is purely in-process (no external calls, no disk I/O beyond what `SessionSource` already does), completing in < 5 ms for a 3000-character input, verified by a benchmark test.
5. When the session JSONL is unavailable or the extracted token set is empty, `DynamicEntitiesSource.Fetch` returns `("", "dynamic_entities")` and the Known Entities section degrades gracefully to the static list only, verified by a unit test with a nil/empty session.

## Proposed Approach

**New source type:** Add `DynamicEntitiesSource` in `internal/context/builder.go` (or a new file `dynamic_entities.go` in the same package). It implements `Source` with `Name() = "dynamic_entities"`. Its `Fetch` method:

1. Obtains recent prose text by either accepting injected text (for testability) or by calling `SessionSource.Fetch` internally to reuse the already-loaded JSONL lines. To avoid double-reading the session file, `DynamicEntitiesSource` can accept the prose text as a field set by a coordinating step, or alternatively read from the same `parseSessionSnippet` pipeline — the exact wiring is an implementation decision, but no second JSONL read should occur per Build cycle.
2. Applies a set of compiled `regexp.Regexp` patterns to extract candidate tokens: PascalCase (`[A-Z][a-z]+(?:[A-Z][a-z]+)+`), camelCase (`[a-z]+[A-Z][a-zA-Z]+`), snake_case (`[a-z][a-z0-9]*(?:_[a-z][a-z0-9]+)+`), kebab-case (`[a-z][a-z0-9]*(?:-[a-z][a-z0-9]+)+`), file extensions (`\.[a-z]{1,4}\b` adjacent to a word character), CLI flags (`--[a-z][a-z0-9-]+`), and plain ASCII words ≥ 4 chars surrounded by CJK characters or at CJK word boundaries.
3. Filters the candidate set against a hardcoded stop-word set and against the static entity keys already emitted by `buildKnownEntities`.
4. Emits `## Known Entities (dynamic)\nToken: Token\n...` (or appends directly to the same `## Known Entities` block — implementation decision to minimize ASR hint complexity).

**Registration:** `defaultBuilder` registers `DynamicEntitiesSource` after `KnownEntitiesSource` so that `assembleAsrHint` naturally appends the dynamic tokens after the static ones. The `assembleAsrHint` method already handles extra sources via the catch-all loop, so no changes to assembly logic are required.

**Prose input wiring:** `DynamicEntitiesSource` will accept an optional `TextFn func() string` field that returns raw prose text for extraction. When `TextFn` is nil, the source calls `SessionSource{}.Fetch(root)` and strips the `## Recent Dialogue` header to obtain plain text. This keeps the source self-contained and independently testable without requiring changes to `parseSessionSnippet`.

**Tests:** Unit tests in `builder_test.go` or a new `dynamic_entities_test.go` cover: (a) correct token extraction from sample mixed Chinese/English prose, (b) deduplication against static entities, (c) graceful empty/nil input, (d) a `testing.B` benchmark.

## Trade-offs and Risks

**Not doing:** We are not generating phonetic spoken-form variants (e.g. "run hinted" → RunHinted) for dynamically extracted tokens — the LLM in `RunHinted` already handles approximate matching, so identity mappings (`Token: Token`) are sufficient and safer (no risk of generating wrong phonetic forms).

**Not doing:** We are not persisting the dynamic entity set to disk or caching it independently; it is recomputed on every `Build` call from the session tail. This is acceptable given the < 5 ms extraction target.

**Risk — false positives:** Common English words appearing incidentally in Chinese dialogue (e.g. "also", "just") could pollute the entity list. Mitigated by the stop-word filter and the ≥ 4-character minimum, but the stop-word list will need ongoing maintenance.

**Risk — session double-read:** If `DynamicEntitiesSource` naively instantiates a new `SessionSource` and `defaultBuilder` also registers one, the JSONL file is tail-read twice per Build. This must be addressed by the `TextFn` injection pattern or by sharing session output between sources, adding minor architectural complexity.

**Risk — regex over-matching:** Patterns like snake_case may match non-code identifiers in prose. The impact is low (extra identity mappings are harmless to the LLM) but could bloat the hint. A cap (e.g. max 30 dynamic tokens per build) mitigates unbounded growth.

---

# Plan: 动态 Known Entities：从近期对话文本提取 code-style token

Proposal: docs/proposals/proposal-dynamic-known-entities.md

## Phase A: DynamicEntitiesSource — extraction logic + unit tests

### Tests (write first)

File: `internal/context/dynamic_entities_test.go`

Test functions (all must FAIL before implementation):

- `TestExtractCodeTokens_PascalCase` — input prose containing "BuildContext" and "SessionSource"; assert both tokens appear in output
- `TestExtractCodeTokens_CamelCase` — input prose containing "defaultBuilder"; assert token appears in output
- `TestExtractCodeTokens_SnakeCase` — input prose containing "session_source"; assert token appears in output
- `TestExtractCodeTokens_KebabCase` — input prose containing "claude-code"; assert token appears in output
- `TestExtractCodeTokens_FileExtension` — input prose containing "builder.go"; assert ".go" token appears in output
- `TestExtractCodeTokens_CliFlag` — input prose containing "--iterate"; assert token appears in output
- `TestExtractCodeTokens_PlainEnglishInChinese` — input "请用 fetch 命令 list 所有任务"; assert "fetch" and "list" appear; "命令" does not appear
- `TestExtractCodeTokens_StopWordFiltered` — input prose containing "with", "from", "that", "this", "will"; assert none appear in output
- `TestExtractCodeTokens_ShortWordFiltered` — input prose "use the add cmd"; assert "use", "the", "add", "cmd" (< 4 chars) not in output; "the" additionally filtered by stop-word list
- `TestDynamicEntitiesSource_EmptyText` — `DynamicEntitiesSource{TextFn: func() string { return "" }}` Fetch returns `("", "dynamic_entities")`
- `TestDynamicEntitiesSource_NilTextFn_NoSession` — `DynamicEntitiesSource{}` with no JSONL available (temp dir with no session file) returns `("", "dynamic_entities")`
- `TestDynamicEntitiesSource_DeduplicatesStaticEntities` — prose containing "voci", "RunHinted", "BuildContext" (all already in `buildKnownEntities` output); assert none appear in dynamic output
- `TestDynamicEntitiesSource_NoDuplicatesWithinDynamic` — prose repeating "DynamicEntitiesSource" three times; assert token appears exactly once in output
- `TestDynamicEntitiesSource_CapAt30Tokens` — prose crafted to yield > 30 distinct tokens; assert dynamic output contains at most 30 `Token: Token` lines
- `BenchmarkExtractCodeTokens_3000Chars` — benchmark with a ~3000-char mixed Chinese/English prose; `b.N` iterations must each complete so total time / N < 5 ms

### Implementation

Files to create or modify:

- **Create** `internal/context/dynamic_entities.go`
  - Compile a package-level `var` holding the set of regex patterns (PascalCase, camelCase, snake_case, kebab-case, file extension, CLI flag, plain ASCII ≥ 4 chars at CJK boundary)
  - `var stopWords = map[string]bool{...}` covering at least: "with", "that", "from", "this", "have", "will", "also", "just", "your", "into", "about", "then", "when", "where", "which", "their", "there", "these", "those"
  - `func extractCodeTokens(text string) []string` — applies patterns, deduplicates, filters stop words and tokens < 4 chars, caps at 30 results
  - `type DynamicEntitiesSource struct { TextFn func() string }` implementing `Source`
  - `func (s *DynamicEntitiesSource) Name() string { return "dynamic_entities" }`
  - `func (s *DynamicEntitiesSource) Fetch(root string) (string, string)` — uses `TextFn` if non-nil; otherwise calls `SessionSource{}.Fetch(root)` and strips the `## Recent Dialogue` header; returns `("", "dynamic_entities")` when text is empty or tokens are empty; otherwise returns `"## Known Entities (dynamic)\n" + "Token: Token\n"...`

### DoD

- [ ] `go test ./internal/context/...`
- [ ] `go test -run TestExtractCodeTokens ./internal/context/...` exits 0 with all 9 extraction tests passing
- [ ] `go test -run TestDynamicEntitiesSource ./internal/context/...` exits 0 with all 5 source tests passing
- [ ] `go test -bench BenchmarkExtractCodeTokens -benchtime=1s ./internal/context/... 2>&1 | grep -E 'ns/op' | awk '{print $3}' | awk -F. '{if ($1 < 5000000) exit 0; else exit 1}'`
- [ ] `! grep -q 'double.*read\|Read.*twice' internal/context/dynamic_entities.go`

---

## Phase B: Wire DynamicEntitiesSource into defaultBuilder + integration test

### Tests (write first)

File: `internal/context/builder_test.go` (append new test functions)

Test functions (all must FAIL before implementation):

- `TestDefaultBuilder_HasDynamicEntitiesSource` — call `defaultBuilder(dir, nil)`; assert that `b.Sources` contains a source with `Name() == "dynamic_entities"`
- `TestBuildContextWithSource_DynamicTokensInAsrHint` — set up `DynamicEntitiesSource` with a `TextFn` returning prose containing "FetchToolAdapter"; call `BuildContextWithSource(dir, src, noopGit)` where `src` is the `DynamicEntitiesSource`; assert `AsrHint` contains `"FetchToolAdapter: FetchToolAdapter"`
- `TestBuildContextWithSource_DynamicAfterStaticEntities` — `BuildContextWithSource` with a `DynamicEntitiesSource` TextFn returning prose containing "MyNewToken"; assert `strings.Index(result, "## Known Entities")` < `strings.Index(result, "MyNewToken")` (dynamic tokens follow static block)
- `TestBuildContextWithSource_DynamicEmptyWhenNoSession` — `BuildContextWithSource(dir, nil, noopGit)` with no session file; assert result does not contain `"## Known Entities (dynamic)"`

### Implementation

Files to modify:

- **Modify** `internal/context/builder.go`
  - In `defaultBuilder`: after `b.Register(&KnownEntitiesSource{})`, add registration of `&DynamicEntitiesSource{}` (nil `TextFn` — will self-source from SessionSource)
  - No changes to `assembleAsrHint` required (existing catch-all loop in `assembleAsrHint` handles extra sources automatically)

### DoD

- [ ] `go test ./internal/context/...`
- [ ] `go test -run TestDefaultBuilder_HasDynamicEntitiesSource ./internal/context/...` exits 0
- [ ] `go test -run TestBuildContextWithSource_Dynamic ./internal/context/...` exits 0
- [ ] `! grep -q 'DynamicEntitiesSource' internal/context/session_source.go`

---

## Constraints

- No LLM calls, no network I/O, no disk writes in `DynamicEntitiesSource.Fetch`
- The JSONL session file must be read at most once per `Builder.Build` cycle; `DynamicEntitiesSource` with a nil `TextFn` calls `SessionSource{}.Fetch(root)` which is a separate tail-read — this is acceptable because it is a single sequential call, not a second read within the same build unless two sources both hold `SessionSource`; `defaultBuilder` must not register both `SessionSource` and `DynamicEntitiesSource` with nil TextFn — the default `BuildContext` wiring adds `SessionSource` via `BuildContextWithSource`, so `defaultBuilder` should register `DynamicEntitiesSource` with nil TextFn only (it will read session independently)
- Token cap: at most 30 dynamic tokens per build to prevent hint bloat
- Stop-word list must cover at minimum the 19 words specified in the proposal background plus common short prepositions
- `DynamicEntitiesSource.Name()` must return exactly `"dynamic_entities"`
- Each Phase must stay within 200 lines of code change

## Acceptance Gate

- [ ] `go test ./...`
- [ ] `go test -run TestExtractCodeTokens ./internal/context/...`
- [ ] `go test -run TestDynamicEntitiesSource ./internal/context/...`
- [ ] `go test -run TestDefaultBuilder_HasDynamicEntitiesSource ./internal/context/...`
- [ ] `go test -run TestBuildContextWithSource_Dynamic ./internal/context/...`
- [ ] `go test -bench BenchmarkExtractCodeTokens -benchtime=1s ./internal/context/... 2>&1 | grep -E '[0-9]+ ns/op'`
- [ ] `! grep -q 'DynamicEntitiesSource' internal/context/session_source.go`
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Proposal self-review: APPROVED
premise-ledger:
[E] Motivation/Background (3-8 lines, explains WHY): 8-line background traces ASR mangle problem → static list gap → session data as zero-config signal; verified by reading the proposal text.
[E] Goals (numbered, concretely verifiable, no vague language): 5 goals each with a measurable condition (integration test, unit test, benchmark, specific return value); verified by inspection.
[E] Feasibility — Source interface exists at builder.go:17-21, defaultBuilder registration at :357-371, assembleAsrHint catch-all at :131-137, SessionSource.Fetch signature at session_source.go:291; all referenced hooks are real.
[E] Completeness — Trade-offs section covers: no phonetic variants, no persistence, false-positive risk, double-read risk, regex over-matching risk.
[E] Consistency — no contradictions found; double-read risk is identified and mitigated by TextFn pattern described in Proposed Approach.
GCL-self-report: E=5 C=0 H=0

Plan review iteration 1: APPROVED
premise-ledger:
[E] Goal coverage: All 5 proposal Goals mapped to Phase A/B tests and implementation items
[E] TDD structure: Both Phase A and B have ### Tests before ### Implementation
[E] TDD order: First DoD item in both phases is `go test ./internal/context/...`
[E] Acceptance gate: First item is `go test ./...`
[E] DoD executability: All DoD and Acceptance Gate items are shell commands
[E] Absence checks: `! grep -q` used correctly in both absence checks
[E] Phase ordering: Phase A creates DynamicEntitiesSource; Phase B wires it into defaultBuilder — no circular deps
[E] Scope discipline: Both phases implement only what Goals 1–5 back
[C] File paths: internal/context/builder.go, builder_test.go, session_source.go all verified present; dynamic_entities.go/.test.go are new (expected); docs/proposals/proposal-dynamic-known-entities.md in plan header does not exist but is metadata only, not an executable reference
GCL-self-report: E=9 C=1 H=0

claimed: 2026-06-29T02:55:41Z

claimed: 2026-06-29T02:55:52Z

Completed: 2026-06-29T03:00:14Z
## Execution Summary
Result: Done / Commit: d0082ad1761f1bb36f2836da7e057f8071f944ad
<!-- SECTION:NOTES:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 go test ./internal/context/...
- [ ] #2 go test -run TestExtractCodeTokens ./internal/context/...
- [ ] #3 go test -run TestDynamicEntitiesSource ./internal/context/...
- [ ] #4 go test -bench BenchmarkExtractCodeTokens -benchtime=1s ./internal/context/... 2>&1 | grep -E 'ns/op' | awk '{print $3}' | awk -F. '{if ($1 < 5000000) exit 0; else exit 1}'
- [ ] #5 ! grep -q 'double.*read\|Read.*twice' internal/context/dynamic_entities.go
- [ ] #6 go test ./internal/context/...
- [ ] #7 go test -run TestDefaultBuilder_HasDynamicEntitiesSource ./internal/context/...
- [ ] #8 go test -run TestBuildContextWithSource_Dynamic ./internal/context/...
- [ ] #9 ! grep -q 'DynamicEntitiesSource' internal/context/session_source.go
- [ ] #10 go test ./...
- [ ] #11 go test -run TestExtractCodeTokens ./internal/context/...
- [ ] #12 go test -run TestDynamicEntitiesSource ./internal/context/...
- [ ] #13 go test -run TestDefaultBuilder_HasDynamicEntitiesSource ./internal/context/...
- [ ] #14 go test -run TestBuildContextWithSource_Dynamic ./internal/context/...
- [ ] #15 go test -bench BenchmarkExtractCodeTokens -benchtime=1s ./internal/context/... 2>&1 | grep -E '[0-9]+ ns/op'
- [ ] #16 ! grep -q 'DynamicEntitiesSource' internal/context/session_source.go
<!-- DOD:END -->

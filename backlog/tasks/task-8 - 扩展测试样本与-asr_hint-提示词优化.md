---
id: TASK-8
title: 扩展测试样本与 asr_hint 提示词优化
status: 'Basic: Done'
assignee: []
created_date: '2026-06-27 16:01'
updated_date: '2026-06-27 16:21'
labels:
  - 'kind:basic'
dependencies: []
priority: high
ordinal: 8000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
创建更多语音和识别测试样本和测试用例。重点是验证 asr_hint 有效性和探索有效的提示词。
<!-- SECTION:DESCRIPTION:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
## Proposal: 扩展测试样本与 asr_hint 提示词优化

### Background

The current `RunHinted` prompt in `internal/pipeline/pipeline.go` instructs the LLM to "correct any speech-to-text errors…especially for technical terms, task IDs, project names, and file paths" using the hint as "Context." However, `BuildContext` in `internal/context/builder.go` produces narrative prose sections (`## Active Tasks`, `## CLAUDE.md`, `## Recent Commits`), not an explicit substitution guide. The LLM therefore treats the hint as background reading rather than a correction dictionary, and fails to map phonetically confused words back to known entities.

TASK-1 real-world testing confirmed this: "task one" became "task wang," "CLI" became "sea l i," and "voci" was never restored across four of five samples. The five existing samples in `scripts/gensamples/main.go` cover only a narrow slice of error types and provide no ground-truth fixture to measure whether a prompt change improves or regresses accuracy.

### Goals

1. The `scripts/gensamples` sample list grows from 5 to at least 15 entries, covering: multi-task-ID utterances, CLI flag names (`--file`, `--iterate`), Go package paths (`internal/pipeline`, `internal/asr`), function names (`RunHinted`, `BuildContext`), and number-spelled-out task IDs.
2. A new golden-fixture file (`testdata/testcases.json`) exists mapping each sample's source text to `expected_entities`. Machine-readable via `jq '.[] | select(.id=="sample-06") | .expected_entities'`.
3. The `RunHinted` system prompt contains an explicit "Known Entities" substitution instruction with the phrase "replace with the exact canonical spelling."
4. `BuildContext` emits a `## Known Entities` section (before `## Active Tasks`) with task IDs, project name, and key package paths as a bullet list.
5. Table-driven `TestRunHintedGolden` test passes via `go test ./internal/pipeline/... -v -run TestRunHintedGolden`.

### Proposed Approach

**Corpus expansion:** Add 10 new TTS source texts to `scripts/gensamples`. Introduce `testcases.json` as ground truth. Refactor gensamples to load from JSON instead of hardcoded slice.

**Hint format redesign:** Add `## Known Entities` section to `BuildContext` with spoken-form → canonical-form pairs: task IDs (number-word derivation for 1–10), project name, package paths, function names.

**Prompt redesign — `RunHinted`:** Restructure system prompt with explicit substitution imperative. Inject hint into an "Entity Correction Rules" block rather than generic context.

**Automated test harness:** `TestRunHintedGolden` uses a fake `ChatFn` (no Ollama required). Pure unit test, passes in CI without network.

### Trade-offs and Risks

- Model capability ceiling: gemma4:e4b may lack instruction-following fidelity for substitution. Prompt changes alone may not suffice.
- TTS API cost: 10 additional WAV files consume SiliconFlow quota (existing skip guard preserved).
- Prompt length: larger entity block may approach model context limit for large projects.
- Regression risk: directive substitution may over-correct genuinely ambiguous utterances (ambiguous sample excluded from golden test).
- Not in scope: streaming ASR, multi-speaker audio, non-English samples, Rewrite stage prompt.

---

## Plan: 扩展测试样本与 asr_hint 提示词优化

## Phase A: 黄金测试夹具与语料扩充

### Tests (write first)

File: `scripts/gensamples/cases_test.go`
- `TestLoadCasesReturnsAtLeast15` — calls `LoadCases("../../testdata/testcases.json")`, asserts `len >= 15`
- `TestLoadCaseHasRequiredFields` — asserts each case has non-empty `ID`, `TTSInput`, `ExpectedEntities`
- `TestLoadCasesAmbiguousCaseHasNoEntities` — finds `id == "sample-04"`, asserts `len(ExpectedEntities) == 0`

### Implementation

- Create `testdata/testcases.json` — 15 entries (5 existing + 10 new: multi-task-ID, CLI flags, package paths, function names, number-spelled task IDs, combined cases)
- Create `scripts/gensamples/cases.go` — `TestCase` struct + `LoadCases(path)` function
- Modify `scripts/gensamples/main.go` — load samples from `testcases.json` instead of hardcoded slice

### DoD

- [ ] `go test ./...`
- [ ] `jq '.[] | .id' /home/yale/work/voci/testdata/testcases.json | wc -l | grep -qE '^1[5-9]$|^[2-9][0-9]$'`
- [ ] `go build ./scripts/gensamples/...`

---

## Phase B: asr_hint 结构优化（Known Entities）

### Tests (write first)

Add to `internal/context/builder_test.go`:
- `TestBuildContextKnownEntitiesSection` — asserts result contains `"## Known Entities"`
- `TestBuildContextKnownEntitiesHasTaskID` — given task TASK-1, asserts Known Entities contains `"task one: TASK-1"`
- `TestBuildContextKnownEntitiesHasProjectName` — asserts Known Entities contains `"vocal: voci"`
- `TestBuildContextKnownEntitiesHasPackagePaths` — asserts entries for `internal/pipeline`, `internal/context`, `internal/asr`
- `TestBuildContextKnownEntitiesBeforeActiveTasks` — asserts index of `"## Known Entities"` < index of `"## Active Tasks"`

### Implementation

- Modify `internal/context/builder.go` — add `spokenTaskID`, `buildKnownEntities`, prepend Known Entities section in `BuildContext`

### DoD

- [ ] `go test ./...`
- [ ] `go test ./internal/context/... -v -run TestBuildContextKnownEntities`

---

## Phase C: RunHinted 提示词重写与黄金测试通过

### Tests (write first)

Add to `internal/pipeline/pipeline_test.go`:
- `TestRunHintedPromptHasExplicitSubstitution` — fake ChatFn captures system message, asserts contains `"replace"` and `"canonical"`
- `TestRunHintedGolden` — table-driven over non-ambiguous testcases.json entries; fake ChatFn; asserts system prompt contains spoken-form and canonical-form pairs

### Implementation

- Rewrite system prompt in `internal/pipeline/pipeline.go` with explicit substitution imperative and "canonical spelling" phrase
- Update `TestRunHintedCallsChatWithHint` to match new user message format

### DoD

- [ ] `go test ./...`
- [ ] `go test ./internal/pipeline/... -v -run TestRunHintedGolden`
- [ ] `grep -q "canonical spelling" /home/yale/work/voci/internal/pipeline/pipeline.go`

---

## Constraints

- `testdata/testcases.json` must be valid JSON parseable by `jq` without flags
- `## Known Entities` must appear before `## Active Tasks` in `BuildContext` output
- Spoken-form derivation for task IDs covers only integers 1–10
- WAV-skip guard in gensamples must not be removed
- `TestRunHintedGolden` is pure unit test — no running Ollama required
- ambiguous sample (sample-04, `expected_entities: []`) excluded from golden test assertions
- No changes to `internal/asr`, `internal/ollama`, or `cmd/voci`

## Acceptance Gate

- [ ] `go test ./...`
- [ ] `go build -o /tmp/voci ./cmd/voci`
- [ ] `jq '.[] | .id' /home/yale/work/voci/testdata/testcases.json | wc -l | grep -qE '^1[5-9]$|^[2-9][0-9]$'`
- [ ] `jq '.[] | select(.id=="sample-04") | .expected_entities | length' /home/yale/work/voci/testdata/testcases.json | grep -q '^0$'`
- [ ] `go test ./internal/context/... -v -run TestBuildContextKnownEntities`
- [ ] `go test ./internal/pipeline/... -v -run TestRunHintedGolden`
- [ ] `grep -q "canonical spelling" /home/yale/work/voci/internal/pipeline/pipeline.go`
- [ ] `! grep -q "Correct any speech-to-text errors" /home/yale/work/voci/internal/pipeline/pipeline.go`
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Proposal self-review: APPROVED
premise-ledger:
[E] background lines: 7 (confirmed by counting paragraphs in Background section; explains root cause — prompt-as-background-reading vs. substitution-dictionary, plus TASK-1 evidence)
[C] goal verifiability: all 5 goals checked — goal 1 by line count in gensamples/main.go; goal 2 by jq command; goal 3 by grep on pipeline.go; goal 4 by Go test assertion; goal 5 by go test command
[H] feasibility basis: RunHinted prompt structure read from internal/pipeline/pipeline.go; BuildContext string-builder pattern read from internal/context/builder.go; gensamples struct pattern read from scripts/gensamples/main.go; ChatFn interface read from cmd/voci/main.go
GCL-self-report: E=1 C=1 H=1

Proposal approved. Starting plan draft.

Plan review iteration 1: APPROVED
premise-ledger:
[E] Goal coverage: All 5 Goals addressed — Phase A covers Goals 1 & 2 (15-entry testcases.json + LoadCases); Phase B covers Goal 4 (## Known Entities in BuildContext); Phase C covers Goals 3 & 5 (RunHinted rewrite + TestRunHintedGolden)
[E] TDD structure: All three phases (A, B, C) have ### Tests before ### Implementation
[E] TDD order: First ### DoD item in every phase is `go test ./...`
[E] Acceptance gate: First ## Acceptance Gate item is `go test ./...`
[E] DoD executability: All ### DoD and ## Acceptance Gate items are valid shell commands; no natural-language items present
[E] Absence checks: `! grep -q` pattern used correctly (not `grep -qv`) in Acceptance Gate
[E] Phase ordering: A produces testcases.json; B produces BuildContext Known Entities; C consumes both — no circular deps
[E] Scope discipline: Every phase maps directly to a named Goal; no gold-plating beyond stated goals
[E] File paths: All referenced existing files confirmed present (builder.go, builder_test.go, pipeline.go, pipeline_test.go, gensamples/main.go, cmd/voci); new files are correctly scoped as outputs of the implementation
GCL-self-report: E=9 C=0 H=0

claimed: 2026-06-27T16:14:43Z

Phase A ✓ 2026-06-27T00:00:00Z: testcases.json (15 entries), cases.go, gensamples refactored

Phase B ✓ 2026-06-27T00:00:00Z: buildKnownEntities added to BuildContext

Phase C ✓ 2026-06-27T00:00:00Z: RunHinted prompt rewritten, TestRunHintedGolden passing

DoD #1: PASS — go test ./...
DoD #2: PASS — jq 15 entries
DoD #3: PASS — go build ./scripts/gensamples/...
DoD #4: PASS — go test ./internal/context/... -v -run TestBuildContextKnownEntities
DoD #5: PASS — go test ./internal/pipeline/... -v -run TestRunHintedGolden
DoD #6: PASS — grep -q "canonical spelling" pipeline.go
DoD #7: PASS — ! grep -q "Correct any speech-to-text errors" pipeline.go
DoD #8: PASS — go build -o /tmp/voci8 ./cmd/voci

## Execution Summary
Result: Done
Commit: e2a2dd1

Completed: 2026-06-27T16:21:11Z
## Execution Summary
Result: Done
Commit: 16a292b3e2b54c31a976eadaf69d761ed234efa3
All 8 DoD checks passed (independent worker verification).
<!-- SECTION:NOTES:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 go test ./...
- [ ] #2 jq '.[] | .id' /home/yale/work/voci/testdata/testcases.json | wc -l | grep -qE '^1[5-9]$|^[2-9][0-9]$'
- [ ] #3 go build ./scripts/gensamples/...
- [ ] #4 go test ./internal/context/... -v -run TestBuildContextKnownEntities
- [ ] #5 go test ./internal/pipeline/... -v -run TestRunHintedGolden
- [ ] #6 grep -q "canonical spelling" /home/yale/work/voci/internal/pipeline/pipeline.go
- [ ] #7 ! grep -q "Correct any speech-to-text errors" /home/yale/work/voci/internal/pipeline/pipeline.go
- [ ] #8 go build -o /tmp/voci ./cmd/voci
<!-- DOD:END -->

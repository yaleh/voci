---
id: TASK-58
title: P2 — internal/asr 富模型加薄接口（按需，收敛对外暴露面）
status: 'Basic: Done'
assignee: []
created_date: '2026-06-30 06:03'
updated_date: '2026-06-30 07:15'
labels:
  - 'kind:basic'
dependencies: []
ordinal: 1000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
internal/asr 2文件11struct/0接口/fanIn=3（daemon/mcp/cmd 引用），archguard 标记 tooManyStructs。daemon 已用 TranscribeFn 函数字段解耦，本条主要是收敛对外暴露面：提取 asr.Transcriber 接口与 asr.Result 聚合类型，将散落的 provider-specific struct 收进聚合类型，减少 11 个 struct 直接外露。优先级低，仅当需替换 ASR provider 时收益明显。
<!-- SECTION:DESCRIPTION:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
# Proposal: internal/asr 富模型加薄接口（按需）— Transcriber interface + Result aggregate

## Background

archguard flags `internal/asr` with `tooManyStructs`: 2 files, 11 structs, 0 interfaces,
fanIn=3 (daemon, mcp, cmd). All 11 structs are already *unexported* JSON transport DTOs
(10 `gemini*` shapes + 1 `transcribeResponse`); no caller references them. Callers use
only free functions (`Transcribe`, `GeminiChat`, `ExtractEntities`). daemon/mcp already
decouple via a *local* `TranscribeFn` func type, and cmd wraps `asr.Transcribe` in a
closure. So the consumer boundary is a bare `string`-returning function, not an
abstraction: swapping or enriching a provider edits every call site and the return shape.
This is LOW priority / on-demand — pursue only when a real provider swap or richer result
(confidence, segments, provider tag) is needed.

## Goals

1. An `asr.Transcriber` interface exists with a single method returning an `asr.Result`
   (provider-agnostic transcription contract).
2. An `asr.Result` aggregate type exists (at minimum `{Text string}`), giving callers one
   output type to depend on instead of a bare `string` plus provider DTOs.
3. A constructor (e.g. `asr.NewProviderTranscriber(...)`) returns a value satisfying
   `Transcriber`, backed by the existing `Transcribe`/`TranscribeGemini` logic — no
   behaviour change.
4. A `var _ Transcriber = ...` compile-time assertion exists, and a fake `Transcriber`
   in a test satisfies the interface.
5. An adapter (`asr.FnFromTranscriber`) converts any `Transcriber` into the legacy
   `func(ctx, key, audioPath, apiURL, language string, entities []string) string`
   signature, so a fake `Transcriber` can drive the existing `daemon.Server` path
   unchanged.
6. The 11 provider DTO structs remain unexported (regression guard), and all existing
   callers (daemon/mcp/cmd) compile and pass without edits.

## Proposed Approach

Add a thin interface layer *on top of* the existing rich implementation, additively — do
not change existing exported function signatures (which would ripple to the two
`TranscribeFn` type definitions and the cmd closures).

- New file `internal/asr/transcriber.go` introduces:
  - `type Result struct { Text string }` — the shared output aggregate, extensible later
    with confidence/segments/provider without touching the interface.
  - `type Options struct { Key, AudioPath, APIURL, Language, Provider, Model string;
    Entities []string }` — request inputs grouped (so the interface method stays small).
  - `type Transcriber interface { Transcribe(ctx context.Context, opts Options)
    (Result, error) }`.
  - `providerTranscriber` (unexported) implementing `Transcriber` by delegating to the
    existing `Transcribe` free function and wrapping the `string` into `Result`.
  - `func NewProviderTranscriber() Transcriber` and `var _ Transcriber =
    providerTranscriber{}`.
  - `func FnFromTranscriber(t Transcriber) func(ctx context.Context, key, audioPath,
    apiURL, language string, entities []string) string` — adapter that bridges the
    interface to the legacy `TranscribeFn`-shaped signature, returning `""` on error to
    preserve current semantics.
- Provider DTO structs stay unexported exactly as today (no rename, no move). The
  "collect provider structs into the aggregate" intent is realised by giving callers
  `Result` as their dependency surface, not by relocating transport DTOs (which are
  per-provider JSON shapes and not meaningfully mergeable).
- No call-site migration is forced. cmd/daemon/mcp remain on the existing free function /
  `TranscribeFn` path; the interface is opt-in for the next provider swap.

## Trade-offs and Risks

- **Low ROI today.** The consumer boundary is already function-decoupled; the interface
  pays off only when a second provider is swapped behind it or `Result` gains fields.
  This is explicitly gated on real need — ship now as a thin, additive seam, not a
  migration.
- **Premature abstraction risk.** Adding an interface with one production implementation
  can be speculative generality. Mitigation: keep it minimal (one method), keep existing
  functions as the implementation, force zero call-site changes, and provide the
  `FnFromTranscriber` adapter so the seam is proven against the real daemon path rather
  than hypothetical.
- **Two ways to call ASR** (free function + interface) temporarily coexist. Accepted: the
  free functions are the impl detail behind the interface; a later task may migrate
  callers and delete the closures once a real second provider lands.
- **archguard struct count unchanged.** This task does not reduce the 11 DTO structs (they
  are required transport shapes); it reduces *coupling*, not struct count. The
  `tooManyStructs` flag may persist and that is acceptable for an on-demand seam.

---

# TDD Plan: TASK-58 — asr.Transcriber interface + asr.Result aggregate (thin, additive)

## Constraints

- Additive only: do NOT change signatures of existing exported functions
  (`Transcribe`, `TranscribeGemini`, `GeminiChat`, `ExtractEntities`) — daemon/mcp
  `TranscribeFn` types and cmd closures must keep compiling untouched.
- No import cycle: `internal/asr` must not import `internal/daemon` (daemon imports asr).
  The daemon-path test therefore lives in package `daemon`, which already imports asr.
- Provider DTO structs (`gemini*`, `transcribeResponse`) stay unexported; no rename/move.
- All new production code lives in `internal/asr/transcriber.go`. Each phase ≤200 LOC.
- No behaviour change for existing callers; `FnFromTranscriber` returns `""` on error to
  match current `Transcribe` semantics.

## Phase 1 — Result aggregate + Transcriber interface + provider impl

### Tests (write first)
`internal/asr/transcriber_test.go`:
- `TestResultHasText`: construct `asr.Result{Text: "hello"}`, assert field readable.
- `TestFakeTranscriberSatisfiesInterface`: define a local `fakeTranscriber` whose
  `Transcribe(ctx, Options) (Result, error)` returns `Result{Text:"fake"}`; assign to a
  `var t asr.Transcriber = fakeTranscriber{}`; call and assert `Text=="fake"`.
- `TestNewProviderTranscriberImplementsInterface`: assert
  `asr.NewProviderTranscriber()` is a non-nil `asr.Transcriber` (compile + runtime).
- `TestProviderTranscriberReturnsResultText`: spin an `httptest` server returning
  `{"text":"hi"}` (siliconflow shape), call `NewProviderTranscriber().Transcribe` with
  `Options{AudioPath: <temp wav>, APIURL: srv.URL, Provider: "siliconflow"}`, assert
  `Result.Text=="hi"` and `err==nil`. Reuse the temp-wav helper pattern from
  `siliconflow_test.go`.

### Implementation
`internal/asr/transcriber.go` (new):
- `type Result struct { Text string }`
- `type Options struct { Key, AudioPath, APIURL, Language, Provider, Model string; Entities []string }`
- `type Transcriber interface { Transcribe(ctx context.Context, opts Options) (Result, error) }`
- `type providerTranscriber struct{}` with `Transcribe` delegating to existing
  `Transcribe(...)` free function, wrapping result into `Result{Text: ...}`.
- `func NewProviderTranscriber() Transcriber { return providerTranscriber{} }`
- `var _ Transcriber = providerTranscriber{}`

### DoD
- [ ] `go test ./internal/asr/...`
- [ ] `grep -q 'type Transcriber interface' internal/asr/transcriber.go`
- [ ] `grep -q 'type Result struct' internal/asr/transcriber.go`
- [ ] `grep -q 'func NewProviderTranscriber()' internal/asr/transcriber.go`
- [ ] `grep -q 'var _ Transcriber =' internal/asr/transcriber.go`
- [ ] `go vet ./internal/asr/...`

## Phase 2 — Legacy-Fn adapter + daemon-path proof + unexported-DTO guard

### Tests (write first)
`internal/asr/transcriber_test.go` (append):
- `TestFnFromTranscriberForwards`: build `fn := asr.FnFromTranscriber(fakeTranscriber{})`;
  call `fn(ctx, "k", "/tmp/x.wav", "", "en", nil)`; assert it returns `"fake"`.
- `TestFnFromTranscriberReturnsEmptyOnError`: fake returns `(Result{}, errBoom)`; assert
  `fn(...) == ""`.

`internal/daemon/transcriber_adapter_test.go` (new, package daemon):
- `TestDaemonAcceptsFnFromTranscriber`: assign `asr.FnFromTranscriber(fakeTranscriber{})`
  to a `daemon.TranscribeFn` variable (proves signature compatibility) and to a
  `daemon.Server{TranscribeFn: ...}`; invoke `Server.TranscribeFn(ctx,...)` and assert
  the fake's text flows through. (Mirrors the existing `TranscribeFn` injection in
  `server_test.go`.)

### Implementation
`internal/asr/transcriber.go` (append):
- `func FnFromTranscriber(t Transcriber) func(ctx context.Context, key, audioPath, apiURL, language string, entities []string) string`
  — builds an `Options` from the args, calls `t.Transcribe`, returns `Result.Text` or
  `""` on error.

### DoD
- [ ] `go test ./internal/asr/...`
- [ ] `go test ./internal/daemon/...`
- [ ] `grep -q 'func FnFromTranscriber' internal/asr/transcriber.go`
- [ ] `test -f internal/daemon/transcriber_adapter_test.go`
- [ ] `! grep -qE '^type (Gemini|Transcribe)[A-Za-z]* struct' internal/asr/gemini.go internal/asr/siliconflow.go`
- [ ] `! grep -qE '^type [A-Z][A-Za-z]*Response struct' internal/asr/gemini.go internal/asr/siliconflow.go`

## Acceptance Gate

- [ ] `go test ./...`
- [ ] `go build ./...`
- [ ] `grep -q 'type Transcriber interface' internal/asr/transcriber.go`
- [ ] `grep -q 'type Result struct' internal/asr/transcriber.go`
- [ ] `grep -q 'func FnFromTranscriber' internal/asr/transcriber.go`
- [ ] `! grep -qE '^type (Gemini|Transcribe)[A-Za-z]* struct' internal/asr/gemini.go internal/asr/siliconflow.go`
- [ ] `git diff --name-only internal/daemon/server.go internal/mcp/server.go cmd/voci/main.go | grep -q . && exit 1 || true`
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
claimed: 2026-06-30T07:10:58Z
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Added thin additive interface layer to `internal/asr`: `Transcriber` interface (single `Transcribe(ctx, Options) (Result, error)` method), `Result{Text string}` aggregate, `Options` struct, `NewProviderTranscriber()` constructor backed by existing `Transcribe()` free function, compile-time assertion, and `FnFromTranscriber()` adapter bridging to legacy `TranscribeFn` shape. Zero call-site changes — daemon/mcp/cmd untouched. Provider DTO structs remain unexported. 3 new files, `go test ./...` and `go build ./...` clean.
<!-- SECTION:FINAL_SUMMARY:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 go test ./internal/asr/...
- [ ] #2 grep -q 'type Transcriber interface' internal/asr/transcriber.go
- [ ] #3 grep -q 'type Result struct' internal/asr/transcriber.go
- [ ] #4 grep -q 'func NewProviderTranscriber()' internal/asr/transcriber.go
- [ ] #5 grep -q 'var _ Transcriber =' internal/asr/transcriber.go
- [ ] #6 go vet ./internal/asr/...
- [ ] #7 go test ./internal/asr/...
- [ ] #8 go test ./internal/daemon/...
- [ ] #9 grep -q 'func FnFromTranscriber' internal/asr/transcriber.go
- [ ] #10 test -f internal/daemon/transcriber_adapter_test.go
- [ ] #11 ! grep -qE '^type (Gemini|Transcribe)[A-Za-z]* struct' internal/asr/gemini.go internal/asr/siliconflow.go
- [ ] #12 ! grep -qE '^type [A-Z][A-Za-z]*Response struct' internal/asr/gemini.go internal/asr/siliconflow.go
- [ ] #13 go test ./...
- [ ] #14 go build ./...
- [ ] #15 grep -q 'type Transcriber interface' internal/asr/transcriber.go
- [ ] #16 grep -q 'type Result struct' internal/asr/transcriber.go
- [ ] #17 grep -q 'func FnFromTranscriber' internal/asr/transcriber.go
- [ ] #18 ! grep -qE '^type (Gemini|Transcribe)[A-Za-z]* struct' internal/asr/gemini.go internal/asr/siliconflow.go
- [ ] #19 git diff --name-only internal/daemon/server.go internal/mcp/server.go cmd/voci/main.go | grep -q . && exit 1 || true
<!-- DOD:END -->

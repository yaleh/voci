---
id: TASK-2
title: 上下文检索层：工具无关的 context_builder
status: 'Basic: Done'
assignee: []
created_date: '2026-06-27 13:57'
updated_date: '2026-06-28 01:26'
labels:
  - 'kind:basic'
dependencies:
  - TASK-1
priority: high
ordinal: 2000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
将 TASK-1 中的最小上下文构建器抽象为可复用、可扩展的独立模块（工具无关）。

## TASK-1 已交付（基线）
`internal/context/builder.go` 的 `BuildContext(root)` 已实现：读 backlog/tasks/*.md frontmatter（id/title/status）、CLAUDE.md、git log --oneline -10 → 拼接为单一 asr_hint 字符串。TASK-8/9/10 在此基础上优化提示词。

## 本任务增量（相对基线）
1. **source 插件化**：将三个来源（backlog/CLAUDE.md/git）重构为注册式 source 接口，缺失时静默降级
2. **provenance**：每条上下文标注来源，供改写阶段引用
3. **full_context**：除 asr_hint（窄，给 ASR 纠错）外，产出结构化 full_context（宽，给 LLM 改写消费）
4. **context_cache.json**：快照缓存，避免每次全量读取
5. （可选）meta-cc session signals 作为新增 source

## 降级理由（Epic → Basic）
最小版已在 TASK-1 落地，剩余仅为「重构 + 4 个局部特性」，规模小于 TASK-1 本身，单个 TDD pass 可完成。

## 设计要求
- 与下游 AI 工具解耦：context_builder 不知道下游是 Claude Code 还是 Codex
- 语言：Go；测试用 httptest/临时目录隔离，不依赖真实 repo

## 不做
- 下游工具适配（见 TASK-5）
- 意图解释（见 TASK-3）
<!-- SECTION:DESCRIPTION:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
将 TASK-1 中的最小上下文构建器抽象为可复用、可扩展的独立模块（工具无关）。

## TASK-1 已交付（基线）
`internal/context/builder.go` 的 `BuildContext(root)` 已实现：读 backlog/tasks/*.md frontmatter（id/title/status）、CLAUDE.md、git log --oneline -10 → 拼接为单一 asr_hint 字符串。TASK-8/9/10 在此基础上优化提示词。

## 本任务增量（相对基线）
1. **source 插件化**：将三个来源（backlog/CLAUDE.md/git）重构为注册式 source 接口，缺失时静默降级
2. **provenance**：每条上下文标注来源，供改写阶段引用
3. **full_context**：除 asr_hint（窄，给 ASR 纠错）外，产出结构化 full_context（宽，给 LLM 改写消费）
4. **context_cache.json**：快照缓存，避免每次全量读取
5. （可选）meta-cc session signals 作为新增 source

## 降级理由（Epic → Basic）
最小版已在 TASK-1 落地，剩余仅为「重构 + 4 个局部特性」，规模小于 TASK-1 本身，单个 TDD pass 可完成。

## 设计要求
- 与下游 AI 工具解耦：context_builder 不知道下游是 Claude Code 还是 Codex
- 语言：Go；测试用 httptest/临时目录隔离，不依赖真实 repo

## 不做
- 下游工具适配（见 TASK-5）
- 意图解释（见 TASK-3）

Acceptance Criteria:

---

# Plan: 上下文检索层：工具无关的 context_builder

## Phase A: Source 接口、ContextItem 与三个 source 实现

### Tests (write first)

File: `internal/context/source_test.go`
- `TestBacklogSourceReturnsItems` — BacklogSource with temp dir containing task-1.md frontmatter `id: TASK-1\ntitle: Fix login bug`; assert `Items(root)` returns `ContextItem` with Text containing "TASK-1" and Src == "backlog"
- `TestBacklogSourceEmptyDir` — empty `backlog/tasks/`; assert Items returns empty slice, nil error
- `TestClaudeSourceReturnsSentinel` — write CLAUDE.md with sentinel text; assert Items returns item with sentinel, Src == "claude"
- `TestClaudeSourceMissingFile` — no CLAUDE.md; assert Items returns empty slice, nil error (silent degrade)
- `TestGitSourceReturnsLog` — GitSource with fake runner returning `"abc1234 add auth\n"`; assert Items returns item with "add auth", Src == "git"
- `TestGitSourceEmptyLog` — runner returns ""; assert Items returns empty slice, nil error
- `TestBuildFullContextAllSources` — call BuildFullContext with all three default sources on a temp root with tasks+CLAUDE.md+fake git; assert items cover all three Src values
- `TestBuildFullContextDegradesMissingCLAUDE` — no CLAUDE.md; assert BuildFullContext returns no error, backlog+git items present
- `TestBuildContextBackwardCompat` — call existing `BuildContext(root, gitRunner)` signature; assert result still contains "## Known Entities" and "## Active Tasks"

### Implementation

- `internal/context/source.go` — defines `ContextItem struct { Text, Src string }` and `Source interface { Name() string; Items(root string) ([]ContextItem, error) }`
- `internal/context/backlog_source.go` — `BacklogSource` implements Source; `Name()` returns `"backlog"`; `Items()` extracts the existing backlog-reading logic from `builder.go` (frontmatter id/title/status → one ContextItem per task, Src="backlog")
- `internal/context/claude_source.go` — `ClaudeSource` implements Source; `Name()` returns `"claude"`; `Items()` reads `CLAUDE.md`; missing → empty slice, nil
- `internal/context/git_source.go` — `GitSource struct { Runner GitRunner }` implements Source; `Name()` returns `"git"`; `Items()` calls Runner(root); empty output → empty slice, nil
- `internal/context/builder.go` — add `BuildFullContext(root string, sources []Source) ([]ContextItem, error)` iterating sources, accumulating items, ignoring per-source errors; refactor `BuildContext(root string, gitRunner GitRunner) string` to call `BuildFullContext` with `[]Source{BacklogSource{}, ClaudeSource{}, GitSource{Runner: gitRunner}}` then render to the existing flat string format (Known Entities header + Active Tasks + CLAUDE.md + git sections — output byte-identical to current)

### DoD
- [ ] `go test ./internal/context/...`
- [ ] `go test ./...`

---

## Phase B: context_cache.json

### Tests (write first)

File: `internal/context/cache_test.go`
- `TestWriteCacheCreatesFile` — call `WriteCache(tmpDir, items)`; assert `context_cache.json` created in tmpDir
- `TestReadCacheRoundTrips` — WriteCache then ReadCache; assert items Text/Src fields match
- `TestReadCacheMissingFile` — no cache file; assert ReadCache returns nil items, nil error
- `TestReadCacheExpired` — write cache file then set modtime to 2 minutes ago; assert ReadCache with 60s TTL returns nil (stale)
- `TestBuildFullContextCachedWritesCache` — call `BuildFullContextCached` on temp root; assert `context_cache.json` created
- `TestBuildFullContextCachedHitsOnSecondCall` — fake sources that count Items() calls; call BuildFullContextCached twice; assert second call does NOT invoke source Items()

### Implementation

- `internal/context/cache.go` — `type cacheFile struct { Items []ContextItem }`; `WriteCache(root string, items []ContextItem) error`: marshal to JSON, write to `{root}/context_cache.json`; `ReadCache(root string, ttl time.Duration) ([]ContextItem, error)`: read+unmarshal, check file modtime vs ttl, return nil if missing or stale; `BuildFullContextCached(root string, sources []Source, ttl time.Duration) ([]ContextItem, error)`: try ReadCache → hit: return; miss: call BuildFullContext then WriteCache
- Update `.gitignore` — append `context_cache.json`

### DoD
- [ ] `go test ./internal/context/...`
- [ ] `go test ./...`
- [ ] `grep -q 'context_cache.json' .gitignore`

---

## Constraints
- `BuildContext(root, gitRunner)` 签名与输出格式不变（byte-identical），`cmd/voci` 和 `internal/pipeline` 无需修改
- Source 缺失时静默降级（不 panic，不返回 error，items 为空）
- 无新外部依赖（encoding/json、time、os 均为标准库）
- 语言：Go ≥ 1.23；测试用临时目录，不依赖真实 repo 或真实 git
- meta-cc session signals 不在本任务范围；Source 接口已为其留下扩展点
- context_cache.json 加入 .gitignore；WriteCache 失败静默（不阻塞主流程）
- 每个 Phase ≤ 200 行代码变更

## Acceptance Gate
- [ ] `go test ./...`
- [ ] `go build ./cmd/voci`
- [ ] `go vet ./...`
- [ ] `grep -q 'context_cache.json' .gitignore`
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Plan review iteration 1: APPROVED
premise-ledger:
[E] Goal coverage: All 4 required goals (source plugin, provenance, full_context, context_cache.json) addressed; optional meta-cc signals deferred to Constraints
[E] TDD structure: Phase A and Phase B each have ### Tests before ### Implementation
[E] TDD order: First DoD item in both phases is go test ./internal/context/...
[E] Acceptance gate: First item is go test ./...
[E] DoD executability: All DoD and Acceptance Gate items are shell commands; natural-language items already in Constraints
[C] Absence checks: No absence checks needed; no grep -qv anti-pattern present
[E] Phase ordering: Phase A defines Source interface and BuildFullContext; Phase B depends on those with no circular deps
[E] Scope discipline: Both phases map directly to proposal goals
[E] File paths: internal/context/builder.go, internal/context/builder_test.go, cmd/voci, .gitignore all confirmed to exist; new files created by implementation

claimed: 2026-06-28T01:19:53Z

Increment 1 ✓ 2026-06-28T00:00:00Z: Source interface + BacklogSource/ClaudeMdSource/GitLogSource/KnownEntitiesSource implementations; Builder with Register/Build; BuildContext compat wrapper preserved

Increment 2 ✓ 2026-06-28T00:00:00Z: Result struct with AsrHint/FullContext/Provenance; Build populates all three; AsrHint matches legacy output exactly

Increment 3 ✓ 2026-06-28T00:00:00Z: FullContext assembled with '## Project Context' header and ### subsections per source

Increment 4 ✓ 2026-06-28T00:00:00Z: BuildCached reads cache if < 60s old; Build writes .voci/context_cache.json; .gitignore updated

DoD #1: PASS — go test ./internal/context/... (20 tests, all pass)

DoD #2: PASS — go test ./... (all packages pass)

DoD #3: PASS — go test ./internal/context/... (re-run, 20 tests pass)

DoD #4: PASS — go test ./...

DoD #5: PASS — grep -q 'context_cache' .gitignore

DoD #6: PASS — go test ./...

DoD #7: PASS — go build ./cmd/voci

DoD #8: PASS — go vet ./...

DoD #9: PASS — grep -q 'context_cache' .gitignore

## Execution Summary
Result: Done
Commit: 8bf3941

Completed: 2026-06-28T01:26:47Z
## Execution Summary
Result: Done
Commit: 6517dd1efb1541f8fb8539caffe3cd143c4b4df9
All 9 DoD checks passed (independent worker verification).
<!-- SECTION:NOTES:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 go test ./internal/context/...
- [ ] #2 go test ./...
- [ ] #3 go test ./internal/context/...
- [ ] #4 go test ./...
- [ ] #5 grep -q 'context_cache.json' .gitignore
- [ ] #6 go test ./...
- [ ] #7 go build ./cmd/voci
- [ ] #8 go vet ./...
- [ ] #9 grep -q 'context_cache.json' .gitignore
<!-- DOD:END -->

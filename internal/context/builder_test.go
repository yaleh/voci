package context

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func makeTasksDir(t *testing.T, root string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, "backlog", "tasks"), 0755); err != nil {
		t.Fatal(err)
	}
}

func TestBuildContextReturnsString(t *testing.T) {
	tmpDir := t.TempDir()
	makeTasksDir(t, tmpDir)

	result := BuildContext(tmpDir, func(_ string) string { return "" })
	if result == "" {
		// Empty is acceptable if no files, but should not panic
		_ = result
	}
	// Just ensure it doesn't panic and returns a string
}

func TestBuildContextReadsCLAUDEMd(t *testing.T) {
	tmpDir := t.TempDir()
	makeTasksDir(t, tmpDir)

	sentinel := "SENTINEL_TEXT_XYZ"
	claudePath := filepath.Join(tmpDir, "CLAUDE.md")
	if err := os.WriteFile(claudePath, []byte(sentinel), 0644); err != nil {
		t.Fatal(err)
	}

	result := BuildContext(tmpDir, func(_ string) string { return "" })

	if !strings.Contains(result, sentinel) {
		t.Errorf("expected sentinel in result, got: %q", result)
	}
}

func TestBuildContextReadsGitLog(t *testing.T) {
	tmpDir := t.TempDir()
	makeTasksDir(t, tmpDir)

	fakeGit := func(_ string) string {
		return "abc1234 add auth\n"
	}

	result := BuildContext(tmpDir, fakeGit)

	if !strings.Contains(result, "add auth") {
		t.Errorf("expected 'add auth' in result, got: %q", result)
	}
}

func TestBuildContextMissingCLAUDEMd(t *testing.T) {
	tmpDir := t.TempDir()
	makeTasksDir(t, tmpDir)

	// Should not error, just return a string
	result := BuildContext(tmpDir, func(_ string) string { return "" })
	_ = result
}

func TestBuildContextEmptyBacklog(t *testing.T) {
	tmpDir := t.TempDir()
	makeTasksDir(t, tmpDir)

	// Empty tasks dir - should not error
	result := BuildContext(tmpDir, func(_ string) string { return "" })
	_ = result
}

// ---- Increment 1: Source interface + plugin registration ----

func TestSourceInterfaceClaudeMd(t *testing.T) {
	tmpDir := t.TempDir()
	sentinel := "CLAUDE_SENTINEL_ABC"
	claudePath := filepath.Join(tmpDir, "CLAUDE.md")
	if err := os.WriteFile(claudePath, []byte(sentinel), 0644); err != nil {
		t.Fatal(err)
	}

	src := &ClaudeMdSource{}
	snippet, prov := src.Fetch(tmpDir)
	if !strings.Contains(snippet, sentinel) {
		t.Errorf("expected snippet to contain sentinel %q, got %q", sentinel, snippet)
	}
	if prov != "claude.md" {
		t.Errorf("expected provenance 'claude.md', got %q", prov)
	}
}

func TestSourceInterfaceGitLog(t *testing.T) {
	src := &GitLogSource{Runner: func() string { return "abc1234 add auth\n" }}
	snippet, prov := src.Fetch("/irrelevant")
	if !strings.Contains(snippet, "add auth") {
		t.Errorf("expected snippet to contain 'add auth', got %q", snippet)
	}
	if prov != "git" {
		t.Errorf("expected provenance 'git', got %q", prov)
	}
}

// ---- Increment 2: Provenance + Result ----

func TestBuilderResultHasAstHint(t *testing.T) {
	tmpDir := t.TempDir()
	makeTasksDir(t, tmpDir)

	b := &Builder{}
	b.Register(&ClaudeMdSource{})
	b.Register(&GitLogSource{Runner: func() string { return "abc add auth\n" }})

	result := b.Build(tmpDir)
	if result.AsrHint == "" {
		t.Error("expected non-empty AsrHint")
	}
}

func TestBuilderResultHasFullContext(t *testing.T) {
	tmpDir := t.TempDir()
	makeTasksDir(t, tmpDir)

	b := &Builder{}
	b.Register(&ClaudeMdSource{})
	b.Register(&GitLogSource{Runner: func() string { return "" }})

	result := b.Build(tmpDir)
	if !strings.Contains(result.FullContext, "## Project Context") {
		t.Errorf("expected FullContext to contain '## Project Context', got: %q", result.FullContext)
	}
}

func TestBuilderResultHasProvenance(t *testing.T) {
	tmpDir := t.TempDir()
	makeTasksDir(t, tmpDir)

	b := &Builder{}
	b.Register(&ClaudeMdSource{})
	b.Register(&GitLogSource{Runner: func() string { return "abc\n" }})

	result := b.Build(tmpDir)
	for _, key := range []string{"claude.md", "git"} {
		if _, ok := result.Provenance[key]; !ok {
			t.Errorf("expected Provenance to have key %q", key)
		}
	}
}

// ---- Increment 4: context_cache.json ----

func TestBuildCachedWritesFile(t *testing.T) {
	tmpDir := t.TempDir()
	makeTasksDir(t, tmpDir)

	b := &Builder{}
	b.Register(&ClaudeMdSource{})
	b.Register(&GitLogSource{Runner: func() string { return "" }})

	_ = b.BuildCached(tmpDir)

	cachePath := filepath.Join(tmpDir, ".voci", "context_cache.json")
	if _, err := os.Stat(cachePath); err != nil {
		t.Errorf("expected cache file at %q, got error: %v", cachePath, err)
	}
}

func TestBuildCachedReadsCache(t *testing.T) {
	tmpDir := t.TempDir()
	makeTasksDir(t, tmpDir)

	// Write a fake cache with a sentinel AsrHint
	vociDir := filepath.Join(tmpDir, ".voci")
	if err := os.MkdirAll(vociDir, 0755); err != nil {
		t.Fatal(err)
	}
	cached := Result{
		AsrHint:    "cached_sentinel_hint",
		FullContext: "## Project Context\ncached",
		Provenance: map[string]string{"claude.md": "y", "git": "z"},
	}
	cf := struct {
		Result    Result    `json:"result"`
		CreatedAt time.Time `json:"created_at"`
	}{Result: cached, CreatedAt: time.Now()}
	data, _ := json.Marshal(cf)
	if err := os.WriteFile(filepath.Join(vociDir, "context_cache.json"), data, 0644); err != nil {
		t.Fatal(err)
	}

	b := &Builder{}
	b.Register(&ClaudeMdSource{})
	b.Register(&GitLogSource{Runner: func() string { return "" }})

	result := b.BuildCached(tmpDir)
	if result.AsrHint != "cached_sentinel_hint" {
		t.Errorf("expected cached AsrHint 'cached_sentinel_hint', got %q", result.AsrHint)
	}
}

// ---- BuildContextWithSource tests ----

// noopGit returns empty string — used to avoid real git calls in tests
func noopGit(root string) string { return "" }

// stubSource implements Source and returns a fixed snippet
type stubSource struct{ snippet string }

func (s *stubSource) Name() string                       { return "stub" }
func (s *stubSource) Fetch(root string) (string, string) { return s.snippet, "stub" }

func TestBuildContextWithSource_NilSrc_NoSessionSnippet(t *testing.T) {
	dir := t.TempDir()
	result := BuildContextWithSource(dir, nil, noopGit)
	if strings.Contains(result, "## Recent Session") {
		t.Error("expected no session section when src is nil")
	}
}

func TestBuildContextWithSource_CustomSrc_SnippetIncluded(t *testing.T) {
	dir := t.TempDir()
	src := &stubSource{snippet: "CUSTOM_SENTINEL"}
	result := BuildContextWithSource(dir, src, noopGit)
	if !strings.Contains(result, "CUSTOM_SENTINEL") {
		t.Errorf("expected CUSTOM_SENTINEL in hint, got: %s", result)
	}
}

// ---- Backward compat ----

func TestBuildContextBackwardCompat(t *testing.T) {
	tmpDir := t.TempDir()
	makeTasksDir(t, tmpDir)
	// Without backlog source, we need at least a CLAUDE.md to produce non-empty context
	if err := os.WriteFile(filepath.Join(tmpDir, "CLAUDE.md"), []byte("test"), 0644); err != nil {
		t.Fatal(err)
	}

	result := BuildContext(tmpDir, nil)
	if result == "" {
		t.Error("expected non-empty result from BuildContext with nil gitRunner")
	}
}

// ---- Config threading: Builder.CacheTTL ----

func TestBuildCached_CustomTTL_BypassesStaleCache(t *testing.T) {
	tmpDir := t.TempDir()
	makeTasksDir(t, tmpDir)

	vociDir := filepath.Join(tmpDir, ".voci")
	if err := os.MkdirAll(vociDir, 0755); err != nil {
		t.Fatal(err)
	}
	cached := Result{AsrHint: "stale_hint"}
	cf := struct {
		Result    Result    `json:"result"`
		CreatedAt time.Time `json:"created_at"`
	}{Result: cached, CreatedAt: time.Now().Add(-5 * time.Millisecond)}
	data, _ := json.Marshal(cf)
	if err := os.WriteFile(filepath.Join(vociDir, "context_cache.json"), data, 0644); err != nil {
		t.Fatal(err)
	}

	b := &Builder{CacheTTL: 1 * time.Millisecond}
	b.Register(&ClaudeMdSource{})

	result := b.BuildCached(tmpDir)
	if result.AsrHint == "stale_hint" {
		t.Error("expected cache to be bypassed when older than CacheTTL, got stale cached hint")
	}
}

func TestBuildCached_CustomTTL_UsesFreshCache(t *testing.T) {
	tmpDir := t.TempDir()
	makeTasksDir(t, tmpDir)

	vociDir := filepath.Join(tmpDir, ".voci")
	if err := os.MkdirAll(vociDir, 0755); err != nil {
		t.Fatal(err)
	}
	cached := Result{AsrHint: "fresh_hint"}
	cf := struct {
		Result    Result    `json:"result"`
		CreatedAt time.Time `json:"created_at"`
	}{Result: cached, CreatedAt: time.Now()}
	data, _ := json.Marshal(cf)
	if err := os.WriteFile(filepath.Join(vociDir, "context_cache.json"), data, 0644); err != nil {
		t.Fatal(err)
	}

	b := &Builder{CacheTTL: 1 * time.Hour}
	result := b.BuildCached(tmpDir)
	if result.AsrHint != "fresh_hint" {
		t.Errorf("expected cached AsrHint 'fresh_hint', got %q", result.AsrHint)
	}
}

// ---- Config threading: BuildContextWithSourceAndTuning ----

func TestBuildContextWithSourceAndTuning_AppliesEntityTuning(t *testing.T) {
	dir := t.TempDir()
	tuning := BuilderTuning{EntityTokenCap: 1, EntityMinTokenLen: 4}
	// TextFn override not available via this entry point, so drive dynamic
	// entities through defaultBuilder's own DynamicEntitiesSource by asserting
	// the tuning value is propagated onto it.
	b := defaultBuilderWithTuning(dir, noopGit, tuning)
	for _, src := range b.Sources {
		if des, ok := src.(*DynamicEntitiesSource); ok {
			if des.TokenCap != 1 {
				t.Errorf("TokenCap = %d, want 1", des.TokenCap)
			}
			if des.MinTokenLen != 4 {
				t.Errorf("MinTokenLen = %d, want 4", des.MinTokenLen)
			}
			return
		}
	}
	t.Error("defaultBuilderWithTuning should register a DynamicEntitiesSource")
}

func TestBuildContextWithSourceAndTuning_AppliesCacheTTL(t *testing.T) {
	dir := t.TempDir()
	tuning := BuilderTuning{CacheTTL: 5 * time.Second}
	b := defaultBuilderWithTuning(dir, noopGit, tuning)
	if b.CacheTTL != 5*time.Second {
		t.Errorf("CacheTTL = %v, want 5s", b.CacheTTL)
	}
}

func TestBuildContextWithSourceAndTuning_ReturnsHint(t *testing.T) {
	dir := t.TempDir()
	makeTasksDir(t, dir)
	gitWithLog := func(_ string) string { return "abc add auth\n" }
	result := BuildContextWithSourceAndTuning(dir, nil, gitWithLog, BuilderTuning{})
	// After removing KnownEntitiesSource, the hint should still contain GitLog
	// output header (Recent Commits) from the registered sources.
	if !strings.Contains(result, "## Recent Commits") {
		t.Errorf("expected ## Recent Commits in hint, got: %s", result)
	}
}

// ---- TASK-32: DynamicEntitiesSource wired into defaultBuilder ----

func TestDefaultBuilder_HasDynamicEntitiesSource(t *testing.T) {
	b := defaultBuilder(t.TempDir(), nil)
	for _, src := range b.Sources {
		if src.Name() == "dynamic_entities" {
			return
		}
	}
	t.Error("defaultBuilder should register a dynamic_entities source")
}

func TestBuildContextWithSource_DynamicTokensInAsrHint(t *testing.T) {
	dir := t.TempDir()
	// Register DynamicEntitiesSource with a TextFn that returns known tokens
	b := defaultBuilder(dir, noopGit)
	b.Register(&DynamicEntitiesSource{TextFn: func() string {
		return "FetchToolAdapter is the main component"
	}})
	result := b.Build(dir)
	if !strings.Contains(result.AsrHint, "FetchToolAdapter: FetchToolAdapter") {
		t.Errorf("expected 'FetchToolAdapter: FetchToolAdapter' in AsrHint, got: %s", result.AsrHint)
	}
}

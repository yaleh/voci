package context

import (
	"strings"
	"testing"
)

func TestExtractCodeTokens_PascalCase(t *testing.T) {
	tokens := extractCodeTokens("We need to fix BuildContext and SessionSource now")
	if !contains(tokens, "BuildContext") {
		t.Error("expected BuildContext")
	}
	if !contains(tokens, "SessionSource") {
		t.Error("expected SessionSource")
	}
}

func TestExtractCodeTokens_CamelCase(t *testing.T) {
	tokens := extractCodeTokens("the defaultBuilder handles registration")
	if !contains(tokens, "defaultBuilder") {
		t.Error("expected defaultBuilder")
	}
}

func TestExtractCodeTokens_SnakeCase(t *testing.T) {
	tokens := extractCodeTokens("use session_source for JSONL")
	if !contains(tokens, "session_source") {
		t.Error("expected session_source")
	}
}

func TestExtractCodeTokens_KebabCase(t *testing.T) {
	tokens := extractCodeTokens("run claude-code binary")
	if !contains(tokens, "claude-code") {
		t.Error("expected claude-code")
	}
}

func TestExtractCodeTokens_FileExtension(t *testing.T) {
	tokens := extractCodeTokens("edit builder.go now")
	if !contains(tokens, "builder.go") {
		t.Error("expected builder.go")
	}
}

func TestExtractCodeTokens_CliFlag(t *testing.T) {
	tokens := extractCodeTokens("run with --iterate flag")
	if !contains(tokens, "--iterate") {
		t.Error("expected --iterate")
	}
}

func TestExtractCodeTokens_PlainEnglishInChinese(t *testing.T) {
	tokens := extractCodeTokens("请用 fetch 命令 list 所有任务")
	if !contains(tokens, "fetch") {
		t.Error("expected fetch")
	}
	if !contains(tokens, "list") {
		t.Error("expected list")
	}
}

func TestExtractCodeTokens_StopWordFiltered(t *testing.T) {
	tokens := extractCodeTokens("with from that this will also just your into about")
	for _, w := range []string{"with", "from", "that", "this", "will", "also", "just", "your", "into", "about"} {
		if contains(tokens, w) {
			t.Errorf("stop word %q should be filtered", w)
		}
	}
}

func TestExtractCodeTokens_ShortWordFiltered(t *testing.T) {
	tokens := extractCodeTokens("use the add cmd")
	for _, w := range []string{"use", "the", "add", "cmd"} {
		if contains(tokens, w) {
			t.Errorf("short word %q should be filtered", w)
		}
	}
}

func TestDynamicEntitiesSource_EmptyText(t *testing.T) {
	s := &DynamicEntitiesSource{TextFn: func() string { return "" }}
	content, name := s.Fetch("/tmp")
	if name != "dynamic_entities" {
		t.Errorf("name = %q", name)
	}
	if content != "" {
		t.Errorf("expected empty content, got %q", content)
	}
}

func TestDynamicEntitiesSource_NilTextFn_NoSession(t *testing.T) {
	s := &DynamicEntitiesSource{}
	content, name := s.Fetch(t.TempDir())
	if name != "dynamic_entities" {
		t.Errorf("name = %q", name)
	}
	if content != "" {
		t.Errorf("expected empty content for no-session, got %q", content)
	}
}

func TestDynamicEntitiesSource_NoDupesInOutput(t *testing.T) {
	s := &DynamicEntitiesSource{TextFn: func() string { return "voci voci voci" }}
	content, _ := s.Fetch("/tmp")
	count := strings.Count(content, "voci: voci")
	if count > 1 {
		t.Errorf("expected at most 1 occurrence, got %d", count)
	}
}

func TestDynamicEntitiesSource_NilTextFn_ExtractsFromStructuredSession(t *testing.T) {
	s := &DynamicEntitiesSource{TextFn: func() string {
		return "## Claude Code Session\n- editing: internal/context/builder.go\n- ran: go test ./...\n"
	}}
	content, _ := s.Fetch("/tmp")
	// reFileExt should match builder.go
	if !strings.Contains(content, "builder.go") {
		t.Errorf("expected builder.go in output, got: %q", content)
	}
}

func TestDynamicEntitiesSource_NilTextFn_ExtractsFromGitLog(t *testing.T) {
	s := &DynamicEntitiesSource{TextFn: func() string {
		return "feat: add DynamicEntitiesSource refactor\n"
	}}
	content, _ := s.Fetch("/tmp")
	// PascalCase should match DynamicEntitiesSource
	if !strings.Contains(content, "DynamicEntitiesSource: DynamicEntitiesSource") {
		t.Errorf("expected DynamicEntitiesSource: DynamicEntitiesSource in output, got: %q", content)
	}
}

func TestDynamicEntitiesSource_NoDuplicatesWithinDynamic(t *testing.T) {
	s := &DynamicEntitiesSource{TextFn: func() string {
		return "DynamicEntitiesSource DynamicEntitiesSource DynamicEntitiesSource"
	}}
	content, _ := s.Fetch("/tmp")
	count := strings.Count(content, "DynamicEntitiesSource: DynamicEntitiesSource")
	if count != 1 {
		t.Errorf("expected 1 occurrence, got %d", count)
	}
}

func TestDynamicEntitiesSource_CapAt30Tokens(t *testing.T) {
	// Generate prose with 40+ distinct PascalCase tokens
	var sb strings.Builder
	for i := 0; i < 40; i++ {
		sb.WriteString("TokenAlpha")
		sb.WriteRune(rune('A' + i%26))
		sb.WriteString("Beta ")
	}
	s := &DynamicEntitiesSource{TextFn: func() string { return sb.String() }}
	content, _ := s.Fetch("/tmp")
	lines := strings.Split(strings.TrimSpace(content), "\n")
	// subtract header line
	tokenLines := 0
	for _, l := range lines {
		if strings.Contains(l, ": ") {
			tokenLines++
		}
	}
	if tokenLines > 30 {
		t.Errorf("expected at most 30 tokens, got %d", tokenLines)
	}
}

func TestDynamicEntitiesSource_CustomTokenCap(t *testing.T) {
	var sb strings.Builder
	for i := 0; i < 40; i++ {
		sb.WriteString("TokenAlpha")
		sb.WriteRune(rune('A' + i%26))
		sb.WriteString("Beta ")
	}
	s := &DynamicEntitiesSource{TextFn: func() string { return sb.String() }, TokenCap: 5}
	content, _ := s.Fetch("/tmp")
	lines := strings.Split(strings.TrimSpace(content), "\n")
	tokenLines := 0
	for _, l := range lines {
		if strings.Contains(l, ": ") {
			tokenLines++
		}
	}
	if tokenLines > 5 {
		t.Errorf("expected at most 5 tokens with custom TokenCap, got %d", tokenLines)
	}
}

func TestDynamicEntitiesSource_CustomMinTokenLen(t *testing.T) {
	// "abc" is 3 chars, filtered by default MinTokenLen=4. With MinTokenLen=2, "abc" should pass.
	s := &DynamicEntitiesSource{TextFn: func() string { return "abc xyz" }, MinTokenLen: 2}
	content, _ := s.Fetch("/tmp")
	if !strings.Contains(content, "abc: abc") {
		t.Errorf("expected 'abc: abc' with custom MinTokenLen=2, got %q", content)
	}
}

func TestDynamicEntitiesSource_DefaultMinTokenLenUnchanged(t *testing.T) {
	s := &DynamicEntitiesSource{TextFn: func() string { return "abc xyz" }}
	content, _ := s.Fetch("/tmp")
	if strings.Contains(content, "abc: abc") {
		t.Errorf("expected short words filtered with default MinTokenLen, got %q", content)
	}
}

func BenchmarkExtractCodeTokens_3000Chars(b *testing.B) {
	// ~3000 char mixed Chinese/English prose
	prose := strings.Repeat("BuildContext SessionSource 请修改 defaultBuilder 运行 --iterate 检查 builder.go 更新 session_source ", 30)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		extractCodeTokens(prose)
	}
}

func contains(slice []string, s string) bool {
	for _, v := range slice {
		if v == s {
			return true
		}
	}
	return false
}

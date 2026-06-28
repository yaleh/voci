package inject

import "testing"

func TestNewDefaultInjector_WithTmuxTarget(t *testing.T) {
	inj := NewDefaultInjector("my-pane")
	if inj == nil {
		t.Fatal("expected non-nil Injector")
	}
}

func TestNewDefaultInjector_NoTarget(t *testing.T) {
	inj := NewDefaultInjector("")
	if inj == nil {
		t.Fatal("expected non-nil Injector (clipboard fallback)")
	}
}

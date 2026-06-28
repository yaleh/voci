package inject

import (
	"errors"
	"testing"
)

type mockInjector struct {
	err    error
	called bool
}

func (m *mockInjector) Inject(text string) error {
	m.called = true
	return m.err
}

func TestChainInjector_FirstSucceeds(t *testing.T) {
	first := &mockInjector{err: nil}
	second := &mockInjector{err: nil}

	chain := &ChainInjector{Injectors: []Injector{first, second}}
	if err := chain.Inject("text"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !first.called {
		t.Error("expected first injector to be called")
	}
	if second.called {
		t.Error("expected second injector NOT to be called when first succeeds")
	}
}

func TestChainInjector_FirstFailsSecondSucceeds(t *testing.T) {
	first := &mockInjector{err: errors.New("first failed")}
	second := &mockInjector{err: nil}

	chain := &ChainInjector{Injectors: []Injector{first, second}}
	if err := chain.Inject("text"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !first.called {
		t.Error("expected first injector to be called")
	}
	if !second.called {
		t.Error("expected second injector to be called as fallback")
	}
}

func TestChainInjector_AllFail(t *testing.T) {
	first := &mockInjector{err: errors.New("first failed")}
	second := &mockInjector{err: errors.New("second failed")}

	chain := &ChainInjector{Injectors: []Injector{first, second}}
	err := chain.Inject("text")
	if err == nil {
		t.Fatal("expected error when all injectors fail, got nil")
	}
	if err.Error() != "second failed" {
		t.Errorf("expected last error, got: %v", err)
	}
}

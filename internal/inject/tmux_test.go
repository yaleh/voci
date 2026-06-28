package inject

import (
	"errors"
	"testing"
)

func TestTmuxInjector_HappyPath(t *testing.T) {
	var gotName string
	var gotArgs []string

	inj := &TmuxInjector{
		Target: "my-session:1.0",
		CmdRunner: func(name string, args ...string) error {
			gotName = name
			gotArgs = args
			return nil
		},
	}

	if err := inj.Inject("hello world"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if gotName != "tmux" {
		t.Errorf("expected command 'tmux', got %q", gotName)
	}
	want := []string{"send-keys", "-t", "my-session:1.0", "hello world", "Enter"}
	if len(gotArgs) != len(want) {
		t.Fatalf("expected args %v, got %v", want, gotArgs)
	}
	for i, w := range want {
		if gotArgs[i] != w {
			t.Errorf("arg[%d]: want %q, got %q", i, w, gotArgs[i])
		}
	}
}

func TestTmuxInjector_CmdError(t *testing.T) {
	inj := &TmuxInjector{
		Target: "my-session:1.0",
		CmdRunner: func(name string, args ...string) error {
			return errors.New("tmux not found")
		},
	}

	err := inj.Inject("hello")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestTmuxInjector_EmptyTarget(t *testing.T) {
	called := false
	inj := &TmuxInjector{
		Target: "",
		CmdRunner: func(name string, args ...string) error {
			called = true
			return nil
		},
	}

	err := inj.Inject("hello")
	if err == nil {
		t.Fatal("expected error for empty target, got nil")
	}
	if called {
		t.Error("CmdRunner should not be called when target is empty")
	}
}

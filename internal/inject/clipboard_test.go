package inject

import (
	"errors"
	"testing"
)

func TestClipboardInjector_XclipSuccess(t *testing.T) {
	inj := &ClipboardInjector{
		CmdRunner: func(name string, args ...string) error {
			if name == "xclip" {
				return nil
			}
			return errors.New("unexpected call to " + name)
		},
	}

	if err := inj.Inject("some text"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestClipboardInjector_XclipFailsXdotoolSuccess(t *testing.T) {
	inj := &ClipboardInjector{
		CmdRunner: func(name string, args ...string) error {
			if name == "xclip" {
				return errors.New("xclip not found")
			}
			if name == "xdotool" {
				return nil
			}
			return errors.New("unexpected call to " + name)
		},
	}

	if err := inj.Inject("some text"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestClipboardInjector_BothFail(t *testing.T) {
	inj := &ClipboardInjector{
		CmdRunner: func(name string, args ...string) error {
			return errors.New(name + " not found")
		},
	}

	err := inj.Inject("some text")
	if err == nil {
		t.Fatal("expected error when both xclip and xdotool fail, got nil")
	}
}

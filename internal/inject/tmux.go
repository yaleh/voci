package inject

import "fmt"

// TmuxInjector sends text to a tmux pane via send-keys.
type TmuxInjector struct {
	// Target is the tmux pane target (e.g. "session:window.pane").
	Target string
	// CmdRunner executes an external command. Defaults to running the real binary.
	CmdRunner func(name string, args ...string) error
}

// Inject sends text to the configured tmux pane followed by Enter.
// Text is passed as a separate argument to prevent shell injection.
func (t *TmuxInjector) Inject(text string) error {
	if t.Target == "" {
		return fmt.Errorf("tmux target is empty")
	}
	return t.CmdRunner("tmux", "send-keys", "-t", t.Target, text, "Enter")
}

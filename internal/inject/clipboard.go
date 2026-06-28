package inject

// ClipboardInjector sends text to the clipboard via xclip, falling back to xdotool.
type ClipboardInjector struct {
	// CmdRunner executes an external command.
	CmdRunner func(name string, args ...string) error
}

// Inject tries xclip first, then xdotool as a fallback.
func (c *ClipboardInjector) Inject(text string) error {
	if err := c.CmdRunner("xclip", "-selection", "clipboard"); err == nil {
		return nil
	}
	return c.CmdRunner("xdotool", "type", "--clearmodifiers", text)
}

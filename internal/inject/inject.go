package inject

// Injector sends text to a target destination (e.g. a tmux pane or clipboard).
type Injector interface {
	Inject(text string) error
}

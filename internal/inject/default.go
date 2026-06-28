package inject

import "os/exec"

// NewDefaultInjector builds a ChainInjector that tries tmux first (if tmuxTarget
// is non-empty) and falls back to the clipboard.
func NewDefaultInjector(tmuxTarget string) Injector {
	realRunner := func(name string, args ...string) error {
		return exec.Command(name, args...).Run()
	}

	var injectors []Injector
	if tmuxTarget != "" {
		injectors = append(injectors, &TmuxInjector{
			Target:    tmuxTarget,
			CmdRunner: realRunner,
		})
	}
	injectors = append(injectors, &ClipboardInjector{
		CmdRunner: realRunner,
	})
	return &ChainInjector{Injectors: injectors}
}

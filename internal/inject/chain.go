package inject

// ChainInjector tries each Injector in order and returns on the first success.
// If all fail, it returns the last error.
type ChainInjector struct {
	Injectors []Injector
}

// Inject iterates through the chain and returns nil on the first success.
func (c *ChainInjector) Inject(text string) error {
	var last error
	for _, inj := range c.Injectors {
		if err := inj.Inject(text); err == nil {
			return nil
		} else {
			last = err
		}
	}
	return last
}

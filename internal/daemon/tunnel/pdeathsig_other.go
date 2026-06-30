//go:build !linux

package tunnel

import "os/exec"

func applyChildAttrs(_ *exec.Cmd) {}

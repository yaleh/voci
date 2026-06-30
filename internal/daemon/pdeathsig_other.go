//go:build !linux

package daemon

import "os/exec"

func applyChildAttrs(_ *exec.Cmd) {}

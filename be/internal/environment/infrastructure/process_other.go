//go:build !windows

package infrastructure

import "os/exec"

func hideWindow(_ *exec.Cmd) {}

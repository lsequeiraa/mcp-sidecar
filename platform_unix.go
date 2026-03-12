//go:build !windows

package main

import (
	"os/exec"
	"syscall"
)

// buildCommand wraps a shell command string for Unix execution via /bin/sh -c.
func buildCommand(command string) *exec.Cmd {
	return exec.Command("/bin/sh", "-c", command)
}

// setSysProcAttr configures the child to run in its own process group so we
// can kill the entire tree with a negative-pgid signal.
func setSysProcAttr(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
	}
}

// gracefulStop sends SIGTERM to the process group.
func gracefulStop(pid int) error {
	return syscall.Kill(-pid, syscall.SIGTERM)
}

// forceKill sends SIGKILL to the process group.
func forceKill(pid int) error {
	return syscall.Kill(-pid, syscall.SIGKILL)
}

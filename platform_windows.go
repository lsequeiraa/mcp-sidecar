//go:build windows

package main

import (
	"fmt"
	"os/exec"
	"syscall"
)

// buildCommand wraps a shell command string for Windows execution via cmd /C.
func buildCommand(command string) *exec.Cmd {
	return exec.Command("cmd", "/C", command)
}

// setSysProcAttr configures process creation flags so the child gets its own
// process group, enabling tree-kill via taskkill /T.
func setSysProcAttr(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP,
	}
}

// gracefulStop asks the process to terminate via taskkill (no /F).
func gracefulStop(pid int) error {
	kill := exec.Command("taskkill", "/PID", fmt.Sprintf("%d", pid))
	return kill.Run()
}

// forceKill forcefully terminates the process and its entire tree.
func forceKill(pid int) error {
	kill := exec.Command("taskkill", "/F", "/T", "/PID", fmt.Sprintf("%d", pid))
	return kill.Run()
}

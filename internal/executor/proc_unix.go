//go:build !windows
// +build !windows

package executor

import (
	"context"
	"os/exec"
	"syscall"
)

func shellCommand(ctx context.Context, cmdStr string) *exec.Cmd {
	return exec.CommandContext(ctx, "/bin/sh", "-c", cmdStr)
}

func shellCommandNoCtx(cmdStr string) *exec.Cmd {
	return exec.Command("/bin/sh", "-c", cmdStr)
}

func setCmdProcessAttrs(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
	}
}

// killProcessGroup sends SIGKILL to the entire process group of cmd, ensuring
// orphaned child processes (e.g. from "sleep 30") are also terminated and do
// not keep stdout/stderr pipes open after context cancellation or timeout.
func killProcessGroup(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	// Negative PID signals the whole process group (pgid == shell PID when Setpgid=true).
	return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
}

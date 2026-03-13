//go:build windows
// +build windows

package executor

import (
	"context"
	"os/exec"
	"strconv"
)

func shellCommand(ctx context.Context, cmdStr string) *exec.Cmd {
	return exec.CommandContext(ctx, "cmd.exe", "/C", cmdStr)
}

func shellCommandNoCtx(cmdStr string) *exec.Cmd {
	return exec.Command("cmd.exe", "/C", cmdStr)
}

func setCmdProcessAttrs(cmd *exec.Cmd) {
	// No special process attributes needed for Windows
}

// killProcessGroup kills the process tree on Windows. Unlike Unix, Windows has
// no process group concept, so we use taskkill /F /T to forcefully terminate
// the process and all of its children (e.g. subshells spawned by cmd.exe).
func killProcessGroup(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	// /F = force, /T = include child processes, /PID = target by PID.
	kill := exec.Command("taskkill", "/F", "/T", "/PID", strconv.Itoa(cmd.Process.Pid)) //nolint:gosec
	_ = kill.Run()
	return cmd.Process.Kill()
}

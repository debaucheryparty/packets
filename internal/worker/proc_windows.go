//go:build windows

package worker

import (
	"fmt"
	"os/exec"
)

func prepareProcessGroup(cmd *exec.Cmd) {
	// On Windows, child processes are attached to Job Objects or killed via taskkill /T
}

func killProcessTree(cmd *exec.Cmd) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	pid := cmd.Process.Pid
	// /F forces termination, /T kills child processes (process tree)
	killCmd := exec.Command("taskkill", "/F", "/T", "/PID", fmt.Sprintf("%d", pid))
	_ = killCmd.Run()
	return cmd.Process.Kill()
}

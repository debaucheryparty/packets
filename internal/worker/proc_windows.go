//go:build windows

package worker

import (
	"fmt"
	"os/exec"
)

func prepareProcessGroup(cmd *exec.Cmd) {
}

func killProcessTree(cmd *exec.Cmd) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	pid := cmd.Process.Pid

	killCmd := exec.Command("taskkill", "/F", "/T", "/PID", fmt.Sprintf("%d", pid))
	_ = killCmd.Run()
	return cmd.Process.Kill()
}

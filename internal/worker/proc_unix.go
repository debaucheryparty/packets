//go:build !windows

package worker

import (
	"os/exec"
	"syscall"
)

func prepareProcessGroup(cmd *exec.Cmd, policy HostSecurityPolicy) {
	attr := cmd.SysProcAttr
	if attr == nil {
		attr = &syscall.SysProcAttr{}
	}
	attr.Setpgid = true
	if policy.DedicatedUID > 0 || policy.DedicatedGID > 0 {
		attr.Credential = &syscall.Credential{
			Uid: uint32(policy.DedicatedUID),
			Gid: uint32(policy.DedicatedGID),
		}
	}
	cmd.SysProcAttr = attr
}

func killProcessTree(cmd *exec.Cmd) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	pgid := cmd.Process.Pid

	return syscall.Kill(-pgid, syscall.SIGKILL)
}

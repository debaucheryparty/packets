package environment

import (
	"context"
	"os/exec"
	"strings"
)

type ZephyrResolver struct{}

func NewZephyrResolver() *ZephyrResolver {
	return &ZephyrResolver{}
}

func (z *ZephyrResolver) Check(ctx context.Context, comp Component, remoteWorker bool) ([]ToolchainRequirement, error) {
	reqs := []ToolchainRequirement{
		checkTool("Python 3", "python3", "--version", "apt-get install -y python3 python3-pip"),
		checkTool("west", "west", "--version", "pip3 install --user west"),
		checkTool("CMake", "cmake", "--version", "apt-get install -y cmake"),
		checkTool("Ninja", "ninja", "--version", "apt-get install -y ninja-build"),
		checkTool("DTC (Device Tree Compiler)", "dtc", "--version", "apt-get install -y device-tree-compiler"),
		z.checkZephyrSDK(),
	}
	return reqs, nil
}

func (z *ZephyrResolver) checkZephyrSDK() ToolchainRequirement {
	req := ToolchainRequirement{
		Name:       "Zephyr SDK",
		CanPrepare: true,
		PrepareCmd: "wget https://github.com/zephyrproject-rtos/sdk-ng/releases/download/v0.16.8/zephyr-sdk-0.16.8_linux-x86_64.tar.xz && tar xf zephyr-sdk-*.tar.xz",
	}

	out, err := exec.Command("west", "config", "zephyr.sdk-path").CombinedOutput()
	if err == nil && len(strings.TrimSpace(string(out))) > 0 {
		req.Status = StatusOk
		req.Details = strings.TrimSpace(string(out))
		return req
	}

	req.Status = StatusMissing
	req.Details = "ZEPHYR_SDK_INSTALL_DIR not set"
	return req
}

func checkTool(name, cmdName, verArg, prepareCmd string) ToolchainRequirement {
	req := ToolchainRequirement{
		Name:       name,
		CanPrepare: prepareCmd != "",
		PrepareCmd: prepareCmd,
	}

	out, err := exec.Command(cmdName, verArg).CombinedOutput()
	if err == nil {
		req.Status = StatusOk
		req.Details = strings.TrimSpace(strings.Split(string(out), "\n")[0])
		return req
	}

	req.Status = StatusMissing
	req.Details = cmdName + " not found in PATH"
	return req
}

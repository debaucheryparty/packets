package android

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

type Capabilities struct {
	JDKInstalled      bool
	JDKVersion        string
	ADBInstalled      bool
	ADBVersion        string
	EmulatorInstalled bool
	EmulatorVersion   string
	SDKInstalled      bool
	SDKRoot           string
	BuildTools        bool
	ScrCpyInstalled   bool
	KVMAvailable      bool
	OS                string
	Arch              string
}

func (c *Capabilities) Status() string {
	if !c.JDKInstalled || !c.ADBInstalled || !c.SDKInstalled {
		return "NOT_READY"
	}
	if !c.KVMAvailable {
		return "DEGRADED"
	}
	return "READY"
}

func (c *Capabilities) DegradedReason() string {
	if !c.JDKInstalled {
		return "JDK is not installed"
	}
	if !c.ADBInstalled {
		return "ADB (Android Platform Tools) is not installed"
	}
	if !c.SDKInstalled {
		return "Android SDK is not installed or ANDROID_HOME is not set"
	}
	if !c.KVMAvailable {
		return "KVM acceleration is unavailable. Android Emulator may perform poorly."
	}
	return ""
}

func CheckCapabilities(ctx context.Context) (*Capabilities, error) {
	caps := &Capabilities{
		OS:   runtime.GOOS,
		Arch: runtime.GOARCH,
	}

	if out, err := runCmd(ctx, "java", "-version"); err == nil {
		caps.JDKInstalled = true
		caps.JDKVersion = firstLine(out)
	} else if out, err := runCmd(ctx, "java", "--version"); err == nil {
		caps.JDKInstalled = true
		caps.JDKVersion = firstLine(out)
	}

	if out, err := runCmd(ctx, "adb", "version"); err == nil {
		caps.ADBInstalled = true
		caps.ADBVersion = firstLine(out)
	}

	if out, err := runCmd(ctx, "emulator", "-version"); err == nil {
		caps.EmulatorInstalled = true
		caps.EmulatorVersion = firstLine(out)
	}

	sdkRoot := os.Getenv("ANDROID_HOME")
	if sdkRoot == "" {
		sdkRoot = os.Getenv("ANDROID_SDK_ROOT")
	}
	if sdkRoot != "" {
		if fi, err := os.Stat(sdkRoot); err == nil && fi.IsDir() {
			caps.SDKInstalled = true
			caps.SDKRoot = sdkRoot
			btDir := filepath.Join(sdkRoot, "build-tools")
			if entries, err := os.ReadDir(btDir); err == nil && len(entries) > 0 {
				caps.BuildTools = true
			}
		}
	}

	if _, err := runCmd(ctx, "scrcpy", "--version"); err == nil {
		caps.ScrCpyInstalled = true
	}

	if runtime.GOOS == "linux" {
		caps.KVMAvailable = checkKVM()
	}

	return caps, nil
}

func checkKVM() bool {
	fi, err := os.Stat("/dev/kvm")
	if err != nil {
		return false
	}
	return !fi.IsDir()
}

func runCmd(ctx context.Context, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%s: %w", name, err)
	}
	return string(out), nil
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	lines := strings.SplitN(s, "\n", 2)
	return strings.TrimSpace(lines[0])
}

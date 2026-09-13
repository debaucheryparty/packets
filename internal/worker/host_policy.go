package worker

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

type HostSecurityPolicy struct {
	AllowRoot       bool
	DedicatedUser   string
	DedicatedUID    int
	DedicatedGID    int
	AllowedRoots    []string
	BlockedCommands []string
	SanitizeEnv     bool
}

func DefaultHostSecurityPolicy() HostSecurityPolicy {
	return HostSecurityPolicy{
		AllowRoot:   false,
		SanitizeEnv: true,
		BlockedCommands: []string{
			"rm -rf /",
			"rm -rf /*",
			"mkfs",
			"dd if=/dev",
			"shutdown",
			"reboot",
			"format",
			"diskpart",
			":(){ :|:& };:",
		},
	}
}

func (p HostSecurityPolicy) Validate(srcDir string, command []string) error {
	cleaned := filepath.Clean(srcDir)
	if cleaned == "" || cleaned == "/" || cleaned == "\\" || cleaned == "." {
		return fmt.Errorf("insecure working directory: %s", srcDir)
	}

	if len(p.AllowedRoots) > 0 {
		allowed := false
		for _, root := range p.AllowedRoots {
			cleanRoot := filepath.Clean(root)
			rel, err := filepath.Rel(cleanRoot, cleaned)
			if err == nil && !strings.HasPrefix(rel, "..") {
				allowed = true
				break
			}
		}
		if !allowed {
			return fmt.Errorf("working directory %s outside allowed roots", srcDir)
		}
	}

	cmdLine := strings.Join(command, " ")
	lowerCmd := strings.ToLower(cmdLine)
	for _, blocked := range p.BlockedCommands {
		if strings.Contains(lowerCmd, strings.ToLower(blocked)) {
			return fmt.Errorf("command violates host security policy: contains blocked sequence %q", blocked)
		}
	}

	if runtime.GOOS != "windows" && !p.AllowRoot && os.Geteuid() == 0 && p.DedicatedUID == 0 {
		return errors.New("host runner security policy forbids executing as root without dedicated non-root UID")
	}

	return nil
}

package android

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type AndroidCacheInputs struct {
	SourceHash    string
	JDKVersion    string
	SDKVersion    string
	BuildToolsVer string
	AGPVersion    string
	GradleVersion string
	NDKVersion    string
	Variant       string
	Arch          string
}

func CacheKey(inputs AndroidCacheInputs) string {
	h := sha256.New()
	for _, v := range []string{
		inputs.SourceHash,
		inputs.JDKVersion,
		inputs.SDKVersion,
		inputs.BuildToolsVer,
		inputs.AGPVersion,
		inputs.GradleVersion,
		inputs.NDKVersion,
		inputs.Variant,
		inputs.Arch,
	} {
		h.Write([]byte(v))
		h.Write([]byte{0})
	}
	return "android:" + hex.EncodeToString(h.Sum(nil))
}

func CollectCacheInputs(ctx context.Context, projectDir, variant, snapshotRef string) (*AndroidCacheInputs, error) {
	inputs := &AndroidCacheInputs{
		SourceHash: snapshotRef,
		Variant:    variant,
	}

	if out, err := runCmdIn(ctx, projectDir, "java", "-version"); err == nil {
		inputs.JDKVersion = firstLineOf(out)
	} else if out, err := runCmdIn(ctx, projectDir, "java", "--version"); err == nil {
		inputs.JDKVersion = firstLineOf(out)
	}

	gradlew := filepath.Join(projectDir, "gradlew")
	if _, err := os.Stat(gradlew); err == nil {
		if out, err := runCmdIn(ctx, projectDir, gradlew, "--version"); err == nil {
			inputs.GradleVersion = extractGradleVersion(out)
		}
	}

	sdkRoot := os.Getenv("ANDROID_HOME")
	if sdkRoot == "" {
		sdkRoot = os.Getenv("ANDROID_SDK_ROOT")
	}
	if sdkRoot != "" {
		inputs.SDKVersion = latestDirVersion(filepath.Join(sdkRoot, "platforms"))
		inputs.BuildToolsVer = latestDirVersion(filepath.Join(sdkRoot, "build-tools"))
		inputs.NDKVersion = latestDirVersion(filepath.Join(sdkRoot, "ndk"))
	}

	if agp := detectAGPVersion(projectDir); agp != "" {
		inputs.AGPVersion = agp
	}

	return inputs, nil
}

func extractGradleVersion(output string) string {
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Gradle ") {
			return strings.TrimPrefix(line, "Gradle ")
		}
	}
	return strings.TrimSpace(firstLineOf(output))
}

func latestDirVersion(dir string) string {
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) == 0 {
		return ""
	}
	latest := ""
	for _, e := range entries {
		if e.IsDir() && e.Name() > latest {
			latest = e.Name()
		}
	}
	return latest
}

func detectAGPVersion(projectDir string) string {
	candidates := []string{
		filepath.Join(projectDir, "build.gradle"),
		filepath.Join(projectDir, "build.gradle.kts"),
	}
	for _, path := range candidates {
		f, err := os.Open(path)
		if err != nil {
			continue
		}
		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			line := scanner.Text()
			if strings.Contains(line, "com.android.tools.build:gradle:") {
				parts := strings.Split(line, "com.android.tools.build:gradle:")
				if len(parts) > 1 {
					ver := strings.Trim(strings.Fields(parts[1])[0], `"'`)
					f.Close() //nolint:errcheck
					return ver
				}
			}
		}
		f.Close() //nolint:errcheck
	}
	return ""
}

func runCmdIn(ctx context.Context, dir, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%s: %w", name, err)
	}
	return string(out), nil
}

func firstLineOf(s string) string {
	s = strings.TrimSpace(s)
	lines := strings.SplitN(s, "\n", 2)
	return strings.TrimSpace(lines[0])
}

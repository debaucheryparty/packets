package android

import (
	"archive/zip"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type BundletoolRunner interface {
	BuildAPKs(ctx context.Context, aabPath, apksPath string, universal bool, serial string) error
	InstallAPKs(ctx context.Context, apksPath, serial string) error
	ExtractUniversalAPK(apksPath, outAPKPath string) error
	GetDeviceSpec(ctx context.Context, serial, outJSONPath string) error
}

type ExecBundletool struct {
	execPath string
	isJar    bool
	adbPath  string
}

func ResolveBundletoolPath() (string, bool) {
	if p, err := exec.LookPath("bundletool"); err == nil {
		return p, false
	}
	var jarCandidates []string
	if val := os.Getenv("ANDROID_HOME"); val != "" {
		jarCandidates = append(jarCandidates,
			filepath.Join(val, "cmdline-tools", "latest", "bin", "bundletool.jar"),
			filepath.Join(val, "bundletool.jar"),
		)
	}
	if home, err := os.UserHomeDir(); err == nil {
		jarCandidates = append(jarCandidates,
			filepath.Join(home, ".packets", "tools", "bundletool.jar"),
			filepath.Join(home, "bundletool.jar"),
		)
	}
	for _, c := range jarCandidates {
		if fi, err := os.Stat(c); err == nil && !fi.IsDir() {
			return c, true
		}
	}
	return "bundletool", false
}

func NewExecBundletool() *ExecBundletool {
	p, isJar := ResolveBundletoolPath()
	return &ExecBundletool{
		execPath: p,
		isJar:    isJar,
		adbPath:  ResolveADBPath(),
	}
}

func (b *ExecBundletool) cmd(ctx context.Context, args ...string) *exec.Cmd {
	fullArgs := args
	if b.adbPath != "" {
		fullArgs = append(fullArgs, "--adb="+b.adbPath)
	}
	if b.isJar {
		javaArgs := append([]string{"-jar", b.execPath}, fullArgs...)
		return exec.CommandContext(ctx, "java", javaArgs...)
	}
	return exec.CommandContext(ctx, b.execPath, fullArgs...)
}

func (b *ExecBundletool) BuildAPKs(ctx context.Context, aabPath, apksPath string, universal bool, serial string) error {
	args := []string{
		"build-apks",
		"--bundle=" + aabPath,
		"--output=" + apksPath,
		"--overwrite",
	}
	if universal {
		args = append(args, "--mode=universal")
	} else if serial != "" {
		args = append(args, "--connected-device", "--device-id="+serial)
	}

	out, err := b.cmd(ctx, args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("bundletool build-apks: %w\n%s", err, string(out))
	}
	return nil
}

func (b *ExecBundletool) InstallAPKs(ctx context.Context, apksPath, serial string) error {
	args := []string{
		"install-apks",
		"--apks=" + apksPath,
	}
	if serial != "" {
		args = append(args, "--device-id="+serial)
	}

	out, err := b.cmd(ctx, args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("bundletool install-apks: %w\n%s", err, string(out))
	}
	return nil
}

func (b *ExecBundletool) ExtractUniversalAPK(apksPath, outAPKPath string) error {
	return ExtractUniversalAPK(apksPath, outAPKPath)
}

func (b *ExecBundletool) GetDeviceSpec(ctx context.Context, serial, outJSONPath string) error {
	args := []string{
		"get-device-spec",
		"--output=" + outJSONPath,
		"--overwrite",
	}
	if serial != "" {
		args = append(args, "--device-id="+serial)
	}

	out, err := b.cmd(ctx, args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("bundletool get-device-spec: %w\n%s", err, string(out))
	}
	return nil
}

func ExtractUniversalAPK(apksPath, outAPKPath string) error {
	r, err := zip.OpenReader(apksPath)
	if err != nil {
		return fmt.Errorf("open apks archive %s: %w", apksPath, err)
	}
	defer func() { _ = r.Close() }()

	var targetFile *zip.File
	for _, f := range r.File {
		if f.Name == "universal.apk" || strings.HasSuffix(f.Name, ".apk") {
			targetFile = f
			if f.Name == "universal.apk" {
				break
			}
		}
	}
	if targetFile == nil {
		return fmt.Errorf("no APK found inside %s", apksPath)
	}

	rc, err := targetFile.Open()
	if err != nil {
		return fmt.Errorf("open file in apks archive: %w", err)
	}
	defer func() { _ = rc.Close() }()

	if err := os.MkdirAll(filepath.Dir(outAPKPath), 0o755); err != nil {
		return err
	}
	out, err := os.Create(outAPKPath)
	if err != nil {
		return fmt.Errorf("create output apk: %w", err)
	}
	defer func() { _ = out.Close() }()

	_, err = io.Copy(out, rc)
	return err
}

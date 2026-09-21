package android

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

type SigningConfig struct {
	KeystorePath     string
	KeystorePassword string
	KeyAlias         string
	KeyPassword      string
}

type APKVerificationResult struct {
	Verified bool
	V1Scheme bool
	V2Scheme bool
	V3Scheme bool
	V4Scheme bool
	Signers  []string
	RawLog   string
}

type KeystoreGenOpts struct {
	Path         string
	Alias        string
	Password     string
	ValidityDays int
	DName        string
}

func ResolveApksignerPath() string {
	names := []string{"apksigner"}
	if runtime.GOOS == "windows" {
		names = []string{"apksigner.bat", "apksigner"}
	}
	for _, name := range names {
		if p, err := exec.LookPath(name); err == nil {
			return p
		}
	}
	if p := findBuildToolsBinary(names...); p != "" {
		return p
	}
	return "apksigner"
}

func ResolveZipalignPath() string {
	names := []string{"zipalign"}
	if runtime.GOOS == "windows" {
		names = []string{"zipalign.exe", "zipalign"}
	}
	for _, name := range names {
		if p, err := exec.LookPath(name); err == nil {
			return p
		}
	}
	if p := findBuildToolsBinary(names...); p != "" {
		return p
	}
	return "zipalign"
}

func ResolveKeytoolPath() string {
	name := "keytool"
	if runtime.GOOS == "windows" {
		name = "keytool.exe"
	}
	if p, err := exec.LookPath(name); err == nil {
		return p
	}
	if jh := os.Getenv("JAVA_HOME"); jh != "" {
		p := filepath.Join(jh, "bin", name)
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
			return p
		}
	}
	return "keytool"
}

func ResolveJarsignerPath() string {
	name := "jarsigner"
	if runtime.GOOS == "windows" {
		name = "jarsigner.exe"
	}
	if p, err := exec.LookPath(name); err == nil {
		return p
	}
	if jh := os.Getenv("JAVA_HOME"); jh != "" {
		p := filepath.Join(jh, "bin", name)
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
			return p
		}
	}
	return "jarsigner"
}

func findBuildToolsBinary(names ...string) string {
	var roots []string
	if val := os.Getenv("ANDROID_HOME"); val != "" {
		roots = append(roots, filepath.Join(val, "build-tools"))
	}
	if val := os.Getenv("ANDROID_SDK_ROOT"); val != "" {
		roots = append(roots, filepath.Join(val, "build-tools"))
	}
	if val := os.Getenv("LOCALAPPDATA"); val != "" {
		roots = append(roots, filepath.Join(val, "Android", "Sdk", "build-tools"))
	}
	if home, err := os.UserHomeDir(); err == nil {
		roots = append(roots,
			filepath.Join(home, "Android", "Sdk", "build-tools"),
			filepath.Join(home, "android-sdk", "build-tools"),
		)
	}

	for _, root := range roots {
		entries, err := os.ReadDir(root)
		if err != nil {
			continue
		}
		for i := len(entries) - 1; i >= 0; i-- {
			if !entries[i].IsDir() {
				continue
			}
			for _, name := range names {
				target := filepath.Join(root, entries[i].Name(), name)
				if fi, err := os.Stat(target); err == nil && !fi.IsDir() {
					return target
				}
			}
		}
	}
	return ""
}

func ResolveSigningConfig(path, password, alias, keypass string) (*SigningConfig, error) {
	if path == "" {
		path = os.Getenv("PACKETS_ANDROID_KEYSTORE")
	}
	if password == "" {
		password = os.Getenv("PACKETS_ANDROID_KEYSTORE_PASSWORD")
	}
	if alias == "" {
		alias = os.Getenv("PACKETS_ANDROID_KEY_ALIAS")
	}
	if keypass == "" {
		keypass = os.Getenv("PACKETS_ANDROID_KEY_PASSWORD")
	}

	if path == "" {
		home, err := os.UserHomeDir()
		if err == nil {
			debugKS := filepath.Join(home, ".android", "debug.keystore")
			if fi, err := os.Stat(debugKS); err == nil && !fi.IsDir() {
				path = debugKS
				if alias == "" {
					alias = "androiddebugkey"
				}
				if password == "" {
					password = "android"
				}
				if keypass == "" {
					keypass = "android"
				}
			}
		}
	}

	if path == "" {
		return nil, fmt.Errorf("no keystore configured (specify --keystore or set PACKETS_ANDROID_KEYSTORE)")
	}
	if alias == "" {
		return nil, fmt.Errorf("no key alias configured (specify --alias or set PACKETS_ANDROID_KEY_ALIAS)")
	}
	if keypass == "" {
		keypass = password
	}

	return &SigningConfig{
		KeystorePath:     path,
		KeystorePassword: password,
		KeyAlias:         alias,
		KeyPassword:      keypass,
	}, nil
}

func GenerateKeystore(ctx context.Context, opts KeystoreGenOpts) error {
	if opts.Path == "" {
		return fmt.Errorf("keystore path is required")
	}
	if opts.Alias == "" {
		opts.Alias = "release"
	}
	if opts.Password == "" {
		return fmt.Errorf("keystore password is required")
	}
	if opts.ValidityDays <= 0 {
		opts.ValidityDays = 10000
	}
	if opts.DName == "" {
		opts.DName = "CN=Packets Developer, OU=Mobile, O=Packets, L=Unknown, ST=Unknown, C=US"
	}

	if err := os.MkdirAll(filepath.Dir(opts.Path), 0o755); err != nil {
		return err
	}

	args := []string{
		"-genkeypair",
		"-v",
		"-keystore", opts.Path,
		"-alias", opts.Alias,
		"-keyalg", "RSA",
		"-keysize", "2048",
		"-validity", fmt.Sprintf("%d", opts.ValidityDays),
		"-storepass", opts.Password,
		"-keypass", opts.Password,
		"-dname", opts.DName,
	}

	cmd := exec.CommandContext(ctx, ResolveKeytoolPath(), args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("keytool generate keystore: %w\n%s", err, string(out))
	}
	return nil
}

func SignAPK(ctx context.Context, inAPK, outAPK string, cfg *SigningConfig) error {
	if cfg == nil {
		return fmt.Errorf("signing config is nil")
	}

	zipalignBin := ResolveZipalignPath()
	tempAligned := inAPK + ".aligned.tmp"
	defer func() { _ = os.Remove(tempAligned) }()

	alignCmd := exec.CommandContext(ctx, zipalignBin, "-v", "-p", "-f", "4", inAPK, tempAligned)
	alignOut, alignErr := alignCmd.CombinedOutput()
	targetForSigning := tempAligned
	if alignErr != nil {
		targetForSigning = inAPK
	}

	if outAPK == "" {
		outAPK = inAPK
	}
	if err := os.MkdirAll(filepath.Dir(outAPK), 0o755); err != nil {
		return err
	}

	apksignerBin := ResolveApksignerPath()
	args := []string{
		"sign",
		"--ks", cfg.KeystorePath,
		"--ks-key-alias", cfg.KeyAlias,
	}
	if cfg.KeystorePassword != "" {
		args = append(args, "--ks-pass", "pass:"+cfg.KeystorePassword)
	}
	if cfg.KeyPassword != "" {
		args = append(args, "--key-pass", "pass:"+cfg.KeyPassword)
	}
	args = append(args, "--out", outAPK, targetForSigning)

	signCmd := exec.CommandContext(ctx, apksignerBin, args...)
	signOut, signErr := signCmd.CombinedOutput()
	if signErr != nil {
		return fmt.Errorf("apksigner sign: %w\n%s (align: %s)", signErr, string(signOut), string(alignOut))
	}
	return nil
}

func VerifyAPK(ctx context.Context, apkPath string) (*APKVerificationResult, error) {
	apksignerBin := ResolveApksignerPath()
	args := []string{"verify", "--verbose", apkPath}
	cmd := exec.CommandContext(ctx, apksignerBin, args...)
	out, err := cmd.CombinedOutput()
	raw := string(out)

	res := &APKVerificationResult{
		RawLog: raw,
	}
	if err != nil {
		return res, fmt.Errorf("apksigner verify failed: %w\n%s", err, raw)
	}
	res.Verified = true

	scanner := bufio.NewScanner(strings.NewReader(raw))
	for scanner.Scan() {
		line := scanner.Text()
		if strings.Contains(line, "Verified using v1 scheme (JAR signing): true") {
			res.V1Scheme = true
		}
		if strings.Contains(line, "Verified using v2 scheme (APK Signature Scheme v2): true") {
			res.V2Scheme = true
		}
		if strings.Contains(line, "Verified using v3 scheme (APK Signature Scheme v3): true") {
			res.V3Scheme = true
		}
		if strings.Contains(line, "Verified using v4 scheme (APK Signature Scheme v4): true") {
			res.V4Scheme = true
		}
		if strings.HasPrefix(line, "Signer #") || strings.Contains(line, "Subject:") {
			res.Signers = append(res.Signers, strings.TrimSpace(line))
		}
	}
	return res, nil
}

func SignAAB(ctx context.Context, aabPath string, cfg *SigningConfig) error {
	if cfg == nil {
		return fmt.Errorf("signing config is nil")
	}
	jarsignerBin := ResolveJarsignerPath()
	args := []string{
		"-keystore", cfg.KeystorePath,
	}
	if cfg.KeystorePassword != "" {
		args = append(args, "-storepass", cfg.KeystorePassword)
	}
	if cfg.KeyPassword != "" {
		args = append(args, "-keypass", cfg.KeyPassword)
	}
	args = append(args, aabPath, cfg.KeyAlias)

	cmd := exec.CommandContext(ctx, jarsignerBin, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("jarsigner sign: %w\n%s", err, string(out))
	}
	return nil
}

func VerifyAAB(ctx context.Context, aabPath string) error {
	jarsignerBin := ResolveJarsignerPath()
	cmd := exec.CommandContext(ctx, jarsignerBin, "-verify", "-verbose", aabPath)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("jarsigner verify: %w\n%s", err, string(out))
	}
	return nil
}

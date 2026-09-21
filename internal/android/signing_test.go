package android

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestResolveSigningConfigExplicit(t *testing.T) {
	cfg, err := ResolveSigningConfig("my.keystore", "pass1", "myalias", "keypass1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.KeystorePath != "my.keystore" {
		t.Errorf("expected my.keystore, got %s", cfg.KeystorePath)
	}
	if cfg.KeystorePassword != "pass1" {
		t.Errorf("expected pass1, got %s", cfg.KeystorePassword)
	}
	if cfg.KeyAlias != "myalias" {
		t.Errorf("expected myalias, got %s", cfg.KeyAlias)
	}
	if cfg.KeyPassword != "keypass1" {
		t.Errorf("expected keypass1, got %s", cfg.KeyPassword)
	}
}

func TestResolveSigningConfigKeypassFallback(t *testing.T) {
	cfg, err := ResolveSigningConfig("my.keystore", "pass1", "myalias", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.KeyPassword != "pass1" {
		t.Errorf("expected fallback keypass pass1, got %s", cfg.KeyPassword)
	}
}

func TestResolveSigningConfigEnv(t *testing.T) {
	t.Setenv("PACKETS_ANDROID_KEYSTORE", "env.keystore")
	t.Setenv("PACKETS_ANDROID_KEYSTORE_PASSWORD", "envpass")
	t.Setenv("PACKETS_ANDROID_KEY_ALIAS", "envalias")
	t.Setenv("PACKETS_ANDROID_KEY_PASSWORD", "envkeypass")

	cfg, err := ResolveSigningConfig("", "", "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.KeystorePath != "env.keystore" {
		t.Errorf("expected env.keystore, got %s", cfg.KeystorePath)
	}
	if cfg.KeystorePassword != "envpass" {
		t.Errorf("expected envpass, got %s", cfg.KeystorePassword)
	}
	if cfg.KeyAlias != "envalias" {
		t.Errorf("expected envalias, got %s", cfg.KeyAlias)
	}
	if cfg.KeyPassword != "envkeypass" {
		t.Errorf("expected envkeypass, got %s", cfg.KeyPassword)
	}
}

func TestResolveSigningConfigMissing(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)
	t.Setenv("PACKETS_ANDROID_KEYSTORE", "")
	t.Setenv("PACKETS_ANDROID_KEYSTORE_PASSWORD", "")
	t.Setenv("PACKETS_ANDROID_KEY_ALIAS", "")
	t.Setenv("PACKETS_ANDROID_KEY_PASSWORD", "")

	_, err := ResolveSigningConfig("", "", "", "")
	if err == nil {
		t.Fatal("expected error when no keystore configured")
	}
}

func TestResolveSigningConfigMissingAlias(t *testing.T) {
	_, err := ResolveSigningConfig("some.keystore", "pass", "", "")
	if err == nil {
		t.Fatal("expected error when alias missing")
	}
}

func TestResolvePaths(t *testing.T) {
	if p := ResolveApksignerPath(); p == "" {
		t.Error("expected non-empty apksigner path")
	}
	if p := ResolveZipalignPath(); p == "" {
		t.Error("expected non-empty zipalign path")
	}
	if p := ResolveKeytoolPath(); p == "" {
		t.Error("expected non-empty keytool path")
	}
	if p := ResolveJarsignerPath(); p == "" {
		t.Error("expected non-empty jarsigner path")
	}
}

func TestGenerateKeystoreValidation(t *testing.T) {
	ctx := context.Background()
	if err := GenerateKeystore(ctx, KeystoreGenOpts{}); err == nil {
		t.Fatal("expected error for empty opts")
	}
	if err := GenerateKeystore(ctx, KeystoreGenOpts{Path: "foo"}); err == nil {
		t.Fatal("expected error for empty password")
	}
}

func TestGenerateKeystoreExecution(t *testing.T) {
	keytool := ResolveKeytoolPath()
	if _, err := os.Stat(keytool); err != nil {
		t.Skip("keytool not found on system")
	}

	tmpDir := t.TempDir()
	ksPath := filepath.Join(tmpDir, "test.keystore")
	ctx := context.Background()
	opts := KeystoreGenOpts{
		Path:         ksPath,
		Alias:        "testkey",
		Password:     "testpassword",
		ValidityDays: 1,
		DName:        "CN=Test, OU=Unit, O=Test, L=City, ST=State, C=US",
	}

	err := GenerateKeystore(ctx, opts)
	if err != nil {
		t.Fatalf("GenerateKeystore failed: %v", err)
	}

	if fi, err := os.Stat(ksPath); err != nil || fi.Size() == 0 {
		t.Fatalf("expected generated keystore file to exist and be non-empty")
	}
}

func TestSignAPKNilConfig(t *testing.T) {
	err := SignAPK(context.Background(), "app.apk", "out.apk", nil)
	if err == nil {
		t.Fatal("expected error with nil config")
	}
}

func TestSignAABNilConfig(t *testing.T) {
	err := SignAAB(context.Background(), "app.aab", nil)
	if err == nil {
		t.Fatal("expected error with nil config")
	}
}

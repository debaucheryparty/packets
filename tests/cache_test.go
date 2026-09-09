package tests

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/debaucheryparty/packets/internal/cache"
	"github.com/debaucheryparty/packets/pkg/apitypes"
)

func TestDepManager_Bind_Go(t *testing.T) {
	root := t.TempDir()
	mgr := cache.NewDepManager(root)
	wsDir := t.TempDir()

	b, err := mgr.Bind(context.Background(), "testowner", apitypes.ToolchainGo, wsDir)
	if err != nil {
		t.Fatalf("Bind Go: %v", err)
	}
	if len(b.Env) == 0 {
		t.Fatal("expected env entries for Go toolchain")
	}

	var hasGopath, hasGocache bool
	for _, e := range b.Env {
		if strings.HasPrefix(e, "GOPATH=") {
			hasGopath = true
		}
		if strings.HasPrefix(e, "GOCACHE=") {
			hasGocache = true
		}
	}
	if !hasGopath {
		t.Error("expected GOPATH env var")
	}
	if !hasGocache {
		t.Error("expected GOCACHE env var")
	}

	for _, e := range b.Env {
		parts := strings.SplitN(e, "=", 2)
		if len(parts) == 2 {
			if _, err := os.Stat(parts[1]); os.IsNotExist(err) {
				t.Errorf("cache directory %q was not created", parts[1])
			}
		}
	}
}

func TestDepManager_Bind_Rust(t *testing.T) {
	root := t.TempDir()
	mgr := cache.NewDepManager(root)
	wsDir := t.TempDir()

	b, err := mgr.Bind(context.Background(), "testowner", apitypes.ToolchainRust, wsDir)
	if err != nil {
		t.Fatalf("Bind Rust: %v", err)
	}

	var hasCargoHome, hasCargoTarget bool
	for _, e := range b.Env {
		if strings.HasPrefix(e, "CARGO_HOME=") {
			hasCargoHome = true
		}
		if strings.HasPrefix(e, "CARGO_TARGET_DIR=") {
			hasCargoTarget = true
		}
	}
	if !hasCargoHome {
		t.Error("expected CARGO_HOME env var")
	}
	if !hasCargoTarget {
		t.Error("expected CARGO_TARGET_DIR env var")
	}
}

func TestDepManager_Bind_Android(t *testing.T) {
	root := t.TempDir()
	mgr := cache.NewDepManager(root)
	wsDir := t.TempDir()

	b, err := mgr.Bind(context.Background(), "testowner", apitypes.ToolchainAndroid, wsDir)
	if err != nil {
		t.Fatalf("Bind Android: %v", err)
	}

	var hasGradleHome bool
	for _, e := range b.Env {
		if strings.HasPrefix(e, "GRADLE_USER_HOME=") {
			hasGradleHome = true
		}
	}
	if !hasGradleHome {
		t.Error("expected GRADLE_USER_HOME env var")
	}
}

func TestDepManager_Bind_Node(t *testing.T) {
	root := t.TempDir()
	mgr := cache.NewDepManager(root)
	wsDir := t.TempDir()

	b, err := mgr.Bind(context.Background(), "testowner", apitypes.ToolchainNode, wsDir)
	if err != nil {
		t.Fatalf("Bind Node: %v", err)
	}

	var hasNpmCache bool
	for _, e := range b.Env {
		if strings.HasPrefix(e, "npm_config_cache=") {
			hasNpmCache = true
		}
	}
	if !hasNpmCache {
		t.Error("expected npm_config_cache env var")
	}

	if len(b.BindMounts) == 0 {
		t.Error("expected bind-mount for node_modules")
	}
}

func TestDepManager_ApplyBindMounts(t *testing.T) {
	root := t.TempDir()
	mgr := cache.NewDepManager(root)
	wsDir := t.TempDir()

	b, err := mgr.Bind(context.Background(), "testowner", apitypes.ToolchainNode, wsDir)
	if err != nil {
		t.Fatalf("Bind Node: %v", err)
	}

	if err := b.ApplyBindMounts(wsDir); err != nil {
		t.Fatalf("ApplyBindMounts: %v", err)
	}

	for _, bm := range b.BindMounts {
		if bm.WorkspacePath == "" {
			continue
		}
		dest := filepath.Join(wsDir, bm.WorkspacePath)
		fi, err := os.Lstat(dest)
		if os.IsNotExist(err) {
			t.Errorf("expected symlink at %q to be created", dest)
			continue
		}
		if err != nil {
			t.Errorf("lstat %q: %v", dest, err)
			continue
		}
		if fi.Mode()&os.ModeSymlink == 0 {
			t.Errorf("expected symlink at %q, got %v", dest, fi.Mode())
		}
	}
}

func TestDepManager_OwnerIsolation(t *testing.T) {
	root := t.TempDir()
	mgr := cache.NewDepManager(root)
	ws := t.TempDir()

	b1, _ := mgr.Bind(context.Background(), "alice", apitypes.ToolchainGo, ws)
	b2, _ := mgr.Bind(context.Background(), "bob", apitypes.ToolchainGo, ws)

	env1 := cacheEnvMap(b1.Env)
	env2 := cacheEnvMap(b2.Env)

	if env1["GOPATH"] == env2["GOPATH"] {
		t.Errorf("alice and bob share the same GOPATH: %s", env1["GOPATH"])
	}
}

func cacheEnvMap(env []string) map[string]string {
	m := make(map[string]string, len(env))
	for _, e := range env {
		parts := strings.SplitN(e, "=", 2)
		if len(parts) == 2 {
			m[parts[0]] = parts[1]
		}
	}
	return m
}

func TestFingerprintEnv(t *testing.T) {
	fp, err := cache.FingerprintEnv(context.Background())
	if err != nil {
		t.Fatalf("FingerprintEnv: %v", err)
	}
	if fp.Hash == "" {
		t.Error("expected non-empty fingerprint hash")
	}
	if fp.Details == nil {
		t.Error("expected non-nil details map")
	}
	t.Logf("env fingerprint: %s (tools: %d detected)", fp.Hash[:8], len(fp.Details))
}

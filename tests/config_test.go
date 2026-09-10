package tests

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/debaucheryparty/packets/internal/config"
)

func TestProfileSaveAndLoad(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("USERPROFILE", tempHome)
	t.Setenv("HOME", tempHome)

	prof := &config.Profile{
		ServerAddr: "100.64.0.1:50051",
		AuthToken:  "test-secret-token-12345",
		TLSEnabled: true,
	}

	if err := config.SaveProfile(prof); err != nil {
		t.Fatalf("SaveProfile failed: %v", err)
	}

	loaded, err := config.LoadProfile()
	if err != nil {
		t.Fatalf("LoadProfile failed: %v", err)
	}

	if loaded.ServerAddr != prof.ServerAddr {
		t.Errorf("ServerAddr mismatch: got %s, want %s", loaded.ServerAddr, prof.ServerAddr)
	}
	if loaded.AuthToken != prof.AuthToken {
		t.Errorf("AuthToken mismatch: got %s, want %s", loaded.AuthToken, prof.AuthToken)
	}
	if loaded.TLSEnabled != prof.TLSEnabled {
		t.Errorf("TLSEnabled mismatch: got %v, want %v", loaded.TLSEnabled, prof.TLSEnabled)
	}
}

func TestProfileLocalOverride(t *testing.T) {
	tempDir := t.TempDir()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(wd) }()
	if err := os.Chdir(tempDir); err != nil {
		t.Fatal(err)
	}

	_ = os.MkdirAll(".packets", 0o755)
	localYAML := []byte("server_addr: \"127.0.0.1:50051\"\nauth_token: \"local-token\"\n")
	if err := os.WriteFile(filepath.Join(".packets", "config.yaml"), localYAML, 0o644); err != nil {
		t.Fatal(err)
	}

	loaded, err := config.LoadProfile()
	if err != nil {
		t.Fatalf("LoadProfile failed: %v", err)
	}

	if loaded.ServerAddr != "127.0.0.1:50051" {
		t.Errorf("expected local override server_addr 127.0.0.1:50051, got %s", loaded.ServerAddr)
	}
	if loaded.AuthToken != "local-token" {
		t.Errorf("expected local override auth_token local-token, got %s", loaded.AuthToken)
	}
}

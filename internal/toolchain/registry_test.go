package toolchain_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/debaucheryparty/packets/internal/toolchain"
	"github.com/debaucheryparty/packets/pkg/apitypes"
)

func TestRegistryLookup(t *testing.T) {
	r := toolchain.NewRegistry()

	tests := []struct {
		name    string
		tc      apitypes.Toolchain
		wantOk  bool
		backend apitypes.Backend
	}{
		{name: "rust exists", tc: apitypes.ToolchainRust, wantOk: true, backend: apitypes.BackendSccacheDist},
		{name: "go exists", tc: apitypes.ToolchainGo, wantOk: true, backend: apitypes.BackendScheduler},
		{name: "python exists", tc: apitypes.ToolchainPython, wantOk: true, backend: apitypes.BackendScheduler},
		{name: "swift exists", tc: apitypes.ToolchainSwift, wantOk: true, backend: apitypes.BackendCIProvider},
		{name: "kotlin exists", tc: apitypes.ToolchainKotlin, wantOk: true, backend: apitypes.BackendGradleCache},
		{name: "unknown missing", tc: apitypes.Toolchain("brainfuck"), wantOk: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			def, ok := r.Lookup(tt.tc)
			if ok != tt.wantOk {
				t.Fatalf("Lookup(%q) ok = %v, want %v", tt.tc, ok, tt.wantOk)
			}
			if ok && def.Backend != tt.backend {
				t.Errorf("Lookup(%q).Backend = %v, want %v", tt.tc, def.Backend, tt.backend)
			}
		})
	}
}

func TestRegistryDetect(t *testing.T) {
	tests := []struct {
		name    string
		files   []string
		dirs    []string
		wantTC  apitypes.Toolchain
		wantErr bool
	}{
		{name: "rust project", files: []string{"Cargo.toml"}, wantTC: apitypes.ToolchainRust},
		{name: "go project", files: []string{"go.mod"}, wantTC: apitypes.ToolchainGo},
		{name: "kotlin project", files: []string{"build.gradle.kts"}, wantTC: apitypes.ToolchainKotlin},
		{name: "python pyproject", files: []string{"pyproject.toml"}, wantTC: apitypes.ToolchainPython},
		{name: "node project", files: []string{"package.json"}, wantTC: apitypes.ToolchainNode},
		{name: "ruby project", files: []string{"Gemfile"}, wantTC: apitypes.ToolchainRuby},
		{name: "elixir project", files: []string{"mix.exs"}, wantTC: apitypes.ToolchainElixir},
		{name: "zig project", files: []string{"build.zig"}, wantTC: apitypes.ToolchainZig},
		{name: "swift project", files: []string{"Package.swift"}, wantTC: apitypes.ToolchainSwift},
		{name: "java maven project", files: []string{"pom.xml"}, wantTC: apitypes.ToolchainJava},
		{name: "scala project", files: []string{"build.sbt"}, wantTC: apitypes.ToolchainScala},
		{name: "dotnet project", files: []string{"global.json"}, wantTC: apitypes.ToolchainDotNet},
		{name: "empty dir fails", wantErr: true},
		{
			name:    "ambiguous across backends fails",
			files:   []string{"go.mod", "Package.swift"},
			wantErr: true,
		},
		{
			name:   "same backend picks first",
			files:  []string{"pom.xml", "build.gradle"},
			wantTC: apitypes.ToolchainJava,
		},
	}

	r := toolchain.NewRegistry()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			for _, f := range tt.files {
				createFile(t, filepath.Join(dir, f))
			}
			for _, d := range tt.dirs {
				createDir(t, filepath.Join(dir, d))
			}

			def, err := r.Detect(dir)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got toolchain %q", def.Name)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if def.Name != tt.wantTC {
				t.Errorf("detected %q, want %q", def.Name, tt.wantTC)
			}
		})
	}
}

func TestRegistryDetectFlutterVsDart(t *testing.T) {
	r := toolchain.NewRegistry()

	t.Run("flutter with ios dir", func(t *testing.T) {
		dir := t.TempDir()
		createFile(t, filepath.Join(dir, "pubspec.yaml"))
		createDir(t, filepath.Join(dir, "ios"))

		def, err := r.Detect(dir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if def.Name != apitypes.ToolchainFlutter {
			t.Errorf("expected flutter, got %q", def.Name)
		}
	})

	t.Run("dart without ios/android", func(t *testing.T) {
		dir := t.TempDir()
		createFile(t, filepath.Join(dir, "pubspec.yaml"))

		def, err := r.Detect(dir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if def.Name != apitypes.ToolchainDart {
			t.Errorf("expected dart, got %q", def.Name)
		}
	})
}

func TestRegistryCustomToolchain(t *testing.T) {
	r := toolchain.NewEmptyRegistry()
	r.Register(apitypes.ToolchainDef{
		Name:         "cobol",
		DisplayName:  "COBOL",
		DetectFiles:  []string{"cobol.config"},
		Backend:      apitypes.BackendScheduler,
		LocalCommand: "cobc",
		DefaultArgs:  []string{"-x"},
		DockerImage:  "cobol:latest",
	})

	def, ok := r.Lookup("cobol")
	if !ok {
		t.Fatal("custom toolchain not found after register")
	}
	if def.LocalCommand != "cobc" {
		t.Errorf("LocalCommand = %q, want cobc", def.LocalCommand)
	}

	dir := t.TempDir()
	createFile(t, filepath.Join(dir, "cobol.config"))

	detected, err := r.Detect(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if detected.Name != "cobol" {
		t.Errorf("detected %q, want cobol", detected.Name)
	}
}

func TestRegistryAll(t *testing.T) {
	r := toolchain.NewRegistry()
	all := r.All()
	if len(all) < 30 {
		t.Errorf("expected at least 30 built-in toolchains, got %d", len(all))
	}
}

func createFile(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte{}, 0o644); err != nil {
		t.Fatal(err)
	}
}

func createDir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
}

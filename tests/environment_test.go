package tests

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/debaucheryparty/packets/internal/environment"
	pb "github.com/debaucheryparty/packets/proto/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
)

func TestDetector_PolyglotDetection(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "packets-polyglot-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	androidDir := filepath.Join(tempDir, "android")
	_ = os.MkdirAll(filepath.Join(androidDir, "app"), 0o755)
	_ = os.WriteFile(filepath.Join(androidDir, "gradlew"), []byte("#!/bin/sh"), 0o755)
	_ = os.WriteFile(filepath.Join(androidDir, "settings.gradle.kts"), []byte("rootProject.name = \"demo\""), 0o644)
	_ = os.WriteFile(filepath.Join(androidDir, "app", "build.gradle.kts"), []byte("android { compileSdk = 35 }"), 0o644)

	rustDir := filepath.Join(tempDir, "backend")
	_ = os.MkdirAll(rustDir, 0o755)
	_ = os.WriteFile(filepath.Join(rustDir, "Cargo.toml"), []byte("[package]\nname = \"srv\"\nversion = \"0.1.0\""), 0o644)

	zephyrDir := filepath.Join(tempDir, "firmware")
	_ = os.MkdirAll(zephyrDir, 0o755)
	_ = os.WriteFile(filepath.Join(zephyrDir, "west.yml"), []byte("manifest:\n  version: '0.14'"), 0o644)
	_ = os.WriteFile(filepath.Join(zephyrDir, "prj.conf"), []byte("CONFIG_LOG=y"), 0o644)

	pyDir := filepath.Join(tempDir, "service")
	_ = os.MkdirAll(pyDir, 0o755)
	_ = os.WriteFile(filepath.Join(pyDir, "pyproject.toml"), []byte("[tool.poetry]\nname = \"service\""), 0o644)

	mgr := environment.NewManager()
	topo, err := mgr.Detect(tempDir)
	if err != nil {
		t.Fatalf("Detect failed: %v", err)
	}

	if !topo.IsHybrid {
		t.Errorf("expected IsHybrid to be true for multi-component project")
	}

	foundAndroid, foundRust, foundZephyr, foundPython := false, false, false, false
	for _, c := range topo.Components {
		switch c.Type {
		case environment.ComponentAndroid:
			foundAndroid = true
			if c.Metadata["compile_sdk"] != "35" {
				t.Errorf("expected compile_sdk 35, got %q", c.Metadata["compile_sdk"])
			}
		case environment.ComponentRust:
			foundRust = true
		case environment.ComponentZephyr:
			foundZephyr = true
		case environment.ComponentPython:
			foundPython = true
			if c.Metadata["package_manager"] != "poetry" {
				t.Errorf("expected package_manager poetry, got %q", c.Metadata["package_manager"])
			}
		}
	}

	if !foundAndroid {
		t.Errorf("expected to detect Android component")
	}
	if !foundRust {
		t.Errorf("expected to detect Rust component")
	}
	if !foundZephyr {
		t.Errorf("expected to detect Zephyr component")
	}
	if !foundPython {
		t.Errorf("expected to detect Python component")
	}

	report, err := mgr.Check(context.Background(), tempDir)
	if err != nil {
		t.Fatalf("Check failed: %v", err)
	}

	if len(report.Components) < 4 {
		t.Errorf("expected at least 4 components in report, got %d", len(report.Components))
	}
}

func TestDetector_PythonVariants(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "packets-python-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	_ = os.WriteFile(filepath.Join(tempDir, "requirements.txt"), []byte("fastapi\nuvicorn\n"), 0o644)

	mgr := environment.NewManager()
	topo, err := mgr.Detect(tempDir)
	if err != nil {
		t.Fatalf("Detect failed: %v", err)
	}

	if len(topo.Components) != 1 {
		t.Fatalf("expected 1 component, got %d", len(topo.Components))
	}
	c := topo.Components[0]
	if c.Type != environment.ComponentPython {
		t.Errorf("expected ComponentPython, got %s", c.Type)
	}
	if c.Metadata["package_manager"] != "pip" {
		t.Errorf("expected pip, got %s", c.Metadata["package_manager"])
	}
}

func TestDetector_AdditionalLanguages(t *testing.T) {
	cases := []struct {
		name         string
		files        map[string]string
		expectedType environment.ComponentType
		metaCheck    func(map[string]string) bool
	}{
		{
			name: "Java Maven",
			files: map[string]string{
				"pom.xml": "<project></project>",
			},
			expectedType: environment.ComponentJava,
			metaCheck: func(m map[string]string) bool {
				return m["build_system"] == "maven"
			},
		},
		{
			name: "Java Gradle",
			files: map[string]string{
				"build.gradle": "plugins { id 'java' }",
			},
			expectedType: environment.ComponentJava,
			metaCheck: func(m map[string]string) bool {
				return m["build_system"] == "gradle"
			},
		},
		{
			name: "Swift Package",
			files: map[string]string{
				"Package.swift": "// swift-tools-version: 5.9",
			},
			expectedType: environment.ComponentSwift,
		},
		{
			name: "Ruby Gemfile",
			files: map[string]string{
				"Gemfile": "source 'https://rubygems.org'",
			},
			expectedType: environment.ComponentRuby,
		},
		{
			name: "PHP Composer",
			files: map[string]string{
				"composer.json": `{"name": "test/app"}`,
			},
			expectedType: environment.ComponentPHP,
		},
		{
			name: "Zig build",
			files: map[string]string{
				"build.zig": "const std = @import(\"std\");",
			},
			expectedType: environment.ComponentZig,
		},
		{
			name: ".NET C# project",
			files: map[string]string{
				"App.csproj": "<Project Sdk=\"Microsoft.NET.Sdk\"></Project>",
			},
			expectedType: environment.ComponentDotNet,
		},
		{
			name: "Flutter project",
			files: map[string]string{
				"pubspec.yaml": "name: flutter_app\ndependencies:\n  flutter:\n    sdk: flutter\n",
			},
			expectedType: environment.ComponentFlutter,
		},
		{
			name: "Dart package",
			files: map[string]string{
				"pubspec.yaml": "name: dart_pkg\nversion: 1.0.0\n",
			},
			expectedType: environment.ComponentDart,
		},
		{
			name: "Elixir Mix",
			files: map[string]string{
				"mix.exs": "defmodule MyApp.MixProject do\nend",
			},
			expectedType: environment.ComponentElixir,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tempDir, err := os.MkdirTemp("", "packets-lang-test-*")
			if err != nil {
				t.Fatal(err)
			}
			defer os.RemoveAll(tempDir)

			for p, content := range tc.files {
				target := filepath.Join(tempDir, p)
				_ = os.MkdirAll(filepath.Dir(target), 0o755)
				_ = os.WriteFile(target, []byte(content), 0o644)
			}

			mgr := environment.NewManager()
			topo, err := mgr.Detect(tempDir)
			if err != nil {
				t.Fatalf("Detect failed: %v", err)
			}

			if len(topo.Components) == 0 {
				t.Fatalf("expected at least 1 component detected")
			}

			found := false
			for _, c := range topo.Components {
				if c.Type == tc.expectedType {
					found = true
					if tc.metaCheck != nil && !tc.metaCheck(c.Metadata) {
						t.Errorf("metaCheck failed for %s: %+v", tc.name, c.Metadata)
					}
				}
			}
			if !found {
				t.Errorf("expected component type %s not found in %+v", tc.expectedType, topo.Components)
			}
		})
	}
}

func setupTestGRPCServer(t *testing.T) (*environment.RemoteClient, func()) {
	lis := bufconn.Listen(1024 * 1024)
	s := grpc.NewServer()
	envServer := environment.NewServer(environment.NewManager())
	pb.RegisterEnvironmentServer(s, envServer)

	go func() {
		_ = s.Serve(lis)
	}()

	dialer := func(context.Context, string) (net.Conn, error) {
		return lis.Dial()
	}

	conn, err := grpc.DialContext(
		context.Background(),
		"passthrough://bufnet",
		grpc.WithContextDialer(dialer),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("Failed to dial bufnet: %v", err)
	}

	client := environment.NewRemoteClient(conn)
	cleanup := func() {
		_ = conn.Close()
		s.Stop()
		_ = lis.Close()
	}

	return client, cleanup
}

func TestRemoteEnvironment_Check(t *testing.T) {
	client, cleanup := setupTestGRPCServer(t)
	defer cleanup()

	ctx := context.Background()
	comps := []environment.Component{
		{
			Type: environment.ComponentGo,
			Path: ".",
		},
	}

	report, err := client.Check(ctx, "proj-123", ".", comps)
	if err != nil {
		t.Fatalf("Remote Check failed: %v", err)
	}

	if report == nil {
		t.Fatal("expected report to not be nil")
	}

	if _, ok := report.Components[environment.ComponentGo]; !ok {
		t.Errorf("expected Go component in report")
	}
}

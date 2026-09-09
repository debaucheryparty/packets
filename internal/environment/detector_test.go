package environment

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestDetector_PolyglotDetection(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "packets-polyglot-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	// Create subproject 1: Android in android/
	androidDir := filepath.Join(tempDir, "android")
	_ = os.MkdirAll(filepath.Join(androidDir, "app"), 0o755)
	_ = os.WriteFile(filepath.Join(androidDir, "gradlew"), []byte("#!/bin/sh"), 0o755)
	_ = os.WriteFile(filepath.Join(androidDir, "settings.gradle.kts"), []byte("rootProject.name = \"demo\""), 0o644)
	_ = os.WriteFile(filepath.Join(androidDir, "app", "build.gradle.kts"), []byte("android { compileSdk = 35 }"), 0o644)

	// Create subproject 2: Rust in backend/
	rustDir := filepath.Join(tempDir, "backend")
	_ = os.MkdirAll(rustDir, 0o755)
	_ = os.WriteFile(filepath.Join(rustDir, "Cargo.toml"), []byte("[package]\nname = \"srv\"\nversion = \"0.1.0\""), 0o644)

	// Create subproject 3: Zephyr firmware in firmware/
	zephyrDir := filepath.Join(tempDir, "firmware")
	_ = os.MkdirAll(zephyrDir, 0o755)
	_ = os.WriteFile(filepath.Join(zephyrDir, "west.yml"), []byte("manifest:\n  version: '0.14'"), 0o644)
	_ = os.WriteFile(filepath.Join(zephyrDir, "prj.conf"), []byte("CONFIG_LOG=y"), 0o644)

	// Create subproject 4: Python service in service/
	pyDir := filepath.Join(tempDir, "service")
	_ = os.MkdirAll(pyDir, 0o755)
	_ = os.WriteFile(filepath.Join(pyDir, "pyproject.toml"), []byte("[tool.poetry]\nname = \"service\""), 0o644)

	mgr := NewManager()
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
		case ComponentAndroid:
			foundAndroid = true
			if c.Metadata["compile_sdk"] != "35" {
				t.Errorf("expected compile_sdk 35, got %q", c.Metadata["compile_sdk"])
			}
		case ComponentRust:
			foundRust = true
		case ComponentZephyr:
			foundZephyr = true
		case ComponentPython:
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

	mgr := NewManager()
	topo, err := mgr.Detect(tempDir)
	if err != nil {
		t.Fatalf("Detect failed: %v", err)
	}

	if len(topo.Components) != 1 {
		t.Fatalf("expected 1 component, got %d", len(topo.Components))
	}
	c := topo.Components[0]
	if c.Type != ComponentPython {
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
		expectedType ComponentType
		metaCheck    func(map[string]string) bool
	}{
		{
			name: "Java Maven",
			files: map[string]string{
				"pom.xml": "<project></project>",
			},
			expectedType: ComponentJava,
			metaCheck: func(m map[string]string) bool {
				return m["build_system"] == "maven"
			},
		},
		{
			name: "Java Gradle",
			files: map[string]string{
				"build.gradle": "plugins { id 'java' }",
			},
			expectedType: ComponentJava,
			metaCheck: func(m map[string]string) bool {
				return m["build_system"] == "gradle"
			},
		},
		{
			name: "Swift Package",
			files: map[string]string{
				"Package.swift": "// swift-tools-version: 5.9",
			},
			expectedType: ComponentSwift,
		},
		{
			name: "Ruby Gemfile",
			files: map[string]string{
				"Gemfile": "source 'https://rubygems.org'",
			},
			expectedType: ComponentRuby,
		},
		{
			name: "PHP Composer",
			files: map[string]string{
				"composer.json": `{"name": "test/app"}`,
			},
			expectedType: ComponentPHP,
		},
		{
			name: "Zig build",
			files: map[string]string{
				"build.zig": "const std = @import(\"std\");",
			},
			expectedType: ComponentZig,
		},
		{
			name: ".NET C# project",
			files: map[string]string{
				"App.csproj": "<Project Sdk=\"Microsoft.NET.Sdk\"></Project>",
			},
			expectedType: ComponentDotNet,
		},
		{
			name: "Flutter project",
			files: map[string]string{
				"pubspec.yaml": "name: flutter_app\ndependencies:\n  flutter:\n    sdk: flutter\n",
			},
			expectedType: ComponentFlutter,
		},
		{
			name: "Dart package",
			files: map[string]string{
				"pubspec.yaml": "name: dart_pkg\nversion: 1.0.0\n",
			},
			expectedType: ComponentDart,
		},
		{
			name: "Elixir Mix",
			files: map[string]string{
				"mix.exs": "defmodule MyApp.MixProject do\nend",
			},
			expectedType: ComponentElixir,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			for f, content := range tc.files {
				p := filepath.Join(dir, f)
				_ = os.MkdirAll(filepath.Dir(p), 0o755)
				_ = os.WriteFile(p, []byte(content), 0o644)
			}

			mgr := NewManager()
			topo, err := mgr.Detect(dir)
			if err != nil {
				t.Fatalf("Detect error: %v", err)
			}
			if len(topo.Components) == 0 {
				t.Fatalf("Expected at least 1 component, found none")
			}

			var matched *Component
			for _, comp := range topo.Components {
				if comp.Type == tc.expectedType {
					c := comp
					matched = &c
					break
				}
			}

			if matched == nil {
				t.Fatalf("Expected component %s not detected in %v", tc.expectedType, topo.Components)
			}

			if tc.metaCheck != nil && !tc.metaCheck(matched.Metadata) {
				t.Errorf("Metadata check failed for component: %+v", matched.Metadata)
			}

			// Verify CheckComponents generates toolchain requirements
			report, err := mgr.CheckComponents(context.Background(), []Component{*matched})
			if err != nil {
				t.Fatalf("CheckComponents failed: %v", err)
			}
			reqs := report.Components[tc.expectedType]
			if len(reqs) == 0 {
				t.Errorf("Expected toolchain requirements for %s, got 0", tc.expectedType)
			}
			for _, r := range reqs {
				if !r.CanPrepare || r.PrepareCmd == "" {
					t.Errorf("Requirement %s for %s is missing CanPrepare or PrepareCmd", r.Name, tc.expectedType)
				}
			}
		})
	}
}

func TestDetector_ManualOverrides(t *testing.T) {
	dir := t.TempDir()

	// 1. Create a go.mod file (heuristic would normally detect Go)
	_ = os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module testapp\n\ngo 1.22\n"), 0o644)

	mgr := NewManager()
	topo, err := mgr.Detect(dir)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if len(topo.Components) != 1 || topo.Components[0].Type != ComponentGo {
		t.Fatalf("Expected auto-detected Go, got: %+v", topo.Components)
	}

	// 2. Set manual overrides in .packets/project.json
	dotPackets := filepath.Join(dir, ".packets")
	_ = os.MkdirAll(dotPackets, 0o755)
	cfgContent := `{
  "project_id": "testapp",
  "components": [
    {"type": "rust", "path": "native"},
    {"type": "android", "path": "mobile"}
  ]
}`
	_ = os.WriteFile(filepath.Join(dotPackets, "project.json"), []byte(cfgContent), 0o644)

	// 3. Detect again - should use manual components, bypassing Go
	topoManual, err := mgr.Detect(dir)
	if err != nil {
		t.Fatalf("Detect manual: %v", err)
	}
	if len(topoManual.Components) != 2 {
		t.Fatalf("Expected 2 manual components, got %d", len(topoManual.Components))
	}
	for _, c := range topoManual.Components {
		if c.Confidence != "manual" {
			t.Errorf("Expected confidence 'manual', got %q for %s", c.Confidence, c.Type)
		}
	}

	// 4. Reset manual components
	resetCfg := `{"project_id": "testapp", "components": []}`
	_ = os.WriteFile(filepath.Join(dotPackets, "project.json"), []byte(resetCfg), 0o644)

	// 5. Detect again - should restore Go auto-detection
	topoRestored, err := mgr.Detect(dir)
	if err != nil {
		t.Fatalf("Detect restored: %v", err)
	}
	if len(topoRestored.Components) != 1 || topoRestored.Components[0].Type != ComponentGo {
		t.Fatalf("Expected restored Go auto-detection, got: %+v", topoRestored.Components)
	}
}



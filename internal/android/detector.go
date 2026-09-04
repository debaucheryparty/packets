package android

import (
	"fmt"
	"os"
	"path/filepath"
)

type ProjectInfo struct {
	Root          string
	HasGradlew    bool
	HasAppDir     bool
	HasManifest   bool
	HasSettingsKt bool
	HasSettingsGr bool
	GradlewPath   string
}

func (p *ProjectInfo) IsValid() bool {
	return p.HasGradlew && p.HasAppDir
}

func (p *ProjectInfo) Confidence() string {
	score := 0
	if p.HasGradlew {
		score++
	}
	if p.HasAppDir {
		score++
	}
	if p.HasManifest {
		score++
	}
	if p.HasSettingsKt || p.HasSettingsGr {
		score++
	}
	switch {
	case score >= 4:
		return "high"
	case score >= 2:
		return "medium"
	default:
		return "low"
	}
}

func DetectProject(dir string) (*ProjectInfo, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("android.DetectProject abs path: %w", err)
	}

	info := &ProjectInfo{Root: abs}

	gradlew := filepath.Join(abs, "gradlew")
	if fi, err := os.Stat(gradlew); err == nil && !fi.IsDir() {
		info.HasGradlew = true
		info.GradlewPath = gradlew
	}

	appDir := filepath.Join(abs, "app")
	if fi, err := os.Stat(appDir); err == nil && fi.IsDir() {
		info.HasAppDir = true
	}
	manifestPaths := []string{
		filepath.Join(abs, "app", "src", "main", "AndroidManifest.xml"),
		filepath.Join(abs, "AndroidManifest.xml"),
	}
	for _, mp := range manifestPaths {
		if fi, err := os.Stat(mp); err == nil && !fi.IsDir() {
			info.HasManifest = true
			break
		}
	}

	if fi, err := os.Stat(filepath.Join(abs, "settings.gradle.kts")); err == nil && !fi.IsDir() {
		info.HasSettingsKt = true
	}
	if fi, err := os.Stat(filepath.Join(abs, "settings.gradle")); err == nil && !fi.IsDir() {
		info.HasSettingsGr = true
	}

	return info, nil
}

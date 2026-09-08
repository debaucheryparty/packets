package workspace

import (
	"os"
	"path/filepath"
	"strings"
)

var defaultIgnorePatterns = []string{
	".git/",
	"node_modules/",
	"target/",
	"dist/",
	"bin/",
	"build/",
	"__pycache__/",
	".venv/",
	".gradle/",
	".west/",
	".idea/",
	".vscode/",
	"*.exe",
	"*.dll",
	"*.so",
	"*.dylib",
	".env",
	".env.local",
	".packets/",
	".DS_Store",
}

func ParseIgnoreFile(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var patterns []string
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		norm := strings.ReplaceAll(line, "\\", "/")
		patterns = append(patterns, norm)
	}
	return patterns, nil
}

func ShouldIgnore(path string, extra []string) bool {
	norm := strings.ReplaceAll(path, "\\", "/")
	all := append(defaultIgnorePatterns, extra...)
	for _, pat := range all {
		pat = strings.TrimPrefix(pat, "/")
		if pat == "" {
			continue
		}
		if strings.HasSuffix(pat, "/") {
			dir := strings.TrimSuffix(pat, "/")
			if strings.HasPrefix(norm, dir+"/") || norm == dir {
				return true
			}
			segments := strings.Split(norm, "/")
			for _, seg := range segments {
				if seg == dir {
					return true
				}
			}
			continue
		}
		if strings.Contains(pat, "/") {
			matched, _ := filepath.Match(pat, norm)
			if matched {
				return true
			}
		} else {
			base := filepath.Base(norm)
			matched, _ := filepath.Match(pat, base)
			if matched {
				return true
			}
		}
	}
	return false
}

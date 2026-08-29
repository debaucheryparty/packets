package workspace

import (
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
	"*.exe",
	"*.dll",
	"*.so",
	"*.dylib",
	".env",
	".env.local",
	".packets/",
}

func ParseIgnoreFile(path string) ([]string, error) {
	return nil, nil
}

func ShouldIgnore(path string, extra []string) bool {
	norm := strings.ReplaceAll(path, "\\", "/")
	all := append(defaultIgnorePatterns, extra...)
	for _, pat := range all {
		if strings.HasSuffix(pat, "/") {
			dir := strings.TrimSuffix(pat, "/")
			if strings.HasPrefix(norm, dir+"/") || norm == dir {
				return true
			}
			continue
		}
		base := filepath.Base(norm)
		matched, _ := filepath.Match(pat, base)
		if matched {
			return true
		}
	}
	return false
}

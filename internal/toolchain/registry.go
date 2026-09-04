package toolchain

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/debaucheryparty/packets/pkg/apitypes"
)

type Registry struct {
	toolchains []apitypes.ToolchainDef
	lookupMap  map[apitypes.Toolchain]int
}

func NewEmptyRegistry() *Registry {
	return &Registry{
		toolchains: make([]apitypes.ToolchainDef, 0),
		lookupMap:  make(map[apitypes.Toolchain]int),
	}
}

func NewRegistry() *Registry {
	r := NewEmptyRegistry()
	registerBuiltins(r)
	return r
}

func (r *Registry) Register(def apitypes.ToolchainDef) {
	if _, exists := r.lookupMap[def.Name]; exists {
		// overwrite existing toolchain definition
		idx := r.lookupMap[def.Name]
		r.toolchains[idx] = def
		return
	}

	r.toolchains = append(r.toolchains, def)
	r.lookupMap[def.Name] = len(r.toolchains) - 1
}

func (r *Registry) Lookup(tc apitypes.Toolchain) (apitypes.ToolchainDef, bool) {
	idx, ok := r.lookupMap[tc]
	if !ok {
		return apitypes.ToolchainDef{}, false
	}
	return r.toolchains[idx], true
}

func (r *Registry) All() []apitypes.ToolchainDef {
	cpy := make([]apitypes.ToolchainDef, len(r.toolchains))
	copy(cpy, r.toolchains)
	return cpy
}

func (r *Registry) DetectAll(dir string) []apitypes.ToolchainDef {
	var matches []apitypes.ToolchainDef
	highestScore := 0

	for _, def := range r.toolchains {
		matchedFile := true
		if len(def.DetectFiles) > 0 {
			matchedFile = false
			for _, f := range def.DetectFiles {
				path := filepath.Join(dir, f)
				if _, err := os.Stat(path); err == nil {
					matchedFile = true
					break
				}
			}
		}

		matchedDir := true
		if len(def.DetectDirs) > 0 {
			matchedDir = false
			for _, d := range def.DetectDirs {
				paths, err := filepath.Glob(filepath.Join(dir, d))
				if err == nil && len(paths) > 0 {
					for _, p := range paths {
						if info, statErr := os.Stat(p); statErr == nil && info.IsDir() {
							matchedDir = true
							break
						}
					}
				}
				if matchedDir {
					break
				}
			}
		}

		hasCriteria := len(def.DetectFiles) > 0 || len(def.DetectDirs) > 0
		if hasCriteria && matchedFile && matchedDir {
			score := 0
			if len(def.DetectFiles) > 0 {
				score++
			}
			if len(def.DetectDirs) > 0 {
				score++
			}

			if score > highestScore {
				highestScore = score
				matches = []apitypes.ToolchainDef{def}
			} else if score == highestScore {
				matches = append(matches, def)
			}
		}
	}

	return matches
}

func (r *Registry) Detect(dir string) (apitypes.ToolchainDef, error) {
	matches := r.DetectAll(dir)
	if len(matches) == 0 {
		return apitypes.ToolchainDef{}, fmt.Errorf("detect in %q: no toolchain matched", dir)
	}

	if len(matches) > 1 {
		firstBackend := matches[0].Backend
		for i := 1; i < len(matches); i++ {
			if matches[i].Backend != firstBackend {
				return apitypes.ToolchainDef{}, fmt.Errorf("detect in %q: multiple toolchains detected with different backends: %s (%s) vs %s (%s)",
					dir, matches[0].Name, matches[0].Backend, matches[i].Name, matches[i].Backend)
			}
		}
		// same backend, just take the first one since it's highest priority
		return matches[0], nil
	}

	return matches[0], nil
}

package shim

import (
	"fmt"
	"path/filepath"

	"github.com/waris4ly/packets/internal/toolchain"
	"github.com/waris4ly/packets/pkg/apitypes"
)

type Detector struct {
	registry *toolchain.Registry
}

func NewDetector(registry *toolchain.Registry) *Detector {
	return &Detector{registry: registry}
}

func (d *Detector) DetectToolchain(dir string) (apitypes.ToolchainDef, error) {
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return apitypes.ToolchainDef{}, fmt.Errorf("DetectToolchain abs path: %w", err)
	}

	def, err := d.registry.Detect(absDir)
	if err != nil {
		return apitypes.ToolchainDef{}, fmt.Errorf("DetectToolchain in %q: %w", absDir, err)
	}

	return def, nil
}

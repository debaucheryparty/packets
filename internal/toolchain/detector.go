package toolchain

import (
	"fmt"
	"path/filepath"

	"github.com/debaucheryparty/packets/pkg/apitypes"
)

// Detector wraps the registry to detect the toolchain for a directory.
type Detector struct {
	registry *Registry
}

func NewDetector(registry *Registry) *Detector {
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

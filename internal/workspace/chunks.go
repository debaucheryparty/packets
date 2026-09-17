package workspace

import (
	"os"
	"path/filepath"

	"github.com/debaucheryparty/packets/pkg/apitypes"
)

func buildHashIndex(manifest *apitypes.WorkspaceManifest) map[string]string {
	idx := make(map[string]string, len(manifest.Files))
	for _, f := range manifest.Files {
		if f.Hash != "" {
			idx[f.Hash] = f.Path
		}
	}
	return idx
}

func readChunkByHash(workspaceDir string, manifest *apitypes.WorkspaceManifest, hash string) ([]byte, error) {
	idx := buildHashIndex(manifest)
	relPath, ok := idx[hash]
	if !ok {
		return nil, nil
	}
	absPath := filepath.Join(workspaceDir, filepath.FromSlash(relPath))
	return os.ReadFile(absPath)
}

func ReadChunkByHash(workspaceDir string, manifest *apitypes.WorkspaceManifest, hash string) ([]byte, error) {
	return readChunkByHash(workspaceDir, manifest, hash)
}

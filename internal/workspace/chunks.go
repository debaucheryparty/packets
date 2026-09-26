package workspace

import (
	"bytes"
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
	return readChunkByHashFromIndex(workspaceDir, idx, hash)
}

func readChunkByHashFromIndex(workspaceDir string, idx map[string]string, hash string) ([]byte, error) {
	relPath, ok := idx[hash]
	if !ok {
		return nil, nil
	}
	absPath := filepath.Join(workspaceDir, filepath.FromSlash(relPath))
	data, err := os.ReadFile(absPath)
	if err != nil {
		return nil, err
	}
	if isShellScript(absPath) && bytes.Contains(data, []byte("\r\n")) {
		data = bytes.ReplaceAll(data, []byte("\r\n"), []byte("\n"))
	}
	return data, nil
}

func ReadChunkByHash(workspaceDir string, manifest *apitypes.WorkspaceManifest, hash string) ([]byte, error) {
	return readChunkByHash(workspaceDir, manifest, hash)
}

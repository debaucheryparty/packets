package workspace

import (
	"io"
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

func readChunkByHashReader(workspaceDir string, manifest *apitypes.WorkspaceManifest, hash string) (io.ReadCloser, int64, error) {
	idx := buildHashIndex(manifest)
	relPath, ok := idx[hash]
	if !ok {
		return nil, 0, nil
	}
	absPath := filepath.Join(workspaceDir, filepath.FromSlash(relPath))
	f, err := os.Open(absPath)
	if err != nil {
		return nil, 0, err
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, 0, err
	}
	return f, info.Size(), nil
}

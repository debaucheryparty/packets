package workspace

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

type manifestCacheEntry struct {
	Path  string    `json:"path"`
	Hash  string    `json:"hash"`
	Size  int64     `json:"size"`
	Mtime time.Time `json:"mtime"`
}

type localManifestCache struct {
	RootHash    string                        `json:"root_hash"`
	Files       map[string]manifestCacheEntry `json:"files"`
	SnapshotRef string                        `json:"snapshot_ref"`
	UploadedAt  time.Time                     `json:"uploaded_at"`
}

func cacheDir(workspaceDir string) string {
	return filepath.Join(workspaceDir, ".packets")
}

func cachePath(workspaceDir string) string {
	return filepath.Join(cacheDir(workspaceDir), "manifest.json")
}

func loadLocalCache(workspaceDir string) (*localManifestCache, error) {
	data, err := os.ReadFile(cachePath(workspaceDir))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var c localManifestCache
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, nil
	}
	return &c, nil
}

func saveLocalCache(workspaceDir string, c *localManifestCache) error {
	if err := os.MkdirAll(cacheDir(workspaceDir), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(cachePath(workspaceDir), data, 0644)
}

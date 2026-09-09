package workspace

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/debaucheryparty/packets/internal/storage"
	"github.com/debaucheryparty/packets/pkg/apitypes"
)

func ExtractSnapshot(ctx context.Context, store storage.ObjectStore, owner, snapshotRef, targetDir string) error {
	manifestKey := fmt.Sprintf("%s/manifests/%s.json", owner, snapshotRef)
	r, err := store.Download(ctx, manifestKey)
	if err != nil {
		return fmt.Errorf("ExtractSnapshot download manifest: %w", err)
	}
	defer r.Close() //nolint:errcheck

	var manifest apitypes.WorkspaceManifest
	if err := json.NewDecoder(r).Decode(&manifest); err != nil {
		return fmt.Errorf("ExtractSnapshot decode manifest: %w", err)
	}

	manifestMarkerFile := filepath.Join(targetDir, ".packets_manifest.json")

	// Deletion detection: compare against previous manifest if present.
	// This is read-only; we never update it until extraction fully succeeds.
	oldManifestFiles := make(map[string]string) // path -> hash
	if oldData, err := os.ReadFile(manifestMarkerFile); err == nil {
		var oldManifest apitypes.WorkspaceManifest
		if err := json.Unmarshal(oldData, &oldManifest); err == nil {
			for _, f := range oldManifest.Files {
				if !f.IsDir {
					oldManifestFiles[filepath.Clean(f.Path)] = f.Hash
				}
			}
		}
	}

	newFilesMap := make(map[string]bool, len(manifest.Files))
	for _, f := range manifest.Files {
		newFilesMap[filepath.Clean(f.Path)] = true
	}

	// Remove files that existed in the previous manifest but are absent in the new one.
	for oldPath := range oldManifestFiles {
		if !newFilesMap[oldPath] {
			delPath := filepath.Join(targetDir, filepath.FromSlash(oldPath))
			_ = os.Chmod(delPath, 0o666) // ensure writable on Windows
			_ = os.Remove(delPath)

			// Prune empty parent directories up to (but not including) targetDir.
			parent := filepath.Dir(delPath)
			for parent != targetDir && strings.HasPrefix(parent, targetDir) {
				entries, err := os.ReadDir(parent)
				if err != nil || len(entries) > 0 {
					break
				}
				_ = os.Remove(parent)
				parent = filepath.Dir(parent)
			}
		}
	}

	// Extract all files from the new snapshot.
	for _, f := range manifest.Files {
		if err := validatePath(f.Path); err != nil {
			return err
		}

		destPath := filepath.Join(targetDir, filepath.FromSlash(f.Path))

		if f.IsDir {
			// If a regular file previously existed at this path, remove it first.
			if fi, err := os.Lstat(destPath); err == nil && !fi.IsDir() {
				_ = os.Remove(destPath)
			}
			if err := os.MkdirAll(destPath, os.FileMode(f.Mode)|0o700); err != nil {
				return fmt.Errorf("ExtractSnapshot mkdir %s: %w", f.Path, err)
			}
			continue
		}

		if f.Link != "" {
			_ = os.RemoveAll(destPath)
			if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
				return fmt.Errorf("ExtractSnapshot mkdir for symlink %s: %w", f.Path, err)
			}
			if err := os.Symlink(f.Link, destPath); err != nil && !os.IsExist(err) {
				return fmt.Errorf("ExtractSnapshot symlink %s: %w", f.Path, err)
			}
			continue
		}

		// If a directory previously existed at this path, remove it first.
		if fi, err := os.Lstat(destPath); err == nil && fi.IsDir() {
			_ = os.RemoveAll(destPath)
		}

		// Skip downloading if the identical file is already on disk.
		cleanedPath := filepath.Clean(f.Path)
		if prevHash, exists := oldManifestFiles[cleanedPath]; exists && prevHash == f.Hash {
			if fi, err := os.Stat(destPath); err == nil && fi.Size() == f.Size {
				continue
			}
		}

		chunkKey := fmt.Sprintf("%s/chunks/%s", owner, f.Hash)
		cr, cerr := store.Download(ctx, chunkKey)
		if cerr != nil {
			return fmt.Errorf("ExtractSnapshot download chunk %s: %w", f.Hash[:8], cerr)
		}

		if err := writeFile(destPath, os.FileMode(f.Mode), cr); err != nil {
			cr.Close() //nolint:errcheck
			return fmt.Errorf("ExtractSnapshot write %s: %w", f.Path, err)
		}
		cr.Close() //nolint:errcheck
	}

	// Post-extraction integrity verification: re-scan the target directory and confirm
	// the resulting RootHash matches the expected snapshot RootHash. This catches
	// corrupted chunks, incomplete downloads, or filesystem issues.
	if err := verifyExtractedRootHash(targetDir, manifest.RootHash, manifestMarkerFile); err != nil {
		return err
	}

	// Only write the manifest marker once we have verified the extraction is complete
	// and correct. This prevents a partially-extracted workspace from being treated as
	// a valid baseline for the next incremental sync.
	if mBytes, err := json.Marshal(manifest); err == nil {
		tmpFile := manifestMarkerFile + ".tmp"
		if writeErr := os.WriteFile(tmpFile, mBytes, 0o644); writeErr == nil {
			_ = os.Rename(tmpFile, manifestMarkerFile)
		} else {
			_ = os.WriteFile(manifestMarkerFile, mBytes, 0o644)
		}
	}

	return nil
}

// verifyExtractedRootHash scans targetDir (excluding the manifest marker itself and
// other build-cache directories that ExtractSnapshot intentionally leaves in place)
// and checks that the computed RootHash equals expectedHash.  On mismatch it removes
// the stale manifest marker so the next sync performs a full re-extraction.
func verifyExtractedRootHash(targetDir, expectedHash, manifestMarkerFile string) error {
	// Re-scan the directory using the same logic as ScanWorkspace so that the
	// hash is computed the same way on both sides.  We pass the marker file name
	// as an extra ignore pattern so it is not included in the hash.
	markerName := filepath.Base(manifestMarkerFile)
	got, err := ScanWorkspace(targetDir, []string{markerName})
	if err != nil {
		return fmt.Errorf("ExtractSnapshot verify scan: %w", err)
	}
	if got.RootHash != expectedHash {
		// Remove the marker so the next sync does a full re-extraction.
		_ = os.Remove(manifestMarkerFile)
		return fmt.Errorf(
			"ExtractSnapshot integrity check failed: expected RootHash %s, got %s (workspace may be corrupted)",
			expectedHash, got.RootHash,
		)
	}
	return nil
}

func validatePath(p string) error {
	if filepath.IsAbs(p) {
		return fmt.Errorf("workspace path must be relative, got: %s", p)
	}
	cleaned := filepath.Clean(p)
	if strings.HasPrefix(cleaned, "..") {
		return fmt.Errorf("workspace path escapes root: %s", p)
	}
	return nil
}

func writeFile(dest string, mode os.FileMode, r io.Reader) error {
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode|0o600)
	if err != nil {
		return err
	}
	defer f.Close() //nolint:errcheck
	_, err = io.Copy(f, r)
	return err
}

func ExtractTarGz(r io.Reader, targetDir string) error {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return fmt.Errorf("ExtractTarGz gzip: %w", err)
	}
	defer gz.Close() //nolint:errcheck

	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("ExtractTarGz next: %w", err)
		}
		if err := validatePath(hdr.Name); err != nil {
			return err
		}
		dest := filepath.Join(targetDir, filepath.FromSlash(hdr.Name))
		if err := writeFile(dest, os.FileMode(hdr.Mode), tr); err != nil {
			return fmt.Errorf("ExtractTarGz write %s: %w", hdr.Name, err)
		}
	}
	return nil
}

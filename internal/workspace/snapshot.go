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

	"github.com/waris4ly/packets/internal/storage"
	"github.com/waris4ly/packets/pkg/apitypes"
)

func ExtractSnapshot(ctx context.Context, store storage.ObjectStore, owner, snapshotRef, targetDir string) error {
	manifestKey := fmt.Sprintf("%s/manifests/%s.json", owner, snapshotRef)
	r, err := store.Download(ctx, manifestKey)
	if err != nil {
		return fmt.Errorf("ExtractSnapshot download manifest: %w", err)
	}
	defer r.Close()

	var manifest apitypes.WorkspaceManifest
	if err := json.NewDecoder(r).Decode(&manifest); err != nil {
		return fmt.Errorf("ExtractSnapshot decode manifest: %w", err)
	}

	for _, f := range manifest.Files {
		if err := validatePath(f.Path); err != nil {
			return err
		}

		destPath := filepath.Join(targetDir, filepath.FromSlash(f.Path))

		if f.IsDir {
			if err := os.MkdirAll(destPath, os.FileMode(f.Mode)|0700); err != nil {
				return fmt.Errorf("ExtractSnapshot mkdir %s: %w", f.Path, err)
			}
			continue
		}

		if f.Link != "" {
			if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
				return fmt.Errorf("ExtractSnapshot mkdir for symlink %s: %w", f.Path, err)
			}
			if err := os.Symlink(f.Link, destPath); err != nil && !os.IsExist(err) {
				return fmt.Errorf("ExtractSnapshot symlink %s: %w", f.Path, err)
			}
			continue
		}

		chunkKey := fmt.Sprintf("%s/chunks/%s", owner, f.Hash)
		cr, cerr := store.Download(ctx, chunkKey)
		if cerr != nil {
			return fmt.Errorf("ExtractSnapshot download chunk %s: %w", f.Hash[:8], cerr)
		}

		if err := writeFile(destPath, os.FileMode(f.Mode), cr); err != nil {
			cr.Close()
			return fmt.Errorf("ExtractSnapshot write %s: %w", f.Path, err)
		}
		cr.Close()
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
	if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
		return err
	}
	f, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode|0600)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(f, r)
	return err
}

func ExtractTarGz(r io.Reader, targetDir string) error {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return fmt.Errorf("ExtractTarGz gzip: %w", err)
	}
	defer gz.Close()

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

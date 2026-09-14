package workspace

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/debaucheryparty/packets/pkg/apitypes"
)

func normalizePath(p string) string {
	return strings.ReplaceAll(p, "\\", "/")
}

func ScanWorkspace(dir string, extraIgnore []string) (*apitypes.WorkspaceManifest, error) {
	var files []apitypes.WorkspaceFile

	combinedIgnore := append([]string{}, extraIgnore...)
	if gitignorePats, err := ParseIgnoreFile(filepath.Join(dir, ".gitignore")); err == nil {
		combinedIgnore = append(combinedIgnore, gitignorePats...)
	}
	if packetsPats, err := ParseIgnoreFile(filepath.Join(dir, ".packetsignore")); err == nil {
		combinedIgnore = append(combinedIgnore, packetsPats...)
	}

	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}

		normRel := normalizePath(rel)

		if ShouldIgnore(normRel, combinedIgnore) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		info, err := d.Info()
		if err != nil {
			return err
		}

		wf := apitypes.WorkspaceFile{
			Path:  normRel,
			Mode:  normalizeMode(uint32(info.Mode()), d.IsDir()),
			IsDir: d.IsDir(),
		}

		if d.IsDir() {
			files = append(files, wf)
			return nil
		}

		if info.Mode()&os.ModeSymlink != 0 {
			target, err := os.Readlink(path)
			if err != nil {
				return err
			}
			wf.Link = target
			files = append(files, wf)
			return nil
		}

		hash, size, err := hashFile(path)
		if err != nil {
			return fmt.Errorf("hash %s: %w", normRel, err)
		}
		wf.Hash = hash
		wf.Size = size
		files = append(files, wf)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("ScanWorkspace walk: %w", err)
	}

	sort.Slice(files, func(i, j int) bool {
		return files[i].Path < files[j].Path
	})

	rootHash, err := computeRootHash(files)
	if err != nil {
		return nil, err
	}

	return &apitypes.WorkspaceManifest{
		RootHash: rootHash,
		Files:    files,
	}, nil
}

func hashFile(path string) (string, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer func() { _ = f.Close() }()

	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(h.Sum(nil)), n, nil
}

func computeRootHash(files []apitypes.WorkspaceFile) (string, error) {
	h := sha256.New()
	enc := json.NewEncoder(h)
	for _, f := range files {
		if err := enc.Encode(struct {
			Path string
			Hash string
			Mode uint32
		}{f.Path, f.Hash, normalizeMode(f.Mode, f.IsDir)}); err != nil {
			return "", err
		}
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func normalizeMode(mode uint32, isDir bool) uint32 {
	if isDir {
		return 0o755
	}
	if mode&0o111 != 0 {
		return 0o755
	}
	return 0o644
}

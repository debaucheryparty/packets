package worker

import (
	"archive/tar"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
)

func createTarGzPipe(srcDir string, paths []string) (io.Reader, io.Closer) {
	pr, pw := io.Pipe()
	go func() {
		gz := gzip.NewWriter(pw)
		tw := tar.NewWriter(gz)
		var werr error
		seen := make(map[string]bool)

		for _, pattern := range paths {
			pattern = filepath.Clean(pattern)
			if pattern == "." || pattern == "" {
				continue
			}

			trimmed := pattern
			isRecursive := false
			if len(trimmed) >= 3 && (trimmed[len(trimmed)-3:] == "/**" || trimmed[len(trimmed)-3:] == `\**`) {
				trimmed = trimmed[:len(trimmed)-3]
				isRecursive = true
			}

			if isRecursive {
				fullBase := filepath.Join(srcDir, trimmed)
				if info, err := os.Stat(fullBase); err == nil && info.IsDir() {
					if err := addTreeToTar(tw, srcDir, fullBase, seen); err != nil {
						werr = err
						break
					}
					continue
				}
			}

			exactPath := filepath.Join(srcDir, pattern)
			if info, err := os.Stat(exactPath); err == nil {
				if info.IsDir() {
					if err := addTreeToTar(tw, srcDir, exactPath, seen); err != nil {
						werr = err
						break
					}
				} else {
					if err := addFileToTar(tw, srcDir, exactPath, seen); err != nil {
						werr = err
						break
					}
				}
				continue
			}

			matches, err := filepath.Glob(filepath.Join(srcDir, pattern))
			if err != nil {
				werr = err
				break
			}
			for _, match := range matches {
				if info, err := os.Stat(match); err == nil && info.IsDir() {
					if err := addTreeToTar(tw, srcDir, match, seen); err != nil {
						werr = err
						break
					}
				} else {
					if err := addFileToTar(tw, srcDir, match, seen); err != nil {
						werr = err
						break
					}
				}
			}
			if werr != nil {
				break
			}
		}
		_ = tw.Close()
		_ = gz.Close()
		pw.CloseWithError(werr)
	}()
	return pr, pw
}

func addTreeToTar(tw *tar.Writer, base, dirPath string, seen map[string]bool) error {
	return filepath.WalkDir(dirPath, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		return addFileToTar(tw, base, p, seen)
	})
}

func addFileToTar(tw *tar.Writer, base, path string, seen map[string]bool) error {
	rel, err := filepath.Rel(base, path)
	if err != nil {
		return err
	}
	cleanRel := filepath.ToSlash(rel)
	if seen[cleanRel] {
		return nil
	}
	seen[cleanRel] = true

	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	if err := tw.WriteHeader(&tar.Header{
		Name: cleanRel,
		Size: info.Size(),
		Mode: int64(info.Mode()),
	}); err != nil {
		return err
	}
	_, err = io.Copy(tw, f)
	return err
}

package worker

import (
	"archive/tar"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
)

func createTarGzPipe(srcDir string, paths []string) (io.Reader, io.Closer, error) { //nolint:unparam
	pr, pw := io.Pipe()
	go func() {
		gz := gzip.NewWriter(pw)
		tw := tar.NewWriter(gz)
		var werr error
		for _, pattern := range paths {
			matches, err := filepath.Glob(filepath.Join(srcDir, pattern))
			if err != nil {
				werr = err
				break
			}
			for _, match := range matches {
				if err := addToTar(tw, srcDir, match); err != nil {
					werr = err
					break
				}
			}
			if werr != nil {
				break
			}
		}
		tw.Close() //nolint:errcheck
		gz.Close() //nolint:errcheck
		pw.CloseWithError(werr)
	}()
	return pr, pw, nil
}

func addToTar(tw *tar.Writer, base, path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(base, path)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return filepath.WalkDir(path, func(p string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			return addToTar(tw, base, p)
		})
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := tw.WriteHeader(&tar.Header{
		Name: rel,
		Size: info.Size(),
		Mode: int64(info.Mode()),
	}); err != nil {
		return err
	}
	_, err = io.Copy(tw, f)
	return err
}

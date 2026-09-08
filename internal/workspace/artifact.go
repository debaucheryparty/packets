package workspace

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// ExtractArtifact unpacks tar.gz, zip, or saves raw artifact data into destDir.
func ExtractArtifact(data []byte, destDir, defaultFileName string) error {
	if len(data) == 0 {
		return fmt.Errorf("artifact payload is empty")
	}

	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return fmt.Errorf("create dest dir: %w", err)
	}

	// 1. Check for tar.gz (gzip magic bytes: 0x1f, 0x8b)
	if len(data) >= 2 && data[0] == 0x1f && data[1] == 0x8b {
		if err := ExtractTarGz(bytes.NewReader(data), destDir); err != nil {
			return fmt.Errorf("extract tar.gz artifact: %w", err)
		}
		return nil
	}

	// 2. Check for zip (PK\x03\x04)
	if len(data) >= 4 && data[0] == 0x50 && data[1] == 0x4b && data[2] == 0x03 && data[3] == 0x04 {
		zipReader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
		if err != nil {
			return fmt.Errorf("open zip artifact: %w", err)
		}
		for _, zf := range zipReader.File {
			outPath := filepath.Join(destDir, filepath.FromSlash(zf.Name))
			if zf.FileInfo().IsDir() {
				_ = os.MkdirAll(outPath, 0o755)
				continue
			}
			_ = os.MkdirAll(filepath.Dir(outPath), 0o755)
			rc, err := zf.Open()
			if err != nil {
				continue
			}
			outFile, err := os.OpenFile(outPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, zf.Mode())
			if err == nil {
				_, _ = io.Copy(outFile, rc)
				outFile.Close()
			}
			rc.Close()
		}
		return nil
	}

	// 3. Raw file
	if defaultFileName == "" {
		defaultFileName = "artifact.bin"
	}
	outPath := filepath.Join(destDir, defaultFileName)
	if err := os.WriteFile(outPath, data, 0o644); err != nil {
		return fmt.Errorf("write raw artifact: %w", err)
	}

	return nil
}

package workspace

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestScanWorkspaceAndNormalize(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "packets-test-scan-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir) //nolint:errcheck

	subDir := filepath.Join(tmpDir, "src", "nested")
	if err := os.MkdirAll(subDir, 0o755); err != nil {
		t.Fatal(err)
	}

	file1 := filepath.Join(tmpDir, "main.go")
	if err := os.WriteFile(file1, []byte("package main\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	file2 := filepath.Join(subDir, "helper.go")
	if err := os.WriteFile(file2, []byte("package nested\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	ignoredDir := filepath.Join(tmpDir, "node_modules", "package")
	if err := os.MkdirAll(ignoredDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ignoredDir, "index.js"), []byte("console.log()"), 0o644); err != nil {
		t.Fatal(err)
	}

	manifest, err := ScanWorkspace(tmpDir, nil)
	if err != nil {
		t.Fatalf("ScanWorkspace failed: %v", err)
	}

	if manifest.RootHash == "" {
		t.Errorf("expected non-empty RootHash")
	}

	for _, f := range manifest.Files {
		if strings.Contains(f.Path, "\\") {
			t.Errorf("path %q contains backslash, expected forward slash", f.Path)
		}
		if strings.HasPrefix(f.Path, "node_modules") {
			t.Errorf("expected node_modules to be ignored, got %q", f.Path)
		}
	}
}

func TestLocalCache(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "packets-test-cache-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir) //nolint:errcheck

	initial, err := loadLocalCache(tmpDir)
	if err != nil {
		t.Fatal(err)
	}
	if initial != nil {
		t.Fatalf("expected nil cache for empty dir, got %+v", initial)
	}

	toSave := &localManifestCache{
		RootHash:    "test-root-hash",
		SnapshotRef: "test-snapshot-ref",
	}

	if err := saveLocalCache(tmpDir, toSave); err != nil {
		t.Fatalf("saveLocalCache failed: %v", err)
	}

	loaded, err := loadLocalCache(tmpDir)
	if err != nil {
		t.Fatalf("loadLocalCache failed: %v", err)
	}
	if loaded == nil || loaded.RootHash != "test-root-hash" || loaded.SnapshotRef != "test-snapshot-ref" {
		t.Fatalf("unexpected loaded cache: %+v", loaded)
	}
}

type mockMemStore struct {
	data map[string][]byte
}

func newMockMemStore() *mockMemStore {
	return &mockMemStore{data: make(map[string][]byte)}
}

func (m *mockMemStore) Upload(ctx context.Context, key string, r io.Reader, size int64) error {
	buf, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	m.data[key] = buf
	return nil
}

func (m *mockMemStore) Download(ctx context.Context, key string) (io.ReadCloser, error) {
	b, ok := m.data[key]
	if !ok {
		return nil, os.ErrNotExist
	}
	return &bytesCloser{b: b}, nil
}

func (m *mockMemStore) Delete(ctx context.Context, key string) error {
	delete(m.data, key)
	return nil
}

func (m *mockMemStore) Exists(ctx context.Context, key string) (bool, error) {
	_, ok := m.data[key]
	return ok, nil
}

func (m *mockMemStore) List(ctx context.Context, prefix string) ([]string, error) {
	var keys []string
	for k := range m.data {
		if strings.HasPrefix(k, prefix) {
			keys = append(keys, k)
		}
	}
	return keys, nil
}

func (m *mockMemStore) PresignUpload(ctx context.Context, key string, ttl time.Duration) (string, error) {
	return "http://mock-upload/" + key, nil
}

func (m *mockMemStore) PresignDownload(ctx context.Context, key string, ttl time.Duration) (string, error) {
	return "http://mock-download/" + key, nil
}

type bytesCloser struct {
	b   []byte
	off int
}

func (b *bytesCloser) Read(p []byte) (int, error) {
	if b.off >= len(b.b) {
		return 0, io.EOF
	}
	n := copy(p, b.b[b.off:])
	b.off += n
	return n, nil
}

func (b *bytesCloser) Close() error {
	return nil
}

func TestExtractSnapshot(t *testing.T) {
	srcDir, err := os.MkdirTemp("", "packets-src-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(srcDir) //nolint:errcheck

	dstDir, err := os.MkdirTemp("", "packets-dst-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dstDir)

	testFile := filepath.Join(srcDir, "hello.txt")
	testContent := []byte("hello packets remote build")
	if err := os.WriteFile(testFile, testContent, 0o644); err != nil {
		t.Fatal(err)
	}

	manifest, err := ScanWorkspace(srcDir, nil)
	if err != nil {
		t.Fatal(err)
	}

	store := newMockMemStore()
	owner := "testuser"

	for _, f := range manifest.Files {
		if f.IsDir || f.Hash == "" {
			continue
		}
		data, err := readChunkByHash(srcDir, manifest, f.Hash)
		if err != nil {
			t.Fatal(err)
		}
		key := "testuser/chunks/" + f.Hash
		store.data[key] = data
	}

	manifestData, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	store.data["testuser/manifests/"+manifest.RootHash+".json"] = manifestData

	err = ExtractSnapshot(context.Background(), store, owner, manifest.RootHash, dstDir)
	if err != nil {
		t.Fatalf("ExtractSnapshot failed: %v", err)
	}

	extractedFile := filepath.Join(dstDir, "hello.txt")
	got, err := os.ReadFile(extractedFile)
	if err != nil {
		t.Fatalf("failed to read extracted file: %v", err)
	}
	if string(got) != string(testContent) {
		t.Errorf("content mismatch: got %q, want %q", string(got), string(testContent))
	}
}

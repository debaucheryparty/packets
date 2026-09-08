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
	defer os.RemoveAll(tmpDir)

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
	defer os.RemoveAll(tmpDir)

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
	defer os.RemoveAll(srcDir)

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

func TestExtractSnapshot_DeletionDetection(t *testing.T) {
	srcDir, err := os.MkdirTemp("", "packets-test-sync-src-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(srcDir)

	dstDir, err := os.MkdirTemp("", "packets-test-sync-dst-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dstDir)

	store := newMockMemStore()
	owner := "testuser"

	// 1. Create initial files
	_ = os.WriteFile(filepath.Join(srcDir, "keep.txt"), []byte("keeper content"), 0o644)
	_ = os.WriteFile(filepath.Join(srcDir, "delete_me.txt"), []byte("to be deleted"), 0o644)

	manifest1, err := ScanWorkspace(srcDir, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range manifest1.Files {
		if !f.IsDir && f.Hash != "" {
			d, _ := readChunkByHash(srcDir, manifest1, f.Hash)
			store.data["testuser/chunks/"+f.Hash] = d
		}
	}
	m1Data, _ := json.Marshal(manifest1)
	store.data["testuser/manifests/"+manifest1.RootHash+".json"] = m1Data

	// Extract Snapshot 1
	if err := ExtractSnapshot(context.Background(), store, owner, manifest1.RootHash, dstDir); err != nil {
		t.Fatalf("extract snapshot 1 failed: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dstDir, "delete_me.txt")); err != nil {
		t.Fatalf("expected delete_me.txt to exist in dstDir")
	}

	// 2. Delete file locally and create Snapshot 2
	if err := os.Remove(filepath.Join(srcDir, "delete_me.txt")); err != nil {
		t.Fatal(err)
	}

	manifest2, err := ScanWorkspace(srcDir, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range manifest2.Files {
		if !f.IsDir && f.Hash != "" {
			d, _ := readChunkByHash(srcDir, manifest2, f.Hash)
			store.data["testuser/chunks/"+f.Hash] = d
		}
	}
	m2Data, _ := json.Marshal(manifest2)
	store.data["testuser/manifests/"+manifest2.RootHash+".json"] = m2Data

	// Extract Snapshot 2 into the same dstDir
	if err := ExtractSnapshot(context.Background(), store, owner, manifest2.RootHash, dstDir); err != nil {
		t.Fatalf("extract snapshot 2 failed: %v", err)
	}

	// Verify delete_me.txt is deleted on remote dstDir
	if _, err := os.Stat(filepath.Join(dstDir, "delete_me.txt")); !os.IsNotExist(err) {
		t.Errorf("expected delete_me.txt to be deleted on remote workspace, but it still exists")
	}

	// Verify keep.txt is preserved
	kept, err := os.ReadFile(filepath.Join(dstDir, "keep.txt"))
	if err != nil || string(kept) != "keeper content" {
		t.Errorf("expected keep.txt to be preserved, got %q, err: %v", string(kept), err)
	}
}

func TestExtractSnapshot_NestedDirectoryPruning(t *testing.T) {
	srcDir, err := os.MkdirTemp("", "packets-test-prune-src-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(srcDir)

	dstDir, err := os.MkdirTemp("", "packets-test-prune-dst-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dstDir)

	store := newMockMemStore()
	owner := "testuser"

	// Create nested structure
	nestedDir := filepath.Join(srcDir, "sub", "deep")
	_ = os.MkdirAll(nestedDir, 0o755)
	_ = os.WriteFile(filepath.Join(nestedDir, "leaf.txt"), []byte("leaf"), 0o644)
	_ = os.WriteFile(filepath.Join(srcDir, "root.txt"), []byte("root"), 0o644)

	m1, err := ScanWorkspace(srcDir, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range m1.Files {
		if !f.IsDir && f.Hash != "" {
			d, _ := readChunkByHash(srcDir, m1, f.Hash)
			store.data["testuser/chunks/"+f.Hash] = d
		}
	}
	m1Bytes, _ := json.Marshal(m1)
	store.data["testuser/manifests/"+m1.RootHash+".json"] = m1Bytes

	if err := ExtractSnapshot(context.Background(), store, owner, m1.RootHash, dstDir); err != nil {
		t.Fatalf("extract snapshot 1: %v", err)
	}

	// Delete the nested directory contents locally
	_ = os.RemoveAll(filepath.Join(srcDir, "sub"))

	m2, err := ScanWorkspace(srcDir, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range m2.Files {
		if !f.IsDir && f.Hash != "" {
			d, _ := readChunkByHash(srcDir, m2, f.Hash)
			store.data["testuser/chunks/"+f.Hash] = d
		}
	}
	m2Bytes, _ := json.Marshal(m2)
	store.data["testuser/manifests/"+m2.RootHash+".json"] = m2Bytes

	if err := ExtractSnapshot(context.Background(), store, owner, m2.RootHash, dstDir); err != nil {
		t.Fatalf("extract snapshot 2: %v", err)
	}

	// Verify deep directory is completely pruned
	if _, err := os.Stat(filepath.Join(dstDir, "sub")); !os.IsNotExist(err) {
		t.Errorf("expected empty parent directory 'sub' to be pruned on remote workspace")
	}

	// Verify root.txt remains intact
	if _, err := os.Stat(filepath.Join(dstDir, "root.txt")); err != nil {
		t.Errorf("expected root.txt to remain intact")
	}
}

func TestExtractSnapshot_TypeTransition(t *testing.T) {
	srcDir, err := os.MkdirTemp("", "packets-test-type-src-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(srcDir)

	dstDir, err := os.MkdirTemp("", "packets-test-type-dst-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dstDir)

	store := newMockMemStore()
	owner := "testuser"

	// 1. Initial snapshot: "entry" is a file
	_ = os.WriteFile(filepath.Join(srcDir, "entry"), []byte("i am a file"), 0o644)
	m1, _ := ScanWorkspace(srcDir, nil)
	for _, f := range m1.Files {
		if !f.IsDir && f.Hash != "" {
			d, _ := readChunkByHash(srcDir, m1, f.Hash)
			store.data["testuser/chunks/"+f.Hash] = d
		}
	}
	m1Bytes, _ := json.Marshal(m1)
	store.data["testuser/manifests/"+m1.RootHash+".json"] = m1Bytes

	if err := ExtractSnapshot(context.Background(), store, owner, m1.RootHash, dstDir); err != nil {
		t.Fatalf("extract snapshot 1: %v", err)
	}

	// 2. Snapshot 2: remove file "entry", replace with directory "entry/child.txt"
	_ = os.Remove(filepath.Join(srcDir, "entry"))
	_ = os.MkdirAll(filepath.Join(srcDir, "entry"), 0o755)
	_ = os.WriteFile(filepath.Join(srcDir, "entry", "child.txt"), []byte("child content"), 0o644)

	m2, _ := ScanWorkspace(srcDir, nil)
	for _, f := range m2.Files {
		if !f.IsDir && f.Hash != "" {
			d, _ := readChunkByHash(srcDir, m2, f.Hash)
			store.data["testuser/chunks/"+f.Hash] = d
		}
	}
	m2Bytes, _ := json.Marshal(m2)
	store.data["testuser/manifests/"+m2.RootHash+".json"] = m2Bytes

	if err := ExtractSnapshot(context.Background(), store, owner, m2.RootHash, dstDir); err != nil {
		t.Fatalf("extract snapshot 2: %v", err)
	}

	childContent, err := os.ReadFile(filepath.Join(dstDir, "entry", "child.txt"))
	if err != nil || string(childContent) != "child content" {
		t.Fatalf("expected entry/child.txt with 'child content', got err: %v, content: %q", err, string(childContent))
	}
}

func TestScanWorkspace_IgnoreFiles(t *testing.T) {
	srcDir, err := os.MkdirTemp("", "packets-test-ignore-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(srcDir)

	_ = os.WriteFile(filepath.Join(srcDir, ".gitignore"), []byte("secret.key\nignored_dir/\n*.tmp\n"), 0o644)
	_ = os.WriteFile(filepath.Join(srcDir, "secret.key"), []byte("key"), 0o644)
	_ = os.WriteFile(filepath.Join(srcDir, "test.tmp"), []byte("temp"), 0o644)
	_ = os.WriteFile(filepath.Join(srcDir, "keep.go"), []byte("package main"), 0o644)

	_ = os.MkdirAll(filepath.Join(srcDir, "ignored_dir"), 0o755)
	_ = os.WriteFile(filepath.Join(srcDir, "ignored_dir", "file.txt"), []byte("ignored"), 0o644)

	m, err := ScanWorkspace(srcDir, nil)
	if err != nil {
		t.Fatalf("ScanWorkspace: %v", err)
	}

	for _, f := range m.Files {
		if f.Path == "secret.key" || f.Path == "test.tmp" || strings.HasPrefix(f.Path, "ignored_dir") {
			t.Errorf("file %q should have been ignored", f.Path)
		}
	}

	foundKeep := false
	for _, f := range m.Files {
		if f.Path == "keep.go" {
			foundKeep = true
		}
	}
	if !foundKeep {
		t.Errorf("keep.go should have been included in manifest")
	}
}

// TestExtractSnapshot_IntegrityCheck verifies that ExtractSnapshot detects when an
// extracted file's content does not match what the manifest expects (simulating a
// corrupted chunk in the object store).  The extraction should fail and the manifest
// marker must NOT be written.
func TestExtractSnapshot_IntegrityCheck(t *testing.T) {
	srcDir, err := os.MkdirTemp("", "packets-test-integrity-src-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(srcDir)

	dstDir, err := os.MkdirTemp("", "packets-test-integrity-dst-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dstDir)

	store := newMockMemStore()
	owner := "testuser"

	// Write a real file and produce a manifest.
	_ = os.WriteFile(filepath.Join(srcDir, "data.bin"), []byte("original content"), 0o644)
	m, err := ScanWorkspace(srcDir, nil)
	if err != nil {
		t.Fatal(err)
	}

	// Store the manifest normally.
	mBytes, _ := json.Marshal(m)
	store.data["testuser/manifests/"+m.RootHash+".json"] = mBytes

	// Store a CORRUPTED chunk (different bytes) for the file.
	for _, f := range m.Files {
		if !f.IsDir && f.Hash != "" {
			store.data["testuser/chunks/"+f.Hash] = []byte("CORRUPTED BYTES HERE")
		}
	}

	err = ExtractSnapshot(context.Background(), store, owner, m.RootHash, dstDir)
	if err == nil {
		t.Fatal("expected ExtractSnapshot to fail due to RootHash mismatch, but it succeeded")
	}
	if !strings.Contains(err.Error(), "integrity check failed") {
		t.Errorf("unexpected error: %v", err)
	}

	// The manifest marker must NOT exist after a failed extraction.
	if _, statErr := os.Stat(filepath.Join(dstDir, ".packets_manifest.json")); statErr == nil {
		t.Error("manifest marker must not be written after a failed integrity check")
	}
}

// TestExtractSnapshot_PartialFailure verifies that if a chunk download fails mid-way
// through extraction, the manifest marker is NOT written, ensuring the next sync
// will retry all missing chunks rather than assuming the workspace is up to date.
func TestExtractSnapshot_PartialFailure(t *testing.T) {
	srcDir, err := os.MkdirTemp("", "packets-test-partial-src-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(srcDir)

	dstDir, err := os.MkdirTemp("", "packets-test-partial-dst-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dstDir)

	store := newMockMemStore()
	owner := "testuser"

	// Write two files so there are two chunks.
	_ = os.WriteFile(filepath.Join(srcDir, "file_a.txt"), []byte("file a content"), 0o644)
	_ = os.WriteFile(filepath.Join(srcDir, "file_b.txt"), []byte("file b content"), 0o644)

	m, err := ScanWorkspace(srcDir, nil)
	if err != nil {
		t.Fatal(err)
	}

	// Only store one of the two chunks — the second download will fail.
	uploadedOne := false
	for _, f := range m.Files {
		if !f.IsDir && f.Hash != "" {
			if !uploadedOne {
				data, _ := readChunkByHash(srcDir, m, f.Hash)
				store.data["testuser/chunks/"+f.Hash] = data
				uploadedOne = true
			}
			// Deliberately omit the second chunk.
		}
	}
	mBytes, _ := json.Marshal(m)
	store.data["testuser/manifests/"+m.RootHash+".json"] = mBytes

	err = ExtractSnapshot(context.Background(), store, owner, m.RootHash, dstDir)
	if err == nil {
		t.Fatal("expected ExtractSnapshot to fail due to missing chunk, but it succeeded")
	}

	// Manifest marker must NOT exist.
	if _, statErr := os.Stat(filepath.Join(dstDir, ".packets_manifest.json")); statErr == nil {
		t.Error("manifest marker must not be written after a partial extraction failure")
	}
}

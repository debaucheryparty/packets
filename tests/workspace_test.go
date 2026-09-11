package tests

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/debaucheryparty/packets/internal/workspace"
)

func TestScanWorkspaceAndNormalize(t *testing.T) {
	tmpDir := t.TempDir()

	subDir := filepath.Join(tmpDir, "src", "nested")
	if err := os.MkdirAll(subDir, 0o755); err != nil {
		t.Fatal(err)
	}

	file1 := filepath.Join(tmpDir, "main.go")
	if err := os.WriteFile(file1, []byte("package main\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	file2 := filepath.Join(subDir, "util.go")
	if err := os.WriteFile(file2, []byte("package nested\nfunc Util() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	fileExe := filepath.Join(tmpDir, "tool.sh")
	if err := os.WriteFile(fileExe, []byte("#!/bin/sh\necho hi\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	manifest, err := workspace.ScanWorkspace(tmpDir, nil)
	if err != nil {
		t.Fatalf("ScanWorkspace failed: %v", err)
	}

	fileCount := 0
	for _, f := range manifest.Files {
		if strings.Contains(f.Path, "\\") {
			t.Errorf("file path %q contains backslashes", f.Path)
		}
		if !f.IsDir {
			fileCount++
			if f.Hash == "" {
				t.Errorf("file %q has empty hash", f.Path)
			}
		}
	}

	if fileCount != 3 {
		t.Errorf("expected 3 files, got %d", fileCount)
	}

	if manifest.RootHash == "" {
		t.Error("manifest RootHash is empty")
	}
}

func TestLocalCache(t *testing.T) {
	tmpDir := t.TempDir()

	initial, err := workspace.LoadLocalCache(tmpDir)
	if err != nil {
		t.Fatalf("unexpected error loading non-existent cache: %v", err)
	}
	if initial != nil {
		t.Fatalf("expected nil cache for new directory, got: %+v", initial)
	}

	toSave := &workspace.LocalManifestCache{
		RootHash:    "test-root-hash",
		SnapshotRef: "test-snapshot-ref",
		UploadedAt:  time.Now(),
	}
	if err := workspace.SaveLocalCache(tmpDir, toSave); err != nil {
		t.Fatalf("SaveLocalCache failed: %v", err)
	}

	loaded, err := workspace.LoadLocalCache(tmpDir)
	if err != nil {
		t.Fatalf("LoadLocalCache failed: %v", err)
	}
	if loaded == nil || loaded.RootHash != "test-root-hash" || loaded.SnapshotRef != "test-snapshot-ref" {
		t.Fatalf("unexpected loaded cache: %+v", loaded)
	}
}

type workspaceTestMemStore struct {
	data map[string][]byte
}

func newWorkspaceTestMemStore() *workspaceTestMemStore {
	return &workspaceTestMemStore{data: make(map[string][]byte)}
}

func (m *workspaceTestMemStore) Upload(ctx context.Context, key string, r io.Reader, size int64) error {
	buf, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	m.data[key] = buf
	return nil
}

func (m *workspaceTestMemStore) Download(ctx context.Context, key string) (io.ReadCloser, error) {
	b, ok := m.data[key]
	if !ok {
		return nil, os.ErrNotExist
	}
	return &workspaceBytesCloser{b: b}, nil
}

func (m *workspaceTestMemStore) Delete(ctx context.Context, key string) error {
	delete(m.data, key)
	return nil
}

func (m *workspaceTestMemStore) Exists(ctx context.Context, key string) (bool, error) {
	_, ok := m.data[key]
	return ok, nil
}

func (m *workspaceTestMemStore) List(ctx context.Context, prefix string) ([]string, error) {
	var keys []string
	for k := range m.data {
		if strings.HasPrefix(k, prefix) {
			keys = append(keys, k)
		}
	}
	return keys, nil
}

func (m *workspaceTestMemStore) PresignUpload(ctx context.Context, key string, ttl time.Duration) (string, error) {
	return "http://mock-upload/" + key, nil
}

func (m *workspaceTestMemStore) PresignDownload(ctx context.Context, key string, ttl time.Duration) (string, error) {
	return "http://mock-download/" + key, nil
}

type workspaceBytesCloser struct {
	b   []byte
	off int
}

func (b *workspaceBytesCloser) Read(p []byte) (int, error) {
	if b.off >= len(b.b) {
		return 0, io.EOF
	}
	n := copy(p, b.b[b.off:])
	b.off += n
	return n, nil
}

func (b *workspaceBytesCloser) Close() error {
	return nil
}

func TestExtractSnapshot(t *testing.T) {
	srcDir := t.TempDir()
	dstDir := t.TempDir()

	testFile := filepath.Join(srcDir, "hello.txt")
	testContent := []byte("hello packets remote build")
	if err := os.WriteFile(testFile, testContent, 0o644); err != nil {
		t.Fatal(err)
	}

	manifest, err := workspace.ScanWorkspace(srcDir, nil)
	if err != nil {
		t.Fatal(err)
	}

	store := newWorkspaceTestMemStore()
	owner := "testuser"

	for _, f := range manifest.Files {
		if f.IsDir || f.Hash == "" {
			continue
		}
		data, err := workspace.ReadChunkByHash(srcDir, manifest, f.Hash)
		if err != nil {
			t.Fatal(err)
		}
		store.data["testuser/chunks/"+f.Hash] = data
	}

	manifestBytes, _ := json.Marshal(manifest)
	store.data["testuser/manifests/"+manifest.RootHash+".json"] = manifestBytes

	err = workspace.ExtractSnapshot(context.Background(), store, owner, manifest.RootHash, dstDir)
	if err != nil {
		t.Fatalf("ExtractSnapshot failed: %v", err)
	}

	extractedFile := filepath.Join(dstDir, "hello.txt")
	gotContent, err := os.ReadFile(extractedFile)
	if err != nil {
		t.Fatalf("reading extracted file: %v", err)
	}
	if string(gotContent) != string(testContent) {
		t.Errorf("got %q, want %q", gotContent, testContent)
	}
}

func TestExtractSnapshot_DeletionDetection(t *testing.T) {
	srcDir := t.TempDir()
	dstDir := t.TempDir()

	fileA := filepath.Join(srcDir, "keep.txt")
	fileB := filepath.Join(srcDir, "deleted_local.txt")
	_ = os.WriteFile(fileA, []byte("keep-me"), 0o644)
	_ = os.WriteFile(fileB, []byte("delete-me"), 0o644)

	manifest1, err := workspace.ScanWorkspace(srcDir, nil)
	if err != nil {
		t.Fatal(err)
	}

	store := newWorkspaceTestMemStore()
	owner := "testuser"

	for _, f := range manifest1.Files {
		if !f.IsDir && f.Hash != "" {
			d, _ := workspace.ReadChunkByHash(srcDir, manifest1, f.Hash)
			store.data["testuser/chunks/"+f.Hash] = d
		}
	}
	m1Bytes, _ := json.Marshal(manifest1)
	store.data["testuser/manifests/"+manifest1.RootHash+".json"] = m1Bytes

	err = workspace.ExtractSnapshot(context.Background(), store, owner, manifest1.RootHash, dstDir)
	if err != nil {
		t.Fatalf("ExtractSnapshot 1 failed: %v", err)
	}

	gradleCacheDir := filepath.Join(dstDir, ".gradle", "caches")
	_ = os.MkdirAll(gradleCacheDir, 0o755)
	_ = os.WriteFile(filepath.Join(gradleCacheDir, "cached_dep.bin"), []byte("cached"), 0o644)

	buildOutputDir := filepath.Join(dstDir, "build", "outputs")
	_ = os.MkdirAll(buildOutputDir, 0o755)
	_ = os.WriteFile(filepath.Join(buildOutputDir, "app.apk"), []byte("apk"), 0o644)

	_ = os.Remove(fileB)

	manifest2, err := workspace.ScanWorkspace(srcDir, nil)
	if err != nil {
		t.Fatal(err)
	}

	for _, f := range manifest2.Files {
		if !f.IsDir && f.Hash != "" {
			d, _ := workspace.ReadChunkByHash(srcDir, manifest2, f.Hash)
			store.data["testuser/chunks/"+f.Hash] = d
		}
	}
	m2Bytes, _ := json.Marshal(manifest2)
	store.data["testuser/manifests/"+manifest2.RootHash+".json"] = m2Bytes

	err = workspace.ExtractSnapshot(context.Background(), store, owner, manifest2.RootHash, dstDir)
	if err != nil {
		t.Fatalf("ExtractSnapshot 2 failed: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dstDir, "deleted_local.txt")); !os.IsNotExist(err) {
		t.Errorf("deleted_local.txt was NOT deleted from destination workspace")
	}

	if _, err := os.Stat(filepath.Join(dstDir, "keep.txt")); err != nil {
		t.Errorf("keep.txt should still exist in destination workspace: %v", err)
	}

	if _, err := os.Stat(filepath.Join(gradleCacheDir, "cached_dep.bin")); err != nil {
		t.Errorf(".gradle cache file should be preserved across syncs: %v", err)
	}
	if _, err := os.Stat(filepath.Join(buildOutputDir, "app.apk")); err != nil {
		t.Errorf("build outputs should be preserved across syncs: %v", err)
	}
}

func TestExtractSnapshot_IntegrityCheck(t *testing.T) {
	srcDir := t.TempDir()
	dstDir := t.TempDir()

	store := newWorkspaceTestMemStore()
	owner := "testuser"

	_ = os.WriteFile(filepath.Join(srcDir, "data.bin"), []byte("original content"), 0o644)
	m, err := workspace.ScanWorkspace(srcDir, nil)
	if err != nil {
		t.Fatal(err)
	}

	mBytes, _ := json.Marshal(m)
	store.data["testuser/manifests/"+m.RootHash+".json"] = mBytes

	for _, f := range m.Files {
		if !f.IsDir && f.Hash != "" {
			store.data["testuser/chunks/"+f.Hash] = []byte("CORRUPTED BYTES HERE")
		}
	}

	err = workspace.ExtractSnapshot(context.Background(), store, owner, m.RootHash, dstDir)
	if err == nil {
		t.Fatal("expected ExtractSnapshot to fail due to RootHash mismatch, but it succeeded")
	}
	if !strings.Contains(err.Error(), "integrity check failed") {
		t.Errorf("unexpected error: %v", err)
	}

	if _, statErr := os.Stat(filepath.Join(dstDir, ".packets_manifest.json")); statErr == nil {
		t.Error("manifest marker must not be written after a failed integrity check")
	}
}

func TestExtractSnapshot_PartialFailure(t *testing.T) {
	srcDir := t.TempDir()
	dstDir := t.TempDir()

	store := newWorkspaceTestMemStore()
	owner := "testuser"

	_ = os.WriteFile(filepath.Join(srcDir, "file_a.txt"), []byte("file a content"), 0o644)
	_ = os.WriteFile(filepath.Join(srcDir, "file_b.txt"), []byte("file b content"), 0o644)

	m, err := workspace.ScanWorkspace(srcDir, nil)
	if err != nil {
		t.Fatal(err)
	}

	uploadedOne := false
	for _, f := range m.Files {
		if !f.IsDir && f.Hash != "" {
			if !uploadedOne {
				data, _ := workspace.ReadChunkByHash(srcDir, m, f.Hash)
				store.data["testuser/chunks/"+f.Hash] = data
				uploadedOne = true
			}
		}
	}
	mBytes, _ := json.Marshal(m)
	store.data["testuser/manifests/"+m.RootHash+".json"] = mBytes

	err = workspace.ExtractSnapshot(context.Background(), store, owner, m.RootHash, dstDir)
	if err == nil {
		t.Fatal("expected ExtractSnapshot to fail due to missing chunk, but it succeeded")
	}

	if _, statErr := os.Stat(filepath.Join(dstDir, ".packets_manifest.json")); statErr == nil {
		t.Error("manifest marker must not be written after a partial extraction failure")
	}
}

func TestWorkspaceSync_DeterministicHashing(t *testing.T) {
	dirA := t.TempDir()
	dirB := t.TempDir()

	_ = os.WriteFile(filepath.Join(dirA, "main.go"), []byte("package main"), 0o644)
	_ = os.MkdirAll(filepath.Join(dirA, "pkg"), 0o755)
	_ = os.WriteFile(filepath.Join(dirA, "pkg", "lib.go"), []byte("package pkg"), 0o644)

	_ = os.WriteFile(filepath.Join(dirB, "main.go"), []byte("package main"), 0o644)
	_ = os.MkdirAll(filepath.Join(dirB, "pkg"), 0o755)
	_ = os.WriteFile(filepath.Join(dirB, "pkg", "lib.go"), []byte("package pkg"), 0o644)

	mA, errA := workspace.ScanWorkspace(dirA, nil)
	if errA != nil {
		t.Fatal(errA)
	}
	mB, errB := workspace.ScanWorkspace(dirB, nil)
	if errB != nil {
		t.Fatal(errB)
	}

	if mA.RootHash != mB.RootHash {
		t.Errorf("expected deterministic RootHash, got mA=%s mB=%s", mA.RootHash, mB.RootHash)
	}

	_ = os.WriteFile(filepath.Join(dirB, "pkg", "lib.go"), []byte("package pkg\n// changed"), 0o644)
	mB2, _ := workspace.ScanWorkspace(dirB, nil)
	if mA.RootHash == mB2.RootHash {
		t.Errorf("expected RootHash to change when file content changes")
	}
}

func TestWorkspaceSync_LifecycleModifications(t *testing.T) {
	srcDir := t.TempDir()
	dstDir := t.TempDir()
	store := newWorkspaceTestMemStore()
	owner := "testuser"

	syncSrcToDst := func() string {
		m, err := workspace.ScanWorkspace(srcDir, nil)
		if err != nil {
			t.Fatal(err)
		}
		for _, f := range m.Files {
			if !f.IsDir && f.Hash != "" {
				d, rErr := workspace.ReadChunkByHash(srcDir, m, f.Hash)
				if rErr != nil {
					t.Fatal(rErr)
				}
				store.data["testuser/chunks/"+f.Hash] = d
			}
		}
		mBytes, _ := json.Marshal(m)
		store.data["testuser/manifests/"+m.RootHash+".json"] = mBytes
		if err := workspace.ExtractSnapshot(context.Background(), store, owner, m.RootHash, dstDir); err != nil {
			t.Fatal(err)
		}
		return m.RootHash
	}

	f1 := filepath.Join(srcDir, "file1.txt")
	_ = os.WriteFile(f1, []byte("version 1"), 0o644)
	_ = os.MkdirAll(filepath.Join(srcDir, "sub"), 0o755)
	f2 := filepath.Join(srcDir, "sub", "file2.txt")
	_ = os.WriteFile(f2, []byte("nested file"), 0o644)

	largeData := make([]byte, 1024*1024)
	for i := range largeData {
		largeData[i] = byte(i % 256)
	}
	fLarge := filepath.Join(srcDir, "large.bin")
	_ = os.WriteFile(fLarge, largeData, 0o644)

	hash1 := syncSrcToDst()
	if _, err := os.Stat(filepath.Join(dstDir, "file1.txt")); err != nil {
		t.Fatalf("file1.txt missing after initial sync: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dstDir, "large.bin")); err != nil {
		t.Fatalf("large.bin missing after initial sync: %v", err)
	}

	_ = os.WriteFile(f1, []byte("version 2 modified"), 0o644)
	hash2 := syncSrcToDst()
	if hash1 == hash2 {
		t.Errorf("hash should change on modification")
	}
	gotMod, _ := os.ReadFile(filepath.Join(dstDir, "file1.txt"))
	if string(gotMod) != "version 2 modified" {
		t.Errorf("modified content mismatch: got %q", string(gotMod))
	}

	f3 := filepath.Join(srcDir, "new_file.txt")
	_ = os.WriteFile(f3, []byte("brand new"), 0o644)
	syncSrcToDst()
	if _, err := os.Stat(filepath.Join(dstDir, "new_file.txt")); err != nil {
		t.Errorf("new_file.txt missing after new file sync")
	}

	_ = os.Remove(f2)
	syncSrcToDst()
	if _, err := os.Stat(filepath.Join(dstDir, "sub", "file2.txt")); !os.IsNotExist(err) {
		t.Errorf("deleted nested file should be removed in dstDir")
	}

	_ = os.WriteFile(f1, []byte("version 1"), 0o644)
	hashReverted := syncSrcToDst()
	gotReverted, _ := os.ReadFile(filepath.Join(dstDir, "file1.txt"))
	if string(gotReverted) != "version 1" {
		t.Errorf("reverted content mismatch: got %q", string(gotReverted))
	}
	_ = hashReverted

	_ = os.Remove(f3)
	fRenamed := filepath.Join(srcDir, "renamed_file.txt")
	_ = os.WriteFile(fRenamed, []byte("brand new"), 0o644)
	syncSrcToDst()
	if _, err := os.Stat(filepath.Join(dstDir, "new_file.txt")); !os.IsNotExist(err) {
		t.Errorf("old file name still exists after rename")
	}
	if _, err := os.Stat(filepath.Join(dstDir, "renamed_file.txt")); err != nil {
		t.Errorf("renamed file missing in dstDir")
	}
}


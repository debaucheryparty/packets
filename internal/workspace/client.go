package workspace

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/debaucheryparty/packets/pkg/apitypes"
	pb "github.com/debaucheryparty/packets/proto/v1"
	"google.golang.org/grpc"
)

func UploadWorkspace(ctx context.Context, conn *grpc.ClientConn, dir string, force bool) (string, error) {
	client := pb.NewWorkspaceClient(conn)

	manifest, err := ScanWorkspace(dir, nil)
	if err != nil {
		return "", fmt.Errorf("UploadWorkspace scan: %w", err)
	}

	pbFiles := manifestToProto(manifest)

	diffResp, err := client.Diff(ctx, &pb.WorkspaceManifest{
		RootHash: manifest.RootHash,
		Files:    pbFiles,
	})
	if err != nil {
		return "", fmt.Errorf("UploadWorkspace diff: %w", err)
	}

	if !force && len(diffResp.MissingHashes) == 0 && diffResp.ExistingSnapshotRef != "" {
		_ = saveLocalCache(dir, &localManifestCache{
			RootHash:    manifest.RootHash,
			SnapshotRef: diffResp.ExistingSnapshotRef,
			UploadedAt:  time.Now(),
		})
		return diffResp.ExistingSnapshotRef, nil
	}

	hashIdx := buildHashIndex(manifest)
	uniqueHashes := make([]string, 0, len(diffResp.MissingHashes))
	seenUpload := make(map[string]bool, len(diffResp.MissingHashes))
	for _, h := range diffResp.MissingHashes {
		if !seenUpload[h] {
			seenUpload[h] = true
			uniqueHashes = append(uniqueHashes, h)
		}
	}

	transport := &http.Transport{
		MaxIdleConns:        64,
		MaxIdleConnsPerHost: 32,
		IdleConnTimeout:     90 * time.Second,
	}
	httpClient := &http.Client{
		Transport: transport,
		Timeout:   5 * time.Minute,
	}

	concurrency := 8
	if len(uniqueHashes) < concurrency {
		concurrency = len(uniqueHashes)
	}
	if concurrency <= 0 {
		concurrency = 1
	}

	jobs := make(chan string, len(uniqueHashes))
	for _, h := range uniqueHashes {
		jobs <- h
	}
	close(jobs)

	errCh := make(chan error, concurrency)
	var wg sync.WaitGroup

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for hash := range jobs {
				select {
				case <-ctx.Done():
					select {
					case errCh <- ctx.Err():
					default:
					}
					return
				default:
				}

				url := diffResp.PresignedPutUrls[hash]
				if url == "" {
					select {
					case errCh <- fmt.Errorf("UploadWorkspace: no presigned URL for hash %s", hash[:8]):
					default:
					}
					return
				}

				data, err := readChunkByHashFromIndex(dir, hashIdx, hash)
				if err != nil {
					select {
					case errCh <- fmt.Errorf("UploadWorkspace read chunk %s: %w", hash[:8], err):
					default:
					}
					return
				}

				req, err := http.NewRequestWithContext(ctx, http.MethodPut, url, bytes.NewReader(data))
				if err != nil {
					select {
					case errCh <- fmt.Errorf("UploadWorkspace build request %s: %w", hash[:8], err):
					default:
					}
					return
				}
				req.ContentLength = int64(len(data))

				resp, err := httpClient.Do(req)
				if err != nil {
					select {
					case errCh <- fmt.Errorf("UploadWorkspace upload chunk %s: %w", hash[:8], err):
					default:
					}
					return
				}
				_ = resp.Body.Close()
				if resp.StatusCode >= 300 {
					select {
					case errCh <- fmt.Errorf("UploadWorkspace upload chunk %s: status %d", hash[:8], resp.StatusCode):
					default:
					}
					return
				}
			}
		}()
	}

	wg.Wait()
	close(errCh)
	if err, ok := <-errCh; ok && err != nil {
		return "", err
	}

	commitResp, err := client.Commit(ctx, &pb.CommitRequest{
		RootHash: manifest.RootHash,
		Files:    pbFiles,
	})
	if err != nil {
		return "", fmt.Errorf("UploadWorkspace commit: %w", err)
	}

	_ = saveLocalCache(dir, &localManifestCache{
		RootHash:    manifest.RootHash,
		SnapshotRef: commitResp.SnapshotRef,
		UploadedAt:  time.Now(),
	})

	return commitResp.SnapshotRef, nil
}

func manifestToProto(m *apitypes.WorkspaceManifest) []*pb.FileEntry {
	out := make([]*pb.FileEntry, len(m.Files))
	for i, f := range m.Files {
		out[i] = &pb.FileEntry{
			Path:  f.Path,
			Hash:  f.Hash,
			Size:  f.Size,
			Mode:  f.Mode,
			IsDir: f.IsDir,
			Link:  f.Link,
		}
	}
	return out
}

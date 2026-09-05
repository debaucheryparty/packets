package workspace

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
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

	pbFiles := make([]*pb.FileEntry, len(manifest.Files))
	for i, f := range manifest.Files {
		pbFiles[i] = &pb.FileEntry{
			Path:  f.Path,
			Hash:  f.Hash,
			Size:  f.Size,
			Mode:  f.Mode,
			IsDir: f.IsDir,
			Link:  f.Link,
		}
	}

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

	httpClient := &http.Client{Timeout: 5 * time.Minute}

	for _, hash := range diffResp.MissingHashes {
		url := diffResp.PresignedPutUrls[hash]
		if url == "" {
			return "", fmt.Errorf("UploadWorkspace: no presigned URL for hash %s", hash[:8])
		}

		data, err := readChunkByHash(dir, manifest, hash)
		if err != nil {
			return "", fmt.Errorf("UploadWorkspace read chunk %s: %w", hash[:8], err)
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodPut, url, bytes.NewReader(data))
		if err != nil {
			return "", fmt.Errorf("UploadWorkspace build request %s: %w", hash[:8], err)
		}
		req.ContentLength = int64(len(data))

		resp, err := httpClient.Do(req)
		if err != nil {
			return "", fmt.Errorf("UploadWorkspace upload chunk %s: %w", hash[:8], err)
		}
		resp.Body.Close()
		if resp.StatusCode >= 300 {
			return "", fmt.Errorf("UploadWorkspace upload chunk %s: status %d", hash[:8], resp.StatusCode)
		}
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

func manifestToProto(m *apitypes.WorkspaceManifest) []*pb.FileEntry { //nolint:unused
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

package workspace

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"time"

	"github.com/waris4ly/packets/internal/storage"
	"github.com/waris4ly/packets/internal/toolchain"
	"github.com/waris4ly/packets/pkg/apitypes"
	pb "github.com/waris4ly/packets/proto/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type Server struct {
	pb.UnimplementedWorkspaceServer
	store    storage.ObjectStore
	registry *toolchain.Registry
	ttl      time.Duration
}

func NewServer(store storage.ObjectStore, registry *toolchain.Registry) *Server {
	return &Server{
		store:    store,
		registry: registry,
		ttl:      15 * time.Minute,
	}
}

func ownerFromCtx(ctx context.Context) string {
	if v, ok := ctx.Value("tailscale_user").(string); ok {
		return v
	}
	return "default"
}

func (s *Server) Diff(ctx context.Context, req *pb.WorkspaceManifest) (*pb.DiffResponse, error) {
	owner := ownerFromCtx(ctx)

	snapshotKey := fmt.Sprintf("%s/manifests/%s.json", owner, req.RootHash)
	exists, err := s.store.Exists(ctx, snapshotKey)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "diff check manifest: %v", err)
	}
	if exists {
		return &pb.DiffResponse{
			ExistingSnapshotRef: req.RootHash,
		}, nil
	}

	missing := make([]string, 0)
	presignedURLs := make(map[string]string)
	for _, f := range req.Files {
		if f.Hash == "" || f.IsDir {
			continue
		}
		chunkKey := fmt.Sprintf("%s/chunks/%s", owner, f.Hash)
		chunkExists, err := s.store.Exists(ctx, chunkKey)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "diff check chunk %s: %v", f.Hash, err)
		}
		if !chunkExists {
			url, err := s.store.PresignUpload(ctx, chunkKey, s.ttl)
			if err != nil {
				return nil, status.Errorf(codes.Internal, "presign chunk %s: %v", f.Hash, err)
			}
			missing = append(missing, f.Hash)
			presignedURLs[f.Hash] = url
		}
	}

	return &pb.DiffResponse{
		MissingHashes:    missing,
		PresignedPutUrls: presignedURLs,
	}, nil
}

func (s *Server) Commit(ctx context.Context, req *pb.CommitRequest) (*pb.CommitResponse, error) {
	owner := ownerFromCtx(ctx)

	manifest := apitypes.WorkspaceManifest{
		RootHash: req.RootHash,
	}
	for _, f := range req.Files {
		manifest.Files = append(manifest.Files, apitypes.WorkspaceFile{
			Path:  f.Path,
			Hash:  f.Hash,
			Size:  f.Size,
			Mode:  f.Mode,
			IsDir: f.IsDir,
			Link:  f.Link,
		})
	}

	data, err := json.Marshal(manifest)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "marshal manifest: %v", err)
	}

	key := fmt.Sprintf("%s/manifests/%s.json", owner, req.RootHash)
	if err := s.store.Upload(ctx, key, bytesReader(data), int64(len(data))); err != nil {
		return nil, status.Errorf(codes.Internal, "store manifest: %v", err)
	}

	return &pb.CommitResponse{SnapshotRef: req.RootHash}, nil
}

func (s *Server) Snapshot(req *pb.DownloadRequest, stream pb.Workspace_SnapshotServer) error {
	ctx := stream.Context()
	owner := ownerFromCtx(ctx)

	manifestKey := fmt.Sprintf("%s/manifests/%s.json", owner, req.SnapshotRef)
	r, err := s.store.Download(ctx, manifestKey)
	if err != nil {
		return status.Errorf(codes.NotFound, "snapshot not found: %v", err)
	}
	defer r.Close()

	var manifest apitypes.WorkspaceManifest
	if err := json.NewDecoder(r).Decode(&manifest); err != nil {
		return status.Errorf(codes.Internal, "decode manifest: %v", err)
	}

	pr, pw := io.Pipe()
	go func() {
		gz := gzip.NewWriter(pw)
		tw := tar.NewWriter(gz)
		var writeErr error
		for _, f := range manifest.Files {
			if f.Hash == "" {
				continue
			}
			chunkKey := fmt.Sprintf("%s/chunks/%s", owner, f.Hash)
			cr, cerr := s.store.Download(ctx, chunkKey)
			if cerr != nil {
				writeErr = cerr
				break
			}
			if err := tw.WriteHeader(&tar.Header{
				Name: f.Path,
				Size: f.Size,
				Mode: int64(f.Mode),
			}); err != nil {
				cr.Close()
				writeErr = err
				break
			}
			if _, err := io.Copy(tw, cr); err != nil {
				cr.Close()
				writeErr = err
				break
			}
			cr.Close()
		}
		tw.Close()
		gz.Close()
		pw.CloseWithError(writeErr)
	}()

	buf := make([]byte, 64*1024)
	for {
		n, err := pr.Read(buf)
		if n > 0 {
			if serr := stream.Send(&pb.WorkspaceChunk{Chunk: buf[:n]}); serr != nil {
				pr.Close()
				return serr
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return status.Errorf(codes.Internal, "stream snapshot: %v", err)
		}
	}
	return nil
}

func (s *Server) ResolveManifest(ctx context.Context, owner, snapshotRef string) (*apitypes.WorkspaceManifest, error) {
	key := fmt.Sprintf("%s/manifests/%s.json", owner, snapshotRef)
	r, err := s.store.Download(ctx, key)
	if err != nil {
		return nil, fmt.Errorf("ResolveManifest download: %w", err)
	}
	defer r.Close()
	var m apitypes.WorkspaceManifest
	if err := json.NewDecoder(r).Decode(&m); err != nil {
		return nil, fmt.Errorf("ResolveManifest decode: %w", err)
	}
	return &m, nil
}

func (s *Server) ChunkKey(owner, hash string) string {
	return fmt.Sprintf("%s/chunks/%s", owner, hash)
}

func bytesReader(b []byte) io.Reader {
	return &bytesReadCloser{data: b}
}

type bytesReadCloser struct {
	data   []byte
	offset int
}

func (b *bytesReadCloser) Read(p []byte) (int, error) {
	if b.offset >= len(b.data) {
		return 0, io.EOF
	}
	n := copy(p, b.data[b.offset:])
	b.offset += n
	return n, nil
}

var _ = filepath.FromSlash

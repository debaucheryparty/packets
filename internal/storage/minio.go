package storage

import (
	"context"
	"fmt"
	"io"
	"log/slog"

	"github.com/klauspost/compress/zstd"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/waris4ly/packets/pkg/apitypes"
)

type MinIOStorage struct {
	logger *slog.Logger
	client *minio.Client
	bucket string
}

func NewMinIOStorage(ctx context.Context, logger *slog.Logger, endpoint, accessKey, secretKey, bucket string) (*MinIOStorage, error) {
	client, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(accessKey, secretKey, ""),
		Secure: false,
	})
	if err != nil {
		return nil, fmt.Errorf("NewMinIOStorage: %w", err)
	}

	// create bucket if it doesn't exist
	exists, err := client.BucketExists(ctx, bucket)
	if err != nil {
		return nil, fmt.Errorf("NewMinIOStorage bucket check: %w", err)
	}
	if !exists {
		if mkErr := client.MakeBucket(ctx, bucket, minio.MakeBucketOptions{}); mkErr != nil {
			return nil, fmt.Errorf("NewMinIOStorage create bucket %s: %w", bucket, mkErr)
		}
	}

	return &MinIOStorage{logger: logger, client: client, bucket: bucket}, nil
}

func (s *MinIOStorage) Upload(ctx context.Context, ref apitypes.ArtifactRef, r io.Reader) error {
	pr, pw := io.Pipe()

	go func() {
		enc, err := zstd.NewWriter(pw)
		if err != nil {
			pw.CloseWithError(fmt.Errorf("zstd init: %w", err))
			return
		}

		if _, err := io.Copy(enc, r); err != nil {
			enc.Close()
			pw.CloseWithError(err)
			return
		}

		if err := enc.Close(); err != nil {
			pw.CloseWithError(err)
			return
		}
		pw.Close()
	}()

	_, err := s.client.PutObject(ctx, s.bucket, string(ref)+".zst", pr, -1, minio.PutObjectOptions{})
	if err != nil {
		return fmt.Errorf("MinIOStorage.Upload %s: %w", ref, err)
	}
	return nil
}

func (s *MinIOStorage) Download(ctx context.Context, ref apitypes.ArtifactRef) (io.ReadCloser, error) {
	obj, err := s.client.GetObject(ctx, s.bucket, string(ref)+".zst", minio.GetObjectOptions{})
	if err != nil {
		return nil, fmt.Errorf("MinIOStorage.Download %s: %w", ref, err)
	}

	dec, err := zstd.NewReader(obj)
	if err != nil {
		obj.Close()
		return nil, fmt.Errorf("zstd decode init: %w", err)
	}

	return &zstdReadCloser{
		Decoder: dec,
		Closer:  obj,
	}, nil
}

type zstdReadCloser struct {
	Decoder *zstd.Decoder
	Closer  io.Closer
}

func (z *zstdReadCloser) Read(p []byte) (n int, err error) {
	return z.Decoder.Read(p)
}

func (z *zstdReadCloser) Close() error {
	z.Decoder.Close()
	return z.Closer.Close()
}

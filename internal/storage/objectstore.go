package storage

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"google.golang.org/grpc/metadata"
)

type ObjectStore interface {
	Upload(ctx context.Context, key string, r io.Reader, size int64) error
	Download(ctx context.Context, key string) (io.ReadCloser, error)
	Delete(ctx context.Context, key string) error
	Exists(ctx context.Context, key string) (bool, error)
	List(ctx context.Context, prefix string) ([]string, error)
	PresignUpload(ctx context.Context, key string, ttl time.Duration) (string, error)
	PresignDownload(ctx context.Context, key string, ttl time.Duration) (string, error)
}

type s3Store struct {
	client  *s3.Client
	presign *s3.PresignClient
	bucket  string
}

func NewS3ObjectStore(endpoint, region, accessKey, secretKey, bucket string, forcePathStyle bool) (ObjectStore, error) {
	opts := []func(*config.LoadOptions) error{
		config.WithRegion(region),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(accessKey, secretKey, "")),
	}

	cfg, err := config.LoadDefaultConfig(context.Background(), opts...)
	if err != nil {
		return nil, fmt.Errorf("NewS3ObjectStore config: %w", err)
	}

	clientOpts := []func(*s3.Options){
		func(o *s3.Options) {
			o.UsePathStyle = forcePathStyle
		},
	}
	if endpoint != "" {
		clientOpts = append(clientOpts, func(o *s3.Options) {
			o.BaseEndpoint = aws.String(endpoint)
		})
	}

	client := s3.NewFromConfig(cfg, clientOpts...)
	return &s3Store{
		client:  client,
		presign: s3.NewPresignClient(client),
		bucket:  bucket,
	}, nil
}

func (s *s3Store) Upload(ctx context.Context, key string, r io.Reader, size int64) error {
	_, err := s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:        aws.String(s.bucket),
		Key:           aws.String(key),
		Body:          r,
		ContentLength: aws.Int64(size),
	})
	if err != nil {
		return fmt.Errorf("s3Store.Upload %q: %w", key, err)
	}
	return nil
}

func (s *s3Store) Download(ctx context.Context, key string) (io.ReadCloser, error) {
	out, err := s.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return nil, fmt.Errorf("s3Store.Download %q: %w", key, err)
	}
	return out.Body, nil
}

func (s *s3Store) Delete(ctx context.Context, key string) error {
	_, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return fmt.Errorf("s3Store.Delete %q: %w", key, err)
	}
	return nil
}

func (s *s3Store) Exists(ctx context.Context, key string) (bool, error) {
	_, err := s.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return false, nil
	}
	return true, nil
}

func (s *s3Store) List(ctx context.Context, prefix string) ([]string, error) {
	out, err := s.client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
		Bucket: aws.String(s.bucket),
		Prefix: aws.String(prefix),
	})
	if err != nil {
		return nil, fmt.Errorf("s3Store.List %q: %w", prefix, err)
	}
	keys := make([]string, 0, len(out.Contents))
	for _, obj := range out.Contents {
		if obj.Key != nil {
			keys = append(keys, *obj.Key)
		}
	}
	return keys, nil
}

func (s *s3Store) PresignUpload(ctx context.Context, key string, ttl time.Duration) (string, error) {
	req, err := s.presign.PresignPutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	}, s3.WithPresignExpires(ttl))
	if err != nil {
		return "", fmt.Errorf("s3Store.PresignUpload %q: %w", key, err)
	}
	return req.URL, nil
}

func (s *s3Store) PresignDownload(ctx context.Context, key string, ttl time.Duration) (string, error) {
	req, err := s.presign.PresignGetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	}, s3.WithPresignExpires(ttl))
	if err != nil {
		return "", fmt.Errorf("s3Store.PresignDownload %q: %w", key, err)
	}
	return req.URL, nil
}

type DiskStore struct {
	baseDir  string
	endpoint string
}

func NewDiskObjectStore(baseDir, endpoint string) (*DiskStore, error) {
	if err := os.MkdirAll(baseDir, 0o755); err != nil {
		return nil, fmt.Errorf("NewDiskObjectStore mkdir: %w", err)
	}
	return &DiskStore{
		baseDir:  filepath.Clean(baseDir),
		endpoint: strings.TrimRight(endpoint, "/"),
	}, nil
}

func (d *DiskStore) pathForKey(key string) (string, error) {
	clean := filepath.Clean(filepath.Join(d.baseDir, filepath.FromSlash(key)))
	cleanBase := filepath.Clean(d.baseDir)
	if !strings.HasPrefix(clean, cleanBase+string(filepath.Separator)) && clean != cleanBase {
		return "", fmt.Errorf("path traversal attempt: %s", key)
	}
	return clean, nil
}

func (d *DiskStore) Upload(ctx context.Context, key string, r io.Reader, size int64) error {
	p, err := d.pathForKey(key)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	f, err := os.Create(p)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	_, err = io.Copy(f, r)
	return err
}

func (d *DiskStore) Download(ctx context.Context, key string) (io.ReadCloser, error) {
	p, err := d.pathForKey(key)
	if err != nil {
		return nil, err
	}
	return os.Open(p)
}

func (d *DiskStore) Delete(ctx context.Context, key string) error {
	p, err := d.pathForKey(key)
	if err != nil {
		return err
	}
	return os.Remove(p)
}

func (d *DiskStore) Exists(ctx context.Context, key string) (bool, error) {
	p, err := d.pathForKey(key)
	if err != nil {
		return false, err
	}
	_, err = os.Stat(p)
	if err != nil {
		return false, nil
	}
	return true, nil
}

func (d *DiskStore) List(ctx context.Context, prefix string) ([]string, error) {
	p, err := d.pathForKey(prefix)
	if err != nil {
		return nil, err
	}
	var keys []string
	_ = filepath.Walk(p, func(path string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			rel, _ := filepath.Rel(d.baseDir, path)
			keys = append(keys, filepath.ToSlash(rel))
		}
		return nil
	})
	return keys, nil
}

func (d *DiskStore) PresignUpload(ctx context.Context, key string, ttl time.Duration) (string, error) {
	base := d.endpoint
	if md, ok := metadata.FromIncomingContext(ctx); ok {
		if auth := md.Get(":authority"); len(auth) > 0 && auth[0] != "" {
			host := auth[0]
			if colon := strings.Index(host, ":"); colon != -1 {
				host = host[:colon]
			}
			base = fmt.Sprintf("http://%s:9090", host)
		}
	}
	return fmt.Sprintf("%s/storage/%s", base, key), nil
}

func (d *DiskStore) PresignDownload(ctx context.Context, key string, ttl time.Duration) (string, error) {
	base := d.endpoint
	if md, ok := metadata.FromIncomingContext(ctx); ok {
		if auth := md.Get(":authority"); len(auth) > 0 && auth[0] != "" {
			host := auth[0]
			if colon := strings.Index(host, ":"); colon != -1 {
				host = host[:colon]
			}
			base = fmt.Sprintf("http://%s:9090", host)
		}
	}
	return fmt.Sprintf("%s/storage/%s", base, key), nil
}

func (d *DiskStore) HTTPHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		key := strings.TrimPrefix(r.URL.Path, "/storage/")
		p, err := d.pathForKey(key)
		if err != nil {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		switch r.Method {
		case http.MethodPut:
			if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			f, err := os.Create(p)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			defer func() { _ = f.Close() }()
			if _, err := io.Copy(f, r.Body); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			w.WriteHeader(http.StatusOK)
		case http.MethodGet:
			http.ServeFile(w, r, p)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	}
}

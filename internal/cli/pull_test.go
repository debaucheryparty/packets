package cli

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/debaucheryparty/packets/internal/config"
	pb "github.com/debaucheryparty/packets/proto/v1"
	"google.golang.org/grpc"
)

type mockSchedulerDownloadServer struct {
	pb.UnimplementedSchedulerServer
	payload []byte
}

func (m *mockSchedulerDownloadServer) DownloadArtifact(req *pb.DownloadArtifactRequest, stream pb.Scheduler_DownloadArtifactServer) error {
	chunkSize := 1024
	for i := 0; i < len(m.payload); i += chunkSize {
		end := i + chunkSize
		if end > len(m.payload) {
			end = len(m.payload)
		}
		if err := stream.Send(&pb.ArtifactChunk{Data: m.payload[i:end]}); err != nil {
			return err
		}
	}
	return nil
}

func TestPullAndExtractArtifact_TarGz(t *testing.T) {
	// Create sample tar.gz payload
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	fileContent := []byte("app-debug.apk binary simulated content")
	hdr := &tar.Header{
		Name: "outputs/apk/app-debug.apk",
		Mode: 0o644,
		Size: int64(len(fileContent)),
	}
	if err := tw.WriteHeader(hdr); err != nil {
		t.Fatalf("WriteHeader: %v", err)
	}
	if _, err := tw.Write(fileContent); err != nil {
		t.Fatalf("tw.Write: %v", err)
	}
	_ = tw.Close()
	_ = gw.Close()

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer lis.Close()

	grpcServer := grpc.NewServer()
	pb.RegisterSchedulerServer(grpcServer, &mockSchedulerDownloadServer{payload: buf.Bytes()})
	go func() { _ = grpcServer.Serve(lis) }()
	defer grpcServer.Stop()

	port := lis.Addr().(*net.TCPAddr).Port
	cfg := &config.Config{
		SchedulerGRPCPort: fmt.Sprintf("%d", port),
	}

	destDir := t.TempDir()
	err = PullAndExtractArtifact(context.Background(), cfg, slog.Default(), "job-tar-123", destDir)
	if err != nil {
		t.Fatalf("PullAndExtractArtifact failed: %v", err)
	}

	extractedFile := filepath.Join(destDir, "outputs", "apk", "app-debug.apk")
	data, err := os.ReadFile(extractedFile)
	if err != nil {
		t.Fatalf("read extracted file: %v", err)
	}
	if string(data) != string(fileContent) {
		t.Errorf("expected file content %q, got %q", fileContent, data)
	}
}

func TestPullAndExtractArtifact_RawBinary(t *testing.T) {
	binaryContent := []byte{0xDE, 0xAD, 0xBE, 0xEF, 0x01, 0x02, 0x03}

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer lis.Close()

	grpcServer := grpc.NewServer()
	pb.RegisterSchedulerServer(grpcServer, &mockSchedulerDownloadServer{payload: binaryContent})
	go func() { _ = grpcServer.Serve(lis) }()
	defer grpcServer.Stop()

	port := lis.Addr().(*net.TCPAddr).Port
	cfg := &config.Config{
		SchedulerGRPCPort: fmt.Sprintf("%d", port),
	}

	destDir := t.TempDir()
	err = PullAndExtractArtifact(context.Background(), cfg, slog.Default(), "job-bin-456", destDir)
	if err != nil {
		t.Fatalf("PullAndExtractArtifact failed: %v", err)
	}

	extractedFile := filepath.Join(destDir, "artifact_job-bin-456.bin")
	data, err := os.ReadFile(extractedFile)
	if err != nil {
		t.Fatalf("read extracted raw binary file: %v", err)
	}
	if !bytes.Equal(data, binaryContent) {
		t.Errorf("expected %v, got %v", binaryContent, data)
	}
}

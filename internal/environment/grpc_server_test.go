package environment

import (
	"context"
	"net"
	"testing"

	pb "github.com/debaucheryparty/packets/proto/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
)

const bufSize = 1024 * 1024

func setupTestGRPCServer(t *testing.T) (*RemoteClient, func()) {
	lis := bufconn.Listen(bufSize)
	s := grpc.NewServer()
	envServer := NewServer(NewManager())
	pb.RegisterEnvironmentServer(s, envServer)

	go func() {
		if err := s.Serve(lis); err != nil && err != grpc.ErrServerStopped {
			t.Logf("Server exited with error: %v", err)
		}
	}()

	dialer := func(context.Context, string) (net.Conn, error) {
		return lis.Dial()
	}

	conn, err := grpc.DialContext(
		context.Background(),
		"passthrough://bufnet",
		grpc.WithContextDialer(dialer),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("Failed to dial bufnet: %v", err)
	}

	client := NewRemoteClient(conn)

	cleanup := func() {
		_ = conn.Close()
		s.Stop()
		_ = lis.Close()
	}

	return client, cleanup
}

func TestRemoteEnvironment_Check(t *testing.T) {
	client, cleanup := setupTestGRPCServer(t)
	defer cleanup()

	ctx := context.Background()
	comps := []Component{
		{
			Type:       ComponentGo,
			Name:       "Go",
			Path:       "",
			Confidence: "high",
		},
		{
			Type:       ComponentPython,
			Name:       "Python",
			Path:       "service",
			Confidence: "high",
			Metadata: map[string]string{
				"package_manager": "pip",
			},
		},
	}

	report, err := client.Check(ctx, "test-proj", "/workspace/test-proj", comps)
	if err != nil {
		t.Fatalf("Remote Check failed: %v", err)
	}

	if report == nil {
		t.Fatal("Expected non-nil report")
	}

	if report.ProjectRoot != "/workspace/test-proj" {
		t.Errorf("Expected project root '/workspace/test-proj', got %q", report.ProjectRoot)
	}

	if len(report.Components) < 2 {
		t.Fatalf("Expected at least 2 components in report, got %d", len(report.Components))
	}

	goReqs, ok := report.Components[ComponentGo]
	if !ok || len(goReqs) == 0 {
		t.Errorf("Expected Go requirements in report")
	}

	pyReqs, ok := report.Components[ComponentPython]
	if !ok || len(pyReqs) == 0 {
		t.Errorf("Expected Python requirements in report")
	}
}

func TestRemoteEnvironment_Prepare(t *testing.T) {
	client, cleanup := setupTestGRPCServer(t)
	defer cleanup()

	ctx := context.Background()
	// Component without missing dependencies or empty
	comps := []Component{
		{
			Type:       ComponentGo,
			Name:       "Go",
			Path:       "",
			Confidence: "high",
		},
	}

	var progressMessages []string
	err := client.Prepare(ctx, "test-proj", "/workspace/test-proj", comps, func(msg string) {
		progressMessages = append(progressMessages, msg)
	})

	if err != nil {
		t.Fatalf("Remote Prepare failed: %v", err)
	}

	if len(progressMessages) == 0 {
		t.Errorf("Expected at least one progress message")
	}
}

func TestRemoteEnvironment_EmptyComponents(t *testing.T) {
	client, cleanup := setupTestGRPCServer(t)
	defer cleanup()

	ctx := context.Background()
	report, err := client.Check(ctx, "empty-proj", "", nil)
	if err != nil {
		t.Fatalf("Remote Check failed on empty components: %v", err)
	}

	if len(report.Components) != 0 {
		t.Errorf("Expected 0 components, got %d", len(report.Components))
	}

	var progressMessages []string
	err = client.Prepare(ctx, "empty-proj", "", nil, func(msg string) {
		progressMessages = append(progressMessages, msg)
	})
	if err != nil {
		t.Fatalf("Remote Prepare failed on empty components: %v", err)
	}
	if len(progressMessages) == 0 {
		t.Errorf("Expected fallback message on empty components")
	}
}

package scheduler

import (
	"context"
	"errors"
	"io"
	"strings"

	"github.com/debaucheryparty/packets/internal/policy"
	"github.com/debaucheryparty/packets/internal/provider"
	"github.com/debaucheryparty/packets/internal/storage"
	"github.com/debaucheryparty/packets/pkg/apitypes"
	pb "github.com/debaucheryparty/packets/proto/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

type Server struct {
	pb.UnimplementedSchedulerServer
	dispatcher   *Dispatcher
	store        *storage.JobStore
	logBroker    *LogBroker
	objectStore  storage.ObjectStore
	providers    map[apitypes.ProviderName]provider.BuildProvider
	policyEngine *policy.PolicyEngine
}

func NewServer(dispatcher *Dispatcher, store *storage.JobStore, logBroker *LogBroker, objectStore storage.ObjectStore, providers map[apitypes.ProviderName]provider.BuildProvider) *Server {
	return &Server{
		dispatcher:  dispatcher,
		store:       store,
		logBroker:   logBroker,
		objectStore: objectStore,
		providers:   providers,
	}
}

func (s *Server) SetPolicyEngine(pe *policy.PolicyEngine) {
	s.policyEngine = pe
}

func (s *Server) SubmitJob(ctx context.Context, req *pb.SubmitJobRequest) (*pb.SubmitJobResponse, error) {
	if req.CacheKey == "" {
		return nil, status.Error(codes.InvalidArgument, "cache_key is required")
	}
	if req.Toolchain == "" {
		return nil, status.Error(codes.InvalidArgument, "toolchain is required")
	}

	owner := "default"
	if u, ok := ctx.Value("tailscale_user").(string); ok && u != "" {
		owner = u
	}

	ticketID := req.GetApprovalTicket()
	projectID := req.GetProjectId()
	if md, ok := metadata.FromIncomingContext(ctx); ok {
		if ticketID == "" {
			if vals := md.Get("x-approval-ticket"); len(vals) > 0 {
				ticketID = vals[0]
			}
		}
		if projectID == "" {
			if vals := md.Get("x-project-id"); len(vals) > 0 {
				projectID = vals[0]
			}
		}
	}

	if s.policyEngine != nil {
		action := "BUILD"
		if req.Toolchain == string(apitypes.ToolchainExec) {
			action = "EXEC"
		}
		cmdName := req.Toolchain
		if len(req.CommandArgs) > 0 {
			cmdName = strings.Join(req.CommandArgs, " ")
		}
		ec := policy.ExecutionContext{
			User:         owner,
			ProjectID:    projectID,
			WorkspaceID:  projectID,
			SnapshotHash: req.SnapshotRef,
			Command:      cmdName,
			Args:         req.CommandArgs,
			Action:       action,
		}
		if s.policyEngine.RequiresApprovalFor(ec) {
			if err := s.policyEngine.ValidateAndConsumeTicket(ticketID, ec); err != nil {
				return nil, status.Errorf(codes.PermissionDenied, "policy rejection: %v", err)
			}
		}
	}

	buildReq := apitypes.BuildRequest{
		Toolchain:     apitypes.Toolchain(req.Toolchain),
		DockerImage:   req.DockerImage,
		Runner:        apitypes.RunnerName(req.Runner),
		SourceMode:    apitypes.SourceMode(req.SourceMode),
		SnapshotRef:   req.SnapshotRef,
		CommandArgs:   req.CommandArgs,
		ArtifactPaths: req.ArtifactPaths,
		ProjectID:     projectID,
	}

	jobID, hit, err := s.dispatcher.Submit(ctx, buildReq, req.CacheKey, owner)
	if err != nil {
		if errors.Is(err, ErrQuotaExceeded) || errors.Is(err, ErrRateLimitExceeded) {
			return nil, status.Errorf(codes.ResourceExhausted, "%v", err)
		}
		return nil, status.Errorf(codes.Internal, "failed to submit job: %v", err)
	}

	return &pb.SubmitJobResponse{
		JobId:    string(jobID),
		CacheHit: hit,
	}, nil
}

func (s *Server) GetJobStatus(ctx context.Context, req *pb.GetJobStatusRequest) (*pb.GetJobStatusResponse, error) {
	if req.JobId == "" {
		return nil, status.Error(codes.InvalidArgument, "job_id is required")
	}

	job, err := s.store.GetJob(ctx, apitypes.JobID(req.JobId))
	if err != nil {
		if err == storage.ErrJobNotFound { //nolint:errorlint
			return nil, status.Error(codes.NotFound, "job not found")
		}
		return nil, status.Errorf(codes.Internal, "failed to get job: %v", err)
	}

	return &pb.GetJobStatusResponse{
		State:        mapState(job.State),
		ArtifactRef:  string(job.ArtifactRef),
		ErrorMessage: job.Error,
	}, nil
}

func (s *Server) StreamJobLogs(req *pb.StreamJobLogsRequest, stream pb.Scheduler_StreamJobLogsServer) error {
	if req.JobId == "" {
		return status.Error(codes.InvalidArgument, "job_id is required")
	}

	if s.logBroker == nil {
		return status.Error(codes.Unavailable, "log streaming is not available")
	}

	ctx := stream.Context()
	jobID := apitypes.JobID(req.JobId)

	existing, ch, cleanup := s.logBroker.Subscribe(ctx, jobID)
	defer cleanup()

	for _, line := range existing {
		if err := stream.Send(&pb.JobLogLine{Content: line}); err != nil {
			return err
		}
	}

	if s.logBroker.IsClosed(jobID) {
		return nil
	}

	if job, err := s.store.GetJob(ctx, jobID); err == nil && (job.State == apitypes.JobStateSucceeded || job.State == apitypes.JobStateFailed) {
		return nil
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case line, ok := <-ch:
			if !ok {
				return nil
			}
			if err := stream.Send(&pb.JobLogLine{Content: line}); err != nil {
				return err
			}
		}
	}
}

func (s *Server) DownloadArtifact(req *pb.DownloadArtifactRequest, stream pb.Scheduler_DownloadArtifactServer) error {
	if req.JobId == "" {
		return status.Error(codes.InvalidArgument, "job_id is required")
	}

	ctx := stream.Context()
	job, err := s.store.GetJob(ctx, apitypes.JobID(req.JobId))
	if err != nil {
		projJob, projErr := s.store.GetLatestProjectArtifactJob(ctx, req.JobId)
		if projErr == nil {
			job = projJob
		} else {
			return status.Errorf(codes.NotFound, "job not found: %v", err)
		}
	}

	if job.State != apitypes.JobStateSucceeded {
		return status.Errorf(codes.FailedPrecondition, "job is in state %s, not succeeded", job.State)
	}

	if job.Runner == apitypes.RunnerGitHub {
		if ghProvider, ok := s.providers[apitypes.ProviderGitHubActions]; ok {
			reader, err := ghProvider.FetchArtifact(ctx, job.ID)
			if err == nil && reader != nil {
				defer reader.Close() //nolint:errcheck
				buf := make([]byte, 64*1024)
				for {
					n, err := reader.Read(buf)
					if n > 0 {
						if sendErr := stream.Send(&pb.ArtifactChunk{Data: buf[:n]}); sendErr != nil {
							return sendErr
						}
					}
					if err == io.EOF {
						break
					}
					if err != nil {
						return status.Errorf(codes.Internal, "read artifact: %v", err)
					}
				}
				return nil
			}
		}
	}

	if string(job.ArtifactRef) == "" {
		return status.Error(codes.NotFound, "no artifact reference found for job")
	}

	if s.objectStore == nil {
		return status.Error(codes.Unavailable, "object store is not configured")
	}

	reader, err := s.objectStore.Download(ctx, string(job.ArtifactRef))
	if err != nil {
		return status.Errorf(codes.NotFound, "artifact download failed: %v", err)
	}
	defer reader.Close() //nolint:errcheck

	buf := make([]byte, 64*1024)
	for {
		n, err := reader.Read(buf)
		if n > 0 {
			if sendErr := stream.Send(&pb.ArtifactChunk{Data: buf[:n]}); sendErr != nil {
				return sendErr
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return status.Errorf(codes.Internal, "stream artifact chunk: %v", err)
		}
	}
	return nil
}

func (s *Server) ClearCache(ctx context.Context, req *pb.ClearCacheRequest) (*pb.ClearCacheResponse, error) {
	count, err := s.store.DeleteCacheEntries(ctx, req.Toolchain)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "ClearCache: %v", err)
	}
	return &pb.ClearCacheResponse{ClearedCount: int32(count)}, nil
}

func mapState(state apitypes.JobState) pb.JobState {
	switch state {
	case apitypes.JobStatePending:
		return pb.JobState_JOB_STATE_PENDING
	case apitypes.JobStateUploading:
		return pb.JobState_JOB_STATE_UPLOADING
	case apitypes.JobStateDispatched:
		return pb.JobState_JOB_STATE_DISPATCHED
	case apitypes.JobStateRunning:
		return pb.JobState_JOB_STATE_RUNNING
	case apitypes.JobStateSucceeded:
		return pb.JobState_JOB_STATE_SUCCEEDED
	case apitypes.JobStateFailed, apitypes.JobStateFallbackLocal:
		return pb.JobState_JOB_STATE_FAILED
	default:
		return pb.JobState_JOB_STATE_UNSPECIFIED
	}
}

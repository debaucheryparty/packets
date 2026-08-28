package scheduler

import (
	"context"
	"fmt"

	"github.com/waris4ly/packets/internal/storage"
	"github.com/waris4ly/packets/pkg/apitypes"
	pb "github.com/waris4ly/packets/proto/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type Server struct {
	pb.UnimplementedSchedulerServer
	dispatcher *Dispatcher
	store      *storage.JobStore
	queue      *storage.NATSQueue
}

func NewServer(dispatcher *Dispatcher, store *storage.JobStore, queue *storage.NATSQueue) *Server {
	return &Server{
		dispatcher: dispatcher,
		store:      store,
		queue:      queue,
	}
}

func (s *Server) SubmitJob(ctx context.Context, req *pb.SubmitJobRequest) (*pb.SubmitJobResponse, error) {
	if req.CacheKey == "" {
		return nil, status.Error(codes.InvalidArgument, "cache_key is required")
	}
	if req.Toolchain == "" {
		return nil, status.Error(codes.InvalidArgument, "toolchain is required")
	}

	buildReq := apitypes.BuildRequest{
		Toolchain:   apitypes.Toolchain(req.Toolchain),
		DockerImage: req.DockerImage,
	}

	jobID, hit, err := s.dispatcher.Submit(ctx, buildReq, req.CacheKey)
	if err != nil {
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
		if err == storage.ErrJobNotFound {
			return nil, status.Error(codes.NotFound, "job not found")
		}
		return nil, status.Errorf(codes.Internal, "failed to get job: %v", err)
	}

	return &pb.GetJobStatusResponse{
		State:       mapState(job.State),
		ArtifactRef: string(job.ArtifactRef),
	}, nil
}

func (s *Server) StreamJobLogs(req *pb.StreamJobLogsRequest, stream pb.Scheduler_StreamJobLogsServer) error {
	if req.JobId == "" {
		return status.Error(codes.InvalidArgument, "job_id is required")
	}

	if s.queue == nil {
		return status.Error(codes.FailedPrecondition, "log streaming is not configured")
	}

	ctx := stream.Context()
	logChan := make(chan string, 100)
	subject := fmt.Sprintf("job.%s.logs", req.JobId)

	err := s.queue.Subscribe(ctx, subject, func(data []byte) {
		select {
		case logChan <- string(data):
		default:
			// drop logs if client is too slow
		}
	})
	if err != nil {
		return status.Errorf(codes.Internal, "failed to subscribe to logs: %v", err)
	}

	for {
		select {
		case <-ctx.Done():
			return nil
		case line := <-logChan:
			if err := stream.Send(&pb.JobLogLine{Content: line}); err != nil {
				return err
			}
		}
	}
}

func mapState(state apitypes.JobState) pb.JobState {
	switch state {
	case apitypes.JobStatePending:
		return pb.JobState_JOB_STATE_PENDING
	case apitypes.JobStateDispatched:
		return pb.JobState_JOB_STATE_PENDING
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

func (s *Server) ClearCache(ctx context.Context, req *pb.ClearCacheRequest) (*pb.ClearCacheResponse, error) {
	// MOCK: in a real implementation we would call s.store.DeleteJobsByToolchain
	return &pb.ClearCacheResponse{ClearedCount: 42}, nil
}

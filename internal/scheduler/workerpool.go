package scheduler

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/debaucheryparty/packets/pkg/apitypes"
	pb "github.com/debaucheryparty/packets/proto/v1"
)

var (
	ErrNoWorkersAvailable = errors.New("no healthy workers available")
	ErrWorkerNotFound     = errors.New("worker not found")
)

type JobExecutor interface {
	Execute(ctx context.Context, job apitypes.Job) (apitypes.ExecutionResult, error)
}

type WorkerNode struct {
	ID            string
	Executor      JobExecutor
	Healthy       bool
	Draining      bool
	ActiveJobs    int
	MaxJobs       int
	Labels        map[string]string
	Capabilities  []string
	LastHeartbeat time.Time
}

type RemoteWorkerExecutor struct {
	workerID  string
	sendCh    chan *pb.SchedulerMsg
	pendingMu sync.Mutex
	pending   map[apitypes.JobID]chan *pb.WorkerJobResult
}

func NewRemoteWorkerExecutor(workerID string, sendCh chan *pb.SchedulerMsg) *RemoteWorkerExecutor {
	return &RemoteWorkerExecutor{
		workerID: workerID,
		sendCh:   sendCh,
		pending:  make(map[apitypes.JobID]chan *pb.WorkerJobResult),
	}
}

func (r *RemoteWorkerExecutor) Execute(ctx context.Context, job apitypes.Job) (apitypes.ExecutionResult, error) {
	resultCh := make(chan *pb.WorkerJobResult, 1)
	r.pendingMu.Lock()
	r.pending[job.ID] = resultCh
	r.pendingMu.Unlock()

	defer func() {
		r.pendingMu.Lock()
		delete(r.pending, job.ID)
		r.pendingMu.Unlock()
	}()

	assignMsg := &pb.SchedulerMsg{
		Payload: &pb.SchedulerMsg_Assign{
			Assign: &pb.JobAssignment{
				JobId:       string(job.ID),
				Toolchain:   string(job.Toolchain),
				CommandArgs: job.CommandArgs,
				SnapshotRef: job.SnapshotRef,
				DockerImage: job.Image,
				Runner:      string(job.Runner),
			},
		},
	}

	select {
	case r.sendCh <- assignMsg:
	case <-ctx.Done():
		return apitypes.ExecutionResult{}, ctx.Err()
	}

	select {
	case res := <-resultCh:
		if res == nil {
			return apitypes.ExecutionResult{}, errors.New("remote worker disconnected")
		}
		var err error
		if res.ErrorMessage != "" {
			err = errors.New(res.ErrorMessage)
		}
		return apitypes.ExecutionResult{
			ExitCode: int(res.ExitCode),
			Stdout:   res.Stdout,
			Stderr:   res.Stderr,
			Error:    err,
		}, nil
	case <-ctx.Done():
		cancelMsg := &pb.SchedulerMsg{
			Payload: &pb.SchedulerMsg_Cancel{
				Cancel: &pb.JobCancellation{JobId: string(job.ID)},
			},
		}
		select {
		case r.sendCh <- cancelMsg:
		default:
		}
		return apitypes.ExecutionResult{}, ctx.Err()
	}
}

func (r *RemoteWorkerExecutor) DeliverResult(res *pb.WorkerJobResult) {
	r.pendingMu.Lock()
	defer r.pendingMu.Unlock()
	if ch, ok := r.pending[apitypes.JobID(res.JobId)]; ok {
		select {
		case ch <- res:
		default:
		}
	}
}

func (r *RemoteWorkerExecutor) Close() {
	r.pendingMu.Lock()
	defer r.pendingMu.Unlock()
	for _, ch := range r.pending {
		close(ch)
	}
	r.pending = make(map[apitypes.JobID]chan *pb.WorkerJobResult)
}

type WorkerPoolMetrics struct {
	TotalWorkers    int
	HealthyWorkers  int
	DrainingWorkers int
	TotalActiveJobs int
	TotalCapacity   int
}

type WorkerPool struct {
	mu         sync.RWMutex
	logger     *slog.Logger
	dispatcher *Dispatcher
	sem        chan struct{}
	workers    map[string]*WorkerNode
}

func NewWorkerPool(logger *slog.Logger, dispatcher *Dispatcher, concurrency int) *WorkerPool {
	if concurrency <= 0 {
		concurrency = 10
	}
	return &WorkerPool{
		logger:     logger,
		dispatcher: dispatcher,
		sem:        make(chan struct{}, concurrency),
		workers:    make(map[string]*WorkerNode),
	}
}

func (w *WorkerPool) RegisterWorker(node *WorkerNode) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if node.MaxJobs <= 0 {
		node.MaxJobs = 5
	}
	if node.LastHeartbeat.IsZero() {
		node.LastHeartbeat = time.Now().UTC()
	}
	w.workers[node.ID] = node
}

func (w *WorkerPool) GetWorker(id string) (*WorkerNode, bool) {
	w.mu.RLock()
	defer w.mu.RUnlock()
	node, ok := w.workers[id]
	return node, ok
}

func (w *WorkerPool) UnregisterWorker(id string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	delete(w.workers, id)
}

func (w *WorkerPool) SetWorkerHealth(id string, healthy bool) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	node, ok := w.workers[id]
	if !ok {
		return ErrWorkerNotFound
	}
	node.Healthy = healthy
	return nil
}

func (w *WorkerPool) Heartbeat(id string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	node, ok := w.workers[id]
	if !ok {
		return ErrWorkerNotFound
	}
	node.LastHeartbeat = time.Now().UTC()
	return nil
}

func (w *WorkerPool) DrainWorker(id string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	node, ok := w.workers[id]
	if !ok {
		return ErrWorkerNotFound
	}
	node.Draining = true
	return nil
}

func (w *WorkerPool) CleanupStaleWorkers(timeout time.Duration) []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	now := time.Now().UTC()
	var removed []string
	for id, node := range w.workers {
		if now.Sub(node.LastHeartbeat) > timeout {
			delete(w.workers, id)
			removed = append(removed, id)
		}
	}
	return removed
}

func (w *WorkerPool) Metrics() WorkerPoolMetrics {
	w.mu.RLock()
	defer w.mu.RUnlock()

	var m WorkerPoolMetrics
	m.TotalWorkers = len(w.workers)
	for _, node := range w.workers {
		if node.Healthy && !node.Draining {
			m.HealthyWorkers++
		}
		if node.Draining {
			m.DrainingWorkers++
		}
		m.TotalActiveJobs += node.ActiveJobs
		m.TotalCapacity += node.MaxJobs
	}
	return m
}

func (w *WorkerPool) SelectWorker(toolchain apitypes.Toolchain) (*WorkerNode, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	var best *WorkerNode
	minActive := int(^uint(0) >> 1)

	for _, node := range w.workers {
		if !node.Healthy || node.Draining {
			continue
		}
		if node.ActiveJobs >= node.MaxJobs {
			continue
		}
		if node.ActiveJobs < minActive {
			minActive = node.ActiveJobs
			best = node
		}
	}

	if best == nil {
		return nil, ErrNoWorkersAvailable
	}
	best.ActiveJobs++
	return best, nil
}

func (w *WorkerPool) ReleaseWorker(id string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if node, ok := w.workers[id]; ok {
		if node.ActiveJobs > 0 {
			node.ActiveJobs--
		}
	}
}

func (w *WorkerPool) ExecuteOnWorker(ctx context.Context, job apitypes.Job) (apitypes.ExecutionResult, string, error) {
	const maxRetries = 2
	var lastErr error
	var lastWorkerID string

	for attempt := 0; attempt <= maxRetries; attempt++ {
		node, err := w.SelectWorker(job.Toolchain)
		if err != nil {
			if lastErr != nil {
				return apitypes.ExecutionResult{}, lastWorkerID, lastErr
			}
			return apitypes.ExecutionResult{}, "", err
		}
		lastWorkerID = node.ID

		if node.Executor == nil {
			w.ReleaseWorker(node.ID)
			_ = w.SetWorkerHealth(node.ID, false)
			lastErr = fmt.Errorf("worker %s executor unconfigured", node.ID)
			continue
		}

		res, err := node.Executor.Execute(ctx, job)
		w.ReleaseWorker(node.ID)

		if err != nil && ctx.Err() == nil {
			_ = w.SetWorkerHealth(node.ID, false)
			lastErr = err
			continue
		}

		return res, node.ID, err
	}

	return apitypes.ExecutionResult{}, lastWorkerID, lastErr
}

func (w *WorkerPool) WorkerCount() int {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return len(w.workers)
}

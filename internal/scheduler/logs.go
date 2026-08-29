package scheduler

import (
	"context"
	"sync"

	"github.com/waris4ly/packets/pkg/apitypes"
)

type LogBroker struct {
	mu          sync.RWMutex
	buffers     map[apitypes.JobID][]string
	subscribers map[apitypes.JobID]map[chan string]struct{}
	closed      map[apitypes.JobID]bool
}

func NewLogBroker() *LogBroker {
	return &LogBroker{
		buffers:     make(map[apitypes.JobID][]string),
		subscribers: make(map[apitypes.JobID]map[chan string]struct{}),
		closed:      make(map[apitypes.JobID]bool),
	}
}

func (b *LogBroker) Publish(jobID apitypes.JobID, line string) {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.buffers[jobID] = append(b.buffers[jobID], line)

	subs, ok := b.subscribers[jobID]
	if !ok {
		return
	}

	for ch := range subs {
		select {
		case ch <- line:
		default:
		}
	}
}

func (b *LogBroker) Subscribe(ctx context.Context, jobID apitypes.JobID) ([]string, <-chan string, func()) {
	b.mu.Lock()
	defer b.mu.Unlock()

	existing := make([]string, len(b.buffers[jobID]))
	copy(existing, b.buffers[jobID])

	ch := make(chan string, 100)
	if _, ok := b.subscribers[jobID]; !ok {
		b.subscribers[jobID] = make(map[chan string]struct{})
	}
	b.subscribers[jobID][ch] = struct{}{}

	cleanup := func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		if subs, ok := b.subscribers[jobID]; ok {
			delete(subs, ch)
			if len(subs) == 0 {
				delete(b.subscribers, jobID)
			}
		}
		close(ch)
	}

	return existing, ch, cleanup
}

func (b *LogBroker) CloseJob(jobID apitypes.JobID) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.closed[jobID] = true
}

func (b *LogBroker) IsClosed(jobID apitypes.JobID) bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.closed[jobID]
}

func (b *LogBroker) GetLogs(jobID apitypes.JobID) []string {
	b.mu.RLock()
	defer b.mu.RUnlock()
	cpy := make([]string, len(b.buffers[jobID]))
	copy(cpy, b.buffers[jobID])
	return cpy
}

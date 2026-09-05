package scheduler

import (
	"errors"
	"fmt"
	"sync"
	"time"
)

var (
	ErrQuotaExceeded     = errors.New("concurrent job quota exceeded")
	ErrRateLimitExceeded = errors.New("rate limit exceeded")
)

type userState struct {
	activeCount int
	tokens      float64
	lastRefill  time.Time
}

type QuotaLimiter struct {
	mu           sync.Mutex
	maxActive    int
	ratePerMin   float64
	users        map[string]*userState
	cleanupTimer *time.Timer //nolint:unused
}

func NewQuotaLimiter(maxActive int, ratePerMin int) *QuotaLimiter {
	if maxActive <= 0 {
		maxActive = 5
	}
	if ratePerMin <= 0 {
		ratePerMin = 60
	}
	return &QuotaLimiter{
		maxActive:  maxActive,
		ratePerMin: float64(ratePerMin),
		users:      make(map[string]*userState),
	}
}

func (q *QuotaLimiter) Acquire(owner string) error {
	if owner == "" {
		owner = "default"
	}

	q.mu.Lock()
	defer q.mu.Unlock()

	now := time.Now()
	st, exists := q.users[owner]
	if !exists {
		st = &userState{
			tokens:     q.ratePerMin,
			lastRefill: now,
		}
		q.users[owner] = st
	} else {
		elapsed := now.Sub(st.lastRefill).Seconds()
		st.tokens += elapsed * (q.ratePerMin / 60.0)
		if st.tokens > q.ratePerMin {
			st.tokens = q.ratePerMin
		}
		st.lastRefill = now
	}

	if st.tokens < 1.0 {
		return fmt.Errorf("%w: user %s has exceeded submission rate limit", ErrRateLimitExceeded, owner)
	}

	if st.activeCount >= q.maxActive {
		return fmt.Errorf("%w: user %s has %d active jobs (max %d)", ErrQuotaExceeded, owner, st.activeCount, q.maxActive)
	}

	st.tokens -= 1.0
	st.activeCount++
	return nil
}

func (q *QuotaLimiter) Release(owner string) {
	if owner == "" {
		owner = "default"
	}

	q.mu.Lock()
	defer q.mu.Unlock()

	st, exists := q.users[owner]
	if !exists {
		return
	}

	if st.activeCount > 0 {
		st.activeCount--
	}
}

func (q *QuotaLimiter) ActiveCount(owner string) int {
	if owner == "" {
		owner = "default"
	}

	q.mu.Lock()
	defer q.mu.Unlock()

	if st, exists := q.users[owner]; exists {
		return st.activeCount
	}
	return 0
}

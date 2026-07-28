package server

import (
	"context"
	"errors"
	"sync"
	"time"
)

var (
	ErrQueueFull   = errors.New("QUERY_QUEUE_FULL: execution queue is full")
	ErrRateLimited = errors.New("RATE_LIMITED: at most 10 executions per minute are allowed")
)

type executionGate struct {
	semaphore chan struct{}
	mu        sync.Mutex
	queued    int
	started   []time.Time
	now       func() time.Time
}

func newExecutionGate() *executionGate {
	return &executionGate{semaphore: make(chan struct{}, 1), now: time.Now}
}

func (g *executionGate) acquire(ctx context.Context) (func(), error) {
	now := g.now()
	g.mu.Lock()
	g.prune(now)
	if len(g.started) >= 10 {
		g.mu.Unlock()
		return nil, ErrRateLimited
	}
	select {
	case g.semaphore <- struct{}{}:
		g.started = append(g.started, now)
		g.mu.Unlock()
		return func() { <-g.semaphore }, nil
	default:
		if g.queued >= 2 {
			g.mu.Unlock()
			return nil, ErrQueueFull
		}
		g.queued++
		g.mu.Unlock()
	}
	select {
	case g.semaphore <- struct{}{}:
		g.mu.Lock()
		g.queued--
		now = g.now()
		g.prune(now)
		if len(g.started) >= 10 {
			g.mu.Unlock()
			<-g.semaphore
			return nil, ErrRateLimited
		}
		g.started = append(g.started, now)
		g.mu.Unlock()
		return func() { <-g.semaphore }, nil
	case <-ctx.Done():
		g.mu.Lock()
		g.queued--
		g.mu.Unlock()
		return nil, ctx.Err()
	}
}

func (g *executionGate) prune(now time.Time) {
	cutoff := now.Add(-time.Minute)
	kept := g.started[:0]
	for _, started := range g.started {
		if started.After(cutoff) {
			kept = append(kept, started)
		}
	}
	g.started = kept
}

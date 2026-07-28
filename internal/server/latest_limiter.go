package server

import (
	"errors"
	"sync"
	"time"
)

var errLatestDocumentsRateLimited = errors.New("LATEST_DOCUMENT_RATE_LIMITED: at most 2 global document scans per minute are allowed")

type latestDocumentsLimiter struct {
	mu      sync.Mutex
	started []time.Time
	now     func() time.Time
}

func newLatestDocumentsLimiter() *latestDocumentsLimiter {
	return &latestDocumentsLimiter{now: time.Now}
}

func (l *latestDocumentsLimiter) allow() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	cutoff := now.Add(-time.Minute)
	kept := l.started[:0]
	for _, started := range l.started {
		if started.After(cutoff) {
			kept = append(kept, started)
		}
	}
	l.started = kept
	if len(l.started) >= 2 {
		return errLatestDocumentsRateLimited
	}
	l.started = append(l.started, now)
	return nil
}

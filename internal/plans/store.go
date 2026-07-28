package plans

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"sync"
	"time"

	"github.com/codex/mcp-1c-data/internal/domain"
)

var (
	ErrNotFound = errors.New("validation plan not found")
	ErrExpired  = errors.New("validation plan expired")
	ErrUsed     = errors.New("validation plan already used")
)

type Plan struct {
	ID          string
	Query       string
	Parameters  map[string]domain.TypedValue
	Limit       int
	Fingerprint string
	Filters     []string
	ExpiresAt   time.Time
	Used        bool
}

type Store struct {
	mu    sync.Mutex
	ttl   time.Duration
	now   func() time.Time
	plans map[string]*Plan
}

func NewStore(ttl time.Duration) *Store {
	return &Store{ttl: ttl, now: time.Now, plans: make(map[string]*Plan)}
}

func (s *Store) Create(query string, parameters map[string]domain.TypedValue, limit int, fingerprint string, filters []string) (Plan, error) {
	random := make([]byte, 24)
	if _, err := rand.Read(random); err != nil {
		return Plan{}, err
	}
	now := s.now()
	plan := &Plan{
		ID:          hex.EncodeToString(random),
		Query:       query,
		Parameters:  domain.CloneParameters(parameters),
		Limit:       limit,
		Fingerprint: fingerprint,
		Filters:     append([]string(nil), filters...),
		ExpiresAt:   now.Add(s.ttl),
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, existing := range s.plans {
		if existing.ExpiresAt.Before(now) || existing.Used {
			delete(s.plans, id)
		}
	}
	s.plans[plan.ID] = plan
	return *plan, nil
}

func (s *Store) Consume(id, fingerprint string) (Plan, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	plan, ok := s.plans[id]
	if !ok || plan.Fingerprint != fingerprint {
		return Plan{}, ErrNotFound
	}
	if plan.Used {
		return Plan{}, ErrUsed
	}
	if !s.now().Before(plan.ExpiresAt) {
		delete(s.plans, id)
		return Plan{}, ErrExpired
	}
	plan.Used = true
	copy := *plan
	copy.Parameters = domain.CloneParameters(plan.Parameters)
	copy.Filters = append([]string(nil), plan.Filters...)
	return copy, nil
}

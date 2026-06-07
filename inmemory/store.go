// Package inmemory provides a thread-safe in-memory implementation of
// chronica.Store and chronica.IdempotentStore. It is intended for use in
// examples, tests, and development. It is not suitable for production
// deployments where data must survive process restarts or be shared across
// replicas.
package inmemory

import (
	"context"
	"sync"
	"time"

	"go.naturallyfunny.dev/chronica"
)

// NewStore returns a new, empty in-memory Store.
// The returned store is safe for concurrent use.
func NewStore() chronica.Store {
	return newStore()
}

// NewIdempotentStore returns a new, empty store that implements both
// chronica.Store and chronica.IdempotentStore.
func NewIdempotentStore() chronica.IdempotentStore {
	return newStore()
}

func newStore() *store {
	return &store{sessions: make(map[string]*session)}
}

type store struct {
	mu       sync.Mutex
	sessions map[string]*session
}

type session struct {
	mu        sync.Mutex
	c         chronica.Chronicum
	acta      []chronica.Actum          // ordered by insertion sequence
	idempKeys map[string]chronica.Actum // idempotencyKey → stored Actum; grows without bound — not for production use
}

// Create implements chronica.Store.
func (s *store) Create(ctx context.Context, c chronica.Chronicum) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.sessions[c.ID]; exists {
		return chronica.ErrChronicumExists
	}

	now := time.Now()
	c.StartedAt = now
	c.LastActivityAt = now
	s.sessions[c.ID] = &session{c: c}
	return nil
}

// Record implements chronica.Store.
// Appends the actum, sets At, bumps LastActivityAt, and returns the stored Actum.
func (s *store) Record(ctx context.Context, a chronica.Actum) (chronica.Actum, error) {
	s.mu.Lock()
	sess, ok := s.sessions[a.ChronicumID]
	s.mu.Unlock()

	if !ok {
		return chronica.Actum{}, chronica.ErrChronicumNotFound
	}

	sess.mu.Lock()
	defer sess.mu.Unlock()

	a.At = time.Now()
	sess.c.LastActivityAt = a.At
	sess.acta = append(sess.acta, a)
	return a, nil
}

// RecordIdempotent implements chronica.IdempotentStore.
// Each chronicum's mutex serializes concurrent calls so the deduplication
// lookup and append are atomic.
func (s *store) RecordIdempotent(ctx context.Context, a chronica.Actum, key string) (chronica.Actum, error) {
	s.mu.Lock()
	sess, ok := s.sessions[a.ChronicumID]
	s.mu.Unlock()

	if !ok {
		return chronica.Actum{}, chronica.ErrChronicumNotFound
	}

	sess.mu.Lock()
	defer sess.mu.Unlock()

	if sess.idempKeys == nil {
		sess.idempKeys = make(map[string]chronica.Actum)
	}
	if prev, hit := sess.idempKeys[key]; hit {
		return prev, nil
	}

	a.At = time.Now()
	sess.c.LastActivityAt = a.At
	sess.acta = append(sess.acta, a)
	sess.idempKeys[key] = a
	return a, nil
}

// Acta implements chronica.Store.
func (s *store) Acta(ctx context.Context, chronicumID string, q chronica.ActaQuery) ([]chronica.Actum, error) {
	s.mu.Lock()
	sess, ok := s.sessions[chronicumID]
	s.mu.Unlock()

	if !ok {
		return nil, chronica.ErrChronicumNotFound
	}

	sess.mu.Lock()
	defer sess.mu.Unlock()

	acta := sess.acta

	if len(q.Kinds) > 0 {
		kindSet := make(map[chronica.ActumKind]bool, len(q.Kinds))
		for _, k := range q.Kinds {
			kindSet[k] = true
		}
		var filtered []chronica.Actum
		for _, a := range acta {
			if kindSet[a.Kind] {
				filtered = append(filtered, a)
			}
		}
		acta = filtered
	}

	if len(q.ActorKinds) > 0 {
		actorKindSet := make(map[chronica.ActorKind]bool, len(q.ActorKinds))
		for _, k := range q.ActorKinds {
			actorKindSet[k] = true
		}
		var filtered []chronica.Actum
		for _, a := range acta {
			if actorKindSet[a.ActorKind] {
				filtered = append(filtered, a)
			}
		}
		acta = filtered
	}

	if q.LastN > 0 && len(acta) > q.LastN {
		acta = acta[len(acta)-q.LastN:]
	}

	result := make([]chronica.Actum, len(acta))
	copy(result, acta)
	return result, nil
}

// Get implements chronica.Store.
func (s *store) Get(ctx context.Context, chronicumID string) (chronica.Chronicum, error) {
	s.mu.Lock()
	sess, ok := s.sessions[chronicumID]
	s.mu.Unlock()

	if !ok {
		return chronica.Chronicum{}, chronica.ErrChronicumNotFound
	}

	sess.mu.Lock()
	defer sess.mu.Unlock()

	return sess.c, nil
}

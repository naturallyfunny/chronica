// Package inmemory provides a thread-safe in-memory implementation of
// chronica.Store. It is intended for use in examples, tests, and development.
// It is not suitable for production deployments where data must survive process
// restarts or be shared across replicas.
package inmemory

import (
	"context"
	"sync"
	"time"

	"go.naturallyfunny.dev/chronica"
)

// New returns a new, empty in-memory Store.
// The returned store is safe for concurrent use.
func New() chronica.Store {
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
	idempKeys map[string]chronica.Actum // idempotencyKey → stored Actum
}

// Create implements chronica.Store.
func (s *store) Create(ctx context.Context, c chronica.Chronicum) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.sessions[c.ID]; exists {
		return chronica.ErrChronicumExists
	}

	c.StartedAt = time.Now()
	s.sessions[c.ID] = &session{
		c:         c,
		idempKeys: make(map[string]chronica.Actum),
	}
	return nil
}

// Record implements chronica.Store.
// Each chronicum's mutex serializes concurrent calls, mirroring the
// SELECT … FOR UPDATE semantics required of persistent backends.
func (s *store) Record(ctx context.Context, a chronica.Actum) (chronica.Actum, error) {
	s.mu.Lock()
	sess, ok := s.sessions[a.ChronicumID]
	s.mu.Unlock()

	if !ok {
		// Should not happen if caller used CreateChronicum properly,
		// but we protect against it anyway.
		return chronica.Actum{}, chronica.ErrChronicumNotFound
	}

	sess.mu.Lock()
	defer sess.mu.Unlock()

	if a.IdempotencyKey != "" {
		if prev, hit := sess.idempKeys[a.IdempotencyKey]; hit {
			return prev, nil
		}
	}

	a.At = time.Now()
	sess.c.LastActivityAt = a.At
	sess.acta = append(sess.acta, a)
	if a.IdempotencyKey != "" {
		sess.idempKeys[a.IdempotencyKey] = a
	}

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
func (s *store) Get(ctx context.Context, ownerID, chronicumID string) (chronica.Chronicum, error) {
	s.mu.Lock()
	sess, ok := s.sessions[chronicumID]
	s.mu.Unlock()

	if !ok {
		return chronica.Chronicum{}, chronica.ErrChronicumNotFound
	}

	sess.mu.Lock()
	defer sess.mu.Unlock()

	if sess.c.OwnerID != ownerID {
		return chronica.Chronicum{}, chronica.ErrChronicumNotFound
	}

	return sess.c, nil
}

package chronica_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"go.naturallyfunny.dev/chronica"
	"go.naturallyfunny.dev/chronica/inmemory"
)

func TestChronicarius_RecordActum_Validation(t *testing.T) {
	ctx := context.Background()
	store := inmemory.NewStore()
	c := chronica.NewChronicarius(store)

	// 1. Empty Owner ID
	_, err := c.RecordActum(ctx, "", chronica.Actum{
		ChronicumID: "session-1",
		Kind:        chronica.ActumMessage,
		ActorKind:   chronica.ActorHuman,
		Actor:       "user-1",
		Content:     "hello",
	})
	if !errors.Is(err, chronica.ErrEmptyOwnerID) {
		t.Errorf("want ErrEmptyOwnerID, got %v", err)
	}

	// 2. Invalid Actum (Empty Actor)
	_, err = c.RecordActum(ctx, "owner-1", chronica.Actum{
		ChronicumID: "session-1",
		Kind:        chronica.ActumMessage,
		ActorKind:   chronica.ActorHuman,
		Content:     "hello",
	})
	if !errors.Is(err, chronica.ErrInvalidActum) {
		t.Errorf("want ErrInvalidActum, got %v", err)
	}
}

func TestChronicarius_RecordActum_AutoCreateAndOwnership(t *testing.T) {
	ctx := context.Background()
	store := inmemory.NewStore()
	c := chronica.NewChronicarius(store)

	actum := chronica.Actum{
		ChronicumID: "session-1",
		Kind:        chronica.ActumMessage,
		ActorKind:   chronica.ActorHuman,
		Actor:       "user-1",
		Content:     "hello",
	}

	// 1. Record actum in a non-existent session -> should auto-create
	stored, err := c.RecordActum(ctx, "owner-1", actum)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stored.ID == "" {
		t.Error("expected server-assigned ID to be non-empty")
	}

	// Verify session exists and is owned by owner-1
	session, err := c.GetChronicum(ctx, "owner-1", "session-1")
	if err != nil {
		t.Fatalf("failed to get session: %v", err)
	}
	if session.OwnerID != "owner-1" {
		t.Errorf("want OwnerID owner-1, got %s", session.OwnerID)
	}

	// 2. Record actum in same session but different owner -> should return ErrChronicumNotFound
	_, err = c.RecordActum(ctx, "owner-2", chronica.Actum{
		ChronicumID: "session-1",
		Kind:        chronica.ActumMessage,
		ActorKind:   chronica.ActorHuman,
		Actor:       "user-2",
		Content:     "hello from owner-2",
	})
	if !errors.Is(err, chronica.ErrChronicumNotFound) {
		t.Errorf("want ErrChronicumNotFound, got %v", err)
	}
}

func TestChronicarius_GetActa_OwnershipAndFilters(t *testing.T) {
	ctx := context.Background()
	store := inmemory.NewStore()
	c := chronica.NewChronicarius(store)

	// Create session and insert some events
	_, err := c.RecordActum(ctx, "owner-1", chronica.Actum{
		ChronicumID: "session-1",
		Kind:        chronica.ActumMessage,
		ActorKind:   chronica.ActorHuman,
		Actor:       "user-1",
		Content:     "msg-1",
	})
	if err != nil {
		t.Fatalf("setup RecordActum: %v", err)
	}

	_, err = c.RecordActum(ctx, "owner-1", chronica.Actum{
		ChronicumID: "session-1",
		Kind:        chronica.ActumThought,
		ActorKind:   chronica.ActorAgent,
		Actor:       "agent-1",
		Content:     "thought-1",
	})
	if err != nil {
		t.Fatalf("setup RecordActum: %v", err)
	}

	_, err = c.RecordActum(ctx, "owner-1", chronica.Actum{
		ChronicumID: "session-1",
		Kind:        chronica.ActumMessage,
		ActorKind:   chronica.ActorSystem,
		Actor:       "system",
		Content:     "sys-1",
	})
	if err != nil {
		t.Fatalf("setup RecordActum: %v", err)
	}

	// 1. Retrieve all (no filters) -> should be 3
	acta, err := c.GetActa(ctx, "owner-1", "session-1")
	if err != nil {
		t.Fatalf("GetActa: %v", err)
	}
	if len(acta) != 3 {
		t.Errorf("want 3 acta, got %d", len(acta))
	}

	// 2. Retrieve for other owner -> should return ErrChronicumNotFound
	_, err = c.GetActa(ctx, "owner-2", "session-1")
	if !errors.Is(err, chronica.ErrChronicumNotFound) {
		t.Errorf("want ErrChronicumNotFound, got %v", err)
	}

	// 3. Filter by ActumKind
	acta, err = c.GetActa(ctx, "owner-1", "session-1", chronica.WithActumKinds(chronica.ActumMessage))
	if err != nil {
		t.Fatalf("GetActa with kind filter: %v", err)
	}
	if len(acta) != 2 {
		t.Errorf("want 2 messages, got %d", len(acta))
	}

	// 4. Filter by ActorKind (exclude system)
	acta, err = c.GetActa(ctx, "owner-1", "session-1", chronica.WithActorKinds(chronica.ActorHuman, chronica.ActorAgent))
	if err != nil {
		t.Fatalf("GetActa with actor kind filter: %v", err)
	}
	if len(acta) != 2 {
		t.Errorf("want 2 human/agent acta, got %d", len(acta))
	}
	for _, a := range acta {
		if a.ActorKind == chronica.ActorSystem {
			t.Error("unexpected system actum in filtered results")
		}
	}

	// 5. WithLastN limits
	acta, err = c.GetActa(ctx, "owner-1", "session-1", chronica.WithLastN(1))
	if err != nil {
		t.Fatalf("GetActa with limit: %v", err)
	}
	if len(acta) != 1 {
		t.Fatalf("want 1 actum, got %d", len(acta))
	}
	if acta[0].Content != "sys-1" {
		t.Errorf("want last actum 'sys-1', got %q", acta[0].Content)
	}
}

func TestChronicarius_WithIDGen(t *testing.T) {
	ctx := context.Background()
	store := inmemory.NewStore()

	customID := "custom-monotonic-id-123"
	c := chronica.NewChronicarius(store, chronica.WithIDGen(func() string {
		return customID
	}))

	stored, err := c.RecordActum(ctx, "owner-1", chronica.Actum{
		ChronicumID: "session-1",
		Kind:        chronica.ActumMessage,
		ActorKind:   chronica.ActorHuman,
		Actor:       "user-1",
		Content:     "hello",
	})
	if err != nil {
		t.Fatalf("RecordActum: %v", err)
	}
	if stored.ID != customID {
		t.Errorf("want Custom ID %q, got %q", customID, stored.ID)
	}
}

func TestChronicarius_GetChronicum_Validation(t *testing.T) {
	ctx := context.Background()
	store := inmemory.NewStore()
	c := chronica.NewChronicarius(store)

	// Seed a session owned by owner-1.
	_, err := c.RecordActum(ctx, "owner-1", chronica.Actum{
		ChronicumID: "session-1",
		Kind:        chronica.ActumMessage,
		ActorKind:   chronica.ActorHuman,
		Actor:       "user-1",
		Content:     "hello",
	})
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	// Empty ownerID.
	_, err = c.GetChronicum(ctx, "", "session-1")
	if !errors.Is(err, chronica.ErrEmptyOwnerID) {
		t.Errorf("want ErrEmptyOwnerID, got %v", err)
	}

	// Non-existent session.
	_, err = c.GetChronicum(ctx, "owner-1", "session-non-existent")
	if !errors.Is(err, chronica.ErrChronicumNotFound) {
		t.Errorf("want ErrChronicumNotFound for missing session, got %v", err)
	}

	// Cross-owner access — must be indistinguishable from not-found.
	_, err = c.GetChronicum(ctx, "owner-2", "session-1")
	if !errors.Is(err, chronica.ErrChronicumNotFound) {
		t.Errorf("want ErrChronicumNotFound for cross-owner access, got %v", err)
	}

	// Correct owner succeeds.
	sess, err := c.GetChronicum(ctx, "owner-1", "session-1")
	if err != nil {
		t.Fatalf("want success for correct owner, got %v", err)
	}
	if sess.OwnerID != "owner-1" {
		t.Errorf("want OwnerID owner-1, got %s", sess.OwnerID)
	}
}

func TestChronicarius_Idempotency(t *testing.T) {
	ctx := context.Background()
	store := inmemory.NewStore()
	c := chronica.NewChronicarius(store)

	occTime := time.Now().Add(-10 * time.Minute)
	stored, err := c.RecordActum(ctx, "owner-1", chronica.Actum{
		ChronicumID: "session-1",
		Kind:        chronica.ActumMessage,
		ActorKind:   chronica.ActorHuman,
		Actor:       "user-1",
		Content:     "hello",
		OccurredAt:  occTime,
	}, chronica.WithIdempotencyKey("idem-key-1"))
	if err != nil {
		t.Fatalf("RecordActum: %v", err)
	}

	if !stored.OccurredAt.Equal(occTime) {
		t.Errorf("want OccurredAt %v, got %v", occTime, stored.OccurredAt)
	}

	// Retry with same idempotency key — stored wins, new payload discarded.
	stored2, err := c.RecordActum(ctx, "owner-1", chronica.Actum{
		ChronicumID: "session-1",
		Kind:        chronica.ActumMessage,
		ActorKind:   chronica.ActorHuman,
		Actor:       "user-1",
		Content:     "new content but same idempotency key",
		OccurredAt:  occTime,
	}, chronica.WithIdempotencyKey("idem-key-1"))
	if err != nil {
		t.Fatalf("RecordActum retry: %v", err)
	}

	if stored2.ID != stored.ID {
		t.Errorf("idempotency failed: first ID %q, second ID %q", stored.ID, stored2.ID)
	}
	if stored2.Content != "hello" {
		t.Errorf("idempotency failed to return original content: got %q", stored2.Content)
	}
}

// baseOnlyStore is a minimal Store that does NOT implement IdempotentStore.
// Used to verify that WithIdempotencyKey returns ErrIdempotencyUnsupported.
type baseOnlyStore struct {
	inner chronica.Store
}

func (b *baseOnlyStore) Create(ctx context.Context, c chronica.Chronicum) error {
	return b.inner.Create(ctx, c)
}
func (b *baseOnlyStore) Record(ctx context.Context, a chronica.Actum) (chronica.Actum, error) {
	return b.inner.Record(ctx, a)
}
func (b *baseOnlyStore) Acta(ctx context.Context, id string, q chronica.ActaQuery) ([]chronica.Actum, error) {
	return b.inner.Acta(ctx, id, q)
}
func (b *baseOnlyStore) Get(ctx context.Context, id string) (chronica.Chronicum, error) {
	return b.inner.Get(ctx, id)
}

func TestChronicarius_RecordActum_IdempotencyUnsupported(t *testing.T) {
	ctx := context.Background()
	store := &baseOnlyStore{inner: inmemory.NewStore()}
	c := chronica.NewChronicarius(store)

	_, err := c.RecordActum(ctx, "owner-1", chronica.Actum{
		ChronicumID: "session-new", // does not exist yet
		Kind:        chronica.ActumMessage,
		ActorKind:   chronica.ActorHuman,
		Actor:       "user-1",
		Content:     "hello",
	}, chronica.WithIdempotencyKey("key-1"))
	if !errors.Is(err, chronica.ErrIdempotencyUnsupported) {
		t.Errorf("want ErrIdempotencyUnsupported, got %v", err)
	}

	// No orphan chronicum must have been created as a side effect.
	_, err = c.GetChronicum(ctx, "owner-1", "session-new")
	if !errors.Is(err, chronica.ErrChronicumNotFound) {
		t.Errorf("want ErrChronicumNotFound (no orphan), got %v", err)
	}
}

func TestChronicarius_RecordActum_AutoCreateRaceCondition(t *testing.T) {
	ctx := context.Background()
	store := inmemory.NewStore()
	c := chronica.NewChronicarius(store)

	const numConcurrent = 20
	errChan := make(chan error, numConcurrent)

	// Concurrently record actum to the same non-existent session.
	// Only one should successfully create it, others should either see it exists
	// and record successfully, but all should eventually complete without returning ErrChronicumNotFound.
	for i := 0; i < numConcurrent; i++ {
		go func(idx int) {
			_, err := c.RecordActum(ctx, "owner-1", chronica.Actum{
				ChronicumID: "session-race",
				Kind:        chronica.ActumMessage,
				ActorKind:   chronica.ActorHuman,
				Actor:       fmt.Sprintf("user-%d", idx),
				Content:     fmt.Sprintf("msg-%d", idx),
			})
			errChan <- err
		}(i)
	}

	for i := 0; i < numConcurrent; i++ {
		err := <-errChan
		if err != nil {
			t.Errorf("concurrent RecordActum failed: %v", err)
		}
	}

	// Double-check total count in session-race
	acta, err := c.GetActa(ctx, "owner-1", "session-race")
	if err != nil {
		t.Fatalf("GetActa: %v", err)
	}
	if len(acta) != numConcurrent {
		t.Errorf("want %d records, got %d", numConcurrent, len(acta))
	}
}

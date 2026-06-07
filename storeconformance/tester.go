// Package storeconformance provides a conformance test suite for chronica.Store
// implementations. Each backend calls Run from its own test file.
//
// Usage:
//
//	func TestConformance(t *testing.T) {
//	    storeconformance.Run(t, func() chronica.Store {
//	        return newMyBackend(t)
//	    })
//	}
package storeconformance

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"go.naturallyfunny.dev/chronica"
)

// Run runs the full Store conformance suite.
// newStore is called once per sub-test and MUST return a fresh, empty store.
func Run(t *testing.T, newStore func() chronica.Store) {
	t.Helper()
	cases := []struct {
		name string
		fn   func(*testing.T, chronica.Store)
	}{
		{"CrossOwnerIsolation", testCrossOwnerIsolation},
		{"Idempotency", testIdempotency},
		{"IdempotencyStoredWins", testIdempotencyStoredWins},
		{"FilterThenLimit", testFilterThenLimit},
		{"FilterActorKinds", testFilterActorKinds},
		{"LastActivityAtBumps", testLastActivityAtBumps},
		{"AppendReturnsFullActum", testAppendReturnsFullActum},
		{"InsertionOrder", testInsertionOrder},
		{"ConcurrentAppend", testConcurrentAppend},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			tc.fn(t, newStore())
		})
	}
}

// makeActum builds a valid Actum with caller-supplied ID, chronicumID, content.
func makeActum(id, chronicumID, content string) chronica.Actum {
	return chronica.Actum{
		ID:          id,
		ChronicumID: chronicumID,
		Kind:        chronica.ActumMessage,
		ActorKind:   chronica.ActorHuman,
		Actor:       "test-user",
		Content:     content,
	}
}

func testCrossOwnerIsolation(t *testing.T, store chronica.Store) {
	t.Helper()
	ctx := context.Background()

	err := store.Create(ctx, chronica.Chronicum{ID: "session", OwnerID: "owner1"})
	if err != nil {
		t.Fatalf("setup Create: %v", err)
	}

	if _, err := store.Record(ctx, makeActum("id-1", "session", "hello")); err != nil {
		t.Fatalf("setup Record: %v", err)
	}

	sess, err := store.Get(ctx, "session")
	if err != nil {
		t.Fatalf("Get: want no error, got %v", err)
	}
	if sess.OwnerID != "owner1" {
		t.Errorf("Get: want OwnerID owner1, got %s", sess.OwnerID)
	}
}

func testIdempotency(t *testing.T, store chronica.Store) {
	t.Helper()
	ctx := context.Background()
	store.Create(ctx, chronica.Chronicum{ID: "cid", OwnerID: "owner"})

	a := makeActum("id-1", "cid", "hello")
	a.IdempotencyKey = "key-1"

	first, err := store.Record(ctx, a)
	if err != nil {
		t.Fatalf("first Record: %v", err)
	}

	second, err := store.Record(ctx, a)
	if err != nil {
		t.Fatalf("idempotent retry: %v", err)
	}
	if second.ID != first.ID {
		t.Errorf("ID: want %s, got %s", first.ID, second.ID)
	}
	if !second.At.Equal(first.At) {
		t.Errorf("At: want %v, got %v", first.At, second.At)
	}

	acta, err := store.Acta(ctx, "cid", chronica.ActaQuery{})
	if err != nil {
		t.Fatalf("Acta: %v", err)
	}
	if len(acta) != 1 {
		t.Errorf("want 1 actum after idempotent write, got %d", len(acta))
	}
}

func testIdempotencyStoredWins(t *testing.T, store chronica.Store) {
	t.Helper()
	ctx := context.Background()
	store.Create(ctx, chronica.Chronicum{ID: "cid", OwnerID: "owner"})

	a := makeActum("id-1", "cid", "original content")
	a.IdempotencyKey = "key-1"
	first, err := store.Record(ctx, a)
	if err != nil {
		t.Fatalf("first Record: %v", err)
	}

	a2 := makeActum("id-2", "cid", "new content — should be discarded")
	a2.IdempotencyKey = "key-1"
	second, err := store.Record(ctx, a2)
	if err != nil {
		t.Fatalf("second Record: %v", err)
	}
	if second.Content != first.Content {
		t.Errorf("stored wins: want %q, got %q", first.Content, second.Content)
	}
	if second.ID != first.ID {
		t.Errorf("stored wins ID: want %s, got %s", first.ID, second.ID)
	}
}

func testFilterThenLimit(t *testing.T, store chronica.Store) {
	t.Helper()
	ctx := context.Background()
	store.Create(ctx, chronica.Chronicum{ID: "cid", OwnerID: "owner"})

	var messageIDs []string
	for i := 0; i < 5; i++ {
		msg := makeActum(fmt.Sprintf("msg-%d", i), "cid", fmt.Sprintf("message %d", i))
		msg.Kind = chronica.ActumMessage
		stored, err := store.Record(ctx, msg)
		if err != nil {
			t.Fatalf("Record message %d: %v", i, err)
		}
		messageIDs = append(messageIDs, stored.ID)

		thought := makeActum(fmt.Sprintf("tht-%d", i), "cid", fmt.Sprintf("thought %d", i))
		thought.Kind = chronica.ActumThought
		if _, err := store.Record(ctx, thought); err != nil {
			t.Fatalf("Record thought %d: %v", i, err)
		}
	}

	acta, err := store.Acta(ctx, "cid", chronica.ActaQuery{
		LastN: 3,
		Kinds: []chronica.ActumKind{chronica.ActumMessage},
	})
	if err != nil {
		t.Fatalf("Acta: %v", err)
	}
	if len(acta) != 3 {
		t.Fatalf("want 3, got %d", len(acta))
	}
	for i, a := range acta {
		if a.Kind != chronica.ActumMessage {
			t.Errorf("acta[%d]: want message, got %s", i, a.Kind)
		}
	}
	wantIDs := messageIDs[2:]
	for i, a := range acta {
		if a.ID != wantIDs[i] {
			t.Errorf("acta[%d]: want ID %s, got %s", i, wantIDs[i], a.ID)
		}
	}
}

func testFilterActorKinds(t *testing.T, store chronica.Store) {
	t.Helper()
	ctx := context.Background()
	store.Create(ctx, chronica.Chronicum{ID: "cid", OwnerID: "owner"})

	var humanIDs []string
	for i := 0; i < 3; i++ {
		a := makeActum(fmt.Sprintf("h-%d", i), "cid", fmt.Sprintf("human message %d", i))
		a.ActorKind = chronica.ActorHuman
		stored, err := store.Record(ctx, a)
		if err != nil {
			t.Fatalf("Record human %d: %v", i, err)
		}
		humanIDs = append(humanIDs, stored.ID)

		b := makeActum(fmt.Sprintf("s-%d", i), "cid", fmt.Sprintf("system event %d", i))
		b.ActorKind = chronica.ActorSystem
		if _, err := store.Record(ctx, b); err != nil {
			t.Fatalf("Record system %d: %v", i, err)
		}

		c := makeActum(fmt.Sprintf("a-%d", i), "cid", fmt.Sprintf("agent message %d", i))
		c.ActorKind = chronica.ActorAgent
		if _, err := store.Record(ctx, c); err != nil {
			t.Fatalf("Record agent %d: %v", i, err)
		}
	}

	acta, err := store.Acta(ctx, "cid", chronica.ActaQuery{
		ActorKinds: []chronica.ActorKind{chronica.ActorHuman},
	})
	if err != nil {
		t.Fatalf("Acta Human: %v", err)
	}
	if len(acta) != 3 {
		t.Fatalf("want 3 human acta, got %d", len(acta))
	}
	for i, a := range acta {
		if a.ActorKind != chronica.ActorHuman {
			t.Errorf("acta[%d]: want ActorHuman, got %s", i, a.ActorKind)
		}
		if a.ID != humanIDs[i] {
			t.Errorf("acta[%d]: want ID %s, got %s", i, humanIDs[i], a.ID)
		}
	}

	acta, err = store.Acta(ctx, "cid", chronica.ActaQuery{
		ActorKinds: []chronica.ActorKind{chronica.ActorHuman, chronica.ActorAgent},
		LastN:      3,
	})
	if err != nil {
		t.Fatalf("Acta Human & Agent: %v", err)
	}
	if len(acta) != 3 {
		t.Fatalf("want 3 acta, got %d", len(acta))
	}
	wantIDs := []string{"a-1", "h-2", "a-2"}
	for i, a := range acta {
		if a.ID != wantIDs[i] {
			t.Errorf("acta[%d]: want ID %s, got %s", i, wantIDs[i], a.ID)
		}
	}
}

func testLastActivityAtBumps(t *testing.T, store chronica.Store) {
	t.Helper()
	ctx := context.Background()
	store.Create(ctx, chronica.Chronicum{ID: "cid", OwnerID: "owner"})

	findChronica := func(id string) chronica.Chronicum {
		t.Helper()
		c, err := store.Get(ctx, id)
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		return c
	}

	stored1, err := store.Record(ctx, makeActum("id-1", "cid", "first"))
	if err != nil {
		t.Fatalf("first Record: %v", err)
	}
	c1 := findChronica("cid")
	if !c1.LastActivityAt.Equal(stored1.At) {
		t.Errorf("after first: LastActivityAt=%v, want %v", c1.LastActivityAt, stored1.At)
	}

	time.Sleep(20 * time.Millisecond)

	stored2, err := store.Record(ctx, makeActum("id-2", "cid", "second"))
	if err != nil {
		t.Fatalf("second Record: %v", err)
	}
	c2 := findChronica("cid")
	if !c2.LastActivityAt.Equal(stored2.At) {
		t.Errorf("after second: LastActivityAt=%v, want %v", c2.LastActivityAt, stored2.At)
	}
	if !c2.LastActivityAt.After(c1.LastActivityAt) {
		t.Errorf("LastActivityAt did not advance: %v → %v", c1.LastActivityAt, c2.LastActivityAt)
	}
}

func testAppendReturnsFullActum(t *testing.T, store chronica.Store) {
	t.Helper()
	ctx := context.Background()
	store.Create(ctx, chronica.Chronicum{ID: "cid", OwnerID: "owner"})

	occurredAt := time.Now().Add(-time.Minute)
	a := chronica.Actum{
		ID:             "test-id-1",
		ChronicumID:    "cid",
		Kind:           chronica.ActumMessage,
		ActorKind:      chronica.ActorHuman,
		Actor:          "test-user",
		Content:        "hello",
		OccurredAt:     occurredAt,
		IdempotencyKey: "idem-1",
	}
	stored, err := store.Record(ctx, a)
	if err != nil {
		t.Fatalf("Record: %v", err)
	}
	if stored.ID != a.ID {
		t.Errorf("ID: want %s, got %s", a.ID, stored.ID)
	}
	if stored.At.IsZero() {
		t.Error("At is zero — Store MUST set At")
	}
	if !stored.OccurredAt.Equal(occurredAt) {
		t.Errorf("OccurredAt: want %v, got %v", occurredAt, stored.OccurredAt)
	}
	if stored.IdempotencyKey != a.IdempotencyKey {
		t.Errorf("IdempotencyKey: want %s, got %s", a.IdempotencyKey, stored.IdempotencyKey)
	}
	if stored.ChronicumID != a.ChronicumID {
		t.Errorf("ChronicumID: want %s, got %s", a.ChronicumID, stored.ChronicumID)
	}
}

func testInsertionOrder(t *testing.T, store chronica.Store) {
	t.Helper()
	ctx := context.Background()
	store.Create(ctx, chronica.Chronicum{ID: "cid", OwnerID: "owner"})

	const N = 10
	var insertedIDs []string
	for i := 0; i < N; i++ {
		stored, err := store.Record(ctx,
			makeActum(fmt.Sprintf("id-%d", i), "cid", fmt.Sprintf("msg %d", i)))
		if err != nil {
			t.Fatalf("Record %d: %v", i, err)
		}
		insertedIDs = append(insertedIDs, stored.ID)
	}

	acta, err := store.Acta(ctx, "cid", chronica.ActaQuery{})
	if err != nil {
		t.Fatalf("Acta: %v", err)
	}
	if len(acta) != N {
		t.Fatalf("want %d acta, got %d", N, len(acta))
	}
	for i, a := range acta {
		if a.ID != insertedIDs[i] {
			t.Errorf("position %d: want ID %s, got %s (insertion order violated)", i, insertedIDs[i], a.ID)
		}
	}
}

func testConcurrentAppend(t *testing.T, store chronica.Store) {
	t.Helper()
	ctx := context.Background()
	store.Create(ctx, chronica.Chronicum{ID: "cid", OwnerID: "owner"})

	const N = 40
	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		gotIDs  = make(map[string]bool, N)
		errored bool
	)
	wg.Add(N)
	for i := 0; i < N; i++ {
		i := i
		go func() {
			defer wg.Done()
			a := makeActum(fmt.Sprintf("id-%d", i), "cid", fmt.Sprintf("concurrent msg %d", i))
			stored, err := store.Record(ctx, a)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				t.Errorf("goroutine %d Record: %v", i, err)
				errored = true
				return
			}
			if gotIDs[stored.ID] {
				t.Errorf("duplicate ID %s returned from goroutine %d", stored.ID, i)
				errored = true
			}
			gotIDs[stored.ID] = true
		}()
	}
	wg.Wait()

	if errored {
		return
	}

	acta, err := store.Acta(ctx, "cid", chronica.ActaQuery{})
	if err != nil {
		t.Fatalf("Acta: %v", err)
	}
	if len(acta) != N {
		t.Errorf("want %d acta, got %d", N, len(acta))
	}
	seenIDs := make(map[string]bool, N)
	for _, a := range acta {
		if seenIDs[a.ID] {
			t.Errorf("duplicate ID %s in Acta result", a.ID)
		}
		seenIDs[a.ID] = true
	}
}

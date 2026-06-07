package chronica

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
)

// Option configures a Chronicarius instance.
type Option func(*Chronicarius)

// WithIDGen overrides the default ID generator.
// The default generates a random 128-bit hex string via crypto/rand.
// Use this to inject ULID, UUIDv7, or a deterministic generator for tests.
//
// fn MUST return globally unique values. Collisions are not detected by
// Chronicarius or the Store; a duplicate Actum.ID within a chronicum will
// be stored as a distinct record.
func WithIDGen(fn func() string) Option {
	return func(c *Chronicarius) { c.idGen = fn }
}

// Chronicarius is the policy orchestrator for session management.
// Construct one with NewChronicarius; it is safe for concurrent use.
// Input validation and owner-scoping are enforced here; persistence is
// delegated to the Store.
type Chronicarius struct {
	store Store
	idGen func() string
}

// NewChronicarius creates a Chronicarius backed by store.
func NewChronicarius(store Store, opts ...Option) *Chronicarius {
	c := &Chronicarius{store: store, idGen: defaultID}
	for _, o := range opts {
		o(c)
	}
	return c
}

func defaultID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("chronica: crypto/rand: " + err.Error())
	}
	return hex.EncodeToString(b[:])
}

// RecordOption configures a single RecordActum call.
type RecordOption func(*recordOptions)

type recordOptions struct {
	idempotencyKey string
}

// WithIdempotencyKey makes the RecordActum call retry-safe.
// If the backing Store implements IdempotentStore, a repeat call with the
// same key in the same chronicum returns the previously stored Actum without
// writing a duplicate — stored wins, new payload is discarded.
// If the Store does not implement IdempotentStore, RecordActum returns
// ErrIdempotencyUnsupported.
func WithIdempotencyKey(key string) RecordOption {
	return func(o *recordOptions) { o.idempotencyKey = key }
}

// RecordActum records an action within a chronicum.
//
// CONTRACT:
//   - ownerID MUST be non-empty; returns ErrEmptyOwnerID if empty.
//   - actum.Validate() is called before touching the store.
//   - actum.ID is always replaced with a server-assigned value.
//   - If actum.ChronicumID does not exist, it is created and bound to ownerID.
//     A failure after auto-create may leave an empty chronicum; retries are safe.
//   - If it exists under a different owner, ErrChronicumNotFound is returned
//     without writing. The caller cannot tell whether the session belongs to
//     another owner.
//   - If WithIdempotencyKey is supplied and the store implements IdempotentStore,
//     a repeat call with the same key returns the stored Actum without writing
//     a duplicate. If the store does not implement IdempotentStore,
//     ErrIdempotencyUnsupported is returned.
//   - On success, returns the fully-populated stored Actum.
func (c *Chronicarius) RecordActum(ctx context.Context, ownerID string, actum Actum, opts ...RecordOption) (Actum, error) {
	if ownerID == "" {
		return Actum{}, ErrEmptyOwnerID
	}
	if err := actum.Validate(); err != nil {
		return Actum{}, err
	}

	var o recordOptions
	for _, opt := range opts {
		opt(&o)
	}

	// Fail fast before any side effect: if a key is requested but the store
	// doesn't support idempotency, there is no point creating a chronicum.
	var is IdempotentStore
	if o.idempotencyKey != "" {
		var ok bool
		if is, ok = c.store.(IdempotentStore); !ok {
			return Actum{}, ErrIdempotencyUnsupported
		}
	}

	_, err := c.ownerGuard(ctx, ownerID, actum.ChronicumID)
	if err != nil {
		if !errors.Is(err, ErrChronicumNotFound) {
			return Actum{}, err
		}
		// Session not visible to this owner. Try to create it.
		newSession := Chronicum{ID: actum.ChronicumID, OwnerID: ownerID}
		if errCreate := c.store.Create(ctx, newSession); errCreate != nil {
			if !errors.Is(errCreate, ErrChronicumExists) {
				return Actum{}, errCreate
			}
			// Lost a concurrent create race. Re-resolve ownership:
			//   - same owner created it concurrently → proceed to Record
			//   - different owner created it         → ErrChronicumNotFound
			if _, errGet := c.ownerGuard(ctx, ownerID, actum.ChronicumID); errGet != nil {
				return Actum{}, errGet
			}
		}
	}

	actum.ID = c.idGen()

	if is != nil {
		return is.RecordIdempotent(ctx, actum, o.idempotencyKey)
	}
	return c.store.Record(ctx, actum)
}

// GetActa retrieves acta of a chronicum, scoped to ownerID.
//
// CONTRACT:
//   - ownerID MUST be non-empty; returns (nil, ErrEmptyOwnerID) if empty.
//   - If the chronicum does not exist or is not owned by ownerID, returns
//     (nil, ErrChronicumNotFound). The two cases are indistinguishable.
//   - Results are in insertion order (oldest first).
//   - WithActumKinds filters before WithLastN limits (filter-then-limit).
func (c *Chronicarius) GetActa(ctx context.Context, ownerID, chronicumID string, opts ...ActaOption) ([]Actum, error) {
	if ownerID == "" {
		return nil, ErrEmptyOwnerID
	}
	if _, err := c.ownerGuard(ctx, ownerID, chronicumID); err != nil {
		return nil, err
	}
	var o ActaOptions
	for _, opt := range opts {
		opt(&o)
	}
	return c.store.Acta(ctx, chronicumID, ActaQuery(o))
}

// GetChronicum retrieves a single Chronicum scoped to ownerID.
//
// CONTRACT:
//   - ownerID MUST be non-empty; returns ErrEmptyOwnerID if empty.
//   - If the chronicum does not exist or is not owned by ownerID, returns
//     ErrChronicumNotFound. The two cases are indistinguishable.
func (c *Chronicarius) GetChronicum(ctx context.Context, ownerID, chronicumID string) (Chronicum, error) {
	if ownerID == "" {
		return Chronicum{}, ErrEmptyOwnerID
	}
	return c.ownerGuard(ctx, ownerID, chronicumID)
}

// ownerGuard fetches the chronicum and verifies it belongs to ownerID.
// Returns ErrEmptyOwnerID if ownerID is empty.
// Returns ErrChronicumNotFound if the chronicum does not exist or belongs to a different owner.
func (c *Chronicarius) ownerGuard(ctx context.Context, ownerID, chronicumID string) (Chronicum, error) {
	if ownerID == "" {
		return Chronicum{}, ErrEmptyOwnerID
	}
	sess, err := c.store.Get(ctx, chronicumID)
	if err != nil {
		return Chronicum{}, err
	}
	if sess.OwnerID != ownerID {
		return Chronicum{}, ErrChronicumNotFound
	}
	return sess, nil
}

// =============================================================================
// Functional options for GetActa
// =============================================================================

// ActaOptions is the resolved configuration for a GetActa call.
type ActaOptions struct {
	// LastN bounds the result to the most recent N acta that satisfy Kinds and ActorKinds.
	// Zero means no limit.
	LastN int
	// Kinds restricts results to these kinds. Empty means all kinds.
	Kinds []ActumKind
	// ActorKinds restricts results to these actor kinds. Empty means all actor kinds.
	ActorKinds []ActorKind
}

// ActaOption configures a GetActa call.
type ActaOption func(*ActaOptions)

// WithLastN bounds the result to the most recent N acta that satisfy the active
// filters. Filter is applied before the limit (filter-then-limit), so LastN
// counts only acta that pass the Kinds and ActorKinds filters. The selected acta
// are still returned in chronological order (Old → New).
//
// n <= 0 means no limit; negative values are treated identically to zero.
func WithLastN(n int) ActaOption {
	return func(o *ActaOptions) {
		if n > 0 {
			o.LastN = n
		}
	}
}

// WithActumKinds filters results to the specified Actum kinds only.
func WithActumKinds(kinds ...ActumKind) ActaOption {
	return func(o *ActaOptions) { o.Kinds = kinds }
}

// WithActorKinds filters results to the specified Actor kinds only.
func WithActorKinds(kinds ...ActorKind) ActaOption {
	return func(o *ActaOptions) { o.ActorKinds = kinds }
}

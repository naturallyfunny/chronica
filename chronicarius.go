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
func WithIDGen(fn func() string) Option {
	return func(c *Chronicarius) { c.idGen = fn }
}

// Chronicarius is the policy orchestrator for session management.
// Construct one with New; it is safe for concurrent use.
// Input validation and owner-scoping are enforced here; persistence is
// delegated to the Store.
type Chronicarius struct {
	store Store
	idGen func() string
}

// New creates a Chronicarius backed by store.
func New(store Store, opts ...Option) *Chronicarius {
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

// RecordActum records an action within a chronicum.
//
// CONTRACT:
//   - ownerID MUST be non-empty; returns ErrEmptyOwnerID if empty.
//   - actum.Validate() is called before touching the store.
//   - actum.ID is always replaced with a server-assigned value.
//   - If actum.ChronicumID does not exist, it is created and bound to ownerID.
//   - If it exists under a different owner, ErrChronicumNotFound is returned
//     without writing. The caller cannot tell whether the session belongs to
//     another owner.
//   - If actum.IdempotencyKey is non-empty and was used before in the same
//     chronicum, the previously stored Actum is returned without writing a
//     duplicate — the stored actum wins, new payload is discarded.
//   - On success, returns the fully-populated stored Actum.
func (c *Chronicarius) RecordActum(ctx context.Context, ownerID string, actum Actum) (Actum, error) {
	if ownerID == "" {
		return Actum{}, ErrEmptyOwnerID
	}
	if err := actum.Validate(); err != nil {
		return Actum{}, err
	}

	_, err := c.GetChronicum(ctx, ownerID, actum.ChronicumID)
	if err != nil {
		if errors.Is(err, ErrChronicumNotFound) {
			newSession := Chronicum{
				ID:      actum.ChronicumID,
				OwnerID: ownerID,
			}
			if errCreate := c.store.Create(ctx, newSession); errCreate != nil {
				if errors.Is(errCreate, ErrChronicumExists) {
					return Actum{}, ErrChronicumNotFound
				}
				return Actum{}, errCreate
			}
		} else {
			return Actum{}, err
		}
	}

	actum.ID = c.idGen()
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
	if _, err := c.GetChronicum(ctx, ownerID, chronicumID); err != nil {
		return nil, err
	}
	var o ActaOptions
	for _, opt := range opts {
		opt(&o)
	}
	return c.store.Acta(ctx, chronicumID, ActaQuery(o))
}

// GetChronicum retrieves a single Chronicum metadata belonging to ownerID.
//
// CONTRACT:
//   - ownerID MUST be non-empty; returns ErrEmptyOwnerID if empty.
//   - If the chronicum does not exist or is not owned by ownerID, returns
//     ErrChronicumNotFound.
func (c *Chronicarius) GetChronicum(ctx context.Context, ownerID, chronicumID string) (Chronicum, error) {
	if ownerID == "" {
		return Chronicum{}, ErrEmptyOwnerID
	}
	return c.store.Get(ctx, ownerID, chronicumID)
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
func WithLastN(n int) ActaOption {
	return func(o *ActaOptions) { o.LastN = n }
}

// WithActumKinds filters results to the specified Actum kinds only.
func WithActumKinds(kinds ...ActumKind) ActaOption {
	return func(o *ActaOptions) { o.Kinds = kinds }
}

// WithActorKinds filters results to the specified Actor kinds only.
func WithActorKinds(kinds ...ActorKind) ActaOption {
	return func(o *ActaOptions) { o.ActorKinds = kinds }
}

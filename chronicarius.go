package chronica

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"time"
)

// Sentinel errors returned by Chronicarius methods and Store implementations.
// Callers match with errors.Is; wrapping with fmt.Errorf("…: %w", err) is allowed.
var (
	// ErrChronicumNotFound is returned when a chronicum does not exist or is not
	// owned by the caller. The two cases MUST be indistinguishable to avoid
	// leaking the existence of other owners' sessions.
	ErrChronicumNotFound = errors.New("chronica: chronicum not found")

	// ErrChronicumExists is returned by CreateChronicum when a session with
	// the given ID already exists.
	ErrChronicumExists = errors.New("chronica: chronicum already exists")

	// ErrInvalidActum is returned by Validate (and therefore by RecordActum)
	// when the actum is not well-formed.
	ErrInvalidActum = errors.New("chronica: invalid actum")

	// ErrEmptyOwnerID is returned when a required ownerID argument is empty.
	ErrEmptyOwnerID = errors.New("chronica: empty owner id")
)

// ActorKind represents the type of participant in a conversation.
type ActorKind string

const (
	ActorHuman  ActorKind = "human"
	ActorAgent  ActorKind = "agent"
	ActorSystem ActorKind = "system"
)

// ActumKind represents the specific nature of a recorded action.
type ActumKind string

const (
	ActumMessage      ActumKind = "message"
	ActumThought      ActumKind = "thought"
	ActumToolRequest  ActumKind = "tool_request"
	ActumToolResponse ActumKind = "tool_response"
)

// Chronicum represents a session tied to an owner.
type Chronicum struct {
	ID      string
	OwnerID string

	// StartedAt is when the session was first created.
	StartedAt time.Time

	// LastActivityAt is bumped on every successful Append. It is the field used
	// to order sessions by recency in ListChronicum.
	LastActivityAt time.Time
}

// Actum is a single recorded action within a session.
type Actum struct {
	// ID is assigned server-side by Chronicarius before each Append call.
	// Any caller-supplied value is always replaced.
	ID          string
	ChronicumID string
	Kind        ActumKind
	ActorKind   ActorKind

	// Actor is the stable identifier for the participant (e.g. user ID, agent ID).
	// Use a stable ID; display names belong in a higher layer.
	Actor   string
	Content string

	// At is an advisory wall-clock timestamp set by the Store at insert time.
	// Caller-supplied At is ignored. The insertion order of acta returned by
	// GetActa is determined by a monotonic per-chronicum sequence maintained by
	// the Store, not by At. To record the real-world event time, use OccurredAt.
	At time.Time

	// OccurredAt is an optional advisory field set by the caller to record when
	// the event happened in the real world. It does not affect ordering.
	// Zero means "not provided".
	OccurredAt time.Time

	// IdempotencyKey is an optional caller-supplied deduplication key scoped to
	// the chronicum. If non-empty and a previous RecordActum in the same
	// chronicum used the same key, the previously stored Actum is returned
	// without writing a duplicate — the stored actum wins and the new payload is
	// discarded. This makes retries safe.
	//
	// IdempotencyKey is persisted and returned in the stored Actum.
	IdempotencyKey string
}

// Validate reports whether the actum is well-formed for recording.
// Returns ErrInvalidActum (wrapped) describing the first violation found.
//
// Content MUST be non-empty. This constraint may be relaxed per-Kind in a
// future revision when structured payloads are introduced.
func (a Actum) Validate() error {
	switch {
	case a.ChronicumID == "":
		return fmt.Errorf("%w: ChronicumID is empty", ErrInvalidActum)
	case a.Actor == "":
		return fmt.Errorf("%w: Actor is empty", ErrInvalidActum)
	case a.Content == "":
		return fmt.Errorf("%w: Content is empty", ErrInvalidActum)
	}

	switch a.Kind {
	case ActumMessage, ActumThought, ActumToolRequest, ActumToolResponse:
		// valid
	default:
		return fmt.Errorf("%w: unknown Kind %q", ErrInvalidActum, a.Kind)
	}

	switch a.ActorKind {
	case ActorHuman, ActorAgent, ActorSystem:
		// valid
	default:
		return fmt.Errorf("%w: unknown ActorKind %q", ErrInvalidActum, a.ActorKind)
	}

	return nil
}

// ActaQuery holds the resolved filter/limit parameters for Store.Acta.
type ActaQuery struct {
	// LastN is the maximum number of acta to return after filtering.
	// Zero means no limit.
	LastN int
	// Kinds restricts results to these kinds. Empty means all kinds.
	Kinds []ActumKind
	// ActorKinds restricts results to these actor kinds. Empty means all actor kinds.
	ActorKinds []ActorKind
}

// Store is the persistence and transaction boundary for Chronicarius.
// Each method is one atomic unit of work.
//
// Implementations MUST serialize concurrent Record calls on the same chronicum
// (e.g. SELECT … FOR UPDATE on the chronicum row, or serializable isolation)
// so that idempotency lookup and LastActivityAt bump are atomic.
type Store interface {
	// Create persists a new chronicum.
	// Returns ErrChronicumExists if a session with the same ID already exists.
	Create(ctx context.Context, c Chronicum) error

	// Record persists a into the chronicum identified by a.ChronicumID, in one
	// transaction:
	//   - if a.IdempotencyKey != "" and the same key was used before in this
	//     chronicum → return the previously stored Actum, discard new payload;
	//   - assign a monotonic per-chronicum sequence (recommended) and set a.At
	//     to the current wall-clock time;
	//   - set the chronicum's LastActivityAt to stored.At;
	//   - return the fully-populated stored Actum.
	//
	// a.ID is already assigned and a is already validated by Chronicarius.
	Record(ctx context.Context, a Actum) (Actum, error)

	// Acta returns acta for chronicumID, in insertion order
	// (oldest first). q.Kinds filters first; q.LastN limits after filtering
	// (filter-then-limit). Zero values mean "no filter / no limit".
	Acta(ctx context.Context, chronicumID string, q ActaQuery) ([]Actum, error)

	// Get returns a single Chronicum by ID, scoped to ownerID.
	// Returns ErrChronicumNotFound if it does not exist or belongs to a different owner.
	Get(ctx context.Context, ownerID, chronicumID string) (Chronicum, error)
}

// =============================================================================
// Chronicarius orchestrator
// =============================================================================

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

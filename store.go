package chronica

import (
	"context"
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

	// ErrChronicumExists is returned by Create when a session with
	// the given ID already exists.
	ErrChronicumExists = errors.New("chronica: chronicum already exists")

	// ErrInvalidActum is returned by Validate (and therefore by RecordActum)
	// when the actum is not well-formed.
	ErrInvalidActum = errors.New("chronica: invalid actum")

	// ErrEmptyOwnerID is returned when a required ownerID argument is empty.
	ErrEmptyOwnerID = errors.New("chronica: empty owner id")

	// ErrIdempotencyUnsupported is returned by RecordActum when an
	// idempotency key is supplied but the backing Store does not implement
	// IdempotentStore.
	ErrIdempotencyUnsupported = errors.New("chronica: store does not support idempotency")
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

	// LastActivityAt is bumped on every successful Record. It is the field used
	// to track activity time.
	LastActivityAt time.Time
}

// Actum is a single recorded action within a session.
type Actum struct {
	// ID is assigned server-side by Chronicarius before each Record call.
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
	// Caller-supplied At is ignored. At MUST NOT be used to determine ordering;
	// the order of acta returned by Acta is the order in which Record was called
	// (insertion order). To record the real-world event time, use OccurredAt.
	At time.Time

	// OccurredAt is an optional advisory field set by the caller to record when
	// the event happened in the real world. It does not affect ordering.
	// Zero means "not provided".
	OccurredAt time.Time
}

// Validate reports whether the actum is well-formed for recording.
// Returns ErrInvalidActum (wrapped) describing the first violation found.
//
// Content requirements are per-Kind:
//   - ActumMessage, ActumThought: Content MUST be non-empty.
//   - ActumToolRequest, ActumToolResponse: Content MAY be empty (no-argument
//     calls and void results are legitimate).
func (a Actum) Validate() error {
	if a.ChronicumID == "" {
		return fmt.Errorf("%w: ChronicumID is empty", ErrInvalidActum)
	}
	if a.Actor == "" {
		return fmt.Errorf("%w: Actor is empty", ErrInvalidActum)
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

	switch a.Kind {
	case ActumMessage, ActumThought:
		if a.Content == "" {
			return fmt.Errorf("%w: Content is empty", ErrInvalidActum)
		}
	}

	return nil
}

// ActaQuery holds the resolved filter/limit parameters for Store.Acta.
type ActaQuery struct {
	// LastN is the maximum number of acta to return after filtering.
	// Zero or negative means no limit.
	LastN int
	// Kinds restricts results to these kinds. Empty means all kinds.
	Kinds []ActumKind
	// ActorKinds restricts results to these actor kinds. Empty means all actor kinds.
	ActorKinds []ActorKind
}

// Store is the persistence boundary for Chronicarius. It is intentionally
// primitive: each method is one atomic unit of work and carries no SDK policy.
// All SDK rules — ownership, validation, ID assignment, idempotency, ordering —
// are enforced by Chronicarius above the Store.
type Store interface {
	// Create persists a new chronicum.
	// Returns ErrChronicumExists if a session with the same ID already exists.
	Create(ctx context.Context, c Chronicum) error

	// Record appends a to the chronicum identified by a.ChronicumID, sets a.At
	// to the current wall-clock time, bumps the chronicum's LastActivityAt, and
	// returns the fully-populated stored Actum.
	//
	// a.ID is already assigned and a is already validated by Chronicarius.
	//
	// Returns ErrChronicumNotFound if a.ChronicumID does not exist.
	//
	// OWNERSHIP BOUNDARY: Record takes no ownerID and performs no ownership
	// check. Tenant isolation on the write path is enforced by Chronicarius
	// before this is called. Implementations MUST NOT be called directly
	// without that guard; doing so can write across tenants.
	Record(ctx context.Context, a Actum) (Actum, error)

	// Acta returns acta for chronicumID in insertion order (oldest first).
	// q.Kinds and q.ActorKinds filter first; q.LastN limits after filtering
	// (filter-then-limit). Zero or negative q.LastN means no limit; empty
	// filter slices mean no filter.
	// Returns ErrChronicumNotFound if chronicumID does not exist.
	// Note for backend authors: Chronicarius never calls Acta without first
	// running ownerGuard (which calls Get), so this error is unreachable
	// through the public API. The requirement exists so the Store interface
	// is fully specified for direct callers; a SQL backend must add an
	// explicit existence check rather than returning nil, nil on zero rows.
	//
	// OWNERSHIP BOUNDARY: Acta takes no ownerID and performs no ownership
	// check. Tenant isolation on the read path is enforced by Chronicarius
	// before this is called. Implementations MUST NOT be called directly
	// without that guard; doing so can return any owner's history.
	Acta(ctx context.Context, chronicumID string, q ActaQuery) ([]Actum, error)

	// Get returns a single Chronicum by ID.
	// Returns ErrChronicumNotFound if it does not exist.
	//
	// OWNERSHIP BOUNDARY: Get takes no ownerID and performs no ownership
	// check. Tenant isolation is enforced by Chronicarius before returning
	// this to the client.
	Get(ctx context.Context, chronicumID string) (Chronicum, error)
}

// IdempotentStore is an optional capability a Store MAY implement to make
// RecordActum retry-safe via a caller-supplied key. Chronicarius detects it
// by type assertion; stores that don't implement it simply don't offer
// idempotency, and RecordActum will return ErrIdempotencyUnsupported if a key
// is supplied to such a store.
type IdempotentStore interface {
	Store
	// RecordIdempotent records a, deduplicating on key within a.ChronicumID.
	// If a previous call in the same chronicum used the same key, the stored
	// Actum is returned and the new payload is discarded — stored wins.
	// Concurrent calls with the same key store exactly one actum.
	//
	// Returns ErrChronicumNotFound if a.ChronicumID does not exist.
	RecordIdempotent(ctx context.Context, a Actum, key string) (Actum, error)
}

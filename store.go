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
	// Caller-supplied At is ignored. The insertion order of acta returned by
	// Acta is determined by a monotonic per-chronicum sequence maintained by
	// the Store, not by At. To record the real-world event time, use OccurredAt.
	At time.Time

	// OccurredAt is an optional advisory field set by the caller to record when
	// the event happened in the real world. It does not affect ordering.
	// Zero means "not provided".
	OccurredAt time.Time

	// IdempotencyKey is an optional caller-supplied deduplication key scoped to
	// the chronicum. If non-empty and a previous Record call in the same
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
	// (oldest first). q.Kinds and q.ActorKinds filters first; q.LastN limits after filtering
	// (filter-then-limit). Zero values mean "no filter / no limit".
	Acta(ctx context.Context, chronicumID string, q ActaQuery) ([]Actum, error)

	// Get returns a single Chronicum by ID, scoped to ownerID.
	// Returns ErrChronicumNotFound if it does not exist or belongs to a different owner.
	Get(ctx context.Context, ownerID, chronicumID string) (Chronicum, error)
}

package chronica

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// Sentinel errors that all Chronicarius implementations MUST return.
// Wrapping with fmt.Errorf("...: %w", err) is fine; callers use errors.Is.
var (
	// ErrChronicumNotFound is returned when a chronicum does not exist or is not
	// owned by the caller. The two cases MUST be indistinguishable to avoid
	// leaking the existence of other owners' sessions.
	ErrChronicumNotFound = errors.New("chronica: chronicum not found")

	// ErrActumExists is returned when RecordActum is called with an Actum.ID
	// that has already been recorded. Acta are immutable once written.
	ErrActumExists = errors.New("chronica: actum id already exists")

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

// Chronicum represents a conversation session tied to an owner.
type Chronicum struct {
	ID      string
	OwnerID string

	// StartedAt indicates when the session commenced.
	StartedAt time.Time

	// LastActivityAt is bumped on every successful RecordActum call and is the
	// field used to order sessions by recency in ListChronicum.
	LastActivityAt time.Time
}

// Actum represents a single recorded action or message within a session.
type Actum struct {
	// ID is a unique, time-sortable identifier (UUIDv7 / ULID recommended).
	// If empty when passed to RecordActum, the implementation assigns one.
	ID          string
	ChronicumID string
	Kind        ActumKind
	ActorKind   ActorKind

	// Actor is the stable identifier for the participant (e.g. user ID, agent ID).
	// Use a stable ID so acta can be reliably grouped and filtered; display names
	// belong in a higher layer.
	Actor   string
	Content string

	// At is the exact moment the action occurred. If zero when passed to
	// RecordActum, the implementation assigns the server's current time.
	At time.Time
}

// Validate reports whether the actum is well-formed for recording.
// Implementations MUST call this (or enforce equivalent rules) in RecordActum.
// Returns ErrInvalidActum (wrapped) describing the first violation found.
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

// Chronicarius is the recorder of actum and manager of chronicum.
type Chronicarius interface {
	// RecordActum records an action within a chronicum.
	//
	// CONTRACT:
	//   - If actum.ChronicumID does not exist, implementations MUST create it
	//     and bind it to ownerID.
	//   - If it already exists under a different owner, implementations MUST
	//     return ErrChronicumNotFound and MUST NOT write. The caller cannot
	//     determine whether the session belongs to another owner.
	//   - Implementations MUST call actum.Validate() before persisting.
	//   - If actum.ID is empty, implementations MUST assign a unique,
	//     time-sortable id (UUIDv7 / ULID recommended). If non-empty,
	//     re-recording an existing id MUST be rejected with ErrActumExists —
	//     acta are immutable once written.
	//   - If actum.At is zero, implementations MUST assign the server's current
	//     time. Implementations SHOULD treat a caller-supplied At as advisory
	//     and MAY overwrite it to preserve monotonicity.
	//   - On success, implementations MUST return the fully-populated stored
	//     Actum (with assigned ID and At).
	//   - Every successful RecordActum MUST bump the chronicum's LastActivityAt.
	RecordActum(ctx context.Context, ownerID string, actum Actum) (Actum, error)

	// GetActa retrieves acta of a chronicum, scoped to ownerID.
	//
	// CONTRACT:
	//   - If the chronicum does not exist, or exists but is not owned by
	//     ownerID, implementations MUST return (nil, ErrChronicumNotFound).
	//     The two cases MUST be indistinguishable to avoid leaking the
	//     existence of other owners' sessions.
	//   - Results MUST be returned in a stable total order consistent with
	//     insertion order (Old → New). At alone is insufficient to establish
	//     this order; implementations MUST break ties deterministically.
	//     Recommended: order by (At, ID) where ID is time-sortable (UUIDv7 /
	//     ULID), or maintain a per-chronicum monotonic sequence.
	GetActa(ctx context.Context, ownerID, chronicumID string, opts ...GetOption) ([]Actum, error)

	// ListChronicum retrieves chronicum metadata belonging to an owner.
	//
	// CONTRACT:
	//   - Results MUST be ordered by (LastActivityAt DESC, ID DESC).
	//   - When ListAfter is provided, implementations MUST return only
	//     chronicum strictly after the cursor in that order (keyset pagination).
	//   - ListWithLimit bounds the page size.
	ListChronicum(ctx context.Context, ownerID string, opts ...ListOption) ([]Chronicum, error)
}

// ==============================================================================
// Functional Options for GetActa
// ==============================================================================

// GetOptions is the resolved configuration for GetActa.
//
// Implementations obtain it by passing the variadic options through
// NewGetOptions, then read its fields directly to build their query.
type GetOptions struct {
	// LastN bounds the result to the most recent N acta that satisfy the active
	// kind filter. Filtering is applied before the limit (filter-then-limit),
	// so LastN counts only acta that pass the Kinds filter.
	// The selected acta MUST still be returned in chronological order (Old → New).
	//
	// Zero (the default) means no bound: return all matching acta.
	LastN int

	// Kinds restricts results to these Actum kinds (e.g. hide thoughts).
	// Empty (the default) means no filter: return all kinds.
	Kinds []ActumKind
}

// GetOption configures GetOptions.
type GetOption func(*GetOptions)

// NewGetOptions folds opts into a resolved GetOptions value.
// Implementations call this at the top of GetActa.
func NewGetOptions(opts ...GetOption) GetOptions {
	var o GetOptions
	for _, opt := range opts {
		opt(&o)
	}
	return o
}

// WithLastN bounds the result to the most recent N acta that satisfy the active
// kind filter. Filtering is applied before the limit (filter-then-limit), so
// LastN counts only acta that pass the Kinds filter. The selected acta are still
// returned in chronological order (Old → New).
func WithLastN(n int) GetOption {
	return func(o *GetOptions) {
		o.LastN = n
	}
}

// WithActumKinds filters results to the specified Actum kinds only.
func WithActumKinds(kinds ...ActumKind) GetOption {
	return func(o *GetOptions) {
		o.Kinds = kinds
	}
}

// ==============================================================================
// Functional Options for ListChronicum
// ==============================================================================

// ListOptions is the resolved configuration for ListChronicum.
//
// Implementations obtain it by passing the variadic options through
// NewListOptions, then read its fields directly to build their query.
type ListOptions struct {
	// Limit bounds the number of chronicum returned per page.
	// Zero (the default) means no bound: return all chronicum for the owner.
	Limit int

	// AfterActivityAt and AfterID form a keyset cursor (set via ListAfter).
	// When set, only chronicum strictly after this cursor in
	// (LastActivityAt DESC, ID DESC) order are returned.
	AfterActivityAt time.Time
	AfterID         string
}

// ListOption configures ListOptions.
type ListOption func(*ListOptions)

// NewListOptions folds opts into a resolved ListOptions value.
// Implementations call this at the top of ListChronicum.
func NewListOptions(opts ...ListOption) ListOptions {
	var o ListOptions
	for _, opt := range opts {
		opt(&o)
	}
	return o
}

// ListWithLimit bounds the number of chronicum returned per page.
func ListWithLimit(n int) ListOption {
	return func(o *ListOptions) {
		o.Limit = n
	}
}

// ListAfter enables stable keyset pagination by requesting chronicum ordered
// after the given cursor. Build the cursor from the last element of the
// previous page: pass its LastActivityAt and ID.
//
// Implementations MUST return only chronicum strictly after (lastActivityAt, id)
// in (LastActivityAt DESC, ID DESC) order.
func ListAfter(lastActivityAt time.Time, id string) ListOption {
	return func(o *ListOptions) {
		o.AfterActivityAt = lastActivityAt
		o.AfterID = id
	}
}

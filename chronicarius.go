package chronica

import (
	"context"
	"time"
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
}

// Actum represents a single recorded action or message within a session.
type Actum struct {
	ID          string
	ChronicumID string
	Kind        ActumKind
	ActorKind   ActorKind
	Actor       string
	Content     string

	// At is the exact moment the action occurred.
	At time.Time
}

// Chronicarius is the recorder of actum and manager of chronicum.
type Chronicarius interface {
	RecordActum(ctx context.Context, ownerID string, actum Actum) error

	// GetActa retrieves acta of a chronicum.
	// CONTRACT: Results are always guaranteed in chronological order (Old to New).
	GetActa(ctx context.Context, chronicumID string, opts ...GetOption) ([]Actum, error)

	// ListChronicum retrieves chronicum metadata belonging to an owner.
	// CONTRACT: Results are always guaranteed in anti-chronological order (Most Recent to Old).
	ListChronicum(ctx context.Context, ownerID string, opts ...ListOption) ([]Chronicum, error)
}

// ==============================================================================
// Functional Options for GetActa
// ==============================================================================

// getOptions holds configuration for filtering Actum retrieval.
type getOptions struct {
	lastN int // AI domain terminology (replaces limit)
	kinds []ActumKind
}

type GetOption func(*getOptions)

// WithLastN takes the last N Actum.
// Internal implementation guarantees those N messages are still returned in
// correct chronological order.
func WithLastN(n int) GetOption {
	return func(o *getOptions) {
		o.lastN = n
	}
}

// WithActumKinds filters results only for specific Actum kinds (e.g.: Hide Thought).
func WithActumKinds(kinds ...ActumKind) GetOption {
	return func(o *getOptions) {
		o.kinds = kinds
	}
}

// ==============================================================================
// Functional Options for ListChronicum
// ==============================================================================

// listOptions holds configuration for paginating Chronicum retrieval.
type listOptions struct {
	limit int // For List, 'limit' is still valid since this is standard pagination UI
}

type ListOption func(*listOptions)

// ListWithLimit limits the number of sessions returned for pagination.
func ListWithLimit(n int) ListOption {
	return func(o *listOptions) {
		o.limit = n
	}
}

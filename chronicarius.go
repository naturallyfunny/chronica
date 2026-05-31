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

// GetOptions is the resolved configuration for GetActa.
//
// Implementations of Chronicarius obtain it by passing the variadic options
// through NewGetOptions, then read its fields directly to build their query.
type GetOptions struct {
	// LastN bounds the result to the most recent N acta (AI domain terminology
	// in place of "limit"). The selected acta MUST still be returned in
	// chronological order (Old to New), per the GetActa contract.
	//
	// Zero (the default) means no bound: return all acta.
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

// WithLastN takes the last N Actum.
// Internal implementation guarantees those N messages are still returned in
// correct chronological order.
func WithLastN(n int) GetOption {
	return func(o *GetOptions) {
		o.LastN = n
	}
}

// WithActumKinds filters results only for specific Actum kinds (e.g.: Hide Thought).
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
	// Limit bounds the number of chronicum returned, for standard pagination.
	// Zero (the default) means no bound: return all chronicum for the owner.
	Limit int
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

// ListWithLimit limits the number of sessions returned for pagination.
func ListWithLimit(n int) ListOption {
	return func(o *ListOptions) {
		o.Limit = n
	}
}

package chronica

import "time"

type ActorKind string

const (
	ActorHuman  ActorKind = "human"
	ActorAgent  ActorKind = "agent"
	ActorSystem ActorKind = "system"
)

type ActumKind string

const (
	ActumMessage      ActumKind = "message"
	ActumThought      ActumKind = "thought"
	ActumToolRequest  ActumKind = "tool_request"
	ActumToolResponse ActumKind = "tool_response"
)

type Chronicum struct {
	ID        string    `json:"id" db:"id"`
	OwnerID   string    `json:"owner_id" db:"owner_id"`
	CreatedAt time.Time `json:"created_at" db:"created_at"`
}

type Actum struct {
	ID          string    `json:"id" db:"id"`
	ChronicumID string    `json:"chronicum_id" db:"chronicum_id"`
	Kind        ActumKind `json:"kind" db:"kind"`
	ActorKind   ActorKind `json:"actor_kind" db:"actor_kind"`
	Actor       string    `json:"actor" db:"actor"`
	Content     string    `json:"content" db:"content"`
	CreatedAt   time.Time `json:"created_at" db:"created_at"`
}

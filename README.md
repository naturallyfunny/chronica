```
______________  _____________________   _______________________ 
__  ____/__  / / /__  __ \_  __ \__  | / /___  _/_  ____/__    |
_  /    __  /_/ /__  /_/ /  / / /_   |/ / __  / _  /    __  /| |
/ /___  _  __  / _  _, _// /_/ /_  /|  / __/ /  / /___  _  ___ |
\____/  /_/ /_/  /_/ |_| \____/ /_/ |_/  /___/  \____/  /_/  |_|
```

A lightweight, 1 Human to N Agent Session System SDK designed for Single-Agent to Multi-Agent Agentic AI ecosystems.

Chronica treats AI conversations not just as text messages, but as a stream of **Actions (Actum)** within a **Session (Chronicum)**. It is built with pure Go idioms, functional options, and strict chronological contracts. 

## Key Features

- **Agent-First Design:** Natively supports various AI actions (`Message`, `Thought`, `ToolRequest`, `ToolResponse`) beyond just standard text.
- **Identity Flexibility:** Agnostic to your identity provider. It uses raw string identifiers (`OwnerID` and `Actor`) to map sessions and actors.
- **Strict Chronological Contracts:** `GetActa` always returns acta in a stable total order consistent with insertion order (Old → New) for LLM context windows. `ListChronicum` always returns sessions in anti-chronological order (New → Old) for UI listing.
- **Ownership Enforcement:** The SDK is the authorization boundary. Both `GetActa` and `RecordActum` are scoped to an `ownerID`; cross-tenant reads and writes are rejected.
- **Sliding Window Context:** Built-in `WithLastN` applies filter-then-limit, so `LastN` counts only acta that pass the kind filter.

## Core Concepts

* **`Chronicum`**: A 1-Human-to-N-Agents session tied to an owner.
* **`Actum`**: A single recorded action (e.g., message, thought) within a session.
* **`Chronicarius`**: The core interface acting as the recorder of history.

> **Note on Naming:** `Chronica` and `Acta` are simply the Latin plural forms of `Chronicum` and `Actum`. You will see these used throughout the SDK's package name and slice returns (e.g., `[]Actum` is referred to as Acta).

## Installation

```bash
go get go.naturallyfunny.dev/chronica
```

## Quick Start

### 1. Recording an Action (RecordActum)

`RecordActum` acts as an upsert for the chronicum. If `ChronicumID` does not exist, the implementation creates it and binds it to `ownerID`. On success it returns the fully-populated `Actum` with any server-assigned `ID` and `At`.

```go
import "go.naturallyfunny.dev/chronica"

// Example: An AI agent replying to a user
actum := chronica.Actum{
    ChronicumID: "session-123",
    Kind:        chronica.ActumMessage,
    ActorKind:   chronica.ActorAgent,
    Actor:       "agent-abc-456", // Use a stable ID; display names belong in a higher layer
    Content:     "Hello, how can I help you today?",
}

stored, err := chronicarius.RecordActum(ctx, "user-999", actum)
```

### 2. Retrieving Context for AI (GetActa)

Retrieve the history for an LLM prompt, scoped to an owner. Use functional options to filter specific actum kinds and limit the context window. `WithLastN` applies filter-then-limit: it counts only acta that pass the kind filter.

```go
// Fetch the last 20 messages (excluding thoughts), scoped to user-999
acta, err := chronicarius.GetActa(ctx, "user-999", "session-123",
    chronica.WithLastN(20),
    chronica.WithActumKinds(chronica.ActumMessage, chronica.ActumToolResponse),
)
// retrieved acta are guaranteed to be in chronological order (Old → New)
```

### 3. Listing Sessions for UI (ListChronicum)

Retrieve a paginated list of sessions belonging to a specific user, ordered by most recent activity. Use `ListAfter` for stable keyset pagination.

```go
// Fetch the 10 most recently active sessions
sessions, err := chronicarius.ListChronicum(ctx, "user-999",
    chronica.ListWithLimit(10),
)
// sessions are ordered by (LastActivityAt DESC, ID DESC)

// Fetch the next page using a keyset cursor
if len(sessions) > 0 {
    last := sessions[len(sessions)-1]
    nextPage, err := chronicarius.ListChronicum(ctx, "user-999",
        chronica.ListWithLimit(10),
        chronica.ListAfter(last.LastActivityAt, last.ID),
    )
}
```

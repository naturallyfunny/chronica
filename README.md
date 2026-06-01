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
- **Strict Chronological Contracts:** `GetActa` always returns acta in a stable total order consistent with insertion order (Old → New) for LLM context windows.
- **Ownership Enforcement:** The SDK is the authorization boundary. Both `GetActa` and `RecordActum` are scoped to an `ownerID` (the session owner); cross-tenant reads and writes are rejected.
- **Sliding Window Context:** Built-in `WithLastN(n)` applies filter-then-limit, so `LastN` counts only acta that pass both the kind and actor kind filters. Values `<= 0` mean no limit.
- **Actor Kind Filtering:** Supports filtering activities by actor types (`ActorHuman`, `ActorAgent`, `ActorSystem`), allowing LLM contexts to easily exclude internal thoughts or system events.
- **Smart Core, Dumb Edges:** Logic for ownership verification, ID assignment, validation, and find-or-create orchestration is housed inside the core SDK (`Chronicarius`), keeping database implementations (`Store`) simple and query-efficient.

## Core Concepts

* **`Chronicum`**: A 1-Human-to-N-Agents session tied to an owner.
* **`Actum`**: A single recorded action (e.g., message, thought) within a session.
* **`Chronicarius`**: The concrete orchestrator that enforces validation and ownership policies. Construct with `chronica.NewChronicarius(store)`.
* **`Store`**: The interface backends implement — four atomic, coarse-grained methods (`Create`, `Record`, `Acta`, `Get`).

> **Note on Naming:** `Chronica` and `Acta` are simply the Latin plural forms of `Chronicum` and `Actum`. You will see these used throughout the SDK's package name and slice returns (e.g., `[]Actum` is referred to as Acta).

## Installation

```bash
go get go.naturallyfunny.dev/chronica
```

## Quick Start

### 0. Pick (or implement) a Store

The SDK ships a thread-safe in-memory store for development and tests:

```go
import (
    "go.naturallyfunny.dev/chronica"
    "go.naturallyfunny.dev/chronica/inmemory"
)

c := chronica.NewChronicarius(inmemory.NewStore())
```

To use a real backend (e.g. Postgres, MongoDB), implement the four-method `chronica.Store` interface and pass it to `chronica.NewChronicarius`. Your backend can be verified against the conformance suite using `storetest.Run`.

### 1. Recording an Action (RecordActum)

`RecordActum` records an action in a chronicum. If the `ChronicumID` does not exist, the orchestrator auto-creates it and binds it to `ownerID`. On success it returns the fully-populated `Actum` with server-assigned `ID` and `At`.

Supply an `IdempotencyKey` to make retries safe: a repeated call with the same key returns the previously stored `Actum` without writing a duplicate.

```go
// Example: An AI agent replying to a user
actum := chronica.Actum{
    ChronicumID:    "session-123",
    Kind:           chronica.ActumMessage,
    ActorKind:      chronica.ActorAgent,
    Actor:          "agent-abc-456", // Use a stable ID; display names belong in a higher layer
    Content:        "Hello, how can I help you today?",
    IdempotencyKey: "req-uuid-xyz",  // optional; makes the call retry-safe
}

stored, err := c.RecordActum(ctx, "user-999", actum)
```

### 2. Retrieving Context for AI (GetActa)

Retrieve the history for an LLM prompt, scoped to an owner. Use functional options to filter specific actum kinds, actor kinds, and limit the context window.

```go
// Fetch the last 20 messages (excluding thoughts and system events), scoped to user-999
acta, err := c.GetActa(ctx, "user-999", "session-123",
    chronica.WithLastN(20),
    chronica.WithActumKinds(chronica.ActumMessage, chronica.ActumToolResponse),
    chronica.WithActorKinds(chronica.ActorHuman, chronica.ActorAgent),
)
// retrieved acta are guaranteed to be in chronological order (Old → New)
```

### 3. Fetching Session Metadata (GetChronicum)

Retrieve metadata for a single session while verifying that it belongs to the caller.

```go
// Get session metadata and verify ownership
session, err := c.GetChronicum(ctx, "user-999", "session-123")
```

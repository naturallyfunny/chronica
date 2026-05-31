```
______________  _____________________   _______________________ 
__  ____/__  / / /__  __ \_  __ \__  | / /___  _/_  ____/__    |
_  /    __  /_/ /__  /_/ /  / / /_   |/ / __  / _  /    __  /| |
/ /___  _  __  / _  _, _// /_/ /_  /|  / __/ /  / /___  _  ___ |
\____/  /_/ /_/  /_/ |_| \____/ /_/ |_/  /___/  \____/  /_/  |_|
```

A lightweight, 1 Human to N Agent Session System SDK designed specifically for Multi-Agent Agentic AI ecosystems.

Chronica treats AI conversations not just as text messages, but as a stream of **Actions (Actum)** within a **Session (Chronicum)**. It is built with pure Go idioms, functional options, and strict chronological contracts. 

## Key Features

- **Agent-First Design:** Natively supports various AI actions (`Message`, `Thought`, `ToolRequest`, `ToolResponse`) beyond just standard text.
- **Identity Flexibility:** Agnostic to your identity provider. It uses raw string identifiers (`OwnerID` and `Actor`) to map sessions and actors.
- **Strict Chronological Contracts:** - `GetActa` always returns messages in chronological order (Old $\rightarrow$ New) for LLM context windows.
  - `ListChronicum` always returns sessions in anti-chronological order (New $\rightarrow$ Old) for UI listing.
- **Sliding Window Context:** Built-in `WithLastN` option handles AI context windows cleanly while maintaining chronological order.

## Core Concepts

* **`Chronicum`**: A 1-Human-to-N-Agents session tied to an owner.
* **`Actum`**: A single recorded action (e.g., message, thought) within a session.
* **`Chronicarius`**: The core interface acting as the recorder of history.

> **Note on Naming:** `Chronica` and `Acta` are simply the Latin plural forms of `Chronicum` and `Actum`. You will see these used throughout the SDK's package name and slice returns (e.g., `[]Actum` is referred to as Acta).

## Installation

```bash
go get [go.naturallyfunny.dev/chronica](https://go.naturallyfunny.dev/chronica)
```

## Quick Start

### 1. Recording an Action (RecordActum)

`RecordActum` acts as an upsert. If the `ChronicumID` does not exist, the implementation will automatically create it and bind it to the `OwnerID`.

```go
import "go.naturallyfunny.dev/chronica"

// Example: An AI agent replying to a user
actum := chronica.Actum{
    ChronicumID: "session-123",
    Kind:        chronica.ActumMessage,
    ActorKind:   chronica.ActorAgent,
    Actor:       "Claude", // Can be a Name or an ID
    Content:     "Hello, how can I help you today?",
}

err := chronicarius.RecordActum(ctx, "user-999", actum)
```

### 2. Retrieving Context for AI (GetActa)

Retrieve the history for an LLM prompt. Use functional options to filter specific actum kinds and limit the context window (`lastN`).

```go
// Fetch the last 20 messages, excluding internal agent thoughts
acta, err := chronicarius.GetActa(ctx, "session-123",
    chronica.WithLastN(20),
    chronica.WithActumKinds(chronica.ActumMessage, chronica.ActumToolResponse),
)
// retreived acta is guaranteed to be in chronological order (Old to New)
```

### 3. Listing Sessions for UI (ListChronicum)

Retrieve a paginated list of sessions belonging to a specific user.

```go
// Fetch the 10 most recently active sessions
sessions, err := chronicarius.ListChronicum(ctx, "user-999",
    chronica.ListWithLimit(10),
)
// listed sessions are guaranteed to be in anti-chronological order (New to Old)
```

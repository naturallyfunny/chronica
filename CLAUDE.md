# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

`chronica` (module `go.naturallyfunny.dev/chronica`, Go 1.25) is a small dependency-free SDK for storing AI agent session history. A **Chronicum** is a session owned by a single owner; an **Actum** is one recorded action within it (message, thought, tool request/response). `Chronica`/`Acta` are just the Latin plurals — `[]Actum` is called "acta" throughout.

## Scope: this is an agent-layer SDK, not CRUD/UI

Chronica targets the **AI agent layer only**. Design decisions follow from that and should be preserved:

- **No `Delete`** anywhere in the API. Session history is append-only; the agent layer has no case that erases a recorded action. Don't add deletion to satisfy a CRUD/UI urge — that belongs in a layer above Chronica.
- **No pagination cursors.** Cursors serve UI lists; agents build context windows. The windowing primitive is `WithLastN` + kind/actor filters, not page/cursor navigation.
- **No editor/UI tooling** (e.g. Cursor/Copilot rule files) — this is a library for the agent layer, not a UI app.

When a request smells like CRUD or UI (delete, pagination, edit-in-place), the answer is usually "that's a higher layer's job," not "add it to chronica."

## Commands

```bash
go test ./...                              # run all tests
go test -race ./...                        # run with the race detector (CI-equivalent; concurrency is core here)
go test -run TestConformance ./inmemory    # run a single test
go test -run 'TestConformance/Idempotency' ./inmemory   # run one conformance sub-case
go vet ./...
go build ./...
```

There is no separate lint config; `go vet` is the linter. The `.github/workflows/claude.yml` workflow only handles `@claude` mentions — it does not run tests.

## Architecture

Three layers, with a deliberate split of responsibilities captured by the README slogan **"Smart Core, Dumb Edges"**:

- **`chronica` (root package)** — the public API. `Chronicarius` (in `chronicarius.go`) is the policy orchestrator: it owns input validation, server-side ID assignment, owner-scoping, and find-or-create orchestration. `store.go` defines the domain types (`Chronicum`, `Actum`, enums), sentinel errors, `Actum.Validate`, and the `Store` interface.
- **`Store` implementations** — `inmemory/` is the reference backend (also used in examples/tests). Real backends (Postgres, Mongo, etc.) implement the same four-method interface. Stores are intentionally "dumb": they do persistence and querying only.
- **`storeconformance/`** — a conformance suite. Any `Store` implementation gets verified by calling `storeconformance.RunTest(t, newStore)` from its own `_test.go` (see `inmemory/store_test.go`). When adding/changing `Store` behavior, update `storeconformance/tester.go` so every backend inherits the check.

### Smart Chronicarius, dumb Store (contract principle)

The `Store` is meant to be a **primitive CRUD repository**, and the contract between it and `Chronicarius` should be **pure: method parameters and return values, that's it.** The guiding rule when editing the interface or its docs:

- The rules `Chronicarius` enforces are: ownership/tenant isolation, validation, ID assignment, and find-or-create orchestration. Do not push these down into "implementations MUST …" prose; a naive-but-correct CRUD `Store` should automatically be a correct backend.
- **Ordering** is an observable contract of `Store.Acta` itself (insertion order), not something Chronicarius enforces.
- **Idempotency** is an optional `Store` capability (`IdempotentStore`). Chronicarius orchestrates it (routing + fail-loud) but does not implement deduplication.
- Prefer making the store primitive enough that the behavior is structural, not a request.

The "Known divergence" that existed in older versions of this codebase (idempotency in base `Store`, mandatory serialization prose) has been resolved. The base `Store` is now a pure append repository; idempotency is an optional capability via the `IdempotentStore` extension interface (see below). There is no remaining known divergence.

### The ownership boundary (most important invariant)

`Chronicarius` is the authorization boundary; the `Store` is **not**. The three `Store` read/write methods (`Record`, `Acta`, `Get`) take **no `ownerID`** and perform **no ownership check** — see the "OWNERSHIP BOUNDARY" comments in `store.go`. Tenant isolation is enforced exclusively by `Chronicarius.ownerGuard`, which fetches the chronicum via `Store.Get` and rejects it with `ErrChronicumNotFound` if `OwnerID` doesn't match. Calling a `Store` method directly bypasses this and can read/write across tenants.

Consequences to preserve when editing:
- Every public `Chronicarius` method that touches a chronicum must go through `ownerGuard` first.
- "Not found" and "owned by someone else" must remain **indistinguishable** — both return `ErrChronicumNotFound` — to avoid leaking the existence of other owners' sessions. Do not add error paths that let a caller tell them apart.
- `RecordActum` auto-creates a chronicum when it doesn't exist and binds it to `ownerID`. It handles the concurrent-create race by catching `ErrChronicumExists` and re-running `ownerGuard` (a different owner winning the race correctly collapses to `ErrChronicumNotFound`).

### Ordering, idempotency, filtering contracts

These are the behaviors the conformance suite enforces:

- **Insertion order:** `Acta` returns acta oldest-first in a stable total order. `Actum.At` is advisory wall-clock time set by the Store at insert time and MUST NOT be used for ordering.
- **Filter-then-limit:** `WithActumKinds` / `WithActorKinds` filter first, then `WithLastN(n)` keeps the most recent `n` of what survived filtering. `n <= 0` means no limit.
- **Idempotency (optional capability):** Idempotency is not a base `Store` requirement. It is an optional extension interface: `chronica.IdempotentStore`. A store that implements it exposes `RecordIdempotent(ctx, actum, key)`, which deduplicates on key within a chronicum — stored actum wins, new payload is discarded. `Chronicarius` detects the capability via type assertion; if `WithIdempotencyKey` is passed to a store that does not implement `IdempotentStore`, `RecordActum` returns `ErrIdempotencyUnsupported`. The `inmemory` backend implements both `Store` and `IdempotentStore`. The conformance suite has two entry points: `storeconformance.RunTest` for the base suite and `storeconformance.RunIdempotentTest` for the idempotency suite.
- **Atomicity (within IdempotentStore):** `RecordIdempotent` implementations must serialize concurrent calls on the same chronicum so the deduplication lookup and append are atomic. The in-memory store does this with a per-session mutex.

### ID assignment

`Actum.ID` is always overwritten server-side by `Chronicarius` before `Record`; any caller-supplied value is discarded. The generator is pluggable via `WithIDGen` (default: random 128-bit hex from `crypto/rand`); inject a deterministic one for tests. The generator MUST return globally unique values — collisions are not detected.

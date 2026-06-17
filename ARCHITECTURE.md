# `grr` – Go Registry Resolver

> Version: 0.2-dev | Status: implemented
>
> This file is the design document and the running log of decisions made
> while building `grr` — the "why is it the way it is" reference. Future
> work is driven by [plan.md](plan.md) (the Phase 2 roadmap); this is the
> historical reference.

---

## Core concept

A name-keyed factory/value store. A general-purpose primitive — not
HTTP-specific, not a DI framework. Usable on its own; [gold](https://github.com/arpaad/gold)
builds a typed layer on top of it.

---

## Repo structure

```
github.com/arpaad/grr       ← core + net/http middleware
github.com/arpaad/grr-gin   ← Gin middleware, separate repo, dep: grr
github.com/arpaad/gold      ← Logic layer, separate repo, dep: grr
```

Every repo has an independent release cycle. If Echo, Fiber, or anything
else comes later — new repo, no impact on the rest.

---

## File layout

```
grr/
├── registry.go     # Registry struct, Default, New, NewFrom, Hooks/Options
├── register.go     # Set, Register, RegisterFunc, RegisterScoped
├── resolve.go      # Resolve, ResolveOK, Get, findEntry
├── scope.go        # scopeStore, BeginScope, scope plumbing
├── buildchain.go   # cycle-detection chain carried through ctx
├── context.go      # WithRegistry, RegistryFromCtx
├── introspect.go   # IsRegistered, Keys, Clear
├── middleware/
│   └── http.go     # net/http middleware — stdlib, Chi, Echo
└── *_test.go
```

---

## Public API

### Creation

```go
grr.Default               // global singleton — sugar, entry point
r := grr.New()            // isolated registry, no parent, own scope store
r := grr.NewFrom(parent)  // parent chain — fallback lookup, shared scope store
r := grr.New(grr.WithHooks(grr.Hooks{...})) // with observability hooks
```

### Registration

```go
// Fixed value — the factory always returns the same value (singleton sugar).
r.Set("key", value)

// Transient or singleton — the factory's own logic decides.
r.Register("key", func(ctx context.Context) any { return newSomething(ctx) })

// Sugar — context-less factory.
r.RegisterFunc("key", func() any { return newSomething() })

// Scoped — one instance per scope, keyed by scope ID.
r.RegisterScoped("key", func(ctx context.Context) any { return newSomething(ctx) })
```

> **No return value** — a duplicate registration panics, it doesn't return an error.

### Lookup

```go
r.Resolve(ctx, "key")        // universal — ctx always present
r.Get("key")                 // sugar = Resolve(context.Background(), "key")
r.ResolveOK(ctx, "key")      // (any, bool) — false only if the key isn't registered
```

`ResolveOK` softens **only** the "not registered" case. Resolving a scoped
key with no active scope, a circular dependency, or resolving after the
scope ended still panic — those are bugs, not conditions to branch on.

### Scope lifecycle

```go
ctx, endScope := r.BeginScope(ctx)
defer endScope()
// endScope: idempotent (sync.Once) — calling it twice is safe
// on ctx.Done(): a goroutine cleans up automatically
```

### Introspection

```go
r.IsRegistered("key")  // bool — walks the parent chain
r.Keys()               // []string — keys in THIS registry only, not parents
r.Clear()              // full reset — mostly for test teardown
```

### Context helpers

```go
grr.WithRegistry(ctx, r)   // attach registry to ctx
grr.RegistryFromCtx(ctx)   // read registry from ctx, fallback: grr.Default
```

### Observability hooks

```go
r := grr.New(grr.WithHooks(grr.Hooks{
    OnResolve:    func(key string, dur time.Duration) { ... },
    OnScopeBegin: func() { ... },
    OnScopeEnd:   func() { ... },
}))
```

A nil hook is never called — the no-hook path costs one nil check. Hooks
belong to the registry they're set on, regardless of which registry in a
chain owns the resolved entry.

---

## Lifetime semantics

| Registration | Lifetime | Decided by |
|---|---|---|
| `Set` | singleton | sugar — the factory always returns the same value |
| `Register` | transient or singleton | the factory's logic |
| `RegisterFunc` | transient or singleton | the factory's logic |
| `RegisterScoped` | scoped | the registry — keyed by scope ID |

**Scoped without a scope = panic.** There is no scope ID to key on, so this
is treated as a programmer error (see the behavioral guarantees below).

---

## Internal structure

```go
type entry struct {
    factory func(ctx context.Context) any
    scoped  bool
}

type Registry struct {
    mu      sync.RWMutex
    entries map[string]*entry
    parent  *Registry

    scopes *scopeStore // per-chain scope cache (see below)
    hooks  Hooks
}

// One per registry chain. New() allocates a fresh one; NewFrom shares the
// parent's. Holds the live scopes and the mutex guarding them.
type scopeStore struct {
    mu     sync.RWMutex
    scopes map[uint64]*scopeData
}

// Process-global, monotonic, lock-free — see "Scope storage" below for why
// the IDs are global even though the maps aren't.
var scopeCounter uint64
```

**RWMutex strategy:**
- `Resolve` / `Get` / `findEntry` → `RLock` on `r.mu` — concurrent readers don't block each other.
- `Register` / `Set` / `Clear` → `Lock` on `r.mu` — exclusive writes.
- The scope cache is guarded by `scopeStore.mu`, separate from `r.mu`.

---

## Scope storage

Scope state lives in a `scopeStore` owned by the registry, **not** in package
globals. The store is shared across a parent/child chain (`NewFrom` copies
the parent's pointer) but a standalone `New()` gets its own. Consequences:

- `New()` registries are genuinely isolated, scope state included.
- The cache mutex is per-chain, so independent registries don't serialize
  on one process-wide lock.

The **scope ID counter, however, is a single process-global atomic**
(`scopeCounter`). That's deliberate: a global monotonic counter is a
lock-free atomic add with negligible contention, and keeping IDs unique
across *all* stores means a context carrying a scope ID from one chain,
used against an unrelated chain, correctly misses (and panics) instead of
silently colliding with that chain's same-numbered scope. Splitting the
counter per-store reintroduced exactly that collision — caught by
`TestIsolatedRegistriesHaveSeparateScopeState` (see the decisions log).

---

## Behavioral guarantees

| Case | Behavior |
|---|---|
| Duplicate registration in the same registry | **panic** |
| Scoped resolve without a scope ID | **panic** |
| `Get` on a scoped entry | **panic** |
| Key not found, no parent | **panic** |
| Key not found via `ResolveOK` | `(nil, false)` — no panic |
| Key not found, has parent | parent chain lookup |
| Concurrent resolve | RWMutex — safe |
| `endScope` called twice | sync.Once — no panic |
| `ctx.Done()` before `endScope` | goroutine cleanup — no leak |
| Circular dependency | **panic** with the full chain (`a -> b -> a`) |

---

## Scope internals

```go
func (r *Registry) BeginScope(parent context.Context) (context.Context, func()) {
    store := r.scopes
    scopeID := store.begin() // global atomic ID + insert into this store
    ctx := context.WithValue(parent, scopeKey{}, scopeID)

    if r.hooks.OnScopeBegin != nil {
        r.hooks.OnScopeBegin()
    }

    // Teardown runs exactly once, whichever path reaches it first.
    var releaseOnce sync.Once
    release := func() {
        releaseOnce.Do(func() {
            store.cleanup(scopeID)
            if r.hooks.OnScopeEnd != nil {
                r.hooks.OnScopeEnd()
            }
        })
    }

    stop := make(chan struct{})
    go func() {
        select {
        case <-ctx.Done():
            release()
        case <-stop:
        }
    }()

    var endOnce sync.Once
    endScope := func() {
        endOnce.Do(func() {
            release()   // synchronous — callers expect teardown done on return
            close(stop) // let the watcher goroutine exit
        })
    }
    return ctx, endScope
}
```

`release` (cleanup + `OnScopeEnd`) is guarded by its own `sync.Once` shared
between the explicit `endScope` and the ctx-cancellation path, so the scope
is torn down — and `OnScopeEnd` fired — exactly once no matter which fires
first.

---

## HTTP middleware

### net/http (stdlib, Chi, Echo) — in core

```go
func Middleware(r *Registry) func(http.Handler) http.Handler {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
            ctx := WithRegistry(req.Context(), r)
            ctx, endScope := r.BeginScope(ctx)
            defer endScope()
            next.ServeHTTP(w, req.WithContext(ctx))
        })
    }
}
```

### Gin — separate repo (`github.com/arpaad/grr-gin`)

Kept out of core so `grr` never needs a Gin dependency. Same pattern:
attach the registry, begin a scope, defer `endScope`, call the next handler.

---

## Parent chain — fallback lookup

```
grr.Default          ← root, always present
    └── appRegistry  ← NewFrom(Default), production override
            └── r    ← NewFrom(appRegistry), test override
```

A child shares its parent's scope store, so a scoped entry registered on a
parent resolves correctly through a scope begun on a child.

---

## Testing pattern

```go
// Override one key, fall back to Default for the rest.
func TestSomething(t *testing.T) {
    r := grr.NewFrom(grr.Default)
    r.Register("db", func(ctx context.Context) any { return &mockDB{} })
}

// Fully isolated — no fallback.
func TestIsolated(t *testing.T) {
    r := grr.New()
    r.Set("config", testConfig)
}
```

---

## What does NOT belong in `grr`

| Concern | Where it lives |
|---|---|
| Type safety — casting is the caller's job, `any`-based storage | the `gold` layer |
| Domain concepts — Logic, port/adapter separation | `gold` |
| Compile-time enforcement of scoped usage | `gold` |
| Pooled lifetime | dropped — see [plan.md](plan.md) |

---

## Open questions

- [x] Module path: `github.com/arpaad/grr`
- [x] The `grr` acronym: **Go Registry Resolver** — the two core operations (Register, Resolve)
- [x] Scope storage: package globals → per-chain `*scopeStore` with a global ID counter (done in 0.2-dev)
- [ ] Whether to keep the per-scope cleanup goroutine, or make it opt-in — decide with benchmark data (**Phase 2**, see plan.md)

---

## Decisions and bugs found during implementation

> v0.1 (2026-06-16) — core + middleware + tests written, all green.

- **Contradiction resolved:** the original "lifetime semantics" section said
  *"scoped without a context = transient"*, while the guarantees table said
  *"scoped resolve without a scope ID → panic"*. The two conflicted. Per the
  `gold` error philosophy ("Do — scoped, no BeginScope → panic"), the
  **panic** version was implemented — consistent across both repos.
- **Deadlock found and fixed:** the first implementation guarded the whole
  scope cache with one mutex held *during* the factory call. If a scoped
  factory itself resolved another scoped key in the same scope (a repo model
  depending on a scoped transaction, say), it **deadlocked** — the mutex
  isn't reentrant. Fix: a per-(scope, key) `sync.Once`, so calling a factory
  holds no shared lock. Covered by `TestScopedFactoryCanResolveAnotherScopedKey`
  and `TestScopedConcurrentResolveBuildsOnce`.
- **Circular dependency — resolved, panics:** `Resolve` threads the chain of
  keys currently being built through `ctx` ([buildchain.go](buildchain.go)).
  A factory that directly or transitively resolves itself **panics** with the
  full chain (`grr: circular dependency: a -> b -> a`) *before* it can
  deadlock (scoped, via the non-reentrant `sync.Once`) or stack-overflow
  (non-scoped). Legitimate nested resolves still work — a separate chain
  entry distinguishes them from a cycle. See `TestCircularScopedDependencyPanics`,
  `TestCircularDependencyThreeKeysPanics`, `TestCircularNonScopedDependencyPanics`.
- **`endScope` cleans up synchronously:** the planned `close(stop)` pattern
  was extended so `endScope` deletes from the scope cache *immediately and
  synchronously*, not only via the background goroutine. Callers `defer
  endScope()` and deterministically expect the scope to be over on return;
  async-only cleanup was a race (a test caught it).

> v0.2-dev (2026-06-17) — scope storage refactor, hooks, Keys/ResolveOK.

- **Scope storage: package globals → per-chain `*scopeStore`.** The global
  `scopes` map + mutex meant `New()` registries weren't truly isolated and
  every `BeginScope`/cleanup contended on one process-wide lock. Moved the
  map+mutex onto a `scopeStore` value owned by the registry; `NewFrom` shares
  the parent's pointer (preserving cross-chain resolution), `New()` gets a
  fresh one.
- **The ID counter stayed global — found by a failing test.** The first cut
  of the refactor also moved the scope ID counter onto the store. That made
  IDs collide across stores (each starts at 1), so a context from registry A
  used against registry B silently hit B's scope #1 instead of panicking.
  `TestIsolatedRegistriesHaveSeparateScopeState` failed and exposed it. Fix:
  keep a single process-global atomic counter for IDs (lock-free, negligible
  contention) while the maps stay per-store — uniqueness across stores
  restores the correct "foreign context misses and panics" behavior.
- **`OnScopeEnd` fires exactly once across both teardown paths.** Sharing one
  `releaseOnce` between `endScope` and the ctx-cancellation goroutine
  guarantees the hook (and the cache cleanup) run once, never twice on a
  cancel-then-endScope sequence. Covered by `TestScopeHooksFireOnEndScope`
  and `TestScopeEndHookFiresOnCtxCancel`.
- **`ResolveOK`/`Keys` are deliberately narrow.** `ResolveOK` only converts
  the "not registered" panic into `(nil, false)`; misuse panics stay panics.
  `Keys` lists the registry's own entries, not the parent chain, so callers
  can reason about what a single registry declares (this is what `gold.Validate`
  builds on).

# grr — Go Registry Resolver

[![CI](https://github.com/arpaad/grr/actions/workflows/ci.yml/badge.svg)](https://github.com/arpaad/grr/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/arpaad/grr.svg)](https://pkg.go.dev/github.com/arpaad/grr)
[![Go Report Card](https://goreportcard.com/badge/github.com/arpaad/grr)](https://goreportcard.com/report/github.com/arpaad/grr)

A name-keyed factory/value store with request-scoped lifetime support. Not a DI framework, not HTTP-specific — a small, general-purpose primitive that other layers (like [gold](https://github.com/arpaad/gold)) build type safety on top of.

```go
import "github.com/arpaad/grr"

grr.Default.Set("config", cfg)

grr.Default.RegisterScoped("db", func(ctx context.Context) any {
    return openConn(ctx)
})

ctx, end := grr.Default.BeginScope(ctx)
defer end()

conn := grr.Default.Resolve(ctx, "db") // same instance for the rest of this scope
```

## Why this exists

Go already has mature DI tools — [wire](https://github.com/google/wire) (compile-time codegen) and [dig](https://github.com/uber-go/dig)/[fx](https://github.com/uber-go/fx) (reflection-based runtime containers). `grr` is not trying to replace them or compete on features. It exists because none of them give you a *minimal*, reflection-free, runtime-resolvable registry with first-class **scope lifecycle** that's small enough to vendor mentally in five minutes and build a typed layer on top of (see [gold](https://github.com/arpaad/gold)).

If you need a full-featured IoC container with struct-tag injection and dependency graphs resolved automatically, `dig`/`fx` are the right tool — use those instead. `grr` deliberately does none of that.

A few choices that look unusual and are worth explaining up front, so reviewers don't mistake them for oversights:

- **`any`-typed storage, on purpose.** `grr` is the untyped storage primitive on purpose — it does not use the `reflect` package anywhere. Type safety is a layer concern, not a storage concern; see [gold](https://github.com/arpaad/gold) for the generics-first layer built on top.
- **Panics instead of error returns for misuse.** A duplicate registration, an unregistered key, or resolving a scoped key with no active scope are all *developer errors* — bugs to fix, not runtime conditions calling code should branch on. This follows the same philosophy as `regexp.MustCompile` or `template.Must`: fail loud and immediately, don't make every caller check an error that should never happen in correct code. Business-logic errors (the actual return value of your factory/handler) are unaffected — they flow through `error` as normal.
- **A global `Default` registry.** It's sugar for the common case (no isolated registries needed), not a requirement — `grr.New()` gives you a fully isolated registry, and `grr.NewFrom(parent)` gives you a parent-chain override (handy for tests: override one key, fall back to `Default`/`appRegistry` for the rest).
- **Scope travels through `context.Context`.** Scopes need to survive across middleware/handler boundaries in *any* HTTP framework, background jobs, and goroutines — `context.Context` is the only thing all of those already share.

## Install

```sh
go get github.com/arpaad/grr
```

## API at a glance

| Operation | Call |
|---|---|
| Fixed value | `r.Set(key, value)` |
| Factory (lifetime is the factory's own logic) | `r.Register(key, func(ctx) any)` |
| Factory, no ctx needed | `r.RegisterFunc(key, func() any)` |
| One instance per scope | `r.RegisterScoped(key, func(ctx) any)` |
| Lookup | `r.Resolve(ctx, key)` / `r.Get(key)` (sugar for `Resolve(context.Background(), key)`) |
| Lookup, non-panicking | `r.ResolveOK(ctx, key)` → `(any, bool)` — `false` only when the key isn't registered |
| Scope lifecycle | `ctx, end := r.BeginScope(ctx); defer end()` |
| Conditional registration check | `r.IsRegistered(key)` |
| List own keys (not parents) | `r.Keys()` |
| Test teardown | `r.Clear()` |
| Isolated registry | `grr.New()` |
| Parent-chain registry | `grr.NewFrom(parent)` |
| Registry with observability hooks | `grr.New(grr.WithHooks(grr.Hooks{...}))` |
| Attach/read registry via ctx | `grr.WithRegistry(ctx, r)` / `grr.RegistryFromCtx(ctx)` |

`ResolveOK` softens **only** the "key not registered" case — resolving a
scoped key with no active scope, a circular dependency, or resolving after
a scope ended all still panic, because those are bugs, not conditions to
branch on.

HTTP integration: `grr/middleware` for `net/http` (and anything built on it, e.g. Chi). For Gin, see the separate [grr-gin](https://github.com/arpaad/grr-gin) module — kept out of core so `grr` itself never needs a Gin dependency.

```go
import grrmw "github.com/arpaad/grr/middleware"

http.Handle("/", grrmw.Middleware(grr.Default)(myHandler))
```

## Testing with grr

```go
func TestSomething(t *testing.T) {
    r := grr.NewFrom(grr.Default) // override one key, fall back to Default for the rest
    r.Register("db", func(ctx context.Context) any { return &mockDB{} })
    // ...
}

func TestIsolated(t *testing.T) {
    r := grr.New() // no fallback at all
    r.Set("config", testConfig)
}
```

Runnable examples live in [example_test.go](example_test.go) and on [pkg.go.dev](https://pkg.go.dev/github.com/arpaad/grr).

## Observability

Attach optional hooks at construction. A `nil` hook is never called, so the
default (no hooks) costs a single nil check per event and nothing more:

```go
r := grr.New(grr.WithHooks(grr.Hooks{
    OnResolve:    func(key string, dur time.Duration) { metrics.Observe(key, dur) },
    OnScopeBegin: func() { ... },
    OnScopeEnd:   func() { ... },
}))
```

Hooks belong to the registry they're set on: `Resolve`/`BeginScope` called
on this registry fire its hooks, regardless of which registry in a parent
chain owns the entry. `OnScopeEnd` fires exactly once per scope, whether it
ends via `endScope` or ctx cancellation.

## Benchmarks

`go test -bench=. -benchmem`. Numbers from an AMD Ryzen AI 9 HX 370, Go
1.25 — reproduce them yourself rather than trusting the absolute values;
what matters is the ratio between the cases.

| Benchmark | ns/op | allocs/op |
|---|---:|---:|
| `ResolveNonScoped` | ~160 | 3 |
| `ResolveScopedCached` (the dominant case) | ~110 | 0 |
| `ParentChainLookup` (8 levels deep) | ~340 | 3 |
| `BeginEndScope` (open + close) | ~2000 | 10 |

The scoped cache-hit path — every resolve after the first within a scope —
is allocation-free. The per-resolve allocations on the other paths come
from threading the build-chain (cycle detection) through `context`; trimming
that is a tracked optimization (see [plan.md](plan.md)).

## Status

v0.2-dev — core registry, scope lifecycle (with cycle detection),
`net/http` middleware, observability hooks, `ResolveOK`/`Keys`, and a
per-chain scope store (isolated `New()` registries no longer share a global
scope table). See [plan.md](plan.md) for what's next and [ARCHITECTURE.md](ARCHITECTURE.md)
for the full design history and the reasoning behind every non-obvious
decision.

## License

MIT — see [LICENSE](LICENSE).

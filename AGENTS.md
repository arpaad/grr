# AGENTS.md

Context for AI agents (and future-you) picking up this repo cold.

## What this repo is

`grr` (Go Registry Resolver) is one of three related, independently-versioned repos:

- **`grr`** (this repo) — the core: a name-keyed, `any`-typed factory/value registry with scope lifecycle. No DI framework ambitions, no HTTP framework dependency. Lives at `github.com/arpaad/grr`.
- **[`gold`](https://github.com/arpaad/gold)** — Go Logic Dependency. A generics-first domain/logic layer built *on top of* `grr`. This is where type safety lives; `grr` stays untyped on purpose.
- **[`grr-gin`](https://github.com/arpaad/grr-gin)** — Gin middleware for `grr`, kept in its own repo so neither `grr` nor `gold` need a Gin dependency.

If you're asked to change something here, check whether `gold` or `grr-gin` also need a matching change (e.g. a new `Registry` method that `gold`'s `RegisterIn`/`RegisterScopedIn` would want to call).

## Where the reasoning lives

- **[ARCHITECTURE.md](ARCHITECTURE.md)** — the original design doc plus a log of every decision made (and bug found) during implementation. Read this before changing core semantics — several things that look like they could be "simplified" (global scope storage instead of per-registry, per-key `sync.Once` instead of one big lock, the build-chain cycle check) were deliberate fixes for real bugs (deadlocks), not arbitrary choices.
- **[plan.md](plan.md)** — phase-2 roadmap (pooled lifetime, observability hooks, more examples, benchmarks). Nothing in here is implemented yet; check before assuming a feature exists.
- **[README.md](README.md)** — the public-facing pitch and rationale, written for Go developers evaluating whether to depend on this. Keep it in sync if you change public API or behavior.

## Hard constraints — don't violate these without a conversation first

- **No `reflect` package, anywhere.** This is an explicit project-wide rule (carried over into `gold` too). Plain type assertions (`x.(T)`) are fine and expected at the `any` boundary — that's not `reflect`.
- **Misuse panics, business errors return `error`.** Duplicate registration, missing key, scoped-resolve-without-scope, circular dependency — all panic, deliberately, because they're developer errors. Don't "fix" this into an `(T, error)` return without reading the rationale in README.md first.
- **Scope storage is global, not per-`Registry`.** See `scope.go`/`resolve.go`. Scope IDs are globally unique (atomic counter), and a scoped entry registered on a *parent* registry must resolve correctly when `BeginScope` was called on a *child*. Don't move this back to a per-`Registry` field — it was tried and is wrong (see ARCHITECTURE.md).
- **Per-key `sync.Once` inside a scope, never one shared lock held during a factory call.** A factory is allowed to call `Resolve` for another key in the same scope (legitimate nested dependency). Holding a single mutex across the factory call deadlocks on that pattern. The build-chain check in `buildchain.go` + per-key `onceValue` in `scope.go` is what makes this safe — keep both in sync if you touch either.

## Running things

```sh
make test     # go test ./... -race
make lint     # golangci-lint run
make vet      # go vet ./...
make tidy     # go mod tidy, fails CI if it would change go.mod/go.sum
make ci       # everything CI runs, locally
```

## Conventions

- Every public behavior change needs a matching test in `registry_test.go` (or the relevant `*_test.go`) — this codebase is small enough that "obviously correct" isn't a justification for skipping a test; the deadlock and the parent/child scope bug were both "obviously correct" until a test caught them.
- Runnable examples (`Example*` functions with `// Output:` comments) belong in `example_test.go` — they double as documentation on pkg.go.dev and as regression tests.
- New `Registry` methods should be considered from `gold`'s perspective too: would `gold` need a typed wrapper around this? If so, mention it so the companion change doesn't get forgotten.

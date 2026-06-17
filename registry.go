package grr

import (
	"context"
	"sync"
	"time"
)

type entry struct {
	factory func(ctx context.Context) any
	scoped  bool
}

// Hooks are optional observability callbacks. A nil field is never called,
// so an empty Hooks value (the default) costs a single nil check per event
// and nothing else.
type Hooks struct {
	// OnResolve fires once per Resolve call with the key and the time the
	// whole resolution took (chain walk + factory or cache lookup).
	OnResolve func(key string, dur time.Duration)
	// OnScopeBegin fires when BeginScope opens a scope.
	OnScopeBegin func()
	// OnScopeEnd fires when a scope is released (via endScope or ctx
	// cancellation).
	OnScopeEnd func()
}

// Option configures a Registry at construction time.
type Option func(*Registry)

// WithHooks attaches observability hooks to a registry. The hooks belong to
// the registry they're set on — Resolve/BeginScope called on this registry
// fire these hooks, regardless of which registry in a parent chain actually
// owns the entry.
func WithHooks(h Hooks) Option {
	return func(r *Registry) { r.hooks = h }
}

// Registry is a name-keyed factory/value store. It is a general-purpose
// primitive: not HTTP-specific, not a DI framework on its own.
type Registry struct {
	mu      sync.RWMutex
	entries map[string]*entry
	parent  *Registry

	// scopes holds per-scope cached instances. New() allocates a fresh
	// store (isolated scope state); NewFrom shares the parent's store so a
	// scoped entry registered on a parent still resolves through a scope
	// begun on a child.
	scopes *scopeStore

	hooks Hooks
}

// Default is the global registry — the entry point for apps that don't
// need isolated registries (e.g. tests, multi-tenant setups).
var Default = New()

// New creates an isolated registry with no parent and its own scope store.
func New(opts ...Option) *Registry {
	r := &Registry{
		entries: make(map[string]*entry),
		scopes:  newScopeStore(),
	}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

// NewFrom creates a registry that falls back to parent for keys it doesn't
// have registered itself. It shares parent's scope store, so scopes are
// consistent across the whole chain.
func NewFrom(parent *Registry, opts ...Option) *Registry {
	r := New(opts...)
	r.parent = parent
	r.scopes = parent.scopes
	return r
}

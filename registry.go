package grr

import (
	"context"
	"sync"
)

type entry struct {
	factory func(ctx context.Context) any
	scoped  bool
}

// Registry is a name-keyed factory/value store. It is a general-purpose
// primitive: not HTTP-specific, not a DI framework on its own.
type Registry struct {
	mu      sync.RWMutex
	entries map[string]*entry
	parent  *Registry
}

// Default is the global registry — the entry point for apps that don't
// need isolated registries (e.g. tests, multi-tenant setups).
var Default = New()

// New creates an isolated registry with no parent.
func New() *Registry {
	return &Registry{
		entries: make(map[string]*entry),
	}
}

// NewFrom creates a registry that falls back to parent for keys it
// doesn't have registered itself.
func NewFrom(parent *Registry) *Registry {
	r := New()
	r.parent = parent
	return r
}

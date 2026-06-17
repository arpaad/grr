package grr

import (
	"context"
	"fmt"
)

// Set registers a fixed value under key — always returns the same value.
// Sugar for a singleton factory.
func (r *Registry) Set(key string, value any) {
	r.register(key, &entry{
		factory: func(ctx context.Context) any { return value },
		scoped:  false,
	})
}

// Register registers a factory under key. Whether it behaves as transient
// or singleton is entirely up to the factory's own logic.
func (r *Registry) Register(key string, factory func(ctx context.Context) any) {
	r.register(key, &entry{factory: factory, scoped: false})
}

// RegisterFunc is sugar for Register with a context-less factory.
func (r *Registry) RegisterFunc(key string, factory func() any) {
	r.Register(key, func(ctx context.Context) any { return factory() })
}

// RegisterScoped registers a factory that produces one instance per active
// scope (see BeginScope). Resolving without an active scope panics.
func (r *Registry) RegisterScoped(key string, factory func(ctx context.Context) any) {
	r.register(key, &entry{factory: factory, scoped: true})
}

func (r *Registry) register(key string, e *entry) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.entries[key]; exists {
		panic(fmt.Sprintf("grr: key %q already registered", key))
	}
	r.entries[key] = e
}

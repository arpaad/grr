package grr

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// Resolve looks up key, walking the parent chain if necessary, and
// produces an instance via the registered factory. Panics if key isn't
// registered anywhere in the chain, or if key is scoped but ctx carries
// no active scope (see BeginScope).
func (r *Registry) Resolve(ctx context.Context, key string) any {
	if r.hooks.OnResolve != nil {
		start := time.Now()
		defer func() { r.hooks.OnResolve(key, time.Since(start)) }()
	}

	e := r.findEntry(key)
	if e == nil {
		panic(fmt.Sprintf("grr: key %q not registered", key))
	}

	chain := buildChainFromCtx(ctx)
	for _, k := range chain {
		if k == key {
			panic(fmt.Sprintf("grr: circular dependency: %s", strings.Join(append(chain, key), " -> ")))
		}
	}

	if !e.scoped {
		return e.factory(withBuildChain(ctx, key))
	}

	scopeID, ok := scopeIDFromCtx(ctx)
	if !ok {
		panic(fmt.Sprintf("grr: key %q is scoped but ctx has no active scope (call BeginScope first)", key))
	}

	sd, ok := r.scopes.get(scopeID)
	if !ok {
		// Scope was ended (or never began) — treat as programmer error.
		panic(fmt.Sprintf("grr: key %q resolved after its scope ended", key))
	}

	ov := sd.onceFor(key)
	ov.once.Do(func() {
		ov.value = e.factory(withBuildChain(ctx, key))
	})
	return ov.value
}

// ResolveOK is like Resolve but reports a missing key with ok == false
// instead of panicking — for genuinely conditional lookups (e.g. an
// optional, feature-flagged registration). It does NOT soften real misuse:
// resolving a scoped key with no active scope, a circular dependency, or
// resolving after a scope ended still panic, because those are bugs, not
// conditions to branch on.
func (r *Registry) ResolveOK(ctx context.Context, key string) (any, bool) {
	if r.findEntry(key) == nil {
		return nil, false
	}
	return r.Resolve(ctx, key), true
}

// Get is sugar for Resolve(context.Background(), key). Panics if key
// resolves to a scoped entry — there is no scope in a bare background
// context.
func (r *Registry) Get(key string) any {
	return r.Resolve(context.Background(), key)
}

// findEntry walks the parent chain looking for key.
func (r *Registry) findEntry(key string) *entry {
	for reg := r; reg != nil; reg = reg.parent {
		reg.mu.RLock()
		e, ok := reg.entries[key]
		reg.mu.RUnlock()
		if ok {
			return e
		}
	}
	return nil
}

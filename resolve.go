package grr

import (
	"context"
	"fmt"
	"strings"
)

// Resolve looks up key, walking the parent chain if necessary, and
// produces an instance via the registered factory. Panics if key isn't
// registered anywhere in the chain, or if key is scoped but ctx carries
// no active scope (see BeginScope).
func (r *Registry) Resolve(ctx context.Context, key string) any {
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

	scopesMu.RLock()
	sd, ok := scopes[scopeID]
	scopesMu.RUnlock()
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

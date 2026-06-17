package grr

import (
	"context"
	"sync"
	"sync/atomic"
)

type scopeKey struct{}

// onceValue lazily produces a value exactly once, independent of any
// scope-wide lock — this is what lets a scoped factory resolve other
// scoped keys in the same scope without deadlocking.
type onceValue struct {
	once  sync.Once
	value any
}

type scopeData struct {
	mu   sync.Mutex
	once map[string]*onceValue
}

func (sd *scopeData) onceFor(key string) *onceValue {
	sd.mu.Lock()
	defer sd.mu.Unlock()
	ov, ok := sd.once[key]
	if !ok {
		ov = &onceValue{}
		sd.once[key] = ov
	}
	return ov
}

// scopeCounter hands out process-globally-unique scope IDs. It's a single
// atomic add (no lock, negligible contention), kept global on purpose so an
// ID minted by one store can never collide with another store's — using a
// context from one registry chain against an unrelated one then correctly
// misses and panics, instead of silently hitting a same-numbered scope.
var scopeCounter uint64

// scopeStore owns the scope cache for a registry chain: the live scopes and
// the mutex guarding them. One store is shared across a parent/child chain
// (NewFrom shares the parent's), while an independent New() gets its own —
// so isolated registries have isolated scope state and don't contend on a
// single process-wide lock.
type scopeStore struct {
	mu     sync.RWMutex
	scopes map[uint64]*scopeData
}

func newScopeStore() *scopeStore {
	return &scopeStore{scopes: make(map[uint64]*scopeData)}
}

func (s *scopeStore) begin() uint64 {
	id := atomic.AddUint64(&scopeCounter, 1)
	s.mu.Lock()
	s.scopes[id] = &scopeData{once: make(map[string]*onceValue)}
	s.mu.Unlock()
	return id
}

func (s *scopeStore) get(id uint64) (*scopeData, bool) {
	s.mu.RLock()
	sd, ok := s.scopes[id]
	s.mu.RUnlock()
	return sd, ok
}

func (s *scopeStore) cleanup(id uint64) {
	s.mu.Lock()
	delete(s.scopes, id)
	s.mu.Unlock()
}

// BeginScope starts a new scope tied to ctx and returns a derived context
// plus an endScope function that releases the scope's cached instances.
// endScope is safe to call multiple times. If ctx is cancelled before
// endScope is called explicitly, the scope is cleaned up automatically.
//
// Scope storage is shared across a whole parent/child registry chain, so a
// scoped entry registered on a parent resolves correctly even when
// BeginScope was called on a child.
func (r *Registry) BeginScope(parent context.Context) (context.Context, func()) {
	store := r.scopes
	scopeID := store.begin()
	ctx := context.WithValue(parent, scopeKey{}, scopeID)

	if r.hooks.OnScopeBegin != nil {
		r.hooks.OnScopeBegin()
	}

	// release runs the actual teardown exactly once, whichever path gets
	// there first (explicit endScope or ctx cancellation).
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
			// endScope already released synchronously.
		}
	}()

	var endOnce sync.Once
	endScope := func() {
		endOnce.Do(func() {
			release() // synchronous teardown — callers expect it done on return
			close(stop)
		})
	}

	return ctx, endScope
}

func scopeIDFromCtx(ctx context.Context) (uint64, bool) {
	id, ok := ctx.Value(scopeKey{}).(uint64)
	return id, ok
}

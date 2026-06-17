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

var (
	scopeCounter uint64

	scopesMu sync.RWMutex
	scopes   = make(map[uint64]*scopeData)
)

// BeginScope starts a new scope tied to ctx and returns a derived context
// plus an endScope function that releases the scope's cached instances.
// endScope is safe to call multiple times. If ctx is cancelled before
// endScope is called explicitly, the scope is cleaned up automatically.
//
// Scope storage is shared across a whole parent/child registry chain
// (scope IDs are globally unique), so a scoped entry registered on a
// parent resolves correctly even when BeginScope was called on a child.
func (r *Registry) BeginScope(parent context.Context) (context.Context, func()) {
	scopeID := atomic.AddUint64(&scopeCounter, 1)
	ctx := context.WithValue(parent, scopeKey{}, scopeID)

	scopesMu.Lock()
	scopes[scopeID] = &scopeData{once: make(map[string]*onceValue)}
	scopesMu.Unlock()

	stop := make(chan struct{})

	go func() {
		select {
		case <-ctx.Done():
			cleanupScope(scopeID)
		case <-stop:
			// endScope already cleaned up synchronously.
		}
	}()

	var once sync.Once
	endScope := func() {
		once.Do(func() {
			cleanupScope(scopeID)
			close(stop)
		})
	}

	return ctx, endScope
}

func cleanupScope(scopeID uint64) {
	scopesMu.Lock()
	defer scopesMu.Unlock()
	delete(scopes, scopeID)
}

func scopeIDFromCtx(ctx context.Context) (uint64, bool) {
	id, ok := ctx.Value(scopeKey{}).(uint64)
	return id, ok
}

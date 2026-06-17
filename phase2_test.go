package grr

import (
	"context"
	"sort"
	"sync/atomic"
	"testing"
	"time"
)

func TestIsolatedRegistriesHaveSeparateScopeState(t *testing.T) {
	r1 := New()
	r2 := New()
	r1.RegisterScoped("conn", func(ctx context.Context) any { return "r1" })
	r2.RegisterScoped("conn", func(ctx context.Context) any { return "r2" })

	ctx1, end1 := r1.BeginScope(context.Background())
	defer end1()
	ctx2, end2 := r2.BeginScope(context.Background())
	defer end2()

	if v := r1.Resolve(ctx1, "conn"); v != "r1" {
		t.Fatalf("r1 resolved %v, want r1", v)
	}
	if v := r2.Resolve(ctx2, "conn"); v != "r2" {
		t.Fatalf("r2 resolved %v, want r2", v)
	}

	// Cross-using the other registry's context must not find this scope —
	// the stores are independent, so scope IDs don't collide across them.
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic resolving r2's scoped key with r1's scope context")
		}
	}()
	r2.Resolve(ctx1, "conn")
}

func TestKeysDirectOnly(t *testing.T) {
	parent := New()
	parent.Set("p", 1)
	child := NewFrom(parent)
	child.Set("c", 2)
	child.Set("d", 3)

	got := child.Keys()
	sort.Strings(got)
	if len(got) != 2 || got[0] != "c" || got[1] != "d" {
		t.Fatalf("Keys() = %v, want [c d] (parent keys must not leak)", got)
	}
}

func TestResolveOKMissingKey(t *testing.T) {
	r := New()
	if v, ok := r.ResolveOK(context.Background(), "nope"); ok || v != nil {
		t.Fatalf("ResolveOK(missing) = (%v, %v), want (nil, false)", v, ok)
	}
}

func TestResolveOKPresentKey(t *testing.T) {
	r := New()
	r.Set("x", 42)
	if v, ok := r.ResolveOK(context.Background(), "x"); !ok || v != 42 {
		t.Fatalf("ResolveOK(present) = (%v, %v), want (42, true)", v, ok)
	}
}

func TestResolveOKStillPanicsOnScopedWithoutScope(t *testing.T) {
	r := New()
	r.RegisterScoped("conn", func(ctx context.Context) any { return 1 })
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic: ResolveOK must not soften scoped-without-scope misuse")
		}
	}()
	r.ResolveOK(context.Background(), "conn")
}

func TestResolveOKStillPanicsOnCircular(t *testing.T) {
	r := New()
	r.Register("a", func(ctx context.Context) any { return r.Resolve(ctx, "a") })
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic: ResolveOK must not soften circular dependency")
		}
	}()
	r.ResolveOK(context.Background(), "a")
}

func TestOnResolveHookFires(t *testing.T) {
	var calls int32
	var lastKey string
	r := New(WithHooks(Hooks{
		OnResolve: func(key string, dur time.Duration) {
			atomic.AddInt32(&calls, 1)
			lastKey = key
		},
	}))
	r.Set("x", 1)
	r.Get("x")

	if atomic.LoadInt32(&calls) != 1 {
		t.Fatalf("OnResolve fired %d times, want 1", calls)
	}
	if lastKey != "x" {
		t.Fatalf("OnResolve key = %q, want x", lastKey)
	}
}

func TestScopeHooksFireOnEndScope(t *testing.T) {
	var begin, end int32
	r := New(WithHooks(Hooks{
		OnScopeBegin: func() { atomic.AddInt32(&begin, 1) },
		OnScopeEnd:   func() { atomic.AddInt32(&end, 1) },
	}))

	_, endScope := r.BeginScope(context.Background())
	if atomic.LoadInt32(&begin) != 1 {
		t.Fatalf("OnScopeBegin fired %d times, want 1", begin)
	}
	endScope()
	endScope() // idempotent — must not fire OnScopeEnd twice
	if atomic.LoadInt32(&end) != 1 {
		t.Fatalf("OnScopeEnd fired %d times, want 1", end)
	}
}

func TestScopeEndHookFiresOnCtxCancel(t *testing.T) {
	var end int32
	r := New(WithHooks(Hooks{
		OnScopeEnd: func() { atomic.AddInt32(&end, 1) },
	}))

	parent, cancel := context.WithCancel(context.Background())
	_, endScope := r.BeginScope(parent)
	defer endScope()

	cancel()
	time.Sleep(50 * time.Millisecond) // cleanup runs in a goroutine

	if atomic.LoadInt32(&end) != 1 {
		t.Fatalf("OnScopeEnd fired %d times after ctx cancel, want 1", end)
	}

	endScope() // releasing again must not fire OnScopeEnd a second time
	if atomic.LoadInt32(&end) != 1 {
		t.Fatalf("OnScopeEnd fired %d times after endScope following cancel, want 1", end)
	}
}

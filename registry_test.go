package grr

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestSetReturnsSameValue(t *testing.T) {
	r := New()
	r.Set("greeting", "hello")

	if got := r.Get("greeting"); got != "hello" {
		t.Fatalf("got %v, want hello", got)
	}
}

func TestRegisterTransientByConvention(t *testing.T) {
	r := New()
	n := 0
	r.Register("counter", func(ctx context.Context) any {
		n++
		return n
	})

	if v := r.Get("counter"); v != 1 {
		t.Fatalf("first call = %v, want 1", v)
	}
	if v := r.Get("counter"); v != 2 {
		t.Fatalf("second call = %v, want 2", v)
	}
}

func TestRegisterSingletonByConvention(t *testing.T) {
	r := New()
	var once sync.Once
	value := 0
	r.Register("singleton", func(ctx context.Context) any {
		once.Do(func() { value = 42 })
		return value
	})

	if v := r.Get("singleton"); v != 42 {
		t.Fatalf("got %v, want 42", v)
	}
	if v := r.Get("singleton"); v != 42 {
		t.Fatalf("got %v, want 42 (singleton should stay constant)", v)
	}
}

func TestRegisterFunc(t *testing.T) {
	r := New()
	r.RegisterFunc("answer", func() any { return 42 })

	if v := r.Get("answer"); v != 42 {
		t.Fatalf("got %v, want 42", v)
	}
}

func TestDuplicateRegistrationPanics(t *testing.T) {
	r := New()
	r.Set("key", 1)

	defer func() {
		if recover() == nil {
			t.Fatal("expected panic on duplicate registration")
		}
	}()
	r.Set("key", 2)
}

func TestResolveMissingKeyPanics(t *testing.T) {
	r := New()
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic on missing key")
		}
	}()
	r.Get("nope")
}

func TestParentChainFallback(t *testing.T) {
	parent := New()
	parent.Set("db", "parent-db")

	child := NewFrom(parent)

	if v := child.Get("db"); v != "parent-db" {
		t.Fatalf("got %v, want parent-db", v)
	}
}

func TestChildOverridesParent(t *testing.T) {
	parent := New()
	parent.Set("db", "parent-db")

	child := NewFrom(parent)
	child.Set("db", "child-db")

	if v := child.Get("db"); v != "child-db" {
		t.Fatalf("got %v, want child-db", v)
	}
	if v := parent.Get("db"); v != "parent-db" {
		t.Fatalf("parent mutated: got %v, want parent-db", v)
	}
}

func TestScopedResolveWithoutScopePanics(t *testing.T) {
	r := New()
	r.RegisterScoped("conn", func(ctx context.Context) any { return new(int) })

	defer func() {
		if recover() == nil {
			t.Fatal("expected panic resolving scoped key without an active scope")
		}
	}()
	r.Resolve(context.Background(), "conn")
}

func TestScopedReturnsSameInstanceWithinScope(t *testing.T) {
	r := New()
	calls := 0
	r.RegisterScoped("conn", func(ctx context.Context) any {
		calls++
		return calls
	})

	ctx, end := r.BeginScope(context.Background())
	defer end()

	a := r.Resolve(ctx, "conn")
	b := r.Resolve(ctx, "conn")

	if a != b {
		t.Fatalf("expected same instance within scope, got %v and %v", a, b)
	}
	if calls != 1 {
		t.Fatalf("factory called %d times, want 1", calls)
	}
}

func TestScopedNewInstancePerScope(t *testing.T) {
	r := New()
	calls := 0
	r.RegisterScoped("conn", func(ctx context.Context) any {
		calls++
		return calls
	})

	ctx1, end1 := r.BeginScope(context.Background())
	v1 := r.Resolve(ctx1, "conn")
	end1()

	ctx2, end2 := r.BeginScope(context.Background())
	defer end2()
	v2 := r.Resolve(ctx2, "conn")

	if v1 == v2 {
		t.Fatalf("expected different instances across scopes, got %v and %v", v1, v2)
	}
}

func TestScopedEntryOwnedByParentResolvesViaChildScope(t *testing.T) {
	parent := New()
	calls := 0
	parent.RegisterScoped("conn", func(ctx context.Context) any {
		calls++
		return calls
	})

	child := NewFrom(parent)
	ctx, end := child.BeginScope(context.Background())
	defer end()

	a := child.Resolve(ctx, "conn")
	b := child.Resolve(ctx, "conn")

	if a != b {
		t.Fatalf("expected same instance, got %v and %v", a, b)
	}
	if calls != 1 {
		t.Fatalf("factory called %d times, want 1", calls)
	}
}

func TestEndScopeIsIdempotent(t *testing.T) {
	r := New()
	_, end := r.BeginScope(context.Background())
	end()
	end() // must not panic
}

func TestResolveAfterScopeEndedPanics(t *testing.T) {
	r := New()
	r.RegisterScoped("conn", func(ctx context.Context) any { return 1 })

	ctx, end := r.BeginScope(context.Background())
	end()

	defer func() {
		if recover() == nil {
			t.Fatal("expected panic resolving a key whose scope already ended")
		}
	}()
	r.Resolve(ctx, "conn")
}

func TestCtxCancellationCleansUpScope(t *testing.T) {
	r := New()
	r.RegisterScoped("conn", func(ctx context.Context) any { return 1 })

	parent, cancel := context.WithCancel(context.Background())
	ctx, end := r.BeginScope(parent)
	defer end()

	cancel()
	// Cleanup runs in a goroutine — give it a moment.
	time.Sleep(50 * time.Millisecond)

	defer func() {
		if recover() == nil {
			t.Fatal("expected panic — scope should have been cleaned up on ctx cancellation")
		}
	}()
	r.Resolve(ctx, "conn")
}

func TestScopedFactoryCanResolveAnotherScopedKey(t *testing.T) {
	r := New()
	r.RegisterScoped("db", func(ctx context.Context) any { return "db-conn" })
	r.RegisterScoped("repo", func(ctx context.Context) any {
		db := r.Resolve(ctx, "db") // nested resolve within the same scope
		return "repo(" + db.(string) + ")"
	})

	ctx, end := r.BeginScope(context.Background())
	defer end()

	if v := r.Resolve(ctx, "repo"); v != "repo(db-conn)" {
		t.Fatalf("got %v, want repo(db-conn)", v)
	}
}

func TestCircularScopedDependencyPanics(t *testing.T) {
	r := New()
	r.RegisterScoped("a", func(ctx context.Context) any { return r.Resolve(ctx, "b") })
	r.RegisterScoped("b", func(ctx context.Context) any { return r.Resolve(ctx, "a") })

	ctx, end := r.BeginScope(context.Background())
	defer end()

	defer func() {
		rec := recover()
		if rec == nil {
			t.Fatal("expected panic on circular scoped dependency")
		}
		msg, _ := rec.(string)
		if !strings.Contains(msg, "a -> b -> a") {
			t.Fatalf("panic message %q does not describe the cycle", msg)
		}
	}()
	r.Resolve(ctx, "a")
}

func TestCircularDependencyThreeKeysPanics(t *testing.T) {
	r := New()
	r.RegisterScoped("a", func(ctx context.Context) any { return r.Resolve(ctx, "b") })
	r.RegisterScoped("b", func(ctx context.Context) any { return r.Resolve(ctx, "c") })
	r.RegisterScoped("c", func(ctx context.Context) any { return r.Resolve(ctx, "a") })

	ctx, end := r.BeginScope(context.Background())
	defer end()

	defer func() {
		rec := recover()
		if rec == nil {
			t.Fatal("expected panic on circular scoped dependency")
		}
		msg, _ := rec.(string)
		if !strings.Contains(msg, "a -> b -> c -> a") {
			t.Fatalf("panic message %q does not describe the cycle", msg)
		}
	}()
	r.Resolve(ctx, "a")
}

func TestCircularNonScopedDependencyPanics(t *testing.T) {
	r := New()
	r.Register("a", func(ctx context.Context) any { return r.Resolve(ctx, "a") })

	defer func() {
		if recover() == nil {
			t.Fatal("expected panic on self-referencing non-scoped dependency")
		}
	}()
	r.Get("a")
}

func TestScopedConcurrentResolveBuildsOnce(t *testing.T) {
	r := New()
	builds := 0
	var buildMu sync.Mutex
	r.RegisterScoped("conn", func(ctx context.Context) any {
		buildMu.Lock()
		builds++
		buildMu.Unlock()
		return 1
	})

	ctx, end := r.BeginScope(context.Background())
	defer end()

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r.Resolve(ctx, "conn")
		}()
	}
	wg.Wait()

	if builds != 1 {
		t.Fatalf("factory built %d times, want 1", builds)
	}
}

func TestIsRegistered(t *testing.T) {
	parent := New()
	parent.Set("db", "x")
	child := NewFrom(parent)

	if !child.IsRegistered("db") {
		t.Fatal("expected db to be registered via parent chain")
	}
	if child.IsRegistered("nope") {
		t.Fatal("expected nope to be unregistered")
	}
}

func TestClear(t *testing.T) {
	r := New()
	r.Set("key", 1)
	r.Clear()

	if r.IsRegistered("key") {
		t.Fatal("expected key to be gone after Clear")
	}
	// Should be re-registerable now.
	r.Set("key", 2)
	if v := r.Get("key"); v != 2 {
		t.Fatalf("got %v, want 2", v)
	}
}

func TestConcurrentResolve(t *testing.T) {
	r := New()
	r.Set("key", 1)

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r.Get("key")
		}()
	}
	wg.Wait()
}

func TestWithRegistryAndRegistryFromCtx(t *testing.T) {
	r := New()
	ctx := WithRegistry(context.Background(), r)

	if got := RegistryFromCtx(ctx); got != r {
		t.Fatalf("got %v, want %v", got, r)
	}
}

func TestRegistryFromCtxFallsBackToDefault(t *testing.T) {
	if got := RegistryFromCtx(context.Background()); got != Default {
		t.Fatalf("got %v, want Default", got)
	}
}

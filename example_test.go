package grr_test

import (
	"context"
	"fmt"

	"github.com/arpaad/grr"
)

// Set/Get is sugar for a fixed value — always the same instance.
func ExampleRegistry_Set() {
	r := grr.New()
	r.Set("greeting", "hello")

	fmt.Println(r.Get("greeting"))
	// Output: hello
}

// RegisterScoped ties an instance to a scope (see BeginScope): the same
// instance comes back for every Resolve call within that scope.
func ExampleRegistry_BeginScope() {
	r := grr.New()
	calls := 0
	r.RegisterScoped("conn", func(ctx context.Context) any {
		calls++
		return fmt.Sprintf("conn-%d", calls)
	})

	ctx, end := r.BeginScope(context.Background())
	defer end()

	a := r.Resolve(ctx, "conn")
	b := r.Resolve(ctx, "conn")
	fmt.Println(a, b, a == b)
	// Output: conn-1 conn-1 true
}

// NewFrom builds a registry that falls back to parent for keys it
// doesn't have itself — handy for tests that only need to override one
// dependency.
func ExampleNewFrom() {
	parent := grr.New()
	parent.Set("db", "parent-db")

	child := grr.NewFrom(parent)
	fmt.Println(child.Get("db")) // falls back to parent

	child.Set("db", "child-db")
	fmt.Println(child.Get("db"), parent.Get("db")) // child overrides, parent untouched
	// Output:
	// parent-db
	// child-db parent-db
}

func ExampleRegistry_IsRegistered() {
	r := grr.New()
	fmt.Println(r.IsRegistered("db"))

	r.Set("db", "conn")
	fmt.Println(r.IsRegistered("db"))
	// Output:
	// false
	// true
}

// A scoped factory may resolve another scoped key from the same scope —
// useful when one dependency is built from another (e.g. a repo needs a
// transaction). The opposite — a cycle — panics instead of deadlocking.
func ExampleRegistry_Resolve_nestedScopedDependency() {
	r := grr.New()
	r.RegisterScoped("db", func(ctx context.Context) any { return "db-conn" })
	r.RegisterScoped("repo", func(ctx context.Context) any {
		db := r.Resolve(ctx, "db")
		return fmt.Sprintf("repo(%s)", db)
	})

	ctx, end := r.BeginScope(context.Background())
	defer end()

	fmt.Println(r.Resolve(ctx, "repo"))
	// Output: repo(db-conn)
}

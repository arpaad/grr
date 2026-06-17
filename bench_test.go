package grr

import (
	"context"
	"testing"
)

func BenchmarkResolveNonScoped(b *testing.B) {
	r := New()
	r.Set("x", 42)
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = r.Resolve(ctx, "x")
	}
}

func BenchmarkResolveScopedCached(b *testing.B) {
	r := New()
	r.RegisterScoped("conn", func(ctx context.Context) any { return 1 })
	ctx, end := r.BeginScope(context.Background())
	defer end()
	_ = r.Resolve(ctx, "conn") // prime the cache
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = r.Resolve(ctx, "conn")
	}
}

func BenchmarkResolveScopedFirstCall(b *testing.B) {
	r := New()
	r.RegisterScoped("conn", func(ctx context.Context) any { return 1 })
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		ctx, end := r.BeginScope(context.Background())
		b.StartTimer()
		_ = r.Resolve(ctx, "conn")
		b.StopTimer()
		end()
		b.StartTimer()
	}
}

func BenchmarkParentChainLookup(b *testing.B) {
	root := New()
	root.Set("x", 42)
	r := root
	for i := 0; i < 8; i++ { // 8 levels deep
		r = NewFrom(r)
	}
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = r.Resolve(ctx, "x")
	}
}

func BenchmarkConcurrentResolve(b *testing.B) {
	r := New()
	r.Set("x", 42)
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_ = r.Resolve(ctx, "x")
		}
	})
}

// BenchmarkBeginEndScope measures the open+close cost of a scope. Run with
// -cpu=1,2,4,8 to see how the per-chain scopeStore lock behaves under
// contention vs. the old process-global mutex.
func BenchmarkBeginEndScope(b *testing.B) {
	r := New()
	bg := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, end := r.BeginScope(bg)
			end()
		}
	})
}

package grr

import "context"

// buildChainKey carries the list of scoped keys currently being built on
// this resolution path — i.e. the chain of nested Resolve calls that got
// us to the current factory invocation. It travels through ctx because
// the chain belongs to a logical call stack, not to a goroutine or a
// scope as a whole (two independent Resolve calls in the same scope must
// not see each other's chain).
type buildChainKey struct{}

func buildChainFromCtx(ctx context.Context) []string {
	chain, _ := ctx.Value(buildChainKey{}).([]string)
	return chain
}

func withBuildChain(ctx context.Context, key string) context.Context {
	chain := buildChainFromCtx(ctx)
	next := make([]string, len(chain)+1)
	copy(next, chain)
	next[len(chain)] = key
	return context.WithValue(ctx, buildChainKey{}, next)
}

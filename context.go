package grr

import "context"

type registryKey struct{}

// WithRegistry attaches r to ctx so it can be recovered later via
// RegistryFromCtx (used by middleware to inject a registry per request).
func WithRegistry(ctx context.Context, r *Registry) context.Context {
	return context.WithValue(ctx, registryKey{}, r)
}

// RegistryFromCtx recovers the registry attached to ctx via WithRegistry,
// falling back to Default if none was attached.
func RegistryFromCtx(ctx context.Context) *Registry {
	if r, ok := ctx.Value(registryKey{}).(*Registry); ok {
		return r
	}
	return Default
}

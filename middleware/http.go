// Package middleware provides net/http (and any router built on it, e.g.
// Chi) integration for grr: injecting a registry into the request context
// and managing a per-request scope.
package middleware

import (
	"net/http"

	"github.com/arpaad/grr"
)

// Middleware attaches r to each request's context and begins a scope that
// lives for the duration of the request.
func Middleware(r *grr.Registry) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			ctx := grr.WithRegistry(req.Context(), r)
			ctx, endScope := r.BeginScope(ctx)
			defer endScope()
			next.ServeHTTP(w, req.WithContext(ctx))
		})
	}
}

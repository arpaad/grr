package middleware_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/arpaad/grr"
	"github.com/arpaad/grr/middleware"
)

func TestMiddlewareInjectsRegistryAndScope(t *testing.T) {
	r := grr.New()
	calls := 0
	r.RegisterScoped("conn", func(ctx context.Context) any {
		calls++
		return calls
	})

	handler := middleware.Middleware(r)(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		a := r.Resolve(req.Context(), "conn")
		b := r.Resolve(req.Context(), "conn")
		if a != b {
			t.Fatalf("expected same scoped instance within one request, got %v and %v", a, b)
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("got status %d, want 200", w.Code)
	}
	if calls != 1 {
		t.Fatalf("factory called %d times, want 1", calls)
	}
}

func TestMiddlewareScopeEndsAfterRequest(t *testing.T) {
	r := grr.New()
	r.RegisterScoped("conn", func(ctx context.Context) any { return 1 })

	var savedCtx context.Context
	handler := middleware.Middleware(r)(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		savedCtx = req.Context()
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	defer func() {
		if recover() == nil {
			t.Fatal("expected panic resolving scoped key after request scope ended")
		}
	}()
	r.Resolve(savedCtx, "conn")
}

package auth_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"esaproperti/internal/platform/auth"
)

// nextOK is a trivial handler that always returns 200.
var nextOK = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
})

func applyMiddleware(secret string, next http.Handler) http.Handler {
	return auth.Middleware(secret)(next)
}

func TestMiddleware_NoAuthHeader_Returns401(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	applyMiddleware(testSecret, nextOK).ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestMiddleware_EmptyBearerToken_Returns401(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer ")
	w := httptest.NewRecorder()
	applyMiddleware(testSecret, nextOK).ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestMiddleware_MalformedToken_Returns401(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer not.a.valid.jwt")
	w := httptest.NewRecorder()
	applyMiddleware(testSecret, nextOK).ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestMiddleware_WrongSecret_Returns401(t *testing.T) {
	token, _ := auth.Generate("other-secret-32-chars-minimum-xx", 1, 1, "owner", time.Hour)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	applyMiddleware(testSecret, nextOK).ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestMiddleware_ExpiredToken_Returns401(t *testing.T) {
	token, _ := auth.Generate(testSecret, 1, 1, "owner", -time.Second)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	applyMiddleware(testSecret, nextOK).ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestMiddleware_ValidToken_Returns200AndSetsContext(t *testing.T) {
	token, _ := auth.Generate(testSecret, 55, 3, "accountant", time.Hour)

	var (
		capturedTenantID uint64
		capturedRole     string
		capturedUserID   uint64
	)
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedTenantID, _ = auth.TenantIDFrom(r.Context())
		capturedRole, _ = auth.RoleFrom(r.Context())
		capturedUserID, _ = auth.UserIDFrom(r.Context())
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	applyMiddleware(testSecret, inner).ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	if capturedTenantID != 55 {
		t.Errorf("TenantID: got %d, want 55", capturedTenantID)
	}
	if capturedRole != "accountant" {
		t.Errorf("Role: got %s, want accountant", capturedRole)
	}
	if capturedUserID != 3 {
		t.Errorf("UserID: got %d, want 3", capturedUserID)
	}
}

// TestMiddleware_TwoRequestsDifferentTenants: each request sees its own tenant only.
func TestMiddleware_TwoRequestsDifferentTenants_ContextIsolated(t *testing.T) {
	tokenA, _ := auth.Generate(testSecret, 10, 1, "owner", time.Hour)
	tokenB, _ := auth.Generate(testSecret, 20, 2, "viewer", time.Hour)

	results := make([]uint64, 2)
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		idx := r.Header.Get("X-Test-Index")
		tid, _ := auth.TenantIDFrom(r.Context())
		if idx == "0" {
			results[0] = tid
		} else {
			results[1] = tid
		}
		w.WriteHeader(http.StatusOK)
	})
	h := applyMiddleware(testSecret, inner)

	for i, tok := range []string{tokenA, tokenB} {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("Authorization", "Bearer "+tok)
		if i == 0 {
			req.Header.Set("X-Test-Index", "0")
		} else {
			req.Header.Set("X-Test-Index", "1")
		}
		h.ServeHTTP(httptest.NewRecorder(), req)
	}

	if results[0] != 10 {
		t.Errorf("request 0: expected tenant 10, got %d", results[0])
	}
	if results[1] != 20 {
		t.Errorf("request 1: expected tenant 20, got %d", results[1])
	}
}

// ── RequireRole tests ─────────────────────────────────────────────────────────

func requireRoleHandler(role string, secret string) http.Handler {
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	return auth.Middleware(secret)(auth.RequireRole(role)(inner))
}

func TestRequireRole_OwnerAccessingOwnerRoute_Returns200(t *testing.T) {
	token, _ := auth.Generate(testSecret, 1, 1, "owner", time.Hour)
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	requireRoleHandler("owner", testSecret).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestRequireRole_ViewerAccessingOwnerRoute_Returns403(t *testing.T) {
	token, _ := auth.Generate(testSecret, 1, 1, "viewer", time.Hour)
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	requireRoleHandler("owner", testSecret).ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", w.Code)
	}
}

func TestRequireWrite_AccountantCanWrite(t *testing.T) {
	token, _ := auth.Generate(testSecret, 1, 1, "accountant", time.Hour)
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	h := auth.Middleware(testSecret)(auth.RequireWrite()(inner))

	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestRequireWrite_ViewerCannotWrite(t *testing.T) {
	token, _ := auth.Generate(testSecret, 1, 1, "viewer", time.Hour)
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	h := auth.Middleware(testSecret)(auth.RequireWrite()(inner))

	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", w.Code)
	}
}

// TestNoClaimsInContext: RequireRole must return 403 if called without Middleware upstream.
func TestRequireRole_MissingClaims_Returns403(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	h := auth.RequireRole("owner")(inner)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	// No auth middleware, no claims in context.
	ctx := context.Background()
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req.WithContext(ctx))
	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", w.Code)
	}
}

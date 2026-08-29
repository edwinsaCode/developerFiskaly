package auth

import (
	"encoding/json"
	"net/http"
	"strings"
)

// Middleware validates the Authorization: Bearer <token> header.
// On success it stores the Claims in the request context via WithClaims.
// Handlers downstream should call TenantIDFrom / RoleFrom to read the values.
func Middleware(secret string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			tokenStr, ok := bearerToken(r)
			if !ok {
				writeAuthError(w, http.StatusUnauthorized, "authorization header required (Bearer token)")
				return
			}
			claims, err := Parse(secret, tokenStr)
			if err != nil {
				writeAuthError(w, http.StatusUnauthorized, "invalid or expired token")
				return
			}
			next.ServeHTTP(w, r.WithContext(WithClaims(r.Context(), claims)))
		})
	}
}

// RequireRole returns a middleware that permits only requests whose JWT role is in `allowed`.
// Must be placed after Middleware (requires claims in context).
func RequireRole(allowed ...string) func(http.Handler) http.Handler {
	set := make(map[string]bool, len(allowed))
	for _, r := range allowed {
		set[r] = true
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			role, ok := RoleFrom(r.Context())
			if !ok || !set[role] {
				writeAuthError(w, http.StatusForbidden, "role tidak cukup untuk operasi ini")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// RequireWrite is a shorthand for RequireRole("owner", "accountant").
func RequireWrite() func(http.Handler) http.Handler {
	return RequireRole("owner", "accountant")
}

// ── helpers ───────────────────────────────────────────────────────────────────

func bearerToken(r *http.Request) (string, bool) {
	h := r.Header.Get("Authorization")
	if h == "" || !strings.HasPrefix(h, "Bearer ") {
		return "", false
	}
	t := strings.TrimPrefix(h, "Bearer ")
	return t, t != ""
}

func writeAuthError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

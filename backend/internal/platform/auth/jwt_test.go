package auth_test

import (
	"context"
	"testing"
	"time"

	"esaproperti/internal/platform/auth"
)

const testSecret = "phase2-test-secret-exactly-32-ch"

func TestGenerate_Parse_RoundTrip(t *testing.T) {
	token, err := auth.Generate(testSecret, 42, 7, "owner", time.Hour)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if token == "" {
		t.Fatal("expected non-empty token")
	}
	claims, err := auth.Parse(testSecret, token)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if claims.TenantID != 42 {
		t.Errorf("TenantID: got %d, want 42", claims.TenantID)
	}
	if claims.UserID != 7 {
		t.Errorf("UserID: got %d, want 7", claims.UserID)
	}
	if claims.Role != "owner" {
		t.Errorf("Role: got %s, want owner", claims.Role)
	}
}

func TestParse_ExpiredToken_ReturnsError(t *testing.T) {
	token, err := auth.Generate(testSecret, 1, 1, "viewer", -time.Second)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	_, err = auth.Parse(testSecret, token)
	if err == nil {
		t.Error("expected error for expired token, got nil")
	}
}

func TestParse_WrongSecret_ReturnsError(t *testing.T) {
	token, err := auth.Generate(testSecret, 1, 1, "accountant", time.Hour)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	_, err = auth.Parse("wrong-secret", token)
	if err == nil {
		t.Error("expected error for wrong secret, got nil")
	}
}

func TestParse_TamperedPayload_ReturnsError(t *testing.T) {
	_, err := auth.Parse(testSecret, "eyJhbGciOiJIUzI1NiJ9.eyJ0aWQiOjl9.invalid")
	if err == nil {
		t.Error("expected error for tampered token, got nil")
	}
}

func TestParse_EmptyToken_ReturnsError(t *testing.T) {
	_, err := auth.Parse(testSecret, "")
	if err == nil {
		t.Error("expected error for empty token, got nil")
	}
}

func TestContextHelpers_RoundTrip(t *testing.T) {
	token, _ := auth.Generate(testSecret, 7, 2, "owner", time.Hour)
	claims, _ := auth.Parse(testSecret, token)

	ctx := auth.WithClaims(context.Background(), claims)

	tid, ok := auth.TenantIDFrom(ctx)
	if !ok || tid != 7 {
		t.Errorf("TenantIDFrom: ok=%v tid=%d", ok, tid)
	}
	uid, ok := auth.UserIDFrom(ctx)
	if !ok || uid != 2 {
		t.Errorf("UserIDFrom: ok=%v uid=%d", ok, uid)
	}
	role, ok := auth.RoleFrom(ctx)
	if !ok || role != "owner" {
		t.Errorf("RoleFrom: ok=%v role=%s", ok, role)
	}
}

func TestContextHelpers_EmptyContext_ReturnsFalse(t *testing.T) {
	ctx := context.Background()
	if _, ok := auth.TenantIDFrom(ctx); ok {
		t.Error("expected TenantIDFrom to return false on empty context")
	}
	if _, ok := auth.UserIDFrom(ctx); ok {
		t.Error("expected UserIDFrom to return false on empty context")
	}
	if _, ok := auth.RoleFrom(ctx); ok {
		t.Error("expected RoleFrom to return false on empty context")
	}
}

// TestDifferentTenants_ProduceDifferentTokens: two tenants must get different tokens.
func TestDifferentTenants_ProduceDifferentTokens(t *testing.T) {
	t1, _ := auth.Generate(testSecret, 1, 10, "owner", time.Hour)
	t2, _ := auth.Generate(testSecret, 2, 20, "owner", time.Hour)
	if t1 == t2 {
		t.Error("different tenants produced identical tokens")
	}

	c1, _ := auth.Parse(testSecret, t1)
	c2, _ := auth.Parse(testSecret, t2)
	if c1.TenantID == c2.TenantID {
		t.Error("token for tenant 1 parsed as tenant 2")
	}
}

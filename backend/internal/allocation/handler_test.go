package allocation

// handler_test.go: QA-4 (permission) dan QA-5 (tenant isolation) di layer HTTP.
// Menggunakan package allocation (bukan allocation_test) agar bisa akses newHandlerWithSvc.
//
// Scenario                                            Expected
// ─────────────────────────────────────────────────────────────
// QA-4a  viewer  POST /execute                        403 Forbidden
// QA-4b  viewer  GET  /history                        200 (read-only; viewers can audit)
// QA-4c  no JWT  POST /execute                        401 Unauthorized
// QA-4d  no JWT  GET  /history                        401 Unauthorized
// QA-5   tenant B GET /projects/1/allocation/history  200 + empty data (not 403)
//         Tenant isolation: SQL WHERE tenant_id filters, data never leaks.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"esaproperti/internal/platform/auth"
)

const testSecret = "phase2-test-secret-exactly-32-ch"

// ── Mock allocSvc ─────────────────────────────────────────────────────────────

type mockAllocSvc struct {
	executions []*AllocationExecution
	execErr    error
	listErr    error
}

func (m *mockAllocSvc) GetBasis(_ context.Context, _, _ uint64) (*AllocationConfig, error) {
	return &AllocationConfig{Basis: BasisSaleableArea}, nil
}
func (m *mockAllocSvc) SetBasis(_ context.Context, _, _ uint64, _ AllocationBasis) error {
	return nil
}
func (m *mockAllocSvc) ComputeAllocation(_ context.Context, _, _ uint64) ([]AllocationResult, error) {
	return nil, nil
}
func (m *mockAllocSvc) Execute(_ context.Context, tenantID, projectID, userID uint64) (*AllocationExecution, error) {
	if m.execErr != nil {
		return nil, m.execErr
	}
	return &AllocationExecution{
		ID: 1, TenantID: tenantID, ProjectID: projectID,
		Basis: BasisSaleableArea, ExecutedBy: userID,
		ExecutedAt: time.Now(),
	}, nil
}
func (m *mockAllocSvc) ListExecutions(_ context.Context, tenantID, _ uint64) ([]*AllocationExecution, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	var out []*AllocationExecution
	for _, e := range m.executions {
		if e.TenantID == tenantID {
			out = append(out, e)
		}
	}
	return out, nil
}

// ── Router helpers ────────────────────────────────────────────────────────────

func buildRouter(secret string, svc *mockAllocSvc) http.Handler {
	h := newHandlerWithSvc(svc)
	r := chi.NewRouter()
	r.Use(auth.Middleware(secret))
	h.Mount(r)
	return r
}

func bearerHeader(secret string, tenantID, userID uint64, role string) string {
	tok, _ := auth.Generate(secret, tenantID, userID, role, time.Hour)
	return "Bearer " + tok
}

// ── QA-4: Permission tests ────────────────────────────────────────────────────

// QA-4a: viewer tidak boleh mengeksekusi alokasi (write-only endpoint).
func TestHandler_Execute_ViewerRole_Returns403(t *testing.T) {
	r := buildRouter(testSecret, &mockAllocSvc{})
	req := httptest.NewRequest(http.MethodPost, "/projects/1/allocation/execute", nil)
	req.Header.Set("Authorization", bearerHeader(testSecret, 1, 1, "viewer"))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Errorf("viewer POST /execute: got %d, want 403", w.Code)
	}
}

// QA-4b: viewer boleh membaca history (read-only; audit trail publik dalam tenant).
func TestHandler_History_ViewerRole_Returns200(t *testing.T) {
	r := buildRouter(testSecret, &mockAllocSvc{})
	req := httptest.NewRequest(http.MethodGet, "/projects/1/allocation/history", nil)
	req.Header.Set("Authorization", bearerHeader(testSecret, 1, 1, "viewer"))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("viewer GET /history: got %d, want 200", w.Code)
	}
}

// QA-4c: tanpa token, POST /execute → 401.
func TestHandler_Execute_NoToken_Returns401(t *testing.T) {
	r := buildRouter(testSecret, &mockAllocSvc{})
	req := httptest.NewRequest(http.MethodPost, "/projects/1/allocation/execute", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("no-token POST /execute: got %d, want 401", w.Code)
	}
}

// QA-4d: tanpa token, GET /history → 401.
func TestHandler_History_NoToken_Returns401(t *testing.T) {
	r := buildRouter(testSecret, &mockAllocSvc{})
	req := httptest.NewRequest(http.MethodGet, "/projects/1/allocation/history", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("no-token GET /history: got %d, want 401", w.Code)
	}
}

// accountant (write role) boleh mengeksekusi.
func TestHandler_Execute_AccountantRole_Returns201(t *testing.T) {
	r := buildRouter(testSecret, &mockAllocSvc{})
	req := httptest.NewRequest(http.MethodPost, "/projects/1/allocation/execute", nil)
	req.Header.Set("Authorization", bearerHeader(testSecret, 1, 1, "accountant"))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Errorf("accountant POST /execute: got %d, want 201", w.Code)
	}
}

// ── QA-5: Tenant isolation di layer HTTP ─────────────────────────────────────

// Tenant B (id=2) memanggil GET /projects/1/allocation/history.
// Mock ListExecutions menyaring berdasarkan tenantID — Tenant B menerima data kosong,
// bukan data Tenant A. Tidak ada leak data antar tenant.
func TestHandler_History_TenantIsolation_NoDataLeak(t *testing.T) {
	// Siapkan dua history record milik Tenant A (tenant_id=1)
	tenantAExecs := []*AllocationExecution{
		{ID: 1, TenantID: 1, ProjectID: 1, Basis: BasisSaleableArea},
		{ID: 2, TenantID: 1, ProjectID: 1, Basis: BasisSalesValue},
	}
	svc := &mockAllocSvc{executions: tenantAExecs}
	r := buildRouter(testSecret, svc)

	// Tenant B (id=2) mencoba membaca /projects/1/allocation/history
	req := httptest.NewRequest(http.MethodGet, "/projects/1/allocation/history", nil)
	req.Header.Set("Authorization", bearerHeader(testSecret, 2, 5, "owner")) // tenant 2
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var body struct {
		Data []AllocationExecution `json:"data"`
	}
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(body.Data) != 0 {
		t.Errorf("tenant B received %d records from tenant A — isolation violation", len(body.Data))
	}
}

// Tenant A melihat hanya miliknya, bukan milik Tenant B.
func TestHandler_History_TenantA_SeesOnlyOwnData(t *testing.T) {
	mixed := []*AllocationExecution{
		{ID: 1, TenantID: 1, ProjectID: 1, Basis: BasisSaleableArea},
		{ID: 2, TenantID: 2, ProjectID: 1, Basis: BasisSalesValue}, // milik tenant lain
	}
	svc := &mockAllocSvc{executions: mixed}
	r := buildRouter(testSecret, svc)

	req := httptest.NewRequest(http.MethodGet, "/projects/1/allocation/history", nil)
	req.Header.Set("Authorization", bearerHeader(testSecret, 1, 1, "owner")) // tenant 1
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var body struct {
		Data []AllocationExecution `json:"data"`
	}
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(body.Data) != 1 {
		t.Errorf("tenant A should see 1 record, got %d", len(body.Data))
	}
	if len(body.Data) > 0 && body.Data[0].TenantID != 0 {
		// TenantID is json:"-" so it won't appear in response — this is correct
	}
}

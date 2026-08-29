package billing

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"esaproperti/internal/platform/auth"
)

// ── Test router setup ─────────────────────────────────────────────────────────

// newTestHandler builds a Handler backed by in-memory mocks.
func newTestHandler() (*Handler, *mockContracts, *mockSchedules, *mockStore) {
	contracts := newMockContracts()
	schedules := newMockSchedules()
	store := newMockStore()
	svc := NewService(contracts, schedules, store, nil)
	h := &Handler{svc: svc, prints: nil}
	return h, contracts, schedules, store
}

func mountTestRouter(h *Handler) *chi.Mux {
	r := chi.NewRouter()
	h.Mount(r)
	return r
}

// withClaims injects auth claims into the request context (bypasses JWT middleware).
func withClaims(req *http.Request, tenantID, userID uint64, role string) *http.Request {
	claims := &auth.Claims{TenantID: tenantID, UserID: userID, Role: role}
	return req.WithContext(auth.WithClaims(req.Context(), claims))
}

func jsonBody(v any) io.Reader {
	b, _ := json.Marshal(v)
	return bytes.NewReader(b)
}

// ── QA 6: Permission guard ────────────────────────────────────────────────────

func TestHandler_Permission_POST_NoAuth_Returns403(t *testing.T) {
	h, _, _, _ := newTestHandler()
	r := mountTestRouter(h)

	// No claims injected — RequireWrite sees no role → 403 Forbidden.
	req := httptest.NewRequest(http.MethodPost, "/sale-contracts/1/invoices", jsonBody(map[string]any{
		"schedule_id": 20,
	}))
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("POST without auth: want 403, got %d", rec.Code)
	}
}

func TestHandler_Permission_POST_Viewer_Returns403(t *testing.T) {
	h, _, _, _ := newTestHandler()
	r := mountTestRouter(h)

	req := httptest.NewRequest(http.MethodPost, "/sale-contracts/1/invoices", jsonBody(map[string]any{
		"schedule_id": 20,
	}))
	req.Header.Set("Content-Type", "application/json")
	req = withClaims(req, tenantA, 1, "viewer")

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("POST as viewer: want 403, got %d", rec.Code)
	}
}

func TestHandler_Permission_POST_Accountant_Allowed(t *testing.T) {
	h, contracts, schedules, _ := newTestHandler()
	r := mountTestRouter(h)

	dueDate := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	contracts.add(tenantA, sampleContract(1))
	schedules.add(tenantA, sampleSchedule(20, 1, "100000000", dueDate, "installment"))

	req := httptest.NewRequest(http.MethodPost, "/sale-contracts/1/invoices", jsonBody(map[string]any{
		"schedule_id": 20,
	}))
	req.Header.Set("Content-Type", "application/json")
	req = withClaims(req, tenantA, 1, "accountant")

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Errorf("POST as accountant: want 201, got %d (body: %s)", rec.Code, rec.Body.String())
	}
}

func TestHandler_Permission_POST_Owner_Allowed(t *testing.T) {
	h, contracts, schedules, _ := newTestHandler()
	r := mountTestRouter(h)

	dueDate := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	contracts.add(tenantA, sampleContract(1))
	schedules.add(tenantA, sampleSchedule(30, 1, "200000000", dueDate, "dp"))

	req := httptest.NewRequest(http.MethodPost, "/sale-contracts/1/invoices", jsonBody(map[string]any{
		"schedule_id": 30,
	}))
	req.Header.Set("Content-Type", "application/json")
	req = withClaims(req, tenantA, 1, "owner")

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Errorf("POST as owner: want 201, got %d (body: %s)", rec.Code, rec.Body.String())
	}
}

func TestHandler_Permission_GET_List_ViewerAllowed(t *testing.T) {
	h, _, _, _ := newTestHandler()
	r := mountTestRouter(h)

	req := httptest.NewRequest(http.MethodGet, "/sale-contracts/1/invoices", nil)
	req = withClaims(req, tenantA, 1, "viewer")

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	// Viewer can list (no RequireWrite on GET) — expect 200, not 403.
	if rec.Code != http.StatusOK {
		t.Errorf("GET list as viewer: want 200, got %d", rec.Code)
	}
}

func TestHandler_Permission_GET_Single_ViewerAllowed(t *testing.T) {
	h, contracts, schedules, _ := newTestHandler()
	r := mountTestRouter(h)

	dueDate := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	contracts.add(tenantA, sampleContract(1))
	schedules.add(tenantA, sampleSchedule(20, 1, "100000000", dueDate, "installment"))

	// Create invoice first as owner.
	postReq := httptest.NewRequest(http.MethodPost, "/sale-contracts/1/invoices", jsonBody(map[string]any{
		"schedule_id": 20,
	}))
	postReq.Header.Set("Content-Type", "application/json")
	postReq = withClaims(postReq, tenantA, 1, "owner")
	postRec := httptest.NewRecorder()
	r.ServeHTTP(postRec, postReq)

	var created Invoice
	json.Unmarshal(postRec.Body.Bytes(), &created)

	// Now get as viewer.
	getReq := httptest.NewRequest(http.MethodGet, "/invoices/1", nil)
	getReq = withClaims(getReq, tenantA, 1, "viewer")
	getRec := httptest.NewRecorder()
	r.ServeHTTP(getRec, getReq)

	if getRec.Code != http.StatusOK {
		t.Errorf("GET single as viewer: want 200, got %d", getRec.Code)
	}
}

func TestHandler_Permission_GET_NoAuth_Returns401(t *testing.T) {
	h, _, _, _ := newTestHandler()
	r := mountTestRouter(h)

	// GET without claims — billingAuth returns 401 (no RequireWrite on GET).
	req := httptest.NewRequest(http.MethodGet, "/sale-contracts/1/invoices", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("GET without auth: want 401, got %d", rec.Code)
	}
}

// ── QA 2: Duplicate → 409 Conflict ───────────────────────────────────────────

func TestHandler_Generate_Duplicate_Returns409(t *testing.T) {
	h, contracts, schedules, _ := newTestHandler()
	r := mountTestRouter(h)

	dueDate := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	contracts.add(tenantA, sampleContract(1))
	schedules.add(tenantA, sampleSchedule(20, 1, "100000000", dueDate, "installment"))

	makeReq := func() *http.Request {
		req := httptest.NewRequest(http.MethodPost, "/sale-contracts/1/invoices", jsonBody(map[string]any{
			"schedule_id": 20,
		}))
		req.Header.Set("Content-Type", "application/json")
		return withClaims(req, tenantA, 1, "owner")
	}

	// First request — must succeed.
	rec1 := httptest.NewRecorder()
	r.ServeHTTP(rec1, makeReq())
	if rec1.Code != http.StatusCreated {
		t.Fatalf("first generate: want 201, got %d", rec1.Code)
	}

	// Second request — must return 409.
	rec2 := httptest.NewRecorder()
	r.ServeHTTP(rec2, makeReq())
	if rec2.Code != http.StatusConflict {
		t.Errorf("duplicate generate: want 409, got %d (body: %s)", rec2.Code, rec2.Body.String())
	}
}

// ── QA 1: Validate response fields ───────────────────────────────────────────

func TestHandler_Generate_ResponseFields_MatchSchedule(t *testing.T) {
	h, contracts, schedules, _ := newTestHandler()
	r := mountTestRouter(h)

	dueDate := time.Date(2026, 11, 30, 0, 0, 0, 0, time.UTC)
	contracts.add(tenantA, sampleContract(1))
	schedules.add(tenantA, sampleSchedule(20, 1, "350000000", dueDate, "dp"))

	req := httptest.NewRequest(http.MethodPost, "/sale-contracts/1/invoices", jsonBody(map[string]any{
		"schedule_id": 20,
		"notes":       "DP termin pertama",
	}))
	req.Header.Set("Content-Type", "application/json")
	req = withClaims(req, tenantA, 1, "owner")

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("want 201, got %d: %s", rec.Code, rec.Body.String())
	}

	var inv Invoice
	if err := json.Unmarshal(rec.Body.Bytes(), &inv); err != nil {
		t.Fatalf("response decode: %v", err)
	}

	if !invoiceNumberRe.MatchString(inv.InvoiceNumber) {
		t.Errorf("InvoiceNumber %q does not match INV/YYYY/NNNNNN", inv.InvoiceNumber)
	}
	if !inv.Amount.Equal(mustMoney("350000000")) {
		t.Errorf("Amount: got %s, want 350000000", inv.Amount.String())
	}
	if inv.InvoiceType != TypeDP {
		t.Errorf("InvoiceType: got %q, want DP", inv.InvoiceType)
	}
	if inv.Status != StatusIssued {
		t.Errorf("Status: got %q, want issued", inv.Status)
	}
	if inv.Notes != "DP termin pertama" {
		t.Errorf("Notes: got %q", inv.Notes)
	}
}

// ── QA 3: Tenant isolation via handler ───────────────────────────────────────

func TestHandler_TenantIsolation_GetInvoice_CrossTenant_404(t *testing.T) {
	h, contracts, schedules, _ := newTestHandler()
	r := mountTestRouter(h)

	dueDate := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	contracts.add(tenantA, sampleContract(1))
	schedules.add(tenantA, sampleSchedule(20, 1, "100000000", dueDate, "installment"))

	// TenantA creates invoice.
	postReq := httptest.NewRequest(http.MethodPost, "/sale-contracts/1/invoices", jsonBody(map[string]any{
		"schedule_id": 20,
	}))
	postReq.Header.Set("Content-Type", "application/json")
	postReq = withClaims(postReq, tenantA, 1, "owner")
	postRec := httptest.NewRecorder()
	r.ServeHTTP(postRec, postReq)

	// TenantB reads the same invoice ID.
	getReq := httptest.NewRequest(http.MethodGet, "/invoices/1", nil)
	getReq = withClaims(getReq, tenantB, 2, "owner")
	getRec := httptest.NewRecorder()
	r.ServeHTTP(getRec, getReq)

	if getRec.Code != http.StatusNotFound {
		t.Errorf("cross-tenant GET: want 404, got %d (body: %s)", getRec.Code, getRec.Body.String())
	}
}

func TestHandler_TenantIsolation_List_TenantBSeesEmptyList(t *testing.T) {
	h, contracts, schedules, _ := newTestHandler()
	r := mountTestRouter(h)

	dueDate := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	contracts.add(tenantA, sampleContract(1))
	schedules.add(tenantA, sampleSchedule(20, 1, "100000000", dueDate, "installment"))

	// TenantA creates invoice for contractID=1.
	postReq := httptest.NewRequest(http.MethodPost, "/sale-contracts/1/invoices", jsonBody(map[string]any{
		"schedule_id": 20,
	}))
	postReq.Header.Set("Content-Type", "application/json")
	postReq = withClaims(postReq, tenantA, 1, "owner")
	r.ServeHTTP(httptest.NewRecorder(), postReq)

	// TenantB lists contractID=1 — must get empty array.
	getReq := httptest.NewRequest(http.MethodGet, "/sale-contracts/1/invoices", nil)
	getReq = withClaims(getReq, tenantB, 2, "viewer")
	getRec := httptest.NewRecorder()
	r.ServeHTTP(getRec, getReq)

	if getRec.Code != http.StatusOK {
		t.Fatalf("TenantB list: want 200, got %d", getRec.Code)
	}
	var list []*Invoice
	json.Unmarshal(getRec.Body.Bytes(), &list)
	if len(list) != 0 {
		t.Errorf("TenantB should see 0 invoices, got %d", len(list))
	}
}

// ── Validation ────────────────────────────────────────────────────────────────

func TestHandler_Generate_MissingScheduleID_Returns400(t *testing.T) {
	h, _, _, _ := newTestHandler()
	r := mountTestRouter(h)

	req := httptest.NewRequest(http.MethodPost, "/sale-contracts/1/invoices", jsonBody(map[string]any{
		"notes": "no schedule_id",
	}))
	req.Header.Set("Content-Type", "application/json")
	req = withClaims(req, tenantA, 1, "owner")

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("missing schedule_id: want 400, got %d", rec.Code)
	}
}

func TestHandler_Generate_InvalidBody_Returns400(t *testing.T) {
	h, _, _, _ := newTestHandler()
	r := mountTestRouter(h)

	req := httptest.NewRequest(http.MethodPost, "/sale-contracts/1/invoices",
		strings.NewReader("not-json"))
	req.Header.Set("Content-Type", "application/json")
	req = withClaims(req, tenantA, 1, "owner")

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("invalid JSON body: want 400, got %d", rec.Code)
	}
}

func TestHandler_Generate_ContractNotFound_Returns404(t *testing.T) {
	h, _, _, _ := newTestHandler()
	r := mountTestRouter(h)

	req := httptest.NewRequest(http.MethodPost, "/sale-contracts/999/invoices", jsonBody(map[string]any{
		"schedule_id": 1,
	}))
	req.Header.Set("Content-Type", "application/json")
	req = withClaims(req, tenantA, 1, "owner")

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("contract not found: want 404, got %d", rec.Code)
	}
}

func TestHandler_GetInvoice_NotFound_Returns404(t *testing.T) {
	h, _, _, _ := newTestHandler()
	r := mountTestRouter(h)

	req := httptest.NewRequest(http.MethodGet, "/invoices/9999", nil)
	req = withClaims(req, tenantA, 1, "viewer")

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("invoice not found: want 404, got %d", rec.Code)
	}
}

// ── Response JSON format ──────────────────────────────────────────────────────

func TestHandler_ListInvoices_ReturnsJSONArray(t *testing.T) {
	h, contracts, schedules, _ := newTestHandler()
	r := mountTestRouter(h)

	dueDate := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	contracts.add(tenantA, sampleContract(1))
	schedules.add(tenantA, sampleSchedule(20, 1, "100000000", dueDate, "installment"))
	schedules.add(tenantA, sampleSchedule(21, 1, "100000000", dueDate, "installment"))

	// Create two invoices.
	for _, sid := range []int{20, 21} {
		req := httptest.NewRequest(http.MethodPost, "/sale-contracts/1/invoices", jsonBody(map[string]any{
			"schedule_id": sid,
		}))
		req.Header.Set("Content-Type", "application/json")
		req = withClaims(req, tenantA, 1, "owner")
		r.ServeHTTP(httptest.NewRecorder(), req)
	}

	// List.
	listReq := httptest.NewRequest(http.MethodGet, "/sale-contracts/1/invoices", nil)
	listReq = withClaims(listReq, tenantA, 1, "viewer")
	listRec := httptest.NewRecorder()
	r.ServeHTTP(listRec, listReq)

	if listRec.Code != http.StatusOK {
		t.Fatalf("list: want 200, got %d", listRec.Code)
	}
	var list []*Invoice
	if err := json.Unmarshal(listRec.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(list) != 2 {
		t.Errorf("expected 2 invoices, got %d", len(list))
	}
	for _, inv := range list {
		if !invoiceNumberRe.MatchString(inv.InvoiceNumber) {
			t.Errorf("InvoiceNumber %q not in expected format", inv.InvoiceNumber)
		}
		if inv.Status != StatusIssued {
			t.Errorf("Status should be issued, got %q", inv.Status)
		}
	}
}

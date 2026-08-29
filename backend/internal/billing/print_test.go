package billing

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"esaproperti/internal/domain"
)

// ── Mock PrintLoader ──────────────────────────────────────────────────────────

type mockPrintLoader struct {
	// [tenantID][invoiceID] → PrintData
	data map[uint64]map[uint64]*PrintData
}

func newMockPrintLoader() *mockPrintLoader {
	return &mockPrintLoader{data: make(map[uint64]map[uint64]*PrintData)}
}

func (m *mockPrintLoader) add(tenantID, invoiceID uint64, d *PrintData) {
	if m.data[tenantID] == nil {
		m.data[tenantID] = make(map[uint64]*PrintData)
	}
	m.data[tenantID][invoiceID] = d
}

func (m *mockPrintLoader) LoadPrintData(_ context.Context, tenantID, invoiceID uint64) (*PrintData, error) {
	if t, ok := m.data[tenantID]; ok {
		if d, ok := t[invoiceID]; ok {
			return d, nil
		}
	}
	return nil, ErrInvoiceNotFound
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func samplePrintData(invoiceNumber string, amount string, status InvoiceStatus) *PrintData {
	m, _ := domain.NewMoney(amount)
	return &PrintData{
		InvoiceNumber: invoiceNumber,
		InvoiceType:   TypeTermin,
		IssueDate:     time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
		DueDate:       time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC),
		Amount:        m,
		Status:        status,
		Notes:         "Cicilan ke-3",
		CompanyName:   "PT Esa Properti Indonesia",
		BuyerName:     "Budi Santoso",
		BuyerID:       "KTP-3275001234",
		PaymentType:   "tunai",
		ProjectName:   "The Lithos Residence",
		UnitCode:      "LITHOS-A01",
		UnitType:      "Villa",
		UnitArea:      "120.0000",
	}
}

// newTestHandlerWithPrint creates a Handler with both invoice and print mocks.
func newTestHandlerWithPrint() (*Handler, *mockContracts, *mockSchedules, *mockStore, *mockPrintLoader) {
	contracts := newMockContracts()
	schedules := newMockSchedules()
	store := newMockStore()
	pl := newMockPrintLoader()
	svc := NewService(contracts, schedules, store, pl)
	h := &Handler{svc: svc, prints: pl}
	return h, contracts, schedules, store, pl
}

// ── QA 1: Generate PDF invoice valid + nominal match ─────────────────────────

func TestPrint_RenderInvoicePrint_ValidHTML(t *testing.T) {
	data := samplePrintData("INV/2026/000001", "350000000", StatusIssued)
	var buf bytes.Buffer
	if err := RenderInvoicePrint(&buf, data); err != nil {
		t.Fatalf("RenderInvoicePrint error: %v", err)
	}
	html := buf.String()

	if !strings.Contains(html, "<!DOCTYPE html>") {
		t.Error("output must be valid HTML")
	}
	if !strings.Contains(html, "A4") || !strings.Contains(html, "297mm") {
		t.Error("template must include A4 page dimensions")
	}
}

func TestPrint_Amount_MatchesDatabase(t *testing.T) {
	data := samplePrintData("INV/2026/000001", "350000000", StatusIssued)
	var buf bytes.Buffer
	RenderInvoicePrint(&buf, data) //nolint
	html := buf.String()

	// 350.000.000 formatted as Rupiah with Indonesian period separators.
	if !strings.Contains(html, "350.000.000") {
		t.Errorf("HTML must contain formatted amount '350.000.000', got:\n%s", html[:min(500, len(html))])
	}
	if !strings.Contains(html, "Rp") {
		t.Error("HTML must contain Rp prefix")
	}
}

func TestPrint_InvoiceNumber_MatchesDatabase(t *testing.T) {
	data := samplePrintData("INV/2026/000042", "100000000", StatusIssued)
	var buf bytes.Buffer
	RenderInvoicePrint(&buf, data) //nolint
	html := buf.String()

	if !strings.Contains(html, "INV/2026/000042") {
		t.Errorf("HTML must contain invoice number 'INV/2026/000042'")
	}
}

func TestPrint_CustomerInfo_Present(t *testing.T) {
	data := samplePrintData("INV/2026/000001", "200000000", StatusIssued)
	var buf bytes.Buffer
	RenderInvoicePrint(&buf, data) //nolint
	html := buf.String()

	if !strings.Contains(html, "Budi Santoso") {
		t.Error("HTML must contain buyer name")
	}
	if !strings.Contains(html, "KTP-3275001234") {
		t.Error("HTML must contain buyer ID")
	}
}

func TestPrint_PropertyInfo_Present(t *testing.T) {
	data := samplePrintData("INV/2026/000001", "200000000", StatusIssued)
	var buf bytes.Buffer
	RenderInvoicePrint(&buf, data) //nolint
	html := buf.String()

	if !strings.Contains(html, "The Lithos Residence") {
		t.Error("HTML must contain project name")
	}
	if !strings.Contains(html, "LITHOS-A01") {
		t.Error("HTML must contain unit code")
	}
	if !strings.Contains(html, "Villa") {
		t.Error("HTML must contain unit type")
	}
}

func TestPrint_CompanyInfo_Present(t *testing.T) {
	data := samplePrintData("INV/2026/000001", "200000000", StatusIssued)
	var buf bytes.Buffer
	RenderInvoicePrint(&buf, data) //nolint
	html := buf.String()

	if !strings.Contains(html, "PT Esa Properti Indonesia") {
		t.Error("HTML must contain company name")
	}
}

// Footer invoice mengidentifikasi DOKUMENnya (nomor + tanggal cetak), bukan
// perangkat lunak yang mencetaknya. Lembar ini pergi ke tangan customer;
// nama internal produk di sana tidak berarti apa-apa baginya dan bukan merek
// yang ia kenal. Uji ini menjaga dua-duanya: nomor tetap ada, nama produk
// tidak muncul lagi di mana pun dalam dokumen.
func TestPrint_Footer_IdentifiesDocumentNotSoftware(t *testing.T) {
	data := samplePrintData("INV/2026/000001", "100000000", StatusIssued)
	var buf bytes.Buffer
	RenderInvoicePrint(&buf, data) //nolint
	html := buf.String()

	if !strings.Contains(html, "INV/2026/000001") {
		t.Error("footer harus memuat nomor invoice")
	}
	if !strings.Contains(html, "Dicetak:") {
		t.Error("footer harus memuat tanggal cetak")
	}
	if strings.Contains(html, "esaProperti") {
		t.Error("nama internal produk tidak boleh tercetak di dokumen customer")
	}
}

func TestPrint_DueDate_MatchesDatabase(t *testing.T) {
	data := samplePrintData("INV/2026/000001", "100000000", StatusIssued)
	var buf bytes.Buffer
	RenderInvoicePrint(&buf, data) //nolint
	html := buf.String()

	// DueDate = 30 September 2026 → should render in Indonesian
	if !strings.Contains(html, "30 September 2026") {
		t.Errorf("HTML must contain due date '30 September 2026'")
	}
}

// ── QA 5: Cancelled invoice still viewable ────────────────────────────────────

func TestPrint_CancelledInvoice_StillRendered(t *testing.T) {
	data := samplePrintData("INV/2026/000007", "500000000", StatusCancelled)
	var buf bytes.Buffer
	if err := RenderInvoicePrint(&buf, data); err != nil {
		t.Fatalf("RenderInvoicePrint on cancelled invoice: %v", err)
	}
	html := buf.String()

	if !strings.Contains(html, "INV/2026/000007") {
		t.Error("Cancelled invoice must still show invoice number")
	}
	// Status is displayed but cannot be "edited" — rendering itself proves read-only access.
	if !strings.Contains(html, "Dibatalkan") || !strings.Contains(html, "cancelled") {
		t.Error("Cancelled invoice must display cancelled status")
	}
}

// ── QA 4: Tenant isolation via print endpoint ─────────────────────────────────

func TestPrint_Endpoint_TenantIsolation_CrossTenant_404(t *testing.T) {
	h, _, _, _, pl := newTestHandlerWithPrint()
	r := mountTestRouter(h)

	// Invoice 99 belongs to tenantA.
	pl.add(tenantA, 99, samplePrintData("INV/2026/000099", "100000000", StatusIssued))

	// TenantB requests print for invoiceID=99.
	req := httptest.NewRequest(http.MethodGet, "/invoices/99/print", nil)
	req = withClaims(req, tenantB, 2, "viewer")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("cross-tenant print: want 404, got %d", rec.Code)
	}
}

func TestPrint_Endpoint_TenantA_CanAccess_OwnInvoice(t *testing.T) {
	h, _, _, _, pl := newTestHandlerWithPrint()
	r := mountTestRouter(h)

	pl.add(tenantA, 99, samplePrintData("INV/2026/000099", "100000000", StatusIssued))

	req := httptest.NewRequest(http.MethodGet, "/invoices/99/print", nil)
	req = withClaims(req, tenantA, 1, "viewer")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("own invoice print: want 200, got %d", rec.Code)
	}
	ct := rec.Header().Get("Content-Type")
	if !strings.HasPrefix(ct, "text/html") {
		t.Errorf("Content-Type: want text/html, got %q", ct)
	}
}

// ── QA 6: Permission — viewer can download, no write required ─────────────────

func TestPrint_Endpoint_ViewerAllowed(t *testing.T) {
	h, _, _, _, pl := newTestHandlerWithPrint()
	r := mountTestRouter(h)

	pl.add(tenantA, 1, samplePrintData("INV/2026/000001", "250000000", StatusIssued))

	req := httptest.NewRequest(http.MethodGet, "/invoices/1/print", nil)
	req = withClaims(req, tenantA, 99, "viewer")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("viewer download: want 200, got %d (body: %s)", rec.Code, rec.Body.String())
	}
}

func TestPrint_Endpoint_AccountantAllowed(t *testing.T) {
	h, _, _, _, pl := newTestHandlerWithPrint()
	r := mountTestRouter(h)

	pl.add(tenantA, 1, samplePrintData("INV/2026/000001", "250000000", StatusIssued))

	req := httptest.NewRequest(http.MethodGet, "/invoices/1/print", nil)
	req = withClaims(req, tenantA, 1, "accountant")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("accountant print: want 200, got %d", rec.Code)
	}
}

func TestPrint_Endpoint_NoAuth_Returns401(t *testing.T) {
	h, _, _, _, _ := newTestHandlerWithPrint()
	r := mountTestRouter(h)

	// No claims — billingAuth returns 401.
	req := httptest.NewRequest(http.MethodGet, "/invoices/1/print", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("no auth print: want 401, got %d", rec.Code)
	}
}

// ── QA 7: No journal created ──────────────────────────────────────────────────

func TestPrint_Endpoint_NoJournalCreated(t *testing.T) {
	// The print handler only calls GetPrintData (read-only).
	// mockPrintLoader has no journal-related methods.
	// Success proves no journal side-effect.
	h, _, _, store, pl := newTestHandlerWithPrint()
	r := mountTestRouter(h)

	pl.add(tenantA, 1, samplePrintData("INV/2026/000001", "100000000", StatusIssued))

	req := httptest.NewRequest(http.MethodGet, "/invoices/1/print", nil)
	req = withClaims(req, tenantA, 1, "viewer")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("print: want 200, got %d", rec.Code)
	}
	// Invoice store untouched (no new invoices, no status updates).
	if len(store.invoices) != 0 {
		t.Errorf("print must not write to invoice store, got %d entries", len(store.invoices))
	}
}

// ── QA: Print endpoint 404 for missing invoice ───────────────────────────────

func TestPrint_Endpoint_NotFound_Returns404(t *testing.T) {
	h, _, _, _, _ := newTestHandlerWithPrint()
	r := mountTestRouter(h)

	req := httptest.NewRequest(http.MethodGet, "/invoices/9999/print", nil)
	req = withClaims(req, tenantA, 1, "viewer")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("missing invoice print: want 404, got %d", rec.Code)
	}
}

// ── Rupiah formatting ─────────────────────────────────────────────────────────

func TestFormatRupiah_Thousands(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"1000000", "Rp 1.000.000"},
		{"500000000", "Rp 500.000.000"},
		{"1500000000", "Rp 1.500.000.000"},
		{"0", "Rp 0"},
		{"1000", "Rp 1.000"},
	}
	for _, tc := range cases {
		m, _ := domain.NewMoney(tc.input)
		got := formatRupiah(m)
		if got != tc.want {
			t.Errorf("formatRupiah(%q): got %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestFormatDate_Indonesian(t *testing.T) {
	cases := []struct {
		date time.Time
		want string
	}{
		{time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), "1 Januari 2026"},
		{time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC), "30 September 2026"},
		{time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC), "31 Desember 2026"},
	}
	for _, tc := range cases {
		got := formatDate(tc.date)
		if got != tc.want {
			t.Errorf("formatDate(%v): got %q, want %q", tc.date, got, tc.want)
		}
	}
}

// ── Cancelled invoice: cannot be edited (service-level guard) ────────────────

func TestPrint_CancelledInvoice_CannotBeUpdated(t *testing.T) {
	// Prove the service rejects status updates on cancelled invoices via MarkPaid.
	_, _, _, store, _ := newTestHandlerWithPrint()

	// Manually seed a cancelled invoice.
	schedID := uint64(20)
	store.invoices[1] = &Invoice{
		ID:       1,
		TenantID: tenantA,
		Status:   StatusCancelled,
		ScheduleID: &schedID,
	}
	store.bySchedule[20] = store.invoices[1]

	svc := NewService(newMockContracts(), newMockSchedules(), store, nil)
	err := svc.MarkPaidByScheduleID(context.Background(), tenantA, 20)
	if err != nil {
		t.Fatalf("MarkPaid on cancelled should be no-op, got: %v", err)
	}
	if store.invoices[1].Status != StatusCancelled {
		t.Errorf("Cancelled invoice status mutated: got %q", store.invoices[1].Status)
	}
}

// ── ErrInvoiceNotFound wrapping in GetPrintData ───────────────────────────────

func TestGetPrintData_NilPrintLoader_ReturnsNotFound(t *testing.T) {
	svc := NewService(newMockContracts(), newMockSchedules(), newMockStore(), nil)
	_, err := svc.GetPrintData(context.Background(), tenantA, 1)
	if !errors.Is(err, ErrInvoiceNotFound) {
		t.Errorf("nil prints: want ErrInvoiceNotFound, got %v", err)
	}
}

// ── helper ────────────────────────────────────────────────────────────────────

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

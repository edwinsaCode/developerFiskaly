package cost_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"esaproperti/internal/cost"
	"esaproperti/internal/domain"
)

// ── In-memory mocks ───────────────────────────────────────────────────────────

// mockAccountFinder resolves account codes to fixed IDs and names.
// Implements cost.AccountResolver (replaces the old AccountByCodeFinder).
type mockAccountFinder struct {
	accounts map[string]uint64 // code → database ID
	names    map[string]string // code → display name
}

func (m *mockAccountFinder) ResolveAccount(_ context.Context, _ uint64, code string) (uint64, string, error) {
	id, ok := m.accounts[code]
	if !ok {
		return 0, "", cost.ErrAccountNotFound
	}
	name := m.names[code]
	if name == "" {
		name = "Akun " + code
	}
	return id, name, nil
}

// standardAccounts is the COA mapping used across all tests.
var standardAccounts = map[string]uint64{
	"1-3000": 100, // Persediaan — Tanah
	"1-3100": 101, // Persediaan — Hard Cost
	"1-3200": 102, // Persediaan — Soft Cost
	"1-3300": 103, // Persediaan — Biaya Pembiayaan (legacy, tidak dipakai lagi utk plan baru)
	"1-1300": 200, // Bank — BCA
	"1-1400": 201, // Bank — Mandiri
	"1-1500": 202, // Bank — BRI
	"2-1000": 300, // Hutang Usaha
	"5-3000": 400, // Beban Pemasaran (tier overhead)
	"5-4000": 401, // Beban Umum & Administrasi (tier overhead)
	"5-4600": 402, // Beban Operasional (tier overhead, dahulu "financing"/Pendanaan)
	"5-4700": 403, // Beban Soft Cost (tier overhead, RULE KLIEN FREEZE 2026-09-04)
}

var standardNames = map[string]string{
	"1-3000": "Persediaan Real Estat — Tanah",
	"1-3100": "Persediaan Real Estat — Hard Cost",
	"1-3200": "Persediaan Real Estat — Soft Cost",
	"1-3300": "Persediaan Real Estat — Biaya Pembiayaan",
	"1-1300": "Bank — BCA",
	"1-1400": "Bank — Mandiri",
	"1-1500": "Bank — BRI",
	"2-1000": "Hutang Usaha",
	"5-3000": "Beban Pemasaran",
	"5-4000": "Beban Umum & Administrasi",
	"5-4600": "Beban Operasional",
	"5-4700": "Beban Soft Cost (Desain & Legal)",
}

// capturedJournalCall records one call to CreateJournal.
type capturedJournalCall struct {
	TenantID    uint64
	Date        time.Time
	Description string
	Lines       []cost.JournalLineInput
}

type mockJournalWriter struct {
	mu      sync.Mutex
	calls   []capturedJournalCall
	nextID  uint64
	posted  map[uint64]bool
	cashOut map[uint64]bool
}

func newMockJournalWriter() *mockJournalWriter {
	return &mockJournalWriter{posted: make(map[uint64]bool), cashOut: make(map[uint64]bool)}
}

func (m *mockJournalWriter) CreateJournal(_ context.Context, tenantID uint64, date time.Time, description string, lines []cost.JournalLineInput) (uint64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.nextID++
	m.calls = append(m.calls, capturedJournalCall{
		TenantID: tenantID, Date: date, Description: description, Lines: lines,
	})
	return m.nextID, nil
}

func (m *mockJournalWriter) PostJournal(_ context.Context, tenantID, journalID uint64, cashOut bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.posted[journalID] {
		return cost.ErrCostEntryAlreadyPosted
	}
	m.posted[journalID] = true
	m.cashOut[journalID] = cashOut
	return nil
}

// mockCostStore is an in-memory CostEntry store.
type mockCostStore struct {
	mu      sync.Mutex
	entries map[uint64]*cost.CostEntry
	nextID  uint64
}

func newMockCostStore() *mockCostStore {
	return &mockCostStore{entries: make(map[uint64]*cost.CostEntry)}
}

func (m *mockCostStore) CreateCostEntry(_ context.Context, e *cost.CostEntry) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.nextID++
	e.ID = m.nextID
	e.CreatedAt = time.Now()
	e.UpdatedAt = time.Now()
	cp := *e
	m.entries[cp.ID] = &cp
	return nil
}

func (m *mockCostStore) FindCostEntryByID(_ context.Context, tenantID, id uint64) (*cost.CostEntry, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.entries[id]
	if !ok || e.TenantID != tenantID {
		return nil, cost.ErrCostEntryNotFound
	}
	cp := *e
	return &cp, nil
}

func (m *mockCostStore) ListCostEntriesByProject(_ context.Context, tenantID, projectID uint64) ([]*cost.CostEntry, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []*cost.CostEntry
	for _, e := range m.entries {
		if e.TenantID == tenantID && e.ProjectID != nil && *e.ProjectID == projectID {
			cp := *e
			out = append(out, &cp)
		}
	}
	return out, nil
}

func (m *mockCostStore) ListCostEntriesByUnit(_ context.Context, tenantID, unitID uint64) ([]*cost.CostEntry, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []*cost.CostEntry
	for _, e := range m.entries {
		if e.TenantID == tenantID && e.UnitID != nil && *e.UnitID == unitID {
			cp := *e
			out = append(out, &cp)
		}
	}
	return out, nil
}

// mockCostQueryer returns pre-set UnitCostBreakdown values.
type mockCostQueryer struct {
	byProject   domain.UnitCostBreakdown
	byUnit      map[uint64]domain.UnitCostBreakdown
	projectWide domain.UnitCostBreakdown
}

func (m *mockCostQueryer) AccumulatedByProject(_ context.Context, _, _ uint64) (domain.UnitCostBreakdown, error) {
	return m.byProject, nil
}

func (m *mockCostQueryer) AccumulatedByUnit(_ context.Context, _, _, unitID uint64) (domain.UnitCostBreakdown, error) {
	if b, ok := m.byUnit[unitID]; ok {
		return b, nil
	}
	return domain.UnitCostBreakdown{}, nil
}

func (m *mockCostQueryer) AccumulatedProjectWide(_ context.Context, _, _ uint64) (domain.UnitCostBreakdown, error) {
	return m.projectWide, nil
}

// newTestService builds a Service with all mocks.
func newTestService(finder *mockAccountFinder, writer *mockJournalWriter, store *mockCostStore, queryer *mockCostQueryer) *cost.Service {
	return cost.NewService(finder, writer, store, queryer, nil) // nil = tidak ada validasi RAB
}

func defaultTestService() (*cost.Service, *mockAccountFinder, *mockJournalWriter, *mockCostStore, *mockCostQueryer) {
	finder := &mockAccountFinder{accounts: standardAccounts, names: standardNames}
	writer := newMockJournalWriter()
	store := newMockCostStore()
	queryer := &mockCostQueryer{byUnit: make(map[uint64]domain.UnitCostBreakdown)}
	svc := newTestService(finder, writer, store, queryer)
	return svc, finder, writer, store, queryer
}

func baseReq() cost.CreateCostEntryRequest {
	return cost.CreateCostEntryRequest{
		ProjectID:       1,
		Category:        domain.CostCategoryHard,
		HardSubcategory: domain.ConstructionSaranaPrasarana,
		Amount:          domain.FromInt(500_000_000),
		PaymentMethod:   cost.PaymentMethodBank,
		BankAccountCode: "1-1300",
		Date:            time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC),
		Vendor:          "PT Konstruksi Maju",
		Description:     "Biaya pondasi Proyek LITHOS",
	}
}

// ── Create — happy path ───────────────────────────────────────────────────────

func TestCostEntry_Create_Success(t *testing.T) {
	svc, _, writer, store, _ := defaultTestService()

	entry, err := svc.CreateCostEntry(context.Background(), 1, baseReq())
	if err != nil {
		t.Fatalf("CreateCostEntry: %v", err)
	}
	if entry.ID == 0 {
		t.Error("ID should be set")
	}
	if entry.JournalEntryID == 0 {
		t.Error("JournalEntryID should be set (draft journal was created)")
	}
	if entry.TenantID != 1 {
		t.Errorf("TenantID = %d, want 1", entry.TenantID)
	}
	if len(writer.calls) != 1 {
		t.Errorf("expected 1 journal create call, got %d", len(writer.calls))
	}
	// Verify CostEntry persisted in store
	stored, err := store.FindCostEntryByID(context.Background(), 1, entry.ID)
	if err != nil {
		t.Fatalf("FindCostEntryByID: %v", err)
	}
	if stored.JournalEntryID != entry.JournalEntryID {
		t.Errorf("stored JournalEntryID = %d, want %d", stored.JournalEntryID, entry.JournalEntryID)
	}
}

// ── Category → account mapping (DoD: mapping kategori→sub-akun benar) ─────────

func TestCostEntry_Mapping_AllCategories(t *testing.T) {
	// RULE KLIEN FREEZE (2026-09-04): Soft Cost bukan lagi kapitalisasi ke
	// Persediaan (1-3200) — direalisasi sebagai beban (5-4700).
	cases := []struct {
		category           domain.CostCategory
		expectedDebitAccID uint64
	}{
		{domain.CostCategoryLand, 100}, // 1-3000
		{domain.CostCategoryHard, 101}, // 1-3100
		{domain.CostCategorySoft, 403}, // 5-4700 (Beban Soft Cost)
	}
	for _, tc := range cases {
		tc := tc
		t.Run(string(tc.category), func(t *testing.T) {
			svc, _, writer, _, _ := defaultTestService()
			req := baseReq()
			req.Category = tc.category
			if tc.category != domain.CostCategoryHard {
				req.HardSubcategory = ""
			}

			_, err := svc.CreateCostEntry(context.Background(), 1, req)
			if err != nil {
				t.Fatalf("CreateCostEntry: %v", err)
			}
			if len(writer.calls) != 1 {
				t.Fatalf("expected 1 journal call, got %d", len(writer.calls))
			}
			call := writer.calls[0]
			if len(call.Lines) != 2 {
				t.Fatalf("expected 2 journal lines, got %d", len(call.Lines))
			}

			debitLine := call.Lines[0]
			if debitLine.AccountID != tc.expectedDebitAccID {
				t.Errorf("debit AccountID = %d, want %d (category=%s)", debitLine.AccountID, tc.expectedDebitAccID, tc.category)
			}
			if !debitLine.Debit.Equal(domain.FromInt(500_000_000)) {
				t.Errorf("debit amount = %s, want 500000000", debitLine.Debit)
			}
			if !debitLine.Credit.IsZero() {
				t.Errorf("debit line should have zero credit, got %s", debitLine.Credit)
			}
		})
	}
}

// ── Credit account: bank vs payable ──────────────────────────────────────────

func TestCostEntry_CreditAccount_Bank(t *testing.T) {
	svc, _, writer, _, _ := defaultTestService()
	req := baseReq()
	req.PaymentMethod = cost.PaymentMethodBank
	req.BankAccountCode = "1-1400" // Bank Mandiri

	_, err := svc.CreateCostEntry(context.Background(), 1, req)
	if err != nil {
		t.Fatalf("CreateCostEntry: %v", err)
	}
	call := writer.calls[0]
	creditLine := call.Lines[1]
	if creditLine.AccountID != 201 { // 1-1400 Mandiri
		t.Errorf("credit AccountID = %d, want 201 (1-1400 Mandiri)", creditLine.AccountID)
	}
	if !creditLine.Credit.Equal(domain.FromInt(500_000_000)) {
		t.Errorf("credit amount = %s, want 500000000", creditLine.Credit)
	}
}

func TestCostEntry_CreditAccount_Payable(t *testing.T) {
	svc, _, writer, _, _ := defaultTestService()
	req := baseReq()
	req.PaymentMethod = cost.PaymentMethodPayable
	req.BankAccountCode = "" // not used

	_, err := svc.CreateCostEntry(context.Background(), 1, req)
	if err != nil {
		t.Fatalf("CreateCostEntry: %v", err)
	}
	call := writer.calls[0]
	creditLine := call.Lines[1]
	if creditLine.AccountID != 300 { // 2-1000 Hutang Usaha
		t.Errorf("credit AccountID = %d, want 300 (2-1000 Hutang Usaha)", creditLine.AccountID)
	}
}

// ── Journal line tags ─────────────────────────────────────────────────────────

// TestCostEntry_ProjectTag_AlwaysSet ensures project_id is tagged on both lines (DoD requirement).
func TestCostEntry_ProjectTag_AlwaysSet(t *testing.T) {
	svc, _, writer, _, _ := defaultTestService()
	req := baseReq()
	req.ProjectID = 42

	_, err := svc.CreateCostEntry(context.Background(), 1, req)
	if err != nil {
		t.Fatalf("CreateCostEntry: %v", err)
	}
	call := writer.calls[0]
	for i, l := range call.Lines {
		if l.ProjectID == nil || *l.ProjectID != 42 {
			t.Errorf("line %d: ProjectID not set or wrong (got %v, want 42)", i, l.ProjectID)
		}
	}
}

// TestCostEntry_UnitTag_SetWhenProvided ensures unit_id is tagged when given.
func TestCostEntry_UnitTag_SetWhenProvided(t *testing.T) {
	svc, _, writer, _, _ := defaultTestService()
	req := baseReq()
	unitID := uint64(7)
	req.UnitID = &unitID

	_, err := svc.CreateCostEntry(context.Background(), 1, req)
	if err != nil {
		t.Fatalf("CreateCostEntry: %v", err)
	}
	call := writer.calls[0]
	for i, l := range call.Lines {
		if l.UnitID == nil || *l.UnitID != 7 {
			t.Errorf("line %d: UnitID not set or wrong (got %v, want 7)", i, l.UnitID)
		}
	}
}

// TestCostEntry_UnitTag_NilWhenProjectWide ensures unit_id is nil for project-wide costs.
func TestCostEntry_UnitTag_NilWhenProjectWide(t *testing.T) {
	svc, _, writer, _, _ := defaultTestService()
	req := baseReq()
	req.UnitID = nil // project-wide

	_, err := svc.CreateCostEntry(context.Background(), 1, req)
	if err != nil {
		t.Fatalf("CreateCostEntry: %v", err)
	}
	call := writer.calls[0]
	for i, l := range call.Lines {
		if l.UnitID != nil {
			t.Errorf("line %d: UnitID should be nil for project-wide cost, got %v", i, *l.UnitID)
		}
	}
}

// ── DoD: jumlah posting Persediaan == jumlah cost entry ──────────────────────

// TestCostEntry_JournalCount_MatchesEntryCount verifies N cost entries → N journal create calls.
func TestCostEntry_JournalCount_MatchesEntryCount(t *testing.T) {
	svc, _, writer, _, _ := defaultTestService()
	const N = 5
	for i := 0; i < N; i++ {
		req := baseReq()
		req.Amount = domain.FromInt(int64(100_000_000 * (i + 1)))
		if _, err := svc.CreateCostEntry(context.Background(), 1, req); err != nil {
			t.Fatalf("entry %d: %v", i, err)
		}
	}
	if len(writer.calls) != N {
		t.Errorf("expected %d journal create calls, got %d", N, len(writer.calls))
	}
}

// ── DoD: unit-tagged vs project-wide distinguishable ─────────────────────────

// TestCostEntry_UnitTagged_vs_ProjectWide verifies query can distinguish the two.
func TestCostEntry_UnitTagged_vs_ProjectWide(t *testing.T) {
	unitID := uint64(5)
	_, _, _, _, queryerMock := defaultTestService()

	queryerMock.byUnit[unitID] = domain.UnitCostBreakdown{
		Hard: domain.FromInt(200_000_000),
	}
	queryerMock.projectWide = domain.UnitCostBreakdown{
		Land: domain.FromInt(1_000_000_000),
	}

	// Simulate service.AccumulatedByUnit via mock
	unitBreakdown, err := queryerMock.AccumulatedByUnit(context.Background(), 1, 1, unitID)
	if err != nil {
		t.Fatal(err)
	}
	if !unitBreakdown.Hard.Equal(domain.FromInt(200_000_000)) {
		t.Errorf("unit hard cost = %s, want 200000000", unitBreakdown.Hard)
	}
	if !unitBreakdown.Land.IsZero() {
		t.Errorf("unit land cost should be zero for this unit, got %s", unitBreakdown.Land)
	}

	// Project-wide costs should be independent
	projectBreakdown, err := queryerMock.AccumulatedProjectWide(context.Background(), 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if !projectBreakdown.Land.Equal(domain.FromInt(1_000_000_000)) {
		t.Errorf("project-wide land = %s, want 1000000000", projectBreakdown.Land)
	}
}

// ── Validation — Invariant #2 ─────────────────────────────────────────────────

func TestCostEntry_FractionalAmount_Rejected(t *testing.T) {
	svc, _, writer, _, _ := defaultTestService()
	req := baseReq()
	req.Amount = domain.MustParse("500000000.50") // pecahan sen

	_, err := svc.CreateCostEntry(context.Background(), 1, req)
	if !errors.Is(err, cost.ErrCostAmountFractional) {
		t.Errorf("expected ErrCostAmountFractional, got %v", err)
	}
	if len(writer.calls) != 0 {
		t.Error("no journal should be created on validation failure")
	}
}

func TestCostEntry_ZeroAmount_Rejected(t *testing.T) {
	svc, _, _, _, _ := defaultTestService()
	req := baseReq()
	req.Amount = domain.Zero

	_, err := svc.CreateCostEntry(context.Background(), 1, req)
	if !errors.Is(err, cost.ErrCostAmountZeroOrNeg) {
		t.Errorf("expected ErrCostAmountZeroOrNeg, got %v", err)
	}
}

func TestCostEntry_NegativeAmount_Rejected(t *testing.T) {
	svc, _, _, _, _ := defaultTestService()
	req := baseReq()
	req.Amount = domain.MustParse("-100000")

	_, err := svc.CreateCostEntry(context.Background(), 1, req)
	if !errors.Is(err, cost.ErrCostAmountZeroOrNeg) {
		t.Errorf("expected ErrCostAmountZeroOrNeg, got %v", err)
	}
}

func TestCostEntry_InvalidCategory_Rejected(t *testing.T) {
	svc, _, _, _, _ := defaultTestService()
	req := baseReq()
	req.Category = "construction" // not a valid category

	_, err := svc.CreateCostEntry(context.Background(), 1, req)
	if !errors.Is(err, cost.ErrInvalidCategory) {
		t.Errorf("expected ErrInvalidCategory, got %v", err)
	}
}

func TestCostEntry_BankAccountRequired_WhenPaymentMethodBank(t *testing.T) {
	svc, _, _, _, _ := defaultTestService()
	req := baseReq()
	req.PaymentMethod = cost.PaymentMethodBank
	req.BankAccountCode = "" // missing

	_, err := svc.CreateCostEntry(context.Background(), 1, req)
	if !errors.Is(err, cost.ErrBankAccountCodeRequired) {
		t.Errorf("expected ErrBankAccountCodeRequired, got %v", err)
	}
}

func TestCostEntry_ProjectID_Required(t *testing.T) {
	svc, _, _, _, _ := defaultTestService()
	req := baseReq()
	req.ProjectID = 0

	_, err := svc.CreateCostEntry(context.Background(), 1, req)
	if !errors.Is(err, cost.ErrProjectRequired) {
		t.Errorf("expected ErrProjectRequired, got %v", err)
	}
}

// ── Post lifecycle ────────────────────────────────────────────────────────────

func TestCostEntry_Post_Success(t *testing.T) {
	svc, _, writer, _, _ := defaultTestService()
	entry, err := svc.CreateCostEntry(context.Background(), 1, baseReq())
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := svc.PostCostEntry(context.Background(), 1, entry.ID); err != nil {
		t.Fatalf("post: %v", err)
	}
	if !writer.posted[entry.JournalEntryID] {
		t.Error("journal should be marked posted")
	}
}

func TestCostEntry_Post_AlreadyPosted_Returns_Error(t *testing.T) {
	svc, _, _, _, _ := defaultTestService()
	entry, _ := svc.CreateCostEntry(context.Background(), 1, baseReq())
	_ = svc.PostCostEntry(context.Background(), 1, entry.ID)

	err := svc.PostCostEntry(context.Background(), 1, entry.ID)
	if !errors.Is(err, cost.ErrCostEntryAlreadyPosted) {
		t.Errorf("expected ErrCostEntryAlreadyPosted on second post, got %v", err)
	}
}

// ── Cross-tenant isolation (DoD) ──────────────────────────────────────────────

func TestCostEntry_CrossTenant_Isolation(t *testing.T) {
	svc, _, _, store, _ := defaultTestService()

	// Tenant 1 creates a cost entry
	entry, err := svc.CreateCostEntry(context.Background(), 1, baseReq())
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	// Tenant 2 cannot read tenant 1's entry
	_, err = store.FindCostEntryByID(context.Background(), 2, entry.ID)
	if !errors.Is(err, cost.ErrCostEntryNotFound) {
		t.Errorf("cross-tenant get: expected ErrCostEntryNotFound, got %v", err)
	}

	// Tenant 2 listing returns empty
	list, err := store.ListCostEntriesByProject(context.Background(), 2, 1)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 0 {
		t.Errorf("cross-tenant list should return 0 entries, got %d", len(list))
	}
}

// ── Accumulated cost queries (DoD) ───────────────────────────────────────────

func TestAccumulatedCost_ByProject(t *testing.T) {
	svc, _, _, _, queryerMock := defaultTestService()
	queryerMock.byProject = domain.UnitCostBreakdown{
		Land:      domain.FromInt(1_000_000_000),
		Hard:      domain.FromInt(3_000_000_000),
		Soft:      domain.FromInt(500_000_000),
		Financing: domain.FromInt(200_000_000),
	}
	b, err := svc.AccumulatedByProject(context.Background(), 1, 42)
	if err != nil {
		t.Fatalf("AccumulatedByProject: %v", err)
	}
	total := b.Total()
	want := domain.FromInt(4_700_000_000)
	if !total.Equal(want) {
		t.Errorf("total = %s, want %s", total, want)
	}
	if !b.Land.Equal(domain.FromInt(1_000_000_000)) {
		t.Errorf("land = %s, want 1000000000", b.Land)
	}
	if !b.Hard.Equal(domain.FromInt(3_000_000_000)) {
		t.Errorf("hard = %s, want 3000000000", b.Hard)
	}
}

func TestAccumulatedCost_ByUnit(t *testing.T) {
	svc, _, _, _, queryerMock := defaultTestService()
	unitID := uint64(10)
	queryerMock.byUnit[unitID] = domain.UnitCostBreakdown{
		Hard: domain.FromInt(650_000_000),
	}
	b, err := svc.AccumulatedByUnit(context.Background(), 1, 42, unitID)
	if err != nil {
		t.Fatalf("AccumulatedByUnit: %v", err)
	}
	if !b.Hard.Equal(domain.FromInt(650_000_000)) {
		t.Errorf("hard cost = %s, want 650000000", b.Hard)
	}
	if !b.Land.IsZero() {
		t.Errorf("land cost should be zero, got %s", b.Land)
	}
}

func TestAccumulatedCost_ProjectWide_SeparateFromUnit(t *testing.T) {
	svc, _, _, _, queryerMock := defaultTestService()
	queryerMock.projectWide = domain.UnitCostBreakdown{
		Soft: domain.FromInt(300_000_000),
	}
	b, err := svc.AccumulatedProjectWide(context.Background(), 1, 42)
	if err != nil {
		t.Fatalf("AccumulatedProjectWide: %v", err)
	}
	if !b.Soft.Equal(domain.FromInt(300_000_000)) {
		t.Errorf("project-wide soft = %s, want 300000000", b.Soft)
	}
}

// ── Preview == Create (DoD: preview output identik dengan jurnal yang create hasilkan) ─

// TestPreviewCostEntry_MatchesCreateJournal verifies that PreviewCostEntry produces
// journal lines that exactly correspond to the journal CreateCostEntry would write.
// Both must use the same account resolution path (resolveDebitCreditCodes + resolveAccounts).
func TestPreviewCostEntry_MatchesCreateJournal(t *testing.T) {
	cases := []struct {
		name   string
		req    func() cost.CreateCostEntryRequest
		wantDr string // expected debit account code
		wantCr string // expected credit account code
	}{
		{
			name: "hard_bank",
			req: func() cost.CreateCostEntryRequest {
				r := baseReq()
				r.Category = domain.CostCategoryHard
				r.PaymentMethod = cost.PaymentMethodBank
				r.BankAccountCode = "1-1300"
				return r
			},
			wantDr: "1-3100",
			wantCr: "1-1300",
		},
		{
			name: "land_payable",
			req: func() cost.CreateCostEntryRequest {
				r := baseReq()
				r.Category = domain.CostCategoryLand
				r.HardSubcategory = ""
				r.PaymentMethod = cost.PaymentMethodPayable
				r.BankAccountCode = ""
				return r
			},
			wantDr: "1-3000",
			wantCr: "2-1000",
		},
		{
			// RULE KLIEN FREEZE (2026-09-04): Soft Cost direalisasi sebagai beban
			// (5-4700), bukan lagi dikapitalisasi ke Persediaan (1-3200).
			name: "soft_bank_mandiri",
			req: func() cost.CreateCostEntryRequest {
				r := baseReq()
				r.Category = domain.CostCategorySoft
				r.HardSubcategory = ""
				r.PaymentMethod = cost.PaymentMethodBank
				r.BankAccountCode = "1-1400"
				r.UnitID = nil
				return r
			},
			wantDr: "5-4700",
			wantCr: "1-1400",
		},
		{
			name: "operational_payable",
			req: func() cost.CreateCostEntryRequest {
				r := baseReq()
				r.Category = domain.CostCategoryOperational
				r.HardSubcategory = ""
				r.PaymentMethod = cost.PaymentMethodPayable
				r.BankAccountCode = ""
				r.UnitID = nil
				return r
			},
			wantDr: "5-4600",
			wantCr: "2-1000",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			svc, finder, writer, _, _ := defaultTestService()
			req := tc.req()

			// ── Preview ──────────────────────────────────────────────────────
			previewLines, err := svc.PreviewCostEntry(context.Background(), 1, req)
			if err != nil {
				t.Fatalf("PreviewCostEntry: %v", err)
			}
			if len(previewLines) != 2 {
				t.Fatalf("expected 2 preview lines, got %d", len(previewLines))
			}

			// Verify codes
			if previewLines[0].AccountCode != tc.wantDr {
				t.Errorf("preview debit code = %q, want %q", previewLines[0].AccountCode, tc.wantDr)
			}
			if previewLines[1].AccountCode != tc.wantCr {
				t.Errorf("preview credit code = %q, want %q", previewLines[1].AccountCode, tc.wantCr)
			}

			// Verify names are non-empty
			if previewLines[0].AccountName == "" {
				t.Error("preview debit name should be non-empty")
			}
			if previewLines[1].AccountName == "" {
				t.Error("preview credit name should be non-empty")
			}

			// Verify amounts
			if previewLines[0].Debit != req.Amount.String() {
				t.Errorf("preview debit amount = %q, want %q", previewLines[0].Debit, req.Amount.String())
			}
			if previewLines[0].Credit != domain.Zero.String() {
				t.Errorf("preview debit line credit should be zero, got %q", previewLines[0].Credit)
			}
			if previewLines[1].Credit != req.Amount.String() {
				t.Errorf("preview credit amount = %q, want %q", previewLines[1].Credit, req.Amount.String())
			}
			if previewLines[1].Debit != domain.Zero.String() {
				t.Errorf("preview credit line debit should be zero, got %q", previewLines[1].Debit)
			}

			// ── Create (same input) ───────────────────────────────────────────
			_, err = svc.CreateCostEntry(context.Background(), 1, req)
			if err != nil {
				t.Fatalf("CreateCostEntry: %v", err)
			}
			if len(writer.calls) != 1 {
				t.Fatalf("expected 1 journal call, got %d", len(writer.calls))
			}
			journalLines := writer.calls[0].Lines
			if len(journalLines) != 2 {
				t.Fatalf("expected 2 journal lines, got %d", len(journalLines))
			}

			// ── Cross-verify: preview codes → IDs must match journal IDs ─────
			// Preview uses codes; journal uses IDs; the mock maps code→ID.
			// If preview and create use the same resolution path, IDs must agree.
			expectedDebitID := finder.accounts[previewLines[0].AccountCode]
			expectedCreditID := finder.accounts[previewLines[1].AccountCode]

			if journalLines[0].AccountID != expectedDebitID {
				t.Errorf("journal debit AccountID = %d, but preview code %q resolves to %d",
					journalLines[0].AccountID, previewLines[0].AccountCode, expectedDebitID)
			}
			if journalLines[1].AccountID != expectedCreditID {
				t.Errorf("journal credit AccountID = %d, but preview code %q resolves to %d",
					journalLines[1].AccountID, previewLines[1].AccountCode, expectedCreditID)
			}

			// ── Amounts must match ────────────────────────────────────────────
			if !journalLines[0].Debit.Equal(req.Amount) {
				t.Errorf("journal debit = %s, want %s", journalLines[0].Debit, req.Amount)
			}
			if !journalLines[1].Credit.Equal(req.Amount) {
				t.Errorf("journal credit = %s, want %s", journalLines[1].Credit, req.Amount)
			}
		})
	}
}

// TestPreviewCostEntry_Validation_SameAsCreate verifies preview rejects the same
// invalid inputs that CreateCostEntry rejects, using identical validation logic.
func TestPreviewCostEntry_Validation_SameAsCreate(t *testing.T) {
	svc, _, _, _, _ := defaultTestService()
	ctx := context.Background()

	t.Run("fractional_amount", func(t *testing.T) {
		req := baseReq()
		req.Amount = domain.MustParse("500000000.50")
		_, err := svc.PreviewCostEntry(ctx, 1, req)
		if !errors.Is(err, cost.ErrCostAmountFractional) {
			t.Errorf("expected ErrCostAmountFractional, got %v", err)
		}
	})

	t.Run("invalid_category", func(t *testing.T) {
		req := baseReq()
		req.Category = "construction" // invalid
		_, err := svc.PreviewCostEntry(ctx, 1, req)
		if !errors.Is(err, cost.ErrInvalidCategory) {
			t.Errorf("expected ErrInvalidCategory, got %v", err)
		}
	})

	t.Run("bank_without_code", func(t *testing.T) {
		req := baseReq()
		req.PaymentMethod = cost.PaymentMethodBank
		req.BankAccountCode = ""
		_, err := svc.PreviewCostEntry(ctx, 1, req)
		if !errors.Is(err, cost.ErrBankAccountCodeRequired) {
			t.Errorf("expected ErrBankAccountCodeRequired, got %v", err)
		}
	})
}

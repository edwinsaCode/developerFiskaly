package cost_test

// Tes jembatan CostEntry ↔ BudgetItem (Phase 5 RAB link).
// Semua tes di file ini harus HIJAU tanpa mengubah logika Event 1 (jurnal).

import (
	"context"
	"errors"
	"testing"
	"time"

	"esaproperti/internal/cost"
	"esaproperti/internal/domain"
)

// ── mock BudgetItemLookup ─────────────────────────────────────────────────────

type mockBudgetItemLookup struct {
	// items: itemID → (projectID, costCategory, isMappable)
	items map[uint64]budgetItemStub
}

type budgetItemStub struct {
	projectID    uint64
	costCategory domain.CostCategory // kosong jika expense-only (marketing/other)
	isMappable   bool
}

func (m *mockBudgetItemLookup) FindItemForCostValidation(
	ctx context.Context, tenantID, projectID, itemID uint64,
) (domain.CostCategory, error) {
	stub, ok := m.items[itemID]
	if !ok {
		return "", cost.ErrBudgetItemNotFound
	}
	if stub.projectID != projectID {
		return "", cost.ErrBudgetItemProjectMismatch
	}
	if !stub.isMappable {
		return "", cost.ErrBudgetItemCategoryMismatch
	}
	return stub.costCategory, nil
}

// buildServiceWithLookup adalah helper yang menyertakan BudgetItemLookup.
func buildServiceWithLookup(lookup cost.BudgetItemLookup) (*cost.Service, *mockJournalWriter, *mockCostStore) {
	finder := &mockAccountFinder{accounts: standardAccounts}
	writer := newMockJournalWriter()
	store := newMockCostStore()
	queryer := &mockCostQueryer{byUnit: make(map[uint64]domain.UnitCostBreakdown)}
	svc := cost.NewService(finder, writer, store, queryer, lookup)
	return svc, writer, store
}

func ptr64(v uint64) *uint64 { return &v }

// ── Test: tanpa budget_item_id tetap valid (regresi) ─────────────────────────

func TestCostEntry_BudgetItemID_Nil_StillValid(t *testing.T) {
	svc, writer, _ := buildServiceWithLookup(nil) // nil = tidak ada lookup

	req := baseReq()
	req.BudgetItemID = nil // tidak tertaut RAB

	entry, err := svc.CreateCostEntry(context.Background(), 1, req)
	if err != nil {
		t.Fatalf("expected success tanpa budget_item_id, got: %v", err)
	}
	if entry.BudgetItemID != nil {
		t.Errorf("BudgetItemID harus nil, got %v", entry.BudgetItemID)
	}
	if len(writer.calls) != 1 {
		t.Errorf("jurnal harus dibuat tepat 1 kali, got %d", len(writer.calls))
	}
}

// ── Test: budget_item_id valid tersimpan di entry ─────────────────────────────

func TestCostEntry_BudgetItemID_Valid_Stored(t *testing.T) {
	lookup := &mockBudgetItemLookup{items: map[uint64]budgetItemStub{
		42: {projectID: 1, costCategory: domain.CostCategoryHard, isMappable: true},
	}}
	svc, _, store := buildServiceWithLookup(lookup)

	req := baseReq()
	req.ProjectID = 1
	req.Category = domain.CostCategoryHard
	req.BudgetItemID = ptr64(42)

	entry, err := svc.CreateCostEntry(context.Background(), 1, req)
	if err != nil {
		t.Fatalf("CreateCostEntry: %v", err)
	}
	if entry.BudgetItemID == nil || *entry.BudgetItemID != 42 {
		t.Errorf("BudgetItemID=%v, want 42", entry.BudgetItemID)
	}

	// Verifikasi tersimpan di store
	stored, _ := store.FindCostEntryByID(context.Background(), 1, entry.ID)
	if stored.BudgetItemID == nil || *stored.BudgetItemID != 42 {
		t.Errorf("stored BudgetItemID=%v, want 42", stored.BudgetItemID)
	}
}

// ── Test: lintas-tenant ditolak ───────────────────────────────────────────────

func TestCostEntry_BudgetItemID_CrossTenant_Rejected(t *testing.T) {
	// Mock mengembalikan ErrBudgetItemNotFound untuk item yang tidak ditemukan
	// di tenant yang diberikan (implementasi GORM melakukan filter by tenant_id).
	lookup := &mockBudgetItemLookup{items: map[uint64]budgetItemStub{
		// item 99 tidak ada di tenant 1 (mock kosong → ErrBudgetItemNotFound)
	}}
	svc, _, _ := buildServiceWithLookup(lookup)

	req := baseReq()
	req.BudgetItemID = ptr64(99) // item milik tenant lain

	_, err := svc.CreateCostEntry(context.Background(), 1, req)
	if !errors.Is(err, cost.ErrBudgetItemNotFound) {
		t.Errorf("expected ErrBudgetItemNotFound (lintas-tenant), got %v", err)
	}
}

// ── Test: lintas-project ditolak ──────────────────────────────────────────────

func TestCostEntry_BudgetItemID_CrossProject_Rejected(t *testing.T) {
	lookup := &mockBudgetItemLookup{items: map[uint64]budgetItemStub{
		// item 10 milik project 99, bukan project 1
		10: {projectID: 99, costCategory: domain.CostCategoryHard, isMappable: true},
	}}
	svc, _, _ := buildServiceWithLookup(lookup)

	req := baseReq()
	req.ProjectID = 1 // request ke project 1
	req.BudgetItemID = ptr64(10)

	_, err := svc.CreateCostEntry(context.Background(), 1, req)
	if !errors.Is(err, cost.ErrBudgetItemProjectMismatch) {
		t.Errorf("expected ErrBudgetItemProjectMismatch (lintas-project), got %v", err)
	}
}

// ── Test: kategori mismatch ditolak ──────────────────────────────────────────

func TestCostEntry_BudgetItemID_CategoryMismatch_Rejected(t *testing.T) {
	lookup := &mockBudgetItemLookup{items: map[uint64]budgetItemStub{
		5: {projectID: 1, costCategory: domain.CostCategoryLand, isMappable: true}, // item = land
	}}
	svc, _, _ := buildServiceWithLookup(lookup)

	req := baseReq()
	req.ProjectID = 1
	req.Category = domain.CostCategorySoft  // cost = soft ≠ land
	req.BudgetItemID = ptr64(5)

	_, err := svc.CreateCostEntry(context.Background(), 1, req)
	if !errors.Is(err, cost.ErrBudgetItemCategoryMismatch) {
		t.Errorf("expected ErrBudgetItemCategoryMismatch (soft vs land), got %v", err)
	}
}

// ── Test: construction ↔ hard alias diterima ──────────────────────────────────
// Budget item kategori 'construction' → mapped ke CostCategoryHard.
// Cost entry kategori 'hard' → cocok (alias).

func TestCostEntry_BudgetItemID_ConstructionHardAlias_Accepted(t *testing.T) {
	lookup := &mockBudgetItemLookup{items: map[uint64]budgetItemStub{
		7: {
			projectID:    1,
			costCategory: domain.CostCategoryHard, // construction telah di-map ke hard oleh FindItemForCostValidation
			isMappable:   true,
		},
	}}
	svc, _, _ := buildServiceWithLookup(lookup)

	req := baseReq()
	req.ProjectID = 1
	req.Category = domain.CostCategoryHard // cost = hard (sama setelah alias)
	req.BudgetItemID = ptr64(7)

	_, err := svc.CreateCostEntry(context.Background(), 1, req)
	if err != nil {
		t.Fatalf("construction↔hard alias harus diterima, got: %v", err)
	}
}

// ── Test: budget item kategori expense-only ditolak ───────────────────────────
// marketing dan other tidak punya CostCategory; tidak bisa ditautkan ke CostEntry.

func TestCostEntry_BudgetItemID_ExpenseOnly_Rejected(t *testing.T) {
	lookup := &mockBudgetItemLookup{items: map[uint64]budgetItemStub{
		8: {projectID: 1, costCategory: "", isMappable: false}, // marketing/other
	}}
	svc, _, _ := buildServiceWithLookup(lookup)

	req := baseReq()
	req.ProjectID = 1
	req.BudgetItemID = ptr64(8)

	_, err := svc.CreateCostEntry(context.Background(), 1, req)
	if !errors.Is(err, cost.ErrBudgetItemCategoryMismatch) {
		t.Errorf("expected ErrBudgetItemCategoryMismatch (expense-only item), got %v", err)
	}
}

// ── Test kritis: Event 1 (jurnal) TIDAK BERUBAH sama sekali ──────────────────
// budget_item_id adalah metadata-only; jurnal harus identik dengan/tanpa tautan.

func TestCostEntry_JournalLines_IdenticalWithAndWithoutBudgetItemID(t *testing.T) {
	// Buat dua entry: satu dengan budget_item_id, satu tanpa.
	lookup := &mockBudgetItemLookup{items: map[uint64]budgetItemStub{
		1: {projectID: 1, costCategory: domain.CostCategoryHard, isMappable: true},
	}}

	// Entry TANPA budget_item_id
	svcA, writerA, _ := buildServiceWithLookup(nil)
	reqA := baseReq()
	reqA.ProjectID = 1
	reqA.BudgetItemID = nil
	_, err := svcA.CreateCostEntry(context.Background(), 1, reqA)
	if err != nil {
		t.Fatalf("entry A: %v", err)
	}
	callA := writerA.calls[0]

	// Entry DENGAN budget_item_id
	svcB, writerB, _ := buildServiceWithLookup(lookup)
	reqB := baseReq()
	reqB.ProjectID = 1
	reqB.BudgetItemID = ptr64(1)
	_, err = svcB.CreateCostEntry(context.Background(), 1, reqB)
	if err != nil {
		t.Fatalf("entry B: %v", err)
	}
	callB := writerB.calls[0]

	// Jurnal harus identik
	if len(callA.Lines) != len(callB.Lines) {
		t.Fatalf("baris jurnal berbeda: A=%d, B=%d", len(callA.Lines), len(callB.Lines))
	}
	for i := range callA.Lines {
		lA, lB := callA.Lines[i], callB.Lines[i]
		if lA.AccountID != lB.AccountID {
			t.Errorf("baris %d: AccountID berbeda: %d vs %d", i, lA.AccountID, lB.AccountID)
		}
		if !lA.Debit.Equal(lB.Debit) {
			t.Errorf("baris %d: Debit berbeda: %s vs %s", i, lA.Debit, lB.Debit)
		}
		if !lA.Credit.Equal(lB.Credit) {
			t.Errorf("baris %d: Credit berbeda: %s vs %s", i, lA.Credit, lB.Credit)
		}
		if (lA.ProjectID == nil) != (lB.ProjectID == nil) || (lA.ProjectID != nil && *lA.ProjectID != *lB.ProjectID) {
			t.Errorf("baris %d: ProjectID berbeda: %v vs %v", i, lA.ProjectID, lB.ProjectID)
		}
		if (lA.UnitID == nil) != (lB.UnitID == nil) {
			t.Errorf("baris %d: UnitID berbeda: %v vs %v", i, lA.UnitID, lB.UnitID)
		}
	}
	// Deskripsi jurnal juga sama
	if callA.Description != callB.Description {
		t.Errorf("deskripsi jurnal berbeda:\n  A: %q\n  B: %q", callA.Description, callB.Description)
	}
}

// ── Test: validasi terjadi sebelum jurnal dibuat ──────────────────────────────

func TestCostEntry_BudgetItemID_ValidationBeforeJournal(t *testing.T) {
	lookup := &mockBudgetItemLookup{items: map[uint64]budgetItemStub{}}
	svc, writer, _ := buildServiceWithLookup(lookup)

	req := baseReq()
	req.BudgetItemID = ptr64(999) // tidak ada

	_, err := svc.CreateCostEntry(context.Background(), 1, req)
	if !errors.Is(err, cost.ErrBudgetItemNotFound) {
		t.Errorf("expected ErrBudgetItemNotFound, got %v", err)
	}
	if len(writer.calls) != 0 {
		t.Errorf("jurnal tidak boleh dibuat jika validasi gagal, got %d calls", len(writer.calls))
	}
}

// ── Test: posting Event 1 tidak berubah (konfirmasi tanggal/akun) ─────────────

func TestCostEntry_Event1_PostingUnchanged_WithBudgetItemID(t *testing.T) {
	lookup := &mockBudgetItemLookup{items: map[uint64]budgetItemStub{
		3: {projectID: 1, costCategory: domain.CostCategoryLand, isMappable: true},
	}}
	svc, writer, _ := buildServiceWithLookup(lookup)

	date := time.Date(2025, 9, 1, 0, 0, 0, 0, time.UTC)
	req := cost.CreateCostEntryRequest{
		ProjectID:       1,
		Category:        domain.CostCategoryLand,
		Amount:          domain.FromInt(200_000_000),
		PaymentMethod:   cost.PaymentMethodPayable,
		Date:            date,
		Vendor:          "PT Tanah Sejahtera",
		Description:     "Pembebasan lahan kavling B",
		BudgetItemID:    ptr64(3),
	}

	_, err := svc.CreateCostEntry(context.Background(), 1, req)
	if err != nil {
		t.Fatalf("CreateCostEntry: %v", err)
	}
	call := writer.calls[0]

	// Debit: 1-3000 Persediaan Tanah (acc ID 100)
	if call.Lines[0].AccountID != 100 {
		t.Errorf("debit account = %d, want 100 (1-3000 Persediaan Tanah)", call.Lines[0].AccountID)
	}
	if !call.Lines[0].Debit.Equal(domain.FromInt(200_000_000)) {
		t.Errorf("debit amount = %s, want 200000000", call.Lines[0].Debit)
	}
	// Kredit: 2-1000 Hutang Usaha (acc ID 300)
	if call.Lines[1].AccountID != 300 {
		t.Errorf("kredit account = %d, want 300 (2-1000 Hutang Usaha)", call.Lines[1].AccountID)
	}
	// Tanggal jurnal harus sama dengan tanggal cost entry
	if !call.Date.Equal(date) {
		t.Errorf("tanggal jurnal = %v, want %v", call.Date, date)
	}
	// Tidak ada baris ekstra
	if len(call.Lines) != 2 {
		t.Errorf("jurnal harus 2 baris (debit+kredit), got %d", len(call.Lines))
	}
}

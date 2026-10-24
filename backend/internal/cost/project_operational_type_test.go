package cost_test

// Biaya proyek kategori Operasional boleh merinci jenis pengeluaran (gaji,
// sewa, …) dari master yang sama dengan pengeluaran kantor, supaya laba rugi
// menampilkan baris beban yang deskriptif — bukan "Beban Operasional" generik.
// Baris tetap biaya proyek: kategori operational, tier overhead, boleh tertaut
// item RAB kategori operational.

import (
	"context"
	"errors"
	"strings"
	"testing"

	"esaproperti/internal/cost"
	"esaproperti/internal/domain"
)

type mockExpenseTypeResolver struct {
	policies map[uint64]cost.ExpenseTypePolicy
}

func (m *mockExpenseTypeResolver) ResolveExpenseType(_ context.Context, _, id uint64) (cost.ExpenseTypePolicy, error) {
	p, ok := m.policies[id]
	if !ok {
		return cost.ExpenseTypePolicy{}, cost.ErrExpenseTypeUnknown
	}
	return p, nil
}

const etGajiID uint64 = 7

func projectOperationalService(lookup cost.BudgetItemLookup) (*cost.Service, *mockJournalWriter, *mockCostStore) {
	accounts := map[string]uint64{"5-4100": 410}
	names := map[string]string{"5-4100": "Beban Gaji & Tunjangan"}
	for k, v := range standardAccounts {
		accounts[k] = v
	}
	for k, v := range standardNames {
		names[k] = v
	}
	finder := &mockAccountFinder{accounts: accounts, names: names}
	writer := newMockJournalWriter()
	store := newMockCostStore()
	queryer := &mockCostQueryer{byUnit: make(map[uint64]domain.UnitCostBreakdown)}
	svc := cost.NewService(finder, writer, store, queryer, lookup)
	svc.SetExpenseTypeResolver(&mockExpenseTypeResolver{policies: map[uint64]cost.ExpenseTypePolicy{
		etGajiID: {ID: etGajiID, Code: "gaji", Name: "Gaji & Tunjangan", ExpenseAccountCode: "5-4100"},
	}})
	return svc, writer, store
}

func projectOperationalReq() cost.CreateCostEntryRequest {
	req := baseReq()
	req.Category = domain.CostCategoryOperational
	req.CostTier = domain.CostTierOverhead
	req.HardSubcategory = ""
	req.Amount = domain.FromInt(4_000_000)
	id := etGajiID
	req.ExpenseTypeID = &id
	return req
}

func TestProjectOperational_JenisPengeluaran_DebitAkunJenis(t *testing.T) {
	lookup := &mockBudgetItemLookup{items: map[uint64]budgetItemStub{
		55: {projectID: 1, costCategory: domain.CostCategoryOperational, isMappable: true},
	}}
	svc, writer, store := projectOperationalService(lookup)

	req := projectOperationalReq()
	req.BudgetItemID = ptr64(55) // realisasi RAB operasional tetap sah

	preview, err := svc.PreviewCostEntry(context.Background(), 1, req)
	if err != nil {
		t.Fatalf("PreviewCostEntry: %v", err)
	}
	if preview[0].AccountCode != "5-4100" || preview[1].AccountCode != "1-1300" {
		t.Fatalf("pratinjau harus Dr 5-4100 / Cr 1-1300, dapat %+v", preview)
	}

	entry, err := svc.CreateCostEntry(context.Background(), 1, req)
	if err != nil {
		t.Fatalf("CreateCostEntry: %v", err)
	}
	call := writer.calls[0]
	if call.Lines[0].AccountID != 410 || call.Lines[1].AccountID != 200 {
		t.Errorf("jurnal harus Dr Beban Gaji (410) / Cr Bank (200), dapat %d / %d",
			call.Lines[0].AccountID, call.Lines[1].AccountID)
	}
	if !call.Lines[0].Debit.Equal(call.Lines[1].Credit) {
		t.Errorf("jurnal tidak balanced: %s vs %s", call.Lines[0].Debit, call.Lines[1].Credit)
	}
	if !strings.Contains(call.Description, "Gaji & Tunjangan") {
		t.Errorf("deskripsi jurnal harus menyebut jenis pengeluaran, dapat %q", call.Description)
	}

	stored, _ := store.FindCostEntryByID(context.Background(), 1, entry.ID)
	if stored.Category != domain.CostCategoryOperational {
		t.Errorf("kategori harus tetap operational (realisasi RAB), dapat %s", stored.Category)
	}
	if stored.ExpenseTypeID == nil || *stored.ExpenseTypeID != etGajiID {
		t.Errorf("expense_type_id harus tersimpan, dapat %v", stored.ExpenseTypeID)
	}
	if stored.ProjectID == nil || *stored.ProjectID != 1 {
		t.Errorf("project_id harus tersimpan, dapat %v", stored.ProjectID)
	}
}

func TestProjectOperational_TanpaJenis_TetapAkunOperasional(t *testing.T) {
	svc, writer, _ := projectOperationalService(nil)

	req := projectOperationalReq()
	req.ExpenseTypeID = nil
	if _, err := svc.CreateCostEntry(context.Background(), 1, req); err != nil {
		t.Fatalf("CreateCostEntry: %v", err)
	}
	if got := writer.calls[0].Lines[0].AccountID; got != 402 {
		t.Errorf("tanpa jenis pengeluaran debit harus 5-4600 (402), dapat %d", got)
	}
}

func TestProjectOperational_JenisTidakDikenal_Ditolak(t *testing.T) {
	svc, writer, _ := projectOperationalService(nil)

	req := projectOperationalReq()
	unknown := uint64(999)
	req.ExpenseTypeID = &unknown
	if _, err := svc.CreateCostEntry(context.Background(), 1, req); !errors.Is(err, cost.ErrExpenseTypeUnknown) {
		t.Fatalf("harus ErrExpenseTypeUnknown (fail-closed), dapat %v", err)
	}
	if len(writer.calls) != 0 {
		t.Error("tidak boleh ada jurnal saat jenis gagal resolve")
	}
}

func TestProjectOperational_JenisPadaKategoriLain_Ditolak(t *testing.T) {
	svc, _, _ := projectOperationalService(nil)

	t.Run("kapitalisasi hard", func(t *testing.T) {
		req := projectOperationalReq()
		req.Category = domain.CostCategoryHard
		req.CostTier = domain.CostTierShared
		req.HardSubcategory = domain.ConstructionSaranaPrasarana
		if _, err := svc.CreateCostEntry(context.Background(), 1, req); !errors.Is(err, cost.ErrExpenseTypeNotForProjectCost) {
			t.Fatalf("harus ErrExpenseTypeNotForProjectCost, dapat %v", err)
		}
	})

	t.Run("soft tetap jalur kantor: tidak boleh tertaut RAB", func(t *testing.T) {
		req := projectOperationalReq()
		req.Category = domain.CostCategorySoft
		req.BudgetItemID = ptr64(55)
		if _, err := svc.CreateCostEntry(context.Background(), 1, req); !errors.Is(err, cost.ErrExpenseTypeWithBudgetItem) {
			t.Fatalf("harus ErrExpenseTypeWithBudgetItem, dapat %v", err)
		}
	})
}

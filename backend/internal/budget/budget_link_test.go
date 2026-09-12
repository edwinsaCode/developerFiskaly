package budget_test

// Tes realisasi per-BudgetItem (Phase 5 RAB link).
// Menggunakan mockRealisasiProvider yang sudah ada di service_test.go,
// diperluas dengan GetRealisasiPerItem.

import (
	"context"
	"errors"
	"testing"

	"esaproperti/internal/budget"
	"esaproperti/internal/domain"
)

// mockRealisasiProviderFull memperluas mockRealisasiProvider dengan GetRealisasiPerItem.
// Dipakai hanya di file ini agar tidak konflik dengan mock yang ada.
type mockRealisasiProviderFull struct {
	byCategory  map[domain.CostCategory]domain.Money
	byItemID    map[uint64]domain.Money // key = budget_item_id
}

func (m *mockRealisasiProviderFull) GetRealisasiByProject(
	_ context.Context, _, _ uint64, _ *uint64,
) (map[domain.CostCategory]domain.Money, error) {
	if m.byCategory == nil {
		return make(map[domain.CostCategory]domain.Money), nil
	}
	return m.byCategory, nil
}

func (m *mockRealisasiProviderFull) GetRealisasiPerItem(
	_ context.Context, _, _ uint64,
) (map[uint64]domain.Money, error) {
	if m.byItemID == nil {
		return make(map[uint64]domain.Money), nil
	}
	return m.byItemID, nil
}

func (m *mockRealisasiProviderFull) GetRealisasiByProjectCash(
	ctx context.Context, tenantID, projectID uint64, phaseID *uint64,
) (map[domain.CostCategory]domain.Money, error) {
	return m.GetRealisasiByProject(ctx, tenantID, projectID, phaseID)
}

func (m *mockRealisasiProviderFull) GetRealisasiPerItemCash(
	ctx context.Context, tenantID, planID uint64,
) (map[uint64]domain.Money, error) {
	return m.GetRealisasiPerItem(ctx, tenantID, planID)
}

func buildServiceFull(store *mockBudgetStore, r *mockRealisasiProviderFull) *budget.Service {
	return budget.NewService(store, r)
}

// ── helper: buat plan active dengan items, return (planID, item1ID, item2ID) ──

func setupActivePlanWithItems(t *testing.T, tenantID, projectID uint64) (planID, item1ID, item2ID uint64) {
	t.Helper()
	store := newMockStore()
	svc := buildServiceFull(store, &mockRealisasiProviderFull{})

	plan, err := svc.CreatePlan(context.Background(), tenantID, budget.CreatePlanRequest{
		ProjectID: projectID, Label: "RAB Test",
	})
	if err != nil {
		t.Fatal(err)
	}

	i1, err := svc.AddItem(context.Background(), tenantID, budget.AddItemRequest{
		PlanID: plan.ID, Category: budget.BudgetCategoryConstruction,
		Subcategory:    string(domain.ConstructionSaranaPrasarana),
		BudgetedAmount: rupiah(500_000_000), Description: "Struktur bangunan",
	})
	if err != nil {
		t.Fatal(err)
	}

	i2, err := svc.AddItem(context.Background(), tenantID, budget.AddItemRequest{
		PlanID: plan.ID, Category: budget.BudgetCategoryLand,
		BudgetedAmount: rupiah(300_000_000), Description: "Pembebasan lahan",
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = svc.ApprovePlan(context.Background(), tenantID, budget.ApprovePlanRequest{
		PlanID: plan.ID, ApprovedBy: "direktur",
	})
	if err != nil {
		t.Fatal(err)
	}
	return plan.ID, i1.ID, i2.ID
}

// ── Test: realisasi per-item benar ───────────────────────────────────────────

func TestBudget_RealisasiPerItem_CorrectAggregate(t *testing.T) {
	store := newMockStore()
	realisasi := &mockRealisasiProviderFull{}
	svc := buildServiceFull(store, realisasi)

	// Setup plan
	plan, _ := svc.CreatePlan(context.Background(), 1, budget.CreatePlanRequest{ProjectID: 10, Label: "RAB"})
	item1, _ := svc.AddItem(context.Background(), 1, budget.AddItemRequest{
		PlanID: plan.ID, Category: budget.BudgetCategoryConstruction,
		Subcategory:    string(domain.ConstructionSaranaPrasarana),
		BudgetedAmount: rupiah(500_000_000), Description: "Struktur",
	})
	item2, _ := svc.AddItem(context.Background(), 1, budget.AddItemRequest{
		PlanID: plan.ID, Category: budget.BudgetCategoryLand,
		BudgetedAmount: rupiah(300_000_000), Description: "Tanah",
	})
	_, _ = svc.ApprovePlan(context.Background(), 1, budget.ApprovePlanRequest{PlanID: plan.ID, ApprovedBy: "x"})

	// Inject realisasi per-item: item1 = 400M, item2 = 250M
	realisasi.byItemID = map[uint64]domain.Money{
		item1.ID: rupiah(400_000_000),
		item2.ID: rupiah(250_000_000),
	}

	rows, err := svc.GetItemsRealisasi(context.Background(), 1, 10, nil)
	if err != nil {
		t.Fatalf("GetItemsRealisasi: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("expect 2 rows, got %d", len(rows))
	}

	// Cari item1 (construction)
	var r1, r2 *budget.ItemRealisasiRow
	for _, r := range rows {
		switch r.ItemID {
		case item1.ID:
			r1 = r
		case item2.ID:
			r2 = r
		}
	}
	if r1 == nil || r2 == nil {
		t.Fatal("tidak menemukan semua row")
	}

	// Item1: budgeted=500M, realisasi=400M, selisih=100M, persen=80.00%
	if r1.Budgeted != "500000000" {
		t.Errorf("item1 Budgeted=%s, want 500000000", r1.Budgeted)
	}
	if r1.Realisasi != "400000000" {
		t.Errorf("item1 Realisasi=%s, want 400000000", r1.Realisasi)
	}
	if r1.Selisih != "100000000" {
		t.Errorf("item1 Selisih=%s, want 100000000", r1.Selisih)
	}
	if r1.PersenRealisasi != "80.00%" {
		t.Errorf("item1 PersenRealisasi=%s, want 80.00%%", r1.PersenRealisasi)
	}

	// Item2: budgeted=300M, realisasi=250M, selisih=50M
	if r2.Realisasi != "250000000" {
		t.Errorf("item2 Realisasi=%s, want 250000000", r2.Realisasi)
	}
	if r2.Selisih != "50000000" {
		t.Errorf("item2 Selisih=%s, want 50000000", r2.Selisih)
	}
	if r2.Category != budget.BudgetCategoryLand {
		t.Errorf("item2 Category=%s, want land", r2.Category)
	}
}

// ── Test: cost entry tanpa budget_item_id tidak masuk realisasi per-item ──────

func TestBudget_RealisasiPerItem_UnlinkedEntries_NotCounted(t *testing.T) {
	store := newMockStore()
	realisasi := &mockRealisasiProviderFull{
		// byItemID kosong = tidak ada cost entry yang tertaut ke item manapun
		byItemID: map[uint64]domain.Money{},
	}
	svc := buildServiceFull(store, realisasi)

	plan, _ := svc.CreatePlan(context.Background(), 1, budget.CreatePlanRequest{ProjectID: 10, Label: "RAB"})
	_, _ = svc.AddItem(context.Background(), 1, budget.AddItemRequest{
		PlanID: plan.ID, Category: budget.BudgetCategoryConstruction,
		Subcategory:    string(domain.ConstructionSaranaPrasarana),
		BudgetedAmount: rupiah(200_000_000),
	})
	_, _ = svc.ApprovePlan(context.Background(), 1, budget.ApprovePlanRequest{PlanID: plan.ID})

	rows, err := svc.GetItemsRealisasi(context.Background(), 1, 10, nil)
	if err != nil {
		t.Fatalf("GetItemsRealisasi: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expect 1 row, got %d", len(rows))
	}
	// Realisasi = 0 karena tidak ada cost entry tertaut
	if rows[0].Realisasi != "0" {
		t.Errorf("Realisasi=%s, want 0 (tidak ada cost entry tertaut item)", rows[0].Realisasi)
	}
	if rows[0].Selisih != "200000000" {
		t.Errorf("Selisih=%s, want 200000000 (under budget sepenuhnya)", rows[0].Selisih)
	}
	if rows[0].PersenRealisasi != "0.00%" {
		t.Errorf("PersenRealisasi=%s, want 0.00%%", rows[0].PersenRealisasi)
	}
}

// ── Test: tidak ada active plan → error ──────────────────────────────────────

func TestBudget_GetItemsRealisasi_NoActivePlan_Error(t *testing.T) {
	svc := buildServiceFull(newMockStore(), &mockRealisasiProviderFull{})

	_, err := svc.GetItemsRealisasi(context.Background(), 1, 10, nil)
	if !errors.Is(err, budget.ErrNoActivePlan) {
		t.Errorf("expected ErrNoActivePlan, got %v", err)
	}
}

// ── Test: realisasi per-item untuk item dengan realisasi melebihi budget ──────

func TestBudget_RealisasiPerItem_OverBudget(t *testing.T) {
	store := newMockStore()
	realisasi := &mockRealisasiProviderFull{}
	svc := buildServiceFull(store, realisasi)

	plan, _ := svc.CreatePlan(context.Background(), 1, budget.CreatePlanRequest{ProjectID: 10, Label: "RAB"})
	item, _ := svc.AddItem(context.Background(), 1, budget.AddItemRequest{
		PlanID: plan.ID, Category: budget.BudgetCategorySoft,
		BudgetedAmount: rupiah(100_000_000), Description: "IMB",
	})
	_, _ = svc.ApprovePlan(context.Background(), 1, budget.ApprovePlanRequest{PlanID: plan.ID})

	// Realisasi 150M > budgeted 100M → selisih negatif
	realisasi.byItemID = map[uint64]domain.Money{
		item.ID: rupiah(150_000_000),
	}

	rows, err := svc.GetItemsRealisasi(context.Background(), 1, 10, nil)
	if err != nil {
		t.Fatalf("GetItemsRealisasi: %v", err)
	}
	if rows[0].Selisih != "-50000000" {
		t.Errorf("Selisih=%s, want -50000000 (over budget)", rows[0].Selisih)
	}
	if rows[0].PersenRealisasi != "150.00%" {
		t.Errorf("PersenRealisasi=%s, want 150.00%%", rows[0].PersenRealisasi)
	}
}

// ── Test: realisasi level-KATEGORI yang sudah ada tidak terpengaruh ───────────
// Regression test: GetRABvsRealisasi (per-kategori) harus tetap berjalan normal.

func TestBudget_GetRABvsRealisasi_Regression_AfterPerItemAdded(t *testing.T) {
	store := newMockStore()
	// mockRealisasiProviderFull juga mengimplementasi RealisasiProvider
	realisasi := &mockRealisasiProviderFull{
		byCategory: map[domain.CostCategory]domain.Money{
			domain.CostCategoryHard: rupiah(300_000_000),
		},
		byItemID: map[uint64]domain.Money{},
	}
	svc := buildServiceFull(store, realisasi)

	plan, _ := svc.CreatePlan(context.Background(), 1, budget.CreatePlanRequest{ProjectID: 10, Label: "RAB"})
	_, _ = svc.AddItem(context.Background(), 1, budget.AddItemRequest{
		PlanID: plan.ID, Category: budget.BudgetCategoryConstruction,
		Subcategory:    string(domain.ConstructionSaranaPrasarana),
		BudgetedAmount: rupiah(500_000_000),
	})
	_, _ = svc.ApprovePlan(context.Background(), 1, budget.ApprovePlanRequest{PlanID: plan.ID})

	// GetRABvsRealisasi per-kategori HARUS tetap jalan
	report, err := svc.GetRABvsRealisasi(context.Background(), 1, 10, nil)
	if err != nil {
		t.Fatalf("GetRABvsRealisasi: %v", err)
	}
	var constructionRow *budget.RABvsRealisasiRow
	for i := range report.Rows {
		if report.Rows[i].Category == budget.BudgetCategoryConstruction {
			constructionRow = &report.Rows[i]
			break
		}
	}
	if constructionRow == nil {
		t.Fatal("baris construction tidak ditemukan")
	}
	// Realisasi construction = 300M (dari byCategory via alias hard)
	if constructionRow.Realisasi != "300000000" {
		t.Errorf("realisasi construction=%s, want 300000000", constructionRow.Realisasi)
	}
}

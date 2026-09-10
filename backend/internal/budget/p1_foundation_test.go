package budget_test

import (
	"context"
	"errors"
	"testing"

	"esaproperti/internal/budget"
	"esaproperti/internal/domain"
)

// P1-1 foundation: HPP-vs-expense taxonomy split + budgeted HPP pool query.

func TestBudgetCategory_HPPvsExpense_Split(t *testing.T) {
	// RULE KLIEN FREEZE (2026-09-04): HPP hanya land + construction/hard.
	// Soft menyusul Operational keluar dari himpunan kapitalisasi.
	capd := []budget.BudgetCategory{
		budget.BudgetCategoryLand, budget.BudgetCategoryConstruction,
	}
	exp := []budget.BudgetCategory{
		budget.BudgetCategoryMarketing, budget.BudgetCategoryOther,
		budget.BudgetCategoryOperational, budget.BudgetCategorySoft,
	}

	for _, c := range capd {
		if !c.IsCapitalized() || c.IsExpenseOnly() {
			t.Errorf("%s harus kapitalisasi (masuk HPP)", c)
		}
		if c.ExpenseAccountCode() != "" {
			t.Errorf("%s (kapitalisasi) tidak boleh punya akun beban, got %s", c, c.ExpenseAccountCode())
		}
	}
	for _, c := range exp {
		if c.IsCapitalized() || !c.IsExpenseOnly() {
			t.Errorf("%s harus expense-only (bukan HPP)", c)
		}
	}
	if got := budget.BudgetCategoryMarketing.ExpenseAccountCode(); got != "5-3000" {
		t.Errorf("marketing → 5-3000 (Beban Pemasaran), got %s", got)
	}
	if got := budget.BudgetCategoryOther.ExpenseAccountCode(); got != "5-4000" {
		t.Errorf("other → 5-4000 (Beban Umum & Adm), got %s", got)
	}
	if got := budget.BudgetCategoryOperational.ExpenseAccountCode(); got != "5-4600" {
		t.Errorf("operational → 5-4600 (Beban Operasional), got %s", got)
	}
	if got := budget.BudgetCategorySoft.ExpenseAccountCode(); got != "5-4700" {
		t.Errorf("soft → 5-4700 (Beban Soft Cost), got %s", got)
	}
}

func TestGetBudgetedHPPPool(t *testing.T) {
	store := newMockStore()
	svc := buildService(store, nil)
	ctx := context.Background()

	plan, _ := svc.CreatePlan(ctx, 1, budget.CreatePlanRequest{ProjectID: 10, Label: "RAB v1"})
	add := func(cat budget.BudgetCategory, amt int64) {
		sub := ""
		if cat == budget.BudgetCategoryConstruction {
			sub = string(domain.ConstructionSaranaPrasarana)
		}
		if _, err := svc.AddItem(ctx, 1, budget.AddItemRequest{
			PlanID: plan.ID, Category: cat, Subcategory: sub, BudgetedAmount: rupiah(amt), Description: string(cat),
		}); err != nil {
			t.Fatalf("AddItem %s: %v", cat, err)
		}
	}
	add(budget.BudgetCategoryLand, 2_000_000_000)
	add(budget.BudgetCategoryConstruction, 5_000_000_000) // → hard
	add(budget.BudgetCategorySoft, 500_000_000)        // EXPENSE (RULE KLIEN FREEZE 2026-09-04) — excluded from HPP pool
	add(budget.BudgetCategoryOperational, 300_000_000) // EXPENSE (RULE KLIEN 2026-09-04) — excluded from HPP pool
	add(budget.BudgetCategoryMarketing, 400_000_000)   // EXPENSE — excluded from HPP pool
	add(budget.BudgetCategoryOther, 100_000_000)       // EXPENSE — excluded from HPP pool

	// Gate (BCA-2): tanpa plan AKTIF → ErrNoActivePlan.
	if _, err := svc.GetBudgetedHPPPool(ctx, 1, 10, nil); !errors.Is(err, budget.ErrNoActivePlan) {
		t.Fatalf("tanpa RAB aktif harus ErrNoActivePlan, got %v", err)
	}

	if _, err := svc.ApprovePlan(ctx, 1, budget.ApprovePlanRequest{PlanID: plan.ID, ApprovedBy: "owner"}); err != nil {
		t.Fatalf("ApprovePlan: %v", err)
	}

	pool, err := svc.GetBudgetedHPPPool(ctx, 1, 10, nil)
	if err != nil {
		t.Fatalf("GetBudgetedHPPPool: %v", err)
	}
	// RULE KLIEN FREEZE (2026-09-04): Soft menyusul Operational keluar dari
	// pool HPP — GetBudgetedHPPPool tidak boleh lagi mengisi pool.Soft.
	if pool.Land.String() != "2000000000" || pool.Hard.String() != "5000000000" ||
		!pool.Soft.IsZero() || !pool.Financing.IsZero() {
		t.Errorf("pool per kategori salah: %+v", pool)
	}
	// soft (500jt) + operational (300jt) + marketing (400jt) + other (100jt)
	// TIDAK masuk pool HPP.
	if pool.Total().String() != "7000000000" {
		t.Errorf("total HPP pool harus 7.000.000.000 (tanpa soft/operational/marketing/other), got %s", pool.Total())
	}
}

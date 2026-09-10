package budget_test

// Matriks validasi Subcategory RAB Konstruksi (UAT 2026-09-07): "Produksi"
// tunggal tidak cukup — item RAB Konstruksi WAJIB menyatakan salah satu dari
// 4 subkategori kanonik. Kategori lain (Land/Soft/Marketing/Other/Operational)
// tidak pernah diwajibkan/divalidasi terhadap subkategori ini.

import (
	"context"
	"errors"
	"testing"

	"esaproperti/internal/budget"
	"esaproperti/internal/domain"
)

func TestBudget_AddItem_ConstructionSubcategory_AllFourCanonicalValues_Valid(t *testing.T) {
	for _, sub := range domain.AllConstructionSubcategories {
		sub := sub
		t.Run(string(sub), func(t *testing.T) {
			store := newMockStore()
			svc := buildService(store, nil)
			plan, err := svc.CreatePlan(context.Background(), 1, budget.CreatePlanRequest{ProjectID: 10, Label: "RAB"})
			if err != nil {
				t.Fatalf("CreatePlan: %v", err)
			}
			item, err := svc.AddItem(context.Background(), 1, budget.AddItemRequest{
				PlanID: plan.ID, Category: budget.BudgetCategoryConstruction,
				Subcategory: string(sub), BudgetedAmount: rupiah(100_000_000), Description: string(sub),
			})
			if err != nil {
				t.Fatalf("AddItem dengan Subcategory=%q harus valid, got %v", sub, err)
			}
			if item.Subcategory != string(sub) {
				t.Errorf("stored Subcategory=%q, want %q", item.Subcategory, sub)
			}
		})
	}
}

func TestBudget_AddItem_ConstructionSubcategory_EmptyOrInvalid_Rejected(t *testing.T) {
	cases := []string{"", "produksi", "Produksi Subsidi", "produksi_subsidi ", "PRODUKSI_SUBSIDI"}
	for _, sub := range cases {
		sub := sub
		t.Run("sub="+sub, func(t *testing.T) {
			store := newMockStore()
			svc := buildService(store, nil)
			plan, _ := svc.CreatePlan(context.Background(), 1, budget.CreatePlanRequest{ProjectID: 10, Label: "RAB"})
			_, err := svc.AddItem(context.Background(), 1, budget.AddItemRequest{
				PlanID: plan.ID, Category: budget.BudgetCategoryConstruction,
				Subcategory: sub, BudgetedAmount: rupiah(100_000_000), Description: "x",
			})
			if !errors.Is(err, budget.ErrInvalidConstructionSubcategory) {
				t.Errorf("Subcategory=%q: expected ErrInvalidConstructionSubcategory, got %v", sub, err)
			}
		})
	}
}

// Produksi Subsidi dan Produksi Komersial adalah item RAB TERPISAH — masing-masing
// tersimpan dengan Subcategory-nya sendiri, tidak digabung ke satu pool "produksi".
func TestBudget_AddItem_ProduksiSubsidiAndKomersial_AreSeparateItems(t *testing.T) {
	store := newMockStore()
	svc := buildService(store, nil)
	plan, _ := svc.CreatePlan(context.Background(), 1, budget.CreatePlanRequest{ProjectID: 10, Label: "RAB"})

	subsidi, err := svc.AddItem(context.Background(), 1, budget.AddItemRequest{
		PlanID: plan.ID, Category: budget.BudgetCategoryConstruction,
		Subcategory: string(domain.ConstructionProduksiSubsidi), BudgetedAmount: rupiah(100_000_000), Description: "Produksi Subsidi",
	})
	if err != nil {
		t.Fatalf("AddItem produksi_subsidi: %v", err)
	}
	komersial, err := svc.AddItem(context.Background(), 1, budget.AddItemRequest{
		PlanID: plan.ID, Category: budget.BudgetCategoryConstruction,
		Subcategory: string(domain.ConstructionProduksiKomersial), BudgetedAmount: rupiah(250_000_000), Description: "Produksi Komersial",
	})
	if err != nil {
		t.Fatalf("AddItem produksi_komersial: %v", err)
	}
	if subsidi.ID == komersial.ID {
		t.Fatal("Produksi Subsidi dan Komersial tidak boleh menjadi item yang sama")
	}
	if subsidi.Subcategory == komersial.Subcategory {
		t.Errorf("Subcategory sama (%q) — Subsidi dan Komersial harus terpisah", subsidi.Subcategory)
	}
	if !subsidi.BudgetedAmount.Equal(rupiah(100_000_000)) || !komersial.BudgetedAmount.Equal(rupiah(250_000_000)) {
		t.Errorf("nilai RAB tercampur: subsidi=%s komersial=%s", subsidi.BudgetedAmount, komersial.BudgetedAmount)
	}
}

// Kategori non-Konstruksi tidak pernah divalidasi terhadap subkategori
// Konstruksi — Subcategory kosong (atau apa pun) tetap diterima.
func TestBudget_AddItem_NonConstructionCategory_SubcategoryNotValidated(t *testing.T) {
	for _, cat := range []budget.BudgetCategory{
		budget.BudgetCategoryLand, budget.BudgetCategorySoft,
		budget.BudgetCategoryMarketing, budget.BudgetCategoryOther, budget.BudgetCategoryOperational,
	} {
		cat := cat
		t.Run(string(cat), func(t *testing.T) {
			store := newMockStore()
			svc := buildService(store, nil)
			plan, _ := svc.CreatePlan(context.Background(), 1, budget.CreatePlanRequest{ProjectID: 10, Label: "RAB"})
			_, err := svc.AddItem(context.Background(), 1, budget.AddItemRequest{
				PlanID: plan.ID, Category: cat, Subcategory: "",
				BudgetedAmount: rupiah(100_000_000), Description: string(cat),
			})
			if err != nil {
				t.Errorf("kategori %s dengan Subcategory kosong harus tetap valid, got %v", cat, err)
			}
		})
	}
}

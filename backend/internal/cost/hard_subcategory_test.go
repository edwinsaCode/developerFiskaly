package cost_test

// Matriks validasi HardSubcategory (UAT 2026-09-07): Cost Entry/Pengeluaran
// harus tahu Produksi mana (Subsidi/Komersial) atau Sarana & Prasarana/
// Perizinan yang dituju SETIAP KALI biayanya masuk ke pool project-wide
// (tier=shared) — di situlah allocation.HardPoolSource butuh sinyal ini untuk
// menentukan unit penerima. Untuk tier=direct, unit sudah menentukan sendiri
// Subsidi/Komersial-nya (tidak ambigu), sehingga HardSubcategory opsional.

import (
	"context"
	"errors"
	"testing"

	"esaproperti/internal/cost"
	"esaproperti/internal/domain"
)

// Category=Hard + tier=Shared + HardSubcategory kosong → WAJIB diisi, pool
// project-wide ambigu tanpa ini (bisa jatuh ke unit Subsidi ATAU Komersial).
func TestCostEntry_HardSubcategory_SharedTier_Empty_Rejected(t *testing.T) {
	svc, _, _, _, _ := defaultTestService()
	req := baseReq()
	req.CostTier = domain.CostTierShared
	req.UnitID = nil
	req.HardSubcategory = ""

	_, err := svc.CreateCostEntry(context.Background(), 1, req)
	if !errors.Is(err, cost.ErrHardSubcategoryRequired) {
		t.Errorf("expected ErrHardSubcategoryRequired, got %v", err)
	}
}

// Category=Hard + tier=Shared + salah satu dari 4 nilai kanonik → valid.
func TestCostEntry_HardSubcategory_SharedTier_AllFourCanonicalValues_Valid(t *testing.T) {
	for _, sub := range domain.AllConstructionSubcategories {
		sub := sub
		t.Run(string(sub), func(t *testing.T) {
			svc, _, _, store, _ := defaultTestService()
			req := baseReq()
			req.CostTier = domain.CostTierShared
			req.UnitID = nil
			req.HardSubcategory = sub

			entry, err := svc.CreateCostEntry(context.Background(), 1, req)
			if err != nil {
				t.Fatalf("HardSubcategory=%q harus valid, got %v", sub, err)
			}
			stored, _ := store.FindCostEntryByID(context.Background(), 1, entry.ID)
			if stored.HardSubcategory != sub {
				t.Errorf("stored HardSubcategory=%q, want %q", stored.HardSubcategory, sub)
			}
		})
	}
}

// Category=Hard + tier=Shared + nilai bukan salah satu dari 4 kanonik → ditolak.
func TestCostEntry_HardSubcategory_SharedTier_InvalidValue_Rejected(t *testing.T) {
	cases := []domain.ConstructionSubcategory{"produksi", "Produksi Subsidi", "produksi_subsidi ", "lainnya"}
	for _, sub := range cases {
		sub := sub
		t.Run(string(sub), func(t *testing.T) {
			svc, _, _, _, _ := defaultTestService()
			req := baseReq()
			req.CostTier = domain.CostTierShared
			req.UnitID = nil
			req.HardSubcategory = sub

			_, err := svc.CreateCostEntry(context.Background(), 1, req)
			if !errors.Is(err, cost.ErrInvalidHardSubcategory) {
				t.Errorf("HardSubcategory=%q: expected ErrInvalidHardSubcategory, got %v", sub, err)
			}
		})
	}
}

// Category=Hard + tier=Direct + HardSubcategory kosong → OPSIONAL, unit sudah
// menentukan sendiri Subsidi/Komersial-nya, tidak ada ambiguitas pool.
func TestCostEntry_HardSubcategory_DirectTier_Empty_Allowed(t *testing.T) {
	svc, _, _, _, _ := defaultTestService()
	req := baseReq()
	req.CostTier = domain.CostTierDirect
	req.UnitID = ptr64(7)
	req.HardSubcategory = ""

	_, err := svc.CreateCostEntry(context.Background(), 1, req)
	if err != nil {
		t.Errorf("direct tier dengan HardSubcategory kosong harus tetap valid, got %v", err)
	}
}

// Category=Hard + tier=Direct + HardSubcategory diisi (opsional tapi valid)
// → tetap diterima dan tersimpan.
func TestCostEntry_HardSubcategory_DirectTier_ValidValue_Allowed(t *testing.T) {
	svc, _, _, store, _ := defaultTestService()
	req := baseReq()
	req.CostTier = domain.CostTierDirect
	req.UnitID = ptr64(7)
	req.HardSubcategory = domain.ConstructionProduksiSubsidi

	entry, err := svc.CreateCostEntry(context.Background(), 1, req)
	if err != nil {
		t.Fatalf("CreateCostEntry: %v", err)
	}
	stored, _ := store.FindCostEntryByID(context.Background(), 1, entry.ID)
	if stored.HardSubcategory != domain.ConstructionProduksiSubsidi {
		t.Errorf("stored HardSubcategory=%q, want produksi_subsidi", stored.HardSubcategory)
	}
}

// Category != Hard (Land/Soft/Marketing/Other/Operational) + HardSubcategory
// diisi → WAJIB kosong, HardSubcategory hanya bermakna untuk Konstruksi/Hard.
func TestCostEntry_HardSubcategory_NonHardCategory_NonEmpty_Rejected(t *testing.T) {
	cases := []struct {
		category domain.CostCategory
		tier     domain.CostTier
		unitID   *uint64
	}{
		{domain.CostCategoryLand, domain.CostTierShared, nil},
		{domain.CostCategoryMarketing, domain.CostTierOverhead, nil},
		{domain.CostCategoryOther, domain.CostTierOverhead, nil},
		{domain.CostCategoryOperational, domain.CostTierOverhead, nil},
		{domain.CostCategorySoft, domain.CostTierOverhead, nil},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(string(tc.category), func(t *testing.T) {
			svc, _, _, _, _ := defaultTestService()
			req := baseReq()
			req.Category = tc.category
			req.CostTier = tc.tier
			req.UnitID = tc.unitID
			req.HardSubcategory = domain.ConstructionSaranaPrasarana // sengaja diisi, harus ditolak

			_, err := svc.CreateCostEntry(context.Background(), 1, req)
			if !errors.Is(err, cost.ErrHardSubcategoryNotAllowed) {
				t.Errorf("category=%s: expected ErrHardSubcategoryNotAllowed, got %v", tc.category, err)
			}
		})
	}
}

// Category != Hard + HardSubcategory kosong → selalu valid (default path).
func TestCostEntry_HardSubcategory_NonHardCategory_Empty_Allowed(t *testing.T) {
	svc, _, _, _, _ := defaultTestService()
	req := baseReq()
	req.Category = domain.CostCategoryLand
	req.HardSubcategory = ""

	_, err := svc.CreateCostEntry(context.Background(), 1, req)
	if err != nil {
		t.Errorf("category land dengan HardSubcategory kosong harus tetap valid, got %v", err)
	}
}

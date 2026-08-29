package cost_test

// Increment 2 — Cost 3-tier (Direct/Shared/Overhead).
// Menguji: inferensi tier (backward compat), matriks tier×kategori, konsistensi
// tier↔unit_id, posting overhead ke akun beban (bukan Persediaan), overhead
// Tenant-level tanpa project, dan preview==create untuk jalur beban.

import (
	"context"
	"errors"
	"testing"

	"esaproperti/internal/cost"
	"esaproperti/internal/domain"
)

// overheadReq adalah request overhead-marketing standar (dengan project sebagai cost center).
func overheadReq() cost.CreateCostEntryRequest {
	r := baseReq()
	r.Category = domain.CostCategoryMarketing
	r.CostTier = domain.CostTierOverhead
	r.Description = "Iklan pemasaran Proyek LITHOS"
	return r
}

// ── Inferensi tier (backward compat — request lama tanpa cost_tier) ───────────

func TestTier_Inference_MatchesBackfillRule(t *testing.T) {
	cases := []struct {
		name     string
		mutate   func(*cost.CreateCostEntryRequest)
		wantTier domain.CostTier
	}{
		{"unit_terisi_jadi_direct", func(r *cost.CreateCostEntryRequest) {
			r.UnitID = ptr64(7)
		}, domain.CostTierDirect},
		{"tanpa_unit_jadi_shared", func(r *cost.CreateCostEntryRequest) {
			r.UnitID = nil
		}, domain.CostTierShared},
		{"kategori_marketing_jadi_overhead", func(r *cost.CreateCostEntryRequest) {
			r.Category = domain.CostCategoryMarketing
		}, domain.CostTierOverhead},
		{"kategori_other_jadi_overhead", func(r *cost.CreateCostEntryRequest) {
			r.Category = domain.CostCategoryOther
		}, domain.CostTierOverhead},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			svc, _, _, store, _ := defaultTestService()
			req := baseReq()
			req.CostTier = "" // tanpa tier eksplisit
			tc.mutate(&req)

			entry, err := svc.CreateCostEntry(context.Background(), 1, req)
			if err != nil {
				t.Fatalf("CreateCostEntry: %v", err)
			}
			if entry.CostTier != tc.wantTier {
				t.Errorf("CostTier = %q, want %q", entry.CostTier, tc.wantTier)
			}
			stored, _ := store.FindCostEntryByID(context.Background(), 1, entry.ID)
			if stored.CostTier != tc.wantTier {
				t.Errorf("stored CostTier = %q, want %q", stored.CostTier, tc.wantTier)
			}
		})
	}
}

// ── Matriks CostTier × CostCategory ───────────────────────────────────────────

func TestTier_CategoryMatrix_InvalidCombos_Rejected(t *testing.T) {
	cases := []struct {
		name     string
		tier     domain.CostTier
		category domain.CostCategory
		unitID   *uint64
	}{
		{"direct_marketing", domain.CostTierDirect, domain.CostCategoryMarketing, ptr64(7)},
		{"direct_other", domain.CostTierDirect, domain.CostCategoryOther, ptr64(7)},
		{"shared_marketing", domain.CostTierShared, domain.CostCategoryMarketing, nil},
		{"shared_other", domain.CostTierShared, domain.CostCategoryOther, nil},
		{"overhead_land", domain.CostTierOverhead, domain.CostCategoryLand, nil},
		{"overhead_hard", domain.CostTierOverhead, domain.CostCategoryHard, nil},
		{"overhead_soft", domain.CostTierOverhead, domain.CostCategorySoft, nil},
		{"overhead_financing", domain.CostTierOverhead, domain.CostCategoryFinancing, nil},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			svc, _, writer, _, _ := defaultTestService()
			req := baseReq()
			req.CostTier = tc.tier
			req.Category = tc.category
			req.UnitID = tc.unitID

			_, err := svc.CreateCostEntry(context.Background(), 1, req)
			if !errors.Is(err, cost.ErrTierCategoryMismatch) {
				t.Errorf("expected ErrTierCategoryMismatch, got %v", err)
			}
			if len(writer.calls) != 0 {
				t.Error("tidak boleh ada jurnal saat kombinasi tier×kategori ditolak")
			}
		})
	}
}

func TestTier_InvalidTier_Rejected(t *testing.T) {
	svc, _, _, _, _ := defaultTestService()
	req := baseReq()
	req.CostTier = "capex" // bukan direct|shared|overhead

	_, err := svc.CreateCostEntry(context.Background(), 1, req)
	if !errors.Is(err, cost.ErrInvalidCostTier) {
		t.Errorf("expected ErrInvalidCostTier, got %v", err)
	}
}

// ── Konsistensi tier ↔ unit_id ────────────────────────────────────────────────

func TestTier_Direct_WithoutUnit_Rejected(t *testing.T) {
	svc, _, _, _, _ := defaultTestService()
	req := baseReq()
	req.CostTier = domain.CostTierDirect
	req.UnitID = nil

	_, err := svc.CreateCostEntry(context.Background(), 1, req)
	if !errors.Is(err, cost.ErrUnitRequiredForDirect) {
		t.Errorf("expected ErrUnitRequiredForDirect, got %v", err)
	}
}

func TestTier_SharedAndOverhead_WithUnit_Rejected(t *testing.T) {
	t.Run("shared", func(t *testing.T) {
		svc, _, _, _, _ := defaultTestService()
		req := baseReq()
		req.CostTier = domain.CostTierShared
		req.UnitID = ptr64(7)
		_, err := svc.CreateCostEntry(context.Background(), 1, req)
		if !errors.Is(err, cost.ErrUnitNotAllowedForTier) {
			t.Errorf("expected ErrUnitNotAllowedForTier, got %v", err)
		}
	})
	t.Run("overhead", func(t *testing.T) {
		svc, _, _, _, _ := defaultTestService()
		req := overheadReq()
		req.UnitID = ptr64(7)
		_, err := svc.CreateCostEntry(context.Background(), 1, req)
		if !errors.Is(err, cost.ErrUnitNotAllowedForTier) {
			t.Errorf("expected ErrUnitNotAllowedForTier, got %v", err)
		}
	})
}

// ── Posting overhead: Dr Beban 5-xxxx (BUKAN Persediaan), Cr Bank/Hutang ─────

func TestTier_Overhead_PostsToExpenseAccount(t *testing.T) {
	cases := []struct {
		category      domain.CostCategory
		wantDebitAcc  uint64 // ID akun beban di standardAccounts
	}{
		{domain.CostCategoryMarketing, 400}, // 5-3000 Beban Pemasaran
		{domain.CostCategoryOther, 401},     // 5-4000 Beban Umum & Administrasi
	}
	for _, tc := range cases {
		tc := tc
		t.Run(string(tc.category), func(t *testing.T) {
			svc, _, writer, _, _ := defaultTestService()
			req := overheadReq()
			req.Category = tc.category

			_, err := svc.CreateCostEntry(context.Background(), 1, req)
			if err != nil {
				t.Fatalf("CreateCostEntry: %v", err)
			}
			call := writer.calls[0]
			debitLine := call.Lines[0]
			if debitLine.AccountID != tc.wantDebitAcc {
				t.Errorf("debit AccountID = %d, want %d (akun beban %s)", debitLine.AccountID, tc.wantDebitAcc, tc.category)
			}
			// Tidak boleh menyentuh akun Persediaan (100-103) — pool HPP bersih.
			for i, l := range call.Lines {
				if l.AccountID >= 100 && l.AccountID <= 103 {
					t.Errorf("line %d menyentuh akun Persediaan (ID %d) — overhead tidak boleh dikapitalisasi", i, l.AccountID)
				}
			}
			// Kredit tetap mengikuti payment method (bank 1-1300 = ID 200).
			creditLine := call.Lines[1]
			if creditLine.AccountID != 200 {
				t.Errorf("credit AccountID = %d, want 200 (bank)", creditLine.AccountID)
			}
		})
	}
}

func TestTier_Overhead_Payable_CreditsHutang(t *testing.T) {
	svc, _, writer, _, _ := defaultTestService()
	req := overheadReq()
	req.PaymentMethod = cost.PaymentMethodPayable
	req.BankAccountCode = ""

	_, err := svc.CreateCostEntry(context.Background(), 1, req)
	if err != nil {
		t.Fatalf("CreateCostEntry: %v", err)
	}
	creditLine := writer.calls[0].Lines[1]
	if creditLine.AccountID != 300 { // 2-1000 Hutang Usaha
		t.Errorf("credit AccountID = %d, want 300 (2-1000 Hutang Usaha)", creditLine.AccountID)
	}
}

func TestTier_Overhead_JournalDescription_Beban(t *testing.T) {
	svc, _, writer, _, _ := defaultTestService()
	if _, err := svc.CreateCostEntry(context.Background(), 1, overheadReq()); err != nil {
		t.Fatalf("CreateCostEntry: %v", err)
	}
	desc := writer.calls[0].Description
	if want := "Beban marketing — Iklan pemasaran Proyek LITHOS"; desc != want {
		t.Errorf("journal description = %q, want %q", desc, want)
	}
}

// ── Overhead Tenant-level (tanpa project) & cost center (dengan project) ─────

func TestTier_Overhead_WithoutProject_Allowed(t *testing.T) {
	svc, _, writer, store, _ := defaultTestService()
	req := overheadReq()
	req.ProjectID = 0 // Tenant-level: gaji kantor pusat, marketing korporat

	entry, err := svc.CreateCostEntry(context.Background(), 1, req)
	if err != nil {
		t.Fatalf("CreateCostEntry: %v", err)
	}
	if entry.ProjectID != nil {
		t.Errorf("ProjectID harus nil untuk overhead Tenant-level, got %v", *entry.ProjectID)
	}
	for i, l := range writer.calls[0].Lines {
		if l.ProjectID != nil {
			t.Errorf("line %d: tag project harus nil untuk overhead Tenant-level, got %v", i, *l.ProjectID)
		}
	}
	stored, _ := store.FindCostEntryByID(context.Background(), 1, entry.ID)
	if stored.ProjectID != nil {
		t.Errorf("stored ProjectID harus nil, got %v", *stored.ProjectID)
	}
}

func TestTier_Overhead_WithProject_TaggedAsCostCenter(t *testing.T) {
	svc, _, writer, _, _ := defaultTestService()
	req := overheadReq()
	req.ProjectID = 42

	entry, err := svc.CreateCostEntry(context.Background(), 1, req)
	if err != nil {
		t.Fatalf("CreateCostEntry: %v", err)
	}
	if entry.ProjectID == nil || *entry.ProjectID != 42 {
		t.Errorf("ProjectID = %v, want 42 (cost center reporting)", entry.ProjectID)
	}
	for i, l := range writer.calls[0].Lines {
		if l.ProjectID == nil || *l.ProjectID != 42 {
			t.Errorf("line %d: tag project = %v, want 42", i, l.ProjectID)
		}
	}
}

func TestTier_Overhead_WithoutProject_PhaseRejected(t *testing.T) {
	svc, _, _, _, _ := defaultTestService()
	req := overheadReq()
	req.ProjectID = 0
	req.PhaseID = ptr64(3)

	_, err := svc.CreateCostEntry(context.Background(), 1, req)
	if !errors.Is(err, cost.ErrPhaseRequiresProject) {
		t.Errorf("expected ErrPhaseRequiresProject, got %v", err)
	}
}

func TestTier_Overhead_WithoutProject_BudgetLinkRejected(t *testing.T) {
	svc, _, _, _, _ := defaultTestService()
	req := overheadReq()
	req.ProjectID = 0
	req.BudgetItemID = ptr64(9)

	_, err := svc.CreateCostEntry(context.Background(), 1, req)
	if !errors.Is(err, cost.ErrBudgetLinkRequiresProject) {
		t.Errorf("expected ErrBudgetLinkRequiresProject, got %v", err)
	}
}

// Direct/shared TIDAK boleh tanpa project (aggregate root rule).
func TestTier_DirectShared_WithoutProject_Rejected(t *testing.T) {
	for _, tier := range []domain.CostTier{domain.CostTierDirect, domain.CostTierShared} {
		tier := tier
		t.Run(string(tier), func(t *testing.T) {
			svc, _, _, _, _ := defaultTestService()
			req := baseReq()
			req.ProjectID = 0
			req.CostTier = tier
			if tier == domain.CostTierDirect {
				req.UnitID = ptr64(7)
			}
			_, err := svc.CreateCostEntry(context.Background(), 1, req)
			if !errors.Is(err, cost.ErrProjectRequired) {
				t.Errorf("expected ErrProjectRequired, got %v", err)
			}
		})
	}
}

// ── Budget link untuk kategori beban (marketing/other realisasi RAB) ──────────

func TestTier_Overhead_BudgetLink_Marketing_OK(t *testing.T) {
	lookup := &mockBudgetItemLookup{items: map[uint64]budgetItemStub{
		11: {projectID: 1, costCategory: domain.CostCategoryMarketing, isMappable: true},
	}}
	svc, writer, store := buildServiceWithLookup(lookup)

	req := overheadReq()
	req.BudgetItemID = ptr64(11)

	entry, err := svc.CreateCostEntry(context.Background(), 1, req)
	if err != nil {
		t.Fatalf("overhead marketing + budget item marketing harus valid: %v", err)
	}
	if entry.BudgetItemID == nil || *entry.BudgetItemID != 11 {
		t.Errorf("BudgetItemID = %v, want 11", entry.BudgetItemID)
	}
	if len(writer.calls) != 1 {
		t.Errorf("jurnal harus dibuat tepat 1 kali, got %d", len(writer.calls))
	}
	stored, _ := store.FindCostEntryByID(context.Background(), 1, entry.ID)
	if stored.CostTier != domain.CostTierOverhead {
		t.Errorf("stored CostTier = %q, want overhead", stored.CostTier)
	}
}

func TestTier_Overhead_BudgetLink_CategoryMismatch_Rejected(t *testing.T) {
	// Budget item marketing, cost entry kategori other → mismatch.
	lookup := &mockBudgetItemLookup{items: map[uint64]budgetItemStub{
		11: {projectID: 1, costCategory: domain.CostCategoryMarketing, isMappable: true},
	}}
	svc, _, _ := buildServiceWithLookup(lookup)

	req := overheadReq()
	req.Category = domain.CostCategoryOther
	req.BudgetItemID = ptr64(11)

	_, err := svc.CreateCostEntry(context.Background(), 1, req)
	if !errors.Is(err, cost.ErrBudgetItemCategoryMismatch) {
		t.Errorf("expected ErrBudgetItemCategoryMismatch, got %v", err)
	}
}

// ── Preview == Create untuk jalur overhead ────────────────────────────────────

func TestTier_Overhead_Preview_MatchesCreate(t *testing.T) {
	svc, finder, writer, _, _ := defaultTestService()
	req := overheadReq()

	previewLines, err := svc.PreviewCostEntry(context.Background(), 1, req)
	if err != nil {
		t.Fatalf("PreviewCostEntry: %v", err)
	}
	if previewLines[0].AccountCode != "5-3000" {
		t.Errorf("preview debit code = %q, want 5-3000", previewLines[0].AccountCode)
	}
	if previewLines[1].AccountCode != "1-1300" {
		t.Errorf("preview credit code = %q, want 1-1300", previewLines[1].AccountCode)
	}

	if _, err := svc.CreateCostEntry(context.Background(), 1, req); err != nil {
		t.Fatalf("CreateCostEntry: %v", err)
	}
	journalLines := writer.calls[0].Lines
	if journalLines[0].AccountID != finder.accounts[previewLines[0].AccountCode] {
		t.Errorf("journal debit AccountID = %d, preview code %q resolves to %d",
			journalLines[0].AccountID, previewLines[0].AccountCode, finder.accounts[previewLines[0].AccountCode])
	}
	if journalLines[1].AccountID != finder.accounts[previewLines[1].AccountCode] {
		t.Errorf("journal credit AccountID = %d, preview code %q resolves to %d",
			journalLines[1].AccountID, previewLines[1].AccountCode, finder.accounts[previewLines[1].AccountCode])
	}
}

// ── Regresi: tier eksplisit direct/shared tetap ke Persediaan ────────────────

func TestTier_ExplicitDirectShared_StillCapitalized(t *testing.T) {
	t.Run("direct", func(t *testing.T) {
		svc, _, writer, _, _ := defaultTestService()
		req := baseReq() // hard
		req.CostTier = domain.CostTierDirect
		req.UnitID = ptr64(7)
		entry, err := svc.CreateCostEntry(context.Background(), 1, req)
		if err != nil {
			t.Fatalf("CreateCostEntry: %v", err)
		}
		if entry.CostTier != domain.CostTierDirect {
			t.Errorf("CostTier = %q, want direct", entry.CostTier)
		}
		if writer.calls[0].Lines[0].AccountID != 101 { // 1-3100 Persediaan Hard
			t.Errorf("debit = %d, want 101 (1-3100)", writer.calls[0].Lines[0].AccountID)
		}
	})
	t.Run("shared", func(t *testing.T) {
		svc, _, writer, _, _ := defaultTestService()
		req := baseReq()
		req.CostTier = domain.CostTierShared
		req.Category = domain.CostCategoryLand
		entry, err := svc.CreateCostEntry(context.Background(), 1, req)
		if err != nil {
			t.Fatalf("CreateCostEntry: %v", err)
		}
		if entry.CostTier != domain.CostTierShared {
			t.Errorf("CostTier = %q, want shared", entry.CostTier)
		}
		if writer.calls[0].Lines[0].AccountID != 100 { // 1-3000 Persediaan Tanah
			t.Errorf("debit = %d, want 100 (1-3000)", writer.calls[0].Lines[0].AccountID)
		}
	})
}

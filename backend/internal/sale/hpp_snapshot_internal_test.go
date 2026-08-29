package sale

import (
	"testing"

	"github.com/shopspring/decimal"

	"esaproperti/internal/domain"
)

// P0-3 — snapshot lines dibangun dari taxonomy (AllCostCategories), bukan string
// kategori hardcoded; Σ lines == HPP total; kode akun sesuai InventoryAccountCode.
func TestSnapshotDraft_ToSnapshot_TaxonomyDriven(t *testing.T) {
	draft := SnapshotDraft{
		ProjectID: 10, UnitID: 42, BudgetPlanID: 7, BudgetPlanVersion: 2, Basis: "saleable_area",
		BasisValue: decimal.NewFromInt(100), AllocationPercentage: decimal.NewFromInt(25),
		Breakdown: domain.UnitCostBreakdown{
			Land: domain.FromInt(100), Hard: domain.FromInt(200),
			Soft: domain.Zero, Financing: domain.FromInt(50),
		},
	}

	snap := draft.toSnapshot(9001, "A-42")

	if snap.TenantID != 9001 || snap.UnitID != 42 || snap.BudgetPlanID != 7 || snap.BudgetPlanVersion != 2 {
		t.Errorf("header salah: %+v", snap)
	}
	// P0-4 Req#1: identitas historis di setiap baris.
	for _, l := range snap.Lines {
		if l.UnitID != 42 || l.UnitNameSnapshot != "A-42" {
			t.Errorf("identitas baris salah: unit=%d nama=%q", l.UnitID, l.UnitNameSnapshot)
		}
	}
	if snap.HPPTotal.String() != "350" {
		t.Errorf("hpp_total: got %s, want 350", snap.HPPTotal)
	}
	// Satu baris per accounting_class (4), termasuk yang nol (soft) — snapshot lengkap.
	if len(snap.Lines) != len(domain.AllCostCategories) {
		t.Fatalf("jumlah baris: got %d, want %d", len(snap.Lines), len(domain.AllCostCategories))
	}

	want := map[string]struct {
		amount string
		code   string
	}{
		"land":      {"100", "1-3000"},
		"hard":      {"200", "1-3100"},
		"soft":      {"0", "1-3200"},
		"financing": {"50", "1-3300"},
	}
	var sum domain.Money
	for _, ln := range snap.Lines {
		w, ok := want[ln.AccountingClass]
		if !ok {
			t.Errorf("class tak dikenal: %s", ln.AccountingClass)
			continue
		}
		if ln.Amount.String() != w.amount {
			t.Errorf("%s amount: got %s, want %s", ln.AccountingClass, ln.Amount, w.amount)
		}
		if ln.InventoryAccountCode != w.code {
			t.Errorf("%s account: got %s, want %s", ln.AccountingClass, ln.InventoryAccountCode, w.code)
		}
		if ln.TenantID != 9001 {
			t.Errorf("%s tenant: got %d, want 9001", ln.AccountingClass, ln.TenantID)
		}
		// Bukti basis didenormalisasi ke SETIAP baris (self-describing).
		if ln.BasisType != "saleable_area" || ln.BasisValue.String() != "100" || !ln.AllocationPercentage.Equal(decimal.NewFromInt(25)) {
			t.Errorf("%s bukti basis salah: type=%s value=%s pct=%s", ln.AccountingClass, ln.BasisType, ln.BasisValue, ln.AllocationPercentage)
		}
		sum = sum.Add(ln.Amount)
	}
	// BCA-1 (level unit): Σ lines == HPP total.
	if sum.String() != snap.HPPTotal.String() {
		t.Errorf("Σ lines (%s) != hpp_total (%s)", sum, snap.HPPTotal)
	}
}

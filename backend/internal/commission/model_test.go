package commission_test

import (
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"esaproperti/internal/commission"
	"esaproperti/internal/domain"
)

func TestStatus_Machine(t *testing.T) {
	legal := [][2]commission.Status{
		{commission.StatusCalculated, commission.StatusApproved},
		{commission.StatusCalculated, commission.StatusCancelled},
		{commission.StatusApproved, commission.StatusPayable},
		{commission.StatusApproved, commission.StatusCancelled},
		{commission.StatusPayable, commission.StatusPaid},
		{commission.StatusPayable, commission.StatusCancelled},
		{commission.StatusPaid, commission.StatusClawedBack},
	}
	for _, p := range legal {
		if !p[0].CanTransitionTo(p[1]) {
			t.Errorf("expected LEGAL: %s → %s", p[0], p[1])
		}
	}
	illegal := [][2]commission.Status{
		{commission.StatusCalculated, commission.StatusPayable}, // skip approve
		{commission.StatusCalculated, commission.StatusPaid},
		{commission.StatusApproved, commission.StatusPaid}, // skip payable (akrual)
		{commission.StatusPaid, commission.StatusCancelled}, // paid hanya clawback
		{commission.StatusCancelled, commission.StatusApproved},
		{commission.StatusClawedBack, commission.StatusPaid},
	}
	for _, p := range illegal {
		if p[0].CanTransitionTo(p[1]) {
			t.Errorf("expected ILLEGAL: %s → %s", p[0], p[1])
		}
	}
}

func TestRule_Matches(t *testing.T) {
	sp := uint64(7)
	proj := uint64(3)
	vt := "villa"
	from := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)
	r := &commission.Rule{
		IsActive: true, EffectiveFrom: from, EffectiveTo: &to,
		SalesPersonID: &sp, ProjectID: &proj, UnitType: &vt,
	}
	mid := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	if !r.Matches(7, 3, "villa", mid) {
		t.Error("scope penuh cocok harus match")
	}
	cases := []struct {
		name              string
		sp, proj          uint64
		ut                string
		date              time.Time
	}{
		{"salesperson beda", 8, 3, "villa", mid},
		{"proyek beda", 7, 4, "villa", mid},
		{"tipe beda", 7, 3, "ruko", mid},
		{"sebelum efektif", 7, 3, "villa", time.Date(2025, 12, 1, 0, 0, 0, 0, time.UTC)},
		{"setelah berakhir", 7, 3, "villa", time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)},
	}
	for _, tc := range cases {
		if r.Matches(tc.sp, tc.proj, tc.ut, tc.date) {
			t.Errorf("%s: harus TIDAK match", tc.name)
		}
	}
	// Rule nonaktif tidak pernah match.
	r.IsActive = false
	if r.Matches(7, 3, "villa", mid) {
		t.Error("rule nonaktif tidak boleh match")
	}
	// Scope NULL = semua.
	open := &commission.Rule{IsActive: true, EffectiveFrom: from}
	if !open.Matches(99, 99, "apapun", mid) {
		t.Error("scope NULL harus match semua")
	}
}

func TestRule_AmountFor(t *testing.T) {
	pct := &commission.Rule{Basis: commission.BasisPercentOfSale, Rate: decimal.RequireFromString("0.025")}
	got, err := pct.AmountFor(domain.FromInt(2_000_000_000))
	if err != nil || got.String() != "50000000" {
		t.Errorf("percent: got %s err %v, want 50000000", got, err)
	}
	// Pembulatan rupiah bulat (Invariant #2): 0.025 × 1.000.001 = 25.000,025 → 25000.
	got, _ = pct.AmountFor(domain.FromInt(1_000_001))
	if got.String() != "25000" {
		t.Errorf("rounding: got %s, want 25000", got)
	}
	flat := &commission.Rule{Basis: commission.BasisFlatPerUnit, FlatAmount: domain.FromInt(10_000_000)}
	got, _ = flat.AmountFor(domain.FromInt(999))
	if got.String() != "10000000" {
		t.Errorf("flat: got %s", got)
	}
	tiered := &commission.Rule{Basis: commission.BasisTiered}
	if _, err := tiered.AmountFor(domain.FromInt(1)); err == nil {
		t.Error("tiered harus ErrBasisNotImplemented (seam)")
	}
}

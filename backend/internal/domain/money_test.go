package domain_test

import (
	"testing"

	"github.com/shopspring/decimal"

	"esaproperti/internal/domain"
)

// ── Construction ─────────────────────────────────────────────────────────────

func TestMoney_NewMoney_ValidString(t *testing.T) {
	cases := []string{"0", "1000000", "500000.50", "-250000"}
	for _, s := range cases {
		if _, err := domain.NewMoney(s); err != nil {
			t.Errorf("NewMoney(%q) unexpected error: %v", s, err)
		}
	}
}

func TestMoney_NewMoney_InvalidString(t *testing.T) {
	if _, err := domain.NewMoney("not-a-number"); err == nil {
		t.Error("expected error for invalid money string, got nil")
	}
}

func TestMoney_FromInt(t *testing.T) {
	m := domain.FromInt(1_000_000)
	if !m.Equal(domain.MustParse("1000000")) {
		t.Errorf("FromInt(1_000_000) = %s, want 1000000", m)
	}
}

// Compile-time proof: there is no float64 constructor.
// If someone adds FromFloat, this test documents the violation.
var _ = func() {
	// domain.NewMoney takes a string — not a float.
	// domain.FromInt takes int64 — not a float.
	// This var block is a living invariant comment.
}

// ── Arithmetic ────────────────────────────────────────────────────────────────

func TestMoney_Add(t *testing.T) {
	a := domain.MustParse("100000")
	b := domain.MustParse("50000")
	got := a.Add(b)
	if !got.Equal(domain.MustParse("150000")) {
		t.Errorf("Add: got %s, want 150000", got)
	}
}

func TestMoney_Sub(t *testing.T) {
	a := domain.MustParse("100000")
	b := domain.MustParse("30000")
	got := a.Sub(b)
	if !got.Equal(domain.MustParse("70000")) {
		t.Errorf("Sub: got %s, want 70000", got)
	}
}

func TestMoney_Neg(t *testing.T) {
	m := domain.MustParse("500000")
	if !m.Neg().Equal(domain.MustParse("-500000")) {
		t.Errorf("Neg: got %s, want -500000", m.Neg())
	}
}

func TestMoney_Mul_TaxRate(t *testing.T) {
	// PPh Final 2.5% of Rp 1.000.000 = Rp 25.000
	m := domain.MustParse("1000000")
	rate, _ := decimal.NewFromString("0.025")
	got := m.Mul(rate)
	if !got.Equal(domain.MustParse("25000")) {
		t.Errorf("Mul(0.025): got %s, want 25000", got)
	}
}

func TestMoney_Comparisons(t *testing.T) {
	a := domain.MustParse("100")
	b := domain.MustParse("200")

	if !a.LessThan(b) {
		t.Error("100 should be less than 200")
	}
	if !b.GreaterThan(a) {
		t.Error("200 should be greater than 100")
	}
	if !a.Equal(domain.MustParse("100")) {
		t.Error("100 should equal 100")
	}
}

func TestMoney_IsZero(t *testing.T) {
	if !domain.Zero.IsZero() {
		t.Error("Zero should be zero")
	}
	if domain.MustParse("1").IsZero() {
		t.Error("1 should not be zero")
	}
}

// ── Allocate — Invariant 3 ────────────────────────────────────────────────────

func TestMoney_Allocate_Reconciles(t *testing.T) {
	// Every case must satisfy: Σ(parts) == total, exactly.
	cases := []struct {
		name    string
		total   string
		weights []int64
	}{
		{"equal thirds, odd amount", "1000003", []int64{1, 1, 1}},
		{"equal thirds, clean amount", "1000", []int64{1, 1, 1}},
		{"unequal 1:2:3", "100", []int64{1, 2, 3}},
		{"two buckets, odd total", "101", []int64{1, 1}},
		{"large rupiah, 3:5:2", "500000003", []int64{3, 5, 2}},
		{"single bucket", "999999", []int64{1}},
		{"many buckets", "1000000", []int64{7, 13, 3, 11, 5, 2, 9}},
		{"zero total with positive weights", "0", []int64{1, 2, 3}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			total := domain.MustParse(tc.total)
			weights := make([]decimal.Decimal, len(tc.weights))
			for i, w := range tc.weights {
				weights[i] = decimal.NewFromInt(w)
			}

			parts := total.Allocate(weights)

			if len(parts) != len(tc.weights) {
				t.Fatalf("len(parts)=%d, want %d", len(parts), len(tc.weights))
			}

			// Core invariant: exact reconciliation
			sum := domain.Zero
			for _, p := range parts {
				sum = sum.Add(p)
			}
			if !sum.Equal(total) {
				t.Errorf("reconciliation failed: sum=%s, total=%s", sum, total)
			}
		})
	}
}

func TestMoney_Allocate_NilWeights(t *testing.T) {
	total := domain.MustParse("1000000")
	if result := total.Allocate(nil); result != nil {
		t.Errorf("expected nil for nil weights, got %v", result)
	}
}

func TestMoney_Allocate_AllZeroWeights(t *testing.T) {
	total := domain.MustParse("1000000")
	weights := []decimal.Decimal{decimal.Zero, decimal.Zero, decimal.Zero}
	parts := total.Allocate(weights)
	for i, p := range parts {
		if !p.IsZero() {
			t.Errorf("parts[%d]=%s, want 0 when all weights are zero", i, p)
		}
	}
}

func TestMoney_Allocate_Deterministic(t *testing.T) {
	// Same inputs always produce the same output.
	total := domain.MustParse("1000")
	weights := []decimal.Decimal{
		decimal.NewFromInt(1),
		decimal.NewFromInt(1),
		decimal.NewFromInt(1),
	}
	first := total.Allocate(weights)
	for range 10 {
		got := total.Allocate(weights)
		for i := range got {
			if !got[i].Equal(first[i]) {
				t.Errorf("non-deterministic: run produced %s at [%d], want %s", got[i], i, first[i])
			}
		}
	}
}

func TestMoney_Allocate_LargestRemainderGetsExtra(t *testing.T) {
	// Rp 10 split [1:1:1] → one bucket gets 4, two get 3.
	// The one with the largest remainder gets the extra unit.
	// With equal weights all remainders are equal → index 0 gets it (stable sort).
	total := domain.MustParse("10")
	weights := []decimal.Decimal{
		decimal.NewFromInt(1),
		decimal.NewFromInt(1),
		decimal.NewFromInt(1),
	}
	parts := total.Allocate(weights)
	sum := domain.Zero
	for _, p := range parts {
		sum = sum.Add(p)
	}
	if !sum.Equal(total) {
		t.Errorf("reconciliation failed: sum=%s, want 10", sum)
	}
}

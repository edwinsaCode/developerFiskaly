package domain

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/shopspring/decimal"
)

// Money is an immutable value object for currency amounts.
// Always construct via NewMoney or FromInt — never from float64.
type Money struct {
	amount decimal.Decimal
}

// Zero is the zero-value Money (Rp 0).
var Zero = Money{}

// NewMoney constructs Money from a string representation (e.g. "1000000" or "1500000.50").
func NewMoney(s string) (Money, error) {
	d, err := decimal.NewFromString(s)
	if err != nil {
		return Money{}, fmt.Errorf("invalid money value %q: %w", s, err)
	}
	return Money{amount: d}, nil
}

// MustParse constructs Money from a string, panicking on error.
// Use only in tests or package-level constants — never in production code paths.
func MustParse(s string) Money {
	m, err := NewMoney(s)
	if err != nil {
		panic(fmt.Sprintf("domain.MustParse(%q): %v", s, err))
	}
	return m
}

// FromInt constructs Money from an integer (e.g. 1_000_000 for Rp 1.000.000).
func FromInt(n int64) Money {
	return Money{amount: decimal.NewFromInt(n)}
}

// FromDecimal constructs Money from a decimal.Decimal.
// For internal use only (e.g. GORM scanning). Do not use for arithmetic in services.
func FromDecimal(d decimal.Decimal) Money {
	return Money{amount: d}
}

func (m Money) Add(other Money) Money    { return Money{m.amount.Add(other.amount)} }
func (m Money) Sub(other Money) Money    { return Money{m.amount.Sub(other.amount)} }
func (m Money) Neg() Money               { return Money{m.amount.Neg()} }
func (m Money) IsZero() bool             { return m.amount.IsZero() }
func (m Money) IsNeg() bool              { return m.amount.IsNegative() }
func (m Money) Equal(o Money) bool       { return m.amount.Equal(o.amount) }
func (m Money) LessThan(o Money) bool    { return m.amount.LessThan(o.amount) }
func (m Money) GreaterThan(o Money) bool { return m.amount.GreaterThan(o.amount) }
func (m Money) String() string           { return m.amount.String() }
func (m Money) Decimal() decimal.Decimal { return m.amount }

// IsWholeRupiah returns true if m has no fractional sen (Invariant #2).
// Zero is always whole. The check is robust across all decimal representations.
func (m Money) IsWholeRupiah() bool {
	return m.amount.Equal(m.amount.Truncate(0))
}

// Mul returns m * ratio (for tax rates, percentage-based calculations).
// ratio is a plain decimal, not a Money — e.g. decimal.NewFromFloat(0.025) for 2.5%.
func (m Money) Mul(ratio decimal.Decimal) Money {
	return Money{m.amount.Mul(ratio)}
}

// MarshalJSON serializes Money as a JSON string (e.g. "1000000").
func (m Money) MarshalJSON() ([]byte, error) {
	return json.Marshal(m.amount.String())
}

// UnmarshalJSON deserializes Money from a JSON string or number.
func (m *Money) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		var n json.Number
		if err2 := json.Unmarshal(data, &n); err2 != nil {
			return fmt.Errorf("unmarshal Money: %w", err)
		}
		s = string(n)
	}
	d, err := decimal.NewFromString(s)
	if err != nil {
		return fmt.Errorf("invalid money %q: %w", s, err)
	}
	m.amount = d
	return nil
}

// Value implements driver.Valuer so domain.Money can be stored directly by GORM.
// The underlying decimal is stored as DECIMAL(20,4) in MySQL.
func (m Money) Value() (driver.Value, error) {
	return m.amount.Value()
}

// Scan implements sql.Scanner so GORM can read DECIMAL columns directly into Money.
func (m *Money) Scan(src interface{}) error {
	return m.amount.Scan(src)
}

// Allocate splits m among len(weights) buckets proportional to weights,
// using the largest-remainder method to guarantee sum(result) == m exactly.
//
// Invariant 3: Σ(result[i]) == m, to the last rupiah.
//
// weights must be non-negative. If all weights are zero, all parts are zero.
// Result order matches weights order.
func (m Money) Allocate(weights []decimal.Decimal) []Money {
	n := len(weights)
	if n == 0 {
		return nil
	}

	totalWeight := decimal.Zero
	for _, w := range weights {
		totalWeight = totalWeight.Add(w)
	}

	// Degenerate case: no positive weight, return all zeros.
	if totalWeight.IsZero() {
		return make([]Money, n)
	}

	// Work in whole rupiah (0 decimal places).
	// Change scale to 2 if sub-rupiah precision is ever required.
	unit := decimal.NewFromInt(1)

	exact := make([]decimal.Decimal, n)
	floored := make([]decimal.Decimal, n)
	remainders := make([]decimal.Decimal, n)
	sumFloored := decimal.Zero

	for i, w := range weights {
		exact[i] = m.amount.Mul(w).Div(totalWeight)
		floored[i] = exact[i].Floor()
		remainders[i] = exact[i].Sub(floored[i])
		sumFloored = sumFloored.Add(floored[i])
	}

	// Number of extra whole-rupiah units to distribute.
	extraUnits := m.amount.Sub(sumFloored).Div(unit).IntPart()

	// Sort bucket indices by remainder descending (largest-remainder first).
	// SliceStable ensures fully deterministic output when remainders are equal.
	idxs := make([]int, n)
	for i := range idxs {
		idxs[i] = i
	}
	sort.SliceStable(idxs, func(a, b int) bool {
		return remainders[idxs[a]].GreaterThan(remainders[idxs[b]])
	})

	result := make([]Money, n)
	for i, f := range floored {
		result[i] = Money{f}
	}
	for i := int64(0); i < extraUnits && int(i) < n; i++ {
		idx := idxs[i]
		result[idx] = Money{result[idx].amount.Add(unit)}
	}
	return result
}

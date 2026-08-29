package tax

// TaxFormula — SEAM perhitungan pajak (hardening pasca-review Increment 4).
// TaxRuleResolver MEMILIH rule; TaxFormula MENGHITUNG nominal dari rule + basis.
// Formula baru (progressive, threshold, fixed_amount, exemption) = satu
// implementasi + satu Register — resolver & business flow TIDAK berubah.

import (
	"fmt"

	"esaproperti/internal/domain"
)

// FormulaType adalah identitas formula (nilai kanonik bertipe — SSOT).
type FormulaType string

const (
	// FormulaProportional: tax = round(base × rate) — satu-satunya yang
	// terimplementasi saat ini (PPh Final proporsional).
	FormulaProportional FormulaType = "proportional"
	// Vocabulary masa depan (CHECK di DB sudah menampungnya; implementasi
	// menyusul sebagai strategy baru):
	FormulaProgressive FormulaType = "progressive"
	FormulaThreshold   FormulaType = "threshold"
	FormulaFixedAmount FormulaType = "fixed_amount"
	FormulaExemption   FormulaType = "exemption"
)

func (f FormulaType) Valid() bool {
	switch f {
	case FormulaProportional, FormulaProgressive, FormulaThreshold, FormulaFixedAmount, FormulaExemption:
		return true
	}
	return false
}

// TaxFormula menghitung nominal pajak dari rule + basis pengenaan.
// Pure domain: tanpa DB, tanpa side effect. Hasil WAJIB rupiah bulat
// (Invariant #2) dan non-negatif.
type TaxFormula interface {
	FormulaType() FormulaType
	Compute(rule *TaxRate, base domain.Money) (domain.Money, error)
}

// ProportionalFormula: tax = round(base × rule.Rate, 0).
type ProportionalFormula struct{}

func (ProportionalFormula) FormulaType() FormulaType { return FormulaProportional }

func (ProportionalFormula) Compute(rule *TaxRate, base domain.Money) (domain.Money, error) {
	return domain.FromDecimal(base.Decimal().Mul(rule.Rate).Round(0)), nil
}

// ── Registry (pola scheme.Registry) ───────────────────────────────────────────

type FormulaRegistry struct {
	formulas map[FormulaType]TaxFormula
}

func NewFormulaRegistry(formulas ...TaxFormula) *FormulaRegistry {
	m := make(map[FormulaType]TaxFormula, len(formulas))
	for _, f := range formulas {
		m[f.FormulaType()] = f
	}
	return &FormulaRegistry{formulas: m}
}

// DefaultFormulaRegistry berisi formula terimplementasi saat ini.
func DefaultFormulaRegistry() *FormulaRegistry {
	return NewFormulaRegistry(ProportionalFormula{})
}

// Formula mengembalikan strategy untuk sebuah FormulaType; kosong = proportional
// (baris rule legacy sebelum kolom formula ada).
func (r *FormulaRegistry) Formula(t FormulaType) (TaxFormula, error) {
	if t == "" {
		t = FormulaProportional
	}
	f, ok := r.formulas[t]
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrFormulaNotImplemented, t)
	}
	return f, nil
}

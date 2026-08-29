package tax_test

// Increment 4 — Tax Rule konfiguratif (unit, in-memory).
// Menguji jalur resolusi rule di AccrueTax: tarif per kategori proyek, akun
// jurnal dari KONFIGURASI rule (bukan hardcode), provenance di obligation,
// default konservatif, dan validasi SetTaxRate yang diperluas.

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"esaproperti/internal/domain"
	"esaproperti/internal/tax"
)

// ── Mock resolver & category reader ──────────────────────────────────────────

type mockRuleResolver struct {
	// rules per kategori; key "" dipakai bila kategori tak terdaftar (fallback all)
	byCategory map[domain.TaxCategory]*tax.TaxRate
	lastCat    domain.TaxCategory
}

func (m *mockRuleResolver) ResolveRule(_ context.Context, _ uint64, _ string, cat domain.TaxCategory, _ time.Time) (*tax.TaxRate, error) {
	m.lastCat = cat
	if r, ok := m.byCategory[cat]; ok {
		return r, nil
	}
	if r, ok := m.byCategory[""]; ok {
		return r, nil
	}
	return nil, tax.ErrRateNotConfigured
}

type mockCategoryReader struct {
	categories map[uint64]domain.TaxCategory
}

func (m *mockCategoryReader) GetProjectTaxCategory(_ context.Context, _ uint64, projectID uint64) (domain.TaxCategory, error) {
	c, ok := m.categories[projectID]
	if !ok {
		return "", tax.ErrProjectNotFoundForTax
	}
	return c, nil
}

func ruleFor(id uint64, rate float64, appliesTo tax.RuleAppliesTo, dr, cr string) *tax.TaxRate {
	return &tax.TaxRate{
		ID:            id,
		RateCode:      tax.RateCodePPhFinalPengalihan,
		Rate:          decimal.NewFromFloat(rate),
		AppliesTo:     appliesTo,
		DebitAccount:  dr,
		CreditAccount: cr,
	}
}

func ptrU64(v uint64) *uint64 { return &v }

// ── AccrueTax via rule resolution ─────────────────────────────────────────────

func TestAccrueTax_RuleResolution_SubsidiVsKomersial(t *testing.T) {
	// Akun beban/hutang khusus dari KONFIGURASI rule (bukan 5-2000/2-4000).
	accounts := map[string]uint64{"5-2100": 910, "2-4100": 920}
	resolver := &mockRuleResolver{byCategory: map[domain.TaxCategory]*tax.TaxRate{
		domain.TaxCategorySubsidi:   ruleFor(11, 0.01, tax.AppliesToSubsidi, "5-2100", "2-4100"),
		domain.TaxCategoryKomersial: ruleFor(12, 0.025, tax.AppliesToKomersial, "5-2100", "2-4100"),
	}}
	categories := &mockCategoryReader{categories: map[uint64]domain.TaxCategory{
		100: domain.TaxCategorySubsidi,
		200: domain.TaxCategoryKomersial,
	}}

	cases := []struct {
		name       string
		projectID  uint64
		wantTax    int64
		wantRuleID uint64
		wantScope  string
	}{
		{"subsidi_1_persen", 100, 5_000_000, 11, "subsidi"},     // 500jt × 1%
		{"komersial_2_5_persen", 200, 12_500_000, 12, "komersial"}, // 500jt × 2,5%
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			jw := &mockJournalWriter{}
			store := &mockTaxStore{}
			svc := buildService(nil, accounts, jw, store, tax.WithRuleResolution(resolver, categories))

			obl, err := svc.AccrueTax(context.Background(), 1, tax.AccrueTaxRequest{
				TransferValue: domain.FromInt(500_000_000),
				AccrualDate:   time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
				ProjectID:     ptrU64(tc.projectID),
			})
			if err != nil {
				t.Fatalf("AccrueTax: %v", err)
			}
			if obl.TaxAmount.String() != domain.FromInt(tc.wantTax).String() {
				t.Errorf("tax = %s, want %d", obl.TaxAmount, tc.wantTax)
			}
			// Provenance rule tercatat (audit "kenapa 1%?").
			if obl.TaxRuleID == nil || *obl.TaxRuleID != tc.wantRuleID {
				t.Errorf("tax_rule_id = %v, want %d", obl.TaxRuleID, tc.wantRuleID)
			}
			if obl.AppliesTo != tc.wantScope {
				t.Errorf("applies_to = %q, want %q", obl.AppliesTo, tc.wantScope)
			}
			// Jurnal memakai akun dari KONFIGURASI rule.
			lines := jw.captured[0].lines
			if lines[0].AccountID != 910 || lines[1].AccountID != 920 {
				t.Errorf("jurnal harus Dr 5-2100(910)/Cr 2-4100(920), got Dr %d/Cr %d",
					lines[0].AccountID, lines[1].AccountID)
			}
		})
	}
}

// Tanpa project → kategori default KONSERVATIF (komersial, tarif tertinggi).
func TestAccrueTax_RuleResolution_NoProject_DefaultsKomersial(t *testing.T) {
	accounts := map[string]uint64{"5-2000": 900, "2-4000": 901}
	resolver := &mockRuleResolver{byCategory: map[domain.TaxCategory]*tax.TaxRate{
		domain.TaxCategoryKomersial: ruleFor(12, 0.025, tax.AppliesToKomersial, "", ""), // akun kosong = default legacy
	}}
	svc := buildService(nil, accounts, nil, nil, tax.WithRuleResolution(resolver, &mockCategoryReader{}))

	obl, err := svc.AccrueTax(context.Background(), 1, tax.AccrueTaxRequest{
		TransferValue: domain.FromInt(100_000_000),
		AccrualDate:   time.Now(),
	})
	if err != nil {
		t.Fatalf("AccrueTax: %v", err)
	}
	if resolver.lastCat != domain.TaxCategoryKomersial {
		t.Errorf("kategori default = %s, want komersial (konservatif)", resolver.lastCat)
	}
	if obl.TaxAmount.String() != "2500000" {
		t.Errorf("tax = %s, want 2500000", obl.TaxAmount)
	}
}

// Rule tanpa akun (baris legacy) → fallback default terpusat 5-2000/2-4000.
func TestAccrueTax_LegacyRuleAccounts_Default(t *testing.T) {
	accounts := map[string]uint64{"5-2000": 900, "2-4000": 901}
	resolver := &mockRuleResolver{byCategory: map[domain.TaxCategory]*tax.TaxRate{
		"": ruleFor(9, 0.025, tax.AppliesToAll, "", ""),
	}}
	categories := &mockCategoryReader{categories: map[uint64]domain.TaxCategory{7: domain.TaxCategoryKomersial}}
	jw := &mockJournalWriter{}
	svc := buildService(nil, accounts, jw, nil, tax.WithRuleResolution(resolver, categories))

	if _, err := svc.AccrueTax(context.Background(), 1, tax.AccrueTaxRequest{
		TransferValue: domain.FromInt(100_000_000), AccrualDate: time.Now(), ProjectID: ptrU64(7),
	}); err != nil {
		t.Fatalf("AccrueTax: %v", err)
	}
	lines := jw.captured[0].lines
	if lines[0].AccountID != 900 || lines[1].AccountID != 901 {
		t.Errorf("akun default legacy harus 5-2000/2-4000, got %d/%d", lines[0].AccountID, lines[1].AccountID)
	}
}

// ── SetTaxRate validasi field baru ────────────────────────────────────────────

func TestSetTaxRate_RuleFieldValidation(t *testing.T) {
	svc := buildService(nil, standardAccounts, nil, nil)
	base := tax.SetTaxRateRequest{
		RateCode:      tax.RateCodePPhFinalPengalihan,
		Rate:          decimal.NewFromFloat(0.01),
		EffectiveFrom: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}

	t.Run("applies_to_tak_dikenal", func(t *testing.T) {
		req := base
		req.AppliesTo = "vip"
		if err := svc.SetTaxRate(context.Background(), 1, req); !errors.Is(err, tax.ErrAppliesToInvalid) {
			t.Errorf("expected ErrAppliesToInvalid, got %v", err)
		}
	})
	t.Run("effective_to_sebelum_from", func(t *testing.T) {
		req := base
		before := base.EffectiveFrom.AddDate(0, -1, 0)
		req.EffectiveTo = &before
		if err := svc.SetTaxRate(context.Background(), 1, req); !errors.Is(err, tax.ErrEffectiveRangeInvalid) {
			t.Errorf("expected ErrEffectiveRangeInvalid, got %v", err)
		}
	})
	t.Run("akun_sebelah_saja", func(t *testing.T) {
		req := base
		req.DebitAccount = "5-2100" // credit kosong
		if err := svc.SetTaxRate(context.Background(), 1, req); !errors.Is(err, tax.ErrRuleAccountsIncomplete) {
			t.Errorf("expected ErrRuleAccountsIncomplete, got %v", err)
		}
	})
	t.Run("subsidi_valid", func(t *testing.T) {
		req := base
		req.AppliesTo = tax.AppliesToSubsidi
		req.DebitAccount, req.CreditAccount = "5-2000", "2-4000"
		if err := svc.SetTaxRate(context.Background(), 1, req); err != nil {
			t.Errorf("rule subsidi valid harus tersimpan, got %v", err)
		}
	})
}

// ── Hardening pasca-review: revision + trigger enum + formula seam ────────────

func TestAccrueTax_RecordsRuleRevision(t *testing.T) {
	accounts := map[string]uint64{"5-2000": 900, "2-4000": 901}
	rule := ruleFor(11, 0.01, tax.AppliesToSubsidi, "", "")
	rule.Revision = 3
	resolver := &mockRuleResolver{byCategory: map[domain.TaxCategory]*tax.TaxRate{
		domain.TaxCategorySubsidi: rule,
	}}
	categories := &mockCategoryReader{categories: map[uint64]domain.TaxCategory{5: domain.TaxCategorySubsidi}}
	svc := buildService(nil, accounts, nil, nil, tax.WithRuleResolution(resolver, categories))

	obl, err := svc.AccrueTax(context.Background(), 1, tax.AccrueTaxRequest{
		TransferValue: domain.FromInt(100_000_000), AccrualDate: time.Now(), ProjectID: ptrU64(5),
	})
	if err != nil {
		t.Fatalf("AccrueTax: %v", err)
	}
	if obl.TaxRuleRevision == nil || *obl.TaxRuleRevision != 3 {
		t.Errorf("tax_rule_revision = %v, want 3 (audit eksplisit revisi konfigurasi)", obl.TaxRuleRevision)
	}
}

func TestSetTaxRate_TriggerAndFormulaValidation(t *testing.T) {
	svc := buildService(nil, standardAccounts, nil, nil)
	base := tax.SetTaxRateRequest{
		RateCode:      tax.RateCodePPhFinalPengalihan,
		Rate:          decimal.NewFromFloat(0.01),
		EffectiveFrom: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}

	t.Run("trigger_tak_dikenal", func(t *testing.T) {
		req := base
		req.TriggerEvent = "monthly" // bukan vocabulary
		if err := svc.SetTaxRate(context.Background(), 1, req); !errors.Is(err, tax.ErrTriggerEventInvalid) {
			t.Errorf("expected ErrTriggerEventInvalid, got %v", err)
		}
	})
	t.Run("formula_tak_dikenal", func(t *testing.T) {
		req := base
		req.Formula = "magic"
		if err := svc.SetTaxRate(context.Background(), 1, req); !errors.Is(err, tax.ErrFormulaInvalid) {
			t.Errorf("expected ErrFormulaInvalid, got %v", err)
		}
	})
	t.Run("formula_vocabulary_sah_tapi_belum_terimplementasi", func(t *testing.T) {
		req := base
		req.Formula = tax.FormulaProgressive // ada di vocabulary, belum ada di registry
		if err := svc.SetTaxRate(context.Background(), 1, req); !errors.Is(err, tax.ErrFormulaNotImplemented) {
			t.Errorf("expected ErrFormulaNotImplemented, got %v", err)
		}
	})
	t.Run("default_bast_proportional_valid", func(t *testing.T) {
		if err := svc.SetTaxRate(context.Background(), 1, base); err != nil {
			t.Errorf("default (bast + proportional) harus sah, got %v", err)
		}
	})
}

func TestFormulaRegistry_Proportional(t *testing.T) {
	reg := tax.DefaultFormulaRegistry()
	f, err := reg.Formula("") // kosong = proportional (baris legacy)
	if err != nil {
		t.Fatalf("formula kosong harus default proportional: %v", err)
	}
	rule := &tax.TaxRate{Rate: decimal.NewFromFloat(0.025)}
	got, err := f.Compute(rule, domain.FromInt(999_999_999))
	if err != nil {
		t.Fatal(err)
	}
	// 999.999.999 × 2,5% = 24.999.999,975 → bulat 25.000.000 (Invariant #2).
	if got.String() != "25000000" {
		t.Errorf("proportional = %s, want 25000000 (rupiah bulat)", got)
	}
	if !got.IsWholeRupiah() {
		t.Errorf("hasil formula wajib rupiah bulat")
	}
}

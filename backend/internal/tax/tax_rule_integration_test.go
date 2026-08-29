//go:build integration

package tax_test

// Increment 4 — integration Tax Rule (real MySQL).
// Membuktikan: resolusi rule per kategori proyek (specificity subsidi/komersial
// > all), effective dating (from/to), inactive di-skip, akrual BAST atomik
// (AccruePPhFinalInTx) memakai tarif + akun dari rule, provenance tersimpan,
// dan perubahan rule TIDAK menyentuh obligation lama (snapshot).
// Prasyarat: TEST_DB_DSN + DB termigrasi (≥ 000039).

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"esaproperti/internal/domain"
	"esaproperti/internal/ledger"
	"esaproperti/internal/tax"
)

const trTenant uint64 = 9_900_006

func trConnect(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := os.Getenv("TEST_DB_DSN")
	if dsn == "" {
		t.Skip("TEST_DB_DSN tidak di-set — lewati")
	}
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("koneksi DB: %v", err)
	}
	return db
}

func trCleanup(t *testing.T, db *gorm.DB) {
	t.Helper()
	for _, tbl := range []string{
		"tax_payments", "tax_obligations", "tax_rates",
		"journal_lines", "journal_entries", "projects", "accounts",
	} {
		if err := db.Exec("DELETE FROM "+tbl+" WHERE tenant_id = ?", trTenant).Error; err != nil {
			t.Fatalf("cleanup %s: %v", tbl, err)
		}
	}
}

func trSetup(t *testing.T) (*gorm.DB, *tax.GORMRepository) {
	t.Helper()
	db := trConnect(t)
	trCleanup(t, db)
	t.Cleanup(func() { trCleanup(t, db) })
	ctx := context.Background()

	if err := ledger.SeedCOA(ctx, db, trTenant); err != nil {
		t.Fatalf("seed COA: %v", err)
	}
	if err := tax.SeedDefaultRates(ctx, db, trTenant); err != nil {
		t.Fatalf("seed rates: %v", err)
	}
	ledgerRepo := ledger.NewGORMRepository(db)
	posting := ledger.NewPostingService(ledgerRepo, ledgerRepo)
	return db, tax.NewGORMRepository(db, posting)
}

func trSeedProject(t *testing.T, db *gorm.DB, name string, category domain.TaxCategory) uint64 {
	t.Helper()
	res := db.Exec(`INSERT INTO projects (tenant_id, name, status, tax_category) VALUES (?,?,?,?)`,
		trTenant, name, "selling", string(category))
	if res.Error != nil {
		t.Fatalf("seed project: %v", res.Error)
	}
	var id uint64
	db.Raw("SELECT LAST_INSERT_ID()").Scan(&id)
	return id
}

// ── Resolusi rule: specificity + effective dating + inactive ─────────────────

func TestIntegration_TaxRule_Resolution(t *testing.T) {
	db, repo := trSetup(t)
	ctx := context.Background()
	ref := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)

	t.Run("subsidi_menang_atas_all", func(t *testing.T) {
		rule, err := repo.ResolveRule(ctx, trTenant, tax.RateCodePPhFinalPengalihan, domain.TaxCategorySubsidi, ref)
		if err != nil {
			t.Fatalf("resolve subsidi: %v", err)
		}
		if rule.AppliesTo != tax.AppliesToSubsidi || rule.Rate.String() != "0.01" {
			t.Errorf("subsidi harus 1%% (specific > all), got %s @%s", rule.AppliesTo, rule.Rate)
		}
	})
	t.Run("komersial_2_5", func(t *testing.T) {
		rule, err := repo.ResolveRule(ctx, trTenant, tax.RateCodePPhFinalPengalihan, domain.TaxCategoryKomersial, ref)
		if err != nil {
			t.Fatalf("resolve komersial: %v", err)
		}
		if rule.AppliesTo != tax.AppliesToKomersial || rule.Rate.String() != "0.025" {
			t.Errorf("komersial harus 2,5%%, got %s @%s", rule.AppliesTo, rule.Rate)
		}
	})

	t.Run("regulasi_berubah_tambah_baris_effective_from", func(t *testing.T) {
		// "Tarif subsidi naik jadi 1,5% mulai 2027" = TAMBAH rule (tanpa ubah kode).
		if err := db.Exec(`INSERT INTO tax_rates (tenant_id, rate_code, name, rate, applies_to, trigger_event, calc_base, debit_account, credit_account, effective_from, is_active, description)
			VALUES (?,?,?,?,?,?,?,?,?,?,1,'')`,
			trTenant, tax.RateCodePPhFinalPengalihan, "Subsidi 2027", "0.015000", "subsidi",
			"bast", "transfer_value", "5-2000", "2-4000", "2027-01-01 00:00:00").Error; err != nil {
			t.Fatalf("insert rule 2027: %v", err)
		}
		// Sebelum 2027 → tetap 1%; sesudah → 1,5%.
		r2026, _ := repo.ResolveRule(ctx, trTenant, tax.RateCodePPhFinalPengalihan, domain.TaxCategorySubsidi, ref)
		if r2026.Rate.String() != "0.01" {
			t.Errorf("2026 harus tetap 1%%, got %s", r2026.Rate)
		}
		r2027, _ := repo.ResolveRule(ctx, trTenant, tax.RateCodePPhFinalPengalihan, domain.TaxCategorySubsidi,
			time.Date(2027, 3, 1, 0, 0, 0, 0, time.UTC))
		if r2027.Rate.String() != "0.015" {
			t.Errorf("2027 harus 1,5%% (effective_from terbaru), got %s", r2027.Rate)
		}
	})

	t.Run("effective_to_kedaluwarsa_dan_inactive", func(t *testing.T) {
		// Nonaktifkan rule subsidi 2027 → resolusi 2027 kembali ke rule 1%.
		if err := db.Exec(`UPDATE tax_rates SET is_active = 0 WHERE tenant_id = ? AND name = 'Subsidi 2027'`,
			trTenant).Error; err != nil {
			t.Fatal(err)
		}
		r, err := repo.ResolveRule(ctx, trTenant, tax.RateCodePPhFinalPengalihan, domain.TaxCategorySubsidi,
			time.Date(2027, 3, 1, 0, 0, 0, 0, time.UTC))
		if err != nil {
			t.Fatalf("resolve: %v", err)
		}
		if r.Rate.String() != "0.01" {
			t.Errorf("rule inactive harus di-skip; got %s", r.Rate)
		}
		// effective_to: batasi rule subsidi 1% sampai 2026-12-31 → 2027 tak ada rule subsidi;
		// fallback 'all' 2,5%.
		if err := db.Exec(`UPDATE tax_rates SET effective_to = '2026-12-31 23:59:59'
			WHERE tenant_id = ? AND applies_to = 'subsidi' AND is_active = 1`, trTenant).Error; err != nil {
			t.Fatal(err)
		}
		r2, err := repo.ResolveRule(ctx, trTenant, tax.RateCodePPhFinalPengalihan, domain.TaxCategorySubsidi,
			time.Date(2027, 3, 1, 0, 0, 0, 0, time.UTC))
		if err != nil {
			t.Fatalf("resolve fallback: %v", err)
		}
		if r2.AppliesTo != tax.AppliesToAll {
			t.Errorf("melewati effective_to harus fallback ke 'all', got %s", r2.AppliesTo)
		}
	})
}

// ── Akrual BAST atomik: tarif + akun dari rule per kategori proyek ────────────

func TestIntegration_TaxRule_AccrualByProjectCategory(t *testing.T) {
	db, repo := trSetup(t)
	ctx := context.Background()

	subsidi := trSeedProject(t, db, "Perumahan Subsidi", domain.TaxCategorySubsidi)
	komersial := trSeedProject(t, db, "Cluster Komersial", domain.TaxCategoryKomersial)

	// Jalur yang SAMA dengan BAST produksi (atomik dalam tx).
	accrue := func(unitID, projectID uint64) {
		t.Helper()
		if err := repo.AccruePPhFinalInTx(ctx, db, trTenant, unitID, projectID,
			domain.FromInt(500_000_000), time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)); err != nil {
			t.Fatalf("accrue unit %d: %v", unitID, err)
		}
	}
	accrue(1, subsidi)
	accrue(2, komersial)

	var rows []struct {
		UnitID    uint64
		TaxAmount string
		AppliesTo string
		TaxRuleID *uint64
	}
	db.Raw(`SELECT unit_id, tax_amount, applies_to, tax_rule_id FROM tax_obligations
		WHERE tenant_id = ? ORDER BY unit_id`, trTenant).Scan(&rows)
	if len(rows) != 2 {
		t.Fatalf("obligation = %d, want 2", len(rows))
	}
	if m, _ := domain.NewMoney(rows[0].TaxAmount); m.String() != "5000000" || rows[0].AppliesTo != "subsidi" {
		t.Errorf("subsidi: %s (%s), want 5000000 (subsidi 1%%)", rows[0].TaxAmount, rows[0].AppliesTo)
	}
	if m, _ := domain.NewMoney(rows[1].TaxAmount); m.String() != "12500000" || rows[1].AppliesTo != "komersial" {
		t.Errorf("komersial: %s (%s), want 12500000 (2,5%%)", rows[1].TaxAmount, rows[1].AppliesTo)
	}
	if rows[0].TaxRuleID == nil || rows[1].TaxRuleID == nil {
		t.Error("provenance tax_rule_id harus terisi")
	}

	// Jurnal akrual balanced di akun rule (default 5-2000/2-4000).
	var n int64
	db.Raw(`SELECT COUNT(*) FROM journal_lines jl JOIN accounts a ON a.id = jl.account_id
		JOIN journal_entries je ON je.id = jl.journal_entry_id
		WHERE jl.tenant_id = ? AND a.code IN ('5-2000','2-4000') AND je.posted_at IS NOT NULL`, trTenant).Scan(&n)
	if n != 4 { // 2 obligation × 2 baris
		t.Errorf("baris jurnal akrual = %d, want 4", n)
	}

	// SNAPSHOT: mengubah rule SETELAH akrual tidak mengubah obligation lama.
	if err := db.Exec(`UPDATE tax_rates SET rate = '0.050000' WHERE tenant_id = ? AND applies_to = 'subsidi'`,
		trTenant).Error; err != nil {
		t.Fatal(err)
	}
	var after string
	db.Raw(`SELECT tax_amount FROM tax_obligations WHERE tenant_id = ? AND unit_id = 1`, trTenant).Scan(&after)
	if m, _ := domain.NewMoney(after); m.String() != "5000000" {
		t.Errorf("obligation lama harus tetap 5000000 (snapshot), got %s", after)
	}
}

// ── Akun konfigurable: rule dengan akun khusus dipakai jurnal ─────────────────

func TestIntegration_TaxRule_CustomAccounts(t *testing.T) {
	db, repo := trSetup(t)
	ctx := context.Background()

	// Akun khusus + rule subsidi baru yang memakainya (effective lebih baru →
	// menang atas seed subsidi default pada tanggal referensi).
	for _, acc := range []struct{ code, name, typ, nb string }{
		{"5-2900", "Beban PPh Final Subsidi", "expense", "debit"},
		{"2-4900", "Hutang PPh Final Subsidi", "liability", "credit"},
	} {
		if err := db.Exec(`INSERT INTO accounts (tenant_id, code, name, type, normal_balance, is_system, is_active, category, description)
			VALUES (?,?,?,?,?,0,1,'','')`, trTenant, acc.code, acc.name, acc.typ, acc.nb).Error; err != nil {
			t.Fatal(err)
		}
	}
	svc := tax.NewService(repo, repo, repo, repo, tax.WithRuleResolution(repo, repo))
	if err := svc.SetTaxRate(ctx, trTenant, tax.SetTaxRateRequest{
		RateCode:      tax.RateCodePPhFinalPengalihan,
		Name:          "Subsidi akun khusus",
		Rate:          decimal.NewFromFloat(0.01),
		AppliesTo:     tax.AppliesToSubsidi,
		DebitAccount:  "5-2900",
		CreditAccount: "2-4900",
		EffectiveFrom: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("SetTaxRate: %v", err)
	}

	projectID := trSeedProject(t, db, "Subsidi Akun Khusus", domain.TaxCategorySubsidi)
	if err := repo.AccruePPhFinalInTx(ctx, db, trTenant, 9, projectID,
		domain.FromInt(200_000_000), time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("accrue: %v", err)
	}

	var n int64
	db.Raw(`SELECT COUNT(*) FROM journal_lines jl JOIN accounts a ON a.id = jl.account_id
		WHERE jl.tenant_id = ? AND a.code IN ('5-2900','2-4900')`, trTenant).Scan(&n)
	if n != 2 {
		t.Errorf("jurnal harus memakai akun KONFIGURASI rule (5-2900/2-4900), got %d baris", n)
	}
}

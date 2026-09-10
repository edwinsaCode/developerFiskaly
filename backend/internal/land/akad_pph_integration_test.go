//go:build integration

package land_test

// Fix remaining risk (2026-09-03): PPh Final Pengalihan Kelebihan Tanah harus
// otomatis ter-accrue saat Akad — bukan taxSvc.AccrueTax manual. Test ini
// membuktikan lewat land.Service.RecordAkad (BUKAN repo.RecordAkad langsung
// seperti akad_integration_test.go — di sana PPhPlan sengaja selalu nil
// karena Service tidak dilibatkan) dengan land.WithPPhResolver dipasang ke
// tax.Service produksi yang SAMA dipakai unit/BAST — tidak ada tax engine
// kedua.

import (
	"context"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"

	"esaproperti/internal/domain"
	"esaproperti/internal/ledger"
	"esaproperti/internal/tax"

	"esaproperti/internal/land"
)

// ltSeedPPhCOA extends ltSeedCOA with the PPh Final accrual accounts
// (5-2000/2-4000, kode default tax.DefaultPPhExpenseAccount/PayableAccount)
// and seeds PP 34/2016 default rates — prasyarat land.WithPPhResolver.
func ltSeedPPhCOA(t *testing.T, db *gorm.DB, tenantID uint64) map[string]uint64 {
	t.Helper()
	ids := ltSeedCOA(t, db, tenantID)
	ids["5-2000"] = ltMustAccount(t, db, tenantID, "5-2000", "Beban PPh Final Pengalihan", domain.AccountExpense, domain.NormalBalanceDebit, "")
	ids["2-4000"] = ltMustAccount(t, db, tenantID, "2-4000", "Hutang PPh Final Pengalihan", domain.AccountLiability, domain.NormalBalanceCredit, "")
	if err := tax.SeedDefaultRates(context.Background(), db, tenantID); err != nil {
		t.Fatalf("seed tax rates: %v", err)
	}
	return ids
}

// ltWirePPhService builds land.Service with BOTH a real HPP resolver
// (ltAkadParams style COGS lines aren't reachable via Service — Service
// resolves HPP itself via LandHPPResolver, see ltFixedHPPResolver below) and
// land.WithPPhResolver wired to tax.Service constructed EXACTLY like
// AccruePPhFinalInTx / sale/handler.go's landTaxSvc — same dependencies, same
// options, one engine.
func ltWirePPhService(db *gorm.DB, ratePerM2 domain.Money) *land.Service {
	repo := land.NewGORMRepository(db)
	posting := ledger.NewPostingService(ledger.NewGORMRepository(db), ledger.NewGORMRepository(db))
	taxRepo := tax.NewGORMRepository(db, posting)
	taxSvc := tax.NewService(taxRepo, taxRepo, taxRepo, taxRepo,
		tax.WithRuleResolution(taxRepo, taxRepo), tax.WithUnitProductPolicy(taxRepo))
	return land.NewService(repo,
		land.WithHPPResolver(ltFixedHPPResolver{ratePerM2: ratePerM2}),
		land.WithPPhResolver(taxSvc))
}

// ltFixedHPPResolver: land.LandHPPResolver stub returning a fixed rate/m2 —
// isolates the PPh test from the finalized→budgeted→actual HPP chain, which
// is already covered elsewhere (repository_integration_test.go).
type ltFixedHPPResolver struct{ ratePerM2 domain.Money }

func (r ltFixedHPPResolver) ResolveLandHPPRate(ctx context.Context, tenantID, projectID uint64) (land.LandHPPResolution, error) {
	return land.LandHPPResolution{RatePerM2: r.ratePerM2, Basis: "land_area"}, nil
}

// ltCleanupPPh extends ltCleanupAkad with tax_obligations/tax_rates/accounts
// touched by the PPh resolver — must run before ltCleanupAkad's own account
// cleanup so the UNIQUE(tenant_id, land_sale_id) constraint on
// tax_obligations never collides across test runs.
func ltCleanupPPh(t *testing.T, db *gorm.DB) {
	t.Helper()
	for _, tenantID := range []uint64{ltTenant, ltOtherTenant} {
		for _, tbl := range []string{"tax_payments", "tax_obligations", "tax_rates"} {
			if err := db.Exec("DELETE FROM "+tbl+" WHERE tenant_id = ?", tenantID).Error; err != nil {
				t.Fatalf("cleanup %s: %v", tbl, err)
			}
		}
	}
	ltCleanupAkad(t, db)
}

func TestIntegration_Service_RecordAkad_PPhFinal_AutoAccrue(t *testing.T) {
	db := ltConnect(t)
	ltCleanupPPh(t, db)
	defer ltCleanupPPh(t, db)

	ltSeedPPhCOA(t, db, ltTenant)
	repo := land.NewGORMRepository(db)
	projectID := ltMustProject(t, db, ltTenant, "LT PPh Auto Project")
	custID := ltMustCustomer(t, db, ltTenant, "LT-PPH-AUTO")
	ltMustPool(t, repo, ltTenant, projectID, "500")

	svc := ltWirePPhService(db, domain.FromInt(600_000))

	// DPP 50m² x 1.600.000 = 80.000.000; proyek default tax_category
	// 'komersial' (migration 000039) → PPh Final 2,5% x 80jt = 2.000.000.
	sale, err := svc.RecordAkad(context.Background(), ltTenant, land.RecordAkadRequest{
		ProjectID: projectID, CustomerID: custID,
		QuantityM2: decimal.RequireFromString("50"), UnitPriceSnapshot: domain.FromInt(1_600_000),
		DPPAmount: domain.FromInt(80_000_000), IsPKP: true,
		VATRateSnapshot:    decimal.RequireFromString("0.11"),
		PaymentAccountCode: "1-1300", RecognitionDate: time.Now(),
	})
	if err != nil {
		t.Fatalf("RecordAkad: %v", err)
	}
	if sale.PPhJournalID == nil {
		t.Fatal("PPhJournalID want non-nil — auto-accrual tidak jalan")
	}
	ltJournalBalanced(t, db, *sale.PPhJournalID)

	if got := ltNet(t, db, ltTenant, "5-2000"); !got.Equal(decimal.RequireFromString("2000000")) {
		t.Errorf("Beban PPh Final want 2000000, got %s", got)
	}
	if got := ltNet(t, db, ltTenant, "2-4000"); !got.Equal(decimal.RequireFromString("-2000000")) {
		t.Errorf("Hutang PPh Final want -2000000 (kredit), got %s", got)
	}

	var oblig tax.TaxObligation
	if err := db.Where("tenant_id = ? AND land_sale_id = ?", ltTenant, sale.ID).First(&oblig).Error; err != nil {
		t.Fatalf("tax_obligations wajib ada utk land_sale_id=%d: %v", sale.ID, err)
	}
	if oblig.UnitID != nil {
		t.Errorf("obligation.UnitID want nil (sumber = land_sale, bukan unit), got %v", *oblig.UnitID)
	}
	if !oblig.TaxAmount.Decimal().Equal(decimal.RequireFromString("2000000")) {
		t.Errorf("obligation.TaxAmount want 2000000, got %s", oblig.TaxAmount.Decimal())
	}
	if oblig.Status != tax.ObligationStatusOutstanding {
		t.Errorf("obligation.Status want outstanding, got %s", oblig.Status)
	}
}

// TestIntegration_Service_RecordAkad_PPhFinal_NoResolverIsNilSafe proves
// land.WithPPhResolver is optional (nil-safe, pola identik sale's
// PPhFinalAccruer) — RecordAkad tanpa resolver terpasang berhasil normal,
// tidak ada jurnal/obligation PPh dibuat, tidak fail-closed.
func TestIntegration_Service_RecordAkad_PPhFinal_NoResolverIsNilSafe(t *testing.T) {
	db := ltConnect(t)
	ltCleanupPPh(t, db)
	defer ltCleanupPPh(t, db)

	ltSeedPPhCOA(t, db, ltTenant)
	repo := land.NewGORMRepository(db)
	projectID := ltMustProject(t, db, ltTenant, "LT PPh Nil Project")
	custID := ltMustCustomer(t, db, ltTenant, "LT-PPH-NIL")
	ltMustPool(t, repo, ltTenant, projectID, "500")

	svc := land.NewService(repo, land.WithHPPResolver(ltFixedHPPResolver{ratePerM2: domain.FromInt(600_000)}))

	sale, err := svc.RecordAkad(context.Background(), ltTenant, land.RecordAkadRequest{
		ProjectID: projectID, CustomerID: custID,
		QuantityM2: decimal.RequireFromString("50"), UnitPriceSnapshot: domain.FromInt(1_600_000),
		DPPAmount: domain.FromInt(80_000_000), IsPKP: true,
		VATRateSnapshot:    decimal.RequireFromString("0.11"),
		PaymentAccountCode: "1-1300", RecognitionDate: time.Now(),
	})
	if err != nil {
		t.Fatalf("RecordAkad: %v", err)
	}
	if sale.PPhJournalID != nil {
		t.Errorf("PPhJournalID want nil (no resolver wired), got %v", *sale.PPhJournalID)
	}
	var count int64
	if err := db.Model(&tax.TaxObligation{}).Where("tenant_id = ? AND land_sale_id = ?", ltTenant, sale.ID).Count(&count).Error; err != nil {
		t.Fatalf("count tax_obligations: %v", err)
	}
	if count != 0 {
		t.Errorf("tax_obligations want 0 (no resolver wired), got %d", count)
	}
}

// TestIntegration_Service_RecordAkad_PPhFinal_NoDoubleAccrualOnRetry proves
// requirement "Tidak double accrual saat retry/duplicate request": a land_sale
// is created atomically WITH its single obligation inside one Akad
// transaction — there is no separate re-accrue action, so the only realistic
// "retry" is calling Akad again on the SAME (now-converted) reservation, which
// the existing reservation-state-machine guard rejects atomically before any
// obligation is touched (pola identik unit ErrReservationNotActive-equivalent
// guard sudah teruji di akad_integration_test.go untuk sisi non-PPh).
func TestIntegration_Service_RecordAkad_PPhFinal_NoDoubleAccrualOnRetry(t *testing.T) {
	db := ltConnect(t)
	ltCleanupPPh(t, db)
	defer ltCleanupPPh(t, db)

	ltSeedPPhCOA(t, db, ltTenant)
	repo := land.NewGORMRepository(db)
	projectID := ltMustProject(t, db, ltTenant, "LT PPh Retry Project")
	custID := ltMustCustomer(t, db, ltTenant, "LT-PPH-RETRY")
	pool := ltMustPool(t, repo, ltTenant, projectID, "500")

	res, err := repo.Reserve(context.Background(), ltTenant, pool.ID, land.ReserveInput{
		ProjectID: projectID, CustomerID: custID, QuantityM2: decimal.RequireFromString("50"), ReservedAt: time.Now(),
	})
	if err != nil {
		t.Fatalf("Reserve: %v", err)
	}

	svc := ltWirePPhService(db, domain.FromInt(600_000))
	req := land.RecordAkadRequest{
		ProjectID: projectID, CustomerID: custID, ReservationID: &res.ID,
		QuantityM2: decimal.RequireFromString("50"), UnitPriceSnapshot: domain.FromInt(1_600_000),
		DPPAmount: domain.FromInt(80_000_000), IsPKP: true,
		VATRateSnapshot:    decimal.RequireFromString("0.11"),
		PaymentAccountCode: "1-1300", RecognitionDate: time.Now(),
	}

	sale, err := svc.RecordAkad(context.Background(), ltTenant, req)
	if err != nil {
		t.Fatalf("RecordAkad (pertama): %v", err)
	}

	// Retry — reservasi yang sama sudah converted; harus ditolak atomik.
	if _, err := svc.RecordAkad(context.Background(), ltTenant, req); err != land.ErrReservationNotActive {
		t.Fatalf("retry Akad want ErrReservationNotActive, got %v", err)
	}

	var count int64
	if err := db.Model(&tax.TaxObligation{}).Where("tenant_id = ? AND land_sale_id = ?", ltTenant, sale.ID).Count(&count).Error; err != nil {
		t.Fatalf("count tax_obligations: %v", err)
	}
	if count != 1 {
		t.Errorf("tax_obligations want tetap 1 setelah retry ditolak, got %d", count)
	}
	var salesCount int64
	if err := db.Model(&land.LandSale{}).Where("tenant_id = ?", ltTenant).Count(&salesCount).Error; err != nil {
		t.Fatalf("count land_sales: %v", err)
	}
	if salesCount != 1 {
		t.Errorf("land_sales want tetap 1 setelah retry ditolak, got %d", salesCount)
	}
}

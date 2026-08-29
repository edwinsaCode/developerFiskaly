// cmd/demo-seed inserts a realistic transaction scenario into an existing esaProperti
// database so that dashboard, laporan, and the full accounting flow can be exercised.
//
// Prerequisites: cmd/seed must have run first (master data — LITHOS project + COA).
// Idempotent: safe to run multiple times; every step skips if data already exists.
//
// Scenario (tenant 1, LITHOS Villas, unit LITHOS-A01):
//
//	Step 1 — RAB (budget plan)      : land 5.5B, construction 9.5B, marketing 500M = 15.5B
//	Step 2 — Cost entries (posted)  : land 5B (payable), construction 8B (payable)
//	          Note: marketing adalah expense-only (tidak dikapitalisasi); masuk RAB saja.
//	Step 3 — Allocation basis       : saleable_area (idempotent SetBasis)
//	Step 4 — Tax rate               : PPh Final 2.5% mulai 2024-01-01
//	Step 5 — Termin / DP            : Rp 500.000.000 dari BCA (1-1300)
//	Step 6 — BAST                   : harga Rp 2.800.000.000 (no VAT)
//	          HPP = alokasi otomatis dari engine Phase 5 (proporsional saleable_area)
//	Step 7 — Akrual PPh Final       : 2.5% × 2.800.000.000 = Rp 70.000.000
//	Step 8 — Validasi               : ledger balanced, unit sold, revenue/HPP/tax muncul
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"time"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"

	"esaproperti/internal/allocation"
	"esaproperti/internal/budget"
	"esaproperti/internal/cost"
	"esaproperti/internal/domain"
	"esaproperti/internal/ledger"
	"esaproperti/internal/platform/config"
	dbplatform "esaproperti/internal/platform/db"
	"esaproperti/internal/project"
	"esaproperti/internal/sale"
	"esaproperti/internal/tax"
)

func main() {
	tenantID := flag.Uint64("tenant", 0, "tenant ID (required)")
	flag.Parse()
	if *tenantID == 0 {
		log.Fatal("--tenant <id> is required")
	}

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	db, err := dbplatform.Connect(cfg.DBDSN, cfg.IsDevelopment())
	if err != nil {
		log.Fatalf("database: %v", err)
	}

	ctx := context.Background()
	tid := *tenantID

	log.Printf("[DEMO] === esaProperti demo-seed (tenant %d) ===", tid)

	// ── Wire services (same pattern as HTTP handlers) ─────────────────────────

	// Ledger base
	ledgerRepo := ledger.NewGORMRepository(db)
	// Wiring seam transaksi seperti main.go — tanpa ini CreateAndPost berjalan
	// tanpa transaksi dan seeder bisa meninggalkan draft yatim (W-3.0).
	postingSvc := ledger.NewPostingService(ledgerRepo, ledgerRepo).WithJournalTx(ledgerRepo)

	// Budget
	budgetStore := budget.NewGORMRepository(db)
	budgetRealisasi := budget.NewGORMRealisasiProvider(db)
	budgetSvc := budget.NewService(budgetStore, budgetRealisasi)

	// Cost
	costRepo := cost.NewGORMRepository(db)
	ledgerJournal := cost.NewLedgerJournalAdapter(postingSvc)
	costSvc := cost.NewService(costRepo, ledgerJournal, costRepo, costRepo, nil) // nil = skip budget validation

	// Allocation
	allocRepo := allocation.NewGORMRepository(db)
	allocSvc := allocation.NewService(allocRepo, allocRepo, allocRepo)

	// Sale
	saleRepo := sale.NewGORMRepository(db, postingSvc, allocSvc)
	saleSvc := sale.NewService(saleRepo, saleRepo, saleRepo, saleRepo, saleRepo, saleRepo)

	// Tax
	taxRepo := tax.NewGORMRepository(db, postingSvc)
	taxSvc := tax.NewService(taxRepo, taxRepo, taxRepo, taxRepo)

	// ── Step 0: Find LITHOS project (must exist from cmd/seed) ────────────────

	proj := findProject(ctx, db, tid, "LITHOS Villas")
	log.Printf("[DEMO] Project: %q (id=%d)", proj.Name, proj.ID)

	unit := findUnit(ctx, db, proj.ID, "LITHOS-A01")
	log.Printf("[DEMO] Unit: %q (id=%d, status=%s)", unit.Code, unit.ID, unit.Status)

	// ── Step 1: RAB (budget plan) ─────────────────────────────────────────────

	if err := seedRAB(ctx, db, tid, proj.ID, budgetSvc); err != nil {
		log.Fatalf("[DEMO] RAB: %v", err)
	}

	// ── Step 2: Cost entries ──────────────────────────────────────────────────

	if err := seedCostEntries(ctx, db, tid, proj.ID, costSvc); err != nil {
		log.Fatalf("[DEMO] CostEntry: %v", err)
	}

	// ── Step 3: Allocation basis ──────────────────────────────────────────────

	if err := allocSvc.SetBasis(ctx, tid, proj.ID, allocation.BasisSaleableArea); err != nil {
		log.Fatalf("[DEMO] SetBasis: %v", err)
	}
	log.Printf("[DEMO] Allocation basis: saleable_area (set/updated)")

	// ── Step 4: Tax rate ──────────────────────────────────────────────────────

	if err := seedTaxRate(ctx, db, tid, taxSvc); err != nil {
		log.Fatalf("[DEMO] TaxRate: %v", err)
	}

	// ── Step 5: Termin / DP ───────────────────────────────────────────────────

	if err := seedTermin(ctx, db, tid, unit.ID, saleSvc); err != nil {
		log.Fatalf("[DEMO] Termin: %v", err)
	}

	// ── Step 6: BAST ──────────────────────────────────────────────────────────

	// Re-fetch unit to get current status (might have changed on previous run)
	unit = findUnit(ctx, db, proj.ID, "LITHOS-A01")
	if err := seedBAST(ctx, db, tid, proj.ID, unit, saleSvc); err != nil {
		log.Fatalf("[DEMO] BAST: %v", err)
	}

	// ── Step 7: Akrual PPh Final ──────────────────────────────────────────────

	if err := seedTaxAccrual(ctx, db, tid, proj.ID, unit.ID, taxSvc); err != nil {
		log.Fatalf("[DEMO] TaxAccrual: %v", err)
	}

	// ── Step 8: Validasi ──────────────────────────────────────────────────────

	validate(ctx, db, tid, proj.ID, unit.ID)

	log.Printf("[DEMO] === Done ===")
}

// ── Step helpers ──────────────────────────────────────────────────────────────

func seedRAB(ctx context.Context, db *gorm.DB, tid, projectID uint64, svc *budget.Service) error {
	// Idempotency: skip jika sudah ada active plan
	var count int64
	db.Table("budget_plans").
		Where("tenant_id = ? AND project_id = ? AND status = 'active'", tid, projectID).
		Count(&count)
	if count > 0 {
		log.Printf("[DEMO] RAB: skipped (active plan already exists)")
		return nil
	}

	// Buat draft plan
	plan, err := svc.CreatePlan(ctx, tid, budget.CreatePlanRequest{
		ProjectID: projectID,
		Label:     "RAB v1 — LITHOS Villas Demo",
		Notes:     "Demo seed — skenario realistis untuk pengujian dashboard dan laporan.",
	})
	if err != nil {
		return err
	}

	// Tambah line items: land, construction, marketing
	items := []budget.AddItemRequest{
		{
			PlanID:         plan.ID,
			Category:       budget.BudgetCategoryLand,
			Description:    "Biaya perolehan lahan 1.5 ha",
			BudgetedAmount: domain.FromInt(5_500_000_000),
		},
		{
			PlanID:         plan.ID,
			Category:       budget.BudgetCategoryConstruction,
			Description:    "Konstruksi 18 unit villa",
			BudgetedAmount: domain.FromInt(9_500_000_000),
		},
		{
			PlanID:         plan.ID,
			Category:       budget.BudgetCategoryMarketing,
			Description:    "Pemasaran dan promosi",
			BudgetedAmount: domain.FromInt(500_000_000),
		},
	}
	for _, req := range items {
		if _, err := svc.AddItem(ctx, tid, req); err != nil {
			return err
		}
	}

	// Approve → active
	if _, err := svc.ApprovePlan(ctx, tid, budget.ApprovePlanRequest{
		PlanID:     plan.ID,
		ApprovedBy: "demo-seed",
	}); err != nil {
		return err
	}

	log.Printf("[DEMO] RAB: created plan#%d (land 5.5B, construction 9.5B, marketing 500M = 15.5B)", plan.ID)
	return nil
}

func seedCostEntries(ctx context.Context, db *gorm.DB, tid, projectID uint64, svc *cost.Service) error {
	// Idempotency: skip jika sudah ada cost entry untuk proyek ini
	var count int64
	db.Table("cost_entries").
		Where("tenant_id = ? AND project_id = ?", tid, projectID).
		Count(&count)
	if count > 0 {
		log.Printf("[DEMO] CostEntry: skipped (%d entries already exist)", count)
		return nil
	}

	refDate := time.Date(2025, 3, 15, 0, 0, 0, 0, time.UTC)

	entries := []cost.CreateCostEntryRequest{
		{
			ProjectID:     projectID,
			Category:      domain.CostCategoryLand,
			Amount:        domain.FromInt(5_000_000_000),
			PaymentMethod: cost.PaymentMethodPayable,
			Date:          refDate,
			Vendor:        "PT Agraria Nusantara",
			Description:   "Pembelian lahan 15.000 m² Kab. Badung",
		},
		{
			ProjectID:     projectID,
			Category:      domain.CostCategoryHard,
			Amount:        domain.FromInt(8_000_000_000),
			PaymentMethod: cost.PaymentMethodPayable,
			Date:          refDate.Add(30 * 24 * time.Hour),
			Vendor:        "PT Konstruksi Gemilang",
			Description:   "Konstruksi fase 1 — 6 unit villa tipe A",
		},
		// Marketing adalah expense-only (BudgetCategory saja) — tidak dikapitalisasi ke Persediaan.
		// Tidak ada CostEntry untuk marketing (domain.CostCategory tidak mengenal marketing).
	}

	created := 0
	for _, req := range entries {
		entry, err := svc.CreateCostEntry(ctx, tid, req)
		if err != nil {
			return err
		}
		if err := svc.PostCostEntry(ctx, tid, entry.ID); err != nil {
			return err
		}
		created++
	}

	log.Printf("[DEMO] CostEntry: %d entries created & posted (land 5B + construction 8B = 13B)", created)
	log.Printf("[DEMO]   Note: marketing 300M adalah expense-only, tidak dikapitalisasi (sesuai PSAK)")
	return nil
}

func seedTaxRate(ctx context.Context, db *gorm.DB, tid uint64, svc *tax.Service) error {
	// Idempotency: skip jika sudah ada tarif PPh Final untuk tenant ini
	var count int64
	db.Table("tax_rates").
		Where("tenant_id = ? AND rate_code = ?", tid, tax.RateCodePPhFinalPengalihan).
		Count(&count)
	if count > 0 {
		log.Printf("[DEMO] TaxRate: skipped (tarif PPh Final sudah ada)")
		return nil
	}

	err := svc.SetTaxRate(ctx, tid, tax.SetTaxRateRequest{
		RateCode:      tax.RateCodePPhFinalPengalihan,
		Rate:          decimal.NewFromFloat(0.025), // 2.5% per PP 34/2016
		EffectiveFrom: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		Description:   "PPh Final Pengalihan Hak atas Tanah dan Bangunan — PP 34/2016",
	})
	if err != nil {
		return err
	}
	log.Printf("[DEMO] TaxRate: PPh Final 2.5%% ditetapkan (effective 2024-01-01)")
	return nil
}

func seedTermin(ctx context.Context, db *gorm.DB, tid, unitID uint64, svc *sale.Service) error {
	// Idempotency: skip jika sudah ada termin untuk unit ini
	var count int64
	db.Table("termin_payments").
		Where("tenant_id = ? AND unit_id = ?", tid, unitID).
		Count(&count)
	if count > 0 {
		log.Printf("[DEMO] Termin: skipped (%d termin already exists)", count)
		return nil
	}

	uid := unitID
	res, err := svc.ReceivePayment(ctx, tid, sale.ReceivePaymentRequest{
		Source:          sale.PaymentSourceUnitTermin,
		UnitID:          &uid,
		BankAccountCode: "1-1300", // BCA
		Amount:          domain.FromInt(500_000_000),
		Date:            time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC),
		Notes:           "DP LITHOS-A01 — Budi Santoso",
	})
	if err != nil {
		return err
	}

	log.Printf("[DEMO] Termin: DP Rp 500.000.000 dicatat (termin#%d, Dr BCA / Cr %s)", res.TerminID, res.CreditAccount)
	return nil
}

func seedBAST(ctx context.Context, db *gorm.DB, tid, projectID uint64, u *project.Unit, svc *sale.Service) error {
	if u.Status == project.UnitStatusSold {
		log.Printf("[DEMO] BAST: skipped (unit %s already sold)", u.Code)
		return nil
	}

	// Increment 6.1 (F-1): BAST hanya sah dari reserved|ppjb. Ikuti alur legal:
	// reserve dulu (tercatat di lifecycle log — audit trail demo pun konsisten).
	if u.Status == project.UnitStatusAvailable {
		projRepo := project.NewGORMRepository(db)
		projSvc := project.NewService(projRepo, projRepo, projRepo)
		buyer := "Budi Santoso / KTP 3171xxxx"
		if _, err := projSvc.Transition(ctx, tid, u.ID, project.TransitionRequest{
			Next:           project.UnitStatusReserved,
			Event:          project.EventReservationConfirmed,
			Notes:          "demo-seed: reserve sebelum BAST",
			UpdateUnitOpts: project.UpdateUnitOpts{BuyerRef: &buyer},
		}); err != nil {
			return fmt.Errorf("reserve unit sebelum BAST: %w", err)
		}
		log.Printf("[DEMO] Unit %s: available → reserved (prasyarat BAST)", u.Code)
	}

	bastDate := time.Date(2025, 6, 30, 0, 0, 0, 0, time.UTC)
	rec, err := svc.RecordAkad(ctx, tid, sale.RecordBASTRequest{
		UnitID:    u.ID,
		SalePrice: domain.FromInt(2_800_000_000),
		IsVAT:     false, // non-PKP untuk simplisitas demo
		BuyerRef:  "Budi Santoso / KTP 3171xxxx",
		BASTDate:  bastDate,
	})
	if err != nil {
		return err
	}

	hppTotal := rec.HPPLand.Add(rec.HPPHard).Add(rec.HPPSoft).Add(rec.HPPFinancing)
	log.Printf("[DEMO] Akad: unit %s sold → Pendapatan Rp 2.800.000.000", u.Code)
	log.Printf("[DEMO]   HPP total: %s (land=%s hard=%s soft=%s)",
		hppTotal.String(), rec.HPPLand.String(), rec.HPPHard.String(), rec.HPPSoft.String())
	return nil
}

func seedTaxAccrual(ctx context.Context, db *gorm.DB, tid, projectID, unitID uint64, svc *tax.Service) error {
	// Idempotency: skip jika sudah ada obligasi PPh Final untuk unit ini
	var count int64
	db.Table("tax_obligations").
		Where("tenant_id = ? AND unit_id = ?", tid, unitID).
		Count(&count)
	if count > 0 {
		log.Printf("[DEMO] TaxAccrual: skipped (obligasi PPh Final sudah ada)")
		return nil
	}

	pid := projectID
	uid := unitID
	obl, err := svc.AccrueTax(ctx, tid, tax.AccrueTaxRequest{
		RateCode:      tax.RateCodePPhFinalPengalihan,
		TransferValue: domain.FromInt(2_800_000_000),
		AccrualDate:   time.Date(2025, 6, 30, 0, 0, 0, 0, time.UTC),
		ProjectID:     &pid,
		UnitID:        &uid,
	})
	if err != nil {
		return err
	}

	log.Printf("[DEMO] TaxAccrual: PPh Final %s (2.5%% × 2.800.000.000), status=%s",
		obl.TaxAmount.String(), obl.Status)
	return nil
}

// ── Validasi ──────────────────────────────────────────────────────────────────

func validate(ctx context.Context, db *gorm.DB, tid, projectID, unitID uint64) {
	log.Printf("[DEMO] --- Validasi ---")

	// 1. Ledger balanced: Σ debit == Σ kredit
	var debitSum, creditSum string
	db.Table("journal_lines jl").
		Joins("JOIN journal_entries je ON je.id = jl.journal_entry_id").
		Where("je.tenant_id = ? AND je.posted_at IS NOT NULL", tid).
		Select("CAST(COALESCE(SUM(jl.debit),0) AS CHAR) AS debit_sum").
		Scan(&debitSum)
	db.Table("journal_lines jl").
		Joins("JOIN journal_entries je ON je.id = jl.journal_entry_id").
		Where("je.tenant_id = ? AND je.posted_at IS NOT NULL", tid).
		Select("CAST(COALESCE(SUM(jl.credit),0) AS CHAR) AS credit_sum").
		Scan(&creditSum)

	type balanceRow struct {
		DebitSum  string
		CreditSum string
	}
	var bal balanceRow
	db.Raw(`
		SELECT
		  CAST(COALESCE(SUM(jl.debit),0)  AS CHAR) AS debit_sum,
		  CAST(COALESCE(SUM(jl.credit),0) AS CHAR) AS credit_sum
		FROM journal_lines jl
		JOIN journal_entries je ON je.id = jl.journal_entry_id
		WHERE je.tenant_id = ? AND je.posted_at IS NOT NULL
	`, tid).Scan(&bal)

	debit, _ := decimal.NewFromString(bal.DebitSum)
	credit, _ := decimal.NewFromString(bal.CreditSum)
	if debit.Equal(credit) {
		log.Printf("[DEMO] ✓ Ledger balanced: Σ debit = Σ kredit = %s", debit.StringFixed(0))
	} else {
		log.Printf("[DEMO] ✗ LEDGER TIDAK BALANCED! debit=%s kredit=%s", debit.StringFixed(0), credit.StringFixed(0))
	}

	// 2. Unit A01 status sold
	var u project.Unit
	db.Where("id = ?", unitID).First(&u)
	if u.Status == project.UnitStatusSold {
		log.Printf("[DEMO] ✓ Unit LITHOS-A01 status: sold")
	} else {
		log.Printf("[DEMO] ✗ Unit LITHOS-A01 status: %s (expected: sold)", u.Status)
	}

	// 3. Revenue muncul (akun 4-1000)
	type amtRow struct{ Total string }
	var rev amtRow
	db.Raw(`
		SELECT CAST(COALESCE(SUM(jl.credit),0) AS CHAR) AS total
		FROM journal_lines jl
		JOIN journal_entries je ON je.id = jl.journal_entry_id
		JOIN accounts a ON a.id = jl.account_id
		WHERE je.tenant_id = ? AND je.posted_at IS NOT NULL AND a.code = '4-1000'
	`, tid).Scan(&rev)
	log.Printf("[DEMO] ✓ Pendapatan (4-1000): Cr %s", rev.Total)

	// 4. HPP muncul (akun 5-1000)
	var hpp amtRow
	db.Raw(`
		SELECT CAST(COALESCE(SUM(jl.debit),0) AS CHAR) AS total
		FROM journal_lines jl
		JOIN journal_entries je ON je.id = jl.journal_entry_id
		JOIN accounts a ON a.id = jl.account_id
		WHERE je.tenant_id = ? AND je.posted_at IS NOT NULL AND a.code = '5-1000'
	`, tid).Scan(&hpp)
	log.Printf("[DEMO] ✓ HPP (5-1000): Dr %s", hpp.Total)

	// 5. PPh Final muncul (akun 2-4000)
	var pph amtRow
	db.Raw(`
		SELECT CAST(COALESCE(SUM(jl.credit),0) AS CHAR) AS total
		FROM journal_lines jl
		JOIN journal_entries je ON je.id = jl.journal_entry_id
		JOIN accounts a ON a.id = jl.account_id
		WHERE je.tenant_id = ? AND je.posted_at IS NOT NULL AND a.code = '2-4000'
	`, tid).Scan(&pph)
	log.Printf("[DEMO] ✓ Hutang PPh Final (2-4000): Cr %s", pph.Total)

	// 6. Persediaan Real Estat berkurang (net Persediaan = Σ Dr 1-3xxx − Σ Cr 1-3xxx)
	var inv amtRow
	db.Raw(`
		SELECT CAST(COALESCE(SUM(jl.debit) - SUM(jl.credit), 0) AS CHAR) AS total
		FROM journal_lines jl
		JOIN journal_entries je ON je.id = jl.journal_entry_id
		JOIN accounts a ON a.id = jl.account_id
		WHERE je.tenant_id = ? AND je.posted_at IS NOT NULL
		  AND a.code IN ('1-3000','1-3100','1-3200','1-3300')
	`, tid).Scan(&inv)
	log.Printf("[DEMO] ✓ Net Persediaan Real Estat (1-3xxx): %s (berkurang setelah BAST)", inv.Total)
}

// ── Lookup helpers ────────────────────────────────────────────────────────────

func findProject(ctx context.Context, db *gorm.DB, tenantID uint64, name string) *project.Project {
	var p project.Project
	err := db.WithContext(ctx).
		Where("tenant_id = ? AND name = ?", tenantID, name).
		First(&p).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		log.Fatalf("[DEMO] Project %q tidak ditemukan — jalankan `make seed` terlebih dahulu", name)
	}
	if err != nil {
		log.Fatalf("[DEMO] findProject: %v", err)
	}
	return &p
}

func findUnit(ctx context.Context, db *gorm.DB, projectID uint64, code string) *project.Unit {
	var u project.Unit
	err := db.WithContext(ctx).
		Where("project_id = ? AND code = ?", projectID, code).
		First(&u).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		log.Fatalf("[DEMO] Unit %q tidak ditemukan — jalankan `make seed` terlebih dahulu", code)
	}
	if err != nil {
		log.Fatalf("[DEMO] findUnit: %v", err)
	}
	return &u
}

//go:build integration

package cancellation_test

// kelebihan-tanah-booking-integration-2026-08 — integration (real MySQL):
// membuktikan bahwa pembatalan pasca-Akad pada kontrak dengan komponen
// Produk Tambahan Kelebihan Tanah membalik jurnal pendapatan+HPP tanah
// (land.CancelLandSaleTx) di DALAM transaksi Process yang SAMA dengan
// pembalikan Event3/4 unitnya sendiri (§D3, simetri arah pembalikan) — bukan
// jalur/waktu terpisah, dan tidak ada COGS ganda.
//
// Prasyarat: TEST_DB_DSN di-set + DB termigrasi (≥ 000084/000085).

import (
	"context"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"

	"esaproperti/internal/allocation"
	"esaproperti/internal/billing"
	"esaproperti/internal/cancellation"
	"esaproperti/internal/domain"
	"esaproperti/internal/land"
	"esaproperti/internal/ledger"
	"esaproperti/internal/sale"
	"esaproperti/internal/tax"
)

type lczUnitHPPResolver struct{}

func (lczUnitHPPResolver) ResolveHPP(_ context.Context, _, _ uint64, _ *uint64, _ uint64) (sale.HPPResolution, error) {
	return sale.HPPResolution{Method: sale.HPPMethodActual}, nil
}

type lczLandHPPResolver struct {
	rate domain.Money
}

func (r lczLandHPPResolver) ResolveLandHPPRate(_ context.Context, _, _ uint64) (land.LandHPPResolution, error) {
	return land.LandHPPResolution{RatePerM2: r.rate, Method: land.HPPMethodActual}, nil
}

// lcWireSale merangkai sale.Service produksi persis seperti cxWireSale,
// ditambah WithLandAkadPreparer (seam Kelebihan Tanah §D3) dan resolver HPP
// unit yang sengaja nol — fokus test ini adalah pembalikan komponen TANAH,
// bukan resolusi HPP rumah (sudah diuji terpisah).
//
// land.WithPPhResolver dipasang dengan tax.Service yang SAMA (dibangun dari
// taxRepo dependencies yang identik dengan SetTaxAccruer unit) — bukan tax
// engine kedua — membuktikan PPh Final Kelebihan Tanah otomatis ter-accrue
// dan ikut dibalik simetris saat pembatalan pasca-Akad (bukan taxSvc.AccrueTax
// manual).
func lcWireSale(db *gorm.DB, landRate domain.Money) *sale.Service {
	ledgerRepo := ledger.NewGORMRepository(db)
	posting := ledger.NewPostingService(ledgerRepo, ledgerRepo).WithPeriodChecker(ledgerRepo)
	allocRepo := allocation.NewGORMRepository(db)
	allocSvc := allocation.NewService(allocRepo, allocRepo, allocRepo, allocation.WithVersionStore(allocRepo))
	saleRepo := sale.NewGORMRepository(db, posting, allocSvc)
	brepo := billing.NewGORMRepository(db)
	saleRepo.SetReceiptTxGenerator(&cxReceiptAdapter{svc: billing.NewReceiptService(brepo, brepo, brepo)})
	taxRepo := tax.NewGORMRepository(db, posting)
	saleRepo.SetTaxAccruer(taxRepo)

	landTaxSvc := tax.NewService(taxRepo, taxRepo, taxRepo, taxRepo,
		tax.WithRuleResolution(taxRepo, taxRepo), tax.WithUnitProductPolicy(taxRepo))
	landSvc := land.NewService(land.NewGORMRepository(db),
		land.WithHPPResolver(lczLandHPPResolver{rate: landRate}),
		land.WithPPhResolver(landTaxSvc))

	return sale.NewService(saleRepo, saleRepo, saleRepo, saleRepo, saleRepo, saleRepo,
		sale.WithHPPResolver(lczUnitHPPResolver{}), sale.WithPaymentCommitter(saleRepo),
		sale.WithContractStore(saleRepo), sale.WithBookingStore(saleRepo),
		sale.WithLandAkadPreparer(landSvc))
}

func TestIntegration_Cancellation_PostAkad_WithLandComponent(t *testing.T) {
	db := cxConnect(t)
	cxCleanup(t, db)
	defer cxCleanup(t, db)
	ctx := context.Background()

	projectID, unitID := cxSeedProjectUnit(t, db, "LC-A", 400, "reserved")
	var customerID uint64
	if err := db.Exec(`INSERT INTO customers (tenant_id, code, name) VALUES (?,?,?)`,
		cxTenant, "CUST-LC", "Buyer LC").Error; err != nil {
		t.Fatal(err)
	}
	db.Raw("SELECT LAST_INSERT_ID()").Scan(&customerID)

	accIDs := cxSeedAccounts(t, db)
	extra := []ledger.Account{
		{TenantID: cxTenant, Code: "4-1100", Name: "Pendapatan Kelebihan Tanah", Type: domain.AccountRevenue, NormalBalance: domain.NormalBalanceCredit, IsActive: true},
		{TenantID: cxTenant, Code: "2-3000", Name: "PPN Keluaran", Type: domain.AccountLiability, NormalBalance: domain.NormalBalanceCredit, IsActive: true},
	}
	for i := range extra {
		if err := db.Create(&extra[i]).Error; err != nil {
			t.Fatalf("seed account %s: %v", extra[i].Code, err)
		}
		accIDs[extra[i].Code] = extra[i].ID
	}
	if err := tax.SeedDefaultRates(ctx, db, cxTenant); err != nil {
		t.Fatalf("seed tax rates: %v", err)
	}

	// Pool tanah 200 m2 @ Rp500.000; HPP tanah dipatok Rp200.000/m2 lewat stub.
	pool := &land.LandStock{
		TenantID: cxTenant, ProjectID: projectID, ProductCode: "kelebihan_tanah",
		TotalQuantityM2: decimal.RequireFromString("200"), UnitPrice: domain.MustParse("500000"),
	}
	if err := land.NewGORMRepository(db).CreatePool(ctx, pool); err != nil {
		t.Fatalf("CreatePool: %v", err)
	}

	saleSvc := lcWireSale(db, domain.FromInt(200_000))
	cxSvc := cancellation.NewService(db)

	// Kontrak langsung dengan komponen tanah 100 m2 (50jt) — reservasi lahir
	// atomik bersama kontrak (§D2).
	qty := decimal.RequireFromString("100")
	cust := customerID
	contract, err := saleSvc.CreateContract(ctx, cxTenant, sale.CreateContractRequest{
		UnitID:         unitID,
		BuyerName:      "Buyer LC",
		PaymentType:    sale.PaymentTypeTunai,
		ContractDate:   time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
		TotalPrice:     domain.FromInt(2_000_000_000),
		CustomerID:     &cust,
		LandQuantityM2: &qty,
	})
	if err != nil {
		t.Fatalf("CreateContract: %v", err)
	}
	if contract.LandReservationID == nil {
		t.Fatal("LandReservationID harus terisi sebelum Akad")
	}

	// Termin uang muka 500jt sebelum Akad (mendapatkan received > 0 utk refund).
	if _, err := saleSvc.ReceivePayment(ctx, cxTenant, sale.ReceivePaymentRequest{
		Source: sale.PaymentSourceUnitTermin, UnitID: &unitID,
		Amount: domain.FromInt(500_000_000), Date: time.Date(2026, 6, 10, 0, 0, 0, 0, time.UTC),
		BankAccountCode: "1-1300",
	}); err != nil {
		t.Fatalf("termin: %v", err)
	}

	// Akad terbundel: rumah (2M, HPP unit sengaja nol) + tanah (100m2 x
	// 500rb=50jt revenue, 100m2 x 200rb=20jt HPP), satu transaksi (§D3).
	rec, err := saleSvc.RecordAkad(ctx, cxTenant, sale.RecordBASTRequest{
		UnitID: unitID, SalePrice: domain.FromInt(2_000_000_000),
		BASTDate: time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("RecordAkad: %v", err)
	}
	if rec.RevenueJournalID == 0 {
		t.Fatal("rumah: RevenueJournalID harus ada")
	}

	var landSale struct {
		ID               uint64
		Status           string
		RevenueJournalID *uint64
		CogsJournalID    *uint64
		// gorm:"column:..." eksplisit — sama seperti land.LandSale (lihat
		// internal/land/sale.go): naming strategy GORM menebak "PPh" jadi
		// "p_ph", bukan "pph", jadi tanpa tag ini field-nya tak pernah
		// terisi meski kolomnya ada di hasil raw SQL.
		PPhJournalID *uint64 `gorm:"column:pph_journal_id"`
	}
	if err := db.Raw(`SELECT id, status, revenue_journal_id, cogs_journal_id, pph_journal_id
		FROM land_sales WHERE tenant_id = ? AND land_stock_id = ?`, cxTenant, pool.ID).Scan(&landSale).Error; err != nil {
		t.Fatalf("baca land_sales: %v", err)
	}
	if landSale.Status != "akad" || landSale.RevenueJournalID == nil || landSale.CogsJournalID == nil {
		t.Fatalf("land_sales pasca-Akad = %+v, want akad + kedua jurnal", landSale)
	}
	landSaleID := landSale.ID

	// PPh Final Pengalihan tanah: otomatis ter-accrue (land.WithPPhResolver),
	// bukan taxSvc.AccrueTax manual. DPP=50jt, proyek default tax_category
	// 'komersial' (migration 000039) → 2,5% x 50jt = 1.250.000.
	if landSale.PPhJournalID == nil {
		t.Fatal("land_sales.pph_journal_id nil — PPh Final tanah tidak ter-accrue otomatis")
	}
	var landObligation tax.TaxObligation
	if err := db.Where("tenant_id = ? AND land_sale_id = ?", cxTenant, landSaleID).First(&landObligation).Error; err != nil {
		t.Fatalf("tax_obligations utk land_sale_id=%d: %v", landSaleID, err)
	}
	if !landObligation.TaxAmount.Decimal().Equal(decimal.RequireFromString("1250000")) {
		t.Errorf("obligation.TaxAmount = %s, want 1250000", landObligation.TaxAmount.Decimal())
	}

	// Pra-cancel: saldo tanah sudah terbentuk.
	if bal := cxNet(t, db, "4-1100"); bal != "-50000000.0000" {
		t.Fatalf("pre-cancel 4-1100 = %s, want -50000000.0000", bal)
	}
	if bal := cxNet(t, db, "5-1000"); bal != "20000000.0000" {
		t.Fatalf("pre-cancel 5-1000 = %s, want 20000000.0000 (tanah, HPP rumah nol)", bal)
	}
	if bal := cxNet(t, db, "1-3000"); bal != "-20000000.0000" {
		t.Fatalf("pre-cancel 1-3000 = %s, want -20000000.0000 (persediaan tanah keluar)", bal)
	}
	// 5-2000/2-4000 dipakai bersama oleh PPh Final unit (SetTaxAccruer, rumah
	// 2M x 2,5% = 50jt) DAN PPh Final tanah (WithPPhResolver, 50jt x 2,5% =
	// 1,25jt) — satu mesin pajak yang sama, akun default yang sama (pola
	// identik unit/BAST), jadi saldonya terakumulasi: 50jt + 1,25jt = 51,25jt.
	if bal := cxNet(t, db, "5-2000"); bal != "51250000.0000" {
		t.Fatalf("pre-cancel 5-2000 (Beban PPh Final) = %s, want 51250000.0000 (unit 50jt + tanah 1,25jt)", bal)
	}
	if bal := cxNet(t, db, "2-4000"); bal != "-51250000.0000" {
		t.Fatalf("pre-cancel 2-4000 (Hutang PPh Final) = %s, want -51250000.0000 (unit 50jt + tanah 1,25jt)", bal)
	}

	// ── Cancel pasca-Akad (penalti 50jt) ────────────────────────────────────
	c, err := cxSvc.Request(ctx, cxTenant, cancellation.RequestInput{
		UnitID: unitID, Reason: "wanprestasi", Penalty: domain.FromInt(50_000_000),
		EventDate: time.Date(2026, 6, 20, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("Request: %v", err)
	}
	if c.Stage != cancellation.StagePostBAST {
		t.Fatalf("stage = %s, want post_bast", c.Stage)
	}
	if _, err := cxSvc.Approve(ctx, cxTenant, c.ID, nil); err != nil {
		t.Fatalf("Approve: %v", err)
	}
	if _, err := cxSvc.Preview(ctx, cxTenant, c.ID); err != nil {
		t.Fatalf("Preview: %v", err)
	}

	journalsBefore := int64(0)
	db.Raw("SELECT COUNT(*) FROM journal_entries WHERE tenant_id = ?", cxTenant).Scan(&journalsBefore)

	done, err := cxSvc.Process(ctx, cxTenant, c.ID, nil)
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	// COGSReversalJournalID (SISI UNIT) sengaja nil di sini — HPP rumah
	// dipatok nol oleh lczUnitHPPResolver (fokus test = komponen tanah), jadi
	// RecordAkad tidak pernah menerbitkan jurnal COGS rumah untuk dibalik.
	if done.RevenueReversalJournalID == nil || done.SettlementJournalID == nil {
		t.Fatalf("jejak jurnal unit tidak lengkap: %+v", done)
	}
	if done.LandSaleID == nil || *done.LandSaleID != landSaleID {
		t.Fatalf("LandSaleID = %v, want %d", done.LandSaleID, landSaleID)
	}
	if done.LandRevenueReversalJournalID == nil || done.LandCOGSReversalJournalID == nil {
		t.Fatalf("jejak jurnal tanah tidak lengkap: %+v", done)
	}

	// §D3 simetri: pembalikan tanah (revenue+HPP) terbit DI DALAM Process yang
	// sama dengan pembalikan unit — bukan aksi/waktu terpisah. Tak ada cara
	// langsung menghitung "berapa jurnal baru" tanpa tahu jumlah komponen unit
	// (Event3 rev-reversal, COGS rev-reversal, settlement, mungkin refund) —
	// yang dibuktikan di sini cukup: kedua reversal tanah muncul di RESPONS
	// Process yang SAMA (done), bukan lewat panggilan lain.
	var journalsAfter int64
	db.Raw("SELECT COUNT(*) FROM journal_entries WHERE tenant_id = ?", cxTenant).Scan(&journalsAfter)
	if journalsAfter <= journalsBefore {
		t.Errorf("jurnal baru pasca-Process = %d, want > 0", journalsAfter-journalsBefore)
	}

	// ── Saldo pasca-cancel: komponen tanah bersih nol, tak ada COGS ganda ───
	if bal := cxNet(t, db, "4-1100"); bal != "0.0000" {
		t.Errorf("4-1100 = %s, want 0.0000 (pendapatan tanah dibalik penuh)", bal)
	}
	if bal := cxNet(t, db, "5-1000"); bal != "0.0000" {
		t.Errorf("5-1000 = %s, want 0.0000 (INV-COGS-SUM: HPP tanah dibalik, tak ada sisa/dobel)", bal)
	}
	if bal := cxNet(t, db, "1-3000"); bal != "0.0000" {
		t.Errorf("1-3000 = %s, want 0.0000 (persediaan tanah kembali)", bal)
	}
	if bal := cxNet(t, db, "4-1000"); bal != "0.0000" {
		t.Errorf("4-1000 = %s, want 0.0000 (pendapatan rumah dibalik)", bal)
	}
	// PPh Final Pengalihan tanah dibalik simetris, konsisten dgn pendapatan/HPP.
	if bal := cxNet(t, db, "5-2000"); bal != "0.0000" {
		t.Errorf("5-2000 (Beban PPh Final) = %s, want 0.0000 (dibalik penuh)", bal)
	}
	if bal := cxNet(t, db, "2-4000"); bal != "0.0000" {
		t.Errorf("2-4000 (Hutang PPh Final) = %s, want 0.0000 (dibalik penuh)", bal)
	}

	// ── land_sales berstatus cancelled, jurnal reversal tercatat ────────────
	var landAfter struct {
		Status                   string
		RevenueReversalJournalID *uint64
		CogsReversalJournalID    *uint64
		PPhReversalJournalID     *uint64 `gorm:"column:pph_reversal_journal_id"`
	}
	if err := db.Raw(`SELECT status, revenue_reversal_journal_id, cogs_reversal_journal_id, pph_reversal_journal_id
		FROM land_sales WHERE id = ?`, landSaleID).Scan(&landAfter).Error; err != nil {
		t.Fatalf("baca land_sales pasca-cancel: %v", err)
	}
	if landAfter.Status != "cancelled" {
		t.Errorf("land_sales.status = %s, want cancelled", landAfter.Status)
	}
	if landAfter.RevenueReversalJournalID == nil || landAfter.CogsReversalJournalID == nil {
		t.Errorf("land_sales reversal journal ids kosong: %+v", landAfter)
	}
	if landAfter.PPhReversalJournalID == nil {
		t.Errorf("land_sales.pph_reversal_journal_id nil — PPh Final tanah tidak ikut dibalik")
	}

	var landObligationAfter tax.TaxObligation
	if err := db.Where("tenant_id = ? AND land_sale_id = ?", cxTenant, landSaleID).First(&landObligationAfter).Error; err != nil {
		t.Fatalf("tax_obligations pasca-cancel utk land_sale_id=%d: %v", landSaleID, err)
	}
	if landObligationAfter.Status != tax.ObligationStatusCancelled {
		t.Errorf("obligation.Status pasca-cancel = %s, want cancelled", landObligationAfter.Status)
	}

	// ── Pool: sold_quantity_m2 kembali turun (100m2 dilepas) ────────────────
	var soldAfter string
	db.Raw(`SELECT sold_quantity_m2 FROM land_stock WHERE id = ?`, pool.ID).Scan(&soldAfter)
	if soldAfter != "0.0000" {
		t.Errorf("pool.sold_quantity_m2 pasca-cancel = %s, want 0.0000", soldAfter)
	}

	// ── Unit kembali available, log cancelled_post_bast ─────────────────────
	var st string
	db.Raw("SELECT status FROM units WHERE id=?", unitID).Scan(&st)
	if st != "available" {
		t.Errorf("unit = %s, want available", st)
	}
	var lg struct{ Event string }
	db.Raw(`SELECT event FROM unit_status_transitions WHERE tenant_id=? AND unit_id=? ORDER BY id DESC LIMIT 1`,
		cxTenant, unitID).Scan(&lg)
	if lg.Event != "cancelled_post_bast" {
		t.Errorf("log = %+v", lg)
	}
}

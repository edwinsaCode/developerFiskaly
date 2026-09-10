//go:build integration

package sale_test

// Bug klien 2026-08-31: "ketika saya coba melakukan penjualan/akad (unit
// rumah+kelebihan tanah) kenapa tidak dicatat otomatis juga piutang yang
// harus dibayar untuk kelebihan tanah. harus sesuai HPP dari produk
// kelebihan tanah=harga beli."
//
// Root cause yang ditemukan & diperbaiki (lihat internal/sale/service.go,
// blok "Produk Tambahan: Kelebihan Tanah" di RecordAkad, dan
// internal/land/hpp_resolver.go):
//   Fix A — HPP Kelebihan Tanah sekarang = purchase_price per m² (harga
//     beli), langsung, BUKAN alokasi RAB/actual (PurchasePriceLandHPPResolver).
//   Fix B — piutang Kelebihan Tanah SELALU ke akun Piutang Customer (1-2000)
//     sekalipun kontrak rumahnya berskema KPR (yang pada state Akad
//     me-resolve piutang RUMAH ke Piutang Bank 1-2200, T-3) — tanah bukan
//     bagian pembiayaan bank.
//
// Test lawas (TestIntegration_Land_BundledAkad_AtomicAndBalanced, di
// land_integration_test.go) HANYA memakai PaymentTypeTunai — jalur itu SUDAH
// benar sebelum perbaikan ini (Tunai selalu ke 1-2000), sehingga tidak
// pernah menangkap bug KPR. Test ini menutup celah itu secara konkret.
//
// Prasyarat: TEST_DB_DSN + DB termigrasi.

import (
	"context"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"

	"esaproperti/internal/allocation"
	"esaproperti/internal/customer"
	"esaproperti/internal/domain"
	"esaproperti/internal/ledger"
	"esaproperti/internal/land"
	"esaproperti/internal/sale"
	"esaproperti/internal/salesorg"
	"esaproperti/internal/scheme"
)

const lkTenant uint64 = 9_900_091

func lkCleanup(t *testing.T, db *gorm.DB) {
	t.Helper()
	if err := db.Exec(`UPDATE land_stock_reservations SET converted_sale_id = NULL WHERE tenant_id = ?`, lkTenant).Error; err != nil {
		t.Fatalf("cleanup putus siklus land_stock_reservations: %v", err)
	}
	for _, tbl := range []string{
		"contract_payment_events", "payment_allocations", "credit_applications",
		"payment_schedules", "receipts", "invoices", "sale_records",
		"termin_payments", "bookings", "sale_contracts",
		"documents", "document_sequences",
		"land_allocations", "land_sales", "land_stock_reservations", "land_stock",
		"journal_lines", "journal_entries",
		"allocation_configs", "unit_status_transitions", "units", "product_types", "project_phases", "projects",
		"financing_sources", "payment_schemes",
		"sales_persons", "sales_teams", "customers", "accounts",
	} {
		if err := db.Exec("DELETE FROM "+tbl+" WHERE tenant_id = ?", lkTenant).Error; err != nil {
			t.Fatalf("cleanup %s: %v", tbl, err)
		}
	}
}

type lkEnv struct {
	db          *gorm.DB
	svc         *sale.Service
	schemeSvc   *scheme.Service
	projectID   uint64
	customerID  uint64
	salesID     uint64
	finSourceID uint64
	schemes     map[string]*scheme.PaymentScheme
}

// lkSetup merangkai wiring PRODUKSI penuh: scheme flow (KPR) + land bundled
// Akad seam bersamaan — pola gabungan psSetup (scheme_integration_test.go)
// dan lbWire (land_integration_test.go), memakai resolver HPP tanah PRODUKSI
// (land.PurchasePriceLandHPPResolver), bukan stub.
func lkSetup(t *testing.T) *lkEnv {
	t.Helper()
	db := itConnect(t)
	lkCleanup(t, db)
	t.Cleanup(func() { lkCleanup(t, db) })
	ctx := context.Background()

	if err := ledger.SeedCOA(ctx, db, lkTenant); err != nil {
		t.Fatalf("seed COA: %v", err)
	}
	if err := scheme.SeedDefaultSchemes(ctx, db, lkTenant); err != nil {
		t.Fatalf("seed schemes: %v", err)
	}
	if err := slSeedDocumentTypes(db, lkTenant); err != nil {
		t.Fatalf("seed jenis dokumen: %v", err)
	}
	// Akun pendapatan Kelebihan Tanah (bukan bagian SeedCOA dasar).
	if err := db.Exec(`INSERT IGNORE INTO accounts (tenant_id, code, name, type, normal_balance, is_active)
		VALUES (?,?,?,?,?,TRUE)`, lkTenant, "4-1100", "Pendapatan Kelebihan Tanah",
		domain.AccountRevenue, domain.NormalBalanceCredit).Error; err != nil {
		t.Fatalf("seed akun 4-1100: %v", err)
	}

	ledgerRepo := ledger.NewGORMRepository(db)
	posting := ledger.NewPostingService(ledgerRepo, ledgerRepo)
	allocRepo := allocation.NewGORMRepository(db)
	allocSvc := allocation.NewService(allocRepo, allocRepo, allocRepo)
	saleRepo := slWireReceipts(db, sale.NewGORMRepository(db, posting, allocSvc))
	custRepo := customer.NewGORMRepository(db)
	personRepo := salesorg.NewGORMRepository(db)

	// Resolver HPP tanah PRODUKSI (koreksi klien 2026-08-31): purchase_price
	// per m², bukan alokasi RAB.
	landRepo := land.NewGORMRepository(db)
	landResolver := land.NewPurchasePriceLandHPPResolver(landRepo)
	landSvc := land.NewService(landRepo, land.WithHPPResolver(landResolver))

	svc := sale.NewService(saleRepo, saleRepo, saleRepo, saleRepo, saleRepo, saleRepo,
		sale.WithContractStore(saleRepo), sale.WithPaymentCommitter(saleRepo),
		sale.WithSchemeFlow(saleRepo, scheme.DefaultRegistry(), &psParties{customers: custRepo, persons: personRepo}),
		sale.WithHandoverWriter(saleRepo),
		sale.WithLandAkadPreparer(landSvc))
	schemeSvc := scheme.NewService(scheme.NewGORMRepository(db), scheme.DefaultRegistry())

	env := &lkEnv{db: db, svc: svc, schemeSvc: schemeSvc}

	res := db.Exec(`INSERT INTO projects (tenant_id, name, status) VALUES (?,?,?)`, lkTenant, "LK Project", "selling")
	if res.Error != nil {
		t.Fatalf("seed project: %v", res.Error)
	}
	db.Raw("SELECT LAST_INSERT_ID()").Scan(&env.projectID)

	if err := db.Exec(`INSERT INTO allocation_configs (tenant_id, project_id, basis) VALUES (?,?,?)`,
		lkTenant, env.projectID, "saleable_area").Error; err != nil {
		t.Fatalf("seed allocation config: %v", err)
	}

	cust, err := customer.NewService(custRepo).Create(ctx, lkTenant, customer.CreateCustomerRequest{Code: "CUST-LK", Name: "Budi LK"})
	if err != nil {
		t.Fatalf("seed customer: %v", err)
	}
	env.customerID = cust.ID

	person := &salesorg.SalesPerson{TenantID: lkTenant, Code: "SP-LK", Name: "Sari LK", IsActive: true}
	if err := personRepo.CreatePerson(ctx, person); err != nil {
		t.Fatalf("seed sales person: %v", err)
	}
	env.salesID = person.ID

	fs, err := schemeSvc.CreateFinancingSource(ctx, lkTenant, scheme.CreateFinancingSourceRequest{
		Code: "BTN-LK", Name: "Bank BTN", Type: scheme.FinSourceKPRKomersial,
	})
	if err != nil {
		t.Fatalf("seed financing source: %v", err)
	}
	env.finSourceID = fs.ID

	all, err := schemeSvc.ListSchemes(ctx, lkTenant)
	if err != nil {
		t.Fatalf("list schemes: %v", err)
	}
	env.schemes = map[string]*scheme.PaymentScheme{}
	for _, m := range all {
		env.schemes[m.Code] = m
	}
	return env
}

func (e *lkEnv) seedUnit(t *testing.T, code string) uint64 {
	t.Helper()
	e.db.Exec(`INSERT IGNORE INTO product_types (tenant_id, code, name, category, revenue_account_code, is_active)
		VALUES (?,?,?,?,?,TRUE)`, lkTenant, "rumah", "rumah", "property", "4-1000")
	res := e.db.Exec(`INSERT INTO units (tenant_id, project_id, code, unit_type, saleable_area, land_area, list_price, status)
		VALUES (?,?,?,?,?,?,?,?)`,
		lkTenant, e.projectID, code, "rumah", domain.FromInt(100), domain.FromInt(100), domain.FromInt(0), "reserved")
	if res.Error != nil {
		t.Fatalf("seed unit: %v", res.Error)
	}
	var id uint64
	e.db.Raw("SELECT LAST_INSERT_ID()").Scan(&id)
	return id
}

func (e *lkEnv) mustPool(t *testing.T, total, purchasePrice, unitPrice string) *land.LandStock {
	t.Helper()
	pool := &land.LandStock{
		TenantID: lkTenant, ProjectID: e.projectID, ProductCode: "kelebihan_tanah",
		TotalQuantityM2: decimal.RequireFromString(total),
		PurchasePrice:   domain.MustParse(purchasePrice),
		UnitPrice:       domain.MustParse(unitPrice),
	}
	if err := land.NewGORMRepository(e.db).CreatePool(context.Background(), pool); err != nil {
		t.Fatalf("CreatePool: %v", err)
	}
	return pool
}

func (e *lkEnv) event(t *testing.T, contractID uint64, ev scheme.Event) *sale.ContractPaymentEvent {
	t.Helper()
	out, err := e.svc.ApplySchemeEvent(context.Background(), lkTenant, sale.ApplySchemeEventRequest{
		ContractID: contractID, Event: ev,
	})
	if err != nil {
		t.Fatalf("ApplySchemeEvent %s: %v", ev, err)
	}
	return out
}

func (e *lkEnv) pay(t *testing.T, contractID uint64, amount int64) *sale.ReceivePaymentResult {
	t.Helper()
	cid := contractID
	res, err := e.svc.ReceivePayment(context.Background(), lkTenant, sale.ReceivePaymentRequest{
		Source: sale.PaymentSourceCollection, ContractID: &cid,
		Amount: domain.FromInt(amount), Date: time.Date(2026, 7, 2, 0, 0, 0, 0, time.UTC),
		BankAccountCode: "1-1300",
	})
	if err != nil {
		t.Fatalf("ReceivePayment %d: %v", amount, err)
	}
	return res
}

func (e *lkEnv) unitAccountBalance(t *testing.T, unitID uint64, code string) string {
	t.Helper()
	var v string
	err := e.db.Raw(`SELECT COALESCE(SUM(jl.debit - jl.credit), 0) FROM journal_lines jl
		JOIN accounts a ON a.id = jl.account_id
		JOIN journal_entries je ON je.id = jl.journal_entry_id
		WHERE jl.tenant_id = ? AND jl.unit_id = ? AND a.code = ? AND je.posted_at IS NOT NULL`,
		lkTenant, unitID, code).Scan(&v).Error
	if err != nil {
		t.Fatalf("balance query: %v", err)
	}
	m, _ := domain.NewMoney(v)
	return m.String()
}

// TestIntegration_Land_BundledAkad_KPR_ReceivableRoutesToCustomerNotBank
// membuktikan Fix A + Fix B bersamaan pada kontrak KPR:
//
//   - land_sales.gross_amount (piutang tanah) HARUS muncul otomatis saat
//     Akad terbundel, walau kontrak rumahnya KPR (bug klien: sebelumnya
//     TIDAK tercatat sama sekali via akun yang salah/hilang).
//   - Piutang tanah HARUS mendarat di 1-2000 (Piutang Customer), BUKAN
//     1-2200 (Piutang Bank) — sekalipun piutang RUMAH pada state Akad
//     memang ke 1-2200 (T-3, tak berubah oleh fix ini).
//   - HPP tanah (COGS jurnal) HARUS persis quantity_m2 × purchase_price
//     (harga beli), bukan alokasi RAB.
//   - Jurnal tanah tetap balanced (Invariant #1).
func TestIntegration_Land_BundledAkad_KPR_ReceivableRoutesToCustomerNotBank(t *testing.T) {
	env := lkSetup(t)
	ctx := context.Background()
	unitID := env.seedUnit(t, "LK-A1")

	// Pool: harga beli 300rb/m², harga jual 500rb/m² (angka berbeda sengaja,
	// supaya assert HPP≠revenue membuktikan resolver benar-benar memakai
	// purchase_price, bukan unit_price/gross yang kebetulan sama).
	pool := env.mustPool(t, "1000", "300000", "500000")

	qty := decimal.RequireFromString("100")
	contract, err := env.svc.CreateContract(ctx, lkTenant, sale.CreateContractRequest{
		UnitID:            unitID,
		BuyerName:         "Budi LK",
		BuyerID:           "3201...",
		ContractDate:      time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
		TotalPrice:        domain.FromInt(1_000_000_000),
		PaymentSchemeID:   &env.schemes["KPR-KOM"].ID,
		FinancingSourceID: &env.finSourceID,
		CustomerID:        &env.customerID,
		SalesPersonID:     &env.salesID,
		LandQuantityM2:    &qty,
	})
	if err != nil {
		t.Fatalf("CreateContract KPR+tanah: %v", err)
	}
	if contract.LandReservationID == nil {
		t.Fatal("LandReservationID harus terisi sebelum Akad")
	}

	// DP customer (pra-akad → Uang Muka) lalu jalur KPR sampai state akad.
	env.pay(t, contract.ID, 100_000_000)
	env.event(t, contract.ID, scheme.EventSubmittedToBank)
	env.event(t, contract.ID, scheme.EventBankApproved)
	env.event(t, contract.ID, scheme.EventAkad)

	// Akad (RecordAkad): rumah + tanah dalam SATU transaksi. Nilai Persetujuan
	// KPR Bank 900jt (== sisa rumah) → seluruhnya ke Dana Jaminan Bank.
	approved900 := domain.FromInt(900_000_000)
	rec, err := env.svc.RecordAkad(ctx, lkTenant, sale.RecordBASTRequest{
		UnitID: unitID, SalePrice: domain.FromInt(1_000_000_000), BuyerRef: "Budi LK",
		BASTDate:           time.Date(2026, 7, 10, 0, 0, 0, 0, time.UTC),
		BankApprovedAmount: &approved900,
	})
	if err != nil {
		t.Fatalf("RecordAkad: %v", err)
	}
	if rec.RevenueJournalID == 0 {
		t.Fatal("rumah: RevenueJournalID harus ada")
	}

	// ── Piutang RUMAH tetap ke Piutang Bank (T-3, tak berubah oleh fix ini) ──
	if got := env.unitAccountBalance(t, unitID, "1-2200"); got != "900000000" {
		t.Errorf("saldo Piutang Bank (rumah) = %s, want 900000000", got)
	}

	// ── INTI FIX: land_sales muncul, DAN piutangnya ke 1-2000 (bukan 1-2200) ──
	var landSale struct {
		Status           string
		QuantityM2       string
		DPPAmount        string
		GrossAmount      string
		RevenueJournalID *uint64
		CogsJournalID    *uint64
	}
	if err := env.db.Raw(`SELECT status, quantity_m2, dpp_amount, gross_amount, revenue_journal_id, cogs_journal_id
		FROM land_sales WHERE tenant_id = ? AND land_stock_id = ?`, lkTenant, pool.ID).Scan(&landSale).Error; err != nil {
		t.Fatalf("baca land_sales: %v", err)
	}
	if landSale.Status != "akad" || landSale.QuantityM2 != "100.0000" {
		t.Fatalf("land_sales harus tercatat akad/100.0000 (bug klien: sebelumnya tidak tercatat), got %+v", landSale)
	}
	// Piutang tanah = 100 x 500.000 = 50.000.000, otomatis.
	if landSale.GrossAmount != "50000000.0000" {
		t.Errorf("land_sales.gross_amount = %s, want 50000000.0000 (piutang tanah otomatis)", landSale.GrossAmount)
	}
	if landSale.RevenueJournalID == nil || landSale.CogsJournalID == nil {
		t.Fatalf("land_sales harus punya kedua jurnal (revenue+HPP), got %+v", landSale)
	}

	// Debit piutang tanah mendarat di 1-2000, TERPISAH dari piutang rumah di
	// 1-2200 — inilah bug yang diperbaiki: sebelumnya tanah ikut resolusi akun
	// skema pembiayaan RUMAH (yang untuk KPR di state Akad = 1-2200).
	var landDebitAccount string
	if err := env.db.Raw(`SELECT a.code FROM journal_lines jl
		JOIN accounts a ON a.id = jl.account_id
		WHERE jl.tenant_id = ? AND jl.journal_entry_id = ? AND jl.debit > 0`,
		lkTenant, *landSale.RevenueJournalID).Scan(&landDebitAccount).Error; err != nil {
		t.Fatalf("baca baris debit jurnal tanah: %v", err)
	}
	if landDebitAccount != "1-2000" {
		t.Errorf("akun debit piutang tanah = %s, want 1-2000 (Piutang Customer — tanah bukan bagian KPR)", landDebitAccount)
	}
	if landDebitAccount == "1-2200" {
		t.Error("BUG: piutang tanah tidak boleh ikut ke Piutang Bank skema KPR rumah")
	}

	// ── Fix A: HPP tanah = quantity_m2 × purchase_price (harga beli) ────────
	// 100 x 300.000 = 30.000.000 — BUKAN 100 x 500.000 (unit_price/gross).
	var cogsTotal string
	if err := env.db.Table("journal_lines").
		Select("COALESCE(SUM(debit),0)").
		Where("tenant_id = ? AND journal_entry_id = ?", lkTenant, *landSale.CogsJournalID).
		Scan(&cogsTotal).Error; err != nil {
		t.Fatalf("sum jurnal HPP tanah: %v", err)
	}
	m, _ := domain.NewMoney(cogsTotal)
	if m.String() != "30000000" {
		t.Errorf("HPP tanah = %s, want 30000000 (100 m² x harga beli 300.000 — bukan harga jual)", m.String())
	}

	// ── Invariant #1: jurnal tanah balanced ──────────────────────────────────
	for _, jid := range []uint64{*landSale.RevenueJournalID, *landSale.CogsJournalID} {
		var r struct {
			Debit  domain.Money `gorm:"column:d"`
			Credit domain.Money `gorm:"column:c"`
		}
		env.db.Table("journal_lines").
			Select("COALESCE(SUM(debit),0) AS d, COALESCE(SUM(credit),0) AS c").
			Where("tenant_id = ? AND journal_entry_id = ?", lkTenant, jid).Scan(&r)
		if r.Debit.String() != r.Credit.String() {
			t.Errorf("jurnal tanah %d tidak balanced: debit=%s credit=%s", jid, r.Debit, r.Credit)
		}
	}
}

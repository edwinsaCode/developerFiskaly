//go:build integration

package charge_test

// Billing Batch 2 — lifecycle penuh Charge Group terhadap DB nyata:
// create → partial payment (alokasi manual K-3) → payout (K-1) → K-5 overrun →
// void → refund (K-4) → transfer antar grup → true-up & settle → gate BAST (K-2).
// Verifikasi ledger: saldo 2-2400 == paid − payout − refund − transfer_house,
// dan outstanding HARGA RUMAH tidak pernah tersentuh (counts_toward_price).

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"esaproperti/internal/billing"
	"esaproperti/internal/charge"
	"esaproperti/internal/document"
	"esaproperti/internal/domain"
	"esaproperti/internal/ledger"
	"esaproperti/internal/project"
	"esaproperti/internal/receivable"
)

// cgReceiptAdapter — pola receiptGenAdapter di cmd/api/main.go.
type cgReceiptAdapter struct{ svc *billing.ReceiptService }

func (a *cgReceiptAdapter) GenerateReceiptInTx(ctx context.Context, tx *gorm.DB, tenantID, createdBy, terminID, unitID uint64, amount domain.Money, bankAccountCode string, date time.Time, notes string) (string, uint64, error) {
	rec, err := a.svc.GenerateReceiptInTx(ctx, tx, tenantID, createdBy, terminID, unitID, amount, bankAccountCode, date, notes)
	if err != nil {
		return "", 0, err
	}
	return rec.ReceiptNumber, rec.ID, nil
}

// cgInvoiceAdapter — pola chargeInvoiceAdapter di cmd/api/main.go.
type cgInvoiceAdapter struct{ svc *billing.Service }

func (a *cgInvoiceAdapter) IssueChargeInvoiceInTx(ctx context.Context, tx *gorm.DB, tenantID, contractID, chargeGroupID, createdBy uint64, outstanding domain.Money, dueDate time.Time, notes string) (uint64, string, time.Time, error) {
	inv, err := a.svc.GenerateChargeGroupInvoiceInTx(ctx, tx, tenantID, contractID, chargeGroupID, createdBy, outstanding, dueDate, notes)
	if err != nil {
		return 0, "", time.Time{}, err
	}
	return inv.ID, inv.InvoiceNumber, inv.DueDate, nil
}

func (a *cgInvoiceAdapter) FindLiveChargeInvoiceInTx(ctx context.Context, tx *gorm.DB, tenantID, contractID, chargeGroupID uint64) (uint64, string, time.Time, error) {
	return a.svc.FindLiveChargeInvoiceInTx(ctx, tx, tenantID, contractID, chargeGroupID)
}

func (a *cgInvoiceAdapter) SettleChargeInvoiceIfPaid(ctx context.Context, tenantID, contractID, chargeGroupID uint64, outstanding domain.Money) {
	a.svc.SettleChargeInvoiceIfPaid(ctx, tenantID, contractID, chargeGroupID, outstanding)
}

const cgTenant uint64 = 9_900_244

func cgConnect(t *testing.T) *gorm.DB {
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

func cgCleanup(t *testing.T, db *gorm.DB) {
	t.Helper()
	for _, tbl := range []string{
		"charge_receivable_recognitions",
		"charge_settlements", "charge_payouts", "charge_item_adjustments",
		"charge_items", "charge_groups", "realization_charge_types",
		"master_data_changes", "payment_allocations", "receipts",
		"receipt_sequences", "invoices", "invoice_sequences", "termin_payments",
		"documents", "document_sequences", "document_types",
		// sale_records: sejak W-8 fixture piutang harga rumah menanam BAST, dan
		// baris yang tidak dibersihkan akan menumpuk antar-run — piutang naik
		// setiap kali test dijalankan tanpa satu pun kode produksi berubah.
		"payment_schedules", "sale_records", "sale_contracts", "units", "projects",
		"journal_lines", "journal_entries", "accounts",
	} {
		if err := db.Exec("DELETE FROM "+tbl+" WHERE tenant_id = ?", cgTenant).Error; err != nil {
			t.Fatalf("cleanup %s: %v", tbl, err)
		}
	}
}

type cgEnv struct {
	db         *gorm.DB
	svc        *charge.Service
	billing    *billing.Service
	projectID  uint64
	unitID     uint64
	contractID uint64
}

func cgSetup(t *testing.T) *cgEnv {
	t.Helper()
	db := cgConnect(t)
	cgCleanup(t, db)
	ctx := context.Background()
	if err := ledger.SeedCOA(ctx, db, cgTenant); err != nil {
		t.Fatalf("seed COA: %v", err)
	}
	db.Exec(`INSERT IGNORE INTO tenants (id, name) VALUES (?, 'CG Test')`, cgTenant)
	// W-1: master jenis biaya realisasi — di produksi di-seed bersama COA
	// (coaSeederAdapter.SeedCOA). Tanpa master ini setiap grup realization
	// ditolak fail-closed, jadi seed-nya adalah bagian dari setup, bukan fixture.
	if err := charge.SeedDefaultChargeTypes(ctx, db, cgTenant); err != nil {
		t.Fatalf("seed jenis biaya realisasi: %v", err)
	}
	// W-2: master jenis dokumen — tanpa ini tidak ada kwitansi/invoice/memo yang
	// bisa terbit (resolver fail-closed). Sama seperti di produksi, disemai
	// bersama COA.
	if err := document.SeedDefaultDocumentTypes(ctx, db, cgTenant); err != nil {
		t.Fatalf("seed jenis dokumen: %v", err)
	}
	// W-13: master katalog produk — item addon menunjuk produk dari sini, dan
	// akun pendapatannya diambil dari master (fail-closed). Sama seperti jenis
	// biaya realisasi, di produksi ia disemai bersama COA.
	if err := project.SeedDefaultProductTypes(ctx, db, cgTenant); err != nil {
		t.Fatalf("seed katalog produk: %v", err)
	}
	// Produk non_property test-only untuk kasus addon: kelebihan_tanah pindah
	// kategori `land` (LT-8, migration 000083) dan tidak lagi bisa dijual lewat
	// jalur addon — katalog default tidak lagi punya contoh non_property aktif
	// (pdam/kavling dinonaktifkan 000062). "souvenir" murni fixture test.
	if err := db.Exec(`INSERT IGNORE INTO product_types (tenant_id, code, name, category, revenue_account_code, is_active)
		VALUES (?, 'souvenir', 'Souvenir', 'non_property', '4-2000', TRUE)`, cgTenant).Error; err != nil {
		t.Fatalf("seed produk test souvenir: %v", err)
	}

	env := &cgEnv{db: db, svc: charge.NewService(db)}
	// Kwitansi KWR — pakai ReceiptService billing produksi (seri & idempotensi asli).
	brepo := billing.NewGORMRepository(db)
	env.svc.SetReceiptTxGenerator(&cgReceiptAdapter{svc: billing.NewReceiptService(brepo, brepo, brepo)})
	// W-5: invoice realisasi + jurnal pengakuan piutangnya lahir dalam satu
	// transaksi, jadi penerbit invoice adalah bagian dari setup produksi — bukan
	// tambahan opsional.
	env.billing = billing.NewService(brepo, brepo, brepo, brepo)
	env.svc.SetInvoiceIssuer(&cgInvoiceAdapter{svc: env.billing})
	// W-13: katalog produk untuk item addon — wiring produksi yang sama.
	env.svc.SetProductPolicyResolver(project.NewGORMRepository(db))
	if err := db.Exec(`INSERT INTO projects (tenant_id, name, status) VALUES (?,?,?)`,
		cgTenant, "CG Proyek", "selling").Error; err != nil {
		t.Fatalf("seed project: %v", err)
	}
	db.Raw("SELECT LAST_INSERT_ID()").Scan(&env.projectID)
	if err := db.Exec(`INSERT INTO units (tenant_id, project_id, code, unit_type, saleable_area, list_price, status)
		VALUES (?,?,?,?,?,?,?)`,
		cgTenant, env.projectID, "CG-A1", "rumah", domain.FromInt(100), domain.FromInt(500_000_000), "available").Error; err != nil {
		t.Fatalf("seed unit: %v", err)
	}
	db.Raw("SELECT LAST_INSERT_ID()").Scan(&env.unitID)
	if err := db.Exec(`INSERT INTO sale_contracts
		(tenant_id, unit_id, buyer_name, buyer_id, payment_type, contract_date,
		 total_price, dpp_amount, is_pkp, vat_rate_snapshot, gross_amount)
		VALUES (?,?,?,?,?,?,?,?,0,0,?)`,
		cgTenant, env.unitID, "Budi", "3201xx", "cash", time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
		domain.FromInt(500_000_000), domain.FromInt(500_000_000), domain.FromInt(500_000_000)).Error; err != nil {
		t.Fatalf("seed contract: %v", err)
	}
	db.Raw("SELECT LAST_INSERT_ID()").Scan(&env.contractID)
	return env
}

// balanceOf: saldo kredit-normal sebuah akun dari jurnal POSTED (ledger = SoT).
func (e *cgEnv) balanceOf(t *testing.T, code string) string {
	t.Helper()
	var s struct{ Bal string }
	if err := e.db.Raw(`SELECT COALESCE(SUM(jl.credit - jl.debit),0) AS bal
		FROM journal_lines jl
		JOIN journal_entries je ON je.id = jl.journal_entry_id
		JOIN accounts a ON a.id = jl.account_id AND a.tenant_id = je.tenant_id
		WHERE je.tenant_id = ? AND je.posted_at IS NOT NULL AND a.code = ?`, cgTenant, code).
		Scan(&s).Error; err != nil {
		t.Fatalf("saldo %s: %v", code, err)
	}
	return s.Bal
}

func (e *cgEnv) balance2400(t *testing.T) string {
	t.Helper()
	return e.balanceOf(t, "2-2400")
}

func (e *cgEnv) housePaid(t *testing.T) string {
	t.Helper()
	var s struct{ Total string }
	if err := e.db.Raw(`SELECT COALESCE(SUM(amount),0) AS total FROM termin_payments
		WHERE tenant_id = ? AND unit_id = ? AND counts_toward_price = TRUE`, cgTenant, e.unitID).
		Scan(&s).Error; err != nil {
		t.Fatalf("house paid: %v", err)
	}
	return s.Total
}

func mustEq(t *testing.T, label, got string, wantInt int64) {
	t.Helper()
	want := domain.FromInt(wantInt)
	gotM, err := domain.NewMoney(got)
	if err != nil {
		t.Fatalf("%s: parse %q: %v", label, got, err)
	}
	if !gotM.Sub(want).IsZero() {
		t.Fatalf("%s: dapat %s, harap %s", label, gotM, want)
	}
}

func moneyEq(t *testing.T, label string, got domain.Money, wantInt int64) {
	t.Helper()
	if !got.Sub(domain.FromInt(wantInt)).IsZero() {
		t.Fatalf("%s: dapat %s, harap %d", label, got, wantInt)
	}
}

func TestIntegration_ChargeGroup_FullLifecycle(t *testing.T) {
	env := cgSetup(t)
	defer cgCleanup(t, env.db)
	ctx := context.Background()
	svc := env.svc
	day := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)

	// ── 1. Create Charge — contoh klien: Notaris 10jt, PDAM 5jt, Listrik 5jt.
	due := time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC)
	sum, err := svc.CreateGroup(ctx, cgTenant, charge.CreateGroupRequest{
		SaleContractID: env.contractID,
		Kind:           charge.KindRealization,
		Label:          "Biaya Realisasi CASH",
		Items: []charge.NewItemInput{
			{Label: "Notaris", ChargeTypeCode: "notaris", Amount: domain.FromInt(10_000_000), DueDate: &due},
			{Label: "PDAM", ChargeTypeCode: "pdam", Amount: domain.FromInt(5_000_000), DueDate: &due},
			{Label: "Listrik", ChargeTypeCode: "listrik", Amount: domain.FromInt(5_000_000), DueDate: &due},
		},
	})
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	moneyEq(t, "billed awal", sum.Billed, 20_000_000)
	moneyEq(t, "outstanding awal", sum.Outstanding, 20_000_000)
	itemID := map[string]uint64{}
	for _, it := range sum.Items {
		itemID[it.Label] = it.ItemID
	}

	// ── 2. Partial payment 15jt — alokasi MANUAL admin (K-3, contoh B klien).
	res, err := svc.ReceivePayment(ctx, cgTenant, charge.ReceivePaymentRequest{
		GroupID: sum.GroupID, Amount: domain.FromInt(15_000_000), Date: day,
		BankAccountCode: "1-1100",
		Allocations: []charge.AllocationInput{
			{ChargeItemID: itemID["Notaris"], Amount: domain.FromInt(10_000_000)},
			{ChargeItemID: itemID["PDAM"], Amount: domain.FromInt(3_000_000)},
			{ChargeItemID: itemID["Listrik"], Amount: domain.FromInt(2_000_000)},
		},
		IdempotencyKey: "cg-pay-1",
	})
	if err != nil {
		t.Fatalf("receive payment: %v", err)
	}
	if res.ReceiptNumber == "" || res.ReceiptNumber[:3] != "KWR" {
		t.Fatalf("kwitansi harus seri KWR, dapat %q", res.ReceiptNumber)
	}
	moneyEq(t, "total dibayar", res.Summary.Paid, 15_000_000)
	moneyEq(t, "sisa terutang", res.Summary.Outstanding, 5_000_000)
	mustEq(t, "saldo 2-2400", env.balance2400(t), 15_000_000)
	mustEq(t, "harga rumah TIDAK tersentuh", env.housePaid(t), 0)

	// Idempotency: kunci sama → tidak ada termin/jurnal kedua.
	res2, err := svc.ReceivePayment(ctx, cgTenant, charge.ReceivePaymentRequest{
		GroupID: sum.GroupID, Amount: domain.FromInt(15_000_000), Date: day,
		BankAccountCode: "1-1100",
		Allocations: []charge.AllocationInput{
			{ChargeItemID: itemID["Notaris"], Amount: domain.FromInt(15_000_000)},
		},
		IdempotencyKey: "cg-pay-1",
	})
	if err != nil {
		t.Fatalf("idempotent replay: %v", err)
	}
	if !res2.AlreadyExisted || res2.TerminID != res.TerminID {
		t.Fatalf("replay harus mengembalikan termin yang sama")
	}
	mustEq(t, "saldo 2-2400 pasca-replay", env.balance2400(t), 15_000_000)

	// Guard alokasi: melebihi sisa item → ditolak.
	_, err = svc.ReceivePayment(ctx, cgTenant, charge.ReceivePaymentRequest{
		GroupID: sum.GroupID, Amount: domain.FromInt(4_000_000), Date: day,
		BankAccountCode: "1-1100",
		Allocations: []charge.AllocationInput{
			{ChargeItemID: itemID["PDAM"], Amount: domain.FromInt(4_000_000)}, // sisa PDAM 2jt
		},
	})
	if !errors.Is(err, charge.ErrAllocationExceeds) {
		t.Fatalf("alokasi > sisa item harus ditolak, dapat: %v", err)
	}

	// ── 3. Payout vendor (K-1): Notaris aktual 9jt → Dr 2-2400 / Cr Kas.
	sump, err := svc.RecordPayout(ctx, cgTenant, charge.PayoutRequest{
		ChargeItemID: itemID["Notaris"], Amount: domain.FromInt(9_000_000), Date: day,
		BankAccountCode: "1-1100", Vendor: "Notaris Sari, S.H.",
	})
	if err != nil {
		t.Fatalf("payout notaris: %v", err)
	}
	moneyEq(t, "payout grup", sump.Payout, 9_000_000)
	moneyEq(t, "residual", sump.Residual, 6_000_000) // 15 − 9
	mustEq(t, "saldo 2-2400 pasca-payout", env.balance2400(t), 6_000_000)

	// ── 4. K-5: payout Listrik 3jt > tagihan berjalan? tagihan 5jt, bayar 2jt.
	// Payout total 6jt > tagihan 5jt → tagihan di-raise otomatis ke 6jt.
	if _, err := svc.RecordPayout(ctx, cgTenant, charge.PayoutRequest{
		ChargeItemID: itemID["Listrik"], Amount: domain.FromInt(6_000_000), Date: day,
		BankAccountCode: "1-1100", Vendor: "PLN",
	}); err != nil {
		t.Fatalf("payout listrik: %v", err)
	}
	sk5, err := svc.GroupSummary(ctx, cgTenant, sum.GroupID)
	if err != nil {
		t.Fatalf("summary k5: %v", err)
	}
	for _, it := range sk5.Items {
		if it.ItemID == itemID["Listrik"] {
			moneyEq(t, "K-5 tagihan Listrik naik", it.Amount, 6_000_000)
			moneyEq(t, "K-5 outstanding Listrik", it.Outstanding, 4_000_000) // 6 − 2
		}
	}
	moneyEq(t, "billed pasca-K5", sk5.Billed, 21_000_000)
	moneyEq(t, "outstanding pasca-K5", sk5.Outstanding, 6_000_000)
	moneyEq(t, "residual pasca-K5", sk5.Residual, 0) // 15 − 15

	// Void kini mustahil (dana sudah terpakai) — guard append-only.
	if _, err := svc.VoidPayment(ctx, cgTenant, charge.VoidPaymentRequest{
		TerminID: res.TerminID, Reason: "salah input",
	}); !errors.Is(err, charge.ErrVoidBreaksFunds) {
		t.Fatalf("void saat dana terpakai harus ditolak, dapat: %v", err)
	}

	// ── 5. Pelunasan 6jt (PDAM 2jt + Listrik 4jt) lalu true-up & settle.
	if _, err := svc.ReceivePayment(ctx, cgTenant, charge.ReceivePaymentRequest{
		GroupID: sum.GroupID, Amount: domain.FromInt(6_000_000), Date: day,
		BankAccountCode: "1-1100",
		Allocations: []charge.AllocationInput{
			{ChargeItemID: itemID["PDAM"], Amount: domain.FromInt(2_000_000)},
			{ChargeItemID: itemID["Listrik"], Amount: domain.FromInt(4_000_000)},
		},
	}); err != nil {
		t.Fatalf("pelunasan: %v", err)
	}
	// PDAM aktual 4jt (bayar customer 5jt) → K-4: true-up turun, residual muncul.
	if _, err := svc.RecordPayout(ctx, cgTenant, charge.PayoutRequest{
		ChargeItemID: itemID["PDAM"], Amount: domain.FromInt(4_000_000), Date: day,
		BankAccountCode: "1-1100", Vendor: "PDAM Kota",
	}); err != nil {
		t.Fatalf("payout pdam: %v", err)
	}

	settle1, err := svc.TrueUpAndSettle(ctx, cgTenant, sum.GroupID, nil)
	if err != nil {
		t.Fatalf("true-up: %v", err)
	}
	// paid=21, payout=19 (9+6+4) → residual 2jt (sisa titipan K-4) → belum settled.
	if settle1.Settled {
		t.Fatal("grup dengan residual 2jt tidak boleh settled")
	}
	moneyEq(t, "outstanding pasca true-up", settle1.Summary.Outstanding, 0)
	moneyEq(t, "residual K-4", settle1.Summary.Residual, 2_000_000)

	// ── 6. K-4: refund 500rb + transfer 1.5jt ke grup addon (produk tambahan).
	if _, err := svc.Refund(ctx, cgTenant, charge.RefundRequest{
		GroupID: sum.GroupID, Amount: domain.FromInt(500_000), Date: day,
		BankAccountCode: "1-1100",
	}); err != nil {
		t.Fatalf("refund: %v", err)
	}
	mustEq(t, "saldo 2-2400 pasca-refund", env.balance2400(t), 1_500_000)

	addon, err := svc.CreateGroup(ctx, cgTenant, charge.CreateGroupRequest{
		SaleContractID: env.contractID,
		Kind:           charge.KindAddon,
		Label:          "Produk Tambahan",
		Items: []charge.NewItemInput{{
			Label: "Souvenir", ProductCode: "souvenir",
			Amount: domain.FromInt(6_000_000),
		}},
	})
	if err != nil {
		t.Fatalf("create addon: %v", err)
	}
	// FAIL-CLOSED (W-2): tanpa master jenis dokumen MTI, transfer HARUS ditolak —
	// lebih baik gagal daripada memindahkan dana tanpa dokumen. Master dihapus
	// sementara, bukan seam yang dilepas: sejak W-2 penomoran datang dari data,
	// jadi inilah cara kegagalan yang sebenarnya mungkin terjadi di produksi.
	if err := env.db.Exec(`DELETE FROM document_types WHERE tenant_id = ? AND code = ?`,
		cgTenant, document.TypeInternalTransfer).Error; err != nil {
		t.Fatalf("hapus jenis dokumen MTI: %v", err)
	}
	if _, err := env.svc.TransferToGroup(ctx, cgTenant, charge.TransferGroupRequest{
		GroupID: sum.GroupID, TargetGroupID: addon.GroupID,
		Amount: domain.FromInt(1_500_000), Date: day,
		Allocations: []charge.AllocationInput{
			{ChargeItemID: addon.Items[0].ItemID, Amount: domain.FromInt(1_500_000)},
		},
	}); !errors.Is(err, document.ErrTypeUnknown) {
		t.Fatalf("transfer tanpa master jenis dokumen harus ditolak, dapat %v", err)
	}
	mustEq(t, "saldo 2-2400 setelah transfer ditolak", env.balance2400(t), 1_500_000)
	if err := document.SeedDefaultDocumentTypes(ctx, env.db, cgTenant); err != nil {
		t.Fatalf("pulihkan jenis dokumen: %v", err)
	}

	if _, err := svc.TransferToGroup(ctx, cgTenant, charge.TransferGroupRequest{
		GroupID: sum.GroupID, TargetGroupID: addon.GroupID,
		Amount: domain.FromInt(1_500_000), Date: day,
		Allocations: []charge.AllocationInput{
			{ChargeItemID: addon.Items[0].ItemID, Amount: domain.FromInt(1_500_000)},
		},
	}); err != nil {
		t.Fatalf("transfer antar grup: %v", err)
	}
	// W-13 mengubah TUJUAN transfer ini, bukan mekanismenya. Dulu kedua sisi
	// jatuh di 2-2400 sehingga saldonya tidak bergerak — dan itu keliru: sisa
	// titipan pihak ketiga dipakai membayar kelebihan tanah, barang yang dijual
	// perusahaan sendiri. Sekarang dana benar-benar KELUAR dari akun titipan dan
	// menjadi uang muka penjualan, siap diakui sebagai pendapatan saat BAST.
	mustEq(t, "titipan pihak ketiga habis dipakai", env.balance2400(t), 0)
	mustEq(t, "menjadi uang muka produk tambahan", env.balanceOf(t, "2-2000"), 1_500_000)

	// T-1: transfer adalah PERPINDAHAN INTERNAL. Buktinya harus lengkap:
	// (a) terbit Memo Transfer Internal berseri MTI, (b) memo merujuk jurnal,
	// (c) TIDAK ada kwitansi baru — kas masuk tidak boleh dihitung dua kali.
	var trf struct {
		ID             uint64
		MemoNumber     *string
		JournalEntryID *uint64
	}
	if err := env.db.Raw(`SELECT id, memo_number, journal_entry_id FROM charge_settlements
		WHERE tenant_id = ? AND action = 'transfer_group' ORDER BY id DESC LIMIT 1`,
		cgTenant).Scan(&trf).Error; err != nil {
		t.Fatalf("baca settlement transfer: %v", err)
	}
	if trf.MemoNumber == nil || !strings.HasPrefix(*trf.MemoNumber, "MTI/") {
		t.Fatalf("transfer wajib bernomor memo MTI, dapat %v", trf.MemoNumber)
	}
	if trf.JournalEntryID == nil || *trf.JournalEntryID == 0 {
		t.Fatalf("memo tanpa rujukan jurnal = audit buntu")
	}
	memo, err := svc.TransferMemo(ctx, cgTenant, trf.ID)
	if err != nil {
		t.Fatalf("baca memo: %v", err)
	}
	moneyEq(t, "nilai memo", memo.Amount, 1_500_000)
	if memo.MemoNumber != *trf.MemoNumber || memo.SourceGroupID != sum.GroupID {
		t.Fatalf("isi memo tidak konsisten: %+v", memo)
	}
	var receiptAfterTransfer int64
	env.db.Raw(`SELECT COUNT(*) FROM receipts WHERE tenant_id = ? AND receipt_number LIKE 'KWT/%'`,
		cgTenant).Scan(&receiptAfterTransfer)
	if receiptAfterTransfer != 0 {
		t.Fatalf("transfer internal tidak boleh menerbitkan kwitansi pembayaran, dapat %d", receiptAfterTransfer)
	}

	addonSum, err := svc.GroupSummary(ctx, cgTenant, addon.GroupID)
	if err != nil {
		t.Fatalf("summary addon: %v", err)
	}
	moneyEq(t, "addon paid via transfer", addonSum.Paid, 1_500_000)
	moneyEq(t, "addon outstanding", addonSum.Outstanding, 4_500_000)

	// Sumber kini residual 0 → settle sukses.
	settle2, err := svc.TrueUpAndSettle(ctx, cgTenant, sum.GroupID, nil)
	if err != nil {
		t.Fatalf("settle final: %v", err)
	}
	if !settle2.Settled {
		t.Fatalf("grup harus settled: %+v", settle2.Summary)
	}

	// Invariant akhir: harga rumah tetap TIDAK tersentuh sepanjang lifecycle.
	mustEq(t, "harga rumah akhir", env.housePaid(t), 0)
}

func TestIntegration_ChargeGroup_VoidAndBASTGate(t *testing.T) {
	env := cgSetup(t)
	defer cgCleanup(t, env.db)
	ctx := context.Background()
	svc := env.svc
	day := time.Date(2026, 8, 2, 0, 0, 0, 0, time.UTC)

	sum, err := svc.CreateGroup(ctx, cgTenant, charge.CreateGroupRequest{
		SaleContractID: env.contractID,
		Kind:           charge.KindRealization,
		Label:          "Realisasi KPR",
		Items:          []charge.NewItemInput{{Label: "BPHTB", ChargeTypeCode: "bphtb", Amount: domain.FromInt(5_000_000)}},
	})
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	res, err := svc.ReceivePayment(ctx, cgTenant, charge.ReceivePaymentRequest{
		GroupID: sum.GroupID, Amount: domain.FromInt(3_000_000), Date: day,
		BankAccountCode: "1-1100",
		Allocations:     []charge.AllocationInput{{ChargeItemID: sum.Items[0].ItemID, Amount: domain.FromInt(3_000_000)}},
	})
	if err != nil {
		t.Fatalf("payment: %v", err)
	}

	// ── Void: jurnal pembalik + mirror negatif; histori tetap utuh.
	vsum, err := svc.VoidPayment(ctx, cgTenant, charge.VoidPaymentRequest{
		TerminID: res.TerminID, Reason: "salah nominal",
	})
	if err != nil {
		t.Fatalf("void: %v", err)
	}
	moneyEq(t, "paid pasca-void", vsum.Paid, 0)
	moneyEq(t, "outstanding pulih", vsum.Outstanding, 5_000_000)
	mustEq(t, "saldo 2-2400 nol", env.balance2400(t), 0)
	// Void kedua ditolak.
	if _, err := svc.VoidPayment(ctx, cgTenant, charge.VoidPaymentRequest{
		TerminID: res.TerminID, Reason: "dobel",
	}); !errors.Is(err, charge.ErrAlreadyVoided) {
		t.Fatalf("void kedua harus ErrAlreadyVoided, dapat: %v", err)
	}
	// Jurnal asli TIDAK dihapus: 2 jurnal posted (asli + pembalik).
	var n int64
	env.db.Raw(`SELECT COUNT(*) FROM journal_entries WHERE tenant_id = ? AND posted_at IS NOT NULL`, cgTenant).Scan(&n)
	if n != 2 {
		t.Fatalf("harus 2 jurnal posted (asli+pembalik), dapat %d", n)
	}

	// ── W-5: gate BAST DICABUT — outstanding realisasi tidak lagi punya
	// pintu penolakan. Yang tersisa hanyalah pelunasan biasa.
	if _, err := svc.ReceivePayment(ctx, cgTenant, charge.ReceivePaymentRequest{
		GroupID: sum.GroupID, Amount: domain.FromInt(5_000_000), Date: day,
		BankAccountCode: "1-1100",
		Allocations:     []charge.AllocationInput{{ChargeItemID: sum.Items[0].ItemID, Amount: domain.FromInt(5_000_000)}},
	}); err != nil {
		t.Fatalf("pelunasan: %v", err)
	}
	final, err := svc.GroupSummary(ctx, cgTenant, sum.GroupID)
	if err != nil {
		t.Fatalf("summary akhir: %v", err)
	}
	moneyEq(t, "outstanding akhir", final.Outstanding, 0)
}

// TestIntegration_EQ_RealizationDeposit menutup lubang TD-4 di Financial
// Consistency Suite.
//
// Suite di internal/reporting hanya mengunci 2-2300 (Titipan Notaris) — akun
// WARISAN yang isinya justru menyusut — sementara seluruh titipan biaya realisasi
// yang hidup menumpuk di 2-2400 tanpa satu pun pemeriksaan konsistensi. Cek ini
// tinggal di sini, bukan di reporting, karena charge meng-import reporting
// (AgingReport) sehingga arah import tidak boleh dibalik.
//
//	EQ_RealizationDeposit: saldo ledger 2-2400 == Σ residual grup (dokumen)
//
// dengan residual = dibayar − dibayarkan ke vendor − dikembalikan, rumus kanonik
// GroupSummary — bukan SQL tandingan yang bisa menyimpang diam-diam.
func TestIntegration_EQ_RealizationDeposit(t *testing.T) {
	env := cgSetup(t)
	defer cgCleanup(t, env.db)
	ctx := context.Background()
	svc := env.svc
	day := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)

	sum, err := svc.CreateGroup(ctx, cgTenant, charge.CreateGroupRequest{
		SaleContractID: env.contractID, Kind: charge.KindRealization, Label: "Biaya Realisasi EQ",
		Items: []charge.NewItemInput{
			{Label: "Notaris", ChargeTypeCode: "notaris", Amount: domain.FromInt(4_000_000)},
			{Label: "BPHTB", ChargeTypeCode: "bphtb", Amount: domain.FromInt(2_000_000)},
		},
	})
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	itemID := map[string]uint64{}
	for _, it := range sum.Items {
		itemID[it.Label] = it.ItemID
	}

	// Persamaan harus tetap berlaku di SETIAP langkah, bukan hanya di akhir —
	// itulah bedanya invariant dan kebetulan.
	assertEQ := func(step string) {
		t.Helper()
		groups, err := svc.ListGroupsByContract(ctx, cgTenant, env.contractID)
		if err != nil {
			t.Fatalf("%s: list grup: %v", step, err)
		}
		doc := domain.FromInt(0)
		for _, g := range groups {
			doc = doc.Add(g.Residual)
		}
		ledgerBal, err := domain.NewMoney(env.balance2400(t))
		if err != nil {
			t.Fatalf("%s: parse saldo: %v", step, err)
		}
		if !ledgerBal.Sub(doc).IsZero() {
			t.Fatalf("EQ_RealizationDeposit pecah setelah %s: ledger 2-2400 = %s, Σ residual = %s",
				step, ledgerBal, doc)
		}
	}
	assertEQ("grup dibuat (belum ada uang)")

	// 1. Bayar sebagian: 4jt dari 6jt.
	if _, err := svc.ReceivePayment(ctx, cgTenant, charge.ReceivePaymentRequest{
		GroupID: sum.GroupID, Amount: domain.FromInt(4_000_000), Date: day,
		BankAccountCode: "1-1100", IdempotencyKey: "eq-rd-1",
		Allocations: []charge.AllocationInput{
			{ChargeItemID: itemID["Notaris"], Amount: domain.FromInt(4_000_000)},
		},
	}); err != nil {
		t.Fatalf("terima pembayaran: %v", err)
	}
	assertEQ("pembayaran 4jt")
	mustEq(t, "titipan tertahan 4jt", env.balance2400(t), 4_000_000)

	// 2. Perusahaan membayarkan 3jt ke notaris — titipan berkurang, sisa 1jt.
	// (D-3: titipan hanya turun saat perusahaan membayar vendor.)
	if _, err := svc.RecordPayout(ctx, cgTenant, charge.PayoutRequest{
		ChargeItemID: itemID["Notaris"],
		Amount:       domain.FromInt(3_000_000), Date: day.AddDate(0, 0, 2),
		BankAccountCode: "1-1100", Vendor: "Notaris Andi, S.H.", IdempotencyKey: "eq-rd-payout-1",
	}); err != nil {
		t.Fatalf("payout: %v", err)
	}
	assertEQ("payout 3jt ke vendor")
	mustEq(t, "titipan sisa 1jt", env.balance2400(t), 1_000_000)

	// 3. Outstanding (2jt BPHTB belum dibayar) adalah TAGIHAN, bukan kewajiban
	// titipan — tidak boleh ikut mengembang di 2-2400.
	fin, err := svc.GroupSummary(ctx, cgTenant, sum.GroupID)
	if err != nil {
		t.Fatalf("summary: %v", err)
	}
	moneyEq(t, "outstanding grup", fin.Outstanding, 2_000_000)
	mustEq(t, "outstanding tidak menyentuh 2-2400", env.balance2400(t), 1_000_000)

	// 4. K-1 — uang titipan TIDAK PERNAH menjadi pendapatan. Dibuktikan di
	// ledger, bukan disimpulkan: tidak satu pun entri yang menyentuh 2-2400
	// punya baris 4-xxxx.
	var revenueTouches int64
	env.db.Raw(`SELECT COUNT(*) FROM journal_lines jl
		JOIN accounts a ON a.id = jl.account_id AND a.tenant_id = jl.tenant_id
		WHERE jl.tenant_id = ? AND a.code LIKE '4-%'
		  AND jl.journal_entry_id IN (
		      SELECT jl2.journal_entry_id FROM journal_lines jl2
		      JOIN accounts a2 ON a2.id = jl2.account_id AND a2.tenant_id = jl2.tenant_id
		      WHERE jl2.tenant_id = ? AND a2.code = '2-2400')`, cgTenant, cgTenant).
		Scan(&revenueTouches)
	if revenueTouches != 0 {
		t.Errorf("jurnal titipan realisasi menyentuh akun pendapatan: %d baris (harus 0)", revenueTouches)
	}
	mustEq(t, "harga rumah tidak tersentuh", env.housePaid(t), 0)
}

// TestIntegration_ChargeTypeMaster mengunci W-1: master Jenis Biaya Realisasi
// adalah SATU-SATUNYA sumber kebenaran untuk "uang customer ini mendarat di akun
// kewajiban mana". Yang diuji:
//
//  1. Seed default = 4 jenis titipan klien (Notaris/BPHTB/PDAM/Listrik) → 2-2400.
//  2. FAIL-CLOSED: item realization tanpa kode / kode asing / kode nonaktif
//     ditolak SEBELUM apa pun tersimpan.
//  3. Admin ber-CRUD tanpa programmer, dan setiap perubahan meninggalkan audit.
//  4. Akun kewajiban adalah ATRIBUT jenis biaya: satu grup boleh berakun campur,
//     dan uangnya terpecah per akun di jurnal, residual, dan rincian settlement.
//  5. Akun yang dikredit jurnal MENGIKUTI master, bukan konstanta.
func TestIntegration_ChargeTypeMaster(t *testing.T) {
	env := cgSetup(t)
	defer cgCleanup(t, env.db)
	ctx := context.Background()
	svc := env.svc
	day := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)

	// ── 1. Seed default: empat jenis titipan klien, semuanya ke 2-2400.
	types, err := svc.ListChargeTypes(ctx, cgTenant)
	if err != nil {
		t.Fatalf("list jenis biaya: %v", err)
	}
	if len(types) != 4 {
		t.Fatalf("seed default harus 4 jenis, dapat %d", len(types))
	}
	byCode := map[string]*charge.RealizationChargeType{}
	for _, ct := range types {
		byCode[ct.Code] = ct
		if ct.DepositAccountCode != "2-2400" {
			t.Fatalf("%s: akun titipan default = %s", ct.Code, ct.DepositAccountCode)
		}
		if ct.Treatment.RecognizesRevenue() {
			t.Fatalf("%s: biaya realisasi tidak boleh mengakui pendapatan (K-1)", ct.Code)
		}
	}
	for _, want := range []string{"notaris", "bphtb", "pdam", "listrik"} {
		if byCode[want] == nil {
			t.Fatalf("jenis %q tidak ter-seed", want)
		}
	}
	// Seed idempoten — dipanggil lagi tidak menggandakan master.
	if err := charge.SeedDefaultChargeTypes(ctx, env.db, cgTenant); err != nil {
		t.Fatalf("seed ulang: %v", err)
	}
	if again, _ := svc.ListChargeTypes(ctx, cgTenant); len(again) != 4 {
		t.Fatalf("seed tidak idempoten: %d baris", len(again))
	}

	// ── 2. FAIL-CLOSED. Tiga cara sebuah item bisa salah, semuanya ditolak.
	newGroup := func(items ...charge.NewItemInput) error {
		_, err := svc.CreateGroup(ctx, cgTenant, charge.CreateGroupRequest{
			SaleContractID: env.contractID, Kind: charge.KindRealization,
			Label: "Uji fail-closed", Items: items,
		})
		return err
	}
	if err := newGroup(charge.NewItemInput{Label: "Notaris", Amount: domain.FromInt(1_000_000)}); !errors.Is(err, charge.ErrChargeTypeRequired) {
		t.Fatalf("item tanpa jenis biaya harus ditolak, dapat: %v", err)
	}
	if err := newGroup(charge.NewItemInput{Label: "X", ChargeTypeCode: "iuran-rt", Amount: domain.FromInt(1_000_000)}); !errors.Is(err, charge.ErrChargeTypeUnknown) {
		t.Fatalf("jenis asing harus ditolak, dapat: %v", err)
	}
	// Tidak ada grup yang lolos → tidak ada sisa di DB.
	var leaked int64
	env.db.Raw(`SELECT COUNT(*) FROM charge_groups WHERE tenant_id = ? AND label = 'Uji fail-closed'`, cgTenant).Scan(&leaked)
	if leaked != 0 {
		t.Fatalf("penolakan harus atomik: %d grup tertinggal", leaked)
	}

	// ── 3. Admin menonaktifkan sebuah jenis → jenis itu tidak bisa dipakai lagi,
	// tetapi datanya TIDAK dihapus (audit trail).
	actor := uint64(42)
	off := false
	if _, err := svc.UpdateChargeType(ctx, cgTenant, byCode["listrik"].ID, charge.ChargeTypeUpdate{IsActive: &off}, &actor); err != nil {
		t.Fatalf("nonaktifkan listrik: %v", err)
	}
	if err := newGroup(charge.NewItemInput{Label: "Listrik", ChargeTypeCode: "listrik", Amount: domain.FromInt(1_000_000)}); !errors.Is(err, charge.ErrChargeTypeInactive) {
		t.Fatalf("jenis nonaktif harus ditolak, dapat: %v", err)
	}
	var stillThere int64
	env.db.Raw(`SELECT COUNT(*) FROM realization_charge_types WHERE tenant_id = ? AND code = 'listrik'`, cgTenant).Scan(&stillThere)
	if stillThere != 1 {
		t.Fatal("nonaktif bukan hapus — barisnya harus tetap ada")
	}

	// ── 4. Admin membuat jenis baru sendiri, dengan akun titipan berbeda.
	created, err := svc.CreateChargeType(ctx, cgTenant, charge.ChargeTypeInput{
		Code: " IPL ", Name: "Iuran Pengelolaan Lingkungan", DepositAccountCode: "2-2300",
	}, &actor)
	if err != nil {
		t.Fatalf("buat jenis baru: %v", err)
	}
	if created.Code != "ipl" {
		t.Fatalf("kode harus ter-normalisasi, dapat %q", created.Code)
	}
	if _, err := svc.CreateChargeType(ctx, cgTenant, charge.ChargeTypeInput{
		Code: "ipl", Name: "Duplikat", DepositAccountCode: "2-2400",
	}, &actor); !errors.Is(err, charge.ErrChargeTypeDuplicate) {
		t.Fatalf("kode ganda harus ditolak, dapat: %v", err)
	}
	if _, err := svc.CreateChargeType(ctx, cgTenant, charge.ChargeTypeInput{
		Code: "salah-akun", Name: "Akun bukan kewajiban", DepositAccountCode: "1-1100",
	}, &actor); !errors.Is(err, charge.ErrDepositAccountInvalid) {
		t.Fatalf("akun non-kewajiban harus ditolak, dapat: %v", err)
	}

	// ── 5. Koreksi arsitektur (owner, 2026-08-06): akun kewajiban adalah ATRIBUT
	// jenis biaya, bukan properti grup. Satu grup BOLEH memuat Notaris (2-2400)
	// dan IPL (2-2300) sekaligus — yang satu-per-grup adalah INVOICE, bukan akun.
	mixed, err := svc.CreateGroup(ctx, cgTenant, charge.CreateGroupRequest{
		SaleContractID: env.contractID, Kind: charge.KindRealization, Label: "Grup campur",
		Items: []charge.NewItemInput{
			{Label: "Notaris", ChargeTypeCode: "notaris", Amount: domain.FromInt(1_000_000)},
			{Label: "IPL", ChargeTypeCode: "ipl", Amount: domain.FromInt(500_000)},
		},
	})
	if err != nil {
		t.Fatalf("grup berakun campur harus diterima, dapat: %v", err)
	}
	// Setiap item membekukan akunnya sendiri (snapshot, seperti Label): jurnal
	// pembalik kelak tetap mengenai akun yang benar-benar dikredit dulu.
	gotAcc := map[string]string{}
	for _, it := range mixed.Items {
		gotAcc[it.ChargeTypeCode] = it.DepositAccount
	}
	if gotAcc["notaris"] != "2-2400" || gotAcc["ipl"] != "2-2300" {
		t.Fatalf("akun per item harus ikut masternya, dapat %+v", gotAcc)
	}

	// ── 6. Akun yang dikredit MENGIKUTI master. Grup ber-IPL mendarat di 2-2300,
	// bukan di konstanta 2-2400.
	sum, err := svc.CreateGroup(ctx, cgTenant, charge.CreateGroupRequest{
		SaleContractID: env.contractID, Kind: charge.KindRealization, Label: "Grup IPL",
		Items: []charge.NewItemInput{{Label: "IPL 2026", ChargeTypeCode: "ipl", Amount: domain.FromInt(2_000_000)}},
	})
	if err != nil {
		t.Fatalf("buat grup IPL: %v", err)
	}
	if len(sum.Items) != 1 || sum.Items[0].ChargeTypeCode != "ipl" {
		t.Fatalf("jenis biaya harus tersimpan di item, dapat %+v", sum.Items)
	}
	if _, err := svc.ReceivePayment(ctx, cgTenant, charge.ReceivePaymentRequest{
		GroupID: sum.GroupID, Amount: domain.FromInt(2_000_000), Date: day,
		BankAccountCode: "1-1100", IdempotencyKey: "cg-ipl-1",
		Allocations: []charge.AllocationInput{
			{ChargeItemID: sum.Items[0].ItemID, Amount: domain.FromInt(2_000_000)},
		},
	}); err != nil {
		t.Fatalf("terima pembayaran IPL: %v", err)
	}
	var s struct{ Bal string }
	env.db.Raw(`SELECT COALESCE(SUM(jl.credit - jl.debit),0) AS bal
		FROM journal_lines jl
		JOIN journal_entries je ON je.id = jl.journal_entry_id
		JOIN accounts a ON a.id = jl.account_id AND a.tenant_id = je.tenant_id
		WHERE je.tenant_id = ? AND je.posted_at IS NOT NULL AND a.code = '2-2300'`, cgTenant).Scan(&s)
	mustEq(t, "saldo 2-2300 (akun dari master)", s.Bal, 2_000_000)
	mustEq(t, "2-2400 tidak tersentuh", env.balance2400(t), 0)
	mustEq(t, "harga rumah tidak tersentuh", env.housePaid(t), 0)

	// ── 7. Setiap perubahan master meninggalkan jejak audit (append-only).
	hist, err := svc.ListMasterChanges(ctx, cgTenant, 0)
	if err != nil {
		t.Fatalf("riwayat master: %v", err)
	}
	var sawDeactivate, sawCreate bool
	for _, h := range hist {
		if h.Entity != charge.EntityRealizationChargeType {
			continue
		}
		if h.EntityCode == "listrik" && h.Field == "is_active" && h.NewValue == "false" {
			sawDeactivate = true
		}
		if h.EntityCode == "ipl" && h.Field == "created" {
			sawCreate = true
		}
		if h.ChangedBy == nil || *h.ChangedBy != actor {
			t.Fatalf("audit %s/%s: pelaku tidak tercatat", h.EntityCode, h.Field)
		}
	}
	if !sawDeactivate {
		t.Fatal("penonaktifan jenis biaya wajib tercatat di audit master")
	}
	if !sawCreate {
		t.Fatal("pembuatan jenis biaya wajib tercatat di audit master")
	}

	// ── 8. Uang di grup berakun campur: SATU pembayaran, jurnal terpecah per
	// akun kewajiban. Ini yang membuat koreksi arsitektur owner bermakna —
	// bukan sekadar "grup campur boleh dibuat".
	itemOf := map[string]uint64{}
	for _, it := range mixed.Items {
		itemOf[it.ChargeTypeCode] = it.ItemID
	}
	res, err := svc.ReceivePayment(ctx, cgTenant, charge.ReceivePaymentRequest{
		GroupID: mixed.GroupID, Amount: domain.FromInt(1_500_000), Date: day,
		BankAccountCode: "1-1100", IdempotencyKey: "cg-campur-1",
		Allocations: []charge.AllocationInput{
			{ChargeItemID: itemOf["notaris"], Amount: domain.FromInt(1_000_000)},
			{ChargeItemID: itemOf["ipl"], Amount: domain.FromInt(500_000)},
		},
	})
	if err != nil {
		t.Fatalf("terima pembayaran grup campur: %v", err)
	}
	// Satu jurnal, dua baris kredit — bukan dua jurnal, bukan satu akun gabungan.
	mustEq(t, "2-2400 pasca-pembayaran campur", env.balance2400(t), 1_000_000)
	mustEq(t, "2-2300 pasca-pembayaran campur", env.balanceOf(t, "2-2300"), 2_500_000)

	// Σ DepositResiduals == Residual, selalu. Rincian per akun tidak boleh
	// menjadi angka kedua yang berbeda dari total.
	sumRes := domain.Zero
	for _, d := range res.Summary.DepositResiduals {
		sumRes = sumRes.Add(d.Amount)
	}
	if !sumRes.Sub(res.Summary.Residual).IsZero() {
		t.Fatalf("Σ rincian titipan %s ≠ residual %s", sumRes, res.Summary.Residual)
	}
	if len(res.Summary.DepositResiduals) != 2 {
		t.Fatalf("residual harus dirinci ke 2 akun, dapat %+v", res.Summary.DepositResiduals)
	}

	// Refund di level grup menarik proporsional dari tiap akun (1.000:500 dari
	// 600.000 → 400.000 : 200.000) dan Σ-nya persis nominal refund.
	if _, err := svc.Refund(ctx, cgTenant, charge.RefundRequest{
		GroupID: mixed.GroupID, Amount: domain.FromInt(600_000), Date: day,
		BankAccountCode: "1-1100", Notes: "refund grup campur", IdempotencyKey: "cg-campur-refund",
	}); err != nil {
		t.Fatalf("refund grup campur: %v", err)
	}
	mustEq(t, "2-2400 pasca-refund campur", env.balance2400(t), 600_000)
	mustEq(t, "2-2300 pasca-refund campur", env.balanceOf(t, "2-2300"), 2_300_000)

	// Rincian per akun ikut tersimpan (bukan hanya ada di jurnal), supaya
	// transfer yang tertunda bisa dilanjutkan dengan pecahan yang sama persis.
	var lines []struct {
		DepositAccountCode string
		Amount             string
	}
	if err := env.db.Raw(`SELECT csl.deposit_account_code, csl.amount
		FROM charge_settlement_lines csl
		JOIN charge_settlements cs ON cs.id = csl.charge_settlement_id
		WHERE csl.tenant_id = ? AND cs.charge_group_id = ? AND cs.action = 'refund'
		ORDER BY csl.deposit_account_code`, cgTenant, mixed.GroupID).Scan(&lines).Error; err != nil {
		t.Fatalf("baca rincian settlement: %v", err)
	}
	if len(lines) != 2 {
		t.Fatalf("refund harus punya 2 baris rincian, dapat %+v", lines)
	}
	mustEq(t, "rincian 2-2300", lines[0].Amount, 200_000)
	mustEq(t, "rincian 2-2400", lines[1].Amount, 400_000)

	mustEq(t, "harga rumah tetap tidak tersentuh", env.housePaid(t), 0)
}

// arBalance: saldo DEBIT-normal 1-2000 dari jurnal POSTED (ledger = SoT).
func (e *cgEnv) arBalance(t *testing.T) string {
	t.Helper()
	var s struct{ Bal string }
	if err := e.db.Raw(`SELECT COALESCE(SUM(jl.debit - jl.credit),0) AS bal
		FROM journal_lines jl
		JOIN journal_entries je ON je.id = jl.journal_entry_id
		JOIN accounts a ON a.id = jl.account_id AND a.tenant_id = je.tenant_id
		WHERE je.tenant_id = ? AND je.posted_at IS NOT NULL AND a.code = '1-2000'`, cgTenant).
		Scan(&s).Error; err != nil {
		t.Fatalf("saldo 1-2000: %v", err)
	}
	return s.Bal
}

// recognitionSum: Σ delta tabel audit — sumber kebenaran kontribusi terposting.
func (e *cgEnv) recognitionSum(t *testing.T) string {
	t.Helper()
	var s struct{ Total string }
	if err := e.db.Raw(`SELECT COALESCE(SUM(delta),0) AS total
		FROM charge_receivable_recognitions WHERE tenant_id = ?`, cgTenant).
		Scan(&s).Error; err != nil {
		t.Fatalf("Σ pengakuan: %v", err)
	}
	return s.Total
}

// TestIntegration_W5_D3_ReceivableFromInvoice menguji ANGKA PERSIS dari keputusan
// klien D-3: biaya realisasi 20jt, saat BAST customer baru membayar 8jt, maka
//
//	Dr Kas 8jt / Dr Piutang Customer 12jt / Cr Titipan 20jt
//
// Piutangnya adalah 1-2000 — akun yang SAMA dengan piutang harga rumah, bukan
// mekanisme kedua.
//
// INV-REC-1 dibuktikan dari DB, bukan dari nilai balikan service: saldo 1-2000
// hasil penjumlahan baris jurnal harus sama dengan Σ delta tabel audit.
func TestIntegration_W5_D3_ReceivableFromInvoice(t *testing.T) {
	env := cgSetup(t)
	defer cgCleanup(t, env.db)
	ctx := context.Background()
	svc := env.svc
	day := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	due := time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC)

	sum, err := svc.CreateGroup(ctx, cgTenant, charge.CreateGroupRequest{
		SaleContractID: env.contractID,
		Kind:           charge.KindRealization,
		Label:          "Biaya Realisasi",
		Items: []charge.NewItemInput{
			{Label: "Notaris", ChargeTypeCode: "notaris", Amount: domain.FromInt(20_000_000), DueDate: &due},
		},
	})
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	itemID := sum.Items[0].ItemID

	// R-5: grup dibuat → BELUM ada jurnal apa pun. Tagihan bukan piutang.
	mustEq(t, "1-2000 sebelum invoice", env.arBalance(t), 0)
	mustEq(t, "2-2400 sebelum invoice", env.balance2400(t), 0)

	// Customer membayar 8jt sebelum ditagihkan → titipan murni.
	if _, err := svc.ReceivePayment(ctx, cgTenant, charge.ReceivePaymentRequest{
		GroupID: sum.GroupID, Amount: domain.FromInt(8_000_000), Date: day,
		BankAccountCode: "1-1100",
		Allocations:     []charge.AllocationInput{{ChargeItemID: itemID, Amount: domain.FromInt(8_000_000)}},
	}); err != nil {
		t.Fatalf("pembayaran 8jt: %v", err)
	}
	mustEq(t, "2-2400 setelah 8jt", env.balance2400(t), 8_000_000)
	mustEq(t, "1-2000 setelah 8jt", env.arBalance(t), 0)

	// Invoice terbit (di produksi ini juga yang terjadi otomatis saat BAST):
	// sisa 12jt menjadi Piutang Customer.
	_, _, recognized, err := svc.IssueInvoice(ctx, cgTenant, sum.GroupID, 0, due, "tagihan biaya realisasi")
	if err != nil {
		t.Fatalf("issue invoice: %v", err)
	}
	// Angka yang dikembalikan ke layar harus angka yang benar-benar diposting.
	moneyEq(t, "delta pengakuan yang dilaporkan", recognized, 12_000_000)
	mustEq(t, "1-2000 setelah invoice", env.arBalance(t), 12_000_000)
	mustEq(t, "2-2400 setelah invoice", env.balance2400(t), 20_000_000)
	mustEq(t, "INV-REC-1: Σ audit == saldo 1-2000", env.recognitionSum(t), 12_000_000)

	after, err := svc.GroupSummary(ctx, cgTenant, sum.GroupID)
	if err != nil {
		t.Fatalf("summary: %v", err)
	}
	moneyEq(t, "receivable grup", after.Receivable, 12_000_000)
	moneyEq(t, "unbilled grup", after.Unbilled, 0)

	// Aging membaca piutang yang SAMA: satu eksposur, satu angka (W-4 + W-5).
	rows, err := svc.ReceivableRowsByContract(ctx, cgTenant, env.contractID)
	if err != nil {
		t.Fatalf("receivable rows: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("harus 1 baris piutang realisasi, dapat %d", len(rows))
	}
	moneyEq(t, "aging amount", rows[0].Amount, 20_000_000)
	moneyEq(t, "aging paid", rows[0].PaidAmount, 8_000_000)
	if rows[0].InvoiceNumber == "" {
		t.Fatalf("baris piutang harus membawa nomor invoice")
	}

	// Pembayaran berikutnya HARUS mengurangi piutang itu (keputusan klien),
	// bukan menumpuk titipan baru.
	if _, err := svc.ReceivePayment(ctx, cgTenant, charge.ReceivePaymentRequest{
		GroupID: sum.GroupID, Amount: domain.FromInt(5_000_000), Date: day,
		BankAccountCode: "1-1100",
		Allocations:     []charge.AllocationInput{{ChargeItemID: itemID, Amount: domain.FromInt(5_000_000)}},
	}); err != nil {
		t.Fatalf("pembayaran 5jt: %v", err)
	}
	mustEq(t, "1-2000 setelah pelunasan sebagian", env.arBalance(t), 7_000_000)
	mustEq(t, "2-2400 tidak berubah", env.balance2400(t), 20_000_000)
	mustEq(t, "INV-REC-1 tetap", env.recognitionSum(t), 7_000_000)

	// R-4: melunasi sisanya membuat piutang NOL — tidak pernah bersaldo kredit.
	if _, err := svc.ReceivePayment(ctx, cgTenant, charge.ReceivePaymentRequest{
		GroupID: sum.GroupID, Amount: domain.FromInt(7_000_000), Date: day,
		BankAccountCode: "1-1100",
		Allocations:     []charge.AllocationInput{{ChargeItemID: itemID, Amount: domain.FromInt(7_000_000)}},
	}); err != nil {
		t.Fatalf("pelunasan: %v", err)
	}
	mustEq(t, "R-4: piutang lunas tepat nol", env.arBalance(t), 0)
	mustEq(t, "INV-REC-1 nol", env.recognitionSum(t), 0)
	mustEq(t, "harga rumah tidak tersentuh", env.housePaid(t), 0)
}

// TestIntegration_W5_VoidRestoresReceivable mengunci pembalikan yang benar:
// yang dibalik adalah apa yang DULU dikredit — piutang kembali hidup, titipan
// tidak ikut naik.
func TestIntegration_W5_VoidRestoresReceivable(t *testing.T) {
	env := cgSetup(t)
	defer cgCleanup(t, env.db)
	ctx := context.Background()
	svc := env.svc
	day := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	due := time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC)

	sum, err := svc.CreateGroup(ctx, cgTenant, charge.CreateGroupRequest{
		SaleContractID: env.contractID,
		Kind:           charge.KindRealization,
		Label:          "Biaya Realisasi",
		Items: []charge.NewItemInput{
			{Label: "BPHTB", ChargeTypeCode: "bphtb", Amount: domain.FromInt(10_000_000), DueDate: &due},
		},
	})
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	itemID := sum.Items[0].ItemID

	if _, _, _, err := svc.IssueInvoice(ctx, cgTenant, sum.GroupID, 0, due, ""); err != nil {
		t.Fatalf("issue invoice: %v", err)
	}
	mustEq(t, "piutang terbit", env.arBalance(t), 10_000_000)

	res, err := svc.ReceivePayment(ctx, cgTenant, charge.ReceivePaymentRequest{
		GroupID: sum.GroupID, Amount: domain.FromInt(4_000_000), Date: day,
		BankAccountCode: "1-1100",
		Allocations:     []charge.AllocationInput{{ChargeItemID: itemID, Amount: domain.FromInt(4_000_000)}},
	})
	if err != nil {
		t.Fatalf("pembayaran: %v", err)
	}
	mustEq(t, "piutang setelah bayar", env.arBalance(t), 6_000_000)
	mustEq(t, "titipan setelah bayar", env.balance2400(t), 10_000_000)

	if _, err := svc.VoidPayment(ctx, cgTenant, charge.VoidPaymentRequest{
		TerminID: res.TerminID, Reason: "salah nominal",
	}); err != nil {
		t.Fatalf("void: %v", err)
	}
	mustEq(t, "piutang hidup kembali", env.arBalance(t), 10_000_000)
	mustEq(t, "titipan tidak ikut naik", env.balance2400(t), 10_000_000)
	mustEq(t, "INV-REC-1 pasca-void", env.recognitionSum(t), 10_000_000)
}

// TestIntegration_W5_CatchUpIdempotent mengunci R-3: jurnal susulan menutup
// invoice yang terbit sebelum W-5, dan putaran kedua tidak menerbitkan apa pun.
func TestIntegration_W5_CatchUpIdempotent(t *testing.T) {
	env := cgSetup(t)
	defer cgCleanup(t, env.db)
	ctx := context.Background()
	svc := env.svc
	due := time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC)

	sum, err := svc.CreateGroup(ctx, cgTenant, charge.CreateGroupRequest{
		SaleContractID: env.contractID,
		Kind:           charge.KindRealization,
		Label:          "Biaya Realisasi Lama",
		Items: []charge.NewItemInput{
			{Label: "PDAM", ChargeTypeCode: "pdam", Amount: domain.FromInt(3_000_000), DueDate: &due},
		},
	})
	if err != nil {
		t.Fatalf("create group: %v", err)
	}

	// Simulasi data PRA-W-5: invoice ada, jurnal pengakuan tidak.
	if _, _, _, err := svc.IssueInvoice(ctx, cgTenant, sum.GroupID, 0, due, ""); err != nil {
		t.Fatalf("issue invoice: %v", err)
	}
	env.db.Exec(`DELETE jl FROM journal_lines jl
		JOIN charge_receivable_recognitions r ON r.journal_entry_id = jl.journal_entry_id
		WHERE r.tenant_id = ?`, cgTenant)
	env.db.Exec(`DELETE je FROM journal_entries je
		JOIN charge_receivable_recognitions r ON r.journal_entry_id = je.id
		WHERE r.tenant_id = ?`, cgTenant)
	env.db.Exec(`DELETE FROM charge_receivable_recognitions WHERE tenant_id = ?`, cgTenant)
	env.db.Exec(`UPDATE charge_items SET recognized_amount = '0.0000', recognized_at = NULL,
		recognized_due_date = NULL, recognized_invoice_id = NULL WHERE tenant_id = ?`, cgTenant)
	mustEq(t, "kondisi awal pra-W-5: piutang belum ada", env.arBalance(t), 0)

	// Dry-run tidak boleh menyisakan jejak apa pun.
	rep, err := svc.CatchUpRecognition(ctx, cgTenant, false, nil)
	if err != nil {
		t.Fatalf("catch-up dry-run: %v", err)
	}
	moneyEq(t, "dry-run menghitung 3jt", rep.Posted, 3_000_000)
	mustEq(t, "dry-run tidak menulis", env.arBalance(t), 0)

	if _, err := svc.CatchUpRecognition(ctx, cgTenant, true, nil); err != nil {
		t.Fatalf("catch-up apply: %v", err)
	}
	mustEq(t, "piutang susulan terbit", env.arBalance(t), 3_000_000)

	rep2, err := svc.CatchUpRecognition(ctx, cgTenant, true, nil)
	if err != nil {
		t.Fatalf("catch-up kedua: %v", err)
	}
	moneyEq(t, "putaran kedua tidak memposting apa pun", rep2.Posted, 0)
	mustEq(t, "saldo tidak berubah", env.arBalance(t), 3_000_000)
}

// TestIntegration_W5_BASTMenerbitkanInvoiceOtomatis mengunci inti keputusan D-3:
// BAST TIDAK PERNAH tertahan karena biaya realisasi belum lunas, dan sisanya
// tidak menguap — pada detik BAST invoice-nya terbit sendiri dan sisa itu
// menjadi Piutang Customer di piutang yang sama (bukan piutang kedua).
//
// Yang diuji adalah fungsi yang benar-benar dipanggil `sale` di dalam transaksi
// serah terima (GORMRepository.realizRec), bukan tiruan alurnya.
func TestIntegration_W5_BASTMenerbitkanInvoiceOtomatis(t *testing.T) {
	env := cgSetup(t)
	defer cgCleanup(t, env.db)
	ctx := context.Background()
	svc := env.svc
	day := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	due := time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC)

	// Dua grup dengan keadaan berbeda: satu sudah ditagihkan sebagian lewat
	// invoice sendiri, satu belum tersentuh. BAST harus menutup keduanya tanpa
	// menagih ulang yang sudah menjadi piutang (INV-CG-1).
	belum, err := svc.CreateGroup(ctx, cgTenant, charge.CreateGroupRequest{
		SaleContractID: env.contractID,
		Kind:           charge.KindRealization,
		Label:          "Belum Ditagihkan",
		Items: []charge.NewItemInput{
			{Label: "Notaris", ChargeTypeCode: "notaris", Amount: domain.FromInt(9_000_000), DueDate: &due},
		},
	})
	if err != nil {
		t.Fatalf("create group belum: %v", err)
	}
	sudah, err := svc.CreateGroup(ctx, cgTenant, charge.CreateGroupRequest{
		SaleContractID: env.contractID,
		Kind:           charge.KindRealization,
		Label:          "Sudah Ditagihkan",
		Items: []charge.NewItemInput{
			{Label: "BPHTB", ChargeTypeCode: "bphtb", Amount: domain.FromInt(6_000_000), DueDate: &due},
		},
	})
	if err != nil {
		t.Fatalf("create group sudah: %v", err)
	}
	if _, _, _, err := svc.IssueInvoice(ctx, cgTenant, sudah.GroupID, 0, due, ""); err != nil {
		t.Fatalf("issue invoice grup kedua: %v", err)
	}

	// Uang yang masuk sebelum ditagihkan tetap titipan murni — ia hanya memotong
	// piutang yang terbentuk nanti, tidak menghalangi BAST.
	if _, err := svc.ReceivePayment(ctx, cgTenant, charge.ReceivePaymentRequest{
		GroupID: belum.GroupID, Amount: domain.FromInt(2_000_000), Date: day,
		BankAccountCode: "1-1100",
		Allocations:     []charge.AllocationInput{{ChargeItemID: belum.Items[0].ItemID, Amount: domain.FromInt(2_000_000)}},
	}); err != nil {
		t.Fatalf("pembayaran pra-tagih: %v", err)
	}
	mustEq(t, "sebelum BAST: piutang hanya dari grup yang sudah ditagihkan", env.arBalance(t), 6_000_000)

	// Inilah yang dijalankan `sale` di dalam transaksi BAST.
	if err := env.db.Transaction(func(tx *gorm.DB) error {
		return svc.RecognizeOnBASTInTx(ctx, tx, cgTenant, env.unitID, nil)
	}); err != nil {
		t.Fatalf("D-3: BAST tidak boleh gagal karena biaya realisasi: %v", err)
	}

	// 9jt − 2jt titipan = 7jt piutang baru, ditambah 6jt yang sudah ada.
	mustEq(t, "piutang pasca-BAST", env.arBalance(t), 13_000_000)
	mustEq(t, "titipan pasca-BAST", env.balance2400(t), 15_000_000)
	mustEq(t, "INV-REC-1: Σ audit == saldo 1-2000", env.recognitionSum(t), 13_000_000)

	after, err := svc.GroupSummary(ctx, cgTenant, belum.GroupID)
	if err != nil {
		t.Fatalf("summary: %v", err)
	}
	moneyEq(t, "sisa yang belum ditagihkan habis", after.Unbilled, 0)
	moneyEq(t, "sisa menjadi piutang", after.Receivable, 7_000_000)
	if after.InvoiceNumber == "" {
		t.Fatalf("grup harus membawa nomor invoice yang terbit otomatis")
	}

	// D-3: tidak ada piutang kedua — seluruh eksposur tetap satu daftar.
	rows, err := svc.ReceivableRowsByContract(ctx, cgTenant, env.contractID)
	if err != nil {
		t.Fatalf("receivable rows: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("harus 2 baris piutang realisasi (satu per tagihan), dapat %d", len(rows))
	}
	for _, r := range rows {
		if r.InvoiceNumber == "" {
			t.Fatalf("baris piutang tanpa nomor invoice: %+v", r)
		}
	}

	// BAST kedua (mis. retry) tidak boleh menerbitkan invoice atau jurnal baru.
	if err := env.db.Transaction(func(tx *gorm.DB) error {
		return svc.RecognizeOnBASTInTx(ctx, tx, cgTenant, env.unitID, nil)
	}); err != nil {
		t.Fatalf("BAST ulang: %v", err)
	}
	mustEq(t, "idempoten: piutang tidak berlipat", env.arBalance(t), 13_000_000)
	mustEq(t, "idempoten: titipan tidak berubah", env.balance2400(t), 15_000_000)
}

// TestIntegration_W13_ProdukTambahanMenjadiPendapatan menguji koreksi klien:
// "Produk kelebihan tanah harus sinkron ke produk tambahan pas mau jual unit,
// bukan diperlakukan sama cara jualnya seperti unit rumah."
//
// Yang dibuktikan bukan sekadar "kolomnya terisi", melainkan uangnya SAMPAI ke
// laba rugi. Sebelum W-13, kelebihan tanah terjual dengan kasnya mendarat di
// 2-2400 Titipan Realisasi dan berhenti di sana selamanya: akun kewajiban
// kepada pihak ketiga yang dipakai untuk barang milik perusahaan sendiri.
func TestIntegration_W13_ProdukTambahanMenjadiPendapatan(t *testing.T) {
	env := cgSetup(t)
	defer cgCleanup(t, env.db)
	ctx := context.Background()
	svc := env.svc
	day := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	due := time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC)

	addonReq := func(items ...charge.NewItemInput) charge.CreateGroupRequest {
		return charge.CreateGroupRequest{
			SaleContractID: env.contractID,
			Kind:           charge.KindAddon,
			Label:          "Produk Tambahan",
			Items:          items,
		}
	}

	// ── 1. Fail-closed: teks bebas tidak lagi cukup ─────────────────────────
	// Inilah lubangnya dulu — label diketik manusia, akun diambil dari default
	// kolom, dan tidak ada satu pun titik yang bertanya "ini produk apa".
	if _, err := svc.CreateGroup(ctx, cgTenant, addonReq(
		charge.NewItemInput{Label: "Kelebihan tanah 12 m²", Amount: domain.FromInt(6_000_000)},
	)); !errors.Is(err, charge.ErrProductRequired) {
		t.Fatalf("item addon tanpa produk harus ditolak, dapat %v", err)
	}
	if _, err := svc.CreateGroup(ctx, cgTenant, addonReq(
		charge.NewItemInput{Label: "Ngarang", ProductCode: "tidak_ada", Amount: domain.FromInt(1_000_000)},
	)); !errors.Is(err, charge.ErrProductUnresolved) {
		t.Fatalf("produk di luar katalog harus ditolak, dapat %v", err)
	}
	// Rumah dijual sebagai UNIT karena unitlah yang menerima alokasi biaya dan
	// melahirkan HPP. Sebagai baris addon ia akan mengakui pendapatan tanpa
	// lawan HPP — laba yang terlalu besar, permanen, dan sulit dilacak.
	if _, err := svc.CreateGroup(ctx, cgTenant, addonReq(
		charge.NewItemInput{Label: "Rumah kedua", ProductCode: "rumah", Amount: domain.FromInt(500_000_000)},
	)); !errors.Is(err, charge.ErrProductNotAddon) {
		t.Fatalf("produk properti tidak boleh dijual sebagai addon, dapat %v", err)
	}
	// LT-8 (kelebihan-tanah-final-architecture §G): kelebihan_tanah sekarang
	// berkategori `land` (migration 000083) — ikut HPP sama seperti properti,
	// jadi jalur addon lama membekukannya lewat guard generik yang sama.
	if _, err := svc.CreateGroup(ctx, cgTenant, addonReq(
		charge.NewItemInput{Label: "Kelebihan tanah 12 m²", ProductCode: "kelebihan_tanah", Amount: domain.FromInt(6_000_000)},
	)); !errors.Is(err, charge.ErrProductNotAddon) {
		t.Fatalf("kelebihan tanah (kategori land, LT-8) tidak boleh lagi dijual sebagai addon, dapat %v", err)
	}

	// ── 2. Akun datang dari master, bukan dari yang mengetik ────────────────
	g, err := svc.CreateGroup(ctx, cgTenant, addonReq(
		charge.NewItemInput{
			Label: "Souvenir", ProductCode: "souvenir",
			Amount: domain.FromInt(6_000_000), DueDate: &due,
		},
	))
	if err != nil {
		t.Fatalf("create addon: %v", err)
	}
	var snap struct {
		Product string `gorm:"column:product_code"`
		Deposit string `gorm:"column:deposit_account_code"`
		Revenue string `gorm:"column:revenue_account_code"`
	}
	if err := env.db.Raw(`SELECT product_code, deposit_account_code, revenue_account_code
		FROM charge_items WHERE id = ?`, g.Items[0].ItemID).Scan(&snap).Error; err != nil {
		t.Fatalf("baca snapshot item: %v", err)
	}
	if snap.Product != "souvenir" || snap.Deposit != "2-2000" || snap.Revenue != "4-2000" {
		t.Fatalf("snapshot akun addon salah: %+v", snap)
	}

	// ── 3. Kas masuk = KEWAJIBAN, bukan pendapatan (Invariant #7) ───────────
	if _, err := svc.ReceivePayment(ctx, cgTenant, charge.ReceivePaymentRequest{
		GroupID: g.GroupID, Amount: domain.FromInt(2_000_000), Date: day,
		BankAccountCode: "1-1100",
		Allocations:     []charge.AllocationInput{{ChargeItemID: g.Items[0].ItemID, Amount: domain.FromInt(2_000_000)}},
	}); err != nil {
		t.Fatalf("terima pembayaran addon: %v", err)
	}
	mustEq(t, "uang muka produk tambahan", env.balanceOf(t, "2-2000"), 2_000_000)
	mustEq(t, "titipan realisasi TIDAK tersentuh", env.balance2400(t), 0)
	mustEq(t, "belum ada pendapatan sebelum pengakuan", env.balanceOf(t, "4-2000"), 0)

	// ── 4. BAST: kewajiban → pendapatan + piutang ───────────────────────────
	if err := env.db.Transaction(func(tx *gorm.DB) error {
		return svc.RecognizeOnBASTInTx(ctx, tx, cgTenant, env.unitID, nil)
	}); err != nil {
		t.Fatalf("pengakuan addon saat BAST: %v", err)
	}
	mustEq(t, "pendapatan produk tambahan diakui penuh", env.balanceOf(t, "4-2000"), 6_000_000)
	mustEq(t, "uang muka ditutup", env.balanceOf(t, "2-2000"), 0)
	mustEq(t, "sisa yang belum dibayar menjadi piutang", env.arBalance(t), 4_000_000)
	mustEq(t, "INV-REC-1: Σ audit == saldo 1-2000", env.recognitionSum(t), 4_000_000)

	// ── 5. Idempoten — BAST diulang tidak melipatgandakan pendapatan ────────
	if err := env.db.Transaction(func(tx *gorm.DB) error {
		return svc.RecognizeOnBASTInTx(ctx, tx, cgTenant, env.unitID, nil)
	}); err != nil {
		t.Fatalf("BAST ulang: %v", err)
	}
	mustEq(t, "idempoten: pendapatan tidak berlipat", env.balanceOf(t, "4-2000"), 6_000_000)
	mustEq(t, "idempoten: piutang tidak berlipat", env.arBalance(t), 4_000_000)

	// ── 6. Piutangnya masuk daftar yang sama, dengan sumber yang jujur ──────
	// Menumpang label "Realisasi" akan membuat penagih menyangka ada vendor
	// yang menunggu dibayar, padahal tidak ada.
	rows, err := svc.ReceivableRowsByContract(ctx, cgTenant, env.contractID)
	if err != nil {
		t.Fatalf("receivable rows: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("harus 1 baris piutang addon, dapat %d", len(rows))
	}
	if rows[0].Source != receivable.SourceAddon {
		t.Fatalf("sumber piutang addon = %q, harap %q", rows[0].Source, receivable.SourceAddon)
	}
	moneyEq(t, "nominal baris piutang", rows[0].Amount, 6_000_000)
	moneyEq(t, "yang sudah dibayar", rows[0].PaidAmount, 2_000_000)
}

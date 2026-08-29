//go:build integration

package tax_test

// W-3.0 B-2 — atomisitas PayTax terhadap MySQL nyata.
//
// PayTax menulis empat kali: buat jurnal, posting, ubah status obligation,
// simpan payment. Sebelum W-3.0 keempatnya berjalan tanpa transaksi, sehingga
// gagal pada tulisan terakhir meninggalkan kondisi mustahil: kas SUDAH
// dikreditkan di ledger, tetapi kewajiban pajak masih outstanding dan tidak ada
// baris pembayaran yang bisa dilacak. Itu persis jenis cash movement tak
// berdokumen yang dilarang INV-DOC-1.
//
// Metode: TxRunner yang membungkus transaksi asli lalu menggagalkan SavePayment.
// Prasyarat: TEST_DB_DSN di-set + DB termigrasi.

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"esaproperti/internal/document"
	"esaproperti/internal/domain"
	"esaproperti/internal/ledger"
	"esaproperti/internal/tax"
)

const txaTenant uint64 = 9_900_031

var errTxaInjected = errors.New("kegagalan disuntik saat menyimpan payment")

func txaConnect(t *testing.T) *gorm.DB {
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

func txaCleanup(t *testing.T, db *gorm.DB) {
	t.Helper()
	for _, tbl := range []string{"tax_payments", "tax_obligations", "documents", "document_sequences", "journal_lines", "journal_entries", "accounts"} {
		if err := db.Exec("DELETE FROM "+tbl+" WHERE tenant_id = ?", txaTenant).Error; err != nil {
			t.Fatalf("cleanup %s: %v", tbl, err)
		}
	}
	// Master jenis dokumen — produksi menanamnya saat tenant dibuat; resolver
	// fail-closed, jadi jalur kas tidak bisa menerbitkan dokumen tanpa ini.
	if err := document.SeedDefaultDocumentTypes(context.Background(), db, txaTenant); err != nil {
		t.Fatalf("seed jenis dokumen: %v", err)
	}
}

func txaSeed(t *testing.T, db *gorm.DB) (obligationID uint64) {
	t.Helper()
	bank := &ledger.Account{
		TenantID: txaTenant, Code: "1-1300", Name: "Bank Pajak W30",
		Type: domain.AccountAsset, NormalBalance: domain.NormalBalanceDebit,
		IsActive: true, Category: ledger.CategoryBank,
	}
	hutang := &ledger.Account{
		TenantID: txaTenant, Code: "2-4000", Name: "Hutang PPh Final W30",
		Type: domain.AccountLiability, NormalBalance: domain.NormalBalanceCredit,
		IsActive: true,
	}
	for _, a := range []*ledger.Account{bank, hutang} {
		if err := db.Create(a).Error; err != nil {
			t.Fatalf("seed akun %s: %v", a.Code, err)
		}
	}

	amount, _ := domain.NewMoney("25000000")
	transfer, _ := domain.NewMoney("1000000000")
	ob := &tax.TaxObligation{
		TenantID: txaTenant, RateCode: "pph-final-property",
		TransferValue: transfer, Rate: decimal.RequireFromString("0.025"),
		TaxAmount: amount, Status: tax.ObligationStatusOutstanding,
		AccrualDate: time.Now(), JournalEntryID: 0,
	}
	if err := db.Create(ob).Error; err != nil {
		t.Fatalf("seed obligation: %v", err)
	}
	return ob.ID
}

// ── Seam penyuntik kegagalan ─────────────────────────────────────────────────

type txaFailSavePayment struct{ tax.TaxStore }

func (txaFailSavePayment) SavePayment(_ context.Context, _ *tax.TaxPayment) error {
	return errTxaInjected
}

// txaWrapRunner memakai transaksi asli repository, hanya SavePayment yang gagal.
type txaWrapRunner struct{ inner tax.TxRunner }

func (w txaWrapRunner) InTx(ctx context.Context, fn func(tax.JournalWriter, tax.TaxStore) error) error {
	return w.inner.InTx(ctx, func(jw tax.JournalWriter, st tax.TaxStore) error {
		return fn(jw, txaFailSavePayment{st})
	})
}

func txaCount(t *testing.T, db *gorm.DB, table string) int64 {
	t.Helper()
	var n int64
	if err := db.Table(table).Where("tenant_id = ?", txaTenant).Count(&n).Error; err != nil {
		t.Fatalf("hitung %s: %v", table, err)
	}
	return n
}

func txaService(db *gorm.DB, runner tax.TxRunner) *tax.Service {
	ledgerRepo := ledger.NewGORMRepository(db)
	posting := ledger.NewPostingService(ledgerRepo, ledgerRepo).
		WithPeriodChecker(ledgerRepo).
		WithJournalTx(ledgerRepo)
	repo := tax.NewGORMRepository(db, posting)
	if runner == nil {
		runner = repo
	}
	return tax.NewService(repo, repo, repo, repo, tax.WithTxRunner(runner))
}

// ── B-2 ──────────────────────────────────────────────────────────────────────

// Gagal pada tulisan terakhir tidak boleh menyisakan jurnal kas terposting.
func TestW30_PayTax_GagalSimpanPayment_TidakMenyisakanJurnalKas(t *testing.T) {
	db := txaConnect(t)
	txaCleanup(t, db)
	t.Cleanup(func() { txaCleanup(t, db) })
	obligationID := txaSeed(t, db)

	ledgerRepo := ledger.NewGORMRepository(db)
	posting := ledger.NewPostingService(ledgerRepo, ledgerRepo).WithJournalTx(ledgerRepo)
	realRunner := tax.NewGORMRepository(db, posting)
	svc := txaService(db, txaWrapRunner{inner: realRunner})

	_, err := svc.PayTax(context.Background(), txaTenant, tax.PayTaxRequest{
		ObligationID: obligationID, BankAccountCode: "1-1300", PaymentDate: time.Now(),
	})
	if !errors.Is(err, errTxaInjected) {
		t.Fatalf("expected kegagalan disuntik, got %v", err)
	}

	if n := txaCount(t, db, "journal_entries"); n != 0 {
		t.Fatalf("jurnal kas harus ikut dibatalkan; tersisa %d", n)
	}
	if n := txaCount(t, db, "tax_payments"); n != 0 {
		t.Fatalf("tidak boleh ada payment; ada %d", n)
	}
	var ob tax.TaxObligation
	if err := db.First(&ob, "id = ? AND tenant_id = ?", obligationID, txaTenant).Error; err != nil {
		t.Fatalf("baca obligation: %v", err)
	}
	if ob.Status != tax.ObligationStatusOutstanding {
		t.Fatalf("status obligation harus tetap outstanding, got %q", ob.Status)
	}
}

// Jalur normal: satu jurnal terposting, satu payment, obligation lunas.
func TestW30_PayTax_Sukses_KonsistenTigaSisi(t *testing.T) {
	db := txaConnect(t)
	txaCleanup(t, db)
	t.Cleanup(func() { txaCleanup(t, db) })
	obligationID := txaSeed(t, db)

	svc := txaService(db, nil)

	payment, err := svc.PayTax(context.Background(), txaTenant, tax.PayTaxRequest{
		ObligationID: obligationID, BankAccountCode: "1-1300", PaymentDate: time.Now(),
	})
	if err != nil {
		t.Fatalf("PayTax: %v", err)
	}
	if payment.JournalEntryID == 0 {
		t.Fatal("payment harus tertaut ke jurnal")
	}

	var entry ledger.JournalEntry
	if err := db.First(&entry, "id = ? AND tenant_id = ?", payment.JournalEntryID, txaTenant).Error; err != nil {
		t.Fatalf("baca jurnal: %v", err)
	}
	if entry.PostedAt == nil {
		t.Fatal("jurnal pelunasan pajak harus terposting")
	}
	var ob tax.TaxObligation
	if err := db.First(&ob, "id = ? AND tenant_id = ?", obligationID, txaTenant).Error; err != nil {
		t.Fatalf("baca obligation: %v", err)
	}
	if ob.Status != tax.ObligationStatusPaid {
		t.Fatalf("obligation harus paid, got %q", ob.Status)
	}
}

// Kas/bank sekarang ditentukan COA (B-1): akun beban ditolak walau kodenya ada.
func TestW30_PayTax_AkunBukanKasBank_Ditolak(t *testing.T) {
	db := txaConnect(t)
	txaCleanup(t, db)
	t.Cleanup(func() { txaCleanup(t, db) })
	obligationID := txaSeed(t, db)

	beban := &ledger.Account{
		TenantID: txaTenant, Code: "5-3000", Name: "Beban W30",
		Type: domain.AccountExpense, NormalBalance: domain.NormalBalanceDebit, IsActive: true,
	}
	if err := db.Create(beban).Error; err != nil {
		t.Fatalf("seed akun beban: %v", err)
	}

	svc := txaService(db, nil)
	_, err := svc.PayTax(context.Background(), txaTenant, tax.PayTaxRequest{
		ObligationID: obligationID, BankAccountCode: "5-3000", PaymentDate: time.Now(),
	})
	if !errors.Is(err, tax.ErrInvalidBankAccount) {
		t.Fatalf("expected ErrInvalidBankAccount, got %v", err)
	}
	if n := txaCount(t, db, "journal_entries"); n != 0 {
		t.Fatalf("tidak boleh ada jurnal; ada %d", n)
	}
}

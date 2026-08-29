//go:build integration

package notary_test

// UAT Batch 2 §3 + W-1 — Titipan Notaris setelah jalur masuknya ditutup.
//
// Sejak W-1 titipan notaris BARU masuk lewat Charge Group (2-2400). Modul ini
// tinggal menjaga uang warisan di 2-2300: tidak menerima lagi, tetapi wajib
// tetap bisa dibayarkan ke notaris dan tetap terekonsiliasi. Yang diuji:
//
//	1. ReceiveDeposit ditolak — di service, bukan hanya di route.
//	2. Titipan warisan tetap bisa di-payout (Dr 2-2300 / Cr Bank) → saldo nol.
//	3. Anti double-pay masih berlaku.
//	4. Liability murni: tidak pernah menyentuh 4-xxxx.

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"esaproperti/internal/document"
	"esaproperti/internal/domain"
	"esaproperti/internal/ledger"
	"esaproperti/internal/notary"
)

const ntTenant uint64 = 9_900_043

func ntConnect(t *testing.T) *gorm.DB {
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

func ntCleanup(t *testing.T, db *gorm.DB) {
	t.Helper()
	for _, tbl := range []string{"notary_deposits", "documents", "document_sequences", "journal_lines", "journal_entries", "accounts", "customers"} {
		if err := db.Exec("DELETE FROM "+tbl+" WHERE tenant_id = ?", ntTenant).Error; err != nil {
			t.Fatalf("cleanup %s: %v", tbl, err)
		}
	}
	// Master jenis dokumen — produksi menanamnya saat tenant dibuat; resolver
	// fail-closed, jadi jalur kas tidak bisa menerbitkan dokumen tanpa ini.
	if err := document.SeedDefaultDocumentTypes(context.Background(), db, ntTenant); err != nil {
		t.Fatalf("seed jenis dokumen: %v", err)
	}
}

func ntBalance(t *testing.T, db *gorm.DB, code string) string {
	t.Helper()
	var s struct{ Bal string }
	if err := db.Raw(`SELECT COALESCE(SUM(jl.credit - jl.debit),0) AS bal
		FROM journal_lines jl
		JOIN journal_entries je ON je.id = jl.journal_entry_id
		JOIN accounts a ON a.id = jl.account_id
		WHERE je.tenant_id = ? AND je.posted_at IS NOT NULL AND a.code = ?`, ntTenant, code).
		Scan(&s).Error; err != nil {
		t.Fatalf("saldo %s: %v", code, err)
	}
	return s.Bal
}

// TestIntegration_NotaryIntakeClosed — W-1: satu jenis uang, satu jalur masuk.
func TestIntegration_NotaryIntakeClosed(t *testing.T) {
	db := ntConnect(t)
	ntCleanup(t, db)
	defer ntCleanup(t, db)
	ctx := context.Background()

	if err := ledger.SeedCOA(ctx, db, ntTenant); err != nil {
		t.Fatalf("seed COA: %v", err)
	}
	var customerID uint64
	db.Exec(`INSERT INTO customers (tenant_id, code, name) VALUES (?,?,?)`, ntTenant, "NT-C1", "Sari")
	db.Raw("SELECT LAST_INSERT_ID()").Scan(&customerID)

	svc := notary.NewService(db)

	// Ditolak dengan permintaan yang SEPENUHNYA valid — penolakan datang dari
	// keputusan arsitektur, bukan dari validasi input yang kebetulan gagal.
	got, err := svc.ReceiveDeposit(ctx, ntTenant, notary.ReceiveDepositRequest{
		CustomerID: customerID, Amount: domain.FromInt(7_500_000),
		BankAccountCode: "1-1300", NotaryName: "Notaris Andi, S.H.",
		ReceivedAt: time.Date(2026, 7, 30, 0, 0, 0, 0, time.UTC),
	})
	if !errors.Is(err, notary.ErrNotaryIntakeClosed) {
		t.Fatalf("ReceiveDeposit harus ditolak ErrNotaryIntakeClosed, dapat: %v", err)
	}
	if got != nil {
		t.Fatal("penolakan tidak boleh mengembalikan deposit")
	}
	// Dan tidak menyisakan jejak apa pun: tanpa baris dokumen, tanpa jurnal.
	var rows, entries int64
	db.Raw(`SELECT COUNT(*) FROM notary_deposits WHERE tenant_id = ?`, ntTenant).Scan(&rows)
	db.Raw(`SELECT COUNT(*) FROM journal_entries WHERE tenant_id = ?`, ntTenant).Scan(&entries)
	if rows != 0 || entries != 0 {
		t.Fatalf("intake tertutup harus nihil efek: %d dokumen, %d jurnal", rows, entries)
	}
}

// TestIntegration_NotaryLegacyPayout — uang warisan tidak boleh menjadi yatim:
// jalur keluarnya WAJIB tetap terbuka setelah jalur masuk ditutup.
func TestIntegration_NotaryLegacyPayout(t *testing.T) {
	db := ntConnect(t)
	ntCleanup(t, db)
	defer ntCleanup(t, db)
	ctx := context.Background()

	if err := ledger.SeedCOA(ctx, db, ntTenant); err != nil {
		t.Fatalf("seed COA: %v", err)
	}
	var customerID uint64
	db.Exec(`INSERT INTO customers (tenant_id, code, name) VALUES (?,?,?)`, ntTenant, "NT-C1", "Sari")
	db.Raw("SELECT LAST_INSERT_ID()").Scan(&customerID)

	svc := notary.NewService(db)
	day := time.Date(2026, 7, 30, 0, 0, 0, 0, time.UTC)

	// Titipan warisan 7.5jt (dibuat lewat jalur lama — mewakili data pra-W-1).
	d, err := svc.SeedLegacyDeposit(ctx, ntTenant, notary.ReceiveDepositRequest{
		CustomerID: customerID, Amount: domain.FromInt(7_500_000),
		BankAccountCode: "1-1300", NotaryName: "Notaris Andi, S.H.", ReceivedAt: day,
	})
	if err != nil {
		t.Fatalf("seed titipan warisan: %v", err)
	}
	if d.Status != notary.DepositHeld || d.ReceiveJournalID == 0 {
		t.Fatalf("deposit = %+v, want held + jurnal terisi", d)
	}
	if bal := ntBalance(t, db, "2-2300"); bal != "7500000.0000" {
		t.Errorf("saldo 2-2300 = %s, want 7500000.0000", bal)
	}
	// Liability murni — pendapatan tidak tersentuh sama sekali.
	var revLines int64
	db.Raw(`SELECT COUNT(*) FROM journal_lines jl
		JOIN accounts a ON a.id = jl.account_id
		WHERE jl.tenant_id = ? AND a.code LIKE '4-%'`, ntTenant).Scan(&revLines)
	if revLines != 0 {
		t.Errorf("jurnal notaris menyentuh akun 4-%%: %d baris (harus 0)", revLines)
	}

	// Payout ke notaris → 2-2300 nol; bank netto nol; status paid_out.
	paid, err := svc.Payout(ctx, ntTenant, d.ID, notary.PayoutRequest{
		BankAccountCode: "1-1300", PaidOutAt: day.AddDate(0, 0, 3),
	})
	if err != nil {
		t.Fatalf("Payout: %v", err)
	}
	if paid.Status != notary.DepositPaidOut || paid.PayoutJournalID == nil {
		t.Fatalf("payout = %+v, want paid_out + jurnal terisi", paid)
	}
	if bal := ntBalance(t, db, "2-2300"); bal != "0.0000" {
		t.Errorf("saldo 2-2300 pasca-payout = %s, want 0.0000", bal)
	}
	if bal := ntBalance(t, db, "1-1300"); bal != "0.0000" {
		t.Errorf("bank netto = %s, want 0.0000 (masuk 7.5jt, keluar 7.5jt)", bal)
	}
	// Anti double-pay: payout ulang ditolak.
	if _, err := svc.Payout(ctx, ntTenant, d.ID, notary.PayoutRequest{BankAccountCode: "1-1300"}); !errors.Is(err, notary.ErrDepositNotHeld) {
		t.Errorf("payout ulang: want ErrDepositNotHeld, got %v", err)
	}
}

// TestIntegration_NotaryLegacyValidation — validasi jalur warisan tetap utuh:
// menutup intake tidak boleh diam-diam melonggarkan guard yang sudah ada.
func TestIntegration_NotaryLegacyValidation(t *testing.T) {
	db := ntConnect(t)
	ntCleanup(t, db)
	defer ntCleanup(t, db)
	ctx := context.Background()

	if err := ledger.SeedCOA(ctx, db, ntTenant); err != nil {
		t.Fatalf("seed COA: %v", err)
	}
	var customerID uint64
	db.Exec(`INSERT INTO customers (tenant_id, code, name) VALUES (?,?,?)`, ntTenant, "NT-C2", "Rina")
	db.Raw("SELECT LAST_INSERT_ID()").Scan(&customerID)

	svc := notary.NewService(db)
	if _, err := svc.SeedLegacyDeposit(ctx, ntTenant, notary.ReceiveDepositRequest{
		CustomerID: customerID, Amount: domain.MustParse("100.5"), BankAccountCode: "1-1300",
	}); !errors.Is(err, notary.ErrDepositAmountInvalid) {
		t.Errorf("amount pecahan: want ErrDepositAmountInvalid, got %v", err)
	}
	if _, err := svc.SeedLegacyDeposit(ctx, ntTenant, notary.ReceiveDepositRequest{
		CustomerID: customerID, Amount: domain.FromInt(1_000_000), BankAccountCode: "4-1000",
	}); err == nil {
		t.Error("akun non-kas harus ditolak (COA-driven)")
	}
}

//go:build integration

package sale_test

import (
	"context"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"esaproperti/internal/domain"
	"esaproperti/internal/ledger"
	"esaproperti/internal/sale"
)

// FE-2 · P2 — integration test: pembuktian ROLLBACK SQL nyata (guard #4).
//
// Menjalankan sale.GORMRepository.CommitPayment terhadap MySQL sungguhan.
// Prasyarat: DB sudah termigrasi (docker compose up + make migrate) dan
// variabel env TEST_DB_DSN di-set, mis:
//   TEST_DB_DSN="user:pass@tcp(127.0.0.1:3306)/esaproperti?parseTime=true&loc=Local"
//   go test ./internal/sale/ -tags=integration -run TestIntegration
//
// Bila TEST_DB_DSN kosong, test di-skip (go test ./... reguler tidak terpengaruh).

const itTenant uint64 = 9_900_001 // tenant terisolasi untuk test; dibersihkan.

func itConnect(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := os.Getenv("TEST_DB_DSN")
	if dsn == "" {
		t.Skip("TEST_DB_DSN tidak di-set — lewati integration test")
	}
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("koneksi DB: %v", err)
	}
	return db
}

func itCleanup(t *testing.T, db *gorm.DB) {
	t.Helper()
	for _, tbl := range []string{
		"credit_applications", "payment_allocations", "termin_payments",
		"documents", "document_sequences", "receipts",
		"journal_lines", "journal_entries",
		"payment_schedules", "sale_contracts", "accounts",
	} {
		if err := db.Exec("DELETE FROM "+tbl+" WHERE tenant_id = ?", itTenant).Error; err != nil {
			t.Fatalf("cleanup %s: %v", tbl, err)
		}
	}
	if err := slSeedDocumentTypes(db, itTenant); err != nil {
		t.Fatalf("seed jenis dokumen: %v", err)
	}
}

func itCount(t *testing.T, db *gorm.DB, table string) int64 {
	t.Helper()
	var n int64
	if err := db.Table(table).Where("tenant_id = ?", itTenant).Count(&n).Error; err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return n
}

// itSeedAccounts membuat akun bank (1-1300) + uang muka (2-2000), mengembalikan ID.
func itSeedAccounts(t *testing.T, db *gorm.DB) (bankID, umpID uint64) {
	t.Helper()
	bank := &ledger.Account{TenantID: itTenant, Code: "1-1300", Name: "Bank — BCA", Type: domain.AccountAsset, NormalBalance: domain.NormalBalanceDebit, IsActive: true, Category: "bank"}
	ump := &ledger.Account{TenantID: itTenant, Code: "2-2000", Name: "Uang Muka Penjualan", Type: domain.AccountLiability, NormalBalance: domain.NormalBalanceCredit, IsActive: true}
	if err := db.Create(bank).Error; err != nil {
		t.Fatalf("seed bank: %v", err)
	}
	if err := db.Create(ump).Error; err != nil {
		t.Fatalf("seed ump: %v", err)
	}
	return bank.ID, ump.ID
}

func itNewRepo(db *gorm.DB) *sale.GORMRepository {
	ledgerRepo := ledger.NewGORMRepository(db)
	posting := ledger.NewPostingService(ledgerRepo, ledgerRepo).WithPeriodChecker(ledgerRepo)
	// W-3.2: generator kwitansi WAJIB di jalur kas — kwitansinya adalah dokumen
	// bernomor yang membuktikan jurnal kasnya.
	return slWireReceipts(db, sale.NewGORMRepository(db, posting, nil))
}

func itJournalLines(bankID, umpID uint64, amount domain.Money) []sale.JournalLineInput {
	return []sale.JournalLineInput{
		{AccountID: bankID, Debit: amount, Description: "IT termin"},
		{AccountID: umpID, Credit: amount, Description: "IT termin"},
	}
}

// failingReceiptTx selalu gagal — untuk memaksa kegagalan SETELAH jurnal+alokasi
// dibuat, membuktikan rollback transaksi penuh.
type failingReceiptTx struct{}

func (failingReceiptTx) GenerateReceiptInTx(_ context.Context, _ *gorm.DB, _, _, _, _ uint64, _ domain.Money, _ string, _ time.Time, _ string) (string, uint64, error) {
	return "", 0, errors.New("injeksi kegagalan kwitansi")
}

// TestIntegration_CommitPayment_LaterStepFailure_RollsBack membuktikan bahwa bila
// sebuah langkah SETELAH jurnal+termin+alokasi gagal (di sini: kwitansi), SELURUH
// transaksi — jurnal, baris jurnal, termin, alokasi — ikut ter-ROLLBACK.
func TestIntegration_CommitPayment_LaterStepFailure_RollsBack(t *testing.T) {
	db := itConnect(t)
	itCleanup(t, db)
	defer itCleanup(t, db)

	bankID, umpID := itSeedAccounts(t, db)
	amount := domain.FromInt(400_000_000)

	// Cicilan valid → plan+jurnal+alokasi berhasil; lalu kwitansi dipaksa gagal.
	sch := &sale.PaymentSchedule{
		TenantID: itTenant, SaleContractID: 1, UnitID: 5, InstallmentNumber: 1,
		DueDate: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
		Amount:  domain.FromInt(1_000_000_000), Type: sale.ScheduleTypeInstallment, Status: sale.ScheduleStatusScheduled,
	}
	if err := db.Create(sch).Error; err != nil {
		t.Fatalf("seed schedule: %v", err)
	}

	repo := itNewRepo(db)
	repo.SetReceiptTxGenerator(failingReceiptTx{})

	sid := sch.ID
	_, err := repo.CommitPayment(context.Background(), itTenant, sale.PaymentCommitParams{
		UnitID:            5,
		ProjectID:         10,
		Amount:            amount,
		Date:              time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC),
		Description:       "IT rollback",
		BankAccountCode:   "1-1300",
		CreditAccountCode: "2-2000",
		Source:            sale.PaymentSourceCollection,
		JournalLines:      itJournalLines(bankID, umpID, amount),
		ScheduleID:        &sid,
		GenerateReceipt:   true,
	})
	if err == nil {
		t.Fatal("CommitPayment harus gagal (kwitansi diinjeksi gagal)")
	}

	// Bukti rollback: tidak ada baris tersisa untuk tenant ini.
	for _, tbl := range []string{"journal_entries", "journal_lines", "termin_payments", "payment_allocations"} {
		if n := itCount(t, db, tbl); n != 0 {
			t.Errorf("%s harus 0 setelah rollback, got %d", tbl, n)
		}
	}
	// paid_amount cicilan TIDAK berubah (rollback).
	var paid domain.Money
	if err := db.Table("payment_schedules").Select("paid_amount").
		Where("id = ? AND tenant_id = ?", sch.ID, itTenant).Scan(&paid).Error; err != nil {
		t.Fatalf("baca paid_amount: %v", err)
	}
	if paid.String() != "0" {
		t.Errorf("paid_amount harus tetap 0 setelah rollback, got %s", paid.String())
	}
}

// TestIntegration_CommitPayment_PlanFailure_NothingWritten: anchor menunjuk cicilan
// yang tak ada → planAllocationsLocked gagal SEBELUM jurnal dibuat → nihil tertulis.
func TestIntegration_CommitPayment_PlanFailure_NothingWritten(t *testing.T) {
	db := itConnect(t)
	itCleanup(t, db)
	defer itCleanup(t, db)

	bankID, umpID := itSeedAccounts(t, db)
	amount := domain.FromInt(400_000_000)

	bogus := uint64(999_999)
	_, err := itNewRepo(db).CommitPayment(context.Background(), itTenant, sale.PaymentCommitParams{
		UnitID:            5,
		ProjectID:         10,
		Amount:            amount,
		Date:              time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC),
		Description:       "IT plan fail",
		BankAccountCode:   "1-1300",
		CreditAccountCode: "2-2000",
		Source:            sale.PaymentSourceCollection,
		JournalLines:      itJournalLines(bankID, umpID, amount),
		ScheduleID:        &bogus,
	})
	if err == nil {
		t.Fatal("CommitPayment harus gagal (cicilan tak ada)")
	}
	for _, tbl := range []string{"journal_entries", "termin_payments", "payment_allocations"} {
		if n := itCount(t, db, tbl); n != 0 {
			t.Errorf("%s harus 0, got %d", tbl, n)
		}
	}
}

// TestIntegration_CommitPayment_Success membuktikan happy path atomik: satu jurnal,
// satu termin, satu baris alokasi 'schedule', dan cache paid_amount ter-update.
func TestIntegration_CommitPayment_Success(t *testing.T) {
	db := itConnect(t)
	itCleanup(t, db)
	defer itCleanup(t, db)

	bankID, umpID := itSeedAccounts(t, db)
	amount := domain.FromInt(400_000_000)

	// Seed satu cicilan valid (1jt) milik tenant.
	sch := &sale.PaymentSchedule{
		TenantID: itTenant, SaleContractID: 1, UnitID: 5, InstallmentNumber: 1,
		DueDate: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
		Amount:  domain.FromInt(1_000_000_000), Type: sale.ScheduleTypeInstallment, Status: sale.ScheduleStatusScheduled,
	}
	if err := db.Create(sch).Error; err != nil {
		t.Fatalf("seed schedule: %v", err)
	}

	sid := sch.ID
	res, err := itNewRepo(db).CommitPayment(context.Background(), itTenant, sale.PaymentCommitParams{
		UnitID:            5,
		ProjectID:         10,
		Amount:            amount,
		Date:              time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC),
		Description:       "IT sukses",
		BankAccountCode:   "1-1300",
		CreditAccountCode: "2-2000",
		Source:            sale.PaymentSourceCollection,
		JournalLines:      itJournalLines(bankID, umpID, amount),
		ScheduleID:        &sid,
	})
	if err != nil {
		t.Fatalf("CommitPayment sukses: %v", err)
	}
	if len(res.AppliedSchedules) != 1 || res.AppliedSchedules[0].Apply.String() != "400000000" {
		t.Errorf("AppliedSchedules: %+v", res.AppliedSchedules)
	}
	if res.TerminID == 0 || res.JournalID == 0 {
		t.Fatalf("hasil commit kosong: %+v", res)
	}

	if n := itCount(t, db, "journal_entries"); n != 1 {
		t.Errorf("journal_entries: got %d, want 1", n)
	}
	if n := itCount(t, db, "termin_payments"); n != 1 {
		t.Errorf("termin_payments: got %d, want 1", n)
	}
	if n := itCount(t, db, "payment_allocations"); n != 1 {
		t.Errorf("payment_allocations: got %d, want 1", n)
	}

	// Cache paid_amount cicilan ter-update = 400jt.
	var paid domain.Money
	if err := db.Table("payment_schedules").Select("paid_amount").
		Where("id = ? AND tenant_id = ?", sch.ID, itTenant).Scan(&paid).Error; err != nil {
		t.Fatalf("baca paid_amount: %v", err)
	}
	if paid.String() != "400000000" {
		t.Errorf("paid_amount: got %s, want 400000000", paid.String())
	}
}

// TestIntegration_CommitPayment_Concurrent_NoLostUpdate membuktikan row-locking:
// N pembayaran KONKUREN atas kontrak yang sama tidak saling menimpa paid_amount.
// Tanpa SELECT FOR UPDATE, lost update akan membuat paid_amount < Σ pembayaran.
func TestIntegration_CommitPayment_Concurrent_NoLostUpdate(t *testing.T) {
	db := itConnect(t)
	itCleanup(t, db)
	defer itCleanup(t, db)

	bankID, umpID := itSeedAccounts(t, db)

	// Satu cicilan 1M milik kontrak 1 (cukup besar untuk menampung semua).
	sch := &sale.PaymentSchedule{
		TenantID: itTenant, SaleContractID: 1, UnitID: 5, InstallmentNumber: 1,
		DueDate: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
		Amount:  domain.FromInt(1_000_000_000), Type: sale.ScheduleTypeInstallment, Status: sale.ScheduleStatusScheduled,
	}
	if err := db.Create(sch).Error; err != nil {
		t.Fatalf("seed schedule: %v", err)
	}

	const n = 4
	amount := domain.FromInt(200_000_000) // 4 × 200jt = 800jt < 1M
	repo := itNewRepo(db)
	cid := uint64(1)

	var wg sync.WaitGroup
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			_, errs[idx] = repo.CommitPayment(context.Background(), itTenant, sale.PaymentCommitParams{
				UnitID: 5, ProjectID: 10, Amount: amount,
				Date:        time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC),
				Description: "IT concurrent", BankAccountCode: "1-1300", CreditAccountCode: "2-2000",
				Source:       sale.PaymentSourceCollection,
				JournalLines: itJournalLines(bankID, umpID, amount),
				ContractID:   &cid,
			})
		}(i)
	}
	wg.Wait()

	for i, e := range errs {
		if e != nil {
			t.Fatalf("pembayaran konkuren #%d gagal: %v", i, e)
		}
	}

	// Konservasi: paid_amount == Σ pembayaran; jumlah baris konsisten.
	var paid domain.Money
	if err := db.Table("payment_schedules").Select("paid_amount").
		Where("id = ? AND tenant_id = ?", sch.ID, itTenant).Scan(&paid).Error; err != nil {
		t.Fatalf("baca paid_amount: %v", err)
	}
	if paid.String() != "800000000" {
		t.Errorf("paid_amount: got %s, want 800000000 (tanpa lost update)", paid.String())
	}
	if c := itCount(t, db, "payment_allocations"); c != n {
		t.Errorf("payment_allocations: got %d, want %d", c, n)
	}
	if c := itCount(t, db, "termin_payments"); c != n {
		t.Errorf("termin_payments: got %d, want %d", c, n)
	}
	// Σ alokasi == Σ pembayaran (tidak ada baris hilang/ganda).
	var sumAlloc domain.Money
	if err := db.Table("payment_allocations").Select("COALESCE(SUM(amount),0)").
		Where("tenant_id = ?", itTenant).Scan(&sumAlloc).Error; err != nil {
		t.Fatalf("sum alokasi: %v", err)
	}
	if sumAlloc.String() != "800000000" {
		t.Errorf("Σ alokasi: got %s, want 800000000", sumAlloc.String())
	}
}

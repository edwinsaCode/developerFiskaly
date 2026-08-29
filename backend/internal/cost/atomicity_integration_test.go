//go:build integration

package cost_test

// W-3.0 B-3 — atomisitas biaya terhadap MySQL nyata.
//
// CreateCostEntry menulis dua kali: jurnal draft lalu baris cost_entries.
// Sebelum W-3.0 keduanya tanpa transaksi — gagal di tulisan kedua meninggalkan
// jurnal yatim yang tak bisa ditelusuri ke biaya mana pun. PostCostEntry adalah
// jalur kas keluar (BKK di W-3.2), jadi nomor dokumen hanya boleh ditumpuk di
// atas transaksi yang sudah atomik.
//
// Prasyarat: TEST_DB_DSN di-set + DB termigrasi.

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
	"esaproperti/internal/cost"
	"esaproperti/internal/domain"
	"esaproperti/internal/ledger"
)

const caTenant uint64 = 9_900_032

var errCaInjected = errors.New("kegagalan disuntik saat menyimpan cost entry")

func caConnect(t *testing.T) *gorm.DB {
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

func caCleanup(t *testing.T, db *gorm.DB) {
	t.Helper()
	for _, tbl := range []string{"cost_entries", "documents", "document_sequences", "journal_lines", "journal_entries", "accounts"} {
		if err := db.Exec("DELETE FROM "+tbl+" WHERE tenant_id = ?", caTenant).Error; err != nil {
			t.Fatalf("cleanup %s: %v", tbl, err)
		}
	}
	// Master jenis dokumen — produksi menanamnya saat tenant dibuat; resolver
	// fail-closed, jadi jalur kas tidak bisa menerbitkan dokumen tanpa ini.
	if err := document.SeedDefaultDocumentTypes(context.Background(), db, caTenant); err != nil {
		t.Fatalf("seed jenis dokumen: %v", err)
	}
}

// caSeed menanam akun beban marketing (5-3000) dan bank (1-1300) — cukup untuk
// biaya tier overhead tanpa project.
func caSeed(t *testing.T, db *gorm.DB) {
	t.Helper()
	accs := []*ledger.Account{
		{TenantID: caTenant, Code: domain.CostCategoryMarketing.ExpenseAccountCode(),
			Name: "Beban Marketing W30", Type: domain.AccountExpense,
			NormalBalance: domain.NormalBalanceDebit, IsActive: true},
		{TenantID: caTenant, Code: "1-1300", Name: "Bank Biaya W30",
			Type: domain.AccountAsset, NormalBalance: domain.NormalBalanceDebit,
			IsActive: true, Category: ledger.CategoryBank},
	}
	for _, a := range accs {
		if err := db.Create(a).Error; err != nil {
			t.Fatalf("seed akun %s: %v", a.Code, err)
		}
	}
}

func caCount(t *testing.T, db *gorm.DB, table string) int64 {
	t.Helper()
	var n int64
	if err := db.Table(table).Where("tenant_id = ?", caTenant).Count(&n).Error; err != nil {
		t.Fatalf("hitung %s: %v", table, err)
	}
	return n
}

// ── Seam penyuntik kegagalan ─────────────────────────────────────────────────

type caFailCreateEntry struct{ cost.CostStore }

func (caFailCreateEntry) CreateCostEntry(_ context.Context, _ *cost.CostEntry) error {
	return errCaInjected
}

// caWrapRunner memakai transaksi asli, hanya penyimpanan cost entry yang gagal.
type caWrapRunner struct{ inner cost.TxRunner }

func (w caWrapRunner) InTx(ctx context.Context, fn func(cost.JournalWriter, cost.CostStore) error) error {
	return w.inner.InTx(ctx, func(jw cost.JournalWriter, st cost.CostStore) error {
		return fn(jw, caFailCreateEntry{st})
	})
}

func caService(t *testing.T, db *gorm.DB, wrap bool) *cost.Service {
	t.Helper()
	ledgerRepo := ledger.NewGORMRepository(db)
	posting := ledger.NewPostingService(ledgerRepo, ledgerRepo).
		WithPeriodChecker(ledgerRepo).
		WithJournalTx(ledgerRepo)
	repo := cost.NewGORMRepository(db)
	svc := cost.NewService(repo, cost.NewLedgerJournalAdapter(posting), repo, repo, nil)

	var runner cost.TxRunner = cost.NewGORMTxRunner(db, posting)
	if wrap {
		runner = caWrapRunner{inner: runner}
	}
	svc.SetTxRunner(runner)
	return svc
}

func caRequest() cost.CreateCostEntryRequest {
	amount, _ := domain.NewMoney("5000000")
	return cost.CreateCostEntryRequest{
		Category:        domain.CostCategoryMarketing,
		CostTier:        domain.CostTierOverhead,
		Amount:          amount,
		PaymentMethod:   cost.PaymentMethodBank,
		BankAccountCode: "1-1300",
		Date:            time.Now(),
		Vendor:          "Vendor W30",
		Description:     "Iklan W30",
	}
}

// ── B-3 ──────────────────────────────────────────────────────────────────────

// Gagal menyimpan cost entry harus membatalkan jurnal yang sudah dibuat.
func TestW30_CreateCostEntry_GagalSimpan_TidakMeninggalkanJurnal(t *testing.T) {
	db := caConnect(t)
	caCleanup(t, db)
	t.Cleanup(func() { caCleanup(t, db) })
	caSeed(t, db)

	svc := caService(t, db, true)
	_, err := svc.CreateCostEntry(context.Background(), caTenant, caRequest())
	if !errors.Is(err, errCaInjected) {
		t.Fatalf("expected kegagalan disuntik, got %v", err)
	}
	if n := caCount(t, db, "journal_entries"); n != 0 {
		t.Fatalf("jurnal harus ikut dibatalkan; tersisa %d", n)
	}
	if n := caCount(t, db, "cost_entries"); n != 0 {
		t.Fatalf("tidak boleh ada cost entry; ada %d", n)
	}
}

// Jalur normal: jurnal draft + cost entry tertaut, lalu PostCostEntry memposting.
func TestW30_CreateDanPostCostEntry_Sukses(t *testing.T) {
	db := caConnect(t)
	caCleanup(t, db)
	t.Cleanup(func() { caCleanup(t, db) })
	caSeed(t, db)

	ctx := context.Background()
	svc := caService(t, db, false)

	entry, err := svc.CreateCostEntry(ctx, caTenant, caRequest())
	if err != nil {
		t.Fatalf("CreateCostEntry: %v", err)
	}
	if entry.JournalEntryID == 0 {
		t.Fatal("cost entry harus tertaut ke jurnal")
	}
	if n := caCount(t, db, "journal_entries"); n != 1 {
		t.Fatalf("harus 1 jurnal, ada %d", n)
	}

	if err := svc.PostCostEntry(ctx, caTenant, entry.ID); err != nil {
		t.Fatalf("PostCostEntry: %v", err)
	}
	var je ledger.JournalEntry
	if err := db.First(&je, "id = ? AND tenant_id = ?", entry.JournalEntryID, caTenant).Error; err != nil {
		t.Fatalf("baca jurnal: %v", err)
	}
	if je.PostedAt == nil {
		t.Fatal("jurnal biaya harus terposting setelah PostCostEntry")
	}
}

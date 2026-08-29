//go:build integration

package ledger_test

// W-3.0 — bukti atomisitas jalur kas terhadap MySQL nyata.
//
// Perintah owner sebelum W-3.2: "Jangan menambahkan numbering/document
// requirement di atas transaksi yang masih bisa menghasilkan partial posting."
// Berkas ini membuktikan dua blocker ledger sudah tertutup:
//
//	B-5  PostingService.Reverse — Create + MarkPosted dulu di luar transaksi.
//	B-6  jalur langsung-post (CreateAndPost) yang dipakai jurnal berulang.
//
// Metode: JournalTx palsu yang MEMBUNGKUS transaksi GORM asli lalu menyuntik
// kegagalan pada tulisan terakhir (MarkPosted). Transaksi tetap transaksi
// sungguhan — yang diuji adalah apakah tulisan pertama ikut dibatalkan.
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
	"esaproperti/internal/domain"
	"esaproperti/internal/ledger"
)

const w30Tenant uint64 = 9_900_030

var errW30Injected = errors.New("kegagalan disuntik setelah jurnal dibuat")

func w30Connect(t *testing.T) *gorm.DB {
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

func w30Cleanup(t *testing.T, db *gorm.DB) {
	t.Helper()
	for _, tbl := range []string{"documents", "document_sequences", "journal_lines", "journal_entries", "accounts"} {
		if err := db.Exec("DELETE FROM "+tbl+" WHERE tenant_id = ?", w30Tenant).Error; err != nil {
			t.Fatalf("cleanup %s: %v", tbl, err)
		}
	}
	// Master jenis dokumen — produksi menanamnya saat tenant dibuat; resolver
	// fail-closed, jadi jalur kas tidak bisa menerbitkan dokumen tanpa ini.
	if err := document.SeedDefaultDocumentTypes(context.Background(), db, w30Tenant); err != nil {
		t.Fatalf("seed jenis dokumen: %v", err)
	}
}

// w30SeedAccounts menanam satu akun bank dan satu akun beban.
func w30SeedAccounts(t *testing.T, db *gorm.DB) (bankID, expenseID uint64) {
	t.Helper()
	bank := &ledger.Account{
		TenantID: w30Tenant, Code: "1-1300", Name: "Bank W30",
		Type: domain.AccountAsset, NormalBalance: domain.NormalBalanceDebit,
		IsActive: true, Category: ledger.CategoryBank,
	}
	expense := &ledger.Account{
		TenantID: w30Tenant, Code: "5-3000", Name: "Beban W30",
		Type: domain.AccountExpense, NormalBalance: domain.NormalBalanceDebit,
		IsActive: true,
	}
	if err := db.Create(bank).Error; err != nil {
		t.Fatalf("seed akun bank: %v", err)
	}
	if err := db.Create(expense).Error; err != nil {
		t.Fatalf("seed akun beban: %v", err)
	}
	return bank.ID, expense.ID
}

func w30CountEntries(t *testing.T, db *gorm.DB) int64 {
	t.Helper()
	var n int64
	if err := db.Model(&ledger.JournalEntry{}).
		Where("tenant_id = ?", w30Tenant).Count(&n).Error; err != nil {
		t.Fatalf("hitung jurnal: %v", err)
	}
	return n
}

// ── Seam penyuntik kegagalan ─────────────────────────────────────────────────

// w30FailMarkPosted meneruskan semua operasi ke store asli kecuali MarkPosted,
// yang selalu gagal — mensimulasikan kegagalan pada tulisan kedua.
type w30FailMarkPosted struct{ ledger.JournalStore }

func (w30FailMarkPosted) MarkPosted(_ context.Context, _, _ uint64, _ time.Time) error {
	return errW30Injected
}

// w30WrapTx membungkus JournalTx asli (transaksi GORM sungguhan) dan menyerahkan
// store yang MarkPosted-nya gagal.
type w30WrapTx struct{ inner ledger.JournalTx }

func (w w30WrapTx) InTx(ctx context.Context, fn func(ledger.JournalStore) error) error {
	return w.inner.InTx(ctx, func(js ledger.JournalStore) error {
		return fn(w30FailMarkPosted{js})
	})
}

func w30Lines(bankID, expenseID uint64) []ledger.LineInput {
	amount, _ := domain.NewMoney("1000000")
	return []ledger.LineInput{
		{AccountID: expenseID, Debit: amount, Credit: domain.Zero, Description: "beban W30"},
		{AccountID: bankID, Debit: domain.Zero, Credit: amount, Description: "bayar dari bank W30"},
	}
}

// ── B-6 — CreateAndPost ──────────────────────────────────────────────────────

// Gagal saat posting harus membatalkan jurnal yang sudah dibuat: tidak boleh ada
// draft yatim yang tertinggal.
func TestW30_CreateAndPost_GagalPosting_TidakMeninggalkanJurnal(t *testing.T) {
	db := w30Connect(t)
	w30Cleanup(t, db)
	t.Cleanup(func() { w30Cleanup(t, db) })
	bankID, expenseID := w30SeedAccounts(t, db)

	repo := ledger.NewGORMRepository(db)
	posting := ledger.NewPostingService(repo, repo).WithJournalTx(w30WrapTx{repo})

	_, err := posting.CreateAndPost(context.Background(), ledger.CreateJournalRequest{
		TenantID: w30Tenant, Date: time.Now(), Description: "W30 create-and-post",
		Source: "recurring", Lines: w30Lines(bankID, expenseID),
	})
	if !errors.Is(err, errW30Injected) {
		t.Fatalf("expected kegagalan disuntik, got %v", err)
	}
	if n := w30CountEntries(t, db); n != 0 {
		t.Fatalf("jurnal harus ikut dibatalkan; tersisa %d baris", n)
	}
}

// Jalur normal tetap menghasilkan satu jurnal POSTED.
func TestW30_CreateAndPost_Sukses_MenghasilkanJurnalPosted(t *testing.T) {
	db := w30Connect(t)
	w30Cleanup(t, db)
	t.Cleanup(func() { w30Cleanup(t, db) })
	bankID, expenseID := w30SeedAccounts(t, db)

	repo := ledger.NewGORMRepository(db)
	posting := ledger.NewPostingService(repo, repo).WithJournalTx(repo)

	entry, err := posting.CreateAndPost(context.Background(), ledger.CreateJournalRequest{
		TenantID: w30Tenant, Date: time.Now(), Description: "W30 create-and-post ok",
		Source: "recurring", Lines: w30Lines(bankID, expenseID),
		// Jurnalnya mengkredit bank; sejak W-3.5 kas tidak bisa bergerak tanpa
		// bukti bernomor, jadi BKK ikut diminta seperti jalur produksinya.
		Document: ledger.DocumentSpec{TypeCode: ledger.DocCashOut},
	})
	if err != nil {
		t.Fatalf("CreateAndPost: %v", err)
	}
	if entry.PostedAt == nil {
		t.Fatal("jurnal harus terposting")
	}
	if n := w30CountEntries(t, db); n != 1 {
		t.Fatalf("harus tepat 1 jurnal, ada %d", n)
	}
}

// ── B-5 — Reverse ────────────────────────────────────────────────────────────

// Reverse adalah satu-satunya cara sah mengoreksi ledger (Invariant #5).
// Gagal saat memposting pembalik dulu meninggalkan pembalik draft: HasReversingEntry
// melihatnya sehingga Reverse berikutnya ditolak, sementara tidak ada yang benar-benar
// terbalik. Setelah W-3.0, pembalik draft itu ikut dibatalkan dan Reverse bisa diulang.
func TestW30_Reverse_GagalPosting_TidakMengunciKoreksi(t *testing.T) {
	db := w30Connect(t)
	w30Cleanup(t, db)
	t.Cleanup(func() { w30Cleanup(t, db) })
	bankID, expenseID := w30SeedAccounts(t, db)

	repo := ledger.NewGORMRepository(db)
	ctx := context.Background()

	// Jurnal asli terposting lewat jalur normal.
	healthy := ledger.NewPostingService(repo, repo).WithJournalTx(repo)
	original, err := healthy.CreateAndPost(ctx, ledger.CreateJournalRequest{
		TenantID: w30Tenant, Date: time.Now(), Description: "W30 asli",
		Source: "manual", Lines: w30Lines(bankID, expenseID),
		Document: ledger.DocumentSpec{TypeCode: ledger.DocCashOut},
	})
	if err != nil {
		t.Fatalf("posting jurnal asli: %v", err)
	}

	// Pembalik gagal saat MarkPosted.
	broken := ledger.NewPostingService(repo, repo).WithJournalTx(w30WrapTx{repo})
	if _, err := broken.Reverse(ctx, w30Tenant, original.ID, time.Now()); !errors.Is(err, errW30Injected) {
		t.Fatalf("expected kegagalan disuntik, got %v", err)
	}
	if n := w30CountEntries(t, db); n != 1 {
		t.Fatalf("pembalik draft harus dibatalkan; ada %d jurnal (harusnya hanya yang asli)", n)
	}

	// Koreksi masih mungkin — tidak terkunci oleh pembalik yatim.
	rev, err := healthy.Reverse(ctx, w30Tenant, original.ID, time.Now())
	if err != nil {
		t.Fatalf("Reverse ulang setelah kegagalan: %v", err)
	}
	if rev.PostedAt == nil {
		t.Fatal("pembalik harus terposting")
	}
	if n := w30CountEntries(t, db); n != 2 {
		t.Fatalf("harus 2 jurnal (asli + pembalik), ada %d", n)
	}
}

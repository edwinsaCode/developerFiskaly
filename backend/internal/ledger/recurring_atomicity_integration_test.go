//go:build integration

package ledger_test

// W-3.0 B-6 — atomisitas jurnal berulang terhadap MySQL nyata.
//
// RunDue memposting jurnal LALU menyematkan last_run_ym sebagai guard idempoten.
// Sebelum W-3.0 keduanya di luar transaksi: gagal di antaranya membuat eksekusi
// berikutnya memposting ulang jurnal yang sama, dan dua runner bersamaan bisa
// lolos guard dua kali. Template berulang boleh mengkredit kas, jadi lubang ini
// menghasilkan pergerakan kas ganda tanpa jejak dokumen.
//
// Prasyarat: TEST_DB_DSN di-set + DB termigrasi.

import (
	"context"
	"sync"
	"testing"
	"time"

	"gorm.io/gorm"

	"esaproperti/internal/document"
	"esaproperti/internal/domain"
	"esaproperti/internal/ledger"
)

const w31Tenant uint64 = 9_900_033

func w31Cleanup(t *testing.T, db *gorm.DB) {
	t.Helper()
	for _, tbl := range []string{"recurring_journals", "documents", "document_sequences", "journal_lines", "journal_entries", "accounts"} {
		if err := db.Exec("DELETE FROM "+tbl+" WHERE tenant_id = ?", w31Tenant).Error; err != nil {
			t.Fatalf("cleanup %s: %v", tbl, err)
		}
	}
	// Master jenis dokumen — produksi menanamnya saat tenant dibuat; resolver
	// fail-closed, jadi jalur kas tidak bisa menerbitkan dokumen tanpa ini.
	if err := document.SeedDefaultDocumentTypes(context.Background(), db, w31Tenant); err != nil {
		t.Fatalf("seed jenis dokumen: %v", err)
	}
}

func w31Service(db *gorm.DB) *ledger.RecurringService {
	repo := ledger.NewGORMRepository(db)
	posting := ledger.NewPostingService(repo, repo).WithJournalTx(repo)
	return ledger.NewRecurringService(db, posting)
}

func w31SeedAccounts(t *testing.T, db *gorm.DB) {
	t.Helper()
	accs := []*ledger.Account{
		{TenantID: w31Tenant, Code: "5-3000", Name: "Beban W31",
			Type: domain.AccountExpense, NormalBalance: domain.NormalBalanceDebit, IsActive: true},
		{TenantID: w31Tenant, Code: "1-1300", Name: "Bank W31",
			Type: domain.AccountAsset, NormalBalance: domain.NormalBalanceDebit,
			IsActive: true, Category: ledger.CategoryBank},
	}
	for _, a := range accs {
		if err := db.Create(a).Error; err != nil {
			t.Fatalf("seed akun %s: %v", a.Code, err)
		}
	}
}

func w31Template(t *testing.T, svc *ledger.RecurringService, day int) *ledger.RecurringJournal {
	t.Helper()
	tmpl, err := svc.Create(context.Background(), w31Tenant, &ledger.RecurringJournal{
		Name: "Sewa kantor W31", DayOfMonth: day, IsActive: true,
		Lines: []ledger.RecurringLine{
			{AccountCode: "5-3000", Debit: "2000000", Description: "sewa"},
			{AccountCode: "1-1300", Credit: "2000000", Description: "bayar dari bank"},
		},
	})
	if err != nil {
		t.Fatalf("buat template: %v", err)
	}
	return tmpl
}

func w31CountEntries(t *testing.T, db *gorm.DB) int64 {
	t.Helper()
	var n int64
	if err := db.Model(&ledger.JournalEntry{}).
		Where("tenant_id = ?", w31Tenant).Count(&n).Error; err != nil {
		t.Fatalf("hitung jurnal: %v", err)
	}
	return n
}

// Dua eksekusi berurutan pada bulan yang sama hanya menghasilkan satu jurnal.
func TestW30_RunDue_DuaKaliBulanSama_HanyaSatuJurnal(t *testing.T) {
	db := w30Connect(t)
	w31Cleanup(t, db)
	t.Cleanup(func() { w31Cleanup(t, db) })
	w31SeedAccounts(t, db)

	svc := w31Service(db)
	now := time.Now()
	w31Template(t, svc, 1)

	ctx := context.Background()
	if n, err := svc.RunDue(ctx, w31Tenant, now, nil); err != nil || n != 1 {
		t.Fatalf("eksekusi pertama: n=%d err=%v", n, err)
	}
	if n, err := svc.RunDue(ctx, w31Tenant, now, nil); err != nil || n != 0 {
		t.Fatalf("eksekusi kedua harus no-op: n=%d err=%v", n, err)
	}
	if n := w31CountEntries(t, db); n != 1 {
		t.Fatalf("harus tepat 1 jurnal, ada %d", n)
	}
}

// Dua runner bersamaan: guard idempoten dievaluasi DI DALAM transaksi, jadi yang
// kalah dibatalkan — kas tidak bergerak dua kali.
func TestW30_RunDue_Bersamaan_TidakGandaPosting(t *testing.T) {
	db := w30Connect(t)
	w31Cleanup(t, db)
	t.Cleanup(func() { w31Cleanup(t, db) })
	w31SeedAccounts(t, db)

	svc := w31Service(db)
	now := time.Now()
	w31Template(t, svc, 1)

	ctx := context.Background()
	var wg sync.WaitGroup
	counts := make([]int, 2)
	errs := make([]error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			counts[i], errs[i] = svc.RunDue(ctx, w31Tenant, now, nil)
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("runner %d error: %v", i, err)
		}
	}
	if total := counts[0] + counts[1]; total != 1 {
		t.Fatalf("hanya satu runner boleh berhasil, total=%d", total)
	}
	if n := w31CountEntries(t, db); n != 1 {
		t.Fatalf("harus tepat 1 jurnal, ada %d", n)
	}
}

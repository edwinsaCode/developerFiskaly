package ledger_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"esaproperti/internal/domain"
	"esaproperti/internal/ledger"
)

// ── mockPeriodChecker ─────────────────────────────────────────────────────────

type mockPeriodChecker struct {
	closed bool
	err    error
}

func (m *mockPeriodChecker) IsPeriodClosed(_ context.Context, _ uint64, _ time.Time) (bool, error) {
	return m.closed, m.err
}

// ── Period guard tests ────────────────────────────────────────────────────────

// TestPeriodGuard_ClosedPeriod_RejectsWithErrPeriodClosed (Sprint 1 QA #1):
// Jurnal dengan tanggal pada periode yang ditutup harus ditolak dengan ErrPeriodClosed.
func TestPeriodGuard_ClosedPeriod_RejectsWithErrPeriodClosed(t *testing.T) {
	store := newMockJournalStore()
	svc := ledger.NewPostingService(twoAccounts(), store).
		WithPeriodChecker(&mockPeriodChecker{closed: true})

	req := ledger.CreateJournalRequest{
		TenantID:    1,
		Date:        time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC),
		Description: "jurnal pada periode tertutup",
		Lines: []ledger.LineInput{
			{AccountID: 1, Debit: domain.FromInt(1_000_000)},
			{AccountID: 2, Credit: domain.FromInt(1_000_000)},
		},
	}
	_, err := svc.Create(context.Background(), req)
	if !errors.Is(err, ledger.ErrPeriodClosed) {
		t.Errorf("expected ErrPeriodClosed, got %v", err)
	}
}

// TestPeriodGuard_ClosedPeriod_JournalNotPersisted (Sprint 1 QA #1):
// Ketika periode ditutup, jurnal tidak boleh tersimpan ke store.
func TestPeriodGuard_ClosedPeriod_JournalNotPersisted(t *testing.T) {
	store := newMockJournalStore()
	svc := ledger.NewPostingService(twoAccounts(), store).
		WithPeriodChecker(&mockPeriodChecker{closed: true})

	req := ledger.CreateJournalRequest{
		TenantID:    1,
		Date:        time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC),
		Description: "jurnal yang seharusnya tidak tersimpan",
		Lines: []ledger.LineInput{
			{AccountID: 1, Debit: domain.FromInt(500_000)},
			{AccountID: 2, Credit: domain.FromInt(500_000)},
		},
	}
	_, _ = svc.Create(context.Background(), req)

	store.mu.Lock()
	count := len(store.entries)
	store.mu.Unlock()

	if count != 0 {
		t.Errorf("journal seharusnya tidak tersimpan saat periode ditutup, got %d entries", count)
	}
}

// TestPeriodGuard_OpenPeriod_AllowsJournal:
// Jurnal dengan tanggal pada periode yang terbuka harus diterima.
func TestPeriodGuard_OpenPeriod_AllowsJournal(t *testing.T) {
	store := newMockJournalStore()
	svc := ledger.NewPostingService(twoAccounts(), store).
		WithPeriodChecker(&mockPeriodChecker{closed: false})

	req := ledger.CreateJournalRequest{
		TenantID:    1,
		Date:        time.Now(),
		Description: "jurnal pada periode terbuka",
		Lines: []ledger.LineInput{
			{AccountID: 1, Debit: domain.FromInt(1_000_000)},
			{AccountID: 2, Credit: domain.FromInt(1_000_000)},
		},
	}
	entry, err := svc.Create(context.Background(), req)
	if err != nil {
		t.Errorf("periode terbuka seharusnya menerima jurnal, got %v", err)
	}
	if entry == nil || entry.ID == 0 {
		t.Error("jurnal seharusnya tersimpan dan mendapat ID")
	}
}

// TestPeriodGuard_NilChecker_AllowsJournal:
// PostingService tanpa PeriodChecker (nil) harus tetap menerima semua jurnal.
// Ini menjaga backward compat: test lama dan seeds tidak perlu mengimplementasi PeriodChecker.
func TestPeriodGuard_NilChecker_AllowsJournal(t *testing.T) {
	// Tidak memanggil WithPeriodChecker — periods == nil
	svc, _ := newTestService()

	req := ledger.CreateJournalRequest{
		TenantID:    1,
		Date:        time.Now(),
		Description: "jurnal tanpa period checker",
		Lines: []ledger.LineInput{
			{AccountID: 1, Debit: domain.FromInt(2_000_000)},
			{AccountID: 2, Credit: domain.FromInt(2_000_000)},
		},
	}
	_, err := svc.Create(context.Background(), req)
	if err != nil {
		t.Errorf("nil PeriodChecker seharusnya menerima semua jurnal, got %v", err)
	}
}

// TestPeriodGuard_CheckerError_PropagatesError:
// Jika PeriodChecker mengembalikan error (misal DB mati), jurnal ditolak dengan error tersebut.
func TestPeriodGuard_CheckerError_PropagatesError(t *testing.T) {
	checkerErr := errors.New("DB koneksi gagal")
	store := newMockJournalStore()
	svc := ledger.NewPostingService(twoAccounts(), store).
		WithPeriodChecker(&mockPeriodChecker{err: checkerErr})

	req := ledger.CreateJournalRequest{
		TenantID:    1,
		Date:        time.Now(),
		Description: "jurnal saat checker error",
		Lines: []ledger.LineInput{
			{AccountID: 1, Debit: domain.FromInt(1_000_000)},
			{AccountID: 2, Credit: domain.FromInt(1_000_000)},
		},
	}
	_, err := svc.Create(context.Background(), req)
	if !errors.Is(err, checkerErr) {
		t.Errorf("expected checker error to propagate, got %v", err)
	}
}

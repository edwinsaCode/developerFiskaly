package ledger

import (
	"context"
	"testing"
	"time"

	"esaproperti/internal/domain"
)

// ── helpers ───────────────────────────────────────────────────────────────────

func acc(id uint64, code, name string, t domain.AccountType) *Account {
	return &Account{ID: id, Code: code, Name: name, Type: t}
}

func tbRow(id uint64, code, name string, bal domain.Money) TrialBalanceRow {
	return TrialBalanceRow{AccountID: id, AccountCode: code, AccountName: name, Balance: bal}
}

// closingAccounts mengembalikan map kode→akun dengan 3-3000 & 3-2000 tersedia.
func closingAccounts() map[string]*Account {
	return map[string]*Account{
		"3-3000": acc(33, "3-3000", "Ikhtisar Laba Rugi", domain.AccountEquity),
		"3-2000": acc(32, "3-2000", "Laba Ditahan", domain.AccountEquity),
		"4-1000": acc(41, "4-1000", "Pendapatan Penjualan", domain.AccountRevenue),
		"5-1000": acc(51, "5-1000", "HPP", domain.AccountExpense),
		"5-3000": acc(53, "5-3000", "Beban Pemasaran", domain.AccountExpense),
	}
}

func sumLines(lines []LineInput) (debit, credit domain.Money) {
	for _, l := range lines {
		debit = debit.Add(l.Debit)
		credit = credit.Add(l.Credit)
	}
	return
}

// ── planClosing: laba ───────────────────────────────────────────────────────

func TestPlanClosing_Profit(t *testing.T) {
	rows := []TrialBalanceRow{
		tbRow(41, "4-1000", "Pendapatan Penjualan", domain.FromInt(8_400_000_000)),
		tbRow(51, "5-1000", "HPP", domain.FromInt(3_000_000_000)),
	}
	p, err := planClosing(rows, closingAccounts())
	if err != nil {
		t.Fatalf("planClosing: %v", err)
	}

	if p.netIncome.String() != "5400000000" {
		t.Errorf("netIncome: got %s, want 5400000000", p.netIncome.String())
	}

	// Entry A harus balanced.
	dA, cA := sumLines(p.entryA)
	if !dA.Equal(cA) {
		t.Errorf("Entry A tidak balanced: D=%s C=%s", dA, cA)
	}
	// Income summary (33) dikredit laba bersih pada Entry A.
	if !hasLine(p.entryA, 33, domain.Zero, domain.FromInt(5_400_000_000)) {
		t.Errorf("Entry A harus Cr 3-3000 5.4M, lines=%+v", p.entryA)
	}

	// Entry B: Dr 3-3000 / Cr 3-2000 sebesar laba.
	dB, cB := sumLines(p.entryB)
	if !dB.Equal(cB) {
		t.Errorf("Entry B tidak balanced: D=%s C=%s", dB, cB)
	}
	if !hasLine(p.entryB, 33, domain.FromInt(5_400_000_000), domain.Zero) {
		t.Errorf("Entry B harus Dr 3-3000 5.4M, lines=%+v", p.entryB)
	}
	if !hasLine(p.entryB, 32, domain.Zero, domain.FromInt(5_400_000_000)) {
		t.Errorf("Entry B harus Cr 3-2000 (Laba Ditahan) 5.4M, lines=%+v", p.entryB)
	}
}

// ── planClosing: rugi ───────────────────────────────────────────────────────

func TestPlanClosing_Loss(t *testing.T) {
	rows := []TrialBalanceRow{
		tbRow(41, "4-1000", "Pendapatan Penjualan", domain.FromInt(2_000_000_000)),
		tbRow(51, "5-1000", "HPP", domain.FromInt(3_000_000_000)),
	}
	p, err := planClosing(rows, closingAccounts())
	if err != nil {
		t.Fatalf("planClosing: %v", err)
	}
	if p.netIncome.String() != "-1000000000" {
		t.Errorf("netIncome: got %s, want -1000000000", p.netIncome.String())
	}
	// Entry A: income summary di-DEBIT sebesar rugi.
	if !hasLine(p.entryA, 33, domain.FromInt(1_000_000_000), domain.Zero) {
		t.Errorf("Entry A harus Dr 3-3000 1M (rugi), lines=%+v", p.entryA)
	}
	dA, cA := sumLines(p.entryA)
	if !dA.Equal(cA) {
		t.Errorf("Entry A tidak balanced: D=%s C=%s", dA, cA)
	}
	// Entry B: rugi membebani Laba Ditahan → Dr 3-2000 / Cr 3-3000.
	if !hasLine(p.entryB, 32, domain.FromInt(1_000_000_000), domain.Zero) {
		t.Errorf("Entry B harus Dr 3-2000 1M (rugi), lines=%+v", p.entryB)
	}
	dB, cB := sumLines(p.entryB)
	if !dB.Equal(cB) {
		t.Errorf("Entry B tidak balanced: D=%s C=%s", dB, cB)
	}
}

// ── planClosing: impas (net 0) ───────────────────────────────────────────────

func TestPlanClosing_BreakEven_NoEntryB(t *testing.T) {
	rows := []TrialBalanceRow{
		tbRow(41, "4-1000", "Pendapatan Penjualan", domain.FromInt(2_000_000_000)),
		tbRow(51, "5-1000", "HPP", domain.FromInt(2_000_000_000)),
	}
	p, err := planClosing(rows, closingAccounts())
	if err != nil {
		t.Fatalf("planClosing: %v", err)
	}
	if !p.netIncome.IsZero() {
		t.Errorf("netIncome harus 0, got %s", p.netIncome.String())
	}
	// Entry A balanced tanpa baris 3-3000; Entry B kosong.
	dA, cA := sumLines(p.entryA)
	if !dA.Equal(cA) {
		t.Errorf("Entry A tidak balanced: D=%s C=%s", dA, cA)
	}
	if len(p.entryB) != 0 {
		t.Errorf("Entry B harus kosong saat impas, got %d baris", len(p.entryB))
	}
}

// ── planClosing: tidak ada yang ditutup ──────────────────────────────────────

func TestPlanClosing_NothingToClose(t *testing.T) {
	rows := []TrialBalanceRow{
		tbRow(13, "1-3100", "Persediaan", domain.FromInt(1_000_000_000)),
	}
	if _, err := planClosing(rows, closingAccounts()); err != ErrNothingToClose {
		t.Errorf("expected ErrNothingToClose, got %v", err)
	}
}

// ── planClosing: akun penutup hilang ─────────────────────────────────────────

func TestPlanClosing_MissingClosingAccounts(t *testing.T) {
	rows := []TrialBalanceRow{tbRow(41, "4-1000", "Pendapatan", domain.FromInt(1_000_000_000))}
	only4 := map[string]*Account{"4-1000": acc(41, "4-1000", "Pendapatan", domain.AccountRevenue)}
	if _, err := planClosing(rows, only4); err != ErrClosingAccountsMissing {
		t.Errorf("expected ErrClosingAccountsMissing, got %v", err)
	}
}

func hasLine(lines []LineInput, accountID uint64, debit, credit domain.Money) bool {
	for _, l := range lines {
		if l.AccountID == accountID && l.Debit.Equal(debit) && l.Credit.Equal(credit) {
			return true
		}
	}
	return false
}

// ── Service: CloseFiscalYear end-to-end (in-memory) ──────────────────────────

type fakeClosingReader struct {
	tb       *TrialBalance
	accounts []*Account
	existing []*JournalSummary
}

func (f *fakeClosingReader) TrialBalance(_ context.Context, _ uint64, _ time.Time) (*TrialBalance, error) {
	return f.tb, nil
}
func (f *fakeClosingReader) ListAccounts(_ context.Context, _ uint64) ([]*Account, error) {
	return f.accounts, nil
}
func (f *fakeClosingReader) ListJournalSummaries(_ context.Context, _ uint64, _ JournalFilter) ([]*JournalSummary, error) {
	return f.existing, nil
}

type fakeAccountLookup struct{}

func (fakeAccountLookup) FindByIDs(_ context.Context, _ uint64, ids []uint64) ([]*Account, error) {
	out := make([]*Account, len(ids))
	for i, id := range ids {
		out[i] = &Account{ID: id}
	}
	return out, nil
}

type fakeJournalStore struct {
	entries map[uint64]*JournalEntry
	next    uint64
}

func newFakeJournalStore() *fakeJournalStore {
	return &fakeJournalStore{entries: make(map[uint64]*JournalEntry)}
}
func (f *fakeJournalStore) Create(_ context.Context, e *JournalEntry) error {
	f.next++
	e.ID = f.next
	cp := *e
	f.entries[e.ID] = &cp
	return nil
}
func (f *fakeJournalStore) FindByID(_ context.Context, _, id uint64) (*JournalEntry, error) {
	e, ok := f.entries[id]
	if !ok {
		return nil, ErrAccountNotFound
	}
	cp := *e
	return &cp, nil
}
func (f *fakeJournalStore) MarkPosted(_ context.Context, _, id uint64, at time.Time) error {
	if e, ok := f.entries[id]; ok {
		e.PostedAt = &at
	}
	return nil
}
func (f *fakeJournalStore) HasReversingEntry(_ context.Context, _, _ uint64) (bool, error) {
	return false, nil
}
func (f *fakeJournalStore) UpdateDraft(_ context.Context, _, _ uint64, _ map[string]interface{}) error {
	return nil
}
func (f *fakeJournalStore) DeleteDraft(_ context.Context, _, _ uint64) error { return nil }

func buildClosingService(reader *fakeClosingReader) (*ClosingService, *fakeJournalStore) {
	store := newFakeJournalStore()
	posting := NewPostingService(fakeAccountLookup{}, store)
	return NewClosingService(posting, reader), store
}

func TestCloseFiscalYear_Profit_PostsTwoBalancedClosingJournals(t *testing.T) {
	reader := &fakeClosingReader{
		tb: &TrialBalance{Rows: []TrialBalanceRow{
			tbRow(41, "4-1000", "Pendapatan Penjualan", domain.FromInt(8_400_000_000)),
			tbRow(51, "5-1000", "HPP", domain.FromInt(3_000_000_000)),
		}},
		accounts: []*Account{
			acc(33, "3-3000", "Ikhtisar Laba Rugi", domain.AccountEquity),
			acc(32, "3-2000", "Laba Ditahan", domain.AccountEquity),
		},
	}
	svc, store := buildClosingService(reader)

	res, err := svc.CloseFiscalYear(context.Background(), 1, 2026, 7)
	if err != nil {
		t.Fatalf("CloseFiscalYear: %v", err)
	}
	if res.NetIncome != "5400000000" {
		t.Errorf("NetIncome: got %s, want 5400000000", res.NetIncome)
	}
	if res.IncomeSummaryJournalID == 0 || res.RetainedEarningsJournalID == 0 {
		t.Errorf("kedua jurnal penutup harus terbentuk: %+v", res)
	}

	// Dua jurnal, semua posted, source "closing", balanced.
	if len(store.entries) != 2 {
		t.Fatalf("harus 2 jurnal penutup, got %d", len(store.entries))
	}
	for id, e := range store.entries {
		if e.PostedAt == nil {
			t.Errorf("jurnal #%d harus diposting", id)
		}
		if e.Source != JournalSourceClosing {
			t.Errorf("jurnal #%d source: got %s, want closing", id, e.Source)
		}
		var d, c domain.Money
		for _, l := range e.Lines {
			d = d.Add(l.Debit)
			c = c.Add(l.Credit)
		}
		if !d.Equal(c) {
			t.Errorf("jurnal #%d tidak balanced: D=%s C=%s", id, d, c)
		}
	}
}

func TestCloseFiscalYear_AlreadyClosed(t *testing.T) {
	reader := &fakeClosingReader{
		tb:       &TrialBalance{Rows: []TrialBalanceRow{tbRow(41, "4-1000", "Pendapatan", domain.FromInt(1_000_000_000))}},
		accounts: []*Account{acc(33, "3-3000", "Ikhtisar", domain.AccountEquity), acc(32, "3-2000", "Laba Ditahan", domain.AccountEquity)},
		existing: []*JournalSummary{{ID: 99, Source: JournalSourceClosing}}, // sudah ada entri penutup
	}
	svc, _ := buildClosingService(reader)
	if _, err := svc.CloseFiscalYear(context.Background(), 1, 2026, 7); err != ErrYearAlreadyClosed {
		t.Errorf("expected ErrYearAlreadyClosed, got %v", err)
	}
}

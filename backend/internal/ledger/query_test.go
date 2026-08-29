package ledger_test

import (
	"math/rand"
	"testing"
	"time"

	"esaproperti/internal/domain"
	"esaproperti/internal/ledger"
)

// TestTrialBalance_AlwaysBalances (Invariant #1):
// Any set of posted balanced journals yields a trial balance where
// TotalDebit == TotalCredit.
func TestTrialBalance_AlwaysBalances(t *testing.T) {
	accounts := map[uint64]*ledger.Account{
		1: {ID: 1, Code: "1-1100", Name: "Kas", Type: domain.AccountAsset, NormalBalance: domain.NormalBalanceDebit},
		2: {ID: 2, Code: "1-3100", Name: "Persediaan", Type: domain.AccountAsset, NormalBalance: domain.NormalBalanceDebit},
		3: {ID: 3, Code: "2-1000", Name: "Hutang Usaha", Type: domain.AccountLiability, NormalBalance: domain.NormalBalanceCredit},
		4: {ID: 4, Code: "4-1000", Name: "Pendapatan", Type: domain.AccountRevenue, NormalBalance: domain.NormalBalanceCredit},
		5: {ID: 5, Code: "5-1000", Name: "HPP", Type: domain.AccountExpense, NormalBalance: domain.NormalBalanceDebit},
	}

	for round := range 50 {
		// Build a random set of balanced journals.
		var allLines []ledger.JournalLine

		for range rand.Intn(20) + 2 {
			amount := rand.Int63n(1_000_000_000) + 1
			// Simple 2-line balanced journal.
			debitAccID := uint64(rand.Intn(3) + 1) // 1,2,3
			creditAccID := uint64(rand.Intn(2) + 4) // 4,5
			allLines = append(allLines,
				ledger.JournalLine{AccountID: debitAccID, Debit: domain.FromInt(amount)},
				ledger.JournalLine{AccountID: creditAccID, Credit: domain.FromInt(amount)},
			)
		}

		tb := ledger.ComputeTrialBalance(allLines, accounts, time.Now())

		if !tb.TotalDebit.Equal(tb.TotalCredit) {
			t.Errorf("round %d: trial balance not equal: debit=%s credit=%s",
				round, tb.TotalDebit, tb.TotalCredit)
		}
	}
}

// TestTrialBalance_EmptyLedger: empty ledger yields zero totals.
func TestTrialBalance_EmptyLedger(t *testing.T) {
	tb := ledger.ComputeTrialBalance(nil, nil, time.Now())
	if !tb.TotalDebit.IsZero() || !tb.TotalCredit.IsZero() {
		t.Errorf("empty ledger: expected zeros, got debit=%s credit=%s", tb.TotalDebit, tb.TotalCredit)
	}
	if len(tb.Rows) != 0 {
		t.Errorf("expected 0 rows, got %d", len(tb.Rows))
	}
}

// TestTrialBalance_SingleJournal: one balanced journal is reflected correctly.
func TestTrialBalance_SingleJournal(t *testing.T) {
	accounts := map[uint64]*ledger.Account{
		1: {ID: 1, Code: "1-3100", Name: "Persediaan Hard Cost", Type: domain.AccountAsset},
		2: {ID: 2, Code: "2-1000", Name: "Hutang Usaha", Type: domain.AccountLiability},
	}
	lines := []ledger.JournalLine{
		{AccountID: 1, Debit: domain.MustParse("500000000")},
		{AccountID: 2, Credit: domain.MustParse("500000000")},
	}
	tb := ledger.ComputeTrialBalance(lines, accounts, time.Now())

	if !tb.TotalDebit.Equal(tb.TotalCredit) {
		t.Errorf("total debit %s != total credit %s", tb.TotalDebit, tb.TotalCredit)
	}
	if !tb.TotalDebit.Equal(domain.MustParse("500000000")) {
		t.Errorf("total debit: got %s, want 500000000", tb.TotalDebit)
	}
	if len(tb.Rows) != 2 {
		t.Errorf("expected 2 rows, got %d", len(tb.Rows))
	}
}

// TestTrialBalance_SortedByCode: rows should be sorted by account code.
func TestTrialBalance_SortedByCode(t *testing.T) {
	accounts := map[uint64]*ledger.Account{
		3: {ID: 3, Code: "5-1000", Name: "HPP", Type: domain.AccountExpense},
		1: {ID: 1, Code: "1-1100", Name: "Kas", Type: domain.AccountAsset},
		2: {ID: 2, Code: "2-1000", Name: "Hutang", Type: domain.AccountLiability},
	}
	lines := []ledger.JournalLine{
		{AccountID: 3, Debit: domain.MustParse("100000")},
		{AccountID: 1, Credit: domain.MustParse("50000")},
		{AccountID: 2, Credit: domain.MustParse("50000")},
	}
	tb := ledger.ComputeTrialBalance(lines, accounts, time.Now())

	codes := make([]string, len(tb.Rows))
	for i, r := range tb.Rows {
		codes[i] = r.AccountCode
	}
	for i := 1; i < len(codes); i++ {
		if codes[i] < codes[i-1] {
			t.Errorf("rows not sorted: %v", codes)
			break
		}
	}
}

// TestGeneralLedger_RunningBalance: verify the running balance is cumulative.
func TestGeneralLedger_RunningBalance(t *testing.T) {
	account := &ledger.Account{ID: 1, Code: "1-1100", Name: "Kas", Type: domain.AccountAsset}
	entries := []ledger.JournalEntry{
		{ID: 1, Date: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC), Description: "Masuk A"},
		{ID: 2, Date: time.Date(2025, 1, 5, 0, 0, 0, 0, time.UTC), Description: "Masuk B"},
		{ID: 3, Date: time.Date(2025, 1, 10, 0, 0, 0, 0, time.UTC), Description: "Keluar C"},
	}
	lines := []ledger.JournalLine{
		{ID: 1, JournalEntryID: 1, AccountID: 1, Debit: domain.MustParse("1000000")},
		{ID: 2, JournalEntryID: 2, AccountID: 1, Debit: domain.MustParse("500000")},
		{ID: 3, JournalEntryID: 3, AccountID: 1, Credit: domain.MustParse("300000")},
	}
	filter := ledger.LedgerFilter{AccountID: &account.ID}
	result := ledger.ComputeGeneralLedger(lines, account, filter, entries)

	if len(result) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(result))
	}
	expectedBalances := []string{"1000000", "1500000", "1200000"}
	for i, e := range result {
		want := domain.MustParse(expectedBalances[i])
		if !e.Balance.Equal(want) {
			t.Errorf("entry %d: balance %s, want %s", i, e.Balance, want)
		}
	}
}

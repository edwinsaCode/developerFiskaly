package ledger_test

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"sync"
	"testing"
	"time"

	"esaproperti/internal/domain"
	"esaproperti/internal/ledger"
)

// ── In-memory mocks ───────────────────────────────────────────────────────────

type mockAccountLookup struct {
	mu       sync.RWMutex
	accounts map[uint64]*ledger.Account
}

func (m *mockAccountLookup) FindByIDs(_ context.Context, _ uint64, ids []uint64) ([]*ledger.Account, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]*ledger.Account, 0, len(ids))
	for _, id := range ids {
		a, ok := m.accounts[id]
		if !ok {
			return nil, fmt.Errorf("%w: id=%d", ledger.ErrAccountNotFound, id)
		}
		result = append(result, a)
	}
	return result, nil
}

type mockJournalStore struct {
	mu      sync.Mutex
	entries map[uint64]*ledger.JournalEntry
	nextID  uint64
}

func newMockJournalStore() *mockJournalStore {
	return &mockJournalStore{entries: make(map[uint64]*ledger.JournalEntry)}
}

func (m *mockJournalStore) Create(_ context.Context, entry *ledger.JournalEntry) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.nextID++
	entry.ID = m.nextID
	now := time.Now()
	entry.CreatedAt = now
	entry.UpdatedAt = now
	for i := range entry.Lines {
		m.nextID++
		entry.Lines[i].ID = m.nextID
		entry.Lines[i].JournalEntryID = entry.ID
		entry.Lines[i].CreatedAt = now
	}
	cp := *entry
	lines := make([]ledger.JournalLine, len(entry.Lines))
	copy(lines, entry.Lines)
	cp.Lines = lines
	m.entries[cp.ID] = &cp
	return nil
}

func (m *mockJournalStore) FindByID(_ context.Context, _, id uint64) (*ledger.JournalEntry, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.entries[id]
	if !ok {
		return nil, ledger.ErrAccountNotFound // reuse as "not found"
	}
	cp := *e
	lines := make([]ledger.JournalLine, len(e.Lines))
	copy(lines, e.Lines)
	cp.Lines = lines
	return &cp, nil
}

func (m *mockJournalStore) MarkPosted(_ context.Context, _, id uint64, at time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.entries[id]
	if !ok {
		return ledger.ErrAccountNotFound
	}
	if e.PostedAt != nil {
		return ledger.ErrJournalAlreadyPosted
	}
	e.PostedAt = &at
	return nil
}

func (m *mockJournalStore) HasReversingEntry(_ context.Context, _, id uint64) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, e := range m.entries {
		if e.ReversesID != nil && *e.ReversesID == id {
			return true, nil
		}
	}
	return false, nil
}

func (m *mockJournalStore) UpdateDraft(_ context.Context, _, id uint64, _ map[string]interface{}) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.entries[id]
	if !ok {
		return ledger.ErrAccountNotFound
	}
	if e.PostedAt != nil {
		return ledger.ErrJournalAlreadyPosted
	}
	return nil
}

func (m *mockJournalStore) DeleteDraft(_ context.Context, _, id uint64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.entries[id]
	if !ok {
		return ledger.ErrAccountNotFound
	}
	if e.PostedAt != nil {
		return ledger.ErrJournalAlreadyPosted
	}
	delete(m.entries, id)
	return nil
}

// AllPostedLines returns all lines from posted entries (for trial balance tests).
func (m *mockJournalStore) AllPostedLines() []ledger.JournalLine {
	m.mu.Lock()
	defer m.mu.Unlock()
	var lines []ledger.JournalLine
	for _, e := range m.entries {
		if e.PostedAt != nil {
			lines = append(lines, e.Lines...)
		}
	}
	return lines
}

// ── Helpers ───────────────────────────────────────────────────────────────────

// twoAccounts returns a minimal account map (one asset + one liability).
func twoAccounts() *mockAccountLookup {
	return &mockAccountLookup{
		accounts: map[uint64]*ledger.Account{
			1: {ID: 1, Code: "1-1100", Name: "Kas", Type: domain.AccountAsset, NormalBalance: domain.NormalBalanceDebit},
			2: {ID: 2, Code: "2-1000", Name: "Hutang Usaha", Type: domain.AccountLiability, NormalBalance: domain.NormalBalanceCredit},
		},
	}
}

func newTestService() (*ledger.PostingService, *mockJournalStore) {
	store := newMockJournalStore()
	svc := ledger.NewPostingService(twoAccounts(), store)
	return svc, store
}

// createAndPostBalanced is a test helper that creates and posts a balanced journal.
func createAndPostBalanced(t *testing.T, svc *ledger.PostingService, amount int64) *ledger.JournalEntry {
	t.Helper()
	req := ledger.CreateJournalRequest{
		TenantID:    0,
		Date:        time.Now(),
		Description: fmt.Sprintf("test-balanced-%d", amount),
		Lines: []ledger.LineInput{
			{AccountID: 1, Debit: domain.FromInt(amount)},
			{AccountID: 2, Credit: domain.FromInt(amount)},
		},
	}
	entry, err := svc.Create(context.Background(), req)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	posted, err := svc.Post(context.Background(), req.TenantID, entry.ID)
	if err != nil {
		t.Fatalf("Post: %v", err)
	}
	return posted
}

// ── Property tests ────────────────────────────────────────────────────────────

// TestPosting_BalancedJournal_AlwaysPosts (Invariant #1):
// For any positive amount, a journal where Σdebit == Σcredit must always post.
func TestPosting_BalancedJournal_AlwaysPosts(t *testing.T) {
	svc, _ := newTestService()
	ctx := context.Background()

	for i := range 300 {
		amount := rand.Int63n(10_000_000_000) + 1 // 1 to 10B
		req := ledger.CreateJournalRequest{
			TenantID:    0,
			Date:        time.Now(),
			Description: fmt.Sprintf("balanced iter %d", i),
			Lines: []ledger.LineInput{
				{AccountID: 1, Debit: domain.FromInt(amount)},
				{AccountID: 2, Credit: domain.FromInt(amount)},
			},
		}
		entry, err := svc.Create(ctx, req)
		if err != nil {
			t.Fatalf("iter %d Create: unexpected error: %v", i, err)
		}
		posted, err := svc.Post(ctx, req.TenantID, entry.ID)
		if err != nil {
			t.Errorf("iter %d Post: unexpected error: %v", i, err)
		}
		if posted.PostedAt == nil {
			t.Errorf("iter %d: PostedAt should be non-nil after posting", i)
		}
	}
}

// TestPosting_UnbalancedJournal_AlwaysRejected (Invariant #1):
// A journal where Σdebit ≠ Σcredit must always be rejected with ErrJournalNotBalanced.
func TestPosting_UnbalancedJournal_AlwaysRejected(t *testing.T) {
	svc, _ := newTestService()
	ctx := context.Background()

	for i := range 300 {
		debit := rand.Int63n(10_000_000_000) + 1
		credit := rand.Int63n(10_000_000_000) + 1
		if debit == credit {
			credit++ // guarantee imbalance
		}
		req := ledger.CreateJournalRequest{
			TenantID:    0,
			Date:        time.Now(),
			Description: fmt.Sprintf("unbalanced iter %d", i),
			Lines: []ledger.LineInput{
				{AccountID: 1, Debit: domain.FromInt(debit)},
				{AccountID: 2, Credit: domain.FromInt(credit)},
			},
		}
		_, err := svc.Create(ctx, req)
		if !errors.Is(err, ledger.ErrJournalNotBalanced) {
			t.Errorf("iter %d: expected ErrJournalNotBalanced, got %v", i, err)
		}
	}
}

// TestPosting_MultiLine_BalancedJournal (Invariant #1):
// A journal with multiple debit and credit lines that balance overall must post.
func TestPosting_MultiLine_BalancedJournal(t *testing.T) {
	accounts := &mockAccountLookup{
		accounts: map[uint64]*ledger.Account{
			1: {ID: 1, Code: "1-3100", Name: "Persediaan Hard Cost", Type: domain.AccountAsset},
			2: {ID: 2, Code: "1-3200", Name: "Persediaan Soft Cost", Type: domain.AccountAsset},
			3: {ID: 3, Code: "2-1000", Name: "Hutang Usaha", Type: domain.AccountLiability},
		},
	}
	store := newMockJournalStore()
	svc := ledger.NewPostingService(accounts, store)

	req := ledger.CreateJournalRequest{
		TenantID:    0,
		Date:        time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC),
		Description: "Biaya konstruksi + soft cost",
		Lines: []ledger.LineInput{
			{AccountID: 1, Debit: domain.MustParse("400000000")},
			{AccountID: 2, Debit: domain.MustParse("100000000")},
			{AccountID: 3, Credit: domain.MustParse("500000000")},
		},
	}
	entry, err := svc.Create(context.Background(), req)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	posted, err := svc.Post(context.Background(), 0, entry.ID)
	if err != nil {
		t.Fatalf("Post: %v", err)
	}
	if posted.PostedAt == nil {
		t.Error("PostedAt is nil")
	}
}

// ── Immutability tests (Invariant #5) ────────────────────────────────────────

// TestPosting_PostedJournal_CannotBeUpdated:
// Updating a posted journal must return ErrJournalAlreadyPosted.
func TestPosting_PostedJournal_CannotBeUpdated(t *testing.T) {
	svc, store := newTestService()
	entry := createAndPostBalanced(t, svc, 1_000_000)

	err := store.UpdateDraft(context.Background(), 0, entry.ID, map[string]interface{}{"description": "hacked"})
	if !errors.Is(err, ledger.ErrJournalAlreadyPosted) {
		t.Errorf("expected ErrJournalAlreadyPosted, got %v", err)
	}
}

// TestPosting_PostedJournal_CannotBeDeleted:
// Deleting a posted journal must return ErrJournalAlreadyPosted.
func TestPosting_PostedJournal_CannotBeDeleted(t *testing.T) {
	svc, store := newTestService()
	entry := createAndPostBalanced(t, svc, 2_500_000)

	err := store.DeleteDraft(context.Background(), 0, entry.ID)
	if !errors.Is(err, ledger.ErrJournalAlreadyPosted) {
		t.Errorf("expected ErrJournalAlreadyPosted, got %v", err)
	}
}

// TestPosting_DraftJournal_CanBeDeleted:
// An unposted draft must be deletable.
func TestPosting_DraftJournal_CanBeDeleted(t *testing.T) {
	svc, store := newTestService()
	req := ledger.CreateJournalRequest{
		TenantID:    0,
		Date:        time.Now(),
		Description: "draft to delete",
		Lines: []ledger.LineInput{
			{AccountID: 1, Debit: domain.FromInt(500_000)},
			{AccountID: 2, Credit: domain.FromInt(500_000)},
		},
	}
	entry, err := svc.Create(context.Background(), req)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := store.DeleteDraft(context.Background(), 0, entry.ID); err != nil {
		t.Errorf("DeleteDraft on draft: expected nil, got %v", err)
	}
}

// TestPosting_CannotPostTwice:
// Posting an already-posted journal must return ErrJournalAlreadyPosted.
func TestPosting_CannotPostTwice(t *testing.T) {
	svc, _ := newTestService()
	entry := createAndPostBalanced(t, svc, 1_000_000)

	_, err := svc.Post(context.Background(), 0, entry.ID)
	if !errors.Is(err, ledger.ErrJournalAlreadyPosted) {
		t.Errorf("expected ErrJournalAlreadyPosted on second post, got %v", err)
	}
}

// ── Reversing entry tests ─────────────────────────────────────────────────────

// TestPosting_ReversingEntry_SwapsDebitsAndCredits:
// The reversing entry must have all debits/credits swapped relative to the original.
func TestPosting_ReversingEntry_SwapsDebitsAndCredits(t *testing.T) {
	svc, _ := newTestService()
	entry := createAndPostBalanced(t, svc, 5_000_000)

	rev, err := svc.Reverse(context.Background(), 0, entry.ID, time.Now())
	if err != nil {
		t.Fatalf("Reverse: %v", err)
	}
	if !rev.IsReversing {
		t.Error("reversing entry should have IsReversing=true")
	}
	if rev.ReversesID == nil || *rev.ReversesID != entry.ID {
		t.Errorf("ReversesID should be %d, got %v", entry.ID, rev.ReversesID)
	}
	if rev.PostedAt == nil {
		t.Error("reversing entry should be posted immediately")
	}
	// Verify swaps
	for i, origLine := range entry.Lines {
		revLine := rev.Lines[i]
		if !revLine.Debit.Equal(origLine.Credit) || !revLine.Credit.Equal(origLine.Debit) {
			t.Errorf("line %d: expected debit=%s credit=%s, got debit=%s credit=%s",
				i, origLine.Credit, origLine.Debit, revLine.Debit, revLine.Credit)
		}
	}
}

// TestPosting_CannotReverseUnpostedJournal:
func TestPosting_CannotReverseUnpostedJournal(t *testing.T) {
	svc, _ := newTestService()
	req := ledger.CreateJournalRequest{
		TenantID:    0,
		Date:        time.Now(),
		Description: "draft",
		Lines: []ledger.LineInput{
			{AccountID: 1, Debit: domain.FromInt(100_000)},
			{AccountID: 2, Credit: domain.FromInt(100_000)},
		},
	}
	entry, _ := svc.Create(context.Background(), req)
	_, err := svc.Reverse(context.Background(), 0, entry.ID, time.Now())
	if !errors.Is(err, ledger.ErrJournalNotPosted) {
		t.Errorf("expected ErrJournalNotPosted, got %v", err)
	}
}

// TestPosting_CannotReverseAlreadyReversed:
func TestPosting_CannotReverseAlreadyReversed(t *testing.T) {
	svc, _ := newTestService()
	entry := createAndPostBalanced(t, svc, 1_000_000)

	_, err := svc.Reverse(context.Background(), 0, entry.ID, time.Now())
	if err != nil {
		t.Fatalf("first Reverse: %v", err)
	}
	_, err = svc.Reverse(context.Background(), 0, entry.ID, time.Now())
	if !errors.Is(err, ledger.ErrJournalAlreadyReversed) {
		t.Errorf("expected ErrJournalAlreadyReversed on second reverse, got %v", err)
	}
}

// ── Validation tests ──────────────────────────────────────────────────────────

func TestPosting_RejectsTooFewLines(t *testing.T) {
	svc, _ := newTestService()
	req := ledger.CreateJournalRequest{
		TenantID:    0,
		Date:        time.Now(),
		Description: "one-liner",
		Lines: []ledger.LineInput{
			{AccountID: 1, Debit: domain.FromInt(100_000)},
		},
	}
	_, err := svc.Create(context.Background(), req)
	if !errors.Is(err, ledger.ErrTooFewLines) {
		t.Errorf("expected ErrTooFewLines, got %v", err)
	}
}

func TestPosting_RejectsBothDebitAndCredit(t *testing.T) {
	svc, _ := newTestService()
	req := ledger.CreateJournalRequest{
		TenantID: 0,
		Date:     time.Now(),
		Lines: []ledger.LineInput{
			// both debit and credit on same line — invalid
			{AccountID: 1, Debit: domain.FromInt(100_000), Credit: domain.FromInt(100_000)},
			{AccountID: 2, Credit: domain.FromInt(100_000)},
		},
	}
	_, err := svc.Create(context.Background(), req)
	if !errors.Is(err, ledger.ErrLineInvalid) {
		t.Errorf("expected ErrLineInvalid, got %v", err)
	}
}

func TestPosting_RejectsZeroLine(t *testing.T) {
	svc, _ := newTestService()
	req := ledger.CreateJournalRequest{
		TenantID: 0,
		Date:     time.Now(),
		Lines: []ledger.LineInput{
			{AccountID: 1, Debit: domain.Zero},   // both zero — invalid
			{AccountID: 2, Credit: domain.FromInt(100_000)},
		},
	}
	_, err := svc.Create(context.Background(), req)
	if !errors.Is(err, ledger.ErrLineInvalid) {
		t.Errorf("expected ErrLineInvalid, got %v", err)
	}
}

// ── Invariant #2: rupiah bulat ────────────────────────────────────────────────

// TestPosting_RejectsFractionalDebit: baris debit 100.50 → ErrAmountNotWholeRupiah.
func TestPosting_RejectsFractionalDebit(t *testing.T) {
	svc, _ := newTestService()
	req := ledger.CreateJournalRequest{
		TenantID:    0,
		Date:        time.Now(),
		Description: "debit pecahan sen",
		Lines: []ledger.LineInput{
			{AccountID: 1, Debit: domain.MustParse("100.50")},
			{AccountID: 2, Credit: domain.MustParse("100.50")},
		},
	}
	_, err := svc.Create(context.Background(), req)
	if !errors.Is(err, ledger.ErrAmountNotWholeRupiah) {
		t.Errorf("expected ErrAmountNotWholeRupiah, got %v", err)
	}
}

// TestPosting_RejectsFractionalCredit: baris kredit 0.0001 → ErrAmountNotWholeRupiah.
func TestPosting_RejectsFractionalCredit(t *testing.T) {
	svc, _ := newTestService()
	req := ledger.CreateJournalRequest{
		TenantID:    0,
		Date:        time.Now(),
		Description: "kredit pecahan sen",
		Lines: []ledger.LineInput{
			{AccountID: 1, Debit: domain.MustParse("0.0001")},
			{AccountID: 2, Credit: domain.MustParse("0.0001")},
		},
	}
	_, err := svc.Create(context.Background(), req)
	if !errors.Is(err, ledger.ErrAmountNotWholeRupiah) {
		t.Errorf("expected ErrAmountNotWholeRupiah, got %v", err)
	}
}

// TestPosting_WholeRupiah_Passes: nominal bulat tetap lolos guard pecahan.
func TestPosting_WholeRupiah_Passes(t *testing.T) {
	svc, _ := newTestService()
	req := ledger.CreateJournalRequest{
		TenantID:    0,
		Date:        time.Now(),
		Description: "nominal bulat",
		Lines: []ledger.LineInput{
			{AccountID: 1, Debit: domain.FromInt(1_000_000)},
			{AccountID: 2, Credit: domain.FromInt(1_000_000)},
		},
	}
	_, err := svc.Create(context.Background(), req)
	if err != nil {
		t.Errorf("nominal bulat seharusnya lolos, got %v", err)
	}
}

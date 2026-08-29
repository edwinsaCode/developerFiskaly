package fixedasset

// Unit tests untuk Service — memakai mock TxRunner/Store/JournalWriter/
// CategoryResolver/Accounts (pola sama dengan internal/cost/service_test.go),
// jadi tidak butuh MySQL. Isolasi tenant, fail-closed kategori, dan unique
// constraint DB (INV-FA-1) diverifikasi terpisah di
// atomicity_integration_test.go terhadap MySQL nyata (TEST_DB_DSN).

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"esaproperti/internal/domain"
)

// ── Mocks ────────────────────────────────────────────────────────────────────

type mockCategoryResolver struct {
	policies map[uint64]CategoryPolicy
}

func (m *mockCategoryResolver) ResolveCategory(_ context.Context, _ uint64, id uint64) (CategoryPolicy, error) {
	p, ok := m.policies[id]
	if !ok {
		return CategoryPolicy{}, ErrCategoryNotFound
	}
	return p, nil
}

var standardCategory = CategoryPolicy{
	ID:                                 1,
	Code:                               "peralatan-kantor",
	Name:                               "Peralatan Kantor",
	AssetAccountCode:                   "1-4000",
	AccumulatedDepreciationAccountCode: "1-4900",
	DepreciationExpenseAccountCode:     "5-4500",
}

func defaultCategoryResolver() *mockCategoryResolver {
	return &mockCategoryResolver{policies: map[uint64]CategoryPolicy{1: standardCategory}}
}

type mockAccounts struct {
	ids map[string]uint64
}

func (m *mockAccounts) ResolveAccountID(_ context.Context, _ uint64, code string) (uint64, error) {
	id, ok := m.ids[code]
	if !ok {
		return 0, ErrAccountNotFound
	}
	return id, nil
}

func (m *mockAccounts) ValidatePaymentAccountCode(_ context.Context, _ uint64, code string) (uint64, error) {
	id, ok := m.ids[code]
	if !ok {
		return 0, ErrAccountNotFound
	}
	return id, nil
}

var standardFAAccounts = map[string]uint64{
	"1-4000": 100, // Peralatan Kantor (aset)
	"1-4900": 101, // Akumulasi Penyusutan
	"5-4500": 102, // Beban Penyusutan
	"1-1300": 200, // Bank BCA
}

func defaultAccounts() *mockAccounts {
	return &mockAccounts{ids: standardFAAccounts}
}

type capturedJournal struct {
	TenantID    uint64
	Date        time.Time
	Description string
	Lines       []JournalLineInput
}

type mockJournalWriter struct {
	mu     sync.Mutex
	calls  []capturedJournal
	nextID uint64
	posted map[uint64]bool
}

func newMockJournalWriter() *mockJournalWriter {
	return &mockJournalWriter{posted: make(map[uint64]bool)}
}

func (m *mockJournalWriter) CreateJournal(_ context.Context, tenantID uint64, date time.Time, description string, lines []JournalLineInput) (uint64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.nextID++
	cp := make([]JournalLineInput, len(lines))
	copy(cp, lines)
	m.calls = append(m.calls, capturedJournal{TenantID: tenantID, Date: date, Description: description, Lines: cp})
	return m.nextID, nil
}

func (m *mockJournalWriter) PostJournal(_ context.Context, _ uint64, journalID uint64, _ bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.posted[journalID] = true
	return nil
}

// mockStore is an in-memory Register + depreciation-line store.
type mockStore struct {
	mu             sync.Mutex
	assets         map[uint64]*FixedAsset
	nextAssetID    uint64
	lines          []*DepreciationLine
	nextLineID     uint64
	createAssetErr error
	createLineErr  error
}

func newMockStore() *mockStore {
	return &mockStore{assets: make(map[uint64]*FixedAsset)}
}

func (m *mockStore) CreateAsset(_ context.Context, asset *FixedAsset) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.createAssetErr != nil {
		return m.createAssetErr
	}
	m.nextAssetID++
	asset.ID = m.nextAssetID
	asset.AssetCode = "FA-TEST"
	cp := *asset
	m.assets[cp.ID] = &cp
	return nil
}

func (m *mockStore) GetAsset(_ context.Context, tenantID, assetID uint64) (*FixedAsset, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	a, ok := m.assets[assetID]
	if !ok || a.TenantID != tenantID {
		return nil, ErrAssetNotFound
	}
	cp := *a
	return &cp, nil
}

func (m *mockStore) ListActiveAssets(_ context.Context, tenantID uint64) ([]*FixedAsset, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []*FixedAsset
	for _, a := range m.assets {
		if a.TenantID == tenantID && a.Status == AssetStatusActive {
			cp := *a
			out = append(out, &cp)
		}
	}
	return out, nil
}

func (m *mockStore) HasDepreciationLine(_ context.Context, tenantID, assetID uint64, year uint16, month uint8) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, l := range m.lines {
		if l.TenantID == tenantID && l.FixedAssetID == assetID && l.PeriodYear == year && l.PeriodMonth == month {
			return true, nil
		}
	}
	return false, nil
}

func (m *mockStore) CreateDepreciationLine(_ context.Context, line *DepreciationLine) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.createLineErr != nil {
		return m.createLineErr
	}
	m.nextLineID++
	cp := *line
	cp.ID = m.nextLineID
	m.lines = append(m.lines, &cp)
	return nil
}

// mockTxRunner runs fn directly against shared mocks — no real transaction,
// which is fine for unit-level behavior (atomicity itself is verified
// against real MySQL in the integration test).
type mockTxRunner struct {
	journals *mockJournalWriter
	store    *mockStore
}

func (t *mockTxRunner) InTx(ctx context.Context, fn func(JournalWriter, Store) error) error {
	return fn(t.journals, t.store)
}

// ── Test fixtures ────────────────────────────────────────────────────────────

func newTestService() (*Service, *mockTxRunner, *mockJournalWriter, *mockStore, *mockCategoryResolver, *mockAccounts) {
	journals := newMockJournalWriter()
	store := newMockStore()
	runner := &mockTxRunner{journals: journals, store: store}
	cats := defaultCategoryResolver()
	accts := defaultAccounts()
	svc := NewService(runner, cats, accts)
	return svc, runner, journals, store, cats, accts
}

func baseAcquisition() AcquisitionInput {
	return AcquisitionInput{
		CategoryID:         1,
		AssetName:          "Laptop Dell",
		AcquisitionDate:    time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
		AcquisitionCost:    domain.FromInt(24_000_000),
		ResidualValue:      domain.FromInt(0),
		UsefulLifeMonths:   48,
		DepreciationMethod: DepreciationMethodStraightLine,
		PaymentMethod:      PaymentMethodBank,
		BankAccountCode:    "1-1300",
		Vendor:             "PT Sumber Komputer",
		Description:        "Laptop kerja tim finance",
	}
}

// ── AcquireAsset — happy path ─────────────────────────────────────────────────

func TestAcquireAsset_Success_BalancedJournal(t *testing.T) {
	svc, _, journals, store, _, _ := newTestService()

	asset, err := svc.AcquireAsset(context.Background(), 1, baseAcquisition())
	if err != nil {
		t.Fatalf("AcquireAsset: %v", err)
	}
	if asset.ID == 0 {
		t.Error("asset ID should be set")
	}
	if asset.Status != AssetStatusActive {
		t.Errorf("status = %s, want active", asset.Status)
	}
	if !asset.DepreciationStartDate.Equal(asset.AcquisitionDate) {
		t.Errorf("v1 convention: depreciation start = acquisition date; got %v want %v", asset.DepreciationStartDate, asset.AcquisitionDate)
	}

	if len(journals.calls) != 1 {
		t.Fatalf("expected 1 journal call, got %d", len(journals.calls))
	}
	call := journals.calls[0]
	if len(call.Lines) != 2 {
		t.Fatalf("expected 2 journal lines, got %d", len(call.Lines))
	}
	debit := call.Lines[0]
	credit := call.Lines[1]
	if debit.AccountID != 100 { // 1-4000
		t.Errorf("debit account = %d, want 100 (1-4000)", debit.AccountID)
	}
	if credit.AccountID != 200 { // 1-1300
		t.Errorf("credit account = %d, want 200 (1-1300)", credit.AccountID)
	}
	// Balanced: Σdebit == Σcredit (Invariant #1).
	if !debit.Debit.Equal(credit.Credit) {
		t.Errorf("journal not balanced: debit=%s credit=%s", debit.Debit, credit.Credit)
	}
	if !debit.Debit.Equal(domain.FromInt(24_000_000)) {
		t.Errorf("debit amount = %s, want 24000000", debit.Debit)
	}
	if !journals.posted[asset.AcquisitionJournalID] {
		t.Error("acquisition journal should be posted")
	}

	stored, err := store.GetAsset(context.Background(), 1, asset.ID)
	if err != nil {
		t.Fatalf("GetAsset: %v", err)
	}
	if stored.AcquisitionJournalID != asset.AcquisitionJournalID {
		t.Error("stored asset should be linked to the acquisition journal")
	}
}

func TestAcquireAsset_WithResidualValue(t *testing.T) {
	svc, _, _, _, _, _ := newTestService()
	in := baseAcquisition()
	in.ResidualValue = domain.FromInt(4_000_000)

	asset, err := svc.AcquireAsset(context.Background(), 1, in)
	if err != nil {
		t.Fatalf("AcquireAsset: %v", err)
	}
	want := domain.FromInt(20_000_000)
	if !asset.DepreciableAmount().Equal(want) {
		t.Errorf("depreciable amount = %s, want %s", asset.DepreciableAmount(), want)
	}
}

// ── Validation ────────────────────────────────────────────────────────────────

func TestAcquireAsset_Validation(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(in *AcquisitionInput)
		wantErr error
	}{
		{"name_required", func(in *AcquisitionInput) { in.AssetName = "   " }, ErrAssetNameRequired},
		{"cost_fractional", func(in *AcquisitionInput) { in.AcquisitionCost = domain.MustParse("24000000.50") }, ErrAmountFractional},
		{"cost_zero", func(in *AcquisitionInput) { in.AcquisitionCost = domain.FromInt(0) }, ErrAmountZeroOrNeg},
		{"cost_negative", func(in *AcquisitionInput) { in.AcquisitionCost = domain.MustParse("-100") }, ErrAmountZeroOrNeg},
		{"residual_fractional", func(in *AcquisitionInput) { in.ResidualValue = domain.MustParse("100.50") }, ErrAmountFractional},
		{"residual_negative", func(in *AcquisitionInput) { in.ResidualValue = domain.MustParse("-1") }, ErrResidualNegative},
		{"residual_exceeds_cost", func(in *AcquisitionInput) { in.ResidualValue = domain.FromInt(30_000_000) }, ErrResidualExceedsCost},
		{"useful_life_zero", func(in *AcquisitionInput) { in.UsefulLifeMonths = 0 }, ErrInvalidUsefulLife},
		{"method_invalid", func(in *AcquisitionInput) { in.DepreciationMethod = "declining_balance" }, ErrUnsupportedDepreciationMethod},
		{"payment_method_invalid", func(in *AcquisitionInput) { in.PaymentMethod = "cash_on_hand" }, ErrInvalidPaymentMethod},
		{"payable_not_supported", func(in *AcquisitionInput) { in.PaymentMethod = PaymentMethodPayable }, ErrPayableNotSupported},
		{"bank_code_required", func(in *AcquisitionInput) { in.BankAccountCode = "" }, ErrBankAccountCodeRequired},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			svc, _, journals, _, _, _ := newTestService()
			in := baseAcquisition()
			tc.mutate(&in)
			_, err := svc.AcquireAsset(context.Background(), 1, in)
			if !errors.Is(err, tc.wantErr) {
				t.Errorf("expected %v, got %v", tc.wantErr, err)
			}
			if len(journals.calls) != 0 {
				t.Error("no journal should be created on validation failure")
			}
		})
	}
}

func TestAcquireAsset_UnknownCategory_FailsClosed(t *testing.T) {
	svc, _, journals, _, _, _ := newTestService()
	in := baseAcquisition()
	in.CategoryID = 999

	_, err := svc.AcquireAsset(context.Background(), 1, in)
	if !errors.Is(err, ErrCategoryNotFound) {
		t.Errorf("expected ErrCategoryNotFound, got %v", err)
	}
	if len(journals.calls) != 0 {
		t.Error("no journal should be created for unknown category")
	}
}

// ── PreviewAcquisition ────────────────────────────────────────────────────────

func TestPreviewAcquisition_NoSideEffects(t *testing.T) {
	svc, _, journals, store, _, _ := newTestService()
	in := baseAcquisition()

	preview, err := svc.PreviewAcquisition(context.Background(), 1, in)
	if err != nil {
		t.Fatalf("PreviewAcquisition: %v", err)
	}
	if preview.DebitAccountCode != "1-4000" {
		t.Errorf("debit code = %q, want 1-4000", preview.DebitAccountCode)
	}
	if preview.CreditAccountCode != "1-1300" {
		t.Errorf("credit code = %q, want 1-1300", preview.CreditAccountCode)
	}
	if !preview.Amount.Equal(in.AcquisitionCost) {
		t.Errorf("amount = %s, want %s", preview.Amount, in.AcquisitionCost)
	}
	if len(journals.calls) != 0 {
		t.Error("preview must not write any journal")
	}
	if len(store.assets) != 0 {
		t.Error("preview must not write any asset")
	}
}

// ── depreciationSchedule — exactness (Invariant #3 applied to time) ──────────

func TestDepreciationSchedule_SumsExactly(t *testing.T) {
	cases := []struct {
		name             string
		depreciable      domain.Money
		usefulLifeMonths uint
	}{
		{"evenly_divisible", domain.FromInt(24_000_000), 48},
		{"not_evenly_divisible", domain.FromInt(10_000_000), 36}, // 277777.77... per month
		{"single_month", domain.FromInt(1_000_000), 1},
		{"odd_remainder", domain.FromInt(1_000_001), 3}, // remainder must land deterministically, not vanish
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			schedule := depreciationSchedule(tc.depreciable, tc.usefulLifeMonths)
			if len(schedule) != int(tc.usefulLifeMonths) {
				t.Fatalf("schedule length = %d, want %d", len(schedule), tc.usefulLifeMonths)
			}
			sum := domain.FromInt(0)
			for _, m := range schedule {
				sum = sum.Add(m)
			}
			if !sum.Equal(tc.depreciable) {
				t.Errorf("Σschedule = %s, want %s (exact, per Invariant #3)", sum, tc.depreciable)
			}
		})
	}
}

func TestDepreciationSchedule_ZeroDepreciable(t *testing.T) {
	schedule := depreciationSchedule(domain.FromInt(0), 12)
	if len(schedule) != 12 {
		t.Fatalf("schedule length = %d, want 12", len(schedule))
	}
	for i, m := range schedule {
		if !m.IsZero() {
			t.Errorf("month %d = %s, want 0", i, m)
		}
	}
}

func TestDepreciationSchedule_ZeroUsefulLife(t *testing.T) {
	schedule := depreciationSchedule(domain.FromInt(1_000_000), 0)
	if schedule != nil {
		t.Errorf("expected nil schedule for zero useful life, got %v", schedule)
	}
}

// ── monthsElapsed ─────────────────────────────────────────────────────────────

func TestMonthsElapsed(t *testing.T) {
	start := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC) // August 2026
	cases := []struct {
		year, month int
		want        int
	}{
		{2026, 8, 1},  // acquisition month itself
		{2026, 9, 2},  // next month
		{2026, 7, 0},  // before start
		{2027, 8, 13}, // exactly one year later
	}
	for _, tc := range cases {
		got := monthsElapsed(start, tc.year, tc.month)
		if got != tc.want {
			t.Errorf("monthsElapsed(%d-%02d) = %d, want %d", tc.year, tc.month, got, tc.want)
		}
	}
}

// ── RunDepreciation ───────────────────────────────────────────────────────────

func acquireForTest(t *testing.T, svc *Service, tenantID uint64) *FixedAsset {
	t.Helper()
	asset, err := svc.AcquireAsset(context.Background(), tenantID, baseAcquisition())
	if err != nil {
		t.Fatalf("AcquireAsset: %v", err)
	}
	return asset
}

func TestRunDepreciation_PostsBalancedJournal(t *testing.T) {
	svc, _, journals, store, _, _ := newTestService()
	asset := acquireForTest(t, svc, 1)
	journals.calls = nil // drop the acquisition call, keep only depreciation from here

	result, err := svc.RunDepreciation(context.Background(), 1, 2026, 8)
	if err != nil {
		t.Fatalf("RunDepreciation: %v", err)
	}
	if len(result.Posted) != 1 {
		t.Fatalf("expected 1 posted, got %d (skipped=%v)", len(result.Posted), result.Skipped)
	}
	if result.Posted[0].AssetID != asset.ID {
		t.Errorf("posted asset ID = %d, want %d", result.Posted[0].AssetID, asset.ID)
	}
	want := domain.FromInt(24_000_000 / 48) // 500000, evenly divisible
	if !result.Posted[0].Amount.Equal(want) {
		t.Errorf("posted amount = %s, want %s", result.Posted[0].Amount, want)
	}

	if len(journals.calls) != 1 {
		t.Fatalf("expected 1 depreciation journal, got %d", len(journals.calls))
	}
	call := journals.calls[0]
	debit, credit := call.Lines[0], call.Lines[1]
	if debit.AccountID != 102 { // 5-4500 beban penyusutan
		t.Errorf("debit account = %d, want 102", debit.AccountID)
	}
	if credit.AccountID != 101 { // 1-4900 akumulasi penyusutan
		t.Errorf("credit account = %d, want 101", credit.AccountID)
	}
	if !debit.Debit.Equal(credit.Credit) {
		t.Errorf("depreciation journal not balanced: debit=%s credit=%s", debit.Debit, credit.Credit)
	}

	if n := len(store.lines); n != 1 {
		t.Fatalf("expected 1 depreciation line stored, got %d", n)
	}
}

func TestRunDepreciation_Idempotent_SkipsDuplicatePeriod(t *testing.T) {
	svc, _, journals, _, _, _ := newTestService()
	acquireForTest(t, svc, 1)

	if _, err := svc.RunDepreciation(context.Background(), 1, 2026, 8); err != nil {
		t.Fatalf("first run: %v", err)
	}
	journals.calls = nil

	result, err := svc.RunDepreciation(context.Background(), 1, 2026, 8)
	if err != nil {
		t.Fatalf("second run: %v", err)
	}
	if len(result.Posted) != 0 {
		t.Errorf("second run for same period should post nothing, got %d", len(result.Posted))
	}
	if len(result.Skipped) != 1 {
		t.Fatalf("expected 1 skipped, got %d", len(result.Skipped))
	}
	if len(journals.calls) != 0 {
		t.Error("no duplicate journal should be created for an already-posted period (INV-FA-1)")
	}
}

func TestRunDepreciation_SkipsBeforeStartDate(t *testing.T) {
	svc, _, _, _, _, _ := newTestService()
	acquireForTest(t, svc, 1) // starts August 2026

	result, err := svc.RunDepreciation(context.Background(), 1, 2026, 7) // before start
	if err != nil {
		t.Fatalf("RunDepreciation: %v", err)
	}
	if len(result.Posted) != 0 {
		t.Errorf("expected 0 posted before start date, got %d", len(result.Posted))
	}
	if len(result.Skipped) != 1 {
		t.Fatalf("expected 1 skipped, got %d", len(result.Skipped))
	}
}

func TestRunDepreciation_SkipsAfterUsefulLifeExhausted(t *testing.T) {
	svc, _, _, _, _, _ := newTestService()
	in := baseAcquisition()
	in.UsefulLifeMonths = 1
	if _, err := svc.AcquireAsset(context.Background(), 1, in); err != nil {
		t.Fatalf("AcquireAsset: %v", err)
	}

	// Month 1 (August 2026) should post.
	r1, err := svc.RunDepreciation(context.Background(), 1, 2026, 8)
	if err != nil {
		t.Fatalf("run 1: %v", err)
	}
	if len(r1.Posted) != 1 {
		t.Fatalf("expected 1 posted in month 1, got %d", len(r1.Posted))
	}

	// Month 2 is past the 1-month useful life — must be skipped, not errored.
	r2, err := svc.RunDepreciation(context.Background(), 1, 2026, 9)
	if err != nil {
		t.Fatalf("run 2: %v", err)
	}
	if len(r2.Posted) != 0 {
		t.Errorf("expected 0 posted past useful life, got %d", len(r2.Posted))
	}
	if len(r2.Skipped) != 1 {
		t.Fatalf("expected 1 skipped past useful life, got %d", len(r2.Skipped))
	}
}

func TestRunDepreciation_CatchUpRun_UsesCorrectScheduleIndex(t *testing.T) {
	svc, _, _, _, _, _ := newTestService()
	acquireForTest(t, svc, 1) // 24,000,000 / 48mo = 500,000/mo, starts Aug 2026

	// Skip straight to month 3 (October 2026) without running months 1-2 —
	// simulates a catch-up run. Schedule index must still be 3, not 1.
	result, err := svc.RunDepreciation(context.Background(), 1, 2026, 10)
	if err != nil {
		t.Fatalf("RunDepreciation: %v", err)
	}
	if len(result.Posted) != 1 {
		t.Fatalf("expected 1 posted, got %d (skipped=%v)", len(result.Posted), result.Skipped)
	}
	// Straight-line with even division: every month is the same amount
	// regardless of index, so this mainly proves idx computation doesn't
	// error/skip incorrectly. The remainder-sensitive case is covered by
	// TestDepreciationSchedule_SumsExactly.
	want := domain.FromInt(500_000)
	if !result.Posted[0].Amount.Equal(want) {
		t.Errorf("posted amount = %s, want %s", result.Posted[0].Amount, want)
	}
}

func TestRunDepreciation_InvalidMonth_Rejected(t *testing.T) {
	svc, _, _, _, _, _ := newTestService()
	_, err := svc.RunDepreciation(context.Background(), 1, 2026, 13)
	if !errors.Is(err, ErrInvalidPeriod) {
		t.Errorf("expected ErrInvalidPeriod, got %v", err)
	}
	_, err = svc.RunDepreciation(context.Background(), 1, 2026, 0)
	if !errors.Is(err, ErrInvalidPeriod) {
		t.Errorf("expected ErrInvalidPeriod, got %v", err)
	}
}

func TestRunDepreciation_MultiTenant_OnlyAffectsOwnTenant(t *testing.T) {
	svc, _, journals, _, _, _ := newTestService()
	acquireForTest(t, svc, 1)
	acquireForTest(t, svc, 2)
	journals.calls = nil

	result, err := svc.RunDepreciation(context.Background(), 1, 2026, 8)
	if err != nil {
		t.Fatalf("RunDepreciation: %v", err)
	}
	if len(result.Posted) != 1 {
		t.Fatalf("expected 1 posted for tenant 1, got %d", len(result.Posted))
	}
	for _, call := range journals.calls {
		if call.TenantID != 1 {
			t.Errorf("journal written for wrong tenant: %d", call.TenantID)
		}
	}
}

// ── postOneDepreciation — race against the DB unique constraint ─────────────

func TestRunDepreciation_DBUniqueConstraintRace_TreatedAsSkip(t *testing.T) {
	svc, _, journals, store, _, _ := newTestService()
	acquireForTest(t, svc, 1)

	// Simulate HasDepreciationLine saying "not yet posted" (e.g. stale read)
	// while the DB unique constraint (INV-FA-1) still rejects the insert —
	// the final guard must convert this into a reported skip, not a lost
	// posting nor a hard failure that aborts the whole run.
	store.createLineErr = errors.New("Error 1062: Duplicate entry (unique constraint)")

	result, err := svc.RunDepreciation(context.Background(), 1, 2026, 8)
	if err != nil {
		t.Fatalf("RunDepreciation should not hard-fail on a race, got %v", err)
	}
	if len(result.Posted) != 0 {
		t.Errorf("expected 0 posted when the DB constraint rejects the line, got %d", len(result.Posted))
	}
	if len(result.Skipped) != 1 {
		t.Fatalf("expected 1 skipped (reported, not silently dropped), got %d", len(result.Skipped))
	}
	// The journal was created and posted before the store insert failed —
	// this is a known trade-off documented in postOneDepreciation: the
	// mock TxRunner doesn't roll back non-DB mocks. Real atomicity (that a
	// failed CreateDepreciationLine rolls back the journal too) is verified
	// against MySQL in atomicity_integration_test.go.
	_ = journals
}

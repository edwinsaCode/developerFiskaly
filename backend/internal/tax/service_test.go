package tax_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"esaproperti/internal/domain"
	"esaproperti/internal/tax"
)

// ── Helpers ───────────────────────────────────────────────────────────────────

func rupiah(n int64) domain.Money { return domain.FromInt(n) }

// sumLines verifies balance of journal lines.
func assertLinesBalanced(t *testing.T, tag string, lines []tax.JournalLineInput) {
	t.Helper()
	var debit, credit domain.Money
	for _, l := range lines {
		debit = debit.Add(l.Debit)
		credit = credit.Add(l.Credit)
	}
	if !debit.Equal(credit) {
		t.Errorf("%s: jurnal tidak balanced — debit=%s kredit=%s", tag, debit, credit)
	}
}

func assertWholeRupiah(t *testing.T, tag string, lines []tax.JournalLineInput) {
	t.Helper()
	for i, l := range lines {
		if !l.Debit.IsWholeRupiah() {
			t.Errorf("%s: baris[%d] debit=%s bukan rupiah bulat", tag, i, l.Debit)
		}
		if !l.Credit.IsWholeRupiah() {
			t.Errorf("%s: baris[%d] credit=%s bukan rupiah bulat", tag, i, l.Credit)
		}
	}
}

func sumDebitByAccount(lines []tax.JournalLineInput, accID uint64) domain.Money {
	var s domain.Money
	for _, l := range lines {
		if l.AccountID == accID {
			s = s.Add(l.Debit)
		}
	}
	return s
}

func sumCreditByAccount(lines []tax.JournalLineInput, accID uint64) domain.Money {
	var s domain.Money
	for _, l := range lines {
		if l.AccountID == accID {
			s = s.Add(l.Credit)
		}
	}
	return s
}

// ── Account IDs (fake, for mocks) ─────────────────────────────────────────────

const (
	accBeban  uint64 = 5200 // 5-2000 Beban PPh Final Pengalihan
	accHutang uint64 = 2400 // 2-4000 Hutang PPh Final Pengalihan
	accBank   uint64 = 1300 // 1-1300 Bank BCA
	accBank2  uint64 = 1400 // 1-1400 Bank Mandiri
)

var standardAccounts = map[string]uint64{
	"5-2000": accBeban,
	"2-4000": accHutang,
	"1-1300": accBank,
	"1-1400": accBank2,
	"1-1500": 1500,
}

// ── Mocks ─────────────────────────────────────────────────────────────────────

type mockAccountFinder struct{ accounts map[string]uint64 }

func (m *mockAccountFinder) FindAccountIDByCode(_ context.Context, _ uint64, code string) (uint64, error) {
	id, ok := m.accounts[code]
	if !ok {
		return 0, errors.New("akun tidak ditemukan: " + code)
	}
	return id, nil
}

// ResolvePaymentAccountID meniru validasi COA-driven produksi: kode yang tidak
// dikenal COA ditolak sebagai akun kas/bank (W-3.0 B-1).
func (m *mockAccountFinder) ResolvePaymentAccountID(_ context.Context, _ uint64, code string) (uint64, error) {
	id, ok := m.accounts[code]
	if !ok {
		return 0, tax.ErrInvalidBankAccount
	}
	return id, nil
}

// capturedJournal records what was passed to the journal writer for test assertions.
type capturedJournal struct {
	lines []tax.JournalLineInput
}

type mockJournalWriter struct {
	nextID    uint64
	captured  []*capturedJournal
	createErr error
	postErr   error
	// lastCashOut merekam nilai penanda kas keluar pada PostJournal terakhir —
	// dipakai menguji bahwa akrual TIDAK ditandai kas, setoran ditandai kas.
	lastCashOut bool
}

func (m *mockJournalWriter) CreateJournal(_ context.Context, _ uint64, _ time.Time, _ string, lines []tax.JournalLineInput) (uint64, error) {
	if m.createErr != nil {
		return 0, m.createErr
	}
	m.nextID++
	cp := make([]tax.JournalLineInput, len(lines))
	copy(cp, lines)
	m.captured = append(m.captured, &capturedJournal{lines: cp})
	return m.nextID, nil
}

func (m *mockJournalWriter) PostJournal(_ context.Context, _, _ uint64, cashOut bool) error {
	m.lastCashOut = cashOut
	return m.postErr
}

// mockRateProvider returns configured rates; simulates a DB-backed dated-rate system.
type mockRateProvider struct {
	rates []tax.TaxRate
}

func (m *mockRateProvider) GetCurrentRate(_ context.Context, _ uint64, rateCode string, referenceDate time.Time) (*tax.TaxRate, error) {
	var best *tax.TaxRate
	for i := range m.rates {
		r := &m.rates[i]
		if r.RateCode != rateCode {
			continue
		}
		if !r.EffectiveFrom.After(referenceDate) {
			if best == nil || r.EffectiveFrom.After(best.EffectiveFrom) {
				best = r
			}
		}
	}
	if best == nil {
		return nil, tax.ErrRateNotConfigured
	}
	return best, nil
}

func (m *mockRateProvider) SaveRate(_ context.Context, r *tax.TaxRate) error {
	m.rates = append(m.rates, *r)
	return nil
}

// mockTaxStore tracks obligations and payments.
type mockTaxStore struct {
	obligations []*tax.TaxObligation
	payments    []*tax.TaxPayment
	nextOblID   uint64
	nextPayID   uint64
	saveObErr   error
	savePayErr  error
}

func (m *mockTaxStore) SaveObligation(_ context.Context, o *tax.TaxObligation) error {
	if m.saveObErr != nil {
		return m.saveObErr
	}
	m.nextOblID++
	o.ID = m.nextOblID
	m.obligations = append(m.obligations, o)
	return nil
}

func (m *mockTaxStore) FindObligationByID(_ context.Context, tenantID, id uint64) (*tax.TaxObligation, error) {
	for _, o := range m.obligations {
		if o.ID == id && o.TenantID == tenantID {
			return o, nil
		}
	}
	return nil, tax.ErrObligationNotFound
}

func (m *mockTaxStore) UpdateObligationStatus(_ context.Context, tenantID, id uint64, status tax.TaxObligationStatus) error {
	for _, o := range m.obligations {
		if o.ID == id && o.TenantID == tenantID {
			o.Status = status
			return nil
		}
	}
	return tax.ErrObligationNotFound
}

func (m *mockTaxStore) SavePayment(_ context.Context, p *tax.TaxPayment) error {
	if m.savePayErr != nil {
		return m.savePayErr
	}
	m.nextPayID++
	p.ID = m.nextPayID
	m.payments = append(m.payments, p)
	return nil
}

func (m *mockTaxStore) ListObligationsByPeriod(_ context.Context, tenantID uint64, from, to time.Time) ([]*tax.TaxObligation, error) {
	var result []*tax.TaxObligation
	for _, o := range m.obligations {
		if o.TenantID == tenantID && !o.AccrualDate.Before(from) && !o.AccrualDate.After(to) {
			result = append(result, o)
		}
	}
	return result, nil
}

func (m *mockTaxStore) ListObligationsForUnits(_ context.Context, tenantID uint64, rateCode string, unitIDs []uint64) ([]*tax.TaxObligation, error) {
	idSet := make(map[uint64]bool, len(unitIDs))
	for _, id := range unitIDs {
		idSet[id] = true
	}
	var result []*tax.TaxObligation
	for _, o := range m.obligations {
		if o.TenantID == tenantID && o.RateCode == rateCode && o.UnitID != nil && idSet[*o.UnitID] {
			result = append(result, o)
		}
	}
	return result, nil
}

// mockBASTReader simulasi daftar unit yang sudah BAST dalam periode.
type mockBASTReader struct {
	unitIDs []uint64
}

func (m *mockBASTReader) ListBASTUnitIDsInPeriod(_ context.Context, _ uint64, _, _ time.Time) ([]uint64, error) {
	return m.unitIDs, nil
}

// mockLedgerReader simulasi saldo sisi kredit/debit per kode akun.
type mockLedgerReader struct {
	creditByCode map[string]domain.Money
	debitByCode  map[string]domain.Money
}

func (m *mockLedgerReader) SumCreditByAccountCodeAndPeriod(_ context.Context, _ uint64, code string, _, _ time.Time) (domain.Money, error) {
	if m.creditByCode != nil {
		if v, ok := m.creditByCode[code]; ok {
			return v, nil
		}
	}
	return domain.Zero, nil
}

func (m *mockLedgerReader) SumDebitByAccountCodeAndPeriod(_ context.Context, _ uint64, code string, _, _ time.Time) (domain.Money, error) {
	if m.debitByCode != nil {
		if v, ok := m.debitByCode[code]; ok {
			return v, nil
		}
	}
	return domain.Zero, nil
}

// ── builder ───────────────────────────────────────────────────────────────────

func buildService(
	rates []tax.TaxRate,
	accounts map[string]uint64,
	jw *mockJournalWriter,
	store *mockTaxStore,
	opts ...tax.ServiceOption,
) *tax.Service {
	if jw == nil {
		jw = &mockJournalWriter{}
	}
	if store == nil {
		store = &mockTaxStore{}
	}
	af := &mockAccountFinder{accounts: accounts}
	rp := &mockRateProvider{rates: rates}
	return tax.NewService(rp, af, jw, store, opts...)
}

func rateAt(rate float64, from time.Time) tax.TaxRate {
	return tax.TaxRate{
		TenantID:      1,
		RateCode:      tax.RateCodePPhFinalPengalihan,
		Rate:          decimal.NewFromFloat(rate),
		EffectiveFrom: from,
	}
}

var epoch2016 = time.Date(2016, 5, 8, 0, 0, 0, 0, time.UTC)
var epoch2024 = time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

// ── Tests: Event 5a — AccrueTax ───────────────────────────────────────────────

func TestTax_AccrueTax_DefaultRate_25Persen(t *testing.T) {
	// tarif 2,5%, nilai pengalihan 1M → pajak 25.000 (1,000,000 × 0.025)
	svc := buildService([]tax.TaxRate{rateAt(0.025, epoch2016)}, standardAccounts, nil, nil)

	req := tax.AccrueTaxRequest{
		RateCode:      tax.RateCodePPhFinalPengalihan,
		TransferValue: rupiah(1_000_000),
		AccrualDate:   time.Now(),
	}
	obl, err := svc.AccrueTax(context.Background(), 1, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := rupiah(25_000)
	if !obl.TaxAmount.Equal(expected) {
		t.Errorf("TaxAmount=%s, want 25000", obl.TaxAmount)
	}
	if obl.Status != tax.ObligationStatusOutstanding {
		t.Errorf("Status=%q, want outstanding", obl.Status)
	}
}

// DoD: "tarif diambil dari rule bertanggal, BUKAN konstanta inline
//
//	(test: ganti tarif via config → angka ikut berubah tanpa ubah kode posting)"
func TestTax_AccrueTax_CustomRate_FromConfig_NotHardcoded(t *testing.T) {
	// Ganti tarif ke 3% → pajak = 3% × 1B = 30M, BUKAN 2,5% × 1B = 25M
	// Membuktikan tarif berasal dari konfigurasi, bukan inline di kode posting.
	svc := buildService([]tax.TaxRate{rateAt(0.03, epoch2016)}, standardAccounts, nil, nil)

	req := tax.AccrueTaxRequest{
		RateCode:      tax.RateCodePPhFinalPengalihan,
		TransferValue: rupiah(1_000_000_000),
		AccrualDate:   time.Now(),
	}
	obl, err := svc.AccrueTax(context.Background(), 1, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := rupiah(30_000_000) // 3%, bukan 2,5%
	if !obl.TaxAmount.Equal(expected) {
		t.Errorf("TaxAmount=%s, want 30000000 (3%%) — bukan nilai hardcoded 2,5%%", obl.TaxAmount)
	}
}

// DoD: tarif bertanggal — rate berbeda untuk periode berbeda.
func TestTax_AccrueTax_DateBasedRate_CorrectRateSelected(t *testing.T) {
	rates := []tax.TaxRate{
		rateAt(0.025, epoch2016), // berlaku mulai 2016
		rateAt(0.03, epoch2024),  // berlaku mulai 2024
	}
	svc := buildService(rates, standardAccounts, nil, nil)
	transfer := rupiah(1_000_000_000)

	// Akrual di 2023 → harus pakai tarif 2,5%
	req2023 := tax.AccrueTaxRequest{
		RateCode: tax.RateCodePPhFinalPengalihan, TransferValue: transfer,
		AccrualDate: time.Date(2023, 6, 1, 0, 0, 0, 0, time.UTC),
	}
	obl23, err := svc.AccrueTax(context.Background(), 1, req2023)
	if err != nil {
		t.Fatalf("2023: %v", err)
	}
	if !obl23.TaxAmount.Equal(rupiah(25_000_000)) {
		t.Errorf("2023: TaxAmount=%s, want 25000000 (2,5%%)", obl23.TaxAmount)
	}

	// Akrual di 2025 → harus pakai tarif 3%
	req2025 := tax.AccrueTaxRequest{
		RateCode: tax.RateCodePPhFinalPengalihan, TransferValue: transfer,
		AccrualDate: time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC),
	}
	obl25, err := svc.AccrueTax(context.Background(), 1, req2025)
	if err != nil {
		t.Fatalf("2025: %v", err)
	}
	if !obl25.TaxAmount.Equal(rupiah(30_000_000)) {
		t.Errorf("2025: TaxAmount=%s, want 30000000 (3%%)", obl25.TaxAmount)
	}
}

func TestTax_AccrueTax_JournalBalanced_5a(t *testing.T) {
	// posting-rules.md Contoh 5a: nilai pengalihan 1B, PPh 25M
	// Dr 5-2000 25.000.000 / Cr 2-4000 25.000.000 → Σdebit == Σkredit
	jw := &mockJournalWriter{}
	svc := buildService([]tax.TaxRate{rateAt(0.025, epoch2016)}, standardAccounts, jw, nil)

	req := tax.AccrueTaxRequest{
		RateCode:      tax.RateCodePPhFinalPengalihan,
		TransferValue: rupiah(1_000_000_000),
		AccrualDate:   time.Now(),
	}
	_, err := svc.AccrueTax(context.Background(), 1, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(jw.captured) == 0 {
		t.Fatal("tidak ada jurnal yang dibuat")
	}
	lines := jw.captured[0].lines
	assertLinesBalanced(t, "Event5a", lines)
	assertWholeRupiah(t, "Event5a", lines)

	// Dr 5-2000
	drBeban := sumDebitByAccount(lines, accBeban)
	if !drBeban.Equal(rupiah(25_000_000)) {
		t.Errorf("Dr 5-2000=%s, want 25000000", drBeban)
	}
	// Cr 2-4000
	crHutang := sumCreditByAccount(lines, accHutang)
	if !crHutang.Equal(rupiah(25_000_000)) {
		t.Errorf("Cr 2-4000=%s, want 25000000", crHutang)
	}
	// Tidak ada akun pendapatan/HPP/persediaan yang tersentuh
	for _, l := range lines {
		if l.AccountID != accBeban && l.AccountID != accHutang {
			t.Errorf("akun yang tidak seharusnya muncul: id=%d", l.AccountID)
		}
	}
}

func TestTax_AccrueTax_RoundingToWholeRupiah(t *testing.T) {
	// 100,000,001 × 0.025 = 2,500,000.025 → round ke 2,500,000
	svc := buildService([]tax.TaxRate{rateAt(0.025, epoch2016)}, standardAccounts, nil, nil)
	req := tax.AccrueTaxRequest{
		RateCode:      tax.RateCodePPhFinalPengalihan,
		TransferValue: rupiah(100_000_001),
		AccrualDate:   time.Now(),
	}
	obl, err := svc.AccrueTax(context.Background(), 1, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !obl.TaxAmount.IsWholeRupiah() {
		t.Errorf("TaxAmount=%s bukan rupiah bulat (Invariant #2)", obl.TaxAmount)
	}
}

func TestTax_AccrueTax_RateSnapshotSaved(t *testing.T) {
	// Tarif saat akrual di-snapshot → perubahan tarif setelah itu tidak mempengaruhi
	svc := buildService([]tax.TaxRate{rateAt(0.025, epoch2016)}, standardAccounts, nil, nil)
	req := tax.AccrueTaxRequest{
		RateCode:      tax.RateCodePPhFinalPengalihan,
		TransferValue: rupiah(1_000_000_000),
		AccrualDate:   time.Now(),
	}
	obl, err := svc.AccrueTax(context.Background(), 1, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := decimal.NewFromFloat(0.025)
	if !obl.Rate.Equal(expected) {
		t.Errorf("Rate snapshot=%s, want 0.025000", obl.Rate)
	}
}

func TestTax_AccrueTax_FractionalTransferValue_ReturnsError(t *testing.T) {
	svc := buildService([]tax.TaxRate{rateAt(0.025, epoch2016)}, standardAccounts, nil, nil)
	m, _ := domain.NewMoney("1000000000.5")
	req := tax.AccrueTaxRequest{
		RateCode: tax.RateCodePPhFinalPengalihan, TransferValue: m, AccrualDate: time.Now(),
	}
	_, err := svc.AccrueTax(context.Background(), 1, req)
	if !errors.Is(err, tax.ErrTransferValueFractional) {
		t.Errorf("expected ErrTransferValueFractional, got %v", err)
	}
}

func TestTax_AccrueTax_ZeroTransferValue_ReturnsError(t *testing.T) {
	svc := buildService([]tax.TaxRate{rateAt(0.025, epoch2016)}, standardAccounts, nil, nil)
	req := tax.AccrueTaxRequest{
		RateCode: tax.RateCodePPhFinalPengalihan, TransferValue: domain.Zero, AccrualDate: time.Now(),
	}
	_, err := svc.AccrueTax(context.Background(), 1, req)
	if !errors.Is(err, tax.ErrTransferValueZeroOrNeg) {
		t.Errorf("expected ErrTransferValueZeroOrNeg, got %v", err)
	}
}

func TestTax_AccrueTax_RateNotConfigured_ReturnsError(t *testing.T) {
	// Tidak ada rate di database → harus error
	svc := buildService(nil, standardAccounts, nil, nil)
	req := tax.AccrueTaxRequest{
		RateCode: tax.RateCodePPhFinalPengalihan, TransferValue: rupiah(1_000_000), AccrualDate: time.Now(),
	}
	_, err := svc.AccrueTax(context.Background(), 1, req)
	if !errors.Is(err, tax.ErrRateNotConfigured) {
		t.Errorf("expected ErrRateNotConfigured, got %v", err)
	}
}

func TestTax_AccrueTax_PostingRules_Contoh5a_Exact(t *testing.T) {
	// posting-rules.md Contoh: nilai pengalihan 1B, PPh = 25M persis
	jw := &mockJournalWriter{}
	svc := buildService([]tax.TaxRate{rateAt(0.025, epoch2016)}, standardAccounts, jw, nil)

	req := tax.AccrueTaxRequest{
		RateCode:      tax.RateCodePPhFinalPengalihan,
		TransferValue: rupiah(1_000_000_000), // 1 Miliar
		AccrualDate:   time.Now(),
	}
	obl, err := svc.AccrueTax(context.Background(), 1, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Angka dari posting-rules.md Contoh 5a
	if !obl.TaxAmount.Equal(rupiah(25_000_000)) {
		t.Errorf("TaxAmount=%s, want 25000000 (sesuai posting-rules.md Contoh 5a)", obl.TaxAmount)
	}
}

// ── Tests: Event 5b — PayTax ──────────────────────────────────────────────────

func TestTax_PayTax_DrHutang_CrBank_Balanced(t *testing.T) {
	// posting-rules.md Contoh 5b: Dr 2-4000 25M / Cr Bank 25M
	jw := &mockJournalWriter{}
	store := &mockTaxStore{}
	svc := buildService([]tax.TaxRate{rateAt(0.025, epoch2016)}, standardAccounts, jw, store)

	// Buat obligation dulu
	req5a := tax.AccrueTaxRequest{
		RateCode: tax.RateCodePPhFinalPengalihan, TransferValue: rupiah(1_000_000_000), AccrualDate: time.Now(),
	}
	obl, err := svc.AccrueTax(context.Background(), 1, req5a)
	if err != nil {
		t.Fatalf("5a: %v", err)
	}

	// Event 5b
	req5b := tax.PayTaxRequest{
		ObligationID:    obl.ID,
		BankAccountCode: "1-1300",
		PaymentDate:     time.Now(),
	}
	payment, err := svc.PayTax(context.Background(), 1, req5b)
	if err != nil {
		t.Fatalf("5b: %v", err)
	}
	if !payment.Amount.Equal(obl.TaxAmount) {
		t.Errorf("payment.Amount=%s, want %s", payment.Amount, obl.TaxAmount)
	}

	// Jurnal 5b harus balanced
	if len(jw.captured) < 2 {
		t.Fatalf("expected ≥2 captured journals, got %d", len(jw.captured))
	}
	lines5b := jw.captured[1].lines // jurnal kedua = 5b
	assertLinesBalanced(t, "Event5b", lines5b)
	assertWholeRupiah(t, "Event5b", lines5b)

	// Dr 2-4000
	drHutang := sumDebitByAccount(lines5b, accHutang)
	if !drHutang.Equal(obl.TaxAmount) {
		t.Errorf("Dr 2-4000=%s, want %s", drHutang, obl.TaxAmount)
	}
	// Cr Bank
	crBank := sumCreditByAccount(lines5b, accBank)
	if !crBank.Equal(obl.TaxAmount) {
		t.Errorf("Cr Bank=%s, want %s", crBank, obl.TaxAmount)
	}
}

func TestTax_PayTax_ObligationStatusBecomePaid(t *testing.T) {
	store := &mockTaxStore{}
	svc := buildService([]tax.TaxRate{rateAt(0.025, epoch2016)}, standardAccounts, nil, store)

	obl, _ := svc.AccrueTax(context.Background(), 1, tax.AccrueTaxRequest{
		RateCode: tax.RateCodePPhFinalPengalihan, TransferValue: rupiah(500_000_000), AccrualDate: time.Now(),
	})

	_, err := svc.PayTax(context.Background(), 1, tax.PayTaxRequest{
		ObligationID: obl.ID, BankAccountCode: "1-1300", PaymentDate: time.Now(),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Obligation status harus berubah ke paid
	updated, _ := store.FindObligationByID(context.Background(), 1, obl.ID)
	if updated.Status != tax.ObligationStatusPaid {
		t.Errorf("status=%q, want paid", updated.Status)
	}
}

func TestTax_PayTax_AlreadyPaid_ReturnsError(t *testing.T) {
	store := &mockTaxStore{}
	svc := buildService([]tax.TaxRate{rateAt(0.025, epoch2016)}, standardAccounts, nil, store)

	obl, _ := svc.AccrueTax(context.Background(), 1, tax.AccrueTaxRequest{
		RateCode: tax.RateCodePPhFinalPengalihan, TransferValue: rupiah(500_000_000), AccrualDate: time.Now(),
	})

	// Bayar pertama kali — berhasil
	req5b := tax.PayTaxRequest{ObligationID: obl.ID, BankAccountCode: "1-1300", PaymentDate: time.Now()}
	_, err := svc.PayTax(context.Background(), 1, req5b)
	if err != nil {
		t.Fatalf("first payment: %v", err)
	}

	// Bayar kedua kali — harus error
	_, err = svc.PayTax(context.Background(), 1, req5b)
	if !errors.Is(err, tax.ErrObligationAlreadyPaid) {
		t.Errorf("expected ErrObligationAlreadyPaid, got %v", err)
	}
}

func TestTax_PayTax_InvalidBank_ReturnsError(t *testing.T) {
	store := &mockTaxStore{}
	svc := buildService([]tax.TaxRate{rateAt(0.025, epoch2016)}, standardAccounts, nil, store)

	obl, _ := svc.AccrueTax(context.Background(), 1, tax.AccrueTaxRequest{
		RateCode: tax.RateCodePPhFinalPengalihan, TransferValue: rupiah(100_000_000), AccrualDate: time.Now(),
	})
	_, err := svc.PayTax(context.Background(), 1, tax.PayTaxRequest{
		ObligationID: obl.ID, BankAccountCode: "9-9999", PaymentDate: time.Now(),
	})
	if !errors.Is(err, tax.ErrInvalidBankAccount) {
		t.Errorf("expected ErrInvalidBankAccount, got %v", err)
	}
}

func TestTax_PayTax_ObligationNotFound_ReturnsError(t *testing.T) {
	svc := buildService([]tax.TaxRate{rateAt(0.025, epoch2016)}, standardAccounts, nil, nil)
	_, err := svc.PayTax(context.Background(), 1, tax.PayTaxRequest{
		ObligationID: 9999, BankAccountCode: "1-1300", PaymentDate: time.Now(),
	})
	if !errors.Is(err, tax.ErrObligationNotFound) {
		t.Errorf("expected ErrObligationNotFound, got %v", err)
	}
}

func TestTax_PayTax_DoesNotTouchRevenuOrInventory(t *testing.T) {
	// PPh Final Event 5b TIDAK boleh menyentuh Pendapatan/HPP/Persediaan
	jw := &mockJournalWriter{}
	store := &mockTaxStore{}
	svc := buildService([]tax.TaxRate{rateAt(0.025, epoch2016)}, standardAccounts, jw, store)

	obl, _ := svc.AccrueTax(context.Background(), 1, tax.AccrueTaxRequest{
		RateCode: tax.RateCodePPhFinalPengalihan, TransferValue: rupiah(1_000_000_000), AccrualDate: time.Now(),
	})
	_, err := svc.PayTax(context.Background(), 1, tax.PayTaxRequest{
		ObligationID: obl.ID, BankAccountCode: "1-1300", PaymentDate: time.Now(),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	forbiddenIDs := map[uint64]string{
		401: "4-1000 Pendapatan",
		501: "5-1000 HPP",
		100: "1-3000 Persediaan Tanah",
		101: "1-3100 Persediaan Hard",
		102: "1-3200 Persediaan Soft",
		103: "1-3300 Persediaan Financing",
	}
	for _, j := range jw.captured {
		for _, l := range j.lines {
			if name, forbidden := forbiddenIDs[l.AccountID]; forbidden {
				t.Errorf("PPh Final tidak boleh menyentuh %s (accID=%d)", name, l.AccountID)
			}
		}
	}
}

// ── Tests: SetTaxRate & GetCurrentRate ───────────────────────────────────────

func TestTax_SetTaxRate_Valid(t *testing.T) {
	rp := &mockRateProvider{}
	svc := tax.NewService(rp, &mockAccountFinder{accounts: standardAccounts}, &mockJournalWriter{}, &mockTaxStore{})

	err := svc.SetTaxRate(context.Background(), 1, tax.SetTaxRateRequest{
		RateCode:      tax.RateCodePPhFinalPengalihan,
		Rate:          decimal.NewFromFloat(0.025),
		EffectiveFrom: epoch2016,
		Description:   "PP 34/2016",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rp.rates) == 0 {
		t.Error("rate tidak tersimpan")
	}
}

func TestTax_SetTaxRate_InvalidRate_ReturnsError(t *testing.T) {
	rp := &mockRateProvider{}
	svc := tax.NewService(rp, &mockAccountFinder{accounts: standardAccounts}, &mockJournalWriter{}, &mockTaxStore{})

	err := svc.SetTaxRate(context.Background(), 1, tax.SetTaxRateRequest{
		RateCode:      tax.RateCodePPhFinalPengalihan,
		Rate:          decimal.Zero, // 0% tidak valid
		EffectiveFrom: epoch2016,
	})
	if !errors.Is(err, tax.ErrRateValueInvalid) {
		t.Errorf("expected ErrRateValueInvalid, got %v", err)
	}
}

func TestTax_SetTaxRate_ZeroDate_ReturnsError(t *testing.T) {
	rp := &mockRateProvider{}
	svc := tax.NewService(rp, &mockAccountFinder{accounts: standardAccounts}, &mockJournalWriter{}, &mockTaxStore{})

	err := svc.SetTaxRate(context.Background(), 1, tax.SetTaxRateRequest{
		RateCode: tax.RateCodePPhFinalPengalihan,
		Rate:     decimal.NewFromFloat(0.025),
		// EffectiveFrom: zero — invalid
	})
	if !errors.Is(err, tax.ErrEffectiveDateRequired) {
		t.Errorf("expected ErrEffectiveDateRequired, got %v", err)
	}
}

// ── Tests: GetTaxReport ───────────────────────────────────────────────────────

func TestTax_GetTaxReport_SumsTotalObligationAndPaid(t *testing.T) {
	store := &mockTaxStore{}
	svc := buildService([]tax.TaxRate{rateAt(0.025, epoch2016)}, standardAccounts, nil, store)

	// Buat 3 kewajiban (25M + 12.5M + 50M = 87.5M)
	// Bayar 1 (25M)
	now := time.Now()

	obl1, _ := svc.AccrueTax(context.Background(), 1, tax.AccrueTaxRequest{
		RateCode: tax.RateCodePPhFinalPengalihan, TransferValue: rupiah(1_000_000_000), AccrualDate: now,
	})
	obl2, _ := svc.AccrueTax(context.Background(), 1, tax.AccrueTaxRequest{
		RateCode: tax.RateCodePPhFinalPengalihan, TransferValue: rupiah(500_000_000), AccrualDate: now,
	})
	_, _ = svc.AccrueTax(context.Background(), 1, tax.AccrueTaxRequest{
		RateCode: tax.RateCodePPhFinalPengalihan, TransferValue: rupiah(2_000_000_000), AccrualDate: now,
	})

	// Bayar obl1 dan obl2
	_, _ = svc.PayTax(context.Background(), 1, tax.PayTaxRequest{ObligationID: obl1.ID, BankAccountCode: "1-1300", PaymentDate: now})
	_, _ = svc.PayTax(context.Background(), 1, tax.PayTaxRequest{ObligationID: obl2.ID, BankAccountCode: "1-1300", PaymentDate: now})

	from := now.Add(-time.Hour)
	to := now.Add(time.Hour)
	report, err := svc.GetTaxReport(context.Background(), 1, from, to)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(report.Items) != 3 {
		t.Errorf("items=%d, want 3", len(report.Items))
	}

	totalObl := rupiah(25_000_000 + 12_500_000 + 50_000_000) // 87.5M
	totalPaid := rupiah(25_000_000 + 12_500_000)             // 37.5M
	totalOutstanding := rupiah(50_000_000)

	if report.TotalObligation != totalObl.String() {
		t.Errorf("TotalObligation=%s, want %s", report.TotalObligation, totalObl)
	}
	if report.TotalPaid != totalPaid.String() {
		t.Errorf("TotalPaid=%s, want %s", report.TotalPaid, totalPaid)
	}
	if report.TotalOutstanding != totalOutstanding.String() {
		t.Errorf("TotalOutstanding=%s, want %s", report.TotalOutstanding, totalOutstanding)
	}
}

func TestTax_GetTaxReport_PeriodFiltering(t *testing.T) {
	store := &mockTaxStore{}
	svc := buildService([]tax.TaxRate{rateAt(0.025, epoch2016)}, standardAccounts, nil, store)

	jan := time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC)
	mar := time.Date(2024, 3, 15, 0, 0, 0, 0, time.UTC)

	// Akrual Januari
	_, _ = svc.AccrueTax(context.Background(), 1, tax.AccrueTaxRequest{
		RateCode: tax.RateCodePPhFinalPengalihan, TransferValue: rupiah(1_000_000_000), AccrualDate: jan,
	})
	// Akrual Maret
	_, _ = svc.AccrueTax(context.Background(), 1, tax.AccrueTaxRequest{
		RateCode: tax.RateCodePPhFinalPengalihan, TransferValue: rupiah(1_000_000_000), AccrualDate: mar,
	})

	// Laporan hanya untuk Januari
	report, err := svc.GetTaxReport(context.Background(), 1,
		time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2024, 1, 31, 0, 0, 0, 0, time.UTC),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(report.Items) != 1 {
		t.Errorf("expected 1 item in January report, got %d", len(report.Items))
	}
}

// ── Tests: Cross-tenant isolation ────────────────────────────────────────────

func TestTax_PayTax_CrossTenant_NotFound(t *testing.T) {
	store := &mockTaxStore{}
	svc := buildService([]tax.TaxRate{rateAt(0.025, epoch2016)}, standardAccounts, nil, store)

	// Tenant 1 buat obligation
	obl, _ := svc.AccrueTax(context.Background(), 1, tax.AccrueTaxRequest{
		RateCode: tax.RateCodePPhFinalPengalihan, TransferValue: rupiah(500_000_000), AccrualDate: time.Now(),
	})

	// Tenant 2 coba bayar → harus NotFound
	_, err := svc.PayTax(context.Background(), 2, tax.PayTaxRequest{
		ObligationID: obl.ID, BankAccountCode: "1-1300", PaymentDate: time.Now(),
	})
	if !errors.Is(err, tax.ErrObligationNotFound) {
		t.Errorf("expected ErrObligationNotFound for cross-tenant, got %v", err)
	}
}

func TestTax_AccrueTax_RateCode_TenantScoped(t *testing.T) {
	// Tenant 2 tidak punya rate → ErrRateNotConfigured
	// (mock hanya punya rate untuk tenant 1 — tapi mock tidak filter tenant,
	//  jadi kita simulasikan dengan rateProvider yang mengembalikan error)
	rp := &mockRateProvider{} // tidak ada rate sama sekali
	af := &mockAccountFinder{accounts: standardAccounts}
	svc := tax.NewService(rp, af, &mockJournalWriter{}, &mockTaxStore{})

	_, err := svc.AccrueTax(context.Background(), 2, tax.AccrueTaxRequest{
		RateCode: tax.RateCodePPhFinalPengalihan, TransferValue: rupiah(1_000_000_000), AccrualDate: time.Now(),
	})
	if !errors.Is(err, tax.ErrRateNotConfigured) {
		t.Errorf("expected ErrRateNotConfigured for unconfigured tenant, got %v", err)
	}
}

// ── Tests: PPh Final TIDAK mengubah pendapatan/HPP/persediaan ────────────────

func TestTax_AccrueTax_5a_OnlyBebandAndHutang(t *testing.T) {
	// Event 5a hanya boleh menyentuh 5-2000 (Beban) dan 2-4000 (Hutang)
	// TIDAK boleh ada akun 4-1000/5-1000/1-3xxx
	jw := &mockJournalWriter{}
	svc := buildService([]tax.TaxRate{rateAt(0.025, epoch2016)}, standardAccounts, jw, nil)

	_, err := svc.AccrueTax(context.Background(), 1, tax.AccrueTaxRequest{
		RateCode: tax.RateCodePPhFinalPengalihan, TransferValue: rupiah(1_000_000_000), AccrualDate: time.Now(),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, l := range jw.captured[0].lines {
		if l.AccountID != accBeban && l.AccountID != accHutang {
			t.Errorf("Event 5a: akun tidak terduga id=%d (hanya 5-2000 dan 2-4000 yang boleh)", l.AccountID)
		}
	}
}

// ── Tests Phase 8: penjaga audit (BAST tanpa PPh Final) ──────────────────────

// DoD: unit dengan BAST tapi tanpa akrual PPh Final harus muncul dalam penjaga.
func TestTax_ListBASTWithoutPPhFinal_FindsGap(t *testing.T) {
	store := &mockTaxStore{}
	bastReader := &mockBASTReader{unitIDs: []uint64{5}}
	svc := buildService(
		[]tax.TaxRate{rateAt(0.025, epoch2016)},
		standardAccounts,
		nil,
		store,
		tax.WithBASTReader(bastReader),
	)

	// Tidak ada obligation untuk unit 5 → penjaga harus menemukannya
	from := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2024, 12, 31, 0, 0, 0, 0, time.UTC)

	items, err := svc.ListBASTWithoutPPhFinal(context.Background(), 1, from, to)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 item (gap ditemukan), got %d", len(items))
	}
	if items[0].UnitID != 5 {
		t.Errorf("expected unit_id=5, got %d", items[0].UnitID)
	}
}

// DoD: setelah akrual PPh Final ada, unit tidak boleh muncul lagi.
func TestTax_ListBASTWithoutPPhFinal_NoGapWhenObligated(t *testing.T) {
	store := &mockTaxStore{}
	bastReader := &mockBASTReader{unitIDs: []uint64{5}}
	svc := buildService(
		[]tax.TaxRate{rateAt(0.025, epoch2016)},
		standardAccounts,
		nil,
		store,
		tax.WithBASTReader(bastReader),
	)

	// Buat akrual PPh Final untuk unit 5 → gap tertutup
	unitID := uint64(5)
	_, err := svc.AccrueTax(context.Background(), 1, tax.AccrueTaxRequest{
		RateCode:      tax.RateCodePPhFinalPengalihan,
		TransferValue: rupiah(1_000_000_000),
		AccrualDate:   time.Now(),
		UnitID:        &unitID,
	})
	if err != nil {
		t.Fatalf("AccrueTax: %v", err)
	}

	from := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2024, 12, 31, 0, 0, 0, 0, time.UTC)

	items, err := svc.ListBASTWithoutPPhFinal(context.Background(), 1, from, to)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(items) != 0 {
		t.Errorf("expected 0 items (gap tertutup setelah akrual), got %d", len(items))
	}
}

// ── Tests Phase 8: laporan PPN (rekonsiliasi ke 2-3000) ──────────────────────

// DoD: PPNKeluaran = Σ kredit 2-3000; PPNMasukan = Σ debit 1-5100;
//
//	PPNTerutang = Keluaran − Masukan; semua akurat ke ledger.
func TestTax_GetVATReport_ReconcilesTo2_3000(t *testing.T) {
	ledgerReader := &mockLedgerReader{
		creditByCode: map[string]domain.Money{
			"2-3000": rupiah(110_000_000), // PPN 11% dari penjualan 1B
		},
		debitByCode: map[string]domain.Money{
			"1-5100": rupiah(5_000_000), // PPN Masukan
		},
	}
	svc := buildService(nil, standardAccounts, nil, nil, tax.WithLedgerReader(ledgerReader))

	from := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2024, 12, 31, 0, 0, 0, 0, time.UTC)

	report, err := svc.GetVATReport(context.Background(), 1, from, to)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if report.PPNKeluaran != rupiah(110_000_000).String() {
		t.Errorf("PPNKeluaran=%s, want %s", report.PPNKeluaran, rupiah(110_000_000))
	}
	if report.PPNMasukan != rupiah(5_000_000).String() {
		t.Errorf("PPNMasukan=%s, want %s", report.PPNMasukan, rupiah(5_000_000))
	}
	// PPNTerutang = 110M − 5M = 105M
	wantTerutang := rupiah(105_000_000)
	if report.PPNTerutang != wantTerutang.String() {
		t.Errorf("PPNTerutang=%s, want %s", report.PPNTerutang, wantTerutang)
	}
}

func TestTax_GetVATReport_ZeroIfNoTransactions(t *testing.T) {
	// Periode kosong → semua nol
	svc := buildService(nil, standardAccounts, nil, nil, tax.WithLedgerReader(&mockLedgerReader{}))

	from := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2024, 1, 31, 0, 0, 0, 0, time.UTC)

	report, err := svc.GetVATReport(context.Background(), 1, from, to)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if report.PPNKeluaran != domain.Zero.String() {
		t.Errorf("PPNKeluaran=%s, want %s (zero)", report.PPNKeluaran, domain.Zero)
	}
	if report.PPNTerutang != domain.Zero.String() {
		t.Errorf("PPNTerutang=%s, want %s (zero)", report.PPNTerutang, domain.Zero)
	}
}

// ── Tests Phase 8: laporan kewajiban gabungan ─────────────────────────────────

// DoD: laporan gabungan berisi PPh Final dan PPN; keduanya tidak nil.
func TestTax_GetCombinedTaxReport_CombinesBoth(t *testing.T) {
	store := &mockTaxStore{}
	ledgerReader := &mockLedgerReader{
		creditByCode: map[string]domain.Money{
			"2-3000": rupiah(110_000_000),
		},
	}
	svc := buildService(
		[]tax.TaxRate{rateAt(0.025, epoch2016)},
		standardAccounts,
		nil,
		store,
		tax.WithLedgerReader(ledgerReader),
	)

	// Buat 1 kewajiban PPh Final (1B × 2,5% = 25M)
	now := time.Now()
	_, err := svc.AccrueTax(context.Background(), 1, tax.AccrueTaxRequest{
		RateCode:      tax.RateCodePPhFinalPengalihan,
		TransferValue: rupiah(1_000_000_000),
		AccrualDate:   now,
	})
	if err != nil {
		t.Fatalf("AccrueTax: %v", err)
	}

	from := now.Add(-time.Hour)
	to := now.Add(time.Hour)

	report, err := svc.GetCombinedTaxReport(context.Background(), 1, from, to)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if report.PPHFinal == nil {
		t.Fatal("PPHFinal nil dalam laporan gabungan")
	}
	if report.PPN == nil {
		t.Fatal("PPN nil dalam laporan gabungan")
	}
	if len(report.PPHFinal.Items) != 1 {
		t.Errorf("PPHFinal.Items=%d, want 1", len(report.PPHFinal.Items))
	}
	if report.PPHFinal.TotalObligation != rupiah(25_000_000).String() {
		t.Errorf("PPHFinal.TotalObligation=%s, want 25000000", report.PPHFinal.TotalObligation)
	}
	if report.PPN.PPNKeluaran != rupiah(110_000_000).String() {
		t.Errorf("PPN.PPNKeluaran=%s, want 110000000", report.PPN.PPNKeluaran)
	}
}

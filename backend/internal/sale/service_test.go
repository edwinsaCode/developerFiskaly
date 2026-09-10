package sale_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"esaproperti/internal/domain"
	"esaproperti/internal/sale"
)

// ── Helpers ───────────────────────────────────────────────────────────────────

func rupiah(n int64) domain.Money { return domain.FromInt(n) }

func ptr[T any](v T) *T { return &v }

// sumLines computes Σdebit and Σcredit for a slice of journal lines.
func sumLines(lines []sale.JournalLineInput) (debit, credit domain.Money) {
	for _, l := range lines {
		debit = debit.Add(l.Debit)
		credit = credit.Add(l.Credit)
	}
	return
}

// assertBalanced verifies the journal lines satisfy Σdebit == Σcredit.
func assertBalanced(t *testing.T, tag string, lines []sale.JournalLineInput) {
	t.Helper()
	d, c := sumLines(lines)
	if !d.Equal(c) {
		t.Errorf("%s: jurnal tidak balanced — debit=%s kredit=%s", tag, d, c)
	}
}

// assertWholeRupiah verifies no fractional amounts exist in any line.
func assertWholeRupiah(t *testing.T, tag string, lines []sale.JournalLineInput) {
	t.Helper()
	for i, l := range lines {
		if !l.Debit.IsWholeRupiah() {
			t.Errorf("%s: baris %d debit=%s bukan rupiah bulat", tag, i, l.Debit)
		}
		if !l.Credit.IsWholeRupiah() {
			t.Errorf("%s: baris %d credit=%s bukan rupiah bulat", tag, i, l.Credit)
		}
	}
}

// sumCreditByAccount sums credits for a specific account ID across lines.
func sumCreditByAccount(lines []sale.JournalLineInput, accountID uint64) domain.Money {
	var sum domain.Money
	for _, l := range lines {
		if l.AccountID == accountID {
			sum = sum.Add(l.Credit)
		}
	}
	return sum
}

func sumDebitByAccount(lines []sale.JournalLineInput, accountID uint64) domain.Money {
	var sum domain.Money
	for _, l := range lines {
		if l.AccountID == accountID {
			sum = sum.Add(l.Debit)
		}
	}
	return sum
}

// ── Mocks ─────────────────────────────────────────────────────────────────────

// Standard account ID map (matches COA codes → fake DB IDs for tests).
const (
	accBank1300   uint64 = 200 // 1-1300 Bank BCA
	accBank1400   uint64 = 201 // 1-1400 Bank Mandiri
	accBank1500   uint64 = 202 // 1-1500 Bank BRI
	accPiutang    uint64 = 301 // 1-2000 Piutang Usaha
	accUMP        uint64 = 302 // 2-2000 Uang Muka Penjualan
	accPPNKeluar  uint64 = 303 // 2-3000 PPN Keluaran
	accPendapatan uint64 = 401 // 4-1000 Pendapatan Penjualan Unit
	accHPP        uint64 = 501 // 5-1000 HPP
	accLand       uint64 = 100 // 1-3000 Persediaan Tanah
	accHard       uint64 = 101 // 1-3100 Persediaan Hard Cost
	accSoft       uint64 = 102 // 1-3200 Persediaan Soft Cost
	accFinancing  uint64 = 103 // 1-3300 Persediaan Financing
)

var standardAccounts = map[string]uint64{
	"1-1300": accBank1300,
	"1-1400": accBank1400,
	"1-1500": accBank1500,
	"1-2000": accPiutang,
	"2-2000": accUMP,
	"2-3000": accPPNKeluar,
	"4-1000": accPendapatan,
	"5-1000": accHPP,
	"1-3000": accLand,
	"1-3100": accHard,
	"1-3200": accSoft,
	"1-3300": accFinancing,
}

type mockAccountFinder struct {
	accounts map[string]uint64
	// validateErr: override hasil ValidateCashBankAccount per kode (untuk uji penolakan).
	validateErr map[string]error
}

func (m *mockAccountFinder) FindAccountIDByCode(_ context.Context, _ uint64, code string) (uint64, error) {
	id, ok := m.accounts[code]
	if !ok {
		return 0, errors.New("account not found: " + code)
	}
	return id, nil
}

// defaultCashBankCodes meniru akun kas/bank aktif hasil seed COA.
var defaultCashBankCodes = map[string]bool{
	"1-1100": true, "1-1200": true, "1-1300": true, "1-1400": true, "1-1500": true,
}

func (m *mockAccountFinder) ValidateCashBankAccount(_ context.Context, _ uint64, code string) error {
	if m.validateErr != nil {
		if e, ok := m.validateErr[code]; ok {
			return e
		}
	}
	if defaultCashBankCodes[code] {
		return nil
	}
	return sale.ErrInvalidBankAccount // bukan akun kas/bank
}

type mockJournalWriter struct {
	nextID    uint64
	postErr   error
	lastLines []sale.JournalLineInput   // baris jurnal pemanggilan terakhir
	allLines  [][]sale.JournalLineInput // riwayat semua pemanggilan
}

func (m *mockJournalWriter) CreateJournal(_ context.Context, _ uint64, _ time.Time, _ string, lines []sale.JournalLineInput) (uint64, error) {
	m.nextID++
	m.lastLines = lines
	m.allLines = append(m.allLines, lines)
	return m.nextID, nil
}

func (m *mockJournalWriter) PostJournal(_ context.Context, _, _ uint64) error {
	return m.postErr
}

type mockUnitReader struct {
	units map[uint64]*sale.UnitSaleInfo
}

func (m *mockUnitReader) FindUnitSaleInfo(_ context.Context, _ uint64, unitID uint64) (*sale.UnitSaleInfo, error) {
	u, ok := m.units[unitID]
	if !ok {
		return nil, sale.ErrUnitNotFound
	}
	return u, nil
}

type mockTerminStore struct {
	termins []*sale.TerminPayment
	saveErr error
	// saleRecord (jika di-set) ditemukan oleh FindSaleRecord hanya untuk
	// saleRecordTenant — mensimulasikan tenant-scoping bukti BAST.
	saleRecord       *sale.SaleRecord
	saleRecordTenant uint64
}

func (m *mockTerminStore) SaveTermin(_ context.Context, t *sale.TerminPayment) error {
	if m.saveErr != nil {
		return m.saveErr
	}
	if t.ID == 0 {
		t.ID = uint64(len(m.termins) + 1) // simulasi auto-increment
	}
	m.termins = append(m.termins, t)
	return nil
}

func (m *mockTerminStore) SumTerminsByUnit(_ context.Context, _ uint64, unitID uint64) (domain.Money, error) {
	var sum domain.Money
	for _, t := range m.termins {
		if t.UnitID == unitID {
			sum = sum.Add(t.Amount)
		}
	}
	return sum, nil
}

func (m *mockTerminStore) SumTerminsByUnitAndCreditAccount(_ context.Context, _ uint64, unitID uint64, creditAccountCode string) (domain.Money, error) {
	var sum domain.Money
	for _, t := range m.termins {
		if t.UnitID == unitID && t.CreditAccountCode == creditAccountCode {
			sum = sum.Add(t.Amount)
		}
	}
	return sum, nil
}

func (m *mockTerminStore) ListTerminsByUnit(_ context.Context, tenantID, unitID uint64) ([]*sale.TerminPayment, error) {
	var out []*sale.TerminPayment
	for _, t := range m.termins {
		if t.UnitID == unitID {
			out = append(out, t)
		}
	}
	return out, nil
}

func (m *mockTerminStore) FindSaleRecord(_ context.Context, tenantID uint64, unitID uint64) (*sale.SaleRecord, error) {
	if m.saleRecord != nil && m.saleRecordTenant == tenantID && m.saleRecord.UnitID == unitID {
		return m.saleRecord, nil
	}
	return nil, sale.ErrSaleRecordNotFound
}

// SetSaleRecord menandai unit sudah BAST (test helper).
func (m *mockTerminStore) SetSaleRecord(sr *sale.SaleRecord, tenantID uint64) {
	m.saleRecord = sr
	m.saleRecordTenant = tenantID
}

// TerminCount mengembalikan jumlah termin tersimpan (test helper).
func (m *mockTerminStore) TerminCount() int { return len(m.termins) }

func (m *mockTerminStore) FindTerminByIdempotencyKey(_ context.Context, tenantID uint64, key string) (*sale.TerminPayment, error) {
	for _, t := range m.termins {
		if t.TenantID == tenantID && t.IdempotencyKey != nil && *t.IdempotencyKey == key {
			return t, nil
		}
	}
	return nil, sale.ErrTerminNotFound
}

// mockBASTWriter captures what the service passes to it for verification.
type mockBASTWriter struct {
	captured *sale.BASTAtomicParams
	result   *sale.SaleRecord
	err      error
}

func (m *mockBASTWriter) Execute(_ context.Context, p sale.BASTAtomicParams) (*sale.SaleRecord, error) {
	m.captured = &p
	if m.err != nil {
		return nil, m.err
	}
	if m.result != nil {
		return m.result, nil
	}
	return &sale.SaleRecord{
		ID: 1, TenantID: p.TenantID, UnitID: p.UnitID, ProjectID: p.ProjectID,
		SalePrice: p.SalePrice, IsVAT: p.IsVAT, VATRate: p.VATRate,
		TotalAdvanceAtBAST: p.TotalAdvance,
		HPPLand:            p.HPPLand, HPPHard: p.HPPHard, HPPSoft: p.HPPSoft, HPPFinancing: p.HPPFinancing,
		BuyerRef: p.BuyerRef, RecognitionDate: p.BASTDate,
	}, nil
}

// mockUnitCostProvider provides a fixed unit cost breakdown for testing.
type mockUnitCostProvider struct {
	costs map[uint64]domain.UnitCostBreakdown
	err   error
}

func (m *mockUnitCostProvider) GetUnitCost(_ context.Context, _, _, unitID uint64) (domain.UnitCostBreakdown, error) {
	if m.err != nil {
		return domain.UnitCostBreakdown{}, m.err
	}
	b, ok := m.costs[unitID]
	if !ok {
		return domain.UnitCostBreakdown{}, nil
	}
	return b, nil
}

// ── Service builder ───────────────────────────────────────────────────────────

func buildService(
	accounts map[string]uint64,
	units map[uint64]*sale.UnitSaleInfo,
	termins []*sale.TerminPayment,
	costs map[uint64]domain.UnitCostBreakdown,
	bastWriter *mockBASTWriter,
) (*sale.Service, *mockBASTWriter, *mockTerminStore) {
	af := &mockAccountFinder{accounts: accounts}
	jw := &mockJournalWriter{nextID: 0}
	ur := &mockUnitReader{units: units}
	ts := &mockTerminStore{termins: termins}
	cp := &mockUnitCostProvider{costs: costs}
	if bastWriter == nil {
		bastWriter = &mockBASTWriter{}
	}
	// ReceivePayment butuh ContractStore; sediakan mock kosong (advance tanpa kontrak).
	svc := sale.NewService(af, jw, ur, ts, cp, bastWriter, sale.WithContractStore(newMockContractStore()))
	return svc, bastWriter, ts
}

// recvUnitTermin mencatat penerimaan advance unit (source=unit_termin) via
// ReceivePayment — pengganti pemanggilan langsung primitif lama RecordTermin.
func recvUnitTermin(svc *sale.Service, tenant, unitID uint64, bank string, amount domain.Money) (*sale.ReceivePaymentResult, error) {
	uid := unitID
	return svc.ReceivePayment(context.Background(), tenant, sale.ReceivePaymentRequest{
		Source:          sale.PaymentSourceUnitTermin,
		UnitID:          &uid,
		BankAccountCode: bank,
		Amount:          amount,
		Date:            time.Now(),
	})
}

func defaultUnit(id uint64, status string) *sale.UnitSaleInfo {
	return &sale.UnitSaleInfo{
		ID: id, ProjectID: 10, PhaseID: ptr(uint64(1)), Status: status,
	}
}

// ── Tests: RecordTermin (Event 2) ─────────────────────────────────────────────

func TestService_RecordTermin_CreatesJournal_DrBank_CrUMP(t *testing.T) {
	svc, _, ts := buildService(standardAccounts, map[uint64]*sale.UnitSaleInfo{
		5: defaultUnit(5, "reserved"),
	}, nil, nil, nil)

	res, err := recvUnitTermin(svc, 1, 5, "1-1300", rupiah(200_000_000))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.TerminID == 0 {
		t.Error("payment/termin not created")
	}
	if len(ts.termins) != 1 {
		t.Errorf("termin not saved: len=%d", len(ts.termins))
	}
	if ts.termins[0].JournalEntryID == 0 {
		t.Error("journal not created")
	}
	if res.CreditAccount != "2-2000" {
		t.Errorf("credit account=%s, want 2-2000 (pre-BAST)", res.CreditAccount)
	}
}

func TestService_RecordTermin_FractionalAmount_ReturnsError(t *testing.T) {
	svc, _, _ := buildService(standardAccounts, map[uint64]*sale.UnitSaleInfo{5: defaultUnit(5, "available")}, nil, nil, nil)
	m, _ := domain.NewMoney("200000000.5")
	_, err := recvUnitTermin(svc, 1, 5, "1-1300", m)
	if !errors.Is(err, sale.ErrTerminAmountFractional) {
		t.Errorf("expected ErrTerminAmountFractional, got %v", err)
	}
}

func TestService_RecordTermin_ZeroAmount_ReturnsError(t *testing.T) {
	svc, _, _ := buildService(standardAccounts, map[uint64]*sale.UnitSaleInfo{5: defaultUnit(5, "available")}, nil, nil, nil)
	_, err := recvUnitTermin(svc, 1, 5, "1-1300", domain.Zero)
	if !errors.Is(err, sale.ErrTerminAmountZeroOrNeg) {
		t.Errorf("expected ErrTerminAmountZeroOrNeg, got %v", err)
	}
}

func TestService_RecordTermin_InvalidBank_ReturnsError(t *testing.T) {
	svc, _, _ := buildService(standardAccounts, map[uint64]*sale.UnitSaleInfo{5: defaultUnit(5, "available")}, nil, nil, nil)
	_, err := recvUnitTermin(svc, 1, 5, "9-9999", rupiah(100_000))
	if !errors.Is(err, sale.ErrInvalidBankAccount) {
		t.Errorf("expected ErrInvalidBankAccount, got %v", err)
	}
}

func TestService_RecordTermin_AlreadySold_ReturnsError(t *testing.T) {
	svc, _, _ := buildService(standardAccounts, map[uint64]*sale.UnitSaleInfo{5: defaultUnit(5, "sold")}, nil, nil, nil)
	_, err := recvUnitTermin(svc, 1, 5, "1-1300", rupiah(100_000))
	if !errors.Is(err, sale.ErrUnitAlreadySold) {
		t.Errorf("expected ErrUnitAlreadySold, got %v", err)
	}
}

func TestService_RecordTermin_UnitRequired_ReturnsError(t *testing.T) {
	svc, _, _ := buildService(standardAccounts, nil, nil, nil, nil)
	_, err := recvUnitTermin(svc, 1, 0, "1-1300", rupiah(100_000))
	if !errors.Is(err, sale.ErrUnitRequired) {
		t.Errorf("expected ErrUnitRequired, got %v", err)
	}
}

// ── Tests: BankFee (UAT 2026-09-03, Rule #5 — biaya realisasi/pengajuan KPR
// yang DITANGGUNG DEVELOPER, bukan titipan customer) ─────────────────────────

const accBankFeeExpense uint64 = 550 // 5-3200 Beban Provisi & Administrasi Bank KPR

var accountsWithBankFee = func() map[string]uint64 {
	m := make(map[string]uint64, len(standardAccounts)+1)
	for k, v := range standardAccounts {
		m[k] = v
	}
	m["5-3200"] = accBankFeeExpense
	return m
}()

func TestService_ReceivePayment_BankFee_SplitsDebit_StaysBalanced(t *testing.T) {
	svc, _, ts := buildService(accountsWithBankFee, map[uint64]*sale.UnitSaleInfo{
		5: defaultUnit(5, "reserved"),
	}, nil, nil, nil)

	uid := uint64(5)
	_, err := svc.ReceivePayment(context.Background(), 1, sale.ReceivePaymentRequest{
		Source:          sale.PaymentSourceUnitTermin,
		UnitID:          &uid,
		BankAccountCode: "1-1300",
		Amount:          rupiah(100_000_000), // nilai piutang yang diselesaikan (bruto)
		BankFee:         rupiah(1_500_000),   // provisi bank dipotong dari pencairan
		Date:            time.Now(),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ts.termins) != 1 || ts.termins[0].JournalEntryID == 0 {
		t.Fatalf("termin/journal not created")
	}
	// termin_payments.amount tetap NILAI BRUTO — waterfall/AR/statement/receipt
	// tidak boleh terpengaruh oleh bank_fee (hanya sisi debit jurnal yang pecah).
	if !ts.termins[0].Amount.Equal(rupiah(100_000_000)) {
		t.Errorf("termin amount = %s, want 100,000,000 (gross, unaffected by bank_fee)", ts.termins[0].Amount)
	}
}

func TestService_RecordTermin_BankFee_JournalBalancedAndRouted(t *testing.T) {
	svc, _, ts := buildService(accountsWithBankFee, map[uint64]*sale.UnitSaleInfo{
		5: defaultUnit(5, "reserved"),
	}, nil, nil, nil)
	// buildService tidak mengembalikan mockJournalWriter secara langsung; ambil
	// baris jurnal lewat termin yang tersimpan + akses ke jw via closure tidak
	// tersedia, jadi verifikasi melalui service kedua yang expose jw.
	_ = svc
	_ = ts

	af := &mockAccountFinder{accounts: accountsWithBankFee}
	jw := &mockJournalWriter{}
	ur := &mockUnitReader{units: map[uint64]*sale.UnitSaleInfo{5: defaultUnit(5, "reserved")}}
	tsr := &mockTerminStore{}
	svc2 := sale.NewService(af, jw, ur, tsr, &mockUnitCostProvider{}, &mockBASTWriter{}, sale.WithContractStore(newMockContractStore()))

	uid := uint64(5)
	_, err := svc2.ReceivePayment(context.Background(), 1, sale.ReceivePaymentRequest{
		Source:          sale.PaymentSourceUnitTermin,
		UnitID:          &uid,
		BankAccountCode: "1-1300",
		Amount:          rupiah(100_000_000),
		BankFee:         rupiah(1_500_000),
		Date:            time.Now(),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	lines := jw.lastLines
	assertBalanced(t, "BankFee split", lines)
	assertWholeRupiah(t, "BankFee split", lines)

	if got := sumDebitByAccount(lines, accBank1300); !got.Equal(rupiah(98_500_000)) {
		t.Errorf("Dr kas bersih = %s, want 98,500,000 (100,000,000 - 1,500,000)", got)
	}
	if got := sumDebitByAccount(lines, accBankFeeExpense); !got.Equal(rupiah(1_500_000)) {
		t.Errorf("Dr Beban Provisi Bank (5-3200) = %s, want 1,500,000", got)
	}
	if got := sumCreditByAccount(lines, accUMP); !got.Equal(rupiah(100_000_000)) {
		t.Errorf("Cr Uang Muka Penjualan = %s, want 100,000,000 penuh (nilai piutang tidak berkurang oleh bank_fee)", got)
	}
}

func TestService_RecordTermin_BankFee_Zero_ProducesIdenticalTwoLineJournal(t *testing.T) {
	af := &mockAccountFinder{accounts: accountsWithBankFee}
	jw := &mockJournalWriter{}
	ur := &mockUnitReader{units: map[uint64]*sale.UnitSaleInfo{5: defaultUnit(5, "reserved")}}
	tsr := &mockTerminStore{}
	svc := sale.NewService(af, jw, ur, tsr, &mockUnitCostProvider{}, &mockBASTWriter{}, sale.WithContractStore(newMockContractStore()))

	uid := uint64(5)
	_, err := svc.ReceivePayment(context.Background(), 1, sale.ReceivePaymentRequest{
		Source:          sale.PaymentSourceUnitTermin,
		UnitID:          &uid,
		BankAccountCode: "1-1300",
		Amount:          rupiah(200_000_000),
		Date:            time.Now(),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(jw.lastLines) != 2 {
		t.Fatalf("BankFee=0 harus menghasilkan jurnal 2 baris seperti sebelumnya, got %d baris", len(jw.lastLines))
	}
}

func TestService_ReceivePayment_BankFee_EqualsAmount_ReturnsError(t *testing.T) {
	uid := uint64(5)
	svc, _, _ := buildService(accountsWithBankFee, map[uint64]*sale.UnitSaleInfo{
		5: defaultUnit(5, "reserved"),
	}, nil, nil, nil)
	_, err := svc.ReceivePayment(context.Background(), 1, sale.ReceivePaymentRequest{
		Source:          sale.PaymentSourceUnitTermin,
		UnitID:          &uid,
		BankAccountCode: "1-1300",
		Amount:          rupiah(100_000_000),
		BankFee:         rupiah(100_000_000), // 100% dipotong — kas bersih nol, bukan kasus nyata
		Date:            time.Now(),
	})
	if !errors.Is(err, sale.ErrBankFeeInvalid) {
		t.Errorf("expected ErrBankFeeInvalid, got %v", err)
	}
}

func TestService_ReceivePayment_BankFee_ExceedsAmount_ReturnsError(t *testing.T) {
	uid := uint64(5)
	svc, _, _ := buildService(accountsWithBankFee, map[uint64]*sale.UnitSaleInfo{
		5: defaultUnit(5, "reserved"),
	}, nil, nil, nil)
	_, err := svc.ReceivePayment(context.Background(), 1, sale.ReceivePaymentRequest{
		Source:          sale.PaymentSourceUnitTermin,
		UnitID:          &uid,
		BankAccountCode: "1-1300",
		Amount:          rupiah(100_000_000),
		BankFee:         rupiah(150_000_000),
		Date:            time.Now(),
	})
	if !errors.Is(err, sale.ErrBankFeeInvalid) {
		t.Errorf("expected ErrBankFeeInvalid, got %v", err)
	}
}

func TestService_ReceivePayment_BankFee_Negative_ReturnsError(t *testing.T) {
	uid := uint64(5)
	svc, _, _ := buildService(accountsWithBankFee, map[uint64]*sale.UnitSaleInfo{
		5: defaultUnit(5, "reserved"),
	}, nil, nil, nil)
	neg, _ := domain.NewMoney("-100")
	_, err := svc.ReceivePayment(context.Background(), 1, sale.ReceivePaymentRequest{
		Source:          sale.PaymentSourceUnitTermin,
		UnitID:          &uid,
		BankAccountCode: "1-1300",
		Amount:          rupiah(100_000_000),
		BankFee:         neg,
		Date:            time.Now(),
	})
	if !errors.Is(err, sale.ErrBankFeeInvalid) {
		t.Errorf("expected ErrBankFeeInvalid, got %v", err)
	}
}

// ── Tests: RecordBAST — Event 3 Non-PKP ──────────────────────────────────────

func TestService_RecordBAST_NonPKP_Event3_Balanced(t *testing.T) {
	// Skenario dari posting-rules.md Contoh A:
	// SalePrice = 1,000,000,000; advance = 800,000,000; piutang = 200,000,000
	existing := []*sale.TerminPayment{
		{UnitID: 5, Amount: rupiah(800_000_000)},
	}
	svc, bast, _ := buildService(standardAccounts, map[uint64]*sale.UnitSaleInfo{5: defaultUnit(5, "reserved")}, existing,
		map[uint64]domain.UnitCostBreakdown{
			5: {Hard: rupiah(650_000_000)},
		}, nil)

	req := sale.RecordBASTRequest{
		UnitID: 5, SalePrice: rupiah(1_000_000_000), IsVAT: false, BuyerRef: "BUYER-01",
		BASTDate: time.Now(),
	}
	_, err := svc.RecordAkad(context.Background(), 1, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	assertBalanced(t, "Event3-NonPKP", bast.captured.RevenueLines)
	assertWholeRupiah(t, "Event3-NonPKP", bast.captured.RevenueLines)

	// Dr UMP = 800M
	drUMP := sumDebitByAccount(bast.captured.RevenueLines, accUMP)
	if !drUMP.Equal(rupiah(800_000_000)) {
		t.Errorf("Dr UMP=%s, want 800000000", drUMP)
	}
	// Dr Piutang = 200M
	drPiutang := sumDebitByAccount(bast.captured.RevenueLines, accPiutang)
	if !drPiutang.Equal(rupiah(200_000_000)) {
		t.Errorf("Dr Piutang=%s, want 200000000", drPiutang)
	}
	// Cr Pendapatan = 1,000M
	crPendapatan := sumCreditByAccount(bast.captured.RevenueLines, accPendapatan)
	if !crPendapatan.Equal(rupiah(1_000_000_000)) {
		t.Errorf("Cr Pendapatan=%s, want 1000000000", crPendapatan)
	}
	// PPN Keluaran = 0 (non-PKP)
	crPPN := sumCreditByAccount(bast.captured.RevenueLines, accPPNKeluar)
	if !crPPN.IsZero() {
		t.Errorf("Cr PPN should be 0 for non-PKP, got %s", crPPN)
	}
}

// ── Tests: RecordBAST — Event 3 PKP ──────────────────────────────────────────

func TestService_RecordBAST_PKP_Event3_Balanced_PiutangBruto(t *testing.T) {
	// Skenario dari posting-rules.md Contoh B:
	// DPP = 1,000,000,000; PPN 11% = 110,000,000; bruto = 1,110,000,000
	// advance = 800,000,000; Piutang bruto = 310,000,000
	existing := []*sale.TerminPayment{
		{UnitID: 5, Amount: rupiah(800_000_000)},
	}
	svc, bast, _ := buildService(standardAccounts, map[uint64]*sale.UnitSaleInfo{5: defaultUnit(5, "reserved")}, existing,
		map[uint64]domain.UnitCostBreakdown{
			5: {Hard: rupiah(650_000_000)},
		}, nil)

	req := sale.RecordBASTRequest{
		UnitID: 5, SalePrice: rupiah(1_000_000_000), IsVAT: true,
		VATRate: decimal.NewFromFloat(0.11), BuyerRef: "BUYER-01", BASTDate: time.Now(),
	}
	_, err := svc.RecordAkad(context.Background(), 1, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	assertBalanced(t, "Event3-PKP", bast.captured.RevenueLines)
	assertWholeRupiah(t, "Event3-PKP", bast.captured.RevenueLines)

	// Cr Pendapatan = DPP = 1,000M
	crPendapatan := sumCreditByAccount(bast.captured.RevenueLines, accPendapatan)
	if !crPendapatan.Equal(rupiah(1_000_000_000)) {
		t.Errorf("Cr Pendapatan=%s, want 1000000000", crPendapatan)
	}
	// Cr PPN = 110M
	crPPN := sumCreditByAccount(bast.captured.RevenueLines, accPPNKeluar)
	if !crPPN.Equal(rupiah(110_000_000)) {
		t.Errorf("Cr PPN=%s, want 110000000", crPPN)
	}
	// Dr UMP = 800M
	drUMP := sumDebitByAccount(bast.captured.RevenueLines, accUMP)
	if !drUMP.Equal(rupiah(800_000_000)) {
		t.Errorf("Dr UMP=%s, want 800000000", drUMP)
	}
	// Dr Piutang = BRUTO sisa = 310M
	drPiutang := sumDebitByAccount(bast.captured.RevenueLines, accPiutang)
	if !drPiutang.Equal(rupiah(310_000_000)) {
		t.Errorf("Dr Piutang=%s, want 310000000 (bruto)", drPiutang)
	}
}

// ── Tests: RecordBAST — Event 4 HPP ──────────────────────────────────────────

func TestService_RecordBAST_Event4_HPP_PerKategori_Balanced(t *testing.T) {
	// HPP = Land 100M + Hard 300M + Soft 50M + Financing 25M = 475M
	hpp := domain.UnitCostBreakdown{
		Land: rupiah(100_000_000), Hard: rupiah(300_000_000),
		Soft: rupiah(50_000_000), Financing: rupiah(25_000_000),
	}
	svc, bast, _ := buildService(standardAccounts, map[uint64]*sale.UnitSaleInfo{5: defaultUnit(5, "reserved")},
		nil, map[uint64]domain.UnitCostBreakdown{5: hpp}, nil)

	req := sale.RecordBASTRequest{
		UnitID: 5, SalePrice: rupiah(2_000_000_000), BASTDate: time.Now(),
	}
	_, err := svc.RecordAkad(context.Background(), 1, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	assertBalanced(t, "Event4-HPP", bast.captured.COGSLines)
	assertWholeRupiah(t, "Event4-HPP", bast.captured.COGSLines)

	// Dr HPP = total 475M
	drHPP := sumDebitByAccount(bast.captured.COGSLines, accHPP)
	if !drHPP.Equal(rupiah(475_000_000)) {
		t.Errorf("Dr HPP=%s, want 475000000", drHPP)
	}

	// Cr Land = 100M, Cr Hard = 300M, Cr Soft = 50M, Cr Financing = 25M
	cases := []struct {
		code uint64
		want domain.Money
	}{
		{accLand, rupiah(100_000_000)},
		{accHard, rupiah(300_000_000)},
		{accSoft, rupiah(50_000_000)},
		{accFinancing, rupiah(25_000_000)},
	}
	for _, c := range cases {
		got := sumCreditByAccount(bast.captured.COGSLines, c.code)
		if !got.Equal(c.want) {
			t.Errorf("accID=%d: Cr=%s, want %s", c.code, got, c.want)
		}
	}

	// Σ Cr Persediaan == Dr HPP (Invariant #4)
	sumCr := sumCreditByAccount(bast.captured.COGSLines, accLand).
		Add(sumCreditByAccount(bast.captured.COGSLines, accHard)).
		Add(sumCreditByAccount(bast.captured.COGSLines, accSoft)).
		Add(sumCreditByAccount(bast.captured.COGSLines, accFinancing))
	if !sumCr.Equal(drHPP) {
		t.Errorf("Σ Cr Persediaan=%s != Dr HPP=%s (Invariant #4 violated)", sumCr, drHPP)
	}
}

func TestService_RecordBAST_Event4_HPPSingleCategory_OnlyNonZeroLines(t *testing.T) {
	// Hanya hard cost, kategori lain nol
	hpp := domain.UnitCostBreakdown{Hard: rupiah(650_000_000)}
	svc, bast, _ := buildService(standardAccounts, map[uint64]*sale.UnitSaleInfo{5: defaultUnit(5, "reserved")},
		nil, map[uint64]domain.UnitCostBreakdown{5: hpp}, nil)

	req := sale.RecordBASTRequest{UnitID: 5, SalePrice: rupiah(2_000_000_000), BASTDate: time.Now()}
	_, err := svc.RecordAkad(context.Background(), 1, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Only 2 lines: Dr HPP + Cr Hard (no zero lines)
	if len(bast.captured.COGSLines) != 2 {
		t.Errorf("expected 2 COGS lines, got %d: %+v", len(bast.captured.COGSLines), bast.captured.COGSLines)
	}
	assertBalanced(t, "Event4-single-category", bast.captured.COGSLines)
}

func TestService_RecordBAST_Event4_ZeroHPP_NoCOGSLines(t *testing.T) {
	// Tidak ada biaya terakumulasi untuk unit ini → tidak ada jurnal Event 4
	svc, bast, _ := buildService(standardAccounts, map[uint64]*sale.UnitSaleInfo{5: defaultUnit(5, "reserved")},
		nil, map[uint64]domain.UnitCostBreakdown{5: {}}, nil)

	req := sale.RecordBASTRequest{UnitID: 5, SalePrice: rupiah(2_000_000_000), BASTDate: time.Now()}
	_, err := svc.RecordAkad(context.Background(), 1, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(bast.captured.COGSLines) != 0 {
		t.Errorf("HPP=0 should produce no COGS lines, got %d", len(bast.captured.COGSLines))
	}
}

// ── Tests: RecordBAST — Atomicity ────────────────────────────────────────────

func TestService_RecordBAST_BASTWriterFails_PropagatesError(t *testing.T) {
	sentinel := errors.New("db tx failed")
	bastWriter := &mockBASTWriter{err: sentinel}
	svc, _, _ := buildService(standardAccounts, map[uint64]*sale.UnitSaleInfo{5: defaultUnit(5, "reserved")},
		nil, map[uint64]domain.UnitCostBreakdown{5: {Hard: rupiah(100_000_000)}}, bastWriter)

	req := sale.RecordBASTRequest{UnitID: 5, SalePrice: rupiah(1_000_000_000), BASTDate: time.Now()}
	_, err := svc.RecordAkad(context.Background(), 1, req)
	if !errors.Is(err, sentinel) {
		t.Errorf("expected sentinel error, got %v", err)
	}
}

func TestService_RecordBAST_Event3And4_PassedTogether_ToAtomicWriter(t *testing.T) {
	// Verifikasi bahwa service memanggil BASTWriter sekali dengan KEDUA set lines.
	hpp := domain.UnitCostBreakdown{Land: rupiah(50_000_000), Hard: rupiah(100_000_000)}
	svc, bast, _ := buildService(standardAccounts, map[uint64]*sale.UnitSaleInfo{5: defaultUnit(5, "reserved")},
		nil, map[uint64]domain.UnitCostBreakdown{5: hpp}, nil)

	req := sale.RecordBASTRequest{UnitID: 5, SalePrice: rupiah(1_000_000_000), BASTDate: time.Now()}
	_, err := svc.RecordAkad(context.Background(), 1, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if bast.captured == nil {
		t.Fatal("BASTWriter not called")
	}
	// Event 3 lines harus ada
	if len(bast.captured.RevenueLines) == 0 {
		t.Error("RevenueLines (Event 3) kosong")
	}
	// Event 4 lines harus ada (HPP > 0)
	if len(bast.captured.COGSLines) == 0 {
		t.Error("COGSLines (Event 4) kosong padahal HPP > 0")
	}
	// Keduanya balanced
	assertBalanced(t, "Event3", bast.captured.RevenueLines)
	assertBalanced(t, "Event4", bast.captured.COGSLines)
}

// ── Tests: RecordBAST — business rules ───────────────────────────────────────

func TestService_RecordBAST_AlreadySold_ReturnsError(t *testing.T) {
	svc, _, _ := buildService(standardAccounts, map[uint64]*sale.UnitSaleInfo{5: defaultUnit(5, "sold")}, nil, nil, nil)
	req := sale.RecordBASTRequest{UnitID: 5, SalePrice: rupiah(1_000_000_000), BASTDate: time.Now()}
	_, err := svc.RecordAkad(context.Background(), 1, req)
	if !errors.Is(err, sale.ErrUnitAlreadySold) {
		t.Errorf("expected ErrUnitAlreadySold, got %v", err)
	}
}

func TestService_RecordBAST_SalePriceFractional_ReturnsError(t *testing.T) {
	svc, _, _ := buildService(standardAccounts, map[uint64]*sale.UnitSaleInfo{5: defaultUnit(5, "reserved")}, nil, nil, nil)
	m, _ := domain.NewMoney("1000000000.5")
	req := sale.RecordBASTRequest{UnitID: 5, SalePrice: m, BASTDate: time.Now()}
	_, err := svc.RecordAkad(context.Background(), 1, req)
	if !errors.Is(err, sale.ErrSalePriceFractional) {
		t.Errorf("expected ErrSalePriceFractional, got %v", err)
	}
}

func TestService_RecordBAST_SalePriceZero_ReturnsError(t *testing.T) {
	svc, _, _ := buildService(standardAccounts, map[uint64]*sale.UnitSaleInfo{5: defaultUnit(5, "reserved")}, nil, nil, nil)
	req := sale.RecordBASTRequest{UnitID: 5, SalePrice: domain.Zero, BASTDate: time.Now()}
	_, err := svc.RecordAkad(context.Background(), 1, req)
	if !errors.Is(err, sale.ErrSalePriceZeroOrNeg) {
		t.Errorf("expected ErrSalePriceZeroOrNeg, got %v", err)
	}
}

func TestService_RecordBAST_AdvanceExceedsPrice_NonPKP_ReturnsError(t *testing.T) {
	existing := []*sale.TerminPayment{{UnitID: 5, Amount: rupiah(1_500_000_000)}}
	svc, _, _ := buildService(standardAccounts, map[uint64]*sale.UnitSaleInfo{5: defaultUnit(5, "reserved")},
		existing, nil, nil)
	req := sale.RecordBASTRequest{UnitID: 5, SalePrice: rupiah(1_000_000_000), BASTDate: time.Now()}
	_, err := svc.RecordAkad(context.Background(), 1, req)
	if !errors.Is(err, sale.ErrAdvanceExceedsSalePrice) {
		t.Errorf("expected ErrAdvanceExceedsSalePrice, got %v", err)
	}
}

func TestService_RecordBAST_PKP_NoVATRate_ReturnsError(t *testing.T) {
	svc, _, _ := buildService(standardAccounts, map[uint64]*sale.UnitSaleInfo{5: defaultUnit(5, "reserved")}, nil, nil, nil)
	req := sale.RecordBASTRequest{
		UnitID: 5, SalePrice: rupiah(1_000_000_000), IsVAT: true,
		VATRate: decimal.Zero, BASTDate: time.Now(),
	}
	_, err := svc.RecordAkad(context.Background(), 1, req)
	if !errors.Is(err, sale.ErrVATRateRequired) {
		t.Errorf("expected ErrVATRateRequired, got %v", err)
	}
}

// ── Tests: cross-tenant isolation ────────────────────────────────────────────

func TestService_RecordTermin_CrossTenantUnit_ReturnsNotFound(t *testing.T) {
	// Unit 5 belongs to tenant 1, not tenant 2.
	svc, _, _ := buildService(standardAccounts, map[uint64]*sale.UnitSaleInfo{
		5: defaultUnit(5, "reserved"),
	}, nil, nil, nil)

	// Tenant 2 tries to record termin on unit 5 (belongs to tenant 1).
	// mockUnitReader checks by unitID only, but in real impl it checks tenant_id too.
	// Here we test that RecordTermin propagates ErrUnitNotFound when unit lookup fails.
	// We simulate cross-tenant by having no unit 99 registered.
	_, err := recvUnitTermin(svc, 2, 99, "1-1300", rupiah(100_000))
	if !errors.Is(err, sale.ErrUnitNotFound) {
		t.Errorf("expected ErrUnitNotFound for cross-tenant, got %v", err)
	}
}

// ── Tests: full cycle ─────────────────────────────────────────────────────────

func TestService_FullCycle_TerminThenBAST_UMPNolkan(t *testing.T) {
	// Siklus penuh: 2 termin → BAST (non-PKP)
	// Verify: total advance = 800M, piutang = 200M, pendapatan = 1,000M
	hpp := domain.UnitCostBreakdown{
		Land: rupiah(200_000_000), Hard: rupiah(500_000_000), Soft: rupiah(100_000_000),
	}
	svc, bast, ts := buildService(standardAccounts, map[uint64]*sale.UnitSaleInfo{
		5: defaultUnit(5, "reserved"),
	}, nil, map[uint64]domain.UnitCostBreakdown{5: hpp}, nil)

	// Termin 1: 500M
	if _, err := recvUnitTermin(svc, 1, 5, "1-1300", rupiah(500_000_000)); err != nil {
		t.Fatalf("termin 1: %v", err)
	}
	// Termin 2: 300M
	if _, err := recvUnitTermin(svc, 1, 5, "1-1400", rupiah(300_000_000)); err != nil {
		t.Fatalf("termin 2: %v", err)
	}

	// Total advance = 800M
	advance, _ := ts.SumTerminsByUnit(context.Background(), 1, 5)
	if !advance.Equal(rupiah(800_000_000)) {
		t.Errorf("advance=%s, want 800000000", advance)
	}

	// BAST
	if _, err := svc.RecordAkad(context.Background(), 1, sale.RecordBASTRequest{
		UnitID: 5, SalePrice: rupiah(1_000_000_000), BASTDate: time.Now(), BuyerRef: "BUYER-X",
	}); err != nil {
		t.Fatalf("BAST: %v", err)
	}

	// Dr UMP = 800M (semua advance dinolkan)
	drUMP := sumDebitByAccount(bast.captured.RevenueLines, accUMP)
	if !drUMP.Equal(rupiah(800_000_000)) {
		t.Errorf("Dr UMP=%s, want 800000000", drUMP)
	}

	// Dr Piutang = 200M (sisa)
	drPiutang := sumDebitByAccount(bast.captured.RevenueLines, accPiutang)
	if !drPiutang.Equal(rupiah(200_000_000)) {
		t.Errorf("Dr Piutang=%s, want 200000000", drPiutang)
	}

	// Event 3 balanced
	assertBalanced(t, "Event3-fullcycle", bast.captured.RevenueLines)

	// Event 4: HPP total = 800M, Σ Cr = 800M
	hppTotal := rupiah(200_000_000 + 500_000_000 + 100_000_000)
	drHPP := sumDebitByAccount(bast.captured.COGSLines, accHPP)
	if !drHPP.Equal(hppTotal) {
		t.Errorf("Dr HPP=%s, want %s", drHPP, hppTotal)
	}
	assertBalanced(t, "Event4-fullcycle", bast.captured.COGSLines)
}

func TestService_RecordBAST_FullyPaid_ZeroPiutang(t *testing.T) {
	// Advance = SalePrice → Piutang = 0 (tidak ada baris Piutang)
	existing := []*sale.TerminPayment{{UnitID: 5, Amount: rupiah(1_000_000_000)}}
	svc, bast, _ := buildService(standardAccounts, map[uint64]*sale.UnitSaleInfo{5: defaultUnit(5, "reserved")},
		existing, map[uint64]domain.UnitCostBreakdown{5: {Hard: rupiah(600_000_000)}}, nil)

	req := sale.RecordBASTRequest{UnitID: 5, SalePrice: rupiah(1_000_000_000), BASTDate: time.Now()}
	_, err := svc.RecordAkad(context.Background(), 1, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Tidak ada Piutang line
	drPiutang := sumDebitByAccount(bast.captured.RevenueLines, accPiutang)
	if !drPiutang.IsZero() {
		t.Errorf("fully paid: piutang should be 0, got %s", drPiutang)
	}

	// Tetap balanced: Dr UMP = 1,000M = Cr Pendapatan
	assertBalanced(t, "fully-paid", bast.captured.RevenueLines)
}

func TestService_RecordBAST_NoPriorTermin_FullPiutang(t *testing.T) {
	// Tidak ada advance → seluruh harga jadi piutang
	svc, bast, _ := buildService(standardAccounts, map[uint64]*sale.UnitSaleInfo{5: defaultUnit(5, "reserved")},
		nil, map[uint64]domain.UnitCostBreakdown{5: {Hard: rupiah(500_000_000)}}, nil)

	req := sale.RecordBASTRequest{UnitID: 5, SalePrice: rupiah(2_000_000_000), BASTDate: time.Now()}
	_, err := svc.RecordAkad(context.Background(), 1, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	drPiutang := sumDebitByAccount(bast.captured.RevenueLines, accPiutang)
	if !drPiutang.Equal(rupiah(2_000_000_000)) {
		t.Errorf("piutang=%s, want 2000000000", drPiutang)
	}

	// Tidak ada Dr UMP (advance = 0)
	drUMP := sumDebitByAccount(bast.captured.RevenueLines, accUMP)
	if !drUMP.IsZero() {
		t.Errorf("Dr UMP should be 0, got %s", drUMP)
	}
	assertBalanced(t, "no-termin", bast.captured.RevenueLines)
}

// ── Tests: HPP tagged to unit and project ─────────────────────────────────────

func TestService_RecordBAST_COGSLines_TaggedWithUnitAndProject(t *testing.T) {
	hpp := domain.UnitCostBreakdown{Hard: rupiah(300_000_000)}
	unit := &sale.UnitSaleInfo{ID: 5, ProjectID: 42, PhaseID: ptr(uint64(3)), Status: "reserved"}
	svc, bast, _ := buildService(standardAccounts, map[uint64]*sale.UnitSaleInfo{5: unit},
		nil, map[uint64]domain.UnitCostBreakdown{5: hpp}, nil)

	req := sale.RecordBASTRequest{UnitID: 5, SalePrice: rupiah(1_000_000_000), BASTDate: time.Now()}
	_, err := svc.RecordAkad(context.Background(), 1, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for i, l := range bast.captured.COGSLines {
		if l.UnitID == nil || *l.UnitID != 5 {
			t.Errorf("COGSLines[%d]: unit_id should be 5, got %v", i, l.UnitID)
		}
		if l.ProjectID == nil || *l.ProjectID != 42 {
			t.Errorf("COGSLines[%d]: project_id should be 42, got %v", i, l.ProjectID)
		}
	}
}

func TestService_RecordBAST_RevenueLines_TaggedWithUnitAndProject(t *testing.T) {
	hpp := domain.UnitCostBreakdown{Hard: rupiah(300_000_000)}
	unit := &sale.UnitSaleInfo{ID: 5, ProjectID: 42, PhaseID: ptr(uint64(3)), Status: "reserved"}
	svc, bast, _ := buildService(standardAccounts, map[uint64]*sale.UnitSaleInfo{5: unit},
		nil, map[uint64]domain.UnitCostBreakdown{5: hpp}, nil)

	req := sale.RecordBASTRequest{UnitID: 5, SalePrice: rupiah(1_000_000_000), BASTDate: time.Now()}
	_, err := svc.RecordAkad(context.Background(), 1, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for i, l := range bast.captured.RevenueLines {
		if l.UnitID == nil || *l.UnitID != 5 {
			t.Errorf("RevenueLines[%d]: unit_id should be 5, got %v", i, l.UnitID)
		}
		if l.ProjectID == nil || *l.ProjectID != 42 {
			t.Errorf("RevenueLines[%d]: project_id should be 42, got %v", i, l.ProjectID)
		}
	}
}

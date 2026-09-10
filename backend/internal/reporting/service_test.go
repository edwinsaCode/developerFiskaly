package reporting_test

import (
	"context"
	"testing"
	"time"

	"esaproperti/internal/domain"
	"esaproperti/internal/ledger"
	"esaproperti/internal/reporting"
)

var asOf = time.Date(2024, 12, 31, 0, 0, 0, 0, time.UTC)

// ── helpers ───────────────────────────────────────────────────────────────────

func mustMoney(s string) domain.Money {
	m, err := domain.NewMoney(s)
	if err != nil {
		panic("mustMoney: " + s + ": " + err.Error())
	}
	return m
}

func tbRow(code, name string, accType domain.AccountType, debit, credit domain.Money) ledger.TrialBalanceRow {
	var net domain.Money
	if domain.NormalBalanceFor(accType) == domain.NormalBalanceDebit {
		net = debit.Sub(credit)
	} else {
		net = credit.Sub(debit)
	}
	return ledger.TrialBalanceRow{
		AccountCode: code,
		AccountName: name,
		AccountType: accType,
		TotalDebit:  debit,
		TotalCredit: credit,
		Balance:     net,
	}
}

// TestComputeNeracaRange_PeriodeTerpilihIsInfoOnly_NeracaStaysBalanced (Item 5,
// dikoreksi setelah live FE verification menemukan bug "Neraca tidak seimbang"
// palsu): ComputeNeracaRange TIDAK PERNAH mengganti LabaRugiTahunBerjalan/
// TotalEkuitas/IsBalanced dengan angka jendela [from, asOf] — itu selalu
// life-to-date dari `rows`, persis seperti ComputeNeraca biasa, sehingga Neraca
// TETAP balanced. LabaRugiPeriodeTerpilih (dari plRows) hanya field informasi
// tambahan.
func TestComputeNeracaRange_PeriodeTerpilihIsInfoOnly_NeracaStaysBalanced(t *testing.T) {
	from := time.Date(2024, 12, 1, 0, 0, 0, 0, time.UTC)
	rows := []ledger.TrialBalanceRow{
		// Skenario balanced end-to-end: Aset 830M == Kewajiban 0 + Ekuitas 500M + LabaRugi(life-to-date) 330M
		tbRow("1-1300", "Bank — BCA", domain.AccountAsset, domain.FromInt(830_000_000), domain.Zero),
		tbRow("3-1000", "Modal Disetor", domain.AccountEquity, domain.Zero, domain.FromInt(500_000_000)),
		tbRow("4-1000", "Pendapatan", domain.AccountRevenue, domain.Zero, domain.FromInt(500_000_000)),
		tbRow("5-3000", "Beban", domain.AccountExpense, domain.FromInt(170_000_000), domain.Zero),
	}
	// plRows: hanya transaksi jendela [from, asOf] — jauh lebih kecil dari life-to-date.
	plRows := []reporting.PLRawRow{
		{AccountCode: "4-1000", AccountName: "Pendapatan", TotalCredit: domain.FromInt(50_000_000)},
		{AccountCode: "5-3000", AccountName: "Beban", TotalDebit: domain.FromInt(20_000_000)},
	}

	neraca := reporting.ComputeNeracaRange(rows, plRows, from, asOf)

	if neraca.TotalAset != "830000000" {
		t.Errorf("TotalAset (harus tetap kumulatif): got %s, want 830000000", neraca.TotalAset)
	}
	if neraca.LabaRugiTahunBerjalan != "330000000" {
		t.Errorf("LabaRugiTahunBerjalan (life-to-date, TIDAK boleh berubah oleh from): got %s, want 330000000", neraca.LabaRugiTahunBerjalan)
	}
	if neraca.LabaRugiPeriodeTerpilih != "30000000" {
		t.Errorf("LabaRugiPeriodeTerpilih (dari jendela plRows): got %s, want 30000000", neraca.LabaRugiPeriodeTerpilih)
	}
	if neraca.From == nil || !neraca.From.Equal(from) {
		t.Errorf("From harus tercatat pada report: got %v", neraca.From)
	}
	if neraca.TotalKewajibanEkuitas != "830000000" {
		t.Errorf("TotalKewajibanEkuitas: got %s, want 830000000 (0+500M+330M)", neraca.TotalKewajibanEkuitas)
	}
	if !neraca.IsBalanced {
		t.Errorf("IsBalanced harus tetap true — from tidak boleh merusak identitas neraca")
	}
}

// ── TestComputeNeraca_IsBalanced ──────────────────────────────────────────────

func TestComputeNeraca_IsBalanced(t *testing.T) {
	// Dr 1-3100 Persediaan Hard Cost 1.000.000
	// Cr 2-1000 Hutang Usaha          600.000
	// Cr 3-1000 Modal Disetor          400.000
	rows := []ledger.TrialBalanceRow{
		tbRow("1-3100", "Persediaan Hard Cost", domain.AccountAsset, domain.FromInt(1_000_000), domain.Zero),
		tbRow("2-1000", "Hutang Usaha", domain.AccountLiability, domain.Zero, domain.FromInt(600_000)),
		tbRow("3-1000", "Modal Disetor", domain.AccountEquity, domain.Zero, domain.FromInt(400_000)),
	}

	neraca := reporting.ComputeNeraca(rows, asOf)

	if !neraca.IsBalanced {
		t.Fatalf("neraca harus balanced: TotalAset=%s TotalKE=%s", neraca.TotalAset, neraca.TotalKewajibanEkuitas)
	}
	if neraca.TotalAset != "1000000" {
		t.Errorf("TotalAset: got %s, want 1000000", neraca.TotalAset)
	}
	if neraca.TotalKewajiban != "600000" {
		t.Errorf("TotalKewajiban: got %s, want 600000", neraca.TotalKewajiban)
	}
	if neraca.TotalEkuitas != "400000" {
		t.Errorf("TotalEkuitas: got %s, want 400000", neraca.TotalEkuitas)
	}
	if neraca.LabaRugiTahunBerjalan != "0" {
		t.Errorf("LabaRugiTahunBerjalan: got %s, want 0", neraca.LabaRugiTahunBerjalan)
	}
}

// TestComputeNeraca_ContraAset_1_4900_ReducesTotalAset memverifikasi bahwa
// akumulasi penyusutan (1-4900, normal balance kredit) mengurangi TotalAset,
// bukan menambah.
func TestComputeNeraca_ContraAset_1_4900_ReducesTotalAset(t *testing.T) {
	// Dr 1-4000 Aset Tetap Peralatan  500.000.000
	// Cr 2-1000 Hutang Usaha          500.000.000
	// Dr 5-4500 Beban Penyusutan       50.000.000
	// Cr 1-4900 Akumulasi Penyusutan   50.000.000
	rows := []ledger.TrialBalanceRow{
		tbRow("1-4000", "Aset Tetap — Peralatan Kantor", domain.AccountAsset,
			domain.FromInt(500_000_000), domain.Zero),
		tbRow("1-4900", "Akumulasi Penyusutan", domain.AccountAsset,
			domain.Zero, domain.FromInt(50_000_000)), // NormalBalance=Credit
		tbRow("2-1000", "Hutang Usaha", domain.AccountLiability,
			domain.Zero, domain.FromInt(500_000_000)),
		tbRow("5-4500", "Beban Penyusutan", domain.AccountExpense,
			domain.FromInt(50_000_000), domain.Zero),
	}

	neraca := reporting.ComputeNeraca(rows, asOf)

	// TotalAset = 500M - 50M = 450M (kontra-aset mengurangi)
	if neraca.TotalAset != "450000000" {
		t.Errorf("TotalAset: got %s, want 450000000 (1-4900 harus mengurangi)", neraca.TotalAset)
	}
	// LabaRugi = 0 - 50M = -50M (rugi karena penyusutan)
	if neraca.LabaRugiTahunBerjalan != "-50000000" {
		t.Errorf("LabaRugiTahunBerjalan: got %s, want -50000000", neraca.LabaRugiTahunBerjalan)
	}
	// Balanced: 450M == 500M + 0 + (-50M) = 450M
	if !neraca.IsBalanced {
		t.Fatalf("neraca harus balanced: TotalAset=%s TotalKE=%s", neraca.TotalAset, neraca.TotalKewajibanEkuitas)
	}
}

// TestComputeNeraca_UangMukaPenjualan_IsKewajiban_BukanPendapatan memverifikasi
// Invariant #7: Uang Muka Penjualan (2-2000) adalah kewajiban, bukan pendapatan.
func TestComputeNeraca_UangMukaPenjualan_IsKewajiban_BukanPendapatan(t *testing.T) {
	// Event 2 (termin): Dr 1-1300 Bank BCA 100M / Cr 2-2000 UMP 100M
	rows := []ledger.TrialBalanceRow{
		tbRow("1-1300", "Bank — BCA", domain.AccountAsset,
			domain.FromInt(100_000_000), domain.Zero),
		tbRow("2-2000", "Uang Muka Penjualan", domain.AccountLiability,
			domain.Zero, domain.FromInt(100_000_000)),
	}

	neraca := reporting.ComputeNeraca(rows, asOf)

	// 2-2000 harus di Kewajiban (bukan pendapatan)
	if len(neraca.Kewajiban) != 1 {
		t.Fatalf("harus ada 1 baris kewajiban (UMP), got %d", len(neraca.Kewajiban))
	}
	if neraca.Kewajiban[0].Code != "2-2000" {
		t.Errorf("kewajiban[0].Code: got %s, want 2-2000", neraca.Kewajiban[0].Code)
	}
	// LabaRugi harus 0 (tidak masuk pendapatan)
	if neraca.LabaRugiTahunBerjalan != "0" {
		t.Errorf("LabaRugiTahunBerjalan harus 0 (UMP bukan pendapatan): got %s", neraca.LabaRugiTahunBerjalan)
	}
	if !neraca.IsBalanced {
		t.Fatal("neraca harus balanced")
	}
}

// TestComputeNeraca_WithLabaRugi_BalancedViaEquitySection memverifikasi bahwa
// LabaRugiTahunBerjalan (dari akun 4-xxxx dan 5-xxxx) membuat persamaan neraca tetap balanced.
//
// Skenario yang benar (Σ D = Σ C):
// Entry 1: Dr 1-1300 Bank 500M / Cr 4-1000 Pendapatan 500M (penjualan tunai)
// Entry 2: Dr 5-3000 Beban 200M / Cr 1-1300 Bank 200M (bayar beban operasi)
func TestComputeNeraca_WithLabaRugi_BalancedViaEquitySection(t *testing.T) {
	rows := []ledger.TrialBalanceRow{
		// 1-1300: D=500M, C=200M → net = 300M
		tbRow("1-1300", "Bank — BCA", domain.AccountAsset,
			domain.FromInt(500_000_000), domain.FromInt(200_000_000)),
		tbRow("4-1000", "Pendapatan Penjualan Unit", domain.AccountRevenue,
			domain.Zero, domain.FromInt(500_000_000)),
		tbRow("5-3000", "Beban Pemasaran", domain.AccountExpense,
			domain.FromInt(200_000_000), domain.Zero),
	}

	neraca := reporting.ComputeNeraca(rows, asOf)

	// TotalAset = 300M (500M - 200M)
	if neraca.TotalAset != "300000000" {
		t.Errorf("TotalAset: got %s, want 300000000", neraca.TotalAset)
	}
	if neraca.TotalKewajiban != "0" {
		t.Errorf("TotalKewajiban: got %s, want 0", neraca.TotalKewajiban)
	}
	// LabaRugi = 500M - 200M = 300M
	if neraca.LabaRugiTahunBerjalan != "300000000" {
		t.Errorf("LabaRugiTahunBerjalan: got %s, want 300000000", neraca.LabaRugiTahunBerjalan)
	}
	// Balanced: 300M == 0 + 0 + 300M ✓
	if !neraca.IsBalanced {
		t.Fatalf("neraca harus balanced: TotalAset=%s TotalKE=%s", neraca.TotalAset, neraca.TotalKewajibanEkuitas)
	}
}

// ── TestComputePL ─────────────────────────────────────────────────────────────

func TestComputePL_PerhitunganDasarPendapatanDanBeban(t *testing.T) {
	rows := []reporting.PLRawRow{
		{AccountCode: "4-1000", AccountName: "Pendapatan Penjualan", TotalCredit: domain.FromInt(500_000_000)},
		{AccountCode: "5-1000", AccountName: "HPP", TotalDebit: domain.FromInt(300_000_000)},
		{AccountCode: "5-3000", AccountName: "Beban Pemasaran", TotalDebit: domain.FromInt(50_000_000)},
	}

	pid := uint64(42)
	report := reporting.ComputePL(rows, &pid, asOf)

	if report.TotalPendapatan != "500000000" {
		t.Errorf("TotalPendapatan: got %s, want 500000000", report.TotalPendapatan)
	}
	if report.TotalHPP != "300000000" {
		t.Errorf("TotalHPP: got %s, want 300000000", report.TotalHPP)
	}
	if report.TotalBebanOperasional != "50000000" {
		t.Errorf("TotalBebanOperasional: got %s, want 50000000", report.TotalBebanOperasional)
	}
	if report.LabaRugiBersih != "150000000" {
		t.Errorf("LabaRugiBersih: got %s, want 150000000", report.LabaRugiBersih)
	}
	if len(report.Pendapatan) != 1 {
		t.Errorf("Pendapatan lines: got %d, want 1", len(report.Pendapatan))
	}
	if len(report.HPP) != 1 {
		t.Errorf("HPP lines: got %d, want 1", len(report.HPP))
	}
	if len(report.BebanOperasional) != 1 {
		t.Errorf("BebanOperasional lines: got %d, want 1", len(report.BebanOperasional))
	}
	if report.ProjectID == nil || *report.ProjectID != 42 {
		t.Errorf("ProjectID: got %v, want &42", report.ProjectID)
	}
}

// TestComputePL_Struktur8Bagian (item B, 2026-08-27): verifikasi klasifikasi
// P&L 8-bagian penuh — pendapatan inti vs luar usaha (4-2000), HPP (5-1000),
// beban operasional, beban luar usaha (5-5000 bunga), dan beban pajak (5-2000
// PPh Final) diklasifikasi via ledger.AccountRole registry, bukan flat 4-x/5-x.
func TestComputePL_Struktur8Bagian(t *testing.T) {
	rows := []reporting.PLRawRow{
		{AccountCode: "4-1000", AccountName: "Pendapatan Penjualan Unit", TotalCredit: domain.FromInt(1_000_000_000)},
		{AccountCode: "4-2100", AccountName: "Pendapatan Booking", TotalCredit: domain.FromInt(50_000_000)},
		{AccountCode: "4-2000", AccountName: "Pendapatan Lain-lain", TotalCredit: domain.FromInt(10_000_000)},
		{AccountCode: "5-1000", AccountName: "HPP", TotalDebit: domain.FromInt(600_000_000)},
		{AccountCode: "5-3000", AccountName: "Beban Pemasaran", TotalDebit: domain.FromInt(30_000_000)},
		{AccountCode: "5-4000", AccountName: "Beban Umum & Administrasi", TotalDebit: domain.FromInt(20_000_000)},
		{AccountCode: "5-5000", AccountName: "Beban Bunga", TotalDebit: domain.FromInt(5_000_000)},
		{AccountCode: "5-2000", AccountName: "Beban PPh Final Pengalihan", TotalDebit: domain.FromInt(25_000_000)},
	}

	pl := reporting.ComputePL(rows, nil, asOf)

	// Pendapatan inti = 1.000M + 50M (booking) = 1.050M — 4-2000 TIDAK ikut.
	if pl.TotalPendapatan != "1050000000" {
		t.Errorf("TotalPendapatan: got %s, want 1050000000 (4-2000 harus di luar usaha)", pl.TotalPendapatan)
	}
	if pl.TotalHPP != "600000000" {
		t.Errorf("TotalHPP: got %s, want 600000000", pl.TotalHPP)
	}
	// Laba Kotor = 1.050M - 600M = 450M
	if pl.LabaKotor != "450000000" {
		t.Errorf("LabaKotor: got %s, want 450000000", pl.LabaKotor)
	}
	// Beban operasional = 30M + 20M = 50M (5-5000 dan 5-2000 TIDAK ikut)
	if pl.TotalBebanOperasional != "50000000" {
		t.Errorf("TotalBebanOperasional: got %s, want 50000000", pl.TotalBebanOperasional)
	}
	// Laba Operasional = 450M - 50M = 400M
	if pl.LabaOperasional != "400000000" {
		t.Errorf("LabaOperasional: got %s, want 400000000", pl.LabaOperasional)
	}
	if pl.TotalPendapatanLuarUsaha != "10000000" {
		t.Errorf("TotalPendapatanLuarUsaha: got %s, want 10000000", pl.TotalPendapatanLuarUsaha)
	}
	if pl.TotalBebanLuarUsaha != "5000000" {
		t.Errorf("TotalBebanLuarUsaha: got %s, want 5000000", pl.TotalBebanLuarUsaha)
	}
	// Laba Sebelum Pajak = 400M + 10M - 5M = 405M
	if pl.LabaBersihSebelumPajak != "405000000" {
		t.Errorf("LabaBersihSebelumPajak: got %s, want 405000000", pl.LabaBersihSebelumPajak)
	}
	if pl.TotalBebanPajak != "25000000" {
		t.Errorf("TotalBebanPajak: got %s, want 25000000", pl.TotalBebanPajak)
	}
	// Laba Bersih Setelah Pajak = 405M - 25M = 380M
	if pl.LabaRugiBersih != "380000000" {
		t.Errorf("LabaRugiBersih: got %s, want 380000000", pl.LabaRugiBersih)
	}
}

// TestLabaRugi_KonsolidasiEqualsPerProyekPlusKorporat adalah REQUIRED TEST.
// Memverifikasi: Σ(laba per proyek) + laba korporat == laba konsolidasi.
func TestLabaRugi_KonsolidasiEqualsPerProyekPlusKorporat(t *testing.T) {
	pid1, pid2 := uint64(1), uint64(2)

	p1Rows := []reporting.PLRawRow{
		{AccountCode: "4-1000", AccountName: "Pendapatan", TotalCredit: domain.FromInt(500_000_000)},
		{AccountCode: "5-1000", AccountName: "HPP", TotalDebit: domain.FromInt(300_000_000)},
	}
	p2Rows := []reporting.PLRawRow{
		{AccountCode: "4-1000", AccountName: "Pendapatan", TotalCredit: domain.FromInt(200_000_000)},
		{AccountCode: "5-3000", AccountName: "Beban Pemasaran", TotalDebit: domain.FromInt(50_000_000)},
	}
	corpRows := []reporting.PLRawRow{
		{AccountCode: "5-4000", AccountName: "Beban GA", TotalDebit: domain.FromInt(100_000_000)},
	}

	pl1 := reporting.ComputePL(p1Rows, &pid1, asOf)
	pl2 := reporting.ComputePL(p2Rows, &pid2, asOf)
	plCorp := reporting.ComputePL(corpRows, nil, asOf)

	// Σ laba per proyek + korporat
	labaP1 := mustMoney(pl1.LabaRugiBersih)   // 500M - 300M = 200M
	labaP2 := mustMoney(pl2.LabaRugiBersih)   // 200M - 50M = 150M
	labaCorp := mustMoney(plCorp.LabaRugiBersih) // 0 - 100M = -100M

	totalPerProyek := labaP1.Add(labaP2).Add(labaCorp) // 200M + 150M - 100M = 250M

	// Konsolidasi: query DB akan menghasilkan Σ per account_code lintas proyek
	// (simulasi: 4-1000 total = 500M+200M=700M, 5-1000=300M, 5-3000=50M, 5-4000=100M)
	konsRows := []reporting.PLRawRow{
		{AccountCode: "4-1000", AccountName: "Pendapatan", TotalCredit: domain.FromInt(700_000_000)},
		{AccountCode: "5-1000", AccountName: "HPP", TotalDebit: domain.FromInt(300_000_000)},
		{AccountCode: "5-3000", AccountName: "Beban Pemasaran", TotalDebit: domain.FromInt(50_000_000)},
		{AccountCode: "5-4000", AccountName: "Beban GA", TotalDebit: domain.FromInt(100_000_000)},
	}
	plKons := reporting.ComputePL(konsRows, nil, asOf)
	labaKons := mustMoney(plKons.LabaRugiBersih) // 700M - 450M = 250M

	if !totalPerProyek.Equal(labaKons) {
		t.Errorf("Σ(per proyek) + korporat != konsolidasi: got %s, want %s",
			totalPerProyek.String(), labaKons.String())
	}
	if labaKons.String() != "250000000" {
		t.Errorf("LabaKonsolidasi: got %s, want 250000000", labaKons.String())
	}
}

// ── TestGetNeraca (service dengan mock) ──────────────────────────────────────

type mockLedgerQuerier struct {
	tb  *ledger.TrialBalance
	err error
}

func (m *mockLedgerQuerier) TrialBalance(_ context.Context, _ uint64, _ time.Time) (*ledger.TrialBalance, error) {
	return m.tb, m.err
}
func (m *mockLedgerQuerier) GeneralLedger(_ context.Context, _ uint64, _ ledger.LedgerFilter) ([]ledger.LedgerEntry, error) {
	return nil, nil
}
func (m *mockLedgerQuerier) ListAccounts(_ context.Context, _ uint64) ([]*ledger.Account, error) {
	return nil, nil
}

type mockPLReader struct{}

func (m *mockPLReader) GetProjectPLRows(_ context.Context, _, _ uint64, _ *time.Time, _ time.Time) ([]reporting.PLRawRow, error) {
	return nil, nil
}
func (m *mockPLReader) GetConsolidatedPLRows(_ context.Context, _ uint64, _ *time.Time, _ time.Time) ([]reporting.PLRawRow, error) {
	return nil, nil
}
func (m *mockPLReader) GetTaxLiabilityReport(_ context.Context, _ uint64, _, _ time.Time) (*reporting.TaxLiabilityReport, error) {
	return &reporting.TaxLiabilityReport{}, nil
}

type mockPipelineReader struct {
	stats *reporting.PipelineStats
	err   error
}

func (m *mockPipelineReader) GetPipelineStats(_ context.Context, _ uint64) (*reporting.PipelineStats, error) {
	return m.stats, m.err
}

type mockCashFlowReader struct {
	movements []reporting.CashMovement
	err       error
}

func (m *mockCashFlowReader) GetCashMovements(_ context.Context, _ uint64, _, _ time.Time) ([]reporting.CashMovement, error) {
	return m.movements, m.err
}

func TestGetNeraca_DelegatesToLedgerAndComputesCorrectly(t *testing.T) {
	rows := []ledger.TrialBalanceRow{
		tbRow("1-3100", "Persediaan Hard Cost", domain.AccountAsset,
			domain.FromInt(800_000_000), domain.Zero),
		tbRow("2-1000", "Hutang Usaha", domain.AccountLiability,
			domain.Zero, domain.FromInt(500_000_000)),
		tbRow("3-1000", "Modal Disetor", domain.AccountEquity,
			domain.Zero, domain.FromInt(300_000_000)),
	}
	tb := &ledger.TrialBalance{AsOf: asOf, Rows: rows}

	svc := reporting.NewService(
		&mockLedgerQuerier{tb: tb},
		&mockPLReader{},
		&mockPipelineReader{stats: &reporting.PipelineStats{}},
		&mockCashFlowReader{},
	)

	neraca, err := svc.GetNeraca(context.Background(), 1, nil, asOf)
	if err != nil {
		t.Fatalf("GetNeraca: %v", err)
	}
	if !neraca.IsBalanced {
		t.Fatal("neraca harus balanced")
	}
	if neraca.TotalAset != "800000000" {
		t.Errorf("TotalAset: got %s, want 800000000", neraca.TotalAset)
	}
}

// ── TestGetSalesPipeline ──────────────────────────────────────────────────────

func TestGetSalesPipeline_CountsAndValues(t *testing.T) {
	stats := &reporting.PipelineStats{
		AvailableCount: 3,
		AvailableSum:   domain.FromInt(3_000_000_000),
		ReservedCount:  2,
		ReservedSum:    domain.FromInt(2_000_000_000),
		TotalAdvance:   domain.FromInt(200_000_000),
		SoldCount:      1,
		SoldContractSum: domain.FromInt(1_500_000_000),
	}

	svc := reporting.NewService(
		&mockLedgerQuerier{},
		&mockPLReader{},
		&mockPipelineReader{stats: stats},
		&mockCashFlowReader{},
	)

	rpt, err := svc.GetSalesPipeline(context.Background(), 1)
	if err != nil {
		t.Fatalf("GetSalesPipeline: %v", err)
	}

	if rpt.Available.Count != 3 {
		t.Errorf("Available.Count: got %d, want 3", rpt.Available.Count)
	}
	if rpt.Reserved.Count != 2 {
		t.Errorf("Reserved.Count: got %d, want 2", rpt.Reserved.Count)
	}
	if rpt.Sold.Count != 1 {
		t.Errorf("Sold.Count: got %d, want 1", rpt.Sold.Count)
	}
	if rpt.TotalUnits != 6 {
		t.Errorf("TotalUnits: got %d, want 6", rpt.TotalUnits)
	}
	if rpt.Reserved.TotalAdvance != "200000000" {
		t.Errorf("Reserved.TotalAdvance: got %s, want 200000000", rpt.Reserved.TotalAdvance)
	}
	if rpt.Sold.TotalContractValue != "1500000000" {
		t.Errorf("Sold.TotalContractValue: got %s, want 1500000000", rpt.Sold.TotalContractValue)
	}
	// ProjectedRevenue = available + reserved list price
	if rpt.ProjectedRevenue != "5000000000" {
		t.Errorf("ProjectedRevenue: got %s, want 5000000000", rpt.ProjectedRevenue)
	}
}

// ── TestGetArusKas ────────────────────────────────────────────────────────────

func TestGetArusKas_Classification_OperasiInvestasiPendanaan(t *testing.T) {
	from := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2024, 12, 31, 0, 0, 0, 0, time.UTC)

	movements := []reporting.CashMovement{
		{EntryID: 1, Description: "Penerimaan termin buyer", NetBankChange: domain.FromInt(100_000_000)},   // operasi
		{EntryID: 2, Description: "Bayar biaya konstruksi", NetBankChange: domain.FromInt(-80_000_000)},    // operasi
		{EntryID: 3, Description: "Beli peralatan kantor", NetBankChange: domain.FromInt(-20_000_000), IsInvesting: true},   // investasi
		{EntryID: 4, Description: "Terima pinjaman bank", NetBankChange: domain.FromInt(200_000_000), IsPendanaan: true},   // pendanaan
	}

	svc := reporting.NewService(
		&mockLedgerQuerier{},
		&mockPLReader{},
		&mockPipelineReader{stats: &reporting.PipelineStats{}},
		&mockCashFlowReader{movements: movements},
	)

	rpt, err := svc.GetArusKas(context.Background(), 1, from, to)
	if err != nil {
		t.Fatalf("GetArusKas: %v", err)
	}

	if len(rpt.Operasi.Lines) != 2 {
		t.Errorf("Operasi.Lines: got %d, want 2", len(rpt.Operasi.Lines))
	}
	if len(rpt.Investasi.Lines) != 1 {
		t.Errorf("Investasi.Lines: got %d, want 1", len(rpt.Investasi.Lines))
	}
	if len(rpt.Pendanaan.Lines) != 1 {
		t.Errorf("Pendanaan.Lines: got %d, want 1", len(rpt.Pendanaan.Lines))
	}
	if rpt.Operasi.Net != "20000000" {
		t.Errorf("Operasi.Net: got %s, want 20000000 (100M-80M)", rpt.Operasi.Net)
	}
	if rpt.Investasi.Net != "-20000000" {
		t.Errorf("Investasi.Net: got %s, want -20000000", rpt.Investasi.Net)
	}
	if rpt.Pendanaan.Net != "200000000" {
		t.Errorf("Pendanaan.Net: got %s, want 200000000", rpt.Pendanaan.Net)
	}
	// NetChange = 20M - 20M + 200M = 200M
	if rpt.NetChange != "200000000" {
		t.Errorf("NetChange: got %s, want 200000000", rpt.NetChange)
	}
}

// TestComputeNeraca_Persediaan_EqualsBiayaUnitBelumBAST memverifikasi bahwa
// saldo bersih akun persediaan (1-3100 + 1-3200) di Neraca == Σ biaya akumulasi
// unit yang BELUM BAST, dan menjadi nol setelah SEMUA unit BAST.
//
// Skenario — 2 unit (X: 150sqm, Y: 250sqm):
//
//	Hard Cost langsung (unit_id set): X=300M, Y=500M → 1-3100
//	Soft Cost project-wide (unit_id NULL, alokasi 150/400 dan 250/400): 400M total → 1-3200
//	  Alokasi: X mendapat 150M, Y mendapat 250M (100% teralokasi, Invariant #3)
//	Per-unit accumulated cost: X=450M, Y=750M
//	Skenario A: hanya Unit X yang BAST (terjual 600M)
//	Skenario B: kedua unit BAST
func TestComputeNeraca_Persediaan_EqualsBiayaUnitBelumBAST(t *testing.T) {
	// ── helper: bangun trial balance untuk skenario A (hanya Unit X BAST) ────
	//
	// Jurnal yang sudah diposting sebelum trial balance ini:
	//   J1: Dr 1-3100 HC 800M / Cr 2-1000 Hutang 800M  (kapitalisasi HC kedua unit)
	//   J2: Dr 1-3200 SC 400M / Cr 2-1000 Hutang 400M  (kapitalisasi SC project-wide)
	//   J3: Dr 1-1300 Bank 600M / Cr 4-1000 Pendapatan 600M  (penjualan Unit X)
	//   J4: Dr 5-1000 HPP 450M / Cr 1-3100 300M + Cr 1-3200 150M  (BAST Unit X)
	//
	// Σ D = 800M+400M+600M+450M = 2250M  |  Σ C = 800M+400M+600M+300M+150M = 2250M ✓
	scenarioA := []ledger.TrialBalanceRow{
		tbRow("1-1300", "Bank — BCA", domain.AccountAsset,
			domain.FromInt(600_000_000), domain.Zero),
		// HC: D=800M (kedua unit), C=300M (BAST Unit X) → net=500M (Unit Y saja)
		tbRow("1-3100", "Persediaan HC", domain.AccountAsset,
			domain.FromInt(800_000_000), domain.FromInt(300_000_000)),
		// SC: D=400M (project-wide), C=150M (alokasi Unit X) → net=250M (Unit Y saja)
		tbRow("1-3200", "Persediaan SC", domain.AccountAsset,
			domain.FromInt(400_000_000), domain.FromInt(150_000_000)),
		tbRow("2-1000", "Hutang Usaha", domain.AccountLiability,
			domain.Zero, domain.FromInt(1_200_000_000)),
		tbRow("4-1000", "Pendapatan Penjualan Unit", domain.AccountRevenue,
			domain.Zero, domain.FromInt(600_000_000)),
		tbRow("5-1000", "HPP", domain.AccountExpense,
			domain.FromInt(450_000_000), domain.Zero),
	}

	neracaA := reporting.ComputeNeraca(scenarioA, asOf)

	if !neracaA.IsBalanced {
		t.Fatalf("Skenario A: neraca harus balanced — TotalAset=%s TotalKE=%s",
			neracaA.TotalAset, neracaA.TotalKewajibanEkuitas)
	}

	// Persediaan (semua akun 1-3xxx) = net 1-3100 + net 1-3200
	// = 500M (Unit Y HC) + 250M (Unit Y SC) = 750M
	// 750M == biaya akumulasi Unit Y (500M HC + 250M SC) ✓
	unitYAccumulatedCost := domain.FromInt(500_000_000 + 250_000_000) // 750M

	var totalPersediaan domain.Money
	for _, line := range neracaA.Aset {
		if line.Code == "1-3100" || line.Code == "1-3200" {
			v, err := domain.NewMoney(line.Amount)
			if err != nil {
				t.Fatalf("parse Amount %q: %v", line.Amount, err)
			}
			totalPersediaan = totalPersediaan.Add(v)
		}
	}
	if !totalPersediaan.Equal(unitYAccumulatedCost) {
		t.Errorf("Skenario A: Persediaan Neraca=%s, biaya Unit Y (belum-BAST)=%s — harus sama",
			totalPersediaan.String(), unitYAccumulatedCost.String())
	}

	// ── Skenario B: semua unit BAST (Unit Y juga terjual 900M, HPP 750M) ────
	//
	// Tambahan jurnal di atas Skenario A:
	//   J5: Dr 1-1300 Bank 900M / Cr 4-1000 Pendapatan 900M
	//   J6: Dr 5-1000 HPP 750M / Cr 1-3100 500M + Cr 1-3200 250M
	//
	// Σ D total = 2250M+900M+750M = 3900M  |  Σ C total = 2250M+900M+500M+250M = 3900M ✓
	scenarioB := []ledger.TrialBalanceRow{
		tbRow("1-1300", "Bank — BCA", domain.AccountAsset,
			domain.FromInt(1_500_000_000), domain.Zero), // 600M + 900M
		// HC: D=800M, C=800M (300M+500M semua BAST) → net=0
		tbRow("1-3100", "Persediaan HC", domain.AccountAsset,
			domain.FromInt(800_000_000), domain.FromInt(800_000_000)),
		// SC: D=400M, C=400M (150M+250M semua BAST) → net=0
		tbRow("1-3200", "Persediaan SC", domain.AccountAsset,
			domain.FromInt(400_000_000), domain.FromInt(400_000_000)),
		tbRow("2-1000", "Hutang Usaha", domain.AccountLiability,
			domain.Zero, domain.FromInt(1_200_000_000)),
		tbRow("4-1000", "Pendapatan Penjualan Unit", domain.AccountRevenue,
			domain.Zero, domain.FromInt(1_500_000_000)), // 600M + 900M
		tbRow("5-1000", "HPP", domain.AccountExpense,
			domain.FromInt(1_200_000_000), domain.Zero), // 450M + 750M
	}

	neracaB := reporting.ComputeNeraca(scenarioB, asOf)

	if !neracaB.IsBalanced {
		t.Fatalf("Skenario B: neraca harus balanced — TotalAset=%s TotalKE=%s",
			neracaB.TotalAset, neracaB.TotalKewajibanEkuitas)
	}

	// Setelah semua unit BAST: Persediaan == 0
	var totalPersediaanB domain.Money
	for _, line := range neracaB.Aset {
		if line.Code == "1-3100" || line.Code == "1-3200" {
			v, err := domain.NewMoney(line.Amount)
			if err != nil {
				t.Fatalf("parse Amount %q: %v", line.Amount, err)
			}
			totalPersediaanB = totalPersediaanB.Add(v)
		}
	}
	if !totalPersediaanB.IsZero() {
		t.Errorf("Skenario B (semua unit BAST): Persediaan harus 0, got %s", totalPersediaanB.String())
	}
}

// TestComputeNeraca_LabaRugi_IndependenDariPersamaanNeraca membuktikan bahwa
// LabaRugi di ComputeNeraca dihitung dari akun 4-xxxx (Pendapatan) dan 5-xxxx (Beban),
// INDEPENDEN dari sisi Aset/Kewajiban/Ekuitas — BUKAN dari (Aset - Kewajiban - Ekuitas).
//
// Bukti anti-tautologi: jika LabaRugi diturunkan dari persamaan (Aset - Kewajiban - Ekuitas),
// maka IsBalanced selalu true secara trivial, tidak peduli jurnal balance atau tidak.
// Test ini menunjukkan bahwa input tidak-balance → IsBalanced=false, membuktikan bahwa
// LabaRugi adalah kalkulasi independen.
func TestComputeNeraca_LabaRugi_IndependenDariPersamaanNeraca(t *testing.T) {
	// ── Bagian 1: neraca dengan P&L, verifikasi LabaRugi dari akun 4/5-xxx ────

	// Jurnal balanced: Dr 1-1300 500M / Cr 4-1000 500M, Dr 5-3000 200M / Cr 1-1300 200M
	rowsBalanced := []ledger.TrialBalanceRow{
		tbRow("1-1300", "Bank", domain.AccountAsset,
			domain.FromInt(500_000_000), domain.FromInt(200_000_000)),
		tbRow("4-1000", "Pendapatan", domain.AccountRevenue,
			domain.Zero, domain.FromInt(500_000_000)),
		tbRow("5-3000", "Beban Operasi", domain.AccountExpense,
			domain.FromInt(200_000_000), domain.Zero),
	}

	balanced := reporting.ComputeNeraca(rowsBalanced, asOf)

	// LabaRugi HARUS sama dengan Pendapatan - Beban (dari akun 4/5-xxx), bukan Aset-Kewajiban
	expectedLabaRugi := "300000000" // 500M - 200M
	if balanced.LabaRugiTahunBerjalan != expectedLabaRugi {
		t.Errorf("LabaRugi dari P&L: got %s, want %s", balanced.LabaRugiTahunBerjalan, expectedLabaRugi)
	}
	if !balanced.IsBalanced {
		t.Errorf("input balanced seharusnya IsBalanced=true")
	}

	// ── Bagian 2: input TIDAK-BALANCE → IsBalanced harus false ─────────────
	//
	// Ini membuktikan IsBalanced BUKAN tautologi.
	// Jika LabaRugi = Aset - Kewajiban - Ekuitas, maka:
	//   LabaRugi = 1000M - 600M - 0 = 400M → TotalKE = 600M + 400M = 1000M = TotalAset → selalu true!
	// Tapi kode menghitung LabaRugi dari akun 4/5-xxx = 0 (tidak ada P&L accounts):
	//   TotalKE = 600M + 0 = 600M ≠ 1000M → IsBalanced = false ✓
	rowsUnbalanced := []ledger.TrialBalanceRow{
		// Dr 1-3100 1000M / Cr 2-1000 600M → tidak balance (selisih 400M)
		tbRow("1-3100", "Persediaan HC", domain.AccountAsset,
			domain.FromInt(1_000_000_000), domain.Zero),
		tbRow("2-1000", "Hutang Usaha", domain.AccountLiability,
			domain.Zero, domain.FromInt(600_000_000)),
		// Tidak ada akun 4-xxx atau 5-xxx → LabaRugi = 0
	}

	unbalanced := reporting.ComputeNeraca(rowsUnbalanced, asOf)

	// LabaRugi dari P&L = 0 (tidak ada akun 4/5-xxx)
	if unbalanced.LabaRugiTahunBerjalan != "0" {
		t.Errorf("LabaRugi dari P&L (tanpa akun 4/5-xxx): got %s, want 0", unbalanced.LabaRugiTahunBerjalan)
	}
	// IsBalanced harus false: TotalAset=1000M ≠ TotalKE=600M+0=600M
	if unbalanced.IsBalanced {
		t.Errorf("IsBalanced seharusnya false untuk input tidak-balance (membuktikan bukan tautologi)")
	}
	if unbalanced.TotalAset != "1000000000" {
		t.Errorf("TotalAset: got %s, want 1000000000", unbalanced.TotalAset)
	}
	if unbalanced.TotalKewajibanEkuitas != "600000000" {
		t.Errorf("TotalKewajibanEkuitas: got %s, want 600000000 (LabaRugi=0, bukan 400M)", unbalanced.TotalKewajibanEkuitas)
	}
}

// TestComputeNeraca_EndToEnd_LITHOSFase1_TigaUnitBAST membuktikan angka end-to-end
// menggunakan skenario riil LITHOS Villas Fase 1 (6 unit Villa Type A, 150sqm, Rp 2.8M).
//
// Skenario setelah seed LITHOS + posting biaya + 3 dari 6 unit BAST:
//
//	Hard Cost: 6 unit × 700M = 4.200M (Dr 1-3100 / Cr 2-1000 Hutang)
//	Soft Cost project-wide: 1.800M total (Dr 1-3200 / Cr 2-1000)
//	  Per unit: 1.800M / 6 = 300M (area sama, alokasi rata)
//	Per-unit accumulated cost: 700M + 300M = 1.000M
//	3 unit BAST: revenue 3 × 2.800M = 8.400M, HPP 3 × 1.000M = 3.000M
//
// Verifikasi Aset = Kewajiban + Ekuitas + LabaRugi
// dan Persediaan = 3 unit belum-BAST × 1.000M = 3.000M
func TestComputeNeraca_EndToEnd_LITHOSFase1_TigaUnitBAST(t *testing.T) {
	// Jurnal-jurnal yang diposting (lewat cost.PostingService dan sale.PostingService):
	//
	//   J1: Dr 1-3100 HC 4.200M / Cr 2-1000 Hutang 4.200M
	//       (kapitalisasi hard cost 6 unit Villa A @ 700M/unit)
	//   J2: Dr 1-3200 SC 1.800M / Cr 2-1000 Hutang 1.800M
	//       (kapitalisasi soft cost project-wide; allocation engine distribusikan 300M/unit)
	//   J3: Dr 1-1300 Bank 8.400M / Cr 4-1000 Pendapatan 8.400M
	//       (Event 3 pengakuan pendapatan: 3 unit × 2.800M — kriteria PSAK 72 terpenuhi)
	//   J4: Dr 5-1000 HPP 3.000M / Cr 1-3100 2.100M + Cr 1-3200 900M
	//       (Event 4 BAST: 3 unit × 1.000M HPP = 3 × 700M HC + 3 × 300M SC)
	//
	// Verifikasi Σ D = Σ C:
	//   D = 4.200M + 1.800M + 8.400M + 3.000M = 17.400M
	//   C = 4.200M + 1.800M + 8.400M + 2.100M + 900M = 17.400M ✓

	rows := []ledger.TrialBalanceRow{
		// Bank: D=8.400M (dari penjualan 3 unit)
		tbRow("1-1300", "Bank — BCA", domain.AccountAsset,
			domain.FromInt(8_400_000_000), domain.Zero),
		// HC: D=4.200M (6 unit), C=2.100M (BAST 3 unit × 700M) → net=2.100M (3 belum-BAST)
		tbRow("1-3100", "Persediaan Hard Cost", domain.AccountAsset,
			domain.FromInt(4_200_000_000), domain.FromInt(2_100_000_000)),
		// SC: D=1.800M (project-wide), C=900M (BAST 3 unit × 300M) → net=900M (3 belum-BAST)
		tbRow("1-3200", "Persediaan Soft Cost", domain.AccountAsset,
			domain.FromInt(1_800_000_000), domain.FromInt(900_000_000)),
		// Hutang: 4.200M HC + 1.800M SC = 6.000M total
		tbRow("2-1000", "Hutang Usaha", domain.AccountLiability,
			domain.Zero, domain.FromInt(6_000_000_000)),
		// Pendapatan: 3 unit × 2.800M = 8.400M
		tbRow("4-1000", "Pendapatan Penjualan Unit", domain.AccountRevenue,
			domain.Zero, domain.FromInt(8_400_000_000)),
		// HPP: 3 unit × 1.000M = 3.000M
		tbRow("5-1000", "HPP — Biaya Terakumulasi Unit", domain.AccountExpense,
			domain.FromInt(3_000_000_000), domain.Zero),
	}

	neraca := reporting.ComputeNeraca(rows, asOf)
	plRows := []reporting.PLRawRow{
		{AccountCode: "4-1000", AccountName: "Pendapatan Penjualan Unit", TotalCredit: domain.FromInt(8_400_000_000)},
		{AccountCode: "5-1000", AccountName: "HPP — Biaya Terakumulasi Unit", TotalDebit: domain.FromInt(3_000_000_000)},
	}
	pid := uint64(1) // LITHOS project
	pl := reporting.ComputePL(plRows, &pid, asOf)

	// ── Verifikasi Neraca ────────────────────────────────────────────────────

	if !neraca.IsBalanced {
		t.Fatalf("LITHOS Fase 1: neraca harus balanced — TotalAset=%s TotalKE=%s",
			neraca.TotalAset, neraca.TotalKewajibanEkuitas)
	}

	// TotalAset = Bank 8.400M + HC net 2.100M + SC net 900M = 11.400M
	if neraca.TotalAset != "11400000000" {
		t.Errorf("TotalAset: got %s, want 11400000000 (8400M bank + 2100M HC + 900M SC)",
			neraca.TotalAset)
	}

	// TotalKewajiban = 6.000M (hutang konstruksi)
	if neraca.TotalKewajiban != "6000000000" {
		t.Errorf("TotalKewajiban: got %s, want 6000000000", neraca.TotalKewajiban)
	}

	// LabaRugi = Pendapatan 8.400M − HPP 3.000M = 5.400M
	if neraca.LabaRugiTahunBerjalan != "5400000000" {
		t.Errorf("LabaRugiTahunBerjalan: got %s, want 5400000000 (8400M pendapatan - 3000M HPP)",
			neraca.LabaRugiTahunBerjalan)
	}

	// TotalKE = 6.000M + 0 + 5.400M = 11.400M == TotalAset ✓
	if neraca.TotalKewajibanEkuitas != "11400000000" {
		t.Errorf("TotalKewajibanEkuitas: got %s, want 11400000000", neraca.TotalKewajibanEkuitas)
	}

	// ── Verifikasi Persediaan == biaya 3 unit belum-BAST ────────────────────

	// 3 unit belum-BAST: masing-masing 700M HC + 300M SC = 1.000M
	// Total persediaan yang diharapkan = 3 × 1.000M = 3.000M
	expectedPersediaan := domain.FromInt(3_000_000_000)

	var totalPersediaan domain.Money
	for _, line := range neraca.Aset {
		if line.Code == "1-3100" || line.Code == "1-3200" {
			v, err := domain.NewMoney(line.Amount)
			if err != nil {
				t.Fatalf("parse Amount %q: %v", line.Amount, err)
			}
			totalPersediaan = totalPersediaan.Add(v)
		}
	}
	if !totalPersediaan.Equal(expectedPersediaan) {
		t.Errorf("Persediaan Neraca=%s, biaya 3 unit belum-BAST=%s — harus sama",
			totalPersediaan.String(), expectedPersediaan.String())
	}

	// ── Verifikasi P&L ──────────────────────────────────────────────────────

	if pl.TotalPendapatan != "8400000000" {
		t.Errorf("P&L TotalPendapatan: got %s, want 8400000000", pl.TotalPendapatan)
	}
	if pl.TotalHPP != "3000000000" {
		t.Errorf("P&L TotalHPP: got %s, want 3000000000", pl.TotalHPP)
	}
	if pl.LabaRugiBersih != "5400000000" {
		t.Errorf("P&L LabaRugiBersih: got %s, want 5400000000", pl.LabaRugiBersih)
	}

	// LabaRugi dari Neraca == LabaRugi dari P&L (keduanya dari akun 4/5-xxx yang sama)
	neracaLaba := mustMoney(neraca.LabaRugiTahunBerjalan)
	plLaba := mustMoney(pl.LabaRugiBersih)
	if !neracaLaba.Equal(plLaba) {
		t.Errorf("LabaRugi Neraca (%s) != LabaRugi P&L (%s) — harus sama karena sumber sama",
			neracaLaba.String(), plLaba.String())
	}
}

// TestComputeNeraca_Persediaan_IsAset_DanUMP_IsKewajiban memverifikasi:
// - Persediaan (1-3xxx) masuk ke sisi Aset (bukan Kewajiban)
// - Uang Muka Penjualan (2-2000) masuk ke sisi Kewajiban (Invariant #7)
func TestComputeNeraca_Persediaan_IsAset_DanUMP_IsKewajiban(t *testing.T) {
	rows := []ledger.TrialBalanceRow{
		tbRow("1-3000", "Persediaan — Tanah", domain.AccountAsset,
			domain.FromInt(2_000_000_000), domain.Zero),
		tbRow("1-3100", "Persediaan Hard Cost", domain.AccountAsset,
			domain.FromInt(500_000_000), domain.Zero),
		tbRow("2-2000", "Uang Muka Penjualan", domain.AccountLiability,
			domain.Zero, domain.FromInt(2_500_000_000)),
	}

	neraca := reporting.ComputeNeraca(rows, asOf)

	// Persediaan harus di sisi Aset
	if len(neraca.Aset) != 2 {
		t.Fatalf("harus ada 2 baris aset (persediaan), got %d", len(neraca.Aset))
	}
	// UMP harus di sisi Kewajiban
	if len(neraca.Kewajiban) != 1 {
		t.Fatalf("harus ada 1 baris kewajiban (UMP), got %d", len(neraca.Kewajiban))
	}
	if neraca.Kewajiban[0].Code != "2-2000" {
		t.Errorf("kewajiban[0] harus 2-2000, got %s", neraca.Kewajiban[0].Code)
	}
	if !neraca.IsBalanced {
		t.Fatalf("neraca harus balanced")
	}
}

package reporting

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"esaproperti/internal/domain"
	"esaproperti/internal/ledger"
	"esaproperti/internal/receivable"
	"esaproperti/internal/tax"
)

// ── Interfaces ────────────────────────────────────────────────────────────────

// LedgerQuerier adalah subset dari ledger.Querier yang dibutuhkan reporting.
// *ledger.QueryService memenuhi interface ini.
type LedgerQuerier interface {
	TrialBalance(ctx context.Context, tenantID uint64, asOf time.Time) (*ledger.TrialBalance, error)
	GeneralLedger(ctx context.Context, tenantID uint64, filter ledger.LedgerFilter) ([]ledger.LedgerEntry, error)
	ListAccounts(ctx context.Context, tenantID uint64) ([]*ledger.Account, error)
}

// PLReader menyediakan data mentah Laba Rugi dari database.
// from (Item 5, filter tanggal Neraca & L/R): nil = tanpa batas bawah (life-to-date
// sejak tutup buku terakhir, perilaku lama); non-nil = hanya jurnal dgn
// je.date >= from yang dihitung.
type PLReader interface {
	GetProjectPLRows(ctx context.Context, tenantID, projectID uint64, from *time.Time, asOf time.Time) ([]PLRawRow, error)
	GetConsolidatedPLRows(ctx context.Context, tenantID uint64, from *time.Time, asOf time.Time) ([]PLRawRow, error)
}

// TaxReader (S7): pembaca KANONIK laporan pajak — diimplementasi tax.Service
// (GetTaxReport, registry #14). Reporting TIDAK meng-agregasi tax_obligations
// sendiri lagi.
type TaxReader interface {
	GetTaxReport(ctx context.Context, tenantID uint64, from, to time.Time) (*tax.TaxReport, error)
}

// PipelineReader menyediakan statistik pipeline penjualan.
type PipelineReader interface {
	GetPipelineStats(ctx context.Context, tenantID uint64) (*PipelineStats, error)
}

// CashFlowReader menyediakan data arus kas dari jurnal yang sudah diposting.
type CashFlowReader interface {
	GetCashMovements(ctx context.Context, tenantID uint64, from, to time.Time) ([]CashMovement, error)
}

// HouseReceivableReader (W-8) menyediakan baris PIUTANG HARGA RUMAH.
// Diimplementasi oleh sale.Service, dipasang lewat WithHouseReader.
//
// Menggantikan ARAgingReader/GetARScheduleRows yang membaca `payment_schedules`
// apa adanya. BD-1: piutang harga rumah lahir SAAT BAST — jadwal sebelum BAST
// adalah rencana penagihan, bukan piutang. Query jadwal lama tidak pernah
// menyentuh `sale_records` maupun jurnal, sehingga angkanya tidak punya
// hubungan apa pun dengan akun kontrol piutang di buku besar; pemiliknya kini
// paket `sale`, yang menulis peristiwa piutangnya.
type HouseReceivableReader interface {
	ReceivableRows(ctx context.Context, tenantID uint64) ([]receivable.Row, error)
}

// RealizationReceivableReader (W-4) menyediakan baris tagihan BIAYA REALISASI
// yang masih ditagih ke customer. Diimplementasi oleh charge.Service.
//
// Interface-nya hidup di sini dan bukan di `charge` supaya arah ketergantungan
// benar: `charge` (transaksional) tidak boleh import `reporting` (read model).
// Keduanya hanya bertemu di `receivable.Row` — kosakata bersama tanpa DB
// (TD-3 / D-W4-2). Pemasangannya lewat WithRealizationReader di main.go.
type RealizationReceivableReader interface {
	ReceivableRows(ctx context.Context, tenantID uint64) ([]receivable.Row, error)
}

// LegacyReceivableReader (W-7) menyediakan baris PIUTANG PROYEK LAMA hasil
// import. Diimplementasi oleh legacyar.Service, dipasang lewat WithLegacyReader.
//
// Alasan interface-nya terpisah dari RealizationReceivableReader walau
// bentuknya identik: keduanya boleh berevolusi sendiri, dan sebuah tenant bisa
// memakai yang satu tanpa yang lain. Menyatukannya hanya karena signature-nya
// kebetulan sama akan mengikat dua bounded context yang tidak berhubungan.
type LegacyReceivableReader interface {
	ReceivableRows(ctx context.Context, tenantID uint64) ([]receivable.Row, error)
}

// LandReceivableReader menyediakan baris PIUTANG KELEBIHAN TANAH (produk
// tambahan dari internal/land, bukan charge_items) — sumber SourceAddon
// KEDUA, terpisah dari RealizationReceivableReader (yang memuat addon lama
// berbasis charge_items). Diimplementasi oleh land.Service, dipasang lewat
// SetLandReceivable.
//
// Bug ditemukan 2026-08-31: land_sales sebelumnya tidak pernah dibaca oleh
// mesin AR sama sekali — piutang Kelebihan Tanah benar tercatat di jurnal
// (GL 1-2000) tapi tidak pernah muncul di layar Piutang Customer/collection
// dashboard. Belum ada pelacakan pembayaran per land_sale (lihat komentar di
// land/receivable.go) — baris ini selalu tampil "belum dibayar" sepenuhnya
// sampai mekanisme itu dibangun.
type LandReceivableReader interface {
	ReceivableRows(ctx context.Context, tenantID uint64) ([]receivable.Row, error)
}

// ── Service ───────────────────────────────────────────────────────────────────

// Service mengelola laporan keuangan.
// READ-ONLY: tidak ada JournalWriter atau operasi tulis apa pun.
type Service struct {
	ledger      LedgerQuerier
	pl          PLReader
	pipeline    PipelineReader
	cashFlow    CashFlowReader
	house       HouseReceivableReader      // W-8: piutang harga rumah (WAJIB di produksi)
	realization RealizationReceivableReader // W-4: tagihan biaya realisasi (opsional)
	legacy      LegacyReceivableReader      // W-7: piutang proyek lama (opsional)
	land        LandReceivableReader        // piutang Kelebihan Tanah (opsional, 2026-08-31)
	finance     ContractFinanceReader       // R4: TotalAdvance kanonik (opsional)
	tax         TaxReader                   // S7: laporan pajak kanonik (opsional)
}

func NewService(lq LedgerQuerier, pl PLReader, pipeline PipelineReader, cf CashFlowReader) *Service {
	return &Service{ledger: lq, pl: pl, pipeline: pipeline, cashFlow: cf}
}

// WithHouseReader (W-8) memasang sumber piutang harga rumah. Berbeda dengan
// realisasi dan legacy yang boleh absen, sumber ini WAJIB: tanpa ia, laporan
// piutang menolak tampil (ErrARReaderNotConfigured) alih-alih diam-diam
// melaporkan nol untuk seluruh piutang penjualan rumah.
func (s *Service) WithHouseReader(h HouseReceivableReader) *Service {
	s.house = h
	return s
}

// WithRealizationReader (W-4) memasang sumber tagihan biaya realisasi sehingga
// Piutang Customer menampilkan SATU eksposur. Opsional: tenant yang belum
// memakai Charge Group tetap mendapat laporan harga rumah yang benar.
func (s *Service) WithRealizationReader(r RealizationReceivableReader) *Service {
	s.realization = r
	return s
}

// WithLegacyReader (W-7) memasang sumber piutang proyek lama. Opsional dengan
// alasan yang sama seperti realisasi: tenant yang tidak punya proyek sebelum
// sistem ini tetap mendapat laporan yang benar tanpa memasangnya.
func (s *Service) WithLegacyReader(r LegacyReceivableReader) *Service {
	s.legacy = r
	return s
}

// WithLandReader memasang sumber piutang Kelebihan Tanah. Opsional dengan
// alasan yang sama seperti realisasi/legacy: tenant tanpa modul Kelebihan
// Tanah aktif tetap mendapat laporan yang benar tanpa memasangnya.
func (s *Service) WithLandReader(r LandReceivableReader) *Service {
	s.land = r
	return s
}

// WithContractFinance (R4) memasang sumber kanonik keuangan kontrak
// (sale.Service) — dipakai GetSalesPipeline utk TotalAdvance.
func (s *Service) WithContractFinance(f ContractFinanceReader) *Service {
	s.finance = f
	return s
}

// WithTaxReader (S7) memasang pembaca kanonik laporan pajak (tax.Service).
func (s *Service) WithTaxReader(t TaxReader) *Service {
	s.tax = t
	return s
}

// ── Pure functions (tidak ada DB access, mudah ditest) ───────────────────────

// ComputeNeraca menghasilkan neraca dari baris trial balance.
//
// Untuk setiap akun:
//   - "1-xxxx" → Aset: net = TotalDebit - TotalCredit
//     (positif = aset; negatif untuk kontra-aset 1-4900 normal balance kredit)
//   - "2-xxxx" → Kewajiban: net = TotalCredit - TotalDebit
//   - "3-xxxx" → Ekuitas: net = TotalCredit - TotalDebit
//   - "4-xxxx" → Pendapatan: net = TotalCredit - TotalDebit
//   - "5-xxxx" → Beban: net = TotalDebit - TotalCredit
//
// Balance check: TotalAset == TotalKewajiban + TotalEkuitas + LabaRugiTahunBerjalan
// Selalu true selama semua jurnal diposting balanced (Σ debit == Σ kredit).
func ComputeNeraca(rows []ledger.TrialBalanceRow, asOf time.Time) NeracaReport {
	return computeNeraca(rows, asOf, nil, nil)
}

// ComputeNeracaRange (Item 5): sama seperti ComputeNeraca (Aset/Kewajiban/
// Ekuitas/LabaRugiTahunBerjalan/IsBalanced tetap life-to-date s/d asOf, TIDAK
// PERNAH berubah oleh from), plus LabaRugiPeriodeTerpilih — Laba Rugi dihitung
// dari jendela [from, asOf] via ComputePL (reuse mesin P&L yang sama, bukan
// engine baru), MURNI sebagai baris informasi tambahan. Mencampur P&L
// berjendela ke dalam identitas neraca akan merusak keseimbangan Aset =
// Kewajiban+Ekuitas sebesar laba yang diakui sebelum `from` (kontra-akun
// asetnya, mis. akumulasi penyusutan, tetap kumulatif) — karena itu jendela
// TIDAK PERNAH menggantikan LabaRugiTahunBerjalan.
func ComputeNeracaRange(rows []ledger.TrialBalanceRow, plRows []PLRawRow, from, asOf time.Time) NeracaReport {
	pl := ComputePL(plRows, nil, asOf)
	periodeTerpilih, err := domain.NewMoney(pl.LabaRugiBersih)
	if err != nil {
		// pl.LabaRugiBersih selalu string decimal valid (output domain.Money.String()) —
		// tidak pernah gagal parse di jalur normal.
		periodeTerpilih = domain.Zero
	}
	return computeNeraca(rows, asOf, &from, &periodeTerpilih)
}

// computeNeraca adalah inti bersama ComputeNeraca/ComputeNeracaRange.
// periodeTerpilih!=nil hanya mengisi field informasi LabaRugiPeriodeTerpilih —
// TIDAK PERNAH memengaruhi LabaRugiTahunBerjalan/TotalEkuitas/IsBalanced.
func computeNeraca(rows []ledger.TrialBalanceRow, asOf time.Time, from *time.Time, periodeTerpilih *domain.Money) NeracaReport {
	var totalAset, totalKewajiban, totalEkuitas, totalPendapatan, totalBeban domain.Money
	var asetLines, kewajibanLines, ekuitasLines []NeracaLine

	for _, row := range rows {
		switch {
		case strings.HasPrefix(row.AccountCode, "1-"):
			net := row.TotalDebit.Sub(row.TotalCredit)
			totalAset = totalAset.Add(net)
			asetLines = append(asetLines, NeracaLine{
				Code:   row.AccountCode,
				Name:   row.AccountName,
				Amount: net.String(),
			})
		case strings.HasPrefix(row.AccountCode, "2-"):
			net := row.TotalCredit.Sub(row.TotalDebit)
			totalKewajiban = totalKewajiban.Add(net)
			kewajibanLines = append(kewajibanLines, NeracaLine{
				Code:   row.AccountCode,
				Name:   row.AccountName,
				Amount: net.String(),
			})
		case strings.HasPrefix(row.AccountCode, "3-"):
			net := row.TotalCredit.Sub(row.TotalDebit)
			totalEkuitas = totalEkuitas.Add(net)
			ekuitasLines = append(ekuitasLines, NeracaLine{
				Code:   row.AccountCode,
				Name:   row.AccountName,
				Amount: net.String(),
			})
		case strings.HasPrefix(row.AccountCode, "4-"):
			totalPendapatan = totalPendapatan.Add(row.TotalCredit.Sub(row.TotalDebit))
		case strings.HasPrefix(row.AccountCode, "5-"):
			totalBeban = totalBeban.Add(row.TotalDebit.Sub(row.TotalCredit))
		}
	}

	labaRugi := totalPendapatan.Sub(totalBeban)
	totalKE := totalKewajiban.Add(totalEkuitas).Add(labaRugi)

	report := NeracaReport{
		AsOf:                  asOf,
		From:                  from,
		Aset:                  asetLines,
		Kewajiban:             kewajibanLines,
		Ekuitas:               ekuitasLines,
		TotalAset:             totalAset.String(),
		TotalKewajiban:        totalKewajiban.String(),
		TotalEkuitas:          totalEkuitas.String(),
		LabaRugiTahunBerjalan: labaRugi.String(),
		TotalEkuitasEfektif:   totalEkuitas.Add(labaRugi).String(),
		TotalKewajibanEkuitas: totalKE.String(),
		IsBalanced:            totalAset.Equal(totalKE),
	}
	if periodeTerpilih != nil {
		report.LabaRugiPeriodeTerpilih = periodeTerpilih.String()
	}
	return report
}

// sumByPaymentType (P3, item A): jumlahkan baris RevenueByPaymentTypeRow ke
// (cash, kpr) — payment_type "tunai" → cash, "kpr" → kpr. Baris payment_type=""
// (belum/tidak ber-kontrak) SENGAJA diabaikan di sini (tidak dipaksa ke salah
// satu sisi) — itu sebabnya cash+kpr bisa < total sumber aslinya.
func sumByPaymentType(rows []RevenueByPaymentTypeRow) (cash, kpr string) {
	var cashSum, kprSum domain.Money
	for _, r := range rows {
		switch r.PaymentType {
		case "tunai":
			cashSum = cashSum.Add(r.Amount)
		case "kpr":
			kprSum = kprSum.Add(r.Amount)
		}
	}
	return cashSum.String(), kprSum.String()
}

// ComputePL menghasilkan Laba Rugi 8-bagian dari baris mentah P&L, via
// ledger.AccountRole registry (item B, 2026-08-27) — bukan flat "semua 4-x /
// semua 5-x" seperti versi lama. Default aman utk akun BARU yang belum
// terdaftar eksplisit di registry:
//   - 4-xxxx yang bukan RoleOtherIncome → Pendapatan inti (operasional).
//   - 5-xxxx yang bukan RoleCOGS/RoleTaxExpense/RoleOtherExpense → Beban
//     Operasional.
//
// rows berisi akun-akun 4-xxxx (pendapatan) dan 5-xxxx (beban).
func ComputePL(rows []PLRawRow, projectID *uint64, asOf time.Time) PLReport {
	var pendapatanLines, hppLines, bebanOpLines, otherIncomeLines, otherExpenseLines, taxLines []PLLine
	var totalPendapatan, totalHPP, totalBebanOp, totalOtherIncome, totalOtherExpense, totalTax domain.Money

	cogs := ledger.RoleCodeList(ledger.RoleCOGS)
	otherIncome := ledger.RoleCodeList(ledger.RoleOtherIncome)
	otherExpense := ledger.RoleCodeList(ledger.RoleOtherExpense)
	taxExpense := ledger.RoleCodeList(ledger.RoleTaxExpense)
	in := func(code string, list []string) bool {
		for _, c := range list {
			if c == code {
				return true
			}
		}
		return false
	}

	for _, row := range rows {
		switch {
		case strings.HasPrefix(row.AccountCode, "4-"):
			net := row.TotalCredit.Sub(row.TotalDebit)
			line := PLLine{Code: row.AccountCode, Name: row.AccountName, Amount: net.String()}
			if in(row.AccountCode, otherIncome) {
				totalOtherIncome = totalOtherIncome.Add(net)
				otherIncomeLines = append(otherIncomeLines, line)
			} else {
				totalPendapatan = totalPendapatan.Add(net)
				pendapatanLines = append(pendapatanLines, line)
			}
		case strings.HasPrefix(row.AccountCode, "5-"):
			net := row.TotalDebit.Sub(row.TotalCredit)
			line := PLLine{Code: row.AccountCode, Name: row.AccountName, Amount: net.String()}
			switch {
			case in(row.AccountCode, cogs):
				totalHPP = totalHPP.Add(net)
				hppLines = append(hppLines, line)
			case in(row.AccountCode, taxExpense):
				totalTax = totalTax.Add(net)
				taxLines = append(taxLines, line)
			case in(row.AccountCode, otherExpense):
				totalOtherExpense = totalOtherExpense.Add(net)
				otherExpenseLines = append(otherExpenseLines, line)
			default:
				totalBebanOp = totalBebanOp.Add(net)
				bebanOpLines = append(bebanOpLines, line)
			}
		}
	}

	labaKotor := totalPendapatan.Sub(totalHPP)
	labaOperasional := labaKotor.Sub(totalBebanOp)
	labaSebelumPajak := labaOperasional.Add(totalOtherIncome).Sub(totalOtherExpense)
	labaBersih := labaSebelumPajak.Sub(totalTax)

	return PLReport{
		AsOf:      asOf,
		ProjectID: projectID,

		Pendapatan:      pendapatanLines,
		TotalPendapatan: totalPendapatan.String(),

		HPP:      hppLines,
		TotalHPP: totalHPP.String(),

		LabaKotor: labaKotor.String(),

		BebanOperasional:      bebanOpLines,
		TotalBebanOperasional: totalBebanOp.String(),

		LabaOperasional: labaOperasional.String(),

		PendapatanLuarUsaha:      otherIncomeLines,
		TotalPendapatanLuarUsaha: totalOtherIncome.String(),

		BebanLuarUsaha:      otherExpenseLines,
		TotalBebanLuarUsaha: totalOtherExpense.String(),

		LabaBersihSebelumPajak: labaSebelumPajak.String(),

		BebanPajak:      taxLines,
		TotalBebanPajak: totalTax.String(),

		LabaRugiBersih: labaBersih.String(),
	}
}

// ── Service methods ───────────────────────────────────────────────────────────

// GetNeraca menghasilkan Neraca per asOf. Akun neraca (1-/2-/3-) SELALU
// kumulatif s/d asOf — neraca adalah snapshot per tanggal, bukan rentang.
// from (Item 5, opsional): saat diisi, baris "Laba Rugi Tahun Berjalan"
// dihitung ulang dari jendela [from, asOf] (reuse ComputePL) menggantikan
// default life-to-date sejak tutup buku terakhir — dipakai saat user memilih
// Start Date pada filter Neraca. from=nil = perilaku lama (tanpa perubahan).
func (s *Service) GetNeraca(ctx context.Context, tenantID uint64, from *time.Time, asOf time.Time) (*NeracaReport, error) {
	tb, err := s.ledger.TrialBalance(ctx, tenantID, asOf)
	if err != nil {
		return nil, fmt.Errorf("reporting: GetNeraca: %w", err)
	}
	if from == nil {
		neraca := ComputeNeraca(tb.Rows, asOf)
		return &neraca, nil
	}
	plRows, err := s.pl.GetConsolidatedPLRows(ctx, tenantID, from, asOf)
	if err != nil {
		return nil, fmt.Errorf("reporting: GetNeraca: %w", err)
	}
	neraca := ComputeNeracaRange(tb.Rows, plRows, *from, asOf)
	return &neraca, nil
}

func (s *Service) GetProjectPL(ctx context.Context, tenantID, projectID uint64, from *time.Time, asOf time.Time) (*PLReport, error) {
	rows, err := s.pl.GetProjectPLRows(ctx, tenantID, projectID, from, asOf)
	if err != nil {
		return nil, fmt.Errorf("reporting: GetProjectPL: %w", err)
	}
	pid := projectID
	report := ComputePL(rows, &pid, asOf)
	report.From = from
	return &report, nil
}

func (s *Service) GetKonsolidasiPL(ctx context.Context, tenantID uint64, from *time.Time, asOf time.Time) (*PLReport, error) {
	rows, err := s.pl.GetConsolidatedPLRows(ctx, tenantID, from, asOf)
	if err != nil {
		return nil, fmt.Errorf("reporting: GetKonsolidasiPL: %w", err)
	}
	report := ComputePL(rows, nil, asOf)
	report.From = from
	return &report, nil
}

func (s *Service) GetSalesPipeline(ctx context.Context, tenantID uint64) (*PipelineReport, error) {
	stats, err := s.pipeline.GetPipelineStats(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("reporting: GetSalesPipeline: %w", err)
	}
	// R4: TotalAdvance dari baris portfolio kanonik (Σ TotalPaid kontrak aktif,
	// counts_toward_price=TRUE) — bukan Σ termin_payments mentah.
	if s.finance != nil {
		rows, ferr := s.finance.PortfolioFinancials(ctx, tenantID)
		if ferr != nil {
			return nil, fmt.Errorf("reporting: GetSalesPipeline advance: %w", ferr)
		}
		total := domain.Zero
		for _, r := range rows {
			total = total.Add(r.TotalPaid)
		}
		stats.TotalAdvance = total
	}
	return buildPipelineReport(stats), nil
}

func buildPipelineReport(stats *PipelineStats) *PipelineReport {
	projected := stats.AvailableSum.Add(stats.ReservedSum)
	total := stats.AvailableCount + stats.ReservedCount + stats.SoldCount
	return &PipelineReport{
		Available: PipelineUnitStat{
			Count:          stats.AvailableCount,
			TotalListPrice: stats.AvailableSum.String(),
		},
		Reserved: PipelineUnitStat{
			Count:          stats.ReservedCount,
			TotalListPrice: stats.ReservedSum.String(),
			TotalAdvance:   stats.TotalAdvance.String(),
		},
		Sold: PipelineUnitStat{
			Count:              stats.SoldCount,
			TotalListPrice:     domain.Zero.String(),
			TotalContractValue: stats.SoldContractSum.String(),
		},
		TotalUnits:       total,
		ProjectedRevenue: projected.String(),
	}
}

func (s *Service) GetArusKas(ctx context.Context, tenantID uint64, from, to time.Time) (*ArusKasReport, error) {
	movements, err := s.cashFlow.GetCashMovements(ctx, tenantID, from, to)
	if err != nil {
		return nil, fmt.Errorf("reporting: GetArusKas: %w", err)
	}
	return buildArusKas(movements, from, to), nil
}

func buildArusKas(movements []CashMovement, from, to time.Time) *ArusKasReport {
	var operasiLines, investasiLines, pendanaanLines []ArusKasLine
	var netOperasi, netInvestasi, netPendanaan domain.Money

	for _, m := range movements {
		line := ArusKasLine{Description: m.Description, Amount: m.NetBankChange.String()}
		switch {
		case m.IsInvesting:
			investasiLines = append(investasiLines, line)
			netInvestasi = netInvestasi.Add(m.NetBankChange)
		case m.IsPendanaan:
			pendanaanLines = append(pendanaanLines, line)
			netPendanaan = netPendanaan.Add(m.NetBankChange)
		default:
			operasiLines = append(operasiLines, line)
			netOperasi = netOperasi.Add(m.NetBankChange)
		}
	}

	netChange := netOperasi.Add(netInvestasi).Add(netPendanaan)
	return &ArusKasReport{
		PeriodFrom: from,
		PeriodTo:   to,
		Operasi:    ArusKasSection{Lines: operasiLines, Net: netOperasi.String()},
		Investasi:  ArusKasSection{Lines: investasiLines, Net: netInvestasi.String()},
		Pendanaan:  ArusKasSection{Lines: pendanaanLines, Net: netPendanaan.String()},
		NetChange:  netChange.String(),
	}
}

func (s *Service) GetTrialBalance(ctx context.Context, tenantID uint64, asOf time.Time) (*ledger.TrialBalance, error) {
	return s.ledger.TrialBalance(ctx, tenantID, asOf)
}

func (s *Service) GetGeneralLedger(ctx context.Context, tenantID uint64, filter ledger.LedgerFilter) ([]ledger.LedgerEntry, error) {
	return s.ledger.GeneralLedger(ctx, tenantID, filter)
}

func (s *Service) GetListAccounts(ctx context.Context, tenantID uint64) ([]*ledger.Account, error) {
	return s.ledger.ListAccounts(ctx, tenantID)
}

// ErrTaxReaderNotConfigured dikembalikan bila GetTaxLiability dipanggil tanpa
// WithTaxReader (wiring produksi memasangnya di main.go).
var ErrTaxReaderNotConfigured = errors.New("reporting: tax reader belum dikonfigurasi")

// GetTaxLiability (S7): laporan kewajiban pajak dari pembaca KANONIK
// tax.GetTaxReport + rekonsiliasi terhadap saldo ledger 2-4000 (registry #14:
// Σ obligations outstanding == saldo RoleTaxLiability). Bentuk payload API
// dipertahankan; field rekonsiliasi additive.
func (s *Service) GetTaxLiability(ctx context.Context, tenantID uint64, from, to time.Time) (*TaxLiabilityReport, error) {
	if s.tax == nil {
		return nil, ErrTaxReaderNotConfigured
	}
	rpt, err := s.tax.GetTaxReport(ctx, tenantID, from, to)
	if err != nil {
		return nil, fmt.Errorf("reporting: GetTaxLiability: %w", err)
	}

	items := make([]TaxLiabilityRow, 0, len(rpt.Items))
	for _, it := range rpt.Items {
		items = append(items, TaxLiabilityRow{
			ID:            it.ObligationID,
			UnitID:        it.UnitID,
			RateCode:      it.RateCode,
			TransferValue: it.TransferValue,
			Rate:          it.Rate,
			TaxAmount:     it.TaxAmount,
			Status:        string(it.Status),
			AccrualDate:   it.AccrualDate.Format("2006-01-02"),
		})
	}

	out := &TaxLiabilityReport{
		PeriodFrom:       from.Format("2006-01-02"),
		PeriodTo:         to.Format("2006-01-02"),
		Items:            items,
		TotalObligation:  rpt.TotalObligation,
		TotalPaid:        rpt.TotalPaid,
		TotalOutstanding: rpt.TotalOutstanding,
	}

	// Rekonsiliasi obligations ↔ ledger: outstanding SELURUH histori s/d `to`
	// vs saldo 2-4000 (RoleTaxLiability) pada trial balance asOf `to`.
	fullRpt, err := s.tax.GetTaxReport(ctx, tenantID, time.Unix(0, 0).UTC(), to)
	if err != nil {
		return nil, fmt.Errorf("reporting: GetTaxLiability rekonsiliasi: %w", err)
	}
	tb, err := s.ledger.TrialBalance(ctx, tenantID, to)
	if err != nil {
		return nil, fmt.Errorf("reporting: GetTaxLiability trial balance: %w", err)
	}
	ledgerBal := domain.Zero
	taxCodes := ledger.RoleCodeList(ledger.RoleTaxLiability)
	for _, row := range tb.Rows {
		for _, c := range taxCodes {
			if row.AccountCode == c {
				ledgerBal = ledgerBal.Add(row.Balance)
			}
		}
	}
	fullOutstanding, err := domain.NewMoney(fullRpt.TotalOutstanding)
	if err != nil {
		return nil, fmt.Errorf("reporting: GetTaxLiability parse outstanding: %w", err)
	}
	out.LedgerOutstanding = ledgerBal.String()
	out.Reconciled = ledgerBal.Equal(fullOutstanding)
	return out, nil
}

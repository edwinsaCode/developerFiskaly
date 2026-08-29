package reporting

import (
	"bytes"
	"fmt"
	"html/template"
	"io"
	"strconv"
	"strings"
	"time"

	"esaproperti/internal/domain"
	"esaproperti/internal/ledger"
	"esaproperti/internal/printkit"
	"esaproperti/internal/receivable"
)

// Cetak laporan finansial ke HTML A4 siap-print, mengikuti pola yang sama
// dengan internal/billing/print.go (invoice/kwitansi): tidak ada library PDF —
// browser yang mencetak lewat window.print(), backend hanya merender HTML.

// ── format helpers ────────────────────────────────────────────────────────────

// rupiahStr memformat string desimal ("250000000.0000" atau "-250000000") ke
// "Rp 250.000.000". Manipulasi string murni — TIDAK PERNAH parse ke float64
// (uang tidak pakai float, lihat CLAUDE.md invariant #2), sama seperti
// billing.formatRupiah.
func rupiahStr(s string) string {
	if s == "" {
		return "Rp 0"
	}
	neg := strings.HasPrefix(s, "-")
	if neg {
		s = s[1:]
	}
	if dot := strings.Index(s, "."); dot >= 0 {
		s = s[:dot]
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return "Rp 0"
	}
	out := "Rp " + addPeriodSeparators(strconv.FormatInt(n, 10))
	if neg && n != 0 {
		out = "-" + out
	}
	return out
}

// rupiahMoney memformat domain.Money (dipakai TrialBalanceRow, dsb.) via
// String()-nya lalu rupiahStr — tetap tanpa float64.
func rupiahMoney(m domain.Money) string {
	return rupiahStr(m.String())
}

func addPeriodSeparators(s string) string {
	var result []byte
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			result = append(result, '.')
		}
		result = append(result, byte(c))
	}
	return string(result)
}

var idMonths = [...]string{
	"", "Januari", "Februari", "Maret", "April", "Mei", "Juni",
	"Juli", "Agustus", "September", "Oktober", "November", "Desember",
}

func fmtTanggal(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	return strconv.Itoa(t.Day()) + " " + idMonths[t.Month()] + " " + strconv.Itoa(t.Year())
}

func acctTypeLabel(t domain.AccountType) string {
	switch t {
	case domain.AccountAsset:
		return "Aset"
	case domain.AccountLiability:
		return "Kewajiban"
	case domain.AccountEquity:
		return "Ekuitas"
	case domain.AccountRevenue:
		return "Pendapatan"
	case domain.AccountExpense:
		return "Beban"
	default:
		return string(t)
	}
}

var printFuncs = template.FuncMap{
	"rupiah":      rupiahStr,
	"rupiahM":     rupiahMoney,
	"tanggal":     fmtTanggal,
	"acctType":    func(t domain.AccountType) string { return acctTypeLabel(t) },
	"now":         func() time.Time { return time.Now() },
	"logoB64":     func() template.URL { return template.URL(printkit.LogoBase64) },
	"sumberLabel": func(s receivable.Source) string { return sourceLabel(s) },
}

// ── shell (header/CSS/footer bersama semua laporan) ──────────────────────────
//
// Shell HTML+CSS-nya sendiri (warna brand, tipografi, chrome cetak) tinggal
// di internal/printkit — leaf package yang bisa diimpor internal/reporting
// maupun internal/ap tanpa membentuk import cycle. Lihat printkit.ShellData
// dan printkit.ReportShellHTML.

func renderPrintPage(w io.Writer, title, subtitle, companyName, bodyTplName, bodyTplStr string, data any) error {
	bodyTmpl, err := template.New(bodyTplName).Funcs(printFuncs).Parse(bodyTplStr)
	if err != nil {
		return fmt.Errorf("reporting: parse body template %s: %w", bodyTplName, err)
	}
	var buf bytes.Buffer
	if err := bodyTmpl.Execute(&buf, data); err != nil {
		return fmt.Errorf("reporting: render body template %s: %w", bodyTplName, err)
	}

	shellTmpl, err := template.New("shell").Funcs(printFuncs).Parse(printkit.ReportShellHTML)
	if err != nil {
		return err
	}
	return shellTmpl.Execute(w, printkit.ShellData{
		Title:       title,
		Subtitle:    subtitle,
		CompanyName: companyName,
		Body:        template.HTML(buf.String()), //nolint:gosec // buf dirender oleh html/template di atas — sudah escaped
	})
}

// ── Neraca (Balance Sheet) ────────────────────────────────────────────────────

const neracaBodyTpl = `
{{if .IsBalanced}}
<div class="rpt-banner ok">&#10003; Neraca seimbang</div>
{{else}}
<div class="rpt-banner bad">&#10007; PERINGATAN: Neraca tidak seimbang — ada jurnal yang belum balance</div>
{{end}}

<div class="rpt-pills">
  <div class="rpt-pill"><div class="pl">Total Aset</div><div class="pv">{{rupiah .TotalAset}}</div></div>
  <div class="rpt-pill"><div class="pl">Total Kewajiban</div><div class="pv">{{rupiah .TotalKewajiban}}</div></div>
  <div class="rpt-pill"><div class="pl">Total Ekuitas</div><div class="pv">{{rupiah .TotalEkuitasEfektif}}</div></div>
</div>

<div class="rpt-section-title">Aset</div>
<table class="rpt-table">
<thead><tr><th>Kode</th><th>Nama Akun</th><th class="num">Saldo</th></tr></thead>
<tbody>
{{range .Aset}}<tr><td class="rpt-code">{{.Code}}</td><td>{{.Name}}</td><td class="num">{{rupiah .Amount}}</td></tr>{{end}}
</tbody>
<tfoot><tr><td colspan="2">Total Aset</td><td class="num">{{rupiah .TotalAset}}</td></tr></tfoot>
</table>

<div class="rpt-section-title">Kewajiban</div>
<table class="rpt-table">
<thead><tr><th>Kode</th><th>Nama Akun</th><th class="num">Saldo</th></tr></thead>
<tbody>
{{range .Kewajiban}}<tr><td class="rpt-code">{{.Code}}</td><td>{{.Name}}</td><td class="num">{{rupiah .Amount}}</td></tr>{{end}}
</tbody>
<tfoot><tr><td colspan="2">Total Kewajiban</td><td class="num">{{rupiah .TotalKewajiban}}</td></tr></tfoot>
</table>

<div class="rpt-section-title">Ekuitas</div>
<table class="rpt-table">
<thead><tr><th>Kode</th><th>Nama Akun</th><th class="num">Saldo</th></tr></thead>
<tbody>
{{range .Ekuitas}}<tr><td class="rpt-code">{{.Code}}</td><td>{{.Name}}</td><td class="num">{{rupiah .Amount}}</td></tr>{{end}}
<tr><td></td><td><em>Laba/Rugi Tahun Berjalan</em></td><td class="num">{{rupiah .LabaRugiTahunBerjalan}}</td></tr>
</tbody>
<tfoot><tr><td colspan="2">Total Ekuitas</td><td class="num">{{rupiah .TotalEkuitasEfektif}}</td></tr></tfoot>
</table>

<table class="rpt-table">
<tfoot><tr><td>Total Kewajiban + Ekuitas</td><td class="num">{{rupiah .TotalKewajibanEkuitas}}</td></tr></tfoot>
</table>
`

// RenderNeracaPrint menulis HTML cetak Neraca ke w.
func RenderNeracaPrint(w io.Writer, companyName string, rpt *NeracaReport) error {
	subtitle := "Per " + fmtTanggal(rpt.AsOf)
	return renderPrintPage(w, "Neraca (Laporan Posisi Keuangan)", subtitle, companyName, "neraca", neracaBodyTpl, rpt)
}

// ── Laba Rugi (P&L) — dipakai untuk konsolidasi maupun per-proyek ────────────

const plBodyTpl = `
<div class="rpt-pills">
  <div class="rpt-pill"><div class="pl">Total Pendapatan</div><div class="pv">{{rupiah .TotalPendapatan}}</div></div>
  <div class="rpt-pill"><div class="pl">Laba Kotor</div><div class="pv">{{rupiah .LabaKotor}}</div></div>
  <div class="rpt-pill"><div class="pl">Laba Operasional</div><div class="pv">{{rupiah .LabaOperasional}}</div></div>
  <div class="rpt-pill"><div class="pl">Laba/Rugi Bersih</div><div class="pv">{{rupiah .LabaRugiBersih}}</div></div>
</div>

<div class="rpt-section-title">Pendapatan</div>
<table class="rpt-table">
<thead><tr><th>Kode</th><th>Nama Akun</th><th class="num">Jumlah</th></tr></thead>
<tbody>
{{range .Pendapatan}}<tr><td class="rpt-code">{{.Code}}</td><td>{{.Name}}</td><td class="num">{{rupiah .Amount}}</td></tr>{{end}}
</tbody>
<tfoot><tr><td colspan="2">Total Pendapatan</td><td class="num">{{rupiah .TotalPendapatan}}</td></tr></tfoot>
</table>

<div class="rpt-section-title">HPP (Harga Pokok Penjualan)</div>
<table class="rpt-table">
<thead><tr><th>Kode</th><th>Nama Akun</th><th class="num">Jumlah</th></tr></thead>
<tbody>
{{range .HPP}}<tr><td class="rpt-code">{{.Code}}</td><td>{{.Name}}</td><td class="num">{{rupiah .Amount}}</td></tr>{{end}}
</tbody>
<tfoot><tr><td colspan="2">Total HPP</td><td class="num">{{rupiah .TotalHPP}}</td></tr></tfoot>
</table>

<table class="rpt-table">
<tfoot><tr><td>Laba Kotor</td><td class="num">{{rupiah .LabaKotor}}</td></tr></tfoot>
</table>

<div class="rpt-section-title">Beban Operasional</div>
<table class="rpt-table">
<thead><tr><th>Kode</th><th>Nama Akun</th><th class="num">Jumlah</th></tr></thead>
<tbody>
{{range .BebanOperasional}}<tr><td class="rpt-code">{{.Code}}</td><td>{{.Name}}</td><td class="num">{{rupiah .Amount}}</td></tr>{{end}}
</tbody>
<tfoot><tr><td colspan="2">Total Beban Operasional</td><td class="num">{{rupiah .TotalBebanOperasional}}</td></tr></tfoot>
</table>

<table class="rpt-table">
<tfoot><tr><td>Laba Operasional</td><td class="num">{{rupiah .LabaOperasional}}</td></tr></tfoot>
</table>

<div class="rpt-section-title">Pendapatan Luar Usaha</div>
<table class="rpt-table">
<thead><tr><th>Kode</th><th>Nama Akun</th><th class="num">Jumlah</th></tr></thead>
<tbody>
{{range .PendapatanLuarUsaha}}<tr><td class="rpt-code">{{.Code}}</td><td>{{.Name}}</td><td class="num">{{rupiah .Amount}}</td></tr>{{end}}
</tbody>
<tfoot><tr><td colspan="2">Total Pendapatan Luar Usaha</td><td class="num">{{rupiah .TotalPendapatanLuarUsaha}}</td></tr></tfoot>
</table>

<div class="rpt-section-title">Beban Luar Usaha</div>
<table class="rpt-table">
<thead><tr><th>Kode</th><th>Nama Akun</th><th class="num">Jumlah</th></tr></thead>
<tbody>
{{range .BebanLuarUsaha}}<tr><td class="rpt-code">{{.Code}}</td><td>{{.Name}}</td><td class="num">{{rupiah .Amount}}</td></tr>{{end}}
</tbody>
<tfoot><tr><td colspan="2">Total Beban Luar Usaha</td><td class="num">{{rupiah .TotalBebanLuarUsaha}}</td></tr></tfoot>
</table>

<table class="rpt-table">
<tfoot><tr><td>Laba Bersih Sebelum Pajak</td><td class="num">{{rupiah .LabaBersihSebelumPajak}}</td></tr></tfoot>
</table>

<div class="rpt-section-title">Beban Pajak</div>
<table class="rpt-table">
<thead><tr><th>Kode</th><th>Nama Akun</th><th class="num">Jumlah</th></tr></thead>
<tbody>
{{range .BebanPajak}}<tr><td class="rpt-code">{{.Code}}</td><td>{{.Name}}</td><td class="num">{{rupiah .Amount}}</td></tr>{{end}}
</tbody>
<tfoot><tr><td colspan="2">Total Beban Pajak</td><td class="num">{{rupiah .TotalBebanPajak}}</td></tr></tfoot>
</table>

<table class="rpt-table">
<tfoot>
<tr><td><strong>Laba/Rugi Bersih Setelah Pajak</strong></td><td class="num"><strong>{{rupiah .LabaRugiBersih}}</strong></td></tr>
</tfoot>
</table>
`

// RenderPLPrint menulis HTML cetak Laba Rugi konsolidasi ke w.
func RenderPLPrint(w io.Writer, companyName string, rpt *PLReport) error {
	subtitle := "Kumulatif s.d. " + fmtTanggal(rpt.AsOf)
	return renderPrintPage(w, "Laporan Laba Rugi", subtitle, companyName, "pl", plBodyTpl, rpt)
}

// RenderProjectPLPrint menulis HTML cetak Laba Rugi per proyek ke w.
func RenderProjectPLPrint(w io.Writer, companyName, projectName string, rpt *PLReport) error {
	subtitle := "Proyek " + projectName + " — kumulatif s.d. " + fmtTanggal(rpt.AsOf)
	return renderPrintPage(w, "Laporan Laba Rugi Proyek", subtitle, companyName, "project-pl", plBodyTpl, rpt)
}

// ── Arus Kas (Cash Flow) ──────────────────────────────────────────────────────

const arusKasBodyTpl = `
<div class="rpt-pills">
  <div class="rpt-pill"><div class="pl">Kas Operasi</div><div class="pv">{{rupiah .Operasi.Net}}</div></div>
  <div class="rpt-pill"><div class="pl">Kas Investasi</div><div class="pv">{{rupiah .Investasi.Net}}</div></div>
  <div class="rpt-pill"><div class="pl">Kas Pendanaan</div><div class="pv">{{rupiah .Pendanaan.Net}}</div></div>
</div>

<div class="rpt-section-title">Aktivitas Operasi</div>
<table class="rpt-table">
<thead><tr><th>Deskripsi</th><th class="num">Jumlah</th></tr></thead>
<tbody>
{{range .Operasi.Lines}}<tr><td>{{.Description}}</td><td class="num">{{rupiah .Amount}}</td></tr>{{end}}
</tbody>
<tfoot><tr><td>Net Arus Kas Operasi</td><td class="num">{{rupiah .Operasi.Net}}</td></tr></tfoot>
</table>

<div class="rpt-section-title">Aktivitas Investasi</div>
<table class="rpt-table">
<thead><tr><th>Deskripsi</th><th class="num">Jumlah</th></tr></thead>
<tbody>
{{range .Investasi.Lines}}<tr><td>{{.Description}}</td><td class="num">{{rupiah .Amount}}</td></tr>{{end}}
</tbody>
<tfoot><tr><td>Net Arus Kas Investasi</td><td class="num">{{rupiah .Investasi.Net}}</td></tr></tfoot>
</table>

<div class="rpt-section-title">Aktivitas Pendanaan</div>
<table class="rpt-table">
<thead><tr><th>Deskripsi</th><th class="num">Jumlah</th></tr></thead>
<tbody>
{{range .Pendanaan.Lines}}<tr><td>{{.Description}}</td><td class="num">{{rupiah .Amount}}</td></tr>{{end}}
</tbody>
<tfoot><tr><td>Net Arus Kas Pendanaan</td><td class="num">{{rupiah .Pendanaan.Net}}</td></tr></tfoot>
</table>

<table class="rpt-table">
<tfoot><tr><td>Perubahan Kas Bersih</td><td class="num">{{rupiah .NetChange}}</td></tr></tfoot>
</table>
`

// RenderArusKasPrint menulis HTML cetak Arus Kas ke w.
func RenderArusKasPrint(w io.Writer, companyName string, rpt *ArusKasReport) error {
	subtitle := fmtTanggal(rpt.PeriodFrom) + " – " + fmtTanggal(rpt.PeriodTo)
	return renderPrintPage(w, "Laporan Arus Kas", subtitle, companyName, "arus-kas", arusKasBodyTpl, rpt)
}

// ── Neraca Saldo (Trial Balance) ──────────────────────────────────────────────

const trialBalanceBodyTpl = `
<table class="rpt-table">
<thead><tr><th>Kode</th><th>Nama Akun</th><th>Tipe</th><th class="num">Debit</th><th class="num">Kredit</th><th class="num">Saldo</th></tr></thead>
<tbody>
{{range .Rows}}<tr><td class="rpt-code">{{.AccountCode}}</td><td>{{.AccountName}}</td><td>{{acctType .AccountType}}</td><td class="num">{{rupiahM .TotalDebit}}</td><td class="num">{{rupiahM .TotalCredit}}</td><td class="num">{{rupiahM .Balance}}</td></tr>{{end}}
</tbody>
<tfoot><tr><td colspan="3">TOTAL</td><td class="num">{{rupiahM .TotalDebit}}</td><td class="num">{{rupiahM .TotalCredit}}</td><td></td></tr></tfoot>
</table>
`

// RenderTrialBalancePrint menulis HTML cetak Neraca Saldo ke w.
func RenderTrialBalancePrint(w io.Writer, companyName string, tb *ledger.TrialBalance) error {
	subtitle := "Per " + fmtTanggal(tb.AsOf)
	return renderPrintPage(w, "Neraca Saldo (Trial Balance)", subtitle, companyName, "trial-balance", trialBalanceBodyTpl, tb)
}

// ── Pajak (Tax Liability) ─────────────────────────────────────────────────────

const pajakBodyTpl = `
<div class="rpt-pills">
  <div class="rpt-pill"><div class="pl">Total Kewajiban</div><div class="pv">{{rupiah .TotalObligation}}</div></div>
  <div class="rpt-pill"><div class="pl">Total Dibayar</div><div class="pv">{{rupiah .TotalPaid}}</div></div>
  <div class="rpt-pill"><div class="pl">Belum Dibayar</div><div class="pv">{{rupiah .TotalOutstanding}}</div></div>
</div>
<table class="rpt-table">
<thead><tr><th>Kode Tarif</th><th class="num">Nilai Transfer</th><th>Tarif</th><th class="num">Pajak</th><th>Status</th><th>Tanggal</th></tr></thead>
<tbody>
{{range .Items}}<tr><td>{{.RateCode}}</td><td class="num">{{rupiah .TransferValue}}</td><td>{{.Rate}}</td><td class="num">{{rupiah .TaxAmount}}</td><td>{{.Status}}</td><td>{{.AccrualDate}}</td></tr>{{end}}
</tbody>
<tfoot><tr><td colspan="3">Total Kewajiban</td><td class="num">{{rupiah .TotalObligation}}</td><td colspan="2"></td></tr></tfoot>
</table>
`

// RenderTaxLiabilityPrint menulis HTML cetak Laporan Kewajiban Pajak ke w.
func RenderTaxLiabilityPrint(w io.Writer, companyName string, rpt *TaxLiabilityReport) error {
	subtitle := rpt.PeriodFrom + " – " + rpt.PeriodTo
	return renderPrintPage(w, "Laporan Kewajiban Pajak", subtitle, companyName, "pajak", pajakBodyTpl, rpt)
}

// ── Pipeline Penjualan ────────────────────────────────────────────────────────

const pipelineBodyTpl = `
<div class="rpt-pills">
  <div class="rpt-pill"><div class="pl">Total Unit</div><div class="pv">{{.TotalUnits}}</div></div>
  <div class="rpt-pill"><div class="pl">Proyeksi Pendapatan</div><div class="pv">{{rupiah .ProjectedRevenue}}</div></div>
</div>
<table class="rpt-table">
<thead><tr><th>Status</th><th class="num">Jumlah Unit</th><th class="num">Total List Price</th><th class="num">Uang Muka</th><th class="num">Nilai Kontrak</th></tr></thead>
<tbody>
<tr><td>Tersedia</td><td class="num">{{.Available.Count}}</td><td class="num">{{rupiah .Available.TotalListPrice}}</td><td class="num">—</td><td class="num">—</td></tr>
<tr><td>Reserved</td><td class="num">{{.Reserved.Count}}</td><td class="num">{{rupiah .Reserved.TotalListPrice}}</td><td class="num">{{rupiah .Reserved.TotalAdvance}}</td><td class="num">—</td></tr>
<tr><td>Terjual</td><td class="num">{{.Sold.Count}}</td><td class="num">—</td><td class="num">—</td><td class="num">{{rupiah .Sold.TotalContractValue}}</td></tr>
</tbody>
</table>
`

// RenderPipelinePrint menulis HTML cetak Pipeline Penjualan ke w.
func RenderPipelinePrint(w io.Writer, companyName string, rpt *PipelineReport) error {
	subtitle := "Per " + fmtTanggal(time.Now())
	return renderPrintPage(w, "Pipeline Penjualan", subtitle, companyName, "pipeline", pipelineBodyTpl, rpt)
}

// ── Buku Besar (General Ledger) ───────────────────────────────────────────────

const generalLedgerBodyTpl = `
<div class="rpt-pills">
  <div class="rpt-pill"><div class="pl">Akun</div><div class="pv">{{.AccountCode}} — {{.AccountName}}</div></div>
  <div class="rpt-pill"><div class="pl">Saldo Akhir</div><div class="pv">{{rupiahM .Closing}}</div></div>
</div>
<table class="rpt-table">
<thead><tr><th>Tanggal</th><th>Deskripsi</th><th>Referensi</th><th class="num">Debit</th><th class="num">Kredit</th><th class="num">Saldo</th></tr></thead>
<tbody>
{{range .Entries}}<tr><td>{{tanggal .Date}}</td><td>{{.Description}}</td><td class="rpt-code">{{.Reference}}</td><td class="num">{{rupiahM .Debit}}</td><td class="num">{{rupiahM .Credit}}</td><td class="num">{{rupiahM .Balance}}</td></tr>{{end}}
</tbody>
</table>
`

type generalLedgerPrintData struct {
	AccountCode string
	AccountName string
	Entries     []ledger.LedgerEntry
	Closing     domain.Money
}

// RenderGeneralLedgerPrint menulis HTML cetak Buku Besar (satu akun, dengan
// saldo berjalan) ke w — sumber data SAMA dengan endpoint JSON/CSV
// (GetGeneralLedger), tidak menghitung ulang.
func RenderGeneralLedgerPrint(w io.Writer, companyName, accountCode, accountName string, entries []ledger.LedgerEntry, from, to time.Time) error {
	var closing domain.Money
	if len(entries) > 0 {
		closing = entries[len(entries)-1].Balance
	}
	subtitle := accountCode + " — " + accountName + " · " + fmtTanggal(from) + " – " + fmtTanggal(to)
	data := generalLedgerPrintData{AccountCode: accountCode, AccountName: accountName, Entries: entries, Closing: closing}
	return renderPrintPage(w, "Buku Besar", subtitle, companyName, "general-ledger", generalLedgerBodyTpl, data)
}

// ── Piutang Customer (AR Aging) ───────────────────────────────────────────────

const arAgingBodyTpl = `
<div class="rpt-pills">
  <div class="rpt-pill"><div class="pl">Total Piutang</div><div class="pv">{{rupiah .TotalPiutang}}</div></div>
  <div class="rpt-pill"><div class="pl">Belum Jatuh Tempo</div><div class="pv">{{rupiah .CurrentDue}}</div></div>
  <div class="rpt-pill"><div class="pl">Jatuh Tempo</div><div class="pv">{{rupiah .Overdue}}</div></div>
  <div class="rpt-pill"><div class="pl">Jatuh Tempo Minggu Ini</div><div class="pv">{{rupiah .DueThisWeek}}</div></div>
</div>
<table class="rpt-table">
<thead><tr><th>Sumber</th><th>Pelanggan</th><th>Unit</th><th>Invoice</th><th>Keterangan</th><th>Jatuh Tempo</th><th class="num">Nominal</th><th class="num">Dibayar</th><th class="num">Outstanding</th><th>Status</th></tr></thead>
<tbody>
{{range .Rows}}<tr><td>{{sumberLabel .Source}}</td><td>{{.BuyerName}}</td><td>{{.UnitCode}}</td><td class="rpt-code">{{.InvoiceNumber}}</td><td>{{.Label}}</td><td>{{.DueDate}}</td><td class="num">{{rupiah .Amount}}</td><td class="num">{{rupiah .Paid}}</td><td class="num">{{rupiah .Outstanding}}</td><td>{{.Status}}</td></tr>{{end}}
</tbody>
<tfoot><tr><td colspan="8">Total Piutang</td><td class="num">{{rupiah .TotalPiutang}}</td><td></td></tr></tfoot>
</table>
`

// RenderARAgingPrint menulis HTML cetak Laporan Piutang Customer (aging) ke w.
func RenderARAgingPrint(w io.Writer, companyName string, rpt *ARAgingReport) error {
	subtitle := "Per " + rpt.AsOf
	return renderPrintPage(w, "Laporan Piutang Customer", subtitle, companyName, "ar-aging", arAgingBodyTpl, rpt)
}

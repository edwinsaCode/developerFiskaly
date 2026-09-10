package cost

// Cetak Riwayat Biaya (daftar) dan Bukti Kas Keluar (satu transaksi) ke HTML
// A4 siap-cetak — pola SAMA dengan internal/ap/print.go dan
// internal/ap/receipt_print.go (tidak ada library PDF, browser yang mencetak
// lewat window.print()). Sumber data SAMA dengan endpoint JSON
// (Service.ListExpenses/GetExpense) — lembar ini tidak menghitung ulang apa
// pun, murni representasi baca.

import (
	"bytes"
	"fmt"
	"html/template"
	"io"
	"strconv"
	"strings"
	"time"

	"esaproperti/internal/domain"
	"esaproperti/internal/printkit"
)

// costCategoryLabels — SAMA dengan CATEGORY_LABELS di
// frontend/components/biaya/CostEntryTable.tsx. Hanya dipakai saat baris tidak
// punya nama jenis pengeluaran (mis. biaya proyek langsung tanpa master
// operasional).
var costCategoryLabels = map[string]string{
	"land":        "Tanah",
	"hard":        "Hard Cost",
	"soft":        "Soft Cost",
	"operational": "Operasional", // dahulu "financing"/Pendanaan — RULE KLIEN FREEZE 2026-09-04
	"marketing":   "Pemasaran",
	"other":       "Biaya Lain-lain",
}

var costPaymentLabels = map[string]string{
	"bank":    "Bank/Kas",
	"payable": "Hutang Usaha",
}

func categoryLabelOf(it *ExpenseListItem) string {
	if it.ExpenseTypeName != "" {
		return it.ExpenseTypeName
	}
	if lbl, ok := costCategoryLabels[it.Category]; ok {
		return lbl
	}
	return it.Category
}

func paymentLabelOf(method string) string {
	if lbl, ok := costPaymentLabels[method]; ok {
		return lbl
	}
	return method
}

func rupiahStrCost(v string) string {
	if v == "" {
		return "Rp 0"
	}
	neg := strings.HasPrefix(v, "-")
	if neg {
		v = v[1:]
	}
	if dot := strings.Index(v, "."); dot >= 0 {
		v = v[:dot]
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return "Rp 0"
	}
	var out []byte
	digits := strconv.FormatInt(n, 10)
	for i, c := range digits {
		if i > 0 && (len(digits)-i)%3 == 0 {
			out = append(out, '.')
		}
		out = append(out, byte(c))
	}
	res := "Rp " + string(out)
	if neg && n != 0 {
		res = "-" + res
	}
	return res
}

func rupiahMoneyCost(m domain.Money) string { return rupiahStrCost(m.String()) }

var idMonthsCost = [...]string{
	"", "Januari", "Februari", "Maret", "April", "Mei", "Juni",
	"Juli", "Agustus", "September", "Oktober", "November", "Desember",
}

func fmtTanggalCost(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	return strconv.Itoa(t.Day()) + " " + idMonthsCost[t.Month()] + " " + strconv.Itoa(t.Year())
}

// ── Terbilang (angka → kata bahasa Indonesia) ────────────────────────────────
//
// Duplikat dari internal/ap/receipt_print.go: domain package tidak saling
// impor (lihat CLAUDE.md aturan import), jadi setiap paket cetak memelihara
// salinannya sendiri.

var terbilangUnitsCost = [...]string{
	"", "satu", "dua", "tiga", "empat", "lima",
	"enam", "tujuh", "delapan", "sembilan", "sepuluh", "sebelas",
}

func terbilangCost(n int64) string {
	switch {
	case n < 0:
		return "minus " + terbilangCost(-n)
	case n < 12:
		return terbilangUnitsCost[n]
	case n < 20:
		return terbilangCost(n-10) + " belas"
	case n < 100:
		return terbilangCost(n/10) + " puluh" + spaceWordCost(terbilangCost(n%10))
	case n < 200:
		return "seratus" + spaceWordCost(terbilangCost(n-100))
	case n < 1000:
		return terbilangCost(n/100) + " ratus" + spaceWordCost(terbilangCost(n%100))
	case n < 2000:
		return "seribu" + spaceWordCost(terbilangCost(n-1000))
	case n < 1_000_000:
		return terbilangCost(n/1000) + " ribu" + spaceWordCost(terbilangCost(n%1000))
	case n < 1_000_000_000:
		return terbilangCost(n/1_000_000) + " juta" + spaceWordCost(terbilangCost(n%1_000_000))
	case n < 1_000_000_000_000:
		return terbilangCost(n/1_000_000_000) + " miliar" + spaceWordCost(terbilangCost(n%1_000_000_000))
	default:
		return terbilangCost(n/1_000_000_000_000) + " triliun" + spaceWordCost(terbilangCost(n%1_000_000_000_000))
	}
}

func spaceWordCost(s string) string {
	if s == "" {
		return ""
	}
	return " " + s
}

func terbilangRupiahCost(m domain.Money) string {
	s := m.String()
	neg := strings.HasPrefix(s, "-")
	if neg {
		s = s[1:]
	}
	if dot := strings.Index(s, "."); dot >= 0 {
		s = s[:dot]
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return "Nol"
	}
	word := terbilangCost(n)
	if word == "" {
		word = "nol"
	}
	word = strings.ToUpper(word[:1]) + word[1:]
	if neg {
		word = "Minus " + word
	}
	return word
}

// ── Daftar (Riwayat Biaya) ────────────────────────────────────────────────────

var costListFuncs = template.FuncMap{
	"rupiah":  rupiahStrCost,
	"rupiahM": rupiahMoneyCost,
	"tanggal": fmtTanggalCost,
	"now":     func() time.Time { return time.Now() },
	"logoB64": func() template.URL { return template.URL(printkit.LogoBase64) },
}

type costHistoryPrintRow struct {
	Date           time.Time
	CategoryLabel  string
	Vendor         string
	Description    string
	DocumentNumber string
	PaymentLabel   string
	Amount         domain.Money
}

type costHistoryPrintData struct {
	Rows  []costHistoryPrintRow
	Count int
	Total domain.Money
}

const costHistoryListBodyTpl = `
<div class="rpt-pills">
  <div class="rpt-pill"><div class="pl">Jumlah Entri</div><div class="pv">{{.Count}}</div></div>
  <div class="rpt-pill"><div class="pl">Total Biaya</div><div class="pv">{{rupiahM .Total}}</div></div>
</div>
<table class="rpt-table">
<thead><tr><th>Tanggal</th><th>Kategori</th><th>Vendor</th><th>Deskripsi</th><th>No. Dokumen</th><th>Metode</th><th class="num">Jumlah</th></tr></thead>
<tbody>
{{range .Rows}}<tr><td>{{tanggal .Date}}</td><td>{{.CategoryLabel}}</td><td>{{.Vendor}}</td><td>{{.Description}}</td><td class="rpt-code">{{.DocumentNumber}}</td><td>{{.PaymentLabel}}</td><td class="num">{{rupiahM .Amount}}</td></tr>{{end}}
</tbody>
<tfoot><tr><td colspan="6">Total</td><td class="num">{{rupiahM .Total}}</td></tr></tfoot>
</table>
`

// RenderCostHistoryPrint menulis HTML cetak Riwayat Biaya satu proyek ke w.
func RenderCostHistoryPrint(w io.Writer, companyName, projectName string, items []*ExpenseListItem) error {
	data := costHistoryPrintData{}
	for _, it := range items {
		data.Rows = append(data.Rows, costHistoryPrintRow{
			Date:           it.Date,
			CategoryLabel:  categoryLabelOf(it),
			Vendor:         it.Vendor,
			Description:    it.Description,
			DocumentNumber: it.DocumentNumber,
			PaymentLabel:   paymentLabelOf(it.PaymentMethod),
			Amount:         it.Amount,
		})
		data.Total = data.Total.Add(it.Amount)
	}
	data.Count = len(data.Rows)

	bodyTmpl, err := template.New("cost-history").Funcs(costListFuncs).Parse(costHistoryListBodyTpl)
	if err != nil {
		return fmt.Errorf("cost: parse body template cost-history: %w", err)
	}
	var buf bytes.Buffer
	if err := bodyTmpl.Execute(&buf, data); err != nil {
		return fmt.Errorf("cost: render body template cost-history: %w", err)
	}

	shellTmpl, err := template.New("shell").Funcs(costListFuncs).Parse(printkit.ReportShellHTML)
	if err != nil {
		return err
	}
	subtitle := "Riwayat Biaya"
	if projectName != "" {
		subtitle = projectName + " — Riwayat Biaya"
	}
	return shellTmpl.Execute(w, printkit.ShellData{
		Title:       "Riwayat Biaya Proyek",
		Subtitle:    subtitle,
		CompanyName: companyName,
		Body:        template.HTML(buf.String()), //nolint:gosec // buf dirender html/template di atas — sudah escaped
	})
}

// ── Satu transaksi (Bukti Kas Keluar) ────────────────────────────────────────

var costReceiptFuncs = template.FuncMap{
	"rupiah":    rupiahStrCost,
	"rupiahM":   rupiahMoneyCost,
	"tanggal":   fmtTanggalCost,
	"terbilang": terbilangRupiahCost,
	"now":       func() time.Time { return time.Now() },
	"logoB64":   func() template.URL { return template.URL(printkit.LogoBase64) },
}

// CostReceiptData adalah bukti kas keluar satu transaksi pengeluaran
// sebagaimana dicetak — dibangun dari ExpenseListItem yang sama dengan
// GET /expenses/{id}, tidak ada field yang dihitung ulang.
type CostReceiptData struct {
	DocumentNumber  string
	Date            time.Time
	Amount          domain.Money
	BankAccountCode string
	BankAccountName string
	CategoryLabel   string
	Vendor          string
	Description     string
	CompanyName     string
	IsReversed      bool
}

const costReceiptBodyTpl = `
<div class="rpt-pills">
  <div class="rpt-pill"><div class="pl">No. Dokumen</div><div class="pv">{{.DocumentNumber}}</div></div>
  <div class="rpt-pill"><div class="pl">Tanggal</div><div class="pv">{{tanggal .Date}}</div></div>
  <div class="rpt-pill"><div class="pl">Jumlah</div><div class="pv">{{rupiahM .Amount}}</div></div>
</div>
{{if .IsReversed}}<div class="rpt-flag">DIBATALKAN — transaksi ini sudah dibalik lewat jurnal pembalik</div>{{end}}
<table class="rpt-table">
<tbody>
<tr><td class="rpt-label">Dibayarkan kepada</td><td>{{.Vendor}}</td></tr>
<tr><td class="rpt-label">Jenis Biaya</td><td>{{.CategoryLabel}}</td></tr>
<tr><td class="rpt-label">Akun Kas/Bank</td><td class="rpt-code">{{.BankAccountCode}}{{if .BankAccountName}} — {{.BankAccountName}}{{end}}</td></tr>
<tr><td class="rpt-label">Keterangan</td><td>{{.Description}}</td></tr>
</tbody>
</table>
<div class="rpt-terbilang">Terbilang: <em>{{terbilang .Amount}} rupiah</em></div>
<div class="rpt-sig-row">
  <div class="rpt-sig-block"><div class="rpt-sig-line"></div><div>Dibayar oleh</div></div>
  <div class="rpt-sig-block"><div class="rpt-sig-line"></div><div>Diterima oleh</div></div>
</div>
`

// receiptExtraCSSCost ditambahkan di atas printkit.ReportShellHTML: baris
// label/nilai, blok terbilang, dan tanda tangan — elemen yang tidak dipakai
// laporan tabular (Riwayat Biaya) tetapi dibutuhkan bukti kas keluar. Duplikat
// dari internal/ap/receipt_print.go (package tidak saling impor).
const receiptExtraCSSCost = `
.rpt-label { font-weight: 600; width: 45mm; color: #6E635A; vertical-align: top; }
.rpt-flag { background: #FEF2F2; color: #991B1B; border: 1px solid #991B1B; border-radius: 6px;
  padding: 3mm 5mm; font-weight: 700; font-size: 0.8rem; margin-bottom: 5mm; }
.rpt-terbilang { border: 1.2px solid #EAE5E0; border-radius: 6px; padding: 3mm 5mm;
  font-style: italic; font-weight: 600; margin-bottom: 8mm; }
.rpt-sig-row { display: flex; justify-content: space-between; margin-top: 12mm; padding: 0 10mm; }
.rpt-sig-block { text-align: center; font-size: 0.82rem; }
.rpt-sig-line { width: 45mm; border-top: 1px solid #211812; margin-bottom: 2mm; margin-top: 16mm; }
`

// RenderCostReceiptPrint menulis HTML cetak Bukti Kas Keluar satu transaksi
// pengeluaran ke w.
func RenderCostReceiptPrint(w io.Writer, data *CostReceiptData) error {
	bodyTmpl, err := template.New("cost-receipt").Funcs(costReceiptFuncs).Parse(costReceiptBodyTpl)
	if err != nil {
		return fmt.Errorf("cost: parse body template cost-receipt: %w", err)
	}
	var buf bytes.Buffer
	if err := bodyTmpl.Execute(&buf, data); err != nil {
		return fmt.Errorf("cost: render body template cost-receipt: %w", err)
	}

	shellTmpl, err := template.New("shell").Funcs(costReceiptFuncs).Parse(
		strings.Replace(printkit.ReportShellHTML, "</style>", receiptExtraCSSCost+"</style>", 1),
	)
	if err != nil {
		return err
	}
	return shellTmpl.Execute(w, printkit.ShellData{
		Title:       "Bukti Kas Keluar",
		Subtitle:    data.DocumentNumber,
		CompanyName: data.CompanyName,
		Body:        template.HTML(buf.String()), //nolint:gosec // buf dirender html/template di atas — sudah escaped
	})
}

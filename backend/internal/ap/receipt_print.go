package ap

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

// Cetak Bukti Kas Keluar (BKK) satu pembayaran vendor — representasi BACA dari
// ap_payments, bukan dokumen baru. Nomor, tanggal, dan jumlahnya SAMA dengan
// yang sudah ditautkan ke jurnal saat RecordPayment memposting (INV-DOC-1);
// lembar ini tidak pernah menghitung ulang apa pun.

func kindLabel(k PaymentKind) string {
	switch k {
	case PaymentKindInvoice:
		return "Pelunasan Tagihan"
	case PaymentKindAdvance:
		return "Uang Muka Vendor"
	case PaymentKindRetention:
		return "Pelepasan Retensi"
	default:
		return string(k)
	}
}

// PaymentReceiptData adalah bukti kas keluar sebagaimana dicetak.
type PaymentReceiptData struct {
	DocumentNumber  string
	PaymentDate     time.Time
	Amount          domain.Money
	CashAccountCode string
	Kind            PaymentKind
	Description     string
	CompanyName     string
	VendorName      string
	VendorAddress   string
	VendorNPWP      string
	Allocations     []AllocationView
	IsReversed      bool
}

var apReceiptFuncs = template.FuncMap{
	"rupiah":     rupiahStrAP,
	"rupiahM":    rupiahMoneyAP,
	"tanggal":    fmtTanggalAP,
	"tanggalISO": fmtTanggalISOAp,
	"kindLabel":  kindLabel,
	"terbilang":  terbilangRupiahAP,
	"now":        func() time.Time { return time.Now() },
	"logoB64":    func() template.URL { return template.URL(printkit.LogoBase64) },
}

// fmtTanggalISOAp memformat tanggal ISO ("2006-01-02") yang sudah diubah
// jadi string di AllocationView — nilainya sendiri dibaca-apa-adanya kalau
// gagal diuraikan, supaya cetakan tidak pernah kosong karena format tak
// terduga.
func fmtTanggalISOAp(s string) string {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return s
	}
	return fmtTanggalAP(t)
}

const paymentReceiptBodyTpl = `
<div class="rpt-pills">
  <div class="rpt-pill"><div class="pl">No. Dokumen</div><div class="pv">{{.DocumentNumber}}</div></div>
  <div class="rpt-pill"><div class="pl">Tanggal</div><div class="pv">{{tanggal .PaymentDate}}</div></div>
  <div class="rpt-pill"><div class="pl">Jumlah</div><div class="pv">{{rupiahM .Amount}}</div></div>
</div>
{{if .IsReversed}}<div class="rpt-flag">DIBATALKAN — pembayaran ini sudah dibalik lewat jurnal pembalik</div>{{end}}
<table class="rpt-table">
<tbody>
<tr><td class="rpt-label">Dibayarkan kepada</td><td>{{.VendorName}}</td></tr>
{{if .VendorNPWP}}<tr><td class="rpt-label">NPWP</td><td>{{.VendorNPWP}}</td></tr>{{end}}
{{if .VendorAddress}}<tr><td class="rpt-label">Alamat</td><td>{{.VendorAddress}}</td></tr>{{end}}
<tr><td class="rpt-label">Jenis Pembayaran</td><td>{{kindLabel .Kind}}</td></tr>
<tr><td class="rpt-label">Akun Kas/Bank</td><td class="rpt-code">{{.CashAccountCode}}</td></tr>
<tr><td class="rpt-label">Keterangan</td><td>{{.Description}}</td></tr>
</tbody>
</table>
<div class="rpt-terbilang">Terbilang: <em>{{terbilang .Amount}} rupiah</em></div>
{{if .Allocations}}
<table class="rpt-table">
<thead><tr><th>No. Tagihan</th><th>Jatuh Tempo</th><th class="num">Nilai Dialokasikan</th></tr></thead>
<tbody>
{{range .Allocations}}<tr><td class="rpt-code">{{.InvoiceNumber}}</td><td>{{tanggalISO .DueDate}}</td><td class="num">{{rupiah .Amount}}</td></tr>{{end}}
</tbody>
</table>
{{end}}
<div class="rpt-sig-row">
  <div class="rpt-sig-block"><div class="rpt-sig-line"></div><div>Dibayar oleh</div></div>
  <div class="rpt-sig-block"><div class="rpt-sig-line"></div><div>Diterima oleh</div></div>
</div>
`

// receiptExtraCSS ditambahkan di atas apReportShellHTML: baris label/nilai,
// blok terbilang, dan tanda tangan — elemen yang tidak dipakai laporan
// tabular (Hutang Usaha) tetapi dibutuhkan bukti kas keluar.
const receiptExtraCSS = `
.rpt-label { font-weight: 600; width: 45mm; color: #6E635A; vertical-align: top; }
.rpt-flag { background: #FEF2F2; color: #991B1B; border: 1px solid #991B1B; border-radius: 6px;
  padding: 3mm 5mm; font-weight: 700; font-size: 0.8rem; margin-bottom: 5mm; }
.rpt-terbilang { border: 1.2px solid #EAE5E0; border-radius: 6px; padding: 3mm 5mm;
  font-style: italic; font-weight: 600; margin-bottom: 8mm; }
.rpt-sig-row { display: flex; justify-content: space-between; margin-top: 12mm; padding: 0 10mm; }
.rpt-sig-block { text-align: center; font-size: 0.82rem; }
.rpt-sig-line { width: 45mm; border-top: 1px solid #211812; margin-bottom: 2mm; margin-top: 16mm; }
`

// RenderPaymentReceiptPrint menulis HTML cetak Bukti Kas Keluar ke w.
func RenderPaymentReceiptPrint(w io.Writer, data *PaymentReceiptData) error {
	bodyTmpl, err := template.New("ap-payment-receipt").Funcs(apReceiptFuncs).Parse(paymentReceiptBodyTpl)
	if err != nil {
		return fmt.Errorf("ap: parse body template ap-payment-receipt: %w", err)
	}
	var buf bytes.Buffer
	if err := bodyTmpl.Execute(&buf, data); err != nil {
		return fmt.Errorf("ap: render body template ap-payment-receipt: %w", err)
	}

	shellTmpl, err := template.New("shell").Funcs(apReceiptFuncs).Parse(
		strings.Replace(printkit.ReportShellHTML, "</style>", receiptExtraCSS+"</style>", 1),
	)
	if err != nil {
		return err
	}
	title := "Bukti Kas Keluar"
	return shellTmpl.Execute(w, printkit.ShellData{
		Title:       title,
		Subtitle:    data.DocumentNumber,
		CompanyName: data.CompanyName,
		Body:        template.HTML(buf.String()), //nolint:gosec // buf dirender html/template di atas — sudah escaped
	})
}

// ── Terbilang (angka → kata bahasa Indonesia) ─────────────────────────────────
//
// Duplikat dari internal/billing/receipt_print.go: domain package tidak
// saling impor (lihat CLAUDE.md aturan import), jadi setiap paket cetak
// memelihara salinannya sendiri.

var terbilangUnitsAP = [...]string{
	"", "satu", "dua", "tiga", "empat", "lima",
	"enam", "tujuh", "delapan", "sembilan", "sepuluh", "sebelas",
}

func terbilangAP(n int64) string {
	switch {
	case n < 0:
		return "minus " + terbilangAP(-n)
	case n < 12:
		return terbilangUnitsAP[n]
	case n < 20:
		return terbilangAP(n-10) + " belas"
	case n < 100:
		return terbilangAP(n/10) + " puluh" + spaceWordAP(terbilangAP(n%10))
	case n < 200:
		return "seratus" + spaceWordAP(terbilangAP(n-100))
	case n < 1000:
		return terbilangAP(n/100) + " ratus" + spaceWordAP(terbilangAP(n%100))
	case n < 2000:
		return "seribu" + spaceWordAP(terbilangAP(n-1000))
	case n < 1_000_000:
		return terbilangAP(n/1000) + " ribu" + spaceWordAP(terbilangAP(n%1000))
	case n < 1_000_000_000:
		return terbilangAP(n/1_000_000) + " juta" + spaceWordAP(terbilangAP(n%1_000_000))
	case n < 1_000_000_000_000:
		return terbilangAP(n/1_000_000_000) + " miliar" + spaceWordAP(terbilangAP(n%1_000_000_000))
	default:
		return terbilangAP(n/1_000_000_000_000) + " triliun" + spaceWordAP(terbilangAP(n%1_000_000_000_000))
	}
}

func spaceWordAP(s string) string {
	if s == "" {
		return ""
	}
	return " " + s
}

// terbilangRupiahAP memformat Money menjadi kata bahasa Indonesia (kapital
// awal). Bagian desimal diabaikan — nominal selalu rupiah bulat (Invariant #2).
func terbilangRupiahAP(m domain.Money) string {
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
	word := terbilangAP(n)
	if word == "" {
		word = "nol"
	}
	word = strings.ToUpper(word[:1]) + word[1:]
	if neg {
		word = "Minus " + word
	}
	return word
}

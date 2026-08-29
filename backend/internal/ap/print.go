package ap

import (
	"bytes"
	"context"
	"fmt"
	"html/template"
	"io"
	"strconv"
	"strings"
	"time"

	"esaproperti/internal/domain"
	"esaproperti/internal/printkit"
)

// Cetak Laporan Hutang Usaha ke HTML A4 siap-print — pola SAMA dengan
// internal/reporting/print.go dan internal/billing/print.go (tidak ada
// library PDF, browser yang mencetak lewat window.print()). Data dari
// s.ListInvoices, sumber yang SAMA dengan endpoint JSON/UI — tidak
// menghitung ulang apa pun.

// TenantName mengambil nama tenant untuk header cetak. Setiap paket domain
// query tenant-nya sendiri (lihat billing.LoadPrintData) — tidak ada tabel
// company_name terpisah untuk dijadikan sumber bersama.
func (s *Service) TenantName(ctx context.Context, tenantID uint64) (string, error) {
	var name string
	err := s.db.WithContext(ctx).Raw("SELECT name FROM tenants WHERE id = ?", tenantID).Scan(&name).Error
	return name, err
}

func rupiahStrAP(v string) string {
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

func rupiahMoneyAP(m domain.Money) string { return rupiahStrAP(m.String()) }

var idMonthsAP = [...]string{
	"", "Januari", "Februari", "Maret", "April", "Mei", "Juni",
	"Juli", "Agustus", "September", "Oktober", "November", "Desember",
}

func fmtTanggalAP(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	return strconv.Itoa(t.Day()) + " " + idMonthsAP[t.Month()] + " " + strconv.Itoa(t.Year())
}

func paymentStatusLabel(s PaymentStatus) string {
	switch s {
	case PayStatusPaid:
		return "Lunas"
	case PayStatusPartial:
		return "Sebagian"
	case PayStatusUnpaid:
		return "Belum Dibayar"
	case PayStatusReversed:
		return "Dibalik"
	default:
		return string(s)
	}
}

var apPrintFuncs = template.FuncMap{
	"rupiah":  rupiahStrAP,
	"rupiahM": rupiahMoneyAP,
	"tanggal": fmtTanggalAP,
	"payStat": paymentStatusLabel,
	"now":     func() time.Time { return time.Now() },
	"logoB64": func() template.URL { return template.URL(printkit.LogoBase64) },
}

type apInvoicePrintRow struct {
	VendorName    string
	InvoiceNumber string
	InvoiceDate   time.Time
	DueDate       time.Time
	Payable       domain.Money
	Paid          string
	Outstanding   string
	Status        PaymentStatus
}

type apInvoicePrintData struct {
	Rows            []apInvoicePrintRow
	TotalPayable    domain.Money
	TotalOutstanding domain.Money
}

const apInvoiceListBodyTpl = `
<div class="rpt-pills">
  <div class="rpt-pill"><div class="pl">Total Tagihan</div><div class="pv">{{rupiahM .TotalPayable}}</div></div>
  <div class="rpt-pill"><div class="pl">Total Outstanding</div><div class="pv">{{rupiahM .TotalOutstanding}}</div></div>
</div>
<table class="rpt-table">
<thead><tr><th>Vendor</th><th>No. Tagihan</th><th>Tgl Tagihan</th><th>Jatuh Tempo</th><th class="num">Nilai Tagihan</th><th class="num">Dibayar</th><th class="num">Outstanding</th><th>Status</th></tr></thead>
<tbody>
{{range .Rows}}<tr><td>{{.VendorName}}</td><td class="rpt-code">{{.InvoiceNumber}}</td><td>{{tanggal .InvoiceDate}}</td><td>{{tanggal .DueDate}}</td><td class="num">{{rupiahM .Payable}}</td><td class="num">{{rupiah .Paid}}</td><td class="num">{{rupiah .Outstanding}}</td><td>{{payStat .Status}}</td></tr>{{end}}
</tbody>
<tfoot><tr><td colspan="4">Total</td><td class="num">{{rupiahM .TotalPayable}}</td><td></td><td class="num">{{rupiahM .TotalOutstanding}}</td><td></td></tr></tfoot>
</table>
`

// RenderAPInvoiceListPrint menulis HTML cetak Laporan Hutang Usaha ke w.
func RenderAPInvoiceListPrint(w io.Writer, companyName string, invoices []*InvoiceView) error {
	data := apInvoicePrintData{}
	for _, inv := range invoices {
		outstanding, err := domain.NewMoney(inv.Outstanding)
		if err != nil {
			outstanding = domain.Money{}
		}
		data.Rows = append(data.Rows, apInvoicePrintRow{
			VendorName:    inv.VendorName,
			InvoiceNumber: inv.InvoiceNumber,
			InvoiceDate:   inv.InvoiceDate,
			DueDate:       inv.DueDate,
			Payable:       inv.PayableAmount,
			Paid:          inv.PaidAmount,
			Outstanding:   inv.Outstanding,
			Status:        inv.PaymentStatus,
		})
		data.TotalPayable = data.TotalPayable.Add(inv.PayableAmount)
		data.TotalOutstanding = data.TotalOutstanding.Add(outstanding)
	}

	bodyTmpl, err := template.New("ap-invoices").Funcs(apPrintFuncs).Parse(apInvoiceListBodyTpl)
	if err != nil {
		return fmt.Errorf("ap: parse body template ap-invoices: %w", err)
	}
	var buf bytes.Buffer
	if err := bodyTmpl.Execute(&buf, data); err != nil {
		return fmt.Errorf("ap: render body template ap-invoices: %w", err)
	}

	shellTmpl, err := template.New("shell").Funcs(apPrintFuncs).Parse(printkit.ReportShellHTML)
	if err != nil {
		return err
	}
	return shellTmpl.Execute(w, printkit.ShellData{
		Title:       "Laporan Hutang Usaha",
		Subtitle:    "Per " + fmtTanggalAP(time.Now()),
		CompanyName: companyName,
		Body:        template.HTML(buf.String()), //nolint:gosec // buf dirender html/template di atas — sudah escaped
	})
}

// Shell HTML+CSS-nya sendiri (warna brand, tipografi, chrome cetak) tinggal
// di internal/printkit, dipakai bersama internal/reporting — dulu disalin
// persis di sini (apReportShellHTML) karena domain package tidak saling
// impor; sekarang cukup satu salinan lewat leaf package printkit.

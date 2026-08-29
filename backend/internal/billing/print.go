package billing

import (
	"html/template"
	"io"
	"strconv"
	"strings"
	"time"

	"esaproperti/internal/domain"
	"esaproperti/internal/printkit"
)

// PrintData holds all data needed to render an invoice print page.
type PrintData struct {
	// Invoice fields
	InvoiceNumber string
	InvoiceType   InvoiceType
	IssueDate     time.Time
	DueDate       time.Time
	Amount        domain.Money
	Status        InvoiceStatus
	Notes         string
	// Company (tenant)
	CompanyName string
	// Customer (contract)
	BuyerName   string
	BuyerID     string
	PaymentType string // "kpr" | "tunai"
	// Property
	ProjectName string
	UnitCode    string
	UnitType    string
	UnitArea    string // e.g. "120.00"
	// Ringkasan finansial kontrak — SUMBER SAMA dengan receipt (satu rumus).
	HasSummary      bool
	UnitPrice       domain.Money
	Discount        domain.Money
	NetContract     domain.Money
	TotalPaid       domain.Money
	Outstanding     domain.Money
	PriceIsSnapshot bool
}

// RenderInvoicePrint writes the invoice HTML to w.
func RenderInvoicePrint(w io.Writer, data *PrintData) error {
	funcs := template.FuncMap{
		"rupiah":           formatRupiah,
		"fmtDate":          formatDate,
		"invoiceTypeLabel": invoiceTypeLabel,
		"statusLabel":      statusLabel,
		"statusClass":      statusClass,
		"paymentTypeLabel": paymentTypeLabel,
		"now":              func() time.Time { return time.Now() },
		"logoB64":          func() template.URL { return template.URL(printkit.LogoBase64) },
	}
	tmpl, err := template.New("invoice").Funcs(funcs).Parse(invoicePrintHTML)
	if err != nil {
		return err
	}
	return tmpl.Execute(w, data)
}

// ── Template helper functions ─────────────────────────────────────────────────

func formatRupiah(m domain.Money) string {
	s := m.String()
	// Remove .XXXX decimal part
	if dot := strings.Index(s, "."); dot >= 0 {
		s = s[:dot]
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return "Rp 0"
	}
	if n < 0 {
		return "-Rp " + addPeriodSeparators(strconv.FormatInt(-n, 10))
	}
	return "Rp " + addPeriodSeparators(strconv.FormatInt(n, 10))
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

var indonesianMonths = [...]string{
	"", // 0 — unused
	"Januari", "Februari", "Maret", "April",
	"Mei", "Juni", "Juli", "Agustus",
	"September", "Oktober", "November", "Desember",
}

func formatDate(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	return strconv.Itoa(t.Day()) + " " + indonesianMonths[t.Month()] + " " + strconv.Itoa(t.Year())
}

func invoiceTypeLabel(t InvoiceType) string {
	switch t {
	case TypeDP:
		return "Uang Muka (DP)"
	case TypeTermin:
		return "Cicilan / Termin"
	case TypePelunasan:
		return "Pelunasan"
	case TypeKekurangan:
		return "Kekurangan Pembayaran"
	default:
		return string(t)
	}
}

func statusLabel(s InvoiceStatus) string {
	switch s {
	case StatusIssued:
		return "Diterbitkan"
	case StatusPaid:
		return "Lunas"
	case StatusCancelled:
		return "Dibatalkan"
	default:
		return string(s)
	}
}

func statusClass(s InvoiceStatus) string {
	switch s {
	case StatusIssued:
		return "badge-issued"
	case StatusPaid:
		return "badge-paid"
	case StatusCancelled:
		return "badge-cancelled"
	default:
		return "badge-issued"
	}
}

func paymentTypeLabel(pt string) string {
	switch strings.ToLower(pt) {
	case "kpr":
		return "KPR (Kredit Pemilikan Rumah)"
	case "tunai":
		return "Tunai / Cash"
	default:
		if pt == "" {
			return "-"
		}
		return pt
	}
}

// ── HTML Template ─────────────────────────────────────────────────────────────

const invoicePrintHTML = `<!DOCTYPE html>
<html lang="id">
<head>
<meta charset="UTF-8" />
<meta name="viewport" content="width=device-width, initial-scale=1.0" />
<title>Invoice {{.InvoiceNumber}} — {{.CompanyName}}</title>
<style>
/* ── Reset & base ── */
*, *::before, *::after { box-sizing: border-box; margin: 0; padding: 0; }
html { font-size: 14px; }
body {
  font-family: 'Segoe UI', Arial, sans-serif;
  background: #F9F7F5;
  color: #211812;
  line-height: 1.55;
}

/* ── Print toolbar (hidden when printing) ── */
.print-bar {
  position: fixed;
  top: 0; left: 0; right: 0;
  z-index: 100;
  background: #A84522;
  color: #fff;
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 10px 24px;
  box-shadow: 0 2px 8px rgba(0,0,0,0.25);
}
.print-bar h1 { font-size: 1rem; font-weight: 600; letter-spacing: 0.02em; }
.btn-print {
  background: #fff;
  color: #A84522;
  border: none;
  border-radius: 6px;
  padding: 8px 22px;
  font-size: 0.9rem;
  font-weight: 700;
  cursor: pointer;
  letter-spacing: 0.03em;
  transition: opacity 0.2s;
}
.btn-print:hover { opacity: 0.9; }

/* ── A4 page wrapper ── */
.page-wrap {
  padding: 80px 32px 40px; /* top padding accounts for fixed toolbar */
  display: flex;
  justify-content: center;
}
.invoice-sheet {
  width: 210mm;
  min-height: 297mm;
  background: #fff;
  border-radius: 10px;
  box-shadow: 0 4px 32px rgba(0,0,0,0.12);
  padding: 14mm 16mm 12mm;
  display: flex;
  flex-direction: column;
  gap: 0;
}

/* ── Header ── */
.inv-header {
  display: flex;
  justify-content: space-between;
  align-items: flex-start;
  padding-bottom: 10mm;
  border-bottom: 3px solid #A84522;
  margin-bottom: 8mm;
}
.inv-brand { display: flex; gap: 4mm; align-items: flex-start; }
.inv-brand img { width: 17mm; height: auto; margin-top: 2px; }
.company-block .company-name {
  font-size: 1.5rem;
  font-weight: 800;
  color: #211812;
  letter-spacing: -0.01em;
}
.company-block .company-sub {
  font-size: 0.78rem;
  color: #6E635A;
  margin-top: 2px;
}
.inv-title-block { text-align: right; }
.inv-title-block .label-invoice {
  font-size: 2rem;
  font-weight: 900;
  color: #211812;
  letter-spacing: 0.05em;
  line-height: 1;
}
.inv-title-block .inv-number {
  font-size: 0.85rem;
  color: #6E635A;
  margin-top: 4px;
  font-family: 'Courier New', Courier, monospace;
  letter-spacing: 0.04em;
}
.inv-title-block .inv-status {
  display: inline-block;
  margin-top: 8px;
  padding: 4px 14px;
  border-radius: 20px;
  font-size: 0.72rem;
  font-weight: 700;
  letter-spacing: 0.06em;
  text-transform: uppercase;
}
.badge-issued  { background: #FFFBEB; color: #92400E; }
.badge-paid    { background: #ECFDF5; color: #065F46; }
.badge-cancelled { background: #FEF2F2; color: #991B1B; }

/* ── Info grid (3 columns) ── */
.info-grid {
  display: grid;
  grid-template-columns: 1fr 1fr 1fr;
  gap: 6mm 8mm;
  margin-bottom: 8mm;
  padding: 6mm 7mm;
  background: #FAF0EB;
  border-radius: 8px;
  border: 1px solid #EAE5E0;
}
.info-section-title {
  font-size: 0.65rem;
  font-weight: 700;
  letter-spacing: 0.1em;
  text-transform: uppercase;
  color: #A0958C;
  margin-bottom: 4px;
  border-bottom: 1px solid #EAE5E0;
  padding-bottom: 3px;
}
.info-row { margin-bottom: 3px; }
.info-label {
  font-size: 0.68rem;
  color: #A0958C;
  display: block;
  line-height: 1.2;
}
.info-value {
  font-size: 0.82rem;
  color: #211812;
  font-weight: 600;
}

/* ── Itemized table ── */
.inv-table {
  width: 100%;
  border-collapse: collapse;
  margin-bottom: 8mm;
  font-size: 0.82rem;
}
.inv-table thead tr {
  background: #A84522;
  color: #fff;
}
.inv-table thead th {
  padding: 8px 12px;
  text-align: left;
  font-weight: 600;
  letter-spacing: 0.04em;
  font-size: 0.72rem;
  text-transform: uppercase;
}
.inv-table thead th:last-child { text-align: right; }
.inv-table tbody tr { border-bottom: 1px solid #F5F2EE; }
.inv-table tbody tr:last-child { border-bottom: none; }
.inv-table tbody td {
  padding: 10px 12px;
  vertical-align: top;
}
.inv-table tbody td:last-child { text-align: right; }
.item-desc { font-weight: 600; color: #211812; }
.item-sub  { font-size: 0.72rem; color: #6E635A; margin-top: 2px; }
.inv-table tfoot tr { border-top: 2px solid #A84522; }
.inv-table tfoot td {
  padding: 10px 12px;
  font-weight: 700;
  font-size: 0.9rem;
}
.inv-table tfoot td:first-child { color: #6E635A; text-transform: uppercase; letter-spacing: 0.05em; }
.inv-table tfoot td:last-child { text-align: right; color: #A84522; font-size: 1rem; }

/* ── Payment instruction ── */
.ctr-summary { display: flex; border: 1.2px solid #2b211b; margin: 12px 0 14px; }
.cs-cell { flex: 1; border-right: 1px solid #2b211b; text-align: center; }
.cs-cell:last-child { border-right: none; }
.cs-cell .csk { font-size: 0.68rem; font-weight: 700; background: #faf0eb; border-bottom: 1px solid #2b211b; padding: 4px 6px; }
.cs-cell .csv { padding: 7px 6px; font-weight: 700; font-variant-numeric: tabular-nums; }
.cs-out .csk { background: #A84522; color: #fff; }
.cs-out .csv { color: #A84522; }
.cs-note { font-size: 0.66rem; color: #7a6e65; margin: -8px 0 12px; }
.payment-section {
  background: #fffbeb;
  border: 1px solid #fcd34d;
  border-radius: 8px;
  padding: 5mm 6mm;
  margin-bottom: 8mm;
}
.payment-section h3 {
  font-size: 0.75rem;
  font-weight: 700;
  letter-spacing: 0.08em;
  text-transform: uppercase;
  color: #92400e;
  margin-bottom: 6px;
}
.payment-section p {
  font-size: 0.78rem;
  color: #78350f;
  line-height: 1.6;
}
.payment-section .due-highlight {
  font-weight: 700;
  color: #b45309;
}

/* ── Signature block ── */
.signature-block {
  display: flex;
  justify-content: space-between;
  gap: 8mm;
  margin-bottom: 8mm;
}
.sig-box {
  flex: 1;
  text-align: center;
  border: 1px solid #EAE5E0;
  border-radius: 8px;
  padding: 5mm 4mm 3mm;
}
.sig-box .sig-role {
  font-size: 0.7rem;
  font-weight: 700;
  letter-spacing: 0.06em;
  text-transform: uppercase;
  color: #A0958C;
  margin-bottom: 16mm; /* blank space for signature */
}
.sig-box .sig-line {
  border-top: 1px solid #211812;
  padding-top: 4px;
  font-size: 0.75rem;
  color: #211812;
  font-weight: 600;
}
.sig-box .sig-name {
  font-size: 0.68rem;
  color: #6E635A;
  margin-top: 2px;
}

/* ── Footer ── */
.inv-footer {
  margin-top: auto;
  padding-top: 5mm;
  border-top: 1px dashed #C9B8AC;
  text-align: center;
  font-size: 0.65rem;
  color: #A89D94;
  line-height: 1.7;
}

/* ── Print styles ── */
@page {
  size: A4 portrait;
  margin: 20mm;
}
@media print {
  .print-bar { display: none !important; }
  body { background: #fff; }
  .page-wrap { padding: 0; display: block; }
  .invoice-sheet {
    width: 100%;
    min-height: auto;
    box-shadow: none;
    border-radius: 0;
    padding: 0;
  }
}
</style>
</head>
<body>

<!-- Print toolbar -->
<div class="print-bar">
  <h1>Preview Invoice</h1>
  <button class="btn-print" onclick="window.print()">&#128438; Cetak / Simpan PDF</button>
</div>

<div class="page-wrap">
<div class="invoice-sheet">

  <!-- ── Header ── -->
  <header class="inv-header">
    <div class="inv-brand">
      <img src="data:image/png;base64,{{logoB64}}" alt="logo" />
      <div class="company-block">
        <div class="company-name">{{.CompanyName}}</div>
        <div class="company-sub">Developer Properti</div>
      </div>
    </div>
    <div class="inv-title-block">
      <div class="label-invoice">INVOICE</div>
      <div class="inv-number">{{.InvoiceNumber}}</div>
      <span class="inv-status {{statusClass .Status}}">{{statusLabel .Status}}</span>
    </div>
  </header>

  <!-- ── 3-column info grid ── -->
  <section class="info-grid">

    <!-- Column 1: Customer -->
    <div>
      <div class="info-section-title">Data Pembeli</div>
      <div class="info-row">
        <span class="info-label">Nama Pembeli</span>
        <span class="info-value">{{.BuyerName}}</span>
      </div>
      <div class="info-row">
        <span class="info-label">No. Identitas (KTP)</span>
        <span class="info-value">{{if .BuyerID}}{{.BuyerID}}{{else}}—{{end}}</span>
      </div>
      <div class="info-row">
        <span class="info-label">Metode Pembayaran</span>
        <span class="info-value">{{paymentTypeLabel .PaymentType}}</span>
      </div>
    </div>

    <!-- Column 2: Invoice details -->
    <div>
      <div class="info-section-title">Detail Invoice</div>
      <div class="info-row">
        <span class="info-label">Tanggal Terbit</span>
        <span class="info-value">{{fmtDate .IssueDate}}</span>
      </div>
      <div class="info-row">
        <span class="info-label">Jatuh Tempo</span>
        <span class="info-value">{{fmtDate .DueDate}}</span>
      </div>
      <div class="info-row">
        <span class="info-label">Jenis Invoice</span>
        <span class="info-value">{{invoiceTypeLabel .InvoiceType}}</span>
      </div>
    </div>

    <!-- Column 3: Property -->
    <div>
      <div class="info-section-title">Data Properti</div>
      <div class="info-row">
        <span class="info-label">Proyek</span>
        <span class="info-value">{{.ProjectName}}</span>
      </div>
      <div class="info-row">
        <span class="info-label">Kode Unit</span>
        <span class="info-value">{{.UnitCode}}</span>
      </div>
      <div class="info-row">
        <span class="info-label">Tipe Unit</span>
        <span class="info-value">{{.UnitType}}</span>
      </div>
      <div class="info-row">
        <span class="info-label">Luas Bangunan</span>
        <span class="info-value">{{.UnitArea}} m²</span>
      </div>
    </div>

  </section>

  <!-- ── Itemized table ── -->
  <table class="inv-table">
    <thead>
      <tr>
        <th>Deskripsi</th>
        <th>Metode Bayar</th>
        <th>Jumlah</th>
      </tr>
    </thead>
    <tbody>
      <tr>
        <td>
          <div class="item-desc">{{invoiceTypeLabel .InvoiceType}} — Unit {{.UnitCode}}</div>
          <div class="item-sub">Proyek {{.ProjectName}} · Tipe {{.UnitType}} · {{.UnitArea}} m²</div>
          {{if .Notes}}<div class="item-sub">Catatan: {{.Notes}}</div>{{end}}
        </td>
        <td>{{paymentTypeLabel .PaymentType}}</td>
        <td><strong>{{rupiah .Amount}}</strong></td>
      </tr>
    </tbody>
    <tfoot>
      <tr>
        <td colspan="2">Total</td>
        <td>{{rupiah .Amount}}</td>
      </tr>
    </tfoot>
  </table>

  <!-- ── Payment instruction ── -->
  {{if .HasSummary}}
  <!-- Ringkasan finansial kontrak — sumber & rumus SAMA dengan kwitansi. -->
  <div class="ctr-summary">
    <div class="cs-cell"><div class="csk">Harga Unit{{if not .PriceIsSnapshot}}*{{end}}</div><div class="csv">{{rupiah .UnitPrice}}</div></div>
    <div class="cs-cell"><div class="csk">Diskon</div><div class="csv">{{rupiah .Discount}}</div></div>
    <div class="cs-cell"><div class="csk">Nilai Kontrak</div><div class="csv">{{rupiah .NetContract}}</div></div>
    <div class="cs-cell"><div class="csk">Sudah Dibayar</div><div class="csv">{{rupiah .TotalPaid}}</div></div>
    <div class="cs-cell cs-out"><div class="csk">Terutang</div><div class="csv">{{rupiah .Outstanding}}</div></div>
  </div>
  {{if not .PriceIsSnapshot}}<div class="cs-note">* Kontrak dibuat sebelum pencatatan harga unit — harga mengikuti nilai kontrak (DPP), diskon 0.</div>{{end}}
  {{end}}

  <div class="payment-section">
    <h3>&#128176; Instruksi Pembayaran</h3>
    <p>
      Mohon lakukan pembayaran sebesar <strong>{{rupiah .Amount}}</strong>
      sebelum tanggal <span class="due-highlight">{{fmtDate .DueDate}}</span>.<br />
      Pembayaran dapat dilakukan melalui transfer bank ke rekening yang tertera pada perjanjian jual beli.<br />
      Harap mencantumkan nomor invoice <strong>{{.InvoiceNumber}}</strong> sebagai keterangan transfer.
    </p>
  </div>

  <!-- ── Signature block ── -->
  <div class="signature-block">
    <div class="sig-box">
      <div class="sig-role">Pembeli</div>
      <div class="sig-line">Tanda tangan &amp; nama jelas</div>
      <div class="sig-name">{{.BuyerName}}</div>
    </div>
    <div class="sig-box">
      <div class="sig-role">Pihak Penjual / Perusahaan</div>
      <div class="sig-line">Tanda tangan &amp; stempel</div>
      <div class="sig-name">{{.CompanyName}}</div>
    </div>
  </div>

  <!-- ── Footer ── -->
  <footer class="inv-footer">
    {{.InvoiceNumber}} &nbsp;|&nbsp; Dicetak: {{fmtDate (now)}}
  </footer>

</div><!-- /.invoice-sheet -->
</div><!-- /.page-wrap -->

</body>
</html>`

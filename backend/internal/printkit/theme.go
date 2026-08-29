// Package printkit adalah SATU sumber kebenaran untuk styling dokumen cetak
// (PDF via browser print) — warna brand dan shell laporan tabular yang
// dipakai internal/reporting dan internal/ap. Package ini sengaja tidak
// mengimpor package internal lain (leaf package) supaya bisa diimpor oleh
// reporting maupun ap tanpa membentuk import cycle (lihat CLAUDE.md aturan
// import: "Tidak ada import siklik").
//
// Warna di sini HARUS sinkron dengan frontend/app/globals.css (--color-accent
// dst.) — terracotta diambil dari logo PT Nata Alam Raya. Untuk rebrand:
// ganti warna HANYA di sini (dan di internal/billing, yang punya template
// sendiri karena layoutnya berbeda dari laporan tabular tapi memakai nilai
// hex yang sama — lihat komentar di internal/billing/print.go).
package printkit

import "html/template"

// Warna brand — mirror frontend/app/globals.css :root tokens persis.
const (
	ColorAccent       = "#A84522" // --color-accent (diambil dari logo)
	ColorAccentHover  = "#8A371A" // --color-accent-hover
	ColorAccentLight  = "#FAF0EB" // --color-accent-light
	ColorInk          = "#211812" // --color-text-primary
	ColorInkSecondary = "#6E635A" // --color-text-secondary
	ColorInkTertiary  = "#A0958C" // --color-text-tertiary
	ColorBorder       = "#EAE5E0" // --color-border
	ColorBorderSubtle = "#F5F2EE" // --color-border-subtle
	ColorBg           = "#F9F7F5" // --color-bg
	ColorSurface      = "#FFFFFF" // --color-surface
	ColorSuccess      = "#065F46" // --color-success
	ColorSuccessBg    = "#ECFDF5"
	ColorDanger       = "#991B1B" // --color-danger
	ColorDangerBg     = "#FEF2F2"
	ColorWarning      = "#92400E" // --color-warning
	ColorWarningBg    = "#FFFBEB"
	ColorFooterDash   = "#C9B8AC" // dashed footer rule — sama dgn kwitansi (internal/billing/receipt_print.go)
	ColorFooterText   = "#A89D94" // teks footer — sama dgn kwitansi
)

// ShellData adalah data yang dibutuhkan ReportShellHTML. Dipakai bersama
// oleh internal/reporting dan internal/ap — sebelumnya masing-masing punya
// struct sendiri yang identik (printShellData / apPrintShellData).
type ShellData struct {
	Title       string
	Subtitle    string
	CompanyName string
	Body        template.HTML
}

// ReportShellHTML adalah shell HTML+CSS laporan tabular A4 (header
// perusahaan, judul, badan laporan, footer) — dipakai oleh SEMUA laporan
// tabular: Neraca, Laba Rugi, Arus Kas, Neraca Saldo, Pajak, Pipeline, Buku
// Besar, Piutang (AR Aging) lewat internal/reporting, dan Laporan Hutang
// Usaha + Bukti Kas Keluar lewat internal/ap.
//
// Sebelumnya konstanta ini disalin persis di kedua package
// (reportShellHTML / apReportShellHTML) karena domain package tidak saling
// impor — sekarang cukup satu salinan di sini sehingga rebrand ke depan
// tidak perlu diedit satu per satu.
const ReportShellHTML = `<!DOCTYPE html>
<html lang="id">
<head>
<meta charset="UTF-8" />
<meta name="viewport" content="width=device-width, initial-scale=1.0" />
<title>{{.Title}} — {{.CompanyName}}</title>
<style>
*, *::before, *::after { box-sizing: border-box; margin: 0; padding: 0; }
html { font-size: 14px; }
body { font-family: 'Segoe UI', Arial, sans-serif; background: #F9F7F5; color: #211812; line-height: 1.55; }

.print-bar {
  position: fixed; top: 0; left: 0; right: 0; z-index: 100;
  background: #A84522; color: #fff;
  display: flex; align-items: center; justify-content: space-between;
  padding: 10px 24px; box-shadow: 0 2px 8px rgba(0,0,0,0.25);
}
.print-bar h1 { font-size: 1rem; font-weight: 600; letter-spacing: 0.02em; }
.btn-print {
  background: #fff; color: #A84522; border: none; border-radius: 6px;
  padding: 8px 22px; font-size: 0.9rem; font-weight: 700; cursor: pointer;
  letter-spacing: 0.03em; transition: opacity 0.2s;
}
.btn-print:hover { opacity: 0.9; }

.page-wrap { padding: 80px 32px 40px; display: flex; justify-content: center; }
.report-sheet {
  width: 210mm; min-height: 297mm; background: #fff; border-radius: 10px;
  box-shadow: 0 4px 32px rgba(0,0,0,0.12); padding: 14mm 16mm 12mm;
  display: flex; flex-direction: column; gap: 0;
}

.rpt-header {
  display: flex; justify-content: space-between; align-items: flex-start;
  padding-bottom: 8mm; border-bottom: 3px solid #A84522; margin-bottom: 8mm;
}
.rpt-brand { display: flex; gap: 4mm; align-items: flex-start; }
.rpt-brand img { width: 16mm; height: auto; margin-top: 2px; }
.rpt-header .company-name { font-size: 1.4rem; font-weight: 800; color: #211812; letter-spacing: -0.01em; }
.rpt-header .company-sub { font-size: 0.78rem; color: #6E635A; margin-top: 2px; }
.rpt-title-block { text-align: right; }
.rpt-title { font-size: 1.4rem; font-weight: 900; color: #211812; letter-spacing: 0.02em; }
.rpt-subtitle { font-size: 0.85rem; color: #6E635A; margin-top: 4px; }

.rpt-section-title {
  font-size: 0.8rem; font-weight: 700; color: #A84522; text-transform: uppercase;
  letter-spacing: 0.05em; margin: 6mm 0 3mm; padding-bottom: 2mm; border-bottom: 1px solid #EAE5E0;
}

table.rpt-table { width: 100%; border-collapse: collapse; margin-bottom: 6mm; font-size: 0.8rem; }
table.rpt-table thead tr { background: #A84522; color: #fff; }
table.rpt-table thead th { padding: 7px 10px; text-align: left; font-weight: 600; letter-spacing: 0.03em; font-size: 0.68rem; text-transform: uppercase; }
table.rpt-table thead th.num { text-align: right; }
table.rpt-table tbody tr { border-bottom: 1px solid #F5F2EE; }
table.rpt-table tbody td { padding: 7px 10px; vertical-align: top; }
table.rpt-table tbody td.num { text-align: right; font-variant-numeric: tabular-nums; }
table.rpt-table tfoot tr { border-top: 2px solid #A84522; }
table.rpt-table tfoot td { padding: 8px 10px; font-weight: 700; }
table.rpt-table tfoot td.num { text-align: right; color: #A84522; }
.rpt-code { font-family: 'Courier New', monospace; font-size: 0.72rem; color: #6E635A; }

.rpt-banner { border-radius: 8px; padding: 8px 14px; margin-bottom: 6mm; font-size: 0.85rem; font-weight: 600; }
.rpt-banner.ok { background: #ECFDF5; color: #065F46; border: 1px solid #065F46; }
.rpt-banner.bad { background: #FEF2F2; color: #991B1B; border: 1px solid #991B1B; }

.rpt-pills { display: flex; gap: 6mm; margin-bottom: 6mm; }
.rpt-pill { flex: 1; background: #FAF0EB; border: 1px solid #EAE5E0; border-radius: 8px; padding: 5mm 6mm; }
.rpt-pill .pl { font-size: 0.63rem; color: #A0958C; text-transform: uppercase; letter-spacing: 0.05em; }
.rpt-pill .pv { font-size: 1rem; font-weight: 800; color: #A84522; margin-top: 2px; }

.rpt-footer { margin-top: auto; padding-top: 5mm; border-top: 1px dashed #C9B8AC; text-align: center; font-size: 0.65rem; color: #A89D94; }

@page { size: A4 portrait; margin: 20mm; }
@media print {
  .print-bar { display: none !important; }
  body { background: #fff; }
  .page-wrap { padding: 0; display: block; }
  .report-sheet { width: 100%; min-height: auto; box-shadow: none; border-radius: 0; padding: 0; }
}
</style>
</head>
<body>
<div class="print-bar">
  <h1>Preview {{.Title}}</h1>
  <button class="btn-print" onclick="window.print()">&#128438; Cetak / Simpan PDF</button>
</div>
<div class="page-wrap">
<div class="report-sheet">
  <header class="rpt-header">
    <div class="rpt-brand">
      <img src="data:image/png;base64,{{logoB64}}" alt="logo" />
      <div>
        <div class="company-name">{{.CompanyName}}</div>
        <div class="company-sub">Developer Properti</div>
      </div>
    </div>
    <div class="rpt-title-block">
      <div class="rpt-title">{{.Title}}</div>
      <div class="rpt-subtitle">{{.Subtitle}}</div>
    </div>
  </header>

  {{.Body}}

  <footer class="rpt-footer">{{.Title}} &nbsp;|&nbsp; Dicetak: {{tanggal (now)}}</footer>
</div>
</div>
</body>
</html>`

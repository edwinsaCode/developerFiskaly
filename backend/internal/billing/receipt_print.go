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

// RenderReceiptPrint writes the receipt (kwitansi) HTML to w.
func RenderReceiptPrint(w io.Writer, data *ReceiptPrintData) error {
	funcs := template.FuncMap{
		"rupiah":           formatRupiah,
		"fmtDate":          formatDate,
		"paymentTypeLabel": paymentTypeLabel,
		"bankLabel":        bankLabel,
		"terbilang":        terbilangRupiah,
		"now":              func() time.Time { return time.Now() },
		"logoB64":          func() template.URL { return template.URL(printkit.LogoBase64) },
	}
	tmpl, err := template.New("receipt").Funcs(funcs).Parse(receiptPrintHTML)
	if err != nil {
		return err
	}
	return tmpl.Execute(w, data)
}

// bankLabel memetakan kode akun bank ke nama yang ramah pembaca.
func bankLabel(code string) string {
	switch code {
	case "1-1100":
		return "Kas"
	case "1-1200":
		return "Bank — Operasional"
	case "1-1300":
		return "Bank — BCA"
	case "1-1400":
		return "Bank — Mandiri"
	case "1-1500":
		return "Bank — BNI"
	default:
		if code == "" {
			return "—"
		}
		return code
	}
}

// ── Terbilang (angka → kata) untuk kwitansi ──────────────────────────────────

var terbilangUnits = [...]string{
	"", "satu", "dua", "tiga", "empat", "lima",
	"enam", "tujuh", "delapan", "sembilan", "sepuluh", "sebelas",
}

// terbilang mengubah bilangan bulat non-negatif menjadi kata bahasa Indonesia.
func terbilang(n int64) string {
	switch {
	case n < 0:
		return "minus " + terbilang(-n)
	case n < 12:
		return terbilangUnits[n]
	case n < 20:
		return terbilang(n-10) + " belas"
	case n < 100:
		return terbilang(n/10) + " puluh" + spaceWord(terbilang(n%10))
	case n < 200:
		return "seratus" + spaceWord(terbilang(n-100))
	case n < 1000:
		return terbilang(n/100) + " ratus" + spaceWord(terbilang(n%100))
	case n < 2000:
		return "seribu" + spaceWord(terbilang(n-1000))
	case n < 1_000_000:
		return terbilang(n/1000) + " ribu" + spaceWord(terbilang(n%1000))
	case n < 1_000_000_000:
		return terbilang(n/1_000_000) + " juta" + spaceWord(terbilang(n%1_000_000))
	case n < 1_000_000_000_000:
		return terbilang(n/1_000_000_000) + " miliar" + spaceWord(terbilang(n%1_000_000_000))
	default:
		return terbilang(n/1_000_000_000_000) + " triliun" + spaceWord(terbilang(n%1_000_000_000_000))
	}
}

func spaceWord(s string) string {
	if s == "" {
		return ""
	}
	return " " + s
}

// terbilangRupiah memformat Money menjadi "<Kata> rupiah" (kapital awal).
// Bagian desimal/sen diabaikan — nominal selalu rupiah bulat (Invariant #2).
func terbilangRupiah(m domain.Money) string {
	s := m.String()
	if dot := strings.Index(s, "."); dot >= 0 {
		s = s[:dot]
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return "-"
	}
	words := terbilang(n)
	if words == "" {
		words = "nol"
	}
	// Kapitalkan huruf pertama.
	r := []rune(words)
	r[0] = []rune(strings.ToUpper(string(r[0])))[0]
	return string(r) + " rupiah"
}

// ── HTML Template ─────────────────────────────────────────────────────────────

const receiptPrintHTML = `<!DOCTYPE html>
<html lang="id">
<head>
<meta charset="UTF-8" />
<meta name="viewport" content="width=device-width, initial-scale=1.0" />
<title>{{if eq .ReceiptType "booking"}}Kwitansi Booking{{else if eq .ReceiptType "realization"}}Kwitansi Biaya Realisasi{{else if eq .ReceiptType "kpr_disbursement"}}Kwitansi Pencairan KPR{{else}}Bukti Pembayaran{{end}} {{.ReceiptNumber}} — {{.CompanyName}}</title>
<style>
*, *::before, *::after { box-sizing: border-box; margin: 0; padding: 0; }
html { font-size: 14px; }
body { font-family: 'Segoe UI', Arial, sans-serif; background: #f5f2ee; color: #2b211b; line-height: 1.5; }

.print-bar {
  position: fixed; top: 0; left: 0; right: 0; z-index: 100;
  background: #A84522; color: #fff; display: flex; align-items: center;
  justify-content: space-between; padding: 10px 24px; box-shadow: 0 2px 8px rgba(0,0,0,0.25);
}
.print-bar h1 { font-size: 1rem; font-weight: 600; letter-spacing: 0.02em; }
.btn-print {
  background: #fff; color: #A84522; border: none; border-radius: 6px;
  padding: 8px 22px; font-size: 0.9rem; font-weight: 700; cursor: pointer;
  letter-spacing: 0.03em; transition: opacity 0.2s;
}
.btn-print:hover { opacity: 0.9; }

.page-wrap { padding: 80px 32px 40px; display: flex; justify-content: center; }
.receipt-sheet {
  width: 210mm; min-height: 140mm; background: #fff; border-radius: 8px;
  box-shadow: 0 4px 32px rgba(0,0,0,0.12); padding: 12mm 14mm;
  display: flex; flex-direction: column; font-size: 0.9rem;
}

/* ── Header: logo + kotak perusahaan (kiri) · judul + kotak tgl/no (kanan) ── */
.hdr { display: flex; justify-content: space-between; align-items: flex-start; margin-bottom: 7mm; }
.hdr-left { display: flex; gap: 5mm; align-items: flex-start; }
.hdr-left img { width: 22mm; height: auto; }
.company-box { border: 1.4px solid #2b211b; padding: 2.5mm 4mm; min-width: 62mm; }
.company-box .cname { font-weight: 800; font-size: 0.92rem; letter-spacing: 0.02em; }
.company-box .caddr { font-size: 0.78rem; margin-top: 1mm; line-height: 1.45; color: #4a3d34; }
.hdr-right { text-align: right; }
.doc-title { font-size: 1.55rem; font-weight: 700; letter-spacing: 0.01em; color: #2b211b;
  border-bottom: 2.5px solid #A84522; padding-bottom: 1.5mm; display: inline-block; }
.meta-boxes { display: flex; gap: 4mm; justify-content: flex-end; margin-top: 4mm; }
.meta-box { border: 1.2px solid #2b211b; min-width: 30mm; text-align: center; }
.meta-box .mlabel { font-size: 0.7rem; border-bottom: 1px solid #2b211b; padding: 1mm 2.5mm; background: #faf0eb; }
.meta-box .mval { padding: 1.5mm 2.5mm; font-weight: 600; font-size: 0.85rem; }

/* ── Diterima dari (kiri) · Jumlah Terima (kanan) ── */
.row-2 { display: flex; justify-content: space-between; align-items: flex-end; margin-bottom: 6mm; gap: 8mm; }
.received-box { flex: 1; max-width: 95mm; }
.received-box .rlabel { display: inline-block; border: 1.2px solid #2b211b; background: #faf0eb;
  font-size: 0.74rem; font-weight: 700; padding: 1mm 3mm; margin-bottom: -1px; }
.received-box .rval { border: 1.2px solid #2b211b; padding: 2.5mm 3.5mm; font-weight: 600; min-height: 14mm; }
.received-box .rval .rid { font-weight: 400; font-size: 0.78rem; color: #4a3d34; }
.amount-terima { border: 1.4px solid #2b211b; text-align: center; min-width: 42mm; }
.amount-terima .alabel { font-size: 0.72rem; font-weight: 700; border-bottom: 1px solid #2b211b;
  padding: 1mm 3mm; background: #faf0eb; }
.amount-terima .aval { padding: 2mm 4mm; font-size: 1.15rem; font-weight: 800; color: #A84522; }

/* ── Tabel rincian (meniru kolom formulir referensi) ── */
table.rincian { width: 100%; border-collapse: collapse; margin-bottom: 2mm; }
table.rincian th, table.rincian td { border: 1.2px solid #2b211b; padding: 2mm 3mm; font-size: 0.82rem; }
table.rincian th { background: #faf0eb; font-weight: 700; text-align: center; }
table.rincian td.num { text-align: right; font-variant-numeric: tabular-nums; }
.total-row { display: flex; justify-content: flex-end; align-items: center; gap: 3mm; margin-bottom: 5mm; }
.total-row .tlabel { border: 1.2px solid #2b211b; padding: 1.2mm 4mm; font-weight: 700; font-size: 0.8rem; background: #faf0eb; }
.total-row .tval { border: 1.2px solid #2b211b; padding: 1.2mm 4mm; min-width: 38mm; text-align: right;
  font-weight: 800; font-variant-numeric: tabular-nums; }

/* ── Ringkasan finansial kontrak ── */
.summary-grid { display: flex; gap: 0; border: 1.4px solid #2b211b; margin-bottom: 5mm; }
.sum-cell { flex: 1; border-right: 1.2px solid #2b211b; text-align: center; }
.sum-cell:last-child { border-right: none; }
.sum-cell .sk { font-size: 0.7rem; font-weight: 700; background: #faf0eb; border-bottom: 1px solid #2b211b; padding: 1.2mm 2mm; }
.sum-cell .sv { padding: 2mm; font-weight: 700; font-size: 0.88rem; font-variant-numeric: tabular-nums; }
.sum-cell.terutang .sk { background: #A84522; color: #fff; }
.sum-cell.terutang .sv { color: #A84522; font-size: 0.95rem; }
.sum-note { font-size: 0.68rem; color: #7a6e65; margin: -3mm 0 5mm; }

/* ── Terbilang ── */
.terbilang { display: flex; align-items: stretch; gap: 3mm; margin-bottom: 9mm; }
.terbilang .tblabel { font-size: 0.82rem; padding-top: 1.5mm; }
.terbilang .tbval { flex: 1; border: 1.2px solid #2b211b; padding: 1.5mm 3.5mm; font-style: italic; font-weight: 600; }

/* ── Tanda tangan ── */
.sig-row { display: flex; justify-content: space-between; margin-top: auto; padding-top: 4mm; }
.sig-left { font-size: 0.82rem; }
.sig-left .sig-line { margin-top: 12mm; border-top: 1px solid #2b211b; width: 40mm; }
.sig-left .tgl { margin-top: 2mm; font-size: 0.78rem; }
.sig-center { text-align: center; font-size: 0.82rem; }
.sig-center .space { height: 16mm; }
.sig-center .who { font-weight: 700; color: #A84522; letter-spacing: 0.03em; }
.sig-center .dept { font-size: 0.76rem; font-weight: 600; color: #4a3d34; }

.rcpt-footer { margin-top: 6mm; padding-top: 3mm; border-top: 1px dashed #c9b8ac;
  text-align: center; font-size: 0.65rem; color: #a89d94; }

@page { size: A4 landscape; margin: 12mm; }
@media print {
  .print-bar { display: none !important; }
  body { background: #fff; }
  .page-wrap { padding: 0; display: block; }
  .receipt-sheet { width: 100%; min-height: auto; box-shadow: none; border-radius: 0; padding: 0; }
}
</style>
</head>
<body>

<div class="print-bar">
  <h1>{{.CompanyName}} — {{if eq .ReceiptType "booking"}}Kwitansi Booking{{else if eq .ReceiptType "realization"}}Kwitansi Biaya Realisasi{{else if eq .ReceiptType "kpr_disbursement"}}Kwitansi Pencairan KPR{{else}}Bukti Pembayaran{{end}}</h1>
  <button class="btn-print" onclick="window.print()">&#128438; Cetak / Simpan PDF</button>
</div>

<div class="page-wrap">
<div class="receipt-sheet">

  <div class="hdr">
    <div class="hdr-left">
      <img src="data:image/png;base64,{{logoB64}}" alt="logo" />
      <div class="company-box">
        <div class="cname">{{.CompanyName}}</div>
        <div class="caddr">Developer Properti<br/>Proyek {{.ProjectName}}</div>
      </div>
    </div>
    <div class="hdr-right">
      <span class="doc-title">{{if eq .ReceiptType "booking"}}Kwitansi Booking{{else if eq .ReceiptType "realization"}}Kwitansi Biaya Realisasi{{else if eq .ReceiptType "kpr_disbursement"}}Kwitansi Pencairan KPR{{else}}Bukti Pembayaran{{end}}</span>
      {{/*
        Tidak ada baris keterangan akuntansi di bawah judul kwitansi.

        Dulu di sini tercetak "Titipan Realisasi — di luar harga unit, bukan
        pendapatan" dan sejenisnya. Itu bahasa buku besar, dan kwitansi adalah
        lembar untuk CUSTOMER: yang ia baca justru "bukan pendapatan" di atas
        uang yang baru saja ia serahkan, lalu ia bertanya apakah uangnya
        dihitung. Perlakuan akuntansinya tidak hilang — tetap hidup di jurnal
        dan di layar internal — hanya tidak dicetak ke tangan pembeli.

        Judul dokumennya sendiri ("Kwitansi Biaya Realisasi", "Kwitansi
        Booking", "Kwitansi Pencairan KPR") sudah cukup memberi tahu ini
        pembayaran apa.
      */}}
      <div class="meta-boxes">
        <div class="meta-box">
          <div class="mlabel">Tgl Pembayaran :</div>
          <div class="mval">{{fmtDate .ReceivedAt}}</div>
        </div>
        <div class="meta-box">
          <div class="mlabel">No. Form :</div>
          <div class="mval">{{.ReceiptNumber}}</div>
        </div>
      </div>
    </div>
  </div>

  <div class="row-2">
    <div class="received-box">
      <span class="rlabel">Diterima dr :</span>
      <div class="rval">
        {{/* KWD: penyetor = bank penyalur. Nama customer tetap dicetak sebagai
             keterangan "untuk kewajiban siapa" — bukan sebagai penyetor.
             Fallback ke nama pembeli hanya bila master bank tidak terisi. */}}
        {{if and (eq .ReceiptType "kpr_disbursement") .DisbursingBankName}}
          {{.DisbursingBankName}}
          {{if .BuyerName}}<div class="rid">Pencairan KPR a.n. {{.BuyerName}}{{if .BuyerID}} · KTP {{.BuyerID}}{{end}}</div>{{end}}
        {{else}}
          {{if .BuyerName}}{{.BuyerName}}{{else}}Pembeli{{end}}
          {{if .BuyerID}}<div class="rid">KTP {{.BuyerID}}</div>{{end}}
        {{end}}
      </div>
    </div>
    <div class="amount-terima">
      <div class="alabel">Jumlah Terima :</div>
      <div class="aval">{{rupiah .Amount}}</div>
    </div>
  </div>

  <table class="rincian">
    <thead>
      <tr>
        <th style="width:15%">No. Form</th>
        <th style="width:12%">Tanggal</th>
        <th style="width:14%">Terutang</th>
        <th style="width:13%">Jumlah</th>
        <th>Keterangan</th>
        <th style="width:12%">Total Diskon</th>
      </tr>
    </thead>
    <tbody>
      <tr>
        <td>{{.ReceiptNumber}}</td>
        <td>{{fmtDate .ReceivedAt}}</td>
        <td class="num">{{if .HasChargeSummary}}{{rupiah .ChargeOutstanding}}{{else if .HasSummary}}{{rupiah .Outstanding}}{{else}}&mdash;{{end}}</td>
        <td class="num">{{rupiah .Amount}}</td>
        <td>Unit {{.UnitCode}} — {{.UnitType}}, {{.ProjectName}}{{if .Notes}}<br/><span style="font-size:0.76rem;color:#4a3d34">{{.Notes}}</span>{{end}}<br/><span style="font-size:0.74rem;color:#4a3d34">{{paymentTypeLabel .PaymentType}} · {{bankLabel .BankAccountCode}}</span></td>
        <td class="num">{{if .HasSummary}}{{rupiah .Discount}}{{else}}&mdash;{{end}}</td>
      </tr>
    </tbody>
  </table>
  <div class="total-row">
    <span class="tlabel">Total</span>
    <span class="tval">{{rupiah .Amount}}</span>
  </div>

  {{if .HasChargeSummary}}
  <!-- Ringkasan GRUP TAGIHAN (Billing Batch 2, K-5) — SATU sumber:
       charge.Service.GroupSummary. Kwitansi KWR WAJIB menampilkan 4 angka:
       total tagihan / pembayaran ini / total dibayar / sisa terutang. -->
  <div class="summary-grid">
    <div class="sum-cell"><div class="sk">{{.ChargeGroupLabel}}</div><div class="sv">&nbsp;</div></div>
    <div class="sum-cell"><div class="sk">Total Tagihan</div><div class="sv">{{rupiah .ChargeBilled}}</div></div>
    <div class="sum-cell"><div class="sk">Pembayaran Ini</div><div class="sv">{{rupiah .Amount}}</div></div>
    <div class="sum-cell"><div class="sk">Total Dibayar</div><div class="sv">{{rupiah .ChargePaid}}</div></div>
    <div class="sum-cell terutang"><div class="sk">Sisa Terutang</div><div class="sv">{{rupiah .ChargeOutstanding}}</div></div>
  </div>
  {{end}}

  {{if and .HasSummary (not .HasChargeSummary)}}
  <!-- Ringkasan finansial kontrak — SATU sumber: sale.ContractFinancialSummary.
       Nilai NOL tetap ditampilkan (requirement). Terutang = nilai kontrak −
       seluruh pembayaran yang telah diakui sistem (termasuk pencairan KPR). -->
  <div class="summary-grid">
    <div class="sum-cell"><div class="sk">Harga Unit{{if not .PriceIsSnapshot}}*{{end}}</div><div class="sv">{{rupiah .UnitPrice}}</div></div>
    <div class="sum-cell"><div class="sk">Diskon</div><div class="sv">{{rupiah .Discount}}</div></div>
    <div class="sum-cell"><div class="sk">Nilai Kontrak</div><div class="sv">{{rupiah .NetContract}}</div></div>
    <div class="sum-cell"><div class="sk">Sudah Dibayar</div><div class="sv">{{rupiah .TotalPaid}}</div></div>
    <div class="sum-cell terutang"><div class="sk">Terutang</div><div class="sv">{{rupiah .Outstanding}}</div></div>
  </div>
  {{if not .PriceIsSnapshot}}<div class="sum-note">*&nbsp;Kontrak dibuat sebelum pencatatan harga unit — harga mengikuti nilai kontrak (DPP), diskon 0.</div>{{end}}
  {{end}}

  <div class="terbilang">
    <span class="tblabel">Terbilang</span>
    <span class="tbval">{{terbilang .Amount}}</span>
  </div>

  <div class="sig-row">
    <div class="sig-left">
      Dibayar
      <div class="sig-line"></div>
      <div class="tgl">Tgl:</div>
    </div>
    <div class="sig-center">
      Diterima oleh
      <div class="space"></div>
      <div class="who">{{.CompanyName}}</div>
      <div class="dept">BAG. KEUANGAN</div>
    </div>
  </div>

  <footer class="rcpt-footer">
    {{.ReceiptNumber}} &nbsp;|&nbsp; Dicetak: {{fmtDate (now)}}
  </footer>

</div>
</div>

</body>
</html>`

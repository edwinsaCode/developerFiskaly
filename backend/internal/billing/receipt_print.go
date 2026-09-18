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

// terminKindLabel memetakan termin_payments.kind (sale.TerminKind, dibaca
// sebagai string mentah — billing tidak boleh mengimpor sale, lihat catatan
// LoadReceiptPrintData) ke label bisnis customer-facing yang dicetak di
// kolom Keterangan kwitansi. Nilai string HARUS tetap sinkron dengan
// sale.TerminKindLabel; kosong (termin lama tanpa kind, atau kwitansi tanpa
// termin terkait mis. booking/realisasi yang judulnya sudah jelas) → "".
func terminKindLabel(kind string, installmentNo *int) string {
	switch kind {
	case "dp":
		return "DP (Uang Muka)"
	case "installment":
		if installmentNo != nil && *installmentNo > 0 && *installmentNo < 9999 {
			return "Cicilan ke-" + strconv.Itoa(*installmentNo)
		}
		return "Cicilan"
	case "final_payment":
		return "Pelunasan"
	case "land_excess":
		return "Kelebihan Tanah"
	case "bank_disbursement":
		return "Pencairan Dana Bank"
	case "other":
		return "Lainnya"
	default:
		return ""
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
<title>{{if eq .ReceiptType "booking"}}Kwitansi Booking{{else if eq .ReceiptType "realization"}}Kwitansi Biaya Realisasi{{else if eq .ReceiptType "kpr_disbursement"}}Kwitansi Pencairan KPR{{else if eq .ReceiptType "legacy_ar"}}Kwitansi Piutang Proyek Lama{{else}}Bukti Pembayaran{{end}} {{.ReceiptNumber}} — {{.CompanyName}}</title>
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

/* ── Layout halaman: A4 PORTRAIT, 2 area kwitansi (atas/bawah) ──────────────
   Satu lembar HVS A4 dipotong jadi 2 kwitansi identik (rangkap: lembar
   customer + lembar arsip) — konvensi kwitansi developer properti Indonesia.
   .receipt-half punya TINGGI TETAP (297mm halaman ÷ 2, dikurangi margin
   cetak) supaya dua salinan selalu pas dalam SATU halaman A4, tidak pernah
   meluber ke halaman kedua. .receipt-sheet sendiri tetap auto-height (desain
   aslinya tidak diubah) — skrip fitReceipts() di bawah mengecilkannya
   (CSS transform: scale, bukan crop) hanya BILA kontennya melebihi tinggi
   area, jadi kwitansi panjang (banyak baris ringkasan/catatan) tetap utuh
   tercetak, tidak pernah terpotong. */
.page-wrap { padding: 80px 16px 40px; display: flex; flex-direction: column; align-items: center; gap: 4mm; }
.receipt-half {
  width: 100%; max-width: 190mm; height: 128mm; overflow: hidden;
  display: flex; justify-content: center; align-items: flex-start;
}
.cut-line { width: 100%; max-width: 190mm; display: flex; align-items: center; gap: 3mm;
  color: #a89d94; font-size: 0.68rem; letter-spacing: 0.04em; }
.cut-line::before, .cut-line::after { content: ""; flex: 1; border-top: 1px dashed #c9b8ac; }
.receipt-sheet {
  width: 190mm; flex: none; transform-origin: top center;
  background: #fff; border-radius: 8px;
  box-shadow: 0 4px 32px rgba(0,0,0,0.12); padding: 9mm 11mm;
  display: flex; flex-direction: column; font-size: 0.86rem;
}

/* ── Header: logo + kotak perusahaan (kiri) · judul + kotak tgl/no (kanan) ── */
.hdr { display: flex; justify-content: space-between; align-items: flex-start; margin-bottom: 4mm; }
.hdr-left { display: flex; gap: 4mm; align-items: flex-start; }
.hdr-left img { width: 18mm; height: auto; }
.company-box { border: 1.4px solid #2b211b; padding: 2mm 3.5mm; min-width: 58mm; }
.company-box .cname { font-weight: 800; font-size: 0.9rem; letter-spacing: 0.02em; }
.company-box .caddr { font-size: 0.74rem; margin-top: 1mm; line-height: 1.35; color: #4a3d34; }
.hdr-right { text-align: right; }
.doc-title { font-size: 1.3rem; font-weight: 700; letter-spacing: 0.01em; color: #2b211b;
  border-bottom: 2.5px solid #A84522; padding-bottom: 1mm; display: inline-block; }
.meta-boxes { display: flex; gap: 3mm; justify-content: flex-end; margin-top: 2.5mm; }
.meta-box { border: 1.2px solid #2b211b; min-width: 28mm; text-align: center; }
.meta-box .mlabel { font-size: 0.66rem; border-bottom: 1px solid #2b211b; padding: 0.8mm 2mm; background: #faf0eb; }
.meta-box .mval { padding: 1mm 2mm; font-weight: 600; font-size: 0.8rem; }

/* ── Diterima dari (kiri) · Jumlah Terima (kanan) ── */
.row-2 { display: flex; justify-content: space-between; align-items: flex-end; margin-bottom: 3mm; gap: 6mm; }
.received-box { flex: 1; max-width: 95mm; }
.received-box .rlabel { display: inline-block; border: 1.2px solid #2b211b; background: #faf0eb;
  font-size: 0.7rem; font-weight: 700; padding: 0.8mm 2.5mm; margin-bottom: -1px; }
.received-box .rval { border: 1.2px solid #2b211b; padding: 1.5mm 3mm; font-weight: 600; min-height: 9mm; }
.received-box .rval .rid { font-weight: 400; font-size: 0.74rem; color: #4a3d34; }
.amount-terima { border: 1.4px solid #2b211b; text-align: center; min-width: 40mm; }
.amount-terima .alabel { font-size: 0.68rem; font-weight: 700; border-bottom: 1px solid #2b211b;
  padding: 0.8mm 2.5mm; background: #faf0eb; }
.amount-terima .aval { padding: 1.2mm 3mm; font-size: 1.05rem; font-weight: 800; color: #A84522; }

/* ── Tabel rincian (meniru kolom formulir referensi) ── */
table.rincian { width: 100%; border-collapse: collapse; margin-bottom: 1.5mm; }
table.rincian th, table.rincian td { border: 1.2px solid #2b211b; padding: 1.2mm 2.5mm; font-size: 0.76rem; }
table.rincian th { background: #faf0eb; font-weight: 700; text-align: center; }
table.rincian td.num { text-align: right; font-variant-numeric: tabular-nums; }
.total-row { display: flex; justify-content: flex-end; align-items: center; gap: 3mm; margin-bottom: 3mm; }
.total-row .tlabel { border: 1.2px solid #2b211b; padding: 1mm 3.5mm; font-weight: 700; font-size: 0.76rem; background: #faf0eb; }
.total-row .tval { border: 1.2px solid #2b211b; padding: 1mm 3.5mm; min-width: 34mm; text-align: right;
  font-weight: 800; font-variant-numeric: tabular-nums; }

/* ── Ringkasan finansial kontrak ── */
.summary-grid { display: flex; gap: 0; border: 1.4px solid #2b211b; margin-bottom: 3mm; }
.sum-cell { flex: 1; border-right: 1.2px solid #2b211b; text-align: center; }
.sum-cell:last-child { border-right: none; }
.sum-cell .sk { font-size: 0.64rem; font-weight: 700; background: #faf0eb; border-bottom: 1px solid #2b211b; padding: 1mm 1.5mm; }
.sum-cell .sv { padding: 1.2mm; font-weight: 700; font-size: 0.8rem; font-variant-numeric: tabular-nums; }
.sum-cell.terutang .sk { background: #A84522; color: #fff; }
.sum-cell.terutang .sv { color: #A84522; font-size: 0.86rem; }
.sum-note { font-size: 0.62rem; color: #7a6e65; margin: -2mm 0 3mm; }

/* ── Terbilang ── */
.terbilang { display: flex; align-items: stretch; gap: 3mm; margin-bottom: 4mm; }
.terbilang .tblabel { font-size: 0.76rem; padding-top: 1mm; }
.terbilang .tbval { flex: 1; border: 1.2px solid #2b211b; padding: 1mm 3mm; font-style: italic; font-weight: 600; font-size: 0.8rem; }

/* ── Tanda tangan ── */
.sig-row { display: flex; justify-content: space-between; margin-top: auto; padding-top: 2mm; }
.sig-left { font-size: 0.76rem; }
.sig-left .sig-line { margin-top: 6mm; border-top: 1px solid #2b211b; width: 36mm; }
.sig-left .tgl { margin-top: 1.5mm; font-size: 0.72rem; }
.sig-center { text-align: center; font-size: 0.76rem; }
.sig-center .space { height: 8mm; }
.sig-center .who { font-weight: 700; color: #A84522; letter-spacing: 0.03em; }
.sig-center .dept { font-size: 0.7rem; font-weight: 600; color: #4a3d34; }

.rcpt-footer { margin-top: 3mm; padding-top: 1.5mm; border-top: 1px dashed #c9b8ac;
  text-align: center; font-size: 0.6rem; color: #a89d94; }

@page { size: A4 portrait; margin: 10mm; }
@media print {
  .print-bar { display: none !important; }
  body { background: #fff; }
  .page-wrap { padding: 0; gap: 3mm; }
  .receipt-half { height: 128mm; }
  .receipt-sheet { box-shadow: none; }
  .cut-line { display: none; }
}
</style>
</head>
<body>

<div class="print-bar">
  <h1>{{.CompanyName}} — {{if eq .ReceiptType "booking"}}Kwitansi Booking{{else if eq .ReceiptType "realization"}}Kwitansi Biaya Realisasi{{else if eq .ReceiptType "kpr_disbursement"}}Kwitansi Pencairan KPR{{else if eq .ReceiptType "legacy_ar"}}Kwitansi Piutang Proyek Lama{{else if .KindLabel}}Bukti Pembayaran — {{.KindLabel}}{{else}}Bukti Pembayaran{{end}}</h1>
  <button class="btn-print" onclick="window.print()">&#128438; Cetak / Simpan PDF</button>
</div>

<div class="page-wrap">
<div class="receipt-half">{{template "receiptBody" .}}</div>
<div class="cut-line">✂ potong di sini — kwitansi rangkap 2 (pembeli &amp; arsip)</div>
<div class="receipt-half">{{template "receiptBody" .}}</div>
</div>

<script>
// Shrink-to-fit: kwitansi didesain auto-height (tidak diubah) — di sini
// hanya diperkecil (transform: scale, BUKAN crop/overflow) bila kontennya
// melebihi tinggi area separuh halaman, supaya kwitansi panjang tetap utuh
// tercetak (tidak pernah terpotong) sekaligus selalu pas 2-per-halaman A4.
(function () {
  function fitReceipts() {
    document.querySelectorAll('.receipt-sheet').forEach(function (el) {
      el.style.transform = 'none';
      var box = el.parentElement;
      var target = box.clientHeight;
      var natural = el.scrollHeight;
      if (target > 0 && natural > target) {
        el.style.transform = 'scale(' + (target / natural).toFixed(4) + ')';
      }
    });
  }
  if (document.readyState === 'complete') fitReceipts();
  else window.addEventListener('load', fitReceipts);
  window.addEventListener('beforeprint', fitReceipts);
})();
</script>

</body>
</html>
{{define "receiptBody"}}
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
      <span class="doc-title">{{if eq .ReceiptType "booking"}}Kwitansi Booking{{else if eq .ReceiptType "realization"}}Kwitansi Biaya Realisasi{{else if eq .ReceiptType "kpr_disbursement"}}Kwitansi Pencairan KPR{{else if eq .ReceiptType "legacy_ar"}}Kwitansi Piutang Proyek Lama{{else if .KindLabel}}Bukti Pembayaran — {{.KindLabel}}{{else}}Bukti Pembayaran{{end}}</span>
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
        {{/* Gap 2 (UAT 2026-09-08): kwitansi Booking WAJIB tampilkan Terutang
             Rp0 secara eksplisit — TIDAK boleh jatuh ke "—" hanya karena unit
             belum punya SaleContract (booking selalu terjadi sebelum kontrak
             dibuat, ini adalah kondisi NORMAL, bukan data hilang). .Outstanding
             sudah dipaksa domain.Zero utk Booking di GetReceiptPrintData
             (Rule A tanpa syarat) — di sini tinggal dipastikan tampil terlepas
             dari .HasSummary. */}}
        <td class="num">{{if eq .ReceiptType "booking"}}{{rupiah .Outstanding}}{{else if eq .ReceiptType "legacy_ar"}}{{rupiah .LegacyOutstanding}}{{else if .HasChargeSummary}}{{rupiah .ChargeOutstanding}}{{else if .HasSummary}}{{rupiah .Outstanding}}{{else}}&mdash;{{end}}</td>
        <td class="num">{{rupiah .Amount}}</td>
        <td>{{if eq .ReceiptType "legacy_ar"}}Piutang Proyek Lama{{if .LegacySourceLabel}} — {{.LegacySourceLabel}}{{end}}{{if .Notes}}<br/><span style="font-size:0.76rem;color:#4a3d34">{{.Notes}}</span>{{end}}<br/><span style="font-size:0.74rem;color:#4a3d34">{{paymentTypeLabel .PaymentType}} · {{bankLabel .BankAccountCode}}</span>{{else}}{{if and .KindLabel (eq .ReceiptType "house_payment")}}<strong>{{.KindLabel}}</strong> — {{end}}Unit {{.UnitCode}} — {{.UnitType}}, {{.ProjectName}}{{if .Notes}}<br/><span style="font-size:0.76rem;color:#4a3d34">{{.Notes}}</span>{{end}}<br/><span style="font-size:0.74rem;color:#4a3d34">{{paymentTypeLabel .PaymentType}} · {{bankLabel .BankAccountCode}}</span>{{end}}</td>
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

  {{if .HasLegacySummary}}
  <!-- Ringkasan Piutang Proyek Lama (W-7 perluasan, requirement #4) — SATU
       sumber: legacy_receivables/legacy_receivable_payments, dibaca langsung
       lewat LEFT JOIN di LoadReceiptPrintData. "Terutang" WAJIB dihitung
       ulang dari saldo piutang terkini SETELAH pembayaran ini diposting —
       bukan jumlah piutang awal (requirement #4: dilarang menampilkan
       piutang awal sebagai sisa pada pembayaran parsial). -->
  <div class="summary-grid">
    <div class="sum-cell"><div class="sk">{{if .LegacySourceLabel}}{{.LegacySourceLabel}}{{else}}Piutang Proyek Lama{{end}}</div><div class="sv">&nbsp;</div></div>
    <div class="sum-cell"><div class="sk">Piutang Awal</div><div class="sv">{{rupiah .LegacyOriginalAmount}}</div></div>
    <div class="sum-cell"><div class="sk">Pembayaran Ini</div><div class="sv">{{rupiah .Amount}}</div></div>
    <div class="sum-cell terutang"><div class="sk">Terutang</div><div class="sv">{{rupiah .LegacyOutstanding}}</div></div>
  </div>
  {{end}}

  {{if and .HasSummary (not .HasChargeSummary)}}
  <!-- Ringkasan finansial kontrak — SATU sumber: sale.ContractFinancialSummary.
       Nilai NOL tetap ditampilkan (requirement). Terutang = nilai kontrak −
       seluruh pembayaran yang telah diakui sistem (termasuk pencairan KPR).
       UAT 2026-09-03 #1: bila pembayaran ini ditarget ke SATU jadwal (mis.
       Kelebihan Tanah), sel "Terutang" WAJIB memakai sisa jadwal itu sendiri
       — bukan Outstanding gabungan seluruh kontrak (bug: kwitansi Kelebihan
       Tanah menampilkan sisa harga rumah). -->
  <div class="summary-grid">
    <div class="sum-cell"><div class="sk">Harga Unit{{if not .PriceIsSnapshot}}*{{end}}</div><div class="sv">{{rupiah .UnitPrice}}</div></div>
    <div class="sum-cell"><div class="sk">Diskon</div><div class="sv">{{rupiah .Discount}}</div></div>
    <div class="sum-cell"><div class="sk">Nilai Kontrak</div><div class="sv">{{rupiah .NetContract}}</div></div>
    <div class="sum-cell"><div class="sk">Sudah Dibayar</div><div class="sv">{{rupiah .TotalPaid}}</div></div>
    {{if .HasScheduleOutstanding}}
    <div class="sum-cell terutang"><div class="sk">Sisa {{.ScheduleTypeLabel}}</div><div class="sv">{{rupiah .ScheduleOutstanding}}</div></div>
    {{else}}
    <div class="sum-cell terutang"><div class="sk">Terutang</div><div class="sv">{{rupiah .Outstanding}}</div></div>
    {{end}}
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
{{end}}`

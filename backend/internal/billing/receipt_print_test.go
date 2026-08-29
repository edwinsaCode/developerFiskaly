package billing

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"esaproperti/internal/domain"
)

// ── Hardening: ringkasan finansial di kwitansi ────────────────────────────────

func TestRenderReceiptPrint_SummaryVisible_ZeroShown(t *testing.T) {
	var buf bytes.Buffer
	amt, _ := domain.NewMoney("25000000")
	price, _ := domain.NewMoney("500000000")
	net, _ := domain.NewMoney("500000000")
	paid, _ := domain.NewMoney("480000000")
	out, _ := domain.NewMoney("20000000")
	data := &ReceiptPrintData{
		ReceiptNumber: "KWT/2026/000099",
		Amount:        amt,
		ReceivedAt:    time.Date(2026, 7, 22, 0, 0, 0, 0, time.UTC),
		CompanyName:   "PT Nata Alam Raya",
		BuyerName:     "Siti Rahma",
		ProjectName:   "PS1 Res",
		UnitCode:      "PS1-02",
		UnitType:      "36/72",
		// Ringkasan: diskon NOL — harus tetap tampil (requirement klien).
		HasSummary:      true,
		PriceIsSnapshot: true,
		UnitPrice:       price,
		Discount:        domain.Zero,
		NetContract:     net,
		TotalPaid:       paid,
		Outstanding:     out,
	}
	if err := RenderReceiptPrint(&buf, data); err != nil {
		t.Fatalf("render: %v", err)
	}
	html := buf.String()
	for _, want := range []string{
		"Harga Unit", "Diskon", "Nilai Kontrak", "Sudah Dibayar", "Terutang",
		"Rp 500.000.000", // harga unit
		"Rp 480.000.000", // sudah dibayar
		"Rp 20.000.000",  // terutang (kekurangan customer pasca-pencairan bank)
		"Rp 0",           // diskon nol TETAP TAMPIL
	} {
		if !strings.Contains(html, want) {
			t.Errorf("HTML kwitansi tidak memuat %q", want)
		}
	}
}

func TestRenderReceiptPrint_NoContract_SummaryHidden(t *testing.T) {
	var buf bytes.Buffer
	amt, _ := domain.NewMoney("1000000")
	data := &ReceiptPrintData{
		ReceiptNumber: "KWT/2026/000100",
		Amount:        amt,
		ReceivedAt:    time.Date(2026, 7, 22, 0, 0, 0, 0, time.UTC),
		CompanyName:   "PT Nata Alam Raya",
		ProjectName:   "PS1 Res",
		UnitCode:      "PS1-09",
		HasSummary:    false, // termin tanpa kontrak
	}
	if err := RenderReceiptPrint(&buf, data); err != nil {
		t.Fatalf("render: %v", err)
	}
	if strings.Contains(buf.String(), "Nilai Kontrak") {
		t.Error("ringkasan kontrak tidak boleh tampil untuk termin tanpa kontrak")
	}
}

// ── E-2: kwitansi pencairan KPR (KWD) ────────────────────────────────────────
//
// Pembayar KWD adalah BANK PENYALUR, bukan customer — itulah alasan KWD punya
// seri dokumen sendiri. Bila kwitansi pencairan tercetak atas nama customer,
// dokumennya salah menyatakan siapa yang menyetor uang.

func TestRenderReceiptPrint_KPRDisbursement_BankIsPayer(t *testing.T) {
	var buf bytes.Buffer
	amt, _ := domain.NewMoney("340000000")
	data := &ReceiptPrintData{
		ReceiptType:        ReceiptTypeKPRDisbursement,
		ReceiptNumber:      "KWD/2026/000001",
		Amount:             amt,
		ReceivedAt:         time.Date(2026, 8, 13, 0, 0, 0, 0, time.UTC),
		CompanyName:        "PT Nata Alam Raya",
		BuyerName:          "Siti Rahma",
		BuyerID:            "5271xxxxxxxx0001",
		DisbursingBankName: "Bank BTN KC Mataram",
		ProjectName:        "PS1 Res",
		UnitCode:           "PS1-02",
		UnitType:           "36/72",
	}
	if err := RenderReceiptPrint(&buf, data); err != nil {
		t.Fatalf("render: %v", err)
	}
	html := buf.String()
	for _, want := range []string{
		"Kwitansi Pencairan KPR",        // judul khusus, bukan "Bukti Pembayaran"
		"Bank BTN KC Mataram",           // penyetor = bank penyalur (dari master)
		"Pencairan KPR a.n. Siti Rahma", // customer = keterangan, bukan penyetor
	} {
		if !strings.Contains(html, want) {
			t.Errorf("HTML KWD tidak memuat %q", want)
		}
	}
	if strings.Contains(html, "Bukti Pembayaran") {
		t.Error("KWD tidak boleh memakai judul generik \"Bukti Pembayaran\"")
	}
}

// Bank tidak terisi di master → kwitansi TIDAK boleh kehilangan penyetor:
// jatuh ke nama pembeli (fallback), bukan kosong.
func TestRenderReceiptPrint_KPRDisbursement_NoBank_FallsBackToBuyer(t *testing.T) {
	var buf bytes.Buffer
	amt, _ := domain.NewMoney("185000000")
	data := &ReceiptPrintData{
		ReceiptType:   ReceiptTypeKPRDisbursement,
		ReceiptNumber: "KWD/2026/000002",
		Amount:        amt,
		ReceivedAt:    time.Date(2026, 8, 13, 0, 0, 0, 0, time.UTC),
		CompanyName:   "PT Nata Alam Raya",
		BuyerName:     "Alex Ferguson",
		ProjectName:   "PS1 Res",
		UnitCode:      "A-01",
	}
	if err := RenderReceiptPrint(&buf, data); err != nil {
		t.Fatalf("render: %v", err)
	}
	html := buf.String()
	if !strings.Contains(html, "Kwitansi Pencairan KPR") {
		t.Error("judul KWD hilang saat bank tidak terisi")
	}
	if !strings.Contains(html, "Alex Ferguson") {
		t.Error("tanpa nama bank, penyetor harus jatuh ke nama pembeli — kwitansi tidak boleh tanpa penyetor")
	}
}

// Regresi: cabang KWD tidak boleh menyentuh KWT/KWB/KWR.
func TestRenderReceiptPrint_OtherTypes_TitlesUnchanged(t *testing.T) {
	amt, _ := domain.NewMoney("5000000")
	cases := []struct {
		rtype     ReceiptType
		wantTitle string
	}{
		{ReceiptTypeHousePayment, "Bukti Pembayaran"},
		{ReceiptTypeBooking, "Kwitansi Booking"},
		{ReceiptTypeRealization, "Kwitansi Biaya Realisasi"},
	}
	for _, tc := range cases {
		t.Run(string(tc.rtype), func(t *testing.T) {
			var buf bytes.Buffer
			data := &ReceiptPrintData{
				ReceiptType:   tc.rtype,
				ReceiptNumber: "XXX/2026/000001",
				Amount:        amt,
				ReceivedAt:    time.Date(2026, 8, 13, 0, 0, 0, 0, time.UTC),
				CompanyName:   "PT Nata Alam Raya",
				BuyerName:     "Budi Santoso",
				ProjectName:   "PS1 Res",
				UnitCode:      "A-01",
				// Nama bank terisi pun TIDAK boleh muncul sebagai penyetor
				// pada tipe selain KWD.
				DisbursingBankName: "Bank BTN KC Mataram",
			}
			if err := RenderReceiptPrint(&buf, data); err != nil {
				t.Fatalf("render: %v", err)
			}
			html := buf.String()
			if !strings.Contains(html, tc.wantTitle) {
				t.Errorf("judul %s berubah — harusnya %q", tc.rtype, tc.wantTitle)
			}
			if strings.Contains(html, "Kwitansi Pencairan KPR") {
				t.Errorf("%s salah memakai judul KWD", tc.rtype)
			}
			if strings.Contains(html, "Bank BTN KC Mataram") {
				t.Errorf("%s tidak boleh mencetak bank penyalur sebagai penyetor", tc.rtype)
			}
		})
	}
}

// ── Bahasa kwitansi: untuk customer, bukan untuk buku besar ───────────────────

// Kwitansi adalah lembar yang diserahkan ke pembeli. Dulu di bawah judulnya
// tercetak keterangan perlakuan akuntansi — "Titipan Realisasi — di luar harga
// unit, bukan pendapatan" dan sejenisnya. Pembeli membaca "bukan pendapatan"
// tepat di atas uang yang baru saja ia bayarkan, lalu bertanya apakah uangnya
// masuk hitungan. Uji ini menjaga agar bahasa buku besar tidak kembali ke
// dokumen customer — tanpa mengubah perlakuan akuntansinya, yang tetap hidup
// di jurnal dan di layar internal.
func TestRenderReceiptPrint_TanpaJargonAkuntansi(t *testing.T) {
	amt, _ := domain.NewMoney("5000000")
	terlarang := []string{
		"bukan pendapatan",
		"di luar harga unit",
		"kewajiban",
		"esaProperti",
	}
	for _, rt := range []ReceiptType{ReceiptTypeBooking, ReceiptTypeRealization, ReceiptTypeKPRDisbursement, ReceiptTypeHousePayment} {
		var buf bytes.Buffer
		data := &ReceiptPrintData{
			ReceiptType:   rt,
			ReceiptNumber: "KWT/2026/000101",
			Amount:        amt,
			ReceivedAt:    time.Date(2026, 8, 19, 0, 0, 0, 0, time.UTC),
			CompanyName:   "PT Nata Alam Raya",
			BuyerName:     "Udin",
			ProjectName:   "PS1 Res",
			UnitCode:      "C-01",
			UnitType:      "36/72",
		}
		if err := RenderReceiptPrint(&buf, data); err != nil {
			t.Fatalf("render %s: %v", rt, err)
		}
		html := buf.String()
		for _, bad := range terlarang {
			if strings.Contains(strings.ToLower(html), strings.ToLower(bad)) {
				t.Errorf("kwitansi %s masih memuat bahasa internal %q", rt, bad)
			}
		}
		// Judulnya tetap memberi tahu ini pembayaran apa — yang dihapus hanya
		// keterangan akuntansinya, bukan identitas dokumennya.
		if !strings.Contains(html, "KWT/2026/000101") {
			t.Errorf("kwitansi %s kehilangan nomor dokumen", rt)
		}
	}
}

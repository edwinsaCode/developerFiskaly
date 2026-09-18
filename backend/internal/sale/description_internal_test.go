package sale

import "testing"

// Readability accounting (2026-09-18): deskripsi Jurnal/Buku Besar untuk
// transaksi penjualan/piutang harus menunjukkan kode Unit kanonik, bukan ID
// mentah — DescribeWithUnit adalah satu-satunya titik format yang dipakai di
// semua lokasi (booking fee, Akad, termin/collection, Kelebihan Tanah
// bundled). Test ini adalah source-of-truth regresi untuk format itu sendiri.

func TestDescribeWithUnit(t *testing.T) {
	cases := []struct {
		name     string
		base     string
		unitCode string
		want     string
	}{
		{"kode tersedia", "Booking fee", "A-15", "Booking fee — Unit A-15"},
		{"kode kosong dipertahankan apa adanya", "Booking fee", "", "Booking fee"},
		{"kode non-konvensional tetap dipakai apa adanya (tanpa parsing Blok)", "Akad HPP", "LITHOS-A01", "Akad HPP — Unit LITHOS-A01"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := DescribeWithUnit(c.base, c.unitCode)
			if got != c.want {
				t.Errorf("DescribeWithUnit(%q, %q) = %q, want %q", c.base, c.unitCode, got, c.want)
			}
		})
	}
}

// TestBuildReceiveDescription_UnitCode memverifikasi deskripsi termin/jurnal
// penerimaan (DP/Cicilan/Pelunasan/KPR/Kelebihan Tanah — satu builder untuk
// semuanya) menyisipkan kode unit, dan tetap menjaga urutan ref/notes yang
// sudah ada (tidak boleh regresi format lama untuk bagian itu).
func TestBuildReceiveDescription_UnitCode(t *testing.T) {
	req := ReceivePaymentRequest{Reference: "TF-001", Notes: "transfer BCA"}

	dp := buildReceiveDescription(req, TerminKindDP, nil, "A-15")
	if want := "Penerimaan DP (Uang Muka) — Unit A-15 ref TF-001 — transfer BCA"; dp != want {
		t.Errorf("DP description = %q, want %q", dp, want)
	}

	n := 3
	installment := buildReceiveDescription(ReceivePaymentRequest{}, TerminKindInstallment, &n, "A-15")
	if want := "Penerimaan Cicilan ke-3 — Unit A-15"; installment != want {
		t.Errorf("installment description = %q, want %q", installment, want)
	}

	final := buildReceiveDescription(ReceivePaymentRequest{}, TerminKindFinalPayment, nil, "A-15")
	if want := "Penerimaan Pelunasan — Unit A-15"; final != want {
		t.Errorf("final payment description = %q, want %q", final, want)
	}

	kpr := buildReceiveDescription(ReceivePaymentRequest{}, TerminKindBankDisbursement, nil, "A-15")
	if want := "Penerimaan Pencairan Dana Bank — Unit A-15"; kpr != want {
		t.Errorf("KPR disbursement description = %q, want %q", kpr, want)
	}

	landExcess := buildReceiveDescription(ReceivePaymentRequest{}, TerminKindLandExcess, nil, "A-15")
	if want := "Penerimaan Kelebihan Tanah — Unit A-15"; landExcess != want {
		t.Errorf("kelebihan tanah description = %q, want %q", landExcess, want)
	}

	// Unit tanpa kode (mis. FindUnitSaleInfo gagal, best-effort) → deskripsi
	// generik lama dipertahankan, TIDAK dipaksakan (ketentuan #6).
	noCode := buildReceiveDescription(ReceivePaymentRequest{}, TerminKindDP, nil, "")
	if want := "Penerimaan DP (Uang Muka)"; noCode != want {
		t.Errorf("description tanpa unit code = %q, want %q", noCode, want)
	}
}

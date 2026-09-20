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
		project  string
		unitCode string
		want     string
	}{
		{"proyek + kode", "Booking fee", "Nata Alam", "A-15", "Booking fee — Proyek Nata Alam — Unit A-15"},
		{"kode tanpa nama proyek (best-effort gagal)", "Booking fee", "", "A-15", "Booking fee — Unit A-15"},
		{"kode kosong dipertahankan apa adanya", "Booking fee", "Nata Alam", "", "Booking fee"},
		{"kode non-konvensional tetap dipakai apa adanya (tanpa parsing Blok)", "Akad HPP", "Nata Alam", "LITHOS-A01", "Akad HPP — Proyek Nata Alam — Unit LITHOS-A01"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := DescribeWithUnit(c.base, c.project, c.unitCode)
			if got != c.want {
				t.Errorf("DescribeWithUnit(%q, %q, %q) = %q, want %q", c.base, c.project, c.unitCode, got, c.want)
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

	dp := buildReceiveDescription(req, TerminKindDP, nil, "Nata Alam", "A-15")
	if want := "Penerimaan DP (Uang Muka) — Proyek Nata Alam — Unit A-15 ref TF-001 — transfer BCA"; dp != want {
		t.Errorf("DP description = %q, want %q", dp, want)
	}

	n := 3
	installment := buildReceiveDescription(ReceivePaymentRequest{}, TerminKindInstallment, &n, "Nata Alam", "A-15")
	if want := "Penerimaan Cicilan ke-3 — Proyek Nata Alam — Unit A-15"; installment != want {
		t.Errorf("installment description = %q, want %q", installment, want)
	}

	final := buildReceiveDescription(ReceivePaymentRequest{}, TerminKindFinalPayment, nil, "Nata Alam", "A-15")
	if want := "Penerimaan Pelunasan — Proyek Nata Alam — Unit A-15"; final != want {
		t.Errorf("final payment description = %q, want %q", final, want)
	}

	kpr := buildReceiveDescription(ReceivePaymentRequest{}, TerminKindBankDisbursement, nil, "Nata Alam", "A-15")
	if want := "Penerimaan Pencairan Dana Bank — Proyek Nata Alam — Unit A-15"; kpr != want {
		t.Errorf("KPR disbursement description = %q, want %q", kpr, want)
	}

	landExcess := buildReceiveDescription(ReceivePaymentRequest{}, TerminKindLandExcess, nil, "Nata Alam", "A-15")
	if want := "Penerimaan Kelebihan Tanah — Proyek Nata Alam — Unit A-15"; landExcess != want {
		t.Errorf("kelebihan tanah description = %q, want %q", landExcess, want)
	}

	// Unit tanpa kode (mis. FindUnitSaleInfo gagal, best-effort) → deskripsi
	// generik lama dipertahankan, TIDAK dipaksakan (ketentuan #6).
	noCode := buildReceiveDescription(ReceivePaymentRequest{}, TerminKindDP, nil, "", "")
	if want := "Penerimaan DP (Uang Muka)"; noCode != want {
		t.Errorf("description tanpa unit code = %q, want %q", noCode, want)
	}
}

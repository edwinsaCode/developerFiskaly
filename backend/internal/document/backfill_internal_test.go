package document

// W-3.3 — klasifikasi jurnal kas historis (murni, tanpa DB).
//
// Yang diuji di sini adalah SATU keputusan: dari agregat baris kas sebuah
// jurnal lama, jenis dokumen apa yang jujur diterbitkan. Salah menyimpulkan
// arah berarti menerbitkan bukti kas MASUK untuk uang yang justru keluar —
// kesalahan yang tidak terkoreksi oleh apa pun di hilir karena dokumen bersifat
// append-only.

import (
	"context"
	"testing"
)

func row(debit, credit string, lines int) cashJournalRow {
	return cashJournalRow{ID: 1, Source: "system", Debit: debit, Credit: credit, CashLines: lines}
}

func TestClassifyCashJournal_ArahMenentukanJenis(t *testing.T) {
	cases := []struct {
		name     string
		r        cashJournalRow
		wantType string
		wantAmt  string
	}{
		{"kas masuk murni", row("5000000", "0", 1), TypeCashIn, "5000000"},
		{"kas keluar murni", row("0", "750000", 1), TypeCashOut, "750000"},
		{
			// Jurnal campuran: kas bertambah 2jt (terima 3jt, bayar biaya bank 1jt
			// dari kas yang sama). Yang dibuktikan adalah pergerakan BERSIH.
			"campuran, bersih masuk", row("3000000", "1000000", 2), TypeCashIn, "2000000",
		},
		{"campuran, bersih keluar", row("1000000", "4000000", 2), TypeCashOut, "3000000"},
		{
			// Setor tunai ke bank: kas berkurang, bank bertambah, posisi kas
			// perusahaan tidak berubah → memo transfer internal, bukan BKM/BKK.
			"transfer antar kas/bank", row("2000000", "2000000", 2), TypeInternalTransfer, "2000000",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			gotType, gotAmt, err := classifyCashJournal(c.r)
			if err != nil {
				t.Fatalf("tak terduga error: %v", err)
			}
			if gotType != c.wantType {
				t.Errorf("jenis = %q, want %q", gotType, c.wantType)
			}
			if gotAmt.String() != c.wantAmt {
				t.Errorf("nominal = %s, want %s", gotAmt, c.wantAmt)
			}
		})
	}
}

func TestClassifyCashJournal_SumberMempertajamSisiKeluar(t *testing.T) {
	cases := map[string]string{
		"cancellation": TypeCustomerRefund,
		"notary":       TypeThirdPartyPayout,
		"charge":       TypeThirdPartyPayout,
		"commission":   TypeCashOut,
		"system":       TypeCashOut,
		"recurring":    TypeCashOut,
	}
	for source, want := range cases {
		t.Run(source, func(t *testing.T) {
			r := row("0", "1000000", 1)
			r.Source = source
			got, _, err := classifyCashJournal(r)
			if err != nil {
				t.Fatalf("tak terduga error: %v", err)
			}
			if got != want {
				t.Errorf("source %q → %q, want %q", source, got, want)
			}
		})
	}
}

// Sisi MASUK tidak dipertajam oleh source: seluruh seri kas masuk selain BKM
// (KWT/KWB/KWR/KWD) lahir dari baris bisnis dan sudah ditangani jalur "tautkan".
// Menebak KWT dari `source` untuk jurnal yang kwitansinya memang tidak ada akan
// menciptakan kwitansi customer yang tidak pernah diserahkan ke siapa pun.
func TestClassifyCashJournal_SisiMasukSelaluBKM(t *testing.T) {
	for _, source := range []string{"system", "sale", "termin", "cancellation", "notary"} {
		r := row("1000000", "0", 1)
		r.Source = source
		got, _, err := classifyCashJournal(r)
		if err != nil {
			t.Fatalf("source %q: %v", source, err)
		}
		if got != TypeCashIn {
			t.Errorf("source %q → %q, want %q", source, got, TypeCashIn)
		}
	}
}

// Jurnal pembalik dapat JR apa pun arahnya — yang dibuktikan adalah pembatalan
// pergerakan sebelumnya, bukan pergerakan kas baru (D-W3-5).
func TestClassifyCashJournal_PembalikSelaluJR(t *testing.T) {
	for _, r := range []cashJournalRow{
		row("1000000", "0", 1),
		row("0", "1000000", 1),
	} {
		r.IsReversing = true
		r.Source = "reversal"
		got, amt, err := classifyCashJournal(r)
		if err != nil {
			t.Fatalf("tak terduga error: %v", err)
		}
		if got != TypeJournalReversal {
			t.Errorf("pembalik → %q, want %q", got, TypeJournalReversal)
		}
		if amt.String() != "1000000" {
			t.Errorf("nominal pembalik = %s, want 1000000", amt)
		}
	}
}

// Jurnal kas bernilai nol tidak boleh ditebak: tidak ada nominal yang bisa
// ditulis di lembar bukti. Ditandai untuk penanganan manual.
func TestClassifyCashJournal_NolDitolakBukanDitebak(t *testing.T) {
	for _, r := range []cashJournalRow{
		row("0", "0", 1),
		row("0", "0", 3),
	} {
		if _, _, err := classifyCashJournal(r); err == nil {
			t.Errorf("jurnal kas nol (%d baris) harus ditandai, bukan diklasifikasi", r.CashLines)
		}
	}
}

// Backfill tanpa resolver dokumen bisnis HARUS gagal: tanpa itu, jurnal yang
// kwitansinya sudah ada akan menerima dokumen retro kedua — persis larangan
// "satu cash movement, dua dokumen".
func TestBackfillCashDocuments_TanpaResolverDitolak(t *testing.T) {
	_, err := BackfillCashDocuments(context.Background(), nil, 1, false, nil)
	if err == nil {
		t.Fatal("backfill tanpa resolver harus gagal")
	}
}

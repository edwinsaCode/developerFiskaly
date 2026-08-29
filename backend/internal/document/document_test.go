package document

import (
	"strings"
	"testing"
	"time"
)

func tipe(prefix, format string, pad uint8, reset ResetPolicy) *DocumentType {
	return &DocumentType{
		Code: "x", Name: "X", Prefix: prefix,
		NumberFormat: format, Padding: pad, ResetPolicy: reset, IsActive: true,
	}
}

func at(y int, m time.Month, d int) time.Time {
	return time.Date(y, m, d, 10, 0, 0, 0, time.UTC)
}

// ── Format ──────────────────────────────────────────────────────────────────

func TestFormatNumber_FormatBawaan(t *testing.T) {
	got, err := formatNumber(tipe("KWT", "{prefix}/{year}/{seq}", 6, ResetYearly), 42, at(2026, time.August, 7))
	if err != nil {
		t.Fatalf("format: %v", err)
	}
	if want := "KWT/2026/000042"; got != want {
		t.Fatalf("nomor = %q, mau %q", got, want)
	}
}

// Kesinambungan dengan dokumen yang SUDAH dicetak: bentuk nomor bawaan engine
// baru harus identik dengan yang dihasilkan engine lama (fmt "%s/%d/%06d").
// Kalau tes ini merah, ada dokumen historis yang jadi tidak sebentuk.
func TestFormatNumber_SamaDenganEngineLama(t *testing.T) {
	for _, c := range []struct {
		prefix string
		seq    uint64
		want   string
	}{
		{"KWT", 1, "KWT/2026/000001"},
		{"KWB", 999999, "KWB/2026/999999"},
		{"KWR", 1234, "KWR/2026/001234"},
		{"MTI", 7, "MTI/2026/000007"},
		{"INV", 88, "INV/2026/000088"},
	} {
		got, err := formatNumber(tipe(c.prefix, "{prefix}/{year}/{seq}", 6, ResetYearly), c.seq, at(2026, time.January, 2))
		if err != nil {
			t.Fatalf("%s: %v", c.prefix, err)
		}
		if got != c.want {
			t.Fatalf("%s → %q, mau %q", c.prefix, got, c.want)
		}
	}
}

func TestFormatNumber_BulanDanPaddingKustom(t *testing.T) {
	got, err := formatNumber(tipe("BKK", "{prefix}-{year}{month}-{seq}", 4, ResetYearly), 9, at(2026, time.March, 15))
	if err != nil {
		t.Fatalf("format: %v", err)
	}
	if want := "BKK-202603-0009"; got != want {
		t.Fatalf("nomor = %q, mau %q", got, want)
	}
}

// Seri yang melampaui padding TIDAK boleh dipotong — nomor 1.000.000 pada
// padding 6 harus tumbuh jadi 7 digit, bukan berputar kembali ke 000000.
func TestFormatNumber_MelewatiPaddingTidakTerpotong(t *testing.T) {
	got, err := formatNumber(tipe("KWT", "{prefix}/{year}/{seq}", 6, ResetYearly), 1_000_000, at(2026, time.August, 7))
	if err != nil {
		t.Fatalf("format: %v", err)
	}
	if want := "KWT/2026/1000000"; got != want {
		t.Fatalf("nomor = %q, mau %q", got, want)
	}
}

// Tanpa {seq} setiap dokumen akan bernomor sama. Harus ditolak.
func TestFormatNumber_TanpaSeqDitolak(t *testing.T) {
	if _, err := formatNumber(tipe("KWT", "{prefix}/{year}", 6, ResetYearly), 1, at(2026, time.August, 7)); err == nil {
		t.Fatal("format tanpa {seq} harus ditolak")
	}
}

func TestFormatNumber_PlaceholderTakDikenalDitolak(t *testing.T) {
	if _, err := formatNumber(tipe("KWT", "{prefix}/{quarter}/{seq}", 6, ResetYearly), 1, at(2026, time.August, 7)); err == nil {
		t.Fatal("placeholder tak dikenal harus ditolak")
	}
}

// ── Kebijakan reset ─────────────────────────────────────────────────────────

func TestFiscalYear_YearlyIkutTahunPenerbitan(t *testing.T) {
	if got := fiscalYearFor(ResetYearly, at(2026, time.December, 31)); got != 2026 {
		t.Fatalf("fiscal year = %d, mau 2026", got)
	}
	if got := fiscalYearFor(ResetYearly, at(2027, time.January, 1)); got != 2027 {
		t.Fatalf("fiscal year = %d, mau 2027", got)
	}
}

// ResetNever memakai kunci tahun 0 supaya bentuk primary key tetap seragam —
// bukan tahun berjalan, yang justru akan diam-diam mereset seri tiap tahun.
func TestFiscalYear_NeverSelaluNol(t *testing.T) {
	if got := fiscalYearFor(ResetNever, at(2026, time.December, 31)); got != 0 {
		t.Fatalf("fiscal year = %d, mau 0", got)
	}
	if got := fiscalYearFor(ResetNever, at(2030, time.June, 1)); got != 0 {
		t.Fatalf("fiscal year = %d, mau 0", got)
	}
}

func TestResetPolicy_Valid(t *testing.T) {
	for _, p := range []ResetPolicy{ResetYearly, ResetNever} {
		if !p.Valid() {
			t.Fatalf("%q harus valid", p)
		}
	}
	for _, p := range []ResetPolicy{"", "monthly", "Yearly "} {
		if p.Valid() {
			t.Fatalf("%q tidak boleh valid", p)
		}
	}
}

// ── Validasi master ─────────────────────────────────────────────────────────

func TestValidateType_MenolakKonfigurasiRusak(t *testing.T) {
	base := func() *DocumentType {
		return &DocumentType{Code: "voucher", Name: "Voucher Kas", Prefix: "BKK",
			NumberFormat: "{prefix}/{year}/{seq}", ResetPolicy: ResetYearly, Padding: 6}
	}
	cases := []struct {
		name   string
		mutate func(*DocumentType)
	}{
		{"kode kosong", func(d *DocumentType) { d.Code = "" }},
		{"nama kosong", func(d *DocumentType) { d.Name = "" }},
		{"prefix kosong", func(d *DocumentType) { d.Prefix = "  " }},
		{"kebijakan reset asing", func(d *DocumentType) { d.ResetPolicy = "monthly" }},
		{"padding nol", func(d *DocumentType) { d.Padding = 0 }},
		{"padding kebesaran", func(d *DocumentType) { d.Padding = 13 }},
		{"format tanpa seq", func(d *DocumentType) { d.NumberFormat = "{prefix}/{year}" }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d := base()
			c.mutate(d)
			if err := validateType(d); err == nil {
				t.Fatalf("%s harus ditolak", c.name)
			}
		})
	}
	if err := validateType(base()); err != nil {
		t.Fatalf("konfigurasi sah ditolak: %v", err)
	}
}

func TestNormalizeCode(t *testing.T) {
	for in, want := range map[string]string{
		"  House_Payment ": "house_payment",
		"INVOICE":          "invoice",
		"":                 "",
	} {
		if got := NormalizeCode(in); got != want {
			t.Fatalf("NormalizeCode(%q) = %q, mau %q", in, got, want)
		}
	}
}

// ── Seed ────────────────────────────────────────────────────────────────────

// Seed Go dan bagian seed migrasi (000065 bagian 4, 000066 bagian 3) harus
// menghasilkan baris yang sama. Kalau salah satu berubah sendiri, tenant lama
// dan tenant baru akan bernomor dengan aturan berbeda — jenis bug yang baru
// ketahuan berbulan-bulan kemudian.
func TestDefaultTypes_KonsistenDenganMigrasi(t *testing.T) {
	want := map[string]string{
		// 000065 — dokumen yang lahir dari baris bisnis.
		"house_payment":     "KWT",
		"booking":           "KWB",
		"realization":       "KWR",
		"internal_transfer": "MTI",
		"invoice":           "INV",
		// 000066 — katalog kas W-3, disusun menurut economic ownership.
		"kpr_disbursement":   "KWD", // pembayar = bank
		"cash_in":            "BKM", // penerimaan perusahaan non-customer
		"cash_out":           "BKK", // uang milik perusahaan
		"third_party_payout": "BTP", // uang titipan milik pihak ketiga
		"customer_refund":    "RFC", // uang milik customer
		"journal_reversal":   "JR",  // pembalikan
	}
	if len(defaultTypes) != len(want) {
		t.Fatalf("jumlah jenis default = %d, mau %d", len(defaultTypes), len(want))
	}
	for _, dt := range defaultTypes {
		prefix, ok := want[dt.Code]
		if !ok {
			t.Fatalf("jenis %q tidak ada di daftar yang diharapkan", dt.Code)
		}
		if dt.Prefix != prefix {
			t.Fatalf("%s prefix = %q, mau %q", dt.Code, dt.Prefix, prefix)
		}
		if dt.Code != NormalizeCode(dt.Code) {
			t.Fatalf("kode %q harus sudah ternormalisasi", dt.Code)
		}
		if strings.TrimSpace(dt.Name) == "" {
			t.Fatalf("%s tidak punya nama", dt.Code)
		}
	}
}

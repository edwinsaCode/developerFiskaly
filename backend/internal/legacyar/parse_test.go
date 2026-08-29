package legacyar

import (
	"bytes"
	"testing"

	"github.com/xuri/excelize/v2"

	"esaproperti/internal/domain"
)

// TestParseAmount menjaga Invariant #2 di titik masuknya data.
//
// Angka dari Excel datang dalam banyak bentuk — "1.000.000", "1,000,000.50",
// "Rp 300.000.000", "300000000". Semuanya diproses sebagai STRING sampai jadi
// domain.Money. Sekali saja lewat float64, "10000000.01" berhenti bisa
// dipercaya, dan tidak ada yang akan menyadarinya sampai neraca meleset satu
// sen sebulan kemudian.
func TestParseAmount(t *testing.T) {
	cases := []struct {
		in      string
		want    string
		wantErr bool
	}{
		{in: "300000000", want: "300000000"},
		{in: "300.000.000", want: "300000000"},
		{in: "300,000,000", want: "300000000"},
		{in: "Rp 300.000.000", want: "300000000"},
		{in: "Rp300.000.000,50", want: "300000000.5"},
		{in: "300,000,000.50", want: "300000000.5"},
		{in: "1.000.000", want: "1000000"},
		{in: "1,000.50", want: "1000.5"},
		{in: "1000000,25", want: "1000000.25"},
		{in: " 250000000 ", want: "250000000"},
		{in: "10000000.01", want: "10000000.01"},
		{in: "", wantErr: true},
		{in: "-", wantErr: true},
		{in: "abc", wantErr: true},
	}
	for _, c := range cases {
		got, err := ParseAmount(c.in)
		if c.wantErr {
			if err == nil {
				t.Errorf("ParseAmount(%q) = %s, want error", c.in, got.String())
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseAmount(%q): %v", c.in, err)
			continue
		}
		want, _ := domain.NewMoney(c.want)
		if !got.Equal(want) {
			t.Errorf("ParseAmount(%q) = %s, want %s", c.in, got.String(), want.String())
		}
	}
}

func TestParseDate(t *testing.T) {
	cases := []struct{ in, want string }{
		{"2024-12-31", "2024-12-31"},
		{"31/12/2024", "2024-12-31"},
		{"31-12-2024", "2024-12-31"},
		{"2024/12/31", "2024-12-31"},
	}
	for _, c := range cases {
		got, err := ParseDate(c.in)
		if err != nil {
			t.Errorf("ParseDate(%q): %v", c.in, err)
			continue
		}
		if got.Format("2006-01-02") != c.want {
			t.Errorf("ParseDate(%q) = %s, want %s", c.in, got.Format("2006-01-02"), c.want)
		}
	}
	if _, err := ParseDate("bukan tanggal"); err == nil {
		t.Error("ParseDate(\"bukan tanggal\") harus error")
	}
}

// TestTemplateIsNotSelfImporting adalah penjaga cacat yang nyaris lolos: kalau
// contoh pengisian berada di sheet data, mengunggah template kosong akan
// mengimpor "Budi Santoso" sebagai piutang sungguhan — dan angkanya ikut masuk
// rekonsiliasi terhadap buku besar.
func TestTemplateIsNotSelfImporting(t *testing.T) {
	buf, err := BuildTemplate()
	if err != nil {
		t.Fatalf("BuildTemplate: %v", err)
	}
	res, err := ParseWorkbook(bytes.NewReader(buf))
	if err != nil {
		t.Fatalf("ParseWorkbook(template): %v", err)
	}
	if len(res.Rows) != 0 {
		t.Fatalf("template kosong menghasilkan %d baris, want 0: %+v", len(res.Rows), res.Rows)
	}
	if !res.Total.IsZero() {
		t.Errorf("total template kosong = %s, want 0", res.Total.String())
	}
}

// TestTemplateRoundTrip mengisi template resmi lalu membacanya kembali.
func TestTemplateRoundTrip(t *testing.T) {
	buf, err := BuildTemplate()
	if err != nil {
		t.Fatalf("BuildTemplate: %v", err)
	}
	filled := fillTemplate(t, buf, [][]string{
		{"Budi Santoso", "Perumahan Griya Asri (2019)", "REF-001", "300.000.000", "2024-12-31", "0811", "budi@mail.test", ""},
		{"Andi Wijaya", "Perumahan Griya Asri (2019)", "REF-002", "Rp 250.000.000", "", "", "", "sisa termin akhir"},
		{"Citra Dewi", "Ruko Pasar Baru (2020)", "", "450000000", "31/01/2025", "", "", ""},
	})

	res, err := ParseWorkbook(bytes.NewReader(filled))
	if err != nil {
		t.Fatalf("ParseWorkbook: %v", err)
	}
	if len(res.Rows) != 3 {
		t.Fatalf("baris terbaca = %d, want 3", len(res.Rows))
	}
	if res.ErrorCount != 0 {
		t.Fatalf("ErrorCount = %d, want 0: %+v", res.ErrorCount, res.Rows)
	}
	want, _ := domain.NewMoney("1000000000")
	if !res.Total.Equal(want) {
		t.Errorf("Total = %s, want %s", res.Total.String(), want.String())
	}
	if res.Rows[0].CustomerName != "Budi Santoso" {
		t.Errorf("nama baris 1 = %q", res.Rows[0].CustomerName)
	}
	if res.Rows[0].DueDate == nil || res.Rows[0].DueDate.Format("2006-01-02") != "2024-12-31" {
		t.Errorf("jatuh tempo baris 1 = %v", res.Rows[0].DueDate)
	}
	// Jatuh tempo kosong BUKAN error — banyak buku lama memang tidak
	// mencatatnya, dan menolak barisnya berarti menolak piutang yang nyata.
	if res.Rows[1].DueDate != nil {
		t.Errorf("baris 2 seharusnya tanpa jatuh tempo, got %v", res.Rows[1].DueDate)
	}
}

// TestParseRejectsNegative — saldo negatif adalah lebih bayar, bukan piutang.
// Perlakuannya keputusan akuntan, jadi barisnya ditolak alih-alih diam-diam
// diubah jadi saldo kredit.
func TestParseRejectsNegative(t *testing.T) {
	buf, _ := BuildTemplate()
	filled := fillTemplate(t, buf, [][]string{
		{"Budi Santoso", "Griya Asri", "REF-001", "-5.000.000", "", "", "", ""},
	})
	res, err := ParseWorkbook(bytes.NewReader(filled))
	if err != nil {
		t.Fatalf("ParseWorkbook: %v", err)
	}
	if res.ErrorCount != 1 || res.Rows[0].Status != ParseError {
		t.Fatalf("saldo negatif harus error, got status=%s errors=%d", res.Rows[0].Status, res.ErrorCount)
	}
}

// TestParseDuplicateRef — nomor rujukan ganda di dalam SATU berkas adalah error,
// karena unique key akan menolaknya juga; lebih baik ketahuan di pratinjau.
func TestParseDuplicateRef(t *testing.T) {
	buf, _ := BuildTemplate()
	filled := fillTemplate(t, buf, [][]string{
		{"Budi Santoso", "Griya Asri", "REF-001", "100.000.000", "", "", "", ""},
		{"Andi Wijaya", "Griya Asri", "REF-001", "200.000.000", "", "", "", ""},
	})
	res, err := ParseWorkbook(bytes.NewReader(filled))
	if err != nil {
		t.Fatalf("ParseWorkbook: %v", err)
	}
	if res.ErrorCount != 1 {
		t.Fatalf("ErrorCount = %d, want 1", res.ErrorCount)
	}
	if res.Rows[1].Status != ParseError {
		t.Errorf("baris kedua status = %s, want error", res.Rows[1].Status)
	}
}

// TestParseDuplicateNatural — nama+proyek+nominal identik hanya PERINGATAN:
// sistem tidak tahu apakah itu dua tagihan berbeda milik orang yang sama.
func TestParseDuplicateNatural(t *testing.T) {
	buf, _ := BuildTemplate()
	filled := fillTemplate(t, buf, [][]string{
		{"Budi Santoso", "Griya Asri", "", "100.000.000", "", "", "", ""},
		{"Budi Santoso", "Griya Asri", "", "100.000.000", "", "", "", ""},
	})
	res, err := ParseWorkbook(bytes.NewReader(filled))
	if err != nil {
		t.Fatalf("ParseWorkbook: %v", err)
	}
	if res.ErrorCount != 0 {
		t.Fatalf("ErrorCount = %d, want 0 (duplikat alami bukan error)", res.ErrorCount)
	}
	if res.WarningCount != 1 {
		t.Errorf("WarningCount = %d, want 1", res.WarningCount)
	}
	if res.ValidCount != 2 {
		t.Errorf("ValidCount = %d, want 2", res.ValidCount)
	}
}

// TestParseMissingRequired — nama dan nominal wajib.
func TestParseMissingRequired(t *testing.T) {
	buf, _ := BuildTemplate()
	filled := fillTemplate(t, buf, [][]string{
		{"", "Griya Asri", "", "100.000.000", "", "", "", ""},
		{"Budi Santoso", "Griya Asri", "", "", "", "", "", ""},
	})
	res, err := ParseWorkbook(bytes.NewReader(filled))
	if err != nil {
		t.Fatalf("ParseWorkbook: %v", err)
	}
	if res.ErrorCount != 2 {
		t.Fatalf("ErrorCount = %d, want 2: %+v", res.ErrorCount, res.Rows)
	}
}

// fillTemplate menulis baris data ke bawah header template resmi.
func fillTemplate(t *testing.T, template []byte, rows [][]string) []byte {
	t.Helper()
	f, err := excelize.OpenReader(bytes.NewReader(template))
	if err != nil {
		t.Fatalf("buka template: %v", err)
	}
	defer f.Close()

	existing, err := f.GetRows(TemplateSheet)
	if err != nil {
		t.Fatalf("baca sheet: %v", err)
	}
	start := len(existing) + 1 // 1-indexed, tepat di bawah baris terakhir
	for i, row := range rows {
		for c, val := range row {
			cell, err := excelize.CoordinatesToCellName(c+1, start+i)
			if err != nil {
				t.Fatalf("koordinat: %v", err)
			}
			if err := f.SetCellStr(TemplateSheet, cell, val); err != nil {
				t.Fatalf("tulis sel: %v", err)
			}
		}
	}
	var out bytes.Buffer
	if err := f.Write(&out); err != nil {
		t.Fatalf("tulis workbook: %v", err)
	}
	return out.Bytes()
}

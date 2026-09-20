package reporting

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/xuri/excelize/v2"
)

func TestWriteTable_XLSX_RoundTrip(t *testing.T) {
	rows := [][]string{
		{"aset", "1-1000", "Kas", "1500000.0000"},
		{"aset_total", "", "Total Aset", "1500000.0000"},
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/x?format=xlsx", nil)
	writeTable(rec, req, "neraca", []string{"seksi", "kode", "nama", "jumlah"}, rows)

	if ct := rec.Header().Get("Content-Type"); ct != xlsxContentType {
		t.Fatalf("content-type = %q", ct)
	}
	body := rec.Body.Bytes()
	if !bytes.HasPrefix(body, []byte("PK")) {
		t.Fatalf("bukan zip/xlsx, prefix = %q", body[:4])
	}
	f, err := excelize.OpenReader(bytes.NewReader(body))
	if err != nil {
		t.Fatalf("workbook tidak bisa dibuka: %v", err)
	}
	defer f.Close()
	if got, _ := f.GetCellValue("neraca", "B2"); got != "1-1000" {
		t.Errorf("kode akun harus tetap teks, got %q", got)
	}
	if typ, _ := f.GetCellType("neraca", "D2"); typ == excelize.CellTypeSharedString || typ == excelize.CellTypeInlineString {
		t.Errorf("kolom jumlah harus sel angka (bukan string), got %v", typ)
	}
	if raw, _ := f.GetCellValue("neraca", "D2", excelize.Options{RawCellValue: true}); raw != "1500000" {
		t.Errorf("nilai jumlah = %q", raw)
	}
	if got, _ := f.GetCellValue("neraca", "C3"); got != "Total Aset" {
		t.Errorf("baris total hilang: %q", got)
	}
}

func TestWriteTable_CSVTetapDefault(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/x?format=csv", nil)
	writeTable(rec, req, "neraca", []string{"a", "jumlah"}, [][]string{{"x", "1"}})
	if ct := rec.Header().Get("Content-Type"); ct != "text/csv; charset=utf-8" {
		t.Fatalf("csv rusak: %q", ct)
	}
}

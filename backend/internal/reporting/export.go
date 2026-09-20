package reporting

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/xuri/excelize/v2"
)

// numericColumns adalah header CSV yang berisi nominal. Di berkas Excel kolom
// ini ditulis sebagai SEL ANGKA (bukan teks) supaya bisa dijumlah/difilter,
// dengan format ribuan. Nilainya string desimal dari service yang SAMA dengan
// laporan/CSV — tidak ada perhitungan ulang di sini.
var numericColumns = map[string]bool{
	"jumlah": true, "total_debit": true, "total_kredit": true, "saldo": true,
	"debit": true, "kredit": true, "nominal": true, "dibayar": true,
	"outstanding": true, "total_list_price": true, "nilai_kontrak": true,
	"transfer_value": true, "tax_amount": true,
}

const xlsxContentType = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"

// writeTable menulis baris laporan sebagai CSV (default) atau XLSX
// (?format=xlsx). Header + baris identik dengan CSV existing, sehingga Excel,
// CSV, dan layar selalu menampilkan data yang sama.
func writeTable(w http.ResponseWriter, r *http.Request, sheet string, header []string, rows [][]string) {
	if r.URL.Query().Get("format") != "xlsx" {
		writeCSV(w, header, rows)
		return
	}
	b, err := buildXLSX(sheet, header, rows)
	if err != nil {
		writeReportError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", xlsxContentType)
	w.Header().Set("Content-Disposition", `attachment; filename="`+sheet+`.xlsx"`)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(b)
}

func buildXLSX(sheet string, header []string, rows [][]string) ([]byte, error) {
	f := excelize.NewFile()
	defer f.Close()
	// Nama sheet Excel maks 31 karakter, tanpa karakter terlarang.
	name := strings.NewReplacer("/", "-", "\\", "-", "?", "", "*", "", "[", "", "]", "", ":", "").Replace(sheet)
	if len(name) > 31 {
		name = name[:31]
	}
	if err := f.SetSheetName("Sheet1", name); err != nil {
		return nil, err
	}

	headStyle, err := f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Bold: true, Color: "FFFFFF"},
		Fill:      excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{"1F3A5F"}},
		Alignment: &excelize.Alignment{Horizontal: "center", Vertical: "center"},
	})
	if err != nil {
		return nil, err
	}
	numStyle, err := f.NewStyle(&excelize.Style{NumFmt: 4}) // #,##0.00
	if err != nil {
		return nil, err
	}

	numeric := make([]bool, len(header))
	for i, h := range header {
		numeric[i] = numericColumns[h]
		cell, _ := excelize.CoordinatesToCellName(i+1, 1)
		_ = f.SetCellValue(name, cell, h)
		_ = f.SetCellStyle(name, cell, cell, headStyle)
		_ = f.SetColWidth(name, colName(i), colName(i), colWidth(h))
	}
	for ri, row := range rows {
		for ci, v := range row {
			cell, _ := excelize.CoordinatesToCellName(ci+1, ri+2)
			if ci < len(numeric) && numeric[ci] && v != "" {
				if n, perr := strconv.ParseFloat(v, 64); perr == nil {
					_ = f.SetCellFloat(name, cell, n, -1, 64)
					_ = f.SetCellStyle(name, cell, cell, numStyle)
					continue
				}
			}
			_ = f.SetCellStr(name, cell, v) // teks: kode akun "1-1000", tanggal, ref — tak boleh ditafsir Excel
		}
	}
	_ = f.SetPanes(name, &excelize.Panes{Freeze: true, YSplit: 1, TopLeftCell: "A2", ActivePane: "bottomLeft"})

	buf, err := f.WriteToBuffer()
	if err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func colName(i int) string {
	n, _ := excelize.ColumnNumberToName(i + 1)
	return n
}

func colWidth(h string) float64 {
	switch h {
	case "keterangan", "nama", "deskripsi":
		return 48
	case "seksi", "referensi", "kategori":
		return 24
	}
	return 18
}

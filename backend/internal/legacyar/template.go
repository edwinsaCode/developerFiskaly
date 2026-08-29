package legacyar

import (
	"bytes"
	"fmt"

	"github.com/xuri/excelize/v2"
)

// BuildTemplate menghasilkan berkas Excel template resmi.
//
// Template dibangkitkan dari `templateColumns` — daftar yang sama yang dipakai
// pembacaan berkas. Sebuah template statis yang disimpan sebagai aset akan
// perlahan menyimpang dari parser-nya, dan penyimpangan itu baru ketahuan saat
// klien sudah mengisi ratusan baris.
func BuildTemplate() ([]byte, error) {
	f := excelize.NewFile()
	defer f.Close()

	// excelize selalu membuat "Sheet1"; template dibuat dengan nama yang dicari
	// parser lalu sheet bawaan dibuang.
	idx, err := f.NewSheet(TemplateSheet)
	if err != nil {
		return nil, err
	}
	f.SetActiveSheet(idx)
	if def := "Sheet1"; hasSheet(f, def) {
		if err := f.DeleteSheet(def); err != nil {
			return nil, err
		}
	}

	titleStyle, err := f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Bold: true, Size: 13},
		Alignment: &excelize.Alignment{Vertical: "center"},
	})
	if err != nil {
		return nil, err
	}
	noteStyle, err := f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Size: 10, Italic: true, Color: "666666"},
		Alignment: &excelize.Alignment{Vertical: "center", WrapText: true},
	})
	if err != nil {
		return nil, err
	}
	headStyle, err := f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Bold: true, Color: "FFFFFF"},
		Fill:      excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{"1F3A5F"}},
		Alignment: &excelize.Alignment{Horizontal: "center", Vertical: "center", WrapText: true},
	})
	if err != nil {
		return nil, err
	}
	hintStyle, err := f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Size: 9, Color: "888888"},
		Alignment: &excelize.Alignment{Vertical: "top", WrapText: true},
	})
	if err != nil {
		return nil, err
	}
	textStyle, err := f.NewStyle(&excelize.Style{
		// Seluruh kolom data diformat TEKS. Tanpa ini Excel akan menafsirkan
		// "0812…" sebagai angka dan membuang nol di depannya, dan nomor rujukan
		// seperti "2019-014" akan berubah jadi tanggal.
		NumFmt: 49,
	})
	if err != nil {
		return nil, err
	}

	_ = f.SetRowHeight(TemplateSheet, 1, 26)
	_ = f.SetCellValue(TemplateSheet, "A1", "Template Piutang Proyek Lama")
	_ = f.SetCellStyle(TemplateSheet, "A1", "A1", titleStyle)

	_ = f.SetRowHeight(TemplateSheet, 2, 44)
	_ = f.SetCellValue(TemplateSheet, "A2",
		"Isi satu baris untuk setiap sisa tagihan. Jangan mengubah nama kolom, "+
			"jangan menambah kolom, dan jangan menghapus baris header. "+
			"Tanggal cutoff (posisi saldo) dipilih saat mengunggah, bukan di berkas ini.")
	lastCol, _ := excelize.ColumnNumberToName(len(templateColumns))
	_ = f.MergeCell(TemplateSheet, "A2", lastCol+"2")
	_ = f.SetCellStyle(TemplateSheet, "A2", lastCol+"2", noteStyle)

	// Baris petunjuk ditaruh DI ATAS header, bukan di bawahnya. Parser membaca
	// setiap baris setelah header sebagai data — petunjuk di bawah header akan
	// masuk sebagai piutang bernama "Nama pembeli sebagaimana tercatat…" dan
	// muncul sebagai baris error di setiap pratinjau.
	const hintRow = 4
	const headerRow = 5
	_ = f.SetRowHeight(TemplateSheet, hintRow, 34)
	_ = f.SetRowHeight(TemplateSheet, headerRow, 30)

	for i, c := range templateColumns {
		col, err := excelize.ColumnNumberToName(i + 1)
		if err != nil {
			return nil, err
		}
		title := c.Title
		if c.Required {
			title += " *"
		}
		hcell := fmt.Sprintf("%s%d", col, headerRow)
		_ = f.SetCellValue(TemplateSheet, hcell, title)
		_ = f.SetCellStyle(TemplateSheet, hcell, hcell, headStyle)

		ncell := fmt.Sprintf("%s%d", col, hintRow)
		_ = f.SetCellValue(TemplateSheet, ncell, c.Hint)
		_ = f.SetCellStyle(TemplateSheet, ncell, ncell, hintStyle)

		_ = f.SetColWidth(TemplateSheet, col, col, c.Width)
		// Baris data diformat teks.
		_ = f.SetCellStyle(TemplateSheet,
			fmt.Sprintf("%s%d", col, headerRow+1),
			fmt.Sprintf("%s%d", col, headerRow+500), textStyle)
	}

	// Contoh pengisian hidup di SHEET TERPISAH. Kalau ditaruh di baris data,
	// berkas yang diunggah tanpa menghapusnya akan mengimpor "Budi Santoso"
	// sebagai piutang sungguhan — dan angka contoh itu masuk rekonsiliasi.
	if err := writeSampleSheet(f, headStyle, noteStyle); err != nil {
		return nil, err
	}

	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// sampleSheet adalah nama sheet contoh. Parser hanya membaca TemplateSheet,
// jadi isi sheet ini tidak pernah ikut terimpor.
const sampleSheet = "Contoh Pengisian"

func writeSampleSheet(f *excelize.File, headStyle, noteStyle int) error {
	if _, err := f.NewSheet(sampleSheet); err != nil {
		return err
	}
	_ = f.SetCellValue(sampleSheet, "A1",
		"Contoh saja — sheet ini TIDAK ikut diimpor. Isi data sebenarnya di sheet \""+TemplateSheet+"\".")
	lastCol, _ := excelize.ColumnNumberToName(len(templateColumns))
	_ = f.MergeCell(sampleSheet, "A1", lastCol+"1")
	_ = f.SetCellStyle(sampleSheet, "A1", lastCol+"1", noteStyle)
	_ = f.SetRowHeight(sampleSheet, 1, 24)

	for i, c := range templateColumns {
		col, err := excelize.ColumnNumberToName(i + 1)
		if err != nil {
			return err
		}
		_ = f.SetCellValue(sampleSheet, col+"3", c.Title)
		_ = f.SetCellStyle(sampleSheet, col+"3", col+"3", headStyle)
		_ = f.SetColWidth(sampleSheet, col, col, c.Width)
	}

	samples := [][]string{
		{"Budi Santoso", "Perum Melati 2019", "MLT-2019-014", "300000000", "2024-12-31", "081234567890", "budi@contoh.id", "sisa termin terakhir"},
		{"Andi Wijaya", "Perum Melati 2019", "MLT-2019-021", "250.000.000", "", "", "", "tanpa jatuh tempo — dipakai tanggal cutoff"},
		{"Citra Dewi", "Ruko Anggrek 2020", "", "450000000", "31/03/2025", "0812-3456-7899", "", ""},
	}
	for r, row := range samples {
		for i, v := range row {
			col, _ := excelize.ColumnNumberToName(i + 1)
			_ = f.SetCellValue(sampleSheet, fmt.Sprintf("%s%d", col, r+4), v)
		}
	}
	return nil
}

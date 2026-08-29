package legacyar

import (
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"

	"github.com/xuri/excelize/v2"

	"esaproperti/internal/domain"
)

// ── Template ──────────────────────────────────────────────────────────────────

// Kolom template resmi. Urutannya tetap; header dicocokkan longgar (lihat
// matchHeader) supaya berkas yang kolomnya digeser atau ditulis huruf besar
// tetap terbaca.
const (
	colCustomerName = "Nama Customer"
	colSourceLabel  = "Proyek / Sumber Lama"
	colExternalRef  = "No. Rujukan"
	colOutstanding  = "Sisa Tagihan"
	colDueDate      = "Jatuh Tempo"
	colPhone        = "No. HP"
	colEmail        = "Email"
	colNotes        = "Catatan"
)

// TemplateSheet adalah nama sheet yang dibaca. Sheet lain diabaikan.
const TemplateSheet = "Piutang Lama"

// templateColumns adalah definisi kolom template — dipakai untuk MENULIS
// template dan untuk MEMBACA berkas, sehingga keduanya tidak bisa menyimpang.
var templateColumns = []struct {
	Title    string
	Required bool
	Width    float64
	Hint     string
}{
	{colCustomerName, true, 28, "Nama pembeli sebagaimana tercatat di pembukuan lama"},
	{colSourceLabel, true, 26, "Nama proyek atau sumber saldo lama, mis. Perum Melati 2019"},
	{colExternalRef, false, 20, "Nomor kontrak/kwitansi lama bila ada — dipakai mencegah impor ganda"},
	{colOutstanding, true, 20, "Sisa yang masih ditagih per tanggal cutoff, angka saja"},
	{colDueDate, false, 16, "Kosongkan bila tidak ada; sistem memakai tanggal cutoff"},
	{colPhone, false, 18, "Opsional"},
	{colEmail, false, 24, "Opsional"},
	{colNotes, false, 30, "Opsional"},
}

// ── Pembacaan ─────────────────────────────────────────────────────────────────

// ParsedRow adalah satu baris berkas setelah dibaca dan divalidasi.
type ParsedRow struct {
	LineNo int
	Raw    map[string]string

	CustomerName string
	SourceLabel  string
	ExternalRef  string
	Amount       domain.Money
	DueDate      *time.Time
	Phone        string
	Email        string
	Notes        string

	Status ParseStatus
	Issues JSONIssues
}

// ParseResult adalah hasil membaca seluruh berkas.
type ParseResult struct {
	Rows         []ParsedRow
	ValidCount   int
	ErrorCount   int
	WarningCount int
	Total        domain.Money
}

// ParseWorkbook membaca berkas Excel menjadi baris-baris tervalidasi.
//
// Nominal SELALU dibaca sebagai string lewat GetCellValue lalu dikonversi ke
// domain.Money. GetCellFloat tidak dipakai: float64 hanya punya ~15 digit
// signifikan dan sudah membulatkan sebelum kita sempat melihat angkanya —
// pelanggaran Invariant #2 yang tidak terlihat sampai selisihnya masuk laporan.
func ParseWorkbook(r io.Reader) (*ParseResult, error) {
	f, err := excelize.OpenReader(r)
	if err != nil {
		return nil, fmt.Errorf("berkas tidak bisa dibaca sebagai Excel: %w", err)
	}
	defer f.Close()

	sheet := TemplateSheet
	if !hasSheet(f, sheet) {
		// Berkas yang di-save-as dari template kadang kehilangan nama sheet.
		// Sheet pertama lebih baik daripada menolak berkas yang isinya benar.
		names := f.GetSheetList()
		if len(names) == 0 {
			return nil, fmt.Errorf("berkas tidak memiliki sheet")
		}
		sheet = names[0]
	}

	rows, err := f.GetRows(sheet)
	if err != nil {
		return nil, fmt.Errorf("sheet %q tidak bisa dibaca: %w", sheet, err)
	}

	headerIdx, colIdx := findHeader(rows)
	if headerIdx < 0 {
		return nil, fmt.Errorf("baris header tidak ditemukan — pastikan memakai template resmi (kolom %q dan %q wajib ada)",
			colCustomerName, colOutstanding)
	}

	res := &ParseResult{Total: domain.FromInt(0)}
	seenRef := map[string]int{}
	seenNatural := map[string]int{}

	for i := headerIdx + 1; i < len(rows); i++ {
		raw := map[string]string{}
		for name, idx := range colIdx {
			raw[name] = cellAt(rows[i], idx)
		}
		if isBlankRow(raw) {
			continue
		}

		pr := parseRow(len(res.Rows)+1, raw)

		// Duplikat DI DALAM berkas: nomor rujukan yang sama dua kali adalah
		// error (unique key akan menolaknya juga, tapi lebih baik ketahuan di
		// pratinjau daripada saat commit gagal separuh jalan).
		if pr.ExternalRef != "" {
			key := strings.ToLower(pr.ExternalRef)
			if prev, dup := seenRef[key]; dup {
				pr.Issues.add(colExternalRef, fmt.Sprintf("nomor rujukan sama dengan baris %d", prev))
				pr.Status = ParseError
			} else {
				seenRef[key] = pr.LineNo
			}
		}
		// Duplikat alami (nama + proyek + nominal) hanya PERINGATAN: sistem
		// tidak tahu apakah dua baris identik adalah dua tagihan berbeda milik
		// orang yang sama. Admin tahu.
		if pr.Status != ParseError {
			key := strings.ToLower(pr.CustomerName + "|" + pr.SourceLabel + "|" + pr.Amount.String())
			if prev, dup := seenNatural[key]; dup {
				pr.Issues.add(colCustomerName, fmt.Sprintf("nama, proyek, dan nominal sama persis dengan baris %d — pastikan ini memang dua tagihan berbeda", prev))
				if pr.Status == ParseOK {
					pr.Status = ParseWarning
				}
			} else {
				seenNatural[key] = pr.LineNo
			}
		}

		switch pr.Status {
		case ParseError:
			res.ErrorCount++
		case ParseWarning:
			res.WarningCount++
			res.ValidCount++
			res.Total = res.Total.Add(pr.Amount)
		default:
			res.ValidCount++
			res.Total = res.Total.Add(pr.Amount)
		}
		res.Rows = append(res.Rows, pr)
	}

	return res, nil
}

// parseRow memvalidasi satu baris. Ia TIDAK pernah gagal secara keseluruhan:
// masalah dikumpulkan sebagai issue supaya pratinjau bisa menampilkan SEMUA
// baris bermasalah sekaligus, bukan satu per satu tiap kali diunggah ulang.
func parseRow(lineNo int, raw map[string]string) ParsedRow {
	pr := ParsedRow{
		LineNo:       lineNo,
		Raw:          raw,
		CustomerName: strings.TrimSpace(raw[colCustomerName]),
		SourceLabel:  strings.TrimSpace(raw[colSourceLabel]),
		ExternalRef:  strings.TrimSpace(raw[colExternalRef]),
		Phone:        strings.TrimSpace(raw[colPhone]),
		Email:        strings.TrimSpace(raw[colEmail]),
		Notes:        strings.TrimSpace(raw[colNotes]),
		Amount:       domain.FromInt(0),
		Status:       ParseOK,
	}

	if pr.CustomerName == "" {
		pr.Issues.add(colCustomerName, "wajib diisi")
	}
	if pr.SourceLabel == "" {
		pr.Issues.add(colSourceLabel, "wajib diisi — tanpa ini piutang tidak bisa dilacak ke proyek asalnya")
	}
	if len(pr.CustomerName) > 200 {
		pr.Issues.add(colCustomerName, "maksimal 200 karakter")
	}
	if len(pr.SourceLabel) > 200 {
		pr.Issues.add(colSourceLabel, "maksimal 200 karakter")
	}
	if len(pr.ExternalRef) > 100 {
		pr.Issues.add(colExternalRef, "maksimal 100 karakter")
	}
	if pr.Email != "" && !strings.Contains(pr.Email, "@") {
		pr.Issues.add(colEmail, "bukan alamat email yang sah")
	}

	amtRaw := strings.TrimSpace(raw[colOutstanding])
	switch {
	case amtRaw == "":
		pr.Issues.add(colOutstanding, "wajib diisi")
	default:
		amt, err := ParseAmount(amtRaw)
		switch {
		case err != nil:
			pr.Issues.add(colOutstanding, err.Error())
		case amt.IsNeg():
			// Saldo negatif berarti pembeli KELEBIHAN bayar — itu kewajiban,
			// bukan piutang, dan perlakuannya keputusan akuntan. Menerimanya
			// diam-diam sebagai saldo kredit akan menyelipkan kewajiban ke
			// dalam akun aset.
			pr.Issues.add(colOutstanding, "bernilai negatif — saldo lebih bayar bukan piutang; keluarkan dari berkas ini")
		case amt.IsZero():
			pr.Issues.add(colOutstanding, "bernilai nol — tidak ada yang perlu ditagih")
		default:
			pr.Amount = amt
		}
	}

	if d := strings.TrimSpace(raw[colDueDate]); d != "" {
		due, err := ParseDate(d)
		if err != nil {
			pr.Issues.add(colDueDate, err.Error())
		} else {
			pr.DueDate = &due
		}
	}

	if len(pr.Issues) > 0 {
		pr.Status = ParseError
	}
	return pr
}

// ── Konversi nilai ────────────────────────────────────────────────────────────

var amountCleaner = regexp.MustCompile(`[^0-9,.\-]`)

// ParseAmount mengubah teks nominal Excel menjadi domain.Money.
//
// Berkas dari klien memakai campuran gaya: "1.000.000", "1000000", "1.000.000,50",
// "Rp 1.000.000", bahkan "1,000,000.50" bila pernah lewat Excel berlokal Inggris.
// Fungsi ini menebak pemisah desimal dari POSISI pemisah terakhir, bukan dari
// asumsi lokal — sebuah berkas bisa saja tidak dibuat di mesin yang sama.
func ParseAmount(s string) (domain.Money, error) {
	clean := amountCleaner.ReplaceAllString(s, "")
	clean = strings.TrimSpace(clean)
	if clean == "" || clean == "-" {
		return domain.Money{}, fmt.Errorf("%q bukan angka", s)
	}

	neg := strings.HasPrefix(clean, "-")
	clean = strings.TrimPrefix(clean, "-")
	// Tanda minus di tengah angka bukan bentuk yang dikenal.
	if strings.Contains(clean, "-") {
		return domain.Money{}, fmt.Errorf("%q bukan angka", s)
	}

	dots := strings.Count(clean, ".")
	commas := strings.Count(clean, ",")
	lastDot := strings.LastIndex(clean, ".")
	lastComma := strings.LastIndex(clean, ",")

	// decSep adalah pemisah desimal, atau 0 bila angkanya bulat. Aturannya
	// simetris untuk titik dan koma — berkas klien bisa datang dari Excel
	// berlokal Indonesia maupun Inggris, dan menebak dari lokal server adalah
	// cara paling pasti untuk salah pada berkas yang dikirim lewat WhatsApp.
	var decSep byte
	var decIdx int
	switch {
	case dots > 0 && commas > 0:
		// Keduanya ada: yang MUNCUL TERAKHIR adalah desimal, sisanya ribuan.
		// "1.000.000,50" → koma; "1,000,000.50" → titik.
		if lastComma > lastDot {
			decSep, decIdx = ',', lastComma
		} else {
			decSep, decIdx = '.', lastDot
		}
	case dots > 1 || commas > 1:
		// Muncul lebih dari sekali ⇒ pasti pemisah ribuan. Tidak ada angka yang
		// punya dua titik desimal.
	case dots == 1 || commas == 1:
		idx, tailLen := lastDot, len(clean)-lastDot-1
		sep := byte('.')
		if commas == 1 {
			idx, tailLen, sep = lastComma, len(clean)-lastComma-1, ','
		}
		// Tepat tiga digit di belakang pemisah tunggal itu ambigu: "1.000"
		// bisa berarti seribu atau satu koma nol nol nol. Di berkas piutang
		// rupiah, seribu jauh lebih mungkin — dan salah membacanya sebagai 1,0
		// mengecilkan piutang seribu kali lipat, bukan membesarkannya.
		if tailLen != 3 {
			decSep, decIdx = sep, idx
		}
	}

	var intPart, fracPart string
	if decSep == 0 {
		intPart = clean
	} else {
		intPart, fracPart = clean[:decIdx], clean[decIdx+1:]
	}
	intPart = strings.ReplaceAll(intPart, ".", "")
	intPart = strings.ReplaceAll(intPart, ",", "")

	if intPart == "" {
		intPart = "0"
	}
	if strings.ContainsAny(intPart, ".,") || strings.ContainsAny(fracPart, ".,") {
		return domain.Money{}, fmt.Errorf("%q bukan angka", s)
	}

	out := intPart
	if fracPart != "" {
		out += "." + fracPart
	}
	if neg {
		out = "-" + out
	}
	m, err := domain.NewMoney(out)
	if err != nil {
		return domain.Money{}, fmt.Errorf("%q bukan angka", s)
	}
	return m, nil
}

var dateLayouts = []string{
	"2006-01-02", "02/01/2006", "02-01-2006", "2/1/2006", "1/2/2006",
	"02 January 2006", "2006/01/02", "01/02/06", "02-Jan-2006",
}

// ParseDate mengubah teks tanggal Excel menjadi time.Time.
//
// Sel bertipe tanggal bisa sampai ke kita sebagai teks terformat ATAU sebagai
// nomor seri Excel, tergantung apakah sel itu punya number format. Keduanya
// ditangani; menolak salah satunya berarti menolak berkas yang isinya benar.
func ParseDate(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	for _, l := range dateLayouts {
		if t, err := time.ParseInLocation(l, s, time.Local); err == nil {
			return t, nil
		}
	}
	// Nomor seri Excel (mis. "45292").
	if n := excelSerial(s); n > 0 {
		if serial, err := excelize.ExcelDateToTime(n, false); err == nil {
			return time.Date(serial.Year(), serial.Month(), serial.Day(), 0, 0, 0, 0, time.Local), nil
		}
	}
	return time.Time{}, fmt.Errorf("%q bukan tanggal yang dikenali (pakai YYYY-MM-DD atau DD/MM/YYYY)", s)
}

// excelSerial membaca nomor seri tanggal Excel. Nomor seri adalah HITUNGAN
// HARI, bukan nominal uang — float aman di sini dan hanya di sini. Teks yang
// bukan angka mengembalikan 0 sehingga ditolak pemanggil.
func excelSerial(s string) float64 {
	var f float64
	n, err := fmt.Sscanf(s, "%g", &f)
	if err != nil || n != 1 {
		return 0
	}
	return f
}

// ── Header ────────────────────────────────────────────────────────────────────

var headerNoise = regexp.MustCompile(`[^a-z0-9]+`)

func normalizeHeader(s string) string {
	return headerNoise.ReplaceAllString(strings.ToLower(strings.TrimSpace(s)), "")
}

// findHeader mencari baris header dan memetakan judul kolom ke indeksnya.
// Header dicari di 10 baris pertama supaya berkas yang diberi judul atau logo
// di atas tabel tetap terbaca.
func findHeader(rows [][]string) (int, map[string]int) {
	want := map[string]string{}
	for _, c := range templateColumns {
		want[normalizeHeader(c.Title)] = c.Title
	}
	// Ejaan alternatif yang wajar dari berkas klien.
	alias := map[string]string{
		"nama":              colCustomerName,
		"namapembeli":       colCustomerName,
		"customer":          colCustomerName,
		"proyek":            colSourceLabel,
		"proyeklama":        colSourceLabel,
		"sumber":            colSourceLabel,
		"norujukan":         colExternalRef,
		"nomorrujukan":      colExternalRef,
		"referensi":         colExternalRef,
		"sisatagihan":       colOutstanding,
		"outstanding":       colOutstanding,
		"sisapiutang":       colOutstanding,
		"saldo":             colOutstanding,
		"jatuhtempo":        colDueDate,
		"tanggaljatuhtempo": colDueDate,
		"nohp":              colPhone,
		"telepon":           colPhone,
		"hp":                colPhone,
		"email":             colEmail,
		"catatan":           colNotes,
		"keterangan":        colNotes,
	}

	limit := len(rows)
	if limit > 10 {
		limit = 10
	}
	for i := 0; i < limit; i++ {
		idx := map[string]int{}
		for j, cell := range rows[i] {
			n := normalizeHeader(cell)
			if n == "" {
				continue
			}
			if title, ok := want[n]; ok {
				if _, dup := idx[title]; !dup {
					idx[title] = j
				}
				continue
			}
			if title, ok := alias[n]; ok {
				if _, dup := idx[title]; !dup {
					idx[title] = j
				}
			}
		}
		// Header dianggap sah bila dua kolom wajib yang paling menentukan ada.
		if _, ok1 := idx[colCustomerName]; ok1 {
			if _, ok2 := idx[colOutstanding]; ok2 {
				return i, idx
			}
		}
	}
	return -1, nil
}

func cellAt(row []string, idx int) string {
	if idx < 0 || idx >= len(row) {
		return ""
	}
	return strings.TrimSpace(row[idx])
}

func isBlankRow(raw map[string]string) bool {
	for _, v := range raw {
		if strings.TrimSpace(v) != "" {
			return false
		}
	}
	return true
}

func hasSheet(f *excelize.File, name string) bool {
	for _, n := range f.GetSheetList() {
		if n == name {
			return true
		}
	}
	return false
}

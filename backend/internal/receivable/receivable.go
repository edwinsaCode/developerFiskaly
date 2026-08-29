// Package receivable adalah pemilik tunggal jawaban atas pertanyaan
// "customer ini masih berutang berapa, dan sudah berapa lama".
//
// Sebelum W-4 mesin aging tinggal di paket `reporting`. Akibatnya setiap
// penghasil baris piutang harus meng-import paket laporan — termasuk `charge`
// yang transaksional. Arah ketergantungan itu terbalik: paket yang menulis
// tidak boleh bergantung pada paket yang membaca.
//
// Paket ini karena itu murni value object: hanya import `domain`, tidak ada DB,
// tidak ada HTTP. Siapa pun boleh menghasilkan `Row` dan memanggil `BuildAging`
// tanpa menyeret ketergantungan apa pun.
//
// INV-AR-1: hanya ada SATU mesin bucketing di seluruh repo — yang di file ini.
package receivable

import (
	"time"

	"github.com/shopspring/decimal"

	"esaproperti/internal/domain"
)

// ── Sumber eksposur ───────────────────────────────────────────────────────────

// Source membedakan asal kewajiban customer. Ini DIMENSI pada satu daftar,
// bukan alasan untuk membuat laporan kedua: seorang customer berutang satu
// jumlah, walau tagihannya lahir dari dua proses berbeda.
type Source string

const (
	// SourceHouse — cicilan harga rumah (payment_schedules).
	SourceHouse Source = "house"
	// SourceRealization — tagihan biaya realisasi (charge_items ber-due-date).
	SourceRealization Source = "realization"
	// SourceLegacy — piutang proyek lama yang diimpor (legacy_receivables).
	// Baris ini tidak punya unit maupun kontrak di sistem: proyeknya memang
	// selesai sebelum buku ini dibuka. Ia tetap masuk daftar yang sama karena
	// customer tidak peduli piutangnya lahir dari proses yang mana.
	SourceLegacy Source = "legacy"
	// SourceAddon (W-13) — produk tambahan yang dijual bersama unit (kelebihan
	// tanah, dll). Ia berdiri sendiri karena bukan biaya realisasi: uangnya
	// menjadi PENDAPATAN perusahaan, bukan titipan untuk pihak ketiga. Menumpang
	// label "Realisasi" akan membuat penagih menyangka ada vendor yang menunggu
	// dibayar, padahal tidak ada.
	SourceAddon Source = "addon"
)

// Valid melaporkan apakah s adalah sumber yang dikenal.
func (s Source) Valid() bool {
	return s == SourceHouse || s == SourceRealization || s == SourceLegacy || s == SourceAddon
}

// ── Baris mentah ──────────────────────────────────────────────────────────────

// Row adalah satu kewajiban customer yang punya tanggal jatuh tempo.
// Received=true berarti sudah lunas penuh (kompatibilitas data lama yang tidak
// mengisi PaidAmount).
type Row struct {
	// Source diisi penghasil baris. Kosong diperlakukan sebagai SourceHouse
	// agar pemanggil lama (sebelum W-4) tetap benar.
	Source Source

	ContractID uint64 `gorm:"column:contract_id"`
	UnitID     uint64 `gorm:"column:unit_id"`
	BuyerName  string `gorm:"column:buyer_name"`
	// UnitCode adalah kode unit apa adanya (mis. "A-12"). JANGAN dititipi
	// keterangan tagihan — itu tugas Label. Sebelum W-4 `charge` menjejalkan
	// "unit — grup / item" ke sini, sehingga kolom yang sama berarti dua hal
	// berbeda tergantung siapa yang mengisinya.
	UnitCode string `gorm:"column:unit_code"`
	// Label adalah keterangan tagihannya ("Cicilan #3", "PDAM"). Boleh kosong.
	Label         string `gorm:"column:label"`
	InvoiceNumber string `gorm:"column:invoice_number"`
	BuyerPhone    string `gorm:"column:buyer_phone"`
	BuyerEmail    string `gorm:"column:buyer_email"`

	// RefID adalah id baris asal (payment_schedules.id atau charge_items.id).
	// Dipakai UI untuk menautkan baris aging ke sumbernya.
	RefID uint64 `gorm:"column:ref_id"`

	DueDate    time.Time    `gorm:"column:due_date"`
	Amount     domain.Money `gorm:"column:amount"`
	PaidAmount domain.Money `gorm:"column:paid_amount"`
	Received   bool         `gorm:"column:received"`
}

// source mengembalikan sumber efektif baris (default: harga rumah).
func (r Row) source() Source {
	if r.Source == "" {
		return SourceHouse
	}
	return r.Source
}

// ── Bucket & status ───────────────────────────────────────────────────────────

// Bucket adalah kunci kategori umur piutang.
type Bucket string

const (
	BucketCurrent Bucket = "current" // belum jatuh tempo
	Bucket1_30    Bucket = "1_30"    // telat 1–30 hari
	Bucket31_60   Bucket = "31_60"   // telat 31–60 hari
	Bucket61_90   Bucket = "61_90"   // telat 61–90 hari
	Bucket90Plus  Bucket = "90_plus" // telat > 90 hari
)

// Status adalah status pembayaran efektif yang DIHITUNG DI BACKEND.
type Status string

const (
	StatusScheduled Status = "scheduled"
	StatusDueToday  Status = "due_today"
	StatusOverdue   Status = "overdue"
	StatusPaid      Status = "paid"
)

// dayInHours adalah durasi satu hari penuh.
const dayInHours = 24 * time.Hour

// BucketFor mengklasifikasikan umur piutang. daysOverdue <= 0 = belum jatuh tempo.
//
// Diekspor untuk W-11: aging HUTANG memakai ambang yang SAMA persis dengan aging
// piutang. Kalau AP menyalin tangga ambangnya sendiri, dua laporan umur di satu
// aplikasi akan menyimpang begitu salah satu ambang diubah. Yang dibagi hanya
// klasifikasi umur — struktur baris & sumber datanya tetap milik masing-masing.
func BucketFor(daysOverdue int) Bucket {
	switch {
	case daysOverdue <= 0:
		return BucketCurrent
	case daysOverdue <= 30:
		return Bucket1_30
	case daysOverdue <= 60:
		return Bucket31_60
	case daysOverdue <= 90:
		return Bucket61_90
	default:
		return Bucket90Plus
	}
}

// DaysBetween menghitung jumlah hari kalender penuh dari dueDate sampai asOf.
// Positif = sudah lewat jatuh tempo. Keduanya dinormalkan ke tengah malam UTC
// agar deterministik dan bebas efek jam.
func DaysBetween(dueDate, asOf time.Time) int {
	d := time.Date(dueDate.Year(), dueDate.Month(), dueDate.Day(), 0, 0, 0, 0, time.UTC)
	a := time.Date(asOf.Year(), asOf.Month(), asOf.Day(), 0, 0, 0, 0, time.UTC)
	return int(a.Sub(d) / dayInHours)
}

// EffectiveStatus memetakan selisih hari ke status untuk baris yang BELUM lunas.
func EffectiveStatus(daysOverdue int) Status {
	switch {
	case daysOverdue > 0:
		return StatusOverdue
	case daysOverdue == 0:
		return StatusDueToday
	default:
		return StatusScheduled
	}
}

// ── Output ────────────────────────────────────────────────────────────────────

// AgingRow adalah satu baris piutang outstanding di laporan.
type AgingRow struct {
	Source        Source `json:"source"`
	ContractID    uint64 `json:"contract_id"`
	UnitID        uint64 `json:"unit_id"`
	RefID         uint64 `json:"ref_id,omitempty"`
	BuyerName     string `json:"buyer_name"`
	UnitCode      string `json:"unit_code"`
	Label         string `json:"label,omitempty"` // keterangan tagihan ("Cicilan #3", "PDAM")
	InvoiceNumber string `json:"invoice_number"`  // "—" bila belum ditagih
	BuyerPhone    string `json:"buyer_phone,omitempty"`
	BuyerEmail    string `json:"buyer_email,omitempty"`
	DueDate       string `json:"due_date"` // YYYY-MM-DD
	Amount        string `json:"amount"`
	Paid          string `json:"paid"`
	Outstanding   string `json:"outstanding"`
	DaysOverdue   int    `json:"days_overdue"`
	Bucket        Bucket `json:"bucket"`
	Status        Status `json:"status"`
	DueThisWeek   bool   `json:"due_this_week"`
}

// BucketSummary adalah ringkasan satu bucket.
type BucketSummary struct {
	Count int    `json:"count"`
	Total string `json:"total"`
}

// SourceSummary adalah subtotal satu sumber eksposur (W-4).
// Ada supaya layar tidak perlu menjumlahkan baris sendiri: satu angka "total
// yang ditagih" hanya boleh lahir di server (D-W4-4).
type SourceSummary struct {
	Source      Source `json:"source"`
	Count       int    `json:"count"`
	Outstanding string `json:"outstanding"`
	Overdue     string `json:"overdue"`
}

// AgingReport adalah output laporan Piutang Customer.
type AgingReport struct {
	AsOf string     `json:"as_of"`
	Rows []AgingRow `json:"rows"`

	// Kartu ringkasan
	TotalPiutang   string `json:"total_piutang"`
	CurrentDue     string `json:"current_due"`
	Overdue        string `json:"overdue"`
	DueThisWeek    string `json:"due_this_week"`
	CollectionRate string `json:"collection_rate"`

	// PS-4 — Collection Command Center
	DueTodayCount  int    `json:"due_today_count"`
	DueTodayAmount string `json:"due_today_amount"`
	Expected30     string `json:"expected_30"`
	Expected60     string `json:"expected_60"`
	Expected90     string `json:"expected_90"`

	// Basis collection rate
	TotalScheduled string `json:"total_scheduled"`
	TotalCollected string `json:"total_collected"`

	// BySource (W-4): subtotal per sumber eksposur, urut house → realization.
	// Selalu terisi untuk sumber yang punya baris.
	BySource []SourceSummary `json:"by_source"`

	// Rincian per bucket
	Buckets struct {
		Current BucketSummary `json:"current"`
		B1_30   BucketSummary `json:"b1_30"`
		B31_60  BucketSummary `json:"b31_60"`
		B61_90  BucketSummary `json:"b61_90"`
		B90Plus BucketSummary `json:"b90_plus"`
	} `json:"buckets"`
}

// ── Mesin ─────────────────────────────────────────────────────────────────────

// BuildAging menyusun laporan Piutang Customer dari baris mentah.
//
// Pure function (tidak ada akses DB). Semua angka uang lewat domain.Money;
// collection rate dihitung dengan decimal (bukan float, Invariant #2).
//
// Aturan:
//   - Baris lunas → masuk TotalCollected; TIDAK muncul di tabel.
//   - Baris bersisa → satu baris outstanding, di-bucket per umur.
//   - Σ Amount semua baris == TotalScheduled (basis collection rate).
//   - CollectionRate = TotalCollected / TotalScheduled × 100 (persen, 2 desimal).
//
// Baris dari sumber berbeda BERBAUR dan tetap diurut seperti diberikan pemanggil
// — memisahkannya kembali per sumber akan mengembalikan dua daftar yang justru
// sedang dihapus W-4.
func BuildAging(rows []Row, asOf time.Time) AgingReport {
	var rpt AgingReport
	rpt.AsOf = asOf.Format("2006-01-02")
	rpt.Rows = make([]AgingRow, 0)

	var totalScheduled, totalCollected, totalOutstanding, currentDue, overdue, dueThisWeek domain.Money
	var dueToday, exp30, exp60, exp90 domain.Money
	dueTodayCount := 0

	bucketTotals := map[Bucket]*domain.Money{
		BucketCurrent: {}, Bucket1_30: {}, Bucket31_60: {}, Bucket61_90: {}, Bucket90Plus: {},
	}
	bucketCounts := map[Bucket]int{}

	// Subtotal per sumber — dikumpulkan sambil jalan supaya tidak ada
	// penjumlahan kedua yang bisa menyimpang dari total utama.
	type srcAcc struct {
		count       int
		outstanding domain.Money
		overdue     domain.Money
	}
	bySource := map[Source]*srcAcc{}

	for _, r := range rows {
		totalScheduled = totalScheduled.Add(r.Amount)

		// effectivePaid: status lunas menyiratkan terbayar penuh (kompat data
		// lama yang tidak mengisi PaidAmount); selain itu pakai PaidAmount.
		effectivePaid := r.PaidAmount
		if r.Received {
			effectivePaid = r.Amount
		}
		totalCollected = totalCollected.Add(effectivePaid)

		outstandingRow := r.Amount.Sub(effectivePaid)
		if outstandingRow.IsZero() || outstandingRow.IsNeg() {
			continue // lunas penuh → tidak masuk tabel
		}

		days := DaysBetween(r.DueDate, asOf)
		bucket := BucketFor(days)

		invNum := r.InvoiceNumber
		if invNum == "" {
			invNum = "—"
		}

		dueThisWeekRow := days >= -7 && days <= 0
		src := r.source()

		rpt.Rows = append(rpt.Rows, AgingRow{
			Source:        src,
			ContractID:    r.ContractID,
			UnitID:        r.UnitID,
			RefID:         r.RefID,
			BuyerName:     r.BuyerName,
			UnitCode:      r.UnitCode,
			Label:         r.Label,
			InvoiceNumber: invNum,
			BuyerPhone:    r.BuyerPhone,
			BuyerEmail:    r.BuyerEmail,
			DueDate:       r.DueDate.Format("2006-01-02"),
			Amount:        r.Amount.String(),
			Paid:          effectivePaid.String(),
			Outstanding:   outstandingRow.String(),
			DaysOverdue:   maxInt(days, 0),
			Bucket:        bucket,
			Status:        EffectiveStatus(days),
			DueThisWeek:   dueThisWeekRow,
		})

		acc, ok := bySource[src]
		if !ok {
			acc = &srcAcc{}
			bySource[src] = acc
		}
		acc.count++
		acc.outstanding = acc.outstanding.Add(outstandingRow)

		totalOutstanding = totalOutstanding.Add(outstandingRow)
		if bucket == BucketCurrent {
			currentDue = currentDue.Add(outstandingRow)
		} else {
			overdue = overdue.Add(outstandingRow)
			acc.overdue = acc.overdue.Add(outstandingRow)
		}
		// "Jatuh tempo minggu ini": hari ini s/d 7 hari ke depan (belum lewat).
		if dueThisWeekRow {
			dueThisWeek = dueThisWeek.Add(outstandingRow)
		}
		// PS-4: due today + prediksi kas masuk (jendela masa depan, kumulatif).
		if days == 0 {
			dueToday = dueToday.Add(outstandingRow)
			dueTodayCount++
		}
		if days >= -30 && days <= 0 {
			exp30 = exp30.Add(outstandingRow)
		}
		if days >= -60 && days <= 0 {
			exp60 = exp60.Add(outstandingRow)
		}
		if days >= -90 && days <= 0 {
			exp90 = exp90.Add(outstandingRow)
		}

		bt := bucketTotals[bucket]
		*bt = bt.Add(outstandingRow)
		bucketCounts[bucket]++
	}

	rpt.TotalPiutang = totalOutstanding.String()
	rpt.CurrentDue = currentDue.String()
	rpt.Overdue = overdue.String()
	rpt.DueThisWeek = dueThisWeek.String()
	rpt.TotalScheduled = totalScheduled.String()
	rpt.TotalCollected = totalCollected.String()
	rpt.CollectionRate = collectionRate(totalCollected, totalScheduled)
	rpt.DueTodayCount = dueTodayCount
	rpt.DueTodayAmount = dueToday.String()
	rpt.Expected30 = exp30.String()
	rpt.Expected60 = exp60.String()
	rpt.Expected90 = exp90.String()

	// Urutan tetap (house dulu) supaya tampilan deterministik — map di Go tidak
	// punya urutan, dan laporan keuangan tidak boleh berubah susunan tiap muat.
	rpt.BySource = make([]SourceSummary, 0, 4)
	for _, s := range []Source{SourceHouse, SourceAddon, SourceRealization, SourceLegacy} {
		if acc, ok := bySource[s]; ok {
			rpt.BySource = append(rpt.BySource, SourceSummary{
				Source:      s,
				Count:       acc.count,
				Outstanding: acc.outstanding.String(),
				Overdue:     acc.overdue.String(),
			})
		}
	}

	rpt.Buckets.Current = BucketSummary{Count: bucketCounts[BucketCurrent], Total: bucketTotals[BucketCurrent].String()}
	rpt.Buckets.B1_30 = BucketSummary{Count: bucketCounts[Bucket1_30], Total: bucketTotals[Bucket1_30].String()}
	rpt.Buckets.B31_60 = BucketSummary{Count: bucketCounts[Bucket31_60], Total: bucketTotals[Bucket31_60].String()}
	rpt.Buckets.B61_90 = BucketSummary{Count: bucketCounts[Bucket61_90], Total: bucketTotals[Bucket61_90].String()}
	rpt.Buckets.B90Plus = BucketSummary{Count: bucketCounts[Bucket90Plus], Total: bucketTotals[Bucket90Plus].String()}

	return rpt
}

// FilterBySource menyaring baris mentah ke satu sumber. Sumber kosong/tidak
// dikenal → seluruh baris dikembalikan apa adanya (perilaku "semua").
func FilterBySource(rows []Row, s Source) []Row {
	if !s.Valid() {
		return rows
	}
	out := make([]Row, 0, len(rows))
	for _, r := range rows {
		if r.source() == s {
			out = append(out, r)
		}
	}
	return out
}

// collectionRate menghitung (collected / scheduled) × 100 sebagai string persen
// dengan 2 desimal. Memakai decimal — TIDAK float. Scheduled nol → "0.00".
func collectionRate(collected, scheduled domain.Money) string {
	if scheduled.IsZero() {
		return "0.00"
	}
	rate := collected.Decimal().
		Div(scheduled.Decimal()).
		Mul(decimal.NewFromInt(100)).
		Round(2)
	return rate.StringFixed(2)
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

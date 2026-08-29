package receivable_test

import (
	"testing"
	"time"

	"esaproperti/internal/domain"
	"esaproperti/internal/receivable"
)

func rupiah(v int64) domain.Money { return domain.FromInt(v) }

func day(y int, m time.Month, d int) time.Time {
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

var asOf = day(2026, time.August, 10)

// row menyusun satu baris dengan default yang masuk akal.
func row(src receivable.Source, due time.Time, amount, paid int64) receivable.Row {
	return receivable.Row{
		Source:     src,
		ContractID: 1,
		UnitID:     1,
		BuyerName:  "Budi",
		UnitCode:   "A-12",
		DueDate:    due,
		Amount:     rupiah(amount),
		PaidAmount: rupiah(paid),
	}
}

// TestBuildAging_TotalSamaDenganJumlahPerSumber adalah INV-AR-2: laporan
// gabungan tidak boleh menghasilkan total yang berbeda dari penjumlahan
// per-sumbernya. Ini inti W-4 — satu eksposur harus benar-benar satu angka.
func TestBuildAging_TotalSamaDenganJumlahPerSumber(t *testing.T) {
	rows := []receivable.Row{
		row(receivable.SourceHouse, day(2026, time.July, 1), 100_000_000, 40_000_000),    // sisa 60jt, telat
		row(receivable.SourceHouse, day(2026, time.September, 1), 50_000_000, 0),         // sisa 50jt, belum JT
		row(receivable.SourceRealization, day(2026, time.July, 20), 4_000_000, 0),        // sisa 4jt, telat
		row(receivable.SourceRealization, day(2026, time.August, 30), 1_000_000, 0),      // sisa 1jt, belum JT
		row(receivable.SourceRealization, day(2026, time.June, 1), 2_000_000, 2_000_000), // lunas — tidak muncul
	}

	rpt := receivable.BuildAging(rows, asOf)

	if got, want := rpt.TotalPiutang, "115000000"; got != want {
		t.Fatalf("TotalPiutang = %s, want %s", got, want)
	}
	if len(rpt.BySource) != 2 {
		t.Fatalf("BySource = %d entri, want 2", len(rpt.BySource))
	}

	var sumOutstanding, sumOverdue domain.Money
	for _, s := range rpt.BySource {
		o, err := domain.NewMoney(s.Outstanding)
		if err != nil {
			t.Fatalf("outstanding %q: %v", s.Outstanding, err)
		}
		ov, err := domain.NewMoney(s.Overdue)
		if err != nil {
			t.Fatalf("overdue %q: %v", s.Overdue, err)
		}
		sumOutstanding = sumOutstanding.Add(o)
		sumOverdue = sumOverdue.Add(ov)
	}
	if sumOutstanding.String() != rpt.TotalPiutang {
		t.Errorf("INV-AR-2 dilanggar: Σ BySource.Outstanding = %s, TotalPiutang = %s",
			sumOutstanding, rpt.TotalPiutang)
	}
	if sumOverdue.String() != rpt.Overdue {
		t.Errorf("INV-AR-2 dilanggar: Σ BySource.Overdue = %s, Overdue = %s",
			sumOverdue, rpt.Overdue)
	}

	// Urutan BySource harus tetap (house dulu) — laporan keuangan tidak boleh
	// berubah susunan tiap kali dimuat.
	if rpt.BySource[0].Source != receivable.SourceHouse ||
		rpt.BySource[1].Source != receivable.SourceRealization {
		t.Errorf("urutan BySource tidak deterministik: %v", rpt.BySource)
	}
}

// TestBuildAging_BarisLunasTidakMuncul menjaga aturan lama: yang sudah lunas
// masuk TotalCollected tapi tidak menghuni tabel tunggakan.
func TestBuildAging_BarisLunasTidakMuncul(t *testing.T) {
	rows := []receivable.Row{
		row(receivable.SourceHouse, day(2026, time.June, 1), 10_000_000, 10_000_000),
		{Source: receivable.SourceHouse, DueDate: day(2026, time.June, 1),
			Amount: rupiah(5_000_000), Received: true}, // received tanpa paid_amount (data lama)
	}
	rpt := receivable.BuildAging(rows, asOf)

	if len(rpt.Rows) != 0 {
		t.Errorf("baris lunas muncul di tabel: %+v", rpt.Rows)
	}
	if rpt.TotalCollected != "15000000" {
		t.Errorf("TotalCollected = %s, want 15000000", rpt.TotalCollected)
	}
	if rpt.CollectionRate != "100.00" {
		t.Errorf("CollectionRate = %s, want 100.00", rpt.CollectionRate)
	}
	// Tidak ada baris outstanding → tidak ada subtotal sumber yang menyesatkan.
	if len(rpt.BySource) != 0 {
		t.Errorf("BySource terisi padahal tak ada tunggakan: %+v", rpt.BySource)
	}
}

// TestBuildAging_SumberKosongDianggapHargaRumah menjaga kompatibilitas: baris
// dari pemanggil pra-W-4 tidak membawa Source dan tidak boleh jatuh ke sumber
// tak dikenal.
func TestBuildAging_SumberKosongDianggapHargaRumah(t *testing.T) {
	rows := []receivable.Row{{
		DueDate: day(2026, time.July, 1), Amount: rupiah(1_000_000),
	}}
	rpt := receivable.BuildAging(rows, asOf)

	if len(rpt.Rows) != 1 || rpt.Rows[0].Source != receivable.SourceHouse {
		t.Fatalf("baris tanpa Source tidak jatuh ke house: %+v", rpt.Rows)
	}
}

func TestFilterBySource(t *testing.T) {
	rows := []receivable.Row{
		row(receivable.SourceHouse, day(2026, time.July, 1), 10, 0),
		row(receivable.SourceRealization, day(2026, time.July, 1), 20, 0),
		{DueDate: day(2026, time.July, 1), Amount: rupiah(30)}, // Source kosong → house
	}

	if got := len(receivable.FilterBySource(rows, receivable.SourceHouse)); got != 2 {
		t.Errorf("filter house = %d baris, want 2 (termasuk baris tanpa Source)", got)
	}
	if got := len(receivable.FilterBySource(rows, receivable.SourceRealization)); got != 1 {
		t.Errorf("filter realization = %d baris, want 1", got)
	}
	// Sumber tak dikenal berarti "semua" — dipakai handler untuk nilai kosong.
	if got := len(receivable.FilterBySource(rows, "")); got != 3 {
		t.Errorf("filter kosong = %d baris, want 3", got)
	}
}

func TestBucketDanStatus(t *testing.T) {
	cases := []struct {
		name   string
		due    time.Time
		bucket receivable.Bucket
		status receivable.Status
		days   int
	}{
		{"belum jatuh tempo", day(2026, time.September, 1), receivable.BucketCurrent, receivable.StatusScheduled, 0},
		{"jatuh tempo hari ini", asOf, receivable.BucketCurrent, receivable.StatusDueToday, 0},
		{"telat 1 hari", day(2026, time.August, 9), receivable.Bucket1_30, receivable.StatusOverdue, 1},
		{"telat 30 hari", day(2026, time.July, 11), receivable.Bucket1_30, receivable.StatusOverdue, 30},
		{"telat 31 hari", day(2026, time.July, 10), receivable.Bucket31_60, receivable.StatusOverdue, 31},
		{"telat 91 hari", day(2026, time.May, 11), receivable.Bucket90Plus, receivable.StatusOverdue, 91},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rpt := receivable.BuildAging(
				[]receivable.Row{row(receivable.SourceHouse, tc.due, 1_000_000, 0)}, asOf)
			if len(rpt.Rows) != 1 {
				t.Fatalf("want 1 baris, got %d", len(rpt.Rows))
			}
			got := rpt.Rows[0]
			if got.Bucket != tc.bucket {
				t.Errorf("bucket = %s, want %s", got.Bucket, tc.bucket)
			}
			if got.Status != tc.status {
				t.Errorf("status = %s, want %s", got.Status, tc.status)
			}
			if got.DaysOverdue != tc.days {
				t.Errorf("days_overdue = %d, want %d", got.DaysOverdue, tc.days)
			}
		})
	}
}

// TestDaysBetween_BebasEfekJam: dua waktu di hari yang sama tapi jam berbeda
// harus menghasilkan umur yang sama. Laporan per tanggal tidak boleh berubah
// hanya karena dibuka sore hari.
func TestDaysBetween_BebasEfekJam(t *testing.T) {
	due := time.Date(2026, time.July, 1, 23, 59, 0, 0, time.UTC)
	pagi := time.Date(2026, time.July, 11, 0, 1, 0, 0, time.UTC)
	sore := time.Date(2026, time.July, 11, 23, 58, 0, 0, time.UTC)

	if a, b := receivable.DaysBetween(due, pagi), receivable.DaysBetween(due, sore); a != b {
		t.Errorf("umur berubah karena jam: pagi=%d sore=%d", a, b)
	}
	if got := receivable.DaysBetween(due, pagi); got != 10 {
		t.Errorf("DaysBetween = %d, want 10", got)
	}
}

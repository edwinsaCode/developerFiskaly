package reporting_test

import (
	"context"
	"testing"
	"time"

	"esaproperti/internal/domain"
	"esaproperti/internal/reporting"
)

var arAsOf = time.Date(2026, 6, 29, 0, 0, 0, 0, time.UTC)

// dueDaysBefore mengembalikan tanggal jatuh tempo `days` hari sebelum arAsOf.
// days negatif → jatuh tempo di masa depan (belum jatuh tempo).
func dueDaysBefore(days int) time.Time {
	return arAsOf.AddDate(0, 0, -days)
}

func arRow(buyer, unit, inv string, due time.Time, amount domain.Money, received bool) reporting.ARScheduleRow {
	return reporting.ARScheduleRow{
		ContractID:    1,
		UnitID:        1,
		BuyerName:     buyer,
		UnitCode:      unit,
		InvoiceNumber: inv,
		DueDate:       due,
		Amount:        amount,
		Received:      received,
	}
}

// TestBuildARAging_BucketClassification memverifikasi setiap baris masuk ke
// bucket umur yang benar berdasarkan selisih hari dueDate→asOf.
func TestBuildARAging_BucketClassification(t *testing.T) {
	amt := domain.FromInt(100_000_000)
	rows := []reporting.ARScheduleRow{
		arRow("A", "U1", "INV/1", dueDaysBefore(-5), amt, false), // belum jatuh tempo → current
		arRow("B", "U2", "INV/2", dueDaysBefore(0), amt, false),  // jatuh tempo hari ini → current (days=0)
		arRow("C", "U3", "INV/3", dueDaysBefore(1), amt, false),  // telat 1 hari → 1_30
		arRow("D", "U4", "INV/4", dueDaysBefore(30), amt, false), // telat 30 → 1_30
		arRow("E", "U5", "INV/5", dueDaysBefore(31), amt, false), // telat 31 → 31_60
		arRow("F", "U6", "INV/6", dueDaysBefore(60), amt, false), // telat 60 → 31_60
		arRow("G", "U7", "INV/7", dueDaysBefore(61), amt, false), // telat 61 → 61_90
		arRow("H", "U8", "INV/8", dueDaysBefore(90), amt, false), // telat 90 → 61_90
		arRow("I", "U9", "INV/9", dueDaysBefore(91), amt, false), // telat 91 → 90_plus
	}

	rpt := reporting.BuildARAging(rows, arAsOf)

	if rpt.Buckets.Current.Count != 2 {
		t.Errorf("Current count: got %d, want 2", rpt.Buckets.Current.Count)
	}
	if rpt.Buckets.B1_30.Count != 2 {
		t.Errorf("1-30 count: got %d, want 2", rpt.Buckets.B1_30.Count)
	}
	if rpt.Buckets.B31_60.Count != 2 {
		t.Errorf("31-60 count: got %d, want 2", rpt.Buckets.B31_60.Count)
	}
	if rpt.Buckets.B61_90.Count != 2 {
		t.Errorf("61-90 count: got %d, want 2", rpt.Buckets.B61_90.Count)
	}
	if rpt.Buckets.B90Plus.Count != 1 {
		t.Errorf("90+ count: got %d, want 1", rpt.Buckets.B90Plus.Count)
	}

	// CurrentDue = 2 × 100jt; Overdue = 7 × 100jt; TotalPiutang = 9 × 100jt.
	if rpt.CurrentDue != "200000000" {
		t.Errorf("CurrentDue: got %s, want 200000000", rpt.CurrentDue)
	}
	if rpt.Overdue != "700000000" {
		t.Errorf("Overdue: got %s, want 700000000", rpt.Overdue)
	}
	if rpt.TotalPiutang != "900000000" {
		t.Errorf("TotalPiutang: got %s, want 900000000", rpt.TotalPiutang)
	}
}

// TestBuildARAging_CollectionRateAndReconciliation memverifikasi:
//   - cicilan received masuk ke collected, BUKAN ke tabel outstanding
//   - CollectionRate = collected / scheduled × 100
//   - TotalScheduled == TotalCollected + TotalPiutang (rekonsiliasi, Invariant #3 semangat)
func TestBuildARAging_CollectionRateAndReconciliation(t *testing.T) {
	rows := []reporting.ARScheduleRow{
		arRow("A", "U1", "INV/1", dueDaysBefore(10), domain.FromInt(300_000_000), true),  // dibayar
		arRow("A", "U1", "INV/2", dueDaysBefore(5), domain.FromInt(200_000_000), false),  // outstanding overdue
		arRow("A", "U1", "", dueDaysBefore(-20), domain.FromInt(500_000_000), false),     // outstanding current, belum ditagih
	}

	rpt := reporting.BuildARAging(rows, arAsOf)

	if rpt.TotalCollected != "300000000" {
		t.Errorf("TotalCollected: got %s, want 300000000", rpt.TotalCollected)
	}
	if rpt.TotalScheduled != "1000000000" {
		t.Errorf("TotalScheduled: got %s, want 1000000000", rpt.TotalScheduled)
	}
	if rpt.TotalPiutang != "700000000" {
		t.Errorf("TotalPiutang: got %s, want 700000000", rpt.TotalPiutang)
	}
	// 300jt / 1.000jt = 30.00%
	if rpt.CollectionRate != "30.00" {
		t.Errorf("CollectionRate: got %s, want 30.00", rpt.CollectionRate)
	}

	// Rekonsiliasi: scheduled == collected + outstanding
	scheduled := mustMoney(rpt.TotalScheduled)
	collected := mustMoney(rpt.TotalCollected)
	outstanding := mustMoney(rpt.TotalPiutang)
	if !scheduled.Equal(collected.Add(outstanding)) {
		t.Errorf("rekonsiliasi gagal: scheduled=%s != collected+outstanding=%s",
			scheduled.String(), collected.Add(outstanding).String())
	}

	// Hanya 2 baris outstanding di tabel (received di-skip).
	if len(rpt.Rows) != 2 {
		t.Fatalf("Rows: got %d, want 2 (received di-skip)", len(rpt.Rows))
	}
	// Baris tanpa invoice → "—".
	var foundDash bool
	for _, r := range rpt.Rows {
		if r.InvoiceNumber == "—" {
			foundDash = true
		}
		// Setiap baris outstanding: Paid=0, Outstanding=Amount.
		if r.Paid != "0" {
			t.Errorf("baris outstanding Paid harus 0, got %s", r.Paid)
		}
		if r.Outstanding != r.Amount {
			t.Errorf("Outstanding (%s) harus == Amount (%s)", r.Outstanding, r.Amount)
		}
	}
	if !foundDash {
		t.Error("baris tanpa invoice harus menampilkan '—'")
	}
}

// TestBuildARAging_StatusAndDueThisWeek memverifikasi status pembayaran yang
// dihitung backend dan akumulasi "jatuh tempo minggu ini".
func TestBuildARAging_StatusAndDueThisWeek(t *testing.T) {
	amt := domain.FromInt(100_000_000)
	rows := []reporting.ARScheduleRow{
		arRow("A", "U1", "INV/1", dueDaysBefore(-10), amt, false), // due +10h → scheduled, BUKAN minggu ini
		arRow("B", "U2", "INV/2", dueDaysBefore(-3), amt, false),  // due +3h → scheduled, minggu ini
		arRow("C", "U3", "INV/3", dueDaysBefore(0), amt, false),   // due hari ini → due_today, minggu ini
		arRow("D", "U4", "INV/4", dueDaysBefore(5), amt, false),   // telat 5h → overdue
		arRow("E", "U5", "INV/5", dueDaysBefore(10), amt, true),   // lunas → tidak muncul
	}

	rpt := reporting.BuildARAging(rows, arAsOf)

	statusByUnit := map[string]reporting.PaymentStatus{}
	for _, r := range rpt.Rows {
		statusByUnit[r.UnitCode] = r.Status
	}
	if statusByUnit["U1"] != reporting.PaymentScheduled {
		t.Errorf("U1 status: got %s, want scheduled", statusByUnit["U1"])
	}
	if statusByUnit["U3"] != reporting.PaymentDueToday {
		t.Errorf("U3 status: got %s, want due_today", statusByUnit["U3"])
	}
	if statusByUnit["U4"] != reporting.PaymentOverdue {
		t.Errorf("U4 status: got %s, want overdue", statusByUnit["U4"])
	}
	if _, ok := statusByUnit["U5"]; ok {
		t.Error("U5 (lunas) tidak boleh muncul di rows")
	}

	// Due this week = U2 (due +3h) + U3 (due hari ini) = 200jt. U1 (+10h) di luar.
	if rpt.DueThisWeek != "200000000" {
		t.Errorf("DueThisWeek: got %s, want 200000000 (U2 + U3)", rpt.DueThisWeek)
	}
}

// TestBuildARAging_AllPaid_CollectionRate100 memverifikasi kasus semua lunas.
func TestBuildARAging_AllPaid_CollectionRate100(t *testing.T) {
	rows := []reporting.ARScheduleRow{
		arRow("A", "U1", "INV/1", dueDaysBefore(10), domain.FromInt(400_000_000), true),
		arRow("A", "U1", "INV/2", dueDaysBefore(5), domain.FromInt(600_000_000), true),
	}
	rpt := reporting.BuildARAging(rows, arAsOf)

	if rpt.CollectionRate != "100.00" {
		t.Errorf("CollectionRate: got %s, want 100.00", rpt.CollectionRate)
	}
	if rpt.TotalPiutang != "0" {
		t.Errorf("TotalPiutang: got %s, want 0", rpt.TotalPiutang)
	}
	if len(rpt.Rows) != 0 {
		t.Errorf("Rows: got %d, want 0 (semua lunas)", len(rpt.Rows))
	}
}

// TestBuildARAging_Empty memverifikasi laporan kosong tidak panik & rate aman.
func TestBuildARAging_Empty(t *testing.T) {
	rpt := reporting.BuildARAging(nil, arAsOf)
	if rpt.CollectionRate != "0.00" {
		t.Errorf("CollectionRate kosong: got %s, want 0.00", rpt.CollectionRate)
	}
	if rpt.TotalPiutang != "0" {
		t.Errorf("TotalPiutang kosong: got %s, want 0", rpt.TotalPiutang)
	}
	if rpt.Rows == nil {
		t.Error("Rows harus non-nil slice (untuk JSON [] bukan null)")
	}
}

// ── Service-level test dengan mock reader ────────────────────────────────────

// mockARReader berperan sebagai sumber piutang harga rumah (W-8). Sejak W-8
// pembacanya mengembalikan baris siap-agregat, bukan baris jadwal mentah:
// yang menentukan BERAPA piutangnya adalah pemilik sub-ledger, bukan laporan.
type mockARReader struct {
	rows []reporting.ARScheduleRow
	err  error
}

func (m *mockARReader) ReceivableRows(_ context.Context, _ uint64) ([]reporting.ARScheduleRow, error) {
	return m.rows, m.err
}

func TestGetARAging_DelegatesToReader(t *testing.T) {
	reader := &mockARReader{rows: []reporting.ARScheduleRow{
		arRow("A", "U1", "INV/1", dueDaysBefore(40), domain.FromInt(150_000_000), false),
	}}
	svc := reporting.NewService(
		&mockLedgerQuerier{}, &mockPLReader{}, &mockPipelineReader{}, &mockCashFlowReader{},
	).WithHouseReader(reader)

	rpt, err := svc.GetARAging(context.Background(), 1, arAsOf, "")
	if err != nil {
		t.Fatalf("GetARAging: %v", err)
	}
	if rpt.TotalPiutang != "150000000" {
		t.Errorf("TotalPiutang: got %s, want 150000000", rpt.TotalPiutang)
	}
	if rpt.Buckets.B31_60.Count != 1 {
		t.Errorf("baris 40 hari harus di bucket 31-60, buckets=%+v", rpt.Buckets)
	}
}

// TestGetARAging_NotConfigured memverifikasi error bila reader belum dipasang.
func TestGetARAging_NotConfigured(t *testing.T) {
	svc := reporting.NewService(
		&mockLedgerQuerier{}, &mockPLReader{}, &mockPipelineReader{}, &mockCashFlowReader{},
	)
	_, err := svc.GetARAging(context.Background(), 1, arAsOf, "")
	if err == nil {
		t.Fatal("harus error ketika pembaca piutang rumah belum dikonfigurasi")
	}
}

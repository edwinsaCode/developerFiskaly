package sale

// W-8 / P6 — Jadwal Penagihan pra-BAST.
//
// BD-1 memindahkan jadwal pembayaran sebelum serah terima KELUAR dari laporan
// piutang: sebelum BAST belum ada piutang yang diakui. Tetapi cicilannya tetap
// wajib ditagih — dan tim penagihan membaca daftar kerjanya dari laporan yang
// sama. Tanpa layar ini, W-8 akan menghapus daftar kerja mereka tanpa menggantinya.
//
// Yang dibaca di sini adalah KOMPLEMEN house AR:
//
//	house AR          = kontrak yang unitnya SUDAH punya SaleRecord aktif
//	jadwal penagihan  = kontrak yang unitnya BELUM punya SaleRecord aktif
//
// Keduanya dihitung di paket yang sama supaya tidak bisa berbeda definisi, dan
// status per cicilan memakai fungsi yang sama dengan statement (scheduleStatusAt)
// — bukan salinan logika kedua. Ini BUKAN mesin aging kedua (INV-AR-1): tidak ada
// bucket umur, tidak ada saldo piutang, tidak ada akun kontrol. Ia hanya menjawab
// "apa yang harus ditagih dan kapan" atas kewajiban yang belum menjadi piutang.

import (
	"context"
	"fmt"
	"sort"
	"time"

	"esaproperti/internal/domain"
)

// ── Status cicilan: satu definisi, dua pemakai ────────────────────────────────

// scheduleState adalah hasil penilaian satu baris jadwal terhadap tanggal acuan.
type scheduleState struct {
	Paid        domain.Money
	Outstanding domain.Money
	Status      string // paid | overdue | due_today | scheduled
	Overdue     bool
	DaysOverdue int
}

// scheduleStatusAt menilai satu baris jadwal. Dipakai statement kontrak DAN
// Jadwal Penagihan; kalau salah satu punya salinannya sendiri, pembeli dan
// penagih akan melihat dua status berbeda untuk cicilan yang sama.
//
// T-5 (keputusan klien 2026-08-05): tunggakan = SISA (amount − paid), bukan
// nominal bruto. `received` menyiratkan lunas penuh — kompatibilitas data lama
// yang tidak mengisi paid_amount.
func scheduleStatusAt(amount, paid domain.Money, received bool, due, asOf time.Time) scheduleState {
	st := scheduleState{Paid: paid}
	if received {
		st.Paid = amount
	}
	st.Outstanding = amount.Sub(st.Paid)
	if st.Outstanding.IsNeg() {
		st.Outstanding = domain.Zero
	}
	st.DaysOverdue = statementDaysOverdue(due, asOf)
	st.Overdue = st.Outstanding.GreaterThan(domain.Zero) && st.DaysOverdue > 0

	switch {
	case received || !st.Outstanding.GreaterThan(domain.Zero):
		st.Status = "paid"
	case st.DaysOverdue > 0:
		st.Status = "overdue"
	case st.DaysOverdue == 0:
		st.Status = "due_today"
	default:
		st.Status = "scheduled"
	}
	return st
}

// ── Seam data ─────────────────────────────────────────────────────────────────

// PreBASTSchedule adalah satu baris jadwal milik kontrak yang unitnya BELUM
// diserahterimakan, lengkap dengan konteks pembeli untuk layar penagihan.
type PreBASTSchedule struct {
	ContractID    uint64       `gorm:"column:contract_id"`
	UnitID        uint64       `gorm:"column:unit_id"`
	UnitCode      string       `gorm:"column:unit_code"`
	BuyerName     string       `gorm:"column:buyer_name"`
	BuyerPhone    string       `gorm:"column:buyer_phone"`
	ContractValue domain.Money `gorm:"column:contract_value"`
	SchemeState   string       `gorm:"column:scheme_state"`

	ScheduleID    uint64       `gorm:"column:schedule_id"`
	InstallmentNo int          `gorm:"column:installment_number"`
	Type          string       `gorm:"column:type"`
	DueDate       time.Time    `gorm:"column:due_date"`
	Amount        domain.Money `gorm:"column:amount"`
	PaidAmount    domain.Money `gorm:"column:paid_amount"`
	Status        string       `gorm:"column:status"`
	InvoiceNumber string       `gorm:"column:invoice_number"`
}

// BillingPlanStore membaca jadwal pra-BAST. Terpisah dari HouseARStore karena
// menjawab pertanyaan yang berbeda, meski komplementer.
type BillingPlanStore interface {
	ListPreBASTSchedules(ctx context.Context, tenantID uint64) ([]PreBASTSchedule, error)
}

// WithBillingPlanStore memasang sumber Jadwal Penagihan.
func WithBillingPlanStore(bs BillingPlanStore) ServiceOption {
	return func(s *Service) { s.billingPlan = bs }
}

// ErrBillingPlanNotConfigured — fail-closed, sama alasannya dengan house AR:
// daftar penagihan yang diam-diam kosong membuat tagihan tidak tertagih.
var ErrBillingPlanNotConfigured = fmt.Errorf("sale: pembaca jadwal penagihan belum dikonfigurasi")

// ── Bentuk laporan ────────────────────────────────────────────────────────────

// BillingPlanLine adalah satu cicilan pada Jadwal Penagihan.
type BillingPlanLine struct {
	ScheduleID        uint64 `json:"schedule_id"`
	InstallmentNumber int    `json:"installment_number"`
	Type              string `json:"type"`
	Label             string `json:"label"`
	DueDate           string `json:"due_date"`
	Amount            string `json:"amount"`
	Paid              string `json:"paid"`
	Outstanding       string `json:"outstanding"`
	Status            string `json:"status"` // paid | overdue | due_today | scheduled
	DaysOverdue       int    `json:"days_overdue"`
	InvoiceNumber     string `json:"invoice_number,omitempty"`
}

// BillingPlanUnit mengelompokkan cicilan per unit — satuan kerja penagihan
// adalah pembeli/unit, bukan baris cicilan lepas.
type BillingPlanUnit struct {
	ContractID    uint64  `json:"contract_id"`
	UnitID        uint64  `json:"unit_id"`
	UnitCode      string  `json:"unit_code"`
	BuyerName     string  `json:"buyer_name"`
	BuyerPhone    string  `json:"buyer_phone,omitempty"`
	SchemeState   string  `json:"scheme_state,omitempty"`
	ContractValue string  `json:"contract_value"`
	Scheduled     string  `json:"scheduled"`
	Paid          string  `json:"paid"`
	Outstanding   string  `json:"outstanding"`
	OverdueAmount string  `json:"overdue_amount"`
	OverdueCount  int     `json:"overdue_count"`
	NextDueDate   *string `json:"next_due_date,omitempty"`

	Schedules []BillingPlanLine `json:"schedules"`
}

// BillingPlan adalah seluruh kewajiban terjadwal yang BELUM menjadi piutang.
type BillingPlan struct {
	AsOf             string            `json:"as_of"`
	TotalScheduled   string            `json:"total_scheduled"`
	TotalPaid        string            `json:"total_paid"`
	TotalOutstanding string            `json:"total_outstanding"`
	TotalOverdue     string            `json:"total_overdue"`
	UnitCount        int               `json:"unit_count"`
	OverdueUnitCount int               `json:"overdue_unit_count"`
	Note             string            `json:"note"`
	Units            []BillingPlanUnit `json:"units"`
}

// billingPlanNote adalah kalimat yang membuat layar ini tidak bisa disalahbaca
// sebagai laporan piutang. Disimpan di backend supaya definisinya satu.
const billingPlanNote = "Belum diserahterimakan (BAST) — kewajiban terjadwal ini BELUM diakui sebagai piutang. Piutang harga rumah lahir saat BAST."

// BillingPlanFor menyusun Jadwal Penagihan pra-BAST per tanggal acuan.
//
// Urutan unit: yang menunggak lebih dulu (terbesar), lalu jatuh tempo terdekat.
// Layar penagihan yang mengurut berdasarkan id unit membuat pekerjaan paling
// mendesak tenggelam di halaman ketiga.
func (s *Service) BillingPlanFor(ctx context.Context, tenantID uint64, asOf time.Time) (*BillingPlan, error) {
	if s.billingPlan == nil {
		return nil, ErrBillingPlanNotConfigured
	}
	rows, err := s.billingPlan.ListPreBASTSchedules(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("baca jadwal pra-BAST: %w", err)
	}

	out := &BillingPlan{
		AsOf:  asOf.Format("2006-01-02"),
		Note:  billingPlanNote,
		Units: make([]BillingPlanUnit, 0, 8),
	}
	totScheduled, totPaid, totOutstanding, totOverdue := domain.Zero, domain.Zero, domain.Zero, domain.Zero

	// Akumulasi dalam Money, bukan string: laporan uang tidak boleh melewati
	// parse/format berulang (Aturan uang).
	type acc struct {
		unit                              BillingPlanUnit
		scheduled, paid, outstanding, ovd domain.Money
	}
	accs := make([]*acc, 0, 8)
	idx := map[uint64]int{} // unitID → posisi

	for _, r := range rows {
		pos, seen := idx[r.UnitID]
		if !seen {
			accs = append(accs, &acc{unit: BillingPlanUnit{
				ContractID:    r.ContractID,
				UnitID:        r.UnitID,
				UnitCode:      r.UnitCode,
				BuyerName:     r.BuyerName,
				BuyerPhone:    r.BuyerPhone,
				SchemeState:   r.SchemeState,
				ContractValue: r.ContractValue.String(),
				Schedules:     make([]BillingPlanLine, 0, 4),
			}})
			pos = len(accs) - 1
			idx[r.UnitID] = pos
		}
		a := accs[pos]
		u := &a.unit

		st := scheduleStatusAt(r.Amount, r.PaidAmount,
			ScheduleStatus(r.Status) == ScheduleStatusReceived, r.DueDate, asOf)

		line := BillingPlanLine{
			ScheduleID:        r.ScheduleID,
			InstallmentNumber: r.InstallmentNo,
			Type:              r.Type,
			Label:             houseScheduleLabel(r.Type, r.InstallmentNo),
			DueDate:           r.DueDate.Format("2006-01-02"),
			Amount:            r.Amount.String(),
			Paid:              st.Paid.String(),
			Outstanding:       st.Outstanding.String(),
			Status:            st.Status,
			InvoiceNumber:     r.InvoiceNumber,
		}
		if st.Overdue {
			line.DaysOverdue = st.DaysOverdue
		}
		u.Schedules = append(u.Schedules, line)

		a.scheduled = a.scheduled.Add(r.Amount)
		a.paid = a.paid.Add(st.Paid)
		a.outstanding = a.outstanding.Add(st.Outstanding)
		totScheduled = totScheduled.Add(r.Amount)
		totPaid = totPaid.Add(st.Paid)
		totOutstanding = totOutstanding.Add(st.Outstanding)

		if st.Overdue {
			a.ovd = a.ovd.Add(st.Outstanding)
			u.OverdueCount++
			totOverdue = totOverdue.Add(st.Outstanding)
		}
		// Jatuh tempo berikutnya = cicilan bersisa paling awal yang belum lewat.
		if st.Outstanding.GreaterThan(domain.Zero) && st.DaysOverdue <= 0 {
			if u.NextDueDate == nil || line.DueDate < *u.NextDueDate {
				d := line.DueDate
				u.NextDueDate = &d
			}
		}
	}

	sort.SliceStable(accs, func(i, j int) bool {
		a, b := accs[i].unit, accs[j].unit
		if (a.OverdueCount > 0) != (b.OverdueCount > 0) {
			return a.OverdueCount > 0
		}
		if a.OverdueCount > 0 && b.OverdueCount > 0 && !accs[i].ovd.Sub(accs[j].ovd).IsZero() {
			return accs[i].ovd.GreaterThan(accs[j].ovd)
		}
		switch {
		case a.NextDueDate != nil && b.NextDueDate != nil && *a.NextDueDate != *b.NextDueDate:
			return *a.NextDueDate < *b.NextDueDate
		case a.NextDueDate != nil && b.NextDueDate == nil:
			return true
		case a.NextDueDate == nil && b.NextDueDate != nil:
			return false
		}
		return a.UnitCode < b.UnitCode
	})

	for _, a := range accs {
		u := a.unit
		u.Scheduled = a.scheduled.String()
		u.Paid = a.paid.String()
		u.Outstanding = a.outstanding.String()
		u.OverdueAmount = a.ovd.String()
		if u.OverdueCount > 0 {
			out.OverdueUnitCount++
		}
		out.Units = append(out.Units, u)
	}

	out.UnitCount = len(out.Units)
	out.TotalScheduled = totScheduled.String()
	out.TotalPaid = totPaid.String()
	out.TotalOutstanding = totOutstanding.String()
	out.TotalOverdue = totOverdue.String()
	return out, nil
}

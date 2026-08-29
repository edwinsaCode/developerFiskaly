package sale

import (
	"sort"
	"strconv"

	"esaproperti/internal/domain"
)

// ── FE-2 · P3 — Backfill payment_allocations (replay + rekonsiliasi) ──────────
//
// Termin lama (pra-FE-2) tak punya baris payment_allocations. Backfill
// merekonstruksinya dengan ME-REPLAY alokasi waterfall per unit berurut tanggal
// termin (dari paid=0). BEST-EFFORT (keputusan #1): hanya unit yang paid_amount
// hasil rekonstruksinya SAMA PERSIS dengan cache paid_amount saat ini yang
// ditulis. Unit yang tidak cocok (mis. histori pernah menargetkan cicilan spesifik
// via schedule_received, bukan waterfall) DITANDAI untuk review manual — TIDAK
// pernah di-auto-merge. Backfill TIDAK menyentuh paid_amount (itu adalah target
// rekonsiliasi, bukan yang ditulis) — hanya menyisipkan baris sub-ledger.

// BackfillFlag menandai satu unit yang tidak rekonsiliasi (butuh review manual).
type BackfillFlag struct {
	UnitID     uint64 `json:"unit_id"`
	ContractID uint64 `json:"contract_id,omitempty"`
	Reason     string `json:"reason"`
	Detail     string `json:"detail,omitempty"`
}

// BackfillReport meringkas hasil backfill (dipakai untuk laporan rekonsiliasi).
type BackfillReport struct {
	TenantID           uint64         `json:"tenant_id"`
	Apply              bool           `json:"apply"` // false = dry-run (tidak menulis)
	UnitsProcessed     int            `json:"units_processed"`
	UnitsReconciled    int            `json:"units_reconciled"`
	UnitsFlagged       int            `json:"units_flagged"`
	TerminsBackfilled  int            `json:"termins_backfilled"`   // termin yang baru dapat alokasi
	TerminsSkipped     int            `json:"termins_skipped"`      // sudah punya alokasi (idempoten)
	AllocationsToWrite int            `json:"allocations_to_write"` // baris alokasi (rencana/tertulis)
	Flags              []BackfillFlag `json:"flags,omitempty"`
}

// terminAllocationPlan adalah rencana alokasi untuk SATU termin hasil replay.
type terminAllocationPlan struct {
	terminID    uint64
	createdBy   *uint64
	schedule    []ScheduleAllocation
	buyerCredit domain.Money
}

// unitBackfillPlan adalah rencana backfill untuk satu unit + status rekonsiliasi.
type unitBackfillPlan struct {
	unitID     uint64
	contractID uint64
	reconciled bool
	reason     string
	detail     string
	termins    []terminAllocationPlan
}

// planUnitBackfill (PURE) merekonstruksi alokasi seluruh termin sebuah unit dan
// menilai rekonsiliasi terhadap cache paid_amount cicilan saat ini.
//
//   - Tanpa cicilan (advance tanpa kontrak): tiap termin → buyer_credit penuh;
//     selalu reconciled (tidak ada paid_amount untuk dibandingkan).
//   - Dengan cicilan: replay waterfall berurut tanggal dari paid=0; reconciled
//     hanya bila paid_amount rekonstruksi == paid_amount aktual untuk SEMUA cicilan.
func planUnitBackfill(unitID, contractID uint64, termins []*TerminPayment, schedules []*PaymentSchedule) unitBackfillPlan {
	plan := unitBackfillPlan{unitID: unitID, contractID: contractID}

	// Urutkan termin: tanggal lalu ID (deterministik).
	sorted := append([]*TerminPayment(nil), termins...)
	sort.SliceStable(sorted, func(i, j int) bool {
		if !sorted[i].Date.Equal(sorted[j].Date) {
			return sorted[i].Date.Before(sorted[j].Date)
		}
		return sorted[i].ID < sorted[j].ID
	})

	// Tanpa cicilan → seluruh amount tiap termin jadi buyer_credit.
	if len(schedules) == 0 {
		for _, t := range sorted {
			plan.termins = append(plan.termins, terminAllocationPlan{
				terminID: t.ID, createdBy: t.CreatedBy, buyerCredit: t.Amount,
			})
		}
		plan.reconciled = true
		return plan
	}

	// Clone cicilan dgn paid=0 untuk replay; simpan paid rekonstruksi.
	clones := make([]*PaymentSchedule, len(schedules))
	reconstructed := make(map[uint64]domain.Money, len(schedules))
	for i, s := range schedules {
		c := *s
		c.PaidAmount = domain.Zero
		clones[i] = &c
		reconstructed[s.ID] = domain.Zero
	}

	for _, t := range sorted {
		alloc, unapplied := planAllocation(clones, t.Amount)
		tp := terminAllocationPlan{terminID: t.ID, createdBy: t.CreatedBy, buyerCredit: unapplied}
		for _, p := range alloc {
			tp.schedule = append(tp.schedule, ScheduleAllocation{
				ScheduleID: p.schedule.ID, Apply: p.apply, NewPaid: p.newPaid, FullyPaid: p.fullyPaid,
			})
			for _, c := range clones {
				if c.ID == p.schedule.ID {
					c.PaidAmount = p.newPaid
					reconstructed[c.ID] = p.newPaid
				}
			}
		}
		plan.termins = append(plan.termins, tp)
	}

	// Rekonsiliasi: paid_amount rekonstruksi == paid_amount aktual per cicilan.
	for _, s := range schedules {
		if !reconstructed[s.ID].Equal(s.PaidAmount) {
			plan.reconciled = false
			plan.reason = "paid_amount mismatch"
			plan.detail = mismatchDetail(schedules, reconstructed)
			return plan
		}
	}
	plan.reconciled = true
	return plan
}

// mismatchDetail merangkai ringkasan selisih rekonstruksi vs aktual (untuk review).
func mismatchDetail(schedules []*PaymentSchedule, reconstructed map[uint64]domain.Money) string {
	detail := ""
	for _, s := range schedules {
		r := reconstructed[s.ID]
		if !r.Equal(s.PaidAmount) {
			if detail != "" {
				detail += "; "
			}
			detail += "cicilan #" + strconv.Itoa(s.InstallmentNumber) +
				": rekonstruksi=" + r.String() + " aktual=" + s.PaidAmount.String()
		}
	}
	return detail
}

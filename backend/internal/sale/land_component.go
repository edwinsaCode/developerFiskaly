package sale

// ── Kelebihan Tanah sebagai piutang TERPISAH dari harga unit ──────────────────
//
// Audit kwitansi (2026-10-29): kasus klien harga unit 185jt + Kelebihan Tanah
// 45jt, DP 6jt → sisa harga unit 179jt. Pembayaran Kelebihan Tanah 45jt
// SEBELUM Akad membuat sisa harga unit turun ke 134jt — padahal seharusnya
// tetap 179jt dan sisa Kelebihan Tanah menjadi 0.
//
// Root cause: jadwal piutang tanah (payment_schedules type=land) baru lahir
// saat Akad. Sebelum itu kind=land_excess hanya label — ReceivePayment
// mewaterfall uangnya ke cicilan rumah dan menandainya
// counts_toward_price=TRUE, sehingga mengurangi harga unit.
//
// Aturan sekarang: penerimaan land_excess TIDAK PERNAH mengurangi harga unit.
//   - Pasca-Akad: ditarget otomatis ke jadwal tanah (planForSchedule).
//   - Pra-Akad: dicatat sebagai uang muka tanah (LandEarmark) —
//     counts_toward_price=FALSE, saldo kredit di Uang Muka 2-2000 (kewajiban,
//     Invariant #7), lalu dinetkan ke piutang tanah saat Akad (landAdvance).

import (
	"context"
	"fmt"

	"esaproperti/internal/domain"
)

// projectedLandAmount adalah tagihan Kelebihan Tanah yang dibekukan di kontrak
// (DPP + PPN bila PKP). Rumus IDENTIK internal/land/akad.go. ok=false bila
// kontrak tidak membawa komponen tanah.
func projectedLandAmount(c *SaleContract) (domain.Money, bool) {
	if c.LandQuantityM2 == nil || !c.LandQuantityM2.IsPositive() || c.LandUnitPriceSnapshot == nil {
		return domain.Zero, false
	}
	landDPP := domain.FromDecimal(c.LandUnitPriceSnapshot.Decimal().Mul(*c.LandQuantityM2).Round(0))
	landVAT := domain.Zero
	if c.IsPKP {
		landVAT = domain.FromDecimal(landDPP.Decimal().Mul(c.VATRateSnapshot).Round(0))
	}
	return landDPP.Add(landVAT), true
}

// liveLandSchedule mengembalikan jadwal piutang tanah yang masih hidup
// (bukan superseded), atau nil bila belum Akad.
func liveLandSchedule(schedules []*PaymentSchedule) *PaymentSchedule {
	for _, sch := range schedules {
		if sch.Type == ScheduleTypeLand && sch.Status != ScheduleStatusSuperseded {
			return sch
		}
	}
	return nil
}

// landOnlyPaid menjumlah termin yang SELURUHNYA untuk Kelebihan Tanah
// (kind=land_excess, counts_toward_price=FALSE) — baik uang muka tanah
// pra-Akad maupun pembayaran yang ditarget ke jadwal tanah pasca-Akad.
// Termin campuran lama (counts_toward_price=TRUE) sudah terhitung di
// SumTerminsByUnit, jadi tidak dihitung ganda di sini.
func (s *Service) landOnlyPaid(ctx context.Context, tenantID, unitID uint64) (domain.Money, error) {
	termins, err := s.store.ListTerminsByUnit(ctx, tenantID, unitID)
	if err != nil {
		return domain.Zero, fmt.Errorf("baca termin Kelebihan Tanah: %w", err)
	}
	total := domain.Zero
	for _, tp := range termins {
		if tp.Kind == TerminKindLandExcess && !tp.CountsTowardPrice {
			total = total.Add(tp.Amount)
		}
	}
	return total, nil
}

// resolveLandPayment menentukan ke mana penerimaan land_excess tanpa target
// jadwal harus diarahkan. Mengembalikan jadwal tanah (pasca-Akad) ATAU
// earmark=true (pra-Akad). Menolak kontrak tanpa komponen tanah dan nominal
// yang melebihi sisa tagihan tanah pra-Akad (agar Akad tidak gagal dengan
// ErrAdvanceExceedsSalePrice belakangan).
func (s *Service) resolveLandPayment(ctx context.Context, tenantID uint64, c *SaleContract, amount domain.Money) (*PaymentSchedule, bool, error) {
	schedules, err := s.contracts.ListSchedulesByContract(ctx, tenantID, c.ID)
	if err != nil {
		return nil, false, fmt.Errorf("baca jadwal kontrak: %w", err)
	}
	if sch := liveLandSchedule(schedules); sch != nil {
		return sch, false, nil
	}
	// LandStockID wajib: tanpa itu RecordAkad tidak menyiapkan Akad tanah,
	// jadi uang muka tanah tidak akan pernah dinetkan (yatim di Uang Muka).
	landAmount, ok := projectedLandAmount(c)
	if !ok || c.LandStockID == nil {
		return nil, false, ErrLandComponentMissing
	}
	paid, err := s.landOnlyPaid(ctx, tenantID, c.UnitID)
	if err != nil {
		return nil, false, err
	}
	if amount.GreaterThan(landAmount.Sub(paid)) {
		return nil, false, ErrLandPaymentExceedsOutstanding
	}
	return nil, true, nil
}

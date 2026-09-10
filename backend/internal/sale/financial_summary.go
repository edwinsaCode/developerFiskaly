package sale

// Hardening Receipt/Invoice/Outstanding — SATU rumus finansial kontrak untuk
// SEMUA konsumen (receipt, invoice, dashboard, collection). Ledger tetap SoT
// untuk jurnal; ringkasan ini adalah agregasi dokumen pembayaran yang SUDAH
// diakui sistem (termin_payments — setiap barisnya berpasangan dengan jurnal
// posted via ReceivePayment). Read-only, tidak menulis apa pun.
//
// Definisi (LOCKED):
//   HargaUnit     = unit_price_snapshot (dibekukan saat kontrak dibuat; kontrak
//                   lama tanpa snapshot → fallback DPP, ditandai PriceIsSnapshot=false)
//   Diskon        = HargaUnit − DPP, minimum 0 (derived — TIDAK disimpan,
//                   no duplicate SoT)
//   NilaiKontrak  = GrossAmount (DPP + PPN utk PKP) — jumlah yang DITAGIH buyer
//   TotalDibayar  = Σ termin_payments unit (SEMUA source: booking_fee yang
//                   dikonversi, DP, cicilan, collection, kpr_disbursement)
//   Terutang      = NilaiKontrak − TotalDibayar (bisa negatif = kelebihan bayar)
//
// Seam KPR (Task 4): pencairan bank = termin ber-source kpr_disbursement +
// financing_source_id (konstanta sudah ada di model.go). Saat Increment KPR
// Realization mencatat pencairan via ReceivePayment, TotalDibayar & Terutang
// di sini — dan semua konsumen — otomatis benar TANPA perubahan lain.

import (
	"context"

	"esaproperti/internal/domain"
)

// ContractFinancialSummary adalah ringkasan keuangan satu kontrak.
type ContractFinancialSummary struct {
	ContractID uint64       `json:"contract_id"`
	UnitID     uint64       `json:"unit_id"`
	UnitPrice  domain.Money `json:"unit_price"`
	// PriceIsSnapshot: true bila UnitPrice berasal dari snapshot beku saat
	// kontrak dibuat; false untuk kontrak lama (fallback DPP, diskon 0).
	PriceIsSnapshot bool         `json:"price_is_snapshot"`
	Discount        domain.Money `json:"discount"`
	DPP             domain.Money `json:"dpp"`
	NetContract     domain.Money `json:"net_contract"` // GrossAmount — nilai kontrak ditagih
	TotalPaid       domain.Money `json:"total_paid"`
	Outstanding     domain.Money `json:"outstanding"` // NetContract − TotalPaid

	// HasLand/LandAmount: komponen Kelebihan Tanah (opsional), dibekukan di
	// kontrak yang sama & saat yang sama dengan NetContract (CreateContract),
	// jauh SEBELUM Akad — sehingga bisa dilacak (ditagih) sedini kontrak
	// dikonversi, bukan menunggu Akad. Rumus IDENTIK internal/land/akad.go
	// (dpp = harga/m2 × luas, vat = dpp × tarif bila PKP, gross = dpp+vat) agar
	// proyeksi ini konsisten dengan jurnal Akad yang sesungguhnya nanti.
	//
	// LandAmount TIDAK digabung ke NetContract/Outstanding (keduanya LOCKED —
	// house-only, dipakai validateScheduleSum/KPR/PortfolioFinancials).
	// TotalContractValue/TotalOutstanding adalah field TAMBAHAN yang
	// menggabungkan rumah+tanah terhadap SATU kolam pembayaran yang sama
	// (house & land berbagi akun 2-2000 Uang Muka pra-Akad) — inilah angka
	// "terutang" yang sesungguhnya harus ditampilkan ke user bila kontrak
	// punya komponen tanah, karena TotalPaid sudah mencakup pembayaran utk
	// keduanya.
	HasLand            bool         `json:"has_land"`
	LandAmount         domain.Money `json:"land_amount"`          // proyeksi tagihan tanah (DPP+PPN)
	TotalContractValue domain.Money `json:"total_contract_value"` // NetContract + LandAmount
	TotalOutstanding   domain.Money `json:"total_outstanding"`    // TotalContractValue − TotalPaid

	// TotalOutstandingActual: berbeda dari TotalOutstanding di atas (proyeksi
	// pra-Akad dari snapshot kontrak) — field ini memakai nilai tagihan tanah
	// SESUNGGUHNYA dari baris payment_schedules (type=land, land_sale_id,
	// migrasi 000095; superseded/dibatalkan diabaikan), anchor AR formal yang
	// sama dipakai waterfall ReceivePayment, lalu dikurangi TotalPaid SEKALI
	// (sama seperti TotalOutstanding — BUKAN Outstanding+sisa-tanah-terpisah:
	// TotalPaid adalah satu kolam gabungan rumah+tanah, jadi menjumlah dua
	// sisa yang masing-masing sudah mengurangkan kolam yang sama akan
	// menghitung ganda begitu waterfall meluber dari rumah ke tanah).
	// Sebelum Akad, tidak ada baris payment_schedules type=land sama sekali →
	// nilainya sama dengan Outstanding (rumah saja, land belum ditagih formal;
	// pakai TotalOutstanding/LandAmount di tempat lain untuk proyeksi pra-Akad).
	// Inilah angka yang benar untuk pratinjau/prefill nominal pembayaran
	// pasca-Akad (Riwayat Penerimaan).
	TotalOutstandingActual domain.Money `json:"total_outstanding_actual"`

	// TotalPaidActual: pasangan TotalOutstandingActual — total kas SESUNGGUHNYA
	// diterima terhadap kolam gabungan rumah+tanah (TotalContractValueActual −
	// TotalOutstandingActual), BUKAN Σ TotalPaid (yang memfilter
	// counts_toward_price=TRUE dan karenanya diam-diam MENGECUALIKAN pembayaran
	// yang 100% meluber ke baris tanah — pembayaran itu sengaja ditandai FALSE
	// oleh planAllocationsLocked, lihat CountsTowardPrice, supaya tidak
	// ganda-hitung di Outstanding house-only). Diturunkan dari sisi outstanding
	// yang sudah benar (schedule-based, sama seperti outstandingForContract)
	// supaya TotalPaidActual + TotalOutstandingActual == TotalContractValueActual
	// SELALU, tanpa pengecualian — konsisten dgn Jadwal Pembayaran yg ditampilkan.
	TotalPaidActual domain.Money `json:"total_paid_actual"`
}

// ── Agregat portfolio (S3/R4 — reporting membaca ini, bukan SQL sendiri) ─────

// UnitProjectResolver memetakan unit → project untuk agregasi per proyek.
// Diimplementasi GORMRepository; dipasang implisit (type assertion pada
// ContractStore) agar mock lama tetap kompatibel.
type UnitProjectResolver interface {
	UnitProjectIDs(ctx context.Context, tenantID uint64, unitIDs []uint64) (map[uint64]uint64, error)
}

// PortfolioFinancials mengembalikan ringkasan keuangan SELURUH kontrak aktif
// dari rumus kanonik (summarizeContract → SumTerminsByUnit). SATU-SATUNYA
// sumber agregat "contract value / collected / outstanding" untuk dashboard,
// sales performance, dan pipeline — konsumen hanya menjumlah baris ini.
func (s *Service) PortfolioFinancials(ctx context.Context, tenantID uint64) ([]domain.ContractPortfolioRow, error) {
	if err := s.requireContractStore(); err != nil {
		return nil, err
	}
	contracts, err := s.contracts.ListActiveContracts(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	unitIDs := make([]uint64, 0, len(contracts))
	for _, c := range contracts {
		unitIDs = append(unitIDs, c.UnitID)
	}
	projects := map[uint64]uint64{}
	if r, ok := s.contracts.(UnitProjectResolver); ok && len(unitIDs) > 0 {
		if projects, err = r.UnitProjectIDs(ctx, tenantID, unitIDs); err != nil {
			return nil, err
		}
	}
	rows := make([]domain.ContractPortfolioRow, 0, len(contracts))
	for _, c := range contracts {
		sum, err := s.summarizeContract(ctx, tenantID, c)
		if err != nil {
			return nil, err
		}
		rows = append(rows, domain.ContractPortfolioRow{
			ContractID:             c.ID,
			UnitID:                 c.UnitID,
			ProjectID:              projects[c.UnitID],
			SalesPersonID:          c.SalesPersonID,
			AdminMarketingPersonID: c.AdminMarketingPersonID,
			NetContract:            sum.NetContract,
			TotalPaid:              sum.TotalPaid,
			Outstanding:            sum.Outstanding,
		})
	}
	return rows, nil
}

// PortfolioOutstanding = Σ Outstanding (hanya positif) seluruh kontrak aktif.
// Derivasi PortfolioFinancials — menggantikan `SUM(gross_amount − Σtermin)`
// raw SQL di dashboard receivables.
func (s *Service) PortfolioOutstanding(ctx context.Context, tenantID uint64) (domain.Money, error) {
	rows, err := s.PortfolioFinancials(ctx, tenantID)
	if err != nil {
		return domain.Zero, err
	}
	total := domain.Zero
	for _, r := range rows {
		if r.Outstanding.Decimal().IsPositive() {
			total = total.Add(r.Outstanding)
		}
	}
	return total, nil
}

// ContractOutstandingPaid mengembalikan (outstanding, total_paid) satu kontrak
// dari rumus kanonik — dipakai KPR pipeline agar tak menghitung sendiri.
func (s *Service) ContractOutstandingPaid(ctx context.Context, tenantID, contractID uint64) (domain.Money, domain.Money, error) {
	sum, err := s.ContractFinancialSummaryByID(ctx, tenantID, contractID)
	if err != nil {
		return domain.Zero, domain.Zero, err
	}
	return sum.Outstanding, sum.TotalPaid, nil
}

// ContractFinancialSummaryByID menghitung ringkasan dari kontrak + Σ termin.
func (s *Service) ContractFinancialSummaryByID(ctx context.Context, tenantID, contractID uint64) (*ContractFinancialSummary, error) {
	if err := s.requireContractStore(); err != nil {
		return nil, err
	}
	c, err := s.contracts.FindContractByID(ctx, tenantID, contractID)
	if err != nil {
		return nil, err
	}
	return s.summarizeContract(ctx, tenantID, c)
}

// ContractFinancialSummaryByUnit menghitung ringkasan untuk kontrak aktif unit.
func (s *Service) ContractFinancialSummaryByUnit(ctx context.Context, tenantID, unitID uint64) (*ContractFinancialSummary, error) {
	if err := s.requireContractStore(); err != nil {
		return nil, err
	}
	c, err := s.contracts.FindContractByUnitID(ctx, tenantID, unitID)
	if err != nil {
		return nil, err
	}
	return s.summarizeContract(ctx, tenantID, c)
}

func (s *Service) summarizeContract(ctx context.Context, tenantID uint64, c *SaleContract) (*ContractFinancialSummary, error) {
	paid, err := s.store.SumTerminsByUnit(ctx, tenantID, c.UnitID)
	if err != nil {
		return nil, err
	}

	sum := &ContractFinancialSummary{
		ContractID:  c.ID,
		UnitID:      c.UnitID,
		DPP:         c.DPPAmount,
		NetContract: c.GrossAmount,
		TotalPaid:   paid,
		Outstanding: c.GrossAmount.Sub(paid),
	}

	landAmount := domain.Zero
	if c.LandQuantityM2 != nil && c.LandQuantityM2.IsPositive() && c.LandUnitPriceSnapshot != nil {
		landDPP := domain.FromDecimal(c.LandUnitPriceSnapshot.Decimal().Mul(*c.LandQuantityM2).Round(0))
		landVAT := domain.Zero
		if c.IsPKP {
			landVAT = domain.FromDecimal(landDPP.Decimal().Mul(c.VATRateSnapshot).Round(0))
		}
		landAmount = landDPP.Add(landVAT)
		sum.HasLand = true
	}
	sum.LandAmount = landAmount
	sum.TotalContractValue = c.GrossAmount.Add(landAmount)
	sum.TotalOutstanding = sum.TotalContractValue.Sub(paid)

	// landAmountActual: Σ nominal TAGIHAN (bukan sisa) baris payment_schedules
	// type=land yang masih hidup (superseded/dibatalkan diabaikan) — dipakai
	// utk TotalContractValueActual (dasar TotalPaidActual di bawah).
	//
	// TotalOutstandingActual TIDAK LAGI dihitung dgn "GrossAmount+landAmountActual
	// − paid" (bug UAT 2026-09, sama persis dgn yang sudah diperbaiki di
	// outstandingForContract/collection.go): `paid` (SumTerminsByUnit) memfilter
	// counts_toward_price=TRUE, dan pembayaran yang 100% meluber ke baris tanah
	// ditandai FALSE oleh planAllocationsLocked — jadi ikut terkecualikan dari
	// "paid" di sini juga, membuat outstanding gabungan tampak masih tersisa
	// padahal Jadwal Pembayaran (payment_schedules.paid_amount, tidak peduli
	// counts_toward_price) sudah menunjukkan lunas. Delegasikan ke
	// outstandingForContract — SATU rumus dgn pratinjau pembayaran (collection.go)
	// — bukan menghitung ulang dgn asumsi yang sudah terbukti salah.
	landAmountActual := domain.Zero
	actualOutstanding := sum.Outstanding
	if s.contracts != nil {
		schedules, err := s.contracts.ListSchedulesByContract(ctx, tenantID, c.ID)
		if err != nil {
			return nil, err
		}
		for _, sched := range schedules {
			if sched.Type != ScheduleTypeLand || sched.Status == ScheduleStatusSuperseded {
				continue
			}
			landAmountActual = landAmountActual.Add(sched.Amount)
		}
		ao, err := s.outstandingForContract(ctx, tenantID, c)
		if err != nil {
			return nil, err
		}
		actualOutstanding = ao
	}
	sum.TotalOutstandingActual = actualOutstanding
	sum.TotalPaidActual = c.GrossAmount.Add(landAmountActual).Sub(actualOutstanding)

	if c.UnitPriceSnapshot != nil && !c.UnitPriceSnapshot.IsZero() {
		sum.UnitPrice = *c.UnitPriceSnapshot
		sum.PriceIsSnapshot = true
		if d := sum.UnitPrice.Sub(c.DPPAmount); !d.IsNeg() {
			sum.Discount = d
		} // snapshot < DPP (markup) → diskon 0, harga tetap apa adanya
	} else {
		// Kontrak lama tanpa snapshot: jujur — harga = DPP, diskon 0.
		sum.UnitPrice = c.DPPAmount
	}
	return sum, nil
}

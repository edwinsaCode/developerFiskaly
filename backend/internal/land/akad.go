package land

// LT-5 (kelebihan-tanah-final-architecture §B.3, §E.4, §F) — Akad: pengakuan
// pendapatan + HPP satu penjualan Kelebihan Tanah, atomik. Pola identik
// sale.Service.RecordAkad: validasi + resolusi HPP/akun/jurnal di sini
// (business logic, lewat seam Store — tidak ada akses DB langsung), penulisan
// atomik (row-lock + posting + persist) didelegasikan ke Store.RecordAkad.

import (
	"context"
	"time"

	"github.com/shopspring/decimal"

	"esaproperti/internal/domain"
)

// Akun COA tetap (bukan dari Product Catalog — Kelebihan Tanah bukan produk
// unit properti, §B.3). PPN keluaran 2-3000 (bukan 2-1300 seperti draf desain
// awal — dikoreksi mengikuti konvensi akun aktual internal/sale).
const (
	landRevenueAccountCode   = "4-1100"
	landPPNKeluarAccountCode = "2-3000"
	landHPPAccountCode       = "5-1000"
	landInventoryAccountCode = "1-3000"
)

// RecordAkadRequest is the input for recording Akad (pengakuan pendapatan +
// HPP) atas satu penjualan Kelebihan Tanah — analog Event 3+4 sale_records,
// tapi berdiri sendiri, bukan sale_contracts.unit_id (§B.3).
type RecordAkadRequest struct {
	ProjectID          uint64
	ReservationID      *uint64
	CustomerID         uint64
	SalesPersonID      *uint64
	QuantityM2         decimal.Decimal
	UnitPriceSnapshot  domain.Money // opsional; default reservasi/pool bila nol
	DPPAmount          domain.Money
	IsPKP              bool
	VATRateSnapshot    decimal.Decimal
	PaymentAccountCode string
	RecognitionDate    time.Time
	CreatedBy          *uint64
}

// RecordAkad mencatat Akad Kelebihan Tanah: validasi request, resolusi tarif
// HPP (finalized→budgeted→actual via LandHPPResolver), resolusi akun COA,
// bangun jurnal pendapatan+HPP, lalu delegasikan penulisan atomik (row-lock
// land_stock + optional tutup reservasi + posting jurnal + simpan
// land_sales/land_allocations, satu transaksi) ke Store.RecordAkad.
func (s *Service) RecordAkad(ctx context.Context, tenantID uint64, req RecordAkadRequest) (*LandSale, error) {
	if !req.QuantityM2.IsPositive() {
		return nil, ErrQuantityMustBePositive
	}
	if req.CustomerID == 0 {
		return nil, ErrCustomerRequired
	}
	if req.DPPAmount.IsNeg() {
		return nil, ErrQuantityNegative
	}
	if req.IsPKP && req.VATRateSnapshot.LessThanOrEqual(decimal.Zero) {
		return nil, ErrVATRateRequired
	}
	if req.PaymentAccountCode == "" {
		return nil, ErrPaymentAccountRequired
	}
	if req.RecognitionDate.IsZero() {
		return nil, ErrRecognitionDateRequired
	}
	if s.hppResolver == nil {
		return nil, ErrHPPResolverNotConfigured
	}
	if err := s.store.ValidateCashBankAccount(ctx, tenantID, req.PaymentAccountCode); err != nil {
		return nil, err
	}

	pool, err := s.store.FindPoolByProject(ctx, tenantID, req.ProjectID)
	if err != nil {
		return nil, err
	}

	unitPrice := req.UnitPriceSnapshot
	var reservation *LandStockReservation
	if req.ReservationID != nil {
		reservation, err = s.store.FindReservation(ctx, tenantID, *req.ReservationID)
		if err != nil {
			return nil, err
		}
		if reservation.Status != ReservationStatusActive {
			return nil, ErrReservationNotActive
		}
		if reservation.CustomerID != req.CustomerID {
			return nil, ErrReservationCustomerMismatch
		}
		if !reservation.QuantityM2.Equal(req.QuantityM2) {
			return nil, ErrReservationQuantityMismatch
		}
		if unitPrice.IsZero() {
			unitPrice = reservation.UnitPriceSnapshot
		}
	} else if req.QuantityM2.GreaterThan(pool.AvailableQuantityM2()) {
		return nil, ErrCapacityExceeded
	}
	if unitPrice.IsZero() {
		unitPrice = pool.UnitPrice
	}

	// ── PPN (rumus tunggal, identik sale.vatAmountOf) ──────────────────────
	vat := domain.Zero
	if req.IsPKP {
		vat = domain.FromDecimal(req.DPPAmount.Decimal().Mul(req.VATRateSnapshot).Round(0))
	}
	gross := req.DPPAmount.Add(vat)

	// ── HPP: rantai finalized→budgeted→actual ──────────────────────────────
	hppRes, err := s.hppResolver.ResolveLandHPPRate(ctx, tenantID, req.ProjectID)
	if err != nil {
		return nil, err
	}
	hppTotal := domain.FromDecimal(hppRes.RatePerM2.Decimal().Mul(req.QuantityM2))
	basis := hppRes.Basis
	if basis == "" {
		basis = "land_area"
	}

	// ── Resolve akun + bangun jurnal ────────────────────────────────────────
	acc, err := s.resolveLandAccounts(ctx, tenantID, req.IsPKP, req.PaymentAccountCode)
	if err != nil {
		return nil, err
	}
	revenueLines := buildLandRevenueLines(acc, req.ProjectID, req.DPPAmount, vat, gross)
	var cogsLines []JournalLineInput
	if !hppTotal.IsZero() {
		cogsLines = buildLandCOGSLines(acc, req.ProjectID, hppTotal)
	}

	return s.store.RecordAkad(ctx, tenantID, RecordAkadParams{
		ProjectID:                 req.ProjectID,
		LandStockID:               pool.ID,
		ReservationID:             req.ReservationID,
		CustomerID:                req.CustomerID,
		SalesPersonID:             req.SalesPersonID,
		QuantityM2:                req.QuantityM2,
		UnitPriceSnapshot:         unitPrice,
		DPPAmount:                 req.DPPAmount,
		IsPKP:                     req.IsPKP,
		VATRateSnapshot:           req.VATRateSnapshot,
		GrossAmount:               gross,
		PaymentAccountCode:        req.PaymentAccountCode,
		RecognitionDate:           req.RecognitionDate,
		CreatedBy:                 req.CreatedBy,
		RevenueLines:              revenueLines,
		COGSLines:                 cogsLines,
		HPPRatePerM2:              hppRes.RatePerM2,
		HPPTotal:                  hppTotal,
		Basis:                     basis,
		BudgetPlanID:              hppRes.BudgetPlanID,
		BudgetPlanVersion:         hppRes.BudgetPlanVersion,
		AllocationConfigVersionID: hppRes.ConfigVersionID,
		AllocationConfigVersion:   hppRes.ConfigVersion,
	})
}

// GetLandSale returns a single land_sale, or ErrLandSaleNotFound.
func (s *Service) GetLandSale(ctx context.Context, tenantID, id uint64) (*LandSale, error) {
	return s.store.FindLandSale(ctx, tenantID, id)
}

// ListLandSales returns all land_sales for a project, newest-recognized first.
func (s *Service) ListLandSales(ctx context.Context, tenantID, projectID uint64) ([]LandSale, error) {
	return s.store.ListLandSalesByProject(ctx, tenantID, projectID)
}

// CancelLandSaleRequest is the input for cancelling a post-Akad land_sale
// (§F.3): reverses the revenue+HPP journals, returns the quantity to the
// pool's available balance, and marks the sale cancelled.
type CancelLandSaleRequest struct {
	Reason      string
	CancelledBy *uint64
	CancelDate  time.Time
}

// CancelLandSale membatalkan satu land_sale berstatus akad — perluasan
// minimal atas RecordAkad (§F.3), BUKAN reuse internal/cancellation:
// land_sales adalah satu aksi atomik tanpa approval/PPh/refund terpisah
// (Event 3 land men-debit Kas/Bank langsung, jadi jurnal pembalik pendapatan
// itu sendiri SUDAH menjadi "pengembalian" — tidak ada Uang Muka/Hutang
// Refund seperti pembatalan unit properti pasca-BAST).
func (s *Service) CancelLandSale(ctx context.Context, tenantID, id uint64, req CancelLandSaleRequest) (*LandSale, error) {
	if req.Reason == "" {
		return nil, ErrCancelReasonRequired
	}
	cancelDate := req.CancelDate
	if cancelDate.IsZero() {
		cancelDate = time.Now()
	}
	return s.store.CancelLandSale(ctx, tenantID, id, CancelLandSaleParams{
		Reason:      req.Reason,
		CancelledBy: req.CancelledBy,
		CancelDate:  cancelDate,
	})
}

// ── helpers ──────────────────────────────────────────────────────────────

type landResolvedAccounts struct {
	payment   uint64
	revenue   uint64
	ppnKeluar uint64
	hpp       uint64
	land      uint64
}

func (s *Service) resolveLandAccounts(ctx context.Context, tenantID uint64, needPPN bool, paymentCode string) (landResolvedAccounts, error) {
	ra, err := s.resolveLandRevenueAccounts(ctx, tenantID, needPPN)
	if err != nil {
		return ra, err
	}
	ra.payment, err = s.store.FindAccountIDByCode(ctx, tenantID, paymentCode)
	if err != nil {
		return ra, err
	}
	return ra, nil
}

// resolveLandRevenueAccounts resolves everything RecordAkad needs EXCEPT the
// debit ("payment") leg — used both by resolveLandAccounts (standalone
// tunai/lunas flow, ra.payment = a cash/bank account) and by
// PrepareBundledAkad (booking-embedded flow, ra.payment = the unit's own
// piutang/financing account, resolved by the caller).
func (s *Service) resolveLandRevenueAccounts(ctx context.Context, tenantID uint64, needPPN bool) (landResolvedAccounts, error) {
	var ra landResolvedAccounts
	var err error
	lookup := func(code string, dest *uint64) {
		if err != nil {
			return
		}
		*dest, err = s.store.FindAccountIDByCode(ctx, tenantID, code)
	}
	lookup(landRevenueAccountCode, &ra.revenue)
	if needPPN {
		lookup(landPPNKeluarAccountCode, &ra.ppnKeluar)
	}
	lookup(landHPPAccountCode, &ra.hpp)
	lookup(landInventoryAccountCode, &ra.land)
	return ra, err
}

// ── Bundled Akad (kelebihan-tanah-booking-integration-2026-08) ─────────────
//
// PrepareBundledAkadRequest/PrepareBundledAkad support the Booking-embedded
// Kelebihan Tanah flow: the land component no longer has its own cash/bank
// collection leg (v1 tunai/lunas, RecordAkad above) — it rides the SAME
// piutang/financing account already resolved for the unit's own Akad, so the
// buyer owes ONE receivable that already includes both house and land
// (§D3: land revenue+HPP posts as its own journal pair, but at the identical
// Akad timing and through the identical collection mechanism as the house —
// no second engine, no second cash leg).
type PrepareBundledAkadRequest struct {
	ProjectID     uint64
	ReservationID *uint64
	CustomerID    uint64
	SalesPersonID *uint64
	QuantityM2    decimal.Decimal
	// UnitPriceSnapshot: harga/m2 dibekukan saat reservasi/kontrak dibuat
	// (Booking.LandUnitPriceSnapshot / SaleContract.LandUnitPriceSnapshot).
	// Nol → fallback ke reservasi lalu pool (identik RecordAkad).
	UnitPriceSnapshot domain.Money
	IsPKP             bool
	VATRateSnapshot   decimal.Decimal
	// ReceivableAccountID/Code: akun piutang/financing yang SUDAH di-resolve
	// oleh sale.Service untuk Akad unit ini (mis. 1-2000, atau akun
	// pembiayaan KPR pasca-akad) — dipakai ULANG sebagai sisi debit jurnal
	// pendapatan tanah, bukan direct ke kas/bank.
	ReceivableAccountID   uint64
	ReceivableAccountCode string
	RecognitionDate       time.Time
	CreatedBy             *uint64
}

// PrepareBundledAkad resolves everything RecordAkad would (pool, reservation
// validation, HPP rate, akun) and builds the journal lines, but does NOT
// execute the atomic write itself — it returns RecordAkadParams for the
// caller (sale.Execute, internal/sale/repository.go) to pass into
// RecordAkadTx INSIDE ITS OWN open transaction, so the unit's Event3/4 and
// the land's revenue+HPP commit or roll back together, atomically, at Akad.
func (s *Service) PrepareBundledAkad(ctx context.Context, tenantID uint64, req PrepareBundledAkadRequest) (RecordAkadParams, error) {
	if !req.QuantityM2.IsPositive() {
		return RecordAkadParams{}, ErrQuantityMustBePositive
	}
	if req.CustomerID == 0 {
		return RecordAkadParams{}, ErrCustomerRequired
	}
	if req.ReceivableAccountID == 0 {
		return RecordAkadParams{}, ErrPaymentAccountRequired
	}
	if req.RecognitionDate.IsZero() {
		return RecordAkadParams{}, ErrRecognitionDateRequired
	}
	if s.hppResolver == nil {
		return RecordAkadParams{}, ErrHPPResolverNotConfigured
	}

	pool, err := s.store.FindPoolByProject(ctx, tenantID, req.ProjectID)
	if err != nil {
		return RecordAkadParams{}, err
	}

	unitPrice := req.UnitPriceSnapshot
	if req.ReservationID != nil {
		reservation, rerr := s.store.FindReservation(ctx, tenantID, *req.ReservationID)
		if rerr != nil {
			return RecordAkadParams{}, rerr
		}
		if reservation.Status != ReservationStatusActive {
			return RecordAkadParams{}, ErrReservationNotActive
		}
		if reservation.CustomerID != req.CustomerID {
			return RecordAkadParams{}, ErrReservationCustomerMismatch
		}
		if !reservation.QuantityM2.Equal(req.QuantityM2) {
			return RecordAkadParams{}, ErrReservationQuantityMismatch
		}
		if unitPrice.IsZero() {
			unitPrice = reservation.UnitPriceSnapshot
		}
	} else if req.QuantityM2.GreaterThan(pool.AvailableQuantityM2()) {
		return RecordAkadParams{}, ErrCapacityExceeded
	}
	if unitPrice.IsZero() {
		unitPrice = pool.UnitPrice
	}

	dpp := domain.FromDecimal(unitPrice.Decimal().Mul(req.QuantityM2).Round(0))
	vat := domain.Zero
	if req.IsPKP {
		vat = domain.FromDecimal(dpp.Decimal().Mul(req.VATRateSnapshot).Round(0))
	}
	gross := dpp.Add(vat)

	hppRes, err := s.hppResolver.ResolveLandHPPRate(ctx, tenantID, req.ProjectID)
	if err != nil {
		return RecordAkadParams{}, err
	}
	hppTotal := domain.FromDecimal(hppRes.RatePerM2.Decimal().Mul(req.QuantityM2))
	basis := hppRes.Basis
	if basis == "" {
		basis = "land_area"
	}

	acc, err := s.resolveLandRevenueAccounts(ctx, tenantID, req.IsPKP)
	if err != nil {
		return RecordAkadParams{}, err
	}
	acc.payment = req.ReceivableAccountID

	revenueLines := buildLandRevenueLines(acc, req.ProjectID, dpp, vat, gross)
	var cogsLines []JournalLineInput
	if !hppTotal.IsZero() {
		cogsLines = buildLandCOGSLines(acc, req.ProjectID, hppTotal)
	}

	return RecordAkadParams{
		ProjectID:                 req.ProjectID,
		LandStockID:               pool.ID,
		ReservationID:             req.ReservationID,
		CustomerID:                req.CustomerID,
		SalesPersonID:             req.SalesPersonID,
		QuantityM2:                req.QuantityM2,
		UnitPriceSnapshot:         unitPrice,
		DPPAmount:                 dpp,
		IsPKP:                     req.IsPKP,
		VATRateSnapshot:           req.VATRateSnapshot,
		GrossAmount:               gross,
		PaymentAccountCode:        req.ReceivableAccountCode,
		RecognitionDate:           req.RecognitionDate,
		CreatedBy:                 req.CreatedBy,
		RevenueLines:              revenueLines,
		COGSLines:                 cogsLines,
		HPPRatePerM2:              hppRes.RatePerM2,
		HPPTotal:                  hppTotal,
		Basis:                     basis,
		BudgetPlanID:              hppRes.BudgetPlanID,
		BudgetPlanVersion:         hppRes.BudgetPlanVersion,
		AllocationConfigVersionID: hppRes.ConfigVersionID,
		AllocationConfigVersion:   hppRes.ConfigVersion,
	}, nil
}

// buildLandRevenueLines: Dr akun pembayaran (bruto) / Cr Pendapatan (DPP) /
// Cr PPN Keluaran (jika PKP). v1 tunai/lunas saja — tidak ada piutang.
func buildLandRevenueLines(acc landResolvedAccounts, projectID uint64, dpp, ppn, gross domain.Money) []JournalLineInput {
	pid := projectID
	lines := []JournalLineInput{
		{
			AccountID: acc.payment, Debit: gross,
			ProjectID:   &pid,
			Description: "penerimaan Akad Kelebihan Tanah",
		},
		{
			AccountID: acc.revenue, Credit: dpp,
			ProjectID:   &pid,
			Description: "pendapatan Akad Kelebihan Tanah",
		},
	}
	if !ppn.IsZero() {
		lines = append(lines, JournalLineInput{
			AccountID: acc.ppnKeluar, Credit: ppn,
			ProjectID:   &pid,
			Description: "PPN keluaran Akad Kelebihan Tanah",
		})
	}
	return lines
}

// buildLandCOGSLines: Dr HPP / Cr Persediaan Tanah — hanya satu kategori
// (bukan land/hard/soft/financing seperti unit properti), karena yang dijual
// adalah tanah itu sendiri.
func buildLandCOGSLines(acc landResolvedAccounts, projectID uint64, hppTotal domain.Money) []JournalLineInput {
	pid := projectID
	return []JournalLineInput{
		{
			AccountID: acc.hpp, Debit: hppTotal,
			ProjectID:   &pid,
			Description: "HPP Akad Kelebihan Tanah",
		},
		{
			AccountID: acc.land, Credit: hppTotal,
			ProjectID:   &pid,
			Description: "kredit Persediaan Tanah saat Akad",
		},
	}
}

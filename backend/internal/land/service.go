package land

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/shopspring/decimal"

	"esaproperti/internal/domain"
	"esaproperti/internal/tax"
)

const defaultProductCode = "kelebihan_tanah"

// ReserveInput is the persistence-layer input for Store.Reserve — the
// row-locked atomic operation that checks availability, inserts the
// reservation, and increments land_stock.reserved_quantity_m2 in one
// transaction (INV-LAND-1, §J).
type ReserveInput struct {
	ProjectID     uint64
	CustomerID    uint64
	SalesPersonID *uint64
	QuantityM2    decimal.Decimal
	ReservedAt    time.Time
	ExpiryDate    *time.Time
	CreatedBy     *uint64
}

// JournalLineInput mirrors ledger.LineInput without importing the ledger
// package into service.go — pola identik internal/sale.
type JournalLineInput struct {
	AccountID   uint64
	Debit       domain.Money
	Credit      domain.Money
	ProjectID   *uint64
	Description string
}

// RecordAkadParams is the persistence-layer input for Store.RecordAkad — the
// row-locked atomic operation that validates+closes an optional reservation,
// increments land_stock.sold_quantity_m2, posts the revenue+HPP journals, and
// inserts land_sales+land_allocations, all in one transaction.
type RecordAkadParams struct {
	ProjectID          uint64
	LandStockID        uint64
	ReservationID      *uint64
	CustomerID         uint64
	SalesPersonID      *uint64
	QuantityM2         decimal.Decimal
	UnitPriceSnapshot  domain.Money
	DPPAmount          domain.Money
	IsPKP              bool
	VATRateSnapshot    decimal.Decimal
	GrossAmount        domain.Money
	PaymentAccountCode string
	RecognitionDate    time.Time
	CreatedBy          *uint64

	RevenueLines []JournalLineInput // Akad pengakuan pendapatan
	COGSLines    []JournalLineInput // Akad HPP; nil jika hpp_total = 0

	// PPhPlan: akrual PPh Final Pengalihan (Event 5a) yang harus diposting DALAM
	// transaksi Akad yang sama, pola identik unit/BAST (AccruePPhFinalInTx) —
	// bukan tax engine kedua, hanya konsumen lain dari internal/tax.AccrualPlan.
	// nil = tidak ada resolver PPh terpasang (mis. test yang tidak peduli pajak);
	// RecordAkadTx melewati seluruh logika PPh bila nil (backward-compatible).
	PPhPlan *tax.AccrualPlan

	HPPRatePerM2              domain.Money
	HPPTotal                  domain.Money
	Basis                     string
	BudgetPlanID              *uint64
	BudgetPlanVersion         *int
	AllocationConfigVersionID *uint64
	AllocationConfigVersion   *int
}

// CancelLandSaleParams is the persistence-layer input for Store.CancelLandSale
// — the row-locked atomic operation that reverses the revenue (and COGS, if
// any) journals, decrements land_stock.sold_quantity_m2, and marks land_sales
// cancelled (§F.3).
type CancelLandSaleParams struct {
	Reason      string
	CancelledBy *uint64
	CancelDate  time.Time
	// AllowPaidSchedule: izinkan pembatalan meski payment_schedules yang
	// ter-link (land_sale_id) sudah menerima pembayaran (PaidAmount > 0).
	// HANYA dipakai internal/cancellation (pembatalan unit bundled), yang
	// menghitung refund/settlement dana buyer secara utuh untuk seluruh
	// kontrak. Jalur standalone (Service.CancelLandSale, endpoint langsung)
	// membiarkan ini false — tidak punya mekanisme refund sendiri, jadi
	// membatalkan land_sale yang sudah dibayar lewat jalur itu akan
	// menyisakan kas yang diterima tanpa penyelesaian (lihat
	// ErrLandSaleHasReceivedPayment).
	AllowPaidSchedule bool
}

// Store is the persistence seam for land_stock/land_stock_reservations — kept
// small and mockable (pattern: internal/project's UnitStore) so Service is
// unit-testable without a database.
type Store interface {
	CreatePool(ctx context.Context, pool *LandStock) error
	FindPoolByProject(ctx context.Context, tenantID, projectID uint64) (*LandStock, error)
	UpdatePoolQuantityAndPrice(ctx context.Context, tenantID, id uint64, totalQuantityM2 decimal.Decimal, unitPrice, purchasePrice domain.Money) error

	// Reserve atomically checks availability and inserts a reservation while
	// incrementing land_stock.reserved_quantity_m2 — row-locked (§J), the sole
	// enforcer of INV-LAND-1 under concurrent reservations.
	Reserve(ctx context.Context, tenantID, landStockID uint64, in ReserveInput) (*LandStockReservation, error)
	FindReservation(ctx context.Context, tenantID, id uint64) (*LandStockReservation, error)
	ListReservationsByProject(ctx context.Context, tenantID, projectID uint64) ([]LandStockReservation, error)
	ListExpiredReservationIDs(ctx context.Context, tenantID uint64, asOf time.Time) ([]uint64, error)
	// CloseReservation atomically transitions an active reservation to a
	// terminal status and decrements land_stock.reserved_quantity_m2.
	CloseReservation(ctx context.Context, tenantID, id uint64, status ReservationStatus, reason string) (*LandStockReservation, error)

	// ── LT-5: Akad (land_sales) ──────────────────────────────────────────────

	// RecordAkad atomically row-locks land_stock (and the reservation, if any),
	// re-validates availability, posts the revenue+HPP journals, and inserts
	// land_sales+land_allocations — one transaction (pola identik sale.Execute).
	RecordAkad(ctx context.Context, tenantID uint64, in RecordAkadParams) (*LandSale, error)
	FindLandSale(ctx context.Context, tenantID, id uint64) (*LandSale, error)
	ListLandSalesByProject(ctx context.Context, tenantID, projectID uint64) ([]LandSale, error)
	// ListReceivableLandSales (receivable.go) — baris untuk AR aging.
	ListReceivableLandSales(ctx context.Context, tenantID uint64) ([]LandSaleReceivable, error)

	// ── LT-6: pembatalan pasca-Akad (§F.3) ──────────────────────────────────

	// CancelLandSale atomically row-locks land_sales (must be status=akad) and
	// land_stock, reverses the revenue journal (and COGS journal, if any) via
	// ledger.PostingService.Reverse(), decrements land_stock.sold_quantity_m2
	// (quantity returns to AVAILABLE, not RESERVED), and marks land_sales
	// cancelled — one transaction (pola identik RecordAkad).
	CancelLandSale(ctx context.Context, tenantID, id uint64, in CancelLandSaleParams) (*LandSale, error)

	// FindAccountIDByCode resolves a COA account code to its id (tenant-scoped).
	FindAccountIDByCode(ctx context.Context, tenantID uint64, code string) (uint64, error)
	// ValidateCashBankAccount validates a payment_account_code is an active
	// cash/bank asset account — pola identik sale.ValidateCashBankAccount.
	ValidateCashBankAccount(ctx context.Context, tenantID uint64, code string) error
}

// Service is the business-logic layer for the Kelebihan Tanah stock pool.
// LT-3/LT-4 scope: pool + reservation. LT-5 adds Akad (revenue+HPP
// recognition), gated behind an injected LandHPPResolver so existing
// single-arg call sites (tests, pre-LT-5 wiring) keep compiling.
type Service struct {
	store       Store
	hppResolver LandHPPResolver
	pph         LandPPhResolver
}

// LandPPhResolver resolves the PPh Final Pengalihan accrual plan (rate, rule,
// formula, akun) for a Kelebihan Tanah Akad — satisfied directly by
// *tax.Service (method signature matches exactly, no adapter needed). Mirrors
// the same seam sale.PPhFinalAccruer uses for unit/BAST, so both flows post
// the IDENTICAL accounting treatment through the ONE tax engine.
type LandPPhResolver interface {
	ResolveAccrualPlan(ctx context.Context, tenantID uint64, req tax.AccrueTaxRequest) (*tax.AccrualPlan, error)
}

// ServiceOption configures optional Service collaborators (LT-5+).
type ServiceOption func(*Service)

// WithHPPResolver wires the HPP rate resolver required by RecordAkad.
// Without it, RecordAkad fails closed with ErrHPPResolverNotConfigured.
func WithHPPResolver(r LandHPPResolver) ServiceOption {
	return func(s *Service) { s.hppResolver = r }
}

// WithPPhResolver wires automatic PPh Final Pengalihan accrual into Akad —
// pola identik sale.SetTaxAccruer: OPSIONAL, bukan fail-closed. nil (default)
// = Akad tidak mengakru PPh (perilaku lama, dipertahankan untuk test yang
// tidak peduli pajak); terpasang = setiap Akad Kelebihan Tanah otomatis
// mengakru PPh Final dalam transaksi yang sama, tanpa campur tangan user.
func WithPPhResolver(r LandPPhResolver) ServiceOption {
	return func(s *Service) { s.pph = r }
}

func NewService(store Store, opts ...ServiceOption) *Service {
	s := &Service{store: store}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// CreatePoolRequest is the input for creating a project's land_stock pool.
type CreatePoolRequest struct {
	ProjectID       uint64
	TotalQuantityM2 decimal.Decimal
	UnitPrice       domain.Money // Harga Jual — dipakai di reservasi/Akad/DPP.
	PurchasePrice   domain.Money // Harga Beli — tarif HPP per m² saat Akad (koreksi klien 2026-08-31).
}

// CreatePool creates the (single) land_stock pool for a project.
func (s *Service) CreatePool(ctx context.Context, tenantID uint64, req CreatePoolRequest) (*LandStock, error) {
	if req.TotalQuantityM2.IsNegative() || req.UnitPrice.IsNeg() || req.PurchasePrice.IsNeg() {
		return nil, ErrQuantityNegative
	}
	existing, err := s.store.FindPoolByProject(ctx, tenantID, req.ProjectID)
	if err != nil && err != ErrLandStockNotFound {
		return nil, err
	}
	if existing != nil {
		return nil, ErrLandStockAlreadyExists
	}

	pool := &LandStock{
		TenantID:        tenantID,
		ProjectID:       req.ProjectID,
		ProductCode:     defaultProductCode,
		TotalQuantityM2: req.TotalQuantityM2,
		UnitPrice:       req.UnitPrice,
		PurchasePrice:   req.PurchasePrice,
	}
	if err := s.store.CreatePool(ctx, pool); err != nil {
		return nil, err
	}
	return pool, nil
}

// GetPool returns the land_stock pool for a project, or ErrLandStockNotFound.
func (s *Service) GetPool(ctx context.Context, tenantID, projectID uint64) (*LandStock, error) {
	return s.store.FindPoolByProject(ctx, tenantID, projectID)
}

// UpdatePoolRequest is the input for correcting a pool's total quantity/price.
// Both fields are always supplied (full replace) — a pool has no other
// admin-editable fields at LT-3.
type UpdatePoolRequest struct {
	TotalQuantityM2 decimal.Decimal
	UnitPrice       domain.Money
	PurchasePrice   domain.Money
}

// UpdatePool corrects total_quantity_m2/unit_price/purchase_price. Rejects
// shrinking total below what's already reserved+sold (INV-LAND-1) — enforced
// here AND by the DB CHECK constraint (chk_land_stock_capacity) as a
// fail-closed backstop.
func (s *Service) UpdatePool(ctx context.Context, tenantID, projectID uint64, req UpdatePoolRequest) (*LandStock, error) {
	if req.TotalQuantityM2.IsNegative() || req.UnitPrice.IsNeg() || req.PurchasePrice.IsNeg() {
		return nil, ErrQuantityNegative
	}
	pool, err := s.store.FindPoolByProject(ctx, tenantID, projectID)
	if err != nil {
		return nil, err
	}
	committed := pool.ReservedQuantityM2.Add(pool.SoldQuantityM2)
	if req.TotalQuantityM2.LessThan(committed) {
		return nil, ErrCapacityExceeded
	}
	if err := s.store.UpdatePoolQuantityAndPrice(ctx, tenantID, pool.ID, req.TotalQuantityM2, req.UnitPrice, req.PurchasePrice); err != nil {
		return nil, err
	}
	pool.TotalQuantityM2 = req.TotalQuantityM2
	pool.UnitPrice = req.UnitPrice
	pool.PurchasePrice = req.PurchasePrice
	return pool, nil
}

// ── Reservasi (LT-4) ────────────────────────────────────────────────────────

// ReserveRequest is the input for reserving quantity out of a project's
// land_stock pool for a customer.
type ReserveRequest struct {
	ProjectID     uint64
	CustomerID    uint64
	SalesPersonID *uint64
	QuantityM2    decimal.Decimal
	ReservedAt    time.Time
	ExpiryDate    *time.Time
	CreatedBy     *uint64
}

// Reserve soft-locks QuantityM2 out of a project's available land_stock for a
// customer. Tanpa booking fee (keputusan klien 2026-08-20): tidak ada
// transaksi finansial, tidak ada jurnal — murni counter inventory.
// INV-LAND-1 ditegakkan atomik di Store.Reserve (row-locked), bukan di sini.
func (s *Service) Reserve(ctx context.Context, tenantID uint64, req ReserveRequest) (*LandStockReservation, error) {
	if !req.QuantityM2.IsPositive() {
		return nil, ErrQuantityMustBePositive
	}
	if req.CustomerID == 0 {
		return nil, ErrCustomerRequired
	}
	pool, err := s.store.FindPoolByProject(ctx, tenantID, req.ProjectID)
	if err != nil {
		return nil, err
	}
	return s.store.Reserve(ctx, tenantID, pool.ID, ReserveInput{
		ProjectID:     req.ProjectID,
		CustomerID:    req.CustomerID,
		SalesPersonID: req.SalesPersonID,
		QuantityM2:    req.QuantityM2,
		ReservedAt:    req.ReservedAt,
		ExpiryDate:    req.ExpiryDate,
		CreatedBy:     req.CreatedBy,
	})
}

// GetReservation returns a single reservation, or ErrReservationNotFound.
func (s *Service) GetReservation(ctx context.Context, tenantID, id uint64) (*LandStockReservation, error) {
	return s.store.FindReservation(ctx, tenantID, id)
}

// ListReservations returns all reservations (any status) for a project,
// newest-reserved first.
func (s *Service) ListReservations(ctx context.Context, tenantID, projectID uint64) ([]LandStockReservation, error) {
	return s.store.ListReservationsByProject(ctx, tenantID, projectID)
}

// CancelReservation transitions an active reservation to cancelled and
// returns its quantity to the pool's available balance.
func (s *Service) CancelReservation(ctx context.Context, tenantID, id uint64, reason string) (*LandStockReservation, error) {
	return s.store.CloseReservation(ctx, tenantID, id, ReservationStatusCancelled, reason)
}

// ExpireReservation transitions an active reservation to expired and returns
// its quantity to the pool's available balance.
func (s *Service) ExpireReservation(ctx context.Context, tenantID, id uint64) (*LandStockReservation, error) {
	return s.store.CloseReservation(ctx, tenantID, id, ReservationStatusExpired, "")
}

// MarkExpiredReservations sweeps all active reservations past expiry_date as
// of asOf — idempotent, one reservation per tx (pattern: sale.MarkExpiredBookings).
// A reservation that's converted/cancelled between listing and closing (race
// with a concurrent action) is skipped, not treated as an error.
func (s *Service) MarkExpiredReservations(ctx context.Context, tenantID uint64, asOf time.Time) (int, error) {
	ids, err := s.store.ListExpiredReservationIDs(ctx, tenantID, asOf)
	if err != nil {
		return 0, err
	}
	count := 0
	var firstErr error
	for _, id := range ids {
		if _, cerr := s.store.CloseReservation(ctx, tenantID, id, ReservationStatusExpired, "lewat masa berlaku reservasi"); cerr != nil {
			if errors.Is(cerr, ErrReservationNotActive) || errors.Is(cerr, ErrReservationNotFound) {
				continue
			}
			if firstErr == nil {
				firstErr = fmt.Errorf("reservasi %d: %w", id, cerr)
			}
			continue
		}
		count++
	}
	return count, firstErr
}

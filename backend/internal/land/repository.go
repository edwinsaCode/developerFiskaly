package land

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"esaproperti/internal/domain"
	"esaproperti/internal/ledger"
)

// GORMRepository implements Store against MySQL via GORM. Every query is
// scoped by tenant_id explicitly (CLAUDE.md invariant #6) — no relying on a
// global scope, since land_stock has no registered GORM plugin scope.
type GORMRepository struct {
	db *gorm.DB
}

func NewGORMRepository(db *gorm.DB) *GORMRepository {
	return &GORMRepository{db: db}
}

func (r *GORMRepository) CreatePool(ctx context.Context, pool *LandStock) error {
	if err := r.db.WithContext(ctx).Create(pool).Error; err != nil {
		if isDuplicateKeyError(err) {
			return ErrLandStockAlreadyExists
		}
		if isForeignKeyViolation(err) {
			return ErrProjectNotFound
		}
		return fmt.Errorf("CreatePool: %w", err)
	}
	return nil
}

func (r *GORMRepository) FindPoolByProject(ctx context.Context, tenantID, projectID uint64) (*LandStock, error) {
	var pool LandStock
	err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND project_id = ?", tenantID, projectID).
		First(&pool).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrLandStockNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("FindPoolByProject: %w", err)
	}
	return &pool, nil
}

// FindPoolByProjectTx resolves a project's land pool WITHOUT a lock, against
// a caller-supplied open transaction (kelebihan-tanah-booking-integration-2026-08)
// — used by sale.CreateBookingAtomic to find the pool ID before handing off
// to ReserveTx, which re-locks by ID immediately after (same prepare/execute
// split as PrepareBundledAkad/RecordAkadTx: a stale read here is harmless
// because ReserveTx re-validates availability under SELECT FOR UPDATE).
func FindPoolByProjectTx(ctx context.Context, tx *gorm.DB, tenantID, projectID uint64) (*LandStock, error) {
	var pool LandStock
	err := tx.WithContext(ctx).
		Where("tenant_id = ? AND project_id = ?", tenantID, projectID).
		First(&pool).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrLandStockNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("FindPoolByProjectTx: %w", err)
	}
	return &pool, nil
}

func (r *GORMRepository) UpdatePoolQuantityAndPrice(ctx context.Context, tenantID, id uint64, totalQuantityM2 decimal.Decimal, unitPrice domain.Money) error {
	res := r.db.WithContext(ctx).Model(&LandStock{}).
		Where("id = ? AND tenant_id = ?", id, tenantID).
		Updates(map[string]any{
			"total_quantity_m2": totalQuantityM2,
			"unit_price":        unitPrice,
		})
	if res.Error != nil {
		if isCheckConstraintViolation(res.Error) {
			return ErrCapacityExceeded
		}
		return fmt.Errorf("UpdatePoolQuantityAndPrice: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return ErrLandStockNotFound
	}
	return nil
}

// ── Reservasi (LT-4) ────────────────────────────────────────────────────────

// Reserve: row-locks land_stock, checks available >= quantity, inserts the
// reservation, and increments reserved_quantity_m2 — all in one transaction.
// The lock makes the availability check reliable under concurrency; the
// chk_land_stock_capacity CHECK constraint is the fail-closed backstop if the
// increment itself would still overshoot (belt-and-suspenders, §J).
func (r *GORMRepository) Reserve(ctx context.Context, tenantID, landStockID uint64, in ReserveInput) (*LandStockReservation, error) {
	var res *LandStockReservation
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var err error
		res, err = ReserveTx(ctx, tx, tenantID, landStockID, in)
		return err
	})
	if err != nil {
		return nil, err
	}
	return res, nil
}

// ReserveTx is the transaction-composable core of Reserve — callers that
// already hold an open *gorm.DB transaction (e.g. sale.CreateBookingAtomic)
// call this directly against their own tx instead of opening a nested one,
// so the reservation commits or rolls back atomically with the booking.
func ReserveTx(ctx context.Context, tx *gorm.DB, tenantID, landStockID uint64, in ReserveInput) (*LandStockReservation, error) {
	var pool LandStock
	if err := tx.WithContext(ctx).
		Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("id = ? AND tenant_id = ?", landStockID, tenantID).
		First(&pool).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrLandStockNotFound
		}
		return nil, fmt.Errorf("lock land_stock %d: %w", landStockID, err)
	}
	if in.QuantityM2.GreaterThan(pool.AvailableQuantityM2()) {
		return nil, ErrCapacityExceeded
	}

	res := LandStockReservation{
		TenantID:          tenantID,
		LandStockID:       landStockID,
		ProjectID:         in.ProjectID,
		CustomerID:        in.CustomerID,
		SalesPersonID:     in.SalesPersonID,
		QuantityM2:        in.QuantityM2,
		UnitPriceSnapshot: pool.UnitPrice,
		ReservedAt:        in.ReservedAt,
		ExpiryDate:        in.ExpiryDate,
		Status:            ReservationStatusActive,
		CreatedBy:         in.CreatedBy,
	}
	if err := tx.WithContext(ctx).Create(&res).Error; err != nil {
		if containsAny(err, "fk_land_reservations_customer") {
			return nil, ErrCustomerNotFound
		}
		return nil, fmt.Errorf("create reservation: %w", err)
	}

	upd := tx.WithContext(ctx).Model(&LandStock{}).
		Where("id = ? AND tenant_id = ?", landStockID, tenantID).
		Update("reserved_quantity_m2", gorm.Expr("reserved_quantity_m2 + ?", in.QuantityM2))
	if upd.Error != nil {
		if isCheckConstraintViolation(upd.Error) {
			return nil, ErrCapacityExceeded
		}
		return nil, fmt.Errorf("increment reserved_quantity_m2: %w", upd.Error)
	}
	return &res, nil
}

func (r *GORMRepository) FindReservation(ctx context.Context, tenantID, id uint64) (*LandStockReservation, error) {
	var res LandStockReservation
	err := r.db.WithContext(ctx).
		Where("id = ? AND tenant_id = ?", id, tenantID).
		First(&res).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrReservationNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("FindReservation: %w", err)
	}
	return &res, nil
}

func (r *GORMRepository) ListReservationsByProject(ctx context.Context, tenantID, projectID uint64) ([]LandStockReservation, error) {
	var list []LandStockReservation
	err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND project_id = ?", tenantID, projectID).
		Order("reserved_at DESC").
		Find(&list).Error
	if err != nil {
		return nil, fmt.Errorf("ListReservationsByProject: %w", err)
	}
	return list, nil
}

func (r *GORMRepository) ListExpiredReservationIDs(ctx context.Context, tenantID uint64, asOf time.Time) ([]uint64, error) {
	var ids []uint64
	err := r.db.WithContext(ctx).Model(&LandStockReservation{}).
		Where("tenant_id = ? AND status = ? AND expiry_date IS NOT NULL AND expiry_date <= ?", tenantID, ReservationStatusActive, asOf).
		Pluck("id", &ids).Error
	if err != nil {
		return nil, fmt.Errorf("ListExpiredReservationIDs: %w", err)
	}
	return ids, nil
}

// CloseReservation: row-locks the reservation, requires it's still active,
// then transitions it to a terminal status and decrements
// land_stock.reserved_quantity_m2 by its quantity — one transaction.
func (r *GORMRepository) CloseReservation(ctx context.Context, tenantID, id uint64, status ReservationStatus, reason string) (*LandStockReservation, error) {
	var res *LandStockReservation
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var err error
		res, err = CloseReservationTx(ctx, tx, tenantID, id, status, reason)
		return err
	})
	if err != nil {
		return nil, err
	}
	return res, nil
}

// CloseReservationTx is the transaction-composable core of CloseReservation
// — used by sale.CloseBookingAtomic to release a booking-embedded land
// reservation (cancel/expire) inside the booking's own transaction.
func CloseReservationTx(ctx context.Context, tx *gorm.DB, tenantID, id uint64, status ReservationStatus, reason string) (*LandStockReservation, error) {
	var res LandStockReservation
	if err := tx.WithContext(ctx).
		Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("id = ? AND tenant_id = ?", id, tenantID).
		First(&res).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrReservationNotFound
		}
		return nil, fmt.Errorf("lock reservation %d: %w", id, err)
	}
	if res.Status != ReservationStatusActive {
		return nil, ErrReservationNotActive
	}

	upd := tx.WithContext(ctx).Model(&LandStock{}).
		Where("id = ? AND tenant_id = ?", res.LandStockID, tenantID).
		Update("reserved_quantity_m2", gorm.Expr("reserved_quantity_m2 - ?", res.QuantityM2))
	if upd.Error != nil {
		return nil, fmt.Errorf("decrement reserved_quantity_m2: %w", upd.Error)
	}

	if err := tx.WithContext(ctx).Model(&LandStockReservation{}).
		Where("id = ? AND tenant_id = ?", id, tenantID).
		Updates(map[string]any{"status": status, "cancelled_reason": reason}).Error; err != nil {
		return nil, fmt.Errorf("close reservation: %w", err)
	}
	res.Status = status
	res.CancelledReason = reason
	return &res, nil
}

// ── Akad (LT-5) ──────────────────────────────────────────────────────────

// RecordAkad: row-locks land_stock (and the reservation, if any), re-checks
// availability, posts the revenue+HPP journals, inserts land_sales
// (status=akad) + land_allocations (always, mandatory 1:1, R-2), and — if a
// reservation was supplied — closes it (status=converted) with the new sale's
// id. One transaction; rollback-all-or-nothing (pola identik sale.Execute).
func (r *GORMRepository) RecordAkad(ctx context.Context, tenantID uint64, in RecordAkadParams) (*LandSale, error) {
	var sale *LandSale
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var err error
		sale, err = RecordAkadTx(ctx, tx, tenantID, in)
		return err
	})
	if err != nil {
		return nil, err
	}
	return sale, nil
}

// RecordAkadTx is the transaction-composable core of RecordAkad — used by
// sale.Execute (internal/sale/repository.go) to post the Kelebihan Tanah
// revenue+HPP journals for a booking-embedded land component inside the
// SAME transaction as the unit's own Event3/4 journals, at Akad timing.
func RecordAkadTx(ctx context.Context, tx *gorm.DB, tenantID uint64, in RecordAkadParams) (*LandSale, error) {
	var sale LandSale
	err := func() error {
		var pool LandStock
		if err := tx.WithContext(ctx).
			Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ? AND tenant_id = ?", in.LandStockID, tenantID).
			First(&pool).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrLandStockNotFound
			}
			return fmt.Errorf("lock land_stock %d: %w", in.LandStockID, err)
		}

		if in.ReservationID != nil {
			var res LandStockReservation
			if err := tx.WithContext(ctx).
				Clauses(clause.Locking{Strength: "UPDATE"}).
				Where("id = ? AND tenant_id = ?", *in.ReservationID, tenantID).
				First(&res).Error; err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return ErrReservationNotFound
				}
				return fmt.Errorf("lock reservation %d: %w", *in.ReservationID, err)
			}
			if res.Status != ReservationStatusActive {
				return ErrReservationNotActive
			}
			if res.CustomerID != in.CustomerID {
				return ErrReservationCustomerMismatch
			}
			if !res.QuantityM2.Equal(in.QuantityM2) {
				return ErrReservationQuantityMismatch
			}
			if upd := tx.WithContext(ctx).Model(&LandStock{}).
				Where("id = ? AND tenant_id = ?", in.LandStockID, tenantID).
				Update("reserved_quantity_m2", gorm.Expr("reserved_quantity_m2 - ?", res.QuantityM2)); upd.Error != nil {
				return fmt.Errorf("decrement reserved_quantity_m2: %w", upd.Error)
			}
		} else if in.QuantityM2.GreaterThan(pool.AvailableQuantityM2()) {
			return ErrCapacityExceeded
		}

		upd := tx.WithContext(ctx).Model(&LandStock{}).
			Where("id = ? AND tenant_id = ?", in.LandStockID, tenantID).
			Update("sold_quantity_m2", gorm.Expr("sold_quantity_m2 + ?", in.QuantityM2))
		if upd.Error != nil {
			if isCheckConstraintViolation(upd.Error) {
				return ErrCapacityExceeded
			}
			return fmt.Errorf("increment sold_quantity_m2: %w", upd.Error)
		}

		txLedgerRepo := ledger.NewGORMRepository(tx)
		txPosting := ledger.NewPostingService(txLedgerRepo, txLedgerRepo).WithPeriodChecker(txLedgerRepo)

		revEntry, err := txPosting.CreateAndPost(ctx, ledger.CreateJournalRequest{
			TenantID:    tenantID,
			Date:        in.RecognitionDate,
			Description: fmt.Sprintf("Akad Kelebihan Tanah proyek %d", in.ProjectID),
			Source:      "land",
			CreatedBy:   in.CreatedBy,
			Lines:       toLedgerLines(in.RevenueLines),
			Document:    ledger.DocumentSpec{TypeCode: ledger.DocCashIn},
		})
		if err != nil {
			return fmt.Errorf("jurnal pendapatan Akad: %w", err)
		}

		var cogsJournalID *uint64
		if len(in.COGSLines) > 0 {
			cogsEntry, err := txPosting.CreateAndPost(ctx, ledger.CreateJournalRequest{
				TenantID:    tenantID,
				Date:        in.RecognitionDate,
				Description: fmt.Sprintf("HPP Akad Kelebihan Tanah proyek %d", in.ProjectID),
				Source:      "land",
				CreatedBy:   in.CreatedBy,
				Lines:       toLedgerLines(in.COGSLines),
			})
			if err != nil {
				return fmt.Errorf("jurnal HPP Akad: %w", err)
			}
			cogsJournalID = &cogsEntry.ID
		}

		recogDate := in.RecognitionDate
		sale = LandSale{
			TenantID:           tenantID,
			ProjectID:          in.ProjectID,
			LandStockID:        in.LandStockID,
			ReservationID:      in.ReservationID,
			CustomerID:         in.CustomerID,
			SalesPersonID:      in.SalesPersonID,
			QuantityM2:         in.QuantityM2,
			UnitPriceSnapshot:  in.UnitPriceSnapshot,
			DPPAmount:          in.DPPAmount,
			IsPKP:              in.IsPKP,
			VATRateSnapshot:    in.VATRateSnapshot,
			GrossAmount:        in.GrossAmount,
			PaymentAccountCode: in.PaymentAccountCode,
			RecognitionDate:    &recogDate,
			Status:             LandSaleStatusAkad,
			RevenueJournalID:   &revEntry.ID,
			CogsJournalID:      cogsJournalID,
			CreatedBy:          in.CreatedBy,
		}
		if err := tx.WithContext(ctx).Create(&sale).Error; err != nil {
			return fmt.Errorf("simpan land_sales: %w", err)
		}

		if in.ReservationID != nil {
			if err := tx.WithContext(ctx).Model(&LandStockReservation{}).
				Where("id = ? AND tenant_id = ?", *in.ReservationID, tenantID).
				Updates(map[string]any{"status": ReservationStatusConverted, "converted_sale_id": sale.ID}).Error; err != nil {
				return fmt.Errorf("tutup reservasi (converted): %w", err)
			}
		}

		alloc := LandAllocation{
			TenantID:                  tenantID,
			ProjectID:                 in.ProjectID,
			LandSaleID:                sale.ID,
			LandStockID:               in.LandStockID,
			QuantityM2:                in.QuantityM2,
			HPPRatePerM2Snapshot:      in.HPPRatePerM2,
			HPPTotal:                  in.HPPTotal,
			Basis:                     in.Basis,
			AllocationConfigVersionID: in.AllocationConfigVersionID,
			AllocationConfigVersion:   in.AllocationConfigVersion,
			BudgetPlanID:              in.BudgetPlanID,
			BudgetPlanVersion:         in.BudgetPlanVersion,
		}
		if err := tx.WithContext(ctx).Create(&alloc).Error; err != nil {
			return fmt.Errorf("simpan land_allocations: %w", err)
		}

		return nil
	}()
	if err != nil {
		return nil, err
	}
	return &sale, nil
}

// CancelLandSale: row-locks land_sales (must be status=akad), row-locks
// land_stock, reverses the posted revenue journal (and COGS journal, if any)
// via ledger.PostingService.Reverse() — the sole sanctioned correction path
// (CLAUDE.md invariant #5) — decrements land_stock.sold_quantity_m2 (quantity
// returns to AVAILABLE, not RESERVED, per §F.3), and marks land_sales
// cancelled. One transaction; rollback-all-or-nothing (pola identik RecordAkad).
func (r *GORMRepository) CancelLandSale(ctx context.Context, tenantID, id uint64, in CancelLandSaleParams) (*LandSale, error) {
	var sale *LandSale
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var err error
		sale, err = CancelLandSaleTx(ctx, tx, tenantID, id, in)
		return err
	})
	if err != nil {
		return nil, err
	}
	return sale, nil
}

// CancelLandSaleTx is the transaction-composable core of CancelLandSale —
// used by internal/cancellation to reverse a booking-embedded land
// component's revenue+HPP journals inside the same transaction as the
// unit's own reversal, when a post-Akad unit sale is cancelled.
func CancelLandSaleTx(ctx context.Context, tx *gorm.DB, tenantID, id uint64, in CancelLandSaleParams) (*LandSale, error) {
	var sale LandSale
	err := func() error {
		if err := tx.WithContext(ctx).
			Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ? AND tenant_id = ?", id, tenantID).
			First(&sale).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrLandSaleNotFound
			}
			return fmt.Errorf("lock land_sale %d: %w", id, err)
		}
		if sale.Status != LandSaleStatusAkad {
			return ErrLandSaleNotAkad
		}

		var pool LandStock
		if err := tx.WithContext(ctx).
			Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ? AND tenant_id = ?", sale.LandStockID, tenantID).
			First(&pool).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrLandStockNotFound
			}
			return fmt.Errorf("lock land_stock %d: %w", sale.LandStockID, err)
		}

		txLedgerRepo := ledger.NewGORMRepository(tx)
		txPosting := ledger.NewPostingService(txLedgerRepo, txLedgerRepo).WithPeriodChecker(txLedgerRepo)

		var revReversalID *uint64
		if sale.RevenueJournalID != nil {
			revRev, err := txPosting.Reverse(ctx, tenantID, *sale.RevenueJournalID, in.CancelDate)
			if err != nil {
				return fmt.Errorf("balik jurnal pendapatan Akad: %w", err)
			}
			revReversalID = &revRev.ID
		}

		var cogsReversalID *uint64
		if sale.CogsJournalID != nil {
			cogsRev, err := txPosting.Reverse(ctx, tenantID, *sale.CogsJournalID, in.CancelDate)
			if err != nil {
				return fmt.Errorf("balik jurnal HPP Akad: %w", err)
			}
			cogsReversalID = &cogsRev.ID
		}

		upd := tx.WithContext(ctx).Model(&LandStock{}).
			Where("id = ? AND tenant_id = ? AND sold_quantity_m2 >= ?", sale.LandStockID, tenantID, sale.QuantityM2).
			Update("sold_quantity_m2", gorm.Expr("sold_quantity_m2 - ?", sale.QuantityM2))
		if upd.Error != nil {
			return fmt.Errorf("decrement sold_quantity_m2: %w", upd.Error)
		}
		if upd.RowsAffected == 0 {
			return fmt.Errorf("land_stock %d: sold_quantity_m2 tidak cukup untuk membalik land_sale %d (data tidak konsisten)", sale.LandStockID, id)
		}

		now := in.CancelDate
		reason := in.Reason
		updates := map[string]any{
			"status":                      LandSaleStatusCancelled,
			"cancelled_at":                now,
			"cancel_reason":               reason,
			"cancelled_by":                in.CancelledBy,
			"revenue_reversal_journal_id": revReversalID,
			"cogs_reversal_journal_id":    cogsReversalID,
		}
		if err := tx.WithContext(ctx).Model(&LandSale{}).
			Where("id = ? AND tenant_id = ?", id, tenantID).
			Updates(updates).Error; err != nil {
			return fmt.Errorf("update land_sales (cancelled): %w", err)
		}

		sale.Status = LandSaleStatusCancelled
		sale.CancelledAt = &now
		sale.CancelReason = reason
		sale.CancelledBy = in.CancelledBy
		sale.RevenueReversalJournalID = revReversalID
		sale.CogsReversalJournalID = cogsReversalID
		return nil
	}()
	if err != nil {
		return nil, err
	}
	return &sale, nil
}

func (r *GORMRepository) FindLandSale(ctx context.Context, tenantID, id uint64) (*LandSale, error) {
	var sale LandSale
	err := r.db.WithContext(ctx).
		Where("id = ? AND tenant_id = ?", id, tenantID).
		First(&sale).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrLandSaleNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("FindLandSale: %w", err)
	}
	return &sale, nil
}

func (r *GORMRepository) ListLandSalesByProject(ctx context.Context, tenantID, projectID uint64) ([]LandSale, error) {
	var list []LandSale
	err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND project_id = ?", tenantID, projectID).
		Order("recognition_date DESC").
		Find(&list).Error
	if err != nil {
		return nil, fmt.Errorf("ListLandSalesByProject: %w", err)
	}
	return list, nil
}

func (r *GORMRepository) FindAccountIDByCode(ctx context.Context, tenantID uint64, code string) (uint64, error) {
	var acc ledger.Account
	err := r.db.WithContext(ctx).
		Select("id").
		Where("tenant_id = ? AND code = ?", tenantID, code).
		First(&acc).Error
	if err != nil {
		return 0, fmt.Errorf("akun %q tidak ditemukan: %w", code, err)
	}
	return acc.ID, nil
}

// ValidateCashBankAccount loads the account (tenant-scoped) then delegates to
// the shared ledger.ValidatePaymentAccount helper — pola identik
// sale.GORMRepository.ValidateCashBankAccount, no duplicated COA logic.
func (r *GORMRepository) ValidateCashBankAccount(ctx context.Context, tenantID uint64, code string) error {
	var acc ledger.Account
	err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND code = ?", tenantID, code).
		First(&acc).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrPaymentAccountNotFound
		}
		return fmt.Errorf("ValidateCashBankAccount: %w", err)
	}
	switch ledger.ValidatePaymentAccount(&acc) {
	case nil:
		return nil
	case ledger.ErrAccountInactive:
		return ErrPaymentAccountInactive
	default:
		return ErrInvalidBankAccount
	}
}

func toLedgerLines(lines []JournalLineInput) []ledger.LineInput {
	out := make([]ledger.LineInput, len(lines))
	for i, l := range lines {
		out[i] = ledger.LineInput{
			AccountID:   l.AccountID,
			Debit:       l.Debit,
			Credit:      l.Credit,
			ProjectID:   l.ProjectID,
			Description: l.Description,
		}
	}
	return out
}

// isDuplicateKeyError detects MySQL duplicate key violation (error 1062).
func isDuplicateKeyError(err error) bool {
	return containsAny(err, "Duplicate entry", "duplicate key", "1062")
}

// isForeignKeyViolation detects MySQL FK constraint violation (error 1452).
func isForeignKeyViolation(err error) bool {
	return containsAny(err, "foreign key constraint", "1452")
}

// isCheckConstraintViolation detects MySQL CHECK constraint violation (error 3819).
func isCheckConstraintViolation(err error) bool {
	return containsAny(err, "chk_land_stock_capacity", "3819")
}

func containsAny(err error, subs ...string) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

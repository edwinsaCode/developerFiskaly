package project

import (
	"context"
	"fmt"
	"strings"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"

	"esaproperti/internal/domain"
)

// GORMRepository implements ProjectStore, PhaseStore, and UnitStore.
// Every query carries an explicit tenant_id predicate (Invariant #6).
type GORMRepository struct {
	db *gorm.DB
}

func NewGORMRepository(db *gorm.DB) *GORMRepository {
	return &GORMRepository{db: db}
}

// ── ProjectStore ──────────────────────────────────────────────────────────────

func (r *GORMRepository) CreateProject(ctx context.Context, p *Project) error {
	if err := r.db.WithContext(ctx).Create(p).Error; err != nil {
		return fmt.Errorf("CreateProject: %w", err)
	}
	return nil
}

func (r *GORMRepository) FindProjectByID(ctx context.Context, tenantID, id uint64) (*Project, error) {
	var p Project
	err := r.db.WithContext(ctx).
		First(&p, "id = ? AND tenant_id = ?", id, tenantID).Error
	if err != nil {
		return nil, ErrProjectNotFound
	}
	return &p, nil
}

func (r *GORMRepository) ListProjects(ctx context.Context, tenantID uint64) ([]*Project, error) {
	var ps []*Project
	err := r.db.WithContext(ctx).
		Where("tenant_id = ?", tenantID).
		Order("id ASC").
		Find(&ps).Error
	if err != nil {
		return nil, fmt.Errorf("ListProjects: %w", err)
	}
	return ps, nil
}

func (r *GORMRepository) UpdateProjectTaxCategory(ctx context.Context, tenantID, id uint64, category domain.TaxCategory) error {
	res := r.db.WithContext(ctx).Model(&Project{}).
		Where("id = ? AND tenant_id = ?", id, tenantID).
		Update("tax_category", string(category))
	if res.Error != nil {
		return fmt.Errorf("UpdateProjectTaxCategory: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return ErrProjectNotFound
	}
	return nil
}

func (r *GORMRepository) UpdateProjectStatus(ctx context.Context, tenantID, id uint64, status ProjectStatus) error {
	res := r.db.WithContext(ctx).Model(&Project{}).
		Where("id = ? AND tenant_id = ?", id, tenantID).
		Update("status", string(status))
	if res.Error != nil {
		return fmt.Errorf("UpdateProjectStatus: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return ErrProjectNotFound
	}
	return nil
}

// ── PhaseStore ────────────────────────────────────────────────────────────────

func (r *GORMRepository) CreatePhase(ctx context.Context, ph *ProjectPhase) error {
	if err := r.db.WithContext(ctx).Create(ph).Error; err != nil {
		return fmt.Errorf("CreatePhase: %w", err)
	}
	return nil
}

func (r *GORMRepository) FindPhaseByID(ctx context.Context, tenantID, id uint64) (*ProjectPhase, error) {
	var ph ProjectPhase
	err := r.db.WithContext(ctx).
		First(&ph, "id = ? AND tenant_id = ?", id, tenantID).Error
	if err != nil {
		return nil, ErrPhaseNotFound
	}
	return &ph, nil
}

func (r *GORMRepository) ListPhasesByProject(ctx context.Context, tenantID, projectID uint64) ([]*ProjectPhase, error) {
	var phs []*ProjectPhase
	err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND project_id = ?", tenantID, projectID).
		Order("id ASC").
		Find(&phs).Error
	if err != nil {
		return nil, fmt.Errorf("ListPhasesByProject: %w", err)
	}
	return phs, nil
}

func (r *GORMRepository) UpdatePhaseStatus(ctx context.Context, tenantID, id uint64, status PhaseStatus) error {
	res := r.db.WithContext(ctx).Model(&ProjectPhase{}).
		Where("id = ? AND tenant_id = ?", id, tenantID).
		Update("status", string(status))
	if res.Error != nil {
		return fmt.Errorf("UpdatePhaseStatus: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return ErrPhaseNotFound
	}
	return nil
}

// ── UnitStore ─────────────────────────────────────────────────────────────────

func (r *GORMRepository) CreateUnit(ctx context.Context, u *Unit) error {
	if err := r.db.WithContext(ctx).Create(u).Error; err != nil {
		if isDuplicateKeyError(err) {
			return ErrUnitCodeDuplicate
		}
		return fmt.Errorf("CreateUnit: %w", err)
	}
	return nil
}

// CreateUnitsAtomic (UAT Batch 2 §4): buat seluruh unit blok dalam SATU
// transaksi. Cek duplikat kode eksplisit di dalam tx (lock ringan via insert
// setelah cek) — satu duplikat = seluruh batch batal, tidak ada partial.
func (r *GORMRepository) CreateUnitsAtomic(ctx context.Context, tenantID, projectID uint64, units []*Unit) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		codes := make([]string, len(units))
		for i, u := range units {
			codes[i] = u.Code
		}
		var existing []string
		if err := tx.Model(&Unit{}).
			Where("tenant_id = ? AND project_id = ? AND code IN ?", tenantID, projectID, codes).
			Pluck("code", &existing).Error; err != nil {
			return fmt.Errorf("cek duplikat kode: %w", err)
		}
		if len(existing) > 0 {
			return fmt.Errorf("%w: %s", ErrBulkDuplicateCode, strings.Join(existing, ", "))
		}
		for _, u := range units {
			if err := tx.Create(u).Error; err != nil {
				if isDuplicateKeyError(err) {
					return fmt.Errorf("%w: %s", ErrBulkDuplicateCode, u.Code)
				}
				return fmt.Errorf("CreateUnitsAtomic %s: %w", u.Code, err)
			}
		}
		return nil
	})
}

func (r *GORMRepository) FindUnitByID(ctx context.Context, tenantID, id uint64) (*Unit, error) {
	var u Unit
	err := r.db.WithContext(ctx).
		First(&u, "id = ? AND tenant_id = ?", id, tenantID).Error
	if err != nil {
		return nil, ErrUnitNotFound
	}
	return &u, nil
}

func (r *GORMRepository) ListUnitsByProject(ctx context.Context, tenantID, projectID uint64) ([]*Unit, error) {
	var us []*Unit
	err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND project_id = ?", tenantID, projectID).
		Order("id ASC").
		Find(&us).Error
	if err != nil {
		return nil, fmt.Errorf("ListUnitsByProject: %w", err)
	}
	return us, nil
}

func (r *GORMRepository) ListUnitsByPhase(ctx context.Context, tenantID, phaseID uint64) ([]*Unit, error) {
	var us []*Unit
	err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND phase_id = ?", tenantID, phaseID).
		Order("id ASC").
		Find(&us).Error
	if err != nil {
		return nil, fmt.Errorf("ListUnitsByPhase: %w", err)
	}
	return us, nil
}

func (r *GORMRepository) UpdateUnitStatus(ctx context.Context, tenantID, id uint64, status UnitStatus, opts UpdateUnitOpts) error {
	updates := map[string]interface{}{"status": string(status)}
	if opts.BuyerRef != nil {
		updates["buyer_ref"] = *opts.BuyerRef
	}
	if opts.SaleDate != nil {
		updates["sale_date"] = opts.SaleDate
	}
	if opts.SalePrice != nil {
		updates["sale_price"] = opts.SalePrice
	}
	res := r.db.WithContext(ctx).Model(&Unit{}).
		Where("id = ? AND tenant_id = ?", id, tenantID).
		Updates(updates)
	if res.Error != nil {
		return fmt.Errorf("UpdateUnitStatus: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return ErrUnitNotFound
	}
	return nil
}

// UpdateUnitLandArea (LT-2): update satu kolom, tanpa mengganggu jalur status
// transition (UpdateUnitStatus/TransitionUnitAtomic) sama sekali.
func (r *GORMRepository) UpdateUnitLandArea(ctx context.Context, tenantID, id uint64, landArea decimal.Decimal) error {
	res := r.db.WithContext(ctx).Model(&Unit{}).
		Where("id = ? AND tenant_id = ?", id, tenantID).
		Update("land_area", landArea)
	if res.Error != nil {
		return fmt.Errorf("UpdateUnitLandArea: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return ErrUnitNotFound
	}
	return nil
}

// TransitionUnitAtomic updates a unit's status AND appends a transition log row
// in ONE DB transaction (Increment 6). The UPDATE is guarded by the expected
// `from` status so a concurrent change cannot slip through (optimistic guard).
// Either both writes commit or neither does.
func (r *GORMRepository) TransitionUnitAtomic(ctx context.Context, tenantID, id uint64, from, to UnitStatus, opts UpdateUnitOpts, log *UnitStatusTransition) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		updates := map[string]interface{}{"status": string(to)}
		if opts.BuyerRef != nil {
			updates["buyer_ref"] = *opts.BuyerRef
		}
		if opts.SaleDate != nil {
			updates["sale_date"] = opts.SaleDate
		}
		if opts.SalePrice != nil {
			updates["sale_price"] = opts.SalePrice
		}
		res := tx.Model(&Unit{}).
			Where("id = ? AND tenant_id = ? AND status = ?", id, tenantID, string(from)).
			Updates(updates)
		if res.Error != nil {
			return fmt.Errorf("TransitionUnitAtomic update: %w", res.Error)
		}
		if res.RowsAffected == 0 {
			// Unit hilang, tenant salah, atau status berubah sejak dibaca (race).
			return ErrUnitTransitionConflict
		}
		if err := LogTransitionTx(tx, log); err != nil {
			return err
		}
		return nil
	})
}

// LogTransitionTx appends one transition row on the given tx. Exported so other
// packages (mis. sale BAST atomic writer) dapat menulis log DI DALAM tx mereka
// sendiri — menjamin "status + log" satu unit-of-work (pola snapshot P0-3).
// Append-only: hanya INSERT, tidak pernah update/delete.
func LogTransitionTx(tx *gorm.DB, log *UnitStatusTransition) error {
	if err := tx.Create(log).Error; err != nil {
		return fmt.Errorf("LogTransitionTx: %w", err)
	}
	return nil
}

// ListProjectTransitions returns recent transitions across ALL units of a
// project, newest first (workspace Timeline tab). Limit hard-capped.
func (r *GORMRepository) ListProjectTransitions(ctx context.Context, tenantID, projectID uint64, limit int) ([]*UnitStatusTransition, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	var out []*UnitStatusTransition
	err := r.db.WithContext(ctx).
		Joins("JOIN units u ON u.id = unit_status_transitions.unit_id").
		Where("unit_status_transitions.tenant_id = ? AND u.project_id = ?", tenantID, projectID).
		Order("unit_status_transitions.id DESC").
		Limit(limit).
		Find(&out).Error
	if err != nil {
		return nil, fmt.Errorf("ListProjectTransitions: %w", err)
	}
	return out, nil
}

// ListTransitions returns a unit's status history, oldest first (audit + time-on-market).
func (r *GORMRepository) ListTransitions(ctx context.Context, tenantID, unitID uint64) ([]*UnitStatusTransition, error) {
	var out []*UnitStatusTransition
	err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND unit_id = ?", tenantID, unitID).
		Order("id ASC").
		Find(&out).Error
	if err != nil {
		return nil, fmt.Errorf("ListTransitions: %w", err)
	}
	return out, nil
}

// isDuplicateKeyError detects MySQL duplicate key violation (error 1062).
func isDuplicateKeyError(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	for _, sub := range []string{"Duplicate entry", "duplicate key", "1062"} {
		if len(s) >= len(sub) {
			for i := 0; i <= len(s)-len(sub); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
		}
	}
	return false
}

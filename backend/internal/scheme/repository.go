package scheme

import (
	"context"
	"errors"
	"fmt"

	"github.com/go-sql-driver/mysql"
	"gorm.io/gorm"
)

// GORMRepository — akses DB tenant-scoped (Invariant #6, predikat manual).
type GORMRepository struct {
	db *gorm.DB
}

func NewGORMRepository(db *gorm.DB) *GORMRepository {
	return &GORMRepository{db: db}
}

func isDuplicateKeyError(err error) bool {
	var me *mysql.MySQLError
	if errors.As(err, &me) {
		return me.Number == 1062
	}
	return false
}

// ── PaymentScheme ─────────────────────────────────────────────────────────────

func (r *GORMRepository) CreateScheme(ctx context.Context, s *PaymentScheme) error {
	if err := r.db.WithContext(ctx).Create(s).Error; err != nil {
		if isDuplicateKeyError(err) {
			return ErrSchemeCodeDup
		}
		return fmt.Errorf("CreateScheme: %w", err)
	}
	return nil
}

func (r *GORMRepository) FindSchemeByID(ctx context.Context, tenantID, id uint64) (*PaymentScheme, error) {
	var s PaymentScheme
	err := r.db.WithContext(ctx).First(&s, "id = ? AND tenant_id = ?", id, tenantID).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrSchemeNotFound
		}
		return nil, fmt.Errorf("FindSchemeByID: %w", err)
	}
	return &s, nil
}

func (r *GORMRepository) ListSchemes(ctx context.Context, tenantID uint64) ([]*PaymentScheme, error) {
	var out []*PaymentScheme
	if err := r.db.WithContext(ctx).
		Where("tenant_id = ?", tenantID).
		Order("code ASC").
		Find(&out).Error; err != nil {
		return nil, fmt.Errorf("ListSchemes: %w", err)
	}
	return out, nil
}

// UpdateScheme memperbarui name/params/is_active. `code` & `policy_type`
// IMMUTABLE by construction — tidak disertakan dalam kolom update.
func (r *GORMRepository) UpdateScheme(ctx context.Context, s *PaymentScheme) error {
	res := r.db.WithContext(ctx).Model(&PaymentScheme{}).
		Where("id = ? AND tenant_id = ?", s.ID, s.TenantID).
		Updates(map[string]interface{}{
			"name":      s.Name,
			"params":    s.Params,
			"is_active": s.IsActive,
		})
	if res.Error != nil {
		return fmt.Errorf("UpdateScheme: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return ErrSchemeNotFound
	}
	return nil
}

// ── FinancingSource ───────────────────────────────────────────────────────────

func (r *GORMRepository) CreateFinancingSource(ctx context.Context, f *FinancingSource) error {
	if err := r.db.WithContext(ctx).Create(f).Error; err != nil {
		if isDuplicateKeyError(err) {
			return ErrFinSourceCodeDup
		}
		return fmt.Errorf("CreateFinancingSource: %w", err)
	}
	return nil
}

func (r *GORMRepository) FindFinancingSourceByID(ctx context.Context, tenantID, id uint64) (*FinancingSource, error) {
	var f FinancingSource
	err := r.db.WithContext(ctx).First(&f, "id = ? AND tenant_id = ?", id, tenantID).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrFinSourceNotFound
		}
		return nil, fmt.Errorf("FindFinancingSourceByID: %w", err)
	}
	return &f, nil
}

func (r *GORMRepository) ListFinancingSources(ctx context.Context, tenantID uint64) ([]*FinancingSource, error) {
	var out []*FinancingSource
	if err := r.db.WithContext(ctx).
		Where("tenant_id = ?", tenantID).
		Order("code ASC").
		Find(&out).Error; err != nil {
		return nil, fmt.Errorf("ListFinancingSources: %w", err)
	}
	return out, nil
}

func (r *GORMRepository) UpdateFinancingSource(ctx context.Context, f *FinancingSource) error {
	res := r.db.WithContext(ctx).Model(&FinancingSource{}).
		Where("id = ? AND tenant_id = ?", f.ID, f.TenantID).
		Updates(map[string]interface{}{
			"name":      f.Name,
			"type":      f.Type,
			"is_active": f.IsActive,
		})
	if res.Error != nil {
		return fmt.Errorf("UpdateFinancingSource: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return ErrFinSourceNotFound
	}
	return nil
}

package salesorg

import (
	"context"
	"errors"
	"fmt"

	"github.com/go-sql-driver/mysql"
	"gorm.io/gorm"
)

// GORMRepository mengimplementasikan TeamStore & PersonStore. Tenant-scoped manual.
type GORMRepository struct {
	db *gorm.DB
}

func NewGORMRepository(db *gorm.DB) *GORMRepository {
	return &GORMRepository{db: db}
}

// ── Sales Team ─────────────────────────────────────────────────────────────────

func (r *GORMRepository) CreateTeam(ctx context.Context, t *SalesTeam) error {
	if err := r.db.WithContext(ctx).Create(t).Error; err != nil {
		if isDuplicateKeyError(err) {
			return ErrTeamCodeDup
		}
		return fmt.Errorf("CreateTeam: %w", err)
	}
	return nil
}

func (r *GORMRepository) FindTeamByID(ctx context.Context, tenantID, id uint64) (*SalesTeam, error) {
	var t SalesTeam
	err := r.db.WithContext(ctx).First(&t, "id = ? AND tenant_id = ?", id, tenantID).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrTeamNotFound
		}
		return nil, fmt.Errorf("FindTeamByID: %w", err)
	}
	return &t, nil
}

func (r *GORMRepository) ListTeams(ctx context.Context, tenantID uint64) ([]*SalesTeam, error) {
	var ts []*SalesTeam
	err := r.db.WithContext(ctx).Where("tenant_id = ?", tenantID).Order("id ASC").Find(&ts).Error
	if err != nil {
		return nil, fmt.Errorf("ListTeams: %w", err)
	}
	return ts, nil
}

func (r *GORMRepository) UpdateTeam(ctx context.Context, t *SalesTeam) error {
	res := r.db.WithContext(ctx).Model(&SalesTeam{}).
		Where("id = ? AND tenant_id = ?", t.ID, t.TenantID).
		Updates(map[string]interface{}{
			"name":                   t.Name,
			"leader_sales_person_id": t.LeaderSalesPersonID,
			"is_active":              t.IsActive,
		})
	if res.Error != nil {
		return fmt.Errorf("UpdateTeam: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return ErrTeamNotFound
	}
	return nil
}

// ── Sales Person ───────────────────────────────────────────────────────────────

func (r *GORMRepository) CreatePerson(ctx context.Context, p *SalesPerson) error {
	if err := r.db.WithContext(ctx).Create(p).Error; err != nil {
		if isDuplicateKeyError(err) {
			return ErrPersonCodeDup
		}
		return fmt.Errorf("CreatePerson: %w", err)
	}
	return nil
}

func (r *GORMRepository) FindPersonByID(ctx context.Context, tenantID, id uint64) (*SalesPerson, error) {
	var p SalesPerson
	err := r.db.WithContext(ctx).First(&p, "id = ? AND tenant_id = ?", id, tenantID).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrPersonNotFound
		}
		return nil, fmt.Errorf("FindPersonByID: %w", err)
	}
	return &p, nil
}

func (r *GORMRepository) ListPersons(ctx context.Context, tenantID uint64) ([]*SalesPerson, error) {
	var ps []*SalesPerson
	err := r.db.WithContext(ctx).Where("tenant_id = ?", tenantID).Order("id ASC").Find(&ps).Error
	if err != nil {
		return nil, fmt.Errorf("ListPersons: %w", err)
	}
	return ps, nil
}

func (r *GORMRepository) UpdatePerson(ctx context.Context, p *SalesPerson) error {
	res := r.db.WithContext(ctx).Model(&SalesPerson{}).
		Where("id = ? AND tenant_id = ?", p.ID, p.TenantID).
		Updates(map[string]interface{}{
			"name":          p.Name,
			"sales_team_id": p.SalesTeamID,
			"phone":         p.Phone,
			"email":         p.Email,
			"join_date":     p.JoinDate,
			"is_active":     p.IsActive,
		})
	if res.Error != nil {
		return fmt.Errorf("UpdatePerson: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return ErrPersonNotFound
	}
	return nil
}

func isDuplicateKeyError(err error) bool {
	var me *mysql.MySQLError
	if errors.As(err, &me) {
		return me.Number == 1062
	}
	return false
}

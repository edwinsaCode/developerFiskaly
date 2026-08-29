package customer

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

func (r *GORMRepository) Create(ctx context.Context, c *Customer) error {
	if err := r.db.WithContext(ctx).Create(c).Error; err != nil {
		if isDuplicateKeyError(err) {
			return ErrCustomerCodeDuplicate
		}
		return fmt.Errorf("Create customer: %w", err)
	}
	return nil
}

func (r *GORMRepository) FindByID(ctx context.Context, tenantID, id uint64) (*Customer, error) {
	var c Customer
	err := r.db.WithContext(ctx).First(&c, "id = ? AND tenant_id = ?", id, tenantID).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrCustomerNotFound
		}
		return nil, fmt.Errorf("FindByID customer: %w", err)
	}
	return &c, nil
}

func (r *GORMRepository) List(ctx context.Context, tenantID uint64) ([]*Customer, error) {
	var cs []*Customer
	err := r.db.WithContext(ctx).
		Where("tenant_id = ?", tenantID).
		Order("id ASC").
		Find(&cs).Error
	if err != nil {
		return nil, fmt.Errorf("List customer: %w", err)
	}
	return cs, nil
}

// Update mengganti field profil yang dapat diubah (bukan code). Mengembalikan
// ErrCustomerNotFound bila baris tak ada / lintas tenant.
func (r *GORMRepository) Update(ctx context.Context, c *Customer) error {
	res := r.db.WithContext(ctx).Model(&Customer{}).
		Where("id = ? AND tenant_id = ?", c.ID, c.TenantID).
		Updates(map[string]interface{}{
			"name":      c.Name,
			"type":      c.Type,
			"segment":   c.Segment,
			"id_number": c.IDNumber,
			"npwp":      c.NPWP,
			"phone":     c.Phone,
			"email":     c.Email,
			"address":   c.Address,
			"is_active": c.IsActive,
		})
	if res.Error != nil {
		return fmt.Errorf("Update customer: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return ErrCustomerNotFound
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

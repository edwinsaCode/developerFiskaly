package tenant

import (
	"context"
	"fmt"

	"gorm.io/gorm"
)

// GORMRepository implements both TenantStore and UserStore using GORM + MySQL.
// Method names are prefixed (CreateTenant, CreateUser, etc.) to avoid collisions
// when a single struct satisfies two interfaces with overlapping method names.
type GORMRepository struct {
	db *gorm.DB
}

// NewGORMRepository constructs a GORMRepository.
func NewGORMRepository(db *gorm.DB) *GORMRepository {
	return &GORMRepository{db: db}
}

// ── TenantStore ───────────────────────────────────────────────────────────────

func (r *GORMRepository) CreateTenant(ctx context.Context, t *Tenant) error {
	if err := r.db.WithContext(ctx).Create(t).Error; err != nil {
		return fmt.Errorf("create tenant: %w", err)
	}
	return nil
}

func (r *GORMRepository) FindTenantByID(ctx context.Context, id uint64) (*Tenant, error) {
	var t Tenant
	if err := r.db.WithContext(ctx).First(&t, "id = ?", id).Error; err != nil {
		return nil, ErrTenantNotFound
	}
	return &t, nil
}

// ── UserStore ─────────────────────────────────────────────────────────────────

func (r *GORMRepository) CreateUser(ctx context.Context, u *User) error {
	if err := r.db.WithContext(ctx).Create(u).Error; err != nil {
		if isDuplicateKey(err) {
			return ErrEmailAlreadyExists
		}
		return fmt.Errorf("create user: %w", err)
	}
	return nil
}

func (r *GORMRepository) FindUserByEmail(ctx context.Context, email string) (*User, error) {
	var u User
	if err := r.db.WithContext(ctx).First(&u, "email = ?", email).Error; err != nil {
		return nil, ErrUserNotFound
	}
	return &u, nil
}

// FindUserByID returns a user only if they belong to the given tenant (Invariant #6).
func (r *GORMRepository) FindUserByID(ctx context.Context, tenantID, userID uint64) (*User, error) {
	var u User
	if err := r.db.WithContext(ctx).
		First(&u, "id = ? AND tenant_id = ?", userID, tenantID).Error; err != nil {
		return nil, ErrUserNotFound
	}
	return &u, nil
}

// UpdateUser menyimpan perubahan pada user yang sudah ada.
//
// Klausa tenant_id sengaja diulang meskipun pemanggil sudah membaca user itu
// lewat FindUserByID: satu baris UPDATE tidak boleh bergantung pada disiplin
// pemanggil untuk menjaga Invariant #6.
func (r *GORMRepository) UpdateUser(ctx context.Context, u *User) error {
	res := r.db.WithContext(ctx).
		Model(&User{}).
		Where("id = ? AND tenant_id = ?", u.ID, u.TenantID).
		Updates(map[string]any{
			"email":         u.Email,
			"name":          u.Name,
			"password_hash": u.PasswordHash,
			"role":          u.Role,
		})
	if res.Error != nil {
		if isDuplicateKey(res.Error) {
			return ErrEmailAlreadyExists
		}
		return fmt.Errorf("update user: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		// MySQL melaporkan 0 baris untuk UPDATE yang nilainya identik, jadi 0
		// belum tentu "tidak ada". Bedakan keduanya, jangan menuduh user hilang
		// hanya karena owner menekan Simpan tanpa mengubah apa pun.
		var n int64
		if err := r.db.WithContext(ctx).Model(&User{}).
			Where("id = ? AND tenant_id = ?", u.ID, u.TenantID).
			Count(&n).Error; err != nil {
			return fmt.Errorf("update user: verifikasi keberadaan: %w", err)
		}
		if n == 0 {
			return ErrUserNotFound
		}
	}
	return nil
}

func (r *GORMRepository) ListUsersByTenant(ctx context.Context, tenantID uint64) ([]*User, error) {
	var users []*User
	if err := r.db.WithContext(ctx).
		Where("tenant_id = ?", tenantID).
		Order("created_at").
		Find(&users).Error; err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}
	return users, nil
}

// ── helpers ───────────────────────────────────────────────────────────────────

func isDuplicateKey(err error) bool {
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

package tenant

import (
	"time"

	"esaproperti/internal/domain"
)

// Tenant is the top-level isolation boundary.
// All data in the system belongs to exactly one tenant.
type Tenant struct {
	ID        uint64    `gorm:"primaryKey;autoIncrement" json:"id"`
	Name      string    `gorm:"not null;size:200"        json:"name"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (Tenant) TableName() string { return "tenants" }

// User belongs to exactly one tenant and carries a role within that tenant.
// Email is globally unique: used as the login identifier.
//
// W-12: Name adalah identitas tampilan, BUKAN kredensial. Ia boleh kosong —
// user yang dibuat sebelum W-12 tidak punya nama, dan tak seorang pun berhak
// mengarangkannya. Layar yang menampilkannya wajib menyediakan fallback;
// gunakan DisplayName() agar aturannya hanya ada di satu tempat.
type User struct {
	ID           uint64      `gorm:"primaryKey;autoIncrement"           json:"id"`
	TenantID     uint64      `gorm:"not null;index"                     json:"-"`
	Email        string      `gorm:"not null;uniqueIndex;size:200"      json:"email"`
	Name         string      `gorm:"size:200;default:''"                json:"name"`
	PasswordHash string      `gorm:"not null;size:72"                   json:"-"`
	Role         domain.Role `gorm:"not null;size:20;default:'viewer'"  json:"role"`
	CreatedAt    time.Time   `json:"created_at"`
	UpdatedAt    time.Time   `json:"updated_at"`
}

// DisplayName returns the name to show on screen: the user's real name when it
// exists, otherwise their email. Never a fabricated name.
func (u User) DisplayName() string {
	if u.Name != "" {
		return u.Name
	}
	return u.Email
}

func (User) TableName() string { return "users" }

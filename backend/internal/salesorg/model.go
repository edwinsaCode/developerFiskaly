// Package salesorg memodelkan master organisasi penjualan: Sales Team & Sales
// Person (CORE master). Atribusi penjualan; belum ada target/KPI/komisi (P2/FUTURE).
// Master murni: tidak menyentuh ledger.
package salesorg

import "time"

// ── Sales Team ─────────────────────────────────────────────────────────────────

// SalesTeam adalah master tim penjualan (tipis). `code` unik per tenant.
// LeaderSalesPersonID nullable (dapat diisi setelah anggota dibuat).
type SalesTeam struct {
	ID                  uint64    `gorm:"primaryKey;autoIncrement" json:"id"`
	TenantID            uint64    `gorm:"not null;index"           json:"-"`
	Code                string    `gorm:"not null;size:50"         json:"code"` // IMMUTABLE setelah dibuat (tak ada di Update request)
	Name                string    `gorm:"not null;size:150"        json:"name"`
	LeaderSalesPersonID *uint64   `                                json:"leader_sales_person_id,omitempty"`
	IsActive            bool      `gorm:"not null;default:1"       json:"is_active"`
	CreatedAt           time.Time `                                json:"created_at"`
	UpdatedAt           time.Time `                                json:"updated_at"`
}

func (SalesTeam) TableName() string { return "sales_teams" }

// ── Sales Person ───────────────────────────────────────────────────────────────

// SalesPerson adalah master tenaga penjual. `code` unik per tenant. SalesTeamID &
// UserID nullable (logical ref).
type SalesPerson struct {
	ID          uint64     `gorm:"primaryKey;autoIncrement" json:"id"`
	TenantID    uint64     `gorm:"not null;index"           json:"-"`
	Code        string     `gorm:"not null;size:50"         json:"code"` // IMMUTABLE setelah dibuat (tak ada di Update request)
	Name        string     `gorm:"not null;size:200"        json:"name"`
	SalesTeamID *uint64    `gorm:"index"                    json:"sales_team_id,omitempty"`
	UserID      *uint64    `                                json:"user_id,omitempty"`
	Phone       string     `gorm:"size:30"                  json:"phone,omitempty"`
	Email       string     `gorm:"size:120"                 json:"email,omitempty"`
	JoinDate    *time.Time `gorm:"type:date"                json:"join_date,omitempty"`
	IsActive    bool       `gorm:"not null;default:1"       json:"is_active"`
	CreatedAt   time.Time  `                                json:"created_at"`
	UpdatedAt   time.Time  `                                json:"updated_at"`
}

func (SalesPerson) TableName() string { return "sales_persons" }

// ── Request types ──────────────────────────────────────────────────────────────

type CreateTeamRequest struct {
	Code                string
	Name                string
	LeaderSalesPersonID *uint64
}

type UpdateTeamRequest struct {
	Name                string
	LeaderSalesPersonID *uint64
	IsActive            *bool
}

type CreatePersonRequest struct {
	Code        string
	Name        string
	SalesTeamID *uint64
	UserID      *uint64
	Phone       string
	Email       string
	JoinDate    *time.Time
}

type UpdatePersonRequest struct {
	Name        string
	SalesTeamID *uint64
	Phone       string
	Email       string
	JoinDate    *time.Time
	IsActive    *bool
}

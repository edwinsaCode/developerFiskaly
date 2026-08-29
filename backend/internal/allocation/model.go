package allocation

import (
	"time"

	"github.com/shopspring/decimal"

	"esaproperti/internal/domain"
)

// AllocationExecution adalah audit log setiap kali alokasi dijalankan secara eksplisit.
// Menyimpan snapshot: basis, total cost, user, dan timestamp.
// Append-only — tidak pernah dihapus (audit trail).
type AllocationExecution struct {
	ID         uint64          `gorm:"primaryKey;autoIncrement"          json:"id"`
	TenantID   uint64          `gorm:"not null;index"                    json:"-"`
	ProjectID  uint64          `gorm:"not null"                          json:"project_id"`
	Basis      AllocationBasis `gorm:"not null;size:20"                  json:"basis"`
	ExecutedBy uint64          `gorm:"column:executed_by;not null"       json:"executed_by"`
	UserEmail  string          `gorm:"column:user_email;size:200"        json:"user_email"`
	TotalCost  domain.Money    `gorm:"type:DECIMAL(20,4);not null;default:'0.0000'" json:"total_cost"`
	ExecutedAt time.Time       `gorm:"not null"                          json:"executed_at"`
	CreatedAt  time.Time       `                                         json:"created_at"`
	UpdatedAt  time.Time       `                                         json:"updated_at"`
}

func (AllocationExecution) TableName() string { return "allocation_executions" }

// AllocationBasis determines which unit attribute is used as the weight
// when distributing project-wide costs across units.
type AllocationBasis string

const (
	// BasisSaleableArea weights each unit by its saleable area in sqm.
	// Most common in Indonesia for residential projects.
	BasisSaleableArea AllocationBasis = "saleable_area"

	// BasisSalesValue weights each unit by its list price (domain.Money).
	// Better reflects relative revenue contribution for mixed-type projects.
	BasisSalesValue AllocationBasis = "sales_value"
)

// Valid returns true if b is a recognized AllocationBasis.
func (b AllocationBasis) Valid() bool {
	return b == BasisSaleableArea || b == BasisSalesValue
}

// AllocationConfig records which basis a project uses for cost distribution.
// Tercatat (auditable) — changes are timestamped via updated_at.
type AllocationConfig struct {
	ID        uint64          `gorm:"primaryKey;autoIncrement" json:"id"`
	TenantID  uint64          `gorm:"not null;index"           json:"-"`
	ProjectID uint64          `gorm:"not null;uniqueIndex"     json:"project_id"`
	Basis     AllocationBasis `gorm:"not null;size:20"         json:"basis"`
	CreatedAt time.Time       `                                json:"created_at"`
	UpdatedAt time.Time       `                                json:"updated_at"`
}

func (AllocationConfig) TableName() string { return "allocation_configs" }

// UnitInput is all data the engine needs for one unit.
type UnitInput struct {
	UnitID       uint64
	SaleableArea decimal.Decimal          // sqm; used when basis = BasisSaleableArea
	SalesValue   domain.Money             // list_price; used when basis = BasisSalesValue
	Direct       domain.UnitCostBreakdown // biaya yang sudah ber-tag unit_id di jurnal
}

// AllocationResult is the engine output for one unit.
// Direct costs are passed through unchanged; Allocated is the prorated share
// of project-wide costs; Total = Direct + Allocated.
//
// Phase 6: Total.Total() is the HPP basis for Event 4 (kredit Persediaan dipecah
// per sub-akun sesuai Total.Land/Hard/Soft/Financing).
type AllocationResult struct {
	UnitID    uint64
	Direct    domain.UnitCostBreakdown
	Allocated domain.UnitCostBreakdown
	Total     domain.UnitCostBreakdown
	// Weight adalah bobot unit ini pada basis yang dipakai (sqm untuk
	// saleable_area, rupiah list_price untuk sales_value). Bukti alokasi:
	// porsi unit = Weight / Σ(Weight semua unit). Lihat snapshot basis evidence.
	Weight decimal.Decimal
}

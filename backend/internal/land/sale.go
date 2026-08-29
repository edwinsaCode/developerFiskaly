package land

// LT-5 (kelebihan-tanah-final-architecture §B.3, §B.4, §F) — Akad-equivalent
// standalone untuk penjualan Kelebihan Tanah. land_sales TIDAK memaksa masuk
// ke sale_contracts.unit_id (instruksi eksplisit owner) — struktur PPN-nya
// meniru sale_contracts (dpp_amount/is_pkp/vat_rate_snapshot/gross_amount)
// supaya mesin pajak generik yang sudah ada bisa dipakai ulang tanpa
// modifikasi. land_allocations adalah snapshot HPP-nya, 1:1 wajib (R-2),
// SELALU dibuat untuk setiap land_sale (berbeda dari allocation_snapshots
// yang hanya dibuat utk metode budgeted) karena land_sales sendiri tidak
// punya kolom hpp_total/hpp_rate — land_allocations satu-satunya tempat
// angka itu hidup, untuk KEDUA metode (budgeted maupun actual).

import (
	"time"

	"github.com/shopspring/decimal"

	"esaproperti/internal/domain"
)

// LandSaleStatus — persis pola ReservationStatus (land_sales.status CHECK).
type LandSaleStatus string

const (
	LandSaleStatusDraft     LandSaleStatus = "draft"
	LandSaleStatusAkad      LandSaleStatus = "akad"
	LandSaleStatusCancelled LandSaleStatus = "cancelled"
)

// Valid returns true if s is a recognized LandSaleStatus.
func (s LandSaleStatus) Valid() bool {
	switch s {
	case LandSaleStatusDraft, LandSaleStatusAkad, LandSaleStatusCancelled:
		return true
	}
	return false
}

// LandSale is the Akad-equivalent record for one Kelebihan Tanah transaction
// — analog sale_contracts+sale_records digabung, tapi berdiri sendiri
// (bukan units).
type LandSale struct {
	ID                       uint64          `gorm:"primaryKey;autoIncrement"                     json:"id"`
	TenantID                 uint64          `gorm:"not null"                                     json:"-"`
	ProjectID                uint64          `gorm:"not null"                                     json:"project_id"`
	LandStockID              uint64          `gorm:"not null"                                     json:"land_stock_id"`
	ReservationID            *uint64         `json:"reservation_id,omitempty"`
	CustomerID               uint64          `gorm:"not null"                                     json:"customer_id"`
	SalesPersonID            *uint64         `json:"sales_person_id,omitempty"`
	QuantityM2               decimal.Decimal `gorm:"type:DECIMAL(20,4);not null"                  json:"quantity_m2"`
	UnitPriceSnapshot        domain.Money    `gorm:"type:DECIMAL(20,4);not null;default:'0.0000'" json:"unit_price_snapshot"`
	DPPAmount                domain.Money    `gorm:"type:DECIMAL(20,4);not null;default:'0.0000'" json:"dpp_amount"`
	IsPKP                    bool            `gorm:"column:is_pkp;not null;default:false"         json:"is_pkp"`
	VATRateSnapshot          decimal.Decimal `gorm:"type:DECIMAL(10,6);not null;default:'0.000000'" json:"vat_rate_snapshot"`
	GrossAmount              domain.Money    `gorm:"type:DECIMAL(20,4);not null;default:'0.0000'" json:"gross_amount"`
	PaymentAccountCode       string          `gorm:"size:20;not null;default:''"                  json:"payment_account_code"`
	RecognitionDate          *time.Time      `json:"recognition_date,omitempty"`
	Status                   LandSaleStatus  `gorm:"size:20;not null;default:'draft'"             json:"status"`
	CancelledAt              *time.Time      `json:"cancelled_at,omitempty"`
	CancelReason             string          `gorm:"size:500"                                     json:"cancel_reason,omitempty"`
	CancelledBy              *uint64         `json:"cancelled_by,omitempty"`
	RevenueJournalID         *uint64         `json:"revenue_journal_id,omitempty"`
	CogsJournalID            *uint64         `json:"cogs_journal_id,omitempty"`
	RevenueReversalJournalID *uint64         `json:"revenue_reversal_journal_id,omitempty"`
	CogsReversalJournalID    *uint64         `json:"cogs_reversal_journal_id,omitempty"`
	CreatedBy                *uint64         `json:"created_by,omitempty"`
	CreatedAt                time.Time       `json:"created_at"`
	UpdatedAt                time.Time       `json:"updated_at"`
}

func (LandSale) TableName() string { return "land_sales" }

// VATAmount — satu-satunya rumus PPN keluaran land_sales: DPP × rate,
// dibulatkan ke rupiah (pola identik sale.vatAmountOf).
func (s LandSale) VATAmount() domain.Money {
	if !s.IsPKP {
		return domain.Zero
	}
	return domain.FromDecimal(s.DPPAmount.Decimal().Mul(s.VATRateSnapshot).Round(0))
}

// LandAllocation is the immutable HPP snapshot for one LandSale (§B.4) —
// UNIQUE(tenant_id, land_sale_id), never updated after creation (R-2).
// BudgetPlanID/AllocationConfigVersionID nil means the actual-cost method
// was used instead of budgeted (implicit method encoding, no separate
// hpp_method column — pola identik allocation_snapshots).
type LandAllocation struct {
	ID                        uint64          `gorm:"primaryKey;autoIncrement"                     json:"id"`
	TenantID                  uint64          `gorm:"not null"                                     json:"-"`
	ProjectID                 uint64          `gorm:"not null"                                     json:"project_id"`
	LandSaleID                uint64          `gorm:"not null;uniqueIndex:uq_land_allocations_sale" json:"land_sale_id"`
	LandStockID               uint64          `gorm:"not null"                                     json:"land_stock_id"`
	QuantityM2                decimal.Decimal `gorm:"type:DECIMAL(20,4);not null"                  json:"quantity_m2"`
	HPPRatePerM2Snapshot      domain.Money    `gorm:"type:DECIMAL(20,4);not null;default:'0.0000'" json:"hpp_rate_per_m2_snapshot"`
	HPPTotal                  domain.Money    `gorm:"type:DECIMAL(20,4);not null;default:'0.0000'" json:"hpp_total"`
	Basis                     string          `gorm:"size:20;not null;default:'land_area'"         json:"basis"`
	AllocationConfigVersionID *uint64         `json:"allocation_config_version_id,omitempty"`
	AllocationConfigVersion   *int            `json:"allocation_config_version,omitempty"`
	BudgetPlanID              *uint64         `json:"budget_plan_id,omitempty"`
	BudgetPlanVersion         *int            `json:"budget_plan_version,omitempty"`
	CreatedAt                 time.Time       `json:"created_at"`
	UpdatedAt                 time.Time       `json:"updated_at"`
}

func (LandAllocation) TableName() string { return "land_allocations" }

package land

// LT-4 (kelebihan-tanah-final-architecture §B.2, §D, §J) — reservasi pool
// Kelebihan Tanah. Keputusan klien 2026-08-20: TANPA booking fee — reservasi
// murni soft-lock kuantitas (tidak ada transaksi finansial, tidak ada jurnal).
// Konversi ke land_sales (§D: reserve → convert → Akad) adalah tugas LT-5.

import (
	"time"

	"github.com/shopspring/decimal"

	"esaproperti/internal/domain"
)

// ReservationStatus — persis pola sale.BookingStatus (bookings.status CHECK).
type ReservationStatus string

const (
	ReservationStatusActive    ReservationStatus = "active"
	ReservationStatusConverted ReservationStatus = "converted"
	ReservationStatusExpired   ReservationStatus = "expired"
	ReservationStatusCancelled ReservationStatus = "cancelled"
)

// Valid returns true if s is a recognized ReservationStatus.
func (s ReservationStatus) Valid() bool {
	switch s {
	case ReservationStatusActive, ReservationStatusConverted, ReservationStatusExpired, ReservationStatusCancelled:
		return true
	}
	return false
}

// Terminal reports whether s is a terminal (immutable) state.
func (s ReservationStatus) Terminal() bool { return s != ReservationStatusActive }

// LandStockReservation is a soft-lock of quantity_m2 out of a project's
// land_stock pool for a customer, pending conversion to a land_sales (LT-5)
// or expiry/cancellation back to available.
type LandStockReservation struct {
	ID                uint64            `gorm:"primaryKey;autoIncrement"                     json:"id"`
	TenantID          uint64            `gorm:"not null"                                     json:"-"`
	LandStockID       uint64            `gorm:"not null"                                     json:"land_stock_id"`
	ProjectID         uint64            `gorm:"not null"                                     json:"project_id"`
	CustomerID        uint64            `gorm:"not null"                                     json:"customer_id"`
	SalesPersonID     *uint64           `json:"sales_person_id,omitempty"`
	QuantityM2        decimal.Decimal   `gorm:"type:DECIMAL(20,4);not null"                  json:"quantity_m2"`
	UnitPriceSnapshot domain.Money      `gorm:"type:DECIMAL(20,4);not null;default:'0.0000'" json:"unit_price_snapshot"`
	ReservedAt        time.Time         `json:"reserved_at"`
	ExpiryDate        *time.Time        `json:"expiry_date,omitempty"`
	Status            ReservationStatus `gorm:"size:20;not null;default:'active'"            json:"status"`
	ConvertedSaleID   *uint64           `json:"converted_sale_id,omitempty"`
	CancelledReason   string            `gorm:"size:500;not null;default:''"                 json:"cancelled_reason,omitempty"`
	CreatedBy         *uint64           `json:"created_by,omitempty"`
	CreatedAt         time.Time         `json:"created_at"`
	UpdatedAt         time.Time         `json:"updated_at"`
}

func (LandStockReservation) TableName() string { return "land_stock_reservations" }

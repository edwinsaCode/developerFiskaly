// Package land — Kelebihan Tanah (kelebihan-tanah-final-architecture-2026-08 §B).
//
// LT-3: land_stock, satu pool inventory Kelebihan Tanah per proyek. Murni
// tabel baru — tidak ada pembaca lain bergantung padanya pada increment ini.
// LT-4 (reservasi) dan LT-5 (penjualan + alokasi HPP) menyusul di package ini.
package land

import (
	"time"

	"github.com/shopspring/decimal"

	"esaproperti/internal/domain"
)

// LandStock adalah pool inventory Kelebihan Tanah untuk satu proyek.
//
// Kuantitas (m²) memakai decimal.Decimal — bukan uang. UnitPrice adalah
// rupiah per m² sehingga memakai domain.Money (Invariant #2).
//
// available_quantity_m2 TIDAK disimpan — selalu dihitung
// total - reserved - sold saat dibaca (lihat AvailableQuantityM2), supaya
// tidak ada dua sumber kebenaran untuk angka yang sama (SSOT).
type LandStock struct {
	ID                 uint64          `gorm:"primaryKey;autoIncrement"                     json:"id"`
	TenantID           uint64          `gorm:"not null"                                     json:"-"`
	ProjectID          uint64          `gorm:"not null"                                     json:"project_id"`
	ProductCode        string          `gorm:"size:64;not null;default:'kelebihan_tanah'"   json:"product_code"`
	TotalQuantityM2    decimal.Decimal `gorm:"type:DECIMAL(20,4);not null;default:'0.0000'" json:"total_quantity_m2"`
	ReservedQuantityM2 decimal.Decimal `gorm:"type:DECIMAL(20,4);not null;default:'0.0000'" json:"reserved_quantity_m2"`
	SoldQuantityM2     decimal.Decimal `gorm:"type:DECIMAL(20,4);not null;default:'0.0000'" json:"sold_quantity_m2"`
	// UnitPrice (Harga Jual) dipakai di reservasi/Akad/DPP (pendapatan).
	// PurchasePrice (Harga Beli) adalah tarif HPP per m² — dipakai LANGSUNG
	// oleh PurchasePriceLandHPPResolver saat Akad (koreksi klien 2026-08-31;
	// lihat hpp_resolver.go). Bukan lagi murni referensi.
	UnitPrice     domain.Money `gorm:"type:DECIMAL(20,4);not null;default:'0.0000'" json:"unit_price"`
	PurchasePrice domain.Money `gorm:"type:DECIMAL(20,4);not null;default:'0.0000'" json:"purchase_price"`
	CreatedAt     time.Time    `json:"created_at"`
	UpdatedAt     time.Time    `json:"updated_at"`
}

func (LandStock) TableName() string { return "land_stock" }

// AvailableQuantityM2 — total - reserved - sold, dihitung saat dibaca (SSOT).
func (l LandStock) AvailableQuantityM2() decimal.Decimal {
	return l.TotalQuantityM2.Sub(l.ReservedQuantityM2).Sub(l.SoldQuantityM2)
}

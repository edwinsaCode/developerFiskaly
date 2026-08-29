package scheme

import "time"

// PaymentScheme adalah master konfigurasi skema pembayaran per tenant (pola Tax
// Rule). `code` IMMUTABLE setelah dibuat (by construction: update request tidak
// punya field Code — pola customers.code). Mengubah `params` master TIDAK
// memengaruhi kontrak berjalan (kontrak membaca snapshot-nya sendiri).
type PaymentScheme struct {
	ID         uint64     `gorm:"primaryKey;autoIncrement" json:"id"`
	TenantID   uint64     `gorm:"not null;index"           json:"-"`
	Code       string     `gorm:"not null;size:30"         json:"code"` // immutable
	Name       string     `gorm:"not null;size:100"        json:"name"`
	PolicyType PolicyType `gorm:"not null;size:30"         json:"policy_type"`
	Params     string     `gorm:"type:json;not null"       json:"params"` // JSON Params
	IsActive   bool       `gorm:"not null;default:true"    json:"is_active"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
}

func (PaymentScheme) TableName() string { return "payment_schemes" }

// FinancingSourceType mengklasifikasikan bank penyalur. BUKAN penentu pajak —
// tarif tetap dari Project.tax_category (blueprint §7).
type FinancingSourceType string

const (
	FinSourceKPRSubsidi   FinancingSourceType = "bank_kpr_subsidi"
	FinSourceKPRKomersial FinancingSourceType = "bank_kpr_komersial"
	FinSourceLainnya      FinancingSourceType = "lainnya"
)

func (t FinancingSourceType) Valid() bool {
	switch t {
	case FinSourceKPRSubsidi, FinSourceKPRKomersial, FinSourceLainnya:
		return true
	}
	return false
}

// FinancingSource adalah master bank penyalur pembiayaan (mengubur bank_kpr
// free-text — BS-3). `code` immutable.
type FinancingSource struct {
	ID        uint64              `gorm:"primaryKey;autoIncrement" json:"id"`
	TenantID  uint64              `gorm:"not null;index"           json:"-"`
	Code      string              `gorm:"not null;size:30"         json:"code"` // immutable
	Name      string              `gorm:"not null;size:200"        json:"name"`
	Type      FinancingSourceType `gorm:"not null;size:30"         json:"type"`
	IsActive  bool                `gorm:"not null;default:true"    json:"is_active"`
	CreatedAt time.Time           `json:"created_at"`
	UpdatedAt time.Time           `json:"updated_at"`
}

func (FinancingSource) TableName() string { return "financing_sources" }

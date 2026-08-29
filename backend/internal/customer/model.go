// Package customer memodelkan Customer/Buyer master (CORE) — pihak pembeli unit.
// Master murni: tidak menyentuh ledger, tidak memposting jurnal.
package customer

import "time"

// CustomerType membedakan perorangan vs badan usaha.
type CustomerType string

const (
	CustomerTypeIndividual CustomerType = "individual"
	CustomerTypeCompany    CustomerType = "company"
)

func (t CustomerType) Valid() bool {
	return t == CustomerTypeIndividual || t == CustomerTypeCompany
}

// Customer adalah master pembeli. `code` unik per tenant. Menggantikan buyer_ref
// free-text; ditautkan ke sale_contracts lewat seam nullable customer_id.
type Customer struct {
	ID       uint64 `gorm:"primaryKey;autoIncrement" json:"id"`
	TenantID uint64 `gorm:"not null;index" json:"-"`
	// Code IMMUTABLE setelah dibuat — identitas master. Tidak ada jalur update
	// yang mengubahnya (UpdateCustomerRequest tak punya field Code).
	Code string       `gorm:"not null;size:50"                      json:"code"`
	Name string       `gorm:"not null;size:200"                     json:"name"`
	Type CustomerType `gorm:"not null;size:20;default:'individual'" json:"type"` // individual | company
	// Segment: dimensi reporting BISNIS (subsidi/komersial/investor/corporate).
	// RESERVED SEAM — belum dipakai logika; untuk segmentasi/reporting masa depan.
	Segment   string    `gorm:"size:30"            json:"segment,omitempty"`
	IDNumber  string    `gorm:"size:50"           json:"id_number,omitempty"` // NIK/KTP
	NPWP      string    `gorm:"size:30"           json:"npwp,omitempty"`
	Phone     string    `gorm:"size:30"           json:"phone,omitempty"`
	Email     string    `gorm:"size:120"          json:"email,omitempty"`
	Address   string    `gorm:"size:500"          json:"address,omitempty"`
	IsActive  bool      `gorm:"not null;default:1" json:"is_active"` // roadmap: → status enum (Active/Inactive/Resigned)
	CreatedBy *uint64   `                          json:"created_by,omitempty"`
	CreatedAt time.Time `                          json:"created_at"`
	UpdatedAt time.Time `                          json:"updated_at"`
}

func (Customer) TableName() string { return "customers" }

// ── Request types (domain-level; DTO JSON ada di handler) ──────────────────────

type CreateCustomerRequest struct {
	Code      string
	Name      string
	Type      CustomerType
	Segment   string
	IDNumber  string
	NPWP      string
	Phone     string
	Email     string
	Address   string
	CreatedBy *uint64
}

// UpdateCustomerRequest: field string kosong = tidak diubah; IsActive nil = tidak
// diubah. Code SENGAJA tidak ada di sini — immutable (identitas master).
type UpdateCustomerRequest struct {
	Name     string
	Type     CustomerType
	Segment  string
	IDNumber string
	NPWP     string
	Phone    string
	Email    string
	Address  string
	IsActive *bool
}

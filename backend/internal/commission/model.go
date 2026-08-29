// Package commission mengimplementasikan Increment 9 — Commission Engine
// (blueprint §3): Rule (config) → Commission (entri per sale) → Approval →
// Payment → Journal, + clawback bila sale dibatalkan (§1).
//
// Ledger (derived, append-only):
//
//	payable  : Dr Beban Komisi / Cr Utang Komisi (akrual)
//	paid     : Dr Utang Komisi / Cr Bank
//	cancel   : reversing akrual (bila sudah payable)
//	clawback : Dr Piutang Lain-lain 1-2100 / Cr Beban Komisi (recovery, sale batal
//	           setelah komisi dibayar)
package commission

import (
	"time"

	"github.com/shopspring/decimal"

	"esaproperti/internal/domain"
)

// ── Rule ──────────────────────────────────────────────────────────────────────

type Basis string

const (
	BasisPercentOfSale Basis = "percent_of_sale"
	BasisFlatPerUnit   Basis = "flat_per_unit"
	// BasisTiered: vocabulary SEAM (blueprint §3 "tiered") — CHECK DB sudah
	// menampung; engine menolak sampai formula terdaftar (pola TaxFormula).
	BasisTiered Basis = "tiered"
)

func (b Basis) Valid() bool {
	return b == BasisPercentOfSale || b == BasisFlatPerUnit || b == BasisTiered
}

type Trigger string

const (
	TriggerAtBAST Trigger = "at_bast"
	// SEAM: at_collection (komisi cair mengikuti collection) & at_lunas —
	// vocabulary dipesan, engine kalkulasi baru mendukung at_bast.
	TriggerAtCollection Trigger = "at_collection"
	TriggerAtLunas      Trigger = "at_lunas"
)

func (t Trigger) Valid() bool {
	return t == TriggerAtBAST || t == TriggerAtCollection || t == TriggerAtLunas
}

// Rule adalah konfigurasi komisi. Scope NULL = berlaku untuk semua
// (salesperson/proyek/tipe unit). Provenance (rate) di-snapshot ke entri.
type Rule struct {
	ID            uint64          `gorm:"primaryKey;autoIncrement" json:"id"`
	TenantID      uint64          `gorm:"not null;index"           json:"-"`
	Name          string          `gorm:"size:200;not null"        json:"name"`
	Basis         Basis           `gorm:"size:20;not null"         json:"basis"`
	Rate          decimal.Decimal `gorm:"type:DECIMAL(10,6);not null;default:'0.000000'" json:"rate"`
	FlatAmount    domain.Money    `gorm:"type:DECIMAL(20,4);not null;default:'0.0000'"   json:"flat_amount"`
	TriggerEvent  Trigger         `gorm:"size:20;not null;default:'at_bast'"             json:"trigger_event"`
	SalesPersonID *uint64         `json:"sales_person_id,omitempty"`
	ProjectID     *uint64         `json:"project_id,omitempty"`
	UnitType      *string         `gorm:"size:50" json:"unit_type,omitempty"`
	ExpenseAccount string         `gorm:"size:20;not null;default:'5-3100'" json:"expense_account"`
	PayableAccount string         `gorm:"size:20;not null;default:'2-6200'" json:"payable_account"`
	EffectiveFrom  time.Time      `gorm:"type:date;not null" json:"effective_from"`
	EffectiveTo    *time.Time     `gorm:"type:date"          json:"effective_to,omitempty"`
	IsActive       bool           `gorm:"not null;default:true" json:"is_active"`
	CreatedBy      *uint64        `json:"created_by,omitempty"`
	CreatedAt      time.Time      `json:"created_at"`
	UpdatedAt      time.Time      `json:"updated_at"`
}

func (Rule) TableName() string { return "commission_rules" }

// Matches: apakah rule berlaku untuk satu penjualan (scope + tanggal efektif).
func (r *Rule) Matches(salesPersonID, projectID uint64, unitType string, recognitionDate time.Time) bool {
	if !r.IsActive {
		return false
	}
	if recognitionDate.Before(r.EffectiveFrom) {
		return false
	}
	if r.EffectiveTo != nil && recognitionDate.After(*r.EffectiveTo) {
		return false
	}
	if r.SalesPersonID != nil && *r.SalesPersonID != salesPersonID {
		return false
	}
	if r.ProjectID != nil && *r.ProjectID != projectID {
		return false
	}
	if r.UnitType != nil && *r.UnitType != unitType {
		return false
	}
	return true
}

// AmountFor menghitung nominal komisi dari basis penjualan (rupiah bulat).
func (r *Rule) AmountFor(basisAmount domain.Money) (domain.Money, error) {
	switch r.Basis {
	case BasisPercentOfSale:
		return domain.FromDecimal(basisAmount.Decimal().Mul(r.Rate).Round(0)), nil
	case BasisFlatPerUnit:
		return r.FlatAmount, nil
	default:
		return domain.Zero, ErrBasisNotImplemented
	}
}

// ── Commission entry ──────────────────────────────────────────────────────────

type Status string

const (
	StatusCalculated Status = "calculated"
	StatusApproved   Status = "approved"
	StatusPayable    Status = "payable"
	StatusPaid       Status = "paid"
	StatusCancelled  Status = "cancelled"   // terminal
	StatusClawedBack Status = "clawed_back" // terminal (sale batal setelah dibayar)
)

// CanTransitionTo (blueprint §3): calculated→approved→payable→paid;
// cancelled dari calculated|approved|payable; clawed_back hanya dari paid.
func (s Status) CanTransitionTo(next Status) bool {
	switch s {
	case StatusCalculated:
		return next == StatusApproved || next == StatusCancelled
	case StatusApproved:
		return next == StatusPayable || next == StatusCancelled
	case StatusPayable:
		return next == StatusPaid || next == StatusCancelled
	case StatusPaid:
		return next == StatusClawedBack
	}
	return false
}

func (s Status) Terminal() bool { return s == StatusCancelled || s == StatusClawedBack }

// Commission adalah entri komisi satu sale × rule × salesperson.
type Commission struct {
	ID             uint64  `gorm:"primaryKey;autoIncrement" json:"id"`
	TenantID       uint64  `gorm:"not null;index"           json:"-"`
	RuleID         uint64  `gorm:"not null"                 json:"rule_id"`
	SaleRecordID   uint64  `gorm:"not null"                 json:"sale_record_id"`
	SaleContractID *uint64 `json:"sale_contract_id,omitempty"`
	ProjectID      uint64  `gorm:"not null"                 json:"project_id"`
	UnitID         uint64  `gorm:"not null"                 json:"unit_id"`
	SalesPersonID  uint64  `gorm:"not null"                 json:"sales_person_id"`

	BasisAmount  domain.Money    `gorm:"type:DECIMAL(20,4);not null;default:'0.0000'"   json:"basis_amount"`
	RateSnapshot decimal.Decimal `gorm:"type:DECIMAL(10,6);not null;default:'0.000000'" json:"rate_snapshot"`
	Amount       domain.Money    `gorm:"type:DECIMAL(20,4);not null;default:'0.0000'"   json:"amount"`
	Status       Status          `gorm:"size:20;not null;default:'calculated'"          json:"status"`

	AccrualJournalID  *uint64 `json:"accrual_journal_id,omitempty"`
	PaymentJournalID  *uint64 `json:"payment_journal_id,omitempty"`
	ClawbackJournalID *uint64 `json:"clawback_journal_id,omitempty"`
	BankAccountCode   string  `gorm:"size:20" json:"bank_account_code,omitempty"`

	CalculatedAt *time.Time `json:"calculated_at,omitempty"`
	ApprovedAt   *time.Time `json:"approved_at,omitempty"`
	ApprovedBy   *uint64    `json:"approved_by,omitempty"`
	PayableAt    *time.Time `json:"payable_at,omitempty"`
	PayableBy    *uint64    `json:"payable_by,omitempty"`
	PaidAt       *time.Time `json:"paid_at,omitempty"`
	PaidBy       *uint64    `json:"paid_by,omitempty"`
	CancelledAt  *time.Time `json:"cancelled_at,omitempty"`
	CancelledBy  *uint64    `json:"cancelled_by,omitempty"`
	CancelReason string     `gorm:"size:500" json:"cancel_reason,omitempty"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (Commission) TableName() string { return "commissions" }

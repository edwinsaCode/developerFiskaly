package ledger

import (
	"time"

	"esaproperti/internal/domain"
)

// Account represents a single entry in the Chart of Accounts.
// Invariant: Code is unique per tenant.
type Account struct {
	ID            uint64               `gorm:"primaryKey;autoIncrement"                  json:"id"`
	TenantID      uint64               `gorm:"not null;index;default:0"                  json:"-"`
	Code          string               `gorm:"not null;size:20"                          json:"code"`
	Name          string               `gorm:"not null;size:200"                         json:"name"`
	Type          domain.AccountType   `gorm:"not null;size:20"                          json:"type"`
	NormalBalance domain.NormalBalance `gorm:"column:normal_balance;not null;size:10"    json:"normal_balance"`
	IsSystem      bool                 `gorm:"not null;default:false"                    json:"is_system"`
	IsActive      bool                 `gorm:"not null;default:true"                     json:"is_active"`
	Category      AccountCategory      `gorm:"column:category;size:20"                   json:"account_category,omitempty"`
	Description   string               `gorm:"size:500"                                  json:"description,omitempty"`
	CreatedAt     time.Time            `json:"created_at"`
	UpdatedAt     time.Time            `json:"updated_at"`
}

func (Account) TableName() string { return "accounts" }

// JournalEntry is the header of a double-entry journal.
// Once PostedAt is set it is immutable (Invariant #5).
type JournalEntry struct {
	ID          uint64     `gorm:"primaryKey;autoIncrement"                json:"id"`
	TenantID    uint64     `gorm:"not null;index;default:0"                json:"-"`
	Date        time.Time  `gorm:"not null"                                json:"date"`
	Description string     `gorm:"not null;size:500"                       json:"description"`
	Reference   string     `gorm:"size:100;default:''"                     json:"reference,omitempty"`
	PostedAt    *time.Time `gorm:"index"                                   json:"posted_at,omitempty"`
	IsReversing bool       `gorm:"not null;default:false"                  json:"is_reversing"`
	ReversesID  *uint64    `gorm:"index"                                   json:"reverses_id,omitempty"`
	// Source identifies the originating subsystem: "manual", "cost", "sale", "tax", "termin", "reversal", "system".
	Source string `gorm:"not null;size:20;default:'system'"       json:"source"`
	// DocumentID menautkan jurnal ke dokumen bernomor yang membuktikannya
	// (INV-DOC-1, W-3.1). Wajib untuk setiap jurnal yang menyentuh akun
	// kas/bank kecuali saldo awal; NULL untuk jurnal non-kas.
	//
	// Logical FK ke `documents` — ledger tidak import package document, supaya
	// arah ketergantungan tetap satu arah (dokumen boleh tahu ledger, tidak
	// sebaliknya).
	DocumentID *uint64       `gorm:"column:document_id"                      json:"document_id,omitempty"`
	CreatedBy  *uint64       `gorm:"index"                                   json:"created_by,omitempty"`
	Lines      []JournalLine `gorm:"foreignKey:JournalEntryID"               json:"lines,omitempty"`
	CreatedAt  time.Time     `json:"created_at"`
	UpdatedAt  time.Time     `json:"updated_at"`
}

func (JournalEntry) TableName() string { return "journal_entries" }

// IsPosted returns true if this journal has been posted.
func (e *JournalEntry) IsPosted() bool { return e.PostedAt != nil }

// JournalLine is one side of a double-entry line within a JournalEntry.
// Exactly one of Debit or Credit must be non-zero (validated by PostingService).
type JournalLine struct {
	ID             uint64       `gorm:"primaryKey;autoIncrement"                               json:"id"`
	TenantID       uint64       `gorm:"not null;index;default:0"                               json:"-"`
	JournalEntryID uint64       `gorm:"not null;index"                                         json:"journal_entry_id"`
	AccountID      uint64       `gorm:"not null;index"                                         json:"account_id"`
	Account        *Account     `gorm:"foreignKey:AccountID"                                   json:"account,omitempty"`
	Debit          domain.Money `gorm:"type:DECIMAL(20,4);not null;default:'0.0000'"           json:"debit"`
	Credit         domain.Money `gorm:"type:DECIMAL(20,4);not null;default:'0.0000'"           json:"credit"`
	ProjectID      *uint64      `gorm:"index"                                                  json:"project_id,omitempty"`
	PhaseID        *uint64      `gorm:"index"                                                  json:"phase_id,omitempty"`
	UnitID         *uint64      `gorm:"index"                                                  json:"unit_id,omitempty"`
	Description    string       `gorm:"size:500;default:''"                                    json:"description,omitempty"`
	CreatedAt      time.Time    `json:"created_at"`
}

func (JournalLine) TableName() string { return "journal_lines" }

// AccountingPeriod represents one calendar month's accounting period for a tenant.
// Status transitions: open → closed. Corrections via reopen (owner only).
// Guard: PostingService rejects journals with dates in closed periods.
type AccountingPeriod struct {
	ID        uint64     `gorm:"primaryKey;autoIncrement"          json:"id"`
	TenantID  uint64     `gorm:"not null;index"                    json:"-"`
	Year      int        `gorm:"not null"                          json:"year"`
	Month     int        `gorm:"not null"                          json:"month"`
	Status    string     `gorm:"not null;size:10;default:'open'"   json:"status"` // "open" | "closed"
	ClosedAt  *time.Time `gorm:"index"                             json:"closed_at,omitempty"`
	ClosedBy  *uint64    `json:"closed_by,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
}

func (AccountingPeriod) TableName() string { return "accounting_periods" }

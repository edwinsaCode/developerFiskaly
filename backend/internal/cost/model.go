package cost

import (
	"time"

	"esaproperti/internal/domain"
)

// PaymentMethod describes how the cost was settled or will be settled.
type PaymentMethod string

const (
	// PaymentMethodBank means the cost was paid immediately via a bank account.
	// Cr: the bank account specified by BankAccountCode (e.g. "1-1300").
	PaymentMethodBank PaymentMethod = "bank"

	// PaymentMethodPayable means the cost was invoiced but not yet paid.
	// Cr: 2-1000 Hutang Usaha.
	PaymentMethodPayable PaymentMethod = "payable"
)

// Valid returns true if p is a recognized PaymentMethod.
func (p PaymentMethod) Valid() bool {
	return p == PaymentMethodBank || p == PaymentMethodPayable
}

// CostEntry records one development cost, classified by CostTier (Increment 2 —
// blueprint-domain-additions.md §7):
//
//	direct   → kapitalisasi milik SATU unit (unit_id WAJIB): Dr Persediaan 1-3xxx.
//	shared   → kapitalisasi pool proyek (unit_id NULL): Dr Persediaan 1-3xxx,
//	           dialokasikan ke unit via basis → HPP.
//	overhead → beban periode (marketing/other): Dr Beban 5-3000/5-4000. TIDAK
//	           dikapitalisasi, TIDAK masuk HPP. project_id opsional (cost center).
//
// The authoritative financial record is the associated JournalEntry;
// the amount field here is a convenient copy for display/listing.
//
// Invariant: amount must be whole rupiah (Invariant #2).
// Invariant: tier direct/shared TIDAK masuk akun beban 5-xxxx — hanya Persediaan
// 1-3xxx; tier overhead TIDAK masuk Persediaan — hanya beban 5-xxxx.
// Invariant: baris jurnal ber-tag project_id bila entry punya project;
// project_id NULL hanya sah untuk tier overhead (biaya Tenant-level).
type CostEntry struct {
	ID              uint64              `gorm:"primaryKey;autoIncrement"                     json:"id"`
	TenantID        uint64              `gorm:"not null;index"                               json:"-"`
	ProjectID       *uint64             `gorm:"index"                                        json:"project_id,omitempty"`
	UnitID          *uint64             `gorm:"index"                                        json:"unit_id,omitempty"`
	PhaseID         *uint64             `gorm:"index"                                        json:"phase_id,omitempty"`
	Category        domain.CostCategory `gorm:"not null;size:20"                             json:"category"`
	// HardSubcategory (UAT 2026-09-07): WAJIB diisi saat Category=Hard dan
	// CostTier=Shared (project-wide pool ambigu tanpa ini — lihat
	// service.validate). Opsional untuk CostTier=Direct (unit-nya sendiri
	// sudah menentukan Subsidi/Komersial tanpa ambiguitas pool). NULL untuk
	// semua baris historis pra-fitur ini dan untuk kategori selain Hard.
	HardSubcategory domain.ConstructionSubcategory `gorm:"size:30"                             json:"hard_subcategory,omitempty"`
	// ExpenseTypeID (W-10): terisi HANYA untuk pengeluaran operasional, dan
	// menjadi penanda bahwa akun debit baris ini berasal dari master
	// expense_types — bukan dari taksonomi Category. NULL = jalur biaya proyek
	// seperti sebelumnya (seluruh baris historis tetap NULL).
	ExpenseTypeID   *uint64             `gorm:"index"                                        json:"expense_type_id,omitempty"`
	CostTier        domain.CostTier     `gorm:"not null;size:20"                             json:"cost_tier"`
	Amount          domain.Money        `gorm:"type:DECIMAL(20,4);not null;default:'0.0000'" json:"amount"`
	PaymentMethod   PaymentMethod       `gorm:"not null;size:20"                             json:"payment_method"`
	BankAccountCode string              `gorm:"size:20"                                      json:"bank_account_code,omitempty"`
	Date            time.Time           `gorm:"not null"                                     json:"date"`
	Vendor          string              `gorm:"size:200"                                     json:"vendor"`
	Description     string              `gorm:"size:500"                                     json:"description"`
	JournalEntryID  uint64              `gorm:"not null;index"                               json:"journal_entry_id"`
	BudgetItemID    *uint64             `gorm:"index"                                        json:"budget_item_id,omitempty"`
	// W-11 — provenance hutang usaha. Keduanya NULL untuk seluruh baris yang
	// lahir dari jalur biaya langsung (kas/bank), termasuk semua baris historis:
	// tidak ada backfill, tidak ada perubahan makna kolom lama.
	//
	// Kolom `Vendor` (teks bebas) SENGAJA dipertahankan: ia adalah apa yang
	// tertulis di dokumen saat itu. VendorID adalah tautan ke master — kalau
	// nama vendor di master kelak diubah, bukti historis tidak ikut berubah.
	// Nama kolom APInvoiceID ditulis EKSPLISIT. Penamaan otomatis GORM
	// menerjemahkan "APInvoiceID" menjadi `apinvoice_id` (initialism "AP"
	// tidak dipecah), yang tidak sama dengan kolom `ap_invoice_id` di migrasi —
	// dan selisih itu baru muncul sebagai "Unknown column" saat runtime.
	VendorID        *uint64             `gorm:"index"                                        json:"vendor_id,omitempty"`
	APInvoiceID     *uint64             `gorm:"index;column:ap_invoice_id"                   json:"ap_invoice_id,omitempty"`
	CreatedAt       time.Time           `json:"created_at"`
	UpdatedAt       time.Time           `json:"updated_at"`
}

func (CostEntry) TableName() string { return "cost_entries" }

// JournalLineInput is the per-line request type for the JournalWriter interface.
// Mirrors ledger.LineInput without importing the ledger package.
type JournalLineInput struct {
	AccountID   uint64
	Debit       domain.Money
	Credit      domain.Money
	ProjectID   *uint64
	PhaseID     *uint64
	UnitID      *uint64
	Description string
}

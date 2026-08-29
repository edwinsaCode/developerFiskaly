package billing

import (
	"time"

	"esaproperti/internal/domain"
)

// ── Invoice types and statuses ────────────────────────────────────────────────

type InvoiceType   string
type InvoiceStatus string

const (
	TypeDP        InvoiceType = "DP"
	TypeTermin    InvoiceType = "TERMIN"
	TypePelunasan InvoiceType = "PELUNASAN"
	// TypeKekurangan (R1): tagihan sisa pembayaran pasca-pencairan bank KPR —
	// nominal SELALU dari ContractFinancialSummary.Outstanding (satu rumus).
	TypeKekurangan InvoiceType = "KEKURANGAN"
	// TypeRealisasi (Billing Batch 2): tagihan Biaya Realisasi / addon per
	// charge group — nominal SELALU dari outstanding kanonik grup (dipasok
	// pemanggil charge.Service; billing tidak menghitung sendiri). Maksimal
	// satu invoice REALISASI unpaid per grup (pola KEKURANGAN).
	TypeRealisasi InvoiceType = "REALISASI"

	StatusIssued    InvoiceStatus = "issued"
	StatusPaid      InvoiceStatus = "paid"
	StatusOverdue   InvoiceStatus = "overdue"
	StatusCancelled InvoiceStatus = "cancelled"
)

// ── Invoice ───────────────────────────────────────────────────────────────────

type Invoice struct {
	ID             uint64        `gorm:"primaryKey;autoIncrement"                         json:"id"`
	TenantID       uint64        `gorm:"not null;index"                                   json:"-"`
	SaleContractID uint64        `gorm:"not null;index"                                   json:"sale_contract_id"`
	ScheduleID     *uint64       `gorm:"index"                                            json:"schedule_id,omitempty"`
	// ChargeGroupID (Billing Batch 2): grup tagihan realisasi/addon yang
	// ditagih invoice REALISASI ini. NULL untuk invoice harga rumah.
	ChargeGroupID *uint64 `gorm:"index"                                            json:"charge_group_id,omitempty"`
	InvoiceNumber  string        `gorm:"not null;size:30"                                 json:"invoice_number"`
	InvoiceType    InvoiceType   `gorm:"not null;size:20"                                 json:"invoice_type"`
	IssueDate      time.Time     `gorm:"not null;type:date"                               json:"issue_date"`
	DueDate        time.Time     `gorm:"not null;type:date"                               json:"due_date"`
	Amount         domain.Money  `gorm:"type:DECIMAL(20,4);not null;default:'0.0000'"     json:"amount"`
	Status         InvoiceStatus `gorm:"not null;size:20;default:'issued'"                json:"status"`
	Notes          string        `gorm:"type:text"                                        json:"notes,omitempty"`
	CreatedBy      uint64        `gorm:"not null"                                         json:"created_by"`
	CreatedAt      time.Time     `                                                        json:"created_at"`
	UpdatedAt      time.Time     `                                                        json:"updated_at"`
}

func (Invoice) TableName() string { return "invoices" }

// W-2: model `InvoiceSequence` DIHAPUS — lihat catatan di receipt.go. Satu
// mesin penomoran untuk seluruh dokumen; invoice tidak lagi punya seri sendiri.

// ── Data loaded from sale tables (no import of sale package) ─────────────────

// ContractInfo is the minimal sale contract data the billing service needs.
// Loaded from sale_contracts table directly — avoids circular import with sale package.
type ContractInfo struct {
	ID           uint64
	BuyerName    string
	BuyerID      string
	ContractDate time.Time
	GrossAmount  domain.Money
	DPPAmount    domain.Money
	IsPKP        bool
}

// ScheduleInfo is the minimal payment schedule data the billing service needs.
type ScheduleInfo struct {
	ID             uint64
	SaleContractID uint64
	UnitID         uint64
	Amount         domain.Money
	DueDate        time.Time
	Type           string // "dp" | "installment" | "final"
	Status         string // "scheduled" | "received" | "overdue"
}

// ── Request types ─────────────────────────────────────────────────────────────

type GenerateInvoiceRequest struct {
	ScheduleID uint64
	IssueDate  time.Time // optional; defaults to now
	Notes      string
}

// InvoiceSummary extends Invoice with joined buyer/unit data for list views.
type InvoiceSummary struct {
	Invoice
	BuyerName string `json:"buyer_name"`
	UnitCode  string `json:"unit_code"`
}

// scheduleTypeToInvoiceType maps payment schedule type to invoice type.
func scheduleTypeToInvoiceType(t string) InvoiceType {
	switch t {
	case "dp":
		return TypeDP
	case "final":
		return TypePelunasan
	default:
		return TypeTermin
	}
}

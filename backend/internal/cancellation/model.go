// Package cancellation mengimplementasikan Increment 8 — Cancellation & Refund
// (blueprint §1, CORE). Membatalkan penjualan pra/pasca-BAST secara benar
// (state + akuntansi) dan membayarkan refund, append-only via jurnal
// baru/pembalik (Invariant #5).
//
// Lifecycle (blueprint): requested → approved → processed | rejected.
//
// Akuntansi:
//
//	pra-BAST  : Dr Uang Muka 2-2000 (Σ diterima unit, derived dari ledger)
//	            / Cr Pendapatan Lain 4-2000 (penalti)
//	            / Cr Hutang Refund 2-2200 (sisa)
//	pasca-BAST: (1) reverse Event 3 (pendapatan) — ledger.Reverse
//	            (2) reverse SELURUH COGS unit — mirror himpunan jurnal COGS
//	                ter-refer (Event 4 + porsi true-up ber-tag unit): INV-COGS-SUM
//	            (3) reverse akrual PPh Final (obligation belum dibayar)
//	            (4) settlement Uang Muka spt pra-BAST (Event 3 reversal sudah
//	                mengembalikan uang buyer ke 2-2000)
//	refund    : Dr 2-2200 / Cr Bank. Sumber lain: booking fee pending_refund
//	            (Increment 7) → reklas Dr 2-2100 / Cr 2-2200 lalu dibayar.
package cancellation

import (
	"time"

	"esaproperti/internal/domain"
)

// ── Cancellation ──────────────────────────────────────────────────────────────

type Status string

const (
	StatusRequested Status = "requested"
	StatusApproved  Status = "approved"
	StatusRejected  Status = "rejected"  // terminal
	StatusProcessed Status = "processed" // terminal
)

// CanTransitionTo: requested→approved|rejected; approved→processed|rejected.
func (s Status) CanTransitionTo(next Status) bool {
	switch s {
	case StatusRequested:
		return next == StatusApproved || next == StatusRejected
	case StatusApproved:
		return next == StatusProcessed || next == StatusRejected
	}
	return false
}

func (s Status) Terminal() bool { return s == StatusRejected || s == StatusProcessed }

type Stage string

const (
	StagePreBAST  Stage = "pre_bast"
	StagePostBAST Stage = "post_bast"
)

// Cancellation adalah dokumen pembatalan penjualan satu unit.
// Satu cancellation AKTIF per unit (UNIQUE active_key).
type Cancellation struct {
	ID             uint64  `gorm:"primaryKey;autoIncrement" json:"id"`
	TenantID       uint64  `gorm:"not null;index"           json:"-"`
	ProjectID      uint64  `gorm:"not null"                 json:"project_id"`
	UnitID         uint64  `gorm:"not null;index"           json:"unit_id"`
	SaleContractID *uint64 `json:"sale_contract_id,omitempty"`
	SaleRecordID   *uint64 `json:"sale_record_id,omitempty"`
	Stage          Stage   `gorm:"size:10;not null"         json:"stage"`
	Reason         string  `gorm:"size:500"                 json:"reason"`
	// EventDate = tanggal kejadian pembatalan (jurnal reversal/settlement memakai
	// tanggal ini; D4 spirit: periode berjalan, period-checker menjaga).
	EventDate     time.Time    `gorm:"not null"                                     json:"event_date"`
	Penalty       domain.Money `gorm:"type:DECIMAL(20,4);not null;default:'0.0000'" json:"penalty"`
	ReceivedTotal domain.Money `gorm:"type:DECIMAL(20,4);not null;default:'0.0000'" json:"received_total"`
	RefundAmount  domain.Money `gorm:"type:DECIMAL(20,4);not null;default:'0.0000'" json:"refund_amount"`
	Status        Status       `gorm:"size:20;not null;default:'requested'"         json:"status"`

	RequestedBy  *uint64    `json:"requested_by,omitempty"`
	ApprovedAt   *time.Time `json:"approved_at,omitempty"`
	ApprovedBy   *uint64    `json:"approved_by,omitempty"`
	RejectedAt   *time.Time `json:"rejected_at,omitempty"`
	RejectedBy   *uint64    `json:"rejected_by,omitempty"`
	RejectReason string     `gorm:"size:500" json:"reject_reason,omitempty"`
	ProcessedAt  *time.Time `json:"processed_at,omitempty"`
	ProcessedBy  *uint64    `json:"processed_by,omitempty"`

	RevenueReversalJournalID *uint64 `json:"revenue_reversal_journal_id,omitempty"`
	COGSReversalJournalID    *uint64 `json:"cogs_reversal_journal_id,omitempty"`
	TaxReversalJournalID     *uint64 `json:"tax_reversal_journal_id,omitempty"`
	SettlementJournalID      *uint64 `json:"settlement_journal_id,omitempty"`
	RefundID                 *uint64 `json:"refund_id,omitempty"`

	// kelebihan-tanah-booking-integration-2026-08: bila kontrak unit ini punya
	// komponen Kelebihan Tanah bundled (booking-embedded), pembalikannya ikut
	// terjadi di dalam Process yang sama (land.CancelLandSaleTx) — kolom ini
	// murni jejak audit sisi cancellation; land_sales sendiri menyimpan status +
	// reversal journal id-nya masing-masing (lihat internal/land).
	LandSaleID                   *uint64 `json:"land_sale_id,omitempty"`
	LandRevenueReversalJournalID *uint64 `json:"land_revenue_reversal_journal_id,omitempty"`
	LandCOGSReversalJournalID    *uint64 `json:"land_cogs_reversal_journal_id,omitempty"`

	ActiveKey *string   `gorm:"size:1" json:"-"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (Cancellation) TableName() string { return "cancellations" }

// ── Refund ────────────────────────────────────────────────────────────────────

type RefundStatus string

const (
	RefundPending   RefundStatus = "pending"
	RefundPaid      RefundStatus = "paid"
	RefundCancelled RefundStatus = "cancelled"
)

type RefundSource string

const (
	RefundFromCancellation RefundSource = "cancellation"
	RefundFromBooking      RefundSource = "booking"
)

// Refund adalah dokumen pembayaran pengembalian dana buyer.
type Refund struct {
	ID             uint64       `gorm:"primaryKey;autoIncrement" json:"id"`
	TenantID       uint64       `gorm:"not null;index"           json:"-"`
	SourceType     RefundSource `gorm:"size:20;not null"         json:"source_type"`
	CancellationID *uint64      `json:"cancellation_id,omitempty"`
	BookingID      *uint64      `json:"booking_id,omitempty"`
	UnitID         uint64       `gorm:"not null"                 json:"unit_id"`
	Payee          string       `gorm:"size:200"                 json:"payee"`
	Amount         domain.Money `gorm:"type:DECIMAL(20,4);not null;default:'0.0000'" json:"amount"`
	Status         RefundStatus `gorm:"size:20;not null;default:'pending'"           json:"status"`

	PayableJournalID *uint64    `json:"payable_journal_id,omitempty"`
	PaymentJournalID *uint64    `json:"payment_journal_id,omitempty"`
	BankAccountCode  string     `gorm:"size:20" json:"bank_account_code,omitempty"`
	PaidAt           *time.Time `json:"paid_at,omitempty"`
	PaidBy           *uint64    `json:"paid_by,omitempty"`
	Notes            string     `gorm:"size:500" json:"notes,omitempty"`
	CreatedBy        *uint64    `json:"created_by,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
}

func (Refund) TableName() string { return "refunds" }

// ── Preview (Journal Preview utk UI — frontend spec) ──────────────────────────

// PreviewLine adalah satu baris jurnal yang AKAN diposting (kode akun + nominal).
type PreviewLine struct {
	AccountCode string       `json:"account_code"`
	AccountName string       `json:"account_name"`
	Debit       domain.Money `json:"debit"`
	Credit      domain.Money `json:"credit"`
}

// PreviewJournal adalah satu jurnal terencana dengan tujuannya.
type PreviewJournal struct {
	Purpose string        `json:"purpose"` // revenue_reversal | cogs_reversal | tax_reversal | settlement
	Label   string        `json:"label"`
	Lines   []PreviewLine `json:"lines"`
}

// ProcessPreview adalah dampak lengkap sebelum eksekusi (Journal Preview).
type ProcessPreview struct {
	CancellationID uint64           `json:"cancellation_id"`
	Stage          Stage            `json:"stage"`
	ReceivedTotal  domain.Money     `json:"received_total"`
	Penalty        domain.Money     `json:"penalty"`
	RefundAmount   domain.Money     `json:"refund_amount"`
	Journals       []PreviewJournal `json:"journals"`
	// Dampak ringkas utk UI:
	RevenueReversed domain.Money `json:"revenue_reversed"` // pendapatan yang dibatalkan (post_bast)
	COGSReversed    domain.Money `json:"cogs_reversed"`    // HPP yang dibatalkan (post_bast)
	TaxReversed     domain.Money `json:"tax_reversed"`     // PPh Final yang dibatalkan (post_bast)
	// LandRevenueReversed/LandCOGSReversed: komponen Kelebihan Tanah bundled
	// pada kontrak unit ini, bila ada (kelebihan-tanah-booking-integration-2026-08).
	LandRevenueReversed domain.Money `json:"land_revenue_reversed,omitempty"`
	LandCOGSReversed    domain.Money `json:"land_cogs_reversed,omitempty"`
	UnitNextStatus      string       `json:"unit_next_status"` // selalu 'available'
}

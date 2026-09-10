package sale

import (
	"context"
	"time"

	"gorm.io/gorm"

	"esaproperti/internal/domain"
)

// ── FE-3 · Buyer Credit Lifecycle ────────────────────────────────────────────
//
// "Apply credit" = reklasifikasi sub-ledger: sebagian saldo kredit buyer (pool
// Uang Muka dari overpay pra-BAST) dipakai menutup cicilan. TANPA jurnal, TANPA
// kas, append-only. Event dicatat di CreditApplication (audit who/when/why +
// idempotency); efeknya adalah SATU baris PaymentAllocation bertipe
// 'credit_application'. Baris buyer_credit lama TIDAK PERNAH diubah.
//
//   available_credit(unit) = Σ(buyer_credit) − Σ(credit_application)
//   paid_amount(S)         = Σ(schedule + credit_application untuk S)

// CreditApplication adalah event pemakaian saldo kredit (audit + idempotency).
//
// PaymentScheduleID nil = konsumsi TANPA cicilan target spesifik — dipakai
// oleh konsumsi otomatis (bukan ApplyCredit eksplisit): netting Uang Muka
// Penjualan saat Akad, atau disposisi terminal saat pembatalan unit. Baik
// jalur eksplisit maupun otomatis menulis ke tabel yang SAMA ini — SATU
// source of truth untuk seluruh konsumsi saldo kredit buyer (cegah double
// counting antara ledger dan sub-ledger).
//
// SaleContractID nil = unit belum/tidak pernah punya sale_contract formal
// saat konsumsi terjadi (mis. pembatalan pra-kontrak atas termin/booking
// yang membawa saldo kredit — lihat consumeRemainingCreditInTx). unit_id
// tetap cukup untuk audit & rekonsiliasi saldo pada kasus ini.
type CreditApplication struct {
	ID                uint64       `gorm:"primaryKey;autoIncrement"                     json:"id"`
	TenantID          uint64       `gorm:"not null;index"                               json:"-"`
	SaleContractID    *uint64      `gorm:"index"                                        json:"sale_contract_id,omitempty"`
	UnitID            uint64       `gorm:"not null"                                     json:"unit_id"`
	PaymentScheduleID *uint64      `gorm:"index"                                        json:"payment_schedule_id,omitempty"`
	Amount            domain.Money `gorm:"type:DECIMAL(20,4);not null;default:'0.0000'" json:"amount"`
	Reason            string       `gorm:"size:500"                                     json:"reason"`               // why
	AppliedBy         *uint64      `                                                    json:"applied_by,omitempty"` // who
	IdempotencyKey    *string      `gorm:"size:64"                                      json:"-"`
	CreatedAt         time.Time    `                                                    json:"created_at"` // when
	UpdatedAt         time.Time    `                                                    json:"updated_at"`
}

func (CreditApplication) TableName() string { return "credit_applications" }

// ── Request / result ─────────────────────────────────────────────────────────

// ApplyCreditRequest adalah input pemakaian saldo kredit ke satu cicilan.
type ApplyCreditRequest struct {
	ScheduleID     uint64
	Amount         *domain.Money // nil → default min(available, sisa cicilan)
	Reason         string
	AppliedBy      *uint64
	IdempotencyKey string
}

// ApplyCreditResult adalah hasil pemakaian saldo kredit.
type ApplyCreditResult struct {
	CreditApplicationID uint64 `json:"credit_application_id"`
	ScheduleID          uint64 `json:"schedule_id"`
	Applied             string `json:"applied"`
	SchedulePaidTotal   string `json:"schedule_paid_total"`
	ScheduleFullyPaid   bool   `json:"schedule_fully_paid"`
	RemainingCredit     string `json:"remaining_credit"`
	AlreadyExisted      bool   `json:"already_existed"`
}

// BuyerCreditSource adalah satu sumber saldo kredit (dari overpay).
type BuyerCreditSource struct {
	TerminPaymentID uint64 `json:"termin_payment_id"`
	Amount          string `json:"amount"`
	Date            string `json:"date"`
	ReceiptNumber   string `json:"receipt_number,omitempty"`
}

// BuyerCreditApplicationView adalah satu pemakaian saldo kredit (ke cicilan,
// atau otomatis tanpa cicilan spesifik — PaymentScheduleID nil, Label
// menjelaskan sumber konsumsinya lewat Reason).
type BuyerCreditApplicationView struct {
	ID                uint64  `json:"id"`
	PaymentScheduleID *uint64 `json:"payment_schedule_id,omitempty"`
	Label             string  `json:"label"`
	Amount            string  `json:"amount"`
	Reason            string  `json:"reason,omitempty"`
	AppliedAt         string  `json:"applied_at"`
}

// BuyerCreditView adalah ringkasan saldo kredit buyer sebuah unit.
type BuyerCreditView struct {
	UnitID       uint64                       `json:"unit_id"`
	Available    string                       `json:"available"`     // sumber − terpakai
	TotalSources string                       `json:"total_sources"` // Σ buyer_credit
	TotalApplied string                       `json:"total_applied"` // Σ credit_application
	Sources      []BuyerCreditSource          `json:"sources"`
	Applications []BuyerCreditApplicationView `json:"applications"`
}

// ── Pure validator ───────────────────────────────────────────────────────────

// resolveCreditAmount menentukan & memvalidasi jumlah kredit yang akan dipakai.
// requested nil → default min(available, remaining). Pure & mudah ditest.
func resolveCreditAmount(requested *domain.Money, available, remaining domain.Money) (domain.Money, error) {
	if available.IsZero() || available.IsNeg() {
		return domain.Zero, ErrNoCreditAvailable
	}
	if remaining.IsZero() || remaining.IsNeg() {
		return domain.Zero, ErrScheduleAlreadyPaid
	}
	amt := minMoney(available, remaining)
	if requested != nil {
		amt = *requested
	}
	if !amt.IsWholeRupiah() {
		return domain.Zero, ErrCreditAmountFractional
	}
	if amt.IsZero() || amt.IsNeg() {
		return domain.Zero, ErrCreditAmountZeroOrNeg
	}
	if amt.GreaterThan(available) {
		return domain.Zero, ErrCreditExceedsAvailable
	}
	if amt.GreaterThan(remaining) {
		return domain.Zero, ErrScheduleOverpaid
	}
	return amt, nil
}

func minMoney(a, b domain.Money) domain.Money {
	if a.LessThan(b) {
		return a
	}
	return b
}

// ── Service methods ──────────────────────────────────────────────────────────

// CreditStore mengelola saldo kredit buyer (baca) + pemakaiannya (atomik).
// Diimplementasikan GORMRepository; bagian dari ContractStore.
type CreditStore interface {
	GetBuyerCredit(ctx context.Context, tenantID, unitID uint64) (*BuyerCreditView, error)
	ApplyCredit(ctx context.Context, tenantID, contractID uint64, req ApplyCreditRequest) (*ApplyCreditResult, error)
}

// GetBuyerCredit mengembalikan ringkasan saldo kredit unit sebuah kontrak.
func (s *Service) GetBuyerCredit(ctx context.Context, tenantID, contractID uint64) (*BuyerCreditView, error) {
	if err := s.requireContractStore(); err != nil {
		return nil, err
	}
	contract, err := s.contracts.FindContractByID(ctx, tenantID, contractID)
	if err != nil {
		return nil, err
	}
	return s.contracts.GetBuyerCredit(ctx, tenantID, contract.UnitID)
}

// ApplyCredit memakai saldo kredit buyer ke sebuah cicilan (atomik, tanpa jurnal).
func (s *Service) ApplyCredit(ctx context.Context, tenantID, contractID uint64, req ApplyCreditRequest) (*ApplyCreditResult, error) {
	if err := s.requireContractStore(); err != nil {
		return nil, err
	}
	if req.ScheduleID == 0 {
		return nil, ErrScheduleNotFound
	}
	return s.contracts.ApplyCredit(ctx, tenantID, contractID, req)
}

// ConsumeRemainingCreditInTx menutup habis SISA saldo kredit buyer yang tidak
// lagi bisa dianggap tersedia (dananya sudah dikonsumsi/didisposisi lewat
// jalur lain — netting Uang Muka saat Akad, atau disposisi terminal saat
// pembatalan unit) dengan mencatatnya sebagai SATU credit_applications baru
// (PaymentScheduleID nil). No-op bila tidak ada sisa saldo. Dipanggil di
// DALAM transaksi atomik pemanggil (Akad/pembatalan) — implementasi konkret
// dari seam cancellation.BuyerCreditConsumer, dipakai juga oleh backfill data
// historis. Idempoten secara alami: begitu dijalankan, available jadi 0 —
// pemanggilan ulang pada state yang sama adalah no-op.
func (s *Service) ConsumeRemainingCreditInTx(ctx context.Context, tx *gorm.DB, tenantID, unitID, saleContractID uint64, reason string, appliedBy *uint64) error {
	return consumeRemainingCreditInTx(ctx, tx, tenantID, unitID, saleContractID, reason, appliedBy)
}

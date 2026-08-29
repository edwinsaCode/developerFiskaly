package sale

import (
	"context"
	"fmt"
)

// ── FE-2 · P4 — Reader: sub-ledger payment_allocations (read-only) ────────────
//
// Endpoint baca yang MENGUNGKAP alokasi termin→cicilan yang sebelumnya mustahil
// ditampilkan (breakdown "cicilan ini dibayar oleh KWT/... sebesar Rp ..."). Kini
// payment_allocations adalah sumber kebenaran alokasi; paid_amount hanya cache.

// AllocationView adalah satu baris alokasi yang sudah diperkaya untuk tampilan.
type AllocationView struct {
	ID                uint64  `json:"id"`
	TerminPaymentID   uint64  `json:"termin_payment_id"`
	PaymentScheduleID *uint64 `json:"payment_schedule_id,omitempty"`
	AllocationType    string  `json:"allocation_type"` // schedule | buyer_credit | direct
	Amount            string  `json:"amount"`
	Label             string  `json:"label"`                        // "Termin 2" | "Uang Muka (DP)" | "Pelunasan" | "Saldo Kredit Buyer" | "Pembayaran Langsung"
	InstallmentNumber *int    `json:"installment_number,omitempty"` // nil untuk buyer_credit/direct
	TerminDate        string  `json:"termin_date"`                  // tanggal penerimaan (YYYY-MM-DD)
	ReceiptNumber     string  `json:"receipt_number,omitempty"`     // KWT/...; kosong bila belum ada kwitansi
}

// AllocationReader menyediakan pembacaan sub-ledger alokasi (tenant-scoped).
// Diimplementasikan oleh GORMRepository; bagian dari ContractStore.
type AllocationReader interface {
	ListAllocationsByUnit(ctx context.Context, tenantID, unitID uint64) ([]AllocationView, error)
	ListAllocationsByTermin(ctx context.Context, tenantID, terminID uint64) ([]AllocationView, error)
}

// allocationLabel memberi nama bisnis untuk satu baris alokasi.
func allocationLabel(allocType AllocationType, schedType ScheduleType, installment int) string {
	if allocType == AllocationTypeBuyerCredit {
		return "Saldo Kredit Buyer"
	}
	if allocType == AllocationTypeDirect {
		return "Pembayaran Langsung"
	}
	switch schedType {
	case ScheduleTypeDP:
		return "Uang Muka (DP)"
	case ScheduleTypeFinal:
		return "Pelunasan"
	default:
		return fmt.Sprintf("Termin %d", installment)
	}
}

// ListContractAllocations mengembalikan seluruh alokasi (semua termin) untuk unit
// dari sebuah kontrak — untuk breakdown pembayaran pada statement/detail unit.
func (s *Service) ListContractAllocations(ctx context.Context, tenantID, contractID uint64) ([]AllocationView, error) {
	if err := s.requireContractStore(); err != nil {
		return nil, err
	}
	contract, err := s.contracts.FindContractByID(ctx, tenantID, contractID)
	if err != nil {
		return nil, err
	}
	return s.contracts.ListAllocationsByUnit(ctx, tenantID, contract.UnitID)
}

// ListTerminAllocations mengembalikan alokasi satu penerimaan (ke mana uangnya).
func (s *Service) ListTerminAllocations(ctx context.Context, tenantID, terminID uint64) ([]AllocationView, error) {
	if err := s.requireContractStore(); err != nil {
		return nil, err
	}
	return s.contracts.ListAllocationsByTermin(ctx, tenantID, terminID)
}

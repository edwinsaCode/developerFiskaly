package billing

import (
	"context"
	"fmt"
	"time"

	"gorm.io/gorm"

	"esaproperti/internal/domain"
)

// ── Interfaces (implemented by GORMRepository) ────────────────────────────────

type TerminLoader interface {
	LoadTerminInfo(ctx context.Context, tenantID, terminID uint64) (*TerminInfo, error)
}

type ReceiptStore interface {
	FindReceiptByTermin(ctx context.Context, tenantID, terminID uint64) (*Receipt, error)
	FindReceiptByID(ctx context.Context, tenantID, id uint64) (*Receipt, error)
	CreateReceiptForTermin(ctx context.Context, tenantID, createdBy uint64, info *TerminInfo, notes string) (*Receipt, error)
}

type ReceiptPrintLoader interface {
	LoadReceiptPrintData(ctx context.Context, tenantID, receiptID uint64) (*ReceiptPrintData, error)
}

// ── ReceiptService ────────────────────────────────────────────────────────────

type ReceiptService struct {
	termins  TerminLoader
	receipts ReceiptStore
	prints   ReceiptPrintLoader
	summary  ContractSummaryProvider // opsional (hardening) — nil = tanpa ringkasan
	// chargeSummary (Billing Batch 2): ringkasan grup tagihan utk kwitansi KWR
	// — formula kanonik dari charge.Service via adapter wiring.
	chargeSummary ChargeSummaryProvider
}

// SetContractSummaryProvider memasang sumber ringkasan finansial kontrak —
// SUMBER YANG SAMA dengan invoice (satu rumus, no duplicate SoT).
func (s *ReceiptService) SetContractSummaryProvider(p ContractSummaryProvider) { s.summary = p }

// SetChargeSummaryProvider memasang sumber ringkasan grup tagihan (KWR).
func (s *ReceiptService) SetChargeSummaryProvider(p ChargeSummaryProvider) { s.chargeSummary = p }

func NewReceiptService(termins TerminLoader, receipts ReceiptStore, prints ReceiptPrintLoader) *ReceiptService {
	return &ReceiptService{termins: termins, receipts: receipts, prints: prints}
}

// GenerateReceipt membuat (atau mengembalikan) kwitansi untuk satu transaksi
// termin. IDEMPOTEN: pemanggilan berulang untuk termin yang sama mengembalikan
// receipt yang sama — tidak pernah membuat duplikat (QA: duplicate receipt
// prevention). TIDAK memposting jurnal (QA: no journal duplication).
func (s *ReceiptService) GenerateReceipt(ctx context.Context, tenantID, createdBy, terminID uint64, notes string) (*Receipt, error) {
	info, err := s.termins.LoadTerminInfo(ctx, tenantID, terminID)
	if err != nil {
		return nil, err
	}
	rec, err := s.receipts.CreateReceiptForTermin(ctx, tenantID, createdBy, info, notes)
	if err != nil {
		return nil, fmt.Errorf("buat kwitansi: %w", err)
	}
	return rec, nil
}

// receiptTxStore adalah kapabilitas tx-aware opsional dari ReceiptStore
// (diimplementasikan GORMRepository) — ditemukan via type assertion.
type receiptTxStore interface {
	CreateReceiptForTerminInTx(ctx context.Context, tx *gorm.DB, tenantID, createdBy uint64, info *TerminInfo, notes string) (*Receipt, error)
}

// GenerateReceiptInTx membuat kwitansi MEMAKAI transaksi yang diberikan, agar
// pembuatan kwitansi ikut atomik dengan penerimaan pembayaran (FE-2, guard #2).
// Data termin diberikan langsung (bukan di-load) karena termin baru dibuat dan
// belum ter-commit — tidak terlihat oleh koneksi lain. IDEMPOTEN dalam scope tx.
func (s *ReceiptService) GenerateReceiptInTx(ctx context.Context, tx *gorm.DB, tenantID, createdBy, terminID, unitID uint64, amount domain.Money, bankAccountCode string, date time.Time, notes string) (*Receipt, error) {
	store, ok := s.receipts.(receiptTxStore)
	if !ok {
		return nil, fmt.Errorf("ReceiptStore tidak mendukung transaksi (InTx)")
	}
	info := &TerminInfo{
		ID:              terminID,
		UnitID:          unitID,
		Amount:          amount,
		BankAccountCode: bankAccountCode,
		Date:            date,
	}
	rec, err := store.CreateReceiptForTerminInTx(ctx, tx, tenantID, createdBy, info, notes)
	if err != nil {
		return nil, fmt.Errorf("buat kwitansi (tx): %w", err)
	}
	return rec, nil
}

// GetReceipt mengembalikan receipt berdasarkan ID (tenant-scoped).
func (s *ReceiptService) GetReceipt(ctx context.Context, tenantID, id uint64) (*Receipt, error) {
	return s.receipts.FindReceiptByID(ctx, tenantID, id)
}

// GetReceiptByTermin mengembalikan receipt yang sudah ada untuk sebuah termin.
func (s *ReceiptService) GetReceiptByTermin(ctx context.Context, tenantID, terminID uint64) (*Receipt, error) {
	return s.receipts.FindReceiptByTermin(ctx, tenantID, terminID)
}

// GetReceiptPrintData mengembalikan data lengkap untuk merender kwitansi A4.
func (s *ReceiptService) GetReceiptPrintData(ctx context.Context, tenantID, receiptID uint64) (*ReceiptPrintData, error) {
	data, err := s.prints.LoadReceiptPrintData(ctx, tenantID, receiptID)
	if err != nil {
		return nil, err
	}
	// Billing Batch 2 (K-5): kwitansi KWR menampilkan 4 angka grup tagihan —
	// total tagihan / bayar ini / total dibayar / sisa. Sumber: formula kanonik
	// charge.Service.GroupSummary (via adapter) — tidak dihitung di sini.
	if s.chargeSummary != nil {
		if rec, rerr := s.receipts.FindReceiptByID(ctx, tenantID, receiptID); rerr == nil && rec != nil && rec.ChargeGroupID != nil {
			if cs, cerr := s.chargeSummary.SummaryByGroupID(ctx, tenantID, *rec.ChargeGroupID); cerr == nil && cs != nil {
				data.HasChargeSummary = true
				data.ChargeGroupLabel = cs.Label
				data.ChargeBilled = cs.Billed
				data.ChargePaid = cs.Paid
				data.ChargeOutstanding = cs.Outstanding
			}
		}
	}
	// Hardening: ringkasan finansial kontrak. Kontrak dicari dari receipt
	// (sale_contract_id bila ada; fallback kontrak aktif unit). Best-effort.
	if s.summary != nil {
		if rec, rerr := s.receipts.FindReceiptByID(ctx, tenantID, receiptID); rerr == nil && rec != nil {
			var cs *ContractSummary
			var serr error
			if rec.SaleContractID != nil {
				cs, serr = s.summary.SummaryByContractID(ctx, tenantID, *rec.SaleContractID)
			} else {
				cs, serr = s.summary.SummaryByUnitID(ctx, tenantID, rec.UnitID)
			}
			if serr == nil && cs != nil {
				data.HasSummary = true
				data.UnitPrice = cs.UnitPrice
				data.Discount = cs.Discount
				data.NetContract = cs.NetContract
				data.TotalPaid = cs.TotalPaid
				data.Outstanding = cs.Outstanding
				data.PriceIsSnapshot = cs.PriceIsSnapshot
			}
		}
	}
	return data, nil
}

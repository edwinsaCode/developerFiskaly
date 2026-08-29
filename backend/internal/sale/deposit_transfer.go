package sale

import (
	"context"
	"errors"
	"fmt"
	"time"

	"esaproperti/internal/domain"
)

// ── Billing Batch 2 (K-4) — Transfer sisa Titipan Realisasi → harga rumah ────
//
// Pembayaran harga yang DIDANAI akun titipan (bukan kas): Dr 2-2400 / Cr
// routing GAP-1 (2-2000 pra-BAST | akun piutang policy pasca-BAST). Memakai
// pintu yang SAMA dengan ReceivePayment (preparePaymentFunded + committer
// atomik) sehingga guard overpayment, alokasi waterfall, kwitansi, dan
// sinkron invoice identik — tidak ada engine pembayaran kedua.
//
// Validasi "jumlah ≤ sisa titipan grup" adalah tanggung jawab pemanggil
// (charge.Service) — sale tidak tahu-menahu soal charge group.

// DepositTransferRequest adalah input transfer titipan → pembayaran harga.
type DepositTransferRequest struct {
	ContractID         uint64
	Amount             domain.Money
	Date               time.Time
	DepositAccountCode string // akun liability sumber dana (mis. 2-2400)
	Reference          string
	Notes              string
	CreatedBy          *uint64
	IdempotencyKey     string
}

// ApplyDepositTransfer mencatat pembayaran harga rumah yang didanai titipan.
func (s *Service) ApplyDepositTransfer(ctx context.Context, tenantID uint64, req DepositTransferRequest) (*ReceivePaymentResult, error) {
	if err := s.requireContractStore(); err != nil {
		return nil, err
	}
	if req.DepositAccountCode == "" {
		return nil, ErrUnitRequired
	}

	// Idempotency — kunci sama tidak pernah membuat termin/jurnal kedua.
	if req.IdempotencyKey != "" {
		if existing, err := s.store.FindTerminByIdempotencyKey(ctx, tenantID, req.IdempotencyKey); err == nil {
			return s.existingPaymentResult(ctx, tenantID, existing), nil
		} else if !errors.Is(err, ErrTerminNotFound) {
			return nil, fmt.Errorf("cek idempotency: %w", err)
		}
	}

	contract, err := s.contracts.FindContractByID(ctx, tenantID, req.ContractID)
	if err != nil {
		return nil, err
	}

	desc := "Transfer titipan realisasi ke pembayaran harga"
	if req.Reference != "" {
		desc += " ref " + req.Reference
	}
	if req.Notes != "" {
		desc += " — " + req.Notes
	}

	// Routing + guard + baris jurnal — jalur yang sama dengan pembayaran kas,
	// hanya sumber dananya akun titipan (requireCashBank=false).
	prepared, err := s.preparePaymentFunded(ctx, tenantID, RecordTerminRequest{
		UnitID:          contract.UnitID,
		BankAccountCode: req.DepositAccountCode,
		Amount:          req.Amount,
		Date:            req.Date,
		Description:     desc,
		CreatedBy:       req.CreatedBy,
		IdempotencyKey:  idemPtr(req.IdempotencyKey),
		Source:          PaymentSourceRealizationTransfer,
	}, contract, false)
	if err != nil {
		return nil, err
	}

	cid := contract.ID
	commit, err := s.committer.CommitPayment(ctx, tenantID, PaymentCommitParams{
		UnitID:            contract.UnitID,
		ProjectID:         prepared.projectID,
		PhaseID:           prepared.phaseID,
		Amount:            req.Amount,
		Date:              req.Date,
		Description:       desc,
		BankAccountCode:   req.DepositAccountCode, // audit: sumber dana = akun titipan
		CreditAccountCode: prepared.creditCode,
		CreatedBy:         req.CreatedBy,
		IdempotencyKey:    idemPtr(req.IdempotencyKey),
		Source:            PaymentSourceRealizationTransfer,
		JournalLines:      prepared.lines,
		ContractID:        &cid,
		// T-1 (keputusan klien 2026-08-05): transfer titipan → harga rumah adalah
		// TRANSFER INTERNAL, bukan kas masuk baru. Uangnya sudah ber-kwitansi KWR
		// saat diterima sebagai titipan, jadi di sini TIDAK boleh terbit kwitansi
		// pembayaran (KWT) — pembeli tidak boleh memegang dua bukti terima atas
		// satu aliran uang. Audit trail-nya adalah Memo Transfer Internal (MTI)
		// pada charge_settlements + jurnal Dr 2-2400 / Cr routing GAP-1.
		GenerateReceipt: false,
		ReceiptNotes:    req.Notes,
	})
	if err != nil {
		return nil, err
	}

	// Milestone scheme + pelunasan invoice kekurangan — konsisten ReceivePayment.
	s.applyPaymentMilestones(ctx, tenantID, contract, req.CreatedBy, req.Date, PaymentSourceRealizationTransfer, nil)
	if s.shortfallInv != nil {
		s.shortfallInv.SettleShortfallIfPaid(ctx, tenantID, contract.ID)
	}

	remaining := domain.Zero.String()
	if out, oerr := s.outstandingForContract(ctx, tenantID, contract); oerr == nil {
		remaining = out.String()
	}
	return &ReceivePaymentResult{
		TerminID:             commit.TerminID,
		UnitID:               contract.UnitID,
		Source:               PaymentSourceRealizationTransfer,
		CreditAccount:        prepared.creditCode,
		RemainingBalance:     remaining,
		AppliedSchedules:     toAppliedSchedules(commit.AppliedSchedules),
		BuyerCredit:          commit.BuyerCredit.String(),
		OverpaymentUnapplied: commit.BuyerCredit.String(),
		ReceiptNumber:        commit.ReceiptNumber,
		ReceiptID:            commit.ReceiptID,
	}, nil
}

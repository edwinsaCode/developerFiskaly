package sale

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"esaproperti/internal/domain"
)

// ── Collection Transaction Flow ──────────────────────────────────────────────
//
// Mengorkestrasi pencatatan pembayaran dari halaman collection/statement dengan
// MENGGUNAKAN ULANG engine yang ada (RecordTermin → routing pre/post-BAST +
// jurnal balanced). Tidak membuat jalur jurnal baru.
//
//   sebelum BAST: Dr Bank / Cr Uang Muka Penjualan (2-2000)
//   setelah BAST: Dr Bank / Cr Piutang Usaha (1-2000)
//
// Tambahan di lapisan ini: guard overpayment level-kontrak, partial payment
// (waterfall ke cicilan via paid_amount), idempotency (anti double-submit),
// dan audit "siapa input".

// CollectionPaymentRequest adalah input pencatatan pembayaran dari collection.
type CollectionPaymentRequest struct {
	ContractID      uint64
	Amount          domain.Money
	Date            time.Time
	BankAccountCode string // kas/bank tujuan
	Reference       string // nomor referensi pembayaran (mis. no. transfer)
	Notes           string
	CreatedBy       *uint64 // audit
	IdempotencyKey  string  // proteksi double-submit (boleh kosong)
	// FinancingSourceID (W-13): isi utk PENCAIRAN BANK KPR — ReceivePayment
	// otomatis source=kpr_disbursement + kind=bank_disbursement.
	FinancingSourceID *uint64
	// Kind/InstallmentNo (W-13): label bisnis dari dropdown "Jenis Penerimaan"
	// di +Catat Penerimaan. Diabaikan (diturunkan otomatis) bila
	// FinancingSourceID diisi.
	Kind          TerminKind
	InstallmentNo *int
}

// AppliedSchedule mencatat alokasi pembayaran ke satu cicilan.
type AppliedSchedule struct {
	ScheduleID uint64 `json:"schedule_id"`
	Applied    string `json:"applied"`
	PaidTotal  string `json:"paid_total"`
	FullyPaid  bool   `json:"fully_paid"`
}

// CollectionPaymentResult adalah hasil pencatatan (sebelum kwitansi dilampirkan).
type CollectionPaymentResult struct {
	TerminID         uint64            `json:"termin_id"`
	UnitID           uint64            `json:"unit_id"`
	CreditAccount    string            `json:"credit_account"` // 2-2000 atau 1-2000
	RemainingBalance string            `json:"remaining_balance"`
	AppliedSchedules []AppliedSchedule `json:"applied_schedules"`
	// S8 (registry #6): SATU key = SATU makna.
	// buyer_credit           = saldo kredit buyer KANONIK terakumulasi (creditBalance).
	// overpayment_unapplied  = sisa TRANSAKSI INI yang tak teralokasi ke cicilan.
	BuyerCredit          string `json:"buyer_credit"`
	OverpaymentUnapplied string `json:"overpayment_unapplied"`
	AlreadyExisted       bool   `json:"already_existed"`
}

// CollectionPreviewLine adalah satu baris jurnal pratinjau.
type CollectionPreviewLine struct {
	AccountCode string `json:"account_code"`
	AccountName string `json:"account_name"`
	Debit       string `json:"debit"`
	Credit      string `json:"credit"`
}

// CollectionPreview adalah pratinjau jurnal + validasi SEBELUM submit.
// PreviewAllocationLine adalah satu baris rencana "dialokasikan ke tagihan".
type PreviewAllocationLine struct {
	ScheduleID uint64 `json:"schedule_id"`
	Label      string `json:"label"`     // "Uang Muka (DP)", "Termin 2", ...
	DueDate    string `json:"due_date"`
	Amount     string `json:"amount"`
	FullyPaid  bool   `json:"fully_paid"` // true=Lunas, false=Sebagian
}

type CollectionPreview struct {
	ContractID  uint64                  `json:"contract_id"`
	UnitID      uint64                  `json:"unit_id"`
	IsBAST      bool                    `json:"is_bast"`
	Outstanding string                  `json:"outstanding"`
	Amount      string                  `json:"amount"`
	Valid       bool                    `json:"valid"`
	Reason      string                  `json:"reason,omitempty"`
	Lines       []CollectionPreviewLine `json:"lines"`   // baris jurnal
	Applied     []PreviewAllocationLine `json:"applied"` // dialokasikan ke tagihan
	// S8: buyer_credit = PROYEKSI saldo kredit kanonik setelah pembayaran ini
	// (saldo sekarang + sisa tak teralokasi bila memang jadi kredit buyer);
	// overpayment_unapplied = sisa transaksi ini yang tak cocok jadwal manapun,
	// terlepas dari ke mana sisa itu berakhir (kredit buyer ATAU langsung ke
	// akun riil pasca-BAST, lihat AllocationTypeDirect).
	BuyerCredit          string `json:"buyer_credit"`
	OverpaymentUnapplied string `json:"overpayment_unapplied"`
	// UnappliedIsCredit: true bila overpayment_unapplied akan tercatat sebagai
	// AllocationTypeBuyerCredit (dana bebas, bisa dipakai ke tagihan lain).
	// false bila akan tercatat AllocationTypeDirect (pasca-BAST, sudah
	// mengurangi piutang/pembiayaan riil — BUKAN kelebihan bayar meski tak
	// cocok satu baris jadwal pun). Frontend memakai ini utk memilih copy
	// peringatan yang benar, bukan sekadar overpayment_unapplied>0.
	UnappliedIsCredit bool `json:"unapplied_is_credit"`
}

// accountLabel memberi nama ramah-baca untuk kode akun (pratinjau).
func accountLabel(code string) string {
	switch code {
	case "1-1100":
		return "Kas"
	case "1-1200":
		return "Kas Kecil"
	case "1-1300":
		return "Bank — BCA"
	case "1-1400":
		return "Bank — Mandiri"
	case "1-1500":
		return "Bank — BRI"
	case accountCodeUangMuka:
		return "Uang Muka Penjualan"
	case accountCodePiutang:
		return "Piutang Usaha"
	case "1-2200":
		return "Piutang Bank (KPR)"
	default:
		return code
	}
}

// outstandingForContract menghitung sisa tagihan kontrak = gross − Σ termin.
func (s *Service) outstandingForContract(ctx context.Context, tenantID uint64, c *SaleContract) (domain.Money, error) {
	collected, err := s.store.SumTerminsByUnit(ctx, tenantID, c.UnitID)
	if err != nil {
		return domain.Zero, fmt.Errorf("hitung termin terkumpul: %w", err)
	}
	return c.GrossAmount.Sub(collected), nil
}

// PreviewCollectionPayment menghasilkan pratinjau jurnal + validasi tanpa posting.
func (s *Service) PreviewCollectionPayment(ctx context.Context, tenantID, contractID uint64, amount domain.Money, bankCode string) (*CollectionPreview, error) {
	if err := s.requireContractStore(); err != nil {
		return nil, err
	}
	contract, err := s.contracts.FindContractByID(ctx, tenantID, contractID)
	if err != nil {
		return nil, err
	}
	outstanding, err := s.outstandingForContract(ctx, tenantID, contract)
	if err != nil {
		return nil, err
	}
	saleRec, srErr := s.store.FindSaleRecord(ctx, tenantID, contract.UnitID)
	if srErr != nil && !errors.Is(srErr, ErrSaleRecordNotFound) {
		return nil, fmt.Errorf("cek status BAST: %w", srErr)
	}
	isBAST := saleRec != nil
	creditCode := accountCodeUangMuka
	if isBAST {
		// SSOT: akun kredit pratinjau HARUS di-resolve dengan resolver yang
		// sama seperti posting (preparePayment) — kontrak ber-scheme bisa
		// menunjuk akun lain (KPR pra-pencairan → Piutang Bank). Sebelumnya
		// baris ini hardcode 1-2000 sehingga pratinjau bisa berbeda dari jurnal
		// yang benar-benar terbentuk.
		creditCode = accountCodePiutang
		if code, ok := s.schemeCreditAccountForPayment(ctx, contract); ok {
			creditCode = code
		}
	}

	// Rencana alokasi ke tagihan (read-only) + saldo kredit (kelebihan).
	schedules, err := s.contracts.ListSchedulesByContract(ctx, tenantID, contractID)
	if err != nil {
		return nil, err
	}
	plan, unapplied := planAllocation(schedules, amount)
	appliedLines := make([]PreviewAllocationLine, 0, len(plan))
	for _, p := range plan {
		appliedLines = append(appliedLines, PreviewAllocationLine{
			ScheduleID: p.schedule.ID,
			Label:      p.label,
			DueDate:    p.schedule.DueDate.Format("2006-01-02"),
			Amount:     p.apply.String(),
			FullyPaid:  p.fullyPaid,
		})
	}

	// S8: proyeksi saldo kredit kanonik = saldo sekarang + sisa tak teralokasi.
	// Sisa hanya menambah saldo kredit BUYER bila kreditnya memang akan ditulis
	// sebagai buyer_credit (creditCode==Uang Muka, pra-BAST). Pasca-BAST, sisa
	// tak-berjadwal masuk akun riil (1-2000/1-2200) — allocationTypeForUnapplied
	// menandainya "direct", bukan buyer_credit, jadi TIDAK boleh diproyeksikan ke
	// sini juga (lihat AllocationTypeDirect di payment_allocation.go) — kalau
	// tidak, preview akan menghitung uang yang sama dua kali.
	projectedCredit := domain.Zero
	if allocationTypeForUnapplied(creditCode) == AllocationTypeBuyerCredit {
		projectedCredit = unapplied
	}
	if bc, bcErr := s.contracts.GetBuyerCredit(ctx, tenantID, contract.UnitID); bcErr == nil {
		if avail, mErr := domain.NewMoney(bc.Available); mErr == nil {
			projectedCredit = projectedCredit.Add(avail)
		}
	}

	preview := &CollectionPreview{
		ContractID:  contractID,
		UnitID:      contract.UnitID,
		IsBAST:      isBAST,
		Outstanding: outstanding.String(),
		Amount:      amount.String(),
		Valid:       true,
		Lines: []CollectionPreviewLine{
			{AccountCode: bankCode, AccountName: accountLabel(bankCode), Debit: amount.String(), Credit: domain.Zero.String()},
			{AccountCode: creditCode, AccountName: accountLabel(creditCode), Debit: domain.Zero.String(), Credit: amount.String()},
		},
		Applied:              appliedLines,
		BuyerCredit:          projectedCredit.String(),
		OverpaymentUnapplied: unapplied.String(),
		UnappliedIsCredit:    allocationTypeForUnapplied(creditCode) == AllocationTypeBuyerCredit,
	}

	switch {
	case !amount.IsWholeRupiah() || amount.IsZero() || amount.IsNeg():
		preview.Valid = false
		preview.Reason = ErrCollectionAmountInvalid.Error()
	case s.accounts.ValidateCashBankAccount(ctx, tenantID, bankCode) != nil:
		preview.Valid = false
		preview.Reason = s.accounts.ValidateCashBankAccount(ctx, tenantID, bankCode).Error()
	case isBAST && amount.GreaterThan(outstanding):
		// Kelebihan bayar SETELAH BAST belum didukung (butuh split-credit).
		preview.Valid = false
		preview.Reason = ErrPaymentExceedsReceivable.Error()
	}
	// Kelebihan bayar SEBELUM BAST tetap valid → muncul sebagai Saldo Kredit Buyer.
	return preview, nil
}

// ── ReceivePaymentService: SATU-SATUNYA pintu masuk penerimaan pembayaran ─────
//
// Setiap penerimaan pembayaran customer WAJIB lewat ReceivePayment. Alurnya:
// preparePayment (validasi + routing + baris jurnal, tanpa tulis DB) → rencana
// alokasi (pure) → committer.CommitPayment (jurnal + termin + alokasi + cache +
// kwitansi + invoice dalam SATU transaksi). Endpoint lama adalah adapter tipis
// yang mendelegasikan ke sini (tidak boleh memposting jurnal sendiri). Tidak ada
// engine akuntansi kedua — tetap memakai ledger posting service yang ada.

// ReceivePaymentRequest adalah input tunggal penerimaan pembayaran.
// Tepat satu dari ScheduleID / ContractID / UnitID menentukan anchor.
type ReceivePaymentRequest struct {
	Source          PaymentSource
	ContractID      *uint64
	UnitID          *uint64
	ScheduleID      *uint64 // bayar cicilan spesifik (schedule_received / alokasi eksplisit)
	Amount          domain.Money
	Date            time.Time
	BankAccountCode string
	Reference       string
	Notes           string
	CreatedBy       *uint64
	IdempotencyKey  string
	// FinancingSourceID: bank penyalur bila penerimaan adalah pencairan KPR
	// (source=kpr_disbursement). Increment 3 — audit + laporan per bank.
	FinancingSourceID *uint64
	// Kind (W-13): label bisnis dari dropdown "Jenis Penerimaan". Kosong =
	// diturunkan otomatis (lihat resolveTerminKind) — pemanggil lama (mis.
	// RecordInstallmentPaid) tidak perlu tahu field ini.
	Kind TerminKind
	// InstallmentNo: nomor cicilan bila Kind=installment. Kosong = diturunkan
	// dari cicilan target (bila ScheduleID diisi).
	InstallmentNo *int
}

// ReceivePaymentResult adalah hasil penerimaan.
type ReceivePaymentResult struct {
	TerminID         uint64            `json:"termin_id"`
	UnitID           uint64            `json:"unit_id"`
	Source           PaymentSource     `json:"payment_source"`
	CreditAccount    string            `json:"credit_account"`
	RemainingBalance string            `json:"remaining_balance"`
	AppliedSchedules []AppliedSchedule `json:"applied_schedules"`
	// S8 (registry #6): buyer_credit = saldo kredit KANONIK terakumulasi pasca
	// commit; overpayment_unapplied = sisa transaksi ini yang tak teralokasi.
	BuyerCredit          string `json:"buyer_credit"`
	OverpaymentUnapplied string `json:"overpayment_unapplied"`
	ReceiptNumber        string `json:"receipt_number,omitempty"`
	ReceiptID            uint64 `json:"receipt_id,omitempty"`
	AlreadyExisted       bool   `json:"already_existed"`
}

// ReceivePayment adalah pintu masuk tunggal penerimaan pembayaran customer.
// Tanggung jawab: validasi tenant/amount, idempotency (anti double-submit),
// penentuan pre/post-BAST, alokasi ke cicilan (partial), kwitansi, sinkron
// invoice, dan audit trail (source + created_by).
func (s *Service) ReceivePayment(ctx context.Context, tenantID uint64, req ReceivePaymentRequest) (*ReceivePaymentResult, error) {
	if err := s.requireContractStore(); err != nil {
		return nil, err
	}

	// 1. Idempotency — kunci sama tidak pernah membuat termin/jurnal/receipt kedua.
	if req.IdempotencyKey != "" {
		if existing, err := s.store.FindTerminByIdempotencyKey(ctx, tenantID, req.IdempotencyKey); err == nil {
			return s.existingPaymentResult(ctx, tenantID, existing), nil
		} else if !errors.Is(err, ErrTerminNotFound) {
			return nil, fmt.Errorf("cek idempotency: %w", err)
		}
	}

	// 2. Resolusi anchor unit/kontrak/cicilan (tenant-scoped).
	var unitID uint64
	var contract *SaleContract
	var targetSchedule *PaymentSchedule
	switch {
	case req.ScheduleID != nil:
		sch, err := s.contracts.FindScheduleByID(ctx, tenantID, *req.ScheduleID)
		if err != nil {
			return nil, err
		}
		targetSchedule = sch
		unitID = sch.UnitID
		if c, cerr := s.contracts.FindContractByID(ctx, tenantID, sch.SaleContractID); cerr == nil {
			contract = c
		}
	case req.ContractID != nil:
		c, err := s.contracts.FindContractByID(ctx, tenantID, *req.ContractID)
		if err != nil {
			return nil, err
		}
		contract = c
		unitID = c.UnitID
	case req.UnitID != nil:
		unitID = *req.UnitID
		if c, cerr := s.contracts.FindContractByUnitID(ctx, tenantID, unitID); cerr == nil {
			contract = c // advance tanpa kontrak diizinkan (booking pra-PPJB) → contract = nil
		}
	default:
		return nil, ErrUnitRequired
	}

	// 3. Kelebihan bayar SEBELUM BAST diizinkan → menjadi Saldo Kredit Buyer
	//    (tetap di Uang Muka/2-2000, kewajiban). SETELAH BAST, preparePayment
	//    menolak kelebihan bayar (ErrPaymentExceedsReceivable) — split-credit
	//    pasca-BAST adalah fase berikutnya. Tidak ada guard level-kontrak di sini.

	// 3b. Financing source (pencairan KPR): validasi keberadaan bank + hanya
	//     untuk source kpr_disbursement (Increment 3).
	if req.FinancingSourceID != nil {
		if req.Source != PaymentSourceKPRDisbursement {
			return nil, ErrFinancingSourceOnNonKPRPayment
		}
		if s.schemeEnabled() {
			if _, ferr := s.schemeFlow.FindFinancingSourceByID(ctx, tenantID, *req.FinancingSourceID); ferr != nil {
				return nil, ferr
			}
		}
	}
	// R1: guard urutan flow KPR — pencairan bank hanya setelah akad (state
	// akad|disbursed; disbursed = pencairan bertahap/parsial diizinkan).
	// Validasi INTEGRITAS FLOW, bukan jurnal — jurnal tetap Event 2 biasa.
	if req.Source == PaymentSourceKPRDisbursement {
		if contract == nil {
			return nil, ErrDisbursementNeedsContract
		}
		if s.schemeEnabled() && contract.SchemeState != nil {
			st := *contract.SchemeState
			if st != "akad" && st != "disbursed" {
				return nil, ErrDisbursementRequiresAkad
			}
		}
	}

	// 3c. W-13: turunkan Kind/InstallmentNo terstruktur. Prioritas: (a) source
	//     kpr_disbursement SELALU bank_disbursement (SoT tetap payment_source,
	//     tidak boleh mismatch lewat kind yang salah kirim dari client);
	//     (b) bayar cicilan spesifik → turunkan dari schedule.Type/Number
	//     (jalur lama recordInstallmentPaid otomatis dapat label benar tanpa
	//     ubah frontend); (c) selainnya pakai yang dikirim pemanggil (fallback "other").
	kind, installmentNo := resolveTerminKind(req, targetSchedule)

	// 4. Validasi + routing + baris jurnal balanced (TANPA tulis DB).
	prepared, err := s.preparePayment(ctx, tenantID, RecordTerminRequest{
		UnitID:          unitID,
		BankAccountCode: req.BankAccountCode,
		Amount:          req.Amount,
		Date:            req.Date,
		Description:     buildReceiveDescription(req),
		CreatedBy:       req.CreatedBy,
		IdempotencyKey:  idemPtr(req.IdempotencyKey),
		Source:          req.Source,
	}, contract)
	if err != nil {
		return nil, err
	}

	// 5. Anchor alokasi: cicilan spesifik / kontrak (waterfall) / tanpa kontrak.
	//    Committer yang merencanakan alokasi DI DALAM transaksi dengan cicilan
	//    dikunci SELECT FOR UPDATE (mencegah race double-apply antar pembayaran).
	var scheduleIDPtr, contractIDPtr *uint64
	if targetSchedule != nil {
		id := targetSchedule.ID
		scheduleIDPtr = &id
	} else if contract != nil {
		id := contract.ID
		contractIDPtr = &id
	}

	// 6. Commit ATOMIK: plan(lock) + jurnal + termin + alokasi + cache + kwitansi +
	//    invoice dalam satu transaksi (guard #2/#4). Bila alokasi gagal → rollback.
	commit, err := s.committer.CommitPayment(ctx, tenantID, PaymentCommitParams{
		UnitID:            unitID,
		ProjectID:         prepared.projectID,
		PhaseID:           prepared.phaseID,
		Amount:            req.Amount,
		Date:              req.Date,
		Description:       buildReceiveDescription(req),
		BankAccountCode:   req.BankAccountCode,
		CreditAccountCode: prepared.creditCode,
		CreatedBy:         req.CreatedBy,
		IdempotencyKey:    idemPtr(req.IdempotencyKey),
		Source:            req.Source,
		FinancingSourceID: req.FinancingSourceID,
		Kind:              kind,
		InstallmentNo:     installmentNo,
		JournalLines:      prepared.lines,
		ScheduleID:        scheduleIDPtr,
		ContractID:        contractIDPtr,
		GenerateReceipt:   true,
		ReceiptNotes:      req.Notes,
	})
	if err != nil {
		return nil, err
	}

	// 6b. Proyeksi milestone scheme (dp_paid/installment_paid/fully_paid) —
	//     best-effort; tidak pernah membatalkan pembayaran (Increment 3).
	s.applyPaymentMilestones(ctx, tenantID, contract, req.CreatedBy, req.Date, req.Source, req.FinancingSourceID)

	// 6c. R1: pasca-pencairan bank, tawarkan/buat invoice kekurangan sesuai
	//     kebijakan tenant (default OFF → UI menampilkan CTA manual).
	//
	//     T-3 (keputusan klien final 2026-08-05): invoice ini ditagih ke PEMBELI
	//     dan ledger-nya kini sejalan — langkah 6b di atas sudah memindahkan
	//     state ke `disbursed`, yang mereklas sisa tagihan Piutang Bank →
	//     Piutang Customer. Jadi invoice KEKURANGAN dan saldo piutang menunjuk
	//     counterparty yang sama. Lihat scheme.KPRPolicy.ResolveReceivableAccount.
	if req.Source == PaymentSourceKPRDisbursement && contract != nil && s.shortfallInv != nil {
		var actor uint64
		if req.CreatedBy != nil {
			actor = *req.CreatedBy
		}
		s.shortfallInv.MaybeAutoShortfallInvoice(ctx, tenantID, contract.ID, actor)
	}

	// 6d. R1: bila pembayaran ini melunasi kontrak, invoice KEKURANGAN yang
	//     masih terbuka ikut ditandai paid (best-effort — invoice kekurangan
	//     tak terikat schedule sehingga tak tersentuh sinkron invoice cicilan).
	if contract != nil && s.shortfallInv != nil {
		s.shortfallInv.SettleShortfallIfPaid(ctx, tenantID, contract.ID)
	}

	// 7. Sisa tagihan setelah pembayaran (SumTermin sudah termasuk termin baru).
	remaining := domain.Zero.String()
	if contract != nil {
		if out, oerr := s.outstandingForContract(ctx, tenantID, contract); oerr == nil {
			remaining = out.String()
		}
	}

	// S8: buyer_credit = saldo kredit KANONIK pasca-commit (creditBalance);
	// sisa transaksi ini dilaporkan terpisah sebagai overpayment_unapplied.
	canonicalCredit := domain.Zero
	if s.contracts != nil {
		if bc, bcErr := s.contracts.GetBuyerCredit(ctx, tenantID, unitID); bcErr == nil {
			if avail, mErr := domain.NewMoney(bc.Available); mErr == nil {
				canonicalCredit = avail
			}
		}
	}
	return &ReceivePaymentResult{
		TerminID:             commit.TerminID,
		UnitID:               unitID,
		Source:               req.Source,
		CreditAccount:        prepared.creditCode,
		RemainingBalance:     remaining,
		AppliedSchedules:     toAppliedSchedules(commit.AppliedSchedules),
		BuyerCredit:          canonicalCredit.String(),
		OverpaymentUnapplied: commit.BuyerCredit.String(),
		ReceiptNumber:        commit.ReceiptNumber,
		ReceiptID:            commit.ReceiptID,
	}, nil
}

// generateReceiptBestEffort membuat kwitansi (idempoten) bila generator terpasang.
func (s *Service) generateReceiptBestEffort(ctx context.Context, tenantID uint64, createdBy *uint64, terminID uint64, notes string) (string, uint64) {
	if s.receiptGen == nil {
		return "", 0
	}
	var cb uint64
	if createdBy != nil {
		cb = *createdBy
	}
	if num, id, err := s.receiptGen.GenerateReceipt(ctx, tenantID, cb, terminID, notes); err == nil {
		return num, id
	}
	return "", 0
}

// RecordCollectionPayment adalah adapter untuk endpoint /collections/payment.
// Mendelegasikan ke ReceivePayment(source=collection).
func (s *Service) RecordCollectionPayment(ctx context.Context, tenantID uint64, req CollectionPaymentRequest) (*CollectionPaymentResult, error) {
	cid := req.ContractID
	source := PaymentSourceCollection
	if req.FinancingSourceID != nil {
		source = PaymentSourceKPRDisbursement // pencairan bank (Increment 3)
	}
	res, err := s.ReceivePayment(ctx, tenantID, ReceivePaymentRequest{
		Source:            source,
		ContractID:        &cid,
		Amount:            req.Amount,
		Date:              req.Date,
		BankAccountCode:   req.BankAccountCode,
		Reference:         req.Reference,
		Notes:             req.Notes,
		CreatedBy:         req.CreatedBy,
		IdempotencyKey:    req.IdempotencyKey,
		FinancingSourceID: req.FinancingSourceID,
		Kind:              req.Kind,
		InstallmentNo:     req.InstallmentNo,
	})
	if err != nil {
		return nil, err
	}
	return &CollectionPaymentResult{
		TerminID:             res.TerminID,
		UnitID:               res.UnitID,
		CreditAccount:        res.CreditAccount,
		RemainingBalance:     res.RemainingBalance,
		AppliedSchedules:     res.AppliedSchedules,
		BuyerCredit:          res.BuyerCredit,
		OverpaymentUnapplied: res.OverpaymentUnapplied,
		AlreadyExisted:       res.AlreadyExisted,
	}, nil
}

// planForSchedule (pure) menghitung alokasi ke SATU cicilan spesifik tanpa persist.
func planForSchedule(sch *PaymentSchedule, amount domain.Money) ([]ScheduleAllocation, domain.Money) {
	need := sch.Amount.Sub(sch.PaidAmount)
	if need.IsZero() || need.IsNeg() {
		return nil, amount // cicilan sudah lunas → semua jadi saldo kredit
	}
	pay := need
	if amount.LessThan(need) {
		pay = amount
	}
	newPaid := sch.PaidAmount.Add(pay)
	return []ScheduleAllocation{{
		ScheduleID: sch.ID,
		Apply:      pay,
		NewPaid:    newPaid,
		FullyPaid:  !newPaid.LessThan(sch.Amount),
	}}, amount.Sub(pay)
}

// planForContract (pure) menerjemahkan waterfall planAllocation menjadi instruksi
// alokasi + sisa tak teralokasi (saldo kredit). Tidak menulis DB.
func planForContract(schedules []*PaymentSchedule, amount domain.Money) ([]ScheduleAllocation, domain.Money) {
	plan, unapplied := planAllocation(schedules, amount)
	out := make([]ScheduleAllocation, 0, len(plan))
	for _, p := range plan {
		out = append(out, ScheduleAllocation{
			ScheduleID: p.schedule.ID,
			Apply:      p.apply,
			NewPaid:    p.newPaid,
			FullyPaid:  p.fullyPaid,
		})
	}
	return out, unapplied
}

// toAppliedSchedules memetakan rencana alokasi ke bentuk respons API.
func toAppliedSchedules(allocs []ScheduleAllocation) []AppliedSchedule {
	out := make([]AppliedSchedule, 0, len(allocs))
	for _, a := range allocs {
		out = append(out, AppliedSchedule{
			ScheduleID: a.ScheduleID,
			Applied:    a.Apply.String(),
			PaidTotal:  a.NewPaid.String(),
			FullyPaid:  a.FullyPaid,
		})
	}
	return out
}

// plannedAllocation adalah hasil simulasi alokasi (belum dipersist).
type plannedAllocation struct {
	schedule  *PaymentSchedule
	label     string
	apply     domain.Money
	newPaid   domain.Money
	fullyPaid bool
}

// scheduleLabel memberi nama bisnis untuk satu cicilan (untuk tampilan).
func scheduleLabel(sch *PaymentSchedule) string {
	switch sch.Type {
	case ScheduleTypeDP:
		return "Uang Muka (DP)"
	case ScheduleTypeFinal:
		return "Pelunasan"
	default:
		return fmt.Sprintf("Termin %d", sch.InstallmentNumber)
	}
}

// planAllocation mensimulasikan distribusi pembayaran ke cicilan (waterfall:
// cicilan tertua dulu) TANPA mempersist. Mengembalikan rencana + sisa yang tak
// teralokasi (saldo kredit buyer). Pure function — mudah ditest.
func planAllocation(schedules []*PaymentSchedule, amount domain.Money) ([]plannedAllocation, domain.Money) {
	sorted := append([]*PaymentSchedule(nil), schedules...)
	sort.SliceStable(sorted, func(i, j int) bool {
		a, b := sorted[i], sorted[j]
		if a.InstallmentNumber != b.InstallmentNumber {
			return a.InstallmentNumber < b.InstallmentNumber
		}
		if !a.DueDate.Equal(b.DueDate) {
			return a.DueDate.Before(b.DueDate)
		}
		return a.ID < b.ID
	})

	remaining := amount
	plan := make([]plannedAllocation, 0)
	for _, sch := range sorted {
		if remaining.IsZero() {
			break
		}
		need := sch.Amount.Sub(sch.PaidAmount)
		if need.IsZero() || need.IsNeg() {
			continue // cicilan sudah lunas
		}
		pay := need
		if remaining.LessThan(need) {
			pay = remaining
		}
		newPaid := sch.PaidAmount.Add(pay)
		plan = append(plan, plannedAllocation{
			schedule:  sch,
			label:     scheduleLabel(sch),
			apply:     pay,
			newPaid:   newPaid,
			fullyPaid: !newPaid.LessThan(sch.Amount),
		})
		remaining = remaining.Sub(pay)
	}
	return plan, remaining // remaining = saldo kredit (unapplied)
}

// existingPaymentResult menyusun hasil untuk idempotency-hit (tanpa posting baru).
func (s *Service) existingPaymentResult(ctx context.Context, tenantID uint64, termin *TerminPayment) *ReceivePaymentResult {
	remaining := domain.Zero.String()
	if contract, err := s.contracts.FindContractByUnitID(ctx, tenantID, termin.UnitID); err == nil {
		if out, oerr := s.outstandingForContract(ctx, tenantID, contract); oerr == nil {
			remaining = out.String()
		}
	}
	// S8: buyer_credit tetap saldo KANONIK saat ini (bukan nol semu);
	// overpayment_unapplied "0" — transaksi ini tidak diposting ulang.
	canonicalCredit := domain.Zero
	if bc, bcErr := s.contracts.GetBuyerCredit(ctx, tenantID, termin.UnitID); bcErr == nil {
		if avail, mErr := domain.NewMoney(bc.Available); mErr == nil {
			canonicalCredit = avail
		}
	}
	return &ReceivePaymentResult{
		TerminID:             termin.ID,
		UnitID:               termin.UnitID,
		Source:               termin.PaymentSource,
		CreditAccount:        termin.CreditAccountCode,
		RemainingBalance:     remaining,
		AppliedSchedules:     []AppliedSchedule{},
		BuyerCredit:          canonicalCredit.String(),
		OverpaymentUnapplied: domain.Zero.String(),
		AlreadyExisted:       true,
	}
}

// resolveTerminKind (W-13) menentukan label bisnis terstruktur final. Lihat
// komentar pemanggil (ReceivePayment §3c) untuk urutan prioritas.
func resolveTerminKind(req ReceivePaymentRequest, targetSchedule *PaymentSchedule) (TerminKind, *int) {
	if req.Source == PaymentSourceKPRDisbursement {
		return TerminKindBankDisbursement, nil
	}
	if targetSchedule != nil {
		switch targetSchedule.Type {
		case ScheduleTypeDP:
			return TerminKindDP, nil
		case ScheduleTypeFinal:
			return TerminKindFinalPayment, nil
		default:
			n := targetSchedule.InstallmentNumber
			return TerminKindInstallment, &n
		}
	}
	if ValidTerminKind(req.Kind) {
		return req.Kind, req.InstallmentNo
	}
	return TerminKindOther, req.InstallmentNo
}

func buildReceiveDescription(req ReceivePaymentRequest) string {
	desc := "Penerimaan pembayaran"
	if req.Reference != "" {
		desc += " ref " + req.Reference
	}
	if req.Notes != "" {
		desc += " — " + req.Notes
	}
	return desc
}

func idemPtr(key string) *string {
	if key == "" {
		return nil
	}
	return &key
}

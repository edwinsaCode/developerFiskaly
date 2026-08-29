package sale

import (
	"context"
	"fmt"
	"time"

	"gorm.io/gorm"

	"esaproperti/internal/domain"
)

// ── FE-2 · Payment Allocation sub-ledger ─────────────────────────────────────
//
// PaymentAllocation memetakan satu penerimaan (TerminPayment, 1 jurnal) ke satu
// cicilan (PaymentSchedule) atau ke saldo kredit buyer (kelebihan bayar).
// TIDAK memposting jurnal — murni sub-ledger (Invariant #1/#5).
//
// Otoritas: baris payment_allocations adalah SUMBER KEBENARAN alokasi;
// payment_schedules.paid_amount hanyalah cache/projection. Tidak ada code path
// yang boleh menyentuh paid_amount tanpa menulis baris alokasi (guard #1) — itu
// dijamin dengan menggabungkan keduanya di ApplyScheduleAllocation.

type AllocationType string

const (
	AllocationTypeSchedule    AllocationType = "schedule"
	AllocationTypeBuyerCredit AllocationType = "buyer_credit"
	// AllocationTypeCreditApplication (FE-3): pemakaian saldo kredit buyer ke
	// cicilan. termin_payment_id NULL (pool), credit_application_id terisi.
	AllocationTypeCreditApplication AllocationType = "credit_application"
	// AllocationTypeChargeItem (Billing Batch 2, K-3): alokasi MANUAL admin dari
	// satu termin realisasi ke satu charge_item. Amount selalu > 0.
	AllocationTypeChargeItem AllocationType = "charge_item"
	// AllocationTypeChargeItemVoid: mirror NEGATIF saat void pembayaran realisasi
	// (histori utuh — Invariant #5). Pembaca outstanding menjumlahkan keduanya.
	AllocationTypeChargeItemVoid AllocationType = "charge_item_void"
	// AllocationTypeDirect (W-13/F, jadwal opsional): sisa pembayaran pasca-BAST
	// yang tak cocok jadwal manapun, TAPI kreditnya sudah akun riil (1-2000
	// Piutang Usaha / 1-2200 Piutang Bank), bukan Uang Muka. preparePaymentFunded
	// menjamin amount<=outstanding pasca-BAST, jadi sisa ini BUKAN kelebihan
	// bayar — beda dari buyer_credit yang bisa dipakai ulang lewat
	// credit_applications. Menandainya buyer_credit akan menghitung uang yang
	// sama dua kali (sudah melunasi piutang/pembiayaan riil, lalu "tersedia"
	// lagi sebagai saldo kredit). Tetap ditulis (bukan didiskon) agar Σalokasi
	// == termin.amount terjaga (Invariant #3 CLAUDE.md — alokasi tak pernah
	// dibuang).
	AllocationTypeDirect AllocationType = "direct"
)

// allocationTypeForUnapplied menentukan tipe baris sub-ledger utk sisa
// pembayaran yang tak cocok jadwal manapun. Lihat AllocationTypeDirect.
func allocationTypeForUnapplied(creditAccountCode string) AllocationType {
	if creditAccountCode == accountCodeUangMuka {
		return AllocationTypeBuyerCredit
	}
	return AllocationTypeDirect
}

// PaymentAllocation adalah satu baris sub-ledger alokasi.
// Kolom generated `schedule_key` (DB) TIDAK dipetakan di sini — dikelola MySQL.
// TerminPaymentID nullable: baris 'credit_application' (FE-3) berasal dari POOL
// kredit, bukan satu termin → NULL. Baris kas selalu punya termin (guard #3 utuh).
type PaymentAllocation struct {
	ID                  uint64         `gorm:"primaryKey;autoIncrement"                     json:"id"`
	TenantID            uint64         `gorm:"not null;index"                               json:"-"`
	TerminPaymentID     *uint64        `gorm:"index"                                        json:"termin_payment_id,omitempty"`
	CreditApplicationID *uint64        `gorm:"index"                                        json:"credit_application_id,omitempty"`
	PaymentScheduleID   *uint64        `gorm:"index"                                        json:"payment_schedule_id,omitempty"`
	ChargeItemID        *uint64        `gorm:"index"                                        json:"charge_item_id,omitempty"`
	AllocationType      AllocationType `gorm:"not null;size:20"                             json:"allocation_type"`
	Amount              domain.Money   `gorm:"type:DECIMAL(20,4);not null;default:'0.0000'" json:"amount"`
	CreatedBy           *uint64        `                                                    json:"created_by,omitempty"`
	CreatedAt           time.Time      `                                                    json:"created_at"`
	UpdatedAt           time.Time      `                                                    json:"updated_at"`
}

func (PaymentAllocation) TableName() string { return "payment_allocations" }

// ── Planning types (pure, dihasilkan Service sebelum commit) ─────────────────

// ScheduleAllocation adalah satu instruksi alokasi ke cicilan (hasil planAllocation).
type ScheduleAllocation struct {
	ScheduleID uint64
	Apply      domain.Money // jumlah dialokasikan ke cicilan ini
	NewPaid    domain.Money // paid_amount cicilan setelah alokasi
	FullyPaid  bool
}

// ── Commit types ─────────────────────────────────────────────────────────────

// PaymentCommitParams adalah paket lengkap SATU penerimaan pembayaran yang siap
// dipersist secara atomik. Semua keputusan bisnis (routing BAST, validasi, baris
// jurnal, rencana alokasi) sudah diselesaikan Service; committer hanya menulis.
type PaymentCommitParams struct {
	// Termin
	UnitID            uint64
	ProjectID         uint64
	PhaseID           *uint64
	Amount            domain.Money
	Date              time.Time
	Description       string
	BankAccountCode   string
	CreditAccountCode string // 2-2000 (pra-BAST) | 1-2000 (pasca-BAST)
	CreatedBy         *uint64
	IdempotencyKey    *string
	Source            PaymentSource
	// FinancingSourceID: bank penyalur bila pencairan KPR (Increment 3).
	FinancingSourceID *uint64
	// Kind/InstallmentNo (W-13): label bisnis terstruktur, sudah diturunkan
	// Service (resolveTerminKind) — committer hanya menulis apa adanya.
	Kind          TerminKind
	InstallmentNo *int

	// Jurnal — sudah dibangun & balanced (Dr Bank / Cr kredit).
	JournalLines []JournalLineInput

	// Anchor alokasi (committer yang merencanakan, DI DALAM transaksi, dengan
	// cicilan dikunci SELECT FOR UPDATE — mencegah race double-apply). Tepat satu:
	//   ScheduleID → bayar cicilan spesifik; ContractID → waterfall; keduanya nil
	//   → seluruh amount jadi saldo kredit buyer (advance tanpa kontrak).
	ScheduleID *uint64
	ContractID *uint64

	// Kwitansi.
	GenerateReceipt bool
	ReceiptNotes    string
}

// PaymentCommitResult adalah hasil commit. AppliedSchedules/BuyerCredit adalah
// alokasi yang BENAR-BENAR dipersist (hasil plan di bawah lock) — otoritatif
// untuk respons, bukan rencana pra-transaksi.
type PaymentCommitResult struct {
	TerminID         uint64
	JournalID        uint64
	AppliedSchedules []ScheduleAllocation
	BuyerCredit      domain.Money
	ReceiptNumber    string
	ReceiptID        uint64
}

// PaymentCommitter mempersist SATU penerimaan pembayaran (jurnal + termin +
// alokasi + cache + kwitansi + sinkron invoice). Implementasi produksi
// (sale.GORMRepository) menjalankan seluruhnya dalam SATU transaksi DB —
// atomik: jika alokasi gagal, jurnal ikut rollback (guard #2/#4, keputusan #4).
type PaymentCommitter interface {
	CommitPayment(ctx context.Context, tenantID uint64, p PaymentCommitParams) (*PaymentCommitResult, error)
}

// ── Sub-ledger write inputs (dipakai default committer & GORMRepository) ──────

// ScheduleAllocationInput menggabungkan penulisan baris alokasi + update cache
// paid_amount dalam SATU operasi (guard #1: tak ada paid_amount tanpa alokasi).
type ScheduleAllocationInput struct {
	ScheduleID      uint64
	TerminPaymentID uint64
	Apply           domain.Money
	NewPaid         domain.Money
	FullyPaid       bool
	ReceivedAt      time.Time
	CreatedBy       *uint64
}

// BuyerCreditAllocationInput adalah input baris sisa pembayaran tak-berjadwal
// (schedule NULL) — buyer_credit (kelebihan pra-BAST) ATAU direct (sisa
// pasca-BAST ke akun riil, lihat AllocationTypeDirect).
type BuyerCreditAllocationInput struct {
	TerminPaymentID uint64
	Amount          domain.Money
	// Type kosong ("") = default AllocationTypeBuyerCredit (perilaku lama).
	Type      AllocationType
	CreatedBy *uint64
}

// ── Tx-aware collaborators (lintas package, pola AccruePPhFinalInTx) ──────────
//
// Diimplementasikan oleh adapter ke billing di wiring layer; sale tidak import
// billing (hindari circular import). Dipanggil DI DALAM transaksi commit agar
// kwitansi & sinkron invoice ikut atomik.

type ReceiptTxGenerator interface {
	GenerateReceiptInTx(ctx context.Context, tx *gorm.DB, tenantID, createdBy, terminID, unitID uint64, amount domain.Money, bankAccountCode string, date time.Time, notes string) (receiptNumber string, receiptID uint64, err error)
}

type InvoiceTxUpdater interface {
	MarkPaidByScheduleIDInTx(ctx context.Context, tx *gorm.DB, tenantID, scheduleID uint64) error
}

// ── defaultCommitter — fallback non-atomik berbasis interface ────────────────
//
// Dipakai bila tidak ada PaymentCommitter atomik yang di-wire (mis. unit test
// dengan mock in-memory). Mereproduksi urutan tulis via interface Service yang
// ada + penulisan alokasi. TIDAK menjamin atomicity DB — produksi WAJIB
// memakai GORMRepository.CommitPayment (satu transaksi). Guard #1 tetap dijaga:
// alokasi + paid_amount selalu lewat ApplyScheduleAllocation.
type defaultCommitter struct {
	svc *Service
}

func (d *defaultCommitter) CommitPayment(ctx context.Context, tenantID uint64, p PaymentCommitParams) (*PaymentCommitResult, error) {
	s := d.svc

	// 1. Rencana alokasi via interface (tanpa lock — jalur unit test in-memory).
	scheduleAllocs, buyerCredit, err := d.planAllocations(ctx, tenantID, p)
	if err != nil {
		return nil, err
	}

	// 2. Jurnal (create + post).
	journalID, err := s.journals.CreateJournal(ctx, tenantID, p.Date, p.Description, p.JournalLines)
	if err != nil {
		return nil, fmt.Errorf("buat jurnal termin: %w", err)
	}
	if err := s.journals.PostJournal(ctx, tenantID, journalID); err != nil {
		return nil, fmt.Errorf("posting jurnal termin: %w", err)
	}

	// 3. Termin.
	termin := &TerminPayment{
		TenantID:          tenantID,
		UnitID:            p.UnitID,
		ProjectID:         p.ProjectID,
		PhaseID:           p.PhaseID,
		Amount:            p.Amount,
		BankAccountCode:   p.BankAccountCode,
		Date:              p.Date,
		Description:       p.Description,
		JournalEntryID:    journalID,
		CreditAccountCode: p.CreditAccountCode,
		CreatedBy:         p.CreatedBy,
		IdempotencyKey:    p.IdempotencyKey,
		PaymentSource:     p.Source,
		FinancingSourceID: p.FinancingSourceID,
		Kind:              p.Kind,
		InstallmentNo:     p.InstallmentNo,
		CountsTowardPrice: true, // R4: pembayaran harga — selalu mengurangi outstanding
	}
	if err := s.store.SaveTermin(ctx, termin); err != nil {
		return nil, fmt.Errorf("simpan termin: %w", err)
	}

	// 4. Alokasi ke cicilan (alokasi + cache, satu jalur).
	for _, a := range scheduleAllocs {
		if err := s.contracts.ApplyScheduleAllocation(ctx, tenantID, ScheduleAllocationInput{
			ScheduleID:      a.ScheduleID,
			TerminPaymentID: termin.ID,
			Apply:           a.Apply,
			NewPaid:         a.NewPaid,
			FullyPaid:       a.FullyPaid,
			ReceivedAt:      p.Date,
			CreatedBy:       p.CreatedBy,
		}); err != nil {
			return nil, fmt.Errorf("alokasi cicilan #%d: %w", a.ScheduleID, err)
		}
	}

	// 5. Sisa tak-berjadwal: saldo kredit buyer (pra-BAST) atau direct (pasca-BAST).
	if buyerCredit.GreaterThan(domain.Zero) {
		if err := s.contracts.InsertBuyerCreditAllocation(ctx, tenantID, BuyerCreditAllocationInput{
			TerminPaymentID: termin.ID,
			Amount:          buyerCredit,
			Type:            allocationTypeForUnapplied(p.CreditAccountCode),
			CreatedBy:       p.CreatedBy,
		}); err != nil {
			return nil, fmt.Errorf("alokasi saldo kredit: %w", err)
		}
	}

	// 6. Kwitansi (best-effort bila generator tidak terpasang — konsisten perilaku lama).
	var receiptNumber string
	var receiptID uint64
	if p.GenerateReceipt {
		receiptNumber, receiptID = s.generateReceiptBestEffort(ctx, tenantID, p.CreatedBy, termin.ID, p.ReceiptNotes)
	}

	// 7. Sinkron status invoice untuk cicilan lunas.
	if s.invoiceUpdater != nil {
		for _, a := range scheduleAllocs {
			if a.FullyPaid {
				_ = s.invoiceUpdater.MarkPaidByScheduleID(ctx, tenantID, a.ScheduleID)
			}
		}
	}

	return &PaymentCommitResult{
		TerminID:         termin.ID,
		JournalID:        journalID,
		AppliedSchedules: scheduleAllocs,
		BuyerCredit:      buyerCredit,
		ReceiptNumber:    receiptNumber,
		ReceiptID:        receiptID,
	}, nil
}

// planAllocations merencanakan alokasi dari anchor (tanpa lock — jalur non-atomik).
func (d *defaultCommitter) planAllocations(ctx context.Context, tenantID uint64, p PaymentCommitParams) ([]ScheduleAllocation, domain.Money, error) {
	switch {
	case p.ScheduleID != nil:
		sch, err := d.svc.contracts.FindScheduleByID(ctx, tenantID, *p.ScheduleID)
		if err != nil {
			return nil, domain.Zero, err
		}
		allocs, unapplied := planForSchedule(sch, p.Amount)
		return allocs, unapplied, nil
	case p.ContractID != nil:
		schedules, err := d.svc.contracts.ListSchedulesByContract(ctx, tenantID, *p.ContractID)
		if err != nil {
			return nil, domain.Zero, err
		}
		allocs, unapplied := planForContract(schedules, p.Amount)
		return allocs, unapplied, nil
	default:
		return nil, p.Amount, nil // tanpa kontrak → seluruhnya saldo kredit
	}
}

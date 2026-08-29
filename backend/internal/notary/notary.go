// Package notary — Titipan Notaris (UAT Batch 2 §3). JALUR MASUK DITUTUP (W-1).
//
// Uang customer yang dititipkan untuk biaya notaris adalah KEWAJIBAN developer,
// BUKAN pendapatan — tidak pernah menyentuh 4-xxxx. Itu tetap benar.
//
// Yang berubah di W-1: keputusan owner menetapkan Notaris sebagai SALAH SATU
// jenis Biaya Realisasi biasa, sederajat dengan BPHTB/PDAM/Listrik. Sejak itu
// modul ini kehilangan alasan eksistensinya — ia adalah jalur masuk KEDUA untuk
// uang yang jenisnya sama, dengan akun sendiri (2-2300), tanpa charge group,
// tanpa kwitansi KWR, dan tanpa kaitan ke kontrak. Dua jalur masuk untuk satu
// jenis uang berarti dua sumber kebenaran (TD-4).
//
//	SEKARANG — semua titipan biaya realisasi (termasuk notaris) masuk lewat
//	Charge Group: internal/charge, jenis biaya dari master realization_charge_types,
//	akun tujuan dari kolom deposit_account_code (default 2-2400 Titipan Realisasi,
//	sesuai bentuk jurnal yang ditetapkan owner: satu baris Titipan Realisasi).
//
// Modul ini menjadi LEGACY READ + DRAIN:
//
//	ReceiveDeposit : DITUTUP → ErrNotaryIntakeClosed (jalur masuk tunggal).
//	Payout         : TETAP TERBUKA — Dr 2-2300 / Cr Kas/Bank. Titipan lama yang
//	                 masih 'held' wajib bisa dibayarkan ke notaris; menutupnya
//	                 akan mengunci kewajiban nyata tanpa jalan keluar.
//	List           : TETAP TERBUKA — saldo lama harus tetap terlihat.
//
// Jurnal lama di 2-2300 TIDAK disentuh (ledger append-only, R-2/R-3). Saldo
// 2-2300 hanya bisa menyusut menuju nol seiring payout, dan EQ_NotaryDeposit
// (Σ deposit 'held' == saldo 2-2300) tetap berlaku sepanjang penyusutan itu.
package notary

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"esaproperti/internal/domain"
	"esaproperti/internal/ledger"
)

const accountCodeTitipanNotaris = "2-2300"

type DepositStatus string

const (
	DepositHeld    DepositStatus = "held"
	DepositPaidOut DepositStatus = "paid_out"
)

// NotaryDeposit adalah dokumen satu titipan notaris (append-only + status).
type NotaryDeposit struct {
	ID               uint64        `gorm:"primaryKey;autoIncrement"                     json:"id"`
	TenantID         uint64        `gorm:"not null;index"                               json:"-"`
	CustomerID       uint64        `gorm:"not null"                                     json:"customer_id"`
	UnitID           *uint64       `                                                    json:"unit_id,omitempty"`
	Amount           domain.Money  `gorm:"type:DECIMAL(20,4);not null;default:'0.0000'" json:"amount"`
	Status           DepositStatus `gorm:"not null;size:20;default:'held'"              json:"status"`
	NotaryName       string        `gorm:"size:200;not null;default:''"                 json:"notary_name"`
	Notes            string        `gorm:"size:500;not null;default:''"                 json:"notes,omitempty"`
	ReceiveJournalID uint64        `gorm:"not null"                                     json:"receive_journal_id"`
	PayoutJournalID  *uint64       `                                                    json:"payout_journal_id,omitempty"`
	ReceivedAt       time.Time     `gorm:"not null"                                     json:"received_at"`
	PaidOutAt        *time.Time    `                                                    json:"paid_out_at,omitempty"`
	CreatedBy        *uint64       `                                                    json:"created_by,omitempty"`
	CreatedAt        time.Time     `                                                    json:"created_at"`
	UpdatedAt        time.Time     `                                                    json:"updated_at"`
}

func (NotaryDeposit) TableName() string { return "notary_deposits" }

var (
	ErrDepositNotFound      = errors.New("titipan notaris tidak ditemukan")
	ErrDepositNotHeld       = errors.New("titipan notaris sudah dibayarkan (bukan status held)")
	ErrDepositAmountInvalid = errors.New("amount harus rupiah bulat dan lebih besar dari nol")
	ErrNotaryAccountMissing = errors.New("akun Titipan Notaris (2-2300) belum ada: jalankan migration 000056 / seed COA")
	ErrCustomerRequired     = errors.New("customer_id wajib diisi")

	// ErrNotaryIntakeClosed — W-1. Lihat catatan paket: jalur masuk titipan
	// notaris disatukan ke Charge Group supaya hanya ada SATU sumber kebenaran.
	ErrNotaryIntakeClosed = errors.New(
		"jalur titipan notaris terpisah sudah ditutup: catat biaya notaris sebagai item Charge Group " +
			"dengan jenis biaya 'notaris' (POST /charges). Titipan lama tetap bisa dibayarkan lewat payout")
)

// Service adalah SATU-SATUNYA penulis jurnal titipan notaris.
type Service struct {
	db *gorm.DB
}

func NewService(db *gorm.DB) *Service { return &Service{db: db} }

type ReceiveDepositRequest struct {
	CustomerID      uint64
	UnitID          *uint64
	Amount          domain.Money
	BankAccountCode string
	NotaryName      string
	Notes           string
	ReceivedAt      time.Time
	CreatedBy       *uint64
}

func (s *Service) accountID(ctx context.Context, tx *gorm.DB, tenantID uint64, code string) (uint64, error) {
	var id uint64
	if err := tx.WithContext(ctx).Raw(
		`SELECT id FROM accounts WHERE tenant_id = ? AND code = ? AND is_active = 1`,
		tenantID, code).Scan(&id).Error; err != nil {
		return 0, err
	}
	return id, nil
}

// cashBankAccountID memuat + memvalidasi akun kas/bank COA-driven
// (ledger.ValidatePaymentAccount — otoritas klasifikasi tunggal).
func (s *Service) cashBankAccountID(ctx context.Context, tx *gorm.DB, tenantID uint64, code string) (uint64, error) {
	var acc ledger.Account
	if err := tx.WithContext(ctx).
		Where("tenant_id = ? AND code = ?", tenantID, code).
		First(&acc).Error; err != nil {
		return 0, fmt.Errorf("akun bank %s tidak ditemukan", code)
	}
	if err := ledger.ValidatePaymentAccount(&acc); err != nil {
		return 0, err
	}
	return acc.ID, nil
}

// ReceiveDeposit DITUTUP sejak W-1 — lihat catatan paket. Uang titipan notaris
// yang BARU masuk lewat Charge Group (internal/charge), bukan lewat sini.
//
// Ditutup di service, bukan hanya di route, supaya tidak ada pemanggil internal
// yang diam-diam membuka kembali jalur kedua.
func (s *Service) ReceiveDeposit(ctx context.Context, tenantID uint64, req ReceiveDepositRequest) (*NotaryDeposit, error) {
	return nil, ErrNotaryIntakeClosed
}

// receiveDepositLegacy adalah implementasi lama yang dipertahankan HANYA untuk
// menumbuhkan data pra-W-1 di test (bukti bahwa payout & EQ_NotaryDeposit masih
// bekerja atas titipan warisan). Tidak dipanggil kode produksi mana pun.
func (s *Service) receiveDepositLegacy(ctx context.Context, tenantID uint64, req ReceiveDepositRequest) (*NotaryDeposit, error) {
	if req.CustomerID == 0 {
		return nil, ErrCustomerRequired
	}
	if req.Amount.IsZero() || req.Amount.IsNeg() || !req.Amount.IsWholeRupiah() {
		return nil, ErrDepositAmountInvalid
	}
	if req.ReceivedAt.IsZero() {
		req.ReceivedAt = time.Now()
	}
	var out *NotaryDeposit
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		txLedger := ledger.NewGORMRepository(tx)
		posting := ledger.NewPostingService(txLedger, txLedger).WithPeriodChecker(txLedger)
		// Validasi akun kas/bank COA-driven (otoritas kategori tunggal).
		bankID, err := s.cashBankAccountID(ctx, tx, tenantID, req.BankAccountCode)
		if err != nil {
			return err
		}
		titipanID, err := s.accountID(ctx, tx, tenantID, accountCodeTitipanNotaris)
		if err != nil || titipanID == 0 {
			return ErrNotaryAccountMissing
		}
		// Jalur intake ini sudah ditutup untuk produksi (W-1, HTTP 410); yang
		// tersisa hanya penumbuh data warisan di test. Tetap diberi BKM supaya
		// data yang lahir darinya patuh INV-DOC-1 seperti jalur hidup lainnya.
		entry, err := posting.CreateAndPost(ctx, ledger.CreateJournalRequest{
			TenantID:    tenantID,
			Date:        req.ReceivedAt,
			Description: fmt.Sprintf("Titipan notaris diterima — %s", req.NotaryName),
			CreatedBy:   req.CreatedBy,
			Lines: []ledger.LineInput{
				{AccountID: bankID, Debit: req.Amount, UnitID: req.UnitID, Description: "Terima titipan notaris"},
				{AccountID: titipanID, Credit: req.Amount, UnitID: req.UnitID, Description: "Titipan Notaris (kewajiban — bukan pendapatan)"},
			},
			Document: ledger.DocumentSpec{TypeCode: ledger.DocCashIn},
		})
		if err != nil {
			return fmt.Errorf("posting jurnal titipan notaris: %w", err)
		}
		d := &NotaryDeposit{
			TenantID:         tenantID,
			CustomerID:       req.CustomerID,
			UnitID:           req.UnitID,
			Amount:           req.Amount,
			Status:           DepositHeld,
			NotaryName:       req.NotaryName,
			Notes:            req.Notes,
			ReceiveJournalID: entry.ID,
			ReceivedAt:       req.ReceivedAt,
			CreatedBy:        req.CreatedBy,
		}
		if err := tx.WithContext(ctx).Create(d).Error; err != nil {
			return fmt.Errorf("simpan titipan notaris: %w", err)
		}
		out = d
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

type PayoutRequest struct {
	BankAccountCode string
	PaidOutAt       time.Time
	Notes           string
	ActorID         *uint64
}

// Payout membayarkan titipan ke notaris: Dr 2-2300 / Cr Kas/Bank + status
// paid_out — SATU transaksi, pinned ke status held (idempoten/anti-double-pay).
func (s *Service) Payout(ctx context.Context, tenantID, depositID uint64, req PayoutRequest) (*NotaryDeposit, error) {
	if req.PaidOutAt.IsZero() {
		req.PaidOutAt = time.Now()
	}
	var out *NotaryDeposit
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var d NotaryDeposit
		if err := tx.WithContext(ctx).
			Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ? AND tenant_id = ?", depositID, tenantID).
			First(&d).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrDepositNotFound
			}
			return err
		}
		if d.Status != DepositHeld {
			return ErrDepositNotHeld
		}
		txLedger := ledger.NewGORMRepository(tx)
		posting := ledger.NewPostingService(txLedger, txLedger).WithPeriodChecker(txLedger)
		bankID, err := s.cashBankAccountID(ctx, tx, tenantID, req.BankAccountCode)
		if err != nil {
			return err
		}
		titipanID, err := s.accountID(ctx, tx, tenantID, accountCodeTitipanNotaris)
		if err != nil || titipanID == 0 {
			return ErrNotaryAccountMissing
		}
		// BTP — yang keluar adalah titipan milik customer yang diteruskan ke
		// notaris, bukan kas perusahaan (economic ownership, D-W3-6).
		entry, err := posting.CreateAndPost(ctx, ledger.CreateJournalRequest{
			TenantID:    tenantID,
			Date:        req.PaidOutAt,
			Description: fmt.Sprintf("Titipan notaris #%d dibayarkan — %s", d.ID, d.NotaryName),
			CreatedBy:   req.ActorID,
			Lines: []ledger.LineInput{
				{AccountID: titipanID, Debit: d.Amount, UnitID: d.UnitID, Description: "Pelepasan titipan notaris"},
				{AccountID: bankID, Credit: d.Amount, UnitID: d.UnitID, Description: "Pembayaran ke notaris"},
			},
			Document: ledger.DocumentSpec{TypeCode: ledger.DocThirdPartyPayout},
		})
		if err != nil {
			return fmt.Errorf("posting jurnal pembayaran notaris: %w", err)
		}
		res := tx.WithContext(ctx).Model(&NotaryDeposit{}).
			Where("id = ? AND tenant_id = ? AND status = ?", d.ID, tenantID, string(DepositHeld)).
			Updates(map[string]interface{}{
				"status":            string(DepositPaidOut),
				"payout_journal_id": entry.ID,
				"paid_out_at":       req.PaidOutAt,
			})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return ErrDepositNotHeld
		}
		d.Status = DepositPaidOut
		d.PayoutJournalID = &entry.ID
		d.PaidOutAt = &req.PaidOutAt
		out = &d
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// List mengembalikan titipan tenant (opsional filter status).
func (s *Service) List(ctx context.Context, tenantID uint64, status DepositStatus) ([]*NotaryDeposit, error) {
	q := s.db.WithContext(ctx).Where("tenant_id = ?", tenantID)
	if status != "" {
		q = q.Where("status = ?", string(status))
	}
	var out []*NotaryDeposit
	if err := q.Order("id DESC").Find(&out).Error; err != nil {
		return nil, fmt.Errorf("list titipan notaris: %w", err)
	}
	return out, nil
}

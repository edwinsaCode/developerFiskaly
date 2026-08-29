package sale

// W-4 / TD-1 — Port transaksional sub-ledger kas & alokasi.
//
// `termin_payments` dan `payment_allocations` adalah tabel milik paket ini.
// Sebelum W-4 paket `charge` menulis keduanya sendiri dengan `tx.Create(&sale.
// TerminPayment{…})`. Akibatnya invarian sub-ledger tersebar: nilai
// `CountsTowardPrice: false` untuk penerimaan realisasi ditulis di tiga tempat
// berbeda di luar paket ini, dan satu kali lupa berarti titipan customer tampil
// sebagai pembayaran harga rumah — Laba Rugi ikut salah, tanpa error apa pun.
//
// Port di bawah menerima `*gorm.DB` transaksi MILIK PEMANGGIL, jadi atomisitas
// charge tetap utuh: charge menyatakan MAKSUD ("terima titipan realisasi untuk
// grup ini"), sale yang menegakkan bentuk barisnya.
//
// INV-OWN-1: di luar paket `sale` tidak boleh ada `sale.TerminPayment{` atau
// `sale.PaymentAllocation{` sebagai literal komposit.

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"esaproperti/internal/domain"
)

// ErrNotChargeReceipt dikembalikan bila termin yang dirujuk bukan penerimaan
// milik sebuah charge group.
var ErrNotChargeReceipt = errors.New("sale: termin bukan penerimaan charge group")

// ChargeReceiptInput adalah maksud "catat penerimaan untuk sebuah charge group".
//
// Yang SENGAJA tidak ada di sini: CountsTowardPrice. Penerimaan charge group
// tidak pernah mengurangi harga rumah (K-1), jadi nilainya bukan pilihan
// pemanggil — ia ditetapkan di bawah dan tidak bisa dilewatkan keliru.
type ChargeReceiptInput struct {
	TenantID          uint64
	UnitID            uint64
	ProjectID         uint64
	PhaseID           *uint64
	ChargeGroupID     uint64
	Amount            domain.Money
	BankAccountCode   string
	Date              time.Time
	Description       string
	JournalEntryID    uint64
	CreditAccountCode string
	CreatedBy         *uint64
	IdempotencyKey    string
	// Source membedakan penerimaan KAS realisasi dari perpindahan titipan antar
	// grup (non-kas). Hanya dua nilai itu yang diterima.
	Source PaymentSource
}

// ChargeAllocationInput adalah satu instruksi alokasi ke item tagihan.
type ChargeAllocationInput struct {
	ChargeItemID uint64
	Amount       domain.Money
}

// RecordChargeReceiptInTx menulis satu baris `termin_payments` untuk penerimaan
// sebuah charge group dan mengembalikan id-nya.
func RecordChargeReceiptInTx(ctx context.Context, tx *gorm.DB, in ChargeReceiptInput) (uint64, error) {
	switch in.Source {
	case PaymentSourceRealization, PaymentSourceRealizationTransfer:
	default:
		return 0, fmt.Errorf("%w: payment_source %q tidak sah untuk charge group",
			ErrNotChargeReceipt, in.Source)
	}
	if in.ChargeGroupID == 0 {
		return 0, fmt.Errorf("%w: charge_group_id kosong", ErrNotChargeReceipt)
	}

	gid := in.ChargeGroupID
	row := &TerminPayment{
		TenantID:          in.TenantID,
		UnitID:            in.UnitID,
		ProjectID:         in.ProjectID,
		PhaseID:           in.PhaseID,
		Amount:            in.Amount,
		BankAccountCode:   in.BankAccountCode,
		Date:              in.Date,
		Description:       in.Description,
		JournalEntryID:    in.JournalEntryID,
		CreditAccountCode: in.CreditAccountCode,
		CreatedBy:         in.CreatedBy,
		IdempotencyKey:    idempotencyPtr(in.IdempotencyKey),
		PaymentSource:     in.Source,
		Kind:              TerminKindOther,
		// K-1: penerimaan charge group ada DI LUAR harga rumah — selalu, tanpa
		// pengecualian. Ditetapkan di sini supaya tidak ada pemanggil yang bisa
		// salah mengisinya.
		CountsTowardPrice: false,
		ChargeGroupID:     &gid,
	}
	if err := tx.WithContext(ctx).Create(row).Error; err != nil {
		return 0, fmt.Errorf("simpan penerimaan charge group: %w", err)
	}
	return row.ID, nil
}

// RecordChargeAllocationsInTx menulis sub-ledger alokasi penerimaan ke item-item
// tagihan (K-3 — keputusan admin, satu tabel yang sama dengan alokasi cicilan).
func RecordChargeAllocationsInTx(ctx context.Context, tx *gorm.DB, tenantID, terminID uint64, allocs []ChargeAllocationInput, createdBy *uint64) error {
	for _, a := range allocs {
		itemID := a.ChargeItemID
		tid := terminID
		row := &PaymentAllocation{
			TenantID:        tenantID,
			TerminPaymentID: &tid,
			ChargeItemID:    &itemID,
			AllocationType:  AllocationTypeChargeItem,
			Amount:          a.Amount,
			CreatedBy:       createdBy,
		}
		if err := tx.WithContext(ctx).Create(row).Error; err != nil {
			return fmt.Errorf("simpan alokasi item #%d: %w", itemID, err)
		}
	}
	return nil
}

// ListChargeAllocationsInTx mengembalikan alokasi ASLI (bukan mirror void) satu
// penerimaan — sumber kebenaran akun mana saja yang dulu dikredit.
func ListChargeAllocationsInTx(ctx context.Context, tx *gorm.DB, tenantID, terminID uint64) ([]PaymentAllocation, error) {
	var out []PaymentAllocation
	if err := tx.WithContext(ctx).
		Where("tenant_id = ? AND termin_payment_id = ? AND allocation_type = ?",
			tenantID, terminID, string(AllocationTypeChargeItem)).
		Find(&out).Error; err != nil {
		return nil, fmt.Errorf("baca alokasi penerimaan #%d: %w", terminID, err)
	}
	return out, nil
}

// RecordChargeVoidMirrorsInTx menulis mirror NEGATIF untuk setiap alokasi asli.
//
// Invariant #5 (append-only): histori alokasi asli TIDAK diubah atau dihapus —
// pembatalan adalah baris baru bertanda berlawanan. Tanda negatifnya dipasang
// di sini, bukan dititipkan ke pemanggil, karena mirror bertanda positif akan
// MENAMBAH pembayaran alih-alih membatalkannya dan tidak ada constraint DB yang
// akan menangkapnya.
func RecordChargeVoidMirrorsInTx(ctx context.Context, tx *gorm.DB, tenantID uint64, originals []PaymentAllocation, createdBy *uint64) error {
	for _, a := range originals {
		neg := domain.Zero.Sub(a.Amount)
		mirror := &PaymentAllocation{
			TenantID:        tenantID,
			TerminPaymentID: a.TerminPaymentID,
			ChargeItemID:    a.ChargeItemID,
			AllocationType:  AllocationTypeChargeItemVoid,
			Amount:          neg,
			CreatedBy:       createdBy,
		}
		if err := tx.WithContext(ctx).Create(mirror).Error; err != nil {
			return fmt.Errorf("simpan mirror void alokasi #%d: %w", a.ID, err)
		}
	}
	return nil
}

// FindChargeReceiptByIdempotencyKeyInTx mencari penerimaan yang sudah pernah
// ditulis dengan kunci idempotency yang sama. Nil,nil bila belum ada.
func FindChargeReceiptByIdempotencyKeyInTx(ctx context.Context, tx *gorm.DB, tenantID uint64, key string) (*TerminPayment, error) {
	if key == "" {
		return nil, nil
	}
	var t TerminPayment
	err := tx.WithContext(ctx).
		Where("tenant_id = ? AND idempotency_key = ?", tenantID, key).
		First(&t).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("cek idempotency: %w", err)
	}
	return &t, nil
}

// FindChargeReceiptForUpdateInTx mengunci dan mengembalikan satu penerimaan
// charge group. Menolak termin yang bukan penerimaan realisasi berkas.
func FindChargeReceiptForUpdateInTx(ctx context.Context, tx *gorm.DB, tenantID, terminID uint64) (*TerminPayment, error) {
	var t TerminPayment
	if err := tx.WithContext(ctx).
		Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("id = ? AND tenant_id = ?", terminID, tenantID).
		First(&t).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotChargeReceipt
		}
		return nil, err
	}
	if t.ChargeGroupID == nil || t.PaymentSource != PaymentSourceRealization {
		return nil, ErrNotChargeReceipt
	}
	return &t, nil
}

// idempotencyPtr mengubah kunci kosong menjadi NULL — kolomnya unik per tenant,
// dan string kosong yang ditulis berulang akan bentrok.
func idempotencyPtr(k string) *string {
	if k == "" {
		return nil
	}
	return &k
}

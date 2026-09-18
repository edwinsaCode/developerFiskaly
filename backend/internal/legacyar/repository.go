package legacyar

import (
	"context"
	"errors"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"esaproperti/internal/domain"
	"esaproperti/internal/ledger"
)

// Repository — akses DB untuk piutang proyek lama.
//
// Setiap query menyertakan predikat `tenant_id = ?` secara eksplisit; itulah
// cara isolasi tenant ditegakkan di repo ini, sama seperti paket lain.
type Repository struct {
	db *gorm.DB
}

func NewRepository(db *gorm.DB) *Repository { return &Repository{db: db} }

// WithTx mengembalikan repository yang terikat pada transaksi berjalan.
func (r *Repository) WithTx(tx *gorm.DB) *Repository { return &Repository{db: tx} }

func (r *Repository) DB() *gorm.DB { return r.db }

// ── Batch ─────────────────────────────────────────────────────────────────────

func (r *Repository) CreateBatch(ctx context.Context, b *Batch) error {
	return r.db.WithContext(ctx).Create(b).Error
}

func (r *Repository) SaveBatch(ctx context.Context, b *Batch) error {
	return r.db.WithContext(ctx).Save(b).Error
}

// FindBatch mengambil satu batch. forUpdate mengunci barisnya — dipakai commit
// supaya dua tab browser tidak meng-commit batch yang sama serentak.
func (r *Repository) FindBatch(ctx context.Context, tenantID, id uint64, forUpdate bool) (*Batch, error) {
	q := r.db.WithContext(ctx).Where("tenant_id = ? AND id = ?", tenantID, id)
	if forUpdate {
		q = q.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	var b Batch
	if err := q.First(&b).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrBatchNotFound
		}
		return nil, err
	}
	return &b, nil
}

func (r *Repository) ListBatches(ctx context.Context, tenantID uint64, limit int) ([]Batch, error) {
	if limit <= 0 {
		limit = 50
	}
	var out []Batch
	err := r.db.WithContext(ctx).
		Where("tenant_id = ?", tenantID).
		Order("id DESC").
		Limit(limit).
		Find(&out).Error
	return out, err
}

// FindCommittedByHash mencari batch SUDAH TER-COMMIT dengan isi berkas identik.
// Inilah lapis idempotensi pertama: berkas yang sama tidak boleh masuk dua kali
// walau namanya diganti.
func (r *Repository) FindCommittedByHash(ctx context.Context, tenantID uint64, hash string) (*Batch, error) {
	var b Batch
	err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND committed_hash = ?", tenantID, hash).
		First(&b).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &b, nil
}

// ── Baris batch ───────────────────────────────────────────────────────────────

func (r *Repository) InsertBatchRows(ctx context.Context, rows []BatchRow) error {
	if len(rows) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).CreateInBatches(&rows, 200).Error
}

func (r *Repository) ListBatchRows(ctx context.Context, tenantID, batchID uint64) ([]BatchRow, error) {
	var out []BatchRow
	err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND batch_id = ?", tenantID, batchID).
		Order("line_no ASC").
		Find(&out).Error
	return out, err
}

func (r *Repository) LinkBatchRow(ctx context.Context, tenantID, rowID, receivableID uint64) error {
	return r.db.WithContext(ctx).
		Model(&BatchRow{}).
		Where("tenant_id = ? AND id = ?", tenantID, rowID).
		Update("legacy_receivable_id", receivableID).Error
}

// ── Piutang ───────────────────────────────────────────────────────────────────

func (r *Repository) CreateReceivable(ctx context.Context, rec *Receivable) error {
	return r.db.WithContext(ctx).Create(rec).Error
}

// FindReceivable mengambil satu piutang. forUpdate mengunci barisnya — WAJIB
// dipakai jalur pelunasan: tanpa kunci, dua pembayaran serentak bisa sama-sama
// membaca sisa yang sama dan keduanya lolos pemeriksaan lebih bayar.
func (r *Repository) FindReceivable(ctx context.Context, tenantID, id uint64, forUpdate bool) (*Receivable, error) {
	q := r.db.WithContext(ctx).Where("tenant_id = ? AND id = ?", tenantID, id)
	if forUpdate {
		q = q.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	var rec Receivable
	if err := q.First(&rec).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrReceivableNotFound
		}
		return nil, err
	}
	return &rec, nil
}

func (r *Repository) SaveReceivable(ctx context.Context, rec *Receivable) error {
	return r.db.WithContext(ctx).Save(rec).Error
}

// ListFilter menyaring daftar piutang lama.
type ListFilter struct {
	Status      Status
	Search      string
	SourceLabel string
	BatchID     uint64
	OnlyOpen    bool
}

func (r *Repository) ListReceivables(ctx context.Context, tenantID uint64, f ListFilter) ([]Receivable, error) {
	q := r.db.WithContext(ctx).Where("tenant_id = ?", tenantID)
	if f.Status != "" {
		q = q.Where("status = ?", f.Status)
	}
	if f.OnlyOpen {
		q = q.Where("status = ?", StatusOpen)
	}
	if f.BatchID > 0 {
		q = q.Where("batch_id = ?", f.BatchID)
	}
	if f.SourceLabel != "" {
		q = q.Where("source_label = ?", f.SourceLabel)
	}
	if s := f.Search; s != "" {
		like := "%" + s + "%"
		q = q.Where("(customer_name LIKE ? OR source_label LIKE ? OR external_ref LIKE ?)", like, like, like)
	}
	var out []Receivable
	err := q.Order("customer_name ASC, id ASC").Find(&out).Error
	return out, err
}

// ExternalRefExists memeriksa apakah nomor rujukan sudah dipakai. Unique key di
// DB tetap penjaga terakhir; ini hanya supaya pratinjau bisa memberi tahu lebih
// awal, dengan pesan yang menyebut nomornya.
func (r *Repository) ExternalRefExists(ctx context.Context, tenantID uint64, refs []string) (map[string]bool, error) {
	out := map[string]bool{}
	if len(refs) == 0 {
		return out, nil
	}
	var found []string
	err := r.db.WithContext(ctx).
		Model(&Receivable{}).
		Where("tenant_id = ? AND external_ref IN ?", tenantID, refs).
		Pluck("external_ref", &found).Error
	if err != nil {
		return nil, err
	}
	for _, f := range found {
		out[f] = true
	}
	return out, nil
}

// SumOutstanding menjumlahkan sisa piutang lama yang masih terbuka.
//
// Dihitung di DB dengan DECIMAL, bukan di Go setelah memuat semua baris: ini
// dipakai rekonsiliasi terhadap buku besar, dan angkanya harus tepat sampai
// rupiah terakhir walau barisnya puluhan ribu.
func (r *Repository) SumOutstanding(ctx context.Context, tenantID uint64) (domain.Money, error) {
	var out domain.Money
	row := r.db.WithContext(ctx).Raw(`
		SELECT COALESCE(SUM(original_amount - paid_amount), 0)
		FROM legacy_receivables
		WHERE tenant_id = ? AND status <> ?`, tenantID, StatusWrittenOff).Row()
	if err := row.Scan(&out); err != nil {
		return domain.Zero, err
	}
	return out, nil
}

// SumOriginal adalah Σ NILAI ASLI seluruh piutang lama tenant — pembanding
// terhadap porsi saldo awal di buku besar.
//
// Nilai asli, bukan sisa: yang dibandingkan adalah SALDO AWAL, dan saldo awal
// tidak ikut mengecil ketika customer mencicil (jurnal pembukanya immutable).
// Pelunasannya sudah punya barisnya sendiri di rekonsiliasi.
//
// Semua status ikut, termasuk written_off. Penghapusbukuan belum punya jurnal di
// W-7 (BD-2), jadi mengeluarkannya dari sini akan memunculkan selisih semu
// sebesar piutang yang dihapus — selisih yang tidak dicerminkan apa pun di buku.
func (r *Repository) SumOriginal(ctx context.Context, tenantID uint64) (domain.Money, error) {
	var out domain.Money
	row := r.db.WithContext(ctx).Raw(`
		SELECT COALESCE(SUM(original_amount), 0)
		FROM legacy_receivables
		WHERE tenant_id = ?`, tenantID).Row()
	if err := row.Scan(&out); err != nil {
		return domain.Zero, err
	}
	return out, nil
}

// ── Pelunasan ─────────────────────────────────────────────────────────────────

func (r *Repository) CreatePayment(ctx context.Context, p *Payment) error {
	return r.db.WithContext(ctx).Create(p).Error
}

// SavePaymentReceipt menempelkan referensi kwitansi (KWL) ke baris pembayaran
// yang sudah dibuat. Kwitansi lahir SETELAH baris pembayaran diinsert (ia
// menunjuk legacy_receivable_payment_id), jadi ini selalu UPDATE susulan di
// transaksi yang sama — bukan write kedua yang terpisah dari jurnalnya.
func (r *Repository) SavePaymentReceipt(ctx context.Context, tenantID, paymentID, receiptID uint64, receiptNumber string) error {
	return r.db.WithContext(ctx).Model(&Payment{}).
		Where("tenant_id = ? AND id = ?", tenantID, paymentID).
		Updates(map[string]any{"receipt_id": receiptID, "receipt_number": receiptNumber}).Error
}

func (r *Repository) FindPayment(ctx context.Context, tenantID, id uint64, forUpdate bool) (*Payment, error) {
	q := r.db.WithContext(ctx).Where("tenant_id = ? AND id = ?", tenantID, id)
	if forUpdate {
		q = q.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	var p Payment
	if err := q.First(&p).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrPaymentNotFound
		}
		return nil, err
	}
	return &p, nil
}

// FindPaymentByIdempotencyKey mengembalikan pelunasan yang sudah terbentuk dari
// kunci yang sama, atau nil. Dipakai supaya klik ganda / retry jaringan tidak
// menghasilkan dua penerimaan kas.
func (r *Repository) FindPaymentByIdempotencyKey(ctx context.Context, tenantID uint64, key string) (*Payment, error) {
	if key == "" {
		return nil, nil
	}
	var p Payment
	err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND idempotency_key = ?", tenantID, key).
		First(&p).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &p, nil
}

// VoidExists melaporkan apakah sebuah pelunasan sudah pernah dibatalkan.
func (r *Repository) VoidExists(ctx context.Context, tenantID, paymentID uint64) (bool, error) {
	var n int64
	err := r.db.WithContext(ctx).
		Model(&Payment{}).
		Where("tenant_id = ? AND voids_payment_id = ?", tenantID, paymentID).
		Limit(1).Count(&n).Error
	return n > 0, err
}

func (r *Repository) ListPayments(ctx context.Context, tenantID, receivableID uint64) ([]Payment, error) {
	var out []Payment
	err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND legacy_receivable_id = ?", tenantID, receivableID).
		Order("id ASC").
		Find(&out).Error
	return out, err
}

// ListPaymentsFor mengambil pelunasan beberapa piutang sekaligus (hindari N+1
// di layar daftar).
func (r *Repository) ListPaymentsFor(ctx context.Context, tenantID uint64, ids []uint64) (map[uint64][]Payment, error) {
	out := map[uint64][]Payment{}
	if len(ids) == 0 {
		return out, nil
	}
	var rows []Payment
	err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND legacy_receivable_id IN ?", tenantID, ids).
		Order("id ASC").
		Find(&rows).Error
	if err != nil {
		return nil, err
	}
	for _, p := range rows {
		out[p.LegacyReceivableID] = append(out[p.LegacyReceivableID], p)
	}
	return out, nil
}

// SumPayments menjumlahkan pelunasan (termasuk baris pembatalan yang negatif)
// atas satu piutang. Inilah SUMBER KEBENARAN untuk paid_amount — kolom cache di
// legacy_receivables hanya proyeksinya (INV-LAR-3).
func (r *Repository) SumPayments(ctx context.Context, tenantID, receivableID uint64) (domain.Money, error) {
	var out domain.Money
	row := r.db.WithContext(ctx).Raw(`
		SELECT COALESCE(SUM(amount), 0)
		FROM legacy_receivable_payments
		WHERE tenant_id = ? AND legacy_receivable_id = ?`, tenantID, receivableID).Row()
	if err := row.Scan(&out); err != nil {
		return domain.Zero, err
	}
	return out, nil
}

// ── Audit ─────────────────────────────────────────────────────────────────────

func (r *Repository) AppendAudit(ctx context.Context, a *Audit) error {
	return r.db.WithContext(ctx).Create(a).Error
}

func (r *Repository) ListAudits(ctx context.Context, tenantID, receivableID uint64) ([]Audit, error) {
	var out []Audit
	err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND legacy_receivable_id = ?", tenantID, receivableID).
		Order("id ASC").
		Find(&out).Error
	return out, err
}

// ── Bacaan lintas-paket (read-only) ───────────────────────────────────────────

// FindAccountByCode mengambil akun COA berdasarkan kode.
func (r *Repository) FindAccountByCode(ctx context.Context, tenantID uint64, code string) (*ledger.Account, error) {
	var a ledger.Account
	err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND code = ?", tenantID, code).
		First(&a).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &a, nil
}

// DistinctSourceLabels mengembalikan daftar proyek/sumber lama untuk filter.
func (r *Repository) DistinctSourceLabels(ctx context.Context, tenantID uint64) ([]string, error) {
	var out []string
	err := r.db.WithContext(ctx).
		Model(&Receivable{}).
		Where("tenant_id = ?", tenantID).
		Distinct().
		Order("source_label ASC").
		Pluck("source_label", &out).Error
	return out, err
}

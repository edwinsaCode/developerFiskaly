package ap

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"esaproperti/internal/cost"
	"esaproperti/internal/domain"
	"esaproperti/internal/ledger"
)

// Repository adalah akses DB paket hutang usaha.
//
// Setiap query menuliskan `tenant_id = ?` SECARA EKSPLISIT (INV-AP-7). Tidak
// ada satu pun method di sini yang boleh menerima "ambil semua lalu saring di
// Go" — penyaringan di aplikasi berarti baris tenant lain sempat berada di
// memori proses, dan satu `return` yang lupa menyaring cukup untuk membocorkan
// daftar vendor beserta nominal tagihannya.
type Repository struct{ db *gorm.DB }

func NewRepository(db *gorm.DB) *Repository { return &Repository{db: db} }

// WithTx mengembalikan repository yang terikat pada satu transaksi.
//
// Pola ini (bukan menyimpan tx di dalam service) dipakai karena satu transaksi
// AP menulis ke empat tabel yang berbeda lewat kolaborator yang berbeda pula:
// jurnal lewat ledger.PostingService, baris biaya lewat model cost, sisanya
// lewat repo ini. Kalau salah satu di antaranya diam-diam memakai koneksi di
// luar transaksi, yang tertinggal saat rollback adalah data yang setengah jadi
// dan tidak bisa ditelusuri dari mana pun.
func (r *Repository) WithTx(tx *gorm.DB) *Repository { return &Repository{db: tx} }

// ── Akun ──────────────────────────────────────────────────────────────────────

// AccountByCode mencari akun COA milik tenant. Gagal terang-terangan bila tidak
// ada: menjurnal ke akun yang ditebak jauh lebih mahal daripada menolak.
func (r *Repository) AccountByCode(ctx context.Context, tenantID uint64, code string) (*ledger.Account, error) {
	if code == "" {
		return nil, fmt.Errorf("%w: kode akun kosong", ErrAccountNotFound)
	}
	var a ledger.Account
	err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND code = ?", tenantID, code).
		First(&a).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, fmt.Errorf("%w: %s", ErrAccountNotFound, code)
	}
	if err != nil {
		return nil, err
	}
	return &a, nil
}

// ── Vendor ────────────────────────────────────────────────────────────────────

func (r *Repository) CreateVendor(ctx context.Context, v *Vendor) error {
	return r.db.WithContext(ctx).Create(v).Error
}

func (r *Repository) SaveVendor(ctx context.Context, v *Vendor) error {
	return r.db.WithContext(ctx).Save(v).Error
}

func (r *Repository) FindVendor(ctx context.Context, tenantID, id uint64) (*Vendor, error) {
	var v Vendor
	err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND id = ?", tenantID, id).
		First(&v).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrVendorNotFound
	}
	if err != nil {
		return nil, err
	}
	return &v, nil
}

// VendorFilter menyaring daftar vendor. ActiveOnly nil = semua.
type VendorFilter struct {
	Search     string
	ActiveOnly *bool
}

func (r *Repository) ListVendors(ctx context.Context, tenantID uint64, f VendorFilter) ([]*Vendor, error) {
	q := r.db.WithContext(ctx).Model(&Vendor{}).Where("tenant_id = ?", tenantID)
	if f.ActiveOnly != nil {
		q = q.Where("is_active = ?", *f.ActiveOnly)
	}
	if f.Search != "" {
		q = q.Where("name LIKE ?", "%"+f.Search+"%")
	}
	var out []*Vendor
	if err := q.Order("name ASC").Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

// ── Tagihan ───────────────────────────────────────────────────────────────────

func (r *Repository) CreateInvoice(ctx context.Context, inv *Invoice) error {
	return r.db.WithContext(ctx).Create(inv).Error
}

// FindInvoice mengambil satu tagihan. `lock` menambahkan SELECT … FOR UPDATE.
//
// Kunci baris dipakai pada post/reverse, bukan pada pembacaan biasa. Tanpa itu
// dua permintaan post yang berbarengan sama-sama membaca status `draft` dan
// keduanya lolos pemeriksaan — menghasilkan dua jurnal pengakuan untuk satu
// kewajiban, yang keduanya balanced dan karena itu tidak akan pernah memunculkan
// error di laporan mana pun.
func (r *Repository) FindInvoice(ctx context.Context, tenantID, id uint64, lock bool) (*Invoice, error) {
	q := r.db.WithContext(ctx)
	if lock {
		q = q.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	var inv Invoice
	err := q.Where("tenant_id = ? AND id = ?", tenantID, id).First(&inv).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrInvoiceNotFound
	}
	if err != nil {
		return nil, err
	}
	return &inv, nil
}

// InvoiceFilter menyaring daftar tagihan.
type InvoiceFilter struct {
	VendorID  uint64
	ProjectID uint64
	Status    InvoiceStatus
	DueBefore *time.Time
}

func (r *Repository) ListInvoices(ctx context.Context, tenantID uint64, f InvoiceFilter) ([]*Invoice, error) {
	q := r.db.WithContext(ctx).Model(&Invoice{}).Where("tenant_id = ?", tenantID)
	if f.VendorID != 0 {
		q = q.Where("vendor_id = ?", f.VendorID)
	}
	if f.ProjectID != 0 {
		q = q.Where("project_id = ?", f.ProjectID)
	}
	if f.Status != "" {
		q = q.Where("status = ?", f.Status)
	}
	if f.DueBefore != nil {
		q = q.Where("due_date <= ?", *f.DueBefore)
	}
	var out []*Invoice
	if err := q.Order("invoice_date DESC, id DESC").Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

// UpdateInvoice menyimpan perubahan kolom tertentu pada satu tagihan.
//
// Sengaja map kolom, bukan Save(struct): Save akan menulis ulang SELURUH kolom
// termasuk nominal — dan perubahan status tidak pernah boleh menjadi jalan
// masuk yang mengubah angka. Yang berubah pada post/reverse hanyalah status,
// stempel waktu, dan tautan jurnal pembalik.
func (r *Repository) UpdateInvoice(ctx context.Context, tenantID, id uint64, fields map[string]any) error {
	res := r.db.WithContext(ctx).Model(&Invoice{}).
		Where("tenant_id = ? AND id = ?", tenantID, id).
		Updates(fields)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrInvoiceNotFound
	}
	return nil
}

// ── Baris biaya (di cost_entries — D-4) ──────────────────────────────────────

// CreateCostEntry menulis satu baris biaya milik tagihan.
//
// Modelnya milik package cost, BUKAN salinan lokal: `ap_invoice_lines` dilarang
// (D-4) karena budget.GetRealisasiPerItem membaca SUM(cost_entries.amount).
// Baris biaya yang hidup di tabel lain akan terlihat oleh laporan realisasi
// per-proyek (yang membaca ledger) tetapi tidak oleh realisasi per-item RAB —
// dua laporan yang keduanya "benar" menurut kodenya dan tidak sepakat.
func (r *Repository) CreateCostEntry(ctx context.Context, e *cost.CostEntry) error {
	return r.db.WithContext(ctx).Create(e).Error
}

// ListCostEntriesByInvoice mengembalikan baris biaya sebuah tagihan.
func (r *Repository) ListCostEntriesByInvoice(ctx context.Context, tenantID, invoiceID uint64) ([]*cost.CostEntry, error) {
	var out []*cost.CostEntry
	err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND ap_invoice_id = ?", tenantID, invoiceID).
		Order("id ASC").
		Find(&out).Error
	if err != nil {
		return nil, err
	}
	return out, nil
}

// ── Alokasi pembayaran (sub-ledger) ──────────────────────────────────────────

// SumAllocations menjumlahkan alokasi AKTIF (belum dibalik) sebuah tagihan.
//
// Inilah satu-satunya sumber "sisa tagihan" (D-12). Tidak ada kolom cache yang
// menyimpannya, jadi tidak ada dua angka yang bisa berselisih.
//
// Tahap 2 belum menulis satu pun baris ke tabel ini — tetapi pembacanya sudah
// hidup, dan gate pembalikan (INV-AP-6) memang bergantung padanya. Membangunnya
// belakangan berarti ada jendela waktu di mana tagihan berbayar bisa dibalik.
func (r *Repository) SumAllocations(ctx context.Context, tenantID, invoiceID uint64) (domain.Money, error) {
	var raw *string
	err := r.db.WithContext(ctx).Model(&Allocation{}).
		Where("tenant_id = ? AND invoice_id = ? AND reversed_at IS NULL", tenantID, invoiceID).
		Select("COALESCE(SUM(amount), 0)").
		Scan(&raw).Error
	if err != nil {
		return domain.Zero, err
	}
	if raw == nil || *raw == "" {
		return domain.Zero, nil
	}
	m, err := domain.NewMoney(*raw)
	if err != nil {
		return domain.Zero, fmt.Errorf("baca Σ alokasi tagihan %d: %w", invoiceID, err)
	}
	return m, nil
}

// SumAllocationsLocked menjumlahkan alokasi aktif sebuah tagihan lewat
// PEMBACAAN TERKUNCI. Ini bukan versi "lebih aman" dari SumAllocations — ini
// versi yang BENAR di dalam transaksi yang hendak menulis.
//
// Alasannya isolasi MySQL, bukan selera. Di REPEATABLE READ, `SELECT … FOR
// UPDATE` atas baris tagihan memang membaca versi terbaru — tetapi pembacaan
// biasa atas tabel lain tetap dilayani dari SNAPSHOT yang dibuka pada query
// pertama transaksi itu. Akibatnya transaksi yang sudah selesai menunggu kunci
// tagihan tetap menjumlahkan alokasi versi LAMA: sisa tagihan terbaca utuh
// padahal baru saja dibayar orang lain, dan uangnya keluar dua kali tanpa satu
// pun error. Pembacaan terkunci selalu melihat versi terbaru yang sudah commit,
// sehingga transaksi kedua menghitung di atas hasil yang pertama.
//
// Barisnya dijumlahkan di Go, bukan lewat SUM(): agregat mengaburkan baris mana
// yang sebenarnya terkunci. Rentang indeks (tenant_id, invoice_id) ikut terkunci
// sehingga alokasi BARU untuk tagihan yang sama juga tertahan — bukan hanya yang
// sudah ada.
func (r *Repository) SumAllocationsLocked(ctx context.Context, tenantID, invoiceID uint64) (domain.Money, error) {
	var rows []*Allocation
	err := r.db.WithContext(ctx).
		Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("tenant_id = ? AND invoice_id = ? AND reversed_at IS NULL", tenantID, invoiceID).
		Order("id ASC").
		Find(&rows).Error
	if err != nil {
		return domain.Zero, err
	}
	total := domain.Zero
	for _, a := range rows {
		total = total.Add(a.Amount)
	}
	return total, nil
}

// CountActiveAllocations menghitung baris alokasi yang belum dibalik.
func (r *Repository) CountActiveAllocations(ctx context.Context, tenantID, invoiceID uint64) (int64, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&Allocation{}).
		Where("tenant_id = ? AND invoice_id = ? AND reversed_at IS NULL", tenantID, invoiceID).
		Count(&n).Error
	return n, err
}

func (r *Repository) CreateAllocation(ctx context.Context, a *Allocation) error {
	return r.db.WithContext(ctx).Create(a).Error
}

// ListAllocationsByPayment mengembalikan seluruh alokasi milik satu pembayaran —
// termasuk yang sudah dibalik, karena riwayatnya bagian dari bukti.
func (r *Repository) ListAllocationsByPayment(ctx context.Context, tenantID, paymentID uint64) ([]*Allocation, error) {
	var out []*Allocation
	err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND payment_id = ?", tenantID, paymentID).
		Order("id ASC").
		Find(&out).Error
	return out, err
}

// ListAllocationsByInvoice mengembalikan seluruh alokasi yang menyentuh satu
// tagihan — dasar riwayat pembayaran di layar detail.
func (r *Repository) ListAllocationsByInvoice(ctx context.Context, tenantID, invoiceID uint64) ([]*Allocation, error) {
	var out []*Allocation
	err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND invoice_id = ?", tenantID, invoiceID).
		Order("id ASC").
		Find(&out).Error
	return out, err
}

// ReverseAllocationsOfPayment menstempel `reversed_at` pada alokasi aktif milik
// satu pembayaran.
//
// UPDATE, bukan DELETE (Invariant #5). Barisnya tetap ada dan tetap terbaca di
// riwayat; yang berubah hanyalah keikutsertaannya dalam penjumlahan sisa
// tagihan. Menghapusnya akan membuat pembayaran yang dibalik lenyap dari jejak
// audit seolah tidak pernah terjadi.
//
// `reversed_at IS NULL` di klausa WHERE bukan hiasan: ia membuat operasi ini
// idempoten terhadap dirinya sendiri, sehingga pembalikan yang terulang tidak
// menggeser stempel waktu yang sudah tercatat.
func (r *Repository) ReverseAllocationsOfPayment(ctx context.Context, tenantID, paymentID uint64, at time.Time) (int64, error) {
	res := r.db.WithContext(ctx).Model(&Allocation{}).
		Where("tenant_id = ? AND payment_id = ? AND reversed_at IS NULL", tenantID, paymentID).
		Update("reversed_at", at)
	return res.RowsAffected, res.Error
}

// ── Pembayaran ────────────────────────────────────────────────────────────────

func (r *Repository) CreatePayment(ctx context.Context, p *Payment) error {
	return r.db.WithContext(ctx).Create(p).Error
}

// FindPayment mengambil satu pembayaran. `lock` menambahkan SELECT … FOR UPDATE.
//
// Kunci dipakai pada pembalikan (R-12). Tanpa itu dua permintaan pembalikan yang
// berbarengan sama-sama membaca `reversed_at IS NULL`, keduanya lolos, dan kas
// yang keluar sekali kembali dua kali — lewat dua jurnal yang sama-sama
// seimbang, jadi tidak ada laporan yang akan mengeluh.
func (r *Repository) FindPayment(ctx context.Context, tenantID, id uint64, lock bool) (*Payment, error) {
	q := r.db.WithContext(ctx)
	if lock {
		q = q.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	var p Payment
	err := q.Where("tenant_id = ? AND id = ?", tenantID, id).First(&p).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrPaymentNotFound
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// FindPaymentByIdemKey mencari pembayaran yang sudah terbentuk dari kunci yang
// sama. nil, nil bila belum ada — bukan error: "belum pernah" adalah jawaban
// yang sah, dan memaksa pemanggil membedakannya dari kegagalan hanya menambah
// cabang yang mudah salah.
func (r *Repository) FindPaymentByIdemKey(ctx context.Context, tenantID uint64, key string) (*Payment, error) {
	if strings.TrimSpace(key) == "" {
		return nil, nil
	}
	var p Payment
	err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND idempotency_key = ?", tenantID, key).
		First(&p).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// UpdatePayment menyimpan perubahan kolom tertentu — map kolom, bukan
// Save(struct), dengan alasan yang sama seperti UpdateInvoice: pembalikan tidak
// pernah boleh menjadi jalan masuk yang mengubah nominal.
func (r *Repository) UpdatePayment(ctx context.Context, tenantID, id uint64, fields map[string]any) error {
	res := r.db.WithContext(ctx).Model(&Payment{}).
		Where("tenant_id = ? AND id = ?", tenantID, id).
		Updates(fields)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrPaymentNotFound
	}
	return nil
}

// PaymentFilter menyaring daftar pembayaran.
type PaymentFilter struct {
	VendorID uint64
	Kind     PaymentKind
	// IncludeReversed false = hanya pembayaran yang masih berdiri.
	IncludeReversed bool
}

func (r *Repository) ListPayments(ctx context.Context, tenantID uint64, f PaymentFilter) ([]*Payment, error) {
	q := r.db.WithContext(ctx).Model(&Payment{}).Where("tenant_id = ?", tenantID)
	if f.VendorID != 0 {
		q = q.Where("vendor_id = ?", f.VendorID)
	}
	if f.Kind != "" {
		q = q.Where("payment_kind = ?", f.Kind)
	}
	if !f.IncludeReversed {
		q = q.Where("reversed_at IS NULL")
	}
	var out []*Payment
	if err := q.Order("payment_date DESC, id DESC").Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

// ── Penguncian (R-12) ────────────────────────────────────────────────────────

// LockInvoices mengunci beberapa baris tagihan sekaligus, DALAM URUTAN ID MENAIK.
//
// Urutannya yang penting, bukan sekadar keberadaan kuncinya. Dua transaksi yang
// mengunci tagihan #7 dan #9 dalam urutan berbeda akan saling menunggu selamanya;
// dengan urutan yang seragam, yang kedua sekadar menunggu yang pertama selesai
// lalu membaca hasilnya. Seluruh jalur pembayaran di paket ini memakai method
// ini — tidak ada yang mengunci tagihan dengan cara lain.
//
// Daftar kosong bukan error: pembayaran yang tidak menyentuh tagihan mana pun
// akan ditolak oleh validasi alokasi, dengan pesan yang menjelaskan sebabnya.
func (r *Repository) LockInvoices(ctx context.Context, tenantID uint64, ids []uint64) ([]*Invoice, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	sorted := make([]uint64, len(ids))
	copy(sorted, ids)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })

	var out []*Invoice
	err := r.db.WithContext(ctx).
		Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("tenant_id = ? AND id IN ?", tenantID, sorted).
		Order("id ASC").
		Find(&out).Error
	if err != nil {
		return nil, err
	}
	return out, nil
}

// ListPayableInvoices mengembalikan tagihan vendor yang BOLEH dibayar, tertua
// lebih dulu.
//
// Hanya `posted`: tagihan draft belum menjadi kewajiban siapa pun (INV-AP-9) dan
// tagihan yang sudah dibalik tidak lagi menjadi kewajiban. Membayar keduanya
// berarti mengeluarkan kas untuk kewajiban yang — menurut buku besar — tidak ada.
//
// Ini pembacaan TANPA kunci, dan memang cukup: hasilnya hanya dipakai untuk
// menentukan baris mana yang perlu dikunci. Seluruh nilai yang masuk perhitungan
// dibaca ulang setelah kuncinya didapat.
func (r *Repository) ListPayableInvoices(ctx context.Context, tenantID, vendorID uint64) ([]*Invoice, error) {
	var out []*Invoice
	err := r.db.WithContext(ctx).Model(&Invoice{}).
		Where("tenant_id = ? AND vendor_id = ? AND status = ?", tenantID, vendorID, InvoicePosted).
		Order("due_date ASC, invoice_date ASC, id ASC").
		Find(&out).Error
	if err != nil {
		return nil, err
	}
	return out, nil
}

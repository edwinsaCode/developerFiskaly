package histfin

import (
	"context"
	"errors"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"esaproperti/internal/ledger"
)

// Repository — akses DB untuk snapshot historis.
//
// Setiap query menyertakan predikat `tenant_id = ?` secara eksplisit; itulah
// cara isolasi tenant ditegakkan di repo ini (bukan scope implisit), sama
// seperti paket lain.
type Repository struct {
	db *gorm.DB
}

func NewRepository(db *gorm.DB) *Repository { return &Repository{db: db} }

// WithTx mengembalikan repository yang terikat pada transaksi berjalan.
func (r *Repository) WithTx(tx *gorm.DB) *Repository { return &Repository{db: tx} }

func (r *Repository) DB() *gorm.DB { return r.db }

// ── Snapshot ─────────────────────────────────────────────────────────────────

func (r *Repository) ListSnapshots(ctx context.Context, tenantID uint64) ([]Snapshot, error) {
	var out []Snapshot
	err := r.db.WithContext(ctx).
		Where("tenant_id = ?", tenantID).
		Order("fiscal_year DESC").
		Find(&out).Error
	return out, err
}

// FindByYear mengambil satu snapshot. `forUpdate` mengunci barisnya — dipakai
// oleh operasi tulis supaya dua admin tidak menyimpan tahun yang sama serentak.
func (r *Repository) FindByYear(ctx context.Context, tenantID uint64, year int, forUpdate bool) (*Snapshot, error) {
	q := r.db.WithContext(ctx).Where("tenant_id = ? AND fiscal_year = ?", tenantID, year)
	if forUpdate {
		q = q.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	var s Snapshot
	if err := q.First(&s).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrSnapshotNotFound
		}
		return nil, err
	}
	return &s, nil
}

func (r *Repository) CreateSnapshot(ctx context.Context, s *Snapshot) error {
	return r.db.WithContext(ctx).Create(s).Error
}

func (r *Repository) SaveSnapshot(ctx context.Context, s *Snapshot) error {
	return r.db.WithContext(ctx).Save(s).Error
}

// DeleteSnapshot menghapus snapshot beserta baris dan auditnya. Hanya dipanggil
// service untuk snapshot yang belum pernah final — histori yang pernah disahkan
// tidak boleh hilang.
func (r *Repository) DeleteSnapshot(ctx context.Context, tenantID, snapshotID uint64) error {
	db := r.db.WithContext(ctx)
	if err := db.Where("tenant_id = ? AND snapshot_id = ?", tenantID, snapshotID).Delete(&SnapshotLine{}).Error; err != nil {
		return err
	}
	if err := db.Where("tenant_id = ? AND snapshot_id = ?", tenantID, snapshotID).Delete(&Audit{}).Error; err != nil {
		return err
	}
	return db.Where("tenant_id = ? AND id = ?", tenantID, snapshotID).Delete(&Snapshot{}).Error
}

// ── Baris ────────────────────────────────────────────────────────────────────

func (r *Repository) ListLines(ctx context.Context, tenantID, snapshotID uint64) ([]SnapshotLine, error) {
	var out []SnapshotLine
	err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND snapshot_id = ?", tenantID, snapshotID).
		Order("sort_order ASC, account_code ASC").
		Find(&out).Error
	return out, err
}

// ListLinesForSnapshots mengambil baris beberapa snapshot sekaligus — dipakai
// daftar tahun supaya tidak N+1.
func (r *Repository) ListLinesForSnapshots(ctx context.Context, tenantID uint64, snapshotIDs []uint64) (map[uint64][]SnapshotLine, error) {
	out := map[uint64][]SnapshotLine{}
	if len(snapshotIDs) == 0 {
		return out, nil
	}
	var rows []SnapshotLine
	err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND snapshot_id IN ?", tenantID, snapshotIDs).
		Order("sort_order ASC, account_code ASC").
		Find(&rows).Error
	if err != nil {
		return nil, err
	}
	for _, l := range rows {
		out[l.SnapshotID] = append(out[l.SnapshotID], l)
	}
	return out, nil
}

func (r *Repository) DeleteLines(ctx context.Context, tenantID, snapshotID uint64) error {
	return r.db.WithContext(ctx).
		Where("tenant_id = ? AND snapshot_id = ?", tenantID, snapshotID).
		Delete(&SnapshotLine{}).Error
}

func (r *Repository) InsertLines(ctx context.Context, lines []SnapshotLine) error {
	if len(lines) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).Create(&lines).Error
}

// ── Audit ────────────────────────────────────────────────────────────────────

func (r *Repository) AppendAudit(ctx context.Context, a *Audit) error {
	return r.db.WithContext(ctx).Create(a).Error
}

func (r *Repository) ListAudits(ctx context.Context, tenantID, snapshotID uint64) ([]Audit, error) {
	var out []Audit
	err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND snapshot_id = ?", tenantID, snapshotID).
		Order("id DESC").
		Find(&out).Error
	return out, err
}

// ── Bacaan lintas-paket (read-only) ──────────────────────────────────────────

// FindAccount mengambil akun COA milik tenant. Snapshot memakai bagan akun yang
// sudah ada — tidak ada daftar akun khusus historis yang harus dirawat terpisah.
func (r *Repository) FindAccount(ctx context.Context, tenantID, accountID uint64) (*ledger.Account, error) {
	var a ledger.Account
	err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND id = ?", tenantID, accountID).
		First(&a).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrAccountNotFound
		}
		return nil, err
	}
	return &a, nil
}

// HasPostedJournals melaporkan apakah tahun buku ini sudah punya AKTIVITAS di
// buku besar.
//
// Jurnal saldo awal dikecualikan: ia menyatakan POSISI pembuka pada tanggal
// go-live, bukan aktivitas tahun itu. Tanpa pengecualian ini, saldo awal yang
// dibukukan 31 Desember akan memblokir snapshot tahun tersebut tanpa alasan.
func (r *Repository) HasPostedJournals(ctx context.Context, tenantID uint64, year int) (bool, error) {
	var n int64
	err := r.db.WithContext(ctx).
		Model(&ledger.JournalEntry{}).
		Where("tenant_id = ? AND posted_at IS NOT NULL AND YEAR(date) = ? AND source <> ?",
			tenantID, year, sourceOpeningBalance).
		Limit(1).
		Count(&n).Error
	return n > 0, err
}

// sourceOpeningBalance disalin dari document.SourceOpeningBalance. Disalin, bukan
// diimport, supaya histfin tidak menarik ketergantungan ke paket dokumen hanya
// demi satu literal — nilainya juga tercatat di kolom journal_entries.source.
const sourceOpeningBalance = "opening_balance"

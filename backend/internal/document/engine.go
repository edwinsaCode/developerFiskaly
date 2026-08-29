package document

// Mesin penomoran. Ini satu-satunya tempat di seluruh sistem yang boleh
// menaikkan seri dan membentuk nomor dokumen.

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"

	"esaproperti/internal/domain"
)

// Allocation adalah hasil pengambilan satu nomor: nomor jadi beserta kunci
// serinya. Dipisah dari pencatatan registry karena semua pemanggil membentuk
// nomor SEBELUM baris sumbernya ada (id-nya belum lahir).
type Allocation struct {
	TypeCode   string
	Number     string
	FiscalYear uint16
	SequenceNo uint64
	IssuedAt   time.Time
}

// Source menunjuk baris yang menerbitkan dokumen.
type Source struct {
	Table string
	ID    uint64
}

// Allocate mengambil SATU nomor berikutnya untuk sebuah jenis dokumen.
//
// WAJIB dipanggil di dalam transaksi pemanggil. Dua alasan, keduanya penting:
//
//  1. Nomor yang sudah diambil harus ikut batal bila transaksi rollback —
//     kalau tidak, seri berlubang tanpa sebab yang bisa dijelaskan ke auditor.
//  2. Baris seri terkunci sampai commit, sehingga dua request bersamaan tidak
//     mungkin membaca nilai yang sama. Inilah yang menjamin nomor tidak kembar,
//     bukan UNIQUE di `documents` (itu jaring terakhir, bukan mekanisme utama).
func Allocate(tx *gorm.DB, tenantID uint64, typeCode string, issuedAt time.Time) (*Allocation, error) {
	dt, err := resolveType(tx, tenantID, typeCode)
	if err != nil {
		return nil, err
	}
	if issuedAt.IsZero() {
		issuedAt = time.Now()
	}
	fy := fiscalYearFor(dt.ResetPolicy, issuedAt)

	// Lantai awal untuk kunci seri yang belum ada TIDAK dipatok 1 melainkan
	// diturunkan dari registry: bila tahun ini sudah pernah punya dokumen (mis.
	// baris seri terhapus, atau engine baru mengambil alih tahun berjalan yang
	// belum ter-seed), seri melanjutkan dan bukan mengulang. Melompati nomor
	// jauh lebih murah daripada menerbitkan nomor kembar.
	//
	// Dibaca TERPISAH — sengaja tidak digabung sebagai subquery di dalam
	// INSERT..SELECT. INSERT..SELECT membaca tabel sumber dengan shared
	// next-key lock (bukan consistent read), sehingga dua transaksi bersamaan
	// saling mengunci: A pegang gap-S di `documents`, B menunggu baris seri
	// milik A, lalu A menunggu gap milik B saat menyisipkan dokumennya sendiri
	// → deadlock. SELECT biasa di bawah ini tidak mengunci apa pun.
	var floor uint64
	if err := tx.Raw(`
		SELECT COALESCE(MAX(sequence_no), 0) FROM documents
		 WHERE tenant_id = ? AND document_type_code = ? AND fiscal_year = ?`,
		tenantID, dt.Code, fy,
	).Scan(&floor).Error; err != nil {
		return nil, fmt.Errorf("baca lantai seri dokumen %s: %w", dt.Code, err)
	}

	// INSERT..ON DUPLICATE KEY UPDATE menaikkan seri secara atomik sekaligus
	// mengunci barisnya sampai commit. Hanya satu baris yang disentuh, jadi
	// urutan kunci antar-transaksi tidak mungkin terbalik.
	if err := tx.Exec(`
		INSERT INTO document_sequences
			(tenant_id, document_type_code, fiscal_year, last_val, created_at, updated_at)
		VALUES (?, ?, ?, ?, NOW(3), NOW(3))
		ON DUPLICATE KEY UPDATE last_val = last_val + 1, updated_at = NOW(3)`,
		tenantID, dt.Code, fy, floor+1,
	).Error; err != nil {
		return nil, fmt.Errorf("naikkan seri dokumen %s: %w", dt.Code, err)
	}

	var seq uint64
	if err := tx.Raw(`
		SELECT last_val FROM document_sequences
		 WHERE tenant_id = ? AND document_type_code = ? AND fiscal_year = ?`,
		tenantID, dt.Code, fy,
	).Scan(&seq).Error; err != nil {
		return nil, fmt.Errorf("baca seri dokumen %s: %w", dt.Code, err)
	}
	if seq == 0 {
		return nil, fmt.Errorf("baca seri dokumen %s: seri tidak terbaca", dt.Code)
	}

	number, err := formatNumber(dt, seq, issuedAt)
	if err != nil {
		return nil, err
	}
	return &Allocation{
		TypeCode:   dt.Code,
		Number:     number,
		FiscalYear: fy,
		SequenceNo: seq,
		IssuedAt:   issuedAt,
	}, nil
}

// Register mencatat nomor yang sudah dialokasikan ke registry, setelah baris
// sumbernya lahir. Dipanggil di transaksi yang sama dengan Allocate.
func Register(tx *gorm.DB, tenantID uint64, alloc *Allocation, src Source, amount domain.Money, createdBy *uint64) (*Document, error) {
	if alloc == nil {
		return nil, ErrSourceRequired
	}
	if strings.TrimSpace(src.Table) == "" || src.ID == 0 {
		return nil, fmt.Errorf("%w: %q/%d", ErrSourceRequired, src.Table, src.ID)
	}
	doc := &Document{
		TenantID:         tenantID,
		DocumentTypeCode: alloc.TypeCode,
		Number:           alloc.Number,
		FiscalYear:       alloc.FiscalYear,
		SequenceNo:       alloc.SequenceNo,
		IssuedAt:         alloc.IssuedAt,
		SourceTable:      strings.TrimSpace(src.Table),
		SourceID:         src.ID,
		Amount:           amount,
		CreatedBy:        createdBy,
	}
	if err := tx.Create(doc).Error; err != nil {
		if isDuplicateKey(err) {
			// Dua unique index, dua penyebab yang sama sekali berbeda:
			// uk_doc_number = tabrakan nomor (seri rusak), uk_doc_source =
			// sumber yang sama minta dokumen KEDUA (larangan INV-DOC-1).
			// Melaporkan keduanya sebagai "nomor sudah terpakai" mengirim yang
			// menyelidiki ke seri penomoran, padahal serinya sehat.
			if sourceAlreadyDocumented(tx, tenantID, doc.SourceTable, doc.SourceID) {
				return nil, fmt.Errorf("%w: %s#%d", ErrSourceHasDocument, doc.SourceTable, doc.SourceID)
			}
			return nil, fmt.Errorf("%w: %s", ErrNumberTaken, alloc.Number)
		}
		return nil, fmt.Errorf("catat dokumen %s: %w", alloc.Number, err)
	}
	return doc, nil
}

// Issue adalah Allocate + Register untuk pemanggil yang baris sumbernya sudah
// ada lebih dulu.
func Issue(tx *gorm.DB, tenantID uint64, typeCode string, issuedAt time.Time, src Source, amount domain.Money, createdBy *uint64) (*Document, error) {
	alloc, err := Allocate(tx, tenantID, typeCode, issuedAt)
	if err != nil {
		return nil, err
	}
	return Register(tx, tenantID, alloc, src, amount, createdBy)
}

// ── Resolver master ─────────────────────────────────────────────────────────

// resolveType memuat dan memvalidasi konfigurasi jenis dokumen.
//
// FAIL-CLOSED, pola yang sama dengan charge.resolveChargeTypePolicyDB: kode tak
// terdaftar, jenis nonaktif, konfigurasi tidak sah, atau error DB SEMUANYA
// mengembalikan error. Tidak ada fallback ke prefix default — nomor dokumen
// yang salah prefix lebih sulit dibereskan daripada transaksi yang gagal.
func resolveType(tx *gorm.DB, tenantID uint64, code string) (*DocumentType, error) {
	c := NormalizeCode(code)
	if c == "" {
		return nil, fmt.Errorf("%w: kode kosong", ErrTypeUnknown)
	}
	var dt DocumentType
	err := tx.Where("tenant_id = ? AND code = ?", tenantID, c).First(&dt).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, fmt.Errorf("%w: %q", ErrTypeUnknown, c)
	}
	if err != nil {
		return nil, fmt.Errorf("resolve jenis dokumen %q: %w", c, err)
	}
	if !dt.IsActive {
		return nil, fmt.Errorf("%w: %q", ErrTypeInactive, c)
	}
	if strings.TrimSpace(dt.Prefix) == "" {
		return nil, fmt.Errorf("%w: %q belum punya prefix", ErrTypeInvalid, c)
	}
	if !dt.ResetPolicy.Valid() {
		return nil, fmt.Errorf("%w: %q kebijakan reset %q", ErrTypeInvalid, c, dt.ResetPolicy)
	}
	return &dt, nil
}

// NormalizeCode menyamakan pencocokan kode dengan collation MySQL
// (utf8mb4_unicode_ci — case-insensitive), agar lookup di Go dan pencarian di
// DB tidak pernah berbeda kesimpulan.
func NormalizeCode(code string) string {
	return strings.ToLower(strings.TrimSpace(code))
}

// sourceAlreadyDocumented membedakan pelanggaran uk_doc_source dari
// uk_doc_number dengan MENANYAKAN faktanya, bukan mengurai teks pesan MySQL:
// nama index di pesan error berbeda antar versi server dan driver, isi tabelnya
// tidak. Aman dipanggil setelah INSERT gagal — MySQL tidak membatalkan transaksi
// karena duplicate key, hanya statement-nya.
func sourceAlreadyDocumented(tx *gorm.DB, tenantID uint64, table string, id uint64) bool {
	var n int64
	if err := tx.Model(&Document{}).
		Where("tenant_id = ? AND source_table = ? AND source_id = ?", tenantID, table, id).
		Count(&n).Error; err != nil {
		return false
	}
	return n > 0
}

func isDuplicateKey(err error) bool {
	return err != nil && (errors.Is(err, gorm.ErrDuplicatedKey) || strings.Contains(err.Error(), "1062"))
}

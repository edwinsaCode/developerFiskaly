package document

// W-2 — Document Domain. SATU mesin penomoran untuk SELURUH dokumen.
//
// Keputusan owner FINAL 2026-08-07:
//   - Tidak ada lagi `receipt_sequences` vs `invoice_sequences`. Satu SSOT:
//     `document_sequences`, kunci (tenant_id, document_type_code, fiscal_year).
//   - Reset tahunan berlaku FORWARD-ONLY sejak engine ini dipakai. Dokumen
//     historis tidak berubah sedikit pun. Tahun baru mulai dari 000001.
//   - Semua jenis dokumen (Kwitansi, Invoice, Memo Transfer Internal, Cash
//     Voucher, dan jenis berikutnya) memakai engine yang SAMA. Yang berbeda
//     hanya KONFIGURASI di master `document_types` — bukan enginenya.
//
// Konsekuensi desain yang disengaja: menambah jenis dokumen baru tidak boleh
// menyentuh package ini sama sekali. Kalau suatu saat ada yang tergoda menulis
// `switch docType` di sini, itu tanda konfigurasinya yang kurang, bukan
// enginenya yang perlu cabang.

import (
	"fmt"
	"strings"
	"time"

	"esaproperti/internal/domain"
)

// ── Kebijakan reset ─────────────────────────────────────────────────────────

// ResetPolicy menentukan kapan seri kembali ke 1.
type ResetPolicy string

const (
	// ResetYearly — seri kembali ke 1 setiap ganti tahun penerbitan.
	ResetYearly ResetPolicy = "yearly"
	// ResetNever — seri berjalan terus seumur tenant (fiscal_year disimpan 0).
	ResetNever ResetPolicy = "never"
)

func (p ResetPolicy) Valid() bool {
	switch p {
	case ResetYearly, ResetNever:
		return true
	}
	return false
}

// fiscalYearFor memetakan waktu penerbitan → kunci tahun pada seri.
//
// Yang dipakai adalah waktu PENERBITAN dokumen, bukan tanggal transaksinya.
// Ini disengaja: nomor dokumen adalah identitas fisik lembar bukti, dan sebuah
// pembayaran bertanggal 30 Desember yang kwitansinya baru dicetak 2 Januari
// tetap merupakan dokumen tahun berjalan. Memakai tanggal transaksi akan
// membuat dokumen baru menyusup ke seri tahun yang sudah ditutup.
func fiscalYearFor(p ResetPolicy, issuedAt time.Time) uint16 {
	if p == ResetNever {
		return 0
	}
	return uint16(issuedAt.Year())
}

// ── Master jenis dokumen ────────────────────────────────────────────────────

// DocumentType adalah KONFIGURASI penomoran satu jenis dokumen. Perilaku
// penomoran adalah data di tabel ini, bukan cabang if di kode.
type DocumentType struct {
	ID           uint64      `gorm:"primaryKey;autoIncrement"                        json:"id"`
	TenantID     uint64      `gorm:"not null;index"                                  json:"-"`
	Code         string      `gorm:"not null;size:32"                                json:"code"`
	Name         string      `gorm:"not null;size:100"                               json:"name"`
	Prefix       string      `gorm:"not null;size:10"                                json:"prefix"`
	NumberFormat string      `gorm:"not null;size:60;default:'{prefix}/{year}/{seq}'" json:"number_format"`
	ResetPolicy  ResetPolicy `gorm:"not null;size:16;default:'yearly'"               json:"reset_policy"`
	Padding      uint8       `gorm:"not null;default:6"                              json:"padding"`
	IsActive     bool        `gorm:"not null;default:true"                           json:"is_active"`
	// IsSystem menandai jenis dokumen yang dipakai alur inti. Prefix, format,
	// dan padding-nya tetap bebas diubah admin — yang dilarang hanya
	// menonaktifkannya, karena resolver fail-closed akan menggagalkan
	// penerimaan pembayaran. Flag ini DATA di master, bukan daftar kode di
	// dalam kode program.
	IsSystem  bool      `gorm:"not null;default:false"                          json:"is_system"`
	CreatedAt time.Time `                                                       json:"created_at"`
	UpdatedAt time.Time `                                                       json:"updated_at"`
}

func (DocumentType) TableName() string { return "document_types" }

// ── Seri ────────────────────────────────────────────────────────────────────

// Sequence adalah SSOT penomoran. `LastVal` = nomor TERAKHIR yang sudah
// dipakai — bukan nomor berikutnya. Lihat komentar kolom di migration 000065:
// tabel lama menyimpan makna yang sama di bawah nama `next_val`, dan salah baca
// satu kali saja berarti dua dokumen bernomor sama.
type Sequence struct {
	TenantID         uint64 `gorm:"primaryKey"`
	DocumentTypeCode string `gorm:"primaryKey;size:32"`
	FiscalYear       uint16 `gorm:"primaryKey"`
	LastVal          uint64 `gorm:"not null;default:0"`
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

func (Sequence) TableName() string { return "document_sequences" }

// ── Registry ────────────────────────────────────────────────────────────────

// Document adalah catatan APPEND-ONLY setiap nomor yang pernah terbit, apa pun
// jenisnya. Nomor tidak pernah diubah dan tidak pernah dipakai ulang.
//
// `SourceTable`/`SourceID` adalah logical FK ke baris yang menerbitkan dokumen
// (receipts, invoices, charge_settlements, …) — sengaja tanpa constraint agar
// package ini tidak perlu tahu modul mana saja yang ada.
type Document struct {
	ID               uint64    `gorm:"primaryKey;autoIncrement"                     json:"id"`
	TenantID         uint64    `gorm:"not null;index"                               json:"-"`
	DocumentTypeCode string    `gorm:"not null;size:32"                             json:"document_type_code"`
	Number           string    `gorm:"not null;size:40"                             json:"number"`
	FiscalYear       uint16    `gorm:"not null"                                     json:"fiscal_year"`
	SequenceNo       uint64    `gorm:"not null"                                     json:"sequence_no"`
	IssuedAt         time.Time `gorm:"not null"                                     json:"issued_at"`
	SourceTable      string    `gorm:"not null;size:40"                             json:"source_table"`
	SourceID         uint64    `gorm:"not null"                                     json:"source_id"`
	// ReversesDocumentID diisi hanya oleh dokumen pembalik (jenis JR): dokumen
	// asli yang dibalik. Koreksi ledger selalu lewat jurnal pembalik dengan
	// dokumennya sendiri (D-W3-5) — tanpa tautan ini, pembalikan terbaca
	// sebagai dua pergerakan kas yang tidak berhubungan.
	ReversesDocumentID *uint64      `                                                    json:"reverses_document_id,omitempty"`
	Amount             domain.Money `gorm:"type:DECIMAL(20,4);not null;default:'0.0000'" json:"amount"`
	CreatedBy          *uint64      `                                                    json:"created_by,omitempty"`
	CreatedAt          time.Time    `                                                    json:"created_at"`
	UpdatedAt          time.Time    `                                                    json:"-"`
}

func (Document) TableName() string { return "documents" }

// ── Format nomor ────────────────────────────────────────────────────────────

// Placeholder yang dikenali `number_format`.
const (
	phPrefix = "{prefix}"
	phYear   = "{year}"
	phMonth  = "{month}"
	phSeq    = "{seq}"
)

// formatNumber merender satu nomor dokumen dari konfigurasi master.
//
// Hanya {seq} yang wajib: tanpa itu setiap dokumen akan bernomor sama, dan
// keseluruhan gunanya mesin ini hilang. Kesalahan konfigurasi ditolak di sini
// supaya gagal SEBELUM ada nomor terbit, bukan sesudahnya.
func formatNumber(t *DocumentType, seq uint64, issuedAt time.Time) (string, error) {
	f := strings.TrimSpace(t.NumberFormat)
	if f == "" {
		f = "{prefix}/{year}/{seq}"
	}
	if !strings.Contains(f, phSeq) {
		return "", fmt.Errorf("%w: format %q tidak memuat %s", ErrFormatInvalid, f, phSeq)
	}
	pad := int(t.Padding)
	if pad < 1 {
		pad = 1
	}
	if pad > 12 {
		pad = 12
	}
	r := strings.NewReplacer(
		phPrefix, strings.TrimSpace(t.Prefix),
		phYear, fmt.Sprintf("%04d", issuedAt.Year()),
		phMonth, fmt.Sprintf("%02d", int(issuedAt.Month())),
		phSeq, fmt.Sprintf("%0*d", pad, seq),
	)
	out := r.Replace(f)
	if strings.Contains(out, "{") {
		return "", fmt.Errorf("%w: format %q memuat placeholder tak dikenal", ErrFormatInvalid, f)
	}
	return out, nil
}

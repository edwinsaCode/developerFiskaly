// Package legacyar (W-7) memegang PIUTANG PROYEK LAMA: saldo yang masih
// ditagih ke pembeli dari proyek yang berjalan sebelum sistem ini dipakai.
//
// Satu kalimat yang menjelaskan seluruh desain paket ini:
//
//	Impor piutang lama adalah BUKU PEMBANTU, bukan kejadian ekonomi.
//
// Saldo piutangnya SUDAH ada di buku besar — masuk lewat jurnal Saldo Awal
// sebagai satu angka gelondongan di akun kontrol (mis. 1-2000 Rp 1 miliar).
// Yang hilang bukan saldonya, melainkan RINCIANNYA: siapa saja yang menyusun
// angka itu. Paket ini mengisi rincian tersebut.
//
// Konsekuensi yang ditegakkan di seluruh file (INV-LAR-4): impor TIDAK PERNAH
// menulis journal_entries. Menjurnal ulang rincian dari saldo yang sudah
// dibukukan berarti mencatat aset yang sama dua kali. Satu-satunya jurnal yang
// lahir dari domain ini adalah PELUNASAN — Dr Kas / Cr akun kontrol — karena
// itu memang kejadian ekonomi baru.
//
// Yang sengaja TIDAK dibuat di sini:
//   - Mesin aging kedua. Paket ini menghasilkan receivable.Row dan menyerahkan
//     seluruh bucketing ke `receivable` (INV-AR-1).
//   - Akun buku besar khusus legacy. Pemisahan legacy vs sistem terjadi di
//     tingkat sub-ledger dan `source`, bukan dengan memecah akun kontrol.
//   - Jenis dokumen baru. Pelunasan legacy memakai BKM lewat Document Domain
//     yang sudah ada, sehingga INV-DOC-1 berlaku tanpa pengecualian.
package legacyar

import (
	"time"

	"esaproperti/internal/domain"
)

// ── Batch impor ───────────────────────────────────────────────────────────────

// BatchStatus adalah daur hidup satu unggahan.
//
// `draft` bukan "impor separuh jadi": selama draft TIDAK ADA satu pun baris
// piutang yang terbentuk. Yang tersimpan hanya hasil baca berkas, supaya
// pratinjau dan rekonsiliasi bisa dilihat sebelum apa pun mengikat.
type BatchStatus string

const (
	BatchDraft     BatchStatus = "draft"
	BatchCommitted BatchStatus = "committed"
	BatchDiscarded BatchStatus = "discarded"
)

func (s BatchStatus) Valid() bool {
	return s == BatchDraft || s == BatchCommitted || s == BatchDiscarded
}

// Batch adalah satu berkas Excel yang diunggah.
type Batch struct {
	ID       uint64 `gorm:"primaryKey;autoIncrement" json:"id"`
	TenantID uint64 `gorm:"not null;index"           json:"-"`
	FileName string `gorm:"not null;size:255"        json:"file_name"`
	FileSize int64  `gorm:"not null;default:0"       json:"file_size"`
	// FileHash (SHA-256 isi berkas) selalu terisi — dipakai memperingatkan
	// pengunggahan ulang sejak tahap pratinjau, sebelum apa pun mengikat.
	FileHash string `gorm:"not null;size:64" json:"file_hash"`
	// CommittedHash hanya terisi saat commit berhasil, dan di situlah unique key
	// terpasang. Kalau kunci itu dipasang di FileHash, satu draft yang dibatalkan
	// akan membuat berkas yang sama mustahil diunggah lagi selamanya.
	CommittedHash *string `gorm:"size:64" json:"-"`
	// AsOfDate adalah tanggal posisi saldo — SATU untuk seluruh berkas. Kalau
	// dibuat per baris, satu berkas bisa memuat lima tanggal berbeda dan
	// rekonsiliasi terhadap saldo buku besar kehilangan artinya.
	AsOfDate           time.Time    `gorm:"type:date;not null" json:"as_of_date"`
	ControlAccountCode string       `gorm:"not null;size:20"   json:"control_account_code"`
	Status             BatchStatus  `gorm:"not null;size:12;default:'draft'" json:"status"`
	RowCount           int          `gorm:"not null;default:0" json:"row_count"`
	ValidCount         int          `gorm:"not null;default:0" json:"valid_count"`
	ErrorCount         int          `gorm:"not null;default:0" json:"error_count"`
	WarningCount       int          `gorm:"not null;default:0" json:"warning_count"`
	TotalAmount        domain.Money `gorm:"type:DECIMAL(20,4);not null;default:'0.0000'" json:"total_amount"`
	// OpeningJournalID menunjuk jurnal saldo awal yang dibuat BERSAMA commit
	// untuk menutup selisih terhadap buku besar. NULL pada kasus normal —
	// Saldo Awal biasanya sudah memuat piutangnya.
	OpeningJournalID *uint64 `json:"opening_journal_id,omitempty"`
	// SkipReason adalah alasan tertulis bila user tetap melanjutkan walau
	// selisih ≠ 0. Selisih tidak diblokir, tetapi juga tidak boleh lewat diam-diam.
	SkipReason  string     `gorm:"size:500" json:"skip_reason,omitempty"`
	Notes       string     `gorm:"size:500" json:"notes,omitempty"`
	CreatedBy   *uint64    `json:"created_by,omitempty"`
	CommittedBy *uint64    `json:"committed_by,omitempty"`
	CommittedAt *time.Time `json:"committed_at,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

func (Batch) TableName() string { return "legacy_ar_batches" }

// ParseStatus adalah hasil pembacaan satu baris berkas.
//
// `warning` sengaja dibedakan dari `error`: nama yang mirip dengan piutang lain
// harus DITAMPILKAN, bukan memblokir impor. Sistem tidak tahu apakah "Budi
// Santoso" kedua adalah orang yang sama; admin tahu.
type ParseStatus string

const (
	ParseOK      ParseStatus = "ok"
	ParseWarning ParseStatus = "warning"
	ParseError   ParseStatus = "error"
)

// RowIssue adalah satu keluhan atas sebuah baris berkas.
type RowIssue struct {
	Column  string `json:"column"`
	Message string `json:"message"`
}

// BatchRow adalah satu baris berkas beserta hasil bacanya.
//
// Kolom Raw* menyimpan isi sel APA ADANYA. Ketika klien protes "angkanya bukan
// segitu", yang harus bisa dibuka adalah apa yang mereka kirim — bukan hasil
// tafsir kita atasnya.
type BatchRow struct {
	ID       uint64 `gorm:"primaryKey;autoIncrement" json:"id"`
	TenantID uint64 `gorm:"not null;index"           json:"-"`
	BatchID  uint64 `gorm:"not null;index"           json:"batch_id"`
	// LineNo adalah nomor baris di berkas (1 = baris data pertama, bukan header).
	// Namanya bukan `row_number` karena itu reserved word di MySQL 8.
	LineNo          int    `gorm:"column:line_no;not null" json:"line_no"`
	RawCustomerName string `gorm:"size:255" json:"raw_customer_name"`
	RawSourceLabel  string `gorm:"size:255" json:"raw_source_label"`
	RawExternalRef  string `gorm:"size:100" json:"raw_external_ref"`
	RawOutstanding  string `gorm:"size:64"  json:"raw_outstanding"`
	RawDueDate      string `gorm:"size:40"  json:"raw_due_date"`
	RawPhone        string `gorm:"size:64"  json:"raw_phone"`
	RawEmail        string `gorm:"size:190" json:"raw_email"`
	RawNotes        string `gorm:"size:500" json:"raw_notes"`

	Amount      domain.Money `gorm:"type:DECIMAL(20,4);not null;default:'0.0000'" json:"amount"`
	DueDate     *time.Time   `gorm:"type:date" json:"due_date,omitempty"`
	ParseStatus ParseStatus  `gorm:"not null;size:10;default:'ok'" json:"parse_status"`
	// Issues disimpan sebagai JSON di kolom `errors`.
	Issues JSONIssues `gorm:"column:errors;type:json" json:"issues,omitempty"`
	// LegacyReceivableID terisi setelah commit — jejak dari baris berkas ke
	// piutang yang terbentuk darinya.
	LegacyReceivableID *uint64   `json:"legacy_receivable_id,omitempty"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}

func (BatchRow) TableName() string { return "legacy_ar_batch_rows" }

// ── Piutang lama ──────────────────────────────────────────────────────────────

// Status adalah keadaan satu piutang lama.
//
// StatusWrittenOff SENGAJA hanya ada sebagai status domain tanpa perlakuan
// akuntansi apa pun: penghapusan piutang adalah keputusan akuntan klien
// (penyisihan? beban langsung? akun mana?), dan menebaknya berarti mengarang
// jurnal. Sampai keputusan itu ada, tidak ada jalur kode yang menghasilkannya.
type Status string

const (
	StatusOpen       Status = "open"
	StatusPaid       Status = "paid"
	StatusWrittenOff Status = "written_off"
)

func (s Status) Valid() bool {
	return s == StatusOpen || s == StatusPaid || s == StatusWrittenOff
}

// Receivable adalah aggregate root: satu tagihan sisa dari proyek lama.
type Receivable struct {
	ID       uint64 `gorm:"primaryKey;autoIncrement" json:"id"`
	TenantID uint64 `gorm:"not null;index"           json:"-"`
	BatchID  uint64 `gorm:"not null;index"           json:"batch_id"`
	// CustomerName adalah identitas OTORITATIF baris ini, disimpan sebagai
	// snapshot. Customer legacy tidak dipaksa masuk master `customers`: proyek
	// lamanya memang tidak ada di sistem, dan memaksakan foreign key hanya demi
	// keterisian akan mencemari master dengan orang yang mungkin tidak pernah
	// bertransaksi lagi.
	CustomerName string `gorm:"not null;size:200" json:"customer_name"`
	// CustomerID adalah tautan OPSIONAL ke master, diisi belakangan bila
	// ternyata orang yang sama membeli unit baru. Logical FK.
	CustomerID *uint64 `json:"customer_id,omitempty"`
	// SourceLabel adalah nama proyek/sumber lama apa adanya ("Perum Melati
	// 2019"). Ia BUKAN foreign key ke `projects` — proyeknya memang tidak ada.
	SourceLabel string `gorm:"not null;size:200" json:"source_label"`
	// ExternalRef adalah nomor rujukan di sistem/pembukuan lama. Unik per tenant
	// bila diisi, dan itulah pertahanan idempotensi paling kuat saat berkas yang
	// sama diunggah dua kali dengan nama berkas berbeda.
	ExternalRef *string `gorm:"size:100" json:"external_ref,omitempty"`
	Phone       string  `gorm:"size:30"  json:"phone,omitempty"`
	Email       string  `gorm:"size:120" json:"email,omitempty"`
	// ControlAccountCode di-SNAPSHOT per baris (pola ChargeItem.DepositAccountCode
	// dan tax_rules rule_id+revision): pelunasan tahun depan harus tetap
	// mengkredit akun tempat piutang ini DIBENTUK, bukan konfigurasi terbaru.
	ControlAccountCode string `gorm:"not null;size:20" json:"control_account_code"`
	// AsOfDate adalah tanggal cutoff posisi saldo, diwarisi dari batch.
	AsOfDate time.Time `gorm:"type:date;not null" json:"as_of_date"`
	// DueDate boleh kosong. Mesin aging memakai AsOfDate sebagai gantinya:
	// piutang proyek lama tanpa tanggal jatuh tempo memang sudah lewat tempo
	// per tanggal cutoff, bukan "belum jatuh tempo".
	DueDate *time.Time `gorm:"type:date" json:"due_date,omitempty"`
	// OriginalAmount IMMUTABLE. Koreksi dilakukan dengan membatalkan baris dan
	// mengimpor ulang: begitu satu pelunasan menempel, mengubah nominalnya
	// mengubah arti pelunasan itu secara surut.
	OriginalAmount domain.Money `gorm:"type:DECIMAL(20,4);not null;default:'0.0000'" json:"original_amount"`
	// PaidAmount adalah CACHE. Sumber kebenarannya legacy_receivable_payments
	// (INV-LAR-3, pola payment_allocations vs payment_schedules.paid_amount):
	// kolom ini tidak pernah berubah tanpa baris pelunasan di transaksi yang sama.
	PaidAmount domain.Money `gorm:"type:DECIMAL(20,4);not null;default:'0.0000'" json:"paid_amount"`
	Status     Status       `gorm:"not null;size:12;default:'open'" json:"status"`
	Notes      string       `gorm:"size:500" json:"notes,omitempty"`
	CreatedBy  *uint64      `json:"created_by,omitempty"`
	CreatedAt  time.Time    `json:"created_at"`
	UpdatedAt  time.Time    `json:"updated_at"`
}

func (Receivable) TableName() string { return "legacy_receivables" }

// Outstanding adalah sisa yang masih ditagih.
func (r Receivable) Outstanding() domain.Money {
	return r.OriginalAmount.Sub(r.PaidAmount)
}

// EffectiveDueDate adalah tanggal yang dipakai mesin aging.
func (r Receivable) EffectiveDueDate() time.Time {
	if r.DueDate != nil && !r.DueDate.IsZero() {
		return *r.DueDate
	}
	return r.AsOfDate
}

// ── Pelunasan ─────────────────────────────────────────────────────────────────

// Payment adalah satu penerimaan kas atas SATU piutang lama.
//
// Pembayaran yang menutup beberapa piutang menghasilkan beberapa baris di sini,
// semuanya menunjuk jurnal kas yang sama. Alokasinya karena itu selalu eksplisit
// dan bisa ditelusuri balik — tidak pernah "kurangi total customer secara buta".
type Payment struct {
	ID                 uint64 `gorm:"primaryKey;autoIncrement" json:"id"`
	TenantID           uint64 `gorm:"not null;index"           json:"-"`
	LegacyReceivableID uint64 `gorm:"not null;index"           json:"legacy_receivable_id"`
	// Amount NEGATIF hanya untuk baris pembatalan (cermin dari baris asal).
	// Pembaca outstanding menjumlahkan keduanya sehingga histori tetap utuh —
	// Invariant #5 (append-only), pola charge_item_void.
	Amount             domain.Money `gorm:"type:DECIMAL(20,4);not null;default:'0.0000'" json:"amount"`
	PaymentDate        time.Time    `gorm:"type:date;not null" json:"payment_date"`
	CashAccountCode    string       `gorm:"not null;size:20"   json:"cash_account_code"`
	ControlAccountCode string       `gorm:"not null;size:20"   json:"control_account_code"`
	JournalEntryID     uint64       `gorm:"not null;index"     json:"journal_entry_id"`
	DocumentID         *uint64      `json:"document_id,omitempty"`
	// DocumentNumber di-snapshot supaya riwayat terbaca tanpa join.
	DocumentNumber string  `gorm:"size:40" json:"document_number,omitempty"`
	ReceiptID      *uint64 `json:"receipt_id,omitempty"`
	// ReceiptNumber di-snapshot dengan pola yang sama seperti DocumentNumber —
	// supaya halaman detail bisa menawarkan cetak kwitansi (KWL) langsung dari
	// baris riwayat pembayaran tanpa pindah ke Buku Dokumen.
	ReceiptNumber  string    `gorm:"size:40" json:"receipt_number,omitempty"`
	VoidsPaymentID *uint64   `json:"voids_payment_id,omitempty"`
	Notes          string    `gorm:"size:500" json:"notes,omitempty"`
	IdempotencyKey *string   `gorm:"size:64"  json:"-"`
	CreatedBy      *uint64   `json:"created_by,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

func (Payment) TableName() string { return "legacy_receivable_payments" }

// IsVoid melaporkan apakah baris ini membatalkan pelunasan lain.
func (p Payment) IsVoid() bool { return p.VoidsPaymentID != nil }

// ── Audit ─────────────────────────────────────────────────────────────────────

// AuditEvent adalah jenis kejadian pada satu piutang lama.
type AuditEvent string

const (
	EventImported     AuditEvent = "imported"
	EventPaid         AuditEvent = "paid"
	EventPaymentVoid  AuditEvent = "payment_void"
	EventCustomerLink AuditEvent = "customer_linked"
)

// Audit adalah jejak APPEND-ONLY per piutang.
//
// Mencatat KEADAAN, bukan klik: operasi yang ditolak tidak meninggalkan baris.
// Riwayat yang penuh percobaan gagal tidak bisa dibaca siapa pun (pelajaran W-6).
type Audit struct {
	ID                 uint64       `gorm:"primaryKey;autoIncrement" json:"id"`
	TenantID           uint64       `gorm:"not null;index"           json:"-"`
	LegacyReceivableID uint64       `gorm:"not null;index"           json:"legacy_receivable_id"`
	Event              AuditEvent   `gorm:"not null;size:24"         json:"event"`
	Amount             domain.Money `gorm:"type:DECIMAL(20,4);not null;default:'0.0000'" json:"amount"`
	OutstandingAfter   domain.Money `gorm:"type:DECIMAL(20,4);not null;default:'0.0000'" json:"outstanding_after"`
	Detail             string       `gorm:"size:500" json:"detail,omitempty"`
	ActorID            *uint64      `json:"actor_id,omitempty"`
	CreatedAt          time.Time    `json:"created_at"`
	UpdatedAt          time.Time    `json:"updated_at"`
}

func (Audit) TableName() string { return "legacy_receivable_audits" }

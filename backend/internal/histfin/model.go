// Package histfin (Historical Financials — W-6) menyimpan kondisi laporan
// keuangan tahun-tahun SEBELUM sistem ini dipakai.
//
// Batas yang menentukan seluruh desain paket ini:
//
//	Snapshot historis TIDAK PERNAH menjadi jurnal.
//
// Bukan karena malas, tapi karena Laba Rugi di sistem ini kumulatif sampai
// `as_of` (reporting.queryPLRows). Memposting pendapatan 2023–2025 akan
// menggelembungkan Laba Rugi berjalan kecuali tiap tahun ikut ditutup dengan
// entri penutup — yaitu merekonstruksi buku besar bertahun-tahun ke belakang.
// Ditambah: jurnal saldo awal go-live sudah menyatakan posisi neraca pembuka,
// jadi keduanya di ledger berarti dobel hitung.
//
// Karena itu paket ini:
//   - tidak memposting apa pun; tidak import PostingService;
//   - tidak dibaca oleh dashboard, trial balance, AR, HPP, atau tutup buku;
//   - hanya dibaca oleh layar laporan historisnya sendiri.
//
// Satu tahun = satu snapshot yang berdiri sendiri. Snapshot 2025 BUKAN
// akumulasi 2021–2025, dan angka 2024 tidak pernah diturunkan dari 2025.
package histfin

import (
	"time"

	"esaproperti/internal/domain"
)

// ── Status ────────────────────────────────────────────────────────────────────

// Status adalah daur hidup snapshot. `final` = angka yang sudah disepakati:
// read-only sampai dibuka kembali dengan alasan.
type Status string

const (
	StatusDraft Status = "draft"
	StatusFinal Status = "final"
)

func (s Status) Valid() bool { return s == StatusDraft || s == StatusFinal }

// ── Jenis laporan ─────────────────────────────────────────────────────────────

// Statement menentukan baris ini masuk Neraca atau Laba Rugi. DITURUNKAN dari
// tipe akun, tidak pernah dipilih user — supaya satu akun tidak bisa muncul di
// laporan yang salah.
type Statement string

const (
	StatementBalanceSheet    Statement = "balance_sheet"
	StatementIncomeStatement Statement = "income_statement"
)

// StatementFor memetakan tipe akun ke jenis laporan. Tipe tak dikenal → "".
func StatementFor(t domain.AccountType) Statement {
	switch t {
	case domain.AccountAsset, domain.AccountLiability, domain.AccountEquity:
		return StatementBalanceSheet
	case domain.AccountRevenue, domain.AccountExpense:
		return StatementIncomeStatement
	}
	return ""
}

// ── Entity ────────────────────────────────────────────────────────────────────

// Snapshot adalah aggregate root: satu tahun buku milik satu tenant.
// Baris hanya hidup di dalamnya — tidak ada operasi yang menyentuh baris tanpa
// melewati root, karena root-lah yang menjaga aturan keseimbangan.
type Snapshot struct {
	ID         uint64 `gorm:"primaryKey;autoIncrement" json:"id"`
	TenantID   uint64 `gorm:"not null;index"           json:"-"`
	FiscalYear int    `gorm:"not null"                 json:"fiscal_year"`
	Status     Status `gorm:"not null;size:10;default:'draft'" json:"status"`
	// Revision naik setiap kali snapshot difinalkan. 0 = belum pernah final.
	Revision    int        `gorm:"not null;default:0"   json:"revision"`
	Notes       string     `gorm:"size:500"             json:"notes,omitempty"`
	FinalizedAt *time.Time `                            json:"finalized_at,omitempty"`
	FinalizedBy *uint64    `                            json:"finalized_by,omitempty"`
	CreatedBy   *uint64    `                            json:"created_by,omitempty"`
	UpdatedBy   *uint64    `                            json:"updated_by,omitempty"`
	CreatedAt   time.Time  `                            json:"created_at"`
	UpdatedAt   time.Time  `                            json:"updated_at"`
}

func (Snapshot) TableName() string { return "financial_snapshots" }

// Editable melaporkan apakah snapshot masih boleh diubah isinya.
func (s Snapshot) Editable() bool { return s.Status == StatusDraft }

// SnapshotLine adalah satu akun beserta nominalnya di tahun tersebut.
//
// AccountCode/Name/Type adalah SNAPSHOT nilai master saat baris dibuat — pola
// yang sama dengan ChargeItem.DepositAccountCode. Laporan tahun lalu tidak boleh
// berubah isinya karena COA diedit tahun ini.
type SnapshotLine struct {
	ID          uint64    `gorm:"primaryKey;autoIncrement" json:"id"`
	TenantID    uint64    `gorm:"not null;index"           json:"-"`
	SnapshotID  uint64    `gorm:"not null;index"           json:"snapshot_id"`
	Statement   Statement `gorm:"not null;size:20"         json:"statement"`
	AccountID   uint64    `gorm:"not null"                 json:"account_id"`
	AccountCode string    `gorm:"not null;size:20"         json:"account_code"`
	AccountName string    `gorm:"not null;size:200"        json:"account_name"`
	// AccountType disimpan sebagai string agar baris historis tidak ikut
	// berubah bila enum domain berkembang.
	AccountType string `gorm:"not null;size:20" json:"account_type"`
	// Amount dalam ARAH NORMAL akun. Boleh negatif (defisit akumulasi nyata).
	Amount    domain.Money `gorm:"type:DECIMAL(20,4);not null;default:'0.0000'" json:"amount"`
	SortOrder int          `gorm:"not null;default:0"       json:"sort_order"`
	CreatedAt time.Time    `                                json:"created_at"`
	UpdatedAt time.Time    `                                json:"updated_at"`
}

func (SnapshotLine) TableName() string { return "financial_snapshot_lines" }

// ── Audit ─────────────────────────────────────────────────────────────────────

// AuditEvent adalah jenis kejadian pada snapshot.
type AuditEvent string

const (
	EventCreated   AuditEvent = "created"
	EventSaved     AuditEvent = "saved"
	EventFinalized AuditEvent = "finalized"
	EventReopened  AuditEvent = "reopened"
)

// Audit adalah jejak APPEND-ONLY perubahan snapshot.
type Audit struct {
	ID          uint64     `gorm:"primaryKey;autoIncrement" json:"id"`
	TenantID    uint64     `gorm:"not null;index"           json:"-"`
	SnapshotID  uint64     `gorm:"not null;index"           json:"snapshot_id"`
	Event       AuditEvent `gorm:"not null;size:20"         json:"event"`
	Revision    int        `gorm:"not null;default:0"       json:"revision"`
	Reason      string     `gorm:"size:500"                 json:"reason,omitempty"`
	TotalAssets domain.Money `gorm:"type:DECIMAL(20,4);not null;default:'0.0000'" json:"total_assets"`
	// TotalLiabEquity sudah termasuk laba/rugi tahun berjalan hasil hitung.
	TotalLiabEquity domain.Money `gorm:"type:DECIMAL(20,4);not null;default:'0.0000'" json:"total_liab_equity"`
	NetIncome       domain.Money `gorm:"type:DECIMAL(20,4);not null;default:'0.0000'" json:"net_income"`
	LineCount       int          `gorm:"not null;default:0"       json:"line_count"`
	ActorID         *uint64      `                                json:"actor_id,omitempty"`
	CreatedAt       time.Time    `                                json:"created_at"`
	UpdatedAt       time.Time    `                                json:"updated_at"`
}

func (Audit) TableName() string { return "financial_snapshot_audits" }

// ── Input service ─────────────────────────────────────────────────────────────

// LineInput adalah satu baris yang dikirim layar editor. Hanya AccountID dan
// Amount yang berasal dari user; kode/nama/tipe di-resolve service dari COA.
type LineInput struct {
	AccountID uint64
	Amount    domain.Money
	SortOrder int
}

// SaveRequest mengganti SELURUH isi snapshot (replace-all). Baris yang tidak
// dikirim berarti dihapus — sesuai perilaku editor yang mengirim keadaan penuh.
type SaveRequest struct {
	Notes  string
	Lines  []LineInput
	Actor  *uint64
}

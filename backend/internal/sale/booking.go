package sale

// Increment 7 — Booking (blueprint §2, CORE): pemesanan unit ber-booking-fee
// sebelum PPJB. Konsumen pertama Unit Lifecycle (Increment 6).
//
// ═══ BUSINESS RULE FINAL KLIEN (2026-07-29) — Booking Fee = PENDAPATAN ═══
// Booking fee BUKAN liability/deposit/DP/bagian harga. Diakui pendapatan
// LANGSUNG saat diterima:
//   - fee diterima : Dr Bank / Cr 4-2100 Pendapatan Booking (disposisi 'recognized')
//   - batal/expire : TANPA jurnal, TANPA refund — pendapatan sudah final
//   - konversi     : TANPA reklas, TANPA buyer_credit — fee tidak pernah
//                    menyentuh harga; outstanding = harga rumah PENUH
//   - Laba Rugi    : tampil sebagai akun tersendiri "Pendapatan Booking";
//                    tidak pernah dipindah ke Penjualan Rumah
//
// ═══ CLIENT FINAL NOTE (2026-09-10) — Booking Fee boleh Rp0 ═══
// Fee 0 = tidak ada uang booking sama sekali (murni reservasi unit). Berlaku
// murni berbasis NOMINAL (bukan tipe unit): TANPA jurnal kas, TANPA termin,
// TANPA kwitansi — disposisi tetap 'recognized' TANPA memandang flag
// Refundable (tidak ada apa pun utk dipegang/direfund). Harga unit TETAP
// nilai kontrak normal (fee tidak pernah bagian harga, dengan atau tanpa
// nominal). Booking fee > 0 memakai jalur di atas TANPA PERUBAHAN.
//
// ═══ JALUR LEGACY (baris histori pra-rule; data-driven, bukan by-deploy) ═══
// Booking lama (disposisi held/transferred/...) tetap diproses rule saat ia
// dibuat (append-only, histori aman):
//   - pra-R4 (termin flag TRUE) : konversi reklas 2-2100→2-2000 + buyer_credit
//   - R4 (flag FALSE, held)     : forfeit/refund via disposisi manual
//
// Semua saldo derived dari ledger; tabel bookings tidak menyimpan saldo.

import (
	"time"

	"esaproperti/internal/domain"
)

// ── Status & disposisi ────────────────────────────────────────────────────────

// BookingStatus adalah state booking. Tanpa `draft` (blueprint §2): pembuatan
// booking = satu aksi atomik bersama penerimaan fee. Semua state terminal
// immutable (append-only spirit) — koreksi = booking baru.
type BookingStatus string

const (
	BookingStatusActive    BookingStatus = "active"
	BookingStatusConverted BookingStatus = "converted" // → Sale Contract (fee TETAP Pendapatan Booking — di luar harga)
	BookingStatusExpired   BookingStatus = "expired"   // lewat expiry_date (via sweep)
	BookingStatusCancelled BookingStatus = "cancelled" // buyer batal pra-PPJB
)

// Valid returns true if s is a recognized BookingStatus.
func (s BookingStatus) Valid() bool {
	switch s {
	case BookingStatusActive, BookingStatusConverted, BookingStatusExpired, BookingStatusCancelled:
		return true
	}
	return false
}

// Terminal reports whether s is a terminal state (immutable).
func (s BookingStatus) Terminal() bool { return s != BookingStatusActive }

// CanTransitionTo: transisi hanya dari active ke salah satu terminal.
func (s BookingStatus) CanTransitionTo(next BookingStatus) bool {
	return s == BookingStatusActive && next.Valid() && next != BookingStatusActive
}

// FeeDisposition mencatat perlakuan akuntansi fee booking.
type FeeDisposition string

const (
	// FeeRecognized (rule klien 2026-07-29): fee diakui Pendapatan Booking
	// (4-2100) saat diterima — FINAL di seluruh lifecycle; batal/konversi
	// tidak mengubahnya. SATU-SATUNYA disposisi utk booking baru.
	FeeRecognized FeeDisposition = "recognized"

	// ── LEGACY (baris histori pra-rule; tidak dipakai booking baru) ──
	FeeHeld          FeeDisposition = "held"           // di 2-2100 (liability lama)
	FeeTransferred   FeeDisposition = "transferred"    // direklas ke 2-2000 (pra-R4)
	FeeForfeited     FeeDisposition = "forfeited"      // hangus ke 4-2000 (lama)
	FeePendingRefund FeeDisposition = "pending_refund" // menunggu domain Refund (lama)
)

// ── Akun (COA) ────────────────────────────────────────────────────────────────

const (
	// accountCodePendapatanBooking: tujuan kredit fee booking BARU (000053).
	accountCodePendapatanBooking = "4-2100"
	// Legacy: dipakai jalur histori (reklas/forfeit/refund baris lama).
	accountCodeTitipanBooking = "2-2100" // Titipan Booking (kewajiban) — migration 000044
	accountCodePendapatanLain = "4-2000" // Pendapatan Lain-lain (tujuan forfeit lama)
)

// ── Model ─────────────────────────────────────────────────────────────────────

// Booking adalah pemesanan unit ber-fee sebelum PPJB. Owner: Project (via Unit).
// Satu booking ACTIVE per unit (UNIQUE tenant+unit+active_key, pola budget_plans).
type Booking struct {
	ID         uint64  `gorm:"primaryKey;autoIncrement" json:"id"`
	TenantID   uint64  `gorm:"not null;index"           json:"-"`
	ProjectID  uint64  `gorm:"not null"                 json:"project_id"`
	UnitID     uint64  `gorm:"not null;index"           json:"unit_id"`
	PhaseID    *uint64 `                                json:"phase_id,omitempty"`
	CustomerID uint64  `gorm:"not null"                 json:"customer_id"`
	// SalesPersonID: atribusi funnel/KPI (blueprint §4). LeadID: SEAM Lead/CRM —
	// kolom referensi tanpa FK (tabel lead belum ada; prinsip reservasi kolom).
	SalesPersonID *uint64 `json:"sales_person_id,omitempty"`
	LeadID        *uint64 `json:"lead_id,omitempty"`

	BookingFee  domain.Money `gorm:"type:DECIMAL(20,4);not null;default:'0.0000'" json:"booking_fee"`
	Refundable  bool         `gorm:"not null;default:false"                       json:"refundable"`
	BookingDate time.Time    `gorm:"not null"                                     json:"booking_date"`
	ExpiryDate  time.Time    `gorm:"not null"                                     json:"expiry_date"`

	Status         BookingStatus  `gorm:"size:20;not null;default:'active'" json:"status"`
	FeeDisposition FeeDisposition `gorm:"size:20;not null;default:'held'"   json:"fee_disposition"`

	// TerminPaymentID: termin penerimaan fee (jurnal Dr Bank / Cr 2-2100 + kwitansi).
	// NULL bila BookingFee = 0 (client final note 2026-09-10): tidak ada uang
	// diterima → TANPA termin, TANPA jurnal, TANPA kwitansi (lihat CreateBookingAtomic).
	TerminPaymentID *uint64 `gorm:"index" json:"termin_payment_id,omitempty"`

	// ReceiptID/ReceiptNumber (INV-DOC-1, read-only): kwitansi KWB penerimaan
	// fee. Kwitansinya SELALU terbit — atomik bersama booking, lihat
	// booking_repo.go langkah 3 — tapi sebelumnya tidak pernah ikut di payload,
	// sehingga di layar booking terlihat seolah tidak ada dokumennya sama
	// sekali. Bukti yang ada tapi tak terlihat sama saja dengan tidak ada bagi
	// admin yang harus menyerahkan lembarannya ke pembeli.
	// gorm:"-" — bukan kolom tabel bookings; diisi dari tabel receipts saat baca.
	ReceiptID     *uint64 `gorm:"-" json:"receipt_id,omitempty"`
	ReceiptNumber string  `gorm:"-" json:"receipt_number,omitempty"`

	ConvertedContractID *uint64 `           json:"converted_contract_id,omitempty"`
	ForfeitJournalID    *uint64 `           json:"forfeit_journal_id,omitempty"`
	ReclassJournalID    *uint64 `           json:"reclass_journal_id,omitempty"`

	CloseReason     string     `gorm:"size:500"  json:"close_reason,omitempty"`
	ClosedAt        *time.Time `               json:"closed_at,omitempty"`
	ClosedEventDate *time.Time `               json:"closed_event_date,omitempty"` // tanggal kejadian bisnis (≠ waktu pencatatan)
	ClosedBy        *uint64    `               json:"closed_by,omitempty"`

	Notes     string  `gorm:"size:500" json:"notes,omitempty"`
	CreatedBy *uint64 `             json:"created_by,omitempty"`
	// ActiveKey: 'Y' saat active, NULL saat terminal (satu aktif per unit).
	ActiveKey *string   `gorm:"size:1" json:"-"`
	CreatedAt time.Time `             json:"created_at"`
	UpdatedAt time.Time `             json:"updated_at"`
}

func (Booking) TableName() string { return "bookings" }

// ── Request types ─────────────────────────────────────────────────────────────

// CreateBookingRequest adalah input pembuatan booking (atomik dengan fee).
type CreateBookingRequest struct {
	UnitID          uint64
	CustomerID      uint64
	SalesPersonID   *uint64
	LeadID          *uint64
	BookingFee      domain.Money
	Refundable      bool
	BankAccountCode string    // akun kas/bank penerima fee (COA-driven)
	BookingDate     time.Time // tanggal kejadian (default: sekarang di handler)
	ExpiryDate      time.Time // wajib > BookingDate
	Notes           string
	CreatedBy       *uint64
}

// CloseBookingInput adalah parameter penutupan (cancel/expire) satu booking.
type CloseBookingInput struct {
	NextStatus BookingStatus // cancelled | expired
	Reason     string
	EventDate  time.Time // tanggal kejadian bisnis
	ActorID    *uint64
	// Resolved account IDs untuk jurnal forfeit (bila non-refundable).
	TitipanAccountID     uint64
	OtherIncomeAccountID uint64
}

// ConvertBookingInput adalah parameter konversi booking → kontrak.
type ConvertBookingInput struct {
	ActorID *uint64
	// Resolved account IDs untuk jurnal reklas Titipan → Uang Muka.
	TitipanAccountID  uint64
	UangMukaAccountID uint64
	EventDate         time.Time // = contract date
}

// TransferBookingInput adalah parameter transfer booking active ke unit lain
// (Item 3, 2026-09). TANPA jurnal: fee sudah diterima & dicatat (recognized
// atau held) di titik unit ASAL — ledger append-only (invariant #5) melarang
// menulis-ulang jurnal/termin/kwitansi historis. Yang berpindah HANYA baris
// booking (unit_id/project_id/phase_id) + status kedua unit; karena tidak ada
// penerimaan kas atau jurnal pendapatan baru, TIDAK MUNGKIN terjadi double
// revenue.
type TransferBookingInput struct {
	NewUnitID uint64
	Reason    string
	EventDate time.Time
	ActorID   *uint64
}

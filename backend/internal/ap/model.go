// Package ap (W-11) memegang HUTANG USAHA: kewajiban kepada vendor yang lahir
// saat pekerjaan/barangnya diakui, bukan saat uangnya dibayar.
//
// Satu kalimat yang menjelaskan seluruh desain paket ini:
//
//	Tagihan vendor adalah SATU kejadian ekonomi yang dicatat di dua tempat —
//	buku besar (kewajiban) dan buku pembantu biaya (cost_entries) — dan
//	keduanya lahir dari transaksi yang sama atau tidak lahir sama sekali.
//
// KENAPA BARIS BIAYANYA DI `cost_entries`, BUKAN DI TABEL SENDIRI (D-4)
//
// Ada DUA pembaca realisasi RAB di sistem ini, dan keduanya membaca sumber yang
// berbeda:
//
//	budget.GetRealisasiByProject  → LEDGER (akun taksonomi, ber-tag project)
//	budget.GetRealisasiPerItem    → SUM(cost_entries.amount) per item RAB
//
// Kalau baris biaya AP disimpan di tabel `ap_invoice_lines` sendiri, pembaca
// pertama akan melihatnya (karena jurnalnya ada) sementara pembaca kedua buta.
// Hasilnya: total realisasi proyek naik, tetapi tidak satu pun item RAB yang
// bertambah — dua laporan yang keduanya "benar" menurut kodenya masing-masing,
// dan tidak ada error yang muncul. Karena itu `ap_invoice_lines` DILARANG.
//
// Yang sengaja TIDAK dibuat di paket ini:
//   - Aturan baris biaya sendiri. Validasi & pemilihan akun debit dipinjam dari
//     cost.PlanAPLine (A-1) supaya hanya ada satu definisi "baris biaya sah".
//   - Mesin aging kedua. Klasifikasi umur memakai receivable.BucketFor yang sama
//     dengan piutang; hanya bentuk barisnya yang milik AP.
//   - Mesin approval kedua. Gate memakai approval.RequireApproved yang ada
//     (OPT-IN: tenant tanpa workflow aktif tidak terhalang apa pun).
//   - Mesin penomoran kedua & jenis dokumen baru. Setiap pengeluaran kas memakai
//     BKK lewat Document Domain W-2/W-3, sehingga INV-DOC-1 berlaku apa adanya.
//   - Kolom status & sisa tagihan. Keduanya DITURUNKAN dari sub-ledger (D-12);
//     kolom cache adalah cara paling mudah membuat dua angka yang tak sepakat.
package ap

import (
	"time"

	"esaproperti/internal/domain"
)

// ── Sumber jurnal ─────────────────────────────────────────────────────────────

// Nilai `journal_entries.source` milik domain ini. Dipakai tie-out AP↔GL
// (INV-AP-1): tiap kelompok jurnal harus bisa ditemukan tanpa menebak dari
// deskripsi.
//
// PERHATIAN: kolomnya `varchar(20)` tanpa CHECK. Tidak ada migrasi yang
// dibutuhkan untuk menambah nilai, tetapi nilai yang lebih panjang dari 20
// karakter akan terpotong DIAM-DIAM di MySQL non-strict — dan jurnal yang
// source-nya terpotong hilang dari tie-out tanpa satu pun error.
// `ap_retention_release` panjangnya tepat 20; sengaja dipendekkan.
const (
	SourceAPInvoice   = "ap_invoice"   // pengakuan kewajiban (Dr biaya / Cr hutang)
	SourceAPPayment   = "ap_payment"   // pelunasan (Dr hutang / Cr kas)
	SourceAPAdvance   = "ap_advance"   // uang muka vendor (Dr 1-5300 / Cr kas)
	SourceAPRetention = "ap_retention" // pelepasan retensi (Dr 2-1100 / Cr kas)
)

// ── Vendor ────────────────────────────────────────────────────────────────────

// Vendor adalah master minimal (D-7): identitas + status PKP. Bukan modul
// procurement — tidak ada kontrak, rating, atau termin default di sini.
//
// Kolom teks `cost_entries.vendor` SENGAJA tidak dihapus dan tidak di-backfill.
// Ia adalah nama yang tertulis di dokumen saat itu; VendorID adalah tautan ke
// master. Kalau master kelak berganti nama, bukti historis tidak ikut berubah.
type Vendor struct {
	ID       uint64 `gorm:"primaryKey;autoIncrement" json:"id"`
	TenantID uint64 `gorm:"not null;index"           json:"-"`
	Name     string `gorm:"not null;size:200"        json:"name"`
	// NPWP & IsPKP menentukan APAKAH sebuah tagihan boleh membawa PPN Masukan.
	// Statusnya disimpan di vendor, tetapi nilai PPN-nya TIDAK dihitung dari
	// sini — lihat Invoice.PPNAmount.
	NPWP     string `gorm:"size:25"            json:"npwp,omitempty"`
	IsPKP    bool   `gorm:"not null;default:0" json:"is_pkp"`
	Address  string `gorm:"size:500"           json:"address,omitempty"`
	Phone    string `gorm:"size:50"            json:"phone,omitempty"`
	Email    string `gorm:"size:200"           json:"email,omitempty"`
	BankName string `gorm:"size:100"           json:"bank_name,omitempty"`
	BankAcc  string `gorm:"size:50;column:bank_account" json:"bank_account,omitempty"`
	// IsActive: vendor dinonaktifkan, TIDAK dihapus. Menghapusnya akan memutus
	// tautan pada tagihan & baris biaya yang sudah terbit.
	IsActive  bool      `gorm:"not null;default:1" json:"is_active"`
	Note      string    `gorm:"size:500"           json:"note,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (Vendor) TableName() string { return "vendors" }

// ── Tagihan (kewajiban) ───────────────────────────────────────────────────────

// InvoiceStatus adalah daur hidup PENGAKUAN — bukan status pembayaran.
//
// Status pembayaran (lunas/sebagian/belum) TIDAK ada di sini dan tidak disimpan
// di kolom mana pun: ia diturunkan dengan menjumlahkan alokasi pembayaran
// (D-12). Kolom cache "sisa tagihan" adalah cara termudah melahirkan dua angka
// yang tidak sepakat — satu di daftar, satu di detail.
type InvoiceStatus string

const (
	// InvoiceDraft: BELUM ADA apa pun di buku. Bukan "setengah tercatat" —
	// tidak ada saldo hutang, tidak ada realisasi RAB, tidak ada baris aging
	// (INV-AP-9). Jurnalnya sudah dibuat sebagai DRAFT karena
	// cost_entries.journal_entry_id NOT NULL, tetapi jurnal draft tidak dibaca
	// oleh satu pun laporan (semuanya posted-only).
	InvoiceDraft InvoiceStatus = "draft"
	// InvoicePosted: kewajiban lahir. Enam pembaca menyala serentak (D-3):
	// buku besar, realisasi RAB per proyek, realisasi per item RAB, aging
	// hutang, saldo peran, dan tie-out.
	InvoicePosted InvoiceStatus = "posted"
	// InvoiceReversed: dibalik lewat jurnal pembalik. Barisnya TIDAK dihapus dan
	// tidak diedit (Invariant #5 append-only).
	InvoiceReversed InvoiceStatus = "reversed"
)

func (s InvoiceStatus) Valid() bool {
	return s == InvoiceDraft || s == InvoicePosted || s == InvoiceReversed
}

// Invoice adalah satu kewajiban kepada vendor.
//
// ATURAN NOMINAL (PPN-1..PPN-6, D-16) — ini bagian yang paling mudah salah:
//
//	DPPAmount       = nilai pekerjaan; INI yang menjadi biaya & realisasi RAB
//	PPNAmount       = PPN Masukan; aset (1-5100), BUKAN biaya, TANPA tag proyek
//	RetentionAmount = bagian DPP yang ditahan; pindah ke kewajiban lain (2-1100)
//	PayableAmount   = (DPP + PPN) − Retensi − kompensasi uang muka
//
// Σ cost_entries.amount milik tagihan ini == DPPAmount, selalu (INV-AP-8).
// Retensi, PPN, dan kompensasi uang muka HANYA memecah sisi KREDIT jurnal;
// tidak satu pun dari mereka boleh mengubah nilai biaya yang diakui.
type Invoice struct {
	ID       uint64 `gorm:"primaryKey;autoIncrement" json:"id"`
	TenantID uint64 `gorm:"not null;index"           json:"-"`
	VendorID uint64 `gorm:"not null;index"           json:"vendor_id"`
	// ProjectID nullable: tagihan overhead (mis. jasa konsultan kantor) tidak
	// bermuara ke proyek mana pun. Konsisten dengan cost_entries.project_id yang
	// juga nullable sejak Increment 2.
	ProjectID *uint64 `gorm:"index" json:"project_id,omitempty"`
	// InvoiceNumber adalah nomor dari VENDOR, bukan nomor terbitan kita.
	// Sengaja TIDAK unique: dua vendor berbeda boleh punya nomor yang sama, dan
	// memaksakan keunikan akan menolak tagihan yang sah.
	InvoiceNumber string    `gorm:"not null;size:100" json:"invoice_number"`
	InvoiceDate   time.Time `gorm:"type:date;not null" json:"invoice_date"`
	DueDate       time.Time `gorm:"type:date;not null" json:"due_date"`

	DPPAmount       domain.Money `gorm:"type:DECIMAL(20,4);not null;default:'0.0000'" json:"dpp_amount"`
	PPNAmount       domain.Money `gorm:"type:DECIMAL(20,4);not null;default:'0.0000'" json:"ppn_amount"`
	RetentionAmount domain.Money `gorm:"type:DECIMAL(20,4);not null;default:'0.0000'" json:"retention_amount"`
	AdvanceApplied  domain.Money `gorm:"type:DECIMAL(20,4);not null;default:'0.0000'" json:"advance_applied"`
	PayableAmount   domain.Money `gorm:"type:DECIMAL(20,4);not null;default:'0.0000'" json:"payable_amount"`

	// FakturPajakNumber wajib bila PPNAmount > 0 (PPN-5): PPN Masukan tanpa
	// faktur tidak bisa dikreditkan, jadi mencatatnya sebagai aset adalah klaim
	// yang tidak punya dasar.
	FakturPajakNumber string `gorm:"size:100" json:"faktur_pajak_number,omitempty"`
	// RetentionDueDate: kapan retensi boleh dilepas. NULL bila tanpa retensi.
	RetentionDueDate *time.Time `gorm:"type:date" json:"retention_due_date,omitempty"`

	Status InvoiceStatus `gorm:"not null;size:12;default:'draft'" json:"status"`
	// JournalEntryID menunjuk jurnal PENGAKUAN. Terisi sejak draft — jurnalnya
	// dibuat lebih dulu karena cost_entries.journal_entry_id NOT NULL.
	JournalEntryID uint64 `gorm:"not null;index" json:"journal_entry_id"`
	// ReversalJournalEntryID terisi saat dibalik, sehingga status `reversed`
	// bisa dibuktikan tanpa menelusuri balik ke tabel jurnal.
	ReversalJournalEntryID *uint64 `json:"reversal_journal_entry_id,omitempty"`

	Description string     `gorm:"size:500" json:"description"`
	CreatedBy   uint64     `json:"created_by"`
	PostedAt    *time.Time `json:"posted_at,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

func (Invoice) TableName() string { return "ap_invoices" }

// ── Pembayaran ────────────────────────────────────────────────────────────────

// PaymentKind membedakan TIGA pengeluaran kas yang jurnalnya berbeda dan tidak
// boleh saling menggantikan:
//
//	invoice   Dr 2-1000 Hutang Usaha    / Cr Kas — melunasi kewajiban
//	advance   Dr 1-5300 Uang Muka Vendor/ Cr Kas — membentuk ASET, bukan biaya
//	retention Dr 2-1100 Hutang Retensi  / Cr Kas — melepas tahanan
//
// Satu pun dari ketiganya TIDAK menyentuh akun taksonomi biaya, sehingga
// pembayaran mustahil terhitung ulang sebagai realisasi RAB (INV-AP-3/4).
type PaymentKind string

const (
	PaymentKindInvoice   PaymentKind = "invoice"
	PaymentKindAdvance   PaymentKind = "advance"
	PaymentKindRetention PaymentKind = "retention"
)

func (k PaymentKind) Valid() bool {
	return k == PaymentKindInvoice || k == PaymentKindAdvance || k == PaymentKindRetention
}

// Payment adalah satu pengeluaran kas ke vendor. Satu baris = satu pergerakan
// kas = satu dokumen BKK bernomor (INV-DOC-1/INV-AP-5).
type Payment struct {
	ID       uint64      `gorm:"primaryKey;autoIncrement" json:"id"`
	TenantID uint64      `gorm:"not null;index"           json:"-"`
	VendorID uint64      `gorm:"not null;index"           json:"vendor_id"`
	Kind     PaymentKind `gorm:"not null;size:12;column:payment_kind" json:"payment_kind"`

	PaymentDate     time.Time    `gorm:"type:date;not null" json:"payment_date"`
	Amount          domain.Money `gorm:"type:DECIMAL(20,4);not null;default:'0.0000'" json:"amount"`
	CashAccountCode string       `gorm:"not null;size:20" json:"cash_account_code"`

	JournalEntryID uint64     `gorm:"not null;index" json:"journal_entry_id"`
	DocumentNumber string     `gorm:"size:50"        json:"document_number"`
	ReversedAt     *time.Time `json:"reversed_at,omitempty"`

	// IdempotencyKey mencegah pembayaran ganda dari klik dobel / retry jaringan.
	// Unique per tenant. Nullable: pemanggil yang tidak mengirim kunci tetap
	// dilayani — tetapi tanpa perlindungan itu.
	IdempotencyKey *string `gorm:"size:100" json:"-"`

	Description string    `gorm:"size:500" json:"description"`
	CreatedBy   uint64    `json:"created_by"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func (Payment) TableName() string { return "ap_payments" }

// ── Alokasi ───────────────────────────────────────────────────────────────────

// AllocationType menyatakan KE APA sebuah pembayaran dibebankan.
//
// `advance` menandai KONSUMSI uang muka, bukan pembentukannya. Membayar uang
// muka TIDAK melahirkan baris alokasi sama sekali — saat itu belum ada tagihan
// yang dituju, sementara InvoiceID di sini NOT NULL. Yang lahir hanya satu
// baris Payment berjenis advance.
//
// Karena itu saldo uang muka yang tersedia SELALU dihitung sebagai
// (nilai pembayaran uang muka − Σ alokasi konsumsi yang belum dibalik), bukan
// disimpan sebagai kolom. Inilah satu-satunya sumber kanonik saldo uang muka —
// dan karena itu ia pula yang harus dikunci saat kompensasi berjalan (R-12).
//
// Penjumlahan itu dilakukan per SourcePaymentID, sehingga baris `advance` yang
// tidak menunjuk sumber akan mengurangi tagihan tanpa pernah ikut terhitung
// sebagai saldo terpakai — saldo yang sama lalu bisa dipakai lagi. DB menolak
// bentuk itu lewat chk_apa_origin (migrasi 000073), jadi ia mustahil walau kode
// pemanggilnya kelak berubah.
type AllocationType string

const (
	AllocationInvoice   AllocationType = "invoice"
	AllocationRetention AllocationType = "retention"
	AllocationAdvance   AllocationType = "advance"
)

func (t AllocationType) Valid() bool {
	return t == AllocationInvoice || t == AllocationRetention || t == AllocationAdvance
}

// Allocation memasangkan sebagian nilai pembayaran ke satu tagihan.
//
// Sub-ledger inilah yang menentukan sisa tagihan — bukan kolom pada Invoice.
// Satu pembayaran boleh terpecah ke banyak tagihan, dan satu tagihan boleh
// menerima banyak pembayaran; keduanya kejadian normal.
type Allocation struct {
	ID       uint64 `gorm:"primaryKey;autoIncrement" json:"id"`
	TenantID uint64 `gorm:"not null;index"           json:"-"`
	// PaymentID nullable: kompensasi uang muka ke tagihan BUKAN pengeluaran kas
	// baru, jadi ia tidak punya baris Payment sendiri. Yang menjadi sumbernya
	// adalah pembayaran uang muka terdahulu — lihat SourcePaymentID.
	PaymentID *uint64        `gorm:"index" json:"payment_id,omitempty"`
	InvoiceID uint64         `gorm:"not null;index" json:"invoice_id"`
	Type      AllocationType `gorm:"not null;size:12;column:allocation_type" json:"allocation_type"`
	Amount    domain.Money   `gorm:"type:DECIMAL(20,4);not null;default:'0.0000'" json:"amount"`
	// SourcePaymentID menunjuk pembayaran UANG MUKA yang dikonsumsi (hanya untuk
	// Type=advance). Inilah kunci yang dipakai menjumlahkan saldo uang muka
	// terpakai — dan yang barisnya dikunci saat kompensasi (R-12).
	SourcePaymentID *uint64 `gorm:"index" json:"source_payment_id,omitempty"`
	// ReversedAt: alokasi dibatalkan lewat pembalikan, tidak dihapus. Seluruh
	// perhitungan sisa & saldo uang muka mengabaikan baris yang sudah dibalik.
	ReversedAt *time.Time `json:"reversed_at,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
}

func (Allocation) TableName() string { return "ap_payment_allocations" }

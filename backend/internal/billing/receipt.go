package billing

import (
	"context"
	"time"

	"esaproperti/internal/domain"
)

// ── Receipt (Kwitansi) ────────────────────────────────────────────────────────
//
// Receipt adalah bukti penerimaan pembayaran atas satu transaksi termin
// (termin_payment). Idempoten: maksimal satu receipt per termin (duplicate
// prevention via UNIQUE (tenant_id, termin_payment_id)).
//
// Receipt TIDAK memposting jurnal apa pun — jurnal kas sudah diposting saat
// termin diterima (Event 2: Dr Bank / Cr Uang Muka Penjualan). Receipt hanya
// dokumen; tidak ada duplikasi pencatatan akuntansi.

// ReceiptType memisahkan kwitansi BOOKING (Pendapatan Booking — di luar harga
// unit) dari kwitansi pembayaran RUMAH. Penomoran terpisah per tipe.
type ReceiptType string

const (
	ReceiptTypeHousePayment ReceiptType = "house_payment" // KWT/{yyyy}/{seq}
	ReceiptTypeBooking      ReceiptType = "booking"       // KWB/{yyyy}/{seq}
	// ReceiptTypeRealization (Billing Batch 2, K-1/K-5): kwitansi pembayaran
	// Biaya Realisasi (Titipan) — KWR/{yyyy}/{seq}. WAJIB menampilkan: total
	// tagihan grup, pembayaran ini, total dibayar, sisa outstanding.
	ReceiptTypeRealization ReceiptType = "realization"
	// ReceiptTypeKPRDisbursement (W-3.2, jalur I-3): pencairan KPR dari bank —
	// KWD/{yyyy}/{seq}. Seri sendiri karena PEMBAYARNYA bank, bukan customer:
	// menomorinya sebagai KWT membuat buku kwitansi customer memuat lembar yang
	// tidak pernah diserahkan ke customer mana pun. 8 pencairan lama yang
	// terlanjur bernomor KWT DIBIARKAN — dokumen historis immutable, KWD
	// berlaku forward-only.
	ReceiptTypeKPRDisbursement ReceiptType = "kpr_disbursement"
)

type Receipt struct {
	ID              uint64       `gorm:"primaryKey;autoIncrement"                     json:"id"`
	TenantID        uint64       `gorm:"not null;index"                               json:"-"`
	TerminPaymentID uint64       `gorm:"not null"                                     json:"termin_payment_id"`
	UnitID          uint64       `gorm:"not null;index"                               json:"unit_id"`
	SaleContractID  *uint64      `gorm:"index"                                        json:"sale_contract_id,omitempty"`
	// ChargeGroupID (Billing Batch 2): grup tagihan realisasi/addon yang dibayar
	// — sumber ringkasan 4-angka pada kwitansi KWR. NULL untuk KWT/KWB.
	ChargeGroupID *uint64 `gorm:"index"                                        json:"charge_group_id,omitempty"`
	ReceiptType     ReceiptType  `gorm:"not null;size:20;default:'house_payment'"     json:"receipt_type"`
	ReceiptNumber   string       `gorm:"not null;size:30"                             json:"receipt_number"`
	Amount          domain.Money `gorm:"type:DECIMAL(20,4);not null;default:'0.0000'" json:"amount"`
	BankAccountCode string       `gorm:"not null;size:20"                             json:"bank_account_code"`
	ReceivedAt      time.Time    `gorm:"not null"                                     json:"received_at"`
	Notes           string       `gorm:"size:500"                                     json:"notes,omitempty"`
	CreatedBy       uint64       `gorm:"not null"                                     json:"created_by"`
	CreatedAt       time.Time    `                                                    json:"created_at"`
	UpdatedAt       time.Time    `                                                    json:"updated_at"`
}

func (Receipt) TableName() string { return "receipts" }

// W-2: model `ReceiptSequence` DIHAPUS. Penomoran kwitansi kini dilayani
// `internal/document` di atas `document_sequences`. Tabel `receipt_sequences`
// masih ada semata-mata agar migration 000065 bisa di-rollback tanpa
// menerbitkan ulang nomor yang sudah tercetak — bukan untuk dibaca kode.

// TerminInfo adalah data transaksi termin minimal yang dibutuhkan untuk
// membuat receipt. Dimuat langsung dari tabel termin_payments (tanpa import
// package sale — hindari circular import, pola yang sama dengan ContractInfo).
type TerminInfo struct {
	ID              uint64
	UnitID          uint64
	Amount          domain.Money
	BankAccountCode string
	Date            time.Time
	Description     string
}

// ReceiptPrintData holds the fully-joined data to render a receipt A4 page.
type ReceiptPrintData struct {
	// ReceiptType (UAT Batch 2 §1) menentukan JUDUL kwitansi saja — mis.
	// "Kwitansi Booking", "Kwitansi Biaya Realisasi". Keterangan perlakuan
	// akuntansinya tidak lagi dicetak: lihat catatan di receipt_print.go.
	ReceiptType     ReceiptType
	ReceiptNumber   string
	Amount          domain.Money
	ReceivedAt      time.Time
	BankAccountCode string
	Notes           string
	// Company (tenant)
	CompanyName string
	// Customer (contract)
	BuyerName   string
	BuyerID     string
	PaymentType string
	// DisbursingBankName (E-2): nama bank penyalur untuk kwitansi KWD.
	// Pada pencairan KPR yang MENYETOR uang adalah bank, bukan customer —
	// itulah alasan KWD dipisah menjadi seri dokumen sendiri. Namanya dibaca
	// dari master financing_sources (atau kolom bank_kpr kontrak), TIDAK
	// pernah di-hardcode. Kosong = master tidak terisi; template jatuh ke
	// nama pembeli agar kwitansi tidak pernah tercetak tanpa penyetor.
	DisbursingBankName string
	// Property
	ProjectName string
	UnitCode    string
	UnitType    string
	// Ringkasan finansial kontrak (hardening). HasSummary=false bila termin
	// tidak terkait kontrak (mis. penerimaan lama tanpa kontrak) — template
	// menampilkan "—". Nilai NOL tetap ditampilkan (requirement klien).
	HasSummary      bool
	UnitPrice       domain.Money
	Discount        domain.Money
	NetContract     domain.Money
	TotalPaid       domain.Money
	Outstanding     domain.Money
	PriceIsSnapshot bool
	// Ringkasan CHARGE GROUP (Billing Batch 2, K-5) — kwitansi KWR wajib tampil
	// 4 angka: total tagihan / bayar ini / total dibayar / sisa. Sumbernya
	// ChargeSummaryProvider (formula kanonik charge.Service.GroupSummary) —
	// billing TIDAK menghitung sendiri (no duplicate SoT).
	HasChargeSummary bool
	ChargeGroupLabel string
	ChargeBilled     domain.Money // total tagihan grup
	ChargePaid       domain.Money // total sudah dibayar (net void)
	ChargeOutstanding domain.Money // sisa terutang
}

// ── Ringkasan finansial kontrak (hardening Receipt/Invoice) ───────────────────
// Sumber TUNGGAL: sale.ContractFinancialSummary via adapter di wiring (main).
// Billing TIDAK menghitung sendiri — no duplicate SoT, receipt & invoice
// dijamin memakai rumus yang sama.

// ContractSummary adalah potret ringkasan keuangan kontrak untuk dokumen cetak.
type ContractSummary struct {
	UnitPrice       domain.Money
	PriceIsSnapshot bool
	Discount        domain.Money
	NetContract     domain.Money
	TotalPaid       domain.Money
	Outstanding     domain.Money
}

// ContractSummaryProvider disediakan wiring layer (adapter ke sale.Service).
type ContractSummaryProvider interface {
	SummaryByContractID(ctx context.Context, tenantID, contractID uint64) (*ContractSummary, error)
	SummaryByUnitID(ctx context.Context, tenantID, unitID uint64) (*ContractSummary, error)
}

// ChargeGroupSummary adalah potret ringkasan satu grup tagihan untuk dokumen.
type ChargeGroupSummary struct {
	Label       string
	Billed      domain.Money
	Paid        domain.Money
	Outstanding domain.Money
}

// ChargeSummaryProvider disediakan wiring layer (adapter ke charge.Service) —
// formula kanonik outstanding grup; billing hanya menampilkan.
type ChargeSummaryProvider interface {
	SummaryByGroupID(ctx context.Context, tenantID, groupID uint64) (*ChargeGroupSummary, error)
}

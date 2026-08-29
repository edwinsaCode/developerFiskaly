// Package charge — Billing Batch 2: Charge Group (Titipan Realisasi & Addon).
//
// Business rule FINAL klien 2026-08-03:
//
//	K-1 Seluruh biaya realisasi = TITIPAN (liability 2-2400). Bukan pendapatan,
//	    bukan bagian harga rumah. Terima: Dr Kas / Cr 2-2400. Payout vendor:
//	    Dr 2-2400 / Cr Kas. Pendapatan developer TIDAK PERNAH lahir di sini.
//	K-2 Realisasi tidak menahan BAST secara default — tenant policy
//	    (tenants.require_realization_settled).
//	K-3 Alokasi pembayaran FLEXIBLE: keputusan admin per item. Sistem hanya
//	    memvalidasi (Σ alokasi == nominal PERSIS; per-item ≤ outstanding item).
//	K-4 Sisa titipan (aktual < titipan) → refund / transfer. BUKAN pendapatan.
//	K-5 Aktual > titipan → outstanding tambahan otomatis (amount item di-raise,
//	    ber-audit di charge_item_adjustments).
//
// Anti duplicate-SoT: kas masuk = termin_payments (satu tabel), alokasi =
// payment_allocations (satu sub-ledger), aging = reporting.BuildARAging (satu
// mesin). FORMULA outstanding grup HANYA di GroupSummary — receipt, invoice,
// statement, dashboard, aging, dan gate BAST semuanya membaca dari sana.
package charge

import (
	"time"

	"esaproperti/internal/domain"
)

// AccountCodeTitipanRealisasi adalah akun titipan realisasi DEFAULT — ter-registry
// di ledger.RoleRealizationDeposit.
//
// Ini BUKAN akun tunggal seluruh titipan. Akun kewajiban adalah atribut masing-
// masing RealizationChargeType (lihat charge_type.go): Notaris, BPHTB, PDAM, dan
// Listrik boleh mendarat di akun yang berbeda-beda, dan satu grup boleh memuat
// campurannya. Konstanta ini hanya dipakai untuk hal yang memang tidak punya
// jenis biaya: grup addon dan item historis pra-W-1.
const AccountCodeTitipanRealisasi = "2-2400"

// ── Kind / Status ────────────────────────────────────────────────────────────

type GroupKind string

const (
	KindRealization GroupKind = "realization" // PDAM/BPHTB/Notaris/Listrik dkk
	KindAddon       GroupKind = "addon"       // produk tambahan (kelebihan tanah, dll)
)

func (k GroupKind) Valid() bool { return k == KindRealization || k == KindAddon }

type GroupStatus string

const (
	GroupOpen      GroupStatus = "open"
	GroupSettled   GroupStatus = "settled"
	GroupCancelled GroupStatus = "cancelled"
)

type ItemStatus string

const (
	ItemOpen      ItemStatus = "open"
	ItemCancelled ItemStatus = "cancelled"
)

// ── Entities ─────────────────────────────────────────────────────────────────

// ChargeGroup adalah agregat penagihan di bawah SATU SaleContract (prinsip
// klien: rumah & realisasi = dua billing, satu kontrak, satu statement).
type ChargeGroup struct {
	ID             uint64      `gorm:"primaryKey;autoIncrement"        json:"id"`
	TenantID       uint64      `gorm:"not null;index"                  json:"-"`
	SaleContractID uint64      `gorm:"not null;index"                  json:"sale_contract_id"`
	UnitID         uint64      `gorm:"not null;index"                  json:"unit_id"`
	Kind           GroupKind   `gorm:"not null;size:20"                json:"kind"`
	Label          string      `gorm:"not null;size:120"               json:"label"`
	Status         GroupStatus `gorm:"not null;size:20;default:'open'" json:"status"`
	Notes          string      `gorm:"size:500"                        json:"notes,omitempty"`
	CreatedBy      *uint64     `                                       json:"created_by,omitempty"`
	CreatedAt      time.Time   `                                       json:"created_at"`
	UpdatedAt      time.Time   `                                       json:"updated_at"`
}

func (ChargeGroup) TableName() string { return "charge_groups" }

// ChargeItem adalah satu item tagihan. Amount = tagihan BERJALAN — hanya
// berubah lewat true-up K-4/K-5 dan SELALU meninggalkan baris audit di
// charge_item_adjustments. OriginalAmount immutable.
type ChargeItem struct {
	ID            uint64 `gorm:"primaryKey;autoIncrement"        json:"id"`
	TenantID      uint64 `gorm:"not null;index"                  json:"-"`
	ChargeGroupID uint64 `gorm:"not null;index"                  json:"charge_group_id"`
	Label         string `gorm:"not null;size:120"               json:"label"`
	// ChargeTypeCode (W-1) menunjuk master realization_charge_types. Item BARU
	// wajib mengisinya (fail-closed di service); NULL hanya untuk baris historis
	// pra-000061 — histori tidak ditulis ulang (invariant #5).
	ChargeTypeCode *string `gorm:"size:50"                         json:"charge_type_code,omitempty"`
	// ProductCode (W-13) menunjuk master katalog produk `product_types`. Item
	// BARU pada grup addon wajib mengisinya (fail-closed di service) — inilah
	// yang membuat "Kelebihan Tanah" berhenti menjadi teks bebas dan menjadi
	// produk yang dijual, dengan akun pendapatan miliknya sendiri.
	// NULL untuk item realisasi dan untuk baris addon historis pra-W-13.
	ProductCode *string `gorm:"size:50"                         json:"product_code,omitempty"`
	// DepositAccountCode adalah SNAPSHOT akun kewajiban jenis biaya ini pada saat
	// item dibuat — pola yang sama dengan Label. Akun boleh berbeda antar item
	// dalam satu grup; jurnalnya yang pecah, bukan grupnya. Snapshot dipakai
	// (bukan lookup master saat transaksi) supaya jurnal pembalik selalu memukul
	// akun yang benar-benar dikredit dulu, meski master sudah dipindah sejak itu.
	DepositAccountCode string `gorm:"not null;size:20;default:'2-2400'" json:"deposit_account_code"`
	// RevenueAccountCode (W-13) adalah SNAPSHOT akun pendapatan produk addon ini
	// pada saat item dibuat — pasangan dari DepositAccountCode, bukan
	// penggantinya. Keduanya hidup berdampingan karena uang addon melewati DUA
	// akun: mendarat di kewajiban saat kas diterima (Invariant #7), lalu pindah
	// ke pendapatan saat kriteria pengakuan terpenuhi.
	// NULL = item realisasi (tidak pernah menjadi pendapatan — K-1) atau baris
	// addon historis pra-W-13 yang memang belum punya produk.
	RevenueAccountCode *string      `gorm:"size:20"                         json:"revenue_account_code,omitempty"`
	Amount             domain.Money `gorm:"type:DECIMAL(20,4);not null;default:'0.0000'" json:"amount"`
	OriginalAmount     domain.Money `gorm:"type:DECIMAL(20,4);not null;default:'0.0000'" json:"original_amount"`
	// RecognizedAmount (W-5/R-5) adalah nominal tagihan item ini yang SUDAH
	// diterbitkan invoicenya. Piutang lahir dari dokumen, jadi angka inilah —
	// bukan Amount — yang menjadi dasar piutang. Selisih Amount − RecognizedAmount
	// adalah tagihan yang belum ditagihkan (mis. hasil raise K-5).
	RecognizedAmount    domain.Money `gorm:"type:DECIMAL(20,4);not null;default:'0.0000'" json:"recognized_amount"`
	RecognizedAt        *time.Time   `                                       json:"recognized_at,omitempty"`
	RecognizedDueDate   *time.Time   `gorm:"type:date"                       json:"recognized_due_date,omitempty"`
	RecognizedInvoiceID *uint64      `                                       json:"recognized_invoice_id,omitempty"`
	DueDate             *time.Time   `gorm:"type:date"                       json:"due_date,omitempty"`
	Status              ItemStatus   `gorm:"not null;size:20;default:'open'" json:"status"`
	CreatedBy           *uint64      `                                       json:"created_by,omitempty"`
	CreatedAt           time.Time    `                                       json:"created_at"`
	UpdatedAt           time.Time    `                                       json:"updated_at"`
}

func (ChargeItem) TableName() string { return "charge_items" }

// AdjustmentReason — alasan perubahan amount item (append-only audit).
type AdjustmentReason string

const (
	ReasonPayoutOverrun    AdjustmentReason = "payout_overrun"    // K-5: aktual > tagihan
	ReasonTrueupSettlement AdjustmentReason = "trueup_settlement" // K-4: true-up saat settle
)

// ChargeItemAdjustment adalah audit trail SETIAP perubahan ChargeItem.Amount.
type ChargeItemAdjustment struct {
	ID             uint64           `gorm:"primaryKey;autoIncrement" json:"id"`
	TenantID       uint64           `gorm:"not null;index"           json:"-"`
	ChargeItemID   uint64           `gorm:"not null;index"           json:"charge_item_id"`
	OldAmount      domain.Money     `gorm:"type:DECIMAL(20,4);not null;default:'0.0000'" json:"old_amount"`
	NewAmount      domain.Money     `gorm:"type:DECIMAL(20,4);not null;default:'0.0000'" json:"new_amount"`
	Reason         AdjustmentReason `gorm:"not null;size:30"         json:"reason"`
	ChargePayoutID *uint64          `                                json:"charge_payout_id,omitempty"`
	CreatedBy      *uint64          `                                json:"created_by,omitempty"`
	CreatedAt      time.Time        `                                json:"created_at"`
	UpdatedAt      time.Time        `                                json:"updated_at"`
}

func (ChargeItemAdjustment) TableName() string { return "charge_item_adjustments" }

// ── Pengakuan piutang realisasi (W-5 / J-14a) ────────────────────────────────

// AccountCodePiutangCustomer adalah akun piutang customer — SATU akun yang sama
// dengan piutang harga rumah (keputusan klien D-3: "jangan membuat mekanisme
// piutang kedua yang terpisah dari customer receivable utama").
const AccountCodePiutangCustomer = "1-2000"

// RecognitionReason menjelaskan MENGAPA kontribusi piutang sebuah item berubah.
// Bukan kosmetik: inilah yang membuat baris 1-2000 bisa dijelaskan tanpa harus
// merekonstruksi ulang seluruh riwayat grup.
type RecognitionReason string

const (
	ReasonRecInvoice       RecognitionReason = "invoice"        // R-5: invoice terbit
	ReasonRecBAST          RecognitionReason = "bast"           // D-3: invoice otomatis saat BAST
	ReasonRecPayment       RecognitionReason = "payment"        // pembayaran mengurangi piutang
	ReasonRecVoidPayment   RecognitionReason = "void_payment"   // pembatalan pembayaran memulihkannya
	ReasonRecTransferIn    RecognitionReason = "transfer_in"    // dana dari grup lain melunasi item
	ReasonRecCancelItem    RecognitionReason = "cancel_item"    // item dibatalkan
	ReasonRecCancelGroup   RecognitionReason = "cancel_group"   // grup dibatalkan
	ReasonRecTrueUp        RecognitionReason = "trueup"         // K-4: tagihan turun di bawah yang diakui
	ReasonRecCatchUp       RecognitionReason = "catch_up"       // R-3: jurnal susulan sekali jalan
	ReasonRecSaleCancelled RecognitionReason = "sale_cancelled" // penjualan dibatalkan pasca-BAST
	// W-13: pengakuan PENDAPATAN produk tambahan saat BAST. Dibedakan dari
	// ReasonRecBAST dengan sengaja — keduanya sama-sama lahir di BAST, tapi
	// yang satu memindahkan titipan menjadi piutang (tidak menyentuh laba rugi)
	// dan yang satu mengakui penjualan. Menyamakan namanya akan membuat baris
	// 1-2000 tidak lagi bisa dijelaskan hanya dari kolom reason.
	ReasonRecAddonBAST RecognitionReason = "addon_bast"
)

// ChargeReceivableRecognition adalah satu perubahan kontribusi piutang sebuah
// item — APPEND-ONLY. Σ delta per item SELALU sama dengan kontribusi berjalan
// item itu (INV-REC-1), dan setiap baris menunjuk jurnal yang mewujudkannya
// (INV-REC-2): tidak ada piutang yang bergerak tanpa jurnal.
type ChargeReceivableRecognition struct {
	ID             uint64            `gorm:"primaryKey;autoIncrement" json:"id"`
	TenantID       uint64            `gorm:"not null;index"           json:"-"`
	ChargeGroupID  uint64            `gorm:"not null;index"           json:"charge_group_id"`
	ChargeItemID   uint64            `gorm:"not null;index"           json:"charge_item_id"`
	UnitID         uint64            `gorm:"not null;index"           json:"unit_id"`
	Delta          domain.Money      `gorm:"type:DECIMAL(20,4);not null;default:'0.0000'" json:"delta"`
	Reason         RecognitionReason `gorm:"not null;size:30"         json:"reason"`
	DepositAccount string            `gorm:"column:deposit_account_code;not null;size:20"    json:"deposit_account_code"`
	ReceivableAcct string            `gorm:"column:receivable_account_code;not null;size:20" json:"receivable_account_code"`
	JournalEntryID *uint64           `                                json:"journal_entry_id,omitempty"`
	InvoiceID      *uint64           `                                json:"invoice_id,omitempty"`
	CreatedBy      *uint64           `                                json:"created_by,omitempty"`
	CreatedAt      time.Time         `                                json:"created_at"`
	UpdatedAt      time.Time         `                                json:"-"`
}

func (ChargeReceivableRecognition) TableName() string { return "charge_receivable_recognitions" }

// RecognitionStatus adalah keadaan pengakuan satu item untuk ditampilkan.
type RecognitionStatus string

const (
	RecognitionUnbilled   RecognitionStatus = "unbilled"   // belum di-invoice → belum piutang
	RecognitionReceivable RecognitionStatus = "receivable" // diakui, masih ada sisa
	RecognitionPaid       RecognitionStatus = "paid"       // diakui, sisanya nol
)

// ChargePayout adalah pembayaran ke vendor dari titipan (Dr 2-2400 / Cr Kas).
type ChargePayout struct {
	ID              uint64       `gorm:"primaryKey;autoIncrement" json:"id"`
	TenantID        uint64       `gorm:"not null;index"           json:"-"`
	ChargeGroupID   uint64       `gorm:"not null;index"           json:"charge_group_id"`
	ChargeItemID    uint64       `gorm:"not null;index"           json:"charge_item_id"`
	Amount          domain.Money `gorm:"type:DECIMAL(20,4);not null;default:'0.0000'" json:"amount"`
	Date            time.Time    `gorm:"not null"                 json:"date"`
	BankAccountCode string       `gorm:"not null;size:20"         json:"bank_account_code"`
	Vendor          string       `gorm:"not null;size:120"        json:"vendor"`
	Notes           string       `gorm:"size:500"                 json:"notes,omitempty"`
	JournalEntryID  uint64       `gorm:"not null"                 json:"journal_entry_id"`
	IdempotencyKey  *string      `gorm:"size:64"                  json:"-"`
	CreatedBy       *uint64      `                                json:"created_by,omitempty"`
	CreatedAt       time.Time    `                                json:"created_at"`
	UpdatedAt       time.Time    `                                json:"updated_at"`
}

func (ChargePayout) TableName() string { return "charge_payouts" }

// SettlementAction — disposisi dana titipan / koreksi (K-4 + void).
type SettlementAction string

const (
	ActionRefund        SettlementAction = "refund"
	ActionTransferHouse SettlementAction = "transfer_house"
	ActionTransferGroup SettlementAction = "transfer_group"
	ActionVoidPayment   SettlementAction = "void_payment"
)

// ChargeSettlement adalah satu disposisi dana grup (append-only).
type ChargeSettlement struct {
	ID              uint64           `gorm:"primaryKey;autoIncrement" json:"id"`
	TenantID        uint64           `gorm:"not null;index"           json:"-"`
	ChargeGroupID   uint64           `gorm:"not null;index"           json:"charge_group_id"`
	Action          SettlementAction `gorm:"not null;size:20"         json:"action"`
	Amount          domain.Money     `gorm:"type:DECIMAL(20,4);not null;default:'0.0000'" json:"amount"`
	Date            time.Time        `gorm:"not null"                 json:"date"`
	BankAccountCode *string          `gorm:"size:20"                  json:"bank_account_code,omitempty"`
	TargetGroupID   *uint64          `                                json:"target_group_id,omitempty"`
	TargetTerminID  *uint64          `                                json:"target_termin_id,omitempty"`
	VoidedTerminID  *uint64          `                                json:"voided_termin_id,omitempty"`
	// MemoNumber (T-1, keputusan klien 2026-08-05) — nomor MEMO TRANSFER INTERNAL
	// MTI/{yyyy}/{6 digit}. Diisi HANYA untuk transfer_house & transfer_group:
	// dana pindah tempat, bukan uang masuk baru, sehingga dokumennya memo — bukan
	// kwitansi. NULL untuk refund/void dan untuk transfer lama (pra-000060).
	MemoNumber     *string   `gorm:"size:30"                  json:"memo_number,omitempty"`
	JournalEntryID *uint64   `                                json:"journal_entry_id,omitempty"`
	Notes          string    `gorm:"size:500"                 json:"notes,omitempty"`
	IdempotencyKey *string   `gorm:"size:64"                  json:"-"`
	CreatedBy      *uint64   `                                json:"created_by,omitempty"`
	CreatedAt      time.Time `                                json:"created_at"`
	UpdatedAt      time.Time `                                json:"updated_at"`
}

func (ChargeSettlement) TableName() string { return "charge_settlements" }

// ChargeSettlementLine adalah rincian satu settlement per akun kewajiban —
// cermin persis baris jurnalnya. Satu aksi tetap SATU dokumen (satu memo, satu
// kunci idempotensi, satu void per termin); yang pecah hanya akunnya.
//
// Tanpa tabel ini residual per akun mustahil direkonstruksi: refund dan transfer
// bergerak di level grup dan tidak menunjuk item mana pun.
type ChargeSettlementLine struct {
	ID                 uint64       `gorm:"primaryKey;autoIncrement" json:"id"`
	TenantID           uint64       `gorm:"not null;index"           json:"-"`
	ChargeSettlementID uint64       `gorm:"not null;index"           json:"charge_settlement_id"`
	DepositAccountCode string       `gorm:"not null;size:20"         json:"deposit_account_code"`
	Amount             domain.Money `gorm:"type:DECIMAL(20,4);not null;default:'0.0000'" json:"amount"`
	CreatedAt          time.Time    `                                json:"created_at"`
	UpdatedAt          time.Time    `                                json:"updated_at"`
}

func (ChargeSettlementLine) TableName() string { return "charge_settlement_lines" }

// DepositAmount adalah pasangan akun kewajiban + nominal. Dipakai untuk residual
// per akun maupun untuk memecah satu aksi grup menjadi baris jurnal per akun.
// Selalu diurut berdasarkan kode akun agar hasilnya deterministik.
type DepositAmount struct {
	AccountCode string       `json:"account_code"`
	Amount      domain.Money `json:"amount"`
}

// ── Audit kebijakan tenant (R-A, keputusan klien 2026-08-05) ─────────────────

// PolicyRequireRealizationSettled adalah kunci audit untuk gate BAST K-2.
const PolicyRequireRealizationSettled = "require_realization_settled"

// TenantPolicyChange adalah satu perubahan konfigurasi finansial tenant.
// APPEND-ONLY: tidak pernah di-update, tidak pernah dihapus. Baris hanya lahir
// bila nilainya benar-benar berubah — set ulang ke nilai yang sama bukan event.
type TenantPolicyChange struct {
	ID        uint64    `gorm:"primaryKey;autoIncrement" json:"id"`
	TenantID  uint64    `gorm:"not null;index"           json:"-"`
	PolicyKey string    `gorm:"not null;size:64"         json:"policy_key"`
	OldValue  string    `gorm:"not null;size:64"         json:"old_value"`
	NewValue  string    `gorm:"not null;size:64"         json:"new_value"`
	ChangedBy *uint64   `                                json:"changed_by,omitempty"`
	Notes     string    `gorm:"size:500"                 json:"notes,omitempty"`
	CreatedAt time.Time `                                json:"created_at"`
	UpdatedAt time.Time `                                json:"-"`
}

func (TenantPolicyChange) TableName() string { return "tenant_policy_changes" }

// ── Memo Transfer Internal (T-1, keputusan klien 2026-08-05) ────────────────

// TransferMemo adalah dokumen transfer internal — pengganti kwitansi untuk
// perpindahan dana titipan. Bukan bukti terima uang: uangnya sudah diterima dan
// sudah ber-kwitansi KWR sebelumnya; dokumen ini menjelaskan ke mana dana itu
// dipindahkan, supaya audit trail tetap utuh tanpa bukti terima ganda.
type TransferMemo struct {
	SettlementID uint64           `json:"settlement_id"`
	MemoNumber   string           `json:"memo_number"`
	Action       SettlementAction `json:"action"` // transfer_house | transfer_group
	Date         time.Time        `json:"date"`
	Amount       domain.Money     `json:"amount"`

	// Sumber dana.
	SourceGroupID    uint64 `json:"source_group_id"`
	SourceGroupLabel string `json:"source_group_label"`
	SourceUnitCode   string `json:"source_unit_code"`
	SaleContractID   uint64 `json:"sale_contract_id"`
	BuyerName        string `json:"buyer_name"`

	// Tujuan — salah satu terisi sesuai action.
	TargetGroupID    *uint64 `json:"target_group_id,omitempty"`
	TargetGroupLabel string  `json:"target_group_label,omitempty"`
	TargetUnitCode   string  `json:"target_unit_code,omitempty"`
	TargetTerminID   *uint64 `json:"target_termin_id,omitempty"`
	TargetLabel      string  `json:"target_label"` // teks siap cetak

	JournalEntryID *uint64 `json:"journal_entry_id,omitempty"`
	Notes          string  `json:"notes,omitempty"`
	CreatedBy      *uint64 `json:"created_by,omitempty"`
	CompanyName    string  `json:"company_name,omitempty"`
}

// ── Ringkasan kanonik ────────────────────────────────────────────────────────

// ItemSummary adalah potret satu item + angka turunannya.
type ItemSummary struct {
	ItemID         uint64       `json:"item_id"`
	Label          string       `json:"label"`
	ChargeTypeCode string       `json:"charge_type_code,omitempty"` // W-1; kosong = baris historis
	ProductCode    string       `json:"product_code,omitempty"`     // W-13; produk katalog item addon
	DepositAccount string       `json:"deposit_account_code"`       // akun kewajiban item ini
	Status         ItemStatus   `json:"status"`
	Amount         domain.Money `json:"amount"`          // tagihan berjalan
	OriginalAmount domain.Money `json:"original_amount"` // tagihan awal
	Paid           domain.Money `json:"paid"`            // Σ alokasi (net void)
	Outstanding    domain.Money `json:"outstanding"`     // amount − paid
	Payout         domain.Money `json:"payout"`          // Σ payout vendor
	DueDate        *time.Time   `json:"due_date,omitempty"`

	// W-5 — pengakuan piutang (R-5: lahir saat invoice terbit).
	RecognizedAmount  domain.Money      `json:"recognized_amount"` // nominal yang sudah di-invoice
	RecognizedAt      *time.Time        `json:"recognized_at,omitempty"`
	Receivable        domain.Money      `json:"receivable"`         // max(0, recognized − paid) → kontribusi 1-2000
	Unbilled          domain.Money      `json:"unbilled"`           // amount − recognized (belum ditagihkan)
	RecognitionStatus RecognitionStatus `json:"recognition_status"` // unbilled | receivable | paid
}

// GroupSummary adalah SATU-SATUNYA formula angka grup (registry-worthy):
//
//	billed      = Σ item.amount        (item open)
//	paid        = Σ alokasi charge_item + charge_item_void (net)
//	outstanding = billed − paid
//	payout      = Σ payout vendor
//	returned    = Σ settlement refund + transfer_house + transfer_group
//	residual    = paid − payout − returned  (dana titipan yang masih dipegang)
//
// DepositResiduals memecah residual yang sama menurut AKUN KEWAJIBAN — rumus
// identik, hanya dikelompokkan per akun snapshot item. Σ DepositResiduals ==
// Residual, selalu. Inilah yang membuat satu grup boleh memuat beberapa jenis
// biaya dengan akun berbeda tanpa saldo kewajibannya menjadi ambigu.
type GroupSummary struct {
	GroupID          uint64          `json:"group_id"`
	SaleContractID   uint64          `json:"sale_contract_id"`
	UnitID           uint64          `json:"unit_id"`
	Kind             GroupKind       `json:"kind"`
	Label            string          `json:"label"`
	Status           GroupStatus     `json:"status"`
	Billed           domain.Money    `json:"billed"`
	Paid             domain.Money    `json:"paid"`
	Outstanding      domain.Money    `json:"outstanding"`
	Payout           domain.Money    `json:"payout"`
	Returned         domain.Money    `json:"returned"`
	Residual         domain.Money    `json:"residual"`
	DepositResiduals []DepositAmount `json:"deposit_residuals"`

	// W-5 — sisi piutang. `Receivable` adalah kontribusi grup ini ke akun 1-2000
	// (Σ per item); `Unbilled` adalah tagihan yang belum pernah di-invoice dan
	// karena itu BELUM menjadi piutang (R-5). Keduanya menjumlah menjadi
	// Outstanding, kecuali bila ada item lebih bayar — kelebihannya tidak pernah
	// menjadi piutang bersaldo kredit (R-4), melainkan residual.
	Recognized bool         `json:"recognized"` // grup sudah pernah di-invoice
	Receivable domain.Money `json:"receivable"`
	Unbilled   domain.Money `json:"unbilled"`
	// Nomor invoice TERAKHIR yang mengakui piutang grup ini — dokumen yang
	// menjelaskan dari mana angka Receivable berasal. Kosong bila belum pernah
	// ditagihkan.
	InvoiceNumber string `json:"invoice_number,omitempty"`

	Items []ItemSummary `json:"items"`
}

// PaymentRow adalah satu pembayaran grup untuk tampilan (dari termin_payments).
type PaymentRow struct {
	TerminID        uint64       `json:"termin_id"`
	Amount          domain.Money `json:"amount"`
	Date            time.Time    `json:"date"`
	BankAccountCode string       `json:"bank_account_code"`
	Source          string       `json:"payment_source"`
	Description     string       `json:"description"`
	ReceiptNumber   string       `json:"receipt_number,omitempty"`
	ReceiptID       uint64       `json:"receipt_id,omitempty"`
	Voided          bool         `json:"voided"`
}

// GroupDetail = ringkasan + histori lengkap untuk halaman detail.
type GroupDetail struct {
	Summary     GroupSummary            `json:"summary"`
	Payments    []PaymentRow            `json:"payments"`
	Payouts     []*ChargePayout         `json:"payouts"`
	Settlements []*ChargeSettlement     `json:"settlements"`
	Adjustments []*ChargeItemAdjustment `json:"adjustments"`
}

// ── Requests ─────────────────────────────────────────────────────────────────

type NewItemInput struct {
	Label string `json:"label"`
	// ChargeTypeCode (W-1) — wajib untuk grup realization; diabaikan untuk addon
	// (addon bukan titipan). Kosong pada grup realization = ditolak.
	ChargeTypeCode string `json:"charge_type_code,omitempty"`
	// ProductCode (W-13) — wajib untuk grup addon; diabaikan untuk realization.
	// Kosong pada grup addon = ditolak, dengan alasan yang sama seperti W-1:
	// masterlah yang menentukan ke akun mana uangnya mendarat, bukan pengetik.
	ProductCode string       `json:"product_code,omitempty"`
	Amount      domain.Money `json:"amount"`
	DueDate     *time.Time   `json:"due_date,omitempty"`
}

type CreateGroupRequest struct {
	SaleContractID uint64
	Kind           GroupKind
	Label          string
	Notes          string
	Items          []NewItemInput
	CreatedBy      *uint64
}

// AllocationInput — alokasi MANUAL admin (K-3). Tidak ada auto-allocation.
type AllocationInput struct {
	ChargeItemID uint64       `json:"charge_item_id"`
	Amount       domain.Money `json:"amount"`
}

type ReceivePaymentRequest struct {
	GroupID         uint64
	Amount          domain.Money
	Date            time.Time
	BankAccountCode string
	Allocations     []AllocationInput
	Reference       string
	Notes           string
	CreatedBy       *uint64
	IdempotencyKey  string
}

type ReceivePaymentResult struct {
	TerminID       uint64       `json:"termin_id"`
	ReceiptNumber  string       `json:"receipt_number,omitempty"`
	ReceiptID      uint64       `json:"receipt_id,omitempty"`
	AlreadyExisted bool         `json:"already_existed"`
	Summary        GroupSummary `json:"summary"`
}

type PayoutRequest struct {
	ChargeItemID    uint64
	Amount          domain.Money
	Date            time.Time
	BankAccountCode string
	Vendor          string
	Notes           string
	CreatedBy       *uint64
	IdempotencyKey  string
}

type RefundRequest struct {
	GroupID         uint64
	Amount          domain.Money
	Date            time.Time
	BankAccountCode string
	Notes           string
	CreatedBy       *uint64
	IdempotencyKey  string
}

type TransferHouseRequest struct {
	GroupID        uint64
	Amount         domain.Money
	Date           time.Time
	Notes          string
	CreatedBy      *uint64
	IdempotencyKey string
}

type TransferGroupRequest struct {
	GroupID        uint64
	TargetGroupID  uint64
	Amount         domain.Money
	Date           time.Time
	Allocations    []AllocationInput // alokasi manual ke item grup TUJUAN (K-3)
	Notes          string
	CreatedBy      *uint64
	IdempotencyKey string
}

type VoidPaymentRequest struct {
	TerminID  uint64
	Reason    string
	CreatedBy *uint64
}

// SettleResult — hasil true-up + percobaan settle (K-4).
type SettleResult struct {
	Settled bool         `json:"settled"`
	Summary GroupSummary `json:"summary"`
}

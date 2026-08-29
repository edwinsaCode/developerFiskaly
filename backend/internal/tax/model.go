package tax

import (
	"time"

	"github.com/shopspring/decimal"

	"esaproperti/internal/domain"
)

// RateCodePPhFinalPengalihan adalah kode tarif PPh Final Pengalihan Hak atas
// Tanah dan/atau Bangunan berdasarkan PP 34/2016.
const RateCodePPhFinalPengalihan = "pph_final_pengalihan"

// RateCodePPNKeluaran adalah kode tarif PPN yang dipungut atas penjualan unit properti.
// Tarif efektif diambil dari tabel tax_rates (TIDAK hardcode di kode posting).
// // TODO(tax-advisor): konfirmasi tarif berlaku untuk villa LITHOS (12% penuh vs 11% efektif).
const RateCodePPNKeluaran = "ppn_keluaran"

// ── Tax Rule (Increment 4 — konfiguratif penuh, blueprint §7) ─────────────────

// Akun default legacy untuk baris rule lama yang belum menyetel akun (kolom
// kosong). SATU-SATUNYA tempat default ini didefinisikan — kode posting selalu
// membaca dari rule, bukan konstanta.
const (
	DefaultPPhExpenseAccount = "5-2000" // Beban PPh Final Pengalihan
	DefaultPPhPayableAccount = "2-4000" // Hutang PPh Final Pengalihan
)

// TriggerEvent adalah vocabulary event pemicu pajak (typed enum — hardening
// pasca-review). Saat ini satu-satunya trigger terimplementasi = bast; invoice
// & payment adalah seam blueprint §7 (BPHTB/AJB, pemotongan saat pembayaran).
type TriggerEvent string

const (
	TriggerBAST    TriggerEvent = "bast"
	TriggerInvoice TriggerEvent = "invoice"
	TriggerPayment TriggerEvent = "payment"
)

func (t TriggerEvent) Valid() bool {
	return t == TriggerBAST || t == TriggerInvoice || t == TriggerPayment
}

// RuleAppliesTo menentukan proyek mana yang cocok dengan sebuah rule:
// kategori spesifik (subsidi/komersial) MENANG atas 'all' (fallback).
type RuleAppliesTo string

const (
	AppliesToAll       RuleAppliesTo = "all"
	AppliesToSubsidi   RuleAppliesTo = RuleAppliesTo(domain.TaxCategorySubsidi)
	AppliesToKomersial RuleAppliesTo = RuleAppliesTo(domain.TaxCategoryKomersial)
)

func (a RuleAppliesTo) Valid() bool {
	return a == AppliesToAll || a == AppliesToSubsidi || a == AppliesToKomersial
}

// TaxRate adalah TAX RULE konfiguratif (blueprint erp-blueprint-review.md §7):
// tarif + cakupan (applies_to ↔ projects.tax_category) + akun jurnal + masa
// berlaku. Regulasi berubah (tarif baru, kategori baru) = TAMBAH baris rule
// dengan effective_from baru — TANPA ubah kode. Snapshot tarif di obligation
// menjaga histori (perubahan rule tidak menyentuh akrual lama).
//
// Nama tipe & tabel dipertahankan `TaxRate`/`tax_rates` (additive, IMPL-2).
//
// // TODO(tax-advisor): konfirmasi bahwa basis pengenaan adalah nilai pengalihan bruto
// (bukan DPP PPN), dan bahwa pajak ini bersifat final (tidak dapat dikreditkan).
// Konfirmasi juga perlakuan untuk struktur HGB-80/leasehold ke pembeli asing (LITHOS).
type TaxRate struct {
	ID       uint64 `gorm:"primaryKey;autoIncrement" json:"id"`
	TenantID uint64 `gorm:"not null;index"           json:"-"`
	RateCode string `gorm:"not null;size:50"         json:"rate_code"`
	Name     string `gorm:"size:200"                 json:"name"`
	Rate     decimal.Decimal `gorm:"type:DECIMAL(10,6);not null" json:"rate"` // e.g. 0.025000
	// AppliesTo: all|subsidi|komersial — dicocokkan dgn projects.tax_category.
	AppliesTo RuleAppliesTo `gorm:"column:applies_to;size:20;default:'all'" json:"applies_to"`
	// TriggerEvent (typed) & CalcBase: seam blueprint §7. Trigger terimplementasi = bast.
	TriggerEvent TriggerEvent `gorm:"size:20;default:'bast'"            json:"trigger_event"`
	CalcBase     string       `gorm:"size:30;default:'transfer_value'"  json:"calc_base"`
	// Formula: seam perhitungan (TaxFormula). Kosong/'proportional' = rate × base.
	Formula FormulaType `gorm:"size:30;default:'proportional'" json:"formula"`
	// Revision: revisi KONFIGURASI baris ini (naik pada setiap perubahan baris);
	// dibekukan ke obligation bersama rule id — audit eksplisit.
	Revision int `gorm:"not null;default:1" json:"revision"`
	// Akun jurnal akrual — KONFIGURASI, bukan hardcode. Kosong = default legacy.
	DebitAccount  string     `gorm:"size:20" json:"debit_account"`
	CreditAccount string     `gorm:"size:20" json:"credit_account"`
	EffectiveFrom time.Time  `gorm:"not null" json:"effective_from"`
	EffectiveTo   *time.Time `json:"effective_to,omitempty"` // NULL = terbuka
	IsActive      bool       `gorm:"not null;default:true" json:"is_active"`
	Description   string     `gorm:"size:500" json:"description"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

func (TaxRate) TableName() string { return "tax_rates" }

// DebitAccountOrDefault / CreditAccountOrDefault: akun dari konfigurasi rule;
// baris legacy (kosong) memakai default terpusat di atas.
func (r *TaxRate) DebitAccountOrDefault() string {
	if r.DebitAccount != "" {
		return r.DebitAccount
	}
	return DefaultPPhExpenseAccount
}

func (r *TaxRate) CreditAccountOrDefault() string {
	if r.CreditAccount != "" {
		return r.CreditAccount
	}
	return DefaultPPhPayableAccount
}

// ── TaxObligationStatus ───────────────────────────────────────────────────────

type TaxObligationStatus string

const (
	ObligationStatusOutstanding TaxObligationStatus = "outstanding"
	ObligationStatusPaid        TaxObligationStatus = "paid"
	// ObligationStatusCancelled (Increment 8): akrual dibalik karena pembatalan
	// penjualan — bukan lagi kewajiban outstanding maupun paid.
	ObligationStatusCancelled TaxObligationStatus = "cancelled"
)

// ── TaxObligation (Event 5a) ──────────────────────────────────────────────────

// TaxObligation mencatat akrual kewajiban PPh Final (Event 5a).
// Jurnal: Dr 5-2000 Beban PPh Final / Cr 2-4000 Hutang PPh Final.
// TaxAmount = tarif × nilai pengalihan, dibulatkan ke rupiah bulat (Invariant #2).
// Rate di-snapshot pada saat akrual — perubahan tarif di masa depan tidak mengubah angka ini.
type TaxObligation struct {
	ID              uint64              `gorm:"primaryKey;autoIncrement"                        json:"id"`
	TenantID        uint64              `gorm:"not null;index"                                  json:"-"`
	UnitID          *uint64             `gorm:"index"                                           json:"unit_id,omitempty"`
	ProjectID       *uint64             `gorm:"index"                                           json:"project_id,omitempty"`
	RateCode        string              `gorm:"not null;size:50"                                json:"rate_code"`
	TransferValue   domain.Money        `gorm:"type:DECIMAL(20,4);not null;default:'0.0000'"   json:"transfer_value"` // nilai pengalihan bruto
	Rate            decimal.Decimal     `gorm:"type:DECIMAL(10,6);not null"                     json:"rate"`           // snapshot tarif saat akrual
	TaxAmount       domain.Money        `gorm:"type:DECIMAL(20,4);not null;default:'0.0000'"   json:"tax_amount"`     // = round(transferValue × rate)
	Status          TaxObligationStatus `gorm:"not null;size:20;default:'outstanding'"          json:"status"`
	AccrualDate     time.Time           `gorm:"not null"                                        json:"accrual_date"`
	JournalEntryID  uint64              `gorm:"not null;index"                                  json:"journal_entry_id"`
	// Provenance rule (Increment 4 + hardening): rule mana + REVISI konfigurasi
	// berapa yang dipakai + snapshot cakupannya. NULL/'' = akrual legacy.
	TaxRuleID       *uint64 `json:"tax_rule_id,omitempty"`
	TaxRuleRevision *int    `json:"tax_rule_revision,omitempty"`
	AppliesTo       string  `gorm:"column:applies_to;size:20;default:''" json:"applies_to,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (TaxObligation) TableName() string { return "tax_obligations" }

// ── TaxPayment (Event 5b) ─────────────────────────────────────────────────────

// TaxPayment mencatat pelunasan kewajiban PPh Final ke kas negara (Event 5b).
// Jurnal: Dr 2-4000 Hutang PPh Final / Cr 1-13xx Bank.
type TaxPayment struct {
	ID              uint64       `gorm:"primaryKey;autoIncrement"                        json:"id"`
	TenantID        uint64       `gorm:"not null;index"                                  json:"-"`
	ObligationID    uint64       `gorm:"not null;index"                                  json:"obligation_id"`
	BankAccountCode string       `gorm:"not null;size:20"                                json:"bank_account_code"`
	Amount          domain.Money `gorm:"type:DECIMAL(20,4);not null;default:'0.0000'"   json:"amount"`
	PaymentDate     time.Time    `gorm:"not null"                                        json:"payment_date"`
	JournalEntryID  uint64       `gorm:"not null;index"                                  json:"journal_entry_id"`
	CreatedAt       time.Time    `                                                       json:"created_at"`
	UpdatedAt       time.Time    `                                                       json:"updated_at"`
}

func (TaxPayment) TableName() string { return "tax_payments" }

// ── Request / response types ──────────────────────────────────────────────────

// JournalLineInput mirrors ledger.LineInput tanpa import ledger package.
type JournalLineInput struct {
	AccountID   uint64
	Debit       domain.Money
	Credit      domain.Money
	ProjectID   *uint64
	UnitID      *uint64
	Description string
}

// AccrueTaxRequest adalah input untuk Event 5a (akrual kewajiban PPh Final).
type AccrueTaxRequest struct {
	RateCode      string       // biasanya RateCodePPhFinalPengalihan
	TransferValue domain.Money // nilai pengalihan bruto (bukan DPP PPN — lihat TODO(tax-advisor))
	AccrualDate   time.Time    // tanggal pengalihan / BAST
	UnitID        *uint64
	ProjectID     *uint64
}

// PayTaxRequest adalah input untuk Event 5b (setor ke kas negara).
type PayTaxRequest struct {
	ObligationID    uint64
	BankAccountCode string // 1-1300 | 1-1400 | 1-1500
	PaymentDate     time.Time
}

// SetTaxRateRequest adalah input untuk konfigurasi rule baru (Increment 4:
// field cakupan + akun + masa berlaku).
type SetTaxRateRequest struct {
	RateCode      string
	Name          string
	Rate          decimal.Decimal
	AppliesTo     RuleAppliesTo // kosong = all
	TriggerEvent  TriggerEvent  // kosong = bast (typed)
	CalcBase      string        // kosong = transfer_value
	Formula       FormulaType   // kosong = proportional
	DebitAccount  string        // kosong = default legacy
	CreditAccount string
	EffectiveFrom time.Time
	EffectiveTo   *time.Time
	Description   string
}

// TaxReportItem adalah satu baris dalam laporan kewajiban PPh Final.
type TaxReportItem struct {
	ObligationID  uint64              `json:"obligation_id"`
	UnitID        *uint64             `json:"unit_id,omitempty"`
	RateCode      string              `json:"rate_code"` // S7: dipakai reporting (satu pembaca)
	TransferValue string              `json:"transfer_value"`
	Rate          string              `json:"rate"`
	TaxAmount     string              `json:"tax_amount"`
	Status        TaxObligationStatus `json:"status"`
	AccrualDate   time.Time           `json:"accrual_date"`
}

// TaxReport adalah laporan kewajiban pajak per periode.
type TaxReport struct {
	PeriodFrom        time.Time       `json:"period_from"`
	PeriodTo          time.Time       `json:"period_to"`
	Items             []TaxReportItem `json:"items"`
	TotalObligation   string          `json:"total_obligation"`
	TotalPaid         string          `json:"total_paid"`
	TotalOutstanding  string          `json:"total_outstanding"`
}

// ── Phase 8: PPN report & penjaga ────────────────────────────────────────────

// VATReport adalah laporan PPN per periode.
// PPNKeluaran diambil dari Σ kredit akun 2-3000 di ledger (rekonsiliasi nyata).
// PPNMasukan diambil dari Σ debit akun 1-5100 di ledger.
// PPNTerutang = PPNKeluaran − PPNMasukan.
//
// // TODO(tax-advisor): hunian mewah LITHOS — konfirmasi apakah 12% penuh atau 11% efektif
// sebelum mengandalkan angka ini untuk SPT Masa PPN.
type VATReport struct {
	PeriodFrom  time.Time `json:"period_from"`
	PeriodTo    time.Time `json:"period_to"`
	PPNKeluaran string    `json:"ppn_keluaran"` // Σ Cr 2-3000 dalam periode
	PPNMasukan  string    `json:"ppn_masukan"`  // Σ Dr 1-5100 dalam periode
	PPNTerutang string    `json:"ppn_terutang"` // keluaran − masukan ≥ 0
}

// CombinedTaxReport adalah laporan kewajiban pajak gabungan (PPh Final + PPN) per periode.
type CombinedTaxReport struct {
	PeriodFrom time.Time  `json:"period_from"`
	PeriodTo   time.Time  `json:"period_to"`
	PPHFinal   *TaxReport `json:"pph_final"`
	PPN        *VATReport `json:"ppn"`
}

// BASTWithoutPPhFinalItem adalah unit yang sudah BAST dalam periode
// tetapi BELUM ada akrual PPh Final (penjaga audit trail).
type BASTWithoutPPhFinalItem struct {
	UnitID uint64 `json:"unit_id"`
}

// W-3.0 (B-1): daftar kode bank hardcoded {1-1300,1-1400,1-1500} DIHAPUS.
// Keanggotaan kas/bank ditentukan COA (accounts.category) lewat otoritas tunggal
// ledger.ValidatePaymentAccount — sama seperti sale, charge, dan notary.
// Lihat PaymentAccountResolver di service.go.

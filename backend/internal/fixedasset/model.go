// Package fixedasset — Fixed Asset Register + Penyusutan (straight-line, v1).
//
// Terintegrasi ke pintu masuk Transaksi Pengeluaran yang sudah ada
// (internal/cost, W-10): satu form, satu toggle "Jenis Pembelian" (Expense |
// Fixed Asset). Paket ini BUKAN mesin posting kedua — jurnal perolehan dan
// jurnal penyusutan sama-sama lewat ledger.PostingService, dibungkus adapter
// package-local (LedgerJournalAdapter) mengikuti pola cost/sale/land/charge.
//
// Kategori aset (Category) memetakan satu kategori → tiga akun (aset,
// akumulasi penyusutan, beban penyusutan) — pola sama dengan W-10
// expense_types dan W-1 realization_charge_types: fail-closed, mapping akun
// divalidasi SEBELUM disimpan.
//
// accumulated_depreciation dan book_value SENGAJA tidak disimpan sebagai
// kolom — keduanya diturunkan dari SUM(fixed_asset_depreciation_lines.amount)
// setiap dibaca, sama seperti accumulated cost proyek/unit di internal/cost
// yang selalu dihitung dari jurnal, bukan dari kolom yang bisa basi.
package fixedasset

import (
	"time"

	"esaproperti/internal/domain"
)

// DepreciationMethod — v1 hanya straight_line. Field ini disimpan (bukan
// konstanta implisit) supaya v2 (declining balance dll., PMK 96/2009 fiskal)
// tinggal menambah kasus tanpa migrasi data.
type DepreciationMethod string

const DepreciationMethodStraightLine DepreciationMethod = "straight_line"

func (m DepreciationMethod) Valid() bool {
	return m == DepreciationMethodStraightLine
}

// AssetStatus — lifecycle register. "disposed" hanya groundwork field di v1:
// belum ada endpoint/service yang menuliskannya (lihat PRIORITAS 2 catatan
// disposal); disiapkan supaya migrasi data tidak diperlukan lagi saat v2.
type AssetStatus string

const (
	AssetStatusActive   AssetStatus = "active"
	AssetStatusDisposed AssetStatus = "disposed"
)

// PaymentMethod — v1 hanya bank (kas/bank langsung). Payable ditolak dengan
// alasan yang SAMA dengan W-10 cost.PaymentMethodPayable (T-9: hutang usaha
// belum punya jalur pelunasan dari pintu pengeluaran ini) — kebijakan yang
// sudah ada, bukan keputusan baru yang perlu ditanyakan ulang.
type PaymentMethod string

const (
	PaymentMethodBank    PaymentMethod = "bank"
	PaymentMethodPayable PaymentMethod = "payable"
)

func (p PaymentMethod) Valid() bool {
	return p == PaymentMethodBank || p == PaymentMethodPayable
}

// Category adalah master kategori aset tetap tenant (migrasi 000086).
type Category struct {
	ID                                 uint64    `gorm:"primaryKey;autoIncrement" json:"id"`
	TenantID                           uint64    `gorm:"not null;index"           json:"-"`
	Code                               string    `gorm:"not null;size:50"         json:"code"`
	Name                               string    `gorm:"not null;size:200"        json:"name"`
	AssetAccountCode                   string    `gorm:"not null;size:20"         json:"asset_account_code"`
	AccumulatedDepreciationAccountCode string    `gorm:"not null;size:20"         json:"accumulated_depreciation_account_code"`
	DepreciationExpenseAccountCode     string    `gorm:"not null;size:20"         json:"depreciation_expense_account_code"`
	DefaultUsefulLifeMonths            uint      `gorm:"not null;default:0"       json:"default_useful_life_months"`
	IsActive                           bool      `gorm:"not null;default:true"    json:"is_active"`
	CreatedAt                          time.Time `                                json:"created_at"`
	UpdatedAt                          time.Time `                                json:"updated_at"`
}

func (Category) TableName() string { return "fixed_asset_categories" }

// FixedAsset adalah satu baris Register (migrasi 000087).
//
// Invariant: acquisition_cost & residual_value rupiah bulat (Invariant #2);
// residual_value <= acquisition_cost; useful_life_months >= 1.
type FixedAsset struct {
	ID                    uint64             `gorm:"primaryKey;autoIncrement" json:"id"`
	TenantID              uint64             `gorm:"not null;index"           json:"-"`
	ProjectID             *uint64            `gorm:"index"                    json:"project_id,omitempty"`
	CategoryID            uint64             `gorm:"not null;index"           json:"category_id"`
	AssetCode             string             `gorm:"not null;size:50"         json:"asset_code"`
	AssetName             string             `gorm:"not null;size:200"        json:"asset_name"`
	AcquisitionDate       time.Time          `gorm:"type:date;not null"       json:"acquisition_date"`
	AcquisitionCost       domain.Money       `gorm:"type:DECIMAL(20,4);not null;default:'0.0000'" json:"acquisition_cost"`
	ResidualValue         domain.Money       `gorm:"type:DECIMAL(20,4);not null;default:'0.0000'" json:"residual_value"`
	UsefulLifeMonths      uint               `gorm:"not null"                 json:"useful_life_months"`
	DepreciationMethod    DepreciationMethod `gorm:"not null;size:20"         json:"depreciation_method"`
	DepreciationStartDate time.Time          `gorm:"type:date;not null"       json:"depreciation_start_date"`
	Status                AssetStatus        `gorm:"not null;size:20;default:'active'" json:"status"`
	DisposedAt            *time.Time         `                                json:"disposed_at,omitempty"`
	AcquisitionJournalID  uint64             `gorm:"not null"                 json:"acquisition_journal_id"`
	Vendor                string             `gorm:"size:200"                 json:"vendor,omitempty"`
	Description           string             `gorm:"size:500"                 json:"description,omitempty"`
	CreatedAt             time.Time          `                                json:"created_at"`
	UpdatedAt             time.Time          `                                json:"updated_at"`
}

func (FixedAsset) TableName() string { return "fixed_assets" }

// DepreciableAmount = AcquisitionCost − ResidualValue.
func (a *FixedAsset) DepreciableAmount() domain.Money {
	return a.AcquisitionCost.Sub(a.ResidualValue)
}

// DepreciationLine adalah satu baris penyusutan TERPOSTING untuk satu aset
// pada satu periode (migrasi 000087). Append-only: idempotensi ditegakkan
// oleh UNIQUE(tenant_id, fixed_asset_id, period_year, period_month).
type DepreciationLine struct {
	ID             uint64       `gorm:"primaryKey;autoIncrement" json:"id"`
	TenantID       uint64       `gorm:"not null;index"           json:"-"`
	FixedAssetID   uint64       `gorm:"not null;index"           json:"fixed_asset_id"`
	PeriodYear     uint16       `gorm:"not null"                 json:"period_year"`
	PeriodMonth    uint8        `gorm:"not null"                 json:"period_month"`
	Amount         domain.Money `gorm:"type:DECIMAL(20,4);not null" json:"amount"`
	JournalEntryID uint64       `gorm:"not null"                 json:"journal_entry_id"`
	CreatedAt      time.Time    `                                json:"created_at"`
	UpdatedAt      time.Time    `                                json:"updated_at"`
}

func (DepreciationLine) TableName() string { return "fixed_asset_depreciation_lines" }

// JournalLineInput — mirrors ledger.LineInput without importing ledger types
// directly (pola sama dengan cost.JournalLineInput).
type JournalLineInput struct {
	AccountID   uint64
	Debit       domain.Money
	Credit      domain.Money
	ProjectID   *uint64
	Description string
}

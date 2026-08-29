package sale

import (
	"context"
	"time"

	"github.com/shopspring/decimal"

	"esaproperti/internal/domain"
)

// ── Tipe enumerasi ────────────────────────────────────────────────────────────

type PaymentType string

const (
	PaymentTypeKPR   PaymentType = "kpr"
	PaymentTypeTunai PaymentType = "tunai"
)

type ScheduleType string

const (
	ScheduleTypeDP          ScheduleType = "dp"
	ScheduleTypeInstallment ScheduleType = "installment"
	ScheduleTypeFinal       ScheduleType = "final"
)

type ScheduleStatus string

const (
	ScheduleStatusScheduled ScheduleStatus = "scheduled"
	ScheduleStatusReceived  ScheduleStatus = "received"
	ScheduleStatusOverdue   ScheduleStatus = "overdue"
	// ScheduleStatusSuperseded: baris digantikan versi jadwal baru
	// (reschedule/konversi scheme — BS-5). Tidak pernah dihapus (audit).
	ScheduleStatusSuperseded ScheduleStatus = "superseded"
)

// RateCodePPNKeluaran adalah kode tarif PPN keluaran.
// Mirror dari tax.RateCodePPNKeluaran — tidak import tax untuk menghindari coupling.
const RateCodePPNKeluaran = "ppn_keluaran"

// ── Model: SaleContract ───────────────────────────────────────────────────────

// SaleContract mencatat kontrak jual beli antara developer dan buyer.
// Tenant-scoped (Invariant #6). Satu unit boleh punya satu kontrak aktif.
//
// Untuk PKP: DPPAmount = harga dasar; GrossAmount = DPP × (1 + VATRateSnapshot).
// Tagihan ke buyer selalu berdasarkan GrossAmount (termasuk PPN).
// TotalPrice adalah alias GrossAmount untuk backward-compat.
// Jurnal BAST TIDAK berubah: Cr Pendapatan = DPP; Cr PPN Keluaran = PPN (terpisah).
type SaleContract struct {
	ID           uint64        `gorm:"primaryKey;autoIncrement"                        json:"id"`
	TenantID     uint64        `gorm:"not null;index"                                  json:"-"`
	UnitID       uint64        `gorm:"not null;index"                                  json:"unit_id"`
	BuyerName    string        `gorm:"not null;size:200"                               json:"buyer_name"`
	BuyerID      string        `gorm:"not null;size:100"                               json:"buyer_id"` // NIK/KTP
	PaymentType  PaymentType   `gorm:"not null;size:20"                                json:"payment_type"`
	BankKPR      *string       `gorm:"size:100"                                        json:"bank_kpr,omitempty"`
	LoanAmount   *domain.Money `gorm:"type:DECIMAL(20,4)"                              json:"loan_amount,omitempty"`
	ContractDate time.Time     `gorm:"not null"                                        json:"contract_date"`
	DPPAmount    domain.Money  `gorm:"type:DECIMAL(20,4);not null;default:'0.0000'"    json:"dpp_amount"`
	// UnitPriceSnapshot: harga list unit SAAT kontrak dibuat (000049). Sumber
	// derivasi diskon (= snapshot − DPP). NULL untuk kontrak lama → diskon 0.
	// TIDAK pernah diubah setelah kontrak dibuat (append-only, snapshot beku).
	UnitPriceSnapshot *domain.Money   `gorm:"type:DECIMAL(20,4)" json:"unit_price_snapshot,omitempty"`
	IsPKP             bool            `gorm:"not null;default:false"                          json:"is_pkp"`
	VATRateSnapshot   decimal.Decimal `gorm:"type:DECIMAL(10,6);not null;default:'0.000000'"  json:"vat_rate_snapshot"`
	GrossAmount       domain.Money    `gorm:"type:DECIMAL(20,4);not null;default:'0.0000'"    json:"gross_amount"`
	// TotalPrice adalah alias GrossAmount — dipertahankan untuk backward-compat.
	// Selalu sama dengan GrossAmount; jadwal cicilan dijumlah ke nilai ini.
	TotalPrice domain.Money `gorm:"type:DECIMAL(20,4);not null;default:'0.0000'"    json:"total_price"`
	// Master refs (Increment 1 → ditegakkan Increment 3): kontrak BARU via alur
	// scheme WAJIB customer_id + sales_person_id. Baris lama nullable (legacy).
	CustomerID    *uint64 `gorm:"index" json:"customer_id,omitempty"`
	SalesPersonID *uint64 `gorm:"index" json:"sales_person_id,omitempty"`
	// AdminMarketingPersonID (P1 — Sales ≠ Admin Marketing): logical ref
	// sales_persons.id, penanggung jawab administrasi kontrak/dokumen/KPR/
	// follow-up. Independen dari SalesPersonID — internal/commission TIDAK
	// PERNAH membaca kolom ini, komisi selalu ke SalesPersonID.
	AdminMarketingPersonID *uint64 `gorm:"index" json:"admin_marketing_person_id,omitempty"`
	// ── Payment Scheme seam (Increment 3) ────────────────────────────────────
	// PaymentSchemeID: reference ke master; NULL = kontrak legacy (payment_type).
	PaymentSchemeID *uint64 `gorm:"index" json:"payment_scheme_id,omitempty"`
	// FinancingSourceID: bank penyalur KPR (menggantikan bank_kpr free-text).
	FinancingSourceID *uint64 `gorm:"index" json:"financing_source_id,omitempty"`
	// SchemeState: state machine scheme; NULL = legacy (tanpa state/gate).
	SchemeState *string `gorm:"size:30" json:"scheme_state,omitempty"`
	// SchemeParamsSnapshot: Contract Payment Terms Snapshot (approval note #2) —
	// SELURUH parameter beku saat kontrak dibuat. Kontrak berjalan TIDAK PERNAH
	// membaca master aktif. NULL = legacy.
	SchemeParamsSnapshot *string `gorm:"type:json" json:"scheme_params_snapshot,omitempty"`
	// ApprovalRequestID: seam Approval Workflow (blueprint §10) — engine menyusul.
	ApprovalRequestID *uint64 `json:"approval_request_id,omitempty"`

	// Produk Tambahan: Kelebihan Tanah (kelebihan-tanah-booking-integration-2026-08,
	// migration 000084) — komponen OPSIONAL. Dibawa dari Booking.Land* saat
	// konversi (CreateContract), atau diisi langsung di CreateContractRequest bila
	// kontrak dibuat tanpa Booking. LandReservationID tetap merujuk reservasi yang
	// SAMA sepanjang lifecycle kontrak — dikonversi (status=converted) di
	// land.RecordAkadTx saat Akad, tidak pernah diganti. Semua NULL = tanpa tanah.
	LandStockID           *uint64          `                           json:"land_stock_id,omitempty"`
	LandReservationID     *uint64          `                           json:"land_reservation_id,omitempty"`
	LandQuantityM2        *decimal.Decimal `gorm:"type:DECIMAL(20,4)" json:"land_quantity_m2,omitempty"`
	LandUnitPriceSnapshot *domain.Money    `gorm:"type:DECIMAL(20,4)" json:"land_unit_price_snapshot,omitempty"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (SaleContract) TableName() string { return "sale_contracts" }

// ── Model: PaymentSchedule ────────────────────────────────────────────────────

// PaymentSchedule mencatat satu baris jadwal cicilan buyer.
// Penerimaan pembayaran selalu lewat jalur Event 2 (RecordTermin).
// TIDAK memposting jurnal sendiri.
type PaymentSchedule struct {
	ID                uint64       `gorm:"primaryKey;autoIncrement"                      json:"id"`
	TenantID          uint64       `gorm:"not null;index"                                json:"-"`
	SaleContractID    uint64       `gorm:"not null;index"                                json:"sale_contract_id"`
	UnitID            uint64       `gorm:"not null;index"                                json:"unit_id"`
	InstallmentNumber int          `gorm:"not null"                                      json:"installment_number"`
	DueDate           time.Time    `gorm:"not null"                                      json:"due_date"`
	Amount            domain.Money `gorm:"type:DECIMAL(20,4);not null;default:'0.0000'" json:"amount"`
	// PaidAmount: akumulasi pembayaran untuk cicilan ini (partial payment).
	// 0 = belum dibayar; >= Amount = lunas. Cache turunan dari penerimaan.
	PaidAmount      domain.Money   `gorm:"type:DECIMAL(20,4);not null;default:'0.0000'" json:"paid_amount"`
	Type            ScheduleType   `gorm:"not null;size:20"                              json:"type"`
	Status          ScheduleStatus `gorm:"not null;size:20"                              json:"status"`
	TerminPaymentID *uint64        `gorm:"index"                                         json:"termin_payment_id,omitempty"`
	ReceivedAt      *time.Time     `                                                     json:"received_at,omitempty"`
	// ScheduleVersion: versi jadwal (naik saat reschedule/konversi; baris versi
	// lama yang belum terbayar menjadi superseded — BS-5).
	ScheduleVersion int       `gorm:"not null;default:1"                            json:"schedule_version"`
	CreatedAt       time.Time `                                                     json:"created_at"`
	UpdatedAt       time.Time `                                                     json:"updated_at"`
}

func (PaymentSchedule) TableName() string { return "payment_schedules" }

// ── Interfaces ────────────────────────────────────────────────────────────────

// ContractStore mengelola SaleContract dan PaymentSchedule.
// Semua query tenant-scoped (Invariant #6).
type ContractStore interface {
	SaveContract(ctx context.Context, c *SaleContract) error
	FindContractByID(ctx context.Context, tenantID, id uint64) (*SaleContract, error)
	FindContractByUnitID(ctx context.Context, tenantID, unitID uint64) (*SaleContract, error)
	// ListActiveContracts: kontrak non-cancelled (untuk agregat portfolio, mis.
	// PortfolioOutstanding). scheme_state NULL = legacy (dianggap aktif).
	ListActiveContracts(ctx context.Context, tenantID uint64) ([]*SaleContract, error)
	SaveScheduleItems(ctx context.Context, items []*PaymentSchedule) error
	FindScheduleByID(ctx context.Context, tenantID, id uint64) (*PaymentSchedule, error)
	UpdateScheduleStatus(ctx context.Context, tenantID, id uint64, status ScheduleStatus, terminPaymentID *uint64, receivedAt *time.Time) error
	// ApplyScheduleAllocation menulis SATU baris payment_allocations (sub-ledger)
	// DAN memperbarui cache paid_amount/status cicilan dalam satu operasi
	// (guard #1: tak ada paid_amount tanpa alokasi). Sumber kebenaran = alokasi.
	ApplyScheduleAllocation(ctx context.Context, tenantID uint64, in ScheduleAllocationInput) error
	// InsertBuyerCreditAllocation menulis baris saldo kredit buyer (schedule NULL)
	// untuk kelebihan bayar (pra-BAST). Menjaga Σ(alokasi) == termin.amount.
	InsertBuyerCreditAllocation(ctx context.Context, tenantID uint64, in BuyerCreditAllocationInput) error
	// AllocationReader — baca sub-ledger (P4 reader).
	AllocationReader
	// CreditStore — saldo kredit buyer + pemakaiannya (FE-3).
	CreditStore
	ListDueInPeriod(ctx context.Context, tenantID uint64, from, to time.Time) ([]*PaymentSchedule, error)
	ListScheduledBefore(ctx context.Context, tenantID uint64, before time.Time) ([]*PaymentSchedule, error)
	ListSchedulesByContract(ctx context.Context, tenantID, contractID uint64) ([]*PaymentSchedule, error)
}

// VATRateProvider menyediakan tarif PPN dari database (bukan konstanta inline).
// Diimplementasikan oleh tax.GORMRepository via adapter di wiring layer.
// TIDAK hardcode tarif — tarif diambil per tanggal transaksi.
type VATRateProvider interface {
	GetVATRate(ctx context.Context, tenantID uint64, rateCode string, referenceDate time.Time) (decimal.Decimal, error)
}

// ── Request types ─────────────────────────────────────────────────────────────

// CreateContractRequest adalah input untuk membuat SaleContract baru.
// TotalPrice = DPP (harga dasar sebelum PPN). Untuk PKP, service akan menghitung
// GrossAmount = DPP × (1 + VATRateSnapshot) dan menyimpannya sebagai tagihan buyer.
type CreateContractRequest struct {
	UnitID          uint64
	BuyerName       string
	BuyerID         string
	PaymentType     PaymentType
	BankKPR         *string
	LoanAmount      *domain.Money
	ContractDate    time.Time
	TotalPrice      domain.Money    // DPP; sumber kebenaran sebelum PPN
	IsPKP           bool            // true = penjual PKP, PPN terutang
	VATRateSnapshot decimal.Decimal // snapshot tarif PPN (misal 0.11); zero jika non-PKP
	// ── Increment 3: WAJIB saat scheme flow aktif (wiring produksi) ──────────
	PaymentSchemeID   *uint64 // master Payment Scheme (params dibekukan ke kontrak)
	FinancingSourceID *uint64 // bank KPR (wajib utk policy kpr; ditolak utk lainnya)
	CustomerID        *uint64 // enforcement kebijakan LOCKED Increment 1
	SalesPersonID     *uint64 // idem
	// AdminMarketingPersonID: opsional, independen dari SalesPersonID (P1 — Sales
	// ≠ Admin Marketing). Boleh diisi/diubah setelah kontrak dibuat via UpdateAdminMarketing.
	AdminMarketingPersonID *uint64
	CreatedBy         *uint64 // audit event contract_signed
	// BookingID (Increment 7): konversi booking → kontrak dalam SATU transaksi
	// (kontrak + reklas titipan→uang muka + buyer credit + unit booked→reserved).
	// Bila diisi, komponen tanah (bila ada) diambil dari Booking.Land* — abaikan
	// LandQuantityM2 di bawah.
	BookingID *uint64

	// LandQuantityM2: komponen opsional Kelebihan Tanah, HANYA dipakai kalau
	// kontrak dibuat LANGSUNG tanpa Booking (BookingID nil). Reservasi baru
	// dibuat atomik di CreateContract (land.ReserveTx), sama seperti jalur
	// booking. nil → kontrak ini tanpa komponen tanah.
	LandQuantityM2 *decimal.Decimal
}

// ScheduleItem adalah satu baris jadwal cicilan.
type ScheduleItem struct {
	InstallmentNumber int
	DueDate           time.Time
	Amount            domain.Money
	Type              ScheduleType
}

// RecordInstallmentPaidRequest adalah input untuk mencatat penerimaan cicilan.
type RecordInstallmentPaidRequest struct {
	ScheduleID      uint64
	BankAccountCode string // 1-1300 | 1-1400 | 1-1500
	ReceivedAt      time.Time
	Description     string
}

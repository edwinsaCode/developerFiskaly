package sale

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/shopspring/decimal"

	"esaproperti/internal/domain"
	"esaproperti/internal/land"
)

// ── TerminPayment (Event 2) ───────────────────────────────────────────────────

// TerminPayment menyimpan satu penerimaan uang muka/termin sebelum BAST.
// Jurnal: Dr Bank (1-13xx) / Cr Uang Muka Penjualan (2-2000) — bukan pendapatan (Invariant #7).
type TerminPayment struct {
	ID              uint64       `gorm:"primaryKey;autoIncrement" json:"id"`
	TenantID        uint64       `gorm:"not null;index"           json:"-"`
	UnitID          uint64       `gorm:"not null;index"           json:"unit_id"`
	ProjectID       uint64       `gorm:"not null;index"           json:"project_id"`
	PhaseID         *uint64      `gorm:"index"                    json:"phase_id,omitempty"`
	Amount          domain.Money `gorm:"type:DECIMAL(20,4);not null;default:'0.0000'" json:"amount"`
	BankAccountCode string       `gorm:"not null;size:20"         json:"bank_account_code"`
	Date            time.Time    `gorm:"not null"                 json:"date"`
	Description     string       `gorm:"size:500"                 json:"description"`
	JournalEntryID  uint64       `gorm:"not null;index"           json:"journal_entry_id"`
	// CreditAccountCode mencatat akun yang dikredit untuk penerimaan ini:
	// "2-2000" (Uang Muka, sebelum BAST) atau "1-2000" (Piutang, setelah BAST).
	// Audit trail eksplisit — sumber kebenaran tetap baris jurnal. NULL untuk
	// baris lama sebelum migrasi 000021.
	CreditAccountCode string `gorm:"size:20"                  json:"credit_account_code,omitempty"`
	// CreatedBy: user yang mencatat penerimaan (audit "siapa input"). NULL untuk baris lama.
	CreatedBy *uint64 `                                json:"created_by,omitempty"`
	// IdempotencyKey: kunci unik per-tenant untuk proteksi double-submit. NULL untuk baris lama.
	IdempotencyKey *string `gorm:"size:64"                  json:"-"`
	// PaymentSource: asal pencatatan (collection|schedule_received|unit_termin|
	// kpr_disbursement) — audit.
	PaymentSource PaymentSource `gorm:"size:20"                 json:"payment_source,omitempty"`
	// Kind (W-13): LABEL BISNIS terstruktur yang dipilih user di +Catat
	// Penerimaan (dp|installment|final_payment|other|bank_disbursement) —
	// berbeda dari PaymentSource (asal/routing akuntansi). Murni deskriptif:
	// tidak pernah dibaca resolver akun/jurnal/gate. Default "other" untuk
	// baris lama sebelum migrasi 000077.
	Kind TerminKind `gorm:"size:20;not null;default:'other'" json:"kind"`
	// InstallmentNo: nomor cicilan bila Kind=installment (mis. "Cicilan 2" → 2).
	// NULL untuk kind lain.
	InstallmentNo *int `gorm:"type:smallint unsigned"   json:"installment_no,omitempty"`
	// FinancingSourceID: bank penyalur bila penerimaan ini adalah pencairan KPR
	// (Increment 3). NULL untuk pembayaran buyer biasa.
	FinancingSourceID *uint64 `gorm:"index"                   json:"financing_source_id,omitempty"`
	// CountsTowardPrice (R4): SATU-SATUNYA penanda "pembayaran ini mengurangi
	// harga rumah". TRUE untuk semua pembayaran harga (DP/cicilan/pencairan KPR
	// + seluruh histori pra-R4). FALSE untuk booking fee kebijakan baru — fee
	// di LUAR harga unit, tidak pernah mengurangi outstanding (keputusan PO).
	// CATATAN: tanpa tag gorm `default` agar nilai FALSE ditulis apa adanya —
	// SETIAP titik insert wajib mengisi field ini eksplisit.
	CountsTowardPrice bool `gorm:"not null"                json:"counts_toward_price"`
	// ChargeGroupID (Billing Batch 2): grup tagihan yang menerima pembayaran ini
	// (Titipan Realisasi / addon). NULL untuk pembayaran harga rumah & booking.
	// Kas masuk tetap SATU tabel — tidak ada ledger kas kedua.
	ChargeGroupID *uint64   `gorm:"index"                   json:"charge_group_id,omitempty"`
	CreatedAt     time.Time `                                json:"created_at"`
	UpdatedAt     time.Time `                                json:"updated_at"`
}

// PaymentSource mencatat ASAL pencatatan pembayaran (semua tetap lewat ReceivePayment).
type PaymentSource string

const (
	PaymentSourceCollection       PaymentSource = "collection"
	PaymentSourceScheduleReceived PaymentSource = "schedule_received"
	PaymentSourceUnitTermin       PaymentSource = "unit_termin"
	// PaymentSourceBookingFee: penerimaan booking fee (Increment 7) — kredit ke
	// Titipan Booking 2-2100, TANPA alokasi (fee tertahan lifecycle booking).
	PaymentSourceBookingFee PaymentSource = "booking_fee"
	// PaymentSourceKPRDisbursement: pencairan bank KPR (Increment 3) — wajib
	// menyertakan financing_source_id.
	PaymentSourceKPRDisbursement PaymentSource = "kpr_disbursement"
	// PaymentSourceRealization (Billing Batch 2, K-1): pembayaran biaya realisasi
	// customer — Cr 2-2400 Titipan Realisasi, counts_toward_price=FALSE,
	// charge_group_id terisi, alokasi manual admin (K-3).
	PaymentSourceRealization PaymentSource = "realization"
	// PaymentSourceRealizationTransfer (K-4): pemakaian sisa titipan realisasi.
	// Leg harga rumah: Dr 2-2400 / Cr routing GAP-1, counts_toward_price=TRUE
	// (masuk primitif kanonik). Leg grup tujuan: non-kas, counts=FALSE.
	PaymentSourceRealizationTransfer PaymentSource = "realization_transfer"
)

// TerminKind (W-13) adalah LABEL BISNIS terstruktur untuk +Catat Penerimaan —
// apa yang dipilih user di dropdown "Jenis Penerimaan". Terpisah dari
// PaymentSource (asal/routing akuntansi): dua termin dengan Source yang sama
// (mis. "collection") bisa punya Kind berbeda (DP vs Cicilan vs Lainnya).
type TerminKind string

const (
	TerminKindDP               TerminKind = "dp"
	TerminKindInstallment      TerminKind = "installment"
	TerminKindFinalPayment     TerminKind = "final_payment"
	TerminKindOther            TerminKind = "other"
	TerminKindBankDisbursement TerminKind = "bank_disbursement"
	// TerminKindLandExcess: penerimaan atas piutang Kelebihan Tanah (baris
	// jadwal ScheduleTypeLand) — sebelumnya tidak punya kind sendiri sehingga
	// jatuh ke TerminKindInstallment ("Cicilan 9999") lewat resolveTerminKind.
	TerminKindLandExcess TerminKind = "land_excess"
)

// ValidTerminKind menolak nilai di luar daftar terkenal (fail-closed ringan —
// bukan invariant akuntansi, hanya menjaga dropdown tetap konsisten).
func ValidTerminKind(k TerminKind) bool {
	switch k {
	case TerminKindDP, TerminKindInstallment, TerminKindFinalPayment, TerminKindOther, TerminKindBankDisbursement, TerminKindLandExcess:
		return true
	default:
		return false
	}
}

// TerminKindLabel mengembalikan label bisnis customer-facing untuk sebuah
// Kind — dipakai kwitansi/riwayat penerimaan supaya keterangan dokumen
// SELALU jelas untuk apa (tanpa terkecuali), bukan istilah ledger internal.
func TerminKindLabel(k TerminKind, installmentNo *int) string {
	switch k {
	case TerminKindDP:
		return "DP (Uang Muka)"
	case TerminKindInstallment:
		if installmentNo != nil && *installmentNo > 0 && *installmentNo < 9999 {
			return fmt.Sprintf("Cicilan ke-%d", *installmentNo)
		}
		return "Cicilan"
	case TerminKindFinalPayment:
		return "Pelunasan"
	case TerminKindLandExcess:
		return "Kelebihan Tanah"
	case TerminKindBankDisbursement:
		return "Pencairan Dana Bank"
	default:
		return "Lainnya"
	}
}

// DescribeWithUnit menambahkan " — Unit {code}" pada deskripsi jurnal/termin
// supaya Jurnal & Buku Besar bisa langsung dikenali tanpa membuka baris
// detail (readability accounting, improvement 2026-09-18). unitCode kosong
// (transaksi tanpa relasi unit, mis. biaya operasional umum) → deskripsi
// dipertahankan apa adanya, tidak dipaksakan.
func DescribeWithUnit(base, unitCode string) string {
	if unitCode == "" {
		return base
	}
	return base + " — Unit " + unitCode
}

func (TerminPayment) TableName() string { return "termin_payments" }

// ── SaleRecord (Event 3+4) ────────────────────────────────────────────────────

// SaleRecord adalah snapshot BAST: pendapatan + HPP yang diakui saat serah terima.
// RevenueJournalID (Event 3) dan COGSJournalID (Event 4, nil jika HPP=0)
// keduanya diposting atomik — immutable setelah posted (Invariant #5).
type SaleRecord struct {
	ID                 uint64          `gorm:"primaryKey;autoIncrement"                        json:"id"`
	TenantID           uint64          `gorm:"not null;index"                                  json:"-"`
	UnitID             uint64          `gorm:"not null;uniqueIndex"                            json:"unit_id"` // satu unit hanya satu BAST
	ProjectID          uint64          `gorm:"not null;index"                                  json:"project_id"`
	PhaseID            *uint64         `gorm:"index"                                           json:"phase_id,omitempty"`
	SalePrice          domain.Money    `gorm:"type:DECIMAL(20,4);not null;default:'0.0000'"   json:"sale_price"` // DPP (neto sebelum PPN)
	IsVAT              bool            `gorm:"not null;default:0"                              json:"is_vat"`
	VATRate            decimal.Decimal `gorm:"type:DECIMAL(10,6);not null;default:'0.000000'" json:"vat_rate"` // e.g. 0.110000
	TotalAdvanceAtBAST domain.Money    `gorm:"type:DECIMAL(20,4);not null;default:'0.0000'"   json:"total_advance_at_bast"`
	// HPP snapshot per kategori (Invariant #4: immutable setelah posting)
	HPPLand      domain.Money `gorm:"type:DECIMAL(20,4);not null;default:'0.0000'"   json:"hpp_land"`
	HPPHard      domain.Money `gorm:"type:DECIMAL(20,4);not null;default:'0.0000'"   json:"hpp_hard"`
	HPPSoft      domain.Money `gorm:"type:DECIMAL(20,4);not null;default:'0.0000'"   json:"hpp_soft"`
	HPPFinancing domain.Money `gorm:"type:DECIMAL(20,4);not null;default:'0.0000'"   json:"hpp_financing"`
	// Metode HPP + referensi snapshot RAB (P0-2/P0-3). hpp_method='actual' +
	// snapshot NULL = HPP legacy (biaya akumulasi aktual). 'budgeted' + snapshot
	// terisi = HPP dari alokasi RAB versi (BudgetPlanID, BudgetPlanVersion).
	HPPMethod            string  `gorm:"not null;size:20;default:'actual'" json:"hpp_method"`
	AllocationSnapshotID *uint64 `gorm:"index"                             json:"allocation_snapshot_id,omitempty"`
	BudgetPlanID         *uint64 `                                         json:"budget_plan_id,omitempty"`
	BudgetPlanVersion    *int    `                                         json:"budget_plan_version,omitempty"`
	BuyerRef             string  `gorm:"size:200"                                        json:"buyer_ref"`
	// RecognitionDate (Temuan #7): tanggal Akad — saat pendapatan+HPP diakui.
	// Dulu bernama bast_date/BASTDate; lihat migration 000076.
	RecognitionDate time.Time `gorm:"column:recognition_date;not null" json:"recognition_date"`
	// HandedOverAt: tanggal serah terima fisik (Temuan #7) — NULL bila belum
	// diserahkan. Murni pencatatan; tidak pernah menggerbang jurnal apa pun.
	HandedOverAt     *time.Time `gorm:"column:handed_over_at" json:"handed_over_at,omitempty"`
	RevenueJournalID uint64     `gorm:"not null;index"                                  json:"revenue_journal_id"`
	COGSJournalID    *uint64    `gorm:"index"                                           json:"cogs_journal_id,omitempty"`
	// Increment 8: penanda pembatalan pasca-BAST (baris BAST tetap — append-only;
	// pembalikan via jurnal reversal yang dirujuk dokumen cancellation).
	CancelledAt    *time.Time `json:"cancelled_at,omitempty"`
	CancellationID *uint64    `json:"cancellation_id,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	// (S9/R-9: payload JSON juga membawa vat_amount + hpp_total derived —
	//  lihat MarshalJSON. FE nol aritmetika bisnis.)
	UpdatedAt time.Time `json:"updated_at"`
}

func (SaleRecord) TableName() string { return "sale_records" }

// HPPTotal mengembalikan total HPP (Invariant #4: HPP = biaya akumulasi unit).
func (s *SaleRecord) HPPTotal() domain.Money {
	return s.HPPLand.Add(s.HPPHard).Add(s.HPPSoft).Add(s.HPPFinancing)
}

// vatAmountOf adalah SATU-SATUNYA rumus PPN keluaran: DPP × rate, dibulatkan
// ke rupiah (Round 0). Dipakai RecordBAST, gross kontrak, dan payload JSON —
// jangan tulis ulang perkalian ini di tempat lain (registry #15).
func vatAmountOf(price domain.Money, rate decimal.Decimal) domain.Money {
	return domain.FromDecimal(price.Decimal().Mul(rate).Round(0))
}

// VATAmount mengembalikan PPN keluaran record ini (nol bila non-PKP).
func (s *SaleRecord) VATAmount() domain.Money {
	if !s.IsVAT {
		return domain.Zero
	}
	return vatAmountOf(s.SalePrice, s.VATRate)
}

// MarshalJSON (S9/R-9): payload membawa agregat derived (vat_amount, hpp_total)
// sehingga frontend TIDAK menghitung ulang angka uang (nol aritmetika bisnis).
func (s SaleRecord) MarshalJSON() ([]byte, error) {
	type alias SaleRecord
	return json.Marshal(struct {
		alias
		VATAmount string `json:"vat_amount"`
		HPPTotal  string `json:"hpp_total"`
	}{alias(s), s.VATAmount().String(), s.HPPTotal().String()})
}

// ── Input / request types ─────────────────────────────────────────────────────

// JournalLineInput mirrors ledger.LineInput tanpa import ledger package.
type JournalLineInput struct {
	AccountID   uint64
	Debit       domain.Money
	Credit      domain.Money
	ProjectID   *uint64
	PhaseID     *uint64
	UnitID      *uint64
	Description string
}

// RecordTerminRequest adalah input untuk Event 2 (penerimaan termin/uang muka).
type RecordTerminRequest struct {
	UnitID          uint64
	BankAccountCode string // 1-1100/1-1200 (kas) | 1-1300 | 1-1400 | 1-1500
	Amount          domain.Money
	Date            time.Time
	Description     string
	CreatedBy       *uint64       // audit: user yang mencatat (opsional)
	IdempotencyKey  *string       // proteksi double-submit (opsional)
	Source          PaymentSource // asal pencatatan (audit)
	// BankFee (UAT 2026-09-03, Rule #5): provisi/biaya administrasi yang
	// dipotong bank dari nominal pencairan (mis. KPR), DITANGGUNG DEVELOPER —
	// bukan titipan customer (K-1 tidak berubah). Nol (default) = jurnal 2
	// baris seperti sebelumnya, tanpa perubahan perilaku. >0 = Amount tetap
	// nilai piutang yang diselesaikan (kredit tidak berubah), tapi debit
	// dipecah: kas bersih (Amount-BankFee) + Beban Provisi Bank (5-3200).
	BankFee domain.Money
}

// RecordBASTRequest adalah input untuk Event 3+4 (BAST — pengakuan pendapatan + HPP).
type RecordBASTRequest struct {
	UnitID    uint64
	CreatedBy *uint64
	SalePrice domain.Money // DPP (sebelum PPN)
	IsVAT     bool
	VATRate   decimal.Decimal // e.g. 0.11; wajib > 0 jika IsVAT = true
	BuyerRef  string
	BASTDate  time.Time
	// BankApprovedAmount (Item 7A, UAT 2026-09-07): Nilai Persetujuan KPR
	// Bank — diisi SAAT AKAD untuk kontrak ber-scheme KPR, bukan saat
	// pembuatan kontrak. Dasar Dana Jaminan Bank (min(approved, gross-advance));
	// sisanya (jika ada) diposting sebagai Piutang Usaha. nil/kosong untuk
	// kontrak non-KPR — perilaku lama (satu baris piutang) tetap berlaku.
	BankApprovedAmount *domain.Money
}

// PhysicalHandoverParams adalah input eksekusi atomik serah terima fisik
// (Temuan #7) — TANPA jurnal, TANPA HPP. Unit harus `sold` (Akad sudah
// tercatat); hasilnya `occupied` + `sale_records.handed_over_at` terisi.
type PhysicalHandoverParams struct {
	TenantID     uint64
	UnitID       uint64
	HandoverDate time.Time
}

// UnitSaleInfo adalah data unit yang diperlukan service untuk memproses penjualan.
type UnitSaleInfo struct {
	ID        uint64
	ProjectID uint64
	PhaseID   *uint64
	Status    string // "available" | "reserved" | "sold"
	// UnitType: kode product type (UAT Batch 2 §2) — dasar routing akun
	// pendapatan BAST via Product Catalog.
	UnitType string
	// ListPrice: harga list unit (master) — sumber UnitPriceSnapshot kontrak.
	ListPrice domain.Money
	// Code: identifier kanonik unit (mis. "A-15") — SATU-SATUNYA sumber
	// "nama/kode unit" di domain ini. Tidak ada entitas Blok terpisah; "Blok"
	// hanya parameter input wizard bulk-create yang di-bake ke dalam Code
	// (lihat BulkCreateUnitsRequest.Block). Dipakai untuk deskripsi
	// jurnal/kwitansi yang bisa dikenali manusia (readability accounting).
	Code string
}

// BASTAtomicParams berisi semua input yang sudah divalidasi dan diresolved
// untuk eksekusi atomik Event 3+4 + update status unit.
type BASTAtomicParams struct {
	TenantID     uint64
	UnitID       uint64
	ProjectID    uint64
	PhaseID      *uint64
	BASTDate     time.Time
	BuyerRef     string
	SalePrice    domain.Money
	IsVAT        bool
	VATRate      decimal.Decimal
	TotalAdvance domain.Money
	HPPLand      domain.Money
	HPPHard      domain.Money
	HPPSoft      domain.Money
	HPPFinancing domain.Money
	// Metode HPP + draft snapshot RAB (P0-2/P0-3). HPPMethod default "actual".
	// Snapshot != nil hanya untuk metode budgeted — dipersist atomik di Execute.
	HPPMethod string
	Snapshot  *SnapshotDraft
	// Pre-built journal lines untuk validasi di test (dan posting di repo).
	RevenueLines []JournalLineInput // Event 3
	COGSLines    []JournalLineInput // Event 4; nil jika HPP total = 0
	// LandAdvance/LandAdvanceLines (bug 2026-09-04): bagian TotalAdvance yang
	// melebihi gross rumah pada kontrak Tunai lump-sum bercampur tanah — sudah
	// dikapitalisasi sebagai kas diterima tapi TIDAK disentuh Event 3 (yang
	// cuma menolkan sampai gross rumah). LandAdvanceLines (Dr UMP / Cr Piutang
	// Kelebihan Tanah) dinolkan Execute ATOMIK bersama landSchedule yang baru
	// dibuat, supaya paid_amount cache & sub-ledger credit_applications tidak
	// menganggap piutang tanah masih 100% outstanding. Zero/nil bila tidak
	// ada tanah atau tidak ada advance berlebih.
	LandAdvance      domain.Money
	LandAdvanceLines []JournalLineInput
	// CreatedBy: aktor BAST — ikut tercatat pada jurnal pengakuan piutang biaya
	// realisasi yang terbit di dalam transaksi ini (W-5).
	CreatedBy *uint64

	// Land (kelebihan-tanah-booking-integration-2026-08): komponen OPSIONAL
	// Produk Tambahan Kelebihan Tanah yang menempel pada kontrak unit ini.
	// Nil bila kontrak tidak menyertakan tanah. Sudah SEPENUHNYA disiapkan
	// oleh land.Service.PrepareBundledAkad (pool resolved, reservasi
	// tervalidasi, HPP resolved, jurnal dibangun) — Execute hanya meneruskan
	// ke land.RecordAkadTx di DALAM transaksi yang sama dengan Event3/4 unit,
	// sehingga rumah dan tanah diakui atomik bersama, pada Akad yang sama.
	Land *land.RecordAkadParams
	// SaleContractID: diresolusi untuk SEMUA Akad (bukan hanya yang membawa
	// tanah — diperluas saat perbaikan bug Saldo Kredit Buyer, 2026-09-04).
	// Dua pemakai:
	//   1. Bila Land != nil — WAJIB, menautkan baris payment_schedules
	//      (ScheduleTypeLand, migrasi 000095) yang dibuat atomik bersama
	//      land.RecordAkadTx, sehingga land_sales menjadi anchor AR yang
	//      terlihat oleh ReceivePayment/planAllocation (bug UAT: piutang
	//      Kelebihan Tanah sebelumnya tidak pernah muncul di Penerimaan
	//      meski sudah benar di Neraca).
	//   2. Execute memakainya sebagai FK audit (opsional) saat menutup SISA
	//      saldo kredit buyer yang dikonsumsi oleh netting Uang Muka
	//      Penjualan Event 3 (consumeRemainingCreditInTx) — bila nilai ini 0
	//      (kontrak tak ditemukan, data legacy) konsumsi tetap dicatat
	//      (sale_contract_id NULL di credit_applications), TIDAK di-skip dan
	//      TIDAK error, karena unit_id sudah cukup untuk audit & mencegah
	//      saldo itu terus terlihat "tersedia".
	// 0 bila kontrak tidak ditemukan (data legacy tanpa SaleContract formal).
	SaleContractID uint64
}

// Validasi rekening pembayaran kini COA-driven (lihat AccountFinder.ValidateCashBankAccount).
// Tidak ada lagi daftar kode kas/bank yang di-hardcode.

package reporting

import (
	"time"

	"esaproperti/internal/domain"
	"esaproperti/internal/receivable"
)

// ── Neraca (Balance Sheet) ────────────────────────────────────────────────────

type NeracaLine struct {
	Code   string `json:"code"`
	Name   string `json:"name"`
	Amount string `json:"amount"`
}

// NeracaReport adalah output laporan neraca konsolidasi.
// IsBalanced harus selalu true; jika false, ada jurnal yang tidak balanced.
type NeracaReport struct {
	AsOf time.Time `json:"as_of"`
	// From (Item 5, opsional): tanggal awal jendela Laba Rugi Periode Terpilih.
	// PENTING: From TIDAK PERNAH mengubah LabaRugiTahunBerjalan/TotalEkuitas/
	// IsBalanced — neraca adalah snapshot per tanggal (life-to-date sejak tutup
	// buku terakhir), bukan rentang. Mencampur P&L berjendela ke dalam identitas
	// neraca (Aset = Kewajiban+Ekuitas) akan merusak keseimbangan sebesar laba
	// yang diakui sebelum From (kontra-akun asetnya tetap kumulatif). From hanya
	// menghasilkan LabaRugiPeriodeTerpilih sebagai baris INFORMASI TAMBAHAN.
	From                  *time.Time   `json:"from,omitempty"`
	Aset                  []NeracaLine `json:"aset"`
	Kewajiban             []NeracaLine `json:"kewajiban"`
	Ekuitas               []NeracaLine `json:"ekuitas"`
	TotalAset             string       `json:"total_aset"`
	TotalKewajiban        string       `json:"total_kewajiban"`
	TotalEkuitas          string       `json:"total_ekuitas"`
	LabaRugiTahunBerjalan string       `json:"laba_rugi_tahun_berjalan"`
	// LabaRugiPeriodeTerpilih (Item 5, opsional): Laba Rugi dihitung ulang dari
	// jendela [From, AsOf] via ComputePL — MURNI INFORMASI, tidak ikut dalam
	// TotalEkuitas/TotalKewajibanEkuitas/IsBalanced. Kosong bila From nil.
	LabaRugiPeriodeTerpilih string `json:"laba_rugi_periode_terpilih,omitempty"`
	// TotalEkuitasEfektif = TotalEkuitas + LabaRugiTahunBerjalan — angka yang
	// ditampilkan sebagai "Total Ekuitas" ke user (laba berjalan termasuk).
	TotalEkuitasEfektif   string `json:"total_ekuitas_efektif"`
	TotalKewajibanEkuitas string `json:"total_kewajiban_ekuitas"`
	IsBalanced            bool   `json:"is_balanced"`
}

// ── Laba Rugi (P&L) ───────────────────────────────────────────────────────────

type PLLine struct {
	Code   string `json:"code"`
	Name   string `json:"name"`
	Amount string `json:"amount"`
}

// PLRawRow adalah baris mentah dari query DB untuk Laba Rugi.
type PLRawRow struct {
	AccountCode string       `gorm:"column:account_code"`
	AccountName string       `gorm:"column:account_name"`
	TotalDebit  domain.Money `gorm:"column:total_debit"`
	TotalCredit domain.Money `gorm:"column:total_credit"`
}

// RevenueByPaymentTypeRow (P3, item A — Dashboard Cash vs KPR): baris teragregasi
// per payment_type kontrak ("kpr"|"tunai"|"" untuk belum/tidak ber-kontrak).
// Dipakai baik untuk pendapatan diakui (GetRevenueByPaymentType) maupun nilai
// kontrak portofolio (GetContractValueByPaymentType) — bentuk sama.
type RevenueByPaymentTypeRow struct {
	PaymentType string       `gorm:"column:payment_type"`
	Amount      domain.Money `gorm:"column:amount"`
}

// UnitPLRow (S4): pendapatan & HPP per unit dari ledger posted ber-tag unit —
// basis unit_profits dashboard (menggantikan snapshot sale_records).
type UnitPLRow struct {
	UnitID  uint64       `gorm:"column:unit_id"`
	Revenue domain.Money `gorm:"column:revenue"`
	HPP     domain.Money `gorm:"column:hpp"`
}

// PLReport adalah output laporan Laba Rugi, struktur 8-bagian (item B,
// 2026-08-27, rule klien):
//
//	Pendapatan (-) HPP = Laba Kotor
//	(-) Beban Operasional = Laba Operasional
//	Pendapatan Luar Usaha (-) Beban Luar Usaha = Laba Bersih Sebelum Pajak
//	(-) Beban Pajak = Laba Bersih Setelah Pajak (LabaRugiBersih)
//
// Klasifikasi akun via ledger.AccountRole registry (ComputePL), bukan
// prefix-matching flat "semua 4-x / semua 5-x" seperti versi lama.
// ProjectID=nil berarti laporan konsolidasi.
type PLReport struct {
	AsOf time.Time `json:"as_of"`
	// From (Item 5, opsional): batas bawah rentang filter tanggal. nil = tanpa
	// batas bawah (life-to-date sejak tutup buku terakhir).
	From      *time.Time `json:"from,omitempty"`
	ProjectID *uint64    `json:"project_id,omitempty"`

	Pendapatan      []PLLine `json:"pendapatan"`
	TotalPendapatan string   `json:"total_pendapatan"`

	HPP      []PLLine `json:"hpp"`
	TotalHPP string   `json:"total_hpp"`

	LabaKotor string `json:"laba_kotor"` // TotalPendapatan − TotalHPP

	BebanOperasional      []PLLine `json:"beban_operasional"`
	TotalBebanOperasional string   `json:"total_beban_operasional"`

	LabaOperasional string `json:"laba_operasional"` // LabaKotor − TotalBebanOperasional

	PendapatanLuarUsaha      []PLLine `json:"pendapatan_luar_usaha"`
	TotalPendapatanLuarUsaha string   `json:"total_pendapatan_luar_usaha"`

	BebanLuarUsaha      []PLLine `json:"beban_luar_usaha"`
	TotalBebanLuarUsaha string   `json:"total_beban_luar_usaha"`

	LabaBersihSebelumPajak string `json:"laba_bersih_sebelum_pajak"`

	BebanPajak      []PLLine `json:"beban_pajak"`
	TotalBebanPajak string   `json:"total_beban_pajak"`

	// LabaRugiBersih = Laba Bersih Setelah Pajak (baris akhir P&L).
	LabaRugiBersih string `json:"laba_rugi_bersih"`
}

// ── Pipeline Penjualan ────────────────────────────────────────────────────────

type PipelineUnitStat struct {
	Count              int    `json:"count"`
	TotalListPrice     string `json:"total_list_price"`
	TotalAdvance       string `json:"total_advance,omitempty"`
	TotalContractValue string `json:"total_contract_value,omitempty"`
}

type PipelineReport struct {
	Available        PipelineUnitStat `json:"available"`
	Reserved         PipelineUnitStat `json:"reserved"`
	Sold             PipelineUnitStat `json:"sold"`
	TotalUnits       int              `json:"total_units"`
	ProjectedRevenue string           `json:"projected_revenue"` // Σ list_price (available+reserved)
}

// PipelineStats adalah hasil aggregasi dari repository.
type PipelineStats struct {
	AvailableCount  int
	AvailableSum    domain.Money
	ReservedCount   int
	ReservedSum     domain.Money
	TotalAdvance    domain.Money
	SoldCount       int
	SoldContractSum domain.Money
}

// ── Arus Kas ──────────────────────────────────────────────────────────────────

type ArusKasLine struct {
	Description string `json:"description"`
	Amount      string `json:"amount"` // positif = kas masuk; negatif = kas keluar
}

type ArusKasSection struct {
	Lines []ArusKasLine `json:"lines"`
	Net   string        `json:"net"`
}

type ArusKasReport struct {
	PeriodFrom time.Time      `json:"period_from"`
	PeriodTo   time.Time      `json:"period_to"`
	Operasi    ArusKasSection `json:"operasi"`
	Investasi  ArusKasSection `json:"investasi"`
	Pendanaan  ArusKasSection `json:"pendanaan"`
	NetChange  string         `json:"net_change"`
}

// CashMovement adalah satu jurnal yang menyentuh akun kas/bank (output repository).
type CashMovement struct {
	EntryID       uint64
	Date          time.Time
	Description   string
	NetBankChange domain.Money // positif = kas masuk; negatif = kas keluar
	IsInvesting   bool         // ada baris 1-4xxx (aset tetap) → investasi
	IsPendanaan   bool         // ada baris 2-5000 atau 3-xxx → pendanaan
}

// ── Piutang Customer (Accounts Receivable Aging) ─────────────────────────────
//
// Basis aging: jadwal cicilan (payment_schedules) — bukan hanya invoice. Setiap
// cicilan yang BELUM diterima adalah piutang operasional terhadap buyer, terlepas
// apakah invoice formal sudah diterbitkan. Nomor invoice ditampilkan bila ada
// (LEFT JOIN), "—" bila cicilan belum ditagih. Ini memberi tim koleksi gambaran
// utuh: tagihan jatuh tempo yang harus dikejar, bukan subset yang kebetulan sudah
// ber-invoice.
//
// Reconciliation: Σ(outstanding per cicilan) konsisten dengan sisa tagihan buyer
// (GrossAmount − Σ termin diterima) selama jadwal cicilan menjumlah ke GrossAmount.

// ── Piutang Customer (W-4: mesin & tipe dimiliki internal/receivable) ────────
//
// Tipe-tipe di bawah adalah ALIAS, bukan salinan. Mesin aging pindah ke paket
// `receivable` supaya penghasil baris (termasuk `charge` yang transaksional)
// tidak perlu meng-import paket laporan (TD-3). Alias menjaga seluruh pemanggil
// lama dan kontrak JSON /reports/ar-aging tetap identik — pemindahan mesin
// bukan pemindahan API.

// ARScheduleRow adalah satu baris kewajiban customer mentah (sebelum bucketing).
type ARScheduleRow = receivable.Row

// ARAgingBucket adalah kunci kategori umur piutang.
type ARAgingBucket = receivable.Bucket

const (
	BucketCurrent = receivable.BucketCurrent
	Bucket1_30    = receivable.Bucket1_30
	Bucket31_60   = receivable.Bucket31_60
	Bucket61_90   = receivable.Bucket61_90
	Bucket90Plus  = receivable.Bucket90Plus
)

// PaymentStatus adalah status pembayaran efektif yang DIHITUNG DI BACKEND.
type PaymentStatus = receivable.Status

const (
	PaymentScheduled = receivable.StatusScheduled
	PaymentDueToday  = receivable.StatusDueToday
	PaymentOverdue   = receivable.StatusOverdue
	PaymentPaid      = receivable.StatusPaid
)

// EffectiveStatus memetakan selisih hari (asOf − dueDate) ke status pembayaran
// untuk cicilan yang BELUM lunas. Dipakai bersama oleh laporan AR dan statement.
func EffectiveStatus(daysOverdue int) PaymentStatus { return receivable.EffectiveStatus(daysOverdue) }

// ARAgingRow adalah satu baris piutang outstanding di laporan aging.
type ARAgingRow = receivable.AgingRow

// ARAgingBucketSummary adalah ringkasan satu bucket (jumlah baris + total).
type ARAgingBucketSummary = receivable.BucketSummary

// ARAgingReport adalah output laporan Piutang Customer.
type ARAgingReport = receivable.AgingReport

// ── Laporan Kewajiban Pajak ───────────────────────────────────────────────────

type TaxLiabilityRow struct {
	ID            uint64  `json:"id"`
	UnitID        *uint64 `json:"unit_id,omitempty"`
	RateCode      string  `json:"rate_code"`
	TransferValue string  `json:"transfer_value"`
	Rate          string  `json:"rate"`
	TaxAmount     string  `json:"tax_amount"`
	Status        string  `json:"status"`
	AccrualDate   string  `json:"accrual_date"`
}

type TaxLiabilityReport struct {
	PeriodFrom       string            `json:"period_from"`
	PeriodTo         string            `json:"period_to"`
	Items            []TaxLiabilityRow `json:"items"`
	TotalObligation  string            `json:"total_obligation"`
	TotalPaid        string            `json:"total_paid"`
	TotalOutstanding string            `json:"total_outstanding"`
	// S7 (additive): rekonsiliasi obligations ↔ ledger 2-4000 (registry #14).
	// LedgerOutstanding = saldo RoleTaxLiability asOf period_to;
	// Reconciled = (Σ outstanding seluruh histori s/d period_to) == saldo ledger.
	LedgerOutstanding string `json:"ledger_outstanding"`
	Reconciled        bool   `json:"reconciled"`
}

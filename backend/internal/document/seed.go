package document

// Seed master jenis dokumen untuk tenant BARU. Cermin dari bagian 4 migration
// 000065 — migration mengurus tenant yang sudah ada, seed ini mengurus tenant
// berikutnya. Keduanya harus menghasilkan baris yang sama persis.

import (
	"context"
	"fmt"

	"gorm.io/gorm"
)

// Kode jenis dokumen inti. Disebut di SATU tempat saja — di daftar seed ini —
// supaya modul lain merujuknya lewat konstanta, bukan menulis ulang string.
const (
	TypeHousePayment     = "house_payment"     // KWT — kwitansi pembayaran rumah
	TypeBooking          = "booking"           // KWB — kwitansi booking
	TypeRealization      = "realization"       // KWR — kwitansi biaya realisasi
	TypeInternalTransfer = "internal_transfer" // MTI — memo transfer internal
	TypeInvoice          = "invoice"           // INV — invoice / tagihan
	TypeLegacyReceivable = "legacy_ar"         // KWL — kwitansi piutang proyek lama

	// W-3.1 — jenis dokumen kas. Dikelompokkan menurut ECONOMIC OWNERSHIP:
	// siapa pemilik ekonomis uang yang bergerak, bukan modul yang mencatatnya.
	TypeKPRDisbursement  = "kpr_disbursement"   // KWD — pencairan KPR (pembayar = bank)
	TypeCashIn           = "cash_in"            // BKM — kas masuk milik perusahaan
	TypeCashOut          = "cash_out"           // BKK — kas keluar milik perusahaan
	TypeThirdPartyPayout = "third_party_payout" // BTP — bayar titipan pihak ketiga
	TypeCustomerRefund   = "customer_refund"    // RFC — pengembalian dana customer
	TypeJournalReversal  = "journal_reversal"   // JR  — dokumen jurnal pembalik
)

var defaultTypes = []DocumentType{
	{Code: TypeHousePayment, Name: "Kwitansi Pembayaran Rumah", Prefix: "KWT"},
	{Code: TypeBooking, Name: "Kwitansi Booking", Prefix: "KWB"},
	{Code: TypeRealization, Name: "Kwitansi Biaya Realisasi", Prefix: "KWR"},
	{Code: TypeInternalTransfer, Name: "Memo Transfer Internal", Prefix: "MTI"},
	{Code: TypeInvoice, Name: "Invoice / Tagihan", Prefix: "INV"},
	{Code: TypeLegacyReceivable, Name: "Kwitansi Piutang Proyek Lama", Prefix: "KWL"},
	{Code: TypeKPRDisbursement, Name: "Kwitansi Pencairan KPR", Prefix: "KWD"},
	{Code: TypeCashIn, Name: "Bukti Kas Masuk", Prefix: "BKM"},
	{Code: TypeCashOut, Name: "Bukti Kas Keluar", Prefix: "BKK"},
	{Code: TypeThirdPartyPayout, Name: "Bukti Pembayaran Titipan", Prefix: "BTP"},
	{Code: TypeCustomerRefund, Name: "Bukti Pengembalian Dana Customer", Prefix: "RFC"},
	{Code: TypeJournalReversal, Name: "Bukti Jurnal Pembalik", Prefix: "JR"},
}

// SeedDefaultDocumentTypes idempoten — aman dipanggil ulang.
func SeedDefaultDocumentTypes(ctx context.Context, db *gorm.DB, tenantID uint64) error {
	for _, t := range defaultTypes {
		row := t
		row.TenantID = tenantID
		row.NumberFormat = "{prefix}/{year}/{seq}"
		row.ResetPolicy = ResetYearly
		row.Padding = 6
		row.IsActive = true
		row.IsSystem = true
		if err := db.WithContext(ctx).
			Where("tenant_id = ? AND code = ?", tenantID, row.Code).
			FirstOrCreate(&row).Error; err != nil {
			return fmt.Errorf("seed jenis dokumen %s: %w", row.Code, err)
		}
	}
	return nil
}

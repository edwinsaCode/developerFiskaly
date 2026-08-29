package ledger

import (
	"context"
	"fmt"
	"log"

	"gorm.io/gorm"

	"esaproperti/internal/domain"
)

// coaEntry defines one row in the default chart of accounts.
type coaEntry struct {
	Code          string
	Name          string
	Type          domain.AccountType
	NormalBalance domain.NormalBalance
	IsSystem      bool
	Description   string
}

// defaultCOA is the real-estate developer chart of accounts for Indonesia.
// These are the system accounts that every new tenant starts with.
// ★ marks accounts critical to the real-estate capitalization mechanism.
var defaultCOA = []coaEntry{
	// ── ASET (1-xxxx) ────────────────────────────────────────────────────────
	{"1-1100", "Kas — Kas Besar", domain.AccountAsset, domain.NormalBalanceDebit, true, ""},
	{"1-1200", "Kas — Petty Cash", domain.AccountAsset, domain.NormalBalanceDebit, true, ""},
	{"1-1300", "Bank — BCA", domain.AccountAsset, domain.NormalBalanceDebit, false, ""},
	{"1-1400", "Bank — Mandiri", domain.AccountAsset, domain.NormalBalanceDebit, false, ""},
	{"1-1500", "Bank — BRI", domain.AccountAsset, domain.NormalBalanceDebit, false, ""},
	{"1-2000", "Piutang Usaha", domain.AccountAsset, domain.NormalBalanceDebit, true, ""},
	{"1-2100", "Piutang Lain-lain", domain.AccountAsset, domain.NormalBalanceDebit, false, ""},
	{"1-2200", "Piutang Bank (KPR)", domain.AccountAsset, domain.NormalBalanceDebit, true,
		"Piutang kepada bank KPR dalam jendela akad → pencairan. Setelah bank mencairkan, " +
			"bank selesai pada nilai pencairan aktualnya: sisa tagihan direklas ke 1-2000 Piutang Usaha (customer)"},
	{"1-3000", "Persediaan Real Estat — Tanah", domain.AccountAsset, domain.NormalBalanceDebit, true,
		"★ Biaya tanah yang dikapitalisasi sebagai persediaan"},
	{"1-3100", "Persediaan Real Estat — Hard Cost", domain.AccountAsset, domain.NormalBalanceDebit, true,
		"★ Biaya konstruksi yang dikapitalisasi"},
	{"1-3200", "Persediaan Real Estat — Soft Cost", domain.AccountAsset, domain.NormalBalanceDebit, true,
		"★ Desain, perizinan, legal yang dikapitalisasi"},
	{"1-3300", "Persediaan Real Estat — Biaya Pembiayaan", domain.AccountAsset, domain.NormalBalanceDebit, true,
		"★ Bunga/biaya pinjaman yang dikapitalisasi"},
	{"1-4000", "Aset Tetap — Peralatan Kantor", domain.AccountAsset, domain.NormalBalanceDebit, false, ""},
	{"1-4100", "Aset Tetap — Kendaraan", domain.AccountAsset, domain.NormalBalanceDebit, false, ""},
	{"1-4900", "Akumulasi Penyusutan", domain.AccountAsset, domain.NormalBalanceCredit, true,
		"Kontra-aset; normal balance kredit"},
	{"1-5000", "Biaya Dibayar di Muka", domain.AccountAsset, domain.NormalBalanceDebit, false, ""},
	{"1-5100", "PPN Masukan", domain.AccountAsset, domain.NormalBalanceDebit, false, ""},
	{"1-5300", "Uang Muka Vendor", domain.AccountAsset, domain.NormalBalanceDebit, true,
		"★ Pembayaran ke vendor sebelum pekerjaannya diakui (W-11 D-8). BUKAN biaya dan BUKAN realisasi RAB sampai dikompensasi ke termin."},

	// ── KEWAJIBAN (2-xxxx) ───────────────────────────────────────────────────
	{"2-1000", "Hutang Usaha", domain.AccountLiability, domain.NormalBalanceCredit, true, ""},
	{"2-1100", "Hutang Retensi Kontraktor", domain.AccountLiability, domain.NormalBalanceCredit, true,
		"★ Bagian termin kontraktor yang ditahan sampai masa pemeliharaan selesai (W-11 D-6). Kewajiban tersendiri: jatuh tempo & syarat pelepasannya berbeda dari 2-1000."},
	{"2-2000", "Uang Muka Penjualan", domain.AccountLiability, domain.NormalBalanceCredit, true,
		"★ DP/termin buyer sebelum BAST — bukan pendapatan (Invariant #7)"},
	{"2-2300", "Titipan Notaris", domain.AccountLiability, domain.NormalBalanceCredit, true,
		"★ Dana customer utk biaya notaris — kewajiban sampai dibayarkan ke notaris (UAT Batch 2 §3); BUKAN pendapatan developer"},
	{"2-2100", "Titipan Booking", domain.AccountLiability, domain.NormalBalanceCredit, true,
		"Uang titipan booking fee sebelum PPJB (Increment 7, blueprint §2). Konversi → reklas ke 2-2000; hangus → 4-2000."},
	{"2-2200", "Hutang Refund", domain.AccountLiability, domain.NormalBalanceCredit, true,
		"Kewajiban pengembalian dana buyer (Increment 8, blueprint §1). Dikredit saat cancellation processed; didebit saat refund dibayar."},
	{"2-2400", "Titipan Realisasi", domain.AccountLiability, domain.NormalBalanceCredit, true,
		"★ Titipan biaya realisasi customer — PDAM/BPHTB/Notaris/Listrik (K-1 2026-08-03). BUKAN pendapatan: terima Cr, payout vendor Dr; sisa titipan wajib refund/transfer (K-4)."},
	{"2-3000", "PPN Keluaran", domain.AccountLiability, domain.NormalBalanceCredit, true, ""},
	{"2-4000", "Hutang PPh Final Pengalihan", domain.AccountLiability, domain.NormalBalanceCredit, true,
		"★ PPh Final 2,5% atas pengalihan properti (PP 34/2016)"},
	{"2-5000", "Hutang Bank", domain.AccountLiability, domain.NormalBalanceCredit, false, ""},
	{"2-6000", "Biaya Akrual", domain.AccountLiability, domain.NormalBalanceCredit, false, ""},
	{"2-6100", "Hutang Gaji & Tunjangan", domain.AccountLiability, domain.NormalBalanceCredit, false, ""},
	{"2-6200", "Utang Komisi", domain.AccountLiability, domain.NormalBalanceCredit, true,
		"Kewajiban komisi sales yang sudah diakru (Increment 9, blueprint §3)."},

	// ── EKUITAS (3-xxxx) ─────────────────────────────────────────────────────
	{"3-1000", "Modal Disetor", domain.AccountEquity, domain.NormalBalanceCredit, true, ""},
	{"3-2000", "Laba Ditahan", domain.AccountEquity, domain.NormalBalanceCredit, true, ""},
	{"3-3000", "Laba/Rugi Tahun Berjalan", domain.AccountEquity, domain.NormalBalanceCredit, true,
		"Ditutup ke 3-2000 setiap akhir periode"},

	// ── PENDAPATAN (4-xxxx) ──────────────────────────────────────────────────
	{"4-1000", "Pendapatan Penjualan Unit", domain.AccountRevenue, domain.NormalBalanceCredit, true,
		"★ Diakui point-in-time saat BAST (PSAK 72)"},
	{"4-2000", "Pendapatan Lain-lain", domain.AccountRevenue, domain.NormalBalanceCredit, false, ""},
	{"4-2100", "Pendapatan Booking", domain.AccountRevenue, domain.NormalBalanceCredit, true,
		"★ Booking fee diakui LANGSUNG saat diterima (rule klien 2026-07-29) — akun tersendiri, tidak pernah direklas ke Penjualan Rumah, tidak ada reversal saat batal"},
	{"4-1100", "Pendapatan Penjualan Tanah", domain.AccountRevenue, domain.NormalBalanceCredit, true,
		"★ Kelebihan Tanah (LT-5) — pendapatan penjualan tanah kaveling per m², terpisah dari Penjualan Unit (4-1000)"},

	// ── BEBAN (5-xxxx) ───────────────────────────────────────────────────────
	{"5-1000", "Harga Pokok Penjualan (HPP)", domain.AccountExpense, domain.NormalBalanceDebit, true,
		"★ Biaya terakumulasi unit dipindah dari Persediaan saat unit terjual"},
	{"5-2000", "Beban PPh Final Pengalihan", domain.AccountExpense, domain.NormalBalanceDebit, true,
		"★ 2,5% nilai pengalihan (PP 34/2016)"},
	{"5-3000", "Beban Pemasaran", domain.AccountExpense, domain.NormalBalanceDebit, false, ""},
	{"5-3100", "Beban Komisi Penjualan", domain.AccountExpense, domain.NormalBalanceDebit, false, ""},
	{"5-4000", "Beban Umum & Administrasi", domain.AccountExpense, domain.NormalBalanceDebit, false, ""},
	{"5-4100", "Beban Gaji & Tunjangan", domain.AccountExpense, domain.NormalBalanceDebit, false, ""},
	{"5-4200", "Beban Sewa Kantor", domain.AccountExpense, domain.NormalBalanceDebit, false, ""},
	{"5-4300", "Beban Utilitas", domain.AccountExpense, domain.NormalBalanceDebit, false, ""},
	{"5-4400", "Beban Perjalanan Dinas", domain.AccountExpense, domain.NormalBalanceDebit, false, ""},
	{"5-4500", "Beban Penyusutan", domain.AccountExpense, domain.NormalBalanceDebit, false, ""},
	{"5-5000", "Beban Bunga", domain.AccountExpense, domain.NormalBalanceDebit, false,
		"Bunga yang tidak dikapitalisasi (sudah terealisasi)"},
}

// SeedCOA inserts the default chart of accounts for a tenant if it doesn't already exist.
// Safe to call multiple times (idempotent via FirstOrCreate keyed on (tenant_id, code)).
func SeedCOA(ctx context.Context, db *gorm.DB, tenantID uint64) error {
	log.Printf("[SEED] COA — tenant %d", tenantID)
	created, skipped := 0, 0
	for _, e := range defaultCOA {
		acc := Account{
			TenantID:      tenantID,
			Code:          e.Code,
			Name:          e.Name,
			Type:          e.Type,
			NormalBalance: e.NormalBalance,
			IsSystem:      e.IsSystem,
			Category:      seedCategoryFor(e.Code, e.Type),
			Description:   e.Description,
		}
		result := db.WithContext(ctx).
			Where(Account{TenantID: tenantID, Code: e.Code}).
			FirstOrCreate(&acc)
		if result.Error != nil {
			return fmt.Errorf("seed COA %s: %w", e.Code, result.Error)
		}
		if result.RowsAffected == 0 {
			skipped++
		} else {
			created++
		}
	}
	log.Printf("[SEED] COA: %d created, %d skipped", created, skipped)
	return nil
}

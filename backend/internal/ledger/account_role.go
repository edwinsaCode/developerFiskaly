package ledger

// ═════════════════════════════════════════════════════════════════════════════
// AccountRoleRegistry (Phase 2 · S1 — docs/financial-canonical-registry.md §R-1)
//
// SATU-SATUNYA definisi "akun mana termasuk peran keuangan apa" (cash/bank,
// piutang, hutang, titipan booking, komisi, persediaan, PPN, dst). Sebelumnya
// keanggotaan ini tersebar sebagai daftar kode hardcoded & prefix `LIKE` di
// banyak query (dashboard, cash-flow, cost, allocation) yang bisa menyimpang.
//
// KONSTITUSI: setiap pemilihan akun untuk MENGHITUNG SALDO wajib lewat registry
// ini — dilarang menuliskan `a.code IN ('1-2000'...)` / `LIKE '1-3%'` sendiri.
// (Sisi POSTING jurnal memakai taxonomy domain.CostCategory.InventoryAccountCode
//  — mapping penulis, di luar cakupan pembacaan saldo ini.)
//
// S1 memperkenalkan registry + memigrasikan konsumen yang keanggotaannya SUDAH
// identik (persediaan di cost & allocation — dulu dua definisi kembar). Konsumen
// yang keanggotaannya masih menyimpang (cash-flow kode-hardcoded) TIDAK diubah
// di S1: mereka menyentuh perbedaan angka yang digerbang keputusan PO pada
// S4/S5/S6. (P&L revenue/HPP/other-income sudah dimigrasikan ke registry ini —
// lihat RoleOtherIncome/RoleOtherExpense/RoleTaxExpense, item B 2026-08-27.)
// ═════════════════════════════════════════════════════════════════════════════

// AccountRole adalah peran keuangan sebuah akun untuk perhitungan saldo.
type AccountRole string

const (
	RoleCashBank            AccountRole = "cash_bank"             // kas & bank (COA category-driven)
	RoleReceivable          AccountRole = "receivable"           // piutang usaha/lain/KPR
	RolePayable             AccountRole = "payable"              // hutang usaha
	RoleBookingLiability    AccountRole = "booking_liability"    // titipan booking
	RoleCommissionLiability AccountRole = "commission_liability" // utang komisi
	RoleInventory           AccountRole = "inventory"            // persediaan real estat (kapitalisasi)
	RoleVATOutput           AccountRole = "vat_output"           // PPN keluaran
	RoleVATInput            AccountRole = "vat_input"            // PPN masukan
	RoleTaxLiability        AccountRole = "tax_liability"        // hutang PPh final
	RoleCOGS                AccountRole = "cogs"                 // HPP (beban pokok penjualan)
	RoleUnitSalesRevenue    AccountRole = "unit_sales_revenue"   // pendapatan penjualan RUMAH (4-1000) — utk laporan per-unit
	RoleBookingRevenue      AccountRole = "booking_revenue"      // Pendapatan Booking 4-2100 (rule klien 2026-07-29)
	RoleNotaryLiability     AccountRole = "notary_liability"     // Titipan Notaris 2-2300 (UAT Batch 2 §3)
	RoleRealizationDeposit  AccountRole = "realization_deposit"  // Titipan Realisasi 2-2400 (Billing Batch 2, K-1)

	// W-11 (D-6, D-8). Dua peran BARU — sengaja terpisah, bukan ditambahkan ke
	// peran yang sudah ada:
	//
	//   RoleRetentionPayable TIDAK digabung ke RolePayable. Selain alasan
	//   akuntansi (jatuh tempo & syarat pelepasan retensi berbeda dari hutang
	//   termin biasa), RolePayable dipakai cost/accounts.go untuk MEMILIH AKUN
	//   KREDIT saat memposting biaya. Menambah anggota ke sana akan mengubah
	//   perilaku posting, bukan cuma angka laporan.
	//
	//   RoleVendorAdvance dipisah dari RoleVATInput/aset lain karena uang muka
	//   vendor adalah HAK TAGIH atas pekerjaan yang belum diakui — bukan biaya,
	//   bukan pajak, dan TIDAK boleh terbaca sebagai realisasi RAB.
	RoleRetentionPayable AccountRole = "retention_payable" // Hutang Retensi Kontraktor 2-1100 (W-11 D-6)
	RoleVendorAdvance    AccountRole = "vendor_advance"    // Uang Muka Vendor 1-5300 (W-11 D-8)

	// L/R restructure (item B, 2026-08-27): klasifikasi P&L 8-bagian
	// (Pendapatan/HPP/Beban Operasional/Pendapatan&Beban Luar Usaha/Beban Pajak)
	// via registry — bukan prefix-matching "semua 4-x = pendapatan, semua 5-x =
	// beban" seperti ComputePL lama. Default aman utk akun BARU yang belum
	// diklasifikasi eksplisit: 4-x → RolePendapatan inti; 5-x → RoleBebanOperasional
	// inti (lihat ComputePL) — hanya akun yang secara eksplisit di luar usaha/pajak
	// yang perlu didaftarkan di sini.
	RoleOtherIncome  AccountRole = "other_income"  // Pendapatan Luar Usaha — 4-2000
	RoleOtherExpense AccountRole = "other_expense" // Beban Luar Usaha — 5-5000 (bunga tak dikapitalisasi)
	RoleTaxExpense   AccountRole = "tax_expense"   // Beban Pajak — 5-2000 (PPh Final Pengalihan)
)

// roleCodes memetakan peran berbasis-KODE-tetap ke himpunan kode COA kanonik.
// (RoleCashBank TIDAK di sini — keanggotaannya via COA category yang editable
//  user, bukan kode tetap; lihat AccountInRole.)
var roleCodes = map[AccountRole][]string{
	RoleReceivable:          {"1-2000", "1-2100", "1-2200"},
	RolePayable:             {"2-1000"},
	RoleBookingLiability:    {"2-2100"},
	RoleCommissionLiability: {"2-6200"},
	RoleInventory:           {"1-3000", "1-3100", "1-3200", "1-3300"},
	RoleVATOutput:           {"2-3000"},
	RoleVATInput:            {"1-5100"},
	RoleTaxLiability:        {"2-4000"},
	RoleCOGS:                {"5-1000"},
	RoleUnitSalesRevenue:    {"4-1000"},
	RoleBookingRevenue:      {"4-2100"},
	RoleNotaryLiability:     {"2-2300"},
	RoleRealizationDeposit:  {"2-2400"},
	RoleRetentionPayable:    {"2-1100"},
	RoleVendorAdvance:       {"1-5300"},
	RoleOtherIncome:         {"4-2000"},
	RoleOtherExpense:        {"5-5000"},
	RoleTaxExpense:          {"5-2000"},
}

// RoleCodeList mengembalikan salinan daftar kode COA untuk peran berbasis-kode.
// Untuk RoleCashBank kembalikan nil (gunakan AccountInRole / COA category).
func RoleCodeList(role AccountRole) []string {
	codes, ok := roleCodes[role]
	if !ok {
		return nil
	}
	out := make([]string, len(codes))
	copy(out, codes)
	return out
}

// AccountInRole melaporkan apakah akun `a` termasuk peran `role`.
//   - RoleCashBank: via COA category (cash|bank) — COA-driven, konsisten dengan
//     ValidatePaymentAccount & ListCashBankAccounts (satu otoritas klasifikasi).
//   - peran lain: keanggotaan kode dari roleCodes.
func AccountInRole(a *Account, role AccountRole) bool {
	if a == nil {
		return false
	}
	if role == RoleCashBank {
		return a.Category.IsCashBank()
	}
	for _, c := range roleCodes[role] {
		if a.Code == c {
			return true
		}
	}
	return false
}

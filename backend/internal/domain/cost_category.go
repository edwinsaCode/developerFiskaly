package domain

// CostCategory classifies a development cost for capitalization routing.
// Each category maps to a distinct Persediaan Real Estat sub-account (1-3xxx).
//
// Phase 5: the posting engine uses CostCategory.InventoryAccountCode() to determine
// the Dr account when posting Event 1 (kapitalisasi biaya pengembangan).
// Phase 6: the allocation engine groups CostEntry records by category before
// distributing project-level costs across units.
type CostCategory string

const (
	// CostCategoryLand covers land acquisition costs → akun 1-3000.
	CostCategoryLand CostCategory = "land"
	// CostCategoryHard covers construction/hard costs → akun 1-3100. RULE KLIEN
	// (UAT 2026-09-03): HPP Konstruksi = Produksi + Sarana & Prasarana +
	// Perizinan (IMB/SLF/dll — biaya langsung terkait pembangunan fisik).
	CostCategoryHard CostCategory = "hard"
	// CostCategorySoft covers design/legal fees → akun 5-4700 Beban Soft Cost.
	// RULE KLIEN (FREEZE 2026-09-04): HPP hanya terdiri dari 2 kelompok — Tanah,
	// dan Konstruksi/Hard Cost (Produksi/Perizinan/Sarana & Prasarana). Soft Cost
	// BUKAN HPP walaupun ada di RAB — direklasifikasi dari kapitalisasi (bekas
	// akun Persediaan 1-3200) menjadi beban periode murni, diakui saat realisasi
	// benar-benar terjadi (bukan saat approval RAB). Field UnitCostBreakdown.Soft
	// dipertahankan HANYA untuk data historis pra-reklasifikasi — jangan populate
	// dari kode baru (lihat komentar di bawah).
	CostCategorySoft CostCategory = "soft"

	// Kategori BEBAN (period expense) — hanya sah untuk CostTier overhead.
	// TIDAK PERNAH dikapitalisasi ke Persediaan, TIDAK PERNAH masuk pool HPP
	// (budgeted-cost-allocation-spec.md §3). Increment 2 (Cost 3-tier).
	// RULE KLIEN (FREEZE 2026-09-04): Soft Cost bergabung ke himpunan ini.

	// CostCategoryMarketing covers marketing/promotion spend → akun 5-3000 Beban Pemasaran.
	CostCategoryMarketing CostCategory = "marketing"
	// CostCategoryOther covers general & administrative spend → akun 5-4000 Beban Umum & Administrasi.
	CostCategoryOther CostCategory = "other"
	// CostCategoryOperational covers operational running costs (dahulu "Pendanaan"/
	// financing) → akun 5-4600 Beban Operasional. RULE KLIEN (2026-09-04):
	// Operasional BUKAN HPP — direklasifikasi dari kapitalisasi (bekas
	// CostCategoryFinancing, akun Persediaan 1-3300) menjadi beban periode murni.
	// HPP yang tersisa hanya: Tanah, Produksi (Hard), Sarana & Prasarana (Hard),
	// Perizinan (Hard). Nama konstanta lama CostCategoryFinancing/akun 1-3300
	// TIDAK dipakai lagi untuk plan RAB baru — dipertahankan hanya di
	// UnitCostBreakdown.Financing utk kompatibilitas data historis (lihat komentar
	// di bawah).
	CostCategoryOperational CostCategory = "operational"
)

// AllCostCategories is the canonical, ordered list of CAPITALIZABLE cost classes
// (the HPP accounting-class taxonomy). It deliberately EXCLUDES the expense-only
// categories (marketing/other/operational/soft) — allocation snapshots, HPP pools,
// and per-class journal lines must keep iterating exactly these two: Tanah, dan
// Konstruksi/Hard Cost (RULE KLIEN FREEZE 2026-09-04: HPP hanya 2 kelompok — Soft
// Cost dikeluarkan dari HPP, menyusul Operasional yang sudah dikeluarkan sebelumnya).
// This is the SINGLE SOURCE OF TRUTH for the accounting-class taxonomy — iterate
// this instead of hardcoding category strings when building per-class journal
// lines, allocation snapshots, or reports.
var AllCostCategories = []CostCategory{
	CostCategoryLand, CostCategoryHard,
}

// ExpenseCostCategories is the canonical, ordered list of expense-only (period
// cost) categories. Companion of AllCostCategories; the two sets are disjoint.
var ExpenseCostCategories = []CostCategory{
	CostCategoryMarketing, CostCategoryOther, CostCategoryOperational, CostCategorySoft,
}

// Valid returns true if c is a recognized CostCategory (capitalizable OR expense).
// Tier-compatibility (which tier may use which category) is CostTier.AllowsCategory.
func (c CostCategory) Valid() bool {
	return c.IsCapitalizable() || c.IsExpense()
}

// IsCapitalizable returns true untuk kategori yang dikapitalisasi ke Persediaan
// Real Estat (1-3xxx) dan masuk pool HPP: land|hard (RULE KLIEN FREEZE 2026-09-04).
func (c CostCategory) IsCapitalizable() bool {
	switch c {
	case CostCategoryLand, CostCategoryHard:
		return true
	}
	return false
}

// IsExpense returns true untuk kategori beban periode: marketing|other|operational|soft.
func (c CostCategory) IsExpense() bool {
	return c == CostCategoryMarketing || c == CostCategoryOther || c == CostCategoryOperational || c == CostCategorySoft
}

// ExpenseAccountCode returns akun beban P&L (Dr side) untuk kategori expense-only.
// PEMETAAN TUNGGAL & TERPUSAT (freeze budgeted-cost-allocation-spec.md §3) —
// budget.BudgetCategory.ExpenseAccountCode mendelegasikan ke sini; jangan
// hardcode akun beban di tempat lain. Kategori kapitalisasi mengembalikan ""
// (mereka lewat InventoryAccountCode ke Persediaan).
func (c CostCategory) ExpenseAccountCode() string {
	switch c {
	case CostCategoryMarketing:
		return "5-3000" // Beban Pemasaran
	case CostCategoryOther:
		return "5-4000" // Beban Umum & Administrasi
	case CostCategoryOperational:
		return "5-4600" // Beban Operasional — TIDAK diregistrasi RoleOtherExpense/RoleCOGS/
		// RoleTaxExpense, sehingga ComputePL memasukkannya ke default bucket
		// "Beban Operasional" (bukan "Beban Luar Usaha" — beda dgn 5-5000 Beban
		// Bunga yang memang non-operating).
	case CostCategorySoft:
		return "5-4700" // Beban Soft Cost (Desain & Legal) — RULE KLIEN FREEZE
		// 2026-09-04: direalisasi sebagai beban periode, bukan lagi dikapitalisasi.
	}
	return ""
}

// InventoryAccountCode returns the Persediaan Real Estat sub-account code
// that costs in this category should be posted to (Dr side, posting-rules.md Event 1).
// Phase 5: posting engine calls this to route each CostEntry to the correct account.
// CostCategorySoft sengaja TIDAK punya case di sini (RULE KLIEN FREEZE 2026-09-04:
// Soft Cost bukan lagi kategori kapitalisasi) — jatuh ke "" seperti kategori beban
// lainnya.
func (c CostCategory) InventoryAccountCode() string {
	switch c {
	case CostCategoryLand:
		return "1-3000"
	case CostCategoryHard:
		return "1-3100"
	}
	return ""
}

// UnitCostBreakdown holds a unit's accumulated development cost split by category.
//
// Phase 5: the cost allocation engine populates this from CostEntry records tagged
// to the unit (direct costs) plus the unit's proportional share of project-level costs.
// Phase 7: breakdown.Total() is the HPP basis for Event 4 (posting-rules.md) when
// the unit is sold — it must equal the sum credited from Persediaan Real Estat.
type UnitCostBreakdown struct {
	Land Money // porsi biaya tanah
	Hard Money // porsi biaya konstruksi (produksi, sarana & prasarana, perizinan)
	// Soft: LEGACY. Dahulu porsi biaya desain/legal dikapitalisasi (akun bekas
	// 1-3200). RULE KLIEN FREEZE (2026-09-04): kategori ini direklasifikasi jadi
	// beban periode (akun 5-4700), jadi TIDAK PERNAH lagi diisi untuk plan RAB
	// yang disetujui setelah tanggal itu — field ini hanya dipertahankan agar
	// snapshot/SaleRecord HPP historis (dari sebelum reklasifikasi) tetap terbaca
	// apa adanya. Jangan populate field ini dari kode baru.
	Soft Money
	// Financing: LEGACY. Dahulu porsi bunga/biaya pinjaman dikapitalisasi (bekas
	// CostCategoryFinancing / akun 1-3300). RULE KLIEN (2026-09-04): kategori ini
	// direklasifikasi jadi CostCategoryOperational (beban periode, akun 5-4600),
	// jadi TIDAK PERNAH lagi diisi untuk plan RAB yang disetujui setelah tanggal
	// itu — field ini hanya dipertahankan agar snapshot/SaleRecord HPP historis
	// (dari sebelum reklasifikasi) tetap terbaca apa adanya. Jangan populate field
	// ini dari kode baru.
	Financing Money
}

// Total returns the sum of all category costs (= HPP saat unit terjual).
// Phase 7: this value drives the Dr HPP / Cr Persediaan entries in Event 4.
// Soft dan Financing disertakan supaya Total() atas snapshot HPP historis
// (sebelum masing-masing direklasifikasi) tetap benar; untuk plan baru kedua
// field itu selalu Zero (RULE KLIEN FREEZE 2026-09-04).
func (b UnitCostBreakdown) Total() Money {
	return b.Land.Add(b.Hard).Add(b.Soft).Add(b.Financing)
}

// Amount returns the breakdown's Money for a given cost class, or Zero for an
// unrecognized category. Lets callers walk AllCostCategories without hardcoding
// the field-per-class mapping (taxonomy = source of truth). AllCostCategories no
// longer includes Soft (RULE KLIEN FREEZE 2026-09-04), so this case only serves
// callers reading historical breakdowns directly by category; CostCategoryOperational
// bukan bagian breakdown kapitalisasi ini (ia beban periode) — sengaja tidak ada
// case untuknya, jatuh ke Zero.
func (b UnitCostBreakdown) Amount(c CostCategory) Money {
	switch c {
	case CostCategoryLand:
		return b.Land
	case CostCategoryHard:
		return b.Hard
	case CostCategorySoft:
		return b.Soft
	}
	return Zero
}

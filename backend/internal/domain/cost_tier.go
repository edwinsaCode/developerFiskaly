package domain

// CostTier klasifikasi 3-tier setiap biaya (blueprint-domain-additions.md §7).
// SINGLE SOURCE OF TRUTH — jangan menyebarkan literal string tier di package lain;
// selalu pakai konstanta bertipe ini.
//
// Perlakuan akuntansi per tier:
//
//	direct   → milik SATU unit (unit_id WAJIB); kapitalisasi Dr Persediaan 1-3xxx → HPP.
//	shared   → dipakai banyak unit (unit_id NULL); kapitalisasi ke pool Persediaan,
//	           dialokasikan ke unit via basis (alokasi = accounting-REAL, masuk HPP).
//	overhead → biaya perusahaan (G&A/period); Dr Beban 5-3000/5-4000. TIDAK PERNAH
//	           dikapitalisasi, TIDAK PERNAH masuk HPP. Boleh di-tag ke Project sebagai
//	           cost center reporting, boleh juga tanpa project (Tenant-level).
type CostTier string

const (
	CostTierDirect   CostTier = "direct"
	CostTierShared   CostTier = "shared"
	CostTierOverhead CostTier = "overhead"
)

// AllCostTiers adalah urutan kanonik tier untuk validasi/laporan.
var AllCostTiers = []CostTier{CostTierDirect, CostTierShared, CostTierOverhead}

// Valid returns true jika t adalah CostTier yang dikenali.
func (t CostTier) Valid() bool {
	switch t {
	case CostTierDirect, CostTierShared, CostTierOverhead:
		return true
	}
	return false
}

// Capitalizes returns true jika biaya tier ini dikapitalisasi ke Persediaan
// Real Estat (1-3xxx) dan pada akhirnya menjadi HPP. Overhead = period expense.
func (t CostTier) Capitalizes() bool {
	return t == CostTierDirect || t == CostTierShared
}

// AllowsCategory adalah matriks kompatibilitas CostTier × CostCategory:
//
//	direct/shared  → hanya kategori kapitalisasi (land|hard|soft|financing)
//	overhead       → hanya kategori beban (marketing|other)
//
// Kombinasi di luar matriks tidak sah (mis. overhead+hard akan mencemari HPP;
// direct+marketing akan mengkapitalisasi beban periode). Kedua pelanggaran itu
// membuat laba proyek & harga pokok salah permanen — tolak di validasi.
func (t CostTier) AllowsCategory(c CostCategory) bool {
	switch t {
	case CostTierDirect, CostTierShared:
		return c.IsCapitalizable()
	case CostTierOverhead:
		return c.IsExpense()
	}
	return false
}

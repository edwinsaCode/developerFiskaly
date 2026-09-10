package domain

// ═════════════════════════════════════════════════════════════════════════════
// PRODUCT CATEGORY — SATU-SATUNYA sumber kebenaran aturan partisipasi produk.
//
// Developer properti tidak hanya menjual RUMAH. Katalog produk (product_types)
// membedakan tiga kategori, dan kategori inilah business rule utamanya:
//
//	property     → ikut alokasi/HPP (seluruh pool: Land/Hard/Soft/Financing),
//	               ikut lifecycle unit properti (boleh jadi baris `units`),
//	               ikut progress fisik proyek, Akad mengakui pendapatan + HPP.
//	land         → ikut alokasi/HPP TAPI HANYA pool Land (LT-1: kategori baru,
//	               belum ada peserta yang memakainya — Kelebihan Tanah, lihat
//	               docs/kelebihan-tanah-final-architecture-2026-08.md). TIDAK
//	               PERNAH menjadi baris `units` dan TIDAK ikut progress fisik
//	               (tidak membangun apa pun).
//	non_property → barang/jasa (sambungan PDAM, biaya admin, dll):
//	               TETAP dijual, di-invoice, dibayar, dan masuk laba rugi sesuai
//	               mapping akun — tapi TIDAK PERNAH ikut alokasi HPP dan tidak
//	               ikut progress fisik proyek.
//
// Aturan "apakah produk ini ikut HPP?" HANYA boleh dijawab oleh
// ProductCategory.ParticipatesInHPP(). "Apakah produk ini boleh jadi baris
// `units`?" HANYA boleh dijawab oleh ProductCategory.MayBecomeUnit() — dua
// pertanyaan yang BERBEDA sejak `land` ada (land ikut HPP tapi tidak pernah
// jadi unit). Tidak boleh ada SQL, helper, atau konstanta lain yang memutuskan
// hal yang sama (anti duplicate source of truth).
// ═════════════════════════════════════════════════════════════════════════════

// ProductCategory mengklasifikasikan satu jenis produk tenant.
type ProductCategory string

const (
	ProductCategoryProperty    ProductCategory = "property"
	ProductCategoryLand        ProductCategory = "land"
	ProductCategoryNonProperty ProductCategory = "non_property"
)

// AllProductCategories adalah daftar lengkap kategori yang dikenal sistem.
func AllProductCategories() []ProductCategory {
	return []ProductCategory{ProductCategoryProperty, ProductCategoryLand, ProductCategoryNonProperty}
}

func (c ProductCategory) Valid() bool {
	for _, k := range AllProductCategories() {
		if c == k {
			return true
		}
	}
	return false
}

// ParticipatesInHPP adalah FUNGSI KANONIK: apakah produk berkategori ini ikut
// basis alokasi biaya proyek (pool RAB maupun biaya aktual) di SETIDAKNYA satu
// pool, dan karenanya mengakui HPP saat Akad. `land` ikut (pool Land saja —
// lihat ParticipatesInCostPool untuk rincian per-pool), `property` ikut
// seluruh pool.
//
// Kategori tidak valid → false (fail-closed: lebih baik menolak daripada
// mendilusi HPP rumah dengan produk yang tidak seharusnya).
//
// PERINGATAN: fungsi ini TIDAK LAGI ekuivalen dengan "boleh jadi baris
// `units`?" sejak `land` ada — untuk itu pakai MayBecomeUnit().
func (c ProductCategory) ParticipatesInHPP() bool {
	return c == ProductCategoryProperty || c == ProductCategoryLand
}

// ParticipatesInProjectProgress: apakah produk ikut menentukan progress fisik
// proyek (kurva pembangunan). Sambungan PDAM / kelebihan tanah tidak membangun
// apa pun — mereka tidak boleh menggeser progress. Sengaja TETAP hanya
// `property` walau `land` sekarang ikut HPP (§A kelebihan-tanah-final-architecture):
// ikut pool biaya tidak sama dengan ikut kurva pembangunan fisik.
func (c ProductCategory) ParticipatesInProjectProgress() bool {
	return c == ProductCategoryProperty
}

// ParticipatesInCostPool: apakah produk berkategori ini ikut alokasi pool
// biaya SPESIFIK ini. Lebih presisi dari ParticipatesInHPP() (yang hanya
// menjawab "ikut pool APA PUN") — dipakai allocation engine saat Compute()
// dipanggil per-pool (LT-5, belum ada pemanggil pada LT-1).
//
//	property → semua pool kapitalisasi (RULE KLIEN FREEZE 2026-09-04: Land/Hard
//	           saja — Soft/Operational bukan lagi pool kapitalisasi).
//	land     → hanya pool Land.
//	lainnya  → tidak ada pool.
func (c ProductCategory) ParticipatesInCostPool(pool CostCategory) bool {
	switch c {
	case ProductCategoryProperty:
		return pool.IsCapitalizable()
	case ProductCategoryLand:
		return pool == CostCategoryLand
	default:
		return false
	}
}

// MayBecomeUnit adalah FUNGSI KANONIK: apakah produk berkategori ini boleh
// menjadi baris `units` (dipakai validateUnitType — gerbang satu-satunya untuk
// CreateUnit/BulkCreateUnits). HANYA `property` — `land` ikut HPP (lihat
// ParticipatesInHPP) tapi TIDAK PERNAH jadi unit; ia standalone product
// (land_stock/land_sales, LT-3+), bukan baris papan penjualan. Sengaja fungsi
// terpisah dari ParticipatesInHPP agar kedua pertanyaan tidak pernah tertukar
// walau kebetulan identik sebelum `land` ada.
func (c ProductCategory) MayBecomeUnit() bool {
	return c == ProductCategoryProperty
}

// HPPEligibleCategories menurunkan daftar kategori yang ikut HPP DARI fungsi
// kanonik di atas (bukan daftar terpisah yang bisa drift). Dipakai bila sebuah
// pembaca perlu memfilter di level data.
func HPPEligibleCategories() []ProductCategory {
	out := make([]ProductCategory, 0, len(AllProductCategories()))
	for _, c := range AllProductCategories() {
		if c.ParticipatesInHPP() {
			out = append(out, c)
		}
	}
	return out
}

// ProductPolicy adalah keputusan lengkap untuk satu kode produk (unit_type):
// kategori + akun pendapatan yang dipakai saat pengakuan pendapatan. Value
// object murni — di-resolve dari master katalog oleh package project, lalu
// dikonsumsi sale/allocation/cost tanpa masing-masing menafsir ulang aturannya.
type ProductPolicy struct {
	Code               string
	Name               string
	Category           ProductCategory
	RevenueAccountCode string
	// TaxCategory (rule klien UAT #3): subsidi/komersial adalah klasifikasi
	// PRODUK (mis. kode "rumah_subsidi" vs "rumah_komersial"), bukan skema KPR
	// dan bukan cuma atribut proyek — satu proyek boleh menjual keduanya.
	// nil = produk ini tidak menentukan sendiri (proyek tetap sumber penentu,
	// mis. kode legacy "rumah" atau produk non-rumah seperti ruko/PDAM).
	TaxCategory *TaxCategory
}

// ParticipatesInHPP mendelegasikan ke fungsi kanonik kategori.
func (p ProductPolicy) ParticipatesInHPP() bool { return p.Category.ParticipatesInHPP() }

// ParticipatesInProjectProgress mendelegasikan ke fungsi kanonik kategori.
func (p ProductPolicy) ParticipatesInProjectProgress() bool {
	return p.Category.ParticipatesInProjectProgress()
}

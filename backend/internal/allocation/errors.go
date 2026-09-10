package allocation

import "errors"

var (
	// ErrInvalidBasis is returned when an unrecognized AllocationBasis is given.
	ErrInvalidBasis = errors.New("basis alokasi tidak valid: gunakan saleable_area atau sales_value")

	// ErrAllWeightsZero is returned when all unit weights are zero for the chosen basis.
	// This makes allocation impossible and must be resolved (e.g., set unit areas).
	// Breaks Invariant #3 — engine refuses rather than silently losing costs.
	ErrAllWeightsZero = errors.New("semua bobot alokasi nol: isi saleable_area atau list_price unit terlebih dahulu")

	// ErrNoUnits is returned when the unit slice is empty.
	// H-1: sejak basis alokasi disaring kategori produk, ini juga berarti
	// "proyek hanya berisi unit non-properti" — pool biaya tidak punya penerima
	// yang sah. Engine menolak, bukan membuang biaya diam-diam (Invariant #3).
	ErrNoUnits = errors.New("tidak ada unit properti dalam proyek: alokasi HPP membutuhkan minimal 1 unit berkategori property")

	// ErrUnitTypeUnregistered (H-1, fail-closed): ada unit yang unit_type-nya
	// tidak terdaftar di master product_types, sehingga sistem tidak bisa tahu
	// apakah unit itu ikut HPP. Perhitungan dibatalkan — tidak menebak.
	ErrUnitTypeUnregistered = errors.New("unit_type tidak terdaftar di katalog produk: alokasi HPP dibatalkan (daftarkan produk terlebih dahulu)")

	// ErrConfigNotFound is returned when no AllocationConfig exists for the project.
	ErrConfigNotFound = errors.New("konfigurasi alokasi belum diatur untuk proyek ini")

	// ErrProjectRequired is returned when project_id is missing.
	ErrProjectRequired = errors.New("project_id wajib diisi")

	// ErrLandStockCostExceedsPool (rule klien UAT #1/#7): carve-out tetap
	// land_stock (purchase_price × total_quantity_m2) melebihi total pool
	// biaya Land project-wide — RAB/actual belum mengakumulasi cukup biaya
	// tanah untuk menutupi harga beli tanah kelebihan yang sudah diketahui
	// pasti. Menolak, bukan menghasilkan porsi unit negatif (Invariant #3).
	ErrLandStockCostExceedsPool = errors.New("carve-out land_stock (purchase_price x luas) melebihi total pool biaya tanah project-wide")

	// ErrNoLandPoolRecipient is returned when the land cost pool has no unit
	// participant to receive the post-carve-out remainder — degenerate
	// configuration (Kelebihan Tanah tanpa satupun unit properti terdaftar).
	ErrNoLandPoolRecipient = errors.New("tidak ada unit properti untuk menerima sisa pool biaya tanah setelah carve-out land_stock")

	// ErrLandAreaMissing (Item 9, UAT 2026-09-07): HPP Tanah per unit kini
	// dialokasikan proporsional terhadap land_area unit (bukan lagi rata) —
	// fail-closed, sama seperti ErrAllWeightsZero untuk basis Hard/Soft/
	// Financing: menolak daripada diam-diam menghasilkan HPP Tanah nol untuk
	// unit yang land_area-nya belum diisi admin (units.land_area default 0,
	// opsional saat create — lihat internal/project/service.go).
	ErrLandAreaMissing = errors.New("land_area belum diisi untuk satu atau lebih unit properti: alokasi HPP Tanah per luas membutuhkan land_area > 0 di semua unit (isi lewat PATCH /units/{id}/land-area)")

	// ErrHardSubpoolNoSubsidiUnit / ErrHardSubpoolNoKomersialUnit (UAT
	// 2026-09-07): ada biaya Produksi Subsidi/Komersial yang sudah tercatat
	// (cost_entries.hard_subcategory), tapi proyek ini tidak punya satu pun
	// unit dengan TaxCategory yang cocok — pool tidak boleh diam-diam jatuh ke
	// unit lain atau hilang (Invariant #3). Perbaiki klasifikasi unit
	// (product_type/project tax_category) atau cost entry yang salah kategori.
	ErrHardSubpoolNoSubsidiUnit   = errors.New("ada biaya Produksi Subsidi tapi tidak ada unit ber-TaxCategory Subsidi di proyek ini")
	ErrHardSubpoolNoKomersialUnit = errors.New("ada biaya Produksi Komersial tapi tidak ada unit ber-TaxCategory Komersial di proyek ini")
)

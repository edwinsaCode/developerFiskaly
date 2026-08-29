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
)

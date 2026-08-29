package cost

import "errors"

var (
	// ErrCostEntryNotFound is returned when a CostEntry does not exist for the given tenant.
	ErrCostEntryNotFound = errors.New("cost entry tidak ditemukan")

	// ErrCostAmountFractional is returned when amount contains fractional sen (Invariant #2).
	ErrCostAmountFractional = errors.New("amount harus rupiah bulat: pecahan sen tidak diizinkan")

	// ErrCostAmountZeroOrNeg is returned when amount is zero or negative.
	ErrCostAmountZeroOrNeg = errors.New("amount harus lebih besar dari nol")

	// ErrInvalidCategory is returned when category is not a recognized CostCategory.
	ErrInvalidCategory = errors.New("kategori tidak valid: gunakan land, hard, soft, financing (kapitalisasi) atau marketing, other (overhead)")

	// ErrInvalidCostTier is returned when cost_tier is not direct|shared|overhead.
	ErrInvalidCostTier = errors.New("cost_tier tidak valid: gunakan direct, shared, atau overhead")

	// ErrTierCategoryMismatch is returned when the CostTier × CostCategory matrix is violated:
	// direct/shared hanya untuk kategori kapitalisasi (land|hard|soft|financing);
	// overhead hanya untuk kategori beban (marketing|other).
	ErrTierCategoryMismatch = errors.New("kombinasi cost_tier dan kategori tidak sah: direct/shared hanya untuk land|hard|soft|financing, overhead hanya untuk marketing|other")

	// ErrUnitRequiredForDirect is returned when tier=direct tanpa unit_id.
	ErrUnitRequiredForDirect = errors.New("cost_tier direct wajib menyebut unit_id (biaya milik satu unit)")

	// ErrUnitNotHPPEligible (Product Catalog): biaya direct ditempel ke unit
	// non-properti. Biaya itu tidak akan pernah lepas menjadi HPP (produk
	// non-properti tidak mengakui HPP saat BAST) sehingga akan mengendap
	// selamanya sebagai persediaan hantu di neraca.
	ErrUnitNotHPPEligible = errors.New("unit tidak ikut perhitungan HPP (produk non-properti): biaya tidak boleh dikapitalisasi ke unit ini — gunakan tier shared/overhead atau perbaiki katalog produk")

	// ErrUnitProductPolicyUnresolved (fail-closed): kebijakan produk unit tidak
	// dapat ditentukan (unit_type tak terdaftar, produk nonaktif, atau DB gagal).
	ErrUnitProductPolicyUnresolved = errors.New("kebijakan produk unit tidak dapat ditentukan: daftarkan unit_type di katalog produk terlebih dahulu")

	// ErrUnitNotAllowedForTier is returned when tier=shared/overhead menyertakan unit_id.
	ErrUnitNotAllowedForTier = errors.New("unit_id hanya untuk cost_tier direct; shared adalah pool proyek dan overhead tidak pernah teratribusi ke unit")

	// ErrPhaseRequiresProject is returned when phase_id diisi tanpa project_id.
	ErrPhaseRequiresProject = errors.New("phase_id membutuhkan project_id")

	// ErrBudgetLinkRequiresProject is returned when budget_item_id diisi tanpa project_id
	// (budget item selalu milik sebuah project).
	ErrBudgetLinkRequiresProject = errors.New("budget_item_id membutuhkan project_id (RAB selalu milik sebuah project)")

	// ErrInvalidPaymentMethod is returned when payment_method is not bank|payable.
	ErrInvalidPaymentMethod = errors.New("payment_method tidak valid: gunakan bank atau payable")

	// ErrBankAccountCodeRequired is returned when payment_method=bank but bank_account_code is empty.
	ErrBankAccountCodeRequired = errors.New("bank_account_code wajib diisi saat payment_method = bank")

	// ErrAccountNotFound is returned when a required COA account cannot be found for the tenant.
	ErrAccountNotFound = errors.New("akun tidak ditemukan di chart of accounts tenant")

	// ErrCostEntryAlreadyPosted is returned when trying to post an already-posted cost entry.
	ErrCostEntryAlreadyPosted = errors.New("jurnal cost entry sudah diposting")

	// ErrProjectRequired is returned when project_id is zero for tier direct/shared.
	// (Hanya tier overhead yang boleh tanpa project — biaya Tenant-level.)
	ErrProjectRequired = errors.New("project_id wajib diisi (hanya cost_tier overhead yang boleh tanpa project)")

	// ErrBudgetItemNotFound is returned when budget_item_id tidak ditemukan untuk tenant ini.
	ErrBudgetItemNotFound = errors.New("budget item tidak ditemukan atau bukan milik tenant ini")

	// ErrBudgetItemProjectMismatch is returned when budget_item_id milik project yang berbeda.
	ErrBudgetItemProjectMismatch = errors.New("budget item milik project yang berbeda dengan cost entry")

	// ErrBudgetItemCategoryMismatch is returned when kategori cost entry tidak cocok
	// dengan kategori budget item (construction↔hard dianggap cocok).
	ErrBudgetItemCategoryMismatch = errors.New("kategori cost entry tidak cocok dengan kategori budget item")

	// ── W-10 Transaksi Pengeluaran ──────────────────────────────────────────

	// ErrExpenseTypeUnknown (fail-closed): expense_type_id tidak dikenal untuk
	// tenant ini. Tidak ada fallback ke akun default — akun beban salah lebih
	// buruk daripada transaksi gagal.
	ErrExpenseTypeUnknown = errors.New("jenis pengeluaran tidak dikenal untuk tenant ini")

	// ErrExpenseTypeInactive: jenis pengeluaran sudah dinonaktifkan. Baris
	// historis yang memakainya tidak terpengaruh — hanya transaksi baru ditolak.
	ErrExpenseTypeInactive = errors.New("jenis pengeluaran sudah nonaktif: pilih jenis lain atau aktifkan kembali di master")

	// ErrExpenseTypeNotFound: master tidak ditemukan saat operasi admin.
	ErrExpenseTypeNotFound = errors.New("jenis pengeluaran tidak ditemukan")

	// ErrExpenseTypeInvalid: input master tidak lengkap (kode/nama kosong).
	ErrExpenseTypeInvalid = errors.New("kode dan nama jenis pengeluaran wajib diisi")

	// ErrExpenseTypeDuplicate: kode sudah dipakai tenant ini.
	ErrExpenseTypeDuplicate = errors.New("kode jenis pengeluaran sudah dipakai")

	// ErrExpenseAccountInvalid: akun tujuan tidak boleh dipakai sebagai akun
	// beban (tidak ada di COA, nonaktif, atau bukan bertipe expense).
	ErrExpenseAccountInvalid = errors.New("akun beban tidak valid: harus ada di COA tenant, aktif, dan bertipe beban")

	// ErrExpenseTypeRequired: scope operasional wajib menyebut jenis pengeluaran
	// — akun debitnya berasal dari master, bukan dari enum kategori.
	ErrExpenseTypeRequired = errors.New("jenis pengeluaran wajib dipilih untuk transaksi pengeluaran operasional")

	// ErrExpenseTypeNotForProjectCost: expense_type_id hanya untuk pengeluaran
	// operasional (tier overhead). Biaya proyek memakai taksonomi CostCategory.
	ErrExpenseTypeNotForProjectCost = errors.New("jenis pengeluaran hanya untuk biaya operasional (cost_tier overhead); biaya proyek memakai kategori RAB")

	// ErrExpenseTypeWithBudgetItem: pengeluaran operasional tidak pernah menjadi
	// realisasi item RAB — item RAB dianggarkan dalam taksonomi CostCategory.
	ErrExpenseTypeWithBudgetItem = errors.New("pengeluaran operasional tidak bisa dikaitkan ke item RAB: catat sebagai biaya proyek bila memang bagian anggaran")

	// ErrExpenseTypeTaxonomyAccount menegakkan INV-EXP-2 (turunan BD-2).
	// Realisasi RAB per kategori dibaca dari LEDGER berdasarkan kode akun
	// taksonomi (1-3xxx, 5-3000, 5-4000) dengan filter project_id. Karena itu
	// pengeluaran operasional yang akunnya kebetulan berada di himpunan itu akan
	// otomatis terhitung sebagai realisasi RAB begitu di-tag ke proyek —
	// bertentangan dengan BD-2 "project tag ≠ realisasi RAB". Ditolak di depan.
	ErrExpenseTypeTaxonomyAccount = errors.New("jenis pengeluaran ini memakai akun taksonomi biaya proyek sehingga tidak bisa di-tag ke proyek (akan terbaca sebagai realisasi RAB): catat sebagai biaya proyek, atau lepas tag proyek")

	// ErrPayableNotSupported (T-9): jalur hutang usaha belum punya mekanisme
	// pelunasan yang lengkap. Membuka payable di sini akan melahirkan saldo
	// hutang yang tidak pernah bisa dilunasi dari UI.
	ErrPayableNotSupported = errors.New("pembayaran via hutang usaha belum didukung: gunakan pembayaran tunai/bank (modul hutang usaha adalah increment terpisah)")

	// ErrNotCashBankAccount (T-5): akun pembayaran bukan kas/bank yang sah.
	ErrNotCashBankAccount = errors.New("akun pembayaran harus akun kas/bank yang aktif di COA tenant")

	// ErrUnitNotInProject: unit yang di-tag milik proyek lain.
	ErrUnitNotInProject = errors.New("unit bukan milik proyek yang dipilih")
)

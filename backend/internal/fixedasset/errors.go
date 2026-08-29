package fixedasset

import "errors"

var (
	// ── Kategori ────────────────────────────────────────────────────────────

	ErrCategoryNotFound = errors.New("kategori aset tetap tidak ditemukan")

	ErrCategoryInactive = errors.New("kategori aset tetap sudah nonaktif: pilih kategori lain atau aktifkan kembali di master")

	ErrCategoryInvalid = errors.New("kode dan nama kategori aset tetap wajib diisi")

	ErrCategoryDuplicate = errors.New("kode kategori aset tetap sudah dipakai")

	// ErrAssetAccountInvalid: akun aset (Dr saat perolehan) harus ada di COA,
	// aktif, dan bertipe asset dengan normal balance debit.
	ErrAssetAccountInvalid = errors.New("akun aset tidak valid: harus ada di COA tenant, aktif, dan bertipe asset (normal debit)")

	// ErrAccumulatedDepreciationAccountInvalid: akun kontra-aset harus ada di
	// COA, aktif, dan bertipe asset dengan normal balance KREDIT (pola 1-4900).
	ErrAccumulatedDepreciationAccountInvalid = errors.New("akun akumulasi penyusutan tidak valid: harus bertipe asset dengan normal balance kredit (kontra-aset)")

	// ErrDepreciationExpenseAccountInvalid: akun beban penyusutan harus ada di
	// COA, aktif, dan bertipe expense.
	ErrDepreciationExpenseAccountInvalid = errors.New("akun beban penyusutan tidak valid: harus ada di COA tenant, aktif, dan bertipe beban")

	// ── Perolehan aset ──────────────────────────────────────────────────────

	ErrAssetNotFound = errors.New("aset tetap tidak ditemukan")

	ErrAssetNameRequired = errors.New("nama aset wajib diisi")

	ErrAmountZeroOrNeg = errors.New("nilai perolehan harus lebih besar dari nol")

	ErrAmountFractional = errors.New("nominal harus rupiah bulat: pecahan sen tidak diizinkan")

	ErrResidualNegative = errors.New("nilai residu tidak boleh negatif")

	ErrResidualExceedsCost = errors.New("nilai residu tidak boleh melebihi nilai perolehan")

	ErrInvalidUsefulLife = errors.New("umur ekonomis (bulan) harus lebih besar dari nol")

	ErrUnsupportedDepreciationMethod = errors.New("metode penyusutan tidak didukung: v1 hanya straight_line (garis lurus)")

	ErrInvalidPaymentMethod = errors.New("payment_method tidak valid: gunakan bank")

	ErrBankAccountCodeRequired = errors.New("bank_account_code wajib diisi saat payment_method = bank")

	ErrNotCashBankAccount = errors.New("akun pembayaran harus akun kas/bank yang aktif di COA tenant")

	// ErrPayableNotSupported (v1): mengikuti kebijakan cost.ErrPayableNotSupported
	// (T-9) — hutang usaha belum punya jalur pelunasan dari pintu pengeluaran ini.
	ErrPayableNotSupported = errors.New("pembelian aset tetap via hutang usaha belum didukung di v1: gunakan pembayaran tunai/bank")

	// ── Penyusutan ──────────────────────────────────────────────────────────

	ErrAssetDisposed = errors.New("aset sudah dihentikan (disposed): tidak bisa disusutkan lagi")

	ErrInvalidPeriod = errors.New("periode tidak valid: bulan harus 1-12")

	// ErrDepreciationAlreadyPosted: baris penyusutan aset ini untuk periode
	// yang diminta sudah ada (INV-FA-1). Fail-closed di belakang UNIQUE
	// constraint DB — bukan silent skip yang menyamarkan penjadwalan ganda.
	ErrDepreciationAlreadyPosted = errors.New("penyusutan aset ini untuk periode tersebut sudah pernah diposting")

	ErrAccountNotFound = errors.New("akun tidak ditemukan di chart of accounts tenant")
)

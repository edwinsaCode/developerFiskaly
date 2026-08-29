package charge

import "errors"

var (
	ErrGroupNotFound    = errors.New("grup tagihan tidak ditemukan")
	ErrItemNotFound     = errors.New("item tagihan tidak ditemukan")
	ErrContractNotFound = errors.New("kontrak penjualan tidak ditemukan")
	ErrGroupNotOpen     = errors.New("grup tagihan tidak berstatus open")
	ErrItemNotOpen      = errors.New("item tagihan tidak berstatus open")
	ErrKindInvalid      = errors.New("kind grup harus 'realization' atau 'addon'")
	ErrLabelRequired    = errors.New("label wajib diisi")
	ErrItemsRequired    = errors.New("minimal satu item tagihan")
	ErrAmountInvalid    = errors.New("nominal harus rupiah bulat dan lebih besar dari nol")

	// K-3 — alokasi manual admin.
	ErrAllocationsRequired = errors.New("alokasi per item wajib diisi (keputusan admin — tidak ada alokasi otomatis)")
	ErrAllocationMismatch  = errors.New("total alokasi harus sama persis dengan nominal pembayaran")
	ErrAllocationDuplicate = errors.New("item yang sama muncul lebih dari sekali di alokasi")
	ErrAllocationExceeds   = errors.New("alokasi melebihi sisa tagihan item")
	ErrItemNotInGroup      = errors.New("item bukan bagian dari grup tagihan ini")

	// K-4 — disposisi sisa titipan.
	ErrExceedsResidual = errors.New("jumlah melebihi sisa dana titipan grup")
	ErrTargetGroupSame = errors.New("grup tujuan tidak boleh sama dengan grup sumber")

	// Cancel / Void / Settle.
	ErrGroupHasMoney     = errors.New("grup tidak bisa dibatalkan: sudah ada pembayaran atau payout — gunakan void/refund")
	ErrItemHasMoney      = errors.New("item tidak bisa dibatalkan: sudah ada alokasi pembayaran atau payout")
	ErrTerminNotForGroup = errors.New("pembayaran ini bukan pembayaran grup tagihan realisasi")
	ErrAlreadyVoided     = errors.New("pembayaran ini sudah pernah di-void")
	ErrVoidBreaksFunds   = errors.New("void ditolak: dana pembayaran ini sudah terpakai untuk payout/refund/transfer")
	ErrSettleBlocked     = errors.New("grup belum bisa ditutup: masih ada outstanding atau sisa titipan")

	ErrDepositAccountMissing = errors.New("akun Titipan Realisasi (2-2400) belum ada: jalankan migration 000059 / seed COA")

	// W-1 — Master Jenis Biaya Realisasi (fail-closed).
	ErrChargeTypeNotFound    = errors.New("jenis biaya realisasi tidak ditemukan")
	ErrChargeTypeUnknown     = errors.New("jenis biaya realisasi tidak terdaftar di master aktif")
	ErrChargeTypeInactive    = errors.New("jenis biaya realisasi nonaktif — tidak bisa dipakai untuk tagihan baru")
	ErrChargeTypeInvalid     = errors.New("kode dan nama jenis biaya realisasi wajib diisi")
	ErrChargeTypeDuplicate   = errors.New("kode jenis biaya realisasi sudah ada")
	ErrChargeTypeRequired    = errors.New("item tagihan realisasi wajib menunjuk jenis biaya dari master")
	ErrDepositAccountInvalid = errors.New("akun titipan jenis biaya realisasi tidak sah")

	// W-13 — Produk Tambahan menunjuk master katalog produk (fail-closed).
	ErrProductRequired   = errors.New("item produk tambahan wajib menunjuk produk dari katalog")
	ErrProductUnresolved = errors.New("produk tambahan tidak dapat ditentukan: daftarkan produknya di Katalog Produk beserta akun pendapatannya")
	// Rumah/ruko dijual sebagai UNIT — ia punya lifecycle, alokasi biaya, dan
	// HPP. Menjualnya sebagai baris addon akan melahirkan pendapatan tanpa HPP.
	ErrProductNotAddon = errors.New("produk berkategori properti dijual sebagai unit, bukan sebagai produk tambahan")
	// Wiring seam katalog produk belum terpasang — jangan diam-diam jatuh ke
	// akun default: ini jalur uang.
	ErrProductResolverMissing = errors.New("wiring tidak lengkap: katalog produk belum terpasang di modul tagihan")
	// Baris tenant tidak ada — kebijakan tidak bisa dibaca/ditulis. Fail-closed:
	// jangan pernah tafsirkan sebagai "kebijakan mati".
	ErrTenantNotFound = errors.New("tenant tidak ditemukan: kebijakan billing tidak bisa dibaca")
	ErrVendorRequired = errors.New("nama vendor wajib diisi")

	// T-1 — transfer internal WAJIB punya dokumen memo (fail-closed: lebih baik
	// transfer ditolak daripada dana berpindah tanpa jejak dokumen).
	ErrSettlementNotFound = errors.New("disposisi dana tidak ditemukan")
	ErrNotATransfer       = errors.New("disposisi ini bukan transfer internal: hanya transfer yang punya memo")
	ErrMemoNotIssued      = errors.New("transfer ini dibuat sebelum memo transfer internal diberlakukan (pra-000060)")

	// Idempotency — kunci yang sama dipakai untuk sasaran berbeda.
	ErrIdempotencyConflict = errors.New("idempotency key sudah dipakai untuk transaksi lain")
)

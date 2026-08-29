package land

import "errors"

var (
	// ErrLandStockNotFound is returned when a project has no land_stock pool yet.
	ErrLandStockNotFound = errors.New("land_stock tidak ditemukan untuk proyek ini")

	// ErrLandStockAlreadyExists is returned when creating a pool for a project
	// that already has one — satu pool per proyek (§B.1).
	ErrLandStockAlreadyExists = errors.New("land_stock sudah ada untuk proyek ini: gunakan update, bukan create")

	// ErrProjectNotFound mirrors project.ErrProjectNotFound without importing
	// the project package — land_stock.project_id is only validated via the
	// database FK, not a cross-package existence check.
	ErrProjectNotFound = errors.New("proyek tidak ditemukan")

	// ErrQuantityNegative is returned when total_quantity_m2 or unit_price is negative.
	ErrQuantityNegative = errors.New("kuantitas/harga tidak boleh negatif")

	// ErrCapacityExceeded (INV-LAND-1): reserved_quantity_m2 + sold_quantity_m2
	// baru akan melebihi total_quantity_m2 — dipakai baik saat admin mengecilkan
	// total pool maupun saat reservasi baru melebihi kuantitas yang tersedia.
	ErrCapacityExceeded = errors.New("kuantitas melebihi yang tersedia di pool (reserved+sold tidak boleh melebihi total)")

	// ErrQuantityMustBePositive is returned when a reservation's quantity_m2 is <= 0.
	ErrQuantityMustBePositive = errors.New("kuantitas reservasi harus lebih besar dari nol")

	// ErrCustomerRequired is returned when a reservation has no customer_id.
	ErrCustomerRequired = errors.New("customer wajib diisi untuk reservasi")

	// ErrCustomerNotFound mirrors a customer_id FK violation.
	ErrCustomerNotFound = errors.New("customer tidak ditemukan")

	// ErrReservationNotFound is returned when a reservation id doesn't exist for the tenant.
	ErrReservationNotFound = errors.New("reservasi tidak ditemukan")

	// ErrReservationNotActive is returned when cancelling/expiring/converting a
	// reservation that is already in a terminal state (converted/expired/cancelled).
	ErrReservationNotActive = errors.New("reservasi tidak lagi aktif")

	// ── LT-5: Akad (land_sales) ──────────────────────────────────────────────

	// ErrLandSaleNotFound is returned when a land_sale id doesn't exist for the tenant.
	ErrLandSaleNotFound = errors.New("land_sale tidak ditemukan")

	// ErrReservationQuantityMismatch is returned when an Akad references a
	// reservation but requests a different quantity_m2 than that reservation
	// holds — v1 requires an exact match (no partial conversion).
	ErrReservationQuantityMismatch = errors.New("kuantitas Akad harus sama dengan kuantitas reservasi")

	// ErrReservationCustomerMismatch is returned when an Akad's customer_id
	// doesn't match the reservation being converted.
	ErrReservationCustomerMismatch = errors.New("customer Akad tidak sama dengan customer reservasi")

	// ErrVATRateRequired is returned when is_pkp is true but vat_rate_snapshot is zero.
	ErrVATRateRequired = errors.New("tarif PPN wajib diisi jika is_pkp true")

	// ErrPaymentAccountRequired is returned when payment_account_code is empty.
	ErrPaymentAccountRequired = errors.New("akun kas/bank tujuan wajib dipilih")

	// ErrInvalidBankAccount, ErrPaymentAccountNotFound, ErrPaymentAccountInactive
	// mirror sale.Err{InvalidBankAccount,PaymentAccountNotFound,PaymentAccountInactive}
	// — COA-driven cash/bank validation, pola identik internal/sale.
	ErrInvalidBankAccount     = errors.New("akun pembayaran tidak valid: harus akun kas/bank (aset) yang aktif")
	ErrPaymentAccountNotFound = errors.New("akun pembayaran tidak ditemukan untuk tenant ini")
	ErrPaymentAccountInactive = errors.New("akun pembayaran nonaktif")

	// ErrRecognitionDateRequired is returned when Akad has no recognition date.
	ErrRecognitionDateRequired = errors.New("tanggal Akad wajib diisi")

	// ErrHPPResolverNotConfigured is returned when Service.RecordAkad is called
	// without a LandHPPResolver wired in (WithHPPResolver) — fail-closed guard,
	// pola identik sale.Service tanpa hppResolver.
	ErrHPPResolverNotConfigured = errors.New("resolver HPP tanah belum dikonfigurasi")

	// ── LT-6: pembatalan pasca-Akad (§F.3) ───────────────────────────────────

	// ErrLandSaleNotAkad is returned when cancelling a land_sale that isn't
	// currently status=akad (already cancelled, or somehow still draft).
	ErrLandSaleNotAkad = errors.New("land_sale hanya bisa dibatalkan dari status akad")

	// ErrCancelReasonRequired is returned when CancelLandSale is called with an
	// empty reason — pembatalan membalik jurnal yang sudah posted, wajib
	// beralasan untuk jejak audit (berbeda dari CloseReservation yang bukan
	// aksi finansial).
	ErrCancelReasonRequired = errors.New("alasan pembatalan wajib diisi")
)

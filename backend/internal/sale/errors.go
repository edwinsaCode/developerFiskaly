package sale

import "errors"

var (
	// ErrProductPolicyUnresolved (M-1, fail-closed): kebijakan produk unit tidak
	// bisa ditentukan — unit_type tidak terdaftar di katalog, produk nonaktif,
	// mapping akun kosong, atau DB gagal. Sebelumnya sistem diam-diam memakai
	// 4-1000 (fail-open) sehingga salah mapping tidak pernah ketahuan.
	ErrProductPolicyUnresolved = errors.New("kebijakan produk unit tidak dapat ditentukan: daftarkan unit_type di katalog produk beserta akun pendapatannya")

	ErrTerminAmountZeroOrNeg  = errors.New("jumlah termin harus lebih besar dari nol")
	ErrTerminAmountFractional = errors.New("jumlah termin harus rupiah bulat: pecahan sen tidak diizinkan")
	ErrInvalidBankAccount     = errors.New("akun pembayaran tidak valid: harus akun kas/bank (aset) yang aktif")
	ErrPaymentAccountNotFound = errors.New("akun pembayaran tidak ditemukan untuk tenant ini")
	ErrPaymentAccountInactive = errors.New("akun pembayaran nonaktif")
	ErrUnitAlreadySold        = errors.New("unit sudah terjual: tidak dapat diproses ulang")
	ErrUnitNotFound           = errors.New("unit tidak ditemukan")
	// ErrUnitNotBASTReady (Increment 6.1 / F-1): BAST hanya sah dari status yang
	// matriks lifecycle izinkan bertransisi ke sold (reserved|ppjb). Unit di
	// status lain (available/booked/hold/blocked/...) harus ditransisikan dulu.
	ErrUnitNotBASTReady          = errors.New("unit belum siap BAST: status harus reserved atau ppjb — transisikan unit terlebih dahulu")
	ErrSalePriceZeroOrNeg        = errors.New("sale_price harus lebih besar dari nol")
	ErrSalePriceFractional       = errors.New("sale_price harus rupiah bulat: pecahan sen tidak diizinkan")
	ErrAdvanceExceedsSalePrice   = errors.New("total uang muka melebihi harga jual: tidak bisa BAST")
	ErrPaymentExceedsReceivable  = errors.New("pembayaran melebihi sisa piutang pelanggan setelah BAST")
	ErrPaymentExceedsOutstanding = errors.New("nominal pembayaran melebihi sisa tagihan kontrak")
	ErrCollectionAmountInvalid   = errors.New("nominal pembayaran harus rupiah bulat dan lebih besar dari nol")
	// ErrBankFeeInvalid (UAT 2026-09-03, Rule #5): provisi/biaya admin bank yang
	// dipotong saat pencairan KPR — harus rupiah bulat, tidak boleh negatif, dan
	// tidak boleh melebihi nominal yang diselesaikan ke piutang (bank_fee tidak
	// pernah membuat penerimaan kas menjadi negatif).
	ErrBankFeeInvalid     = errors.New("bank_fee harus rupiah bulat, tidak negatif, dan tidak melebihi amount")
	ErrVATRateRequired    = errors.New("vat_rate wajib diisi dan > 0 ketika is_vat = true")
	ErrUnitRequired       = errors.New("unit_id wajib diisi")
	ErrProjectRequired    = errors.New("project_id wajib diisi")
	ErrBASTDateRequired   = errors.New("bast_date wajib diisi")
	ErrSaleRecordNotFound = errors.New("sale record tidak ditemukan")
	ErrTerminNotFound     = errors.New("termin tidak ditemukan")
	// ErrScheduleContractMismatch: schedule_id yang diminta bukan milik contract_id
	// yang dikirim — mencegah pembayaran ditarget ke cicilan kontrak lain (mis. via
	// payload yang salah suntik dari client).
	ErrScheduleContractMismatch = errors.New("schedule_id bukan milik contract_id yang diberikan")

	// Phase 7 — PaymentSchedule
	ErrContractNotFound           = errors.New("sale contract tidak ditemukan")
	ErrScheduleNotFound           = errors.New("payment schedule tidak ditemukan")
	ErrInstallmentAlreadyReceived = errors.New("installment sudah diterima: tidak bisa diproses ulang")
	ErrContractTotalPriceInvalid  = errors.New("total_price harus lebih besar dari nol")
	ErrInvalidPaymentType         = errors.New("payment_type tidak valid: gunakan 'kpr' atau 'tunai'")
	ErrContractStoreNotConfigured = errors.New("ContractStore belum dikonfigurasi di service")

	// FE-3 — Buyer Credit Lifecycle (apply credit)
	ErrNoCreditAvailable      = errors.New("tidak ada saldo kredit buyer yang tersedia")
	ErrCreditExceedsAvailable = errors.New("jumlah melebihi saldo kredit buyer yang tersedia")
	ErrScheduleOverpaid       = errors.New("jumlah melebihi sisa tagihan cicilan: cicilan akan overpaid")
	ErrScheduleAlreadyPaid    = errors.New("cicilan sudah lunas: tidak ada sisa untuk ditutup")
	ErrCreditAmountFractional = errors.New("jumlah kredit harus rupiah bulat: pecahan sen tidak diizinkan")
	ErrCreditAmountZeroOrNeg  = errors.New("jumlah kredit harus lebih besar dari nol")

	// Increment 3 — Payment Scheme (strategy seam)
	ErrSchemeFlowNotConfigured            = errors.New("payment scheme flow belum dikonfigurasi di service")
	ErrPaymentSchemeRequired              = errors.New("payment_scheme_id wajib untuk kontrak baru")
	ErrCustomerRequiredForContract        = errors.New("customer_id wajib untuk kontrak baru (kebijakan Increment 1)")
	ErrSalesPersonRequiredForContract     = errors.New("sales_person_id wajib untuk kontrak baru (kebijakan Increment 1)")
	ErrContractHasNoScheme                = errors.New("kontrak legacy tanpa payment scheme: operasi ini butuh kontrak ber-scheme")
	ErrSchemeEventNotManual               = errors.New("event ini tidak boleh diposting manual (otomatis dari pembayaran/BAST, atau menunggu domain Cancellation)")
	ErrFinancingSourceRequiredForTakeover = errors.New("takeover wajib menyebut financing_source_id bank baru")
	ErrScheduleSumMismatch                = errors.New("total jadwal tidak sama persis dengan nilai kontrak")
	ErrScheduleItemsRequired              = errors.New("items jadwal wajib diisi")
	ErrActiveScheduleExists               = errors.New("jadwal aktif sudah ada: gunakan schedule-regenerate untuk mengganti")
	ErrFinancingSourceOnNonKPRPayment     = errors.New("financing_source_id pada pembayaran hanya untuk pencairan KPR (source=kpr_disbursement)")
	// R1 KPR Realization: pencairan bank hanya sah setelah akad kredit tercatat.
	ErrDisbursementRequiresAkad  = errors.New("pencairan bank membutuhkan akad kredit tercatat — catat akad terlebih dahulu di milestone KPR")
	ErrDisbursementNeedsContract = errors.New("pencairan bank membutuhkan kontrak (tidak bisa dicatat pada unit tanpa kontrak)")

	// Increment 7 — Booking
	ErrBookingStoreNotConfigured = errors.New("booking store belum dikonfigurasi di service")
	ErrBookingNotFound           = errors.New("booking tidak ditemukan")
	ErrBookingNotActive          = errors.New("booking sudah terminal (converted/expired/cancelled): tidak bisa diproses")
	ErrActiveBookingExists       = errors.New("unit sudah punya booking aktif: batalkan/konversikan dulu")
	ErrBookingFeeInvalid         = errors.New("booking_fee harus rupiah bulat dan tidak boleh negatif")
	ErrBookingExpiryInvalid      = errors.New("expiry_date harus setelah booking_date")
	ErrBookingCustomerRequired   = errors.New("customer_id wajib untuk booking (kebijakan Increment 1)")
	ErrBookingUnitStateInvalid   = errors.New("status unit tidak sesuai untuk operasi booking ini")
	ErrBookingUnitMismatch       = errors.New("unit kontrak tidak sama dengan unit booking")
	ErrBookingCustomerMismatch   = errors.New("customer kontrak tidak sama dengan customer booking")
	ErrTitipanAccountMissing     = errors.New("akun Titipan Booking (2-2100) belum ada: jalankan migration 000044 / seed COA")
	// Rule klien 2026-07-29 — fee = Pendapatan Booking (4-2100)
	ErrBookingRevenueAccountMissing = errors.New("akun Pendapatan Booking (4-2100) belum ada: jalankan migration 000053 / seed COA")
	// LEGACY R4 — disposisi fee held pasca-konversi (hanya baris histori)
	ErrFeeNotDisposable            = errors.New("fee booking tidak bisa didisposisi: hanya baris legacy converted dengan fee yang masih held (booking baru = pendapatan final)")
	ErrInvalidFeeDispositionAction = errors.New("aksi disposisi fee tidak dikenal: gunakan forfeit atau refund")

	// Item 3 (2026-09) — Transfer booking ke unit lain (TANPA jurnal baru).
	ErrBookingTransferSameUnit = errors.New("unit tujuan sama dengan unit booking saat ini")

	// P0-2/P0-3 — Budgeted Cost Allocation (HPP snapshot saat BAST)
	// ErrAllocationBasisMissing: proyek punya RAB aktif tapi basis alokasi
	// (saleable_area/sales_value) belum diatur — tidak bisa menurunkan HPP
	// budgeted (gate BCA-2).
	ErrAllocationBasisMissing = errors.New("basis alokasi belum diatur: tetapkan saleable_area atau sales_value sebelum BAST dengan RAB")

	// P0-4 D2 — proyek sudah finalized tapi true-up belum posted: BAST unit baru
	// harus menunggu act_HPP finalized (no live recalculation after completion).
	ErrTrueupNotPostedForBAST = errors.New("proyek sudah difinalisasi: selesaikan & posting HPP true-up sebelum BAST unit berikutnya")

	// Billing Batch 2 (K-2): kebijakan tenant require_realization_settled aktif
	// dan unit masih punya outstanding Biaya Realisasi → BAST ditolak.

	// Temuan #7 — serah terima fisik (RecordPhysicalHandover)
	// ErrUnitNotHandoverReady: serah terima fisik hanya sah dari status `sold`
	// (Akad sudah tercatat). Unit di status lain harus melalui Akad dulu.
	ErrUnitNotHandoverReady        = errors.New("unit belum siap serah terima fisik: Akad harus sudah tercatat (status sold) terlebih dahulu")
	ErrHandoverWriterNotConfigured = errors.New("HandoverWriter belum dikonfigurasi di service")

	// kelebihan-tanah-booking-integration-2026-08 — Produk Tambahan Kelebihan
	// Tanah, komponen opsional pada Booking/Kontrak.
	// ErrLandAkadPreparerNotConfigured: kontrak punya komponen tanah tapi
	// seam-nya belum di-wire — menolak fail-closed, BUKAN diam-diam
	// melewatkan pengakuan pendapatan+HPP tanahnya.
	ErrLandAkadPreparerNotConfigured  = errors.New("LandAkadPreparer belum dikonfigurasi di service: kontrak punya komponen Kelebihan Tanah tapi tidak bisa diproses")
	ErrLandComponentRequiresLandStock = errors.New("komponen Kelebihan Tanah butuh land_stock_id, land_reservation_id, dan quantity yang lengkap")
	ErrLandQuantityInvalid            = errors.New("land_quantity_m2 harus lebih besar dari nol")

	// Item 7C (UAT 2026-09-07): pencairan KPR dibatasi ke sisa Dana Jaminan
	// Bank (Nilai Persetujuan KPR Bank dikurangi pencairan sebelumnya) —
	// bukan sisa piutang unit gabungan. Kelebihan tidak pernah diam-diam
	// dialihkan ke Piutang Usaha.
	ErrDisbursementExceedsFinancing = errors.New("pencairan melebihi sisa Dana Jaminan Bank kontrak")

	// Item 7A (UAT 2026-09-07): Nilai Persetujuan KPR Bank kini diisi SAAT
	// AKAD (bukan saat pembuatan kontrak) untuk kontrak ber-scheme KPR —
	// dasar pemisahan Dana Jaminan Bank vs Piutang Usaha di Event 3.
	ErrBankApprovedAmountRequired   = errors.New("nilai persetujuan KPR Bank wajib diisi untuk kontrak KPR sebelum Akad")
	ErrBankApprovedAmountFractional = errors.New("nilai persetujuan KPR Bank harus rupiah bulat: pecahan sen tidak diizinkan")
	ErrBankApprovedAmountZeroOrNeg  = errors.New("nilai persetujuan KPR Bank harus lebih besar dari nol")
)

package tax

import "errors"

var (
	ErrTransferValueFractional = errors.New("nilai pengalihan harus rupiah bulat: pecahan sen tidak diizinkan")
	ErrTransferValueZeroOrNeg  = errors.New("nilai pengalihan harus lebih besar dari nol")
	ErrObligationNotFound      = errors.New("kewajiban pajak tidak ditemukan")
	ErrObligationAlreadyPaid   = errors.New("kewajiban pajak sudah dilunasi")
	ErrPaymentAmountMismatch   = errors.New("jumlah pembayaran harus sama persis dengan kewajiban pajak")
	ErrInvalidBankAccount      = errors.New("bank_account_code tidak valid: harus akun kas/bank (aset) yang aktif menurut COA")
	ErrRateNotConfigured       = errors.New("tarif PPh Final belum dikonfigurasi untuk tenant ini: jalankan seed atau POST /tax/tax-rates terlebih dahulu")

	// Increment 4 — Tax Rule konfiguratif
	ErrAppliesToInvalid       = errors.New("applies_to tidak valid: gunakan all, subsidi, atau komersial")
	ErrEffectiveRangeInvalid  = errors.New("effective_to tidak boleh sebelum effective_from")
	ErrRuleAccountsIncomplete = errors.New("debit_account dan credit_account harus diisi berpasangan (atau keduanya kosong untuk default)")
	ErrProjectNotFoundForTax  = errors.New("proyek tidak ditemukan untuk resolusi kategori pajak")
	ErrInvalidTaxCategory     = errors.New("tax_category tidak valid: gunakan subsidi atau komersial")
	ErrTriggerEventInvalid    = errors.New("trigger_event tidak valid: gunakan bast, invoice, atau payment")
	ErrFormulaNotImplemented  = errors.New("formula pajak belum terimplementasi")
	ErrFormulaInvalid         = errors.New("formula tidak valid: proportional, progressive, threshold, fixed_amount, atau exemption")
	ErrRateCodeRequired        = errors.New("rate_code wajib diisi")
	ErrRateValueInvalid        = errors.New("rate harus antara 0 (eksklusif) dan 1 (inklusif)")
	ErrEffectiveDateRequired   = errors.New("effective_from wajib diisi")
	ErrUnitRequired            = errors.New("unit_id wajib diisi")
	ErrObligationAmountZero    = errors.New("tax_amount tidak boleh nol")

	// Phase 8 — laporan PPN & penjaga
	ErrLedgerReaderNotConfigured = errors.New("LedgerAccountReader belum dikonfigurasi — pasang WithLedgerReader")
	ErrBASTReaderNotConfigured   = errors.New("BASTReader belum dikonfigurasi — pasang WithBASTReader")
)

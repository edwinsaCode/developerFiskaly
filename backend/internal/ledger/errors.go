package ledger

import "errors"

var (
	// ErrJournalNotBalanced is returned when Σ debit ≠ Σ credit. Invariant #1.
	ErrJournalNotBalanced = errors.New("jurnal tidak seimbang: Σ debit ≠ Σ kredit")

	// ErrJournalAlreadyPosted is returned when a mutation is attempted on a posted journal.
	// Posted journals are immutable (Invariant #5). Corrections via reversing entry only.
	ErrJournalAlreadyPosted = errors.New("jurnal sudah diposting dan tidak bisa diubah (gunakan jurnal pembalik)")

	// ErrJournalNotPosted is returned when trying to reverse an unposted journal.
	ErrJournalNotPosted = errors.New("jurnal belum diposting")

	// ErrJournalAlreadyReversed is returned when the journal already has a reversing entry.
	ErrJournalAlreadyReversed = errors.New("jurnal sudah memiliki entri pembalik")

	// ErrLineInvalid is returned when a journal line has both debit and credit, or neither.
	ErrLineInvalid = errors.New("baris jurnal tidak valid: harus memiliki debit ATAU kredit, tidak keduanya")

	// ErrLineNegative is returned when a debit or credit amount is negative.
	ErrLineNegative = errors.New("baris jurnal tidak valid: jumlah tidak boleh negatif")

	// ErrTooFewLines is returned when a journal has fewer than 2 lines.
	ErrTooFewLines = errors.New("jurnal harus memiliki minimal 2 baris")

	// ErrAmountNotWholeRupiah is returned when a line amount has fractional sen. Invariant #2.
	// Auto-rounding is intentionally refused — rounding before posting is the caller's responsibility.
	ErrAmountNotWholeRupiah = errors.New("jumlah baris jurnal bukan rupiah bulat: pecahan sen tidak diizinkan")

	// ErrAccountNotFound is returned when an account ID does not exist for the tenant.
	ErrAccountNotFound = errors.New("akun tidak ditemukan")

	// ErrAccountInactive is returned when a payment account is not active.
	ErrAccountInactive = errors.New("akun nonaktif")

	// ErrNotCashBankAccount is returned when an account is not an asset cash/bank account.
	ErrNotCashBankAccount = errors.New("akun bukan kas/bank")

	// ErrInvalidCategory is returned when an account category value is invalid.
	ErrInvalidCategory = errors.New("kategori akun tidak valid")

	// ErrAccountCodeDuplicate is returned when creating an account with an existing code.
	ErrAccountCodeDuplicate = errors.New("kode akun sudah digunakan")

	// ErrPeriodClosed is returned when a journal date falls in a closed accounting period.
	ErrPeriodClosed = errors.New("periode akuntansi sudah ditutup")

	// ErrPeriodNotFound is returned when the period record does not exist.
	ErrPeriodNotFound = errors.New("periode akuntansi tidak ditemukan")

	// ErrPeriodAlreadyClosed is returned when trying to close an already-closed period.
	ErrPeriodAlreadyClosed = errors.New("periode akuntansi sudah ditutup")

	// ErrPeriodNotClosed is returned when trying to reopen an open period.
	ErrPeriodNotClosed = errors.New("periode akuntansi belum ditutup")

	// ── Tutup buku tahunan (year-end closing) ────────────────────────────────

	// ErrYearAlreadyClosed is returned when closing entries already exist for the year.
	ErrYearAlreadyClosed = errors.New("tahun buku sudah ditutup: entri penutup sudah ada")

	// ErrNothingToClose is returned when there is no revenue/expense to close for the year.
	ErrNothingToClose = errors.New("tidak ada pendapatan atau beban untuk ditutup pada tahun ini")

	// ErrClosingAccountsMissing is returned when the Income Summary (3-3000) or
	// Retained Earnings (3-2000) account is not found for the tenant.
	ErrClosingAccountsMissing = errors.New("akun penutup (Ikhtisar Laba Rugi 3-3000 / Laba Ditahan 3-2000) tidak ditemukan")

	// ── Dokumen kas (W-3.2, INV-DOC-1) ───────────────────────────────────────

	// ErrDocumentTypeRequired — jurnal manual menggerakkan kas keluar tanpa
	// menyatakan jenis buktinya (D-W3-4). Pilihannya menentukan siapa pemilik
	// uang yang keluar, jadi tidak boleh ditebak sistem.
	ErrDocumentTypeRequired = errors.New("jenis dokumen kas wajib dipilih")

	// ErrDocumentTypeMismatch — jenis dokumen yang dipilih bertentangan dengan
	// arah pergerakan kas jurnalnya.
	ErrDocumentTypeMismatch = errors.New("jenis dokumen tidak sesuai arah pergerakan kas")

	// ErrDocumentRequired — penegakan INV-DOC-1 (W-3.5): jurnal yang menggerakkan
	// kas selesai diposting tanpa dokumen bernomor yang tertaut. Posting
	// dibatalkan seluruhnya; kas tidak boleh bergerak tanpa bukti.
	ErrDocumentRequired = errors.New("jurnal kas wajib memiliki dokumen bernomor (INV-DOC-1)")
)

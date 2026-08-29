package project

import "errors"

var (
	// ErrProjectNotFound is returned when a project does not exist for the given tenant.
	ErrProjectNotFound = errors.New("proyek tidak ditemukan")
	// ErrInvalidTaxCategory: tax_category harus subsidi|komersial (Increment 4).
	ErrInvalidTaxCategory = errors.New("tax_category tidak valid: gunakan subsidi atau komersial")

	// ErrProjectInvalidTransition is returned when a requested status transition is not allowed.
	ErrProjectInvalidTransition = errors.New("transisi status proyek tidak valid")

	// ErrPhaseNotFound is returned when a project phase does not exist.
	ErrPhaseNotFound = errors.New("fase proyek tidak ditemukan")

	// ErrPhaseInvalidTransition is returned when a phase status transition is not allowed.
	ErrPhaseInvalidTransition = errors.New("transisi status fase tidak valid")

	// ErrUnitNotFound is returned when a unit does not exist for the given tenant.
	ErrUnitNotFound = errors.New("unit tidak ditemukan")

	// ErrUnitInvalidTransition is returned when a unit status transition is not allowed.
	// Example: available → sold (must go through reserved first).
	ErrUnitInvalidTransition = errors.New("transisi status unit tidak valid: periksa urutan available → reserved → sold")

	// ErrUnitCodeDuplicate is returned when a unit code already exists within the project.
	ErrUnitCodeDuplicate = errors.New("kode unit sudah digunakan dalam proyek ini")

	// ErrListPriceFractional is returned when list_price contains fractional sen (Invariant #2).
	ErrListPriceFractional = errors.New("list_price harus rupiah bulat: pecahan sen tidak diizinkan")

	// UAT Batch 2 §4 — Bulk Unit Generator.
	ErrBulkBlockRequired  = errors.New("block dan unit_type wajib diisi")
	ErrBulkRangeInvalid   = errors.New("rentang unit tidak valid: unit_start >= 1 dan unit_end >= unit_start")
	ErrBulkTooMany        = errors.New("jumlah unit melebihi batas satu batch")
	ErrBulkDuplicateCode  = errors.New("kode unit sudah ada di proyek ini — seluruh batch dibatalkan")

	// ErrSalePriceFractional is returned when sale_price contains fractional sen (Invariant #2).
	ErrSalePriceFractional = errors.New("sale_price harus rupiah bulat: pecahan sen tidak diizinkan")

	// ErrUnknownTransitionEvent is returned when an unrecognized event is supplied (Increment 6).
	ErrUnknownTransitionEvent = errors.New("event transisi tidak dikenal")

	// ErrEventTransitionMismatch (Increment 6.1 / F-4): event yang dikirim tidak
	// koheren dengan pasangan (from → to) — mis. bast_executed pada
	// available → reserved. Audit trail wajib jujur soal sebab transisi.
	ErrEventTransitionMismatch = errors.New("event tidak sesuai dengan transisi yang diminta")

	// ErrUnitTransitionConflict is returned when the unit's status changed between
	// read and write (optimistic guard) or the unit vanished — retry after re-read.
	ErrUnitTransitionConflict = errors.New("transisi unit gagal: status berubah bersamaan atau unit tidak ditemukan")

	// ErrPostBASTCancellation is returned when a manual transition tries to move a
	// unit out of `sold` (sold → available). Pasca-BAST butuh jurnal pembalik —
	// domain Cancellation (Increment 8). Matriks menyediakan transisinya, tetapi
	// endpoint manual menolaknya sampai Cancellation dibangun (D2).
	ErrPostBASTCancellation = errors.New("pembatalan pasca-BAST butuh domain Cancellation (jurnal pembalik) — belum tersedia")

	// ErrApprovalRequired is returned when a governance-gated transition (hold/blocked)
	// lacks an APPROVED approval request. Adapter menerjemahkan approval.ErrApprovalRequired
	// ke sentinel ini agar package project tidak bergantung pada package approval.
	ErrApprovalRequired = errors.New("transisi ini membutuhkan approval yang disetujui (workflow unit_transition aktif)")

	// ErrLandAreaNegative (LT-2): land_area adalah luas — negatif tidak berarti
	// apa pun secara fisik, ditolak fail-closed (beda dari nol, yang sah = belum diisi).
	ErrLandAreaNegative = errors.New("land_area tidak boleh negatif")
)

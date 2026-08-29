package closing

import "errors"

var (
	ErrProjectNotFound    = errors.New("proyek tidak ditemukan")
	ErrCompletionNotFound = errors.New("completion event tidak ditemukan untuk proyek ini")
	// ErrInvalidStateTransition (IMPL-3): transisi state machine ilegal.
	ErrInvalidStateTransition = errors.New("transisi state tidak valid")
	// ErrCompletionNotFinalized: true-up hanya boleh calculate setelah completion FINALIZED.
	ErrCompletionNotFinalized = errors.New("completion belum finalized: finalisasi biaya aktual dulu sebelum calculate true-up")
	// ErrMixedAllocationBasisVersion (IMPL-1): snapshot terjual dalam scope memakai
	// >1 version basis — tolak, minta scope dipisah (tidak ada auto-merge).
	ErrMixedAllocationBasisVersion = errors.New("snapshot dalam scope memakai lebih dari satu versi basis alokasi: pisahkan scope per fase")
	ErrRunNotFound                 = errors.New("true-up run tidak ditemukan")
	// ErrNoBasisConfigured: basis alokasi proyek belum diatur (tidak bisa
	// menurunkan act_HPP).
	ErrNoBasisConfigured = errors.New("basis alokasi proyek belum diatur")
	// ErrTrueupNotPosted (D2): proyek finalized tapi belum ada run posted —
	// BAST unit baru diblokir sampai act_HPP finalized tersedia.
	ErrTrueupNotPosted = errors.New("belum ada true-up run yang posted untuk proyek finalized ini")
	// ErrNoSoldUnits: true-up tanpa satu pun unit terjual ber-snapshot tidak
	// bermakna (tidak ada HPP budgeted untuk direkonsiliasi).
	ErrNoSoldUnits = errors.New("tidak ada unit terjual ber-snapshot budgeted dalam scope ini")
)

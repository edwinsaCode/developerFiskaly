package budget

import "errors"

var (
	ErrPlanNotFound      = errors.New("budget plan tidak ditemukan")
	ErrItemNotFound      = errors.New("budget item tidak ditemukan")
	ErrPlanNotDraft      = errors.New("hanya budget plan berstatus draft yang bisa diedit atau dihapus itemnya")
	ErrPlanNotApprovable = errors.New("hanya budget plan berstatus draft yang bisa di-approve")
	ErrNoItemsToApprove  = errors.New("budget plan tidak bisa di-approve tanpa item — tambahkan minimal satu item terlebih dahulu")
	ErrInvalidCategory   = errors.New("kategori tidak valid: gunakan land|construction|soft|operational|marketing|other")
	ErrAmountFractional  = errors.New("budgeted_amount harus rupiah bulat: pecahan sen tidak diizinkan")
	ErrAmountZeroOrNeg   = errors.New("budgeted_amount harus lebih besar dari nol")
	ErrProjectRequired   = errors.New("project_id wajib diisi")
	ErrLabelRequired     = errors.New("label wajib diisi")
	ErrNoActivePlan      = errors.New("tidak ada budget plan active untuk proyek/fase ini")

	// UAT 2026-09-07: RAB Konstruksi wajib subkategori kanonik agar HPP
	// Produksi Subsidi dan Produksi Komersial tidak tercampur satu pool.
	ErrInvalidConstructionSubcategory = errors.New("subcategory Konstruksi wajib salah satu dari: produksi_subsidi|produksi_komersial|sarana_prasarana|perizinan")
)

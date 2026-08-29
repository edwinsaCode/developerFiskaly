package scheme

import "errors"

var (
	// ErrInvalidParams: params JSON tidak valid / nilai di luar rentang.
	ErrInvalidParams = errors.New("parameter payment scheme tidak valid")

	// ErrUnknownPolicyType: policy_type tidak terdaftar di registry.
	ErrUnknownPolicyType = errors.New("policy_type tidak dikenali: gunakan cash, cash_installment, kpr, atau inhouse")

	// ErrInvalidTransition: event tidak sah dari state saat ini (state machine).
	ErrInvalidTransition = errors.New("transisi scheme tidak sah dari state saat ini")

	// ErrBASTGateNotMet: syarat gate BAST scheme belum terpenuhi.
	ErrBASTGateNotMet = errors.New("gate BAST scheme belum terpenuhi")

	// ErrFinancingSourceRequired: scheme KPR wajib menyebut financing source (bank).
	ErrFinancingSourceRequired = errors.New("financing_source_id wajib untuk scheme KPR")

	// ErrFinancingSourceNotAllowed: scheme non-KPR tidak memakai financing source.
	ErrFinancingSourceNotAllowed = errors.New("financing_source_id hanya untuk scheme KPR")

	// ErrInterestNotSupported: bunga cicilan belum didukung (seam BS-6).
	ErrInterestNotSupported = errors.New("interest_rate_pct belum didukung: jadwal harus zero-interest (TODO(tax-advisor))")

	// ── Master errors ─────────────────────────────────────────────────────────

	ErrSchemeNotFound       = errors.New("payment scheme tidak ditemukan")
	ErrSchemeCodeDup        = errors.New("kode payment scheme sudah dipakai")
	ErrSchemeInactive       = errors.New("payment scheme tidak aktif")
	ErrSchemeCodeRequired   = errors.New("code dan name payment scheme wajib diisi")
	ErrFinSourceNotFound    = errors.New("financing source tidak ditemukan")
	ErrFinSourceCodeDup     = errors.New("kode financing source sudah dipakai")
	ErrFinSourceCodeReq     = errors.New("code dan name financing source wajib diisi")
	ErrFinSourceInvalidType = errors.New("type financing source tidak valid: bank_kpr_subsidi, bank_kpr_komersial, atau lainnya")
)

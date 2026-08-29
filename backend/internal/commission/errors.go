package commission

import "errors"

var (
	ErrRuleNotFound     = errors.New("commission rule tidak ditemukan")
	ErrRuleInvalid      = errors.New("commission rule tidak valid")
	ErrBasisNotImplemented = errors.New("basis komisi ini belum diimplementasikan (seam): daftar formula dulu")
	ErrTriggerNotImplemented = errors.New("trigger komisi ini belum diimplementasikan (seam): baru at_bast")
	ErrNotFound              = errors.New("commission tidak ditemukan")
	ErrInvalidStateTransition = errors.New("transisi status komisi tidak valid")
	ErrBankAccountInvalid     = errors.New("akun bank tidak valid: harus akun kas/bank aktif")
	// ErrApprovalRequired: gate opt-in Generic Approval Workflow (TargetCommission).
	ErrApprovalRequired = errors.New("komisi ini membutuhkan approval yang disetujui (workflow commission aktif)")
)

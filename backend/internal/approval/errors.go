package approval

import "errors"

var (
	// Config
	ErrWorkflowNotFound      = errors.New("approval workflow tidak ditemukan")
	ErrWorkflowNameRequired  = errors.New("name workflow wajib diisi")
	ErrTargetTypeInvalid     = errors.New("target_type tidak dikenali")
	ErrStepsRequired         = errors.New("workflow butuh minimal satu step")
	ErrStepRoleRequired      = errors.New("approver_role wajib diisi pada setiap step")
	ErrStepSeqInvalid        = errors.New("seq step harus berurutan mulai 1 tanpa duplikat")
	ErrStepQuorumInvalid     = errors.New("quorum step harus >= 1")
	ErrWorkflowActiveExists  = errors.New("sudah ada workflow aktif untuk target_type ini: nonaktifkan dulu atau ganti")
	ErrStepMinAmountNegative = errors.New("min_amount step tidak boleh negatif")

	// Runtime
	ErrRequestNotFound        = errors.New("approval request tidak ditemukan")
	ErrNoActiveWorkflow       = errors.New("tidak ada approval workflow aktif untuk target_type ini")
	ErrActiveRequestExists    = errors.New("sudah ada approval request aktif untuk dokumen ini")
	ErrRequestTerminal        = errors.New("approval request sudah final: tidak bisa diproses lagi")
	ErrActorRoleNotAllowed    = errors.New("role Anda bukan approver untuk step ini")
	ErrActorAlreadyActed      = errors.New("Anda sudah memberi keputusan pada step ini")
	ErrDecisionInvalid        = errors.New("decision tidak valid: gunakan approve atau reject")
	ErrOnlyRequesterCanCancel = errors.New("hanya pemohon yang boleh membatalkan request")
	// H4 seam: vocabulary delegate ada, eksekusinya menyusul (Approval Matrix P2).
	ErrDelegateNotImplemented = errors.New("delegate belum diimplementasikan: vocabulary/seam tersedia, eksekusi menyusul")

	// Gate (dipakai modul konsumen — mis. RAB)
	ErrApprovalRequired = errors.New("dokumen ini membutuhkan approval request yang APPROVED sebelum diproses (workflow aktif terkonfigurasi)")
)

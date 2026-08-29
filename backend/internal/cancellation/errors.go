package cancellation

import "errors"

var (
	ErrNotFound                 = errors.New("cancellation tidak ditemukan")
	ErrUnitNotFound             = errors.New("unit tidak ditemukan")
	ErrActiveCancellationExists = errors.New("unit sudah punya cancellation aktif")
	ErrInvalidStateTransition   = errors.New("transisi status cancellation tidak valid")
	// ErrUnitStageInvalid: unit tidak dalam kondisi yang bisa dibatalkan
	// (pra-BAST butuh reserved|ppjb ber-uang-masuk; pasca-BAST butuh sold).
	ErrUnitStageInvalid = errors.New("status unit tidak sesuai untuk pembatalan")
	// ErrNothingToCancel: tidak ada uang diterima & tidak ada BAST — tidak ada
	// yang perlu dibatalkan secara akuntansi (gunakan transisi unit manual).
	ErrNothingToCancel        = errors.New("tidak ada penerimaan/pengakuan untuk dibatalkan: gunakan transisi unit biasa")
	ErrPenaltyInvalid         = errors.New("penalti harus rupiah bulat dan ≥ 0")
	ErrPenaltyExceedsReceived = errors.New("penalti melebihi total dana yang diterima")
	// ErrTaxAlreadyPaid: PPh Final unit sudah disetor ke kas negara — pembalikan
	// otomatis tidak aman. Butuh penanganan restitusi manual.
	// TODO(tax-advisor): prosedur restitusi/pemindahbukuan PPh Final atas
	// pembatalan setelah setor (PER-mekanisme Pbk?).
	ErrTaxAlreadyPaid = errors.New("PPh Final unit ini sudah disetor: pembatalan otomatis diblokir — tangani restitusi pajak secara manual dulu")
	// ErrApprovalRequired: gate opt-in Generic Approval Workflow (TargetCancellation).
	ErrApprovalRequired = errors.New("pembatalan ini membutuhkan approval yang disetujui (workflow cancellation aktif)")

	ErrRefundNotFound         = errors.New("refund tidak ditemukan")
	ErrRefundNotPending       = errors.New("refund sudah terminal (paid/cancelled)")
	ErrNothingToRefund        = errors.New("tidak ada dana untuk direfund")
	ErrBookingNotRefundable   = errors.New("booking tidak dalam status pending_refund")
	ErrRefundAlreadyRequested = errors.New("refund untuk sumber ini sudah ada")
	ErrBankAccountInvalid     = errors.New("akun bank tidak valid: harus akun kas/bank aktif")
)

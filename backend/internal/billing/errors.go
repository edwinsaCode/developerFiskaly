package billing

import "errors"

var (
	ErrInvoiceNotFound          = errors.New("invoice tidak ditemukan")
	ErrScheduleNotFound         = errors.New("jadwal pembayaran tidak ditemukan")
	ErrContractNotFound         = errors.New("kontrak tidak ditemukan")
	ErrInvoiceAlreadyExists     = errors.New("invoice sudah ada untuk jadwal ini")
	// R1 KPR Realization:
	ErrNoOutstanding          = errors.New("tidak ada kekurangan pembayaran — outstanding kontrak 0")
	ErrShortfallInvoiceExists = errors.New("masih ada invoice kekurangan yang belum diselesaikan untuk kontrak ini")
	// Billing Batch 2 — invoice REALISASI per charge group.
	ErrChargeInvoiceExists = errors.New("masih ada invoice realisasi yang belum diselesaikan untuk grup tagihan ini")
	ErrChargeNoOutstanding = errors.New("tidak ada tagihan tersisa — outstanding grup 0")
	ErrScheduleAlreadyReceived  = errors.New("jadwal sudah dibayar — buat invoice baru tidak diperlukan")
	ErrScheduleContractMismatch = errors.New("jadwal tidak termasuk dalam kontrak ini")
	ErrTerminNotFound           = errors.New("transaksi pembayaran tidak ditemukan")
	ErrReceiptNotFound          = errors.New("kwitansi tidak ditemukan")
)

package legacyar

import "errors"

// Teks error di sini SAMPAI KE LAYAR admin apa adanya (handler menuliskannya ke
// body respons, frontend menampilkannya). Karena itu tidak ada prefiks nama
// package: "legacyar: berkas ini sudah pernah diimpor" memberi tahu pembacanya
// satu hal yang tidak berguna baginya dan membuat pesan terbaca seperti bocoran
// internal. Konvensinya sama dengan internal/charge.
var (
	ErrBatchNotFound      = errors.New("batch tidak ditemukan")
	ErrBatchNotDraft      = errors.New("batch sudah tidak berstatus draft")
	ErrBatchDuplicate     = errors.New("berkas ini sudah pernah diimpor")
	ErrNoValidRows        = errors.New("tidak ada baris yang bisa diimpor")
	ErrHasErrorRows       = errors.New("masih ada baris bermasalah — perbaiki berkasnya lalu unggah ulang")
	ErrReceivableNotFound = errors.New("piutang lama tidak ditemukan")
	ErrPaymentNotFound    = errors.New("pelunasan tidak ditemukan")
	ErrPaymentVoided      = errors.New("pelunasan ini sudah dibatalkan")
	ErrAmountNotPositive  = errors.New("nominal harus lebih dari nol")
	ErrOverpayment        = errors.New("pembayaran melebihi sisa piutang")
	ErrAlreadySettled     = errors.New("piutang ini sudah lunas")
	ErrWrittenOff         = errors.New("piutang ini sudah dihapusbukukan")
	ErrControlAccount     = errors.New("akun kontrol bukan akun piutang yang sah")
	ErrCashAccount        = errors.New("akun penerimaan harus akun kas/bank")
	ErrExternalRefTaken   = errors.New("nomor rujukan sudah dipakai piutang lain")
	ErrOpeningNotAllowed  = errors.New("jurnal saldo awal hanya boleh dibuat saat masih ada selisih")
	ErrCounterAccount     = errors.New("akun lawan saldo awal harus dipilih dari bagan akun")
)

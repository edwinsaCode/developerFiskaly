package histfin

import "errors"

var (
	ErrSnapshotNotFound = errors.New("snapshot keuangan tahun tersebut belum ada")
	ErrSnapshotExists   = errors.New("snapshot untuk tahun tersebut sudah ada")

	// Tahun buku.
	ErrYearInvalid = errors.New("tahun buku tidak masuk akal (gunakan 1980–2100)")
	ErrYearFuture  = errors.New("tahun buku belum berjalan: snapshot historis hanya untuk tahun yang sudah lewat atau tahun berjalan")
	// Guard utama W-6: tahun yang bukunya sudah hidup di sistem ini tidak boleh
	// punya snapshot manual — laporannya sudah dihasilkan dari ledger. Jurnal
	// saldo awal dikecualikan (ia menyatakan posisi pembuka, bukan aktivitas).
	ErrYearHasJournals = errors.New("tahun ini sudah punya jurnal terposting: laporannya dihasilkan dari buku besar, bukan snapshot manual")

	// Status.
	ErrSnapshotFinal    = errors.New("snapshot berstatus final: buka kembali dulu sebelum mengubah angka")
	ErrSnapshotNotFinal = errors.New("snapshot belum final")
	ErrSnapshotEmpty    = errors.New("snapshot belum berisi satu baris pun")
	ErrNotBalanced      = errors.New("neraca belum seimbang: total aset harus sama dengan kewajiban + ekuitas + laba/rugi tahun berjalan")
	ErrReasonRequired   = errors.New("alasan wajib diisi minimal 10 karakter")

	// Baris.
	ErrAccountNotFound  = errors.New("akun tidak ditemukan di bagan akun tenant ini")
	ErrAccountTypeUnkn  = errors.New("tipe akun tidak dikenal: tidak bisa ditentukan masuk Neraca atau Laba Rugi")
	ErrDuplicateAccount = errors.New("akun yang sama muncul lebih dari sekali")
	// Laba/rugi tahun berjalan adalah HASIL HITUNG (pendapatan − beban), bukan
	// angka yang diketik. Menerimanya sebagai baris berarti membiarkan neraca
	// "seimbang" dengan angka yang saling meniadakan.
	ErrComputedAccount = errors.New("akun ikhtisar laba rugi tidak boleh diisi manual: laba/rugi tahun berjalan dihitung dari pendapatan − beban")
)

package ap

import (
	"errors"
	"fmt"

	"esaproperti/internal/domain"
)

// Kesalahan domain hutang usaha.
//
// Aturan yang dipegang seluruh paket: setiap penolakan menyebut APA yang salah
// dan APA yang harus dilakukan. Pesan seperti "invalid request" memaksa
// pemakainya menebak, dan yang ditebak adalah uang.

var (
	// ── Vendor ───────────────────────────────────────────────────────────────

	ErrVendorNotFound = errors.New("vendor tidak ditemukan")
	ErrVendorInactive = errors.New("vendor sudah dinonaktifkan: aktifkan kembali bila memang masih bertransaksi")
	ErrVendorNameReq  = errors.New("nama vendor wajib diisi")

	// ── Nominal tagihan ──────────────────────────────────────────────────────

	ErrDPPZeroOrNeg = errors.New("nilai pekerjaan (DPP) harus lebih besar dari nol")
	ErrAmountFrac   = errors.New("nominal harus bilangan rupiah bulat")
	ErrNegAmount    = errors.New("nominal tidak boleh negatif")

	// ErrLineSumMismatch menegakkan INV-AP-8. Kalau Σ baris biaya tidak sama
	// dengan DPP, salah satu dari dua hal pasti terjadi: biaya yang diakui tidak
	// sama dengan yang ditagih, atau realisasi RAB menyimpang dari buku besar.
	// Keduanya tidak menghasilkan error di kemudian hari — hanya angka berbeda
	// di dua laporan. Ditolak di depan.
	ErrLineSumMismatch = errors.New("jumlah baris biaya tidak sama dengan nilai DPP tagihan")
	ErrNoLines         = errors.New("tagihan harus punya minimal satu baris biaya")

	// ErrLineAccountNotTaxonomy adalah sisi lain dari INV-AP-8. Σ baris boleh
	// saja sama dengan DPP, tetapi kalau salah satu baris mendarat di akun yang
	// TIDAK dibaca sebagai realisasi RAB, buku besar mencatat biaya yang tak
	// pernah muncul di laporan RAB. Selisihnya tidak pernah memunculkan error;
	// karena itu ditolak di depan.
	ErrLineAccountNotTaxonomy = errors.New("baris biaya mendarat di akun yang tidak terbaca sebagai realisasi RAB")
	// ErrLineCreditMismatch: dua akun kewajiban berbeda dalam satu tagihan
	// berarti sebagian nilainya berdiri di akun kontrol yang tidak dilacak
	// modul ini — dan tidak akan pernah ikut terbayar lewat jalur AP.
	ErrLineCreditMismatch = errors.New("baris biaya menghasilkan akun kewajiban yang berbeda-beda dalam satu tagihan")
	// ErrComposeUnbalanced adalah jaring pengaman komposisi. PostingService juga
	// menolak jurnal tak seimbang, tetapi pesannya tidak menyebut komponen mana
	// yang tidak cocok — dan komponen itulah yang perlu diperbaiki.
	ErrComposeUnbalanced = errors.New("komposisi jurnal tagihan tidak seimbang")

	// ── PPN (D-16) ───────────────────────────────────────────────────────────

	// ErrPPNRequiresPKP: PPN Masukan hanya lahir dari vendor Pengusaha Kena
	// Pajak. Mencatatnya dari vendor non-PKP berarti mengklaim kredit pajak yang
	// tidak ada dasarnya.
	ErrPPNRequiresPKP = errors.New("PPN Masukan hanya untuk vendor PKP: periksa status PKP vendor atau kosongkan nilai PPN")
	// ErrFakturRequired: PPN tanpa nomor faktur pajak tidak dapat dikreditkan.
	ErrFakturRequired = errors.New("nomor faktur pajak wajib diisi bila tagihan memuat PPN Masukan")
	// ErrPPNProjectTagged menegakkan INV-AP-17. Akun 1-5100 bukan akun taksonomi
	// biaya, tetapi memberi tag proyek pada barisnya tetap salah secara makna —
	// dan bila suatu saat taksonomi berubah, tag itulah yang membuat PPN
	// terhitung sebagai realisasi RAB.
	ErrPPNProjectTagged = errors.New("baris PPN Masukan tidak boleh diberi tag proyek: PPN bukan biaya proyek")

	// ── Retensi (D-6) ────────────────────────────────────────────────────────

	// ErrRetentionExceedsDPP: retensi adalah bagian DARI nilai pekerjaan.
	// Menahan lebih dari nilainya berarti kewajiban ke vendor menjadi negatif.
	ErrRetentionExceedsDPP  = errors.New("nilai retensi tidak boleh melebihi nilai pekerjaan (DPP)")
	ErrRetentionDueRequired = errors.New("tanggal jatuh tempo retensi wajib diisi bila ada retensi")
	ErrRetentionNotFound    = errors.New("tagihan ini tidak memuat retensi")
	ErrRetentionSettled     = errors.New("retensi tagihan ini sudah dilepas seluruhnya")

	// ── Yang BELUM aktif pada tahap ini ──────────────────────────────────────
	//
	// Retensi dan kompensasi uang muka sudah lengkap di komposisi jurnal dan
	// diuji di sana — tetapi PELEPASAN retensi dan PEMBENTUKAN uang muka adalah
	// pengeluaran kas yang jalurnya belum dibangun. Menerima keduanya sekarang
	// berarti melahirkan kewajiban 2-1100 dan konsumsi 1-5300 yang tidak punya
	// cara sah untuk diselesaikan. Ditutup di pintu masuk, bukan dibiarkan
	// menghasilkan saldo yang mengendap.
	ErrRetentionNotEnabled = errors.New("retensi belum bisa dicatat: jalur pelepasan retensi belum tersedia")
	ErrAdvanceNotEnabled   = errors.New("kompensasi uang muka belum bisa dicatat: jalur uang muka vendor belum tersedia")

	// ── Daur hidup ───────────────────────────────────────────────────────────

	ErrInvoiceNotFound  = errors.New("tagihan tidak ditemukan")
	ErrInvoiceNotDraft  = errors.New("hanya tagihan berstatus draft yang bisa diposting")
	ErrInvoiceNotPosted = errors.New("tagihan belum diposting: kewajibannya belum lahir di buku besar")
	ErrInvoiceReversed  = errors.New("tagihan sudah dibalik")
	// ErrHasAllocations menegakkan INV-AP-6. Membalik pengakuan sementara
	// pembayarannya masih berdiri akan meninggalkan kas yang keluar untuk
	// kewajiban yang tidak pernah ada.
	ErrHasAllocations = errors.New("tagihan sudah menerima pembayaran: balikkan pembayarannya lebih dulu, baru pengakuannya")

	// ── Pembayaran ───────────────────────────────────────────────────────────

	ErrPaymentNotFound = errors.New("pembayaran tidak ditemukan")
	ErrPaymentReversed = errors.New("pembayaran sudah dibalik")
	ErrCashAccount     = errors.New("akun pembayaran harus akun kas/bank yang aktif di COA tenant")
	ErrPaymentKind     = errors.New("jenis pembayaran tidak dikenal")
	// ErrOverpayment menegakkan INV-AP-2 & D-18. Sengaja MENOLAK, bukan
	// menampung diam-diam: kelebihan bayar yang otomatis menjadi aset akan
	// menyembunyikan salah ketik nominal sampai rekonsiliasi berikutnya.
	ErrOverpayment = errors.New("pembayaran melebihi sisa tagihan")
	// ErrExcessNeedsFlag: jalan keluar yang SAH untuk kelebihan bayar, tetapi
	// harus disengaja — bukan efek samping.
	ErrExcessNeedsFlag = errors.New("kelebihan bayar hanya bisa dicatat sebagai uang muka vendor bila diminta secara eksplisit")
	ErrNothingToPay    = errors.New("tidak ada sisa tagihan yang bisa dibayar")

	// ErrNoAllocations: pembayaran tanpa alokasi adalah kas yang keluar tanpa
	// kewajiban yang berkurang. Karena alokasi adalah SATU-SATUNYA sumber
	// kebenaran sisa tagihan (D-12), pembayaran seperti itu tidak akan pernah
	// terbaca di sisa tagihan mana pun — uang hilang tanpa jejak di AP.
	ErrNoAllocations  = errors.New("pembayaran harus punya minimal satu alokasi ke tagihan")
	ErrAllocationZero = errors.New("nilai alokasi harus lebih besar dari nol")
	// ErrAllocationSumMismatch: Σ alokasi yang tidak sama dengan nilai pembayaran
	// berarti kas yang keluar berbeda dari kewajiban yang berkurang.
	ErrAllocationSumMismatch = errors.New("jumlah alokasi tidak sama dengan nilai pembayaran")
	// ErrPaymentTouchesCost menegakkan INV-AP-3. Bila jurnal pembayaran
	// menyentuh akun taksonomi biaya, membayar tagihan akan menambah realisasi
	// RAB untuk kedua kalinya — dan selisihnya tidak pernah memunculkan error.
	ErrPaymentTouchesCost = errors.New("jurnal pembayaran tidak boleh menyentuh akun taksonomi biaya")
	// ErrInvoiceWrongVendor: satu pembayaran adalah satu kas keluar ke SATU
	// vendor. Mengalokasikannya ke tagihan vendor lain membuat saldo hutang per
	// vendor tidak lagi berarti apa pun.
	ErrInvoiceWrongVendor = errors.New("tagihan milik vendor lain: satu pembayaran hanya untuk satu vendor")
	ErrDuplicateAllocTgt  = errors.New("tagihan yang sama dialokasikan lebih dari sekali dalam satu pembayaran")

	// ── Uang muka (D-8) ──────────────────────────────────────────────────────

	ErrAdvanceNotFound = errors.New("uang muka vendor tidak ditemukan")
	// ErrAdvanceExhausted menegakkan INV-AP-12 & menutup R-12: saldo uang muka
	// yang sama tidak boleh dikompensasi dua kali. Dijaga dengan mengunci baris
	// pembayaran uang muka yang menjadi sumbernya, bukan hanya tagihannya.
	ErrAdvanceExhausted   = errors.New("saldo uang muka vendor tidak mencukupi untuk kompensasi ini")
	ErrAdvanceWrongVendor = errors.New("uang muka milik vendor lain: kompensasi hanya boleh ke tagihan vendor yang sama")

	// ── Umum ─────────────────────────────────────────────────────────────────

	ErrPeriodClosed = errors.New("periode akuntansi sudah ditutup: gunakan tanggal pada periode terbuka atau buka kembali periodenya")
	// ErrIdempotencyConflict: kunci yang sama dipakai untuk permintaan yang
	// berbeda. Mengembalikan hasil lama akan menyembunyikan permintaan kedua;
	// menjalankannya akan membayar dua kali. Keduanya salah — jadi ditolak.
	ErrIdempotencyConflict = errors.New("kunci idempotensi sudah dipakai untuk permintaan yang berbeda")
	ErrAccountNotFound     = errors.New("akun COA tidak ditemukan di tenant ini")
	// ErrApprovalRequired menegakkan INV-AP-19. OPT-IN: hanya muncul pada tenant
	// yang memang mengaktifkan workflow persetujuan untuk dokumen biaya.
	ErrApprovalRequired = errors.New("tagihan belum disetujui: selesaikan persetujuan sebelum diposting")
	ErrInvoiceNumberReq = errors.New("nomor tagihan vendor wajib diisi")
	ErrDueBeforeInvoice = errors.New("tanggal jatuh tempo tidak boleh mendahului tanggal tagihan")
)

// OverpaymentError adalah ErrOverpayment yang MEMBAWA angkanya (D-18).
//
// Alasannya praktis: penolakan "pembayaran melebihi sisa tagihan" tanpa
// menyebutkan sisa yang sebenarnya memaksa operator menutup dialog, membuka
// tagihannya, membaca angkanya, lalu mengulang — dan pada percobaan kedua
// angkanya bisa sudah berubah lagi. Sisa yang dikirim di sini adalah sisa yang
// dibaca DI BAWAH KUNCI, jadi ia angka yang berlaku pada saat penolakan.
type OverpaymentError struct {
	// InvoiceID & InvoiceNumber kosong bila penolakannya atas TOTAL sisa vendor
	// (mode otomatis), bukan atas satu tagihan tertentu.
	InvoiceID     uint64
	InvoiceNumber string
	Outstanding   domain.Money
	Requested     domain.Money
}

// Excess adalah selisih yang tidak punya kewajiban untuk dituju.
func (e *OverpaymentError) Excess() domain.Money { return e.Requested.Sub(e.Outstanding) }

func (e *OverpaymentError) Error() string {
	if e.InvoiceNumber != "" {
		return fmt.Sprintf("%s: tagihan %s bersisa %s, diminta %s (kelebihan %s)",
			ErrOverpayment.Error(), e.InvoiceNumber,
			e.Outstanding.String(), e.Requested.String(), e.Excess().String())
	}
	return fmt.Sprintf("%s: sisa seluruh tagihan vendor %s, diminta %s (kelebihan %s)",
		ErrOverpayment.Error(), e.Outstanding.String(), e.Requested.String(), e.Excess().String())
}

// Unwrap membuat errors.Is(err, ErrOverpayment) tetap bekerja, sehingga
// pemetaan status HTTP tidak perlu tahu ada dua bentuk error yang sama artinya.
func (e *OverpaymentError) Unwrap() error { return ErrOverpayment }

//go:build integration

package ap_test

// W-11 Tahap 4 — PEMBAYARAN tagihan vendor terhadap MySQL nyata.
//
// Yang dibuktikan di sini tidak bisa dibuktikan unit test:
//
//   - sisa tagihan benar-benar TURUN dari sub-ledger alokasi, bukan dari kolom;
//   - kas benar-benar tidak bergerak tanpa BKK bernomor (INV-DOC-1);
//   - pembayaran benar-benar tidak menambah realisasi RAB (INV-AP-3/4) —
//     dibaca lewat pembaca RAB produksi, bukan query khusus test;
//   - pembalikan benar-benar append-only: jurnal asli tetap posted;
//   - kunci idempotensi benar-benar ditegakkan oleh unique key MySQL;
//   - dan R-12: dua transaksi yang berebut sisa tagihan yang sama TIDAK bisa
//     sama-sama lolos. Yang terakhir hanya bisa dibuktikan dengan dua koneksi
//     sungguhan yang berjalan bersamaan — mock tidak punya lock manager.
//
// Prasyarat: TEST_DB_DSN di-set + DB termigrasi (>= 000073).

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"esaproperti/internal/ap"
	"esaproperti/internal/domain"
	"esaproperti/internal/ledger"
)

// ── Helper ──────────────────────────────────────────────────────────────────

// tagihanTerposting membuat satu tagihan proyek dan langsung mempostingnya —
// keadaan awal untuk semua uji pembayaran, karena hanya tagihan terposting yang
// punya kewajiban untuk dibayar.
func (f *w11Fixture) tagihanTerposting(t *testing.T, no, amount string) *ap.Invoice {
	t.Helper()
	inv, err := f.svc.RecordInvoice(context.Background(), w11Tenant, f.tagihanProyek(t, no, amount))
	if err != nil {
		t.Fatalf("catat tagihan %s: %v", no, err)
	}
	posted, err := f.svc.PostInvoice(context.Background(), w11Tenant, inv.ID)
	if err != nil {
		t.Fatalf("posting tagihan %s: %v", no, err)
	}
	return posted
}

// sisaTagihan membaca sisa lewat pembaca produksi — angka yang sama yang dilihat
// layar. Kalau turunannya salah, test ini yang harus gagal.
func (f *w11Fixture) sisaTagihan(t *testing.T, invoiceID uint64) domain.Money {
	t.Helper()
	v, err := f.svc.GetInvoice(context.Background(), w11Tenant, invoiceID)
	if err != nil {
		t.Fatalf("baca tagihan %d: %v", invoiceID, err)
	}
	return w11Money(t, v.Outstanding)
}

func (f *w11Fixture) statusBayar(t *testing.T, invoiceID uint64) ap.PaymentStatus {
	t.Helper()
	v, err := f.svc.GetInvoice(context.Background(), w11Tenant, invoiceID)
	if err != nil {
		t.Fatalf("baca tagihan %d: %v", invoiceID, err)
	}
	return v.PaymentStatus
}

// rencanaQuery mengembalikan rencana eksekusi MySQL sebuah query sebagai JSON.
//
// Dipakai untuk membuktikan pembacaan TERKUNCI berjalan lewat indeks. Ini bukan
// uji performa: kunci yang diambil sebuah query mengikuti baris yang DIPINDAI,
// bukan baris yang cocok. Pemindaian penuh atas ap_payment_allocations akan
// mengunci alokasi SELURUH tagihan — pembayaran atas tagihan yang sama sekali
// tidak berhubungan ikut antre, dan "aman dari lomba" berubah menjadi "satu
// pembayaran pada satu waktu untuk seluruh tenant".
func (f *w11Fixture) rencanaQuery(t *testing.T, query string, args ...any) string {
	t.Helper()
	var plan string
	if err := f.db.Raw("EXPLAIN FORMAT=JSON "+query, args...).Row().Scan(&plan); err != nil {
		t.Fatalf("EXPLAIN: %v", err)
	}
	return plan
}

func bayar(vendorID uint64, amount domain.Money, cash string) ap.CreatePaymentRequest {
	return ap.CreatePaymentRequest{
		VendorID: vendorID, Kind: ap.PaymentKindInvoice,
		PaymentDate: time.Now(), Amount: amount, CashAccountCode: cash,
	}
}

// ── 1. Alur utuh: pratinjau → bayar → sisa turun → BKK terbit ───────────────

func TestPayAlurUtuh(t *testing.T) {
	f := w11Setup(t)
	ctx := context.Background()

	inv := f.tagihanTerposting(t, "INV/PAY/001", "100000000")
	if got := f.sisaTagihan(t, inv.ID).String(); got != "100000000" {
		t.Fatalf("sisa awal = %s, mau 100000000", got)
	}
	if s := f.statusBayar(t, inv.ID); s != ap.PayStatusUnpaid {
		t.Fatalf("status awal = %s, mau unpaid", s)
	}

	// Pratinjau tidak boleh menulis apa pun.
	prev, err := f.svc.PreviewPayment(ctx, w11Tenant, bayar(f.vendorPKP, w11Money(t, "40000000"), "1-1300"))
	if err != nil {
		t.Fatalf("pratinjau: %v", err)
	}
	if prev.Amount != "40000000" || len(prev.Allocations) != 1 {
		t.Fatalf("pratinjau salah: %s / %d alokasi", prev.Amount, len(prev.Allocations))
	}
	if prev.Allocations[0].OutstandingAfter != "60000000" {
		t.Fatalf("sisa sesudah di pratinjau = %s, mau 60000000", prev.Allocations[0].OutstandingAfter)
	}
	if prev.TotalOutstanding != "100000000" {
		t.Fatalf("total sisa vendor = %s, mau 100000000", prev.TotalOutstanding)
	}
	var nPay int64
	f.db.Raw("SELECT COUNT(*) FROM ap_payments WHERE tenant_id = ?", w11Tenant).Scan(&nPay)
	if nPay != 0 {
		t.Fatalf("pratinjau menulis %d pembayaran ke DB", nPay)
	}
	if f.sisaTagihan(t, inv.ID).String() != "100000000" {
		t.Fatal("pratinjau mengubah sisa tagihan")
	}

	// Pembayaran sebagian.
	res, err := f.svc.RecordPayment(ctx, w11Tenant, bayar(f.vendorPKP, w11Money(t, "40000000"), "1-1300"))
	if err != nil {
		t.Fatalf("bayar: %v", err)
	}
	if res.Replayed {
		t.Fatal("pembayaran pertama ditandai replayed")
	}
	if res.Payment.JournalEntryID == 0 {
		t.Fatal("pembayaran tanpa jurnal")
	}

	// INV-DOC-1: kas bergerak, jadi WAJIB ada BKK bernomor.
	if res.DocumentNumber == "" || res.Payment.DocumentNumber == "" {
		t.Fatalf("pembayaran tanpa nomor dokumen: %q", res.DocumentNumber)
	}
	if !strings.HasPrefix(res.DocumentNumber, "BKK/") {
		t.Fatalf("nomor dokumen = %q, mau berawalan BKK/", res.DocumentNumber)
	}
	var docs int64
	f.db.Raw(`SELECT COUNT(*) FROM documents d
		JOIN journal_entries je ON je.document_id = d.id
		WHERE d.tenant_id = ? AND je.id = ? AND d.document_type_code = 'cash_out'`,
		w11Tenant, res.Payment.JournalEntryID).Scan(&docs)
	if docs != 1 {
		t.Fatalf("BKK atas jurnal pembayaran = %d, mau tepat 1", docs)
	}

	// Buku besar: hutang usaha berkurang, bank berkurang.
	if got, want := f.saldoAkun(t, "2-1000").String(), "-60000000"; got != want {
		t.Fatalf("hutang usaha = %s, mau %s", got, want)
	}
	if got, want := f.saldoAkun(t, "1-1300").String(), "-40000000"; got != want {
		t.Fatalf("saldo bank = %s, mau %s", got, want)
	}

	// Sisa TURUN dari sub-ledger.
	if got := f.sisaTagihan(t, inv.ID).String(); got != "60000000" {
		t.Fatalf("sisa setelah bayar = %s, mau 60000000", got)
	}
	if s := f.statusBayar(t, inv.ID); s != ap.PayStatusPartial {
		t.Fatalf("status = %s, mau partial", s)
	}

	// INV-AP-3/4: membayar tidak menambah realisasi RAB sedikit pun.
	if got, want := f.realisasiItem(t).String(), "100000000"; got != want {
		t.Fatalf("realisasi RAB setelah bayar = %s, mau tetap %s", got, want)
	}

	// Pelunasan.
	if _, err := f.svc.RecordPayment(ctx, w11Tenant, bayar(f.vendorPKP, w11Money(t, "60000000"), "1-1300")); err != nil {
		t.Fatalf("pelunasan: %v", err)
	}
	if got := f.sisaTagihan(t, inv.ID).String(); got != "0" {
		t.Fatalf("sisa setelah lunas = %s, mau 0", got)
	}
	if s := f.statusBayar(t, inv.ID); s != ap.PayStatusPaid {
		t.Fatalf("status = %s, mau paid", s)
	}
	if s := f.saldoAkun(t, "2-1000"); !s.IsZero() {
		t.Fatalf("hutang usaha setelah lunas = %s, mau nol", s)
	}

	// Riwayat pembayaran tagihan terbaca lengkap.
	rows, err := f.svc.ListInvoicePayments(ctx, w11Tenant, inv.ID)
	if err != nil {
		t.Fatalf("riwayat pembayaran: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("riwayat = %d baris, mau 2", len(rows))
	}
	for _, r := range rows {
		if r.DocumentNumber == "" {
			t.Fatalf("baris riwayat tanpa nomor BKK: %+v", r)
		}
	}
}

// ── 2. Jurnal pembayaran seimbang & tidak menyentuh akun biaya ──────────────

func TestPayJurnalSeimbangDanBersihDariAkunBiaya(t *testing.T) {
	f := w11Setup(t)
	ctx := context.Background()

	inv := f.tagihanTerposting(t, "INV/PAY/BAL", "75000000")
	res, err := f.svc.RecordPayment(ctx, w11Tenant, bayar(f.vendorPKP, w11Money(t, "75000000"), "1-1100"))
	if err != nil {
		t.Fatalf("bayar: %v", err)
	}
	_ = inv

	// Invariant #1 dibuktikan pada baris yang BENAR-BENAR tersimpan.
	var debit, credit *string
	if err := f.db.Raw(`
		SELECT COALESCE(SUM(debit),0), COALESCE(SUM(credit),0)
		FROM journal_lines WHERE tenant_id = ? AND journal_entry_id = ?`,
		w11Tenant, res.Payment.JournalEntryID).Row().Scan(&debit, &credit); err != nil {
		t.Fatalf("baca baris jurnal: %v", err)
	}
	if *debit != *credit {
		t.Fatalf("jurnal pembayaran tidak seimbang: debit %s, kredit %s", *debit, *credit)
	}

	// INV-AP-3 pada data nyata: tidak satu pun baris mendarat di akun taksonomi
	// biaya, dan tidak satu pun diberi tag proyek/unit/fase.
	var kode []string
	if err := f.db.Raw(`
		SELECT a.code FROM journal_lines jl JOIN accounts a ON a.id = jl.account_id
		WHERE jl.tenant_id = ? AND jl.journal_entry_id = ?`,
		w11Tenant, res.Payment.JournalEntryID).Scan(&kode).Error; err != nil {
		t.Fatalf("baca akun jurnal: %v", err)
	}
	if len(kode) != 2 {
		t.Fatalf("baris jurnal = %d, mau 2", len(kode))
	}
	for _, c := range kode {
		if strings.HasPrefix(c, "1-3") || strings.HasPrefix(c, "5-") {
			t.Fatalf("jurnal pembayaran menyentuh akun biaya %s", c)
		}
	}
	var tagged int64
	f.db.Raw(`SELECT COUNT(*) FROM journal_lines WHERE tenant_id = ? AND journal_entry_id = ?
		AND (project_id IS NOT NULL OR unit_id IS NOT NULL OR phase_id IS NOT NULL)`,
		w11Tenant, res.Payment.JournalEntryID).Scan(&tagged)
	if tagged != 0 {
		t.Fatalf("%d baris jurnal pembayaran diberi tag proyek", tagged)
	}
}

// ── 3. Akun pembayaran wajib kas/bank ───────────────────────────────────────

func TestPayAkunPembayaranWajibKasBank(t *testing.T) {
	f := w11Setup(t)
	ctx := context.Background()
	f.tagihanTerposting(t, "INV/PAY/ACC", "10000000")

	// 2-1000 (hutang usaha) bukan sumber dana.
	_, err := f.svc.RecordPayment(ctx, w11Tenant, bayar(f.vendorPKP, w11Money(t, "10000000"), "2-1000"))
	if !errors.Is(err, ap.ErrCashAccount) {
		t.Fatalf("bayar dari akun kewajiban: error = %v, mau ErrCashAccount", err)
	}
	// Akun yang tidak ada.
	_, err = f.svc.RecordPayment(ctx, w11Tenant, bayar(f.vendorPKP, w11Money(t, "10000000"), "9-9999"))
	if err == nil {
		t.Fatal("bayar dari akun yang tidak ada harus ditolak")
	}
	// Tidak boleh ada jejak.
	var n int64
	f.db.Raw("SELECT COUNT(*) FROM ap_payments WHERE tenant_id = ?", w11Tenant).Scan(&n)
	if n != 0 {
		t.Fatalf("%d pembayaran tertinggal setelah penolakan", n)
	}
	if s := f.saldoAkun(t, "1-1300"); !s.IsZero() {
		t.Fatalf("saldo bank bergerak padahal pembayaran ditolak: %s", s)
	}
}

// ── 4. Kelebihan bayar ditolak (D-18) ───────────────────────────────────────

func TestPayKelebihanBayarDitolak(t *testing.T) {
	f := w11Setup(t)
	ctx := context.Background()

	inv := f.tagihanTerposting(t, "INV/PAY/OVER", "10000000")

	_, err := f.svc.RecordPayment(ctx, w11Tenant, bayar(f.vendorPKP, w11Money(t, "12000000"), "1-1300"))
	if !errors.Is(err, ap.ErrOverpayment) {
		t.Fatalf("error = %v, mau ErrOverpayment", err)
	}
	var over *ap.OverpaymentError
	if !errors.As(err, &over) || over.Excess().String() != "2000000" {
		t.Fatalf("angka kelebihan tidak terbawa: %v", err)
	}
	if f.sisaTagihan(t, inv.ID).String() != "10000000" {
		t.Fatal("penolakan mengubah sisa tagihan")
	}
	if s := f.saldoAkun(t, "1-1300"); !s.IsZero() {
		t.Fatalf("kas bergerak padahal ditolak: %s", s)
	}
}

// ── 5. Alokasi eksplisit & tagihan vendor lain ──────────────────────────────

func TestPayAlokasiEksplisit(t *testing.T) {
	f := w11Setup(t)
	ctx := context.Background()

	a := f.tagihanTerposting(t, "INV/PAY/EX-A", "30000000")
	b := f.tagihanTerposting(t, "INV/PAY/EX-B", "20000000")

	req := bayar(f.vendorPKP, w11Money(t, "25000000"), "1-1300")
	req.Allocations = []ap.AllocationInput{
		{InvoiceID: a.ID, Amount: w11Money(t, "5000000")},
		{InvoiceID: b.ID, Amount: w11Money(t, "20000000")},
	}
	if _, err := f.svc.RecordPayment(ctx, w11Tenant, req); err != nil {
		t.Fatalf("bayar eksplisit: %v", err)
	}
	if got := f.sisaTagihan(t, a.ID).String(); got != "25000000" {
		t.Fatalf("sisa A = %s, mau 25000000", got)
	}
	if got := f.sisaTagihan(t, b.ID).String(); got != "0" {
		t.Fatalf("sisa B = %s, mau 0", got)
	}

	// Tagihan yang sama dua kali dalam satu dokumen: hampir selalu salah isi.
	dup := bayar(f.vendorPKP, w11Money(t, "2000000"), "1-1300")
	dup.Allocations = []ap.AllocationInput{
		{InvoiceID: a.ID, Amount: w11Money(t, "1000000")},
		{InvoiceID: a.ID, Amount: w11Money(t, "1000000")},
	}
	if _, err := f.svc.RecordPayment(ctx, w11Tenant, dup); !errors.Is(err, ap.ErrDuplicateAllocTgt) {
		t.Fatalf("alokasi ganda: error = %v, mau ErrDuplicateAllocTgt", err)
	}
}

func TestPayTagihanVendorLainDitolak(t *testing.T) {
	f := w11Setup(t)
	ctx := context.Background()

	milikPKP := f.tagihanTerposting(t, "INV/PAY/VEN", "10000000")

	req := bayar(f.vendorNonPKP, w11Money(t, "5000000"), "1-1300")
	req.Allocations = []ap.AllocationInput{{InvoiceID: milikPKP.ID, Amount: w11Money(t, "5000000")}}
	if _, err := f.svc.RecordPayment(ctx, w11Tenant, req); !errors.Is(err, ap.ErrInvoiceWrongVendor) {
		t.Fatalf("error = %v, mau ErrInvoiceWrongVendor", err)
	}
	if f.sisaTagihan(t, milikPKP.ID).String() != "10000000" {
		t.Fatal("sisa tagihan berubah")
	}
}

// Tagihan draft belum menjadi kewajiban siapa pun — membayarnya berarti
// memindahkan kas atas hutang yang belum lahir di buku besar.
func TestPayTagihanDraftTidakBisaDibayar(t *testing.T) {
	f := w11Setup(t)
	ctx := context.Background()

	inv, err := f.svc.RecordInvoice(ctx, w11Tenant, f.tagihanProyek(t, "INV/PAY/DRAFT", "10000000"))
	if err != nil {
		t.Fatalf("catat tagihan: %v", err)
	}

	// Mode otomatis: draft tidak masuk daftar yang bisa dibayar sama sekali.
	if _, err := f.svc.RecordPayment(ctx, w11Tenant,
		bayar(f.vendorPKP, w11Money(t, "1000000"), "1-1300")); !errors.Is(err, ap.ErrNothingToPay) {
		t.Fatalf("mode otomatis: error = %v, mau ErrNothingToPay", err)
	}
	// Mode eksplisit: ditolak dengan sebab yang jelas.
	req := bayar(f.vendorPKP, w11Money(t, "1000000"), "1-1300")
	req.Allocations = []ap.AllocationInput{{InvoiceID: inv.ID, Amount: w11Money(t, "1000000")}}
	if _, err := f.svc.RecordPayment(ctx, w11Tenant, req); !errors.Is(err, ap.ErrInvoiceNotPosted) {
		t.Fatalf("mode eksplisit: error = %v, mau ErrInvoiceNotPosted", err)
	}
}

// ── 6. Urutan otomatis: tertua dulu, lintas beberapa tagihan ────────────────

func TestPayOtomatisTertuaDulu(t *testing.T) {
	f := w11Setup(t)
	ctx := context.Background()

	lama := f.tagihanProyek(t, "INV/PAY/LAMA", "10000000")
	lama.InvoiceDate = time.Now().AddDate(0, -2, 0)
	lama.DueDate = time.Now().AddDate(0, -1, 0)
	invLama, err := f.svc.RecordInvoice(ctx, w11Tenant, lama)
	if err != nil {
		t.Fatalf("catat tagihan lama: %v", err)
	}
	if _, err := f.svc.PostInvoice(ctx, w11Tenant, invLama.ID); err != nil {
		t.Fatalf("posting tagihan lama: %v", err)
	}
	invBaru := f.tagihanTerposting(t, "INV/PAY/BARU", "10000000")

	// Bayar 15jt: 10jt ke yang tertua, 5jt ke berikutnya.
	res, err := f.svc.RecordPayment(ctx, w11Tenant, bayar(f.vendorPKP, w11Money(t, "15000000"), "1-1300"))
	if err != nil {
		t.Fatalf("bayar: %v", err)
	}
	if len(res.Allocations) != 2 {
		t.Fatalf("alokasi = %d, mau 2", len(res.Allocations))
	}
	if res.Allocations[0].InvoiceNumber != "INV/PAY/LAMA" {
		t.Fatalf("alokasi pertama ke %s, mau INV/PAY/LAMA", res.Allocations[0].InvoiceNumber)
	}
	if f.sisaTagihan(t, invLama.ID).String() != "0" {
		t.Fatal("tagihan tertua belum lunas")
	}
	if got := f.sisaTagihan(t, invBaru.ID).String(); got != "5000000" {
		t.Fatalf("sisa tagihan baru = %s, mau 5000000", got)
	}
}

// ── 7. Idempotensi ──────────────────────────────────────────────────────────

func TestPayIdempotensi(t *testing.T) {
	f := w11Setup(t)
	ctx := context.Background()

	inv := f.tagihanTerposting(t, "INV/PAY/IDEM", "20000000")

	req := bayar(f.vendorPKP, w11Money(t, "8000000"), "1-1300")
	req.IdempotencyKey = "kunci-uji-001"

	first, err := f.svc.RecordPayment(ctx, w11Tenant, req)
	if err != nil {
		t.Fatalf("bayar pertama: %v", err)
	}
	second, err := f.svc.RecordPayment(ctx, w11Tenant, req)
	if err != nil {
		t.Fatalf("bayar ulang dengan kunci sama: %v", err)
	}
	if !second.Replayed {
		t.Fatal("permintaan kedua tidak ditandai replayed")
	}
	if second.Payment.ID != first.Payment.ID {
		t.Fatalf("pembayaran kedua membuat baris baru: %d vs %d", second.Payment.ID, first.Payment.ID)
	}

	var n int64
	f.db.Raw("SELECT COUNT(*) FROM ap_payments WHERE tenant_id = ?", w11Tenant).Scan(&n)
	if n != 1 {
		t.Fatalf("baris pembayaran = %d, mau 1", n)
	}
	// Uang hanya keluar sekali — inilah alasan idempotensi ada.
	if got, want := f.saldoAkun(t, "1-1300").String(), "-8000000"; got != want {
		t.Fatalf("saldo bank = %s, mau %s", got, want)
	}
	if got := f.sisaTagihan(t, inv.ID).String(); got != "12000000" {
		t.Fatalf("sisa = %s, mau 12000000", got)
	}

	// Kunci yang sama untuk permintaan yang BERBEDA adalah kesalahan pemanggil,
	// bukan pengulangan — dan memutar ulang jawaban lama akan menyembunyikannya.
	beda := bayar(f.vendorPKP, w11Money(t, "9000000"), "1-1300")
	beda.IdempotencyKey = "kunci-uji-001"
	if _, err := f.svc.RecordPayment(ctx, w11Tenant, beda); !errors.Is(err, ap.ErrIdempotencyConflict) {
		t.Fatalf("error = %v, mau ErrIdempotencyConflict", err)
	}
}

// ── 8. Pembalikan append-only ───────────────────────────────────────────────

func TestPayReverseAppendOnly(t *testing.T) {
	f := w11Setup(t)
	ctx := context.Background()

	inv := f.tagihanTerposting(t, "INV/PAY/REV", "50000000")
	res, err := f.svc.RecordPayment(ctx, w11Tenant, bayar(f.vendorPKP, w11Money(t, "50000000"), "1-1300"))
	if err != nil {
		t.Fatalf("bayar: %v", err)
	}
	if f.statusBayar(t, inv.ID) != ap.PayStatusPaid {
		t.Fatal("tagihan belum lunas sebelum pembalikan")
	}

	rev, err := f.svc.ReversePayment(ctx, w11Tenant, res.Payment.ID, time.Now())
	if err != nil {
		t.Fatalf("balik pembayaran: %v", err)
	}
	if rev.ReversedAt == nil {
		t.Fatal("pembayaran tidak ditandai dibalik")
	}

	// Invariant #5: jurnal asli TETAP ADA dan TETAP POSTED.
	var original ledger.JournalEntry
	if err := f.db.Where("tenant_id = ? AND id = ?", w11Tenant, res.Payment.JournalEntryID).
		First(&original).Error; err != nil {
		t.Fatalf("jurnal pembayaran asli hilang: %v", err)
	}
	if original.PostedAt == nil {
		t.Fatal("jurnal asli tidak lagi posted setelah dibalik")
	}
	// Ada dua jurnal atas pembayaran ini: asli (source `ap_payment`) + pembalik
	// (source `reversal`, diberikan ledger.Reverse).
	var nAsli, nPembalik int64
	f.db.Raw("SELECT COUNT(*) FROM journal_entries WHERE tenant_id = ? AND source = 'ap_payment'",
		w11Tenant).Scan(&nAsli)
	f.db.Raw("SELECT COUNT(*) FROM journal_entries WHERE tenant_id = ? AND source = 'reversal' AND reverses_id = ?",
		w11Tenant, res.Payment.JournalEntryID).Scan(&nPembalik)
	if nAsli != 1 || nPembalik != 1 {
		t.Fatalf("jurnal asli = %d, pembalik = %d — mau 1 dan 1", nAsli, nPembalik)
	}

	// Baris pembayaran & alokasinya TIDAK dihapus — ditandai.
	var nPay, nAlloc, nAllocAktif int64
	f.db.Raw("SELECT COUNT(*) FROM ap_payments WHERE tenant_id = ?", w11Tenant).Scan(&nPay)
	f.db.Raw("SELECT COUNT(*) FROM ap_payment_allocations WHERE tenant_id = ?", w11Tenant).Scan(&nAlloc)
	f.db.Raw("SELECT COUNT(*) FROM ap_payment_allocations WHERE tenant_id = ? AND reversed_at IS NULL",
		w11Tenant).Scan(&nAllocAktif)
	if nPay != 1 || nAlloc != 1 {
		t.Fatalf("baris terhapus: pembayaran %d, alokasi %d — mau 1 dan 1", nPay, nAlloc)
	}
	if nAllocAktif != 0 {
		t.Fatalf("alokasi aktif setelah pembalikan = %d, mau 0", nAllocAktif)
	}

	// Buku besar & sisa tagihan kembali seperti sebelum dibayar.
	if got, want := f.saldoAkun(t, "2-1000").String(), "-50000000"; got != want {
		t.Fatalf("hutang usaha = %s, mau %s (kewajiban hidup lagi)", got, want)
	}
	if s := f.saldoAkun(t, "1-1300"); !s.IsZero() {
		t.Fatalf("saldo bank = %s, mau nol", s)
	}
	if got := f.sisaTagihan(t, inv.ID).String(); got != "50000000" {
		t.Fatalf("sisa = %s, mau 50000000", got)
	}
	if s := f.statusBayar(t, inv.ID); s != ap.PayStatusUnpaid {
		t.Fatalf("status = %s, mau unpaid", s)
	}

	// Riwayat tetap memperlihatkan pembayaran yang dibatalkan — "pernah dibayar
	// lalu dibatalkan" bukan hal yang sama dengan "tidak pernah dibayar".
	rows, err := f.svc.ListInvoicePayments(ctx, w11Tenant, inv.ID)
	if err != nil {
		t.Fatalf("riwayat: %v", err)
	}
	if len(rows) != 1 || rows[0].ReversedAt == nil {
		t.Fatalf("riwayat pembalikan hilang: %+v", rows)
	}

	// Pembalikan kedua ditolak.
	if _, err := f.svc.ReversePayment(ctx, w11Tenant, res.Payment.ID, time.Now()); !errors.Is(err, ap.ErrPaymentReversed) {
		t.Fatalf("pembalikan kedua: error = %v, mau ErrPaymentReversed", err)
	}

	// Setelah dibalik, tagihannya bisa dibayar lagi sepenuhnya.
	if _, err := f.svc.RecordPayment(ctx, w11Tenant, bayar(f.vendorPKP, w11Money(t, "50000000"), "1-1300")); err != nil {
		t.Fatalf("bayar ulang setelah pembalikan: %v", err)
	}
	if got := f.sisaTagihan(t, inv.ID).String(); got != "0" {
		t.Fatalf("sisa setelah bayar ulang = %s, mau 0", got)
	}
}

// INV-AP-6: pengakuan tidak boleh dibalik selagi pembayarannya masih berdiri.
// Urutannya harus pembayaran dulu, baru tagihan (H2-sebelum-H1).
func TestPayTagihanBerbayarTidakBisaDibalik(t *testing.T) {
	f := w11Setup(t)
	ctx := context.Background()

	inv := f.tagihanTerposting(t, "INV/PAY/H2H1", "10000000")
	res, err := f.svc.RecordPayment(ctx, w11Tenant, bayar(f.vendorPKP, w11Money(t, "4000000"), "1-1300"))
	if err != nil {
		t.Fatalf("bayar: %v", err)
	}

	if _, err := f.svc.ReverseInvoice(ctx, w11Tenant, inv.ID, time.Now()); !errors.Is(err, ap.ErrHasAllocations) {
		t.Fatalf("balik tagihan berbayar: error = %v, mau ErrHasAllocations", err)
	}

	// Setelah pembayarannya dibalik, pengakuannya boleh dibalik.
	if _, err := f.svc.ReversePayment(ctx, w11Tenant, res.Payment.ID, time.Now()); err != nil {
		t.Fatalf("balik pembayaran: %v", err)
	}
	if _, err := f.svc.ReverseInvoice(ctx, w11Tenant, inv.ID, time.Now()); err != nil {
		t.Fatalf("balik tagihan setelah pembayaran dibalik: %v", err)
	}
}

// ── 9. Periode tertutup ─────────────────────────────────────────────────────

func TestPayPeriodeTertutup(t *testing.T) {
	f := w11Setup(t)
	ctx := context.Background()

	f.tagihanTerposting(t, "INV/PAY/CLOSE", "10000000")

	now := time.Now()
	closed := &ledger.AccountingPeriod{
		TenantID: w11Tenant, Year: now.Year(), Month: int(now.Month()), Status: "closed", ClosedAt: &now,
	}
	if err := f.db.Create(closed).Error; err != nil {
		t.Fatalf("tutup periode: %v", err)
	}

	if _, err := f.svc.RecordPayment(ctx, w11Tenant,
		bayar(f.vendorPKP, w11Money(t, "5000000"), "1-1300")); !errors.Is(err, ap.ErrPeriodClosed) {
		t.Fatalf("error = %v, mau ErrPeriodClosed", err)
	}
	var n int64
	f.db.Raw("SELECT COUNT(*) FROM ap_payments WHERE tenant_id = ?", w11Tenant).Scan(&n)
	if n != 0 {
		t.Fatalf("%d pembayaran tertinggal di periode tertutup", n)
	}
	if s := f.saldoAkun(t, "1-1300"); !s.IsZero() {
		t.Fatalf("kas bergerak di periode tertutup: %s", s)
	}
}

// ── 10. Isolasi tenant ──────────────────────────────────────────────────────

func TestPayIsolasiTenant(t *testing.T) {
	f := w11Setup(t)
	ctx := context.Background()

	inv := f.tagihanTerposting(t, "INV/PAY/ISO", "10000000")
	res, err := f.svc.RecordPayment(ctx, w11Tenant, bayar(f.vendorPKP, w11Money(t, "4000000"), "1-1300"))
	if err != nil {
		t.Fatalf("bayar: %v", err)
	}

	if _, err := f.svc.GetPayment(ctx, w11OtherTenant, res.Payment.ID); !errors.Is(err, ap.ErrPaymentNotFound) {
		t.Fatalf("baca lintas tenant: error = %v, mau ErrPaymentNotFound", err)
	}
	if _, err := f.svc.ReversePayment(ctx, w11OtherTenant, res.Payment.ID, time.Now()); !errors.Is(err, ap.ErrPaymentNotFound) {
		t.Fatalf("balik lintas tenant: error = %v, mau ErrPaymentNotFound", err)
	}
	list, err := f.svc.ListPayments(ctx, w11OtherTenant, ap.PaymentFilter{})
	if err != nil {
		t.Fatalf("daftar pembayaran tenant lain: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("tenant lain melihat %d pembayaran", len(list))
	}
	rows, err := f.svc.ListInvoicePayments(ctx, w11OtherTenant, inv.ID)
	if err != nil {
		t.Fatalf("riwayat tenant lain: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("tenant lain melihat %d baris riwayat", len(rows))
	}

	// Tenant lain juga tidak boleh MEMBAYAR tagihan tenant ini.
	req := bayar(f.vendorPKP, w11Money(t, "1000000"), "1-1300")
	req.Allocations = []ap.AllocationInput{{InvoiceID: inv.ID, Amount: w11Money(t, "1000000")}}
	if _, err := f.svc.RecordPayment(ctx, w11OtherTenant, req); err == nil {
		t.Fatal("tenant lain berhasil membayar tagihan tenant ini")
	}

	// Dan penolakan itu tidak mengubah apa pun.
	if got := f.sisaTagihan(t, inv.ID).String(); got != "6000000" {
		t.Fatalf("sisa = %s, mau 6000000", got)
	}
	if res.Payment.ReversedAt != nil {
		t.Fatal("pembayaran ikut tertandai dibalik")
	}
}

// ── 11. R-12 — KONKURENSI NYATA ─────────────────────────────────────────────
//
// Inilah kelas kegagalan yang tidak bisa ditutup oleh validasi mana pun di
// dalam satu transaksi: sisa tagihan DIHITUNG dari sub-ledger, tidak disimpan.
// Dua transaksi yang membacanya bersamaan sama-sama melihat "sisa 10jt" dan
// sama-sama lolos — hasilnya tagihan 10jt dibayar 20jt tanpa satu pun error.
//
// Penjaganya: baris tagihan dikunci SELECT … FOR UPDATE (urut ID menaik)
// SEBELUM sisanya dijumlahkan. Transaksi kedua menunggu, lalu menghitung ulang
// di atas hasil yang pertama.
//
// Test ini menjalankan dua transaksi SUNGGUHAN terhadap MySQL. Tanpa kunci itu
// ia gagal — bukan karena assert-nya galak, tetapi karena uangnya benar-benar
// keluar dua kali.

func TestPayR12KonkurensiSisaTagihan(t *testing.T) {
	f := w11Setup(t)
	ctx := context.Background()

	inv := f.tagihanTerposting(t, "INV/PAY/R12", "10000000")

	// Dua pembayaran, masing-masing SELURUH sisa. Persis satu yang boleh lolos.
	var wg sync.WaitGroup
	errs := make([]error, 2)
	start := make(chan struct{})
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start // dilepas bersamaan supaya keduanya benar-benar berebut
			req := bayar(f.vendorPKP, w11Money(t, "10000000"), "1-1300")
			req.Allocations = []ap.AllocationInput{{InvoiceID: inv.ID, Amount: w11Money(t, "10000000")}}
			_, errs[i] = f.svc.RecordPayment(ctx, w11Tenant, req)
		}(i)
	}
	close(start)
	wg.Wait()

	sukses := 0
	for i, err := range errs {
		if err == nil {
			sukses++
			continue
		}
		// Yang kalah harus kalah karena SISANYA HABIS — bukan karena deadlock,
		// timeout, atau kegagalan acak. Sebabnya harus yang benar.
		if !errors.Is(err, ap.ErrOverpayment) && !errors.Is(err, ap.ErrNothingToPay) {
			t.Fatalf("transaksi %d gagal dengan sebab yang salah: %v", i, err)
		}
	}
	if sukses != 1 {
		t.Fatalf("%d dari 2 pembayaran lolos, mau tepat 1 (errs: %v)", sukses, errs)
	}

	// Bukti di angka, bukan di jumlah error: tagihan 10jt tidak boleh terbayar
	// lebih dari 10jt, dan kas tidak boleh keluar lebih dari 10jt.
	if got := f.sisaTagihan(t, inv.ID).String(); got != "0" {
		t.Fatalf("sisa = %s, mau 0", got)
	}
	var terbayar *string
	f.db.Raw(`SELECT COALESCE(SUM(amount),0) FROM ap_payment_allocations
		WHERE tenant_id = ? AND invoice_id = ? AND reversed_at IS NULL`,
		w11Tenant, inv.ID).Row().Scan(&terbayar)
	if *terbayar != "10000000.0000" {
		t.Fatalf("Σ alokasi = %s, mau 10000000.0000", *terbayar)
	}
	if got, want := f.saldoAkun(t, "1-1300").String(), "-10000000"; got != want {
		t.Fatalf("kas keluar %s, mau %s", got, want)
	}
	if s := f.saldoAkun(t, "2-1000"); !s.IsZero() {
		t.Fatalf("hutang usaha = %s, mau nol", s)
	}
	var nPay int64
	f.db.Raw("SELECT COUNT(*) FROM ap_payments WHERE tenant_id = ?", w11Tenant).Scan(&nPay)
	if nPay != 1 {
		t.Fatalf("baris pembayaran = %d, mau 1", nPay)
	}
}

// Kunci yang diambil MySQL mengikuti baris yang DIPINDAI. Kedua pembacaan
// terkunci di jalur pembayaran karena itu wajib lewat indeks — kalau salah
// satunya memindai tabel, kuncinya meluas ke tagihan yang tidak ada urusannya
// dengan pembayaran ini.
func TestPayR12PembacaanTerkunciLewatIndeks(t *testing.T) {
	f := w11Setup(t)

	// Datanya harus cukup untuk membuat pilihan indeks BERMAKNA. Di atas tabel
	// berisi satu baris, optimizer memilih sembarang indeks dengan biaya yang
	// sama — rencana yang dihasilkannya tidak membuktikan apa pun. Dengan belasan
	// tagihan yang masing-masing punya alokasi, (tenant_id, invoice_id) benar-benar
	// lebih selektif daripada (tenant_id) saja, dan pilihannya bisa dinilai.
	var inv *ap.Invoice
	for i := 0; i < 12; i++ {
		cur := f.tagihanTerposting(t, fmt.Sprintf("INV/PAY/R12-IDX-%02d", i), "10000000")
		req := bayar(f.vendorPKP, w11Money(t, "4000000"), "1-1300")
		req.Allocations = []ap.AllocationInput{{InvoiceID: cur.ID, Amount: w11Money(t, "4000000")}}
		if _, err := f.svc.RecordPayment(context.Background(), w11Tenant, req); err != nil {
			t.Fatalf("bayar tagihan %d: %v", i, err)
		}
		inv = cur
	}

	cases := []struct {
		nama  string
		query string
		args  []any
		index string
	}{
		{
			"kunci baris tagihan",
			"SELECT * FROM ap_invoices WHERE tenant_id = ? AND id IN (?) ORDER BY id ASC FOR UPDATE",
			[]any{w11Tenant, inv.ID},
			"PRIMARY",
		},
		{
			"kunci sub-ledger alokasi",
			"SELECT * FROM ap_payment_allocations WHERE tenant_id = ? AND invoice_id = ? AND reversed_at IS NULL ORDER BY id ASC FOR UPDATE",
			[]any{w11Tenant, inv.ID},
			"idx_apa_tenant_invoice",
		},
	}
	for _, c := range cases {
		t.Run(c.nama, func(t *testing.T) {
			plan := f.rencanaQuery(t, c.query, c.args...)
			if strings.Contains(plan, `"access_type": "ALL"`) {
				t.Fatalf("pembacaan terkunci memindai seluruh tabel:\n%s", plan)
			}
			if !strings.Contains(plan, `"key": "`+c.index+`"`) {
				t.Fatalf("indeks yang dipakai bukan %s:\n%s", c.index, plan)
			}
		})
	}
}

// Versi mode-otomatis dari uji yang sama: tanpa daftar alokasi eksplisit,
// keduanya menyusun alokasinya sendiri dari sisa yang mereka baca. Kalau
// pembacaan itu tidak terkunci, keduanya menyusun rencana atas uang yang sama.
func TestPayR12KonkurensiModeOtomatis(t *testing.T) {
	f := w11Setup(t)

	inv := f.tagihanTerposting(t, "INV/PAY/R12-AUTO", "9000000")

	var wg sync.WaitGroup
	errs := make([]error, 3)
	start := make(chan struct{})
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			_, errs[i] = f.svc.RecordPayment(context.Background(), w11Tenant,
				bayar(f.vendorPKP, w11Money(t, "6000000"), "1-1300"))
		}(i)
	}
	close(start)
	wg.Wait()

	sukses := 0
	for i, err := range errs {
		if err == nil {
			sukses++
			continue
		}
		if !errors.Is(err, ap.ErrOverpayment) && !errors.Is(err, ap.ErrNothingToPay) {
			t.Fatalf("transaksi %d gagal dengan sebab yang salah: %v", i, err)
		}
	}
	// Sisa 9jt, masing-masing minta 6jt: hanya satu yang muat.
	if sukses != 1 {
		t.Fatalf("%d dari 3 pembayaran lolos, mau tepat 1 (errs: %v)", sukses, errs)
	}
	if got := f.sisaTagihan(t, inv.ID).String(); got != "3000000" {
		t.Fatalf("sisa = %s, mau 3000000", got)
	}
	if got, want := f.saldoAkun(t, "1-1300").String(), "-6000000"; got != want {
		t.Fatalf("kas keluar %s, mau %s", got, want)
	}
}

// Pembalikan ganda yang berlomba. Kunci di sini ada pada BARIS PEMBAYARAN
// (SELECT … FOR UPDATE) — tanpa itu dua pembalik terbit atas satu pembayaran
// dan kewajiban vendor hidup dua kali.
func TestPayR12KonkurensiPembalikan(t *testing.T) {
	f := w11Setup(t)
	ctx := context.Background()

	inv := f.tagihanTerposting(t, "INV/PAY/R12-REV", "8000000")
	res, err := f.svc.RecordPayment(ctx, w11Tenant, bayar(f.vendorPKP, w11Money(t, "8000000"), "1-1300"))
	if err != nil {
		t.Fatalf("bayar: %v", err)
	}

	var wg sync.WaitGroup
	errs := make([]error, 2)
	start := make(chan struct{})
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			_, errs[i] = f.svc.ReversePayment(ctx, w11Tenant, res.Payment.ID, time.Now())
		}(i)
	}
	close(start)
	wg.Wait()

	sukses := 0
	for i, err := range errs {
		if err == nil {
			sukses++
			continue
		}
		if !errors.Is(err, ap.ErrPaymentReversed) {
			t.Fatalf("pembalikan %d gagal dengan sebab yang salah: %v", i, err)
		}
	}
	if sukses != 1 {
		t.Fatalf("%d dari 2 pembalikan lolos, mau tepat 1 (errs: %v)", sukses, errs)
	}

	// Kewajiban hidup lagi TEPAT SEKALI.
	if got, want := f.saldoAkun(t, "2-1000").String(), "-8000000"; got != want {
		t.Fatalf("hutang usaha = %s, mau %s", got, want)
	}
	if s := f.saldoAkun(t, "1-1300"); !s.IsZero() {
		t.Fatalf("saldo bank = %s, mau nol", s)
	}
	if got := f.sisaTagihan(t, inv.ID).String(); got != "8000000" {
		t.Fatalf("sisa = %s, mau 8000000", got)
	}
	// Tepat SATU pembalik terbit — dua pembalik berarti kewajiban hidup dua kali.
	var nPembalik int64
	f.db.Raw("SELECT COUNT(*) FROM journal_entries WHERE tenant_id = ? AND reverses_id = ?",
		w11Tenant, res.Payment.JournalEntryID).Scan(&nPembalik)
	if nPembalik != 1 {
		t.Fatalf("jurnal pembalik = %d, mau tepat 1", nPembalik)
	}
}

// Kunci idempotensi yang sama, dua permintaan bersamaan. Pemeriksaan
// idempotensi di awal tidak bisa menutup lomba ini sendirian — yang menutupnya
// adalah unique key `uq_app_tenant_idem` di MySQL.
func TestPayR12KonkurensiKunciIdempotensi(t *testing.T) {
	f := w11Setup(t)

	inv := f.tagihanTerposting(t, "INV/PAY/R12-IDEM", "10000000")

	var wg sync.WaitGroup
	errs := make([]error, 2)
	start := make(chan struct{})
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			req := bayar(f.vendorPKP, w11Money(t, "3000000"), "1-1300")
			req.IdempotencyKey = "lomba-001"
			_, errs[i] = f.svc.RecordPayment(context.Background(), w11Tenant, req)
		}(i)
	}
	close(start)
	wg.Wait()

	for i, err := range errs {
		// Keduanya boleh "berhasil" (yang satu memutar ulang hasil yang lain),
		// tetapi tidak boleh gagal dengan sebab lain.
		if err != nil && !errors.Is(err, ap.ErrIdempotencyConflict) {
			t.Fatalf("permintaan %d gagal: %v", i, err)
		}
	}

	// Yang menentukan bukan jumlah error, tetapi jumlah uang yang keluar.
	var nPay int64
	f.db.Raw("SELECT COUNT(*) FROM ap_payments WHERE tenant_id = ?", w11Tenant).Scan(&nPay)
	if nPay != 1 {
		t.Fatalf("baris pembayaran = %d, mau 1 — kunci idempotensi bocor", nPay)
	}
	if got, want := f.saldoAkun(t, "1-1300").String(), "-3000000"; got != want {
		t.Fatalf("kas keluar %s, mau %s — uang keluar dua kali", got, want)
	}
	if got := f.sisaTagihan(t, inv.ID).String(); got != "7000000" {
		t.Fatalf("sisa = %s, mau 7000000", got)
	}
}

// ── 12. Uang muka & retensi tetap tertutup ──────────────────────────────────

func TestPayUangMukaDanRetensiBelumDibuka(t *testing.T) {
	f := w11Setup(t)
	ctx := context.Background()
	f.tagihanTerposting(t, "INV/PAY/SHUT", "10000000")

	adv := bayar(f.vendorPKP, w11Money(t, "1000000"), "1-1300")
	adv.Kind = ap.PaymentKindAdvance
	if _, err := f.svc.RecordPayment(ctx, w11Tenant, adv); !errors.Is(err, ap.ErrAdvanceNotEnabled) {
		t.Fatalf("uang muka: error = %v, mau ErrAdvanceNotEnabled", err)
	}

	ret := bayar(f.vendorPKP, w11Money(t, "1000000"), "1-1300")
	ret.Kind = ap.PaymentKindRetention
	if _, err := f.svc.RecordPayment(ctx, w11Tenant, ret); !errors.Is(err, ap.ErrRetentionNotEnabled) {
		t.Fatalf("retensi: error = %v, mau ErrRetentionNotEnabled", err)
	}

	// Tidak satu pun baris uang muka/retensi lahir — termasuk di sub-ledger,
	// tempat baris bertipe `advance` akan merusak perhitungan saldo uang muka.
	var n int64
	f.db.Raw(`SELECT COUNT(*) FROM ap_payment_allocations
		WHERE tenant_id = ? AND allocation_type <> 'invoice'`, w11Tenant).Scan(&n)
	if n != 0 {
		t.Fatalf("%d alokasi non-invoice lahir", n)
	}
	f.db.Raw("SELECT COUNT(*) FROM ap_payments WHERE tenant_id = ?", w11Tenant).Scan(&n)
	if n != 0 {
		t.Fatalf("%d pembayaran lahir dari jenis yang belum dibuka", n)
	}
	if s := f.saldoAkun(t, "1-5300"); !s.IsZero() {
		t.Fatalf("uang muka vendor = %s, mau nol", s)
	}
	if s := f.saldoAkun(t, "2-1100"); !s.IsZero() {
		t.Fatalf("hutang retensi = %s, mau nol", s)
	}
}

//go:build integration

package document_test

// W-2 — mesin penomoran dokumen terhadap MySQL nyata.
//
// Empat sifat yang menjadi tumpuan keputusan owner diuji di sini, karena
// semuanya lahir dari perilaku DB (kunci baris, ON DUPLICATE KEY, UNIQUE) dan
// tidak bisa dibuktikan oleh unit test:
//
//  1. Tahun adopsi MELANJUTKAN nomor yang sudah tercetak — engine baru tidak
//     boleh mengulang nomor yang sudah dipegang pembeli.
//  2. Tahun berikutnya mulai dari 000001 — reset tahunan, forward-only.
//  3. Baris seri hilang tidak membuat seri mengulang (self-heal dari registry).
//  4. Fail-closed: tanpa master jenis dokumen yang aktif, tidak ada nomor terbit.
//
// Plus satu sifat yang menjadi inti perintah owner: menambah JENIS dokumen baru
// cukup menambah baris konfigurasi — enginenya tidak disentuh sama sekali.

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"esaproperti/internal/document"
	"esaproperti/internal/domain"
)

const docTenant uint64 = 9_900_247

func docConnect(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := os.Getenv("TEST_DB_DSN")
	if dsn == "" {
		t.Skip("TEST_DB_DSN tidak di-set — lewati")
	}
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("koneksi DB: %v", err)
	}
	return db
}

func docCleanup(t *testing.T, db *gorm.DB) {
	t.Helper()
	// Urutan FK: anak sebelum induk (ap_payments/cost_entries → journal_entries
	// & vendors/projects; documents tidak punya FK ke tabel-tabel ini).
	for _, tbl := range []string{
		"documents", "document_sequences", "document_types", "master_data_changes",
		"ap_payments", "cost_entries", "journal_entries", "vendors", "projects",
	} {
		if err := db.Exec("DELETE FROM "+tbl+" WHERE tenant_id = ?", docTenant).Error; err != nil {
			t.Fatalf("cleanup %s: %v", tbl, err)
		}
	}
}

func docSetup(t *testing.T) *gorm.DB {
	t.Helper()
	db := docConnect(t)
	docCleanup(t, db)
	t.Cleanup(func() { docCleanup(t, db) })
	if err := document.SeedDefaultDocumentTypes(context.Background(), db, docTenant); err != nil {
		t.Fatalf("seed jenis dokumen: %v", err)
	}
	return db
}

func at(y int, m time.Month, d int) time.Time {
	return time.Date(y, m, d, 9, 0, 0, 0, time.UTC)
}

// issue menerbitkan satu dokumen di transaksinya sendiri, meniru pemakaian
// nyata (Allocate + Register selalu satu transaksi dengan baris sumbernya).
func issue(t *testing.T, db *gorm.DB, typeCode string, when time.Time, srcID uint64) *document.Document {
	t.Helper()
	var doc *document.Document
	err := db.Transaction(func(tx *gorm.DB) error {
		d, err := document.Issue(tx, docTenant, typeCode, when,
			document.Source{Table: "it_sources", ID: srcID}, domain.FromInt(1_000_000), nil)
		if err != nil {
			return err
		}
		doc = d
		return nil
	})
	if err != nil {
		t.Fatalf("terbitkan %s: %v", typeCode, err)
	}
	return doc
}

func wantNumber(t *testing.T, got, want string) {
	t.Helper()
	if got != want {
		t.Fatalf("nomor = %q, mau %q", got, want)
	}
}

// ── 1. Tahun adopsi: melanjutkan, bukan mengulang ───────────────────────────

// Migration 000065 memindahkan penghitung lama ke `document_sequences` untuk
// tahun berjalan. Tes ini mensimulasikan tenant yang sudah mencetak
// KWT/2026/000041 lalu engine baru mulai dipakai: nomor berikutnya WAJIB 000042.
// Kalau tes ini merah, ada pembeli yang memegang dua kwitansi bernomor sama.
func TestAdopsi_MelanjutkanNomorLama(t *testing.T) {
	db := docSetup(t)
	if err := db.Exec(`
		INSERT INTO document_sequences (tenant_id, document_type_code, fiscal_year, last_val, created_at, updated_at)
		VALUES (?, ?, 2026, 41, NOW(3), NOW(3))`,
		docTenant, document.TypeHousePayment).Error; err != nil {
		t.Fatalf("seed seri adopsi: %v", err)
	}

	doc := issue(t, db, document.TypeHousePayment, at(2026, time.August, 7), 1)
	wantNumber(t, doc.Number, "KWT/2026/000042")
	if doc.SequenceNo != 42 || doc.FiscalYear != 2026 {
		t.Fatalf("seq/fy = %d/%d, mau 42/2026", doc.SequenceNo, doc.FiscalYear)
	}

	next := issue(t, db, document.TypeHousePayment, at(2026, time.August, 7), 2)
	wantNumber(t, next.Number, "KWT/2026/000043")
}

// ── 2. Reset tahunan ────────────────────────────────────────────────────────

// Keputusan owner: tahun baru mulai dari 000001. Seri tahun lama tetap utuh —
// dua tahun hidup berdampingan sebagai dua baris seri terpisah.
func TestResetTahunan_TahunBaruMulaiDariSatu(t *testing.T) {
	db := docSetup(t)
	if err := db.Exec(`
		INSERT INTO document_sequences (tenant_id, document_type_code, fiscal_year, last_val, created_at, updated_at)
		VALUES (?, ?, 2026, 120, NOW(3), NOW(3))`,
		docTenant, document.TypeInvoice).Error; err != nil {
		t.Fatalf("seed seri 2026: %v", err)
	}

	wantNumber(t, issue(t, db, document.TypeInvoice, at(2026, time.December, 31), 10).Number, "INV/2026/000121")
	wantNumber(t, issue(t, db, document.TypeInvoice, at(2027, time.January, 1), 11).Number, "INV/2027/000001")
	wantNumber(t, issue(t, db, document.TypeInvoice, at(2027, time.January, 2), 12).Number, "INV/2027/000002")

	// Seri 2026 tidak ikut bergerak saat 2027 berjalan.
	var last2026 uint64
	db.Raw(`SELECT last_val FROM document_sequences
	         WHERE tenant_id = ? AND document_type_code = ? AND fiscal_year = 2026`,
		docTenant, document.TypeInvoice).Scan(&last2026)
	if last2026 != 121 {
		t.Fatalf("seri 2026 = %d, mau tetap 121", last2026)
	}
}

// ── 3. Self-heal dari registry ──────────────────────────────────────────────

// Baris seri boleh hilang (restore parsial, tahun yang tak ter-seed, pergeseran
// zona waktu di pergantian tahun). Registry `documents` adalah bukti terakhir
// nomor mana yang sudah terpakai, dan engine harus melanjutkan dari situ.
func TestSelfHeal_SeriHilangTidakMengulangNomor(t *testing.T) {
	db := docSetup(t)
	for i := uint64(1); i <= 3; i++ {
		issue(t, db, document.TypeRealization, at(2026, time.May, 4), 100+i)
	}
	if err := db.Exec(`DELETE FROM document_sequences
	                    WHERE tenant_id = ? AND document_type_code = ?`,
		docTenant, document.TypeRealization).Error; err != nil {
		t.Fatalf("hapus baris seri: %v", err)
	}

	doc := issue(t, db, document.TypeRealization, at(2026, time.May, 5), 104)
	wantNumber(t, doc.Number, "KWR/2026/000004")
}

// ── 4. Fail-closed ──────────────────────────────────────────────────────────

func TestFailClosed_TanpaMasterTidakAdaNomorTerbit(t *testing.T) {
	db := docSetup(t)

	t.Run("kode tak terdaftar", func(t *testing.T) {
		err := db.Transaction(func(tx *gorm.DB) error {
			_, e := document.Allocate(tx, docTenant, "surat_jalan", at(2026, time.August, 7))
			return e
		})
		if !errors.Is(err, document.ErrTypeUnknown) {
			t.Fatalf("dapat %v, mau ErrTypeUnknown", err)
		}
	})

	t.Run("jenis nonaktif", func(t *testing.T) {
		db.Exec(`UPDATE document_types SET is_active = 0 WHERE tenant_id = ? AND code = ?`,
			docTenant, document.TypeBooking)
		defer db.Exec(`UPDATE document_types SET is_active = 1 WHERE tenant_id = ? AND code = ?`,
			docTenant, document.TypeBooking)

		err := db.Transaction(func(tx *gorm.DB) error {
			_, e := document.Allocate(tx, docTenant, document.TypeBooking, at(2026, time.August, 7))
			return e
		})
		if !errors.Is(err, document.ErrTypeInactive) {
			t.Fatalf("dapat %v, mau ErrTypeInactive", err)
		}
	})

	t.Run("format rusak ditolak sebelum nomor dipakai", func(t *testing.T) {
		db.Exec(`UPDATE document_types SET number_format = '{prefix}/{year}' WHERE tenant_id = ? AND code = ?`,
			docTenant, document.TypeInternalTransfer)
		defer db.Exec(`UPDATE document_types SET number_format = '{prefix}/{year}/{seq}' WHERE tenant_id = ? AND code = ?`,
			docTenant, document.TypeInternalTransfer)

		err := db.Transaction(func(tx *gorm.DB) error {
			_, e := document.Allocate(tx, docTenant, document.TypeInternalTransfer, at(2026, time.August, 7))
			return e
		})
		if !errors.Is(err, document.ErrFormatInvalid) {
			t.Fatalf("dapat %v, mau ErrFormatInvalid", err)
		}
		// Transaksi gagal ⇒ seri tidak boleh ikut naik.
		var n int64
		db.Raw(`SELECT COUNT(*) FROM document_sequences
		         WHERE tenant_id = ? AND document_type_code = ?`,
			docTenant, document.TypeInternalTransfer).Scan(&n)
		if n != 0 {
			t.Fatalf("seri terbentuk (%d baris) padahal penerbitan gagal", n)
		}
	})

	// Tenant lain tidak boleh ikut kena — masternya milik masing-masing tenant.
	t.Run("tenant tanpa master sama sekali", func(t *testing.T) {
		err := db.Transaction(func(tx *gorm.DB) error {
			_, e := document.Allocate(tx, docTenant+1, document.TypeHousePayment, at(2026, time.August, 7))
			return e
		})
		if !errors.Is(err, document.ErrTypeUnknown) {
			t.Fatalf("dapat %v, mau ErrTypeUnknown", err)
		}
	})
}

// ── 5. Rollback tidak menyisakan nomor terpakai ─────────────────────────────

// Nomor diambil di dalam transaksi pemanggil justru supaya ikut batal. Kalau
// tidak, setiap penerimaan pembayaran yang gagal akan meninggalkan lubang nomor
// yang tidak bisa dijelaskan ke auditor.
func TestRollback_NomorIkutBatal(t *testing.T) {
	db := docSetup(t)
	wantNumber(t, issue(t, db, document.TypeHousePayment, at(2026, time.March, 1), 200).Number, "KWT/2026/000001")

	boom := errors.New("gagal di tengah jalan")
	err := db.Transaction(func(tx *gorm.DB) error {
		if _, e := document.Allocate(tx, docTenant, document.TypeHousePayment, at(2026, time.March, 2)); e != nil {
			return e
		}
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("transaksi harus gagal, dapat %v", err)
	}

	wantNumber(t, issue(t, db, document.TypeHousePayment, at(2026, time.March, 3), 201).Number, "KWT/2026/000002")
}

// ── 6. Idempotensi sumber ───────────────────────────────────────────────────

// Satu baris sumber hanya boleh punya SATU dokumen. UNIQUE (tenant, source) di
// registry adalah jaring pengaman terakhir bila ada jalur baru yang lupa cek
// idempotensinya sendiri.
func TestRegistry_SatuSumberSatuDokumen(t *testing.T) {
	db := docSetup(t)
	issue(t, db, document.TypeHousePayment, at(2026, time.April, 1), 300)

	err := db.Transaction(func(tx *gorm.DB) error {
		_, e := document.Issue(tx, docTenant, document.TypeHousePayment, at(2026, time.April, 2),
			document.Source{Table: "it_sources", ID: 300}, domain.FromInt(1), nil)
		return e
	})
	if err == nil {
		t.Fatal("dokumen kedua untuk sumber yang sama harus ditolak")
	}
}

// ── 6a. ListFilter.Number: pencarian persis satu nomor ──────────────────────

// Dipakai layar mana pun yang menampilkan nomor dokumen sebagai teks dan perlu
// membukanya (DocumentNumberLink di frontend). Nomor bersifat unik per tenant,
// jadi filter ini wajib mengembalikan tepat satu baris untuk nomor yang tepat,
// dan nol baris untuk nomor yang tidak pernah terbit.
func TestListFilter_NomorPersisSatuBaris(t *testing.T) {
	db := docSetup(t)
	svc := document.NewService(db)
	target := issue(t, db, document.TypeHousePayment, at(2026, time.April, 1), 800)
	issue(t, db, document.TypeHousePayment, at(2026, time.April, 2), 801)

	docs, err := svc.ListDocuments(context.Background(), docTenant, document.ListFilter{Number: target.Number})
	if err != nil {
		t.Fatalf("ListDocuments: %v", err)
	}
	if len(docs) != 1 {
		t.Fatalf("jumlah dokumen = %d, mau 1", len(docs))
	}
	wantNumber(t, docs[0].Number, target.Number)

	none, err := svc.ListDocuments(context.Background(), docTenant, document.ListFilter{Number: "KWT/2026/999999"})
	if err != nil {
		t.Fatalf("ListDocuments (tak ada): %v", err)
	}
	if len(none) != 0 {
		t.Fatalf("jumlah dokumen untuk nomor tak dikenal = %d, mau 0", len(none))
	}
}

// ── 6b. Forward-only: konfigurasi berubah, dokumen lama tidak ───────────────

// Verifikasi yang diminta owner sebelum W-3.
//
// Mengubah prefix/format adalah perubahan KEBIJAKAN, bukan migrasi. Dokumen yang
// sudah terbit memegang nomornya sendiri sebagai string beku di registry — tidak
// pernah dirender ulang dari master. Kalau suatu saat ada yang "merapikan" ini
// menjadi nomor yang dihitung on-the-fly dari `document_types`, tes ini merah,
// dan memang harus merah: kwitansi yang sudah dipegang pembeli akan berubah
// tampilannya di layar sementara kertasnya tidak.
func TestForwardOnly_UbahPrefixTidakMenyentuhDokumenLama(t *testing.T) {
	db := docSetup(t)
	svc := document.NewService(db)

	lama := issue(t, db, document.TypeHousePayment, at(2026, time.September, 1), 700)
	wantNumber(t, lama.Number, "KWT/2026/000001")

	var sys document.DocumentType
	if err := db.Where("tenant_id = ? AND code = ?", docTenant, document.TypeHousePayment).
		First(&sys).Error; err != nil {
		t.Fatalf("baca jenis: %v", err)
	}
	prefix, format, pad := "KWX", "{prefix}-{year}{month}-{seq}", uint8(4)
	actor := uint64(4242)
	if _, err := svc.UpdateType(context.Background(), docTenant, sys.ID, document.TypeUpdate{
		Prefix: &prefix, NumberFormat: &format, Padding: &pad,
	}, &actor); err != nil {
		t.Fatalf("ubah konfigurasi: %v", err)
	}

	// (a) Dokumen lama tidak bergerak sedikit pun.
	var after document.Document
	if err := db.Where("id = ?", lama.ID).First(&after).Error; err != nil {
		t.Fatalf("baca ulang dokumen lama: %v", err)
	}
	if after.Number != "KWT/2026/000001" {
		t.Fatalf("nomor dokumen lama berubah jadi %q — dokumen historis harus immutable", after.Number)
	}
	if after.SequenceNo != lama.SequenceNo || after.FiscalYear != lama.FiscalYear {
		t.Fatalf("kunci seri dokumen lama berubah: %d/%d", after.SequenceNo, after.FiscalYear)
	}

	// (b) Yang berikutnya memakai konfigurasi baru, dan MELANJUTKAN seri —
	//     ganti bentuk nomor tidak boleh diam-diam mereset penghitung.
	wantNumber(t, issue(t, db, document.TypeHousePayment, at(2026, time.September, 2), 701).Number,
		"KWX-202609-0002")

	// (c) Audit menyimpan nilai sebelum, sesudah, actor, dan waktu — untuk
	//     SETIAP field yang berubah, bukan satu baris ringkasan.
	hist, err := svc.ListMasterChanges(context.Background(), docTenant, 50)
	if err != nil {
		t.Fatalf("baca audit: %v", err)
	}
	type want struct{ old, nw string }
	wants := map[string]want{
		"prefix":        {"KWT", "KWX"},
		"number_format": {"{prefix}/{year}/{seq}", "{prefix}-{year}{month}-{seq}"},
		"padding":       {"6", "4"},
	}
	seen := map[string]bool{}
	for _, h := range hist {
		w, ok := wants[h.Field]
		if !ok || h.EntityCode != document.TypeHousePayment {
			continue
		}
		seen[h.Field] = true
		if h.OldValue != w.old || h.NewValue != w.nw {
			t.Fatalf("audit %s: %q → %q, mau %q → %q", h.Field, h.OldValue, h.NewValue, w.old, w.nw)
		}
		if h.ChangedBy == nil || *h.ChangedBy != actor {
			t.Fatalf("audit %s: actor = %v, mau %d", h.Field, h.ChangedBy, actor)
		}
		if h.CreatedAt.IsZero() {
			t.Fatalf("audit %s: tanpa timestamp", h.Field)
		}
	}
	for f := range wants {
		if !seen[f] {
			t.Fatalf("perubahan %q tidak tercatat di audit", f)
		}
	}
}

// Nomor yang sudah terbit tidak boleh bisa ditimpa — bukan hanya "tidak ada
// endpoint untuk itu", tapi DB yang menolak. UNIQUE (tenant, type, number)
// adalah penjaganya.
func TestForwardOnly_NomorTerbitTidakBisaDipakaiUlang(t *testing.T) {
	db := docSetup(t)
	doc := issue(t, db, document.TypeInvoice, at(2026, time.October, 1), 800)

	err := db.Exec(`
		INSERT INTO documents
			(tenant_id, document_type_code, number, fiscal_year, sequence_no, issued_at,
			 source_table, source_id, amount, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, NOW(3), 'it_sources', 801, '0.0000', NOW(3), NOW(3))`,
		docTenant, document.TypeInvoice, doc.Number, doc.FiscalYear, doc.SequenceNo).Error
	if err == nil {
		t.Fatal("nomor yang sudah terbit berhasil dipakai ulang — UNIQUE registry bocor")
	}
}

// ── 7. Jenis baru = konfigurasi, bukan kode ─────────────────────────────────

// Inti perintah owner: "Semua jenis dokumen … harus menggunakan engine yang
// sama. Yang berbeda hanya konfigurasi document_type." Tes ini menerbitkan
// Cash Voucher — jenis yang tidak dikenal package mana pun — hanya dengan
// menambah satu baris master. Tidak ada satu baris kode Go pun yang menyebut
// "cash_voucher".
func TestJenisBaru_CukupTambahMaster(t *testing.T) {
	db := docSetup(t)
	svc := document.NewService(db)

	created, err := svc.CreateType(context.Background(), docTenant, document.TypeInput{
		Code:         "cash_voucher",
		Name:         "Bukti Kas Keluar",
		Prefix:       "BKK",
		NumberFormat: "{prefix}-{year}{month}-{seq}",
		ResetPolicy:  document.ResetYearly,
		Padding:      4,
	}, nil)
	if err != nil {
		t.Fatalf("buat jenis dokumen: %v", err)
	}
	if created.IsSystem {
		t.Fatal("jenis buatan admin tidak boleh bertanda is_system")
	}

	wantNumber(t, issue(t, db, "cash_voucher", at(2026, time.March, 15), 400).Number, "BKK-202603-0001")
	wantNumber(t, issue(t, db, "cash_voucher", at(2026, time.March, 16), 401).Number, "BKK-202603-0002")
	// Bulan ikut format, tapi resetnya tetap tahunan — seri tidak balik ke 1.
	wantNumber(t, issue(t, db, "cash_voucher", at(2026, time.April, 1), 402).Number, "BKK-202604-0003")
	wantNumber(t, issue(t, db, "cash_voucher", at(2027, time.January, 5), 403).Number, "BKK-202701-0001")
}

// Jenis dokumen inti tidak boleh dimatikan lewat API — mematikannya membuat
// penerimaan pembayaran gagal total, dan admin biasanya tidak menyadari itu
// sampai kasir tidak bisa mencetak kwitansi.
func TestMaster_JenisSistemTidakBisaDinonaktifkan(t *testing.T) {
	db := docSetup(t)
	svc := document.NewService(db)

	var sys document.DocumentType
	if err := db.Where("tenant_id = ? AND code = ?", docTenant, document.TypeHousePayment).
		First(&sys).Error; err != nil {
		t.Fatalf("baca jenis sistem: %v", err)
	}

	off := false
	if _, err := svc.UpdateType(context.Background(), docTenant, sys.ID,
		document.TypeUpdate{IsActive: &off}, nil); err == nil {
		t.Fatal("menonaktifkan jenis sistem harus ditolak")
	}

	// Tapi prefix/format tetap milik admin.
	prefix := "KW"
	if _, err := svc.UpdateType(context.Background(), docTenant, sys.ID,
		document.TypeUpdate{Prefix: &prefix}, nil); err != nil {
		t.Fatalf("ubah prefix jenis sistem: %v", err)
	}
	wantNumber(t, issue(t, db, document.TypeHousePayment, at(2026, time.June, 1), 500).Number, "KW/2026/000001")
}

// ── 8. Preview tidak menggerakkan seri ──────────────────────────────────────

func TestPreview_TidakMemakaiNomor(t *testing.T) {
	db := docSetup(t)
	svc := document.NewService(db)
	issue(t, db, document.TypeHousePayment, at(2026, time.July, 1), 600)

	for i := 0; i < 3; i++ {
		all, err := svc.PreviewAll(context.Background(), docTenant, at(2026, time.July, 2))
		if err != nil {
			t.Fatalf("preview: %v", err)
		}
		var found bool
		for _, p := range all {
			if p.Code != document.TypeHousePayment {
				continue
			}
			found = true
			if p.NextNumber != "KWT/2026/000002" {
				t.Fatalf("pratinjau nomor = %q, mau KWT/2026/000002", p.NextNumber)
			}
			if p.Issued != 1 {
				t.Fatalf("jumlah terbit = %d, mau 1", p.Issued)
			}
		}
		if !found {
			t.Fatal("pratinjau tidak memuat jenis kwitansi rumah")
		}
	}
	wantNumber(t, issue(t, db, document.TypeHousePayment, at(2026, time.July, 2), 601).Number, "KWT/2026/000002")
}

// ── 9. ResolvePrintTargetByNumber — bypass cetak, bukan navigasi ────────────

// Perbaikan atas koreksi owner: klik nomor dokumen di FE harus langsung
// mencetak/mengunduh, BUKAN mengarahkan ke layar lain (mis. detail jurnal).
// Backend menjawab "kalau nomor ini diklik, cetakan mana yang harus dibuka"
// sebagai {kind, id} yang dipetakan FE ke fetcher cetak modul terkait — tidak
// pernah tebakan, dan tidak pernah alamat layar.
func TestResolvePrintTarget_KwitansiDanInvoiceLangsungKeSumbernya(t *testing.T) {
	db := docSetup(t)
	svc := document.NewService(db)

	receiptDoc := issueFromSource(t, db, document.TypeHousePayment, at(2026, time.May, 1), "receipts", 4001)
	invoiceDoc := issueFromSource(t, db, document.TypeInvoice, at(2026, time.May, 1), "invoices", 4002)

	target, err := svc.ResolvePrintTargetByNumber(context.Background(), docTenant, receiptDoc.Number)
	if err != nil {
		t.Fatalf("resolve kwitansi: %v", err)
	}
	if target.Kind != document.PrintReceipt || target.ID != 4001 {
		t.Fatalf("target kwitansi = %+v, mau {receipt 4001}", target)
	}

	target, err = svc.ResolvePrintTargetByNumber(context.Background(), docTenant, invoiceDoc.Number)
	if err != nil {
		t.Fatalf("resolve invoice: %v", err)
	}
	if target.Kind != document.PrintInvoice || target.ID != 4002 {
		t.Fatalf("target invoice = %+v, mau {invoice 4002}", target)
	}
}

// Dokumen kas (BKK) yang lahir dari jurnal — SourceID di registry adalah
// journal_entries.id, bukan id entitas yang bisa dicetak. Resolver harus
// menembus ke pemilik jurnal itu (ap_payments ATAU cost_entries) untuk
// menemukan cetakan yang SUDAH ADA di aplikasi.
func TestResolvePrintTarget_BKKMenembusKePemilikJurnal(t *testing.T) {
	db := docSetup(t)
	svc := document.NewService(db)

	seedProject(t, db, 9001)
	seedVendor(t, db, 9002, "Vendor Uji BKK")

	// BKK milik pembayaran vendor.
	apJournalID := seedJournal(t, db, at(2026, time.June, 1))
	seedAPPayment(t, db, 9101, 9002, apJournalID)
	apDoc := issueFromSource(t, db, document.TypeCashOut, at(2026, time.June, 1), document.SourceJournal, apJournalID)

	// BKK milik biaya tunai langsung.
	costJournalID := seedJournal(t, db, at(2026, time.June, 1))
	seedCostEntry(t, db, 9201, 9001, costJournalID)
	costDoc := issueFromSource(t, db, document.TypeCashOut, at(2026, time.June, 2), document.SourceJournal, costJournalID)

	// BKK jurnal manual murni — bukan pembayaran vendor, bukan biaya tunai
	// (mis. setoran modal). Belum ada cetakan untuk ini di aplikasi mana pun.
	manualJournalID := seedJournal(t, db, at(2026, time.June, 3))
	manualDoc := issueFromSource(t, db, document.TypeCashOut, at(2026, time.June, 3), document.SourceJournal, manualJournalID)

	target, err := svc.ResolvePrintTargetByNumber(context.Background(), docTenant, apDoc.Number)
	if err != nil {
		t.Fatalf("resolve BKK vendor: %v", err)
	}
	if target.Kind != document.PrintAPPayment || target.ID != 9101 {
		t.Fatalf("target BKK vendor = %+v, mau {ap_payment 9101}", target)
	}

	target, err = svc.ResolvePrintTargetByNumber(context.Background(), docTenant, costDoc.Number)
	if err != nil {
		t.Fatalf("resolve BKK biaya: %v", err)
	}
	if target.Kind != document.PrintExpense || target.ID != 9201 {
		t.Fatalf("target BKK biaya = %+v, mau {expense 9201}", target)
	}

	target, err = svc.ResolvePrintTargetByNumber(context.Background(), docTenant, manualDoc.Number)
	if err != nil {
		t.Fatalf("resolve BKK manual: %v", err)
	}
	if target.Kind != document.PrintUnknown {
		t.Fatalf("target BKK manual = %+v, mau {unknown}", target)
	}
}

// Nomor yang tidak pernah terbit harus gagal jelas (ErrDocumentNotFound), tidak
// pernah dipetakan ke tebakan.
func TestResolvePrintTarget_NomorTakDikenalGagalJelas(t *testing.T) {
	db := docSetup(t)
	svc := document.NewService(db)

	_, err := svc.ResolvePrintTargetByNumber(context.Background(), docTenant, "BKK/2026/999999")
	if !errors.Is(err, document.ErrDocumentNotFound) {
		t.Fatalf("err = %v, mau ErrDocumentNotFound", err)
	}
}

// issueFromSource menerbitkan satu dokumen dengan SourceTable/SourceID
// eksplisit — dipakai untuk meniru asal dokumen kwitansi/invoice/jurnal kas,
// berbeda dari issue() yang selalu memakai "it_sources" generik.
func issueFromSource(t *testing.T, db *gorm.DB, typeCode string, when time.Time, srcTable string, srcID uint64) *document.Document {
	t.Helper()
	var doc *document.Document
	err := db.Transaction(func(tx *gorm.DB) error {
		d, err := document.Issue(tx, docTenant, typeCode, when,
			document.Source{Table: srcTable, ID: srcID}, domain.FromInt(1_000_000), nil)
		if err != nil {
			return err
		}
		doc = d
		return nil
	})
	if err != nil {
		t.Fatalf("terbitkan %s dari %s#%d: %v", typeCode, srcTable, srcID, err)
	}
	return doc
}

func seedProject(t *testing.T, db *gorm.DB, id uint64) {
	t.Helper()
	if err := db.Exec(`INSERT INTO projects (id, tenant_id, name) VALUES (?, ?, ?)`,
		id, docTenant, "Proyek Uji Print Target").Error; err != nil {
		t.Fatalf("seed project: %v", err)
	}
}

func seedVendor(t *testing.T, db *gorm.DB, id uint64, name string) {
	t.Helper()
	if err := db.Exec(`INSERT INTO vendors (id, tenant_id, name) VALUES (?, ?, ?)`,
		id, docTenant, name).Error; err != nil {
		t.Fatalf("seed vendor: %v", err)
	}
}

func seedJournal(t *testing.T, db *gorm.DB, when time.Time) uint64 {
	t.Helper()
	res := db.Exec(`INSERT INTO journal_entries (tenant_id, date, description, reference)
		VALUES (?, ?, ?, ?)`, docTenant, when, "jurnal uji print target", "")
	if res.Error != nil {
		t.Fatalf("seed journal: %v", res.Error)
	}
	var id uint64
	if err := db.Raw(`SELECT id FROM journal_entries WHERE tenant_id = ? AND date = ? ORDER BY id DESC LIMIT 1`,
		docTenant, when).Scan(&id).Error; err != nil {
		t.Fatalf("baca id journal: %v", err)
	}
	return id
}

func seedAPPayment(t *testing.T, db *gorm.DB, id, vendorID, journalID uint64) {
	t.Helper()
	if err := db.Exec(`INSERT INTO ap_payments
		(id, tenant_id, vendor_id, payment_kind, payment_date, amount, cash_account_code, journal_entry_id)
		VALUES (?, ?, ?, 'invoice', CURDATE(), 1000000, '1-1000', ?)`,
		id, docTenant, vendorID, journalID).Error; err != nil {
		t.Fatalf("seed ap_payment: %v", err)
	}
}

func seedCostEntry(t *testing.T, db *gorm.DB, id, projectID, journalID uint64) {
	t.Helper()
	if err := db.Exec(`INSERT INTO cost_entries
		(id, tenant_id, project_id, category, amount, payment_method, bank_account_code, date, journal_entry_id)
		VALUES (?, ?, ?, 'hard', 1000000, 'bank', '1-1000', CURDATE(), ?)`,
		id, docTenant, projectID, journalID).Error; err != nil {
		t.Fatalf("seed cost_entry: %v", err)
	}
}

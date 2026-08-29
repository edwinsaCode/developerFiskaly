//go:build integration

package document_test

// W-3.4 — audit INV-DOC-1 terhadap MySQL nyata.
//
// Setiap larangan pada acceptance criterion owner ditanam sebagai kerusakan
// nyata di database, lalu dibuktikan bahwa audit MENEMUKANNYA. Audit yang hanya
// diuji pada data sehat tidak membuktikan apa pun: laporan yang selalu bersih
// juga lulus uji semacam itu.
//
//	A-1  buku sehat → clean, tanpa temuan palsu
//	A-2  jurnal kas terposting tanpa dokumen → tertangkap
//	A-3  saldo awal TIDAK dianggap pelanggaran (dan dihitung sebagai exempt)
//	A-4  dokumen kas bernomor yang tidak dipegang jurnal → tertangkap
//	A-5  dokumen menunjuk jurnal hantu → tertangkap, alasan spesifik
//	A-6  satu pergerakan kas dengan dua dokumen → tertangkap
//	A-7  satu dokumen untuk dua jurnal ditolak schema; audit tetap 0
//	A-8  counter seri tertinggal → tertangkap (lubang seri: riwayat, bukan pelanggaran)
//	A-9  batas `limit` memotong daftar tanpa memalsukan jumlahnya

import (
	"context"
	"testing"
	"time"

	"gorm.io/gorm"

	"esaproperti/internal/document"
)

func auditCheck(t *testing.T, rep *document.AuditReport, code string) document.AuditCheck {
	t.Helper()
	for _, c := range rep.Checks {
		if c.Code == code {
			return c
		}
	}
	t.Fatalf("pemeriksaan %q tidak ada di laporan", code)
	return document.AuditCheck{}
}

func auditRun(t *testing.T, db *gorm.DB, limit int) *document.AuditReport {
	t.Helper()
	rep, err := document.AuditCashDocuments(context.Background(), db, bfTenant, limit)
	if err != nil {
		t.Fatalf("audit: %v", err)
	}
	return rep
}

// A-1/A-3 — buku sehat. Semua jurnal kas berdokumen kecuali saldo awal, yang
// memang dikecualikan. Tidak boleh ada satu pun temuan.
func TestW34_Audit_BukuSehatBersih(t *testing.T) {
	db := bfSetup(t)
	bank, _, lawan := bfAccounts(t, db)

	jKas := bfPostedJournal(t, db, "system", nil,
		bfLine{bank, "5000000", "0"}, bfLine{lawan, "0", "5000000"})
	if err := db.Transaction(func(tx *gorm.DB) error {
		_, e := document.IssueForJournal(tx, bfTenant, document.TypeCashIn, time.Now(),
			jKas, jlMoney(t, "5000000"), nil)
		return e
	}); err != nil {
		t.Fatalf("terbitkan BKM: %v", err)
	}
	// Saldo awal: kas didebit, tanpa dokumen — dan itu benar (W-3.3 §F.1).
	bfPostedJournal(t, db, document.SourceOpeningBalance, nil,
		bfLine{bank, "9000000", "0"}, bfLine{lawan, "0", "9000000"})

	rep := auditRun(t, db, 0)
	if !rep.Clean || rep.Violations != 0 {
		t.Fatalf("buku sehat dilaporkan melanggar: %d temuan, checks=%+v", rep.Violations, rep.Checks)
	}
	if rep.CashPosted != 2 || rep.Documented != 1 || rep.Exempt != 1 {
		t.Fatalf("ringkasan salah: posted=%d documented=%d exempt=%d",
			rep.CashPosted, rep.Documented, rep.Exempt)
	}
}

// A-2 — pelanggaran paling umum: kas bergerak, tidak ada bukti bernomor.
func TestW34_Audit_JurnalKasTanpaDokumen(t *testing.T) {
	db := bfSetup(t)
	bank, _, lawan := bfAccounts(t, db)

	jTelanjang := bfPostedJournal(t, db, "termin", nil,
		bfLine{bank, "1500000", "0"}, bfLine{lawan, "0", "1500000"})

	rep := auditRun(t, db, 0)
	c := auditCheck(t, rep, document.AuditCashWithoutDocument)
	if c.Count != 1 {
		t.Fatalf("temuan = %d, want 1 (checks=%+v)", c.Count, rep.Checks)
	}
	f := c.Findings[0]
	if f.JournalID != jTelanjang {
		t.Errorf("jurnal tertuduh = %d, want %d", f.JournalID, jTelanjang)
	}
	if f.Source != "termin" || f.Amount != "1500000.0000" {
		t.Errorf("temuan tidak informatif: %+v", f)
	}
	if rep.Clean {
		t.Error("laporan menyatakan bersih padahal ada pelanggaran")
	}
}

// A-4/A-5 — nomor terbakar tanpa transaksi yang berhasil, dua bentuknya.
func TestW34_Audit_DokumenTanpaTransaksi(t *testing.T) {
	db := bfSetup(t)

	// Yatim: BKK terbit dari baris bisnis, tidak ada jurnal yang memegangnya.
	var yatim *document.Document
	if err := db.Transaction(func(tx *gorm.DB) error {
		var e error
		yatim, e = document.Issue(tx, bfTenant, document.TypeCashOut, time.Now(),
			document.Source{Table: "expense_payments", ID: 77}, jlMoney(t, "250000"), nil)
		return e
	}); err != nil {
		t.Fatalf("terbitkan BKK yatim: %v", err)
	}
	// Hantu: dokumen menunjuk jurnal yang tidak pernah ada.
	var hantu *document.Document
	if err := db.Transaction(func(tx *gorm.DB) error {
		var e error
		hantu, e = document.Issue(tx, bfTenant, document.TypeCashIn, time.Now(),
			document.Source{Table: document.SourceJournal, ID: 999_999_999}, jlMoney(t, "10000"), nil)
		return e
	}); err != nil {
		t.Fatalf("terbitkan BKM hantu: %v", err)
	}

	rep := auditRun(t, db, 0)
	c := auditCheck(t, rep, document.AuditDocumentWithoutTx)
	if c.Count != 2 {
		t.Fatalf("temuan = %d, want 2: %+v", c.Count, c.Findings)
	}
	alasan := map[uint64]string{}
	for _, f := range c.Findings {
		alasan[f.DocumentID] = f.Detail
	}
	if got := alasan[hantu.ID]; got != "menunjuk jurnal yang tidak ada" {
		t.Errorf("alasan dokumen hantu = %q", got)
	}
	if got := alasan[yatim.ID]; got != "nomor terbit tetapi tidak ada jurnal kas yang memegangnya" {
		t.Errorf("alasan dokumen yatim = %q", got)
	}
}

// A-6 — bentuk yang lolos dari KEDUA unique index: jurnal memegang kwitansi,
// tetapi ada juga BKM yang lahir dari jurnal itu sendiri. Dua bukti untuk satu
// kali uang berpindah.
func TestW34_Audit_SatuPergerakanDuaDokumen(t *testing.T) {
	db := bfSetup(t)
	bank, _, lawan := bfAccounts(t, db)

	j := bfPostedJournal(t, db, "termin", nil,
		bfLine{bank, "3000000", "0"}, bfLine{lawan, "0", "3000000"})
	if err := db.Transaction(func(tx *gorm.DB) error {
		kwitansi, e := document.Issue(tx, bfTenant, document.TypeHousePayment, time.Now(),
			document.Source{Table: "receipts", ID: 555}, jlMoney(t, "3000000"), nil)
		if e != nil {
			return e
		}
		if e = document.LinkJournal(tx, bfTenant, kwitansi.ID, j); e != nil {
			return e
		}
		// Dokumen kedua: sumbernya jurnal, jadi uk_doc_source tidak terusik.
		_, e = document.Issue(tx, bfTenant, document.TypeCashIn, time.Now(),
			document.Source{Table: document.SourceJournal, ID: j}, jlMoney(t, "3000000"), nil)
		return e
	}); err != nil {
		t.Fatalf("tanam dokumen ganda: %v", err)
	}

	rep := auditRun(t, db, 0)
	c := auditCheck(t, rep, document.AuditDoubleDocument)
	if c.Count != 1 || c.Findings[0].JournalID != j {
		t.Fatalf("dokumen ganda tidak tertangkap: %+v", c)
	}
	// BKM liar itu juga tidak dipegang jurnal mana pun — kerusakan yang sama
	// terbaca dari dua sudut, dan audit memang harus melaporkan keduanya.
	if got := auditCheck(t, rep, document.AuditDocumentWithoutTx); got.Count != 1 {
		t.Errorf("dokumen liar tidak terbaca sebagai nomor tanpa transaksi: %+v", got)
	}
}

// A-7 — satu dokumen dipakai dua jurnal ditolak di level schema (uk_je_document).
// Yang diuji: penjaganya masih hidup, dan audit membaca 0 karena memang bersih —
// bukan karena querynya salah.
func TestW34_Audit_DokumenBersamaDitolakSchema(t *testing.T) {
	db := bfSetup(t)
	bank, _, lawan := bfAccounts(t, db)

	j1 := bfPostedJournal(t, db, "system", nil,
		bfLine{bank, "100000", "0"}, bfLine{lawan, "0", "100000"})
	j2 := bfPostedJournal(t, db, "system", nil,
		bfLine{bank, "100000", "0"}, bfLine{lawan, "0", "100000"})

	var doc *document.Document
	if err := db.Transaction(func(tx *gorm.DB) error {
		var e error
		doc, e = document.IssueForJournal(tx, bfTenant, document.TypeCashIn, time.Now(),
			j1, jlMoney(t, "100000"), nil)
		return e
	}); err != nil {
		t.Fatalf("terbitkan BKM: %v", err)
	}
	if err := db.Exec(`UPDATE journal_entries SET document_id = ? WHERE id = ? AND tenant_id = ?`,
		doc.ID, j2, bfTenant).Error; err == nil {
		t.Fatal("uk_je_document tidak menolak dokumen yang dipakai dua jurnal")
	}

	if c := auditCheck(t, auditRun(t, db, 0), document.AuditSharedDocument); c.Count != 0 {
		t.Fatalf("temuan palsu dokumen bersama: %+v", c.Findings)
	}
}

// A-8 — counter seri tertinggal di belakang dokumen yang sudah terbit: penerbitan
// berikutnya mengarah ke nomor yang sudah dipakai. Lubang di seri sengaja TIDAK
// diuji sebagai pelanggaran — lihat alasannya di auditSequenceBehind.
func TestW34_Audit_SeriTertinggal(t *testing.T) {
	db := bfSetup(t)
	bank, _, lawan := bfAccounts(t, db)

	j := bfPostedJournal(t, db, "system", nil,
		bfLine{bank, "100000", "0"}, bfLine{lawan, "0", "100000"})
	if err := db.Transaction(func(tx *gorm.DB) error {
		_, e := document.IssueForJournal(tx, bfTenant, document.TypeCashIn, time.Now(),
			j, jlMoney(t, "100000"), nil)
		return e
	}); err != nil {
		t.Fatalf("terbitkan BKM: %v", err)
	}
	if c := auditCheck(t, auditRun(t, db, 0), document.AuditSequenceBehind); c.Count != 0 {
		t.Fatalf("seri sehat dilaporkan tertinggal: %+v", c.Findings)
	}

	// Seri MAJU tanpa dokumen (lubang) — riwayat, bukan pelanggaran.
	if err := db.Exec(`UPDATE document_sequences SET last_val = last_val + 3
		WHERE tenant_id = ? AND document_type_code = ?`, bfTenant, document.TypeCashIn).Error; err != nil {
		t.Fatalf("majukan seri: %v", err)
	}
	if c := auditCheck(t, auditRun(t, db, 0), document.AuditSequenceBehind); c.Count != 0 {
		t.Fatalf("lubang seri tidak boleh dihitung sebagai pelanggaran: %+v", c.Findings)
	}

	// Seri MUNDUR di belakang dokumen terbit — nomor berikutnya akan bentrok.
	if err := db.Exec(`UPDATE document_sequences SET last_val = 0
		WHERE tenant_id = ? AND document_type_code = ?`, bfTenant, document.TypeCashIn).Error; err != nil {
		t.Fatalf("mundurkan seri: %v", err)
	}
	c := auditCheck(t, auditRun(t, db, 0), document.AuditSequenceBehind)
	if c.Count != 1 {
		t.Fatalf("seri tertinggal tidak tertangkap: %+v", c)
	}
	if c.Findings[0].Source != document.TypeCashIn {
		t.Errorf("temuan tidak menyebut seri yang tertinggal: %+v", c.Findings[0])
	}
}

// A-9 — `limit` memotong daftar temuan, TIDAK memotong jumlahnya. Laporan yang
// diam-diam terpotong terbaca lebih sehat daripada bukunya.
func TestW34_Audit_LimitTidakMemalsukanJumlah(t *testing.T) {
	db := bfSetup(t)
	bank, _, lawan := bfAccounts(t, db)
	for i := 0; i < 5; i++ {
		bfPostedJournal(t, db, "system", nil,
			bfLine{bank, "100000", "0"}, bfLine{lawan, "0", "100000"})
	}

	rep := auditRun(t, db, 2)
	c := auditCheck(t, rep, document.AuditCashWithoutDocument)
	if c.Count != 5 {
		t.Fatalf("jumlah temuan = %d, want 5", c.Count)
	}
	if len(c.Findings) != 2 || !c.Truncated {
		t.Fatalf("pemotongan tidak jujur: %d baris, truncated=%v", len(c.Findings), c.Truncated)
	}
	if rep.Violations != 5 {
		t.Errorf("total pelanggaran = %d, want 5", rep.Violations)
	}
}

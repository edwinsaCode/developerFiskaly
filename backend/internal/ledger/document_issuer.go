package ledger

// W-3.2 — seam penerbitan dokumen untuk jurnal kas (INV-DOC-1).
//
// Masalah yang diselesaikan berkas ini: dokumen HARUS terbit di transaksi yang
// sama dengan posting jurnalnya, sementara PostingService sengaja tidak tahu
// apa-apa soal database — ia hanya memegang JournalStore.
//
// Solusinya: issuer MENUMPANG pada JournalStore. GORMRepository yang dibangun
// dari sebuah `*gorm.DB` transaksi otomatis menyerahkan issuer yang terikat
// transaksi ITU. Konsekuensinya `withJournals(txStore)` — yang sudah dipakai
// CreateAndPost/Reverse — ikut memindahkan issuer-nya tanpa satu baris pun
// plumbing tambahan, dan store in-memory di unit test cukup TIDAK memenuhi
// interface ini untuk berjalan seperti dulu.
//
// Alternatif yang ditolak: menambah parameter pada JournalTx.InTx. Itu memaksa
// setiap pemanggil Reverse yang sudah punya transaksi sendiri (commission,
// cancellation) meneruskan issuer secara manual — dan satu pemanggil yang lupa
// berarti jurnal kas terposting tanpa dokumen, persis lubang yang ditutup W-3.

import (
	"context"
	"fmt"
	"time"

	"gorm.io/gorm"

	"esaproperti/internal/document"
	"esaproperti/internal/domain"
)

// ── Deklarasi dokumen ───────────────────────────────────────────────────────

// DocumentSpec menyatakan dokumen apa yang membuktikan sebuah jurnal.
//
// Tepat satu dari dua jalur dipakai:
//
//	TypeCode           — terbitkan dokumen BARU jenis ini (BKM/BKK/BTP/RFC/KWD)
//	ExistingDocumentID — tautkan dokumen yang SUDAH terbit dari baris bisnis
//	                     (KWT dari receipts, KWB dari booking, KWR dari titipan)
//
// Spec kosong berarti "jurnal ini bukan pergerakan kas". Penegakan bahwa jurnal
// kas tidak boleh berspec kosong dipasang di W-3.5 (DocumentChecker), bukan di
// sini — supaya seluruh jalur bisa disambungkan lebih dulu tanpa memutus alur
// yang belum sempat diperbaiki.
// ReversesDocumentID hanya bermakna bersama TypeCode = JR: dokumen pembalik
// mendapat nomor sendiri (D-W3-5, ledger append-only) tetapi menunjuk balik ke
// dokumen yang dibatalkan, sehingga penelusuran "bukti mana yang dibalik oleh
// bukti mana" tidak bergantung pada pencocokan nominal.
type DocumentSpec struct {
	TypeCode           string
	ExistingDocumentID uint64
	ReversesDocumentID uint64
}

// IsZero melaporkan spec tanpa dokumen apa pun.
func (d DocumentSpec) IsZero() bool { return d.TypeCode == "" && d.ExistingDocumentID == 0 }

// Kode jenis dokumen kas yang dipakai jalur ledger. Nilainya dipinjam dari
// package document supaya tidak ada dua daftar konstanta yang bisa berselisih.
const (
	DocCashIn           = document.TypeCashIn           // BKM
	DocCashOut          = document.TypeCashOut          // BKK
	DocThirdPartyPayout = document.TypeThirdPartyPayout // BTP
	DocCustomerRefund   = document.TypeCustomerRefund   // RFC
	DocKPRDisbursement  = document.TypeKPRDisbursement  // KWD
	DocJournalReversal  = document.TypeJournalReversal  // JR
)

// IssuedDocument adalah hasil penerbitan yang perlu diketahui pemanggil.
type IssuedDocument struct {
	ID     uint64
	Number string
}

// ── Seam ────────────────────────────────────────────────────────────────────

// DocumentIssuer menerbitkan dan menautkan dokumen DI DALAM transaksi yang
// sedang berjalan. Diimplementasi GORMRepository; store in-memory tidak.
type DocumentIssuer interface {
	IssueForJournal(ctx context.Context, tenantID, journalID uint64, typeCode string,
		issuedAt time.Time, amount domain.Money, createdBy *uint64) (*IssuedDocument, error)
	LinkExisting(ctx context.Context, tenantID, documentID, journalID uint64) error
	IssueReversal(ctx context.Context, tenantID, reversingJournalID, originalDocumentID uint64,
		issuedAt time.Time, amount domain.Money, createdBy *uint64) (*IssuedDocument, error)
	// DocumentIDOf mengembalikan dokumen sebuah jurnal, 0 bila belum bertaut.
	DocumentIDOf(ctx context.Context, tenantID, journalID uint64) (uint64, error)
}

// documentCapable adalah JournalStore yang tahu cara menerbitkan dokumen di
// transaksinya sendiri.
type documentCapable interface {
	Documents() DocumentIssuer
}

// Documents menyerahkan issuer yang terikat pada `r.db`. Bila r dibangun dari
// transaksi (lihat InTx / NewGORMRepository(tx)), issuer ini menulis di
// transaksi tersebut — bukan di koneksi lain.
func (r *GORMRepository) Documents() DocumentIssuer { return gormDocuments{db: r.db} }

// documents mengambil issuer dari store yang sedang terpasang. nil berarti
// store-nya bukan GORM (unit test) — jalur lama, tanpa dokumen.
func (s *PostingService) documents() DocumentIssuer {
	dc, ok := s.journals.(documentCapable)
	if !ok {
		return nil
	}
	return dc.Documents()
}

// ── Adapter GORM ────────────────────────────────────────────────────────────

type gormDocuments struct{ db *gorm.DB }

func (g gormDocuments) IssueForJournal(ctx context.Context, tenantID, journalID uint64, typeCode string,
	issuedAt time.Time, amount domain.Money, createdBy *uint64) (*IssuedDocument, error) {
	doc, err := document.IssueForJournal(g.db.WithContext(ctx), tenantID, typeCode, issuedAt, journalID, amount, createdBy)
	if err != nil {
		return nil, err
	}
	return &IssuedDocument{ID: doc.ID, Number: doc.Number}, nil
}

func (g gormDocuments) LinkExisting(ctx context.Context, tenantID, documentID, journalID uint64) error {
	return document.LinkJournal(g.db.WithContext(ctx), tenantID, documentID, journalID)
}

func (g gormDocuments) IssueReversal(ctx context.Context, tenantID, reversingJournalID, originalDocumentID uint64,
	issuedAt time.Time, amount domain.Money, createdBy *uint64) (*IssuedDocument, error) {
	doc, err := document.IssueReversal(g.db.WithContext(ctx), tenantID, issuedAt,
		reversingJournalID, originalDocumentID, amount, createdBy)
	if err != nil {
		return nil, err
	}
	return &IssuedDocument{ID: doc.ID, Number: doc.Number}, nil
}

func (g gormDocuments) DocumentIDOf(ctx context.Context, tenantID, journalID uint64) (uint64, error) {
	doc, err := document.FindByJournal(g.db.WithContext(ctx), tenantID, journalID)
	if err != nil || doc == nil {
		return 0, err
	}
	return doc.ID, nil
}

// ── Deteksi & nilai pergerakan kas ──────────────────────────────────────────

// cashMovement melaporkan apakah sekumpulan baris menyentuh kas dan berapa
// nilai pergerakannya.
//
// Definisi "menyentuh kas" COA-driven lewat AccountInRole(RoleCashBank) —
// `accounts.category IN ('cash','bank')`, satu otoritas yang sama dengan
// ListCashBankAccounts dan ValidatePaymentAccount. Daftar kode hardcoded adalah
// cara yang sudah terbukti salah (B-1 di tax/model.go).
//
// Nilai dokumen = sisi kas yang lebih besar. Untuk jurnal biasa hanya satu sisi
// yang terisi; untuk pemindahan antar rekening (kas keluar di satu bank, masuk
// di bank lain) keduanya terisi dan sama besar, sehingga hasilnya tetap nilai
// pemindahan — bukan dua kali lipatnya.
func cashMovement(accounts map[uint64]*Account, lines []LineInput) (bool, domain.Money) {
	debit, credit := domain.Zero, domain.Zero
	touched := false
	for _, l := range lines {
		a := accounts[l.AccountID]
		if !AccountInRole(a, RoleCashBank) {
			continue
		}
		touched = true
		debit = debit.Add(l.Debit)
		credit = credit.Add(l.Credit)
	}
	if !touched {
		return false, domain.Zero
	}
	if credit.GreaterThan(debit) {
		return true, credit
	}
	return true, debit
}

// cashMovementOfEntry adalah cashMovement untuk jurnal yang sudah tersimpan.
func cashMovementOfEntry(accounts map[uint64]*Account, lines []JournalLine) (bool, domain.Money) {
	in := make([]LineInput, len(lines))
	for i, l := range lines {
		in[i] = LineInput{AccountID: l.AccountID, Debit: l.Debit, Credit: l.Credit}
	}
	return cashMovement(accounts, in)
}

// DefaultCashSpec menentukan dokumen bawaan untuk sekumpulan baris jurnal yang
// tidak lahir dari baris bisnis: kas BERTAMBAH → BKM, kas BERKURANG → BKK.
// Spec kosong bila barisnya tidak menyentuh kas.
//
// Hanya dua jenis ini yang boleh dipilih otomatis. BTP dan RFC menyatakan uang
// yang bergerak itu MILIK pihak ketiga atau MILIK customer — fakta yang tidak
// bisa disimpulkan dari arah debit/kredit dan harus datang dari jalur bisnisnya
// (charge.RecordPayout, cancellation.PayRefund). Menebaknya di sini berarti
// mencampur kas perusahaan dengan uang titipan di satu seri bukti.
func (s *PostingService) DefaultCashSpec(ctx context.Context, tenantID uint64, lines []LineInput) (DocumentSpec, error) {
	accounts, err := s.accountMap(ctx, tenantID, accountIDsOf(lines))
	if err != nil {
		return DocumentSpec{}, err
	}
	isCash, _ := cashMovement(accounts, lines)
	if !isCash {
		return DocumentSpec{}, nil
	}
	debit, credit := domain.Zero, domain.Zero
	for _, l := range lines {
		if !AccountInRole(accounts[l.AccountID], RoleCashBank) {
			continue
		}
		debit = debit.Add(l.Debit)
		credit = credit.Add(l.Credit)
	}
	if debit.GreaterThan(credit) {
		return DocumentSpec{TypeCode: DocCashIn}, nil
	}
	return DocumentSpec{TypeCode: DocCashOut}, nil
}

// EntryCashSpec adalah DefaultCashSpec untuk jurnal yang sudah tersimpan.
func (s *PostingService) EntryCashSpec(ctx context.Context, tenantID uint64, entry *JournalEntry) (DocumentSpec, error) {
	if entry == nil {
		return DocumentSpec{}, nil
	}
	lines := make([]LineInput, len(entry.Lines))
	for i, l := range entry.Lines {
		lines[i] = LineInput{AccountID: l.AccountID, Debit: l.Debit, Credit: l.Credit}
	}
	return s.DefaultCashSpec(ctx, tenantID, lines)
}

// ManualDocumentChoices mengembalikan jenis dokumen yang SAH dipilih operator
// untuk arah pergerakan kas sebuah jurnal manual (D-W3-4). nil = jurnalnya
// tidak menyentuh kas, jadi tidak butuh dokumen.
//
// Kas masuk hanya punya satu pilihan: KWT/KWB/KWR/KWD semuanya lahir dari baris
// bisnis (kwitansi customer, pencairan bank) dan tidak pernah dari jurnal
// manual, sehingga yang tersisa BKM. Kas keluar punya tiga, dan pilihannya
// menyatakan siapa PEMILIK uang yang keluar — fakta yang hanya diketahui
// operator, tidak bisa disimpulkan dari angka.
func ManualDocumentChoices(spec DocumentSpec) []string {
	switch spec.TypeCode {
	case DocCashIn:
		return []string{DocCashIn}
	case DocCashOut:
		return []string{DocCashOut, DocThirdPartyPayout, DocCustomerRefund}
	}
	return nil
}

// ResolveManualDocument memvalidasi pilihan operator terhadap arah pergerakan
// kas jurnalnya, dan mengisi pilihan tunggal bila memang hanya ada satu.
//
// Mengembalikan ErrDocumentTypeRequired bila jurnalnya menggerakkan kas keluar
// tanpa jenis yang dipilih, dan ErrDocumentTypeMismatch bila jenis yang dipilih
// bertentangan dengan arah kasnya — mis. BKM (kas masuk) untuk jurnal yang
// mengkredit bank.
func ResolveManualDocument(chosen string, spec DocumentSpec) (DocumentSpec, error) {
	choices := ManualDocumentChoices(spec)
	if len(choices) == 0 {
		if chosen != "" {
			return DocumentSpec{}, fmt.Errorf("%w: jurnal ini tidak menyentuh kas", ErrDocumentTypeMismatch)
		}
		return DocumentSpec{}, nil
	}
	if chosen == "" {
		if len(choices) == 1 {
			return DocumentSpec{TypeCode: choices[0]}, nil
		}
		return DocumentSpec{}, fmt.Errorf("%w: pilih salah satu dari %v", ErrDocumentTypeRequired, choices)
	}
	c := document.NormalizeCode(chosen)
	for _, allowed := range choices {
		if c == allowed {
			return DocumentSpec{TypeCode: c}, nil
		}
	}
	return DocumentSpec{}, fmt.Errorf("%w: %q tidak sah untuk arah kas jurnal ini (pilihan: %v)",
		ErrDocumentTypeMismatch, chosen, choices)
}

// accountMap memuat akun-akun yang dipakai baris jurnal, terindeks id.
func (s *PostingService) accountMap(ctx context.Context, tenantID uint64, ids []uint64) (map[uint64]*Account, error) {
	if len(ids) == 0 {
		return map[uint64]*Account{}, nil
	}
	accounts, err := s.accounts.FindByIDs(ctx, tenantID, ids)
	if err != nil {
		return nil, err
	}
	m := make(map[uint64]*Account, len(accounts))
	for _, a := range accounts {
		m[a.ID] = a
	}
	return m, nil
}

// applyDocument menerbitkan atau menautkan dokumen untuk jurnal yang BARU
// diposting, di transaksi yang sama.
//
// Dipanggil sesudah MarkPosted dan bukan sebelumnya: dokumen bernomor tanpa
// transaksi yang berhasil termasuk yang dilarang INV-DOC-1, jadi nomor baru
// boleh terbit setelah jurnalnya benar-benar jadi — dan karena keduanya satu
// transaksi, kegagalan di langkah ini membatalkan jurnalnya juga.
func (s *PostingService) applyDocument(ctx context.Context, tenantID uint64, entry *JournalEntry,
	spec DocumentSpec, amount domain.Money, createdBy *uint64) error {
	if spec.IsZero() {
		return nil
	}
	di := s.documents()
	if di == nil {
		// Store non-GORM (unit test in-memory): tidak ada tempat menyimpan
		// dokumen. Bukan fail-open produksi — wiring produksi selalu GORM.
		return nil
	}
	if spec.ExistingDocumentID != 0 {
		if err := di.LinkExisting(ctx, tenantID, spec.ExistingDocumentID, entry.ID); err != nil {
			return fmt.Errorf("tautkan dokumen ke jurnal %d: %w", entry.ID, err)
		}
		entry.DocumentID = &spec.ExistingDocumentID
		return nil
	}
	// issuedAt = SEKARANG, bukan entry.Date. Aturan W-2 (lihat fiscalYearFor):
	// nomor dokumen adalah identitas lembar bukti, dan seri tahun berjalan tidak
	// boleh disusupi dokumen yang tanggal transaksinya mundur ke tahun lalu.
	if spec.TypeCode == DocJournalReversal {
		doc, err := di.IssueReversal(ctx, tenantID, entry.ID, spec.ReversesDocumentID, time.Now(), amount, createdBy)
		if err != nil {
			return fmt.Errorf("terbitkan dokumen pembalik untuk jurnal %d: %w", entry.ID, err)
		}
		entry.DocumentID = &doc.ID
		return nil
	}
	doc, err := di.IssueForJournal(ctx, tenantID, entry.ID, spec.TypeCode, time.Now(), amount, createdBy)
	if err != nil {
		return fmt.Errorf("terbitkan dokumen %s untuk jurnal %d: %w", spec.TypeCode, entry.ID, err)
	}
	entry.DocumentID = &doc.ID
	return nil
}

// ── Penegakan INV-DOC-1 (W-3.5) ─────────────────────────────────────────────

// SourceOpeningBalance menandai jurnal saldo awal. Nilainya dipinjam dari
// package document supaya pengecualian backfill (W-3.3) dan pengecualian
// penegakan di sini tidak pernah bisa berselisih.
const SourceOpeningBalance = document.SourceOpeningBalance

// enforceDocument menolak jurnal yang menggerakkan kas tetapi selesai diposting
// tanpa dokumen bernomor. Karena seluruh jalur posting berjalan dalam satu
// transaksi (W-3.0), penolakan di sini membatalkan jurnalnya juga — kas tidak
// pernah benar-benar bergerak.
//
// Fail-CLOSED: yang diperiksa adalah keadaan akhir jurnal, bukan niat pemanggil.
// Pemanggil yang lupa menyatakan dokumen, salah urutan, atau menambah jalur kas
// baru tanpa membacanya sama sekali akan tertahan di sini. Itu sebabnya
// pemeriksaan ini tidak dipasang lewat `With…` seperti PeriodChecker: seam
// opt-in berarti setiap jalur baru harus ingat memasangnya, dan satu yang lupa
// sudah cukup untuk melubangi invariant ini.
//
// Dua pengecualian, keduanya sempit dan disengaja:
//
//   - `opening_balance` — mendebit kas untuk menetapkan POSISI, bukan
//     pergerakan. Tidak ada uang berpindah, jadi tidak ada bukti yang bisa
//     diterbitkan (sama dengan pengecualian backfill W-3.3 §F.1).
//   - store non-GORM (unit test in-memory) — tidak ada tempat menyimpan
//     dokumen sama sekali. Wiring produksi selalu GORM, jadi ini tidak pernah
//     menjadi fail-open di jalur nyata.
func (s *PostingService) enforceDocument(ctx context.Context, tenantID uint64, entry *JournalEntry) error {
	if entry == nil || entry.DocumentID != nil {
		return nil
	}
	if entry.Source == SourceOpeningBalance {
		return nil
	}
	if s.documents() == nil {
		return nil
	}
	accounts, err := s.accountMap(ctx, tenantID, accountIDsOfEntry(entry))
	if err != nil {
		return err
	}
	isCash, amount := cashMovementOfEntry(accounts, entry.Lines)
	if !isCash {
		return nil
	}
	return fmt.Errorf("%w: jurnal %d (%s, %s) menggerakkan kas %s tanpa dokumen",
		ErrDocumentRequired, entry.ID, entry.Source, entry.Description, amount)
}

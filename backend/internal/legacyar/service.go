package legacyar

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"

	"esaproperti/internal/billing"
	"esaproperti/internal/domain"
	"esaproperti/internal/ledger"
)

// DefaultControlAccount adalah akun kontrol piutang customer.
//
// Piutang lama memakai akun yang SAMA dengan piutang sistem (keputusan BD-1).
// Membuat akun "1-2050 Piutang Legacy" akan memecah satu kewajiban customer
// menjadi dua baris neraca yang harus dijumlahkan manual selamanya, padahal
// pemisahan yang benar-benar dibutuhkan — legacy vs sistem — sudah tersedia di
// tingkat sub-ledger lewat `source`.
const DefaultControlAccount = "1-2000"

// ── Seam lintas-paket ─────────────────────────────────────────────────────────

// LedgerBalanceReader membaca saldo posted satu akun, utuh maupun dipecah per
// source jurnal. Diimplementasi ledger.LedgerBalanceService — dideklarasikan di
// sini supaya legacyar tidak terikat pada tipe konkret dan bisa diuji tanpa DB.
//
// Pemecahan per source inilah yang membuat rekonsiliasi bisa memisahkan bagian
// saldo 1-2000 yang berasal dari SALDO AWAL (jatah piutang lama) dari bagian
// yang berasal dari operasi berjalan — BAST, penagihan, pelunasan penjualan.
// Alternatifnya, meminta paket sale/charge menghitung ulang "piutang yang sudah
// diakui", akan melahirkan definisi piutang kedua yang harus dijaga agar tidak
// menyimpang dari buku besar. Di sini buku besarnya sendiri yang menjawab.
type LedgerBalanceReader interface {
	AccountBalance(ctx context.Context, tenantID uint64, code string, asOf *time.Time) (domain.Money, error)
	AccountMovementBySource(ctx context.Context, tenantID uint64, code string, asOf *time.Time) (map[string]domain.Money, error)
}

// ReceiptCreator adalah seam ke MESIN KWITANSI YANG SUDAH ADA
// (billing.GORMRepository) — requirement W-7 perluasan (piutang proyek lama):
// jangan pernah membuat mesin kwitansi kedua. Diimplementasikan langsung oleh
// billing.GORMRepository (lihat billing.LegacyReceiptInput dan
// createLegacyReceiptInTx) dan dipanggil DI DALAM transaksi ReceivePayment
// yang sama, supaya kwitansi dan jurnal penerimaan atomik bersama.
type ReceiptCreator interface {
	CreateLegacyReceiptInTx(ctx context.Context, tx *gorm.DB, tenantID, createdBy uint64, in billing.LegacyReceiptInput) (*billing.Receipt, error)
}

// Service adalah pintu masuk seluruh operasi piutang proyek lama.
type Service struct {
	repo     *Repository
	db       *gorm.DB
	balance  LedgerBalanceReader
	receipts ReceiptCreator
}

func NewService(repo *Repository, db *gorm.DB) *Service {
	return &Service{repo: repo, db: db}
}

// WithBalanceReader memasang pembaca saldo buku besar (chaining, pola
// WithPeriodChecker di ledger).
func (s *Service) WithBalanceReader(b LedgerBalanceReader) *Service {
	s.balance = b
	return s
}

// WithReceiptCreator memasang mesin kwitansi yang sudah ada (billing). Bila
// tidak dipasang, ReceivePayment tetap berjalan (jurnal & sub-ledger tetap
// tercatat) tapi tidak menerbitkan kwitansi — dipakai test yang tidak
// menguji kwitansi, persis pola WithBalanceReader.
func (s *Service) WithReceiptCreator(r ReceiptCreator) *Service {
	s.receipts = r
	return s
}

// ── Unggah & pratinjau ────────────────────────────────────────────────────────

// UploadRequest adalah permintaan unggah berkas.
type UploadRequest struct {
	FileName  string
	Content   []byte
	AsOfDate  time.Time
	Notes     string
	CreatedBy *uint64
	// ControlAccount opsional; kosong berarti DefaultControlAccount.
	ControlAccount string
}

// Upload membaca berkas, menyimpannya sebagai batch DRAFT, dan mengembalikan
// pratinjau.
//
// Tidak ada satu pun baris piutang yang terbentuk di sini. Draft ada supaya
// pratinjau dan rekonsiliasi bisa dilihat sebelum apa pun mengikat — dan supaya
// commit nanti bekerja atas data yang SUDAH dibaca, bukan membaca ulang berkas
// yang bisa saja berbeda.
func (s *Service) Upload(ctx context.Context, tenantID uint64, req UploadRequest) (*Batch, error) {
	if len(req.Content) == 0 {
		return nil, fmt.Errorf("berkas kosong")
	}
	if req.AsOfDate.IsZero() {
		return nil, fmt.Errorf("tanggal cutoff wajib diisi")
	}
	control := strings.TrimSpace(req.ControlAccount)
	if control == "" {
		control = DefaultControlAccount
	}
	if err := s.validateControlAccount(ctx, tenantID, control); err != nil {
		return nil, err
	}

	sum := sha256.Sum256(req.Content)
	hash := hex.EncodeToString(sum[:])

	// Lapis idempotensi #1: isi berkas identik yang SUDAH ter-commit.
	if dup, err := s.repo.FindCommittedByHash(ctx, tenantID, hash); err != nil {
		return nil, err
	} else if dup != nil {
		return nil, fmt.Errorf("%w (batch #%d, %s)", ErrBatchDuplicate, dup.ID,
			dup.CreatedAt.Format("2 Jan 2006 15:04"))
	}

	parsed, err := ParseWorkbook(bytes.NewReader(req.Content))
	if err != nil {
		return nil, err
	}
	if len(parsed.Rows) == 0 {
		return nil, ErrNoValidRows
	}

	// Lapis idempotensi #2: nomor rujukan yang sudah dipakai piutang lain.
	refs := make([]string, 0, len(parsed.Rows))
	for _, r := range parsed.Rows {
		if r.ExternalRef != "" {
			refs = append(refs, r.ExternalRef)
		}
	}
	taken, err := s.repo.ExternalRefExists(ctx, tenantID, refs)
	if err != nil {
		return nil, err
	}

	batch := &Batch{
		TenantID:           tenantID,
		FileName:           req.FileName,
		FileSize:           int64(len(req.Content)),
		FileHash:           hash,
		AsOfDate:           truncDate(req.AsOfDate),
		ControlAccountCode: control,
		Status:             BatchDraft,
		Notes:              req.Notes,
		CreatedBy:          req.CreatedBy,
		TotalAmount:        domain.Zero,
	}

	rows := make([]BatchRow, 0, len(parsed.Rows))
	total := domain.Zero
	var valid, errCnt, warnCnt int
	for _, p := range parsed.Rows {
		br := BatchRow{
			TenantID:        tenantID,
			LineNo:          p.LineNo,
			RawCustomerName: p.Raw[colCustomerName],
			RawSourceLabel:  p.Raw[colSourceLabel],
			RawExternalRef:  p.Raw[colExternalRef],
			RawOutstanding:  p.Raw[colOutstanding],
			RawDueDate:      p.Raw[colDueDate],
			RawPhone:        p.Raw[colPhone],
			RawEmail:        p.Raw[colEmail],
			RawNotes:        p.Raw[colNotes],
			Amount:          p.Amount,
			DueDate:         p.DueDate,
			ParseStatus:     p.Status,
			Issues:          p.Issues,
		}
		if p.ExternalRef != "" && taken[p.ExternalRef] {
			br.Issues.add(colExternalRef,
				"nomor rujukan ini sudah dipakai piutang lama yang sudah diimpor")
			br.ParseStatus = ParseError
		}
		switch br.ParseStatus {
		case ParseError:
			errCnt++
		case ParseWarning:
			warnCnt++
			valid++
			total = total.Add(br.Amount)
		default:
			valid++
			total = total.Add(br.Amount)
		}
		rows = append(rows, br)
	}

	batch.RowCount = len(rows)
	batch.ValidCount = valid
	batch.ErrorCount = errCnt
	batch.WarningCount = warnCnt
	batch.TotalAmount = total

	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		r := s.repo.WithTx(tx)
		if err := r.CreateBatch(ctx, batch); err != nil {
			return err
		}
		for i := range rows {
			rows[i].BatchID = batch.ID
		}
		return r.InsertBatchRows(ctx, rows)
	})
	if err != nil {
		return nil, err
	}
	return batch, nil
}

// ── Rekonsiliasi ──────────────────────────────────────────────────────────────

// Reconciliation membandingkan rincian piutang terhadap saldo buku besar.
//
// Perbandingannya berbasis NOMINAL, bukan jumlah baris: dua puluh baris yang
// jumlahnya Rp 999 juta tidak "cocok" dengan buku besar Rp 1 miliar hanya
// karena barisnya lengkap.
type Reconciliation struct {
	ControlAccountCode string `json:"control_account_code"`
	ControlAccountName string `json:"control_account_name"`
	AsOfDate           string `json:"as_of_date"`

	// LedgerBalance adalah saldo akun kontrol di buku besar (posted, per cutoff).
	LedgerBalance domain.Money `json:"ledger_balance"`
	// OperationalMovement adalah bagian saldo itu yang lahir dari OPERASI
	// BERJALAN — BAST, penagihan, pelunasan penjualan. Ia bukan jatah piutang
	// lama dan karena itu dikeluarkan dari pembanding.
	OperationalMovement domain.Money `json:"operational_movement"`
	// LegacyPaymentEffect adalah pengurangan saldo akibat pelunasan piutang lama
	// (nilainya negatif atau nol). Ditampilkan terpisah supaya jelas bahwa
	// turunnya saldo 1-2000 setelah customer membayar bukan selisih.
	LegacyPaymentEffect domain.Money `json:"legacy_payment_effect"`
	// OpeningPortion = LedgerBalance − OperationalMovement − LegacyPaymentEffect.
	// Inilah saldo awal piutang yang tercatat di buku besar — angka yang benar
	// untuk dibandingkan dengan rincian piutang lama.
	OpeningPortion domain.Money `json:"opening_portion"`

	// LegacyExisting adalah nilai ASLI piutang lama yang sudah diimpor.
	// Aslinya, bukan sisanya: yang dibandingkan adalah saldo awal, dan saldo awal
	// tidak ikut berubah ketika customer mencicil.
	LegacyExisting domain.Money `json:"legacy_existing"`
	// Incoming adalah total berkas yang sedang dipratinjau (nol di luar impor).
	Incoming domain.Money `json:"incoming"`
	// SubledgerTotal = LegacyExisting + Incoming.
	SubledgerTotal domain.Money `json:"subledger_total"`
	// Difference = OpeningPortion − SubledgerTotal. Positif berarti buku besar
	// memuat piutang awal yang belum ada rinciannya; negatif berarti rinciannya
	// lebih besar daripada yang pernah dicatat di buku besar.
	Difference domain.Money `json:"difference"`
	Matched    bool         `json:"matched"`

	// LedgerAvailable=false berarti saldo buku besar tidak terbaca sama sekali.
	// Selisihnya lalu TIDAK bisa ditafsirkan, dan layar harus mengatakan itu
	// alih-alih menampilkan angka yang terlihat pasti.
	LedgerAvailable bool   `json:"ledger_available"`
	Note            string `json:"note,omitempty"`
}

// Reconcile menyusun rekonsiliasi. incoming boleh nol (di luar alur impor).
//
// Bentuknya sengaja "kupas lapis", bukan "bandingkan dua angka besar":
//
//	saldo 1-2000 per cutoff
//	 − mutasi operasi berjalan       (bukan jatah piutang lama)
//	 − efek pelunasan piutang lama   (sudah tercermin di sisa sub-ledger)
//	 = porsi saldo awal              ← dibandingkan dengan Σ nilai asli piutang lama
//
// Konsekuensinya rekonsiliasi tetap COCOK setelah customer legacy membayar:
// saldo buku besar turun, tapi turunnya sudah dijelaskan barisnya sendiri.
// Rekonsiliasi yang rusak setiap kali ada pembayaran akan segera diabaikan
// orang, dan alat kontrol yang diabaikan sama saja tidak ada.
func (s *Service) Reconcile(ctx context.Context, tenantID uint64, controlCode string, asOf time.Time, incoming domain.Money) (*Reconciliation, error) {
	if controlCode == "" {
		controlCode = DefaultControlAccount
	}
	rec := &Reconciliation{
		ControlAccountCode:  controlCode,
		AsOfDate:            asOf.Format("2006-01-02"),
		LedgerBalance:       domain.Zero,
		OperationalMovement: domain.Zero,
		LegacyPaymentEffect: domain.Zero,
		OpeningPortion:      domain.Zero,
		LegacyExisting:      domain.Zero,
		Incoming:            incoming,
		LedgerAvailable:     true,
	}
	if acc, err := s.repo.FindAccountByCode(ctx, tenantID, controlCode); err == nil && acc != nil {
		rec.ControlAccountName = acc.Name
	}

	// Saldo buku besar dibaca per TANGGAL CUTOFF, bukan posisi hari ini.
	// Membandingkan rincian per 31 Des 2024 dengan saldo hari ini akan selalu
	// meleset sebesar transaksi setelahnya — dan orang akan menutup selisih itu
	// dengan jurnal yang mengarang.
	cut := endOfDay(asOf)
	if s.balance == nil {
		rec.LedgerAvailable = false
		rec.Note = "saldo buku besar tidak terbaca — selisih di bawah belum bisa ditafsirkan"
	} else {
		bySource, err := s.balance.AccountMovementBySource(ctx, tenantID, controlCode, &cut)
		if err != nil {
			return nil, err
		}
		for src, v := range bySource {
			rec.LedgerBalance = rec.LedgerBalance.Add(v)
			switch src {
			case string(ledger.SourceOpeningBalance):
				rec.OpeningPortion = rec.OpeningPortion.Add(v)
			case SourceLegacyPayment:
				rec.LegacyPaymentEffect = rec.LegacyPaymentEffect.Add(v)
			default:
				rec.OperationalMovement = rec.OperationalMovement.Add(v)
			}
		}
	}

	existing, err := s.repo.SumOriginal(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	rec.LegacyExisting = existing

	rec.SubledgerTotal = rec.LegacyExisting.Add(rec.Incoming)
	rec.Difference = rec.OpeningPortion.Sub(rec.SubledgerTotal)
	rec.Matched = rec.LedgerAvailable && rec.Difference.IsZero()
	return rec, nil
}

// PreviewBatch mengembalikan batch, barisnya, dan rekonsiliasinya.
func (s *Service) PreviewBatch(ctx context.Context, tenantID, batchID uint64) (*Batch, []BatchRow, *Reconciliation, error) {
	b, err := s.repo.FindBatch(ctx, tenantID, batchID, false)
	if err != nil {
		return nil, nil, nil, err
	}
	rows, err := s.repo.ListBatchRows(ctx, tenantID, batchID)
	if err != nil {
		return nil, nil, nil, err
	}
	incoming := b.TotalAmount
	if b.Status == BatchCommitted {
		// Batch yang sudah masuk sudah ikut terhitung di LegacyExisting;
		// menambahkannya lagi akan menghitungnya dua kali.
		incoming = domain.Zero
	}
	rec, err := s.Reconcile(ctx, tenantID, b.ControlAccountCode, b.AsOfDate, incoming)
	if err != nil {
		return nil, nil, nil, err
	}
	return b, rows, rec, nil
}

// ── Commit ────────────────────────────────────────────────────────────────────

// CommitRequest menyelesaikan impor.
type CommitRequest struct {
	// SkipReason wajib bila rekonsiliasi tidak cocok DAN user tidak membuat
	// jurnal saldo awal. Selisih tidak diblokir — sistem tidak berhak memaksa
	// klien menutup selisih yang mungkin memang berasal dari luar — tetapi juga
	// tidak boleh lewat tanpa jejak.
	SkipReason string
	// OpeningCounterAccountCode, bila diisi, membuat jurnal SALDO AWAL untuk
	// menutup selisih: Dr akun kontrol / Cr akun lawan. Akun lawan DIPILIH USER
	// dari bagan akun — tidak ada akun clearing/ekuitas yang dihardcode di sini,
	// karena akun mana yang benar adalah keputusan akuntan klien.
	OpeningCounterAccountCode string
	OpeningDescription        string
	ActorID                   *uint64
}

// CommitResult adalah hasil impor.
type CommitResult struct {
	Batch          *Batch          `json:"batch"`
	ImportedCount  int             `json:"imported_count"`
	SkippedCount   int             `json:"skipped_count"`
	TotalImported  domain.Money    `json:"total_imported"`
	OpeningJournal *uint64         `json:"opening_journal_id,omitempty"`
	Reconciliation *Reconciliation `json:"reconciliation"`
}

// Commit membentuk piutang lama dari batch draft — SEKALI JALAN, ATOMIK.
//
// Tidak ada impor sebagian: satu baris gagal berarti seluruh batch batal. Impor
// separuh jadi adalah keadaan terburuk yang mungkin — saldonya salah, dan tidak
// ada yang tahu bagian mana yang sudah masuk.
func (s *Service) Commit(ctx context.Context, tenantID, batchID uint64, req CommitRequest) (*CommitResult, error) {
	var out CommitResult

	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		r := s.repo.WithTx(tx)

		b, err := r.FindBatch(ctx, tenantID, batchID, true)
		if err != nil {
			return err
		}
		if b.Status != BatchDraft {
			return fmt.Errorf("%w (status: %s)", ErrBatchNotDraft, b.Status)
		}
		if b.ErrorCount > 0 {
			return ErrHasErrorRows
		}
		if b.ValidCount == 0 {
			return ErrNoValidRows
		}
		// Lapis idempotensi #1, diperiksa ULANG di dalam transaksi: dua tab yang
		// mengunggah berkas sama bisa lolos pemeriksaan di Upload secara bersamaan.
		if dup, err := r.FindCommittedByHash(ctx, tenantID, b.FileHash); err != nil {
			return err
		} else if dup != nil {
			return fmt.Errorf("%w (batch #%d)", ErrBatchDuplicate, dup.ID)
		}

		rows, err := r.ListBatchRows(ctx, tenantID, batchID)
		if err != nil {
			return err
		}

		total := domain.Zero
		imported := 0
		for i := range rows {
			row := &rows[i]
			if row.ParseStatus == ParseError {
				return ErrHasErrorRows
			}
			rec := &Receivable{
				TenantID:           tenantID,
				BatchID:            b.ID,
				CustomerName:       row.RawCustomerName,
				SourceLabel:        row.RawSourceLabel,
				ExternalRef:        nilIfEmpty(strings.TrimSpace(row.RawExternalRef)),
				Phone:              trunc(strings.TrimSpace(row.RawPhone), 30),
				Email:              trunc(strings.TrimSpace(row.RawEmail), 120),
				ControlAccountCode: b.ControlAccountCode,
				AsOfDate:           b.AsOfDate,
				DueDate:            row.DueDate,
				OriginalAmount:     row.Amount,
				PaidAmount:         domain.Zero,
				Status:             StatusOpen,
				Notes:              trunc(strings.TrimSpace(row.RawNotes), 500),
				CreatedBy:          req.ActorID,
			}
			rec.CustomerName = trunc(strings.TrimSpace(rec.CustomerName), 200)
			rec.SourceLabel = trunc(strings.TrimSpace(rec.SourceLabel), 200)

			if err := r.CreateReceivable(ctx, rec); err != nil {
				if isDuplicateKey(err) {
					return fmt.Errorf("%w: baris %d (%s)", ErrExternalRefTaken, row.LineNo, row.RawExternalRef)
				}
				return err
			}
			if err := r.LinkBatchRow(ctx, tenantID, row.ID, rec.ID); err != nil {
				return err
			}
			if err := r.AppendAudit(ctx, &Audit{
				TenantID:           tenantID,
				LegacyReceivableID: rec.ID,
				Event:              EventImported,
				Amount:             rec.OriginalAmount,
				OutstandingAfter:   rec.OriginalAmount,
				Detail: fmt.Sprintf("Impor batch #%d (%s), posisi %s — tidak ada jurnal dibuat",
					b.ID, b.FileName, b.AsOfDate.Format("2 Jan 2006")),
				ActorID: req.ActorID,
			}); err != nil {
				return err
			}
			total = total.Add(rec.OriginalAmount)
			imported++
		}

		// Jurnal saldo awal OPSIONAL untuk menutup selisih. Ini satu-satunya
		// tempat di paket ini yang menyentuh buku besar saat impor, dan ia hanya
		// berjalan bila user MEMILIH akun lawannya sendiri.
		if code := strings.TrimSpace(req.OpeningCounterAccountCode); code != "" {
			// incoming = nol: baris-baris di atas SUDAH tersimpan di transaksi ini,
			// jadi rekonsiliasi di dalam postOpeningTx sudah menghitungnya lewat
			// LegacyExisting. Mengirim `total` ke sini akan menghitungnya dua kali
			// dan memposting jurnal saldo awal dua kali lipat.
			jid, err := s.postOpeningTx(ctx, tx, tenantID, b, code, domain.Zero, req)
			if err != nil {
				return err
			}
			b.OpeningJournalID = &jid
		}

		now := time.Now()
		hash := b.FileHash
		b.Status = BatchCommitted
		b.CommittedHash = &hash
		b.CommittedAt = &now
		b.CommittedBy = req.ActorID
		b.SkipReason = trunc(strings.TrimSpace(req.SkipReason), 500)
		if err := r.SaveBatch(ctx, b); err != nil {
			if isDuplicateKey(err) {
				return ErrBatchDuplicate
			}
			return err
		}

		out.Batch = b
		out.ImportedCount = imported
		out.SkippedCount = len(rows) - imported
		out.TotalImported = total
		out.OpeningJournal = b.OpeningJournalID
		return nil
	})
	if err != nil {
		return nil, err
	}

	// Rekonsiliasi SESUDAH commit dihitung di luar transaksi supaya ia membaca
	// keadaan yang benar-benar tersimpan — itulah angka yang akan dilihat
	// auditor besok, bukan proyeksi kita atas transaksi yang belum selesai.
	rec, err := s.Reconcile(ctx, tenantID, out.Batch.ControlAccountCode, out.Batch.AsOfDate, domain.Zero)
	if err != nil {
		return nil, err
	}
	out.Reconciliation = rec
	return &out, nil
}

// postOpeningTx membuat jurnal saldo awal penutup selisih.
//
// Sumbernya `opening_balance` — sama dengan Saldo Awal biasa, sehingga ia
// dikecualikan INV-DOC-1 (posisi pembuka bukan pergerakan kas) dan dikecualikan
// penjaga tahun berjurnal di W-6, persis seperti saldo awal yang dibuat manual.
func (s *Service) postOpeningTx(ctx context.Context, tx *gorm.DB, tenantID uint64, b *Batch,
	counterCode string, incoming domain.Money, req CommitRequest) (uint64, error) {

	control, err := accountByCode(ctx, tx, tenantID, b.ControlAccountCode)
	if err != nil {
		return 0, err
	}
	counter, err := accountByCode(ctx, tx, tenantID, counterCode)
	if err != nil {
		return 0, ErrCounterAccount
	}
	if counter.ID == control.ID {
		return 0, fmt.Errorf("%w: akun lawan tidak boleh sama dengan akun kontrol", ErrCounterAccount)
	}

	// Selisih dihitung ULANG di dalam transaksi. Angka yang dikirim layar hanya
	// pratinjau; kalau ada jurnal lain masuk sementara user membaca layar,
	// memakai angka lama akan memposting nominal yang salah.
	rec, err := s.reconcileTx(ctx, tx, tenantID, b.ControlAccountCode, b.AsOfDate, incoming)
	if err != nil {
		return 0, err
	}
	amount := rec.Difference
	if amount.IsZero() {
		return 0, ErrOpeningNotAllowed
	}

	desc := strings.TrimSpace(req.OpeningDescription)
	if desc == "" {
		desc = fmt.Sprintf("Saldo Awal Piutang Proyek Lama per %s (batch #%d)",
			b.AsOfDate.Format("2 Jan 2006"), b.ID)
	}

	var lines []ledger.LineInput
	if amount.IsNeg() {
		// Buku besar KURANG dari rincian: akun kontrol perlu ditambah.
		pos := amount.Neg()
		lines = []ledger.LineInput{
			{AccountID: control.ID, Debit: pos, Description: "Piutang proyek lama"},
			{AccountID: counter.ID, Credit: pos, Description: desc},
		}
	} else {
		// Buku besar LEBIH dari rincian: akun kontrol perlu dikurangi.
		lines = []ledger.LineInput{
			{AccountID: counter.ID, Debit: amount, Description: desc},
			{AccountID: control.ID, Credit: amount, Description: "Penyesuaian piutang proyek lama"},
		}
	}

	txLedger := ledger.NewGORMRepository(tx)
	posting := ledger.NewPostingService(txLedger, txLedger).WithPeriodChecker(txLedger)
	entry, err := posting.CreateAndPost(ctx, ledger.CreateJournalRequest{
		TenantID:    tenantID,
		Date:        b.AsOfDate,
		Description: desc,
		Source:      ledger.SourceOpeningBalance,
		CreatedBy:   req.ActorID,
		Lines:       lines,
	})
	if err != nil {
		return 0, err
	}
	return entry.ID, nil
}

// reconcileTx adalah Reconcile yang membaca lewat transaksi berjalan.
func (s *Service) reconcileTx(ctx context.Context, tx *gorm.DB, tenantID uint64, controlCode string,
	asOf time.Time, incoming domain.Money) (*Reconciliation, error) {

	txSvc := &Service{repo: s.repo.WithTx(tx), db: tx}
	if s.balance != nil {
		txSvc.balance = ledger.NewLedgerBalanceService(ledger.NewQueryService(tx))
	}
	return txSvc.Reconcile(ctx, tenantID, controlCode, asOf, incoming)
}

// DiscardBatch membuang draft yang tidak jadi diimpor.
func (s *Service) DiscardBatch(ctx context.Context, tenantID, batchID uint64) error {
	b, err := s.repo.FindBatch(ctx, tenantID, batchID, false)
	if err != nil {
		return err
	}
	if b.Status != BatchDraft {
		return ErrBatchNotDraft
	}
	b.Status = BatchDiscarded
	return s.repo.SaveBatch(ctx, b)
}

// ── Util ──────────────────────────────────────────────────────────────────────

func (s *Service) validateControlAccount(ctx context.Context, tenantID uint64, code string) error {
	acc, err := s.repo.FindAccountByCode(ctx, tenantID, code)
	if err != nil {
		return err
	}
	if acc == nil {
		return fmt.Errorf("%w: akun %s tidak ada di bagan akun", ErrControlAccount, code)
	}
	// Akun kontrol harus benar-benar akun piutang menurut registry peran — satu
	// tempat yang mendefinisikan "akun mana yang berperan piutang" (S2).
	for _, c := range ledger.RoleCodeList(ledger.RoleReceivable) {
		if c == code {
			return nil
		}
	}
	return fmt.Errorf("%w: %s bukan akun piutang", ErrControlAccount, code)
}

func accountByCode(ctx context.Context, tx *gorm.DB, tenantID uint64, code string) (*ledger.Account, error) {
	var a ledger.Account
	err := tx.WithContext(ctx).Where("tenant_id = ? AND code = ?", tenantID, code).First(&a).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("akun %s tidak ditemukan", code)
		}
		return nil, err
	}
	return &a, nil
}

func truncDate(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.Local)
}

func endOfDay(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 23, 59, 59, 0, time.Local)
}

func trunc(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

func nilIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// isDuplicateKey mengenali pelanggaran unique key MySQL (1062).
func isDuplicateKey(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "1062") || strings.Contains(err.Error(), "Duplicate entry")
}

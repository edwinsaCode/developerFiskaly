package ledger

import (
	"context"
	"fmt"
	"time"

	"esaproperti/internal/domain"
)

// ── Interfaces (implemented by GORMRepository and mocks in tests) ─────────────

// AccountLookup reads accounts from storage.
type AccountLookup interface {
	FindByIDs(ctx context.Context, tenantID uint64, ids []uint64) ([]*Account, error)
}

// JournalStore persists journal entries and enforces immutability.
type JournalStore interface {
	Create(ctx context.Context, entry *JournalEntry) error
	FindByID(ctx context.Context, tenantID, id uint64) (*JournalEntry, error)
	MarkPosted(ctx context.Context, tenantID, id uint64, at time.Time) error
	HasReversingEntry(ctx context.Context, tenantID, id uint64) (bool, error)
	// UpdateDraft and DeleteDraft return ErrJournalAlreadyPosted if the entry is posted.
	UpdateDraft(ctx context.Context, tenantID, id uint64, updates map[string]interface{}) error
	DeleteDraft(ctx context.Context, tenantID, id uint64) error
}

// ── Input types ───────────────────────────────────────────────────────────────

// LineInput is the input for a single journal line.
type LineInput struct {
	AccountID   uint64
	Debit       domain.Money
	Credit      domain.Money
	ProjectID   *uint64
	PhaseID     *uint64
	UnitID      *uint64
	Description string
}

// CreateJournalRequest is the input for creating a new journal entry.
type CreateJournalRequest struct {
	TenantID    uint64
	Date        time.Time
	Description string
	Reference   string
	Lines       []LineInput
	// Source identifies the originating subsystem. Defaults to "system" if empty.
	Source    string
	CreatedBy *uint64
	// Document mendeklarasikan bukti bernomor untuk jurnal ini (W-3.2,
	// INV-DOC-1). Wajib diisi jalur yang menyentuh kas; diabaikan oleh Create
	// (draft belum menggerakkan kas) dan dipakai CreateAndPost.
	Document DocumentSpec
}

// PeriodChecker guards against posting into closed accounting periods.
// Implemented by GORMRepository; nil = no check (tests, seeds).
type PeriodChecker interface {
	IsPeriodClosed(ctx context.Context, tenantID uint64, date time.Time) (bool, error)
}

// JournalTx menjalankan fn dalam SATU transaksi database dan menyerahkan
// JournalStore yang terikat pada transaksi tersebut.
//
// W-3.0: dua operasi ledger menulis lebih dari sekali (Create+MarkPosted) dan
// dulu berjalan tanpa transaksi — gagal di tengah meninggalkan jurnal draft
// yatim. Nomor dokumen (W-3.2) hanya boleh ditumpuk di atas jalur yang atomik,
// karena itu seam ini mendahului numbering.
//
// Diimplementasi GORMRepository. nil = jalur non-transaksional (unit test
// dengan store in-memory) — perilaku lama dipertahankan persis.
type JournalTx interface {
	InTx(ctx context.Context, fn func(js JournalStore) error) error
}

// ── PostingService ────────────────────────────────────────────────────────────

// PostingService is the core double-entry accounting engine.
// It is the only place where journals are created, posted, or reversed.
type PostingService struct {
	accounts AccountLookup
	journals JournalStore
	periods  PeriodChecker // nil = no period checking (backward compat for tests)
	tx       JournalTx     // nil = no transaction wrapping (backward compat for tests)
}

// NewPostingService constructs a PostingService.
func NewPostingService(accounts AccountLookup, journals JournalStore) *PostingService {
	return &PostingService{accounts: accounts, journals: journals}
}

// WithPeriodChecker attaches a period guard to the PostingService (fluent, returns self).
func (s *PostingService) WithPeriodChecker(pc PeriodChecker) *PostingService {
	s.periods = pc
	return s
}

// WithJournalTx memasang seam transaksi (fluent, returns self). Wajib pada
// wiring produksi: tanpa ini CreateAndPost/Reverse berjalan tanpa transaksi.
func (s *PostingService) WithJournalTx(jt JournalTx) *PostingService {
	s.tx = jt
	return s
}

// withJournals mengembalikan salinan service yang terikat store lain.
// tx dikosongkan supaya panggilan bersarang berjalan inline di transaksi yang
// sudah berjalan — bukan membuka transaksi/savepoint baru.
func (s *PostingService) withJournals(js JournalStore) *PostingService {
	c := *s
	c.journals = js
	c.tx = nil
	return &c
}

// inTx menjalankan fn dengan service yang terikat satu transaksi.
// Tanpa seam transaksi, fn berjalan apa adanya (perilaku lama).
func (s *PostingService) inTx(ctx context.Context, fn func(*PostingService) error) error {
	if s.tx == nil {
		return fn(s)
	}
	return s.tx.InTx(ctx, func(js JournalStore) error {
		return fn(s.withJournals(js))
	})
}

// Create validates the journal request and persists it as a draft.
// Returns ErrJournalNotBalanced if Σdebit ≠ Σcredit (Invariant #1).
// Returns ErrLineInvalid if any line has debit+credit or neither.
// Returns ErrTooFewLines if fewer than 2 lines.
func (s *PostingService) Create(ctx context.Context, req CreateJournalRequest) (*JournalEntry, error) {
	if s.periods != nil {
		closed, err := s.periods.IsPeriodClosed(ctx, req.TenantID, req.Date)
		if err != nil {
			return nil, fmt.Errorf("cek periode: %w", err)
		}
		if closed {
			return nil, ErrPeriodClosed
		}
	}
	if err := validateLines(req.Lines); err != nil {
		return nil, err
	}
	if err := s.verifyAccountsExist(ctx, req.TenantID, req.Lines); err != nil {
		return nil, err
	}

	src := req.Source
	if src == "" {
		src = "system"
	}
	entry := &JournalEntry{
		TenantID:    req.TenantID,
		Date:        req.Date,
		Description: req.Description,
		Reference:   req.Reference,
		Source:      src,
		CreatedBy:   req.CreatedBy,
		Lines:       inputLinesToModel(req.TenantID, req.Lines),
	}
	if err := s.journals.Create(ctx, entry); err != nil {
		return nil, fmt.Errorf("create journal: %w", err)
	}
	return entry, nil
}

// Post marks a draft journal as posted (sets PostedAt).
// Once posted, the journal is immutable — corrections via Reverse only (Invariant #5).
// Returns ErrJournalAlreadyPosted if already posted.
// W-3.5 — penegakan INV-DOC-1 di jalur posting telanjang. Pemanggil yang
// menerbitkan dokumennya SESUDAH posting (booking, pelunasan) menautkannya
// lebih dulu lalu memanggil PostDraft; yang lupa berhenti di sini.
//
// Pemeriksaannya berada DI DALAM transaksi yang sama dengan MarkPosted: kalau
// ditolak, penandaan posted ikut batal. Tanpa itu penolakan justru melahirkan
// hal yang dilarang — jurnal kas terposting tanpa dokumen.
func (s *PostingService) Post(ctx context.Context, tenantID, entryID uint64) (*JournalEntry, error) {
	var out *JournalEntry
	err := s.inTx(ctx, func(ps *PostingService) error {
		entry, err := ps.post(ctx, tenantID, entryID)
		if err != nil {
			return err
		}
		if err := ps.enforceDocument(ctx, tenantID, entry); err != nil {
			return err
		}
		out = entry
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// post memposting tanpa memeriksa dokumen — dipakai jalur yang menerbitkan
// dokumennya sendiri sesudah MarkPosted dan memeriksanya di ujung transaksi.
func (s *PostingService) post(ctx context.Context, tenantID, entryID uint64) (*JournalEntry, error) {
	entry, err := s.journals.FindByID(ctx, tenantID, entryID)
	if err != nil {
		return nil, fmt.Errorf("find journal: %w", err)
	}
	if entry.IsPosted() {
		return nil, ErrJournalAlreadyPosted
	}
	if err := s.journals.MarkPosted(ctx, tenantID, entryID, time.Now()); err != nil {
		return nil, fmt.Errorf("mark posted: %w", err)
	}
	entry, err = s.journals.FindByID(ctx, tenantID, entryID)
	if err != nil {
		return nil, fmt.Errorf("reload after post: %w", err)
	}
	return entry, nil
}

// PostDraft memposting draft DAN menerbitkan dokumennya dalam SATU transaksi
// (W-3.2, jalur I-7/O-9: jurnal manual yang menyentuh kas).
//
// Dokumen sengaja tidak terbit saat draft dibuat: draft boleh saja tidak pernah
// diposting, dan nomor yang terpakai untuk transaksi yang tidak jadi termasuk
// yang dilarang INV-DOC-1. Kas baru bergerak di sini, jadi nomornya lahir di
// sini juga.
func (s *PostingService) PostDraft(ctx context.Context, tenantID, entryID uint64, spec DocumentSpec) (*JournalEntry, error) {
	var out *JournalEntry
	err := s.inTx(ctx, func(ps *PostingService) error {
		posted, err := ps.post(ctx, tenantID, entryID)
		if err != nil {
			return err
		}
		if !spec.IsZero() {
			accounts, err := ps.accountMap(ctx, tenantID, accountIDsOfEntry(posted))
			if err != nil {
				return err
			}
			_, amount := cashMovementOfEntry(accounts, posted.Lines)
			if err := ps.applyDocument(ctx, tenantID, posted, spec, amount, posted.CreatedBy); err != nil {
				return err
			}
		}
		if err := ps.enforceDocument(ctx, tenantID, posted); err != nil {
			return err
		}
		out = posted
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// TouchesCash melaporkan apakah jurnal tersimpan menyentuh akun kas/bank —
// dipakai lapisan HTTP untuk memutuskan apakah jenis dokumen wajib diminta.
func (s *PostingService) TouchesCash(ctx context.Context, tenantID uint64, entry *JournalEntry) (bool, error) {
	if entry == nil {
		return false, nil
	}
	accounts, err := s.accountMap(ctx, tenantID, accountIDsOfEntry(entry))
	if err != nil {
		return false, err
	}
	isCash, _ := cashMovementOfEntry(accounts, entry.Lines)
	return isCash, nil
}

// Draft memuat satu jurnal apa adanya — lapisan HTTP memerlukannya untuk
// memeriksa isi draft sebelum memutuskan syarat dokumennya.
func (s *PostingService) Draft(ctx context.Context, tenantID, entryID uint64) (*JournalEntry, error) {
	return s.journals.FindByID(ctx, tenantID, entryID)
}

func accountIDsOfEntry(e *JournalEntry) []uint64 {
	ids := make([]uint64, 0, len(e.Lines))
	for _, l := range e.Lines {
		ids = append(ids, l.AccountID)
	}
	return ids
}

// CreateAndPost membuat lalu memposting satu jurnal dalam SATU transaksi.
//
// Inilah jalur yang wajib dipakai setiap kali jurnal langsung diposting tanpa
// tahap draft. Create+Post terpisah bisa berhenti di tengah dan meninggalkan
// draft yatim; di jalur kas, kegagalan seperti itu berarti kas bergerak tanpa
// dokumen yang sah (INV-DOC-1) atau nomor terpakai untuk transaksi yang tidak
// pernah jadi.
func (s *PostingService) CreateAndPost(ctx context.Context, req CreateJournalRequest) (*JournalEntry, error) {
	var out *JournalEntry
	err := s.inTx(ctx, func(ps *PostingService) error {
		entry, err := ps.Create(ctx, req)
		if err != nil {
			return err
		}
		posted, err := ps.post(ctx, req.TenantID, entry.ID)
		if err != nil {
			return err
		}
		// W-3.2 — dokumen terbit di transaksi yang sama dengan postingnya.
		if !req.Document.IsZero() {
			accounts, err := ps.accountMap(ctx, req.TenantID, accountIDsOf(req.Lines))
			if err != nil {
				return err
			}
			_, amount := cashMovement(accounts, req.Lines)
			if err := ps.applyDocument(ctx, req.TenantID, posted, req.Document, amount, req.CreatedBy); err != nil {
				return err
			}
		}
		if err := ps.enforceDocument(ctx, req.TenantID, posted); err != nil {
			return err
		}
		out = posted
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// Reverse creates and immediately posts a reversing entry for an existing posted journal.
// The reversing entry has all debits and credits swapped.
// Returns ErrJournalNotPosted if the original is not posted.
// Returns ErrJournalAlreadyReversed if a reversing entry already exists.
// W-3.0 (B-5): pembuatan dan posting pembalik berjalan dalam SATU transaksi.
// Reverse adalah satu-satunya cara sah mengoreksi ledger (Invariant #5) —
// gagal di tengah dulu meninggalkan pembalik draft yang tidak bisa diposting
// ulang (HasReversingEntry sudah melihatnya) sekaligus tidak membalik apa pun.
func (s *PostingService) Reverse(ctx context.Context, tenantID, entryID uint64, reverseDate time.Time) (*JournalEntry, error) {
	var out *JournalEntry
	err := s.inTx(ctx, func(ps *PostingService) error {
		rev, err := ps.reverseWithin(ctx, tenantID, entryID, reverseDate)
		if err != nil {
			return err
		}
		out = rev
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// reverseWithin adalah badan Reverse tanpa pembungkus transaksi — dipanggil
// oleh Reverse dengan service yang sudah terikat transaksi.
func (s *PostingService) reverseWithin(ctx context.Context, tenantID, entryID uint64, reverseDate time.Time) (*JournalEntry, error) {
	// R-2 (W-8): pembalik adalah jurnal baru bertanggal reverseDate, jadi ia
	// tunduk pada penutupan periode persis seperti jurnal lain. Tanpa cek ini
	// Create menolak jurnal ke periode tertutup sementara Reverse menulisinya —
	// dan setiap laporan yang sudah diterbitkan atas periode itu berubah diam-diam
	// setelah ditandatangani. Koreksi atas periode tertutup tetap bisa dilakukan:
	// beri tanggal pembalik pada periode berjalan, bukan pada periode yang ditutup.
	if s.periods != nil {
		closed, err := s.periods.IsPeriodClosed(ctx, tenantID, reverseDate)
		if err != nil {
			return nil, fmt.Errorf("cek periode: %w", err)
		}
		if closed {
			return nil, ErrPeriodClosed
		}
	}

	original, err := s.journals.FindByID(ctx, tenantID, entryID)
	if err != nil {
		return nil, fmt.Errorf("find original journal: %w", err)
	}
	if !original.IsPosted() {
		return nil, ErrJournalNotPosted
	}
	has, err := s.journals.HasReversingEntry(ctx, tenantID, entryID)
	if err != nil {
		return nil, fmt.Errorf("check reversing entry: %w", err)
	}
	if has {
		return nil, ErrJournalAlreadyReversed
	}

	reversedLines := make([]JournalLine, len(original.Lines))
	for i, l := range original.Lines {
		reversedLines[i] = JournalLine{
			TenantID:    tenantID,
			AccountID:   l.AccountID,
			Debit:       l.Credit, // swap
			Credit:      l.Debit,  // swap
			ProjectID:   l.ProjectID,
			PhaseID:     l.PhaseID,
			UnitID:      l.UnitID,
			Description: l.Description,
		}
	}

	rev := &JournalEntry{
		TenantID:    tenantID,
		Date:        reverseDate,
		Description: "Pembalik: " + original.Description,
		Reference:   original.Reference,
		Source:      "reversal",
		IsReversing: true,
		ReversesID:  &entryID,
		Lines:       reversedLines,
	}
	if err := s.journals.Create(ctx, rev); err != nil {
		return nil, fmt.Errorf("create reversing entry: %w", err)
	}
	// Post immediately
	now := time.Now()
	if err := s.journals.MarkPosted(ctx, tenantID, rev.ID, now); err != nil {
		return nil, fmt.Errorf("post reversing entry: %w", err)
	}
	rev.PostedAt = &now

	// W-3.2 — pembalik jurnal KAS mendapat dokumen JR sendiri yang menunjuk
	// dokumen asli (D-W3-5). Otomatis, tanpa parameter: setiap pemanggil Reverse
	// yang membalik pergerakan kas membutuhkannya, dan satu pemanggil yang lupa
	// meminta sudah cukup untuk melubangi INV-DOC-1.
	if err := s.reversalDocument(ctx, tenantID, original, rev); err != nil {
		return nil, err
	}
	if err := s.enforceDocument(ctx, tenantID, rev); err != nil {
		return nil, err
	}
	return rev, nil
}

// reversalDocument menerbitkan dokumen JR bila jurnal yang dibalik menyentuh kas.
// Jurnal non-kas (akrual, reklasifikasi, tutup periode) tidak menghasilkan
// dokumen — di luar cakupan INV-DOC-1.
func (s *PostingService) reversalDocument(ctx context.Context, tenantID uint64, original, rev *JournalEntry) error {
	di := s.documents()
	if di == nil {
		return nil
	}
	ids := make([]uint64, 0, len(original.Lines))
	for _, l := range original.Lines {
		ids = append(ids, l.AccountID)
	}
	accounts, err := s.accountMap(ctx, tenantID, ids)
	if err != nil {
		return err
	}
	isCash, amount := cashMovementOfEntry(accounts, original.Lines)
	if !isCash {
		return nil
	}
	// Dokumen asli boleh belum ada (data pra-W-3 yang belum di-backfill):
	// pembaliknya tetap dapat dokumen sendiri, hanya tanpa penunjuk balik.
	origDocID, err := di.DocumentIDOf(ctx, tenantID, original.ID)
	if err != nil {
		return fmt.Errorf("baca dokumen jurnal asal %d: %w", original.ID, err)
	}
	doc, err := di.IssueReversal(ctx, tenantID, rev.ID, origDocID, time.Now(), amount, rev.CreatedBy)
	if err != nil {
		return fmt.Errorf("terbitkan dokumen pembalik jurnal %d: %w", original.ID, err)
	}
	rev.DocumentID = &doc.ID
	return nil
}

// ── Private helpers ───────────────────────────────────────────────────────────

// validateLines enforces:
//   - At least 2 lines
//   - Each line: exactly one of debit/credit is positive; no negatives
//   - Each amount is whole rupiah — no fractional sen (Invariant #2)
//   - Σdebit == Σcredit (Invariant #1)
func validateLines(lines []LineInput) error {
	if len(lines) < 2 {
		return ErrTooFewLines
	}
	totalDebit := domain.Zero
	totalCredit := domain.Zero
	for i, l := range lines {
		if l.Debit.IsNeg() || l.Credit.IsNeg() {
			return fmt.Errorf("%w: baris %d memiliki nilai negatif", ErrLineNegative, i)
		}
		if !isWholeRupiah(l.Debit) || !isWholeRupiah(l.Credit) {
			return fmt.Errorf("%w: baris %d (debit=%s kredit=%s)", ErrAmountNotWholeRupiah, i, l.Debit, l.Credit)
		}
		debitPos := l.Debit.GreaterThan(domain.Zero)
		creditPos := l.Credit.GreaterThan(domain.Zero)
		if debitPos == creditPos { // both zero OR both non-zero
			return fmt.Errorf("%w: baris %d (debit=%s credit=%s)", ErrLineInvalid, i, l.Debit, l.Credit)
		}
		totalDebit = totalDebit.Add(l.Debit)
		totalCredit = totalCredit.Add(l.Credit)
	}
	if !totalDebit.Equal(totalCredit) {
		return fmt.Errorf("%w: total debit=%s total kredit=%s", ErrJournalNotBalanced, totalDebit, totalCredit)
	}
	return nil
}

// isWholeRupiah delegates to domain.Money.IsWholeRupiah (Invariant #2).
func isWholeRupiah(m domain.Money) bool { return m.IsWholeRupiah() }

func (s *PostingService) verifyAccountsExist(ctx context.Context, tenantID uint64, lines []LineInput) error {
	if _, err := s.accounts.FindByIDs(ctx, tenantID, accountIDsOf(lines)); err != nil {
		return err
	}
	return nil
}

// accountIDsOf mengumpulkan id akun unik dari daftar baris, urutan dipertahankan.
func accountIDsOf(lines []LineInput) []uint64 {
	seen := make(map[uint64]bool, len(lines))
	ids := make([]uint64, 0, len(lines))
	for _, l := range lines {
		if !seen[l.AccountID] {
			ids = append(ids, l.AccountID)
			seen[l.AccountID] = true
		}
	}
	return ids
}

func inputLinesToModel(tenantID uint64, inputs []LineInput) []JournalLine {
	lines := make([]JournalLine, len(inputs))
	for i, in := range inputs {
		lines[i] = JournalLine{
			TenantID:    tenantID,
			AccountID:   in.AccountID,
			Debit:       in.Debit,
			Credit:      in.Credit,
			ProjectID:   in.ProjectID,
			PhaseID:     in.PhaseID,
			UnitID:      in.UnitID,
			Description: in.Description,
		}
	}
	return lines
}

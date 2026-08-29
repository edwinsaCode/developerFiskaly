package histfin

import (
	"context"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"

	"esaproperti/internal/domain"
	"esaproperti/internal/ledger"
)

// Service — business logic snapshot keuangan historis.
//
// Yang TIDAK ada di sini, dan itu disengaja: PostingService, JournalEntry, dan
// segala jalan menuju buku besar. Paket ini hanya menulis ke tiga tabelnya
// sendiri. Kalau suatu hari ada kebutuhan "posting snapshot", itu keputusan
// bisnis baru — bukan penambahan diam-diam di sini.
type Service struct {
	repo *Repository
	db   *gorm.DB
}

func NewService(db *gorm.DB) *Service {
	return &Service{repo: NewRepository(db), db: db}
}

// ── Payload ──────────────────────────────────────────────────────────────────

// Detail adalah snapshot lengkap: header, baris, dan totalnya.
type Detail struct {
	Snapshot
	Lines  []SnapshotLine `json:"lines"`
	Totals Totals         `json:"totals"`
	// ComputedAccountCode diberitahukan ke frontend supaya pemilih akun bisa
	// menyembunyikannya tanpa menghardcode kode akun di sisi browser.
	ComputedAccountCode string `json:"computed_account_code"`
}

// YearSummary adalah satu baris di daftar tahun.
type YearSummary struct {
	Snapshot
	Totals Totals `json:"totals"`
}

// ── Baca ─────────────────────────────────────────────────────────────────────

// ListYears mengembalikan seluruh tahun yang punya snapshot, terbaru dulu.
// Setiap tahun berdiri sendiri — tidak ada angka yang diturunkan antar tahun.
func (s *Service) ListYears(ctx context.Context, tenantID uint64) ([]YearSummary, error) {
	snaps, err := s.repo.ListSnapshots(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	ids := make([]uint64, 0, len(snaps))
	for _, sn := range snaps {
		ids = append(ids, sn.ID)
	}
	byID, err := s.repo.ListLinesForSnapshots(ctx, tenantID, ids)
	if err != nil {
		return nil, err
	}
	out := make([]YearSummary, 0, len(snaps))
	for _, sn := range snaps {
		out = append(out, YearSummary{Snapshot: sn, Totals: ComputeTotals(byID[sn.ID])})
	}
	return out, nil
}

// Get mengambil satu tahun beserta barisnya.
func (s *Service) Get(ctx context.Context, tenantID uint64, year int) (*Detail, error) {
	snap, err := s.repo.FindByYear(ctx, tenantID, year, false)
	if err != nil {
		return nil, err
	}
	return s.detail(ctx, tenantID, *snap)
}

// Neraca mengembalikan Neraca historis tahun tersebut.
func (s *Service) Neraca(ctx context.Context, tenantID uint64, year int) (*NeracaReport, error) {
	snap, lines, err := s.load(ctx, tenantID, year)
	if err != nil {
		return nil, err
	}
	rep := BuildNeraca(*snap, lines)
	return &rep, nil
}

// LabaRugi mengembalikan Laba Rugi historis tahun tersebut.
func (s *Service) LabaRugi(ctx context.Context, tenantID uint64, year int) (*PLReport, error) {
	snap, lines, err := s.load(ctx, tenantID, year)
	if err != nil {
		return nil, err
	}
	rep := BuildLabaRugi(*snap, lines)
	return &rep, nil
}

// Audits mengembalikan jejak perubahan satu tahun (terbaru dulu).
func (s *Service) Audits(ctx context.Context, tenantID uint64, year int) ([]Audit, error) {
	snap, err := s.repo.FindByYear(ctx, tenantID, year, false)
	if err != nil {
		return nil, err
	}
	return s.repo.ListAudits(ctx, tenantID, snap.ID)
}

// ── Tulis ────────────────────────────────────────────────────────────────────

// Create membuka tahun buku baru dalam status draft dan kosong.
func (s *Service) Create(ctx context.Context, tenantID uint64, year int, actor *uint64) (*Detail, error) {
	if err := s.guardYear(ctx, tenantID, year); err != nil {
		return nil, err
	}
	var detail *Detail
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		repo := s.repo.WithTx(tx)
		if _, err := repo.FindByYear(ctx, tenantID, year, true); err == nil {
			return ErrSnapshotExists
		} else if !errors.Is(err, ErrSnapshotNotFound) {
			return err
		}
		snap := Snapshot{
			TenantID:   tenantID,
			FiscalYear: year,
			Status:     StatusDraft,
			CreatedBy:  actor,
			UpdatedBy:  actor,
		}
		if err := repo.CreateSnapshot(ctx, &snap); err != nil {
			return err
		}
		if err := repo.AppendAudit(ctx, &Audit{
			TenantID: tenantID, SnapshotID: snap.ID, Event: EventCreated,
			Revision: snap.Revision, ActorID: actor,
			TotalAssets: domain.Zero, TotalLiabEquity: domain.Zero, NetIncome: domain.Zero,
		}); err != nil {
			return err
		}
		d, err := s.withRepo(repo).detail(ctx, tenantID, snap)
		if err != nil {
			return err
		}
		detail = d
		return nil
	})
	if err != nil {
		return nil, err
	}
	return detail, nil
}

// Save mengganti SELURUH isi snapshot dengan baris yang dikirim (replace-all).
//
// Baris bernilai nol dibuang: laporan yang menampilkan puluhan akun Rp 0 tidak
// menolong siapa pun, dan menghapus baris memang caranya mengosongkan akun.
func (s *Service) Save(ctx context.Context, tenantID uint64, year int, req SaveRequest) (*Detail, error) {
	var detail *Detail
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		repo := s.repo.WithTx(tx)
		snap, err := repo.FindByYear(ctx, tenantID, year, true)
		if err != nil {
			return err
		}
		if !snap.Editable() {
			return ErrSnapshotFinal
		}
		// Tahun bisa saja mulai dipakai sejak snapshot dibuat.
		if has, herr := repo.HasPostedJournals(ctx, tenantID, year); herr != nil {
			return herr
		} else if has {
			return ErrYearHasJournals
		}

		lines, err := s.buildLines(ctx, repo, tenantID, snap.ID, req.Lines)
		if err != nil {
			return err
		}
		if err := repo.DeleteLines(ctx, tenantID, snap.ID); err != nil {
			return err
		}
		if err := repo.InsertLines(ctx, lines); err != nil {
			return err
		}

		snap.Notes = strings.TrimSpace(req.Notes)
		snap.UpdatedBy = req.Actor
		if err := repo.SaveSnapshot(ctx, snap); err != nil {
			return err
		}

		t := ComputeTotals(lines)
		if err := repo.AppendAudit(ctx, auditOf(tenantID, *snap, EventSaved, "", t, req.Actor)); err != nil {
			return err
		}
		d, err := s.withRepo(repo).detailWithLines(*snap, lines)
		detail = d
		return err
	})
	if err != nil {
		return nil, err
	}
	return detail, nil
}

// Finalize mengunci angka satu tahun.
//
// Syaratnya keras dengan sengaja: snapshot yang tidak seimbang bukan laporan
// keuangan, dan yang dikunci harus angka yang bisa dipertanggungjawabkan.
func (s *Service) Finalize(ctx context.Context, tenantID uint64, year int, actor *uint64) (*Detail, error) {
	var detail *Detail
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		repo := s.repo.WithTx(tx)
		snap, err := repo.FindByYear(ctx, tenantID, year, true)
		if err != nil {
			return err
		}
		if !snap.Editable() {
			return ErrSnapshotFinal
		}
		lines, err := repo.ListLines(ctx, tenantID, snap.ID)
		if err != nil {
			return err
		}
		if len(lines) == 0 {
			return ErrSnapshotEmpty
		}
		t := ComputeTotals(lines)
		if !t.IsBalanced {
			return ErrNotBalanced
		}
		now := time.Now()
		snap.Status = StatusFinal
		snap.Revision++
		snap.FinalizedAt = &now
		snap.FinalizedBy = actor
		snap.UpdatedBy = actor
		if err := repo.SaveSnapshot(ctx, snap); err != nil {
			return err
		}
		if err := repo.AppendAudit(ctx, auditOf(tenantID, *snap, EventFinalized, "", t, actor)); err != nil {
			return err
		}
		d, err := s.withRepo(repo).detailWithLines(*snap, lines)
		detail = d
		return err
	})
	if err != nil {
		return nil, err
	}
	return detail, nil
}

// Reopen membuka kembali tahun yang sudah final. Wajib beralasan — inilah satu-
// satunya jejak mengapa angka yang pernah disahkan berubah.
func (s *Service) Reopen(ctx context.Context, tenantID uint64, year int, reason string, actor *uint64) (*Detail, error) {
	reason = strings.TrimSpace(reason)
	if len(reason) < 10 {
		return nil, ErrReasonRequired
	}
	var detail *Detail
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		repo := s.repo.WithTx(tx)
		snap, err := repo.FindByYear(ctx, tenantID, year, true)
		if err != nil {
			return err
		}
		if snap.Status != StatusFinal {
			return ErrSnapshotNotFinal
		}
		lines, err := repo.ListLines(ctx, tenantID, snap.ID)
		if err != nil {
			return err
		}
		snap.Status = StatusDraft
		snap.UpdatedBy = actor
		if err := repo.SaveSnapshot(ctx, snap); err != nil {
			return err
		}
		if err := repo.AppendAudit(ctx, auditOf(tenantID, *snap, EventReopened, reason, ComputeTotals(lines), actor)); err != nil {
			return err
		}
		d, err := s.withRepo(repo).detailWithLines(*snap, lines)
		detail = d
		return err
	})
	if err != nil {
		return nil, err
	}
	return detail, nil
}

// Delete menghapus snapshot yang belum pernah difinalkan — sekadar membatalkan
// tahun yang salah dibuka. Snapshot yang pernah final tidak bisa dihapus:
// histori yang pernah disahkan tetap tinggal, seperti jurnal.
func (s *Service) Delete(ctx context.Context, tenantID uint64, year int) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		repo := s.repo.WithTx(tx)
		snap, err := repo.FindByYear(ctx, tenantID, year, true)
		if err != nil {
			return err
		}
		if snap.Status != StatusDraft || snap.Revision > 0 {
			return ErrSnapshotFinal
		}
		return repo.DeleteSnapshot(ctx, tenantID, snap.ID)
	})
}

// ── Internal ─────────────────────────────────────────────────────────────────

// guardYear memutuskan apakah satu tahun boleh punya snapshot manual.
//
// Aturannya data-driven, bukan konfigurasi: tahun yang buku besarnya sudah hidup
// dilaporkan DARI buku besar. Efek sampingnya menyenangkan — begitu operasional
// berjalan, tahun berjalan otomatis tertutup untuk snapshot, sehingga W-6 tidak
// perlu menjawab pertanyaan perlakuan tahun berjalan yang memang belum
// diputuskan (OPEN BUSINESS DECISION EDGE-6).
func (s *Service) guardYear(ctx context.Context, tenantID uint64, year int) error {
	if year < 1980 || year > 2100 {
		return ErrYearInvalid
	}
	if year > time.Now().Year() {
		return ErrYearFuture
	}
	has, err := s.repo.HasPostedJournals(ctx, tenantID, year)
	if err != nil {
		return err
	}
	if has {
		return ErrYearHasJournals
	}
	return nil
}

// buildLines mengubah input admin menjadi baris siap simpan: akun di-resolve
// dari COA, jenis laporan diturunkan dari tipe akun, nilai nol dibuang.
func (s *Service) buildLines(ctx context.Context, repo *Repository, tenantID, snapshotID uint64, in []LineInput) ([]SnapshotLine, error) {
	seen := map[uint64]bool{}
	out := make([]SnapshotLine, 0, len(in))
	for _, li := range in {
		if li.AccountID == 0 {
			return nil, ErrAccountNotFound
		}
		if seen[li.AccountID] {
			return nil, ErrDuplicateAccount
		}
		seen[li.AccountID] = true
		if li.Amount.IsZero() {
			continue
		}
		acc, err := repo.FindAccount(ctx, tenantID, li.AccountID)
		if err != nil {
			return nil, err
		}
		if acc.Code == ledger.AccountIncomeSummaryCode {
			return nil, ErrComputedAccount
		}
		st := StatementFor(acc.Type)
		if st == "" {
			return nil, ErrAccountTypeUnkn
		}
		out = append(out, SnapshotLine{
			TenantID:    tenantID,
			SnapshotID:  snapshotID,
			Statement:   st,
			AccountID:   acc.ID,
			AccountCode: acc.Code,
			AccountName: acc.Name,
			AccountType: string(acc.Type),
			Amount:      li.Amount,
			SortOrder:   li.SortOrder,
		})
	}
	return out, nil
}

func (s *Service) load(ctx context.Context, tenantID uint64, year int) (*Snapshot, []SnapshotLine, error) {
	snap, err := s.repo.FindByYear(ctx, tenantID, year, false)
	if err != nil {
		return nil, nil, err
	}
	lines, err := s.repo.ListLines(ctx, tenantID, snap.ID)
	if err != nil {
		return nil, nil, err
	}
	return snap, lines, nil
}

func (s *Service) detail(ctx context.Context, tenantID uint64, snap Snapshot) (*Detail, error) {
	lines, err := s.repo.ListLines(ctx, tenantID, snap.ID)
	if err != nil {
		return nil, err
	}
	return s.detailWithLines(snap, lines)
}

func (s *Service) detailWithLines(snap Snapshot, lines []SnapshotLine) (*Detail, error) {
	return &Detail{
		Snapshot:            snap,
		Lines:               lines,
		Totals:              ComputeTotals(lines),
		ComputedAccountCode: ledger.AccountIncomeSummaryCode,
	}, nil
}

// withRepo mengembalikan salinan service yang membaca lewat repo transaksional,
// supaya payload yang dikembalikan operasi tulis konsisten dengan tulisannya.
func (s *Service) withRepo(repo *Repository) *Service {
	return &Service{repo: repo, db: repo.DB()}
}

func auditOf(tenantID uint64, snap Snapshot, ev AuditEvent, reason string, t Totals, actor *uint64) *Audit {
	return &Audit{
		TenantID:        tenantID,
		SnapshotID:      snap.ID,
		Event:           ev,
		Revision:        snap.Revision,
		Reason:          reason,
		TotalAssets:     t.TotalAset,
		TotalLiabEquity: t.TotalKewajibanEkuitas,
		NetIncome:       t.NetIncome,
		LineCount:       t.LineCount,
		ActorID:         actor,
	}
}

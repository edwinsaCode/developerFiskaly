package cost

import (
	"context"
	"fmt"
	"time"

	"gorm.io/gorm"

	"esaproperti/internal/domain"
	"esaproperti/internal/ledger"
)

// ── GORMRepository implements CostStore, AccountByCodeFinder, CostQueryer ─────

type GORMRepository struct {
	db *gorm.DB
}

func NewGORMRepository(db *gorm.DB) *GORMRepository {
	return &GORMRepository{db: db}
}

// ResolveAccount looks up an account's database ID and display name by its COA code.
// Used by both CreateCostEntry (needs ID) and PreviewCostEntry (needs ID + name),
// so both paths hit the same query — single source of truth for account resolution.
func (r *GORMRepository) ResolveAccount(ctx context.Context, tenantID uint64, code string) (id uint64, name string, err error) {
	var acc ledger.Account
	err = r.db.WithContext(ctx).
		Select("id", "name").
		Where("tenant_id = ? AND code = ?", tenantID, code).
		First(&acc).Error
	if err != nil {
		return 0, "", fmt.Errorf("%w: %q", ErrAccountNotFound, code)
	}
	return acc.ID, acc.Name, nil
}

// ValidatePaymentAccountCode membuktikan sebuah kode akun sah sebagai sumber
// pembayaran (W-10 T-5). Pembuktiannya DIPINJAM dari ledger
// (ledger.ValidatePaymentAccount) — aturan "apa itu akun kas/bank" hanya boleh
// hidup di satu tempat, dan tempat itu adalah pemilik chart of accounts.
func (r *GORMRepository) ValidatePaymentAccountCode(ctx context.Context, tenantID uint64, code string) error {
	var acc ledger.Account
	err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND code = ?", tenantID, code).
		First(&acc).Error
	if err != nil {
		return fmt.Errorf("%w: %q", ErrAccountNotFound, code)
	}
	return ledger.ValidatePaymentAccount(&acc)
}

// ResolveUnitProject mengembalikan proyek pemilik sebuah unit.
// Query tabel langsung (bukan import package project) supaya arah
// ketergantungan cost → project tidak bertambah hanya demi satu kolom.
func (r *GORMRepository) ResolveUnitProject(ctx context.Context, tenantID, unitID uint64) (uint64, error) {
	var row struct {
		ProjectID uint64 `gorm:"column:project_id"`
	}
	err := r.db.WithContext(ctx).
		Table("units").Select("project_id").
		Where("tenant_id = ? AND id = ?", tenantID, unitID).
		Take(&row).Error
	if err != nil {
		return 0, fmt.Errorf("unit %d tidak ditemukan: %w", unitID, err)
	}
	return row.ProjectID, nil
}

// ── CostStore implementation ──────────────────────────────────────────────────

func (r *GORMRepository) CreateCostEntry(ctx context.Context, e *CostEntry) error {
	if err := r.db.WithContext(ctx).Create(e).Error; err != nil {
		return fmt.Errorf("CreateCostEntry: %w", err)
	}
	return nil
}

func (r *GORMRepository) FindCostEntryByID(ctx context.Context, tenantID, id uint64) (*CostEntry, error) {
	var e CostEntry
	err := r.db.WithContext(ctx).
		First(&e, "id = ? AND tenant_id = ?", id, tenantID).Error
	if err != nil {
		return nil, ErrCostEntryNotFound
	}
	return &e, nil
}

func (r *GORMRepository) ListCostEntriesByProject(ctx context.Context, tenantID, projectID uint64) ([]*CostEntry, error) {
	var es []*CostEntry
	err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND project_id = ?", tenantID, projectID).
		Order("date ASC, id ASC").
		Find(&es).Error
	if err != nil {
		return nil, fmt.Errorf("ListCostEntriesByProject: %w", err)
	}
	return es, nil
}

// TenantName dan ProjectName memberi label header cetak (Riwayat Biaya /
// Bukti Kas Keluar) — pola sama dengan internal/reporting/repository.go,
// duplikat karena domain package tidak saling impor.
func (r *GORMRepository) TenantName(ctx context.Context, tenantID uint64) (string, error) {
	var name string
	err := r.db.WithContext(ctx).Raw(`SELECT name FROM tenants WHERE id = ?`, tenantID).Scan(&name).Error
	if err != nil {
		return "", fmt.Errorf("cost: TenantName: %w", err)
	}
	return name, nil
}

func (r *GORMRepository) ProjectName(ctx context.Context, tenantID, projectID uint64) (string, error) {
	var name string
	err := r.db.WithContext(ctx).Raw(`SELECT name FROM projects WHERE id = ? AND tenant_id = ?`, projectID, tenantID).Scan(&name).Error
	if err != nil {
		return "", fmt.Errorf("cost: ProjectName: %w", err)
	}
	return name, nil
}

func (r *GORMRepository) ListCostEntriesByUnit(ctx context.Context, tenantID, unitID uint64) ([]*CostEntry, error) {
	var es []*CostEntry
	err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND unit_id = ?", tenantID, unitID).
		Order("date ASC, id ASC").
		Find(&es).Error
	if err != nil {
		return nil, fmt.Errorf("ListCostEntriesByUnit: %w", err)
	}
	return es, nil
}

// ── CostQueryer implementation ─────────────────────────────────────────────────
//
// All queries aggregate from posted journal_lines, NOT from cost_entries.amount.
// This enforces the "sumber kebenaran = JURNAL" rule.
//
// S5: agregasi via pembaca KANONIK ledger.ActualCostByCode — netting tunggal
// (debit non-reversal − kredit reversal); kode akun dari AccountRoleRegistry.

// inventoryCodes = Persediaan sub-accounts (Dr side of Event 1). SATU definisi
// via AccountRoleRegistry (S1) — dulu di-hardcode kembar di cost & allocation.
var inventoryCodes = ledger.RoleCodeList(ledger.RoleInventory)

func (r *GORMRepository) accumulatedQuery(ctx context.Context, tenantID, projectID uint64, unitFilter string, unitID *uint64) (domain.UnitCostBreakdown, error) {
	sc := ledger.ActualCostScope{ProjectID: projectID}
	switch unitFilter {
	case "unit":
		sc.UnitID = unitID
	case "project-wide":
		sc.ProjectWideOnly = true
		// "all" = no additional filter
	}
	byCode, err := ledger.NewQueryService(r.db).ActualCostByCode(ctx, tenantID, inventoryCodes, sc)
	if err != nil {
		return domain.UnitCostBreakdown{}, fmt.Errorf("accumulated cost query: %w", err)
	}
	return breakdownFromCodes(byCode), nil
}

func (r *GORMRepository) AccumulatedByProject(ctx context.Context, tenantID, projectID uint64) (domain.UnitCostBreakdown, error) {
	return r.accumulatedQuery(ctx, tenantID, projectID, "all", nil)
}

func (r *GORMRepository) AccumulatedByUnit(ctx context.Context, tenantID, projectID, unitID uint64) (domain.UnitCostBreakdown, error) {
	return r.accumulatedQuery(ctx, tenantID, projectID, "unit", &unitID)
}

func (r *GORMRepository) AccumulatedProjectWide(ctx context.Context, tenantID, projectID uint64) (domain.UnitCostBreakdown, error) {
	return r.accumulatedQuery(ctx, tenantID, projectID, "project-wide", nil)
}

// breakdownFromCodes memetakan hasil pembaca kanonik (kode → total) ke
// UnitCostBreakdown lewat taxonomy domain (bukan hardcode kode di sini).
// AllCostCategories hanya land|hard (RULE KLIEN FREEZE 2026-09-04) — b.Soft
// tidak pernah diisi dari sini (field itu legacy-only untuk baca data historis).
func breakdownFromCodes(byCode map[string]domain.Money) domain.UnitCostBreakdown {
	var b domain.UnitCostBreakdown
	for _, c := range domain.AllCostCategories {
		m, ok := byCode[c.InventoryAccountCode()]
		if !ok {
			continue
		}
		switch c {
		case domain.CostCategoryLand:
			b.Land = m
		case domain.CostCategoryHard:
			b.Hard = m
		}
	}
	return b
}

// ── GORMTxRunner (W-3.0 B-3) ──────────────────────────────────────────────────

// GORMTxRunner menjalankan operasi cost dalam satu transaksi database.
// Kolaborator dibangun ulang di atas transaksi mengikuti idiom yang sudah
// dipakai sale/charge/commission.
type GORMTxRunner struct {
	db      *gorm.DB
	posting *ledger.PostingService
}

func NewGORMTxRunner(db *gorm.DB, posting *ledger.PostingService) *GORMTxRunner {
	return &GORMTxRunner{db: db, posting: posting}
}

func (t *GORMTxRunner) InTx(ctx context.Context, fn func(JournalWriter, CostStore) error) error {
	return t.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		txLedgerRepo := ledger.NewGORMRepository(tx)
		txPosting := ledger.NewPostingService(txLedgerRepo, txLedgerRepo).WithPeriodChecker(txLedgerRepo)
		return fn(NewLedgerJournalAdapter(txPosting), NewGORMRepository(tx))
	})
}

// ── LedgerJournalAdapter wraps *ledger.PostingService as cost.JournalWriter ───

type LedgerJournalAdapter struct {
	svc *ledger.PostingService
}

func NewLedgerJournalAdapter(svc *ledger.PostingService) *LedgerJournalAdapter {
	return &LedgerJournalAdapter{svc: svc}
}

func (a *LedgerJournalAdapter) CreateJournal(ctx context.Context, tenantID uint64, date time.Time, description string, lines []JournalLineInput) (uint64, error) {
	llines := make([]ledger.LineInput, len(lines))
	for i, l := range lines {
		llines[i] = ledger.LineInput{
			AccountID:   l.AccountID,
			Debit:       l.Debit,
			Credit:      l.Credit,
			ProjectID:   l.ProjectID,
			PhaseID:     l.PhaseID,
			UnitID:      l.UnitID,
			Description: l.Description,
		}
	}
	entry, err := a.svc.Create(ctx, ledger.CreateJournalRequest{
		TenantID:    tenantID,
		Date:        date,
		Description: description,
		Lines:       llines,
	})
	if err != nil {
		return 0, err
	}
	return entry.ID, nil
}

// PostJournal memposting jurnal biaya. Untuk pembayaran kas (O-1) dokumen BKK
// terbit di transaksi yang sama lewat PostDraft — bukan dua langkah terpisah.
func (a *LedgerJournalAdapter) PostJournal(ctx context.Context, tenantID, journalID uint64, cashOut bool) error {
	if !cashOut {
		_, err := a.svc.Post(ctx, tenantID, journalID)
		return err
	}
	_, err := a.svc.PostDraft(ctx, tenantID, journalID, ledger.DocumentSpec{TypeCode: ledger.DocCashOut})
	return err
}

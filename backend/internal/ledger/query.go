package ledger

import (
	"context"
	"fmt"
	"sort"
	"time"

	"gorm.io/gorm"

	"esaproperti/internal/domain"
)

// ── Trial Balance ─────────────────────────────────────────────────────────────

// TrialBalanceRow is one account's totals in the trial balance.
type TrialBalanceRow struct {
	AccountID   uint64             `json:"account_id"`
	AccountCode string             `json:"account_code"`
	AccountName string             `json:"account_name"`
	AccountType domain.AccountType `json:"account_type"`
	TotalDebit  domain.Money       `json:"total_debit"`
	TotalCredit domain.Money       `json:"total_credit"`
	// Balance = net balance in the normal direction for the account type.
	// Asset/Expense: Debit - Credit. Liability/Equity/Revenue: Credit - Debit.
	Balance domain.Money `json:"balance"`
}

// TrialBalance is the aggregate of all posted journal lines.
// Invariant: TotalDebit == TotalCredit (guaranteed by PostingService).
type TrialBalance struct {
	AsOf        time.Time         `json:"as_of"`
	Rows        []TrialBalanceRow `json:"rows"`
	TotalDebit  domain.Money      `json:"total_debit"`
	TotalCredit domain.Money      `json:"total_credit"`
}

// ComputeTrialBalance builds a trial balance from a slice of posted journal lines
// and an account index. This is a pure function — no DB access, easily testable.
//
// Invariant proof: because every posted journal satisfies Σdebit==Σcredit,
// the sum across all journals also satisfies this:
//
//	∀ j : Σ j.lines.debit == Σ j.lines.credit
//	⟹ Σ all_lines.debit == Σ all_lines.credit
func ComputeTrialBalance(lines []JournalLine, accounts map[uint64]*Account, asOf time.Time) TrialBalance {
	type balance struct{ debit, credit domain.Money }
	acc := make(map[uint64]*balance)

	for _, l := range lines {
		b, ok := acc[l.AccountID]
		if !ok {
			b = &balance{}
			acc[l.AccountID] = b
		}
		b.debit = b.debit.Add(l.Debit)
		b.credit = b.credit.Add(l.Credit)
	}

	// Sort by account code for deterministic output.
	ids := make([]uint64, 0, len(acc))
	for id := range acc {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool {
		ai, aj := accounts[ids[i]], accounts[ids[j]]
		if ai == nil || aj == nil {
			return ids[i] < ids[j]
		}
		return ai.Code < aj.Code
	})

	rows := make([]TrialBalanceRow, 0, len(ids))
	totalDebit := domain.Zero
	totalCredit := domain.Zero

	for _, id := range ids {
		b := acc[id]
		a := accounts[id]
		if a == nil {
			continue
		}
		var net domain.Money
		switch domain.NormalBalanceFor(a.Type) {
		case domain.NormalBalanceDebit:
			net = b.debit.Sub(b.credit)
		default:
			net = b.credit.Sub(b.debit)
		}
		rows = append(rows, TrialBalanceRow{
			AccountID:   id,
			AccountCode: a.Code,
			AccountName: a.Name,
			AccountType: a.Type,
			TotalDebit:  b.debit,
			TotalCredit: b.credit,
			Balance:     net,
		})
		totalDebit = totalDebit.Add(b.debit)
		totalCredit = totalCredit.Add(b.credit)
	}

	return TrialBalance{AsOf: asOf, Rows: rows, TotalDebit: totalDebit, TotalCredit: totalCredit}
}

// ── Journal Summary (for list view with aggregate totals) ─────────────────────

// JournalSummary is a journal header augmented with aggregate line totals.
// Returned by ListJournalSummaries — avoids N+1 queries for list views.
type JournalSummary struct {
	ID          uint64       `gorm:"column:id"           json:"id"`
	Date        time.Time    `gorm:"column:date"         json:"date"`
	Description string       `gorm:"column:description"  json:"description"`
	Reference   string       `gorm:"column:reference"    json:"reference,omitempty"`
	PostedAt    *time.Time   `gorm:"column:posted_at"    json:"posted_at,omitempty"`
	IsReversing bool         `gorm:"column:is_reversing" json:"is_reversing"`
	ReversesID  *uint64      `gorm:"column:reverses_id"  json:"reverses_id,omitempty"`
	Source      string       `gorm:"column:source"       json:"source"`
	CreatedBy   *uint64      `gorm:"column:created_by"   json:"created_by,omitempty"`
	CreatedAt   time.Time    `gorm:"column:created_at"   json:"created_at"`
	UpdatedAt   time.Time    `gorm:"column:updated_at"   json:"updated_at"`
	TotalDebit  domain.Money `gorm:"column:total_debit"  json:"total_debit"`
	TotalCredit domain.Money `gorm:"column:total_credit" json:"total_credit"`
	// W-3.6: nomor bukti yang membuktikan jurnal ini. Kosong = jurnal non-kas
	// (atau saldo awal, satu-satunya pergerakan kas yang dikecualikan
	// INV-DOC-1). Dibaca lewat LEFT JOIN, bukan fetch per baris.
	DocumentNumber string `gorm:"column:document_number" json:"document_number,omitempty"`
}

// JournalFilter scopes a journal list query.
type JournalFilter struct {
	DateFrom *time.Time
	DateTo   *time.Time
	Source   string // "manual", "system", "reversal", or "" for all
}

// ── Ledger Filter & Entry ─────────────────────────────────────────────────────

// LedgerFilter scopes a buku besar (general ledger) query.
type LedgerFilter struct {
	AccountID *uint64
	ProjectID *uint64
	PhaseID   *uint64
	UnitID    *uint64
	DateFrom  *time.Time
	DateTo    *time.Time
}

// LedgerEntry is one line in the buku besar (general ledger), with a running balance.
type LedgerEntry struct {
	EntryID     uint64       `json:"entry_id"`
	Date        time.Time    `json:"date"`
	Reference   string       `json:"reference,omitempty"`
	Description string       `json:"description"`
	Debit       domain.Money `json:"debit"`
	Credit      domain.Money `json:"credit"`
	Balance     domain.Money `json:"balance"`
}

// ComputeGeneralLedger builds a sorted general ledger (buku besar) from lines,
// applying an optional filter. It computes a running balance based on the account's
// normal balance direction.
func ComputeGeneralLedger(lines []JournalLine, account *Account, filter LedgerFilter, entries []JournalEntry) []LedgerEntry {
	// Build a lookup from journal_entry_id → entry (for date, description, reference).
	entryByID := make(map[uint64]*JournalEntry, len(entries))
	for i := range entries {
		entryByID[entries[i].ID] = &entries[i]
	}

	// Filter and sort lines.
	type withDate struct {
		line  JournalLine
		entry *JournalEntry
	}
	var filtered []withDate
	for _, l := range lines {
		e, ok := entryByID[l.JournalEntryID]
		if !ok {
			continue
		}
		if filter.AccountID != nil && l.AccountID != *filter.AccountID {
			continue
		}
		if filter.ProjectID != nil && (l.ProjectID == nil || *l.ProjectID != *filter.ProjectID) {
			continue
		}
		if filter.PhaseID != nil && (l.PhaseID == nil || *l.PhaseID != *filter.PhaseID) {
			continue
		}
		if filter.UnitID != nil && (l.UnitID == nil || *l.UnitID != *filter.UnitID) {
			continue
		}
		if filter.DateFrom != nil && e.Date.Before(*filter.DateFrom) {
			continue
		}
		if filter.DateTo != nil && e.Date.After(*filter.DateTo) {
			continue
		}
		filtered = append(filtered, withDate{line: l, entry: e})
	}
	sort.Slice(filtered, func(i, j int) bool {
		di, dj := filtered[i].entry.Date, filtered[j].entry.Date
		if di.Equal(dj) {
			return filtered[i].line.ID < filtered[j].line.ID
		}
		return di.Before(dj)
	})

	normalDebit := account == nil || domain.NormalBalanceFor(account.Type) == domain.NormalBalanceDebit

	result := make([]LedgerEntry, 0, len(filtered))
	running := domain.Zero
	for _, wd := range filtered {
		l, e := wd.line, wd.entry
		if normalDebit {
			running = running.Add(l.Debit).Sub(l.Credit)
		} else {
			running = running.Add(l.Credit).Sub(l.Debit)
		}
		result = append(result, LedgerEntry{
			EntryID:     l.JournalEntryID,
			Date:        e.Date,
			Reference:   e.Reference,
			Description: e.Description,
			Debit:       l.Debit,
			Credit:      l.Credit,
			Balance:     running,
		})
	}
	return result
}

// ── QueryService (GORM-backed) ────────────────────────────────────────────────

// QueryService provides read-only queries backed by GORM.
type QueryService struct {
	db *gorm.DB
}

// NewQueryService constructs a QueryService.
func NewQueryService(db *gorm.DB) *QueryService {
	return &QueryService{db: db}
}

// ListAccounts returns all accounts for a tenant, sorted by code.
func (q *QueryService) ListAccounts(ctx context.Context, tenantID uint64) ([]*Account, error) {
	var accounts []*Account
	err := q.db.WithContext(ctx).
		Where("tenant_id = ?", tenantID).
		Order("code").
		Find(&accounts).Error
	return accounts, err
}

// ListCashBankAccounts returns active asset accounts of category cash/bank for
// a tenant — the COA-driven source for payment account selection.
func (q *QueryService) ListCashBankAccounts(ctx context.Context, tenantID uint64) ([]*Account, error) {
	var accounts []*Account
	err := q.db.WithContext(ctx).
		Where("tenant_id = ? AND type = ? AND is_active = ? AND category IN ?",
			tenantID, domain.AccountAsset, true,
			[]string{string(CategoryCash), string(CategoryBank)}).
		Order("category, code").
		Find(&accounts).Error
	return accounts, err
}

// GetAccount returns a single account by ID.
func (q *QueryService) GetAccount(ctx context.Context, tenantID, id uint64) (*Account, error) {
	var a Account
	err := q.db.WithContext(ctx).
		First(&a, "id = ? AND tenant_id = ?", id, tenantID).Error
	return &a, err
}

// ListJournals returns journal entries for a tenant (most recent first), without preloading lines.
func (q *QueryService) ListJournals(ctx context.Context, tenantID uint64) ([]*JournalEntry, error) {
	var entries []*JournalEntry
	err := q.db.WithContext(ctx).
		Where("tenant_id = ?", tenantID).
		Order("date DESC, id DESC").
		Find(&entries).Error
	return entries, err
}

// ListJournalSummaries returns journal headers with aggregate totals in a single query.
// Used for the journal list view to avoid N+1 loading of lines.
func (q *QueryService) ListJournalSummaries(ctx context.Context, tenantID uint64, filter JournalFilter) ([]*JournalSummary, error) {
	db := q.db.WithContext(ctx).
		Table("journal_entries").
		Select(`journal_entries.id, journal_entries.date, journal_entries.description,
			journal_entries.reference, journal_entries.posted_at, journal_entries.is_reversing,
			journal_entries.reverses_id, journal_entries.source, journal_entries.created_by,
			journal_entries.created_at, journal_entries.updated_at,
			COALESCE(SUM(journal_lines.debit), '0') AS total_debit,
			COALESCE(SUM(journal_lines.credit), '0') AS total_credit,
			COALESCE(documents.number, '') AS document_number`).
		Joins("LEFT JOIN journal_lines ON journal_lines.journal_entry_id = journal_entries.id").
		// documents ditaut lewat logical FK (ledger tidak import package
		// document) — join mentah, sama seperti kolom document_id sendiri.
		Joins("LEFT JOIN documents ON documents.id = journal_entries.document_id AND documents.tenant_id = journal_entries.tenant_id").
		Where("journal_entries.tenant_id = ?", tenantID).
		Group("journal_entries.id").
		Order("journal_entries.date DESC, journal_entries.id DESC")

	if filter.DateFrom != nil {
		db = db.Where("journal_entries.date >= ?", *filter.DateFrom)
	}
	if filter.DateTo != nil {
		db = db.Where("journal_entries.date <= ?", *filter.DateTo)
	}
	if filter.Source != "" {
		db = db.Where("journal_entries.source = ?", filter.Source)
	}

	var summaries []*JournalSummary
	return summaries, db.Scan(&summaries).Error
}

// GetJournal returns a single journal entry with its lines and account info preloaded.
func (q *QueryService) GetJournal(ctx context.Context, tenantID, id uint64) (*JournalEntry, error) {
	var e JournalEntry
	err := q.db.WithContext(ctx).
		Preload("Lines").
		Preload("Lines.Account").
		First(&e, "id = ? AND tenant_id = ?", id, tenantID).Error
	return &e, err
}

// JournalDocument adalah identitas bukti yang membuktikan sebuah jurnal.
type JournalDocument struct {
	Number   string `gorm:"column:number"`
	TypeCode string `gorm:"column:document_type_code"`
	TypeName string `gorm:"column:type_name"`
	IssuedAt string `gorm:"column:issued_at"`
}

// DocumentOf membaca bukti sebuah jurnal (W-3.6). nil = jurnal tidak menunjuk
// dokumen mana pun — non-kas, saldo awal, atau draft yang belum diposting.
//
// Raw query, bukan relasi GORM: `document_id` adalah logical FK karena `ledger`
// tidak boleh import package `document` (arah ketergantungan satu arah).
func (q *QueryService) DocumentOf(ctx context.Context, tenantID, journalID uint64) (*JournalDocument, error) {
	var out []JournalDocument
	err := q.db.WithContext(ctx).Raw(`
		SELECT d.number, d.document_type_code,
		       COALESCE(t.name, d.document_type_code) AS type_name,
		       DATE_FORMAT(d.issued_at, '%Y-%m-%d') AS issued_at
		  FROM journal_entries j
		  JOIN documents d ON d.id = j.document_id AND d.tenant_id = j.tenant_id
		  LEFT JOIN document_types t ON t.tenant_id = d.tenant_id AND t.code = d.document_type_code
		 WHERE j.tenant_id = ? AND j.id = ?`, tenantID, journalID).Scan(&out).Error
	if err != nil {
		return nil, fmt.Errorf("baca dokumen jurnal %d: %w", journalID, err)
	}
	if len(out) == 0 {
		return nil, nil
	}
	return &out[0], nil
}

// TrialBalance computes the trial balance as of a given date for a tenant.
func (q *QueryService) TrialBalance(ctx context.Context, tenantID uint64, asOf time.Time) (*TrialBalance, error) {
	lines, accMap, err := q.postedBalanceData(ctx, tenantID, &asOf)
	if err != nil {
		return nil, err
	}
	tb := ComputeTrialBalance(lines, accMap, asOf)
	return &tb, nil
}

// postedBalanceData memuat posted journal lines + indeks akun untuk perhitungan
// saldo. asOf nil = SEMUA posted (tanpa batas tanggal); else date <= asOf.
// Dipakai bersama oleh TrialBalance dan LedgerBalanceService (satu jalur muat).
func (q *QueryService) postedBalanceData(ctx context.Context, tenantID uint64, asOf *time.Time) ([]JournalLine, map[uint64]*Account, error) {
	lineQ := q.db.WithContext(ctx).
		Joins("JOIN journal_entries ON journal_entries.id = journal_lines.journal_entry_id").
		Where("journal_lines.tenant_id = ? AND journal_entries.posted_at IS NOT NULL", tenantID)
	if asOf != nil {
		lineQ = lineQ.Where("journal_entries.date <= ?", *asOf)
	}
	var lines []JournalLine
	if err := lineQ.Find(&lines).Error; err != nil {
		return nil, nil, err
	}
	var accs []*Account
	if err := q.db.WithContext(ctx).Where("tenant_id = ?", tenantID).Find(&accs).Error; err != nil {
		return nil, nil, err
	}
	accMap := make(map[uint64]*Account, len(accs))
	for _, a := range accs {
		accMap[a.ID] = a
	}
	return lines, accMap, nil
}

// GeneralLedger returns the buku besar for a given filter.
func (q *QueryService) GeneralLedger(ctx context.Context, tenantID uint64, filter LedgerFilter) ([]LedgerEntry, error) {
	lineQ := q.db.WithContext(ctx).
		Joins("JOIN journal_entries ON journal_entries.id = journal_lines.journal_entry_id").
		Where("journal_lines.tenant_id = ? AND journal_entries.posted_at IS NOT NULL", tenantID)

	if filter.AccountID != nil {
		lineQ = lineQ.Where("journal_lines.account_id = ?", *filter.AccountID)
	}
	if filter.ProjectID != nil {
		lineQ = lineQ.Where("journal_lines.project_id = ?", *filter.ProjectID)
	}
	if filter.PhaseID != nil {
		lineQ = lineQ.Where("journal_lines.phase_id = ?", *filter.PhaseID)
	}
	if filter.UnitID != nil {
		lineQ = lineQ.Where("journal_lines.unit_id = ?", *filter.UnitID)
	}
	if filter.DateFrom != nil {
		lineQ = lineQ.Where("journal_entries.date >= ?", *filter.DateFrom)
	}
	if filter.DateTo != nil {
		lineQ = lineQ.Where("journal_entries.date <= ?", *filter.DateTo)
	}

	var lines []JournalLine
	if err := lineQ.Find(&lines).Error; err != nil {
		return nil, err
	}

	// Collect unique entry IDs, then fetch entries.
	entryIDs := make(map[uint64]bool)
	for _, l := range lines {
		entryIDs[l.JournalEntryID] = true
	}
	ids := make([]uint64, 0, len(entryIDs))
	for id := range entryIDs {
		ids = append(ids, id)
	}
	var entries []JournalEntry
	if len(ids) > 0 {
		if err := q.db.WithContext(ctx).Where("id IN ?", ids).Find(&entries).Error; err != nil {
			return nil, err
		}
	}

	var account *Account
	if filter.AccountID != nil {
		a, err := q.GetAccount(ctx, tenantID, *filter.AccountID)
		if err == nil {
			account = a
		}
	}
	return ComputeGeneralLedger(lines, account, filter, entries), nil
}

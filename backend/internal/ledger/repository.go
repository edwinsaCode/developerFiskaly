package ledger

import (
	"context"
	"fmt"
	"time"

	"gorm.io/gorm"
)

// ── GORMRepository implements both AccountLookup and JournalStore ──────────────

// GORMRepository provides GORM-backed implementations for the ledger interfaces.
// Every query is scoped to a tenant via the tenant_id column.
// Phase 2 will add middleware enforcement; here the tenantID parameter is explicit.
type GORMRepository struct {
	db *gorm.DB
}

// NewGORMRepository constructs a GORMRepository.
func NewGORMRepository(db *gorm.DB) *GORMRepository {
	return &GORMRepository{db: db}
}

// ── AccountLookup ─────────────────────────────────────────────────────────────

// FindByIDs fetches accounts by IDs, scoped to the tenant.
func (r *GORMRepository) FindByIDs(ctx context.Context, tenantID uint64, ids []uint64) ([]*Account, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	var accounts []*Account
	err := r.db.WithContext(ctx).
		Where("id IN ? AND tenant_id = ?", ids, tenantID).
		Find(&accounts).Error
	if err != nil {
		return nil, fmt.Errorf("FindByIDs: %w", err)
	}
	// Verify all requested IDs were found.
	found := make(map[uint64]bool, len(accounts))
	for _, a := range accounts {
		found[a.ID] = true
	}
	for _, id := range ids {
		if !found[id] {
			return nil, fmt.Errorf("%w: id=%d", ErrAccountNotFound, id)
		}
	}
	return accounts, nil
}

// CreateAccount inserts a new account for the tenant.
func (r *GORMRepository) CreateAccount(ctx context.Context, a *Account) error {
	if err := r.db.WithContext(ctx).Create(a).Error; err != nil {
		// MySQL duplicate key error
		if isDuplicateKeyError(err) {
			return ErrAccountCodeDuplicate
		}
		return fmt.Errorf("CreateAccount: %w", err)
	}
	return nil
}

// UpdateAccount updates a mutable account fields (name, description, is_active,
// category). Code and type are immutable after creation. Returns ErrAccountNotFound if not found.
func (r *GORMRepository) UpdateAccount(ctx context.Context, tenantID, id uint64, name, description string, isActive bool, category AccountCategory) error {
	res := r.db.WithContext(ctx).Model(&Account{}).
		Where("id = ? AND tenant_id = ?", id, tenantID).
		Updates(map[string]interface{}{
			"name":        name,
			"description": description,
			"is_active":   isActive,
			"category":    category,
		})
	if res.Error != nil {
		return fmt.Errorf("UpdateAccount: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return ErrAccountNotFound
	}
	return nil
}

// ── JournalTx ─────────────────────────────────────────────────────────────────

// InTx menjalankan fn dalam satu transaksi database, menyerahkan repository
// yang terikat transaksi tersebut. Implementasi ledger.JournalTx (W-3.0).
func (r *GORMRepository) InTx(ctx context.Context, fn func(JournalStore) error) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return fn(&GORMRepository{db: tx})
	})
}

// ── JournalStore ──────────────────────────────────────────────────────────────

// Create inserts a JournalEntry with all its Lines in one transaction.
func (r *GORMRepository) Create(ctx context.Context, entry *JournalEntry) error {
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return tx.Create(entry).Error
	})
	if err != nil {
		return fmt.Errorf("Create journal: %w", err)
	}
	return nil
}

// FindByID retrieves a JournalEntry with its Lines preloaded.
func (r *GORMRepository) FindByID(ctx context.Context, tenantID, id uint64) (*JournalEntry, error) {
	var e JournalEntry
	err := r.db.WithContext(ctx).
		Preload("Lines").
		Preload("Lines.Account").
		First(&e, "id = ? AND tenant_id = ?", id, tenantID).Error
	if err != nil {
		return nil, fmt.Errorf("FindByID journal %d: %w", id, err)
	}
	return &e, nil
}

// MarkPosted sets PostedAt atomically.
// Returns ErrJournalAlreadyPosted if the entry is already posted.
func (r *GORMRepository) MarkPosted(ctx context.Context, tenantID, id uint64, at time.Time) error {
	res := r.db.WithContext(ctx).Model(&JournalEntry{}).
		Where("id = ? AND tenant_id = ? AND posted_at IS NULL", id, tenantID).
		Update("posted_at", at)
	if res.Error != nil {
		return fmt.Errorf("MarkPosted: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return ErrJournalAlreadyPosted
	}
	return nil
}

// HasReversingEntry checks whether any reversing entry already exists for the given entry.
func (r *GORMRepository) HasReversingEntry(ctx context.Context, tenantID, id uint64) (bool, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&JournalEntry{}).
		Where("reverses_id = ? AND tenant_id = ?", id, tenantID).
		Count(&count).Error
	return count > 0, err
}

// UpdateDraft updates a draft (unposted) journal. Returns ErrJournalAlreadyPosted if posted.
func (r *GORMRepository) UpdateDraft(ctx context.Context, tenantID, id uint64, updates map[string]interface{}) error {
	var e JournalEntry
	if err := r.db.WithContext(ctx).First(&e, "id = ? AND tenant_id = ?", id, tenantID).Error; err != nil {
		return err
	}
	if e.PostedAt != nil {
		return ErrJournalAlreadyPosted
	}
	return r.db.WithContext(ctx).Model(&e).Updates(updates).Error
}

// DeleteDraft deletes a draft (unposted) journal and its lines. Returns ErrJournalAlreadyPosted if posted.
func (r *GORMRepository) DeleteDraft(ctx context.Context, tenantID, id uint64) error {
	var e JournalEntry
	if err := r.db.WithContext(ctx).First(&e, "id = ? AND tenant_id = ?", id, tenantID).Error; err != nil {
		return err
	}
	if e.PostedAt != nil {
		return ErrJournalAlreadyPosted
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("journal_entry_id = ?", id).Delete(&JournalLine{}).Error; err != nil {
			return err
		}
		return tx.Delete(&e).Error
	})
}

// ── PeriodChecker (implements PeriodChecker interface) ────────────────────────

// IsPeriodClosed reports whether the calendar month containing `date` is closed for the tenant.
// Returns false if no accounting_period record exists (open by default).
func (r *GORMRepository) IsPeriodClosed(ctx context.Context, tenantID uint64, date time.Time) (bool, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&AccountingPeriod{}).
		Where("tenant_id = ? AND year = ? AND month = ? AND status = 'closed'",
			tenantID, date.Year(), int(date.Month())).
		Count(&count).Error
	return count > 0, err
}

// ── Period CRUD ───────────────────────────────────────────────────────────────

// ListPeriods returns accounting_periods merged with distinct months from journal_entries.
// Months with journal activity but no period record appear with status "open".
func (r *GORMRepository) ListPeriods(ctx context.Context, tenantID uint64) ([]*AccountingPeriod, error) {
	type ym struct {
		Year  int
		Month int
	}
	// Collect distinct year/month from journal_entries.
	var rows []ym
	err := r.db.WithContext(ctx).
		Table("journal_entries").
		Select("YEAR(date) AS year, MONTH(date) AS month").
		Where("tenant_id = ?", tenantID).
		Group("YEAR(date), MONTH(date)").
		Order("year DESC, month DESC").
		Scan(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("ListPeriods scan journals: %w", err)
	}

	// Fetch existing period records.
	var existing []*AccountingPeriod
	if err := r.db.WithContext(ctx).
		Where("tenant_id = ?", tenantID).
		Find(&existing).Error; err != nil {
		return nil, fmt.Errorf("ListPeriods scan periods: %w", err)
	}

	// Index existing records.
	type key struct{ Year, Month int }
	index := make(map[key]*AccountingPeriod, len(existing))
	for _, p := range existing {
		index[key{p.Year, p.Month}] = p
	}

	// Merge: journal months not yet in accounting_periods appear as virtual "open" entries.
	result := make([]*AccountingPeriod, 0, len(rows))
	for _, r2 := range rows {
		k := key{r2.Year, r2.Month}
		if p, ok := index[k]; ok {
			result = append(result, p)
		} else {
			result = append(result, &AccountingPeriod{
				TenantID: tenantID,
				Year:     r2.Year,
				Month:    r2.Month,
				Status:   "open",
			})
		}
	}
	// Also include period records that exist but have no journal entries.
	for _, p := range existing {
		k := key{p.Year, p.Month}
		found := false
		for _, r2 := range rows {
			if r2.Year == k.Year && r2.Month == k.Month {
				found = true
				break
			}
		}
		if !found {
			result = append(result, p)
		}
	}
	return result, nil
}

// UpsertPeriodClose creates or updates an accounting_period as closed.
// Returns ErrPeriodAlreadyClosed if already closed.
func (r *GORMRepository) UpsertPeriodClose(ctx context.Context, tenantID uint64, year, month int, closedBy uint64) (*AccountingPeriod, error) {
	var p AccountingPeriod
	err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND year = ? AND month = ?", tenantID, year, month).
		FirstOrCreate(&p, AccountingPeriod{TenantID: tenantID, Year: year, Month: month, Status: "open"}).Error
	if err != nil {
		return nil, fmt.Errorf("UpsertPeriodClose: %w", err)
	}
	if p.Status == "closed" {
		return nil, ErrPeriodAlreadyClosed
	}
	now := time.Now()
	p.Status = "closed"
	p.ClosedAt = &now
	p.ClosedBy = &closedBy
	if err := r.db.WithContext(ctx).Save(&p).Error; err != nil {
		return nil, fmt.Errorf("UpsertPeriodClose save: %w", err)
	}
	return &p, nil
}

// ReopenPeriod sets an existing closed period back to "open".
// Returns ErrPeriodNotFound if no record, ErrPeriodNotClosed if already open.
func (r *GORMRepository) ReopenPeriod(ctx context.Context, tenantID uint64, year, month int) (*AccountingPeriod, error) {
	var p AccountingPeriod
	err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND year = ? AND month = ?", tenantID, year, month).
		First(&p).Error
	if err != nil {
		return nil, ErrPeriodNotFound
	}
	if p.Status == "open" {
		return nil, ErrPeriodNotClosed
	}
	p.Status = "open"
	p.ClosedAt = nil
	p.ClosedBy = nil
	if err := r.db.WithContext(ctx).Save(&p).Error; err != nil {
		return nil, fmt.Errorf("ReopenPeriod save: %w", err)
	}
	return &p, nil
}

// isDuplicateKeyError detects MySQL duplicate key violation (error 1062).
func isDuplicateKeyError(err error) bool {
	if err == nil {
		return false
	}
	return len(err.Error()) > 0 && containsAny(err.Error(), "Duplicate entry", "duplicate key", "1062")
}

func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if len(s) >= len(sub) {
			for i := 0; i <= len(s)-len(sub); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
		}
	}
	return false
}

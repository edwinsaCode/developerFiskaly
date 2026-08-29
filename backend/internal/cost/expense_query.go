package cost

// Pembaca daftar Transaksi Pengeluaran (W-10).
//
// §8 keputusan pemilik: TIDAK ADA status akuntansi yang disimpan. Status yang
// dilihat user diturunkan setiap kali dibaca dari keadaan kanonik jurnal:
//
//	posted_at NULL                        → "draft"
//	posted_at terisi, tidak ada pembalik  → "posted"
//	ada jurnal lain yang membalikkannya   → "reversed"
//
// Kolom status yang disimpan akan drift begitu sebuah jurnal dibalik lewat
// jalur mana pun — dan pembalikan memang boleh terjadi dari luar modul ini.

import (
	"context"
	"fmt"
	"time"

	"esaproperti/internal/domain"
)

// ExpenseStatus adalah status turunan sebuah transaksi pengeluaran.
type ExpenseStatus string

const (
	ExpenseStatusDraft    ExpenseStatus = "draft"
	ExpenseStatusPosted   ExpenseStatus = "posted"
	ExpenseStatusReversed ExpenseStatus = "reversed"
)

// ExpenseListItem adalah satu baris daftar pengeluaran, lengkap dengan jejak
// akuntansinya (nomor dokumen + jurnal) supaya user tidak perlu membuka modul
// lain untuk membuktikan sebuah transaksi benar-benar tercatat.
type ExpenseListItem struct {
	ID          uint64       `json:"id"`
	Date        time.Time    `json:"date"`
	Amount      domain.Money `json:"amount"`
	Vendor      string       `json:"vendor"`
	Description string       `json:"description"`

	Scope           string  `json:"scope"` // "operasional" | "proyek"
	ExpenseTypeID   *uint64 `json:"expense_type_id,omitempty"`
	ExpenseTypeName string  `json:"expense_type_name,omitempty"`
	Category        string  `json:"category"`
	CostTier        string  `json:"cost_tier"`

	ProjectID    *uint64 `json:"project_id,omitempty"`
	ProjectName  string  `json:"project_name,omitempty"`
	UnitID       *uint64 `json:"unit_id,omitempty"`
	UnitCode     string  `json:"unit_code,omitempty"`
	BudgetItemID *uint64 `json:"budget_item_id,omitempty"`
	// IsRABRealization menegaskan BD-2 di layar: tag proyek saja TIDAK membuat
	// sebuah biaya menjadi realisasi RAB — hanya tautan ke item RAB yang membuatnya.
	IsRABRealization bool `json:"is_rab_realization"`

	PaymentMethod   string `json:"payment_method"`
	BankAccountCode string `json:"bank_account_code,omitempty"`
	BankAccountName string `json:"bank_account_name,omitempty"`

	JournalEntryID uint64        `json:"journal_entry_id"`
	DocumentNumber string        `json:"document_number,omitempty"`
	Status         ExpenseStatus `json:"status"`
}

// ExpenseFilter menyaring daftar pengeluaran.
type ExpenseFilter struct {
	ID        *uint64 // satu transaksi tertentu
	From      *time.Time
	To        *time.Time
	Scope     string  // "" = semua, "operasional", "proyek"
	ProjectID *uint64 // hanya bermakna untuk scope proyek
	Limit     int
}

// ExpenseReader membaca daftar/detail transaksi pengeluaran beserta jejak
// akuntansinya. Implementasi produksi: GORMRepository.
type ExpenseReader interface {
	ListExpenses(ctx context.Context, tenantID uint64, f ExpenseFilter) ([]*ExpenseListItem, error)
}

// ListExpenses meneruskan ke pembaca; nil = fitur tidak terpasang.
func (s *Service) ListExpenses(ctx context.Context, tenantID uint64, f ExpenseFilter) ([]*ExpenseListItem, error) {
	if s.expenses == nil {
		return nil, fmt.Errorf("pembaca pengeluaran tidak terpasang")
	}
	return s.expenses.ListExpenses(ctx, tenantID, f)
}

// GetExpense mengembalikan satu transaksi pengeluaran lengkap dengan status,
// nomor dokumen, dan referensi jurnalnya.
func (s *Service) GetExpense(ctx context.Context, tenantID, id uint64) (*ExpenseListItem, error) {
	items, err := s.ListExpenses(ctx, tenantID, ExpenseFilter{ID: &id, Limit: 1})
	if err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, ErrCostEntryNotFound
	}
	return items[0], nil
}

// SetExpenseReader memasang pembaca daftar pengeluaran (W-10).
func (s *Service) SetExpenseReader(rd ExpenseReader) { s.expenses = rd }

// ListExpenses mengembalikan daftar transaksi pengeluaran tenant.
//
// Satu query dengan LEFT JOIN: jurnal (status), dokumen (nomor BKK), master
// jenis pengeluaran, proyek, unit, dan akun kas/bank. LEFT — bukan INNER —
// supaya baris yang datanya tidak lengkap tetap terlihat; menyembunyikan
// transaksi dari daftar adalah cara terburuk melaporkan sebuah anomali.
func (r *GORMRepository) ListExpenses(ctx context.Context, tenantID uint64, f ExpenseFilter) ([]*ExpenseListItem, error) {
	limit := f.Limit
	if limit <= 0 || limit > 500 {
		limit = 200
	}

	type row struct {
		ID              uint64
		Date            time.Time
		Amount          domain.Money
		Vendor          string
		Description     string
		Category        string
		CostTier        string
		ExpenseTypeID   *uint64
		ExpenseTypeName *string
		ProjectID       *uint64
		ProjectName     *string
		UnitID          *uint64
		UnitCode        *string
		BudgetItemID    *uint64
		PaymentMethod   string
		BankAccountCode string
		BankAccountName *string
		JournalEntryID  uint64
		PostedAt        *time.Time
		DocumentNumber  *string
		ReversalID      *uint64
	}

	q := r.db.WithContext(ctx).
		Table("cost_entries ce").
		Select(`ce.id, ce.date, ce.amount, ce.vendor, ce.description,
			ce.category, ce.cost_tier, ce.expense_type_id, et.name AS expense_type_name,
			ce.project_id, p.name AS project_name,
			ce.unit_id, u.code AS unit_code, ce.budget_item_id,
			ce.payment_method, ce.bank_account_code, acc.name AS bank_account_name,
			ce.journal_entry_id, je.posted_at, doc.number AS document_number,
			rev.id AS reversal_id`).
		Joins("LEFT JOIN expense_types et ON et.id = ce.expense_type_id AND et.tenant_id = ce.tenant_id").
		Joins("LEFT JOIN projects p ON p.id = ce.project_id AND p.tenant_id = ce.tenant_id").
		Joins("LEFT JOIN units u ON u.id = ce.unit_id AND u.tenant_id = ce.tenant_id").
		Joins("LEFT JOIN accounts acc ON acc.code = ce.bank_account_code AND acc.tenant_id = ce.tenant_id").
		Joins("LEFT JOIN journal_entries je ON je.id = ce.journal_entry_id AND je.tenant_id = ce.tenant_id").
		Joins("LEFT JOIN documents doc ON doc.id = je.document_id AND doc.tenant_id = ce.tenant_id").
		Joins("LEFT JOIN journal_entries rev ON rev.reverses_id = je.id AND rev.tenant_id = ce.tenant_id AND rev.posted_at IS NOT NULL").
		Where("ce.tenant_id = ?", tenantID)

	if f.ID != nil {
		q = q.Where("ce.id = ?", *f.ID)
	}
	if f.From != nil {
		q = q.Where("ce.date >= ?", *f.From)
	}
	if f.To != nil {
		q = q.Where("ce.date <= ?", *f.To)
	}
	switch f.Scope {
	case "operasional":
		q = q.Where("ce.expense_type_id IS NOT NULL")
	case "proyek":
		q = q.Where("ce.expense_type_id IS NULL AND ce.project_id IS NOT NULL")
	}
	if f.ProjectID != nil {
		q = q.Where("ce.project_id = ?", *f.ProjectID)
	}

	var rows []row
	if err := q.Order("ce.date DESC, ce.id DESC").Limit(limit).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("ListExpenses: %w", err)
	}

	out := make([]*ExpenseListItem, 0, len(rows))
	for _, rw := range rows {
		item := &ExpenseListItem{
			ID:               rw.ID,
			Date:             rw.Date,
			Amount:           rw.Amount,
			Vendor:           rw.Vendor,
			Description:      rw.Description,
			Scope:            expenseScopeOf(rw.ExpenseTypeID),
			ExpenseTypeID:    rw.ExpenseTypeID,
			ExpenseTypeName:  deref(rw.ExpenseTypeName),
			Category:         rw.Category,
			CostTier:         rw.CostTier,
			ProjectID:        rw.ProjectID,
			ProjectName:      deref(rw.ProjectName),
			UnitID:           rw.UnitID,
			UnitCode:         deref(rw.UnitCode),
			BudgetItemID:     rw.BudgetItemID,
			IsRABRealization: rw.BudgetItemID != nil,
			PaymentMethod:    rw.PaymentMethod,
			BankAccountCode:  rw.BankAccountCode,
			BankAccountName:  deref(rw.BankAccountName),
			JournalEntryID:   rw.JournalEntryID,
			DocumentNumber:   deref(rw.DocumentNumber),
			Status:           expenseStatusOf(rw.PostedAt, rw.ReversalID),
		}
		out = append(out, item)
	}
	return out, nil
}

// expenseStatusOf menurunkan status dari keadaan jurnal (§8) — tidak pernah
// dibaca dari kolom tersimpan.
func expenseStatusOf(postedAt *time.Time, reversalID *uint64) ExpenseStatus {
	switch {
	case reversalID != nil:
		return ExpenseStatusReversed
	case postedAt != nil:
		return ExpenseStatusPosted
	default:
		return ExpenseStatusDraft
	}
}

func expenseScopeOf(expenseTypeID *uint64) string {
	if expenseTypeID != nil {
		return "operasional"
	}
	return "proyek"
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

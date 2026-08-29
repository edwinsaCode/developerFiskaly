package ledger

// PS-5 — Jurnal Berulang: template konfigurasi → dieksekusi menjadi jurnal
// POSTED biasa via PostingService (balanced dijamin engine; period-checker
// tetap menjaga). Idempoten per (template, bulan) via last_run_ym.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"

	"esaproperti/internal/domain"
)

// RecurringLine adalah satu baris template (uang string desimal — Invariant #2).
type RecurringLine struct {
	AccountCode string `json:"account_code"`
	Debit       string `json:"debit"`
	Credit      string `json:"credit"`
	Description string `json:"description,omitempty"`
}

// RecurringJournal adalah template jurnal bulanan.
type RecurringJournal struct {
	ID          uint64    `gorm:"primaryKey;autoIncrement" json:"id"`
	TenantID    uint64    `gorm:"not null;index"           json:"-"`
	Name        string    `gorm:"size:200;not null"        json:"name"`
	Description string    `gorm:"size:500"                 json:"description,omitempty"`
	DayOfMonth  int       `gorm:"not null;default:1"       json:"day_of_month"`
	LinesJSON   string    `gorm:"column:lines;type:json"   json:"-"`
	IsActive    bool      `gorm:"not null;default:true"    json:"is_active"`
	LastRunYM   string    `gorm:"size:7;column:last_run_ym" json:"last_run_ym,omitempty"`
	CreatedBy   *uint64   `json:"created_by,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`

	Lines []RecurringLine `gorm:"-" json:"lines"`
	// Total (S9/R-9, derived): Σ debit baris template — dihitung backend
	// (decimal), FE hanya menampilkan.
	Total string `gorm:"-" json:"total"`
}

// computeTotal mengisi Total = Σ debit baris (jurnal balanced: Σdebit == Σkredit).
func (r *RecurringJournal) computeTotal() {
	total := domain.Zero
	for _, l := range r.Lines {
		if m, err := domain.NewMoney(l.Debit); err == nil {
			total = total.Add(m)
		}
	}
	r.Total = total.String()
}

func (RecurringJournal) TableName() string { return "recurring_journals" }

var (
	ErrRecurringNotFound = errors.New("template jurnal berulang tidak ditemukan")
	ErrRecurringInvalid  = errors.New("template jurnal berulang tidak valid")

	// errRecurringRaced: guard idempoten kalah — runner lain sudah mengeksekusi
	// template ini untuk bulan yang sama. Bukan kegagalan; transaksi dibatalkan
	// supaya jurnal tidak terposting dua kali (lihat RunDue).
	errRecurringRaced = errors.New("template sudah dieksekusi untuk bulan ini")
)

// RecurringService mengelola template + eksekusi.
type RecurringService struct {
	db      *gorm.DB
	posting *PostingService
}

func NewRecurringService(db *gorm.DB, posting *PostingService) *RecurringService {
	return &RecurringService{db: db, posting: posting}
}

func (s *RecurringService) validate(r *RecurringJournal) error {
	if r.Name == "" || r.DayOfMonth < 1 || r.DayOfMonth > 28 || len(r.Lines) < 2 {
		return ErrRecurringInvalid
	}
	var totalD, totalC domain.Money
	for _, l := range r.Lines {
		if l.AccountCode == "" {
			return fmt.Errorf("%w: account_code kosong", ErrRecurringInvalid)
		}
		d, err := domain.NewMoney(nzMoney(l.Debit))
		if err != nil {
			return fmt.Errorf("%w: debit %q", ErrRecurringInvalid, l.Debit)
		}
		c, err := domain.NewMoney(nzMoney(l.Credit))
		if err != nil {
			return fmt.Errorf("%w: credit %q", ErrRecurringInvalid, l.Credit)
		}
		totalD = totalD.Add(d)
		totalC = totalC.Add(c)
	}
	if !totalD.Equal(totalC) || totalD.IsZero() {
		return fmt.Errorf("%w: Σ debit harus == Σ kredit dan > 0", ErrRecurringInvalid)
	}
	return nil
}

func nzMoney(s string) string {
	if s == "" {
		return "0"
	}
	return s
}

func (s *RecurringService) Create(ctx context.Context, tenantID uint64, r *RecurringJournal) (*RecurringJournal, error) {
	if err := s.validate(r); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(r.Lines)
	if err != nil {
		return nil, err
	}
	r.TenantID = tenantID
	r.LinesJSON = string(raw)
	if err := s.db.WithContext(ctx).Create(r).Error; err != nil {
		return nil, fmt.Errorf("simpan template: %w", err)
	}
	return r, nil
}

func (s *RecurringService) SetActive(ctx context.Context, tenantID, id uint64, active bool) (*RecurringJournal, error) {
	res := s.db.WithContext(ctx).Model(&RecurringJournal{}).
		Where("id = ? AND tenant_id = ?", id, tenantID).
		Update("is_active", active)
	if res.Error != nil {
		return nil, res.Error
	}
	if res.RowsAffected == 0 {
		return nil, ErrRecurringNotFound
	}
	return s.Get(ctx, tenantID, id)
}

func (s *RecurringService) Get(ctx context.Context, tenantID, id uint64) (*RecurringJournal, error) {
	var r RecurringJournal
	err := s.db.WithContext(ctx).Where("id = ? AND tenant_id = ?", id, tenantID).First(&r).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrRecurringNotFound
		}
		return nil, err
	}
	_ = json.Unmarshal([]byte(r.LinesJSON), &r.Lines)
	r.computeTotal()
	return &r, nil
}

func (s *RecurringService) List(ctx context.Context, tenantID uint64) ([]*RecurringJournal, error) {
	var out []*RecurringJournal
	if err := s.db.WithContext(ctx).Where("tenant_id = ?", tenantID).
		Order("id DESC").Find(&out).Error; err != nil {
		return nil, err
	}
	for _, r := range out {
		_ = json.Unmarshal([]byte(r.LinesJSON), &r.Lines)
		r.computeTotal()
	}
	return out, nil
}

// RunDue mengeksekusi semua template aktif yang jatuh tempo bulan ini dan
// belum dieksekusi (idempoten via last_run_ym; guard pinned saat update).
func (s *RecurringService) RunDue(ctx context.Context, tenantID uint64, now time.Time, actorID *uint64) (int, error) {
	ym := now.Format("2006-01")
	templates, err := s.List(ctx, tenantID)
	if err != nil {
		return 0, err
	}
	created := 0
	var firstErr error
	for _, t := range templates {
		if !t.IsActive || t.LastRunYM >= ym || now.Day() < t.DayOfMonth {
			continue
		}
		// Resolve akun per kode (tenant-scoped).
		lines := make([]LineInput, 0, len(t.Lines))
		skip := false
		for _, l := range t.Lines {
			var acc Account
			if err := s.db.WithContext(ctx).
				Where("tenant_id = ? AND code = ?", tenantID, l.AccountCode).
				First(&acc).Error; err != nil {
				firstErr = fmt.Errorf("template %q: akun %s tidak ditemukan", t.Name, l.AccountCode)
				skip = true
				break
			}
			d, _ := domain.NewMoney(nzMoney(l.Debit))
			c, _ := domain.NewMoney(nzMoney(l.Credit))
			lines = append(lines, LineInput{
				AccountID: acc.ID, Debit: d, Credit: c, Description: l.Description,
			})
		}
		if skip {
			continue
		}
		date := time.Date(now.Year(), now.Month(), t.DayOfMonth, 0, 0, 0, 0, now.Location())
		req := CreateJournalRequest{
			TenantID:    tenantID,
			Date:        date,
			Description: fmt.Sprintf("%s (jurnal berulang %s)", t.Name, ym),
			Reference:   fmt.Sprintf("RJ-%d-%s", t.ID, ym),
			Source:      "recurring",
			CreatedBy:   actorID,
			Lines:       lines,
		}

		// W-3.0 (B-6): jurnal DAN guard idempoten dalam SATU transaksi.
		// Sebelumnya terpisah — gagal di antara Post dan pin last_run_ym membuat
		// eksekusi berikutnya memposting jurnal yang sama untuk bulan yang sama,
		// dan dua runner bersamaan bisa lolos guard dua kali. Template berulang
		// bisa mengkredit kas (jalur O-10), jadi lubang ini menghasilkan
		// pergerakan kas ganda tanpa jejak dokumen.
		err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			txRepo := NewGORMRepository(tx)
			txPosting := NewPostingService(txRepo, txRepo)
			if s.posting != nil && s.posting.periods != nil {
				txPosting = txPosting.WithPeriodChecker(s.posting.periods)
			}
			// W-3.2 (I-8/O-10): template berulang bisa menyentuh kas. Jenisnya
			// disimpulkan dari arah pergerakan — BKM kalau kas bertambah, BKK
			// kalau berkurang — karena template tidak punya lawan bicara yang
			// bisa ditanya saat cron berjalan.
			spec, err := txPosting.DefaultCashSpec(ctx, tenantID, req.Lines)
			if err != nil {
				return err
			}
			req.Document = spec
			if _, err := txPosting.CreateAndPost(ctx, req); err != nil {
				return err
			}
			res := tx.WithContext(ctx).Model(&RecurringJournal{}).
				Where("id = ? AND tenant_id = ? AND last_run_ym = ?", t.ID, tenantID, t.LastRunYM).
				Update("last_run_ym", ym)
			if res.Error != nil {
				return res.Error
			}
			if res.RowsAffected == 0 {
				return errRecurringRaced
			}
			return nil
		})
		switch {
		case err == nil:
			created++
		case errors.Is(err, errRecurringRaced):
			// Sudah dieksekusi runner lain — jurnal ikut dibatalkan. Bukan error.
		default:
			if firstErr == nil {
				firstErr = fmt.Errorf("template %q: %w", t.Name, err)
			}
		}
	}
	return created, firstErr
}

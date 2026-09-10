package budget

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"

	"esaproperti/internal/domain"
	"esaproperti/internal/ledger"
)

// ── GORMRepository implements BudgetStore ────────────────────────────────────

type GORMRepository struct {
	db *gorm.DB
}

func NewGORMRepository(db *gorm.DB) *GORMRepository {
	return &GORMRepository{db: db}
}

func (r *GORMRepository) SavePlan(ctx context.Context, plan *BudgetPlan) error {
	// Isi PhaseIDKey dari PhaseID sebelum INSERT.
	// PhaseIDKey = 0 berarti project-level (phase_id IS NULL).
	// Ini dibutuhkan oleh UNIQUE INDEX udx_one_active yang tidak bisa memakai NULL.
	plan.PhaseIDKey = phaseIDKey(plan.PhaseID)
	// Plan baru selalu draft — active_key NULL (diizinkan banyak oleh unique index).
	plan.ActiveKey = nil
	if err := r.db.WithContext(ctx).Create(plan).Error; err != nil {
		return fmt.Errorf("budget: SavePlan: %w", err)
	}
	return nil
}

func (r *GORMRepository) FindPlanByID(ctx context.Context, tenantID, id uint64) (*BudgetPlan, error) {
	var plan BudgetPlan
	err := r.db.WithContext(ctx).
		Where("id = ? AND tenant_id = ?", id, tenantID).
		First(&plan).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrPlanNotFound
		}
		return nil, fmt.Errorf("budget: FindPlanByID: %w", err)
	}
	return &plan, nil
}

func (r *GORMRepository) FindActivePlan(ctx context.Context, tenantID, projectID uint64, phaseID *uint64) (*BudgetPlan, error) {
	var plan BudgetPlan
	q := r.db.WithContext(ctx).
		Where("tenant_id = ? AND project_id = ? AND status = 'active'", tenantID, projectID)
	q = applyPhaseFilter(q, phaseID)
	err := q.First(&plan).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrPlanNotFound
		}
		return nil, fmt.Errorf("budget: FindActivePlan: %w", err)
	}
	return &plan, nil
}

// ListPlansByProject mengembalikan semua plan untuk project.
// phaseID=nil: semua plan untuk project (tidak ada filter fase).
// phaseID!=nil: hanya plan untuk fase tersebut.
func (r *GORMRepository) ListPlansByProject(ctx context.Context, tenantID, projectID uint64, phaseID *uint64) ([]*BudgetPlan, error) {
	var plans []*BudgetPlan
	q := r.db.WithContext(ctx).
		Where("tenant_id = ? AND project_id = ?", tenantID, projectID)
	if phaseID != nil {
		q = q.Where("phase_id = ?", *phaseID)
	}
	if err := q.Order("version DESC").Find(&plans).Error; err != nil {
		return nil, fmt.Errorf("budget: ListPlansByProject: %w", err)
	}
	return plans, nil
}

// ApproveAndSupersede adalah operasi atomik yang:
//  1. Men-supersede plan active yang ada untuk (project, phase) yang sama.
//  2. Mengaktifkan plan target.
//
// Menegakkan INVARIANT #8: hanya satu plan 'active' per (project, phase).
func (r *GORMRepository) ApproveAndSupersede(ctx context.Context, tenantID, planID uint64, now time.Time, approvedBy string) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Ambil plan target untuk mendapatkan project_id dan phase_id.
		var target BudgetPlan
		if err := tx.First(&target, "id = ? AND tenant_id = ?", planID, tenantID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrPlanNotFound
			}
			return fmt.Errorf("budget: ApproveAndSupersede lookup: %w", err)
		}

		// Supersede plan active yang ada: SET active_key = NULL (rilis slot unique).
		// Harus dilakukan SEBELUM mengaktifkan plan baru agar tidak ada konflik index.
		q := tx.Model(&BudgetPlan{}).
			Where("tenant_id = ? AND project_id = ? AND status = 'active' AND id != ?",
				tenantID, target.ProjectID, planID)
		q = applyPhaseFilter(q, target.PhaseID)
		if err := q.Updates(map[string]interface{}{
			"status":     BudgetPlanStatusSuperseded,
			"active_key": nil,
			"updated_at": now,
		}).Error; err != nil {
			return fmt.Errorf("budget: ApproveAndSupersede supersede: %w", err)
		}

		// Aktifkan plan target: SET active_key = 'Y'.
		// UNIQUE INDEX (tenant_id, project_id, phase_id_key, active_key) memastikan
		// hanya satu row 'Y' per (tenant, project, phase) yang bisa ada di DB.
		activeMarker := "Y"
		updates := map[string]interface{}{
			"status":      BudgetPlanStatusActive,
			"active_key":  activeMarker,
			"approved_at": now,
			"updated_at":  now,
		}
		if approvedBy != "" {
			updates["approved_by"] = approvedBy
		}
		if err := tx.Model(&BudgetPlan{}).
			Where("id = ? AND tenant_id = ?", planID, tenantID).
			Updates(updates).Error; err != nil {
			return fmt.Errorf("budget: ApproveAndSupersede activate: %w", err)
		}
		return nil
	})
}

func (r *GORMRepository) SaveItem(ctx context.Context, item *BudgetItem) error {
	if err := r.db.WithContext(ctx).Create(item).Error; err != nil {
		return fmt.Errorf("budget: SaveItem: %w", err)
	}
	return nil
}

func (r *GORMRepository) FindItemByID(ctx context.Context, tenantID, id uint64) (*BudgetItem, error) {
	var item BudgetItem
	err := r.db.WithContext(ctx).
		Where("id = ? AND tenant_id = ?", id, tenantID).
		First(&item).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrItemNotFound
		}
		return nil, fmt.Errorf("budget: FindItemByID: %w", err)
	}
	return &item, nil
}

func (r *GORMRepository) ListItemsByPlan(ctx context.Context, tenantID, planID uint64) ([]*BudgetItem, error) {
	var items []*BudgetItem
	if err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND budget_plan_id = ?", tenantID, planID).
		Order("id ASC").
		Find(&items).Error; err != nil {
		return nil, fmt.Errorf("budget: ListItemsByPlan: %w", err)
	}
	return items, nil
}

func (r *GORMRepository) DeleteItem(ctx context.Context, tenantID, planID, itemID uint64) error {
	result := r.db.WithContext(ctx).
		Where("id = ? AND tenant_id = ? AND budget_plan_id = ?", itemID, tenantID, planID).
		Delete(&BudgetItem{})
	if result.Error != nil {
		return fmt.Errorf("budget: DeleteItem: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return ErrItemNotFound
	}
	return nil
}

type categorySum struct {
	Category BudgetCategory `gorm:"column:category"`
	Total    domain.Money   `gorm:"column:total"`
}

func (r *GORMRepository) SumItemsByCategory(ctx context.Context, tenantID, planID uint64) (map[BudgetCategory]domain.Money, error) {
	var rows []categorySum
	if err := r.db.WithContext(ctx).
		Model(&BudgetItem{}).
		Select("category, SUM(budgeted_amount) as total").
		Where("tenant_id = ? AND budget_plan_id = ?", tenantID, planID).
		Group("category").
		Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("budget: SumItemsByCategory: %w", err)
	}
	result := make(map[BudgetCategory]domain.Money, len(rows))
	for _, row := range rows {
		result[row.Category] = row.Total
	}
	return result, nil
}

// SumActiveBudgetByProject (S5): total RAB TERANGGARKAN per proyek — Σ
// budgeted_amount seluruh item pada SEMUA plan status 'active' milik proyek
// (lintas fase). SATU-SATUNYA implementasi "budget_total per proyek";
// dashboard membaca lewat budget.Service, tidak menulis SQL sendiri lagi.
func (r *GORMRepository) SumActiveBudgetByProject(ctx context.Context, tenantID uint64) (map[uint64]domain.Money, error) {
	var rows []struct {
		ProjectID uint64       `gorm:"column:project_id"`
		Total     domain.Money `gorm:"column:total"`
	}
	if err := r.db.WithContext(ctx).
		Table("budget_plans bp").
		Joins("JOIN budget_items bi ON bi.budget_plan_id = bp.id AND bi.tenant_id = bp.tenant_id").
		Select("bp.project_id AS project_id, SUM(bi.budgeted_amount) AS total").
		Where("bp.tenant_id = ? AND bp.status = ?", tenantID, BudgetPlanStatusActive).
		Group("bp.project_id").
		Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("budget: SumActiveBudgetByProject: %w", err)
	}
	result := make(map[uint64]domain.Money, len(rows))
	for _, row := range rows {
		result[row.ProjectID] = row.Total
	}
	return result, nil
}

// FindItemForCostLookup digunakan oleh cost handler (via adapter) untuk memvalidasi
// referensi budget_item_id dari cost entry.
// Mengembalikan (projectID, costCategory, isMappable, error).
// Sejak Increment 2, kategori expense-only (marketing/other) JUGA mappable —
// mereka terealisasi lewat CostEntry tier overhead (marketing→marketing,
// other→other). isMappable=false hanya untuk kategori tak dikenal.
// Error: ErrItemNotFound jika item tidak ditemukan untuk tenant ini.
func (r *GORMRepository) FindItemForCostLookup(ctx context.Context, tenantID, itemID uint64) (projectID uint64, costCat domain.CostCategory, isMappable bool, err error) {
	var row struct {
		ProjectID uint64         `gorm:"column:project_id"`
		Category  BudgetCategory `gorm:"column:category"`
	}
	err = r.db.WithContext(ctx).
		Table("budget_items bi").
		Select("bp.project_id, bi.category").
		Joins("JOIN budget_plans bp ON bp.id = bi.budget_plan_id AND bp.tenant_id = bi.tenant_id").
		Where("bi.id = ? AND bi.tenant_id = ?", itemID, tenantID).
		Scan(&row).Error
	if err != nil {
		return 0, "", false, fmt.Errorf("budget: FindItemForCostLookup: %w", err)
	}
	if row.ProjectID == 0 {
		return 0, "", false, ErrItemNotFound
	}
	cc, ok := row.Category.CostEntryCategory()
	return row.ProjectID, cc, ok, nil
}

// ── applyPhaseFilter ─────────────────────────────────────────────────────────
// MySQL tidak mendukung RLS, jadi NULL harus di-handle secara eksplisit.
// phaseID=nil → WHERE phase_id IS NULL
// phaseID!=nil → WHERE phase_id = ?
func applyPhaseFilter(q *gorm.DB, phaseID *uint64) *gorm.DB {
	if phaseID == nil {
		return q.Where("phase_id IS NULL")
	}
	return q.Where("phase_id = ?", *phaseID)
}

// phaseIDKey mengkonversi *uint64 phase ke nilai non-NULL untuk unique index.
// 0 = project-level (tanpa fase); ≥1 = ID fase.
// Dibutuhkan karena MySQL mengizinkan banyak NULL dalam UNIQUE index sehingga
// phase_id IS NULL tidak bisa dipakai langsung sebagai kolom index.
func phaseIDKey(phaseID *uint64) uint64 {
	if phaseID == nil {
		return 0
	}
	return *phaseID
}

// ── GORMRealisasiProvider implements RealisasiProvider ──────────────────────

// GORMRealisasiProvider (S5) membaca realisasi dari LEDGER posted via pembaca
// kanonik ledger.ActualCostByCode — BUKAN lagi Σ cost_entries.amount.
// cost_entries tetap dokumen sumber (audit trail + drill-down per item), tapi
// TOTAL realisasi keuangan berasal dari jurnal (registry #13, keputusan PO #2:
// netting = debit non-reversal − kredit reversal; relief BAST TIDAK mengurangi).
// Jurnal manual ber-tag project pun kini terhitung (SoT = jurnal).
type GORMRealisasiProvider struct {
	db *gorm.DB
}

func NewGORMRealisasiProvider(db *gorm.DB) *GORMRealisasiProvider {
	return &GORMRealisasiProvider{db: db}
}

// GetRealisasiByProject: total realisasi per kategori dari ledger posted.
// Kategori kapitalisasi via akun Persediaan (1-3xxx); kategori beban
// (marketing/other) via akun beban (5-3000/5-4000) — pemetaan taxonomy domain.
// phaseID=nil: semua baris project; phaseID!=nil: hanya baris ber-tag fase itu.
//
// Item 8 (UAT 2026-09-07): RAB TIDAK LAGI mengkapitalisasi Construction/Hard
// di muka saat approval (RULE KLIEN 2026-09-04 DICABUT). Setiap cost entry
// Hard selalu mendebit Persediaan langsung, sama seperti kategori lain — jadi
// saldo akun sudah = realisasi aktual, tanpa perlu redirect add-back.
// ExcludeSource tetap dipertahankan untuk mengecualikan jurnal
// "rab_capitalization" historis dari data lama (lihat accounts.go), supaya
// tenant yang sempat memakai rule lama tidak melaporkan realisasi ganda.
func (r *GORMRealisasiProvider) GetRealisasiByProject(ctx context.Context, tenantID, projectID uint64, phaseID *uint64) (map[domain.CostCategory]domain.Money, error) {
	codeToCat := map[string]domain.CostCategory{}
	var codes []string
	for _, c := range domain.AllCostCategories {
		codeToCat[c.InventoryAccountCode()] = c
		codes = append(codes, c.InventoryAccountCode())
	}
	for _, c := range domain.ExpenseCostCategories {
		codeToCat[c.ExpenseAccountCode()] = c
		codes = append(codes, c.ExpenseAccountCode())
	}

	byCode, err := ledger.NewQueryService(r.db).ActualCostByCode(ctx, tenantID, codes,
		ledger.ActualCostScope{ProjectID: projectID, PhaseID: phaseID, ExcludeSource: capitalizationJournalSource})
	if err != nil {
		return nil, fmt.Errorf("budget: GetRealisasiByProject: %w", err)
	}

	result := make(map[domain.CostCategory]domain.Money, len(byCode))
	for code, m := range byCode {
		cat := codeToCat[code]
		result[cat] = result[cat].Add(m)
	}
	return result, nil
}

type itemRealisasiRow struct {
	BudgetItemID uint64       `gorm:"column:budget_item_id"`
	Total        domain.Money `gorm:"column:total"`
}

// GetRealisasiPerItem mengembalikan Σ amount per budget_item_id dari cost_entries.
// Hanya baris yang budget_item_id-nya cocok dengan item di plan ini yang dihitung.
// Cost entry tanpa budget_item_id tidak termasuk (masuk ke GetRealisasiByProject).
func (r *GORMRealisasiProvider) GetRealisasiPerItem(ctx context.Context, tenantID, planID uint64) (map[uint64]domain.Money, error) {
	var rows []itemRealisasiRow
	// posted-only (Decision A): hanya cost entry yang jurnalnya sudah diposting.
	err := r.db.WithContext(ctx).
		Table("cost_entries ce").
		Select("ce.budget_item_id, SUM(ce.amount) as total").
		Joins("JOIN budget_items bi ON bi.id = ce.budget_item_id AND bi.tenant_id = ce.tenant_id").
		Joins("JOIN journal_entries je ON je.id = ce.journal_entry_id AND je.tenant_id = ce.tenant_id").
		Where("ce.tenant_id = ? AND bi.budget_plan_id = ? AND ce.budget_item_id IS NOT NULL AND je.posted_at IS NOT NULL",
			tenantID, planID).
		Group("ce.budget_item_id").
		Scan(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("budget: GetRealisasiPerItem: %w", err)
	}
	result := make(map[uint64]domain.Money, len(rows))
	for _, row := range rows {
		result[row.BudgetItemID] = row.Total
	}
	return result, nil
}

package budget

import (
	"context"
	"fmt"
	"time"

	"github.com/shopspring/decimal"

	"esaproperti/internal/domain"
)

// ── Interfaces ────────────────────────────────────────────────────────────────

type BudgetStore interface {
	SavePlan(ctx context.Context, plan *BudgetPlan) error
	FindPlanByID(ctx context.Context, tenantID, id uint64) (*BudgetPlan, error)
	FindActivePlan(ctx context.Context, tenantID, projectID uint64, phaseID *uint64) (*BudgetPlan, error)
	ListPlansByProject(ctx context.Context, tenantID, projectID uint64, phaseID *uint64) ([]*BudgetPlan, error)
	ApproveAndSupersede(ctx context.Context, tenantID, planID uint64, now time.Time, approvedBy string) error

	SaveItem(ctx context.Context, item *BudgetItem) error
	FindItemByID(ctx context.Context, tenantID, id uint64) (*BudgetItem, error)
	ListItemsByPlan(ctx context.Context, tenantID, planID uint64) ([]*BudgetItem, error)
	DeleteItem(ctx context.Context, tenantID, planID, itemID uint64) error
	SumItemsByCategory(ctx context.Context, tenantID, planID uint64) (map[BudgetCategory]domain.Money, error)
	SumActiveBudgetByProject(ctx context.Context, tenantID uint64) (map[uint64]domain.Money, error)
}

// RealisasiProvider mengambil realisasi biaya dari CostEntry yang sudah diposting.
// Hanya kategori yang bisa dikapitalisasi (land|hard|soft|financing) yang ada di sini.
// marketing dan other tidak ada, sehingga realisasinya selalu 0.
type RealisasiProvider interface {
	GetRealisasiByProject(ctx context.Context, tenantID, projectID uint64, phaseID *uint64) (map[domain.CostCategory]domain.Money, error)
	// GetRealisasiPerItem mengembalikan Σ amount cost entry yang tertaut ke tiap budget item
	// dalam plan tersebut. Hanya cost entry dengan budget_item_id yang cocok yang dihitung.
	// Cost entry tanpa budget_item_id tidak masuk ke sini (masuk ke GetRealisasiByProject).
	GetRealisasiPerItem(ctx context.Context, tenantID, planID uint64) (map[uint64]domain.Money, error)
}

// ── Service ───────────────────────────────────────────────────────────────────

// Service mengelola RAB (Rencana Anggaran Biaya).
// RAB TIDAK memposting jurnal apa pun ke ledger — hanya perencanaan.
// Tidak ada JournalWriter atau PostingService di sini.
type Service struct {
	store     BudgetStore
	realisasi RealisasiProvider
	// approvalGate: gate opt-in Generic Approval Workflow (Increment 5).
	approvalGate ApprovalGate
}

func NewService(store BudgetStore, r RealisasiProvider) *Service {
	return &Service{store: store, realisasi: r}
}

// ApprovalGate adalah SEAM ke Generic Approval Workflow (Increment 5,
// blueprint §10) — RAB adalah KONSUMEN PERTAMA engine. Kontrak:
// nil error = boleh lanjut (termasuk saat governance belum dikonfigurasi —
// opt-in); approval.ErrApprovalRequired = dokumen butuh request APPROVED.
type ApprovalGate interface {
	RequireApproved(ctx context.Context, tenantID, planID uint64) error
}

// SetApprovalGate memasang gate (wiring produksi). Tanpa gate = perilaku lama.
func (s *Service) SetApprovalGate(g ApprovalGate) { s.approvalGate = g }

// ── CreatePlan ────────────────────────────────────────────────────────────────

func (s *Service) CreatePlan(ctx context.Context, tenantID uint64, req CreatePlanRequest) (*BudgetPlan, error) {
	if req.ProjectID == 0 {
		return nil, ErrProjectRequired
	}
	if req.Label == "" {
		return nil, ErrLabelRequired
	}

	// Hitung versi berikutnya di dalam (project, phase) ini.
	existing, err := s.store.ListPlansByProject(ctx, tenantID, req.ProjectID, req.PhaseID)
	if err != nil {
		return nil, fmt.Errorf("ListPlansByProject: %w", err)
	}
	nextVersion := 1
	for _, p := range existing {
		if p.Version >= nextVersion {
			nextVersion = p.Version + 1
		}
	}

	plan := &BudgetPlan{
		TenantID:  tenantID,
		ProjectID: req.ProjectID,
		PhaseID:   req.PhaseID,
		Version:   nextVersion,
		Label:     req.Label,
		Notes:     req.Notes,
		Status:    BudgetPlanStatusDraft,
	}
	if err := s.store.SavePlan(ctx, plan); err != nil {
		return nil, fmt.Errorf("SavePlan: %w", err)
	}
	return plan, nil
}

// ── AddItem ───────────────────────────────────────────────────────────────────

func (s *Service) AddItem(ctx context.Context, tenantID uint64, req AddItemRequest) (*BudgetItem, error) {
	if !req.Category.Valid() {
		return nil, ErrInvalidCategory
	}
	if req.BudgetedAmount.IsZero() || req.BudgetedAmount.IsNeg() {
		return nil, ErrAmountZeroOrNeg
	}
	if !req.BudgetedAmount.IsWholeRupiah() {
		return nil, ErrAmountFractional
	}

	plan, err := s.store.FindPlanByID(ctx, tenantID, req.PlanID)
	if err != nil {
		return nil, err
	}
	if plan.Status != BudgetPlanStatusDraft {
		return nil, ErrPlanNotDraft
	}

	item := &BudgetItem{
		TenantID:       tenantID,
		BudgetPlanID:   req.PlanID,
		Category:       req.Category,
		Subcategory:    req.Subcategory,
		Description:    req.Description,
		BudgetedAmount: req.BudgetedAmount,
	}
	if err := s.store.SaveItem(ctx, item); err != nil {
		return nil, fmt.Errorf("SaveItem: %w", err)
	}
	return item, nil
}

// ── DeleteItem ────────────────────────────────────────────────────────────────

func (s *Service) DeleteItem(ctx context.Context, tenantID, planID, itemID uint64) error {
	plan, err := s.store.FindPlanByID(ctx, tenantID, planID)
	if err != nil {
		return err
	}
	if plan.Status != BudgetPlanStatusDraft {
		return ErrPlanNotDraft
	}
	return s.store.DeleteItem(ctx, tenantID, planID, itemID)
}

// ── ApprovePlan ───────────────────────────────────────────────────────────────

// ApprovePlan mengubah plan dari draft → active dan secara atomik men-supersede
// plan active sebelumnya untuk (project, phase) yang sama.
// Ini menegakkan INVARIANT #8: hanya satu BudgetPlan 'active' per (project, phase).
// RAB TIDAK memposting jurnal apa pun ke ledger.
func (s *Service) ApprovePlan(ctx context.Context, tenantID uint64, req ApprovePlanRequest) (*BudgetPlan, error) {
	plan, err := s.store.FindPlanByID(ctx, tenantID, req.PlanID)
	if err != nil {
		return nil, err
	}
	if plan.Status != BudgetPlanStatusDraft {
		return nil, ErrPlanNotApprovable
	}

	items, err := s.store.ListItemsByPlan(ctx, tenantID, req.PlanID)
	if err != nil {
		return nil, fmt.Errorf("ListItemsByPlan: %w", err)
	}
	if len(items) == 0 {
		return nil, ErrNoItemsToApprove
	}

	// Gate Generic Approval Workflow (Increment 5, opt-in): bila tenant
	// mengonfigurasi workflow aktif utk target 'rab', plan WAJIB punya
	// Approval Request APPROVED sebelum diaktifkan. Tanpa konfigurasi →
	// perilaku lama (langsung approve).
	if s.approvalGate != nil {
		if err := s.approvalGate.RequireApproved(ctx, tenantID, req.PlanID); err != nil {
			return nil, err
		}
	}

	now := time.Now()
	if err := s.store.ApproveAndSupersede(ctx, tenantID, req.PlanID, now, req.ApprovedBy); err != nil {
		return nil, fmt.Errorf("ApproveAndSupersede: %w", err)
	}

	return s.store.FindPlanByID(ctx, tenantID, req.PlanID)
}

// ── Read-only ─────────────────────────────────────────────────────────────────

func (s *Service) GetPlan(ctx context.Context, tenantID, planID uint64) (*BudgetPlan, error) {
	return s.store.FindPlanByID(ctx, tenantID, planID)
}

func (s *Service) GetActivePlan(ctx context.Context, tenantID, projectID uint64, phaseID *uint64) (*BudgetPlan, error) {
	p, err := s.store.FindActivePlan(ctx, tenantID, projectID, phaseID)
	if err != nil {
		return nil, ErrNoActivePlan
	}
	return p, nil
}

// GetBudgetedHPPPool mengembalikan total RAB TERANGGARKAN per kategori HPP
// (land/hard/soft/financing) dari plan RAB yang AKTIF & disetujui untuk
// (project, phase). Ini adalah "pool" biaya teranggarkan yang akan dialokasikan
// ke unit sebagai HPP (metode Budgeted Cost Allocation — lihat
// docs/budgeted-cost-allocation-spec.md). Kategori BEBAN (marketing/other)
// DIKECUALIKAN — bukan bagian HPP. Mengembalikan ErrNoActivePlan bila belum ada
// RAB aktif (gate BCA-2). Fondasi untuk pengakuan HPP di BAST (P0-2).
func (s *Service) GetBudgetedHPPPool(ctx context.Context, tenantID, projectID uint64, phaseID *uint64) (domain.UnitCostBreakdown, error) {
	basis, err := s.GetBudgetedHPPBasis(ctx, tenantID, projectID, phaseID)
	if err != nil {
		return domain.UnitCostBreakdown{}, err
	}
	return basis.Pool, nil
}

// BudgetedHPPBasis membekukan versi RAB (PlanID + Version) beserta pool biaya
// kapitalisasi yang akan dialokasikan menjadi HPP. Dipakai saat BAST agar HPP
// budgeted reproducible: snapshot mencatat versi RAB persis yang dipakai
// (freeze §4 / requirement "RAB version snapshot saat BAST, tidak mutable").
type BudgetedHPPBasis struct {
	PlanID  uint64
	Version int
	Pool    domain.UnitCostBreakdown
}

// GetBudgetedHPPBasis mengembalikan RAB aktif (id+versi) dan pool biaya
// kapitalisasi (land/hard/soft/financing) — kategori BEBAN (marketing/other)
// DIKECUALIKAN. Mengembalikan ErrNoActivePlan bila belum ada RAB aktif (gate
// BCA-2). Kategori dipetakan lewat taxonomy (ToCostCategory), tidak di-hardcode.
func (s *Service) GetBudgetedHPPBasis(ctx context.Context, tenantID, projectID uint64, phaseID *uint64) (BudgetedHPPBasis, error) {
	plan, err := s.store.FindActivePlan(ctx, tenantID, projectID, phaseID)
	if err != nil {
		return BudgetedHPPBasis{}, ErrNoActivePlan
	}
	byCat, err := s.store.SumItemsByCategory(ctx, tenantID, plan.ID)
	if err != nil {
		return BudgetedHPPBasis{}, fmt.Errorf("SumItemsByCategory: %w", err)
	}
	var pool domain.UnitCostBreakdown
	for cat, amt := range byCat {
		cc, ok := cat.ToCostCategory() // hanya kategori kapitalisasi; marketing/other → skip
		if !ok {
			continue
		}
		switch cc {
		case domain.CostCategoryLand:
			pool.Land = pool.Land.Add(amt)
		case domain.CostCategoryHard:
			pool.Hard = pool.Hard.Add(amt)
		case domain.CostCategorySoft:
			pool.Soft = pool.Soft.Add(amt)
		case domain.CostCategoryFinancing:
			pool.Financing = pool.Financing.Add(amt)
		}
	}
	return BudgetedHPPBasis{PlanID: plan.ID, Version: plan.Version, Pool: pool}, nil
}

// TotalActiveBudgetByProject (S5): pembaca KANONIK total RAB aktif per proyek
// (registry #12) — dikonsumsi dashboard, menggantikan subquery SQL duplikat.
func (s *Service) TotalActiveBudgetByProject(ctx context.Context, tenantID uint64) (map[uint64]domain.Money, error) {
	return s.store.SumActiveBudgetByProject(ctx, tenantID)
}

func (s *Service) ListPlans(ctx context.Context, tenantID, projectID uint64, phaseID *uint64) ([]*BudgetPlan, error) {
	return s.store.ListPlansByProject(ctx, tenantID, projectID, phaseID)
}

func (s *Service) ListItems(ctx context.Context, tenantID, planID uint64) ([]*BudgetItem, error) {
	return s.store.ListItemsByPlan(ctx, tenantID, planID)
}

// ── GetRABvsRealisasi ─────────────────────────────────────────────────────────

// GetRABvsRealisasi menghasilkan laporan perbandingan RAB vs realisasi untuk
// plan active di (project, phase). Realisasi diambil dari CostEntry yang sudah
// diposting ke ledger. construction ↔ hard adalah alias.
// marketing dan other terealisasi lewat CostEntry tier overhead (Increment 2) —
// bukan lagi selalu 0.
func (s *Service) GetRABvsRealisasi(ctx context.Context, tenantID, projectID uint64, phaseID *uint64) (*RABvsRealisasiReport, error) {
	plan, err := s.store.FindActivePlan(ctx, tenantID, projectID, phaseID)
	if err != nil {
		return nil, ErrNoActivePlan
	}

	budgetByCategory, err := s.store.SumItemsByCategory(ctx, tenantID, plan.ID)
	if err != nil {
		return nil, fmt.Errorf("SumItemsByCategory: %w", err)
	}

	realisasiByCC, err := s.realisasi.GetRealisasiByProject(ctx, tenantID, projectID, phaseID)
	if err != nil {
		return nil, fmt.Errorf("GetRealisasiByProject: %w", err)
	}

	var (
		totalBudgeted  domain.Money
		totalRealisasi domain.Money
	)

	rows := make([]RABvsRealisasiRow, 0, len(AllBudgetCategories))
	for _, cat := range AllBudgetCategories {
		budgeted := budgetByCategory[cat] // zero value if not in map

		var realisasi domain.Money
		if cc, ok := cat.CostEntryCategory(); ok {
			realisasi = realisasiByCC[cc] // zero value if not in map
		}
		// marketing/other memetakan ke kategori expense (tier overhead) — Increment 2

		selisih := budgeted.Sub(realisasi)

		var persen string
		if budgeted.IsZero() {
			persen = "N/A"
		} else {
			p := realisasi.Decimal().Div(budgeted.Decimal()).Mul(decimal.NewFromInt(100))
			f64, _ := p.Float64()
			persen = fmt.Sprintf("%.2f%%", f64)
		}

		rows = append(rows, RABvsRealisasiRow{
			Category:        cat,
			Budgeted:        budgeted.String(),
			Realisasi:       realisasi.String(),
			Selisih:         selisih.String(),
			PersenRealisasi: persen,
		})

		totalBudgeted = totalBudgeted.Add(budgeted)
		totalRealisasi = totalRealisasi.Add(realisasi)
	}

	totalSelisih := totalBudgeted.Sub(totalRealisasi)

	// S9/R-9: usage % + status kesehatan anggaran dihitung backend (decimal).
	usagePct := decimal.Zero
	if !totalBudgeted.IsZero() {
		usagePct = totalRealisasi.Decimal().
			Div(totalBudgeted.Decimal()).Mul(decimal.NewFromInt(100)).Round(2)
	}

	return &RABvsRealisasiReport{
		PlanID:         plan.ID,
		PlanLabel:      plan.Label,
		PlanVersion:    plan.Version,
		ProjectID:      plan.ProjectID,
		PhaseID:        plan.PhaseID,
		Rows:           rows,
		TotalBudgeted:  totalBudgeted.String(),
		TotalRealisasi: totalRealisasi.String(),
		TotalSelisih:   totalSelisih.String(),
		UsagePct:       usagePct.String(),
		Status:         BudgetHealthStatus(usagePct),
	}, nil
}

// GetItemsRealisasi menghasilkan realisasi per-budget-item untuk plan active.
// Granularitas lebih halus dari GetRABvsRealisasi (per-item bukan per-kategori).
// Hanya cost entry yang tertaut ke budget_item_id yang dihitung di sini.
func (s *Service) GetItemsRealisasi(ctx context.Context, tenantID, projectID uint64, phaseID *uint64) ([]*ItemRealisasiRow, error) {
	plan, err := s.store.FindActivePlan(ctx, tenantID, projectID, phaseID)
	if err != nil {
		return nil, ErrNoActivePlan
	}

	items, err := s.store.ListItemsByPlan(ctx, tenantID, plan.ID)
	if err != nil {
		return nil, fmt.Errorf("GetItemsRealisasi: ListItemsByPlan: %w", err)
	}

	realisasiPerItem, err := s.realisasi.GetRealisasiPerItem(ctx, tenantID, plan.ID)
	if err != nil {
		return nil, fmt.Errorf("GetItemsRealisasi: GetRealisasiPerItem: %w", err)
	}

	rows := make([]*ItemRealisasiRow, 0, len(items))
	for _, item := range items {
		r := realisasiPerItem[item.ID] // domain.Money zero value jika tidak ada cost entry tertaut
		selisih := item.BudgetedAmount.Sub(r)

		var persen string
		if item.BudgetedAmount.IsZero() {
			persen = "N/A"
		} else {
			p := r.Decimal().Div(item.BudgetedAmount.Decimal()).Mul(decimal.NewFromInt(100))
			f64, _ := p.Float64()
			persen = fmt.Sprintf("%.2f%%", f64)
		}

		rows = append(rows, &ItemRealisasiRow{
			ItemID:          item.ID,
			Category:        item.Category,
			Subcategory:     item.Subcategory,
			Description:     item.Description,
			Budgeted:        item.BudgetedAmount.String(),
			Realisasi:       r.String(),
			Selisih:         selisih.String(),
			PersenRealisasi: persen,
		})
	}
	return rows, nil
}

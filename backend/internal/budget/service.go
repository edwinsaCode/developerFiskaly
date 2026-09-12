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

// RealisasiProvider mengambil realisasi biaya dari CostEntry yang sudah diposting,
// untuk SEMUA kategori (kapitalisasi land|hard via akun Persediaan, dan beban
// operational|marketing|other|soft via akun beban masing-masing) — lihat
// GORMRealisasiProvider.GetRealisasiByProject.
type RealisasiProvider interface {
	GetRealisasiByProject(ctx context.Context, tenantID, projectID uint64, phaseID *uint64) (map[domain.CostCategory]domain.Money, error)
	// GetRealisasiPerItem mengembalikan Σ amount cost entry yang tertaut ke tiap budget item
	// dalam plan tersebut. Hanya cost entry dengan budget_item_id yang cocok yang dihitung.
	// Cost entry tanpa budget_item_id tidak masuk ke sini (masuk ke GetRealisasiByProject).
	GetRealisasiPerItem(ctx context.Context, tenantID, planID uint64) (map[uint64]domain.Money, error)

	// GetRealisasiByProjectCash dan GetRealisasiPerItemCash adalah versi KAS dari
	// dua method di atas: cost entry dari tagihan vendor (AP invoice) diprorata
	// sebesar fraksi yang SUDAH DIBAYAR, bukan nilai penuh yang diakui saat
	// tagihan diposting (akrual). Dipakai KHUSUS laporan RAB vs Realisasi
	// (GetRABvsRealisasi, GetItemsRealisasi, GetConstructionRealisasiTree) —
	// HPP/closing/alokasi/dashboard tetap pakai versi akrual di atas, tidak
	// berubah. Lihat GORMRealisasiProvider.apPaidFractionSubquery.
	GetRealisasiByProjectCash(ctx context.Context, tenantID, projectID uint64, phaseID *uint64) (map[domain.CostCategory]domain.Money, error)
	GetRealisasiPerItemCash(ctx context.Context, tenantID, planID uint64) (map[uint64]domain.Money, error)
}

// ── Service ───────────────────────────────────────────────────────────────────

// Service mengelola RAB (Rencana Anggaran Biaya).
//
// RAB TIDAK memposting jurnal sama sekali — murni budget/planning (Item 8,
// UAT 2026-09-07). RULE KLIEN 2026-09-04 (kapitalisasi Construction penuh
// saat approval) DICABUT klien: Persediaan/HPP kini murni biaya aktual dari
// cost entry, bukan estimasi RAB. Lihat cost.Service untuk sisi actual-cost.
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
	// UAT 2026-09-07: RAB Konstruksi WAJIB menyatakan salah satu dari 4
	// subkategori kanonik (Produksi Subsidi / Produksi Komersial / Sarana &
	// Prasarana / Perizinan) — "Produksi" tunggal tidak cukup untuk menentukan
	// unit mana yang berhak menerima HPP-nya (lihat allocation.HardPoolSource).
	if req.Category == BudgetCategoryConstruction {
		if !domain.ConstructionSubcategory(req.Subcategory).Valid() {
			return nil, ErrInvalidConstructionSubcategory
		}
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
//
// RULE KLIEN 2026-09-04: saat plan diaktifkan, TOTAL budgeted_amount kategori
// Construction (Produksi + Sarana & Prasarana + Perizinan, alias domain
// CostCategoryHard) langsung dikapitalisasi ke Persediaan — Dr 1-3100 / Cr Hutang
// Usaha 2-1000 — TANPA menunggu vendor payment. Vendor diidentifikasi belakangan
// saat realisasi; realisasi itu HANYA menyelesaikan Hutang Usaha (lihat
// cost.Service.plan(), yang mengalihkan debit dari Persediaan ke Hutang Usaha
// begitu IsProjectHardCapitalized/alreadyCapitalized bernilai true), TIDAK
// PERNAH mendebit Persediaan lagi (no double-capitalize).
//
// Land SENGAJA tidak ikut jalur ini: Land punya modul kapitalisasi sendiri
// (internal/land, sudah shipped) dan klien hanya menyebut kategori Construction
// dalam laporan bug ini. Soft juga tidak pernah ikut jalur ini — RULE KLIEN
// FREEZE (2026-09-04): Soft Cost bukan lagi kategori kapitalisasi sama sekali
// (lihat domain.CostCategorySoft), jadi tidak ada logika untuk diperluas di sini.
//
// Untuk plan yang MEN-SUPERSEDE plan aktif sebelumnya (revisi RAB), hanya
// SELISIH (delta) terhadap CapitalizedHardAmount plan sebelumnya yang diposting
// — delta positif menambah Persediaan, delta negatif membalikkannya sebagian
// (Invariant #5: koreksi lewat jurnal, bukan edit histori).
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
// (land/hard) dari plan RAB yang AKTIF & disetujui untuk (project, phase).
// Ini adalah "pool" biaya teranggarkan yang akan dialokasikan ke unit sebagai
// HPP (metode Budgeted Cost Allocation — lihat docs/budgeted-cost-allocation-spec.md).
// Kategori BEBAN (operational/marketing/other/soft) DIKECUALIKAN — bukan bagian
// HPP (RULE KLIEN FREEZE 2026-09-04). Mengembalikan ErrNoActivePlan bila belum
// ada RAB aktif (gate BCA-2). Fondasi untuk pengakuan HPP di BAST (P0-2).
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
// kapitalisasi (land/hard) — kategori BEBAN (operational/marketing/other/soft,
// RULE KLIEN FREEZE 2026-09-04: HPP hanya Tanah + Konstruksi/Hard Cost)
// DIKECUALIKAN. Mengembalikan ErrNoActivePlan bila belum ada RAB aktif (gate
// BCA-2). Kategori dipetakan lewat taxonomy (ToCostCategory), tidak di-hardcode
// — pool.Soft SENGAJA tidak pernah diisi di sini (field itu legacy-only, lihat
// domain.UnitCostBreakdown.Soft).
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
		cc, ok := cat.ToCostCategory() // hanya kategori kapitalisasi; operational/marketing/other/soft → skip
		if !ok {
			continue
		}
		switch cc {
		case domain.CostCategoryLand:
			pool.Land = pool.Land.Add(amt)
		case domain.CostCategoryHard:
			pool.Hard = pool.Hard.Add(amt)
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

	realisasiByCC, err := s.realisasi.GetRealisasiByProjectCash(ctx, tenantID, projectID, phaseID)
	if err != nil {
		return nil, fmt.Errorf("GetRealisasiByProjectCash: %w", err)
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
			// Fraksi mentah (bukan sudah dikali 100 / ber-suffix "%") — kontrak
			// komponen FE <Persen> yang mengonsumsi field ini mengalikan 100
			// sendiri. Lihat RABvsRealisasiSection.tsx.
			persen = realisasi.Decimal().Div(budgeted.Decimal()).String()
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

	realisasiPerItem, err := s.realisasi.GetRealisasiPerItemCash(ctx, tenantID, plan.ID)
	if err != nil {
		return nil, fmt.Errorf("GetItemsRealisasi: GetRealisasiPerItemCash: %w", err)
	}

	rows := make([]*ItemRealisasiRow, 0, len(items))
	for _, item := range items {
		r := realisasiPerItem[item.ID] // domain.Money zero value jika tidak ada cost entry tertaut
		selisih := item.BudgetedAmount.Sub(r)

		rows = append(rows, &ItemRealisasiRow{
			ItemID:          item.ID,
			Category:        item.Category,
			Subcategory:     item.Subcategory,
			Description:     item.Description,
			Budgeted:        item.BudgetedAmount.String(),
			Realisasi:       r.String(),
			Selisih:         selisih.String(),
			PersenRealisasi: percentString(r, item.BudgetedAmount),
		})
	}
	return rows, nil
}

// percentString menghitung realisasi/budget × 100, format "12.34%" — "N/A" bila
// budget nol. SATU-SATUNYA tempat rumus ini ditulis untuk laporan RAB vs
// Realisasi (item, subkategori, maupun kategori Konstruksi) — supaya
// pembulatan/formatnya tidak pernah berbeda antar level.
func percentString(realisasi, budgeted domain.Money) string {
	if budgeted.IsZero() {
		return "N/A"
	}
	p := realisasi.Decimal().Div(budgeted.Decimal()).Mul(decimal.NewFromInt(100))
	f64, _ := p.Float64()
	return fmt.Sprintf("%.2f%%", f64)
}

// ── GetConstructionRealisasiTree ──────────────────────────────────────────────

// GetConstructionRealisasiTree menghasilkan RAB vs Realisasi Konstruksi dalam
// HIERARKI Subkategori → Item RAB, untuk plan active di (project, phase).
//
// Tidak ada nama item yang di-hardcode: pengelompokan hanya memakai 4
// subkategori kanonik (domain.ConstructionSubcategory, SUDAH WAJIB diisi saat
// item dibuat — lihat AddItem), item di dalamnya persis apa yang dibuat user
// di RAB. Subkategori tanpa item disembunyikan (bukan baris kosong).
//
// Total subkategori/Konstruksi = SUM item anak (budgeted & realisasi masing-
// masing dijumlah dulu), lalu persentase dihitung dari total itu — BUKAN
// rata-rata persentase anak (step 11-12 permintaan klien).
func (s *Service) GetConstructionRealisasiTree(ctx context.Context, tenantID, projectID uint64, phaseID *uint64) (*ConstructionRealisasiTree, error) {
	plan, err := s.store.FindActivePlan(ctx, tenantID, projectID, phaseID)
	if err != nil {
		return nil, ErrNoActivePlan
	}

	items, err := s.store.ListItemsByPlan(ctx, tenantID, plan.ID)
	if err != nil {
		return nil, fmt.Errorf("GetConstructionRealisasiTree: ListItemsByPlan: %w", err)
	}

	realisasiPerItem, err := s.realisasi.GetRealisasiPerItemCash(ctx, tenantID, plan.ID)
	if err != nil {
		return nil, fmt.Errorf("GetConstructionRealisasiTree: GetRealisasiPerItemCash: %w", err)
	}

	bySub := make(map[domain.ConstructionSubcategory][]*ItemRealisasiRow, len(domain.AllConstructionSubcategories))
	for _, item := range items {
		if item.Category != BudgetCategoryConstruction {
			continue
		}
		sub := domain.ConstructionSubcategory(item.Subcategory)
		r := realisasiPerItem[item.ID]
		bySub[sub] = append(bySub[sub], &ItemRealisasiRow{
			ItemID:          item.ID,
			Category:        item.Category,
			Subcategory:     item.Subcategory,
			Description:     item.Description,
			Budgeted:        item.BudgetedAmount.String(),
			Realisasi:       r.String(),
			Selisih:         item.BudgetedAmount.Sub(r).String(),
			PersenRealisasi: percentString(r, item.BudgetedAmount),
		})
	}

	var (
		groups         []*SubcategoryRealisasiGroup
		totalBudgeted  domain.Money
		totalRealisasi domain.Money
	)
	for _, sub := range domain.AllConstructionSubcategories {
		rows, ok := bySub[sub]
		if !ok {
			continue // subkategori yang belum dipakai di RAB ini — tidak ditampilkan
		}
		var gBudgeted, gRealisasi domain.Money
		for _, row := range rows {
			b, _ := domain.NewMoney(row.Budgeted)
			r, _ := domain.NewMoney(row.Realisasi)
			gBudgeted = gBudgeted.Add(b)
			gRealisasi = gRealisasi.Add(r)
		}
		groups = append(groups, &SubcategoryRealisasiGroup{
			Subcategory:     sub,
			Label:           sub.Label(),
			Items:           rows,
			Budgeted:        gBudgeted.String(),
			Realisasi:       gRealisasi.String(),
			Selisih:         gBudgeted.Sub(gRealisasi).String(),
			PersenRealisasi: percentString(gRealisasi, gBudgeted),
		})
		totalBudgeted = totalBudgeted.Add(gBudgeted)
		totalRealisasi = totalRealisasi.Add(gRealisasi)
	}

	return &ConstructionRealisasiTree{
		PlanID:          plan.ID,
		ProjectID:       plan.ProjectID,
		PhaseID:         plan.PhaseID,
		Groups:          groups,
		Budgeted:        totalBudgeted.String(),
		Realisasi:       totalRealisasi.String(),
		Selisih:         totalBudgeted.Sub(totalRealisasi).String(),
		PersenRealisasi: percentString(totalRealisasi, totalBudgeted),
	}, nil
}

package budget_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"esaproperti/internal/budget"
	"esaproperti/internal/domain"
)

// ── helpers ───────────────────────────────────────────────────────────────────

func rupiah(n int64) domain.Money { return domain.FromInt(n) }
func ptr[T any](v T) *T           { return &v }

// ── mockBudgetStore ───────────────────────────────────────────────────────────
// In-memory store yang menegakkan invariant #8 (satu active per project/phase).

type mockBudgetStore struct {
	mu         sync.Mutex
	plans      map[uint64]*budget.BudgetPlan
	items      map[uint64]*budget.BudgetItem // key=itemID
	planItems  map[uint64][]uint64           // planID → []itemID
	nextPlanID uint64
	nextItemID uint64
}

func newMockStore() *mockBudgetStore {
	return &mockBudgetStore{
		plans:     make(map[uint64]*budget.BudgetPlan),
		items:     make(map[uint64]*budget.BudgetItem),
		planItems: make(map[uint64][]uint64),
	}
}

func samePhase(a, b *uint64) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return *a == *b
}

func (m *mockBudgetStore) SavePlan(_ context.Context, plan *budget.BudgetPlan) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.nextPlanID++
	plan.ID = m.nextPlanID
	plan.CreatedAt = time.Now()
	plan.UpdatedAt = time.Now()
	cp := *plan
	m.plans[plan.ID] = &cp
	return nil
}

func (m *mockBudgetStore) FindPlanByID(_ context.Context, tenantID, id uint64) (*budget.BudgetPlan, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.plans[id]
	if !ok || p.TenantID != tenantID {
		return nil, budget.ErrPlanNotFound
	}
	cp := *p
	return &cp, nil
}

func (m *mockBudgetStore) FindActivePlan(_ context.Context, tenantID, projectID uint64, phaseID *uint64) (*budget.BudgetPlan, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, p := range m.plans {
		if p.TenantID == tenantID &&
			p.ProjectID == projectID &&
			p.Status == budget.BudgetPlanStatusActive &&
			samePhase(p.PhaseID, phaseID) {
			cp := *p
			return &cp, nil
		}
	}
	return nil, budget.ErrPlanNotFound
}

func (m *mockBudgetStore) ListPlansByProject(_ context.Context, tenantID, projectID uint64, phaseID *uint64) ([]*budget.BudgetPlan, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var result []*budget.BudgetPlan
	for _, p := range m.plans {
		if p.TenantID != tenantID || p.ProjectID != projectID {
			continue
		}
		// phaseID=nil → return ALL regardless of phase
		// phaseID!=nil → filter by that specific phase
		if phaseID != nil && !samePhase(p.PhaseID, phaseID) {
			continue
		}
		cp := *p
		result = append(result, &cp)
	}
	return result, nil
}

// ApproveAndSupersede implementasi in-memory: atomik supersede old active → activate new.
// Inilah yang menegakkan Invariant #8.
func (m *mockBudgetStore) ApproveAndSupersede(_ context.Context, tenantID, planID uint64, now time.Time, approvedBy string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	target, ok := m.plans[planID]
	if !ok || target.TenantID != tenantID {
		return budget.ErrPlanNotFound
	}

	// Supersede existing active plan for same (project, phase)
	for _, p := range m.plans {
		if p.TenantID == tenantID &&
			p.ProjectID == target.ProjectID &&
			p.Status == budget.BudgetPlanStatusActive &&
			samePhase(p.PhaseID, target.PhaseID) {
			p.Status = budget.BudgetPlanStatusSuperseded
			p.UpdatedAt = now
		}
	}

	// Activate the target plan
	target.Status = budget.BudgetPlanStatusActive
	target.ApprovedAt = &now
	s := approvedBy
	target.ApprovedBy = &s
	target.UpdatedAt = now
	return nil
}

func (m *mockBudgetStore) SaveItem(_ context.Context, item *budget.BudgetItem) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.nextItemID++
	item.ID = m.nextItemID
	item.CreatedAt = time.Now()
	item.UpdatedAt = time.Now()
	cp := *item
	m.items[item.ID] = &cp
	m.planItems[item.BudgetPlanID] = append(m.planItems[item.BudgetPlanID], item.ID)
	return nil
}

func (m *mockBudgetStore) FindItemByID(_ context.Context, tenantID, id uint64) (*budget.BudgetItem, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	it, ok := m.items[id]
	if !ok || it.TenantID != tenantID {
		return nil, budget.ErrItemNotFound
	}
	cp := *it
	return &cp, nil
}

func (m *mockBudgetStore) ListItemsByPlan(_ context.Context, tenantID, planID uint64) ([]*budget.BudgetItem, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var result []*budget.BudgetItem
	for _, id := range m.planItems[planID] {
		it := m.items[id]
		if it != nil && it.TenantID == tenantID {
			cp := *it
			result = append(result, &cp)
		}
	}
	return result, nil
}

func (m *mockBudgetStore) DeleteItem(_ context.Context, tenantID, planID, itemID uint64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	it, ok := m.items[itemID]
	if !ok || it.TenantID != tenantID || it.BudgetPlanID != planID {
		return budget.ErrItemNotFound
	}
	delete(m.items, itemID)
	ids := m.planItems[planID]
	filtered := ids[:0]
	for _, id := range ids {
		if id != itemID {
			filtered = append(filtered, id)
		}
	}
	m.planItems[planID] = filtered
	return nil
}

func (m *mockBudgetStore) SumItemsByCategory(_ context.Context, tenantID, planID uint64) (map[budget.BudgetCategory]domain.Money, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make(map[budget.BudgetCategory]domain.Money)
	for _, id := range m.planItems[planID] {
		it := m.items[id]
		if it != nil && it.TenantID == tenantID {
			result[it.Category] = result[it.Category].Add(it.BudgetedAmount)
		}
	}
	return result, nil
}

func (m *mockBudgetStore) SumActiveBudgetByProject(_ context.Context, tenantID uint64) (map[uint64]domain.Money, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make(map[uint64]domain.Money)
	for _, p := range m.plans {
		if p.TenantID != tenantID || p.Status != budget.BudgetPlanStatusActive {
			continue
		}
		for _, id := range m.planItems[p.ID] {
			if it := m.items[id]; it != nil && it.TenantID == tenantID {
				result[p.ProjectID] = result[p.ProjectID].Add(it.BudgetedAmount)
			}
		}
	}
	return result, nil
}

// countActivePerProjectPhase adalah helper test untuk membuktikan Invariant #8.
func (m *mockBudgetStore) countActivePerProjectPhase(tenantID, projectID uint64, phaseID *uint64) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	count := 0
	for _, p := range m.plans {
		if p.TenantID == tenantID &&
			p.ProjectID == projectID &&
			p.Status == budget.BudgetPlanStatusActive &&
			samePhase(p.PhaseID, phaseID) {
			count++
		}
	}
	return count
}

// ── mockRealisasiProvider ─────────────────────────────────────────────────────

type mockRealisasiProvider struct {
	data map[domain.CostCategory]domain.Money
}

func (m *mockRealisasiProvider) GetRealisasiByProject(_ context.Context, _, _ uint64, _ *uint64) (map[domain.CostCategory]domain.Money, error) {
	if m.data == nil {
		return make(map[domain.CostCategory]domain.Money), nil
	}
	return m.data, nil
}

// GetRealisasiPerItem: tes per-item ada di budget_link_test.go menggunakan mockRealisasiProviderFull.
// Sini kembalikan map kosong agar interface terpenuhi.
func (m *mockRealisasiProvider) GetRealisasiPerItem(_ context.Context, _, _ uint64) (map[uint64]domain.Money, error) {
	return make(map[uint64]domain.Money), nil
}

func (m *mockRealisasiProvider) GetRealisasiByProjectCash(ctx context.Context, tenantID, projectID uint64, phaseID *uint64) (map[domain.CostCategory]domain.Money, error) {
	return m.GetRealisasiByProject(ctx, tenantID, projectID, phaseID)
}

func (m *mockRealisasiProvider) GetRealisasiPerItemCash(ctx context.Context, tenantID, planID uint64) (map[uint64]domain.Money, error) {
	return m.GetRealisasiPerItem(ctx, tenantID, planID)
}

// ── builder ───────────────────────────────────────────────────────────────────

func buildService(store *mockBudgetStore, r *mockRealisasiProvider) *budget.Service {
	if r == nil {
		r = &mockRealisasiProvider{}
	}
	return budget.NewService(store, r)
}

// helper: buat draft plan dengan satu item, return planID
func mustCreateDraftWithItem(t *testing.T, svc *budget.Service, tenantID, projectID uint64, phaseID *uint64, amount int64) uint64 {
	t.Helper()
	plan, err := svc.CreatePlan(context.Background(), tenantID, budget.CreatePlanRequest{
		ProjectID: projectID, PhaseID: phaseID, Label: "RAB Test",
	})
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	_, err = svc.AddItem(context.Background(), tenantID, budget.AddItemRequest{
		PlanID:         plan.ID,
		Category:       budget.BudgetCategoryConstruction,
		Subcategory:    string(domain.ConstructionSaranaPrasarana),
		BudgetedAmount: rupiah(amount),
	})
	if err != nil {
		t.Fatalf("AddItem: %v", err)
	}
	return plan.ID
}

// ── Tests: CreatePlan ─────────────────────────────────────────────────────────

func TestBudget_CreateDraft_OK(t *testing.T) {
	store := newMockStore()
	svc := buildService(store, nil)

	plan, err := svc.CreatePlan(context.Background(), 1, budget.CreatePlanRequest{
		ProjectID: 10, Label: "RAB v1", Notes: "Initial",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if plan.Status != budget.BudgetPlanStatusDraft {
		t.Errorf("status=%s, want draft", plan.Status)
	}
	if plan.Version != 1 {
		t.Errorf("version=%d, want 1", plan.Version)
	}
	if plan.ID == 0 {
		t.Error("plan.ID == 0 — SavePlan did not set ID")
	}
}

func TestBudget_CreateDraft_MissingProjectID_Error(t *testing.T) {
	svc := buildService(newMockStore(), nil)
	_, err := svc.CreatePlan(context.Background(), 1, budget.CreatePlanRequest{Label: "X"})
	if !errors.Is(err, budget.ErrProjectRequired) {
		t.Errorf("expected ErrProjectRequired, got %v", err)
	}
}

func TestBudget_CreateDraft_MissingLabel_Error(t *testing.T) {
	svc := buildService(newMockStore(), nil)
	_, err := svc.CreatePlan(context.Background(), 1, budget.CreatePlanRequest{ProjectID: 10})
	if !errors.Is(err, budget.ErrLabelRequired) {
		t.Errorf("expected ErrLabelRequired, got %v", err)
	}
}

func TestBudget_CreateDraft_VersionIncrementsPerProjectPhase(t *testing.T) {
	store := newMockStore()
	svc := buildService(store, nil)

	// Plan 1
	p1, _ := svc.CreatePlan(context.Background(), 1, budget.CreatePlanRequest{ProjectID: 10, Label: "RAB v1"})
	// Plan 2 (same project, same phase nil)
	p2, _ := svc.CreatePlan(context.Background(), 1, budget.CreatePlanRequest{ProjectID: 10, Label: "RAB v2"})
	// Plan 3 (different project)
	p3, _ := svc.CreatePlan(context.Background(), 1, budget.CreatePlanRequest{ProjectID: 20, Label: "RAB v1"})

	if p1.Version != 1 {
		t.Errorf("p1.Version=%d, want 1", p1.Version)
	}
	if p2.Version != 2 {
		t.Errorf("p2.Version=%d, want 2 (increments per project)", p2.Version)
	}
	if p3.Version != 1 {
		t.Errorf("p3.Version=%d, want 1 (different project, resets)", p3.Version)
	}
}

// ── Tests: AddItem (Invariant #2 — no float, whole rupiah) ────────────────────

func TestBudget_AddItem_WholeRupiah_OK(t *testing.T) {
	store := newMockStore()
	svc := buildService(store, nil)
	plan, _ := svc.CreatePlan(context.Background(), 1, budget.CreatePlanRequest{ProjectID: 10, Label: "RAB"})

	item, err := svc.AddItem(context.Background(), 1, budget.AddItemRequest{
		PlanID:         plan.ID,
		Category:       budget.BudgetCategoryLand,
		BudgetedAmount: rupiah(500_000_000),
		Description:    "Biaya tanah kavling A",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if item.ID == 0 {
		t.Error("item.ID == 0")
	}
}

func TestBudget_AddItem_FractionalAmount_Error(t *testing.T) {
	store := newMockStore()
	svc := buildService(store, nil)
	plan, _ := svc.CreatePlan(context.Background(), 1, budget.CreatePlanRequest{ProjectID: 10, Label: "RAB"})

	fractional, _ := domain.NewMoney("100000000.50")
	_, err := svc.AddItem(context.Background(), 1, budget.AddItemRequest{
		PlanID:         plan.ID,
		Category:       budget.BudgetCategoryConstruction,
		Subcategory:    string(domain.ConstructionSaranaPrasarana),
		BudgetedAmount: fractional,
	})
	if !errors.Is(err, budget.ErrAmountFractional) {
		t.Errorf("expected ErrAmountFractional, got %v", err)
	}
}

func TestBudget_AddItem_ZeroAmount_Error(t *testing.T) {
	store := newMockStore()
	svc := buildService(store, nil)
	plan, _ := svc.CreatePlan(context.Background(), 1, budget.CreatePlanRequest{ProjectID: 10, Label: "RAB"})

	_, err := svc.AddItem(context.Background(), 1, budget.AddItemRequest{
		PlanID: plan.ID, Category: budget.BudgetCategoryLand,
		BudgetedAmount: domain.Zero,
	})
	if !errors.Is(err, budget.ErrAmountZeroOrNeg) {
		t.Errorf("expected ErrAmountZeroOrNeg, got %v", err)
	}
}

func TestBudget_AddItem_InvalidCategory_Error(t *testing.T) {
	store := newMockStore()
	svc := buildService(store, nil)
	plan, _ := svc.CreatePlan(context.Background(), 1, budget.CreatePlanRequest{ProjectID: 10, Label: "RAB"})

	_, err := svc.AddItem(context.Background(), 1, budget.AddItemRequest{
		PlanID: plan.ID, Category: budget.BudgetCategory("unknown"),
		BudgetedAmount: rupiah(1_000_000),
	})
	if !errors.Is(err, budget.ErrInvalidCategory) {
		t.Errorf("expected ErrInvalidCategory, got %v", err)
	}
}

func TestBudget_AddItem_ToActivePlan_Error(t *testing.T) {
	// Setelah plan di-approve → active, tidak bisa ditambah item
	store := newMockStore()
	svc := buildService(store, nil)
	planID := mustCreateDraftWithItem(t, svc, 1, 10, nil, 100_000_000)
	_, _ = svc.ApprovePlan(context.Background(), 1, budget.ApprovePlanRequest{PlanID: planID, ApprovedBy: "admin"})

	_, err := svc.AddItem(context.Background(), 1, budget.AddItemRequest{
		PlanID:         planID,
		Category:       budget.BudgetCategoryLand,
		BudgetedAmount: rupiah(50_000_000),
	})
	if !errors.Is(err, budget.ErrPlanNotDraft) {
		t.Errorf("expected ErrPlanNotDraft, got %v", err)
	}
}

func TestBudget_AddItem_AllSixCategories_Valid(t *testing.T) {
	store := newMockStore()
	svc := buildService(store, nil)
	plan, _ := svc.CreatePlan(context.Background(), 1, budget.CreatePlanRequest{ProjectID: 10, Label: "RAB"})

	for _, cat := range budget.AllBudgetCategories {
		sub := ""
		if cat == budget.BudgetCategoryConstruction {
			sub = string(domain.ConstructionSaranaPrasarana)
		}
		_, err := svc.AddItem(context.Background(), 1, budget.AddItemRequest{
			PlanID:         plan.ID,
			Category:       cat,
			Subcategory:    sub,
			BudgetedAmount: rupiah(10_000_000),
		})
		if err != nil {
			t.Errorf("kategori %s: unexpected error: %v", cat, err)
		}
	}
}

// ── Tests: ApprovePlan — INVARIANT #8 ─────────────────────────────────────────

func TestBudget_ApprovePlan_DraftBecomesActive(t *testing.T) {
	store := newMockStore()
	svc := buildService(store, nil)
	planID := mustCreateDraftWithItem(t, svc, 1, 10, nil, 100_000_000)

	approved, err := svc.ApprovePlan(context.Background(), 1, budget.ApprovePlanRequest{
		PlanID:     planID,
		ApprovedBy: "manager@lithos.id",
	})
	if err != nil {
		t.Fatalf("ApprovePlan: %v", err)
	}
	if approved.Status != budget.BudgetPlanStatusActive {
		t.Errorf("status=%s, want active", approved.Status)
	}
	if approved.ApprovedAt == nil {
		t.Error("approved_at harus diisi saat approve")
	}
	if approved.ApprovedBy == nil || *approved.ApprovedBy != "manager@lithos.id" {
		t.Error("approved_by tidak cocok")
	}
}

// TestBudget_ApprovePlan_SupersedesExistingActive membuktikan Invariant #8:
// saat plan baru di-approve, plan lama OTOMATIS menjadi 'superseded'.
func TestBudget_ApprovePlan_SupersedesExistingActive(t *testing.T) {
	store := newMockStore()
	svc := buildService(store, nil)

	// Plan v1: buat draft, tambah item, approve → active
	plan1ID := mustCreateDraftWithItem(t, svc, 1, 10, nil, 500_000_000)
	_, err := svc.ApprovePlan(context.Background(), 1, budget.ApprovePlanRequest{PlanID: plan1ID, ApprovedBy: "admin"})
	if err != nil {
		t.Fatalf("approve plan1: %v", err)
	}

	// Verifikasi plan1 active sebelum plan2 di-approve
	if store.countActivePerProjectPhase(1, 10, nil) != 1 {
		t.Fatal("seharusnya ada 1 plan active setelah plan1 di-approve")
	}

	// Plan v2: buat draft baru, tambah item, approve
	plan2ID := mustCreateDraftWithItem(t, svc, 1, 10, nil, 800_000_000)
	_, err = svc.ApprovePlan(context.Background(), 1, budget.ApprovePlanRequest{PlanID: plan2ID, ApprovedBy: "admin"})
	if err != nil {
		t.Fatalf("approve plan2: %v", err)
	}

	// ── Invariant #8: hanya satu plan active ─────────────────────────────────
	activeCount := store.countActivePerProjectPhase(1, 10, nil)
	if activeCount != 1 {
		t.Errorf("INVARIANT #8 VIOLATED: ada %d plan active untuk (project=10, phase=nil), harus 1", activeCount)
	}

	// Plan1 harus sekarang 'superseded'
	plan1After, _ := svc.GetPlan(context.Background(), 1, plan1ID)
	if plan1After.Status != budget.BudgetPlanStatusSuperseded {
		t.Errorf("plan1.Status=%s, want superseded (auto-superseded saat plan2 di-approve)", plan1After.Status)
	}

	// Plan2 harus 'active'
	plan2After, _ := svc.GetPlan(context.Background(), 1, plan2ID)
	if plan2After.Status != budget.BudgetPlanStatusActive {
		t.Errorf("plan2.Status=%s, want active", plan2After.Status)
	}
}

// TestBudget_TidakBisaDuaActive_SatuProyek membuktikan Invariant #8 — bukti struktural:
// setelah 3 approve berurutan, selalu hanya ada 1 plan active.
func TestBudget_TidakBisaDuaActive_SatuProyek(t *testing.T) {
	store := newMockStore()
	svc := buildService(store, nil)

	for i := 0; i < 3; i++ {
		planID := mustCreateDraftWithItem(t, svc, 1, 10, nil, int64((i+1)*100_000_000))
		if _, err := svc.ApprovePlan(context.Background(), 1, budget.ApprovePlanRequest{PlanID: planID, ApprovedBy: "x"}); err != nil {
			t.Fatalf("approve round %d: %v", i+1, err)
		}

		// Setelah setiap approve: TEPAT 1 plan active
		count := store.countActivePerProjectPhase(1, 10, nil)
		if count != 1 {
			t.Errorf("round %d: ada %d plan active, harus selalu 1 (Invariant #8)", i+1, count)
		}
	}
}

func TestBudget_ApprovePlan_NoItems_Error(t *testing.T) {
	store := newMockStore()
	svc := buildService(store, nil)
	plan, _ := svc.CreatePlan(context.Background(), 1, budget.CreatePlanRequest{ProjectID: 10, Label: "RAB kosong"})

	_, err := svc.ApprovePlan(context.Background(), 1, budget.ApprovePlanRequest{PlanID: plan.ID})
	if !errors.Is(err, budget.ErrNoItemsToApprove) {
		t.Errorf("expected ErrNoItemsToApprove, got %v", err)
	}
}

func TestBudget_ApprovePlan_AlreadyActive_Error(t *testing.T) {
	store := newMockStore()
	svc := buildService(store, nil)
	planID := mustCreateDraftWithItem(t, svc, 1, 10, nil, 100_000_000)
	_, _ = svc.ApprovePlan(context.Background(), 1, budget.ApprovePlanRequest{PlanID: planID})

	// Approve yang sama lagi → sudah active
	_, err := svc.ApprovePlan(context.Background(), 1, budget.ApprovePlanRequest{PlanID: planID})
	if !errors.Is(err, budget.ErrPlanNotApprovable) {
		t.Errorf("expected ErrPlanNotApprovable, got %v", err)
	}
}

// ── Tests: Plan 'superseded' tetap terbaca (Invariant #8 audit trail) ─────────

func TestBudget_SupersededPlanStillReadable(t *testing.T) {
	store := newMockStore()
	svc := buildService(store, nil)

	// Plan v1: approve
	plan1ID := mustCreateDraftWithItem(t, svc, 1, 10, nil, 300_000_000)
	_, _ = svc.ApprovePlan(context.Background(), 1, budget.ApprovePlanRequest{PlanID: plan1ID, ApprovedBy: "a"})

	// Plan v2: approve → supersedes v1
	plan2ID := mustCreateDraftWithItem(t, svc, 1, 10, nil, 400_000_000)
	_, _ = svc.ApprovePlan(context.Background(), 1, budget.ApprovePlanRequest{PlanID: plan2ID, ApprovedBy: "b"})

	// Plan v1 masih bisa dibaca
	plan1, err := svc.GetPlan(context.Background(), 1, plan1ID)
	if err != nil {
		t.Fatalf("GetPlan(plan1): %v — plan superseded harus tetap bisa dibaca", err)
	}
	if plan1.Status != budget.BudgetPlanStatusSuperseded {
		t.Errorf("plan1.Status=%s, want superseded", plan1.Status)
	}

	// List histori: plan1 dan plan2 keduanya muncul
	plans, _ := svc.ListPlans(context.Background(), 1, 10, nil)
	if len(plans) != 2 {
		t.Errorf("ListPlans: got %d plans, want 2 (histori lengkap, tidak ada yang dihapus)", len(plans))
	}
}

// ── Tests: Fase berbeda bisa masing-masing punya satu active ─────────────────

func TestBudget_DifferentPhases_EachHaveOwnActive(t *testing.T) {
	store := newMockStore()
	svc := buildService(store, nil)
	phase1 := ptr(uint64(1))
	phase2 := ptr(uint64(2))

	// Approve plan untuk fase 1
	p1ID := mustCreateDraftWithItem(t, svc, 1, 10, phase1, 100_000_000)
	_, _ = svc.ApprovePlan(context.Background(), 1, budget.ApprovePlanRequest{PlanID: p1ID})

	// Approve plan untuk fase 2
	p2ID := mustCreateDraftWithItem(t, svc, 1, 10, phase2, 200_000_000)
	_, _ = svc.ApprovePlan(context.Background(), 1, budget.ApprovePlanRequest{PlanID: p2ID})

	// Masing-masing fase punya 1 active
	c1 := store.countActivePerProjectPhase(1, 10, phase1)
	c2 := store.countActivePerProjectPhase(1, 10, phase2)
	if c1 != 1 {
		t.Errorf("fase 1: %d active, want 1", c1)
	}
	if c2 != 1 {
		t.Errorf("fase 2: %d active, want 1", c2)
	}
}

// ── Tests: Delete item ────────────────────────────────────────────────────────

func TestBudget_DeleteItem_OnlyFromDraft_OK(t *testing.T) {
	store := newMockStore()
	svc := buildService(store, nil)
	plan, _ := svc.CreatePlan(context.Background(), 1, budget.CreatePlanRequest{ProjectID: 10, Label: "RAB"})
	item, _ := svc.AddItem(context.Background(), 1, budget.AddItemRequest{
		PlanID: plan.ID, Category: budget.BudgetCategoryMarketing, BudgetedAmount: rupiah(5_000_000),
	})

	if err := svc.DeleteItem(context.Background(), 1, plan.ID, item.ID); err != nil {
		t.Errorf("DeleteItem: %v", err)
	}

	items, _ := svc.ListItems(context.Background(), 1, plan.ID)
	if len(items) != 0 {
		t.Errorf("item harus terhapus, got %d items", len(items))
	}
}

func TestBudget_DeleteItem_FromActivePlan_Error(t *testing.T) {
	store := newMockStore()
	svc := buildService(store, nil)
	planID := mustCreateDraftWithItem(t, svc, 1, 10, nil, 100_000_000)

	// Ambil item sebelum approve
	items, _ := svc.ListItems(context.Background(), 1, planID)
	_, _ = svc.ApprovePlan(context.Background(), 1, budget.ApprovePlanRequest{PlanID: planID})

	err := svc.DeleteItem(context.Background(), 1, planID, items[0].ID)
	if !errors.Is(err, budget.ErrPlanNotDraft) {
		t.Errorf("expected ErrPlanNotDraft, got %v", err)
	}
}

// ── Tests: Tenant isolation ───────────────────────────────────────────────────

func TestBudget_TenantIsolation_OtherTenantCannotReadPlan(t *testing.T) {
	store := newMockStore()
	svc := buildService(store, nil)

	// Tenant 1 buat plan
	plan, _ := svc.CreatePlan(context.Background(), 1, budget.CreatePlanRequest{ProjectID: 10, Label: "RAB"})

	// Tenant 2 tidak bisa baca
	_, err := svc.GetPlan(context.Background(), 2, plan.ID)
	if !errors.Is(err, budget.ErrPlanNotFound) {
		t.Errorf("expected ErrPlanNotFound untuk cross-tenant, got %v", err)
	}
}

// ── Tests: RAB vs Realisasi ───────────────────────────────────────────────────

// TestBudget_RABvsRealisasi_ConstructionMapsToHard membuktikan alias construction↔hard.
// Budget 'construction' = 1M; realisasi domain.CostCategoryHard = 600K
// → selisih = 400K; persen = 60%.
func TestBudget_RABvsRealisasi_ConstructionMapsToHard(t *testing.T) {
	store := newMockStore()
	realisasi := &mockRealisasiProvider{data: map[domain.CostCategory]domain.Money{
		domain.CostCategoryHard: rupiah(600_000_000), // realisasi 'hard' = alias 'construction'
	}}
	svc := buildService(store, realisasi)

	// Setup: plan active dengan item 'construction' = 1M
	plan, _ := svc.CreatePlan(context.Background(), 1, budget.CreatePlanRequest{ProjectID: 10, Label: "RAB"})
	_, _ = svc.AddItem(context.Background(), 1, budget.AddItemRequest{
		PlanID: plan.ID, Category: budget.BudgetCategoryConstruction,
		Subcategory:    string(domain.ConstructionSaranaPrasarana),
		BudgetedAmount: rupiah(1_000_000_000),
	})
	_, _ = svc.ApprovePlan(context.Background(), 1, budget.ApprovePlanRequest{PlanID: plan.ID, ApprovedBy: "admin"})

	report, err := svc.GetRABvsRealisasi(context.Background(), 1, 10, nil)
	if err != nil {
		t.Fatalf("GetRABvsRealisasi: %v", err)
	}

	// Cari baris 'construction'
	var constructionRow *budget.RABvsRealisasiRow
	for i := range report.Rows {
		if report.Rows[i].Category == budget.BudgetCategoryConstruction {
			constructionRow = &report.Rows[i]
			break
		}
	}
	if constructionRow == nil {
		t.Fatal("baris 'construction' tidak ditemukan dalam laporan")
	}

	// Budgeted = 1M
	if constructionRow.Budgeted != "1000000000" {
		t.Errorf("Budgeted=%s, want 1000000000", constructionRow.Budgeted)
	}
	// Realisasi = 600K (dari CostCategoryHard, via alias)
	if constructionRow.Realisasi != "600000000" {
		t.Errorf("Realisasi=%s, want 600000000 (CostCategoryHard → BudgetCategoryConstruction alias)", constructionRow.Realisasi)
	}
	// Selisih = 400K
	if constructionRow.Selisih != "400000000" {
		t.Errorf("Selisih=%s, want 400000000", constructionRow.Selisih)
	}
	// Persen = fraksi 0.6 (kontrak <Persen> FE: fraksi mentah, bukan sudah *100)
	if constructionRow.PersenRealisasi != "0.6" {
		t.Errorf("PersenRealisasi=%s, want 0.6", constructionRow.PersenRealisasi)
	}
}

// TestBudget_RABvsRealisasi_MarketingOther_TerealisasiViaOverhead (Increment 2)
// membuktikan marketing/other TEREALISASI dari CostEntry tier overhead (kategori
// expense marketing/other) — menggantikan perilaku lama "realisasi selalu 0".
// Realisasi kapitalisasi (hard dst.) tidak bercampur ke baris beban.
func TestBudget_RABvsRealisasi_MarketingOther_TerealisasiViaOverhead(t *testing.T) {
	store := newMockStore()
	realisasi := &mockRealisasiProvider{data: map[domain.CostCategory]domain.Money{
		domain.CostCategoryHard:      rupiah(100_000_000),
		domain.CostCategoryMarketing: rupiah(30_000_000), // dari CostEntry tier overhead
		domain.CostCategoryOther:     rupiah(5_000_000),  // dari CostEntry tier overhead
	}}
	svc := buildService(store, realisasi)

	plan, _ := svc.CreatePlan(context.Background(), 1, budget.CreatePlanRequest{ProjectID: 10, Label: "RAB"})
	_, _ = svc.AddItem(context.Background(), 1, budget.AddItemRequest{
		PlanID: plan.ID, Category: budget.BudgetCategoryMarketing, BudgetedAmount: rupiah(50_000_000),
	})
	_, _ = svc.AddItem(context.Background(), 1, budget.AddItemRequest{
		PlanID: plan.ID, Category: budget.BudgetCategoryOther, BudgetedAmount: rupiah(20_000_000),
	})
	_, _ = svc.ApprovePlan(context.Background(), 1, budget.ApprovePlanRequest{PlanID: plan.ID, ApprovedBy: "x"})

	report, _ := svc.GetRABvsRealisasi(context.Background(), 1, 10, nil)

	for _, row := range report.Rows {
		switch row.Category {
		case budget.BudgetCategoryMarketing:
			if row.Realisasi != "30000000" {
				t.Errorf("marketing: realisasi=%s, want 30000000 (via tier overhead)", row.Realisasi)
			}
			if row.PersenRealisasi != "0.6" {
				t.Errorf("marketing: persen=%s, want 0.6", row.PersenRealisasi)
			}
		case budget.BudgetCategoryOther:
			if row.Realisasi != "5000000" {
				t.Errorf("other: realisasi=%s, want 5000000 (via tier overhead)", row.Realisasi)
			}
		}
	}
}

// TestBudget_RABvsRealisasi_SemuaKategori membuktikan laporan lengkap 6 kategori.
func TestBudget_RABvsRealisasi_SemuaKategori(t *testing.T) {
	store := newMockStore()
	realisasi := &mockRealisasiProvider{data: map[domain.CostCategory]domain.Money{
		domain.CostCategoryLand:        rupiah(180_000_000),
		domain.CostCategoryHard:        rupiah(350_000_000),
		domain.CostCategorySoft:        rupiah(40_000_000),
		domain.CostCategoryOperational: rupiah(15_000_000),
	}}
	svc := buildService(store, realisasi)

	plan, _ := svc.CreatePlan(context.Background(), 1, budget.CreatePlanRequest{ProjectID: 10, Label: "RAB"})
	budgets := map[budget.BudgetCategory]int64{
		budget.BudgetCategoryLand:         200_000_000,
		budget.BudgetCategoryConstruction: 500_000_000, // construction ↔ hard
		budget.BudgetCategorySoft:         80_000_000,
		budget.BudgetCategoryOperational:  30_000_000,
		budget.BudgetCategoryMarketing:    25_000_000,
		budget.BudgetCategoryOther:        10_000_000,
	}
	for cat, amt := range budgets {
		sub := ""
		if cat == budget.BudgetCategoryConstruction {
			sub = string(domain.ConstructionSaranaPrasarana)
		}
		_, _ = svc.AddItem(context.Background(), 1, budget.AddItemRequest{
			PlanID: plan.ID, Category: cat, Subcategory: sub, BudgetedAmount: rupiah(amt),
		})
	}
	_, _ = svc.ApprovePlan(context.Background(), 1, budget.ApprovePlanRequest{PlanID: plan.ID, ApprovedBy: "x"})

	report, err := svc.GetRABvsRealisasi(context.Background(), 1, 10, nil)
	if err != nil {
		t.Fatalf("GetRABvsRealisasi: %v", err)
	}

	if len(report.Rows) != 6 {
		t.Errorf("expect 6 rows, got %d", len(report.Rows))
	}

	// Total budgeted = 200+500+80+30+25+10 = 845M
	if report.TotalBudgeted != "845000000" {
		t.Errorf("TotalBudgeted=%s, want 845000000", report.TotalBudgeted)
	}
	// Total realisasi = 180+350+40+15 = 585M (marketing dan other = 0)
	if report.TotalRealisasi != "585000000" {
		t.Errorf("TotalRealisasi=%s, want 585000000", report.TotalRealisasi)
	}
	// Total selisih = 845 - 585 = 260M
	if report.TotalSelisih != "260000000" {
		t.Errorf("TotalSelisih=%s, want 260000000", report.TotalSelisih)
	}
}

func TestBudget_RABvsRealisasi_BudgetNol_PersenNA(t *testing.T) {
	store := newMockStore()
	svc := buildService(store, &mockRealisasiProvider{})

	// Buat plan dengan item construction saja; land tidak ada item → budget land = 0
	plan, _ := svc.CreatePlan(context.Background(), 1, budget.CreatePlanRequest{ProjectID: 10, Label: "RAB"})
	_, _ = svc.AddItem(context.Background(), 1, budget.AddItemRequest{
		PlanID: plan.ID, Category: budget.BudgetCategoryConstruction, Subcategory: string(domain.ConstructionSaranaPrasarana), BudgetedAmount: rupiah(100_000_000),
	})
	_, _ = svc.ApprovePlan(context.Background(), 1, budget.ApprovePlanRequest{PlanID: plan.ID})

	report, _ := svc.GetRABvsRealisasi(context.Background(), 1, 10, nil)

	for _, row := range report.Rows {
		if row.Category == budget.BudgetCategoryLand {
			if row.PersenRealisasi != "N/A" {
				t.Errorf("land budget=0: PersenRealisasi=%s, want N/A", row.PersenRealisasi)
			}
		}
	}
}

func TestBudget_RABvsRealisasi_NoActivePlan_Error(t *testing.T) {
	svc := buildService(newMockStore(), nil)
	_, err := svc.GetRABvsRealisasi(context.Background(), 1, 10, nil)
	if !errors.Is(err, budget.ErrNoActivePlan) {
		t.Errorf("expected ErrNoActivePlan, got %v", err)
	}
}

// ── Test: RAB APPROVED tidak memposting jurnal apa pun (Item 8, UAT 2026-09-07) ─
// RULE KLIEN 2026-09-04 (kapitalisasi Construction penuh ke Persediaan saat
// approval) DICABUT klien: RAB murni budget/planning. Persediaan/HPP kini
// murni biaya AKTUAL dari cost entry (internal/cost), tidak lagi dari RAB.
// ApprovePlan hanya mengaktifkan plan (dan men-supersede versi lama) — tidak
// menyentuh ledger sama sekali.

func TestBudget_ApprovePlan_DoesNotPostAnyJournal(t *testing.T) {
	store := newMockStore()
	realisasi := &mockRealisasiProvider{}
	svc := buildService(store, realisasi)

	planID := mustCreateDraftWithItem(t, svc, 1, 10, nil, 100_000_000)
	approved, err := svc.ApprovePlan(context.Background(), 1, budget.ApprovePlanRequest{
		PlanID: planID, ApprovedBy: "test",
	})
	if err != nil {
		t.Fatalf("ApprovePlan: %v", err)
	}
	if approved.Status != budget.BudgetPlanStatusActive {
		t.Error("plan harus active setelah approve")
	}
	// Tidak ada TxRunner/JournalWriter untuk dipasang lagi — ApprovePlan sukses
	// tanpa kolaborator ledger sama sekali, membuktikan tidak ada jurnal yang
	// bisa terposting dari jalur ini.
}

package project_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"esaproperti/internal/domain"
	"esaproperti/internal/project"
)

// ── In-memory mock stores ─────────────────────────────────────────────────────

type mockProjectStore struct {
	mu       sync.Mutex
	projects map[uint64]*project.Project
	nextID   uint64
}

func newMockProjectStore() *mockProjectStore {
	return &mockProjectStore{projects: make(map[uint64]*project.Project)}
}

func (m *mockProjectStore) CreateProject(_ context.Context, p *project.Project) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.nextID++
	p.ID = m.nextID
	p.CreatedAt = time.Now()
	p.UpdatedAt = time.Now()
	cp := *p
	m.projects[cp.ID] = &cp
	return nil
}

func (m *mockProjectStore) FindProjectByID(_ context.Context, tenantID, id uint64) (*project.Project, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.projects[id]
	if !ok || p.TenantID != tenantID {
		return nil, project.ErrProjectNotFound
	}
	cp := *p
	return &cp, nil
}

func (m *mockProjectStore) ListProjects(_ context.Context, tenantID uint64) ([]*project.Project, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []*project.Project
	for _, p := range m.projects {
		if p.TenantID == tenantID {
			cp := *p
			out = append(out, &cp)
		}
	}
	return out, nil
}

func (m *mockProjectStore) UpdateProjectTaxCategory(_ context.Context, tenantID, id uint64, category domain.TaxCategory) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.projects[id]
	if !ok || p.TenantID != tenantID {
		return project.ErrProjectNotFound
	}
	p.TaxCategory = category
	return nil
}

func (m *mockProjectStore) UpdateProjectStatus(_ context.Context, tenantID, id uint64, status project.ProjectStatus) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.projects[id]
	if !ok || p.TenantID != tenantID {
		return project.ErrProjectNotFound
	}
	p.Status = status
	return nil
}

type mockPhaseStore struct {
	mu     sync.Mutex
	phases map[uint64]*project.ProjectPhase
	nextID uint64
}

func newMockPhaseStore() *mockPhaseStore {
	return &mockPhaseStore{phases: make(map[uint64]*project.ProjectPhase)}
}

func (m *mockPhaseStore) CreatePhase(_ context.Context, ph *project.ProjectPhase) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.nextID++
	ph.ID = m.nextID
	ph.CreatedAt = time.Now()
	ph.UpdatedAt = time.Now()
	cp := *ph
	m.phases[cp.ID] = &cp
	return nil
}

func (m *mockPhaseStore) FindPhaseByID(_ context.Context, tenantID, id uint64) (*project.ProjectPhase, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	ph, ok := m.phases[id]
	if !ok || ph.TenantID != tenantID {
		return nil, project.ErrPhaseNotFound
	}
	cp := *ph
	return &cp, nil
}

func (m *mockPhaseStore) ListPhasesByProject(_ context.Context, tenantID, projectID uint64) ([]*project.ProjectPhase, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []*project.ProjectPhase
	for _, ph := range m.phases {
		if ph.TenantID == tenantID && ph.ProjectID == projectID {
			cp := *ph
			out = append(out, &cp)
		}
	}
	return out, nil
}

func (m *mockPhaseStore) UpdatePhaseStatus(_ context.Context, tenantID, id uint64, status project.PhaseStatus) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	ph, ok := m.phases[id]
	if !ok || ph.TenantID != tenantID {
		return project.ErrPhaseNotFound
	}
	ph.Status = status
	return nil
}

type mockUnitStore struct {
	mu          sync.Mutex
	units       map[uint64]*project.Unit
	nextID      uint64
	transitions []*project.UnitStatusTransition
	transID     uint64
}

func newMockUnitStore() *mockUnitStore {
	return &mockUnitStore{units: make(map[uint64]*project.Unit)}
}

func (m *mockUnitStore) CreateUnitsAtomic(ctx context.Context, tenantID, projectID uint64, units []*project.Unit) error {
	m.mu.Lock()
	for _, u := range units {
		for _, existing := range m.units {
			if existing.TenantID == u.TenantID && existing.ProjectID == u.ProjectID && existing.Code == u.Code {
				m.mu.Unlock()
				return project.ErrBulkDuplicateCode
			}
		}
	}
	m.mu.Unlock()
	for _, u := range units {
		if err := m.CreateUnit(ctx, u); err != nil {
			return err
		}
	}
	return nil
}

func (m *mockUnitStore) CreateUnit(_ context.Context, u *project.Unit) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	// Enforce unique (tenant_id, project_id, code)
	for _, existing := range m.units {
		if existing.TenantID == u.TenantID && existing.ProjectID == u.ProjectID && existing.Code == u.Code {
			return project.ErrUnitCodeDuplicate
		}
	}
	m.nextID++
	u.ID = m.nextID
	u.CreatedAt = time.Now()
	u.UpdatedAt = time.Now()
	cp := *u
	m.units[cp.ID] = &cp
	return nil
}

func (m *mockUnitStore) FindUnitByID(_ context.Context, tenantID, id uint64) (*project.Unit, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	u, ok := m.units[id]
	if !ok || u.TenantID != tenantID {
		return nil, project.ErrUnitNotFound
	}
	cp := *u
	return &cp, nil
}

func (m *mockUnitStore) ListUnitsByProject(_ context.Context, tenantID, projectID uint64) ([]*project.Unit, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []*project.Unit
	for _, u := range m.units {
		if u.TenantID == tenantID && u.ProjectID == projectID {
			cp := *u
			out = append(out, &cp)
		}
	}
	return out, nil
}

func (m *mockUnitStore) ListUnitsByPhase(_ context.Context, tenantID, phaseID uint64) ([]*project.Unit, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []*project.Unit
	for _, u := range m.units {
		if u.TenantID == tenantID && u.PhaseID != nil && *u.PhaseID == phaseID {
			cp := *u
			out = append(out, &cp)
		}
	}
	return out, nil
}

// Nama pemegang unit lahir dari join lintas tabel (kontrak/booking) yang tidak
// ada di mock ini; unit test service tidak menguji isinya. Integration test
// yang menjaga urutannya.
func (m *mockUnitStore) AttachBuyerNames(_ context.Context, _ uint64, _ []*project.Unit) error {
	return nil
}

func (m *mockUnitStore) UpdateUnitStatus(_ context.Context, tenantID, id uint64, status project.UnitStatus, opts project.UpdateUnitOpts) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	u, ok := m.units[id]
	if !ok || u.TenantID != tenantID {
		return project.ErrUnitNotFound
	}
	u.Status = status
	if opts.BuyerRef != nil {
		u.BuyerRef = opts.BuyerRef
	}
	if opts.SaleDate != nil {
		u.SaleDate = opts.SaleDate
	}
	if opts.SalePrice != nil {
		u.SalePrice = opts.SalePrice
	}
	return nil
}

func (m *mockUnitStore) UpdateUnitLandArea(_ context.Context, tenantID, id uint64, landArea decimal.Decimal) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	u, ok := m.units[id]
	if !ok || u.TenantID != tenantID {
		return project.ErrUnitNotFound
	}
	u.LandArea = landArea
	return nil
}

func (m *mockUnitStore) TransitionUnitAtomic(_ context.Context, tenantID, id uint64, from, to project.UnitStatus, opts project.UpdateUnitOpts, log *project.UnitStatusTransition) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	u, ok := m.units[id]
	if !ok || u.TenantID != tenantID {
		return project.ErrUnitNotFound
	}
	// Optimistic guard: status must still equal `from` (mirror the SQL WHERE).
	if u.Status != from {
		return project.ErrUnitTransitionConflict
	}
	u.Status = to
	if opts.BuyerRef != nil {
		u.BuyerRef = opts.BuyerRef
	}
	if opts.SaleDate != nil {
		u.SaleDate = opts.SaleDate
	}
	if opts.SalePrice != nil {
		u.SalePrice = opts.SalePrice
	}
	m.transID++
	log.ID = m.transID
	log.CreatedAt = time.Now()
	cp := *log
	m.transitions = append(m.transitions, &cp)
	return nil
}

func (m *mockUnitStore) ListProjectTransitions(_ context.Context, tenantID, projectID uint64, limit int) ([]*project.UnitStatusTransition, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []*project.UnitStatusTransition
	for i := len(m.transitions) - 1; i >= 0 && len(out) < limit; i-- {
		t := m.transitions[i]
		u, ok := m.units[t.UnitID]
		if t.TenantID == tenantID && ok && u.ProjectID == projectID {
			cp := *t
			out = append(out, &cp)
		}
	}
	return out, nil
}

func (m *mockUnitStore) ListTransitions(_ context.Context, tenantID, unitID uint64) ([]*project.UnitStatusTransition, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []*project.UnitStatusTransition
	for _, t := range m.transitions {
		if t.TenantID == tenantID && t.UnitID == unitID {
			cp := *t
			out = append(out, &cp)
		}
	}
	return out, nil
}

// newTestService builds a Service backed by in-memory mocks.
func newTestService() *project.Service {
	return project.NewService(newMockProjectStore(), newMockPhaseStore(), newMockUnitStore())
}

// mockProductTypeStore (LT-0): mock in-memory project.ProductTypeStore, dipakai
// untuk menguji excludeNonPropertyUnits tanpa DB nyata.
type mockProductTypeStore struct {
	byKey map[string]*project.ProductType
}

func newMockProductTypeStore() *mockProductTypeStore {
	return &mockProductTypeStore{byKey: map[string]*project.ProductType{}}
}

func (m *mockProductTypeStore) key(tenantID uint64, code string) string {
	return fmt.Sprintf("%d:%s", tenantID, code)
}

// seed mendaftarkan satu product type ke master — dipakai fixture test.
func (m *mockProductTypeStore) seed(tenantID uint64, code string, category project.ProductCategory) {
	m.byKey[m.key(tenantID, code)] = &project.ProductType{
		TenantID: tenantID, Code: code, Name: code, Category: category,
		RevenueAccountCode: "4-1000", IsActive: true,
	}
}

func (m *mockProductTypeStore) ListProductTypes(_ context.Context, tenantID uint64) ([]*project.ProductType, error) {
	var out []*project.ProductType
	for _, pt := range m.byKey {
		if pt.TenantID == tenantID {
			out = append(out, pt)
		}
	}
	return out, nil
}

func (m *mockProductTypeStore) FindProductTypeByCode(_ context.Context, tenantID uint64, code string) (*project.ProductType, error) {
	pt, ok := m.byKey[m.key(tenantID, code)]
	if !ok {
		return nil, project.ErrProductTypeNotFound
	}
	return pt, nil
}

func (m *mockProductTypeStore) CreateProductType(_ context.Context, pt *project.ProductType) error {
	m.byKey[m.key(pt.TenantID, pt.Code)] = pt
	return nil
}

func (m *mockProductTypeStore) UpdateProductType(_ context.Context, tenantID, id uint64, updates map[string]interface{}) (*project.ProductType, error) {
	return nil, errors.New("mockProductTypeStore: UpdateProductType tidak diimplementasikan")
}

func (m *mockProductTypeStore) ValidateRevenueAccount(_ context.Context, tenantID uint64, code string) error {
	return nil
}

// ── Project tests ─────────────────────────────────────────────────────────────

func TestProject_Create_Success(t *testing.T) {
	svc := newTestService()
	p, err := svc.CreateProject(context.Background(), 1, project.CreateProjectRequest{
		Name:     "LITHOS Villas",
		LandArea: decimal.NewFromFloat(5000),
		Notes:    "Proyek villa premium Bali",
	})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	if p.ID == 0 {
		t.Error("ID should be set after create")
	}
	if p.Name != "LITHOS Villas" {
		t.Errorf("Name = %q, want %q", p.Name, "LITHOS Villas")
	}
	if p.Status != project.ProjectStatusPlanning {
		t.Errorf("initial status should be planning, got %s", p.Status)
	}
	if p.TenantID != 1 {
		t.Errorf("TenantID = %d, want 1", p.TenantID)
	}
}

func TestProject_StatusTransition_Legal(t *testing.T) {
	cases := []struct {
		from project.ProjectStatus
		to   project.ProjectStatus
	}{
		{project.ProjectStatusPlanning, project.ProjectStatusActive},
		{project.ProjectStatusActive, project.ProjectStatusSelling},
		{project.ProjectStatusSelling, project.ProjectStatusCompleted},
	}
	for _, tc := range cases {
		svc := newTestService()
		p, _ := svc.CreateProject(context.Background(), 1, project.CreateProjectRequest{Name: "X"})

		// Fast-forward status to `from` state.
		if err := forceProjectStatus(svc, p.ID, tc.from); err != nil {
			t.Fatalf("setup transition to %s: %v", tc.from, err)
		}

		updated, err := svc.TransitionProject(context.Background(), 1, p.ID, tc.to)
		if err != nil {
			t.Errorf("%s → %s should be legal, got error: %v", tc.from, tc.to, err)
			continue
		}
		if updated.Status != tc.to {
			t.Errorf("status after transition = %s, want %s", updated.Status, tc.to)
		}
	}
}

func TestProject_StatusTransition_Illegal(t *testing.T) {
	illegal := []struct {
		from project.ProjectStatus
		to   project.ProjectStatus
	}{
		{project.ProjectStatusPlanning, project.ProjectStatusSelling},
		{project.ProjectStatusPlanning, project.ProjectStatusCompleted},
		{project.ProjectStatusActive, project.ProjectStatusPlanning},
		{project.ProjectStatusCompleted, project.ProjectStatusPlanning},
		{project.ProjectStatusCompleted, project.ProjectStatusActive},
	}
	for _, tc := range illegal {
		svc := newTestService()
		p, _ := svc.CreateProject(context.Background(), 1, project.CreateProjectRequest{Name: "X"})
		if err := forceProjectStatus(svc, p.ID, tc.from); err != nil {
			t.Fatalf("setup: %v", err)
		}
		_, err := svc.TransitionProject(context.Background(), 1, p.ID, tc.to)
		if !errors.Is(err, project.ErrProjectInvalidTransition) {
			t.Errorf("%s → %s should be illegal, got %v", tc.from, tc.to, err)
		}
	}
}

// ── Unit tests ────────────────────────────────────────────────────────────────

func TestUnit_Create_Success(t *testing.T) {
	svc := newTestService()
	p, _ := svc.CreateProject(context.Background(), 1, project.CreateProjectRequest{Name: "LITHOS"})

	u, err := svc.CreateUnit(context.Background(), 1, project.CreateUnitRequest{
		ProjectID:    p.ID,
		Code:         "LITHOS-A01",
		UnitType:     "villa",
		SaleableArea: decimal.NewFromFloat(150),
		ListPrice:    domain.FromInt(2_000_000_000),
	})
	if err != nil {
		t.Fatalf("CreateUnit: %v", err)
	}
	if u.ID == 0 {
		t.Error("ID should be set")
	}
	if u.Status != project.UnitStatusAvailable {
		t.Errorf("initial status should be available, got %s", u.Status)
	}
	if u.ProjectID != p.ID {
		t.Errorf("ProjectID = %d, want %d", u.ProjectID, p.ID)
	}
	if u.TenantID != 1 {
		t.Errorf("TenantID = %d, want 1", u.TenantID)
	}
}

func TestUnit_Create_FractionalListPrice_Rejected(t *testing.T) {
	svc := newTestService()
	p, _ := svc.CreateProject(context.Background(), 1, project.CreateProjectRequest{Name: "X"})

	_, err := svc.CreateUnit(context.Background(), 1, project.CreateUnitRequest{
		ProjectID:    p.ID,
		Code:         "X-01",
		UnitType:     "villa",
		SaleableArea: decimal.NewFromFloat(100),
		ListPrice:    domain.MustParse("1500000.50"), // fractional sen
	})
	if !errors.Is(err, project.ErrListPriceFractional) {
		t.Errorf("expected ErrListPriceFractional, got %v", err)
	}
}

func TestUnit_Create_DuplicateCode_Rejected(t *testing.T) {
	svc := newTestService()
	p, _ := svc.CreateProject(context.Background(), 1, project.CreateProjectRequest{Name: "X"})

	req := project.CreateUnitRequest{
		ProjectID:    p.ID,
		Code:         "X-01",
		UnitType:     "villa",
		SaleableArea: decimal.NewFromFloat(100),
		ListPrice:    domain.FromInt(1_000_000),
	}
	if _, err := svc.CreateUnit(context.Background(), 1, req); err != nil {
		t.Fatalf("first create: %v", err)
	}
	_, err := svc.CreateUnit(context.Background(), 1, req)
	if !errors.Is(err, project.ErrUnitCodeDuplicate) {
		t.Errorf("duplicate code should return ErrUnitCodeDuplicate, got %v", err)
	}
}

// LT-1: kelebihan-tanah-final-architecture — `land` ikut HPP (ParticipatesInHPP)
// tapi TIDAK PERNAH boleh jadi baris `units` (bukan lifecycle unit, standalone
// product LT-3+). validateUnitType wajib memakai MayBecomeUnit(), bukan
// ParticipatesInHPP() — kalau salah pakai, `land` akan lolos jadi unit dengan
// HPP salah tempel.
func TestUnit_Create_LandCategory_Rejected(t *testing.T) {
	svc := newTestService()
	pts := newMockProductTypeStore()
	pts.seed(1, "kelebihan-tanah", domain.ProductCategoryLand)
	svc.SetProductTypeStore(pts)

	p, _ := svc.CreateProject(context.Background(), 1, project.CreateProjectRequest{Name: "X"})

	_, err := svc.CreateUnit(context.Background(), 1, project.CreateUnitRequest{
		ProjectID:    p.ID,
		Code:         "X-LT-01",
		UnitType:     "kelebihan-tanah",
		SaleableArea: decimal.NewFromFloat(100),
		ListPrice:    domain.FromInt(1_000_000),
	})
	if !errors.Is(err, project.ErrUnitTypeNotProperty) {
		t.Errorf("kategori land tidak boleh jadi unit: want ErrUnitTypeNotProperty, got %v", err)
	}
}

// Kontrol positif untuk test di atas: `property` tetap boleh jadi unit lewat
// gerbang yang sama, membuktikan fix tidak menolak kategori yang seharusnya lolos.
func TestUnit_Create_PropertyCategory_Allowed(t *testing.T) {
	svc := newTestService()
	pts := newMockProductTypeStore()
	pts.seed(1, "villa", domain.ProductCategoryProperty)
	svc.SetProductTypeStore(pts)

	p, _ := svc.CreateProject(context.Background(), 1, project.CreateProjectRequest{Name: "X"})

	_, err := svc.CreateUnit(context.Background(), 1, project.CreateUnitRequest{
		ProjectID:    p.ID,
		Code:         "X-PROP-01",
		UnitType:     "villa",
		SaleableArea: decimal.NewFromFloat(100),
		ListPrice:    domain.FromInt(1_000_000),
	})
	if err != nil {
		t.Errorf("kategori property harus boleh jadi unit, got %v", err)
	}
}

// LT-2: land_area diisi eksplisit-admin lewat jalur terpisah dari create/transition.
func TestUnit_UpdateLandArea_Success(t *testing.T) {
	svc := newTestService()
	p, _ := svc.CreateProject(context.Background(), 1, project.CreateProjectRequest{Name: "X"})
	u, err := svc.CreateUnit(context.Background(), 1, project.CreateUnitRequest{
		ProjectID: p.ID, Code: "X-LA-01", UnitType: "villa",
		SaleableArea: decimal.NewFromFloat(100), ListPrice: domain.FromInt(1_000_000),
	})
	if err != nil {
		t.Fatalf("create unit: %v", err)
	}
	if !u.LandArea.IsZero() {
		t.Fatalf("land_area default harus 0, got %s", u.LandArea)
	}

	updated, err := svc.UpdateUnitLandArea(context.Background(), 1, u.ID, decimal.NewFromFloat(150))
	if err != nil {
		t.Fatalf("UpdateUnitLandArea: %v", err)
	}
	if !updated.LandArea.Equal(decimal.NewFromFloat(150)) {
		t.Errorf("land_area want 150, got %s", updated.LandArea)
	}
}

func TestUnit_UpdateLandArea_NegativeRejected(t *testing.T) {
	svc := newTestService()
	p, _ := svc.CreateProject(context.Background(), 1, project.CreateProjectRequest{Name: "X"})
	u, _ := svc.CreateUnit(context.Background(), 1, project.CreateUnitRequest{
		ProjectID: p.ID, Code: "X-LA-02", UnitType: "villa",
		SaleableArea: decimal.NewFromFloat(100), ListPrice: domain.FromInt(1_000_000),
	})

	_, err := svc.UpdateUnitLandArea(context.Background(), 1, u.ID, decimal.NewFromFloat(-1))
	if !errors.Is(err, project.ErrLandAreaNegative) {
		t.Errorf("want ErrLandAreaNegative, got %v", err)
	}
}

func TestUnit_UpdateLandArea_NotFound(t *testing.T) {
	svc := newTestService()
	_, err := svc.UpdateUnitLandArea(context.Background(), 1, 999999, decimal.NewFromFloat(50))
	if !errors.Is(err, project.ErrUnitNotFound) {
		t.Errorf("want ErrUnitNotFound, got %v", err)
	}
}

func TestUnit_StatusTransition_Legal(t *testing.T) {
	cases := []struct {
		from project.UnitStatus
		to   project.UnitStatus
	}{
		{project.UnitStatusAvailable, project.UnitStatusReserved},
		{project.UnitStatusReserved, project.UnitStatusAvailable},
		{project.UnitStatusReserved, project.UnitStatusSold},
	}
	for _, tc := range cases {
		svc := newTestService()
		p, _ := svc.CreateProject(context.Background(), 1, project.CreateProjectRequest{Name: "X"})
		u, _ := svc.CreateUnit(context.Background(), 1, project.CreateUnitRequest{
			ProjectID: p.ID, Code: "X-01", UnitType: "villa",
			SaleableArea: decimal.NewFromFloat(100), ListPrice: domain.FromInt(1_000_000),
		})

		if err := forceUnitStatus(svc, u.ID, tc.from); err != nil {
			t.Fatalf("setup: %v", err)
		}

		buyer := "Budi Santoso"
		updated, err := svc.TransitionUnit(context.Background(), 1, u.ID, tc.to, project.UpdateUnitOpts{
			BuyerRef: &buyer,
		})
		if err != nil {
			t.Errorf("%s → %s should be legal, got %v", tc.from, tc.to, err)
			continue
		}
		if updated.Status != tc.to {
			t.Errorf("status = %s, want %s", updated.Status, tc.to)
		}
	}
}

func TestUnit_StatusTransition_Illegal(t *testing.T) {
	illegal := []struct {
		from    project.UnitStatus
		to      project.UnitStatus
		wantErr error // sentinel yang diharapkan
	}{
		{project.UnitStatusAvailable, project.UnitStatusSold, project.ErrUnitInvalidTransition},        // skip reserved
		{project.UnitStatusSold, project.UnitStatusAvailable, project.ErrPostBASTCancellation},         // pasca-BAST: butuh domain Cancellation (D2)
		{project.UnitStatusSold, project.UnitStatusReserved, project.ErrUnitInvalidTransition},         // sold hanya → occupied|available
		{project.UnitStatusAvailable, project.UnitStatusAvailable, project.ErrUnitInvalidTransition},   // no-op tidak sah
		{project.UnitStatusBooked, project.UnitStatusSold, project.ErrUnitInvalidTransition},           // booked tak bisa langsung sold
		{project.UnitStatusHold, project.UnitStatusReserved, project.ErrUnitInvalidTransition},         // hold hanya → available
		{project.UnitStatusMaintenance, project.UnitStatusAvailable, project.ErrUnitInvalidTransition}, // maintenance hanya → occupied
	}
	for _, tc := range illegal {
		svc := newTestService()
		p, _ := svc.CreateProject(context.Background(), 1, project.CreateProjectRequest{Name: "X"})
		u, _ := svc.CreateUnit(context.Background(), 1, project.CreateUnitRequest{
			ProjectID: p.ID, Code: "X-01", UnitType: "villa",
			SaleableArea: decimal.NewFromFloat(100), ListPrice: domain.FromInt(1_000_000),
		})

		if err := forceUnitStatus(svc, u.ID, tc.from); err != nil {
			t.Fatalf("setup: %v", err)
		}

		_, err := svc.TransitionUnit(context.Background(), 1, u.ID, tc.to, project.UpdateUnitOpts{})
		if !errors.Is(err, tc.wantErr) {
			t.Errorf("%s → %s: want %v, got %v", tc.from, tc.to, tc.wantErr, err)
		}
	}
}

// TestUnit_BelongsToExactlyOneProject: unit's project_id is always set and immutable.
func TestUnit_BelongsToExactlyOneProject(t *testing.T) {
	svc := newTestService()
	p, _ := svc.CreateProject(context.Background(), 1, project.CreateProjectRequest{Name: "LITHOS"})
	u, err := svc.CreateUnit(context.Background(), 1, project.CreateUnitRequest{
		ProjectID: p.ID, Code: "LITHOS-A01", UnitType: "villa",
		SaleableArea: decimal.NewFromFloat(150), ListPrice: domain.FromInt(2_000_000_000),
	})
	if err != nil {
		t.Fatalf("CreateUnit: %v", err)
	}
	if u.ProjectID != p.ID {
		t.Errorf("unit.ProjectID = %d, want %d", u.ProjectID, p.ID)
	}

	// Getting unit returns the same project_id.
	fetched, err := svc.GetUnit(context.Background(), 1, u.ID)
	if err != nil {
		t.Fatalf("GetUnit: %v", err)
	}
	if fetched.ProjectID != p.ID {
		t.Errorf("fetched.ProjectID = %d, want %d", fetched.ProjectID, p.ID)
	}
}

// TestUnit_CrossTenant_Isolation: tenant B cannot access tenant A's units.
func TestUnit_CrossTenant_Isolation(t *testing.T) {
	svc := newTestService()
	pA, _ := svc.CreateProject(context.Background(), 1, project.CreateProjectRequest{Name: "Project A"})
	uA, _ := svc.CreateUnit(context.Background(), 1, project.CreateUnitRequest{
		ProjectID: pA.ID, Code: "A-01", UnitType: "villa",
		SaleableArea: decimal.NewFromFloat(100), ListPrice: domain.FromInt(1_000_000),
	})

	// Tenant B tries to fetch tenant A's unit.
	_, err := svc.GetUnit(context.Background(), 2, uA.ID)
	if !errors.Is(err, project.ErrUnitNotFound) {
		t.Errorf("cross-tenant GetUnit: expected ErrUnitNotFound, got %v", err)
	}

	// Tenant B list returns empty (not tenant A's data).
	units, err := svc.ListUnitsByProject(context.Background(), 2, pA.ID)
	if err != nil {
		t.Fatalf("ListUnitsByProject: %v", err)
	}
	if len(units) != 0 {
		t.Errorf("cross-tenant list should return 0 units, got %d", len(units))
	}
}

// TestListUnitsByProject_ExcludesNonPropertyUnits_KeepsUnmappedLegacyTypes
// (LT-0): listing /penjualan (ListUnitsByProject/ListUnitsByPhase) tidak boleh
// lagi menampilkan unit non-properti ("Kelebihan Tanah", dulu tersimpan sebagai
// baris `units` biasa — bug ditemukan lewat audit arsitektur Kelebihan Tanah,
// contoh live: unit id 1714 & 5931). unit_type legacy yang belum terdaftar di
// master product_types harus TETAP tampil (fail-open) — regresi ini menjaga
// listing tidak pernah kehilangan unit properti sungguhan gara-gara master data
// yang belum lengkap (banyak ditemukan di tenant lama, mis. "villa"/"Hook"
// pra-katalog).
func TestListUnitsByProject_ExcludesNonPropertyUnits_KeepsUnmappedLegacyTypes(t *testing.T) {
	units := newMockUnitStore()
	svc := project.NewService(newMockProjectStore(), newMockPhaseStore(), units)
	pts := newMockProductTypeStore()
	svc.SetProductTypeStore(pts)
	ctx := context.Background()

	p, err := svc.CreateProject(ctx, 1, project.CreateProjectRequest{Name: "LT-0 Fixture"})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	ph, err := svc.CreatePhase(ctx, 1, project.CreatePhaseRequest{ProjectID: p.ID, Name: "Fase 1"})
	if err != nil {
		t.Fatalf("CreatePhase: %v", err)
	}

	pts.seed(1, "rumah", project.ProductCategoryProperty)
	pts.seed(1, "kelebihan_tanah", project.ProductCategoryNonProperty)
	// "villa-legacy" sengaja TIDAK didaftarkan di master — mensimulasikan
	// unit_type pra-katalog tenant lama yang belum termapping.

	// Seed langsung ke store (bypass CreateUnit/validateUnitType) — baris
	// "kelebihan_tanah" di produksi (1714, 5931) sudah ada SEBELUM guard W-13
	// berlaku, jadi test ini harus mensimulasikan data yang sudah terlanjur ada,
	// bukan mencoba membuatnya lewat jalur yang sekarang menolaknya.
	seed := func(code, unitType string, phaseID *uint64) {
		t.Helper()
		if err := units.CreateUnit(ctx, &project.Unit{
			TenantID: 1, ProjectID: p.ID, PhaseID: phaseID, Code: code, UnitType: unitType,
			SaleableArea: decimal.NewFromFloat(100), ListPrice: domain.FromInt(1_000_000),
			Status: project.UnitStatusAvailable,
		}); err != nil {
			t.Fatalf("seed unit %s: %v", code, err)
		}
	}
	seed("R-01", "rumah", &ph.ID)
	seed("KT-01", "kelebihan_tanah", &ph.ID)
	seed("V-LEGACY-01", "villa-legacy", &ph.ID)

	assertCodes := func(t *testing.T, list []*project.Unit, label string) {
		t.Helper()
		got := map[string]bool{}
		for _, u := range list {
			got[u.Code] = true
		}
		if !got["R-01"] {
			t.Errorf("%s: unit properti terdaftar (rumah) harus tetap tampil", label)
		}
		if !got["V-LEGACY-01"] {
			t.Errorf("%s: unit_type legacy yang belum termapping di master harus tetap tampil (fail-open)", label)
		}
		if got["KT-01"] {
			t.Errorf("%s: unit non-properti terdaftar (kelebihan_tanah) tidak boleh muncul di listing /penjualan", label)
		}
		if len(list) != 2 {
			t.Errorf("%s: jumlah unit di listing = %d, want 2", label, len(list))
		}
	}

	byProject, err := svc.ListUnitsByProject(ctx, 1, p.ID)
	if err != nil {
		t.Fatalf("ListUnitsByProject: %v", err)
	}
	assertCodes(t, byProject, "ListUnitsByProject")

	byPhase, err := svc.ListUnitsByPhase(ctx, 1, ph.ID)
	if err != nil {
		t.Fatalf("ListUnitsByPhase: %v", err)
	}
	assertCodes(t, byPhase, "ListUnitsByPhase")
}

// TestListUnitsByProject_NilProductTypeStore_PreservesLegacyBehavior: tanpa
// store product type terpasang (svc.SetProductTypeStore tidak dipanggil —
// kondisi banyak unit test lama di file ini), listing tidak boleh menyaring
// apa pun — perilaku sebelum LT-0 harus tetap terjaga persis.
func TestListUnitsByProject_NilProductTypeStore_PreservesLegacyBehavior(t *testing.T) {
	svc := newTestService() // productTypes store TIDAK dipasang
	ctx := context.Background()

	p, _ := svc.CreateProject(ctx, 1, project.CreateProjectRequest{Name: "Legacy"})
	_, err := svc.CreateUnit(ctx, 1, project.CreateUnitRequest{
		ProjectID: p.ID, Code: "KT-01", UnitType: "kelebihan_tanah",
		SaleableArea: decimal.NewFromFloat(100), ListPrice: domain.FromInt(1_000_000),
	})
	if err != nil {
		t.Fatalf("CreateUnit: %v", err)
	}

	list, err := svc.ListUnitsByProject(ctx, 1, p.ID)
	if err != nil {
		t.Fatalf("ListUnitsByProject: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("tanpa product type store, listing tidak boleh menyaring apa pun; got %d unit, want 1", len(list))
	}
}

// TestProject_CrossTenant_Isolation: tenant B cannot see tenant A's projects.
func TestProject_CrossTenant_Isolation(t *testing.T) {
	svc := newTestService()
	_, _ = svc.CreateProject(context.Background(), 1, project.CreateProjectRequest{Name: "Tenant A Project"})

	list, err := svc.ListProjects(context.Background(), 2)
	if err != nil {
		t.Fatalf("ListProjects: %v", err)
	}
	if len(list) != 0 {
		t.Errorf("tenant B should see 0 projects, got %d", len(list))
	}
}

// ── Helpers ───────────────────────────────────────────────────────────────────

// forceProjectStatus drives a project to the given status via sequential legal transitions.
func forceProjectStatus(svc *project.Service, projectID uint64, target project.ProjectStatus) error {
	chain := []project.ProjectStatus{
		project.ProjectStatusPlanning,
		project.ProjectStatusActive,
		project.ProjectStatusSelling,
		project.ProjectStatusCompleted,
	}
	for i, s := range chain {
		if s == target {
			return nil // already at target
		}
		if i+1 >= len(chain) {
			break
		}
		if _, err := svc.TransitionProject(context.Background(), 1, projectID, chain[i+1]); err != nil {
			return err
		}
		if chain[i+1] == target {
			return nil
		}
	}
	return nil
}

// forceUnitStatus drives a unit to the given status via legal transitions.
// forceUnitStatus drives a unit to `target` via legal transitions (chains for the
// deeper states). Used to set up the `from` state before an illegal-move assertion.
func forceUnitStatus(svc *project.Service, unitID uint64, target project.UnitStatus) error {
	ctx := context.Background()
	buyer := "test-buyer"
	step := func(to project.UnitStatus, opts project.UpdateUnitOpts) error {
		_, err := svc.TransitionUnit(ctx, 1, unitID, to, opts)
		return err
	}
	// chain applies a sequence, stopping at the first error.
	chain := func(steps ...project.UnitStatus) error {
		for _, st := range steps {
			opts := project.UpdateUnitOpts{}
			if st == project.UnitStatusReserved {
				opts.BuyerRef = &buyer
			}
			if err := step(st, opts); err != nil {
				return err
			}
		}
		return nil
	}
	switch target {
	case project.UnitStatusAvailable:
		return nil // default
	case project.UnitStatusBooked:
		return chain(project.UnitStatusBooked)
	case project.UnitStatusReserved:
		return chain(project.UnitStatusReserved)
	case project.UnitStatusPPJB:
		return chain(project.UnitStatusReserved, project.UnitStatusPPJB)
	case project.UnitStatusSold:
		return chain(project.UnitStatusReserved, project.UnitStatusSold)
	case project.UnitStatusOccupied:
		return chain(project.UnitStatusReserved, project.UnitStatusSold, project.UnitStatusOccupied)
	case project.UnitStatusMaintenance:
		return chain(project.UnitStatusReserved, project.UnitStatusSold, project.UnitStatusOccupied, project.UnitStatusMaintenance)
	case project.UnitStatusHold:
		return chain(project.UnitStatusHold)
	case project.UnitStatusBlocked:
		return chain(project.UnitStatusBlocked)
	}
	return nil
}

// ── Phase tests ───────────────────────────────────────────────────────────────

func TestPhase_Create_Success(t *testing.T) {
	svc := newTestService()
	p, _ := svc.CreateProject(context.Background(), 1, project.CreateProjectRequest{Name: "LITHOS"})

	ph, err := svc.CreatePhase(context.Background(), 1, project.CreatePhaseRequest{
		ProjectID:   p.ID,
		Name:        "Fase 1",
		TargetUnits: 6,
	})
	if err != nil {
		t.Fatalf("CreatePhase: %v", err)
	}
	if ph.ID == 0 {
		t.Error("phase ID should be set")
	}
	if ph.ProjectID != p.ID {
		t.Errorf("phase.ProjectID = %d, want %d", ph.ProjectID, p.ID)
	}
	if ph.Status != project.PhaseStatusPlanning {
		t.Errorf("initial phase status should be planning, got %s", ph.Status)
	}
}

func TestPhase_StatusTransition_Legal(t *testing.T) {
	cases := []struct {
		from project.PhaseStatus
		to   project.PhaseStatus
	}{
		{project.PhaseStatusPlanning, project.PhaseStatusActive},
		{project.PhaseStatusActive, project.PhaseStatusCompleted},
	}
	for _, tc := range cases {
		svc := newTestService()
		p, _ := svc.CreateProject(context.Background(), 1, project.CreateProjectRequest{Name: "X"})
		ph, _ := svc.CreatePhase(context.Background(), 1, project.CreatePhaseRequest{ProjectID: p.ID, Name: "F"})

		if tc.from == project.PhaseStatusActive {
			if _, err := svc.TransitionPhase(context.Background(), 1, ph.ID, project.PhaseStatusActive); err != nil {
				t.Fatalf("setup to active: %v", err)
			}
		}

		updated, err := svc.TransitionPhase(context.Background(), 1, ph.ID, tc.to)
		if err != nil {
			t.Errorf("%s → %s should be legal, got %v", tc.from, tc.to, err)
			continue
		}
		if updated.Status != tc.to {
			t.Errorf("phase status = %s, want %s", updated.Status, tc.to)
		}
	}
}

func TestPhase_StatusTransition_Illegal(t *testing.T) {
	illegal := []struct {
		from project.PhaseStatus
		to   project.PhaseStatus
	}{
		{project.PhaseStatusPlanning, project.PhaseStatusCompleted},
		{project.PhaseStatusCompleted, project.PhaseStatusPlanning},
	}
	for _, tc := range illegal {
		svc := newTestService()
		p, _ := svc.CreateProject(context.Background(), 1, project.CreateProjectRequest{Name: "X"})
		ph, _ := svc.CreatePhase(context.Background(), 1, project.CreatePhaseRequest{ProjectID: p.ID, Name: "F"})

		// Advance to from state.
		if tc.from == project.PhaseStatusCompleted {
			_, _ = svc.TransitionPhase(context.Background(), 1, ph.ID, project.PhaseStatusActive)
			_, _ = svc.TransitionPhase(context.Background(), 1, ph.ID, project.PhaseStatusCompleted)
		}

		_, err := svc.TransitionPhase(context.Background(), 1, ph.ID, tc.to)
		if !errors.Is(err, project.ErrPhaseInvalidTransition) {
			t.Errorf("%s → %s should be illegal, got %v", tc.from, tc.to, err)
		}
	}
}

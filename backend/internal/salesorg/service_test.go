package salesorg_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"esaproperti/internal/salesorg"
)

type mockRepo struct {
	mu      sync.Mutex
	teams   map[uint64]*salesorg.SalesTeam
	persons map[uint64]*salesorg.SalesPerson
	tCodes  map[string]bool
	pCodes  map[string]bool
	nextID  uint64
}

func newMockRepo() *mockRepo {
	return &mockRepo{
		teams: map[uint64]*salesorg.SalesTeam{}, persons: map[uint64]*salesorg.SalesPerson{},
		tCodes: map[string]bool{}, pCodes: map[string]bool{},
	}
}

func ck(tenantID uint64, code string) string { return code + "@" + string(rune(tenantID)) }

func (m *mockRepo) CreateTeam(_ context.Context, t *salesorg.SalesTeam) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.tCodes[ck(t.TenantID, t.Code)] {
		return salesorg.ErrTeamCodeDup
	}
	m.nextID++
	t.ID = m.nextID
	m.tCodes[ck(t.TenantID, t.Code)] = true
	cp := *t
	m.teams[cp.ID] = &cp
	return nil
}

func (m *mockRepo) FindTeamByID(_ context.Context, tenantID, id uint64) (*salesorg.SalesTeam, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, ok := m.teams[id]
	if !ok || t.TenantID != tenantID {
		return nil, salesorg.ErrTeamNotFound
	}
	cp := *t
	return &cp, nil
}

func (m *mockRepo) ListTeams(_ context.Context, tenantID uint64) ([]*salesorg.SalesTeam, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []*salesorg.SalesTeam
	for _, t := range m.teams {
		if t.TenantID == tenantID {
			cp := *t
			out = append(out, &cp)
		}
	}
	return out, nil
}

func (m *mockRepo) UpdateTeam(_ context.Context, t *salesorg.SalesTeam) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	ex, ok := m.teams[t.ID]
	if !ok || ex.TenantID != t.TenantID {
		return salesorg.ErrTeamNotFound
	}
	cp := *t
	m.teams[cp.ID] = &cp
	return nil
}

func (m *mockRepo) CreatePerson(_ context.Context, p *salesorg.SalesPerson) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.pCodes[ck(p.TenantID, p.Code)] {
		return salesorg.ErrPersonCodeDup
	}
	m.nextID++
	p.ID = m.nextID
	p.CreatedAt, p.UpdatedAt = time.Now(), time.Now()
	m.pCodes[ck(p.TenantID, p.Code)] = true
	cp := *p
	m.persons[cp.ID] = &cp
	return nil
}

func (m *mockRepo) FindPersonByID(_ context.Context, tenantID, id uint64) (*salesorg.SalesPerson, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.persons[id]
	if !ok || p.TenantID != tenantID {
		return nil, salesorg.ErrPersonNotFound
	}
	cp := *p
	return &cp, nil
}

func (m *mockRepo) ListPersons(_ context.Context, tenantID uint64) ([]*salesorg.SalesPerson, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []*salesorg.SalesPerson
	for _, p := range m.persons {
		if p.TenantID == tenantID {
			cp := *p
			out = append(out, &cp)
		}
	}
	return out, nil
}

func (m *mockRepo) UpdatePerson(_ context.Context, p *salesorg.SalesPerson) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	ex, ok := m.persons[p.ID]
	if !ok || ex.TenantID != p.TenantID {
		return salesorg.ErrPersonNotFound
	}
	cp := *p
	m.persons[cp.ID] = &cp
	return nil
}

func newSvc() *salesorg.Service {
	r := newMockRepo()
	return salesorg.NewService(r, r)
}

func TestTeam_Create_And_Validation(t *testing.T) {
	svc := newSvc()
	ctx := context.Background()
	if _, err := svc.CreateTeam(ctx, 1, salesorg.CreateTeamRequest{Name: "X"}); !errors.Is(err, salesorg.ErrCodeRequired) {
		t.Errorf("tanpa code → ErrCodeRequired, got %v", err)
	}
	tm, err := svc.CreateTeam(ctx, 1, salesorg.CreateTeamRequest{Code: "TEAM-A", Name: "Tim A"})
	if err != nil {
		t.Fatalf("CreateTeam: %v", err)
	}
	if tm.ID == 0 || !tm.IsActive {
		t.Errorf("team baru harus ber-ID & aktif: %+v", tm)
	}
}

func TestPerson_Create_RequiresValidTeam(t *testing.T) {
	svc := newSvc()
	ctx := context.Background()
	// Team tidak ada → ErrTeamNotFound.
	badTeam := uint64(999)
	if _, err := svc.CreatePerson(ctx, 1, salesorg.CreatePersonRequest{Code: "SP1", Name: "Sari", SalesTeamID: &badTeam}); !errors.Is(err, salesorg.ErrTeamNotFound) {
		t.Errorf("team invalid → ErrTeamNotFound, got %v", err)
	}
	// Team valid → sukses.
	tm, _ := svc.CreateTeam(ctx, 1, salesorg.CreateTeamRequest{Code: "T", Name: "Tim"})
	p, err := svc.CreatePerson(ctx, 1, salesorg.CreatePersonRequest{Code: "SP1", Name: "Sari", SalesTeamID: &tm.ID})
	if err != nil {
		t.Fatalf("CreatePerson: %v", err)
	}
	if p.SalesTeamID == nil || *p.SalesTeamID != tm.ID {
		t.Errorf("person harus tertaut team %d", tm.ID)
	}
}

func TestPerson_TenantIsolation(t *testing.T) {
	svc := newSvc()
	ctx := context.Background()
	p, _ := svc.CreatePerson(ctx, 1, salesorg.CreatePersonRequest{Code: "SP", Name: "A"})
	if _, err := svc.GetPerson(ctx, 2, p.ID); !errors.Is(err, salesorg.ErrPersonNotFound) {
		t.Errorf("lintas tenant → ErrPersonNotFound, got %v", err)
	}
}

func TestPerson_CrossTenantTeamRejected(t *testing.T) {
	svc := newSvc()
	ctx := context.Background()
	// Team milik tenant 1, person tenant 2 tak boleh menautkannya.
	tm, _ := svc.CreateTeam(ctx, 1, salesorg.CreateTeamRequest{Code: "T", Name: "Tim"})
	if _, err := svc.CreatePerson(ctx, 2, salesorg.CreatePersonRequest{Code: "SP", Name: "A", SalesTeamID: &tm.ID}); !errors.Is(err, salesorg.ErrTeamNotFound) {
		t.Errorf("team lintas tenant → ErrTeamNotFound, got %v", err)
	}
}

package salesorg

import "context"

type TeamStore interface {
	CreateTeam(ctx context.Context, t *SalesTeam) error
	FindTeamByID(ctx context.Context, tenantID, id uint64) (*SalesTeam, error)
	ListTeams(ctx context.Context, tenantID uint64) ([]*SalesTeam, error)
	UpdateTeam(ctx context.Context, t *SalesTeam) error
}

type PersonStore interface {
	CreatePerson(ctx context.Context, p *SalesPerson) error
	FindPersonByID(ctx context.Context, tenantID, id uint64) (*SalesPerson, error)
	ListPersons(ctx context.Context, tenantID uint64) ([]*SalesPerson, error)
	UpdatePerson(ctx context.Context, p *SalesPerson) error
}

type Service struct {
	teams   TeamStore
	persons PersonStore
}

func NewService(teams TeamStore, persons PersonStore) *Service {
	return &Service{teams: teams, persons: persons}
}

// ── Team ───────────────────────────────────────────────────────────────────────

func (s *Service) CreateTeam(ctx context.Context, tenantID uint64, req CreateTeamRequest) (*SalesTeam, error) {
	if req.Code == "" {
		return nil, ErrCodeRequired
	}
	if req.Name == "" {
		return nil, ErrNameRequired
	}
	t := &SalesTeam{
		TenantID: tenantID, Code: req.Code, Name: req.Name,
		LeaderSalesPersonID: req.LeaderSalesPersonID, IsActive: true,
	}
	if err := s.teams.CreateTeam(ctx, t); err != nil {
		return nil, err
	}
	return t, nil
}

func (s *Service) GetTeam(ctx context.Context, tenantID, id uint64) (*SalesTeam, error) {
	return s.teams.FindTeamByID(ctx, tenantID, id)
}

func (s *Service) ListTeams(ctx context.Context, tenantID uint64) ([]*SalesTeam, error) {
	return s.teams.ListTeams(ctx, tenantID)
}

func (s *Service) UpdateTeam(ctx context.Context, tenantID, id uint64, req UpdateTeamRequest) (*SalesTeam, error) {
	t, err := s.teams.FindTeamByID(ctx, tenantID, id)
	if err != nil {
		return nil, err
	}
	if req.Name != "" {
		t.Name = req.Name
	}
	if req.LeaderSalesPersonID != nil {
		t.LeaderSalesPersonID = req.LeaderSalesPersonID
	}
	if req.IsActive != nil {
		t.IsActive = *req.IsActive
	}
	if err := s.teams.UpdateTeam(ctx, t); err != nil {
		return nil, err
	}
	return t, nil
}

// ── Person ─────────────────────────────────────────────────────────────────────

func (s *Service) CreatePerson(ctx context.Context, tenantID uint64, req CreatePersonRequest) (*SalesPerson, error) {
	if req.Code == "" {
		return nil, ErrCodeRequired
	}
	if req.Name == "" {
		return nil, ErrNameRequired
	}
	// Integritas: bila menunjuk team, team wajib ada & satu tenant.
	if req.SalesTeamID != nil {
		if _, err := s.teams.FindTeamByID(ctx, tenantID, *req.SalesTeamID); err != nil {
			return nil, err
		}
	}
	p := &SalesPerson{
		TenantID: tenantID, Code: req.Code, Name: req.Name,
		SalesTeamID: req.SalesTeamID, UserID: req.UserID,
		Phone: req.Phone, Email: req.Email, JoinDate: req.JoinDate, IsActive: true,
	}
	if err := s.persons.CreatePerson(ctx, p); err != nil {
		return nil, err
	}
	return p, nil
}

func (s *Service) GetPerson(ctx context.Context, tenantID, id uint64) (*SalesPerson, error) {
	return s.persons.FindPersonByID(ctx, tenantID, id)
}

func (s *Service) ListPersons(ctx context.Context, tenantID uint64) ([]*SalesPerson, error) {
	return s.persons.ListPersons(ctx, tenantID)
}

func (s *Service) UpdatePerson(ctx context.Context, tenantID, id uint64, req UpdatePersonRequest) (*SalesPerson, error) {
	p, err := s.persons.FindPersonByID(ctx, tenantID, id)
	if err != nil {
		return nil, err
	}
	if req.SalesTeamID != nil {
		if _, err := s.teams.FindTeamByID(ctx, tenantID, *req.SalesTeamID); err != nil {
			return nil, err
		}
		p.SalesTeamID = req.SalesTeamID
	}
	if req.Name != "" {
		p.Name = req.Name
	}
	if req.Phone != "" {
		p.Phone = req.Phone
	}
	if req.Email != "" {
		p.Email = req.Email
	}
	if req.JoinDate != nil {
		p.JoinDate = req.JoinDate
	}
	if req.IsActive != nil {
		p.IsActive = *req.IsActive
	}
	if err := s.persons.UpdatePerson(ctx, p); err != nil {
		return nil, err
	}
	return p, nil
}

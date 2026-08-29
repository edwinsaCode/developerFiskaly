package customer

import "context"

// Store adalah kontrak persistensi (interface → memungkinkan unit test in-memory).
type Store interface {
	Create(ctx context.Context, c *Customer) error
	FindByID(ctx context.Context, tenantID, id uint64) (*Customer, error)
	List(ctx context.Context, tenantID uint64) ([]*Customer, error)
	Update(ctx context.Context, c *Customer) error
}

type Service struct {
	store Store
}

func NewService(store Store) *Service {
	return &Service{store: store}
}

func (s *Service) Create(ctx context.Context, tenantID uint64, req CreateCustomerRequest) (*Customer, error) {
	if req.Code == "" {
		return nil, ErrCustomerCodeRequired
	}
	if req.Name == "" {
		return nil, ErrCustomerNameRequired
	}
	typ := req.Type
	if typ == "" {
		typ = CustomerTypeIndividual
	}
	if !typ.Valid() {
		return nil, ErrCustomerTypeInvalid
	}
	c := &Customer{
		TenantID:  tenantID,
		Code:      req.Code,
		Name:      req.Name,
		Type:      typ,
		Segment:   req.Segment,
		IDNumber:  req.IDNumber,
		NPWP:      req.NPWP,
		Phone:     req.Phone,
		Email:     req.Email,
		Address:   req.Address,
		IsActive:  true,
		CreatedBy: req.CreatedBy,
	}
	if err := s.store.Create(ctx, c); err != nil {
		return nil, err
	}
	return c, nil
}

func (s *Service) Get(ctx context.Context, tenantID, id uint64) (*Customer, error) {
	return s.store.FindByID(ctx, tenantID, id)
}

func (s *Service) List(ctx context.Context, tenantID uint64) ([]*Customer, error) {
	return s.store.List(ctx, tenantID)
}

// Update: hanya field yang diisi yang diubah (string kosong/IsActive nil = skip).
func (s *Service) Update(ctx context.Context, tenantID, id uint64, req UpdateCustomerRequest) (*Customer, error) {
	c, err := s.store.FindByID(ctx, tenantID, id)
	if err != nil {
		return nil, err
	}
	if req.Name != "" {
		c.Name = req.Name
	}
	if req.Type != "" {
		if !req.Type.Valid() {
			return nil, ErrCustomerTypeInvalid
		}
		c.Type = req.Type
	}
	if req.Segment != "" {
		c.Segment = req.Segment
	}
	if req.IDNumber != "" {
		c.IDNumber = req.IDNumber
	}
	if req.NPWP != "" {
		c.NPWP = req.NPWP
	}
	if req.Phone != "" {
		c.Phone = req.Phone
	}
	if req.Email != "" {
		c.Email = req.Email
	}
	if req.Address != "" {
		c.Address = req.Address
	}
	if req.IsActive != nil {
		c.IsActive = *req.IsActive
	}
	if err := s.store.Update(ctx, c); err != nil {
		return nil, err
	}
	return c, nil
}

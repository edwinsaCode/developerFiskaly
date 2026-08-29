package customer_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"esaproperti/internal/customer"
)

// In-memory mock store (map + mutex), meniru semantik value & isolasi tenant.
type mockStore struct {
	mu     sync.Mutex
	items  map[uint64]*customer.Customer
	codes  map[string]bool // "tenant:code" untuk uji duplikat
	nextID uint64
}

func newMockStore() *mockStore {
	return &mockStore{items: make(map[uint64]*customer.Customer), codes: make(map[string]bool)}
}

func key(tenantID uint64, code string) string { return code + "@" + string(rune(tenantID)) }

func (m *mockStore) Create(_ context.Context, c *customer.Customer) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	k := key(c.TenantID, c.Code)
	if m.codes[k] {
		return customer.ErrCustomerCodeDuplicate
	}
	m.nextID++
	c.ID = m.nextID
	c.CreatedAt, c.UpdatedAt = time.Now(), time.Now()
	m.codes[k] = true
	cp := *c
	m.items[cp.ID] = &cp
	return nil
}

func (m *mockStore) FindByID(_ context.Context, tenantID, id uint64) (*customer.Customer, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, ok := m.items[id]
	if !ok || c.TenantID != tenantID { // isolasi tenant (Invariant #6)
		return nil, customer.ErrCustomerNotFound
	}
	cp := *c
	return &cp, nil
}

func (m *mockStore) List(_ context.Context, tenantID uint64) ([]*customer.Customer, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []*customer.Customer
	for _, c := range m.items {
		if c.TenantID == tenantID {
			cp := *c
			out = append(out, &cp)
		}
	}
	return out, nil
}

func (m *mockStore) Update(_ context.Context, c *customer.Customer) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	ex, ok := m.items[c.ID]
	if !ok || ex.TenantID != c.TenantID {
		return customer.ErrCustomerNotFound
	}
	cp := *c
	cp.UpdatedAt = time.Now()
	m.items[cp.ID] = &cp
	return nil
}

func newSvc() *customer.Service { return customer.NewService(newMockStore()) }

func TestCustomer_Create_Success(t *testing.T) {
	svc := newSvc()
	c, err := svc.Create(context.Background(), 1, customer.CreateCustomerRequest{
		Code: "CUST-001", Name: "Budi Santoso",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if c.ID == 0 {
		t.Error("ID harus terisi")
	}
	if c.Type != customer.CustomerTypeIndividual {
		t.Errorf("default type harus individual, got %s", c.Type)
	}
	if !c.IsActive {
		t.Error("customer baru harus aktif")
	}
}

func TestCustomer_Create_Validation(t *testing.T) {
	svc := newSvc()
	if _, err := svc.Create(context.Background(), 1, customer.CreateCustomerRequest{Name: "X"}); !errors.Is(err, customer.ErrCustomerCodeRequired) {
		t.Errorf("tanpa code harus ErrCustomerCodeRequired, got %v", err)
	}
	if _, err := svc.Create(context.Background(), 1, customer.CreateCustomerRequest{Code: "C1"}); !errors.Is(err, customer.ErrCustomerNameRequired) {
		t.Errorf("tanpa name harus ErrCustomerNameRequired, got %v", err)
	}
	if _, err := svc.Create(context.Background(), 1, customer.CreateCustomerRequest{Code: "C1", Name: "X", Type: "alien"}); !errors.Is(err, customer.ErrCustomerTypeInvalid) {
		t.Errorf("type invalid harus ErrCustomerTypeInvalid, got %v", err)
	}
}

func TestCustomer_Create_DuplicateCode(t *testing.T) {
	svc := newSvc()
	ctx := context.Background()
	if _, err := svc.Create(ctx, 1, customer.CreateCustomerRequest{Code: "DUP", Name: "A"}); err != nil {
		t.Fatalf("create 1: %v", err)
	}
	if _, err := svc.Create(ctx, 1, customer.CreateCustomerRequest{Code: "DUP", Name: "B"}); !errors.Is(err, customer.ErrCustomerCodeDuplicate) {
		t.Errorf("kode duplikat harus ErrCustomerCodeDuplicate, got %v", err)
	}
}

func TestCustomer_TenantIsolation(t *testing.T) {
	svc := newSvc()
	ctx := context.Background()
	c, _ := svc.Create(ctx, 1, customer.CreateCustomerRequest{Code: "T1", Name: "A"})
	// Tenant 2 tidak boleh melihat customer tenant 1.
	if _, err := svc.Get(ctx, 2, c.ID); !errors.Is(err, customer.ErrCustomerNotFound) {
		t.Errorf("lintas tenant harus ErrCustomerNotFound, got %v", err)
	}
}

func TestCustomer_Update_Partial(t *testing.T) {
	svc := newSvc()
	ctx := context.Background()
	c, _ := svc.Create(ctx, 1, customer.CreateCustomerRequest{Code: "U1", Name: "Awal", Phone: "0811", Segment: "komersial"})
	if c.Segment != "komersial" {
		t.Errorf("segment harus tersimpan saat create, got %q", c.Segment)
	}
	inactive := false
	upd, err := svc.Update(ctx, 1, c.ID, customer.UpdateCustomerRequest{Name: "Baru", Segment: "subsidi", IsActive: &inactive})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if upd.Name != "Baru" {
		t.Errorf("name harus terupdate, got %s", upd.Name)
	}
	if upd.Code != "U1" {
		t.Errorf("code IMMUTABLE — harus tetap U1, got %s", upd.Code)
	}
	if upd.Segment != "subsidi" {
		t.Errorf("segment harus terupdate, got %s", upd.Segment)
	}
	if upd.Phone != "0811" {
		t.Errorf("phone tak dikirim = tak berubah, got %s", upd.Phone)
	}
	if upd.IsActive {
		t.Error("IsActive harus false")
	}
}

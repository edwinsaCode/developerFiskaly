package land_test

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"esaproperti/internal/domain"
	"esaproperti/internal/land"
	"esaproperti/internal/receivable"
)

type mockStore struct {
	mu                sync.Mutex
	byKey             map[[2]uint64]*land.LandStock
	nextID            uint64
	created           *land.LandStock
	reservations      map[uint64]*land.LandStockReservation
	nextReservationID uint64

	// LT-5
	accounts       map[string]uint64
	accountErrs    map[string]error
	landSales      map[uint64]*land.LandSale
	allocations    []*land.LandAllocation
	nextSaleID     uint64
	receivableRows []land.LandSaleReceivable
}

func newMockStore() *mockStore {
	return &mockStore{
		byKey:        map[[2]uint64]*land.LandStock{},
		reservations: map[uint64]*land.LandStockReservation{},
		accounts: map[string]uint64{
			"1-1000": 101, // kas — akun pembayaran default di test
			"4-1100": 102,
			"2-3000": 103,
			"5-1000": 104,
			"1-3000": 105,
		},
		accountErrs: map[string]error{},
		landSales:   map[uint64]*land.LandSale{},
	}
}

func (m *mockStore) poolByID(tenantID, id uint64) *land.LandStock {
	for key, pool := range m.byKey {
		if key[0] == tenantID && pool.ID == id {
			return pool
		}
	}
	return nil
}

func (m *mockStore) Reserve(_ context.Context, tenantID, landStockID uint64, in land.ReserveInput) (*land.LandStockReservation, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	pool := m.poolByID(tenantID, landStockID)
	if pool == nil {
		return nil, land.ErrLandStockNotFound
	}
	if in.QuantityM2.GreaterThan(pool.AvailableQuantityM2()) {
		return nil, land.ErrCapacityExceeded
	}
	m.nextReservationID++
	res := &land.LandStockReservation{
		ID:                m.nextReservationID,
		TenantID:          tenantID,
		LandStockID:       landStockID,
		ProjectID:         in.ProjectID,
		CustomerID:        in.CustomerID,
		SalesPersonID:     in.SalesPersonID,
		QuantityM2:        in.QuantityM2,
		UnitPriceSnapshot: pool.UnitPrice,
		ReservedAt:        in.ReservedAt,
		ExpiryDate:        in.ExpiryDate,
		Status:            land.ReservationStatusActive,
		CreatedBy:         in.CreatedBy,
	}
	pool.ReservedQuantityM2 = pool.ReservedQuantityM2.Add(in.QuantityM2)
	cp := *res
	m.reservations[res.ID] = &cp
	out := *res
	return &out, nil
}

func (m *mockStore) FindReservation(_ context.Context, tenantID, id uint64) (*land.LandStockReservation, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	res, ok := m.reservations[id]
	if !ok || res.TenantID != tenantID {
		return nil, land.ErrReservationNotFound
	}
	cp := *res
	return &cp, nil
}

func (m *mockStore) ListReservationsByProject(_ context.Context, tenantID, projectID uint64) ([]land.LandStockReservation, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []land.LandStockReservation
	for _, res := range m.reservations {
		if res.TenantID == tenantID && res.ProjectID == projectID {
			out = append(out, *res)
		}
	}
	return out, nil
}

func (m *mockStore) ListExpiredReservationIDs(_ context.Context, tenantID uint64, asOf time.Time) ([]uint64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var ids []uint64
	for _, res := range m.reservations {
		if res.TenantID == tenantID && res.Status == land.ReservationStatusActive &&
			res.ExpiryDate != nil && !res.ExpiryDate.After(asOf) {
			ids = append(ids, res.ID)
		}
	}
	return ids, nil
}

func (m *mockStore) CloseReservation(_ context.Context, tenantID, id uint64, status land.ReservationStatus, reason string) (*land.LandStockReservation, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	res, ok := m.reservations[id]
	if !ok || res.TenantID != tenantID {
		return nil, land.ErrReservationNotFound
	}
	if res.Status != land.ReservationStatusActive {
		return nil, land.ErrReservationNotActive
	}
	pool := m.poolByID(tenantID, res.LandStockID)
	if pool != nil {
		pool.ReservedQuantityM2 = pool.ReservedQuantityM2.Sub(res.QuantityM2)
	}
	res.Status = status
	res.CancelledReason = reason
	cp := *res
	return &cp, nil
}

func (m *mockStore) CreatePool(_ context.Context, pool *land.LandStock) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := [2]uint64{pool.TenantID, pool.ProjectID}
	if _, ok := m.byKey[key]; ok {
		return land.ErrLandStockAlreadyExists
	}
	m.nextID++
	pool.ID = m.nextID
	cp := *pool
	m.byKey[key] = &cp
	m.created = &cp
	return nil
}

func (m *mockStore) FindPoolByProject(_ context.Context, tenantID, projectID uint64) (*land.LandStock, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	pool, ok := m.byKey[[2]uint64{tenantID, projectID}]
	if !ok {
		return nil, land.ErrLandStockNotFound
	}
	cp := *pool
	return &cp, nil
}

func (m *mockStore) UpdatePoolQuantityAndPrice(_ context.Context, tenantID, id uint64, totalQuantityM2 decimal.Decimal, unitPrice, purchasePrice domain.Money) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for key, pool := range m.byKey {
		if key[0] == tenantID && pool.ID == id {
			pool.TotalQuantityM2 = totalQuantityM2
			pool.UnitPrice = unitPrice
			pool.PurchasePrice = purchasePrice
			return nil
		}
	}
	return land.ErrLandStockNotFound
}

// ── LT-5 mock methods ────────────────────────────────────────────────────

func (m *mockStore) RecordAkad(_ context.Context, tenantID uint64, in land.RecordAkadParams) (*land.LandSale, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	pool := m.poolByID(tenantID, in.LandStockID)
	if pool == nil {
		return nil, land.ErrLandStockNotFound
	}
	if in.ReservationID != nil {
		res, ok := m.reservations[*in.ReservationID]
		if !ok || res.TenantID != tenantID {
			return nil, land.ErrReservationNotFound
		}
		if res.Status != land.ReservationStatusActive {
			return nil, land.ErrReservationNotActive
		}
		if res.CustomerID != in.CustomerID {
			return nil, land.ErrReservationCustomerMismatch
		}
		if !res.QuantityM2.Equal(in.QuantityM2) {
			return nil, land.ErrReservationQuantityMismatch
		}
		pool.ReservedQuantityM2 = pool.ReservedQuantityM2.Sub(res.QuantityM2)
	} else if in.QuantityM2.GreaterThan(pool.AvailableQuantityM2()) {
		return nil, land.ErrCapacityExceeded
	}
	pool.SoldQuantityM2 = pool.SoldQuantityM2.Add(in.QuantityM2)

	m.nextSaleID++
	recogDate := in.RecognitionDate
	sale := &land.LandSale{
		ID:                 m.nextSaleID,
		TenantID:           tenantID,
		ProjectID:          in.ProjectID,
		LandStockID:        in.LandStockID,
		ReservationID:      in.ReservationID,
		CustomerID:         in.CustomerID,
		SalesPersonID:      in.SalesPersonID,
		QuantityM2:         in.QuantityM2,
		UnitPriceSnapshot:  in.UnitPriceSnapshot,
		DPPAmount:          in.DPPAmount,
		IsPKP:              in.IsPKP,
		VATRateSnapshot:    in.VATRateSnapshot,
		GrossAmount:        in.GrossAmount,
		PaymentAccountCode: in.PaymentAccountCode,
		RecognitionDate:    &recogDate,
		Status:             land.LandSaleStatusAkad,
		CreatedBy:          in.CreatedBy,
	}
	m.landSales[sale.ID] = sale

	if in.ReservationID != nil {
		res := m.reservations[*in.ReservationID]
		res.Status = land.ReservationStatusConverted
		saleID := sale.ID
		res.ConvertedSaleID = &saleID
	}

	m.allocations = append(m.allocations, &land.LandAllocation{
		ID:                        uint64(len(m.allocations) + 1),
		TenantID:                  tenantID,
		ProjectID:                 in.ProjectID,
		LandSaleID:                sale.ID,
		LandStockID:               in.LandStockID,
		QuantityM2:                in.QuantityM2,
		HPPRatePerM2Snapshot:      in.HPPRatePerM2,
		HPPTotal:                  in.HPPTotal,
		Basis:                     in.Basis,
		AllocationConfigVersionID: in.AllocationConfigVersionID,
		AllocationConfigVersion:   in.AllocationConfigVersion,
		BudgetPlanID:              in.BudgetPlanID,
		BudgetPlanVersion:         in.BudgetPlanVersion,
	})

	out := *sale
	return &out, nil
}

func (m *mockStore) CancelLandSale(_ context.Context, tenantID, id uint64, in land.CancelLandSaleParams) (*land.LandSale, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	sale, ok := m.landSales[id]
	if !ok || sale.TenantID != tenantID {
		return nil, land.ErrLandSaleNotFound
	}
	if sale.Status != land.LandSaleStatusAkad {
		return nil, land.ErrLandSaleNotAkad
	}
	pool := m.poolByID(tenantID, sale.LandStockID)
	if pool == nil {
		return nil, land.ErrLandStockNotFound
	}
	pool.SoldQuantityM2 = pool.SoldQuantityM2.Sub(sale.QuantityM2)

	now := in.CancelDate
	sale.Status = land.LandSaleStatusCancelled
	sale.CancelledAt = &now
	sale.CancelReason = in.Reason
	sale.CancelledBy = in.CancelledBy
	cp := *sale
	return &cp, nil
}

func (m *mockStore) FindLandSale(_ context.Context, tenantID, id uint64) (*land.LandSale, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	sale, ok := m.landSales[id]
	if !ok || sale.TenantID != tenantID {
		return nil, land.ErrLandSaleNotFound
	}
	cp := *sale
	return &cp, nil
}

func (m *mockStore) ListLandSalesByProject(_ context.Context, tenantID, projectID uint64) ([]land.LandSale, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []land.LandSale
	for _, sale := range m.landSales {
		if sale.TenantID == tenantID && sale.ProjectID == projectID {
			out = append(out, *sale)
		}
	}
	return out, nil
}

func (m *mockStore) ListReceivableLandSales(_ context.Context, _ uint64) ([]land.LandSaleReceivable, error) {
	return m.receivableRows, nil
}

func (m *mockStore) FindAccountIDByCode(_ context.Context, _ uint64, code string) (uint64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if id, ok := m.accounts[code]; ok {
		return id, nil
	}
	return 0, fmt.Errorf("akun %q tidak ditemukan", code)
}

func (m *mockStore) ValidateCashBankAccount(_ context.Context, _ uint64, code string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err, ok := m.accountErrs[code]; ok {
		return err
	}
	if _, ok := m.accounts[code]; ok {
		return nil
	}
	return land.ErrPaymentAccountNotFound
}

// stubHPPResolver is a configurable fake land.LandHPPResolver.
type stubHPPResolver struct {
	res land.LandHPPResolution
	err error
}

func (r *stubHPPResolver) ResolveLandHPPRate(_ context.Context, _, _ uint64) (land.LandHPPResolution, error) {
	return r.res, r.err
}

func newTestService() (*land.Service, *mockStore) {
	store := newMockStore()
	return land.NewService(store), store
}

// newTestServiceWithAkad wires a stub HPP resolver (fixed rate, no error) so
// RecordAkad's fail-closed ErrHPPResolverNotConfigured guard doesn't block
// Akad-focused tests.
func newTestServiceWithAkad(t *testing.T) (*land.Service, *mockStore, *stubHPPResolver) {
	t.Helper()
	store := newMockStore()
	resolver := &stubHPPResolver{res: land.LandHPPResolution{
		RatePerM2: mustMoney(t, "500000"),
		Method:    land.HPPMethodActual,
	}}
	return land.NewService(store, land.WithHPPResolver(resolver)), store, resolver
}

func mustMoney(t *testing.T, s string) domain.Money {
	t.Helper()
	m, err := domain.NewMoney(s)
	if err != nil {
		t.Fatalf("NewMoney(%q): %v", s, err)
	}
	return m
}

func TestCreatePool_Success(t *testing.T) {
	svc, _ := newTestService()
	pool, err := svc.CreatePool(context.Background(), 1, land.CreatePoolRequest{
		ProjectID:       10,
		TotalQuantityM2: decimal.NewFromInt(500),
		UnitPrice:       mustMoney(t, "1000000"),
	})
	if err != nil {
		t.Fatalf("CreatePool: %v", err)
	}
	if pool.ProductCode != "kelebihan_tanah" {
		t.Errorf("product_code want kelebihan_tanah, got %s", pool.ProductCode)
	}
	if !pool.TotalQuantityM2.Equal(decimal.NewFromInt(500)) {
		t.Errorf("total_quantity_m2 want 500, got %s", pool.TotalQuantityM2)
	}
	if !pool.AvailableQuantityM2().Equal(decimal.NewFromInt(500)) {
		t.Errorf("available want 500, got %s", pool.AvailableQuantityM2())
	}
}

func TestCreatePool_DuplicateRejected(t *testing.T) {
	svc, _ := newTestService()
	req := land.CreatePoolRequest{ProjectID: 10, TotalQuantityM2: decimal.NewFromInt(500), UnitPrice: mustMoney(t, "1000000")}
	if _, err := svc.CreatePool(context.Background(), 1, req); err != nil {
		t.Fatalf("first CreatePool: %v", err)
	}
	_, err := svc.CreatePool(context.Background(), 1, req)
	if err != land.ErrLandStockAlreadyExists {
		t.Fatalf("want ErrLandStockAlreadyExists, got %v", err)
	}
}

func TestCreatePool_NegativeQuantityRejected(t *testing.T) {
	svc, _ := newTestService()
	_, err := svc.CreatePool(context.Background(), 1, land.CreatePoolRequest{
		ProjectID: 10, TotalQuantityM2: decimal.NewFromInt(-1), UnitPrice: mustMoney(t, "0"),
	})
	if err != land.ErrQuantityNegative {
		t.Fatalf("want ErrQuantityNegative, got %v", err)
	}
}

func TestCreatePool_NegativePriceRejected(t *testing.T) {
	svc, _ := newTestService()
	_, err := svc.CreatePool(context.Background(), 1, land.CreatePoolRequest{
		ProjectID: 10, TotalQuantityM2: decimal.NewFromInt(1), UnitPrice: mustMoney(t, "-1"),
	})
	if err != land.ErrQuantityNegative {
		t.Fatalf("want ErrQuantityNegative, got %v", err)
	}
}

func TestGetPool_NotFound(t *testing.T) {
	svc, _ := newTestService()
	_, err := svc.GetPool(context.Background(), 1, 999)
	if err != land.ErrLandStockNotFound {
		t.Fatalf("want ErrLandStockNotFound, got %v", err)
	}
}

func TestUpdatePool_Success(t *testing.T) {
	svc, _ := newTestService()
	_, err := svc.CreatePool(context.Background(), 1, land.CreatePoolRequest{
		ProjectID: 10, TotalQuantityM2: decimal.NewFromInt(500), UnitPrice: mustMoney(t, "1000000"),
	})
	if err != nil {
		t.Fatalf("CreatePool: %v", err)
	}
	updated, err := svc.UpdatePool(context.Background(), 1, 10, land.UpdatePoolRequest{
		TotalQuantityM2: decimal.NewFromInt(800), UnitPrice: mustMoney(t, "1200000"),
	})
	if err != nil {
		t.Fatalf("UpdatePool: %v", err)
	}
	if !updated.TotalQuantityM2.Equal(decimal.NewFromInt(800)) {
		t.Errorf("total_quantity_m2 want 800, got %s", updated.TotalQuantityM2)
	}
	if !updated.UnitPrice.Equal(mustMoney(t, "1200000")) {
		t.Errorf("unit_price want 1200000, got %s", updated.UnitPrice)
	}
}

func TestUpdatePool_CapacityExceededRejected(t *testing.T) {
	svc, store := newTestService()
	if _, err := svc.CreatePool(context.Background(), 1, land.CreatePoolRequest{
		ProjectID: 10, TotalQuantityM2: decimal.NewFromInt(500), UnitPrice: mustMoney(t, "1000000"),
	}); err != nil {
		t.Fatalf("CreatePool: %v", err)
	}
	// simulate committed reserved+sold directly on the mock's stored pool
	store.mu.Lock()
	store.byKey[[2]uint64{1, 10}].ReservedQuantityM2 = decimal.NewFromInt(300)
	store.byKey[[2]uint64{1, 10}].SoldQuantityM2 = decimal.NewFromInt(150)
	store.mu.Unlock()

	_, err := svc.UpdatePool(context.Background(), 1, 10, land.UpdatePoolRequest{
		TotalQuantityM2: decimal.NewFromInt(400), UnitPrice: mustMoney(t, "1000000"),
	})
	if err != land.ErrCapacityExceeded {
		t.Fatalf("want ErrCapacityExceeded, got %v", err)
	}
}

func TestUpdatePool_NotFound(t *testing.T) {
	svc, _ := newTestService()
	_, err := svc.UpdatePool(context.Background(), 1, 999, land.UpdatePoolRequest{
		TotalQuantityM2: decimal.NewFromInt(1), UnitPrice: mustMoney(t, "1"),
	})
	if err != land.ErrLandStockNotFound {
		t.Fatalf("want ErrLandStockNotFound, got %v", err)
	}
}

func TestUpdatePool_NegativeRejected(t *testing.T) {
	svc, _ := newTestService()
	if _, err := svc.CreatePool(context.Background(), 1, land.CreatePoolRequest{
		ProjectID: 10, TotalQuantityM2: decimal.NewFromInt(500), UnitPrice: mustMoney(t, "1000000"),
	}); err != nil {
		t.Fatalf("CreatePool: %v", err)
	}
	_, err := svc.UpdatePool(context.Background(), 1, 10, land.UpdatePoolRequest{
		TotalQuantityM2: decimal.NewFromInt(-1), UnitPrice: mustMoney(t, "1000000"),
	})
	if err != land.ErrQuantityNegative {
		t.Fatalf("want ErrQuantityNegative, got %v", err)
	}
}

// ── Reservasi (LT-4) ────────────────────────────────────────────────────────

func seedPool(t *testing.T, svc *land.Service, tenantID, projectID uint64, total string) {
	t.Helper()
	_, err := svc.CreatePool(context.Background(), tenantID, land.CreatePoolRequest{
		ProjectID: projectID, TotalQuantityM2: mustDecimal(t, total), UnitPrice: mustMoney(t, "1000000"),
	})
	if err != nil {
		t.Fatalf("seed CreatePool: %v", err)
	}
}

func mustDecimal(t *testing.T, s string) decimal.Decimal {
	t.Helper()
	d, err := decimal.NewFromString(s)
	if err != nil {
		t.Fatalf("decimal.NewFromString(%q): %v", s, err)
	}
	return d
}

func TestReserve_Success(t *testing.T) {
	svc, _ := newTestService()
	seedPool(t, svc, 1, 10, "500")

	res, err := svc.Reserve(context.Background(), 1, land.ReserveRequest{
		ProjectID: 10, CustomerID: 42, QuantityM2: mustDecimal(t, "100"), ReservedAt: time.Now(),
	})
	if err != nil {
		t.Fatalf("Reserve: %v", err)
	}
	if res.Status != land.ReservationStatusActive {
		t.Errorf("status want active, got %s", res.Status)
	}
	if !res.UnitPriceSnapshot.Equal(mustMoney(t, "1000000")) {
		t.Errorf("unit_price_snapshot want 1000000, got %s", res.UnitPriceSnapshot)
	}

	pool, err := svc.GetPool(context.Background(), 1, 10)
	if err != nil {
		t.Fatalf("GetPool: %v", err)
	}
	if !pool.AvailableQuantityM2().Equal(mustDecimal(t, "400")) {
		t.Errorf("available want 400, got %s", pool.AvailableQuantityM2())
	}
}

func TestReserve_ExceedsAvailableRejected(t *testing.T) {
	svc, _ := newTestService()
	seedPool(t, svc, 1, 10, "500")

	_, err := svc.Reserve(context.Background(), 1, land.ReserveRequest{
		ProjectID: 10, CustomerID: 42, QuantityM2: mustDecimal(t, "600"), ReservedAt: time.Now(),
	})
	if err != land.ErrCapacityExceeded {
		t.Fatalf("want ErrCapacityExceeded, got %v", err)
	}
}

func TestReserve_TwoReservationsCombinedExceedAvailable(t *testing.T) {
	svc, _ := newTestService()
	seedPool(t, svc, 1, 10, "500")

	if _, err := svc.Reserve(context.Background(), 1, land.ReserveRequest{
		ProjectID: 10, CustomerID: 1, QuantityM2: mustDecimal(t, "300"), ReservedAt: time.Now(),
	}); err != nil {
		t.Fatalf("first Reserve: %v", err)
	}
	// Sisa 200 — reservasi kedua minta 300, harus ditolak.
	_, err := svc.Reserve(context.Background(), 1, land.ReserveRequest{
		ProjectID: 10, CustomerID: 2, QuantityM2: mustDecimal(t, "300"), ReservedAt: time.Now(),
	})
	if err != land.ErrCapacityExceeded {
		t.Fatalf("want ErrCapacityExceeded, got %v", err)
	}
}

func TestReserve_ZeroQuantityRejected(t *testing.T) {
	svc, _ := newTestService()
	seedPool(t, svc, 1, 10, "500")
	_, err := svc.Reserve(context.Background(), 1, land.ReserveRequest{
		ProjectID: 10, CustomerID: 1, QuantityM2: decimal.Zero, ReservedAt: time.Now(),
	})
	if err != land.ErrQuantityMustBePositive {
		t.Fatalf("want ErrQuantityMustBePositive, got %v", err)
	}
}

func TestReserve_NoCustomerRejected(t *testing.T) {
	svc, _ := newTestService()
	seedPool(t, svc, 1, 10, "500")
	_, err := svc.Reserve(context.Background(), 1, land.ReserveRequest{
		ProjectID: 10, QuantityM2: mustDecimal(t, "10"), ReservedAt: time.Now(),
	})
	if err != land.ErrCustomerRequired {
		t.Fatalf("want ErrCustomerRequired, got %v", err)
	}
}

func TestReserve_NoPoolRejected(t *testing.T) {
	svc, _ := newTestService()
	_, err := svc.Reserve(context.Background(), 1, land.ReserveRequest{
		ProjectID: 999, CustomerID: 1, QuantityM2: mustDecimal(t, "10"), ReservedAt: time.Now(),
	})
	if err != land.ErrLandStockNotFound {
		t.Fatalf("want ErrLandStockNotFound, got %v", err)
	}
}

func TestCancelReservation_ReturnsQuantityToPool(t *testing.T) {
	svc, _ := newTestService()
	seedPool(t, svc, 1, 10, "500")
	res, err := svc.Reserve(context.Background(), 1, land.ReserveRequest{
		ProjectID: 10, CustomerID: 1, QuantityM2: mustDecimal(t, "100"), ReservedAt: time.Now(),
	})
	if err != nil {
		t.Fatalf("Reserve: %v", err)
	}
	closed, err := svc.CancelReservation(context.Background(), 1, res.ID, "buyer batal")
	if err != nil {
		t.Fatalf("CancelReservation: %v", err)
	}
	if closed.Status != land.ReservationStatusCancelled {
		t.Errorf("status want cancelled, got %s", closed.Status)
	}
	if closed.CancelledReason != "buyer batal" {
		t.Errorf("reason want %q, got %q", "buyer batal", closed.CancelledReason)
	}
	pool, _ := svc.GetPool(context.Background(), 1, 10)
	if !pool.AvailableQuantityM2().Equal(mustDecimal(t, "500")) {
		t.Errorf("available want 500 (restored), got %s", pool.AvailableQuantityM2())
	}
}

func TestCancelReservation_AlreadyTerminalRejected(t *testing.T) {
	svc, _ := newTestService()
	seedPool(t, svc, 1, 10, "500")
	res, err := svc.Reserve(context.Background(), 1, land.ReserveRequest{
		ProjectID: 10, CustomerID: 1, QuantityM2: mustDecimal(t, "100"), ReservedAt: time.Now(),
	})
	if err != nil {
		t.Fatalf("Reserve: %v", err)
	}
	if _, err := svc.CancelReservation(context.Background(), 1, res.ID, ""); err != nil {
		t.Fatalf("first cancel: %v", err)
	}
	_, err = svc.CancelReservation(context.Background(), 1, res.ID, "")
	if err != land.ErrReservationNotActive {
		t.Fatalf("want ErrReservationNotActive, got %v", err)
	}
}

func TestCancelReservation_NotFound(t *testing.T) {
	svc, _ := newTestService()
	_, err := svc.CancelReservation(context.Background(), 1, 999, "")
	if err != land.ErrReservationNotFound {
		t.Fatalf("want ErrReservationNotFound, got %v", err)
	}
}

func TestMarkExpiredReservations_SweepsPastExpiry(t *testing.T) {
	svc, _ := newTestService()
	seedPool(t, svc, 1, 10, "500")
	past := time.Now().Add(-time.Hour)
	future := time.Now().Add(time.Hour)

	expired, err := svc.Reserve(context.Background(), 1, land.ReserveRequest{
		ProjectID: 10, CustomerID: 1, QuantityM2: mustDecimal(t, "100"), ReservedAt: time.Now(), ExpiryDate: &past,
	})
	if err != nil {
		t.Fatalf("Reserve (past expiry): %v", err)
	}
	stillActive, err := svc.Reserve(context.Background(), 1, land.ReserveRequest{
		ProjectID: 10, CustomerID: 2, QuantityM2: mustDecimal(t, "100"), ReservedAt: time.Now(), ExpiryDate: &future,
	})
	if err != nil {
		t.Fatalf("Reserve (future expiry): %v", err)
	}

	n, err := svc.MarkExpiredReservations(context.Background(), 1, time.Now())
	if err != nil {
		t.Fatalf("MarkExpiredReservations: %v", err)
	}
	if n != 1 {
		t.Fatalf("want 1 expired, got %d", n)
	}

	got, _ := svc.GetReservation(context.Background(), 1, expired.ID)
	if got.Status != land.ReservationStatusExpired {
		t.Errorf("expired reservation status want expired, got %s", got.Status)
	}
	got2, _ := svc.GetReservation(context.Background(), 1, stillActive.ID)
	if got2.Status != land.ReservationStatusActive {
		t.Errorf("future-expiry reservation want still active, got %s", got2.Status)
	}
}

func TestReservation_TenantIsolation(t *testing.T) {
	svc, _ := newTestService()
	seedPool(t, svc, 1, 10, "500")
	res, err := svc.Reserve(context.Background(), 1, land.ReserveRequest{
		ProjectID: 10, CustomerID: 1, QuantityM2: mustDecimal(t, "100"), ReservedAt: time.Now(),
	})
	if err != nil {
		t.Fatalf("Reserve: %v", err)
	}
	_, err = svc.GetReservation(context.Background(), 2, res.ID)
	if err != land.ErrReservationNotFound {
		t.Fatalf("cross-tenant read want ErrReservationNotFound, got %v", err)
	}
}

func TestTenantIsolation_DifferentTenantCannotSeeOtherPool(t *testing.T) {
	svc, _ := newTestService()
	if _, err := svc.CreatePool(context.Background(), 1, land.CreatePoolRequest{
		ProjectID: 10, TotalQuantityM2: decimal.NewFromInt(500), UnitPrice: mustMoney(t, "1000000"),
	}); err != nil {
		t.Fatalf("CreatePool: %v", err)
	}
	_, err := svc.GetPool(context.Background(), 2, 10)
	if err != land.ErrLandStockNotFound {
		t.Fatalf("cross-tenant read want ErrLandStockNotFound, got %v", err)
	}
}

// ── LT-5: RecordAkad ─────────────────────────────────────────────────────

func validAkadReq() land.RecordAkadRequest {
	return land.RecordAkadRequest{
		ProjectID:          10,
		CustomerID:         42,
		QuantityM2:         decimal.NewFromInt(100),
		DPPAmount:          domain.Money{}, // set by caller via mustMoney where needed
		PaymentAccountCode: "1-1000",
		RecognitionDate:    time.Now(),
	}
}

func TestRecordAkad_HPPResolverNotConfigured(t *testing.T) {
	svc, _ := newTestService() // no WithHPPResolver
	seedPool(t, svc, 1, 10, "500")
	req := validAkadReq()
	req.DPPAmount = mustMoney(t, "50000000")
	_, err := svc.RecordAkad(context.Background(), 1, req)
	if err != land.ErrHPPResolverNotConfigured {
		t.Fatalf("want ErrHPPResolverNotConfigured, got %v", err)
	}
}

func TestRecordAkad_QuantityMustBePositive(t *testing.T) {
	svc, _, _ := newTestServiceWithAkad(t)
	seedPool(t, svc, 1, 10, "500")
	req := validAkadReq()
	req.QuantityM2 = decimal.Zero
	req.DPPAmount = mustMoney(t, "50000000")
	_, err := svc.RecordAkad(context.Background(), 1, req)
	if err != land.ErrQuantityMustBePositive {
		t.Fatalf("want ErrQuantityMustBePositive, got %v", err)
	}
}

func TestRecordAkad_CustomerRequired(t *testing.T) {
	svc, _, _ := newTestServiceWithAkad(t)
	seedPool(t, svc, 1, 10, "500")
	req := validAkadReq()
	req.CustomerID = 0
	req.DPPAmount = mustMoney(t, "50000000")
	_, err := svc.RecordAkad(context.Background(), 1, req)
	if err != land.ErrCustomerRequired {
		t.Fatalf("want ErrCustomerRequired, got %v", err)
	}
}

func TestRecordAkad_VATRateRequiredWhenPKP(t *testing.T) {
	svc, _, _ := newTestServiceWithAkad(t)
	seedPool(t, svc, 1, 10, "500")
	req := validAkadReq()
	req.DPPAmount = mustMoney(t, "50000000")
	req.IsPKP = true
	_, err := svc.RecordAkad(context.Background(), 1, req)
	if err != land.ErrVATRateRequired {
		t.Fatalf("want ErrVATRateRequired, got %v", err)
	}
}

func TestRecordAkad_PaymentAccountRequired(t *testing.T) {
	svc, _, _ := newTestServiceWithAkad(t)
	seedPool(t, svc, 1, 10, "500")
	req := validAkadReq()
	req.DPPAmount = mustMoney(t, "50000000")
	req.PaymentAccountCode = ""
	_, err := svc.RecordAkad(context.Background(), 1, req)
	if err != land.ErrPaymentAccountRequired {
		t.Fatalf("want ErrPaymentAccountRequired, got %v", err)
	}
}

func TestRecordAkad_RecognitionDateRequired(t *testing.T) {
	svc, _, _ := newTestServiceWithAkad(t)
	seedPool(t, svc, 1, 10, "500")
	req := validAkadReq()
	req.DPPAmount = mustMoney(t, "50000000")
	req.RecognitionDate = time.Time{}
	_, err := svc.RecordAkad(context.Background(), 1, req)
	if err != land.ErrRecognitionDateRequired {
		t.Fatalf("want ErrRecognitionDateRequired, got %v", err)
	}
}

func TestRecordAkad_PaymentAccountNotFound(t *testing.T) {
	svc, _, _ := newTestServiceWithAkad(t)
	seedPool(t, svc, 1, 10, "500")
	req := validAkadReq()
	req.DPPAmount = mustMoney(t, "50000000")
	req.PaymentAccountCode = "9-9999"
	_, err := svc.RecordAkad(context.Background(), 1, req)
	if err != land.ErrPaymentAccountNotFound {
		t.Fatalf("want ErrPaymentAccountNotFound, got %v", err)
	}
}

func TestRecordAkad_CapacityExceededWithoutReservation(t *testing.T) {
	svc, _, _ := newTestServiceWithAkad(t)
	seedPool(t, svc, 1, 10, "50")
	req := validAkadReq()
	req.QuantityM2 = mustDecimal(t, "100")
	req.DPPAmount = mustMoney(t, "50000000")
	_, err := svc.RecordAkad(context.Background(), 1, req)
	if err != land.ErrCapacityExceeded {
		t.Fatalf("want ErrCapacityExceeded, got %v", err)
	}
}

func TestRecordAkad_ReservationQuantityMismatch(t *testing.T) {
	svc, _, _ := newTestServiceWithAkad(t)
	seedPool(t, svc, 1, 10, "500")
	res, err := svc.Reserve(context.Background(), 1, land.ReserveRequest{
		ProjectID: 10, CustomerID: 42, QuantityM2: mustDecimal(t, "100"), ReservedAt: time.Now(),
	})
	if err != nil {
		t.Fatalf("Reserve: %v", err)
	}
	req := validAkadReq()
	req.ReservationID = &res.ID
	req.QuantityM2 = mustDecimal(t, "80") // mismatch vs reserved 100
	req.DPPAmount = mustMoney(t, "50000000")
	_, err = svc.RecordAkad(context.Background(), 1, req)
	if err != land.ErrReservationQuantityMismatch {
		t.Fatalf("want ErrReservationQuantityMismatch, got %v", err)
	}
}

func TestRecordAkad_ReservationCustomerMismatch(t *testing.T) {
	svc, _, _ := newTestServiceWithAkad(t)
	seedPool(t, svc, 1, 10, "500")
	res, err := svc.Reserve(context.Background(), 1, land.ReserveRequest{
		ProjectID: 10, CustomerID: 42, QuantityM2: mustDecimal(t, "100"), ReservedAt: time.Now(),
	})
	if err != nil {
		t.Fatalf("Reserve: %v", err)
	}
	req := validAkadReq()
	req.ReservationID = &res.ID
	req.CustomerID = 999 // mismatch
	req.QuantityM2 = mustDecimal(t, "100")
	req.DPPAmount = mustMoney(t, "50000000")
	_, err = svc.RecordAkad(context.Background(), 1, req)
	if err != land.ErrReservationCustomerMismatch {
		t.Fatalf("want ErrReservationCustomerMismatch, got %v", err)
	}
}

func TestRecordAkad_Success(t *testing.T) {
	svc, store, resolver := newTestServiceWithAkad(t)
	resolver.res = land.LandHPPResolution{RatePerM2: mustMoney(t, "300000"), Method: land.HPPMethodActual}
	seedPool(t, svc, 1, 10, "500")

	req := validAkadReq()
	req.QuantityM2 = mustDecimal(t, "100")
	req.DPPAmount = mustMoney(t, "50000000")
	sale, err := svc.RecordAkad(context.Background(), 1, req)
	if err != nil {
		t.Fatalf("RecordAkad: %v", err)
	}
	if sale.Status != land.LandSaleStatusAkad {
		t.Errorf("status want akad, got %s", sale.Status)
	}
	if !sale.GrossAmount.Equal(mustMoney(t, "50000000")) {
		t.Errorf("gross (no PPN) want 50000000, got %s", sale.GrossAmount)
	}

	pool, err := svc.GetPool(context.Background(), 1, 10)
	if err != nil {
		t.Fatalf("GetPool: %v", err)
	}
	if !pool.SoldQuantityM2.Equal(mustDecimal(t, "100")) {
		t.Errorf("sold_quantity_m2 want 100, got %s", pool.SoldQuantityM2)
	}

	got, err := svc.GetLandSale(context.Background(), 1, sale.ID)
	if err != nil {
		t.Fatalf("GetLandSale: %v", err)
	}
	if got.ID != sale.ID {
		t.Errorf("GetLandSale returned wrong id: %d", got.ID)
	}
	_ = store
}

func TestRecordAkad_SuccessWithPPN(t *testing.T) {
	svc, _, _ := newTestServiceWithAkad(t)
	seedPool(t, svc, 1, 10, "500")

	req := validAkadReq()
	req.QuantityM2 = mustDecimal(t, "100")
	req.DPPAmount = mustMoney(t, "50000000")
	req.IsPKP = true
	req.VATRateSnapshot = mustDecimal(t, "0.11")
	sale, err := svc.RecordAkad(context.Background(), 1, req)
	if err != nil {
		t.Fatalf("RecordAkad: %v", err)
	}
	// DPP 50.000.000 x 11% = 5.500.000; gross = 55.500.000.
	if !sale.GrossAmount.Equal(mustMoney(t, "55500000")) {
		t.Errorf("gross (with PPN) want 55500000, got %s", sale.GrossAmount)
	}
}

func TestRecordAkad_ConvertsReservation(t *testing.T) {
	svc, _, _ := newTestServiceWithAkad(t)
	seedPool(t, svc, 1, 10, "500")
	res, err := svc.Reserve(context.Background(), 1, land.ReserveRequest{
		ProjectID: 10, CustomerID: 42, QuantityM2: mustDecimal(t, "100"), ReservedAt: time.Now(),
	})
	if err != nil {
		t.Fatalf("Reserve: %v", err)
	}

	req := validAkadReq()
	req.ReservationID = &res.ID
	req.QuantityM2 = mustDecimal(t, "100")
	req.DPPAmount = mustMoney(t, "50000000")
	sale, err := svc.RecordAkad(context.Background(), 1, req)
	if err != nil {
		t.Fatalf("RecordAkad: %v", err)
	}

	got, err := svc.GetReservation(context.Background(), 1, res.ID)
	if err != nil {
		t.Fatalf("GetReservation: %v", err)
	}
	if got.Status != land.ReservationStatusConverted {
		t.Errorf("reservation status want converted, got %s", got.Status)
	}
	if got.ConvertedSaleID == nil || *got.ConvertedSaleID != sale.ID {
		t.Errorf("reservation converted_sale_id want %d, got %v", sale.ID, got.ConvertedSaleID)
	}

	pool, err := svc.GetPool(context.Background(), 1, 10)
	if err != nil {
		t.Fatalf("GetPool: %v", err)
	}
	// Converting a reservation moves quantity from reserved to sold — total
	// committed (reserved+sold) is unchanged, available stays at 400.
	if !pool.AvailableQuantityM2().Equal(mustDecimal(t, "400")) {
		t.Errorf("available want 400, got %s", pool.AvailableQuantityM2())
	}
	if !pool.SoldQuantityM2.Equal(mustDecimal(t, "100")) {
		t.Errorf("sold_quantity_m2 want 100, got %s", pool.SoldQuantityM2)
	}
	if !pool.ReservedQuantityM2.IsZero() {
		t.Errorf("reserved_quantity_m2 want 0, got %s", pool.ReservedQuantityM2)
	}
}

// ── LT-6: CancelLandSale (§F.3) ──────────────────────────────────────────

func TestCancelLandSale_ReasonRequired(t *testing.T) {
	svc, _, _ := newTestServiceWithAkad(t)
	seedPool(t, svc, 1, 10, "500")
	req := validAkadReq()
	req.DPPAmount = mustMoney(t, "50000000")
	sale, err := svc.RecordAkad(context.Background(), 1, req)
	if err != nil {
		t.Fatalf("RecordAkad: %v", err)
	}
	_, err = svc.CancelLandSale(context.Background(), 1, sale.ID, land.CancelLandSaleRequest{})
	if err != land.ErrCancelReasonRequired {
		t.Fatalf("want ErrCancelReasonRequired, got %v", err)
	}
}

func TestCancelLandSale_Success(t *testing.T) {
	svc, _, _ := newTestServiceWithAkad(t)
	seedPool(t, svc, 1, 10, "500")
	req := validAkadReq()
	req.DPPAmount = mustMoney(t, "50000000")
	sale, err := svc.RecordAkad(context.Background(), 1, req)
	if err != nil {
		t.Fatalf("RecordAkad: %v", err)
	}

	poolBefore, err := svc.GetPool(context.Background(), 1, 10)
	if err != nil {
		t.Fatalf("GetPool: %v", err)
	}
	if !poolBefore.SoldQuantityM2.Equal(mustDecimal(t, "100")) {
		t.Fatalf("precondition: sold_quantity_m2 want 100, got %s", poolBefore.SoldQuantityM2)
	}

	cancelled, err := svc.CancelLandSale(context.Background(), 1, sale.ID, land.CancelLandSaleRequest{
		Reason:     "batal — kesepakatan customer",
		CancelDate: time.Now(),
	})
	if err != nil {
		t.Fatalf("CancelLandSale: %v", err)
	}
	if cancelled.Status != land.LandSaleStatusCancelled {
		t.Errorf("status want cancelled, got %s", cancelled.Status)
	}
	if cancelled.CancelledAt == nil {
		t.Error("cancelled_at want set, got nil")
	}
	if cancelled.CancelReason != "batal — kesepakatan customer" {
		t.Errorf("cancel_reason mismatch: got %q", cancelled.CancelReason)
	}

	pool, err := svc.GetPool(context.Background(), 1, 10)
	if err != nil {
		t.Fatalf("GetPool: %v", err)
	}
	// §F.3: kuantitas kembali ke AVAILABLE (bukan RESERVED).
	if !pool.SoldQuantityM2.IsZero() {
		t.Errorf("sold_quantity_m2 want 0, got %s", pool.SoldQuantityM2)
	}
	if !pool.AvailableQuantityM2().Equal(mustDecimal(t, "500")) {
		t.Errorf("available want 500, got %s", pool.AvailableQuantityM2())
	}
}

func TestCancelLandSale_NotFound(t *testing.T) {
	svc, _, _ := newTestServiceWithAkad(t)
	_, err := svc.CancelLandSale(context.Background(), 1, 999, land.CancelLandSaleRequest{Reason: "x"})
	if err != land.ErrLandSaleNotFound {
		t.Fatalf("want ErrLandSaleNotFound, got %v", err)
	}
}

func TestCancelLandSale_AlreadyCancelledRejected(t *testing.T) {
	svc, _, _ := newTestServiceWithAkad(t)
	seedPool(t, svc, 1, 10, "500")
	req := validAkadReq()
	req.DPPAmount = mustMoney(t, "50000000")
	sale, err := svc.RecordAkad(context.Background(), 1, req)
	if err != nil {
		t.Fatalf("RecordAkad: %v", err)
	}
	if _, err := svc.CancelLandSale(context.Background(), 1, sale.ID, land.CancelLandSaleRequest{Reason: "pertama"}); err != nil {
		t.Fatalf("first CancelLandSale: %v", err)
	}
	_, err = svc.CancelLandSale(context.Background(), 1, sale.ID, land.CancelLandSaleRequest{Reason: "kedua"})
	if err != land.ErrLandSaleNotAkad {
		t.Fatalf("want ErrLandSaleNotAkad on double-cancel, got %v", err)
	}
}

func TestCancelLandSale_TenantIsolation(t *testing.T) {
	svc, _, _ := newTestServiceWithAkad(t)
	seedPool(t, svc, 1, 10, "500")
	req := validAkadReq()
	req.DPPAmount = mustMoney(t, "50000000")
	sale, err := svc.RecordAkad(context.Background(), 1, req)
	if err != nil {
		t.Fatalf("RecordAkad: %v", err)
	}
	_, err = svc.CancelLandSale(context.Background(), 999, sale.ID, land.CancelLandSaleRequest{Reason: "x"})
	if err != land.ErrLandSaleNotFound {
		t.Fatalf("cross-tenant cancel want ErrLandSaleNotFound, got %v", err)
	}
}

// TestReceivableRows_ReflectsLinkedScheduleProgress verifies land_sale
// menjadi AR anchor yang sah: PaidAmount/Received sekarang dihitung dari
// payment_schedules yang ter-link via land_sale_id (migrasi 000095), bukan
// hardcode Zero/false lagi.
func TestReceivableRows_ReflectsLinkedScheduleProgress(t *testing.T) {
	svc, store := newTestService()
	gross := mustMoney(t, "25000000")
	partial := mustMoney(t, "10000000")
	receivedStatus := "received"
	scheduledStatus := "scheduled"
	store.receivableRows = []land.LandSaleReceivable{
		{
			ID:              1,
			QuantityM2:      "50",
			GrossAmount:     gross,
			RecognitionDate: time.Now(),
			CustomerName:    "Budi",
			PaidAmount:      &partial,
			ScheduleStatus:  &scheduledStatus,
		},
		{
			ID:              2,
			QuantityM2:      "30",
			GrossAmount:     mustMoney(t, "15000000"),
			RecognitionDate: time.Now(),
			CustomerName:    "Siti",
			PaidAmount:      nil, // belum ada schedule ter-link (data lama)
			ScheduleStatus:  nil,
		},
		{
			ID:              3,
			QuantityM2:      "20",
			GrossAmount:     mustMoney(t, "8000000"),
			RecognitionDate: time.Now(),
			CustomerName:    "Ani",
			PaidAmount:      func() *domain.Money { m := mustMoney(t, "8000000"); return &m }(),
			ScheduleStatus:  &receivedStatus,
		},
	}

	rows, err := svc.ReceivableRows(context.Background(), 1)
	if err != nil {
		t.Fatalf("ReceivableRows: %v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("want 3 rows, got %d", len(rows))
	}

	if got := rows[0]; got.Source != receivable.SourceAddon || !got.PaidAmount.Equal(partial) || got.Received {
		t.Fatalf("row 0 (partial, unreceived): got paid=%v received=%v", got.PaidAmount, got.Received)
	}
	if got := rows[1]; !got.PaidAmount.Equal(domain.Zero) || got.Received {
		t.Fatalf("row 1 (no linked schedule): want PaidAmount=0, Received=false; got paid=%v received=%v", got.PaidAmount, got.Received)
	}
	if got := rows[2]; !got.PaidAmount.Equal(mustMoney(t, "8000000")) || !got.Received {
		t.Fatalf("row 2 (fully paid+received): got paid=%v received=%v", got.PaidAmount, got.Received)
	}
}

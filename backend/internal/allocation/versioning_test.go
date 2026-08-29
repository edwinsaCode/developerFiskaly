package allocation_test

// P0-4 D1 — SetBasis versioning: pra-freeze update in-place; pasca-freeze
// (snapshot ada) → version baru. Tanpa VersionStore = perilaku legacy.

import (
	"context"
	"testing"

	"esaproperti/internal/allocation"
)

type mockVersionStore struct {
	active       *allocation.AllocationConfigVersion
	hasSnapshots bool
	upserts      int
	nextCreates  int
}

func (m *mockVersionStore) GetActiveVersion(_ context.Context, _, _ uint64) (*allocation.AllocationConfigVersion, error) {
	if m.active == nil {
		return nil, allocation.ErrConfigNotFound
	}
	return m.active, nil
}
func (m *mockVersionStore) HasSnapshots(_ context.Context, _, _ uint64) (bool, error) {
	return m.hasSnapshots, nil
}
func (m *mockVersionStore) UpsertActiveVersion(_ context.Context, tenantID, projectID uint64, basis allocation.AllocationBasis) (*allocation.AllocationConfigVersion, error) {
	m.upserts++
	if m.active == nil {
		m.active = &allocation.AllocationConfigVersion{ID: 1, TenantID: tenantID, ProjectID: projectID, Version: 1, Basis: basis}
	} else {
		m.active.Basis = basis
	}
	return m.active, nil
}
func (m *mockVersionStore) CreateNextVersion(_ context.Context, tenantID, projectID uint64, basis allocation.AllocationBasis) (*allocation.AllocationConfigVersion, error) {
	m.nextCreates++
	m.active = &allocation.AllocationConfigVersion{ID: m.active.ID + 1, TenantID: tenantID, ProjectID: projectID, Version: m.active.Version + 1, Basis: basis}
	return m.active, nil
}

// mockConfigStore minimal untuk SetBasis.
type vtConfigStore struct{ cfg *allocation.AllocationConfig }

func (m *vtConfigStore) GetConfig(_ context.Context, _, _ uint64) (*allocation.AllocationConfig, error) {
	if m.cfg == nil {
		return nil, allocation.ErrConfigNotFound
	}
	return m.cfg, nil
}
func (m *vtConfigStore) SaveConfig(_ context.Context, cfg *allocation.AllocationConfig) error {
	m.cfg = cfg
	return nil
}

func TestSetBasis_Versioning_D1(t *testing.T) {
	ctx := context.Background()
	cfgs := &vtConfigStore{}
	vs := &mockVersionStore{}
	svc := allocation.NewService(cfgs, nil, nil, allocation.WithVersionStore(vs))

	// 1. Basis pertama → config + version 1.
	if err := svc.SetBasis(ctx, 1, 10, allocation.BasisSaleableArea); err != nil {
		t.Fatalf("SetBasis awal: %v", err)
	}
	if vs.active == nil || vs.active.Version != 1 || vs.active.Basis != allocation.BasisSaleableArea {
		t.Fatalf("version aktif = %+v, want v1 saleable_area", vs.active)
	}

	// 2. Ganti basis SEBELUM snapshot → update in-place (tetap v1).
	if err := svc.SetBasis(ctx, 1, 10, allocation.BasisSalesValue); err != nil {
		t.Fatalf("SetBasis pra-freeze: %v", err)
	}
	if vs.active.Version != 1 || vs.active.Basis != allocation.BasisSalesValue || vs.nextCreates != 0 {
		t.Fatalf("pra-freeze harus in-place: %+v (creates=%d)", vs.active, vs.nextCreates)
	}

	// 3. Snapshot ada (freeze D1) → ganti basis = VERSION BARU.
	vs.hasSnapshots = true
	if err := svc.SetBasis(ctx, 1, 10, allocation.BasisSaleableArea); err != nil {
		t.Fatalf("SetBasis pasca-freeze: %v", err)
	}
	if vs.active.Version != 2 || vs.active.Basis != allocation.BasisSaleableArea || vs.nextCreates != 1 {
		t.Fatalf("pasca-freeze harus version baru: %+v (creates=%d)", vs.active, vs.nextCreates)
	}
	if cfgs.cfg.Basis != allocation.BasisSaleableArea {
		t.Fatalf("legacy config harus ikut ter-update: %+v", cfgs.cfg)
	}

	// 4. Basis sama (no-op) → tidak membuat version baru.
	if err := svc.SetBasis(ctx, 1, 10, allocation.BasisSaleableArea); err != nil {
		t.Fatalf("SetBasis no-op: %v", err)
	}
	if vs.active.Version != 2 || vs.nextCreates != 1 {
		t.Fatalf("no-op tidak boleh menambah version: %+v", vs.active)
	}
}

func TestSetBasis_LegacyWithoutVersionStore(t *testing.T) {
	// Tanpa WithVersionStore → perilaku lama utuh (test lama tak tersentuh).
	cfgs := &vtConfigStore{}
	svc := allocation.NewService(cfgs, nil, nil)
	if err := svc.SetBasis(context.Background(), 1, 10, allocation.BasisSaleableArea); err != nil {
		t.Fatalf("SetBasis legacy: %v", err)
	}
	if cfgs.cfg == nil || cfgs.cfg.Basis != allocation.BasisSaleableArea {
		t.Fatalf("config legacy = %+v", cfgs.cfg)
	}
}

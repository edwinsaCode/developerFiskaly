package scheme

import (
	"context"
	"fmt"

	"gorm.io/gorm"
)

// ── Store interfaces ──────────────────────────────────────────────────────────

type SchemeStore interface {
	CreateScheme(ctx context.Context, s *PaymentScheme) error
	FindSchemeByID(ctx context.Context, tenantID, id uint64) (*PaymentScheme, error)
	ListSchemes(ctx context.Context, tenantID uint64) ([]*PaymentScheme, error)
	UpdateScheme(ctx context.Context, s *PaymentScheme) error

	CreateFinancingSource(ctx context.Context, f *FinancingSource) error
	FindFinancingSourceByID(ctx context.Context, tenantID, id uint64) (*FinancingSource, error)
	ListFinancingSources(ctx context.Context, tenantID uint64) ([]*FinancingSource, error)
	UpdateFinancingSource(ctx context.Context, f *FinancingSource) error
}

// Service mengelola master Payment Scheme + Financing Source.
// Semua params divalidasi oleh policy-nya (registry) sebelum tersimpan.
type Service struct {
	store    SchemeStore
	registry *Registry
}

func NewService(store SchemeStore, registry *Registry) *Service {
	return &Service{store: store, registry: registry}
}

// Registry mengekspos registry (dipakai sale service via wiring — satu instance).
func (s *Service) Registry() *Registry { return s.registry }

// ── PaymentScheme CRUD ────────────────────────────────────────────────────────

type CreateSchemeRequest struct {
	Code       string
	Name       string
	PolicyType PolicyType
	Params     Params
}

func (s *Service) CreateScheme(ctx context.Context, tenantID uint64, req CreateSchemeRequest) (*PaymentScheme, error) {
	if req.Code == "" || req.Name == "" {
		return nil, ErrSchemeCodeRequired
	}
	pol, err := s.registry.Policy(req.PolicyType)
	if err != nil {
		return nil, err
	}
	if err := pol.ValidateParams(req.Params); err != nil {
		return nil, err
	}
	raw, err := req.Params.JSON()
	if err != nil {
		return nil, fmt.Errorf("serialize params: %w", err)
	}
	m := &PaymentScheme{
		TenantID:   tenantID,
		Code:       req.Code,
		Name:       req.Name,
		PolicyType: req.PolicyType,
		Params:     raw,
		IsActive:   true,
	}
	if err := s.store.CreateScheme(ctx, m); err != nil {
		return nil, err
	}
	return m, nil
}

// UpdateSchemeRequest: parsial — field kosong = tidak diubah. Code & PolicyType
// immutable by construction (tidak ada field-nya).
type UpdateSchemeRequest struct {
	Name     string
	Params   *Params
	IsActive *bool
}

func (s *Service) UpdateScheme(ctx context.Context, tenantID, id uint64, req UpdateSchemeRequest) (*PaymentScheme, error) {
	m, err := s.store.FindSchemeByID(ctx, tenantID, id)
	if err != nil {
		return nil, err
	}
	if req.Name != "" {
		m.Name = req.Name
	}
	if req.Params != nil {
		pol, err := s.registry.Policy(m.PolicyType)
		if err != nil {
			return nil, err
		}
		if err := pol.ValidateParams(*req.Params); err != nil {
			return nil, err
		}
		raw, err := req.Params.JSON()
		if err != nil {
			return nil, fmt.Errorf("serialize params: %w", err)
		}
		m.Params = raw
	}
	if req.IsActive != nil {
		m.IsActive = *req.IsActive
	}
	if err := s.store.UpdateScheme(ctx, m); err != nil {
		return nil, err
	}
	return m, nil
}

func (s *Service) GetScheme(ctx context.Context, tenantID, id uint64) (*PaymentScheme, error) {
	return s.store.FindSchemeByID(ctx, tenantID, id)
}

func (s *Service) ListSchemes(ctx context.Context, tenantID uint64) ([]*PaymentScheme, error) {
	return s.store.ListSchemes(ctx, tenantID)
}

// ── FinancingSource CRUD ──────────────────────────────────────────────────────

type CreateFinancingSourceRequest struct {
	Code string
	Name string
	Type FinancingSourceType
}

func (s *Service) CreateFinancingSource(ctx context.Context, tenantID uint64, req CreateFinancingSourceRequest) (*FinancingSource, error) {
	if req.Code == "" || req.Name == "" {
		return nil, ErrFinSourceCodeReq
	}
	if req.Type == "" {
		req.Type = FinSourceKPRKomersial
	}
	if !req.Type.Valid() {
		return nil, ErrFinSourceInvalidType
	}
	f := &FinancingSource{
		TenantID: tenantID,
		Code:     req.Code,
		Name:     req.Name,
		Type:     req.Type,
		IsActive: true,
	}
	if err := s.store.CreateFinancingSource(ctx, f); err != nil {
		return nil, err
	}
	return f, nil
}

type UpdateFinancingSourceRequest struct {
	Name     string
	Type     FinancingSourceType
	IsActive *bool
}

func (s *Service) UpdateFinancingSource(ctx context.Context, tenantID, id uint64, req UpdateFinancingSourceRequest) (*FinancingSource, error) {
	f, err := s.store.FindFinancingSourceByID(ctx, tenantID, id)
	if err != nil {
		return nil, err
	}
	if req.Name != "" {
		f.Name = req.Name
	}
	if req.Type != "" {
		if !req.Type.Valid() {
			return nil, ErrFinSourceInvalidType
		}
		f.Type = req.Type
	}
	if req.IsActive != nil {
		f.IsActive = *req.IsActive
	}
	if err := s.store.UpdateFinancingSource(ctx, f); err != nil {
		return nil, err
	}
	return f, nil
}

func (s *Service) GetFinancingSource(ctx context.Context, tenantID, id uint64) (*FinancingSource, error) {
	return s.store.FindFinancingSourceByID(ctx, tenantID, id)
}

func (s *Service) ListFinancingSources(ctx context.Context, tenantID uint64) ([]*FinancingSource, error) {
	return s.store.ListFinancingSources(ctx, tenantID)
}

// ── Seed default schemes (tenant baru — dipanggil saat registrasi) ────────────

// defaultSchemes mendefinisikan lima scheme bawaan. Sumber parameter yang SAMA
// dengan seed migrasi 000037 untuk tenant existing — jaga tetap sinkron.
func defaultSchemes() []CreateSchemeRequest {
	return []CreateSchemeRequest{
		{Code: "CASH", Name: "Tunai Keras", PolicyType: PolicyCash,
			Params: Params{BASTGate: GateFullPayment, ReceivableAccount: DefaultReceivableAccount}},
		{Code: "CASH-BTHP", Name: "Tunai Bertahap", PolicyType: PolicyCashInstallment,
			Params: Params{DPPercent: "20", InstallmentCount: 3, BASTGate: GateFullPayment, ReceivableAccount: DefaultReceivableAccount}},
		{Code: "KPR-SUB", Name: "KPR Subsidi", PolicyType: PolicyKPR,
			Params: Params{DPPercent: "1", FinalDueMonths: 6, BASTGate: GateAkad, ReceivableAccount: DefaultReceivableAccount, FinancingReceivableAccount: "1-2200"}},
		{Code: "KPR-KOM", Name: "KPR Komersial", PolicyType: PolicyKPR,
			Params: Params{DPPercent: "10", FinalDueMonths: 6, BASTGate: GateAkad, ReceivableAccount: DefaultReceivableAccount, FinancingReceivableAccount: "1-2200"}},
		{Code: "INHOUSE", Name: "In-House (cicilan developer)", PolicyType: PolicyInHouse,
			Params: Params{DPPercent: "20", TenorMonths: 24, BASTGate: GateDPPaid, ReceivableAccount: DefaultReceivableAccount}},
	}
}

// SeedDefaultSchemes membuat scheme bawaan untuk tenant (idempoten — skip yang
// sudah ada). Dipanggil di alur registrasi tenant (pola ledger.SeedCOA).
func SeedDefaultSchemes(ctx context.Context, db *gorm.DB, tenantID uint64) error {
	reg := DefaultRegistry()
	for _, req := range defaultSchemes() {
		pol, err := reg.Policy(req.PolicyType)
		if err != nil {
			return err
		}
		if err := pol.ValidateParams(req.Params); err != nil {
			return fmt.Errorf("seed scheme %s: %w", req.Code, err)
		}
		raw, err := req.Params.JSON()
		if err != nil {
			return err
		}
		m := PaymentScheme{
			TenantID:   tenantID,
			Code:       req.Code,
			Name:       req.Name,
			PolicyType: req.PolicyType,
			Params:     raw,
			IsActive:   true,
		}
		res := db.WithContext(ctx).
			Where(PaymentScheme{TenantID: tenantID, Code: req.Code}).
			FirstOrCreate(&m)
		if res.Error != nil {
			return fmt.Errorf("seed scheme %s: %w", req.Code, res.Error)
		}
	}
	return nil
}

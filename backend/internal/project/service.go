package project

import (
	"context"
	"fmt"
	"time"

	"github.com/shopspring/decimal"

	"esaproperti/internal/domain"
)

// ── Store interfaces ──────────────────────────────────────────────────────────

type ProjectStore interface {
	CreateProject(ctx context.Context, p *Project) error
	FindProjectByID(ctx context.Context, tenantID, id uint64) (*Project, error)
	ListProjects(ctx context.Context, tenantID uint64) ([]*Project, error)
	UpdateProjectStatus(ctx context.Context, tenantID, id uint64, status ProjectStatus) error
	UpdateProjectTaxCategory(ctx context.Context, tenantID, id uint64, category domain.TaxCategory) error
}

type PhaseStore interface {
	CreatePhase(ctx context.Context, ph *ProjectPhase) error
	FindPhaseByID(ctx context.Context, tenantID, id uint64) (*ProjectPhase, error)
	ListPhasesByProject(ctx context.Context, tenantID, projectID uint64) ([]*ProjectPhase, error)
	UpdatePhaseStatus(ctx context.Context, tenantID, id uint64, status PhaseStatus) error
}

type UnitStore interface {
	CreateUnit(ctx context.Context, u *Unit) error
	// CreateUnitsAtomic (UAT Batch 2 §4): buat banyak unit SATU transaksi;
	// kode duplikat dalam proyek → seluruh batch batal (ErrBulkDuplicateCode).
	CreateUnitsAtomic(ctx context.Context, tenantID, projectID uint64, units []*Unit) error
	FindUnitByID(ctx context.Context, tenantID, id uint64) (*Unit, error)
	ListUnitsByProject(ctx context.Context, tenantID, projectID uint64) ([]*Unit, error)
	ListUnitsByPhase(ctx context.Context, tenantID, phaseID uint64) ([]*Unit, error)
	UpdateUnitStatus(ctx context.Context, tenantID, id uint64, status UnitStatus, opts UpdateUnitOpts) error
	// UpdateUnitLandArea (LT-2): koreksi luas tanah unit, admin eksplisit —
	// bukan bagian dari status transition, jadi method terpisah dari opts di atas.
	UpdateUnitLandArea(ctx context.Context, tenantID, id uint64, landArea decimal.Decimal) error
	// TransitionUnitAtomic updates status AND appends a transition log in one tx (Increment 6).
	TransitionUnitAtomic(ctx context.Context, tenantID, id uint64, from, to UnitStatus, opts UpdateUnitOpts, log *UnitStatusTransition) error
	// ListTransitions returns a unit's status history, oldest first.
	ListTransitions(ctx context.Context, tenantID, unitID uint64) ([]*UnitStatusTransition, error)
	// ListProjectTransitions: transisi terbaru lintas unit satu proyek (Timeline).
	ListProjectTransitions(ctx context.Context, tenantID, projectID uint64, limit int) ([]*UnitStatusTransition, error)
	// AttachBuyerNames mengisi nama pemegang unit (kontrak → booking → buyer_ref).
	// Lihat buyer_name.go untuk urutan dan alasannya.
	AttachBuyerNames(ctx context.Context, tenantID uint64, units []*Unit) error
}

// ApprovalGate adalah SEAM ke Generic Approval Workflow (Increment 5, opt-in).
// Increment 6 D2: transisi ke hold/blocked di-gate. Tanpa gate terpasang atau
// tanpa workflow aktif utk `unit_transition` → transisi langsung (perilaku lama).
// nil error = boleh lanjut; approval.ErrApprovalRequired = butuh request APPROVED.
type ApprovalGate interface {
	RequireApproved(ctx context.Context, tenantID, unitID uint64) error
}

// ── Request / option types ────────────────────────────────────────────────────

type CreateProjectRequest struct {
	Name      string
	LandArea  decimal.Decimal
	Notes     string
	StartDate *time.Time
	// TaxCategory: kosong = komersial (default konservatif). Increment 4.
	TaxCategory domain.TaxCategory
}

type CreatePhaseRequest struct {
	ProjectID   uint64
	Name        string
	Description string
	TargetUnits int
}

type CreateUnitRequest struct {
	ProjectID    uint64
	PhaseID      *uint64
	Code         string
	UnitType     string
	TypeLabel    *string // label komersial (mis. "36/72"), opsional
	SaleableArea decimal.Decimal
	// LandArea (LT-2): opsional saat create — nol berarti diisi admin belakangan
	// lewat UpdateUnitLandArea (form edit unit), bukan wajib di titik pembuatan.
	LandArea  decimal.Decimal
	ListPrice domain.Money
}

// BulkCreateUnitsRequest (UAT Batch 2 §4): pembuatan unit per BLOK dalam satu
// transaksi — Block A, Unit 1–12, tipe & harga sama → kode "A-01".."A-12".
type BulkCreateUnitsRequest struct {
	ProjectID    uint64
	PhaseID      *uint64
	Block        string // prefix blok, mis. "A"
	UnitStart    int    // nomor awal (inklusif), ≥ 1
	UnitEnd      int    // nomor akhir (inklusif)
	UnitType     string
	TypeLabel    *string
	SaleableArea decimal.Decimal
	// LandArea: luas tanah per unit dalam blok — diterapkan sama ke setiap unit
	// yang dibuat pada batch ini (unit dalam satu blok lazimnya satu ukuran).
	LandArea  decimal.Decimal
	ListPrice domain.Money
}

// UpdateUnitOpts carries optional fields set during status transitions
// (e.g. reserved → buyer captured; sold → sale date + price set).
type UpdateUnitOpts struct {
	BuyerRef  *string
	SaleDate  *time.Time
	SalePrice *decimal.Decimal
}

// ── Service ───────────────────────────────────────────────────────────────────

type Service struct {
	projects     ProjectStore
	phases       PhaseStore
	units        UnitStore
	approvalGate ApprovalGate // opt-in; nil = tanpa gate (perilaku lama)
	// productTypes (UAT Batch 2 §2): master katalog produk — nil = tanpa
	// validasi unit_type (kompat unit test lama).
	productTypes ProductTypeStore
}

func NewService(projects ProjectStore, phases PhaseStore, units UnitStore) *Service {
	return &Service{projects: projects, phases: phases, units: units}
}

// SetApprovalGate memasang gate approval (wiring produksi). Tanpa gate = perilaku lama.
func (s *Service) SetApprovalGate(g ApprovalGate) { s.approvalGate = g }

// ── Project operations ────────────────────────────────────────────────────────

func (s *Service) CreateProject(ctx context.Context, tenantID uint64, req CreateProjectRequest) (*Project, error) {
	category := req.TaxCategory
	if category == "" {
		category = domain.TaxCategoryKomersial
	}
	if !category.Valid() {
		return nil, ErrInvalidTaxCategory
	}
	p := &Project{
		TenantID:    tenantID,
		Name:        req.Name,
		Status:      ProjectStatusPlanning,
		LandArea:    req.LandArea,
		TaxCategory: category,
		Notes:       req.Notes,
		StartDate:   req.StartDate,
	}
	if err := s.projects.CreateProject(ctx, p); err != nil {
		return nil, fmt.Errorf("buat proyek: %w", err)
	}
	return p, nil
}

// UpdateTaxCategory mengubah penentu tarif pajak proyek (Increment 4).
// Perubahan hanya berefek pada AKRUAL BERIKUTNYA — obligation lama memegang
// snapshot tarif + rule provenance sendiri (append-only, tidak direcalculate).
func (s *Service) UpdateTaxCategory(ctx context.Context, tenantID, id uint64, category domain.TaxCategory) (*Project, error) {
	if !category.Valid() {
		return nil, ErrInvalidTaxCategory
	}
	p, err := s.projects.FindProjectByID(ctx, tenantID, id)
	if err != nil {
		return nil, err
	}
	if err := s.projects.UpdateProjectTaxCategory(ctx, tenantID, id, category); err != nil {
		return nil, fmt.Errorf("update tax_category: %w", err)
	}
	p.TaxCategory = category
	return p, nil
}

func (s *Service) GetProject(ctx context.Context, tenantID, id uint64) (*Project, error) {
	p, err := s.projects.FindProjectByID(ctx, tenantID, id)
	if err != nil {
		return nil, err
	}
	return p, nil
}

func (s *Service) ListProjects(ctx context.Context, tenantID uint64) ([]*Project, error) {
	return s.projects.ListProjects(ctx, tenantID)
}

// TransitionProject moves a project to the next status.
// Returns ErrProjectInvalidTransition if the transition is not legal.
func (s *Service) TransitionProject(ctx context.Context, tenantID, id uint64, next ProjectStatus) (*Project, error) {
	p, err := s.projects.FindProjectByID(ctx, tenantID, id)
	if err != nil {
		return nil, err
	}
	if !p.Status.CanTransitionTo(next) {
		return nil, fmt.Errorf("%w: %s → %s", ErrProjectInvalidTransition, p.Status, next)
	}
	if err := s.projects.UpdateProjectStatus(ctx, tenantID, id, next); err != nil {
		return nil, fmt.Errorf("update status proyek: %w", err)
	}
	p.Status = next
	return p, nil
}

// ── Phase operations ──────────────────────────────────────────────────────────

func (s *Service) CreatePhase(ctx context.Context, tenantID uint64, req CreatePhaseRequest) (*ProjectPhase, error) {
	// Verify the parent project belongs to this tenant.
	if _, err := s.projects.FindProjectByID(ctx, tenantID, req.ProjectID); err != nil {
		return nil, fmt.Errorf("proyek induk: %w", err)
	}
	ph := &ProjectPhase{
		TenantID:    tenantID,
		ProjectID:   req.ProjectID,
		Name:        req.Name,
		Description: req.Description,
		TargetUnits: req.TargetUnits,
		Status:      PhaseStatusPlanning,
	}
	if err := s.phases.CreatePhase(ctx, ph); err != nil {
		return nil, fmt.Errorf("buat fase: %w", err)
	}
	return ph, nil
}

func (s *Service) GetPhase(ctx context.Context, tenantID, id uint64) (*ProjectPhase, error) {
	return s.phases.FindPhaseByID(ctx, tenantID, id)
}

func (s *Service) ListPhasesByProject(ctx context.Context, tenantID, projectID uint64) ([]*ProjectPhase, error) {
	return s.phases.ListPhasesByProject(ctx, tenantID, projectID)
}

// TransitionPhase moves a phase to the next status.
// Returns ErrPhaseInvalidTransition if the transition is not legal.
func (s *Service) TransitionPhase(ctx context.Context, tenantID, id uint64, next PhaseStatus) (*ProjectPhase, error) {
	ph, err := s.phases.FindPhaseByID(ctx, tenantID, id)
	if err != nil {
		return nil, err
	}
	if !ph.Status.CanTransitionTo(next) {
		return nil, fmt.Errorf("%w: %s → %s", ErrPhaseInvalidTransition, ph.Status, next)
	}
	if err := s.phases.UpdatePhaseStatus(ctx, tenantID, id, next); err != nil {
		return nil, fmt.Errorf("update status fase: %w", err)
	}
	ph.Status = next
	return ph, nil
}

// ── Unit operations ───────────────────────────────────────────────────────────

// CreateUnit validates and creates a new unit.
// Enforces Invariant #2: list_price must be whole rupiah.
func (s *Service) CreateUnit(ctx context.Context, tenantID uint64, req CreateUnitRequest) (*Unit, error) {
	if !req.ListPrice.IsWholeRupiah() {
		return nil, ErrListPriceFractional
	}
	// UAT Batch 2 §2: unit baru wajib memakai product type aktif dari master.
	if err := s.validateUnitType(ctx, tenantID, req.UnitType); err != nil {
		return nil, err
	}
	// Verify project belongs to this tenant.
	if _, err := s.projects.FindProjectByID(ctx, tenantID, req.ProjectID); err != nil {
		return nil, fmt.Errorf("proyek: %w", err)
	}
	u := &Unit{
		TenantID:     tenantID,
		ProjectID:    req.ProjectID,
		PhaseID:      req.PhaseID,
		Code:         req.Code,
		UnitType:     req.UnitType,
		TypeLabel:    req.TypeLabel,
		SaleableArea: req.SaleableArea,
		LandArea:     req.LandArea,
		ListPrice:    req.ListPrice,
		Status:       UnitStatusAvailable,
	}
	if err := s.units.CreateUnit(ctx, u); err != nil {
		return nil, err
	}
	return u, nil
}

// bulkMaxUnits: batas satu batch generator (cap eksplisit, bukan silent).
const bulkMaxUnits = 500

// BulkCreateUnits membuat seluruh unit satu blok ATOMIK (satu transaksi):
// kode "{BLOCK}-{NN}" zero-padded. Kode duplikat di proyek → SELURUH batch
// dibatalkan (jujur, tidak partial). Pembuatan satuan tetap ada (CreateUnit).
func (s *Service) BulkCreateUnits(ctx context.Context, tenantID uint64, req BulkCreateUnitsRequest) ([]*Unit, error) {
	if req.Block == "" || req.UnitType == "" {
		return nil, ErrBulkBlockRequired
	}
	if req.UnitStart < 1 || req.UnitEnd < req.UnitStart {
		return nil, ErrBulkRangeInvalid
	}
	count := req.UnitEnd - req.UnitStart + 1
	if count > bulkMaxUnits {
		return nil, fmt.Errorf("%w: %d unit (maks %d)", ErrBulkTooMany, count, bulkMaxUnits)
	}
	if !req.ListPrice.IsWholeRupiah() {
		return nil, ErrListPriceFractional
	}
	if err := s.validateUnitType(ctx, tenantID, req.UnitType); err != nil {
		return nil, err
	}
	if _, err := s.projects.FindProjectByID(ctx, tenantID, req.ProjectID); err != nil {
		return nil, fmt.Errorf("proyek: %w", err)
	}

	units := make([]*Unit, 0, count)
	for n := req.UnitStart; n <= req.UnitEnd; n++ {
		units = append(units, &Unit{
			TenantID:     tenantID,
			ProjectID:    req.ProjectID,
			PhaseID:      req.PhaseID,
			Code:         fmt.Sprintf("%s-%02d", req.Block, n),
			UnitType:     req.UnitType,
			TypeLabel:    req.TypeLabel,
			SaleableArea: req.SaleableArea,
			LandArea:     req.LandArea,
			ListPrice:    req.ListPrice,
			Status:       UnitStatusAvailable,
		})
	}
	if err := s.units.CreateUnitsAtomic(ctx, tenantID, req.ProjectID, units); err != nil {
		return nil, err
	}
	return units, nil
}

// Ketiga jalur baca unit di bawah melewati withBuyerNames. Kalau salah satu
// dilewati, layar itu jadi satu-satunya yang tak tahu siapa pemegang unitnya —
// persis keadaan yang dikeluhkan sebelum ini ada.

func (s *Service) GetUnit(ctx context.Context, tenantID, id uint64) (*Unit, error) {
	u, err := s.units.FindUnitByID(ctx, tenantID, id)
	if err != nil {
		return nil, err
	}
	return u, s.withBuyerNames(ctx, tenantID, []*Unit{u})
}

// UpdateUnitLandArea (LT-2, kelebihan-tanah-final-architecture §B.5): koreksi
// eksplisit-admin luas tanah unit, TERPISAH dari lifecycle transition. Tidak ada
// backfill massal — hanya jalur ini yang boleh mengubah land_area, satu unit
// per panggilan, kapan saja (bukan hanya saat create).
func (s *Service) UpdateUnitLandArea(ctx context.Context, tenantID, id uint64, landArea decimal.Decimal) (*Unit, error) {
	if landArea.IsNegative() {
		return nil, ErrLandAreaNegative
	}
	u, err := s.units.FindUnitByID(ctx, tenantID, id)
	if err != nil {
		return nil, err
	}
	if err := s.units.UpdateUnitLandArea(ctx, tenantID, id, landArea); err != nil {
		return nil, err
	}
	u.LandArea = landArea
	return u, nil
}

func (s *Service) ListUnitsByProject(ctx context.Context, tenantID, projectID uint64) ([]*Unit, error) {
	us, err := s.units.ListUnitsByProject(ctx, tenantID, projectID)
	if err != nil {
		return nil, err
	}
	us = s.excludeNonPropertyUnits(ctx, tenantID, us)
	return us, s.withBuyerNames(ctx, tenantID, us)
}

func (s *Service) ListUnitsByPhase(ctx context.Context, tenantID, phaseID uint64) ([]*Unit, error) {
	us, err := s.units.ListUnitsByPhase(ctx, tenantID, phaseID)
	if err != nil {
		return nil, err
	}
	us = s.excludeNonPropertyUnits(ctx, tenantID, us)
	return us, s.withBuyerNames(ctx, tenantID, us)
}

func (s *Service) withBuyerNames(ctx context.Context, tenantID uint64, us []*Unit) error {
	return s.units.AttachBuyerNames(ctx, tenantID, us)
}

// excludeNonPropertyUnits (LT-0): listing penjualan (/penjualan) hanya boleh
// menampilkan unit properti. Sebelum W-13, beberapa produk non-properti
// (mis. "Kelebihan Tanah") pernah tersimpan sebagai baris `units` biasa —
// disaring di SINI SAJA, hanya dari hasil listing. Baris datanya, jurnal, dan
// histori transaksinya sama sekali tidak disentuh/diubah.
//
// unit_type yang tidak terdaftar di master product_types (data legacy
// pra-katalog, banyak ditemukan di tenant lama) TETAP tampil — fail-open,
// bukan fail-closed. Ini listing baca-saja, bukan gerbang finansial: menyaring
// terlalu agresif akan menyembunyikan unit properti sungguhan gara-gara master
// data yang belum lengkap, yang jauh lebih berbahaya daripada satu unit palsu
// yang lolos.
func (s *Service) excludeNonPropertyUnits(ctx context.Context, tenantID uint64, us []*Unit) []*Unit {
	if s.productTypes == nil || len(us) == 0 {
		return us
	}
	categoryByCode := map[string]ProductCategory{}
	out := make([]*Unit, 0, len(us))
	for _, u := range us {
		cat, known := categoryByCode[u.UnitType]
		if !known {
			pt, err := s.productTypes.FindProductTypeByCode(ctx, tenantID, u.UnitType)
			if err != nil {
				categoryByCode[u.UnitType] = "" // tidak terdaftar → fail-open
				out = append(out, u)
				continue
			}
			cat = pt.Category
			categoryByCode[u.UnitType] = cat
		}
		if cat == ProductCategoryNonProperty {
			continue
		}
		out = append(out, u)
	}
	return out
}

// TransitionRequest carries a status transition + its audit metadata (Increment 6).
type TransitionRequest struct {
	Next          UnitStatus
	Event         TransitionEvent // kosong → manual
	EventDate     *time.Time      // kosong → sekarang (waktu pencatatan)
	ActorID       *uint64         // user pemicu (audit "siapa")
	Notes         string
	ReferenceType string  // kosong → manual
	ReferenceID   *uint64 //
	UpdateUnitOpts
}

// TransitionUnit moves a unit to the next status (kompat: event=manual, tanpa actor).
// Dipertahankan untuk pemanggil lama; delegasi ke Transition.
func (s *Service) TransitionUnit(ctx context.Context, tenantID, id uint64, next UnitStatus, opts UpdateUnitOpts) (*Unit, error) {
	return s.Transition(ctx, tenantID, id, TransitionRequest{Next: next, UpdateUnitOpts: opts})
}

// Transition memvalidasi & menerapkan satu perubahan status unit, menulis status
// + audit log secara ATOMIK (satu tx). Aturan (blueprint §9):
//   - hanya transisi di matriks yang sah (ErrUnitInvalidTransition);
//   - transisi ke hold/blocked di-gate approval (opt-in, D2);
//   - sold → available (pasca-BAST) DITOLAK di sini — butuh domain Cancellation (D2);
//   - setiap transisi tercatat append-only (event, actor, event_date, reference).
func (s *Service) Transition(ctx context.Context, tenantID, id uint64, req TransitionRequest) (*Unit, error) {
	u, err := s.units.FindUnitByID(ctx, tenantID, id)
	if err != nil {
		return nil, err
	}
	next := req.Next
	if !u.Status.CanTransitionTo(next) {
		return nil, fmt.Errorf("%w: %s → %s", ErrUnitInvalidTransition, u.Status, next)
	}
	// D2: pasca-BAST (sold → available) hanya boleh via domain Cancellation.
	if u.Status == UnitStatusSold && next == UnitStatusAvailable {
		return nil, ErrPostBASTCancellation
	}

	event := req.Event
	if event == "" {
		event = EventManual
	}
	if !event.Valid() {
		return nil, fmt.Errorf("%w: %s", ErrUnknownTransitionEvent, event)
	}
	// F-4: label sebab wajib koheren dengan transisi (manual = generik;
	// backfill hanya untuk migration → ditolak di sini).
	if !event.Allows(u.Status, next) {
		return nil, fmt.Errorf("%w: %s pada %s → %s", ErrEventTransitionMismatch, event, u.Status, next)
	}
	if req.UpdateUnitOpts.SalePrice != nil {
		sp := domain.FromDecimal(*req.UpdateUnitOpts.SalePrice)
		if !sp.IsWholeRupiah() {
			return nil, ErrSalePriceFractional
		}
	}

	// Gate approval (opt-in) untuk hold/blocked. Tanpa gate → skip (perilaku lama);
	// dengan gate tapi tanpa workflow aktif → RequireApproved mengembalikan nil.
	if next.RequiresApproval() && s.approvalGate != nil {
		if err := s.approvalGate.RequireApproved(ctx, tenantID, id); err != nil {
			return nil, err
		}
	}

	refType := req.ReferenceType
	if refType == "" {
		refType = RefTypeManual
	}
	eventDate := time.Now()
	if req.EventDate != nil {
		eventDate = *req.EventDate
	}
	log := &UnitStatusTransition{
		TenantID:      tenantID,
		UnitID:        id,
		FromStatus:    u.Status,
		ToStatus:      next,
		Event:         event,
		EventDate:     eventDate,
		ReferenceType: refType,
		ReferenceID:   req.ReferenceID,
		ActorID:       req.ActorID,
		Notes:         req.Notes,
	}
	if err := s.units.TransitionUnitAtomic(ctx, tenantID, id, u.Status, next, req.UpdateUnitOpts, log); err != nil {
		return nil, err
	}

	u.Status = next
	if req.UpdateUnitOpts.BuyerRef != nil {
		u.BuyerRef = req.UpdateUnitOpts.BuyerRef
	}
	if req.UpdateUnitOpts.SaleDate != nil {
		u.SaleDate = req.UpdateUnitOpts.SaleDate
	}
	if req.UpdateUnitOpts.SalePrice != nil {
		u.SalePrice = req.UpdateUnitOpts.SalePrice
	}
	return u, nil
}

// ListProjectTransitions: timeline workspace proyek.
func (s *Service) ListProjectTransitions(ctx context.Context, tenantID, projectID uint64, limit int) ([]*UnitStatusTransition, error) {
	if _, err := s.projects.FindProjectByID(ctx, tenantID, projectID); err != nil {
		return nil, err
	}
	return s.units.ListProjectTransitions(ctx, tenantID, projectID, limit)
}

// ListTransitions returns a unit's status history (audit + time-on-market).
func (s *Service) ListTransitions(ctx context.Context, tenantID, unitID uint64) ([]*UnitStatusTransition, error) {
	// Pastikan unit milik tenant ini sebelum membaca histori.
	if _, err := s.units.FindUnitByID(ctx, tenantID, unitID); err != nil {
		return nil, err
	}
	return s.units.ListTransitions(ctx, tenantID, unitID)
}

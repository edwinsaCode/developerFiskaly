package project

import (
	"time"

	"github.com/shopspring/decimal"

	"esaproperti/internal/domain"
)

// ── Project ───────────────────────────────────────────────────────────────────

// ProjectStatus is the lifecycle state of a development project.
type ProjectStatus string

const (
	ProjectStatusPlanning  ProjectStatus = "planning"
	ProjectStatusActive    ProjectStatus = "active"
	ProjectStatusSelling   ProjectStatus = "selling"
	ProjectStatusCompleted ProjectStatus = "completed"
)

// Valid returns true if s is a recognized ProjectStatus.
func (s ProjectStatus) Valid() bool {
	switch s {
	case ProjectStatusPlanning, ProjectStatusActive, ProjectStatusSelling, ProjectStatusCompleted:
		return true
	}
	return false
}

// CanTransitionTo returns true if transitioning from s to next is a legal status change.
// Legal transitions (one-way):
//
//	planning → active    (ground broken, construction starts)
//	active   → selling   (units listed for sale)
//	selling  → completed (project wrapped up)
func (s ProjectStatus) CanTransitionTo(next ProjectStatus) bool {
	switch s {
	case ProjectStatusPlanning:
		return next == ProjectStatusActive
	case ProjectStatusActive:
		return next == ProjectStatusSelling
	case ProjectStatusSelling:
		return next == ProjectStatusCompleted
	}
	return false
}

// Project is a real-estate development project.
// It is the primary accumulator of capitalized development costs.
// Phase 5: all CostEntry records are tagged to a project_id; the allocation
// engine distributes project-level costs to units using this as the boundary.
type Project struct {
	ID        uint64          `gorm:"primaryKey;autoIncrement"                              json:"id"`
	TenantID  uint64          `gorm:"not null;index"                                        json:"-"`
	Name      string          `gorm:"not null;size:200"                                     json:"name"`
	Status    ProjectStatus   `gorm:"not null;size:20;default:'planning'"                   json:"status"`
	StartDate *time.Time      `                                                             json:"start_date,omitempty"`
	LandArea  decimal.Decimal `gorm:"type:DECIMAL(20,4);not null;default:'0.0000'"          json:"land_area"` // sqm — NOT money
	// TaxCategory: PENENTU tarif pajak (blueprint §7 — Increment 4). CHECK di DB
	// (subsidi|komersial); default komersial (tarif tertinggi, konservatif).
	TaxCategory domain.TaxCategory `gorm:"size:20;default:'komersial'"                       json:"tax_category"`
	Notes     string          `gorm:"size:2000"                                             json:"notes,omitempty"`
	CreatedAt time.Time       `                                                             json:"created_at"`
	UpdatedAt time.Time       `                                                             json:"updated_at"`
}

func (Project) TableName() string { return "projects" }

// ── ProjectPhase ──────────────────────────────────────────────────────────────

// PhaseStatus is the lifecycle state of a project phase.
type PhaseStatus string

const (
	PhaseStatusPlanning  PhaseStatus = "planning"
	PhaseStatusActive    PhaseStatus = "active"
	PhaseStatusCompleted PhaseStatus = "completed"
)

// Valid returns true if s is a recognized PhaseStatus.
func (s PhaseStatus) Valid() bool {
	switch s {
	case PhaseStatusPlanning, PhaseStatusActive, PhaseStatusCompleted:
		return true
	}
	return false
}

// CanTransitionTo returns true if the transition from s to next is legal.
//
//	planning → active    (phase construction started)
//	active   → completed (phase wrapped up)
func (s PhaseStatus) CanTransitionTo(next PhaseStatus) bool {
	switch s {
	case PhaseStatusPlanning:
		return next == PhaseStatusActive
	case PhaseStatusActive:
		return next == PhaseStatusCompleted
	}
	return false
}

// ProjectPhase is an optional sub-division of a Project.
// Units and cost entries can be tagged to a phase for per-phase reporting.
type ProjectPhase struct {
	ID          uint64      `gorm:"primaryKey;autoIncrement"           json:"id"`
	TenantID    uint64      `gorm:"not null;index"                     json:"-"`
	ProjectID   uint64      `gorm:"not null;index"                     json:"project_id"`
	Name        string      `gorm:"not null;size:100"                  json:"name"`        // e.g. "Fase 1", "Flagship"
	Description string      `gorm:"size:500"                           json:"description,omitempty"`
	TargetUnits int         `gorm:"not null;default:0"                 json:"target_units"`
	Status      PhaseStatus `gorm:"not null;size:20;default:'planning'" json:"status"`
	CreatedAt   time.Time   `                                          json:"created_at"`
	UpdatedAt   time.Time   `                                          json:"updated_at"`
}

func (ProjectPhase) TableName() string { return "project_phases" }

// ── Unit ──────────────────────────────────────────────────────────────────────

// UnitStatus is the lifecycle state of a saleable unit (Increment 6, blueprint §9).
//
// Kompatibilitas dikunci: tiga nilai lama (available|reserved|sold) TIDAK di-rename;
// data & reader lama tetap sah. Enam nilai baru additive. `sold` = titik BAST
// (serah terima terjadi) — alias blueprint "BAST" dipertahankan (D1).
type UnitStatus string

const (
	UnitStatusAvailable   UnitStatus = "available"   // siap dijual (existing)
	UnitStatusBooked      UnitStatus = "booked"      // booking + booking fee (baru — konsumen: Increment Booking)
	UnitStatusReserved    UnitStatus = "reserved"    // DP/komitmen buyer (existing)
	UnitStatusPPJB        UnitStatus = "ppjb"        // Sale Contract ditandatangani (baru)
	UnitStatusSold        UnitStatus = "sold"        // = BAST/serah terima terjadi (existing; alias BAST)
	UnitStatusOccupied    UnitStatus = "occupied"    // serah fisik/dihuni pasca-BAST (baru)
	UnitStatusHold        UnitStatus = "hold"        // hold administratif (baru; ber-gate approval)
	UnitStatusBlocked     UnitStatus = "blocked"     // blokir legal/sengketa (baru; ber-gate approval)
	UnitStatusMaintenance UnitStatus = "maintenance" // perbaikan/renovasi dari occupied (baru)
)

// Valid returns true if s is a recognized UnitStatus.
func (s UnitStatus) Valid() bool {
	switch s {
	case UnitStatusAvailable, UnitStatusBooked, UnitStatusReserved, UnitStatusPPJB,
		UnitStatusSold, UnitStatusOccupied, UnitStatusHold, UnitStatusBlocked,
		UnitStatusMaintenance:
		return true
	}
	return false
}

// CanTransitionTo returns true if transitioning from s to next is legal
// (blueprint §9 state machine). Unit selalu TEPAT satu status; hanya transisi
// di matriks ini yang sah.
//
// Kompatibilitas existing (WAJIB tetap sah):
//
//	available → reserved  (shortcut cash — perilaku lama)
//	reserved  → available (pembatalan reservasi)
//	reserved  → sold      (BAST — unit lama tanpa tahap ppjb)
//
// Catatan: `sold → available` (cancelled_post_bast) DISEDIAKAN di matriks agar
// domain Cancellation kelak memakainya, tetapi endpoint manual MENOLAKnya sampai
// Cancellation dibangun (butuh jurnal pembalik — lihat service.ErrPostBASTCancellation).
func (s UnitStatus) CanTransitionTo(next UnitStatus) bool {
	switch s {
	case UnitStatusAvailable:
		return next == UnitStatusBooked || next == UnitStatusReserved ||
			next == UnitStatusHold || next == UnitStatusBlocked
	case UnitStatusBooked:
		return next == UnitStatusReserved || next == UnitStatusAvailable
	case UnitStatusReserved:
		return next == UnitStatusPPJB || next == UnitStatusSold || next == UnitStatusAvailable
	case UnitStatusPPJB:
		return next == UnitStatusSold || next == UnitStatusAvailable
	case UnitStatusSold:
		return next == UnitStatusOccupied || next == UnitStatusAvailable // available hanya via Cancellation
	case UnitStatusOccupied:
		return next == UnitStatusMaintenance
	case UnitStatusHold:
		return next == UnitStatusAvailable
	case UnitStatusBlocked:
		return next == UnitStatusAvailable
	case UnitStatusMaintenance:
		return next == UnitStatusOccupied
	}
	return false
}

// RequiresApproval reports whether a transition into next is governance-gated
// (opt-in approval, TargetType `unit_transition`). Blueprint §9 / Increment 6 D2:
// hold & blocked adalah keputusan administratif/legal yang perlu approval.
func (next UnitStatus) RequiresApproval() bool {
	return next == UnitStatusHold || next == UnitStatusBlocked
}

// ── TransitionEvent (typed SSOT — pola scheme.Event) ──────────────────────────

// TransitionEvent adalah pemicu bisnis sebuah transisi status unit. Typed agar
// audit "kenapa unit berubah status" bisa dijawab tanpa menebak dari (from,to).
type TransitionEvent string

const (
	EventBookingCreated       TransitionEvent = "booking_created"
	EventBookingExpired       TransitionEvent = "booking_expired"
	EventBookingCancelled     TransitionEvent = "booking_cancelled"
	EventReservationConfirmed TransitionEvent = "reservation_confirmed"
	EventReservationCancelled TransitionEvent = "reservation_cancelled"
	EventContractSigned       TransitionEvent = "contract_signed"
	EventContractCancelled    TransitionEvent = "contract_cancelled"
	EventAkadExecuted         TransitionEvent = "akad_executed"
	EventCancelledPostBAST    TransitionEvent = "cancelled_post_bast"
	EventPhysicallyOccupied   TransitionEvent = "physically_occupied"
	EventAdminHold            TransitionEvent = "admin_hold"
	EventHoldReleased         TransitionEvent = "hold_released"
	EventLegalBlock           TransitionEvent = "legal_block"
	EventUnblocked            TransitionEvent = "unblocked"
	EventMaintenanceStarted   TransitionEvent = "maintenance_started"
	EventMaintenanceFinished  TransitionEvent = "maintenance_finished"
	EventManual               TransitionEvent = "manual"   // transisi operator via endpoint (kompat)
	EventBackfill             TransitionEvent = "backfill" // baris sintetis titik awal audit
)

// Valid returns true if e is a recognized TransitionEvent.
func (e TransitionEvent) Valid() bool {
	switch e {
	case EventBookingCreated, EventBookingExpired, EventBookingCancelled,
		EventReservationConfirmed, EventReservationCancelled, EventContractSigned,
		EventContractCancelled, EventAkadExecuted, EventCancelledPostBAST,
		EventPhysicallyOccupied, EventAdminHold, EventHoldReleased, EventLegalBlock,
		EventUnblocked, EventMaintenanceStarted, EventMaintenanceFinished,
		EventManual, EventBackfill:
		return true
	}
	return false
}

// eventTransitions memetakan setiap event bisnis ke pasangan (from, to) yang
// boleh ia labeli (Increment 6.1 / F-4 — koherensi event↔transisi, SSOT dari
// state diagram blueprint §9). Tanpa peta ini, endpoint manual bisa merekam
// label sebab yang bohong (mis. event=bast_executed pada available→reserved).
//
// EventManual sengaja TIDAK ada di peta: ia sah untuk transisi legal mana pun
// (kompat operator). EventBackfill juga tidak ada: hanya untuk baris sintetis
// migration, ditolak di jalur API.
var eventTransitions = map[TransitionEvent][][2]UnitStatus{
	EventBookingCreated:       {{UnitStatusAvailable, UnitStatusBooked}},
	EventBookingExpired:       {{UnitStatusBooked, UnitStatusAvailable}},
	EventBookingCancelled:     {{UnitStatusBooked, UnitStatusAvailable}},
	EventReservationConfirmed: {{UnitStatusAvailable, UnitStatusReserved}, {UnitStatusBooked, UnitStatusReserved}},
	EventReservationCancelled: {{UnitStatusReserved, UnitStatusAvailable}},
	EventContractSigned:       {{UnitStatusReserved, UnitStatusPPJB}},
	EventContractCancelled:    {{UnitStatusPPJB, UnitStatusAvailable}},
	EventAkadExecuted:         {{UnitStatusReserved, UnitStatusSold}, {UnitStatusPPJB, UnitStatusSold}},
	EventCancelledPostBAST:    {{UnitStatusSold, UnitStatusAvailable}},
	EventPhysicallyOccupied:   {{UnitStatusSold, UnitStatusOccupied}},
	EventAdminHold:            {{UnitStatusAvailable, UnitStatusHold}},
	EventHoldReleased:         {{UnitStatusHold, UnitStatusAvailable}},
	EventLegalBlock:           {{UnitStatusAvailable, UnitStatusBlocked}},
	EventUnblocked:            {{UnitStatusBlocked, UnitStatusAvailable}},
	EventMaintenanceStarted:   {{UnitStatusOccupied, UnitStatusMaintenance}},
	EventMaintenanceFinished:  {{UnitStatusMaintenance, UnitStatusOccupied}},
}

// Allows reports whether event e may label the transition from → to.
// Prasyarat: (from, to) sudah lolos CanTransitionTo (matriks divalidasi dulu).
func (e TransitionEvent) Allows(from, to UnitStatus) bool {
	if e == EventManual {
		return true // operator generik — matriks tetap penjaganya
	}
	pairs, ok := eventTransitions[e]
	if !ok {
		return false // backfill & event tak terpetakan: tidak sah via jalur API
	}
	for _, p := range pairs {
		if p[0] == from && p[1] == to {
			return true
		}
	}
	return false
}

// ── UnitStatusTransition (audit trail — APPEND-ONLY) ──────────────────────────

// Reference types (polimorfik) untuk asal transisi.
const (
	RefTypeManual          = "manual"
	RefTypeBooking         = "booking"
	RefTypeSaleContract    = "sale_contract"
	RefTypeSaleRecord      = "sale_record"
	RefTypeCancellation    = "cancellation"
	RefTypeApprovalRequest = "approval_request"
	RefTypeBackfill        = "backfill"
)

// UnitStatusTransition mencatat satu perubahan status unit. Tidak pernah
// di-update/delete (Invariant #5 append-only) — koreksi = transisi balik baru.
// Owner: Unit owns status + transition history (blueprint §9).
type UnitStatusTransition struct {
	ID            uint64          `gorm:"primaryKey;autoIncrement"          json:"id"`
	TenantID      uint64          `gorm:"not null;index"                    json:"-"`
	UnitID        uint64          `gorm:"not null;index"                    json:"unit_id"`
	FromStatus    UnitStatus      `gorm:"size:20;not null;default:''"       json:"from_status"`
	ToStatus      UnitStatus      `gorm:"size:20;not null"                  json:"to_status"`
	Event         TransitionEvent `gorm:"size:40;not null"                  json:"event"`
	EventDate     time.Time       `gorm:"not null"                          json:"event_date"`
	ReferenceType string          `gorm:"size:30;not null;default:'manual'" json:"reference_type"`
	ReferenceID   *uint64         `                                         json:"reference_id,omitempty"`
	ActorID       *uint64         `                                         json:"actor_id,omitempty"`
	Notes         string          `gorm:"size:500"                          json:"notes,omitempty"`
	CreatedAt     time.Time       `                                         json:"created_at"`
	UpdatedAt     time.Time       `                                         json:"-"`
}

func (UnitStatusTransition) TableName() string { return "unit_status_transitions" }

// Unit is a single saleable unit within a project/phase.
//
// Costs are NOT stored as a single total_cost field here — Phase 5's allocation
// engine maintains per-category cost snapshots in a separate table.
// Phase 5: unit_cost_snapshots table holds (unit_id, category, amount) rows.
// Phase 7: sale_date, sale_price, buyer_ref are populated when the unit is sold.
type Unit struct {
	ID           uint64           `gorm:"primaryKey;autoIncrement"                     json:"id"`
	TenantID     uint64           `gorm:"not null;index"                               json:"-"`
	ProjectID    uint64           `gorm:"not null;index"                               json:"project_id"`
	PhaseID      *uint64          `gorm:"index"                                        json:"phase_id,omitempty"`
	Code         string           `gorm:"not null;size:50"                             json:"code"`          // e.g. "LITHOS-A01"
	UnitType     string           `gorm:"not null;size:50"                             json:"unit_type"`     // kode product type (master product_types) — legacy bebas
	// TypeLabel: label komersial tipe (mis. "36/72") — atribut katalog, bukan uang.
	TypeLabel    *string          `gorm:"size:30"                                      json:"type_label,omitempty"`
	SaleableArea decimal.Decimal  `gorm:"type:DECIMAL(20,4);not null;default:'0.0000'" json:"saleable_area"` // sqm — NOT money
	// LandArea (LT-2, kelebihan-tanah-final-architecture §B.5): luas TANAH unit,
	// TERPISAH dari SaleableArea (luas bangunan). Default 0 — tidak backfill dari
	// SaleableArea maupun projects.land_area (keduanya terbukti tidak reliable
	// untuk tujuan ini). Bobot unit ini di pool alokasi Tanah = LandArea, bukan
	// SaleableArea; 0 berarti unit belum diisi admin → bobot Tanah nol (fail-safe,
	// BUKAN fail-closed — unit tetap dapat porsi Hard/Soft/Financing normal).
	LandArea     decimal.Decimal  `gorm:"type:DECIMAL(20,4);not null;default:'0.0000'" json:"land_area"`     // sqm — NOT money
	ListPrice    domain.Money     `gorm:"type:DECIMAL(20,4);not null;default:'0.0000'" json:"list_price"`    // rupiah bulat
	Status       UnitStatus       `gorm:"not null;size:20;default:'available'"         json:"status"`
	BuyerRef     *string          `gorm:"size:200"                                     json:"buyer_ref,omitempty"`
	// BuyerName/BuyerSource TIDAK disimpan — diturunkan saat baca oleh
	// AttachBuyerNames (buyer_name.go). Disimpan berarti dua salinan nama yang
	// bisa berbeda; diturunkan berarti semua layar menjawab sama.
	BuyerName    string           `gorm:"-"                                            json:"buyer_name,omitempty"`
	BuyerSource  string           `gorm:"-"                                            json:"buyer_source,omitempty"`
	SaleDate     *time.Time       `                                                    json:"sale_date,omitempty"`
	SalePrice    *decimal.Decimal `gorm:"type:DECIMAL(20,4)"                           json:"sale_price,omitempty"` // rupiah bulat; set at BAST
	CreatedAt    time.Time        `                                                    json:"created_at"`
	UpdatedAt    time.Time        `                                                    json:"updated_at"`
}

func (Unit) TableName() string { return "units" }

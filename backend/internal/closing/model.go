// Package closing mengimplementasikan P0-4 — Project Completion & HPP True-Up
// (docs/p0-4-completion-true-up-design.md, design v3 FINAL, D1–D4 + IMPL-1..5
// LOCKED). Proses CLOSING akuntansi, bukan fungsi sekali jalan:
//
//	Completion : draft → completed → finalized        (gate true-up)
//	True-up    : draft → calculated → approved → posted (+ cancelled)
//
// Hanya approved→posted yang memposting jurnal; posted immutable (Invariant #5).
// Semua saldo derived dari ledger; tabel run/line = dokumen proses, bukan saldo.
package closing

import (
	"time"

	"esaproperti/internal/domain"
)

// ── Completion (project_completion_events, migration 000031) ──────────────────

type CompletionStatus string

const (
	CompletionDraft     CompletionStatus = "draft"
	CompletionCompleted CompletionStatus = "completed"
	CompletionFinalized CompletionStatus = "finalized" // terminal-maju (IMPL-3)
)

// CanTransitionTo: draft→completed→finalized; finalized tidak bisa mundur.
func (s CompletionStatus) CanTransitionTo(next CompletionStatus) bool {
	switch s {
	case CompletionDraft:
		return next == CompletionCompleted
	case CompletionCompleted:
		return next == CompletionFinalized
	}
	return false
}

// CompletionEvent adalah event akuntansi "konstruksi selesai" per scope
// (D3: phase_id NULL = whole project; implementasi pertama project-level).
type CompletionEvent struct {
	ID        uint64           `gorm:"primaryKey;autoIncrement" json:"id"`
	TenantID  uint64           `gorm:"not null;index"           json:"-"`
	ProjectID uint64           `gorm:"not null"                 json:"project_id"`
	PhaseID   *uint64          `                                json:"phase_id,omitempty"`
	Status    CompletionStatus `gorm:"size:20;not null;default:'draft'" json:"status"`
	// CompletedAt = tanggal akuntansi konstruksi selesai (kejadian bisnis).
	CompletedAt *time.Time `json:"completed_at,omitempty"`
	CompletedBy *uint64    `json:"completed_by,omitempty"`
	// ActualCostFinalizedAt: saat A_c dikunci → membuka true-up.
	ActualCostFinalizedAt *time.Time `json:"actual_cost_finalized_at,omitempty"`
	CreatedBy             *uint64    `json:"created_by,omitempty"`
	CreatedAt             time.Time  `json:"created_at"`
	UpdatedAt             time.Time  `json:"updated_at"`
}

func (CompletionEvent) TableName() string { return "project_completion_events" }

// ── True-up (hpp_trueup_runs + hpp_trueup_lines, migration 000032) ────────────

type RunStatus string

const (
	RunDraft      RunStatus = "draft"
	RunCalculated RunStatus = "calculated"
	RunApproved   RunStatus = "approved"
	RunPosted     RunStatus = "posted"    // terminal; immutable
	RunCancelled  RunStatus = "cancelled" // terminal
)

// CanTransitionTo (IMPL-3): draft→calculated→approved→posted; cancel dari
// semua non-terminal. Recalculate hanya draft/calculated (di-service).
func (s RunStatus) CanTransitionTo(next RunStatus) bool {
	switch s {
	case RunDraft:
		return next == RunCalculated || next == RunCancelled
	case RunCalculated:
		return next == RunCalculated || next == RunApproved || next == RunCancelled // calculated→calculated = recalc
	case RunApproved:
		return next == RunPosted || next == RunCancelled
	}
	return false
}

// TrueupRun adalah satu proses closing true-up per scope. Maksimal SATU run
// non-cancelled per (tenant, project, phase) — UNIQUE active_scope di DB (TU-5).
type TrueupRun struct {
	ID        uint64    `gorm:"primaryKey;autoIncrement" json:"id"`
	TenantID  uint64    `gorm:"not null;index"           json:"-"`
	ProjectID uint64    `gorm:"not null"                 json:"project_id"`
	PhaseID   *uint64   `                                json:"phase_id,omitempty"`
	Status    RunStatus `gorm:"size:20;not null;default:'draft'" json:"status"`
	// AllocationConfigVersionID: SATU version basis untuk seluruh scope (IMPL-1).
	AllocationConfigVersionID *uint64 `json:"allocation_config_version_id,omitempty"`

	BudgetHPPTotal  domain.Money `gorm:"type:DECIMAL(20,4);not null;default:'0.0000'" json:"budget_hpp_total"`
	ActualCostTotal domain.Money `gorm:"type:DECIMAL(20,4);not null;default:'0.0000'" json:"actual_cost_total"`
	VarianceTotal   domain.Money `gorm:"type:DECIMAL(20,4);not null;default:'0.0000'" json:"variance_total"`

	// Audit actor per transisi (IMPL-5).
	CreatedBy    *uint64    `json:"created_by,omitempty"`
	CalculatedAt *time.Time `json:"calculated_at,omitempty"`
	CalculatedBy *uint64    `json:"calculated_by,omitempty"`
	ApprovedAt   *time.Time `json:"approved_at,omitempty"`
	ApprovedBy   *uint64    `json:"approved_by,omitempty"`
	PostedAt     *time.Time `json:"posted_at,omitempty"`
	PostedBy     *uint64    `json:"posted_by,omitempty"`
	CancelledAt  *time.Time `json:"cancelled_at,omitempty"`
	CancelledBy  *uint64    `json:"cancelled_by,omitempty"`
	// JournalID: jurnal adjustment — diisi SEKALI saat posted (UNIQUE, IMPL-4).
	// NULL setelah posted = tidak ada variance yang perlu dijurnal.
	JournalID *uint64 `json:"journal_id,omitempty"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`

	Lines []TrueupLine `gorm:"-" json:"lines,omitempty"`
}

func (TrueupRun) TableName() string { return "hpp_trueup_runs" }

// TrueupLine adalah rincian per (unit, accounting_class): budgeted (dari
// snapshot BAST) vs actual (alokasi A_c). Unsold: budgeted 0, variance 0,
// journal NULL — act_HPP-nya menjadi HPP FINALIZED untuk BAST pasca-completion (D2).
type TrueupLine struct {
	ID               uint64       `gorm:"primaryKey;autoIncrement" json:"id"`
	TenantID         uint64       `gorm:"not null;index"           json:"-"`
	RunID            uint64       `gorm:"not null;index"           json:"run_id"`
	UnitID           uint64       `gorm:"not null"                 json:"unit_id"`
	UnitNameSnapshot string       `gorm:"size:100;not null;default:''" json:"unit_name_snapshot"`
	Category         string       `gorm:"size:20;not null"         json:"category"` // accounting_class
	IsSold           bool         `gorm:"not null;default:false"   json:"is_sold"`
	BudgetedAmount   domain.Money `gorm:"type:DECIMAL(20,4);not null;default:'0.0000'" json:"budgeted_amount"`
	ActualAmount     domain.Money `gorm:"type:DECIMAL(20,4);not null;default:'0.0000'" json:"actual_amount"`
	VarianceAmount   domain.Money `gorm:"type:DECIMAL(20,4);not null;default:'0.0000'" json:"variance_amount"`
	SnapshotID       *uint64      `json:"snapshot_id,omitempty"`
	JournalEntryID   *uint64      `json:"journal_entry_id,omitempty"`
	CreatedAt        time.Time    `json:"created_at"`
	UpdatedAt        time.Time    `json:"updated_at"`
}

func (TrueupLine) TableName() string { return "hpp_trueup_lines" }

// VariancePreview adalah hasil GET hpp-variance (read-only, Req #4).
type VariancePreview struct {
	ProjectID        uint64           `json:"project_id"`
	CompletionStatus CompletionStatus `json:"completion_status"` // '' bila belum ada event
	Basis            string           `json:"basis"`
	ConfigVersionID  *uint64          `json:"allocation_config_version_id,omitempty"`
	BudgetHPPTotal   domain.Money     `json:"budget_hpp_total"`
	ActualCostTotal  domain.Money     `json:"actual_cost_total"`
	VarianceTotal    domain.Money     `json:"variance_total"`
	SoldUnits        int              `json:"sold_units"`
	UnsoldUnits      int              `json:"unsold_units"`
	Lines            []TrueupLine     `json:"lines"`
}

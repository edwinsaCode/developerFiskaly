package approval

// Increment 5 — Generic Approval Workflow (blueprint blueprint-domain-additions.md §10).
// CORE governance, cross-cutting. SATU mekanisme approval untuk semua modul via
// target POLIMORFIK (target_type + target_id). TANPA dampak ledger — hanya
// MEM-GATE transisi/posting modul konsumen.
//
// Governance OPT-IN (additive): modul digate hanya bila workflow AKTIF untuk
// target_type-nya terkonfigurasi. Tenant tanpa konfigurasi berperilaku lama.

import (
	"time"

	"esaproperti/internal/domain"
)

// ── TargetType (vocabulary konsumen — SSOT bertipe) ───────────────────────────

// TargetType adalah jenis dokumen yang bisa digate approval (blueprint §10 tabel).
type TargetType string

const (
	TargetRAB           TargetType = "rab"
	TargetHPPTrueup     TargetType = "hpp_trueup"
	TargetRefund        TargetType = "refund"
	TargetCancellation  TargetType = "cancellation"
	TargetCommission    TargetType = "commission"
	TargetDiscount      TargetType = "discount"
	TargetCost           TargetType = "cost"
	TargetPayment        TargetType = "payment"
	TargetManualJournal  TargetType = "manual_journal"
	TargetUnitTransition TargetType = "unit_transition" // Increment 6 D2: gate hold/blocked
)

func (t TargetType) Valid() bool {
	switch t {
	case TargetRAB, TargetHPPTrueup, TargetRefund, TargetCancellation,
		TargetCommission, TargetDiscount, TargetCost, TargetPayment, TargetManualJournal,
		TargetUnitTransition:
		return true
	}
	return false
}

// ── Status & Decision ─────────────────────────────────────────────────────────

// RequestStatus mengikuti lifecycle blueprint §10:
// pending → in_review → approved | rejected; pending/in_review → cancelled.
type RequestStatus string

const (
	StatusPending   RequestStatus = "pending"
	StatusInReview  RequestStatus = "in_review"
	StatusApproved  RequestStatus = "approved"
	StatusRejected  RequestStatus = "rejected"
	StatusCancelled RequestStatus = "cancelled"
)

// Terminal: approved/rejected/cancelled tidak bisa berubah lagi.
func (s RequestStatus) Terminal() bool {
	return s == StatusApproved || s == StatusRejected || s == StatusCancelled
}

type Decision string

const (
	DecisionApprove Decision = "approve"
	DecisionReject  Decision = "reject"
	// DecisionDelegate (H4 — SEAM): vocabulary siap; eksekusi delegasi BELUM
	// diimplementasikan (Act mengembalikan ErrDelegateNotImplemented).
	DecisionDelegate Decision = "delegate"
	// DecisionCancel: jejak audit pembatalan request oleh pemohon (ditulis
	// otomatis oleh Cancel — bukan input Act).
	DecisionCancel Decision = "cancel"
)

// Valid: keputusan yang boleh DIAJUKAN via Act (cancel lewat endpoint cancel).
func (d Decision) Valid() bool {
	return d == DecisionApprove || d == DecisionReject || d == DecisionDelegate
}

// ── Config: Workflow + Step ───────────────────────────────────────────────────

// Workflow adalah template approval per (tenant, target_type). Hanya SATU yang
// aktif per target_type (UNIQUE active_key — pola budget_plans).
type Workflow struct {
	ID         uint64     `gorm:"primaryKey;autoIncrement" json:"id"`
	TenantID   uint64     `gorm:"not null;index"           json:"-"`
	TargetType TargetType `gorm:"not null;size:30"         json:"target_type"`
	Name       string     `gorm:"not null;size:200"        json:"name"`
	// Revision (H3): revisi konfigurasi per (tenant, target_type). Baris
	// workflow IMMUTABLE by construction (tidak ada jalur edit step/nama) —
	// "mengubah workflow" = nonaktifkan lama + buat baris baru (revision naik).
	Revision  int     `gorm:"not null;default:1"       json:"revision"`
	IsActive  bool    `gorm:"not null;default:true"    json:"is_active"`
	ActiveKey *string `gorm:"size:1"                   json:"-"`
	CreatedBy  *uint64    `json:"created_by,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
	// Steps dimuat eksplisit oleh repository (bukan preload otomatis).
	Steps []Step `gorm:"-" json:"steps,omitempty"`
}

func (Workflow) TableName() string { return "approval_workflows" }

// Step adalah satu tahap approval dalam workflow. Berurutan (seq 1..n).
// min_amount = threshold Approval Matrix (step di-skip bila amount request di
// bawahnya); quorum = jumlah persetujuan yang dibutuhkan pada step ini.
type Step struct {
	ID           uint64       `gorm:"primaryKey;autoIncrement" json:"id"`
	TenantID     uint64       `gorm:"not null;index"           json:"-"`
	WorkflowID   uint64       `gorm:"not null;index"           json:"workflow_id"`
	Seq          int          `gorm:"not null"                 json:"seq"`
	Name         string       `gorm:"size:200"                 json:"name"`
	ApproverRole string       `gorm:"not null;size:30"         json:"approver_role"`
	MinAmount    domain.Money `gorm:"type:DECIMAL(20,4);not null;default:'0.0000'" json:"min_amount"`
	Quorum       int          `gorm:"not null;default:1"       json:"quorum"`
	// Eskalasi (H4 — SEAM): vocabulary siap; scheduler/otomasi BELUM dibangun.
	EscalationAfterHours int       `gorm:"not null;default:0" json:"escalation_after_hours,omitempty"`
	EscalationRole       string    `gorm:"size:30"            json:"escalation_role,omitempty"`
	CreatedAt            time.Time `json:"created_at"`
	UpdatedAt            time.Time `json:"updated_at"`
}

func (Step) TableName() string { return "approval_steps" }

// AppliesTo: step berlaku untuk request bila amount ≥ min_amount (0 = selalu).
func (s Step) AppliesTo(amount domain.Money) bool {
	return !amount.Decimal().LessThan(s.MinAmount.Decimal())
}

// ── Runtime: Request + Action ─────────────────────────────────────────────────

// Request adalah satu instance approval untuk sebuah dokumen target (polimorfik).
// Satu request AKTIF per target (UNIQUE active_key).
type Request struct {
	ID          uint64        `gorm:"primaryKey;autoIncrement" json:"id"`
	TenantID    uint64        `gorm:"not null;index"           json:"-"`
	WorkflowID  uint64        `gorm:"not null"                 json:"workflow_id"`
	TargetType  TargetType    `gorm:"not null;size:30"         json:"target_type"`
	TargetID    uint64        `gorm:"not null"                 json:"target_id"`
	Status      RequestStatus `gorm:"not null;size:20;default:'pending'" json:"status"`
	CurrentSeq  int           `gorm:"not null;default:1"       json:"current_seq"`
	Amount      domain.Money  `gorm:"type:DECIMAL(20,4);not null;default:'0.0000'" json:"amount"`
	ActiveKey *string `gorm:"size:1" json:"-"`
	// Snapshot beku (Increment 5.1). NULL = request pra-hardening (fallback
	// evaluasi ke workflow hidup — kompat).
	WorkflowRevision   *int    `json:"workflow_revision,omitempty"`
	WorkflowSnapshotJS *string `gorm:"column:workflow_snapshot;type:json" json:"workflow_snapshot,omitempty"`
	TargetSnapshotJS   *string `gorm:"column:target_snapshot;type:json"   json:"target_snapshot,omitempty"`
	RequestedBy        *uint64 `json:"requested_by,omitempty"`
	Notes              string  `gorm:"size:500"                 json:"notes,omitempty"`
	DecidedAt   *time.Time    `json:"decided_at,omitempty"`
	CreatedAt   time.Time     `json:"created_at"`
	UpdatedAt   time.Time     `json:"updated_at"`
	// Actions dimuat eksplisit oleh repository untuk respons detail.
	Actions []Action `gorm:"-" json:"actions,omitempty"`
}

func (Request) TableName() string { return "approval_requests" }

// Action adalah satu keputusan pada satu step — APPEND-ONLY (tidak ada jalur
// update/delete di seluruh aplikasi). Audit trail lengkap siapa-kapan-apa.
type Action struct {
	ID        uint64    `gorm:"primaryKey;autoIncrement" json:"id"`
	TenantID  uint64    `gorm:"not null;index"           json:"-"`
	RequestID uint64    `gorm:"not null;index"           json:"request_id"`
	StepSeq   int       `gorm:"not null"                 json:"step_seq"`
	ActorID   uint64    `gorm:"not null"                 json:"actor_id"`
	ActorRole string    `gorm:"not null;size:30"         json:"actor_role"`
	Decision  Decision  `gorm:"not null;size:20"         json:"decision"`
	Comment   string    `gorm:"size:500"                 json:"comment,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (Action) TableName() string { return "approval_actions" }

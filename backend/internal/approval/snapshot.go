package approval

// Increment 5.1 (H1/H2) — snapshot beku pada Approval Request, pola yang sama
// dengan Payment Scheme Terms Snapshot / Tax Rule revision / Allocation
// Snapshot: histori approval TIDAK bergantung pada konfigurasi hidup.

import (
	"encoding/json"
	"fmt"
)

// StepSnapshot membekukan satu step (role/quorum/threshold/escalation) —
// evaluasi keputusan membaca INI, bukan approval_steps hidup.
type StepSnapshot struct {
	Seq                  int    `json:"seq"`
	Name                 string `json:"name,omitempty"`
	ApproverRole         string `json:"approver_role"`
	MinAmount            string `json:"min_amount"` // string decimal — tanpa float
	Quorum               int    `json:"quorum"`
	EscalationAfterHours int    `json:"escalation_after_hours,omitempty"` // seam H4
	EscalationRole       string `json:"escalation_role,omitempty"`        // seam H4
}

// WorkflowSnapshot adalah amplop beku workflow saat request dibuat (H1).
type WorkflowSnapshot struct {
	WorkflowID   uint64         `json:"workflow_id"`
	Revision     int            `json:"revision"`
	Name         string         `json:"name"`
	TargetType   TargetType     `json:"target_type"`
	Steps        []StepSnapshot `json:"steps"`
}

// NewWorkflowSnapshot membekukan workflow + seluruh step-nya.
func NewWorkflowSnapshot(w *Workflow) WorkflowSnapshot {
	steps := make([]StepSnapshot, len(w.Steps))
	for i, st := range w.Steps {
		steps[i] = StepSnapshot{
			Seq:                  st.Seq,
			Name:                 st.Name,
			ApproverRole:         st.ApproverRole,
			MinAmount:            st.MinAmount.String(),
			Quorum:               st.Quorum,
			EscalationAfterHours: st.EscalationAfterHours,
			EscalationRole:       st.EscalationRole,
		}
	}
	return WorkflowSnapshot{
		WorkflowID: w.ID,
		Revision:   w.Revision,
		Name:       w.Name,
		TargetType: w.TargetType,
		Steps:      steps,
	}
}

func (s WorkflowSnapshot) JSON() (string, error) {
	b, err := json.Marshal(s)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// ParseWorkflowSnapshot membaca snapshot beku dari request.
func ParseWorkflowSnapshot(raw string) (WorkflowSnapshot, error) {
	var s WorkflowSnapshot
	if raw == "" {
		return s, fmt.Errorf("workflow snapshot kosong")
	}
	if err := json.Unmarshal([]byte(raw), &s); err != nil {
		return s, fmt.Errorf("workflow snapshot rusak: %w", err)
	}
	return s, nil
}

// TargetSnapshot (H2) membekukan konteks minimum dokumen target saat submit.
// SEAM invalidation: bila dokumen berubah setelah approval diminta, konsumen
// dapat membandingkan TargetVersion/konteks — business rule invalidation BELUM
// diimplementasikan (sesuai permintaan review), hanya snapshot + seam.
type TargetSnapshot struct {
	TargetType TargetType `json:"target_type"`
	TargetID   uint64     `json:"target_id"`
	// TargetVersion: versi/updated-marker dokumen saat submit (seam — diisi
	// pemanggil bila dokumen punya versi; 0 = tidak diketahui).
	TargetVersion int    `json:"target_version,omitempty"`
	Amount        string `json:"amount"`             // string decimal
	Currency      string `json:"currency,omitempty"` // default IDR
	Notes         string `json:"notes,omitempty"`
}

func (s TargetSnapshot) JSON() (string, error) {
	b, err := json.Marshal(s)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func ParseTargetSnapshot(raw string) (TargetSnapshot, error) {
	var s TargetSnapshot
	if raw == "" {
		return s, fmt.Errorf("target snapshot kosong")
	}
	if err := json.Unmarshal([]byte(raw), &s); err != nil {
		return s, fmt.Errorf("target snapshot rusak: %w", err)
	}
	return s, nil
}

// ── Domain Event Seam (H6) ────────────────────────────────────────────────────

// EventKind adalah vocabulary event domain approval.
type EventKind string

const (
	EventApprovalApproved  EventKind = "approval.approved"
	EventApprovalRejected  EventKind = "approval.rejected"
	EventApprovalCancelled EventKind = "approval.cancelled"
)

// Event adalah fakta domain yang dipublikasikan engine saat request mencapai
// status terminal. Engine TIDAK PERNAH memanggil modul lain secara langsung —
// konsumen (Booking, notifikasi, dsb.) mendengarkan lewat EventSink.
type Event struct {
	Kind       EventKind  `json:"kind"`
	TenantID   uint64     `json:"tenant_id"`
	RequestID  uint64     `json:"request_id"`
	TargetType TargetType `json:"target_type"`
	TargetID   uint64     `json:"target_id"`
}

// EventSink adalah extension point publikasi event (H6). Belum ada bus/async/
// subscriber — implementasi default = noop. Kegagalan sink TIDAK membatalkan
// keputusan (fakta approval sudah persist; event hanyalah notifikasi).
type EventSink interface {
	Publish(event Event)
}

// noopSink: default — tidak melakukan apa pun.
type noopSink struct{}

func (noopSink) Publish(Event) {}

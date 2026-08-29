package project_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"esaproperti/internal/domain"
	"esaproperti/internal/project"
)

// ── Matriks state machine (pure) ──────────────────────────────────────────────

// TestUnitLifecycle_LegalMatrix membuktikan seluruh transisi sah (blueprint §9)
// diterima CanTransitionTo, dan kompatibilitas 3 nilai lama tetap terjaga.
func TestUnitLifecycle_LegalMatrix(t *testing.T) {
	legal := map[project.UnitStatus][]project.UnitStatus{
		project.UnitStatusAvailable:   {project.UnitStatusBooked, project.UnitStatusReserved, project.UnitStatusHold, project.UnitStatusBlocked},
		project.UnitStatusBooked:      {project.UnitStatusReserved, project.UnitStatusAvailable},
		project.UnitStatusReserved:    {project.UnitStatusPPJB, project.UnitStatusSold, project.UnitStatusAvailable},
		project.UnitStatusPPJB:        {project.UnitStatusSold, project.UnitStatusAvailable},
		project.UnitStatusSold:        {project.UnitStatusOccupied, project.UnitStatusAvailable},
		project.UnitStatusOccupied:    {project.UnitStatusMaintenance},
		project.UnitStatusHold:        {project.UnitStatusAvailable},
		project.UnitStatusBlocked:     {project.UnitStatusAvailable},
		project.UnitStatusMaintenance: {project.UnitStatusOccupied},
	}
	for from, tos := range legal {
		for _, to := range tos {
			if !from.CanTransitionTo(to) {
				t.Errorf("expected LEGAL: %s → %s", from, to)
			}
		}
	}

	// Kompatibilitas existing (WAJIB tetap sah).
	compat := [][2]project.UnitStatus{
		{project.UnitStatusAvailable, project.UnitStatusReserved},
		{project.UnitStatusReserved, project.UnitStatusAvailable},
		{project.UnitStatusReserved, project.UnitStatusSold},
	}
	for _, c := range compat {
		if !c[0].CanTransitionTo(c[1]) {
			t.Errorf("kompat lama harus sah: %s → %s", c[0], c[1])
		}
	}

	// Sanity: transisi acak di luar matriks ditolak.
	if project.UnitStatusAvailable.CanTransitionTo(project.UnitStatusSold) {
		t.Error("available → sold harus ilegal (lewat reserved dulu)")
	}
	if project.UnitStatusHold.CanTransitionTo(project.UnitStatusBlocked) {
		t.Error("hold → blocked harus ilegal")
	}
}

func TestTransitionEvent_Valid(t *testing.T) {
	valid := []project.TransitionEvent{
		project.EventBookingCreated, project.EventReservationConfirmed,
		project.EventContractSigned, project.EventAkadExecuted, project.EventAdminHold,
		project.EventManual, project.EventBackfill,
	}
	for _, e := range valid {
		if !e.Valid() {
			t.Errorf("event %q harus valid", e)
		}
	}
	if project.TransitionEvent("teleport").Valid() {
		t.Error("event tak dikenal harus invalid")
	}
}

func TestUnitStatus_RequiresApproval(t *testing.T) {
	if !project.UnitStatusHold.RequiresApproval() || !project.UnitStatusBlocked.RequiresApproval() {
		t.Error("hold & blocked harus ber-gate approval")
	}
	if project.UnitStatusReserved.RequiresApproval() || project.UnitStatusSold.RequiresApproval() {
		t.Error("reserved/sold tidak ber-gate")
	}
}

// ── Service: transisi menulis audit log append-only ───────────────────────────

func newUnitFixture(t *testing.T, svc *project.Service) uint64 {
	t.Helper()
	p, err := svc.CreateProject(context.Background(), 1, project.CreateProjectRequest{Name: "LITHOS"})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	u, err := svc.CreateUnit(context.Background(), 1, project.CreateUnitRequest{
		ProjectID: p.ID, Code: "A-01", UnitType: "villa",
		SaleableArea: decimal.NewFromFloat(100), ListPrice: domain.FromInt(1_000_000),
	})
	if err != nil {
		t.Fatalf("CreateUnit: %v", err)
	}
	return u.ID
}

func TestTransition_WritesAuditLog(t *testing.T) {
	svc := newTestService()
	unitID := newUnitFixture(t, svc)
	actor := uint64(42)
	evDate := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)

	buyer := "Budi"
	if _, err := svc.Transition(context.Background(), 1, unitID, project.TransitionRequest{
		Next:          project.UnitStatusReserved,
		Event:         project.EventReservationConfirmed,
		EventDate:     &evDate,
		ActorID:       &actor,
		Notes:         "DP masuk",
		ReferenceType: project.RefTypeManual,
		UpdateUnitOpts: project.UpdateUnitOpts{BuyerRef: &buyer},
	}); err != nil {
		t.Fatalf("Transition: %v", err)
	}

	ts, err := svc.ListTransitions(context.Background(), 1, unitID)
	if err != nil {
		t.Fatalf("ListTransitions: %v", err)
	}
	if len(ts) != 1 {
		t.Fatalf("want 1 transition, got %d", len(ts))
	}
	got := ts[0]
	if got.FromStatus != project.UnitStatusAvailable || got.ToStatus != project.UnitStatusReserved {
		t.Errorf("from/to = %s/%s", got.FromStatus, got.ToStatus)
	}
	if got.Event != project.EventReservationConfirmed {
		t.Errorf("event = %s", got.Event)
	}
	if got.ActorID == nil || *got.ActorID != actor {
		t.Errorf("actor = %v", got.ActorID)
	}
	if !got.EventDate.Equal(evDate) {
		t.Errorf("event_date = %v, want %v", got.EventDate, evDate)
	}
	if got.Notes != "DP masuk" {
		t.Errorf("notes = %q", got.Notes)
	}
}

// TestTransition_DefaultsEventManual: tanpa event eksplisit → tercatat 'manual'.
func TestTransition_DefaultsEventManual(t *testing.T) {
	svc := newTestService()
	unitID := newUnitFixture(t, svc)
	buyer := "x"
	if _, err := svc.TransitionUnit(context.Background(), 1, unitID, project.UnitStatusReserved, project.UpdateUnitOpts{BuyerRef: &buyer}); err != nil {
		t.Fatalf("TransitionUnit: %v", err)
	}
	ts, _ := svc.ListTransitions(context.Background(), 1, unitID)
	if len(ts) != 1 || ts[0].Event != project.EventManual {
		t.Fatalf("want 1 log event=manual, got %+v", ts)
	}
}

func TestTransition_UnknownEventRejected(t *testing.T) {
	svc := newTestService()
	unitID := newUnitFixture(t, svc)
	_, err := svc.Transition(context.Background(), 1, unitID, project.TransitionRequest{
		Next:  project.UnitStatusReserved,
		Event: project.TransitionEvent("teleport"),
	})
	if !errors.Is(err, project.ErrUnknownTransitionEvent) {
		t.Fatalf("want ErrUnknownTransitionEvent, got %v", err)
	}
	// Transisi ditolak ⇒ tak ada log tertulis (atomik).
	ts, _ := svc.ListTransitions(context.Background(), 1, unitID)
	if len(ts) != 0 {
		t.Fatalf("transisi gagal tidak boleh menulis log, got %d", len(ts))
	}
}

// TestTransition_NoLogOnIllegal: transisi ilegal tidak mengubah status & tak menulis log.
func TestTransition_NoLogOnIllegal(t *testing.T) {
	svc := newTestService()
	unitID := newUnitFixture(t, svc)
	if _, err := svc.TransitionUnit(context.Background(), 1, unitID, project.UnitStatusSold, project.UpdateUnitOpts{}); !errors.Is(err, project.ErrUnitInvalidTransition) {
		t.Fatalf("want ErrUnitInvalidTransition, got %v", err)
	}
	ts, _ := svc.ListTransitions(context.Background(), 1, unitID)
	if len(ts) != 0 {
		t.Fatalf("illegal transition tidak boleh menulis log, got %d", len(ts))
	}
}

// ── Approval gate (opt-in) ────────────────────────────────────────────────────

type mockApprovalGate struct {
	err       error
	lastUnit  uint64
	callCount int
}

func (m *mockApprovalGate) RequireApproved(_ context.Context, _ uint64, unitID uint64) error {
	m.callCount++
	m.lastUnit = unitID
	return m.err
}

// Tanpa gate terpasang → hold langsung (perilaku lama, opt-in).
func TestTransition_Hold_NoGate_Allowed(t *testing.T) {
	svc := newTestService()
	unitID := newUnitFixture(t, svc)
	if _, err := svc.Transition(context.Background(), 1, unitID, project.TransitionRequest{
		Next: project.UnitStatusHold, Event: project.EventAdminHold,
	}); err != nil {
		t.Fatalf("hold tanpa gate harus lolos, got %v", err)
	}
}

// Dengan gate menolak → hold ditolak dengan ErrApprovalRequired; status tak berubah.
func TestTransition_Hold_GateRejects(t *testing.T) {
	svc := newTestService()
	gate := &mockApprovalGate{err: project.ErrApprovalRequired}
	svc.SetApprovalGate(gate)
	unitID := newUnitFixture(t, svc)

	_, err := svc.Transition(context.Background(), 1, unitID, project.TransitionRequest{
		Next: project.UnitStatusHold, Event: project.EventAdminHold,
	})
	if !errors.Is(err, project.ErrApprovalRequired) {
		t.Fatalf("want ErrApprovalRequired, got %v", err)
	}
	if gate.callCount != 1 || gate.lastUnit != unitID {
		t.Errorf("gate dipanggil dgn unit %d (n=%d)", gate.lastUnit, gate.callCount)
	}
	u, _ := svc.GetUnit(context.Background(), 1, unitID)
	if u.Status != project.UnitStatusAvailable {
		t.Errorf("status harus tetap available, got %s", u.Status)
	}
	ts, _ := svc.ListTransitions(context.Background(), 1, unitID)
	if len(ts) != 0 {
		t.Errorf("gate menolak ⇒ tak ada log, got %d", len(ts))
	}
}

// Gate TIDAK dipanggil untuk transisi non-hold/blocked (mis. reserved).
func TestTransition_NonGated_SkipsGate(t *testing.T) {
	svc := newTestService()
	gate := &mockApprovalGate{err: project.ErrApprovalRequired}
	svc.SetApprovalGate(gate)
	unitID := newUnitFixture(t, svc)
	buyer := "y"
	if _, err := svc.Transition(context.Background(), 1, unitID, project.TransitionRequest{
		Next: project.UnitStatusReserved, Event: project.EventReservationConfirmed,
		UpdateUnitOpts: project.UpdateUnitOpts{BuyerRef: &buyer},
	}); err != nil {
		t.Fatalf("reserved tidak boleh ter-gate: %v", err)
	}
	if gate.callCount != 0 {
		t.Errorf("gate tidak boleh dipanggil utk reserved, n=%d", gate.callCount)
	}
}

// ── D2: pasca-BAST cancellation ditolak sampai domain Cancellation ────────────

func TestTransition_PostBASTCancellation_Rejected(t *testing.T) {
	svc := newTestService()
	unitID := newUnitFixture(t, svc)
	if err := forceUnitStatus(svc, unitID, project.UnitStatusSold); err != nil {
		t.Fatalf("setup sold: %v", err)
	}
	_, err := svc.Transition(context.Background(), 1, unitID, project.TransitionRequest{
		Next: project.UnitStatusAvailable, Event: project.EventCancelledPostBAST,
	})
	if !errors.Is(err, project.ErrPostBASTCancellation) {
		t.Fatalf("want ErrPostBASTCancellation, got %v", err)
	}
}

// ── Tenant isolation: ListTransitions ter-scope tenant ────────────────────────

func TestListTransitions_TenantScoped(t *testing.T) {
	svc := newTestService()
	unitID := newUnitFixture(t, svc) // tenant 1
	buyer := "z"
	if _, err := svc.TransitionUnit(context.Background(), 1, unitID, project.UnitStatusReserved, project.UpdateUnitOpts{BuyerRef: &buyer}); err != nil {
		t.Fatalf("transition: %v", err)
	}
	// Tenant 2 tidak boleh melihat unit/histori tenant 1.
	if _, err := svc.ListTransitions(context.Background(), 2, unitID); !errors.Is(err, project.ErrUnitNotFound) {
		t.Fatalf("cross-tenant ListTransitions want ErrUnitNotFound, got %v", err)
	}
}

// ── F-4 (Increment 6.1): koherensi event ↔ transisi ──────────────────────────

func TestEventAllows_Coherence(t *testing.T) {
	cases := []struct {
		event project.TransitionEvent
		from  project.UnitStatus
		to    project.UnitStatus
		want  bool
	}{
		// koheren
		{project.EventReservationConfirmed, project.UnitStatusAvailable, project.UnitStatusReserved, true},
		{project.EventReservationConfirmed, project.UnitStatusBooked, project.UnitStatusReserved, true},
		{project.EventContractSigned, project.UnitStatusReserved, project.UnitStatusPPJB, true},
		{project.EventAkadExecuted, project.UnitStatusReserved, project.UnitStatusSold, true},
		{project.EventAkadExecuted, project.UnitStatusPPJB, project.UnitStatusSold, true},
		{project.EventAdminHold, project.UnitStatusAvailable, project.UnitStatusHold, true},
		// manual = generik (matriks tetap penjaga)
		{project.EventManual, project.UnitStatusAvailable, project.UnitStatusReserved, true},
		{project.EventManual, project.UnitStatusOccupied, project.UnitStatusMaintenance, true},
		// tidak koheren — label bohong
		{project.EventAkadExecuted, project.UnitStatusAvailable, project.UnitStatusReserved, false},
		{project.EventAdminHold, project.UnitStatusAvailable, project.UnitStatusReserved, false},
		{project.EventContractSigned, project.UnitStatusAvailable, project.UnitStatusBooked, false},
		{project.EventBookingCreated, project.UnitStatusReserved, project.UnitStatusPPJB, false},
		// backfill: hanya migration — tidak pernah sah via jalur API
		{project.EventBackfill, project.UnitStatusAvailable, project.UnitStatusReserved, false},
	}
	for _, tc := range cases {
		if got := tc.event.Allows(tc.from, tc.to); got != tc.want {
			t.Errorf("Allows(%s, %s→%s) = %v, want %v", tc.event, tc.from, tc.to, got, tc.want)
		}
	}
}

func TestTransition_EventMismatchRejected(t *testing.T) {
	svc := newTestService()
	unitID := newUnitFixture(t, svc)
	// available → reserved dengan label bast_executed = bohong → ditolak.
	_, err := svc.Transition(context.Background(), 1, unitID, project.TransitionRequest{
		Next:  project.UnitStatusReserved,
		Event: project.EventAkadExecuted,
	})
	if !errors.Is(err, project.ErrEventTransitionMismatch) {
		t.Fatalf("want ErrEventTransitionMismatch, got %v", err)
	}
	// Ditolak sebelum menulis apa pun.
	ts, _ := svc.ListTransitions(context.Background(), 1, unitID)
	if len(ts) != 0 {
		t.Fatalf("mismatch tidak boleh menulis log, got %d", len(ts))
	}
}

func TestTransition_BackfillEventRejectedViaAPI(t *testing.T) {
	svc := newTestService()
	unitID := newUnitFixture(t, svc)
	_, err := svc.Transition(context.Background(), 1, unitID, project.TransitionRequest{
		Next:  project.UnitStatusReserved,
		Event: project.EventBackfill,
	})
	if !errors.Is(err, project.ErrEventTransitionMismatch) {
		t.Fatalf("backfill via API: want ErrEventTransitionMismatch, got %v", err)
	}
}

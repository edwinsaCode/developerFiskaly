package closing_test

import (
	"testing"

	"esaproperti/internal/closing"
)

// IMPL-3 — state machine guards (pure).

func TestCompletionStatus_Machine(t *testing.T) {
	c := closing.CompletionDraft
	if !c.CanTransitionTo(closing.CompletionCompleted) {
		t.Error("draft → completed harus sah")
	}
	if c.CanTransitionTo(closing.CompletionFinalized) {
		t.Error("draft → finalized harus ilegal (lewat completed dulu)")
	}
	if !closing.CompletionCompleted.CanTransitionTo(closing.CompletionFinalized) {
		t.Error("completed → finalized harus sah")
	}
	// finalized terminal-maju: tidak bisa mundur.
	for _, next := range []closing.CompletionStatus{closing.CompletionDraft, closing.CompletionCompleted, closing.CompletionFinalized} {
		if closing.CompletionFinalized.CanTransitionTo(next) {
			t.Errorf("finalized → %s harus ilegal", next)
		}
	}
}

func TestRunStatus_Machine(t *testing.T) {
	legal := [][2]closing.RunStatus{
		{closing.RunDraft, closing.RunCalculated},
		{closing.RunDraft, closing.RunCancelled},
		{closing.RunCalculated, closing.RunCalculated}, // recalc
		{closing.RunCalculated, closing.RunApproved},
		{closing.RunCalculated, closing.RunCancelled},
		{closing.RunApproved, closing.RunPosted},
		{closing.RunApproved, closing.RunCancelled},
	}
	for _, p := range legal {
		if !p[0].CanTransitionTo(p[1]) {
			t.Errorf("expected LEGAL: %s → %s", p[0], p[1])
		}
	}
	illegal := [][2]closing.RunStatus{
		{closing.RunDraft, closing.RunApproved},   // skip calculate
		{closing.RunDraft, closing.RunPosted},     // skip semuanya
		{closing.RunCalculated, closing.RunPosted}, // skip approve
		{closing.RunApproved, closing.RunCalculated}, // approved tak boleh recalc
		{closing.RunPosted, closing.RunCancelled},  // posted immutable
		{closing.RunPosted, closing.RunCalculated},
		{closing.RunCancelled, closing.RunDraft}, // cancelled terminal
	}
	for _, p := range illegal {
		if p[0].CanTransitionTo(p[1]) {
			t.Errorf("expected ILLEGAL: %s → %s", p[0], p[1])
		}
	}
}

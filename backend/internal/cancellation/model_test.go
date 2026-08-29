package cancellation_test

import (
	"testing"

	"esaproperti/internal/cancellation"
)

func TestCancellationStatus_Machine(t *testing.T) {
	legal := [][2]cancellation.Status{
		{cancellation.StatusRequested, cancellation.StatusApproved},
		{cancellation.StatusRequested, cancellation.StatusRejected},
		{cancellation.StatusApproved, cancellation.StatusProcessed},
		{cancellation.StatusApproved, cancellation.StatusRejected},
	}
	for _, p := range legal {
		if !p[0].CanTransitionTo(p[1]) {
			t.Errorf("expected LEGAL: %s → %s", p[0], p[1])
		}
	}
	illegal := [][2]cancellation.Status{
		{cancellation.StatusRequested, cancellation.StatusProcessed}, // skip approve
		{cancellation.StatusProcessed, cancellation.StatusRejected},  // terminal
		{cancellation.StatusProcessed, cancellation.StatusApproved},
		{cancellation.StatusRejected, cancellation.StatusApproved}, // terminal
		{cancellation.StatusRejected, cancellation.StatusRequested},
	}
	for _, p := range illegal {
		if p[0].CanTransitionTo(p[1]) {
			t.Errorf("expected ILLEGAL: %s → %s", p[0], p[1])
		}
	}
	if !cancellation.StatusProcessed.Terminal() || !cancellation.StatusRejected.Terminal() {
		t.Error("processed & rejected harus terminal")
	}
	if cancellation.StatusRequested.Terminal() || cancellation.StatusApproved.Terminal() {
		t.Error("requested & approved bukan terminal")
	}
}

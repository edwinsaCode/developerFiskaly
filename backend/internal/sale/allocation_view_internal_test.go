package sale

import "testing"

// FE-2 · P4 — label alokasi (pure).
func TestAllocationLabel(t *testing.T) {
	cases := []struct {
		allocType   AllocationType
		schedType   ScheduleType
		installment int
		want        string
	}{
		{AllocationTypeSchedule, ScheduleTypeDP, 1, "Uang Muka (DP)"},
		{AllocationTypeSchedule, ScheduleTypeFinal, 3, "Pelunasan"},
		{AllocationTypeSchedule, ScheduleTypeInstallment, 2, "Termin 2"},
		{AllocationTypeBuyerCredit, ScheduleTypeInstallment, 0, "Saldo Kredit Buyer"},
	}
	for _, c := range cases {
		if got := allocationLabel(c.allocType, c.schedType, c.installment); got != c.want {
			t.Errorf("allocationLabel(%s,%s,%d) = %q, want %q", c.allocType, c.schedType, c.installment, got, c.want)
		}
	}
}

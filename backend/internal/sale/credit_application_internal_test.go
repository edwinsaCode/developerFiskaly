package sale

import (
	"errors"
	"testing"

	"esaproperti/internal/domain"
)

// FE-3 · P2 — validator murni resolveCreditAmount.

func money(n int64) domain.Money { return domain.FromInt(n) }

func TestResolveCreditAmount(t *testing.T) {
	m := func(n int64) *domain.Money { v := money(n); return &v }
	frac, _ := domain.NewMoney("100.5")

	cases := []struct {
		name      string
		requested *domain.Money
		available domain.Money
		remaining domain.Money
		wantAmt   string
		wantErr   error
	}{
		{"default min(available,remaining)=remaining", nil, money(500), money(300), "300", nil},
		{"default min(available,remaining)=available", nil, money(200), money(300), "200", nil},
		{"requested full == remaining", m(300), money(500), money(300), "300", nil},
		{"requested partial", m(100), money(500), money(300), "100", nil},
		{"requested > remaining → overpaid", m(400), money(500), money(300), "", ErrScheduleOverpaid},
		{"requested > available → exceeds", m(600), money(500), money(1000), "", ErrCreditExceedsAvailable},
		{"no credit available", m(100), money(0), money(300), "", ErrNoCreditAvailable},
		{"schedule already paid", m(100), money(500), money(0), "", ErrScheduleAlreadyPaid},
		{"fractional", &frac, money(500), money(300), "", ErrCreditAmountFractional},
		{"zero requested", m(0), money(500), money(300), "", ErrCreditAmountZeroOrNeg},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := resolveCreditAmount(c.requested, c.available, c.remaining)
			if c.wantErr != nil {
				if !errors.Is(err, c.wantErr) {
					t.Fatalf("err: got %v, want %v", err, c.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			if got.String() != c.wantAmt {
				t.Errorf("amount: got %s, want %s", got.String(), c.wantAmt)
			}
		})
	}
}

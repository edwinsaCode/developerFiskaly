package scheme

import (
	"fmt"
	"time"

	"github.com/shopspring/decimal"

	"esaproperti/internal/domain"
)

// PaymentSchemePolicy adalah STRATEGY SEAM skema pembayaran (blueprint §5;
// desain docs/increment-3-payment-scheme-design.md §3 + §11).
//
// Kontrak interface:
//   - Pure domain: tanpa DB, tanpa HTTP, tanpa side effect.
//   - Semua keputusan perilaku skema lewat interface ini — TIDAK BOLEH ada
//     `if policy_type == "kpr"` di service/handler mana pun (pola CostTier).
//   - Params selalu berasal dari SNAPSHOT kontrak (approval note #2), bukan
//     master aktif — kecuali saat kontrak pertama kali dibuat.
type PaymentSchemePolicy interface {
	// PolicyType mengembalikan identitas strategy (nilai kanonik bertipe).
	PolicyType() PolicyType

	// PolicyVersion adalah versi ALGORITMA strategy — dibekukan ke Terms
	// Snapshot kontrak. Perubahan perilaku yang mengubah interpretasi kontrak
	// (rumus plan, makna state, resolusi akun) WAJIB menaikkan versi dan
	// mendaftarkan implementasi baru; kontrak lama tetap dibaca dengan
	// interpretasi versinya (hardening pasca-review Increment 3).
	PolicyVersion() int

	// ValidateParams memeriksa kewajaran parameter untuk policy ini —
	// dipanggil saat master dibuat/diubah DAN saat snapshot dibekukan.
	ValidateParams(p Params) error

	// ValidateContract memeriksa fakta kontrak terhadap aturan scheme
	// (mis. KPR wajib financing source; cash menolak financing source).
	ValidateContract(p Params, c ContractFacts) error

	// BuildSchedulePlan menghasilkan USULAN jadwal deterministik.
	// Σ(amount) == c.GrossAmount PERSIS (largest-remainder, Invariant #3).
	// User boleh override manual — Σ divalidasi ulang oleh sale service.
	BuildSchedulePlan(p Params, c ContractFacts) ([]PlanItem, error)

	// AllowedEvents mendefinisikan state machine scheme: event → transisi sah.
	AllowedEvents() map[Event]Transition

	// ResolveReceivableAccount me-resolve kode akun piutang untuk sisa tagihan
	// berdasar KONFIGURASI params + state — approval note #1: tanpa hardcode
	// akun AR di service. (KPR pasca-akad → financing_receivable_account.)
	ResolveReceivableAccount(p Params, s State) string

	// CanRecognize menegakkan gate pengakuan pendapatan/HPP scheme
	// (params.bast_gate; Temuan #7 — dulu bernama CanBAST, gate value TIDAK
	// berubah, hanya nama karena sekarang menggerbang aksi Akad, bukan BAST) —
	// MENAMBAH gate BCA-2, tidak menggantikan.
	CanRecognize(p Params, s State, f PaymentFacts) error
}

// ── Registry ──────────────────────────────────────────────────────────────────

// Registry memetakan PolicyType → strategy. Diisi sekali di wiring; immutable
// saat runtime. Menambah skema baru = policy baru + satu Register — Sale
// Contract & ledger tidak tersentuh (future-proof §3 desain).
type Registry struct {
	policies map[PolicyType]PaymentSchemePolicy
}

func NewRegistry(policies ...PaymentSchemePolicy) *Registry {
	m := make(map[PolicyType]PaymentSchemePolicy, len(policies))
	for _, p := range policies {
		m[p.PolicyType()] = p
	}
	return &Registry{policies: m}
}

// DefaultRegistry berisi empat policy bawaan.
func DefaultRegistry() *Registry {
	return NewRegistry(CashPolicy{}, CashInstallmentPolicy{}, KPRPolicy{}, InHousePolicy{})
}

// Policy mengembalikan strategy untuk sebuah PolicyType.
func (r *Registry) Policy(t PolicyType) (PaymentSchemePolicy, error) {
	p, ok := r.policies[t]
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrUnknownPolicyType, t)
	}
	return p, nil
}

// ── Helper bersama antar policy ───────────────────────────────────────────────

// validateCommonParams: aturan lintas-policy (gate valid, akun, interest seam).
func validateCommonParams(p Params) error {
	if p.BASTGate != "" && !p.BASTGate.Valid() {
		return fmt.Errorf("%w: bast_gate %q", ErrInvalidParams, p.BASTGate)
	}
	if _, err := p.dpFraction(); err != nil {
		return err
	}
	// SEAM bunga (BS-6): tolak nilai selain kosong/"0" sampai dimodelkan benar.
	if p.InterestRatePct != "" && p.InterestRatePct != "0" {
		return ErrInterestNotSupported
	}
	return nil
}

// gateOrDefault: gate efektif policy (default per policy dilewatkan pemanggil).
func gateOrDefault(p Params, def BASTGate) BASTGate {
	if p.BASTGate != "" {
		return p.BASTGate
	}
	return def
}

// checkGate mengevaluasi gate terhadap state + fakta pembayaran.
func checkGate(gate BASTGate, p Params, s State, f PaymentFacts) error {
	switch gate {
	case GateFullPayment:
		if f.TotalReceived.Decimal().LessThan(f.GrossAmount.Decimal()) {
			return fmt.Errorf("%w: wajib lunas (diterima %s dari %s)",
				ErrBASTGateNotMet, f.TotalReceived, f.GrossAmount)
		}
	case GateAkad:
		// fully_paid lolos gate: pelunasan penuh adalah kepastian pembayaran
		// yang LEBIH kuat dari akad (flow nyata: akad → cair → lunas → BAST;
		// proyeksi milestone memajukan state melewati akad — R1 E2E).
		if s != StateAkad && s != StateDisbursed && s != StateFullyPaid {
			return fmt.Errorf("%w: wajib akad kredit terlebih dahulu (state saat ini %q)", ErrBASTGateNotMet, s)
		}
	case GateDPPaid:
		dp, err := p.DPAmount(f.GrossAmount)
		if err != nil {
			return err
		}
		if f.TotalReceived.Decimal().LessThan(dp.Decimal()) {
			return fmt.Errorf("%w: DP belum terbayar penuh (diterima %s dari DP %s)",
				ErrBASTGateNotMet, f.TotalReceived, dp)
		}
	default:
		return fmt.Errorf("%w: bast_gate %q", ErrInvalidParams, gate)
	}
	return nil
}

// monthlyDue: jatuh tempo bulan ke-n dari tanggal kontrak.
func monthlyDue(c ContractFacts, monthsAhead int) time.Time {
	return c.ContractDate.AddDate(0, monthsAhead, 0)
}

// equalWeights menghasilkan n bobot sama untuk Money.Allocate.
func equalWeights(n int) []decimal.Decimal {
	w := make([]decimal.Decimal, n)
	for i := range w {
		w[i] = decimal.NewFromInt(1)
	}
	return w
}

// splitDPAndRest membagi gross menjadi (dp, sisa) — Σ == gross persis.
func splitDPAndRest(p Params, gross domain.Money) (dp, rest domain.Money, err error) {
	frac, err := p.dpFraction()
	if err != nil {
		return domain.Zero, domain.Zero, err
	}
	if frac.IsZero() {
		return domain.Zero, gross, nil
	}
	parts := gross.Allocate([]decimal.Decimal{frac, decimal.NewFromInt(100).Sub(frac)})
	return parts[0], parts[1], nil
}

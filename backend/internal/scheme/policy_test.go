package scheme_test

import (
	"errors"
	"testing"
	"time"

	"esaproperti/internal/domain"
	"esaproperti/internal/scheme"
)

var contractDate = time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)

func facts(gross int64) scheme.ContractFacts {
	return scheme.ContractFacts{GrossAmount: domain.FromInt(gross), ContractDate: contractDate}
}

func kprFacts(gross int64) scheme.ContractFacts {
	f := facts(gross)
	f.HasFinancingSource = true
	return f
}

func planSum(items []scheme.PlanItem) domain.Money {
	var s domain.Money
	for _, it := range items {
		s = s.Add(it.Amount)
	}
	return s
}

func kprParams() scheme.Params {
	return scheme.Params{DPPercent: "10", FinalDueMonths: 6, BASTGate: scheme.GateAkad,
		FinancingReceivableAccount: "1-2200"}
}

// ── Registry ──────────────────────────────────────────────────────────────────

func TestRegistry_AllPoliciesRegistered(t *testing.T) {
	reg := scheme.DefaultRegistry()
	for _, pt := range scheme.AllPolicyTypes {
		pol, err := reg.Policy(pt)
		if err != nil {
			t.Fatalf("policy %s tidak terdaftar: %v", pt, err)
		}
		if pol.PolicyType() != pt {
			t.Errorf("policy %s mengaku sebagai %s", pt, pol.PolicyType())
		}
	}
	if _, err := reg.Policy("rent_to_own"); !errors.Is(err, scheme.ErrUnknownPolicyType) {
		t.Errorf("policy tak dikenal harus ErrUnknownPolicyType, got %v", err)
	}
}

// ── BuildSchedulePlan: Σ == gross PERSIS (Invariant #3) ──────────────────────

func TestPlan_SumEqualsGross_Exactly(t *testing.T) {
	// Angka ganjil yang tidak habis dibagi — membuktikan largest-remainder.
	const gross = 1_000_000_001
	reg := scheme.DefaultRegistry()

	cases := []struct {
		policy scheme.PolicyType
		params scheme.Params
		facts  scheme.ContractFacts
		nItems int
	}{
		{scheme.PolicyCash, scheme.Params{BASTGate: scheme.GateFullPayment}, facts(gross), 1},
		{scheme.PolicyCashInstallment, scheme.Params{DPPercent: "20", InstallmentCount: 3}, facts(gross), 4},
		{scheme.PolicyKPR, kprParams(), kprFacts(gross), 2},
		{scheme.PolicyInHouse, scheme.Params{DPPercent: "20", TenorMonths: 36}, facts(gross), 37},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(string(tc.policy), func(t *testing.T) {
			pol, _ := reg.Policy(tc.policy)
			items, err := pol.BuildSchedulePlan(tc.params, tc.facts)
			if err != nil {
				t.Fatalf("BuildSchedulePlan: %v", err)
			}
			if len(items) != tc.nItems {
				t.Errorf("jumlah item = %d, want %d", len(items), tc.nItems)
			}
			if got := planSum(items); !got.Equal(domain.FromInt(gross)) {
				t.Errorf("Σ plan = %s, want %d PERSIS (Invariant #3)", got, gross)
			}
			// Baris terakhir bertipe final; DP (jika ada) di baris pertama.
			if items[len(items)-1].Type != scheme.PlanItemFinal {
				t.Errorf("baris terakhir harus final, got %s", items[len(items)-1].Type)
			}
			if tc.params.DPPercent != "" && items[0].Type != scheme.PlanItemDP {
				t.Errorf("baris pertama harus dp, got %s", items[0].Type)
			}
		})
	}
}

func TestPlan_DPAmount_LargestRemainder(t *testing.T) {
	p := scheme.Params{DPPercent: "1"} // KPR subsidi: 1% dari angka ganjil
	dp, err := p.DPAmount(domain.FromInt(999_999_999))
	if err != nil {
		t.Fatal(err)
	}
	rest := domain.FromInt(999_999_999).Sub(dp)
	if !dp.Add(rest).Equal(domain.FromInt(999_999_999)) {
		t.Errorf("DP + sisa harus == gross persis")
	}
	if !dp.IsWholeRupiah() {
		t.Errorf("DP harus rupiah bulat, got %s", dp)
	}
}

// ── ValidateParams ────────────────────────────────────────────────────────────

func TestValidateParams_Rejections(t *testing.T) {
	reg := scheme.DefaultRegistry()
	cases := []struct {
		name   string
		policy scheme.PolicyType
		params scheme.Params
	}{
		{"cash_dengan_tenor", scheme.PolicyCash, scheme.Params{TenorMonths: 12}},
		{"cash_gate_bukan_lunas", scheme.PolicyCash, scheme.Params{BASTGate: scheme.GateDPPaid}},
		{"cash_installment_nol", scheme.PolicyCashInstallment, scheme.Params{InstallmentCount: 0}},
		{"cash_installment_25x", scheme.PolicyCashInstallment, scheme.Params{InstallmentCount: 25}},
		{"kpr_tanpa_akun_bank", scheme.PolicyKPR, scheme.Params{DPPercent: "10"}},
		{"inhouse_tenor_pendek", scheme.PolicyInHouse, scheme.Params{TenorMonths: 3}},
		{"dp_negatif", scheme.PolicyInHouse, scheme.Params{DPPercent: "-5", TenorMonths: 24}},
		{"dp_lebih_100", scheme.PolicyInHouse, scheme.Params{DPPercent: "150", TenorMonths: 24}},
		{"gate_tak_dikenal", scheme.PolicyInHouse, scheme.Params{TenorMonths: 24, BASTGate: "whenever"}},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			pol, _ := reg.Policy(tc.policy)
			if err := pol.ValidateParams(tc.params); !errors.Is(err, scheme.ErrInvalidParams) {
				t.Errorf("expected ErrInvalidParams, got %v", err)
			}
		})
	}
}

// TestValidateParams_InterestSeamRejected: bunga cicilan belum didukung (BS-6).
func TestValidateParams_InterestSeamRejected(t *testing.T) {
	pol, _ := scheme.DefaultRegistry().Policy(scheme.PolicyInHouse)
	p := scheme.Params{TenorMonths: 24, InterestRatePct: "5"}
	if err := pol.ValidateParams(p); !errors.Is(err, scheme.ErrInterestNotSupported) {
		t.Errorf("expected ErrInterestNotSupported, got %v", err)
	}
	p.InterestRatePct = "0" // eksplisit zero-interest = sah
	if err := pol.ValidateParams(p); err != nil {
		t.Errorf("interest 0 harus sah, got %v", err)
	}
}

// ── ValidateContract ──────────────────────────────────────────────────────────

func TestValidateContract_FinancingSourceRules(t *testing.T) {
	reg := scheme.DefaultRegistry()

	kpr, _ := reg.Policy(scheme.PolicyKPR)
	if err := kpr.ValidateContract(kprParams(), facts(500_000_000)); !errors.Is(err, scheme.ErrFinancingSourceRequired) {
		t.Errorf("KPR tanpa bank: expected ErrFinancingSourceRequired, got %v", err)
	}
	if err := kpr.ValidateContract(kprParams(), kprFacts(500_000_000)); err != nil {
		t.Errorf("KPR dengan bank harus sah, got %v", err)
	}

	for _, pt := range []scheme.PolicyType{scheme.PolicyCash, scheme.PolicyCashInstallment, scheme.PolicyInHouse} {
		pol, _ := reg.Policy(pt)
		if err := pol.ValidateContract(scheme.Params{}, kprFacts(500_000_000)); !errors.Is(err, scheme.ErrFinancingSourceNotAllowed) {
			t.Errorf("%s dengan bank: expected ErrFinancingSourceNotAllowed, got %v", pt, err)
		}
	}
}

// ── State machine ─────────────────────────────────────────────────────────────

func TestKPR_StateMachine_FullJourney(t *testing.T) {
	pol, _ := scheme.DefaultRegistry().Policy(scheme.PolicyKPR)
	events := pol.AllowedEvents()

	steps := []struct {
		from scheme.State
		ev   scheme.Event
		to   scheme.State
	}{
		{scheme.StateSigned, scheme.EventDPPaid, scheme.StateDPPaid},
		{scheme.StateDPPaid, scheme.EventSubmittedToBank, scheme.StateSubmittedToBank},
		{scheme.StateSubmittedToBank, scheme.EventBankRejected, scheme.StateBankRejected},
		{scheme.StateBankRejected, scheme.EventSubmittedToBank, scheme.StateSubmittedToBank}, // ganti bank
		{scheme.StateSubmittedToBank, scheme.EventBankApproved, scheme.StateBankApproved},
		{scheme.StateBankApproved, scheme.EventAkad, scheme.StateAkad},
		{scheme.StateAkad, scheme.EventDisbursed, scheme.StateDisbursed},
	}
	for _, st := range steps {
		tr, ok := events[st.ev]
		if !ok {
			t.Fatalf("event %s tidak dikenal KPR", st.ev)
		}
		if !containsState(tr.From, st.from) {
			t.Errorf("event %s harus sah dari %s", st.ev, st.from)
		}
		if !tr.KeepState && tr.To != st.to {
			t.Errorf("event %s → %s, want %s", st.ev, tr.To, st.to)
		}
	}

	// Transisi ilegal: akad tanpa persetujuan bank; converted dari state sehat.
	if containsState(events[scheme.EventAkad].From, scheme.StateSigned) {
		t.Error("akad langsung dari signed harus TIDAK sah")
	}
	if containsState(events[scheme.EventConverted].From, scheme.StateAkad) {
		t.Error("konversi dari akad harus TIDAK sah (hanya bank_rejected)")
	}
	// Takeover: event tercatat tanpa memindah state.
	if !events[scheme.EventTakeover].KeepState {
		t.Error("takeover harus KeepState")
	}
	// BAST pada KPR: event tercatat TANPA memindah state (counterparty piutang
	// bergantung pada state akad/disbursed — resolusi akun tidak boleh hilang).
	if !events[scheme.EventHandedOver].KeepState {
		t.Error("handed_over pada KPR harus KeepState")
	}
	if !containsState(events[scheme.EventHandedOver].From, scheme.StateAkad) {
		t.Error("handed_over harus sah dari akad")
	}
}

func containsState(list []scheme.State, s scheme.State) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// ── ResolveReceivableAccount (approval note #1 — config-driven) ───────────────

func TestResolveReceivableAccount_KPR_ByState(t *testing.T) {
	pol, _ := scheme.DefaultRegistry().Policy(scheme.PolicyKPR)
	p := kprParams()

	preAkad := []scheme.State{scheme.StateSigned, scheme.StateDPPaid, scheme.StateSubmittedToBank, scheme.StateBankApproved}
	for _, st := range preAkad {
		if got := pol.ResolveReceivableAccount(p, st); got != scheme.DefaultReceivableAccount {
			t.Errorf("state %s: akun = %s, want %s (buyer)", st, got, scheme.DefaultReceivableAccount)
		}
	}
	// Hanya JENDELA akad→pencairan yang menjadi piutang bank.
	if got := pol.ResolveReceivableAccount(p, scheme.StateAkad); got != "1-2200" {
		t.Errorf("state akad: akun = %s, want 1-2200 (bank, dari KONFIGURASI)", got)
	}

	// T-3 (keputusan klien final 2026-08-05): begitu bank mencairkan, bank
	// SELESAI pada nilai pencairan aktualnya — sisa tagihan menjadi piutang
	// CUSTOMER. Tidak boleh ada state pasca-pencairan yang menunjuk piutang bank.
	pascaCair := []scheme.State{scheme.StateDisbursed, scheme.StateHandedOver, scheme.StateFullyPaid}
	for _, st := range pascaCair {
		if got := pol.ResolveReceivableAccount(p, st); got != scheme.DefaultReceivableAccount {
			t.Errorf("state %s: akun = %s, want %s (piutang customer — T-3)", st, got, scheme.DefaultReceivableAccount)
		}
	}

	// Bukti config-driven (bukan hardcode): ganti akun di params → resolusi ikut.
	p.FinancingReceivableAccount = "1-2300"
	if got := pol.ResolveReceivableAccount(p, scheme.StateAkad); got != "1-2300" {
		t.Errorf("akun harus mengikuti konfigurasi params, got %s", got)
	}
	p.ReceivableAccount = "1-2001"
	if got := pol.ResolveReceivableAccount(p, scheme.StateSigned); got != "1-2001" {
		t.Errorf("akun buyer harus mengikuti konfigurasi params, got %s", got)
	}
}

func TestResolveReceivableAccount_NonKPR_AlwaysBuyer(t *testing.T) {
	reg := scheme.DefaultRegistry()
	for _, pt := range []scheme.PolicyType{scheme.PolicyCash, scheme.PolicyCashInstallment, scheme.PolicyInHouse} {
		pol, _ := reg.Policy(pt)
		if got := pol.ResolveReceivableAccount(scheme.Params{}, scheme.StateHandedOver); got != scheme.DefaultReceivableAccount {
			t.Errorf("%s: akun = %s, want %s", pt, got, scheme.DefaultReceivableAccount)
		}
	}
}

// ── CanRecognize gates ─────────────────────────────────────────────────────────────

func TestCanRecognize_Gates(t *testing.T) {
	reg := scheme.DefaultRegistry()
	gross := domain.FromInt(1_000_000_000)

	t.Run("cash_wajib_lunas", func(t *testing.T) {
		pol, _ := reg.Policy(scheme.PolicyCash)
		p := scheme.Params{BASTGate: scheme.GateFullPayment}
		err := pol.CanRecognize(p, scheme.StateSigned, scheme.PaymentFacts{TotalReceived: domain.FromInt(999_999_999), GrossAmount: gross})
		if !errors.Is(err, scheme.ErrBASTGateNotMet) {
			t.Errorf("kurang 1 rupiah harus ditolak, got %v", err)
		}
		if err := pol.CanRecognize(p, scheme.StateSigned, scheme.PaymentFacts{TotalReceived: gross, GrossAmount: gross}); err != nil {
			t.Errorf("lunas harus lolos, got %v", err)
		}
	})

	t.Run("kpr_wajib_akad", func(t *testing.T) {
		pol, _ := reg.Policy(scheme.PolicyKPR)
		p := kprParams()
		err := pol.CanRecognize(p, scheme.StateBankApproved, scheme.PaymentFacts{TotalReceived: gross, GrossAmount: gross})
		if !errors.Is(err, scheme.ErrBASTGateNotMet) {
			t.Errorf("belum akad harus ditolak walau lunas, got %v", err)
		}
		if err := pol.CanRecognize(p, scheme.StateAkad, scheme.PaymentFacts{TotalReceived: domain.Zero, GrossAmount: gross}); err != nil {
			t.Errorf("sudah akad harus lolos, got %v", err)
		}
		// R1: flow nyata akad → cair → lunas memajukan state ke fully_paid —
		// pelunasan penuh adalah kepastian LEBIH kuat dari akad, gate lolos.
		if err := pol.CanRecognize(p, scheme.StateFullyPaid, scheme.PaymentFacts{TotalReceived: gross, GrossAmount: gross}); err != nil {
			t.Errorf("fully_paid harus lolos gate akad, got %v", err)
		}
	})

	t.Run("inhouse_wajib_dp", func(t *testing.T) {
		pol, _ := reg.Policy(scheme.PolicyInHouse)
		p := scheme.Params{DPPercent: "20", TenorMonths: 24, BASTGate: scheme.GateDPPaid}
		// DP = 200jt; baru terima 199jt → tolak.
		err := pol.CanRecognize(p, scheme.StateSigned, scheme.PaymentFacts{TotalReceived: domain.FromInt(199_000_000), GrossAmount: gross})
		if !errors.Is(err, scheme.ErrBASTGateNotMet) {
			t.Errorf("DP kurang harus ditolak, got %v", err)
		}
		if err := pol.CanRecognize(p, scheme.StateDPPaid, scheme.PaymentFacts{TotalReceived: domain.FromInt(200_000_000), GrossAmount: gross}); err != nil {
			t.Errorf("DP terpenuhi harus lolos, got %v", err)
		}
	})
}

// ── Params snapshot roundtrip ─────────────────────────────────────────────────

func TestParams_JSONRoundtrip(t *testing.T) {
	orig := kprParams()
	raw, err := orig.JSON()
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := scheme.ParseParams(raw)
	if err != nil {
		t.Fatal(err)
	}
	if parsed != orig {
		t.Errorf("roundtrip berubah: %+v != %+v", parsed, orig)
	}
	if _, err := scheme.ParseParams(""); !errors.Is(err, scheme.ErrInvalidParams) {
		t.Errorf("params kosong harus ditolak, got %v", err)
	}
	if _, err := scheme.ParseParams("{bukan json"); !errors.Is(err, scheme.ErrInvalidParams) {
		t.Errorf("JSON rusak harus ditolak, got %v", err)
	}
}

// ── TermsSnapshot envelope (hardening pasca-review) ───────────────────────────

func TestTermsSnapshot_EnvelopeRoundtrip(t *testing.T) {
	pol, _ := scheme.DefaultRegistry().Policy(scheme.PolicyKPR)
	snap := scheme.NewTermsSnapshot(pol, kprParams())
	if snap.PolicyType != scheme.PolicyKPR || snap.PolicyVersion != 1 {
		t.Fatalf("amplop harus membekukan policy_type + version: %+v", snap)
	}
	raw, err := snap.JSON()
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := scheme.ParseTermsSnapshot(raw)
	if err != nil {
		t.Fatal(err)
	}
	if parsed != snap {
		t.Errorf("roundtrip berubah: %+v != %+v", parsed, snap)
	}
}

// Snapshot format awal (bare Params tanpa amplop) tetap terbaca — policy_type
// kosong menandakan pemanggil harus fallback ke master row (kompat).
func TestTermsSnapshot_LegacyBareParams(t *testing.T) {
	raw, _ := kprParams().JSON()
	parsed, err := scheme.ParseTermsSnapshot(raw)
	if err != nil {
		t.Fatalf("snapshot legacy harus terbaca: %v", err)
	}
	if parsed.PolicyType != "" {
		t.Errorf("legacy snapshot: policy_type harus kosong, got %s", parsed.PolicyType)
	}
	if parsed.Params.DPPercent != "10" {
		t.Errorf("params legacy harus terbaca utuh, got %+v", parsed.Params)
	}
}

func TestAllPolicies_HaveVersion(t *testing.T) {
	reg := scheme.DefaultRegistry()
	for _, pt := range scheme.AllPolicyTypes {
		pol, _ := reg.Policy(pt)
		if pol.PolicyVersion() < 1 {
			t.Errorf("%s: PolicyVersion harus >= 1", pt)
		}
	}
}

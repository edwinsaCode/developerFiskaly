package charge

import (
	"errors"
	"testing"

	"esaproperti/internal/domain"
)

// ── Resolver master: FAIL-CLOSED ────────────────────────────────────────────

func TestChargeTypePolicyOf_RejectsInactive(t *testing.T) {
	ct := &RealizationChargeType{
		Code: "notaris", Name: "Biaya Notaris",
		Treatment: domain.TreatmentDepositLiability, DepositAccountCode: "2-2400",
		IsActive: false,
	}
	if _, err := chargeTypePolicyOf(ct); !errors.Is(err, ErrChargeTypeInactive) {
		t.Fatalf("jenis nonaktif harus ditolak, dapat %v", err)
	}
}

func TestChargeTypePolicyOf_RejectsUnknownTreatment(t *testing.T) {
	ct := &RealizationChargeType{
		Code: "notaris", Treatment: "titipan", DepositAccountCode: "2-2400", IsActive: true,
	}
	if _, err := chargeTypePolicyOf(ct); !errors.Is(err, ErrChargeTypeInvalid) {
		t.Fatalf("perlakuan tak dikenal harus ditolak, dapat %v", err)
	}
}

func TestChargeTypePolicyOf_RejectsEmptyDepositAccount(t *testing.T) {
	ct := &RealizationChargeType{
		Code: "notaris", Treatment: domain.TreatmentDepositLiability,
		DepositAccountCode: "   ", IsActive: true,
	}
	if _, err := chargeTypePolicyOf(ct); !errors.Is(err, ErrDepositAccountInvalid) {
		t.Fatalf("akun titipan kosong harus ditolak, dapat %v", err)
	}
}

func TestChargeTypePolicyOf_Valid(t *testing.T) {
	ct := &RealizationChargeType{
		Code: "pdam", Name: "Sambungan PDAM",
		Treatment: domain.TreatmentDepositLiability, DepositAccountCode: " 2-2400 ", IsActive: true,
	}
	pol, err := chargeTypePolicyOf(ct)
	if err != nil {
		t.Fatalf("jenis valid ditolak: %v", err)
	}
	if pol.DepositAccountCode != "2-2400" {
		t.Fatalf("akun titipan harus di-trim, dapat %q", pol.DepositAccountCode)
	}
	// K-1: biaya realisasi TIDAK PERNAH menjadi pendapatan.
	if pol.RecognizesRevenue() {
		t.Fatal("titipan realisasi tidak boleh mengakui pendapatan")
	}
}

func TestNormalizeChargeTypeCode(t *testing.T) {
	for in, want := range map[string]string{
		"  Notaris ": "notaris",
		"PDAM":       "pdam",
		"":           "",
		"   ":        "",
	} {
		if got := normalizeChargeTypeCode(in); got != want {
			t.Fatalf("normalize(%q) = %q, mau %q", in, got, want)
		}
	}
}

// ── Akun kewajiban per jenis biaya (koreksi arsitektur 2026-08-06) ──────────
//
// Yang diuji di sini adalah pencabutan "satu grup = satu akun titipan": grup
// BOLEH memuat jenis biaya dengan akun berbeda, dan setiap rupiah tetap punya
// akun yang jelas.

func mustMoney(t *testing.T, s string) domain.Money {
	t.Helper()
	m, err := domain.NewMoney(s)
	if err != nil {
		t.Fatalf("uang %q: %v", s, err)
	}
	return m
}

func sumParts(parts []DepositAmount) domain.Money {
	total := domain.Zero
	for _, p := range parts {
		total = total.Add(p.Amount)
	}
	return total
}

func TestDepositSplitOfAllocations_MixedAccounts(t *testing.T) {
	items := []*ChargeItem{
		{ID: 1, DepositAccountCode: "2-2400"}, // Notaris
		{ID: 2, DepositAccountCode: "2-2410"}, // BPHTB — akun berbeda, grup yang sama
		{ID: 3, DepositAccountCode: "2-2400"},
	}
	allocs := []AllocationInput{
		{ChargeItemID: 1, Amount: mustMoney(t, "300000")},
		{ChargeItemID: 2, Amount: mustMoney(t, "500000")},
		{ChargeItemID: 3, Amount: mustMoney(t, "200000")},
	}
	parts := depositSplitOfAllocations(allocs, items)
	if len(parts) != 2 {
		t.Fatalf("harus terpecah ke 2 akun, dapat %d: %+v", len(parts), parts)
	}
	// Deterministik: urut kode akun menaik.
	if parts[0].AccountCode != "2-2400" || parts[1].AccountCode != "2-2410" {
		t.Fatalf("urutan akun harus deterministik, dapat %+v", parts)
	}
	if parts[0].Amount.String() != mustMoney(t, "500000").String() {
		t.Fatalf("2-2400 harus 500000, dapat %s", parts[0].Amount)
	}
	// Invariant #3 turunan: Σ kredit == Σ alokasi == nominal pembayaran.
	if !sumParts(parts).Sub(mustMoney(t, "1000000")).IsZero() {
		t.Fatalf("Σ pecahan harus 1.000.000, dapat %s", sumParts(parts))
	}
}

func TestDepositSplitOfAllocations_ItemTanpaSnapshotJatuhKeDefault(t *testing.T) {
	// Baris pra-000063: snapshot kosong → akun default, persis akun yang dulu
	// dipakai. Histori tidak pernah ditulis ulang (Invariant #5).
	items := []*ChargeItem{{ID: 1, DepositAccountCode: ""}}
	parts := depositSplitOfAllocations(
		[]AllocationInput{{ChargeItemID: 1, Amount: mustMoney(t, "100000")}}, items)
	if len(parts) != 1 || parts[0].AccountCode != AccountCodeTitipanRealisasi {
		t.Fatalf("harus jatuh ke akun default, dapat %+v", parts)
	}
}

func TestSplitByResidual_SatuAkunTidakBerubah(t *testing.T) {
	// Keadaan seluruh data hari ini: satu grup satu akun. Hasilnya harus
	// identik dengan perilaku sebelum koreksi — nominal utuh, tanpa pembulatan.
	res := []DepositAmount{{AccountCode: "2-2400", Amount: mustMoney(t, "1000000")}}
	parts, err := splitByResidual(res, mustMoney(t, "750000"))
	if err != nil {
		t.Fatalf("split satu akun: %v", err)
	}
	if len(parts) != 1 || parts[0].Amount.String() != mustMoney(t, "750000").String() {
		t.Fatalf("harus satu baris 750000, dapat %+v", parts)
	}
}

func TestSplitByResidual_ProporsionalSampaiRupiahTerakhir(t *testing.T) {
	// 1.000 : 2.000 dari 1.000 → 333 : 667 (largest-remainder). Yang wajib:
	// Σ == nominal PERSIS, dan tidak ada bagian yang melampaui residualnya.
	res := []DepositAmount{
		{AccountCode: "2-2400", Amount: mustMoney(t, "1000")},
		{AccountCode: "2-2410", Amount: mustMoney(t, "2000")},
	}
	amount := mustMoney(t, "1000")
	parts, err := splitByResidual(res, amount)
	if err != nil {
		t.Fatalf("split proporsional: %v", err)
	}
	if !sumParts(parts).Sub(amount).IsZero() {
		t.Fatalf("Invariant #3: Σ pecahan harus %s, dapat %s", amount, sumParts(parts))
	}
	byAcc := map[string]domain.Money{}
	for _, p := range parts {
		byAcc[p.AccountCode] = p.Amount
	}
	for _, r := range res {
		if byAcc[r.AccountCode].GreaterThan(r.Amount) {
			t.Fatalf("%s: pecahan %s melampaui residual %s", r.AccountCode, byAcc[r.AccountCode], r.Amount)
		}
	}
}

func TestSplitByResidual_AkunMinusTidakIkutMenanggung(t *testing.T) {
	// Payout melampaui titipan (K-5) membuat residual sebuah akun negatif.
	// Menariknya lebih dalam hanya memperbesar kewajiban yang sudah minus.
	res := []DepositAmount{
		{AccountCode: "2-2400", Amount: mustMoney(t, "500000")},
		{AccountCode: "2-2410", Amount: mustMoney(t, "-100000")},
	}
	parts, err := splitByResidual(res, mustMoney(t, "500000"))
	if err != nil {
		t.Fatalf("split: %v", err)
	}
	if len(parts) != 1 || parts[0].AccountCode != "2-2400" {
		t.Fatalf("akun minus harus dilewati, dapat %+v", parts)
	}
}

func TestSplitByResidual_MelampauiSisaDitolak(t *testing.T) {
	res := []DepositAmount{{AccountCode: "2-2400", Amount: mustMoney(t, "100000")}}
	if _, err := splitByResidual(res, mustMoney(t, "100001")); !errors.Is(err, ErrExceedsResidual) {
		t.Fatalf("melampaui sisa harus ditolak, dapat %v", err)
	}
}

func TestPrimaryDepositCode_NominalTerbesar(t *testing.T) {
	got := primaryDepositCode([]DepositAmount{
		{AccountCode: "2-2400", Amount: mustMoney(t, "100")},
		{AccountCode: "2-2410", Amount: mustMoney(t, "900")},
	})
	if got != "2-2410" {
		t.Fatalf("kode dominan harus 2-2410, dapat %s", got)
	}
	if got := primaryDepositCode(nil); got != AccountCodeTitipanRealisasi {
		t.Fatalf("tanpa pecahan harus jatuh ke default, dapat %s", got)
	}
}

// ── Seed default: keempat jenis titipan, akun sama, aturan owner ───────────

func TestDefaultChargeTypesAreAllDeposits(t *testing.T) {
	if len(defaultChargeTypes) != 4 {
		t.Fatalf("seed default harus 4 jenis (Notaris/BPHTB/PDAM/Listrik), dapat %d", len(defaultChargeTypes))
	}
	for _, ct := range defaultChargeTypes {
		if !ct.Treatment.IsThirdPartyDeposit() {
			t.Fatalf("%s: seluruh biaya realisasi adalah titipan (K-1)", ct.Code)
		}
		// Jurnal owner satu baris "Titipan Realisasi" — akun default seragam.
		if ct.DepositAccountCode != AccountCodeTitipanRealisasi {
			t.Fatalf("%s: akun default harus %s, dapat %s", ct.Code, AccountCodeTitipanRealisasi, ct.DepositAccountCode)
		}
		if normalizeChargeTypeCode(ct.Code) != ct.Code {
			t.Fatalf("%s: kode seed harus sudah ter-normalisasi", ct.Code)
		}
	}
}

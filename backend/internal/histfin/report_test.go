package histfin

import (
	"testing"

	"esaproperti/internal/domain"
)

// Mesin laporan snapshot tidak menyentuh DB, jadi aturan angkanya diuji murni
// di sini. Yang dijaga: laba/rugi tahun berjalan SELALU hasil hitung, dan
// "seimbang" berarti aset == kewajiban + ekuitas + laba berjalan — bukan
// sekadar dua kolom yang kebetulan sama.

func line(code string, t domain.AccountType, amount string, order int) SnapshotLine {
	return SnapshotLine{
		AccountCode: code,
		AccountName: "Akun " + code,
		AccountType: string(t),
		Statement:   StatementFor(t),
		Amount:      domain.MustParse(amount),
		SortOrder:   order,
	}
}

func TestComputeTotals_LabaMasukSisiKanan(t *testing.T) {
	lines := []SnapshotLine{
		line("1-1000", domain.AccountAsset, "500000000", 0),
		line("2-1000", domain.AccountLiability, "120000000", 0),
		line("3-1000", domain.AccountEquity, "300000000", 0),
		line("4-1000", domain.AccountRevenue, "200000000", 0),
		line("5-1000", domain.AccountExpense, "120000000", 0),
	}
	got := ComputeTotals(lines)

	if got.NetIncome.String() != "80000000" {
		t.Fatalf("laba bersih = %s, mau 80000000", got.NetIncome)
	}
	// 120jt + 300jt + 80jt = 500jt = total aset.
	if !got.IsBalanced {
		t.Fatalf("seharusnya seimbang; selisih = %s", got.Difference)
	}
	if got.TotalKewajibanEkuitas.String() != "500000000" {
		t.Fatalf("total kewajiban+ekuitas = %s, mau 500000000", got.TotalKewajibanEkuitas)
	}
}

// Neraca yang seimbang TANPA memperhitungkan laba berjalan justru salah: begitu
// ada pendapatan/beban, sisi kanan bergeser. Test ini mengunci arah itu supaya
// tidak ada yang "memperbaiki" rumusnya dengan membuang NetIncome.
func TestComputeTotals_SeimbangTanpaLabaAdalahTidakSeimbang(t *testing.T) {
	lines := []SnapshotLine{
		line("1-1000", domain.AccountAsset, "420000000", 0),
		line("2-1000", domain.AccountLiability, "120000000", 0),
		line("3-1000", domain.AccountEquity, "300000000", 0),
		line("4-1000", domain.AccountRevenue, "200000000", 0),
		line("5-1000", domain.AccountExpense, "120000000", 0),
	}
	got := ComputeTotals(lines)
	if got.IsBalanced {
		t.Fatal("aset 420jt vs kanan 500jt seharusnya TIDAK seimbang")
	}
	if got.Difference.String() != "-80000000" {
		t.Fatalf("selisih = %s, mau -80000000", got.Difference)
	}
}

func TestComputeTotals_RugiDanDefisitBoleh(t *testing.T) {
	lines := []SnapshotLine{
		line("1-1000", domain.AccountAsset, "50000000", 0),
		line("2-1000", domain.AccountLiability, "90000000", 0),
		line("3-2000", domain.AccountEquity, "-10000000", 0), // defisit akumulasi
		line("4-1000", domain.AccountRevenue, "10000000", 0),
		line("5-1000", domain.AccountExpense, "40000000", 0),
	}
	got := ComputeTotals(lines)
	if got.NetIncome.String() != "-30000000" {
		t.Fatalf("rugi = %s, mau -30000000", got.NetIncome)
	}
	if !got.IsBalanced {
		t.Fatalf("90jt + (-10jt) + (-30jt) = 50jt seharusnya seimbang; selisih %s", got.Difference)
	}
}

func TestComputeTotals_Kosong(t *testing.T) {
	got := ComputeTotals(nil)
	if !got.IsBalanced || got.LineCount != 0 || !got.NetIncome.IsZero() {
		t.Fatalf("snapshot kosong: %+v", got)
	}
}

func TestStatementFor(t *testing.T) {
	cases := map[domain.AccountType]Statement{
		domain.AccountAsset:     StatementBalanceSheet,
		domain.AccountLiability: StatementBalanceSheet,
		domain.AccountEquity:    StatementBalanceSheet,
		domain.AccountRevenue:   StatementIncomeStatement,
		domain.AccountExpense:   StatementIncomeStatement,
		domain.AccountType("x"): "",
	}
	for in, want := range cases {
		if got := StatementFor(in); got != want {
			t.Errorf("StatementFor(%q) = %q, mau %q", in, got, want)
		}
	}
}

func TestBuildNeraca_PisahSeksiDanUrut(t *testing.T) {
	snap := Snapshot{FiscalYear: 2024, Status: StatusFinal, Revision: 1}
	lines := []SnapshotLine{
		line("1-2000", domain.AccountAsset, "20000000", 2),
		line("1-1000", domain.AccountAsset, "80000000", 1),
		line("2-1000", domain.AccountLiability, "30000000", 0),
		line("3-1000", domain.AccountEquity, "70000000", 0),
	}
	rep := BuildNeraca(snap, lines)

	if len(rep.Aset) != 2 || rep.Aset[0].Code != "1-1000" {
		t.Fatalf("aset harus urut sort_order: %+v", rep.Aset)
	}
	if len(rep.Kewajiban) != 1 || len(rep.Ekuitas) != 1 {
		t.Fatalf("seksi neraca salah: %+v", rep)
	}
	if !rep.Historical {
		t.Fatal("laporan historis wajib menandai dirinya historis")
	}
	if rep.FiscalYear != 2024 {
		t.Fatalf("tahun = %d", rep.FiscalYear)
	}
	// Tanpa pendapatan/beban, ekuitas efektif == ekuitas.
	if rep.TotalEkuitasEfektif != "70000000" {
		t.Fatalf("ekuitas efektif = %s", rep.TotalEkuitasEfektif)
	}
}

func TestBuildLabaRugi_HanyaPendapatanDanBeban(t *testing.T) {
	snap := Snapshot{FiscalYear: 2023}
	lines := []SnapshotLine{
		line("1-1000", domain.AccountAsset, "80000000", 0),
		line("4-1000", domain.AccountRevenue, "150000000", 0),
		line("5-1000", domain.AccountExpense, "90000000", 0),
	}
	rep := BuildLabaRugi(snap, lines)

	if len(rep.Pendapatan) != 1 || len(rep.Beban) != 1 {
		t.Fatalf("baris neraca bocor ke Laba Rugi: %+v", rep)
	}
	if rep.LabaRugiBersih != "60000000" {
		t.Fatalf("laba bersih = %s, mau 60000000", rep.LabaRugiBersih)
	}
}

// Dua tahun yang dihitung dari kumpulan barisnya masing-masing tidak boleh
// saling memengaruhi — inti permintaan klien: 2024 bukan turunan 2025.
func TestPerTahunBerdiriSendiri(t *testing.T) {
	l2024 := []SnapshotLine{
		line("1-1000", domain.AccountAsset, "100000000", 0),
		line("3-1000", domain.AccountEquity, "100000000", 0),
	}
	l2025 := []SnapshotLine{
		line("1-1000", domain.AccountAsset, "250000000", 0),
		line("3-1000", domain.AccountEquity, "250000000", 0),
	}
	a := BuildNeraca(Snapshot{FiscalYear: 2024}, l2024)
	b := BuildNeraca(Snapshot{FiscalYear: 2025}, l2025)

	if a.TotalAset != "100000000" || b.TotalAset != "250000000" {
		t.Fatalf("angka tahun tercampur: 2024=%s 2025=%s", a.TotalAset, b.TotalAset)
	}
	if !a.IsBalanced || !b.IsBalanced {
		t.Fatal("kedua tahun seharusnya seimbang sendiri-sendiri")
	}
}

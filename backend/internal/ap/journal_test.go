package ap

import (
	"context"
	"errors"
	"testing"

	"esaproperti/internal/cost"
	"esaproperti/internal/domain"
	"esaproperti/internal/ledger"
)

// Uji bentuk jurnal pengakuan tanpa database.
//
// compose() adalah satu-satunya tempat bentuk jurnal ditentukan, jadi seluruh
// invariant bentuk (INV-AP-8, INV-AP-17, balance) dapat dibuktikan di sini —
// termasuk bentuk retensi dan uang muka yang pintu masuknya belum dibuka pada
// tahap ini. Menguji bentuknya sekarang berarti saat pintunya dibuka nanti,
// yang berubah hanya validasi intake, bukan akuntansinya.

type fakeAccounts struct {
	byCode map[string]*ledger.Account
	asked  []string
}

func newFakeAccounts(codes ...string) *fakeAccounts {
	f := &fakeAccounts{byCode: map[string]*ledger.Account{}}
	var id uint64 = 100
	for _, c := range codes {
		id++
		f.byCode[c] = &ledger.Account{ID: id, Code: c, Name: "Akun " + c}
	}
	return f
}

func (f *fakeAccounts) AccountByCode(_ context.Context, _ uint64, code string) (*ledger.Account, error) {
	f.asked = append(f.asked, code)
	a, ok := f.byCode[code]
	if !ok {
		return nil, ErrAccountNotFound
	}
	return a, nil
}

func money(t *testing.T, s string) domain.Money {
	t.Helper()
	m, err := domain.NewMoney(s)
	if err != nil {
		t.Fatalf("nominal %q tidak sah: %v", s, err)
	}
	return m
}

// taxonomyCode mengambil satu kode akun yang BENAR-BENAR terbaca sebagai
// realisasi RAB, bukan kode yang ditulis tangan di test.
func taxonomyCode(t *testing.T) string {
	t.Helper()
	for _, c := range domain.AllCostCategories {
		if code := c.InventoryAccountCode(); code != "" && cost.IsCostTaxonomyAccount(code) {
			return code
		}
	}
	t.Fatal("tidak ada akun taksonomi biaya di domain — asumsi test tidak berlaku")
	return ""
}

func line(t *testing.T, amount string, projectID uint64, debitCode string, taxonomy bool) plannedLine {
	t.Helper()
	return plannedLine{
		in: InvoiceLineInput{ProjectID: projectID, Amount: money(t, amount)},
		plan: cost.CostLinePlan{
			Tier:            domain.CostTierDirect,
			DebitCode:       debitCode,
			CreditCode:      payableAccountCode(),
			DebitIsTaxonomy: taxonomy,
			Description:     "pekerjaan",
		},
	}
}

func sums(c *composition) (debit, credit domain.Money) {
	debit, credit = domain.Zero, domain.Zero
	for _, l := range c.lines {
		debit = debit.Add(l.debit)
		credit = credit.Add(l.credit)
	}
	return
}

func find(c *composition, code string) *composedLine {
	for i := range c.lines {
		if c.lines[i].account.Code == code {
			return &c.lines[i]
		}
	}
	return nil
}

func TestComposeShapes(t *testing.T) {
	ctx := context.Background()
	tax := taxonomyCode(t)
	acc := newFakeAccounts(tax, payableAccountCode(), vatInputCode(), retentionPayableCode(), vendorAdvanceCode())

	cases := []struct {
		name          string
		amt           Amounts
		lines         []plannedLine
		wantPayable   string
		wantLineCount int
	}{
		{
			name:          "polos",
			amt:           Amounts{DPP: money(t, "100000000"), PPN: domain.Zero, Retention: domain.Zero, Advance: domain.Zero},
			lines:         []plannedLine{line(t, "100000000", 7, tax, true)},
			wantPayable:   "100000000",
			wantLineCount: 2, // 1 biaya + hutang usaha
		},
		{
			name:          "dengan PPN 11%",
			amt:           Amounts{DPP: money(t, "100000000"), PPN: money(t, "11000000"), Retention: domain.Zero, Advance: domain.Zero},
			lines:         []plannedLine{line(t, "100000000", 7, tax, true)},
			wantPayable:   "111000000",
			wantLineCount: 3, // biaya + PPN + hutang usaha
		},
		{
			name:          "dengan retensi 5%",
			amt:           Amounts{DPP: money(t, "100000000"), PPN: domain.Zero, Retention: money(t, "5000000"), Advance: domain.Zero},
			lines:         []plannedLine{line(t, "60000000", 7, tax, true), line(t, "40000000", 7, tax, true)},
			wantPayable:   "95000000",
			wantLineCount: 4, // 2 biaya + hutang usaha + hutang retensi
		},
		{
			name: "PPN + retensi + kompensasi uang muka",
			amt: Amounts{
				DPP: money(t, "100000000"), PPN: money(t, "11000000"),
				Retention: money(t, "5000000"), Advance: money(t, "20000000"),
			},
			lines:         []plannedLine{line(t, "100000000", 7, tax, true)},
			wantPayable:   "86000000",
			wantLineCount: 5, // biaya + PPN + hutang usaha + retensi + uang muka
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, err := compose(ctx, acc, 1, tc.amt, tc.lines)
			if err != nil {
				t.Fatalf("compose: %v", err)
			}
			if len(c.lines) != tc.wantLineCount {
				t.Fatalf("jumlah baris jurnal = %d, mau %d", len(c.lines), tc.wantLineCount)
			}

			// Invariant 1 CLAUDE.md: jurnal selalu balanced.
			debit, credit := sums(c)
			if !debit.Equal(credit) {
				t.Fatalf("jurnal tidak balanced: debit %s vs kredit %s", debit, credit)
			}

			// INV-AP-8: Σ debit taksonomi == DPP. PPN tidak boleh ikut.
			if !c.taxonomyDebit.Equal(tc.amt.DPP) {
				t.Fatalf("debit taksonomi %s, DPP %s", c.taxonomyDebit, tc.amt.DPP)
			}

			if c.payable.String() != tc.wantPayable {
				t.Fatalf("kewajiban vendor = %s, mau %s", c.payable, tc.wantPayable)
			}
			if pay := find(c, payableAccountCode()); pay == nil || pay.credit.String() != tc.wantPayable {
				t.Fatalf("baris hutang usaha tidak sesuai: %+v", pay)
			}

			// INV-AP-17: baris PPN tanpa tag proyek apa pun.
			if tc.amt.PPN.GreaterThan(domain.Zero) {
				ppn := find(c, vatInputCode())
				if ppn == nil {
					t.Fatal("baris PPN Masukan tidak ada")
				}
				if !ppn.debit.Equal(tc.amt.PPN) {
					t.Fatalf("debit PPN = %s, mau %s", ppn.debit, tc.amt.PPN)
				}
				if ppn.projectID != nil || ppn.phaseID != nil || ppn.unitID != nil {
					t.Fatal("baris PPN Masukan diberi tag proyek/fase/unit — melanggar INV-AP-17")
				}
			}

			// Baris kewajiban tidak boleh membawa tag proyek.
			for _, l := range c.lines {
				if l.credit.GreaterThan(domain.Zero) && l.projectID != nil {
					t.Fatalf("baris kredit %s diberi tag proyek", l.account.Code)
				}
			}
		})
	}
}

// TestComposeRejects membuktikan penolakan terjadi SEBELUM ada tulisan apa pun.
func TestComposeRejects(t *testing.T) {
	ctx := context.Background()
	tax := taxonomyCode(t)
	acc := newFakeAccounts(tax, payableAccountCode(), vatInputCode(), retentionPayableCode(), vendorAdvanceCode())
	dpp := money(t, "100000000")

	cases := []struct {
		name  string
		amt   Amounts
		lines []plannedLine
		want  error
	}{
		{
			name: "tanpa baris biaya",
			amt:  Amounts{DPP: dpp}, lines: nil, want: ErrNoLines,
		},
		{
			name: "DPP nol",
			amt:  Amounts{DPP: domain.Zero}, lines: []plannedLine{line(t, "1000", 7, tax, true)},
			want: ErrDPPZeroOrNeg,
		},
		{
			name: "Σ baris ≠ DPP",
			amt:  Amounts{DPP: dpp}, lines: []plannedLine{line(t, "99000000", 7, tax, true)},
			want: ErrLineSumMismatch,
		},
		{
			name: "baris mendarat di akun non-taksonomi",
			amt:  Amounts{DPP: dpp}, lines: []plannedLine{line(t, "100000000", 7, vatInputCode(), false)},
			want: ErrLineAccountNotTaxonomy,
		},
		{
			name: "retensi melebihi DPP",
			amt:  Amounts{DPP: dpp, Retention: money(t, "200000000")},
			lines: []plannedLine{
				line(t, "100000000", 7, tax, true),
			},
			want: ErrRetentionExceedsDPP,
		},
		{
			name: "PPN negatif",
			amt:  Amounts{DPP: dpp, PPN: money(t, "-1000")},
			lines: []plannedLine{
				line(t, "100000000", 7, tax, true),
			},
			want: ErrNegAmount,
		},
		{
			name: "retensi + uang muka melebihi tagihan",
			amt:  Amounts{DPP: dpp, Retention: money(t, "60000000"), Advance: money(t, "60000000")},
			lines: []plannedLine{
				line(t, "100000000", 7, tax, true),
			},
			want: ErrNegAmount,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := compose(ctx, acc, 1, tc.amt, tc.lines)
			if !errors.Is(err, tc.want) {
				t.Fatalf("error = %v, mau %v", err, tc.want)
			}
		})
	}
}

// TestComposeRejectsMixedCreditAccounts: dua akun kewajiban berbeda dalam satu
// tagihan berarti sebagian nilainya tidak akan pernah terbayar lewat jalur AP.
func TestComposeRejectsMixedCreditAccounts(t *testing.T) {
	tax := taxonomyCode(t)
	acc := newFakeAccounts(tax, payableAccountCode())

	a := line(t, "50000000", 7, tax, true)
	b := line(t, "50000000", 7, tax, true)
	b.plan.CreditCode = "2-9999"

	_, err := compose(context.Background(), acc, 1, Amounts{DPP: money(t, "100000000")}, []plannedLine{a, b})
	if !errors.Is(err, ErrLineCreditMismatch) {
		t.Fatalf("error = %v, mau %v", err, ErrLineCreditMismatch)
	}
}

// TestPPNTidakMenambahRealisasiRAB adalah pernyataan bisnisnya, bukan sekadar
// pemeriksaan angka: tagihan 100jt dari vendor PKP dan dari vendor non-PKP
// merealisasi RAB dengan jumlah yang PERSIS sama.
func TestPPNTidakMenambahRealisasiRAB(t *testing.T) {
	ctx := context.Background()
	tax := taxonomyCode(t)
	acc := newFakeAccounts(tax, payableAccountCode(), vatInputCode())
	dpp := money(t, "100000000")
	lines := []plannedLine{line(t, "100000000", 7, tax, true)}

	nonPKP, err := compose(ctx, acc, 1, Amounts{DPP: dpp}, lines)
	if err != nil {
		t.Fatalf("compose non-PKP: %v", err)
	}
	pkp, err := compose(ctx, acc, 1, Amounts{DPP: dpp, PPN: money(t, "11000000")}, lines)
	if err != nil {
		t.Fatalf("compose PKP: %v", err)
	}

	if !nonPKP.taxonomyDebit.Equal(pkp.taxonomyDebit) {
		t.Fatalf("realisasi RAB berbeda: non-PKP %s vs PKP %s", nonPKP.taxonomyDebit, pkp.taxonomyDebit)
	}
	// Yang berbeda hanyalah kewajiban kasnya.
	if pkp.payable.Equal(nonPKP.payable) {
		t.Fatal("kewajiban vendor seharusnya bertambah sebesar PPN")
	}
}

// TestVATAccountBukanAkunTaksonomi mengunci asumsi yang membuat INV-AP-17 bisa
// ditegakkan sama sekali. Kalau suatu hari taksonomi biaya berubah dan mencakup
// 1-5100, test ini gagal SEBELUM ada PPN yang terhitung sebagai realisasi RAB.
func TestVATAccountBukanAkunTaksonomi(t *testing.T) {
	for _, code := range []string{vatInputCode(), vendorAdvanceCode(), retentionPayableCode(), payableAccountCode()} {
		if code == "" {
			t.Fatal("registry peran akun mengembalikan kode kosong")
		}
		if cost.IsCostTaxonomyAccount(code) {
			t.Fatalf("akun %s terbaca sebagai akun taksonomi biaya", code)
		}
	}
}

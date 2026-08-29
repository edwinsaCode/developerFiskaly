package ap

import (
	"context"
	"errors"
	"testing"

	"esaproperti/internal/cost"
	"esaproperti/internal/domain"
	"esaproperti/internal/ledger"
)

// Uji bentuk jurnal PEMBAYARAN tanpa database.
//
// composePayment() adalah satu-satunya tempat bentuk jurnal pembayaran
// ditentukan, jadi seluruh invariant bentuknya (INV-AP-3, balance, Σ alokasi)
// bisa dibuktikan di sini — termasuk bentuk uang muka & retensi yang pintu
// masuknya belum dibuka. Menguji bentuknya sekarang berarti saat pintunya
// dibuka nanti, yang berubah hanya validasi intake, bukan akuntansinya.

// cashAccount membuat akun yang BENAR-BENAR lolos ledger.ValidatePaymentAccount
// — bukan akun kosong yang kebetulan bernama "kas".
func cashAccount(id uint64, code string) *ledger.Account {
	return &ledger.Account{
		ID: id, Code: code, Name: "Bank " + code,
		Type: domain.AccountAsset, Category: ledger.CategoryBank, IsActive: true,
	}
}

// payAccounts merakit lookup akun untuk jalur pembayaran: satu akun kas yang
// sah + seluruh akun kewajiban dari registry peran.
func payAccounts(cashCode string, extra ...string) *fakeAccounts {
	f := newFakeAccounts(extra...)
	f.byCode[cashCode] = cashAccount(900, cashCode)
	var id uint64 = 200
	for _, c := range []string{payableAccountCode(), retentionPayableCode(), vendorAdvanceCode()} {
		if c == "" || f.byCode[c] != nil {
			continue
		}
		id++
		f.byCode[c] = &ledger.Account{ID: id, Code: c, Name: "Akun " + c}
	}
	return f
}

func plan(t *testing.T, invoiceID uint64, number, before, amount string) allocationPlan {
	t.Helper()
	return allocationPlan{
		invoice:           &Invoice{ID: invoiceID, InvoiceNumber: number},
		outstandingBefore: money(t, before),
		amount:            money(t, amount),
	}
}

func TestComposePayment_BentukDasar(t *testing.T) {
	acc := payAccounts("1-1300")
	plans := []allocationPlan{plan(t, 1, "INV-001", "10000000", "10000000")}

	comp, err := composePayment(context.Background(), acc, 1, PaymentKindInvoice,
		"1-1300", plans, money(t, "10000000"))
	if err != nil {
		t.Fatalf("komposisi gagal: %v", err)
	}
	if len(comp.lines) != 2 {
		t.Fatalf("baris jurnal = %d, mau 2", len(comp.lines))
	}

	debit, credit := domain.Zero, domain.Zero
	for _, l := range comp.lines {
		debit = debit.Add(l.debit)
		credit = credit.Add(l.credit)
	}
	if !debit.Equal(credit) {
		t.Fatalf("jurnal tidak seimbang: debit %s, kredit %s", debit, credit)
	}
	if !debit.Equal(money(t, "10000000")) {
		t.Fatalf("nilai jurnal %s, mau 10000000", debit)
	}
	if comp.lines[0].account.Code != payableAccountCode() {
		t.Fatalf("akun debit %s, mau %s", comp.lines[0].account.Code, payableAccountCode())
	}
	if comp.lines[1].account.Code != "1-1300" || !comp.lines[1].credit.Equal(money(t, "10000000")) {
		t.Fatalf("baris kas salah: %+v", comp.lines[1])
	}
	// Baris kewajiban tidak boleh di-tag proyek — ia bukan biaya.
	for i, l := range comp.lines {
		if l.projectID != nil || l.unitID != nil || l.phaseID != nil {
			t.Fatalf("baris %d membawa tag proyek/unit/fase pada jurnal pembayaran", i)
		}
	}
}

// INV-AP-3 dalam bentuk yang paling penting: berapa pun alokasinya, tidak satu
// pun baris jurnal pembayaran boleh mendarat di akun taksonomi biaya. Kalau ia
// bisa, membayar tagihan menambah realisasi RAB untuk kedua kalinya.
func TestComposePayment_TidakPernahMenyentuhAkunBiaya(t *testing.T) {
	acc := payAccounts("1-1300")
	plans := []allocationPlan{
		plan(t, 1, "INV-001", "6000000", "6000000"),
		plan(t, 2, "INV-002", "4000000", "4000000"),
	}
	comp, err := composePayment(context.Background(), acc, 1, PaymentKindInvoice,
		"1-1300", plans, money(t, "10000000"))
	if err != nil {
		t.Fatalf("komposisi gagal: %v", err)
	}
	for _, l := range comp.lines {
		if cost.IsCostTaxonomyAccount(l.account.Code) {
			t.Fatalf("jurnal pembayaran menyentuh akun taksonomi biaya %s", l.account.Code)
		}
	}
}

// Kalau akun kas yang diminta ternyata akun taksonomi biaya, komposisi harus
// BERHENTI — bukan menerbitkan jurnal yang membuat pembayaran terhitung biaya.
func TestComposePayment_KasBerupaAkunBiayaDitolak(t *testing.T) {
	code := taxonomyCode(t)
	acc := payAccounts("1-1300")
	acc.byCode[code] = &ledger.Account{
		ID: 999, Code: code, Name: "Akun " + code,
		Type: domain.AccountAsset, Category: ledger.CategoryCash, IsActive: true,
	}
	_, err := composePayment(context.Background(), acc, 1, PaymentKindInvoice,
		code, []allocationPlan{plan(t, 1, "INV-001", "1000000", "1000000")}, money(t, "1000000"))
	if !errors.Is(err, ErrPaymentTouchesCost) {
		t.Fatalf("error = %v, mau ErrPaymentTouchesCost", err)
	}
}

func TestComposePayment_AkunKasHarusKasBank(t *testing.T) {
	acc := payAccounts("1-1300", "2-1500")
	// 2-1500 dibuat lewat newFakeAccounts: tanpa tipe, tanpa kategori, tidak
	// aktif — persis akun yang tidak boleh menjadi sumber dana.
	_, err := composePayment(context.Background(), acc, 1, PaymentKindInvoice,
		"2-1500", []allocationPlan{plan(t, 1, "INV-001", "1000000", "1000000")}, money(t, "1000000"))
	if !errors.Is(err, ErrCashAccount) {
		t.Fatalf("error = %v, mau ErrCashAccount", err)
	}
}

func TestComposePayment_SigmaAlokasiHarusSamaDenganNilaiPembayaran(t *testing.T) {
	acc := payAccounts("1-1300")
	plans := []allocationPlan{
		plan(t, 1, "INV-001", "6000000", "6000000"),
		plan(t, 2, "INV-002", "3000000", "3000000"),
	}
	_, err := composePayment(context.Background(), acc, 1, PaymentKindInvoice,
		"1-1300", plans, money(t, "10000000"))
	if !errors.Is(err, ErrAllocationSumMismatch) {
		t.Fatalf("error = %v, mau ErrAllocationSumMismatch", err)
	}
}

func TestComposePayment_TanpaAlokasiDitolak(t *testing.T) {
	acc := payAccounts("1-1300")
	_, err := composePayment(context.Background(), acc, 1, PaymentKindInvoice,
		"1-1300", nil, money(t, "1000000"))
	if !errors.Is(err, ErrNoAllocations) {
		t.Fatalf("error = %v, mau ErrNoAllocations", err)
	}
}

func TestComposePayment_NominalTidakSah(t *testing.T) {
	acc := payAccounts("1-1300")
	cases := []struct {
		name   string
		amount string
		want   error
	}{
		{"nol", "0", ErrNegAmount},
		{"negatif", "-1000", ErrNegAmount},
		{"pecahan sen", "1000000.50", ErrAmountFrac},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := composePayment(context.Background(), acc, 1, PaymentKindInvoice,
				"1-1300", []allocationPlan{plan(t, 1, "INV-001", "1000000", "1000000")}, money(t, c.amount))
			if !errors.Is(err, c.want) {
				t.Fatalf("error = %v, mau %v", err, c.want)
			}
		})
	}
}

// Jenis pembayaran menentukan akun yang DIDEBIT. Ketiganya harus berbeda —
// menukarnya menghasilkan jurnal yang tetap seimbang tetapi memindahkan saldo
// dari kewajiban yang salah.
func TestComposePayment_AkunKewajibanPerJenis(t *testing.T) {
	acc := payAccounts("1-1300")
	cases := []struct {
		kind PaymentKind
		want string
	}{
		{PaymentKindInvoice, payableAccountCode()},
		{PaymentKindRetention, retentionPayableCode()},
		{PaymentKindAdvance, vendorAdvanceCode()},
	}
	seen := map[string]bool{}
	for _, c := range cases {
		t.Run(string(c.kind), func(t *testing.T) {
			comp, err := composePayment(context.Background(), acc, 1, c.kind,
				"1-1300", []allocationPlan{plan(t, 1, "INV-001", "1000000", "1000000")}, money(t, "1000000"))
			if err != nil {
				t.Fatalf("komposisi gagal: %v", err)
			}
			if comp.lines[0].account.Code != c.want {
				t.Fatalf("akun debit %s, mau %s", comp.lines[0].account.Code, c.want)
			}
			if seen[c.want] {
				t.Fatalf("jenis %s memakai akun yang sama dengan jenis lain (%s)", c.kind, c.want)
			}
			seen[c.want] = true
		})
	}
}

func TestComposePayment_JenisTidakDikenal(t *testing.T) {
	acc := payAccounts("1-1300")
	_, err := composePayment(context.Background(), acc, 1, PaymentKind("transfer"),
		"1-1300", []allocationPlan{plan(t, 1, "INV-001", "1000000", "1000000")}, money(t, "1000000"))
	if !errors.Is(err, ErrPaymentKind) {
		t.Fatalf("error = %v, mau ErrPaymentKind", err)
	}
}

// ── Pembagian otomatis (tertua dulu) ─────────────────────────────────────────

func inv(id uint64, number, due, payable string) *Invoice {
	d, _ := parseDate(due, "due")
	m, _ := domain.NewMoney(payable)
	return &Invoice{ID: id, InvoiceNumber: number, DueDate: d, InvoiceDate: d, PayableAmount: m}
}

func TestAllocateOldestFirst_UrutanJatuhTempo(t *testing.T) {
	invoices := []*Invoice{
		inv(3, "INV-C", "2026-03-01", "5000000"),
		inv(1, "INV-A", "2026-01-01", "4000000"),
		inv(2, "INV-B", "2026-02-01", "3000000"),
	}
	outstanding := map[uint64]domain.Money{
		1: money(t, "4000000"), 2: money(t, "3000000"), 3: money(t, "5000000"),
	}

	plans, err := allocateOldestFirst(invoices, outstanding, money(t, "6000000"))
	if err != nil {
		t.Fatalf("alokasi gagal: %v", err)
	}
	if len(plans) != 2 {
		t.Fatalf("alokasi = %d, mau 2", len(plans))
	}
	if plans[0].invoice.InvoiceNumber != "INV-A" || !plans[0].amount.Equal(money(t, "4000000")) {
		t.Fatalf("alokasi pertama salah: %s %s", plans[0].invoice.InvoiceNumber, plans[0].amount)
	}
	if plans[1].invoice.InvoiceNumber != "INV-B" || !plans[1].amount.Equal(money(t, "2000000")) {
		t.Fatalf("alokasi kedua salah: %s %s", plans[1].invoice.InvoiceNumber, plans[1].amount)
	}

	sum := domain.Zero
	for _, p := range plans {
		sum = sum.Add(p.amount)
	}
	if !sum.Equal(money(t, "6000000")) {
		t.Fatalf("Σ alokasi %s, mau 6000000", sum)
	}
}

func TestAllocateOldestFirst_MelebihiSisaDitolak(t *testing.T) {
	invoices := []*Invoice{inv(1, "INV-A", "2026-01-01", "4000000")}
	outstanding := map[uint64]domain.Money{1: money(t, "4000000")}

	_, err := allocateOldestFirst(invoices, outstanding, money(t, "5000000"))
	if !errors.Is(err, ErrOverpayment) {
		t.Fatalf("error = %v, mau ErrOverpayment", err)
	}
	var over *OverpaymentError
	if !errors.As(err, &over) {
		t.Fatal("error tidak membawa angka kelebihan bayar")
	}
	if !over.Excess().Equal(money(t, "1000000")) {
		t.Fatalf("kelebihan %s, mau 1000000", over.Excess())
	}
}

func TestAllocateOldestFirst_TagihanLunasDilewati(t *testing.T) {
	invoices := []*Invoice{
		inv(1, "INV-A", "2026-01-01", "4000000"),
		inv(2, "INV-B", "2026-02-01", "3000000"),
	}
	outstanding := map[uint64]domain.Money{1: domain.Zero, 2: money(t, "3000000")}

	plans, err := allocateOldestFirst(invoices, outstanding, money(t, "3000000"))
	if err != nil {
		t.Fatalf("alokasi gagal: %v", err)
	}
	if len(plans) != 1 || plans[0].invoice.InvoiceNumber != "INV-B" {
		t.Fatalf("alokasi salah: %+v", plans)
	}
}

func TestAllocateOldestFirst_SemuaLunas(t *testing.T) {
	invoices := []*Invoice{inv(1, "INV-A", "2026-01-01", "4000000")}
	_, err := allocateOldestFirst(invoices, map[uint64]domain.Money{1: domain.Zero}, money(t, "1000000"))
	if !errors.Is(err, ErrNothingToPay) {
		t.Fatalf("error = %v, mau ErrNothingToPay", err)
	}
}

// ── Pembagian eksplisit ──────────────────────────────────────────────────────

func TestAllocateExplicit_SigmaWajibSamaDenganNilaiPembayaran(t *testing.T) {
	invoices := []*Invoice{
		inv(1, "INV-A", "2026-01-01", "4000000"),
		inv(2, "INV-B", "2026-02-01", "3000000"),
	}
	outstanding := map[uint64]domain.Money{1: money(t, "4000000"), 2: money(t, "3000000")}

	_, err := allocateExplicit(invoices, outstanding, []AllocationInput{
		{InvoiceID: 1, Amount: money(t, "1000000")},
	}, money(t, "2000000"))
	if !errors.Is(err, ErrAllocationSumMismatch) {
		t.Fatalf("error = %v, mau ErrAllocationSumMismatch", err)
	}
}

func TestAllocateExplicit_MelebihiSisaSatuTagihan(t *testing.T) {
	invoices := []*Invoice{inv(1, "INV-A", "2026-01-01", "4000000")}
	outstanding := map[uint64]domain.Money{1: money(t, "1000000")}

	_, err := allocateExplicit(invoices, outstanding, []AllocationInput{
		{InvoiceID: 1, Amount: money(t, "4000000")},
	}, money(t, "4000000"))

	var over *OverpaymentError
	if !errors.As(err, &over) {
		t.Fatalf("error = %v, mau OverpaymentError", err)
	}
	if over.InvoiceNumber != "INV-A" {
		t.Fatalf("error menunjuk tagihan %q, mau INV-A", over.InvoiceNumber)
	}
	if !over.Outstanding.Equal(money(t, "1000000")) || !over.Excess().Equal(money(t, "3000000")) {
		t.Fatalf("angka salah: sisa %s, kelebihan %s", over.Outstanding, over.Excess())
	}
}

func TestAllocateExplicit_NolDitolak(t *testing.T) {
	invoices := []*Invoice{inv(1, "INV-A", "2026-01-01", "4000000")}
	outstanding := map[uint64]domain.Money{1: money(t, "4000000")}

	_, err := allocateExplicit(invoices, outstanding, []AllocationInput{
		{InvoiceID: 1, Amount: domain.Zero},
	}, domain.Zero)
	if !errors.Is(err, ErrAllocationZero) {
		t.Fatalf("error = %v, mau ErrAllocationZero", err)
	}
}

func TestAllocateExplicit_TagihanTidakDikenal(t *testing.T) {
	invoices := []*Invoice{inv(1, "INV-A", "2026-01-01", "4000000")}
	outstanding := map[uint64]domain.Money{1: money(t, "4000000")}

	_, err := allocateExplicit(invoices, outstanding, []AllocationInput{
		{InvoiceID: 77, Amount: money(t, "1000000")},
	}, money(t, "1000000"))
	if !errors.Is(err, ErrInvoiceNotFound) {
		t.Fatalf("error = %v, mau ErrInvoiceNotFound", err)
	}
}

// allocationTypeFor: `advance` sebagai jenis PEMBAYARAN tidak boleh menghasilkan
// alokasi bertipe `advance`. Yang terakhir berarti KONSUMSI uang muka, dan DB
// mewajibkannya menunjuk source_payment_id (chk_apa_origin) — baris seperti itu
// mustahil lahir dari jalur pembayaran biasa.
func TestAllocationTypeFor(t *testing.T) {
	if got := allocationTypeFor(PaymentKindInvoice); got != AllocationInvoice {
		t.Fatalf("invoice → %s, mau %s", got, AllocationInvoice)
	}
	if got := allocationTypeFor(PaymentKindRetention); got != AllocationRetention {
		t.Fatalf("retention → %s, mau %s", got, AllocationRetention)
	}
	if got := allocationTypeFor(PaymentKindAdvance); got == AllocationAdvance {
		t.Fatal("pembayaran uang muka menghasilkan alokasi bertipe advance — " +
			"baris itu wajib menunjuk source_payment_id dan akan ditolak DB")
	}
}

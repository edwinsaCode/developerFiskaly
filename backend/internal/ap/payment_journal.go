package ap

import (
	"context"
	"fmt"

	"esaproperti/internal/cost"
	"esaproperti/internal/domain"
	"esaproperti/internal/ledger"
)

// Komposisi jurnal PEMBAYARAN ke vendor.
//
// Ini satu-satunya tempat bentuk jurnal pembayaran ditentukan. Pratinjau dan
// pencatatan memanggil fungsi yang SAMA — sama seperti jalur pengakuan — supaya
// angka yang disetujui operator mustahil berbeda dari jurnal yang terbit.
//
// BENTUK JURNALNYA:
//
//	Dr  akun kewajiban (per alokasi)      Σ = nilai pembayaran
//	    Cr  akun kas/bank                     nilai pembayaran
//
// Akun kewajiban ditentukan oleh JENIS pembayaran, bukan oleh isi tagihan:
//
//	invoice   2-1000 Hutang Usaha
//	retention 2-1100 Hutang Retensi
//	advance   1-5300 Uang Muka Vendor   ← DEBIT ASET, bukan pelunasan kewajiban
//
// YANG TIDAK BOLEH ADA DI JURNAL INI — dan ditegakkan di bawah, bukan sekadar
// diuji: satu pun barisnya tidak boleh menyentuh akun taksonomi biaya. Kalau ia
// bisa, membayar tagihan akan menambah realisasi RAB untuk kedua kalinya, dan
// selisihnya tidak akan pernah memunculkan error di laporan mana pun
// (INV-AP-3/INV-AP-4). Karena itu pemeriksaannya dipasang pada AKUN HASIL
// RESOLUSI, bukan pada kode yang ditulis di sini: bila suatu hari taksonomi
// biaya berubah sehingga 2-1000 masuk ke dalamnya, modul ini berhenti bekerja
// alih-alih diam-diam menghitung pembayaran sebagai biaya.

// allocationPlan adalah satu alokasi yang SUDAH tervalidasi terhadap sisa
// tagihannya — dengan baris tagihan yang sudah dikunci (R-12).
type allocationPlan struct {
	invoice *Invoice
	// outstandingBefore adalah sisa tagihan pada saat baris dikunci, bukan saat
	// permintaan masuk. Dua angka itu bisa berbeda kalau ada pembayaran lain yang
	// commit di antaranya — dan yang berlaku adalah yang dibaca di bawah kunci.
	outstandingBefore domain.Money
	amount            domain.Money
}

// paymentComposition adalah hasil komposisi lengkap satu jurnal pembayaran.
type paymentComposition struct {
	lines []composedLine
	total domain.Money
	// cash adalah akun kas/bank yang sudah terbukti sah sebagai sumber dana.
	cash *ledger.Account
}

// liabilityCodeFor memetakan jenis pembayaran ke akun yang DIDEBIT.
//
// Ketiganya sengaja terpisah: mengganti salah satunya dengan yang lain
// menghasilkan jurnal yang tetap seimbang tetapi memindahkan saldo dari
// kewajiban yang salah — kesalahan yang hanya terlihat saat rekonsiliasi vendor.
func liabilityCodeFor(kind PaymentKind) (string, error) {
	switch kind {
	case PaymentKindInvoice:
		return payableAccountCode(), nil
	case PaymentKindRetention:
		return retentionPayableCode(), nil
	case PaymentKindAdvance:
		return vendorAdvanceCode(), nil
	}
	return "", fmt.Errorf("%w: %q", ErrPaymentKind, string(kind))
}

// composePayment menyusun jurnal pembayaran dan MENOLAK setiap bentuk yang
// melanggar invariant — sebelum satu baris pun ditulis.
func composePayment(
	ctx context.Context,
	acc accountLookup,
	tenantID uint64,
	kind PaymentKind,
	cashCode string,
	plans []allocationPlan,
	total domain.Money,
) (*paymentComposition, error) {
	if !kind.Valid() {
		return nil, fmt.Errorf("%w: %q", ErrPaymentKind, string(kind))
	}
	if !total.GreaterThan(domain.Zero) {
		return nil, fmt.Errorf("%w: nilai pembayaran %s", ErrNegAmount, total.String())
	}
	if !total.IsWholeRupiah() {
		return nil, ErrAmountFrac
	}
	if len(plans) == 0 {
		return nil, ErrNoAllocations
	}

	// ── Sisi DEBIT: kewajiban yang berkurang ────────────────────────────────
	liabCode, err := liabilityCodeFor(kind)
	if err != nil {
		return nil, err
	}
	if liabCode == "" {
		return nil, fmt.Errorf("%w: akun kewajiban untuk pembayaran %q kosong di registry", ErrAccountNotFound, string(kind))
	}
	// INV-AP-3 di bentuk yang tidak bisa dilewati.
	if cost.IsCostTaxonomyAccount(liabCode) {
		return nil, fmt.Errorf("%w: akun %s terbaca sebagai akun taksonomi biaya", ErrPaymentTouchesCost, liabCode)
	}
	liab, err := acc.AccountByCode(ctx, tenantID, liabCode)
	if err != nil {
		return nil, err
	}

	var lines []composedLine
	sum := domain.Zero
	for i, p := range plans {
		if !p.amount.GreaterThan(domain.Zero) {
			return nil, fmt.Errorf("%w: alokasi %d bernilai %s", ErrAllocationZero, i+1, p.amount.String())
		}
		if !p.amount.IsWholeRupiah() {
			return nil, ErrAmountFrac
		}
		// Baris kewajiban TIDAK di-tag proyek: ia bukan biaya, dan memberinya tag
		// membuat akun kewajiban ikut muncul di pembacaan per-proyek.
		lines = append(lines, composedLine{
			account:     liab,
			debit:       p.amount,
			credit:      domain.Zero,
			description: paymentLineDescription(p.invoice),
		})
		sum = sum.Add(p.amount)
	}
	if !sum.Equal(total) {
		return nil, fmt.Errorf("%w: Σ alokasi %s, nilai pembayaran %s", ErrAllocationSumMismatch, sum.String(), total.String())
	}

	// ── Sisi KREDIT: kas keluar ─────────────────────────────────────────────
	//
	// Akun kas divalidasi lewat ledger.ValidatePaymentAccount — otoritas yang
	// sama dengan GET /ledger/accounts/cash-bank dan jalur biaya kas. Daftar kode
	// hardcoded adalah cara yang sudah terbukti salah.
	cash, err := acc.AccountByCode(ctx, tenantID, cashCode)
	if err != nil {
		return nil, err
	}
	if err := ledger.ValidatePaymentAccount(cash); err != nil {
		return nil, fmt.Errorf("%w: %s (%v)", ErrCashAccount, cashCode, err)
	}
	if cost.IsCostTaxonomyAccount(cash.Code) {
		return nil, fmt.Errorf("%w: akun kas %s terbaca sebagai akun taksonomi biaya", ErrPaymentTouchesCost, cash.Code)
	}
	lines = append(lines, composedLine{
		account:     cash,
		debit:       domain.Zero,
		credit:      total,
		description: "Pembayaran ke vendor",
	})

	// ── Assert terakhir sebelum menulis ─────────────────────────────────────
	//
	// PostingService juga menolak jurnal tak seimbang, tetapi pesannya tidak
	// menyebut komponen mana yang tidak cocok — dan komponen itulah yang perlu
	// diperbaiki.
	totalDebit, totalCredit := domain.Zero, domain.Zero
	for _, l := range lines {
		totalDebit = totalDebit.Add(l.debit)
		totalCredit = totalCredit.Add(l.credit)
	}
	if !totalDebit.Equal(totalCredit) {
		return nil, fmt.Errorf("%w: debit %s vs kredit %s", ErrComposeUnbalanced, totalDebit.String(), totalCredit.String())
	}

	return &paymentComposition{lines: lines, total: total, cash: cash}, nil
}

func paymentLineDescription(inv *Invoice) string {
	if inv == nil {
		return "Pelunasan kewajiban vendor"
	}
	return fmt.Sprintf("Pelunasan tagihan no. %s", inv.InvoiceNumber)
}

// ledgerLines menerjemahkan komposisi menjadi input PostingService.
func (c *paymentComposition) ledgerLines() []ledger.LineInput {
	out := make([]ledger.LineInput, 0, len(c.lines))
	for _, l := range c.lines {
		out = append(out, ledger.LineInput{
			AccountID:   l.account.ID,
			Debit:       l.debit,
			Credit:      l.credit,
			Description: l.description,
		})
	}
	return out
}

func (c *paymentComposition) previewLines() []PreviewLine {
	out := make([]PreviewLine, 0, len(c.lines))
	for _, l := range c.lines {
		out = append(out, PreviewLine{
			AccountCode: l.account.Code,
			AccountName: l.account.Name,
			Debit:       l.debit.String(),
			Credit:      l.credit.String(),
			Description: l.description,
		})
	}
	return out
}

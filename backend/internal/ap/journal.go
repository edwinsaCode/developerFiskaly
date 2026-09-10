package ap

import (
	"context"
	"fmt"

	"esaproperti/internal/cost"
	"esaproperti/internal/domain"
	"esaproperti/internal/ledger"
)

// Komposisi jurnal PENGAKUAN kewajiban vendor.
//
// Ini satu-satunya tempat di mana bentuk jurnal tagihan ditentukan. Preview dan
// posting memanggil fungsi yang SAMA — bukan dua fungsi yang "seharusnya"
// menghasilkan hal yang sama — sehingga pratinjau yang disetujui pengguna
// mustahil berbeda dari jurnal yang akhirnya terbit.
//
// BENTUK JURNALNYA (D-16, PPN-1..PPN-6):
//
//	Dr  akun taksonomi biaya (per baris)   Σ = DPP
//	Dr  1-5100 PPN Masukan                     PPN        ← tanpa tag proyek
//	    Cr  2-1000 Hutang Usaha                    payable
//	    Cr  2-1100 Hutang Retensi                  retensi
//	    Cr  1-5300 Uang Muka Vendor                kompensasi uang muka
//
// Yang harus dibaca dari bentuk itu: PPN, retensi, dan uang muka HANYA memecah
// sisi KREDIT. Tidak satu pun dari ketiganya boleh menambah atau mengurangi
// sisi debit — karena sisi debit itulah yang dibaca sebagai realisasi RAB.
// Tagihan 100 juta + PPN 11 juta tetap merealisasi RAB sebesar 100 juta, sama
// persis dengan tagihan 100 juta dari vendor non-PKP (INV-AP-17).

// Amounts adalah sisi nominal sebuah tagihan.
//
// PayableAmount TIDAK diterima dari pemanggil — ia diturunkan. Menerimanya
// sebagai input berarti mengizinkan klien mengirim angka yang tidak konsisten
// dengan komponennya, dan jurnalnya akan tetap balanced karena selisihnya
// tertutup di sisi kredit yang lain.
type Amounts struct {
	DPP       domain.Money
	PPN       domain.Money
	Retention domain.Money
	Advance   domain.Money
}

// Payable adalah kas yang pada akhirnya harus keluar untuk tagihan ini.
func (a Amounts) Payable() domain.Money {
	return a.DPP.Add(a.PPN).Sub(a.Retention).Sub(a.Advance)
}

// plannedLine adalah satu baris biaya yang SUDAH lolos cost.PlanAPLine.
// Tipe ini tidak bisa dibuat tanpa melewati planner — itu memang tujuannya.
type plannedLine struct {
	in   InvoiceLineInput
	plan cost.CostLinePlan
}

// accountLookup dipenuhi *Repository (termasuk yang terikat transaksi).
type accountLookup interface {
	AccountByCode(ctx context.Context, tenantID uint64, code string) (*ledger.Account, error)
}

// composedLine adalah satu baris jurnal beserta akunnya yang sudah teresolusi,
// sehingga pratinjau (yang butuh nama akun) dan posting (yang butuh ID) lahir
// dari objek yang sama.
type composedLine struct {
	account     *ledger.Account
	debit       domain.Money
	credit      domain.Money
	projectID   *uint64
	phaseID     *uint64
	unitID      *uint64
	description string
}

// composition adalah hasil komposisi lengkap sebuah jurnal pengakuan.
type composition struct {
	lines   []composedLine
	amounts Amounts
	payable domain.Money
	// taxonomyDebit adalah Σ debit ke akun yang DIBACA sebagai realisasi RAB.
	// Disimpan supaya INV-AP-8 bisa di-assert, bukan sekadar diuji.
	taxonomyDebit domain.Money
}

// compose menyusun jurnal pengakuan dan MENOLAK setiap bentuk yang melanggar
// invariant — sebelum satu baris pun ditulis.
func compose(
	ctx context.Context,
	acc accountLookup,
	tenantID uint64,
	amt Amounts,
	planned []plannedLine,
) (*composition, error) {
	if len(planned) == 0 {
		return nil, ErrNoLines
	}
	if amt.DPP.IsZero() || amt.DPP.IsNeg() {
		return nil, ErrDPPZeroOrNeg
	}
	for _, m := range []domain.Money{amt.PPN, amt.Retention, amt.Advance} {
		if m.IsNeg() {
			return nil, ErrNegAmount
		}
		if !m.IsWholeRupiah() {
			return nil, ErrAmountFrac
		}
	}
	if amt.Retention.GreaterThan(amt.DPP) {
		return nil, ErrRetentionExceedsDPP
	}

	// ── Sisi DEBIT: baris biaya ─────────────────────────────────────────────
	//
	// INV-AP-8 ditegakkan DI SINI, bukan hanya di test: Σ baris biaya harus
	// sama dengan DPP, dan seluruh baris itu harus mendarat di akun taksonomi
	// — KECUALI satu bentuk yang sengaja dikecualikan: RULE KLIEN 2026-09-04,
	// item/proyek Hard cost yang SUDAH dikapitalisasi penuh ke Persediaan saat
	// RAB approval. Untuk baris itu, cost.Service.plan() mengalihkan DebitCode
	// dari akun taksonomi ke payableAccountCode() sendiri (lihat komentar di
	// cost.Service.plan()) — supaya realisasi/invoice vendor pasca-approval
	// TIDAK mendebit Persediaan lagi (double-capitalize), melainkan mengakui
	// vendor lewat Dr Hutang Usaha / Cr Hutang Usaha (dipecah oleh package ini
	// sesuai retensi/uang muka di bawah).
	//
	// Sinyalnya aman dipakai sebagai pembeda "sengaja" vs "salah akun": tidak
	// ada jalur lain yang bisa membuat DebitCode == payableAccountCode(), sebab
	// costTaxonomyAccountCodes() (himpunan akun taksonomi) dan akun peran
	// RolePayable dijamin lepas satu sama lain — lihat cost.IsCostTaxonomyAccount.
	// Baris seperti ini SENGAJA tidak ikut taxonomyDebit: ia bukan realisasi RAB
	// baru (itu sudah terjadi saat RAB approval), jadi tidak boleh dihitung dua
	// kali di laporan yang membaca taxonomyDebit.
	var lines []composedLine
	sumLines := domain.Zero
	taxonomyDebit := domain.Zero
	creditCode := ""

	for i, pl := range planned {
		isCapitalizationRedirect := pl.plan.DebitCode == payableAccountCode()
		if !pl.plan.DebitIsTaxonomy && !isCapitalizationRedirect {
			return nil, fmt.Errorf("%w: baris %d mendebit %s", ErrLineAccountNotTaxonomy, i+1, pl.plan.DebitCode)
		}
		if creditCode == "" {
			creditCode = pl.plan.CreditCode
		} else if creditCode != pl.plan.CreditCode {
			// Dua akun kewajiban berbeda dalam satu tagihan berarti sebagian
			// nilainya berdiri di akun kontrol yang tidak dilacak modul ini.
			return nil, fmt.Errorf("%w: baris %d mengkredit %s, baris sebelumnya %s",
				ErrLineCreditMismatch, i+1, pl.plan.CreditCode, creditCode)
		}

		a, err := acc.AccountByCode(ctx, tenantID, pl.plan.DebitCode)
		if err != nil {
			return nil, err
		}
		var pid *uint64
		if pl.in.ProjectID != 0 {
			p := pl.in.ProjectID
			pid = &p
		}
		lines = append(lines, composedLine{
			account:     a,
			debit:       pl.in.Amount,
			credit:      domain.Zero,
			projectID:   pid,
			phaseID:     pl.in.PhaseID,
			unitID:      pl.in.UnitID,
			description: pl.plan.Description,
		})
		sumLines = sumLines.Add(pl.in.Amount)
		if !isCapitalizationRedirect {
			taxonomyDebit = taxonomyDebit.Add(pl.in.Amount)
		}
	}

	if !sumLines.Equal(amt.DPP) {
		return nil, fmt.Errorf("%w: Σ baris %s, DPP %s", ErrLineSumMismatch, sumLines.String(), amt.DPP.String())
	}

	// ── Sisi DEBIT: PPN Masukan ─────────────────────────────────────────────
	//
	// Kodenya diambil dari registry peran ledger, bukan ditulis "1-5100" di
	// sini. Dan sebelum dipakai, dibuktikan bahwa ia BUKAN akun taksonomi —
	// itulah INV-AP-17 dalam bentuk yang tidak bisa dilewati: kalau suatu hari
	// taksonomi biaya berubah sehingga 1-5100 masuk ke dalamnya, modul ini
	// berhenti bekerja alih-alih diam-diam menghitung PPN sebagai biaya proyek.
	if amt.PPN.GreaterThan(domain.Zero) {
		code := vatInputCode()
		if code == "" {
			return nil, fmt.Errorf("%w: peran PPN Masukan kosong di registry", ErrAccountNotFound)
		}
		if cost.IsCostTaxonomyAccount(code) {
			return nil, fmt.Errorf("%w: akun PPN %s terbaca sebagai akun taksonomi biaya", ErrPPNProjectTagged, code)
		}
		a, err := acc.AccountByCode(ctx, tenantID, code)
		if err != nil {
			return nil, err
		}
		// project/phase/unit sengaja nil — lihat INV-AP-17.
		lines = append(lines, composedLine{
			account:     a,
			debit:       amt.PPN,
			credit:      domain.Zero,
			description: "PPN Masukan",
		})
	}

	// ── Sisi KREDIT ─────────────────────────────────────────────────────────
	payable := amt.Payable()
	if payable.IsNeg() {
		// Retensi + kompensasi uang muka melebihi nilai tagihan: bukan kewajiban
		// negatif, melainkan input yang salah.
		return nil, fmt.Errorf("%w: kewajiban hasil hitung %s", ErrNegAmount, payable.String())
	}

	credits := []struct {
		code   string
		amount domain.Money
		desc   string
	}{
		{creditCode, payable, "Hutang usaha vendor"},
		{retentionPayableCode(), amt.Retention, "Retensi ditahan"},
		{vendorAdvanceCode(), amt.Advance, "Kompensasi uang muka vendor"},
	}
	for _, c := range credits {
		if !c.amount.GreaterThan(domain.Zero) {
			continue
		}
		if c.code == "" {
			return nil, fmt.Errorf("%w: akun kredit untuk %q kosong", ErrAccountNotFound, c.desc)
		}
		a, err := acc.AccountByCode(ctx, tenantID, c.code)
		if err != nil {
			return nil, err
		}
		// Baris kewajiban TIDAK di-tag proyek. Ia bukan biaya; memberinya tag
		// tidak menambah informasi apa pun dan membuat akun kewajiban ikut
		// muncul di pembacaan per-proyek yang menyaring berdasarkan tag.
		lines = append(lines, composedLine{
			account:     a,
			debit:       domain.Zero,
			credit:      c.amount,
			description: c.desc,
		})
	}

	// ── Assert terakhir sebelum menulis ─────────────────────────────────────
	//
	// PostingService memang menolak jurnal tak seimbang. Pemeriksaan ini tetap
	// ada karena pesannya berbeda: yang ingin diketahui saat ini gagal adalah
	// KOMPONEN mana yang tidak cocok, bukan sekadar "jurnal tidak balanced"
	// beberapa lapis di bawah.
	totalDebit, totalCredit := domain.Zero, domain.Zero
	for _, l := range lines {
		totalDebit = totalDebit.Add(l.debit)
		totalCredit = totalCredit.Add(l.credit)
	}
	if !totalDebit.Equal(totalCredit) {
		return nil, fmt.Errorf("%w: debit %s vs kredit %s (DPP %s, PPN %s, retensi %s, uang muka %s)",
			ErrComposeUnbalanced, totalDebit.String(), totalCredit.String(),
			amt.DPP.String(), amt.PPN.String(), amt.Retention.String(), amt.Advance.String())
	}
	// Tidak ada pemeriksaan tambahan "taxonomyDebit == DPP" di sini: itu sudah
	// ternyatakan sepenuhnya oleh sumLines.Equal(amt.DPP) di atas — SELAMA tidak
	// ada baris redirect kapitalisasi, taxonomyDebit == sumLines persis (setiap
	// baris yang lolos loop wajib taksonomi). Begitu ada baris redirect,
	// taxonomyDebit < sumLines BY DESIGN (baris itu memang bukan realisasi RAB
	// baru) — bukan sinyal kesalahan.
	return &composition{lines: lines, amounts: amt, payable: payable, taxonomyDebit: taxonomyDebit}, nil
}

// ledgerLines menerjemahkan komposisi menjadi input PostingService.
func (c *composition) ledgerLines() []ledger.LineInput {
	out := make([]ledger.LineInput, 0, len(c.lines))
	for _, l := range c.lines {
		out = append(out, ledger.LineInput{
			AccountID:   l.account.ID,
			Debit:       l.debit,
			Credit:      l.credit,
			ProjectID:   l.projectID,
			PhaseID:     l.phaseID,
			UnitID:      l.unitID,
			Description: l.description,
		})
	}
	return out
}

// PreviewLine adalah satu baris jurnal untuk ditampilkan sebelum disimpan.
type PreviewLine struct {
	AccountCode string  `json:"account_code"`
	AccountName string  `json:"account_name"`
	Debit       string  `json:"debit"`
	Credit      string  `json:"credit"`
	ProjectID   *uint64 `json:"project_id,omitempty"`
	UnitID      *uint64 `json:"unit_id,omitempty"`
	Description string  `json:"description"`
}

func (c *composition) previewLines() []PreviewLine {
	out := make([]PreviewLine, 0, len(c.lines))
	for _, l := range c.lines {
		out = append(out, PreviewLine{
			AccountCode: l.account.Code,
			AccountName: l.account.Name,
			Debit:       l.debit.String(),
			Credit:      l.credit.String(),
			ProjectID:   l.projectID,
			UnitID:      l.unitID,
			Description: l.description,
		})
	}
	return out
}

// ── Kode akun dari registry peran (nol literal di modul ini) ─────────────────

func roleCode(role ledger.AccountRole) string {
	codes := ledger.RoleCodeList(role)
	if len(codes) == 0 {
		return ""
	}
	return codes[0]
}

func payableAccountCode() string   { return roleCode(ledger.RolePayable) }
func vatInputCode() string         { return roleCode(ledger.RoleVATInput) }
func retentionPayableCode() string { return roleCode(ledger.RoleRetentionPayable) }
func vendorAdvanceCode() string    { return roleCode(ledger.RoleVendorAdvance) }

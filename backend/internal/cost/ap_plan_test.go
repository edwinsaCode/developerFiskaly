package cost_test

// W-11 (A-1) — REGRESI: PlanAPLine wajib memakai semantics yang SAMA dengan
// jalur biaya existing.
//
// Nilai file ini bukan "PlanAPLine mengembalikan 1-3100". Nilai file ini adalah
// pembuktian bahwa PlanAPLine TIDAK punya aturannya sendiri: untuk setiap
// request, hasil & error-nya harus identik dengan apa yang dilihat jalur biaya
// biasa (PreviewCostEntry) atas request yang sama. Bila suatu hari seseorang
// menambahkan cabang khusus AP di dalam plan(), atau menyalin validasi ke
// package ap, test ini yang jatuh lebih dulu — sebelum angka RAB menyimpang
// diam-diam di laporan.

import (
	"context"
	"errors"
	"testing"

	"esaproperti/internal/cost"
	"esaproperti/internal/domain"
)

// apReq mengembalikan request biaya proyek yang sah, tanpa metode pembayaran —
// PlanAPLine yang menormalkannya menjadi `payable`.
func apReq() cost.CreateCostEntryRequest {
	r := baseReq()
	r.PaymentMethod = ""
	r.BankAccountCode = ""
	return r
}

// planParity menjalankan request yang sama lewat dua pintu dan membandingkan
// hasilnya: PlanAPLine vs PreviewCostEntry (yang memanggil plan() yang sama).
func planParity(t *testing.T, req cost.CreateCostEntryRequest) {
	t.Helper()
	svc, _, _, _, _ := defaultTestService()
	ctx := context.Background()

	got, apErr := svc.PlanAPLine(ctx, 1, req)

	// Pembanding: jalur existing atas request yang SAMA, dengan metode
	// pembayaran terutang (satu-satunya bentuk yang mungkin untuk baris AP).
	ref := req
	ref.PaymentMethod = cost.PaymentMethodPayable
	ref.BankAccountCode = ""
	lines, refErr := svc.PreviewCostEntry(ctx, 1, ref)

	switch {
	case apErr != nil && refErr == nil:
		t.Fatalf("PlanAPLine menolak (%v) padahal jalur biaya existing menerima", apErr)
	case apErr == nil && refErr != nil:
		t.Fatalf("PlanAPLine menerima padahal jalur biaya existing menolak (%v)", refErr)
	case apErr != nil && refErr != nil:
		if !errors.Is(apErr, refErr) && apErr.Error() != refErr.Error() {
			t.Fatalf("error berbeda:\n  PlanAPLine = %v\n  Preview    = %v", apErr, refErr)
		}
		return
	}

	if len(lines) != 2 {
		t.Fatalf("preview mengembalikan %d baris, want 2", len(lines))
	}
	if got.DebitCode != lines[0].AccountCode {
		t.Errorf("DebitCode = %q, jalur existing mendebit %q", got.DebitCode, lines[0].AccountCode)
	}
	if got.CreditCode != lines[1].AccountCode {
		t.Errorf("CreditCode = %q, jalur existing mengkredit %q", got.CreditCode, lines[1].AccountCode)
	}
	if got.DebitIsTaxonomy != cost.IsCostTaxonomyAccount(got.DebitCode) {
		t.Errorf("DebitIsTaxonomy = %v tidak konsisten dengan IsCostTaxonomyAccount(%q)", got.DebitIsTaxonomy, got.DebitCode)
	}
	if got.Description == "" {
		t.Error("Description kosong: jurnal AP akan terbaca beda gaya dari jurnal kas")
	}
}

// TestPlanAPLine_Parity_WithExistingCostPath menyapu kombinasi tier × kategori
// yang sah maupun yang terlarang. Yang diuji adalah KESAMAAN kedua jalur, bukan
// nilai spesifiknya — jadi test ini tetap benar bila taksonomi akun berubah.
func TestPlanAPLine_Parity_WithExistingCostPath(t *testing.T) {
	unit := uint64(10)
	cases := []struct {
		name string
		mut  func(*cost.CreateCostEntryRequest)
	}{
		{"hard cost proyek (shared)", func(r *cost.CreateCostEntryRequest) {}},
		{"soft cost proyek", func(r *cost.CreateCostEntryRequest) { r.Category = domain.CostCategorySoft }},
		{"biaya pembiayaan", func(r *cost.CreateCostEntryRequest) { r.Category = domain.CostCategoryFinancing }},
		{"direct ke unit", func(r *cost.CreateCostEntryRequest) {
			r.CostTier = domain.CostTierDirect
			r.UnitID = &unit
		}},
		{"overhead marketing tanpa proyek", func(r *cost.CreateCostEntryRequest) {
			r.CostTier = domain.CostTierOverhead
			r.Category = domain.CostCategoryMarketing
			r.ProjectID = 0
		}},

		// ── Yang harus DITOLAK oleh kedua jalur dengan error yang sama ─────────
		{"direct tanpa unit", func(r *cost.CreateCostEntryRequest) { r.CostTier = domain.CostTierDirect }},
		{"shared dengan unit", func(r *cost.CreateCostEntryRequest) {
			r.CostTier = domain.CostTierShared
			r.UnitID = &unit
		}},
		{"kategori beban pada tier kapitalisasi", func(r *cost.CreateCostEntryRequest) {
			r.CostTier = domain.CostTierShared
			r.Category = domain.CostCategoryMarketing
		}},
		{"kategori kapitalisasi pada overhead", func(r *cost.CreateCostEntryRequest) {
			r.CostTier = domain.CostTierOverhead
			r.Category = domain.CostCategoryHard
		}},
		{"tanpa proyek pada tier kapitalisasi", func(r *cost.CreateCostEntryRequest) { r.ProjectID = 0 }},
		{"nominal nol", func(r *cost.CreateCostEntryRequest) { r.Amount = domain.Zero }},
		{"nominal negatif", func(r *cost.CreateCostEntryRequest) { r.Amount = domain.FromInt(-1) }},
		{"nominal pecahan rupiah", func(r *cost.CreateCostEntryRequest) {
			m, err := domain.NewMoney("1000.5000")
			if err != nil {
				panic(err)
			}
			r.Amount = m
		}},
		{"kategori tidak dikenal", func(r *cost.CreateCostEntryRequest) { r.Category = domain.CostCategory("gaib") }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := apReq()
			tc.mut(&req)
			planParity(t, req)
		})
	}
}

// TestPlanAPLine_ForcesPayable membuktikan tagihan vendor tidak bisa diam-diam
// mengkredit rekening bank, apa pun yang dikirim pemanggil. Tanpa normalisasi
// ini, satu field yang salah isi akan menghasilkan "hutang" yang uangnya sudah
// keluar — kas berkurang dua kali saat tagihannya dibayar.
func TestPlanAPLine_ForcesPayable(t *testing.T) {
	svc, _, _, _, _ := defaultTestService()

	req := apReq()
	req.PaymentMethod = cost.PaymentMethodBank
	req.BankAccountCode = "1-1300"

	got, err := svc.PlanAPLine(context.Background(), 1, req)
	if err != nil {
		t.Fatalf("PlanAPLine: %v", err)
	}
	if got.CreditCode == "1-1300" {
		t.Fatal("baris AP mengkredit rekening bank: metode pembayaran tidak dinormalkan ke payable")
	}

	// Pembanding: request payable eksplisit harus menghasilkan kredit yang sama.
	ref := apReq()
	ref.PaymentMethod = cost.PaymentMethodPayable
	want, err := svc.PlanAPLine(context.Background(), 1, ref)
	if err != nil {
		t.Fatalf("PlanAPLine (payable eksplisit): %v", err)
	}
	if got.CreditCode != want.CreditCode {
		t.Errorf("CreditCode = %q, want %q", got.CreditCode, want.CreditCode)
	}
}

// TestPlanAPLine_DebitIsTaxonomy_TracksRABReadership mengunci makna flag yang
// dipakai INV-AP-8/17: benar untuk akun yang DIBACA sebagai realisasi RAB, salah
// untuk akun yang tidak — khususnya PPN Masukan, yang boleh ikut dalam jurnal
// pengakuan tetapi tidak boleh dihitung sebagai biaya proyek.
func TestPlanAPLine_DebitIsTaxonomy_TracksRABReadership(t *testing.T) {
	svc, _, _, _, _ := defaultTestService()

	got, err := svc.PlanAPLine(context.Background(), 1, apReq())
	if err != nil {
		t.Fatalf("PlanAPLine: %v", err)
	}
	if !got.DebitIsTaxonomy {
		t.Errorf("biaya hard cost proyek mendebit %q tetapi tidak dianggap akun taksonomi", got.DebitCode)
	}
	if cost.IsCostTaxonomyAccount("1-5100") {
		t.Error("PPN Masukan 1-5100 terbaca sebagai akun taksonomi biaya: INV-AP-17 akan gagal — PPN akan terhitung sebagai realisasi RAB")
	}
	if cost.IsCostTaxonomyAccount("1-5300") {
		t.Error("Uang Muka Vendor 1-5300 terbaca sebagai akun taksonomi biaya: INV-AP-15 akan gagal — uang muka akan terhitung sebagai realisasi RAB")
	}
}

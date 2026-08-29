package domain

// ═════════════════════════════════════════════════════════════════════════════
// BILLING TREATMENT — SATU-SATUNYA sumber kebenaran atas pertanyaan:
// "uang yang ditagihkan ini menjadi PENDAPATAN perusahaan, atau TITIPAN untuk
// pihak ketiga?"
//
// Koreksi domain klien 2026-08-06 memisahkan dua taksonomi yang selama ini
// tercampur di satu master:
//
//	Produk yang DIJUAL          → Rumah, Ruko, Kelebihan Tanah.
//	                              Ditagih, diakui sebagai PENDAPATAN (4-xxxx),
//	                              punya HPP. Masternya: project.ProductType,
//	                              aturannya: ProductCategory.
//	Jenis BIAYA REALISASI       → Notaris, BPHTB, PDAM, Listrik.
//	                              Ditagih ke customer tapi TIDAK PERNAH menjadi
//	                              pendapatan — ia KEWAJIBAN kepada pihak ketiga
//	                              (2-xxxx) sampai perusahaan membayar vendornya.
//	                              Masternya: charge.RealizationChargeType.
//
// Dua master, SATU seam. BillingTreatment adalah seam itu: ia yang menjawab
// apakah sebuah baris tagihan boleh menyentuh akun pendapatan. Tidak boleh ada
// SQL, konstanta, atau cabang `if kind == "addon"` lain yang memutuskan hal yang
// sama (anti duplicate source of truth).
// ═════════════════════════════════════════════════════════════════════════════

// BillingTreatment mengklasifikasikan perlakuan akuntansi satu baris tagihan.
type BillingTreatment string

const (
	// TreatmentRevenue — uangnya menjadi milik perusahaan saat kriteria
	// pengakuan terpenuhi. Kredit ke akun pendapatan (4-xxxx).
	TreatmentRevenue BillingTreatment = "revenue"
	// TreatmentDepositLiability — uangnya TIDAK PERNAH menjadi milik perusahaan.
	// Kredit ke akun kewajiban titipan (2-xxxx); kewajiban baru berkurang saat
	// perusahaan membayar pihak ketiga.
	TreatmentDepositLiability BillingTreatment = "deposit_liability"
)

// AllBillingTreatments adalah daftar lengkap perlakuan yang dikenal sistem.
func AllBillingTreatments() []BillingTreatment {
	return []BillingTreatment{TreatmentRevenue, TreatmentDepositLiability}
}

func (t BillingTreatment) Valid() bool {
	for _, k := range AllBillingTreatments() {
		if t == k {
			return true
		}
	}
	return false
}

// RecognizesRevenue adalah FUNGSI KANONIK: bolehkah baris tagihan ini menyentuh
// akun pendapatan? Perlakuan tidak valid → false (fail-closed: lebih baik uang
// mengendap sebagai kewajiban daripada salah diakui sebagai pendapatan).
func (t BillingTreatment) RecognizesRevenue() bool { return t == TreatmentRevenue }

// IsThirdPartyDeposit: apakah uang ini kewajiban kepada pihak ketiga yang hanya
// boleh berkurang lewat pembayaran ke vendor / refund ke customer.
func (t BillingTreatment) IsThirdPartyDeposit() bool { return t == TreatmentDepositLiability }

// BillingTreatment produk: SETIAP produk yang dijual berperlakuan pendapatan.
// Inilah jembatan antara dua master — pembaca yang memegang ProductCategory dan
// pembaca yang memegang RealizationChargePolicy sama-sama bisa bertanya
// "RecognizesRevenue?" tanpa tahu master mana yang menjawabnya.
func (c ProductCategory) BillingTreatment() BillingTreatment { return TreatmentRevenue }

// RealizationChargePolicy adalah keputusan lengkap untuk satu kode jenis biaya
// realisasi: perlakuan + akun kewajiban titipan yang dipakai saat kas diterima.
// Value object murni — di-resolve dari master oleh package charge, lalu
// dikonsumsi jurnal/kwitansi/invoice tanpa masing-masing menafsir ulang.
type RealizationChargePolicy struct {
	Code               string
	Name               string
	Treatment          BillingTreatment
	DepositAccountCode string
}

// RecognizesRevenue mendelegasikan ke fungsi kanonik perlakuan.
func (p RealizationChargePolicy) RecognizesRevenue() bool { return p.Treatment.RecognizesRevenue() }

// IsThirdPartyDeposit mendelegasikan ke fungsi kanonik perlakuan.
func (p RealizationChargePolicy) IsThirdPartyDeposit() bool { return p.Treatment.IsThirdPartyDeposit() }

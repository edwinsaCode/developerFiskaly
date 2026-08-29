package cost

// W-11 (A-1) — jembatan perencanaan baris biaya untuk modul Hutang Usaha.
//
// KENAPA FILE INI ADA
//
// D-4 mewajibkan baris biaya yang lahir dari tagihan vendor mendarat di tabel
// `cost_entries` yang SAMA, karena budget.GetRealisasiPerItem membaca
// SUM(cost_entries.amount) — bukan ledger. Konsekuensinya baris AP harus lolos
// aturan yang persis sama dengan baris biaya biasa: matriks tier × kategori,
// unit wajib milik proyek yang dipilih, kelayakan HPP produk (fail-closed),
// kecocokan kategori item RAB, INV-EXP-2, serta tiga CHECK constraint di DB.
//
// Seluruh aturan itu hidup di Service.plan()/validate(). Menyalinnya ke package
// `ap` berarti dua definisi "baris biaya yang sah" yang pasti menyimpang begitu
// salah satu diubah — dan penyimpangannya tidak akan terlihat sebagai error,
// melainkan sebagai angka RAB yang beda antara dua laporan.
//
// Maka: `ap` TIDAK memvalidasi baris biaya sendiri. Ia memanggil pintu ini, yang
// hanya membungkus plan() apa adanya. Tidak ada cabang khusus AP di dalam
// plan(); kalau suatu hari aturannya berubah, kedua jalur ikut berubah bersama.
//
// Yang TIDAK diputuskan di sini: sisi kredit jurnal. Pengakuan hutang memecah
// kreditnya menjadi 2-1000 / 2-1100 / 1-5300 sesuai retensi & uang muka, dan itu
// urusan package `ap`. Yang dipinjam dari sini hanya sisi DEBIT (akun biaya) —
// satu-satunya sisi yang menentukan realisasi RAB.

import (
	"context"

	"esaproperti/internal/domain"
)

// CostLinePlan adalah bentuk terekspos dari costPlan: hasil perencanaan satu
// baris biaya, tanpa membocorkan internal Service.
type CostLinePlan struct {
	// Tier efektif hasil resolveTier — bukan nilai mentah dari request.
	Tier domain.CostTier
	// DebitCode adalah akun biaya/persediaan yang akan didebit. Inilah satu-
	// satunya nilai yang menentukan apakah baris ini terbaca sebagai realisasi
	// RAB oleh budget.GetRealisasiByProject.
	DebitCode string
	// CreditCode selalu akun Hutang Usaha untuk jalur AP. Dikembalikan supaya
	// pemanggil bisa MEMBUKTIKAN itu (assertion), bukan supaya memakainya:
	// komposisi kredit sebenarnya ditentukan di package ap.
	CreditCode string
	// DebitIsTaxonomy menandai apakah DebitCode termasuk himpunan akun yang
	// dibaca sebagai realisasi RAB. Dipakai menegakkan INV-AP-8/17.
	DebitIsTaxonomy bool
	// Description adalah deskripsi baris jurnal yang sama persis dengan yang
	// dipakai jalur biaya langsung, sehingga jurnal AP dan jurnal kas tidak
	// terbaca sebagai dua gaya pencatatan yang berbeda.
	Description string
}

// PlanAPLine memvalidasi dan merencanakan SATU baris biaya milik tagihan vendor.
//
// Metode ini sengaja tidak menerima metode pembayaran: baris AP menurut definisi
// bersifat terutang. Nilai apa pun yang dikirim pemanggil pada PaymentMethod /
// BankAccountCode diabaikan dan dinormalkan ke `payable`, agar tidak mungkin
// ada tagihan vendor yang diam-diam mengkredit rekening bank.
//
// Tidak menyentuh DB selain lewat resolver yang sudah dipakai plan() (jenis
// pengeluaran, kebijakan produk unit, kepemilikan unit, item RAB), dan tidak
// menulis apa pun.
func (s *Service) PlanAPLine(ctx context.Context, tenantID uint64, req CreateCostEntryRequest) (CostLinePlan, error) {
	req.PaymentMethod = PaymentMethodPayable
	req.BankAccountCode = ""

	p, err := s.plan(ctx, tenantID, req)
	if err != nil {
		return CostLinePlan{}, err
	}
	return CostLinePlan{
		Tier:            p.tier,
		DebitCode:       p.debitCode,
		CreditCode:      p.creditCode,
		DebitIsTaxonomy: IsCostTaxonomyAccount(p.debitCode),
		Description:     p.journalDescription(req),
	}, nil
}

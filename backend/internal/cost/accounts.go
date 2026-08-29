package cost

// Sumber kode akun untuk package cost.
//
// Aturan: TIDAK ADA kode akun yang ditulis literal di service/handler. Semuanya
// berasal dari satu tempat — taksonomi domain (domain.CostCategory) atau
// registry peran akun ledger (ledger.RoleCodeList). Kode akun yang tersebar
// sebagai string literal adalah cara paling mudah membuat dua bagian sistem
// diam-diam tidak sepakat soal ke mana uang dicatat.

import (
	"esaproperti/internal/domain"
	"esaproperti/internal/ledger"
)

// payableAccountCode mengembalikan akun Hutang Usaha kanonik dari registry
// peran ledger. Sebelumnya nilainya ditulis "2-1000" langsung di service.
func payableAccountCode() string {
	codes := ledger.RoleCodeList(ledger.RolePayable)
	if len(codes) == 0 {
		// Registry peran adalah konstanta compile-time di ledger; kosong berarti
		// bug pemrograman, bukan kondisi data. Kembalikan kosong agar
		// ResolveAccount gagal dengan ErrAccountNotFound alih-alih menebak.
		return ""
	}
	return codes[0]
}

// costTaxonomyAccountCodes adalah himpunan akun yang DIBACA sebagai realisasi
// RAB per kategori oleh budget.GetRealisasiByProject (lihat internal/budget/
// repository.go). Isinya diturunkan dari taksonomi domain — bukan disalin —
// sehingga penambahan kategori biaya otomatis ikut terlindungi.
//
// Dipakai untuk menegakkan INV-EXP-2: pengeluaran operasional yang akun
// bebannya ada di himpunan ini tidak boleh di-tag ke proyek, karena tag itu
// akan berubah menjadi realisasi RAB dan melanggar BD-2.
func costTaxonomyAccountCodes() map[string]struct{} {
	out := make(map[string]struct{}, len(domain.AllCostCategories)+len(domain.ExpenseCostCategories))
	for _, c := range domain.AllCostCategories {
		out[c.InventoryAccountCode()] = struct{}{}
	}
	for _, c := range domain.ExpenseCostCategories {
		out[c.ExpenseAccountCode()] = struct{}{}
	}
	return out
}

// IsCostTaxonomyAccount menjawab apakah sebuah kode akun termasuk himpunan di atas.
//
// Diekspor untuk W-11 (INV-AP-17): jurnal pengakuan hutang usaha harus bisa
// membuktikan bahwa baris PPN Masukan-nya BUKAN akun taksonomi biaya, dan bahwa
// Σ debit akun taksonomi persis sama dengan Σ cost_entries yang ditulisnya.
// Menyalin himpunan ini ke package ap akan membuat dua definisi "akun mana yang
// terbaca sebagai realisasi RAB" — persis kondisi yang registry ini hapus.
func IsCostTaxonomyAccount(code string) bool {
	_, ok := costTaxonomyAccountCodes()[code]
	return ok
}

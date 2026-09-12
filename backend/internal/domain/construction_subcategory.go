package domain

// ConstructionSubcategory memecah CostCategoryHard (Konstruksi) menjadi 4
// klasifikasi yang WAJIB terpisah untuk kebutuhan HPP (UAT 2026-09-07,
// business rule final klien):
//
//   - Produksi Subsidi dan Produksi Komersial nilainya berbeda dan TIDAK BOLEH
//     dicampur dalam satu pool alokasi — masing-masing hanya boleh membentuk
//     HPP unit/blok dengan TaxCategory yang sama (lihat RestrictsToTaxCategory).
//   - Sarana & Prasarana dan Perizinan tetap dialokasikan ke SEMUA unit
//     HPP-eligible sesuai basis alokasi existing (tidak dibatasi TaxCategory).
//
// SSOT — jangan sebarkan literal string subkategori di package lain.
type ConstructionSubcategory string

const (
	ConstructionProduksiSubsidi   ConstructionSubcategory = "produksi_subsidi"
	ConstructionProduksiKomersial ConstructionSubcategory = "produksi_komersial"
	ConstructionSaranaPrasarana   ConstructionSubcategory = "sarana_prasarana"
	ConstructionPerizinan         ConstructionSubcategory = "perizinan"
)

// AllConstructionSubcategories adalah urutan kanonik 4 subkategori Konstruksi.
var AllConstructionSubcategories = []ConstructionSubcategory{
	ConstructionProduksiSubsidi,
	ConstructionProduksiKomersial,
	ConstructionSaranaPrasarana,
	ConstructionPerizinan,
}

func (c ConstructionSubcategory) Valid() bool {
	switch c {
	case ConstructionProduksiSubsidi, ConstructionProduksiKomersial, ConstructionSaranaPrasarana, ConstructionPerizinan:
		return true
	default:
		return false
	}
}

// Label mengembalikan nama tampilan subkategori — SSOT untuk teks yang dilihat
// pengguna (RAB vs Realisasi Konstruksi), supaya tidak ada package lain yang
// menuliskan ulang label ini sebagai string literal.
func (c ConstructionSubcategory) Label() string {
	switch c {
	case ConstructionProduksiSubsidi:
		return "Produksi Subsidi"
	case ConstructionProduksiKomersial:
		return "Produksi Komersial"
	case ConstructionSaranaPrasarana:
		return "Sarana & Prasarana"
	case ConstructionPerizinan:
		return "Perizinan"
	default:
		return string(c)
	}
}

// RestrictsToTaxCategory mengembalikan TaxCategory yang berhak menerima HPP
// dari subkategori ini, dan true bila subkategori ini memang dibatasi.
// Sarana & Prasarana dan Perizinan mengembalikan (_, false) — dialokasikan ke
// semua unit HPP-eligible sesuai rule existing, tidak dibatasi Subsidi/Komersial.
func (c ConstructionSubcategory) RestrictsToTaxCategory() (TaxCategory, bool) {
	switch c {
	case ConstructionProduksiSubsidi:
		return TaxCategorySubsidi, true
	case ConstructionProduksiKomersial:
		return TaxCategoryKomersial, true
	default:
		return "", false
	}
}

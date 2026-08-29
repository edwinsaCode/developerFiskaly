package domain

// TaxCategory mengklasifikasikan proyek untuk PENENTUAN TARIF PAJAK
// (blueprint erp-blueprint-review.md §7): PPh Final subsidi 1% vs komersial
// 2,5%. SSOT — jangan sebarkan literal string kategori di package lain.
//
// BERBEDA dari customer segment (dimensi analitik reporting) — jangan dicampur:
// bank penyalur subsidi juga BUKAN penentu pajak (financing_sources.type).
type TaxCategory string

const (
	TaxCategorySubsidi   TaxCategory = "subsidi"
	TaxCategoryKomersial TaxCategory = "komersial"
)

// AllTaxCategories adalah urutan kanonik kategori pajak proyek.
var AllTaxCategories = []TaxCategory{TaxCategorySubsidi, TaxCategoryKomersial}

func (c TaxCategory) Valid() bool {
	return c == TaxCategorySubsidi || c == TaxCategoryKomersial
}

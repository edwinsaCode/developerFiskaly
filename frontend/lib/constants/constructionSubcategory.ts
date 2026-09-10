// UAT 2026-09-07: "Produksi" tunggal tidak cukup untuk menentukan HPP —
// Produksi Subsidi dan Komersial WAJIB terpisah karena nilai biaya
// produksinya berbeda dan alokasinya HANYA jatuh ke unit dengan tax_category
// yang sama (lihat allocation.ComputeHardPool di backend). Sarana & Prasarana
// dan Perizinan tetap dialokasikan sesuai rule existing (lintas semua unit).
//
// 4 nilai ini adalah string kanonik domain.ConstructionSubcategory — HARUS
// tetap sinkron dengan backend/internal/domain/construction_subcategory.go.
// Dipakai baik di RAB (BudgetItem.subcategory) maupun Cost Entry
// (CostEntry.hard_subcategory) — satu sumber kebenaran di frontend untuk
// keduanya supaya label dan nilai tidak pernah bergeser antar form.
export const CONSTRUCTION_SUBCATEGORIES: { value: string; label: string }[] = [
  { value: "produksi_subsidi",   label: "Produksi — Subsidi" },
  { value: "produksi_komersial", label: "Produksi — Komersial" },
  { value: "sarana_prasarana",   label: "Sarana & Prasarana" },
  { value: "perizinan",          label: "Perizinan" },
];

export function constructionSubcategoryLabel(value: string): string {
  return CONSTRUCTION_SUBCATEGORIES.find(s => s.value === value)?.label ?? value;
}

package budget

// capitalizationJournalSource menandai jurnal kapitalisasi RAB Construction
// historis (dari mekanisme RULE KLIEN 2026-09-04, DICABUT klien — lihat Item 8
// UAT 2026-09-07). Package ini tidak menulis jurnal ber-source ini lagi;
// konstanta dipertahankan agar GetRealisasiByProject tetap bisa mengecualikan
// jurnal lama itu (ExcludeSource) dari realisasi Hard pada tenant yang sempat
// memakai rule lama, mencegah realisasi ganda atas data historis.
const capitalizationJournalSource = "rab_capitalization"

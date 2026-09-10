-- Item 2 (RAB vs Realisasi, 2026-09-05): jurnal kapitalisasi RAB Construction
-- yang diposting SEBELUM budget.LedgerJournalAdapter.CreateJournal mulai
-- menandai source="rab_capitalization" masih tersimpan dengan source="system"
-- (default lama). Tanpa backfill ini, budget.GetRealisasiByProject.ExcludeSource
-- tidak akan mengecualikannya, menyebabkan double-capitalization pada laporan
-- RAB vs Realisasi untuk proyek yang RAB-nya sudah diapprove sebelum fix ini.
--
-- Jurnal kapitalisasi diidentifikasi lewat description literal unik yang HANYA
-- ditulis oleh postCapitalizationJournal (satu-satunya call site, lihat
-- internal/budget/service.go) — bukan lewat kecocokan nominal/timestamp yang
-- rapuh.
UPDATE journal_entries je
JOIN journal_lines jl ON jl.journal_entry_id = je.id
SET je.source = 'rab_capitalization'
WHERE je.source = 'system'
  AND jl.description IN (
    'Kapitalisasi RAB Construction disetujui',
    'Koreksi kapitalisasi RAB Construction (revisi turun)'
  );

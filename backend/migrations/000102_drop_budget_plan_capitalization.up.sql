-- Item 8 (UAT 2026-09-07): RULE KLIEN 2026-09-04 (kapitalisasi Construction
-- penuh ke Persediaan saat RAB approval) DICABUT klien. RAB tidak lagi
-- memposting jurnal apa pun; kolom ini tidak dipakai lagi.
ALTER TABLE budget_plans
  DROP COLUMN capitalized_at,
  DROP COLUMN capitalized_hard_amount;

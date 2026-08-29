-- Increment 2 hardening — database-level protection untuk invariant Cost 3-tier.
-- Service tetap lapisan validasi utama (error bertipe + pesan jelas); CHECK ini
-- adalah defense-in-depth agar direct write yang mem-bypass service (SQL manual,
-- tool lain, bug masa depan) TIDAK bisa memasukkan data yang melanggar tier.
-- MySQL 8.0.16+ meng-enforce CHECK; ADD CONSTRAINT memvalidasi baris existing.
--
-- Catatan taxonomy: daftar kategori di chk_ce_tier_category sengaja di-hardcode
-- di level schema (mirror domain.CostTier.AllowsCategory). Menambah kategori baru
-- memang HARUS lewat migration — itu perilaku yang diinginkan untuk taxonomy
-- akuntansi, bukan hambatan.

ALTER TABLE cost_entries
    -- Tier hanya boleh salah satu dari tiga nilai kanonik.
    ADD CONSTRAINT chk_ce_tier_valid CHECK (
        cost_tier IN ('direct', 'shared', 'overhead')
    ),
    -- Aggregate-root rule: direct/shared WAJIB project; hanya overhead boleh NULL.
    ADD CONSTRAINT chk_ce_tier_project CHECK (
        cost_tier = 'overhead' OR project_id IS NOT NULL
    ),
    -- Konsistensi atribusi: direct wajib unit; shared (pool) & overhead tanpa unit.
    ADD CONSTRAINT chk_ce_tier_unit CHECK (
        (cost_tier = 'direct' AND unit_id IS NOT NULL)
        OR (cost_tier <> 'direct' AND unit_id IS NULL)
    ),
    -- Matriks tier × kategori: kapitalisasi hanya direct/shared; beban hanya overhead.
    ADD CONSTRAINT chk_ce_tier_category CHECK (
        (cost_tier IN ('direct', 'shared') AND category IN ('land', 'hard', 'soft', 'financing'))
        OR (cost_tier = 'overhead' AND category IN ('marketing', 'other'))
    );

-- W-5 / J-14a — Biaya realisasi menjadi Piutang Customer.
--
-- R-5 (final klien): piutang lahir saat INVOICE diterbitkan, bukan saat grup
-- dibuat. Karena itu yang disimpan bukan "sudah/belum piutang" melainkan NILAI
-- TAGIHAN YANG SUDAH DI-INVOICE per item; kontribusinya ke 1-2000 diturunkan
-- (max(0, recognized_amount − paid)) sehingga tidak ada angka kembar yang bisa
-- menyimpang dari sub-ledger alokasi.
--
-- Pengakuan adalah atribut ITEM, bukan grup: akun kewajibannya pun atribut item
-- (W-1), dan satu grup boleh memuat item yang sudah di-invoice bersama item yang
-- baru ditambahkan.

ALTER TABLE charge_items
    ADD COLUMN recognized_amount     DECIMAL(20,4)   NOT NULL DEFAULT '0.0000' AFTER original_amount,
    ADD COLUMN recognized_at         DATETIME(3)     NULL     AFTER recognized_amount,
    ADD COLUMN recognized_due_date   DATE            NULL     AFTER recognized_at,
    ADD COLUMN recognized_invoice_id BIGINT UNSIGNED NULL     AFTER recognized_due_date;

-- Laporan piutang memfilter tepat pada dua kolom ini.
CREATE INDEX idx_charge_items_recognized ON charge_items (tenant_id, recognized_at);

-- Jejak audit APPEND-ONLY setiap perubahan kontribusi piutang. Tidak pernah
-- di-update, tidak pernah dihapus (Invariant #5). Σ delta per item WAJIB sama
-- dengan kontribusi berjalan item itu (INV-REC-1).
CREATE TABLE charge_receivable_recognitions (
    id                      BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    tenant_id               BIGINT UNSIGNED NOT NULL,
    charge_group_id         BIGINT UNSIGNED NOT NULL,
    charge_item_id          BIGINT UNSIGNED NOT NULL,
    unit_id                 BIGINT UNSIGNED NOT NULL,
    -- Bertanda: positif menambah piutang, negatif menguranginya.
    delta                   DECIMAL(20,4)   NOT NULL DEFAULT '0.0000',
    reason                  VARCHAR(30)     NOT NULL,
    deposit_account_code    VARCHAR(20)     NOT NULL,
    receivable_account_code VARCHAR(20)     NOT NULL,
    journal_entry_id        BIGINT UNSIGNED NULL,
    invoice_id              BIGINT UNSIGNED NULL,
    created_by              BIGINT UNSIGNED NULL,
    created_at              DATETIME(3)     NULL,
    updated_at              DATETIME(3)     NULL,
    KEY idx_crr_tenant (tenant_id),
    KEY idx_crr_item (tenant_id, charge_item_id),
    KEY idx_crr_group (tenant_id, charge_group_id),
    KEY idx_crr_unit (tenant_id, unit_id),
    KEY idx_crr_journal (tenant_id, journal_entry_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- ── Pencabutan gate BAST (K-2) ───────────────────────────────────────────────
--
-- D-3 final klien: gate BAST TIDAK BOLEH mensyaratkan biaya realisasi lunas.
-- Kebijakannya dicabut, bukan dimatikan — kolom yang tidak dipakai adalah
-- undangan untuk dinyalakan kembali tanpa sengaja.
--
-- Riwayat audit kebijakan (R-A) TIDAK ikut dihapus: tenant_policy_changes
-- bersifat append-only. Untuk tenant yang gate-nya sedang menyala, transisi
-- terakhirnya dicatat dulu supaya riwayatnya tetap utuh dan bisa dijelaskan.
INSERT INTO tenant_policy_changes (tenant_id, policy_key, old_value, new_value, changed_by, notes, created_at, updated_at)
SELECT id, 'require_realization_settled', 'true', 'false', NULL,
       'W-5: gate BAST realisasi dicabut permanen (keputusan klien D-3 — sisa biaya realisasi menjadi Piutang Customer, bukan penahan BAST).',
       NOW(3), NOW(3)
FROM tenants
WHERE require_realization_settled = 1;

ALTER TABLE tenants DROP COLUMN require_realization_settled;

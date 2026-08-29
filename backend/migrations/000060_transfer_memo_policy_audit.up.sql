-- UAT Batch 2 — keputusan klien 2026-08-05 (T-1 & R-A).
--
-- T-1  Transfer Titipan → Harga Rumah adalah TRANSFER INTERNAL, bukan kas masuk
--      baru. Tidak boleh menerbitkan kwitansi pembayaran (KWT). Sebagai gantinya
--      transfer memiliki dokumen sendiri: MEMO TRANSFER INTERNAL (MTI/yyyy/nnnnnn)
--      yang melekat pada baris charge_settlements (append-only) — audit trail
--      tetap ada, tapi pembeli tidak menerima dua bukti atas satu aliran uang.
--
-- R-A  Perubahan kebijakan finansial per tenant (mis. require_realization_settled,
--      gate BAST K-2) wajib ber-audit: siapa, kapan, dari nilai berapa ke berapa.
--      Tabel append-only — tidak pernah di-update atau dihapus (Invariant #5 spirit).

-- ── 1. Nomor memo transfer internal pada settlement ─────────────────────────
-- NULL untuk aksi non-transfer (refund/void) dan untuk transfer lama (pra-000060).
ALTER TABLE charge_settlements
    ADD COLUMN memo_number VARCHAR(30) NULL COMMENT 'MTI/{yyyy}/{6-digit} — dokumen transfer internal (T-1). NULL utk aksi non-transfer.',
    ADD UNIQUE KEY uk_cs_memo (tenant_id, memo_number);

-- ── 2. Seri dokumen MTI ─────────────────────────────────────────────────────
-- Memakai receipt_sequences yang sudah ada (kunci: tenant_id + doc_type) agar
-- hanya ada SATU mekanisme penomoran dokumen. doc_type 'internal_transfer' →
-- prefix MTI, seri sendiri (tidak menyusul KWT/KWB/KWR).

-- ── 3. Audit perubahan kebijakan tenant (R-A) ───────────────────────────────
CREATE TABLE tenant_policy_changes (
    id          BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    tenant_id   BIGINT UNSIGNED NOT NULL,
    policy_key  VARCHAR(64)     NOT NULL,   -- mis. 'require_realization_settled'
    old_value   VARCHAR(64)     NOT NULL,   -- representasi string (audit apa adanya)
    new_value   VARCHAR(64)     NOT NULL,
    changed_by  BIGINT UNSIGNED NULL,       -- user_id aktor; NULL = sistem/migrasi
    notes       VARCHAR(500)    NULL,
    created_at  DATETIME(3),
    updated_at  DATETIME(3),

    INDEX idx_tpc_tenant (tenant_id),
    INDEX idx_tpc_key    (tenant_id, policy_key, id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
  COMMENT='APPEND-ONLY: riwayat perubahan kebijakan finansial per tenant (R-A 2026-08-05)';

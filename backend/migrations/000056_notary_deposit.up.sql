-- UAT Batch 2 §3 — Titipan Notaris.
-- Uang customer yang dititipkan untuk notaris = LIABILITY developer (bukan
-- pendapatan) sampai dibayarkan ke notaris. Tidak pernah menyentuh 4-xxxx.

-- 1) Akun 2-2300 Titipan Notaris untuk semua tenant existing (idempoten;
--    clone atribut dari 2-2100 — sama-sama liability titipan).
INSERT INTO accounts (tenant_id, code, name, type, normal_balance, is_system, description, is_active, category)
SELECT a.tenant_id, '2-2300', 'Titipan Notaris', a.type, a.normal_balance, 1,
       'Dana customer yang dititipkan untuk biaya notaris — kewajiban sampai dibayarkan ke notaris (UAT Batch 2 §3). BUKAN pendapatan developer.',
       1, a.category
FROM accounts a
WHERE a.code = '2-2100'
  AND NOT EXISTS (SELECT 1 FROM accounts b WHERE b.tenant_id = a.tenant_id AND b.code = '2-2300');

-- 2) Dokumen sub-ledger titipan notaris (rekonsiliasi Σ held == saldo 2-2300).
CREATE TABLE notary_deposits (
    id                 BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    tenant_id          BIGINT UNSIGNED NOT NULL,
    customer_id        BIGINT UNSIGNED NOT NULL,
    unit_id            BIGINT UNSIGNED NULL,
    amount             DECIMAL(20,4) NOT NULL DEFAULT '0.0000',
    status             VARCHAR(20) NOT NULL DEFAULT 'held', -- held | paid_out
    notary_name        VARCHAR(200) NOT NULL DEFAULT '',
    notes              VARCHAR(500) NOT NULL DEFAULT '',
    receive_journal_id BIGINT UNSIGNED NOT NULL,
    payout_journal_id  BIGINT UNSIGNED NULL,
    received_at        DATETIME(3) NOT NULL,
    paid_out_at        DATETIME(3) NULL,
    created_by         BIGINT UNSIGNED NULL,
    created_at         DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at         DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    CONSTRAINT chk_notary_status CHECK (status IN ('held','paid_out')),
    INDEX idx_notary_tenant (tenant_id),
    INDEX idx_notary_tenant_status (tenant_id, status),
    INDEX idx_notary_customer (tenant_id, customer_id)
);

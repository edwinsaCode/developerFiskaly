-- Increment 3 — Payment Scheme (strategy seam).
-- Blueprint: blueprint-domain-additions.md §5; desain: docs/increment-3-payment-scheme-design.md.
-- Semua ADD-only (IMPL-2): kolom lama (payment_type, bank_kpr, loan_amount) TIDAK
-- disentuh — legacy read-only untuk kompatibilitas. Kontrak lama = snapshot NULL
-- (tanpa state machine/gate); kontrak baru wajib scheme (ditegakkan service).

-- ── 1. Master: payment_schemes (config per tenant, pola Tax Rule) ─────────────
CREATE TABLE IF NOT EXISTS payment_schemes (
    id          BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    tenant_id   BIGINT UNSIGNED NOT NULL,
    code        VARCHAR(30)     NOT NULL COMMENT 'immutable (identitas master, pola customers.code)',
    name        VARCHAR(100)    NOT NULL,
    policy_type VARCHAR(30)     NOT NULL COMMENT 'binding ke strategy: cash|cash_installment|kpr|inhouse',
    params      JSON            NOT NULL COMMENT 'parameter default scheme; kontrak MEMBEKUKAN salinannya (snapshot)',
    is_active   TINYINT(1)      NOT NULL DEFAULT 1,
    created_at  DATETIME(3)     NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at  DATETIME(3)     NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    UNIQUE KEY uq_ps_tenant_code (tenant_id, code),
    KEY idx_ps_tenant_id (tenant_id),
    CONSTRAINT chk_ps_policy_type CHECK (
        policy_type IN ('cash', 'cash_installment', 'kpr', 'inhouse')
    )
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- ── 2. Master: financing_sources (bank penyalur — mengubur bank_kpr free-text) ─
CREATE TABLE IF NOT EXISTS financing_sources (
    id         BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    tenant_id  BIGINT UNSIGNED NOT NULL,
    code       VARCHAR(30)     NOT NULL COMMENT 'immutable',
    name       VARCHAR(200)    NOT NULL,
    type       VARCHAR(30)     NOT NULL DEFAULT 'bank_kpr_komersial'
        COMMENT 'bank_kpr_subsidi|bank_kpr_komersial|lainnya — BUKAN penentu pajak (Project.tax_category)',
    is_active  TINYINT(1)      NOT NULL DEFAULT 1,
    created_at DATETIME(3)     NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at DATETIME(3)     NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    UNIQUE KEY uq_fs_tenant_code (tenant_id, code),
    KEY idx_fs_tenant_id (tenant_id),
    CONSTRAINT chk_fs_type CHECK (
        type IN ('bank_kpr_subsidi', 'bank_kpr_komersial', 'lainnya')
    )
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- ── 3. Payment Event append-only (approval note #3 — audit trail + dashboard) ──
-- TIDAK ada jalur UPDATE/DELETE dari aplikasi; scheme_state kontrak hanyalah
-- proyeksi event terakhir.
CREATE TABLE IF NOT EXISTS contract_payment_events (
    id                  BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    tenant_id           BIGINT UNSIGNED NOT NULL,
    sale_contract_id    BIGINT UNSIGNED NOT NULL,
    event               VARCHAR(30)     NOT NULL
        COMMENT 'contract_signed|dp_paid|submitted_to_bank|bank_approved|bank_rejected|akad|disbursed|takeover|rescheduled|converted|fully_paid|handed_over|cancelled',
    from_state          VARCHAR(30)     NOT NULL DEFAULT '',
    to_state            VARCHAR(30)     NOT NULL DEFAULT '',
    financing_source_id BIGINT UNSIGNED NULL,
    journal_entry_id    BIGINT UNSIGNED NULL COMMENT 'terisi bila event memicu jurnal (mis. reklas akad pasca-BAST)',
    notes               VARCHAR(500)    NOT NULL DEFAULT '',
    created_by          BIGINT UNSIGNED NULL,
    created_at          DATETIME(3)     NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at          DATETIME(3)     NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    KEY idx_cpe_tenant_id (tenant_id),
    KEY idx_cpe_contract (tenant_id, sale_contract_id),
    CONSTRAINT fk_cpe_contract FOREIGN KEY (sale_contract_id) REFERENCES sale_contracts (id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- ── 4. sale_contracts: kolom scheme (semua nullable — kontrak lama = legacy) ───
ALTER TABLE sale_contracts
    ADD COLUMN payment_scheme_id BIGINT UNSIGNED NULL
        COMMENT 'reference ke payment_schemes; NULL = kontrak legacy (payment_type)',
    ADD COLUMN financing_source_id BIGINT UNSIGNED NULL
        COMMENT 'bank penyalur KPR; menggantikan bank_kpr free-text utk kontrak baru',
    ADD COLUMN scheme_state VARCHAR(30) NULL
        COMMENT 'state machine scheme; NULL = legacy (tanpa state machine/gate)',
    ADD COLUMN scheme_params_snapshot JSON NULL
        COMMENT 'Contract Payment Terms Snapshot (approval note #2) — beku saat kontrak; kontrak TIDAK membaca master aktif',
    ADD COLUMN approval_request_id BIGINT UNSIGNED NULL
        COMMENT 'seam Approval Workflow (blueprint §10) — engine menyusul',
    ADD INDEX idx_sc_scheme (tenant_id, payment_scheme_id),
    ADD INDEX idx_sc_finsource (financing_source_id),
    ADD CONSTRAINT fk_sc_scheme FOREIGN KEY (payment_scheme_id) REFERENCES payment_schemes (id),
    ADD CONSTRAINT fk_sc_finsource FOREIGN KEY (financing_source_id) REFERENCES financing_sources (id);

-- ── 5. termin_payments: tag pencairan bank (payment_source='kpr_disbursement') ─
ALTER TABLE termin_payments
    ADD COLUMN financing_source_id BIGINT UNSIGNED NULL
        COMMENT 'terisi bila penerimaan berasal dari pencairan bank (KPR)',
    ADD INDEX idx_tp_finsource (financing_source_id);

-- ── 6. payment_schedules: versi jadwal (supersede pattern, BS-5) ──────────────
ALTER TABLE payment_schedules
    ADD COLUMN schedule_version INT NOT NULL DEFAULT 1
        COMMENT 'naik saat reschedule/konversi; baris lama status=superseded (tidak dihapus)';

-- ── 7. COA: 1-2200 Piutang Bank (KPR) untuk tenant existing ───────────────────
-- (Tenant baru mendapatkannya via ledger.SeedCOA yang diperbarui.)
INSERT INTO accounts (tenant_id, code, name, type, normal_balance, is_system, is_active, category, description)
SELECT t.id, '1-2200', 'Piutang Bank (KPR)', 'asset', 'debit', 1, 1, '',
       'Piutang kepada bank KPR setelah akad — counterparty AR per skema pembayaran'
FROM tenants t
WHERE NOT EXISTS (
    SELECT 1 FROM accounts a WHERE a.tenant_id = t.id AND a.code = '1-2200'
);

-- ── 8. Seed default schemes untuk tenant existing ─────────────────────────────
-- (Tenant baru: scheme.SeedDefaultSchemes di registrasi — sumber parameter yang sama.)
INSERT INTO payment_schemes (tenant_id, code, name, policy_type, params)
SELECT t.id, s.code, s.name, s.policy_type, s.params
FROM tenants t
JOIN (
    SELECT 'CASH' AS code, 'Tunai Keras' AS name, 'cash' AS policy_type,
           JSON_OBJECT('bast_gate','full_payment','receivable_account','1-2000') AS params
    UNION ALL SELECT 'CASH-BTHP', 'Tunai Bertahap', 'cash_installment',
           JSON_OBJECT('dp_percent','20','installment_count',3,'bast_gate','full_payment','receivable_account','1-2000')
    UNION ALL SELECT 'KPR-SUB', 'KPR Subsidi', 'kpr',
           JSON_OBJECT('dp_percent','1','final_due_months',6,'bast_gate','akad','receivable_account','1-2000','financing_receivable_account','1-2200')
    UNION ALL SELECT 'KPR-KOM', 'KPR Komersial', 'kpr',
           JSON_OBJECT('dp_percent','10','final_due_months',6,'bast_gate','akad','receivable_account','1-2000','financing_receivable_account','1-2200')
    UNION ALL SELECT 'INHOUSE', 'In-House (cicilan developer)', 'inhouse',
           JSON_OBJECT('dp_percent','20','tenor_months',24,'bast_gate','dp_paid','receivable_account','1-2000')
) s
WHERE NOT EXISTS (
    SELECT 1 FROM payment_schemes ps WHERE ps.tenant_id = t.id AND ps.code = s.code
);

-- ── 9. Backfill legacy: payment_type → payment_scheme_id (approval note #4) ────
-- Deterministik: tunai→CASH, kpr→KPR-KOM. scheme_state & snapshot TETAP NULL
-- (legacy semantics: tanpa state machine/gate — perilaku lama tidak berubah).
UPDATE sale_contracts sc
JOIN payment_schemes ps
  ON ps.tenant_id = sc.tenant_id
 AND ps.code = CASE sc.payment_type WHEN 'tunai' THEN 'CASH' WHEN 'kpr' THEN 'KPR-KOM' END
SET sc.payment_scheme_id = ps.id
WHERE sc.payment_scheme_id IS NULL;

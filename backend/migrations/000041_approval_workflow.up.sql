-- Increment 5 — Generic Approval Workflow (blueprint blueprint-domain-additions.md §10).
-- CORE governance, cross-cutting: SATU mekanisme approval untuk semua modul
-- (RAB, True-up, Refund/Cancellation, Commission, Discount, ...) via target
-- POLIMORFIK. TANPA dampak ledger — hanya MEM-GATE transisi/posting modul.
--
-- Config (template):   approval_workflows → approval_steps
-- Runtime (instance):  approval_requests → approval_actions (APPEND-ONLY)
-- Governance OPT-IN: modul digate HANYA bila workflow aktif utk target_type-nya
-- terkonfigurasi — tenant tanpa config berperilaku seperti sebelumnya (additive).

CREATE TABLE IF NOT EXISTS approval_workflows (
    id          BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    tenant_id   BIGINT UNSIGNED NOT NULL,
    target_type VARCHAR(30)     NOT NULL
        COMMENT 'rab|hpp_trueup|refund|cancellation|commission|discount|cost|payment|manual_journal',
    name        VARCHAR(200)    NOT NULL,
    is_active   TINYINT(1)      NOT NULL DEFAULT 1,
    -- Satu workflow AKTIF per (tenant, target_type) — pola active_key budget_plans
    -- (MySQL mengizinkan banyak NULL pada UNIQUE; hanya satu 'Y').
    active_key  VARCHAR(1)      NULL,
    created_by  BIGINT UNSIGNED NULL,
    created_at  DATETIME(3)     NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at  DATETIME(3)     NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    UNIQUE KEY uq_aw_one_active (tenant_id, target_type, active_key),
    KEY idx_aw_tenant_id (tenant_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS approval_steps (
    id            BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    tenant_id     BIGINT UNSIGNED NOT NULL,
    workflow_id   BIGINT UNSIGNED NOT NULL,
    seq           INT             NOT NULL COMMENT 'urutan step (1..n)',
    name          VARCHAR(200)    NOT NULL DEFAULT '',
    approver_role VARCHAR(30)     NOT NULL COMMENT 'role JWT yang boleh memutus step ini (owner|accountant|...)',
    -- Threshold (Approval Matrix seam): step hanya berlaku bila amount request
    -- ≥ min_amount. 0 = selalu berlaku.
    min_amount    DECIMAL(20,4)   NOT NULL DEFAULT '0.0000',
    -- Quorum: jumlah persetujuan yang dibutuhkan pada step ini (default 1).
    quorum        INT             NOT NULL DEFAULT 1,
    created_at    DATETIME(3)     NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at    DATETIME(3)     NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    UNIQUE KEY uq_as_workflow_seq (workflow_id, seq),
    KEY idx_as_tenant_id (tenant_id),
    CONSTRAINT fk_as_workflow FOREIGN KEY (workflow_id)
        REFERENCES approval_workflows (id) ON DELETE CASCADE,
    CONSTRAINT chk_as_quorum CHECK (quorum >= 1),
    CONSTRAINT chk_as_seq CHECK (seq >= 1)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS approval_requests (
    id           BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    tenant_id    BIGINT UNSIGNED NOT NULL,
    workflow_id  BIGINT UNSIGNED NOT NULL,
    target_type  VARCHAR(30)     NOT NULL,
    target_id    BIGINT UNSIGNED NOT NULL COMMENT 'id dokumen target (polimorfik)',
    status       VARCHAR(20)     NOT NULL DEFAULT 'pending'
        COMMENT 'pending|in_review|approved|rejected|cancelled',
    current_seq  INT             NOT NULL DEFAULT 1 COMMENT 'step aktif saat ini',
    amount       DECIMAL(20,4)   NOT NULL DEFAULT '0.0000'
        COMMENT 'konteks nominal utk threshold step (0 bila tidak relevan)',
    -- Satu request AKTIF (pending/in_review) per target — active_key pattern.
    active_key   VARCHAR(1)      NULL,
    requested_by BIGINT UNSIGNED NULL,
    notes        VARCHAR(500)    NOT NULL DEFAULT '',
    decided_at   DATETIME(3)     NULL COMMENT 'saat mencapai status terminal',
    created_at   DATETIME(3)     NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at   DATETIME(3)     NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    UNIQUE KEY uq_ar_one_active (tenant_id, target_type, target_id, active_key),
    KEY idx_ar_tenant_id (tenant_id),
    KEY idx_ar_target (tenant_id, target_type, target_id),
    CONSTRAINT fk_ar_workflow FOREIGN KEY (workflow_id) REFERENCES approval_workflows (id),
    CONSTRAINT chk_ar_status CHECK (
        status IN ('pending', 'in_review', 'approved', 'rejected', 'cancelled')
    )
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- APPEND-ONLY: tidak ada jalur UPDATE/DELETE dari aplikasi.
CREATE TABLE IF NOT EXISTS approval_actions (
    id          BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    tenant_id   BIGINT UNSIGNED NOT NULL,
    request_id  BIGINT UNSIGNED NOT NULL,
    step_seq    INT             NOT NULL,
    actor_id    BIGINT UNSIGNED NOT NULL,
    actor_role  VARCHAR(30)     NOT NULL,
    decision    VARCHAR(20)     NOT NULL COMMENT 'approve|reject',
    comment     VARCHAR(500)    NOT NULL DEFAULT '',
    created_at  DATETIME(3)     NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at  DATETIME(3)     NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    KEY idx_aa_tenant_id (tenant_id),
    KEY idx_aa_request (tenant_id, request_id),
    CONSTRAINT fk_aa_request FOREIGN KEY (request_id)
        REFERENCES approval_requests (id) ON DELETE CASCADE,
    CONSTRAINT chk_aa_decision CHECK (decision IN ('approve', 'reject'))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

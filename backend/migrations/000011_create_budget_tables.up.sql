-- Phase 4: RAB (Rencana Anggaran Biaya)
-- Budget plans dan items. RAB tidak memposting ke ledger — hanya perencanaan.
--
-- INVARIANT #8: hanya satu budget_plan berstatus 'active' per (tenant_id, project_id, phase_id).
-- Dijaga oleh DUA lapisan:
--   1. UNIQUE INDEX udx_budget_plans_one_active (tenant_id, project_id, phase_id_key, active_key)
--      active_key = 'Y' saat active, NULL saat draft/superseded.
--      MySQL mengizinkan banyak NULL dalam UNIQUE — hanya satu 'Y' per kombinasi yang bisa ada.
--      phase_id_key = 0 untuk project-level (phase_id IS NULL), else phase_id.
--      Ini mencegah race condition & direct DB write yang melanggar invariant.
--   2. ApproveAndSupersede di aplikasi menjaga active_key secara atomik dalam transaksi.

CREATE TABLE IF NOT EXISTS budget_plans (
    id           BIGINT UNSIGNED   NOT NULL AUTO_INCREMENT,
    tenant_id    BIGINT UNSIGNED   NOT NULL,
    project_id   BIGINT UNSIGNED   NOT NULL,
    phase_id     BIGINT UNSIGNED   NULL,
    phase_id_key BIGINT UNSIGNED   NOT NULL DEFAULT 0 COMMENT '0=project-level; else=phase_id. Sentinel untuk UNIQUE index karena MySQL NULL != NULL.',
    active_key   CHAR(1)           NULL     COMMENT 'Y saat active, NULL saat draft/superseded. NULL diizinkan banyak dalam UNIQUE; hanya satu Y per (tenant,project,phase_id_key).',
    version      INT               NOT NULL DEFAULT 1,
    label        VARCHAR(100)      NOT NULL,
    status       VARCHAR(20)       NOT NULL DEFAULT 'draft',
    notes        VARCHAR(2000)     NOT NULL DEFAULT '',
    approved_at  DATETIME(3)       NULL,
    approved_by  VARCHAR(200)      NULL,
    created_at   DATETIME(3)       NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at   DATETIME(3)       NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),

    PRIMARY KEY (id),
    INDEX idx_budget_plans_tenant    (tenant_id),
    INDEX idx_budget_plans_project   (tenant_id, project_id),
    INDEX idx_budget_plans_phase     (tenant_id, project_id, phase_id),

    -- INVARIANT #8 enforcement: hanya satu baris dengan active_key='Y' per
    -- (tenant_id, project_id, phase_id_key). Baris dengan active_key=NULL
    -- (draft/superseded) tidak dibatasi karena MySQL NULL != NULL dalam UNIQUE.
    UNIQUE KEY udx_budget_plans_one_active (tenant_id, project_id, phase_id_key, active_key),

    CONSTRAINT chk_budget_plan_status
        CHECK (status IN ('draft', 'active', 'superseded')),
    CONSTRAINT chk_budget_plan_active_key
        CHECK (active_key IS NULL OR active_key = 'Y')
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS budget_items (
    id              BIGINT UNSIGNED    NOT NULL AUTO_INCREMENT,
    tenant_id       BIGINT UNSIGNED    NOT NULL,
    budget_plan_id  BIGINT UNSIGNED    NOT NULL,
    category        VARCHAR(20)        NOT NULL,
    subcategory     VARCHAR(100)       NOT NULL DEFAULT '',
    description     VARCHAR(500)       NOT NULL DEFAULT '',
    budgeted_amount DECIMAL(20,4)      NOT NULL DEFAULT '0.0000',
    created_at      DATETIME(3)        NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at      DATETIME(3)        NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),

    PRIMARY KEY (id),
    INDEX idx_budget_items_tenant  (tenant_id),
    INDEX idx_budget_items_plan    (tenant_id, budget_plan_id),

    CONSTRAINT chk_budget_item_category
        CHECK (category IN ('land', 'construction', 'soft', 'financing', 'marketing', 'other')),
    CONSTRAINT chk_budget_item_amount_positive
        CHECK (budgeted_amount > 0),

    FOREIGN KEY (budget_plan_id) REFERENCES budget_plans(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
